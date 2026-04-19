// Package configstore persists graywolf configuration in a SQLite database
// via GORM. Pure-Go (no cgo) via glebarez/sqlite.
package configstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	return openDSN(path)
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
	); err != nil {
		return err
	}
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
func (s *Store) validateChannel(ctx context.Context, c *Channel, excludeID uint32) error {
	// Validate input device (required)
	inDev, err := s.GetAudioDevice(ctx, c.InputDeviceID)
	if err != nil {
		return fmt.Errorf("invalid input_device_id %d: device not found", c.InputDeviceID)
	}
	if inDev.Direction != "input" {
		return fmt.Errorf("input_device_id %d: device %q is not an input device", c.InputDeviceID, inDev.Name)
	}
	if c.InputChannel >= inDev.Channels {
		return fmt.Errorf("input_channel %d out of range for device %q (%d channels)",
			c.InputChannel, inDev.Name, inDev.Channels)
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
	return s.db.WithContext(ctx).Create(k).Error
}
func (s *Store) UpdateKissInterface(ctx context.Context, k *KissInterface) error {
	return s.db.WithContext(ctx).Save(k).Error
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
	return s.db.WithContext(ctx).Save(c).Error
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
