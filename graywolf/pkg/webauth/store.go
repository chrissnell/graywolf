package webauth

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// WebUser is a credential record for the web UI.
type WebUser struct {
	ID           uint32 `gorm:"primaryKey;autoIncrement"`
	Username     string `gorm:"uniqueIndex;not null"`
	PasswordHash string `gorm:"not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
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

func (s *AuthStore) CreateUser(_ context.Context, username, passwordHash string) (*WebUser, error) {
	u := &WebUser{Username: username, PasswordHash: passwordHash}
	if err := s.db.Create(u).Error; err != nil {
		return nil, err
	}
	return u, nil
}

func (s *AuthStore) GetUserByUsername(_ context.Context, username string) (*WebUser, error) {
	var u WebUser
	if err := s.db.Where("username = ?", username).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *AuthStore) ListUsers(_ context.Context) ([]WebUser, error) {
	var out []WebUser
	return out, s.db.Order("username").Find(&out).Error
}

func (s *AuthStore) DeleteUser(_ context.Context, username string) error {
	// Delete associated sessions first.
	var u WebUser
	if err := s.db.Where("username = ?", username).First(&u).Error; err != nil {
		return err
	}
	if err := s.db.Where("user_id = ?", u.ID).Delete(&WebSession{}).Error; err != nil {
		return err
	}
	return s.db.Delete(&u).Error
}

func (s *AuthStore) CreateSession(_ context.Context, userID uint32, token string, expiry time.Time) (*WebSession, error) {
	ws := &WebSession{Token: token, UserID: userID, ExpiresAt: expiry}
	if err := s.db.Create(ws).Error; err != nil {
		return nil, err
	}
	return ws, nil
}

// GetSessionByToken returns the session only if it hasn't expired.
func (s *AuthStore) GetSessionByToken(_ context.Context, token string) (*WebSession, error) {
	var ws WebSession
	err := s.db.Where("token = ? AND expires_at > ?", token, time.Now()).First(&ws).Error
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

func (s *AuthStore) DeleteSession(_ context.Context, token string) error {
	return s.db.Where("token = ?", token).Delete(&WebSession{}).Error
}

func (s *AuthStore) DeleteExpiredSessions(_ context.Context) (int64, error) {
	tx := s.db.Where("expires_at <= ?", time.Now()).Delete(&WebSession{})
	return tx.RowsAffected, tx.Error
}

func (s *AuthStore) UserCount(_ context.Context) (int64, error) {
	var count int64
	return count, s.db.Model(&WebUser{}).Count(&count).Error
}
