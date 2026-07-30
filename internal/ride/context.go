package ride

import (
	"context"

	"ride-hail/internal/shared/auth"
	"ride-hail/internal/shared/logging"
)

type ctxKey string

const (
	authClaimsKey ctxKey = "auth_claims"
)

func withRideID(ctx context.Context, rideID string) context.Context {
	return context.WithValue(ctx, logging.RideIDKey, rideID)
}

func withRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, logging.RequestIDKey, requestID)
}

func withAuthClaims(ctx context.Context, claims *auth.Claims) context.Context {
	return context.WithValue(ctx, authClaimsKey, claims)
}

func authClaimsFromContext(ctx context.Context) (*auth.Claims, bool) {
	claims, ok := ctx.Value(authClaimsKey).(*auth.Claims)
	return claims, ok && claims != nil
}
