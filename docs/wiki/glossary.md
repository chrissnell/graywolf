# Glossary

Domain terms as graywolf uses them. Each entry points at where the term
is implemented or canonically described in *this* project. For RF / APRS
background, the operator handbook is the starting point.

## Protocol / RF

| Term | In this project | Pointer |
|---|---|---|
| APRS | Amateur Packet Reporting System; positions, messages, telemetry over AX.25 UI frames. Parsing implemented in Go. | [`../../graywolf/pkg/aprs/`](../../graywolf/pkg/aprs/), handbook [`index.html`](../handbook/index.html) |
| AFSK (Bell 202, 1200 baud) | Standard RF modulation for VHF/UHF APRS. Demodulator in Rust; modulator in `tx/afsk_mod.rs`. | [`../../graywolf-modem/src/demod_afsk.rs`](../../graywolf-modem/src/demod_afsk.rs), [`../handbook/channels.html`](../handbook/channels.html) |
| 9600 G3RUH | 9600 baud baseband FSK with the G3RUH scrambler `x^17 + x^12 + 1`. | [`../../graywolf-modem/src/modem_9600/`](../../graywolf-modem/src/modem_9600/), [`../handbook/channels.html`](../handbook/channels.html) |
| PSK (QPSK 2400, 8-PSK 4800) | V.26/V.27 PSK modems with Costas + Gardner. | [`../../graywolf-modem/src/modem_psk/`](../../graywolf-modem/src/modem_psk/) |
| AX.25 | Amateur radio link-layer (UI frames only in this project). | [`../../graywolf/pkg/ax25/`](../../graywolf/pkg/ax25/) |
| KISS | TNC framing protocol (`FEND`/escape). KISS framing + TCP server + client + multi-port manager. | [`../../graywolf/pkg/kiss/`](../../graywolf/pkg/kiss/), [`../handbook/kiss.html`](../handbook/kiss.html), [`../handbook/remote-kiss-tnc.html`](../handbook/remote-kiss-tnc.html) |
| AGWPE | direwolf-compatible TCP TNC protocol. Subset implemented: `R/G/g/k/K/m/X/x/y/Y/V`. | [`../../graywolf/pkg/agw/protocol.go`](../../graywolf/pkg/agw/protocol.go), [`../handbook/agwpe.html`](../handbook/agwpe.html) |
| HDLC | NRZI bit stream, flag/abort, bit-stuffing, FCS-16. RX on Rust side has fix-bits retry. | [`../../graywolf-modem/src/hdlc.rs`](../../graywolf-modem/src/hdlc.rs), [`../../graywolf-modem/src/tx/hdlc_encode.rs`](../../graywolf-modem/src/tx/hdlc_encode.rs) |
| FX.25 | Reed-Solomon FEC over AX.25 (CCSDS poly), correlation-tag preamble. | [`../../graywolf-modem/src/fx25/`](../../graywolf-modem/src/fx25/) |
| IL2P | RS-coded header/payload framing with scrambler. | [`../../graywolf-modem/src/il2p/`](../../graywolf-modem/src/il2p/) |
| SDR (UDP audio bridge) | Listens on UDP for `s16le`/`f32le` audio from an external SDR. Selected via `source_type: sdr_udp` on the Audio Devices settings page. | [`../../graywolf-modem/src/sdr/`](../../graywolf-modem/src/sdr/), [`../../graywolf/web/src/routes/AudioDevices.svelte`](../../graywolf/web/src/routes/AudioDevices.svelte), [`../handbook/audio.html`](../handbook/audio.html) |
| Beacon | Scheduled transmit (position / object / tracker / custom / igate). Min-heap scheduler. | [`../../graywolf/pkg/beacon/`](../../graywolf/pkg/beacon/), [`../handbook/beacons.html`](../handbook/beacons.html) |
| Smart beacon | Speed/heading-aware beacon timing (mirrors direwolf SMARTBEACON / HamHUD). | [`../../graywolf/pkg/beacon/smartbeacon.go`](../../graywolf/pkg/beacon/smartbeacon.go), [`../handbook/beacons.html`](../handbook/beacons.html) |
| Digipeater | WIDEn-N / TRACEn-N digipeater with preemptive digi and per-channel dedup. | [`../../graywolf/pkg/digipeater/`](../../graywolf/pkg/digipeater/), [`../handbook/digipeater.html`](../handbook/digipeater.html) |
| iGate | Bidirectional APRS-IS gateway (RF<->IS). | [`../../graywolf/pkg/igate/`](../../graywolf/pkg/igate/), [`../handbook/igate.html`](../handbook/igate.html) |
| WIDEn-N | APRS digipeater path semantic; consumed by the digipeater. | [`../../graywolf/pkg/digipeater/`](../../graywolf/pkg/digipeater/), [`../handbook/digipeater.html`](../handbook/digipeater.html) |
| Mic-E | Compact APRS encoding for mobile rigs. | [`../../graywolf/pkg/aprs/mice.go`](../../graywolf/pkg/aprs/mice.go) |
| PHG | Power / antenna height / antenna gain encoding. | [`../../graywolf/pkg/aprs/phg.go`](../../graywolf/pkg/aprs/phg.go) |
| DAO | High-precision position augmentation. | [`../../graywolf/pkg/aprs/dao.go`](../../graywolf/pkg/aprs/dao.go) |
| DF | Direction-finding bearing/quality fields. | [`../../graywolf/pkg/aprs/df.go`](../../graywolf/pkg/aprs/df.go) |
| Peet Bros | Peet Bros weather-station serial format. | [`../../graywolf/pkg/aprs/peetbros.go`](../../graywolf/pkg/aprs/peetbros.go) |
| Telemetry | APRS telemetry packets and parameter coefficients. | [`../../graywolf/pkg/aprs/telemetry.go`](../../graywolf/pkg/aprs/telemetry.go) |
| Capabilities packet | APRS station-capabilities advertisements. | [`../../graywolf/pkg/aprs/capabilities.go`](../../graywolf/pkg/aprs/capabilities.go) |
| Object packet | APRS named-object advertisements. | [`../../graywolf/pkg/aprs/object.go`](../../graywolf/pkg/aprs/object.go) |
| Base-91 | Compressed-position encoding alphabet. | [`../../graywolf/pkg/aprs/base91.go`](../../graywolf/pkg/aprs/base91.go) |
| Callsign / SSID | Station identifier, optional `-N` suffix. | [`../../graywolf/pkg/callsign/parse.go`](../../graywolf/pkg/callsign/parse.go), [`../handbook/preferences.html`](../handbook/preferences.html) |
| Tactical callsign | Operator-chosen alias mapped to a real callsign for chat / display. | [`../../graywolf/pkg/messages/tactical_set.go`](../../graywolf/pkg/messages/tactical_set.go), [`../handbook/messaging.html`](../handbook/messaging.html) |
| Tactical chat invite | `!GW1 INVITE TAC` protocol for joining a tactical group. | [`../../graywolf/pkg/messages/invite.go`](../../graywolf/pkg/messages/invite.go) |
| APRS-IS passcode | Login auth code derived from callsign. | [`../../graywolf/pkg/callsign/passcode.go`](../../graywolf/pkg/callsign/passcode.go) |
| IS->RF filter | Rule engine for gating internet traffic to RF; deny-by-default ([invariant 15](invariants.md)). | [`../../graywolf/pkg/igate/filters/`](../../graywolf/pkg/igate/filters/), [`../handbook/igate.html`](../handbook/igate.html) |
| Server-side filter (`g/`) | Filter clause sent to the APRS-IS server to limit upstream feed. | [`../../graywolf/pkg/igate/server_filter.go`](../../graywolf/pkg/igate/server_filter.go) |
| Heard list | Recently-heard local stations used by iGate gating. | [`../../graywolf/pkg/igate/heard.go`](../../graywolf/pkg/igate/heard.go) |
| Third-party encap | APRS `}` third-party packet wrapper. | [`../../graywolf/pkg/igate/third_party.go`](../../graywolf/pkg/igate/third_party.go) |
| TNC2 monitor format | Standard text format for monitored frames. | [`../../graywolf/pkg/igate/tnc2.go`](../../graywolf/pkg/igate/tnc2.go) |

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
| GPS (gpsd) | Reads positions from a local gpsd daemon. | [`../../graywolf/pkg/gps/gpsd.go`](../../graywolf/pkg/gps/gpsd.go), [`../handbook/gps.html`](../handbook/gps.html) |
| GPS (serial NMEA) | Reads positions directly from a serial GPS speaking NMEA. | [`../../graywolf/pkg/gps/serial.go`](../../graywolf/pkg/gps/serial.go), [`../handbook/gps.html`](../handbook/gps.html) |
| CPAL | Upstream Rust audio crate; cross-platform live audio I/O. | integration in [`../../graywolf-modem/src/audio/soundcard.rs`](../../graywolf-modem/src/audio/soundcard.rs) |

