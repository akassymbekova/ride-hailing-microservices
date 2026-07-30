package admin

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const earthRadiusKM = 6371.0

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) GetOverview(ctx context.Context) (OverviewResponse, error) {
	var resp OverviewResponse

	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM rides WHERE status NOT IN ('COMPLETED', 'CANCELLED')
	`).Scan(&resp.Metrics.ActiveRides)
	if err != nil {
		return resp, err
	}

	err = r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM drivers WHERE status = 'AVAILABLE'
	`).Scan(&resp.Metrics.AvailableDrivers)
	if err != nil {
		return resp, err
	}

	err = r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM drivers WHERE status IN ('BUSY', 'EN_ROUTE')
	`).Scan(&resp.Metrics.BusyDrivers)
	if err != nil {
		return resp, err
	}

	err = r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM rides WHERE created_at >= CURRENT_DATE
	`).Scan(&resp.Metrics.TotalRidesToday)
	if err != nil {
		return resp, err
	}

	err = r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(COALESCE(final_fare, estimated_fare)), 0)
		FROM rides
		WHERE status = 'COMPLETED' AND completed_at >= CURRENT_DATE
	`).Scan(&resp.Metrics.TotalRevenueToday)
	if err != nil {
		return resp, err
	}

	err = r.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (matched_at - requested_at)) / 60.0), 0)
		FROM rides
		WHERE matched_at IS NOT NULL AND requested_at >= CURRENT_DATE
	`).Scan(&resp.Metrics.AverageWaitTimeMinutes)
	if err != nil {
		return resp, err
	}

	err = r.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (completed_at - started_at)) / 60.0), 0)
		FROM rides
		WHERE completed_at IS NOT NULL
		  AND started_at IS NOT NULL
		  AND completed_at >= CURRENT_DATE
	`).Scan(&resp.Metrics.AverageRideDurationMinutes)
	if err != nil {
		return resp, err
	}

	var totalToday, cancelledToday int64
	err = r.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'CANCELLED')
		FROM rides
		WHERE created_at >= CURRENT_DATE
	`).Scan(&totalToday, &cancelledToday)
	if err != nil {
		return resp, err
	}
	if totalToday > 0 {
		resp.Metrics.CancellationRate = float64(cancelledToday) / float64(totalToday)
	}

	resp.DriverDistribution = make(map[string]int64)
	rows, err := r.pool.Query(ctx, `
		SELECT vehicle_type, COUNT(*)
		FROM drivers
		WHERE status != 'OFFLINE'
		GROUP BY vehicle_type
	`)
	if err != nil {
		return resp, err
	}
	defer rows.Close()

	for rows.Next() {
		var vehicleType string
		var count int64
		if err := rows.Scan(&vehicleType, &count); err != nil {
			return resp, err
		}
		resp.DriverDistribution[vehicleType] = count
	}
	if err := rows.Err(); err != nil {
		return resp, err
	}

	hotspotRows, err := r.pool.Query(ctx, `
		WITH hotspot AS (
			SELECT pc.address AS location,
			       AVG(pc.longitude) AS lng,
			       AVG(pc.latitude) AS lat,
			       COUNT(*) AS active_rides
			FROM rides r
			JOIN coordinates pc ON pc.id = r.pickup_coordinate_id
			WHERE r.status NOT IN ('COMPLETED', 'CANCELLED')
			GROUP BY pc.address
			ORDER BY active_rides DESC
			LIMIT 5
		)
		SELECT h.location, h.active_rides,
		       (
		         SELECT COUNT(*)
		         FROM drivers d
		         JOIN coordinates c
		           ON c.entity_id = d.id
		          AND c.entity_type = 'driver'
		          AND c.is_current = true
		         WHERE d.status = 'AVAILABLE'
		           AND ST_DWithin(
		                 ST_MakePoint(c.longitude, c.latitude)::geography,
		                 ST_MakePoint(h.lng, h.lat)::geography,
		                 5000
		               )
		       ) AS waiting_drivers
		FROM hotspot h
	`)
	if err != nil {
		return resp, err
	}
	defer hotspotRows.Close()

	for hotspotRows.Next() {
		var h Hotspot
		if err := hotspotRows.Scan(&h.Location, &h.ActiveRides, &h.WaitingDrivers); err != nil {
			return resp, err
		}
		resp.Hotspots = append(resp.Hotspots, h)
	}
	if resp.Hotspots == nil {
		resp.Hotspots = []Hotspot{}
	}

	return resp, hotspotRows.Err()
}

