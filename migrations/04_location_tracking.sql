-- Additive migration for Driver & Location Service: location tracking.
-- Keeps existing tables/columns used by other services.

BEGIN;

CREATE TABLE IF NOT EXISTS location_history (
    id SERIAL PRIMARY KEY,
    coordinate_id INTEGER REFERENCES coordinates(id),
    driver_id VARCHAR(50) REFERENCES drivers(id),
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    accuracy_meters DOUBLE PRECISION,
    speed_kmh DOUBLE PRECISION,
    heading_degrees DOUBLE PRECISION,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ride_id VARCHAR(50) REFERENCES rides(id)
);

CREATE INDEX IF NOT EXISTS idx_location_history_driver ON location_history(driver_id, recorded_at DESC);

COMMIT;
