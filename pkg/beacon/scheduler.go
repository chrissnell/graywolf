// Package beacon implements the graywolf beacon scheduler: position,
// object, tracker, custom, and igate beacons driven by the configstore
// `beacons` table, with optional SmartBeaconing for tracker beacons and
// safe `comment_cmd` execution for dynamic comments.
//
// All outgoing frames are submitted through a TxSink (satisfied by
// *txgovernor.Governor in production) at PriorityBeacon.
package beacon

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/gps"
)

// TxSink is the transmit contract the scheduler needs; matches the
// Submit signature of txgovernor.Governor so no adapter is required in
// production.
type TxSink interface {
	Submit(ctx context.Context, channel uint32, frame *ax25.Frame, src SubmitSource) error
}

// SubmitSource mirrors txgovernor.SubmitSource but is defined locally
// so the beacon package does not import txgovernor (avoiding a cycle if
// txgovernor ever wants to import beacon types). cmd/graywolf wraps
// governor.Submit with a tiny shim that translates fields.
type SubmitSource struct {
	Kind     string // "beacon"
	Detail   string // beacon type + id
	Priority int
}

// Type enumerates the supported beacon kinds.
type Type string

const (
	TypePosition Type = "position"
	TypeObject   Type = "object"
	TypeTracker  Type = "tracker"
	TypeCustom   Type = "custom"
	TypeIGate    Type = "igate"
)

// Config describes one beacon entry from the beacons table. Fields match
// the SQL schema in .context/graywolf-implementation-plan.md §beacons.
type Config struct {
	ID          uint32
	Type        Type
	Channel     uint32 // send_to parsed as channel number (IG/APP handled by caller)
	Source      ax25.Address
	Dest        ax25.Address
	Path        []ax25.Address
	Delay       time.Duration // initial delay
	Every       time.Duration // periodic interval
	Slot        int           // seconds past the hour; -1 means unset
	Lat, Lon    float64       // fixed position
	AltFt       float64
	SymbolTable byte
	SymbolCode  byte
	Comment     string
	CommentCmd  []string // already-split argv; empty = static comment
	Messaging   bool
	ObjectName  string     // for TypeObject
	CustomInfo  string     // for TypeCustom (raw info field override)
	SmartBeacon *SmartBeaconConfig // non-nil + .Enabled → use for tracker
	Enabled     bool
}

// Observer is an optional hook for metrics. Scheduler calls these on
// beacon send; nil methods are skipped.
type Observer interface {
	OnBeaconSent(beaconType Type)
	OnSmartBeaconRate(channel uint32, interval time.Duration)
}

// Clock abstracts time for deterministic tests.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Scheduler runs one goroutine per beacon entry plus one for
// SmartBeaconing turn detection if any tracker beacons exist.
type Scheduler struct {
	sink     TxSink
	cache    gps.PositionCache
	logger   *slog.Logger
	observer Observer
	clock    Clock

	mu       sync.Mutex
	beacons  []Config
	reloadCh chan struct{}
}

// Options configures a Scheduler.
type Options struct {
	Sink     TxSink
	Cache    gps.PositionCache // may be nil for fixed/igate-only deployments
	Logger   *slog.Logger
	Observer Observer
	Clock    Clock // defaults to wall clock
}

// New constructs a Scheduler.
func New(opts Options) (*Scheduler, error) {
	if opts.Sink == nil {
		return nil, fmt.Errorf("beacon: nil sink")
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
		sink:     opts.Sink,
		cache:    opts.Cache,
		logger:   logger.With("component", "beacon"),
		observer: opts.Observer,
		clock:    clock,
		reloadCh: make(chan struct{}, 1),
	}, nil
}

// SetBeacons replaces the beacon list. If Run is active, call Reload
// instead to also restart per-beacon goroutines with the new config.
func (s *Scheduler) SetBeacons(b []Config) {
	s.mu.Lock()
	s.beacons = append([]Config(nil), b...)
	s.mu.Unlock()
}

// Reload atomically swaps in a new beacon list and signals Run to cancel
// the currently-running per-beacon goroutines and re-spawn them from the
// new config. Safe to call from any goroutine; non-blocking — coalesces
// rapid successive calls into one re-spawn cycle.
func (s *Scheduler) Reload(b []Config) {
	s.SetBeacons(b)
	select {
	case s.reloadCh <- struct{}{}:
	default:
	}
}

