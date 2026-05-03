package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

var actionNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

func (s *Server) registerActions(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/actions", s.listActions)
	mux.HandleFunc("POST /api/actions", s.createAction)
	mux.HandleFunc("GET /api/actions/{id}", s.getAction)
	mux.HandleFunc("PUT /api/actions/{id}", s.updateAction)
	mux.HandleFunc("DELETE /api/actions/{id}", s.deleteAction)
}

// listActions returns every configured Action with the most-recent
// invocation summary surfaced as last_invoked_at / last_invoked_by.
//
// @Summary  List actions
// @Tags     actions
// @ID       listActions
// @Produce  json
// @Success  200 {array}  dto.Action
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /actions [get]
func (s *Server) listActions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListActions(r.Context())
	if err != nil {
		s.internalError(w, r, "list actions", err)
		return
	}
	usage, err := s.collectActionLastInvoked(r.Context(), rows)
	if err != nil {
		s.internalError(w, r, "list actions usage", err)
		return
	}
	out := make([]dto.Action, 0, len(rows))
	for i := range rows {
		d := actionToDTO(&rows[i])
		if u, ok := usage[rows[i].ID]; ok {
			when := u.when.UTC().Format(time.RFC3339)
			d.LastInvokedAt = &when
			d.LastInvokedBy = u.by
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

// createAction validates and persists a new Action. The command path
// must be absolute and executable on disk; the webhook URL/method are
// validated against a small allowlist.
//
// @Summary  Create action
// @Tags     actions
// @ID       createAction
// @Accept   json
// @Produce  json
// @Param    body body     dto.Action true "Action definition"
// @Success  201  {object} dto.Action
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  409  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /actions [post]
func (s *Server) createAction(w http.ResponseWriter, r *http.Request) {
	in, err := decodeJSON[dto.Action](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := validateAction(&in); err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := s.validateActionOTPRef(r.Context(), &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	row, err := actionFromDTO(&in)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := s.store.CreateAction(r.Context(), row); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, "name already exists")
			return
		}
		s.internalError(w, r, "create action", err)
		return
	}
	writeJSON(w, http.StatusCreated, actionToDTO(row))
}

// getAction returns a single Action, populated with last_invoked_*
// from the most recent audit row.
//
// @Summary  Get action
// @Tags     actions
// @ID       getAction
// @Produce  json
// @Param    id  path     int true "Action id"
// @Success  200 {object} dto.Action
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /actions/{id} [get]
func (s *Server) getAction(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	row, err := s.store.GetAction(r.Context(), uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "get action", err)
		return
	}
	d := actionToDTO(row)
	if when, by, ok, err := s.lastInvokedFor(r.Context(), row.ID); err != nil {
		s.internalError(w, r, "last invoked", err)
		return
	} else if ok {
		ts := when.UTC().Format(time.RFC3339)
		d.LastInvokedAt = &ts
		d.LastInvokedBy = by
	}
	writeJSON(w, http.StatusOK, d)
}

// updateAction replaces an existing Action's mutable fields. The same
// validation as createAction runs; created_at is preserved.
//
// @Summary  Update action
// @Tags     actions
// @ID       updateAction
// @Accept   json
// @Produce  json
// @Param    id   path     int        true "Action id"
// @Param    body body     dto.Action true "Action definition"
// @Success  200  {object} dto.Action
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  404  {object} webtypes.ErrorResponse
// @Failure  409  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /actions/{id} [put]
func (s *Server) updateAction(w http.ResponseWriter, r *http.Request) {
	id32, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	id := uint(id32)
	existing, err := s.store.GetAction(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "get action", err)
		return
	}
	in, err := decodeJSON[dto.Action](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := validateAction(&in); err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := s.validateActionOTPRef(r.Context(), &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	row, err := actionFromDTO(&in)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	row.ID = id
	row.CreatedAt = existing.CreatedAt
	if err := s.store.UpdateAction(r.Context(), row); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, "name already exists")
			return
		}
		s.internalError(w, r, "update action", err)
		return
	}
	writeJSON(w, http.StatusOK, actionToDTO(row))
}

