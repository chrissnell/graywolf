# Channel CW ID Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the broken audio Test Tone feature and add a per-channel "Send CW ID" button that transmits the station callsign in Morse through the channel's real TX path (key PTT → CW audio → unkey).

**Architecture:** A new `TransmitCwId` IPC message carries `{channel, callsign}` from Go to the Rust modem. A pure Rust `morse` module turns the callsign into i16 PCM samples (hardcoded 20 WPM / 700 Hz, PARIS timing); the modem submits them as a `TxJob` to the existing TX worker, which keys/unkeys PTT automatically — exactly like `handle_transmit_frame`. Go resolves the centralized station callsign and refuses to key the radio on an empty or N0CALL callsign.

**Tech Stack:** Rust (graywolf-modem, prost protobuf), Go (webapi + modembridge, protoc-gen-go), Svelte 5 (web UI, chonky-ui), Protocol Buffers IPC.

**Reference spec:** `docs/superpowers/specs/2026-05-26-channel-cw-id-design.md`

**Branch:** `feature/channel-cw-id` (already created; the design doc commit is its first commit).

---

## Build / regen reference (used by multiple tasks)

- **Regenerate Go protobuf bindings:** `make proto`
- **Regenerate OpenAPI spec:** `make docs`
- **Regenerate frontend API types:** `cd web && npm run api:generate`
- **Rust bindings** regenerate automatically on `cargo build` (graywolf-modem/build.rs runs prost on `proto/graywolf.proto`).
- **Rust tests:** `cargo test -p graywolf-modem`
- **Go tests (webapi + bridge):** `go test ./pkg/webapi/... ./pkg/modembridge/...`
- **Docs drift check:** `make docs-check`

> Note: `cfg(linux)` Rust cannot fully compile on the dev Mac (see project memory). `cargo test -p graywolf-modem` for the pure `morse` module and host-buildable code works; full-modem on-target verification happens in CI / on-device. Where a step's Rust build is expected to need CI, the step says so.

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

Find and remove the line declaring `testingTone` (search the file):

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

In `pkg/webapi/dto/audio_device.go`, delete the `TestToneResponse` type and its doc comment (around line 131):

```go
// TestToneResponse is the body returned by POST /api/audio-devices/{id}/test-tone
// ...
type TestToneResponse struct {
	Status string `json:"status" example:"ok"`
}
```

(Confirm exact field by reading lines 131-136 first.)

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
Expected: succeeds; `pkg/ipcproto/graywolf.pb.go` no longer contains `PlayTestTone` or `TestToneResult`.

Run: `grep -c "PlayTestTone\|TestToneResult" pkg/ipcproto/graywolf.pb.go`
Expected: `0`.

- [ ] **Step 4: Regenerate Rust bindings + build**

Run: `cargo build -p graywolf-modem 2>&1 | tail -20`
Expected: build proceeds past codegen with no `TestTone`/`PlayTestTone` errors (prost regenerates from the edited proto via build.rs). Defer to CI if the host can't finish a `cfg(linux)` build.

- [ ] **Step 5: Regenerate docs and API types**

Run: `make docs && cd web && npm run api:generate && cd ..`
Expected: succeeds. `web/src/api/generated/api.d.ts` no longer references `test-tone`.

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

# PART B — Add Channel CW ID

## Task B1: Pure Rust `morse` module (TDD)

**Files:**
- Create: `graywolf-modem/src/morse.rs`
- Modify: `graywolf-modem/src/lib.rs` (add `pub(crate) mod morse;`)

- [ ] **Step 1: Register the module**

In `graywolf-modem/src/lib.rs`, add alongside the other top-level module declarations:

```rust
pub(crate) mod morse;
```

- [ ] **Step 2: Write the failing tests**