// SendNow finds the beacon with the given id in the current beacon list
// and transmits it once immediately, independently of its scheduled
// interval. Returns an error if the id is not present. The Enabled flag
// is intentionally ignored — operators may want to test a beacon that
// is otherwise disabled.
func (s *Scheduler) SendNow(ctx context.Context, id uint32) error {
	s.mu.Lock()
	var found *Config
	for i := range s.beacons {
		if s.beacons[i].ID == id {
			b := s.beacons[i]
			found = &b
			break
		}
	}
	s.mu.Unlock()
	if found == nil {
		return fmt.Errorf("beacon: id %d not found", id)
	}
	s.sendBeacon(ctx, *found)
	return nil
}

// Run launches one goroutine per enabled beacon and blocks until ctx is
// cancelled. On Reload, the current goroutines are cancelled and a fresh
// generation is spawned from the latest beacons slice.
func (s *Scheduler) Run(ctx context.Context) error {
	for {
		genCtx, cancel := context.WithCancel(ctx)
		done := s.runGeneration(genCtx)

		select {
		case <-ctx.Done():
			cancel()
			<-done
			return nil
		case <-s.reloadCh:
			s.logger.Info("beacon scheduler reloading")
			cancel()
			<-done
			// Drain any extra reload signals that arrived during shutdown
			// so we don't immediately re-cycle on the next iteration.
			select {
			case <-s.reloadCh:
			default:
			}
		case <-done:
			// All beacons exited on their own (none enabled, or all errored
			// out). Wait for ctx or a reload before deciding what to do.
			cancel()
			select {
			case <-ctx.Done():
				return nil
			case <-s.reloadCh:
				s.logger.Info("beacon scheduler reloading")
			}
		}
	}
}

// runGeneration spawns one goroutine per enabled beacon and returns a
// channel closed when all of them have exited. The caller is responsible
// for cancelling genCtx to make them exit.
func (s *Scheduler) runGeneration(genCtx context.Context) <-chan struct{} {
	s.mu.Lock()
	beacons := append([]Config(nil), s.beacons...)
	s.mu.Unlock()

	var wg sync.WaitGroup
	for i := range beacons {
		b := beacons[i]
		if !b.Enabled {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.runBeacon(genCtx, b)
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	return done
}

// runBeacon drives one beacon entry's schedule and send loop.
func (s *Scheduler) runBeacon(ctx context.Context, b Config) {
	// Initial delay (optionally overridden by slot alignment).
	initial := b.Delay
	if b.Slot >= 0 && b.Slot < 3600 {
		initial = timeToNextSlot(s.clock.Now(), b.Slot)
	}
	if initial < 0 {
		initial = 0
	}
	select {
	case <-ctx.Done():
		return
	case <-s.clock.After(initial):
	}

	// Tracker beacons with SmartBeaconing enabled use a dynamic interval
	// and corner-pegging driven by GPS updates. Other beacons use the
	// fixed `every` interval.
	smart := b.Type == TypeTracker && b.SmartBeacon != nil && b.SmartBeacon.Enabled && s.cache != nil
	if smart {
		s.runTrackerSmart(ctx, b)
		return
	}

	// Fixed-interval loop.
	s.sendBeacon(ctx, b)
	interval := b.Every
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.clock.After(interval):
		}
		s.sendBeacon(ctx, b)
	}
}

// runTrackerSmart implements SmartBeaconing: the beacon interval is
// recomputed on every GPS update; corner pegging fires early when the
// heading delta exceeds the speed-dependent threshold.
func (s *Scheduler) runTrackerSmart(ctx context.Context, b Config) {
	cfg := *b.SmartBeacon
	var (
		lastHeading float64
		lastSend    = s.clock.Now()
	)
	s.sendBeacon(ctx, b)
	fix, ok := s.cache.Get()
	if ok {
		lastHeading = fix.Heading
	}

	for {
		fix, _ := s.cache.Get()
		interval := cfg.Interval(fix.Speed)
		if s.observer != nil {
			s.observer.OnSmartBeaconRate(b.Channel, interval)
		}

		// Sleep in small slices so turn detection remains responsive.
		slice := 1 * time.Second
		if interval < slice {
			slice = interval
		}
		select {
		case <-ctx.Done():
			return
		case <-s.clock.After(slice):
		}

		now := s.clock.Now()
		fix, _ = s.cache.Get()
		elapsed := now.Sub(lastSend)

		// Corner pegging: heading change exceeds threshold and turn_time
		// has elapsed since last beacon.
		if fix.HasCourse && elapsed >= cfg.TurnTime {
			delta := HeadingDelta(lastHeading, fix.Heading)
			if delta >= cfg.TurnThreshold(fix.Speed) {
				s.sendBeacon(ctx, b)
				lastSend = now
				lastHeading = fix.Heading
				continue
			}
		}

		// Fixed-rate trigger: elapsed ≥ current interval.
		if elapsed >= interval {
			s.sendBeacon(ctx, b)
			lastSend = now
			if fix.HasCourse {
				lastHeading = fix.Heading
			}
		}
	}
}

