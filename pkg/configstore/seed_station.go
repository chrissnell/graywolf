package configstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/callsign"
)

// ---------------------------------------------------------------------------
// StationConfig (singleton)
// ---------------------------------------------------------------------------

// GetStationConfig returns the singleton station configuration row.
// Returns a zero-value StationConfig (no error) when no row exists —
// callers can treat an empty Callsign as "unconfigured" without a
// separate nil-check. DB errors other than not-found are returned
// verbatim.
func (s *Store) GetStationConfig(ctx context.Context) (StationConfig, error) {
	var c StationConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return StationConfig{}, nil
	}
	if err != nil {
		return StationConfig{}, err
	}
	return c, nil
}

// UpsertStationConfig stores the singleton station config row,
// normalizing the Callsign (TrimSpace + ToUpper) before persist. When
// c.ID == 0 and a row already exists, the existing ID is adopted so
// Save updates in place. Normalization at the store boundary means
// every caller — including future ones — sees a canonical value
// without having to remember to uppercase on write.
func (s *Store) UpsertStationConfig(ctx context.Context, c StationConfig) error {
	c.Callsign = strings.ToUpper(strings.TrimSpace(c.Callsign))
	if c.ID == 0 {
		existing, err := s.GetStationConfig(ctx)
		if err != nil {
			return err
		}
		if existing.ID != 0 {
			c.ID = existing.ID
		}
	}
	return s.db.WithContext(ctx).Save(&c).Error
}

// ResolveStationCallsign returns the normalized station callsign or a
// sentinel error. The callsign is read from StationConfig; empty (or
// whitespace-only) returns callsign.ErrCallsignEmpty, N0CALL
// (case-insensitive, SSID-agnostic) returns callsign.ErrCallsignN0Call.
// DB errors are returned as-is. Callers can branch on the sentinel
// errors via errors.Is.
func (s *Store) ResolveStationCallsign(ctx context.Context) (string, error) {
	c, err := s.GetStationConfig(ctx)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(c.Callsign)
	if trimmed == "" {
		return "", callsign.ErrCallsignEmpty
	}
	if callsign.IsN0Call(trimmed) {
		return "", callsign.ErrCallsignN0Call
	}
	return strings.ToUpper(trimmed), nil
}

