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
| ALSA card canonicalize + capture probe | `audio/soundcard.rs` (`parse_proc_asound_cards`, `group_alsa_cards`, `probe_capture`), `modem/mod.rs` (`collect_input_devices_linux`) |
| Configured device name resolution (exact + ALSA shorthand alias) | `audio/soundcard.rs` (`find_device_by_id_or_alias`, `parse_alsa_hw_id`, `alsa_alias_matches`) |
| Windows audio "enhancements"/APO detection (per-endpoint) -> UI warning | `modem/mod.rs` (`windows_enhancements_enabled`) reads the MMDevices registry hive `HKLM\…\MMDevices\Audio\{Render\|Capture}\{guid}\FxProperties` keyed off the cpal endpoint id; flows `AudioDeviceInfo.audio_enhancements_enabled` (proto) -> `modembridge.AvailableDevice.EnhancementsEnabled` (`enhancements_enabled` JSON) -> `web/src/routes/AudioDevices.svelte` banner/badge/per-device warning. Windows-only; always false elsewhere |
| FLAC test vector playback | `audio/flac.rs` |
| Stdin raw i16 PCM | `audio/stdin_raw.rs` |
| SDR UDP audio bridge | `sdr/mod.rs` |
| Modem orchestration + `TransmitTestSignal` / `TestSignalResult` IPC handler (`handle_transmit_test_signal`) | `modem/mod.rs` |
| TX worker thread (owns sinks + PTT) | `modem/tx_worker.rs` |
| CW / tone PCM synthesis for TX test signals | `txtest.rs` |
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
| `--record <device> --seconds N --out f.wav` (cpal capture to WAV) | `src/record.rs` (+ `src/wavio.rs`) |
| `--decode <f.wav\|f.flac>` (offline decode -> JSON score: `rx_frames`, `rx_bad_fcs`, per-packet `*_dbfs`) | `src/decode.rs` (+ `src/wavio.rs`) |
| WAV/FLAC sample I/O + linear->dBFS helper | `src/wavio.rs` |
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
| `kiss` | KISS framing + TCP server + TCP client + serial supervisor + multi-port manager + tx queue + ratelimit. Per-interface `gate_tx_to_is` flag, when set on a Mode=modem interface, gates frames accepted by `Sink.Submit` into the iGate's RF→IS path via `Server.OnClientTxAccepted`/`Client.OnClientTxAccepted` (wired from `App.kissClientTxGateToIs`). The flag is inert in Mode=tnc because the RX fanout already feeds the iGate (see `pkg/app/rxfanout.go:107-127`). | `framing.go`, `server.go`, `client.go`, `serial.go`, `manager.go`, `tx_queue.go` |
| `platformsvc` (USB serial) | Android USB serial open + device enumeration; Go side of the platform UDS serial transport | `pkg/platformsvc/usbserial.go` (`UsbSerialOpen`, `AvailableUsbSerialDevices`), `pkg/platformsvc/serialstream.go` (shared `serialReadWriteCloser` / `openSerialStream` / `SerialError`) |
| `webapi` (USB serial) | REST endpoint listing connected USB serial devices | `pkg/webapi/kiss_usb.go` (`GET /api/kiss/available-usb-serial-devices`) |
| `webapi` (host serial ports) | REST endpoint listing host serial ports (COM*/dev/tty*/cu.*) for the desktop `serial` interface "Detected ports" dropdown; reuses `gps.EnumerateSerialPorts` | `pkg/webapi/kiss_serial_ports.go` (`GET /api/kiss/available-serial-ports`) |
| `app` (USB serial source) | Build-tag dispatch for the USB serial device source (Android vs. stub) | `pkg/app/usbserialsource_android.go`, `pkg/app/usbserialsource_default.go` |
| `agw` | AGWPE TCP server (direwolf-compatible subset: R/G/g/k/K/m/X/x/y/Y/V) | `server.go`, `protocol.go` |
| `ipcproto` | Generated Go bindings for `proto/graywolf.proto` | `graywolf.pb.go` (regen via `make proto`) |
| `modembridge` | Supervises Rust modem child + IPC state machine + dispatcher + status cache + DCD publisher. `Bridge.TransmitTestSignal` sends the `TransmitTestSignal` IPC message and returns a `TestSignalResult`. | `bridge.go`, `supervisor.go`, `ipc_unix.go`, `ipc_windows.go`, `dispatcher.go`, `session.go`, `status_cache.go` |
| `txgovernor` | Centralized TX gate: per-channel rate limits, dedup, priority queue | `governor.go`, `pqueue.go`, `sink.go` |

