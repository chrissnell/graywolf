package stationcache

import (
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// CacheEntry is the output of ExtractEntry, ready for MemCache.Update().
type CacheEntry struct {
	Key       string
	IsObject  bool
	Killed    bool // object/item with Live==false → delete from cache
	Callsign  string
	Lat, Lon  float64
	HasPos    bool
	Alt       float64
	HasAlt    bool
	Speed     float64
	Course    int
	HasCourse bool
	Symbol    [2]byte
	Via       string
	Path      []string
	Hops      int
	Direction string
	Channel   uint32
	Comment   string
	Weather   *Weather
	Timestamp time.Time
}

// ExtractEntry converts a decoded APRS packet into cache update(s).
// Most packets produce one entry; a packet carrying an object/item
// produces an entry for the object plus (if the originator has a
// position) an entry for the originating station. Returns nil if the
// packet has no useful station data.
func ExtractEntry(decoded *aprs.DecodedAPRSPacket, source, dir string, ch uint32) []CacheEntry {
	if decoded == nil {
		return nil
	}

	// Third-party unwrapping: use the inner packet's Source/Path/Position.
	pkt := decoded
	if decoded.ThirdParty != nil {
		pkt = decoded.ThirdParty
	}

	via := deriveVia(source)
	path := pkt.Path
	hops := countHops(path)
	ts := pkt.Timestamp
	if ts.IsZero() {
		// Inner packet (third-party) often lacks a timestamp — fall back
		// to the outer packet's timestamp before using wall clock.
		ts = decoded.Timestamp
	}
	if ts.IsZero() {
		ts = time.Now()
	}

	var entries []CacheEntry

	// Object or item → keyed by object/item name in the "obj:" namespace.
	if pkt.Object != nil {
		e := buildObjectEntry(pkt.Object.Name, pkt.Object.Live, pkt.Object.Position, pkt.Object.Comment, via, path, hops, dir, ch, ts)
		if pkt.Weather != nil {
			e.Weather = convertWeather(pkt.Weather)
		}
		entries = append(entries, e)

		// Also emit a station entry for the originator if we have
		// a top-level position (rare but possible in some encodings).
		if pkt.Position != nil {
			entries = append(entries, buildStationEntry(pkt.Source, pkt.Position, pkt.Comment, via, path, hops, dir, ch, ts, pkt.Weather))
		}
		return entries
	}

	if pkt.Item != nil {
		e := buildObjectEntry(pkt.Item.Name, pkt.Item.Live, pkt.Item.Position, pkt.Item.Comment, via, path, hops, dir, ch, ts)
		if pkt.Weather != nil {
			e.Weather = convertWeather(pkt.Weather)
		}
		entries = append(entries, e)
		return entries
	}

	// Normal station packet — position may come from Position field
	// (includes Mic-E, which the parser copies to pkt.Position).
	if pkt.Position != nil || pkt.Weather != nil {
		entries = append(entries, buildStationEntry(pkt.Source, pkt.Position, pkt.Comment, via, path, hops, dir, ch, ts, pkt.Weather))
		return entries
	}

	return nil
}

func buildStationEntry(callsign string, pos *aprs.Position, comment, via string, path []string, hops int, dir string, ch uint32, ts time.Time, wx *aprs.Weather) CacheEntry {
	e := CacheEntry{
		Key:       "stn:" + callsign,
		Callsign:  callsign,
		Via:       via,
		Path:      path,
		Hops:      hops,
		Direction: dir,
		Channel:   ch,
		Comment:   comment,
		Timestamp: ts,
	}
	if pos != nil {
		e.HasPos = true
		e.Lat = pos.Latitude
		e.Lon = pos.Longitude
		e.Alt = pos.Altitude
		e.HasAlt = pos.HasAlt
		e.Speed = pos.Speed
		e.Course = pos.Course
		e.HasCourse = pos.HasCourse
		e.Symbol = [2]byte{pos.Symbol.Table, pos.Symbol.Code}
	}
	if wx != nil {
		e.Weather = convertWeather(wx)
	}
	return e
}

func buildObjectEntry(name string, live bool, pos *aprs.Position, comment, via string, path []string, hops int, dir string, ch uint32, ts time.Time) CacheEntry {
	e := CacheEntry{
		Key:       "obj:" + name,
		Callsign:  name,
		IsObject:  true,
		Killed:    !live,
		Via:       via,
		Path:      path,
		Hops:      hops,
		Direction: dir,
		Channel:   ch,
		Comment:   comment,
		Timestamp: ts,
	}
	if pos != nil {
		e.HasPos = true
		e.Lat = pos.Latitude
		e.Lon = pos.Longitude
		e.Alt = pos.Altitude
		e.HasAlt = pos.HasAlt
		e.Speed = pos.Speed
		e.Course = pos.Course
		e.HasCourse = pos.HasCourse
		e.Symbol = [2]byte{pos.Symbol.Table, pos.Symbol.Code}
	}
	return e
}

func deriveVia(source string) string {
	switch source {
	case "modem", "kiss", "agw", "digipeater", "beacon":
		return "rf"
	case "igate-is":
		return "is"
	case "igate":
		// RF→IS upload — the station was heard on RF
		return "rf"
	default:
		return "rf"
	}
}

func countHops(path []string) int {
	n := 0
	for _, p := range path {
		if strings.HasSuffix(p, "*") {
			n++
		}
	}
	return n
}

// convertWeather converts aprs.Weather to the cache Weather struct.
// Pressure is converted from tenths of millibar to millibars.
func convertWeather(wx *aprs.Weather) *Weather {
	if wx == nil {
		return nil
	}
	w := &Weather{
		Temp:          wx.Temperature,
		HasTemp:       wx.HasTemp,
		WindSpeed:     wx.WindSpeed,
		HasWindSpeed:  wx.HasWindSpeed,
		WindDir:       wx.WindDirection,
		HasWindDir:    wx.HasWindDir,
		WindGust:      wx.WindGust,
		HasWindGust:   wx.HasWindGust,
		Humidity:      wx.Humidity,
		HasHumidity:   wx.HasHumidity,
		Pressure:      wx.Pressure / 10, // tenths of mbar → mbar
		HasPressure:   wx.HasPressure,
		Rain1h:        wx.Rain1Hour,
		HasRain1h:     wx.HasRain1h,
		Rain24h:       wx.Rain24Hour,
		HasRain24h:    wx.HasRain24h,
		Snow24h:       wx.Snowfall24h,
		HasSnow24h:    wx.HasSnow,
		Luminosity:    wx.Luminosity,
		HasLuminosity: wx.HasLuminosity,
	}
	return w
}
