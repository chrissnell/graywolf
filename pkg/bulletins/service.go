package bulletins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"gorm.io/gorm"
)


// IGateSender is the narrow IS-uplink interface consumed by the bulletin
// sender. *app.liveIGateLineSender satisfies this. Nil when the operator
// runs without an iGate; sending still works via RF only.
type IGateSender interface {
	SendLine(line string) error
}

// ServiceConfig holds the Service dependencies.
type ServiceConfig struct {
	DB           *gorm.DB
	TxSink       txgovernor.TxSink
	IGateSender  IGateSender // may be nil; bulletins still send via RF
	OurCall      func() string
	TxChannel    uint32
	Path         string // digipeater path, e.g. "WIDE1-1,WIDE2-1"
	Logger       *slog.Logger
}

// Service is the bulletin subsystem: ingest, compose, schedule, and
// query bulletins.
type Service struct {
	store     *Store
	sender    *Sender
	scheduler *Scheduler
	logger    *slog.Logger

	startOnce sync.Once
	stopOnce  sync.Once
}

// NewService constructs the Service and its collaborators. Call Start
// to begin the scheduler loop.
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.DB == nil {
		return nil, errors.New("bulletins: Service requires DB")
	}
	if cfg.TxSink == nil {
		return nil, errors.New("bulletins: Service requires TxSink")
	}
	if cfg.OurCall == nil {
		return nil, errors.New("bulletins: Service requires OurCall")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	store := NewStore(cfg.DB)
	sender := NewSender(cfg.TxSink, cfg.IGateSender, cfg.OurCall, cfg.Path, logger)
	scheduler := NewScheduler(store, sender, cfg.TxChannel, logger)
	return &Service{
		store:     store,
		sender:    sender,
		scheduler: scheduler,
		logger:    logger,
	}, nil
}

// Start begins the scheduler retransmit loop. Idempotent.
func (s *Service) Start(ctx context.Context) {
	s.startOnce.Do(func() { s.scheduler.Start(ctx) })
}

// Stop shuts down the scheduler. Idempotent.
func (s *Service) Stop() {
	s.stopOnce.Do(func() { s.scheduler.Stop() })
}

// Store returns the underlying bulletin store (for webapi layer access).
func (s *Service) Store() *Store { return s.store }

// IngestBulletin is the BulletinSink interface called by the message
// router when it receives an inbound BLN* packet.
func (s *Service) IngestBulletin(ctx context.Context, pkt *aprs.DecodedAPRSPacket, msg *aprs.Message) error {
	slot := strings.ToUpper(strings.TrimSpace(msg.Addressee))
	if slot == "" {
		return nil
	}
	now := time.Now().UTC()
	// Bulletins (BLN0-9) expire after 4 hours; announcements (BLNA-Z)
	// after 4 days, matching their retransmit lifecycles.
	isAnn := isAnnouncement(slot)
	var expires time.Time
	if isAnn {
		expires = now.Add(4 * 24 * time.Hour)
	} else {
		expires = now.Add(4 * time.Hour)
	}
	b := &configstore.Bulletin{
		Slot:           slot,
		FromCall:       pkt.Source,
		Text:           msg.Text,
		Source:         string(pkt.Direction),
		Channel:        uint32(pkt.Channel),
		RawTNC2:        string(pkt.Raw),
		IsAnnouncement: isAnn,
		ExpiresAt:      &expires,
	}
	return s.store.UpsertInbound(ctx, b)
}

// SendRequest is the payload for creating an outbound bulletin.
type SendRequest struct {
	Slot string // "BLN0".."BLNZ"
	Text string
}

// Validate returns an error if req is not a valid outbound bulletin request.
func (r SendRequest) Validate() error {
	slot := strings.ToUpper(strings.TrimSpace(r.Slot))
	if !validSlot(slot) {
		return fmt.Errorf("bulletins: slot %q is not a valid BLN0-9 or BLNA-Z identifier", r.Slot)
	}
	text := strings.TrimSpace(r.Text)
	if text == "" {
		return errors.New("bulletins: text is required")
	}
	if len(text) > 67 {
		return fmt.Errorf("bulletins: text too long (%d chars, max 67)", len(text))
	}
	return nil
}

// Send creates an outbound bulletin row, sends it immediately, and
// enrolls it in the retransmit scheduler. The bulletin will be
// re-sent at the APRS-spec interval until max_sends is reached.
func (s *Service) Send(ctx context.Context, req SendRequest) (*configstore.Bulletin, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	slot := strings.ToUpper(strings.TrimSpace(req.Slot))
	isAnn := isAnnouncement(slot)
	maxSends := uint32(BulletinMaxSends)
	if isAnn {
		maxSends = AnnouncementMaxSends
	}
	now := time.Now().UTC()
	b := &configstore.Bulletin{
		Slot:           slot,
		Text:           strings.TrimSpace(req.Text),
		IsAnnouncement: isAnn,
		MaxSends:       maxSends,
		NextSendAt:     &now, // schedule for immediate send
	}
	if err := s.store.Insert(ctx, b); err != nil {
		return nil, fmt.Errorf("bulletins: insert: %w", err)
	}
	// Kick scheduler so the first send fires without waiting a minute.
	s.scheduler.Kick()
	return b, nil
}

// List returns bulletins matching the filter.
func (s *Service) List(ctx context.Context, f Filter) ([]configstore.Bulletin, error) {
	return s.store.List(ctx, f)
}

// Delete soft-deletes the bulletin, stopping future retransmits.
func (s *Service) Delete(ctx context.Context, id uint64) error {
	return s.store.SoftDelete(ctx, id)
}

// MarkRead marks the bulletin as read.
func (s *Service) MarkRead(ctx context.Context, id uint64) error {
	return s.store.MarkRead(ctx, id)
}

// MarkAllRead clears unread on all inbound bulletins.
func (s *Service) MarkAllRead(ctx context.Context) error {
	return s.store.MarkAllRead(ctx)
}

// SetTxChannel updates the channel used for outbound bulletin sends.
func (s *Service) SetTxChannel(ch uint32) {
	s.scheduler.txChannel = ch
}

// validSlot reports whether slot is a legal APRS bulletin or
// announcement identifier: BLN0..BLN9 or BLNA..BLNZ.
func validSlot(slot string) bool {
	if len(slot) != 4 {
		return false
	}
	if slot[:3] != "BLN" {
		return false
	}
	c := rune(slot[3])
	return (c >= '0' && c <= '9') || (unicode.IsLetter(c) && unicode.IsUpper(c))
}

// isAnnouncement returns true for BLNA-Z slots.
func isAnnouncement(slot string) bool {
	if len(slot) != 4 {
		return false
	}
	c := rune(slot[3])
	return unicode.IsLetter(c) && unicode.IsUpper(c)
}
