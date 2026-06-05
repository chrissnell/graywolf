# Channel TX Test Signals Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the broken audio Test Tone feature and add a per-channel "Test TX" dropdown menu offering four TX test signals — station callsign in CW, a 1200 Hz tone, a 2400 Hz tone, and a 1200/2400 Hz alternating tone — each transmitted through the channel's real TX path (key PTT → audio → unkey).

**Architecture:** A new `TransmitTestSignal` IPC message carries `{channel, kind, callsign, freqs, duration…}` from Go to the Rust modem. A pure Rust `txtest` module synthesizes i16 PCM (CW via a Morse encoder, plus steady and alternating tone generators); the modem submits the samples as a `TxJob` to the existing TX worker, which keys/unkeys PTT automatically — exactly like `handle_transmit_frame`. The four UI options map to hardcoded parameter sets in one place in Go. The CW option resolves the centralized station callsign and refuses (422) to key the radio on an empty or N0CALL callsign; the three tones need no callsign.

**Tech Stack:** Rust (graywolf-modem, prost protobuf), Go (webapi + modembridge, protoc-gen-go), Svelte 5 (web UI, chonky-ui `DropdownMenu`), Protocol Buffers IPC.

**Reference spec:** `docs/superpowers/specs/2026-05-26-channel-cw-id-design.md`

**Branch:** `feature/channel-cw-id` (already created; the design + plan commits are its first commits).

---

## Build / regen reference (used by multiple tasks)

- **Regenerate Go protobuf bindings:** `make proto`
- **Regenerate OpenAPI spec:** `make docs`
- **Regenerate frontend API types:** `cd web && npm run api:generate`
- **Rust bindings** regenerate automatically on `cargo build` (graywolf-modem/build.rs runs prost on `proto/graywolf.proto`).
- **Rust tests:** `cargo test -p graywolf-modem`
- **Go tests (webapi + bridge):** `go test ./pkg/webapi/... ./pkg/modembridge/...`
- **Docs drift check:** `make docs-check`

> Note: `cfg(linux)` Rust cannot fully compile on the dev Mac (see project memory). `cargo test -p graywolf-modem` for the pure `txtest` module and host-buildable code works; full-modem on-target verification happens in CI / on-device. Where a step's Rust build is expected to need CI, the step says so.

---

# PART A — Remove Test Tone

Order matters: remove every consumer of the `PlayTestTone` / `TestToneResult` proto types **before** deleting them from the `.proto`, otherwise regeneration breaks the build.

## Task A1: Remove Test Tone from the Audio Interfaces UI

**Files:**
- Modify: `web/src/routes/AudioDevices.svelte`

- [ ] **Step 1: Delete the `playTestTone` function**

Remove this block (around line 66):

```javascript
  async function playTestTone(dev) {
    testingTone = dev.id;
    try {
      await api.post(`/audio-devices/${dev.id}/test-tone`);
      toasts.success(`Test tone played on "${dev.name}"`);
    } catch (err) {
      toasts.error(`Test tone failed: ${err.message}`);
    } finally {
      testingTone = null;
    }
  }
```

- [ ] **Step 2: Delete the Test Tone button**

Remove this block from the output-device actions (around line 405):

```svelte
          {#if dev.direction === 'output'}
            <Button
              variant="ghost"
              onclick={() => playTestTone(dev)}
              disabled={testingTone === dev.id}
            >
              {testingTone === dev.id ? 'Playing...' : 'Test Tone'}
            </Button>
          {/if}
```

- [ ] **Step 3: Delete the `testingTone` state declaration**

Run: `grep -n "testingTone" web/src/routes/AudioDevices.svelte`
Delete the remaining declaration line (e.g. `let testingTone = $state(null);`). After this, the grep must return nothing.

- [ ] **Step 4: Verify no references remain and the file builds**

Run: `grep -rn "testingTone\|test-tone\|Test Tone\|playTestTone" web/src/routes/AudioDevices.svelte`
Expected: no output.

Run: `cd web && npm run build`
Expected: build succeeds (no reference errors).

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/AudioDevices.svelte
git commit -m "Remove broken test tone button from audio interfaces UI"
```

## Task A2: Remove the Test Tone Go webapi handler

**Files:**
- Modify: `pkg/webapi/audio_devices.go`
- Modify: `pkg/webapi/dto/audio_device.go`
- Modify: `pkg/webapi/docs/op_ids.go`
- Modify: `pkg/webapi/audio_devices_test.go`

- [ ] **Step 1: Remove the route registration**

In `pkg/webapi/audio_devices.go`, delete the line:

```go
	mux.HandleFunc("POST /api/audio-devices/{id}/test-tone", s.playTestTone)
```

Also update the package/registration comment at the top of the file (line ~14) that lists `/{id}/test-tone` among the routes — remove `test-tone` from that list.

- [ ] **Step 2: Remove the `playTestTone` handler**

Delete the entire `playTestTone` method (the func starting around line 284, including its `// @...` swagger annotation block starting around line 267).

Run: `grep -n "playTestTone\|PlayTestTone\|test tone\|test-tone" pkg/webapi/audio_devices.go`
Expected: no output.

- [ ] **Step 3: Remove the DTO**

In `pkg/webapi/dto/audio_device.go`, delete the `TestToneResponse` type and its doc comment (around line 131). Read lines 131-136 first to match exactly.

- [ ] **Step 4: Remove the op-id constant**

In `pkg/webapi/docs/op_ids.go`, delete the line:

```go
	OpPlayTestTone              = "playTestTone"
```

- [ ] **Step 5: Remove test references**

Run: `grep -n "TestTone\|test-tone\|playTestTone\|PlayTestTone" pkg/webapi/audio_devices_test.go`
Delete any test functions or assertions that exercise the test-tone route/handler/DTO. Remove whole test funcs, do not skip them.

- [ ] **Step 6: Verify the package compiles and tests pass**

Run: `go build ./pkg/webapi/... && go test ./pkg/webapi/...`
Expected: PASS. (`pb.PlayTestTone` types still exist at this point — only Go webapi references are gone.)

- [ ] **Step 7: Commit**

```bash
git add pkg/webapi/audio_devices.go pkg/webapi/dto/audio_device.go pkg/webapi/docs/op_ids.go pkg/webapi/audio_devices_test.go
git commit -m "Remove test tone REST endpoint and DTO"
```

## Task A3: Remove Test Tone from the Go modem bridge

