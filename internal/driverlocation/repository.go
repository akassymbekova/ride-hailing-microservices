package driverlocation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository инкапсулирует все SQL-запросы, связанные с поиском
// и обновлением статуса водителей.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// FindNearbyDrivers ищет доступных водителей нужного типа авто
// в радиусе radiusMeters от точки (lat, lng), отсортированных
// по расстоянию, затем по рейтингу. Это тот самый PostGIS-запрос
// из спецификации, обёрнутый в Go-функцию.
//
// ВАЖНО: ST_MakePoint принимает (долгота, широта) — то есть (lng, lat),
// а не (lat, lng), как многие интуитивно ожидают.
func (r *Repository) FindNearbyDrivers(ctx context.Context, lat, lng float64, vehicleType string, radiusMeters float64, limit int) ([]NearbyDriver, error) {
	const query = `
		SELECT d.id, u.email, d.rating, c.latitude, c.longitude,
		       ST_Distance(
		         ST_MakePoint(c.longitude, c.latitude)::geography,
		         ST_MakePoint($1, $2)::geography
		       ) / 1000 as distance_km
		FROM drivers d
		JOIN users u ON d.id = u.id
		JOIN coordinates c ON c.entity_id = d.id
		  AND c.entity_type = 'driver'
		  AND c.is_current = true
		WHERE d.status = 'AVAILABLE'
		  AND d.vehicle_type = $3
		  AND ST_DWithin(
		        ST_MakePoint(c.longitude, c.latitude)::geography,
		        ST_MakePoint($1, $2)::geography,
		        $4
		      )
		ORDER BY distance_km, d.rating DESC
		LIMIT $5;
	`

	// $1=lng, $2=lat — порядок совпадает с ST_MakePoint(lng, lat) внутри запроса.
	rows, err := r.pool.Query(ctx, query, lng, lat, vehicleType, radiusMeters, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var drivers []NearbyDriver
	for rows.Next() {
		var d NearbyDriver
		if err := rows.Scan(&d.DriverID, &d.Email, &d.Rating, &d.Lat, &d.Lng, &d.DistanceKm); err != nil {
			return nil, err
		}
		drivers = append(drivers, d)
	}
	return drivers, rows.Err()
}

// SetDriverStatus обновляет статус водителя (AVAILABLE / BUSY / EN_ROUTE / OFFLINE).
func (r *Repository) SetDriverStatus(ctx context.Context, driverID, status string) error {
	_, err := r.pool.Exec(ctx, `UPDATE drivers SET status = $1, updated_at = now() WHERE id = $2`, status, driverID)
	return err
}

// UpdateDriverLocation записывает новую текущую позицию водителя и архивирует
// её в location_history. Работает в транзакции: старая "текущая" координата
// сбрасывается (is_current = false), новая вставляется как текущая, и та же
// точка попадает в location_history для истории/разбора спорных ситуаций.
func (r *Repository) UpdateDriverLocation(
	ctx context.Context,
	driverID string,
	lat, lng float64,
	accuracyMeters, speedKmh, headingDegrees *float64,
	rideID *string,
) (coordinateID int64, updatedAt time.Time, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err = tx.Exec(ctx, `
		UPDATE coordinates SET is_current = false
		WHERE entity_id = $1 AND entity_type = 'driver' AND is_current = true
	`, driverID); err != nil {
		return 0, time.Time{}, err
	}

	if err = tx.QueryRow(ctx, `
		INSERT INTO coordinates (entity_id, entity_type, latitude, longitude, is_current, updated_at)
		VALUES ($1, 'driver', $2, $3, true, now())
		RETURNING id, updated_at
	`, driverID, lat, lng).Scan(&coordinateID, &updatedAt); err != nil {
		return 0, time.Time{}, err
	}

	if _, err = tx.Exec(ctx, `
		INSERT INTO location_history
			(coordinate_id, driver_id, latitude, longitude, accuracy_meters, speed_kmh, heading_degrees, recorded_at, ride_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), $8)
	`, coordinateID, driverID, lat, lng, accuracyMeters, speedKmh, headingDegrees, rideID); err != nil {
		return 0, time.Time{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return 0, time.Time{}, err
	}
	return coordinateID, updatedAt, nil
}

// StartDriverSession открывает новую сессию водителя (при выходе на линию)
// и возвращает её ID.
func (r *Repository) StartDriverSession(ctx context.Context, driverID string) (int64, error) {
	var sessionID int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO driver_sessions (driver_id, started_at)
		VALUES ($1, now())
		RETURNING id
	`, driverID).Scan(&sessionID)
	return sessionID, err
}

// SessionSummary — сводка по завершённой сессии водителя (см. HandleOffline).
type SessionSummary struct {
	DurationHours  float64
	RidesCompleted int
	Earnings       float64
}

// EndDriverSession закрывает последнюю открытую сессию водителя (ended_at IS NULL)
// и возвращает сводку по ней. Если открытой сессии нет (например, /offline
// вызвали без предварительного /online), возвращает нулевую сводку.
func (r *Repository) EndDriverSession(ctx context.Context, driverID string) (SessionSummary, error) {
	var (
		startedAt     time.Time
		totalRides    int
		totalEarnings float64
	)

	err := r.pool.QueryRow(ctx, `
		UPDATE driver_sessions
		SET ended_at = now()
		WHERE id = (
			SELECT id FROM driver_sessions
			WHERE driver_id = $1 AND ended_at IS NULL
			ORDER BY started_at DESC
			LIMIT 1
		)
		RETURNING started_at, total_rides, total_earnings
	`, driverID).Scan(&startedAt, &totalRides, &totalEarnings)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionSummary{}, nil
	}
	if err != nil {
		return SessionSummary{}, err
	}

	return SessionSummary{
		DurationHours:  time.Since(startedAt).Hours(),
		RidesCompleted: totalRides,
		Earnings:       totalEarnings,
	}, nil
}

// RecordCompletedRide прибавляет поездку и заработок водителя к его текущей
// открытой сессии (вызывается из HandleComplete).
func (r *Repository) RecordCompletedRide(ctx context.Context, driverID string, earnings float64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE driver_sessions
		SET total_rides = total_rides + 1, total_earnings = total_earnings + $2
		WHERE id = (
			SELECT id FROM driver_sessions
			WHERE driver_id = $1 AND ended_at IS NULL
			ORDER BY started_at DESC
			LIMIT 1
		)
	`, driverID, earnings)
	return err
}

// GetRideFare возвращает итоговую стоимость поездки (final_fare, если уже
// проставлен Ride Service'ом, иначе estimated_fare) — нужна для расчёта
// заработка водителя при завершении поездки.
func (r *Repository) GetRideFare(ctx context.Context, rideID string) (float64, error) {
	var fare float64
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(final_fare, estimated_fare, 0) FROM rides WHERE id = $1
	`, rideID).Scan(&fare)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return fare, err
}

// GetAssignedRideStatus возвращает статус поездки, назначенной водителю.
func (r *Repository) GetAssignedRideStatus(ctx context.Context, driverID, rideID string) (string, error) {
	var status string
	err := r.pool.QueryRow(ctx, `
		SELECT status FROM rides WHERE id = $1 AND driver_id = $2
	`, rideID, driverID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	return status, err
}

// GetActiveRideTarget возвращает активную поездку водителя (если есть) и точку,
// к которой он сейчас едет: pickup, пока не подобрал пассажира, либо dropoff,
// если поездка уже IN_PROGRESS. Возвращает (nil, nil), если активной поездки нет.
func (r *Repository) GetActiveRideTarget(ctx context.Context, driverID string) (*ActiveRideTarget, error) {
	const query = `
		SELECT id, status, pickup_lat, pickup_lng, dropoff_lat, dropoff_lng
		FROM rides
		WHERE driver_id = $1 AND status IN ('MATCHED', 'EN_ROUTE', 'ARRIVED', 'IN_PROGRESS')
		ORDER BY requested_at DESC
		LIMIT 1
	`

	var (
		rideID, status                         string
		pickupLat, pickupLng, dropLat, dropLng float64
	)
	err := r.pool.QueryRow(ctx, query, driverID).Scan(&rideID, &status, &pickupLat, &pickupLng, &dropLat, &dropLng)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	target := &ActiveRideTarget{RideID: rideID, Status: status}
	if status == "IN_PROGRESS" {
		target.TargetLat, target.TargetLng = dropLat, dropLng
	} else {
		target.TargetLat, target.TargetLng = pickupLat, pickupLng
	}
	return target, nil
}

// GetRideDetailsForDriver загружает данные поездки для WebSocket-события ride_details.
func (r *Repository) GetRideDetailsForDriver(ctx context.Context, rideID string) (RideDetailsData, error) {
	const query = `
		SELECT
			r.id,
			u.email,
			COALESCE(u.attrs, '{}'::jsonb),
			pc.latitude,
			pc.longitude,
			pc.address
		FROM rides r
		JOIN users u ON u.id = r.passenger_id
		LEFT JOIN coordinates pc ON pc.id = r.pickup_coordinate_id
		WHERE r.id = $1
	`

	var (
		data      RideDetailsData
		email     string
		attrsJSON []byte
		address   *string
	)

	err := r.pool.QueryRow(ctx, query, rideID).Scan(
		&data.RideID,
		&email,
		&attrsJSON,
		&data.PickupLat,
		&data.PickupLng,
		&address,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RideDetailsData{}, err
	}
	if err != nil {
		return RideDetailsData{}, err
	}

	if address != nil {
		data.PickupAddress = *address
	}

	name, phone, notes := passengerContactFromAttrs(attrsJSON, email)
	data.PassengerName = name
	data.PassengerPhone = phone
	data.PickupNotes = notes

	return data, nil
}

func passengerContactFromAttrs(attrsJSON []byte, email string) (name, phone, notes string) {
	var attrs struct {
		Name        string `json:"name"`
		Phone       string `json:"phone"`
		PickupNotes string `json:"pickup_notes"`
	}
	_ = json.Unmarshal(attrsJSON, &attrs)

	name = strings.TrimSpace(attrs.Name)
	phone = strings.TrimSpace(attrs.Phone)
	notes = strings.TrimSpace(attrs.PickupNotes)

	if name == "" && email != "" {
		if at := strings.Index(email, "@"); at > 0 {
			name = email[:at]
		} else {
			name = email
		}
	}
	if name == "" {
		name = "Passenger"
	}
	if phone == "" {
		phone = "+7-XXX-XXX-XX-XX"
	}

	return name, phone, notes
}

// GetDriverInfo загружает профиль водителя для driver_info в ответе матчинга.
func (r *Repository) GetDriverInfo(ctx context.Context, driverID string) (DriverInfo, error) {
	const query = `
		SELECT u.email, COALESCE(u.attrs, '{}'::jsonb), d.rating, COALESCE(d.vehicle_attrs, '{}'::jsonb)
		FROM drivers d
		JOIN users u ON u.id = d.id
		WHERE d.id = $1
	`

	var (
		email         string
		userAttrsJSON []byte
		rating        float64
		vehicleJSON   []byte
	)

	err := r.pool.QueryRow(ctx, query, driverID).Scan(&email, &userAttrsJSON, &rating, &vehicleJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return DriverInfo{}, err
	}
	if err != nil {
		return DriverInfo{}, err
	}

	return buildDriverInfo(email, userAttrsJSON, rating, vehicleJSON), nil
}

func buildDriverInfo(email string, userAttrsJSON []byte, rating float64, vehicleJSON []byte) DriverInfo {
	var userAttrs struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(userAttrsJSON, &userAttrs)

	name := strings.TrimSpace(userAttrs.Name)
	if name == "" && email != "" {
		if at := strings.Index(email, "@"); at > 0 {
			name = email[:at]
		} else {
			name = email
		}
	}
	if name == "" {
		name = "Driver"
	}

	var vehicleAttrs struct {
		Make  string `json:"vehicle_make"`
		Model string `json:"vehicle_model"`
		Color string `json:"vehicle_color"`
		Plate string `json:"vehicle_plate"`
	}
	_ = json.Unmarshal(vehicleJSON, &vehicleAttrs)

	return DriverInfo{
		Name:   name,
		Rating: rating,
		Vehicle: VehicleInfo{
			Make:  strings.TrimSpace(vehicleAttrs.Make),
			Model: strings.TrimSpace(vehicleAttrs.Model),
			Color: strings.TrimSpace(vehicleAttrs.Color),
			Plate: strings.TrimSpace(vehicleAttrs.Plate),
		},
	}
}
