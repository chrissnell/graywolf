package webapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/callsign"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// errStationCallsignUnset is the exact body string returned by PUT
// handlers that refuse to enable iGate or Digipeater while the station
// callsign is empty or N0CALL. Kept as a package-level constant so the
// iGate and Digipeater guards speak with one voice and tests can
// compare on equality instead of substring.
const errStationCallsignUnset = "station callsign is not set; set it on the Station Callsign page before enabling this feature"

// Canonical feature names returned in StationConfigResponse.Disabled on
// the clear-with-auto-disable flow. Lowercase, exactly as the frontend
// plan (D7) requires. Any change here is a wire-breaking change for the
// UI notice logic.
const (
	disabledFeatureIGate      = "igate"
	disabledFeatureDigipeater = "digipeater"
)

// registerStationConfig installs the /api/station/config route tree on
// mux using Go 1.22+ method-scoped patterns.
func (s *Server) registerStationConfig(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/station/config", s.getStationConfig)
	mux.HandleFunc("PUT /api/station/config", s.updateStationConfig)
}

// getStationConfig returns the singleton station config. On a fresh
// install the underlying row doesn't exist yet; the store returns a
// zero-value StationConfig (no error) in that case and this handler
// surfaces it as `{"callsign":""}` so the UI always gets a valid body
// to render.
//
// @Summary  Get station config
// @Tags     station
// @ID       getStationConfig
// @Produce  json
// @Success  200 {object} dto.StationConfigResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /station/config [get]
func (s *Server) getStationConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetStationConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get station config", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.StationConfigResponse{Callsign: c.Callsign})
}

// updateStationConfig replaces the singleton station config. When the
// incoming callsign is empty or N0CALL, iGate and Digipeater are
// auto-disabled atomically (D7 rule 2) and the list of dependents
// that were actually flipped from enabled→disabled is returned in the
// response envelope. When the incoming callsign is a real value, the
// upsert is a straightforward singleton replace.
//
// The multi-table auto-disable path writes station_configs,
// i_gate_configs, and digipeater_configs in a single gorm transaction
// so a crash between writes can't leave iGate enabled against an
// empty station callsign (which the iGate would refuse to start on
// the next reload). The upsert's "zero callsign/passcode columns"
// contract (Phase 2) is preserved by UpsertIGateConfig's own
// transactional scrub; we invoke it through the outer tx by
// temporarily swapping the store's underlying *gorm.DB.
//
// @Summary  Update station config
// @Tags     station
// @ID       updateStationConfig
// @Accept   json
// @Produce  json
// @Param    body body     dto.StationConfigRequest true "Station config"
// @Success  200  {object} dto.StationConfigResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /station/config [put]
func (s *Server) updateStationConfig(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.StationConfigRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}

	ctx := r.Context()

	// Normalize for the "is this clearing / N0CALL?" decision. The
	// store also normalizes before persist, but we need the canonical
	// value here to decide whether to trigger auto-disable.
	normalized := strings.ToUpper(strings.TrimSpace(req.Callsign))
	clearing := normalized == "" || callsign.IsN0Call(normalized)

	var disabled []string
	txErr := s.store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Upsert StationConfig.
		if err := upsertStationConfigTx(tx, normalized); err != nil {
			return err
		}
		if !clearing {
			return nil
		}
		// 2. Auto-disable dependents that were Enabled. Track which
		//    ones actually flipped so the response is honest about
		//    what changed (an already-disabled iGate does NOT appear
		//    in the disabled list).
		flippedIGate, err := disableIGateIfEnabled(tx)
		if err != nil {
			return err
		}
		if flippedIGate {
			disabled = append(disabled, disabledFeatureIGate)
		}
		flippedDigi, err := disableDigipeaterIfEnabled(tx)
		if err != nil {
			return err
		}
		if flippedDigi {
			disabled = append(disabled, disabledFeatureDigipeater)
		}
		return nil
	})
	if txErr != nil {
		s.internalError(w, r, "update station config", txErr)
		return
	}

	// Read back the stored (normalized) value for the response body.
	c, err := s.store.GetStationConfig(ctx)
	if err != nil {
		s.internalError(w, r, "read station config after upsert", err)
		return
	}

	// If dependents were flipped, nudge the reload channels so the
	// running iGate / digipeater pick up the new (disabled) state
	// without a restart.
	if len(disabled) > 0 {
		for _, name := range disabled {
			switch name {
			case disabledFeatureIGate:
				s.signalIgateReload()
			case disabledFeatureDigipeater:
				s.signalDigipeaterReload()
			}
		}
	}

	writeJSON(w, http.StatusOK, dto.StationConfigResponse{
		Callsign: c.Callsign,
		Disabled: disabled,
	})
}

