package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// sseKeepalivePeriod is the cadence at which the SSE handler emits a
// comment line so intermediate proxies don't idle-close the stream.
// 15 s matches the plan's 5 s UI poll × 3 headroom; shorter would
// waste bytes, longer risks a load balancer closing a quiet tunnel.
const sseKeepalivePeriod = 15 * time.Second

// defaultConversationLimit caps the conversation rollup response at a
// sensible default when the caller doesn't specify.
const defaultConversationLimit = 200

// registerMessages installs the /api/messages route tree. Registered
// from Server.RegisterRoutes alongside the other resource registrars.
func (s *Server) registerMessages(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/messages", s.listMessages)
	mux.HandleFunc("POST /api/messages", s.sendMessage)
	mux.HandleFunc("GET /api/messages/conversations", s.listConversations)
	mux.HandleFunc("GET /api/messages/events", s.streamMessageEvents)
	mux.HandleFunc("GET /api/messages/preferences", s.getMessagePreferences)
	mux.HandleFunc("PUT /api/messages/preferences", s.putMessagePreferences)
	mux.HandleFunc("GET /api/messages/tactical", s.listTacticalCallsigns)
	mux.HandleFunc("POST /api/messages/tactical", s.createTacticalCallsign)
	mux.HandleFunc("PUT /api/messages/tactical/{id}", s.updateTacticalCallsign)
	mux.HandleFunc("DELETE /api/messages/tactical/{id}", s.deleteTacticalCallsign)
	mux.HandleFunc("GET /api/messages/tactical/{key}/participants", s.getTacticalParticipants)
	mux.HandleFunc("GET /api/messages/{id}", s.getMessage)
	mux.HandleFunc("DELETE /api/messages/{id}", s.deleteMessage)
	mux.HandleFunc("POST /api/messages/{id}/read", s.markMessageRead)
	mux.HandleFunc("POST /api/messages/{id}/unread", s.markMessageUnread)
	mux.HandleFunc("POST /api/messages/{id}/resend", s.resendMessage)
}

// parseUint64ID parses a uint64 path segment. Message rows use uint64
// PKs (to accommodate long-running deployments) whereas most other
// resources use uint32; a shared helper would widen the others
// unnecessarily.
func parseUint64ID(s string) (uint64, error) {
	return strconv.ParseUint(s, 10, 64)
}

// requireMessagesSvc returns the messages service or writes 503 and
// returns false. Handlers call this first.
func (s *Server) requireMessagesSvc(w http.ResponseWriter) (MessagesService, bool) {
	if s.messagesService == nil {
		serviceUnavailable(w, "messages service not configured")
		return nil, false
	}
	return s.messagesService, true
}

// requireMessagesStore returns the repository or writes 503.
func (s *Server) requireMessagesStore(w http.ResponseWriter) (MessagesStore, bool) {
	if s.messagesStore == nil {
		serviceUnavailable(w, "messages store not configured")
		return nil, false
	}
	return s.messagesStore, true
}

// signalMessagesReload performs a non-blocking send on the reload
// channel; coalesces if a previous signal is still buffered.
func (s *Server) signalMessagesReload() {
	if s.messagesReload == nil {
		return
	}
	select {
	case s.messagesReload <- struct{}{}:
	default:
	}
}

// signalTacticalChanged fans a tactical-mutation event out to both the
// message router (tactical set refresh) and the iGate (server-login
// filter recompose — enabled tacticals are appended as g/ clauses).
// Every tactical CRUD path in this package must call exactly this
// helper; skipping one half of the pair would desync the two consumers.
// Both underlying sends are buffered-1 non-blocking coalescing — a
// drop is safe because each consumer reads current tactical state
// fresh on its next reload (pull model), so signals are best-effort.
func (s *Server) signalTacticalChanged() {
	s.signalMessagesReload()
	s.signalIgateReload()
}

// --- List / get ----------------------------------------------------------

