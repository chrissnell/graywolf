// Package historydb persists station position history in a standalone
// SQLite database. The schema is bootstrapped on every Open so a fresh
// file (e.g. on a tmpfs after reboot) is ready immediately.
package historydb

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/chrissnell/graywolf/pkg/stationcache"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// posEpsilon matches stationcache's dedup threshold (~1 m at equator).
const posEpsilon = 0.00001

// DB wraps a gorm.DB handle to the history database.
type DB struct {
	db *gorm.DB
}

// Open opens (or creates) the history database at path, applies pragmas,
// and ensures the schema exists. Safe to call on an empty file.
func Open(path string) (*DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open history db %q: %w", path, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)

	_ = db.Exec("PRAGMA journal_mode=WAL").Error
	_ = db.Exec("PRAGMA busy_timeout=5000").Error
	_ = db.Exec("PRAGMA foreign_keys=ON").Error

	if err := bootstrap(db); err != nil {
		return nil, fmt.Errorf("bootstrap schema: %w", err)
	}
	return &DB{db: db}, nil
}

// Close releases the database handle.
func (d *DB) Close() error {
	sqlDB, err := d.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// WriteEntries persists cache entries to the history database.
// Stations are upserted; positions are appended only when the station
// has moved beyond posEpsilon from its last stored position.
func (d *DB) WriteEntries(entries []stationcache.CacheEntry) error {
	return d.db.Transaction(func(tx *gorm.DB) error {
		for i := range entries {
			e := &entries[i]

			if e.Killed {
				// CASCADE deletes positions and weather.
				if err := tx.Exec("DELETE FROM stations WHERE key = ?", e.Key).Error; err != nil {
					return err
				}
				continue
			}

			pathJSON, _ := json.Marshal(e.Path)
			if err := tx.Exec(`
				INSERT INTO stations (key, callsign, is_object, symbol, via, path, hops, direction, channel, comment, last_heard)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(key) DO UPDATE SET
					callsign=excluded.callsign, symbol=excluded.symbol, via=excluded.via,
					path=excluded.path, hops=excluded.hops, direction=excluded.direction,
					channel=excluded.channel, comment=excluded.comment, last_heard=excluded.last_heard`,
				e.Key, e.Callsign, e.IsObject, e.Symbol[:], e.Via, string(pathJSON),
				e.Hops, e.Direction, e.Channel, e.Comment, time.Now(),
			).Error; err != nil {
				return fmt.Errorf("upsert station %s: %w", e.Key, err)
			}

			if e.HasPos {
				if err := insertPositionIfMoved(tx, e); err != nil {
					return err
				}
			}

			if e.Weather != nil {
				if err := upsertWeather(tx, e.Key, e.Weather); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// insertPositionIfMoved appends a position row only when the station
// has moved beyond posEpsilon from its most recent stored position.
func insertPositionIfMoved(tx *gorm.DB, e *stationcache.CacheEntry) error {
	var lastLat, lastLon float64
	var found bool

	row := tx.Raw(
		"SELECT lat, lon FROM positions WHERE station_key = ? ORDER BY timestamp DESC LIMIT 1",
		e.Key,
	).Row()
	if row.Err() == nil {
		if err := row.Scan(&lastLat, &lastLon); err == nil {
			found = true
		}
	}

	pathJSON, _ := json.Marshal(e.Path)

	if found && math.Abs(lastLat-e.Lat) <= posEpsilon && math.Abs(lastLon-e.Lon) <= posEpsilon {
		// Static re-beacon — update timestamp and metadata on latest position.
		return tx.Exec(
			`UPDATE positions SET timestamp = ?, via = ?, path = ?, hops = ?, direction = ?, channel = ?, comment = ?
			 WHERE station_key = ? AND id = (
				SELECT id FROM positions WHERE station_key = ? ORDER BY timestamp DESC LIMIT 1
			)`, e.Timestamp, e.Via, string(pathJSON), e.Hops, e.Direction, e.Channel, e.Comment, e.Key, e.Key,
		).Error
	}

	return tx.Exec(
		`INSERT INTO positions (station_key, lat, lon, alt, has_alt, speed, course, has_course, via, path, hops, direction, channel, comment, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Key, e.Lat, e.Lon, e.Alt, e.HasAlt, e.Speed, e.Course, e.HasCourse,
		e.Via, string(pathJSON), e.Hops, e.Direction, e.Channel, e.Comment, e.Timestamp,
	).Error
}

func upsertWeather(tx *gorm.DB, key string, w *stationcache.Weather) error {
	return tx.Exec(`
		INSERT INTO weather (station_key, temp, has_temp, wind_speed, has_wind_speed,
			wind_dir, has_wind_dir, wind_gust, has_wind_gust, humidity, has_humidity,
			pressure, has_pressure, rain_1h, has_rain_1h, rain_24h, has_rain_24h,
			snow_24h, has_snow_24h, luminosity, has_luminosity)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(station_key) DO UPDATE SET
			temp=excluded.temp, has_temp=excluded.has_temp,
			wind_speed=excluded.wind_speed, has_wind_speed=excluded.has_wind_speed,
			wind_dir=excluded.wind_dir, has_wind_dir=excluded.has_wind_dir,
			wind_gust=excluded.wind_gust, has_wind_gust=excluded.has_wind_gust,
			humidity=excluded.humidity, has_humidity=excluded.has_humidity,
			pressure=excluded.pressure, has_pressure=excluded.has_pressure,
			rain_1h=excluded.rain_1h, has_rain_1h=excluded.has_rain_1h,
			rain_24h=excluded.rain_24h, has_rain_24h=excluded.has_rain_24h,
			snow_24h=excluded.snow_24h, has_snow_24h=excluded.has_snow_24h,
			luminosity=excluded.luminosity, has_luminosity=excluded.has_luminosity`,
		key,
		w.Temp, w.HasTemp, w.WindSpeed, w.HasWindSpeed,
		w.WindDir, w.HasWindDir, w.WindGust, w.HasWindGust,
		w.Humidity, w.HasHumidity, w.Pressure, w.HasPressure,
		w.Rain1h, w.HasRain1h, w.Rain24h, w.HasRain24h,
		w.Snow24h, w.HasSnow24h, w.Luminosity, w.HasLuminosity,
	).Error
}

// LoadRecent returns stations heard within maxAge, each with up to
// trailLimit positions (newest first). The returned map is keyed by
// composite station key ("stn:..." or "obj:...").
func (d *DB) LoadRecent(maxAge time.Duration, trailLimit int) (map[string]*stationcache.Station, error) {
	cutoff := time.Now().Add(-maxAge)

	type stationRow struct {
		Key       string
		Callsign  string
		IsObject  bool
		Symbol    []byte
		Via       string
		Path      string
		Hops      int
		Direction string
		Channel   uint32
		Comment   string
		LastHeard time.Time
	}
	var rows []stationRow
	if err := d.db.Raw(
		"SELECT key, callsign, is_object, symbol, via, path, hops, direction, channel, comment, last_heard FROM stations WHERE last_heard >= ?",
		cutoff,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load stations: %w", err)
	}

	out := make(map[string]*stationcache.Station, len(rows))
	for _, r := range rows {
		s := &stationcache.Station{
			Key:       r.Key,
			Callsign:  r.Callsign,
			IsObject:  r.IsObject,
			Via:       r.Via,
			Hops:      r.Hops,
			Direction: r.Direction,
			Channel:   r.Channel,
			Comment:   r.Comment,
			LastHeard: r.LastHeard,
		}
		if len(r.Symbol) >= 2 {
			s.Symbol = [2]byte{r.Symbol[0], r.Symbol[1]}
		}
		_ = json.Unmarshal([]byte(r.Path), &s.Path)

		// Load positions
		type posRow struct {
			Lat       float64
			Lon       float64
			Alt       float64
			HasAlt    bool
			Speed     float64
			Course    int
			HasCourse bool
			Via       string
			Path      string
			Hops      int
			Direction string
			Channel   uint32
			Comment   string
			Timestamp time.Time
		}
		var posRows []posRow
		if err := d.db.Raw(
			"SELECT lat, lon, alt, has_alt, speed, course, has_course, via, path, hops, direction, channel, comment, timestamp FROM positions WHERE station_key = ? ORDER BY timestamp DESC LIMIT ?",
			r.Key, trailLimit,
		).Scan(&posRows).Error; err != nil {
			return nil, fmt.Errorf("load positions for %s: %w", r.Key, err)
		}
		s.Positions = make([]stationcache.Position, len(posRows))
		for i, p := range posRows {
			s.Positions[i] = stationcache.Position{
				Lat: p.Lat, Lon: p.Lon,
				Alt: p.Alt, HasAlt: p.HasAlt,
				Speed: p.Speed, Course: p.Course, HasCourse: p.HasCourse,
				Via: p.Via, Hops: p.Hops,
				Direction: p.Direction, Channel: p.Channel, Comment: p.Comment,
				Timestamp: p.Timestamp,
			}
			_ = json.Unmarshal([]byte(p.Path), &s.Positions[i].Path)
		}

		// Load weather
		type wxRow struct {
			Temp          float64
			HasTemp       bool
			WindSpeed     float64
			HasWindSpeed  bool
			WindDir       int
			HasWindDir    bool
			WindGust      float64
			HasWindGust   bool
			Humidity      int
			HasHumidity   bool
			Pressure      float64
			HasPressure   bool
			Rain1h        float64
			HasRain1h     bool
			Rain24h       float64
			HasRain24h    bool
			Snow24h       float64
			HasSnow24h    bool
			Luminosity    int
			HasLuminosity bool
		}
		var wx wxRow
		res := d.db.Raw(`SELECT temp, has_temp, wind_speed, has_wind_speed, wind_dir, has_wind_dir,
			wind_gust, has_wind_gust, humidity, has_humidity, pressure, has_pressure,
			rain_1h, has_rain_1h, rain_24h, has_rain_24h, snow_24h, has_snow_24h,
			luminosity, has_luminosity FROM weather WHERE station_key = ?`, r.Key).Scan(&wx)
		if res.Error == nil && res.RowsAffected > 0 {
			s.Weather = &stationcache.Weather{
				Temp: wx.Temp, HasTemp: wx.HasTemp,
				WindSpeed: wx.WindSpeed, HasWindSpeed: wx.HasWindSpeed,
				WindDir: wx.WindDir, HasWindDir: wx.HasWindDir,
				WindGust: wx.WindGust, HasWindGust: wx.HasWindGust,
				Humidity: wx.Humidity, HasHumidity: wx.HasHumidity,
				Pressure: wx.Pressure, HasPressure: wx.HasPressure,
				Rain1h: wx.Rain1h, HasRain1h: wx.HasRain1h,
				Rain24h: wx.Rain24h, HasRain24h: wx.HasRain24h,
				Snow24h: wx.Snow24h, HasSnow24h: wx.HasSnow24h,
				Luminosity: wx.Luminosity, HasLuminosity: wx.HasLuminosity,
			}
		}

		out[r.Key] = s
	}

	return out, nil
}

// Prune deletes positions older than maxAge and removes any stations
// that no longer have any positions.
func (d *DB) Prune(maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge)
	return d.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM positions WHERE timestamp < ?", cutoff).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM stations WHERE key NOT IN (SELECT DISTINCT station_key FROM positions)").Error
	})
}

// bootstrap creates the schema tables and indices if they don't exist.
func bootstrap(db *gorm.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS stations (
			key        TEXT PRIMARY KEY,
			callsign   TEXT NOT NULL,
			is_object  INTEGER NOT NULL DEFAULT 0,
			symbol     BLOB NOT NULL,
			via        TEXT NOT NULL DEFAULT 'rf',
			path       TEXT NOT NULL DEFAULT '[]',
			hops       INTEGER NOT NULL DEFAULT 0,
			direction  TEXT NOT NULL DEFAULT 'RX',
			channel    INTEGER NOT NULL DEFAULT 0,
			comment    TEXT NOT NULL DEFAULT '',
			last_heard DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS positions (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			station_key TEXT NOT NULL REFERENCES stations(key) ON DELETE CASCADE,
			lat         REAL NOT NULL,
			lon         REAL NOT NULL,
			alt         REAL NOT NULL DEFAULT 0,
			has_alt     INTEGER NOT NULL DEFAULT 0,
			speed       REAL NOT NULL DEFAULT 0,
			course      INTEGER NOT NULL DEFAULT 0,
			has_course  INTEGER NOT NULL DEFAULT 0,
			timestamp   DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pos_station_time ON positions(station_key, timestamp DESC)`,
		`CREATE TABLE IF NOT EXISTS weather (
			station_key   TEXT PRIMARY KEY REFERENCES stations(key) ON DELETE CASCADE,
			temp          REAL, has_temp INTEGER NOT NULL DEFAULT 0,
			wind_speed    REAL, has_wind_speed INTEGER NOT NULL DEFAULT 0,
			wind_dir      INTEGER, has_wind_dir INTEGER NOT NULL DEFAULT 0,
			wind_gust     REAL, has_wind_gust INTEGER NOT NULL DEFAULT 0,
			humidity      INTEGER, has_humidity INTEGER NOT NULL DEFAULT 0,
			pressure      REAL, has_pressure INTEGER NOT NULL DEFAULT 0,
			rain_1h       REAL, has_rain_1h INTEGER NOT NULL DEFAULT 0,
			rain_24h      REAL, has_rain_24h INTEGER NOT NULL DEFAULT 0,
			snow_24h      REAL, has_snow_24h INTEGER NOT NULL DEFAULT 0,
			luminosity    INTEGER, has_luminosity INTEGER NOT NULL DEFAULT 0
		)`,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}

	// Migrate: add per-position metadata columns to existing databases.
	// Errors are ignored (column already exists).
	for _, m := range []string{
		`ALTER TABLE positions ADD COLUMN via TEXT NOT NULL DEFAULT 'rf'`,
		`ALTER TABLE positions ADD COLUMN path TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE positions ADD COLUMN hops INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE positions ADD COLUMN direction TEXT NOT NULL DEFAULT 'RX'`,
		`ALTER TABLE positions ADD COLUMN channel INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE positions ADD COLUMN comment TEXT NOT NULL DEFAULT ''`,
	} {
		_ = db.Exec(m).Error
	}

	return nil
}
