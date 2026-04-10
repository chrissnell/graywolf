// Package configstore persists graywolf configuration in a SQLite database
// via GORM. Pure-Go (no cgo) via glebarez/sqlite.
package configstore

import (
	"errors"
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store wraps a *gorm.DB with typed helpers for graywolf's tables.
type Store struct {
	db *gorm.DB
}

// Open opens (or creates) the SQLite database at path and seeds first-run
// defaults. Use OpenMemory for tests (no seeding).
func Open(path string) (*Store, error) {
	s, err := openDSN(path)
	if err != nil {
		return nil, err
	}
	if err := s.seedDefaults(); err != nil {
		return nil, fmt.Errorf("seed defaults: %w", err)
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

// Close releases the database handle.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Migrate brings the schema up to date. Safe to call repeatedly.
func (s *Store) Migrate() error {
	if err := s.db.AutoMigrate(
		&AudioDevice{},
		&Channel{},
		&PttConfig{},
		// Phase 4 additions.
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
	); err != nil {
		return err
	}
	if err := s.migrateChannelDeviceFields(); err != nil {
		return err
	}
	return s.migrateBeaconCompressDefault()
}

// migrateBeaconCompressDefault flips every existing beacon row to
// compress=1 exactly once. Earlier versions defaulted the column to
// false but never wired it to the encoder, so any stored 0 is a legacy
// artifact, not an operator choice. Gated by PRAGMA user_version so we
// don't stomp a deliberate post-migration change.
func (s *Store) migrateBeaconCompressDefault() error {
	var version int
	if err := s.db.Raw("PRAGMA user_version").Scan(&version).Error; err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if version >= 1 {
		return nil
	}
	if err := s.db.Exec("UPDATE beacons SET compress = 1 WHERE compress = 0").Error; err != nil {
		return fmt.Errorf("migrate beacon compress default: %w", err)
	}
	if err := s.db.Exec("PRAGMA user_version = 1").Error; err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}
	return nil
}

// migrateChannelDeviceFields migrates the old single audio_device_id/audio_channel
// columns to the new input_device_id/input_channel/output_device_id/output_channel
// split. No-op if the old columns no longer exist.
func (s *Store) migrateChannelDeviceFields() error {
	var count int
	s.db.Raw("SELECT COUNT(*) FROM pragma_table_info('channels') WHERE name='audio_device_id'").Scan(&count)
	if count == 0 {
		return nil // already migrated or fresh DB
	}
	if err := s.db.Exec("UPDATE channels SET input_device_id = audio_device_id, input_channel = audio_channel, output_device_id = 0, output_channel = 0 WHERE input_device_id = 0").Error; err != nil {
		return fmt.Errorf("migrate channel device fields: %w", err)
	}
	if err := s.db.Exec("ALTER TABLE channels DROP COLUMN audio_device_id").Error; err != nil {
		return fmt.Errorf("drop audio_device_id: %w", err)
	}
	if err := s.db.Exec("ALTER TABLE channels DROP COLUMN audio_channel").Error; err != nil {
		return fmt.Errorf("drop audio_channel: %w", err)
	}
	return nil
}

// seedDefaults populates a first-run database with a sensible starting
// configuration: one soundcard audio device and one AFSK 1200 channel.
// It's a no-op if any audio devices already exist.
func (s *Store) seedDefaults() error {
	var count int64
	if err := s.db.Model(&AudioDevice{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	dev := &AudioDevice{
		Name:       "Default Input",
		Direction:  "input",
		SourceType: "soundcard",
		SourcePath: "default",
		SampleRate: 48000,
		Channels:   1,
		Format:     "s16le",
	}
	if err := s.db.Create(dev).Error; err != nil {
		return fmt.Errorf("seed audio device: %w", err)
	}

	ch := &Channel{
		Name:           "Channel 1",
		InputDeviceID:  dev.ID,
		InputChannel:   0,
		OutputDeviceID: 0,
		ModemType:      "afsk",
		BitRate:        1200,
		MarkFreq:       1200,
		SpaceFreq:      2200,
		Profile:        "A",
		NumSlicers:     1,
		NumDecoders:    1,
	}
	if err := s.CreateChannel(ch); err != nil {
		return fmt.Errorf("seed channel: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// AudioDevice CRUD
// ---------------------------------------------------------------------------

func (s *Store) CreateAudioDevice(d *AudioDevice) error {
	return s.db.Create(d).Error
}

func (s *Store) GetAudioDevice(id uint32) (*AudioDevice, error) {
	var d AudioDevice
	if err := s.db.First(&d, id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) ListAudioDevices() ([]AudioDevice, error) {
	var out []AudioDevice
	return out, s.db.Order("id").Find(&out).Error
}

func (s *Store) UpdateAudioDevice(d *AudioDevice) error {
	return s.db.Save(d).Error
}

func (s *Store) DeleteAudioDevice(id uint32) error {
	return s.db.Delete(&AudioDevice{}, id).Error
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
func (s *Store) DeleteAudioDeviceChecked(id uint32, cascade bool) (deleted []Channel, refs []Channel, err error) {
	err = s.db.Transaction(func(tx *gorm.DB) error {
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

func (s *Store) CreateChannel(c *Channel) error {
	if err := s.validateChannel(c, 0); err != nil {
		return err
	}
	return s.db.Create(c).Error
}

func (s *Store) GetChannel(id uint32) (*Channel, error) {
	var c Channel
	if err := s.db.First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ListChannels() ([]Channel, error) {
	var out []Channel
	return out, s.db.Order("id").Find(&out).Error
}

func (s *Store) UpdateChannel(c *Channel) error {
	if err := s.validateChannel(c, c.ID); err != nil {
		return err
	}
	return s.db.Save(c).Error
}

func (s *Store) DeleteChannel(id uint32) error {
	return s.db.Delete(&Channel{}, id).Error
}

// ---------------------------------------------------------------------------
// PttConfig CRUD
// ---------------------------------------------------------------------------

func (s *Store) UpsertPttConfig(p *PttConfig) error {
	var existing PttConfig
	err := s.db.Where("channel_id = ?", p.ChannelID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.db.Create(p).Error
	}
	if err != nil {
		return err
	}
	p.ID = existing.ID
	return s.db.Save(p).Error
}

func (s *Store) GetPttConfigForChannel(channelID uint32) (*PttConfig, error) {
	var p PttConfig
	if err := s.db.Where("channel_id = ?", channelID).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) ListPttConfigs() ([]PttConfig, error) {
	var list []PttConfig
	if err := s.db.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (s *Store) DeletePttConfig(id uint32) error {
	return s.db.Delete(&PttConfig{}, id).Error
}

// ---------------------------------------------------------------------------
// Channel validation
// ---------------------------------------------------------------------------

// validateChannel checks that a channel's input/output device references are
// valid, channels are within bounds, and the channel ID is unique.
// excludeID is the channel's own ID (for updates) or 0 (for creates).
func (s *Store) validateChannel(c *Channel, excludeID uint32) error {
	// Validate input device (required)
	inDev, err := s.GetAudioDevice(c.InputDeviceID)
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
		outDev, err := s.GetAudioDevice(c.OutputDeviceID)
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
		q := s.db.Where("id = ? AND id != ?", c.ID, excludeID).First(&dup)
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
func (s *Store) SetChannelFX25(id uint32, enable bool) error {
	return s.db.Model(&Channel{}).Where("id = ?", id).Update("fx25_encode", enable).Error
}

// SetChannelIL2P sets IL2P encoding for a channel.
func (s *Store) SetChannelIL2P(id uint32, enable bool) error {
	return s.db.Model(&Channel{}).Where("id = ?", id).Update("il2p_encode", enable).Error
}

// ---------------------------------------------------------------------------
// KissInterface
// ---------------------------------------------------------------------------

func (s *Store) ListKissInterfaces() ([]KissInterface, error) {
	var out []KissInterface
	return out, s.db.Order("id").Find(&out).Error
}

func (s *Store) GetKissInterface(id uint32) (*KissInterface, error) {
	var k KissInterface
	if err := s.db.First(&k, id).Error; err != nil {
		return nil, err
	}
	return &k, nil
}
func (s *Store) CreateKissInterface(k *KissInterface) error { return s.db.Create(k).Error }
func (s *Store) UpdateKissInterface(k *KissInterface) error { return s.db.Save(k).Error }
func (s *Store) DeleteKissInterface(id uint32) error {
	return s.db.Delete(&KissInterface{}, id).Error
}

// ---------------------------------------------------------------------------
// AgwConfig (singleton)
// ---------------------------------------------------------------------------

func (s *Store) GetAgwConfig() (*AgwConfig, error) {
	var c AgwConfig
	err := s.db.Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertAgwConfig(c *AgwConfig) error {
	if c.ID == 0 {
		existing, err := s.GetAgwConfig()
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	return s.db.Save(c).Error
}

// ---------------------------------------------------------------------------
// TxTiming
// ---------------------------------------------------------------------------

func (s *Store) ListTxTimings() ([]TxTiming, error) {
	var out []TxTiming
	return out, s.db.Order("channel").Find(&out).Error
}

func (s *Store) GetTxTiming(channel uint32) (*TxTiming, error) {
	var t TxTiming
	err := s.db.Where("channel = ?", channel).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) UpsertTxTiming(t *TxTiming) error {
	existing, err := s.GetTxTiming(t.Channel)
	if err != nil {
		return err
	}
	if existing != nil {
		t.ID = existing.ID
	}
	return s.db.Save(t).Error
}

// ---------------------------------------------------------------------------
// DigipeaterConfig (singleton)
// ---------------------------------------------------------------------------

func (s *Store) GetDigipeaterConfig() (*DigipeaterConfig, error) {
	var c DigipeaterConfig
	err := s.db.Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertDigipeaterConfig(c *DigipeaterConfig) error {
	if c.ID == 0 {
		existing, err := s.GetDigipeaterConfig()
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	return s.db.Save(c).Error
}

// ---------------------------------------------------------------------------
// DigipeaterRule
// ---------------------------------------------------------------------------

func (s *Store) ListDigipeaterRules() ([]DigipeaterRule, error) {
	var out []DigipeaterRule
	return out, s.db.Order("priority, id").Find(&out).Error
}

func (s *Store) ListDigipeaterRulesForChannel(channel uint32) ([]DigipeaterRule, error) {
	var out []DigipeaterRule
	return out, s.db.Where("from_channel = ? AND enabled = ?", channel, true).
		Order("priority, id").Find(&out).Error
}

func (s *Store) CreateDigipeaterRule(r *DigipeaterRule) error { return s.db.Create(r).Error }
func (s *Store) UpdateDigipeaterRule(r *DigipeaterRule) error { return s.db.Save(r).Error }
func (s *Store) DeleteDigipeaterRule(id uint32) error {
	return s.db.Delete(&DigipeaterRule{}, id).Error
}

// ---------------------------------------------------------------------------
// IGateConfig (singleton) + filters
// ---------------------------------------------------------------------------

func (s *Store) GetIGateConfig() (*IGateConfig, error) {
	var c IGateConfig
	err := s.db.Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertIGateConfig(c *IGateConfig) error {
	if c.ID == 0 {
		existing, err := s.GetIGateConfig()
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	return s.db.Save(c).Error
}

func (s *Store) ListIGateRfFilters() ([]IGateRfFilter, error) {
	var out []IGateRfFilter
	return out, s.db.Order("priority, id").Find(&out).Error
}

func (s *Store) ListIGateRfFiltersForChannel(channel uint32) ([]IGateRfFilter, error) {
	var out []IGateRfFilter
	return out, s.db.Where("channel = ? AND enabled = ?", channel, true).
		Order("priority, id").Find(&out).Error
}

func (s *Store) CreateIGateRfFilter(f *IGateRfFilter) error { return s.db.Create(f).Error }
func (s *Store) UpdateIGateRfFilter(f *IGateRfFilter) error { return s.db.Save(f).Error }
func (s *Store) DeleteIGateRfFilter(id uint32) error {
	return s.db.Delete(&IGateRfFilter{}, id).Error
}

// ---------------------------------------------------------------------------
// Beacon
// ---------------------------------------------------------------------------

func (s *Store) ListBeacons() ([]Beacon, error) {
	var out []Beacon
	return out, s.db.Order("id").Find(&out).Error
}

func (s *Store) GetBeacon(id uint32) (*Beacon, error) {
	var b Beacon
	if err := s.db.First(&b, id).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Store) CreateBeacon(b *Beacon) error { return s.db.Create(b).Error }
func (s *Store) UpdateBeacon(b *Beacon) error { return s.db.Save(b).Error }
func (s *Store) DeleteBeacon(id uint32) error { return s.db.Delete(&Beacon{}, id).Error }

// ---------------------------------------------------------------------------
// GPSConfig (singleton)
// ---------------------------------------------------------------------------

func (s *Store) GetGPSConfig() (*GPSConfig, error) {
	var c GPSConfig
	err := s.db.Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertGPSConfig(c *GPSConfig) error {
	if c.ID == 0 {
		existing, err := s.GetGPSConfig()
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	return s.db.Save(c).Error
}

// ---------------------------------------------------------------------------
// PacketFilter (stub)
// ---------------------------------------------------------------------------

func (s *Store) ListPacketFilters() ([]PacketFilter, error) {
	var out []PacketFilter
	return out, s.db.Order("id").Find(&out).Error
}
