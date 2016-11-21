var map;

function initBuoyMap() {
    map = new google.maps.Map($('#map')[0], {
        center: {lat: 39.8282, lng: -98.5795},
        zoom: 4,
        mapTypeId: google.maps.MapTypeId.HYBRID
    });

    $.ajax({
        url: '/api/stations',
        type: 'GET'
    }).done(function(data) {
        var buoyInfoPopup = new google.maps.InfoWindow();

        for (var i = 0; i < data.Stations.length; i++) {
            var station = data.Stations[i];

            // Ignore inactive stations
            if (station.Active === 'n') {
                continue;
            } else if (station.Type != 'buoy') {
                continue;
            }

            // Create the marker for the new buoy
            var buoyMarker = new google.maps.Marker({
                position: {lat: station.Latitude, lng: station.Longitude},
                map: map,
                title: station.LocationName,
            });

            buoyMarker.content = '<div><h5>' + station.LocationName + '</h5><p>Owned by: ' + station.Owner + '</p><p>' + station.PGM + '</p><a href="/buoy/' + station.StationID + '">View Buoy Data for Station ' + station.StationID + '</a></div>';

            google.maps.event.addListener(buoyMarker, 'click', function () {
                buoyInfoPopup.setContent(this.content);
                buoyInfoPopup.open(map, this);
            });
        }
    });
}