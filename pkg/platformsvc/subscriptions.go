//go:build android

package platformsvc

import "context"

func (c *clientImpl) SubscribeGpsFix(_ context.Context, ch chan<- *GpsFix) error {
	if c.closed.Load() {
		return ErrClosed
	}
	c.subsMu.Lock()
	c.gpsFixSubs = append(c.gpsFixSubs, ch)
	c.subsMu.Unlock()
	return nil
}

func (c *clientImpl) SubscribeAudioRouteChanged(_ context.Context, ch chan<- *AudioRouteChanged) error {
	if c.closed.Load() {
		return ErrClosed
	}
	c.subsMu.Lock()
	c.audioRouteSubs = append(c.audioRouteSubs, ch)
	c.subsMu.Unlock()
	return nil
}
