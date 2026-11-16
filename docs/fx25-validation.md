# FX.25 validation record

Base: upstream/main `bde16a1752ef96e8907aea9dc18c698e3e96a9d7`.
Reference: Dire Wolf `eda1383f5fa9d8ba3cb27f99db1d2c79494404c9`.
See [implementation/provenance](fx25.md), [fixture README](../graywolf-modem/testdata/fx25/README),
and [operator test instructions](fx25-hardware-test.md).

## Passed locally

| Check | Result |
|---|---|
| Exact independent wire vectors | 33/33; all standard tags 1–11, short/boundary/maximum, binary endings |
| Graywolf audio → real Dire Wolf atest | 33/33 forced-tag cases plus 4/4 production-mode cases (auto/16/32/64), exact bytes, exactly one delivery |
| Real Dire Wolf gen_packets → Graywolf audio RX | 33/33, exact bytes, one delivery |
| Symbol-damaged independent Dire Wolf tone recordings → Graywolf | 11/11 require FEC recovery, one delivery; parity/2 corrupted data symbols |
| `cargo test --release --lib fx25` | 26 pass, including production dispatch (single/triple), mixed plain/FEC repeats, full parity/PTT airtime, noise/reset/error/rejection tests |
| `cargo test --release --lib il2p` | 30 pass; shared RS source unchanged |
| `cargo check --workspace` and default-target `cargo clippy --workspace -- -D warnings` | Pass on Linux x86_64 |
| `cargo check --workspace --target x86_64-pc-windows-gnu` | Pass; existing unused `Read` warning in demod_ipc_bench |
| `cargo check --lib --target aarch64-linux-android` | Pass; this is Android's shared library target, not the desktop CLI binary |
| Linux ARM64 Rust modem / offline utility and Go app cross-build | Pass; Bookworm glibc 2.36 sysroot; startup smoke under QEMU (not physical acceptance) |
| Go FX.25 validation / independent length-vector tests | Pass |
| Go vet native and `GOOS=linux GOARCH=arm GOARM=6 go vet ./...` | Pass |
| `make docs-check docs-lint api-client-check` | Pass |
| Web unit tests / build / API TypeScript check | 550 pass / build succeeds / type check succeeds |

`.github/workflows/fx25.yml` runs focused offline tests natively on Linux,
Windows and macOS plus an Android library target check. Check the actual
workflow results on the fork branch; a configured job is not evidence of a pass.
The initial `27ca74b5` build passed native Linux/Windows/macOS CI; follow-up
changes repeat that matrix. Android host-stub FX.25 tests pass 27 cases,
including the separate Android TX dispatch helper. Its dispatcher now uses
the same FX.25 builder as desktop and the shared verified RX metadata mapper.
An APK, macOS installation, Windows sound device and Android radio/PTT have
not been physically tested here. The shared implementation is not Pi-only.

## Demonstrated recovery beyond ordinary HDLC/FCS

The same independently generated recordings are fed sample-for-sample to two
otherwise identical recommended triple ensembles (same profiles, nine slicers
per profile, clock recovery and FCS policy). One has FX.25 reception enabled;
the comparison has only the FEC path disabled by a **test-only** switch.

| Recording | Verified FX.25 deliveries | Ordinary HDLC/FCS-only deliveries |
|---|---:|---:|
| `direwolf-damaged.wav` (11 packets) | **11/11, byte-for-byte correct** | **0/11** |
| `direwolf.wav` clean control (33 packets) | 33/33 | 33/33 |

The damaged packets use every standard tag and exactly parity/2 corrupted data
symbols (8, 16 or 32) before Dire Wolf tone generation. Every recovered damaged
packet has a nonzero correction count. Both clean and repaired results retain
FX.25 metadata and are delivered once. The ordinary path's clean control
proves that it is operational rather than disabled wholesale.

Reproduce:

```sh
cargo test --release --lib fx25_recovers_independent_audio -- --nocapture
```

This demonstrates FEC recovery of packets that the same modem's ordinary
HDLC/FCS path discards on these synthetic fixtures. It does **not** establish
RF sensitivity gains, comparative performance against Dire Wolf on arbitrary
noise/recordings, or on-air performance. The separate interoperability checks
use real Dire Wolf, not this internal comparison.

## Broad checks with reproduced upstream failures

`cargo test --release --workspace` reaches the library suite and fails on
five existing tests: the unavailable soundcard output test, three blocked
Unix socket tests, and `rxonly::tests::feed_chunk_decodes_at_least_one_frame_from_reference_track`
(the reference FLAC is not present). The same five fail on untouched upstream
in a separate detached worktree. No tests were disabled or weakened.
The Android host-stub suite similarly passes its additional tests and hits
those five environment failures.

`go test -race ./...` passes the other packages but fails
`TestSupervisor_ConnectOnly_DialsExistingSocket` because this environment
prohibits the test's Unix socket. Reproduced with `go test -race ./pkg/modembridge`
on untouched upstream. Configstore, web API, DTO and new FX.25 tests pass.

The additional `cargo clippy --workspace --all-targets -- -D warnings` check
reports `chunks_exact_to_as_chunks` in existing `tests/twist.rs` and
`tests/audio_level.rs` with Rust 1.100. These files are unchanged; the
repository-required default-target Clippy check passes.

## Representative host workload (not Pi performance)

Release offline utility, recommended triple demodulator, independent clean
Dire Wolf fixture (43.656 seconds per pass), AMD EPYC 9V74 x86_64 host:

| Passes | Packets | Audio seconds | Wall seconds | User + system CPU seconds | Peak RSS KiB |
|---:|---:|---:|---:|---:|---:|
| 1 | 33 | 43.656 | 1.738 | 1.73 | 1972 |
| 30 | 990 | 1309.665 | 53.457 | 53.37 | 3000 |

Commands: `time -v fx25_audio workload direwolf.wav 1` and the same with
`30`. This is a synthetic throughput/memory check; it establishes neither
Pi CPU usage nor RF/noise performance gains. The receiver block and dedup
histories have explicit bounds. Collect Pi 1-pass/30-pass measurements using
the supplied instructions before accepting the Pi workload requirement.

## Pending

All seven physical matrix rows in `fx25-hardware-test.md`, actual Pi CPU/RSS,
USB audio/serial PTT timing, external over-air decode in both directions,
ordinary AX.25 regression over RF, per-transmission delivery counts and
restart/configuration behavior remain pending operator evidence. Keep the
work on the fork only. Do not open an upstream PR until local testing is
complete and the operator explicitly requests it. No station was replaced and no transmission initiated.
