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
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// IGateLineSender is the narrow interface the router uses to mirror
// auto-ACKs back to APRS-IS when the triggering inbound arrived via
// IS. *igate.Igate satisfies this via its SendLine method.
type IGateLineSender interface {
	SendLine(line string) error
}

// RouterClock abstracts time for deterministic tests.
type RouterClock interface {
	Now() time.Time
}

type realRouterClock struct{}

func (realRouterClock) Now() time.Time { return time.Now().UTC() }

// RouterConfig captures the router's collaborators. All fields except
// Logger, Registerer, and Clock are required.
type RouterConfig struct {
	Store         *Store
	TxSink        txgovernor.TxSink
	IGateSender   IGateLineSender
	OurCall       func() string // returns our primary callsign (possibly with SSID)
	LocalTxRing   *LocalTxRing
	TacticalSet   *TacticalSet
	EventHub      *EventHub
	Logger        *slog.Logger
	Registerer    prometheus.Registerer
	Clock         RouterClock
	// AutoAckChannel is the RF channel used when submitting auto-ACKs.
	// Defaults to 1 (mirrors IGateConfig.TxChannel semantics).
	AutoAckChannel uint32
	// QueueCapacity overrides the internal packet-queue capacity. <= 0
	// uses DefaultRouterQueueCapacity.
	QueueCapacity int
	// DedupWindow overrides the (from_call, msg_id, text_hash) dedup
	// window. <= 0 uses DefaultRouterDedupWindow.
	DedupWindow time.Duration
}

// Defaults for the router.
const (
	// DefaultRouterQueueCapacity is the internal bounded-channel size
	// between SendPacket (fan-out producer) and the consumer goroutine.
	// 256 matches the APRS fan-out queue capacity and gives roughly 30
	// seconds of headroom at realistic inbound message rates (a very
	// busy channel tops out at a few msg/s).
	DefaultRouterQueueCapacity = 256

	// DefaultRouterDedupWindow is the window over which
	// (from_call, msg_id, text_hash) tuples are treated as duplicates
	// at the router's pre-insert check.
	DefaultRouterDedupWindow = 30 * time.Second

	// defaultRouterLogThrottle caps the drop-log rate.
	defaultRouterLogThrottle = 10 * time.Second
)

// Router is the inbound-message classification + auto-ACK pipeline.
// It implements aprs.PacketOutput. Start/Stop control the lifecycle
// of the consumer goroutine.
type Router struct {
	cfg RouterConfig

	logger     *slog.Logger
	clock      RouterClock
	queueCap   int
	dedupWin   time.Duration
	autoAckCh  uint32

	queue chan *aprs.DecodedAPRSPacket

	// lifecycle
	startOnce sync.Once
	stopOnce  sync.Once
	stopped   chan struct{}
	wg        sync.WaitGroup
	running   atomic.Bool

	// drop-log throttle.
	lastDropLog atomic.Int64

	// Pre-insert dedup cache: keyed by "from|msgid|texthash" → expiry
	// nanos.
	dedupMu   sync.Mutex
	dedupMap  map[string]time.Time

	// Metrics.
	mDropped       prometheus.Counter
	mClassified    *prometheus.CounterVec
	mAutoAckSent   prometheus.Counter
	mDedupHits     prometheus.Counter
	mAckCorrelated prometheus.Counter
}

