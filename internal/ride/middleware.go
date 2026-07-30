package ride

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"ride-hail/internal/shared/auth"
)

var (
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbiddenPassenger = errors.New("passenger access required")
	ErrPassengerMismatch  = errors.New("passenger_id does not match token")
)

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = "req_" + randomHex(8)
		}

		ctx := withRequestID(r.Context(), requestID)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func PassengerAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		claims, err := auth.ParseBearerToken(header)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
			return
		}

		if strings.ToUpper(claims.Role) != "PASSENGER" {
			writeError(w, http.StatusForbidden, "forbidden", "passenger role required")
			return
		}

		ctx := withAuthClaims(r.Context(), claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func passengerIDFromContext(ctx context.Context) (string, bool) {
	claims, ok := authClaimsFromContext(ctx)
	if !ok {
		return "", false
	}
	return claims.Subject, true
}

func ensurePassengerAccess(ctx context.Context, passengerID string) error {
	tokenPassengerID, ok := passengerIDFromContext(ctx)
	if !ok {
		return ErrUnauthorized
	}
	if tokenPassengerID != passengerID {
		return ErrPassengerMismatch
	}
	return nil
}
