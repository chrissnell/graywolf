package webapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerMessagesBlocklist installs the /api/messages/blocklist CRUD
// routes. The blocklist mutes inbound messages from specific call signs
// so their traffic (e.g. repeated certificate-claim messages) never
// reaches the inbox. See upstream #465.
func (s *Server) registerMessagesBlocklist(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/messages/blocklist", s.listBlockedCallsigns)
	mux.HandleFunc("POST /api/messages/blocklist", s.createBlockedCallsign)
	mux.HandleFunc("PUT /api/messages/blocklist/{id}", s.updateBlockedCallsign)
	mux.HandleFunc("DELETE /api/messages/blocklist/{id}", s.deleteBlockedCallsign)
}

// reloadBlocklist refreshes the router's in-memory blocklist inline and
// signals the messages-reload channel so the reload also propagates
// through the app-level drainer. Called after every blocklist mutation.
func (s *Server) reloadBlocklist(r *http.Request) {
	if s.messagesService != nil {
		if err := s.messagesService.ReloadBlockedCallsigns(r.Context()); err != nil {
			s.logger.Warn("reload blocked callsigns", "err", err)
		}
	}
	s.signalMessagesReload()
}

// listBlockedCallsigns returns every blocklist entry (enabled or not).
//
// @Summary  List blocked call signs
// @Tags     messages
// @ID       listBlockedCallsigns
// @Produce  json
// @Success  200 {array} dto.BlockedCallsignResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/blocklist [get]
func (s *Server) listBlockedCallsigns(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListBlockedCallsigns(r.Context())
	if err != nil {
		s.internalError(w, r, "list blocked callsigns", err)
		return
	}
	out := make([]dto.BlockedCallsignResponse, len(rows))
	for i, row := range rows {
		out[i] = dto.BlockedCallsignFromModel(row)
	}
	writeJSON(w, http.StatusOK, out)
}

// createBlockedCallsign adds a call sign to the blocklist.
//
// @Summary  Block a call sign
// @Tags     messages
// @ID       createBlockedCallsign
// @Accept   json
// @Produce  json
// @Param    body body     dto.BlockedCallsignRequest true "Blocklist entry"
// @Success  201  {object} dto.BlockedCallsignResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  409  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/blocklist [post]
func (s *Server) createBlockedCallsign(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.BlockedCallsignRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	callsign := strings.ToUpper(strings.TrimSpace(req.Callsign))
	ourCall, _ := s.resolveOurCall(r.Context())
	if baseCall(callsign) != "" && baseCall(ourCall) != "" && baseCall(callsign) == baseCall(ourCall) {
		badRequest(w, "cannot block your own call sign")
		return
	}
	model := req.ToModel()
	if err := s.store.CreateBlockedCallsign(r.Context(), &model); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, fmt.Sprintf("call sign %q is already blocked", callsign))
			return
		}
		s.internalError(w, r, "create blocked callsign", err)
		return
	}
	s.reloadBlocklist(r)
	writeJSON(w, http.StatusCreated, dto.BlockedCallsignFromModel(model))
}

// updateBlockedCallsign replaces an existing blocklist entry. Used to
// rename, edit the note, or toggle Enabled.
//
// @Summary  Update a blocked call sign
// @Tags     messages
// @ID       updateBlockedCallsign
// @Accept   json
// @Produce  json
// @Param    id   path     int                        true "Blocklist entry id"
// @Param    body body     dto.BlockedCallsignRequest true "Blocklist entry"
// @Success  200  {object} dto.BlockedCallsignResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  404  {object} webtypes.ErrorResponse
// @Failure  409  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/blocklist/{id} [put]
func (s *Server) updateBlockedCallsign(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	req, err := decodeJSON[dto.BlockedCallsignRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	existing, err := s.store.GetBlockedCallsign(r.Context(), id)
	if err != nil {
		s.internalError(w, r, "get blocked callsign", err)
		return
	}
	if existing == nil {
		notFound(w)
		return
	}
	callsign := strings.ToUpper(strings.TrimSpace(req.Callsign))
	ourCall, _ := s.resolveOurCall(r.Context())
	if baseCall(callsign) != "" && baseCall(ourCall) != "" && baseCall(callsign) == baseCall(ourCall) {
		badRequest(w, "cannot block your own call sign")
		return
	}
	model := req.ToModel()
	model.ID = id
	// Preserve CreatedAt so only UpdatedAt moves.
	model.CreatedAt = existing.CreatedAt
	if err := s.store.UpdateBlockedCallsign(r.Context(), &model); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, fmt.Sprintf("call sign %q is already blocked", callsign))
			return
		}
		s.internalError(w, r, "update blocked callsign", err)
		return
	}
	s.reloadBlocklist(r)
	writeJSON(w, http.StatusOK, dto.BlockedCallsignFromModel(model))
}

// deleteBlockedCallsign removes a blocklist entry. Traffic from the call
// sign is admitted again from the next inbound packet onward.
//
// @Summary  Unblock a call sign
// @Tags     messages
// @ID       deleteBlockedCallsign
// @Param    id  path int true "Blocklist entry id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/blocklist/{id} [delete]
func (s *Server) deleteBlockedCallsign(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	existing, err := s.store.GetBlockedCallsign(r.Context(), id)
	if err != nil {
		s.internalError(w, r, "get blocked callsign", err)
		return
	}
	if existing == nil {
		notFound(w)
		return
	}
	if err := s.store.DeleteBlockedCallsign(r.Context(), id); err != nil {
		s.internalError(w, r, "delete blocked callsign", err)
		return
	}
	s.reloadBlocklist(r)
	w.WriteHeader(http.StatusNoContent)
}