// NewRouter constructs a Router from cfg. Returns an error if any
// required field is missing.
func NewRouter(cfg RouterConfig) (*Router, error) {
	if cfg.Store == nil {
		return nil, errors.New("messages: router requires Store")
	}
	if cfg.TxSink == nil {
		return nil, errors.New("messages: router requires TxSink")
	}
	if cfg.OurCall == nil {
		return nil, errors.New("messages: router requires OurCall")
	}
	if cfg.LocalTxRing == nil {
		return nil, errors.New("messages: router requires LocalTxRing")
	}
	if cfg.TacticalSet == nil {
		return nil, errors.New("messages: router requires TacticalSet")
	}
	if cfg.EventHub == nil {
		return nil, errors.New("messages: router requires EventHub")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clock := cfg.Clock
	if clock == nil {
		clock = realRouterClock{}
	}
	qcap := cfg.QueueCapacity
	if qcap <= 0 {
		qcap = DefaultRouterQueueCapacity
	}
	dedupWin := cfg.DedupWindow
	if dedupWin <= 0 {
		dedupWin = DefaultRouterDedupWindow
	}
	ch := cfg.AutoAckChannel
	if ch == 0 {
		ch = 1
	}

	r := &Router{
		cfg:       cfg,
		logger:    logger,
		clock:     clock,
		queueCap:  qcap,
		dedupWin:  dedupWin,
		autoAckCh: ch,
		queue:     make(chan *aprs.DecodedAPRSPacket, qcap),
		stopped:   make(chan struct{}),
		dedupMap:  make(map[string]time.Time),
	}
	if err := r.initMetrics(cfg.Registerer); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Router) initMetrics(reg prometheus.Registerer) error {
	r.mDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "messages_router_dropped_total",
		Help: "Inbound APRS message packets dropped because the router's internal queue was full.",
	})
	r.mClassified = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "messages_router_classified_total",
		Help: "Inbound APRS message packets classified by the router, by outcome.",
	}, []string{"outcome"})
	r.mAutoAckSent = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "messages_router_autoack_sent_total",
		Help: "Auto-ACK frames submitted by the router.",
	})
	r.mDedupHits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "messages_router_dedup_hits_total",
		Help: "Inbound APRS message packets suppressed by the router's pre-insert (from,msgid,text_hash) dedup window.",
	})
	r.mAckCorrelated = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "messages_router_ack_correlated_total",
		Help: "Inbound ack/rej packets that matched an outstanding outbound row.",
	})
	if reg == nil {
		return nil
	}
	collectors := []prometheus.Collector{r.mDropped, r.mClassified, r.mAutoAckSent, r.mDedupHits, r.mAckCorrelated}
	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			are := prometheus.AlreadyRegisteredError{}
			if !errors.As(err, &are) {
				return err
			}
		}
	}
	return nil
}

// Start spins up the consumer goroutine. Idempotent: a second call is
// a no-op.
func (r *Router) Start(ctx context.Context) {
	r.startOnce.Do(func() {
		r.running.Store(true)
		r.wg.Add(1)
		go r.runConsumer(ctx)
	})
}

// Stop closes the queue and waits for the consumer goroutine to
// drain. Idempotent.
func (r *Router) Stop() {
	r.stopOnce.Do(func() {
		r.running.Store(false)
		close(r.stopped)
		close(r.queue)
	})
	r.wg.Wait()
}

// Close satisfies aprs.PacketOutput. Alias for Stop.
func (r *Router) Close() error {
	r.Stop()
	return nil
}

// SendPacket enqueues pkt for classification. Non-blocking: if the
// internal queue is full, the oldest pending packet is dropped and a
// metric advances. Always returns nil so the APRS fan-out never
// stalls on the router.
func (r *Router) SendPacket(ctx context.Context, pkt *aprs.DecodedAPRSPacket) error {
	if pkt == nil {
		return nil
	}
	// Pre-filter cheaply: discard anything that isn't a non-meta
	// message — either direct or wrapped in an APRS101 ch 20 third-party
	// envelope (as produced by an IS→RF gating iGate). Everything else
	// is noise.
	if !isClassifiableMessage(pkt) {
		return nil
	}
	if !r.running.Load() {
		// Router stopped — drop silently to avoid leaking work.
		return nil
	}
	select {
	case r.queue <- pkt:
		return nil
	default:
	}
	// Queue full — drop oldest, enqueue new. The extra unbuffered
	// channel receive may briefly race with the consumer; that's fine
	// — in the worst case the "drop" is actually a successful consume
	// and we still make forward progress.
	select {
	case <-r.queue:
	default:
	}
	select {
	case r.queue <- pkt:
	default:
		// Still full (consumer + another producer). Give up.
	}
	r.mDropped.Inc()
	r.maybeLogDrop()
	return nil
}

// maybeLogDrop emits a throttled log line (at most once per
// defaultRouterLogThrottle) announcing the queue-full drop.
func (r *Router) maybeLogDrop() {
	now := r.clock.Now().UnixNano()
	prev := r.lastDropLog.Load()
	if now-prev < defaultRouterLogThrottle.Nanoseconds() {
		return
	}
	if !r.lastDropLog.CompareAndSwap(prev, now) {
		return
	}
	r.logger.Warn("messages router queue full, dropping inbound packet",
		"queue_cap", r.queueCap)
}

