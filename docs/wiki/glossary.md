# Glossary

Domain terms as graywolf uses them. Each entry points at where the term
is implemented or canonically described in *this* project. For RF / APRS
background, the operator handbook is the starting point.

## Protocol / RF

| Term | In this project | Pointer |
|---|---|---|
| APRS | Amateur Packet Reporting System; positions, messages, telemetry over AX.25 UI frames. Parsing implemented in Go. | [`../../pkg/aprs/`](../../pkg/aprs/), handbook [`index.html`](../handbook/index.html) |
| AFSK (Bell 202, 1200 baud) | Standard RF modulation for VHF/UHF APRS. Demodulator in Rust; modulator in `tx/afsk_mod.rs`. | [`../../graywolf-modem/src/demod_afsk.rs`](../../graywolf-modem/src/demod_afsk.rs), [`../handbook/channels.html`](../handbook/channels.html) |
| 9600 G3RUH | 9600 baud baseband FSK with the G3RUH scrambler `x^17 + x^12 + 1`. | [`../../graywolf-modem/src/modem_9600/`](../../graywolf-modem/src/modem_9600/), [`../handbook/channels.html`](../handbook/channels.html) |
| PSK (QPSK 2400, 8-PSK 4800) | V.26/V.27 PSK modems with Costas + Gardner. | [`../../graywolf-modem/src/modem_psk/`](../../graywolf-modem/src/modem_psk/) |
| AX.25 | Amateur radio link-layer. UI (Unnumbered Information) frames are the workhorse for APRS; connected-mode (LAPB) carries reliable point-to-point data. | [`../../pkg/ax25/`](../../pkg/ax25/), [`../../pkg/ax25conn/`](../../pkg/ax25conn/) |
| LAPB | Link Access Procedure, Balanced. AX.25's connected-mode (reliable) data link layer with sliding-window I-frames, S-frames (RR/RNR/REJ), and U-frames (SABM/SABME/UA/DM/DISC). graywolf implements outbound-only LAPB v2.0 (mod-8) in `pkg/ax25conn`; behavior derives from Linux `net/ax25/` + ax25-tools (see `CREDITS.md`). | [`../../pkg/ax25conn/`](../../pkg/ax25conn/) |
| SABM | Set Asynchronous Balanced Mode. Connection-establishment U-frame (mod-8). Sent by the calling side; peer replies with UA. | [`../../pkg/ax25conn/transitions_disconnected.go`](../../pkg/ax25conn/transitions_disconnected.go) |
| SABME | SABM Extended. Mod-128 variant of SABM that requests v2.2 extended sequence numbers. graywolf falls back to SABM if a peer answers with DM. | [`../../pkg/ax25conn/transitions_awaiting_connection.go`](../../pkg/ax25conn/transitions_awaiting_connection.go) |
| Modulo-8 / Modulo-128 | LAPB sequence-number range. Mod-8 (default) uses 3-bit N(S)/N(R) and a 1-byte control field; mod-128 uses 7-bit fields and a 2-byte control field with a larger window. graywolf negotiates mod-128 via SABME and falls back to mod-8 on DM. | [`../../pkg/ax25conn/control.go`](../../pkg/ax25conn/control.go), [`../../pkg/ax25conn/defaults.go`](../../pkg/ax25conn/defaults.go) |
| K3NA | Eric Williams (K3NA), 1988 ARRL CNC paper "AX.25 Data-Link State Machine" — the canonical state-table reference Linux's kernel stack and graywolf both follow (with the kernel's documented deviations). | [CNC1988-AX.25DataLinkStateMachine-K3NA.pdf](https://web.tapr.org/meetings/CNC_1988/CNC1988-AX.25DataLinkStateMachine-K3NA.pdf) |
| KISS | TNC framing protocol (`FEND`/escape). KISS framing + TCP server + client + multi-port manager. | [`../../pkg/kiss/`](../../pkg/kiss/), [`../handbook/kiss.html`](../handbook/kiss.html), [`../handbook/remote-kiss-tnc.html`](../handbook/remote-kiss-tnc.html) |
| AGWPE | direwolf-compatible TCP TNC protocol. Subset implemented: `R/G/g/k/K/m/X/x/y/Y/V`. | [`../../pkg/agw/protocol.go`](../../pkg/agw/protocol.go), [`../handbook/agwpe.html`](../handbook/agwpe.html) |
| HDLC | NRZI bit stream, flag/abort, bit-stuffing, FCS-16. RX on Rust side has fix-bits retry. | [`../../graywolf-modem/src/hdlc.rs`](../../graywolf-modem/src/hdlc.rs), [`../../graywolf-modem/src/tx/hdlc_encode.rs`](../../graywolf-modem/src/tx/hdlc_encode.rs) |
| FX.25 | Reed-Solomon FEC over AX.25 (CCSDS poly), correlation-tag preamble. | [`../../graywolf-modem/src/fx25/`](../../graywolf-modem/src/fx25/) |
| IL2P | RS-coded header/payload framing with scrambler. | [`../../graywolf-modem/src/il2p/`](../../graywolf-modem/src/il2p/) |
| SDR (UDP audio bridge) | Listens on UDP for `s16le`/`f32le` audio from an external SDR. Selected via `source_type: sdr_udp` on the Audio Devices settings page. | [`../../graywolf-modem/src/sdr/`](../../graywolf-modem/src/sdr/), [`../../web/src/routes/AudioDevices.svelte`](../../web/src/routes/AudioDevices.svelte), [`../handbook/audio.html`](../handbook/audio.html) |
| Beacon | Scheduled transmit (position / object / tracker / custom / igate). Min-heap scheduler. | [`../../pkg/beacon/`](../../pkg/beacon/), [`../handbook/beacons.html`](../handbook/beacons.html) |
| Smart beacon | Speed/heading-aware beacon timing (mirrors direwolf SMARTBEACON / HamHUD). | [`../../pkg/beacon/smartbeacon.go`](../../pkg/beacon/smartbeacon.go), [`../handbook/beacons.html`](../handbook/beacons.html) |
| Digipeater | WIDEn-N / TRACEn-N digipeater with preemptive digi and per-channel dedup. | [`../../pkg/digipeater/`](../../pkg/digipeater/), [`../handbook/digipeater.html`](../handbook/digipeater.html) |
| iGate | Bidirectional APRS-IS gateway (RF<->IS). | [`../../pkg/igate/`](../../pkg/igate/), [`../handbook/igate.html`](../handbook/igate.html) |
| WIDEn-N | APRS digipeater path semantic; consumed by the digipeater. | [`../../pkg/digipeater/`](../../pkg/digipeater/), [`../handbook/digipeater.html`](../handbook/digipeater.html) |
| Mic-E | Compact APRS encoding for mobile rigs. | [`../../pkg/aprs/mice.go`](../../pkg/aprs/mice.go) |
| PHG | Power / antenna height / antenna gain encoding. | [`../../pkg/aprs/phg.go`](../../pkg/aprs/phg.go) |
| DAO | High-precision position augmentation. | [`../../pkg/aprs/dao.go`](../../pkg/aprs/dao.go) |
| DF | Direction-finding bearing/quality fields. | [`../../pkg/aprs/df.go`](../../pkg/aprs/df.go) |
| Peet Bros | Peet Bros weather-station serial format. | [`../../pkg/aprs/peetbros.go`](../../pkg/aprs/peetbros.go) |
| Telemetry | APRS telemetry packets and parameter coefficients. | [`../../pkg/aprs/telemetry.go`](../../pkg/aprs/telemetry.go) |
| Capabilities packet | APRS station-capabilities advertisements. | [`../../pkg/aprs/capabilities.go`](../../pkg/aprs/capabilities.go) |
| Object packet | APRS named-object advertisements. | [`../../pkg/aprs/object.go`](../../pkg/aprs/object.go) |
| Base-91 | Compressed-position encoding alphabet. | [`../../pkg/aprs/base91.go`](../../pkg/aprs/base91.go) |
| Callsign / SSID | Station identifier, optional `-N` suffix. | [`../../pkg/callsign/parse.go`](../../pkg/callsign/parse.go), [`../handbook/preferences.html`](../handbook/preferences.html) |
| Tactical callsign | Operator-chosen alias mapped to a real callsign for chat / display. | [`../../pkg/messages/tactical_set.go`](../../pkg/messages/tactical_set.go), [`../handbook/messaging.html`](../handbook/messaging.html) |
| Tactical chat invite | `!GW1 INVITE TAC` protocol for joining a tactical group. | [`../../pkg/messages/invite.go`](../../pkg/messages/invite.go) |
| APRS-IS passcode | Login auth code derived from callsign. | [`../../pkg/callsign/passcode.go`](../../pkg/callsign/passcode.go) |
| IS->RF filter | Rule engine for gating internet traffic to RF; deny-by-default ([invariant 15](invariants.md)). | [`../../pkg/igate/filters/`](../../pkg/igate/filters/), [`../handbook/igate.html`](../handbook/igate.html) |
| Server-side filter (`g/`) | Filter clause sent to the APRS-IS server to limit upstream feed. | [`../../pkg/igate/server_filter.go`](../../pkg/igate/server_filter.go) |
| Heard list | Recently-heard local stations used by iGate gating. | [`../../pkg/igate/heard.go`](../../pkg/igate/heard.go) |
| Third-party encap | APRS `}` third-party packet wrapper. | [`../../pkg/igate/third_party.go`](../../pkg/igate/third_party.go) |
| TNC2 monitor format | Standard text format for monitored frames. | [`../../pkg/igate/tnc2.go`](../../pkg/igate/tnc2.go) |

## Hardware / OS

| Term | In this project | Pointer |
|---|---|---|
| PTT | Push-to-talk keying. Methods: serial RTS/DTR, CM108 USB HID, GPIO chardev (Linux), rigctld. Driving on Rust; enumeration on Go. | [`../handbook/ptt.html`](../handbook/ptt.html); see [code-map.md](code-map.md) |
| CM108 USB HID GPIO | USB sound-card chip with a HID GPIO pin used for PTT. Three platform-specific drivers. | [`../../graywolf-modem/src/cm108.rs`](../../graywolf-modem/src/cm108.rs), `tx/ptt_cm108_*.rs` |
| GPIO chardev (Linux) | Modern `/dev/gpiochipN` v2 ioctl interface (gpiocdev crate). | [`../../graywolf-modem/src/tx/ptt_gpio_linux.rs`](../../graywolf-modem/src/tx/ptt_gpio_linux.rs) |
| rigctld / hamlib | TCP daemon that drives a radio's CAT interface; PTT via `T 1\n` / `T 0\n`. | [`../../graywolf-modem/src/tx/ptt_rigctld.rs`](../../graywolf-modem/src/tx/ptt_rigctld.rs) |
| Digirig | USB-serial sound interface, PTT via RTS/DTR. | [`../../graywolf-modem/src/tx/ptt_unix.rs`](../../graywolf-modem/src/tx/ptt_unix.rs), [`../handbook/configurations.html`](../handbook/configurations.html) |
| AIOC (All-In-One-Cable) | CM108-class USB cable; udev rule grants hidraw access. | [`../../packaging/udev/99-graywolf-cm108.rules`](../../packaging/udev/99-graywolf-cm108.rules), [`../handbook/configurations.html`](../handbook/configurations.html) |
| udev rule | Linux device-permission rule shipped in deb/rpm/AUR for hidraw access. | [`../../packaging/udev/99-graywolf-cm108.rules`](../../packaging/udev/99-graywolf-cm108.rules) |
| systemd unit | Service unit shipped with deb/rpm; runs as user `graywolf`, listens `0.0.0.0:8080`. | [`../../packaging/systemd/graywolf.service`](../../packaging/systemd/graywolf.service) |
| GPS (gpsd) | Reads positions from a local gpsd daemon. | [`../../pkg/gps/gpsd.go`](../../pkg/gps/gpsd.go), [`../handbook/gps.html`](../handbook/gps.html) |
| GPS (serial NMEA) | Reads positions directly from a serial GPS speaking NMEA. | [`../../pkg/gps/serial.go`](../../pkg/gps/serial.go), [`../handbook/gps.html`](../handbook/gps.html) |
| CPAL | Upstream Rust audio crate; cross-platform live audio I/O. | integration in [`../../graywolf-modem/src/audio/soundcard.rs`](../../graywolf-modem/src/audio/soundcard.rs) |

## Project-internal

| Term | What it is | Pointer |
|---|---|---|
| `graywolf` | The Go binary (and the project name). | [`../../cmd/graywolf/`](../../cmd/graywolf/) |
| `graywolf-modem` | The shipped Rust DSP daemon binary. | [`../../graywolf-modem/src/bin/graywolf_modem.rs`](../../graywolf-modem/src/bin/graywolf_modem.rs) |
| `graywolf-demod` | The Rust **crate** name. The binary it produces is named `graywolf-modem`. | [`../../graywolf-modem/Cargo.toml`](../../graywolf-modem/Cargo.toml) |
| `modembridge` | Go-side IPC consumer + child-process supervisor. | [`../../pkg/modembridge/`](../../pkg/modembridge/) |
| `txgovernor` | Centralized TX rate limiter / dedup / priority queue. See [invariant 16](invariants.md). | [`../../pkg/txgovernor/`](../../pkg/txgovernor/) |
| `historydb` | Standalone position-history SQLite. | [`../../pkg/historydb/`](../../pkg/historydb/), [`../handbook/history-database.html`](../handbook/history-database.html) |
| `configstore` | Config SQLite via GORM (pure Go, no cgo). | [`../../pkg/configstore/`](../../pkg/configstore/) |
| `mapsauth` | Client for `auth.nw5w.com` (per-device bearer token). | [`../../pkg/mapsauth/`](../../pkg/mapsauth/) |
| `mapscache` | On-disk PMTiles cache for offline maps. | [`../../pkg/mapscache/`](../../pkg/mapscache/) |
| `releasenotes` | Embedded release-note YAML + parser; powers the in-app "What's new" popup. | [`../../pkg/releasenotes/`](../../pkg/releasenotes/) |
| `updatescheck` | Daily GitHub releases poll. | [`../../pkg/updatescheck/`](../../pkg/updatescheck/) |
| `packetlog` | In-memory ring of RX/TX/IS records with filter-query API. | [`../../pkg/packetlog/`](../../pkg/packetlog/) |
| `logbuffer` | `slog.Handler` tee that writes every record at DEBUG into a circular SQLite ring (`graywolf-logs.db`), separate from the config and history DBs. Env-aware path (tmpfs on Pi/SD-card hosts, disk elsewhere); feeds the future `graywolf flare` diagnostic submission. | [`../../pkg/logbuffer/`](../../pkg/logbuffer/) |
| `stationcache` | Heard-station cache (memory + persistent). | [`../../pkg/stationcache/`](../../pkg/stationcache/) |
| `webapi` / `webauth` / `webtypes` | REST API, auth primitives, shared schemas. | [`../../pkg/webapi/`](../../pkg/webapi/), [`../../pkg/webauth/`](../../pkg/webauth/), [`../../pkg/webtypes/`](../../pkg/webtypes/) |
| `ipcproto` | Generated Go bindings for the proto. | [`../../pkg/ipcproto/`](../../pkg/ipcproto/) |
| `ingress.Source` | In-process RX-frame provenance tag. See [invariant 17](invariants.md). | [`../../pkg/app/ingress/source.go`](../../pkg/app/ingress/source.go) |
| `txbackend` | Per-channel TX backend dispatcher (KISS-as-backend, modem-as-backend). | [`../../pkg/app/txbackend/`](../../pkg/app/txbackend/) |
| Workspace shim | The root `Cargo.toml`; see [invariant 1](invariants.md). | [`../../Cargo.toml`](../../Cargo.toml) |
| `notes.yaml` | The in-app release-notes data file (embedded into the Go binary). | [`../../pkg/releasenotes/notes.yaml`](../../pkg/releasenotes/notes.yaml) |
| `VERSION` | Repo-root authoritative version file. Read by Makefile and Rust `build.rs`. | [`../../VERSION`](../../VERSION) |
| `graywolf.db` / `graywolf-history.db` / `graywolf-logs.db` | Config, history, and log-ring SQLite files at runtime (gitignored). | see [system-topology.md](system-topology.md) |

## Diagnostics

| Term | In this project | Pointer |
|---|---|---|
| Flare | A schema-versioned diagnostic submission carrying a graywolf user's config, hardware, devices, and recent logs. Sent by `graywolf flare` (Plan 2b) to `graywolf-flare-server` (Plan 2c). | [`../../.context/2026-04-25-graywolf-flare-system-design.md`](../../.context/2026-04-25-graywolf-flare-system-design.md) |
| Wire schema (flareschema) | The canonical Go struct tree at `pkg/flareschema/` defining the JSON document a flare submission carries. | [`../../pkg/flareschema/`](../../pkg/flareschema/), [`../../docs/flareschema/v1.json`](../../docs/flareschema/v1.json) |
| `graywolf flare` CLI | The user-facing subcommand that collects a diagnostic flare from the local install, scrubs it, presents a review TUI, and submits to graywolf-flare-server. | [`../../cmd/graywolf/flare.go`](../../cmd/graywolf/flare.go) |
| Redact rule | One regex + replacement pair in `pkg/diagcollect/redact/rules.go`. Each rule has a positive and a negative test fixture; coverage is enforced by `rules_test.go`. | [`../../pkg/diagcollect/redact/rules.go`](../../pkg/diagcollect/redact/rules.go) |
| Pending flare | A flare body the CLI saved to `~/.local/state/graywolf/pending-flare-<unix-ts>.json` (mode 0600) after the server returned 5xx. The operator retries by re-running `graywolf flare`. | [`../../pkg/diagcollect/submit/store.go`](../../pkg/diagcollect/submit/store.go) |
