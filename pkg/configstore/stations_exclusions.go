package configstore

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// ExcludedStation CRUD
// ---------------------------------------------------------------------------

// CreateExcludedStation inserts a new exclusion. Callsign is normalized
// to uppercase by the ExcludedStation.BeforeSave hook.
func (s *Store) CreateExcludedStation(ctx context.Context, e *ExcludedStation) error {
	return s.db.WithContext(ctx).Create(e).Error
}

// DeleteExcludedStation removes an exclusion by id.
func (s *Store) DeleteExcludedStation(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Delete(&ExcludedStation{}, id).Error
}

// GetExcludedStation returns a single exclusion by id. Returns (nil, nil)
// on not-found to match the other getters (e.g. GetFavoriteStation).
func (s *Store) GetExcludedStation(ctx context.Context, id uint32) (*ExcludedStation, error) {
	var e ExcludedStation
	err := s.db.WithContext(ctx).First(&e, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListExcludedStations returns every exclusion, ordered by callsign for
// stable UI display.
func (s *Store) ListExcludedStations(ctx context.Context) ([]ExcludedStation, error) {
	var out []ExcludedStation
	return out, s.db.WithContext(ctx).Order("callsign").Find(&out).Error
}
