package weather

import "context"

// Source produces one hardware-independent weather observation. Beacon
// scheduling and APRS encoding depend on this interface, not on a vendor.
type Source interface {
	Read(context.Context) (Observation, error)
}

type WxNowSource struct {
	Path string
}

var _ Source = WxNowSource{}

func (s WxNowSource) Read(context.Context) (Observation, error) {
	return ReadWxNow(s.Path)
}
