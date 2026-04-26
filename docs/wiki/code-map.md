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

## Go service: networking & protocol (`graywolf/pkg/`)

| Package | Purpose | Key files |
|---|---|---|
| `ax25` | AX.25 v2.0 UI frame encode/decode (UI only) | `frame.go`, `address.go`, `priority.go` |
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
| `packetlog` | In-memory ring of RX/TX/IS packet records with filter-query API | [`../handbook/monitoring.html`](../handbook/monitoring.html) |
| `metrics` | Prometheus metrics + helper to fold Rust-side StatusUpdate into them | [`../handbook/monitoring.html`](../handbook/monitoring.html) |
| `logbuffer` | `slog.Handler` tee that persists DEBUG records into a circular SQLite ring (`graywolf-logs.db`); env-aware path (tmpfs on Pi/SD-card, disk elsewhere); feeds the `graywolf flare` diagnostic submission | (no dedicated page) |
| `releasenotes` | Embedded release-note YAML (`notes.yaml`); lazy parse + markdown render | (in-app "What's new") |

## Go service: PTT enumeration

| Package | Purpose |
|---|---|
| `pttdevice` | Enumerates serial ports, gpiochip devices, CM108 HID devices on the Go side. On macOS/Windows it shells out to `graywolf-modem --list-cm108` (`cm108_modem.go`). |

PTT *driving* is on the Rust side; see the `tx/ptt_*.rs` files above.
The split is enforced by [invariant 9](invariants.md).

## Go service: web

| Package | Purpose |
|---|---|
| `webapi` | REST API handlers (Gorilla mux); one handler file per feature -- see [`../../graywolf/pkg/webapi/`](../../graywolf/pkg/webapi/) |
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

## Wire schema (Go)

Canonical struct tree for the flare wire payload — the contract between
`graywolf flare` (Plan 2b) and graywolf-flare-server (Plan 2c).

| Concern | File |
|---|---|
| Top-level `Flare` struct | [`../../graywolf/pkg/flareschema/flare.go`](../../graywolf/pkg/flareschema/flare.go) |
| `SchemaVersion` constant + `Unmarshal` | [`../../graywolf/pkg/flareschema/version.go`](../../graywolf/pkg/flareschema/version.go), [`../../graywolf/pkg/flareschema/unmarshal.go`](../../graywolf/pkg/flareschema/unmarshal.go) |
| Per-section types | `audio.go`, `usb.go`, `cm108.go`, `system.go`, `devices.go`, `logs.go`, `config.go`, `user.go`, `issue.go` |
| Sample fixture (round-trip + schema gen) | [`../../graywolf/pkg/flareschema/sample.go`](../../graywolf/pkg/flareschema/sample.go) |
| Cross-language convergence test | [`../../graywolf/pkg/flareschema/convergence_test.go`](../../graywolf/pkg/flareschema/convergence_test.go) |
| JSON Schema generator | [`../../graywolf/cmd/flareschema-gen/main.go`](../../graywolf/cmd/flareschema-gen/main.go) |
| Generated JSON Schema document | [`../../docs/flareschema/v1.json`](../../docs/flareschema/v1.json) |

## Web UI (`graywolf/web/`)

Built into `dist/` under [`../../graywolf/web/`](../../graywolf/web/) (gitignored)
and embedded via `go:embed all:dist` -- see [invariant 12](invariants.md).

| Path | What |
|---|---|
| `package.json`, `vite.config.js`, `svelte.config.js` | Build config |
| `embed.go` | `Handler()` and `SPAHandler()` |
| `src/App.svelte`, `src/main.js` | App shell, route table |
| `src/routes/` | One Svelte route per page (Dashboard, LiveMapV2, Channels, Beacons, Digipeater, Igate, Kiss, Agw, Ptt, Gps, AudioDevices, Messages, PositionLog, MapsSettings, Preferences, Login, About, Logs, Simulation) |
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
| Auth registration + bearer token | [`../../graywolf/pkg/mapsauth/client.go`](../../graywolf/pkg/mapsauth/client.go) |
| Tile cache (PMTiles, atomic writes) | [`../../graywolf/pkg/mapscache/manager.go`](../../graywolf/pkg/mapscache/manager.go), `atomic_writer.go` |
| Web-side glue | `graywolf/web/src/lib/maps/`, `graywolf/web/src/routes/MapsSettings.svelte` |
| Map render | `graywolf/web/src/lib/map/`, `graywolf/web/src/routes/LiveMapV2.svelte` |

PMTiles infrastructure (manifest gen, R2 sync, Cloudflare Worker) is in
`~/dev/graywolf-maps`, not here. See [invariant 7](invariants.md).

## Live updates

[`../../graywolf/pkg/updatescheck/checker.go`](../../graywolf/pkg/updatescheck/checker.go)
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
