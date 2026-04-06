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

// Open opens (or creates) the SQLite database at path. Use ":memory:" for
// tests.
func Open(path string) (*Store, error) {
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
func (s *Store) Migrate() error {
	return s.db.AutoMigrate(
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
	)
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


// ---------------------------------------------------------------------------
// Channel validation
// ---------------------------------------------------------------------------

// validateChannel checks that a channel's device_id references a valid audio
// device, audio_channel is within the device's channel count, and channel_num
// (ID) is unique. excludeID is the channel's own ID (for updates) or 0 (for
// creates).
func (s *Store) validateChannel(c *Channel, excludeID uint32) error {
	dev, err := s.GetAudioDevice(c.AudioDeviceID)
	if err != nil {
		return fmt.Errorf("invalid device_id %d: device not found", c.AudioDeviceID)
	}
	if c.AudioChannel >= dev.Channels {
		return fmt.Errorf("audio_channel %d out of range for device %q (%d channels)",
			c.AudioChannel, dev.Name, dev.Channels)
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