**Files:**
- Modify: `pkg/modembridge/requests.go`
- Modify: `pkg/modembridge/bridge.go`
- Modify: `pkg/modembridge/session.go`
- Modify: `pkg/modembridge/bridge_stop_test.go`

- [ ] **Step 1: Remove the `PlayTestTone` method**

In `pkg/modembridge/requests.go`, delete the entire `PlayTestTone` method (starts around line 86, ends at its closing brace before `dispatchEnumResponse`).

- [ ] **Step 2: Remove the tone dispatch hook**

In `pkg/modembridge/requests.go`, delete:

```go
func (b *Bridge) dispatchToneResponse(r *pb.TestToneResult) {
	b.toneDispatcher.Deliver(r.RequestId, r)
}
```

- [ ] **Step 3: Remove the dispatcher field and its init**

In `pkg/modembridge/bridge.go`, delete the struct field:

```go
	toneDispatcher *dispatcher[*pb.TestToneResult]
```

and the init line in `New`:

```go
		toneDispatcher: newDispatcher[*pb.TestToneResult](),
```

- [ ] **Step 4: Remove the session dispatch case**

In `pkg/modembridge/session.go`, delete:

```go
	case *pb.IpcMessage_TestToneResult:
		b.dispatchToneResponse(p.TestToneResult)
```

- [ ] **Step 5: Remove the stop-test case**

In `pkg/modembridge/bridge_stop_test.go`, delete the `PlayTestTone` test-case entry (around line 43-46), the `"tone": adaptPbTestToneResult(toneCh),` map entry (line ~100), the `adaptPbTestToneResult` helper (line ~127), and the standalone assertion (lines ~168-169). Read the file first to remove cleanly; remove whole entries, do not skip.

- [ ] **Step 6: Verify the package compiles and tests pass**

Run: `go build ./pkg/modembridge/... && go test ./pkg/modembridge/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/modembridge/
git commit -m "Remove test tone request path from modem bridge"
```

## Task A4: Remove Test Tone from the Rust modem

**Files:**
- Modify: `graywolf-modem/src/modem/mod.rs`
- Modify: `graywolf-modem/src/ipc/proto.rs`

- [ ] **Step 1: Remove the dispatch arm**

In `graywolf-modem/src/modem/mod.rs`, delete (around line 322):

```rust
            Some(Payload::PlayTestTone(req)) => {
                self.handle_play_test_tone(req);
            }
```

- [ ] **Step 2: Remove `TestToneResult` from the inbound-ignore list**

In the match arm that ignores Rust→Go message types (around line 353-360), remove the `| Some(Payload::TestToneResult(_))` term. Leave the rest of the `|`-chain valid.

- [ ] **Step 3: Remove the handler**

Delete the entire `fn handle_play_test_tone(...)` method (around line 774-793).

- [ ] **Step 4: Remove the blocking implementation**

Delete the entire `fn play_test_tone_blocking(...)` free function (around line 2213 through its closing brace ~2390). Read the surrounding lines first to find the exact end.

- [ ] **Step 5: Remove the import**

In the `use ...proto::{... TestToneResult ...}` import (around line 29), remove `TestToneResult`.

- [ ] **Step 6: Remove the proto.rs helper**

In `graywolf-modem/src/ipc/proto.rs`, delete:

```rust
    pub fn test_tone_result(r: TestToneResult) -> Self {
        Self { payload: Some(ipc_message::Payload::TestToneResult(r)) }
    }
```

and any now-unused `TestToneResult` import in that file.

- [ ] **Step 7: Verify (compile-check; may require CI for full target)**

Run: `cargo build -p graywolf-modem 2>&1 | tail -30`
Expected: compiles, OR fails only on known `cfg(linux)`-only modules unrelated to test tone. There must be **no** errors mentioning `test_tone`, `TestTone`, or `play_test_tone`. If the dev host can't complete the build, note it and rely on CI; the grep in Step 8 is the local gate.

- [ ] **Step 8: Verify no references remain**

Run: `grep -rn "test_tone\|TestTone\|play_test_tone\|PlayTestTone" graywolf-modem/src/`
Expected: no output.

- [ ] **Step 9: Commit**

```bash
git add graywolf-modem/src/
git commit -m "Remove test tone handler from Rust modem"
```

## Task A5: Delete the Test Tone proto messages and regenerate

**Files:**
- Modify: `proto/graywolf.proto`
- Regenerate: `pkg/ipcproto/graywolf.pb.go`, `graywolf-modem` prost bindings, `pkg/webapi/docs/gen/*`, `web/src/api/generated/api.d.ts`

- [ ] **Step 1: Remove the oneof entries**

In `proto/graywolf.proto`, delete these two lines from the `oneof payload` block:

```proto
    TestToneResult test_tone_result = 6;
```
```proto
    PlayTestTone play_test_tone = 18;
```

Do **not** renumber other fields and do **not** reuse 6 or 18 later (Part B uses fresh numbers 9 and 22).

- [ ] **Step 2: Remove the message definitions**

Delete the `message PlayTestTone { ... }` block (around line 243-249) and the `message TestToneResult { ... }` block (around line 252-256), including their comments.

- [ ] **Step 3: Regenerate Go bindings**

Run: `make proto`
Expected: succeeds.

Run: `grep -c "PlayTestTone\|TestToneResult" pkg/ipcproto/graywolf.pb.go`
Expected: `0`.

- [ ] **Step 4: Regenerate Rust bindings + build**

Run: `cargo build -p graywolf-modem 2>&1 | tail -20`
Expected: build proceeds past codegen with no `TestTone`/`PlayTestTone` errors. Defer to CI if the host can't finish a `cfg(linux)` build.

- [ ] **Step 5: Regenerate docs and API types**

Run: `make docs && cd web && npm run api:generate && cd ..`
Expected: succeeds.

Run: `grep -rn "test-tone\|TestTone" web/src/api/generated/api.d.ts pkg/webapi/docs/gen/`
Expected: no output.

- [ ] **Step 6: Full Go build + test**

Run: `go build ./... && go test ./pkg/webapi/... ./pkg/modembridge/... ./pkg/ipcproto/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add proto/graywolf.proto pkg/ipcproto/graywolf.pb.go pkg/webapi/docs/gen web/src/api/generated/api.d.ts
git commit -m "Remove test tone IPC messages and regenerate bindings"
```

---

# PART B — Add Channel TX Test Signals

## Task B1: Pure Rust `txtest` module (TDD)

