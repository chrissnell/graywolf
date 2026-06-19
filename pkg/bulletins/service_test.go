package bulletins

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeBulletinTxSink struct {
	mu        sync.Mutex
	submitted []fakeBulletinSubmit
}

type fakeBulletinSubmit struct {
	Channel uint32
	Frame   *ax25.Frame
	Src     txgovernor.SubmitSource
}

func (f *fakeBulletinTxSink) Submit(_ context.Context, ch uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitted = append(f.submitted, fakeBulletinSubmit{Channel: ch, Frame: frame, Src: src})
	return nil
}

func (f *fakeBulletinTxSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.submitted)
}

// ---------------------------------------------------------------------------
// Test rig
// ---------------------------------------------------------------------------

type serviceRig struct {
	svc    *Service
	sink   *fakeBulletinTxSink
	cs     *configstore.Store
	ourCall string
}

func buildServiceRig(t *testing.T) *serviceRig {
	t.Helper()
	cs, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	sink := &fakeBulletinTxSink{}
	ourCall := "W5X-9"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc, err := NewService(ServiceConfig{
		DB:        cs.DB(),
		TxSink:    sink,
		OurCall:   func() string { return ourCall },
		TxChannel: 1,
		Logger:    logger,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return &serviceRig{svc: svc, sink: sink, cs: cs, ourCall: ourCall}
}

// ---------------------------------------------------------------------------
// validSlot / isAnnouncement
// ---------------------------------------------------------------------------

func TestValidSlot(t *testing.T) {
	tests := []struct {
		slot string
		want bool
	}{
		{"BLN0", true},
		{"BLN9", true},
		{"BLNA", true},
		{"BLNZ", true},
		{"BLN", false},
		{"BLNAA", false},
		{"BLN!", false},
		{"bln0", false}, // lower-case not accepted (caller must upper)
		{"", false},
		{"ABCD", false},
	}
	for _, tc := range tests {
		if got := validSlot(tc.slot); got != tc.want {
			t.Errorf("validSlot(%q) = %v, want %v", tc.slot, got, tc.want)
		}
	}
}

func TestIsAnnouncement(t *testing.T) {
	tests := []struct {
		slot string
		want bool
	}{
		{"BLNA", true},
		{"BLNZ", true},
		{"BLN0", false},
		{"BLN9", false},
		{"BLN", false},
	}
	for _, tc := range tests {
		if got := isAnnouncement(tc.slot); got != tc.want {
			t.Errorf("isAnnouncement(%q) = %v, want %v", tc.slot, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Send
// ---------------------------------------------------------------------------

func TestSend_Valid(t *testing.T) {
	rig := buildServiceRig(t)
	ctx := context.Background()

	b, err := rig.svc.Send(ctx, SendRequest{Slot: "BLN0", Text: "test bulletin"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if b == nil || b.ID == 0 {
		t.Fatal("expected non-nil bulletin with ID")
	}
	if b.Slot != "BLN0" {
		t.Errorf("Slot: got %q, want BLN0", b.Slot)
	}
	if b.MaxSends != BulletinMaxSends {
		t.Errorf("MaxSends: got %d, want %d", b.MaxSends, BulletinMaxSends)
	}
}

func TestSend_Announcement(t *testing.T) {
	rig := buildServiceRig(t)
	ctx := context.Background()

	b, err := rig.svc.Send(ctx, SendRequest{Slot: "BLNA", Text: "annual net"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if b.MaxSends != AnnouncementMaxSends {
		t.Errorf("MaxSends: got %d, want %d", b.MaxSends, AnnouncementMaxSends)
	}
	if !b.IsAnnouncement {
		t.Error("expected IsAnnouncement=true")
	}
}

func TestSend_InvalidSlot(t *testing.T) {
	rig := buildServiceRig(t)
	ctx := context.Background()

	if _, err := rig.svc.Send(ctx, SendRequest{Slot: "BLN", Text: "x"}); err == nil {
		t.Error("expected error for invalid slot")
	}
}

func TestSend_EmptyText(t *testing.T) {
	rig := buildServiceRig(t)
	ctx := context.Background()

	if _, err := rig.svc.Send(ctx, SendRequest{Slot: "BLN0", Text: ""}); err == nil {
		t.Error("expected error for empty text")
	}
}

func TestSend_TextTooLong(t *testing.T) {
	rig := buildServiceRig(t)
	ctx := context.Background()

	long := "x"
	for i := 0; i < 68; i++ {
		long += "a"
	}
	if _, err := rig.svc.Send(ctx, SendRequest{Slot: "BLN0", Text: long}); err == nil {
		t.Error("expected error for text > 67 chars")
	}
}

// ---------------------------------------------------------------------------
// IngestBulletin
// ---------------------------------------------------------------------------

func makeBulletinPacket(t *testing.T, from, slot, text string) (*aprs.DecodedAPRSPacket, *aprs.Message) {
	t.Helper()
	// Build info field for a bulletin: ":BLN0     :text"
	pad := slot + "         "
	pad = pad[:9]
	info := ":" + pad + ":" + text

	src, err := ax25.ParseAddress(from)
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	dst, err := ax25.ParseAddress("APGRWO")
	if err != nil {
		t.Fatalf("ParseAddress dst: %v", err)
	}
	f, err := ax25.NewUIFrame(src, dst, nil, []byte(info))
	if err != nil {
		t.Fatalf("NewUIFrame: %v", err)
	}
	pkt, err := aprs.Parse(f)
	if err != nil {
		t.Fatalf("aprs.Parse: %v", err)
	}
	pkt.Direction = aprs.DirectionRF
	return pkt, pkt.Message
}

func TestIngestBulletin_StoresInbound(t *testing.T) {
	rig := buildServiceRig(t)
	ctx := context.Background()

	pkt, msg := makeBulletinPacket(t, "W5X-1", "BLN0", "Net tonight")
	if err := rig.svc.IngestBulletin(ctx, pkt, msg); err != nil {
		t.Fatalf("IngestBulletin: %v", err)
	}

	rows, err := rig.svc.List(ctx, Filter{Direction: "in"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].FromCall != "W5X-1" {
		t.Errorf("FromCall: got %q, want W5X-1", rows[0].FromCall)
	}
	if rows[0].Slot != "BLN0" {
		t.Errorf("Slot: got %q, want BLN0", rows[0].Slot)
	}
	if rows[0].Text != "Net tonight" {
		t.Errorf("Text: got %q, want %q", rows[0].Text, "Net tonight")
	}
	if rows[0].ExpiresAt == nil {
		t.Error("expected ExpiresAt to be set")
	}
}

func TestIngestBulletin_Upserts(t *testing.T) {
	rig := buildServiceRig(t)
	ctx := context.Background()

	pkt1, msg1 := makeBulletinPacket(t, "W5X-1", "BLN0", "first")
	if err := rig.svc.IngestBulletin(ctx, pkt1, msg1); err != nil {
		t.Fatal(err)
	}
	pkt2, msg2 := makeBulletinPacket(t, "W5X-1", "BLN0", "updated")
	if err := rig.svc.IngestBulletin(ctx, pkt2, msg2); err != nil {
		t.Fatal(err)
	}

	rows, err := rig.svc.List(ctx, Filter{Direction: "in"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after upsert, got %d", len(rows))
	}
	if rows[0].Text != "updated" {
		t.Errorf("Text not updated: %q", rows[0].Text)
	}
}

func TestIngestBulletin_AnnouncementExpiry(t *testing.T) {
	rig := buildServiceRig(t)
	ctx := context.Background()

	pkt, msg := makeBulletinPacket(t, "W5X-1", "BLNA", "big event")
	if err := rig.svc.IngestBulletin(ctx, pkt, msg); err != nil {
		t.Fatal(err)
	}

	rows, _ := rig.svc.List(ctx, Filter{Direction: "in"})
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	if rows[0].ExpiresAt == nil {
		t.Fatal("expected ExpiresAt")
	}
	// Announcement expires in ~4 days; confirm it is at least 3 days away.
	minExpiry := time.Now().UTC().Add(3 * 24 * time.Hour)
	if rows[0].ExpiresAt.Before(minExpiry) {
		t.Errorf("announcement expires too soon: %v", rows[0].ExpiresAt)
	}
}

// ---------------------------------------------------------------------------
// Delete / MarkRead / MarkAllRead delegation
// ---------------------------------------------------------------------------

func TestDelete_SoftDeletes(t *testing.T) {
	rig := buildServiceRig(t)
	ctx := context.Background()

	b, err := rig.svc.Send(ctx, SendRequest{Slot: "BLN0", Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.Delete(ctx, b.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	rows, err := rig.svc.List(ctx, Filter{Direction: "out"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows after Delete, got %d", len(rows))
	}
}

func TestMarkRead_ClearsFlag(t *testing.T) {
	rig := buildServiceRig(t)
	ctx := context.Background()

	pkt, msg := makeBulletinPacket(t, "W5X-1", "BLN0", "ping")
	if err := rig.svc.IngestBulletin(ctx, pkt, msg); err != nil {
		t.Fatal(err)
	}
	rows, _ := rig.svc.List(ctx, Filter{Direction: "in"})
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	if err := rig.svc.MarkRead(ctx, rows[0].ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	unread, _ := rig.svc.List(ctx, Filter{Direction: "in", UnreadOnly: true})
	if len(unread) != 0 {
		t.Errorf("expected 0 unread, got %d", len(unread))
	}
}

func TestMarkAllRead(t *testing.T) {
	rig := buildServiceRig(t)
	ctx := context.Background()

	for _, slot := range []string{"BLN0", "BLN1"} {
		pkt, msg := makeBulletinPacket(t, "W5X-1", slot, slot)
		if err := rig.svc.IngestBulletin(ctx, pkt, msg); err != nil {
			t.Fatal(err)
		}
	}
	if err := rig.svc.MarkAllRead(ctx); err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}
	unread, _ := rig.svc.List(ctx, Filter{Direction: "in", UnreadOnly: true})
	if len(unread) != 0 {
		t.Errorf("expected 0 unread, got %d", len(unread))
	}
}
