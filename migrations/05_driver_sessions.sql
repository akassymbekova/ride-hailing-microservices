-- Additive migration: driver sessions (online/offline tracking).

BEGIN;

CREATE TABLE IF NOT EXISTS driver_sessions (
    id SERIAL PRIMARY KEY,
    driver_id VARCHAR(50) NOT NULL REFERENCES drivers(id),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at TIMESTAMPTZ,
    total_rides INTEGER NOT NULL DEFAULT 0,
    total_earnings DECIMAL(10, 2) NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_driver_sessions_open ON driver_sessions(driver_id) WHERE ended_at IS NULL;

COMMIT;