// runConsumer drains r.queue and processes each packet.
func (r *Router) runConsumer(ctx context.Context) {
	defer r.wg.Done()
	for pkt := range r.queue {
		if ctx.Err() != nil {
			// Context cancelled — drain and exit. We still process
			// remaining items so late packets land.
		}
		r.classify(ctx, pkt)
	}
}

// classify runs the full pipeline for one packet.
func (r *Router) classify(ctx context.Context, pkt *aprs.DecodedAPRSPacket) {
	if !isClassifiableMessage(pkt) {
		return
	}

	ourCall := baseCallUpper(r.cfg.OurCall())

	// Third-party unwrap: if Message.Text begins with '}', parse the
	// inner TNC-2 body and re-attribute the source. Outer path/via
	// stay on display fields. We build a shallow-copy view so the
	// caller's pkt is not mutated (the APRS fan-out shares packets
	// across outputs).
	effSource, effMsg := unwrapThirdParty(pkt)

	source := strings.ToUpper(strings.TrimSpace(effSource))
	baseSource := baseCallUpper(effSource)

	// Step 2 — self-filter. Direct base-call match or LocalTxRing hit.
	if ourCall != "" && baseSource == ourCall {
		r.mClassified.WithLabelValues("self_filter").Inc()
		return
	}
	if r.cfg.LocalTxRing.Contains(source, effMsg.MessageID) {
		r.mClassified.WithLabelValues("self_filter").Inc()
		return
	}

	addressee := strings.ToUpper(strings.TrimSpace(effMsg.Addressee))
	baseAddressee := baseCall(addressee)

	// Step 5 — addressee match determines thread_kind.
	var threadKind, threadKey string
	switch {
	case ourCall != "" && baseAddressee == ourCall:
		threadKind = ThreadKindDM
		threadKey = source
	case r.cfg.TacticalSet.Contains(addressee):
		threadKind = ThreadKindTactical
		threadKey = addressee
	default:
		r.mClassified.WithLabelValues("not_for_us").Inc()
		return
	}

	// Step 7 — DM ack/rej correlation happens before Insert. An ack
	// (or rej) does not get its own row; it only flips the original
	// outbound.
	if threadKind == ThreadKindDM && (effMsg.IsAck || effMsg.IsRej) {
		r.correlateAck(ctx, source, effMsg)
		// Fall through: we still need reply-ack correlation? For a
		// pure ack/rej there's no reply-ack trailer on the same msg,
		// but the parser does populate MessageID on ack/rej. No
		// Insert. Auto-ACK is skipped (guarded below).
		r.mClassified.WithLabelValues("ack_or_rej").Inc()
		return
	}

	// Step 3 — Dedup window on (from, msgid, text_hash). A hit skips
	// Insert but still emits auto-ACK (APRS101 §14.2: ack every copy).
	dedupHit := false
	if effMsg.MessageID != "" {
		if r.checkDedup(source, effMsg.MessageID, effMsg.Text) {
			dedupHit = true
			r.mDedupHits.Inc()
		}
	}

	if !dedupHit {
		if err := r.persistInbound(ctx, pkt, source, effMsg, threadKind, threadKey, ourCall); err != nil {
			r.logger.Warn("messages router persist failed",
				"error", err,
				"source", source,
				"addressee", addressee)
			// Fall through — we still want to auto-ACK for DM so the
			// sender doesn't spin on retries, and we still correlate
			// any reply-ack.
		}
	}

	// Step 8 — reply-ack correlation. Works for DM (closes as acked)
	// AND tactical (sets ReceivedByCall without flipping AckState).
	if effMsg.HasReplyAck && effMsg.ReplyAck != "" {
		r.correlateReplyAck(ctx, source, effMsg.ReplyAck)
	}

	// Step 9 — auto-ACK. DM only, has msgid, not bulletin/NWS, not
	// itself an ack/rej (guarded above), and not a tactical match.
	if threadKind == ThreadKindDM &&
		effMsg.MessageID != "" &&
		!effMsg.IsAck && !effMsg.IsRej &&
		!effMsg.IsBulletin && !effMsg.IsNWS {
		r.sendAutoAck(ctx, pkt, source, effMsg.MessageID)
	}

	if dedupHit {
		r.mClassified.WithLabelValues("dedup_hit").Inc()
	} else {
		r.mClassified.WithLabelValues(threadKind).Inc()
	}
}

