package ride

import (
	"context"
	"errors"
	"sync"
	"time"

	"ride-hail/internal/shared/logging"
	"ride-hail/internal/shared/ws"
)

const matchingTimeout = 2 * time.Minute

var ErrRepositoryUnavailable = errors.New("ride repository is unavailable")

type Repository interface {
	CreateRide(ctx context.Context, ride *Ride) error
	GetRide(ctx context.Context, rideID string) (*Ride, error)
	CancelRide(ctx context.Context, rideID, reason string, refundAmount float64) (*Ride, error)
	UpdateRideMatched(ctx context.Context, rideID, driverID string) (*Ride, error)
	UpdateRideStatus(
		ctx context.Context,
		rideID string,
		newStatus string,
		eventType string,
		eventData map[string]any,
		finalFare *float64,
	) (*Ride, error)
}

type Service struct {
	repository Repository
	events     *EventPublisher
	hub        *ws.Hub
	log        *logging.Logger

	mu            sync.Mutex
	matchTimers   map[string]*time.Timer
	correlationID map[string]string
}

func NewService(
	repository Repository,
	events *EventPublisher,
	hub *ws.Hub,
	log *logging.Logger,
) *Service {
	return &Service{
		repository:    repository,
		events:        events,
		hub:           hub,
		log:           log,
		matchTimers:   make(map[string]*time.Timer),
		correlationID: make(map[string]string),
	}
}

func (s *Service) CreateRide(
	ctx context.Context,
	request CreateRideRequest,
) (*Ride, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s == nil || s.repository == nil {
		return nil, ErrRepositoryUnavailable
	}

	if err := ValidateCreateRideRequest(&request); err != nil {
		return nil, err
	}

	estimate, err := CalculateFareEstimate(request)
	if err != nil {
		return nil, err
	}

	ride := &Ride{
		PassengerID: request.PassengerID,
		Pickup: Coordinate{
			Latitude:  request.PickupLatitude,
			Longitude: request.PickupLongitude,
			Address:   request.PickupAddress,
		},
		Destination: Coordinate{
			Latitude:  request.DestinationLatitude,
			Longitude: request.DestinationLongitude,
			Address:   request.DestinationAddress,
		},
		RideType:              request.RideType,
		Status:                RideStatusRequested,
		EstimatedDistanceKM:   estimate.DistanceKM,
		EstimatedDurationMins: estimate.DurationMinutes,
		EstimatedFare:         estimate.Fare,
	}

	if err := s.repository.CreateRide(ctx, ride); err != nil {
		return nil, err
	}

	correlationID := newCorrelationID()
	s.rememberCorrelation(ride.ID, correlationID)

	if s.events != nil {
		if err := s.events.PublishRideRequest(ride, correlationID); err != nil {
			s.log.Error(ctx, "ride_request_publish_failed", "failed to publish ride request", err, "ride_id", ride.ID)
			return nil, err
		}
	}

	s.startMatchingTimeout(ride.ID)
	s.log.Info(ctx, "ride_created", "ride request stored and published", "ride_id", ride.ID)

	return ride, nil
}

func (s *Service) CancelRide(
	ctx context.Context,
	rideID string,
	passengerID string,
	reason string,
) (*CancelResult, error) {
	return s.cancelRide(ctx, rideID, passengerID, reason)
}

func (s *Service) cancelRide(
	ctx context.Context,
	rideID string,
	passengerID string,
	reason string,
) (*CancelResult, error) {
	if s == nil || s.repository == nil {
		return nil, ErrRepositoryUnavailable
	}

	if reason == "" {
		reason = "Cancelled by passenger"
	}

	ride, err := s.repository.GetRide(ctx, rideID)
	if err != nil {
		return nil, err
	}

	if passengerID != "" && ride.PassengerID != passengerID {
		return nil, ErrPassengerMismatch
	}

	refundAmount := CalculateRefund(ride.Status, ride.EstimatedFare)
	updated, err := s.repository.CancelRide(ctx, rideID, reason, refundAmount)
	if err != nil {
		return nil, err
	}

	s.clearMatchingTimeout(rideID)
	correlationID := s.correlationFor(rideID)

	if s.events != nil {
		if err := s.events.PublishRideStatus(updated, correlationID); err != nil {
			s.log.Error(ctx, "ride_status_publish_failed", "failed to publish cancelled status", err, "ride_id", rideID)
		}
	}

	notifyPassenger(s.hub, updated.PassengerID, PassengerStatusUpdate{
		Type:    "ride_status_update",
		RideID:  updated.ID,
		Status:  updated.Status,
		Message: statusMessage(updated.Status),
	})

	return &CancelResult{Ride: updated, RefundAmount: refundAmount}, nil
}

