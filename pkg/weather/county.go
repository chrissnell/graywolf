// Package weather implements the NWS weather-alert forwarding subsystem.
// It receives NWS APRS packets from the iGate's IS-receive hook, maintains
// a live in-memory county alert table, and selectively retransmits eligible
// alerts over RF by submitting directly to the TX governor — bypassing the
// iGate's IsNWS policy block, which remains unchanged.
package weather

import (
	"math"
	"strings"
)

// County holds the static geographic and administrative data for one US county.
// Loaded once at startup from the embedded county_data.json.
type County struct {
	FIPS       string  `json:"fips"`
	State      string  `json:"state"`
	CountyName string  `json:"county_name"`
	CWA        string  `json:"cwa"`
	NWSCode    string  `json:"nws_code"` // e.g. "OHC019"
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
}

// NWSCodeFromFIPS derives the NWS zone code used in APRS packets from a
// state abbreviation and 5-char FIPS code (e.g. "OH"+"39019" → "OHC019").
func NWSCodeFromFIPS(state, fips string) string {
	if len(fips) < 5 || state == "" {
		return ""
	}
	return strings.ToUpper(state) + "C" + fips[2:]
}

// haversineDistanceMi returns the great-circle distance in statute miles
// between two lat/lon coordinates. Used for distance-gating county alerts.
func haversineDistanceMi(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMi = 3958.8
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	lat1R := lat1 * math.Pi / 180
	lat2R := lat2 * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1R)*math.Cos(lat2R)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMi * c
}

// knownNWSWFOs is the set of 3-letter NWS Weather Forecast Office identifiers.
// Used to classify APRS object source callsigns as NWS-originated.
var knownNWSWFOs = map[string]bool{
	"ABQ": true, "ABR": true, "AFC": true, "AFG": true, "AJK": true,
	"AKQ": true, "ALY": true, "AMA": true, "APX": true, "ARX": true,
	"BGM": true, "BIS": true, "BMX": true, "BOI": true, "BOU": true,
	"BOX": true, "BRO": true, "BTR": true, "BUF": true, "BYZ": true,
	"CAE": true, "CAR": true, "CHS": true, "CLE": true, "CRP": true,
	"CTP": true, "CYS": true, "DDC": true, "DLH": true, "DMX": true,
	"DTX": true, "DVN": true, "EAX": true, "EKA": true, "EPZ": true,
	"EWX": true, "EYW": true, "FFC": true, "FGF": true, "FGZ": true,
	"FSD": true, "FSX": true, "FWD": true, "GGW": true, "GID": true,
	"GJT": true, "GLD": true, "GRB": true, "GRR": true, "GSP": true,
	"GUM": true, "GYX": true, "HFO": true, "HGX": true, "HNX": true,
	"HUN": true, "ICT": true, "ILM": true, "ILN": true, "ILX": true,
	"IND": true, "IWX": true, "JAN": true, "JAX": true, "JKL": true,
	"KEY": true, "LBF": true, "LCH": true, "LKN": true, "LMK": true,
	"LOT": true, "LOX": true, "LSX": true, "LUB": true, "LWX": true,
	"LZK": true, "MAF": true, "MEG": true, "MFR": true, "MHX": true,
	"MKX": true, "MLB": true, "MQT": true, "MRX": true, "MSO": true,
	"MTR": true, "MXX": true, "OAX": true, "OHX": true, "OKX": true,
	"OTX": true, "OUN": true, "PAH": true, "PBZ": true, "PDT": true,
	"PHI": true, "PIH": true, "PQR": true, "PSR": true, "PUB": true,
	"RAH": true, "REV": true, "RIW": true, "RLX": true, "RNK": true,
	"SEW": true, "SGF": true, "SGX": true, "SHV": true, "SJT": true,
	"SJU": true, "SLC": true, "STO": true, "TAE": true, "TBW": true,
	"TFX": true, "TOP": true, "TSA": true, "TWC": true, "UNR": true,
	"VEF": true,
}

// IsNWSWFOCallsign reports whether src looks like an NWS WFO callsign.
// NWS WFO callsigns start with a 3-letter WFO code (e.g. "PBZ", "LOT").
// The callsign may have additional characters after the WFO code.
func IsNWSWFOCallsign(src string) bool {
	src = strings.ToUpper(strings.TrimSpace(src))
	if len(src) < 3 {
		return false
	}
	return knownNWSWFOs[src[:3]]
}

// NWSAlertKeywords is the set of alert type strings the NWS embeds in
// APRS object comments to classify the event. Used to recognise NWS
// position objects from WFOs.
var NWSAlertKeywords = map[string]bool{
	"FLASH_FLOOD":    true,
	"SVR_STORM":      true,
	"TORNADO":        true,
	"TORNADO_WARNING": true,
	"SEVERE_TSTORM":  true,
	"WINTER_STORM":   true,
	"ICE_STORM":      true,
	"BLIZZARD":       true,
	"FLOOD":          true,
	"DENSE_FOG":      true,
	"FIRE_WEATHER":   true,
	"HIGH_WIND":      true,
	"HURRICANE":      true,
	"TROPICAL_STORM": true,
	"FREEZE":         true,
	"FROST":          true,
	"HEAT":           true,
	"WIND_CHILL":     true,
}