// persistInbound writes the inbound row, emits a received event, and
// updates classification metrics.
func (r *Router) persistInbound(
	ctx context.Context,
	pkt *aprs.DecodedAPRSPacket,
	source string,
	m *aprs.Message,
	threadKind, threadKey, ourCall string,
) error {
	row := &configstore.Message{
		Direction:  "in",
		OurCall:    ourCall,
		FromCall:   source,
		ToCall:     strings.ToUpper(strings.TrimSpace(m.Addressee)),
		Text:       m.Text,
		MsgID:      m.MessageID,
		Source:     string(pkt.Direction),
		Path:       joinPath(pkt.Path),
		Via:        firstVia(pkt.Path),
		RawTNC2:    string(pkt.Raw),
		Unread:     true,
		IsBulletin: m.IsBulletin,
		IsNWS:      m.IsNWS,
		IsAck:      m.IsAck,
		IsRej:      m.IsRej,
		ReplyAckID: m.ReplyAck,
		ThreadKind: threadKind,
		ThreadKey:  threadKey,
		Kind:       MessageKindText,
	}
	// Detect tactical-invite wire bodies on DM rows only. ParseInvite
	// is strict: valid iff `^!GW1 INVITE <TAC>$`. On match we stamp
	// Kind=invite + InviteTactical so the UI can render the accept
	// affordance; otherwise the row remains a plain text DM.
	if threadKind == ThreadKindDM {
		if tac, ok := ParseInvite(m.Text); ok {
			row.Kind = MessageKindInvite
			row.InviteTactical = tac
		}
	}
	if pkt.Direction == aprs.DirectionRF {
		row.Channel = uint32(pkt.Channel)
	}
	now := r.clock.Now()
	row.CreatedAt = now
	received := now
	row.ReceivedAt = &received

	if err := r.cfg.Store.Insert(ctx, row); err != nil {
		return err
	}
	r.cfg.EventHub.Publish(Event{
		Type:       EventMessageReceived,
		MessageID:  row.ID,
		ThreadKind: row.ThreadKind,
		ThreadKey:  row.ThreadKey,
		Timestamp:  now,
	})
	return nil
}

// correlateAck flips the matching outbound row to acked/rejected and
// emits an event. No-op if no match.
func (r *Router) correlateAck(ctx context.Context, peerCall string, m *aprs.Message) {
	if m.MessageID == "" {
		return
	}
	rows, err := r.cfg.Store.FindOutstandingByMsgID(ctx, m.MessageID, peerCall)
	if err != nil {
		r.logger.Warn("messages router ack correlation lookup failed",
			"error", err, "peer", peerCall, "msgid", m.MessageID)
		return
	}
	state := AckStateAcked
	evtType := EventMessageAcked
	if m.IsRej {
		state = AckStateRejected
		evtType = EventMessageRejected
	}
	now := r.clock.Now()
	for i := range rows {
		row := rows[i]
		// Skip rows already closed — idempotency.
		if row.AckState == state {
			continue
		}
		row.AckState = state
		ackedAt := now
		row.AckedAt = &ackedAt
		if err := r.cfg.Store.Update(ctx, &row); err != nil {
			r.logger.Warn("messages router ack correlation update failed",
				"error", err, "id", row.ID)
			continue
		}
		r.mAckCorrelated.Inc()
		r.cfg.EventHub.Publish(Event{
			Type:       evtType,
			MessageID:  row.ID,
			ThreadKind: row.ThreadKind,
			ThreadKey:  row.ThreadKey,
			Timestamp:  now,
		})
	}
}

