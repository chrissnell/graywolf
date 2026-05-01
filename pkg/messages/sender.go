package messages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// SubmitSourceKind values used by the messages sender. The router
// uses "messages-autoack" for DM auto-acks; the sender uses
// "messages" for operator-originated outbound. TxHook consumers
// registered by the Service filter on these values.
const (
	SubmitKindMessages        = "messages"
	SubmitKindMessagesAutoAck = "messages-autoack"
)

// SendPath identifies which transport a SendResult describes.
type SendPath string

const (
	SendPathRF   SendPath = "rf"
	SendPathIS   SendPath = "is"
	SendPathBoth SendPath = "both"
	SendPathNone SendPath = ""
)

// SendResult describes the outcome of one Sender.Send call. Path is
// the transport that accepted (or failed) the outbound; Err is nil
// on success. Retryable is true when the caller (RetryManager) should
// re-arm the row for a later attempt rather than marking it failed.
type SendResult struct {
	Path      SendPath
	Err       error
	Retryable bool
}

// ShortRetryDelay is the grace period before the sender retries an
// outbound that hit a transient queue-full. It does NOT count against
// the attempt budget — the retry manager uses it as a back-pressure
// sleep, not a backoff step.
const ShortRetryDelay = 5 * time.Second

// ErrMessageTextTooLong is returned by Sender.Send when the row's Text
// exceeds the effective per-message cap from MessagePreferences. The
// sender is the authoritative gate — REST DTO validation is early-
// reject feedback, but this error catches APRS-IS routed, bot-
// originated, and retry-resend paths that don't pass through the
// webapi validator. Non-retryable: mutating the body requires a new
// compose.
var ErrMessageTextTooLong = errors.New("messages: text exceeds effective length cap")

// SenderClock abstracts time for deterministic tests. Mirrors
// RouterClock semantics so a single fakeClock drives all of messages.
type SenderClock interface {
	Now() time.Time
}

// RFAvailability reports whether the RF modem is currently considered
// reachable. *modembridge.Bridge satisfies it via IsRunning().
type RFAvailability interface {
	IsRunning() bool
}

// alwaysRF is the no-op RFAvailability used when the caller doesn't
// inject a bridge (e.g. tests that never exercise the RF fallback).
type alwaysRF struct{}

func (alwaysRF) IsRunning() bool { return true }

// SenderConfig captures the sender's collaborators. All fields except
// Logger, Clock, Bridge, and IGate are required.
type SenderConfig struct {
	Store       *Store
	TxSink      txgovernor.TxSink
	IGateSender IGateLineSender // may be nil when operator runs no igate
	Bridge      RFAvailability  // may be nil in tests
	LocalTxRing *LocalTxRing
	Preferences *Preferences
	EventHub    *EventHub
	Logger      *slog.Logger
	Clock       SenderClock
	// TxChannel is the RF channel used for outbound submissions.
	// Defaults to 1 (matches MessagesConfig.TxChannel semantics).
	TxChannel uint32
	// ChannelModes refuses outbound when the resolved TX channel mode
	// is "packet". Nil = treat every channel as APRS-permissive
	// (preserves pre-Phase-0 behavior). Lookup errors are silently
	// ignored (fail-open at TX time; the operator's configured channel
	// wins on transient DB issues).
	ChannelModes configstore.ChannelModeLookup
	// IGatePasscode is the APRS-IS passcode; "-1" indicates read-only
	// and disables IS transmits so the sender can short-circuit the
	// IS fallback when the operator hasn't provisioned credentials.
	// Empty string is treated the same as absent ("-1" implicit).
	IGatePasscode string
}

// Sender is the outbound message pipeline. One instance per Service.
// Send() is stateless re-entrant — callers (the REST compose path
// and the retry manager) may call it from multiple goroutines.
type Sender struct {
	cfg SenderConfig

	logger *slog.Logger
	clock  SenderClock
	bridge RFAvailability
	// txChannel is the live RF channel used for outbound submissions.
	// Mutable at runtime via SetTxChannel so an iGate-config save can
	// retarget messaging without restarting the daemon. cfg.TxChannel
	// seeds the initial value at NewSender; concurrent callers (Send,
	// retry manager, REST compose) read it lock-free.
	txChannel atomic.Uint32
	// pending tracks frames submitted via this sender, keyed by frame
	// pointer. The TxHook looks up the row id by frame pointer to
	// flip SentAt. The governor guarantees it invokes the hook with
	// the same *ax25.Frame pointer we submitted.
	pendingMu sync.Mutex
	pending   map[*ax25.Frame]pendingFrame
}

