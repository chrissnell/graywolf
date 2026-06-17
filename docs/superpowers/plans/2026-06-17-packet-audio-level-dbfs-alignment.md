# Packet audio-level dBFS alignment (graywolf #324, GRA-130 Option 1)

> Reconstructed 2026-06-17 from the GRA-130 investigation comment, verified
> line-by-line against the current `main`. The original plan file lived only in
> the GRA-130 run's ephemeral workspace and was never committed.

## Goal

Express the per-packet received audio level in **dBFS** — the same unit the
real-time device meter already uses — so an operator's −25 dBFS signal reads
≈ −25 in both the Dashboard/Audio-Devices meter and the packet log. The packet
meter then reuses the device meter's dBFS colour zones, so the two meters agree.

## Background

- The Rust demod ships **linear** mark/space tone envelopes in
  `ReceivedFrame.audio_level_{mark,space}` (≈1.0 = full-scale tone). **No Rust
  change** — this is purely Go + web presentation.
- Today `audioLevelFromFrame` (`pkg/app/rxfanout.go`) scales those by ×100 into
  a 0–100 "Direwolf rec" number. That scale is calibrated for a signal ~20 dB
  hotter than operators actually run, so a healthy −25 dBFS packet reads ~5 and
  the meter sits pinned amber/empty.
- The device meter (`web/src/routes/Dashboard.svelte`) already uses dBFS with
  zones: red `> −6`, amber `−20…−6`, green `≤ −20`, range clamped `−60…0`.

## Conversion

`dBFS = 20·log10(amplitude)`, floored at **−60** (matching the device meter's
clamp). Spot checks: 1.0→0, 0.5→−6.0, 0.1→−20.0, 0.05→−26.0, ≤0.001→−60.
Non-positive amplitude (incl. the −1.0 "unset" placeholder) → −60.

## Field shape

`packetlog.AudioLevel` **keeps** `mark`/`space` (the existing linear ×100 ints,
for backward compatibility) and **gains** three dBFS fields:
`mark_dbfs`, `space_dbfs`, `level_dbfs` (float, one decimal). `level_dbfs` is
the dBFS of the mean of the two linear amplitudes (so it tracks the old `level`
= mean-of-tones semantics). The web meter switches to `level_dbfs`.

---

## Task 1 — Go: add dBFS fields to `packetlog.AudioLevel`

**File:** `pkg/packetlog/packetlog.go`

- Add `MarkDBFS`, `SpaceDBFS`, `LevelDBFS float64` with json tags
  `mark_dbfs`, `space_dbfs`, `level_dbfs`.
- Rewrite the struct doc comment: drop the "scaled 0-100 / ~50 healthy /
  Direwolf rec" convention; describe linear `mark`/`space` plus dBFS fields
  that match the real-time meter's unit. Keep the "twist = mark vs space
  spread" note.

**Verify:** `go build ./pkg/packetlog/...`

## Task 2 — Go: compute dBFS in `audioLevelFromFrame` + tests

**File:** `pkg/app/rxfanout.go`

- Add a `toDBFS(v float32) float64` helper: `v <= 0` → `-60`; else
  `20*log10(v)` clamped to a `-60` floor; round to 1 decimal.
- Populate `MarkDBFS`, `SpaceDBFS` from the per-tone amplitudes and
  `LevelDBFS` from `toDBFS((mark+space)/2)` using clamped (≥0) amplitudes.
- Keep the existing nil-gate (both ≤ 0 → nil) and the linear `Mark`/`Space`
  ×100 scaling unchanged.
- Update the function doc comment to describe the dBFS output.

**File:** `pkg/app/rxfanout_test.go`

- Extend `TestAudioLevelFromFrame` cases to assert the dBFS fields
  (0.5→−6.0, 1.0→0.0, 0.1→−20.0, 0.05→−26.0, floor at −60, and the
  `mark=-1.0, space=0.40` placeholder → `mark_dbfs=-60`).
- `TestDispatchRxFrameAudioLevelGating` already asserts the linear values and
  nil-gating; add a `level_dbfs` assertion on the modem entry.

**Verify:** `go test ./pkg/app/... ./pkg/packetlog/...`

## Task 3 — Web: switch the packet meter to dBFS + node:test

**File:** `web/src/lib/packetColumns.js`

- `audioLevel(pkt)` returns `{ level, mark, space, lit, zone }` where:
  - `level` = `Math.round(a.level_dbfs)` (integer dBFS for display).
  - `mark`/`space` = `a.mark_dbfs`/`a.space_dbfs` (rounded) for the tooltip.
  - `lit` = device-meter mapping: clamp dBFS to −60…0, `round((dbfs+60)/60*10)`.
  - `zone`: `level_dbfs > -6` → `hot`; `> -20` → `warm`; else `good`
    (mirrors the device meter's `levelColor`: red `>−6`, amber `−20…−6`,
    green `≤−20` — green is the nominal received level).
- Rewrite the docstring to the dBFS model.
- Return `null` when `a` or `a.level_dbfs == null`.

**File (new):** `web/src/lib/packetColumns.test.js`

- `node:test` covering: null when no `audio_level`; zone boundaries
  (−3→hot, −10→warm, −25→good); `lit` mapping (0→0, −30→5, −6→9/10);
  `level`/`mark`/`space` rounding.

**Verify:** `cd web && node --test 'src/**/*.test.js'`

## Task 4 — Web: packet-log viewer tooltip/label

**File:** `web/src/components/PacketLogViewer.svelte`

- Update the `levelCell` snippet so the numeric label and `title` read in dBFS
  (e.g. `audio level -25 dBFS (mark -25 / space -25)` and `{al.level} dBFS`).
  No structural/CSS change — `al.lit`/`al.zone` keep driving the segments.
- Refresh the audio-level comment in the file if it repeats the 0-100 scale.

**Verify:** `cd web && npx svelte-check --threshold error` (or `npm run check`)

## Task 5 — Regenerate swagger + api.d.ts

The struct doc/fields feed the generated OpenAPI spec and TS types.

**Commands:**
- `make docs` (regenerates `pkg/webapi/docs/gen/swagger.{json,yaml}` via swag)
- `make api-client` (regenerates `web/src/api/generated/api.d.ts`)
- `make api-client-check` to confirm no drift.

**Verify:** `packetlog.AudioLevel` in `api.d.ts` shows the three new
`*_dbfs` fields; `git diff --stat` lists only the generated artifacts.

## Task 6 — Verification sweep + wiki

- `go build ./...`, `go test ./...` (modem crate excluded — needs ALSA).
- `cd web && node --test 'src/**/*.test.js'` and `npm run check`.
- Update `docs/wiki/glossary.md` "per-packet audio level" row to the dBFS
  model (it currently says "scaled to ~0-100, ~50 healthy").
- Confirm `git status` is clean of stray files.
