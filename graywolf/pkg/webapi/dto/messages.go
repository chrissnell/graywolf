package dto

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
)

// --- Status derivation ---------------------------------------------------

// Status wire values. Derived from the underlying
// configstore.Message columns — see MessageResponse.Status below for
// the derivation table.
const (
	MessageStatusQueued       = "queued"        // outbound, not yet submitted
	MessageStatusTxSubmitted  = "tx_submitted"  // outbound, submitted but not yet TxHook-confirmed
	MessageStatusSentRF       = "sent_rf"       // outbound DM, RF sent, awaiting ack
	MessageStatusSentIS       = "sent_is"       // outbound DM, IS sent, awaiting ack
	MessageStatusAwaitingAck  = "awaiting_ack"  // outbound DM, sent but not yet acked
	MessageStatusAcked        = "acked"         // outbound DM, acked
	MessageStatusRejected     = "rejected"      // outbound DM, REJ received
	MessageStatusTimeout      = "timeout"       // outbound DM, retry budget exhausted
	MessageStatusBroadcast    = "sent"          // tactical outbound broadcast — terminal
	MessageStatusFailed       = "failed"        // terminal failure (non-retryable)
	MessageStatusReceived     = "received"      // inbound
)

// DeriveMessageStatus maps the row's persisted column tuple to the
// wire-visible status enum. The mapping is deterministic — same inputs
// always produce the same output — and is the single source of truth
// for the Svelte UI's status pill.
//
// Direction = "in"   → "received"
// Direction = "out" && ThreadKind = "tactical":
//	AckState == "broadcast"           → "sent"   (per plan: tactical terminal maps to "sent")
//	SentAt == nil && Attempts == 0    → "queued"
//	SentAt == nil && Attempts > 0     → "tx_submitted"
//	default                            → "sent"
// Direction = "out" && ThreadKind = "dm":
//	AckState == "acked"                                          → "acked"
//	AckState == "rejected" && FailureReason != ""                → "timeout" or "failed"
//	AckState == "rejected"                                       → "rejected"
//	SentAt == nil && Attempts == 0                               → "queued"
//	SentAt == nil && Attempts > 0                                → "tx_submitted"
//	SentAt != nil && Source == "is"                              → "sent_is"  (if not yet acked)
//	SentAt != nil                                                → "sent_rf"
func DeriveMessageStatus(m configstore.Message) string {
	if m.Direction == "in" {
		return MessageStatusReceived
	}
	// Outbound
	if m.ThreadKind == messages.ThreadKindTactical {
		switch m.AckState {
		case messages.AckStateBroadcast:
			return MessageStatusBroadcast
		}
		if m.SentAt == nil {
			if m.Attempts == 0 {
				return MessageStatusQueued
			}
			return MessageStatusTxSubmitted
		}
		return MessageStatusBroadcast
	}
	// Outbound DM
	switch m.AckState {
	case messages.AckStateAcked:
		return MessageStatusAcked
	case messages.AckStateRejected:
		// Retry manager populates FailureReason on budget exhaustion /
		// permanent governor error; empty reason → peer-sent REJ.
		switch {
		case strings.Contains(strings.ToLower(m.FailureReason), "retry budget"):
			return MessageStatusTimeout
		case m.FailureReason != "":
			return MessageStatusFailed
		default:
			return MessageStatusRejected
		}
	}
	// AckStateNone (awaiting)
	if m.SentAt == nil {
		if m.Attempts == 0 {
			return MessageStatusQueued
		}
		return MessageStatusTxSubmitted
	}
	// Sent, awaiting ack
	if strings.EqualFold(m.Source, "is") {
		return MessageStatusSentIS
	}
	return MessageStatusSentRF
}

// --- Validation helpers --------------------------------------------------

// addresseeRe accepts the APRS addressee shapes the plan documents:
// either a real callsign (1-6 alnum, optional -SSID of 1-2 alnum) or a
// tactical label (1-9 alnum+hyphen). Both shapes are uppercase; input
// is normalized via strings.ToUpper before matching.
var addresseeRe = regexp.MustCompile(`^[A-Z0-9]{1,6}(-[A-Z0-9]{1,2})?$|^[A-Z0-9-]{1,9}$`)

