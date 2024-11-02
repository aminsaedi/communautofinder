package communautofinder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"time"
)

const fetchDelayInMin = 0.1 // delay between two API call

const dateFormat = "2006-01-02T15:04:05" // time format accepted by communauto API

// As soon as at least one car is found return the number of cars found

// Function to calculate the distance between two coordinates using the Haversine formula
func haversine(coord1, coord2 location) float64 {
	const earthRadius = 6371 // Earth radius in kilometers

	dLat := (coord2.Latitude - coord1.Latitude) * (math.Pi / 180.0)
	dLon := (coord2.Longitude - coord1.Longitude) * (math.Pi / 180.0)

	lat1 := coord1.Latitude * (math.Pi / 180.0)
	lat2 := coord2.Latitude * (math.Pi / 180.0)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadius * c // Distance in kilometers
}

// Function to find the closest vehicle to the given coordinates
func closestVehicle(vehicles []vehicle, target location) *vehicle {
	if len(vehicles) == 0 {
		return nil // Return nil if the vehicle array is empty
	}
	if len(vehicles) == 1 {
		return &vehicles[0] // Return the only vehicle if there is only one
	}

	closest := &vehicles[0]
	closestDistance := haversine(closest.VehicleLocation, target)

	for _, v := range vehicles[1:] {
		distance := haversine(v.VehicleLocation, target)
		if distance < closestDistance {
			closest = &v
			closestDistance = distance
		}
	}

	return closest
}

func SearchStationCar(cityId CityId, currentCoordinate Coordinate, marginInKm float64, startDate time.Time, endDate time.Time, vehiculeType VehiculeType) int {
	responseChannel := make(chan int, 1)
	ctx, cancel := context.WithCancel(context.Background())
	nbCarFound := searchCar(SearchingStation, cityId, currentCoordinate, marginInKm, startDate, endDate, vehiculeType, responseChannel, ctx, cancel)
	cancel()

	return nbCarFound
}

// As soon as at least one car is found return the number of cars found
func SearchFlexCar(cityId CityId, currentCoordinate Coordinate, marginInKm float64) int {
	responseChannel := make(chan int, 1)
	ctx, cancel := context.WithCancel(context.Background())
	nbCarFound := searchCar(SearchingFlex, cityId, currentCoordinate, marginInKm, time.Time{}, time.Time{}, AllTypes, responseChannel, ctx, cancel)
	cancel()

	return nbCarFound
}

// This function is designed to be called as a goroutine. As soon as at least one car is found return the number of cars found. Or can be cancelled by the context
func SearchStationCarForGoRoutine(cityId CityId, currentCoordinate Coordinate, marginInKm float64, startDate time.Time, endDate time.Time, vehiculeType VehiculeType, responseChannel chan<- int, ctx context.Context, cancelCtxFunc context.CancelFunc) int {
	defer func() {
		if r := recover(); r != nil {
			responseChannel <- -1
			log.Printf("Pannic append : %s", r)
		}
	}()

	return searchCar(SearchingStation, cityId, currentCoordinate, marginInKm, startDate, endDate, vehiculeType, responseChannel, ctx, cancelCtxFunc)
}

// This function is designed to be called as a goroutine. As soon as at least one car is found return the number of cars found. Or can be cancelled by the context
func SearchFlexCarForGoRoutine(cityId CityId, currentCoordinate Coordinate, marginInKm float64, responseChannel chan<- int, ctx context.Context, cancelCtxFunc context.CancelFunc) int {
	defer func() {
		if r := recover(); r != nil {
			responseChannel <- -1
			log.Printf("Pannic append : %s", r)
		}
	}()

	return searchCar(SearchingFlex, cityId, currentCoordinate, marginInKm, time.Time{}, time.Time{}, AllTypes, responseChannel, ctx, cancelCtxFunc)
}