// listMessages returns a paginated slice of messages matching the
// query filters. Each row is wrapped in a MessageChange envelope
// (kind=created|updated) so the response shape matches the SSE
// streaming format — clients use one reconciliation codepath.
//
// @Summary  List messages
// @Tags     messages
// @ID       listMessages
// @Produce  json
// @Param    folder       query string false "Folder filter: inbox|sent|all"
// @Param    peer         query string false "PeerCall filter"
// @Param    thread_kind  query string false "dm|tactical"
// @Param    thread_key   query string false "Exact thread key (peer callsign for DM, tactical label for tactical)"
// @Param    since        query string false "Only messages at or after this RFC3339 timestamp"
// @Param    cursor       query string false "Opaque cursor from a prior response; pages forward"
// @Param    unread_only  query bool   false "Restrict to unread rows"
// @Param    limit        query int    false "Cap result count (1..500)"
// @Success  200 {object} dto.MessageListResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages [get]
func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	store, ok := s.requireMessagesStore(w)
	if !ok {
		return
	}
	q := r.URL.Query()
	f := messages.Filter{
		Folder:     strings.ToLower(q.Get("folder")),
		Peer:       strings.ToUpper(strings.TrimSpace(q.Get("peer"))),
		ThreadKind: strings.ToLower(strings.TrimSpace(q.Get("thread_kind"))),
		ThreadKey:  strings.ToUpper(strings.TrimSpace(q.Get("thread_key"))),
		Cursor:     q.Get("cursor"),
		UnreadOnly: q.Get("unread_only") == "true",
	}
	if s := q.Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			badRequest(w, "bad since (expected RFC3339)")
			return
		}
		f.Since = t
	}
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 500 {
			badRequest(w, "bad limit (expected 1..500)")
			return
		}
		f.Limit = n
	}
	rows, cursor, err := store.List(r.Context(), f)
	if err != nil {
		s.internalError(w, r, "list messages", err)
		return
	}
	changes := make([]dto.MessageChange, len(rows))
	for i, row := range rows {
		rowCopy := row
		msg := dto.MessageFromModel(rowCopy)
		kind := "created"
		if !row.UpdatedAt.IsZero() && row.UpdatedAt.After(row.CreatedAt.Add(time.Millisecond)) {
			kind = "updated"
		}
		changes[i] = dto.MessageChange{ID: row.ID, Kind: kind, Message: &msg}
	}
	writeJSON(w, http.StatusOK, dto.MessageListResponse{Cursor: cursor, Changes: changes})
}

// getMessage returns a single message by id.
//
// @Summary  Get message
// @Tags     messages
// @ID       getMessage
// @Produce  json
// @Param    id  path     int true "Message id"
// @Success  200 {object} dto.MessageResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/{id} [get]
func (s *Server) getMessage(w http.ResponseWriter, r *http.Request) {
	store, ok := s.requireMessagesStore(w)
	if !ok {
		return
	}
	id, err := parseUint64ID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	m, err := store.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "get message", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MessageFromModel(*m))
}

// --- Compose + mutate ----------------------------------------------------

