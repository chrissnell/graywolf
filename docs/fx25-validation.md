# FX.25 validation record

Base: upstream/main `bde16a1752ef96e8907aea9dc18c698e3e96a9d7`.
Reference: Dire Wolf `eda1383f5fa9d8ba3cb27f99db1d2c79494404c9`.
See [implementation/provenance](fx25.md), [fixture README](../graywolf-modem/testdata/fx25/README),
and [operator test instructions](fx25-hardware-test.md).

## Passed locally

| Check | Result |
|---|---|
| Exact independent wire vectors | 33/33; all standard tags 1–11, short/boundary/maximum, binary endings |
| Graywolf audio → real Dire Wolf atest | 33/33 forced-tag cases plus 4/4 production-mode cases (auto/16/32/64) and 4/4 oversized AX.25 fallback cases, exact bytes, exactly one delivery |
| Real Dire Wolf gen_packets → Graywolf audio RX | 33/33, exact bytes, one delivery |
| Symbol-damaged independent Dire Wolf tone recordings → Graywolf | 11/11 require FEC recovery, one delivery; parity/2 corrupted data symbols |
| `cargo test --release --lib fx25` | 27 pass, including production dispatch (single/triple), mixed plain/FEC repeats, full parity/PTT airtime, noise/reset/error/rejection tests |
| `cargo test --release --lib il2p` | 30 pass; shared RS source unchanged |
| `cargo check --workspace` and default-target `cargo clippy --workspace -- -D warnings` | Pass on Linux x86_64 |
| `cargo check --workspace --target x86_64-pc-windows-gnu` | Pass; existing unused `Read` warning in demod_ipc_bench |
| `cargo check --lib --target aarch64-linux-android` | Pass; this is Android's shared library target, not the desktop CLI binary |
| Linux ARM64 Rust modem / offline utility and Go app cross-build | Pass; Bookworm glibc 2.36 sysroot; startup smoke under QEMU (not physical acceptance) |
| Go configuration, TX metadata, packet-log and DCD reconciliation regression tests | Pass |
| Go vet native and `GOOS=linux GOARCH=arm GOARM=6 go vet ./...` | Pass |
| `make docs-check docs-lint api-client-check` | Pass |
| Web unit tests / build / API TypeScript check | 551 pass (557 in the combined PTT test build) / build succeeds / type check succeeds |

