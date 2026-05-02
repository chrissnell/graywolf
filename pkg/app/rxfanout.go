package app

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/ax25conn"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/stationcache"
)

// kissTncProduce is the RxIngress callback wired into kiss.Manager. It
// performs a non-blocking send of (rf, src) onto the shared rxFanout
// channel; on drop (consumer saturated) the app-level counter
// increments so the failure mode is observable rather than silent.
//
// Non-blocking is deliberate: a stuck consumer must drop off-air KISS
// frames before it back-pressures the KISS server's socket reader,
// which could in turn back-pressure the physical hardware TNC.
func (a *App) kissTncProduce(rf *pb.ReceivedFrame, src ingress.Source) {
	if rf == nil {
		return
	}
	select {
	case a.rxFanout <- rxFanoutItem{rf: rf, src: src}:
		if a.metrics != nil {
			a.metrics.ObserveKissTncRxDispatched(src.ID)
		}
	default:
		a.rxFanoutDropped.Add(1)
		if a.metrics != nil {
			a.metrics.RxFanoutDropped.WithLabelValues("kiss_tnc").Inc()
		}
	}
}

// dispatchRxFrame runs the fanout consumer's per-frame work: KISS
// broadcast (with self-echo suppression for KISS-TNC sources), digi
// handling, AGW monitoring, APRS decode + submit, station cache
// update, and packet-log recording. Source-specific differences are
// limited to the broadcast skip arguments and the packetlog "source"
// string; all other subscribers treat KISS-TNC frames identically to
// modem-RX frames, which is the D2 invariant.
func (a *App) dispatchRxFrame(ctx context.Context, item rxFanoutItem, aprsSubmit *aprsSubmitter) {
	rf := item.rf
	src := item.src
	var (
		logSource string
		skipID    uint32
		skip      bool
	)
	switch src.Kind {
	case ingress.KindModem:
		logSource = "modem"
	case ingress.KindKissTnc:
		logSource = "kiss-tnc"
		skipID = src.ID
		skip = true
	default:
		if a.logger != nil {
			a.logger.Warn("rx fanout: unknown ingress kind; dropping frame",
				"kind", src.Kind, "channel", rf.Channel)
		}
		return
	}

	a.kissMgr.BroadcastFromChannel(rf.Channel, rf.Data, skipID, skip)

	f, err := ax25.Decode(rf.Data)
	if err != nil {
		a.plog.Record(packetlog.Entry{
			Channel:   rf.Channel,
			Direction: packetlog.DirRX,
			Source:    logSource,
			Raw:       rf.Data,
		})
		return
	}

	e := packetlog.Entry{
		Channel:   rf.Channel,
		Direction: packetlog.DirRX,
		Source:    logSource,
		Raw:       rf.Data,
		Display:   f.String(),
	}

	// Per-frame debug log for KISS-TNC ingest — Phase 5 of the KISS
	// modem/TNC plan. Modem-RX is not logged here because existing TX
	// and packet-log paths already cover those events.
	if src.Kind == ingress.KindKissTnc && a.logger != nil {
		a.logger.Debug("kiss tnc ingress",
			"interface_id", src.ID,
			"channel", rf.Channel,
			"frame_len", len(rf.Data),
			"source_callsign", f.Source.String(),
		)
	}

	if f.IsUI() {
		if srv := a.currentAgwServer(); srv != nil {
			srv.BroadcastMonitoredUI(uint8(rf.Channel), f)
		}
		a.digi.Handle(ctx, rf.Channel, f, src)
		if pkt, err := aprs.Parse(f); err == nil && pkt != nil {
			pkt.Channel = int(rf.Channel)
			pkt.Direction = aprs.DirectionRF
			e.Type = string(pkt.Type)
			e.Decoded = pkt
			aprsSubmit.submit(pkt)
			if entries := stationcache.ExtractEntry(pkt, logSource, "RX", rf.Channel); len(entries) > 0 {
				a.stationCache.Update(entries)
			}
		}
	} else if a.ax25Mgr != nil {
		// Connected-mode dispatch: any non-UI frame that decodes goes to
		// the LAPB manager. Mismatch on (channel, local, peer) is silent —
		// the manager has no session for it.
		if cmFrame, err := ax25conn.Decode(rf.Data, false); err == nil {
			a.ax25Mgr.Dispatch(rf.Channel, cmFrame)
		}
	}
	a.plog.Record(e)
}
