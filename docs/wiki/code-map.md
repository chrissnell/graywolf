# Code map

Where to look for a given concern. One section per major component, table
per section. For *what* a piece does in operator terms, follow the
handbook link in the section header; this page only routes you to source.

## Rust modem (`graywolf-modem/`)

Crate name: `graywolf-demod`. Binary: `graywolf-modem`. Source:
[`../../graywolf-modem/src/`](../../graywolf-modem/src/). Handbook:
[`../handbook/audio.html`](../handbook/audio.html), [`../handbook/channels.html`](../handbook/channels.html), [`../handbook/ptt.html`](../handbook/ptt.html).

| Concern | File |
|---|---|
| Daemon entry point | `bin/graywolf_modem.rs` |
| Library re-exports | `lib.rs` |
| AFSK demodulator (Bell 202, profiles A/B) | `demod_afsk.rs` |
| Multi-demod ensemble + dedup | `demod_afsk_multi.rs` |
| 9600 G3RUH modem | `modem_9600/mod.rs` |
| QPSK 2400 / 8-PSK 4800 | `modem_psk/mod.rs` |
| FX.25 RS-coded FEC | `fx25/{mod,rs,tests}.rs` |
| IL2P framing + RS | `il2p/{mod,header,payload,scramble,rs_il2p,tests}.rs` |
| HDLC RX (NRZI, FCS-16, bit-unstuff, fix-bits retry) | `hdlc.rs` |
| HDLC TX bit stream | `tx/hdlc_encode.rs` |
| AFSK modulator (NCO + sine LUT) | `tx/afsk_mod.rs` |
| DSP filters (windowed-sinc, RRC, mark/space) | `dsp.rs`, `filter_buf.rs` |
| Slicer / PLL / DCD state | `state.rs` |
| Constants, enums, modem types | `types.rs` |
| Audio source enum | `audio/mod.rs` |
| CPAL live audio I/O | `audio/soundcard.rs` |
| FLAC test vector playback | `audio/flac.rs` |
| Stdin raw i16 PCM | `audio/stdin_raw.rs` |
| SDR UDP audio bridge | `sdr/mod.rs` |
| Modem orchestration | `modem/mod.rs` |
| TX worker thread (owns sinks + PTT) | `modem/tx_worker.rs` |
| PTT method enum + factory | `tx/ptt.rs` |
| Serial RTS/DTR PTT (Unix `ioctl(TIOCMSET)`) | `tx/ptt_unix.rs` |
| Serial PTT (Windows `EscapeCommFunction`) | `tx/ptt_win.rs` |
| CM108 PTT (Linux `/dev/hidrawN`) | `tx/ptt_cm108_unix.rs` |
| CM108 PTT (macOS hidapi IOKit) | `tx/ptt_cm108_macos.rs` |
| CM108 PTT (Windows hidapi `WriteFile`) | `tx/ptt_cm108_win.rs` |
| GPIO chardev v2 PTT (Linux gpiocdev) | `tx/ptt_gpio_linux.rs` |
| rigctld TCP PTT (`T 1\n` / `T 0\n`) | `tx/ptt_rigctld.rs` |
| CM108 HID enumeration (`--list-cm108`) | `cm108.rs` |
| `--list-audio` JSON enumerator (cpal hosts/devices) | `src/audio/soundcard.rs` (`listing` module), `src/list_audio.rs` |
| `--list-usb` JSON enumerator (nusb tree walk) | `src/list_usb.rs` |
| Modem CLI dispatch + flag handlers | `src/bin/graywolf_modem.rs` |
| IPC framing | `ipc/framing.rs` |
| IPC server (UDS / Windows TCP) | `ipc/server.rs` |
| Generated proto types | `ipc/proto.rs` (re-exports `OUT_DIR/graywolf.rs`) |
| Build script (prost + version env) | `build.rs` |

## Go service: networking & protocol (`pkg/`)

