# Build pipelines

How each artifact is produced. Orchestration entry: [`../../Makefile`](../../Makefile).
Release pipeline definition: [`../../.goreleaser.yml`](../../.goreleaser.yml).

## Producers and outputs

| Artifact | Producer | Inputs | Output | Trigger |
|---|---|---|---|---|
| Go binary `graywolf` | `make graywolf` (= `release web` + `go build`) | `cmd/graywolf/`, `pkg/...`, `web/dist/` (embedded) | `bin/graywolf` (+ `bin/graywolf-modem` copied from `target/release/`) | Manual |
| Go binary + web only (skip Rust) | `make graywolf-quick` (= `web` + `go build`) | same as above, minus `target/release/graywolf-modem` | `bin/graywolf` only; reuses existing `bin/graywolf-modem` | Manual; safe when `proto/` + `VERSION` unchanged |
| Windows resource file (icon + version metadata) | `goversioninfo` via `go generate` (`//go:generate goversioninfo -o resource_windows.syso versioninfo.json` in `cmd/graywolf/main.go`) | [`../../cmd/graywolf/versioninfo.json`](../../cmd/graywolf/versioninfo.json) (version numbers, `IconPath: "graywolf.ico"`), [`../../cmd/graywolf/graywolf.ico`](../../cmd/graywolf/graywolf.ico) | [`../../cmd/graywolf/resource_windows.syso`](../../cmd/graywolf/resource_windows.syso) — **committed to the repo**; `go build` picks it up automatically on Windows as a PE resource object, embedding the icon and version info visible in Explorer / Task Manager | `make bump-*` runs `cd cmd/graywolf && go generate .` after patching `versioninfo.json`; to regenerate manually: `go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest` then `cd cmd/graywolf && go generate .` |
| Rust modem (native dev) | `make release` | `graywolf-modem/`, `proto/graywolf.proto`, `VERSION` | `target/release/graywolf-modem` (workspace root, not `graywolf-modem/target/`) | Manual; see [invariant 1](invariants.md) |
| Rust modem (cross arm64) | `cross` per [`../../Cross.toml`](../../Cross.toml) | same + Docker image with aarch64 ALSA/udev libs and protoc | `target/<triple>/release/graywolf-modem` | Release CI |
| Rust modem (cross armv6) | `cross` per [`../../Cross.toml`](../../Cross.toml) target `arm-unknown-linux-gnueabihf` | same + Docker image with armhf ALSA/udev libs and protoc | `target/arm-unknown-linux-gnueabihf/release/graywolf-modem` | Release CI; scalar VFP, no NEON -- covers ARMv6-only Pi 1, Pi Zero / Zero W (and runs on any ARMv7 board too) |
| Rust modem (cross armv7+NEON) | `cross` per [`../../Cross.toml`](../../Cross.toml) target `armv7-unknown-linux-gnueabihf` | same Docker image / `:armhf` libs as armv6 (shared `arm-linux-gnueabihf` multi-arch tuple) | `target/armv7-unknown-linux-gnueabihf/release/graywolf-modem` | Release CI; NEON enabled via [`../../.cargo/config.toml`](../../.cargo/config.toml) so the AFSK demod FIR loop vectorizes -- for Cortex-A7+ boards (e.g. RV1106). Will SIGILL on ARMv6-only Pi |
| Web UI | `make web` (`npm install`, `npm run build`) | `web/src/`, `package.json`, themes, public, generated TS client | `web/dist/` (gitignored, then `go:embed` -- [invariant 12](invariants.md)) | Triggered by `make graywolf` / `make all` / `make api-client`. On Android, the `webBuild` Gradle task in `android/app/build.gradle.kts` also produces `web/dist/` before `goCrossCompile` runs, so APK builds can never ship a stale SPA. |
| Android APK / AAB | `./gradlew :app:assembleDebug` (APK) / `:app:bundleRelease` (Play AAB), run from `android/` | `android/`, `web/dist/` (auto-built via `webBuild`), Rust cdylib via `cargoNdkBuild`, Go binary cross-compiled per ABI via `goCrossCompile_{arm64-v8a,x86_64}` | `android/app/build/outputs/{apk,bundle}/...` | Manual; CI workflow planned in [`../superpowers/plans/2026-05-21-android-play-store-pipeline.md`](../superpowers/plans/2026-05-21-android-play-store-pipeline.md). Release signing reads keystore from `GRAYWOLF_KEYSTORE_*` env vars; emits an unsigned APK if env is absent. `versionCode` / `versionName` derive from the repo-root `VERSION` file at configure time, so `make bump-*` cascades into Android automatically. |
| TS API client | `make api-client` (`make docs` + `npm run api:generate`) | `pkg/webapi/docs/gen/swagger.{json,yaml}` | `web/src/api/generated/api.d.ts` (committed) | `make bump-*`; CI guard `api-client-check` |
| Proto codegen (Go) | `make proto` (`protoc --go_out`) | [`../../proto/graywolf.proto`](../../proto/graywolf.proto) | `pkg/ipcproto/graywolf.pb.go` (committed) | Manual after proto edits |
| Proto codegen (Rust) | `prost-build` in [`../../graywolf-modem/build.rs`](../../graywolf-modem/build.rs) | `proto/graywolf.proto`, `VERSION` | `OUT_DIR/graywolf.rs` (build-tree only; included via `src/ipc/proto.rs`) | Every `cargo build` (cargo `rerun-if-changed`) |
| Swagger spec | `make docs` (`swag init` + `tagify`) | swag annotations in `pkg/webapi`, `pkg/modembridge`, `pkg/webauth` | `pkg/webapi/docs/gen/swagger.{json,yaml}` (committed) | `make bump-*`; CI guard `docs-check` |
| Handbook OpenAPI sibling | `make docs-api-html` | `gen/swagger.{json,yaml}` | copies them to `docs/handbook/openapi.{json,yaml}` | Manual; see "two copies" note below |
| In-app release notes | hand-edited [`../../pkg/releasenotes/notes.yaml`](../../pkg/releasenotes/notes.yaml) | n/a | embedded in Go binary via `go:embed` | Hand-authored; bump targets refuse without an entry for the new version (see project [`../../CLAUDE.md`](../../CLAUDE.md)) |
| Release commit + tag | `make bump-point` / `make bump-minor` / `make bump-beta` | All of the above | git commit + `git tag vX.Y.Z` + push | Manual |
| Goreleaser archives | `.goreleaser.yml` `archives:` | Go binary (built per OS/arch by goreleaser) + `rust-bin/<os>_<arch>/graywolf-modem*` (pre-built outside goreleaser, supplied as `extra_files`) | Tarball / zip in goreleaser dist | Tag push (`release.yml`) |
| `.deb`, `.rpm` | goreleaser `nfpms:` | Go binary + rust-bin + systemd unit + udev rules + post/pre scripts | `.deb` / `.rpm` artifacts | Tag push |
| OCI image | goreleaser `dockers:` + [`../../Dockerfile.goreleaser`](../../Dockerfile.goreleaser), [`../../Dockerfile.goreleaser.arm64`](../../Dockerfile.goreleaser.arm64) | Go binary + `graywolf-modem-{amd64,arm64}` | `ghcr.io/chrissnell/graywolf:<tag>-{amd64,arm64}`, manifest list `:<tag>` and `:latest` | Tag push |
| Arch AUR | [`../../packaging/aur/PKGBUILD`](../../packaging/aur/PKGBUILD) (pkgname `graywolf-aprs`) | github archive of the tag, `.service`, `.sysusers` | AUR (off-repo upload by maintainer) | `make bump-*` rewrites `pkgver` in `PKGBUILD` and `.SRCINFO` |
| Windows NSIS installer | [`../../packaging/nsis/graywolf.nsi`](../../packaging/nsis/graywolf.nsi) (`makensis`) | `BINARY_PATH`, `MODEM_PATH`, `APP_VERSION`, `APP_VERSION_NUMERIC` | `graywolf_<ver>_Windows_x86_64.exe` | Manual; outside goreleaser |
| Pre-built rust-bin (CI) | rust-build matrix in `.github/workflows/release.yml` | Per-target `cargo build --release` (Linux amd64, arm64, armv6, armv7; macOS amd64, arm64; Windows amd64) | `rust-bin/<os>_<arch>/graywolf-modem[.exe]` -- note the two 32-bit ARM modems land in distinct dirs `linux_arm` (armv6) and `linux_armv7` (NEON); plus top-level `graywolf-modem-{amd64,arm64}` for the docker context (no 32-bit ARM docker image) | Tag push |

