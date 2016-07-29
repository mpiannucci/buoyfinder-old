package buoyfinder

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/gorilla/mux"
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
	vars := mux.Vars(r)

	// Grab the user vars
	latitude := vars["lat"]
	longitude := vars["lon"]
	rawdate := vars["epoch"]

	fmt.Fprintf(w, "Lat: %v\nLon: %v\nEpoch: %v", latitude, longitude, rawdate)
}
