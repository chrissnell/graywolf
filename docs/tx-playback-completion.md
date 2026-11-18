# TX playback completion and PTT timing

## Problem and field evidence

The existing upstream TX worker, also present in combined.9 (4819b73c), treated
a sample watermark copied into
the CPAL callback as drained. It also waited the nominal submitted waveform
duration. Neither condition accounts for silence already queued at submission
or audio remaining between the callback and DAC.

The transmitting station's PCH output ran at 48 kHz, with 16,384-frame periods and a 32,768-frame
buffer (341/683 ms); a status sample reported 17,011 frames of delay (354 ms).
The other station’s USB output used 241/482-frame periods/buffer (5/10 ms), with a
reported 951-frame delay (20 ms). These are snapshots, not PTT-edge measurements.
Stereo frame counts are not divided by the number of channels.

Paired RF logs matched complete AX.25 raw bytes, paths and payloads within
15 seconds of the transmitter's queue/encoding record. APRS-IS copies were
excluded. TX timestamps are not measured RF start times. Rolling histories
cover different durations; do not compare the counts as equal-time rates.

| User-controlled condition | FX-valid RX | Plain HDLC RX | Unmatched TX |
| --- | ---: | ---: | ---: |
| Original 100 ms tail, approximately 44 minutes | 3 | 48 | 9 |
| 1000 ms tail, approximately 10:50–11:20 EST | 32 | 0 | 1 |
| Return to 100 ms, approximately 11:22–11:32 EST | 0 | 10 | 1 |

All recorded TX encoding outcomes were FX.25. Captured mixer and software gains
did not change. Initial FX successes were first in closely queued pairs;
long-tail successes included isolated transmissions. This is strong operational
evidence for tail-dependent truncation, consistent with the playback-completion
gap. Physical audio and PTT-edge captures have not yet been collected.

## Comparison with Dire Wolf 1.8.1

Dire Wolf's ALSA path flushes its pending software buffer and calls
`snd_pcm_drain()` in `audio_wait()` before returning to `xmit_ax25_frames()`.
The transmitter also waits any remaining calculated airtime before releasing
PTT. It does not rely solely on having copied samples into an output callback.
Its configured TXDELAY/TXTAIL generate pre/postamble flags; they are not a
substitute for draining pending playback.

Dire Wolf's source comments acknowledge historically inconsistent waiting
behavior from the underlying drain operation. Its additional calculated-
airtime check is a backstop for a drain that returns early. Graywolf retains
that duration safeguard too, while the CPAL presentation deadline accounts
for the device latency missing from its former callback-consumption check.

Graywolf retains its continuously running CPAL stream, which emits silence
between submissions. Draining/stopping ALSA directly is not available through
CPAL's cross-platform interface. Instead, CPAL supplies an output callback
timestamp and a predicted playback timestamp for the callback's first frame.
On the pinned CPAL 0.18.2 ALSA backend this prediction includes ALSA delay
frames. The fix uses their difference plus the actual position of the final
submitted sample in that callback to compute its presentation deadline.

References:

