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
	router.HandleFunc("/api/stations", findAllStationsHandler)
	router.HandleFunc("/api/stationinfo/{station}", findStationInfoHandler)
	router.HandleFunc("/api/latest/wave/{lat}/{lon}", closestLatestWaveHandler)
	router.HandleFunc("/api/latest/weather/{lat}/{lon}", closestLatestWeatherHandler)
	router.HandleFunc("/api/latest/wave/{station}", latestWaveIDHandler)
	router.HandleFunc("/api/latest/weather/{station}", latestWeatherIDHandler)
	router.HandleFunc("/api/latest/{lat}/{lon}", closestLatestHandler)
	router.HandleFunc("/api/latest/{station}", latestIDHandler)
	router.HandleFunc("/api/date/wave/{lat}/{lon}/{epoch}", closestWaveDateHandler)
	router.HandleFunc("/api/date/weather/{lat}/{lon}/{epoch}", closestWeatherDateHandler)
	router.HandleFunc("/api/date/wave/{station}/{epoch}", dateWaveIDHandler)
	router.HandleFunc("/api/date/weather/{station}/{epoch}", dateWeatherIDHandler)

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

func findAllStationsHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	stationsResponse, _ := client.Get(surfnerd.ActiveBuoysURL)
	defer stationsResponse.Body.Close()

	stationsContents, _ := ioutil.ReadAll(stationsResponse.Body)
	stations := surfnerd.BuoyStations{}
	xml.Unmarshal(stationsContents, &stations)
	stationsJson, _ := stations.ToJSON()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(stationsJson)
}

func findStationInfoHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)
	stationID := vars["station"]

	requestedBuoy, requestedBuoyError := fetchBuoyWithID(client, stationID)
	if requestedBuoyError != nil {
		http.Error(w, requestedBuoyError.Error(), http.StatusInternalServerError)
		return
	}

	buoyJson, buoyJsonErr := requestedBuoy.ToJSON()
	if buoyJsonErr != nil {
		http.Error(w, buoyJsonErr.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(buoyJson)
}

func closestWaveDateHandler(w http.ResponseWriter, r *http.Request) {
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
	count := int(time.Since(requestedDate).Hours())
	fetchBuoyError := fetchDetailedWaveBuoyData(client, closestBuoy, count)
	if fetchBuoyError != nil {
		http.Error(w, fetchBuoyError.Error(), http.StatusInternalServerError)
		return
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

func closestWeatherDateHandler(w http.ResponseWriter, r *http.Request) {
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
	count := int(time.Since(requestedDate).Hours())
	fetchBuoyError := fetchStandardBuoyData(client, closestBuoy, count)
	if fetchBuoyError != nil {
		http.Error(w, fetchBuoyError.Error(), http.StatusInternalServerError)
		return
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

func closestLatestHandler(w http.ResponseWriter, r *http.Request) {
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

	closestBuoyData.WaveSummary.ConvertToMetricUnits()
	for index, _ := range closestBuoyData.SwellComponents {
		closestBuoyData.SwellComponents[index].ConvertToMetricUnits()
	}

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

func closestLatestWaveHandler(w http.ResponseWriter, r *http.Request) {
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
	fetchBuoyError := fetchDetailedWaveBuoyData(client, closestBuoy, 1)
	if fetchBuoyError != nil {
		http.Error(w, fetchBuoyError.Error(), http.StatusInternalServerError)
		return
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

func closestLatestWeatherHandler(w http.ResponseWriter, r *http.Request) {
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
	fetchBuoyError := fetchStandardBuoyData(client, closestBuoy, 1)
	if fetchBuoyError != nil {
		http.Error(w, fetchBuoyError.Error(), http.StatusInternalServerError)
		return
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

func latestIDHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)
	stationID := vars["station"]

	// Find the closest buoy
	requestedBuoy := &surfnerd.Buoy{StationID: stationID}

	// Get the buoy data
	buoyFetchError := fetchLatestBuoyData(client, requestedBuoy)
	if buoyFetchError != nil {
		http.Error(w, buoyFetchError.Error(), http.StatusInternalServerError)
		return
	}

	requestedDate := time.Now()
	requestedBuoyData, timeDiff := requestedBuoy.FindConditionsForDateAndTime(requestedDate)

	requestedBuoyData.WaveSummary.ConvertToMetricUnits()
	for index, _ := range requestedBuoyData.SwellComponents {
		requestedBuoyData.SwellComponents[index].ConvertToMetricUnits()
	}

	requestedBuoyContainer := ClosestBuoy{
		RequestedDate: requestedDate,
		TimeDiffFound: timeDiff,
		BuoyStationID: requestedBuoy.StationID,
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

func latestWaveIDHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)
	stationID := vars["station"]

	// Find the closest buoy
	requestedBuoy := &surfnerd.Buoy{StationID: stationID}

	// Get the buoy data
	buoyFetchError := fetchDetailedWaveBuoyData(client, requestedBuoy, 1)
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

func latestWeatherIDHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)
	stationID := vars["station"]

	// Find the closest buoy
	requestedBuoy := &surfnerd.Buoy{StationID: stationID}

	// Get the buoy data
	buoyFetchError := fetchStandardBuoyData(client, requestedBuoy, 1)
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

func dateWaveIDHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)

	// Grab the user vars
	stationID := vars["station"]
	rawdate, _ := strconv.ParseInt(vars["epoch"], 10, 64)

	requestedDate := time.Unix(rawdate, 0)

	// Create the requested buoy
	requestedBuoy := &surfnerd.Buoy{StationID: stationID}

	count := int(time.Since(requestedDate).Hours() * 2)
	fetchBuoyError := fetchDetailedWaveBuoyData(client, requestedBuoy, count)
	if fetchBuoyError != nil {
		http.Error(w, fetchBuoyError.Error(), http.StatusInternalServerError)
		return
	}

	requestedBuoyData, timeDiff := requestedBuoy.FindConditionsForDateAndTime(requestedDate)

	requestedBuoyContainer := ClosestBuoy{
		RequestedDate: requestedDate,
		TimeDiffFound: timeDiff,
		BuoyStationID: requestedBuoy.StationID,
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

func dateWeatherIDHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)

	// Grab the user vars
	stationID := vars["station"]
	rawdate, _ := strconv.ParseInt(vars["epoch"], 10, 64)

	requestedDate := time.Unix(rawdate, 0)

	// Create the requested buoy
	requestedBuoy := &surfnerd.Buoy{StationID: stationID}

	count := int(time.Since(requestedDate).Hours() * 2)
	fetchBuoyError := fetchStandardBuoyData(client, requestedBuoy, count)
	if fetchBuoyError != nil {
		http.Error(w, fetchBuoyError.Error(), http.StatusInternalServerError)
		return
	}

	requestedBuoyData, timeDiff := requestedBuoy.FindConditionsForDateAndTime(requestedDate)

	requestedBuoyContainer := ClosestBuoy{
		RequestedDate: requestedDate,
		TimeDiffFound: timeDiff,
		BuoyStationID: requestedBuoy.StationID,
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

func fetchStandardBuoyData(client *http.Client, buoy *surfnerd.Buoy, count int) error {
	buoyResponse, buoyError := client.Get(buoy.CreateStandardDataURL())
	if buoyError != nil {
		return buoyError
	}
	defer buoyResponse.Body.Close()

	buoyContents, _ := ioutil.ReadAll(buoyResponse.Body)
	rawBuoyData := strings.Fields(string(buoyContents))

	buoyParseError := buoy.ParseRawStandardData(rawBuoyData, count)
	if buoyParseError != nil {
		return buoyParseError
	}

	return nil
}

func fetchDetailedWaveBuoyData(client *http.Client, buoy *surfnerd.Buoy, count int) error {
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

	buoyParseError := buoy.ParseRawWaveSpectraData(rawAlphaData, rawEnergyData, count)
	if buoyParseError != nil {
		return buoyParseError
	}

	return nil
}
