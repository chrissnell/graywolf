# C program analysis and Go porting

**Date:** March 31, 2026  
**Original workspace:** direwolf  
**Chat ID:** `132e2654-0cf9-4c03-9fdd-c3c8f716ea26`

---

## User

Please analyze this C program.  Would it be possible to port this to Go?  Would it still be performant?

---

## Assistant

I'll start by exploring the codebase to understand the structure and nature of this C program.

The user wants me to analyze a C program and evaluate whether it could be ported to Go while maintaining performance. First, I need to understand what this C program is. The workspace is at `/Users/csnell/dev/direwolf`, which sounds like it could be the "Dire Wolf" project - a software TNC (Terminal Node Controller) for packet radio / APRS. Let me explore the codebase to understand its structure and nature.

## Analysis of Dire Wolf

**Dire Wolf** (Decoded Information from Radio Emissions for Windows Or Linux Fans) is a ~115,000-line C software TNC (Terminal Node Controller) for amateur packet radio / APRS. It replaces a dedicated hardware TNC by using a PC sound card and DSP. Here's a breakdown of its major subsystems and an assessment of a Go port.

---

### Architecture Overview

The program is organized around a **pipeline** of threads and queues:

1. **Audio input** (one thread per sound device) reads samples via ALSA, PortAudio, Windows Wave API, or UDP
2. **Demodulation** runs per-sample DSP: FIR filtering, AFSK/PSK/9600-baud demod, PLL clock recovery, HDLC bit extraction
3. **Decoded frames** are pushed through `dlq` (a mutex+condvar frame queue) for serial processing
4. **Protocol handling**: AX.25 link layer, APRS encode/decode, FX.25/IL2P FEC, digipeater, IGate, beaconing
5. **Transmit** threads (one per channel) pull from `tq`, generate modulated audio, control PTT
6. **Network interfaces**: KISS (TCP/serial) and AGW TCP servers connect to client applications

### Key Technical Characteristics

| Aspect | Details |
|--------|---------|
| **Size** | ~95K lines in `src/`, ~20K in vendored `external/` |
| **Build** | CMake, targets Linux/macOS/Windows/FreeBSD |
| **Threading** | pthreads (Unix) / Win32 threads; mutex+condvar queues |
| **Audio backends** | ALSA, PortAudio, sndio, Windows Wave, UDP |
| **Hardware I/O** | Serial ports, GPIO (libgpiod), CM108 HID, parallel port |
| **DSP** | All floating-point (`float`), FIR via `convolve`, 256-entry trig LUTs, Goertzel, phase-accumulator DDS |
| **No SIMD** | No intrinsics or inline assembly; relies on compiler autovectorization hints (`aligned(16)`, `__restrict__`, `-ffast-math`) |

---

## Could It Be Ported to Go?

**Yes, it is feasible**, but it is a substantial undertaking with several areas requiring careful attention. Here's a domain-by-domain assessment:

### What Ports Naturally

- **Protocol logic** (~40% of the codebase): AX.25 framing, APRS encode/decode, KISS/AGW protocol, digipeater rules, IGate, beaconing, config parsing. These are essentially string/byte manipulation and state machines. Go excels here — cleaner error handling, built-in networking, and easier concurrency with goroutines/channels would likely produce *better* code.

- **Networking**: The KISS TCP server, AGW server, and client tools are straightforward socket code. Go's `net` package and goroutine-per-connection model would simplify this significantly.

- **Threading/queues**: The `dlq` and `tq` queue pattern maps directly to Go channels. The current pthread mutex+condvar synchronization would become idiomatic channel operations with less boilerplate.

- **Configuration**: The 6,200-line `config.c` parser is tedious but conceptually simple line-by-line parsing — straightforward to port.

### What Requires Careful Work

- **Audio I/O**: There's no standard Go audio library equivalent to ALSA or PortAudio. You'd need to either:
  - Use CGo bindings to PortAudio/ALSA (adds CGo overhead on every callback)
  - Use a pure-Go audio library like `gordonklaus/portaudio` (which is itself CGo)
  - Implement a raw ALSA interface via syscalls (ambitious but possible)
  - Focus on UDP SDR input (already supported; trivial in Go)

