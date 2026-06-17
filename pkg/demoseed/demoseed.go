// Package demoseed holds canned data used by graywolf's -demo mode.
// It exists so a fresh launch can look like a busy real Salt Lake-metro
// APRS station for screenshots and demo recordings, without depending on
// a real device DB or live RF. All data here is public APRS information
// (callsigns + positions broadcast over the air), captured from a real
// tablet and filtered to a Salt Lake bounding box.
package demoseed

import (
	"fmt"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/stationcache"
)

// station is the compact internal form of a canned demo station; Stations()
// expands it into a stationcache.CacheEntry with a fresh timestamp.
type station struct {
	callsign string
	lat, lon float64
	symbol   [2]byte
	isObject bool
	speed    float64 // knots; 0 for fixed
	course   int     // degrees; 0 when not moving
	comment  string
}

// objectSources maps a demo object's name to the station that originated
// it, so the Live Map popup can show the true author rather than implying
// the object came from the home station (GH #323). Regular stations are
// their own source and are absent here.
var objectSources = map[string]string{
	"446.25HRM": "N7MVC",
}

// slcStations is the curated set captured from a real tablet RF session,
// filtered to lat 40.4-41.0 / lon -112.3 to -111.6. NW5W-8 is the home
// station running graywolf.
var slcStations = []station{
	{"446.25HRM", 40.51717, -112.01383, [2]byte{0x2F, 0x72}, true, 0, 0, "T118 -5 NetSun9pm n7mvc.org"},
	{"AC7H-2", 40.83733, -111.87767, [2]byte{0x2F, 0x3E}, false, 0, 327, ""},
	{"AG7LY-7", 40.76183, -112.02483, [2]byte{0x2F, 0x6B}, false, 0, 1, ""},
	{"K7SRB", 40.63417, -111.90867, [2]byte{0x5C, 0x4B}, false, 0, 0, ""},
	{"K7TX-10", 40.42350, -111.83450, [2]byte{0x53, 0x23}, false, 0, 0, "APRS Digi & iGate"},
	{"K7UHP", 40.92200, -111.89433, [2]byte{0x2F, 0x5F}, false, 0, 0, "K7UHP Centerville Shop"},
	{"KB0STG-9", 40.49117, -111.99150, [2]byte{0x2F, 0x39}, false, 15, 181, "146.52 or 446.25- 100hz"},
	{"KD7OUT", 40.72950, -111.96817, [2]byte{0x2F, 0x3E}, false, 0, 173, ""},
	{"KF6RAL-6", 40.73417, -112.21083, [2]byte{0x2F, 0x5F}, false, 0, 0, "Tooele WX"},
	{"KI7NNK-9", 40.57317, -111.84083, [2]byte{0x5C, 0x6B}, false, 30, 272, ""},
	{"KJ6APX-6", 40.72067, -112.05017, [2]byte{0x2F, 0x3E}, false, 18, 239, ""},
	{"KK7GBE-7", 40.77000, -112.00600, [2]byte{0x2F, 0x6B}, false, 62, 287, ""},
	{"KK7ZSV-14", 40.52583, -111.84167, [2]byte{0x2F, 0x22}, false, 0, 0, "146.540MHz Hefe's Flyer"},
	{"KM7GAN-1", 40.64900, -111.86717, [2]byte{0x2F, 0x3E}, false, 0, 93, ""},
	{"NW5W-8", 40.47624, -111.84587, [2]byte{0x2F, 0x3C}, false, 0, 0, "Graywolf"},
	{"W1UTE-10", 40.54300, -112.02967, [2]byte{0x2F, 0x2D}, false, 0, 0, "Raspberry Pi, Dire Wolf, Xastir"},
	{"W7SAR-8", 40.66467, -112.02100, [2]byte{0x5C, 0x6B}, false, 0, 11, "Search and Rescue"},
}

// Stations returns the canned demo stations as cache entries with
// timestamps stamped to now (offset by a few minutes so they look
// freshly heard but distinct). They are stamped at call time because
// stationcache.QueryBBox filters by a now-relative cutoff; stale
// timestamps would make them invisible on the map.
func Stations() []stationcache.CacheEntry {
	now := time.Now()
	out := make([]stationcache.CacheEntry, 0, len(slcStations))
	for i, s := range slcStations {
		key := "stn:" + s.callsign
		if s.isObject {
			key = "obj:" + s.callsign
		}
		out = append(out, stationcache.CacheEntry{
			Key:       key,
			IsObject:  s.isObject,
			Callsign:  s.callsign,
			Source:    objectSources[s.callsign],
			Lat:       s.lat,
			Lon:       s.lon,
			HasPos:    true,
			Speed:     s.speed,
			Course:    s.course,
			HasCourse: s.course != 0,
			Symbol:    s.symbol,
			Via:       "rf",
			Path:      []string{},
			Direction: "RX",
			Channel:   2,
			Comment:   s.comment,
			// Spread across the last ~12 minutes so "last heard" varies.
			Timestamp: now.Add(-time.Duration(i) * 42 * time.Second),
		})
	}
	return out
}

