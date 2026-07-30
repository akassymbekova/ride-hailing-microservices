package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"ride-hail/internal/shared/logging"
)

type Handler struct {
	repo *Repository
	log  *logging.Logger
}

func NewHandler(repo *Repository, log *logging.Logger) *Handler {
	return &Handler{repo: repo, log: log}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/overview", AdminAuthMiddleware(h.Overview))
	mux.HandleFunc("GET /admin/rides/active", AdminAuthMiddleware(h.ActiveRides))
}

func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	resp, err := h.repo.GetOverview(ctx)
	if err != nil {
		h.log.Error(ctx, "admin_overview_failed", "failed to load overview metrics", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load overview")
		return
	}

	resp.Timestamp = time.Now().UTC()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) ActiveRides(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	page := parsePositiveInt(r.URL.Query().Get("page"), 1)
	pageSize := parsePositiveInt(r.URL.Query().Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}

	resp, err := h.repo.GetActiveRides(ctx, page, pageSize)
	if err != nil {
		h.log.Error(ctx, "admin_active_rides_failed", "failed to load active rides", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load active rides")
		return
	}

	if resp.Rides == nil {
		resp.Rides = []ActiveRide{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
}
