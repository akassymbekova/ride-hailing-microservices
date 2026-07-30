package ride

import "time"

func CalculateRefund(previousStatus string, estimatedFare float64) float64 {
	switch previousStatus {
	case RideStatusRequested:
		return round(estimatedFare, moneyRoundingPrecision)
	case RideStatusMatched:
		return round(estimatedFare*0.8, moneyRoundingPrecision)
	default:
		return 0
	}
}

type CancelResult struct {
	Ride         *Ride
	RefundAmount float64
}

func calculateFinalFare(ride *Ride) (float64, error) {
	if ride == nil {
		return 0, ErrInvalidDistance
	}

	distanceKM := CalculateDistanceKM(
		ride.Pickup.Latitude,
		ride.Pickup.Longitude,
		ride.Destination.Latitude,
		ride.Destination.Longitude,
	)

	durationMinutes := ride.EstimatedDurationMins
	if ride.StartedAt != nil {
		end := time.Now().UTC()
		if ride.CompletedAt != nil {
			end = ride.CompletedAt.UTC()
		}
		elapsed := end.Sub(*ride.StartedAt)
		durationMinutes = int(elapsed.Minutes())
		if durationMinutes < minimumRideDuration {
			durationMinutes = minimumRideDuration
		}
	}

	return CalculateFare(ride.RideType, distanceKM, durationMinutes)
}
