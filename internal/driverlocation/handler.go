package driverlocation

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"ride-hail/internal/shared/auth"
	"ride-hail/internal/shared/logging"
	"ride-hail/internal/shared/messaging"
	"ride-hail/internal/shared/ws"
)

type Handler struct {
	repo      *Repository
	hub       *ws.Hub
	publisher *messaging.Publisher
	log       *logging.Logger

	rateMu       sync.Mutex
	lastLocation map[string]time.Time
}

func NewHandler(repo *Repository, hub *ws.Hub, publisher *messaging.Publisher, log *logging.Logger) *Handler {
	return &Handler{
		repo:         repo,
		hub:          hub,
		publisher:    publisher,
		log:          log,
		lastLocation: make(map[string]time.Time),
	}
}

func (h *Handler) publishStatus(ctx context.Context, driverID, status, rideID string) {
	if err := publishDriverStatus(ctx, h.publisher, driverID, status, rideID); err != nil {
		h.log.Error(ctx, "driver_status_publish_failed", "failed to publish driver status event", err)
	}
}

type wsStatusUpdate struct {
	Type   string `json:"type"`
	RideID string `json:"ride_id"`
	Status string `json:"status"`
}

// HandleWSStatusUpdate обрабатывает WebSocket status_update от водителя (Phase 4).
func (h *Handler) HandleWSStatusUpdate(msg ws.InboundMessage) {
	ctx := context.Background()

	var req wsStatusUpdate
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		h.log.Error(ctx, "ws_status_parse_failed", "failed to parse status_update message", err)
		return
	}

	status := strings.ToUpper(strings.TrimSpace(req.Status))
	if status != DriverStatusArrived {
		return
	}

	rideID := strings.TrimSpace(req.RideID)
	if rideID == "" {
		h.log.Info(ctx, "ws_status_invalid", "status_update missing ride_id", "driver_id", msg.ClientID)
		return
	}

	rideStatus, err := h.repo.GetAssignedRideStatus(ctx, msg.ClientID, rideID)
	if err != nil {
		h.log.Error(ctx, "ws_status_ride_lookup_failed", "failed to validate assigned ride", err, "ride_id", rideID)
		return
	}
	if rideStatus != "MATCHED" && rideStatus != "EN_ROUTE" {
		h.log.Info(ctx, "ws_status_rejected", "ride is not ready for arrival update", "ride_id", rideID, "ride_status", rideStatus)
		return
	}

	if err := h.repo.SetDriverStatus(ctx, msg.ClientID, DriverStatusArrived); err != nil {
		h.log.Error(ctx, "ws_status_update_failed", "failed to set driver arrived", err, "ride_id", rideID)
		return
	}
	h.publishStatus(ctx, msg.ClientID, DriverStatusArrived, rideID)

	h.log.Info(ctx, "driver_arrived", "driver reported arrival at pickup", "ride_id", rideID, "driver_id", msg.ClientID)
}

// HandleWSLocationUpdate обрабатывает WebSocket location_update от водителя (Phase 5).
func (h *Handler) HandleWSLocationUpdate(msg ws.InboundMessage) {
	ctx := context.Background()

	var req locationRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		h.log.Error(ctx, "ws_location_parse_failed", "failed to parse location_update message", err)
		return
	}
	if err := validateLocationRequest(req); err != nil {
		h.log.Info(ctx, "ws_location_invalid", err.Error(), "driver_id", msg.ClientID)
		return
	}
	if !h.allowLocationUpdate(msg.ClientID) {
		h.log.Info(ctx, "ws_location_rate_limited", "location update ignored due to rate limit", "driver_id", msg.ClientID)
		return
	}

	if _, err := h.processLocationUpdate(ctx, msg.ClientID, req); err != nil {
		h.log.Error(ctx, "ws_location_update_failed", "failed to persist driver location", err, "driver_id", msg.ClientID)
	}
}

func validateLocationRequest(req locationRequest) error {
	if req.Latitude < -90 || req.Latitude > 90 {
		return errInvalidLatitude
	}
	if req.Longitude < -180 || req.Longitude > 180 {
		return errInvalidLongitude
	}
	return nil
}

var (
	errInvalidLatitude  = &locationValidationError{"latitude must be between -90 and 90"}
	errInvalidLongitude = &locationValidationError{"longitude must be between -180 and 180"}
)

type locationValidationError struct {
	msg string
}

func (e *locationValidationError) Error() string { return e.msg }