- **Serial/GPIO/HID for PTT**: These require platform-specific system calls. Go libraries exist (`go-serial`, `periph.io` for GPIO) but coverage varies. CM108 HID would need a CGo binding to hidapi or a pure-Go HID library.

- **C-specific idioms** that need rethinking:
  - **`union` of modem state** (`fsk_demod_state.h`) — would become an interface or separate structs
  - **Signed 32-bit integer overflow** for PLL timing — Go integer overflow is well-defined for unsigned but signed overflow is undefined behavior territory; needs explicit masking
  - **`memmove` delay lines** — would use Go slices with copy or ring buffers
  - **`volatile` for cross-thread mute flags** — would use `atomic` package
  - **Packed structs** (`rpack.h`) — would use `encoding/binary`

### The Performance Question

This is the critical consideration. Here's a nuanced breakdown:

**The hot path**: For each audio sample (at 44,100 or 48,000 Hz, per channel), Dire Wolf runs:
1. Multiple FIR convolutions (`convolve` — multiply-accumulate over filter taps)
2. Phase-accumulator LO mixing with table lookup
3. `hypotf` for envelope detection
4. PLL clock recovery
5. HDLC bit processing
6. Optionally 8 parallel Goertzel updates (DTMF)

This is **O(samples × channels × subchannels × filter_taps)** floating-point work per second.

**Go vs C performance on this workload:**

| Factor | Impact |
|--------|--------|
| **FIR inner loop** | Go's compiler generates decent but not great floating-point code. Expect **~1.3-2× slower** than GCC/Clang with `-O2 -ffast-math`. Go doesn't do fast-math reassociation or aggressive autovectorization. |
| **GC pauses** | The DSP path allocates almost nothing on the heap (static state arrays, stack locals). A well-written Go port using pre-allocated slices would have negligible GC pressure. **Not a problem.** |
| **Function call overhead** | The C code uses `__attribute__((always_inline))` and `hot` extensively. Go's compiler does inline small functions but is less aggressive. Tight per-sample call chains may see overhead. |
| **Goroutine scheduling** | For the receive thread (pinned to a core, processing samples continuously), a goroutine with `runtime.LockOSThread()` would behave similarly to a pthread. **Comparable.** |
| **Memory access patterns** | The `memmove`-per-sample delay line pattern is already suboptimal in C. A Go port using ring buffers could actually **improve** cache behavior. |
| **`unsafe` escape hatch** | For truly hot paths, Go's `unsafe` package and `math` intrinsics allow close-to-C performance where needed. |

**Bottom line on performance**: On a modern desktop/laptop CPU, a Go port would likely run **1.5–2× slower** in the DSP hot path. Since Dire Wolf on a Raspberry Pi 4 already handles multiple channels with headroom, a Go port on any modern x86 machine would almost certainly keep up with real-time audio rates. The real risk is on **very constrained hardware** (RPi Zero, older ARM boards) where the margin is already thin.

### Practical Recommendation

A pragmatic approach would be a **hybrid architecture**:

1. **Pure Go** for everything above the DSP layer: protocol handling, networking, configuration, queues, application logic (~60% of the code). This would be cleaner, safer, more maintainable, and benefit from Go's ecosystem.

2. **CGo or pure Go with careful optimization** for the DSP core. You could:
   - Start with pure Go DSP and benchmark against real-time requirements
   - Keep a C DSP library callable via CGo as a fallback
   - Use Go assembly (`.s` files) for the FIR inner loop if needed — Go supports per-platform assembly

3. **CGo wrappers** for audio I/O (PortAudio) and hardware interfaces (hidapi) where pure Go alternatives don't exist.

This gets you the maintainability and concurrency benefits of Go where they matter most, while preserving performance where it's critical.

### Summary

| Dimension | Assessment |
|-----------|------------|
| **Feasibility** | Yes — no fundamental blockers |
| **Effort** | Large (~115K lines, many subsystems, 3 platform targets) |
| **Protocol/networking** | Would be *improved* by Go |
| **Concurrency model** | Goroutines+channels are a natural fit |
| **DSP performance** | ~1.5-2× slower in pure Go; sufficient for desktop, risky on low-end ARM |
| **Hardware I/O** | Requires CGo or platform-specific libraries |
| **Biggest risk** | Audio I/O latency through CGo; DSP performance on constrained hardware |
| **Biggest win** | Dramatically simpler concurrency, cross-compilation, memory safety, single binary deployment |