See [`../handbook/kiss.html`](../handbook/kiss.html), [`../handbook/agwpe.html`](../handbook/agwpe.html), [`../handbook/remote-kiss-tnc.html`](../handbook/remote-kiss-tnc.html).
The TX-funnel rule lives in [invariant 16](invariants.md).

## Go service: APRS features

| Package | Purpose | Handbook |
|---|---|---|
| `beacon` | Position/object/tracker/custom/igate beacon scheduler (min-heap), smart-beacon, encoder. Per-beacon `position_format` column ('compressed'\|'uncompressed'\|'mic_e', added in configstore migration 23) selects the wire encoding; uncompressed and Mic-E beacons honor `ambiguity` 0..4 via `aprs.ApplyLatLonAmbiguity` and the K/L/Z destination variants. Encoder entry points: `pkg/beacon/builder.go` switches on `Format`; `PositionInfo` (uncompressed, APRS101 ch 6) and `CompressedPositionInfo` (APRS101 ch 9) live in `pkg/beacon/encoder.go`; `MicEPositionInfo` + `MicEDestination` (APRS101 ch 10) live in `pkg/beacon/mice.go`. Mic-E swaps the AX.25 destination with the lat-derived destination at frame-build time in `scheduler.go`. | [`../handbook/beacons.html`](../handbook/beacons.html) |
| `digipeater` | WIDEn-N / TRACEn-N digipeater with preemptive digi, per-channel dedup, and a source-address block list (digipeater-only) | [`../handbook/digipeater.html`](../handbook/digipeater.html) |
| `digipeater/blocklist` | Source-address pattern validator and matcher used only by the digipeater engine | — |
| `igate` | APRS-IS bidirectional gateway: client/login/filter, RF<->IS gating, third-party encap, TNC2 | [`../handbook/igate.html`](../handbook/igate.html) |
| `igate/filters` | IS->RF rule engine (priority-ordered, deny by default) | [`../handbook/igate.html`](../handbook/igate.html) |
| `messages` | APRS messaging domain: router, store (GORM), sender, retry, invite, tactical_set, bots, preferences, event_hub, local_tx_ring, **preflight** (shared auto-ACK + dedup transport, owned by `messages.Service`, consulted by both `messages.Router` and `actions.Classifier`) | [`../handbook/messaging.html`](../handbook/messaging.html) |
| `actions` | `@@`-prefixed APRS message Actions: classifier, parser, OTP verifier, per-Action runner with rate limit + queue, command/webhook executors, source-aware reply, audit pruner | [`actions.md`](actions.md) |
| `remoteactions` | OUTBOUND counterpart: macro + remote-OTP credential stores, base32/target-call/action-name validators, RFC 6238 TOTP generator, service composition root. Sibling, not fork, of `pkg/actions/` (shares only the wire grammar via exported `actions.ValidActionName`). | [`remote-actions.md`](remote-actions.md) |
| `gps` | GPSD client + serial NMEA reader + cache + station-position layered cache + enumerate | [`../handbook/gps.html`](../handbook/gps.html) |
| `callsign` | Callsign parsing, N0CALL detection, APRS-IS passcode | [`../handbook/preferences.html`](../handbook/preferences.html) |
| `stationcache` | Heard-station cache (memory + persistent) and APRS-extract helpers | (no dedicated page) |
| `clocksync` | Host clock-discipline check (Linux `adjtimex(2)` read-only query; `Unknown` elsewhere). The startup banner in `pkg/app/wiring.go` warns once when `Check() == Unsynced` because an undisciplined clock skews packet ages and the map Time Range filter (graywolf#234). | (no dedicated page) |

## Go service: storage & telemetry

| Package | Purpose | Handbook |
|---|---|---|
| `configstore` | SQLite config DB (GORM, glebarez/sqlite, pure Go); migrations, seeds, models. Actions tables live here too: `actions`, `otp_credentials`, `action_listener_addressees`, `action_invocations` (migration 15, raw SQL — not AutoMigrate; see [`actions.md`](actions.md)). Outbound Actions adds `remote_otp_credentials`, `remote_action_macros` (migration 16, raw SQL with FK ON DELETE SET NULL; see [`remote-actions.md`](remote-actions.md)). | [`../handbook/preferences.html`](../handbook/preferences.html) |
| `historydb` | Position-history SQLite (separate DB, schema bootstrapped on `Open`) | [`../handbook/history-database.html`](../handbook/history-database.html) |
| `packetlog` | In-memory ring of RX/TX/IS packet records with filter-query API. Live-tail fan-out (`Subscribe`) is in `subscribe.go`; per-subscriber bounded channel, drop-on-full -- backs the AX.25 terminal raw-tail mode and any future live monitor pages. | [`../handbook/monitoring.html`](../handbook/monitoring.html) |
| `metrics` | Prometheus metrics + helper to fold Rust-side StatusUpdate into them | [`../handbook/monitoring.html`](../handbook/monitoring.html) |
| `logbuffer` | `slog.Handler` tee that persists DEBUG records into a circular SQLite ring (`graywolf-logs.db`); env-aware path (tmpfs on Pi/SD-card, disk elsewhere); feeds the `graywolf flare` diagnostic submission. Read side: `(*DB).Query` (`query.go`) backs the **System Logs** UI tab via `GET /api/system-logs` (`pkg/webapi/system_logs.go`, wired out-of-band in `pkg/app/wiring.go`; the `*logbuffer.DB` handle is threaded from `cmd/graywolf/main.go`'s `setupLogger` through `app.Config.LogBuffer`, nil when persistence is disabled → endpoint reports `available:false`) | (no dedicated page) |
| `releasenotes` | Embedded release-note YAML (`notes.yaml`); lazy parse + markdown render | (in-app "What's new") |

## Go service: PTT enumeration

| Package | Purpose |
|---|---|
| `pttdevice` | Enumerates serial ports, gpiochip devices, CM108 HID devices on the Go side. On macOS/Windows it shells out to `graywolf-modem --list-cm108` (`cm108_modem.go`). |

PTT *driving* is on the Rust side; see the `tx/ptt_*.rs` files above.
The split is enforced by [invariant 9](invariants.md).

### Channel-card PTT indicator (issue #112)

| Surface | Where |
|---|---|
| Computed read-only summary on `ChannelResponse` | `pkg/webapi/dto/channel.go` — `ChannelPtt{Method,Configured,Detail}`; `ChannelPttFromModel` derives the operator-facing detail string (CM108 pin, GPIO line, serial path, rigctld endpoint). |
| Wiring | `pkg/webapi/channels.go` — `listChannels` looks up `ListPttConfigs` once and indexes by channel id; `getChannel` does a single `GetPttConfigForChannel` lookup. Missing row → nil `Ptt` (omitempty) so the UI distinguishes "never configured" from `method=none`. |
| UI helpers | `web/src/lib/channelPtt.js` — `summaryLine`, `pttState`, `methodLabel`, `ariaLabel`. Mirrors `channelBacking.js`. |
| Card row | `web/src/routes/Channels.svelte` — second `backing-row`-styled block under the BACKING row, only shown for modem-backed TX channels (KISS-only and RX-only channels don't drive PTT). |

### PTT tab (unified Android + desktop)

| Surface | Where |
|---|---|
| Page shell, dialog hosts, Platform.kind branch | `web/src/routes/Ptt.svelte` |
| One-card-per-PttConfig with Change Method / Change Device / Test PTT | `web/src/routes/ptt/PttCard.svelte` |
| Method radio-card list | `web/src/routes/ptt/MethodPicker.svelte` |
| Device list (Recommended / Other split + permission CTA) | `web/src/routes/ptt/DevicePicker.svelte` |
| Dialog A — method picker + rigctld host:port + Test Connection | `web/src/routes/ptt/DialogChangeMethod.svelte` |
| Dialog B — device picker + GPIO line / CM108 pin / invert | `web/src/routes/ptt/DialogChangeDevice.svelte` |
| Method options per platform | `web/src/routes/ptt/devices/methodOptions.{android,desktop}.js` |
| Device-source adapters per platform | `web/src/routes/ptt/devices/{android,desktop}DeviceSource.js` |
| Channel-selector auto-hide + Add visibility rule | `web/src/routes/ptt/channelSelector.js` |
| Android USB enumeration into `[]AvailableDevice` shape | `pkg/pttdevice/android.go` |

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
| UI | `web/src/routes/Channels.svelte` shows mode selector + badge; `web/src/routes/MessagesSettings.svelte` shows messages TX-channel selector filtered to non-packet channels. |

See [invariant 23](invariants.md) for the TX-gating contract.

### Channel TX test signal

| Surface | Where |
|---|---|
| REST endpoint | `pkg/webapi/channels.go` -- `sendTestSignal` handles `POST /api/channels/{id}/test-tx`; validates callsign before IPC (see [invariant 39](invariants.md)). |
| Go bridge call | `pkg/modembridge` -- `Bridge.TransmitTestSignal` sends the `TransmitTestSignal` proto message and returns `TestSignalResult`. |
| Rust handler | `graywolf-modem/src/modem/mod.rs` -- `handle_transmit_test_signal` dispatches to `graywolf-modem/src/txtest.rs` for PCM synthesis. |

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
| `src/routes/` | One Svelte route per page (Dashboard, LiveMapV2, Channels, Beacons, Digipeater, Igate, Kiss, Agw, Ptt, Gps, AudioDevices, Messages, Terminal + TerminalTranscripts, PositionLog, MapsSettings, Preferences, MessagesSettings, Login, About, Logs, Simulation) |
| `src/components/terminal/` | AX.25 terminal pieces: `TerminalViewport` (xterm.js host, 80x24 fixed), `PreConnectForm` (channel + CALL[-N] + advanced timers), `StatusBar`, `TabBar`, `CommandBar` (Ctrl-] command line: `disconnect`, `transcript on/off`, etc.), `MacroToolbar` + `MacroEditor` (operator-defined byte-payload buttons), `RawPacketView` (APRS-only channels show packetlog raw-tail in lieu of LAPB session), `TelemetryPanel` (live `link_stats` side panel: V(S)/V(R)/V(A), N2 retry, RTT EWMA, busy flags) |
| `src/lib/terminal/` | Terminal client state: `session.svelte.js` (one WebSocket per link), `sessions.svelte.js` (multi-tab map, cap 6, focus + visibility tracking), `palette.ts` + `theme.js` (CSS-var-resolved xterm ITheme; `theme.test.js` covers the resolver), `presets.ts` (classic / phosphor-green / phosphor-amber), `envelope.js` (b64 ↔ Uint8Array), `macros.svelte.js` (singleton-config-backed macro store), `profiles.svelte.js` (saved + recent connection profiles store), `lineendings.js` (stateful inbound CR/LF→CRLF normalizer wired into `TerminalViewport`'s `onDataRX`; xterm runs `convertEol:false` and only rewrites bare LF, so this is what lets bare-CR BBS streams advance lines instead of overwriting. Bare-CR-as-in-place-overwrite (progress bars) is intentionally collapsed to a line advance -- AX.25 BBSes are line-oriented; ANSI CSI cursor moves still pass through untouched. `lineendings.test.js`) |
| `src/lib/stores/terminal.svelte.js` | Sidebar-facing summary: unread-bytes total across non-focused sessions for the Sidebar `NotificationBadge` |
| `public/fonts/saucecodepro-nerd/` | SauceCodePro Nerd Font face declarations for the terminal viewport. Ships with `local()` fallbacks; the woff2 binaries are pending vendoring (see `VERSION.txt` in that directory) |
| `src/components/` | Reusable: ConfirmDialog, DataTable, FormField, Modal, NewsPopup, PacketLogViewer, PacketInspector, PageHeader, ReleaseNoteCard, Sidebar, StationCallsignBanner, SymbolPicker, UpdateAvailableBanner. `PacketInspector` is the deep packet-inspection modal opened from the APRS Logs tab (a subtle loupe button in each `PacketLogViewer` row footer, gated behind its `inspectable` prop so the Dashboard stays uncluttered); it decodes `Entry.raw` (base64 AX.25 frame) into a hex/ASCII dump + frame-structure summary + anomaly list (malformed Mic-E, invalid addresses, unexpected control/PID) via the pure helpers in `src/lib/packetInspect.js` (`packetInspect.test.js`). Each `PacketLogViewer` row also shows a **scope-reticle locate button** in the Src→Dst cell whenever the packet carries coordinates (`Entry.lat`/`Entry.lon`, surfaced by `pkg/webapi/packets.go` `enrichPacket`/`packetPosition` for position, Mic-E, weather, object, and item reports alike — independent of the local station's own GPS, unlike `distance_mi`); clicking it deep-links to the live map framed on that fix via `#/map?focus=CALL&lat=…&lon=…` (the reverse of the station popup's "APRS logs" link; GH #350). The reticle is the consistent cross-type "has a position" affordance, so Mic-E and weather transmissions get it too. |
| `src/components/messages/remote_actions/` | Outbound Actions UI: `RemoteActionsDrawer` (zap-icon-anchored thread drawer), `MacroTile` + `MacroEditRow` (fire / edit modes), `FreeFormSender` (ad-hoc `@@<otp>#cmd`), `CredentialsModal` + `EditCredentialModal` + `CredentialPicker` (TOTP secret CRUD), `ReplyBubbleAdornment` (zap-tagged inbound badge). See [`remote-actions.md`](remote-actions.md). |
| `src/lib/remote_actions/` | Outbound Actions client lib: typed API wrapper, reactive store (Svelte 5 runes singleton), TOTP countdown timer, reply correlation (60s window + status-prefix allowlist), wire-string assembler + send helper that piggy-backs on `POST /api/messages`. |
| `src/lib/map/` | MapLibre integration (data-store, map-store, layers, sources, popups, APRS icons, `clock-offset.svelte.js` — host-clock skew correction for packet ages, see invariant 41, right-click `map-context-menu.svelte` — Copy GPS / Copy grid / Add fixed beacon here / Add fixed point here; "Add fixed beacon" deep-links into `Beacons.svelte` via `#/beacons?lat=…&lon=…`. "Add fixed point" opens `fixed-point-dialog.svelte` (name + APRS `SymbolPicker`) and drops a landmark into `fixed-points-store.svelte.js`, rendered by `layers/fixed-points.js`; clicking a point opens a delete popup. Fixed points are persisted **server-side** and shared across every device/browser pointed at the server (graywolf#347): the store reads/writes `GET/POST/DELETE /api/fixed-points` (handlers `pkg/webapi/fixed_points.go`, model `configstore.FixedPoint`) via `lib/api.js`, loading on map mount; the snake_case to layer-shape conversion lives in the pure `fixed-points-api.js`. A one-time `load()` migration uploads any points left in the old `localStorage['map-fixed-points']` so upgrading operators don't lose them. The station popup (`popup.js` `renderStationActionsHTML`, suppressed for `is_object` items) renders three action links: **Message** → `#/messages?thread=dm:CALL`, **APRS logs** → `#/logs?callsign=CALL` (seeds the `Logs.svelte` search box from the hash), and an external **QRZ** lookup `https://www.qrz.com/db/CALL`. The inverse link runs from the log back to the map: the packet log's reticle navigates to `#/map?focus=CALL&lat=…&lon=…`, which `LiveMapV2.svelte` parses on mount (`parseFocusFromHash`), claims the camera with an `easeTo` to those coordinates in `onMapReady` (setting `didAutoFit` so the default framing yields), and — once the first poll populates the store — opens that station's popup (one-shot `focusPopupDone` effect; a station older than the active time-range still gets the camera fly, just no popup). For an `is_object` item the popup also shows a "from CALL" originating-station line beneath the object name (`StationDTO.source`, carried from the packet's AX.25 source through `stationcache`), clickable as a path-link to that station when it is itself on the map — so an object reads as authored by its true originator rather than the digipeater that relayed it (GH #323)) |
| Radar overlay | `src/lib/map/layers/radar.js` (imperative MapLibre layer) consumes a descriptor from `src/lib/map/sources/radar-source.js`. Two orthogonal axes: **region** (`radarProviderForRegion`, operator-selected on the maps tab, persisted `gw_radar_region`) and the US **backend seam** (`ACTIVE_RADAR_BACKEND` → `raster` IEM N0Q vs `vector` contour MVT; **vector is active**). Region `us` delegates to the backend; region `world` is `worldRadarProvider()` — the RainViewer global composite, now an **animated per-frame raster loop** (`/radar/rainviewer/*`, `maxzoom 7` for native cap) that rides the same per-frame source/opacity machinery as the US vector loop. `radar.js` `setRegion()` tears down and rebuilds the provider on switch (clearing the frame set, since US contour ts and RainViewer ts are different namespaces; `LiveMapV2.svelte` resets the loop store and re-polls in step); the layer module and `LiveMapV2.svelte` are region- and backend-agnostic. **Both the US vector backend and the world RainViewer raster are animated rolling loops**: a `perFrame` provider whose tile URL is keyed by an immutable per-frame epoch ts (US `radar/<ts>/{z}/{x}/{y}.pbf`, world `radar/rainviewer/<ts>/{z}/{x}/{y}.png`; no `?v=` cache-bust — the ts IS the cache key). Each frame gets its **own** MapLibre source + layer (fill for US vector, raster for world): `radar.js` `setFrames(tsList)` preloads every manifest frame up front at opacity 0 (one cached source each) and reconciles the set as frames roll in/out, and `setFrameTs(ts)` animates the loop by handing the visible opacity from the previous frame to the current one. That is a pure paint-property toggle — no `setTiles`, no source reload — so looping reuses already-loaded frames instead of refetching/re-parsing tiles every cycle (the prior single-source `setTiles`-per-frame design refetched the whole loop each pass, which made the animation choppy). Frame discovery + playback live in `src/lib/map/radar-frames.svelte.js` (a `$state` wrapper) over the pure `src/lib/map/radar-frames-core.js`: it polls the region's loop manifest (~15s, bearer `?t=`) — `GET /radar/manifest.json` for US (`parseManifestFrames`) or `GET /radar/rainviewer/manifest.json` for world (`parseRainviewerManifestFrames`, which synthesizes `iso` from `ts` since the RainViewer manifest carries only `ts`), both yielding oldest-first `{ts,iso}` via `radarManifestUrlForRegion`/`parseManifestFramesForRegion` — and drives Play/Pause/Stop + a frame slider in the layers pane (`LiveMapV2.svelte`, mirrored in the mobile drawer). A region switch calls the store's `reset()` and re-polls so the slider never animates the wrong region's frames. Vector source bounded to `minzoom 3`/`maxzoom 8`. The inactive US IEM **raster** fallback backend (`ACTIVE_RADAR_BACKEND = raster`) is a plain single-source static-tiles raster (no loop); the origin Worker 400s any stray param on `/radar/*` except the `?t=` bearer. Toggle/opacity/region persisted to `gw_radar_*` localStorage keys. Pure logic unit-tested in `radar-source.test.js` + `radar-frames-core.test.js`; per-frame preload/opacity-toggle + region switch in `radar.test.js`. Tiles served by the graywolf-maps origin Worker under `https://maps.nw5w.com/radar/*` (US raster = edge-cached IEM pull-through, per-frame `.pbf` = contour loop from R2, `rainviewer` = RainViewer pull-through), riding the same `?t=` bearer token as the basemap. Design: `docs/superpowers/specs/2026-06-14-radar-loop-animation-design.md`. |
| `src/lib/maps/` | Offline-maps client glue (downloads-store, state-bounds, state-list, state-picker) |
| `src/lib/settings/` | Reactive prefs stores (units, maps, messages-preferences, theme, ui-scale, `log-prefs` — device-local APRS Logs auto-refresh/auto-scroll toggles, GH #373) |
| `src/lib/compactLayout.js` | Single source of truth for the `COMPACT_LAYOUT_QUERY` matchMedia breakpoint that swaps the full desktop sidebar / map perma-card for the top bar / drawer / FAB chrome. Covers narrow portrait **and** short landscape phones (`(max-width: 768px), (orientation: landscape) and (max-height: 500px)`) so a phone rotated to landscape is treated as first-class instead of dropping to the desktop layout that crowded the map (GH #419). Imported by `LiveMapV2.svelte` (`isMobile`) and `lib/map/info-panel.svelte`; the **same conditions are mirrored in CSS** in `App.svelte`, `components/Sidebar.svelte`, and `lib/map/maplibre-map.svelte` — keep JS and CSS in sync. In landscape the sidebar renders as a slim vertical icon rail (`--landscape-rail-width`, app.css) down the left edge rather than a horizontal top bar, preserving the scarce viewport height. |
| `src/lib/themes/`, `themes/` | Theme registry + static CSS theme files. Map chrome surfaces read `--map-overlay-bg` + `--map-overlay-blur`: standard themes make the surface translucent (`color-mix(... 88%, transparent)`) with a 10px backdrop blur so the map reads through the overlays; high-contrast themes keep it opaque with blur 0. |
| `src/api/` | Generated TS client + hand-written wrapper |
| `scripts/generate-api.mjs` | Swagger -> TS generator (driven by `make api-client`) |

## Maps integration (graywolf-maps client)

| Concern | File |
|---|---|
| Auth registration + bearer token | [`../../pkg/mapsauth/client.go`](../../pkg/mapsauth/client.go) |
| Tile cache (PMTiles, atomic writes) | [`../../pkg/mapscache/manager.go`](../../pkg/mapscache/manager.go), `atomic_writer.go` |
| PMTiles v3 header bbox reader (used by the startup backfill) | [`../../pkg/mapscache/pmtiles_header.go`](../../pkg/mapscache/pmtiles_header.go) |
| Render-path bounds (offline-safe; reads `maps_downloads.bbox`, no remote dep) | [`../../pkg/webapi/local_bounds.go`](../../pkg/webapi/local_bounds.go) (`GET /api/maps/local-bounds`) |
| Catalog fetcher + disk fallback for the region picker | [`../../pkg/mapscatalog/catalog.go`](../../pkg/mapscatalog/catalog.go) (`NewWithDiskCache` writes/reads `<TileCacheDir>/catalog.json`) |
| Style/glyph/sprite/shields/tiles.json pull-through cache, served at `/api/maps/style/{path}` | [`../../pkg/mapsstyle/`](../../pkg/mapsstyle/) |
| Web-side glue | `web/src/lib/maps/`, `web/src/routes/MapsSettings.svelte` |
| Web local-bounds store (consumed by gw-tile protocol) | `web/src/lib/maps/local-bounds-store.svelte.js` |
| Map render | `web/src/lib/map/`, `web/src/routes/LiveMapV2.svelte` |

The render path (the `gw-tile://` MapLibre protocol) reads bounds from
`/api/maps/local-bounds`, NOT the catalog. This is what lets the map
serve already-downloaded regions on a host that has never reached
maps.nw5w.com in the current boot. The picker still reads the catalog
(via `/api/maps/catalog`), with a disk fallback so the operator sees
the last-known region list when offline.

PMTiles infrastructure (manifest gen, R2 sync, Cloudflare Worker) is in
`~/dev/graywolf-maps`, not here. See [invariant 7](invariants.md).

## Live updates

[`../../pkg/updatescheck/checker.go`](../../pkg/updatescheck/checker.go)
polls GitHub Releases once per day and serves the snapshot via webapi `/api/updates`.

## Android Kotlin platform service (`android/app/`)

Kotlin code that backs the Android tablet build. The webview hosts the
graywolf Go binary in-process; everything platform-specific (USB, audio,
GPS, Bluetooth) lives on the Kotlin side and is reached through the
platform UDS via proto messages defined in
[`../../proto/platform.proto`](../../proto/platform.proto). Handbook:
[`../handbook/installation.html`](../handbook/installation.html) (Android
section), [`../handbook/kiss-bluetooth.html`](../handbook/kiss-bluetooth.html),
[`../handbook/kiss-usb-serial.html`](../handbook/kiss-usb-serial.html).

| Concern | File |
|---|---|
| Activity + webview host | `com/nw5w/graywolf/MainActivity.kt`, `webview/WebAppInterface.kt` |
| Foreground service + Go binary supervisor | `GraywolfService.kt`, `binaries/Supervisor.kt`, `binaries/GoLauncher.kt` |
| Modem JNI bridge | `jni/ModemBridge.kt` |
| Platform UDS server + proto codec | `platformsvc/PlatformServer.kt`, `platformsvc/MessageHandler.kt`, `platformsvc/WireCodec.kt` |
| USB PTT adapter (CM108 / CP2102N / AIOC / VOX) | `usb/UsbPttAdapter.kt`, `usb/PttMethodConsts.kt` |
| USB device ownership arbiter (KISS vs. PTT) | `usb/UsbDeviceArbiter.kt` -- process-global set of deviceNames claimed by non-PTT subsystems (`claim` adds, `release` removes, `isClaimed` queries); `UsbPttAdapter` consults `isClaimed` before auto-opening. PTT eviction is a separate step: `UsbSerialFacade.open()` calls `UsbPttAdapter.evictDevice` immediately after `UsbDeviceArbiter.claim`. |
| USB serial byte relay for KISS-over-USB-Serial | `platformsvc/UsbSerialAdapter.kt` -- owns one `UsbDeviceConnection` per handle; pump pair on worker thread; multiplexes through the platform UDS via `SerialOpen` / `SerialData` / `SerialClose` / `SerialError` proto messages |
| USB serial permission + chip-family facade | `platformsvc/UsbSerialFacade.kt` -- enumerates connected USB serial devices (CDC-ACM, CP210x, CH34x), requests Android `UsbManager` permissions, delivers open handles to `UsbSerialAdapter` |
| Bluetooth facade + permission/bond receivers | `platformsvc/BluetoothFacade.kt` |
| RFCOMM byte relay for KISS-over-Bluetooth | `platformsvc/BtSerialAdapter.kt` -- owns one `BluetoothSocket` per handle; pump pair on worker thread; multiplexes through the platform UDS via `SerialOpen` / `SerialData` / `SerialClose` / `SerialError` proto messages |
| Audio capture / playback pumps | `audio/AudioPump.kt`, `audio/AudioTxPump.kt`, `audio/AudioTxTest.kt` |
| GPS adapter | `gps/GpsAdapter.kt` |

The blocking-call-on-worker-thread rule that applies to USB and
Bluetooth code on this surface is [invariant 35](invariants.md).

## Packaging (`packaging/`)

| Target | Path |
|---|---|
| Arch AUR (`graywolf-aprs`) | [`../../packaging/aur/`](../../packaging/aur/) (`PKGBUILD`, `.SRCINFO`, `*.install`, `*.service`, `*.sysusers`) |
| nfpm (deb/rpm) scripts | [`../../packaging/scripts/`](../../packaging/scripts/) (`postinstall.sh`, `preremove.sh`) |
| systemd unit | [`../../packaging/systemd/graywolf.service`](../../packaging/systemd/graywolf.service) |
| udev rules (CM108 / AIOC / SSS) | [`../../packaging/udev/99-graywolf-cm108.rules`](../../packaging/udev/99-graywolf-cm108.rules) |
| Windows NSIS installer | [`../../packaging/nsis/graywolf.nsi`](../../packaging/nsis/graywolf.nsi) |
| Grafana dashboard | [`../../packaging/grafana/remote-kiss-tnc.json`](../../packaging/grafana/remote-kiss-tnc.json) |
