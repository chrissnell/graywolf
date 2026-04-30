package messages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sync"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/stationcache"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// tacticalCallsignRe is the canonical APRS tactical-callsign regex:
// 1-9 uppercase alnum or hyphen. Kept local to the service so the
// invite validator and any future tactical-keyed send path agree on
// one shape. Matches the wire regex in pkg/messages/invite.go.
var tacticalCallsignRe = regexp.MustCompile(`^[A-Z0-9-]{1,9}$`)

// ErrInvalidInvite indicates SendMessage was invoked with Kind=invite
// but InviteTactical was absent or malformed. Returned to handlers so
// they can surface a 400 to REST callers.
var ErrInvalidInvite = errors.New("messages: invite requires a valid invite_tactical")

// ServiceConfigReader is the narrow read/write surface the Service
// needs from *configstore.Store. Kept as an interface so tests can
// inject a fake.
type ServiceConfigReader interface {
	MessagePreferencesReader
	ListEnabledTacticalCallsigns(ctx context.Context) ([]configstore.TacticalCallsign, error)
}

// ServiceConfig wires the Service constructor.
type ServiceConfig struct {
	Store         *Store
	ConfigStore   ServiceConfigReader
	TxSink        txgovernor.TxSink
	TxHookReg     txgovernor.TxHookRegistry
	IGate         IGateLineSender // may be nil (no iGate configured)
	Bridge        RFAvailability  // may be nil in tests (alwaysRF)
	StationCache  stationcache.StationStore // optional — Phase 4 autocomplete
	Logger        *slog.Logger
	Clock         SenderClock
	// TxChannel is the RF channel used for outbound messages.
	// Defaults to 1 when zero.
	TxChannel uint32
	// TxChannelResolver, if non-nil, is invoked by ReloadConfig to
	// fetch the live TX channel ID. The resolver should return zero
	// when no usable channel is available; ReloadConfig then leaves
	// the current value unchanged. Used by the app-level reload path
	// to push iGate-config changes (TxChannel field) into the running
	// Sender + Router without a daemon restart.
	TxChannelResolver func(context.Context) uint32
	// IGatePasscode lets the sender short-circuit the IS fallback
	// when the operator runs a read-only iGate ("-1").
	IGatePasscode string
	// OurCall returns the operator's primary callsign (possibly with
	// SSID). The router uses it for self-filter and auto-ACK; the
	// sender doesn't read it directly — rows carry FromCall.
	OurCall func() string
	// AutoAckChannel overrides the router's auto-ACK channel. Zero
	// falls back to TxChannel.
	AutoAckChannel uint32
	// LocalTxRing / TacticalSet / EventHub can be injected for tests
	// that need to observe them; if nil the Service constructs its
	// own defaults.
	LocalTxRing *LocalTxRing
	TacticalSet *TacticalSet
	EventHub    *EventHub
}

// Service is the top-level messages component. It owns Router,
// Sender, RetryManager, Preferences, TacticalSet, LocalTxRing, and
// EventHub. Lifecycle: NewService → Start(ctx) → (use) → Stop().
//
// Start registers the TxHook with the governor and loads initial
// preferences + tactical callsigns into the cached snapshots. Stop
// unregisters the TxHook and stops Router + RetryManager.
type Service struct {
	cfg ServiceConfig

	logger *slog.Logger
	ctx    context.Context

	router  *Router
	sender  *Sender
	retry   *RetryManager
	prefs   *Preferences
	hub     *EventHub
	ring    *LocalTxRing
	tactSet *TacticalSet

	// TxHook unregister closure — nil before Start, set in Start.
	unregTxHook func()

	startOnce sync.Once
	stopOnce  sync.Once
}

