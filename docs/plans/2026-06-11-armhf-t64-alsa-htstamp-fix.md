# Design: Fix the t64 ALSA crash on 32-bit ARM without building with time64

Status: proposed (for execution by another agent)
Date: 2026-06-11
Issue: #231 ("Modem crash-loops on 32-bit Raspberry Pi OS (Pi Zero / armv6)")
Related: PR #246 (the shipped-but-non-working `GNU_TIME_BITS=64` attempt), release GRA-39 (v0.13.16 retag is blocked on this)

---

## 1. TL;DR

`graywolf-modem` SIGSEGV crash-loops within ~0.3s of starting audio capture on
32-bit ARM systems whose `libasound2` was rebuilt for 64-bit `time_t` (the
"t64" transition). The crash is a stack-buffer overflow at one FFI call inside
`alsa-rs`. The fix that shipped in #231/#246 (compile the whole armhf binary
with `RUST_LIBC_UNSTABLE_GNU_TIME_BITS=64`) **cannot be linked by our release
toolchain** and is not in any released binary.

This doc specifies a smaller, toolchain-neutral fix: give that one FFI call a
buffer that is large enough for the t64 `struct timespec` (16 bytes), so the C
library cannot overflow it. We do **not** build with time64, so the binary
keeps linking against the current toolchain and keeps running on both pre-t64
and t64 systems. The decoded timestamp value does not need to be correct
because graywolf discards it (see section 5).

Net effect: **4 dependency forks -> 1**, no toolchain change, single armhf
binary works everywhere.

---

## 2. Who is affected, and does it matter? (answers to the questions raised on GRA-39)

**Is "32-bit + t64 userland" a real, deployed combination?** Yes. The Debian
"time64" (t64) transition rebuilt 32-bit (`armhf`) shared libraries -- including
`libasound2`, now packaged as `libasound2t64` -- with 64-bit `time_t`. This is
the **current 32-bit Raspberry Pi OS** (Bookworm/Trixie generation). Any 32-bit
ARM install on current Pi OS is this combination.

**Is the bug reporter on it?** Yes, directly. Issue #231 is titled "Modem
crash-loops on 32-bit Raspberry Pi OS (Pi Zero / armv6)". PR #246 confirms the
repro: a Pi Zero on post-t64 32-bit Pi OS, SIGSEGV ~0.3s into capture. Issue
#129 shows we also have Trixie users.

**Is it important to support?** Yes. The Pi Zero / Pi 1 / Pi 2 (all 32-bit only)
and 32-bit installs on Pi 3/4 are a primary deployment target for low-power
APRS/packet nodes (iGate, digipeater, tracker) -- the core graywolf audience.
This is not a fringe config; it is one of the most common ways graywolf is run.

Conclusion: this must work on 32-bit t64 Pi OS, and ideally keep working on
older 32-bit OSes too.

---

## 3. Root cause

`alsa-rs` (`alsa` crate) `Status` accessors read the ALSA hardware timestamp:

```rust
// alsa-0.11.0/src/pcm.rs
pub fn get_htstamp(&self) -> timespec {
    let mut h = timespec { tv_sec: 0, tv_nsec: 0 };      // Rust libc::timespec
    unsafe { alsa::snd_pcm_status_get_htstamp(self.ptr(), &mut h) };
    h
}
// ... and get_trigger_htstamp(), get_audio_htstamp(), same shape.
```

`snd_pcm_status_get_htstamp(const snd_pcm_status_t*, snd_htimestamp_t*)` copies
a `struct timespec` into the caller's pointer. Sizes:

| build / lib | `struct timespec` |
|---|---|
| 32-bit, legacy time32 | 8 bytes (`tv_sec` i32 @0, `tv_nsec` i32 @4) |
| 32-bit, t64 | 16 bytes (`tv_sec` i64 @0, `tv_nsec` i32 @8, pad @12) |
| 64-bit (arm64/x86_64) | 16 bytes (always) |

Our armhf binary is built with the **default time32** `libc`, so Rust's
`timespec` is 8 bytes. On a t64 system `libasound` writes **16 bytes** into that
8-byte stack slot, smashing the saved return address -> SIGSEGV (PC=0x0).

`cpal`'s ALSA backend calls these accessors internally on the capture path to
populate `InputCallbackInfo` timestamps, so the crash happens regardless of what
the application does.

It only reproduces on **32-bit + t64** because that is the only place Rust's
`timespec` (8 bytes) and the system's (16 bytes) disagree. On 64-bit both are 16
bytes; on legacy time32 both are 8 bytes.

