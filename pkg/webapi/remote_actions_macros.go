package webapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/remoteactions"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerRemoteActionsMacros(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/remote-actions/macros", s.listRemoteActionMacros)
	mux.HandleFunc("POST /api/remote-actions/macros", s.createRemoteActionMacro)
	mux.HandleFunc("PUT /api/remote-actions/macros/{id}", s.updateRemoteActionMacro)
	mux.HandleFunc("DELETE /api/remote-actions/macros/{id}", s.deleteRemoteActionMacro)
	mux.HandleFunc("POST /api/remote-actions/macros/reorder", s.reorderRemoteActionMacros)
}

//	@Summary	List remote action macros for one target
//	@Tags		remote-actions
//	@ID			listRemoteActionMacros
//	@Param		target	query	string	true	"Target callsign (uppercased server-side)"
//	@Produce	json
//	@Success	200	{array}	dto.RemoteActionMacro
//	@Router		/remote-actions/macros [get]
func (s *Server) listRemoteActionMacros(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	target := r.URL.Query().Get("target")
	if target == "" {
		badRequest(w, "target query parameter required")
		return
	}
	norm, err := remoteactions.NormalizeTargetCall(target)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	rows, err := s.remoteActions.Macros().ListByTarget(r.Context(), norm)
	if err != nil {
		s.internalError(w, r, "list macros", err)
		return
	}
	out := make([]dto.RemoteActionMacro, 0, len(rows))
	for i := range rows {
		out = append(out, remoteMacroToDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

//	@Summary	Create remote action macro
//	@Tags		remote-actions
//	@ID			createRemoteActionMacro
//	@Accept		json
//	@Produce	json
//	@Success	201	{object}	dto.RemoteActionMacro
//	@Failure	400	{object}	webtypes.ErrorResponse
//	@Router		/remote-actions/macros [post]
func (s *Server) createRemoteActionMacro(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	var in dto.RemoteActionMacroRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid JSON")
		return
	}
	target, err := remoteactions.NormalizeTargetCall(in.TargetCall)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := remoteactions.ValidateActionName(in.ActionName); err != nil {
		badRequest(w, err.Error())
		return
	}
	if in.Label == "" {
		badRequest(w, "label required")
		return
	}
	if len(in.ArgsString) > remoteactions.MaxArgsStringLen {
		badRequest(w, "args_string too long")
		return
	}
	row := &remoteactions.RemoteActionMacro{
		TargetCall:            target,
		Label:                 in.Label,
		ActionName:            in.ActionName,
		ArgsString:            in.ArgsString,
		RemoteOTPCredentialID: in.RemoteOTPCredentialID,
		Position:              in.Position,
	}
	if err := s.remoteActions.Macros().Create(r.Context(), row); err != nil {
		s.internalError(w, r, "create macro", err)
		return
	}
	writeJSON(w, http.StatusCreated, remoteMacroToDTO(row))
}

//	@Summary	Update remote action macro
//	@Tags		remote-actions
//	@ID			updateRemoteActionMacro
//	@Accept		json
//	@Produce	json
//	@Success	200	{object}	dto.RemoteActionMacro
//	@Failure	400	{object}	webtypes.ErrorResponse
//	@Failure	404	{object}	webtypes.ErrorResponse
//	@Router		/remote-actions/macros/{id} [put]
func (s *Server) updateRemoteActionMacro(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	cur, err := s.remoteActions.Macros().Get(r.Context(), uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "get macro", err)
		return
	}
	var in dto.RemoteActionMacroRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid JSON")
		return
	}
	if in.Label != "" {
		cur.Label = in.Label
	}
	if in.ActionName != "" {
		if err := remoteactions.ValidateActionName(in.ActionName); err != nil {
			badRequest(w, err.Error())
			return
		}
		cur.ActionName = in.ActionName
	}
	// ArgsString and RemoteOTPCredentialID always overwrite. Empty
	// string clears args; nil unbinds the credential. Callers must
	// send the full update body (see RemoteActionMacroRequest doc).
	cur.ArgsString = in.ArgsString
	cur.RemoteOTPCredentialID = in.RemoteOTPCredentialID
	// Position is intentionally not touched here: drag-reorder is the
	// one path that should rewrite ordering, via POST /macros/reorder.
	// A partial PUT that omits position (zero value) must not silently
	// demote the macro.
	if err := s.remoteActions.Macros().Update(r.Context(), cur); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "update macro", err)
		return
	}
	writeJSON(w, http.StatusOK, remoteMacroToDTO(cur))
}

//	@Summary	Delete remote action macro
//	@Tags		remote-actions
//	@ID			deleteRemoteActionMacro
//	@Success	204
//	@Router		/remote-actions/macros/{id} [delete]
func (s *Server) deleteRemoteActionMacro(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if err := s.remoteActions.Macros().Delete(r.Context(), uint(id)); err != nil {
		s.internalError(w, r, "delete macro", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

//	@Summary	Reorder remote action macros for one target
//	@Tags		remote-actions
//	@ID			reorderRemoteActionMacros
//	@Accept		json
//	@Success	204
//	@Failure	400	{object}	webtypes.ErrorResponse
//	@Router		/remote-actions/macros/reorder [post]
func (s *Server) reorderRemoteActionMacros(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	var in dto.RemoteActionMacroReorderRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid JSON")
		return
	}
	target, err := remoteactions.NormalizeTargetCall(in.TargetCall)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := s.remoteActions.Macros().Reorder(r.Context(), target, in.IDs); err != nil {
		if errors.Is(err, remoteactions.ErrReorderUnknownID) {
			badRequest(w, "reorder list contains an unknown macro id")
			return
		}
		s.internalError(w, r, "reorder macros", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func remoteMacroToDTO(m *remoteactions.RemoteActionMacro) dto.RemoteActionMacro {
	return dto.RemoteActionMacro{
		ID:                    m.ID,
		TargetCall:            m.TargetCall,
		Label:                 m.Label,
		ActionName:            m.ActionName,
		ArgsString:            m.ArgsString,
		RemoteOTPCredentialID: m.RemoteOTPCredentialID,
		Position:              m.Position,
		CreatedAt:             m.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:             m.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
