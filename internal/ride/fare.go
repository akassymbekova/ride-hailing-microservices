package ride

import (
	"errors"
	"math"
)

const (
	earthRadiusKM             = 6371.0
	averageRideSpeedKMH       = 30.0
	minimumRideDuration       = 1
	moneyRoundingPrecision    = 100.0
	distanceRoundingPrecision = 100.0
)

var (
	ErrInvalidFareRideType = errors.New("unsupported ride type for fare calculation")
	ErrInvalidDistance     = errors.New("distance cannot be negative")
	ErrInvalidDuration     = errors.New("duration cannot be negative")
)

type FareRates struct {
	BaseFare      float64
	RatePerKM     float64
	RatePerMinute float64
}

type FareEstimate struct {
	DistanceKM      float64
	DurationMinutes int
	Fare            float64
}

var fareRatesByRideType = map[string]FareRates{
	RideTypeEconomy: {
		BaseFare:      500,
		RatePerKM:     100,
		RatePerMinute: 50,
	},
	RideTypePremium: {
		BaseFare:      800,
		RatePerKM:     120,
		RatePerMinute: 60,
	},
	RideTypeXL: {
		BaseFare:      1000,
		RatePerKM:     150,
		RatePerMinute: 75,
	},
}

func CalculateFareEstimate(request CreateRideRequest) (FareEstimate, error) {
	distanceKM := CalculateDistanceKM(
		request.PickupLatitude,
		request.PickupLongitude,
		request.DestinationLatitude,
		request.DestinationLongitude,
	)

	durationMinutes := EstimateDurationMinutes(distanceKM)

	fare, err := CalculateFare(
		request.RideType,
		distanceKM,
		durationMinutes,
	)
	if err != nil {
		return FareEstimate{}, err
	}

	return FareEstimate{
		DistanceKM:      round(distanceKM, distanceRoundingPrecision),
		DurationMinutes: durationMinutes,
		Fare:            fare,
	}, nil
}

func CalculateFare(
	rideType string,
	distanceKM float64,
	durationMinutes int,
) (float64, error) {
	if distanceKM < 0 {
		return 0, ErrInvalidDistance
	}

	if durationMinutes < 0 {
		return 0, ErrInvalidDuration
	}

	rates, exists := fareRatesByRideType[rideType]
	if !exists {
		return 0, ErrInvalidFareRideType
	}

	fare := rates.BaseFare +
		(distanceKM * rates.RatePerKM) +
		(float64(durationMinutes) * rates.RatePerMinute)

	return round(fare, moneyRoundingPrecision), nil
}

func CalculateDistanceKM(
	latitude1 float64,
	longitude1 float64,
	latitude2 float64,
	longitude2 float64,
) float64 {
	latitude1Radians := degreesToRadians(latitude1)
	latitude2Radians := degreesToRadians(latitude2)

	latitudeDifference := degreesToRadians(latitude2 - latitude1)
	longitudeDifference := degreesToRadians(longitude2 - longitude1)

	haversine := math.Sin(latitudeDifference/2)*math.Sin(latitudeDifference/2) +
		math.Cos(latitude1Radians)*
			math.Cos(latitude2Radians)*
			math.Sin(longitudeDifference/2)*
			math.Sin(longitudeDifference/2)

	centralAngle := 2 * math.Atan2(
		math.Sqrt(haversine),
		math.Sqrt(1-haversine),
	)

	return earthRadiusKM * centralAngle
}

func EstimateDurationMinutes(distanceKM float64) int {
	if distanceKM <= 0 {
		return minimumRideDuration
	}

	durationHours := distanceKM / averageRideSpeedKMH
	durationMinutes := int(math.Ceil(durationHours * 60))

	if durationMinutes < minimumRideDuration {
		return minimumRideDuration
	}

	return durationMinutes
}

func degreesToRadians(degrees float64) float64 {
	return degrees * math.Pi / 180
}

func round(value float64, precision float64) float64 {
	return math.Round(value*precision) / precision
}
