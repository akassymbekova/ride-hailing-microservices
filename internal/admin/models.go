package admin

import "time"

type OverviewResponse struct {
	Timestamp          time.Time        `json:"timestamp"`
	Metrics            OverviewMetrics  `json:"metrics"`
	DriverDistribution map[string]int64 `json:"driver_distribution"`
	Hotspots           []Hotspot        `json:"hotspots"`
}

type OverviewMetrics struct {
	ActiveRides                int64   `json:"active_rides"`
	AvailableDrivers           int64   `json:"available_drivers"`
	BusyDrivers                int64   `json:"busy_drivers"`
	TotalRidesToday            int64   `json:"total_rides_today"`
	TotalRevenueToday          float64 `json:"total_revenue_today"`
	AverageWaitTimeMinutes     float64 `json:"average_wait_time_minutes"`
	AverageRideDurationMinutes float64 `json:"average_ride_duration_minutes"`
	CancellationRate           float64 `json:"cancellation_rate"`
}

type Hotspot struct {
	Location       string `json:"location"`
	ActiveRides    int64  `json:"active_rides"`
	WaitingDrivers int64  `json:"waiting_drivers"`
}

type ActiveRidesResponse struct {
	Rides      []ActiveRide `json:"rides"`
	TotalCount int64        `json:"total_count"`
	Page       int          `json:"page"`
	PageSize   int          `json:"page_size"`
}

type ActiveRide struct {
	RideID                string          `json:"ride_id"`
	RideNumber            string          `json:"ride_number"`
	Status                string          `json:"status"`
	PassengerID           string          `json:"passenger_id"`
	DriverID              *string         `json:"driver_id,omitempty"`
	PickupAddress         string          `json:"pickup_address"`
	DestinationAddress    string          `json:"destination_address"`
	StartedAt             *time.Time      `json:"started_at,omitempty"`
	EstimatedCompletion   *time.Time      `json:"estimated_completion,omitempty"`
	CurrentDriverLocation *DriverLocation `json:"current_driver_location,omitempty"`
	DistanceCompletedKM   float64         `json:"distance_completed_km"`
	DistanceRemainingKM   float64         `json:"distance_remaining_km"`
}

type DriverLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
