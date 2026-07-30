package ride

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrRideNotFound       = errors.New("ride not found")
	ErrRideNotCancellable = errors.New("ride cannot be cancelled in current status")
	ErrRideAlreadyMatched = errors.New("ride is already matched or processed")
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) CreateRide(ctx context.Context, ride *Ride) error {
	if ride == nil {
		return errors.New("ride is required")
	}

	ride.ID = newRideID()
	ride.RideNumber = newRideNumber(time.Now().UTC())
	ride.CreatedAt = time.Now().UTC()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	pickupID, err := insertCoordinate(ctx, tx, ride.PassengerID, ride.Pickup)
	if err != nil {
		return err
	}

	destinationID, err := insertCoordinate(ctx, tx, ride.PassengerID, ride.Destination)
	if err != nil {
		return err
	}

	_, err = tx.Exec(
		ctx,
		`INSERT INTO rides (
			id, ride_number, passenger_id, vehicle_type, status,
			pickup_coordinate_id, destination_coordinate_id,
			pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
			estimated_fare, requested_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		ride.ID,
		ride.RideNumber,
		ride.PassengerID,
		ride.RideType,
		ride.Status,
		pickupID,
		destinationID,
		ride.Pickup.Latitude,
		ride.Pickup.Longitude,
		ride.Destination.Latitude,
		ride.Destination.Longitude,
		ride.EstimatedFare,
		ride.CreatedAt,
		ride.CreatedAt,
		ride.CreatedAt,
	)
	if err != nil {
		return err
	}

	if err := insertRideEvent(ctx, tx, ride.ID, "RIDE_REQUESTED", map[string]any{
		"status":          ride.Status,
		"estimated_fare":  ride.EstimatedFare,
		"passenger_id":    ride.PassengerID,
		"ride_number":     ride.RideNumber,
		"vehicle_type":    ride.RideType,
		"pickup_location": ride.Pickup,
		"destination":     ride.Destination,
	}); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *PostgresRepository) GetRide(ctx context.Context, rideID string) (*Ride, error) {
	const query = `
		SELECT
			r.id, r.ride_number, r.passenger_id, r.driver_id, r.status,
			r.vehicle_type, r.estimated_fare, r.final_fare, r.cancellation_reason,
			r.created_at, r.matched_at, r.arrived_at, r.started_at, r.completed_at, r.cancelled_at,
			pc.latitude, pc.longitude, pc.address,
			dc.latitude, dc.longitude, dc.address
		FROM rides r
		JOIN coordinates pc ON pc.id = r.pickup_coordinate_id
		JOIN coordinates dc ON dc.id = r.destination_coordinate_id
		WHERE r.id = $1
	`

	var ride Ride
	var driverID *string
	var cancellationReason *string
	var pickupAddress, destinationAddress string

	err := r.pool.QueryRow(ctx, query, rideID).Scan(
		&ride.ID,
		&ride.RideNumber,
		&ride.PassengerID,
		&driverID,
		&ride.Status,
		&ride.RideType,
		&ride.EstimatedFare,
		&ride.FinalFare,
		&cancellationReason,
		&ride.CreatedAt,
		&ride.MatchedAt,
		&ride.ArrivedAt,
		&ride.StartedAt,
		&ride.CompletedAt,
		&ride.CancelledAt,
		&ride.Pickup.Latitude,
		&ride.Pickup.Longitude,
		&pickupAddress,
		&ride.Destination.Latitude,
		&ride.Destination.Longitude,
		&destinationAddress,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRideNotFound
	}
	if err != nil {
		return nil, err
	}

	ride.DriverID = driverID
	ride.CancellationReason = cancellationReason
	ride.Pickup.Address = pickupAddress
	ride.Destination.Address = destinationAddress

	return &ride, nil
}

func (r *PostgresRepository) CancelRide(
	ctx context.Context,
	rideID, reason string,
	refundAmount float64,
) (*Ride, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM rides WHERE id = $1 FOR UPDATE`, rideID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRideNotFound
	}
	if err != nil {
		return nil, err
	}

	if status != RideStatusRequested && status != RideStatusMatched {
		return nil, ErrRideNotCancellable
	}

	now := time.Now().UTC()
	_, err = tx.Exec(
		ctx,
		`UPDATE rides
		 SET status = $1, cancellation_reason = $2, cancelled_at = $3, updated_at = $3
		 WHERE id = $4`,
		RideStatusCancelled,
		reason,
		now,
		rideID,
	)
	if err != nil {
		return nil, err
	}

	if err := insertRideEvent(ctx, tx, rideID, "RIDE_CANCELLED", map[string]any{
		"old_status":    status,
		"new_status":    RideStatusCancelled,
		"reason":        reason,
		"refund_amount": refundAmount,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return r.GetRide(ctx, rideID)
}

func (r *PostgresRepository) UpdateRideMatched(
	ctx context.Context,
	rideID string,
	driverID string,
) (*Ride, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM rides WHERE id = $1 FOR UPDATE`, rideID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRideNotFound
	}
	if err != nil {
		return nil, err
	}

	if status != RideStatusRequested {
		return nil, ErrRideAlreadyMatched
	}

	now := time.Now().UTC()
	_, err = tx.Exec(
		ctx,
		`UPDATE rides
		 SET status = $1, driver_id = $2, matched_at = $3, updated_at = $3
		 WHERE id = $4`,
		RideStatusMatched,
		driverID,
		now,
		rideID,
	)
	if err != nil {
		return nil, err
	}

	if err := insertRideEvent(ctx, tx, rideID, "DRIVER_MATCHED", map[string]any{
		"old_status": status,
		"new_status": RideStatusMatched,
		"driver_id":  driverID,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return r.GetRide(ctx, rideID)
}

func (r *PostgresRepository) UpdateRideStatus(
	ctx context.Context,
	rideID string,
	newStatus string,
	eventType string,
	eventData map[string]any,
	finalFare *float64,
) (*Ride, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var oldStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM rides WHERE id = $1 FOR UPDATE`, rideID).Scan(&oldStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRideNotFound
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	query := `UPDATE rides SET status = $1, updated_at = $2`
	args := []any{newStatus, now}
	argIndex := 3

	switch newStatus {
	case RideStatusMatched:
		query += fmt.Sprintf(", matched_at = $%d", argIndex)
		args = append(args, now)
		argIndex++
	case RideStatusArrived:
		query += fmt.Sprintf(", arrived_at = $%d", argIndex)
		args = append(args, now)
		argIndex++
	case RideStatusInProgress:
		query += fmt.Sprintf(", started_at = $%d", argIndex)
		args = append(args, now)
		argIndex++
	case RideStatusCompleted:
		query += fmt.Sprintf(", completed_at = $%d", argIndex)
		args = append(args, now)
		argIndex++
		if finalFare != nil {
			query += fmt.Sprintf(", final_fare = $%d", argIndex)
			args = append(args, *finalFare)
			argIndex++
			if eventData != nil {
				eventData["final_fare"] = *finalFare
			}
		}
	case RideStatusCancelled:
		query += fmt.Sprintf(", cancelled_at = $%d", argIndex)
		args = append(args, now)
		argIndex++
	}

	query += fmt.Sprintf(" WHERE id = $%d", argIndex)
	args = append(args, rideID)

	_, err = tx.Exec(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	if eventData == nil {
		eventData = map[string]any{}
	}
	eventData["old_status"] = oldStatus
	eventData["new_status"] = newStatus

	if err := insertRideEvent(ctx, tx, rideID, eventType, eventData); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return r.GetRide(ctx, rideID)
}

func insertCoordinate(ctx context.Context, tx pgx.Tx, passengerID string, coord Coordinate) (string, error) {
	const query = `
		INSERT INTO coordinates (
			entity_id, entity_type, latitude, longitude, address, is_current, created_at, updated_at
		) VALUES ($1, 'passenger', $2, $3, $4, false, now(), now())
		RETURNING id
	`

	var id string
	err := tx.QueryRow(
		ctx,
		query,
		passengerID,
		coord.Latitude,
		coord.Longitude,
		coord.Address,
	).Scan(&id)
	return id, err
}

func insertRideEvent(
	ctx context.Context,
	tx pgx.Tx,
	rideID string,
	eventType string,
	eventData map[string]any,
) error {
	payload, err := json.Marshal(eventData)
	if err != nil {
		return err
	}

	_, err = tx.Exec(
		ctx,
		`INSERT INTO ride_events (ride_id, event_type, event_data) VALUES ($1, $2, $3)`,
		rideID,
		eventType,
		payload,
	)
	return err
}

func newRideID() string {
	return newUUID()
}

func newRideNumber(now time.Time) string {
	return fmt.Sprintf("RIDE_%s_%s", now.Format("20060102"), randomHex(3))
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = readRandom(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = readRandom(b)
	return fmt.Sprintf("%x", b)
}