## CI workflows

| Workflow | Triggers | Jobs |
|---|---|---|
| `ci.yml` | push to main, PRs to main | Go vet + test (runs `docs-check` and `api-client-check` via `make go-test`) |
| `release.yml` | tag push `v*` | Rust build matrix, then goreleaser orchestrates Go + nfpm + docker |
| `fuzz.yml` | nightly `0 8 * * *` UTC, manual | `go-fuzz` on `pkg/ax25` and `pkg/aprs` |

The pre-commit hook in [`../../.githooks/`](../../.githooks/) (wired via
`make install-hooks`) runs the same `docs-check` / `api-client-check`
guards locally.

## Two 32-bit ARM builds share one dpkg arch

There are two distinct 32-bit ARM hard-float builds, and both the Go
(`GOARM=6`/`GOARM=7`) and Rust (`arm-` / `armv7-unknown-linux-gnueabihf`)
sides build twice:

- **armv6** (`GOARM=6`, scalar): broad compatibility, runs on every 32-bit
  Pi including ARMv6-only Pi 1 / Pi Zero.
- **armv7+NEON** (`GOARM=7`): NEON enabled in
  [`../../.cargo/config.toml`](../../.cargo/config.toml) so the demod FIR
  loop vectorizes. For Cortex-A7+ boards; **SIGILLs on ARMv6-only Pi**.