## Project-internal

| Term | What it is | Pointer |
|---|---|---|
| `graywolf` | The Go binary (and the project name). | [`../../graywolf/cmd/graywolf/`](../../graywolf/cmd/graywolf/) |
| `graywolf-modem` | The shipped Rust DSP daemon binary. | [`../../graywolf-modem/src/bin/graywolf_modem.rs`](../../graywolf-modem/src/bin/graywolf_modem.rs) |
| `graywolf-demod` | The Rust **crate** name. The binary it produces is named `graywolf-modem`. | [`../../graywolf-modem/Cargo.toml`](../../graywolf-modem/Cargo.toml) |
| `modembridge` | Go-side IPC consumer + child-process supervisor. | [`../../graywolf/pkg/modembridge/`](../../graywolf/pkg/modembridge/) |
| `txgovernor` | Centralized TX rate limiter / dedup / priority queue. See [invariant 16](invariants.md). | [`../../graywolf/pkg/txgovernor/`](../../graywolf/pkg/txgovernor/) |
| `historydb` | Standalone position-history SQLite. | [`../../graywolf/pkg/historydb/`](../../graywolf/pkg/historydb/), [`../handbook/history-database.html`](../handbook/history-database.html) |
| `configstore` | Config SQLite via GORM (pure Go, no cgo). | [`../../graywolf/pkg/configstore/`](../../graywolf/pkg/configstore/) |
| `mapsauth` | Client for `auth.nw5w.com` (per-device bearer token). | [`../../graywolf/pkg/mapsauth/`](../../graywolf/pkg/mapsauth/) |
| `mapscache` | On-disk PMTiles cache for offline maps. | [`../../graywolf/pkg/mapscache/`](../../graywolf/pkg/mapscache/) |
| `releasenotes` | Embedded release-note YAML + parser; powers the in-app "What's new" popup. | [`../../graywolf/pkg/releasenotes/`](../../graywolf/pkg/releasenotes/) |
| `updatescheck` | Daily GitHub releases poll. | [`../../graywolf/pkg/updatescheck/`](../../graywolf/pkg/updatescheck/) |
| `packetlog` | In-memory ring of RX/TX/IS records with filter-query API. | [`../../graywolf/pkg/packetlog/`](../../graywolf/pkg/packetlog/) |
| `logbuffer` | `slog.Handler` tee that writes every record at DEBUG into a circular SQLite ring (`graywolf-logs.db`), separate from the config and history DBs. Env-aware path (tmpfs on Pi/SD-card hosts, disk elsewhere); feeds the future `graywolf flare` diagnostic submission. | [`../../graywolf/pkg/logbuffer/`](../../graywolf/pkg/logbuffer/) |
| `stationcache` | Heard-station cache (memory + persistent). | [`../../graywolf/pkg/stationcache/`](../../graywolf/pkg/stationcache/) |
| `webapi` / `webauth` / `webtypes` | REST API, auth primitives, shared schemas. | [`../../graywolf/pkg/webapi/`](../../graywolf/pkg/webapi/), [`../../graywolf/pkg/webauth/`](../../graywolf/pkg/webauth/), [`../../graywolf/pkg/webtypes/`](../../graywolf/pkg/webtypes/) |
| `ipcproto` | Generated Go bindings for the proto. | [`../../graywolf/pkg/ipcproto/`](../../graywolf/pkg/ipcproto/) |
| `ingress.Source` | In-process RX-frame provenance tag. See [invariant 17](invariants.md). | [`../../graywolf/pkg/app/ingress/source.go`](../../graywolf/pkg/app/ingress/source.go) |
| `txbackend` | Per-channel TX backend dispatcher (KISS-as-backend, modem-as-backend). | [`../../graywolf/pkg/app/txbackend/`](../../graywolf/pkg/app/txbackend/) |
| Workspace shim | The root `Cargo.toml`; see [invariant 1](invariants.md). | [`../../Cargo.toml`](../../Cargo.toml) |
| `notes.yaml` | The in-app release-notes data file (embedded into the Go binary). | [`../../graywolf/pkg/releasenotes/notes.yaml`](../../graywolf/pkg/releasenotes/notes.yaml) |
| `VERSION` | Repo-root authoritative version file. Read by Makefile and Rust `build.rs`. | [`../../VERSION`](../../VERSION) |
| `graywolf.db` / `graywolf-history.db` / `graywolf-logs.db` | Config, history, and log-ring SQLite files at runtime (gitignored). | see [system-topology.md](system-topology.md) |