Create `graywolf-modem/src/morse.rs` with the test module only first:

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
        // "EE" => dit, 3-unit inter-char gap, dit
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
                on(1), off(1), on(3),          // A
                off(7),                         // word gap
                on(3), off(1), on(1), off(1), on(1), off(1), on(1), // B
            ]
        );
    }

    #[test]
    fn encode_skips_unknown_chars() {
        // '@' has no Morse representation; result equals "EE".
        assert_eq!(encode("E@E"), encode("EE"));
    }

    #[test]
    fn synthesize_dit_length_matches_wpm() {
        // 20 WPM at 48 kHz: dit = 1.2/20 s = 60 ms = 2880 samples.
        let samples = synthesize(&encode("E"), 48_000, 20, 700.0);
        assert_eq!(samples.len(), 2880);
        assert!(samples.iter().any(|&s| s != 0), "tone must be non-silent");
    }

    #[test]
    fn synthesize_empty_is_empty() {
        assert!(synthesize(&[], 48_000, 20, 700.0).is_empty());
    }
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cargo test -p graywolf-modem morse:: 2>&1 | tail -20`
Expected: FAIL — `encode`, `synthesize`, `Segment` not found.

- [ ] **Step 4: Implement the module**

Prepend the implementation above the test module in `graywolf-modem/src/morse.rs`:

```rust
//! Pure CW (Morse) generation for station identification.
//!
//! No I/O and no audio device — this turns a callsign string into i16 PCM
//! samples. The modem submits those samples as a normal TxJob, so PTT
//! keying and play-out reuse the existing TX worker path.

/// One keyed or unkeyed span, measured in Morse time units (1 unit = 1 dit).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Segment {
    pub on: bool,
    pub units: u32,
}

/// Dot/dash pattern for a character, or None when there is no standard
/// Morse representation (encode skips those).
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

