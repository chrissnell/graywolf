# FX.25 supervised hardware testing (1200-baud baseline)

The recorded acceptance and package below are **1200 baud only**. The separate
[experimental 300-baud draft](fx25-300.md) has no hardware/on-air acceptance;
its checklist must be completed by an appropriately equipped tester.

Local operator RF/PTT checks passed on the disposable ARM64 Raspberry Pi
station. See [validation](fx25-validation.md) for exact versions, results,
CPU baseline and limitations. This guide describes repeatable testing;
commands below do not send radio packets. Every RF transmission is initiated
by the operator. Do not apply these installation commands to a working
production station without a separate migration plan.

## Tested package and installation

The latest test archive combines FX.25 with the updated independent serial
RTS/DTR PTT PR #676. The feature branch itself contains no PTT PR changes.
Exact constituent commits and the archive checksum are in the validation
record and package `BUILD.txt`. This is a branch build, not a release.
Requires aarch64, glibc 2.36 or later and ALSA runtime libraries. The actual
operator station reported aarch64 and glibc 2.41. This archive is not for
x86 or Android, although the native FX.25 implementation is shared.

On the **disposable test Pi**, upload to `/tmp`, then run this single block.
It installs paired binaries and leaves Graywolf stopped for an explicit
operator start. PacketRover's UI remains available for sign-in. No mixer
commands are included. Enter appliance maintenance mode first if `/usr` is
read-only.

```sh
(
  set -eu
  cd /tmp
  test "$(uname -m)" = aarch64
  echo '58186e1f37370cb0cbaeb286c7d5b13cf5fc2e950db042bc17ec4ecca64281d3  graywolf-fx25-ptt-linux-arm64-d460a5df.tar.gz' | sha256sum -c -
  tar -xzf graywolf-fx25-ptt-linux-arm64-d460a5df.tar.gz
  cd /tmp/graywolf-fx25-ptt-linux-arm64-d460a5df
  sha256sum -c SHA256SUMS
  cat BUILD.txt
  sudo test -w /usr/bin
  sudo systemctl stop packetrover-direwolf.service graywolf.service
  if pgrep -x graywolf || pgrep -x graywolf-modem || pgrep -x direwolf; then
    echo 'Radio process still running; installation stopped.'
    exit 1
  fi
  sudo install -o root -g root -m 0755 graywolf /usr/bin/graywolf
  sudo install -o root -g root -m 0755 graywolf-modem /usr/bin/graywolf-modem
  cmp graywolf /usr/bin/graywolf
  cmp graywolf-modem /usr/bin/graywolf-modem
  /usr/bin/graywolf version
)
```

If the archive has already been extracted, inspect that directory rather than
overwriting unrelated files. `/tmp` is staging, not a service executable path:
systemd's `PrivateTmp` can hide it from services. On this appliance the existing
service uses `/usr/bin/graywolf` and `/usr/bin/graywolf-modem`. Check the actual
unit before reusing this procedure on another installation.

## Offline checks (no audio devices or PTT)

```sh
(
  set -eu
  cd /tmp/graywolf-fx25-ptt-linux-arm64-d460a5df
  test -x /usr/bin/time || { echo 'Install the OS time package first.'; exit 1; }
  ./fx25_audio decode fixtures/direwolf-damaged.wav > /tmp/fx25-recovered.txt
  wc -l /tmp/fx25-recovered.txt
  cat /tmp/fx25-recovered.txt
  for passes in 1 30; do
    /usr/bin/time -v ./fx25_audio workload fixtures/direwolf.wav "$passes" \
      > "/tmp/fx25-workload-${passes}.out" \
      2> "/tmp/fx25-workload-${passes}.time"
  done
  cat /tmp/fx25-workload-{1,30}.{out,time}
  vcgencmd get_throttled
)
```

Expected: 11 damaged-fixture recoveries, each ending in `fx25`; 33 and 990
workload frames. Use CPU seconds divided by audio seconds, not raw wall time,
when comparing recordings of different durations. See validation for the
completed paired upstream baseline. These are offline measurements, not RF
proof or whole-application CPU/RAM measurements.

## Establish native operation

Before starting, ensure saved scheduled beacons, digipeating and Internet-to-RF
gating are disabled for this supervised test. Starting an existing station
configuration can otherwise activate its normal automation. Then manually run:

