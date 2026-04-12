package stationcache

import "time"

// StationStore is the read interface consumed by the API handler.
// MemCache implements it for RF-scale traffic; a future DB-backed
// implementation can serve full APRS-IS feeds behind this same interface.
type StationStore interface {
	// QueryBBox returns stations within the bounding box heard within maxAge.
	// Returned slice is a snapshot — caller owns it.
	QueryBBox(bbox BBox, maxAge time.Duration) []Station

	// Lookup returns positions for the given callsigns regardless of bbox.
	// Used for server-side digi path resolution. Missing callsigns omitted.
	Lookup(callsigns []string) map[string]LatLon
}

type BBox struct {
	SwLat, SwLon, NeLat, NeLon float64
}

type LatLon struct {
	Lat, Lon float64
}

// Station is the last-known state of a single station or object.
// One entry per composite key. Position history is kept for moving
// stations only — static stations that beacon the same coordinates
// repeatedly don't accumulate trail points.
type Station struct {
	Key       string     // "stn:W1ABC-9" or "obj:SHELTER1"
	Callsign  string     // display name (callsign or object/item name)
	IsObject  bool       // true for APRS objects/items
	Positions []Position // newest first; cap at MaxTrailLen. Static stations have len 1.
	Symbol    [2]byte    // [table, code]
	Via       string     // "rf" or "is"
	Path      []string   // digi path from AX.25 header
	Hops      int        // count of H-bit digi addresses
	Direction string     // "RX", "TX", "IS"
	Channel   uint32
	Comment   string
	Weather   *Weather // nil if not a weather station
	LastHeard time.Time
}

// Position is a single position fix with metadata.
type Position struct {
	Lat       float64
	Lon       float64
	Alt       float64
	HasAlt    bool
	Speed     float64 // knots
	Course    int
	HasCourse bool
	Timestamp time.Time
}

const MaxTrailLen = 200

type Weather struct {
	Temp       float64
	HasTemp    bool
	WindSpeed  float64
	HasWindSpeed bool
	WindDir    int
	HasWindDir bool
	WindGust   float64
	HasWindGust bool
	Humidity   int
	HasHumidity bool
	Pressure   float64
	HasPressure bool
	Rain1h     float64
	HasRain1h  bool
	Rain24h    float64
	HasRain24h bool
	Snow24h    float64
	HasSnow24h bool
	Luminosity int
	HasLuminosity bool
}