Would you like me to dive deeper into any specific subsystem, prototype a particular component in Go, or create a more detailed porting plan?

Now I have a comprehensive understanding of the Dire Wolf codebase. Let me synthesize this into a thorough analysis for the user, covering what the program does, and then a detailed assessment of porting to Go with performance considerations.

---

## User

what about Rust?  would it be any better there?

---

## Assistant

Great question. Rust is actually a significantly better fit for this codebase than Go in several important ways. Here's the comparison:

## Rust vs Go for Dire Wolf: Where Rust Wins

### Performance — The Big One

This is where Rust pulls decisively ahead. The DSP hot path that concerned us with Go becomes a non-issue:

| Factor | Go | Rust | C (baseline) |
|--------|-----|------|---------------|
| **FIR inner loop** | ~1.5-2× slower | **~1.0× (parity)** | 1.0× |
| **Compiler backend** | Custom Go compiler | **LLVM (same as Clang)** | GCC/Clang |
| **Fast-math** | Not available | **`-C target-feature`, `#[cfg]` per-fn** | `-ffast-math` |
| **Autovectorization** | Minimal | **Full LLVM vectorizer (SSE/AVX/NEON)** | Full |
| **Inlining control** | Limited | **`#[inline(always)]`, LTO** | `__attribute__((always_inline))` |
| **GC pauses** | Rare but possible | **None — no GC** | None |
| **Zero-cost abstractions** | No (interfaces have vtable cost) | **Yes (monomorphization, traits)** | N/A (manual) |

Rust with LLVM can produce essentially **identical machine code** to Clang-compiled C for numerical loops. The `FindCPUflags.cmake` in Dire Wolf already targets SSE on x86 — Rust's LLVM backend does the same autovectorization with the same flags. You could even use `std::arch` for explicit SIMD intrinsics if needed, without dropping to assembly.

This means a Rust port would be **performant even on a Raspberry Pi**, where Go would be marginal.

### C-Specific Patterns Map Naturally

The C idioms I flagged as tricky for Go are mostly straightforward in Rust:

- **`union` of modem state** — Rust has native `union` (unsafe) or, better, `enum` (tagged union) which is safer and idiomatic. A `DemodState` enum with `Afsk(AfskState)`, `Psk(PskState)`, `Baseband(BasebandState)` variants is exactly the right abstraction.

- **Signed 32-bit overflow for PLL** — Rust's `i32::wrapping_add()` / `Wrapping<i32>` gives the same defined overflow behavior, explicitly and safely.

- **`__attribute__((aligned(16)))`** — Rust has `#[repr(align(16))]`, direct equivalent.

- **`__restrict__` on FIR pointers** — Rust's ownership model inherently prevents aliasing. The compiler can assume `&mut` references don't alias, giving the same optimization benefit automatically.

- **`volatile` for cross-thread flags** — `AtomicBool` / `AtomicI32` with explicit memory ordering.

- **Packed structs** — `#[repr(packed)]`, `#[repr(C)]` for wire-format compatibility.

- **Pointer arithmetic / buffer manipulation** — Slices with bounds checking (optimized away in release when the compiler can prove safety), or `unsafe` blocks for the rare case where you need raw pointer math.

- **`memmove` delay lines** — Same options as C (or ring buffers), but Rust's `copy_within` on slices is the direct equivalent.

### Memory Safety Without Sacrifice

Dire Wolf is a long-running daemon processing untrusted radio input — exactly the scenario where memory safety matters. The AX.25/APRS decode path (`decode_aprs.c` at 5,200+ lines) parses complex, variable-format data from over the air. In C, this is a fertile ground for buffer overflows. Rust eliminates this entire class of bugs at compile time, with no runtime cost.

The `unsafe` blocks needed for hardware I/O would be small and auditable — the vast majority of the codebase would be safe Rust.

### Concurrency Model

