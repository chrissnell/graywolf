package messages

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// Preflight is the inbound-message preflight: a shared cache of
// (from, msg_id, text_hash) tuples plus the transport for auto-ACKs.
// Both messages.Router and actions.Classifier consult the same
// instance so an @@-prefixed packet that the classifier consumes
// still gets ACKed and dedup-suppressed exactly the way a normal
// message would. APRS101 §14.2 — every copy is acked even when the
// original was already deduped.
type Preflight struct {
	cfg PreflightConfig

	logger    *slog.Logger
	clock     RouterClock
	dedupWin  time.Duration
	autoAckCh atomic.Uint32

	dedupMu  sync.Mutex
	dedupMap map[string]time.Time

	mAutoAckSent prometheus.Counter
	mDedupHits   prometheus.Counter
}

// PreflightConfig captures the preflight's collaborators.
type PreflightConfig struct {
	// OurCall returns our primary callsign (possibly with SSID). Required.
	OurCall func() string
	// TxSink is the governor used to submit RF auto-ACK frames. Required.
	TxSink txgovernor.TxSink
	// IGateSender is the IS-side line sender used to mirror auto-ACKs
	// when the inbound was IS-sourced. Optional — IS auto-ACKs are
	// skipped when nil.
	IGateSender IGateLineSender
	// Logger is optional; nil falls back to slog.Default().
	Logger *slog.Logger
	// Registerer is optional; nil disables metric registration but the
	// counters are still created so callers can read them in tests.
	Registerer prometheus.Registerer
	// Clock is optional; nil falls back to wall clock.
	Clock RouterClock
	// AutoAckChannel is the RF channel used when submitting auto-ACKs
	// for IS-sourced inbound. Defaults to 1.
	AutoAckChannel uint32
	// DedupWindow overrides the (from, msg_id, text_hash) dedup window.
	// <= 0 falls back to DefaultRouterDedupWindow.
	DedupWindow time.Duration
}

// NewPreflight constructs a Preflight from cfg. Returns an error if
// any required field is missing.
func NewPreflight(cfg PreflightConfig) (*Preflight, error) {
	if cfg.OurCall == nil {
		return nil, errors.New("messages: preflight requires OurCall")
	}
	if cfg.TxSink == nil {
		return nil, errors.New("messages: preflight requires TxSink")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clock := cfg.Clock
	if clock == nil {
		clock = realRouterClock{}
	}
	win := cfg.DedupWindow
	if win <= 0 {
		win = DefaultRouterDedupWindow
	}
	ch := cfg.AutoAckChannel
	if ch == 0 {
		ch = 1
	}
	p := &Preflight{
		cfg:      cfg,
		logger:   logger,
		clock:    clock,
		dedupWin: win,
		dedupMap: make(map[string]time.Time),
	}
	p.autoAckCh.Store(ch)
	if err := p.initMetrics(cfg.Registerer); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Preflight) initMetrics(reg prometheus.Registerer) error {
	p.mAutoAckSent = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "messages_preflight_autoack_sent_total",
		Help: "Auto-ACK frames submitted by the messages preflight.",
	})
	p.mDedupHits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "messages_preflight_dedup_hits_total",
		Help: "Inbound APRS message packets suppressed by the preflight (from,msgid,text_hash) dedup window.",
	})
	if reg == nil {
		return nil
	}
	for _, c := range []prometheus.Collector{p.mAutoAckSent, p.mDedupHits} {
		if err := reg.Register(c); err != nil {
			are := prometheus.AlreadyRegisteredError{}
			if !errors.As(err, &are) {
				return err
			}
		}
	}
	return nil
}

// AutoAckChannel returns the live RF channel ID used for auto-ACKs
// when the inbound was IS-sourced. Reads are lock-free.
func (p *Preflight) AutoAckChannel() uint32 { return p.autoAckCh.Load() }

// SetAutoAckChannel updates the IS-fallback auto-ACK channel. Zero is
// ignored. Safe to call concurrently.
func (p *Preflight) SetAutoAckChannel(ch uint32) {
	if ch == 0 {
		return
	}
	p.autoAckCh.Store(ch)
}

// DedupHits returns the live dedup-hit metric so callers can read it
// in tests without standing up a registry.
func (p *Preflight) DedupHits() prometheus.Counter { return p.mDedupHits }

