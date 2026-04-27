package diagcollect

import (
	"net"
	"os"
	"time"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

const defaultGPSDSocket = "/var/run/gpsd.sock"

// CollectGPS performs a quick gpsd socket reachability probe. The
// serial-port scan promised in the design doc is deferred — operators
// running gpsd is the dominant deployment, and a per-port permission
// walk would duplicate the PTT collector.
func CollectGPS() flareschema.GPSSection {
	out := flareschema.GPSSection{
		Candidates: make([]flareschema.GPSCandidate, 0),
	}
	if cand, issue := probeGPSDSocketAt(defaultGPSDSocket); cand != nil {
		out.Candidates = append(out.Candidates, *cand)
	} else if issue != nil {
		out.Issues = append(out.Issues, *issue)
	}
	return out
}

// probeGPSDSocketAt attempts an AF_UNIX dial against the candidate
// path. Anything other than "the socket exists and accepts a
// connection" yields a gpsd_unavailable issue and a nil candidate.
//
// The dial timeout is short (500ms): a misbehaving gpsd could leave
// the socket open without accepting connections; we don't want to
// stall the whole flare on this probe.
func probeGPSDSocketAt(path string) (*flareschema.GPSCandidate, *flareschema.CollectorIssue) {
	if _, err := os.Stat(path); err != nil {
		return nil, &flareschema.CollectorIssue{
			Kind: "gpsd_unavailable", Message: err.Error(), Path: path,
		}
	}
	d := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := d.Dial("unix", path)
	if err != nil {
		return nil, &flareschema.CollectorIssue{
			Kind: "gpsd_unreachable", Message: err.Error(), Path: path,
		}
	}
	_ = conn.Close()
	return &flareschema.GPSCandidate{
		Kind:       "gpsd_socket",
		Path:       path,
		Reachable:  true,
		Accessible: true,
	}, nil
}

// unixListen is a thin helper used only by the test; declared here so
// the test file doesn't need the net import.
func unixListen(path string) (net.Listener, error) {
	return net.Listen("unix", path)
}
