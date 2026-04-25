// Package configstore persists graywolf configuration in a SQLite database
// via GORM. Pure-Go (no cgo) via glebarez/sqlite.
package configstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store wraps a *gorm.DB with typed helpers for graywolf's tables.
type Store struct {
	db *gorm.DB
}

// Open opens (or creates) the SQLite database at path.
// Use OpenMemory for tests.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create config db directory %q: %w", dir, err)
		}
	}
	s, err := openDSN(path)
	if err != nil {
		return nil, err
	}
	// Best-effort: tighten permissions on the SQLite file. The DB holds
	// session credentials, the maps registration token, and other
	// device-local secrets the operator wouldn't want world-readable.
	// glebarez's :memory: driver accepts paths starting with ":" and has
	// no real file to chmod, so skip those. Errors are intentionally
	// discarded — chmod is hygiene, not a security control. Filesystems that
	// don't support unix permissions (e.g. FAT32) will simply ignore it.
	if !strings.HasPrefix(path, ":") {
		_ = os.Chmod(path, 0o600)
	}
	return s, nil
}

// OpenMemory opens an isolated in-memory database (one per call).
func OpenMemory() (*Store, error) {
	return openDSN("file::memory:?cache=shared")
}

func openDSN(dsn string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dsn, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	// SQLite writers must be single-threaded; GORM + WAL make reads concurrent
	// but writes are still serialized. One connection avoids "database is
	// locked" surprises on bursty writes.
	sqlDB.SetMaxOpenConns(1)

	// Apply pragmas. Ignore failures on in-memory DSNs where WAL isn't
	// applicable.
	_ = db.Exec("PRAGMA journal_mode=WAL").Error
	_ = db.Exec("PRAGMA busy_timeout=5000").Error
	_ = db.Exec("PRAGMA foreign_keys=ON").Error

	s := &Store{db: db}
	if err := s.Migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// DB exposes the underlying gorm DB for callers that need ad-hoc queries.
func (s *Store) DB() *gorm.DB { return s.db }

// SQLiteVersion returns the runtime SQLite library version string
// (e.g. "3.42.0") via `SELECT sqlite_version()`. Called from app
// startup so ops can see the version in the logs — important for
// migrations that depend on a minimum SQLite version (the 12-step
// table rebuild added in migration 8 needs ≥ 3.25 for
// ALTER TABLE RENAME, which this driver satisfies). Returns the
// empty string on error; callers log the returned value verbatim.
func (s *Store) SQLiteVersion() string {
	var v string
	if err := s.db.Raw("SELECT sqlite_version()").Scan(&v).Error; err != nil {
		return ""
	}
	return v
}

// Close releases the database handle.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Migrate brings the schema up to date. Safe to call repeatedly.
//
// Ordering matters: the pre-AutoMigrate pass runs first to fix up
// legacy columns that AutoMigrate would otherwise stumble over (a
// column rename, for example, looks like an add+drop to the migrator),
// then AutoMigrate reconciles the Go model shape with SQLite, then the
// post-AutoMigrate pass runs data migrations that need the new schema
// in place. See migrate.go for the migration list and the
// user_version contract.
func (s *Store) Migrate() error {
	if err := s.runMigrations(preAutoMigrate); err != nil {
		return err
	}
	// Disable FK enforcement around AutoMigrate. glebarez/sqlite's
	// Migrator wraps AlterColumn in RunWithoutForeignKey but NOT the
	// CreateConstraint / DropConstraint paths that recreateTable also
	// hits. Because recreateTable issues a raw `DROP TABLE <parent>`,
	// any child row with ON DELETE CASCADE gets cascaded out while
	// AutoMigrate is reconciling schema drift — which is what wiped
	// ptt_configs on v0.11.0 upgrades when migration 8's multi-line FK
	// declaration didn't match glebarez's single-line HasConstraint
	// LIKE pattern. Toggling FK off at the call site defends against
	// every recreateTable code path regardless of the upstream fix.
	//
	// Safety: runMigrations' own foreign_key_check already ran inside
	// migration 8's transaction, so referential integrity is verified
	// by the time we get here. AutoMigrate only reshapes schema; it
	// never inserts orphan references.
	_ = s.db.Exec("PRAGMA foreign_keys = OFF").Error
	if err := s.db.AutoMigrate(
		&AudioDevice{},
		&Channel{},
		&PttConfig{},
		&KissInterface{},
		&AgwConfig{},
		&TxTiming{},
		&DigipeaterConfig{},
		&DigipeaterRule{},
		&IGateConfig{},
		&IGateRfFilter{},
		&Beacon{},
		&PacketFilter{},
		&GPSConfig{},
		&SmartBeaconConfig{},
		&PositionLogConfig{},
		&Message{},
		&MessageCounter{},
		&MessagePreferences{},
		&TacticalCallsign{},
		&StationConfig{},
		&UpdatesConfig{},
		&UnitsConfig{},
		&ThemeConfig{},
		&MapsConfig{},
		&MapsDownload{},
	); err != nil {
		_ = s.db.Exec("PRAGMA foreign_keys = ON").Error
		return err
	}
	_ = s.db.Exec("PRAGMA foreign_keys = ON").Error
	if err := s.runMigrations(postAutoMigrate); err != nil {
		return err
	}
	// Seed the SmartBeacon singleton from any legacy per-beacon Sb*
	// tunings the first time the new table appears. Idempotent — no-op
	// once the singleton row exists or no beacon has non-default values.
	if err := s.seedSmartBeaconFromLegacyBeacons(context.Background()); err != nil {
		return fmt.Errorf("seed smart beacon: %w", err)
	}
	// Seed the MessagePreferences singleton with defaults on first run.
	// Idempotent: no-op once the row exists.
	if err := s.seedMessagePreferences(context.Background()); err != nil {
		return fmt.Errorf("seed message preferences: %w", err)
	}
	// Seed the StationConfig singleton from legacy per-feature callsigns
	// on first run. Idempotent: no-op once the row exists. See
	// .context/2026-04-21-centralized-station-callsign.md §D5.
	if err := s.seedStationConfig(context.Background()); err != nil {
		return fmt.Errorf("seed station config: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// AudioDevice CRUD
// ---------------------------------------------------------------------------

func (s *Store) CreateAudioDevice(ctx context.Context, d *AudioDevice) error {
	return s.db.WithContext(ctx).Create(d).Error
}

func (s *Store) GetAudioDevice(ctx context.Context, id uint32) (*AudioDevice, error) {
	var d AudioDevice
	if err := s.db.WithContext(ctx).First(&d, id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) ListAudioDevices(ctx context.Context) ([]AudioDevice, error) {
	var out []AudioDevice
	return out, s.db.WithContext(ctx).Order("id").Find(&out).Error
}

func (s *Store) UpdateAudioDevice(ctx context.Context, d *AudioDevice) error {
	return s.db.WithContext(ctx).Save(d).Error
}

func (s *Store) DeleteAudioDevice(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Delete(&AudioDevice{}, id).Error
}

// DeleteAudioDeviceChecked atomically checks for channels referencing the
// device and either refuses the delete (cascade=false with refs) or
// cascades through them (cascade=true, or no refs) within a single
// transaction. There is no window for a concurrent writer to slip in a
// new referencing channel between the check and the delete, so an
// operator who declined to cascade can never have a channel silently
// swept away.
//
// Return shapes:
//   - refs non-empty, deleted nil: operator refused to cascade; nothing
//     was modified. Caller should surface refs to the user and ask.
//   - refs nil, deleted: the device is gone; deleted lists the channels
//     that went with it (possibly empty if nothing referenced the device).
func (s *Store) DeleteAudioDeviceChecked(ctx context.Context, id uint32, cascade bool) (deleted []Channel, refs []Channel, err error) {
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var found []Channel
		if err := tx.Where("input_device_id = ? OR output_device_id = ?", id, id).
			Order("id").Find(&found).Error; err != nil {
			return err
		}
		if len(found) > 0 && !cascade {
			refs = found
			return nil
		}
		for _, ch := range found {
			if err := tx.Delete(&Channel{}, ch.ID).Error; err != nil {
				return fmt.Errorf("delete channel %d: %w", ch.ID, err)
			}
		}
		if err := tx.Delete(&AudioDevice{}, id).Error; err != nil {
			return err
		}
		deleted = found
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return deleted, refs, nil
}

// ---------------------------------------------------------------------------
// Channel CRUD
// ---------------------------------------------------------------------------

func (s *Store) CreateChannel(ctx context.Context, c *Channel) error {
	if err := s.validateChannel(ctx, c, 0); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Create(c).Error
}

func (s *Store) GetChannel(ctx context.Context, id uint32) (*Channel, error) {
	var c Channel
	if err := s.db.WithContext(ctx).First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	var out []Channel
	return out, s.db.WithContext(ctx).Order("id").Find(&out).Error
}

func (s *Store) UpdateChannel(ctx context.Context, c *Channel) error {
	if err := s.validateChannel(ctx, c, c.ID); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Save(c).Error
}

func (s *Store) DeleteChannel(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Delete(&Channel{}, id).Error
}

// ChannelExists reports whether a channel row with the given ID exists.
// Returns (false, nil) when the row is absent; (false, err) on any
// driver / context error. Callers that need the row itself should use
// GetChannel; ChannelExists is the cheaper probe for write-time
// reference validation (see dto.ValidateChannelRef).
func (s *Store) ChannelExists(ctx context.Context, id uint32) (bool, error) {
	if id == 0 {
		return false, nil
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&Channel{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// ---------------------------------------------------------------------------
// Referential integrity: ChannelReferrers + DeleteChannelCascade (Phase 5)
// ---------------------------------------------------------------------------

// Referrer describes a single row in a dependent table that references a
// channel via a soft integer FK. Used by ChannelReferrers to surface the
// impact of a cascade delete to the operator before commit.
//
// Type is a stable token identifying the referent table + role (so two
// columns of the same table — e.g. digipeater_rule_from vs
// digipeater_rule_to — don't collapse together). ID is the row's own
// primary key. Name is a human-legible label chosen from the row
// (Beacon.Callsign+Type, DigipeaterRule.Alias, KissInterface.Name, etc.);
// empty for singleton referents (IGateConfig) where the row has no
// meaningful display name.
type Referrer struct {
	Type string `json:"type"`
	ID   uint32 `json:"id"`
	Name string `json:"name"`
}

// Referrer Type tokens. Exported so the webapi layer can switch on them
// without re-stringing constants in two places.
const (
	ReferrerTypeBeacon             = "beacon"
	ReferrerTypeDigipeaterRuleFrom = "digipeater_rule_from"
	ReferrerTypeDigipeaterRuleTo   = "digipeater_rule_to"
	ReferrerTypeKissInterface      = "kiss_interface"
	ReferrerTypeIGateConfigRf      = "igate_config_rf"
	ReferrerTypeIGateConfigTx      = "igate_config_tx"
	ReferrerTypeIGateRfFilter      = "igate_rf_filter"
	ReferrerTypeTxTiming           = "tx_timing"
)

// Referrers is the collected result of a ChannelReferrers scan. Items
// is always non-nil (possibly empty) so JSON encoders emit `[]` rather
// than `null` on the wire.
type Referrers struct {
	Items []Referrer `json:"items"`
}

// ChannelReferrers queries every table that references channels.id via a
// soft integer foreign key and returns a structured list of dependent
// rows. Used by DELETE /api/channels/{id} to return 409 with the impact
// list, and by GET /api/channels/{id}/referrers to power the first
// confirmation dialog in the UI.
//
// Covered tables (see design decision D12 in
// .context/2026-04-20-kiss-tcp-client-and-channel-backing.md):
//
//   - Beacon.Channel
//   - DigipeaterRule.FromChannel (emitted as "digipeater_rule_from")
//   - DigipeaterRule.ToChannel (emitted as "digipeater_rule_to" only
//     when ToChannel matches AND FromChannel does NOT — cross-channel
//     rules where only the destination matched; same-channel rules are
//     already covered by the FromChannel branch).
//   - KissInterface.Channel
//   - IGateConfig.RfChannel (as "igate_config_rf")
//   - IGateConfig.TxChannel (as "igate_config_tx")
//   - IGateRfFilter.Channel
//   - TxTiming.Channel
//
// PttConfig.ChannelID has a hard FK with OnDelete:CASCADE, so SQLite
// removes those rows automatically and this scan deliberately omits them.
//
// A channelID of 0 returns an empty list — 0 is reserved for "none" in
// singletons like IGateConfig.TxChannel and never a real channel row.
func (s *Store) ChannelReferrers(ctx context.Context, channelID uint32) (Referrers, error) {
	out := Referrers{Items: []Referrer{}}
	if channelID == 0 {
		return out, nil
	}
	db := s.db.WithContext(ctx)

	// Beacons: Type + Callsign composes a useful label when Type is set
	// (the UI labels beacons by type in lists); fall back to Callsign.
	var beacons []Beacon
	if err := db.Where("channel = ?", channelID).Order("id").Find(&beacons).Error; err != nil {
		return out, fmt.Errorf("channel referrers: beacons: %w", err)
	}
	for _, b := range beacons {
		label := b.Callsign
		if b.Type != "" && label != "" {
			label = fmt.Sprintf("%s (%s)", b.Callsign, b.Type)
		}
		out.Items = append(out.Items, Referrer{Type: ReferrerTypeBeacon, ID: b.ID, Name: label})
	}

	// Digipeater rules: from-side.
	var rulesFrom []DigipeaterRule
	if err := db.Where("from_channel = ?", channelID).Order("id").Find(&rulesFrom).Error; err != nil {
		return out, fmt.Errorf("channel referrers: digipeater rules from: %w", err)
	}
	fromIDs := make(map[uint32]struct{}, len(rulesFrom))
	for _, r := range rulesFrom {
		fromIDs[r.ID] = struct{}{}
		out.Items = append(out.Items, Referrer{
			Type: ReferrerTypeDigipeaterRuleFrom, ID: r.ID, Name: digipeaterRuleLabel(r),
		})
	}

	// Digipeater rules: to-side, excluding rows already listed via
	// from-side (same-channel rules where From == To).
	var rulesTo []DigipeaterRule
	if err := db.Where("to_channel = ?", channelID).Order("id").Find(&rulesTo).Error; err != nil {
		return out, fmt.Errorf("channel referrers: digipeater rules to: %w", err)
	}
	for _, r := range rulesTo {
		if _, dup := fromIDs[r.ID]; dup {
			continue
		}
		out.Items = append(out.Items, Referrer{
			Type: ReferrerTypeDigipeaterRuleTo, ID: r.ID, Name: digipeaterRuleLabel(r),
		})
	}

	// KISS interfaces: Name is populated; Channel=0 won't match by the
	// query predicate.
	var ifaces []KissInterface
	if err := db.Where("channel = ?", channelID).Order("id").Find(&ifaces).Error; err != nil {
		return out, fmt.Errorf("channel referrers: kiss interfaces: %w", err)
	}
	for _, k := range ifaces {
		out.Items = append(out.Items, Referrer{Type: ReferrerTypeKissInterface, ID: k.ID, Name: k.Name})
	}

	// IGate config singleton: RfChannel and TxChannel are separate
	// roles. Emit one entry per role that matches so the UI can render
	// a clear "RF channel assignment will be cleared" vs "TX channel
	// assignment will be cleared" message.
	igc, err := s.GetIGateConfig(ctx)
	if err != nil {
		return out, fmt.Errorf("channel referrers: igate config: %w", err)
	}
	if igc != nil {
		if igc.RfChannel == channelID {
			out.Items = append(out.Items, Referrer{Type: ReferrerTypeIGateConfigRf, ID: igc.ID, Name: ""})
		}
		if igc.TxChannel == channelID {
			out.Items = append(out.Items, Referrer{Type: ReferrerTypeIGateConfigTx, ID: igc.ID, Name: ""})
		}
	}

	// IGate RF filters.
	var filters []IGateRfFilter
	if err := db.Where("channel = ?", channelID).Order("id").Find(&filters).Error; err != nil {
		return out, fmt.Errorf("channel referrers: igate rf filters: %w", err)
	}
	for _, f := range filters {
		label := fmt.Sprintf("%s: %s", f.Type, f.Pattern)
		out.Items = append(out.Items, Referrer{Type: ReferrerTypeIGateRfFilter, ID: f.ID, Name: label})
	}

	// TxTiming rows (per-channel singleton).
	var timings []TxTiming
	if err := db.Where("channel = ?", channelID).Order("id").Find(&timings).Error; err != nil {
		return out, fmt.Errorf("channel referrers: tx timings: %w", err)
	}
	for _, t := range timings {
		out.Items = append(out.Items, Referrer{Type: ReferrerTypeTxTiming, ID: t.ID, Name: ""})
	}

	return out, nil
}

// digipeaterRuleLabel builds a short human label for a digipeater rule.
// Falls back to "rule #<id>" when the row has no alias (shouldn't happen
// — Alias is required — but defensive for fixtures).
func digipeaterRuleLabel(r DigipeaterRule) string {
	if r.Alias == "" {
		return fmt.Sprintf("rule #%d", r.ID)
	}
	if r.FromChannel == r.ToChannel {
		return fmt.Sprintf("%s (ch %d)", r.Alias, r.FromChannel)
	}
	return fmt.Sprintf("%s (ch %d → %d)", r.Alias, r.FromChannel, r.ToChannel)
}

// DeleteChannelCascade atomically removes a channel plus every soft-FK
// reference per the D12 per-table policy:
//
//   - Beacon.Channel == id → delete row
//   - DigipeaterRule.FromChannel == id → delete row
//   - DigipeaterRule.ToChannel == id AND FromChannel != id → delete row
//     (cross-channel rules where only the destination matched)
//   - KissInterface.Channel == id → set Channel=0 AND NeedsReconfig=true
//     (the interface may still be useful on another channel after
//     reconfig; don't delete it)
//   - IGateConfig.RfChannel == id → set RfChannel=0
//   - IGateConfig.TxChannel == id → set TxChannel=0
//   - IGateRfFilter.Channel == id → delete row
//   - TxTiming.Channel == id → delete row
//   - Channel itself → delete (fires ON DELETE CASCADE for PttConfig)
//
// All operations run in a single SQLite transaction; either every
// change lands or none do. Callers are expected to fire a single
// post-commit bridge / kiss-manager reload so in-memory state
// reconverges exactly once (not N times, one per affected row).
//
// Returns the count of rows that were touched (for observability in the
// caller's reload log line) plus any error. A nonexistent channelID
// returns (0, gorm.ErrRecordNotFound) without changing anything.
func (s *Store) DeleteChannelCascade(ctx context.Context, channelID uint32) (int, error) {
	if channelID == 0 {
		return 0, fmt.Errorf("channel id 0 is not a valid cascade target")
	}
	var affected int
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Require the channel to exist — returning ErrRecordNotFound
		// lets the handler layer emit a 404.
		var ch Channel
		if err := tx.First(&ch, channelID).Error; err != nil {
			return err
		}

		// Beacons.
		res := tx.Where("channel = ?", channelID).Delete(&Beacon{})
		if res.Error != nil {
			return fmt.Errorf("cascade beacons: %w", res.Error)
		}
		affected += int(res.RowsAffected)

		// Digipeater rules: from-side first.
		res = tx.Where("from_channel = ?", channelID).Delete(&DigipeaterRule{})
		if res.Error != nil {
			return fmt.Errorf("cascade digipeater rules from: %w", res.Error)
		}
		affected += int(res.RowsAffected)

		// Digipeater rules: cross-channel to-side (rows where only the
		// destination matched — the from-side delete already swept
		// same-channel rules).
		res = tx.Where("to_channel = ? AND from_channel != ?", channelID, channelID).Delete(&DigipeaterRule{})
		if res.Error != nil {
			return fmt.Errorf("cascade digipeater rules to: %w", res.Error)
		}
		affected += int(res.RowsAffected)

		// KISS interfaces: null the channel + flag for reconfig. GORM
		// treats zero-valued booleans in a struct-based Updates as
		// "skip", so we use map updates to force the assignment.
		res = tx.Model(&KissInterface{}).Where("channel = ?", channelID).Updates(map[string]any{
			"channel":        0,
			"needs_reconfig": true,
		})
		if res.Error != nil {
			return fmt.Errorf("cascade kiss interfaces: %w", res.Error)
		}
		affected += int(res.RowsAffected)

		// iGate config singleton: null rf_channel / tx_channel
		// independently so the assignments survive in isolation when
		// only one matches.
		res = tx.Model(&IGateConfig{}).Where("rf_channel = ?", channelID).Update("rf_channel", 0)
		if res.Error != nil {
			return fmt.Errorf("cascade igate rf_channel: %w", res.Error)
		}
		affected += int(res.RowsAffected)
		res = tx.Model(&IGateConfig{}).Where("tx_channel = ?", channelID).Update("tx_channel", 0)
		if res.Error != nil {
			return fmt.Errorf("cascade igate tx_channel: %w", res.Error)
		}
		affected += int(res.RowsAffected)

		// iGate RF filters.
		res = tx.Where("channel = ?", channelID).Delete(&IGateRfFilter{})
		if res.Error != nil {
			return fmt.Errorf("cascade igate rf filters: %w", res.Error)
		}
		affected += int(res.RowsAffected)

		// TxTiming.
		res = tx.Where("channel = ?", channelID).Delete(&TxTiming{})
		if res.Error != nil {
			return fmt.Errorf("cascade tx timings: %w", res.Error)
		}
		affected += int(res.RowsAffected)

		// Finally, the channel itself — triggers ON DELETE CASCADE for
		// PttConfig rows with this channel_id.
		if err := tx.Delete(&Channel{}, channelID).Error; err != nil {
			return fmt.Errorf("cascade channel %d: %w", channelID, err)
		}
		affected++
		return nil
	})
	return affected, err
}

// CountOrphanChannelRefs runs a one-shot scan at bootstrap for rows whose
// channel references don't resolve. Returns a map keyed by a stable
// table-role token (mirroring ReferrerType*) to the number of orphan
// rows in that role. Never returns nil — an empty map means "all refs
// resolve". Tables are scanned with a LEFT JOIN + NULL predicate so the
// query is O(n) per table rather than O(n*m).
//
// The caller is expected to log a warn line per non-zero entry. No
// deletion or cleanup happens here; operators decide whether to remediate
// via the cascade-delete UI.
func (s *Store) CountOrphanChannelRefs(ctx context.Context) (map[string]int, error) {
	out := make(map[string]int)
	db := s.db.WithContext(ctx)

	type countRow struct {
		Cnt int64
	}
	count := func(table, predicate string) (int, error) {
		// We use a sub-select rather than a JOIN so the predicate stays
		// compatible with the per-table column name (kiss_interfaces.channel,
		// digipeater_rules.from_channel, etc.) without having to thread
		// an alias through.
		sql := "SELECT COUNT(*) AS cnt FROM " + table + " WHERE " + predicate +
			" != 0 AND " + predicate + " NOT IN (SELECT id FROM channels)"
		var r countRow
		if err := db.Raw(sql).Scan(&r).Error; err != nil {
			return 0, fmt.Errorf("orphan scan %s.%s: %w", table, predicate, err)
		}
		return int(r.Cnt), nil
	}

	specs := []struct {
		token, table, predicate string
	}{
		{ReferrerTypeBeacon, "beacons", "channel"},
		{ReferrerTypeDigipeaterRuleFrom, "digipeater_rules", "from_channel"},
		{ReferrerTypeDigipeaterRuleTo, "digipeater_rules", "to_channel"},
		{ReferrerTypeKissInterface, "kiss_interfaces", "channel"},
		{ReferrerTypeIGateConfigRf, "i_gate_configs", "rf_channel"},
		{ReferrerTypeIGateConfigTx, "i_gate_configs", "tx_channel"},
		{ReferrerTypeIGateRfFilter, "i_gate_rf_filters", "channel"},
		{ReferrerTypeTxTiming, "tx_timings", "channel"},
	}
	for _, s := range specs {
		n, err := count(s.table, s.predicate)
		if err != nil {
			// Table might not exist on a fresh DB before AutoMigrate,
			// or on a very-old schema — treat as zero orphans rather
			// than failing the whole scan.
			continue
		}
		if n > 0 {
			out[s.token] = n
		}
	}
	return out, nil
}

// OrphanChannelRefRows describes one table's set of rows whose
// channel soft-FK column points at a non-existent channel id. Used by
// the bootstrap audit in pkg/app/wiring.go to emit a per-table WARN
// line listing the affected row ids plus the distinct missing channel
// ids, so operators can locate the referrers without clicking through
// every list page.
type OrphanChannelRefRows struct {
	// Token mirrors one of the ReferrerType* constants above so callers
	// can key their log field consistently with the 409 response body.
	Token string
	// RowIDs is the set of row primary keys that have a dangling ref.
	RowIDs []uint32
	// MissingChannelIDs is the deduplicated set of channel ids those
	// rows point at but that no longer exist. May have fewer entries
	// than RowIDs when several rows share the same missing channel.
	MissingChannelIDs []uint32
}

// ListOrphanChannelRefs returns, per referrer table, the set of row
// ids whose channel soft-FK does not resolve, plus the distinct set of
// missing channel ids referenced. One query per table, same LEFT-JOIN /
// NOT-IN pattern as CountOrphanChannelRefs but returning the ids
// instead of just the count.
//
// An empty slice return means "no orphans anywhere". Per-table probe
// errors are swallowed (e.g. table missing on a fresh DB before
// AutoMigrate) so the overall scan never fails startup.
func (s *Store) ListOrphanChannelRefs(ctx context.Context) ([]OrphanChannelRefRows, error) {
	db := s.db.WithContext(ctx)

	type idRow struct {
		ID      uint32
		Channel uint32
	}
	gather := func(table, idCol, predicate string) ([]idRow, error) {
		sql := "SELECT " + idCol + " AS id, " + predicate + " AS channel FROM " + table +
			" WHERE " + predicate + " != 0 AND " + predicate + " NOT IN (SELECT id FROM channels)"
		var rows []idRow
		if err := db.Raw(sql).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("orphan list %s.%s: %w", table, predicate, err)
		}
		return rows, nil
	}

	specs := []struct {
		token, table, idCol, predicate string
	}{
		{ReferrerTypeBeacon, "beacons", "id", "channel"},
		{ReferrerTypeDigipeaterRuleFrom, "digipeater_rules", "id", "from_channel"},
		{ReferrerTypeDigipeaterRuleTo, "digipeater_rules", "id", "to_channel"},
		{ReferrerTypeKissInterface, "kiss_interfaces", "id", "channel"},
		{ReferrerTypeIGateConfigRf, "i_gate_configs", "id", "rf_channel"},
		{ReferrerTypeIGateConfigTx, "i_gate_configs", "id", "tx_channel"},
		{ReferrerTypeIGateRfFilter, "i_gate_rf_filters", "id", "channel"},
		{ReferrerTypeTxTiming, "tx_timings", "id", "channel"},
	}
	var out []OrphanChannelRefRows
	for _, sp := range specs {
		rows, err := gather(sp.table, sp.idCol, sp.predicate)
		if err != nil {
			// Pristine DB / missing table — skip silently, matching
			// CountOrphanChannelRefs behaviour.
			continue
		}
		if len(rows) == 0 {
			continue
		}
		entry := OrphanChannelRefRows{Token: sp.token}
		seen := make(map[uint32]struct{}, len(rows))
		for _, r := range rows {
			entry.RowIDs = append(entry.RowIDs, r.ID)
			if _, ok := seen[r.Channel]; !ok {
				seen[r.Channel] = struct{}{}
				entry.MissingChannelIDs = append(entry.MissingChannelIDs, r.Channel)
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// PttConfig CRUD
// ---------------------------------------------------------------------------

func (s *Store) UpsertPttConfig(ctx context.Context, p *PttConfig) error {
	var existing PttConfig
	err := s.db.WithContext(ctx).Where("channel_id = ?", p.ChannelID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.db.WithContext(ctx).Create(p).Error
	}
	if err != nil {
		return err
	}
	p.ID = existing.ID
	return s.db.WithContext(ctx).Save(p).Error
}

func (s *Store) GetPttConfigForChannel(ctx context.Context, channelID uint32) (*PttConfig, error) {
	var p PttConfig
	if err := s.db.WithContext(ctx).Where("channel_id = ?", channelID).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) ListPttConfigs(ctx context.Context) ([]PttConfig, error) {
	var list []PttConfig
	if err := s.db.WithContext(ctx).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (s *Store) DeletePttConfig(ctx context.Context, channelID uint32) error {
	return s.db.WithContext(ctx).Where("channel_id = ?", channelID).Delete(&PttConfig{}).Error
}

// ---------------------------------------------------------------------------
// Channel validation
// ---------------------------------------------------------------------------

// validateChannel checks that a channel's input/output device references are
// valid, channels are within bounds, and the channel ID is unique.
// excludeID is the channel's own ID (for updates) or 0 (for creates).
//
// InputDeviceID is optional (nil = KISS-only channel, no modem). When nil,
// OutputDeviceID must be 0 — a channel cannot have TX audio without RX
// audio, and a KISS-only channel has neither. When non-nil, the referenced
// device must exist, have direction=input, and InputChannel must be within
// the device's channel count.
//
// Phase 3 mutual exclusivity (D3): when the channel has an audio input
// device (hence a modem backend), any KissInterface row targeting it
// with Mode=tnc AND AllowTxFromGovernor=true would create a dual-backend
// channel — every frame would double-transmit. Forbidden by design;
// this validator walks the kiss_interfaces table and rejects the edit
// with a clear error naming both rows when the combination is detected.
func (s *Store) validateChannel(ctx context.Context, c *Channel, excludeID uint32) error {
	// Validate input device when bound. A nil InputDeviceID means
	// "KISS-only channel" — the modem subprocess is never told about
	// this channel, so there is no audio device to validate.
	if c.InputDeviceID != nil {
		inDev, err := s.GetAudioDevice(ctx, *c.InputDeviceID)
		if err != nil {
			return fmt.Errorf("invalid input_device_id %d: device not found", *c.InputDeviceID)
		}
		if inDev.Direction != "input" {
			return fmt.Errorf("input_device_id %d: device %q is not an input device", *c.InputDeviceID, inDev.Name)
		}
		if c.InputChannel >= inDev.Channels {
			return fmt.Errorf("input_channel %d out of range for device %q (%d channels)",
				c.InputChannel, inDev.Name, inDev.Channels)
		}
	} else if c.OutputDeviceID != 0 {
		// A KISS-only channel has no RX audio; forbidding TX audio
		// without RX keeps the mental model simple (no "half-audio"
		// channels) and matches the UI's segmented type picker where
		// KISS-TNC channels hide all audio fields.
		return fmt.Errorf("output_device_id must be 0 when input_device_id is null (KISS-only channel)")
	}

	// Validate output device (optional, 0 = RX-only)
	if c.OutputDeviceID != 0 {
		outDev, err := s.GetAudioDevice(ctx, c.OutputDeviceID)
		if err != nil {
			return fmt.Errorf("invalid output_device_id %d: device not found", c.OutputDeviceID)
		}
		if outDev.Direction != "output" {
			return fmt.Errorf("output_device_id %d: device %q is not an output device", c.OutputDeviceID, outDev.Name)
		}
		if c.OutputChannel >= outDev.Channels {
			return fmt.Errorf("output_channel %d out of range for device %q (%d channels)",
				c.OutputChannel, outDev.Name, outDev.Channels)
		}
	}

	// Check ID uniqueness only when the caller has set a specific ID (non-zero
	// on update, or when the caller pre-assigns an ID on create).
	if c.ID != 0 {
		var dup Channel
		q := s.db.WithContext(ctx).Where("id = ? AND id != ?", c.ID, excludeID).First(&dup)
		if q.Error == nil {
			return fmt.Errorf("duplicate channel_num %d", c.ID)
		}
	}

	// D3 mutual exclusivity: a modem-backed channel cannot also be
	// attached to a TNC interface that would receive governor TX.
	// We only need to walk the kiss_interfaces table when this edit
	// sets a non-nil InputDeviceID (the trigger condition). The
	// excludeID / c.ID lookup mirrors the uniqueness check above so
	// an update that merely renames a channel doesn't self-reject.
	if c.InputDeviceID != nil && c.ID != 0 {
		var conflict KissInterface
		err := s.db.WithContext(ctx).
			Where("channel = ? AND mode = ? AND allow_tx_from_governor = ?",
				c.ID, KissModeTnc, true).
			First(&conflict).Error
		if err == nil {
			return fmt.Errorf(
				"channel %d cannot have audio input device while KissInterface %q (id=%d) "+
					"has mode=%q with allow_tx_from_governor=true; clear allow_tx_from_governor "+
					"or detach the interface first",
				c.ID, conflict.Name, conflict.ID, conflict.Mode)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("validate channel: look up conflicting kiss interface: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// FX.25 / IL2P TX config helpers
// ---------------------------------------------------------------------------

// SetChannelFX25 sets FX.25 encoding for a channel.
func (s *Store) SetChannelFX25(ctx context.Context, id uint32, enable bool) error {
	return s.db.WithContext(ctx).Model(&Channel{}).Where("id = ?", id).Update("fx25_encode", enable).Error
}

// SetChannelIL2P sets IL2P encoding for a channel.
func (s *Store) SetChannelIL2P(ctx context.Context, id uint32, enable bool) error {
	return s.db.WithContext(ctx).Model(&Channel{}).Where("id = ?", id).Update("il2p_encode", enable).Error
}

// ---------------------------------------------------------------------------
// KissInterface
// ---------------------------------------------------------------------------

func (s *Store) ListKissInterfaces(ctx context.Context) ([]KissInterface, error) {
	var out []KissInterface
	return out, s.db.WithContext(ctx).Order("id").Find(&out).Error
}

func (s *Store) GetKissInterface(ctx context.Context, id uint32) (*KissInterface, error) {
	var k KissInterface
	if err := s.db.WithContext(ctx).First(&k, id).Error; err != nil {
		return nil, err
	}
	return &k, nil
}
func (s *Store) CreateKissInterface(ctx context.Context, k *KissInterface) error {
	if err := normalizeKissInterface(k); err != nil {
		return err
	}
	if err := s.validateKissInterface(ctx, k); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Create(k).Error
}
func (s *Store) UpdateKissInterface(ctx context.Context, k *KissInterface) error {
	if err := normalizeKissInterface(k); err != nil {
		return err
	}
	if err := s.validateKissInterface(ctx, k); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Save(k).Error
}

// validateKissInterface enforces Phase 3's D3 mutual exclusivity at
// the kiss side of the pair: a TNC interface with AllowTxFromGovernor
// cannot attach to a modem-backed channel. The channel→interface side
// of the same rule lives in validateChannel. Running this on both
// sides catches the mismatch regardless of which row is edited.
//
// When AllowTxFromGovernor is false OR Mode != tnc, no cross-table
// look-up is performed — the interface can attach to any channel
// (it won't receive governor TX, so dual-backend cannot occur).
func (s *Store) validateKissInterface(ctx context.Context, k *KissInterface) error {
	if !k.AllowTxFromGovernor || k.Mode != KissModeTnc {
		return nil
	}
	if k.Channel == 0 {
		return nil // no channel attached yet
	}
	var ch Channel
	err := s.db.WithContext(ctx).First(&ch, k.Channel).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Channel doesn't exist — validateChannel will enforce that
		// separately when the channel is finally created; here we
		// only care about the conflict rule.
		return nil
	}
	if err != nil {
		return fmt.Errorf("validate kiss interface: look up channel %d: %w", k.Channel, err)
	}
	if ch.InputDeviceID != nil {
		return fmt.Errorf(
			"kiss interface %q cannot set allow_tx_from_governor=true with mode=%q while "+
				"attached to channel %d (%q) which has an audio input device; "+
				"detach the audio input device or clear allow_tx_from_governor first",
			k.Name, k.Mode, ch.ID, ch.Name)
	}
	return nil
}

// normalizeKissInterface applies the "absent field" defaults and the
// store-boundary validation for KissInterface. It exists so both
// CreateKissInterface and UpdateKissInterface share a single source of
// truth — the handler layer validates too, but the store-boundary check
// is the backstop that keeps a bad row from ever reaching SQLite if a
// future caller forgets the DTO path.
//
// An empty Mode is silently upgraded to KissModeModem so older clients
// that don't know about the field keep working. Zero rate values are
// treated as "unset" and replaced with the defaults documented on the
// struct tags; the column's NOT NULL constraint would accept 0, but 0
// would disable the Phase 3 token bucket entirely, which is almost
// certainly not what the caller meant.
func normalizeKissInterface(k *KissInterface) error {
	if k.Mode == "" {
		k.Mode = KissModeModem
	}
	if !ValidKissMode(k.Mode) {
		return fmt.Errorf("kiss interface: invalid mode %q: must be %q or %q", k.Mode, KissModeModem, KissModeTnc)
	}
	if k.TncIngressRateHz == 0 {
		k.TncIngressRateHz = DefaultTncIngressRateHz
	}
	if k.TncIngressBurst == 0 {
		k.TncIngressBurst = DefaultTncIngressBurst
	}
	return nil
}
func (s *Store) DeleteKissInterface(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Delete(&KissInterface{}, id).Error
}

// ---------------------------------------------------------------------------
// AgwConfig (singleton)
// ---------------------------------------------------------------------------

func (s *Store) GetAgwConfig(ctx context.Context) (*AgwConfig, error) {
	var c AgwConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertAgwConfig(ctx context.Context, c *AgwConfig) error {
	if c.ID == 0 {
		existing, err := s.GetAgwConfig(ctx)
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	return s.db.WithContext(ctx).Save(c).Error
}

// ---------------------------------------------------------------------------
// TxTiming
// ---------------------------------------------------------------------------

func (s *Store) ListTxTimings(ctx context.Context) ([]TxTiming, error) {
	var out []TxTiming
	return out, s.db.WithContext(ctx).Order("channel").Find(&out).Error
}

func (s *Store) GetTxTiming(ctx context.Context, channel uint32) (*TxTiming, error) {
	var t TxTiming
	err := s.db.WithContext(ctx).Where("channel = ?", channel).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) UpsertTxTiming(ctx context.Context, t *TxTiming) error {
	existing, err := s.GetTxTiming(ctx, t.Channel)
	if err != nil {
		return err
	}
	if existing != nil {
		t.ID = existing.ID
	}
	return s.db.WithContext(ctx).Save(t).Error
}

// ---------------------------------------------------------------------------
// DigipeaterConfig (singleton)
// ---------------------------------------------------------------------------

func (s *Store) GetDigipeaterConfig(ctx context.Context) (*DigipeaterConfig, error) {
	var c DigipeaterConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertDigipeaterConfig(ctx context.Context, c *DigipeaterConfig) error {
	if c.ID == 0 {
		existing, err := s.GetDigipeaterConfig(ctx)
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	return s.db.WithContext(ctx).Save(c).Error
}

// ---------------------------------------------------------------------------
// DigipeaterRule
// ---------------------------------------------------------------------------

func (s *Store) ListDigipeaterRules(ctx context.Context) ([]DigipeaterRule, error) {
	var out []DigipeaterRule
	return out, s.db.WithContext(ctx).Order("priority, id").Find(&out).Error
}

func (s *Store) ListDigipeaterRulesForChannel(ctx context.Context, channel uint32) ([]DigipeaterRule, error) {
	var out []DigipeaterRule
	return out, s.db.WithContext(ctx).Where("from_channel = ? AND enabled = ?", channel, true).
		Order("priority, id").Find(&out).Error
}

func (s *Store) CreateDigipeaterRule(ctx context.Context, r *DigipeaterRule) error {
	return s.db.WithContext(ctx).Create(r).Error
}
func (s *Store) UpdateDigipeaterRule(ctx context.Context, r *DigipeaterRule) error {
	return s.db.WithContext(ctx).Save(r).Error
}
func (s *Store) DeleteDigipeaterRule(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Delete(&DigipeaterRule{}, id).Error
}

// ---------------------------------------------------------------------------
// IGateConfig (singleton) + filters
// ---------------------------------------------------------------------------

func (s *Store) GetIGateConfig(ctx context.Context) (*IGateConfig, error) {
	var c IGateConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertIGateConfig(ctx context.Context, c *IGateConfig) error {
	if c.ID == 0 {
		existing, err := s.GetIGateConfig(ctx)
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	// The callsign and passcode columns remain in the schema for
	// downgrade-safety (see models.go IGateConfig doc comment), but
	// application code no longer uses them. Zero them on every upsert
	// unconditionally so a rollback to a pre-Phase-2 binary sees an
	// empty callsign/passcode and re-prompts the user rather than
	// silently using stale values. Save + scrub run in one transaction
	// so a crash between the two cannot leave a row with fresh config
	// but stale callsign/passcode.
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(c).Error; err != nil {
			return err
		}
		return tx.Model(&IGateConfig{}).Where("id = ?", c.ID).UpdateColumns(map[string]any{
			"callsign": "",
			"passcode": "",
		}).Error
	})
}

func (s *Store) ListIGateRfFilters(ctx context.Context) ([]IGateRfFilter, error) {
	var out []IGateRfFilter
	return out, s.db.WithContext(ctx).Order("priority, id").Find(&out).Error
}

func (s *Store) ListIGateRfFiltersForChannel(ctx context.Context, channel uint32) ([]IGateRfFilter, error) {
	var out []IGateRfFilter
	return out, s.db.WithContext(ctx).Where("channel = ? AND enabled = ?", channel, true).
		Order("priority, id").Find(&out).Error
}

func (s *Store) CreateIGateRfFilter(ctx context.Context, f *IGateRfFilter) error {
	return s.db.WithContext(ctx).Create(f).Error
}
func (s *Store) UpdateIGateRfFilter(ctx context.Context, f *IGateRfFilter) error {
	return s.db.WithContext(ctx).Save(f).Error
}
func (s *Store) DeleteIGateRfFilter(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Delete(&IGateRfFilter{}, id).Error
}

// ---------------------------------------------------------------------------
// Beacon
// ---------------------------------------------------------------------------

func (s *Store) ListBeacons(ctx context.Context) ([]Beacon, error) {
	var out []Beacon
	return out, s.db.WithContext(ctx).Order("id").Find(&out).Error
}

func (s *Store) GetBeacon(ctx context.Context, id uint32) (*Beacon, error) {
	var b Beacon
	if err := s.db.WithContext(ctx).First(&b, id).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Store) CreateBeacon(ctx context.Context, b *Beacon) error {
	return s.db.WithContext(ctx).Create(b).Error
}
func (s *Store) UpdateBeacon(ctx context.Context, b *Beacon) error {
	return s.db.WithContext(ctx).Save(b).Error
}
func (s *Store) DeleteBeacon(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Delete(&Beacon{}, id).Error
}

// ---------------------------------------------------------------------------
// GPSConfig (singleton)
// ---------------------------------------------------------------------------

func (s *Store) GetGPSConfig(ctx context.Context) (*GPSConfig, error) {
	var c GPSConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertGPSConfig(ctx context.Context, c *GPSConfig) error {
	if c.ID == 0 {
		existing, err := s.GetGPSConfig(ctx)
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	return s.db.WithContext(ctx).Save(c).Error
}

// ---------------------------------------------------------------------------
// PacketFilter (stub)
// ---------------------------------------------------------------------------

func (s *Store) ListPacketFilters(ctx context.Context) ([]PacketFilter, error) {
	var out []PacketFilter
	return out, s.db.WithContext(ctx).Order("id").Find(&out).Error
}

// ---------------------------------------------------------------------------
// PositionLogConfig (singleton)
// ---------------------------------------------------------------------------

func (s *Store) GetPositionLogConfig(ctx context.Context) (*PositionLogConfig, error) {
	var c PositionLogConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertPositionLogConfig(ctx context.Context, c *PositionLogConfig) error {
	if c.ID == 0 {
		existing, err := s.GetPositionLogConfig(ctx)
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	return s.db.WithContext(ctx).Save(c).Error
}
