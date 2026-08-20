package weather

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed county_data.json
var countyDataJSON []byte

// LoadCounties parses the embedded county dataset and returns a slice of all
// US counties. Called once at service startup.
func LoadCounties() ([]County, error) {
	var counties []County
	if err := json.Unmarshal(countyDataJSON, &counties); err != nil {
		return nil, fmt.Errorf("weather: parse county data: %w", err)
	}
	return counties, nil
}

// BuildNWSCodeIndex constructs a map keyed by NWS zone code (e.g. "OHC019")
// for O(1) lookups when processing incoming NWS alert messages.
func BuildNWSCodeIndex(counties []County) map[string]*County {
	idx := make(map[string]*County, len(counties))
	for i := range counties {
		c := &counties[i]
		if c.NWSCode != "" {
			idx[c.NWSCode] = c
		}
	}
	return idx
}

// BuildFIPSIndex constructs a map keyed by FIPS code for O(1) lookups.
func BuildFIPSIndex(counties []County) map[string]*County {
	idx := make(map[string]*County, len(counties))
	for i := range counties {
		c := &counties[i]
		if c.FIPS != "" {
			idx[c.FIPS] = c
		}
	}
	return idx
}
