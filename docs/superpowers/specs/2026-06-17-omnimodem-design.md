# Omnimodem — Design

**Date:** 2026-06-17
**Status:** Approved for planning (direction approved; review feedback incorporated)

## Summary

Omnimodem is a gRPC-driven, batteries-included multi-mode software modem written
in Rust. A single binary is operated entirely over gRPC and can run multiple
amateur-radio modes simultaneously, each bound to its own audio interface and
PTT. It generalizes the reception techniques that make the Graywolf AFSK
demodulator best-in-class into mode-agnostic building blocks so that adding a new
mode is cheap, and it fixes the audio-device-identity and PTT robustness gaps
that were the weakest parts of Graywolf.

Three design goals drive everything:

1. **Easy to add new modes** — reusable DSP/FEC "batteries" and a one-module mode
   registry, so a new mode touches one place, not five `match` arms.
2. **Easy to integrate with** — a stable, versioned gRPC API so developers build
   their own frontends in any language.
3. **Rock-solid audio detection and PTT** — stable device identity across
   renames/hotplug, structured errors, and a transmitter you can trust.

Plus useful metrics (SNR, signal level, bad frames, DCD, duty cycle, etc.).

## Architecture

A hard line separates an **async control edge** from a **synchronous DSP/audio/PTT
core**. This preserves Graywolf's correct decision never to async-ify the hot
path (Graywolf's core is plain `std::thread`, no tokio — and it works).

- **Control edge** (tonic + tokio): gRPC handlers only. They validate requests
  and translate them into commands sent over an `mpsc` into the sync core.
  Events flow back out through a `tokio::broadcast` that fans out to subscriber
  streams. No DSP runs here.
- **Sync core** (plain threads): owns audio, demod/mod, and PTT. No async in the
  sample path.

Four concepts:

- **AudioInterface** — owns one audio device (capture and/or playback).
- **Mode instance** — pure DSP demod/mod for one mode.
- **Client** — a gRPC consumer (frontend).
- **Channel** — ties them together: `audio → demod → frames + metrics` on RX,
  and `frame → mod → audio + PTT` on TX.

A **Supervisor** owns the live channels, the device cache, the persistence store,
and the shared PTT registry. (Graywolf's `struct Modem` already plays this role
for in-memory state — `modem/mod.rs:139`; Supervisor is its evolution, not a
greenfield invention.)

### Concurrency note

Multi-channel concurrency is **not new** — Graywolf already runs multiple
channels concurrently by processing every channel on a single thread in
`pump_all_audio` (`modem/mod.rs`). Omnimodem moves to **per-channel RX/TX worker
threads** instead. This is a deliberate choice of parallelism (better for many
channels × ensemble demods on multi-core) over Graywolf's single-thread
simplicity, made consciously because best-of-class reception runs several
demodulators per channel. The only cross-channel shared state is the PTT
registry and the device cache.

## gRPC control surface

A `ModemControl` service:

- **Unary RPCs** for command-and-control (configure audio / channel / PTT,
  start/stop, transmit, query state) — each with acknowledgements and request
  validation.
- **Server-streaming `SubscribeEvents`** for RX frames, DCD transitions, status
  changes, audio levels, and metrics, with a **state-snapshot replay on
  subscribe** so a reconnecting client is never stale.

Graywolf already has a working custom protobuf-over-UDS/TCP IPC
(`ipc/server.rs`) but it is **single-client by design** (`ipc/server.rs:18-19`).
gRPC is chosen specifically to serve goal #2: polyglot codegen and standard
streaming let third parties build frontends without reimplementing a bespoke
framing protocol. The existing `proto/graywolf.proto` is a good starting
vocabulary to lift.

**Backpressure policy (must specify, not hand-wave).** `tokio::broadcast`
returns `Lagged(n)` and silently drops messages for slow receivers. Decoded RX
frames must **never** be silently dropped. Policy:

- **Frames: lossless.** Per-client bounded queue; on overflow, either buffer or
  disconnect the client — never discard a decoded frame.
- **Telemetry (levels, metrics): lossy.** Dropping intermediate samples is fine;
  only the latest value matters.

**Local authorization (transmitting is a legal act).** Opening the control
socket means the ability to key a transmitter under the operator's license, so
authz is required even on the default local transport, not only when routable:

- Default transport: **UDS** (or TCP loopback). On UDS, enforce socket-file mode
  and `SO_PEERCRED` peer-uid checks. Note explicitly that loopback TCP exposes
  every local user.
- **mTLS + per-method authz is mandatory** if the service is ever bound to a
  routable interface.

**API versioning.** Because third-party frontends are the whole point, publish a
stability/versioning policy for the proto from day one (semver on the package,
additive-only within a major).

## Mode framework

`trait Demodulator` / `trait Modulator`, each declaring a `ModeCaps`
(native sample rate, bandwidth, TX support, duplex). A **mode registry** means
adding a mode touches one module instead of ~5 `match` sites — directly fixing
Graywolf's #1 weakness (ad-hoc enum + flat config struct). Per-mode config is
**parametric**: `enum ModeConfig { Afsk{..}, G3ruh9600{..}, Psk{..}, Rtty{..},
Ft8{..} }`, not one flat struct.

### Streaming AND block/windowed modes — first-class from day one

This is the most important correction from review. Every Graywolf demod (AFSK,
PSK, 9600) emits hard-sliced bits **directly** into per-slicer `HdlcDecoder`s —
there is no symbol or soft-bit interface (`demod_afsk.rs:637-644`). A trait whose
contract is "feed samples, get HDLC frames" fits the continuous/HDLC family but
**cannot express FT8/JS8/WSPR**, which are an early target. WSJT-X-family modes
are:

- **windowed and time-aligned** (e.g. 15 s slots), decoded multi-pass over a
  whole buffer, producing multiple decodes per window;
- **FEC-heavy and soft-decision** (LDPC + Costas sync), with **no HDLC**;
- **dependent on an accurate clock**.

So the mode abstraction supports two demod shapes from the start:

1. **Streaming demod** — `feed(samples) -> Vec<Frame>` (AFSK/PSK/RTTY/9600).
2. **Block/windowed demod** — buffers a time-aligned window and runs a
   multi-pass decode, returning multiple decodes (FT8/JS8/WSPR).

And the pipeline carries **soft information (LLRs)** end-to-end, not just hard
bits — otherwise the listed Viterbi/LDPC batteries are useless and FEC-mode
reception is not best-of-class. Frame assembly (HDLC and friends) moves
downstream of the demod rather than living inside it.

### Best-of-class reception, generalized

`ParallelDemodulator<D>` generalizes Graywolf's standout multi-decoder ensemble
("hydra") into a reusable pattern for any mode: run N decoder configurations in
parallel and union/dedup their outputs. Note carefully:

- The **pattern** generalizes; the **specific profiles do not.** Graywolf's
  Profile A (no-limit / hard-limit) + Profile B FM-discriminator are
  AFSK-specific. PSK's diversity axis is different (loop bandwidths, etc.). The
  registry composes per-mode ensembles, not one fixed profile set.
- Dedup-by-`(content, sample-offset)` within a ~3-symbol window
  (`demod_afsk_multi.rs`) is the HDLC-streaming mechanism. Windowed modes dedup
  by decoded-message + time-slot instead.

### Batteries-included DSP/FEC toolkit

Filters, resampler, PLL/Costas, AGC, FFT/Goertzel; HDLC, FX.25, IL2P,
Reed-Solomon, Viterbi, LDPC, varicode. Mode authors draw from these instead of
reinventing them. The resampler also closes Graywolf's "every source must
natively match the demod rate" gap, and per-mode `ModeCaps` rates turn
Graywolf's global 48 kHz ceiling into a per-mode capability so SDR-rate modes
coexist with 48 kHz AFSK.

### Honest scope note

Extracting the batteries is a **real refactor, not a lift-and-shift.** Today the
AFSK demod owns `Vec<HdlcDecoder>` and frames internally, DCD scoring is
copy-pasted between AFSK and 9600 (`demod_afsk.rs:691-723` and
`modem_9600/mod.rs:172-182`), and PSK/9600 share no code with AFSK. The reusable
`MultiSlicer` / `DcdScorer` / `DpllClockRecovery` / `ParallelDemodulator<D>`
abstractions do not exist yet and threading soft decisions through is genuine
Phase-2 work. The implementation plan should budget for this.

## Audio subsystem (goal #3)

Lift Graywolf's hardened bits — defensive I16 format selection,
stream-rebuild-with-backoff, submitted/drained TX watermarks, ALSA
canonicalization pure-functions, `probe_capture`, in-use device caching
(all in `audio/soundcard.rs`). Fixes:

- **A real `trait AudioBackend`** (cpal / file / stdin / SDR / JACK pluggable)
  replacing Graywolf's ad-hoc spawn-functions dispatched by a `match`
  (`modem/mod.rs:449-480`).
- **A unified cross-platform `DeviceId`** collapsing Graywolf's two diverging
  enumeration paths (cpal/ALSA `list_audio.rs` vs nusb `list_usb.rs`) and their
  per-OS identity mismatch into one stable identity derived from durable
  attributes (USB `idVendor:idProduct` + serial, ALSA stable card *name* not
  index, USB port-topology as fallback; prefer `/dev/serial/by-id/` symlinks).
- **Resampling** so a source rate need not match the demod rate. Crucially, this
  must **retain** Graywolf's ALSA `plughw` format/rate hardening
  (`audio/mod.rs`, `soundcard.rs`) — the 48 kHz ceiling exists to avoid ALSA's
  synthetic-rate `plughw` trap that desyncs bit timing. Resampling is additive;
  it does not replace that defensive selection.
- **Capture fan-out** — one capture stream can feed several demods (1200 + 9600
  on the same audio, or SDR slices). Opt-in; 1:1 is the default. (Graywolf's
  `extra_demods` already proves this works.)

## PTT subsystem (goal #3)

Lift the whole PTT subsystem — `trait PttDriver` + factory + `PortRegistry`
(multi-handle) + per-OS drivers + `drive_tx_cycle` no-sleep sequencing +
unkey-on-Drop (`tx/ptt.rs`, `modem/tx_worker.rs`). Fixes:

- **Structured `PttError` enum** replacing the stringly-typed `Result<(),
  String>` errors (`ptt.rs:184-189`), so callers can distinguish
  device-went-away vs permission-denied vs busy.
- **Hotplug eviction for serial / CM108.** `PortRegistry` currently caches serial
  fds by path string and never evicts them (`ptt.rs:484-487` documents this as a
  known limitation); GPIO has `LineGone` eviction, serial does not. Since
  "rock-solid PTT" is a primary goal, the `DeviceId` work must **evict and reopen
  by `DeviceId` on device disappearance/hotplug**, not only resolve at startup.
- **RX/TX interlock per channel.** When a channel keys PTT on a shared device, RX
  decode on that device must be muted/skipped to avoid decoding our own
  transmission or feedback. Graywolf handles this implicitly on its single
  thread; the per-channel-thread model must make it explicit.

## Persistence

Config is persisted in a **SQLite** file owned by the modem (frontends are
arbitrary external clients, so config must outlive any single client and be
shared across them). Key rules:

- **Key config on the stable `DeviceId`, never on the volatile `/dev` path**, so a
  TNC that jumps `ttyUSB0 → ttyUSB1` still binds. At startup and on hotplug the
  core resolves each stored `DeviceId` to its current device node.
- **Keep SQLite off the DSP hot path.** Writes happen on the control edge or a
  dedicated thread — never in the audio pump — so a disk hiccup can't become an
  audio underrun.
- **`SuggestUdevRule(device_id)` RPC** returns ready-to-install udev rule text
  (keyed on vendor/product/serial or topology, producing a stable
  `/dev/omnimodem/<label>` symlink) plus instructions — useful for two identical
  adapters that `by-id` can't disambiguate. The modem only *suggests*; it never
  writes to `/etc/udev` (root-owned; operator stays in control).

## TX model

- **Cooperative queue + optional exclusive lease.** TX frames from any client
  queue on the channel's TX worker and serialize on-air. Sessions that can't
  tolerate interleaving (contest/Winlink) take an **optional exclusive TX lease**.
- **Per-channel TX worker** (improvement over Graywolf's single global TX worker
  in `tx_worker.rs`, which needlessly serializes TX across independent radios).
  **Rule:** two channels that share one physical rig must still serialize via the
  shared PTT registry — concurrency is per-rig, not per-channel.
- **Time-slot-aligned scheduling** for windowed modes: the TX worker must be able
  to transmit precisely on the next even/odd slot boundary (e.g. FT8's 15 s
  grid), not "as soon as queued."

## Time synchronization

WSJT-X-family modes require an accurate system clock. Omnimodem depends on the
host clock being disciplined (NTP/PTP) and **surfaces clock offset as a metric**
so operators can see when decode failures are a time-sync problem rather than a
signal problem.

## Metrics (goal #4)

Per-channel over gRPC plus an optional Prometheus exporter: SNR, dBFS level, DCD
state, good/bad-FCS counts, PTT state, duty cycle. Additional high-value metrics:

- **Which ensemble member / slicer decoded each frame** (Graywolf has this data;
  it's gold for tuning the hydra).
- **AFC / frequency-error offset.**
- **Audio over/underrun and clip counts.**
- **Clock offset** (for WSJT-X modes; see Time synchronization).

## Other features

- **Record/replay to FLAC** plus a **decode-rate regression harness** over a known
  corpus — serves the best-of-class-reception goal and guards DSP regressions.
- **Reference CLI/TUI client.**
- **KISS/AGWPE compatibility** is **out of scope for the core**: it belongs in a
  separate external KISS↔gRPC translator process (future work), so existing TNC
  apps work without polluting the core.

## Resolved decisions

1. **Language: Rust.** (Maximizes Graywolf reuse.)
2. **Persistence: SQLite**, keyed on stable `DeviceId`, with `SuggestUdevRule`
   for the device-rename problem.
3. **Build order:** (1) modem service structure — gRPC + control plane + PTT +
   audio I/O, provable end-to-end via loopback/level-metering and a real
   key/unkey before any DSP exists; (2) the audio building blocks — DSP/FEC
   toolkit + mode framework (streaming **and** block paths, soft-decision
   plumbing); (3) modes — 1200 AFSK first, then FT8, then outward.
4. **Multi-client TX:** cooperative queue + optional exclusive lease.
5. **KISS/AGWPE:** external translator, not in the core (future work).

## Graywolf reuse map — the AFSK "secret sauce" as mode-agnostic batteries

Lift these out of the AFSK demod so PSK/RTTY/9600/FT8 inherit them. All verified
present in the Graywolf source:

- **Profile ensemble ("hydra")** — Profile A no-hard-limit, Profile A +
  hard-limit, Profile B FM-discriminator run in parallel, outputs unioned
  (`demod_afsk_multi.rs`) → generic `ParallelDemodulator<D>` (pattern only;
  profiles are AFSK-specific).
- **Multi-slicer** — N slicers (default 9) deciding mark-vs-space at different
  thresholds via the geometric space-gain table (`demod_afsk.rs:425-433, 530`) →
  generic `MultiSlicer`.
- **Decision-feedback AGC** (independent mark/space reference tracking, W7ION's
  technique) **and** peak/valley envelope AGC (fast-attack/slow-decay) — note
  these are **mutually exclusive** in Graywolf: DFB runs only in single-slicer
  mode (`demod_afsk.rs:507`), multi-slicer uses peak/valley. Model them in the
  registry as alternatives selected by slicer count, not as two independently
  composable batteries.
- **Hard-limiter-before-bandpass** correlator stage — `sign(x)`, keep
  zero-crossing timing (`demod_afsk.rs:459-461, 545-547`).
- **Digital PLL clock recovery** with locked-vs-searching inertia
  (`demod_afsk.rs:626-678`) → generic `DpllClockRecovery`.
- **DCD scoring with hysteresis** (shift-register popcount on/off thresholds,
  `demod_afsk.rs:691-723`) → generic `DcdScorer` (currently duplicated in
  `modem_9600`).
- **Content+offset frame dedup** windowed to ~3 symbol times
  (`demod_afsk_multi.rs`).
- **SIMD-friendly 8-accumulator FIR** (`demod_afsk.rs:67-107`).
- **256-entry cos/sin oscillator lookup tables** — note these are **`f32`
  floating-point** tables indexed by the top 8 bits of a phase accumulator
  (`demod_afsk.rs:40-60`), not fixed-point as previously described.

W7ION (Ion Todirel) attribution for the decision-feedback AGC, the hard-limiter
correlator, and the hydra idea carries over into omnimodem's code, same as
Graywolf.

## Phasing

1. **Foundations + vertical slice** — workspace, lift DSP/HDLC, mode framework
   (streaming + block traits, soft-decision plumbing), 1200 AFSK with the
   ensemble end-to-end over gRPC + cpal + PTT. Prove gRPC backpressure and local
   authz here.
2. **Audio/device hardening** — `AudioBackend`, unified `DeviceId`, resampling,
   capture fan-out, hotplug + serial-PTT eviction.
3. **Breadth + observability** — 9600 / PSK31 / RTTY → WSJT-X family (exercising
   the block/windowed + time-slot-TX paths); metrics / Prometheus; record/replay
   + regression harness.
4. **Integration + safety** — reference TUI, TX exclusive lease, mTLS for
   routable binds. (KISS/AGWPE translator is separate future work.)