// upsertStationConfigTx runs the equivalent of Store.UpsertStationConfig
// against an open transaction. Duplicates the normalize + adopt-existing-id
// logic from pkg/configstore/seed_station.go since the store's exported
// method doesn't accept a *gorm.DB.
func upsertStationConfigTx(tx *gorm.DB, normalized string) error {
	var existing configstore.StationConfig
	err := tx.Order("id").First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	row := configstore.StationConfig{Callsign: normalized}
	if existing.ID != 0 {
		row.ID = existing.ID
	}
	return tx.Save(&row).Error
}

// disableIGateIfEnabled flips Enabled from true→false on the singleton
// iGate row when it was previously true. Returns (true, nil) exactly
// when the row existed AND Enabled was true. The scrub of the legacy
// callsign/passcode columns (Phase 2 D4 contract) is preserved: any
// UPDATE we issue against i_gate_configs includes callsign=” and
// passcode=” so a downgrade-rollback binary never re-reads stale
// values.
func disableIGateIfEnabled(tx *gorm.DB) (bool, error) {
	var cfg configstore.IGateConfig
	err := tx.Order("id").First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !cfg.Enabled {
		// iGate was already disabled — don't report a flip, but still
		// honour the Phase 2 D4 scrub contract (callsign='' / passcode='')
		// against this row in case a legacy binary or out-of-band writer
		// left stale values in the orphan columns. Idempotent UPDATE.
		res := tx.Model(&configstore.IGateConfig{}).Where("id = ?", cfg.ID).UpdateColumns(map[string]any{
			"callsign": "",
			"passcode": "",
		})
		if res.Error != nil {
			return false, res.Error
		}
		return false, nil
	}
	res := tx.Model(&configstore.IGateConfig{}).Where("id = ?", cfg.ID).UpdateColumns(map[string]any{
		"enabled":  false,
		"callsign": "",
		"passcode": "",
	})
	if res.Error != nil {
		return false, res.Error
	}
	return true, nil
}

// disableDigipeaterIfEnabled flips Enabled from true→false on the
// singleton digipeater row when it was previously true. Returns
// (true, nil) exactly when the row existed AND Enabled was true.
func disableDigipeaterIfEnabled(tx *gorm.DB) (bool, error) {
	var cfg configstore.DigipeaterConfig
	err := tx.Order("id").First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !cfg.Enabled {
		return false, nil
	}
	res := tx.Model(&configstore.DigipeaterConfig{}).Where("id = ?", cfg.ID).
		UpdateColumn("enabled", false)
	if res.Error != nil {
		return false, res.Error
	}
	return true, nil
}

// requireStationCallsignForEnable returns a non-nil error (already
// formatted for the client) when the incoming request enables a
// feature but the station callsign is empty or N0CALL. Shared by the
// iGate and Digipeater PUT handlers.
func (s *Server) requireStationCallsignForEnable(ctx context.Context, enabled bool) error {
	if !enabled {
		return nil
	}
	_, err := s.store.ResolveStationCallsign(ctx)
	if err == nil {
		return nil
	}
	if errors.Is(err, callsign.ErrCallsignEmpty) || errors.Is(err, callsign.ErrCallsignN0Call) {
		return fmt.Errorf("%s", errStationCallsignUnset)
	}
	return err
}