// AutoAcksSent returns the live auto-ACK counter for tests.
func (p *Preflight) AutoAcksSent() prometheus.Counter { return p.mAutoAckSent }

// CheckDedup consults the (from, msg_id, text_hash) cache. Returns
// true on a hit. Always records the current tuple so the next
// identical packet within the window also hits. Expired entries are
// evicted during the pass.
func (p *Preflight) CheckDedup(fromCall, msgID, text string) bool {
	key := preflightDedupKey(fromCall, msgID, text)
	p.dedupMu.Lock()
	defer p.dedupMu.Unlock()
	now := p.clock.Now()
	for k, exp := range p.dedupMap {
		if now.After(exp) {
			delete(p.dedupMap, k)
		}
	}
	exp, hit := p.dedupMap[key]
	p.dedupMap[key] = now.Add(p.dedupWin)
	if hit && !now.After(exp) {
		p.mDedupHits.Inc()
		return true
	}
	return false
}

func preflightDedupKey(fromCall, msgID, text string) string {
	h := sha1.Sum([]byte(text))
	return strings.ToUpper(fromCall) + "|" + msgID + "|" + hex.EncodeToString(h[:8])
}

// SendAutoAck builds and submits an auto-ACK for an inbound message.
// The ack follows the path the message arrived on: RF inbound acks
// over RF (on the receiving channel when known, configured fallback
// otherwise); IS inbound acks via IGateSender. Empty msgID is a
// no-op. Mirroring an IS-sourced ack onto RF would waste local
// airtime on a channel the correspondent cannot hear.
func (p *Preflight) SendAutoAck(
	ctx context.Context,
	pkt *aprs.DecodedAPRSPacket,
	peerCall, msgID string,
) {
	if msgID == "" {
		return
	}
	ourCall := p.cfg.OurCall()
	if ourCall == "" {
		p.logger.Debug("preflight skipping auto-ACK: our_call empty")
		return
	}
	if pkt.Direction == aprs.DirectionIS {
		if p.cfg.IGateSender == nil {
			return
		}
		line := preflightAckTNC2(ourCall, peerCall, msgID)
		if err := p.cfg.IGateSender.SendLine(line); err != nil {
			p.logger.Debug("preflight auto-ACK IS mirror failed",
				"error", err, "peer", peerCall, "msgid", msgID)
			return
		}
		p.mAutoAckSent.Inc()
		return
	}
	frame, err := preflightAckFrame(ourCall, peerCall, msgID)
	if err != nil {
		p.logger.Warn("preflight auto-ACK encode failed",
			"error", err, "peer", peerCall, "msgid", msgID)
		return
	}
	ch := p.autoAckCh.Load()
	if pkt.Direction == aprs.DirectionRF && pkt.Channel > 0 {
		ch = uint32(pkt.Channel)
	}
	src := txgovernor.SubmitSource{
		Kind:      "messages-autoack",
		Priority:  txgovernor.PriorityIGateMsg,
		SkipDedup: true,
	}
	if err := p.cfg.TxSink.Submit(ctx, ch, frame, src); err != nil {
		p.logger.Warn("preflight auto-ACK submit failed",
			"error", err, "peer", peerCall, "msgid", msgID)
		return
	}
	p.mAutoAckSent.Inc()
}

func preflightAckFrame(ourCall, peerCall, msgID string) (*ax25.Frame, error) {
	info, err := aprs.EncodeMessageAck(peerCall, msgID)
	if err != nil {
		return nil, err
	}
	src, err := ax25.ParseAddress(ourCall)
	if err != nil {
		return nil, fmt.Errorf("messages: ack source: %w", err)
	}
	dest, err := ax25.ParseAddress("APGRWO")
	if err != nil {
		return nil, fmt.Errorf("messages: ack dest: %w", err)
	}
	return ax25.NewUIFrame(src, dest, nil, info)
}

func preflightAckTNC2(ourCall, peerCall, msgID string) string {
	addr := peerCall
	if len(addr) > 9 {
		addr = addr[:9]
	}
	if len(addr) < 9 {
		addr = addr + strings.Repeat(" ", 9-len(addr))
	}
	info := ":" + addr + ":ack" + msgID
	return aprs.FormatTNC2(ourCall, "APGRWO", nil, []byte(info))
}