// TxChannel returns the live RF channel ID used for outbound
// submissions. Reads are lock-free and observe the most recent
// SetTxChannel mutation.
func (s *Sender) TxChannel() uint32 { return s.txChannel.Load() }

// SetTxChannel updates the live TX channel. A zero value is ignored
// (matches NewSender's default-to-1 behaviour for unset configs).
// Safe to call concurrently with Send.
func (s *Sender) SetTxChannel(ch uint32) {
	if ch == 0 {
		return
	}
	s.txChannel.Store(ch)
}

// pendingFrame is the metadata recorded at Submit time and consumed
// by the TxHook. MsgID + PeerCall + RowID uniquely identify the row
// in case two sends race against each other.
type pendingFrame struct {
	RowID      uint64
	MsgID      string
	PeerCall   string
	ThreadKind string
}

// NewSender validates cfg and returns a ready Sender. Returns an
// error if any required field is missing.
func NewSender(cfg SenderConfig) (*Sender, error) {
	if cfg.Store == nil {
		return nil, errors.New("messages: Sender requires Store")
	}
	if cfg.TxSink == nil {
		return nil, errors.New("messages: Sender requires TxSink")
	}
	if cfg.LocalTxRing == nil {
		return nil, errors.New("messages: Sender requires LocalTxRing")
	}
	if cfg.Preferences == nil {
		return nil, errors.New("messages: Sender requires Preferences")
	}
	if cfg.EventHub == nil {
		return nil, errors.New("messages: Sender requires EventHub")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clock := cfg.Clock
	if clock == nil {
		clock = realRouterClock{}
	}
	bridge := cfg.Bridge
	if bridge == nil {
		bridge = alwaysRF{}
	}
	if cfg.TxChannel == 0 {
		cfg.TxChannel = 1
	}
	s := &Sender{
		cfg:     cfg,
		logger:  logger,
		clock:   clock,
		bridge:  bridge,
		pending: make(map[*ax25.Frame]pendingFrame),
	}
	s.txChannel.Store(cfg.TxChannel)
	return s, nil
}

// Send dispatches a single outbound attempt for row. The row must
// already be persisted with Direction="out", ThreadKind populated,
// MsgID allocated, and AckState=="none". Send updates the row
// (QueuedAt, Attempts, FailureReason) via store.Update on terminal
// paths; the retry manager owns the next-retry bookkeeping.
//
// Returns a SendResult describing the outcome. RetryManager decides
// whether to re-arm based on Result.Retryable.
func (s *Sender) Send(ctx context.Context, row *configstore.Message) SendResult {
	if row == nil {
		return SendResult{Err: errors.New("messages: Send requires non-nil row")}
	}
	if row.Direction != "out" {
		return SendResult{Err: errors.New("messages: Send requires direction=out")}
	}
	if row.MsgID == "" && row.ThreadKind == ThreadKindDM {
		return SendResult{Err: errors.New("messages: DM outbound requires msg_id")}
	}

	// Authoritative length gate. Applies to every outbound addressee-
	// line frame (DM + tactical) regardless of entry point — REST
	// compose, retry manager, operator resend, and any future bot or
	// IS-routed path share this check. The DTO validator handles early-
	// reject for REST; this gate catches the rest so the 67/200 policy
	// is enforced in exactly one place.
	if maxText := s.cfg.Preferences.EffectiveMaxMessageText(); len(row.Text) > maxText {
		row.FailureReason = truncReason(fmt.Sprintf("text exceeds %d-char cap (got %d)", maxText, len(row.Text)))
		_ = s.cfg.Store.Update(ctx, row)
		return SendResult{Err: fmt.Errorf("%w: %d > %d", ErrMessageTextTooLong, len(row.Text), maxText), Retryable: false}
	}

	policy := NormalizeFallbackPolicy(s.cfg.Preferences.Current().FallbackPolicy)
	rfAvailable := s.rfAvailable()

	switch policy {
	case FallbackPolicyRFOnly:
		return s.sendRF(ctx, row, rfAvailable, false)
	case FallbackPolicyISOnly:
		return s.sendIS(ctx, row)
	case FallbackPolicyBoth:
		return s.sendBoth(ctx, row, rfAvailable)
	case FallbackPolicyISFallback:
		fallthrough
	default:
		return s.sendRFWithISFallback(ctx, row, rfAvailable)
	}
}

// sendRF attempts an RF submission. If allowFallback is true and the
// RF path is unavailable (or the governor rejects synchronously), the
// caller upgrades to IS; otherwise this is a single-shot RF attempt.
//
// Precedence: length cap (caller) -> packet-mode refusal (here) -> RF
// availability -> governor submit. The packet-mode refusal must come
// before the rfAvailable check because a misconfigured TxChannel is an
// operator error that shouldn't be hidden by a transient bridge stop.
func (s *Sender) sendRF(ctx context.Context, row *configstore.Message, rfAvailable, allowFallback bool) SendResult {
	if s.cfg.ChannelModes != nil {
		mode, _ := s.cfg.ChannelModes.ModeForChannel(ctx, s.txChannel.Load())
		if mode == configstore.ChannelModePacket {
			err := errors.New("messages: tx channel is packet-mode")
			row.FailureReason = truncReason(err.Error())
			_ = s.cfg.Store.Update(ctx, row)
			return SendResult{Path: SendPathRF, Err: err, Retryable: false}
		}
	}
	if !rfAvailable {
		err := errors.New("messages: RF unavailable")
		return s.finalizeRFFailure(ctx, row, err, allowFallback)
	}
	frame, err := s.buildFrame(row)
	if err != nil {
		row.FailureReason = truncReason(fmt.Sprintf("encode: %v", err))
		_ = s.cfg.Store.Update(ctx, row)
		return SendResult{Path: SendPathRF, Err: err}
	}
	// Record (source, msg_id) in the LocalTxRing BEFORE submit so a
	// simultaneous re-heard digi copy is recognized as local.
	s.cfg.LocalTxRing.Add(row.FromCall, row.MsgID)

	// Register pending metadata before Submit so the TxHook can look
	// it up after the governor's send loop fires it. The hook removes
	// the entry on fire; we remove it ourselves on submit failure.
	s.recordPending(frame, row)

	// SkipDedup=true: APRS101 §14.3 specifies retransmission of the
	// same frame (same source/dest/info + msgid) until an ack arrives.
	// The governor's 30s dedup window would silently swallow the first
	// retry — identical bytes, tight window. That broke retry semantics
	// in practice: operators saw a 60s gap to the *second* retry
	// instead of 30s to the first, because attempt 1 was eaten on the
	// wire. Opting out of dedup is correct for message TX; we keep the
	// governor's other admission controls (rate limits, DCD-aware
	// CSMA, queue capacity) active.
	submitErr := s.cfg.TxSink.Submit(ctx, s.txChannel.Load(), frame, txgovernor.SubmitSource{
		Kind:      SubmitKindMessages,
		Priority:  txgovernor.PriorityIGateMsg,
		SkipDedup: true,
	})
	if submitErr == nil {
		// Governor accepted. SentAt flips later when the TxHook fires
		// — until then the row is "tx_submitted" implicitly (attempts
		// incremented, no SentAt, no NextRetryAt). Retryable=true for
		// DM so the retry manager enrolls while we wait for an ack.
		//
		// Use ClearFailureReason (field-selective UPDATE) rather than
		// Store.Update (whole-row Save). The TxHook can fire on a
		// concurrent goroutine between our Submit returning and this
		// write — if we Save'd the full in-memory row here, our
		// stale SentAt=nil would clobber the hook's SentAt write and
		// the row would appear permanently un-sent. Touching only
		// failure_reason avoids the conflict.
		row.FailureReason = ""
		if err := s.cfg.Store.ClearFailureReason(ctx, row.ID); err != nil {
			s.logger.Warn("messages sender clear-reason after queue failed", "error", err, "id", row.ID)
		}
		return SendResult{Path: SendPathRF, Err: nil, Retryable: row.ThreadKind == ThreadKindDM}
	}

	// Submit failed — remove from pending so stale entries don't leak.
	s.clearPending(frame)

	switch {
	case errors.Is(submitErr, txgovernor.ErrQueueFull):
		// Transient back-pressure. Caller schedules a ShortRetryDelay
		// retry outside the attempt budget.
		row.FailureReason = truncReason("governor queue full")
		_ = s.cfg.Store.Update(ctx, row)
		return SendResult{Path: SendPathRF, Err: submitErr, Retryable: true}
	case errors.Is(submitErr, txgovernor.ErrStopped):
		// Governor shut down — RF will not recover on its own.
		return s.finalizeRFFailure(ctx, row, submitErr, allowFallback)
	default:
		row.FailureReason = truncReason(fmt.Sprintf("submit: %v", submitErr))
		_ = s.cfg.Store.Update(ctx, row)
		return SendResult{Path: SendPathRF, Err: submitErr, Retryable: row.ThreadKind == ThreadKindDM}
	}
}

// finalizeRFFailure records a terminal RF failure on row. When
// allowFallback is true, the caller upgrades to IS; otherwise the
// failure is surfaced immediately.
func (s *Sender) finalizeRFFailure(ctx context.Context, row *configstore.Message, cause error, allowFallback bool) SendResult {
	reason := fmt.Sprintf("rf unavailable: %v", cause)
	row.FailureReason = truncReason(reason)
	_ = s.cfg.Store.Update(ctx, row)
	if allowFallback {
		return SendResult{Path: SendPathRF, Err: cause, Retryable: false}
	}
	return SendResult{Path: SendPathRF, Err: cause, Retryable: false}
}

// sendIS submits the row to APRS-IS via the IGateLineSender. IS
// sends are single-shot — the IS server does not provide a retry
// signal, and RetryManager does NOT enroll IS-only outbounds in the
// DM retry budget.
func (s *Sender) sendIS(ctx context.Context, row *configstore.Message) SendResult {
	if s.cfg.IGateSender == nil {
		err := errors.New("messages: iGate not configured")
		row.FailureReason = truncReason(err.Error())
		_ = s.cfg.Store.Update(ctx, row)
		return SendResult{Path: SendPathIS, Err: err}
	}
	if s.readOnlyIS() {
		err := errors.New("messages: iGate passcode is read-only (-1)")
		row.FailureReason = truncReason(err.Error())
		_ = s.cfg.Store.Update(ctx, row)
		return SendResult{Path: SendPathIS, Err: err}
	}
	line := buildMessageTNC2(row)
	if err := s.cfg.IGateSender.SendLine(line); err != nil {
		row.FailureReason = truncReason(fmt.Sprintf("igate: %v", err))
		_ = s.cfg.Store.Update(ctx, row)
		return SendResult{Path: SendPathIS, Err: err, Retryable: false}
	}
	// IS accepted. Flip SentAt inline — IS has no TxHook analogue
	// and we treat the SendLine return as sent-on-write.
	now := s.clock.Now()
	sent := now
	row.SentAt = &sent
	row.FailureReason = ""
	// For tactical, IS-only send goes terminal broadcast; for DM we
	// stay in awaiting_ack — reply-acks arrive on whichever path
	// the peer chooses.
	if row.ThreadKind == ThreadKindTactical {
		row.AckState = AckStateBroadcast
	}
	if err := s.cfg.Store.Update(ctx, row); err != nil {
		s.logger.Warn("messages sender IS-update failed", "error", err, "id", row.ID)
	}
	// Emit a sent event so the REST compose handler can observe
	// delivery. For RF, the TxHook emits its own message.sent_rf
	// event when the hook fires.
	s.cfg.EventHub.Publish(Event{
		Type:       EventMessageSentIS,
		MessageID:  row.ID,
		ThreadKind: row.ThreadKind,
		ThreadKey:  row.ThreadKey,
		Timestamp:  now,
	})
	return SendResult{Path: SendPathIS, Err: nil, Retryable: false}
}

// sendBoth fans out to RF and IS in parallel on the first attempt.
// On subsequent attempts (re-entered by the retry manager) the caller
// decides whether to re-fan or narrow to RF-only — the sender does
// not track attempt state directly, it relies on the RetryManager.
func (s *Sender) sendBoth(ctx context.Context, row *configstore.Message, rfAvailable bool) SendResult {
	rf := s.sendRF(ctx, row, rfAvailable, false)
	// Re-read the row in case sendRF mutated persisted state. We pass
	// a shallow copy so IS updates don't clobber RF timestamps.
	rowForIS := *row
	is := s.sendIS(ctx, &rowForIS)

	// Merge transient state from IS send back into row so the caller
	// observes sent_at when IS accepts and RF was queued.
	if is.Err == nil {
		row.SentAt = rowForIS.SentAt
		if row.ThreadKind == ThreadKindTactical {
			row.AckState = AckStateBroadcast
		}
		_ = s.cfg.Store.Update(ctx, row)
	}

	switch {
	case rf.Err == nil && is.Err == nil:
		return SendResult{Path: SendPathBoth, Retryable: rf.Retryable}
	case rf.Err == nil:
		// RF success, IS failure. Still "succeeded" — caller treats
		// IS failure as an annotated warning in the row.
		return SendResult{Path: SendPathBoth, Retryable: rf.Retryable}
	case is.Err == nil:
		return SendResult{Path: SendPathIS, Retryable: false}
	default:
		// Both failed. Propagate the RF error; IS was additive.
		return SendResult{Path: SendPathBoth, Err: rf.Err, Retryable: rf.Retryable}
	}
}

// sendRFWithISFallback runs RF first; if RF is unavailable OR RF
// submit fails definitively, falls back to IS. Transient (queue-full)
// RF failures do NOT trigger fallback — they stay on RF for the short
// retry.
func (s *Sender) sendRFWithISFallback(ctx context.Context, row *configstore.Message, rfAvailable bool) SendResult {
	// If RF is already unavailable at decision time, skip straight to IS.
	if !rfAvailable {
		return s.sendIS(ctx, row)
	}
	result := s.sendRF(ctx, row, true, true)
	// If RF succeeded (queue accepted) return as-is — IS does not
	// duplicate the send. Retry manager will re-try RF on ack
	// timeout.
	if result.Err == nil {
		return result
	}
	// Transient queue full — stay on RF for the short retry.
	if errors.Is(result.Err, txgovernor.ErrQueueFull) {
		return result
	}
	// RF definitively failed (stopped / encode error / unavailable).
	// Fall through to IS.
	return s.sendIS(ctx, row)
}

// readOnlyIS reports whether the configured IS passcode is "-1"
// (or empty, which we treat as read-only for safety).
func (s *Sender) readOnlyIS() bool {
	p := strings.TrimSpace(s.cfg.IGatePasscode)
	return p == "" || p == "-1"
}

// rfAvailable folds the bridge's IsRunning into the sender's view.
// Returns true when the caller didn't inject a bridge (tests).
func (s *Sender) rfAvailable() bool {
	if s.bridge == nil {
		return true
	}
	return s.bridge.IsRunning()
}

// buildFrame constructs the AX.25 UI frame carrying the APRS message
// for row. Uses APGRWO as destination (graywolf's software identifier)
// — matches the router's auto-ACK convention.
func (s *Sender) buildFrame(row *configstore.Message) (*ax25.Frame, error) {
	info, err := aprs.EncodeMessage(row.ToCall, row.Text, row.MsgID)
	if err != nil {
		return nil, fmt.Errorf("messages: encode: %w", err)
	}
	src, err := ax25.ParseAddress(row.FromCall)
	if err != nil {
		return nil, fmt.Errorf("messages: source %q: %w", row.FromCall, err)
	}
	dest, err := ax25.ParseAddress("APGRWO")
	if err != nil {
		return nil, fmt.Errorf("messages: dest: %w", err)
	}
	// Digipeater path: the row does not persist a path; outbound
	// uses the preferences DefaultPath. Parse once per send — the
	// path rarely changes and this keeps the sender stateless.
	path, err := parsePath(s.cfg.Preferences.Current().DefaultPath)
	if err != nil {
		return nil, fmt.Errorf("messages: path: %w", err)
	}
	return ax25.NewUIFrame(src, dest, path, info)
}

// parsePath splits a comma-separated path string into addresses.
// Empty input yields nil (no digipeater path).
func parsePath(p string) ([]ax25.Address, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return nil, nil
	}
	parts := strings.Split(p, ",")
	out := make([]ax25.Address, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		addr, err := ax25.ParseAddress(part)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", part, err)
		}
		out = append(out, addr)
	}
	return out, nil
}

