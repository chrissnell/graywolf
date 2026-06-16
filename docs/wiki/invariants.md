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

### 9c. Android PTT transport is a first-class `ptt_method`; `gpio_pin` is CM108-only

*Why:* The Android per-channel PTT transport (PttMethod enum, spec Appendix B: 1 = CP2102N RTS / Digirig, 2 = CM108 HID, 3 = AIOC CDC-ACM DTR, 4 = VOX) travels in its own field the whole way: SPA `POST /api/ptt {method:"android", ptt_method:N}` → `PttConfig.PttMethod` → `session.go` → `ConfigurePtt.ptt_method` (a `uint32`, deliberately not the `platform.proto` enum, to keep `graywolf.proto` self-contained — invariant #2) → Rust `build_driver` (`PttMethod::Android` does `let method = cfg.ptt_method as i32`) → `AndroidPtt` → JNI → Kotlin `UsbPttAdapter.pttSet`. `method=="android"` is only the coarse subsystem discriminator. **`gpio_pin` is the CM108 HID pin and nothing else** — it must never carry the Android transport (an earlier build did, via a `method=="android"` sentinel, which silently downgraded saved AIOC configs to CP2102N on re-save when a response DTO dropped the field; removed by migration v22 `ptt_android_method_field`).

Source: [`../../pkg/webapi/dto/channel.go`](../../pkg/webapi/dto/channel.go) (`ChannelPtt.PttMethod`, `ChannelPttFromModel`);
[`../../pkg/configstore/migrate.go`](../../pkg/configstore/migrate.go) (`migratePttAndroidMethodField`, v22);
[`../../pkg/modembridge/session.go`](../../pkg/modembridge/session.go) (`ConfigurePtt` construction, `PttMethod` field);
[`../../graywolf-modem/src/tx/ptt.rs`](../../graywolf-modem/src/tx/ptt.rs) (`PttMethod::Android` arm);
[`../../android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt) (`pttSet`, `setAiocRts`).

*UI contract:* PTT configuration is operated only from the **PTT tab**
(`Ptt.svelte`) on both Android and desktop. The Channels page modal is
channel-only; an earlier PR-#157-era Android branch in
`ChannelEditModal.svelte` (`AndroidPttFields`) was removed in favor of
the unified tab. The Channels page's read-only PTT indicator row stays
— that's a glance-surface, not a config surface.

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

### 33. Stream format is device-advertised, never `default_{input,output}_config()` (both RX and TX)

`soundcard::spawn` (capture) and `soundcard::spawn_output` (playback)
both pick the `SampleFormat` from the device's *advertised* supported
configs at the chosen rate, preferring native `I16`
(`pick_input_sample_format` / `pick_output_sample_format`, both ranked
by the shared `native_format_rank`). Neither path may open a stream
using `device.default_input_config().sample_format()` or
`device.default_output_config().sample_format()`; the cpal default is
only a last-resort fallback when the device advertises nothing usable
at the rate.

*Why:* On an ALSA `plughw:`/`default` PCM, cpal's
`default_input_config()` **and** `default_output_config()` return
**`F32`**. Opening an `F32` stream on a full-speed USB radio codec
(AIOC, Signalink, Digirig) makes cpal `alsa::poll()` return `POLLERR`
on essentially every period -- the holding thread rebuilds, POLLERRs
again, and loops forever, flooding the log. The *same hardware*
streams the native `I16` config cleanly (`arecord -f S16_LE` /
`aplay`).

This bit RX first: capture was observed looping 24,344 errors in one
session with RX stuck at zero and no fatal error. The capture fix
(`pick_input_sample_format`, commit `f917b8ff`) left `spawn_output`
still calling `default_output_config().sample_format()`, so the
identical failure resurfaced on **TX only** (issue #227: TX floods
`cpal output stream error: ... alsa::poll() returned POLLERR` while RX
is fine). `spawn_output` now selects the format the same way.

The detection probe (`pick_input_probe_config`) and both runtime
selectors share `native_format_rank`, so detection and runtime cannot
drift. The sample-*rate* clamp (invariant 32) is necessary but
independent: a clipping analog input or an `F32` plughw stream each
kill audio on their own.

Source:
[`../../graywolf-modem/src/audio/soundcard.rs`](../../graywolf-modem/src/audio/soundcard.rs)
(`pick_input_sample_format`, `pick_output_sample_format`, `native_format_rank`, `pick_input_probe_config`, `spawn`, `spawn_output`).

### 34. KISS InterfaceType dispatch must be updated in two independent places

Adding or changing a KISS InterfaceType (e.g. `KissTypeSerial`) requires updating the `switch ki.InterfaceType` in **both**:

1. `pkg/app/wiring.go` -- `kissComponent().start` (startup dispatch)
2. `pkg/webapi/kiss.go` -- `notifyKissManager` (hot-reload / config-write dispatch)

The two switches are independent; omitting #2 means a config write calls `Stop()` on a running interface and silently leaves it stopped with no error.

*Why:* There is no shared dispatch table -- each switch is a separate match on the stored `InterfaceType` string, so a new type added to one switch must be consciously added to the other.

Source: [`../../pkg/app/wiring.go`](../../pkg/app/wiring.go) (`kissComponent`),
[`../../pkg/webapi/kiss.go`](../../pkg/webapi/kiss.go) (`notifyKissManager`).

### 35. All blocking Bluetooth and USB calls run on a worker thread

`BluetoothSocket.connect`, `BluetoothAdapter.bondedDevices`,
`BluetoothSocket.inputStream.read`, `UsbDeviceConnection.controlTransfer`,
`UsbDeviceConnection.bulkTransfer`, `UsbManager.openDevice`,
`UsbDeviceConnection.claimInterface`, and HID Set_Report calls are blocking
JNI/native invocations. Main-thread invocation can block UI for several
seconds the first time the corresponding stack is touched, leading to ANR
("Application Not Responding") dialogs on Android.

*Why:* feedback memory `feedback_android_usb_open_worker_thread` -- phase-4b
USB enumeration on the main thread caused a 5-second ANR. The lesson is
general to any blocking native call.

*How to apply:* Kotlin code touching `BluetoothAdapter`, `BluetoothSocket`,
`UsbDeviceConnection`, `UsbSerialAdapter`, `UsbSerialFacade`, or `HidDevice`
MUST dispatch onto an IO/worker dispatcher (`Dispatchers.IO`, a dedicated
`SingleThreadExecutor`, or a worker `Thread`). Calls arriving FROM the
`@JavascriptInterface` binder thread that need a main-thread API surface
(`requestPermissions` etc.) may `mainHandler.post { ... }` only that
AndroidManifest-API call; the actual blocking work still belongs on a
worker thread.

Source: [`../../android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt),
[`../../android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/BluetoothFacade.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/BluetoothFacade.kt),
[`../../android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/BtSerialAdapter.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/BtSerialAdapter.kt),
[`../../android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/UsbSerialAdapter.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/UsbSerialAdapter.kt),
[`../../android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/UsbSerialFacade.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/UsbSerialFacade.kt).

### 36. The Android Go child dies with the app (two-layer shutdown)

*Why:* Swiping the app from recents removes the Activity but, with
`android:stopWithTask` at its default (`false`), the foreground service keeps
running -- so the forked Go backend would keep iGating/beaconing and the next
launch shows stale uptime. A hard kill (Force Stop / OOM / OEM killer) can
orphan the Go child entirely. Two independent layers guarantee the child
cannot outlive the app, because no single mechanism covers both cases.

*How to apply:*
- **Layer 1 (clean swipe):** `GraywolfService.onTaskRemoved` calls `stopSelf()`,
  which runs the existing `onDestroy()` teardown (`supervisor.stop()`,
  `goLauncher.stop()` -> SIGTERM, `ModemBridge.modemStop()`, audio pumps,
  `UsbPttAdapter.closeAll()`, `platformServer.stop()`). Keep `stopWithTask`
  **unset** (default false) so `onTaskRemoved` is delivered; do NOT set
  `android:stopWithTask="true"` -- the implicit stop is historically flaky
  across OEMs and skips the deterministic log line.
  - **USB auto-relaunch suppression:** `MainActivity` declares a
    `USB_DEVICE_ATTACHED` intent-filter so plugging in a radio launches the app.
    Tearing down on swipe releases the radio's USB interfaces, which physically
    re-enumerate (~2s later, new bus device numbers) -- indistinguishable from a
    fresh plug-in, so Android would auto-relaunch and revive the station the
    operator just dismissed. To prevent that, `onTaskRemoved` records a
    deliberate-stop timestamp (`MainActivity.markUserStopped`, in `graywolf-prefs`)
    and `MainActivity.onCreate` `finish()`es immediately if the launch action is
    `USB_DEVICE_ATTACHED` within `STOP_RELAUNCH_SUPPRESS_WINDOW_MS` (15s) of that
    stop. A launcher tap (action `MAIN`) or a genuine re-plug after the window is
    NOT suppressed; the marker is cleared in `startEverything()`. The window
    (not a one-shot) is required because re-enumeration fires one attach per
    interface (e.g. CP2102N serial + C-Media audio).
- **Layer 2 (hard-kill safety net):** `watchParentDeath` in
  `cmd/graywolf/parentwatch.go` cancels the app context when the parent dies,
  via two triggers -- stdin EOF (the JVM holds the write end of the child's
  stdin pipe; the kernel closes it on app death) and a reparent poll
  (`os.Getppid()` changes when the child reparents to init). A deadline
  `os.Exit` backstops a hung unwind. This is separate from the stdout
  readiness channel ([invariant 13](invariants.md)) -- stdin carries death
  detection, stdout carries readiness; different fds, no conflict. The modem
  cdylib is in-process JNI, so it always dies with the app -- no separate
  handling needed.
- **Single backend, serialized startup:** the platformsvc socket
  (`<cacheDir>/platform.sock`, Linux abstract namespace) is effectively the
  station's single-instance lock. A new instance must not start its backend
  while a previous one is still tearing down. `MainActivity.waitForPredecessorThenStart`
  probes the socket on a background thread (a live predecessor still accepts a
  `LocalSocket` connect) and shows a "waiting" page until it is free, then starts
  the foreground service. `PlatformServer.start()` additionally retries the bind
  for `BIND_WAIT_MS` and, on timeout, throws `BindContendedException` so
  `GraywolfService.onCreate` `stopSelf()`s cleanly -- it never lets
  `Address already in use` crash the process, because that crash relaunches and
  re-collides, a loop that re-enumerates the USB bus fast enough to wedge devices
  off a powered hub.
- **No blocking HAL calls on the main thread in `onCreate`:** `AudioTxPump.start()`
  (AudioTrack build + `setPreferredDevice`) and `UsbPttAdapter.enumerate()` (USB
  device opens) are synchronous HAL/binder calls that block for seconds when a USB
  audio dongle is wedged, ANR'ing the process within ~5s. `GraywolfService.onCreate`
  runs them on a `graywolf-io-init` worker thread (the modem TX/PTT callbacks are
  installed first and tolerate the brief gap); `onDestroy` joins that worker
  (bounded) before tearing the same resources down. The cheap `UsbPttAdapter.init()`
  stays on the main thread so `MainActivity.onResume`'s `enumerate()` never races an
  uninitialized adapter.

Source: [`../../android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt),
[`../../cmd/graywolf/parentwatch.go`](../../cmd/graywolf/parentwatch.go),
[`../../cmd/graywolf/main_android.go`](../../cmd/graywolf/main_android.go).

### 37. The Android modem never goes permanently deaf (IPC re-accept + no-halt supervisor)

*Why:* Two independent failure modes used to leave the modem alive but RX
permanently dead -- no levels, no decode -- while audio capture stayed healthy:
(1) the Rust `run_demod` outbound loop did a bare `break` on the first IPC send
error, exiting the serve loop and silently dropping every frame + level while the
DSP thread kept draining audio; (2) the Kotlin `Supervisor` permanently halted its
restart loop after 3 failures in 60s (or any `onRestart` returning false), so a
transient burst left the station deaf with no path to recovery.

*How to apply:*
- **Modem IPC re-accepts, it does not exit.** `run_demod` wraps the per-connection
  serve loop in an outer `'serve` loop that calls
  `IpcServer::accept_interruptible(&stop, poll)`. On any send error (frame, level,
  or status) the inner loop breaks with `link_alive = true`, tears down just that
  connection (`drop(handle)` + join the reader), and loops back to await the Go
  child's reconnect. The DSP thread keeps decoding throughout. Only a `stop`
  request or a disconnected audio channel exits `run_demod` (the single exit path,
  which now also `stop.store(true)` before `dsp_join.join()` to fix a latent
  DSP-thread hang).
- **`ready` stays true across reconnects.** It is set true once at IPC bind and set
  false only on the real exit path -- never on a mere client reconnect -- so the
  Kotlin `modemWatcher` (which polls `modemAwaitReady`) does NOT trigger a full
  modem restart just because the Go child reconnected.
- **`accept_interruptible` is stop-aware.** It polls the listener non-blocking so a
  `modemStop` is honored while waiting for reconnect; it returns `Ok(None)` when
  `stop` is set before any client connects. The blocking `accept` and the
  interruptible variant share `finish_accept` (sends `ModemReady`, spawns the
  reader thread).
- **Supervisor degrades, never halts.** Restart-decision logic lives in the pure,
  host-testable `RestartPolicy` (sliding 60s window). Inside the window it escalates
  through a backoff curve; once the limit is exceeded it enters DEGRADED mode and
  keeps retrying at a long capped delay instead of returning from the restart
  thread. An `onRestart` that returns false re-signals for another attempt rather
  than halting. `GraywolfService` surfaces degraded state as an Android notification
  ("graywolf modem stopped ... auto-retrying") via `onDegraded`, cleared on
  `onHealthy`.
- **Diagnosability:** when the DSP decodes but no IPC client is draining, dropped
  frames are counted and a `warn!` fires every 50 drops -- a deaf-but-decoding modem
  is visible in logcat instead of silent.

Source: [`../../graywolf-modem/src/android/mod.rs`](../../graywolf-modem/src/android/mod.rs),
[`../../graywolf-modem/src/ipc/server.rs`](../../graywolf-modem/src/ipc/server.rs),
[`../../android/app/src/main/kotlin/com/nw5w/graywolf/binaries/RestartPolicy.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/binaries/RestartPolicy.kt),
[`../../android/app/src/main/kotlin/com/nw5w/graywolf/binaries/Supervisor.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/binaries/Supervisor.kt).

### 38. A USB device claimed for KISS serial cannot simultaneously be a PTT device (Android)

A CP210x or CH34x USB cable can act as either a PTT keying device
(via `UsbPttAdapter`) or a serial KISS TNC (via `UsbSerialAdapter`), but
not both at once.

*Why:* Both paths open the same `UsbDeviceConnection`. Concurrent ownership
would cause control-transfer collisions and undefined PTT state.

*How it is enforced:*
1. When `UsbSerialFacade.open(device)` is called to attach a KISS interface,
   it calls `UsbDeviceArbiter.claim(device)`, which calls
   `UsbPttAdapter.evictDevice(device)` to close and release any existing PTT
   connection on that device before the serial port is opened.
2. `UsbPttAdapter.tryOpen(device)` consults `UsbDeviceArbiter.isClaimed(device)`
   and refuses to open a device that is currently held by the KISS path.
3. When the KISS interface is stopped or reconfigured, `UsbSerialFacade`
   calls `UsbDeviceArbiter.release(device)` so the device becomes available
   for PTT again.

CDC-ACM devices (TH-D75, AIOC, Mobilinkd) are not affected: KISS takes full
ownership of the device and PTT is handled by the TNC hardware or inside the
KISS stream itself -- there is no separate PTT claim on a CDC-ACM device
that could conflict.

Source: [`../../android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbDeviceArbiter.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbDeviceArbiter.kt),
[`../../android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/UsbSerialFacade.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/UsbSerialFacade.kt),
[`../../android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt`](../../android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt).

### 39. The CW test signal never keys the radio with an empty or N0CALL callsign

`POST /api/channels/{id}/test-tx` with `signal=cw` resolves the station
callsign via `Store.ResolveStationCallsign` and returns 422 before any IPC if
it is empty or N0CALL. The tone signals (`tone1200`, `tone2400`, `alt`) do not
use the callsign.

*Why:* A CW ID that transmits nothing (or the literal string "N0CALL") gives
no useful identification and violates the intent of the feature.

Source: [`../../pkg/webapi/channels.go`](../../pkg/webapi/channels.go)
(`sendTestSignal`).

### 40. Digipeater block list is digipeater-only

[`pkg/digipeater/blocklist`](../../pkg/digipeater/blocklist) is consulted
only by [`pkg/digipeater/digipeater.go`](../../pkg/digipeater/digipeater.go)'s
`Handle`. Frames whose source matches an enabled entry are not digipeated;
every other RX consumer (iGate, packet log, dashboard, station cache,
messages router, AGW/KISS fanouts) is unaffected and still sees the frame.

*Why:* operators commonly want to silence a misbehaving station's
digipeated copies without losing the ability to see, log, or gate that
station's original RF or APRS-IS appearances. A shared "blocked
stations" surface across subsystems would conflate two different
operator intents.

*How to apply:* never import `pkg/digipeater/blocklist` outside
`pkg/digipeater/`. If another subsystem grows a similar need, give it
its own list. The regression test in
[`pkg/app/wiring_blocklist_isolation_test.go`](../../pkg/app/wiring_blocklist_isolation_test.go)
locks the invariant in behaviorally.

### 41. The live map measures packet age against the host clock, not the browser's

Packet receive times (`last_heard`) and every other server timestamp are
stamped by the **graywolf host** clock. The browser must therefore compute
packet age relative to that same clock, never its own `Date.now()` —
otherwise a host whose clock is unsynced (a Pi with no RTC, a browser that
has drifted off NTP) makes ages go negative or silently hides every station
from the map (GH #234).

[`web/src/lib/map/clock-offset.svelte.js`](../../web/src/lib/map/clock-offset.svelte.js)
reads the standard HTTP `Date:` header off each `/api/stations` response
(`clockOffset.observe`), derives `offsetMs = serverNow - browserNow`, and
exposes `serverNow() ≈ Date.now() + offsetMs`. The two age-math sites use it:
the prune cutoff in
[`data-store.svelte.js`](../../web/src/lib/map/data-store.svelte.js)
(`pruneStale`) and `timeAgo` in
[`popup-helpers.js`](../../web/src/lib/map/popup-helpers.js).

*Why:* the host stamps the timestamps, so the host clock is the only shared
reference; correcting the browser to it (rather than the reverse) keeps every
connected browser consistent even when their clocks disagree with each
other. No new endpoint or protocol — the `Date` header is already on the wire.

*How to apply:* never reintroduce `Date.now()` for map packet-age math; route
it through `clockOffset.serverNow()`. The opposite case — timing a
*browser-local* event, e.g. "last fetch N ago" — must pass `Date.now()`
explicitly as `timeAgo`'s second arg to opt out of the host correction.

### 42. Hop count excludes generic path aliases

The displayed "hop" count for a station/packet is the number of *real*
digipeaters that retransmitted it, not the number of path elements with
the H-bit set (`*`). Generic routing aliases — `WIDE`, `RELAY`, `TRACE`,
the APRS-IS `qA*` constructs, and the IS-injection markers `TCPIP` /
`TCPXX` — are excluded even when flagged used,
because a used alias rides alongside the digipeater that consumed it
rather than being a hop of its own. Example: `SHEPRD*,WIDE1*,ELY*,WIDE2*`
is **2** hops (SHEPRD, ELY), not 4.

*Why:* counting raw `*` elements over-reported hops on the map station
view (GitHub issue #222) — WIDEn-N aliases left in the path by callsign-
inserting digipeaters were each tallied as a separate repeat.

*How to apply:* route any hop-counting or last-digipeater logic through
[`aprs.CountHops`](../../pkg/aprs/path.go) /
[`aprs.IsGenericPathAlias`](../../pkg/aprs/path.go) — the single source
of truth for which path entries are real hops. The map's `hops` field is
computed once in [`pkg/stationcache/extract.go`](../../pkg/stationcache/extract.go)
and consumed verbatim by the frontend popup; do not recompute it from
`path.length` client-side.

### 43. 64-bit atomic counters must be `atomic.Uint64`, not bare `uint64`

Any struct field touched by a 64-bit atomic operation must use the
`atomic.Uint64` / `atomic.Int64` types, never a plain `uint64` accessed via
`atomic.AddUint64`/`atomic.LoadUint64`. On 32-bit platforms (notably ARMv6 --
the Raspberry Pi Zero), a 64-bit atomic op requires the address to be 8-byte
aligned; a bare `uint64` sitting mid-struct on a 4-byte boundary is not
guaranteed to be, and the op panics with `unaligned 64-bit atomic operation`.
The `atomic.Uint64` wrapper carries an alignment guarantee, so the compiler
places it correctly regardless of field order. The iGate's four packet
counters (`statGated`, `statDownlinked`, `statFiltered`, `statDropped` in
[`pkg/igate/igate.go`](../../pkg/igate/igate.go)) are the canonical example.

*Why:* graywolf issue #262 -- on a Pi Zero the iGate connected to APRS-IS fine
but the web UI showed "Disabled" because `handleISLine`'s first
`atomic.AddUint64` panicked on the misaligned `statFiltered`, crash-looping the
process roughly every 90 seconds so the UI never observed a healthy session.
64-bit hosts are unaffected, which is why it slipped through.

*How to apply:* declare any atomically-accessed 64-bit counter as
`atomic.Uint64`/`atomic.Int64` and use the method API (`.Add(1)`, `.Load()`,
`.Store()`). Do not reintroduce bare `uint64` + `atomic.AddUint64`, and do not
rely on field ordering for alignment.

### 44. Every Go→Rust IPC payload must be dispatched in BOTH modem loops

The desktop/server modem and the Android modem read the same
`graywolf.proto` IPC stream through **two independent dispatch matches**,
and a Go→Rust payload only works on a platform whose match has an arm for
it:

1. `graywolf-modem/src/modem/mod.rs` — `handle_message` (desktop, server, the cdylib's non-Android path)
2. `graywolf-modem/src/android/mod.rs` — the inbound `while let` in `run_demod` (Android)

Both end in a catch-all (`_ => {}` / the grouped Rust→Go ignore arm), so a
new payload added to one loop but not the other is **silently dropped** on
the platform that missed it — no error, no log, just a Go-side request that
never gets its reply and times out.

*Why:* this has bitten twice. 5d6b75ad wired `ConfigurePtt` / `ManualPtt` /
`TransmitFrame` into `run_demod` after they were found dropped on Android
(PTT stayed silent); then `TransmitTestSignal` (the Channel TX test-signal
feature, #193) shipped to the desktop loop only and fell into the Android
catch-all, so **Test TX** returned `modembridge: test signal timeout` on
Android (#267). Same failure mode as invariant #34 (KISS dispatch in two
places), but across the Go↔Rust boundary.

*How to apply:* when you add a Go→Rust `Payload` variant, add a matching
arm to **both** loops in the same change — and on Android remember to send
the corresponding Rust→Go reply (e.g. `TestSignalResult`) so the Go side's
bounded wait resolves. Android TX jobs submit samples unscaled (gain is
applied by `AndroidTxSink`), unlike the desktop arm which pre-scales by the
output device gain.

### 45. Disabling a KISS interface releases its device; the DTO flag is a pointer

A KISS interface row carries `Enabled` (`KissInterface.Enabled`,
gorm `default:true`). When `Enabled=false` the manager does **not** keep
the device open looping reconnect attempts — it stops the supervisor,
which cancels the serve context and closes the underlying
`io.ReadWriteCloser` (the serial fd / TCP socket). This is what lets an
operator release e.g. a Bluetooth `/dev/rfcomm0` tty before a battery
swap without deleting the interface (graywolf #152). Three call sites
already honor the flag and must stay in sync — they are the same
two-switch dispatch as invariant #34 plus the TX snapshot:

1. `pkg/app/wiring.go` — `kissComponent().start` skips `!Enabled` rows at boot.
2. `pkg/webapi/kiss.go` — `notifyKissManager` calls `kissManager.Stop(id)` (release) on `!Enabled`, and (re)starts on `Enabled`. The focused `PUT /api/kiss/{id}/enabled` toggle (`setKissEnabled`) routes through here.
3. `pkg/app/wiring.go` — `buildTxBackendSnapshot` excludes `!Enabled` rows so a disabled TNC registers no governor TX backend.

*notifyKissManager must enumerate the same interface types as the boot
switch* (this is invariant #34's two-switch hazard in practice). The
re-enable path goes through `notifyKissManager`, so its `switch` needs a
case for **every** type `kissComponent` can start — `tcp`, `tcp-client`,
`serial`, `bluetooth`, `usbserial`. A type that falls into `default:`
hits `Stop(id)` and silently never restarts on re-enable (this bit
Bluetooth/USB: they were missing and re-enable was a no-op). The
serial-family cases (`serial`/`bluetooth`/`usbserial`) must also pass
`OpenFunc: s.kissSerialOpenFunc` — on Android that opener routes MAC /
vid:pid device strings through `platformsvc`; without it the supervisor
cannot open the device. The webapi `Server` receives it via
`Config.KissSerialOpenFunc` (wired from `App.kissSerialOpenFunc()`; nil
on desktop).

*Why a pointer:* `dto.KissRequest.Enabled` is `*bool`, not `bool`. KISS
`POST`/`PUT` is full-resource replace (invariant at line ~185), so a
plain `bool` would conflate "field omitted" with "disable" and an older
client editing any field would silently stop the interface. `nil` means
"omitted → default true"; `ToModel` substitutes the explicit value. The
frontend always sends `enabled` on save so a `PUT` never re-enables a row
the operator disabled.

*Create vs. the gorm default:* the `default:true` tag is kept (so the SQL
DDL default survives for raw inserts / downgrade-safety — `migrate_kiss_*`
tests rely on it). But gorm treats a Go zero-value `bool` as "unset" and
sends the column default on `INSERT`, so a `POST` with `enabled=false`
would otherwise persist `true` (the footgun the `actions` table dodged by
dropping its tag default). `Store.CreateKissInterface` therefore captures
the requested value *before* `db.Create` (gorm writes the applied default
back into the struct) and re-asserts `false` with an explicit `Update`.
`UpdateKissInterface` (`db.Save`) writes `false` correctly on its own, so
only the create path needs the fix-up.

Source: [`../../pkg/webapi/dto/kiss.go`](../../pkg/webapi/dto/kiss.go)
(`KissRequest.ToModel`, `KissEnabledRequest`),
[`../../pkg/webapi/kiss.go`](../../pkg/webapi/kiss.go) (`setKissEnabled`, `notifyKissManager`),
[`../../pkg/configstore/store.go`](../../pkg/configstore/store.go) (`CreateKissInterface`),
[`../../pkg/app/wiring.go`](../../pkg/app/wiring.go) (`kissComponent`, `buildTxBackendSnapshot`, webapi `Config.KissSerialOpenFunc`).

### 46. Per-frame metadata must be populated by every demod profile, not just Profile A

The default AFSK RX path is the `RECOMMENDED_3DEMOD` ensemble (Profile A,
Profile A + hard-limiter, Profile B / FM-discriminator). The ensemble dedups
identical frames across sub-demods and keeps the *first* emitter within a
~110-sample window; in practice **Profile B usually wins** because its
pipeline detects the closing flag a few samples earlier. So any per-frame
field stamped onto `DecodedFrame` (e.g. `audio_level_mark`/`space`) must be
produced by *all three* profiles -- a field tracked only in
`process_profile_a` will be the `-1.0`/zero init on the frames that actually
reach the application, even though Profile A "also" decoded the packet.

This bit the per-packet audio level (GRA-84): Profile B never called
`track_level`, so nearly every logged packet showed no level (a dash) until
Profile B was taught to track its center-frequency envelope. When adding a
new per-frame measurement, populate it in `process_profile_a` **and**
`process_profile_b`, or compute it profile-independently.

Source: [`../../graywolf-modem/src/demod_afsk.rs`](../../graywolf-modem/src/demod_afsk.rs)
(`process_profile_a`, `process_profile_b`, `track_level`),
[`../../graywolf-modem/src/demod_afsk_multi.rs`](../../graywolf-modem/src/demod_afsk_multi.rs)
(dedup in `process_sample`).

### 47. The GPS serial reader must deassert RTS/DTR on open

`gps.RunSerial` opens its NMEA port with
`InitialStatusBits: &serial.ModemOutputBits{RTS: false, DTR: false}`. This is
not optional cosmetics: opening a tty otherwise leaves RTS and DTR raised
(`go.bug.st/serial` only touches the modem bits when `InitialStatusBits` is
non-nil, and the kernel asserts both on open by default). On shared serial
rigs where RTS drives PTT -- the Digirig Mobile RS232 is the canonical case --
raised RTS keys the transmitter the instant GPS connects, producing a
persistent PTT that can't be toggled off (issue #305). GPS only consumes RX,
so it holds the control lines low and leaves RTS for the PTT driver to assert
when it keys. macOS does not exhibit this because its tty open does not raise
the lines the same way, which is why the bug was Linux-only.

Source: [`../../pkg/gps/serial.go`](../../pkg/gps/serial.go) (`RunSerial`).