Rust doesn't have goroutines/channels built in, but offers equivalent (and arguably better) tools:

- **Threads**: `std::thread` maps 1:1 to pthreads, same as Dire Wolf's current model. No runtime scheduler overhead.
- **Channels**: `crossbeam::channel` or `std::sync::mpsc` directly replace `dlq` and `tq`. Crossbeam's channels are faster than Go's.
- **Async**: For the network servers (KISS TCP, AGW), `tokio` or `async-std` provide efficient async I/O without thread-per-connection overhead. Though thread-per-connection is fine at Dire Wolf's scale.
- **Fearless concurrency**: The compiler prevents data races at compile time. Dire Wolf's `mute_input[]` volatile array accessed from multiple threads? Rust simply won't compile unless you use `AtomicBool` or a `Mutex`.

### Ecosystem for Hardware I/O

| Subsystem | Rust Ecosystem |
|-----------|---------------|
| **Audio (ALSA)** | `alsa-rs` (safe bindings), `cpal` (cross-platform, like PortAudio but pure Rust API) |
| **Audio (PortAudio)** | `portaudio-rs` (CGo-equivalent FFI) |
| **Audio (cross-platform)** | **`cpal`** — well-maintained, covers ALSA/CoreAudio/WASAPI/JACK |
| **Serial ports** | `serialport-rs` — mature, cross-platform |
| **GPIO** | `gpio-cdev` (Linux gpiod), `rppal` (Raspberry Pi specific) |
| **HID (CM108)** | `hidapi-rs` (bindings to hidapi) |
| **DNS-SD** | `zeroconf` or `mdns` crates |
| **GPS** | `nmea` crate for parsing, `gpsd_client` for gpsd |

`cpal` is particularly relevant — it's the de facto Rust audio I/O library and would replace the three platform-specific `audio*.c` files with a single cross-platform abstraction, no FFI overhead.

## Where Rust is Harder Than Go

It's not all upside. Some honest trade-offs:

| Concern | Details |
|---------|---------|
| **Learning curve** | Rust's ownership/borrowing model has a steep learning curve. The Dire Wolf contributor community is ham radio operators who know C — Rust is a bigger ask than Go. |
| **Compile times** | A 100K-line Rust project compiles significantly slower than equivalent C or Go. Incremental builds help, but full rebuilds are painful. |
| **Porting effort** | Slightly harder than Go for the protocol logic — Rust demands more thought about ownership of packet buffers, frame lifetimes, etc. (But the resulting code is more correct.) |
| **String handling** | C's casual `char*` manipulation in `config.c` and `decode_aprs.c` becomes more verbose in Rust (`&str` vs `String`, UTF-8 validation). |
| **Rapid prototyping** | Go is faster to write quick-and-dirty working code. Rust makes you get it right upfront. |
| **Cross-compilation** | Both are excellent here, but Go edges ahead for trivially producing static binaries for odd targets. Rust's `cross` tool is good but slightly more setup. |

## Head-to-Head Summary

| Dimension | C (current) | Go | Rust |
|-----------|-------------|-----|------|
| **DSP performance** | Baseline | 1.5-2× slower | **~Parity** |
| **Memory safety** | None | GC-safe, not memory-safe | **Compile-time safe** |
| **Concurrency safety** | Manual (error-prone) | Good (race detector) | **Best (compile-time)** |
| **Hardware I/O** | Native | CGo required | FFI or good native crates |
| **Binary size/deploy** | Small + libc | **Single static binary** | Single static binary (with musl) |
| **Contributor accessibility** | High (ham community knows C) | **Medium-high** | Medium-low |
| **Porting effort** | N/A | Large | **Large** (but higher quality result) |
| **Constrained HW (RPi Zero)** | Works | Risky | **Works** |
| **Long-term maintainability** | Low (manual memory mgmt) | Good | **Best** |

## Recommendation

If the goal is a **production-quality, long-term replacement** — Rust is the clear winner. You get C-equivalent performance, memory safety on untrusted radio input, compile-time concurrency guarantees, and a good hardware I/O ecosystem. The resulting code would be more maintainable and more secure than the C original.

