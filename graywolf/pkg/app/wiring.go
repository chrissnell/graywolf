package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/agw"
	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/digipeater"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/igate"
	"github.com/chrissnell/graywolf/pkg/igate/filters"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/modembridge"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
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
	a.plog = packetlog.New(packetlog.Config{Capacity: 2000, MaxAge: 30 * time.Minute})

	// --- Modem bridge (construction; Start happens later) --------------
	a.bridge = modembridge.New(modembridge.Config{
		BinaryPath: resolvedModem,
		Store:      a.store,
		Metrics:    a.metrics,
		Logger:     a.logger,
	})

	// --- TX governor ---------------------------------------------------
	txSender := func(tf *pb.TransmitFrame) error {
		if err := a.bridge.SendTransmitFrame(tf); err != nil {
			return err
		}
		a.metrics.ObserveTxFrame(tf.Channel)
		return nil
	}

	// Load per-channel timing and rate limits from configstore. A store
	// error is not fatal: the governor just runs with empty defaults.
	channelTimings := make(map[uint32]txgovernor.ChannelTiming)
	var rate1, rate5 int
	if timings, err := a.store.ListTxTimings(ctx); err == nil {
		for _, t := range timings {
			channelTimings[t.Channel] = txgovernor.ChannelTiming{
				TxDelayMs: t.TxDelayMs,
				TxTailMs:  t.TxTailMs,
				SlotTime:  time.Duration(t.SlotMs) * time.Millisecond,
				Persist:   uint8(t.Persist),
				FullDup:   t.FullDup,
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

	// TX hook: record transmitted frames into the packet log.
	plog := a.plog
	a.gov.SetTxHook(func(channel uint32, frame *ax25.Frame, source txgovernor.SubmitSource) {
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
	})

	// --- KISS manager --------------------------------------------------
	a.kissMgr = kiss.NewManager(kiss.ManagerConfig{
		Sink:          a.gov,
		Logger:        a.logger,
		OnDecodeError: a.metrics.KissDecodeErrors.Inc,
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

	// --- iGate (optional) ----------------------------------------------
	if err := a.wireIGate(ctx); err != nil {
		return err
	}
	if a.ig != nil {
		a.beaconSched.SetISSink(a.ig)
	}

	// --- AGW server (optional) -----------------------------------------
	if err := a.wireAGW(ctx); err != nil {
		return err
	}

	// --- APRS fan-out queue (consumed from the bridge component) -------
	a.aprsQueue = make(chan *aprs.DecodedAPRSPacket, 256)

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
		a.governorComponent(),
		a.backgroundStatsComponent(),
		a.kissComponent(),
		a.digipeaterComponent(),
		a.gpsComponent(),
		a.beaconComponent(),
		a.bridgeComponent(),
		a.agwComponent(),
		a.igateComponent(),
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
			Name: "FLAC Test", InputDeviceID: devs[0].ID,
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
// handles via a nil-check.
func (a *App) wireIGate(ctx context.Context) error {
	igCfg, err := a.store.GetIGateConfig(ctx)
	if err != nil || igCfg == nil || !igCfg.Enabled {
		return nil
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
	var igGov *txgovernor.Governor
	if igCfg.GateIsToRf {
		igGov = a.gov
	}

	ig, err := igate.New(igate.Config{
		Server:          serverAddr,
		Callsign:        igCfg.Callsign,
		Passcode:        igCfg.Passcode,
		ServerFilter:    igCfg.ServerFilter,
		SoftwareName:    igCfg.SoftwareName,
		SoftwareVersion: igCfg.SoftwareVersion,
		Rules:           rules,
		TxChannel:       igCfg.TxChannel,
		Governor:        igGov,
		SimulationMode:  igCfg.SimulationMode,
		Logger:          a.logger,
		Registry:        a.metrics.Registry,
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
		},
	})
	if err != nil {
		// Matches the old main.go behavior: init failure is logged but
		// does not take out the whole app. The iGate just stays nil.
		a.logger.Error("igate init", "err", err)
		return nil
	}
	a.ig = ig
	return nil
}

// wireAGW constructs a.agwServer from configstore. A disabled or
// missing AGW config leaves a.agwServer nil.
func (a *App) wireAGW(ctx context.Context) error {
	agwCfg, err := a.store.GetAgwConfig(ctx)
	if err != nil || agwCfg == nil || !agwCfg.Enabled {
		return nil
	}

	calls := strings.Split(agwCfg.Callsigns, ",")
	for i := range calls {
		calls[i] = strings.TrimSpace(calls[i])
	}
	a.agwServer = agw.NewServer(agw.ServerConfig{
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
	return nil
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
		Store:       a.store,
		Bridge:      a.bridge,
		KissManager: a.kissMgr,
		KissCtx:     ctx,
		Logger:      a.logger,
	})
	if err != nil {
		return fmt.Errorf("webapi new: %w", err)
	}
	a.apiSrv = apiSrv

	if a.ig != nil {
		apiSrv.SetIgateStatusFn(a.ig.Status)
	}
	apiSrv.SetGPSReload(a.gpsReload)
	apiSrv.SetBeaconReload(a.beaconReload)
	apiSrv.SetBeaconSendNow(a.beaconSched.SendNow)
	apiSrv.SetDigipeaterReload(a.digipeaterReload)

	version := a.cfg.Version
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"version":%q}`, version)
	})

	authHandlers := &webauth.Handlers{Auth: a.authStore, Secure: secure, Logger: a.logger}
	mux.HandleFunc("/api/auth/login", authHandlers.HandleLogin)
	mux.HandleFunc("/api/auth/logout", authHandlers.HandleLogout)
	mux.HandleFunc("/api/auth/setup", authHandlers.HandleSetup)

	apiMux := http.NewServeMux()
	apiSrv.RegisterRoutes(apiMux)
	webapi.RegisterPackets(apiSrv, a.plog, a.gpsCache)(apiMux)
	webapi.RegisterPosition(apiSrv, a.gpsCache, apiMux)
	if a.ig != nil {
		webapi.RegisterIgate(apiSrv, apiMux, a.ig.SetSimulationMode, a.ig.Status)
	}

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

func (a *App) kissComponent() namedComponent {
	return namedComponent{
		name: "kiss",
		start: func(ctx context.Context) error {
			kissIfaces, _ := a.store.ListKissInterfaces(ctx)
			for _, ki := range kissIfaces {
				if !ki.Enabled || ki.InterfaceType != "tcp" || ki.ListenAddr == "" {
					continue
				}
				ch := ki.Channel
				if ch == 0 {
					ch = 1
				}
				name := ki.Name
				a.kissMgr.Start(ctx, ki.ID, kiss.ServerConfig{
					Name:       name,
					ListenAddr: ki.ListenAddr,
					Logger:     a.logger,
					ChannelMap: map[uint8]uint32{0: ch},
					Broadcast:  ki.Broadcast,
					OnClientChange: func(n int) {
						a.metrics.SetKissClients(name, n)
					},
				})
			}
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			// kiss.Manager's goroutines are cancelled by the ctx passed
			// to Start; there is no explicit Stop. Nothing to wait on
			// here that is not already covered by other components.
			return nil
		},
	}
}

func (a *App) digipeaterComponent() namedComponent {
	reload := func(ctx context.Context) {
		cfg, err := a.store.GetDigipeaterConfig(ctx)
		if err != nil || cfg == nil {
			a.digi.SetEnabled(false)
			a.digi.SetRules(nil)
			return
		}
		mycall, _ := ax25.ParseAddress(cfg.MyCall)
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
	loadBeaconConfigs := func(ctx context.Context) []beacon.Config {
		stored, err := a.store.ListBeacons(ctx)
		if err != nil {
			a.logger.Warn("beacon load", "err", err)
			return nil
		}
		var configs []beacon.Config
		for _, b := range stored {
			bc, err := beaconConfigFromStore(b)
			if err != nil {
				a.logger.Warn("beacon config", "id", b.ID, "err", err)
				continue
			}
			configs = append(configs, bc)
		}
		return configs
	}

	return namedComponent{
		name: "beacon",
		start: func(ctx context.Context) error {
			a.beaconSched.SetBeacons(loadBeaconConfigs(ctx))
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
				for {
					select {
					case <-ctx.Done():
						return
					case <-a.beaconReload:
						a.beaconSched.Reload(loadBeaconConfigs(ctx))
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

			a.fanOutWG.Add(1)
			go func() {
				defer a.fanOutWG.Done()
				var igOut aprs.PacketOutput
				if igateOut != nil {
					igOut = igateOut
				}
				runAPRSFanOut(ctx, a.aprsQueue, aprsOut, igOut)
			}()

			a.frameConsumerWG.Add(1)
			go func() {
				defer a.frameConsumerWG.Done()
				// Closing aprsQueue unblocks the fan-out goroutine once
				// the frame consumer exits, which happens when
				// bridge.Frames() closes (i.e., bridge.Stop ran).
				defer close(a.aprsQueue)
				for rf := range a.bridge.Frames() {
					if rf == nil {
						continue
					}
					// KISS broadcast to all interfaces.
					a.kissMgr.BroadcastFromChannel(rf.Channel, rf.Data)

					f, err := ax25.Decode(rf.Data)
					if err != nil {
						// Undecoded frame — record raw only.
						a.plog.Record(packetlog.Entry{
							Channel:   rf.Channel,
							Direction: packetlog.DirRX,
							Source:    "modem",
							Raw:       rf.Data,
						})
						continue
					}

					e := packetlog.Entry{
						Channel:   rf.Channel,
						Direction: packetlog.DirRX,
						Source:    "modem",
						Raw:       rf.Data,
						Display:   f.String(),
					}

					if f.IsUI() {
						if a.agwServer != nil {
							a.agwServer.BroadcastMonitoredUI(uint8(rf.Channel), f)
						}
						a.digi.Handle(ctx, rf.Channel, f)
						if pkt, err := aprs.Parse(f); err == nil && pkt != nil {
							pkt.Channel = int(rf.Channel)
							e.Type = string(pkt.Type)
							e.Decoded = pkt
							aprsSubmit.submit(pkt)
						}
					}
					a.plog.Record(e)
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

			// 2. Frame consumer exits when bridge.Frames() closes,
			//    which happens inside bridge.Stop. It then closes the
			//    APRS queue.
			if err := waitGroup(shutdownCtx, &a.frameConsumerWG, "frame consumer"); err != nil {
				return err
			}
			// 3. Fan-out drains the APRS queue and exits.
			return waitGroup(shutdownCtx, &a.fanOutWG, "aprs fan-out")
		},
	}
}

func (a *App) agwComponent() namedComponent {
	return namedComponent{
		name: "agw",
		start: func(ctx context.Context) error {
			if a.agwServer == nil {
				return nil
			}
			a.agwWG.Add(1)
			go func() {
				defer a.agwWG.Done()
				if err := a.agwServer.ListenAndServe(ctx); err != nil {
					a.logger.Error("agw server", "err", err)
				}
			}()
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			if a.agwServer == nil {
				return nil
			}
			// Shutdown closes live connections and the listener port
			// so a quick restart is safe even before ListenAndServe
			// observes ctx cancel.
			if err := a.agwServer.Shutdown(shutdownCtx); err != nil {
				a.logger.Warn("agw server shutdown", "err", err)
			}
			return waitGroup(shutdownCtx, &a.agwWG, "agw server")
		},
	}
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
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			if a.ig == nil {
				return nil
			}
			a.ig.Stop()
			return nil
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