**Files:**
- Create: `graywolf-modem/src/txtest.rs`
- Modify: `graywolf-modem/src/lib.rs` (add `pub(crate) mod txtest;`)

- [ ] **Step 1: Register the module**

In `graywolf-modem/src/lib.rs`, add alongside the other top-level module declarations:

```rust
pub(crate) mod txtest;
```

- [ ] **Step 2: Write the failing tests**

Create `graywolf-modem/src/txtest.rs` with the test module only first:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    fn on(units: u32) -> Segment { Segment { on: true, units } }
    fn off(units: u32) -> Segment { Segment { on: false, units } }

    #[test]
    fn encode_single_dit() {
        assert_eq!(encode("E"), vec![on(1)]);
    }

    #[test]
    fn encode_inter_character_gap() {
        assert_eq!(encode("EE"), vec![on(1), off(3), on(1)]);
    }

    #[test]
    fn encode_dah_and_intra_gaps() {
        // "K" = -.-  => dah, gap, dit, gap, dah
        assert_eq!(encode("K"), vec![on(3), off(1), on(1), off(1), on(3)]);
    }

    #[test]
    fn encode_word_gap() {
        // "A B": A=.- ; word gap 7 ; B=-...
        assert_eq!(
            encode("A B"),
            vec![
                on(1), off(1), on(3),
                off(7),
                on(3), off(1), on(1), off(1), on(1), off(1), on(1),
            ]
        );
    }

    #[test]
    fn encode_skips_unknown_chars() {
        assert_eq!(encode("E@E"), encode("EE"));
    }

    #[test]
    fn cw_samples_dit_length_matches_wpm() {
        // 20 WPM at 48 kHz: dit = 1.2/20 s = 60 ms = 2880 samples.
        let s = cw_samples("E", 48_000, 20, 700.0);
        assert_eq!(s.len(), 2880);
        assert!(s.iter().any(|&v| v != 0), "tone must be non-silent");
    }

    #[test]
    fn cw_samples_empty_callsign_is_empty() {
        assert!(cw_samples("", 48_000, 20, 700.0).is_empty());
    }

    #[test]
    fn tone_samples_length_and_nonsilent() {
        // 3000 ms at 48 kHz = 144000 samples.
        let s = tone_samples(48_000, 1200.0, 3000);
        assert_eq!(s.len(), 144_000);
        assert!(s.iter().any(|&v| v != 0));
    }

    #[test]
    fn alternating_samples_length_and_nonsilent() {
        let s = alternating_samples(48_000, 1200.0, 2400.0, 3000, 200);
        assert_eq!(s.len(), 144_000);
        assert!(s.iter().any(|&v| v != 0));
    }
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cargo test -p graywolf-modem txtest:: 2>&1 | tail -20`
Expected: FAIL — `encode`, `cw_samples`, `tone_samples`, `alternating_samples`, `Segment` not found.

- [ ] **Step 4: Implement the module**

Prepend the implementation above the test module in `graywolf-modem/src/txtest.rs`:

```rust
//! Pure TX test-signal generation: CW (Morse) station ID and steady /
//! alternating test tones.
//!
//! No I/O and no audio device — this turns parameters into i16 PCM samples.
//! The modem submits the samples as a normal TxJob, so PTT keying and
//! play-out reuse the existing TX worker path.

const AMP: f32 = 0.6 * 32767.0;
const RAMP_MS: f32 = 5.0;

/// One keyed or unkeyed CW span, in Morse time units (1 unit = 1 dit).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Segment {
    pub on: bool,
    pub units: u32,
}

fn ramp_samples(sample_rate: u32) -> usize {
    ((sample_rate as f32) * RAMP_MS / 1000.0) as usize
}

/// Apply a raised-cosine rise/fall (in place) to suppress edge clicks.
fn apply_edges(buf: &mut [i16], ramp: usize) {
    let n = buf.len();
    let r = ramp.min(n / 2);
    if r == 0 {
        return;
    }
    for i in 0..r {
        let env = 0.5 * (1.0 - (std::f32::consts::PI * i as f32 / r as f32).cos());
        buf[i] = (buf[i] as f32 * env) as i16;
        buf[n - 1 - i] = (buf[n - 1 - i] as f32 * env) as i16;
    }
}

/// Dot/dash pattern for a character, or None when there is no standard Morse
/// representation (encode skips those).
fn pattern(c: char) -> Option<&'static str> {
    Some(match c.to_ascii_uppercase() {
        'A' => ".-", 'B' => "-...", 'C' => "-.-.", 'D' => "-..",
        'E' => ".", 'F' => "..-.", 'G' => "--.", 'H' => "....",
        'I' => "..", 'J' => ".---", 'K' => "-.-", 'L' => ".-..",
        'M' => "--", 'N' => "-.", 'O' => "---", 'P' => ".--.",
        'Q' => "--.-", 'R' => ".-.", 'S' => "...", 'T' => "-",
        'U' => "..-", 'V' => "...-", 'W' => ".--", 'X' => "-..-",
        'Y' => "-.--", 'Z' => "--..",
        '0' => "-----", '1' => ".----", '2' => "..---", '3' => "...--",
        '4' => "....-", '5' => ".....", '6' => "-....", '7' => "--...",
        '8' => "---..", '9' => "----.",
        '/' => "-..-.", '-' => "-....-", '.' => ".-.-.-", ',' => "--..--",
        '?' => "..--..",
        _ => return None,
    })
}

/// Encode text into keyed/unkeyed segments with standard Morse timing:
/// dit=1, dah=3, intra-character gap=1, inter-character gap=3, word gap=7
/// (in dit units). No leading/trailing gaps. Unknown characters are skipped.
pub fn encode(text: &str) -> Vec<Segment> {
    let mut out: Vec<Segment> = Vec::new();
    let mut prev_was_char = false;
    for raw in text.chars() {
        if raw == ' ' {
            if prev_was_char {
                out.push(Segment { on: false, units: 7 });
                prev_was_char = false;
            }
            continue;
        }
        let pat = match pattern(raw) {
            Some(p) => p,
            None => continue,
        };
        if prev_was_char {
            out.push(Segment { on: false, units: 3 });
        }
        for (i, el) in pat.chars().enumerate() {
            if i > 0 {
                out.push(Segment { on: false, units: 1 });
            }
            out.push(Segment { on: true, units: if el == '-' { 3 } else { 1 } });
        }
        prev_was_char = true;
    }
    out
}