If the goal is to **get something working quickly** with a smaller team, or if the contributor community's familiarity matters more than raw performance — Go is the pragmatic choice, with the caveat that you may need CGo for the DSP path on constrained hardware.

If you're seriously considering this, a reasonable starting point in Rust would be porting the **AFSK demodulator** (`demod_afsk.c`) and benchmarking it against the C version. That would validate the performance claim on your target hardware and give you a feel for how the patterns translate. Want me to prototype that?

The user is asking about porting Dire Wolf to Rust instead of Go. Let me think about this comprehensively based on what I already know about the codebase from the previous analysis. I don't need to re-explore the codebase — I have a thorough understanding from the previous turn. Let me reason through the Rust comparison.

Key considerations:
1. Performance: Rust compiles to native code via LLVM, same backend as Clang. It supports `-ffast-math` equivalent (`-C target-feature`, `fast-math` intrinsics). No GC. This is a huge advantage over Go for DSP.
2. Memory safety without GC — perfect for this use case


3. Now I need to think through how Rust's concurrency model compares, its FFI capabilities for C interop, what audio libraries are available, the actual effort involved in porting, cross-platform considerations, and how the existing C patterns in Dire Wolf would translate to idiomatic Rust.

---

## User

I have FLAC files that contain test data for demodulation tests.  How could I use these with a mod_afsk.c/rust equivalent head to head test

---

## Assistant

Let me look at how Dire Wolf already handles audio file input and its existing test infrastructure — there's likely an `atest` tool that does something close to what you need.

The user wants to create a head-to-head benchmark comparing the C and Rust AFSK demodulators using FLAC files as test input. Let me understand the existing test infrastructure first - Dire Wolf likely has some test tooling already (like `atest`). Let me look at how the existing code reads audio files and how `atest` works, since that's probably the closest existing tool to what they need.

Good news — Dire Wolf already has a solid test harness (`atest`) that's almost exactly what you need. It reads WAV files, runs them through the full demod chain, counts decoded packets, and reports timing. The challenge is just bridging FLAC input and setting up the Rust side to be comparable. Here's a practical approach:

## The Existing Pipeline

Dire Wolf's `atest` tool already does this:

```
WAV file → audio_get() → demod_get_sample() → multi_modem_process_sample()
              ↓                                         ↓
         raw bytes                              int16 [-32768..32767]
                                                        ↓
                                                demod_afsk_process_sample()
                                                        ↓
                                                   hdlc_rec_bit()
                                                        ↓
                                                   decoded frames
```

It reports: packets decoded, wall-clock time, and a "× realtime" ratio. That's exactly the metric you want for a benchmark.

## Strategy: Shared Raw PCM, Separate Demod Engines

The cleanest approach avoids embedding FLAC decoding in either test harness. Instead, use a **three-stage pipeline**:

### Stage 1: Convert FLAC to raw PCM (once, offline)

```bash
# Decode to raw signed 16-bit little-endian PCM
ffmpeg -i test_data.flac -f s16le -acodec pcm_s16le test_data.raw

# Or if you want WAV (for the C side's atest as-is):
ffmpeg -i test_data.flac test_data.wav

# Get the sample rate and channel count for reference:
ffmpeg -i test_data.flac 2>&1 | grep "Audio:"
```

This gives you:
- **`.wav`** — feed directly to `atest` unchanged
- **`.raw`** (headerless s16le PCM) — trivial to read from both C and Rust without any library dependency

### Stage 2: C benchmark (existing `atest`, minimal changes)

`atest` already handles WAV, so you can run it directly:

```bash
# Build direwolf
cd /Users/csnell/dev/direwolf && mkdir -p build && cd build && cmake .. && make atest

# Run against your WAV files
time ./atest -B 1200 -P A ../test_data.wav
# Output: "N packets decoded in X.XXs, Y.Y × realtime"
```

If you want to benchmark with raw PCM instead (to remove WAV parsing from the timing), a small wrapper that reads raw s16le and calls `demod_afsk_process_sample` directly would be ~50 lines of C.

### Stage 3: Rust benchmark harness

Here's the architecture for a minimal Rust test harness that's a fair comparison:

