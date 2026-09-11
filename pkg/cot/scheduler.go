package cot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// pollInterval is how often Run checks the store for due CoT targets.
// CoT retransmit delays default to minutes (300s+), so a coarse poll
// costs negligible latency while keeping the scheduler stateless (see
// package doc).
const pollInterval = 5 * time.Second

// Store is the narrow persistence surface Scheduler needs;
// *configstore.Store satisfies it.
type Store interface {
	GetCotTarget(ctx context.Context, id uint32) (*configstore.CotTarget, error)
	DueCotTargets(ctx context.Context, now time.Time) ([]configstore.CotTarget, error)
	RecordCotSend(ctx context.Context, id uint32, sentAt time.Time, nextSendAt *time.Time) error
}

// Options configures a Scheduler.
type Options struct {
	Sink   txgovernor.TxSink
	ISSink beacon.ISSink // optional APRS-IS destination; reuses beacon's interface
	Store  Store
	Logger *slog.Logger
	Clock  Clock // defaults to wall clock

	// ChannelModes gates transmit on packet-mode channels, mirroring
	// every other TX-gated subsystem (beacon/digipeater/igate/messages).
	ChannelModes configstore.ChannelModeLookup
	// AutoChannelResolver resolves Channel == 0 ("Auto") to a live
	// channel at send time, reusing beacon's resolver type/semantics.
	AutoChannelResolver beacon.AutoChannelResolver
	// StationCallsignResolver resolves the station callsign at send
	// time. CoT objects always transmit under the inherited station
	// callsign -- there is no per-target override (per spec).
	StationCallsignResolver func(ctx context.Context) (string, error)
	// OnISSent fires after a CoT frame is successfully uploaded to
	// APRS-IS. The IS leg bypasses the governor's TX hook entirely (an
	// is_only target never reaches txgovernor.Submit), so without this
	// callback an is_only CoT never appears on the local map even though
	// it reached APRS-IS -- mirrors beacon.Options.OnISSent and
	// invariant 57 in docs/wiki/invariants.md. nil = no-op.
	OnISSent func(frame *ax25.Frame, channel uint32)
}

// Scheduler transmits CoT targets: an immediate first send plus a
// finite number of decaying-interval retransmits, all driven by rows
// in the cot_targets table. See the package doc for why this is
// stateless rather than an in-memory heap like pkg/beacon's.
type Scheduler struct {
	sink                    txgovernor.TxSink
	store                   Store
	logger                  *slog.Logger
	clock                   Clock
	channelModes            configstore.ChannelModeLookup
	autoChannelResolver     beacon.AutoChannelResolver
	stationCallsignResolver func(ctx context.Context) (string, error)
	onISSent                func(frame *ax25.Frame, channel uint32)

	mu     sync.Mutex // guards isSink only; every other field is set once at construction
	isSink beacon.ISSink
}

