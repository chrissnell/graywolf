package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/agw"
	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/app/txbackend"
	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/callsign"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/digipeater"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/historydb"
	"github.com/chrissnell/graywolf/pkg/igate"
	"github.com/chrissnell/graywolf/pkg/igate/filters"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/modembridge"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/pttdevice"
	"github.com/chrissnell/graywolf/pkg/stationcache"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"github.com/chrissnell/graywolf/pkg/updatescheck"
	"github.com/chrissnell/graywolf/pkg/webapi"
	"github.com/chrissnell/graywolf/pkg/webauth"
	"github.com/chrissnell/graywolf/web"
)

// wireServices constructs every component graywolf owns and populates
// a.startOrder with namedComponent entries in the order they must be
// brought up. Reverse of that order is the shutdown order.
//
// Ordering constraint summary (forward = start order, reverse = stop):
//
//	configstore  — must come up first so every subsequent construction
//	               call can read live config; must stop last so ongoing
//	               component stops can still read if they need to.
//	metrics      — no goroutines; registered purely for symmetric
//	               visibility in the startOrder log.
//	tx governor  — owns the Submit() path every sink adapter uses.
//	background stats tickers — read governor + packetlog state, stop
//	               before the governor so they never see a torn-down gov.
//	kiss manager — its listener goroutines feed the governor.
//	digipeater   — uses governor Submit; reload loop goroutine.
//	gps manager  — independent, but beacon reads its cache.
//	beacon       — uses governor Submit and gps cache.
//	bridge       — owns the subprocess and bridge.Frames() channel. The
//	               RX fan-out is bundled into the bridge component so
//	               stop can sequence bridge.Stop() → frame consumer
//	               exit → APRS fan-out drain atomically.
//	agw          — server-side, broadcasts decoded UI from the RX
//	               fan-out, so must be torn down before the fan-out.
//	igate        — uses governor for IS→RF; external network connection.
//	http         — last in; stops first on shutdown so requests stop
//	               arriving before handlers start seeing torn-down state.
//
// This function may open real resources (the configstore SQLite file).
// If it fails partway through, it rolls back whatever it opened before
// returning the error so Run's Stop path does not see half-wired state.
func (a *App) wireServices(ctx context.Context) error {
	if err := a.cfg.Validate(); err != nil {
		return err
	}

	// --- Configstore ---------------------------------------------------
	//
	// Opened synchronously here (not inside the configstore component's
	// start closure) because every subsequent constructor below reads
	// from the store. On any later error we close the store before
	// returning so the caller does not leak the fd.
	store, err := configstore.Open(a.cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open configstore: %w", err)
	}
	a.store = store
	// Log the SQLite runtime version so ops can confirm the database
	// binary satisfies the minimum version required by our migrations
	// (≥ 3.25 for ALTER TABLE RENAME COLUMN + the 12-step rebuild
	// added in migration 8). Empty string means the probe failed,
	// which we log verbatim rather than treating as fatal.
	a.logger.Info("sqlite runtime", "version", a.store.SQLiteVersion())

	// Phase 5 orphan scan: one-shot bootstrap check for soft-FK
	// references that don't resolve (channels were deleted before the
	// cascade logic landed, or a SQL shell surgery left dangling IDs).
	// Logged at warn per referrer table with the offending row ids and
	// the distinct missing channel ids, so operators can locate the
	// broken referrers without clicking through every list page (plan
	// D6). No deletion or cleanup happens here; remediation is the
	// cascade-delete UI. A probe error here (table missing on a
	// pristine DB before AutoMigrate) is swallowed per-table in
	// ListOrphanChannelRefs and not fatal.
	if orphans, err := a.store.ListOrphanChannelRefs(ctx); err == nil {
		for _, entry := range orphans {
			a.logger.Warn("orphaned "+entry.Token+" referrers",
				"ids", entry.RowIDs,
				"missing_channels", entry.MissingChannelIDs)
		}
	} else {
		// Nonsense for the whole scan to fail at bootstrap, but surface
		// it rather than silently swallow.
		a.logger.Warn("orphan channel-ref scan failed", "err", err)
	}

	if err := a.wireServicesInner(ctx); err != nil {
		_ = a.store.Close()
		a.store = nil
		a.startOrder = nil
		return err
	}
	return nil
}

