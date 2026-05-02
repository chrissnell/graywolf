package webapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerAX25TerminalConfig(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ax25/terminal-config", s.getAX25TerminalConfig)
	mux.HandleFunc("PUT /api/ax25/terminal-config", s.putAX25TerminalConfig)
}

// getAX25TerminalConfig returns the singleton terminal-UI config. The
// store auto-creates a defaulted row on first read, so this never 404s.
//
// @Summary  Get AX.25 terminal config
// @Tags     ax25
// @ID       getAX25TerminalConfig
// @Produce  json
// @Success  200 {object} dto.AX25TerminalConfig
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ax25/terminal-config [get]
func (s *Server) getAX25TerminalConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.GetAX25TerminalConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get ax25 terminal config", err)
		return
	}
	macros, err := decodeMacrosJSON(cfg.MacrosJSON)
	if err != nil {
		s.internalError(w, r, "decode macros json", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.AX25TerminalConfig{
		ScrollbackRows: cfg.ScrollbackRows,
		CursorBlink:    cfg.CursorBlink,
		DefaultModulo:  cfg.DefaultModulo,
		DefaultPaclen:  cfg.DefaultPaclen,
		Macros:         macros,
		RawTailFilter:  cfg.RawTailFilter,
	})
}

// putAX25TerminalConfig merges a partial patch into the singleton
// terminal-UI config. Every patch field is a pointer; absent fields
// preserve the existing column so a UI surface that only edits one
// field (macros, raw_tail_filter, ...) can PUT just that field
// without zeroing every other column.
//
// @Summary  Update AX.25 terminal config
// @Tags     ax25
// @ID       putAX25TerminalConfig
// @Accept   json
// @Produce  json
// @Param    body body     dto.AX25TerminalConfigPatch true "Terminal config patch"
// @Success  200  {object} dto.AX25TerminalConfig
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ax25/terminal-config [put]
func (s *Server) putAX25TerminalConfig(w http.ResponseWriter, r *http.Request) {
	patch, err := decodeJSON[dto.AX25TerminalConfigPatch](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	// Validate non-nil fields before touching the store. Zero-valued
	// fields are explicit ("operator wants to disable this") and must
	// pass the same range checks as set values.
	if patch.DefaultModulo != nil && *patch.DefaultModulo != 8 && *patch.DefaultModulo != 128 {
		badRequest(w, "default_modulo must be 8 or 128")
		return
	}
	if patch.DefaultPaclen != nil && (*patch.DefaultPaclen == 0 || *patch.DefaultPaclen > 2048) {
		badRequest(w, "default_paclen must be 1..2048")
		return
	}
	if patch.ScrollbackRows != nil && (*patch.ScrollbackRows == 0 || *patch.ScrollbackRows > 1_000_000) {
		badRequest(w, "scrollback_rows must be 1..1000000")
		return
	}
	if patch.Macros != nil {
		for i, m := range patch.Macros {
			if m.Label == "" {
				badRequest(w, fmt.Sprintf("macros[%d].label required", i))
				return
			}
		}
	}

	// Load current row so absent fields preserve their stored values.
	current, err := s.store.GetAX25TerminalConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "load ax25 terminal config", err)
		return
	}
	merged := *current
	if patch.ScrollbackRows != nil {
		merged.ScrollbackRows = *patch.ScrollbackRows
	}
	if patch.CursorBlink != nil {
		merged.CursorBlink = *patch.CursorBlink
	}
	if patch.DefaultModulo != nil {
		merged.DefaultModulo = *patch.DefaultModulo
	}
	if patch.DefaultPaclen != nil {
		merged.DefaultPaclen = *patch.DefaultPaclen
	}
	if patch.Macros != nil {
		macrosJSON, err := encodeMacrosJSON(patch.Macros)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		merged.MacrosJSON = macrosJSON
	}
	if patch.RawTailFilter != nil {
		merged.RawTailFilter = *patch.RawTailFilter
	}

	if err := s.store.UpsertAX25TerminalConfig(r.Context(), &merged); err != nil {
		s.internalError(w, r, "upsert ax25 terminal config", err)
		return
	}
	persisted, err := s.store.GetAX25TerminalConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "re-fetch ax25 terminal config", err)
		return
	}
	macros, err := decodeMacrosJSON(persisted.MacrosJSON)
	if err != nil {
		s.internalError(w, r, "decode macros json", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.AX25TerminalConfig{
		ScrollbackRows: persisted.ScrollbackRows,
		CursorBlink:    persisted.CursorBlink,
		DefaultModulo:  persisted.DefaultModulo,
		DefaultPaclen:  persisted.DefaultPaclen,
		Macros:         macros,
		RawTailFilter:  persisted.RawTailFilter,
	})
}

func decodeMacrosJSON(raw string) ([]dto.AX25TerminalMacro, error) {
	if raw == "" {
		return []dto.AX25TerminalMacro{}, nil
	}
	var out []dto.AX25TerminalMacro
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("macros json: %w", err)
	}
	if out == nil {
		out = []dto.AX25TerminalMacro{}
	}
	return out, nil
}

func encodeMacrosJSON(macros []dto.AX25TerminalMacro) (string, error) {
	if len(macros) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(macros)
	if err != nil {
		return "", fmt.Errorf("encode macros: %w", err)
	}
	return string(b), nil
}