// NewService constructs a Service from cfg. Returns an error if any
// required field is missing. Does NOT start any goroutines — callers
// invoke Start(ctx) after wiring dependencies.
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Store == nil {
		return nil, errors.New("messages: Service requires Store")
	}
	if cfg.ConfigStore == nil {
		return nil, errors.New("messages: Service requires ConfigStore")
	}
	if cfg.TxSink == nil {
		return nil, errors.New("messages: Service requires TxSink")
	}
	if cfg.TxHookReg == nil {
		return nil, errors.New("messages: Service requires TxHookReg")
	}
	if cfg.OurCall == nil {
		return nil, errors.New("messages: Service requires OurCall")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clock := cfg.Clock
	if clock == nil {
		clock = realRouterClock{}
	}

	ring := cfg.LocalTxRing
	if ring == nil {
		ring = NewLocalTxRing(DefaultLocalTxRingSize, DefaultLocalTxRingTTL)
	}
	tactSet := cfg.TacticalSet
	if tactSet == nil {
		tactSet = NewTacticalSet()
	}
	hub := cfg.EventHub
	if hub == nil {
		hub = NewEventHub(DefaultSubscriberBuffer)
	}

	prefs := NewPreferences(cfg.ConfigStore)
	if prefs == nil {
		return nil, errors.New("messages: Service failed to construct Preferences")
	}

	txCh := cfg.TxChannel
	if txCh == 0 {
		txCh = 1
	}
	autoAckCh := cfg.AutoAckChannel
	if autoAckCh == 0 {
		autoAckCh = txCh
	}

	sender, err := NewSender(SenderConfig{
		Store:         cfg.Store,
		TxSink:        cfg.TxSink,
		IGateSender:   cfg.IGate,
		Bridge:        cfg.Bridge,
		LocalTxRing:   ring,
		Preferences:   prefs,
		EventHub:      hub,
		Logger:        logger.With("component", "messages-sender"),
		Clock:         clock,
		TxChannel:     txCh,
		IGatePasscode: cfg.IGatePasscode,
	})
	if err != nil {
		return nil, err
	}

	retry, err := NewRetryManager(RetryManagerConfig{
		Store:       cfg.Store,
		Sender:      sender,
		Preferences: prefs,
		EventHub:    hub,
		Logger:      logger.With("component", "messages-retry"),
		Clock:       clock,
	})
	if err != nil {
		return nil, err
	}

	router, err := NewRouter(RouterConfig{
		Store:          cfg.Store,
		TxSink:         cfg.TxSink,
		IGateSender:    cfg.IGate,
		OurCall:        cfg.OurCall,
		LocalTxRing:    ring,
		TacticalSet:    tactSet,
		EventHub:       hub,
		Logger:         logger.With("component", "messages-router"),
		Clock:          clock,
		AutoAckChannel: autoAckCh,
	})
	if err != nil {
		return nil, err
	}

	return &Service{
		cfg:     cfg,
		logger:  logger,
		router:  router,
		sender:  sender,
		retry:   retry,
		prefs:   prefs,
		hub:     hub,
		ring:    ring,
		tactSet: tactSet,
	}, nil
}

// Start wires runtime dependencies:
//  1. Loads preferences + tactical callsigns into cached snapshots.
//  2. Registers the sender's TxHook with the governor.
//  3. Starts the Router and RetryManager goroutines.
//
// Idempotent: a second Start call is a no-op.
func (s *Service) Start(ctx context.Context) error {
	var startErr error
	s.startOnce.Do(func() {
		s.ctx = ctx
		if _, err := s.prefs.Load(ctx); err != nil {
			s.logger.Warn("messages preferences initial load failed", "error", err)
			// Not fatal — Preferences falls back to defaults.
		}
		if err := s.ReloadTacticalCallsigns(ctx); err != nil {
			s.logger.Warn("messages tactical callsigns initial load failed", "error", err)
			// Not fatal.
		}
		_, unreg := s.cfg.TxHookReg.AddTxHook(s.sender.onTxComplete)
		s.unregTxHook = unreg

		s.router.Start(ctx)
		s.retry.Start(ctx)
	})
	return startErr
}