---

## 4. Why the shipped approach (#231) does not work, and why not option 1

**Shipped attempt (PR #246): `RUST_LIBC_UNSTABLE_GNU_TIME_BITS=64` for armhf.**
This makes Rust's `libc` emit the 16-byte t64 `timespec` across the whole graph,
matching `libasound2t64`. Two fatal problems, discovered while trying to cut
v0.13.16:

1. **It does not link.** time64 makes `libc` emit calls to `__ioctl_time64`
   (and friends), which only exist in **glibc >= 2.34**. Our cross-rs images for
   `arm-unknown-linux-gnueabihf` / `armv7-unknown-linux-gnueabihf` are
   **Ubuntu Bionic (glibc 2.27)**. Result: `undefined reference to __ioctl_time64`
   at link time (hits our own ioctl code, `gpiocdev`, etc.). The flag produces a
   binary that compiles but never links on this pipeline.
2. **It cascades.** To even compile under time64, four crates need patches
   because they build `timespec`/`timeval` with struct literals or wrong-width
   casts: `alsa` (fixed upstream-master only), `gpiocdev-uapi` (no fix anywhere),
   `nix` (no fix anywhere; also forces our direct dep 0.29->0.30 and a code
   change), and `cpal` (fixed in 0.18, which is an API migration of our audio
   code). amd64 builds green with all four, but armhf still dies at the link step
   above.

**Option 1: bump the cross images to glibc >= 2.34 (Ubuntu 22.04/24.04 base).**
Would let time64 link, but: keeps all four forks; and the armhf binary would then
**require glibc >= 2.34 at runtime**, dropping legacy 32-bit systems (Bullseye,
glibc 2.31) that today's time32 binary still serves. Bigger CI change for a
worse compatibility story. Not chosen.

---

## 5. The key insight that makes option 2 cheap

graywolf **does not consume cpal's stream timestamps.** Every audio callback in
`graywolf-modem/src/audio/soundcard.rs` ignores the `InputCallbackInfo` /
`OutputCallbackInfo` argument:

```rust
move |data: &[i16], _| { ... }     // the `_` is the callback info / timestamps
```

cpal still *calls* `get_htstamp` internally (to build that info), which is why
the crash happens -- but the **value never reaches graywolf**. Therefore:

- We only need to stop the **overflow**. The decoded timestamp value is
  irrelevant; we do not need to detect the runtime ABI or decode correctly.
- A buffer sized for the **largest** possible `struct timespec` (16 bytes) is
  safe on every system: t64 writes 16 (fits exactly), time32 writes 8 (fits with
  room to spare). No overflow either way, on any 32-bit or 64-bit target.

Also audited: **`libasound` (via `alsa-sys`) is the only C library on the Linux
armhf path that exchanges time structs.** Everything else is pure Rust: `nusb`,
hidapi's `linux-native-basic-udev` backend, `gpiocdev`. (`ioctl-sys` /
`linux-raw-sys` are pure-Rust syscall helpers, no external C lib.) So fixing this
one boundary is sufficient.

---

## 6. Design

Build everything **time32** (no `GNU_TIME_BITS` flag, current toolchain). Patch
`alsa-rs` so the three `Status` htstamp accessors pass a 16-byte buffer to the C
call instead of an 8-byte `libc::timespec`:

```rust
pub fn get_htstamp(&self) -> timespec {
    // libasound2t64 writes a 16-byte (64-bit time_t) struct timespec even on
    // 32-bit, where Rust's libc::timespec is 8 bytes. Hand the C call a 16-byte
    // buffer so it cannot overflow our stack slot, then copy the low fields out.
    // (cpal consumers that ignore the timestamp -- like graywolf -- never read
    // the returned value; correctness across both ABIs is best-effort.)
    #[repr(C, align(8))]
    struct HtsBuf([u8; 16]);
    let mut buf = HtsBuf([0u8; 16]);
    unsafe {
        alsa::snd_pcm_status_get_htstamp(self.ptr(), buf.0.as_mut_ptr() as *mut _);
    }
    let tv_sec = i64::from_ne_bytes(buf.0[0..8].try_into().unwrap());
    let tv_nsec = i32::from_ne_bytes(buf.0[8..12].try_into().unwrap());
    timespec { tv_sec: tv_sec as _, tv_nsec: tv_nsec as _ }
}
```

Apply the identical shape to `get_trigger_htstamp` and `get_audio_htstamp`. Notes:

