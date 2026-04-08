# Loopback TX verification (Phase B)

This is a **manual** end-to-end test for the TX path introduced in Phase B.
It confirms that a `TransmitFrame` IPC message from the Go side produces real
audio on the configured output device and that the same frame is decoded back
through the RX path.

Phase B does *not* implement half-duplex RX-during-TX mute — that is a
Phase D concern. The loopback test works *because* there is no mute, so
please do not add one here.

## Prerequisites (macOS)

1. Install [BlackHole 2ch](https://existential.audio/blackhole/) — a virtual
   audio device that routes output back into input with zero added latency.

2. Open **Audio MIDI Setup** and confirm `BlackHole 2ch` appears in the
   device list with 2 input channels and 2 output channels. No special
   aggregate device is needed.

## Graywolf configuration

Configure a single channel that uses BlackHole for *both* input and output:

| Field               | Value          |
| ------------------- | -------------- |
| Input device        | `BlackHole 2ch`|
| Output device       | `BlackHole 2ch`|
| Input channel       | 0              |
| Output channel      | 0              |
| Sample rate         | 48000          |
| Channels            | 2              |
| Modem               | AFSK 1200 Bell 202 |
| Profile             | A              |

Save the configuration and start the modem.

## Trigger a transmission

Use any path that sends a `TransmitFrame` IPC to the Rust side. The simplest
is the beacon system introduced in Phase 0:

1. Open the Beacons page in the UI.
2. Create a position beacon (or use an existing one).
3. Click **Beacon Now**.

Alternatively, wait for the scheduler to fire its next periodic beacon.

## What to look for

**Before Phase B** (baseline, to confirm you are on the right build):

```
graywolf-modem: TransmitFrame ignored (not implemented)
```

**After Phase B** — this log line should be *gone*. Instead you should see
the RX side decode the frame that was just transmitted:

```
graywolf-modem: ReceivedFrame ... <your beacon bytes>
```

In the UI, the packet monitor should show an incoming frame whose payload
matches the beacon you just sent, within a few hundred milliseconds of the
transmission.

## Sanity checks

- **No audible pops** when the modem starts or between transmissions. The
  output stream is kept continuously open; a pop on every frame would
  indicate the stream is being recreated, which is a regression.

- **txdelay / txtail are not doubled.** A 1200 baud frame with `txdelay=300`
  and `txtail=100` should take roughly `300 ms + (frame_bits / 1200 s) +
  100 ms` of wall time. If it takes *twice* that, something is sleeping on
  top of the audio buffer — Phase B deliberately does not do that because
  the flag-byte preamble/postamble is already encoded in the samples.

- **Silence between transmissions is truly silent.** Between frames the
  output callback emits zeros; you should not hear hum, buzz, or the
  previous frame's tail repeating.

## Troubleshooting

| Symptom                                | Likely cause                                             |
| -------------------------------------- | -------------------------------------------------------- |
| `open output device BlackHole 2ch`     | BlackHole not installed, or spelled differently in the   |
| failure log                            | Audio MIDI Setup. Check `EnumerateAudioDevices` output.  |
| TX audio plays but nothing decodes     | Input channel mismatch. BlackHole routes ch0→ch0,         |
|                                        | ch1→ch1; make sure input_channel == output_channel.      |
| `drain timeout` log                    | cpal's callback stopped draining. Check that the output  |
|                                        | device wasn't hot-unplugged; confirm `sample_rate` in    |
|                                        | the channel config matches what cpal negotiated on this  |
|                                        | device (Phase B has no renegotiation path).              |
| Tail of frame chopped                  | The hybrid drain wait is being shortcut. Verify that     |
|                                        | both conditions (`drained_samples >= watermark` **and**  |
|                                        | `expected_ms elapsed`) are required before unkeying.     |