| Package | Purpose | Key files |
|---|---|---|
| `ax25` | AX.25 frame encode/decode. UI fields (`Control`, `PID`, `Info`) and connected-mode fields (`ConnectedControl`, `ConnectedHasInfo`) coexist on one `Frame` so UI senders and `pkg/ax25conn` share a single TX surface; `Encode()` dispatches on `IsConnectedMode()`. | `frame.go`, `address.go`, `priority.go` |
| `ax25conn` | LAPB v2.0 (mod-8) + mod-128 data-link state machine. Outbound-only client; SABME -> DM(F=1) auto-falls-back to SABM. Per-session goroutine over (local, peer, channel); RX dispatched from `pkg/app/rxfanout.go`, TX through `pkg/txgovernor`. CONNECTED state emits an `OutLinkStats` snapshot at 1Hz via `statsTick` for the telemetry side panel. Behavioral edge cases attributed to Linux `net/ax25/` (v6.12) and ax25-tools — per-state kernel-source citations in [`pkg/ax25conn/CREDITS.md`](../../pkg/ax25conn/CREDITS.md). | `session.go`, `manager.go`, `transitions_*.go`, `control.go`, `frame.go`, `timers.go`, `heartbeat.go`, `stats_tick_test.go`, `events.go`, `state.go`, `defaults.go` |
| `ax25termws` | One-bridge-per-WebSocket glue between `pkg/ax25conn.Manager` and the `/api/ax25/terminal` endpoint. JSON envelopes: `connect`, `data`, `disconnect`, `abort`, `transcript_set`, `raw_tail_subscribe`/`raw_tail_unsubscribe` C→S; `state`, `data_rx`, `link_stats`, `error`, `raw_tail` S→C. `data_rx` uses a blocking send so the LAPB window propagates back-pressure into the peer; control envelopes use non-blocking sends with drop+warn. The bridge optionally adapts a `TranscriptRecorder` and a `*packetlog.Log` for raw-tail mode. | `envelope.go`, `bridge.go` |
| `aprs` | APRS info-field parsing (positions, messages, weather, telemetry, mic-e, plus assorted extensions -- see [glossary.md](glossary.md)) | `parse.go`, `position.go`, `mice.go`, `message.go`, `weather.go`, ... |
| `kiss` | KISS framing + TCP server + TCP client + multi-port manager + tx queue + ratelimit | `framing.go`, `server.go`, `client.go`, `manager.go`, `tx_queue.go` |
| `agw` | AGWPE TCP server (direwolf-compatible subset: R/G/g/k/K/m/X/x/y/Y/V) | `server.go`, `protocol.go` |
| `ipcproto` | Generated Go bindings for `proto/graywolf.proto` | `graywolf.pb.go` (regen via `make proto`) |
| `modembridge` | Supervises Rust modem child + IPC state machine + dispatcher + status cache + DCD publisher | `bridge.go`, `supervisor.go`, `ipc_unix.go`, `ipc_windows.go`, `dispatcher.go`, `session.go`, `status_cache.go` |
| `txgovernor` | Centralized TX gate: per-channel rate limits, dedup, priority queue | `governor.go`, `pqueue.go`, `sink.go` |

See [`../handbook/kiss.html`](../handbook/kiss.html), [`../handbook/agwpe.html`](../handbook/agwpe.html), [`../handbook/remote-kiss-tnc.html`](../handbook/remote-kiss-tnc.html).
The TX-funnel rule lives in [invariant 16](invariants.md).

## Go service: APRS features

| Package | Purpose | Handbook |
|---|---|---|
| `beacon` | Position/object/tracker/custom/igate beacon scheduler (min-heap), smart-beacon, encoder | [`../handbook/beacons.html`](../handbook/beacons.html) |
| `digipeater` | WIDEn-N / TRACEn-N digipeater with preemptive digi and per-channel dedup | [`../handbook/digipeater.html`](../handbook/digipeater.html) |
| `igate` | APRS-IS bidirectional gateway: client/login/filter, RF<->IS gating, third-party encap, TNC2 | [`../handbook/igate.html`](../handbook/igate.html) |
| `igate/filters` | IS->RF rule engine (priority-ordered, deny by default) | [`../handbook/igate.html`](../handbook/igate.html) |
| `messages` | APRS messaging domain: router, store (GORM), sender, retry, invite, tactical_set, bots, preferences, event_hub, local_tx_ring | [`../handbook/messaging.html`](../handbook/messaging.html) |
| `gps` | GPSD client + serial NMEA reader + cache + station-position layered cache + enumerate | [`../handbook/gps.html`](../handbook/gps.html) |
| `callsign` | Callsign parsing, N0CALL detection, APRS-IS passcode | [`../handbook/preferences.html`](../handbook/preferences.html) |
| `stationcache` | Heard-station cache (memory + persistent) and APRS-extract helpers | (no dedicated page) |

## Go service: storage & telemetry

