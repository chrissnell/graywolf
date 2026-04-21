package webapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// tacticalCallsignRe enforces the 1-9 [A-Z0-9-] tactical grammar at
// the REST boundary before any DB I/O. Same shape as the wire regex
// in pkg/messages/invite.go; keeping a local copy avoids importing a
// tacticals-only regex from the messages package.
var tacticalCallsignRe = regexp.MustCompile(`^[A-Z0-9-]{1,9}$`)

// registerTacticals installs the top-level /api/tacticals routes.
// Acceptance is tactical-keyed, not message-keyed — the endpoint
// lives outside /api/messages/tactical (the CRUD space) so the
// semantic separation is visible in the path. Registered from
// RegisterRoutes alongside the other resource registrars.
func (s *Server) registerTacticals(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/tacticals", s.acceptTacticalInvite)
}

// acceptTacticalInvite subscribes the local operator to a tactical
// callsign, optionally stamping InviteAcceptedAt on the originating
// invite row for audit. Idempotent: re-posting returns the same state
// with AlreadyMember=true.
//
// "Already a member" is never a 409 — it's a normal 200 OK with the
// AlreadyMember flag set so the client can surface a distinct toast
// without error-handling ceremony.
//
// @Summary  Accept a tactical invite (subscribe)
// @Tags     messages
// @ID       acceptTacticalInvite
// @Accept   json
// @Produce  json
// @Param    body body     dto.AcceptInviteRequest true "Tactical + optional source message id"
// @Success  200  {object} dto.AcceptInviteResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Failure  503  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /tacticals [post]
func (s *Server) acceptTacticalInvite(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.AcceptInviteRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	callsign := strings.ToUpper(strings.TrimSpace(req.Callsign))
	if !tacticalCallsignRe.MatchString(callsign) {
		badRequest(w, "callsign must be 1-9 uppercase alphanumeric or hyphen characters")
		return
	}

	// Look up the existing row (if any) so we can distinguish the
	// "create new" / "re-enable disabled" / "already a member" paths.
	existing, err := s.store.GetTacticalCallsignByCallsign(r.Context(), callsign)
	if err != nil {
		s.internalError(w, r, "get tactical callsign by callsign", err)
		return
	}

	var (
		finalModel    configstore.TacticalCallsign
		alreadyMember bool
	)
	if existing == nil {
		// Fresh subscription.
		finalModel = configstore.TacticalCallsign{
			Callsign: callsign,
			Enabled:  true,
		}
		if err := s.store.CreateTacticalCallsign(r.Context(), &finalModel); err != nil {
			if !isUniqueConstraintErr(err) {
				s.internalError(w, r, "create tactical callsign", err)
				return
			}
			// Lost a race with a concurrent create — re-fetch and
			// treat as an upsert. Callers see idempotent behavior.
			row, err2 := s.store.GetTacticalCallsignByCallsign(r.Context(), callsign)
			if err2 != nil || row == nil {
				s.internalError(w, r, "reload tactical after race", err)
				return
			}
			existing = row
		}
	}

	if existing != nil {
		// Existing row path — preserve Alias; flip Enabled=true if
		// needed. Detect the "already a member" case (Enabled was
		// already true) so the UI can toast distinctly.
		alreadyMember = existing.Enabled
		if !existing.Enabled {
			existing.Enabled = true
			if err := s.store.UpdateTacticalCallsign(r.Context(), existing); err != nil {
				s.internalError(w, r, "update tactical callsign enabled", err)
				return
			}
		}
		finalModel = *existing
	}

	// Optional audit stamp on the originating invite row. The stamp
	// is strictly invariant-gated so a malicious or malformed
	// SourceMessageID can't corrupt an unrelated row. If any
	// invariant fails, the subscription still succeeds — acceptance
	// is tactical-keyed, the message link is informational.
	var stampedMessageID uint64
	if req.SourceMessageID != 0 && s.messagesStore != nil {
		id := uint64(req.SourceMessageID)
		row, err := s.messagesStore.GetByID(r.Context(), id)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			// Row gone — silent skip.
		case err != nil:
			s.logger.Warn("accept-invite: lookup source message failed",
				"err", err, "id", id)
		case row == nil:
			// Silent skip.
		case row.Direction != "in",
			row.Kind != messages.MessageKindInvite,
			row.InviteTactical != callsign,
			row.InviteAcceptedAt != nil:
			// Invariant mismatch or already stamped — silent skip.
			// Idempotent: re-posting the same accept is a no-op.
		default:
			now := time.Now().UTC()
			row.InviteAcceptedAt = &now
			if err := s.messagesStoreUpdater(row); err != nil {
				s.logger.Warn("accept-invite: stamp InviteAcceptedAt failed",
					"err", err, "id", id)
			} else {
				stampedMessageID = row.ID
			}
		}
	}

	// Reload the router's TacticalSet so incoming packets addressed
	// to the newly-accepted tactical classify correctly. The
	// messagesReload channel is drained by cmd/graywolf's wiring
	// goroutine, which calls ReloadTacticalCallsigns. We also call
	// ReloadTacticalCallsigns inline when the service is present so
	// subsequent requests see the update even before the reload
	// goroutine wakes.
	if s.messagesService != nil {
		if err := s.messagesService.ReloadTacticalCallsigns(r.Context()); err != nil {
			s.logger.Warn("reload tactical callsigns after accept", "err", err)
		}
	}
	// Accept-invite either creates a fresh enabled row or re-enables a
	// disabled one — either way the enabled-tactical set changed, so
	// fan out to both consumers (router + iGate filter).
	s.signalTacticalChanged()

	// Emit a message.updated SSE event so other tabs / the sender
	// view re-render the invite bubble with its new accepted state.
	// Only emit when we actually stamped a row — spurious events
	// would force useless UI redraws across the whole stream.
	if stampedMessageID != 0 && s.messagesService != nil {
		s.messagesService.EventHub().Publish(messages.Event{
			Type:      messages.EventMessageUpdated,
			MessageID: stampedMessageID,
			Timestamp: time.Now().UTC(),
		})
	}

	writeJSON(w, http.StatusOK, dto.AcceptInviteResponse{
		Tactical:      dto.TacticalCallsignFromModel(finalModel),
		AlreadyMember: alreadyMember,
	})
}

// messagesStoreUpdater writes a single message row through the store
// the handler is configured with. Kept as a method so tests can
// inject a store that records writes without needing a full DB.
//
// The webapi.MessagesStore interface intentionally exposes reads
// only; mutation still flows through *messages.Store. Tests wire the
// same store for both interfaces so this method uses a type switch
// to reach the mutating API without widening MessagesStore.
func (s *Server) messagesStoreUpdater(m *configstore.Message) error {
	if ms, ok := s.messagesStore.(interface {
		Update(ctx context.Context, m *configstore.Message) error
	}); ok {
		return ms.Update(context.Background(), m)
	}
	// Fall back to a direct DB write via the configstore.
	return s.store.DB().Save(m).Error
}