// Loop until a result is found. Return the number of cars found or can be cancelled by the context
func searchCar(searchingType SearchType, cityId CityId, currentCoordinate Coordinate, marginInKm float64, startDate time.Time, endDate time.Time, vehiculeType VehiculeType, responseChannel chan<- int, ctx context.Context, cancelCtxFunc context.CancelFunc) int {
	minCoordinate, maxCoordinate := currentCoordinate.ExpandCoordinate(marginInKm)

	var urlCalled string

	if searchingType == SearchingFlex {
		urlCalled = fmt.Sprintf("https://restapifrontoffice.reservauto.net/api/v2/Vehicle/FreeFloatingAvailability?CityId=%d&MaxLatitude=%f&MinLatitude=%f&MaxLongitude=%f&MinLongitude=%f", cityId, maxCoordinate.latitude, minCoordinate.latitude, maxCoordinate.longitude, minCoordinate.longitude)
	} else if searchingType == SearchingStation {
		startDateFormat := startDate.Format(dateFormat)
		endDataFormat := endDate.Format(dateFormat)

		urlCalled = fmt.Sprintf("https://restapifrontoffice.reservauto.net/api/v2/StationAvailability?CityId=%d&MaxLatitude=%f&MinLatitude=%f&MaxLongitude=%f&MinLongitude=%f&StartDate=%s&EndDate=%s", cityId, maxCoordinate.latitude, minCoordinate.latitude, maxCoordinate.longitude, minCoordinate.longitude, url.QueryEscape(startDateFormat), url.QueryEscape(endDataFormat))

		if vehiculeType != AllTypes {
			urlCalled += fmt.Sprintf("&VehicleTypes=%d", vehiculeType)
		}
	}

	msSecondeToSleep := 0

	for {
		select {
		case <-ctx.Done():
			responseChannel <- -1
			return -1
		default:

			if msSecondeToSleep > 0 {
				time.Sleep(time.Millisecond)
				msSecondeToSleep--
			} else {
				nbCarFound := 0

				var err error

				if searchingType == SearchingFlex {
					var flexAvailable flexCarResponse

					err = apiCall(urlCalled, &flexAvailable)

					// nbCarFound = flexAvailable.TotalNbVehicles
					if flexAvailable.TotalNbVehicles > 0 {
						// log.Printf("Car Coordinates : %f, %f", flexAvailable.Vehicles[0].VehicleLocation.Latitude, flexAvailable.Vehicles[0].VehicleLocation.Longitude)
						// nbCarFound = flexAvailable.Vehicles[0].VehicleId
						// Find the closest car
						closest := closestVehicle(flexAvailable.Vehicles, location{Latitude: currentCoordinate.latitude, Longitude: currentCoordinate.longitude})
						if closest != nil {
							nbCarFound = closest.VehicleId
						} else {
							log.Println("Failed to find closest vehicle, Falling back to the first one")
							nbCarFound = flexAvailable.Vehicles[0].VehicleId
						}
					}
				} else if searchingType == SearchingStation {
					var stationsAvailable stationsResponse

					err = apiCall(urlCalled, &stationsAvailable)

					for _, station := range stationsAvailable.Stations {
						if station.SatisfiesFilters && station.RecommendedVehicleId != nil {
							nbCarFound++
						}
					}
				}

				if err != nil {
					cancelCtxFunc()
				}

				if nbCarFound > 0 {
					responseChannel <- nbCarFound
					return nbCarFound
				}

				//msSecondeToSleep = fetchDelayInMin * 60 * 1000 // Wait only 1ms each time to don't block the for loop and be able to catch the cancel signal
				msSecondeToSleep = 1500 // Wait only 1ms each time to don't block the for loop and be able to catch the cancel signal
			}
		}
	}
}

// Make an api call at url passed and return the result in response object
func apiCall(url string, response interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {

		errDecode := json.NewDecoder(resp.Body).Decode(response)

		if errDecode != nil {
			log.Fatal(errDecode)
		}
	} else {

		errString := fmt.Sprintf("Error %d in API call", resp.StatusCode)
		err = errors.New(errString)

		log.Print(err)
	}

	return err
}
