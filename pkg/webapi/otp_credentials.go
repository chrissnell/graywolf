package webapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerOTPCredentials(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/otp-credentials", s.listOTPCredentials)
	mux.HandleFunc("POST /api/otp-credentials", s.createOTPCredential)
	mux.HandleFunc("GET /api/otp-credentials/{id}", s.getOTPCredential)
	mux.HandleFunc("DELETE /api/otp-credentials/{id}", s.deleteOTPCredential)
}

// listOTPCredentials returns every credential WITHOUT the secret.
// Used-by is derived from a single scan of the actions table so the
// list cost stays linear in the credential count.
//
// @Summary  List OTP credentials
// @Tags     actions
// @ID       listOTPCredentials
// @Produce  json
// @Success  200 {array}  dto.OTPCredential
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /otp-credentials [get]
func (s *Server) listOTPCredentials(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListOTPCredentials(r.Context())
	if err != nil {
		s.internalError(w, r, "list otp credentials", err)
		return
	}
	usedBy, err := s.store.OTPCredentialUsedBy(r.Context())
	if err != nil {
		s.internalError(w, r, "lookup used-by", err)
		return
	}
	out := make([]dto.OTPCredential, 0, len(rows))
	for i := range rows {
		out = append(out, credentialToDTO(&rows[i], usedBy[rows[i].ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

// createOTPCredential generates a fresh TOTP secret server-side and
// returns it once on this response (secret_b32 + otpauth URI). The
// secret is never readable from any other endpoint.
//
// @Summary  Create OTP credential
// @Tags     actions
// @ID       createOTPCredential
// @Accept   json
// @Produce  json
// @Param    body body     dto.OTPCredential true "Credential definition"
// @Success  201  {object} dto.OTPCredentialCreated
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  409  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /otp-credentials [post]
func (s *Server) createOTPCredential(w http.ResponseWriter, r *http.Request) {
	in, err := decodeJSON[dto.OTPCredential](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if in.Name == "" {
		badRequest(w, "name required")
		return
	}
	issuer := in.Issuer
	if issuer == "" {
		issuer = "Graywolf"
	}
	account := in.Account
	if account == "" {
		account = in.Name
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		s.internalError(w, r, "totp generate", err)
		return
	}
	row := &configstore.OTPCredential{
		Name:      in.Name,
		Issuer:    issuer,
		Account:   account,
		Algorithm: "SHA1",
		Digits:    6,
		Period:    30,
		SecretB32: key.Secret(),
	}
	if err := s.store.CreateOTPCredential(r.Context(), row); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, "name already exists")
			return
		}
		s.internalError(w, r, "create otp credential", err)
		return
	}
	resp := dto.OTPCredentialCreated{
		OTPCredential: credentialToDTO(row, nil),
		SecretB32:     row.SecretB32,
		OtpAuthURI:    key.URL(),
	}
	writeJSON(w, http.StatusCreated, resp)
}

// getOTPCredential returns one credential without the secret.
//
// @Summary  Get OTP credential
// @Tags     actions
// @ID       getOTPCredential
// @Produce  json
// @Param    id  path     int true "Credential id"
// @Success  200 {object} dto.OTPCredential
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /otp-credentials/{id} [get]
func (s *Server) getOTPCredential(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	row, err := s.store.GetOTPCredential(r.Context(), uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "get otp credential", err)
		return
	}
	usedBy, err := s.store.OTPCredentialUsedBy(r.Context())
	if err != nil {
		s.internalError(w, r, "lookup used-by", err)
		return
	}
	writeJSON(w, http.StatusOK, credentialToDTO(row, usedBy[row.ID]))
}

// deleteOTPCredential removes the credential. Actions referencing it
// retain their otp_credential_id as null (FK on-delete-set-null) so
// the action stays present and surfaces a "no credential" status.
//
// @Summary  Delete OTP credential
// @Tags     actions
// @ID       deleteOTPCredential
// @Param    id  path int true "Credential id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /otp-credentials/{id} [delete]
func (s *Server) deleteOTPCredential(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if err := s.store.DeleteOTPCredential(r.Context(), uint(id)); err != nil {
		s.internalError(w, r, "delete otp credential", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// credentialToDTO never copies SecretB32 — defense-in-depth so a
// non-create path cannot accidentally leak the shared secret. The
// one-shot reveal lives on dto.OTPCredentialCreated, populated only by
// createOTPCredential.
func credentialToDTO(c *configstore.OTPCredential, usedBy []string) dto.OTPCredential {
	d := dto.OTPCredential{
		ID:        c.ID,
		Name:      c.Name,
		Issuer:    c.Issuer,
		Account:   c.Account,
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
	return d
}
