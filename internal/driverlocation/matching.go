package driverlocation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"ride-hail/internal/shared/logging"
	"ride-hail/internal/shared/messaging"
	"ride-hail/internal/shared/ws"
)

const (
	offerTimeout    = 30 * time.Second
	searchRadiusM   = 5000.0
	maxCandidates   = 5
	driverEarnRatio = 0.8
)

type Matcher struct {
	hub       *ws.Hub
	repo      *Repository
	publisher *messaging.Publisher
	log       *logging.Logger

	mu      sync.Mutex
	pending map[string]chan RideResponse
}

func NewMatcher(hub *ws.Hub, repo *Repository, publisher *messaging.Publisher, log *logging.Logger) *Matcher {
	return &Matcher{
		hub:       hub,
		repo:      repo,
		publisher: publisher,
		log:       log,
		pending:   make(map[string]chan RideResponse),
	}
}

func generateOfferID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "offer_" + hex.EncodeToString(b)
}

func (m *Matcher) MatchRide(ctx context.Context, req RideRequest) {
	m.log.Info(ctx, "matching_started", "searching for nearby drivers", "ride_id", req.RideID)

	candidates, err := m.repo.FindNearbyDrivers(ctx, req.PickupLocation.Lat, req.PickupLocation.Lng, req.RideType, searchRadiusM, maxCandidates)
	if err != nil {
		m.log.Error(ctx, "matching_query_failed", "failed to query nearby drivers", err, "ride_id", req.RideID)
		return
	}

	if len(candidates) == 0 {
		m.log.Info(ctx, "matching_no_candidates", "no available drivers found nearby", "ride_id", req.RideID)
		return
	}

	for _, driver := range candidates {
		accepted, resp := m.offerToDriver(ctx, req, driver)
		if !accepted {
			continue
		}
		m.onMatchSuccess(ctx, req, driver, resp)
		return
	}

	m.log.Info(ctx, "matching_exhausted", "all candidates rejected or timed out", "ride_id", req.RideID)
}

func (m *Matcher) offerToDriver(ctx context.Context, req RideRequest, driver NearbyDriver) (bool, RideResponse) {
	offerID := generateOfferID()

	respCh := make(chan RideResponse, 1)

	m.mu.Lock()
	m.pending[offerID] = respCh
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.pending, offerID)
		m.mu.Unlock()
	}()

	offer := RideOffer{
		Type:                         "ride_offer",
		OfferID:                      offerID,
		RideID:                       req.RideID,
		RideNumber:                   req.RideNumber,
		PickupLocation:               req.PickupLocation,
		DestinationLocation:          req.DestLocation,
		EstimatedFare:                req.EstimatedFare,
		DriverEarnings:               req.EstimatedFare * driverEarnRatio,
		DistanceToPickupKm:           driver.DistanceKm,
		EstimatedRideDurationMinutes: 15,
		ExpiresAt:                    time.Now().Add(offerTimeout).UTC().Format(time.RFC3339),
	}

	if !m.hub.SendJSON(driver.DriverID, offer) {
		m.log.Info(ctx, "matching_driver_unreachable", "driver not connected via websocket, skipping", "ride_id", req.RideID)
		return false, RideResponse{}
	}

	m.log.Info(ctx, "matching_offer_sent", "ride offer sent to driver", "ride_id", req.RideID)

	select {
	case resp := <-respCh:
		if resp.Accepted {
			m.log.Info(ctx, "matching_offer_accepted", "driver accepted the offer", "ride_id", req.RideID)
			return true, resp
		}
		m.log.Info(ctx, "matching_offer_rejected", "driver rejected the offer", "ride_id", req.RideID)
		return false, RideResponse{}

	case <-time.After(offerTimeout):
		m.log.Info(ctx, "matching_offer_timeout", "driver did not respond in time", "ride_id", req.RideID)
		return false, RideResponse{}

	case <-ctx.Done():
		return false, RideResponse{}
	}
}

func (m *Matcher) onMatchSuccess(ctx context.Context, req RideRequest, driver NearbyDriver, resp RideResponse) {
	if err := m.repo.SetDriverStatus(ctx, driver.DriverID, DriverStatusEnRoute); err != nil {
		m.log.Error(ctx, "matching_status_update_failed", "failed to update driver status", err, "ride_id", req.RideID)
	}

	driverInfo, err := m.repo.GetDriverInfo(ctx, driver.DriverID)
	if err != nil {
		m.log.Error(ctx, "matching_driver_info_failed", "failed to load driver profile", err, "ride_id", req.RideID)
		driverInfo = DriverInfo{
			Name:   "Driver",
			Rating: driver.Rating,
		}
	}

	match := DriverMatchResponse{
		RideID:                  req.RideID,
		DriverID:                driver.DriverID,
		Accepted:                true,
		EstimatedArrivalMinutes: estimateArrivalMinutes(driver.DistanceKm),
		DriverLocation:          Location{Lat: driver.Lat, Lng: driver.Lng},
		DriverInfo:              &driverInfo,
		CorrelationID:           req.CorrelationID,
	}

	routingKey := "driver.response." + req.RideID
	if err := m.publisher.Publish("driver_topic", routingKey, match); err != nil {
		m.log.Error(ctx, "matching_publish_failed", "failed to publish driver match response", err, "ride_id", req.RideID)
		return
	}

	m.log.Info(ctx, "matching_completed", "ride successfully matched with driver", "ride_id", req.RideID)
}

func estimateArrivalMinutes(distanceKm float64) int {
	const avgSpeedKmh = 30.0
	minutes := int((distanceKm / avgSpeedKmh) * 60)
	if minutes < 1 {
		return 1
	}
	return minutes
}

func (m *Matcher) HandleDriverResponse(msg ws.InboundMessage) {
	ctx := context.Background()

	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg.Data, &envelope); err != nil {
		return
	}
	if envelope.Type != "ride_response" {
		return
	}

	var resp RideResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		m.log.Error(ctx, "matching_response_parse_failed", "failed to parse ride_response", err)
		return
	}
	resp.OfferID = strings.TrimSpace(resp.OfferID)

	m.mu.Lock()
	ch, ok := m.pending[resp.OfferID]
	m.mu.Unlock()

	if !ok {
		m.log.Info(ctx, "matching_late_response", "received response for expired or unknown offer", "ride_id", resp.RideID)
		return
	}

	select {
	case ch <- resp:
	default:
	}
}
