package modembridge

import (
	"sync"
	"testing"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

func TestStatusCacheUpdateAndRead(t *testing.T) {
	c := newStatusCache()

	c.UpdateStatus(&pb.StatusUpdate{
		Channel:        0,
		RxFrames:       10,
		RxBadFcs:       1,
		TxFrames:       5,
		AudioLevelPeak: 0.7,
		DcdState:       true,
	})
	c.UpdateStatus(&pb.StatusUpdate{
		Channel:  1,
		RxFrames: 20,
	})

	s, ok := c.Channel(0)
	if !ok || s.RxFrames != 10 || !s.DcdState {
		t.Fatalf("ch0 stats: %+v ok=%v", s, ok)
	}
	if _, ok := c.Channel(99); ok {
		t.Error("expected miss on non-existent channel")
	}

	all := c.AllChannels()
	if len(all) != 2 {
		t.Errorf("AllChannels len = %d, want 2", len(all))
	}

	// Mutating the snapshot must not affect the cache.
	s.RxFrames = 999
	s2, _ := c.Channel(0)
	if s2.RxFrames != 10 {
		t.Errorf("snapshot mutation leaked into cache: RxFrames=%d", s2.RxFrames)
	}
}

func TestStatusCacheDeviceLevels(t *testing.T) {
	c := newStatusCache()

	c.UpdateDeviceLevel(&pb.DeviceLevelUpdate{
		DeviceId: 1,
		PeakDbfs: -3,
		RmsDbfs:  -10,
		Clipping: false,
	})
	all := c.AllDeviceLevels()
	if len(all) != 1 || all[1].PeakDBFS != -3 {
		t.Errorf("device level cache: %+v", all)
	}
}

func TestStatusCacheResetDropsStaleEntries(t *testing.T) {
	c := newStatusCache()
	c.UpdateStatus(&pb.StatusUpdate{Channel: 0, RxFrames: 5})
	c.UpdateDeviceLevel(&pb.DeviceLevelUpdate{DeviceId: 1, PeakDbfs: -1})

	c.Reset()

	if _, ok := c.Channel(0); ok {
		t.Error("Channel(0) still present after Reset")
	}
	if n := len(c.AllDeviceLevels()); n != 0 {
		t.Errorf("AllDeviceLevels len = %d after Reset, want 0", n)
	}
}

func TestStatusCacheConcurrentUpdateAndRead(t *testing.T) {
	c := newStatusCache()
	var wg sync.WaitGroup
	const n = 100
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			c.UpdateStatus(&pb.StatusUpdate{Channel: uint32(i % 4), RxFrames: uint64(i)})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			_ = c.AllChannels()
		}
	}()
	wg.Wait()
}