| Package | Purpose | Handbook |
|---|---|---|
| `configstore` | SQLite config DB (GORM, glebarez/sqlite, pure Go); migrations, seeds, models | [`../handbook/preferences.html`](../handbook/preferences.html) |
| `historydb` | Position-history SQLite (separate DB, schema bootstrapped on `Open`) | [`../handbook/history-database.html`](../handbook/history-database.html) |
| `packetlog` | In-memory ring of RX/TX/IS packet records with filter-query API. Live-tail fan-out (`Subscribe`) is in `subscribe.go`; per-subscriber bounded channel, drop-on-full -- backs the AX.25 terminal raw-tail mode and any future live monitor pages. | [`../handbook/monitoring.html`](../handbook/monitoring.html) |
| `metrics` | Prometheus metrics + helper to fold Rust-side StatusUpdate into them | [`../handbook/monitoring.html`](../handbook/monitoring.html) |
| `logbuffer` | `slog.Handler` tee that persists DEBUG records into a circular SQLite ring (`graywolf-logs.db`); env-aware path (tmpfs on Pi/SD-card, disk elsewhere); feeds the `graywolf flare` diagnostic submission | (no dedicated page) |
| `releasenotes` | Embedded release-note YAML (`notes.yaml`); lazy parse + markdown render | (in-app "What's new") |

## Go service: PTT enumeration

| Package | Purpose |
|---|---|
| `pttdevice` | Enumerates serial ports, gpiochip devices, CM108 HID devices on the Go side. On macOS/Windows it shells out to `graywolf-modem --list-cm108` (`cm108_modem.go`). |

PTT *driving* is on the Rust side; see the `tx/ptt_*.rs` files above.
The split is enforced by [invariant 9](invariants.md).

## Channel TX gating

