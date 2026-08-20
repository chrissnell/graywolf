package weather

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// positionExpiry is how long a WFO position entry is retained without a refresh.
const positionExpiry = 20 * time.Minute

// expiryCheckInterval is how often the service scans for stale entries.
const expiryCheckInterval = 10 * time.Second

// ConfigStore is the slice of the configstore.Store the weather service needs.
type ConfigStore interface {
	GetWeatherConfig(ctx context.Context) (*configstore.WeatherConfig, error)
	ListWeatherCountyPrefs(ctx context.Context) (map[string]bool, error)
}

// Options configures the weather service.
type Options struct {
	TxSink    txgovernor.TxSink
	Store     ConfigStore
	PosCache  gps.PositionCache
	Logger    *slog.Logger
	OurCallFn func() string // returns the station's current callsign
}

// CountyAlertDTO is the wire shape returned by the REST API for one county.
type CountyAlertDTO struct {
	FIPS        string   `json:"fips"`
	NWSCode     string   `json:"nws_code"`
	CountyName  string   `json:"county_name"`
	State       string   `json:"state"`
	CWA         string   `json:"cwa"`
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`
	DistanceMi  float64  `json:"distance_mi"`  // -1 when position unknown
	AlertStatus string   `json:"alert_status"` // "clear" | "watch" | "warning"
	AlertType   string   `json:"alert_type"`   // e.g. "FLASH_FLOOD"; "" for clear
	AllowTX     bool     `json:"allow_tx"`
	LastUpdated *string  `json:"last_updated,omitempty"` // RFC3339 time of the last heard packet; nil when county has never been heard
}

// Service is the NWS weather-alert forwarding service.
type Service struct {
	opts      Options
	forwarder *Forwarder
	state     *StateStore
	counties  []County
	nwsIndex  map[string]*County // keyed by NWS code
	fipsIndex map[string]*County // keyed by FIPS

	mu    sync.RWMutex
	cfg   *configstore.WeatherConfig
	prefs map[string]bool
}

// New constructs a weather Service and loads the embedded county data. Returns
// an error if the county data cannot be parsed.
func New(opts Options) (*Service, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	counties, err := LoadCounties()
	if err != nil {
		return nil, err
	}
	svc := &Service{
		opts:      opts,
		forwarder: NewForwarder(opts.TxSink, opts.OurCallFn, opts.Logger),
		state:     NewStateStore(),
		counties:  counties,
		nwsIndex:  BuildNWSCodeIndex(counties),
		fipsIndex: BuildFIPSIndex(counties),
		prefs:     make(map[string]bool),
		cfg: &configstore.WeatherConfig{
			Enabled:            false,
			MaxDistanceMiles:   50,
			MinIntervalSeconds: 300,
		},
	}
	return svc, nil
}

// Reload re-reads the weather config and county prefs from the store. Called
// at startup and whenever config is saved through the REST API.
func (s *Service) Reload(ctx context.Context) {
	cfg, err := s.opts.Store.GetWeatherConfig(ctx)
	if err != nil {
		s.opts.Logger.Warn("weather: reload config failed", "err", err)
	}
	prefs, err := s.opts.Store.ListWeatherCountyPrefs(ctx)
	if err != nil {
		s.opts.Logger.Warn("weather: reload prefs failed", "err", err)
		prefs = make(map[string]bool)
	}
	s.mu.Lock()
	if cfg != nil {
		s.cfg = cfg
	}
	s.prefs = prefs
	s.mu.Unlock()
}

// Run starts the background stale-alert and position-expiry loop. It blocks until ctx is cancelled.
func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(expiryCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.mu.RLock()
			clearMinutes := s.cfg.AlertClearMinutes
			s.mu.RUnlock()
			if clearMinutes == 0 {
				clearMinutes = 10
			}
			clearCutoff := time.Now().Add(-time.Duration(clearMinutes) * time.Minute)
			if cleared := s.state.ClearStaleAlerts(clearCutoff); len(cleared) > 0 {
				s.opts.Logger.Debug("weather: auto-cleared stale alerts", "codes", cleared)
			}
			s.state.ExpirePositions(time.Now().Add(-positionExpiry))
		}
	}
}

// HandleISPacket processes one IS-received APRS packet. This is called from
// onIGateIsRxPacket in pkg/app/wiring.go for every IS-arrived packet.
func (s *Service) HandleISPacket(ctx context.Context, pkt *aprs.DecodedAPRSPacket) {
	if pkt == nil {
		return
	}
	s.mu.RLock()
	cfg := s.cfg
	prefs := s.prefs
	s.mu.RUnlock()

	myLat, myLon, havePos := s.myPosition()
	now := time.Now()

	// NWS alert message path.
	if alert, ok := ParseNWSMessage(pkt); ok {
		s.handleAlertMessage(ctx, alert, pkt, cfg, prefs, myLat, myLon, havePos, now)
		return
	}

	// NWS position object path.
	if posEvent, ok := ParseNWSPosition(pkt); ok {
		s.handlePositionPacket(ctx, posEvent, pkt, cfg, myLat, myLon, havePos, now)
	}
}

func (s *Service) handleAlertMessage(
	ctx context.Context,
	alert *NWSAlert,
	pkt *aprs.DecodedAPRSPacket,
	cfg *configstore.WeatherConfig,
	prefs map[string]bool,
	myLat, myLon float64,
	havePos bool,
	now time.Time,
) {
	for _, nwsCode := range alert.NWSCodes {
		changed := s.state.UpsertAlert(nwsCode, alert.AlertStatus, alert.AlertType, pkt)
		county := s.nwsIndex[nwsCode]
		if county == nil {
			continue
		}
		if !havePos {
			continue
		}
		entry := s.state.GetAlert(nwsCode)
		if entry == nil {
			continue
		}
		ok, reason := CanForwardMessage(cfg, county, entry, prefs, myLat, myLon, now, changed)
		if !ok {
			s.opts.Logger.Debug("weather: message not forwarded",
				"nws_code", nwsCode, "reason", reason)
			continue
		}
		result := s.forwarder.ForwardPacket(ctx, cfg.TxChannelID, pkt)
		if result == ForwardOK {
			s.state.SetLastTX(nwsCode, now)
		}
	}
}

func (s *Service) handlePositionPacket(
	ctx context.Context,
	posEvent *NWSPositionEvent,
	pkt *aprs.DecodedAPRSPacket,
	cfg *configstore.WeatherConfig,
	myLat, myLon float64,
	havePos bool,
	now time.Time,
) {
	posLat, posLon := s.positionFromPacket(pkt)
	if posLat == 0 && posLon == 0 {
		return
	}
	changed := s.state.UpsertPosition(posEvent.WFO, posEvent.AlertType, posEvent.AlertStatus, posLat, posLon, pkt)

	if !havePos {
		return
	}
	// Get LastTX for this position entry.
	var lastTX time.Time
	for _, e := range s.state.PositionSnapshot() {
		if e.WFO == posEvent.WFO && e.AlertType == posEvent.AlertType {
			lastTX = e.LastTX
			break
		}
	}
	ok, reason := CanForwardPosition(cfg, posLat, posLon, myLat, myLon, lastTX, now, changed)
	if !ok {
		s.opts.Logger.Debug("weather: position not forwarded", "wfo", posEvent.WFO, "reason", reason)
		return
	}
	result := s.forwarder.ForwardPacket(ctx, cfg.TxChannelID, pkt)
	if result == ForwardOK {
		s.state.SetPositionLastTX(posEvent.WFO, posEvent.AlertType, now)
	}
}

func (s *Service) positionFromPacket(pkt *aprs.DecodedAPRSPacket) (lat, lon float64) {
	switch {
	case pkt.Object != nil && pkt.Object.Position != nil:
		return pkt.Object.Position.Latitude, pkt.Object.Position.Longitude
	case pkt.Position != nil:
		return pkt.Position.Latitude, pkt.Position.Longitude
	}
	return 0, 0
}

func (s *Service) myPosition() (lat, lon float64, ok bool) {
	if s.opts.PosCache == nil {
		return 0, 0, false
	}
	fix, valid := s.opts.PosCache.Get()
	if !valid || (fix.Latitude == 0 && fix.Longitude == 0) {
		return 0, 0, false
	}
	return fix.Latitude, fix.Longitude, true
}

// CountyAlerts assembles the full county list with live alert state, county
// prefs, and distances from the station's current position. Counties with
// active alerts sort first within the same distance.
func (s *Service) CountyAlerts(ctx context.Context) ([]CountyAlertDTO, error) {
	s.mu.RLock()
	prefs := s.prefs
	s.mu.RUnlock()

	myLat, myLon, havePos := s.myPosition()

	out := make([]CountyAlertDTO, 0, len(s.counties))
	for _, c := range s.counties {
		dto := CountyAlertDTO{
			FIPS:        c.FIPS,
			NWSCode:     c.NWSCode,
			CountyName:  c.CountyName,
			State:       c.State,
			CWA:         c.CWA,
			Lat:         c.Lat,
			Lon:         c.Lon,
			DistanceMi:  -1,
			AlertStatus: AlertStatusClear,
			AllowTX:     prefs[c.FIPS],
		}
		if havePos {
			dto.DistanceMi = haversineDistanceMi(myLat, myLon, c.Lat, c.Lon)
		}
		if entry := s.state.GetAlert(c.NWSCode); entry != nil {
			dto.AlertStatus = entry.AlertStatus
			dto.AlertType = entry.AlertType
			lastSeen := entry.LastSeen.Format(time.RFC3339)
			dto.LastUpdated = &lastSeen
			_ = ctx // ctx reserved for future DB queries
		}
		out = append(out, dto)
	}
	return out, nil
}

// Config returns the current weather config (safe for concurrent use).
func (s *Service) Config() *configstore.WeatherConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := *s.cfg
	return &cp
}
