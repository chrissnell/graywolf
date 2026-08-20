package weather

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/igate"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

const (
	// aprsGatewayToCall is the outer AX.25 destination used for weather-forwarded
	// third-party frames, matching the iGate's own tocall.
	aprsGatewayToCall = "APGWLF"
	// submitKind identifies weather-forwarded frames in governor logs.
	submitKind = "weather"
	// submitTimeout bounds how long a single TX submit may block.
	submitTimeout = 2 * time.Second
)

// ForwardResult describes the outcome of a forward attempt.
type ForwardResult int

const (
	ForwardOK ForwardResult = iota
	ForwardDisabled
	ForwardNoChannel
	ForwardCountyNotEnabled
	ForwardTooFar
	ForwardTooSoon
	ForwardSubmitError
	ForwardBuildError
	ForwardNoPacket
)

// Forwarder submits eligible NWS packets to the TX governor for RF transmission.
type Forwarder struct {
	sink       txgovernor.TxSink
	ourCallFn  func() string
	logger     *slog.Logger
}

// NewForwarder creates a Forwarder. ourCallFn is called each time a frame is
// submitted so callsign changes (runtime edit) take effect without a restart.
func NewForwarder(sink txgovernor.TxSink, ourCallFn func() string, logger *slog.Logger) *Forwarder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Forwarder{sink: sink, ourCallFn: ourCallFn, logger: logger}
}

// CanForwardMessage is the pure eligibility check for a county alert message.
// All five conditions must pass for the packet to be forwarded. Returns (true,
// ForwardOK) when eligible. The changed flag, when true, bypasses the interval
// check so a status/type transition is forwarded immediately.
func CanForwardMessage(
	cfg *configstore.WeatherConfig,
	county *County,
	alert *AlertEntry,
	prefs map[string]bool,
	myLat, myLon float64,
	now time.Time,
	changed bool,
) (bool, ForwardResult) {
	if cfg == nil || !cfg.Enabled {
		return false, ForwardDisabled
	}
	if cfg.TxChannelID == 0 {
		return false, ForwardNoChannel
	}
	if !prefs[county.FIPS] {
		return false, ForwardCountyNotEnabled
	}
	dist := haversineDistanceMi(myLat, myLon, county.Lat, county.Lon)
	if dist > cfg.MaxDistanceMiles {
		return false, ForwardTooFar
	}
	if !changed {
		minInterval := time.Duration(cfg.MinIntervalSeconds) * time.Second
		if !alert.LastTX.IsZero() && now.Sub(alert.LastTX) < minInterval {
			return false, ForwardTooSoon
		}
	}
	return true, ForwardOK
}

// CanForwardPosition is the eligibility check for an NWS position object.
// Distance and interval are evaluated; county prefs are not (positions do
// not carry county codes). changed bypasses the interval check.
func CanForwardPosition(
	cfg *configstore.WeatherConfig,
	posLat, posLon float64,
	myLat, myLon float64,
	lastTX time.Time,
	now time.Time,
	changed bool,
) (bool, ForwardResult) {
	if cfg == nil || !cfg.Enabled {
		return false, ForwardDisabled
	}
	if cfg.TxChannelID == 0 {
		return false, ForwardNoChannel
	}
	dist := haversineDistanceMi(myLat, myLon, posLat, posLon)
	if dist > cfg.MaxDistanceMiles {
		return false, ForwardTooFar
	}
	if !changed {
		minInterval := time.Duration(cfg.MinIntervalSeconds) * time.Second
		if !lastTX.IsZero() && now.Sub(lastTX) < minInterval {
			return false, ForwardTooSoon
		}
	}
	return true, ForwardOK
}

