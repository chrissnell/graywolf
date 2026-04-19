// Package messages is graywolf's APRS messaging domain. This file
// provides the storage/repository layer on top of configstore's GORM
// DB handle: CRUD on Message rows, conversation rollups for the
// master/detail UI, msgid allocation with per-peer collision skipping,
// ack correlation queries, retry-scheduler feeds, and participant
// extraction for tactical threads.
//
// Phase boundaries: this file owns persistence only. The router (Phase
// 2), sender / retry manager (Phase 3), and REST handlers (Phase 4)
// consume the methods defined here but live in sibling files not yet
// created.
package messages

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// Thread-kind wire values. Kept as string constants so handlers, DTOs,
// and persisted columns all agree on the same literals.
const (
	ThreadKindDM       = "dm"
	ThreadKindTactical = "tactical"
)

// AckState wire values.
const (
	AckStateNone      = "none"
	AckStateAcked     = "acked"
	AckStateRejected  = "rejected"
	AckStateBroadcast = "broadcast"
)

// Folder discriminator for List.
const (
	FolderAll    = "all"
	FolderInbox  = "inbox"
	FolderSent   = "sent"
)

// Sentinel errors surfaced to callers.
var (
	// ErrMsgIDExhausted is returned by AllocateMsgID when every 001..999
	// slot for the target peer is held by an outstanding outbound DM
	// row. The sender treats this as back-pressure (retry later or drop
	// with a user-visible failure).
	ErrMsgIDExhausted = errors.New("messages: no msgid available for peer (all 999 outstanding)")

	// ErrInvalidThreadKind is returned by Insert when a caller provides
	// a ThreadKind not in the accepted set.
	ErrInvalidThreadKind = errors.New("messages: invalid thread_kind")
)

// Store is the message repository. Thin wrapper around *gorm.DB so the
// production path (configstore.Store.DB()) and test path (an
// in-memory configstore.OpenMemory()) share the same code.
type Store struct {
	db *gorm.DB
}

// NewStore constructs a repository over the given GORM handle. Callers
// pass configstore.Store.DB(); tests pass an in-memory DB that already
// ran configstore.Migrate() so the messages table exists.
func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// Filter describes a List query. All fields are optional — an empty
// Filter returns recent messages across all threads.
type Filter struct {
	// Folder filters by direction: "inbox" (in), "sent" (out), or
	// "all" / "" (both).
	Folder string
	// Peer matches the PeerCall column. For DM threads this equals the
	// thread key; for tactical threads it equals the human sender
	// (inbound) or our_call (outbound).
	Peer string
	// ThreadKind + ThreadKey together select a specific thread. Either
	// or both may be empty.
	ThreadKind string
	ThreadKey  string
	// Since restricts results to CreatedAt >= Since. Zero = no bound.
	Since time.Time
	// Cursor is an opaque string produced by a previous List call.
	// When non-empty, results are ordered by (UpdatedAt, ID) ascending
	// strictly greater than the cursor.
	Cursor string
	// UnreadOnly limits to rows with Unread=true.
	UnreadOnly bool
	// Limit caps the result set. Values <= 0 apply the package
	// default (DefaultListLimit).
	Limit int
	// IncludeDeleted includes soft-deleted rows. Defaults to false.
	IncludeDeleted bool
}

// DefaultListLimit is applied when Filter.Limit is non-positive. The
// UI polls on 5 s intervals with a reasonable window and the tests want
// a sane default; 100 is generous without being unbounded.
const DefaultListLimit = 100

// cursor is the decoded form of Filter.Cursor. Encoded as base64 of
// "<updated_at_unix_nanos>:<id>" so both components stay packed into
// one opaque string.
type cursor struct {
	UpdatedAtNanos int64
	ID             uint64
}

