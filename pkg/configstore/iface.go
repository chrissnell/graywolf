package configstore

// ConfigStore defines the persistence contract for graywolf configuration.
// The concrete *Store satisfies this interface; consumers should depend on
// ConfigStore to enable testing with fakes.
type ConfigStore interface {
	// Audio devices
	CreateAudioDevice(d *AudioDevice) error
	GetAudioDevice(id uint32) (*AudioDevice, error)
	ListAudioDevices() ([]AudioDevice, error)
	UpdateAudioDevice(d *AudioDevice) error
	DeleteAudioDevice(id uint32) error

	// Channels
	CreateChannel(c *Channel) error
	GetChannel(id uint32) (*Channel, error)
	ListChannels() ([]Channel, error)
	UpdateChannel(c *Channel) error
	DeleteChannel(id uint32) error
	SetChannelFX25(id uint32, enable bool) error
	SetChannelIL2P(id uint32, enable bool) error

	// PTT
	UpsertPttConfig(p *PttConfig) error
	GetPttConfigForChannel(channelID uint32) (*PttConfig, error)

	// TX timing
	ListTxTimings() ([]TxTiming, error)
	GetTxTiming(channel uint32) (*TxTiming, error)
	UpsertTxTiming(t *TxTiming) error

	// KISS interfaces
	ListKissInterfaces() ([]KissInterface, error)
	GetKissInterface(id uint32) (*KissInterface, error)
	CreateKissInterface(k *KissInterface) error
	UpdateKissInterface(k *KissInterface) error
	DeleteKissInterface(id uint32) error

	// AGW
	GetAgwConfig() (*AgwConfig, error)
	UpsertAgwConfig(c *AgwConfig) error

	// Digipeater
	GetDigipeaterConfig() (*DigipeaterConfig, error)
	UpsertDigipeaterConfig(c *DigipeaterConfig) error
	ListDigipeaterRules() ([]DigipeaterRule, error)
	ListDigipeaterRulesForChannel(channel uint32) ([]DigipeaterRule, error)
	CreateDigipeaterRule(r *DigipeaterRule) error
	UpdateDigipeaterRule(r *DigipeaterRule) error
	DeleteDigipeaterRule(id uint32) error

	// iGate
	GetIGateConfig() (*IGateConfig, error)
	UpsertIGateConfig(c *IGateConfig) error
	ListIGateRfFilters() ([]IGateRfFilter, error)
	ListIGateRfFiltersForChannel(channel uint32) ([]IGateRfFilter, error)
	CreateIGateRfFilter(f *IGateRfFilter) error
	UpdateIGateRfFilter(f *IGateRfFilter) error
	DeleteIGateRfFilter(id uint32) error

	// Beacons
	ListBeacons() ([]Beacon, error)
	GetBeacon(id uint32) (*Beacon, error)
	CreateBeacon(b *Beacon) error
	UpdateBeacon(b *Beacon) error
	DeleteBeacon(id uint32) error

	// GPS
	GetGPSConfig() (*GPSConfig, error)
	UpsertGPSConfig(c *GPSConfig) error

	// Packet filters
	ListPacketFilters() ([]PacketFilter, error)
}

// Compile-time check: *Store implements ConfigStore.
var _ ConfigStore = (*Store)(nil)
