package configstore

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// FavoriteStation CRUD
// ---------------------------------------------------------------------------

// CreateFavoriteStation inserts a new favorite. Callsign is normalized
// to uppercase by the FavoriteStation.BeforeSave hook.
func (s *Store) CreateFavoriteStation(ctx context.Context, f *FavoriteStation) error {
	return s.db.WithContext(ctx).Create(f).Error
}

// DeleteFavoriteStation removes a favorite by id.
func (s *Store) DeleteFavoriteStation(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Delete(&FavoriteStation{}, id).Error
}

// GetFavoriteStation returns a single favorite by id. Returns (nil, nil)
// on not-found to match the other getters (e.g. GetBlockedCallsign).
func (s *Store) GetFavoriteStation(ctx context.Context, id uint32) (*FavoriteStation, error) {
	var f FavoriteStation
	err := s.db.WithContext(ctx).First(&f, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// ListFavoriteStations returns every favorite, ordered by callsign for
// stable UI display.
func (s *Store) ListFavoriteStations(ctx context.Context) ([]FavoriteStation, error) {
	var out []FavoriteStation
	return out, s.db.WithContext(ctx).Order("callsign").Find(&out).Error
}