The trap: Go reports both as `goarch=arm` and dpkg labels both `armhf`, so
naive config makes the two collide (same archive name, same
`graywolf_<ver>_armhf.deb`, and the wrong Rust modem gets bundled). The
`.goreleaser.yml` disambiguates **by `GOARM`** in three places — archive
`name_template` (`armv6l` vs `armv7l`), the Rust modem `src` paths
(`rust-bin/linux_arm` vs `rust-bin/linux_armv7`), and the nfpm
`file_name_template` (armv7 gets an explicit `_armv7l` name; everything
else keeps `ConventionalFileName`). If you add another `goarm` value,
update all three or artifacts will silently overwrite each other.

## OpenAPI lives in two places

The swag-generated spec is committed to
`pkg/webapi/docs/gen/swagger.{json,yaml}` and is the source of
truth for the TS client generator. `make docs-api-html` copies it next to
the hand-edited [`../handbook/api.html`](../handbook/api.html), producing
[`../handbook/openapi.json`](../handbook/openapi.json) and
[`../handbook/openapi.yaml`](../handbook/openapi.yaml). The two copies
should agree because the handbook one is `cp`'d from the gen one. CI's
`docs-check` enforces that the gen file matches `make docs` output.

## Hand-edited (NOT pipelined)

- `docs/handbook/*.html` -- operator handbook, hand-edited. Only the
  `openapi.{json,yaml}` siblings are regenerated by `make docs-api-html`;
  `api.html` itself is checked-in static HTML and is not regenerated.
- `notes.yaml` release notes -- hand-authored before bump.
- [`../../packaging/nsis/graywolf.nsi`](../../packaging/nsis/graywolf.nsi)
  invocation -- run by hand, not goreleaser.
- `docs/changelogs/`, `docs/superpowers/` -- manual.

## Release ritual (summary)

The full operator-facing release flow lives in [`../../CLAUDE.md`](../../CLAUDE.md).
Wiki-side notes:

1. A `notes.yaml` entry for the new version must exist before
   `make bump-*`; the targets `grep` for it and refuse otherwise.
2. The bump target rewrites `VERSION`, `graywolf-modem/Cargo.toml`,
   `Cargo.lock`, `packaging/aur/PKGBUILD`, `packaging/aur/.SRCINFO`, and
   the sample tag in `docs/handbook/installation.html`. See
   [invariant 3](invariants.md).
3. Retag contract: if CI fails after the tag is pushed, delete and
   re-tag the same version; do not rewrite the release note. See
   [invariant 5](invariants.md).