// wireServicesInner performs the rest of wireServices after the store
// is open. Split out so the outer function can handle its error path
// with a single defer-like cleanup. Any error here means the outer
// function closes the store before returning.
func (a *App) wireServicesInner(ctx context.Context) error {
	// --- FLAC override (optional, mutates the store) -------------------
	if err := a.applyFlacOverride(ctx); err != nil {
		return fmt.Errorf("apply flac override: %w", err)
	}

	// --- Metrics -------------------------------------------------------
	a.metrics = metrics.New()

	// --- Modem binary resolution + version banner ----------------------
	resolvedModem, err := ResolveModemPath(a.cfg.ModemPath)
	if err != nil {
		return fmt.Errorf("locate graywolf-modem: %w", err)
	}
	a.resolvedModem = resolvedModem
	pttdevice.SetModemPath(resolvedModem)
	modemVersion, verr := QueryModemVersion(resolvedModem)
	if verr != nil {
		// Not fatal: log and move on. If the binary is actually broken,
		// bridge.Start's handshake will surface it with a better error.
		a.logger.Warn("query graywolf-modem version",
			"path", resolvedModem, "err", verr)
		modemVersion = "unknown"
	}
	a.logger.Info("starting graywolf",
		"graywolf", a.cfg.FullVersion(),
		"graywolf-modem", modemVersion)
	if modemVersion != "unknown" && modemVersion != a.cfg.FullVersion() {
		a.logger.Warn("graywolf and graywolf-modem versions disagree — possibly a mixed build",
			"graywolf", a.cfg.FullVersion(),
			"graywolf-modem", modemVersion,
			"modem_path", resolvedModem)
	}

	// --- Packet log ----------------------------------------------------
	a.plog = packetlog.New(packetlog.Config{Capacity: 5000, MaxAge: 120 * time.Minute})

	// --- Station cache (map's last-known-state store) ------------------
	a.stationCache = stationcache.NewPersistentCache(a.logger)
	plCfg, _ := a.store.GetPositionLogConfig(ctx)
	if plCfg != nil && plCfg.Enabled && a.cfg.HistoryDBPath != "" {
		hdb, err := historydb.Open(a.cfg.HistoryDBPath)
		if err != nil {
			a.logger.Warn("failed to open history db, starting without persistence", "path", a.cfg.HistoryDBPath, "err", err)
		} else {
			a.logger.Info("opened history db", "path", hdb.Path)
			if err := a.stationCache.Reconfigure(hdb); err != nil {
				a.logger.Warn("failed to hydrate from history db", "err", err)
			}
		}
	}

	// --- Modem bridge (construction; Start happens later) --------------
	a.bridge = modembridge.New(modembridge.Config{
		BinaryPath: resolvedModem,
		Store:      a.store,
		Metrics:    a.metrics,
		Logger:     a.logger,
	})

	// --- TX backend dispatcher (Phase 3) -------------------------------
	//
	// Replaces the pre-Phase-3 direct-to-modem Sender. The dispatcher
	// resolves every governor-scheduled frame to zero-or-more backends
	// (ModemBackend and/or one KissTncBackend per eligible KissInterface
	// row) and fans out; see pkg/app/txbackend for the fanout contract.
	// The kiss manager doesn't exist yet at this point in the wiring
	// order — we attach it to the snapshot builder via closure capture
	// and the first snapshot is rebuilt once the manager starts.
	a.txDispatcher = txbackend.New(txbackend.Config{
		Metrics: a.metrics, // *metrics.Metrics satisfies the txbackend.Metrics interface.
		Logger:  a.logger,
	})
	a.txBackendReload = make(chan struct{}, 1)

	txSender := func(tf *pb.TransmitFrame) error {
		return a.txDispatcher.Send(tf)
	}

	// Load per-channel timing and rate limits from configstore. A store
	// error is not fatal: the governor just runs with empty defaults.
	channelTimings := make(map[uint32]txgovernor.ChannelTiming)
	var rate1, rate5 int
	if timings, err := a.store.ListTxTimings(ctx); err == nil {
		for _, t := range timings {
			channelTimings[t.Channel] = txgovernor.ChannelTiming{
				SlotTime: time.Duration(t.SlotMs) * time.Millisecond,
				Persist:  uint8(t.Persist),
				FullDup:  t.FullDup,
			}
			if t.Rate1Min > 0 && rate1 == 0 {
				rate1 = int(t.Rate1Min)
			}
			if t.Rate5Min > 0 && rate5 == 0 {
				rate5 = int(t.Rate5Min)
			}
		}
	}

	a.gov = txgovernor.New(txgovernor.Config{
		Sender:        txSender,
		DcdEvents:     a.bridge.DcdEvents(),
		Rate1MinLimit: rate1,
		Rate5MinLimit: rate5,
		DedupWindow:   30 * time.Second,
		Channels:      channelTimings,
		Logger:        a.logger,
	})
	// D3.4: governor consults the dispatcher's per-channel CSMA-skip
	// flag to bypass p-persistence / slot-time / DCD waits for
	// KISS-only channels.
	a.gov.SetSkipCSMA(a.txDispatcher.SkipCSMA)

	// TX hook: record transmitted frames into the packet log and
	// update the station cache for beacon transmissions.
	plog := a.plog
	sc := a.stationCache
	_, unregisterTxHook := a.gov.AddTxHook(func(channel uint32, frame *ax25.Frame, source txgovernor.SubmitSource) {
		e := packetlog.Entry{
			Channel:   channel,
			Direction: packetlog.DirTX,
			Source:    source.Kind,
			Display:   frame.String(),
			Notes:     source.Detail,
		}
		if raw, err := frame.Encode(); err == nil {
			e.Raw = raw
		}
		plog.Record(e)

		// Feed our own beacon position into the station cache.
		if source.Kind == "beacon" && frame.IsUI() {
			if pkt, err := aprs.Parse(frame); err == nil && pkt != nil {
				pkt.Channel = int(channel)
				if entries := stationcache.ExtractEntry(pkt, "beacon", "TX", channel); len(entries) > 0 {
					sc.Update(entries)
				}
			}
		}
	})
	a.govHookUnregister = unregisterTxHook

	// --- KISS manager --------------------------------------------------
	a.kissMgr = kiss.NewManager(kiss.ManagerConfig{
		Sink:          a.gov,
		Logger:        a.logger,
		OnDecodeError: a.metrics.KissDecodeErrors.Inc,
		OnFrameIngress: func(ifaceID uint32, mode kiss.Mode) {
			a.metrics.ObserveKissIngressFrame(ifaceID, string(mode))
		},
		OnBroadcastSuppressed: a.metrics.ObserveKissBroadcastSuppressed,
		RxIngress:             a.kissTncProduce,
		OnTxQueueDepth:        a.metrics.SetKissInstanceTxQueueDepth,
		// OnTxQueueDrop surfaces per-instance tx drops to the Phase 4
		// tcp-client counter (graywolf_kiss_client_tx_drops_total).
		// The dispatcher also records backend_busy / backend_down
		// outcomes with instance labels — that's the preferred
		// cardinality (per channel × backend). This counter is kept
		// on purpose so operators can split tcp-client health from
		// server-listen health at the interface grain, which the
		// dispatcher's per-instance instance label already mixes
		// into the same series when the queues are fanned out.
		OnTxQueueDrop: a.metrics.ObserveKissClientTxDrop,
		OnClientStateChange: func(ifaceID uint32, name string, st kiss.InterfaceStatus) {
			connected := st.State == kiss.StateConnected
			a.metrics.SetKissClientConnected(ifaceID, name, connected)
			a.metrics.SetKissClientBackoffSeconds(ifaceID, st.BackoffSeconds)
		},
		OnClientReconnect: a.metrics.ObserveKissClientReconnect,
	})

	// --- Digipeater ----------------------------------------------------
	digi, err := digipeater.New(digipeater.Config{
		DedupeWindow: 30 * time.Second,
		Submit:       a.gov.Submit,
		Logger:       a.logger,
		OnPacket: func(note string, fromChan, toChan uint32, f *ax25.Frame) {
			a.metrics.DigipeaterPackets.Inc()
			a.plog.Record(packetlog.Entry{
				Channel:   toChan,
				Direction: packetlog.DirTX,
				Source:    "digipeater",
				Display:   f.String(),
				Notes:     note,
			})
			// Update station cache with digipeated station positions.
			if f.IsUI() {
				if pkt, err := aprs.Parse(f); err == nil && pkt != nil {
					pkt.Channel = int(toChan)
					if entries := stationcache.ExtractEntry(pkt, "digipeater", "TX", toChan); len(entries) > 0 {
						a.stationCache.Update(entries)
					}
				}
			}
		},
		OnDedup: func() { a.metrics.DigipeaterDeduped.Inc() },
	})
	if err != nil {
		return fmt.Errorf("digipeater init: %w", err)
	}
	a.digi = digi
	a.digipeaterReload = make(chan struct{}, 1)

	// --- GPS cache + manager -------------------------------------------
	a.gpsCache = gps.NewMemCache()
	a.stationPos = gps.NewStationPos(a.gpsCache)
	a.gpsReload = make(chan struct{}, 1)
	a.gpsMgr = newGPSManager(a.store, a.gpsCache, a.logger, a.metrics)

	// --- Beacon scheduler ----------------------------------------------
	beaconSched, err := beacon.New(beacon.Options{
		Sink:     a.gov,
		Cache:    a.gpsCache,
		Logger:   a.logger,
		Observer: &beaconObserver{m: a.metrics},
		Version:  a.cfg.Version,
	})
	if err != nil {
		return fmt.Errorf("beacon scheduler init: %w", err)
	}
	a.beaconSched = beaconSched
	a.beaconReload = make(chan struct{}, 1)
	a.smartBeaconReload = make(chan struct{}, 1)

	// --- Messages: LocalTxRing is shared by iGate gating + messages ----
	//
	// The ring is constructed before the iGate so we can pass it into
	// igate.Config.LocalOrigin with SuppressLocalMessageReGate=true; the
	// same ring is later handed to messages.Service via ServiceConfig.
	// Keeping construction here (rather than deferred into wireMessages)
	// means the iGate and messages.Service share the exact same ring
	// pointer without needing a post-construction SetLocalOrigin setter.
	a.msgLocalRing = messages.NewLocalTxRing(messages.DefaultLocalTxRingSize, messages.DefaultLocalTxRingTTL)

	// --- iGate (optional) ----------------------------------------------
	if err := a.wireIGate(ctx); err != nil {
		return err
	}
	if a.ig != nil {
		a.beaconSched.SetISSink(a.ig)
	}

	// --- Messages service ---------------------------------------------
	if err := a.wireMessages(ctx); err != nil {
		return err
	}

	// --- AGW server (optional) -----------------------------------------
	if err := a.wireAGW(ctx); err != nil {
		return err
	}

	// --- APRS fan-out queue (consumed from the bridge component) -------
	a.aprsQueue = make(chan *aprs.DecodedAPRSPacket, 256)

	// --- RX fanout channel (shared by modem + KISS-TNC producers) ----
	// Buffer sized to match aprsQueue: steady-state arrival rates are
	// the same order of magnitude, and making it any smaller would
	// regress modem backpressure because the modem path now goes
	// through this channel first before reaching aprsQueue.
	a.rxFanout = make(chan rxFanoutItem, 256)

	// --- Auth store ----------------------------------------------------
	authStore, err := webauth.NewAuthStore(a.store.DB())
	if err != nil {
		return fmt.Errorf("init auth store: %w", err)
	}
	a.authStore = authStore

	// --- HTTP server ---------------------------------------------------
	if err := a.wireHTTP(ctx); err != nil {
		return err
	}

	// --- Populate startOrder ------------------------------------------
	//
	// The order here is load-bearing. See the doc comment on
	// wireServices for the full justification.
	a.startOrder = []namedComponent{
		a.configstoreComponent(),
		a.metricsComponent(),
		a.stationCacheComponent(),
		a.governorComponent(),
		// Phase 3: the TX dispatcher's watcher goroutine is wired just
		// after the governor. Start order: governor spawns Run (which
		// will call into the dispatcher's Send); dispatcher starts
		// watcher so the initial snapshot is present before the first
		// frame flows. Stop order (reverse): dispatcher stops accepting
		// new sends BEFORE governor goroutines wind down — see D15.
		a.txBackendComponent(),
		a.backgroundStatsComponent(),
		a.updatesCheckComponent(),
		a.kissComponent(),
		a.digipeaterComponent(),
		a.gpsComponent(),
		a.beaconComponent(),
		a.bridgeComponent(),
		a.agwComponent(),
		a.igateComponent(),
		// messages runs after igate (sender needs IGateLineSender) and
		// after the bridge fan-out is spun up (Router is already in the
		// outputs slice). It runs BEFORE http so handlers see a fully
		// started service on first request; the 503 guard in the webapi
		// layer covers the brief window between http bind and start.
		a.messagesComponent(),
		a.httpComponent(),
	}
	return nil
}

// applyFlacOverride implements the -flac flag: point the first (or a
// newly-created) audio device at a local FLAC file and ensure at least
// one channel uses it, so offline tests don't need a real radio.
func (a *App) applyFlacOverride(ctx context.Context) error {
	if a.cfg.FlacFile == "" {
		return nil
	}
	absPath, err := filepath.Abs(a.cfg.FlacFile)
	if err != nil {
		return fmt.Errorf("resolve flac path: %w", err)
	}
	if _, err := os.Stat(absPath); err != nil {
		return fmt.Errorf("flac file not found: %s", absPath)
	}
	devs, _ := a.store.ListAudioDevices(ctx)
	if len(devs) == 0 {
		dev := &configstore.AudioDevice{
			Name: "FLAC Input", Direction: "input",
			SourceType: "flac", SourcePath: absPath,
			SampleRate: 44100, Channels: 1, Format: "s16le",
		}
		if err := a.store.CreateAudioDevice(ctx, dev); err != nil {
			return fmt.Errorf("create flac audio device: %w", err)
		}
		devs = []configstore.AudioDevice{*dev}
	} else {
		devs[0].SourceType = "flac"
		devs[0].SourcePath = absPath
		devs[0].SampleRate = 44100
		if err := a.store.UpdateAudioDevice(ctx, &devs[0]); err != nil {
			return fmt.Errorf("update audio device for flac: %w", err)
		}
	}
	a.logger.Info("audio device overridden", "source", "flac", "path", absPath)

	// Ensure at least one channel exists so the FLAC source gets used.
	chs, _ := a.store.ListChannels(ctx)
	if len(chs) == 0 {
		ch := &configstore.Channel{
			Name: "FLAC Test", InputDeviceID: configstore.U32Ptr(devs[0].ID),
			ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		}
		if err := a.store.CreateChannel(ctx, ch); err != nil {
			return fmt.Errorf("create default channel for flac: %w", err)
		}
		a.logger.Info("created default channel for flac input", "device_id", devs[0].ID)
	}
	return nil
}