// Stop unregisters the TxHook and stops the Router + RetryManager.
// Idempotent.
func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		if s.unregTxHook != nil {
			s.unregTxHook()
			s.unregTxHook = nil
		}
		s.retry.Stop()
		s.router.Stop()
	})
}

// Router returns the inbound-classification router so Phase 5 wiring
// can append it to the APRS fan-out outputs slice.
func (s *Service) Router() *Router { return s.router }

// Sender returns the outbound sender. REST compose handlers call
// Sender.Send via SendMessage / Resend wrappers exposed directly on
// Service.
func (s *Service) Sender() *Sender { return s.sender }

// RetryManager returns the retry scheduler for tests and the REST
// /resend handler.
func (s *Service) RetryManager() *RetryManager { return s.retry }

// Preferences returns the cached preferences snapshot.
func (s *Service) Preferences() *Preferences { return s.prefs }

// EventHub returns the pub/sub hub so REST SSE handlers can subscribe.
func (s *Service) EventHub() *EventHub { return s.hub }

// LocalTxRing returns the self-filter ring so Phase 5 can inject it
// into the iGate gating filter via the LocalOriginRing interface.
func (s *Service) LocalTxRing() *LocalTxRing { return s.ring }

// TacticalSet returns the live tactical-set snapshot (read-only).
// Tests use it to observe reload results.
func (s *Service) TacticalSet() *TacticalSet { return s.tactSet }

// ReloadConfig re-resolves the TX channel via the configured
// TxChannelResolver and pushes the new value into the live Sender +
// Router. A nil resolver, a zero return, or a value matching the
// current channel is a no-op. Logs a single info line when the value
// actually changes so operators can correlate iGate-config saves
// against the swap.
//
// Called from the app's messagesReload drainer after every iGate
// config save. Concurrency: SetTxChannel / SetAutoAckChannel use
// atomics; safe to call while the Router consumer goroutine and the
// Sender's compose / retry callers are running.
func (s *Service) ReloadConfig(ctx context.Context) error {
	if s.cfg.TxChannelResolver == nil {
		return nil
	}
	ch := s.cfg.TxChannelResolver(ctx)
	if ch == 0 {
		return nil
	}
	prev := s.sender.TxChannel()
	if ch == prev {
		return nil
	}
	s.sender.SetTxChannel(ch)
	// Mirror the constructor's "AutoAckChannel defaults to TxChannel"
	// rule on reload: when the operator never overrode AutoAckChannel
	// explicitly, keep it tracking TxChannel; when they did, leave it
	// pinned. Detection: if the router's current auto-ack channel
	// equals the prior TxChannel, treat it as the default-tracking
	// case and update it.
	//
	// Caveat: this detection is value-based, not intent-based. It is
	// correct only while AutoAckChannel has no separate config surface
	// (today: wireMessages never calls SetAutoAckChannel, so the value
	// always equals the seeded TxChannel). If a future change adds an
	// independent operator knob for AutoAckChannel, replace this with
	// an explicit "overridden" flag — otherwise an operator override
	// equal to a future TxChannel value will be silently overwritten
	// on the next reload.
	if s.router.AutoAckChannel() == prev {
		s.router.SetAutoAckChannel(ch)
	}
	s.logger.Info("messages tx channel updated", "previous", prev, "new", ch)
	return nil
}

// ReloadPreferences refetches the MessagePreferences singleton and
// replaces the cached snapshot. Called by the Phase 4 messagesReload
// channel consumer.
func (s *Service) ReloadPreferences(ctx context.Context) error {
	_, err := s.prefs.Load(ctx)
	if err != nil {
		return err
	}
	// Kick the retry loop so a change to RetryMaxAttempts fires
	// immediately rather than waiting for the next ack timeout.
	s.retry.Kick()
	return nil
}

