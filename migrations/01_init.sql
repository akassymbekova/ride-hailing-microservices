-- Включаем PostGIS расширение для работы с гео-данными
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(50) PRIMARY KEY,
    email VARCHAR(100) NOT NULL
);

CREATE TABLE IF NOT EXISTS drivers (
    id VARCHAR(50) PRIMARY KEY REFERENCES users(id),
    status VARCHAR(20) NOT NULL DEFAULT 'OFFLINE',
    vehicle_type VARCHAR(20) NOT NULL,
    rating NUMERIC(3, 2) DEFAULT 5.0,
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS coordinates (
    id SERIAL PRIMARY KEY,
    entity_id VARCHAR(50) NOT NULL,
    entity_type VARCHAR(20) NOT NULL, -- 'driver' или 'passenger'
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    is_current BOOLEAN DEFAULT TRUE,
    timestamp TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS rides (
    id VARCHAR(50) PRIMARY KEY,
    passenger_id VARCHAR(50) NOT NULL,
    driver_id VARCHAR(50) REFERENCES users(id),
    status VARCHAR(20) NOT NULL DEFAULT 'REQUESTED',
    pickup_lat DOUBLE PRECISION NOT NULL,
    pickup_lng DOUBLE PRECISION NOT NULL,
    dropoff_lat DOUBLE PRECISION NOT NULL,
    dropoff_lng DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);