func encodeCursor(c cursor) string {
	raw := fmt.Sprintf("%d:%d", c.UpdatedAtNanos, c.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (cursor, error) {
	if s == "" {
		return cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}, fmt.Errorf("messages: decode cursor: %w", err)
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 {
		return cursor{}, fmt.Errorf("messages: malformed cursor")
	}
	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return cursor{}, fmt.Errorf("messages: cursor timestamp: %w", err)
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return cursor{}, fmt.Errorf("messages: cursor id: %w", err)
	}
	return cursor{UpdatedAtNanos: ts, ID: id}, nil
}

// Insert persists a new row. The caller sets Direction, ThreadKind,
// FromCall/ToCall/OurCall, Text, MsgID, Source, Path, etc.; Insert
// fills the derived columns (PeerCall, ThreadKey) based on the tuple:
//
//   - ThreadKind == "dm":  PeerCall/ThreadKey = FromCall for inbound,
//     ToCall for outbound.
//   - ThreadKind == "tactical":
//     ThreadKey = ToCall (outbound) or the addressee already set on
//     ToCall (inbound — router normalizes).
//     PeerCall = FromCall (the actual human sender in both
//     directions; equals OurCall for outbound).
//
// CreatedAt defaults to time.Now() when zero so callers don't need to
// stamp it. Callers that want deterministic timestamps (tests) set it
// explicitly.
func (s *Store) Insert(ctx context.Context, m *configstore.Message) error {
	if m == nil {
		return errors.New("messages: Insert requires a non-nil message")
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	kind := strings.ToLower(strings.TrimSpace(m.ThreadKind))
	if kind == "" {
		kind = ThreadKindDM
	}
	m.ThreadKind = kind

	switch kind {
	case ThreadKindDM:
		if m.Direction == "in" {
			if m.ThreadKey == "" {
				m.ThreadKey = m.FromCall
			}
			m.PeerCall = m.FromCall
		} else {
			if m.ThreadKey == "" {
				m.ThreadKey = m.ToCall
			}
			m.PeerCall = m.ToCall
		}
	case ThreadKindTactical:
		if m.ThreadKey == "" {
			m.ThreadKey = m.ToCall
		}
		// For tactical, PeerCall is the real human sender. Inbound:
		// FromCall is the sender; outbound: FromCall is our_call.
		m.PeerCall = m.FromCall
	default:
		return fmt.Errorf("%w: %q", ErrInvalidThreadKind, m.ThreadKind)
	}
	if m.AckState == "" {
		m.AckState = AckStateNone
	}
	return s.db.WithContext(ctx).Create(m).Error
}

// GetByID returns a single message by primary key. Returns gorm.ErrRecordNotFound
// wrapped when absent so callers can use errors.Is.
func (s *Store) GetByID(ctx context.Context, id uint64) (*configstore.Message, error) {
	var m configstore.Message
	if err := s.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// List returns a page of messages matching the filter. The returned
// cursor points at the last row in the page and can be fed back into a
// subsequent List call to page forward deterministically.
//
// Ordering: (UpdatedAt ASC, ID ASC). UpdatedAt is populated by GORM on
// every Create/Save and also advances when the row is touched by ack
// correlation or retry bookkeeping, so a polling client seeing an
// updated row after the initial sync will find it past its cursor.
func (s *Store) List(ctx context.Context, f Filter) ([]configstore.Message, string, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	q := s.db.WithContext(ctx).Model(&configstore.Message{})
	if f.IncludeDeleted {
		q = q.Unscoped()
	}

	switch strings.ToLower(f.Folder) {
	case FolderInbox:
		q = q.Where("direction = ?", "in")
	case FolderSent:
		q = q.Where("direction = ?", "out")
	}
	if f.Peer != "" {
		q = q.Where("peer_call = ?", f.Peer)
	}
	if f.ThreadKind != "" {
		q = q.Where("thread_kind = ?", f.ThreadKind)
	}
	if f.ThreadKey != "" {
		q = q.Where("thread_key = ?", f.ThreadKey)
	}
	if !f.Since.IsZero() {
		q = q.Where("created_at >= ?", f.Since)
	}
	if f.UnreadOnly {
		q = q.Where("unread = ?", true)
	}
	if f.Cursor != "" {
		c, err := decodeCursor(f.Cursor)
		if err != nil {
			return nil, "", err
		}
		q = q.Where(
			"(strftime('%s', updated_at) * 1000000000 > ?) OR "+
				"(strftime('%s', updated_at) * 1000000000 = ? AND id > ?)",
			c.UpdatedAtNanos, c.UpdatedAtNanos, c.ID,
		)
	}
	q = q.Order("updated_at ASC").Order("id ASC").Limit(limit)

	var out []configstore.Message
	if err := q.Find(&out).Error; err != nil {
		return nil, "", err
	}
	var nextCursor string
	if len(out) > 0 {
		last := out[len(out)-1]
		nextCursor = encodeCursor(cursor{
			UpdatedAtNanos: last.UpdatedAt.Unix() * int64(time.Second/time.Nanosecond),
			ID:             last.ID,
		})
	}
	return out, nextCursor, nil
}

// Update saves changes to an existing row. Callers typically Update
// after flipping AckState/AckedAt/Attempts/NextRetryAt/SentAt; the
// repository does not derive thread identity on update (Insert owns
// that).
func (s *Store) Update(ctx context.Context, m *configstore.Message) error {
	return s.db.WithContext(ctx).Save(m).Error
}

// SoftDelete sets DeletedAt on the row. Retry code clears NextRetryAt
// and removes the row from its in-flight map when a soft-delete races
// with a pending retry; late acks may still flip AckState on the
// tombstoned row (correlation queries use .Unscoped()).
func (s *Store) SoftDelete(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Delete(&configstore.Message{}, id).Error
}

// MarkRead clears Unread on the row.
func (s *Store) MarkRead(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Model(&configstore.Message{}).
		Where("id = ?", id).Update("unread", false).Error
}

// MarkUnread sets Unread on the row.
func (s *Store) MarkUnread(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Model(&configstore.Message{}).
		Where("id = ?", id).Update("unread", true).Error
}

// ConversationSummary is one row per (thread_kind, thread_key) in the
// UI's master pane. The REST layer (Phase 4) wraps this in its DTO.
type ConversationSummary struct {
	ThreadKind       string
	ThreadKey        string
	LastAt           time.Time
	LastSnippet      string
	LastSenderCall   string
	UnreadCount      int
	TotalCount       int
	ParticipantCount int // tactical only; 0 for DM
}

// ConversationRollup returns one summary per thread, ordered by
// LastAt descending (most recent on top). Soft-deleted rows are
// excluded; tactical participant counts are computed via a correlated
// subquery on (thread_key, peer_call) over non-deleted rows.
//
// The "exclude archived tactical threads from unread_count" defense-
// in-depth hook is not implemented at the store layer in Phase 1
// because tactical entries do not yet carry an archived flag; Phase 4
// can subtract a follow-up count in application code if the field
// lands. For now ArchivedCount returns to 0 and the UnreadCount is
// unconditional — callers see the same value they would have before.
func (s *Store) ConversationRollup(ctx context.Context, limit int) ([]ConversationSummary, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	// glebarez/sqlite returns TIMESTAMP columns as text when scanned
	// into a raw struct (not the model), so aggregate queries scan the
	// last_at column as a string and parse it back into time.Time.
	type rowAgg struct {
		ThreadKind  string
		ThreadKey   string
		LastAt      string
		TotalCount  int
		UnreadCount int
	}
	var rows []rowAgg
	if err := s.db.WithContext(ctx).
		Model(&configstore.Message{}).
		Select("thread_kind, thread_key, MAX(created_at) AS last_at, COUNT(*) AS total_count, SUM(CASE WHEN unread THEN 1 ELSE 0 END) AS unread_count").
		Group("thread_kind, thread_key").
		Order("last_at DESC").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]ConversationSummary, 0, len(rows))
	for _, r := range rows {
		lastAt, err := parseSQLiteTime(r.LastAt)
		if err != nil {
			return nil, fmt.Errorf("parse last_at %q: %w", r.LastAt, err)
		}
		summary := ConversationSummary{
			ThreadKind:  r.ThreadKind,
			ThreadKey:   r.ThreadKey,
			LastAt:      lastAt,
			TotalCount:  r.TotalCount,
			UnreadCount: r.UnreadCount,
		}

		// Last message (snippet + sender). One row per thread; cheap.
		var last configstore.Message
		if err := s.db.WithContext(ctx).
			Where("thread_kind = ? AND thread_key = ?", r.ThreadKind, r.ThreadKey).
			Order("created_at DESC").
			Order("id DESC").
			Limit(1).
			Find(&last).Error; err != nil {
			return nil, err
		}
		summary.LastSnippet = last.Text
		summary.LastSenderCall = last.FromCall

		// Participant count only applies to tactical threads.
		if r.ThreadKind == ThreadKindTactical {
			var count int64
			if err := s.db.WithContext(ctx).
				Model(&configstore.Message{}).
				Where("thread_kind = ? AND thread_key = ?", r.ThreadKind, r.ThreadKey).
				Distinct("peer_call").
				Count(&count).Error; err != nil {
				return nil, err
			}
			summary.ParticipantCount = int(count)
		}

		out = append(out, summary)
	}
	return out, nil
}

// AllocateMsgID returns the next 3-digit decimal msgid ("001".."999")
// for a DM outbound to peerCall. Runs in a transaction: reads the
// counter, finds an unused slot (skipping values currently held by
// outstanding outbound DM rows to this peer), writes the counter back,
// and returns the chosen id. Wraps 999→1.
//
// Returns ErrMsgIDExhausted when all 999 slots for the peer are held
// by outstanding rows. Caller treats this as back-pressure.
//
// Transaction scope is tight: one SELECT + one UPDATE + one in-memory
// set lookup. The skip predicate is `ack_state='none' AND direction='out'
// AND thread_kind='dm' AND peer_call = ? AND deleted_at IS NULL`, so a
// row that transitioned to broadcast/acked/rejected frees its slot.
func (s *Store) AllocateMsgID(ctx context.Context, peerCall string) (string, error) {
	var chosen uint32
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Seed the counter if absent.
		var counter configstore.MessageCounter
		err := tx.Order("id").First(&counter).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			counter = configstore.MessageCounter{NextID: 1}
			if err := tx.Create(&counter).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		// Build the set of in-flight ids.
		var outstanding []string
		if err := tx.Model(&configstore.Message{}).
			Where("ack_state = ? AND direction = ? AND thread_kind = ? AND peer_call = ?",
				AckStateNone, "out", ThreadKindDM, peerCall).
			Pluck("msg_id", &outstanding).Error; err != nil {
			return err
		}
		held := make(map[string]struct{}, len(outstanding))
		for _, id := range outstanding {
			if id != "" {
				held[id] = struct{}{}
			}
		}

		// Find the first free slot starting at NextID, scanning at
		// most 999 candidates. The counter value is always in
		// [1, 999]; we tolerate zero by clamping to 1.
		start := counter.NextID
		if start == 0 || start > 999 {
			start = 1
		}
		cur := start
		for i := 0; i < 999; i++ {
			candidate := fmt.Sprintf("%03d", cur)
			if _, busy := held[candidate]; !busy {
				chosen = cur
				// Advance counter to the slot *after* the chosen one.
				next := cur + 1
				if next > 999 {
					next = 1
				}
				counter.NextID = next
				if err := tx.Save(&counter).Error; err != nil {
					return err
				}
				return nil
			}
			cur++
			if cur > 999 {
				cur = 1
			}
		}
		return ErrMsgIDExhausted
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%03d", chosen), nil
}

// FindOutstandingByMsgID returns every outbound row matching
// (msg_id, peer_call) regardless of soft-delete status. Used for ack
// correlation: a late ack may arrive after the operator deleted their
// outbound, and we still want to set AckedAt for audit even though the
// row remains DeletedAt != NULL.
func (s *Store) FindOutstandingByMsgID(ctx context.Context, msgID, peerCall string) ([]configstore.Message, error) {
	var out []configstore.Message
	err := s.db.WithContext(ctx).
		Unscoped().
		Where("msg_id = ? AND peer_call = ? AND direction = ?", msgID, peerCall, "out").
		Order("id DESC").
		Find(&out).Error
	return out, err
}

// ListRetryDue returns DM outbound rows whose NextRetryAt is in the
// past and that are still awaiting ack. Tactical rows never populate
// NextRetryAt, so the thread_kind filter is belt-and-suspenders.
func (s *Store) ListRetryDue(ctx context.Context, now time.Time) ([]configstore.Message, error) {
	var out []configstore.Message
	err := s.db.WithContext(ctx).
		Where("thread_kind = ? AND direction = ? AND ack_state = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?",
			ThreadKindDM, "out", AckStateNone, now).
		Order("next_retry_at ASC").
		Find(&out).Error
	return out, err
}

// ListAwaitingAckOnStartup returns every DM outbound row still
// awaiting ack, time-bound excluded. The retry manager calls this on
// startup to re-arm its timer from the persisted state.
func (s *Store) ListAwaitingAckOnStartup(ctx context.Context) ([]configstore.Message, error) {
	var out []configstore.Message
	err := s.db.WithContext(ctx).
		Where("thread_kind = ? AND direction = ? AND ack_state = ?",
			ThreadKindDM, "out", AckStateNone).
		Order("id ASC").
		Find(&out).Error
	return out, err
}

// Participant is one distinct sender observed on a tactical thread,
// with the most recent time we saw a message from them.
type Participant struct {
	Callsign   string
	LastActive time.Time
}

// ListParticipants returns distinct senders on a tactical thread
// within the requested window. The effective window is clamped by
// MessagePreferences.RetentionDays (when non-zero) so a 7-day request
// against a 3-day retention yields a 3-day response — surfaces the
// clamped value as the second return so the UI can caption it
// honestly. When retention is 0 (forever) effective = requested.
//
// Sort: last_active DESC so the most recently heard stations come
// first. OurCall is excluded — the participant chip row is about
// other stations.
func (s *Store) ListParticipants(ctx context.Context, tacticalKey string, within time.Duration) ([]Participant, time.Duration, error) {
	effective := within
	var prefs configstore.MessagePreferences
	err := s.db.WithContext(ctx).Order("id").First(&prefs).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, 0, err
	}
	if prefs.RetentionDays > 0 {
		retention := time.Duration(prefs.RetentionDays) * 24 * time.Hour
		if effective == 0 || effective > retention {
			effective = retention
		}
	}

	type row struct {
		Callsign   string
		LastActive string
	}
	q := s.db.WithContext(ctx).
		Model(&configstore.Message{}).
		Select("peer_call AS callsign, MAX(created_at) AS last_active").
		Where("thread_kind = ? AND thread_key = ?", ThreadKindTactical, tacticalKey)
	if effective > 0 {
		cutoff := time.Now().Add(-effective)
		q = q.Where("created_at >= ?", cutoff)
	}
	// Exclude our own messages from the chip row. our_call column is
	// always populated on outbound and router writes it on inbound.
	q = q.Where("peer_call != our_call")
	// Defensive: also exclude the tactical label itself as a "participant".
	// The production Insert path sets peer_call = from_call for tactical
	// rows (→ our_call on outbound, sender on inbound), so peer_call
	// should never equal thread_key. But a raw INSERT from a seed or
	// migration could violate that invariant, and a real-world sender
	// whose callsign coincidentally matches the tactical label would
	// still be indistinguishable from "the tactical itself" in the UI.
	// Treat the tactical label as an address, not a participant.
	q = q.Where("peer_call != thread_key")
	q = q.Group("peer_call").Order("last_active DESC")

	var rows []row
	if err := q.Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]Participant, 0, len(rows))
	for _, r := range rows {
		if r.Callsign == "" {
			continue
		}
		la, err := parseSQLiteTime(r.LastActive)
		if err != nil {
			return nil, 0, fmt.Errorf("parse last_active %q: %w", r.LastActive, err)
		}
		out = append(out, Participant{Callsign: r.Callsign, LastActive: la})
	}
	return out, effective, nil
}

