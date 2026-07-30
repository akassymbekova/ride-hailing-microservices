package main

import (
	"fmt"
	"os"

	// Импортируем main функции наших сервисов, переименовав их
	adminMain "ride-hail/cmd/admin-service"
	locationMain "ride-hail/cmd/driver-location-service"
	rideMain "ride-hail/cmd/ride-service"
)

func main() {
	service := os.Getenv("SERVICE_NAME")

	switch service {
	case "admin-service":
		adminMain.Run()
	case "driver-location-service":
		locationMain.Run()
	case "ride-service":
		rideMain.Run()
	default:
		fmt.Printf("Unknown or missing SERVICE_NAME: '%s'. Please set SERVICE_NAME env variable.\n", service)
		os.Exit(1)
	}
}