| Surface | Where |
|---|---|
| Per-channel mode enum | `pkg/configstore/models.go` — `Channel.Mode` (`aprs`/`packet`/`aprs+packet`); migrated by `migrate_channel_mode.go` (v12). |
| Lookup interface | `pkg/configstore/channel_mode_lookup.go` — `ChannelModeLookup` interface; `*Store` satisfies it via `ModeForChannel`. |
| Beacon refusal | `pkg/beacon/scheduler.go` — `Options.ChannelModes`; `sendBeaconWith` skips packet-mode channels and emits `OnBeaconSkipped(name, "packet_mode")`. |
| Digipeater refusal | `pkg/digipeater/digipeater.go` — `Config.ChannelModes`; `Handle` short-circuits packet-mode rxChannel; rule loop skips packet-mode `ToChannel`. |
| iGate refusal | `pkg/igate/igate.go` — `Config.ChannelModes`; runtime check in `handleISLine` logs WARN and increments `mSubmitDropped` on packet-mode TxChannel. |
| Messages refusal | `pkg/messages/sender.go` — `SenderConfig.ChannelModes`; `sendRF` returns non-retryable error and persists FailureReason on packet-mode channels. |
| Messages TX channel singleton | `pkg/configstore/messages_config.go` — `MessagesConfig` (id=1); migration v13 (`messages_config_singleton`) seeds `tx_channel` from legacy `IGateConfig.TxChannel` on first run. iGate's column now governs IS→RF only. |
| AX.25 terminal config singleton | `pkg/configstore/ax25_terminal_config.go` — `AX25TerminalConfig` (id=1); migration v14 (`ax25_terminal_tables`) seeds the singleton on first run. Persists scrollback rows, cursor blink, default modulo + paclen, the macro toolbar JSON, and the operator's last raw-tail filter. |
| AX.25 saved profiles + recents | `pkg/configstore/ax25_profiles.go` — `AX25SessionProfile` rows; pinned profiles persist forever, unpinned recents are upserted on every CONNECTED transition (via `transcriptRecorder` in `pkg/webapi/ax25_terminal.go`'s `OnFirstConnected` callback) and trimmed to 20. |
| AX.25 transcript store | `pkg/configstore/ax25_transcripts.go` — `AX25TranscriptSession` + `AX25TranscriptEntry`. Bridge calls `transcriptRecorder.Begin/Append/End` when the operator runs `Ctrl-]` `transcript on`; the per-session writer logs every RX/TX byte block plus state-change + error events. |
| Wiring entry | `pkg/app/wiring.go` — injects `*configstore.Store` as `ChannelModes` into beacon/digi/igate/messages constructors. |
| REST | `webapi/channels.go` accepts `mode` on POST/PUT; `webapi/messages_config.go` exposes GET/PUT `/api/messages/config` with packet-mode validation. |
| UI | `web/src/routes/Channels.svelte` shows mode selector + badge; `web/src/routes/Preferences.svelte` shows messages TX-channel selector filtered to non-packet channels. |

See [invariant 23](invariants.md) for the TX-gating contract.

## Go service: web

| Package | Purpose |
|---|---|
| `webapi` | REST API handlers (Gorilla mux); one handler file per feature -- see [`../../pkg/webapi/`](../../pkg/webapi/). The AX.25 terminal upgrades to a WebSocket via `coder/websocket` in [`ax25_terminal.go`](../../pkg/webapi/ax25_terminal.go) (`GET /api/ax25/terminal`); the handler returns 503 until `SetAX25Manager` has been called. |
| `webapi/dto` | Wire-shape DTOs; constants like `DefaultAgwListenAddr`, `MaxMessageText` live here |
| `webapi/docs` | Swag annotation infra: `op_ids.go`, `cmd/idlint`, `cmd/tagify`, `gen/swagger.{json,yaml}` |
| `webauth` | Password hash, session tokens (cookie), `RequireAuth` middleware, store, handlers |
| `webtypes` | Shared shapes (kept here so swag emits one schema, not duplicates per package) |
| `app` | Composition root: `Config`, `App`, `New`, `Run`; wires every subsystem |
| `app/ingress` | Typed RX-frame source enum (in-process, not on the wire) -- see [invariant 17](invariants.md) |
| `app/txbackend` | Per-channel TX backend dispatcher; KISS-as-backend, modem-as-backend |
| `app/{aprsfanout,rxfanout}` | RX fanout to digipeater / KISS broadcast / APRS submit |
| `app/{auth_store,gpsmanager,adapters,wiring,modem,flags,config,shutdown,platform_*}` | Wiring helpers |
| `internal/{backoff,dedup,ratelimit,testsync,testtx}` | Internal utilities |

## Go service: diagnostic flare CLI

| Concern | File |
|---|---|
| `graywolf flare` CLI subcommand entry | `cmd/graywolf/flare.go` |
| Diagnostic-flare orchestration (`Collect`, `Options`) | `pkg/diagcollect/collect.go` |
| Flare DB discovery (graywolf.db) | `pkg/diagcollect/dbpath.go` |
| Modem locator + listing exec helper | `pkg/diagcollect/modem.go` |
| Per-collector domains | `pkg/diagcollect/{configdump,system,service,serial,gpio,gps,audio,usb,cm108,logs}.go` |
| Flare scrubber (rules, hostname, ad-hoc, ScrubFlare) | `pkg/diagcollect/redact/{rules,hostname,engine,flare}.go` |
| Review TUI | `pkg/diagcollect/review/review.go` |
| Submission HTTP client + 5xx pending-flare save | `pkg/diagcollect/submit/{client,store}.go` |

## Wire schema (Go)

Canonical struct tree for the flare wire payload — the contract between
`graywolf flare` (Plan 2b) and graywolf-flare-server (Plan 2c).

| Concern | File |
|---|---|
| Top-level `Flare` struct | [`../../pkg/flareschema/flare.go`](../../pkg/flareschema/flare.go) |
| `SchemaVersion` constant + `Unmarshal` | [`../../pkg/flareschema/version.go`](../../pkg/flareschema/version.go), [`../../pkg/flareschema/unmarshal.go`](../../pkg/flareschema/unmarshal.go) |
| Per-section types | `audio.go`, `usb.go`, `cm108.go`, `system.go`, `devices.go`, `logs.go`, `config.go`, `user.go`, `issue.go` |
| Sample fixture (round-trip + schema gen) | [`../../pkg/flareschema/sample.go`](../../pkg/flareschema/sample.go) |
| Cross-language convergence test | [`../../pkg/flareschema/convergence_test.go`](../../pkg/flareschema/convergence_test.go) |
| JSON Schema generator | [`../../cmd/flareschema-gen/main.go`](../../cmd/flareschema-gen/main.go) |
| Generated JSON Schema document | [`../../docs/flareschema/v1.json`](../../docs/flareschema/v1.json) |

## Web UI (`web/`)

Built into `dist/` under [`../../web/`](../../web/) (gitignored)
and embedded via `go:embed all:dist` -- see [invariant 12](invariants.md).

| Path | What |
|---|---|
| `package.json`, `vite.config.js`, `svelte.config.js` | Build config |
| `embed.go` | `Handler()` and `SPAHandler()` |
| `src/App.svelte`, `src/main.js` | App shell, route table |
| `src/routes/` | One Svelte route per page (Dashboard, LiveMapV2, Channels, Beacons, Digipeater, Igate, Kiss, Agw, Ptt, Gps, AudioDevices, Messages, Terminal + TerminalTranscripts, PositionLog, MapsSettings, Preferences, Login, About, Logs, Simulation) |
| `src/components/terminal/` | AX.25 terminal pieces: `TerminalViewport` (xterm.js host, 80x24 fixed), `PreConnectForm` (channel + CALL[-N] + advanced timers), `StatusBar`, `TabBar`, `CommandBar` (Ctrl-] command line: `disconnect`, `transcript on/off`, etc.), `MacroToolbar` + `MacroEditor` (operator-defined byte-payload buttons), `RawPacketView` (APRS-only channels show packetlog raw-tail in lieu of LAPB session), `TelemetryPanel` (live `link_stats` side panel: V(S)/V(R)/V(A), N2 retry, RTT EWMA, busy flags) |
| `src/lib/terminal/` | Terminal client state: `session.svelte.js` (one WebSocket per link), `sessions.svelte.js` (multi-tab map, cap 6, focus + visibility tracking), `palette.ts` + `theme.js` (CSS-var-resolved xterm ITheme; `theme.test.js` covers the resolver), `presets.ts` (classic / phosphor-green / phosphor-amber), `envelope.js` (b64 ↔ Uint8Array), `macros.svelte.js` (singleton-config-backed macro store), `profiles.svelte.js` (saved + recent connection profiles store) |
| `src/lib/stores/terminal.svelte.js` | Sidebar-facing summary: unread-bytes total across non-focused sessions for the Sidebar `NotificationBadge` |
| `public/fonts/saucecodepro-nerd/` | SauceCodePro Nerd Font face declarations for the terminal viewport. Ships with `local()` fallbacks; the woff2 binaries are pending vendoring (see `VERSION.txt` in that directory) |
| `src/components/` | Reusable: ConfirmDialog, DataTable, FormField, Modal, NewsPopup, PacketLogViewer, PageHeader, ReleaseNoteCard, Sidebar, StationCallsignBanner, SymbolPicker, UpdateAvailableBanner |
| `src/lib/map/` | MapLibre integration (data-store, map-store, layers, sources, popups, APRS icons) |
| `src/lib/maps/` | Offline-maps client glue (downloads-store, state-bounds, state-list, state-picker) |
| `src/lib/settings/` | Reactive prefs stores (units, maps, messages-preferences, theme) |
| `src/lib/themes/`, `themes/` | Theme registry + static CSS theme files |
| `src/api/` | Generated TS client + hand-written wrapper |
| `scripts/generate-api.mjs` | Swagger -> TS generator (driven by `make api-client`) |