/// CW: encode `callsign` and render the on/off-keyed sidetone at `wpm`
/// (PARIS timing) and `tone_hz`, with raised-cosine edges on each keyed span.
/// Returns empty if the callsign has no renderable characters.
pub fn cw_samples(callsign: &str, sample_rate: u32, wpm: u32, tone_hz: f32) -> Vec<i16> {
    let segments = encode(callsign);
    let wpm = wpm.max(1);
    let dit = ((sample_rate as f64) * 1.2 / wpm as f64).round() as usize;
    let ramp = ramp_samples(sample_rate);
    let w = 2.0 * std::f32::consts::PI * tone_hz / sample_rate as f32;
    let mut out: Vec<i16> = Vec::new();
    for seg in &segments {
        let n = dit * seg.units as usize;
        if !seg.on {
            out.extend(std::iter::repeat(0i16).take(n));
            continue;
        }
        let start = out.len();
        for i in 0..n {
            out.push(((w * i as f32).sin() * AMP) as i16);
        }
        let end = out.len();
        apply_edges(&mut out[start..end], ramp);
    }
    out
}

/// Steady sine of `freq_hz` for `duration_ms`, with edge ramps.
pub fn tone_samples(sample_rate: u32, freq_hz: f32, duration_ms: u32) -> Vec<i16> {
    let n = (sample_rate as u64 * duration_ms as u64 / 1000) as usize;
    let w = 2.0 * std::f32::consts::PI * freq_hz / sample_rate as f32;
    let mut out: Vec<i16> = (0..n).map(|i| ((w * i as f32).sin() * AMP) as i16).collect();
    apply_edges(&mut out, ramp_samples(sample_rate));
    out
}