// wireIGate constructs a.ig from configstore. A disabled or missing
// iGate config leaves a.ig nil, which the igateComponent stop closure
// handles via a nil-check. Per D7 the webapi layer refuses to save
// Enabled=true without a station callsign set, so the resolve-below
// should succeed in the happy path. If it doesn't (hand-edited DB,
// migration anomaly), we log a warning and leave the iGate nil rather
// than panicking — graceful degradation matches the rest of the wiring.
func (a *App) wireIGate(ctx context.Context) error {
	igCfg, err := a.store.GetIGateConfig(ctx)
	if err != nil || igCfg == nil || !igCfg.Enabled {
		return nil
	}

	stationCall, err := a.store.ResolveStationCallsign(ctx)
	if err != nil {
		if errors.Is(err, callsign.ErrCallsignEmpty) || errors.Is(err, callsign.ErrCallsignN0Call) {
			a.logger.Warn("iGate will not start: station callsign unset or N0CALL — set it on the Station Callsign page",
				"reason", err.Error())
			return nil
		}
		return fmt.Errorf("resolve station callsign: %w", err)
	}

	rfFilters, _ := a.store.ListIGateRfFilters(ctx)
	rules := make([]filters.Rule, 0, len(rfFilters))
	for _, f := range rfFilters {
		if !f.Enabled {
			continue
		}
		rules = append(rules, filters.Rule{
			ID:       f.ID,
			Priority: int(f.Priority),
			Type:     filters.RuleType(f.Type),
			Pattern:  f.Pattern,
			Action:   filters.Action(f.Action),
		})
	}

	serverAddr := fmt.Sprintf("%s:%d", igCfg.Server, igCfg.Port)
	var igGov txgovernor.TxSink
	if len(rules) > 0 {
		igGov = a.gov
	}

	txCh := igCfg.TxChannel
	if txCh == 0 {
		if chs, err := a.store.ListChannels(ctx); err == nil && len(chs) > 0 {
			txCh = chs[0].ID // lowest channel ID
		}
	}

	// Compose the APRS-IS server filter via the single entry point so
	// enabled tactical callsigns are auto-appended as g/ clauses. Any
	// raw read of igCfg.ServerFilter outside buildIgateFilter is
	// guarded by an enforcement test.
	composedFilter, err := buildIgateFilter(ctx, a.store)
	if err != nil {
		return fmt.Errorf("compose igate server filter: %w", err)
	}
	a.lastAppliedIgateFilter = composedFilter

	ig, err := igate.New(igate.Config{
		Server:          serverAddr,
		StationCallsign: stationCall,
		ServerFilter:    composedFilter,
		SoftwareName:    igCfg.SoftwareName,
		SoftwareVersion: igCfg.SoftwareVersion,
		Rules:           rules,
		TxChannel:       txCh,
		Governor:        igGov,
		SimulationMode:  igCfg.SimulationMode,
		Logger:          a.logger,
		Registry:        a.metrics.Registry,
		// Messages self-filter: messages.Service records every outbound
		// message (source, msg_id) into a.msgLocalRing before submit.
		// When a digipeater re-broadcasts our packet back to us over RF,
		// the iGate's gateRFToIS path consults this ring and skips the
		// APRS-IS upload so we don't double-gate our own messages.
		LocalOrigin:                a.msgLocalRing,
		SuppressLocalMessageReGate: true,
		RfToIsHook: func(pkt *aprs.DecodedAPRSPacket, line string) {
			if pkt == nil {
				return
			}
			a.plog.Record(packetlog.Entry{
				Channel:   uint32(pkt.Channel),
				Direction: packetlog.DirIS,
				Source:    "igate",
				Raw:       pkt.Raw,
				Display:   line,
				Type:      string(pkt.Type),
				Decoded:   pkt,
				Notes:     "rf2is",
			})
			// RF-heard station uploaded to IS — cache as via=rf.
			if entries := stationcache.ExtractEntry(pkt, "igate", "IS", uint32(pkt.Channel)); len(entries) > 0 {
				a.stationCache.Update(entries)
			}
		},
		IsRxHook: func(pkt *aprs.DecodedAPRSPacket, line string) {
			if pkt == nil {
				return
			}
			a.plog.Record(packetlog.Entry{
				Channel:   uint32(pkt.Channel),
				Direction: packetlog.DirRX,
				Source:    "igate-is",
				Raw:       pkt.Raw,
				Display:   line,
				Type:      string(pkt.Type),
				Decoded:   pkt,
				Notes:     "is-rx",
			})
			// Station received from APRS-IS — cache as via=is.
			if entries := stationcache.ExtractEntry(pkt, "igate-is", "RX", uint32(pkt.Channel)); len(entries) > 0 {
				a.stationCache.Update(entries)
			}
		},
	})
	if err != nil {
		// Matches the old main.go behavior: init failure is logged but
		// does not take out the whole app. The iGate just stays nil.
		a.logger.Error("igate init", "err", err)
		return nil
	}
	a.ig = ig
	a.igateReload = make(chan struct{}, 1)
	return nil
}

// wireMessages constructs the messages.Service that owns the APRS
// text-messaging pipeline (inbound Router, outbound Sender, RetryManager,
// Preferences cache, EventHub, TacticalSet). Construction is unconditional:
// the service exists even when iGate is disabled because RF-only messaging
// is a supported mode. The service's iGate fallback path is a no-op when
// a.ig is nil — Sender.sendIS returns a clear error and the RF-only
// policy (or RF-only passcode fallback) continues to work.
//
// Ordering — this runs AFTER wireIGate so the Service sees the live
// *igate.Igate (for IGateLineSender) and shares the same LocalTxRing
// pointer that was already bound into igate.Config.LocalOrigin. The
// Service is NOT started here — Service.Start happens inside
// messagesComponent so the TxHook registration and the Router / retry
// goroutines fire in the right lifecycle slot.
func (a *App) wireMessages(ctx context.Context) error {
	a.msgStore = messages.NewStore(a.store.DB())
	a.messagesReload = make(chan struct{}, 1)

	// OurCall closure: resolves the operator's primary callsign from
	// StationConfig. Per D8 messaging identity is always the station
	// callsign — no per-feature override, no fallback. The closure is
	// invoked on every router self-filter check, every auto-ACK, and
	// every compose-handler loopback check, so it needs to re-read each
	// time to pick up live StationConfig changes.
	//
	// Resolution errors (empty / N0CALL) collapse to "" here because the
	// router + sender contract treats "" as "unset — refuse to source
	// self-addressed traffic". A proper operator-facing error is surfaced
	// at the compose path (see Service.SendMessage's OurCall=="" check).
	ourCall := func() string {
		c, err := a.store.ResolveStationCallsign(context.Background())
		if err != nil {
			return ""
		}
		return c
	}

	// TxChannel is read from the iGate config when present so the sender
	// picks the operator's preferred RF channel. The IGatePasscode
	// argument to messages.NewService is kept for its read-only-IS gate
	// semantics (empty / "-1" → disable IS fallback): we compute the
	// passcode at wire time from the resolved station callsign via
	// callsign.APRSPasscode. When the station callsign is unset, we pass
	// "" so the sender treats IS as read-only; this matches the pre-
	// centralization behaviour where an unconfigured iGate row meant
	// an empty passcode.
	var txChannel uint32
	if igCfg, _ := a.store.GetIGateConfig(ctx); igCfg != nil {
		txChannel = igCfg.TxChannel
	}
	var passcode string
	if stationCall, err := a.store.ResolveStationCallsign(ctx); err == nil {
		passcode = strconv.Itoa(callsign.APRSPasscode(stationCall))
	}

	// iGate line-sender: only when the iGate is wired. A nil IGate in
	// ServiceConfig means the IS path logs + emits message.failed and
	// falls back per policy; the Service tolerates nil.
	var igSender messages.IGateLineSender
	if a.ig != nil {
		igSender = a.ig
	}

	svc, err := messages.NewService(messages.ServiceConfig{
		Store:         a.msgStore,
		ConfigStore:   a.store,
		TxSink:        a.gov,
		TxHookReg:     a.gov,
		IGate:         igSender,
		Bridge:        a.bridge,
		StationCache:  a.stationCache,
		Logger:        a.logger.With("component", "messages"),
		TxChannel:     txChannel,
		IGatePasscode: passcode,
		OurCall:       ourCall,
		LocalTxRing:   a.msgLocalRing, // shared with iGate.Config.LocalOrigin
	})
	if err != nil {
		return fmt.Errorf("messages service init: %w", err)
	}
	a.msgSvc = svc
	return nil
}

