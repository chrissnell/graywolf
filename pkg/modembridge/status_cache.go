package modembridge

import (
	"sync"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// statusCache holds the two per-session caches the bridge populates from
// modem-side IPC messages: per-channel stats (from StatusUpdate) and
// per-device audio levels (from DeviceLevelUpdate). Safe for concurrent
// use.
//
// Behavior change vs. the pre-refactor Bridge: Reset is called at the top
// of every supervise iteration, so stale stats and levels from a crashed
// modem instance do not survive across the restart. Previously the
// caches persisted for the bridge's lifetime, which meant a frontend
// polling GetChannelStats() would see ghost counters from a dead child.
type statusCache struct {
	statsMu sync.RWMutex
	stats   map[uint32]*ChannelStats

	levelsMu sync.RWMutex
	levels   map[uint32]*DeviceLevel
}

func newStatusCache() *statusCache {
	return &statusCache{
		stats:  make(map[uint32]*ChannelStats),
		levels: make(map[uint32]*DeviceLevel),
	}
}

// UpdateStatus folds a Rust-side StatusUpdate into the per-channel cache.
func (c *statusCache) UpdateStatus(s *pb.StatusUpdate) {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	c.stats[s.Channel] = &ChannelStats{
		Channel:         s.Channel,
		RxFrames:        s.RxFrames,
		RxBadFCS:        s.RxBadFcs,
		TxFrames:        s.TxFrames,
		DcdTransitions:  s.DcdTransitions,
		AudioLevelMark:  s.AudioLevelMark,
		AudioLevelSpace: s.AudioLevelSpace,
		AudioLevelPeak:  s.AudioLevelPeak,
		DcdState:        s.DcdState,
	}
}

// Channel returns a snapshot of the cached stats for a single channel.
func (c *statusCache) Channel(channel uint32) (*ChannelStats, bool) {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()
	s, ok := c.stats[channel]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

// AllChannels returns snapshots of every cached channel's stats.
func (c *statusCache) AllChannels() map[uint32]*ChannelStats {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()
	out := make(map[uint32]*ChannelStats, len(c.stats))
	for k, v := range c.stats {
		cp := *v
		out[k] = &cp
	}
	return out
}

// InjectStatsForTest installs synthetic channel stats, bypassing the
// full StatusUpdate deserialisation path. Test-only; the Bridge-level
// wrapper is exposed as Bridge.InjectStatusForTest.
func (c *statusCache) InjectStatsForTest(s *ChannelStats) {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	cp := *s
	c.stats[s.Channel] = &cp
}

// UpdateDeviceLevel folds a DeviceLevelUpdate into the per-device cache.
func (c *statusCache) UpdateDeviceLevel(u *pb.DeviceLevelUpdate) {
	c.levelsMu.Lock()
	defer c.levelsMu.Unlock()
	c.levels[u.DeviceId] = &DeviceLevel{
		DeviceID: u.DeviceId,
		PeakDBFS: u.PeakDbfs,
		RmsDBFS:  u.RmsDbfs,
		Clipping: u.Clipping,
	}
}

// AllDeviceLevels returns snapshots of every cached device's audio levels.
func (c *statusCache) AllDeviceLevels() map[uint32]*DeviceLevel {
	c.levelsMu.RLock()
	defer c.levelsMu.RUnlock()
	out := make(map[uint32]*DeviceLevel, len(c.levels))
	for k, v := range c.levels {
		cp := *v
		out[k] = &cp
	}
	return out
}

// Reset discards both the per-channel and per-device caches. The bridge
// calls this at the top of every supervise iteration so a restarted
// modem child starts with an empty view.
func (c *statusCache) Reset() {
	c.statsMu.Lock()
	c.stats = make(map[uint32]*ChannelStats)
	c.statsMu.Unlock()

	c.levelsMu.Lock()
	c.levels = make(map[uint32]*DeviceLevel)
	c.levelsMu.Unlock()
}
