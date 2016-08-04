package buoyfinder

import (
	"encoding/json"
	"encoding/xml"
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

type ClosestBuoy struct {
	RequestedLocation surfnerd.Location
	RequestedDate     time.Time
	TimeDiffFound     time.Duration
	BuoyStationID     string
	BuoyLocation      surfnerd.Location
	BuoyData          surfnerd.BuoyItem
}

var indexTemplate = template.Must(template.New("base.html").Funcs(nil).ParseFiles("templates/base.html", "templates/index.html"))
var apiDocTemplate = template.Must(template.New("base.html").Funcs(nil).ParseFiles("templates/base.html", "templates/apidoc.html"))

func init() {
	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/", indexHandler)
	router.HandleFunc("/api", apiDocHandler)
	router.HandleFunc("/api/{lat}/{lon}/{epoch}", closestBuoyDateHandler)
	router.HandleFunc("/api/{lat}/{lon}", closestBuoyLatestHandler)

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

	// Find the closest buoy
	stationsResp, stationsErr := client.Get(surfnerd.ActiveBuoysURL)
	if stationsErr != nil {
		http.Error(w, stationsErr.Error(), http.StatusInternalServerError)
		return
	}
	defer stationsResp.Body.Close()

	stationsContents, _ := ioutil.ReadAll(stationsResp.Body)
	stations := surfnerd.BuoyStations{}
	xml.Unmarshal(stationsContents, &stations)

	requestedLocation := surfnerd.NewLocationForLatLong(latitude, longitude)
	closestBuoy := stations.FindClosestActiveWaveBuoy(requestedLocation)
	if closestBuoy == nil {
		http.Error(w, "Could not find the closest buoy", http.StatusInternalServerError)
		return
	}

	// Get the buoy data
	requestedDate := time.Unix(rawdate, 0)
	if time.Since(requestedDate).Hours() < 1.0 {
		buoyResp, buoyErr := client.Get(closestBuoy.CreateLatestReadingURL())
		if buoyErr != nil {
			http.Error(w, buoyErr.Error(), http.StatusInternalServerError)
			return
		}
		defer buoyResp.Body.Close()

		buoyContents, _ := ioutil.ReadAll(buoyResp.Body)
		rawBuoyData := string(buoyContents[:])

		buoyParseError := closestBuoy.ParseRawLatestBuoyData(rawBuoyData)
		if buoyParseError != nil {
			http.Error(w, buoyParseError.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		buoyResp, buoyErr := client.Get(closestBuoy.CreateDetailedWaveDataURL())
		if buoyErr != nil {
			http.Error(w, buoyErr.Error(), http.StatusInternalServerError)
			return
		}
		defer buoyResp.Body.Close()

		buoyContents, _ := ioutil.ReadAll(buoyResp.Body)
		rawBuoyData := strings.Fields(string(buoyContents))

		buoyParseError := closestBuoy.ParseRawDetailedWaveData(rawBuoyData, 100000000)
		if buoyParseError != nil {
			http.Error(w, buoyParseError.Error(), http.StatusInternalServerError)
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

	// Find the closest buoy
	stationsResp, stationsErr := client.Get(surfnerd.ActiveBuoysURL)
	if stationsErr != nil {
		http.Error(w, stationsErr.Error(), http.StatusInternalServerError)
		return
	}
	defer stationsResp.Body.Close()

	stationsContents, _ := ioutil.ReadAll(stationsResp.Body)
	stations := surfnerd.BuoyStations{}
	xml.Unmarshal(stationsContents, &stations)

	requestedLocation := surfnerd.NewLocationForLatLong(latitude, longitude)
	closestBuoy := stations.FindClosestActiveWaveBuoy(requestedLocation)
	if closestBuoy == nil {
		http.Error(w, "Could not find the closest buoy", http.StatusInternalServerError)
		return
	}

	// Get the buoy data
	requestedDate := time.Now()
	buoyResp, buoyErr := client.Get(closestBuoy.CreateLatestReadingURL())
	if buoyErr != nil {
		http.Error(w, buoyErr.Error(), http.StatusInternalServerError)
		return
	}
	defer buoyResp.Body.Close()

	buoyContents, _ := ioutil.ReadAll(buoyResp.Body)
	rawBuoyData := string(buoyContents[:])

	buoyParseError := closestBuoy.ParseRawLatestBuoyData(rawBuoyData)
	if buoyParseError != nil {
		http.Error(w, buoyParseError.Error(), http.StatusInternalServerError)
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
	w.Write(closestBuoyJson)
}
