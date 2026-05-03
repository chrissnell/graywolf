package webapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerActionListeners(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/actions/listeners", s.listActionListeners)
	mux.HandleFunc("POST /api/actions/listeners", s.createActionListener)
	mux.HandleFunc("DELETE /api/actions/listeners/{name}", s.deleteActionListener)
}

func (s *Server) listActionListeners(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListActionListenerAddressees(r.Context())
	if err != nil {
		s.internalError(w, r, "list listeners", err)
		return
	}
	out := make([]dto.ActionListenerAddressee, 0, len(rows))
	for _, row := range rows {
		out = append(out, dto.ActionListenerAddressee{
			ID:        row.ID,
			Addressee: row.Addressee,
			CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createActionListener(w http.ResponseWriter, r *http.Request) {
	in, err := decodeJSON[dto.ActionListenerAddressee](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	name := strings.ToUpper(strings.TrimSpace(in.Addressee))
	if name == "" {
		badRequest(w, "addressee required")
		return
	}
	if len(name) > 9 {
		badRequest(w, "addressee exceeds 9 chars")
		return
	}
	// Refuse to register an addressee that collides with a tactical
	// alias — the tactical lookup wins on inbound dispatch, so silently
	// allowing the duplicate would let an operator add a listener that
	// can never fire.
	if existing, err := s.store.GetTacticalCallsignByCallsign(r.Context(), name); err == nil && existing != nil {
		conflict(w, "addressee collides with a tactical alias")
		return
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.internalError(w, r, "tactical lookup", err)
		return
	}
	if err := s.store.CreateActionListenerAddressee(r.Context(), name); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, "addressee already registered")
			return
		}
		s.internalError(w, r, "create listener", err)
		return
	}
	s.signalActionsReload(r)
	rows, err := s.store.ListActionListenerAddressees(r.Context())
	if err != nil {
		s.internalError(w, r, "list listeners", err)
		return
	}
	for _, row := range rows {
		if row.Addressee == name {
			writeJSON(w, http.StatusCreated, dto.ActionListenerAddressee{
				ID:        row.ID,
				Addressee: row.Addressee,
				CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
			})
			return
		}
	}
	// Fell through — row vanished between writes. Treat as 500 so the
	// failure is visible.
	s.internalError(w, r, "create listener", errors.New("row missing post-create"))
}

func (s *Server) deleteActionListener(w http.ResponseWriter, r *http.Request) {
	name := strings.ToUpper(strings.TrimSpace(r.PathValue("name")))
	if name == "" {
		badRequest(w, "name required")
		return
	}
	if err := s.store.DeleteActionListenerAddresseeByName(r.Context(), name); err != nil {
		s.internalError(w, r, "delete listener", err)
		return
	}
	s.signalActionsReload(r)
	w.WriteHeader(http.StatusNoContent)
}

// signalActionsReload tells the running Actions subsystem to refresh
// its listener-addressee snapshot. No-op when the service hasn't been
// wired (test fixtures, early startup).
func (s *Server) signalActionsReload(r *http.Request) {
	if s.actions == nil {
		return
	}
	if err := s.actions.ReloadListeners(r.Context()); err != nil {
		s.logger.Warn("actions reload listeners", "err", err)
	}
}

