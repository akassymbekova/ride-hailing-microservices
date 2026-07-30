package driverlocation

import "testing"

func TestShouldSendRideDetails(t *testing.T) {
	tests := []struct {
		name string
		msg  RideStatusMessage
		want bool
	}{
		{
			name: "matched with driver",
			msg: RideStatusMessage{
				RideID:   "ride-1",
				Status:   "MATCHED",
				DriverID: "driver-1",
			},
			want: true,
		},
		{
			name: "matched lowercase",
			msg: RideStatusMessage{
				RideID:   "ride-1",
				Status:   "matched",
				DriverID: "driver-1",
			},
			want: true,
		},
		{
			name: "in progress ignored",
			msg: RideStatusMessage{
				RideID:   "ride-1",
				Status:   "IN_PROGRESS",
				DriverID: "driver-1",
			},
			want: false,
		},
		{
			name: "matched without driver",
			msg: RideStatusMessage{
				RideID: "ride-1",
				Status: "MATCHED",
			},
			want: false,
		},
		{
			name: "missing ride id",
			msg: RideStatusMessage{
				Status:   "MATCHED",
				DriverID: "driver-1",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSendRideDetails(tt.msg); got != tt.want {
				t.Fatalf("shouldSendRideDetails() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPassengerContactFromAttrs(t *testing.T) {
	name, phone, notes := passengerContactFromAttrs(
		[]byte(`{"name":"Saule Karimova","phone":"+7-700-000-00-00","pickup_notes":"Near entrance"}`),
		"passenger@example.com",
	)

	if name != "Saule Karimova" {
		t.Fatalf("unexpected name: %s", name)
	}
	if phone != "+7-700-000-00-00" {
		t.Fatalf("unexpected phone: %s", phone)
	}
	if notes != "Near entrance" {
		t.Fatalf("unexpected notes: %s", notes)
	}
}

func TestPassengerContactFromAttrsFallbacks(t *testing.T) {
	name, phone, notes := passengerContactFromAttrs([]byte(`{}`), "passenger@example.com")

	if name != "passenger" {
		t.Fatalf("unexpected name fallback: %s", name)
	}
	if phone != "+7-XXX-XXX-XX-XX" {
		t.Fatalf("unexpected phone fallback: %s", phone)
	}
	if notes != "" {
		t.Fatalf("unexpected notes fallback: %s", notes)
	}
}
