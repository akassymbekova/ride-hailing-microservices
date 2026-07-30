package ride

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	service *Service
	ws      *WebSocketHandler
}

func NewHandler(service *Service, wsHandler *WebSocketHandler) *Handler {
	return &Handler{
		service: service,
		ws:      wsHandler,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /rides", PassengerAuthMiddleware(h.CreateRide))
	mux.HandleFunc("POST /rides/{ride_id}/cancel", PassengerAuthMiddleware(h.CancelRide))
	mux.HandleFunc("/ws/passengers/", h.ws.HandlePassengerWS)
}

func (h *Handler) CreateRide(w http.ResponseWriter, r *http.Request) {
	var request CreateRideRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if err := ensurePassengerAccess(r.Context(), request.PassengerID); err != nil {
		switch {
		case errors.Is(err, ErrPassengerMismatch):
			writeError(w, http.StatusForbidden, "forbidden", err.Error())
		default:
			writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		}
		return
	}

	ride, err := h.service.CreateRide(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, ErrRepositoryUnavailable):
			writeError(w, http.StatusInternalServerError, "repository_unavailable", err.Error())
		default:
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		}
		return
	}

	response := CreateRideResponse{
		RideID:                   ride.ID,
		RideNumber:               ride.RideNumber,
		Status:                   ride.Status,
		EstimatedFare:            ride.EstimatedFare,
		EstimatedDurationMinutes: ride.EstimatedDurationMins,
		EstimatedDistanceKM:      ride.EstimatedDistanceKM,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(response)
}

func (h *Handler) CancelRide(w http.ResponseWriter, r *http.Request) {
	rideID := r.PathValue("ride_id")
	if rideID == "" {
		rideID = extractRideIDFromCancelPath(r.URL.Path)
	}
	if rideID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "ride_id is required")
		return
	}

	passengerID, ok := passengerIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing auth context")
		return
	}

	var request CancelRideRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&request)
	}

	result, err := h.service.CancelRide(r.Context(), rideID, passengerID, request.Reason)
	if err != nil {
		switch {
		case errors.Is(err, ErrRideNotFound):
			writeError(w, http.StatusNotFound, "ride_not_found", err.Error())
		case errors.Is(err, ErrRideNotCancellable):
			writeError(w, http.StatusConflict, "ride_not_cancellable", err.Error())
		case errors.Is(err, ErrPassengerMismatch):
			writeError(w, http.StatusForbidden, "forbidden", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "cancel_failed", err.Error())
		}
		return
	}

	response := CancelRideResponse{
		RideID:       result.Ride.ID,
		Status:       result.Ride.Status,
		CancelledAt:  result.Ride.CancelledAt,
		RefundAmount: result.RefundAmount,
		Message:      "Ride cancelled successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func extractRideIDFromCancelPath(path string) string {
	path = strings.TrimPrefix(path, "/rides/")
	path = strings.TrimSuffix(path, "/cancel")
	return strings.Trim(path, "/")
}

func writeError(
	w http.ResponseWriter,
	status int,
	code string,
	message string,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Error:   code,
		Message: message,
	})
}

type CancelRideResponse struct {
	RideID       string     `json:"ride_id"`
	Status       string     `json:"status"`
	CancelledAt  *time.Time `json:"cancelled_at,omitempty"`
	RefundAmount float64    `json:"refund_amount"`
	Message      string     `json:"message"`
}