// buildMessageTNC2 renders the TNC-2 text line for APRS-IS. Uses
// APGRWO as the destination (software identifier) and TCPIP as the
// digipeater path (APRS-IS convention for locally-originated traffic).
// Addressee is padded to 9 chars.
func buildMessageTNC2(row *configstore.Message) string {
	addr := row.ToCall
	if len(addr) > 9 {
		addr = addr[:9]
	}
	if len(addr) < 9 {
		addr = addr + strings.Repeat(" ", 9-len(addr))
	}
	body := ":" + addr + ":" + row.Text
	if row.MsgID != "" {
		body = body + "{" + row.MsgID
	}
	return fmt.Sprintf("%s>APGRWO,TCPIP*%s", row.FromCall, body)
}

// recordPending stores metadata keyed by frame pointer for the TxHook.
func (s *Sender) recordPending(frame *ax25.Frame, row *configstore.Message) {
	s.pendingMu.Lock()
	s.pending[frame] = pendingFrame{
		RowID:      row.ID,
		MsgID:      row.MsgID,
		PeerCall:   row.PeerCall,
		ThreadKind: row.ThreadKind,
	}
	s.pendingMu.Unlock()
}

// clearPending removes frame's pending entry without consuming it.
// Used when Submit returns an error.
func (s *Sender) clearPending(frame *ax25.Frame) {
	s.pendingMu.Lock()
	delete(s.pending, frame)
	s.pendingMu.Unlock()
}