```sh
(
  set -eu
  sudo systemctl start packetrover-aprs.service graywolf.service
  systemctl is-active packetrover-aprs.service graywolf.service
  systemctl is-active packetrover-direwolf.service || true
  pgrep -a -x graywolf
  pgrep -a -x graywolf-modem
  pgrep -a -x direwolf || true
  sudo ss -ltnp
)
```

Open PacketRover's UI and use its Graywolf sign-in link. Confirm the native
channel uses the installed modem, USB input/output, AFSK 1200 baud and
1200/2200 Hz tones. A KISS channel is not a native test. Configure the
already-tested serial PTT method and independent RTS/DTR polarity on the PTT
page. Preserve audio/mixer levels. Dire Wolf must not be running on the test
Pi; the separate Dire Wolf station is the independent receiver/transmitter.

## Supervised RF matrix

Use an appropriate frequency, identification and normal audio/radio levels.
Create a **disabled position beacon** using the sending station's actual
GPS/coordinates, no digipeater path, and a unique test marker in its comment.
Do not use a status packet: it can change the station's persistent APRS status.
Do not copy the other station's coordinates into the Pi's beacon. Beacon Now
ignores the Enabled scheduling flag, so leave the schedule disabled.

Initiate one packet at a time and wait for external reception before sending
another. Beacon Now means **queued**, not received over RF. If PTT is delayed,
inspect carrier/queue state rather than repeatedly clicking. Genuine busy
carrier still inhibits TX. The final tested build reconciles stale DCD after
audio reconfiguration and does not require a decoded packet to release TX.

| Check | Operator action/evidence |
|---|---|
| Graywolf → Dire Wolf | Test FX.25 off, then Auto, 16, 32 and 64; save settings before each manual send. Record exact external payload and FX.25 identification |
| Dire Wolf → Graywolf | On the separate station test `FX25TX 0`, 16, 32 and 64 using its existing audio/PTT setup. Record ordinary AX.25 or verified FX.25 badge, tag, parity and correction count in native Graywolf |
| Longer frame / PTT | Add a longer position comment ending in a unique marker; send with fixed 64. Observe actual keying/release and complete remote payload through the ending marker |
| Oversized fallback | Use a position comment too large for fixed 64. Expect explicit AX.25 fallback locally and complete ordinary AX.25 remote decode, without truncation |
| Delivery count | One event per deliberate send; repeat an identical payload after the first finishes and confirm a second event |
| Restart/configuration | Restart Graywolf after confirming schedules remain disabled; manually repeat RX/TX and saved off/on changes |
| Unsupported configuration | Verify editor/API rejects FX.25 with unsupported rate, tones or modulation. This passed automated checks; a physical editor trial was not independently reported |

Auto is recommended for everyday use. Fixed parity modes are useful for
specific use cases and testing. The selector controls TX only; native RX
recognizes all eleven standard tag/block formats regardless of TX settings.
Auto follows Dire Wolf `FX25TX 1` size selection, not measured signal quality.
Dire Wolf normally defaults FX.25 TX off for APRS (`FX25TX 0`); 16 explicitly
requests 16 parity bytes, not the default. Oversized packets fall back to
ordinary AX.25 in both Auto and fixed Graywolf modes. See [limits](fx25.md).

The log shows **FEC decoded · N symbols repaired** for verified RX, including
zero on clean codewords. Actual native TX shows encoding/tag/parity or
**AX.25 fallback** after local queue acceptance. These TX labels are not remote
reception evidence. Correction counts cover codeword symbols, including
parity; nonzero counts alone do not establish an RF sensitivity comparison.

Capture the separate Dire Wolf tmux pane without changing its configuration:

```sh
sudo -u direwolf tmux -S /run/direwolf/tmux.sock capture-pane -p -S -500 \
  > /tmp/direwolf-fx25-capture.txt
```

Choose the correct pane with `-t` if multiple panes exist. Record build/commit,
settings, exact markers, both stations' observations and per-send counts.
Preserve logs/screenshots separately from local API responses and counters.

## Recovery

This disposable installation replaced its binaries deliberately. Re-image it
when testing ends, as planned. To stop testing first, disable schedules and
stop Graywolf; if a radio remains keyed, manually unkey/disconnect PTT before
restarting anything. For an earlier return, reinstall the known-good paired
app/modem package and restore the intended station configuration before
starting radio services. Do not start native Graywolf and Dire Wolf against
the same audio/PTT hardware simultaneously.

On the separate production station, restore its original Graywolf and Dire
Wolf services/configuration after supervised testing. The completed test
script did this and verified unchanged ALSA mixer state and config hash.
Neither permanent station migration nor an upstream PR is part of these tests.