// correlateReplyAck handles the APRS11 reply-ack trailer. For DM
// threads the outbound closes as acked (peer_call on the outbound row
// equals the DM peer); for tactical it sets ReceivedByCall without
// changing AckState (stays "broadcast"). Tactical outbound rows have
// PeerCall = OurCall (per store.Insert) because we are the sender, so
// the lookup tries peer_call=source first (DM path) and, on no match,
// peer_call=our_call (tactical path). This keeps the store API narrow
// while handling both thread kinds correctly.
func (r *Router) correlateReplyAck(ctx context.Context, peerCall, replyAckID string) {
	if replyAckID == "" {
		return
	}
	rows, err := r.cfg.Store.FindOutstandingByMsgID(ctx, replyAckID, peerCall)
	if err != nil {
		r.logger.Warn("messages router reply-ack lookup failed",
			"error", err, "peer", peerCall, "msgid", replyAckID)
		return
	}
	// Second lookup keyed on our_call — catches tactical outbound
	// rows whose PeerCall is OurCall by Insert convention.
	ourCall := baseCallUpper(r.cfg.OurCall())
	if ourCall != "" {
		extra, err := r.cfg.Store.FindOutstandingByMsgID(ctx, replyAckID, r.cfg.OurCall())
		if err == nil {
			// Deduplicate by row id in case the caller somehow matched
			// both queries.
			seen := make(map[uint64]struct{}, len(rows))
			for _, r := range rows {
				seen[r.ID] = struct{}{}
			}
			for _, r := range extra {
				if _, ok := seen[r.ID]; ok {
					continue
				}
				rows = append(rows, r)
			}
		}
	}
	now := r.clock.Now()
	for i := range rows {
		row := rows[i]
		switch row.ThreadKind {
		case ThreadKindDM:
			if row.AckState == AckStateAcked {
				continue
			}
			row.AckState = AckStateAcked
			ackedAt := now
			row.AckedAt = &ackedAt
			if err := r.cfg.Store.Update(ctx, &row); err != nil {
				r.logger.Warn("messages router reply-ack DM update failed",
					"error", err, "id", row.ID)
				continue
			}
			r.cfg.EventHub.Publish(Event{
				Type:       EventMessageAcked,
				MessageID:  row.ID,
				ThreadKind: row.ThreadKind,
				ThreadKey:  row.ThreadKey,
				Timestamp:  now,
			})
		case ThreadKindTactical:
			if row.ReceivedByCall != "" {
				continue
			}
			row.ReceivedByCall = peerCall
			if err := r.cfg.Store.Update(ctx, &row); err != nil {
				r.logger.Warn("messages router reply-ack tactical update failed",
					"error", err, "id", row.ID)
				continue
			}
			r.cfg.EventHub.Publish(Event{
				Type:       EventMessageReplyAckRcvd,
				MessageID:  row.ID,
				ThreadKind: row.ThreadKind,
				ThreadKey:  row.ThreadKey,
				Timestamp:  now,
			})
		}
	}
}

// sendAutoAck builds and submits an auto-ACK for an inbound DM. If
// the inbound arrived on IS, the ack is also mirrored to APRS-IS.
func (r *Router) sendAutoAck(
	ctx context.Context,
	pkt *aprs.DecodedAPRSPacket,
	peerCall, msgID string,
) {
	if msgID == "" {
		return
	}
	ourCall := r.cfg.OurCall()
	if ourCall == "" {
		r.logger.Debug("messages router skipping auto-ACK: our_call empty")
		return
	}
	frame, err := buildAckFrame(ourCall, peerCall, msgID)
	if err != nil {
		r.logger.Warn("messages router auto-ACK encode failed",
			"error", err, "peer", peerCall, "msgid", msgID)
		return
	}
	ch := r.autoAckCh
	if pkt.Direction == aprs.DirectionRF && pkt.Channel > 0 {
		ch = uint32(pkt.Channel)
	}
	src := txgovernor.SubmitSource{
		Kind:      "messages-autoack",
		Priority:  txgovernor.PriorityIGateMsg,
		SkipDedup: true,
	}
	if err := r.cfg.TxSink.Submit(ctx, ch, frame, src); err != nil {
		r.logger.Warn("messages router auto-ACK submit failed",
			"error", err, "peer", peerCall, "msgid", msgID)
	} else {
		r.mAutoAckSent.Inc()
	}
	// Mirror via APRS-IS if the inbound arrived on IS and the
	// operator runs an iGate. SendLine returns an error when the
	// iGate is not connected; surface as Debug — losing the mirror
	// does not invalidate the RF ack.
	if pkt.Direction == aprs.DirectionIS && r.cfg.IGateSender != nil {
		line := buildAckTNC2(ourCall, peerCall, msgID)
		if err := r.cfg.IGateSender.SendLine(line); err != nil {
			r.logger.Debug("messages router auto-ACK IS mirror failed",
				"error", err, "peer", peerCall, "msgid", msgID)
		}
	}
}

