package app

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/telemetry"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

func TestTelemetryWaitsForConnectedKissAndSupportsReceiveOnlyInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ch := configstore.Channel{Name: "TNC input"}
	if err = store.CreateChannel(ctx, &ch); err != nil {
		t.Fatal(err)
	}
	ki := configstore.KissInterface{Name: "test-tnc", InterfaceType: "tcp-client", Mode: configstore.KissModeTnc, Channel: ch.ID, RemoteHost: "127.0.0.1", RemotePort: 8001, Enabled: true}
	if err = store.CreateKissInterface(ctx, &ki); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "TEST1-10"}); err != nil {
		t.Fatal(err)
	}
	a := &App{store: store, metrics: metrics.New(), logger: quietLogger()}
	a.kissMgr = kiss.NewManager(kiss.ManagerConfig{Logger: quietLogger()})
	defer a.kissMgr.StopAll()
	cfg := telemetry.Defaults()
	cfg.Channel = ch.ID
	obs, err := a.observeTelemetry(ctx, cfg)
	if err != nil || obs.InputReady || obs.RFReady {
		t.Fatalf("unconnected: %+v %v", obs, err)
	}
	client, peer := net.Pipe()
	defer peer.Close()
	a.kissMgr.StartClient(ctx, ki.ID, kiss.ClientConfig{Name: ki.Name, RemoteHost: "127.0.0.1", RemotePort: 8001, Mode: kiss.ModeTnc, ChannelMap: map[uint8]uint32{0: ch.ID}, DialFunc: func(context.Context, string, string) (net.Conn, error) { return client, nil }, Logger: quietLogger()})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.kissMgr.Status()[ki.ID].State == kiss.StateConnected {
			break
		}
		time.Sleep(time.Millisecond)
	}
	obs, err = a.observeTelemetry(ctx, cfg)
	if err != nil || !obs.InputReady || obs.RFReady || obs.ISReady {
		t.Fatalf("RX-only connection: %+v %v", obs, err)
	}
	// No received packet, audio device, PTT, or Internet session was needed.
	ki.AllowTxFromGovernor = true
	if err = store.UpdateKissInterface(ctx, &ki); err != nil {
		t.Fatal(err)
	}
	obs, err = a.observeTelemetry(ctx, cfg)
	if err != nil || !obs.RFReady {
		t.Fatalf("TX-enabled: %+v %v", obs, err)
	}
	model := &configstore.TelemetryConfig{Enabled: true, Channel: ch.ID, Delivery: "rf", MetadataISSeconds: 7200, MetadataRFSeconds: 21600}
	if err = store.UpsertTelemetryConfig(ctx, model); err != nil {
		t.Fatal(err)
	}
	a.gov = txgovernor.New(txgovernor.Config{}) // Queues only; no sender or radio is run.
	a.wireTelemetry()
	start := time.Unix(1000000, 0)
	for elapsed := time.Duration(0); elapsed <= telemetry.Window; elapsed += telemetry.SampleEvery {
		a.telemetry.Tick(ctx, start.Add(elapsed))
	}
	if a.gov.QueueLen() != 1 || len(a.telemetry.Snapshot().Recent) != 1 {
		t.Fatalf("missing native submission: %+v", a.telemetry.Snapshot())
	}
	a.telemetry.Invalidate()
	if a.telemetry.Snapshot().Ready {
		t.Fatal("reset did not invalidate baseline")
	}
}

func TestTelemetryISSubmissionPacketLog(t *testing.T) {
	for _, info := range []string{
		"T#001,001,002,003,004,005,00000000",
		":TEST1-10 :PARM.Rx,Digi,Tx,RF2IS,IS2RF",
		":TEST1-10 :UNIT.Packets,Packets,Packets,Packets,Packets",
		":TEST1-10 :EQNS.0,1,0,0,1,0,0,1,0,0,1,0,0,1,0",
	} {
		t.Run(info, func(t *testing.T) {
			src, _ := ax25.ParseAddress("TEST1-10")
			dst, _ := ax25.ParseAddress("APGRWO")
			frame, err := ax25.NewUIFrame(src, dst, nil, []byte(info))
			if err != nil {
				t.Fatal(err)
			}
			line := aprs.FormatTNC2("TEST1-10", "APGRWO", []string{"TCPIP*"}, []byte(info))
			for _, fail := range []bool{false, true} {
				plog := packetlog.New(packetlog.Config{})
				sink := &fakeISLineSink{}
				if fail {
					sink.err = errors.New("not connected")
				}
				err = sendTelemetryIS(sink, plog, frame, line)
				if fail {
					if !errors.Is(err, sink.err) || plog.Len() != 0 {
						t.Fatalf("failed send: %v; log size %d", err, plog.Len())
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				entries := plog.Query(packetlog.Filter{Channel: -1})
				if len(entries) != 1 || len(sink.lines) != 1 || sink.lines[0] != line {
					t.Fatalf("send/log mismatch: %v %+v", sink.lines, entries)
				}
				e := entries[0]
				if e.Direction != packetlog.DirIS || e.Source != "telemetry" || e.Notes != "aprs-is" || e.Channel != 0 || e.Display != line || e.Type != "telemetry" {
					t.Fatalf("missing explicit telemetry upload context: %+v", e)
				}
				decoded, err := ax25.Decode(e.Raw)
				if err != nil || string(decoded.Info) != info {
					t.Fatalf("raw payload mismatch: %v %v", decoded, err)
				}
			}
		})
	}
}
