// graywolf entry point: configstore, modembridge, metrics, KISS/AGW,
// digipeater, iGate, GPS, beacon scheduler, packet log, REST API,
// embedded web UI, and web authentication.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	// Handle subcommands before flag parsing.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "auth":
			handleAuthSubcommand(os.Args[2:])
			return
		case "version":
			fmt.Println(Version)
			return
		}
	}

	var (
		dbPath     = flag.String("config", "./graywolf.db", "path to SQLite config database")
		modemPath  = flag.String("modem", "./target/release/graywolf-modem", "path to graywolf-modem binary")
		httpAddr   = flag.String("http", "127.0.0.1:8080", "HTTP listen address")
		shutdownTO = flag.Duration("shutdown-timeout", 10*time.Second, "max time to wait for clean shutdown")
		flacFile   = flag.String("flac", "", "override audio device with a FLAC file for testing")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	store, err := configstore.Open(*dbPath)
	if err != nil {
		logger.Error("open configstore", "err", err, "path", *dbPath)
		os.Exit(1)
	}
	defer store.Close()

	// -flac flag: override (or create) the first audio device to use a FLAC file,
	// and ensure at least one channel references it.
	if *flacFile != "" {
		absPath, err := filepath.Abs(*flacFile)
		if err != nil {
			logger.Error("resolve flac path", "err", err)
			os.Exit(1)
		}
		if _, err := os.Stat(absPath); err != nil {
			logger.Error("flac file not found", "path", absPath)
			os.Exit(1)
		}
		devs, _ := store.ListAudioDevices()
		if len(devs) == 0 {
			dev := &configstore.AudioDevice{
				Name: "FLAC Input", Direction: "input",
				SourceType: "flac", SourcePath: absPath,
				SampleRate: 44100, Channels: 1, Format: "s16le",
			}
			if err := store.CreateAudioDevice(dev); err != nil {
				logger.Error("create flac audio device", "err", err)
				os.Exit(1)
			}
			devs = []configstore.AudioDevice{*dev}
		} else {
			devs[0].SourceType = "flac"
			devs[0].SourcePath = absPath
			devs[0].SampleRate = 44100
			if err := store.UpdateAudioDevice(&devs[0]); err != nil {
				logger.Error("update audio device for flac", "err", err)
				os.Exit(1)
			}
		}
		logger.Info("audio device overridden", "source", "flac", "path", absPath)

		// Ensure at least one channel exists so the FLAC source gets used.
		chs, _ := store.ListChannels()
		if len(chs) == 0 {
			ch := &configstore.Channel{
				Name: "FLAC Test", InputDeviceID: devs[0].ID,
				ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
			}
			if err := store.CreateChannel(ch); err != nil {
				logger.Error("create default channel for flac", "err", err)
				os.Exit(1)
			}
			logger.Info("created default channel for flac input", "device_id", devs[0].ID)
		}
	}

	m := metrics.New()

	// --- Modem bridge ---------------------------------------------------
	bridge := modembridge.New(modembridge.Config{
		BinaryPath: *modemPath,
		Store:      store,
		Metrics:    m,
		Logger:     logger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bridge.Start(ctx); err != nil {
		logger.Error("start modembridge", "err", err)
		os.Exit(1)
	}

	// --- Packet log -----------------------------------------------------
	plog := packetlog.New(packetlog.Config{Capacity: 2000, MaxAge: 30 * time.Minute})

	// RX hook: record every received frame into the packet log.
	// This is a fast pre-record for frames that may not decode as AX.25;
	// successfully decoded frames get a richer entry in the fan-out below.
	bridge.SetRxHook(func(rf *pb.ReceivedFrame) {
		// no-op: recording moved to fan-out goroutine for richer data
	})

	// --- TX governor ----------------------------------------------------
	txSender := func(tf *pb.TransmitFrame) error {
		if err := bridge.SendTransmitFrame(tf); err != nil {
			return err
		}
		m.ObserveTxFrame(tf.Channel)
		return nil
	}

	// Load per-channel timing from configstore.
	channelTimings := make(map[uint32]txgovernor.ChannelTiming)
	if timings, err := store.ListTxTimings(); err == nil {
		for _, t := range timings {
			channelTimings[t.Channel] = txgovernor.ChannelTiming{
				TxDelayMs: t.TxDelayMs,
				TxTailMs:  t.TxTailMs,
				SlotTime:  time.Duration(t.SlotMs) * time.Millisecond,
				Persist:   uint8(t.Persist),
				FullDup:   t.FullDup,
			}
		}
	}

	// Rate limits from timing rows (first one wins as global default).
	var rate1, rate5 int
	if timings, err := store.ListTxTimings(); err == nil {
		for _, t := range timings {
			if t.Rate1Min > 0 && rate1 == 0 {
				rate1 = int(t.Rate1Min)
			}
			if t.Rate5Min > 0 && rate5 == 0 {
				rate5 = int(t.Rate5Min)
			}
		}
	}

	gov := txgovernor.New(txgovernor.Config{
		Sender:        txSender,
		DcdEvents:     bridge.DcdEvents(),
		Rate1MinLimit: rate1,
		Rate5MinLimit: rate5,
		DedupWindow:   30 * time.Second,
		Channels:      channelTimings,
		Logger:        logger,
	})

	// TX hook: record transmitted frames into the packet log.
	gov.SetTxHook(func(channel uint32, frame *ax25.Frame, source txgovernor.SubmitSource) {
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

	go func() {
		if err := gov.Run(ctx); err != nil {
			logger.Error("tx governor", "err", err)
		}
	}()

	// Governor stats → Prometheus.
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		var prev txgovernor.Stats
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s := gov.Stats()
				if d := s.RateLimited - prev.RateLimited; d > 0 {
					for i := uint64(0); i < d; i++ {
						m.TxRateLimited.Inc()
					}
				}
				if d := s.Deduped - prev.Deduped; d > 0 {
					for i := uint64(0); i < d; i++ {
						m.TxDeduped.Inc()
					}
				}
				if d := s.QueueDropped - prev.QueueDropped; d > 0 {
					for i := uint64(0); i < d; i++ {
						m.TxQueueDropped.Inc()
					}
				}
				prev = s
			}
		}
	}()

	// Packetlog gauge ticker.
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.PacketlogEntries.Set(float64(plog.Len()))
			}
		}
	}()

	// --- KISS TCP servers (from configstore) ----------------------------
	kissMgr := kiss.NewManager(kiss.ManagerConfig{
		Sink:   &kissSinkAdapter{gov: gov},
		Logger: logger,
	})
	kissIfaces, _ := store.ListKissInterfaces()
	for _, ki := range kissIfaces {
		if !ki.Enabled || ki.InterfaceType != "tcp" || ki.ListenAddr == "" {
			continue
		}
		ch := ki.Channel
		if ch == 0 {
			ch = 1
		}
		name := ki.Name
		kissMgr.Start(ctx, ki.ID, kiss.ServerConfig{
			Name:       name,
			ListenAddr: ki.ListenAddr,
			Logger:     logger,
			ChannelMap: map[uint8]uint32{0: ch},
			Broadcast:  ki.Broadcast,
			OnClientChange: func(n int) {
				m.SetKissClients(name, n)
			},
		})
	}

	// --- AGW TCP server (from configstore) ------------------------------
	var agwServer *agw.Server
	if agwCfg, err := store.GetAgwConfig(); err == nil && agwCfg != nil && agwCfg.Enabled {
		calls := strings.Split(agwCfg.Callsigns, ",")
		for i := range calls {
			calls[i] = strings.TrimSpace(calls[i])
		}
		agwServer = agw.NewServer(agw.ServerConfig{
			ListenAddr:    agwCfg.ListenAddr,
			PortCallsigns: calls,
			PortToChannel: map[uint8]uint32{0: 1},
			Sink:          &agwSinkAdapter{gov: gov},
			Logger:        logger,
			OnClientChange: func(n int) {
				m.SetAgwClients(n)
			},
		})
		go func() {
			if err := agwServer.ListenAndServe(ctx); err != nil {
				logger.Error("agw server", "err", err)
			}
		}()
	}

	// --- Digipeater -----------------------------------------------------
	var digi *digipeater.Digipeater
	if digiCfg, err := store.GetDigipeaterConfig(); err == nil && digiCfg != nil && digiCfg.Enabled {
		digiRules, _ := store.ListDigipeaterRules()
		mycall, _ := ax25.ParseAddress(digiCfg.MyCall)
		digi, err = digipeater.New(digipeater.Config{
			MyCall:       mycall,
			DedupeWindow: time.Duration(digiCfg.DedupeWindowSeconds) * time.Second,
			Rules:        digipeater.RulesFromStore(digiRules),
			Submit:       gov.Submit,
			Logger:       logger,
			OnPacket: func(note string, fromChan, toChan uint32, f *ax25.Frame) {
				m.DigipeaterPackets.Inc()
				plog.Record(packetlog.Entry{
					Channel:   toChan,
					Direction: packetlog.DirTX,
					Source:    "digipeater",
					Display:   f.String(),
					Notes:     note,
				})
			},
			OnDedup: func() { m.DigipeaterDeduped.Inc() },
		})
		if err != nil {
			logger.Error("digipeater init", "err", err)
		}
	}

	// --- GPS cache + reader ---------------------------------------------
	gpsCache := gps.NewMemCache()
	if gpsCfg, err := store.GetGPSConfig(); err == nil && gpsCfg != nil && gpsCfg.Enabled {
		switch gpsCfg.SourceType {
		case "serial":
			go func() {
				for {
					err := gps.RunSerial(ctx, gps.SerialConfig{
						Device:   gpsCfg.Device,
						BaudRate: int(gpsCfg.BaudRate),
					}, gpsCache, logger)
					if ctx.Err() != nil {
						return
					}
					logger.Warn("gps serial reader exited", "err", err)
					time.Sleep(5 * time.Second)
				}
			}()
		case "gpsd":
			go func() {
				for {
					err := gps.RunGPSD(ctx, gps.GPSDConfig{
						Host: gpsCfg.GpsdHost,
						Port: int(gpsCfg.GpsdPort),
					}, gpsCache, logger)
					if ctx.Err() != nil {
						return
					}
					logger.Warn("gpsd reader exited", "err", err)
					time.Sleep(5 * time.Second)
				}
			}()
		}
	}

	// --- Beacon scheduler -----------------------------------------------
	beaconSched, err := beacon.New(beacon.Options{
		Sink:     &beaconSinkAdapter{gov: gov},
		Cache:    gpsCache,
		Logger:   logger,
		Observer: &beaconObserver{m: m},
	})
	if err != nil {
		logger.Error("beacon scheduler init", "err", err)
		os.Exit(1)
	}
	if beacons, err := store.ListBeacons(); err == nil && len(beacons) > 0 {
		var configs []beacon.Config
		for _, b := range beacons {
			if !b.Enabled {
				continue
			}
			bc, err := beaconConfigFromStore(b)
			if err != nil {
				logger.Warn("beacon config", "id", b.ID, "err", err)
				continue
			}
			configs = append(configs, bc)
		}
		beaconSched.SetBeacons(configs)
		go func() {
			if err := beaconSched.Run(ctx); err != nil {
				logger.Error("beacon scheduler", "err", err)
			}
		}()
	}

	// --- iGate ----------------------------------------------------------
	var ig *igate.Igate
	if igCfg, err := store.GetIGateConfig(); err == nil && igCfg != nil && igCfg.Enabled {
		rfFilters, _ := store.ListIGateRfFilters()
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
			igGov = gov
		}

		ig, err = igate.New(igate.Config{
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
			Logger:          logger,
			Registry:        m.Registry,
		})
		if err != nil {
			logger.Error("igate init", "err", err)
		} else {
			if err := ig.Start(ctx); err != nil {
				logger.Error("igate start", "err", err)
			}
		}
	}

	// iGate output adapter for the APRS fan-out.
	var igateOut *igate.IgateOutput
	if ig != nil {
		igateOut = igate.NewIgateOutput(ig)
	}

	// --- APRS decode + log output ---------------------------------------
	aprsOut := aprs.NewLogOutput(logger)

	aprsQueue := make(chan *aprs.DecodedAPRSPacket, 256)
	go func() {
		for pkt := range aprsQueue {
			_ = aprsOut.SendPacket(ctx, pkt)
			if igateOut != nil {
				_ = igateOut.SendPacket(ctx, pkt)
			}
		}
	}()

	// --- RX fan-out: modem → KISS broadcast + AGW monitor + digi + APRS log + packet log
	go func() {
		for rf := range bridge.Frames() {
			if rf == nil {
				continue
			}
			// KISS broadcast to all interfaces.
			kissMgr.BroadcastFromChannel(rf.Channel, rf.Data)

			f, err := ax25.Decode(rf.Data)
			if err != nil {
				// Undecoded frame — record raw only.
				plog.Record(packetlog.Entry{
					Channel:   rf.Channel,
					Direction: packetlog.DirRX,
					Source:    "modem",
					Raw:       rf.Data,
				})
				continue
			}

			// Build packet log entry with decoded callsigns.
			e := packetlog.Entry{
				Channel:   rf.Channel,
				Direction: packetlog.DirRX,
				Source:    "modem",
				Raw:       rf.Data,
				Display:   f.String(),
			}

			// AGW monitor.
			if f.IsUI() {
				if agwServer != nil {
					agwServer.BroadcastMonitoredUI(uint8(rf.Channel), f)
				}

				// Digipeater.
				if digi != nil {
					digi.Handle(ctx, rf.Channel, f)
				}

				// APRS parse → fan-out.
				if pkt, err := aprs.Parse(f); err == nil && pkt != nil {
					pkt.Channel = int(rf.Channel)
					e.Type = string(pkt.Type)
					e.Decoded = pkt
					select {
					case aprsQueue <- pkt:
					default:
						select {
						case <-aprsQueue:
							m.AprsOutDropped.Inc()
						default:
						}
						select {
						case aprsQueue <- pkt:
						default:
							m.AprsOutDropped.Inc()
						}
					}
				}
			}

			plog.Record(e)
		}
	}()

	// --- Auth -----------------------------------------------------------
	authStore, err := webauth.NewAuthStore(store.DB())
	if err != nil {
		logger.Error("init auth store", "err", err)
		os.Exit(1)
	}

	// Warn if binding to non-loopback address.
	// Secure cookies require HTTPS; since we don't support TLS, always false.
	secure := false
	host, _, _ := net.SplitHostPort(*httpAddr)
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		logger.Warn(fmt.Sprintf("Web server binding to %s — accessible from all network interfaces", *httpAddr))
	}

	// --- HTTP -----------------------------------------------------------
	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())

	apiSrv, err := webapi.NewServer(webapi.Config{Store: store, Bridge: bridge, KissManager: kissMgr, KissCtx: ctx, Logger: logger})
	if err != nil {
		logger.Error("webapi new", "err", err)
		os.Exit(1)
	}

	// Wire igate status into /api/status for dashboard.
	if ig != nil {
		apiSrv.SetIgateStatusFn(func() webapi.IgateStatus {
			s := ig.Status()
			return webapi.IgateStatus{
				Connected:      s.Connected,
				Server:         s.Server,
				Callsign:       s.Callsign,
				SimulationMode: s.SimulationMode,
				LastConnected:  s.LastConnected,
				Gated:          s.Gated,
				Downlinked:     s.Downlinked,
				Filtered:       s.Filtered,
				DroppedOffline: s.DroppedOffline,
			}
		})
	}

	// Public endpoints (no auth).
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"version":%q}`, Version)
	})

	authHandlers := &webauth.Handlers{Auth: authStore, Secure: secure}
	mux.HandleFunc("/api/auth/login", authHandlers.HandleLogin)
	mux.HandleFunc("/api/auth/logout", authHandlers.HandleLogout)
	mux.HandleFunc("/api/auth/setup", authHandlers.HandleSetup)

	// Protected API routes — all existing registrations go on a sub-mux
	// wrapped by auth middleware.
	apiMux := http.NewServeMux()
	apiSrv.RegisterRoutes(apiMux)

	// Phase 4 real endpoint registrations (replace stubs).
	webapi.RegisterPackets(apiSrv, plog)(apiMux)
	webapi.RegisterPosition(apiSrv, gpsCache, apiMux)
	if ig != nil {
		webapi.RegisterIgate(apiSrv, apiMux,
			ig.SetSimulationMode,
			func() webapi.IgateStatus {
				s := ig.Status()
				return webapi.IgateStatus{
					Connected:      s.Connected,
					Server:         s.Server,
					Callsign:       s.Callsign,
					SimulationMode: s.SimulationMode,
					LastConnected:  s.LastConnected,
					Gated:          s.Gated,
					Downlinked:     s.Downlinked,
					Filtered:       s.Filtered,
					DroppedOffline: s.DroppedOffline,
				}
			},
		)
	}

	mux.Handle("/api/", webauth.RequireAuth(authStore)(apiMux))

	// Embedded Svelte UI with SPA history-mode rewriting.
	mux.Handle("/", web.SPAHandler())

	httpSrv := &http.Server{Addr: *httpAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		scheme := "http"
		host, port, _ := net.SplitHostPort(*httpAddr)
		if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
			// List reachable URLs for every interface address.
			if ifaces, err := net.Interfaces(); err == nil {
				for _, iface := range ifaces {
					if iface.Flags&net.FlagUp == 0 {
						continue
					}
					addrs, err := iface.Addrs()
					if err != nil {
						continue
					}
					for _, a := range addrs {
						ipNet, ok := a.(*net.IPNet)
						if !ok {
							continue
						}
						ifIP := ipNet.IP
						if ifIP.IsLoopback() || ifIP.IsLinkLocalMulticast() || ifIP.IsLinkLocalUnicast() {
							continue
						}
						addr := net.JoinHostPort(ifIP.String(), port)
						logger.Info("web UI available", "url", fmt.Sprintf("%s://%s", scheme, addr), "iface", iface.Name)
					}
				}
			}
			logger.Info("web UI available", "url", fmt.Sprintf("%s://127.0.0.1:%s", scheme, port), "iface", "lo")
		} else {
			logger.Info("web UI available", "url", fmt.Sprintf("%s://%s", scheme, *httpAddr))
		}
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "err", err)
		}
	}()

	// --- Shutdown -------------------------------------------------------
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Info("shutdown signal received", "signal", sig.String())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), *shutdownTO)
	defer shutdownCancel()

	cancel() // signal all goroutines

	if ig != nil {
		ig.Stop()
	}

	done := make(chan struct{})
	go func() { bridge.Stop(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		logger.Warn("modembridge shutdown timed out")
	}

	_ = httpSrv.Shutdown(shutdownCtx)
}

// --- Sink adapters --------------------------------------------------------

// kissSinkAdapter bridges kiss.TxSink to txgovernor.Submit.
type kissSinkAdapter struct{ gov *txgovernor.Governor }

func (a *kissSinkAdapter) Submit(ctx context.Context, channel uint32, f *ax25.Frame, s kiss.SubmitSource) error {
	return a.gov.Submit(ctx, channel, f, txgovernor.SubmitSource{
		Kind: s.Kind, Detail: s.Detail, Priority: s.Priority,
	})
}

// agwSinkAdapter bridges agw.TxSink to txgovernor.Submit.
type agwSinkAdapter struct{ gov *txgovernor.Governor }

func (a *agwSinkAdapter) Submit(ctx context.Context, channel uint32, f *ax25.Frame, s agw.SubmitSource) error {
	return a.gov.Submit(ctx, channel, f, txgovernor.SubmitSource{
		Kind: s.Kind, Detail: s.Detail, Priority: s.Priority,
	})
}

// beaconSinkAdapter bridges beacon.TxSink to txgovernor.Submit.
type beaconSinkAdapter struct{ gov *txgovernor.Governor }

func (a *beaconSinkAdapter) Submit(ctx context.Context, channel uint32, f *ax25.Frame, s beacon.SubmitSource) error {
	return a.gov.Submit(ctx, channel, f, txgovernor.SubmitSource{
		Kind: s.Kind, Detail: s.Detail, Priority: s.Priority,
	})
}

// --- Beacon observer for metrics ------------------------------------------

type beaconObserver struct{ m *metrics.Metrics }

func (o *beaconObserver) OnBeaconSent(t beacon.Type) {
	o.m.BeaconPackets.WithLabelValues(string(t)).Inc()
}

func (o *beaconObserver) OnSmartBeaconRate(channel uint32, interval time.Duration) {
	o.m.SmartBeaconRate.WithLabelValues(strconv.FormatUint(uint64(channel), 10)).Set(interval.Seconds())
}

// --- Config mapping helpers -----------------------------------------------

func beaconConfigFromStore(b configstore.Beacon) (beacon.Config, error) {
	src, err := ax25.ParseAddress(b.Callsign)
	if err != nil {
		return beacon.Config{}, fmt.Errorf("parse callsign %q: %w", b.Callsign, err)
	}
	dest, err := ax25.ParseAddress(b.Destination)
	if err != nil {
		return beacon.Config{}, fmt.Errorf("parse destination %q: %w", b.Destination, err)
	}
	var path []ax25.Address
	for _, p := range strings.Split(b.Path, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		a, err := ax25.ParseAddress(p)
		if err != nil {
			return beacon.Config{}, fmt.Errorf("parse path %q: %w", p, err)
		}
		path = append(path, a)
	}

	var commentCmd []string
	if b.CommentCmd != "" {
		argv, err := beacon.SplitArgv(b.CommentCmd)
		if err != nil {
			return beacon.Config{}, fmt.Errorf("split comment_cmd: %w", err)
		}
		commentCmd = argv
	}

	symTable := byte('/')
	if len(b.SymbolTable) > 0 {
		symTable = b.SymbolTable[0]
	}
	symCode := byte('-')
	if len(b.Symbol) > 0 {
		symCode = b.Symbol[0]
	}

	cfg := beacon.Config{
		ID:          b.ID,
		Type:        beacon.Type(b.Type),
		Channel:     b.Channel,
		Source:      src,
		Dest:        dest,
		Path:        path,
		Delay:       time.Duration(b.DelaySeconds) * time.Second,
		Every:       time.Duration(b.EverySeconds) * time.Second,
		Slot:        int(b.SlotSeconds),
		Lat:         b.Latitude,
		Lon:         b.Longitude,
		AltFt:       b.AltFt,
		SymbolTable: symTable,
		SymbolCode:  symCode,
		Comment:     b.Comment,
		CommentCmd:  commentCmd,
		Messaging:   b.Messaging,
		ObjectName:  b.ObjectName,
		CustomInfo:  b.CustomInfo,
		Enabled:     b.Enabled,
	}

	if b.SmartBeacon {
		cfg.SmartBeacon = &beacon.SmartBeaconConfig{
			Enabled:   true,
			FastSpeed: float64(b.SbFastSpeed),
			SlowSpeed: float64(b.SbSlowSpeed),
			FastRate:  time.Duration(b.SbFastRate) * time.Second,
			SlowRate:  time.Duration(b.SbSlowRate) * time.Second,
			TurnAngle: float64(b.SbTurnAngle),
			TurnSlope: float64(b.SbTurnSlope),
			TurnTime:  time.Duration(b.SbMinTurnTime) * time.Second,
		}
	}

	return cfg, nil
}

// --- Auth subcommand ----------------------------------------------------------

// handleAuthSubcommand dispatches graywolf auth {set-password,list-users,delete-user}.
func handleAuthSubcommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: graywolf auth {set-password|list-users|delete-user} [options]")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("auth", flag.ExitOnError)
	dbPath := fs.String("config", "./graywolf.db", "path to SQLite config database")

	switch args[0] {
	case "set-password":
		user := ""
		remaining := args[1:]
		for i := 0; i < len(remaining); i++ {
			if remaining[i] == "--user" && i+1 < len(remaining) {
				user = remaining[i+1]
				remaining = append(remaining[:i], remaining[i+2:]...)
				break
			}
			if strings.HasPrefix(remaining[i], "--user=") {
				user = strings.TrimPrefix(remaining[i], "--user=")
				remaining = append(remaining[:i], remaining[i+1:]...)
				break
			}
		}
		fs.Parse(remaining)
		if user == "" {
			user = "admin"
		}

		fmt.Printf("Enter password for %s: ", user)
		var password string
		fmt.Scanln(&password)
		if password == "" {
			fmt.Fprintln(os.Stderr, "error: empty password")
			os.Exit(1)
		}

		store, authStore := openAuthDB(*dbPath)
		defer store.Close()

		hash, err := webauth.HashPassword(password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		ctx := context.Background()
		existing, err := authStore.GetUserByUsername(ctx, user)
		if err != nil {
			if _, err := authStore.CreateUser(ctx, user, hash); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Created user %s\n", user)
		} else {
			existing.PasswordHash = hash
			store.DB().Save(existing)
			fmt.Printf("Updated password for %s\n", user)
		}

	case "list-users":
		fs.Parse(args[1:])
		_, authStore := openAuthDB(*dbPath)

		users, err := authStore.ListUsers(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(users) == 0 {
			fmt.Println("No users configured.")
			return
		}
		fmt.Printf("%-20s %-20s\n", "USERNAME", "CREATED")
		for _, u := range users {
			fmt.Printf("%-20s %-20s\n", u.Username, u.CreatedAt.Format(time.RFC3339))
		}

	case "delete-user":
		fs.Parse(args[1:])
		remaining := fs.Args()
		if len(remaining) == 0 {
			fmt.Fprintln(os.Stderr, "usage: graywolf auth delete-user USERNAME")
			os.Exit(1)
		}
		username := remaining[0]
		_, authStore := openAuthDB(*dbPath)

		if err := authStore.DeleteUser(context.Background(), username); err != nil {
			fmt.Fprintf(os.Stderr, "error deleting %s: %v\n", username, err)
			os.Exit(1)
		}
		fmt.Printf("Deleted user %s\n", username)

	default:
		fmt.Fprintf(os.Stderr, "unknown auth subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func openAuthDB(dbPath string) (*configstore.Store, *webauth.AuthStore) {
	store, err := configstore.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	authStore, err := webauth.NewAuthStore(store.DB())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing auth: %v\n", err)
		os.Exit(1)
	}
	return store, authStore
}