// buildAckFrame constructs a ready-to-submit ax25.Frame carrying the
// ack info field. Uses APGRWO as the destination (graywolf's APRS
// software identifier) and no digipeater path — auto-ACKs reply
// directly to the sender.
func buildAckFrame(ourCall, peerCall, msgID string) (*ax25.Frame, error) {
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

// buildAckTNC2 renders the TNC-2 text representation of an ack for
// APRS-IS. Mirrors the format APRS-IS expects: SRC>DEST::PEER     :ack123
func buildAckTNC2(ourCall, peerCall, msgID string) string {
	// Pad peer to 9 chars (APRS101 §14.1 addressee field width).
	addr := peerCall
	if len(addr) > 9 {
		addr = addr[:9]
	}
	if len(addr) < 9 {
		addr = addr + strings.Repeat(" ", 9-len(addr))
	}
	return fmt.Sprintf("%s>APGRWO::%s:ack%s", ourCall, addr, msgID)
}

// checkDedup consults the 30-second (from, msgid, text_hash) cache.
// Returns true on a hit. Always records the current tuple so the
// next identical packet within the window also hits. Expired entries
// are evicted during the pass.
func (r *Router) checkDedup(fromCall, msgID, text string) bool {
	key := dedupKey(fromCall, msgID, text)
	r.dedupMu.Lock()
	defer r.dedupMu.Unlock()
	now := r.clock.Now()
	// Lazy eviction: walk the map and drop expired entries. The map
	// stays small (one entry per live dedup window) so a linear pass
	// is cheap.
	for k, exp := range r.dedupMap {
		if now.After(exp) {
			delete(r.dedupMap, k)
		}
	}
	exp, hit := r.dedupMap[key]
	r.dedupMap[key] = now.Add(r.dedupWin)
	if hit && !now.After(exp) {
		return true
	}
	return false
}

func dedupKey(fromCall, msgID, text string) string {
	h := sha1.Sum([]byte(text))
	return strings.ToUpper(fromCall) + "|" + msgID + "|" + hex.EncodeToString(h[:8])
}

// --- helpers -----------------------------------------------------------------

// baseCallUpper returns the uppercased callsign with SSID stripped.
func baseCallUpper(call string) string {
	return baseCall(strings.ToUpper(strings.TrimSpace(call)))
}

// baseCall strips the SSID suffix ("-n") from an already-uppercase
// callsign. Returns s unchanged if no SSID is present.
func baseCall(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '-'); i >= 0 {
		return s[:i]
	}
	return s
}

// joinPath renders pkt.Path (already a []string in TNC-2 form) into a
// comma-separated display string.
func joinPath(p []string) string {
	if len(p) == 0 {
		return ""
	}
	return strings.Join(p, ",")
}

// firstVia returns the last path entry that has been marked repeated
// ("*"), which is the conventional "via" hop in a TNC-2 line.
func firstVia(p []string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if strings.HasSuffix(p[i], "*") {
			return strings.TrimSuffix(p[i], "*")
		}
	}
	return ""
}

// isClassifiableMessage reports whether pkt carries a message the router
// should classify. Accepts direct PacketMessage and APRS101 ch 20
// third-party envelopes whose inner is itself a message (the shape an
// IS→RF gating iGate produces when relaying a directed message onto RF).
func isClassifiableMessage(pkt *aprs.DecodedAPRSPacket) bool {
	if pkt == nil {
		return false
	}
	if pkt.Type == aprs.PacketMessage && pkt.Message != nil && pkt.TelemetryMeta == nil {
		return true
	}
	if pkt.Type == aprs.PacketThirdParty && pkt.ThirdParty != nil {
		inner := pkt.ThirdParty
		if inner.Type == aprs.PacketMessage && inner.Message != nil && inner.TelemetryMeta == nil {
			return true
		}
	}
	return false
}

