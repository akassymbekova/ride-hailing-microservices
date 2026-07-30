package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

type Claims struct {
	Subject string `json:"sub"`
	Role    string `json:"role"`
	Expires int64  `json:"exp"`
}

func Secret() string {
	if v := os.Getenv("JWT_SECRET"); v != "" {
		return v
	}
	return "ridehail_dev_secret"
}

func ParseBearerToken(rawToken string) (*Claims, error) {
	token := strings.TrimSpace(rawToken)
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInvalidToken
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	signingInput := parts[0] + "." + parts[1]
	expectedSig := sign(signingInput)
	if parts[2] != expectedSig {
		return nil, ErrInvalidToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, ErrInvalidToken
	}

	if claims.Subject == "" || claims.Role == "" {
		return nil, ErrInvalidToken
	}

	if claims.Expires > 0 && time.Now().UTC().Unix() > claims.Expires {
		return nil, ErrExpiredToken
	}

	return &claims, nil
}

func GenerateToken(subject, role string, ttl time.Duration) (string, error) {
	if subject == "" || role == "" {
		return "", ErrInvalidToken
	}

	header, err := encodeSegment(map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	})
	if err != nil {
		return "", err
	}

	claims, err := encodeSegment(Claims{
		Subject: subject,
		Role:    role,
		Expires: time.Now().UTC().Add(ttl).Unix(),
	})
	if err != nil {
		return "", err
	}

	signingInput := header + "." + claims
	return signingInput + "." + sign(signingInput), nil
}

func encodeSegment(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func sign(input string) string {
	mac := hmac.New(sha256.New, []byte(Secret()))
	_, _ = mac.Write([]byte(input))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
