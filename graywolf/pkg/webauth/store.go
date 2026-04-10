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

func (s *AuthStore) CreateUser(ctx context.Context, username, passwordHash string) (*WebUser, error) {
	u := &WebUser{Username: username, PasswordHash: passwordHash}
	if err := s.db.WithContext(ctx).Create(u).Error; err != nil {
		return nil, err
	}
	return u, nil
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
