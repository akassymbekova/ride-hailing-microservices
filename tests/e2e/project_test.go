//go:build integration

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"ride-hail/internal/shared/auth"
)

const (
	passengerID = "11111111-1111-4111-8111-111111111111"
	driverID    = "22222222-2222-4222-8222-222222222222"
	adminID     = "33333333-3333-4333-8333-333333333333"

	pickupLat  = 43.238949
	pickupLng  = 76.889709
	dropoffLat = 43.222015
	dropoffLng = 76.851511
)

type env struct {
	rideBase   string
	driverBase string
	adminBase  string
}

func testEnv() env {
	return env{
		rideBase:   getenv("RIDE_SERVICE_URL", "http://localhost:3000"),
		driverBase: getenv("DRIVER_SERVICE_URL", "http://localhost:3001"),
		adminBase:  getenv("ADMIN_SERVICE_URL", "http://localhost:3004"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func requireServices(t *testing.T) env {
	t.Helper()
	e := testEnv()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	checks := []struct {
		name string
		url  string
	}{
		{"ride-service", e.rideBase + "/rides"},
		{"driver-location-service", e.driverBase + "/drivers/"},
		{"admin-service", e.adminBase + "/admin/overview"},
	}

	for _, c := range checks {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
		if err != nil {
			t.Fatalf("build request for %s: %v", c.name, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Skipf("skip e2e: %s unavailable at %s (%v)", c.name, c.url, err)
		}
		_ = resp.Body.Close()
	}

	return e
}

func mustToken(t *testing.T, subject, role string) string {
	t.Helper()
	token, err := auth.GenerateToken(subject, role, time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token
}

func doJSON(t *testing.T, method, url, token string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, data
}

type wsClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func dialWS(t *testing.T, url string) *wsClient {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial ws %s: %v", url, err)
	}
	return &wsClient{conn: conn}
}

func (c *wsClient) auth(t *testing.T, token string) {
	t.Helper()
	c.writeJSON(t, map[string]string{
		"type":  "auth",
		"token": "Bearer " + token,
	})
}

func (c *wsClient) writeJSON(t *testing.T, v any) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.WriteJSON(v); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

func (c *wsClient) readUntil(t *testing.T, timeout time.Duration, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var msg map[string]any
		if err := c.conn.ReadJSON(&msg); err != nil {
			continue
		}
		if match(msg) {
			return msg
		}
	}
	t.Fatalf("ws message not received within %s", timeout)
	return nil
}

func (c *wsClient) close() {
	_ = c.conn.Close()
}

type passengerCollector struct {
	mu       sync.Mutex
	statuses []string
	last     map[string]any
}

func (p *passengerCollector) start(t *testing.T, token string, e env) {
	t.Helper()
	client := dialWS(t, wsURL(e.rideBase, "/ws/passengers/"+passengerID))
	client.auth(t, token)

	go func() {
		defer client.close()
		for {
			var msg map[string]any
			if err := client.conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg["type"] != "ride_status_update" {
				continue
			}
			status, _ := msg["status"].(string)
			p.mu.Lock()
			p.statuses = append(p.statuses, status)
			p.last = msg
			p.mu.Unlock()
		}
	}()

	time.Sleep(300 * time.Millisecond)
}

func (p *passengerCollector) waitStatus(t *testing.T, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		for _, s := range p.statuses {
			if s == want {
				p.mu.Unlock()
				return
			}
		}
		p.mu.Unlock()
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("passenger status %q not received within %s (got %v)", want, timeout, p.statuses)
}

func wsURL(httpBase, path string) string {
	return "ws" + strings.TrimPrefix(httpBase, "http") + path
}

func TestAdminAPI(t *testing.T) {
	e := requireServices(t)
	token := mustToken(t, adminID, "ADMIN")

	code, body := doJSON(t, http.MethodGet, e.adminBase+"/admin/overview", token, nil)
	if code != http.StatusOK {
		t.Fatalf("admin overview status=%d body=%s", code, body)
	}

	var overview struct {
		Metrics struct {
			ActiveRides int `json:"active_rides"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal(body, &overview); err != nil {
		t.Fatalf("decode overview: %v", err)
	}

	code, body = doJSON(t, http.MethodGet, e.adminBase+"/admin/rides/active?page=1&page_size=5", token, nil)
	if code != http.StatusOK {
		t.Fatalf("admin active rides status=%d body=%s", code, body)
	}
}

func TestFullRideLifecycle(t *testing.T) {
	e := requireServices(t)

	passengerToken := mustToken(t, passengerID, "PASSENGER")
	driverToken := mustToken(t, driverID, "DRIVER")

	code, body := doJSON(t, http.MethodPost, fmt.Sprintf("%s/drivers/%s/online", e.driverBase, driverID), driverToken, map[string]float64{
		"latitude":  pickupLat,
		"longitude": pickupLng,
	})
	if code != http.StatusOK {
		t.Fatalf("driver online status=%d body=%s", code, body)
	}

	collector := &passengerCollector{}
	collector.start(t, passengerToken, e)

	driverWS := dialWS(t, wsURL(e.driverBase, "/ws/drivers/"+driverID))
	defer driverWS.close()
	driverWS.auth(t, driverToken)

	code, body = doJSON(t, http.MethodPost, e.rideBase+"/rides", passengerToken, map[string]any{
		"passenger_id":          passengerID,
		"pickup_latitude":       pickupLat,
		"pickup_longitude":      pickupLng,
		"pickup_address":        "Almaty Central Park",
		"destination_latitude":  dropoffLat,
		"destination_longitude": dropoffLng,
		"destination_address":   "Kok-Tobe Hill",
		"ride_type":             "ECONOMY",
	})
	if code != http.StatusCreated {
		t.Fatalf("create ride status=%d body=%s", code, body)
	}

	var created struct {
		RideID string `json:"ride_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create ride: %v", err)
	}
	if created.RideID == "" {
		t.Fatal("empty ride_id in create response")
	}
	if created.Status != "REQUESTED" {
		t.Fatalf("expected REQUESTED, got %s", created.Status)
	}

	offer := driverWS.readUntil(t, 35*time.Second, func(msg map[string]any) bool {
		return msg["type"] == "ride_offer" && msg["ride_id"] == created.RideID
	})

	offerID, _ := offer["offer_id"].(string)
	if offerID == "" {
		t.Fatalf("missing offer_id in %v", offer)
	}

	driverWS.writeJSON(t, map[string]any{
		"type":     "ride_response",
		"offer_id": offerID,
		"ride_id":  created.RideID,
		"accepted": true,
	})

	driverWS.readUntil(t, 10*time.Second, func(msg map[string]any) bool {
		return msg["type"] == "ride_details" && msg["ride_id"] == created.RideID
	})

	collector.waitStatus(t, "MATCHED", 15*time.Second)
	collector.waitStatus(t, "EN_ROUTE", 15*time.Second)

	driverWS.writeJSON(t, map[string]any{
		"type":    "status_update",
		"ride_id": created.RideID,
		"status":  "ARRIVED",
	})
	collector.waitStatus(t, "ARRIVED", 15*time.Second)

	driverWS.writeJSON(t, map[string]any{
		"type":      "location_update",
		"latitude":  43.236,
		"longitude": 76.886,
	})

	code, body = doJSON(t, http.MethodPost, fmt.Sprintf("%s/drivers/%s/start", e.driverBase, driverID), driverToken, map[string]any{
		"ride_id": created.RideID,
		"driver_location": map[string]float64{
			"latitude":  43.236,
			"longitude": 76.886,
		},
	})
	if code != http.StatusOK {
		t.Fatalf("start ride status=%d body=%s", code, body)
	}
	collector.waitStatus(t, "IN_PROGRESS", 15*time.Second)

	code, body = doJSON(t, http.MethodPost, fmt.Sprintf("%s/drivers/%s/complete", e.driverBase, driverID), driverToken, map[string]any{
		"ride_id":                 created.RideID,
		"actual_distance_km":      3.6,
		"actual_duration_minutes": 12,
		"final_location": map[string]float64{
			"latitude":  dropoffLat,
			"longitude": dropoffLng,
		},
	})
	if code != http.StatusOK {
		t.Fatalf("complete ride status=%d body=%s", code, body)
	}
	collector.waitStatus(t, "COMPLETED", 15*time.Second)

	code, _ = doJSON(t, http.MethodPost, fmt.Sprintf("%s/drivers/%s/offline", e.driverBase, driverID), driverToken, nil)
	if code != http.StatusOK {
		t.Fatalf("driver offline status=%d", code)
	}
}

func TestRideCancellation(t *testing.T) {
	e := requireServices(t)

	passengerToken := mustToken(t, passengerID, "PASSENGER")
	driverToken := mustToken(t, driverID, "DRIVER")

	code, body := doJSON(t, http.MethodPost, fmt.Sprintf("%s/drivers/%s/online", e.driverBase, driverID), driverToken, map[string]float64{
		"latitude":  pickupLat,
		"longitude": pickupLng,
	})
	if code != http.StatusOK {
		t.Fatalf("driver online status=%d body=%s", code, body)
	}

	code, body = doJSON(t, http.MethodPost, e.rideBase+"/rides", passengerToken, map[string]any{
		"passenger_id":          passengerID,
		"pickup_latitude":       pickupLat,
		"pickup_longitude":      pickupLng,
		"pickup_address":        "Almaty Central Park",
		"destination_latitude":  dropoffLat,
		"destination_longitude": dropoffLng,
		"destination_address":   "Kok-Tobe Hill",
		"ride_type":             "ECONOMY",
	})
	if code != http.StatusCreated {
		t.Fatalf("create ride status=%d body=%s", code, body)
	}

	var created struct {
		RideID string `json:"ride_id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create ride: %v", err)
	}

	code, body = doJSON(t, http.MethodPost, e.rideBase+"/rides/"+created.RideID+"/cancel", passengerToken, map[string]string{
		"reason": "e2e cancellation test",
	})
	if code != http.StatusOK {
		t.Fatalf("cancel ride status=%d body=%s", code, body)
	}

	var cancelled struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &cancelled); err != nil {
		t.Fatalf("decode cancel: %v", err)
	}
	if cancelled.Status != "CANCELLED" {
		t.Fatalf("expected CANCELLED, got %s", cancelled.Status)
	}
}
