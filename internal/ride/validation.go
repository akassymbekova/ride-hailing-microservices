package ride

import (
	"errors"
	"strings"
)

var (
	ErrNilCreateRideRequest        = errors.New("create ride request is required")
	ErrPassengerIDRequired         = errors.New("passenger_id is required")
	ErrPickupAddressRequired       = errors.New("pickup_address is required")
	ErrDestinationAddressRequired  = errors.New("destination_address is required")
	ErrRideTypeRequired            = errors.New("ride_type is required")
	ErrInvalidRideType             = errors.New("invalid ride_type")
	ErrInvalidPickupLatitude       = errors.New("pickup_latitude must be between -90 and 90")
	ErrInvalidPickupLongitude      = errors.New("pickup_longitude must be between -180 and 180")
	ErrInvalidDestinationLatitude  = errors.New("destination_latitude must be between -90 and 90")
	ErrInvalidDestinationLongitude = errors.New("destination_longitude must be between -180 and 180")
	ErrSamePickupAndDestination    = errors.New("pickup and destination coordinates must be different")
)

func ValidateCreateRideRequest(request *CreateRideRequest) error {
	if request == nil {
		return ErrNilCreateRideRequest
	}

	request.PassengerID = strings.TrimSpace(request.PassengerID)
	request.PickupAddress = strings.TrimSpace(request.PickupAddress)
	request.DestinationAddress = strings.TrimSpace(request.DestinationAddress)
	request.RideType = strings.ToUpper(strings.TrimSpace(request.RideType))

	if request.PassengerID == "" {
		return ErrPassengerIDRequired
	}

	if request.PickupAddress == "" {
		return ErrPickupAddressRequired
	}

	if request.DestinationAddress == "" {
		return ErrDestinationAddressRequired
	}

	if request.PickupLatitude < -90 || request.PickupLatitude > 90 {
		return ErrInvalidPickupLatitude
	}

	if request.PickupLongitude < -180 || request.PickupLongitude > 180 {
		return ErrInvalidPickupLongitude
	}

	if request.DestinationLatitude < -90 || request.DestinationLatitude > 90 {
		return ErrInvalidDestinationLatitude
	}

	if request.DestinationLongitude < -180 || request.DestinationLongitude > 180 {
		return ErrInvalidDestinationLongitude
	}

	if request.PickupLatitude == request.DestinationLatitude &&
		request.PickupLongitude == request.DestinationLongitude {
		return ErrSamePickupAndDestination
	}

	if request.RideType == "" {
		return ErrRideTypeRequired
	}

	switch request.RideType {
	case RideTypeEconomy, RideTypePremium, RideTypeXL:
		return nil
	default:
		return ErrInvalidRideType
	}
}
