# Ride Hail

A microservices-based ride-hailing backend written in Go. The system orchestrates the full ride lifecycle—from passenger request through driver matching, real-time location tracking, and ride completion—using PostgreSQL (PostGIS), RabbitMQ, and WebSockets.

## Architecture

Three HTTP services share one binary (`ride-hail-system`) and are started via the `SERVICE_NAME` environment variable:

| Service | Port (default) | Responsibility |
|---------|----------------|----------------|
| **Ride Service** | `3000` | Ride lifecycle, passenger API, passenger WebSocket notifications |
| **Driver & Location Service** | `3001` | Driver matching, location updates, driver WebSocket (offers & details) |
| **Admin Service** | `3004` | Monitoring dashboard API (overview metrics, active rides) |

```
┌─────────────┐     ride_topic      ┌──────────────────────────┐
│ Ride Service│ ──────────────────► │ Driver & Location Service │
│   :3000     │ ◄── driver_topic ── │         :3001             │
└──────┬──────┘                     └────────────┬─────────────┘
       │                                       │
       │         PostgreSQL + PostGIS          │
       └───────────────────┬───────────────────┘
                           │
                    ┌──────▼──────┐
                    │  RabbitMQ   │
                    └─────────────┘
```

### Message flow (matching)

1. Passenger creates a ride → Ride Service persists it and publishes `ride.request.*` to RabbitMQ.
2. Driver & Location Service consumes `driver_matching`, finds nearby available drivers (PostGIS), sends `ride_offer` over WebSocket.
3. Driver accepts via WebSocket → match response published → Ride Service assigns driver and notifies passenger (`MATCHED`).
4. Status and location updates propagate through `driver.status.*` and `location_fanout`.

### Ride status lifecycle

```
REQUESTED → MATCHED → EN_ROUTE → ARRIVED → IN_PROGRESS → COMPLETED
                └────────────────── CANCELLED ──────────────────┘
```

## Tech stack

Allowed dependencies (per project spec):

