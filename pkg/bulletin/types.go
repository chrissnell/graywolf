// Package bulletin implements the graywolf bulletin scheduler: APRS
// bulletins (addressed to BLN0–BLN9 for global, BLN0GROUP–BLN9GROUP for
// named groups) transmitted on a decaying-interval schedule modelled after
// the APRS101 §14.3 retransmit contract. All outgoing frames are submitted
// through txgovernor or sent directly to an APRS-IS sink, matching the same
// dual-leg pattern used by the beacon scheduler.
package bulletin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// SendPath constants mirror the beacon send_path enum.
const (
	SendPathRF     = "rf"
	SendPathBoth   = "both"
	SendPathISOnly = "is_only"

	// MinInitialRate is the smallest permitted initial_rate. APRS etiquette
	// discourages bulletins faster than once every 30 seconds.
	MinInitialRate = 30 * time.Second

	// aprsDestination is the standard Graywolf destination address.
	aprsDestination = "APGRWO"
)

// TxSink abstracts txgovernor.TxSink for the bulletin scheduler.
type TxSink interface {
	Submit(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error
}

// ISSink abstracts the APRS-IS line sender (igate outbound path).
type ISSink interface {
	SendLine(line string) error
}

// Clock abstracts time.Now for deterministic tests.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// GroupConfig is the scheduler's view of one bulletin group. Loaded from
// configstore on each Reload.
type GroupConfig struct {
	ID          uint32
	Name        string // "" = Global
	Channel     uint32 // TX channel for RF leg; 0 = auto / no RF
	SendPath    string
	DigiPath    string
	InitialRate time.Duration
	DecayFactor float64
	StableRate  time.Duration
	Items       []ItemConfig
}

// ItemConfig is one active slot within a GroupConfig.
type ItemConfig struct {
	GroupID   uint32
	Slot      int
	Text      string
	SendCount int // used to compute starting interval on reload
}

// bulletinAddressee returns the 9-char APRS addressee for a given slot
// and group name. Global (Name="") produces "BLN0     " etc.
// Named groups produce "BLN0WX   " etc. (group name is up to 5 chars).
func bulletinAddressee(slot int, groupName string) string {
	raw := fmt.Sprintf("BLN%d%s", slot, strings.ToUpper(groupName))
	// Pad or truncate to exactly 9 chars.
	if len(raw) < 9 {
		raw += strings.Repeat(" ", 9-len(raw))
	} else if len(raw) > 9 {
		raw = raw[:9]
	}
	return raw
}

// buildInfoField returns the APRS info field for a bulletin.
// Format: ":ADDRESSEE:text"  (no line number — bulletins are never ACKed)
func buildInfoField(slot int, groupName, text string) string {
	return ":" + bulletinAddressee(slot, groupName) + ":" + text
}