- The decode assumes the **t64 little-endian** layout (tv_sec i64 @0, tv_nsec i32
  @8). armhf is LE. On a legacy time32 system the decoded value is wrong, but
  unused (section 5). If a big-endian 32-bit target ever matters, gate the offsets
  on `target_endian`.
- The returned `timespec` is the build's `libc::timespec` (8 bytes on a time32
  armhf build); `tv_sec as _` truncates to i32 there. Fine -- value is discarded.
- This is the only change in `alsa-rs`. It is also a legitimate upstream
  contribution ("don't overflow when libasound is t64 but we built time32"); file
  a PR so the fork can eventually be dropped.

### Everything that gets reverted/removed (the cleanup half of the work)

The branch `fix/armhf-t64-build` currently carries the time64 approach. Option 2
removes all of it:

- `graywolf/Cargo.toml` `[patch.crates-io]`: drop the `diwic/alsa-rs`,
  `gpiocdev-uapi`, `nix`, and `cpal` pins; add a single pin to the new
  `alsa` buffer-fix fork.
- `graywolf-modem/Cargo.toml`: revert `nix = "0.30"` back to `"0.29"`; `cpal`
  stays `"0.17"` (no fork).
- `graywolf-modem/src/tx/ptt_unix.rs` and `ptt_cm108_unix.rs`: revert to the
  nix-0.29 form (`open()` returns `RawFd`, wrapped via `OwnedFd::from_raw_fd`;
  restore the `FromRawFd`/`AsRawFd` imports).
- `.github/workflows/release.yml`: remove the `RUST_LIBC_UNSTABLE_GNU_TIME_BITS`
  matrix line.
- `Cross.toml`: remove `RUST_LIBC_UNSTABLE_GNU_TIME_BITS` from both armhf
  `passthrough` lists.
- Regenerate `Cargo.lock` (expect: `alsa` -> fork git source; no `gpiocdev`/`nix`/
  `cpal` git sources; `nix` back to a 0.29.x registry entry).
- Delete the now-unused GitHub forks: `chrissnell/gpiocdev-rs`,
  `chrissnell/nix`, `chrissnell/cpal`. Keep only `chrissnell/alsa-rs`.

---

## 7. Validation / tests

This is a runtime ABI bug that only reproduces on **32-bit + t64**, so the tests
have to reach that environment. Three layers, cheapest first.

**Layer 1 -- host unit test (runs in current CI, x86_64).** In the patched
`alsa-rs`, lock the invariant that the FFI buffer is at least as large as any
`libasound` `timespec`:

```rust
const _: () = assert!(core::mem::size_of::<HtsBuf>() >= 16);
const _: () = assert!(core::mem::align_of::<HtsBuf>() >= 8);
```

Plus a small unit test of the decode against known bytes. Limited on x86_64 (where
`timespec` is already 16) but it pins the layout assumption and the buffer size.

**Layer 2 -- emulated armhf + t64 canary test (the real regression gate, in CI).**
Run a 32-bit ARM container with a t64 userland (Debian Trixie / Ubuntu 24.04
`armhf`, which ship `libasound2t64`) under QEMU/binfmt. Build a tiny test that:
- `snd_pcm_status_malloc`s a status struct (no sound card needed --
  `get_htstamp` just copies the stored timestamp out);
- calls `snd_pcm_status_get_htstamp` into a buffer with guard bytes after offset
  16; asserts the guard bytes are untouched.

This **fails on stock alsa-rs (8-byte slot -> overflow into the guard) and passes
with the 16-byte fix** -- a true regression test for the exact bug, with no
hardware. The armhf job linking at all is also a standing guard against anyone
re-introducing the time64 flag.

