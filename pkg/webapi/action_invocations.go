package webapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerActionInvocations(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/actions/invocations", s.listActionInvocations)
	mux.HandleFunc("DELETE /api/actions/invocations", s.clearActionInvocations)
}

func (s *Server) listActionInvocations(w http.ResponseWriter, r *http.Request) {
	f := configstore.ActionInvocationFilter{
		SenderCall: r.URL.Query().Get("sender_call"),
		Status:     r.URL.Query().Get("status"),
		Source:     r.URL.Query().Get("source"),
		Search:     r.URL.Query().Get("q"),
	}
	if v := r.URL.Query().Get("action_id"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			badRequest(w, "invalid action_id")
			return
		}
		id := uint(n)
		f.ActionID = &id
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 1000 {
			badRequest(w, "limit must be 0..1000")
			return
		}
		f.Limit = n
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			badRequest(w, "offset must be >= 0")
			return
		}
		f.Offset = n
	}
	rows, err := s.store.ListActionInvocations(r.Context(), f)
	if err != nil {
		s.internalError(w, r, "list invocations", err)
		return
	}
	out := make([]dto.ActionInvocation, 0, len(rows))
	for i := range rows {
		out = append(out, invocationToDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) clearActionInvocations(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.DeleteAllActionInvocations(r.Context()); err != nil {
		s.internalError(w, r, "clear invocations", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func invocationToDTO(row *configstore.ActionInvocation) dto.ActionInvocation {
	d := dto.ActionInvocation{
		ID:            row.ID,
		ActionID:      row.ActionID,
		ActionName:    row.ActionNameAt,
		SenderCall:    row.SenderCall,
		Source:        row.Source,
		OTPCredID:     row.OTPCredentialID,
		OTPVerified:   row.OTPVerified,
		Status:        row.Status,
		StatusDetail:  row.StatusDetail,
		ExitCode:      row.ExitCode,
		HTTPStatus:    row.HTTPStatus,
		OutputCapture: row.OutputCapture,
		ReplyText:     row.ReplyText,
		Truncated:     row.Truncated,
		CreatedAt:     row.CreatedAt.UTC().Format(time.RFC3339),
	}
	if row.RawArgsJSON != "" {
		_ = json.Unmarshal([]byte(row.RawArgsJSON), &d.Args)
	}
	if d.Args == nil {
		d.Args = map[string]string{}
	}
	return d
}
