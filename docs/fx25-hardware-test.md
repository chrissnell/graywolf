# FX.25 Pi 4 test station (operator-run, all RF checks pending)

Use a separate test station. Do not replace PacketRover or remove its Dire
Wolf installation. The test archive is relocatable: it installs no service,
changes no system binary and contains no automatic transmit script. It has
`graywolf`, `graywolf-modem`, the offline `fx25_audio` utility, fixtures,
`BUILD.txt`, checksums and these instructions. `BUILD.txt` gives the exact
source commit, compiler versions and component version stamp. This is a
branch test build, not a tagged release. Requires aarch64 Raspberry Pi OS
Bookworm or later (glibc 2.36+, ALSA runtime); physical compatibility still
needs confirmation. Do not install this ARM64 archive on x86 or Android.
The implementation itself is shared across supported native modem platforms.

## 1. Unpack and verify without changing the running station

On the **test Pi**, copy the provided archive and its adjacent SHA256SUMS into
an empty directory. Replace the archive filename below with the delivered
filename. Commands in this document are for you to run; none runs remotely.

```sh
uname -m                         # must report aarch64
getconf GNU_LIBC_VERSION          # must be glibc 2.36 or later
sha256sum -c SHA256SUMS
tar -xzf graywolf-fx25-linux-arm64.tar.gz
cd graywolf-fx25-linux-arm64
sha256sum -c SHA256SUMS
cat BUILD.txt
ldd ./graywolf-modem              # no "not found" dependencies
./graywolf version
```

If the downloaded archive is named with a commit suffix, use that exact name
in `tar -xzf`; the internal directory is still `graywolf-fx25-linux-arm64`.
You need permission to use USB audio and the USB serial device (normally the
`audio` and `dialout` groups). Preserve your already-tested PTT wiring and
settings. This branch contains only upstream/main plus FX.25; it does not
include the independent RTS/DTR polarity PR #676. If your hardware requires
that PR, stop before RF testing and request a disposable integration build;
record both exact commits. Do not permanently combine the branches.

## 2. Offline workload first (no audio devices, no PTT)

```sh
/usr/bin/time -v ./fx25_audio workload fixtures/direwolf.wav 1 2>workload-1.txt
/usr/bin/time -v ./fx25_audio workload fixtures/direwolf.wav 30 2>workload-30.txt
./fx25_audio decode fixtures/direwolf-damaged.wav > recovered.txt
wc -l recovered.txt              # must be 11; each line ends in fx25
cat workload-1.txt workload-30.txt
```

Install the OS `time` package if `/usr/bin/time` is unavailable. Expected
workload counts: 33 and 990 frames respectively. Compare user+system CPU time
to reported audio seconds; compare maximum RSS for 1 vs 30 passes. Send both
outputs, Pi OS version, temperature/throttling state (`vcgencmd get_throttled`)
and the version stamp. These are measurements to collect, not predeclared
performance thresholds or claims of RF gain. Investigate sustained memory
increase or inability to keep up with real-time before radio testing.

## 3. Establish native operation on the test station

Record which services are active, then stop **only on the test Pi**, manually.
If your units have different names, substitute them. Keep the independent
Dire Wolf receiver/transmitter on the other station running.

```sh
systemctl is-active graywolf direwolf > services-before-test.txt || true
sudo systemctl stop graywolf
sudo systemctl stop direwolf
pgrep -a -f '(^|/)(direwolf|graywolf)( |$)' || true
./graywolf --config "$PWD/test-config.db" \
  --history-db "$PWD/test-history.db" \
  --modem "$PWD/graywolf-modem" --http 0.0.0.0:8081 \
  2>&1 | tee test-station.log
```

