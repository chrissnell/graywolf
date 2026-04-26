# Build pipelines

How each artifact is produced. Orchestration entry: [`../../Makefile`](../../Makefile).
Release pipeline definition: [`../../.goreleaser.yml`](../../.goreleaser.yml).

## Producers and outputs

| Artifact | Producer | Inputs | Output | Trigger |
|---|---|---|---|---|
| Go binary `graywolf` | `make graywolf` (= `release web` + `go build`) | `graywolf/cmd/graywolf/`, `graywolf/pkg/...`, `graywolf/web/dist/` (embedded) | `bin/graywolf` (+ `bin/graywolf-modem` copied from `target/release/`) | Manual |
| Rust modem (native dev) | `make release` | `graywolf-modem/`, `proto/graywolf.proto`, `VERSION` | `target/release/graywolf-modem` (workspace root, not `graywolf-modem/target/`) | Manual; see [invariant 1](invariants.md) |
| Rust modem (cross arm64) | `cross` per [`../../Cross.toml`](../../Cross.toml) | same + Docker image with aarch64 ALSA/udev libs and protoc | `target/<triple>/release/graywolf-modem` | Release CI |
| Web UI | `make web` (`npm install`, `npm run build`) | `graywolf/web/src/`, `package.json`, themes, public, generated TS client | `graywolf/web/dist/` (gitignored, then `go:embed` -- [invariant 12](invariants.md)) | Triggered by `make graywolf` / `make all` / `make api-client` |
| TS API client | `make api-client` (`make docs` + `npm run api:generate`) | `graywolf/pkg/webapi/docs/gen/swagger.{json,yaml}` | `graywolf/web/src/api/generated/api.d.ts` (committed) | `make bump-*`; CI guard `api-client-check` |
| Proto codegen (Go) | `make proto` (`protoc --go_out`) | [`../../proto/graywolf.proto`](../../proto/graywolf.proto) | `graywolf/pkg/ipcproto/graywolf.pb.go` (committed) | Manual after proto edits |
| Proto codegen (Rust) | `prost-build` in [`../../graywolf-modem/build.rs`](../../graywolf-modem/build.rs) | `proto/graywolf.proto`, `VERSION` | `OUT_DIR/graywolf.rs` (build-tree only; included via `src/ipc/proto.rs`) | Every `cargo build` (cargo `rerun-if-changed`) |
| Swagger spec | `make docs` (`swag init` + `tagify`) | swag annotations in `pkg/webapi`, `pkg/modembridge`, `pkg/webauth` | `graywolf/pkg/webapi/docs/gen/swagger.{json,yaml}` (committed) | `make bump-*`; CI guard `docs-check` |
| Handbook OpenAPI sibling | `make docs-api-html` | `gen/swagger.{json,yaml}` | copies them to `docs/handbook/openapi.{json,yaml}` | Manual; see "two copies" note below |
| In-app release notes | hand-edited [`../../graywolf/pkg/releasenotes/notes.yaml`](../../graywolf/pkg/releasenotes/notes.yaml) | n/a | embedded in Go binary via `go:embed` | Hand-authored; bump targets refuse without an entry for the new version (see project [`../../CLAUDE.md`](../../CLAUDE.md)) |
| Release commit + tag | `make bump-point` / `make bump-minor` / `make bump-beta` | All of the above | git commit + `git tag vX.Y.Z` + push | Manual |
| Goreleaser archives | `.goreleaser.yml` `archives:` | Go binary (built per OS/arch by goreleaser) + `rust-bin/<os>_<arch>/graywolf-modem*` (pre-built outside goreleaser, supplied as `extra_files`) | Tarball / zip in goreleaser dist | Tag push (`release.yml`) |
| `.deb`, `.rpm` | goreleaser `nfpms:` | Go binary + rust-bin + systemd unit + udev rules + post/pre scripts | `.deb` / `.rpm` artifacts | Tag push |
| OCI image | goreleaser `dockers:` + [`../../Dockerfile.goreleaser`](../../Dockerfile.goreleaser), [`../../Dockerfile.goreleaser.arm64`](../../Dockerfile.goreleaser.arm64) | Go binary + `graywolf-modem-{amd64,arm64}` | `ghcr.io/chrissnell/graywolf:<tag>-{amd64,arm64}`, manifest list `:<tag>` and `:latest` | Tag push |
| Arch AUR | [`../../packaging/aur/PKGBUILD`](../../packaging/aur/PKGBUILD) (pkgname `graywolf-aprs`) | github archive of the tag, `.service`, `.sysusers` | AUR (off-repo upload by maintainer) | `make bump-*` rewrites `pkgver` in `PKGBUILD` and `.SRCINFO` |
| Windows NSIS installer | [`../../packaging/nsis/graywolf.nsi`](../../packaging/nsis/graywolf.nsi) (`makensis`) | `BINARY_PATH`, `MODEM_PATH`, `APP_VERSION`, `APP_VERSION_NUMERIC` | `graywolf_<ver>_Windows_x86_64.exe` | Manual; outside goreleaser |
| Pre-built rust-bin (CI) | rust-build matrix in `.github/workflows/release.yml` | Per-target `cargo build --release` | `rust-bin/<os>_<arch>/graywolf-modem[.exe]`, plus top-level `graywolf-modem-{amd64,arm64}` for the docker context | Tag push |

## CI workflows

| Workflow | Triggers | Jobs |
|---|---|---|
| `ci.yml` | push to main, PRs to main | Go vet + test (runs `docs-check` and `api-client-check` via `make go-test`) |
| `release.yml` | tag push `v*` | Rust build matrix, then goreleaser orchestrates Go + nfpm + docker |
| `fuzz.yml` | nightly `0 8 * * *` UTC, manual | `go-fuzz` on `pkg/ax25` and `pkg/aprs` |

The pre-commit hook in [`../../.githooks/`](../../.githooks/) (wired via
`make install-hooks`) runs the same `docs-check` / `api-client-check`
guards locally.

## OpenAPI lives in two places

The swag-generated spec is committed to
`graywolf/pkg/webapi/docs/gen/swagger.{json,yaml}` and is the source of
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