// ReloadTacticalCallsigns refetches the enabled tactical callsign
// set and swaps it into the router's cache. Called by the Phase 4
// messagesReload channel consumer after a tactical CRUD mutation.
func (s *Service) ReloadTacticalCallsigns(ctx context.Context) error {
	rows, err := s.cfg.ConfigStore.ListEnabledTacticalCallsigns(ctx)
	if err != nil {
		return err
	}
	set := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		set[row.Callsign] = struct{}{}
	}
	s.tactSet.Store(set)
	return nil
}

// SendMessageRequest is the Go-level compose input. The Phase 4 REST
// handler decodes its DTO and calls Service.SendMessage(ctx, req).
type SendMessageRequest struct {
	// To is the destination callsign (DM) or tactical label. Required.
	To string
	// Text is the message body (<=67 APRS chars). Required unless
	// Kind == MessageKindInvite, in which case the service builds the
	// wire body server-side from InviteTactical and the caller's Text
	// is ignored.
	Text string
	// OurCall is the source callsign — usually the operator's
	// primary callsign (possibly with SSID). Required.
	OurCall string
	// ThreadKind is optional; when empty, Service derives it from
	// the tactical set (exact match → tactical, else DM).
	ThreadKind string
	// Kind classifies the outbound row. Empty or "text" is a normal
	// DM/tactical message; "invite" triggers invite semantics below.
	Kind string
	// InviteTactical is the tactical callsign referenced by an
	// invite. Required and validated when Kind == "invite"; ignored
	// otherwise. Uppercase, 1-9 of [A-Z0-9-].
	InviteTactical string
}

// SendMessage persists the outbound row via store.Insert (allocating
// a msgid for DM) and dispatches it via Sender.Send. Returns the
// persisted row with its assigned ID so the REST handler can return
// 202 + echo the row.
//
// The REST handler is responsible for 67-char validation and
// addressee regex validation; Service validates only the minimum
// required to persist a sensible row.
func (s *Service) SendMessage(ctx context.Context, req SendMessageRequest) (*configstore.Message, error) {
	if req.To == "" {
		return nil, errors.New("messages: SendMessage requires To")
	}
	if req.OurCall == "" {
		return nil, errors.New("messages: SendMessage requires OurCall")
	}

	// Invite branch: the server is the single source of truth for the
	// wire body — we validate the tactical and construct
	// `!GW1 INVITE <TAC>` ourselves, discarding any client-supplied
	// Text. This mirrors the inbound ParseInvite contract: the wire
	// format lives in exactly two places (this builder and the
	// parser), both agreeing on the strict grammar.
	bodyKind := MessageKindText
	inviteTactical := ""
	text := req.Text
	if req.Kind == MessageKindInvite {
		tac := req.InviteTactical
		if tac == "" || !tacticalCallsignRe.MatchString(tac) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidInvite, tac)
		}
		bodyKind = MessageKindInvite
		inviteTactical = tac
		text = "!GW1 INVITE " + tac
	} else if text == "" {
		return nil, errors.New("messages: SendMessage requires Text")
	}

	kind := req.ThreadKind
	if kind == "" {
		if s.tactSet.Contains(req.To) {
			kind = ThreadKindTactical
		} else {
			kind = ThreadKindDM
		}
	}

	row := &configstore.Message{
		Direction:      "out",
		OurCall:        req.OurCall,
		FromCall:       req.OurCall,
		ToCall:         req.To,
		Text:           text,
		ThreadKind:     kind,
		AckState:       AckStateNone,
		Unread:         false,
		Kind:           bodyKind,
		InviteTactical: inviteTactical,
	}
	// DM requires a msgid; tactical may omit.
	if kind == ThreadKindDM {
		id, err := s.cfg.Store.AllocateMsgID(ctx, req.To)
		if err != nil {
			return nil, err
		}
		row.MsgID = id
	}
	if err := s.cfg.Store.Insert(ctx, row); err != nil {
		return nil, err
	}
	// Kick off the first send. Don't block the handler on it —
	// Send is synchronous but fast; we return the row ID so the
	// client sees the 202 even on a slow governor.
	//
	// Pass a copy of the row into the goroutine rather than sharing the
	// same pointer with the caller — the REST handler returns and
	// serializes *row concurrently with Sender.Send mutating the row's
	// columns (SentAt, FailureReason, etc. via store.Update → GORM
	// reflect writes). Without the copy, -race flags this as a data
	// race between handler response-serialization and background send.
	rowCopy := *row
	go func() {
		sendCtx := s.ctx
		if sendCtx == nil {
			sendCtx = context.Background()
		}
		result := s.sender.Send(sendCtx, &rowCopy)
		if result.Err != nil {
			s.logger.Warn("messages initial send failed",
				"error", result.Err, "id", rowCopy.ID, "path", result.Path)
		}
		// Enroll in the retry ladder for DM when the first attempt
		// queues successfully or returned a retryable error. Tactical
		// rows do not participate in the retry scheduler.
		if result.Retryable && rowCopy.ThreadKind == ThreadKindDM {
			// Re-read the row — Sender may have flipped fields.
			cur, err := s.cfg.Store.GetByID(sendCtx, rowCopy.ID)
			if err == nil {
				cur.Attempts = 1
				s.retry.scheduleNext(sendCtx, cur)
			}
		}
	}()
	return row, nil
}

