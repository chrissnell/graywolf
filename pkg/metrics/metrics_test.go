package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}

func TestHandlerExposesMetrics(t *testing.T) {
	m := New()
	m.SetChildUp(true)
	m.ObserveReceivedFrame(0)
	m.ObserveReceivedFrame(0)
	m.ChildRestarts.Inc()

	body := scrape(t, m)
	for _, want := range []string{
		`graywolf_rx_frames_total{channel="0"} 2`,
		`graywolf_child_up 1`,
		`graywolf_child_restarts_total 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q\n---\n%s", want, body)
		}
	}
}

func TestUpdateFromStatus(t *testing.T) {
	m := New()

	// First update establishes the baseline; no counter delta yet.
	m.UpdateFromStatus(&pb.StatusUpdate{
		Channel:        0,
		DcdTransitions: 5,
		AudioLevelPeak: 0.4,
		DcdState:       true,
	})
	// Second update: +3 DCD transitions.
	m.UpdateFromStatus(&pb.StatusUpdate{
		Channel:        0,
		DcdTransitions: 8,
		AudioLevelPeak: 0.2,
		DcdState:       false,
	})

	body := scrape(t, m)
	for _, want := range []string{
		`graywolf_dcd_transitions_total{channel="0"} 3`,
		`graywolf_dcd_active{channel="0"} 0`,
		`graywolf_audio_level{channel="0"} 0.2`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestUpdateFromStatusHandlesRestart(t *testing.T) {
	m := New()
	m.UpdateFromStatus(&pb.StatusUpdate{Channel: 0, DcdTransitions: 100})
	// Child restarted, counter reset to 2. Should rebaseline, not go negative.
	m.UpdateFromStatus(&pb.StatusUpdate{Channel: 0, DcdTransitions: 2})
	m.UpdateFromStatus(&pb.StatusUpdate{Channel: 0, DcdTransitions: 5})

	body := scrape(t, m)
	// 0 from first, rebaseline to 2, then +3 → total 3.
	if !strings.Contains(body, `graywolf_dcd_transitions_total{channel="0"} 3`) {
		t.Errorf("unexpected transitions counter after restart:\n%s", body)
	}
}