// wireAGW constructs a.agwServer from configstore. A disabled or
// missing AGW config leaves a.agwServer nil. The agwReload channel is
// created unconditionally so an initially-disabled AGW can be toggled
// on via the API without restarting graywolf.
func (a *App) wireAGW(ctx context.Context) error {
	a.agwReload = make(chan struct{}, 1)

	agwCfg, err := a.store.GetAgwConfig(ctx)
	if err != nil || agwCfg == nil || !agwCfg.Enabled {
		return nil
	}
	a.agwServer = a.buildAgwServer(agwCfg)
	return nil
}

// buildAgwServer constructs an *agw.Server from the given AGW config.
// Shared by wireAGW (startup) and reloadAgw (live reconfigure).
func (a *App) buildAgwServer(agwCfg *configstore.AgwConfig) *agw.Server {
	calls := strings.Split(agwCfg.Callsigns, ",")
	for i := range calls {
		calls[i] = strings.TrimSpace(calls[i])
	}
	return agw.NewServer(agw.ServerConfig{
		ListenAddr:    agwCfg.ListenAddr,
		PortCallsigns: calls,
		PortToChannel: map[uint8]uint32{0: 1},
		Sink:          a.gov,
		Logger:        a.logger,
		OnClientChange: func(n int) {
			a.metrics.SetAgwClients(n)
		},
		OnDecodeError: func(stage string) {
			a.metrics.AgwDecodeErrors.WithLabelValues(stage).Inc()
		},
	})
}

// currentAgwServer returns the currently-installed AGW server or nil
// if AGW is disabled. Takes the guard mutex so callers can race with
// a live reload swap.
func (a *App) currentAgwServer() *agw.Server {
	a.agwMu.Lock()
	defer a.agwMu.Unlock()
	return a.agwServer
}

// wireHTTP builds the HTTP server, webapi server, auth handlers, and
// embedded UI mux. It does NOT call ListenAndServe — the httpComponent
// start closure does that so the lifecycle hook is symmetric.
func (a *App) wireHTTP(ctx context.Context) error {
	// Warn if binding to a non-loopback address. Secure cookies require
	// HTTPS; since we don't support TLS, always false.
	secure := false
	host, _, _ := net.SplitHostPort(a.cfg.HTTPAddr)
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		a.logger.Warn(fmt.Sprintf("Web server binding to %s — accessible from all network interfaces", a.cfg.HTTPAddr))
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", a.metrics.Handler())

	apiSrv, err := webapi.NewServer(webapi.Config{
		Store:         a.store,
		Bridge:        a.bridge,
		KissManager:   a.kissMgr,
		KissCtx:       ctx,
		Logger:        a.logger,
		HistoryDBPath: a.cfg.HistoryDBPath,
		Version:       a.cfg.Version,
	})
	if err != nil {
		return fmt.Errorf("webapi new: %w", err)
	}
	a.apiSrv = apiSrv

	if a.ig != nil {
		apiSrv.SetIgateStatusFn(a.ig.Status)
		apiSrv.SetIgateReload(a.igateReload)
	}
	apiSrv.SetGPSReload(a.gpsReload)
	apiSrv.SetBeaconReload(a.beaconReload)
	apiSrv.SetSmartBeaconReload(a.smartBeaconReload)
	apiSrv.SetBeaconSendNow(a.beaconSched.SendNow)
	apiSrv.SetDigipeaterReload(a.digipeaterReload)
	apiSrv.SetAgwReload(a.agwReload)
	apiSrv.SetTxBackendReload(a.txBackendReload)
	a.positionLogReload = make(chan struct{}, 1)
	apiSrv.SetPositionLogReload(a.positionLogReload)

	// --- Messages wiring on the API server ---------------------------
	//
	// Install the store first so pure-read handlers (list, get,
	// conversations, participants) can serve as soon as the HTTP
	// listener binds. Then install the service so mutating handlers
	// (compose, resend, delete, read/unread, preferences PUT) can
	// route through the Service. Finally install the reload channel
	// — messagesComponent's drainer goroutine consumes from it.
	//
	// Handlers guard each setter with a 503 until the corresponding
	// field is populated, so the narrow window between HTTP listener
	// bind and messagesComponent.start is safe: any request that
	// lands early gets "service unavailable" rather than a crash.
	if a.msgStore != nil {
		apiSrv.SetMessagesStore(a.msgStore)
	}
	if a.msgSvc != nil {
		apiSrv.SetMessagesService(a.msgSvc)
	}
	if a.messagesReload != nil {
		apiSrv.SetMessagesReload(a.messagesReload)
	}
	apiSrv.SetMessagesBotDirectory(messages.DefaultBotDirectory)

	// Construct the GitHub-update checker and install it on the webapi
	// server so GET /api/updates/status can project its cached Snapshot.
	// The checker's Run goroutine is launched by updatesCheckComponent;
	// the reload channel it selects on is owned by apiSrv and surfaced
	// via UpdatesReloadCh(). See pkg/updatescheck and plan D4.
	a.updatesChecker = updatescheck.NewChecker(
		a.cfg.Version,
		a.store,
		updatescheck.DefaultBaseURL,
		a.logger.With("component", "updatescheck"),
	)
	apiSrv.SetUpdatesChecker(a.updatesChecker)

	// /api/version is public (the UI reads it before login to pick
	// which screens to show). It is mounted on the outer mux so it
	// bypasses RequireAuth. The handler itself lives in pkg/webapi.
	webapi.RegisterVersion(apiSrv, mux)

	// /api/auth/* is public and lives on the outer mux (not the
	// RequireAuth-wrapped apiMux). Each endpoint is bound to an explicit
	// method via Go 1.22 method-scoped patterns so the mux produces
	// 405 with an Allow header automatically for wrong verbs.
	authHandlers := &webauth.Handlers{
		Auth:          a.authStore,
		Secure:        secure,
		Logger:        a.logger,
		SessionMaxAge: a.cfg.SessionMaxAge,
		BuildVersion:  a.cfg.Version,
	}
	mux.HandleFunc("POST /api/auth/login", authHandlers.HandleLogin)
	mux.HandleFunc("POST /api/auth/logout", authHandlers.HandleLogout)
	mux.HandleFunc("GET /api/auth/setup", authHandlers.GetSetupStatus)
	mux.HandleFunc("POST /api/auth/setup", authHandlers.CreateFirstUser)

	apiMux := http.NewServeMux()
	apiSrv.RegisterRoutes(apiMux)
	// Canonical shape for every out-of-band RegisterXxx helper is
	// (srv, mux, deps...). Keep this block consistent.
	webapi.RegisterPackets(apiSrv, apiMux, a.plog, a.stationPos)
	webapi.RegisterStations(apiSrv, apiMux, a.stationCache)
	webapi.RegisterPosition(apiSrv, apiMux, a.stationPos)
	if a.ig != nil {
		webapi.RegisterIgate(apiSrv, apiMux, a.ig.SetSimulationMode, a.ig.Status)
	}
	// Stations autocomplete is an out-of-band registrar (matches the
	// RegisterPackets / RegisterStations shape). Needs the messages
	// store for history-prefix lookups and the station cache for
	// live-station prefix lookups; bot results come from the
	// BotDirectory installed via SetMessagesBotDirectory above.
	if a.msgStore != nil {
		webapi.RegisterStationsAutocomplete(apiSrv, apiMux, a.msgStore, a.stationCache)
	}
	// Release notes live on apiMux (auth-required). Caller identity
	// (for the ack write and unseen filter) comes from the session
	// middleware via webauth.AuthenticatedUser; the version string
	// seeds the response envelope's `current` field and is what the
	// ack handler writes to LastSeenReleaseVersion.
	webapi.RegisterReleaseNotes(apiSrv, apiMux, a.cfg.Version, a.authStore)

	mux.Handle("/api/", webauth.RequireAuth(a.authStore)(apiMux))
	mux.Handle("/", web.SPAHandler())

	a.httpSrv = &http.Server{
		Addr:              a.cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return nil
}

// --- Component factories -------------------------------------------------
//
// Each of the functions below returns a namedComponent whose closures
// capture whatever state from the App they need. Kept as methods so
// they can see the private fields; kept separate from wireServices so
// the startup ordering table at the bottom of wireServicesInner is a
// simple list and not an inline wall of closures.

func (a *App) configstoreComponent() namedComponent {
	return namedComponent{
		name:  "configstore",
		start: func(ctx context.Context) error { return nil },
		stop: func(ctx context.Context) error {
			if a.store == nil {
				return nil
			}
			return a.store.Close()
		},
	}
}

func (a *App) metricsComponent() namedComponent {
	// Metrics has no goroutines — this entry exists purely so the
	// startup log lists it in the right place for symmetry.
	return namedComponent{
		name:  "metrics",
		start: func(ctx context.Context) error { return nil },
		stop:  func(ctx context.Context) error { return nil },
	}
}

func (a *App) stationCacheComponent() namedComponent {
	return namedComponent{
		name: "station cache",
		start: func(ctx context.Context) error {
			if a.positionLogReload == nil {
				return nil
			}
			a.positionLogReloadWG.Add(1)
			go func() {
				defer a.positionLogReloadWG.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case <-a.positionLogReload:
						a.reconfigurePositionLog(ctx)
					}
				}
			}()
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			if err := waitGroup(shutdownCtx, &a.positionLogReloadWG, "position log reload"); err != nil {
				return err
			}
			if a.stationCache != nil {
				a.stationCache.Close()
			}
			return nil
		},
	}
}

