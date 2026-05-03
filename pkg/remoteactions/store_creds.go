package remoteactions

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// CredStore is the persistence layer for RemoteOTPCredential rows.
// One instance per Service; safe for concurrent use (delegates to
// gorm.DB which is goroutine-safe).
type CredStore struct {
	db *gorm.DB
}

func NewCredStore(db *gorm.DB) *CredStore { return &CredStore{db: db} }

// Create inserts a new credential, stamping CreatedAt to time.Now().UTC().
// Returns the unique-constraint error from SQLite verbatim when Name
// collides; callers map that to HTTP 409.
func (s *CredStore) Create(ctx context.Context, c *RemoteOTPCredential) error {
	if c == nil {
		return errors.New("remoteactions: nil credential")
	}
	c.CreatedAt = time.Now().UTC()
	return s.db.WithContext(ctx).Create(c).Error
}

// Get fetches by primary key. Returns gorm.ErrRecordNotFound (use
// errors.Is) when missing.
func (s *CredStore) Get(ctx context.Context, id uint) (*RemoteOTPCredential, error) {
	var c RemoteOTPCredential
	if err := s.db.WithContext(ctx).First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// List returns every credential ordered by Name. The list is small (one
// or two per remote station the operator interacts with) so a full
// scan is fine.
func (s *CredStore) List(ctx context.Context) ([]RemoteOTPCredential, error) {
	var out []RemoteOTPCredential
	if err := s.db.WithContext(ctx).Order("name").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// Update writes the row identified by c.ID. The fields touched are
// Name, SecretB32, Algorithm, Digits, Period — caller is responsible
// for validation. CreatedAt and LastUsedAt are not modified. Returns
// gorm.ErrRecordNotFound when no row matches c.ID so callers can map
// to HTTP 404.
func (s *CredStore) Update(ctx context.Context, c *RemoteOTPCredential) error {
	if c == nil || c.ID == 0 {
		return errors.New("remoteactions: nil credential or zero id")
	}
	res := s.db.WithContext(ctx).Model(&RemoteOTPCredential{}).
		Where("id = ?", c.ID).
		Updates(map[string]any{
			"name":       c.Name,
			"secret_b32": c.SecretB32,
			"algorithm":  c.Algorithm,
			"digits":     c.Digits,
			"period":     c.Period,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// Delete removes the credential. Macros bound to it have their
// remote_otp_credential_id nulled by the FK ON DELETE SET NULL.
func (s *CredStore) Delete(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&RemoteOTPCredential{}, id).Error
}

// TouchLastUsed records the most recent moment a credential generated
// a TOTP code. UTC always; the UI sorts the picker by this column so
// recently-used credentials float to the top.
func (s *CredStore) TouchLastUsed(ctx context.Context, id uint, when time.Time) error {
	return s.db.WithContext(ctx).Model(&RemoteOTPCredential{}).
		Where("id = ?", id).
		Update("last_used_at", when.UTC()).Error
}

// UsedBy returns a map of credential id -> distinct uppercased target
// callsigns whose macros reference it. One scan over the macros table;
// the REST list endpoint joins this map onto the credential rows so
// the deletion gate ("Unbind from N macro(s) first") can render
// without a per-credential query.
func (s *CredStore) UsedBy(ctx context.Context) (map[uint][]string, error) {
	type row struct {
		CredID     uint
		TargetCall string
	}
	var rows []row
	if err := s.db.WithContext(ctx).
		Table("remote_action_macros").
		Select("DISTINCT remote_otp_credential_id AS cred_id, target_call").
		Where("remote_otp_credential_id IS NOT NULL").
		Order("target_call").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[uint][]string{}
	for _, r := range rows {
		out[r.CredID] = append(out[r.CredID], r.TargetCall)
	}
	return out, nil
}
