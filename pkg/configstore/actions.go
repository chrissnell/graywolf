package configstore

import (
	"context"
	"errors"
	"time"
)

func (s *Store) CreateAction(ctx context.Context, a *Action) error {
	if a == nil {
		return errors.New("configstore: nil action")
	}
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now
	return s.db.WithContext(ctx).Create(a).Error
}

func (s *Store) UpdateAction(ctx context.Context, a *Action) error {
	if a == nil || a.ID == 0 {
		return errors.New("configstore: action requires ID for update")
	}
	a.UpdatedAt = time.Now().UTC()
	return s.db.WithContext(ctx).Save(a).Error
}

func (s *Store) GetAction(ctx context.Context, id uint) (*Action, error) {
	var a Action
	if err := s.db.WithContext(ctx).First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) GetActionByName(ctx context.Context, name string) (*Action, error) {
	var a Action
	if err := s.db.WithContext(ctx).Where("name = ?", name).First(&a).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) ListActions(ctx context.Context) ([]Action, error) {
	var out []Action
	if err := s.db.WithContext(ctx).Order("name").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) DeleteAction(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&Action{}, id).Error
}
