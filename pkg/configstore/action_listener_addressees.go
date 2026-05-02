package configstore

import (
	"context"
	"errors"
	"strings"
	"time"
)

// normalizeAddressee uppercases and trims whitespace from an APRS
// addressee. AX.25 addressees are case-insensitive on the wire so we
// canonicalize on write to keep lookups consistent.
func normalizeAddressee(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func (s *Store) CreateActionListenerAddressee(ctx context.Context, name string) error {
	n := normalizeAddressee(name)
	if n == "" {
		return errors.New("configstore: empty addressee")
	}
	if len(n) > 9 {
		return errors.New("configstore: addressee exceeds 9 chars")
	}
	row := &ActionListenerAddressee{Addressee: n, CreatedAt: time.Now().UTC()}
	return s.db.WithContext(ctx).Create(row).Error
}

func (s *Store) ListActionListenerAddressees(ctx context.Context) ([]ActionListenerAddressee, error) {
	var out []ActionListenerAddressee
	if err := s.db.WithContext(ctx).Order("addressee").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) DeleteActionListenerAddresseeByName(ctx context.Context, name string) error {
	return s.db.WithContext(ctx).
		Where("addressee = ?", normalizeAddressee(name)).
		Delete(&ActionListenerAddressee{}).Error
}
