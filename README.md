<p align="center">
  <img src="assets/graywolf.svg" alt="Graywolf" width="300">
</p>

Graywolf is a modern APRS station with a software modem, digipeater, iGate, and web UI. It bundles everything you need to put an APRS station on the air — from raw audio demodulation to APRS-IS gating — and makes it easy with a browser-based configuration interface. 

**📖 [Read the Handbook](https://chrissnell.com/software/graywolf/)** — installation, configuration, operation guide, and REST API reference.

**🔧 [Known-Working Configurations](https://chrissnell.com/software/graywolf/configurations.html)** — community-tested hardware setups with exact settings. Check here for your device, and submit a PR if yours isn't listed.

**💬 [Graywolf APRS Discord](https://discord.gg/TYcHzgWf)** — community chat for help, discussion, and development.

Written by Chris Snell, [NW5W](https://nw5w.com). 

The modem is written in Rust and includes a port of the AFSK demodulator from [Dire Wolf](https://github.com/wb2osz/direwolf) by WB2OSZ.

The AX.25 decoding, APRS operatations (beacons, digipeater, and iGate), and the web API is handled by a service written in the Go programming language.

The web frontend was built in Svelte.

## Performance

Graywolf's AFSK demodulator **beats Direwolf's best published mode** on every track of the [WA8LMF TNC test CD](http://www.wa8lmf.net/TNCtest/), using about 1.6% of one CPU core. The gains come from a three-demodulator ensemble running in parallel and cross-deduped per-packet, combining Profile A (amplitude comparison), Profile A with a hard-limiter for flat-audio boost, and Profile B (FM discriminator) into a single output stream.

| WA8LMF Track | Direwolf `-P AD+` | Graywolf (default) | Δ |
|---|---:|---:|---:|
| 01 — 40-min flat traffic on 144.39 MHz | 1020 | **1026** | **+6** |
| 02 — 100 Mic-E bursts, de-emphasized | 1000 | 1000 | tie |
| 03 — 100 Mic-E bursts, flat | 100 | 100 | tie |
| 04 — 25-min drive test | 107 | **108** | **+1** |
| **Total** | **2227** | **2234** | **+7 (+0.3%)** |

Both binaries were run head-to-head via `bench.sh`, which drives the real `graywolf-modem` binary end-to-end through the IPC pipeline — not just the DSP library in isolation. The numbers reproduce what an operator sees at runtime.

```
$ ./bench.sh aprs-test-tracks/01_40-Mins-Traffic\ -on-144.39.flac 2
=== Direwolf -P AD+ (2 iterations) ===
1020 packets decoded in 76.930 seconds.  20.1 x realtime
1020 packets decoded in 79.315 seconds.  19.5 x realtime

=== graywolf-modem via IPC — ensemble=triple (default) — 2 iterations ===
1026 packets decoded in 31.659s (unique=855, ensemble=triple (default))
1026 packets decoded in 31.626s (unique=855, ensemble=triple (default))
```

**CPU-budget alternatives** are available via the `demod_ensemble` configuration knob — `"dual"` drops one demodulator for ~33% less CPU with only a 3-event Track 1 cost, `"single"` reproduces the legacy single-demodulator path for reference or low-power deployments.

Two of the three demodulators in the ensemble — the decision-feedback AGC and the hard-limiter-before-bandpass correlator variant — are based on designs published by **Ion Todirel (W7ION)** on the APRS Users Facebook group. His posts and [libmodem](https://github.com/iontodirel/libmodem) reference code were the source of both techniques.

## Features

<p align="center">
  <img src="docs/handbook/img/dashboard.png" alt="Graywolf dashboard" width="800">
</p>

- **Modern Web UI** - Configure and monitor your station from your browser, with live packet logs and preset-driven setup for digipeater and iGate

  - Imperial/Metric unit toggle for altitude, distance, and speed

- **Live Map** - Real-time APRS station map with trails, weather overlays, APRS-IS layer, and station popups with path and heard-via details

- **Messages** - SMS-style APRS messaging with unread badges, delivery status, and APRS-IS fallback

  - 1:1 direct messages with auto-ACK, retry, and reply-ack correlation
  - Tactical callsigns (e.g. `NET`, `EOC`) for group nets — broadcast to every monitor, color-coded sender labels per bubble
  - Tactical invites via `!GW1 INVITE <TAC>` DM — recipient clicks Accept to subscribe locally, no out-of-band coordination needed
  - Soft-split for messages longer than 67 chars, RF-first with IS fallback
  - Manage monitored tactical callsigns under *Messages → Manage tactical callsigns*

- **Software Modem** - Native Rust DSP, no external sound card tooling required

  - AFSK 1200 baud (Bell 202)
  - 9600 baud G3RUH
  - PSK
  - FX.25 and IL2P forward error correction
  - SDR input support

- **Push-to-Talk** - Multiple PTT methods for any setup

  - Serial RTS/DTR (Digirig, USB-serial adapters)
  - CM108 USB HID GPIO (AIOC, homebrew sound card adapters)
  - Linux GPIO (Raspberry Pi, BeagleBone)
  - Hamlib rigctld (CAT control)

- **Digipeater** - Full-featured APRS digipeater

  - WIDEn-N path handling
  - Preset-driven configuration (fill-in, wide-area, etc.)
  - Duplicate suppression
  - Per-path filtering

- **iGate** - Bidirectional APRS-IS gateway

  - RF → APRS-IS and APRS-IS → RF gating
  - Configurable filters
  - Packet origin tracking in logs

- **TNC Interfaces** - Speak the protocols other packet software expects

  - KISS TNC (serial and TCP)
  - AGWPE TCP interface

- **Beacons and GPS** - Position reporting made easy

  - Static and GPS-driven position beacons
  - Status and telemetry beacons
  - Configurable beacon intervals and paths

- **Observability**

  - [Prometheus](https://prometheus.io/) metrics
  - Packet logging to SQLite
  - Live packet stream in the web UI

- **Simple installation** - single binary, SQLite config database

  - systemd service unit
  - Debian/Ubuntu (APT), Red Hat (RPM), and Arch (AUR) packages
  - Windows installer (MSI)
  - Runs on x86-64 and ARM (Raspberry Pi)