// seedStationConfig seeds the StationConfig singleton from legacy
// per-feature callsigns on first run. Idempotent — no-op once any
// StationConfig row exists.
//
// Candidate priority (N0CALL-filtered at every step):
//
//  1. "Main beacon heuristic": the first beacon (by ID) whose callsign
//     exactly matches (case-insensitive, full CALL-SSID) the digi
//     MyCall or the iGate Callsign. Prevents promoting a vanity beacon
//     to the station callsign when the operator has a clearly
//     identified "main" beacon.
//  2. First beacon by ID that survived the N0CALL filter.
//  3. DigipeaterConfig.MyCall.
//  4. IGateConfig.Callsign (read via raw SQL — the Go struct field was
//     removed in Phase 2 but the column remains in the schema).
//  5. No winner → no-op; the user lands on a blank Station Callsign
//     page at first boot.
//
// After the insert, in the same transaction, any Beacon.Callsign or
// DigipeaterConfig.MyCall whose value equals the seeded callsign
// (case-insensitive, post-trim) is cleared to "" so the UI does not
// render a spurious "override" on every row the operator had set to
// their station call. Overrides that genuinely differ are preserved.
func (s *Store) seedStationConfig(ctx context.Context) error {
	db := s.db.WithContext(ctx)

	// Idempotency gate: any row at all means we've already seeded (or a
	// user saved via the API after a prior run).
	var count int64
	if err := db.Model(&StationConfig{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	// Pull candidates. Errors on any of these are fatal to the seed —
	// we don't want to silently skip legitimate values on a transient
	// DB error and end up with an empty StationConfig forever.
	var beacons []Beacon
	if err := db.Order("id").Find(&beacons).Error; err != nil {
		return err
	}

	digiCall := ""
	var digi DigipeaterConfig
	err := db.Order("id").First(&digi).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil {
		digiCall = strings.TrimSpace(digi.MyCall)
	}

	// IGateConfig.Callsign was removed from the Go struct in Phase 2 but
	// the column is still present in the schema. Read it directly so
	// legacy installs (which may have a populated callsign column) are
	// still considered as a seed candidate.
	igateCall := ""
	var igateCallRaw []string
	if err := db.Raw("SELECT callsign FROM i_gate_configs ORDER BY id LIMIT 1").Scan(&igateCallRaw).Error; err != nil {
		// "no such table" on a very fresh DB (before AutoMigrate completes
		// in the same migration round) is fine — treat as no candidate.
		// Any other error (permission denied, schema corruption, etc.)
		// must surface so the seeder doesn't silently skip a legitimate
		// candidate under a corrupt DB.
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("read legacy igate callsign: %w", err)
		}
		igateCallRaw = nil
	}
	if len(igateCallRaw) > 0 {
		igateCall = strings.TrimSpace(igateCallRaw[0])
	}

	// Filter N0CALL (and empties) from the reference points used by the
	// heuristic before comparing against beacon rows.
	digiCallRef := ""
	if digiCall != "" && !callsign.IsN0Call(digiCall) {
		digiCallRef = strings.ToUpper(digiCall)
	}
	igateCallRef := ""
	if igateCall != "" && !callsign.IsN0Call(igateCall) {
		igateCallRef = strings.ToUpper(igateCall)
	}

	// Collect usable beacons (non-empty, non-N0CALL), normalized to upper
	// case for comparisons. ID-ascending order comes from the earlier
	// Find(..., Order("id")) call.
	var usable []string
	for _, b := range beacons {
		t := strings.TrimSpace(b.Callsign)
		if t == "" || callsign.IsN0Call(t) {
			continue
		}
		usable = append(usable, strings.ToUpper(t))
	}

	winner := ""

	// Priority 1: main beacon heuristic — first beacon whose callsign
	// matches the digi MyCall or the iGate Callsign.
	if digiCallRef != "" || igateCallRef != "" {
		for _, bc := range usable {
			if digiCallRef != "" && bc == digiCallRef {
				winner = bc
				break
			}
			if igateCallRef != "" && bc == igateCallRef {
				winner = bc
				break
			}
		}
	}

	// Priority 2: first beacon by ID that survived the N0CALL filter.
	if winner == "" && len(usable) > 0 {
		winner = usable[0]
	}

	// Priority 3: digi MyCall.
	if winner == "" && digiCallRef != "" {
		winner = digiCallRef
	}

	// Priority 4: iGate callsign.
	if winner == "" && igateCallRef != "" {
		winner = igateCallRef
	}

	if winner == "" {
		return nil
	}

	// Insert + override normalization run in one transaction so a
	// mid-way failure rolls the whole thing back.
	return db.Transaction(func(tx *gorm.DB) error {
		seed := StationConfig{Callsign: winner}
		if err := tx.Create(&seed).Error; err != nil {
			return err
		}
		// Normalize: clear per-beacon and digi overrides that equal the
		// seeded callsign. Any value that differs (vanity / tactical
		// override) is preserved as-is. UPPER(TRIM(...)) handles the
		// case-insensitive, whitespace-tolerant match without pulling
		// rows into Go.
		if err := tx.Exec(
			"UPDATE beacons SET callsign = '' WHERE UPPER(TRIM(callsign)) = ?",
			winner,
		).Error; err != nil {
			return err
		}
		if err := tx.Exec(
			"UPDATE digipeater_configs SET my_call = '' WHERE UPPER(TRIM(my_call)) = ?",
			winner,
		).Error; err != nil {
			return err
		}
		return nil
	})
}
