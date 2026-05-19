# Invariants

Cross-cutting "if you change X, also touch Y" rules. Each entry: rule,
one-line *why*, source.

### 1. Root `Cargo.toml` is a workspace shim, sole member `graywolf-modem`

*Why:* Lets cross-rs's Docker mount see `proto/` and `VERSION` from the repo root; cargo output therefore lands at `/target/`, which Makefile, modem-path fallbacks, modembridge integration tests, and release CI all hard-code.

Source: [`../../Cargo.toml`](../../Cargo.toml) (comment is authoritative),
[`../../Makefile`](../../Makefile),
[`../../pkg/app/modem.go`](../../pkg/app/modem.go).

### 2. `proto/graywolf.proto` is the single Go<->Rust IPC contract

*Why:* Both sides regenerate from this file (wire format `[4 BE bytes length][IpcMessage]`), so any change requires regenerating both.

Source: [`../../proto/graywolf.proto`](../../proto/graywolf.proto). Go:
`make proto` -> `pkg/ipcproto/graywolf.pb.go`. Rust:
[`../../graywolf-modem/build.rs`](../../graywolf-modem/build.rs) ->
`OUT_DIR/graywolf.rs`.

### 3. Version locks

*Why:* `make bump-*` rewrites every version-bearing file in lockstep, so hand edits drift and downstream packaging breaks.

Source: bump targets in [`../../Makefile`](../../Makefile)
(`VERSION`, `graywolf-modem/Cargo.toml`, `Cargo.lock`,
`packaging/aur/PKGBUILD`, `packaging/aur/.SRCINFO`,
`docs/handbook/installation.html`).

### 4. Release notes precede the bump

*Why:* The bump targets `grep` `pkg/releasenotes/notes.yaml` for the *new* version and refuse to run if the entry is missing.

Source: [`../../Makefile`](../../Makefile),
[`../../pkg/releasenotes/notes.yaml`](../../pkg/releasenotes/notes.yaml),
[`../../CLAUDE.md`](../../CLAUDE.md).

### 5. Release notes ship as-tagged (retag contract)

*Why:* When CI fails after tag-push and you delete-and-re-tag the same version, leave the note alone -- operators see whatever shipped at the final successful tag, and silent reword between retags is a trust hazard.

Source: [`../../CLAUDE.md`](../../CLAUDE.md) ("Retag contract").

### 6. Plain ASCII in release notes

*Why:* No emoji, em dashes, smart quotes, or non-ASCII punctuation -- keeps the operator-facing changelog portable since bump targets do not re-encode the YAML.

Source: [`../../CLAUDE.md`](../../CLAUDE.md);
[`../../pkg/releasenotes/notes.yaml`](../../pkg/releasenotes/notes.yaml)
header.

### 7. PMTiles / offline-maps infra lives in `~/dev/graywolf-maps`, not here

*Why:* Tile generation, R2 sync, manifest publishing, and the origin Cloudflare Worker all live in the maps repo; graywolf is a *client* (`mapsauth`, `mapscache`, MapLibre rendering).

Source: absence of those modules in this tree;
`~/dev/graywolf-maps/.context/graywolf-client-integration.md`.

### 8. Audio I/O is on the Rust side

*Why:* CPAL runs in `graywolf-modem` and Go talks to the modem only via the IPC proto, keeping realtime DSP out of Go's GC and platform-specific audio in one place.

Source: [`../../graywolf-modem/src/audio/`](../../graywolf-modem/src/audio/);
no CPAL dep in `pkg/`; control surface is the proto messages
`ConfigureAudio` / `StartAudio` / `StopAudio` / `EnumerateAudioDevices`.

