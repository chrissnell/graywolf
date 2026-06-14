package app

import (
	"testing"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

func TestAudioLevelFromFrame(t *testing.T) {
	tests := []struct {
		name             string
		mark, space      float32
		wantNil          bool
		wantMark, wantSp int
	}{
		{name: "healthy signal scales to ~0-100", mark: 0.65, space: 0.60, wantMark: 65, wantSp: 60},
		{name: "full-scale tone maps near 100", mark: 1.0, space: 0.98, wantMark: 100, wantSp: 98},
		{name: "hot input exceeds 100", mark: 1.07, space: 1.02, wantMark: 107, wantSp: 102},
		{name: "rounds to nearest", mark: 0.504, space: 0.506, wantMark: 50, wantSp: 51},
		{name: "both zero yields nil", mark: 0, space: 0, wantNil: true},
		{name: "negative placeholder clamps, keeps non-nil if other is set", mark: -1.0, space: 0.40, wantMark: 0, wantSp: 40},
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
		})
	}
}