- [Dire Wolf audio_wait](https://github.com/wb2osz/direwolf/blob/1.8.1/src/audio.c#L1552)
- [Dire Wolf PTT/duration sequencing](https://github.com/wb2osz/direwolf/blob/1.8.1/src/xmit.c#L885)
- [CPAL 0.18.2 timestamp API](https://docs.rs/cpal/0.18.2/cpal/struct.OutputStreamTimestamp.html)

## Implementation and scope

- Keep callback-consumed watermarks distinct from presentation completion.
- Publish a watermark/deadline only when actual submitted samples were rendered;
  later silence cannot postpone completion indefinitely.
- Count mono sample positions as output frames, including stereo routing; use
  the final submitted sample position rather than the entire callback size.
- Require presentation completion and the existing nominal-duration safeguard
  before unkeying. Do not add TXTAIL again: it is already in the generated audio.
- Allow a bounded five-second completion grace beyond waveform duration for
  CPAL sinks. A missing/backwards timestamp cannot falsely signal completion.
  On timeout, release PTT and discard the sink so old queued audio cannot play
  under a later transmission. Other sink implementations retain their existing
  completion/grace behavior; Android JNI playback is not changed by this fix.
- Preserve stream-rebuild behavior: discard lost audio and release its waiter;
  never replay abandoned samples when the replacement stream starts.

PTT still asserts before samples are submitted, as it must for the preamble.
Already-buffered silence may cause additional keyed lead-in. This change fixes
the unkey decision; it does not optimize that initial silence or change device
buffer negotiation, gains, channel access, FX.25 encoding, or dashboard LEDs.

Playback timestamps are backend predictions, not physical instrumentation.
Affected PCH/radio results at the normal 100 ms tail are recorded below.
Low-latency USB field trials are recorded below, with their small-sample
limitations. The old-binary
1000 ms tail is a documented rollback workaround, rather than a new default.

## Regression coverage

Hardware-free tests distinguish callback consumption from 354 ms presentation
latency; cover repeated submissions, idle callbacks, missing timestamps,
discard/rebuild, stereo position accounting in F32/I16/U16, and bounded timeout
with PTT release. Existing FX.25 generation, independent WAV decoding and PTT
sequencing tests remain relevant. Local test/build outcomes are recorded in
the change description; field validation must not be claimed from unit tests.

Local validation on Linux AMD64:

- `cargo test --locked --lib`: 235 passed, 5 failed, 1 ignored. All five
  failures reproduce on unmodified combined.9: three Unix-socket tests are
  blocked by the workspace, one test requires an available default audio
  device, and one references an absent FLAC fixture.
- Focused tests with `--features android-test-stub` for the new clock,
  output-position accounting, TX worker and Android audio TX: 19 passed.
- `cargo check --locked --all-targets`: passed.
- `git diff --check`: passed.

## Additional ordinary AX.25 evidence

The operator disabled FX.25 TX just before 11:50 EST, retaining 100 ms tail
and the old modem binary. The later captures retain the rolling packet
history, so collection at approximately 12:07 still covers the test interval.
From 11:50:05 through 12:06:48 there are 14 TX records whose encoding metadata
explicitly says `encoding=ax25, fx25_requested=false`. None has a matching RF
reception at the other station under the same full-byte/path/payload and
15-second matching rule. The receiver decoded 25 other RF packets between
11:50 and 12:07, and its packet history covers the full interval.

Captured transmitter mixer controls and software audio-device configuration
are unchanged from the preceding trial. This supports a format-independent
TX completion defect: without the FX.25 block after the inner HDLC frame,
ordinary AX.25 reception disappears in this sample. These are software TX
records, not independent RF measurements; no match does not independently
prove that each individual transmission was physically emitted and clipped.
The corresponding old-binary AX.25 control with 1000 ms tail is described
below.
At that stage neither the new code nor a release containing it had been deployed.

### Ordinary AX.25 control with 1000 ms tail

The operator increased only the tail to 1000 ms just before 12:15 EST,
leaving FX.25 transmit disabled on the same old binary. Paired captures
collected at approximately 12:26–12:27 retain all ten TX records from
12:15:05 through 12:26:49. Every record explicitly identifies ordinary
AX.25 with `fx25_requested=false`; every one has a full-byte/path/payload
RF match at the receiving station within 15 seconds. All ten receptions
are ordinary AX.25, as expected. Captured software gains and hardware mixer
controls on both stations are unchanged from the preceding trial.

| Old-binary ordinary AX.25 condition | TX records | Matching RF RX | Unmatched |
| --- | ---: | ---: | ---: |
| 100 ms tail, approximately 11:50–12:07 EST | 14 | 0 | 14 |
| 1000 ms tail, approximately 12:15–12:27 EST | 10 | 10 | 0 |

These differing observation windows are compared as reception outcomes per
recorded TX, not as traffic rates. The control strengthens the evidence for
a format-independent playback-completion fault: the longer tail restores
ordinary AX.25 reception as well as complete FX.25 validation. Patched-binary
hardware trials with the normal 100 ms tail follow below.

## Patched PCH transmitter acceptance at 100 ms tail

The transmitting station installed paired c.10 components from source commit
34d36ddd3359025c949e33813e9269d41f9ceba5. Its live startup log confirms the
c.10 modem. The receiving station stayed on combined.9 / 4819b73c throughout,
isolating the transmitter update. The operator confirmed 100 ms tail for
both trials; the original collector does not independently export timing.
Captured hardware mixer controls and software audio gains stayed unchanged.

| Patched transmitter trial | Recorded TX | Matching RF RX | Validated FX.25 | Unmatched |
| --- | ---: | ---: | ---: | ---: |
| Ordinary AX.25, approximately 12:57–13:09 EST | 10 | 9 | Not applicable | 1 |
| FX.25 Auto, approximately 13:17–13:41 EST | 18 | 18 | 18 | 0 |

Each match requires the full raw AX.25 bytes and full displayed header/path/
payload, at the other station's RF input within 15 seconds of the TX record.
The ordinary AX.25 trial's unmatched TX occurred at 13:02:37.885; the receiver
had queued its own digipeat about 77 ms earlier. That is consistent with
half-duplex overlap/contention, not a proven measurement of the RF/PTT edges.

All 18 FX.25 receptions validated tag 0x06 (128 data bytes, 32 parity bytes),
with zero Reed–Solomon symbol corrections. They include isolated position
beacons, digipeated traffic and third-party message packets. There are no
captured drain-timeout, sink-submit or PTT-unkey errors. Different test-window
durations are not compared as traffic rates.

These results support the playback-completion fix on the affected large-buffer
Linux PCH/radio installation in both ordinary AX.25 and FX.25 at 100 ms tail.
They do not establish perfect RF delivery, direct waveform timing, every
FX.25 mode, or universal compatibility with untested audio backends.

### Low-latency device scope

The completion wait follows each device's reported playback timing rather
than imposing a fixed extra second. Unit tests cover short and large pending
playback intervals and preserve the configured tail. This design is intended
to benefit devices with excessive buffering while avoiding unnecessary fixed
padding on low-latency devices. USB-transmitter field trials below show successful operation in both formats,
with no captured TX completion errors. Their small samples do not rule out
all regressions or establish perfect RF delivery.

## Standalone upstream scope

The implementation was ported onto upstream main bde16a1752ef96e8907aea9dc18c698e3e96a9d7
without importing the FX.25 feature or other combined-build changes. The port
contains only the audio presentation clock, soundcard callback integration,
TX-worker completion handling and generic regression tests. Eleven focused
clock/output/TX-worker tests pass on that upstream base. Its production change
does not depend on FX.25; the field evidence includes ordinary AX.25 recovery.
Both merge orders with the current FX.25 PR head f66fe3a86f2ab85650d08ee020da06f4dc698892
are conflict-free and produce the same merged tree. Twelve focused tests of
that merged source pass, including the FX.25 parity-airtime test. Neither
feature depends on the other. A separate general modem/PTT PR is appropriate.

The low-latency USB transmitter installed c.10 and kept its original 100 ms
tail. Its ordinary AX.25 trial from approximately 13:50–14:08 EST contains
three TX records, two exact RF matches and one unmatched TX. For the unmatched
14:02:37.417 TX, the receiving station queued its own digipeat approximately
267 ms later. This is consistent with half-duplex overlap or contention,
without independently measuring RF/PTT timing. Mixer controls and software
gains remained unchanged, and no TX completion errors were captured. The
small sample demonstrates successful USB AX.25 operation; the final planned
USB FX.25 trial follows.

The USB FX.25 Auto trial started at 14:10 EST, still at 100 ms tail. Three
TX records were retained: two exact RF matches validated tag 0x06 / 32 parity
with zero corrected symbols; one had no matching RX. For that 14:12:37.635
TX, the receiving station queued its own digipeat approximately 106 ms later.
As in the AX.25 USB trial, contention/half-duplex overlap is plausible but
not proven by independent RF/PTT timing. Both devices ran paired c.10
components; gains and mixers stayed unchanged; no TX completion errors were
captured. The planned on-air matrix is complete.

| c.10 transmitter at 100 ms tail | Ordinary AX.25 RX/TX | FX.25-valid RX/TX |
| --- | ---: | ---: |
| Large-buffer PCH | 9/10 | 18/18 |
| Low-latency USB | 2/3 | 2/3 |

These observations support restoration on the affected device and successful
operation of the tested low-latency device, with the limited USB sample
explicitly retained. They do not justify an unconditional no-regression
guarantee for every radio, backend, driver or timestamp implementation.
