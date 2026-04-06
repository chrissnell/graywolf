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
	return s.migrateChannelDeviceFields()
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
		ModemType:     "afsk",
		BitRate:       1200,
		MarkFreq:      1200,
		SpaceFreq:     2200,
		Profile:       "A",
		NumSlicers:    1,
		NumDecoders:   1,
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