// ValidateAddressee returns nil iff to is a syntactically valid APRS
// addressee. Does NOT check against the tactical set or the loopback
// guard — handlers do those checks after this one.
func ValidateAddressee(to string) error {
	to = strings.TrimSpace(strings.ToUpper(to))
	if to == "" {
		return fmt.Errorf("addressee is required")
	}
	if !addresseeRe.MatchString(to) {
		return fmt.Errorf("addressee %q is not a valid APRS callsign or tactical label", to)
	}
	return nil
}

// MaxMessageText is the APRS101 per-message body cap. The router and
// sender enforce this; REST rejects it early so the client sees 400
// instead of a silent truncation.
const MaxMessageText = 67

// ValidateMessageText rejects empty or over-long bodies.
func ValidateMessageText(text string) error {
	if text == "" {
		return fmt.Errorf("text is required")
	}
	if len(text) > MaxMessageText {
		return fmt.Errorf("text exceeds %d characters (got %d)", MaxMessageText, len(text))
	}
	return nil
}

// --- Send + list DTOs ----------------------------------------------------

// SendMessageRequest is the body accepted by POST /api/messages.
type SendMessageRequest struct {
	// To is the addressee: a station callsign for a DM or a tactical
	// label for a group broadcast. Uppercase-normalized server-side.
	To string `json:"to"`
	// Text is the message body (<= 67 APRS chars after validation).
	// Ignored when Kind == "invite" — the server builds the wire body
	// from InviteTactical.
	Text string `json:"text"`
	// PreferIS, when true, routes the outbound via APRS-IS regardless
	// of the current fallback policy.
	PreferIS bool `json:"prefer_is,omitempty"`
	// Path overrides the default RF path from preferences. Empty =
	// use MessagePreferences.DefaultPath.
	Path string `json:"path,omitempty"`
	// Channel overrides the configured TX channel. Nil = use default.
	Channel *uint32 `json:"channel,omitempty"`
	// ClientID is an opaque client-side correlation token. Echoed back
	// unchanged in the response so the optimistic UI can reconcile its
	// local row with the persisted ID.
	ClientID string `json:"client_id,omitempty"`
	// Kind classifies the outbound row. Empty or "text" is a normal
	// DM/tactical message; "invite" makes the sender build a
	// `!GW1 INVITE <InviteTactical>` body and stamp the row with
	// Kind=invite + InviteTactical. The sender (Phase 2) is
	// responsible for honoring this; the DTO just carries it.
	Kind string `json:"kind,omitempty"`
	// InviteTactical is the tactical callsign referenced by an invite.
	// Must be set when Kind == "invite"; ignored otherwise.
	InviteTactical string `json:"invite_tactical,omitempty"`
}

// Validate enforces the minimal invariants every compose request must
// satisfy. Loopback / tactical-vs-DM classification is handler-local
// because it needs the OurCall context.
func (r SendMessageRequest) Validate() error {
	if err := ValidateAddressee(r.To); err != nil {
		return err
	}
	if err := ValidateMessageText(r.Text); err != nil {
		return err
	}
	return nil
}

// MessageResponse is the full wire shape for one message row. The
// Status field is derived server-side; clients don't infer it from the
// underlying columns.
type MessageResponse struct {
	ID             uint64     `json:"id"`
	Direction      string     `json:"direction"`                // "in" | "out"
	Status         string     `json:"status"`                   // derived — see DeriveMessageStatus
	OurCall        string     `json:"our_call"`
	PeerCall       string     `json:"peer_call"`
	FromCall       string     `json:"from_call"`
	ToCall         string     `json:"to_call"`
	Text           string     `json:"text"`
	MsgID          string     `json:"msg_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	ReceivedAt     *time.Time `json:"received_at,omitempty"`
	SentAt         *time.Time `json:"sent_at,omitempty"`
	AckedAt        *time.Time `json:"acked_at,omitempty"`
	Source         string     `json:"source,omitempty"`          // "rf" | "is"
	Channel        *uint32    `json:"channel,omitempty"`
	Path           string     `json:"path,omitempty"`
	Via            string     `json:"via,omitempty"`
	Unread         bool       `json:"unread"`
	Attempts       uint32     `json:"attempts"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	FailureReason  string     `json:"failure_reason,omitempty"`
	IsAck          bool       `json:"is_ack,omitempty"`
	IsBulletin     bool       `json:"is_bulletin,omitempty"`
	ThreadKind     string     `json:"thread_kind"`
	ThreadKey      string     `json:"thread_key"`
	ReceivedByCall string     `json:"received_by_call,omitempty"`
	// Kind is the body classification. Always populated — "text" for
	// normal messages, "invite" for tactical invitations. Never omitted
	// so clients can use a simple equality check without worrying about
	// a legacy empty string.
	Kind string `json:"kind"`
	// InviteTactical is the tactical callsign referenced by an invite.
	// Empty (and omitted) on non-invite rows.
	InviteTactical string `json:"invite_tactical,omitempty"`
	// InviteAcceptedAt is audit-only: set when the local operator
	// accepted this invite. The UI must NOT use this to decide "joined"
	// state — that comes from the live TacticalSet cache. Kept so
	// operators can see when/if an invite was acted on.
	InviteAcceptedAt *time.Time `json:"invite_accepted_at,omitempty"`
}