```rust
use std::fs::File;
use std::io::Read;
use std::time::Instant;

// Option A: Read raw s16le PCM directly (no dependencies)
fn read_raw_pcm(path: &str) -> Vec<i16> {
    let mut file = File::open(path).unwrap();
    let mut bytes = Vec::new();
    file.read_to_end(&mut bytes).unwrap();
    bytes.chunks_exact(2)
        .map(|chunk| i16::from_le_bytes([chunk[0], chunk[1]]))
        .collect()
}

// Option B: Read FLAC directly (add `claxon` crate - pure Rust, no C deps)
fn read_flac(path: &str) -> (Vec<i16>, u32) {
    let mut reader = claxon::FlacReader::open(path).unwrap();
    let sample_rate = reader.streaminfo().sample_rate;
    let bits = reader.streaminfo().bits_per_sample;
    let samples: Vec<i16> = reader.samples()
        .map(|s| {
            let s = s.unwrap();
            // Normalize to 16-bit range
            (s >> (bits - 16)) as i16
        })
        .collect();
    (samples, sample_rate)
}

fn main() {
    let (samples, sample_rate) = read_flac("test_data.flac");
    // Or: let samples = read_raw_pcm("test_data.raw");

    // Initialize demodulator (your Rust port of demod_afsk_init)
    let mut demod = AfskDemodulator::new(sample_rate, 1200, 1200, 2200);
    let mut packets_decoded = 0;

    let start = Instant::now();
    for &sample in &samples {
        // This is the function you're benchmarking — your Rust port of
        // demod_afsk_process_sample + hdlc_rec_bit
        if let Some(frame) = demod.process_sample(sample as i32) {
            packets_decoded += 1;
        }
    }
    let elapsed = start.elapsed();

    let file_duration = samples.len() as f64 / sample_rate as f64;
    println!("{} packets decoded in {:.3}s ({:.1}× realtime)",
        packets_decoded,
        elapsed.as_secs_f64(),
        file_duration / elapsed.as_secs_f64()
    );
}
```

### What "Fair" Looks Like

For the comparison to be meaningful:

| Aspect | C side | Rust side | Notes |
|--------|--------|-----------|-------|
| **Input** | Same `.raw` or `.wav` samples | Same `.raw` or `.flac` samples | Identical PCM data |
| **Demod algorithm** | `demod_afsk_process_sample` | Faithful Rust translation | Same filter coefficients, same PLL constants |
| **HDLC** | `hdlc_rec_bit` | Rust equivalent | Same bit-stuffing / CRC logic |
| **Output metric** | Packets decoded + wall time | Packets decoded + wall time | Must decode the **same** packets |
| **Compiler flags** | `-O2 -ffast-math` | `--release` + `lto = true` | Comparable optimization |
| **Warmup** | Load file, then time demod only | Same | Don't time I/O |

### Recommended Rust `Cargo.toml`

```toml
[package]
name = "direwolf-demod-bench"
version = "0.1.0"
edition = "2021"

[dependencies]
claxon = "0.4"       # Pure Rust FLAC decoder (no C deps)

[profile.release]
opt-level = 3
lto = "fat"
codegen-units = 1
target-cpu = "native" # Match C's -march=native
```

Build with:
```bash
RUSTFLAGS="-C target-cpu=native" cargo build --release
```

### Measuring Apples to Apples

A good test script would look like:

```bash
#!/bin/bash
FLAC_FILE="$1"
WAV_FILE="${FLAC_FILE%.flac}.wav"
RAW_FILE="${FLAC_FILE%.flac}.raw"

# Pre-convert once
ffmpeg -y -i "$FLAC_FILE" "$WAV_FILE" 2>/dev/null
ffmpeg -y -i "$FLAC_FILE" -f s16le -acodec pcm_s16le "$RAW_FILE" 2>/dev/null

echo "=== C (atest) ==="
# Run 5 times, take median
for i in {1..5}; do
    ./build/atest -B 1200 "$WAV_FILE" 2>&1 | tail -1
done

echo ""
echo "=== Rust ==="
for i in {1..5}; do
    ./target/release/direwolf-demod-bench "$FLAC_FILE"
done
```

### Correctness Before Performance