// unwrapThirdParty returns the effective (source, message) for pkt.
//
// For an APRS101 ch 20 third-party envelope, the outer parser has
// already recursively decoded the inner packet into pkt.ThirdParty and
// we use its source/message directly — the outer source identifies the
// relaying iGate, not the author.
//
// For the edge case where the outer parsed as a PacketMessage but its
// body starts with '}' (a malformed/hand-rolled envelope that slipped
// past the dispatcher), the inner TNC-2 body is parsed here.
//
// If neither applies, returns the original (outer) source and message.
func unwrapThirdParty(pkt *aprs.DecodedAPRSPacket) (string, *aprs.Message) {
	if pkt.Type == aprs.PacketThirdParty && pkt.ThirdParty != nil &&
		pkt.ThirdParty.Message != nil {
		return pkt.ThirdParty.Source, pkt.ThirdParty.Message
	}
	source := pkt.Source
	msg := pkt.Message
	if msg == nil {
		return source, msg
	}
	text := msg.Text
	if !strings.HasPrefix(text, "}") {
		return source, msg
	}
	body := text[1:]
	colon := strings.IndexByte(body, ':')
	if colon < 0 || colon+1 >= len(body) {
		return source, msg
	}
	header := body[:colon]
	innerInfo := body[colon+1:]
	// header looks like "SRC>DEST,path"
	gt := strings.IndexByte(header, '>')
	if gt < 0 || gt == 0 {
		return source, msg
	}
	innerSrc := header[:gt]
	// The inner info field must itself be a message (":AAAA:text...").
	// Anything else falls through with the original source intact. Per
	// APRS101 §14.1 the addressee is 9 chars, but tolerate the short
	// forms some iGates produce by scanning for the closing ':' within
	// the first 10 bytes.
	if len(innerInfo) < 3 || innerInfo[0] != ':' {
		return source, msg
	}
	innerUpper := 10
	if innerUpper >= len(innerInfo) {
		innerUpper = len(innerInfo) - 1
	}
	innerSep := -1
	for i := 2; i <= innerUpper; i++ {
		if innerInfo[i] == ':' {
			innerSep = i
			break
		}
	}
	if innerSep < 0 {
		return source, msg
	}
	addressee := strings.TrimRight(innerInfo[1:innerSep], " ")
	rest := innerInfo[innerSep+1:]
	// Reconstruct a minimal Message view. We reuse the outer flags
	// where possible; the inner text is what matters for
	// classification / auto-ACK. Reply-ack and ack/rej flags are
	// re-derived below.
	out := &aprs.Message{
		Addressee:   addressee,
		Text:        rest,
		MessageID:   "",
		ReplyAck:    "",
		HasReplyAck: false,
		IsAck:       false,
		IsRej:       false,
		IsBulletin:  strings.HasPrefix(addressee, "BLN"),
		IsNWS:       strings.HasPrefix(addressee, "NWS") || strings.HasPrefix(addressee, "SKY") || strings.HasPrefix(addressee, "CWA"),
	}
	// Trailing {id or {id}ackid.
	if brace := strings.LastIndexByte(rest, '{'); brace >= 0 {
		tail := rest[brace+1:]
		if closeIdx := strings.IndexByte(tail, '}'); closeIdx >= 0 {
			out.MessageID = tail[:closeIdx]
			out.ReplyAck = tail[closeIdx+1:]
			out.HasReplyAck = true
			out.Text = rest[:brace]
		} else {
			out.MessageID = tail
			out.Text = rest[:brace]
		}
	}
	// If the unwrapped inner text had no {id trailer, the outer parser
	// most likely consumed it (a well-formed third-party envelope
	// carries the inner msgid at the tail of the whole info field;
	// parseMessage on the outer attaches it to the outer Message). In
	// that case the outer's MessageID morally belongs to the inner
	// message — inherit it so auto-ACK / correlation still work.
	if out.MessageID == "" && msg != nil && msg.MessageID != "" {
		out.MessageID = msg.MessageID
		out.ReplyAck = msg.ReplyAck
		out.HasReplyAck = msg.HasReplyAck
	}
	// ack/rej prefix detection on the rebuilt text.
	switch {
	case strings.HasPrefix(out.Text, "ack"):
		out.IsAck = true
		out.MessageID = out.Text[3:]
		out.Text = ""
	case strings.HasPrefix(out.Text, "rej"):
		out.IsRej = true
		out.MessageID = out.Text[3:]
		out.Text = ""
	}
	return innerSrc, out
}