// New constructs a Scheduler.
func New(opts Options) (*Scheduler, error) {
	if opts.Sink == nil {
		return nil, fmt.Errorf("cot: nil sink")
	}
	if opts.Store == nil {
		return nil, fmt.Errorf("cot: nil store")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clock := opts.Clock
	if clock == nil {
		clock = realClock{}
	}
	return &Scheduler{
		sink:                    opts.Sink,
		isSink:                  opts.ISSink,
		store:                   opts.Store,
		logger:                  logger.With("component", "cot"),
		clock:                   clock,
		channelModes:            opts.ChannelModes,
		autoChannelResolver:     opts.AutoChannelResolver,
		stationCallsignResolver: opts.StationCallsignResolver,
		onISSent:                opts.OnISSent,
	}, nil
}

// SetISSink installs the optional APRS-IS sink. Safe to call before or
// during Run (e.g. when the iGate comes up/down at runtime).
func (s *Scheduler) SetISSink(sink beacon.ISSink) {
	s.mu.Lock()
	s.isSink = sink
	s.mu.Unlock()
}

func (s *Scheduler) currentISSink() beacon.ISSink {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isSink
}

// Run polls the store every pollInterval for due targets and sends
// them, until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick sends every currently-due target. A per-target failure is
// logged and left due -- the next tick retries automatically, so a
// transiently-down channel or unset callsign self-heals without any
// special-case retry/backoff logic.
func (s *Scheduler) tick(ctx context.Context) {
	due, err := s.store.DueCotTargets(ctx, s.clock.Now())
	if err != nil {
		s.logger.Warn("cot due targets query", "err", err)
		return
	}
	for _, t := range due {
		if err := s.SendScheduled(ctx, t.ID); err != nil {
			s.logger.Warn("cot scheduled send", "id", t.ID, "name", t.ObjectName, "err", err)
		}
	}
}

// SendScheduled sends target id as a scheduled transmit (RF dedup
// active) and, on success, records the send: increments TxCount,
// stamps FirstSentAt/LastSentAt, and computes the next NextSendAt from
// ComputeOffsets -- or nil once TxCount reaches NumTransmits. Used both
// by tick (for TX2..N) and by the create handler (for the immediate
// TX1).
func (s *Scheduler) SendScheduled(ctx context.Context, id uint32) error {
	t, err := s.store.GetCotTarget(ctx, id)
	if err != nil {
		return err
	}
	if err := s.send(ctx, *t, false, true); err != nil {
		return err
	}

	now := s.clock.Now()
	newCount := t.TxCount + 1
	// Offsets are always relative to the FIRST send, not the previous
	// one -- so the basis is FirstSentAt once it's set, and only this
	// send's own time on the very first (FirstSentAt still nil) call.
	basis := now
	if t.FirstSentAt != nil {
		basis = *t.FirstSentAt
	}
	var next *time.Time
	if newCount < t.NumTransmits {
		offsets := ComputeOffsets(int(t.NumTransmits), time.Duration(t.SecondTxDelaySeconds)*time.Second, t.DecayFactor)
		n := basis.Add(offsets[newCount])
		next = &n
	}
	return s.store.RecordCotSend(ctx, id, now, next)
}

// SendNow transmits target id immediately ("Beacon Now"), bypassing
// the governor's dedup window. It never touches TxCount/NextSendAt --
// a manual resend is invisible to the scheduled-send counter, per spec.
func (s *Scheduler) SendNow(ctx context.Context, id uint32) error {
	t, err := s.store.GetCotTarget(ctx, id)
	if err != nil {
		return err
	}
	return s.send(ctx, *t, true, true)
}

// SendKill transmits a one-shot APRS object-kill report (live=false,
// status byte '_') for target id, then leaves it alone -- the caller
// (a delete handler) removes the row itself. Bypasses the governor's
// dedup window and never touches TxCount/NextSendAt/FirstSentAt, since
// the target won't be scheduled again. Best-effort by design: callers
// should log a failure and proceed with the delete rather than block
// on it, matching the same policy as the immediate first send at
// creation time.
func (s *Scheduler) SendKill(ctx context.Context, id uint32) error {
	t, err := s.store.GetCotTarget(ctx, id)
	if err != nil {
		return err
	}
	return s.send(ctx, *t, true, false)
}

// send builds one CoT object-report frame from t and submits it via
// whichever of the RF/IS legs t.SendPath selects. skipDedup=true
// bypasses the governor's dedup window (manual "Beacon Now" sends).
// live=false renders the APRS101 kill status byte ('_') instead of the
// normal live one ('*') -- see SendKill.
func (s *Scheduler) send(ctx context.Context, t configstore.CotTarget, skipDedup bool, live bool) error {
	if s.stationCallsignResolver == nil {
		return &SendError{Kind: SendErrorCallsign, Err: errors.New("cot: station callsign resolver not configured")}
	}
	call, err := s.stationCallsignResolver(ctx)
	if err != nil {
		return &SendError{Kind: SendErrorCallsign, Err: err}
	}
	source, err := ax25.ParseAddress(call)
	if err != nil {
		return &SendError{Kind: SendErrorEncode, Err: fmt.Errorf("parse station callsign %q: %w", call, err)}
	}
	dest, err := ax25.ParseAddress(t.Destination)
	if err != nil {
		return &SendError{Kind: SendErrorEncode, Err: fmt.Errorf("parse destination %q: %w", t.Destination, err)}
	}
	path, err := parsePath(t.Path)
	if err != nil {
		return &SendError{Kind: SendErrorEncode, Err: err}
	}

	sendRF := t.SendPath != beacon.SendPathISOnly
	sendIS := t.SendPath == beacon.SendPathBoth || t.SendPath == beacon.SendPathISOnly

	channel := t.Channel
	if sendRF && channel == 0 && s.autoChannelResolver != nil {
		channel = s.autoChannelResolver(ctx)
	}
	if sendRF && s.channelModes != nil {
		mode, _ := s.channelModes.ModeForChannel(ctx, channel)
		if mode == configstore.ChannelModePacket {
			return &SendError{Kind: SendErrorChannelMode, Err: fmt.Errorf("channel %d is in packet mode and cannot transmit CoT objects", channel)}
		}
	}

	info := buildInfo(t, s.clock.Now().UTC(), live)
	frame, err := ax25.NewUIFrame(source, dest, path, []byte(info))
	if err != nil {
		return &SendError{Kind: SendErrorEncode, Err: err}
	}

	src := txgovernor.SubmitSource{
		Kind:      "cot",
		Detail:    fmt.Sprintf("cot/%d/%s", t.ID, strings.TrimSpace(t.ObjectName)),
		Priority:  ax25.PriorityBeacon,
		SkipDedup: skipDedup,
	}

	sent := false
	if sendRF {
		if err := s.sink.Submit(ctx, channel, frame, src); err != nil {
			return &SendError{Kind: SendErrorSubmit, Err: err}
		}
		s.logger.Info("cot sent", "id", t.ID, "name", t.ObjectName, "channel", channel, "send_path", t.SendPath, "live", live)
		sent = true
	}

	if sendIS {
		isSink := s.currentISSink()
		if isSink == nil {
			if !sendRF {
				return &SendError{Kind: SendErrorSubmit, Err: errors.New("aprs-is sink not configured for is_only cot target")}
			}
		} else {
			// TCPIP* path convention for self-originated traffic, same
			// as beacon.sendBeaconWith's IS leg -- servers silently
			// drop self-originated packets carrying an RF path.
			line := aprs.FormatTNC2(source.String(), dest.String(), []string{"TCPIP*"}, []byte(info))
			if err := isSink.SendLine(line); err != nil {
				s.logger.Warn("cot aprs-is send", "id", t.ID, "err", err)
				if !sendRF {
					return &SendError{Kind: SendErrorSubmit, Err: err}
				}
			} else {
				s.logger.Info("cot sent to aprs-is", "id", t.ID, "name", t.ObjectName, "send_path", t.SendPath, "live", live)
				sent = true
				// Feed the station cache so an is_only CoT plots on the
				// local map -- this leg never touches the governor's RF
				// TX hook (invariant 57).
				if s.onISSent != nil {
					s.onISSent(frame, channel)
				}
			}
		}
	}

	if !sent {
		return &SendError{Kind: SendErrorSubmit, Err: errors.New("cot: no transmit leg succeeded")}
	}
	return nil
}

// parsePath splits a comma-separated digipeater path string into AX.25
// addresses, mirroring pkg/app/adapters.go's beaconConfigFromStore.
func parsePath(raw string) ([]ax25.Address, error) {
	var path []ax25.Address
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		a, err := ax25.ParseAddress(p)
		if err != nil {
			return nil, fmt.Errorf("parse path %q: %w", p, err)
		}
		path = append(path, a)
	}
	return path, nil
}