func (h *Handler) processLocationUpdate(ctx context.Context, driverID string, req locationRequest) (*locationResponse, error) {
	target, err := h.repo.GetActiveRideTarget(ctx, driverID)
	if err != nil {
		h.log.Error(ctx, "location_ride_lookup_failed", "failed to look up active ride for driver", err)
	}

	var rideID *string
	if target != nil {
		rideID = &target.RideID
	}

	coordinateID, updatedAt, err := h.repo.UpdateDriverLocation(
		ctx, driverID, req.Latitude, req.Longitude, req.AccuracyMeters, req.SpeedKmh, req.HeadingDegrees, rideID,
	)
	if err != nil {
		return nil, err
	}

	event := DriverLocationEvent{
		DriverID:       driverID,
		RideID:         rideID,
		Location:       Location{Lat: req.Latitude, Lng: req.Longitude},
		SpeedKmh:       req.SpeedKmh,
		HeadingDegrees: req.HeadingDegrees,
		Timestamp:      updatedAt,
	}
	if err := h.publisher.Publish("location_fanout", "", event); err != nil {
		h.log.Error(ctx, "location_publish_failed", "failed to publish location update", err)
	}

	return &locationResponse{
		CoordinateID: coordinateID,
		UpdatedAt:    updatedAt,
	}, nil
}