func (a *App) reconfigurePositionLog(ctx context.Context) {
	cfg, err := a.store.GetPositionLogConfig(ctx)
	if err != nil {
		a.logger.Warn("read position log config", "err", err)
		return
	}
	if cfg == nil || !cfg.Enabled || a.cfg.HistoryDBPath == "" {
		if err := a.stationCache.Reconfigure(nil); err != nil {
			a.logger.Warn("disable position log", "err", err)
		}
		return
	}
	hdb, err := historydb.Open(a.cfg.HistoryDBPath)
	if err != nil {
		a.logger.Warn("open history db", "path", a.cfg.HistoryDBPath, "err", err)
		return
	}
	a.logger.Info("opened history db", "path", hdb.Path)
	if err := a.stationCache.Reconfigure(hdb); err != nil {
		a.logger.Warn("reconfigure position log", "err", err)
	}
}

func (a *App) governorComponent() namedComponent {
	return namedComponent{
		name: "tx governor",
		start: func(ctx context.Context) error {
			a.govWG.Add(1)
			go func() {
				defer a.govWG.Done()
				if err := a.gov.Run(ctx); err != nil {
					a.logger.Error("tx governor", "err", err)
				}
			}()
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			// Release the packetlog/stationcache TX hook before draining
			// so any in-flight post-send invocation completes before we
			// declare the governor stopped.
			if a.govHookUnregister != nil {
				a.govHookUnregister()
				a.govHookUnregister = nil
			}
			// Run exits when its parent ctx is cancelled, which already
			// happened by the time shutdown started in Run. Just wait.
			return waitGroup(shutdownCtx, &a.govWG, "tx governor")
		},
	}
}

// backgroundStatsComponent owns the governor-stats → Prometheus ticker
// and the packetlog gauge ticker. They are grouped because they share
// the same lifetime: neither has any dependency other than ctx
// cancellation, and neither has a meaningful stop signal beyond "the
// context is dead".
func (a *App) backgroundStatsComponent() namedComponent {
	return namedComponent{
		name: "background stats",
		start: func(ctx context.Context) error {
			a.statsWG.Add(2)
			go func() {
				defer a.statsWG.Done()
				t := time.NewTicker(2 * time.Second)
				defer t.Stop()
				var prev txgovernor.Stats
				// KISS-TNC drop counters are cumulative at the source
				// (Phase 3 accessors). Track last-seen values per
				// interface + reason so we can translate cumulative
				// snapshots into Prometheus counter deltas, matching
				// the pattern used for the TX governor above.
				lastKissRate := map[uint32]uint64{}
				lastKissQueue := map[uint32]uint64{}
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						s := a.gov.Stats()
						if d := s.RateLimited - prev.RateLimited; d > 0 {
							for i := uint64(0); i < d; i++ {
								a.metrics.TxRateLimited.Inc()
							}
						}
						if d := s.Deduped - prev.Deduped; d > 0 {
							for i := uint64(0); i < d; i++ {
								a.metrics.TxDeduped.Inc()
							}
						}
						if d := s.QueueDropped - prev.QueueDropped; d > 0 {
							for i := uint64(0); i < d; i++ {
								a.metrics.TxQueueDropped.Inc()
							}
						}
						prev = s

						a.syncKissTncDropMetrics(ctx, lastKissRate, lastKissQueue)
					}
				}
			}()
			go func() {
				defer a.statsWG.Done()
				t := time.NewTicker(5 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						a.metrics.PacketlogEntries.Set(float64(a.plog.Len()))
					}
				}
			}()
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			return waitGroup(shutdownCtx, &a.statsWG, "background stats")
		},
	}
}

// updatesCheckComponent launches the daily GitHub release-tag poller.
// The Checker is constructed in wireHTTP (it needs the webapi server's
// reload channel); this component only owns the lifetime of its Run
// goroutine. Run blocks until ctx is cancelled; on shutdown we wait on
// updatesWG for the goroutine to actually exit.
func (a *App) updatesCheckComponent() namedComponent {
	return namedComponent{
		name: "updates check",
		start: func(ctx context.Context) error {
			a.updatesWG.Add(1)
			go func() {
				defer a.updatesWG.Done()
				// Run returns ctx.Err() on shutdown; the checker logs
				// transient errors internally at debug level, so we
				// deliberately discard the returned error here —
				// ctx-cancellation is the expected exit path.
				_ = a.updatesChecker.Run(ctx, a.apiSrv.UpdatesReloadCh())
			}()
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			return waitGroup(shutdownCtx, &a.updatesWG, "updates check")
		},
	}
}

// syncKissTncDropMetrics polls Phase 3's per-interface cumulative drop
// counters (rate-limit + per-interface queue overflow) and translates
// any delta since the last tick into Prometheus counter increments.
// Iterates configured interfaces from the store so hot-added or
// hot-removed rows are picked up naturally between ticks; polling a
// non-running or non-TNC interface returns zero and is a no-op.
func (a *App) syncKissTncDropMetrics(ctx context.Context, lastRate, lastQueue map[uint32]uint64) {
	ifaces, err := a.store.ListKissInterfaces(ctx)
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("kiss tnc drop metrics sync: list interfaces", "err", err)
		}
		return
	}
	for _, ki := range ifaces {
		cur := a.KissTncDropped(ki.ID)
		if cur > lastRate[ki.ID] {
			a.metrics.KissTncIngressDropped.
				WithLabelValues(strconv.FormatUint(uint64(ki.ID), 10), "rate_limit").
				Add(float64(cur - lastRate[ki.ID]))
		}
		lastRate[ki.ID] = cur

		curQ := a.KissTncQueueOverflow(ki.ID)
		if curQ > lastQueue[ki.ID] {
			a.metrics.KissTncIngressDropped.
				WithLabelValues(strconv.FormatUint(uint64(ki.ID), 10), "queue_full").
				Add(float64(curQ - lastQueue[ki.ID]))
		}
		lastQueue[ki.ID] = curQ
	}
}

func (a *App) kissComponent() namedComponent {
	return namedComponent{
		name: "kiss",
		start: func(ctx context.Context) error {
			kissIfaces, _ := a.store.ListKissInterfaces(ctx)
			for _, ki := range kissIfaces {
				if !ki.Enabled {
					continue
				}
				ch := ki.Channel
				if ch == 0 {
					ch = 1
				}
				name := ki.Name
				mode := kiss.Mode(ki.Mode)
				if mode == "" {
					mode = kiss.ModeModem
				}
				switch ki.InterfaceType {
				case configstore.KissTypeTCPClient:
					if ki.RemoteHost == "" || ki.RemotePort == 0 {
						continue
					}
					a.kissMgr.StartClient(ctx, ki.ID, kiss.ClientConfig{
						Name:                name,
						RemoteHost:          ki.RemoteHost,
						RemotePort:          ki.RemotePort,
						ReconnectInitMs:     ki.ReconnectInitMs,
						ReconnectMaxMs:      ki.ReconnectMaxMs,
						Logger:              a.logger,
						ChannelMap:          map[uint8]uint32{0: ch},
						Mode:                mode,
						TncIngressRateHz:    ki.TncIngressRateHz,
						TncIngressBurst:     ki.TncIngressBurst,
						AllowTxFromGovernor: ki.AllowTxFromGovernor,
						OnReload:            a.notifyTxBackendReload,
					})
					continue
				case configstore.KissTypeTCP:
					if ki.ListenAddr == "" {
						continue
					}
				default:
					// serial / bluetooth not wired through the
					// manager today; skip.
					continue
				}
				a.kissMgr.Start(ctx, ki.ID, kiss.ServerConfig{
					Name:                name,
					ListenAddr:          ki.ListenAddr,
					Logger:              a.logger,
					ChannelMap:          map[uint8]uint32{0: ch},
					Broadcast:           ki.Broadcast,
					Mode:                mode,
					TncIngressRateHz:    ki.TncIngressRateHz,
					TncIngressBurst:     ki.TncIngressBurst,
					AllowTxFromGovernor: ki.AllowTxFromGovernor,
					OnClientChange: func(n int) {
						a.metrics.SetKissClients(name, n)
					},
				})
			}
			// Nudge the TX dispatcher to rebuild its snapshot now that
			// kiss.Manager has running interfaces (including tx queues
			// for AllowTxFromGovernor rows). Non-blocking — the
			// watcher goroutine coalesces bursts.
			a.notifyTxBackendReload()
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			// D15 step 4: cancel every running server and wait for its
			// per-instance tx queue writer to exit. Manager.StopAll is
			// idempotent; see pkg/kiss/manager.go.
			a.kissMgr.StopAll()
			return nil
		},
	}
}