// onTxComplete is the TxHook body. Looks up the row by frame pointer,
// flips SentAt, transitions to awaiting_ack (DM) or broadcast
// (tactical), and emits a sent_rf event. Safe to call for frames we
// did not originate — the lookup misses and we return immediately.
func (s *Sender) onTxComplete(_ uint32, frame *ax25.Frame, src txgovernor.SubmitSource) {
	if src.Kind != SubmitKindMessages {
		return
	}
	s.pendingMu.Lock()
	pf, ok := s.pending[frame]
	if ok {
		delete(s.pending, frame)
	}
	s.pendingMu.Unlock()
	if !ok {
		// Not our frame, or the row-update race already consumed it.
		return
	}

	ctx := context.Background()
	// Look up the row to learn ThreadKind for the event publish (and
	// to decide whether to flip ack_state to "broadcast" for tactical).
	// The actual write uses a field-selective UPDATE so it doesn't
	// clobber concurrent writes — scheduleNext is racing us to set
	// next_retry_at on this same row, and a whole-row Save on either
	// side would overwrite the other's field.
	row, err := s.cfg.Store.GetByID(ctx, pf.RowID)
	if err != nil {
		s.logger.Warn("messages sender tx-hook row lookup failed",
			"error", err, "id", pf.RowID)
		return
	}
	now := s.clock.Now()
	ackState := ""
	if row.ThreadKind == ThreadKindTactical {
		// Tactical: terminal broadcast state. DM stays AckStateNone
		// ("awaiting ack") — retry manager and ack correlation take it
		// from here.
		ackState = AckStateBroadcast
	}
	if err := s.cfg.Store.UpdateSentAtAndAckState(ctx, row.ID, now, ackState); err != nil {
		s.logger.Warn("messages sender tx-hook update failed",
			"error", err, "id", row.ID)
		return
	}
	s.cfg.EventHub.Publish(Event{
		Type:       EventMessageSentRF,
		MessageID:  row.ID,
		ThreadKind: row.ThreadKind,
		ThreadKey:  row.ThreadKey,
		Timestamp:  now,
	})
}

// truncReason clips a failure reason to the FailureReason column's
// max length (128 chars) so a long error doesn't hit a DB constraint.
func truncReason(s string) string {
	const max = 128
	if len(s) <= max {
		return s
	}
	return s[:max]
}