// sendMessage enqueues a compose. Returns 202 (accepted) plus the
// persisted row with its assigned id. client_id echoes back so the
// optimistic UI can reconcile.
//
// @Summary  Send message
// @Tags     messages
// @ID       sendMessage
// @Accept   json
// @Produce  json
// @Param    body body     dto.SendMessageRequest true "Compose payload"
// @Success  202  {object} dto.MessageResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Failure  503  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages [post]
func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireMessagesSvc(w)
	if !ok {
		return
	}
	req, err := decodeJSON[dto.SendMessageRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	// Invite requests skip the default Text validation: the server
	// builds the wire body from InviteTactical, so an empty Text is
	// valid and a client-supplied Text is ignored downstream.
	isInvite := strings.EqualFold(req.Kind, messages.MessageKindInvite)
	if isInvite {
		if err := dto.ValidateAddressee(req.To); err != nil {
			badRequest(w, err.Error())
			return
		}
		if strings.TrimSpace(req.InviteTactical) == "" {
			badRequest(w, "invite_tactical is required when kind=invite")
			return
		}
	} else {
		if err := req.Validate(); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	// Loopback guard — client must not target its own base callsign.
	ourCall, err := s.resolveOurCall(r.Context())
	if err != nil {
		s.internalError(w, r, "resolve our call", err)
		return
	}
	to := strings.ToUpper(strings.TrimSpace(req.To))
	if baseCall(to) != "" && baseCall(ourCall) != "" && baseCall(to) == baseCall(ourCall) {
		badRequest(w, "cannot send a message to our own callsign")
		return
	}
	svcReq := messages.SendMessageRequest{
		To:      to,
		Text:    req.Text,
		OurCall: ourCall,
	}
	if isInvite {
		svcReq.Kind = messages.MessageKindInvite
		svcReq.InviteTactical = strings.ToUpper(strings.TrimSpace(req.InviteTactical))
	}
	row, err := svc.SendMessage(r.Context(), svcReq)
	if err != nil {
		if errors.Is(err, messages.ErrMsgIDExhausted) {
			serviceUnavailable(w, "all 999 msgid slots for this peer are outstanding; retry later")
			return
		}
		if errors.Is(err, messages.ErrInvalidThreadKind) {
			badRequest(w, err.Error())
			return
		}
		if errors.Is(err, messages.ErrInvalidInvite) {
			badRequest(w, err.Error())
			return
		}
		s.internalError(w, r, "send message", err)
		return
	}
	writeJSON(w, http.StatusAccepted, dto.MessageFromModel(*row))
}

// deleteMessage soft-deletes a message. Triggers the service's
// retry-cancel and unread-clear bookkeeping.
//
// @Summary  Delete message
// @Tags     messages
// @ID       deleteMessage
// @Param    id  path int true "Message id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/{id} [delete]
func (s *Server) deleteMessage(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireMessagesSvc(w)
	if !ok {
		return
	}
	id, err := parseUint64ID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	// Pre-check for 404 via the store — SoftDelete against a non-existent
	// row is a no-op in GORM and would surface as 204.
	if store, ok := s.messagesStore.(MessagesStore); ok && store != nil {
		if _, err := store.GetByID(r.Context(), id); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				notFound(w)
				return
			}
			s.internalError(w, r, "delete message lookup", err)
			return
		}
	}
	if err := svc.SoftDelete(r.Context(), id); err != nil {
		s.internalError(w, r, "delete message", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// markMessageRead clears the unread flag.
//
// @Summary  Mark message read
// @Tags     messages
// @ID       markMessageRead
// @Param    id  path int true "Message id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/{id}/read [post]
func (s *Server) markMessageRead(w http.ResponseWriter, r *http.Request) {
	s.markMessageReadUnread(w, r, true)
}

// markMessageUnread sets the unread flag.
//
// @Summary  Mark message unread
// @Tags     messages
// @ID       markMessageUnread
// @Param    id  path int true "Message id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/{id}/unread [post]
func (s *Server) markMessageUnread(w http.ResponseWriter, r *http.Request) {
	s.markMessageReadUnread(w, r, false)
}

func (s *Server) markMessageReadUnread(w http.ResponseWriter, r *http.Request, read bool) {
	svc, ok := s.requireMessagesSvc(w)
	if !ok {
		return
	}
	id, err := parseUint64ID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if read {
		err = svc.MarkRead(r.Context(), id)
	} else {
		err = svc.MarkUnread(r.Context(), id)
	}
	if err != nil {
		s.internalError(w, r, "mark message read/unread", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resendMessage re-submits a terminal-failed outbound. DM rows re-enroll
// in the retry ladder; tactical rows are single-shot.
//
// @Summary  Resend message
// @Tags     messages
// @ID       resendMessage
// @Produce  json
// @Param    id  path     int true "Message id"
// @Success  202 {object} dto.MessageResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  409 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/{id}/resend [post]
func (s *Server) resendMessage(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireMessagesSvc(w)
	if !ok {
		return
	}
	store, ok := s.requireMessagesStore(w)
	if !ok {
		return
	}
	id, err := parseUint64ID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	row, err := store.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "resend lookup", err)
		return
	}
	if row.Direction != "out" {
		conflict(w, "resend requires an outbound message")
		return
	}
	// Only terminal-failed rows should resend. Acked / still-retrying /
	// fresh outbound rows return 409 so the client can surface a clear
	// "nothing to do" error.
	if row.ThreadKind == messages.ThreadKindDM {
		switch row.AckState {
		case messages.AckStateRejected:
			// Allowed — retry budget exhausted or explicit REJ.
		case messages.AckStateAcked, messages.AckStateBroadcast:
			conflict(w, fmt.Sprintf("message is already in terminal state %q", row.AckState))
			return
		default:
			if row.NextRetryAt != nil || row.Attempts == 0 {
				conflict(w, "message is still pending; cannot resend until it fails or is acked")
				return
			}
		}
	}
	result, err := svc.Resend(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "already in flight") {
			conflict(w, err.Error())
			return
		}
		s.internalError(w, r, "resend message", err)
		return
	}
	if result.Err != nil && !result.Retryable {
		conflict(w, result.Err.Error())
		return
	}
	// Re-read so the response reflects post-send columns.
	cur, err := store.GetByID(r.Context(), id)
	if err != nil {
		s.internalError(w, r, "resend reload", err)
		return
	}
	writeJSON(w, http.StatusAccepted, dto.MessageFromModel(*cur))
}

// --- Conversations -------------------------------------------------------

// listConversations returns a per-thread rollup.
//
// @Summary  List conversations
// @Tags     messages
// @ID       listConversations
// @Produce  json
// @Param    limit query int false "Cap result count (1..500, default 200)"
// @Success  200 {array}  dto.ConversationSummary
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/conversations [get]
func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	store, ok := s.requireMessagesStore(w)
	if !ok {
		return
	}
	limit := defaultConversationLimit
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 500 {
			badRequest(w, "bad limit (expected 1..500)")
			return
		}
		limit = n
	}
	summaries, err := store.ConversationRollup(r.Context(), limit)
	if err != nil {
		s.internalError(w, r, "list conversations", err)
		return
	}
	// Look up tactical aliases so tactical threads carry the free-text
	// label. DM threads leave alias empty.
	aliases := map[string]string{}
	if rows, err := s.store.ListTacticalCallsigns(r.Context()); err == nil {
		for _, row := range rows {
			aliases[row.Callsign] = row.Alias
		}
	}
	out := make([]dto.ConversationSummary, len(summaries))
	for i, sm := range summaries {
		alias := ""
		if sm.ThreadKind == messages.ThreadKindTactical {
			alias = aliases[sm.ThreadKey]
		}
		out[i] = dto.ConversationSummaryFromModel(sm, alias)
	}
	writeJSON(w, http.StatusOK, out)
}