// notifyTxBackendReload pokes the dispatcher's watcher goroutine to
// rebuild the registry snapshot. Non-blocking; the watcher coalesces
// bursts into a single rebuild.
func (a *App) notifyTxBackendReload() {
	if a.txBackendReload == nil {
		return
	}
	select {
	case a.txBackendReload <- struct{}{}:
	default:
	}
}

// txBackendComponent owns the dispatcher's rebuild watcher goroutine.
// Lifecycle is independent of the governor / modem: the watcher just
// recomputes the snapshot when the caller signals a config change.
// Shutdown ordering (D15):
//
//  1. governor.Drain via governorComponent's own stop already ran by
//     the time this stop is called (component order: governor starts
//     first, stops last after kiss / bridge).
//  2. StopAccepting flips the dispatcher-closed bit so any late
//     governor callback returns ErrStopped rather than fanning out
//     to backends whose Close was queued.
//  3. The watcher exits on ctx cancellation; WaitWatcher blocks
//     until it is truly gone.
//
// The watcher's build closure captures a.store, a.kissMgr, and
// a.bridge by reference — all three are already wired by the time
// this component's start runs (wireServicesInner order), and they
// remain valid across the watcher's lifetime (kiss.Manager is
// stopped AFTER the governor, and the dispatcher's Send hot path
// already stops accepting by then).
func (a *App) txBackendComponent() namedComponent {
	return namedComponent{
		name: "tx dispatcher",
		start: func(ctx context.Context) error {
			build := a.buildTxBackendSnapshot
			a.txDispatcher.StartWatcher(ctx, a.txBackendReload, build)
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			// Dispatcher accepts no more sends from here on. Governor's
			// own drain already ran (see governorComponent.stop), and
			// the kissComponent below us tore down per-instance queues.
			a.txDispatcher.StopAccepting()
			done := make(chan struct{})
			go func() { a.txDispatcher.WaitWatcher(); close(done) }()
			select {
			case <-done:
				return nil
			case <-shutdownCtx.Done():
				return fmt.Errorf("tx dispatcher watcher: shutdown timed out")
			}
		},
	}
}

// buildTxBackendSnapshot is the builder closure the dispatcher's
// watcher invokes on every rebuild. Pure function from the live
// configstore + running kiss manager + modem bridge state; no
// caching. Called from the watcher goroutine, so there is no
// overlapping concurrent invocation — the dispatcher's atomic
// Publish handles reader visibility on its own.
func (a *App) buildTxBackendSnapshot() *txbackend.Snapshot {
	ctx := context.Background()

	// Modem side: attach a ModemBackend to every channel with a bound
	// input audio device. The bridge's subprocess may or may not be
	// running; the backend forwards via bridge.SendTransmitFrame which
	// returns an error (surfaced as outcome=err) if no IPC session is
	// live. Registering the backend regardless keeps the snapshot a
	// pure config projection: health is a separate runtime concern.
	var modemChannels []uint32
	if chs, err := a.store.ListChannels(ctx); err == nil {
		for _, c := range chs {
			if c.InputDeviceID != nil {
				modemChannels = append(modemChannels, c.ID)
			}
		}
	}
	var modem *txbackend.ModemBackend
	if a.bridge != nil {
		modem = txbackend.NewModemBackend(a.bridge, modemChannels)
	}

	// KISS-TNC side: one backend per eligible interface row. The
	// registry stays populated even when an interface's supervisor is
	// down (see D3.3) — Enqueue surfaces ErrBackendDown in that case,
	// which the dispatcher records per-instance.
	var kissBackends []*txbackend.KissTncBackend
	if ifaces, err := a.store.ListKissInterfaces(ctx); err == nil {
		for _, ki := range ifaces {
			if !ki.Enabled {
				continue
			}
			if ki.Mode != configstore.KissModeTnc {
				continue
			}
			if !ki.AllowTxFromGovernor {
				continue
			}
			if ki.Channel == 0 {
				continue
			}
			q := a.kissMgr.InstanceQueueFor(ki.ID)
			if q == nil {
				// Interface configured but not started yet, or Mode flip
				// without restart. The next kiss reload will signal us
				// again and we'll pick it up.
				continue
			}
			kissBackends = append(kissBackends, txbackend.NewKissTncBackend(q, ki.ID, ki.Channel))
		}
	}

	return txbackend.BuildSnapshot(modem, modemChannels, kissBackends)
}

func (a *App) digipeaterComponent() namedComponent {
	reload := func(ctx context.Context) {
		cfg, err := a.store.GetDigipeaterConfig(ctx)
		if err != nil || cfg == nil {
			a.digi.SetEnabled(false)
			a.digi.SetRules(nil)
			return
		}
		// Resolve per-digipeater override against the station callsign.
		// Per D7 the webapi layer refuses Enabled=true when the station
		// callsign is unset, so the happy path always yields a usable
		// value. If resolution fails (stale DB, migration race), disable
		// the digipeater and log — the per-frame guard in pkg/digipeater
		// would drop frames anyway, but refusing to flip Enabled=true is
		// cleaner and avoids the WARN-per-frame flood.
		stationCall, _ := a.store.ResolveStationCallsign(ctx)
		resolved, err := callsign.Resolve(cfg.MyCall, stationCall)
		if err != nil {
			a.logger.Warn("digipeater will not be enabled: station callsign unset or N0CALL",
				"override", cfg.MyCall, "err", err)
			a.digi.SetEnabled(false)
			a.digi.SetRules(nil)
			return
		}
		mycall, err := ax25.ParseAddress(resolved)
		if err != nil {
			a.logger.Warn("digipeater mycall parse failed",
				"value", resolved, "err", err)
			a.digi.SetEnabled(false)
			a.digi.SetRules(nil)
			return
		}
		a.digi.SetMyCall(mycall)
		a.digi.SetDedupeWindow(time.Duration(cfg.DedupeWindowSeconds) * time.Second)
		rules, err := a.store.ListDigipeaterRules(ctx)
		if err != nil {
			a.logger.Warn("digipeater rules load", "err", err)
			rules = nil
		}
		a.digi.SetRules(digipeater.RulesFromStore(rules))
		a.digi.SetEnabled(cfg.Enabled)
	}

	return namedComponent{
		name: "digipeater",
		start: func(ctx context.Context) error {
			reload(ctx)
			a.digiReloadWG.Add(1)
			go func() {
				defer a.digiReloadWG.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case <-a.digipeaterReload:
						reload(ctx)
					}
				}
			}()
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			return waitGroup(shutdownCtx, &a.digiReloadWG, "digipeater reload")
		},
	}
}

func (a *App) gpsComponent() namedComponent {
	return namedComponent{
		name: "gps",
		start: func(ctx context.Context) error {
			a.gpsWG.Add(1)
			go func() {
				defer a.gpsWG.Done()
				a.gpsMgr.Run(ctx, a.gpsReload)
			}()
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			return waitGroup(shutdownCtx, &a.gpsWG, "gps")
		},
	}
}

func (a *App) beaconComponent() namedComponent {
	return namedComponent{
		name: "beacon",
		start: func(ctx context.Context) error {
			a.beaconSched.SetBeacons(a.loadBeaconConfigs(ctx, "initial"))
			a.beaconWG.Add(1)
			go func() {
				defer a.beaconWG.Done()
				if err := a.beaconSched.Run(ctx); err != nil {
					a.logger.Error("beacon scheduler", "err", err)
				}
			}()
			a.beaconReloadWG.Add(1)
			go func() {
				defer a.beaconReloadWG.Done()
				// Fan in both beacon reload signals: per-beacon rows
				// (beaconReload) and the global SmartBeacon singleton
				// (smartBeaconReload) both feed into the same
				// loadBeaconConfigs → scheduler.Reload path, because
				// beaconConfigFromStore re-reads both on every reload.
				// Extending the existing select is simpler than a
				// forwarder goroutine and keeps non-blocking-send
				// coalescing on both source channels.
				for {
					select {
					case <-ctx.Done():
						return
					case <-a.beaconReload:
						a.beaconSched.Reload(a.loadBeaconConfigs(ctx, "beacon-reload"))
						a.notifyBeaconReload()
					case <-a.smartBeaconReload:
						a.beaconSched.Reload(a.loadBeaconConfigs(ctx, "smart-beacon-reload"))
						a.notifyBeaconReload()
					}
				}
			}()
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			if err := waitGroup(shutdownCtx, &a.beaconReloadWG, "beacon reload"); err != nil {
				return err
			}
			return waitGroup(shutdownCtx, &a.beaconWG, "beacon scheduler")
		},
	}
}

