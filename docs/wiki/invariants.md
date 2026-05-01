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

### 9. PTT enumeration vs. driving split

*Why:* Go enumerates PTT hardware and Rust drives it, so both sides must agree on the identifier scheme passed via `ConfigurePtt.method` and `ConfigurePtt.device`.

Source: [`../../pkg/pttdevice/`](../../pkg/pttdevice/);
[`../../graywolf-modem/src/tx/`](../../graywolf-modem/src/tx/);
[`../../proto/graywolf.proto`](../../proto/graywolf.proto)
(`ConfigurePtt`).

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

When a channel's `Mode` is `packet`, the beacon scheduler, digipeater engine, iGate IS→RF gate, and APRS messages sender all skip, refuse, or down-shift when asked to transmit on that channel. RX is unchanged: frames keep demodulating and fan out via the existing fanout; subscribers self-filter.

The lookup contract is fail-open at the resolver: if `ChannelModeLookup` returns an error or `nil`, callers behave as if the channel were `aprs` (preserves pre-Phase-0 behavior). The IS→RF runtime gate also fails open -- a transient DB error does not silently suppress beaconing or gating.

*Why:* Operators may want to dedicate a channel to AX.25 connected-mode (Phase 1+) without it accidentally absorbing APRS beacons, digipeated packets, IS→RF traffic, or outbound APRS messages. The `aprs+packet` value preserves the legacy "any channel does anything" behavior for setups that don't care about the split.

Source: [`../../pkg/configstore/channel_mode_lookup.go`](../../pkg/configstore/channel_mode_lookup.go),
[`../../pkg/configstore/models.go`](../../pkg/configstore/models.go)
(`Channel.Mode` field comments).
