# Loopback TX verification (Phases B + C)

This is a **manual** end-to-end test for the TX path. It has two parts:

1. **Phase B loopback** — confirms that a `TransmitFrame` IPC message from the
   Go side produces real audio on the configured output device and that the
   same frame is decoded back through the RX path (no radio, no PTT line,
   pure audio).

2. **Phase C on-air** — plugs in real hardware (DigiRig + radio) and confirms
   that PTT keys the rig, AFSK is audible on a separate receiver, and a
   second APRS receiver decodes the frame.

Phases B/C do *not* implement half-duplex RX-during-TX mute — that is a
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

---

# Phase C — On-air verification with a real radio

This section assumes Phase B loopback already passes and you now want to
confirm that PTT keying and RF output work end-to-end with real hardware.
The primary target is a [DigiRig](https://digirig.net/) on a Mac driving a
handheld transceiver, but the same steps apply to any serial-handshake-line
PTT setup on Linux.

## Prerequisites

- DigiRig (or equivalent USB-serial + USB-audio interface) plugged in.
- A handheld VHF radio wired to the DigiRig's audio + PTT connector.
- A *second* APRS-capable receiver on the same band (another HT, a scanner,
  another TNC, or [aprs.fi](https://aprs.fi) if your location has IS coverage).

## Finding the serial device

### macOS

On macOS, the DigiRig shows up as **two** serial devices:

```
ls /dev/cu.usbserial-* /dev/tty.usbserial-*
/dev/cu.usbserial-AB0NZ3Z7    /dev/tty.usbserial-AB0NZ3Z7
```

**Use the `cu.*` path, never the `tty.*` path.** The `tty.*` variant on
macOS blocks the `open()` call forever waiting for DCD to assert — the
process will hang and PTT will never key. This is a macOS-wide serial-port
gotcha, not a graywolf bug. The `cu.*` ("call-up") variant skips the DCD
wait and is the right device for any dial-out use case.

### Linux

On Linux you will usually have `/dev/ttyUSB0` or `/dev/ttyACM0` — no
`cu.*`/`tty.*` split. Use whichever the kernel assigned.

### Windows

Open Device Manager → **Ports (COM & LPT)** to find the assigned COM
port number, or run `mode` at a `cmd.exe` prompt to list active ports.

For `COM1` through `COM9` either `COM3` or `\\.\COM3` works as a device
path. **For `COM10` and higher the `\\.\` prefix is mandatory** — e.g.
`\\.\COM14` — because the Win32 namespace reserves the bare `COM10+`
names for legacy reasons and `CreateFileW` will fail without the prefix.

## Configure PTT

1. Open the **PTT** page in the graywolf UI.
2. Click **+ Add PTT** (or edit an existing entry).
3. Fill in:
   - **Channel ID** — match the channel ID you configured for loopback.
   - **Method** — `Serial RTS` (or `Serial DTR` if your DigiRig is wired
     that way — most DigiRigs use RTS).
   - **Device Path** — the `/dev/cu.usbserial-*` path you found above.
   - **Invert Polarity** — leave unchecked unless your radio is wired
     backwards and keys on line-low.
4. Save.

## Configure audio

Configure the channel to use the DigiRig's audio device as both input and
output (so you can still see RX decodes coming back from the rig if it
happens to pick up local traffic). A typical DigiRig appears in the
audio-device picker as `USB Audio CODEC` or similar.

## Trigger a transmission

1. Open the **Beacons** page.
2. Create a position beacon if you don't already have one.
3. Click **Beacon Now**.

## What to look for

- **TX LED on the radio lights** for the full preamble + frame duration,
  then drops. The duration is approximately
  `txdelay_ms + (frame_bits * 1000 / 1200) + txtail_ms` —
  e.g. ~500 ms for `txdelay=300, txtail=100` plus a short frame.
- **AFSK tones are audible** on the second receiver (if you can hear them
  through the speaker, the tone balance is in the right ballpark — there
  is no explicit TX gain knob yet).
- **The frame decodes** on the second APRS receiver / aprs.fi, showing the
  same callsign and coordinates you configured on the beacon.
- **No stuck PTT.** The TX LED goes dark at the end of the frame and stays
  dark. If it stays lit, see troubleshooting below.

## Troubleshooting (Phase C)

| Symptom | Likely cause |
| --- | --- |
| Modem hangs on startup after `ConfigurePtt` | You pointed at `/dev/tty.usbserial-*` on macOS. Switch to the `cu.*` variant and restart. |
| `open /dev/ttyUSB0: Permission denied` (Linux) | Your user is not in the `dialout` group. Either add yourself (`sudo usermod -aG dialout $USER`, log out/in) or run graywolf via a user that is. |
| `CreateFileW \\.\COM14: Access is denied.` (Windows) | Another application is holding the COM port open. Close `rigctld`, terminal emulators, or any vendor control-panel app that auto-opens the device. Sysinternals `handle.exe \\.\COM14` shows the current holder. |
| TX LED flickers briefly at startup, then nothing | A process on the system (modemmanager, cu, gpsd) is grabbing the same device and toggling its lines. Graywolf itself never calls `tcsetattr`. `sudo lsof /dev/cu.usbserial-*` on macOS or `sudo fuser /dev/ttyUSB0` on Linux shows the current holder. |
| TX LED lights but radio is silent | Audio routing wrong. Verify the DigiRig appears as an **output** device in the audio picker and that `output_channel` matches how your DigiRig is wired (most are mono on channel 0). |
| Radio keys but modulation sounds distorted / the second receiver shows decode errors | Output level is too hot and clipping the radio's mic input. Lower the channel's TX gain, or attenuate at the DigiRig hardware pot if it has one. The modem targets half-scale i16 on purpose to leave 6 dB of downstream headroom. |
| PTT never releases — radio stays keyed forever | A `thread::sleep(txtail_ms)` or similar was reintroduced around unkey. It must not be. See the comment above `driver.unkey()` in `src/modem/tx_worker.rs` and the sequencing note in `src/tx/ptt.rs`. |
| Two channels share one serial device and only the first one keys | The port-sharing logic is broken. Check that `PortRegistry::open_or_reuse` is returning the cached `Arc` on the second call; `Arc::strong_count` should be ≥ 2 after both drivers are built. |
