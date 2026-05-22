# Android release pipeline (Play Store)

How the Graywolf APRS Android app gets built, signed, and shipped to the
Google Play Store. This page is the *navigation + intent* layer; the
authority for build mechanics is
[`../../.github/workflows/android.yml`](../../.github/workflows/android.yml)
and [`../../android/app/build.gradle.kts`](../../android/app/build.gradle.kts).

## One-paragraph overview

`make bump-point` (or `bump-minor`) is the only manual action a release
needs. It writes `VERSION`, commits, tags `vX.Y.Z`, and pushes. The tag
push fires two workflows in parallel: `release.yml` (goreleaser:
Linux/macOS/Windows binaries, Docker, `.deb`/`.rpm`, NSIS) and
`android.yml`. The Android workflow builds a release-signed `.aab` +
`.apk`, attaches both to the GitHub Release goreleaser created, and
auto-uploads the `.aab` to the Play **Internal Testing** track. Promotion
from Internal to the **closed beta** track is a separate, deliberate
manual step (`gh workflow run android.yml --field version=X.Y.Z`) so a CI
tag never auto-ships to external testers.

## Workflow triggers and jobs (`android.yml`)

| Trigger | Jobs that run | Result |
|---|---|---|
| `pull_request` -> main | `build` | Unsigned debug APK artifact (sanity check) |
| `push` -> main | `build` | Same; catches breakage before a tag |
| `push` tag `v*` | `build` + `release-sign` | Signed `.aab`+`.apk` on the GH Release; `.aab` auto-uploaded to Play Internal |
| `workflow_dispatch` (no `version`) | `build` | Manual build re-run |
| `workflow_dispatch` (`version=X.Y.Z`) | `promote-to-closed` | Promotes that release's `.aab` from Internal to the closed beta track |

`build` is skipped on `workflow_dispatch` (a promote run doesn't need a
fresh APK). `release-sign` only runs on tags. `promote-to-closed` only
runs on `workflow_dispatch` with a `version`.

## Signing (Play App Signing)

Graywolf uses **Play App Signing**: Google holds the real app-signing
key; the developer holds only an *upload* key. Losing the upload key is
recoverable via a Play Console reset; the app key can't be lost because
Google holds it.

- Upload keystore: `1Password -> "Graywolf Android Upload Keystore"`
  (alias `graywolf-upload`, PKCS12). SHA-256 fingerprint is stored
  alongside; compare it against the `apksigner --print-certs` line the
  `release-sign` job prints to confirm a build was signed with the right
  key.
- Gradle reads the keystore from env (see
  `android/app/build.gradle.kts` `signingConfigs`):
  `GRAYWOLF_KEYSTORE_BASE64` (CI) or `GRAYWOLF_KEYSTORE_PATH` (local),
  plus `GRAYWOLF_KEYSTORE_PASSWORD`, `GRAYWOLF_KEY_ALIAS`,
  `GRAYWOLF_KEY_PASSWORD`. With none set, the release build emits an
  *unsigned* APK -- which is why PR-build CI (no secrets) stays green.
- Those four values are GitHub Actions secrets on `chrissnell/graywolf`.

## Play upload service account

The auto-upload (and the closed-beta promotion) authenticate to the Play
Developer API with a Google Cloud service account.

- GCP project `graywolf-play-upload`, service account `play-upload@...`,
  with the **Google Play Android Developer API** enabled.
- In Play Console -> Users and permissions, the account is granted
  **"Release apps to testing tracks"** on `com.nw5w.graywolf`. Production
  release permission is intentionally NOT granted, so CI can never ship
  to production.
- The JSON key is stored in `1Password -> "Graywolf Play Upload Service
  Account JSON"` and as the GH secret `GRAYWOLF_PLAY_SERVICE_ACCOUNT_JSON`.
- Both Play steps gate on that secret via a job-level
  `env: HAVE_PLAY_SA: ${{ secrets.GRAYWOLF_PLAY_SERVICE_ACCOUNT_JSON != '' }}`
  and `if: env.HAVE_PLAY_SA == 'true'`. (The `secrets` context is not
  allowed directly in `if:` -- it must be hopped through `env`.) When the
  secret is absent the steps skip, not fail.
- **Propagation:** after granting permission in Play Console, it can take
  up to 24h to take effect. Check without burning a release:
  `make android-play-check JSON=path/to/service-account.json` -- HTTP 200
  means ready, 403 means not propagated yet, 401 means a bad key.

## Version derivation

`android/app/build.gradle.kts` reads the repo-root `VERSION` file at
configure time, so `make bump-*` cascades into Android automatically:

- `versionName` = the `VERSION` string (e.g. `0.13.8`).
- `versionCode` = `M*1_000_000 + m*10_000 + p*100` (so `0.13.8` ->
  `130800`), leaving 100 slots per patch for hotfix re-uploads. Play
  requires `versionCode` to increase monotonically across uploads.

## Tracks

- **Internal** -- auto-uploaded on every `v*` tag. Dev/team loop.
- **Closed (`graywolf-beta`)** -- the ~15-person private beta. Builds
  reach it only via the manual `promote-to-closed` workflow. Add testers
  in Play Console -> Closed testing -> graywolf-beta -> Testers (email
  list); they install via the track's opt-in URL.
- **Production** -- not enabled. The service account lacks the permission
  and the workflow never targets it.

## Retag flow on CI failure

If a workflow fails *after* the tag is pushed, follow the retag contract
in [`../../CLAUDE.md`](../../CLAUDE.md): fix the cause, delete and re-tag
the same version (`git tag -d vX.Y.Z && git push origin :refs/tags/vX.Y.Z`,
commit fix, re-tag, push), and **do not rewrite the release note**. A
re-tag re-runs both workflows. Note Play rejects a duplicate
`versionCode`, so if the `.aab` already uploaded to Internal before the
failure, a plain re-tag's upload step will conflict -- in that case bump
to a new patch instead.

## armv6 (Pi 1 / Pi Zero) -- temporarily dropped

Unrelated to Android, but it rides the same release: the Linux armv6
cross-build is currently removed from `release.yml` (a cross-rs upstream
glibc/libudev mismatch). Pi 1 / Pi Zero / Pi Zero W users skip these
releases until it's restored. Tracking + restoration options are in the
session memory `project_followup_armv6_release`; search `FOLLOWUP` in
`release.yml`, `.goreleaser.yml`.

## Screenshots and store graphics

`make android-screenshots` drives the SPA in Android mode against a
local graywolf launched with `-demo` (canned Salt Lake-metro stations +
counters), capturing tablet + phone screenshots and rendering the
512x512 icon + 1024x500 feature graphic. Output under `scratch/ss-work/`
(gitignored -- holds rendered assets only). The harness fakes the
`GraywolfWebInterface` bridge so `Platform.kind === 'android'` and the
SPA renders the Android-filtered UI. See
[`../../scripts/screenshots/`](../../scripts/screenshots/) and the
`-demo` flag in `android/app/build.gradle.kts` / `pkg/demoseed`. Uploading
the assets to Play is manual (or a future `fastlane supply` step).

## What's hidden on Android

The SPA hides surfaces that don't work on Android (sidebar + route map):
Actions (command handlers can't `execve` under the W^X sandbox), AGW,
Simulation, and the `/login` flow (Android authenticates via the
per-launch WebView bearer token). See `web/src/components/Sidebar.svelte`
(`HIDDEN_ON_ANDROID`) and `web/src/App.svelte` (route map).
