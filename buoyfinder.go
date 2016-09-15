package buoyfinder

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"html/template"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mpiannucci/surfnerd"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/urlfetch"
)

var indexTemplate = template.Must(template.New("base.html").Funcs(nil).ParseFiles("templates/base.html", "templates/index.html"))
var apiDocTemplate = template.Must(template.New("base.html").Funcs(nil).ParseFiles("templates/base.html", "templates/apidoc.html"))

func init() {
	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/", indexHandler)
	router.HandleFunc("/api", apiDocHandler)
	router.HandleFunc("/api/date/{lat}/{lon}/{epoch}", closestBuoyDateHandler)
	router.HandleFunc("/api/latest/{lat}/{lon}", closestBuoyLatestHandler)
	router.HandleFunc("/api/latest/{station}", latestForIDHandler)
	router.HandleFunc("/api/date/{station}/{epoch}", dateBuoyIDHandler)
	router.HandleFunc("/api/latestenergy/{station}", latestEnergyIDHandler)

	http.Handle("/", router)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if err := indexTemplate.Execute(w, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func apiDocHandler(w http.ResponseWriter, r *http.Request) {
	if err := apiDocTemplate.Execute(w, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func closestBuoyDateHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)

	// Grab the user vars
	latitude, _ := strconv.ParseFloat(vars["lat"], 64)
	longitude, _ := strconv.ParseFloat(vars["lon"], 64)
	rawdate, _ := strconv.ParseInt(vars["epoch"], 10, 64)

	requestedLocation := surfnerd.NewLocationForLatLong(latitude, longitude)
	requestedDate := time.Unix(rawdate, 0)

	// Find the closest buoy
	closestBuoy, closestError := fetchClosestBuoy(client, requestedLocation)
	if closestError != nil {
		http.Error(w, closestError.Error(), http.StatusInternalServerError)
		return
	}

	// Get the buoy data
	if time.Since(requestedDate).Hours() < 1.0 {
		fetchBuoyError := fetchLatestBuoyData(client, closestBuoy)
		if fetchBuoyError != nil {
			http.Error(w, fetchBuoyError.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		fetchBuoyError := fetchDetailedWaveBuoyData(client, closestBuoy)
		if fetchBuoyError != nil {
			http.Error(w, fetchBuoyError.Error(), http.StatusInternalServerError)
			return
		}
	}

	closestBuoyData, timeDiff := closestBuoy.FindConditionsForDateAndTime(requestedDate)

	closestBuoyContainer := ClosestBuoy{
		RequestedLocation: requestedLocation,
		RequestedDate:     requestedDate,
		TimeDiffFound:     timeDiff,
		BuoyStationID:     closestBuoy.StationID,
		BuoyLocation:      *closestBuoy.Location,
		BuoyData:          closestBuoyData,
	}

	closestBuoyJson, closestBuoyJsonErr := json.MarshalIndent(&closestBuoyContainer, "", "    ")
	if closestBuoyJsonErr != nil {
		http.Error(w, closestBuoyJsonErr.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(closestBuoyJson)
}

func closestBuoyLatestHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)

	// Grab the user vars
	latitude, _ := strconv.ParseFloat(vars["lat"], 64)
	longitude, _ := strconv.ParseFloat(vars["lon"], 64)
	requestedLocation := surfnerd.NewLocationForLatLong(latitude, longitude)

	// Find the closest buoy
	closestBuoy, closestError := fetchClosestBuoy(client, requestedLocation)
	if closestError != nil {
		http.Error(w, closestError.Error(), http.StatusInternalServerError)
		return
	}

	// Get the buoy data
	buoyFetchError := fetchLatestBuoyData(client, closestBuoy)
	if buoyFetchError != nil {
		http.Error(w, buoyFetchError.Error(), http.StatusInternalServerError)
		return
	}

	requestedDate := time.Now()
	closestBuoyData, timeDiff := closestBuoy.FindConditionsForDateAndTime(requestedDate)

	closestBuoyContainer := ClosestBuoy{
		RequestedLocation: requestedLocation,
		RequestedDate:     requestedDate,
		TimeDiffFound:     timeDiff,
		BuoyStationID:     closestBuoy.StationID,
		BuoyLocation:      *closestBuoy.Location,
		BuoyData:          closestBuoyData,
	}

	closestBuoyJson, closestBuoyJsonErr := json.MarshalIndent(&closestBuoyContainer, "", "    ")
	if closestBuoyJsonErr != nil {
		http.Error(w, closestBuoyJsonErr.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(closestBuoyJson)
}

func latestForIDHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)

	stationID := vars["station"]

	// Find the closest buoy
	requestedBuoy, requestedError := fetchBuoyWithID(client, stationID)
	if requestedError != nil {
		http.Error(w, requestedError.Error(), http.StatusInternalServerError)
		return
	}

	// Get the buoy data
	buoyFetchError := fetchLatestBuoyData(client, requestedBuoy)
	if buoyFetchError != nil {
		http.Error(w, buoyFetchError.Error(), http.StatusInternalServerError)
		return
	}

	requestedDate := time.Now()
	requestedBuoyData, timeDiff := requestedBuoy.FindConditionsForDateAndTime(requestedDate)

	requestedBuoyContainer := ClosestBuoy{
		RequestedDate: requestedDate,
		TimeDiffFound: timeDiff,
		BuoyStationID: requestedBuoy.StationID,
		BuoyLocation:  *requestedBuoy.Location,
		BuoyData:      requestedBuoyData,
	}

	requestedBuoyJson, requestedBuoyJsonErr := json.MarshalIndent(&requestedBuoyContainer, "", "    ")
	if requestedBuoyJsonErr != nil {
		http.Error(w, requestedBuoyJsonErr.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(requestedBuoyJson)
}

func latestEnergyIDHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)

	stationID := vars["station"]

	// Find the closest buoy
	requestedBuoy, requestedError := fetchBuoyWithID(client, stationID)
	if requestedError != nil {
		http.Error(w, requestedError.Error(), http.StatusInternalServerError)
		return
	}

	// Get the buoy data
	buoyFetchError := fetchRawSpectraBuoyData(client, requestedBuoy)
	if buoyFetchError != nil {
		http.Error(w, buoyFetchError.Error(), http.StatusInternalServerError)
		return
	}

	requestedDate := time.Now()
	requestedBuoyData, timeDiff := requestedBuoy.FindWaveSpectraForDateAndTime(requestedDate)

	requestedBuoyContainer := ClosestBuoy{
		RequestedDate: requestedDate,
		TimeDiffFound: timeDiff,
		BuoyStationID: requestedBuoy.StationID,
		BuoyLocation:  *requestedBuoy.Location,
		WaveSpectra:   requestedBuoyData,
	}

	requestedBuoyJson, requestedBuoyJsonErr := json.MarshalIndent(&requestedBuoyContainer, "", "    ")
	if requestedBuoyJsonErr != nil {
		http.Error(w, requestedBuoyJsonErr.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(requestedBuoyJson)
}

func dateBuoyIDHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)

	// Grab the user vars
	stationID := vars["station"]
	rawdate, _ := strconv.ParseInt(vars["epoch"], 10, 64)

	requestedDate := time.Unix(rawdate, 0)

	// Find the closest buoy
	requestedBuoy, requestedError := fetchBuoyWithID(client, stationID)
	if requestedError != nil {
		http.Error(w, requestedError.Error(), http.StatusInternalServerError)
		return
	}

	// Get the buoy data
	if time.Since(requestedDate).Hours() < 1.0 {
		fetchBuoyError := fetchLatestBuoyData(client, requestedBuoy)
		if fetchBuoyError != nil {
			http.Error(w, fetchBuoyError.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		fetchBuoyError := fetchDetailedWaveBuoyData(client, requestedBuoy)
		if fetchBuoyError != nil {
			http.Error(w, fetchBuoyError.Error(), http.StatusInternalServerError)
			return
		}
	}

	requestedBuoyData, timeDiff := requestedBuoy.FindConditionsForDateAndTime(requestedDate)

	requestedBuoyContainer := ClosestBuoy{
		RequestedDate: requestedDate,
		TimeDiffFound: timeDiff,
		BuoyStationID: requestedBuoy.StationID,
		BuoyLocation:  *requestedBuoy.Location,
		BuoyData:      requestedBuoyData,
	}

	requestedBuoyJson, requestedBuoyJsonErr := json.MarshalIndent(&requestedBuoyContainer, "", "    ")
	if requestedBuoyJsonErr != nil {
		http.Error(w, requestedBuoyJsonErr.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(requestedBuoyJson)
}

func fetchBuoyWithID(client *http.Client, stationID string) (*surfnerd.Buoy, error) {
	stationsResponse, stationsError := client.Get(surfnerd.ActiveBuoysURL)
	if stationsError != nil {
		return nil, stationsError
	}
	defer stationsResponse.Body.Close()

	stationsContents, _ := ioutil.ReadAll(stationsResponse.Body)
	stations := surfnerd.BuoyStations{}
	xml.Unmarshal(stationsContents, &stations)

	requestedBuoy := stations.FindBuoyByID(stationID)
	if requestedBuoy == nil {
		return nil, errors.New("Could not find the requested buoy")
	}

	return requestedBuoy, nil
}

func fetchClosestBuoy(client *http.Client, requestedLocation surfnerd.Location) (*surfnerd.Buoy, error) {
	stationsResponse, stationsError := client.Get(surfnerd.ActiveBuoysURL)
	if stationsError != nil {
		return nil, stationsError
	}
	defer stationsResponse.Body.Close()

	stationsContents, _ := ioutil.ReadAll(stationsResponse.Body)
	stations := surfnerd.BuoyStations{}
	xml.Unmarshal(stationsContents, &stations)

	closestBuoy := stations.FindClosestActiveWaveBuoy(requestedLocation)
	if closestBuoy == nil {
		return nil, errors.New("Could not find the closest buoy")
	}

	return closestBuoy, nil
}

func fetchLatestBuoyData(client *http.Client, buoy *surfnerd.Buoy) error {
	buoyResponse, buoyError := client.Get(buoy.CreateLatestReadingURL())
	if buoyError != nil {
		return buoyError
	}
	defer buoyResponse.Body.Close()

	buoyContents, _ := ioutil.ReadAll(buoyResponse.Body)
	rawBuoyData := string(buoyContents[:])

	buoyParseError := buoy.ParseRawLatestBuoyData(rawBuoyData)
	if buoyParseError != nil {
		return buoyParseError
	}

	return nil
}

func fetchStandardBuoyData(client *http.Client, buoy *surfnerd.Buoy) error {
	buoyResponse, buoyError := client.Get(buoy.CreateStandardDataURL())
	if buoyError != nil {
		return buoyError
	}
	defer buoyResponse.Body.Close()

	buoyContents, _ := ioutil.ReadAll(buoyResponse.Body)
	rawBuoyData := strings.Fields(string(buoyContents))

	buoyParseError := buoy.ParseRawStandardData(rawBuoyData, 100000000)
	if buoyParseError != nil {
		return buoyParseError
	}

	return nil
}

func fetchDetailedWaveBuoyData(client *http.Client, buoy *surfnerd.Buoy) error {
	buoyResponse, buoyError := client.Get(buoy.CreateDetailedWaveDataURL())
	if buoyError != nil {
		return buoyError
	}
	defer buoyResponse.Body.Close()

	buoyContents, _ := ioutil.ReadAll(buoyResponse.Body)
	rawBuoyData := strings.Fields(string(buoyContents))

	buoyParseError := buoy.ParseRawDetailedWaveData(rawBuoyData, 100000000)
	if buoyParseError != nil {
		return buoyParseError
	}

	return nil
}

func fetchRawSpectraBuoyData(client *http.Client, buoy *surfnerd.Buoy) error {
	directionalResponse, directionalError := client.Get(buoy.CreateDirectionalSpectraDataURL())
	if directionalError != nil {
		return directionalError
	}
	defer directionalResponse.Body.Close()
	directionalContents, _ := ioutil.ReadAll(directionalResponse.Body)
	rawAlphaData := strings.Split(string(directionalContents), "\n")

	energyResponse, energyError := client.Get(buoy.CreateEnergySpectraDataURL())
	if energyError != nil {
		return energyError
	}
	defer energyResponse.Body.Close()
	energyContents, _ := ioutil.ReadAll(energyResponse.Body)
	rawEnergyData := strings.Split(string(energyContents), "\n")

	buoyParseError := buoy.ParseRawWaveSpectraData(rawAlphaData, rawEnergyData, 1)
	if buoyParseError != nil {
		return buoyParseError
	}

	return nil
}