// --- SSE -----------------------------------------------------------------

// streamMessageEvents opens a Server-Sent-Events stream of message
// lifecycle notifications. Each event carries the full MessageChange
// envelope so the client shares a reconciliation codepath with the
// polling `/api/messages` endpoint.
//
// The connection stays open until the client disconnects or the
// service shuts down. A comment-only keepalive is emitted every 15 s
// so load balancers don't idle-close the stream.
//
// @Summary  Stream message events
// @Tags     messages
// @ID       streamMessageEvents
// @Produce  text/event-stream
// @Success  200 {string} string "Server-Sent-Events stream of message changes"
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/events [get]
func (s *Server) streamMessageEvents(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireMessagesSvc(w)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		serviceUnavailable(w, "streaming unsupported by this transport")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	ch, unsub := svc.EventHub().Subscribe()
	defer unsub()

	keepalive := time.NewTicker(sseKeepalivePeriod)
	defer keepalive.Stop()

	// Send an initial comment so the client sees the connection open
	// immediately even if no events fire.
	_, _ = fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case evt, ok := <-ch:
			if !ok {
				return
			}
			// Build a MessageChange envelope — deleted events omit the
			// message body; everything else fetches the row so the
			// client has the current state without a second round trip.
			change := dto.MessageChange{ID: evt.MessageID, Kind: eventKind(evt.Type)}
			if evt.Type != messages.EventMessageDeleted && s.messagesStore != nil && evt.MessageID != 0 {
				if row, err := s.messagesStore.GetByID(ctx, evt.MessageID); err == nil && row != nil {
					msg := dto.MessageFromModel(*row)
					change.Message = &msg
				}
			}
			// SSE event: include the event type as the `event:` field so
			// clients can filter with addEventListener. The data line
			// carries JSON. Flush each frame.
			if _, err := fmt.Fprintf(w, "event: %s\n", evt.Type); err != nil {
				return
			}
			payload, err := json.Marshal(change)
			if err != nil {
				s.logger.Warn("sse marshal failed", "err", err)
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// eventKind collapses the fine-grained event type into the
// MessageChange kind enum the UI consumes.
func eventKind(eventType string) string {
	switch eventType {
	case messages.EventMessageReceived:
		return "created"
	case messages.EventMessageDeleted:
		return "deleted"
	default:
		return "updated"
	}
}

// --- Preferences ---------------------------------------------------------

// getMessagePreferences returns the singleton preferences row. Seeded
// defaults come back when the row has never been mutated.
//
// @Summary  Get message preferences
// @Tags     messages
// @ID       getMessagePreferences
// @Produce  json
// @Success  200 {object} dto.MessagePreferencesResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/preferences [get]
func (s *Server) getMessagePreferences(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.store.GetMessagePreferences(r.Context())
	if err != nil {
		s.internalError(w, r, "get message preferences", err)
		return
	}
	var model configstore.MessagePreferences
	if prefs != nil {
		model = *prefs
	}
	writeJSON(w, http.StatusOK, dto.MessagePreferencesFromModel(model))
}

// putMessagePreferences upserts the singleton preferences row.
//
// @Summary  Update message preferences
// @Tags     messages
// @ID       putMessagePreferences
// @Accept   json
// @Produce  json
// @Param    body body     dto.MessagePreferencesRequest true "Preferences"
// @Success  200  {object} dto.MessagePreferencesResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/preferences [put]
func (s *Server) putMessagePreferences(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.MessagePreferencesRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	model := req.ToModel()
	if err := s.store.UpsertMessagePreferences(r.Context(), &model); err != nil {
		s.internalError(w, r, "upsert message preferences", err)
		return
	}
	// Reload inline so the next request sees the new policy immediately.
	if s.messagesService != nil {
		if err := s.messagesService.ReloadPreferences(r.Context()); err != nil {
			s.logger.Warn("reload message preferences", "err", err)
		}
	}
	s.signalMessagesReload()
	writeJSON(w, http.StatusOK, dto.MessagePreferencesFromModel(model))
}

// --- Tactical callsigns --------------------------------------------------

// listTacticalCallsigns returns every tactical callsign row.
//
// @Summary  List tactical callsigns
// @Tags     messages
// @ID       listTacticalCallsigns
// @Produce  json
// @Success  200 {array}  dto.TacticalCallsignResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/tactical [get]
func (s *Server) listTacticalCallsigns(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListTacticalCallsigns(r.Context())
	if err != nil {
		s.internalError(w, r, "list tactical callsigns", err)
		return
	}
	out := make([]dto.TacticalCallsignResponse, len(rows))
	for i, row := range rows {
		out[i] = dto.TacticalCallsignFromModel(row)
	}
	writeJSON(w, http.StatusOK, out)
}

// createTacticalCallsign registers a new tactical label.
//
// @Summary  Create tactical callsign
// @Tags     messages
// @ID       createTacticalCallsign
// @Accept   json
// @Produce  json
// @Param    body body     dto.TacticalCallsignRequest true "Tactical definition"
// @Success  201  {object} dto.TacticalCallsignResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  409  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/tactical [post]
func (s *Server) createTacticalCallsign(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.TacticalCallsignRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	callsign := strings.ToUpper(strings.TrimSpace(req.Callsign))
	if messages.IsWellKnownBot(callsign) {
		badRequest(w, fmt.Sprintf(
			"%q is a well-known APRS service address. Pick a different tactical label to avoid routing confusion.",
			callsign,
		))
		return
	}
	ourCall, _ := s.resolveOurCall(r.Context())
	if baseCall(callsign) != "" && baseCall(ourCall) != "" && baseCall(callsign) == baseCall(ourCall) {
		badRequest(w, "tactical callsign must differ from the primary operator callsign")
		return
	}
	model := req.ToModel()
	if err := s.store.CreateTacticalCallsign(r.Context(), &model); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, fmt.Sprintf("tactical callsign %q already exists", callsign))
			return
		}
		s.internalError(w, r, "create tactical callsign", err)
		return
	}
	if s.messagesService != nil {
		if err := s.messagesService.ReloadTacticalCallsigns(r.Context()); err != nil {
			s.logger.Warn("reload tactical callsigns", "err", err)
		}
	}
	s.signalTacticalChanged()
	writeJSON(w, http.StatusCreated, dto.TacticalCallsignFromModel(model))
}