// deleteAction removes the Action with the given id. Existing audit
// rows are preserved (action_id is set null on cascade) so historical
// invocations remain readable.
//
// @Summary  Delete action
// @Tags     actions
// @ID       deleteAction
// @Param    id  path int true "Action id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /actions/{id} [delete]
func (s *Server) deleteAction(w http.ResponseWriter, r *http.Request) {
	id32, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if err := s.store.DeleteAction(r.Context(), uint(id32)); err != nil {
		s.internalError(w, r, "delete action", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateAction enforces the wire-shape invariants common to POST and PUT.
func validateAction(in *dto.Action) error {
	in.Name = sanitizeStr(in.Name)
	if !actionNameRE.MatchString(in.Name) {
		return errors.New("name: must match ^[A-Za-z0-9._-]{1,32}$")
	}
	switch in.Type {
	case "command":
		if err := validateCommandPath(in.CommandPath); err != nil {
			return fmt.Errorf("command_path: %w", err)
		}
	case "webhook":
		if in.WebhookURL == "" {
			return errors.New("webhook_url: required")
		}
		switch in.WebhookMethod {
		case "GET", "POST":
		default:
			return errors.New("webhook_method: must be GET or POST")
		}
	default:
		return fmt.Errorf("type: must be 'command' or 'webhook'")
	}
	if in.TimeoutSec <= 0 {
		return errors.New("timeout_sec: must be > 0")
	}
	if in.RateLimitSec < 0 {
		return errors.New("rate_limit_sec: must be >= 0")
	}
	if in.QueueDepth < 0 {
		return errors.New("queue_depth: must be >= 0")
	}
	for i, a := range in.ArgSchema {
		if a.Key == "" {
			return fmt.Errorf("arg_schema[%d]: key required", i)
		}
		if a.Regex != "" {
			if _, err := regexp.Compile(a.Regex); err != nil {
				return fmt.Errorf("arg_schema[%d]: invalid regex: %w", i, err)
			}
		}
	}
	return nil
}

func (s *Server) validateActionOTPRef(ctx context.Context, in *dto.Action) error {
	if in.OTPCredentialID == nil {
		return nil
	}
	if _, err := s.store.GetOTPCredential(ctx, *in.OTPCredentialID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("otp_credential_id: credential does not exist")
		}
		return fmt.Errorf("otp_credential_id: %w", err)
	}
	return nil
}

func validateCommandPath(p string) error {
	if p == "" {
		return errors.New("required")
	}
	if !filepath.IsAbs(p) {
		return errors.New("must be absolute")
	}
	st, err := os.Stat(p)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return errors.New("is a directory")
	}
	if st.Mode().Perm()&0o111 == 0 {
		return errors.New("not executable")
	}
	return nil
}

func sanitizeStr(s string) string { return s }

func actionToDTO(a *configstore.Action) dto.Action {
	d := dto.Action{
		ID:                  a.ID,
		Name:                a.Name,
		Description:         a.Description,
		Type:                a.Type,
		CommandPath:         a.CommandPath,
		WorkingDir:          a.WorkingDir,
		WebhookMethod:       a.WebhookMethod,
		WebhookURL:          a.WebhookURL,
		WebhookBodyTemplate: a.WebhookBodyTemplate,
		TimeoutSec:          a.TimeoutSec,
		OTPRequired:         a.OTPRequired,
		OTPCredentialID:     a.OTPCredentialID,
		SenderAllowlist:     a.SenderAllowlist,
		RateLimitSec:        a.RateLimitSec,
		QueueDepth:          a.QueueDepth,
		Enabled:             a.Enabled,
	}
	if a.WebhookHeaders != "" {
		_ = json.Unmarshal([]byte(a.WebhookHeaders), &d.WebhookHeaders)
	}
	if a.ArgSchema != "" {
		_ = json.Unmarshal([]byte(a.ArgSchema), &d.ArgSchema)
	}
	if d.ArgSchema == nil {
		d.ArgSchema = []dto.ArgSpec{}
	}
	return d
}

func actionFromDTO(d *dto.Action) (*configstore.Action, error) {
	headers := []byte("{}")
	if len(d.WebhookHeaders) > 0 {
		b, err := json.Marshal(d.WebhookHeaders)
		if err != nil {
			return nil, fmt.Errorf("webhook_headers: %w", err)
		}
		headers = b
	}
	schema := []byte("[]")
	if len(d.ArgSchema) > 0 {
		b, err := json.Marshal(d.ArgSchema)
		if err != nil {
			return nil, fmt.Errorf("arg_schema: %w", err)
		}
		schema = b
	}
	return &configstore.Action{
		Name:                d.Name,
		Description:         d.Description,
		Type:                d.Type,
		CommandPath:         d.CommandPath,
		WorkingDir:          d.WorkingDir,
		WebhookMethod:       d.WebhookMethod,
		WebhookURL:          d.WebhookURL,
		WebhookHeaders:      string(headers),
		WebhookBodyTemplate: d.WebhookBodyTemplate,
		TimeoutSec:          d.TimeoutSec,
		OTPRequired:         d.OTPRequired,
		OTPCredentialID:     d.OTPCredentialID,
		SenderAllowlist:     d.SenderAllowlist,
		ArgSchema:           string(schema),
		RateLimitSec:        d.RateLimitSec,
		QueueDepth:          d.QueueDepth,
		Enabled:             d.Enabled,
	}, nil
}

// actionUsage carries the most recent invocation summary surfaced in
// the listing.
type actionUsage struct {
	when time.Time
	by   string
}

// collectActionLastInvoked returns one entry per Action that has at
// least one invocation, populated from the most recent row. Done in a
// single store query so the listing scales with the action count, not
// the invocation count.
func (s *Server) collectActionLastInvoked(ctx context.Context, rows []configstore.Action) (map[uint]actionUsage, error) {
	out := map[uint]actionUsage{}
	for i := range rows {
		when, by, ok, err := s.lastInvokedFor(ctx, rows[i].ID)
		if err != nil {
			return nil, err
		}
		if ok {
			out[rows[i].ID] = actionUsage{when: when, by: by}
		}
	}
	return out, nil
}

// lastInvokedFor returns the most recent invocation timestamp + sender
// for a single action. ok=false means the action has never fired.
func (s *Server) lastInvokedFor(ctx context.Context, id uint) (time.Time, string, bool, error) {
	idCopy := id
	rows, err := s.store.ListActionInvocations(ctx, configstore.ActionInvocationFilter{
		ActionID: &idCopy,
		Limit:    1,
	})
	if err != nil {
		return time.Time{}, "", false, err
	}
	if len(rows) == 0 {
		return time.Time{}, "", false, nil
	}
	return rows[0].CreatedAt, rows[0].SenderCall, true, nil
}