Use a **fresh test-config.db**. Keep scheduled beacons, digipeating,
APRS-IS-to-RF gating and remote transmit actions off. Visit
`http://TEST_PI_ADDRESS:8081`, complete initial authentication, set your own
licensed callsign, and select USB audio devices. In **Channels**, create/edit
a native modem-backed channel: AFSK, 1200 baud, mark 1200, space 2200, your
USB input/output. Configure the same already-tested serial PTT method and
polarity on the **PTT** page. Do not click manual PTT or Beacon Now yet.
Verify the modem log/version and process path name this extracted binary.
Verify `pgrep -a direwolf` is empty on the Graywolf test Pi. A channel backed
by a KISS TNC is not a native-modem test.

## 4. RF tests (you initiate every transmission)

Use an appropriate test frequency, station identification and radio/audio
levels. Do not enable any repeating schedule merely to run this matrix.
Create a raw RF-only beacon with a unique sequence marker, e.g.
`>FX25 TEST 001`, a long interval, and the selected native channel. The
current UI calls the manual button **Beacon Now** (handbook: Send Now).
Keep the beacon disabled: the manual Beacon Now action explicitly ignores
the Enabled scheduling flag, so you do not need to enable a schedule.
Use the manual-send control only when ready. Keep an event log of each
deliberate send and every external decode.

| Check | Operator action and required evidence | Status |
|---|---|---|
| Native FX.25 TX → Dire Wolf | Enable FX.25 transmit in channel editor, save; initiate one beacon. On separate Dire Wolf receiver, enable FX.25 debug (`-d x`) and record correlation tag, successful FEC/HDLC/FCS decode and exact packet. Include a longer packet to exercise longer parity airtime. | Pending |
| Dire Wolf FX.25 TX → native RX | On the separate station select `FX25TX 16`, then 32 and 64, transmit identifiable packets one at a time. Graywolf test Pi must have Dire Wolf stopped and native audio active. Record exact payloads and single delivery. Its FX.25 TX toggle does not control RX. | Pending |
| Ordinary AX.25 both directions | Disable FX.25 transmit in Graywolf; set Dire Wolf `FX25TX 0`. Send one ordinary packet each way and verify external decode. Restore FX.25 afterward and confirm ordinary RX still works. | Pending |
| PTT duration/release | Observe physical keying through the complete longest FX.25 transmission and release after tail. Record separate receiver success; a local TX-done/counter alone is insufficient. | Pending |
| No duplicates / legitimate repeats | Record packet log and independent receiver for one send, then deliberately repeat the same payload after the first transmission finishes. Expect one delivered event per send, two events total. Check subscriber/packet log events rather than unique station-map rows. | Pending |
| Restart/configuration | Stop test app with Ctrl-C, restart identical command, repeat both directions. Toggle FX.25 off/on with save and repeat. Verify unsupported 300-baud/PSK combination is rejected in editor/API. | Pending |

For the separate Dire Wolf station, its normal channel-0 configuration uses:

```text
CHANNEL 0
MODEM 1200
FX25TX 16
```

Change only `FX25TX` to 32, 64 or 0 for the corresponding supervised test;
use that station's existing audio/PTT config and normal manual TX client.
No instruction here automatically sends a radio packet. If a clean FX.25
packet arrives via ordinary HDLC first, Graywolf metadata can say `none`;
`fx25` identifies an actual FEC recovery. Capture audio if diagnosing that
case; do not intentionally degrade a transmitter or change RF power without
a separate test plan.

Record: BUILD commit, OS/architecture, USB device names, channel settings,
PTT method, exact marker/frame, sender timestamp, receiver timestamp/tag,
received-event count, receiver audio capture if available, and outcome.
Keep RF reception evidence separate from API responses, queue completion,
local counters and offline tests. All matrix rows remain pending until you
provide their results; the PR remains draft.

## Recovery

Ctrl-C the foreground test app. Leave the extracted directory/test databases
in place for diagnosis or remove them later; no system binary was replaced.
Manually restart only the services that were active in `services-before-test.txt`:

```sh
# Run only the appropriate lines for your recorded prior service state:
sudo systemctl start direwolf
sudo systemctl start graywolf
```

If a radio remains keyed, unkey it/disconnect PTT before restarting software.
The untouched installed binaries/configuration are the recovery path. Do not
point the test build at the installed production configuration database.
