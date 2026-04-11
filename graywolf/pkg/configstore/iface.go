package configstore

import "context"

// ConfigStore defines the persistence contract for graywolf configuration.
// The concrete *Store satisfies this interface; consumers should depend on
// ConfigStore to enable testing with fakes.
type ConfigStore interface {
	// Audio devices
	CreateAudioDevice(ctx context.Context, d *AudioDevice) error
	GetAudioDevice(ctx context.Context, id uint32) (*AudioDevice, error)
	ListAudioDevices(ctx context.Context) ([]AudioDevice, error)
	UpdateAudioDevice(ctx context.Context, d *AudioDevice) error
	DeleteAudioDevice(ctx context.Context, id uint32) error

	// Channels
	CreateChannel(ctx context.Context, c *Channel) error
	GetChannel(ctx context.Context, id uint32) (*Channel, error)
	ListChannels(ctx context.Context) ([]Channel, error)
	UpdateChannel(ctx context.Context, c *Channel) error
	DeleteChannel(ctx context.Context, id uint32) error
	SetChannelFX25(ctx context.Context, id uint32, enable bool) error
	SetChannelIL2P(ctx context.Context, id uint32, enable bool) error

	// PTT
	UpsertPttConfig(ctx context.Context, p *PttConfig) error
	GetPttConfigForChannel(ctx context.Context, channelID uint32) (*PttConfig, error)

	// TX timing
	ListTxTimings(ctx context.Context) ([]TxTiming, error)
	GetTxTiming(ctx context.Context, channel uint32) (*TxTiming, error)
	UpsertTxTiming(ctx context.Context, t *TxTiming) error

	// KISS interfaces
	ListKissInterfaces(ctx context.Context) ([]KissInterface, error)
	GetKissInterface(ctx context.Context, id uint32) (*KissInterface, error)
	CreateKissInterface(ctx context.Context, k *KissInterface) error
	UpdateKissInterface(ctx context.Context, k *KissInterface) error
	DeleteKissInterface(ctx context.Context, id uint32) error

	// AGW
	GetAgwConfig(ctx context.Context) (*AgwConfig, error)
	UpsertAgwConfig(ctx context.Context, c *AgwConfig) error

	// Digipeater
	GetDigipeaterConfig(ctx context.Context) (*DigipeaterConfig, error)
	UpsertDigipeaterConfig(ctx context.Context, c *DigipeaterConfig) error
	ListDigipeaterRules(ctx context.Context) ([]DigipeaterRule, error)
	ListDigipeaterRulesForChannel(ctx context.Context, channel uint32) ([]DigipeaterRule, error)
	CreateDigipeaterRule(ctx context.Context, r *DigipeaterRule) error
	UpdateDigipeaterRule(ctx context.Context, r *DigipeaterRule) error
	DeleteDigipeaterRule(ctx context.Context, id uint32) error

	// iGate
	GetIGateConfig(ctx context.Context) (*IGateConfig, error)
	UpsertIGateConfig(ctx context.Context, c *IGateConfig) error
	ListIGateRfFilters(ctx context.Context) ([]IGateRfFilter, error)
	ListIGateRfFiltersForChannel(ctx context.Context, channel uint32) ([]IGateRfFilter, error)
	CreateIGateRfFilter(ctx context.Context, f *IGateRfFilter) error
	UpdateIGateRfFilter(ctx context.Context, f *IGateRfFilter) error
	DeleteIGateRfFilter(ctx context.Context, id uint32) error

	// Beacons
	ListBeacons(ctx context.Context) ([]Beacon, error)
	GetBeacon(ctx context.Context, id uint32) (*Beacon, error)
	CreateBeacon(ctx context.Context, b *Beacon) error
	UpdateBeacon(ctx context.Context, b *Beacon) error
	DeleteBeacon(ctx context.Context, id uint32) error

	// GPS
	GetGPSConfig(ctx context.Context) (*GPSConfig, error)
	UpsertGPSConfig(ctx context.Context, c *GPSConfig) error

	// Packet filters
	ListPacketFilters(ctx context.Context) ([]PacketFilter, error)
}

// Compile-time check: *Store implements ConfigStore.
var _ ConfigStore = (*Store)(nil)