// authorizeDriver проверяет Bearer-токен в заголовке Authorization: роль
// должна быть DRIVER, а subject токена — совпадать с driver_id из URL (водитель
// не может дёргать ручки от имени другого водителя). При ошибке сам пишет
// ответ (401/403) и возвращает false — вызывающему остаётся просто return.
func (h *Handler) authorizeDriver(w http.ResponseWriter, r *http.Request, driverID string) bool {
	claims, err := auth.ParseBearerToken(r.Header.Get("Authorization"))
	if err != nil {
		http.Error(w, "invalid or missing bearer token", http.StatusUnauthorized)
		return false
	}
	if strings.ToUpper(claims.Role) != "DRIVER" {
		http.Error(w, "driver role required", http.StatusForbidden)
		return false
	}
	if claims.Subject != driverID {
		http.Error(w, "driver can only act on their own account", http.StatusForbidden)
		return false
	}
	return true
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func (h *Handler) HandleDriverWS(w http.ResponseWriter, r *http.Request) {
	driverID := extractIDFromPath(r.URL.Path, "/ws/drivers/")
	if driverID == "" {
		http.Error(w, "driver_id is required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error(r.Context(), "ws_upgrade_failed", "failed to upgrade connection to websocket", err)
		return
	}

	validate := func(clientID, token string) bool {
		claims, err := auth.ParseBearerToken(token)
		if err != nil {
			return false
		}
		if strings.ToUpper(claims.Role) != "DRIVER" {
			return false
		}
		return claims.Subject == clientID
	}
	h.hub.HandleConnection(conn, driverID, validate)
}

type onlineRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

func (h *Handler) HandleOnline(w http.ResponseWriter, r *http.Request) {
	driverID := extractIDFromPath(r.URL.Path, "/drivers/")
	if !h.authorizeDriver(w, r, driverID) {
		return
	}

	var req onlineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateLocationRequest(locationRequest{
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.repo.SetDriverStatus(ctx, driverID, DriverStatusAvailable); err != nil {
		h.log.Error(ctx, "driver_online_failed", "failed to set driver available", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	loc := locationRequest{Latitude: req.Latitude, Longitude: req.Longitude}
	if _, err := h.processLocationUpdate(ctx, driverID, loc); err != nil {
		h.log.Error(ctx, "driver_online_location_failed", "failed to save driver location on go online", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.markLocationUpdated(driverID)

	h.publishStatus(ctx, driverID, DriverStatusAvailable, "")

	sessionID, err := h.repo.StartDriverSession(ctx, driverID)
	if err != nil {
		h.log.Error(ctx, "driver_session_start_failed", "failed to start driver session", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "AVAILABLE",
		"session_id": sessionID,
		"message":    "You are now online and ready to accept rides",
	})
}

func (h *Handler) HandleOffline(w http.ResponseWriter, r *http.Request) {
	driverID := extractIDFromPath(r.URL.Path, "/drivers/")
	if !h.authorizeDriver(w, r, driverID) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.repo.SetDriverStatus(ctx, driverID, DriverStatusOffline); err != nil {
		h.log.Error(ctx, "driver_offline_failed", "failed to set driver offline", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.publishStatus(ctx, driverID, DriverStatusOffline, "")

	summary, err := h.repo.EndDriverSession(ctx, driverID)
	if err != nil {
		h.log.Error(ctx, "driver_session_end_failed", "failed to end driver session", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "OFFLINE",
		"session_summary": map[string]any{
			"duration_hours":  summary.DurationHours,
			"rides_completed": summary.RidesCompleted,
			"earnings":        summary.Earnings,
		},
		"message": "You are now offline",
	})
}

type startRequest struct {
	RideID         string `json:"ride_id"`
	DriverLocation struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"driver_location"`
}

func (h *Handler) HandleStart(w http.ResponseWriter, r *http.Request) {
	driverID := extractIDFromPath(r.URL.Path, "/drivers/")
	if !h.authorizeDriver(w, r, driverID) {
		return
	}

	var req startRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.repo.SetDriverStatus(ctx, driverID, DriverStatusBusy); err != nil {
		h.log.Error(ctx, "driver_start_failed", "failed to set driver busy", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.publishStatus(ctx, driverID, DriverStatusBusy, req.RideID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ride_id":    req.RideID,
		"status":     "BUSY",
		"started_at": time.Now().UTC().Format(time.RFC3339),
		"message":    "Ride started successfully",
	})
}

type completeRequest struct {
	RideID                string  `json:"ride_id"`
	ActualDistanceKm      float64 `json:"actual_distance_km"`
	ActualDurationMinutes int     `json:"actual_duration_minutes"`
	FinalLocation         struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"final_location"`
}

func (h *Handler) HandleComplete(w http.ResponseWriter, r *http.Request) {
	driverID := extractIDFromPath(r.URL.Path, "/drivers/")
	if !h.authorizeDriver(w, r, driverID) {
		return
	}

	var req completeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.repo.SetDriverStatus(ctx, driverID, DriverStatusAvailable); err != nil {
		h.log.Error(ctx, "driver_complete_failed", "failed to set driver available", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.publishStatus(ctx, driverID, DriverStatusAvailable, req.RideID)

	var driverEarnings float64
	if fare, err := h.repo.GetRideFare(ctx, req.RideID); err != nil {
		h.log.Error(ctx, "ride_fare_lookup_failed", "failed to look up ride fare for earnings", err)
	} else {
		driverEarnings = fare * driverEarnRatio
		if err := h.repo.RecordCompletedRide(ctx, driverID, driverEarnings); err != nil {
			h.log.Error(ctx, "driver_session_record_failed", "failed to record completed ride in session", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ride_id":         req.RideID,
		"status":          "AVAILABLE",
		"completed_at":    time.Now().UTC().Format(time.RFC3339),
		"driver_earnings": driverEarnings,
		"message":         "Ride completed successfully",
	})
}

const locationRateLimit = 3 * time.Second

type locationRequest struct {
	Latitude       float64  `json:"latitude"`
	Longitude      float64  `json:"longitude"`
	AccuracyMeters *float64 `json:"accuracy_meters,omitempty"`
	SpeedKmh       *float64 `json:"speed_kmh,omitempty"`
	HeadingDegrees *float64 `json:"heading_degrees,omitempty"`
}

type locationResponse struct {
	CoordinateID int64     `json:"coordinate_id"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// allowLocationUpdate реализует rate limit "не чаще 1 обновления в 3 секунды
// на водителя" из ТЗ.
func (h *Handler) allowLocationUpdate(driverID string) bool {
	h.rateMu.Lock()
	defer h.rateMu.Unlock()

	now := time.Now()
	if last, ok := h.lastLocation[driverID]; ok && now.Sub(last) < locationRateLimit {
		return false
	}
	h.lastLocation[driverID] = now
	return true
}

func (h *Handler) markLocationUpdated(driverID string) {
	h.rateMu.Lock()
	h.lastLocation[driverID] = time.Now()
	h.rateMu.Unlock()
}

// HandleLocation обрабатывает POST /drivers/{id}/location: валидирует
// координаты, пишет новую текущую позицию (с архивацией в location_history)
// и публикует событие в location_fanout для Ride Service.
func (h *Handler) HandleLocation(w http.ResponseWriter, r *http.Request) {
	driverID := extractIDFromPath(r.URL.Path, "/drivers/")
	if !h.authorizeDriver(w, r, driverID) {
		return
	}

	var req locationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateLocationRequest(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if !h.allowLocationUpdate(driverID) {
		http.Error(w, "location updates are rate limited to 1 per 3 seconds", http.StatusTooManyRequests)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.processLocationUpdate(ctx, driverID, req)
	if err != nil {
		h.log.Error(ctx, "location_update_failed", "failed to update driver location", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func extractIDFromPath(path, prefix string) string {
	rest := strings.TrimPrefix(path, prefix)
	if idx := strings.Index(rest, "/"); idx != -1 {
		return rest[:idx]
	}
	return rest
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
