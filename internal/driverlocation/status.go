package driverlocation

import (
	"context"
	"time"

	"ride-hail/internal/shared/messaging"
)

const (
	DriverStatusOffline   = "OFFLINE"
	DriverStatusAvailable = "AVAILABLE"
	DriverStatusEnRoute   = "EN_ROUTE"
	DriverStatusArrived   = "ARRIVED"
	DriverStatusBusy      = "BUSY"
)

// DriverStatusEvent публикуется в driver_topic (routing key driver.status.{driver_id}).
type DriverStatusEvent struct {
	DriverID  string    `json:"driver_id"`
	Status    string    `json:"status"`
	RideID    string    `json:"ride_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

func publishDriverStatus(
	ctx context.Context,
	publisher *messaging.Publisher,
	driverID, status, rideID string,
) error {
	event := DriverStatusEvent{
		DriverID:  driverID,
		Status:    status,
		RideID:    rideID,
		Timestamp: time.Now().UTC(),
	}
	return publisher.Publish("driver_topic", "driver.status."+driverID, event)
}
