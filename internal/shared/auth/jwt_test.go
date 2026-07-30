package auth

import (
	"testing"
	"time"
)

func TestGenerateAndParseToken(t *testing.T) {
	token, err := GenerateToken("11111111-1111-4111-8111-111111111111", "PASSENGER", time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	claims, err := ParseBearerToken("Bearer " + token)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}

	if claims.Subject != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("unexpected subject: %s", claims.Subject)
	}

	if claims.Role != "PASSENGER" {
		t.Fatalf("unexpected role: %s", claims.Role)
	}
}