// loadBeaconConfigs reads the current beacon rows and the global
// SmartBeacon singleton from configstore, maps each beacon through
// beaconConfigFromStore against the same singleton, and seeds the
// station-position fallback from the first enabled fixed-position
// beacon. Extracted from beaconComponent's start closure so the integration
// test in pkg/app can exercise the read-and-map path directly without
// standing up the full scheduler goroutine.
func (a *App) loadBeaconConfigs(ctx context.Context, source string) []beacon.Config {
	stored, err := a.store.ListBeacons(ctx)
	if err != nil {
		// Returning nil here unschedules every beacon for this reload
		// cycle — log at Error level so transient DB faults don't hide
		// behind routine Warn traffic. Source tags the reload trigger
		// so operators can correlate with webapi PUTs.
		a.logger.Error("beacon load", "source", source, "err", err)
		return nil
	}
	// Fetch the global SmartBeacon singleton once before the loop so
	// every beacon row is mapped against the same curve. nil is fine
	// — beaconConfigFromStore treats a nil singleton as "global off"
	// per the precedence rule, which means per-beacon SmartBeacon
	// flags become no-ops until a global config is saved.
	smart, err := a.store.GetSmartBeaconConfig(ctx)
	if err != nil {
		a.logger.Warn("smart-beacon load failed; falling back to disabled", "err", err)
		smart = nil
	}
	// Resolve the station callsign once per reload. A resolution error
	// (empty / N0CALL) is passed through as an empty string: per-beacon
	// overrides that supply their own callsign still work; beacons
	// relying on the station fallback fail with an error in
	// beaconConfigFromStore and are skipped individually (D6 —
	// "one bad beacon does not kill the scheduler").
	stationCall, _ := a.store.ResolveStationCallsign(ctx)
	var configs []beacon.Config
	for _, b := range stored {
		bc, err := beaconConfigFromStore(b, smart, stationCall)
		if err != nil {
			a.logger.Warn("beacon config", "id", b.ID, "err", err)
			continue
		}
		configs = append(configs, bc)
	}
	// Seed station-position fallback from the first enabled
	// fixed-position beacon so distance works without GPS.
	var fb *gps.Fix
	for _, c := range configs {
		if c.Enabled && !c.UseGps && c.Lat != 0 && c.Lon != 0 {
			f := gps.Fix{Latitude: c.Lat, Longitude: c.Lon}
			if c.AltFt != 0 {
				f.Altitude = c.AltFt / 3.28084
				f.HasAlt = true
			}
			fb = &f
			break
		}
	}
	if a.stationPos != nil {
		a.stationPos.SetFallback(fb)
	}
	return configs
}

// notifyBeaconReload is a test seam. When a.beaconReloadDone is non-nil,
// every successful reload performs a non-blocking send onto it so tests
// can wait for a specific reload pass to land without polling. Nil in
// production — the channel stays unset unless a test wires it up.
func (a *App) notifyBeaconReload() {
	if a.beaconReloadDone == nil {
		return
	}
	select {
	case a.beaconReloadDone <- struct{}{}:
	default:
	}
}

// bridgeComponent owns three things at once: the modembridge.Bridge
// lifecycle, the modem→KISS/digi/APRS frame consumer goroutine, and
// the APRS fan-out consumer goroutine. They are bundled because their
// shutdown dependencies form a strict chain: bridge.Stop() closes
// bridge.Frames() → frame consumer exits and closes a.aprsQueue →
// fan-out drains and exits. Splitting them into separate components
// would force the shutdown loop to interleave their stops in a way
// that loses the chain.
func (a *App) bridgeComponent() namedComponent {
	return namedComponent{
		name: "modembridge",
		start: func(ctx context.Context) error {
			if err := a.bridge.Start(ctx); err != nil {
				return err
			}
			// --- APRS decode + log output ---
			aprsOut := aprs.NewLogOutput(a.logger)
			aprsSubmit := newAPRSSubmitter(a.aprsQueue, a.metrics.AprsOutDropped, a.logger)

			// iGate output adapter for the fan-out (nil if iGate is off).
			var igateOut *igate.IgateOutput
			if a.ig != nil {
				igateOut = igate.NewIgateOutput(a.ig)
			}

			// Messages router output: classifies inbound APRS text
			// messages into DM / tactical / auto-ACK responses. Nil
			// is tolerated (messagesComponent wires it above; an empty
			// slot in the outputs slice is filtered by runAPRSFanOut).
			// The router's SendPacket drops silently until Router.Start
			// runs inside messagesComponent; the net effect is that any
			// inbound packets arriving before the Service starts are
			// ignored rather than queued indefinitely.
			var msgOut aprs.PacketOutput
			if a.msgSvc != nil {
				msgOut = a.msgSvc.Router()
			}

			a.fanOutWG.Add(1)
			go func() {
				defer a.fanOutWG.Done()
				var igOut aprs.PacketOutput
				if igateOut != nil {
					igOut = igateOut
				}
				runAPRSFanOut(ctx, a.aprsQueue, aprsOut, igOut, msgOut)
			}()

			// Modem producer: reads bridge.Frames() and blocking-sends
			// into rxFanout. Blocking send preserves today's demod
			// backpressure: if the fanout consumer is slow, modem RX
			// slows to match.
			a.frameConsumerWG.Add(1)
			go func() {
				defer a.frameConsumerWG.Done()
				for rf := range a.bridge.Frames() {
					if rf == nil {
						continue
					}
					select {
					case a.rxFanout <- rxFanoutItem{rf: rf, src: ingress.Modem()}:
					case <-ctx.Done():
						return
					}
				}
			}()

			// Fanout consumer: dispatches each (rf, src) to subscribers.
			// Closes aprsQueue on exit so the APRS fan-out goroutine can
			// drain and return. See bridgeComponent.stop for the ordered
			// teardown chain that makes close-on-exit safe.
			a.rxFanoutWG.Add(1)
			go func() {
				defer a.rxFanoutWG.Done()
				defer close(a.aprsQueue)
				for {
					select {
					case <-ctx.Done():
						return
					case item := <-a.rxFanout:
						a.dispatchRxFrame(ctx, item, aprsSubmit)
					}
				}
			}()
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			// 1. Tell the bridge to stop. bridge.Stop is synchronous but
			//    can take non-trivial time to kill the subprocess; run
			//    it in a goroutine bounded by shutdownCtx.
			done := make(chan struct{})
			go func() { a.bridge.Stop(); close(done) }()
			select {
			case <-done:
			case <-shutdownCtx.Done():
				a.logger.Warn("modembridge shutdown timed out")
				// Fall through — we still wait on the downstream WGs
				// because they might drain even without a clean bridge
				// stop if bridge.Frames() was already closed.
			}

			// 2. Modem producer exits when bridge.Frames() closes
			//    (inside bridge.Stop) or when ctx cancels.
			if err := waitGroup(shutdownCtx, &a.frameConsumerWG, "modem producer"); err != nil {
				return err
			}
			// 3. RX fanout consumer exits on ctx.Done. It closes
			//    aprsQueue on exit so the APRS fan-out can drain.
			if err := waitGroup(shutdownCtx, &a.rxFanoutWG, "rx fanout"); err != nil {
				return err
			}
			// 4. APRS fan-out drains aprsQueue and exits.
			return waitGroup(shutdownCtx, &a.fanOutWG, "aprs fan-out")
		},
	}
}

func (a *App) agwComponent() namedComponent {
	return namedComponent{
		name: "agw",
		start: func(ctx context.Context) error {
			if a.agwServer != nil {
				a.startAgwServer(ctx, a.agwServer)
			}
			if a.agwReload != nil {
				a.agwReloadWG.Add(1)
				go func() {
					defer a.agwReloadWG.Done()
					for {
						select {
						case <-ctx.Done():
							return
						case <-a.agwReload:
							a.reloadAgw(ctx)
						}
					}
				}()
			}
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			// Stop the reload watcher before tearing the server down so
			// a signal racing with shutdown cannot revive it.
			if err := waitGroup(shutdownCtx, &a.agwReloadWG, "agw reload"); err != nil {
				return err
			}
			srv := a.currentAgwServer()
			if srv == nil {
				return nil
			}
			// Shutdown closes live connections and the listener port
			// so a quick restart is safe even before ListenAndServe
			// observes ctx cancel.
			if err := srv.Shutdown(shutdownCtx); err != nil {
				a.logger.Warn("agw server shutdown", "err", err)
			}
			return waitGroup(shutdownCtx, &a.agwWG, "agw server")
		},
	}
}

// startAgwServer launches ListenAndServe for the given server in a
// goroutine tracked by a.agwWG. Caller is expected to have already
// installed the server via a.agwMu.
func (a *App) startAgwServer(ctx context.Context, srv *agw.Server) {
	a.agwWG.Add(1)
	go func() {
		defer a.agwWG.Done()
		if err := srv.ListenAndServe(ctx); err != nil {
			a.logger.Error("agw server", "err", err)
		}
	}()
}

// reloadAgw re-reads AGW config from configstore and reconciles the
// running AGW server with it: stops the old one (if any), starts a
// fresh one (if enabled), and updates a.agwServer under a.agwMu.
//
// Any live TCP clients on the old listener are dropped — AGW has no
// graceful mid-connection config switch, so the only correct behaviour
// on a ListenAddr or callsign change is a hard cycle. Clients will see
// their TCP connection close and reconnect.
func (a *App) reloadAgw(ctx context.Context) {
	agwCfg, err := a.store.GetAgwConfig(ctx)
	if err != nil {
		a.logger.Warn("agw reload: read config", "err", err)
		return
	}

	a.agwMu.Lock()
	old := a.agwServer
	a.agwServer = nil
	a.agwMu.Unlock()

	// Tear the old listener down outside the lock so a slow shutdown
	// can't stall BroadcastMonitoredUI callers.
	if old != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := old.Shutdown(shutdownCtx); err != nil {
			a.logger.Warn("agw reload: old server shutdown", "err", err)
		}
		cancel()
	}

	if agwCfg == nil || !agwCfg.Enabled {
		a.logger.Info("agw reload: disabled")
		return
	}

	srv := a.buildAgwServer(agwCfg)
	a.agwMu.Lock()
	a.agwServer = srv
	a.agwMu.Unlock()
	a.startAgwServer(ctx, srv)
	a.logger.Info("agw reload: restarted", "listen_addr", agwCfg.ListenAddr)
}

