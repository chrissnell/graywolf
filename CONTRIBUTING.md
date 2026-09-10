# Contributing to Graywolf

Thanks for your interest in Graywolf. This document explains how the
project is run, how to set up a development environment, and what we
look for in a pull request. It is written for humans and for AI coding
agents alike: the rules are explicit and the commands are exact.

Graywolf is a small project. There are two maintainers, both volunteers
working on it in their spare time:

- Chris Snell, NW5W (`chrissnell`) -- original author and project lead
- Jared Crapo, K0TFU (`kotfu`) -- maintainer

The project is licensed under the GNU General Public License, version 2
(see [`LICENSE`](LICENSE)). By contributing you agree that your
contribution is licensed under the same terms. See
[Copyright and licensing](#copyright-and-licensing) below.

> **Draft note for reviewers:** paragraphs that begin with "Draft note"
> mark places where the maintainers still need to agree on a policy.
> Remove them before adoption.

## Table of contents

- [Contributing to Graywolf](#contributing-to-graywolf)
  - [Table of contents](#table-of-contents)
  - [Ways to contribute](#ways-to-contribute)
  - [Before you start: is this the right project for your change?](#before-you-start-is-this-the-right-project-for-your-change)
  - [Reporting bugs](#reporting-bugs)
  - [Requesting features](#requesting-features)
  - [How we use GitHub issue labels](#how-we-use-github-issue-labels)
  - [Setting up a development environment](#setting-up-a-development-environment)
    - [Toolchain](#toolchain)
    - [Optional tools](#optional-tools)
    - [First build](#first-build)
  - [How the Makefile works](#how-the-makefile-works)
    - [Build targets](#build-targets)
    - [Test and lint targets](#test-and-lint-targets)
    - [Generated-artifact targets](#generated-artifact-targets)
    - [Release targets](#release-targets)
  - [Running Graywolf from a source build](#running-graywolf-from-a-source-build)
    - [Web UI development loop](#web-ui-development-loop)
    - [Modem development loop](#modem-development-loop)
  - [Tests](#tests)
    - [What CI runs on every PR](#what-ci-runs-on-every-pr)
    - [Hardware testing](#hardware-testing)
  - [Documentation you are expected to update](#documentation-you-are-expected-to-update)
  - [Submitting a pull request](#submitting-a-pull-request)
    - [Principles](#principles)
    - [Branches](#branches)
    - [Workflow](#workflow)
    - [Commit and PR title style](#commit-and-pr-title-style)
    - [PR description](#pr-description)
    - [PR checklist](#pr-checklist)
    - [Review expectations](#review-expectations)
  - [Use of AI coding agents](#use-of-ai-coding-agents)
  - [Copyright and licensing](#copyright-and-licensing)
  - [Reporting security issues](#reporting-security-issues)
  - [Code of conduct](#code-of-conduct)
  - [Getting help](#getting-help)

## Ways to contribute

Not every contribution is code. All of these are welcome:

- **Bug reports** with enough detail to reproduce (see
  [Reporting bugs](#reporting-bugs)).
- **Bug fixes.** Always welcome. Small, focused fixes are the easiest
  kind of PR to review and merge.
- **Known-working configurations.** If you got Graywolf on the air with
  a radio, sound card, or PTT setup that is not listed on the
  handbook's [Known-Working Configs](docs/handbook/configurations.html)
  page, add it. Every entry helps the next operator. If you aren't
  comfortable opening your own pull request with your working configuration,
  open a new issue and put your working config in it.
- **Handbook improvements.** Corrections, clarifications, and missing
  steps in [`docs/handbook/`](docs/handbook/).
- **Helping other operators** on the
  [Graywolf APRS Discord](https://discord.gg/3r5brb7mjV) and in GitHub
  issues.
- **Testing** release candidates and pull requests on hardware the
  maintainers do not own. Graywolf runs on Linux (x86-64, arm64, armv6,
  armv7), macOS, Windows, and Android. Two people cannot cover that
  matrix alone.

## Before you start: is this the right project for your change?

Graywolf has a clear scope and a strong point of view about what an
APRS station should be. That is what makes it coherent. It also means
we are **not generally looking for big new features.** A large feature
takes review time we do not have, expands the surface area we have to
keep working across seven platforms, and often pulls the project in a
direction its author did not choose.

So, before you write code:

1. **Bug fixes: just do it.** If the behavior is clearly wrong, a PR
   with a fix and a test is the best possible bug report. You do not
   need to ask first.
2. **Small improvements:** if the change is a few dozen lines and does
   not add a new configuration surface, a PR is fine. If you are unsure,
   open an issue first. It costs you five minutes and can save you a
   weekend.
3. **New features or behavior changes:** **open an issue and have the
   discussion first.** Describe the problem you are solving, not just
   the solution. We would much rather say "yes, but do it this way" or
   "no, and here's why" before you have invested the work than reject a
   finished PR.
4. **If you feel strongly about a feature we decline,** you are welcome
   and encouraged to fork. That is exactly what the GPL is for.

The [`ROADMAP.md`](ROADMAP.md) lists modem work that is known to be
incomplete. Issues labeled `help wanted` and `good first issue` are
things we have already agreed we want.

## Reporting bugs

Open an issue at <https://github.com/chrissnell/graywolf/issues>. A
good bug report includes:

- **Graywolf version.** Run `graywolf version`, or copy the version
  string from the About page in the web UI. The string looks like
  `v0.14.13-f30e0066`. If you built from source, say so, and include
  the commit.
- **Platform.** OS and version, CPU architecture (`uname -m` on Linux),
  and how you installed Graywolf (`.deb`, `.rpm`, AUR, Docker, Windows
  installer, macOS tarball, Android, source build).
- **Hardware**, when the bug involves RF, audio, or PTT: radio, sound
  interface, PTT method, and how they are wired.
- **What you did, what you expected, what happened.** Exact steps.
- **Logs.** The Logs page in the web UI, or `journalctl -u graywolf` on
  a systemd install. Packet log excerpts are helpful for decode and
  routing problems.

Graywolf includes a diagnostic collector, `graywolf flare`, which
gathers configuration, system, audio, USB, and log data, shows you
exactly what it collected, lets you redact anything you want, and
submits it to the maintainers. If a maintainer asks you to run it,
please do. It saves several rounds of back and forth.

Please open one issue per bug. If you are not sure whether two symptoms
share a cause, open two issues and mention the other in each.

## Requesting features

Open an issue with the `enhancement` label. Explain:

- **The problem** you are trying to solve as an operator. What are you
  doing at the radio, and what gets in the way?
- **How you handle it today**, if you have a workaround.
- **Other software** that does this, if any, and what you like or
  dislike about how it does it.

Do not open a PR for a feature before the issue has been discussed. See
the previous section for why.

## How we use GitHub issue labels

Maintainers apply labels; you do not need to. Here is what they mean
when you see them.

| Label | Meaning |
|---|---|
| `bug` | Confirmed or strongly suspected defect. Fixes welcome. |
| `enhancement` | New feature or behavior change. Being discussed, or accepted but not started. |
| `documentation` | Handbook, wiki, README, or in-code documentation. |
| `question` | Needs clarification from the reporter, or is a support question rather than a defect. |
| `good first issue` | Small, well-scoped, and does not require deep knowledge of the codebase. A good place to start. |
| `help wanted` | We want this and would welcome a contributor taking it. Comment on the issue before you start so we can point you in the right direction. |
| `in-progress` | Someone (usually a maintainer) is actively working on it. **Ask before starting your own work** on an `in-progress` issue so we do not duplicate effort. |
| `blocked-pending-other-work` | Cannot proceed until something else lands. The blocking issue or PR is linked in the thread. |
| `backlog` | Acknowledged, but deliberately back-burnered. Not a priority for the maintainers right now. If you want to pick one up, comment first. |
| `PTT` | Push-to-talk related. PTT touches hardware, kernel drivers, and three operating systems, so these issues are grouped for triage. |
| `duplicate` | Already tracked elsewhere. The canonical issue is linked. |
| `invalid` | Not actionable as filed: not reproducible, not a Graywolf problem, or missing information that was not provided. |
| `wontfix` | Considered and declined. The reason is in the thread. This is not a judgment of the idea, only of its fit for this project. |

## Setting up a development environment

Graywolf is three programs in one repository:

| Component | Language | Where | Produces |
|---|---|---|---|
| `graywolf` service | Go | [`cmd/graywolf/`](cmd/graywolf/), [`pkg/`](pkg/) | `bin/graywolf` (embeds the web UI) |
| `graywolf-modem` DSP daemon | Rust | [`graywolf-modem/`](graywolf-modem/) | `bin/graywolf-modem` |
| Web UI | Svelte 5 + Vite | [`web/`](web/) | `web/dist/`, embedded into `bin/graywolf` |

The two binaries talk over a protobuf IPC channel defined in
[`proto/graywolf.proto`](proto/graywolf.proto). The Android app in
[`android/`](android/) wraps all three; you do not need the Android
toolchain unless you are working on Android.

Before anything else, read [`docs/wiki/README.md`](docs/wiki/README.md).
The wiki explains how the pieces connect and where things live. It is
the fastest way to orient yourself, and it is what the maintainers'
coding agents read first.

### Toolchain

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26 or newer | The exact minimum is the `go` directive in [`go.mod`](go.mod). |
| Rust | stable, via [rustup](https://rustup.rs) | No pinned toolchain file; CI uses current stable. `cargo fmt` and `cargo clippy` components are required. |
| Node.js | 22 | Plus npm. The web build uses `npm ci`, so the committed `package-lock.json` is authoritative. |
| GNU Make | any recent | Orchestrates everything. |
| `protoc` | 28.x | Required to build the Rust modem: its `build.rs` compiles `proto/graywolf.proto` on every build. |
| ALSA development headers | | Linux only. `libasound2-dev` on Debian/Ubuntu, `alsa-lib` on Arch, `alsa-lib-devel` on Fedora. |

You do **not** need `libhidapi` or `libudev`. The modem uses pure-Rust
backends for both.

On Debian or Ubuntu:

```bash
sudo apt-get install -y build-essential libasound2-dev protobuf-compiler
```

On macOS with Homebrew:

```bash
brew install go node protobuf
```

Then install Rust via rustup on either platform.

### Optional tools

Install these only when you need the corresponding `make` target.

**swag** (OpenAPI generator) is needed for `make docs`, and therefore
for `make go-test`, which runs the OpenAPI drift check. Install the
exact version CI uses into the repo-local `scratch/bin` so it does not
pollute your `GOBIN`:

```bash
GOBIN=$(pwd)/scratch/bin go install github.com/swaggo/swag/cmd/swag@v1.16.4
```

Do not use a newer swag. Different swag versions emit slightly different
output, and CI will reject a spec regenerated with a version that does
not match its own.

**protoc-gen-go** is needed only for `make proto`, which you run after
editing [`proto/graywolf.proto`](proto/graywolf.proto):

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
```

**cross** is needed only to cross-compile the Rust modem for ARM. See
the Build from Source tab on the handbook's
[Installation](docs/handbook/installation.html) page.

### First build

```bash
git clone https://github.com/chrissnell/graywolf.git
cd graywolf
make install-hooks   # one time per clone; see below
make graywolf
```

`make graywolf` builds the Rust modem in release mode, installs web
dependencies and builds the UI, then builds the Go binary with the UI
embedded. Output is `bin/graywolf` and `bin/graywolf-modem`. The first
Rust build takes several minutes; later builds are incremental.

`make install-hooks` points git at [`.githooks/`](.githooks/), which
holds a pre-commit hook that runs the same generated-artifact drift
checks CI runs. It only fires when you commit `.go` files or anything
under `web/`. Bypass it for a single commit with `git commit
--no-verify` if you must, but CI will run the same checks.

## How the Makefile works

The [`Makefile`](Makefile) is the single entry point for building,
testing, and releasing. It is well commented; when in doubt, read it.
The wiki's [build pipelines](docs/wiki/build-pipelines.md) page has the
full artifact-by-artifact table.

### Build targets

| Target | What it does | When to use it |
|---|---|---|
| `make graywolf` (same as `make all`) | Rust release build + web build + Go build. Stages both binaries into `bin/`. | The default full build. Required after changing `proto/` or `VERSION`. |
| `make graywolf-quick` | Web build + Go build only. Reuses the existing `bin/graywolf-modem`. | Fast iteration on Go or web code when the modem is unchanged. Refuses to run if `bin/graywolf-modem` is missing. |
| `make release` | Rust modem, release profile, `-C target-cpu=native`. Output in `target/release/` at the repo root. | Modem work where you want realistic DSP performance. |
| `make build` | Rust modem, debug profile. | Modem work where you want fast compiles and debug symbols. |
| `make web` | `npm ci` (if needed) + `vite build` into `web/dist/`. | Usually invoked for you by `make graywolf`. |
| `make go-build` | `go build ./...` with version stamping. | Compile check without producing `bin/`. |
| `make clean` | Remove Rust `target/`, `bin/`, `web/node_modules`, `web/dist`. Leaves committed generated files alone. | Start over. |
| `make clean-web` | Remove only `web/node_modules` and `web/dist`. | After pulling a lockfile change made on another OS. |
| `make distclean` | `clean` plus wipe committed generated artifacts. | Almost never. Only when intentionally regenerating the OpenAPI spec and TS client from scratch. |

Note that Rust output lands in `target/` at the **repository root**, not
in `graywolf-modem/target/`. The root `Cargo.toml` is a workspace shim
that exists so cross-compilation containers can see `proto/` and
`VERSION`. Several paths depend on this; do not "fix" it.

### Test and lint targets

| Target | What it does |
|---|---|
| `make go-test` | `docs-check` + `api-client-check`, then `go test -race ./...`. This is what CI runs. |
| `make test` | `cargo test` for the modem. |
| `make lint` | `cargo fmt` then `cargo clippy -- -D warnings`. CI fails on any clippy warning. |
| `make check` | `cargo check` (type-check without codegen). |
| `make bench` | `cargo bench` (criterion benchmarks). |
| `make go-fuzz` | Go fuzz targets for the AX.25 and APRS parsers. `FUZZTIME=5m make go-fuzz` to run longer. |
| `make docs-lint` | Verifies every OpenAPI `@ID` annotation is declared as a constant. Runs in CI. |

### Generated-artifact targets

Several generated files are **committed** to the repository, and CI
fails if they drift from what the generator would produce. When you
change the corresponding source, regenerate and commit the result in
the same PR.

| You changed | Run | Commits |
|---|---|---|
| A swag annotation on any HTTP handler in `pkg/webapi`, `pkg/modembridge`, or `pkg/webauth` (new endpoint, new DTO field, changed description) | `make api-client` | `pkg/webapi/docs/gen/swagger.{json,yaml}` and `web/src/api/generated/api.d.ts` |
| Only the spec, not the client | `make docs` | `pkg/webapi/docs/gen/swagger.{json,yaml}` |
| [`proto/graywolf.proto`](proto/graywolf.proto) or `proto/platform.proto` | `make proto` | `pkg/ipcproto/*.pb.go`. The Rust side regenerates itself on the next `cargo build`. |
| Anything in `pkg/flareschema` | `make flareschema` | `docs/flareschema/v1.json` |

`make docs-check` and `make api-client-check` are the drift guards. They
regenerate into a temp dir and diff. The pre-commit hook and CI both
run them.

### Release targets

`make bump-point`, `make bump-minor`, `make bump-beta`, and the
`handbook-sync*` and `android-*` targets are **maintainer-only**. They
rewrite version files, commit, tag, and push. Never run them from a
fork, and never edit `VERSION`, the `version` in
`graywolf-modem/Cargo.toml`, the AUR packaging files, or
`pkg/releasenotes/notes.yaml` in a pull request. Maintainers do that at
release time.

## Running Graywolf from a source build

```bash
./bin/graywolf
```

By default this listens on `http://127.0.0.1:8080` and creates
`graywolf.db`, `graywolf-history.db`, and `graywolf-logs.db` in the
current directory (all gitignored). The service finds `graywolf-modem`
next to its own binary, so running from `bin/` just works. Pass
`-http 0.0.0.0:8080` to reach it from another machine on your LAN.

On first run the web UI walks you through creating an admin password.

### Web UI development loop

For UI work you want Vite's hot reload instead of rebuilding the Go
binary on every change. The Vite dev server proxies `/api` to port
`8081`, so run the backend there:

```bash
./bin/graywolf -http 127.0.0.1:8081
```

and in a second terminal:

```bash
cd web && npm run dev
```

Open the URL Vite prints. If no backend is running, dev mode falls back
to mock data so pages stay explorable, but anything involving real
packets, audio devices, or PTT needs the real backend.

When you are done, `make graywolf-quick` rebuilds the embedded UI so
the Go binary matches what you saw in the dev server.

### Modem development loop

The modem can be exercised without a radio. `graywolf-modem --decode
<file.wav|file.flac>` runs the demodulator over a recording and prints a
JSON score. `graywolf-modem/bench.sh` runs the WA8LMF TNC Test CD
tracks head-to-head against Dire Wolf; see the README's Performance
section. The test tracks are not in the repository; several Rust tests
skip themselves if `aprs-test-tracks/` is absent.

There is a manual end-to-end loopback procedure for the TX path in
[`graywolf-modem/tests/loopback_README.md`](graywolf-modem/tests/loopback_README.md).

## Tests

Every PR that changes behavior needs a test that fails without the
change and passes with it. Where each kind of test lives:

| Component | Run | Notes |
|---|---|---|
| Go | `go test -race ./...` or `go test ./pkg/<name>/` | Table-driven `_test.go` files next to the code. Follow the conventions of the package you are in. |
| Rust | `make test` or `cargo test -p graywolf-demod` | Unit tests inline with `#[test]`; integration tests in `graywolf-modem/tests/`. |
| Web | `cd web && npm test` | `node --test` over `src/**/*.test.js`. Pure-logic modules under `web/src/lib/` are tested; Svelte components generally are not. |
| Web API client | `cd web && npm run api:check` | Type-checks the generated client. |
| Example Action scripts | shellcheck, ruff, pytest | CI lints `examples/actions/`. Scripts must not use `eval` or `sh -c`, and must quote every `$GW_*` expansion. |

Some tests skip when a prerequisite is missing rather than fail:

- Go tests that drive the real modem binary (`pkg/flareschema`,
  `pkg/modembridge`) skip if `graywolf-modem` is not built. Run
  `make release` first for full coverage.
- Rust end-to-end tests skip without `aprs-test-tracks/`.
- `TestMigrateFromPriorRelease` needs a fixture generated by
  `scripts/testdata/gen_prev_release_db.sh`. CI generates it; locally it
  skips unless you run the script.

A skipped test is fine locally. CI on Linux runs the full set.

### What CI runs on every PR

[`.github/workflows/ci.yml`](.github/workflows/ci.yml):

- **Go Vet + Test:** `make docs-check`, `make docs-lint`,
  `make api-client-check`, `go vet ./...`, `go test -race ./...`.
- **Rust Check + Clippy:** `cargo check --workspace`,
  `cargo clippy --workspace -- -D warnings`.
- **Example Scripts Lint:** shellcheck and safety greps on
  `examples/actions/posix/`, ruff and pytest on
  `examples/actions/python/`.

[`.github/workflows/android.yml`](.github/workflows/android.yml) also
builds an unsigned debug APK on every PR. A nightly job in
[`.github/workflows/fuzz.yml`](.github/workflows/fuzz.yml) runs the
AX.25 and APRS parser fuzzers.

CI runs on Linux only. If your change touches macOS-, Windows-, or
Android-specific code, say in the PR what you tested it on. If you
cannot test a platform, say that too, and we will find someone who can.

### Hardware testing

Graywolf's job is to put packets on the air correctly. Tests cannot
prove that. If your change touches the modem, audio, PTT, KISS, or the
TX path, tell us in the PR description what radio, interface, and PTT
method you tested with and what you observed (decodes, a second station
hearing you, PTT timing). If you tested against a recording rather
than live RF, say so.

## Documentation you are expected to update

Graywolf has three documentation surfaces, each with a different
audience. A PR is incomplete if it changes behavior without updating
the ones that apply.

**The operator handbook** ([`docs/handbook/`](docs/handbook/), published
at <https://chrissnell.com/software/graywolf/>) is hand-edited HTML for
people running a station. Update it when you add or change anything an
operator can see or configure: a setting, a page, a CLI flag, a
supported device, an install step. Follow the structure of the page you
are editing; the CSS and navigation are shared. Do not edit
`docs/handbook/api.html`, `openapi.json`, or `openapi.yaml` by hand;
they are generated. Maintainers publish the handbook after merge.

**The wiki** ([`docs/wiki/`](docs/wiki/)) is for developers and for
coding agents. It records how the pieces connect, where things live,
and the cross-cutting rules ("if you change X you must also change Y")
that are not obvious from any single file. Update it when you:

- add, rename, or move a component, package, endpoint, or file that
  other people will need to find (`code-map.md`);
- add a new "if X then also Y" rule, or discover one the hard way
  (`invariants.md`);
- change topology: a process, a port, a persisted file, an external
  service (`system-topology.md`);
- add a build stage or generated artifact (`build-pipelines.md`).

The wiki's own rule is: if you had to grep to find something the wiki
should have told you, add it. If the wiki disagrees with the code, the
code wins; fix the wiki in the same PR. The maintenance triggers are
spelled out in [`CLAUDE.md`](CLAUDE.md), and they apply to everyone,
not just agents.

**In-code documentation.** Go doc comments on exported identifiers,
Rust doc comments on public items, and swag annotations on HTTP
handlers (which feed the REST API reference). Regenerate the spec when
you touch an annotation; see
[Generated-artifact targets](#generated-artifact-targets).

**Release notes** ([`pkg/releasenotes/notes.yaml`](pkg/releasenotes/notes.yaml))
are written by a maintainer at release time, in the operator's
language. Do not edit this file in a PR. If you want to suggest how
your change should be described to operators, put a sentence in the PR
description and we will use it.

## Submitting a pull request

### Principles

- **Small and reviewable.** A PR should do one thing. If you fixed two
  bugs, open two PRs. If a refactor is needed before a fix, open the
  refactor first and the fix on top. A 100-line PR will be reviewed
  more quickly than a 2,000-line PR.
- **Bug fixes are always welcome** and do not need prior discussion.
- **Features need an issue first.** See
  [Before you start](#before-you-start-is-this-the-right-project-for-your-change).
- **Tests and docs are part of the change,** not a follow-up.
- **You must understand every line you submit.** See
  [Use of AI coding agents](#use-of-ai-coding-agents).

### Branches

`main` is the only long-lived branch. It must always build and pass CI,
because releases are tagged directly on it: the bump targets tag `main`
as `vX.Y.Z`, and betas as `vX.Y.Z-beta.N`. There are no `develop`,
`release`, or maintenance branches. A hotfix is a fix merged to `main`
followed by a point release.

Everything else is a short-lived topic branch: cut from `main`,
carrying one change, opened as one PR against `main`. Maintainers push
topic branches to the main repository; everyone else works from a fork.
Either way the change arrives as a PR.

Name branches `<type>/<short-kebab-description>`:

| Prefix | Use for | Examples from this repository |
|---|---|---|
| `fix/` | Bug fixes | `fix/kiss-manager-rebind-race`, `fix/issue-207-protectclock` |
| `feature/` | New or changed behavior (agree on it in an issue first) | `feature/fixed-gps-coordinate`, `feature/igate-packet-type-filter` |
| `docs/` | Handbook, wiki, README, or this file, with no code change | `docs/arch-hidapi-link-note`, `docs/contributing` |
| `chore/` | Tooling, CI, dependency bumps, housekeeping | `chore/glob-override-bump` |
| `refactor/` | Behavior-preserving restructuring | `refactor/split-modem-and-app` |

Putting the issue number in the name (`fix/issue-207-...`) is welcome
but not required; the `(GH #NNN)` in the PR title is what ties the work
to the issue.

Housekeeping:

- Do not open a PR from your fork's `main`. Keep it clean so you can
  pull upstream changes into it.
- If `main` moves under you before review starts, rebase or merge as
  you like. Once review has started, merge `main` into your branch
  rather than rebasing, so review comments stay attached to the commits
  they were made on.
- Delete your branch after the PR is merged or closed.

### Workflow

1. Fork the repository and create a topic branch from `main`, named as
   described in [Branches](#branches).
2. Make the change. Run the tests and linters for every component you
   touched (`make go-test`, `make test`, `make lint`,
   `cd web && npm test`). Regenerate committed artifacts if needed.
3. Commit in the project's style (below).
4. Open a pull request against `main`. Draft PRs are welcome if you want
   early feedback on direction.
5. Respond to review. Push additional commits rather than force-pushing
   over reviewed ones; it makes re-review much easier. We squash on
   merge, so the intermediate history does not matter.

There is no CLA to sign.

### Commit and PR title style

Titles take the form `area: imperative summary (GH #NNN)`, where the
area is the subsystem and `#NNN` is the issue being fixed:

```
messages: fix chat window dropping newest messages on busy threads (GH #521)
gps: add fixed station coordinate as a third GPS source (GH #510)
handbook: add LoRa SPI HAT / KISS TNC known-working configuration
```

Common areas: `modem`, `messages`, `igate`, `digipeater`, `kiss`,
`ptt`, `gps`, `beacon`, `livemap`, `web`, `webapi`, `android`,
`handbook`, `wiki`, `docs`, `examples`, `ci`, `test`. Use whatever
reads naturally; the goal is that `git log --oneline` scans well.

Maintainers usually squash-merge, so the PR title becomes the commit
message on `main`. Write it accordingly.

### PR description

Write for a maintainer who has not been following the issue. The best
PR descriptions in this repository are structured like a short
engineering note:

- **Summary:** what changed, one paragraph, linking the issue.
- **Root cause** (for bugs): what was actually wrong and why. Quote the
  relevant code or log line.
- **The fix:** what you did and, importantly, what alternatives you
  rejected and why.
- **Tests:** what the new test asserts, and what you ran.
- **Hardware / platform tested**, when relevant.
- **Docs:** which handbook and wiki pages you updated.
- **Follow-ups:** anything you deliberately left out of scope.

### PR checklist

Copy this into your PR description and check what applies.

```markdown
- [ ] One logical change; separate issues are separate PRs
- [ ] For features: linked issue where the approach was agreed
- [ ] Tests added or updated; they fail without the change
- [ ] `make go-test` passes (Go changes)
- [ ] `make test` and `make lint` pass (Rust changes)
- [ ] `cd web && npm test` passes (web changes)
- [ ] Generated artifacts regenerated and committed (`make api-client` / `make proto` / `make flareschema`), if applicable
- [ ] Handbook updated for any operator-visible change
- [ ] Wiki updated for any new component, file, invariant, endpoint, or topology change
- [ ] Platforms and hardware tested are listed in the description
- [ ] No edits to VERSION, Cargo version, AUR files, or notes.yaml
- [ ] I understand and can explain every line in this diff
```

### Review expectations

Two volunteers review everything, and both of them have families, jobs, and love
the outdoors. Temper your expectations on turnaround times accordingly. We may
ask for changes, ask you to split a PR, or ask you to hold a change until
something else lands. We may also decline a PR; when we do, we will explain why.

## Use of AI coding agents

Both maintainers use AI coding agents extensively, and a large share of
this codebase was written with their help. You are welcome to do the
same. The repository is deliberately set up for it:

- [`CLAUDE.md`](CLAUDE.md) holds project-wide instructions for agents,
  including the wiki-maintenance rules and the release workflow.
- [`docs/wiki/`](docs/wiki/) is written so an agent can orient itself
  in one read. Point your agent at
  [`docs/wiki/README.md`](docs/wiki/README.md) before it touches code.
- This document is written so an agent can follow it. If you are an
  agent reading this: the PR checklist above is binding, the
  wiki-maintenance triggers in `CLAUDE.md` are binding, and the
  release targets are off limits.

The rules for AI-assisted contributions are the same as for any other
contribution, with one point of emphasis:

**You are the author.** The agent is a tool. You are responsible for
understanding what the code does, for having tested it, and for being
able to answer review questions about it. If a reviewer asks "why did
you do it this way?" and the honest answer is "I don't know, the agent
did it," the PR is not ready. Read the diff before you open the PR.

Things that will get a PR closed quickly:

- Large, unrequested changes that an agent produced from a vague
  prompt. See the section on scope; the rule that features need an
  issue first applies doubly here, because agents make it cheap to
  generate a lot of code nobody asked for.
- Changes the contributor cannot explain.
- "Fixes" that make a test pass without addressing the reported
  problem.
- Sweeping stylistic rewrites, reformatting, or "improvements" to code
  the PR did not need to touch.

You do not have to disclose that you used an agent, but you are welcome
to. Some commits in this repository carry a `Co-authored-by:` trailer
naming the model; that is fine and you may do the same. What matters to
us is the quality of the change and that a human stands behind it.

## Copyright and licensing

Graywolf is licensed under the GNU General Public License, version 2.
Contributions are accepted under the same license ("inbound equals
outbound"). By opening a pull request you confirm that you have the
right to contribute the code under GPL-2.0 and that you are doing so.
You retain copyright in your contribution; the project does not require
copyright assignment or a contributor license agreement.

If your contribution includes code ported from or derived from another
project, it must be under a GPL-2.0-compatible license, and you must
say so in the PR and preserve the original attribution in the source.
Graywolf already does this for the Dire Wolf demodulator port and for
techniques credited to libmodem; follow those examples.

> **Draft note for reviewers:** the maintainers have not yet settled a
> position on copyright in AI-generated contributions. Questions to
> resolve before adoption, then replace this note with the policy:
>
> - Do we ask contributors to disclose AI-generated code, or is the
>   "you are the author" standard above sufficient?
> - Do we want a Developer Certificate of Origin (`Signed-off-by`)
>   asserting the contributor has the right to submit the code, which
>   many GPL projects use instead of a CLA?
> - How do we treat a contribution that is substantially agent-written
>   with minimal human authorship, given the unsettled legal status of
>   copyright in such output? Does it change anything for a GPL project
>   in practice?
> - Should the `Co-authored-by: <model>` trailer be required, optional,
>   or discouraged?
>
> Until this is resolved, the inbound-equals-outbound rule above applies
> to all contributions regardless of how they were produced.

## Reporting security issues

Graywolf listens on the network (web UI, KISS over TCP, AGWPE), executes
operator-configured scripts via Actions, and talks to radios. By default,
it does not run as a privileged user, but there is a large security surface
area.

If you find a vulnerability, **do not open a public issue.** Email the
maintainers at <graywolf@nw5w.com> with a description and reproduction
steps. We will acknowledge within a few days and work with you on a fix
and a disclosure timeline.

> **Draft note for reviewers:** confirm `graywolf@nw5w.com` is the
> address Chris wants for this (it is the contact on the handbook's
> privacy page). Also consider enabling GitHub's private vulnerability
> reporting on the repository so reports can arrive as draft advisories;
> it is currently off.

## Code of conduct

Graywolf is an amateur radio project and promotes:

- non-commercial public service
- advancement of the radio art
- improving technical and communication skills
- global communication and international goodwill

Concretely, in issues, pull requests, Discord, and anywhere else you
represent this project:

- **Be respectful.** Disagree with ideas, not people. Assume good faith.
- **Be constructive.** "This is wrong" helps nobody; "this fails when
  the SSID is 0, here is a packet that shows it" helps everybody.
- **Be patient.** Operators arrive with every level of experience, from
  a brand-new Technician to someone who has been building TNCs since
  the 1980s. Neither deserves condescension.
- **Be welcoming.** Harassment, personal attacks, discriminatory
  language, and unwelcome sexual attention are not tolerated, in any
  form, toward anyone.
- **Respect the maintainers' time and decisions.** You may disagree
  with a `wontfix`. Say so once, clearly, and then let it go or fork.

Maintainers may edit, hide, or delete comments and may block
contributors who violate these expectations. If you experience or
witness unacceptable behavior, contact the maintainers privately at
<graywolf@nw5w.com>. Reports are handled confidentially.

> **Draft note for reviewers:** this is a deliberately short,
> self-contained code of conduct. If the maintainers would rather adopt
> the full [Contributor Covenant](https://www.contributor-covenant.org/)
> 2.1, it goes in a separate `CODE_OF_CONDUCT.md` (which GitHub
> recognizes in the community profile) and this section becomes a
> pointer to it. Either way, confirm the reporting contact.

## Getting help

- **Using Graywolf:** the [handbook](https://chrissnell.com/software/graywolf/)
  first, then the [Graywolf APRS Discord](https://discord.gg/3r5brb7mjV).
- **Developing Graywolf:** [`docs/wiki/README.md`](docs/wiki/README.md),
  then the `Makefile`, then ask on Discord or in a draft PR.
- **Is this a bug?** When in doubt, open an issue. A closed
  `question` costs nothing.

