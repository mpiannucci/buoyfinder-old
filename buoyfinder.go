package buoyfinder

import (
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"html/template"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mpiannucci/surfnerd"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/urlfetch"
)

var funcMap = template.FuncMap{
	"ToFixedPoint": ToFixedPoint,
}

var indexTemplate = template.Must(template.New("base.html").Funcs(nil).ParseFiles("templates/base.html", "templates/index.html"))
var buoyTemplate = template.Must(template.New("base.html").Funcs(funcMap).ParseFiles("templates/base.html", "templates/buoy.html"))
var apiDocTemplate = template.Must(template.New("base.html").Funcs(nil).ParseFiles("templates/base.html", "templates/apidoc.html"))

func init() {
	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/", indexHandler)

	// API
	router.HandleFunc("/api", apiDocHandler)
	router.HandleFunc("/api/stations", findAllStationsHandler)
	router.HandleFunc("/api/stationinfo/{station}", findStationInfoHandler)
	router.HandleFunc("/api/latest/wave/charts{lat}/{lon}", closestLatestWaveChartsHandler)
	router.HandleFunc("/api/latest/wave/charts/{station}", latestWaveIDChartsHandler)
	router.HandleFunc("/api/latest/wave/{lat}/{lon}", closestLatestWaveHandler)
	router.HandleFunc("/api/latest/weather/{lat}/{lon}", closestLatestWeatherHandler)
	router.HandleFunc("/api/latest/wave/{station}", latestWaveIDHandler)
	router.HandleFunc("/api/latest/weather/{station}", latestWeatherIDHandler)
	router.HandleFunc("/api/latest/{lat}/{lon}", closestLatestHandler)
	router.HandleFunc("/api/latest/{station}", latestIDHandler)
	router.HandleFunc("/api/date/wave/charts/{lat}/{lon}/{epoch}", closestWaveChartsDateHandler)
	router.HandleFunc("/api/date/wave/charts/{station}/{epoch}", dateWaveIDChartsHandler)
	router.HandleFunc("/api/date/wave/{lat}/{lon}/{epoch}", closestWaveDateHandler)
	router.HandleFunc("/api/date/weather/{lat}/{lon}/{epoch}", closestWeatherDateHandler)
	router.HandleFunc("/api/date/wave/{station}/{epoch}", dateWaveIDHandler)
	router.HandleFunc("/api/date/weather/{station}/{epoch}", dateWeatherIDHandler)

	// Buoy Web Views
	router.HandleFunc("/buoy/{station}", buoyViewHandler)

	http.Handle("/", router)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if err := indexTemplate.Execute(w, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func buoyViewHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)

	// Grab the user vars
	stationID := vars["station"]
	requestedDate := time.Now()

	// Create the requested buoy
	requestedBuoy := &surfnerd.Buoy{StationID: stationID}

	count := int(time.Since(requestedDate).Hours()*2) + 1
	fetchBuoyError := fetchDetailedWaveBuoyData(client, requestedBuoy, count)
	if fetchBuoyError != nil {
		http.Error(w, fetchBuoyError.Error(), http.StatusInternalServerError)
		return
	}

	requestedBuoyData, timeDiff := requestedBuoy.FindConditionsForDateAndTime(requestedDate)

	directionalPlot, directionalError := fetchDirectionalSpectraChart(client, stationID, requestedBuoyData)
	if directionalError != nil {
		directionalPlot = ""
	}

	spectraPlot, spectraError := fetchSpectraDistributionChart(client, stationID, requestedBuoyData)
	if spectraError != nil {
		spectraPlot = ""
	}

	// For now convert the swell to feet
	requestedBuoyData.WaveSummary.ChangeUnits(surfnerd.English)
	for i, _ := range requestedBuoyData.SwellComponents {
		requestedBuoyData.SwellComponents[i].ChangeUnits(surfnerd.English)
	}

	requestedBuoyContainer := ClosestBuoy{
		RequestedDate:           requestedDate,
		TimeDiffFound:           timeDiff,
		BuoyStationID:           requestedBuoy.StationID,
		BuoyData:                requestedBuoyData,
		DirectionalSpectraPlot:  directionalPlot,
		SpectraDistributionPlot: spectraPlot,
	}

	if err := buoyTemplate.Execute(w, requestedBuoyContainer); err != nil {
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

func closestWaveChartsDateHandler(w http.ResponseWriter, r *http.Request) {
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

	directionalPlot, directionalError := fetchDirectionalSpectraChart(client, closestBuoy.StationID, closestBuoyData)
	if directionalError != nil {
		directionalPlot = ""
	}

	spectraPlot, spectraError := fetchSpectraDistributionChart(client, closestBuoy.StationID, closestBuoyData)
	if spectraError != nil {
		spectraPlot = ""
	}

	closestBuoyContainer := ClosestBuoy{
		RequestedLocation:       requestedLocation,
		RequestedDate:           requestedDate,
		TimeDiffFound:           timeDiff,
		BuoyStationID:           closestBuoy.StationID,
		BuoyLocation:            *closestBuoy.Location,
		BuoyData:                closestBuoyData,
		DirectionalSpectraPlot:  directionalPlot,
		SpectraDistributionPlot: spectraPlot,
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

	closestBuoyData.WaveSummary.ChangeUnits(surfnerd.Metric)
	for index, _ := range closestBuoyData.SwellComponents {
		closestBuoyData.SwellComponents[index].ChangeUnits(surfnerd.Metric)
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

func closestLatestWaveChartsHandler(w http.ResponseWriter, r *http.Request) {
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

	directionalPlot, directionalError := fetchDirectionalSpectraChart(client, closestBuoy.StationID, closestBuoyData)
	if directionalError != nil {
		directionalPlot = ""
	}

	spectraPlot, spectraError := fetchSpectraDistributionChart(client, closestBuoy.StationID, closestBuoyData)
	if spectraError != nil {
		spectraPlot = ""
	}

	closestBuoyContainer := ClosestBuoy{
		RequestedLocation:       requestedLocation,
		RequestedDate:           requestedDate,
		TimeDiffFound:           timeDiff,
		BuoyStationID:           closestBuoy.StationID,
		BuoyLocation:            *closestBuoy.Location,
		BuoyData:                closestBuoyData,
		DirectionalSpectraPlot:  directionalPlot,
		SpectraDistributionPlot: spectraPlot,
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

	requestedBuoyData.WaveSummary.ChangeUnits(surfnerd.Metric)
	for index, _ := range requestedBuoyData.SwellComponents {
		requestedBuoyData.SwellComponents[index].ChangeUnits(surfnerd.Metric)
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

func latestWaveIDChartsHandler(w http.ResponseWriter, r *http.Request) {
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

	directionalPlot, directionalError := fetchDirectionalSpectraChart(client, stationID, requestedBuoyData)
	if directionalError != nil {
		directionalPlot = ""
	}

	spectraPlot, spectraError := fetchSpectraDistributionChart(client, stationID, requestedBuoyData)
	if spectraError != nil {
		spectraPlot = ""
	}

	requestedBuoyContainer := ClosestBuoy{
		RequestedDate:           requestedDate,
		TimeDiffFound:           timeDiff,
		BuoyStationID:           requestedBuoy.StationID,
		BuoyData:                requestedBuoyData,
		DirectionalSpectraPlot:  directionalPlot,
		SpectraDistributionPlot: spectraPlot,
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

func dateWaveIDChartsHandler(w http.ResponseWriter, r *http.Request) {
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

	directionalPlot, directionalError := fetchDirectionalSpectraChart(client, stationID, requestedBuoyData)
	if directionalError != nil {
		directionalPlot = ""
	}

	spectraPlot, spectraError := fetchSpectraDistributionChart(client, stationID, requestedBuoyData)
	if spectraError != nil {
		spectraPlot = ""
	}

	requestedBuoyContainer := ClosestBuoy{
		RequestedDate:           requestedDate,
		TimeDiffFound:           timeDiff,
		BuoyStationID:           requestedBuoy.StationID,
		BuoyData:                requestedBuoyData,
		DirectionalSpectraPlot:  directionalPlot,
		SpectraDistributionPlot: spectraPlot,
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

func fetchDirectionalSpectraChart(client *http.Client, stationID string, buoyData surfnerd.BuoyDataItem) (string, error) {
	values := "["
	for index, energy := range buoyData.WaveSpectra.Energies {
		if index > 0 {
			values += ","
		}
		values += "[" + strconv.FormatFloat(buoyData.WaveSpectra.Angles[index], 'f', 2, 64) + "," + strconv.FormatFloat(energy, 'f', 2, 64) + "]"
	}
	values += "]"

	buoyTime := buoyData.Date.Format("01/02/2006 15:04 UTC")

	exportURL := "http://export.highcharts.com"
	data := url.Values{}
	data.Set("content", "options")
	data.Set("options", "{chart: {polar: true, type: 'column', spacing: [0, 0, 0, 0], margin: [20, 0, 0, 0], width: 600, height: 600}, title: {text: 'Station "+stationID+": Directional Wave Spectra', style: {font: '10px Helvetica, sans-serif'}}, subtitle: {text: 'Valid "+buoyTime+"', style: {font: '8px Helvetica, sans-serif'}}, legend: {enabled: false}, credits: {enabled: false}, pane: {startAngle: 0, endAngle: 360}, xAxis: {labels: {style: {fontWeight: 'bold', fontSize: '13px'}}, gridLineWidth: 1, tickmarkPlacement: 'on', tickInterval: 45, min: 0, max: 360, minPadding: 0, maxPadding: 0}, yAxis: {labels: {style: {fontWeight: 'bold', fontSize: '13px'}}, gridLineWidth: 1, min: 0, endOnTick: true, showLastLabel: true, title: {useHTML: true, text: 'Energy (m<sup>2</sup>/Hz)'}, labels: {formatter: function(){return this.value}}, reversedStacks: false}, plotOptions: {series: {stacking: null, shadow: false, groupPadding: 0, pointPlacement: 'on', pointWidth: 0.6}}, series: [{type: 'column', name: 'Energy', data: "+values+", pointPlacement: 'on', colorByPoint: true, }]};")
	data.Set("scale", "3")
	data.Set("type", "image/png")
	data.Set("constr", "Chart")

	resp, err := client.PostForm(exportURL, data)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	rawChart, err := ioutil.ReadAll(resp.Body)
	encodedChart := base64.StdEncoding.EncodeToString(rawChart)
	return encodedChart, err
}

func fetchSpectraDistributionChart(client *http.Client, stationID string, buoyData surfnerd.BuoyDataItem) (string, error) {
	values := "["
	for index, freq := range buoyData.WaveSpectra.Frequencies {
		if index > 0 {
			values += ","
		}
		values += "[" + strconv.FormatFloat(1.0/freq, 'f', 2, 64) + "," + strconv.FormatFloat(buoyData.WaveSpectra.Energies[index], 'f', 2, 64) + "]"
	}
	values += "]"

	buoyTime := buoyData.Date.Format("01/02/2006 15:04 UTC")

	exportURL := "http://export.highcharts.com"
	data := url.Values{}
	data.Set("content", "options")
	data.Set("options", "{chart: {type: 'line'}, title: {text: 'Station "+stationID+": Wave Spectra', style: {font: '10px Helvetica, sans-serif'}}, subtitle: {text: 'Valid "+buoyTime+"', style: {font: '8px Helvetica, sans-serif'}}, legend: {enabled: false}, credits: {enabled: false}, xAxis: {labels: {style: {fontWeight: 'bold', fontSize: '13px'}}, min: 0, max: 20, title: {text: 'Period (s)'}, gridLineWidth: 1, tickmarkPlacement: 'on', minPadding: 0, maxPadding: 0}, yAxis: {labels: {style: {fontWeight: 'bold', fontSize: '13px'}}, gridLineWidth: 1, min: 0, endOnTick: true, showLastLabel: true, title: {useHTML: true, text: 'Energy (m<sup>2</sup>/Hz)'}, labels: {formatter: function(){return this.value}}, reversedStacks: false}, plotOptions: {series: {stacking: null, shadow: false, groupPadding: 0}}, series: [{type: 'line', name: 'Energy', data: "+values+"}]};")
	data.Set("scale", "3")
	data.Set("type", "image/png")
	data.Set("constr", "Chart")

	resp, err := client.PostForm(exportURL, data)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	rawChart, err := ioutil.ReadAll(resp.Body)
	encodedChart := base64.StdEncoding.EncodeToString(rawChart)
	return encodedChart, err
}

func round(num float64) int {
	return int(num + math.Copysign(0.5, num))
}

func ToFixedPoint(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(round(num*output)) / output
}