func (a *App) igateComponent() namedComponent {
	return namedComponent{
		name: "igate",
		start: func(ctx context.Context) error {
			if a.ig == nil {
				return nil
			}
			if err := a.ig.Start(ctx); err != nil {
				a.logger.Error("igate start", "err", err)
				// Match old main.go behavior: don't abort startup on
				// an iGate connection error, just log it.
			}
			if a.igateReload != nil {
				a.igateReloadWG.Add(1)
				go func() {
					defer a.igateReloadWG.Done()
					for {
						select {
						case <-ctx.Done():
							return
						case <-a.igateReload:
							a.reloadIgate(ctx)
						}
					}
				}()
			}
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			if a.ig == nil {
				return nil
			}
			a.ig.Stop()
			return waitGroup(shutdownCtx, &a.igateReloadWG, "igate reload")
		},
	}
}

// reloadIgate re-reads igate config, rf filters, and tactical callsigns
// from the database and pushes them into the running igate. When the
// composed filter is unchanged we still call Reconfigure (so rule and
// governor changes on the same signal propagate) but skip the metric
// and debug log; Reconfigure's own filter-changed check prevents the
// reconnect.
func (a *App) reloadIgate(ctx context.Context) {
	rfFilters, _ := a.store.ListIGateRfFilters(ctx)
	rules := make([]filters.Rule, 0, len(rfFilters))
	for _, f := range rfFilters {
		if !f.Enabled {
			continue
		}
		rules = append(rules, filters.Rule{
			ID:       f.ID,
			Priority: int(f.Priority),
			Type:     filters.RuleType(f.Type),
			Pattern:  f.Pattern,
			Action:   filters.Action(f.Action),
		})
	}

	var gov txgovernor.TxSink
	if len(rules) > 0 {
		gov = a.gov
	}

	composed, err := buildIgateFilter(ctx, a.store)
	if err != nil {
		a.logger.Warn("igate reload: compose server filter", "err", err)
		return
	}

	if composed == a.lastAppliedIgateFilter {
		a.ig.Reconfigure(composed, rules, gov)
		return
	}

	a.logger.Debug("igate filter recomposed, reconnecting",
		"filter", composed,
		"previous", a.lastAppliedIgateFilter)
	a.ig.Reconfigure(composed, rules, gov)
	a.lastAppliedIgateFilter = composed
	if a.metrics != nil {
		a.metrics.IgateFilterRecompositions.Inc()
	}
}

// buildIgateFilter is the single entry point for constructing the
// APRS-IS login filter. It reads the operator's base ServerFilter and
// appends g/ clauses for each enabled tactical callsign. Tacticals are
// sorted so the composed string is deterministic across DB orderings
// (keeps reloadIgate's no-op-skip stable). Sentinel substitution for
// an empty composed filter stays in igate/client.go; don't duplicate
// it here. The filter_enforcement_test asserts no other caller in
// pkg/app reads igCfg.ServerFilter directly.
func buildIgateFilter(ctx context.Context, store *configstore.Store) (string, error) {
	igCfg, err := store.GetIGateConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("read igate config: %w", err)
	}
	base := ""
	if igCfg != nil {
		base = igCfg.ServerFilter
	}

	tacRows, err := store.ListEnabledTacticalCallsigns(ctx)
	if err != nil {
		return "", fmt.Errorf("list enabled tactical callsigns: %w", err)
	}
	tacticals := make([]string, 0, len(tacRows))
	for _, t := range tacRows {
		if t.Callsign == "" {
			continue
		}
		tacticals = append(tacticals, strings.ToUpper(t.Callsign))
	}
	sort.Strings(tacticals)

	return igate.ComposeServerFilter(base, tacticals), nil
}

// messagesComponent owns the messages.Service lifecycle: Start registers
// the TxHook with the governor, loads initial preferences + tactical
// callsigns, and spins up the Router + RetryManager goroutines. A
// drainer goroutine forwards messagesReload signals into
// Service.ReloadTacticalCallsigns + ReloadPreferences so a REST CRUD
// handler's non-blocking send propagates into the in-memory caches.
//
// Lifecycle invariants:
//   - Start runs AFTER igate so the sender's IS-fallback path has a
//     live *igate.Igate (when enabled). The bridge fan-out is already
//     running by then; Router.SendPacket drops silently until Start
//     flips the running flag, so any inbound message packets that
//     arrived before Start are ignored rather than queued.
//   - Stop runs BEFORE igate / governor in reverse-startup order.
//     Service.Stop unregisters the TxHook before the governor stops
//     so a late hook fire can't touch a torn-down store handle.
//   - The reload-channel drainer exits on ctx.Done or channel close;
//     the channel is owned by the webapi Server, so the drainer never
//     closes it. Stop waits for the drainer via messagesReloadWG.
func (a *App) messagesComponent() namedComponent {
	return namedComponent{
		name: "messages",
		start: func(ctx context.Context) error {
			if a.msgSvc == nil {
				return nil
			}
			if err := a.msgSvc.Start(ctx); err != nil {
				return fmt.Errorf("messages service start: %w", err)
			}
			if a.messagesReload != nil {
				a.messagesReloadWG.Add(1)
				go func() {
					defer a.messagesReloadWG.Done()
					for {
						select {
						case <-ctx.Done():
							return
						case _, ok := <-a.messagesReload:
							if !ok {
								return
							}
							if err := a.msgSvc.ReloadTacticalCallsigns(ctx); err != nil {
								a.logger.Warn("messages reload tactical callsigns", "err", err)
							}
							if err := a.msgSvc.ReloadPreferences(ctx); err != nil {
								a.logger.Warn("messages reload preferences", "err", err)
							}
						}
					}
				}()
			}
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			if a.msgSvc == nil {
				return nil
			}
			// Stop the Service first: unregisters the TxHook, stops the
			// Router consumer goroutine, stops the RetryManager. Late
			// hook fires from a governor drain can no longer reach the
			// Service after this returns.
			a.msgSvc.Stop()
			// Then drain the reload goroutine. ctx cancel already woke
			// it via the select; the wait is only here for the narrow
			// case where a signal and the cancel race.
			return waitGroup(shutdownCtx, &a.messagesReloadWG, "messages reload")
		},
	}
}

func (a *App) httpComponent() namedComponent {
	return namedComponent{
		name: "http",
		start: func(ctx context.Context) error {
			a.logBanner()
			a.httpWG.Add(1)
			go func() {
				defer a.httpWG.Done()
				if err := a.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					a.logger.Error("http server", "err", err)
				}
			}()
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			if err := a.httpSrv.Shutdown(shutdownCtx); err != nil {
				a.logger.Warn("http shutdown", "err", err)
			}
			return waitGroup(shutdownCtx, &a.httpWG, "http server")
		},
	}
}

// logBanner writes one or more "web UI available" log lines at startup.
// For a wildcard bind (0.0.0.0/::) it enumerates every usable interface
// address so operators see real, clickable URLs rather than "0.0.0.0".
func (a *App) logBanner() {
	scheme := "http"
	host, port, _ := net.SplitHostPort(a.cfg.HTTPAddr)
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		if ifaces, err := net.Interfaces(); err == nil {
			for _, iface := range ifaces {
				if iface.Flags&net.FlagUp == 0 {
					continue
				}
				addrs, err := iface.Addrs()
				if err != nil {
					continue
				}
				for _, addr := range addrs {
					ipNet, ok := addr.(*net.IPNet)
					if !ok {
						continue
					}
					ifIP := ipNet.IP
					if ifIP.IsLoopback() || ifIP.IsLinkLocalMulticast() || ifIP.IsLinkLocalUnicast() {
						continue
					}
					url := net.JoinHostPort(ifIP.String(), port)
					a.logger.Info("web UI available", "url", fmt.Sprintf("%s://%s", scheme, url), "iface", iface.Name)
				}
			}
		}
		a.logger.Info("web UI available", "url", fmt.Sprintf("%s://127.0.0.1:%s", scheme, port), "iface", "lo")
		return
	}
	a.logger.Info("web UI available", "url", fmt.Sprintf("%s://%s", scheme, a.cfg.HTTPAddr))
}

// waitGroup blocks until wg signals Done or shutdownCtx fires. On
// timeout it logs a warning and returns a descriptive error, but does
// not panic — the parent Stop loop continues to the next component so
// one stuck goroutine cannot strand everything else.
func waitGroup(shutdownCtx context.Context, wg interface{ Wait() }, name string) error {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-shutdownCtx.Done():
		return fmt.Errorf("%s: shutdown timed out", name)
	}
}
