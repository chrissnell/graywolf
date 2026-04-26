# Invariants

Cross-cutting "if you change X, also touch Y" rules. Each entry: rule,
one-line *why*, source.

### 1. Root `Cargo.toml` is a workspace shim, sole member `graywolf-modem`

*Why:* Lets cross-rs's Docker mount see `proto/` and `VERSION` from the
repo root. Side effect: cargo output lives at `/target/` (not
`graywolf-modem/target/`); Makefile, `pkg/app/modem.go` modem-path
fallbacks, modembridge integration tests, and release CI hard-code this.

Source: [`../../Cargo.toml`](../../Cargo.toml) (comment is authoritative),
[`../../Makefile`](../../Makefile),
[`../../graywolf/pkg/app/modem.go`](../../graywolf/pkg/app/modem.go).

### 2. `proto/graywolf.proto` is the single Go<->Rust IPC contract

*Why:* Both sides regenerate from this file; the wire format is
`[4 BE bytes length][IpcMessage]`. Any change requires regenerating both.

Source: [`../../proto/graywolf.proto`](../../proto/graywolf.proto). Go:
`make proto` -> `graywolf/pkg/ipcproto/graywolf.pb.go`. Rust:
[`../../graywolf-modem/build.rs`](../../graywolf-modem/build.rs) ->
`OUT_DIR/graywolf.rs`.

### 3. Version locks

*Why:* `make bump-*` rewrites `VERSION`, `graywolf-modem/Cargo.toml`,
`Cargo.lock`, `packaging/aur/PKGBUILD`, `packaging/aur/.SRCINFO`, and the
sample tag in `docs/handbook/installation.html` in lockstep. Hand edits
drift, downstream packaging breaks.

Source: bump targets in [`../../Makefile`](../../Makefile).

### 4. Release notes precede the bump

*Why:* `graywolf/pkg/releasenotes/notes.yaml` must contain an entry for
the *new* version before `make bump-*`. The bump targets `grep` for it
and refuse to run otherwise.

Source: [`../../Makefile`](../../Makefile),
[`../../graywolf/pkg/releasenotes/notes.yaml`](../../graywolf/pkg/releasenotes/notes.yaml),
[`../../CLAUDE.md`](../../CLAUDE.md).

### 5. Release notes ship as-tagged (retag contract)

*Why:* If CI fails after a tag is pushed and you delete-and-re-tag the
same version, do **not** rewrite the release note. Operators see whatever
shipped at the final successful tag; rewording between retags is a trust
hazard.

Source: [`../../CLAUDE.md`](../../CLAUDE.md) ("Retag contract").

### 6. Plain ASCII in release notes

*Why:* No emoji, no em dashes (use `--`), no smart quotes, no non-ASCII
punctuation. Keeps the operator-facing changelog portable; bump targets
do not re-encode the YAML.

Source: [`../../CLAUDE.md`](../../CLAUDE.md);
[`../../graywolf/pkg/releasenotes/notes.yaml`](../../graywolf/pkg/releasenotes/notes.yaml)
header.

### 7. PMTiles / offline-maps infra lives in `~/dev/graywolf-maps`, not here

*Why:* Tile generation, R2 sync, manifest publishing, the origin
Cloudflare Worker -- all in the maps repo. Graywolf is a *client*:
`mapsauth` (bearer token), `mapscache` (PMTiles), MapLibre rendering.

Source: absence of those modules in this tree;
`~/dev/graywolf-maps/.context/graywolf-client-integration.md`.

### 8. Audio I/O is on the Rust side

*Why:* CPAL runs in `graywolf-modem`; the Go side speaks to the modem
only via the IPC proto. Keeps realtime DSP out of Go's GC and
concentrates platform-specific audio in one place.

Source: [`../../graywolf-modem/src/audio/`](../../graywolf-modem/src/audio/);
no CPAL dep in `graywolf/pkg/`; control surface is the proto messages
`ConfigureAudio` / `StartAudio` / `StopAudio` / `EnumerateAudioDevices`.

### 9. PTT enumeration vs. driving split

*Why:* `graywolf/pkg/pttdevice/` enumerates serial / GPIO / CM108
hardware on the Go side; PTT *driving* runs on the Rust side
(`tx/ptt_*.rs`). The two must agree on the hardware identifier scheme
used over `ConfigurePtt.method` and `ConfigurePtt.device`.