// updateTacticalCallsign replaces an existing row.
//
// @Summary  Update tactical callsign
// @Tags     messages
// @ID       updateTacticalCallsign
// @Accept   json
// @Produce  json
// @Param    id   path     int                         true "Tactical id"
// @Param    body body     dto.TacticalCallsignRequest true "Tactical definition"
// @Success  200  {object} dto.TacticalCallsignResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  404  {object} webtypes.ErrorResponse
// @Failure  409  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/tactical/{id} [put]
func (s *Server) updateTacticalCallsign(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	req, err := decodeJSON[dto.TacticalCallsignRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	existing, err := s.store.GetTacticalCallsign(r.Context(), id)
	if err != nil {
		s.internalError(w, r, "get tactical callsign", err)
		return
	}
	if existing == nil {
		notFound(w)
		return
	}
	callsign := strings.ToUpper(strings.TrimSpace(req.Callsign))
	if messages.IsWellKnownBot(callsign) {
		badRequest(w, fmt.Sprintf(
			"%q is a well-known APRS service address. Pick a different tactical label to avoid routing confusion.",
			callsign,
		))
		return
	}
	ourCall, _ := s.resolveOurCall(r.Context())
	if baseCall(callsign) != "" && baseCall(ourCall) != "" && baseCall(callsign) == baseCall(ourCall) {
		badRequest(w, "tactical callsign must differ from the primary operator callsign")
		return
	}
	model := req.ToModel()
	model.ID = id
	// Preserve CreatedAt so the UpdatedAt tick is the only timestamp
	// movement. Save with a model whose ID is set and GORM updates
	// in place.
	model.CreatedAt = existing.CreatedAt
	if err := s.store.UpdateTacticalCallsign(r.Context(), &model); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, fmt.Sprintf("tactical callsign %q already exists", callsign))
			return
		}
		s.internalError(w, r, "update tactical callsign", err)
		return
	}
	if s.messagesService != nil {
		if err := s.messagesService.ReloadTacticalCallsigns(r.Context()); err != nil {
			s.logger.Warn("reload tactical callsigns", "err", err)
		}
	}
	// Covers update (non-callsign field), rename, enable toggle, and
	// disable toggle — all four land in this single PUT handler.
	s.signalTacticalChanged()
	writeJSON(w, http.StatusOK, dto.TacticalCallsignFromModel(model))
}

