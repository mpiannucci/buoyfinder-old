package buoyfinder

import (
	"encoding/xml"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"strconv"
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
	router.HandleFunc("/api/{lat}/{lon}/{epoch}", buoyHandler)

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

func buoyHandler(w http.ResponseWriter, r *http.Request) {
	ctxParent := appengine.NewContext(r)
	ctx, _ := context.WithTimeout(ctxParent, 20*time.Second)
	client := urlfetch.Client(ctx)

	vars := mux.Vars(r)

	// Grab the user vars
	latitude, _ := strconv.ParseFloat(vars["lat"], 64)
	longitude, _ := strconv.ParseFloat(vars["lon"], 64)
	rawdate, _ := strconv.ParseInt(vars["epoch"], 10, 64)

	// TODO: Find the closest buoy
	stationsResp, stationsErr := client.Get(surfnerd.ActiveBuoysURL)
	if stationsErr != nil {
		http.Error(w, stationsErr.Error(), http.StatusInternalServerError)
		return
	}
	defer stationsResp.Body.Close()

	stationsContents, _ := ioutil.ReadAll(stationsResp.Body)
	stations := surfnerd.BuoyStations{}
	xml.Unmarshal(stationsContents, &stations)

	closestBuoy := stations.FindClosestActiveBuoy(surfnerd.NewLocationForLatLong(latitude, longitude))

	// TODO: Get the closest buoy reading

	// For now just print out the results
	fmt.Fprintf(w, "Lat: %v\nLon: %v\nDate: %v\n", latitude, 360-longitude, time.Unix(rawdate, 0))
	fmt.Fprintf(w, "ClosestStation: %v", closestBuoy.StationID)
}