`.github/workflows/fx25.yml` runs focused offline tests natively on Linux,
Windows and macOS plus an Android library target check. Check the actual
workflow results on the fork branch; a configured job is not evidence of a pass.
The final implementation `fae73a81` passed native Linux/Windows/macOS CI
([run 43807290765](https://github.com/samwathegreat/graywolf/actions/runs/43807290765)). Android host-stub FX.25 tests pass 28 cases,
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
histories have explicit bounds. The operator supplied the Pi measurements below.

## Operator acceptance on the ARM64 Raspberry Pi station

The operator tested native USB audio and independent RTS/DTR serial PTT on a
disposable PacketRover installation (aarch64, Raspberry Pi OS/Debian Trixie,
glibc 2.41). Dire Wolf was stopped on that test station. Its PacketRover UI
remained available for sign-in; no PacketRover source change was made.
The separate KS9N-10 station ran real Dire Wolf 1.8.1 on Debian x86_64.
Its Graywolf application and tmux-managed Dire Wolf were paused for supervised
position-packet sends and restored afterward. Its configuration hash and ALSA
mixer snapshots were unchanged. No mixer levels were changed for these tests.

| Physical check | Operator evidence/result |
|---|---|
| Dire Wolf → native Graywolf | Ordinary AX.25 plus fixed 16, 32 and 64 parity received; native log showed tags 0x02, 0x06 and 0x0A with the expected parity |
| Repaired RF packets | Ambient receptions displayed nonzero repaired-symbol counts, including one and multiple symbols; this alone does not prove the same RF packets would fail an HDLC-only comparison |
| Native Graywolf → Dire Wolf | Off, Auto, 16, 32 and 64 decoded on the separate receiver; operator supplied its tmux output and confirmed each test |
| PTT | Operator confirmed actual keying and release; longer fixed-64 position packet decoded through its END64 marker |
| Duplicates/repeats | Controlled sends delivered once; deliberately identical fixed-64 sends produced two separate receptions |
| Restart/settings | Operator confirmed reception/transmission after restart and saved channel changes |
| TX packet-log metadata | Native TX rows showed FX.25 tag/parity; external decode remained the RF evidence |
| Oversized fallback / ordinary native TX | Operator confirmed complete END-FALLBACK payload decoded as ordinary AX.25, explicit local AX.25 fallback row, and ordinary native AX.25 TX |
| Final dispatch regression | Updated build installed; operator reported “pass, near zero delay” after the DCD reconciliation fix |

The long-frame receiver output did not independently print its correlation
tag. Do not treat the expected tag calculated from size as an observed remote
tag. Likewise, local metadata, API acceptance and counters are not proof of
RF delivery. The final delay result is an operator observation, not an
instrumented zero-millisecond latency measurement.

### Exact tested source

The feature branch remains separate from PTT PR #676. Disposable integration
builds were local merges, not permanent integration branches or upstream PRs.

| Build | FX.25 parent | PTT parent |
|---|---|---|
| b0e9a9c7e23467ba657134ac0bff99f82bc5a9b6 | c3741b996b5abd17cc03ff0ac9049a915f597ca1 | ddd72737e7da72c21130c850c405126be4b6e547 |
| e50ecfc3c204d9748c7092471037802c7d1ce875 | ba50845e5081abb2ebf96679c9aff96a61d4c6a5 | be9bd8b6dd996da2a27d64f28db553356cc27dc9 |
| d460a5df0588cb4afdeda8ba0cca7173f8e880d2 | fae73a8151a236f7bed9846ada9189bc91712c2a | be9bd8b6dd996da2a27d64f28db553356cc27dc9 |

Final integration tree: `fc41cbe0163000f0eb2244550924754a5fafefba`.
Delivered archive: `graywolf-fx25-ptt-linux-arm64-d460a5df.tar.gz`.
SHA256: `58186e1f37370cb0cbaeb286c7d5b13cf5fc2e950db042bc17ec4ecca64281d3`.
The archive includes exact source and BUILD provenance. It is a test build,
not a published Graywolf release.

The earlier combined build exposed queued frames delayed until a later RX
carrier event, then sent back-to-back. The supporting DCD fix is documented
in [implementation](fx25.md#carrier-sensing-and-queue-status); an isolated
regression reproduced the stale-state behavior on earlier source. It does
not bypass genuine busy-channel sensing. The final operator check passed.

## Operator Pi workload and upstream baseline

### Primary comparison: original upstream versus new FX.25 RX enabled

**The new decoder was benchmarked with FX.25 reception enabled.** These are
matched comparisons against original upstream on the same recordings, with
the same compiler/dependencies and recommended triple decoder ensemble:

| Audio fixture | Original upstream CPU (% of one core) | New, FX.25 RX enabled CPU (% of one core) | Relative CPU change |
|---|---:|---:|---:|
| Ordinary AX.25 | 16.70% | 16.18% | -3.11% |
| Clean FX.25 | 16.39% | 16.66% | +1.63% |

The relative changes use the unrounded CPU-seconds/audio-second values:
0.16697 → 0.16177 for ordinary audio and 0.16393 → 0.16661 for FX.25 audio.
For example, +1.63% is a relative CPU increase, not 1.63 percentage points of
an entire core. Small differences may include measurement noise; this does
not establish a speedup or a precise universal FEC overhead.

Original upstream can decode these **clean** FX.25 recordings through their
embedded ordinary AX.25 HDLC frames. That does not mean it has working FX.25
recovery. The separate [damaged-fixture comparison](#demonstrated-recovery-beyond-ordinary-hdlcfcs)
shows the functional benefit: new FX.25 RX recovered 11/11 packets versus
0/11 with the same new decoder ensemble using ordinary HDLC/FCS alone. That
recovery comparison is not an upstream CPU benchmark or an RF sensitivity test.

### Additional control: new code with RX FEC disabled versus enabled

The disabled runs isolate the incremental cost of the FEC receive path within
the new code. They supplement the upstream comparison above. “Disabled” here
means a **test-only RX FEC switch**, not the channel's FX.25 TX toggle. Native
FX.25 RX normally runs automatically on supported channels regardless of TX
settings.

| Audio fixture | New, RX FEC disabled CPU (% of one core) | New, RX FEC enabled CPU (% of one core) | Relative CPU change |
|---|---:|---:|---:|
| Ordinary AX.25 | 15.81% | 16.18% | +2.33% |
| Clean FX.25 | 16.11% | 16.66% | +3.39% |

The unrounded disabled CPU-seconds/audio-second values were 0.15809 and
0.16115, respectively. These small relative changes also include run-to-run
variation; do not interpret them as fixed overhead for every workload.

Peak memory in the same paired trials:

| Audio fixture | Original upstream peak RSS KiB | New, RX FEC disabled peak RSS KiB | New, RX FEC enabled peak RSS KiB |
|---|---:|---:|---:|
| Ordinary AX.25 | 4360 | 4628 | 4632 |
| Clean FX.25 | 4376 | 4760 | 4760 |

### Method, source and audio-duration normalization

The separately supplied benchmark package compared original upstream
`bde16a1752ef96e8907aea9dc18c698e3e96a9d7` with the b0e9a9c7 combined build.
Instrumented source commits were `075aecd92649c5d77aa2987129a6804ec84e749c`
(upstream) and `2a6ed24ec249bf77ec30de83951b1c00124d3bdc` (new). Exact source
archives and commands were included in that package, separate from the
feature fixtures. Each trial ran three passes and byte-verified 99 packets;
two rounds reversed mode order. CPU results above average those two rounds.
These measurements predate the later TX metadata and DCD fixes; they are not
new measurements of the final d460a5df integration build.

CPU percent of one core = 100 × (user + system CPU seconds) / audio seconds.
This measures offline receive processing, not whole-app CPU usage, TX cost
or a comparison of Graywolf CPU against Dire Wolf CPU.

The fixtures contain the same 33 payloads, but ordinary audio lasts 23.098
seconds and forced-tag FX.25 audio lasts 43.656 seconds per pass. The FX.25
recording therefore takes nearly twice as long to process at a similar CPU
cost per audio second. Compare normalized rates, not raw execution time
across different recordings. That fixture's 1.89× airtime is specific to its
forced-tag mix; it is not a general multiplier for Auto or everyday traffic.

### Longer offline workload

A separate recommended-triple-ensemble run processed the clean FX.25 fixture
without opening sound devices or PTT:

| Passes | Packets | Audio seconds | Wall seconds | User + system CPU seconds | Peak RSS KiB |
|---:|---:|---:|---:|---:|---:|
| 1 | 33 | 43.656 | 7.367 | 7.36 | 2968 |
| 30 | 990 | 1309.665 | 224.329 | 224.28 | 3992 |

No swaps; `vcgencmd get_throttled` reported `0x0`. Processing was about 5.8
times faster than real time. This is a separate run from the paired benchmark,
so its RSS should not be substituted into those comparisons. Two peak-RSS
measurements do not establish long-term memory stability or whole-application
resource usage.

## Remaining limits and publication status

Unsupported-rate rejection passed automated tests; a separate physical UI
rejection trial was not independently reported. No 300-baud, PSK, G3RUH,
Windows, macOS or Android RF/PTT acceptance is claimed. Android library
compilation is not an APK or device test. No controlled comparative RF/noise
sensitivity or TX waveform-quality benchmark against Dire Wolf was performed.
No claim of equal or superior overall modem performance follows from these
interoperability checks.

The required local RF checks have operator confirmation as described above.
Keep the contribution on the fork until the operator explicitly requests an
upstream PR. No upstream PR has been opened for this feature. Documentation
in this branch does not update the upstream published handbook automatically.