// deleteTacticalCallsign removes a tactical label. Historical message
// rows keyed by the label persist so the thread stays a read-only
// archive; only the monitor entry is dropped.
//
// @Summary  Delete tactical callsign
// @Tags     messages
// @ID       deleteTacticalCallsign
// @Param    id  path int true "Tactical id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/tactical/{id} [delete]
func (s *Server) deleteTacticalCallsign(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	existing, err := s.store.GetTacticalCallsign(r.Context(), id)
	if err != nil {
		s.internalError(w, r, "get tactical callsign", err)
		return
	}
	if existing == nil {
		notFound(w)
		return
	}
	if err := s.store.DeleteTacticalCallsign(r.Context(), id); err != nil {
		s.internalError(w, r, "delete tactical callsign", err)
		return
	}
	if s.messagesService != nil {
		if err := s.messagesService.ReloadTacticalCallsigns(r.Context()); err != nil {
			s.logger.Warn("reload tactical callsigns", "err", err)
		}
	}
	s.signalTacticalChanged()
	w.WriteHeader(http.StatusNoContent)
}

// getTacticalParticipants returns the distinct senders observed on a
// tactical thread within the requested window (default 7 days, clamped
// by MessagePreferences.RetentionDays).
//
// @Summary  Tactical thread participants
// @Tags     messages
// @ID       getTacticalParticipants
// @Produce  json
// @Param    key    path  string true  "Tactical key (callsign label)"
// @Param    within query string false "Lookback window (e.g. 7d, 72h); default 7d"
// @Success  200 {object} dto.ParticipantsEnvelope
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/tactical/{key}/participants [get]
func (s *Server) getTacticalParticipants(w http.ResponseWriter, r *http.Request) {
	store, ok := s.requireMessagesStore(w)
	if !ok {
		return
	}
	key := strings.ToUpper(strings.TrimSpace(r.PathValue("key")))
	if key == "" {
		badRequest(w, "key is required")
		return
	}
	within := 7 * 24 * time.Hour
	if s := r.URL.Query().Get("within"); s != "" {
		d, err := parseDaysOrDuration(s)
		if err != nil {
			badRequest(w, "bad within ("+err.Error()+")")
			return
		}
		within = d
	}
	participants, effective, err := store.ListParticipants(r.Context(), key, within)
	if err != nil {
		s.internalError(w, r, "list participants", err)
		return
	}
	out := make([]dto.ParticipantResponse, 0, len(participants))
	for _, p := range participants {
		out = append(out, dto.ParticipantResponse{
			Callsign:     p.Callsign,
			LastActive:   p.LastActive.UTC(),
			MessageCount: 0, // participants query doesn't return per-peer counts; Phase 8 may add
		})
	}
	days := 0
	if effective > 0 {
		days = int(effective.Hours() / 24)
		if days == 0 {
			days = 1 // round up so "6h window" doesn't report 0 days
		}
	}
	writeJSON(w, http.StatusOK, dto.ParticipantsEnvelope{
		Participants:        out,
		EffectiveWithinDays: days,
	})
}

