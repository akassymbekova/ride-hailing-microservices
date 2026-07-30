package driverlocation

import (
	"encoding/json"
	"time"
)

// Location описывает географическую точку (широта и долгота)
type Location struct {
	Lat float64 `json:"latitude"`
	Lng float64 `json:"longitude"`
}

// UnmarshalJSON принимает оба варианта именования координат: {"lat","lng"}
// (так публикует Ride Service в ride.request, см. MQPoint в internal/ride/events.go)
// и {"latitude","longitude"} (наш собственный формат в driver_offer/driver_response).
// Без этого координаты из ride.request тихо приходили нулевыми, и подбор
// водителей всегда искал от точки (0,0).
func (l *Location) UnmarshalJSON(data []byte) error {
	var raw struct {
		Lat       float64 `json:"lat"`
		Lng       float64 `json:"lng"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if raw.Lat != 0 || raw.Lng != 0 {
		l.Lat, l.Lng = raw.Lat, raw.Lng
	} else {
		l.Lat, l.Lng = raw.Latitude, raw.Longitude
	}
	return nil
}

// RideRequest описывает событие запроса поездки, поступающее из RabbitMQ
type RideRequest struct {
	RideID         string   `json:"ride_id"`
	RideNumber     string   `json:"ride_number"`
	RideType       string   `json:"ride_type"` // 'eco', 'comfort', 'business'
	PickupLocation Location `json:"pickup_location"`
	DestLocation   Location `json:"destination_location"`
	EstimatedFare  float64  `json:"estimated_fare"`
	CorrelationID  string   `json:"correlation_id"`
}

// RideOffer описывает структуру предложения поездки, которая отправляется водителю по WebSocket
type RideOffer struct {
	Type                         string   `json:"type"` // "ride_offer"
	OfferID                      string   `json:"offer_id"`
	RideID                       string   `json:"ride_id"`
	RideNumber                   string   `json:"ride_number"`
	PickupLocation               Location `json:"pickup_location"`
	DestinationLocation          Location `json:"destination_location"`
	EstimatedFare                float64  `json:"estimated_fare"`
	DriverEarnings               float64  `json:"driver_earnings"`
	DistanceToPickupKm           float64  `json:"distance_to_pickup_km"`
	EstimatedRideDurationMinutes int      `json:"estimated_ride_duration_minutes"`
	ExpiresAt                    string   `json:"expires_at"` // RFC3339
}

// RideResponse описывает ответ водителя на предложение поездки через WebSocket
type RideResponse struct {
	Type     string `json:"type"` // "ride_response"
	OfferID  string `json:"offer_id"`
	RideID   string `json:"ride_id"`
	Accepted bool   `json:"accepted"`
}

// NearbyDriver описывает структуру водителя, найденного поблизости через PostGIS
type NearbyDriver struct {
	DriverID   string  `json:"driver_id"`
	Email      string  `json:"email"`
	Rating     float64 `json:"rating"`
	Lat        float64 `json:"latitude"`
	Lng        float64 `json:"longitude"`
	DistanceKm float64 `json:"distance_km"`
}

// DriverMatchResponse отправляется в RabbitMQ в случае успешного подбора водителя
type DriverMatchResponse struct {
	RideID                  string      `json:"ride_id"`
	DriverID                string      `json:"driver_id"`
	Accepted                bool        `json:"accepted"`
	EstimatedArrivalMinutes int         `json:"estimated_arrival_minutes"`
	DriverLocation          Location    `json:"driver_location"`
	DriverInfo              *DriverInfo `json:"driver_info,omitempty"`
	CorrelationID           string      `json:"correlation_id"`
}

// DriverInfo описывает водителя для пассажира после матча.
type DriverInfo struct {
	Name    string      `json:"name"`
	Rating  float64     `json:"rating"`
	Vehicle VehicleInfo `json:"vehicle"`
}

// VehicleInfo описывает автомобиль водителя.
type VehicleInfo struct {
	Make  string `json:"make"`
	Model string `json:"model"`
	Color string `json:"color"`
	Plate string `json:"plate"`
}

// ActiveRideTarget — активная поездка водителя и точка, к которой считается
// расстояние: pickup, пока водитель едет забирать пассажира, либо dropoff,
// когда поездка уже в процессе (IN_PROGRESS).
type ActiveRideTarget struct {
	RideID    string
	Status    string
	TargetLat float64
	TargetLng float64
}

// DriverLocationEvent публикуется в location_fanout при каждом обновлении
// локации водителя (см. POST /drivers/{id}/location).
type DriverLocationEvent struct {
	DriverID       string    `json:"driver_id"`
	RideID         *string   `json:"ride_id,omitempty"`
	Location       Location  `json:"location"`
	SpeedKmh       *float64  `json:"speed_kmh,omitempty"`
	HeadingDegrees *float64  `json:"heading_degrees,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

// RideStatusMessage приходит из ride_topic (routing key ride.status.*).
type RideStatusMessage struct {
	RideID        string `json:"ride_id"`
	Status        string `json:"status"`
	Timestamp     string `json:"timestamp"`
	DriverID      string `json:"driver_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
}

// RideDetailsLocation — точка подачи в событии ride_details.
type RideDetailsLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Address   string  `json:"address"`
	Notes     string  `json:"notes,omitempty"`
}

// RideDetails отправляется водителю по WebSocket после подтверждения матча.
type RideDetails struct {
	Type           string              `json:"type"`
	RideID         string              `json:"ride_id"`
	PassengerName  string              `json:"passenger_name"`
	PassengerPhone string              `json:"passenger_phone"`
	PickupLocation RideDetailsLocation `json:"pickup_location"`
}

// RideDetailsData — данные из БД для формирования ride_details.
type RideDetailsData struct {
	RideID         string
	PassengerName  string
	PassengerPhone string
	PickupLat      float64
	PickupLng      float64
	PickupAddress  string
	PickupNotes    string
}