/// Synthesize keyed segments to i16 PCM at `sample_rate`. `wpm` sets dit
/// length via PARIS timing (dit = 1.2 / wpm seconds); `tone_hz` is the
/// sidetone. Keyed spans get a 5 ms raised-cosine rise/fall to suppress key
/// clicks; unkeyed spans are silence.
pub fn synthesize(segments: &[Segment], sample_rate: u32, wpm: u32, tone_hz: f32) -> Vec<i16> {
    let wpm = wpm.max(1);
    let dit_samples = ((sample_rate as f64) * 1.2 / (wpm as f64)).round() as usize;
    let total: usize = segments.iter().map(|s| dit_samples * s.units as usize).sum();
    let mut out = Vec::with_capacity(total);
    const AMP: f32 = 0.6 * 32767.0;
    let ramp = ((sample_rate as f32) * 0.005) as usize; // 5 ms
    let w = 2.0 * std::f32::consts::PI * tone_hz / sample_rate as f32;
    for seg in segments {
        let n = dit_samples * seg.units as usize;
        if !seg.on {
            out.extend(std::iter::repeat(0i16).take(n));
            continue;
        }
        let r = ramp.min(n / 2);
        for i in 0..n {
            let env = if r > 0 && i < r {
                0.5 * (1.0 - (std::f32::consts::PI * i as f32 / r as f32).cos())
            } else if r > 0 && i >= n - r {
                0.5 * (1.0 - (std::f32::consts::PI * (n - 1 - i) as f32 / r as f32).cos())
            } else {
                1.0
            };
            let s = (w * i as f32).sin() * AMP * env;
            out.push(s as i16);
        }
    }
    out
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cargo test -p graywolf-modem morse:: 2>&1 | tail -20`
Expected: all 7 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add graywolf-modem/src/morse.rs graywolf-modem/src/lib.rs
git commit -m "Add pure CW (Morse) sample-generation module"
```

## Task B2: Add the CW ID IPC messages and regenerate bindings

**Files:**
- Modify: `proto/graywolf.proto`
- Modify: `graywolf-modem/src/ipc/proto.rs`
- Regenerate: `pkg/ipcproto/graywolf.pb.go`, prost bindings

- [ ] **Step 1: Add oneof entries**

In `proto/graywolf.proto`, inside `oneof payload`, add to the Rust→Go group (use field **9**, the next free RX id):

```proto
    CwIdResult cw_id_result = 9;
```

and to the Go→Rust group (use field **22**, the next free TX id):

```proto
    TransmitCwId transmit_cw_id = 22;
```

- [ ] **Step 2: Add message definitions**

Add near the other audio/PTT messages:

```proto
// Go -> Rust: transmit the station callsign as CW (Morse) for ID / TX
// self-test. Callsign is already resolved and uppercased by Go.
message TransmitCwId {
  uint32 request_id = 1;        // echoed back in CwIdResult
  uint32 channel = 2;           // selects output device + PTT driver
  string callsign = 3;
}

// Result of a CW ID transmission attempt (submission to the TX worker).
message CwIdResult {
  uint32 request_id = 1;
  bool success = 2;
  string error = 3;             // empty on success
}
```

- [ ] **Step 3: Regenerate Go bindings**

Run: `make proto`
Expected: succeeds.

Run: `grep -c "TransmitCwId\|CwIdResult" pkg/ipcproto/graywolf.pb.go`
Expected: non-zero (types generated).

- [ ] **Step 4: Add the Rust proto.rs helper**

In `graywolf-modem/src/ipc/proto.rs`, add a constructor mirroring the others (next to where `test_tone_result` used to be):

```rust
    pub fn cw_id_result(r: CwIdResult) -> Self {
        Self { payload: Some(ipc_message::Payload::CwIdResult(r)) }
    }
```

Ensure `CwIdResult` is imported in that file's `use` block (mirror how the other result types are imported).

- [ ] **Step 5: Build Rust to regenerate prost bindings**

Run: `cargo build -p graywolf-modem 2>&1 | tail -20`
Expected: codegen succeeds; `CwIdResult` / `TransmitCwId` resolve. (`cw_id_result` is unused until Task B3 — a dead-code warning is acceptable here.)

- [ ] **Step 6: Verify Go compiles**

Run: `go build ./pkg/ipcproto/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add proto/graywolf.proto pkg/ipcproto/graywolf.pb.go graywolf-modem/src/ipc/proto.rs
git commit -m "Add TransmitCwId and CwIdResult IPC messages"
```

## Task B3: Rust modem CW ID handler

**Files:**
- Modify: `graywolf-modem/src/modem/mod.rs`

- [ ] **Step 1: Import the new result type**

In the `use crate::ipc::proto::{...}` block (around line 29), add `CwIdResult` to the imported names.

- [ ] **Step 2: Add the dispatch arm**

In the inbound-payload `match` (near the `Some(Payload::TransmitFrame(tf))` arm, around line 338), add:

```rust
            Some(Payload::TransmitCwId(req)) => {
                self.handle_transmit_cw_id(req);
            }
```

- [ ] **Step 3: Add `CwIdResult` to the inbound-ignore list**

In the ignore arm (the `|`-chain of Rust→Go types, around line 353), add `| Some(Payload::CwIdResult(_))`.

- [ ] **Step 4: Implement the handler**

Add this method to the same `impl` block that holds `handle_transmit_frame` (place it right after `handle_transmit_frame`, before the closing `}` of the impl). It mirrors `handle_transmit_frame`'s channel/audio-config resolution, but builds samples from the `morse` module and replies with a `CwIdResult`:

```rust
    fn handle_transmit_cw_id(&mut self, req: crate::ipc::proto::TransmitCwId) {
        const CW_WPM: u32 = 20;
        const CW_TONE_HZ: f32 = 700.0;

        let reply = |success: bool, error: String| {
            let _ = self.handle.send(&IpcMessage::cw_id_result(crate::ipc::proto::CwIdResult {
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
                reply(false, format!("unknown channel {}", req.channel));
                return;
            }
        };
        let acfg = match self.audio_configs.get(&ccfg.output_device_id) {
            Some(a) => a.clone(),
            None => {
                reply(false, format!("no audio config for output device {}", ccfg.output_device_id));
                return;
            }
        };

        let segments = crate::morse::encode(&req.callsign);
        if segments.is_empty() {
            reply(false, "callsign produced no CW symbols".to_string());
            return;
        }
        let mut samples = crate::morse::synthesize(&segments, acfg.sample_rate, CW_WPM, CW_TONE_HZ);

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
            sample_rate: acfg.sample_rate,
            output_device_id: ccfg.output_device_id,
            sink_config: audio::soundcard::SoundcardOutputConfig {
                device_name: acfg.device_name.clone(),
                sample_rate: acfg.sample_rate,
                channels: acfg.channels,
                audio_channel: ccfg.output_channel,
            },
        };

        match self.tx_worker.transmit(job) {
            Ok(()) => {
                *self.tx_frames.entry(req.channel).or_default() += 1;
                reply(true, String::new());
            }
            Err(e) => reply(false, e),
        }
    }
```

> Note: `channel_configs`/`audio_configs` are cloned here (rather than borrowed) so the later `&mut self` calls (`apply_ptt_config`, `tx_worker.transmit`, `tx_frames`) don't conflict with the immutable borrow. If `ChannelConfig`/`AudioConfig` are not `Clone`, read their definitions and copy out only the fields used above (`output_device_id`, `output_channel`, `sample_rate`, `device_name`, `channels`) into locals before the mutable calls.

- [ ] **Step 5: Verify (compile-check; CI for full target)**

Run: `cargo build -p graywolf-modem 2>&1 | tail -30`
Expected: compiles, or fails only on unrelated `cfg(linux)`-only code. No errors mentioning `handle_transmit_cw_id`, `morse`, `CwIdResult`, or `TransmitCwId`. If the host can't finish, note it; CI is the gate.

Run: `cargo test -p graywolf-modem morse:: 2>&1 | tail -5`
Expected: morse tests still PASS.

- [ ] **Step 6: Commit**

```bash
git add graywolf-modem/src/modem/mod.rs
git commit -m "Handle TransmitCwId in the modem: synthesize CW and submit a TxJob"
```

## Task B4: Go bridge `TransmitCwID`

**Files:**
- Modify: `pkg/modembridge/requests.go`
- Modify: `pkg/modembridge/bridge.go`
- Modify: `pkg/modembridge/session.go`
- Modify: `pkg/modembridge/bridge_stop_test.go`

- [ ] **Step 1: Add the dispatcher field + init**

In `pkg/modembridge/bridge.go`, add the field next to the other dispatchers:

```go
	cwDispatcher *dispatcher[*pb.CwIdResult]
```

and in `New`, next to the other dispatcher inits:

```go
		cwDispatcher: newDispatcher[*pb.CwIdResult](),
```

- [ ] **Step 2: Add the `TransmitCwID` method**

In `pkg/modembridge/requests.go`, add (mirrors the former `PlayTestTone` pattern):

```go
// TransmitCwID asks the Rust modem to transmit the given callsign as CW
// (Morse) on the named channel and waits for the submission result. The
// modem keys PTT, plays the CW audio, and unkeys via the TX worker.
func (b *Bridge) TransmitCwID(ctx context.Context, channel uint32, callsign string) error {
	if b.State() != StateRunning {
		return errors.New("modembridge: not in RUNNING state")
	}

	reqID, ch := b.cwDispatcher.Register()
	defer b.cwDispatcher.Cancel(reqID)

	msg := &pb.IpcMessage{Payload: &pb.IpcMessage_TransmitCwId{
		TransmitCwId: &pb.TransmitCwId{
			RequestId: reqID,
			Channel:   channel,
			Callsign:  callsign,
		},
	}}
	if err := b.sendIPC(msg); err != nil {
		return fmt.Errorf("send TransmitCwId: %w", err)
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case resp := <-ch:
		if resp == nil {
			return errBridgeStopped
		}
		if !resp.Success {
			return fmt.Errorf("CW ID failed: %s", resp.Error)
		}
		return nil
	case <-timer.C:
		return errors.New("modembridge: CW ID timeout")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Bridge) dispatchCwIdResponse(r *pb.CwIdResult) {
	b.cwDispatcher.Deliver(r.RequestId, r)
}
```

- [ ] **Step 3: Add the session dispatch case**

In `pkg/modembridge/session.go`, in the inbound `switch`, add:

```go
	case *pb.IpcMessage_CwIdResult:
		b.dispatchCwIdResponse(p.CwIdResult)
```

- [ ] **Step 4: Add the stop-test coverage**

In `pkg/modembridge/bridge_stop_test.go`, mirror the existing dispatcher patterns. Read the file first, then:

- Add a test-case entry that calls `b.TransmitCwID(context.Background(), 0, "N0CALL")` and asserts it unblocks with `errBridgeStopped` (mirror the former `PlayTestTone` entry shape).
- Add `"cw": adaptPbCwIdResult(cwCh),` to the channel map and declare `cwCh` like the others.
- Add the adapter:

```go
func adaptPbCwIdResult(c <-chan *pb.CwIdResult) <-chan any {
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
git commit -m "Add TransmitCwID request path to modem bridge"
```

## Task B5: Go webapi `POST /api/channels/{id}/cw-id`

**Files:**
- Modify: `pkg/webapi/channels.go`
- Modify: `pkg/webapi/dto/` (add `CwIdResponse` — put it in the channels DTO file; find it via grep below)
- Modify: `pkg/webapi/docs/op_ids.go`
- Test: `pkg/webapi/channels_cw_id_test.go` (create)

- [ ] **Step 1: Locate the channels DTO file**

Run: `grep -rln "ChannelResponse\|BeaconSendResponse" pkg/webapi/dto/`
Use the file that holds channel-related DTOs (likely `pkg/webapi/dto/channel.go`). Add there:

```go
// CwIdResponse is the body returned by POST /api/channels/{id}/cw-id.
type CwIdResponse struct {
	Status string `json:"status" example:"sent"`
}
```

- [ ] **Step 2: Add the op-id constant**

In `pkg/webapi/docs/op_ids.go`, near `OpManualPtt`/`OpSendBeacon`, add:

```go
	OpSendCwID = "sendCwId"
```

- [ ] **Step 3: Register the route**

In `pkg/webapi/channels.go`, in `registerChannels`, add after the `ptt` route:

```go
	mux.HandleFunc("POST /api/channels/{id}/cw-id", s.sendCwID)
```

- [ ] **Step 4: Write the handler**

In `pkg/webapi/channels.go`, add (mirrors `manualPtt` + the beacon refusal mapping). Ensure imports include `errors` and `github.com/chrissnell/graywolf/pkg/callsign`:

```go
// sendCwID transmits the station callsign as CW (Morse) on a channel. It
// refuses to key the radio when the station callsign is empty or N0CALL,
// and when the channel is not TX-capable.
//
// @Summary  Send CW ID (station callsign in Morse) on a channel
// @Tags     Channels
// @ID       sendCwId
// @Produce  json
// @Param    id path int true "Channel ID"
// @Success  200 {object} dto.CwIdResponse
// @Failure  409 {object} webtypes.ErrorResponse
// @Failure  422 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Router   /channels/{id}/cw-id [post]
func (s *Server) sendCwID(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	if s.bridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: "bridge not available"})
		return
	}

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

	if err := s.requireTxCapableChannel(r.Context(), "channel", id); err != nil {
		writeJSON(w, http.StatusConflict, webtypes.ErrorResponse{Error: err.Error()})
		return
	}

	if err := s.bridge.TransmitCwID(r.Context(), id, call); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, dto.CwIdResponse{Status: "sent"})
}
```

> Verify the exact import path/alias for `webtypes` and `dto` at the top of `channels.go` and match them (they are already used by sibling handlers in this file).

- [ ] **Step 5: Write the test**

Create `pkg/webapi/channels_cw_id_test.go`. Read an existing webapi test (e.g. `pkg/webapi/beacons_send_test.go`) first to copy the exact server-construction/test harness helpers used in this package (store setup, bridge stub, request helper). Then assert these cases:

- Empty station callsign → `422`.
- N0CALL station callsign → `422`.
- Non-TX channel → `409`.
- Valid callsign + TX-capable channel + bridge stub returning nil → `200` with `{"status":"sent"}`.

Use the package's existing patterns for stubbing `s.bridge.TransmitCwID` (e.g. an interface stub or fake bridge already present). If the bridge field is a concrete `*modembridge.Bridge`, follow how `manualPtt`/beacon tests inject a fake — replicate that mechanism rather than inventing a new one.

- [ ] **Step 6: Run the test**

Run: `go test ./pkg/webapi/ -run CwId -v`
Expected: PASS (all four cases).

- [ ] **Step 7: Full webapi build + test**

Run: `go build ./pkg/webapi/... && go test ./pkg/webapi/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add pkg/webapi/channels.go pkg/webapi/dto/ pkg/webapi/docs/op_ids.go pkg/webapi/channels_cw_id_test.go
git commit -m "Add channel CW ID REST endpoint with callsign and TX-capability guards"
```

## Task B6: Regenerate docs and API types

**Files:**
- Regenerate: `pkg/webapi/docs/gen/*`, `web/src/api/generated/api.d.ts`

- [ ] **Step 1: Regenerate**

Run: `make docs && cd web && npm run api:generate && cd ..`
Expected: succeeds.

- [ ] **Step 2: Verify the new endpoint is present**

Run: `grep -rn "cw-id\|sendCwId" pkg/webapi/docs/gen/swagger.json web/src/api/generated/api.d.ts`
Expected: matches in both.

- [ ] **Step 3: Docs drift check**

Run: `make docs-check`
Expected: no drift.

- [ ] **Step 4: Commit**

```bash
git add pkg/webapi/docs/gen web/src/api/generated/api.d.ts
git commit -m "Regenerate API docs and types for CW ID endpoint"
```

## Task B7: Frontend "Send CW ID" button

**Files:**
- Modify: `web/src/routes/channels/ChannelRow.svelte`

The click handler runs inline in `ChannelRow` (with local in-flight state), matching how `AudioDevices.svelte` handled Test Tone. The button is gated by the same TX-capability condition the PTT indicator uses: `!isKissOnly && channel.output_device_id && channel.output_device_id !== 0`.

- [ ] **Step 1: Add the API + toast imports and local state**

In the `<script>` block of `ChannelRow.svelte`, add to imports:

```javascript
  import { api } from '../../lib/api.js';
  import { toasts } from '../../lib/stores.js';
```

> Verify the relative depth: `ChannelRow.svelte` is in `web/src/routes/channels/`, so `lib` is two levels up (`../../lib/...`). Confirm against how `channelBacking.js` is imported (`../../lib/...`) at the top of the file.

After the `$props()` line, add:

```javascript
  let sendingCwId = $state(false);

  async function sendCwId() {
    sendingCwId = true;
    try {
      await api.post(`/channels/${channel.id}/cw-id`);
      toasts.success(`Sent CW ID on "${channel.name}"`);
    } catch (err) {
      toasts.error(`CW ID failed: ${err.message}`);
    } finally {
      sendingCwId = false;
    }
  }
```

> Confirm the channel display field name (`channel.name`) by checking how the row already renders the channel title; use whatever field that markup uses.

- [ ] **Step 2: Add the button to the action row**

In the action row that currently holds Edit/Delete (around line 139), add the CW ID button before Edit, gated on TX capability:

```svelte
  {#if !isKissOnly && channel.output_device_id && channel.output_device_id !== 0}
    <Button variant="ghost" onclick={sendCwId} disabled={sendingCwId}>
      {sendingCwId ? 'Sending…' : 'Send CW ID'}
    </Button>
  {/if}
  <Button variant="ghost" onclick={() => onEdit?.(channel)}>Edit</Button>
  <Button variant="danger" onclick={() => onDelete?.(channel)}>Delete</Button>
```

(Keep the existing Edit/Delete buttons; only add the guarded CW ID button.)

- [ ] **Step 3: Build the frontend**

Run: `cd web && npm run build`
Expected: build succeeds.

- [ ] **Step 4: Manual verification (record result)**

Run the app and confirm: the "Send CW ID" button appears only on modem-backed TX channels (not on KISS-only or RX-only rows); clicking a TX channel shows a success toast (or a clear error toast if no station callsign is set). Note the observed result in the task checklist.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/channels/ChannelRow.svelte
git commit -m "Add Send CW ID button to TX-capable channel rows"
```

## Task B8: Update the wiki

**Files:**
- Modify: `docs/wiki/code-map.md`
- Modify: `docs/wiki/invariants.md`

- [ ] **Step 1: Update code-map**

In `docs/wiki/code-map.md`: remove any Test Tone / `playTestTone` / `play_test_tone` references; add the new `POST /api/channels/{id}/cw-id` endpoint (handler `sendCwID` in `pkg/webapi/channels.go`), the `graywolf-modem/src/morse.rs` module, and the `TransmitCwId`/`CwIdResult` IPC messages. Keep it to navigation-level pointers, not internals.

- [ ] **Step 2: Update invariants**

In `docs/wiki/invariants.md`, add an invariant:

> **CW ID never keys the radio with an empty or N0CALL callsign.** `POST /api/channels/{id}/cw-id` resolves the station callsign via `Store.ResolveStationCallsign` and returns 422 before any IPC if it is empty or N0CALL.

- [ ] **Step 3: Verify references**

Run: `grep -rn "test tone\|Test Tone\|test-tone\|playTestTone" docs/wiki/`
Expected: no output (all Test Tone mentions removed from the wiki).

- [ ] **Step 4: Commit**

```bash
git add docs/wiki/code-map.md docs/wiki/invariants.md
git commit -m "Update wiki for channel CW ID; drop test tone references"
```

---

## Final verification (whole feature)

- [ ] **Go:** `go build ./... && go test ./pkg/webapi/... ./pkg/modembridge/... ./pkg/ipcproto/...` → PASS
- [ ] **Rust (host-buildable parts):** `cargo test -p graywolf-modem morse::` → PASS; `cargo build -p graywolf-modem` → no CW/test-tone errors (full `cfg(linux)` build via CI)
- [ ] **Frontend:** `cd web && npm run build` → PASS
- [ ] **Docs:** `make docs-check` → no drift
- [ ] **No stragglers:** `grep -rn "test_tone\|TestTone\|test-tone\|playTestTone\|PlayTestTone" pkg/ graywolf-modem/src/ web/src/ proto/ docs/wiki/` → no output
- [ ] **On-device (CI build + radio):** confirm clicking "Send CW ID" keys PTT and the callsign is audible/decodable in Morse on a second radio, and that an unset callsign yields a clear refusal.

---

## Self-review notes (author)

- **Spec coverage:** Part A removes Test Tone across all six layers named in the spec (UI, webapi, bridge, proto, Rust handler, regen). Part B covers the IPC messages, Rust `morse` + handler, Go bridge, webapi endpoint with all three refusal guards + success, frontend button gated by TX capability, and wiki updates — matching the spec's architecture and testing sections.
- **Type consistency:** Go `TransmitCwID`/`pb.TransmitCwId`/`pb.CwIdResult`/`OpSendCwID`/`dto.CwIdResponse`; Rust `morse::encode`/`morse::synthesize`/`Segment`/`handle_transmit_cw_id`/`IpcMessage::cw_id_result`; proto field ids 9 (RX) and 22 (TX) are the next free numbers after the Part A removals; frontend `sendCwId`/`sendingCwId`. Names are used identically across tasks.
- **No placeholders:** every code step shows complete code; verification steps give exact commands and expected output. Cross-file uncertainties (DTO file location, test harness shape, `webtypes` alias, Clone-ability of config structs, import depth) are flagged with a grep/read-first instruction rather than guessed.