// --- helpers -------------------------------------------------------------

// resolveOurCall reads the operator's primary callsign from the
// centralized StationConfig (the same source the iGate, digipeater,
// and beacon paths now use). Returns the stored value verbatim,
// preserving legacy loopback-guard semantics (an "N0CALL" station
// config is still treated as the station's current identity by the
// duplicate-destination check). An empty string means "no row yet".
// DB errors are returned as-is.
func (s *Server) resolveOurCall(ctx context.Context) (string, error) {
	c, err := s.store.GetStationConfig(ctx)
	if err != nil {
		return "", err
	}
	return c.Callsign, nil
}

// baseCall strips the SSID from a callsign ("N0CALL-9" → "N0CALL").
// Uppercases and trims as a defense against unusual inputs.
func baseCall(c string) string {
	c = strings.ToUpper(strings.TrimSpace(c))
	if i := strings.IndexByte(c, '-'); i >= 0 {
		return c[:i]
	}
	return c
}

// isUniqueConstraintErr detects SQLite's unique-constraint error so
// the handlers can map it to 409 without reading the raw driver string
// in a dozen places. Works across glebarez/sqlite and modernc.org/sqlite.
func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE") ||
		strings.Contains(strings.ToLower(msg), "unique constraint")
}

// parseDaysOrDuration accepts either a plain time.Duration string
// ("72h", "30m") or a "Nd" shorthand for days ("7d", "14d"). Returns
// an error for any other format.
func parseDaysOrDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil || n < 0 {
			return 0, fmt.Errorf("days must be a non-negative integer")
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("negative duration")
	}
	return d, nil
}