Source: [`../../graywolf/pkg/pttdevice/`](../../graywolf/pkg/pttdevice/);
[`../../graywolf-modem/src/tx/`](../../graywolf-modem/src/tx/);
[`../../proto/graywolf.proto`](../../proto/graywolf.proto)
(`ConfigurePtt`).

### 10. Gitignored output dirs are not canonical

*Why:* `target/`, `bin/`, `dist/`, `rust-bin/`, `rust-artifacts/`,
`graywolf/web/dist/`, `.worktrees/`, `.context/`, `*.db*` are all
gitignored. They regenerate from sources; never reference them as
authoritative.

Source: [`../../.gitignore`](../../.gitignore);
[`build-pipelines.md`](build-pipelines.md).

### 11. Generated-bindings drift is enforced in CI

*Why:* `docs-check` and `api-client-check` regenerate to a tempdir and
`diff` against committed copies; both run inside `make go-test`. The
pre-commit hook runs them locally too.

Source: [`../../Makefile`](../../Makefile),
[`../../.githooks/`](../../.githooks/), `make install-hooks`.

### 12. Web UI is embedded into the Go binary at compile time

*Why:* `go:embed all:dist` means `go build` produces a self-contained
binary. The dir must exist; a placeholder `.keep` is enough for `go
build` to succeed before `npm run build` populates it.

Source: [`../../graywolf/web/embed.go`](../../graywolf/web/embed.go).

### 13. Modem readiness signal is on stdout, not the IPC channel

*Why:* The Go parent waits for `\n` (Unix) or `<port>\n` (Windows) on
the modem's stdout before connecting. Lets the parent know the bind
succeeded without a connect-retry race.

Source: [`../../graywolf-modem/src/ipc/server.rs`](../../graywolf-modem/src/ipc/server.rs);
[`../../graywolf/pkg/modembridge/`](../../graywolf/pkg/modembridge/)
(`ipc_unix.go`, `ipc_windows.go`).

### 14. Version display string is shared across Go and Rust

*Why:* Both sides produce `v<Version>-<GitCommit>`; modembridge checks
they match at startup. A mismatch is a quick visual signal that the two
halves of the build disagree.

Source:
[`../../graywolf/cmd/graywolf/main.go`](../../graywolf/cmd/graywolf/main.go),
[`../../graywolf/pkg/app/config.go`](../../graywolf/pkg/app/config.go),
[`../../graywolf-modem/build.rs`](../../graywolf-modem/build.rs).

### 15. Default IS->RF policy is deny

*Why:* The iGate IS->RF rule engine evaluates rules in priority order;
if no rule matches, traffic drops. Prevents accidental flooding of RF
with arbitrary internet traffic.

Source: [`../../graywolf/pkg/igate/filters/filters.go`](../../graywolf/pkg/igate/filters/filters.go)
(package comment).

### 16. TX path is single-source-of-truth via `txgovernor`

*Why:* Every TX origin (KISS, AGW, beacons, digipeater, iGate IS->RF)
funnels through one Governor before frames hit the modem -- one place
for per-channel rate limits, dedup, and priority. New TX sources must
route through it, not around.

Source: [`../../graywolf/pkg/txgovernor/governor.go`](../../graywolf/pkg/txgovernor/governor.go)
(package comment).

### 17. RX fanout carries provenance via `ingress.Source` (in-process)

*Why:* Lets KISS broadcast suppress its own loopback without leaking a
transport detail into the proto. The provenance tag is in-process only,
not on the wire.

Source: [`../../graywolf/pkg/app/ingress/source.go`](../../graywolf/pkg/app/ingress/source.go)
(package comment).

### 18. Generated artifacts that ship in commits

*Why:* Drift in these vs. their generators is what the CI guards in
[invariant 11](invariants.md) catch. Bump targets stage them so a
release tag includes regenerated copies.

Files:
[`../../graywolf/pkg/ipcproto/graywolf.pb.go`](../../graywolf/pkg/ipcproto/graywolf.pb.go),
`graywolf/pkg/webapi/docs/gen/swagger.{json,yaml}`,
[`../../graywolf/web/src/api/generated/api.d.ts`](../../graywolf/web/src/api/generated/api.d.ts),
[`../handbook/openapi.json`](../handbook/openapi.json),
[`../handbook/openapi.yaml`](../handbook/openapi.yaml).
Source: `GENERATED_SPEC_FILES` in [`../../Makefile`](../../Makefile).
