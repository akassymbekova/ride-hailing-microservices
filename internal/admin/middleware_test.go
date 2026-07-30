package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ride-hail/internal/shared/auth"
)

func TestAdminAuthMiddlewareRejectsMissingToken(t *testing.T) {
	handler := AdminAuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/overview", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAdminAuthMiddlewareRejectsNonAdminRole(t *testing.T) {
	token, err := auth.GenerateToken("11111111-1111-4111-8111-111111111111", "PASSENGER", time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	handler := AdminAuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/overview", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestAdminAuthMiddlewareAllowsAdminRole(t *testing.T) {
	token, err := auth.GenerateToken("33333333-3333-4333-8333-333333333333", "ADMIN", time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	handler := AdminAuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/overview", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestParsePositiveInt(t *testing.T) {
	if got := parsePositiveInt("", 1); got != 1 {
		t.Fatalf("expected fallback 1, got %d", got)
	}
	if got := parsePositiveInt("2", 1); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
	if got := parsePositiveInt("bad", 1); got != 1 {
		t.Fatalf("expected fallback for bad input, got %d", got)
	}
}

func TestDistanceKM(t *testing.T) {
	d := distanceKM(43.238949, 76.889709, 43.222015, 76.851511)
	if d <= 0 {
		t.Fatalf("expected positive distance, got %f", d)
	}
}