// MessageFromModel renders one row into its DTO. Channel is surfaced
// as a *uint32 so "unset" (0) serializes as omitted rather than as a
// semantic "channel 0" that would confuse clients.
func MessageFromModel(m configstore.Message) MessageResponse {
	resp := MessageResponse{
		ID:             m.ID,
		Direction:      m.Direction,
		Status:         DeriveMessageStatus(m),
		OurCall:        m.OurCall,
		PeerCall:       m.PeerCall,
		FromCall:       m.FromCall,
		ToCall:         m.ToCall,
		Text:           m.Text,
		MsgID:          m.MsgID,
		CreatedAt:      m.CreatedAt.UTC(),
		ReceivedAt:     nilUTC(m.ReceivedAt),
		SentAt:         nilUTC(m.SentAt),
		AckedAt:        nilUTC(m.AckedAt),
		Source:         m.Source,
		Path:           m.Path,
		Via:            m.Via,
		Unread:         m.Unread,
		Attempts:       m.Attempts,
		NextRetryAt:    nilUTC(m.NextRetryAt),
		FailureReason:  m.FailureReason,
		IsAck:          m.IsAck,
		IsBulletin:     m.IsBulletin,
		ThreadKind:       m.ThreadKind,
		ThreadKey:        m.ThreadKey,
		ReceivedByCall:   m.ReceivedByCall,
		Kind:             m.Kind,
		InviteTactical:   m.InviteTactical,
		InviteAcceptedAt: nilUTC(m.InviteAcceptedAt),
	}
	if m.Channel != 0 {
		c := m.Channel
		resp.Channel = &c
	}
	return resp
}