// ForwardPacket wraps pkt in APRS third-party format and submits it to the TX
// governor on channel. Returns ForwardOK on success.
func (f *Forwarder) ForwardPacket(ctx context.Context, channel uint32, pkt *aprs.DecodedAPRSPacket) ForwardResult {
	if pkt == nil {
		return ForwardNoPacket
	}
	ourCall := f.ourCallFn()
	if ourCall == "" {
		f.logger.Warn("weather: no station callsign configured, skipping forward")
		return ForwardBuildError
	}

	// Reconstruct an AX.25 frame from the raw bytes if available.
	frame, err := buildFrameFromPacket(pkt, ourCall)
	if err != nil {
		f.logger.Warn("weather: build tx frame failed", "err", err, "src", pkt.Source)
		return ForwardBuildError
	}

	wrapped, err := igate.WrapThirdParty(frame, ourCall, nil)
	if err != nil {
		f.logger.Warn("weather: wrap third-party failed", "err", err)
		return ForwardBuildError
	}

	submitCtx, cancel := context.WithTimeout(ctx, submitTimeout)
	defer cancel()

	err = f.sink.Submit(submitCtx, channel, wrapped, txgovernor.SubmitSource{
		Kind:      submitKind,
		Detail:    fmt.Sprintf("src=%s", pkt.Source),
		Priority:  txgovernor.PriorityIGateMsg,
		SkipDedup: false,
	})
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			f.logger.Warn("weather: TX submit failed", "err", err, "channel", channel)
		}
		return ForwardSubmitError
	}
	f.logger.Info("weather: forwarded NWS packet to RF", "channel", channel, "src", pkt.Source)
	return ForwardOK
}

// buildFrameFromPacket reconstructs an AX.25 UI frame from a decoded APRS
// packet. It uses pkt.Raw when available (lossless), otherwise re-encodes
// from the decoded fields.
func buildFrameFromPacket(pkt *aprs.DecodedAPRSPacket, igateCall string) (*ax25.Frame, error) {
	if len(pkt.Raw) > 0 {
		f, err := ax25.Decode(pkt.Raw)
		if err == nil {
			return f, nil
		}
	}
	// Fallback: build a frame from the decoded source/dest/info. The info
	// field is re-derived from the raw packet if available.
	src, err := ax25.ParseAddress(pkt.Source)
	if err != nil {
		return nil, fmt.Errorf("parse source %q: %w", pkt.Source, err)
	}
	dest := pkt.Dest
	if dest == "" {
		dest = aprsGatewayToCall
	}
	dst, err := ax25.ParseAddress(dest)
	if err != nil {
		return nil, fmt.Errorf("parse dest %q: %w", dest, err)
	}
	path := make([]ax25.Address, 0, len(pkt.Path))
	for _, p := range pkt.Path {
		a, err := ax25.ParseAddress(p)
		if err != nil {
			continue
		}
		path = append(path, a)
	}

	// Re-encode the info field from the decoded message or object.
	info := buildInfoFromPacket(pkt, igateCall)
	return ax25.NewUIFrame(src, dst, path, info)
}

// buildInfoFromPacket re-encodes the APRS info field from the decoded packet.
// This is a best-effort fallback for when pkt.Raw is not available.
func buildInfoFromPacket(pkt *aprs.DecodedAPRSPacket, _ string) []byte {
	if pkt.Message != nil {
		// Re-encode message: ":ADDRESSEE:text{id"
		addr := pkt.Message.Addressee
		// Pad addressee to 9 chars per APRS101 §14.1.
		if len(addr) < 9 {
			addr = addr + strings.Repeat(" ", 9-len(addr))
		}
		var b bytes.Buffer
		b.WriteByte(':')
		b.WriteString(addr)
		b.WriteByte(':')
		b.WriteString(pkt.Message.Text)
		if pkt.Message.MessageID != "" {
			b.WriteByte('{')
			b.WriteString(pkt.Message.MessageID)
		}
		return b.Bytes()
	}
	// For position and object packets we have no reliable re-encoding path
	// without the raw bytes. Return an empty info byte so the third-party
	// wrapper at least contains a structurally valid frame.
	return []byte{}
}
