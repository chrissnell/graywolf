package configstore

import (
	"errors"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// KissInterface
// ---------------------------------------------------------------------------

func (s *Store) ListKissInterfaces() ([]KissInterface, error) {
	var out []KissInterface
	return out, s.db.Order("id").Find(&out).Error
}

func (s *Store) GetKissInterface(id uint32) (*KissInterface, error) {
	var k KissInterface
	if err := s.db.First(&k, id).Error; err != nil {
		return nil, err
	}
	return &k, nil
}
func (s *Store) CreateKissInterface(k *KissInterface) error { return s.db.Create(k).Error }
func (s *Store) UpdateKissInterface(k *KissInterface) error { return s.db.Save(k).Error }
func (s *Store) DeleteKissInterface(id uint32) error {
	return s.db.Delete(&KissInterface{}, id).Error
}

// ---------------------------------------------------------------------------
// AgwConfig (singleton)
// ---------------------------------------------------------------------------

func (s *Store) GetAgwConfig() (*AgwConfig, error) {
	var c AgwConfig
	err := s.db.Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertAgwConfig(c *AgwConfig) error {
	if c.ID == 0 {
		existing, err := s.GetAgwConfig()
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	return s.db.Save(c).Error
}

// ---------------------------------------------------------------------------
// TxTiming
// ---------------------------------------------------------------------------

func (s *Store) ListTxTimings() ([]TxTiming, error) {
	var out []TxTiming
	return out, s.db.Order("channel").Find(&out).Error
}

func (s *Store) GetTxTiming(channel uint32) (*TxTiming, error) {
	var t TxTiming
	err := s.db.Where("channel = ?", channel).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) UpsertTxTiming(t *TxTiming) error {
	existing, err := s.GetTxTiming(t.Channel)
	if err != nil {
		return err
	}
	if existing != nil {
		t.ID = existing.ID
	}
	return s.db.Save(t).Error
}

// ---------------------------------------------------------------------------
// DigipeaterConfig (singleton)
// ---------------------------------------------------------------------------

func (s *Store) GetDigipeaterConfig() (*DigipeaterConfig, error) {
	var c DigipeaterConfig
	err := s.db.Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertDigipeaterConfig(c *DigipeaterConfig) error {
	if c.ID == 0 {
		existing, err := s.GetDigipeaterConfig()
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	return s.db.Save(c).Error
}

// ---------------------------------------------------------------------------
// DigipeaterRule
// ---------------------------------------------------------------------------

func (s *Store) ListDigipeaterRules() ([]DigipeaterRule, error) {
	var out []DigipeaterRule
	return out, s.db.Order("priority, id").Find(&out).Error
}

func (s *Store) ListDigipeaterRulesForChannel(channel uint32) ([]DigipeaterRule, error) {
	var out []DigipeaterRule
	return out, s.db.Where("from_channel = ? AND enabled = ?", channel, true).
		Order("priority, id").Find(&out).Error
}

func (s *Store) CreateDigipeaterRule(r *DigipeaterRule) error { return s.db.Create(r).Error }
func (s *Store) UpdateDigipeaterRule(r *DigipeaterRule) error { return s.db.Save(r).Error }
func (s *Store) DeleteDigipeaterRule(id uint32) error {
	return s.db.Delete(&DigipeaterRule{}, id).Error
}

// ---------------------------------------------------------------------------
// IGateConfig (singleton) + filters
// ---------------------------------------------------------------------------

func (s *Store) GetIGateConfig() (*IGateConfig, error) {
	var c IGateConfig
	err := s.db.Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertIGateConfig(c *IGateConfig) error {
	if c.ID == 0 {
		existing, err := s.GetIGateConfig()
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	return s.db.Save(c).Error
}

func (s *Store) ListIGateRfFilters() ([]IGateRfFilter, error) {
	var out []IGateRfFilter
	return out, s.db.Order("priority, id").Find(&out).Error
}

func (s *Store) ListIGateRfFiltersForChannel(channel uint32) ([]IGateRfFilter, error) {
	var out []IGateRfFilter
	return out, s.db.Where("channel = ? AND enabled = ?", channel, true).
		Order("priority, id").Find(&out).Error
}

func (s *Store) CreateIGateRfFilter(f *IGateRfFilter) error { return s.db.Create(f).Error }
func (s *Store) UpdateIGateRfFilter(f *IGateRfFilter) error { return s.db.Save(f).Error }
func (s *Store) DeleteIGateRfFilter(id uint32) error {
	return s.db.Delete(&IGateRfFilter{}, id).Error
}

// ---------------------------------------------------------------------------
// Beacon
// ---------------------------------------------------------------------------

func (s *Store) ListBeacons() ([]Beacon, error) {
	var out []Beacon
	return out, s.db.Order("id").Find(&out).Error
}

func (s *Store) GetBeacon(id uint32) (*Beacon, error) {
	var b Beacon
	if err := s.db.First(&b, id).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Store) CreateBeacon(b *Beacon) error { return s.db.Create(b).Error }
func (s *Store) UpdateBeacon(b *Beacon) error { return s.db.Save(b).Error }
func (s *Store) DeleteBeacon(id uint32) error { return s.db.Delete(&Beacon{}, id).Error }

// ---------------------------------------------------------------------------
// GPSConfig (singleton)
// ---------------------------------------------------------------------------

func (s *Store) GetGPSConfig() (*GPSConfig, error) {
	var c GPSConfig
	err := s.db.Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpsertGPSConfig(c *GPSConfig) error {
	if c.ID == 0 {
		existing, err := s.GetGPSConfig()
		if err != nil {
			return err
		}
		if existing != nil {
			c.ID = existing.ID
		}
	}
	return s.db.Save(c).Error
}

// ---------------------------------------------------------------------------
// PacketFilter (stub)
// ---------------------------------------------------------------------------

func (s *Store) ListPacketFilters() ([]PacketFilter, error) {
	var out []PacketFilter
	return out, s.db.Order("id").Find(&out).Error
}
