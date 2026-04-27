package diagcollect

import "testing"

func TestProbeGPSDSocket_AbsentReturnsIssue(t *testing.T) {
	cand, issue := probeGPSDSocketAt("/no/such/path/gpsd.sock")
	if cand != nil {
		t.Fatalf("got candidate, want nil: %+v", cand)
	}
	if issue == nil || issue.Kind != "gpsd_unavailable" {
		t.Fatalf("issue = %+v, want gpsd_unavailable", issue)
	}
}

func TestProbeGPSDSocket_ListeningReturnsCandidate(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/gpsd.sock"
	stop := startUnixListener(t, path)
	defer stop()

	cand, issue := probeGPSDSocketAt(path)
	if issue != nil {
		t.Fatalf("issue = %+v, want nil", issue)
	}
	if cand == nil {
		t.Fatal("cand = nil, want gpsd_socket")
	}
	if cand.Kind != "gpsd_socket" || cand.Path != path || !cand.Reachable {
		t.Fatalf("unexpected: %+v", cand)
	}
}

// startUnixListener stands up a minimal AF_UNIX listener on path so
// the probe sees a reachable socket. Returns a stop func.
func startUnixListener(t *testing.T, path string) func() {
	t.Helper()
	ln, err := unixListen(path)
	if err != nil {
		t.Skipf("unix sockets not available on this host: %v", err)
	}
	return func() { _ = ln.Close() }
}