// sendBeacon builds and submits one beacon frame.
func (s *Scheduler) sendBeacon(ctx context.Context, b Config) {
	info, err := s.buildInfo(ctx, b)
	if err != nil {
		s.logger.Warn("beacon build", "id", b.ID, "type", b.Type, "err", err)
		return
	}
	frame, err := ax25.NewUIFrame(b.Source, b.Dest, b.Path, []byte(info))
	if err != nil {
		s.logger.Warn("beacon frame", "id", b.ID, "err", err)
		return
	}
	src := SubmitSource{
		Kind:     "beacon",
		Detail:   fmt.Sprintf("%s/%d", b.Type, b.ID),
		Priority: ax25.PriorityBeacon,
	}
	if err := s.sink.Submit(ctx, b.Channel, frame, src); err != nil {
		s.logger.Warn("beacon submit", "id", b.ID, "err", err)
		return
	}
	s.logger.Info("beacon sent", "id", b.ID, "type", b.Type, "channel", b.Channel, "info", info)
	if s.observer != nil {
		s.observer.OnBeaconSent(b.Type)
	}
}

// buildInfo constructs the APRS info field for b, including optional
// comment_cmd stdout appended to the static comment.
func (s *Scheduler) buildInfo(ctx context.Context, b Config) (string, error) {
	comment := b.Comment
	if len(b.CommentCmd) > 0 {
		out, err := RunCommentCmd(ctx, b.CommentCmd, 5*time.Second)
		if err != nil {
			s.logger.Warn("comment_cmd failed", "id", b.ID, "err", err)
			// Fall through with static comment.
		} else if out != "" {
			if comment != "" {
				comment = comment + " " + out
			} else {
				comment = out
			}
		}
	}

	switch b.Type {
	case TypePosition, TypeIGate:
		altM := b.AltFt / 3.28084
		return PositionInfo(b.Lat, b.Lon, 0, 0, altM, b.SymbolTable, b.SymbolCode, b.Messaging, comment), nil

	case TypeTracker:
		if s.cache == nil {
			return "", fmt.Errorf("tracker beacon without GPS cache")
		}
		fix, ok := s.cache.Get()
		if !ok {
			return "", fmt.Errorf("tracker beacon: no GPS fix available")
		}
		course := 0
		if fix.HasCourse {
			course = int(fix.Heading)
			if course == 0 {
				course = 360 // APRS encodes 0 as 360 per spec
			}
		}
		altM := 0.0
		if fix.HasAlt {
			altM = fix.Altitude
		}
		return PositionInfo(fix.Latitude, fix.Longitude, course, fix.Speed, altM, b.SymbolTable, b.SymbolCode, b.Messaging, comment), nil

	case TypeObject:
		if b.ObjectName == "" {
			return "", fmt.Errorf("object beacon missing object_name")
		}
		return ObjectInfo(b.ObjectName, true, "", b.Lat, b.Lon, b.SymbolTable, b.SymbolCode, comment), nil

	case TypeCustom:
		if b.CustomInfo == "" {
			return "", fmt.Errorf("custom beacon missing info field")
		}
		if comment != "" {
			return b.CustomInfo + comment, nil
		}
		return b.CustomInfo, nil
	}
	return "", fmt.Errorf("unknown beacon type %q", b.Type)
}

// timeToNextSlot returns the duration until the next occurrence of the
// given "seconds past the hour" boundary.
func timeToNextSlot(now time.Time, slot int) time.Duration {
	if slot < 0 {
		return 0
	}
	slot = slot % 3600
	sec := now.Minute()*60 + now.Second()
	diff := slot - sec
	if diff <= 0 {
		diff += 3600
	}
	return time.Duration(diff)*time.Second - time.Duration(now.Nanosecond())
}

// Jitter adds random +/- half-interval jitter to prevent multiple
// schedulers from beaconing in lock-step. Reserved for Phase 4 carry
// over — currently unused.
var _ = func(r *rand.Rand, d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	return d + time.Duration(r.Int63n(int64(d/10)))
}

// cleanCall is reserved for cmd/graywolf's config mapping (strip
// whitespace before ParseAddress). Kept here so future edits land in the
// same file.
func cleanCall(s string) string { return strings.TrimSpace(s) }

var _ = cleanCall
