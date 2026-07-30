package ride

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"ride-hail/internal/shared/logging"
	"ride-hail/internal/shared/messaging"
	"ride-hail/internal/shared/ws"
)

type Consumer struct {
	service *Service
	log     *logging.Logger
}

func NewConsumer(service *Service, log *logging.Logger) *Consumer {
	return &Consumer{service: service, log: log}
}

func (c *Consumer) Start(ctx context.Context, conn *messaging.Connection) {
	messaging.RunConsumer(ctx, conn, "driver_responses", c.log, c.handleDriverResponse)
	messaging.RunConsumer(ctx, conn, "driver_status", c.log, c.handleDriverStatus)
	messaging.RunConsumer(ctx, conn, "location_updates_ride", c.log, c.handleLocationUpdate)
}

func (c *Consumer) handleDriverResponse(body []byte) error {
	var message DriverResponseMessage
	if err := json.Unmarshal(body, &message); err != nil {
		return err
	}

	if !message.Accepted || message.RideID == "" || message.DriverID == "" {
		return nil
	}

	ctx := withRideID(context.Background(), message.RideID)
	return c.service.HandleDriverMatched(ctx, message)
}

func (c *Consumer) handleDriverStatus(body []byte) error {
	var message DriverStatusMessage
	if err := json.Unmarshal(body, &message); err != nil {
		return err
	}

	if message.RideID == "" {
		return nil
	}

	ctx := withRideID(context.Background(), message.RideID)
	return c.service.HandleDriverStatus(ctx, message)
}

func (c *Consumer) handleLocationUpdate(body []byte) error {
	var message LocationUpdateMessage
	if err := json.Unmarshal(body, &message); err != nil {
		return err
	}

	if message.RideID == "" {
		return nil
	}

	ctx := withRideID(context.Background(), message.RideID)
	return c.service.HandleLocationUpdate(ctx, message)
}

func mapDriverStatusToRideStatus(driverStatus string) (string, bool) {
	switch strings.ToUpper(driverStatus) {
	case "EN_ROUTE":
		return RideStatusEnRoute, true
	case "ARRIVED":
		return RideStatusArrived, true
	case "BUSY":
		return RideStatusInProgress, true
	case "AVAILABLE":
		return RideStatusCompleted, false
	default:
		return "", false
	}
}

type PassengerStatusUpdate struct {
	Type          string      `json:"type"`
	RideID        string      `json:"ride_id"`
	RideNumber    string      `json:"ride_number,omitempty"`
	Status        string      `json:"status"`
	Message       string      `json:"message,omitempty"`
	DriverInfo    *DriverInfo `json:"driver_info,omitempty"`
	CorrelationID string      `json:"correlation_id,omitempty"`
	FinalFare     *float64    `json:"final_fare,omitempty"`
}

type PassengerLocationUpdate struct {
	Type               string  `json:"type"`
	RideID             string  `json:"ride_id"`
	DriverLocation     MQPoint `json:"driver_location"`
	EstimatedArrival   string  `json:"estimated_arrival,omitempty"`
	DistanceToPickupKM float64 `json:"distance_to_pickup_km,omitempty"`
}

func notifyPassenger(hub *ws.Hub, passengerID string, payload any) {
	if hub == nil || passengerID == "" {
		return
	}
	_ = hub.SendJSON(passengerID, payload)
}

func statusMessage(status string) string {
	switch status {
	case RideStatusMatched:
		return "Your driver has been matched"
	case RideStatusEnRoute:
		return "Your driver is on the way"
	case RideStatusArrived:
		return "Your driver has arrived at the pickup location"
	case RideStatusInProgress:
		return "Your trip is in progress"
	case RideStatusCompleted:
		return "Your trip has been completed"
	case RideStatusCancelled:
		return "Your ride has been cancelled"
	default:
		return ""
	}
}

func estimateArrivalTimestamp(minutes int) string {
	if minutes <= 0 {
		minutes = 1
	}
	return time.Now().UTC().Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339)
}
