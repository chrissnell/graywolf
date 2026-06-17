package app

import (
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/packetlog"
)

func TestAudioLevelFromFrame(t *testing.T) {
	tests := []struct {
		name             string
		mark, space      float32
		wantNil          bool
		wantMark, wantSp int
		wantMarkDB       float64
		wantSpaceDB      float64
		wantLevelDB      float64
	}{
		{name: "healthy signal scales to ~0-100", mark: 0.65, space: 0.60, wantMark: 65, wantSp: 60, wantMarkDB: -3.7, wantSpaceDB: -4.4, wantLevelDB: -4.1},
		{name: "full-scale tone maps to 0 dBFS", mark: 1.0, space: 0.98, wantMark: 100, wantSp: 98, wantMarkDB: 0.0, wantSpaceDB: -0.2, wantLevelDB: -0.1},
		{name: "half-scale tone is -6 dBFS", mark: 0.5, space: 0.5, wantMark: 50, wantSp: 50, wantMarkDB: -6.0, wantSpaceDB: -6.0, wantLevelDB: -6.0},
		{name: "tenth-scale tone is -20 dBFS", mark: 0.1, space: 0.1, wantMark: 10, wantSp: 10, wantMarkDB: -20.0, wantSpaceDB: -20.0, wantLevelDB: -20.0},
		{name: "quiet healthy signal is ~-26 dBFS", mark: 0.05, space: 0.05, wantMark: 5, wantSp: 5, wantMarkDB: -26.0, wantSpaceDB: -26.0, wantLevelDB: -26.0},
		{name: "very weak tone floors at -60 dBFS", mark: 0.0005, space: 0.0005, wantMark: 0, wantSp: 0, wantMarkDB: -60.0, wantSpaceDB: -60.0, wantLevelDB: -60.0},
		{name: "both zero yields nil", mark: 0, space: 0, wantNil: true},
		{name: "negative placeholder clamps to -60 dBFS, keeps non-nil if other is set", mark: -1.0, space: 0.40, wantMark: 0, wantSp: 40, wantMarkDB: -60.0, wantSpaceDB: -8.0, wantLevelDB: -14.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := audioLevelFromFrame(&pb.ReceivedFrame{
				AudioLevelMark:  tt.mark,
				AudioLevelSpace: tt.space,
			})
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil AudioLevel")
			}
			if got.Mark != tt.wantMark || got.Space != tt.wantSp {
				t.Errorf("mark/space = %d/%d, want %d/%d", got.Mark, got.Space, tt.wantMark, tt.wantSp)
			}
			if got.MarkDBFS != tt.wantMarkDB || got.SpaceDBFS != tt.wantSpaceDB || got.LevelDBFS != tt.wantLevelDB {
				t.Errorf("mark/space/level dBFS = %.1f/%.1f/%.1f, want %.1f/%.1f/%.1f",
					got.MarkDBFS, got.SpaceDBFS, got.LevelDBFS, tt.wantMarkDB, tt.wantSpaceDB, tt.wantLevelDB)
			}
		})
	}
}

// TestDispatchRxFrameAudioLevelGating proves the source gating end-to-end:
// a modem-RX frame lands in the packet log with its mark/space level
// attached, while a hardware KISS-TNC frame (already demodulated, no
// soundcard level) records a nil AudioLevel even when the proto fields
// happen to be populated. This is the part most likely to regress — the
// scaling math itself is covered by TestAudioLevelFromFrame above.
func TestDispatchRxFrameAudioLevelGating(t *testing.T) {
	h := newKissTncHarness(t)
	defer h.stop()

	modemFrame := buildUIFrame(t, "MODEM-1", ">from-modem", nil)
	modemBytes, _ := modemFrame.Encode()
	tncFrame := buildUIFrame(t, "TNC-1", ">from-tnc", nil)
	tncBytes, _ := tncFrame.Encode()

	h.app.rxFanout <- rxFanoutItem{
		rf:  &pb.ReceivedFrame{Channel: 1, Data: modemBytes, AudioLevelMark: 0.65, AudioLevelSpace: 0.60},
		src: ingress.Modem(),
	}
	h.app.rxFanout <- rxFanoutItem{
		rf:  &pb.ReceivedFrame{Channel: 1, Data: tncBytes, AudioLevelMark: 0.65, AudioLevelSpace: 0.60},
		src: ingress.KissTnc(50),
	}
	h.waitDispatched(2, 2*time.Second)

	bySource := map[string]packetlog.Entry{}
	for _, e := range h.app.plog.Query(packetlog.Filter{Channel: -1}) {
		bySource[e.Source] = e
	}

	modem, ok := bySource["modem"]
	if !ok {
		t.Fatal("no modem-source entry recorded")
	}
	if modem.AudioLevel == nil {
		t.Fatal("modem entry: AudioLevel is nil, want mark/space attached")
	}
	if modem.AudioLevel.Mark != 65 || modem.AudioLevel.Space != 60 {
		t.Errorf("modem AudioLevel = %d/%d, want 65/60", modem.AudioLevel.Mark, modem.AudioLevel.Space)
	}
	if modem.AudioLevel.LevelDBFS != -4.1 {
		t.Errorf("modem AudioLevel.LevelDBFS = %.1f, want -4.1", modem.AudioLevel.LevelDBFS)
	}

	tnc, ok := bySource["kiss-tnc"]
	if !ok {
		t.Fatal("no kiss-tnc-source entry recorded")
	}
	if tnc.AudioLevel != nil {
		t.Errorf("kiss-tnc entry: AudioLevel = %+v, want nil (no soundcard level)", tnc.AudioLevel)
	}
}
