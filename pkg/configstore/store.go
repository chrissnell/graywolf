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
		&WebAuth{},
		&WebSession{},
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
// WebAuth / WebSession (stubs — consumed in later phases)
// ---------------------------------------------------------------------------

func (s *Store) UpsertWebAuth(w *WebAuth) error {
	return s.db.Save(w).Error
}

func (s *Store) GetWebAuth(username string) (*WebAuth, error) {
	var w WebAuth
	if err := s.db.First(&w, "username = ?", username).Error; err != nil {
		return nil, err
	}
	return &w, nil
}

func (s *Store) CreateWebSession(ws *WebSession) error {
	return s.db.Create(ws).Error
}

func (s *Store) GetWebSession(token string) (*WebSession, error) {
	var ws WebSession
	if err := s.db.First(&ws, "token = ?", token).Error; err != nil {
		return nil, err
	}
	return &ws, nil
}

func (s *Store) DeleteWebSession(token string) error {
	return s.db.Delete(&WebSession{}, "token = ?", token).Error
}