// MessageHistoryEntry is one row of the autocomplete "seen-before"
// feed. Callsign is the peer we corresponded with; LastHeard is the
// latest message exchanged with that peer. No other state is surfaced —
// the autocomplete endpoint merges this with the stationcache hit
// list and the bot directory.
type MessageHistoryEntry struct {
	Callsign  string
	LastHeard time.Time
}

// QueryMessageHistoryByPeer returns distinct peer callsigns whose
// names begin (case-insensitive) with prefix, paired with the most
// recent CreatedAt time we exchanged any message with them. Results
// are ordered newest-first. An empty prefix lists every recent peer
// up to limit. limit <= 0 applies DefaultListLimit.
//
// Used by the stations autocomplete endpoint to seed the "seen
// before" suggestions even when the station cache has evicted the
// peer.
func (s *Store) QueryMessageHistoryByPeer(ctx context.Context, prefix string, limit int) ([]MessageHistoryEntry, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	type row struct {
		Callsign  string
		LastHeard string
	}
	q := s.db.WithContext(ctx).
		Model(&configstore.Message{}).
		Select("peer_call AS callsign, MAX(created_at) AS last_heard").
		Where("peer_call <> ''")
	if prefix != "" {
		pattern := strings.ToUpper(strings.TrimSpace(prefix)) + "%"
		q = q.Where("UPPER(peer_call) LIKE ?", pattern)
	}
	q = q.Group("peer_call").Order("last_heard DESC").Limit(limit)

	var rows []row
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]MessageHistoryEntry, 0, len(rows))
	for _, r := range rows {
		if r.Callsign == "" {
			continue
		}
		t, err := parseSQLiteTime(r.LastHeard)
		if err != nil {
			return nil, fmt.Errorf("parse last_heard %q: %w", r.LastHeard, err)
		}
		out = append(out, MessageHistoryEntry{Callsign: r.Callsign, LastHeard: t})
	}
	return out, nil
}

// parseSQLiteTime parses the text encoding that glebarez/sqlite uses
// for TIMESTAMP-typed columns in aggregate results. glebarez emits
// several formats depending on how the column was originally written;
// the list below covers the forms GORM produces via Create/Save.
func parseSQLiteTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	formats := []string{
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999Z07:00",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("messages: cannot parse sqlite time %q", s)
}
