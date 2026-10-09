//go:build darwin && cgo

package kiss

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// TestClassicBluetooth_Hardware dials a real paired radio. Skipped unless
// GRAYWOLF_BT_ADDR is set (e.g. DC:0D:30:57:04:31). It only reads; the
// optional GRAYWOLF_BT_SECS (default 30) is how long to listen. Run it
// while another radio beacons, with or without the radio already
// connected to the Mac (the open drops an existing link first).
//
//	GRAYWOLF_BT_ADDR=DC:0D:30:57:04:31 go test ./pkg/kiss -run Hardware -v
func TestClassicBluetooth_Hardware(t *testing.T) {
	addr := os.Getenv("GRAYWOLF_BT_ADDR")
	if addr == "" {
		t.Skip("set GRAYWOLF_BT_ADDR to run against a real radio")
	}
	secs := 30
	if v, err := strconv.Atoi(os.Getenv("GRAYWOLF_BT_SECS")); err == nil && v > 0 {
		secs = v
	}

	devs, err := PairedClassicBluetooth()
	if err != nil {
		t.Fatalf("PairedClassicBluetooth: %v", err)
	}
	t.Logf("paired devices: %+v", devs)

	start := time.Now()
	c, err := OpenClassicBluetooth(addr, 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Logf("opened in %s", time.Since(start).Round(time.Millisecond))

	got := 0
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	go func() {
		time.Sleep(time.Until(deadline))
		c.Close()
	}()
	buf := make([]byte, 512)
	for {
		n, err := c.Read(buf)
		if n > 0 {
			got += n
			t.Logf("RX %d bytes: % x", n, buf[:min(n, 24)])
		}
		if err != nil {
			t.Logf("read ended: %v (total %d bytes)", err, got)
			break
		}
	}
}
