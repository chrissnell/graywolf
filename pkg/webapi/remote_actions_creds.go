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

// requireRemoteActions writes 503 and returns false when the outbound
// Actions service is not wired. Handlers call this before touching
// s.remoteActions.
func (s *Server) requireRemoteActions(w http.ResponseWriter) bool {
	if s.remoteActions == nil {
		serviceUnavailable(w, "remote actions service not configured")
		return false
	}
	return true
}

// registerRemoteActionsCreds installs /api/remote-actions/credentials.
func (s *Server) registerRemoteActionsCreds(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/remote-actions/credentials", s.listRemoteOTPCredentials)
	mux.HandleFunc("POST /api/remote-actions/credentials", s.createRemoteOTPCredential)
	mux.HandleFunc("PUT /api/remote-actions/credentials/{id}", s.updateRemoteOTPCredential)
	mux.HandleFunc("DELETE /api/remote-actions/credentials/{id}", s.deleteRemoteOTPCredential)
}

// listRemoteOTPCredentials returns every credential WITHOUT the secret.
// UsedBy is populated by a single scan of the macros table.
//
//	@Summary	List remote OTP credentials
//	@Tags		remote-actions
//	@ID			listRemoteOTPCredentials
//	@Produce	json
//	@Success	200	{array}	dto.RemoteOTPCredential
//	@Router		/remote-actions/credentials [get]
func (s *Server) listRemoteOTPCredentials(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	rows, err := s.remoteActions.Creds().List(r.Context())
	if err != nil {
		s.internalError(w, r, "list remote creds", err)
		return
	}
	usedBy, err := s.remoteActions.Creds().UsedBy(r.Context())
	if err != nil {
		s.internalError(w, r, "lookup used-by", err)
		return
	}
	out := make([]dto.RemoteOTPCredential, 0, len(rows))
	for i := range rows {
		out = append(out, remoteCredToDTO(&rows[i], usedBy[rows[i].ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

// createRemoteOTPCredential validates and stores a new credential.
//
//	@Summary	Create remote OTP credential
//	@Tags		remote-actions
//	@ID			createRemoteOTPCredential
//	@Accept		json
//	@Produce	json
//	@Success	201	{object}	dto.RemoteOTPCredential
//	@Failure	400	{object}	webtypes.ErrorResponse
//	@Failure	409	{object}	webtypes.ErrorResponse
//	@Router		/remote-actions/credentials [post]
func (s *Server) createRemoteOTPCredential(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	var in dto.RemoteOTPCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid JSON")
		return
	}
	if in.Name == "" {
		badRequest(w, "name required")
		return
	}
	secret, err := remoteactions.NormalizeBase32Secret(in.SecretB32)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	row := &remoteactions.RemoteOTPCredential{
		Name:      in.Name,
		SecretB32: secret,
		Algorithm: defaultStr(in.Algorithm, "sha1"),
		Digits:    defaultInt(in.Digits, 6),
		Period:    defaultInt(in.Period, 30),
	}
	if err := s.remoteActions.Creds().Create(r.Context(), row); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, "name already exists")
			return
		}
		s.internalError(w, r, "create remote cred", err)
		return
	}
	writeJSON(w, http.StatusCreated, remoteCredToDTO(row, nil))
}

// updateRemoteOTPCredential updates name / secret / algorithm fields.
// SecretB32 is optional in the body; an empty string leaves the stored
// secret untouched.
//
//	@Summary	Update remote OTP credential
//	@Tags		remote-actions
//	@ID			updateRemoteOTPCredential
//	@Accept		json
//	@Produce	json
//	@Success	200	{object}	dto.RemoteOTPCredential
//	@Failure	400	{object}	webtypes.ErrorResponse
//	@Failure	404	{object}	webtypes.ErrorResponse
//	@Router		/remote-actions/credentials/{id} [put]
func (s *Server) updateRemoteOTPCredential(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	cur, err := s.remoteActions.Creds().Get(r.Context(), uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "get remote cred", err)
		return
	}
	var in dto.RemoteOTPCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid JSON")
		return
	}
	if in.Name != "" {
		cur.Name = in.Name
	}
	if in.SecretB32 != "" {
		norm, err := remoteactions.NormalizeBase32Secret(in.SecretB32)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		cur.SecretB32 = norm
	}
	if in.Algorithm != "" {
		cur.Algorithm = in.Algorithm
	}
	if in.Digits != 0 {
		cur.Digits = in.Digits
	}
	if in.Period != 0 {
		cur.Period = in.Period
	}
	if err := s.remoteActions.Creds().Update(r.Context(), cur); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, "name already exists")
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "update remote cred", err)
		return
	}
	usedBy, _ := s.remoteActions.Creds().UsedBy(r.Context())
	writeJSON(w, http.StatusOK, remoteCredToDTO(cur, usedBy[cur.ID]))
}

// deleteRemoteOTPCredential rejects with 409 when the credential is
// still bound to one or more macros (UsedBy length > 0). The UI uses
// the same property to disable the Delete button.
//
//	@Summary	Delete remote OTP credential
//	@Tags		remote-actions
//	@ID			deleteRemoteOTPCredential
//	@Success	204
//	@Failure	409	{object}	webtypes.ErrorResponse
//	@Router		/remote-actions/credentials/{id} [delete]
func (s *Server) deleteRemoteOTPCredential(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	usedBy, err := s.remoteActions.Creds().UsedBy(r.Context())
	if err != nil {
		s.internalError(w, r, "usedBy", err)
		return
	}
	if len(usedBy[uint(id)]) > 0 {
		conflict(w, "credential is bound to one or more macros")
		return
	}
	if err := s.remoteActions.Creds().Delete(r.Context(), uint(id)); err != nil {
		s.internalError(w, r, "delete remote cred", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func remoteCredToDTO(c *remoteactions.RemoteOTPCredential, usedBy []string) dto.RemoteOTPCredential {
	d := dto.RemoteOTPCredential{
		ID:        c.ID,
		Name:      c.Name,
		Algorithm: c.Algorithm,
		Digits:    c.Digits,
		Period:    c.Period,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
		UsedBy:    usedBy,
	}
	if c.LastUsedAt != nil {
		t := c.LastUsedAt.UTC().Format(time.RFC3339)
		d.LastUsedAt = &t
	}
	if d.UsedBy == nil {
		d.UsedBy = []string{}
	}
	return d
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func defaultInt(n, def int) int {
	if n == 0 {
		return def
	}
	return n
}