// nilUTC returns (*time.Time)(nil) for nil input; otherwise returns a
// pointer to a UTC copy of t so JSON serialization is stable.
func nilUTC(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// MessagesFromModels renders a slice of rows.
func MessagesFromModels(ms []configstore.Message) []MessageResponse {
	out := make([]MessageResponse, len(ms))
	for i, m := range ms {
		out[i] = MessageFromModel(m)
	}
	return out
}

// MessageChange is one entry in a MessageListResponse or an SSE frame.
// Kind is "created" | "updated" | "deleted"; Message is nil for
// deletions.
type MessageChange struct {
	ID      uint64           `json:"id"`
	Kind    string           `json:"kind"`
	Message *MessageResponse `json:"message,omitempty"`
}

// MessageListResponse is the envelope returned by GET /api/messages.
// The future SSE endpoint reuses the MessageChange shape inside its
// data frames so clients share a single reconciliation codepath.
type MessageListResponse struct {
	Cursor  string          `json:"cursor"`
	Changes []MessageChange `json:"changes"`
}

// --- Conversations -------------------------------------------------------

// ConversationSummary is the wire format for one thread in the master
// pane's rollup. Tactical threads populate ParticipantCount; DM rows
// omit it.
type ConversationSummary struct {
	ThreadKind       string    `json:"thread_kind"`
	ThreadKey        string    `json:"thread_key"`
	Alias            string    `json:"alias,omitempty"`
	LastAt           time.Time `json:"last_at"`
	LastSnippet      string    `json:"last_snippet"`
	LastSenderCall   string    `json:"last_sender_call"`
	UnreadCount      int       `json:"unread_count"`
	TotalCount       int       `json:"total_count"`
	ParticipantCount int       `json:"participant_count,omitempty"`
	Archived         bool      `json:"archived"`
}

// ConversationSummaryFromModel renders one store summary into its DTO.
// alias is looked up by the caller from the tactical map.
func ConversationSummaryFromModel(s messages.ConversationSummary, alias string) ConversationSummary {
	out := ConversationSummary{
		ThreadKind:     s.ThreadKind,
		ThreadKey:      s.ThreadKey,
		Alias:          alias,
		LastAt:         s.LastAt.UTC(),
		LastSnippet:    s.LastSnippet,
		LastSenderCall: s.LastSenderCall,
		UnreadCount:    s.UnreadCount,
		TotalCount:     s.TotalCount,
	}
	if s.ThreadKind == messages.ThreadKindTactical {
		out.ParticipantCount = s.ParticipantCount
	}
	return out
}

// --- Station autocomplete ------------------------------------------------

// StationAutocomplete is one suggestion in GET /api/stations/autocomplete.
// Description is only set for "bot" sources; the station cache and
// history sources leave it empty.
type StationAutocomplete struct {
	Callsign    string `json:"callsign"`
	LastHeard   string `json:"last_heard,omitempty"` // RFC3339, empty for bots / missing
	Source      string `json:"source"`               // "bot" | "cache" | "history" | "cache+history"
	Description string `json:"description,omitempty"`
}

// --- Preferences ---------------------------------------------------------

// MessagePreferencesRequest is the body accepted by PUT /api/messages/preferences.
type MessagePreferencesRequest struct {
	FallbackPolicy   string `json:"fallback_policy"`
	DefaultPath      string `json:"default_path"`
	RetryMaxAttempts uint32 `json:"retry_max_attempts"`
	RetentionDays    uint32 `json:"retention_days"`
}

// Validate clamps FallbackPolicy to the canonical enum and enforces
// retry_max_attempts > 0 (zero is surprising — treat as invalid so
// operators see a clear error instead of the defaults swallow silently).
func (r MessagePreferencesRequest) Validate() error {
	switch r.FallbackPolicy {
	case messages.FallbackPolicyRFOnly,
		messages.FallbackPolicyISFallback,
		messages.FallbackPolicyISOnly,
		messages.FallbackPolicyBoth:
	case "":
		// allow empty — handler defaults to is_fallback
	default:
		return fmt.Errorf("fallback_policy %q is not one of rf_only|is_fallback|is_only|both", r.FallbackPolicy)
	}
	if r.RetryMaxAttempts > 100 {
		return fmt.Errorf("retry_max_attempts %d exceeds sanity cap (100)", r.RetryMaxAttempts)
	}
	return nil
}

// ToModel maps the request to the persisted configstore row.
func (r MessagePreferencesRequest) ToModel() configstore.MessagePreferences {
	policy := r.FallbackPolicy
	if policy == "" {
		policy = messages.FallbackPolicyISFallback
	}
	return configstore.MessagePreferences{
		FallbackPolicy:   policy,
		DefaultPath:      r.DefaultPath,
		RetryMaxAttempts: r.RetryMaxAttempts,
		RetentionDays:    r.RetentionDays,
	}
}

// MessagePreferencesResponse is the body returned by GET/PUT preferences.
type MessagePreferencesResponse struct {
	FallbackPolicy   string `json:"fallback_policy"`
	DefaultPath      string `json:"default_path"`
	RetryMaxAttempts uint32 `json:"retry_max_attempts"`
	RetentionDays    uint32 `json:"retention_days"`
}

// MessagePreferencesFromModel renders one row. Applies policy
// normalization so GETs against an uninitialised row return a sensible
// default instead of an empty string.
func MessagePreferencesFromModel(m configstore.MessagePreferences) MessagePreferencesResponse {
	policy := messages.NormalizeFallbackPolicy(m.FallbackPolicy)
	path := m.DefaultPath
	if path == "" {
		path = "WIDE1-1,WIDE2-1"
	}
	retry := m.RetryMaxAttempts
	if retry == 0 {
		retry = messages.DefaultRetryMaxAttempts
	}
	return MessagePreferencesResponse{
		FallbackPolicy:   policy,
		DefaultPath:      path,
		RetryMaxAttempts: retry,
		RetentionDays:    m.RetentionDays,
	}
}

// --- Tactical callsigns --------------------------------------------------

// TacticalCallsignRequest is the body accepted by POST + PUT on
// /api/messages/tactical.
type TacticalCallsignRequest struct {
	Callsign string `json:"callsign"`
	Alias    string `json:"alias,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// Validate enforces addressee syntax and non-empty callsign. The
// handler additionally rejects well-known bot labels after this.
func (r TacticalCallsignRequest) Validate() error {
	if strings.TrimSpace(r.Callsign) == "" {
		return fmt.Errorf("callsign is required")
	}
	if err := ValidateAddressee(r.Callsign); err != nil {
		return err
	}
	return nil
}

// ToModel builds a configstore row from the request. Callsign is
// uppercased by the model's BeforeSave hook; we upper here too so
// validation and collision checks use the canonical form.
func (r TacticalCallsignRequest) ToModel() configstore.TacticalCallsign {
	return configstore.TacticalCallsign{
		Callsign: strings.ToUpper(strings.TrimSpace(r.Callsign)),
		Alias:    r.Alias,
		Enabled:  r.Enabled,
	}
}

// TacticalCallsignResponse is the body returned by GET/POST/PUT.
type TacticalCallsignResponse struct {
	ID        uint32    `json:"id"`
	Callsign  string    `json:"callsign"`
	Alias     string    `json:"alias,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TacticalCallsignFromModel renders one row.
func TacticalCallsignFromModel(m configstore.TacticalCallsign) TacticalCallsignResponse {
	return TacticalCallsignResponse{
		ID:        m.ID,
		Callsign:  m.Callsign,
		Alias:     m.Alias,
		Enabled:   m.Enabled,
		CreatedAt: m.CreatedAt.UTC(),
		UpdatedAt: m.UpdatedAt.UTC(),
	}
}

// AcceptInviteRequest is the body accepted by the accept-invite endpoint
// (Phase 2 wires POST /api/tacticals/subscribe or similar). Acceptance
// is tactical-keyed, not message-keyed: the message ID is optional and
// exists only to let the handler stamp InviteAcceptedAt on the originating
// row for audit.
type AcceptInviteRequest struct {
	// Callsign is the tactical label to subscribe to. Required. Must
	// match the APRS tactical syntax (1-9 of [A-Z0-9-]) after uppercase
	// normalization.
	Callsign string `json:"callsign" binding:"required"`
	// SourceMessageID, when non-zero, identifies the inbound invite
	// message that triggered the accept. Used only for audit — the
	// handler sets InviteAcceptedAt on that row if it resolves to a
	// valid invite for the same tactical. Zero = accept without audit.
	SourceMessageID uint `json:"source_message_id"`
}

// AcceptInviteResponse is the body returned by a successful accept.
// Never returns 409 — "already a member" is a normal success with
// AlreadyMember=true so the client can render a distinct toast without
// error-handling ceremony.
type AcceptInviteResponse struct {
	// Tactical is the post-accept state of the subscription. Always
	// populated with Enabled=true (accept is the "turn it on" verb).
	Tactical TacticalCallsignResponse `json:"tactical"`
	// AlreadyMember is true when the operator was already subscribed
	// and enabled before this request. Lets the UI suppress the
	// "Joined TAC" toast and emit "Already a member" instead.
	AlreadyMember bool `json:"already_member"`
}

// --- Participants --------------------------------------------------------

// ParticipantResponse is one distinct sender on a tactical thread.
type ParticipantResponse struct {
	Callsign     string    `json:"callsign"`
	LastActive   time.Time `json:"last_active"`
	MessageCount int       `json:"message_count"`
}

// ParticipantsEnvelope wraps ParticipantResponse with the effective
// window (in days) after retention clamp, so the UI can caption the
// chip row honestly even when a 7d request was clamped to 3d.
type ParticipantsEnvelope struct {
	Participants        []ParticipantResponse `json:"participants"`
	EffectiveWithinDays int                   `json:"effective_within_days"`
}