// Resend is the REST /resend entry point. Thin wrapper around
// RetryManager.Resend so handlers can stay agnostic of the retry
// plumbing.
func (s *Service) Resend(ctx context.Context, id uint64) (SendResult, error) {
	return s.retry.Resend(ctx, id)
}

// SoftDelete is the REST DELETE entry point. Cancels any pending
// retry then calls store.SoftDelete. Emits a message.deleted event
// so SSE subscribers can prune their UI.
func (s *Service) SoftDelete(ctx context.Context, id uint64) error {
	if err := s.retry.CancelRetry(ctx, id); err != nil {
		// Log and continue — a lookup miss here shouldn't block the
		// tombstone write.
		s.logger.Debug("messages SoftDelete cancel-retry failed",
			"error", err, "id", id)
	}
	row, _ := s.cfg.Store.GetByID(ctx, id)
	if err := s.cfg.Store.SoftDelete(ctx, id); err != nil {
		return err
	}
	if row != nil {
		s.hub.Publish(Event{
			Type:       EventMessageDeleted,
			MessageID:  id,
			ThreadKind: row.ThreadKind,
			ThreadKey:  row.ThreadKey,
		})
	}
	return nil
}

// SoftDeleteThread soft-deletes every message belonging to (kind, key).
// Cancels each row's pending retry and emits one EventMessageDeleted
// per deleted row so SSE subscribers can prune the UI without a full
// rollup refetch. Returns the number of rows deleted (zero is not an
// error — empty thread is a no-op).
func (s *Service) SoftDeleteThread(ctx context.Context, kind, key string) (int, error) {
	ids, err := s.cfg.Store.SoftDeleteByThread(ctx, kind, key)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := s.retry.CancelRetry(ctx, id); err != nil {
			s.logger.Debug("messages SoftDeleteThread cancel-retry failed",
				"error", err, "id", id)
		}
		s.hub.Publish(Event{
			Type:       EventMessageDeleted,
			MessageID:  id,
			ThreadKind: kind,
			ThreadKey:  key,
		})
	}
	return len(ids), nil
}

// MarkRead / MarkUnread proxy to the store for REST handlers.
func (s *Service) MarkRead(ctx context.Context, id uint64) error {
	return s.cfg.Store.MarkRead(ctx, id)
}
func (s *Service) MarkUnread(ctx context.Context, id uint64) error {
	return s.cfg.Store.MarkUnread(ctx, id)
}