Before benchmarking speed, verify both decoders produce **identical output**. Have both dump decoded frame bytes to a file and diff:

```bash
# C side: modify atest (or use -h for hex dump) to write frames to file
./build/atest -B 1200 -h test.wav 2>&1 | grep "^[0-9a-f]" > frames_c.txt

# Rust side: dump frames the same way
./target/release/direwolf-demod-bench --dump-frames test.flac > frames_rust.txt

diff frames_c.txt frames_rust.txt
```

If the frames differ, the benchmark is meaningless — you'd be comparing different algorithms.

### What to Port First

The minimum Rust code for a working benchmark is a translation of these functions:

1. **`demod_afsk_init`** — filter coefficient generation, PLL setup (~200 lines)
2. **`demod_afsk_process_sample`** — the per-sample hot loop (~300 lines)
3. **`hdlc_rec_bit`** — bit-to-frame assembly with CRC (~400 lines)
4. **`dsp.c: gen_lowpass/gen_bandpass`** — FIR filter kernel generation (~150 lines)

That's roughly **1,000-1,200 lines of C** to port — a manageable chunk that contains the entire performance-critical path.

Want me to start on the Rust port of `demod_afsk_process_sample`, or set up the benchmark harness structure first?

Now I have a clear picture. The user has FLAC files with test data, but Dire Wolf only supports WAV. Let me outline a practical approach for head-to-head testing. The key insight is:

1. Both C and Rust need to read the same audio samples from FLAC files
2. They need to run through equivalent demod pipelines
3. We need to measure both correctness (same frames decoded) and performance (time)

Let me design a practical test harness approach.

---

## User

create a document that I can feed to an agent that explains what we're working with and specifies the work to be done to create a rust port and conduct a head to head test

---

## Assistant

Let me read the key source files so I can include accurate details in the document.

The user wants me to create a comprehensive document that can be fed to an AI coding agent. This document needs to thoroughly explain the Dire Wolf codebase (specifically the AFSK demodulation path), the architecture, the C code patterns, and specify the work to create a Rust port and benchmark harness.

Let me read the key source files that the agent will need to understand, so I can include the right level of detail in the document.

Now let me read the remaining key files needed for the document.

Now let me also read the key supporting files: dsp.c, hdlc_rec.c (the parts we need), and the atest.c to understand the test harness more fully.

Let me grab a few more key pieces needed for the document.

Now let me also quickly check the atest.c file and the direwolf.h for the key constants.

Now I have all the details needed to create a comprehensive document. Let me also check the constants in audio.h/direwolf.h for MAX_SUBCHANS and MAX_SLICERS.

Now I have everything needed. Let me create the document.

I've created `RUST_PORT_SPEC.md` at the repository root. It's a comprehensive, self-contained document (~500 lines) that an agent can use to execute the full porting effort. Here's what it covers:

**Part 1 — The C Codebase** (what it's working with):
- Repository layout and key file paths
- All relevant constants and defaults
- Complete Profile A and B signal flow diagrams
- Every hot-path function documented with its algorithm and C signature
- The demodulator state struct layout (including the tricky union, unsigned overflow PLL, aligned buffers)
- HDLC frame extraction logic and DCD scoring
- The existing `atest` harness and how it works
- The CI test pattern from `check-modem1200`

**Part 2 — Specification of Work** (what to build):
- Rust crate structure with exact file layout
- `Cargo.toml` with dependencies and release profile
- Detailed porting instructions for each module in dependency order
- Specific guidance on C-to-Rust pitfalls (wrapping arithmetic, signed overflow, popcount)
- Benchmark binary spec with FLAC/WAV/raw input support
- `bench.sh` script for side-by-side comparison
- Correctness criteria (byte-identical frames, handling FP rounding edge cases)
- Performance targets (within 1.5× of C, ideally parity)
- Porting guidelines (faithfulness first, then optimize)
- Optimization roadmap (ring buffers, SIMD, PGO — deferred until after correctness)

**Part 3 — Reference** — All C function signatures in one place

**Part 4 — Test Data** — How to handle FLAC files in both Rust and C, plus synthetic test generation

**Summary** — 13 prioritized work items as a task checklist

---

