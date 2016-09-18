package buoyfinder

import (
	"time"

	"github.com/mpiannucci/surfnerd"
)

type ClosestBuoy struct {
	RequestedLocation surfnerd.Location
	RequestedDate     time.Time
	TimeDiffFound     time.Duration
	BuoyStationID     string
	BuoyLocation      surfnerd.Location
	BuoyData          surfnerd.BuoyDataItem
}
