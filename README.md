<p align="center">
  <img src="assets/graywolf.svg" alt="Graywolf" width="300">
</p>

Graywolf is a modern APRS station with a software modem, digipeater, iGate, and web UI. It bundles everything you need to put a packet station on the air — from raw audio demodulation to APRS-IS gating — into a single binary with a browser-based configuration interface. Written by Chris Snell, NW5W. The modem began as a Rust port of the AFSK demodulator from [Dire Wolf](https://github.com/wb2osz/direwolf) by WB2OSZ.

> **Important**: Graywolf is an independent project. Please **do not** send support requests, bug reports, or feature requests for Graywolf to the Dire Wolf project. If you have issues with Graywolf, please file them [here](https://github.com/chrissnell/graywolf/issues).

## Features

- **Modern Web UI** - Configure and monitor your station from your browser, with live packet logs and preset-driven setup for digipeater and iGate

- **Software Modem** - Native Rust DSP, no external sound card tooling required

  - AFSK 1200 baud (Bell 202)
  - 9600 baud G3RUH
  - PSK
  - FX.25 and IL2P forward error correction
  - SDR input support

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
