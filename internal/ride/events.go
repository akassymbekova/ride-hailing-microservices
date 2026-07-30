package ride

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ride-hail/internal/shared/messaging"
)

const (
	defaultOfferTimeoutSeconds = 30
	defaultMaxDistanceKM       = 5.0
)

type MQPoint struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Address string  `json:"address,omitempty"`
}

func (p *MQPoint) UnmarshalJSON(data []byte) error {
	var raw struct {
		Lat       float64 `json:"lat"`
		Lng       float64 `json:"lng"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Address   string  `json:"address"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch {
	case raw.Lat != 0 || raw.Lng != 0:
		p.Lat = raw.Lat
		p.Lng = raw.Lng
	default:
		p.Lat = raw.Latitude
		p.Lng = raw.Longitude
	}
	p.Address = raw.Address
	return nil
}

type RideRequestMessage struct {
	RideID         string  `json:"ride_id"`
	RideNumber     string  `json:"ride_number"`
	PickupLocation MQPoint `json:"pickup_location"`
	Destination    MQPoint `json:"destination_location"`
	RideType       string  `json:"ride_type"`
	EstimatedFare  float64 `json:"estimated_fare"`
	MaxDistanceKM  float64 `json:"max_distance_km"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	CorrelationID  string  `json:"correlation_id"`
}

type RideStatusMessage struct {
	RideID        string `json:"ride_id"`
	Status        string `json:"status"`
	Timestamp     string `json:"timestamp"`
	DriverID      string `json:"driver_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
}

type DriverResponseMessage struct {
	RideID                  string      `json:"ride_id"`
	DriverID                string      `json:"driver_id"`
	Accepted                bool        `json:"accepted"`
	EstimatedArrivalMinutes int         `json:"estimated_arrival_minutes,omitempty"`
	DriverLocation          MQPoint     `json:"driver_location"`
	DriverInfo              *DriverInfo `json:"driver_info,omitempty"`
	CorrelationID           string      `json:"correlation_id,omitempty"`
	EstimatedArrival        string      `json:"estimated_arrival,omitempty"`
}

type DriverInfo struct {
	Name    string      `json:"name"`
	Rating  float64     `json:"rating"`
	Vehicle VehicleInfo `json:"vehicle"`
}

type VehicleInfo struct {
	Make  string `json:"make"`
	Model string `json:"model"`
	Color string `json:"color"`
	Plate string `json:"plate"`
}

type DriverStatusMessage struct {
	DriverID  string `json:"driver_id"`
	Status    string `json:"status"`
	RideID    string `json:"ride_id,omitempty"`
	Timestamp string `json:"timestamp"`
}

type LocationUpdateMessage struct {
	DriverID       string  `json:"driver_id"`
	RideID         string  `json:"ride_id"`
	Location       MQPoint `json:"location"`
	SpeedKMH       float64 `json:"speed_kmh,omitempty"`
	HeadingDegrees float64 `json:"heading_degrees,omitempty"`
	Timestamp      string  `json:"timestamp"`
}

type EventPublisher struct {
	publisher *messaging.Publisher
}

func NewEventPublisher(publisher *messaging.Publisher) *EventPublisher {
	return &EventPublisher{publisher: publisher}
}

func (p *EventPublisher) PublishRideRequest(ride *Ride, correlationID string) error {
	if p == nil || p.publisher == nil || ride == nil {
		return fmt.Errorf("publisher or ride is not configured")
	}

	message := RideRequestMessage{
		RideID:     ride.ID,
		RideNumber: ride.RideNumber,
		PickupLocation: MQPoint{
			Lat:     ride.Pickup.Latitude,
			Lng:     ride.Pickup.Longitude,
			Address: ride.Pickup.Address,
		},
		Destination: MQPoint{
			Lat:     ride.Destination.Latitude,
			Lng:     ride.Destination.Longitude,
			Address: ride.Destination.Address,
		},
		RideType:       ride.RideType,
		EstimatedFare:  ride.EstimatedFare,
		MaxDistanceKM:  defaultMaxDistanceKM,
		TimeoutSeconds: defaultOfferTimeoutSeconds,
		CorrelationID:  correlationID,
	}

	routingKey := "ride.request." + strings.ToLower(ride.RideType)
	return p.publisher.Publish("ride_topic", routingKey, message)
}

func (p *EventPublisher) PublishRideStatus(ride *Ride, correlationID string) error {
	if p == nil || p.publisher == nil || ride == nil {
		return fmt.Errorf("publisher or ride is not configured")
	}

	message := RideStatusMessage{
		RideID:        ride.ID,
		Status:        ride.Status,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		CorrelationID: correlationID,
	}
	if ride.DriverID != nil {
		message.DriverID = *ride.DriverID
	}

	routingKey := "ride.status." + strings.ToLower(ride.Status)
	return p.publisher.Publish("ride_topic", routingKey, message)
}

func newCorrelationID() string {
	return "req_" + randomHex(6)
}