**8a. Capture-device enumeration never probes in-use hardware.**
On Linux, `EnumerateAudioDevices` (`collect_input_devices_linux`
in `modem/mod.rs`) collapses cpal's numeric/symbolic ALSA aliases to one
entry per physical card (via `/proc/asound/cards`) and *probes* each
**idle** card — briefly opening it — to badge "Recommended" the PCM form
that actually streams. A card currently held open by a live capture
stream is **never** probed (opening a second stream on in-use hardware
can disrupt the running radio) and is surfaced from the in-use snapshot
the handler passes in, so a rescan keeps showing it. The
string-only `is_recommended_pcm_id` heuristic is now used **only** by the
flare `--list-audio` path and the Linux *output* path; flare
`recommended` and the live capture picker intentionally diverge (the
separate `--list-audio` process can't probe safely). The live picker is
authoritative.

Output side: idle outputs enumerate unchanged (`collect_devices`), but a
configured/in-use output card (e.g. the AIOC's shared in/out PCM, whose
`supported_output_configs()` fails once RX holds the card) is re-appended
from the in-use-output snapshot, one entry per physical card, without
opening the device — so a configured output never vanishes from
detection while audio is running.

### 9. PTT enumeration vs. driving split

*Why:* Go enumerates PTT hardware and Rust drives it, so both sides must agree on the identifier scheme passed via `ConfigurePtt.method` and `ConfigurePtt.device`.

Source: [`../../pkg/pttdevice/`](../../pkg/pttdevice/);
[`../../graywolf-modem/src/tx/`](../../graywolf-modem/src/tx/);
[`../../proto/graywolf.proto`](../../proto/graywolf.proto)
(`ConfigurePtt`).

### 9a. PTT is one-row-per-channel; PUT supports atomic rekey

*Why:* `PttConfig.ChannelID` carries a uniqueIndex, so an operator changing the channel field on an existing PTT means *move*, not copy. `PUT /api/ptt/{channel}` matches the body's `channel_id` against the URL's: same → in-place upsert; different → atomic rekey in a single GORM transaction (`Store.RekeyPttConfig`), with `ErrPttChannelTaken` mapped to HTTP 400 on collision. The bridge reload (`notifyBridgeForChannel` → `ReconfigureAudioDevice`) is global, so a single notify covers both vacated and newly-targeted channels.

Source: [`../../pkg/configstore/store.go`](../../pkg/configstore/store.go) (`RekeyPttConfig`, `ErrPttChannelTaken`);
[`../../pkg/webapi/ptt.go`](../../pkg/webapi/ptt.go) (`updatePttConfig`).

### 9b. PTT writes are rejected on KISS-TNC channels

*Why:* A KISS TNC owns the radio interface end-to-end including PTT, so a graywolf-driven PTT row on top of a KISS-only channel (`Channel.InputDeviceID == nil`) is at best redundant and at worst keys the wrong radio after the operator reassigns channels (issue #110). The webapi handlers gate POST `/api/ptt` and both branches of PUT `/api/ptt/{channel}` (in-place upsert AND rekey) through `validatePttChannelBacking`, which returns HTTP 400. For rekey the validator runs against `req.ChannelID` (the move target), not the URL id, so an operator cannot bypass the rule by editing an existing PTT row onto a KISS channel. The PTT page mirrors the rule by hiding KISS-only channels from the channel dropdown.

Source: [`../../pkg/webapi/ptt.go`](../../pkg/webapi/ptt.go) (`validatePttChannelBacking`);
[`../../pkg/webapi/ptt_test.go`](../../pkg/webapi/ptt_test.go) (`TestPttRejectsKissOnlyChannel`);
[`../../web/src/routes/Ptt.svelte`](../../web/src/routes/Ptt.svelte) (`modemChannels` filter).

### 9c. Android PTT method rides in `gpio_pin`; every PTT read path must round-trip it

*Why:* On Android there is no dedicated PTT-method column. The Channels modal sends `POST /api/ptt {method:"android", gpio_pin:N}` where `N` is the method int from spec Appendix B (1 = CP2102N RTS / Digirig, 2 = CM108 HID, 3 = AIOC CDC-ACM DTR, 4 = VOX). That int rides in `PttConfig.GpioPin` the whole way: configstore → `session.go` (which must NOT zero `gpio_pin` for `method=="android"` — it only zeroes it for `method=="gpio"`) → `ConfigurePtt.GpioPin` → Rust `build_driver` (`PttMethod::Android` does `let method = cfg.gpio_pin as i32`) → `AndroidPtt` → JNI → Kotlin `UsbPttAdapter.pttSet`. Therefore **any response DTO or read path that surfaces a channel's PTT must carry `gpio_pin`**. If it doesn't, the edit modal can't restore the selected method, falls back to CP2102N, and the next save of that channel POSTs `gpio_pin=1`, silently downgrading a saved AIOC/CM108 config to CP2102N (this was the `ChannelPtt`-DTO bug fixed in #149). The Kotlin doc was correspondingly wrong: AIOC keys via CDC-ACM DTR (RTS held low), not CM108 HID GPIO.

Source: [`../../pkg/webapi/dto/channel.go`](../../pkg/webapi/dto/channel.go) (`ChannelPtt.GpioPin`, `ChannelPttFromModel`);
[`../../pkg/modembridge/session.go`](../../pkg/modembridge/session.go) (`gpioPin` zeroed only for `method=="gpio"`);
[`../../graywolf-modem/src/tx/ptt.rs`](../../graywolf-modem/src/tx/ptt.rs) (`PttMethod::Android` arm);
[`../../android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt) (`pttSet`, `setAiocRts`).

### 10. Gitignored output dirs are not canonical

*Why:* `target/`, `bin/`, `dist/`, `rust-bin/`, `rust-artifacts/`, `web/dist/`, `.worktrees/`, `.context/`, `*.db*` regenerate from sources and are gitignored, so never reference them as authoritative.

Source: [`../../.gitignore`](../../.gitignore);
[`build-pipelines.md`](build-pipelines.md).

### 11. Generated-bindings drift is enforced in CI

*Why:* `docs-check` and `api-client-check` regenerate to a tempdir and diff against committed copies, running inside `make go-test` and the pre-commit hook.

Source: [`../../Makefile`](../../Makefile),
[`../../.githooks/`](../../.githooks/), `make install-hooks`.

### 12. Web UI is embedded into the Go binary at compile time

*Why:* `go:embed all:dist` produces a self-contained binary, but the dir must exist at build time -- a placeholder `.keep` suffices until `npm run build` populates it.

Source: [`../../web/embed.go`](../../web/embed.go).

### 13. Modem readiness signal is on stdout, not the IPC channel

*Why:* The Go parent waits for `\n` (Unix) or `<port>\n` (Windows) on the modem's stdout before connecting, avoiding a connect-retry race against the bind.

Source: [`../../graywolf-modem/src/ipc/server.rs`](../../graywolf-modem/src/ipc/server.rs);
[`../../pkg/modembridge/`](../../pkg/modembridge/)
(`ipc_unix.go`, `ipc_windows.go`).

### 14. Version display string is shared across Go and Rust

*Why:* Both sides produce `v<Version>-<GitCommit>` and modembridge checks them at startup, so a mismatch immediately flags that the two halves of the build disagree.

Source:
[`../../cmd/graywolf/main.go`](../../cmd/graywolf/main.go),
[`../../pkg/app/config.go`](../../pkg/app/config.go),
[`../../graywolf-modem/build.rs`](../../graywolf-modem/build.rs).

### 15. Default IS->RF policy is deny

*Why:* The IS->RF rule engine evaluates rules in priority order and drops anything no rule matches, preventing accidental flooding of RF with arbitrary internet traffic.

Source: [`../../pkg/igate/filters/filters.go`](../../pkg/igate/filters/filters.go)
(package comment).

### 16. TX path is single-source-of-truth via `txgovernor`

*Why:* Every TX origin (KISS, AGW, beacons, digipeater, iGate IS->RF) funnels through one Governor for per-channel rate limits, dedup, and priority -- new sources must route through it, not around.

Source: [`../../pkg/txgovernor/governor.go`](../../pkg/txgovernor/governor.go)
(package comment).

### 16b. KISS `tcp-client` defaults to a TX-capable TNC link

*Why:* A `tcp-client` KISS interface dials OUT to a hardware TNC, so its only useful default is `Mode=tnc` + `AllowTxFromGovernor=true` -- otherwise it registers no TX backend and silently transmits nothing while still receiving (issue #128). When `Mode` is **omitted from the request**, both the API boundary (`dto.KissRequest.ToModel`) and the store backstop (`normalizeKissInterface`) apply this default for `tcp-client` only; every other interface type keeps the historical `modem` default. An *explicitly supplied* `Mode` is always honored verbatim. Note `POST`/`PUT /api/kiss` is full-resource replace (`Store.UpdateKissInterface` does `db.Save`, like every DTO in the codebase): a `PUT` that omits `mode` re-applies the default exactly as create does -- it does NOT merge against the persisted row. This is consistent with how every other KISS field default (reconnect bounds, ingress rates) already behaves on `PUT`. The one hazardous case -- silently enabling TX on a `tcp-client` whose channel also has an audio input device (a modem backend), which would double-transmit -- cannot occur on either path: `validateKissInterface` independently rejects `tnc`+`AllowTxFromGovernor` on a modem-backed channel before the row is written. Migration 20 (`kiss_tcp_client_tx_default`) repairs pre-existing `tcp-client` rows stuck at the old `modem`/`false` default, and likewise skips any row whose channel has an audio input device.

Source: [`../../pkg/webapi/dto/kiss.go`](../../pkg/webapi/dto/kiss.go),
[`../../pkg/configstore/store.go`](../../pkg/configstore/store.go) (`normalizeKissInterface`),
[`../../pkg/configstore/migrate.go`](../../pkg/configstore/migrate.go) (`migrateKissTcpClientTxDefault`).

### 17. RX fanout carries provenance via `ingress.Source` (in-process)

*Why:* Lets KISS broadcast suppress its own loopback without leaking a transport detail into the proto -- the provenance tag is in-process only, never on the wire.

Source: [`../../pkg/app/ingress/source.go`](../../pkg/app/ingress/source.go)
(package comment).

### 18. Generated artifacts that ship in commits

*Why:* CI's drift guards (see [invariant 11](invariants.md)) only work if regenerated copies are committed; bump targets stage them so each release tag includes them.

Files:
[`../../pkg/ipcproto/graywolf.pb.go`](../../pkg/ipcproto/graywolf.pb.go),
`pkg/webapi/docs/gen/swagger.{json,yaml}`,
[`../../web/src/api/generated/api.d.ts`](../../web/src/api/generated/api.d.ts),
[`../handbook/openapi.json`](../handbook/openapi.json),
[`../handbook/openapi.yaml`](../handbook/openapi.yaml).
Source: `GENERATED_SPEC_FILES` in [`../../Makefile`](../../Makefile).

### 19. APRS callsigns are NOT redacted by `graywolf flare`

*Why:* APRS callsigns are public ham-radio identifiers, and the entire purpose of a flare submission is to diagnose APRS issues. Redacting them would defeat the operator UI's correlation between flare config and observed packets.

Source: [`../../pkg/diagcollect/redact/doc.go`](../../pkg/diagcollect/redact/doc.go), enforced by `TestEngine_PreservesAPRSCallsigns`.

### 20. Hostname hash is consistent within one submission

*Why:* The hostname appears across many fields (system, log lines, file paths). Hashing it once per submission and substituting the same 8-hex tag everywhere preserves cross-references inside the operator UI without leaking the literal name.

Source: [`../../pkg/diagcollect/redact/hostname.go`](../../pkg/diagcollect/redact/hostname.go); the engine wires it through `SetHostname`.

### 21. Redaction always runs before review

*Why:* The review TUI is the user's audit step; the user can only audit what we're going to ship. Showing a pre-scrub payload would mean a user pressing 's' submits a different document than they reviewed.

Source: `pkg/diagcollect/Collect` calls `redact.ScrubFlare` before returning; the review TUI re-applies it after every ad-hoc rule add.

### 22. Review is mandatory for non-dry-run, non-`--out` invocations

*Why:* Anything that leaves the host across the network must pass through human eyes. `--dry-run` and `--out` print the same scrubbed payload, but only `--dry-run` skips the network -- and both still run the scrub.

Source: `cmd/graywolf/flare.go`'s control flow: the network `client.Submit` call is unreachable except through `runReviewLoop` returning `OutcomeSubmit`.

### 23. Channel mode gates TX, not RX

When a channel's `Mode` is `packet`, the beacon scheduler, digipeater engine, iGate IS→RF gate, APRS messages sender, and `ax25conn.Manager.Open` (connected-mode session bring-up) all skip, refuse, or down-shift when asked to transmit on that channel. Conversely, `ax25conn.Manager.Open` rejects channels in `aprs`-only mode. RX is unchanged: frames keep demodulating and fan out via the existing fanout; subscribers self-filter.

The lookup contract is fail-open at the resolver: if `ChannelModeLookup` returns an error or `nil`, callers behave as if the channel were `aprs` (preserves the legacy any-channel-does-anything behavior). The IS→RF runtime gate also fails open -- a transient DB error does not silently suppress beaconing or gating.

*Why:* Operators may want to dedicate a channel to AX.25 connected-mode without it accidentally absorbing APRS beacons, digipeated packets, IS→RF traffic, or outbound APRS messages. The `aprs+packet` value preserves the legacy "any channel does anything" behavior for setups that don't care about the split.

Source: [`../../pkg/configstore/channel_mode_lookup.go`](../../pkg/configstore/channel_mode_lookup.go),
[`../../pkg/configstore/models.go`](../../pkg/configstore/models.go)
(`Channel.Mode` field comments).

### 24. AX.25 `link_stats` is emitted only while CONNECTED, 1Hz, never blocking

The `pkg/ax25conn` session goroutine arms a 1-second `tStats` timer on
the transition into `StateConnected` and stops it on every transition
out. Each tick refreshes `LinkStats` from the live session vars
(V(S)/V(R)/V(A), N2 retry count, busy flags, RTT EWMA) under the stats
mutex and emits one `OutLinkStats` event through the observer. The
bridge translates that to the `link_stats` envelope feeding the
TelemetryPanel.

The contract:

1. **Cadence is exactly 1Hz while CONNECTED.** No tick fires in
   DISCONNECTED, AWAITING_CONNECTION, AWAITING_RELEASE, or
   TIMER_RECOVERY -- only `setState(StateConnected)` arms the timer.
2. **Re-entry is harmless.** `setState` stops the timer on leaving
   CONNECTED before re-arming on the next entry, so a CONNECTED ->
   TIMER_RECOVERY -> CONNECTED bounce produces a single armed timer.
3. **Emit is non-blocking.** The observer call hits the same pump
   goroutine as every other `OutEvent`, and the pump non-blocking-sends
   to the WebSocket out-channel. A jammed browser never back-pressures
   the LAPB session goroutine.

*Why:* The telemetry panel must show useful RTT/sequence data without
ever stalling the LAPB timers. Tying the tick to a state-machine
event-bit (`pendStats`) instead of an external `time.Ticker` keeps the
session loop authoritative -- timer expiry races and CPU contention
can't spawn ghost ticks while the link is down.

Source: [`../../pkg/ax25conn/session.go`](../../pkg/ax25conn/session.go)
(`statsTick`, `setState`),
[`../../pkg/ax25conn/stats_tick_test.go`](../../pkg/ax25conn/stats_tick_test.go).

### 25. Theme top-level rule must be `:root:root[data-theme="<id>"]`

Every CSS theme in `web/themes/` declares its variables under a
doubled `:root:root[data-theme="..."]` selector (specificity (0,2,1))
rather than the simpler `:root[data-theme="..."]` (0,1,1). Sub-rules
that target descendants (`:root[data-theme="X"] .badge`) are already
(0,1,2) and don't need the bump.

*Why:* `@chrissnell/chonky-ui` ships an OS-dark-mode fallback at
`@media (prefers-color-scheme: dark) :root:not([data-theme="light"])`
with specificity (0,1,1). Vite bundles graywolf's theme stylesheets
*before* chonky-ui's, so on a specificity tie the chonky-ui rule wins
the cascade and clobbers any explicit graywolf theme whenever the OS
reports dark. The doubled `:root:root` lifts every graywolf theme one
notch above that fallback, so the operator's explicit choice always
beats the OS preference. Without this, Windows users with OS dark mode
could not switch back to a light graywolf theme.

Source: [`../../web/themes/graywolf.css`](../../web/themes/graywolf.css)
(top-level rule + comment block),
[`../../web/themes/README.md`](../../web/themes/README.md) (theme
authoring guide),
[`../../web/node_modules/@chrissnell/chonky-ui/dist/css/chonky.css`](../../web/node_modules/@chrissnell/chonky-ui/dist/css/chonky.css)
(`@media (prefers-color-scheme: dark) :root:not([data-theme="light"])`).

### 26. Actions classifier consumes the packet

When the Actions classifier matches an inbound APRS message (addressee
in the trigger surface AND info-field begins with `@@`), the packet is
consumed before the messages router sees it. No `messages.in` row is
written for that packet. The classifier and the messages router share
one [`messages.Preflight`](../../pkg/messages/preflight.go) instance
constructed by `messages.Service`, so a consumed Actions packet still
gets an auto-ACK on every copy and a `(from, msgid, text_hash)` dedup
verdict — the first copy reaches the runner, every subsequent copy
within the dedup window is ACKed and silently dropped (APRS101 §14.2).

*Why:* Actions are operator-controlled command channels, not
correspondence; surfacing every Action invocation in the inbox would
clutter the operator's message view and break the audit-log-as-source-of-truth
contract for Actions traffic. Consumption is the cleanest cut. Sharing
the preflight closes the prior gap where action senders kept retrying
because no ACK ever arrived, and where iGate fan-out delivered N copies
that each fired the executor.

Source: [`../../pkg/actions/classifier.go`](../../pkg/actions/classifier.go),
[`../../pkg/app/rxfanout.go`](../../pkg/app/rxfanout.go)
(`dispatchRxFrame`),
[`../../pkg/app/wiring.go`](../../pkg/app/wiring.go)
(`onIGateIsRxPacket`, `wireActions`),
[`../../pkg/messages/preflight.go`](../../pkg/messages/preflight.go),
[`../../pkg/messages/router.go`](../../pkg/messages/router.go)
(`Router.classify`),
[`actions.md`](actions.md) ("Hot path", "Preflight" sections).

### 27. Inbound dedup + auto-ACK is centralized in `messages.Preflight`

Any new inbound discriminator that diverts traffic before
`messages.Router` (today only `actions.Classifier`) MUST send an
auto-ACK and consult the dedup ring via `messages.Preflight`, or
iGate-relayed duplicates will re-fire its handlers and the original
sender will retry forever.

Source: [`../../pkg/messages/preflight.go`](../../pkg/messages/preflight.go),
[`../../pkg/messages/service.go`](../../pkg/messages/service.go)
(`(*Service).Preflight()`),
[`actions.md`](actions.md) ("Preflight: shared auto-ACK + dedup").

### 28. iGate enable/disable is hot-reloadable; reload plumbing is unconditional

The iGate's reload signal channel (`a.igateReload`), the reload-drainer
goroutine, the RF→IS fanout adapter (`a.igateOut`, an `*igate.IgateOutput`
holding an `atomic.Pointer[Igate]`), and the live `IGateLineSender`
adapter passed to `messages.Service` (`a.igateLineSender`) are ALL
allocated at boot regardless of the persisted `IGateConfig.Enabled`
flag. `App.reloadIgate` then handles three transitions on every signal
from `signalIgateReload`:

1. **disabled → enabled**: build a fresh `*igate.Igate` via
   `App.buildIgateInstance`, store the pointer + propagate to
   `a.igateOut.SetIgate` and `beaconSched.SetISSink` BEFORE calling
   `Start`, then seed `lastAppliedIgateFilter`. A `Start` failure rolls
   all of those back to nil so a subsequent toggle gets a fresh build
   instead of trying to re-Start the dead instance.
2. **enabled → disabled**: `Stop` the current iGate, clear `a.ig` /
   `a.igateOut` / beacon ISSink, reset `lastAppliedIgateFilter`.
3. **enabled → enabled**: re-read filters/rules and call `Reconfigure`
   on the running iGate (skipping the reconnect when the composed
   filter is unchanged).

Consequence: `a.ig` is `atomic.Pointer[igate.Igate]`. Code paths that
read it MUST go through `a.ig.Load()` and tolerate a nil result —
captured method values (`a.ig.Status`, `a.ig.SetSimulationMode`) are
forbidden because they freeze a stale instance across toggles. The
status / simulation REST routes at `/api/igate*` and the
`SetIgateStatusFn` callback are registered unconditionally with
closures that re-load `a.ig` on every call.

The disabled-state HTTP contract is **503 "igate not available"**, not
200 with a Connected=false snapshot. `GET /api/igate` and the
`/api/status` aggregate's `igate` field both omit the body when the
status callback returns nil, and the simulation toggle returns
`igate.ErrNotEnabled` which `setIgateSimulation` maps to 503. The Svelte
"Disabled" badge logic in `web/src/routes/Igate.svelte` keys off a
non-2xx response — a 200 with `connected:false` would render a red
"Disconnected" badge for an iGate the operator deliberately turned off.

Repeated enable cycles must not orphan Prometheus collectors. On a
second-and-later `igate.New`, `initMetrics` rebinds `ig.m*` to the
already-registered collector via `prometheus.AlreadyRegisteredError.ExistingCollector`
so `/metrics` keeps reflecting live counter increments instead of
freezing at the first instance's values.

*Why:* graywolf issue #84 — toggling the Enable iGate switch on the
iGate page used to require a daemon restart. The reload signal
silently no-op'd (channel was nil when boot-time config was disabled)
and the reload path only ever ran `Reconfigure`, never `Stop` or build.

Source: [`../../pkg/app/wiring.go`](../../pkg/app/wiring.go)
(`wireIGate`, `buildIgateInstance`, `igateComponent`, `reloadIgate`),
[`../../pkg/igate/output.go`](../../pkg/igate/output.go)
(`IgateOutput.SetIgate`),
[`../../pkg/app/igate_toggle_test.go`](../../pkg/app/igate_toggle_test.go).

### 29. AX.25 callsigns are uppercased on decode

*Why:* APRS callsigns are uppercase alphanumeric per spec, but
non-conformant transmitters occasionally ship lowercase shifted bytes
in the address field. The text parser `ax25.ParseAddress` already
uppercases, but the binary `decodeAddress` path used for every RF
frame did not, so lowercase callsigns leaked through `pkt.Source`
into the station cache and message store. Normalizing at the single
binary-decode chokepoint keeps every downstream consumer (router,
station cache, persistInbound, digipeater) working from canonical
uppercase. Object and Item names are NOT normalized — APRS101 §11
defines them as case-sensitive free-form names, not callsigns.

Source: [`../../pkg/ax25/address.go`](../../pkg/ax25/address.go)
(`decodeAddress`),
[`../../pkg/ax25/frame_test.go`](../../pkg/ax25/frame_test.go)
(`TestDecodeAddressUppercasesCallsign`).

### 30. Per-channel dashboard stats have two sources by backing

*Why:* The dashboard channel card RX/TX (`GET /api/status`,
`GET /api/channels/{id}/stats`) reads `modembridge` per-channel
counters, which are fed *only* by the Rust modem's `StatusUpdate`
IPC. KISS-TNC-backed channels have no Rust modem, so their card was
permanently stuck at zero even though the aggregate Prometheus
tiles incremented (issue #132). Per-channel counts for TNC-mode KISS
interfaces are therefore tracked separately in `kiss.Manager`: RX
via the wrapped `RxIngress` (per inbound frame, per interface); TX
via `txbackend.Dispatcher`'s `OnChannelTx` hook, called once per
dispatched frame on a KISS-backed channel, co-located with the
aggregate `ObserveTxFrame` so it stays in lockstep and does NOT
multiply by fan-out width when a channel has multiple KISS-TNC
interfaces attached. `webapi` prefers the bridge cache and falls
back to `kiss.Manager.ChannelStats` only when the bridge has no
entry. The TX-backend validator forbids a channel being both modem-
and KISS-backed, so the two sources never overlap and cannot
double-count (a modem-backed channel carries no KISS backend, so
`OnChannelTx` never fires for it). Bad-FCS is intentionally absent for KISS-TNC channels:
a hardware TNC validates the FCS and never forwards a bad frame over
KISS. Unlike the modem cache, the KISS counters are process-lifetime
monotonic and are NOT reset on a modem restart.

Source: [`../../pkg/kiss/channelstats.go`](../../pkg/kiss/channelstats.go),
[`../../pkg/kiss/manager.go`](../../pkg/kiss/manager.go)
(`wrapRxIngress` wiring in `Start`/`StartClient`),
[`../../pkg/app/txbackend/dispatcher.go`](../../pkg/app/txbackend/dispatcher.go)
(`OnChannelTx`),
[`../../pkg/webapi/status.go`](../../pkg/webapi/status.go),
[`../../pkg/webapi/channels.go`](../../pkg/webapi/channels.go)
(`getChannelStats`).

### 31. `aprs.Weather` holds raw APRS101 integers; unit conversion is the stationcache boundary's job

*Why:* The parser (`pkg/aprs/weather.go`) stores `Rain1Hour`,
`Rain24Hour`, `RainSinceMid` as raw hundredths-of-an-inch and
`Pressure` as raw tenths-of-millibar — a deliberate contract the FAP
conformance corpus enforces (`pkg/aprs/fap_corpus_test.go`, header
comment). Display-unit conversion happens exactly once, at
`convertWeather` in `pkg/stationcache/extract.go` (`/100` for rain,
`/10` for pressure). Snowfall is the lone exception: the parser
already divides it by 100, so `convertWeather` passes it through.
Adding a new WX field, or surfacing `RainSinceMid`, means converting
at that boundary — never assume the parser did it, and never add a
second `/100` downstream. The converted cache value flows unchanged
into `historydb` and the `webapi` WeatherDTO, and `historydb` is read
back into `stationcache.Weather` *without* re-running `convertWeather`
when the cache is hydrated on restart (`pkg/stationcache/persistent.go`)
— so persisted rows must already be in display units. Issue #126:
rain shipped 100x too large because this conversion was missing;
because legacy rows persisted the raw value, `bootstrap` carries a
one-time `PRAGMA user_version`-gated backfill
(`UPDATE weather SET rain_1h = rain_1h/100.0, rain_24h = rain_24h/100.0`,
`user_version` 0 → 1). `user_version` is the historydb data-migration
counter; bump it and add a gated block for any future persisted-units
correction.

Source: [`../../pkg/aprs/weather.go`](../../pkg/aprs/weather.go),
[`../../pkg/aprs/types.go`](../../pkg/aprs/types.go) (`Weather` field
docs), [`../../pkg/aprs/fap_corpus_test.go`](../../pkg/aprs/fap_corpus_test.go),
[`../../pkg/stationcache/extract.go`](../../pkg/stationcache/extract.go)
(`convertWeather`),
[`../../pkg/historydb/historydb.go`](../../pkg/historydb/historydb.go)
(`bootstrap` `user_version` backfill).
### 32. Modem sample rate is capped at 48 kHz

The modem never advertises, defaults to, or opens an audio stream
above **48 kHz** (`audio::MODEM_MAX_SAMPLE_RATE`). Every Graywolf
modem mode (AFSK 1200, G3RUH 9600, QPSK/8-PSK) is well served by
48 kHz.

*Why:* An ALSA `plughw:`/`default` PCM advertises a *synthetic*
resample range (up to 192 kHz) via cpal `supported_*_configs()`
even though the USB codec hardware runs at 48 kHz. The Audio
Devices form used to auto-fill the **highest** advertised rate, so
operators who ran "Detect Devices" persisted `sample_rate=96000`.
At runtime the modem opened the stream at the inflated rate while
the hardware ran 48 kHz; the demodulator clocked AFSK bit timing
against the wrong rate and **every frame failed FCS -- RX went
silent with no error** (anguilla.local, 2026-05-16). Defense in
depth, all three layers required:

1. **Enumeration** never lists >48 kHz: `STANDARD_SAMPLE_RATES`
   stops at 48000, asserted by `audio::rate_invariants`.
2. **UI** never defaults to a corrupt/max rate:
   `pickDefaultSampleRate` prefers 48000 → 44100 → highest ≤48 kHz,
   never above; the manual rate `<Select>` offers no 96000.
3. **Runtime backstop**: `soundcard::choose_stream_rate` honors the
   requested rate only when ≤48 kHz *and* covered by a real
   supported range, else falls back to the device native rate
   clamped to the ceiling. `AudioSource.sample_rate` reports the
   rate **actually opened**, so the demod can never be silently
   desynced by a bad config again.

Migration 21 (`audio_devices_clamp_sample_rate`) repairs
already-corrupted field databases (`sample_rate > 48000 → 48000`).

Source:
[`../../graywolf-modem/src/audio/mod.rs`](../../graywolf-modem/src/audio/mod.rs)
(`MODEM_MAX_SAMPLE_RATE`, `STANDARD_SAMPLE_RATES`),
[`../../graywolf-modem/src/audio/soundcard.rs`](../../graywolf-modem/src/audio/soundcard.rs)
(`choose_stream_rate`, `spawn`),
[`../../web/src/lib/sampleRate.js`](../../web/src/lib/sampleRate.js),
[`../../pkg/configstore/migrate_audio_devices_clamp_sample_rate.go`](../../pkg/configstore/migrate_audio_devices_clamp_sample_rate.go).

### 33. Capture stream format is device-advertised, never `default_input_config()`

`soundcard::spawn` picks the input `SampleFormat` from the device's
*advertised* supported configs at the chosen rate, preferring native
`I16` (`pick_input_sample_format`). It must **never** open a capture
stream using `device.default_input_config().sample_format()`.

*Why:* On an ALSA `plughw:`/`default` PCM, cpal's
`default_input_config()` returns **`F32`**. Opening an `F32` capture
stream on a full-speed USB radio codec (AIOC and similar) makes cpal
`alsa::poll()` return `POLLERR` on essentially every period -- the
holding thread rebuilds, POLLERRs again, and loops forever (observed:
24,344 errors in one session, RX stuck at zero with no fatal error).
The *same hardware* streams the native `I16` config cleanly (`arecord
-f S16_LE` and the detection probe both work). The detection probe
(`pick_input_probe_config`) already selected I16; the runtime
`spawn()` did not, so the probe verified a config the runtime never
used. `pick_input_sample_format` and the probe now share
`input_format_rank` so detection and runtime cannot drift. The
sample-*rate* clamp (invariant 32) is necessary but independent: a
clipping analog input or an `F32` plughw stream each kill RX on their
own.

Source:
[`../../graywolf-modem/src/audio/soundcard.rs`](../../graywolf-modem/src/audio/soundcard.rs)
(`pick_input_sample_format`, `input_format_rank`, `pick_input_probe_config`, `spawn`).

### 34. KISS InterfaceType dispatch must be updated in two independent places

Adding or changing a KISS InterfaceType (e.g. `KissTypeSerial`) requires updating the `switch ki.InterfaceType` in **both**:

1. `pkg/app/wiring.go` -- `kissComponent().start` (startup dispatch)
2. `pkg/webapi/kiss.go` -- `notifyKissManager` (hot-reload / config-write dispatch)

The two switches are independent; omitting #2 means a config write calls `Stop()` on a running interface and silently leaves it stopped with no error.

*Why:* There is no shared dispatch table -- each switch is a separate match on the stored `InterfaceType` string, so a new type added to one switch must be consciously added to the other.

Source: [`../../pkg/app/wiring.go`](../../pkg/app/wiring.go) (`kissComponent`),
[`../../pkg/webapi/kiss.go`](../../pkg/webapi/kiss.go) (`notifyKissManager`).