// Counters is the canned dashboard status used by /api/status in demo mode.
type Counters struct {
	UptimeSeconds int64
	RxFrames      uint64
	TxFrames      uint64
	RxBadFCS      uint64
	IgateGated    uint64
	IgateDownlink uint64
	AudioPeakDBFS float32
	AudioRmsDBFS  float32
}

// StatusCounters returns plausible "active station" dashboard values.
// ~2 days uptime with ~4250 RX frames (roughly 2100 packets/day, a
// believable rate for a busy metro APRS receiver). TX/iGate are bogus
// but plausible.
func StatusCounters() Counters {
	return Counters{
		UptimeSeconds: 2*24*3600 + 3*3600 + 41*60, // ~2d 3h 41m
		RxFrames:      4253,
		TxFrames:      188,
		RxBadFCS:      37,
		IgateGated:    3914,
		IgateDownlink: 12,
		AudioPeakDBFS: -12.0,
		AudioRmsDBFS:  -24.0,
	}
}

// aprsCoord formats decimal degrees into the APRS DMM string used in
// position packets, e.g. 40.47624,-111.84587 -> "4028.57N/11150.75W".
func aprsCoord(lat, lon float64) string {
	latHemi, lonHemi := "N", "W"
	if lat < 0 {
		lat, latHemi = -lat, "S"
	}
	if lon < 0 {
		lon = -lon
	} else {
		lonHemi = "E"
	}
	latDeg := int(lat)
	latMin := (lat - float64(latDeg)) * 60
	lonDeg := int(lon)
	lonMin := (lon - float64(lonDeg)) * 60
	return fmt.Sprintf("%02d%05.2f%s/%03d%05.2f%s", latDeg, latMin, latHemi, lonDeg, lonMin, lonHemi)
}

// Messages returns a canned DM thread between NW5W-8 and KK7GBE-7 so
// the Messages screen renders a real-looking conversation for screenshots.
// Timestamps are stamped to now at call time (same reasoning as Stations).
func Messages() []configstore.Message {
	now := time.Now()
	const (
		ourCall  = "NW5W-8"
		peerCall = "KK7GBE-7"
	)
	seed := []struct {
		dir      string
		text     string
		ackState string
		unread   bool
		age      time.Duration
	}{
		{"in", "Good morning! You up for the SLC valley net tonight?", "none", false, 9 * time.Minute},
		{"out", "Affirmative, QRV 1900 local on 146.520.", "acked", false, 6 * time.Minute},
		{"in", "Perfect, talk then. 73", "none", false, 2 * time.Minute},
	}
	msgs := make([]configstore.Message, 0, len(seed))
	for _, s := range seed {
		ts := now.Add(-s.age)
		from, to := peerCall, ourCall
		if s.dir == "out" {
			from, to = ourCall, peerCall
		}
		msgs = append(msgs, configstore.Message{
			Direction:  s.dir,
			OurCall:    ourCall,
			PeerCall:   peerCall,
			FromCall:   from,
			ToCall:     to,
			Text:       s.text,
			AckState:   s.ackState,
			Source:     "rf",
			Channel:    2,
			ThreadKind: "dm",
			ThreadKey:  peerCall,
			Kind:       "text",
			Unread:     s.unread,
			CreatedAt:  ts,
			UpdatedAt:  ts,
		})
	}
	return msgs
}

// Packets returns canned packet-log entries derived from the demo
// stations, so the dashboard's recent-packets panel renders real-looking
// traffic instead of "Waiting for packets...". Timestamps are stamped to
// now at call time (same reasoning as Stations).
func Packets() []packetlog.Entry {
	now := time.Now()
	out := make([]packetlog.Entry, 0, len(slcStations))
	for i, s := range slcStations {
		display := fmt.Sprintf("%s>APDW17,WIDE1-1:!%s#%s",
			s.callsign, aprsCoord(s.lat, s.lon), s.comment)
		out = append(out, packetlog.Entry{
			Timestamp: now.Add(-time.Duration(i) * 37 * time.Second),
			Channel:   2,
			Direction: packetlog.DirRX,
			Source:    "modem",
			Display:   display,
			Type:      "position",
		})
	}
	return out
}
