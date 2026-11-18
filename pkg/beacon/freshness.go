package beacon

import (
	"fmt"
	"github.com/chrissnell/graywolf/pkg/gps"
	"time"
)

// DefaultGPSMaxAge applies only to trackers whose operator opts in.
const DefaultGPSMaxAge = 30 * time.Second

func trackerFreshness(b Config, fix gps.Fix, now time.Time) error {
	if b.Type != TypeTracker || !b.RequireFreshGPS {
		return nil
	}
	ageLimit := b.GPSMaxAge
	if ageLimit <= 0 {
		ageLimit = DefaultGPSMaxAge
	}
	if fix.ReceivedAt.IsZero() {
		return fmt.Errorf("tracker beacon suppressed: GPS freshness unavailable")
	}
	age := now.Sub(fix.ReceivedAt)
	if age < 0 || age > ageLimit {
		return fmt.Errorf("tracker beacon suppressed: GPS position is stale (maximum age %s)", ageLimit)
	}
	return nil
}