func (r *Repository) GetActiveRides(ctx context.Context, page, pageSize int) (ActiveRidesResponse, error) {
	offset := (page - 1) * pageSize

	var resp ActiveRidesResponse
	resp.Page = page
	resp.PageSize = pageSize

	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM rides
		WHERE status NOT IN ('COMPLETED', 'CANCELLED')
	`).Scan(&resp.TotalCount)
	if err != nil {
		return resp, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT
			r.id,
			r.ride_number,
			r.status,
			r.passenger_id,
			r.driver_id,
			pc.address,
			dc.address,
			r.started_at,
			r.pickup_lat,
			r.pickup_lng,
			r.dropoff_lat,
			r.dropoff_lng,
			c.latitude,
			c.longitude
		FROM rides r
		LEFT JOIN coordinates pc ON pc.id = r.pickup_coordinate_id
		LEFT JOIN coordinates dc ON dc.id = r.destination_coordinate_id
		LEFT JOIN coordinates c
		  ON c.entity_id = r.driver_id
		 AND c.entity_type = 'driver'
		 AND c.is_current = true
		WHERE r.status NOT IN ('COMPLETED', 'CANCELLED')
		ORDER BY r.created_at DESC
		LIMIT $1 OFFSET $2
	`, pageSize, offset)
	if err != nil {
		return resp, err
	}
	defer rows.Close()

	resp.Rides = make([]ActiveRide, 0)
	for rows.Next() {
		var (
			ride          ActiveRide
			rideNumber    *string
			pickupAddress *string
			destAddress   *string
			driverLat     *float64
			driverLng     *float64
			pickupLat     float64
			pickupLng     float64
			dropoffLat    float64
			dropoffLng    float64
		)

		if err := rows.Scan(
			&ride.RideID,
			&rideNumber,
			&ride.Status,
			&ride.PassengerID,
			&ride.DriverID,
			&pickupAddress,
			&destAddress,
			&ride.StartedAt,
			&pickupLat,
			&pickupLng,
			&dropoffLat,
			&dropoffLng,
			&driverLat,
			&driverLng,
		); err != nil {
			return resp, err
		}

		if rideNumber != nil {
			ride.RideNumber = *rideNumber
		}
		if pickupAddress != nil {
			ride.PickupAddress = *pickupAddress
		}
		if destAddress != nil {
			ride.DestinationAddress = *destAddress
		}

		totalTripKM := distanceKM(pickupLat, pickupLng, dropoffLat, dropoffLng)
		if ride.StartedAt != nil {
			duration := estimateDuration(totalTripKM)
			estimated := ride.StartedAt.Add(duration)
			ride.EstimatedCompletion = &estimated
		}

		if driverLat != nil && driverLng != nil {
			ride.CurrentDriverLocation = &DriverLocation{
				Latitude:  *driverLat,
				Longitude: *driverLng,
			}

			if ride.Status == "IN_PROGRESS" {
				ride.DistanceCompletedKM = roundKM(distanceKM(pickupLat, pickupLng, *driverLat, *driverLng))
				ride.DistanceRemainingKM = roundKM(distanceKM(*driverLat, *driverLng, dropoffLat, dropoffLng))
			}
		}

		resp.Rides = append(resp.Rides, ride)
	}

	return resp, rows.Err()
}

func estimateDuration(distanceKM float64) time.Duration {
	if distanceKM <= 0 {
		return time.Minute
	}
	hours := distanceKM / 30.0
	if hours < 1.0/60.0 {
		hours = 1.0 / 60.0
	}
	return time.Duration(hours * float64(time.Hour))
}

func distanceKM(lat1, lng1, lat2, lng2 float64) float64 {
	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKM * c
}

func roundKM(v float64) float64 {
	return math.Round(v*10) / 10
}
