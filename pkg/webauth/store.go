package webauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ErrSetupAlreadyComplete is returned by CreateFirstUser when a user already
// exists in the database.
var ErrSetupAlreadyComplete = errors.New("webauth: setup already complete")

// WebUser is a credential record for the web UI.
//
// LastSeenReleaseVersion is the high-water mark of release-notes
// acknowledgement. Empty string (the default for AutoMigrate'd
// existing rows) is treated by releasenotes.Compare as less than any
// real version, so an existing user on first login after upgrade sees
// the full backlog. New users (created via CreateFirstUser /
// CreateUser) are seeded with the running build version so they
// don't get the backlog. Gorm size:20 leaves headroom for
// "999.999.999" (the longest strict x.y.z we'd ever emit).
type WebUser struct {
	ID                     uint32 `gorm:"primaryKey;autoIncrement"`
	Username               string `gorm:"uniqueIndex;not null"`
	PasswordHash           string `gorm:"not null"`
	LastSeenReleaseVersion string `gorm:"size:20"`
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// WebSession ties a bearer token to a user with an expiry.
// Table name is "auth_sessions" to avoid collision with configstore's web_sessions.
type WebSession struct {
	ID        uint32 `gorm:"primaryKey;autoIncrement"`
	Token     string `gorm:"uniqueIndex;not null"`
	UserID    uint32 `gorm:"not null;index"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time
}

func (WebSession) TableName() string { return "auth_sessions" }

// AuthStore persists web users and sessions via GORM.
type AuthStore struct {
	db *gorm.DB
}

// NewAuthStore wraps an existing GORM DB and auto-migrates auth tables.
func NewAuthStore(db *gorm.DB) (*AuthStore, error) {
	s := &AuthStore{db: db}
	if err := db.AutoMigrate(&WebUser{}, &WebSession{}); err != nil {
		return nil, fmt.Errorf("auth migrate: %w", err)
	}
	return s, nil
}

// CreateUser inserts a new user and seeds LastSeenReleaseVersion to
// buildVersion so the user does not see the release-notes backlog on
// first login. An empty buildVersion is permitted (a CLI utility or
// test with no build-time version plumbed through) but the user will
// see every note on first login.
func (s *AuthStore) CreateUser(ctx context.Context, username, passwordHash, buildVersion string) (*WebUser, error) {
	u := &WebUser{Username: username, PasswordHash: passwordHash, LastSeenReleaseVersion: buildVersion}
	if err := s.db.WithContext(ctx).Create(u).Error; err != nil {
		return nil, err
	}
	return u, nil
}

// CreateFirstUser atomically creates the first user in the system. Returns
// ErrSetupAlreadyComplete if any user already exists. Safe under concurrent
// requests.
//
// buildVersion seeds LastSeenReleaseVersion so the first user does not
// see the release-notes backlog — they just installed, everything is
// "current" by definition.
//
// Relies on SQLite serializable writers; if we move to a concurrent DB this
// needs a different strategy (e.g. an explicit advisory lock or an INSERT
// guarded by a WHERE NOT EXISTS subquery).
func (s *AuthStore) CreateFirstUser(ctx context.Context, username, passwordHash, buildVersion string) (*WebUser, error) {
	u := &WebUser{Username: username, PasswordHash: passwordHash, LastSeenReleaseVersion: buildVersion}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&WebUser{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrSetupAlreadyComplete
		}
		return tx.Create(u).Error
	})
	if err != nil {
		return nil, err
	}
	return u, nil
}

// SetLastSeenReleaseVersion records that the user has acknowledged
// every release note up to and including version. Idempotent.
// Returns an error if no row matched (stale session whose user was
// deleted) so the caller's 204 response doesn't lie about the write.
func (s *AuthStore) SetLastSeenReleaseVersion(ctx context.Context, userID uint32, version string) error {
	tx := s.db.WithContext(ctx).
		Model(&WebUser{}).
		Where("id = ?", userID).
		Update("last_seen_release_version", version)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return fmt.Errorf("webauth: user id %d not found", userID)
	}
	return nil
}

// GetLastSeenReleaseVersion returns the stored high-water mark for the
// given user. Empty string is the zero-value default.
func (s *AuthStore) GetLastSeenReleaseVersion(ctx context.Context, userID uint32) (string, error) {
	var u WebUser
	if err := s.db.WithContext(ctx).
		Select("last_seen_release_version").
		First(&u, userID).Error; err != nil {
		return "", err
	}
	return u.LastSeenReleaseVersion, nil
}

func (s *AuthStore) GetUserByUsername(ctx context.Context, username string) (*WebUser, error) {
	var u WebUser
	if err := s.db.WithContext(ctx).Where("username = ?", username).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *AuthStore) ListUsers(ctx context.Context) ([]WebUser, error) {
	var out []WebUser
	return out, s.db.WithContext(ctx).Order("username").Find(&out).Error
}

func (s *AuthStore) DeleteUser(ctx context.Context, username string) error {
	// Delete associated sessions first.
	db := s.db.WithContext(ctx)
	var u WebUser
	if err := db.Where("username = ?", username).First(&u).Error; err != nil {
		return err
	}
	if err := db.Where("user_id = ?", u.ID).Delete(&WebSession{}).Error; err != nil {
		return err
	}
	return db.Delete(&u).Error
}

func (s *AuthStore) CreateSession(ctx context.Context, userID uint32, token string, expiry time.Time) (*WebSession, error) {
	ws := &WebSession{Token: token, UserID: userID, ExpiresAt: expiry}
	if err := s.db.WithContext(ctx).Create(ws).Error; err != nil {
		return nil, err
	}
	return ws, nil
}

// GetSessionByToken returns the session only if it hasn't expired.
func (s *AuthStore) GetSessionByToken(ctx context.Context, token string) (*WebSession, error) {
	var ws WebSession
	err := s.db.WithContext(ctx).Where("token = ? AND expires_at > ?", token, time.Now()).First(&ws).Error
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

func (s *AuthStore) DeleteSession(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Where("token = ?", token).Delete(&WebSession{}).Error
}

func (s *AuthStore) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tx := s.db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&WebSession{})
	return tx.RowsAffected, tx.Error
}

func (s *AuthStore) UserCount(ctx context.Context) (int64, error) {
	var count int64
	return count, s.db.WithContext(ctx).Model(&WebUser{}).Count(&count).Error
}
