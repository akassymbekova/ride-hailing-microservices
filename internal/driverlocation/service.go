package driverlocation

import (
	"context"
	"encoding/json"
	"strings"

	"ride-hail/internal/shared/logging"
	"ride-hail/internal/shared/messaging"
	"ride-hail/internal/shared/ws"
)

type Service struct {
	matcher   *Matcher
	repo      *Repository
	hub       *ws.Hub
	publisher *messaging.Publisher
	log       *logging.Logger
}

func NewService(
	matcher *Matcher,
	repo *Repository,
	hub *ws.Hub,
	publisher *messaging.Publisher,
	log *logging.Logger,
) *Service {
	return &Service{
		matcher:   matcher,
		repo:      repo,
		hub:       hub,
		publisher: publisher,
		log:       log,
	}
}

func (s *Service) StartConsuming(ctx context.Context, conn *messaging.Connection) {
	messaging.RunConsumer(ctx, conn, "driver_matching", s.log, s.handleRideRequest)
	messaging.RunConsumer(ctx, conn, "ride_status", s.log, s.handleRideStatus)
}

func (s *Service) handleRideRequest(body []byte) error {
	var req RideRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.log.Error(context.Background(), "ride_request_parse_failed", "failed to parse ride request message", err)
		return err
	}

	go s.matcher.MatchRide(context.Background(), req)

	return nil
}

func (s *Service) handleRideStatus(body []byte) error {
	var msg RideStatusMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		s.log.Error(context.Background(), "ride_status_parse_failed", "failed to parse ride status message", err)
		return err
	}

	if !shouldSendRideDetails(msg) {
		return nil
	}

	ctx := context.Background()
	if err := s.sendRideDetails(ctx, msg.RideID, msg.DriverID); err != nil {
		s.log.Error(ctx, "ride_details_send_failed", "failed to send ride details to driver", err, "ride_id", msg.RideID)
		return err
	}

	if err := s.repo.SetDriverStatus(ctx, msg.DriverID, DriverStatusEnRoute); err != nil {
		s.log.Error(ctx, "driver_en_route_failed", "failed to set driver en route", err, "ride_id", msg.RideID)
		return err
	}
	if err := publishDriverStatus(ctx, s.publisher, msg.DriverID, DriverStatusEnRoute, msg.RideID); err != nil {
		s.log.Error(ctx, "driver_en_route_publish_failed", "failed to publish driver en route status", err, "ride_id", msg.RideID)
		return err
	}

	s.log.Info(ctx, "driver_en_route", "driver is en route to pickup", "ride_id", msg.RideID, "driver_id", msg.DriverID)
	return nil
}

func shouldSendRideDetails(msg RideStatusMessage) bool {
	return strings.EqualFold(msg.Status, "MATCHED") &&
		msg.RideID != "" &&
		msg.DriverID != ""
}

func (s *Service) sendRideDetails(ctx context.Context, rideID, driverID string) error {
	details, err := s.repo.GetRideDetailsForDriver(ctx, rideID)
	if err != nil {
		return err
	}

	payload := RideDetails{
		Type:           "ride_details",
		RideID:         details.RideID,
		PassengerName:  details.PassengerName,
		PassengerPhone: details.PassengerPhone,
		PickupLocation: RideDetailsLocation{
			Latitude:  details.PickupLat,
			Longitude: details.PickupLng,
			Address:   details.PickupAddress,
			Notes:     details.PickupNotes,
		},
	}

	if !s.hub.SendJSON(driverID, payload) {
		s.log.Info(ctx, "ride_details_driver_offline", "driver is not connected via websocket", "ride_id", rideID, "driver_id", driverID)
		return nil
	}

	s.log.Info(ctx, "ride_details_sent", "ride details sent to driver", "ride_id", rideID, "driver_id", driverID)
	return nil
}
