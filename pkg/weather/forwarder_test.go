package weather

import (
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func makeCfg(enabled bool, channelID uint32, maxMi float64, minSec uint32) *configstore.WeatherConfig {
	return &configstore.WeatherConfig{
		Enabled:            enabled,
		TxChannelID:        channelID,
		MaxDistanceMiles:   maxMi,
		MinIntervalSeconds: minSec,
	}
}

var (
	testCounty = &County{FIPS: "39019", State: "OH", NWSCode: "OHC019", Lat: 41.0, Lon: -81.5}
	testMyLat  = 41.2
	testMyLon  = -81.8 // ~18 mi from testCounty
)

func TestCanForwardMessage_AllowedOnFirstSend(t *testing.T) {
	cfg := makeCfg(true, 1, 100, 300)
	alert := &AlertEntry{AlertStatus: AlertStatusWarning, AlertType: "FLASH_FLOOD"}
	prefs := map[string]bool{"39019": true}

	ok, result := CanForwardMessage(cfg, testCounty, alert, prefs, testMyLat, testMyLon, time.Now(), true)
	if !ok {
		t.Errorf("expected allow, got %v", result)
	}
}

func TestCanForwardMessage_DisabledConfig(t *testing.T) {
	cfg := makeCfg(false, 1, 100, 300)
	alert := &AlertEntry{AlertStatus: AlertStatusWarning}
	prefs := map[string]bool{"39019": true}

	ok, result := CanForwardMessage(cfg, testCounty, alert, prefs, testMyLat, testMyLon, time.Now(), true)
	if ok || result != ForwardDisabled {
		t.Errorf("expected ForwardDisabled, got ok=%v result=%v", ok, result)
	}
}

func TestCanForwardMessage_NoChannel(t *testing.T) {
	cfg := makeCfg(true, 0, 100, 300)
	alert := &AlertEntry{}
	prefs := map[string]bool{"39019": true}

	ok, result := CanForwardMessage(cfg, testCounty, alert, prefs, testMyLat, testMyLon, time.Now(), true)
	if ok || result != ForwardNoChannel {
		t.Errorf("expected ForwardNoChannel, got ok=%v result=%v", ok, result)
	}
}

func TestCanForwardMessage_CountyNotEnabled(t *testing.T) {
	cfg := makeCfg(true, 1, 100, 300)
	alert := &AlertEntry{}
	prefs := map[string]bool{} // county not opted in

	ok, result := CanForwardMessage(cfg, testCounty, alert, prefs, testMyLat, testMyLon, time.Now(), true)
	if ok || result != ForwardCountyNotEnabled {
		t.Errorf("expected ForwardCountyNotEnabled, got ok=%v result=%v", ok, result)
	}
}

func TestCanForwardMessage_TooFar(t *testing.T) {
	cfg := makeCfg(true, 1, 1, 300) // max 1 mile — county is ~18 mi away
	alert := &AlertEntry{}
	prefs := map[string]bool{"39019": true}

	ok, result := CanForwardMessage(cfg, testCounty, alert, prefs, testMyLat, testMyLon, time.Now(), true)
	if ok || result != ForwardTooFar {
		t.Errorf("expected ForwardTooFar, got ok=%v result=%v", ok, result)
	}
}

func TestCanForwardMessage_TooSoon(t *testing.T) {
	cfg := makeCfg(true, 1, 100, 300)
	recentTX := time.Now().Add(-60 * time.Second) // only 1 min ago, need 5
	alert := &AlertEntry{LastTX: recentTX}
	prefs := map[string]bool{"39019": true}

	ok, result := CanForwardMessage(cfg, testCounty, alert, prefs, testMyLat, testMyLon, time.Now(), false)
	if ok || result != ForwardTooSoon {
		t.Errorf("expected ForwardTooSoon, got ok=%v result=%v", ok, result)
	}
}

func TestCanForwardMessage_ChangedBypassesInterval(t *testing.T) {
	cfg := makeCfg(true, 1, 100, 300)
	recentTX := time.Now().Add(-60 * time.Second)
	alert := &AlertEntry{LastTX: recentTX}
	prefs := map[string]bool{"39019": true}

	// changed=true bypasses the interval check
	ok, result := CanForwardMessage(cfg, testCounty, alert, prefs, testMyLat, testMyLon, time.Now(), true)
	if !ok {
		t.Errorf("expected allow on changed, got %v", result)
	}
}

func TestCanForwardPosition_TooFar(t *testing.T) {
	cfg := makeCfg(true, 1, 1, 300)
	ok, result := CanForwardPosition(cfg, 41.0, -81.5, testMyLat, testMyLon, time.Time{}, time.Now(), false)
	if ok || result != ForwardTooFar {
		t.Errorf("expected ForwardTooFar, got ok=%v result=%v", ok, result)
	}
}

func TestCanForwardPosition_OK(t *testing.T) {
	cfg := makeCfg(true, 1, 100, 300)
	ok, result := CanForwardPosition(cfg, 41.0, -81.5, testMyLat, testMyLon, time.Time{}, time.Now(), false)
	if !ok {
		t.Errorf("expected allow, got %v", result)
	}
}