## Maps integration (graywolf-maps client)

| Concern | File |
|---|---|
| Auth registration + bearer token | [`../../pkg/mapsauth/client.go`](../../pkg/mapsauth/client.go) |
| Tile cache (PMTiles, atomic writes) | [`../../pkg/mapscache/manager.go`](../../pkg/mapscache/manager.go), `atomic_writer.go` |
| Web-side glue | `web/src/lib/maps/`, `web/src/routes/MapsSettings.svelte` |
| Map render | `web/src/lib/map/`, `web/src/routes/LiveMapV2.svelte` |

PMTiles infrastructure (manifest gen, R2 sync, Cloudflare Worker) is in
`~/dev/graywolf-maps`, not here. See [invariant 7](invariants.md).

## Live updates

[`../../pkg/updatescheck/checker.go`](../../pkg/updatescheck/checker.go)
polls GitHub Releases once per day and serves the snapshot via webapi `/api/updates`.

## Packaging (`packaging/`)

| Target | Path |
|---|---|
| Arch AUR (`graywolf-aprs`) | [`../../packaging/aur/`](../../packaging/aur/) (`PKGBUILD`, `.SRCINFO`, `*.install`, `*.service`, `*.sysusers`) |
| nfpm (deb/rpm) scripts | [`../../packaging/scripts/`](../../packaging/scripts/) (`postinstall.sh`, `preremove.sh`) |
| systemd unit | [`../../packaging/systemd/graywolf.service`](../../packaging/systemd/graywolf.service) |
| udev rules (CM108 / AIOC / SSS) | [`../../packaging/udev/99-graywolf-cm108.rules`](../../packaging/udev/99-graywolf-cm108.rules) |
| Windows NSIS installer | [`../../packaging/nsis/graywolf.nsi`](../../packaging/nsis/graywolf.nsi) |
| Grafana dashboard | [`../../packaging/grafana/remote-kiss-tnc.json`](../../packaging/grafana/remote-kiss-tnc.json) |
