package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/telemetry"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

func telemetryConfig(c *configstore.TelemetryConfig) telemetry.Config {
	if c == nil {
		return telemetry.Defaults()
	}
	return telemetry.Normalize(telemetry.Config{Enabled: c.Enabled, Callsign: c.Callsign, Channel: c.Channel, Delivery: c.Delivery, RFPath: c.RFPath, Extended: c.AllowAbove255, MetadataISSeconds: c.MetadataISSeconds, MetadataRFSeconds: c.MetadataRFSeconds})
}

// wireTelemetry uses the same counters and transports as existing APRS features.
// No loopback API requests, managed beacon rows or external helper are involved.
func (a *App) wireTelemetry() {
	var queueMu sync.Mutex
	queueToken := make(chan struct{})
	invalidate := func() { queueMu.Lock(); close(queueToken); queueToken = make(chan struct{}); queueMu.Unlock() }
	a.telemetry = telemetry.New(telemetry.Options{
		Load: func(ctx context.Context) (telemetry.Config, telemetry.State, error) {
			cfg, err := a.store.GetTelemetryConfig(ctx)
			if err != nil {
				return telemetry.Config{}, telemetry.State{}, err
			}
			raw, err := a.store.GetTelemetryState(ctx)
			if err != nil {
				return telemetry.Config{}, telemetry.State{}, err
			}
			var state telemetry.State
			if raw != "" {
				if err = json.Unmarshal([]byte(raw), &state); err != nil {
					return telemetry.Config{}, state, fmt.Errorf("telemetry state: %w", err)
				}
			}
			return telemetryConfig(cfg), state, nil
		},
		Save: func(ctx context.Context, state telemetry.State) error {
			raw, err := json.Marshal(state)
			if err != nil {
				return err
			}
			return a.store.SaveTelemetryState(ctx, string(raw))
		},
		Cancel:  invalidate,
		Observe: a.observeTelemetry,
		Send: func(ctx context.Context, cfg telemetry.Config, identity, route, kind, info string) error {
			// Recheck readiness/configuration at submit time, not merely at sampling time.
			obs, err := a.observeTelemetry(ctx, cfg)
			if err != nil {
				return err
			}
			if cfg.Callsign == "" && identity != obs.StationCallsign {
				return fmt.Errorf("station identity changed before submission")
			}
			current, err := a.store.GetTelemetryConfig(ctx)
			if err != nil {
				return err
			}
			if current == nil || telemetryConfig(current) != cfg {
				return fmt.Errorf("telemetry settings changed before submission")
			}
			via, err := ax25.ParseVia(cfg.RFPath)
			if err != nil {
				return err
			}
			src, err := ax25.ParseAddress(identity)
			if err != nil {
				return err
			}
			dst, _ := ax25.ParseAddress("APGRWO")
			frame, err := ax25.NewUIFrame(src, dst, via, []byte(info))
			if err != nil {
				return err
			}
			if route == "rf" {
				if !obs.RFReady {
					return fmt.Errorf("RF delivery channel is not ready")
				}
				queueMu.Lock()
				token := queueToken
				queueMu.Unlock()
				return a.gov.SubmitExpiring(ctx, cfg.Channel, frame, txgovernor.SubmitSource{Kind: "telemetry", Detail: "T# " + kind, Priority: txgovernor.PriorityBeacon}, time.Now().Add(telemetry.MaxQueueAge), token)
			}
			if !obs.ISReady {
				return fmt.Errorf("APRS-IS is not connected or is in simulation mode")
			}
			ig := a.ig.Load()
			if ig == nil {
				return fmt.Errorf("APRS-IS is disabled")
			}
			line := aprs.FormatTNC2(identity, "APGRWO", []string{"TCPIP*"}, []byte(info))
			return sendTelemetryIS(ig, a.plog, cfg.Channel, frame, line)
		},
	})
}

// sendTelemetryIS logs only accepted APRS-IS submissions, using the same
// route annotation as beacon uploads. Source remains telemetry so consumers
// can identify the producer without guessing from the packet payload.
func sendTelemetryIS(sink beacon.ISSink, plog *packetlog.Log, channel uint32, frame *ax25.Frame, line string) error {
	raw, err := frame.Encode()
	if err != nil {
		return err
	}
	if err = sink.SendLine(line); err != nil {
		return err
	}
	plog.Record(packetlog.Entry{Direction: packetlog.DirIS, Source: "telemetry", Channel: channel, Raw: raw, Display: line, Type: "telemetry", Notes: "aprs-is"})
	return nil
}

func (a *App) observeTelemetry(ctx context.Context, cfg telemetry.Config) (telemetry.Observation, error) {
	var obs telemetry.Observation
	if a.cfg.Demo {
		return obs, fmt.Errorf("telemetry transmission is unavailable in demo mode")
	}
	counts, source, err := telemetry.CollectCounters(a.metrics.Registry)
	if err != nil {
		return obs, err
	}
	obs.Counts = counts
	obs.DigiSource = source
	obs.StationCallsign, _ = a.store.ResolveStationCallsign(ctx)
	channels, err := a.store.ListChannels(ctx)
	if err != nil {
		return obs, err
	}
	ifaces, err := a.store.ListKissInterfaces(ctx)
	if err != nil {
		return obs, err
	}
	statuses := a.kissMgr.Status()
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		native := false
		if a.bridge != nil && ch.InputDeviceID != nil {
			_, active := a.bridge.GetChannelStats(ch.ID)
			native = a.bridge.IsRunning() && active
		}
		connected := false
		kissTX := false
		for _, ki := range ifaces {
			if !ki.Enabled || ki.Channel != ch.ID || ki.Mode != configstore.KissModeTnc {
				continue
			}
			state := statuses[ki.ID].State
			live := state == kiss.StateConnected || (state == kiss.StateListening && a.kissMgr.ActiveClients(ki.ID) > 0)
			connected = connected || live
			kissTX = kissTX || (live && ki.AllowTxFromGovernor)
		}
		obs.InputReady = obs.InputReady || native || connected
		if ch.ID == cfg.Channel && ch.Mode != configstore.ChannelModePacket {
			obs.RFReady = kissTX || (native && ch.OutputDeviceID != 0)
			if native {
				ptt, e := a.store.GetPttConfigForChannel(ctx, ch.ID)
				if e != nil {
					return obs, e
				}
				obs.RFReady = obs.RFReady && ptt != nil && ptt.Method != "none"
			}
		}
	}
	if ig := a.ig.Load(); ig != nil {
		st := ig.Status()
		obs.ISReady = st.Connected && !st.SimulationMode
	}
	return obs, nil
}

func (a *App) telemetryComponent() namedComponent {
	return namedComponent{name: "telemetry", start: func(ctx context.Context) error {
		runCtx, cancel := context.WithCancel(ctx)
		a.telemetryCancel = cancel
		a.telemetryWG.Add(1)
		go func() { defer a.telemetryWG.Done(); a.telemetry.Run(runCtx) }()
		return nil
	}, stop: func(ctx context.Context) error {
		if a.telemetryCancel != nil {
			a.telemetryCancel()
		}
		a.telemetry.Invalidate()
		return waitGroup(ctx, &a.telemetryWG, "telemetry")
	}}
}
