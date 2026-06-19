package bulletins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/igate"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

const submitKindBulletins = "bulletins"

// Sender builds and submits APRS bulletin packets via the TX governor
// and, when an IGateSender is wired, also to APRS-IS directly.
type Sender struct {
	txSink  txgovernor.TxSink
	igSink  IGateSender // may be nil; RF-only when absent
	ourCall func() string
	path    string // default digipeater path, e.g. "WIDE1-1,WIDE2-1"
	logger  *slog.Logger
}

// NewSender returns a Sender that submits on txSink using ourCall() as
// the source callsign. igSink may be nil (RF-only). path is a
// comma-separated digipeater path string.
func NewSender(txSink txgovernor.TxSink, igSink IGateSender, ourCall func() string, path string, logger *slog.Logger) *Sender {
	if logger == nil {
		logger = slog.Default()
	}
	return &Sender{txSink: txSink, igSink: igSink, ourCall: ourCall, path: path, logger: logger}
}

// Send formats b as an APRS bulletin UI frame and submits it via the
// TX governor (RF) and, if an IGateSender is wired, also to APRS-IS.
// Returns nil on acceptance by the RF governor queue; IS errors are
// logged but do not fail the RF send.
func (s *Sender) Send(ctx context.Context, b *configstore.Bulletin, txChannel uint32) error {
	info, err := aprs.EncodeMessage(b.Slot, b.Text, "")
	if err != nil {
		return fmt.Errorf("bulletins: encode: %w", err)
	}
	src, err := ax25.ParseAddress(s.ourCall())
	if err != nil {
		return fmt.Errorf("bulletins: source %q: %w", s.ourCall(), err)
	}
	dest, err := ax25.ParseAddress("APGRWO")
	if err != nil {
		return fmt.Errorf("bulletins: dest: %w", err)
	}
	path, err := parsePath(s.path)
	if err != nil {
		return fmt.Errorf("bulletins: path: %w", err)
	}
	frame, err := ax25.NewUIFrame(src, dest, path, info)
	if err != nil {
		return fmt.Errorf("bulletins: frame: %w", err)
	}

	// RF path via TX governor.
	if err := s.txSink.Submit(ctx, txChannel, frame, txgovernor.SubmitSource{
		Kind:     submitKindBulletins,
		Priority: txgovernor.PriorityBeacon,
	}); err != nil {
		return err
	}

	// IS path — send directly to APRS-IS so the bulletin appears on
	// aprs.fi regardless of whether a nearby iGate hears our RF.
	// ErrNotEnabled means the operator has no iGate configured; that
	// is expected and not logged. Any other error is a live IS failure.
	if s.igSink != nil {
		line := aprs.FormatTNC2(s.ourCall(), "APGRWO", []string{"TCPIP*"}, info)
		if err := s.igSink.SendLine(line); err != nil && !errors.Is(err, igate.ErrNotEnabled) {
			s.logger.Warn("bulletins: IS send failed", "slot", b.Slot, "err", err)
		}
	}

	return nil
}

func parsePath(p string) ([]ax25.Address, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return nil, nil
	}
	parts := strings.Split(p, ",")
	out := make([]ax25.Address, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		addr, err := ax25.ParseAddress(part)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", part, err)
		}
		out = append(out, addr)
	}
	return out, nil
}
