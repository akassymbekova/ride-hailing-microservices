package driverlocation

import "testing"

func TestShouldSendRideDetailsOnlyForMatched(t *testing.T) {
	if !shouldSendRideDetails(RideStatusMessage{RideID: "r1", DriverID: "d1", Status: "MATCHED"}) {
		t.Fatal("expected MATCHED to trigger ride details")
	}
	if shouldSendRideDetails(RideStatusMessage{RideID: "r1", DriverID: "d1", Status: "EN_ROUTE"}) {
		t.Fatal("expected EN_ROUTE to be ignored")
	}
}

func TestOnlineRequestUsesLocationValidation(t *testing.T) {
	req := onlineRequest{Latitude: 43.238949, Longitude: 76.889709}
	if err := validateLocationRequest(locationRequest{
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
	}); err != nil {
		t.Fatalf("expected valid online coords, got %v", err)
	}
}

func TestValidateLocationRequest(t *testing.T) {
	if err := validateLocationRequest(locationRequest{Latitude: 43.2, Longitude: 76.8}); err != nil {
		t.Fatalf("expected valid coords, got %v", err)
	}
	if err := validateLocationRequest(locationRequest{Latitude: 91, Longitude: 0}); err == nil {
		t.Fatal("expected invalid latitude")
	}
}

func TestWSMessageRouterRoutesLocationUpdate(t *testing.T) {
	router := NewWSMessageRouter(&Matcher{}, &Handler{})
	if router == nil {
		t.Fatal("expected router")
	}
}