**Layer 3 -- hardware smoke test (acceptance).** On a real Pi Zero running current
32-bit Pi OS: run graywolf, start capture, leave it for >= 1 minute. Pass = no
crash-loop (the #231 symptom). Timestamp values are not checked (unused). This is
the final acceptance gate; layers 1-2 should make it a formality.

### Compatibility matrix (what we expect after the fix)

| target | time_t | libasound timespec | stock 0.13.13 binary | option-2 binary |
|---|---|---|---|---|
| 32-bit, legacy (Bullseye) | 32 | 8 bytes | works | works (no overflow) |
| 32-bit, t64 (current Pi OS) | 64 | 16 bytes | **crash-loop** | works (16-byte buffer) |
| arm64 / x86_64 | 64 | 16 bytes | works | works (unchanged) |

One time32 binary, correct on all of them.

---

## 8. Execution checklist

1. Fork `diwic/alsa-rs`; branch off the **v0.11.0** tag (the version cpal 0.17.3
   resolves; cpal pins `alsa = "^0.11"`). Apply the 16-byte-buffer patch to the
   three htstamp accessors + Layer-1 static asserts/test. Push; open an upstream PR.
2. `graywolf/Cargo.toml`: `[patch.crates-io] alsa = { git = "<fork>", rev = "<sha>" }`;
   remove the gpiocdev-uapi/nix/cpal/diwic-alsa pins.
3. `graywolf-modem/Cargo.toml`: `nix = "0.29"`; leave `cpal = "0.17"`.
4. Revert `ptt_unix.rs` + `ptt_cm108_unix.rs` to the nix-0.29 `open()`/`from_raw_fd` form.
5. Remove `RUST_LIBC_UNSTABLE_GNU_TIME_BITS` from `release.yml` and `Cross.toml`.
6. `cargo update`; sanity-check `Cargo.lock` (alsa -> fork; no nix/gpiocdev/cpal git sources; nix back to 0.29.x).
7. Add the Layer-2 QEMU armhf-t64 canary job to CI.
8. `gh workflow run release.yml --ref <branch>` and confirm **linux-arm** +
   **linux-armv7** link and go green (this is what the time64 approach could never do).
9. Hardware smoke on a Pi Zero (Layer 3).
10. Delete the gpiocdev/nix/cpal forks. Complete the v0.13.16 release: delete +
    re-push the `v0.13.16` tag (do **not** rewrite the release note -- retag
    contract in graywolf/CLAUDE.md), watch CI to green.

### Local validation harness (no hardware / no root needed for compile checks)

`cargo check` does not link, so you can compile-check the whole modem without
libasound or root:
- stub pkg-config: write a fake `alsa.pc` (`Name: alsa` / `Version: 1.2.11` /
  `Libs: -lasound` / empty `Cflags`) and export `PKG_CONFIG_PATH` to its dir;
- download a `protoc` binary and export `PROTOC=/path/to/protoc` (the build script needs it);
- then `PKG_CONFIG_PATH=... PROTOC=... cargo +stable check --manifest-path graywolf-modem/Cargo.toml`.
The actual t64 overflow only manifests under Layer 2 (QEMU) / Layer 3 (hardware).

---

## 9. Risks / open questions

- **Only-boundary assumption.** We rely on `libasound` being the sole t64 C-lib
  boundary on Linux armhf (audited true today). If a future dependency adds
  another C lib that passes time structs, re-audit.
- **Timestamp value is lossy on the non-native ABI.** Acceptable *only* because
  graywolf discards cpal's stream timestamps. If graywolf ever starts using them,
  this fix must be revisited (runtime ABI detection, or option 1).
- **Fork lifetime.** `chrissnell/alsa-rs` is frozen at 0.11.0 (cpal 0.17.3 pins
  `^0.11`); drop it once the upstream PR is released.
- **`get_audio_htstamp` width.** Audio htstamp is also a `timespec` in alsa-rs;
  same 16-byte treatment. Confirm there is no other `Status` accessor returning a
  bare `timespec` that cpal touches (current alsa-rs: just the three).

---

## Postscript: the inverse regression (#336)

The 16-byte buffer landed in fork rev `bd06687`, but its decode assumed the
**time64 layout unconditionally** (`tv_sec` i64 @0, `tv_nsec` i32 @8). Section 5
above bet that the decoded value was "wrong but unused" on time32 systems --
that bet was wrong. cpal's ALSA backend *derives and validates* `htstamp -
trigger_htstamp` internally before handing over the (ignored) `InputCallbackInfo`,
so the garbage decode on a **pre-t64 / time32** `libasound` (Ubuntu 22.04 armv7l,
glibc 2.35) produced a flood of `get_htstamp was earlier than
get_trigger_htstamp` errors and endless stream rebuilds. Reported as #336.

Fix (fork rev `56099e8`, pinned in `Cargo.toml`): decode per ABI. 64-bit targets
read the native 16-byte layout; 32-bit targets recover `tv_sec` at offset 0 and
take `tv_nsec` from the t64 slot (@8) when non-zero, else the time32 slot (@4).
The buffer is zeroed before the C call, so the unwritten tail is deterministic
and a single time32 binary is correct on **both** pre-t64 and t64 userlands.
Unit tests cover both layouts (alsa-rs PR #1).

Test-gap note: the original plan added only a t64 canary (Layer 2). A time32 /
pre-t64 canary is still missing and is what would have caught this -- worth
adding alongside the t64 one.
