package ride

import "time"

const (
	RideTypeEconomy = "ECONOMY"
	RideTypePremium = "PREMIUM"
	RideTypeXL      = "XL"

	RideStatusRequested  = "REQUESTED"
	RideStatusMatched    = "MATCHED"
	RideStatusEnRoute    = "EN_ROUTE"
	RideStatusArrived    = "ARRIVED"
	RideStatusInProgress = "IN_PROGRESS"
	RideStatusCompleted  = "COMPLETED"
	RideStatusCancelled  = "CANCELLED"
)

type CreateRideRequest struct {
	PassengerID          string  `json:"passenger_id"`
	PickupLatitude       float64 `json:"pickup_latitude"`
	PickupLongitude      float64 `json:"pickup_longitude"`
	PickupAddress        string  `json:"pickup_address"`
	DestinationLatitude  float64 `json:"destination_latitude"`
	DestinationLongitude float64 `json:"destination_longitude"`
	DestinationAddress   string  `json:"destination_address"`
	RideType             string  `json:"ride_type"`
}

type CancelRideRequest struct {
	Reason string `json:"reason"`
}

type Coordinate struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Address   string  `json:"address,omitempty"`
}

type Ride struct {
	ID                    string     `json:"id"`
	RideNumber            string     `json:"ride_number"`
	PassengerID           string     `json:"passenger_id"`
	DriverID              *string    `json:"driver_id,omitempty"`
	Pickup                Coordinate `json:"pickup"`
	Destination           Coordinate `json:"destination"`
	RideType              string     `json:"ride_type"`
	Status                string     `json:"status"`
	EstimatedDistanceKM   float64    `json:"estimated_distance_km"`
	EstimatedDurationMins int        `json:"estimated_duration_minutes"`
	EstimatedFare         float64    `json:"estimated_fare"`
	FinalFare             *float64   `json:"final_fare,omitempty"`
	CancellationReason    *string    `json:"cancellation_reason,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	MatchedAt             *time.Time `json:"matched_at,omitempty"`
	ArrivedAt             *time.Time `json:"arrived_at,omitempty"`
	StartedAt             *time.Time `json:"started_at,omitempty"`
	CompletedAt           *time.Time `json:"completed_at,omitempty"`
	CancelledAt           *time.Time `json:"cancelled_at,omitempty"`
}

type CreateRideResponse struct {
	RideID                   string  `json:"ride_id"`
	RideNumber               string  `json:"ride_number"`
	Status                   string  `json:"status"`
	EstimatedFare            float64 `json:"estimated_fare"`
	EstimatedDurationMinutes int     `json:"estimated_duration_minutes"`
	EstimatedDistanceKM      float64 `json:"estimated_distance_km"`
}

type ErrorResponse struct {
	Error   string            `json:"error"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}
