package ride

import "testing"

func TestCalculateFareEconomy(t *testing.T) {
	fare, err := CalculateFare(RideTypeEconomy, 5.0, 9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fare != 1450 {
		t.Fatalf("expected fare 1450, got %v", fare)
	}
}

func TestValidateCreateRideRequestRejectsSameCoordinates(t *testing.T) {
	request := CreateRideRequest{
		PassengerID:          "11111111-1111-4111-8111-111111111111",
		PickupLatitude:       43.2,
		PickupLongitude:      76.8,
		PickupAddress:        "A",
		DestinationLatitude:  43.2,
		DestinationLongitude: 76.8,
		DestinationAddress:   "B",
		RideType:             RideTypeEconomy,
	}

	err := ValidateCreateRideRequest(&request)
	if err == nil {
		t.Fatal("expected validation error for identical coordinates")
	}
}

func TestCalculateRefund(t *testing.T) {
	if got := CalculateRefund(RideStatusRequested, 1000); got != 1000 {
		t.Fatalf("expected full refund 1000, got %v", got)
	}

	if got := CalculateRefund(RideStatusMatched, 1000); got != 800 {
		t.Fatalf("expected partial refund 800, got %v", got)
	}
}