func (s *Service) HandleDriverMatched(ctx context.Context, message DriverResponseMessage) error {
	ride, err := s.repository.UpdateRideMatched(ctx, message.RideID, message.DriverID)
	if err != nil {
		if errors.Is(err, ErrRideNotFound) || errors.Is(err, ErrRideAlreadyMatched) {
			return nil
		}
		return err
	}

	s.clearMatchingTimeout(message.RideID)
	correlationID := s.correlationFor(message.RideID)
	if message.CorrelationID != "" {
		correlationID = message.CorrelationID
	}

	if s.events != nil {
		if err := s.events.PublishRideStatus(ride, correlationID); err != nil {
			s.log.Error(ctx, "ride_status_publish_failed", "failed to publish matched status", err)
		}
	}

	notifyPassenger(s.hub, ride.PassengerID, PassengerStatusUpdate{
		Type:          "ride_status_update",
		RideID:        ride.ID,
		RideNumber:    ride.RideNumber,
		Status:        ride.Status,
		DriverInfo:    message.DriverInfo,
		CorrelationID: correlationID,
		Message:       statusMessage(ride.Status),
	})

	s.log.Info(ctx, "ride_matched", "driver assigned to ride", "driver_id", message.DriverID)
	return nil
}

func (s *Service) HandleDriverStatus(ctx context.Context, message DriverStatusMessage) error {
	ride, err := s.repository.GetRide(ctx, message.RideID)
	if err != nil {
		if errors.Is(err, ErrRideNotFound) {
			return nil
		}
		return err
	}

	newStatus, ok := mapDriverStatusToRideStatus(message.Status)
	if !ok {
		if message.Status == "AVAILABLE" && ride.Status == RideStatusInProgress {
			newStatus = RideStatusCompleted
		} else {
			return nil
		}
	}

	if ride.Status == newStatus {
		return nil
	}

	var finalFare *float64
	if newStatus == RideStatusCompleted {
		fare, err := calculateFinalFare(ride)
		if err != nil {
			return err
		}
		finalFare = &fare
	}

	updated, err := s.repository.UpdateRideStatus(ctx, message.RideID, newStatus, "STATUS_CHANGED", map[string]any{
		"driver_id": message.DriverID,
	}, finalFare)
	if err != nil {
		return err
	}

	correlationID := s.correlationFor(message.RideID)
	if s.events != nil {
		if err := s.events.PublishRideStatus(updated, correlationID); err != nil {
			s.log.Error(ctx, "ride_status_publish_failed", "failed to publish ride status", err)
		}
	}

	notifyPassenger(s.hub, updated.PassengerID, PassengerStatusUpdate{
		Type:       "ride_status_update",
		RideID:     updated.ID,
		RideNumber: updated.RideNumber,
		Status:     updated.Status,
		Message:    statusMessage(updated.Status),
		FinalFare:  updated.FinalFare,
	})

	return nil
}

func (s *Service) HandleLocationUpdate(ctx context.Context, message LocationUpdateMessage) error {
	ride, err := s.repository.GetRide(ctx, message.RideID)
	if err != nil {
		if errors.Is(err, ErrRideNotFound) {
			return nil
		}
		return err
	}

	distanceKM := CalculateDistanceKM(
		message.Location.Lat,
		message.Location.Lng,
		ride.Pickup.Latitude,
		ride.Pickup.Longitude,
	)

	notifyPassenger(s.hub, ride.PassengerID, PassengerLocationUpdate{
		Type:   "driver_location_update",
		RideID: message.RideID,
		DriverLocation: MQPoint{
			Lat: message.Location.Lat,
			Lng: message.Location.Lng,
		},
		DistanceToPickupKM: round(distanceKM, distanceRoundingPrecision),
		EstimatedArrival:   estimateArrivalTimestamp(EstimateDurationMinutes(distanceKM)),
	})

	return nil
}

func (s *Service) startMatchingTimeout(rideID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if timer, ok := s.matchTimers[rideID]; ok {
		timer.Stop()
	}

	s.matchTimers[rideID] = time.AfterFunc(matchingTimeout, func() {
		ctx := withRideID(context.Background(), rideID)
		ride, err := s.repository.GetRide(ctx, rideID)
		if err != nil || ride.Status != RideStatusRequested {
			return
		}

		_, err = s.cancelRide(ctx, rideID, "", "No driver found within matching timeout")
		if err != nil && !errors.Is(err, ErrRideNotCancellable) {
			s.log.Error(ctx, "matching_timeout_failed", "failed to cancel ride after timeout", err)
		}
	})
}

func (s *Service) clearMatchingTimeout(rideID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if timer, ok := s.matchTimers[rideID]; ok {
		timer.Stop()
		delete(s.matchTimers, rideID)
	}
}

func (s *Service) rememberCorrelation(rideID, correlationID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.correlationID[rideID] = correlationID
}

func (s *Service) correlationFor(rideID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.correlationID[rideID]
}