/// Alternating tone: switch between `freq_a` and `freq_b` every `period_ms`
/// for `duration_ms`. Phase-continuous (a running phase accumulator) so the
/// frequency switches don't click; raised-cosine ramps on the outer edges.
pub fn alternating_samples(
    sample_rate: u32,
    freq_a: f32,
    freq_b: f32,
    duration_ms: u32,
    period_ms: u32,
) -> Vec<i16> {
    let n = (sample_rate as u64 * duration_ms as u64 / 1000) as usize;
    let per = ((sample_rate as u64 * period_ms.max(1) as u64 / 1000) as usize).max(1);
    let mut out: Vec<i16> = Vec::with_capacity(n);
    let mut phase = 0.0f32;
    let two_pi = 2.0 * std::f32::consts::PI;
    for i in 0..n {
        let f = if (i / per) % 2 == 0 { freq_a } else { freq_b };
        phase += two_pi * f / sample_rate as f32;
        out.push((phase.sin() * AMP) as i16);
    }
    apply_edges(&mut out, ramp_samples(sample_rate));
    out
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cargo test -p graywolf-modem txtest:: 2>&1 | tail -20`
Expected: all 9 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add graywolf-modem/src/txtest.rs graywolf-modem/src/lib.rs
git commit -m "Add pure TX test-signal module (CW, steady tone, alternating tone)"
```

## Task B2: Add the TX test-signal IPC messages and regenerate bindings

**Files:**
- Modify: `proto/graywolf.proto`
- Modify: `graywolf-modem/src/ipc/proto.rs`
- Regenerate: `pkg/ipcproto/graywolf.pb.go`, prost bindings

- [ ] **Step 1: Add oneof entries**

In `proto/graywolf.proto`, inside `oneof payload`, add to the Rust→Go group (field **9**, the next free RX id):

```proto
    TestSignalResult test_signal_result = 9;
```

and to the Go→Rust group (field **22**, the next free TX id):

```proto
    TransmitTestSignal transmit_test_signal = 22;
```

- [ ] **Step 2: Add message definitions**

Add near the other audio/PTT messages:

```proto
// Go -> Rust: transmit a TX test signal on a channel. kind selects the
// generator: 0 = station callsign in CW, 1 = steady tone, 2 = alternating
// tone. Go fills the relevant parameters; the modem is a pure renderer.
message TransmitTestSignal {
  uint32 request_id = 1;     // echoed back in TestSignalResult
  uint32 channel = 2;        // selects output device + PTT driver
  uint32 kind = 3;           // 0=CW callsign, 1=steady tone, 2=alternating
  string callsign = 4;       // kind 0; already resolved + uppercased by Go
  uint32 cw_wpm = 5;         // kind 0
  uint32 freq_a_hz = 6;      // CW sidetone (0) / tone (1) / tone A (2)
  uint32 freq_b_hz = 7;      // tone B (kind 2)
  uint32 duration_ms = 8;    // kinds 1, 2
  uint32 alt_period_ms = 9;  // kind 2: ms per tone before switching
}

// Result of a TX test-signal submission to the TX worker.
message TestSignalResult {
  uint32 request_id = 1;
  bool success = 2;
  string error = 3;          // empty on success
}
```

- [ ] **Step 3: Regenerate Go bindings**

Run: `make proto`
Expected: succeeds.

Run: `grep -c "TransmitTestSignal\|TestSignalResult" pkg/ipcproto/graywolf.pb.go`
Expected: non-zero.

- [ ] **Step 4: Add the Rust proto.rs helper**

In `graywolf-modem/src/ipc/proto.rs`, add a constructor mirroring the others:

```rust
    pub fn test_signal_result(r: TestSignalResult) -> Self {
        Self { payload: Some(ipc_message::Payload::TestSignalResult(r)) }
    }
```

Ensure `TestSignalResult` is imported in that file's `use` block (mirror the other result types).

- [ ] **Step 5: Build Rust to regenerate prost bindings**

Run: `cargo build -p graywolf-modem 2>&1 | tail -20`
Expected: codegen succeeds; `TestSignalResult` / `TransmitTestSignal` resolve. (`test_signal_result` is unused until Task B3 — a dead-code warning is acceptable here.)

- [ ] **Step 6: Verify Go compiles**

Run: `go build ./pkg/ipcproto/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add proto/graywolf.proto pkg/ipcproto/graywolf.pb.go graywolf-modem/src/ipc/proto.rs
git commit -m "Add TransmitTestSignal and TestSignalResult IPC messages"
```

## Task B3: Rust modem test-signal handler

**Files:**
- Modify: `graywolf-modem/src/modem/mod.rs`

- [ ] **Step 1: Import the new result type**

In the `use crate::ipc::proto::{...}` block (around line 29), add `TestSignalResult` to the imported names.

- [ ] **Step 2: Add the dispatch arm**

In the inbound-payload `match` (near the `Some(Payload::TransmitFrame(tf))` arm, around line 338), add:

```rust
            Some(Payload::TransmitTestSignal(req)) => {
                self.handle_transmit_test_signal(req);
            }
```

- [ ] **Step 3: Add `TestSignalResult` to the inbound-ignore list**

In the ignore arm (the `|`-chain of Rust→Go types, around line 353), add `| Some(Payload::TestSignalResult(_))`.

- [ ] **Step 4: Implement the handler**

Add this method to the same `impl` block that holds `handle_transmit_frame` (place it right after `handle_transmit_frame`). It mirrors that method's channel/audio-config resolution, picks the generator by `kind`, applies gain, submits a `TxJob`, and replies with `TestSignalResult`:

```rust
    fn handle_transmit_test_signal(&mut self, req: crate::ipc::proto::TransmitTestSignal) {
        let reply = |handle: &IpcHandle, success: bool, error: String| {
            let _ = handle.send(&IpcMessage::test_signal_result(crate::ipc::proto::TestSignalResult {
                request_id: req.request_id,
                success,
                error,
            }));
        };

        // Lazy rigctld retry, same as handle_transmit_frame.
        if self.ptt_rigctld_pending.contains(&req.channel) {
            if let Some(cfg) = self.ptt_cfgs.get(&req.channel).cloned() {
                self.apply_ptt_config(cfg);
            }
        }

        let ccfg = match self.channel_configs.get(&req.channel) {
            Some(c) => c.clone(),
            None => {
                reply(&self.handle, false, format!("unknown channel {}", req.channel));
                return;
            }
        };
        let acfg = match self.audio_configs.get(&ccfg.output_device_id) {
            Some(a) => a.clone(),
            None => {
                reply(&self.handle, false, format!("no audio config for output device {}", ccfg.output_device_id));
                return;
            }
        };

        let sr = acfg.sample_rate;
        let mut samples = match req.kind {
            0 => {
                let s = crate::txtest::cw_samples(&req.callsign, sr, req.cw_wpm.max(1), req.freq_a_hz as f32);
                if s.is_empty() {
                    reply(&self.handle, false, "callsign produced no CW symbols".to_string());
                    return;
                }
                s
            }
            1 => crate::txtest::tone_samples(sr, req.freq_a_hz as f32, req.duration_ms),
            2 => crate::txtest::alternating_samples(
                sr,
                req.freq_a_hz as f32,
                req.freq_b_hz as f32,
                req.duration_ms,
                req.alt_period_ms,
            ),
            other => {
                reply(&self.handle, false, format!("unknown test signal kind {}", other));
                return;
            }
        };

        // Apply output device gain, matching handle_transmit_frame.
        if let Some(gain_atom) = self.gain_atoms.get(&ccfg.output_device_id) {
            let gain_db = f32::from_bits(gain_atom.load(std::sync::atomic::Ordering::Relaxed));
            if gain_db.abs() > f32::EPSILON {
                let gain_linear = 10f32.powf(gain_db / 20.0);
                for s in samples.iter_mut() {
                    let amplified = (*s as f32) * gain_linear;
                    *s = amplified.clamp(-32767.0, 32767.0) as i16;
                }
            }
        }

        let job = tx_worker::TxJob {
            channel: req.channel,
            samples,
            sample_rate: sr,
            output_device_id: ccfg.output_device_id,
            sink_config: audio::soundcard::SoundcardOutputConfig {
                device_name: acfg.device_name.clone(),
                sample_rate: sr,
                channels: acfg.channels,
                audio_channel: ccfg.output_channel,
            },
        };

        match self.tx_worker.transmit(job) {
            Ok(()) => {
                *self.tx_frames.entry(req.channel).or_default() += 1;
                reply(&self.handle, true, String::new());
            }
            Err(e) => reply(&self.handle, false, e),
        }
    }
```

> Note: `channel_configs`/`audio_configs` are cloned so the later `&mut self`
> calls don't conflict with the immutable borrow. If `ChannelConfig`/`AudioConfig`
> are not `Clone`, read their definitions and copy out only the fields used
> (`output_device_id`, `output_channel`, `sample_rate`, `device_name`,
> `channels`) into locals before the mutable calls. The `reply` closure takes
> `&self.handle` explicitly to avoid capturing `self`.

- [ ] **Step 5: Verify (compile-check; CI for full target)**

Run: `cargo build -p graywolf-modem 2>&1 | tail -30`
Expected: compiles, or fails only on unrelated `cfg(linux)`-only code. No errors mentioning `handle_transmit_test_signal`, `txtest`, `TestSignalResult`, or `TransmitTestSignal`. If the host can't finish, note it; CI is the gate.

Run: `cargo test -p graywolf-modem txtest:: 2>&1 | tail -5`
Expected: txtest tests still PASS.

- [ ] **Step 6: Commit**

```bash
git add graywolf-modem/src/modem/mod.rs
git commit -m "Handle TransmitTestSignal in the modem: render CW/tone and submit a TxJob"
```

## Task B4: Go bridge `TransmitTestSignal`

**Files:**
- Modify: `pkg/modembridge/requests.go`
- Modify: `pkg/modembridge/bridge.go`
- Modify: `pkg/modembridge/session.go`
- Modify: `pkg/modembridge/bridge_stop_test.go`

- [ ] **Step 1: Add the dispatcher field + init**

In `pkg/modembridge/bridge.go`, add the field next to the other dispatchers:

```go
	testSignalDispatcher *dispatcher[*pb.TestSignalResult]
```

and in `New`, next to the other dispatcher inits:

```go
		testSignalDispatcher: newDispatcher[*pb.TestSignalResult](),
```

- [ ] **Step 2: Add the params type and `TransmitTestSignal` method**

In `pkg/modembridge/requests.go`, add (mirrors the former `PlayTestTone` pattern):

```go
// TestSignalParams describes one TX test signal. Kind: 0=CW callsign,
// 1=steady tone, 2=alternating tone. Unused fields for a given kind are
// ignored by the modem.
type TestSignalParams struct {
	Channel     uint32
	Kind        uint32
	Callsign    string
	CwWpm       uint32
	FreqAHz     uint32
	FreqBHz     uint32
	DurationMs  uint32
	AltPeriodMs uint32
}

// TransmitTestSignal asks the Rust modem to transmit a TX test signal on a
// channel and waits for the submission result. The modem keys PTT, plays the
// audio, and unkeys via the TX worker.
func (b *Bridge) TransmitTestSignal(ctx context.Context, p TestSignalParams) error {
	if b.State() != StateRunning {
		return errors.New("modembridge: not in RUNNING state")
	}

	reqID, ch := b.testSignalDispatcher.Register()
	defer b.testSignalDispatcher.Cancel(reqID)

	msg := &pb.IpcMessage{Payload: &pb.IpcMessage_TransmitTestSignal{
		TransmitTestSignal: &pb.TransmitTestSignal{
			RequestId:   reqID,
			Channel:     p.Channel,
			Kind:        p.Kind,
			Callsign:    p.Callsign,
			CwWpm:       p.CwWpm,
			FreqAHz:     p.FreqAHz,
			FreqBHz:     p.FreqBHz,
			DurationMs:  p.DurationMs,
			AltPeriodMs: p.AltPeriodMs,
		},
	}}
	if err := b.sendIPC(msg); err != nil {
		return fmt.Errorf("send TransmitTestSignal: %w", err)
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case resp := <-ch:
		if resp == nil {
			return errBridgeStopped
		}
		if !resp.Success {
			return fmt.Errorf("test signal failed: %s", resp.Error)
		}
		return nil
	case <-timer.C:
		return errors.New("modembridge: test signal timeout")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Bridge) dispatchTestSignalResponse(r *pb.TestSignalResult) {
	b.testSignalDispatcher.Deliver(r.RequestId, r)
}
```

> Verify the generated Go field names against `pkg/ipcproto/graywolf.pb.go`
> after `make proto` — protoc-gen-go names `cw_wpm` → `CwWpm`, `freq_a_hz` →
> `FreqAHz`, `alt_period_ms` → `AltPeriodMs`, `transmit_test_signal` →
> `IpcMessage_TransmitTestSignal` / field `TransmitTestSignal`. Adjust if the
> generated names differ.

- [ ] **Step 3: Add the session dispatch case**

In `pkg/modembridge/session.go`, in the inbound `switch`, add:

```go
	case *pb.IpcMessage_TestSignalResult:
		b.dispatchTestSignalResponse(p.TestSignalResult)
```

- [ ] **Step 4: Add the stop-test coverage**

In `pkg/modembridge/bridge_stop_test.go`, mirror the existing dispatcher patterns. Read the file first, then:

- Add a test-case entry that calls
  `b.TransmitTestSignal(context.Background(), modembridge.TestSignalParams{Channel: 0, Kind: 1, FreqAHz: 1200, DurationMs: 100})`
  (use the package-internal form — no `modembridge.` prefix since the test is in-package; check the file's package clause) and asserts it unblocks with `errBridgeStopped` (mirror the former `PlayTestTone` entry shape).
- Add `"testsignal": adaptPbTestSignalResult(testSignalCh),` to the channel map and declare `testSignalCh` like the others.
- Add the adapter:

```go
func adaptPbTestSignalResult(c <-chan *pb.TestSignalResult) <-chan any {
	out := make(chan any)
	go func() {
		defer close(out)
		for v := range c {
			out <- v
		}
	}()
	return out
}
```

(Match the exact structure of the sibling adapters already in the file.)

- [ ] **Step 5: Verify build + tests**

Run: `go build ./pkg/modembridge/... && go test ./pkg/modembridge/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/modembridge/
git commit -m "Add TransmitTestSignal request path to modem bridge"
```

## Task B5: Go webapi `POST /api/channels/{id}/test-tx`

**Files:**
- Modify: `pkg/webapi/channels.go`
- Modify: `pkg/webapi/dto/` (channels DTO file — find via grep)
- Modify: `pkg/webapi/docs/op_ids.go`
- Test: `pkg/webapi/channels_test_tx_test.go` (create)

- [ ] **Step 1: Locate the channels DTO file**

Run: `grep -rln "ChannelResponse\|BeaconSendResponse" pkg/webapi/dto/`
Use the file that holds channel-related DTOs (likely `pkg/webapi/dto/channel.go`). Add there:

```go
// TestSignalRequest is the body for POST /api/channels/{id}/test-tx.
// Signal is one of: "cw", "tone1200", "tone2400", "alt".
type TestSignalRequest struct {
	Signal string `json:"signal" example:"cw"`
}

// TestSignalResponse is the body returned by POST /api/channels/{id}/test-tx.
type TestSignalResponse struct {
	Status string `json:"status" example:"sent"`
}
```

- [ ] **Step 2: Add the op-id constant**

In `pkg/webapi/docs/op_ids.go`, near `OpManualPtt`/`OpSendBeacon`, add:

```go
	OpSendTestSignal = "sendTestSignal"
```

- [ ] **Step 3: Register the route**

In `pkg/webapi/channels.go`, in `registerChannels`, add after the `ptt` route:

```go
	mux.HandleFunc("POST /api/channels/{id}/test-tx", s.sendTestSignal)
```

- [ ] **Step 4: Write the handler**

In `pkg/webapi/channels.go`, add (mirrors `manualPtt` + the beacon refusal mapping). Ensure imports include `errors` and `github.com/chrissnell/graywolf/pkg/callsign` and `github.com/chrissnell/graywolf/pkg/modembridge`:

```go
// CW / tone recipe constants — the single source of the hardcoded test-signal
// parameters. The four UI options map to these.
const (
	cwTestWpm       = 20
	cwTestToneHz    = 700
	toneTestDurMs   = 3000
	altTestPeriodMs = 200
	toneTestLowHz   = 1200
	toneTestHighHz  = 2400
)

// sendTestSignal transmits a TX test signal (CW callsign or a tone) on a
// channel. The "cw" signal refuses to key the radio when the station callsign
// is empty or N0CALL; tone signals need no callsign. All signals require a
// TX-capable channel.
//
// @Summary  Send a TX test signal (CW callsign or tone) on a channel
// @Tags     Channels
// @ID       sendTestSignal
// @Accept   json
// @Produce  json
// @Param    id path int true "Channel ID"
// @Param    body body dto.TestSignalRequest true "Signal to send"
// @Success  200 {object} dto.TestSignalResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  409 {object} webtypes.ErrorResponse
// @Failure  422 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Router   /channels/{id}/test-tx [post]
func (s *Server) sendTestSignal(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	if s.bridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: "bridge not available"})
		return
	}

	var req dto.TestSignalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}

	params := modembridge.TestSignalParams{Channel: id}
	switch req.Signal {
	case "cw":
		call, err := s.store.ResolveStationCallsign(r.Context())
		if err != nil {
			switch {
			case errors.Is(err, callsign.ErrCallsignEmpty):
				writeJSON(w, http.StatusUnprocessableEntity, webtypes.ErrorResponse{Error: "set your station callsign before sending CW ID"})
			case errors.Is(err, callsign.ErrCallsignN0Call):
				writeJSON(w, http.StatusUnprocessableEntity, webtypes.ErrorResponse{Error: "station callsign is still N0CALL; set a real callsign before sending CW ID"})
			default:
				s.internalError(w, r, "resolve station callsign", err)
			}
			return
		}
		params.Kind = 0
		params.Callsign = call
		params.CwWpm = cwTestWpm
		params.FreqAHz = cwTestToneHz
	case "tone1200":
		params.Kind = 1
		params.FreqAHz = toneTestLowHz
		params.DurationMs = toneTestDurMs
	case "tone2400":
		params.Kind = 1
		params.FreqAHz = toneTestHighHz
		params.DurationMs = toneTestDurMs
	case "alt":
		params.Kind = 2
		params.FreqAHz = toneTestLowHz
		params.FreqBHz = toneTestHighHz
		params.DurationMs = toneTestDurMs
		params.AltPeriodMs = altTestPeriodMs
	default:
		badRequest(w, "unknown signal: "+req.Signal)
		return
	}

	if err := s.requireTxCapableChannel(r.Context(), "channel", id); err != nil {
		writeJSON(w, http.StatusConflict, webtypes.ErrorResponse{Error: err.Error()})
		return
	}

	if err := s.bridge.TransmitTestSignal(r.Context(), params); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, dto.TestSignalResponse{Status: "sent"})
}
```

> Verify the exact import aliases for `webtypes`, `dto`, and `modembridge` at
> the top of `channels.go` and match them (sibling handlers already use
> `webtypes` and `dto`; confirm whether `modembridge` is already imported and
> under what name). Confirm `s.bridge` has type `*modembridge.Bridge` (the
> `manualPtt` handler calls `s.bridge.ManualPttWithWatchdog`, so the field
> exists).

- [ ] **Step 5: Write the test**

Create `pkg/webapi/channels_test_tx_test.go`. Read `pkg/webapi/beacons_send_test.go` first to copy the exact server-construction / store / bridge-stub harness this package uses. Then assert:

- `cw` with empty station callsign → `422`.
- `cw` with N0CALL station callsign → `422`.
- unknown signal (e.g. `{"signal":"bogus"}`) → `400`.
- non-TX channel (any valid signal) → `409`.
- `tone1200` on a TX-capable channel with the bridge stub returning nil → `200` and body `{"status":"sent"}`.
- (Optional but cheap) `cw` with a valid station callsign + TX channel → `200`, and assert the stub received `Kind==0`, `Callsign` non-empty, `FreqAHz==700`.

Use the package's existing mechanism for stubbing the bridge (interface stub / fake). If `s.bridge` is a concrete `*modembridge.Bridge`, replicate exactly how `manualPtt`/beacon tests inject a fake rather than inventing a new mechanism. If the harness only supports a real bridge in `StateRunning`, follow the beacon-send test's approach for driving `TransmitTestSignal` to a deterministic result.

- [ ] **Step 6: Run the test**

Run: `go test ./pkg/webapi/ -run TestSignal -v`
Expected: PASS (all cases).

- [ ] **Step 7: Full webapi build + test**

Run: `go build ./pkg/webapi/... && go test ./pkg/webapi/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add pkg/webapi/channels.go pkg/webapi/dto/ pkg/webapi/docs/op_ids.go pkg/webapi/channels_test_tx_test.go
git commit -m "Add channel TX test-signal REST endpoint with callsign and TX-capability guards"
```

## Task B6: Regenerate docs and API types

**Files:**
- Regenerate: `pkg/webapi/docs/gen/*`, `web/src/api/generated/api.d.ts`

- [ ] **Step 1: Regenerate**

Run: `make docs && cd web && npm run api:generate && cd ..`
Expected: succeeds.

- [ ] **Step 2: Verify the new endpoint is present**

Run: `grep -rn "test-tx\|sendTestSignal" pkg/webapi/docs/gen/swagger.json web/src/api/generated/api.d.ts`
Expected: matches in both.

- [ ] **Step 3: Docs drift check**

Run: `make docs-check`
Expected: no drift.

- [ ] **Step 4: Commit**

```bash
git add pkg/webapi/docs/gen web/src/api/generated/api.d.ts
git commit -m "Regenerate API docs and types for TX test-signal endpoint"
```

## Task B7: Frontend "Test TX" dropdown menu

**Files:**
- Modify: `web/src/routes/channels/ChannelRow.svelte`

The click handlers run inline in `ChannelRow` (with a single local in-flight
flag), matching how `AudioDevices.svelte` handled Test Tone. The menu is gated
by the same TX-capability condition the PTT indicator uses:
`!isKissOnly && channel.output_device_id && channel.output_device_id !== 0`.

- [ ] **Step 1: Add imports, state, and the send function**

In the `<script>` block of `ChannelRow.svelte`, add `DropdownMenu` to the chonky-ui import (it already imports `{ Badge, Button }`):

```javascript
  import { Badge, Button, DropdownMenu } from '@chrissnell/chonky-ui';
```

Add to the imports:

```javascript
  import { api } from '../../lib/api.js';
  import { toasts } from '../../lib/stores.js';
```

> Verify the relative depth: `ChannelRow.svelte` is in `web/src/routes/channels/`,
> so `lib` is two levels up. Confirm against the existing `../../lib/channelBacking.js`
> import at the top of the file.

After the `$props()` line, add:

```javascript
  let sendingSignal = $state(false);

  const SIGNAL_LABELS = {
    cw: 'Send callsign in CW',
    tone1200: 'Send 1200 Hz tone',
    tone2400: 'Send 2400 Hz tone',
    alt: 'Send 1200/2400 Hz alternating tone',
  };

  async function sendTestSignal(signal) {
    sendingSignal = true;
    try {
      await api.post(`/channels/${channel.id}/test-tx`, { signal });
      toasts.success(`Sent "${SIGNAL_LABELS[signal]}" on "${channel.name}"`);
    } catch (err) {
      toasts.error(`Test TX failed: ${err.message}`);
    } finally {
      sendingSignal = false;
    }
  }
```

> Confirm the channel display field (`channel.name`) and the `api.post(url, body)`
> signature against `web/src/lib/api.js` and an existing caller (e.g.
> `AudioDevices.svelte` calls `api.post('/audio-devices/${id}/test-tone')` with
> no body; check how a body is passed elsewhere — `grep -n "api.post" web/src`).
> Adjust the call to the project's actual `api.post` body convention.

- [ ] **Step 2: Add the menu to the action row**

In the action row that currently holds Edit/Delete (around line 139), add the dropdown before Edit, gated on TX capability:

```svelte
  {#if !isKissOnly && channel.output_device_id && channel.output_device_id !== 0}
    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        <Button variant="ghost" disabled={sendingSignal}>
          {sendingSignal ? 'Sending…' : 'Test TX ▾'}
        </Button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Content>
        <DropdownMenu.Item onSelect={() => sendTestSignal('cw')}>Send callsign in CW</DropdownMenu.Item>
        <DropdownMenu.Item onSelect={() => sendTestSignal('tone1200')}>Send 1200 Hz tone</DropdownMenu.Item>
        <DropdownMenu.Item onSelect={() => sendTestSignal('tone2400')}>Send 2400 Hz tone</DropdownMenu.Item>
        <DropdownMenu.Item onSelect={() => sendTestSignal('alt')}>Send 1200/2400 Hz alternating tone</DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu.Root>
  {/if}
  <Button variant="ghost" onclick={() => onEdit?.(channel)}>Edit</Button>
  <Button variant="danger" onclick={() => onDelete?.(channel)}>Delete</Button>
```

> The `DropdownMenu` compound API (`Root` / `Trigger` / `Content` / `Item`
> with `onSelect`+`disabled`) is confirmed from chonky-ui's type definitions.
> If `Trigger` does not accept a nested `<Button>` cleanly (some trigger
> components render their own button), check
> `node_modules/@chrissnell/chonky-ui/dist/components/DropdownMenu/DropdownMenuTrigger.svelte`
> and adapt (e.g. apply the trigger's `class` to style it as a button).

- [ ] **Step 3: Build the frontend**

Run: `cd web && npm run build`
Expected: build succeeds.

- [ ] **Step 4: Manual verification (record result)**

Run the app and confirm: the "Test TX" menu appears only on modem-backed TX
channels (not on KISS-only or RX-only rows); the four items are present;
choosing a tone shows a success toast; choosing "Send callsign in CW" with no
station callsign set shows a clear error toast. Note the observed result.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/channels/ChannelRow.svelte
git commit -m "Add Test TX dropdown menu to TX-capable channel rows"
```

## Task B8: Update the wiki

**Files:**
- Modify: `docs/wiki/code-map.md`
- Modify: `docs/wiki/invariants.md`

- [ ] **Step 1: Update code-map**

In `docs/wiki/code-map.md`: remove any Test Tone / `playTestTone` / `play_test_tone` references; add the new `POST /api/channels/{id}/test-tx` endpoint (handler `sendTestSignal` in `pkg/webapi/channels.go`), the `graywolf-modem/src/txtest.rs` module, and the `TransmitTestSignal`/`TestSignalResult` IPC messages. Navigation-level pointers only.

- [ ] **Step 2: Update invariants**

In `docs/wiki/invariants.md`, add:

> **The CW test signal never keys the radio with an empty or N0CALL callsign.**
> `POST /api/channels/{id}/test-tx` with `signal=cw` resolves the station
> callsign via `Store.ResolveStationCallsign` and returns 422 before any IPC if
> it is empty or N0CALL. The tone signals (`tone1200`, `tone2400`, `alt`) do not
> use the callsign.

- [ ] **Step 3: Verify references**

Run: `grep -rn "test tone\|Test Tone\|test-tone\|playTestTone" docs/wiki/`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add docs/wiki/code-map.md docs/wiki/invariants.md
git commit -m "Update wiki for channel TX test signals; drop test tone references"
```

---

## Final verification (whole feature)

- [ ] **Go:** `go build ./... && go test ./pkg/webapi/... ./pkg/modembridge/... ./pkg/ipcproto/...` → PASS
- [ ] **Rust (host-buildable parts):** `cargo test -p graywolf-modem txtest::` → PASS; `cargo build -p graywolf-modem` → no test-signal/test-tone errors (full `cfg(linux)` build via CI)
- [ ] **Frontend:** `cd web && npm run build` → PASS
- [ ] **Docs:** `make docs-check` → no drift
- [ ] **No stragglers:** `grep -rn "test_tone\|TestTone\|test-tone\|playTestTone\|PlayTestTone" pkg/ graywolf-modem/src/ web/src/ proto/ docs/wiki/` → no output
- [ ] **On-device (CI build + radio):** each menu item keys PTT and transmits the expected signal (callsign decodable in Morse; steady 1200/2400 tones; audible warble for alternating), all verifiable on a second radio; CW with an unset callsign yields a clear refusal.

---

## Self-review notes (author)

- **Spec coverage:** Part A removes Test Tone across all six layers. Part B
  covers the `TransmitTestSignal`/`TestSignalResult` IPC, the `txtest` Rust
  module (CW + tone + alternating) and handler dispatching on `kind`, the Go
  bridge `TransmitTestSignal` + `TestSignalParams`, the webapi endpoint with the
  four hardcoded recipes and all guards (CW callsign 422, unknown signal 400,
  non-TX 409, success 200), the chonky-ui `DropdownMenu` frontend gated by TX
  capability, and wiki updates — matching the revised spec.
- **Type consistency:** Go `TransmitTestSignal`/`TestSignalParams`/
  `pb.TransmitTestSignal`/`pb.TestSignalResult`/`OpSendTestSignal`/
  `dto.TestSignalRequest`/`dto.TestSignalResponse`; Rust `txtest::encode`/
  `cw_samples`/`tone_samples`/`alternating_samples`/`Segment`/
  `handle_transmit_test_signal`/`IpcMessage::test_signal_result`; proto fields 9
  (RX) and 22 (TX); `kind` values 0/1/2 are used identically in proto comments,
  the Rust match, and the Go recipe switch; frontend signal ids
  `cw`/`tone1200`/`tone2400`/`alt` match the Go handler switch exactly.
- **No placeholders:** every code step shows complete code; verification steps
  give exact commands and expected output. Cross-file uncertainties (DTO file
  location, generated Go field names, `api.post` body convention, `Clone`-ability
  of config structs, `DropdownMenu.Trigger` nesting, import depth/aliases) are
  flagged with grep/read-first instructions rather than guessed.
