// graywolf is the Phase 1 Go entry point: it opens the configstore, starts
// the modem bridge, and exposes /metrics on 127.0.0.1:8080.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chrissnell/graywolf/pkg/agw"
	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/modembridge"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"github.com/chrissnell/graywolf/pkg/webapi"
	"github.com/chrissnell/graywolf/web"
)

func main() {
	var (
		dbPath     = flag.String("config", "./graywolf.db", "path to SQLite config database")
		modemPath  = flag.String("modem", "./target/release/graywolf-modem", "path to graywolf-modem binary")
		httpAddr   = flag.String("http", "127.0.0.1:8080", "HTTP listen address for /metrics")
		kissAddr   = flag.String("kiss", "", "KISS TCP listen address, e.g. 0.0.0.0:8001 (empty = disabled)")
		agwAddr    = flag.String("agw", "", "AGWPE TCP listen address, e.g. 0.0.0.0:8000 (empty = disabled)")
		mycall     = flag.String("mycall", "N0CALL", "default station callsign for AGW port info")
		rate1      = flag.Int("tx-rate-1min", 0, "tx governor 1-minute frame rate limit per channel (0=unlimited)")
		rate5      = flag.Int("tx-rate-5min", 0, "tx governor 5-minute frame rate limit per channel (0=unlimited)")
		dedupWin   = flag.Duration("tx-dedup-window", 30*time.Second, "tx dedup window")
		shutdownTO = flag.Duration("shutdown-timeout", 10*time.Second, "max time to wait for clean shutdown")
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

	m := metrics.New()

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

	// --- TX governor ----------------------------------------------------
	txSender := func(tf *pb.TransmitFrame) error {
		if err := bridge.SendTransmitFrame(tf); err != nil {
			return err
		}
		m.ObserveTxFrame(tf.Channel)
		return nil
	}
	gov := txgovernor.New(txgovernor.Config{
		Sender:        txSender,
		DcdEvents:     bridge.DcdEvents(),
		Rate1MinLimit: *rate1,
		Rate5MinLimit: *rate5,
		DedupWindow:   *dedupWin,
		Logger:        logger,
	})
	go func() {
		if err := gov.Run(ctx); err != nil {
			logger.Error("tx governor", "err", err)
		}
	}()
	// Periodically mirror governor stats into Prometheus counters.
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

	// --- KISS TCP server (optional) -------------------------------------
	var kissServer *kiss.Server
	if *kissAddr != "" {
		kissServer = kiss.NewServer(kiss.ServerConfig{
			Name:       "tcp",
			ListenAddr: *kissAddr,
			Sink:       &kissSinkAdapter{gov: gov},
			Logger:     logger,
			ChannelMap: map[uint8]uint32{0: 1},
			OnClientChange: func(n int) {
				m.SetKissClients("tcp", n)
			},
		})
		go func() {
			if err := kissServer.ListenAndServe(ctx); err != nil {
				logger.Error("kiss server", "err", err)
			}
		}()
	}

	// --- AGW TCP server (optional) --------------------------------------
	var agwServer *agw.Server
	if *agwAddr != "" {
		agwServer = agw.NewServer(agw.ServerConfig{
			ListenAddr:    *agwAddr,
			PortCallsigns: []string{*mycall},
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

	// --- APRS decode + log output ---------------------------------------
	aprsOut := aprs.NewLogOutput(logger)

	// Bounded worker queue so a slow PacketOutput (log rotate, syslog
	// stall) never backs up the modem RX goroutine. On full, the oldest
	// packet is dropped and the AprsOutDropped counter is incremented.
	aprsQueue := make(chan *aprs.DecodedAPRSPacket, 256)
	go func() {
		for pkt := range aprsQueue {
			_ = aprsOut.SendPacket(ctx, pkt)
		}
	}()

	// --- RX fan-out: modem → KISS broadcast + AGW monitor + APRS log ----
	// The decoded ax25.Frame is passed to both AGW and aprs.Parse without
	// cloning; both consumers treat it as read-only.
	go func() {
		for rf := range bridge.Frames() {
			if rf == nil {
				continue
			}
			if kissServer != nil {
				kissServer.Broadcast(0, rf.Data)
			}
			f, err := ax25.Decode(rf.Data)
			if err != nil {
				continue
			}
			if f.IsUI() {
				if agwServer != nil {
					agwServer.BroadcastMonitoredUI(uint8(rf.Channel), f)
				}
				if pkt, err := aprs.Parse(f); err == nil && pkt != nil {
					pkt.Channel = int(rf.Channel)
					select {
					case aprsQueue <- pkt:
					default:
						// Drop oldest to make room for the new packet.
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
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())

	// REST API (Phase 3 skeleton; Phase 6 fleshes out write paths).
	apiSrv, err := webapi.NewServer(webapi.Config{Store: store, Logger: logger})
	if err != nil {
		logger.Error("webapi new", "err", err)
		os.Exit(1)
	}
	apiSrv.RegisterRoutes(mux)

	// Embedded Svelte UI at "/"; Phase 6 replaces the placeholder dist.
	mux.Handle("/", web.Handler())

	httpSrv := &http.Server{Addr: *httpAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("http listening", "addr", *httpAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "err", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Info("shutdown signal received", "signal", sig.String())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), *shutdownTO)
	defer shutdownCancel()

	cancel() // tell bridge to begin shutdown
	done := make(chan struct{})
	go func() { bridge.Stop(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		logger.Warn("modembridge shutdown timed out")
	}

	_ = httpSrv.Shutdown(shutdownCtx)
}

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