- Go 1.24+
- [pgx/v5](https://github.com/jackc/pgx) — PostgreSQL driver
- [amqp091-go](https://github.com/rabbitmq/amqp091-go) — RabbitMQ client
- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket

Infrastructure:

- PostgreSQL 15 + PostGIS 3.3
- RabbitMQ 3 (management UI on port `15672`)

## Prerequisites

- Go 1.24+
- Docker & Docker Compose
- [gofumpt](https://github.com/mvdan/gofumpt) (installed automatically by `make fmt`)
- Optional: [wscat](https://github.com/websockets/wscat) for manual WebSocket testing
- Optional: [jq](https://jqlang.github.io/jq/) for pretty JSON in curl

## Quick start

### 1. Start infrastructure

```bash
make run-infra
make migrate
make build
```

> **Note:** You do **not** need to delete containers before every run. Use `make clean` only when you want a full reset (wipes database volumes).

### 2. Seed demo drivers

Migration `03_seed_demo.sql` creates demo **users** only. Insert **driver** rows manually (required for matching):

```bash
docker exec -i ride_hail_postgres psql -U ridehail_user -d ridehail_db <<'SQL'
BEGIN;

INSERT INTO drivers (id, status, vehicle_type, rating, vehicle_attrs)
VALUES
  ('22222222-2222-4222-8222-222222222222', 'OFFLINE', 'ECONOMY', 5.0,
   '{"vehicle_make":"Toyota","vehicle_model":"Camry","vehicle_color":"White","vehicle_plate":"KZ 123 ABC","vehicle_year":2020}'::jsonb)
ON CONFLICT (id) DO UPDATE SET
  status = EXCLUDED.status,
  vehicle_type = EXCLUDED.vehicle_type,
  vehicle_attrs = EXCLUDED.vehicle_attrs;

COMMIT;
SQL
```

### 3. Run services (three terminals)

```bash
SERVICE_NAME=ride-service ./ride-hail-system
SERVICE_NAME=driver-location-service ./ride-hail-system
SERVICE_NAME=admin-service ./ride-hail-system
```

### 4. Generate JWT tokens

```bash
export PASSENGER_ID=11111111-1111-4111-8111-111111111111
export DRIVER_ID=22222222-2222-4222-8222-222222222222
export ADMIN_ID=33333333-3333-4333-8333-333333333333

export PASSENGER_TOKEN=$(SUBJECT=$PASSENGER_ID ROLE=PASSENGER go run ./cmd/gen-admin-token)
export DRIVER_TOKEN=$(SUBJECT=$DRIVER_ID ROLE=DRIVER go run ./cmd/gen-admin-token)
export ADMIN_TOKEN=$(SUBJECT=$ADMIN_ID ROLE=ADMIN go run ./cmd/gen-admin-token)
```

Default JWT secret: `ridehail_dev_secret` (override with `JWT_SECRET`).

## Demo identities

| Role | UUID | Email |
|------|------|-------|
| Passenger | `11111111-1111-4111-8111-111111111111` | passenger@example.com |
| Driver | `22222222-2222-4222-8222-222222222222` | driver@example.com |
| Admin | `33333333-3333-4333-8333-333333333333` | admin@example.com |

## API reference

### Ride Service (`:3000`)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/rides` | Passenger JWT | Create ride request |
| `POST` | `/rides/{ride_id}/cancel` | Passenger JWT | Cancel ride |
| `WS` | `/ws/passengers/{passenger_id}` | JWT in first message | Real-time status & location updates |

**Create ride example:**

```bash
curl -s -X POST http://localhost:3000/rides \
  -H "Authorization: Bearer $PASSENGER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "passenger_id": "11111111-1111-4111-8111-111111111111",
    "pickup_latitude": 43.238949,
    "pickup_longitude": 76.889709,
    "pickup_address": "Almaty Central Park",
    "destination_latitude": 43.222015,
    "destination_longitude": 76.851511,
    "destination_address": "Kok-Tobe Hill",
    "ride_type": "ECONOMY"
  }'
```

### Driver & Location Service (`:3001`)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/drivers/{id}/online` | Driver JWT | Go online (saves coordinates from body) |
| `POST` | `/drivers/{id}/offline` | Driver JWT | Go offline, session summary |
| `POST` | `/drivers/{id}/location` | Driver JWT | Update location (rate limit: 1 per 3s) |
| `POST` | `/drivers/{id}/start` | Driver JWT | Start ride → `IN_PROGRESS` |
| `POST` | `/drivers/{id}/complete` | Driver JWT | Complete ride → `COMPLETED` |
| `WS` | `/ws/drivers/{driver_id}` | JWT in first message | Offers, details, inbound driver messages |

**Go online:**

```bash
curl -s -X POST "http://localhost:3001/drivers/$DRIVER_ID/online" \
  -H "Authorization: Bearer $DRIVER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"latitude":43.238949,"longitude":76.889709}'
```

### Admin Service (`:3004`)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/admin/overview` | Admin JWT | System metrics snapshot |
| `GET` | `/admin/rides/active` | Admin JWT | Paginated active rides |

```bash
curl -s -H "Authorization: Bearer $ADMIN_TOKEN" http://localhost:3004/admin/overview | jq .
```

## WebSocket protocol

### Authentication (first message on every connection)

```json
{"type":"auth","token":"Bearer <JWT>"}
```

The token subject must match the `{passenger_id}` or `{driver_id}` in the URL path.

### Driver → server (inbound)

| Type | Description |
|------|-------------|
| `ride_response` | Accept/reject a ride offer (must arrive within **30 seconds**) |
| `status_update` | Report arrival: `"status":"ARRIVED"` |
| `location_update` | GPS update during ride |

**Accept offer:**

```json
{
  "type": "ride_response",
  "offer_id": "<from ride_offer>",
  "ride_id": "<uuid>",
  "accepted": true
}
```

**Arrived at pickup:**

```json
{
  "type": "status_update",
  "ride_id": "<uuid>",
  "status": "ARRIVED"
}
```

### Server → driver (outbound)

| Type | When |
|------|------|
| `ride_offer` | New matching request |
| `ride_details` | After successful match (passenger & pickup info) |

> Completion is **not** sent over driver WebSocket. The driver receives the result via the HTTP response from `POST /complete`.

### Server → passenger (outbound)

| Type | Description |
|------|-------------|
| `ride_status_update` | Status changes (`MATCHED`, `EN_ROUTE`, `ARRIVED`, `IN_PROGRESS`, `COMPLETED`, `CANCELLED`) |
| `driver_location_update` | Driver position, distance to pickup, ETA |

## Manual end-to-end test

1. **Passenger WS** — `wscat -c ws://localhost:3000/ws/passengers/11111111-1111-4111-8111-111111111111`
2. **Driver WS** — `wscat -c ws://localhost:3001/ws/drivers/22222222-2222-4222-8222-222222222222`
3. Send `auth` on both connections.
4. Driver: `POST /online` with coordinates.
5. Passenger: `POST /rides`.
6. Driver WS: accept `ride_offer` within 30s (copy `offer_id` and `ride_id` exactly).
7. Driver WS: `status_update` → `ARRIVED`.
8. Driver WS or HTTP: `location_update`.
9. Driver HTTP: `POST /start` with `"ride_id":"<uuid>"` (set `export RIDE_ID=...`).
10. Driver HTTP: `POST /complete`.
11. Passenger WS should show `COMPLETED` with `final_fare`.

## Testing

### Unit tests

```bash
make test
```

Runs all packages except integration-tagged E2E tests.

### End-to-end tests

Requires running infrastructure and all three services:

```bash
make test-e2e
```

E2E tests live in `tests/e2e/project_test.go` (build tag `integration`). They cover:

- Admin API health
- Full ride lifecycle (match → arrive → start → complete)
- Ride cancellation

If services are unavailable, tests **skip** gracefully.

Optional URL overrides:

```bash
export RIDE_SERVICE_URL=http://localhost:3000
export DRIVER_SERVICE_URL=http://localhost:3001
export ADMIN_SERVICE_URL=http://localhost:3004
make test-e2e
```

## Makefile commands

| Command | Description |
|---------|-------------|
| `make help` | List available targets |
| `make build` | Format + build `ride-hail-system` |
| `make run-infra` | Start Postgres + RabbitMQ in Docker |
| `make migrate` | Apply SQL migrations |
| `make stop-infra` | Stop containers (keep data) |
| `make clean` | Remove binary and **destroy** Docker volumes |
| `make fmt` | Run gofumpt |
| `make vet` | Run go vet |
| `make test` | Unit tests |
| `make test-e2e` | Integration E2E tests |

## Environment variables

### Shared / database

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `ridehail_user` | Database user |
| `DB_PASSWORD` | `ridehail_pass` | Database password |
| `DB_NAME` | `ridehail_db` | Database name |
| `JWT_SECRET` | `ridehail_dev_secret` | HMAC secret for JWT |

### RabbitMQ

| Variable | Default |
|----------|---------|
| `RABBITMQ_HOST` | `localhost` |
| `RABBITMQ_PORT` | `5672` |
| `RABBITMQ_USER` | `guest` |
| `RABBITMQ_PASSWORD` | `guest` |

### Service ports

| Variable | Default |
|----------|---------|
| `RIDE_SERVICE_PORT` | `3000` |
| `DRIVER_LOCATION_SERVICE_PORT` | `3001` |
| `ADMIN_SERVICE_PORT` | `3004` |

## RabbitMQ topology

| Exchange | Type | Purpose |
|----------|------|---------|
| `ride_topic` | topic | Ride requests & status |
| `driver_topic` | topic | Driver responses & status |
| `location_fanout` | fanout | Location broadcasts |

| Queue | Binding |
|-------|---------|
| `ride_requests` | `ride.request.*` |
| `ride_status` | `ride.status.*` |
| `driver_matching` | `ride.request.*` |
| `driver_responses` | `driver.response.*` |
| `driver_status` | `driver.status.*` |
| `location_updates_ride` | `location_fanout` |

Consumers automatically resubscribe after RabbitMQ reconnect.

Management UI: http://localhost:15672 (guest / guest)

## Project layout

```
.
├── main.go                    # Single binary entry; SERVICE_NAME switch
├── cmd/
│   ├── ride-service/
│   ├── driver-location-service/
│   ├── admin-service/
│   └── gen-admin-token/       # JWT helper for local testing
├── internal/
│   ├── ride/                  # Ride orchestration
│   ├── driverlocation/        # Matching, location, driver HTTP/WS
│   ├── admin/                 # Admin dashboard API
│   └── shared/                # auth, db, logging, messaging, ws
├── migrations/                # SQL schema & seed
├── tests/e2e/                 # Integration tests (tag: integration)
├── docker-compose.yml
└── makefile
```

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `relation "rides" does not exist` | Migrations not applied | `make migrate` |
| No drivers found for matching | Missing `drivers` row or wrong status | Insert driver; call `/online` with coordinates |
| `matching_offer_timeout` | Accept took > 30s or wrong `offer_id` | Accept immediately; no extra spaces in JSON |
| Passenger never gets status | Passenger WS not connected or wrong token | Reconnect wscat; auth first |
| `ride_id: ""` in `/start` response | `$RIDE_ID` not exported in shell | `export RIDE_ID=<uuid>` |
| WS disconnect code 1006 | Auth not sent within 5s | First message must be `auth` |
| E2E tests skip | Services not running | Start all three services before `make test-e2e` |

## Resilience

- **RabbitMQ:** automatic reconnect with consumer restart (`RunConsumer`).
- **WebSocket:** server ping every 30s; connection closed if no pong within 60s.
- **Location rate limit:** max one update per driver every 3 seconds.

## License

Academic / demo project — see course requirements in `req.md`.
