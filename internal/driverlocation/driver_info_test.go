package driverlocation

import "testing"

func TestBuildDriverInfo(t *testing.T) {
	info := buildDriverInfo(
		"driver@example.com",
		[]byte(`{"name":"Aidar Nurlan"}`),
		4.8,
		[]byte(`{
			"vehicle_make":"Toyota",
			"vehicle_model":"Camry",
			"vehicle_color":"White",
			"vehicle_plate":"KZ 123 ABC"
		}`),
	)

	if info.Name != "Aidar Nurlan" {
		t.Fatalf("unexpected name: %s", info.Name)
	}
	if info.Rating != 4.8 {
		t.Fatalf("unexpected rating: %f", info.Rating)
	}
	if info.Vehicle.Make != "Toyota" || info.Vehicle.Model != "Camry" {
		t.Fatalf("unexpected vehicle: %+v", info.Vehicle)
	}
	if info.Vehicle.Plate != "KZ 123 ABC" {
		t.Fatalf("unexpected plate: %s", info.Vehicle.Plate)
	}
}

func TestBuildDriverInfoFallbacks(t *testing.T) {
	info := buildDriverInfo("driver@example.com", []byte(`{}`), 5.0, []byte(`{}`))

	if info.Name != "driver" {
		t.Fatalf("unexpected name fallback: %s", info.Name)
	}
	if info.Vehicle.Make != "" {
		t.Fatalf("expected empty vehicle make, got %s", info.Vehicle.Make)
	}
}
