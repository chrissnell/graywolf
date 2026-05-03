package webapi

import (
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/remoteactions"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerRemoteActionsOTP(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/remote-actions/otp/{id}", s.generateRemoteOTPCode)
}

// generateRemoteOTPCode computes the current TOTP code for the named
// credential and bumps last_used_at. Response carries the next step
// boundary so the UI countdown is driven by server time, not local
// clock drift.
//
//	@Summary	Generate one-shot TOTP code for a remote credential
//	@Tags		remote-actions
//	@ID			generateRemoteOTPCode
//	@Produce	json
//	@Param		id	path		int	true	"Credential id"
//	@Success	200	{object}	dto.RemoteOTPCode
//	@Failure	404	{object}	webtypes.ErrorResponse
//	@Router		/remote-actions/otp/{id} [post]
func (s *Server) generateRemoteOTPCode(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	cred, err := s.remoteActions.Creds().Get(r.Context(), uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "get cred", err)
		return
	}
	now := time.Now().UTC()
	code, expires, err := remoteactions.Generate(cred, now)
	if err != nil {
		s.internalError(w, r, "totp generate", err)
		return
	}
	if err := s.remoteActions.Creds().TouchLastUsed(r.Context(), cred.ID, now); err != nil {
		// Non-fatal — log then carry on. The code is still correct;
		// LastUsedAt drift only affects picker sort.
		s.logger.Warn("touch last_used_at", "id", cred.ID, "err", err)
	}
	writeJSON(w, http.StatusOK, dto.RemoteOTPCode{
		Code:      code,
		ExpiresAt: expires.Format(time.RFC3339),
	})
}
