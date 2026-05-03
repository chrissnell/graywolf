package configstore

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

func (s *Store) CreateOTPCredential(ctx context.Context, c *OTPCredential) error {
	if c == nil {
		return errors.New("configstore: nil credential")
	}
	c.CreatedAt = time.Now().UTC()
	return s.db.WithContext(ctx).Create(c).Error
}

func (s *Store) GetOTPCredential(ctx context.Context, id uint) (*OTPCredential, error) {
	var c OTPCredential
	if err := s.db.WithContext(ctx).First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) GetOTPCredentialByName(ctx context.Context, name string) (*OTPCredential, error) {
	var c OTPCredential
	if err := s.db.WithContext(ctx).Where("name = ?", name).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ListOTPCredentials(ctx context.Context) ([]OTPCredential, error) {
	var out []OTPCredential
	if err := s.db.WithContext(ctx).Order("name").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) DeleteOTPCredential(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&OTPCredential{}, id).Error
}

// TouchOTPCredentialUsed records the most recent moment a credential
// successfully verified a TOTP code. Stored UTC; the UI surfaces this so
// operators can spot dormant credentials.
func (s *Store) TouchOTPCredentialUsed(ctx context.Context, id uint, when time.Time) error {
	return s.db.WithContext(ctx).Model(&OTPCredential{}).
		Where("id = ?", id).
		Update("last_used_at", when.UTC()).Error
}

// OTPCredentialUsedBy returns a map cred-id -> action names that
// reference it. One scan over the actions table; callers iterate the
// returned map per credential rather than issuing N queries.
func (s *Store) OTPCredentialUsedBy(ctx context.Context) (map[uint][]string, error) {
	type row struct {
		CredID uint
		Name   string
	}
	var rows []row
	if err := s.db.WithContext(ctx).
		Table("actions").
		Select("otp_credential_id AS cred_id, name").
		Where("otp_credential_id IS NOT NULL").
		Order("name").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[uint][]string{}
	for _, r := range rows {
		out[r.CredID] = append(out[r.CredID], r.Name)
	}
	return out, nil
}

// IsNotFound mirrors gorm.ErrRecordNotFound for callers that want a
// stable not-found check without importing gorm directly.
func IsNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
