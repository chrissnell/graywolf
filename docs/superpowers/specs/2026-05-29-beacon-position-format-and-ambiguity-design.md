# Beacon Position Format Selector + Position Ambiguity

**Date:** 2026-05-29
**Status:** Design approved, ready for planning
**Scope:** Add operator-selectable position report format (Compressed /
Uncompressed / Mic-E) to Position, Tracker, and IGate beacons, with
per-beacon position-ambiguity control on the two formats that support
it. Replaces the implicit `compress=true` default that's been baked into
new beacons since v0.10. Adds Mic-E TX, which graywolf has not had to
date.

Closes operator request [#146](https://github.com/chrissnell/graywolf/issues/146).

---

## 1. Why

APRS101 supports three position report formats. Graywolf has historically
emitted only the compressed format (base-91, 13 bytes, ~5 cm resolution)
because it's the densest and most precise on-air representation. But
several real operator needs are now bumping into that choice:

1. **Position ambiguity.** Many operators want to publish a deliberately
   coarse position - home QTH privacy, "I'm somewhere in this town"
   group meetups, mobile stations on public maps that don't want
   address-level precision. APRS101 supports this natively on
   uncompressed and Mic-E position reports by replacing trailing
   minute/hundredths digits with spaces. **Compressed positions
   cannot carry ambiguity** - base-91 has fixed precision.

2. **Mic-E.** Mic-E is the most efficient APRS position format on the
   air (9-byte info field vs 19 for uncompressed or 13 for compressed)
   and was designed for mobile / tracker stations. Graywolf can
   *receive* Mic-E packets today (full parser in `pkg/aprs/mice.go`),
   but cannot *send* them - the destination-callsign encoder
   (`EncodeMicEDest`) was scaffolded for future use but the info-field
   encoder, the beacon-builder dispatch, and the UI surface were never
   built.

3. **Existing schema is half-wired.** The `Beacon.Ambiguity` column has
   been on the GORM model since the beacons table was created, with a
   matching DTO field and Swagger documentation, but **nothing reads
   it**. The encoder in `pkg/beacon/encoder.go` ignores the column on
   every TX. This spec finishes that wiring.

## 2. Out of scope

These are deliberately excluded from this plan and noted here so that
they don't sneak in during implementation review.

- **RX-side ambiguity rendering on the map.** Drawing translucent
  circles around stations whose received position is ambiguous is a
  separate UI feature and a separate plan. The RX parser already
  records `Position.Ambiguity` (`pkg/aprs/types.go:48`); the work to
  render it is independent of this TX-side change.
- **Object beacons.** The `Beacon.Ambiguity` column is type-agnostic
  and the object encoder uses the same uncompressed lat/lon string, so
  ambiguity is technically achievable there too. Out of scope for this
  plan; objects keep their current always-exact behavior. If a future
  operator request lands, it's a one-line wiring change.
- **Operator-selectable Mic-E message code.** The Mic-E wire format
  requires a 3-bit message code in the destination callsign. This plan
  hard-codes M0 ("Off Duty") - the most innocuous standard code -
  rather than adding a UI knob nobody asked for. If a tracker user
  wants "In Service" or "Emergency", a follow-up plan can add the field.
- **Mic-E telemetry channels, status text, and other extensions.**
  Almost nobody transmits these. The encoder will reject them via
  YAGNI; a future plan can add them if the demand is real.
- **Direwolf-style auto-format switching** (e.g. "always send Mic-E for
  trackers, always send Compressed for fixed stations"). The operator
  picks per-beacon; this is graywolf, not an opinionated auto-mode.
- **Removing the compressed format as a choice.** It stays the default
  for new beacons.

## 3. User-facing change (Beacons.svelte)

The "Edit beacon" / "New beacon" modal grows a new section, placed
between the existing Symbol section and the Position-source section:

```
Position report format
( ) Compressed (highest precision)
( ) Uncompressed (standard precision)
( ) Mic-E (most efficient)

[ ] Use position ambiguity
    Level: [ Block (~600 ft)              v ]
```

### 3.1 Radio behavior

- The radio is shown only for `type ∈ {position, tracker, igate}`. For
  `type = object` or `type = custom` the radio (and the whole ambiguity
  sub-block) is hidden.
- New beacons default to **Compressed** (preserves today's behavior).
- Existing beacons load with the format derived from their stored
  `position_format` column (migration handles this; see §5.1).

### 3.2 Ambiguity sub-block

The "Use position ambiguity" checkbox + the level dropdown appear only
when the selected format is **Uncompressed** or **Mic-E**. When
**Compressed** is selected the checkbox is hidden entirely (not
disabled; an unsettable control is worse than no control).

Level dropdown - intent-first labels per operator pick, with the
distance hint following the operator's current alt-unit preference
(feet vs metres, mirrors the unit toggle already in the form):

| Stored value | Label (feet preference) | Label (metres preference) | APRS101 meaning |
|--|--|--|--|
| 1 | Block (~600 ft) | Block (~185 m) | nearest 1/10 minute |
| 2 | Neighborhood (~1 mi) | Neighborhood (~1.85 km) | nearest minute |
| 3 | Town (~11 mi) | Town (~18.5 km) | nearest 10 minutes |
| 4 | Region (~69 mi) | Region (~111 km) | nearest degree |

Unchecking "Use position ambiguity" sets stored ambiguity to 0; checking
it defaults the dropdown to level 1 (Block).

### 3.3 Conditional hides when format=Mic-E

Mic-E preempts two existing form controls. When the operator selects
Mic-E:

- **Destination** input is hidden. The encoder auto-computes the
  destination callsign from lat/lon per transmission. A small hint
  under the format radio reads "Destination is auto-computed for Mic-E."
- **PHG fieldset** (Power / Height / Gain / Directivity) is hidden.
  PHG and Mic-E's mandatory course/speed bytes share the same wire
  slot. If a row has stored PHG values from a previous non-Mic-E save,
  they're preserved in the DB but not emitted; switching back to
  Compressed or Uncompressed re-reveals them with their old values.

### 3.4 No phase-1 "coming soon" footnote

Phase 1 ships the radio with all three options visible *and* the
ambiguity controls fully wired for the Uncompressed branch. The Mic-E
option is selectable in the radio but **the API rejects** saves with
`position_format = mic_e` until Phase 2 lands. The UI shows an inline
warning under the radio when Mic-E is selected:

> "Mic-E TX support is shipping in the next release. Save with
> Uncompressed for now."

and Save is disabled while Mic-E is selected. This is a deliberate
choice over hiding the option entirely: it tells operators the feature
is coming and gives the UI a chance to be exercised before Phase 2.

## 4. Format-by-format wire behavior

This section is the contract the encoder must satisfy. Implementation
lives in `pkg/beacon/encoder.go` (compressed, uncompressed) and a new
`pkg/beacon/mice.go` (Mic-E, Phase 2).

### 4.1 Compressed (unchanged)

Today's behavior. 13-byte base-91 form per APRS101 ch 9, `/A=NNNNNN`
for altitude, PHG when set, comment last. Ambiguity is **not honored**
- the value is silently treated as 0. The DTO validation rejects
saving `ambiguity != 0` with `position_format = compressed` so this
silent drop can't happen via the UI.

### 4.2 Uncompressed + ambiguity (Phase 1)

19-byte `DDMM.hhN/DDDMM.hhW` form. The existing `PositionInfo()` grows
an `ambiguity int` parameter; the lat/lon strings are post-processed by
the same `applyAmbiguity` helper already in `pkg/aprs/position.go` (the
parser uses it for round-trip canonicalization). Spaces replace the
trailing digits per APRS101 table 8:

```
ambiguity=1: DDMM.h N/DDDMM.h W  (1/10 minute blanked)
ambiguity=2: DDMM.  N/DDDMM.  W  (1 minute blanked)
ambiguity=3: DDM .  N/DDDM .  W  (10 minutes blanked)
ambiguity=4: DD  .  N/DDD  .  W  (degree blanked)
```

The dot stays in the string (it's part of the field width). PHG,
altitude, and comment are appended unchanged. CSE/SPD on trackers
continues to take the PHG slot.

### 4.3 Mic-E (Phase 2)

New file `pkg/beacon/mice.go` with `MicEPositionInfo(...)` returning
the info field and `MicEDestination(lat float64) ax25.Address`
returning the destination AX.25 address. The encoder produces an APRS101
ch 10 Mic-E packet:

- **Destination callsign** (6 chars): encoded from latitude digits plus
  high-bit flags for N/S, longitude offset >100, and E/W. Built by
  calling `aprs.EncodeMicEDest(lat, msgCode=0 /* Off Duty */,
  lonOffset100, westLong)`. The function is already in tree and tested.
- **Info field**:
  - Data type indicator: `'` (no messaging) or backtick (messaging).
    The existing `Beacon.Messaging` flag drives the choice; matches
    APRS101 §10 table.
  - Bytes 0..2: longitude degrees / minutes / hundredths, range-shifted
    per the lonOffset100 hemisphere logic.
  - Bytes 3..5: speed and course packed into the APRS101 base-10
    triplet (the same layout the existing parser decodes in
    `pkg/aprs/mice.go`). Source is the same fix-derived course/speed
    used today for compressed-tracker (`fix.Heading`, `fix.Speed`).
    For Position and IGate types (no fix-derived motion), course = 0
    and speed = 0.
  - Byte 6: symbol code (single byte, e.g. `>` for car).
  - Byte 7: symbol table (`/` or `\`).
  - Optional altitude extension after byte 7: `}xxx` 4-byte base-91
    form per APRS101 ch 10, omitted when altitude is 0.
  - Comment appended verbatim.
- **Ambiguity**: applied identically to uncompressed - replace trailing
  positions in the latitude *digits* (which live in the destination
  callsign) with the Mic-E "space" variants (`K`, `L`, `Z`) per
  APRS101 ch 10. The destination encoder grows an `ambiguity int`
  parameter; the longitude info bytes blank the same number of trailing
  positions using ASCII space `0x20` (which Mic-E parsers already handle
  in `pkg/aprs/mice.go`).

PHG is dropped silently when format=Mic-E (see §1 architecture). A
debug-level log entry fires once at construction time so operators who
configured PHG and then switched to Mic-E can find the explanation.

### 4.4 Type-by-type matrix

| Type | Compressed | Uncompressed | Mic-E |
|--|--|--|--|
| position | yes | yes | yes |
| tracker | yes | yes | yes |
| igate | yes | yes | yes |
| object | n/a (always `;`) | n/a | n/a |
| custom | n/a (raw passthrough) | n/a | n/a |

For Object and Custom, the format radio is hidden in the UI and the
DTO ignores `position_format` on read.

## 5. Schema and migration

### 5.1 New column

```go
// pkg/configstore/models.go - Beacon struct
PositionFormat string `gorm:"not null;default:'compressed'" json:"position_format"`
// allowed: "compressed" | "uncompressed" | "mic_e"
```

The existing `Ambiguity uint32` column stays as-is (default 0, range
0..4).

The existing `Compress bool` column is **removed** by the migration
(§5.2). After this change, `Beacon` no longer carries a `Compress`
field at any layer.

### 5.2 Migration

New entry in `pkg/configstore/migrate.go`'s migration table:

```go
{version: 2, name: "beacon_position_format", phase: postAutoMigrate,
 run: migrateBeaconPositionFormat},
```

```go
func migrateBeaconPositionFormat(tx *gorm.DB) error {
    // GORM auto-migrate has just added position_format with default
    // 'compressed'. Re-derive it from the soon-to-be-dropped compress
    // column so existing data round-trips.
    if err := tx.Exec(
        `UPDATE beacons SET position_format = 'uncompressed' WHERE compress = 0`,
    ).Error; err != nil {
        return err
    }
    // 'compressed' is already the default; explicit update is a no-op
    // but kept for clarity if the default ever changes.
    if err := tx.Exec(
        `UPDATE beacons SET position_format = 'compressed' WHERE compress = 1`,
    ).Error; err != nil {
        return err
    }
    return tx.Exec(`ALTER TABLE beacons DROP COLUMN compress`).Error
}
```

`user_version` becomes 2, gated by the same idempotency mechanism that
protects the v1 migration (see `migrate_test.go`).

### 5.3 Migration test

Mirror the existing v1 test pattern in `migrate_test.go`:

1. Open the store at the pre-v2 schema (via raw SQL insertion of a
   `compress=0` row before the migration table is consulted).
2. Re-open at the current schema; assert the migration runs.
3. Assert `position_format='uncompressed'` for that row, `='compressed'`
   for a sibling row that had `compress=1`, and that the `compress`
   column no longer exists in `pragma_table_info('beacons')`.
4. Re-open a third time; assert the migration does **not** re-run
   (user_version gate works).

### 5.4 DTO validation

`pkg/webapi/dto/beacon.go` validates on Create and Update:

```
position_format must be one of "compressed" | "uncompressed" | "mic_e"
position_format == "compressed" => ambiguity must be 0
ambiguity must be in 0..4
type ∉ {position, tracker, igate} => position_format ignored on write,
    not returned on read (kept at default in DB)
```

**Phase 1 extra:** `position_format == "mic_e"` returns 400 with
`message: "Mic-E TX is not yet supported; coming in the next release"`.
Phase 2 removes that check.

## 6. Code surface

### 6.1 Phase 1 file map

| File | Change |
|--|--|
| `pkg/configstore/models.go` | Add `PositionFormat string`; remove `Compress bool` |
| `pkg/configstore/migrate.go` | Add `migrateBeaconPositionFormat` and table entry |
| `pkg/configstore/migrate_test.go` | Add the test described in §5.3 |
| `pkg/beacon/types.go` | Replace `Compress bool` with `Format string` ("compressed"/"uncompressed"/"mic_e"); add `Ambiguity int` |
| `pkg/beacon/builder.go` | Replace `if b.Compress` switch with `switch b.Format`; pass `b.Ambiguity` into `PositionInfo`; reject "mic_e" with `fmt.Errorf` for Phase 1 |
| `pkg/beacon/encoder.go` | Grow `PositionInfo` signature: `... ambiguity int, ...`; post-process lat/lon strings with `applyAmbiguity` helper |
| `pkg/beacon/encoder_test.go` | Cover ambiguity levels 1..4 in `PositionInfo`; cover ambiguity=0 (no change) regression |
| `pkg/webapi/dto/beacon.go` | Add `PositionFormat string`; remove `Compress bool`; validation per §5.4 |
| `pkg/webapi/dto/beacon_test.go` | Validation tests for the new rules |
| `pkg/webapi/docs/...` (swagger gen) | Regenerated via `make swagger`; verify the diff matches the new field set |
| `web/src/api/generated/api.d.ts` | Regenerated via the same |
| `web/src/routes/Beacons.svelte` | Add format radio + ambiguity sub-block; conditional hide of Destination and PHG on Mic-E; the "Mic-E coming soon" inline warning + Save-disable |
| `pkg/releasenotes/notes.yaml` | New entry for the release that ships Phase 1 (§9.1 has the text) |

`pkg/beacon/scheduler.go` is **not** touched - the scheduler asks
`buildInfo` for the info string and the builder absorbs the format
selection.

### 6.2 Phase 2 file map

| File | Change |
|--|--|
| `pkg/beacon/mice.go` *(new)* | `MicEPositionInfo(...)` and `MicEDestination(...)` |
| `pkg/beacon/mice_test.go` *(new)* | Round-trip tests against `pkg/aprs/mice.go` parser (encode, then parse, assert lat/lon/course/speed/altitude/ambiguity match within tolerance); also a corpus test against known APRS101 example packets |
| `pkg/beacon/builder.go` | Remove the Phase 1 "mic_e not yet supported" rejection; wire `MicEPositionInfo` and `MicEDestination` |
| `pkg/beacon/scheduler.go` | The frame-building path overrides `Beacon.Dest` with the computed Mic-E destination when format=mic_e. (This is the *only* scheduler change in the whole plan.) |
| `pkg/webapi/dto/beacon.go` | Remove the Phase 1 mic_e rejection |
| `web/src/routes/Beacons.svelte` | Remove the "coming soon" warning; un-disable Save |
| `pkg/releasenotes/notes.yaml` | New entry for the Phase 2 release |

### 6.3 Helper reuse

`pkg/beacon/encoder.go` does not duplicate the ambiguity-replacement
logic. It calls into a new exported helper:

```go
// pkg/aprs/position.go
func ApplyLatLonAmbiguity(latStr, lonStr string, level int) (string, string)
```

Built as a thin wrapper around the existing private
`applyAmbiguity(b []byte, amb int, positions []int)` (used today by
parser-side `formatLatitude` / `formatLongitude`). The wrapper handles
the lat-vs-lon position-index difference and string-vs-byte conversion
so the beacon encoder doesn't have to know either. The private helper
stays private; only the new high-level wrapper is exported. A unit
test pinning the input/output strings for levels 0..4 prevents
regression.

## 7. Testing strategy

### 7.1 Phase 1

- **Go unit tests** in `pkg/beacon/encoder_test.go`: round-trip
  `PositionInfo(lat, lon, ..., ambiguity)` then parse with
  `aprs.Parse`; assert the parsed `Position.Ambiguity` matches and the
  parsed lat/lon lies within the expected blanked box.
- **Migration test** per §5.3.
- **DTO validation table-test** per §5.4 covers every combination of
  `position_format` x `ambiguity`.
- **UI smoke** via Playwright: open the Beacons form, switch the radio
  through all three options, verify the ambiguity sub-block appears /
  disappears, verify the Mic-E "coming soon" warning + Save-disable.

### 7.2 Phase 2

- **Round-trip corpus test** in `mice_test.go`: a set of fixture
  positions (varied hemisphere, equator, prime meridian, antipodes,
  trackers with motion, fixed stations, every ambiguity level 0..4)
  encoded via `MicEPositionInfo` + `MicEDestination`, then parsed back
  via the existing `pkg/aprs/mice.go` parser. Lat/lon must match within
  the precision allowed by the current ambiguity level; course/speed
  must match exactly; altitude must match within 1 foot
  (base-91 quantization).
- **Hand-encoded fixture** for the canonical APRS101 ch 10 example
  packet ("Stealth Cougar"): assert the encoder reproduces it byte for
  byte.
- **On-air verification before Phase 2 ships** (operator gate, not a
  test): operator transmits a Mic-E beacon on a known channel, captures
  with another graywolf and a non-graywolf receiver (e.g. APRSdroid,
  YAAC), confirms both decode it correctly. This is the same gate
  channel-CW-ID went through.

### 7.3 Regression budget

- `pkg/beacon` existing tests must continue to pass unchanged after
  Phase 1 (only `PositionInfo` signature changes, so call sites in
  the same file/test update).
- `pkg/aprs` tests untouched after the `applyAmbiguity` rename - the
  rename includes the test file in the same change.

## 8. Failure modes and observability

- **Bad format string in DB** (e.g. operator hand-edited SQLite):
  `buildInfo` returns an error, the scheduler logs the error at WARN
  via the existing `OnEncodeError` Observer hook, and the beacon is
  dropped on the floor for that tick. Existing behavior.
- **Ambiguity out of range** in DB (manual SQL corruption): clamped to
  4 at the encoder boundary and a WARN log fires once per beacon ID.
- **Mic-E TX attempted in Phase 1 via DB bypass**: same dropped-with-
  WARN behavior; the DTO validation is the primary guard, the encoder
  is the belt-and-suspenders backstop.
- **PHG silently dropped with Mic-E**: DEBUG log entry per beacon at
  scheduler-add time so operators searching for "why isn't my PHG
  showing up" find it.

## 9. Rollout

### 9.1 Phase 1 release

Shipped as a patch release (`v0.13.12` or whichever is current at
merge time). Release note entry:

```yaml
- version: 0.13.12
  date: 2026-XX-XX
  changes:
    - style: info
      text: |
        Beacons: pick the position report format per beacon
        (Compressed, Uncompressed, or Mic-E coming soon). Uncompressed
        beacons can now use position ambiguity to publish a coarser
        position - useful for privacy or for group meetups where
        address-level precision isn't wanted. Pick the level (Block,
        Neighborhood, Town, Region) in the beacon form.
      route: /#/beacons
```

No operator action required. Existing beacons keep transmitting the
exact same compressed packets they always did; the only visible
change is the new radio in the form, defaulted to their current
choice.

### 9.2 Phase 2 release

Patch release that unblocks the Mic-E radio option. Release note:

```yaml
- version: 0.13.13
  date: 2026-XX-XX
  changes:
    - style: cta
      text: |
        Beacons: Mic-E transmit is now supported. It's the most
        efficient APRS position format and the standard for mobile
        and tracker stations. If you have a tracker beacon, consider
        switching its format to Mic-E in the beacon form.
      route: /#/beacons
```

The `cta` style on this one is deliberate: trackers benefit most from
the switch and a nudge gets operators to actually use the new format.

### 9.3 No backport, no flag

Phase 1 is small enough that flagging it would add more code than the
feature itself. Phase 2's risk is contained by the API rejection in
Phase 1: even if Phase 2 ships with a Mic-E encoder bug, only
operators who *opt in* to Mic-E experience it, and they revert to
Uncompressed in two clicks.

## 10. Open questions deferred to implementation

These are small enough that the implementation plan / executor should
decide rather than re-litigating in the spec:

- The exact label text for the format radio options (current draft:
  "Compressed (highest precision)" / "Uncompressed (standard
  precision)" / "Mic-E (most efficient)" - matches operator wireframe).
- Whether the Mic-E "coming soon" warning sits inside the radio group
  or in a separate alert below it. Either is fine; pick whichever
  matches the project's existing inline-warning pattern.
- The DEBUG log message wording for "PHG dropped when format=mic_e".

## 11. Phasing summary

| Phase | What ships | Gates Phase 2 |
|--|--|--|
| Phase 1 | Schema migration + new column. Format radio in UI (all three options visible). Uncompressed ambiguity wire support. Mic-E option visible but Save-disabled with inline warning. API rejects mic_e on write. | none - ships independently |
| Phase 2 | `pkg/beacon/mice.go` encoder. API stops rejecting mic_e. UI removes warning and re-enables Save. Round-trip test corpus passes. On-air verification by operator before merging. | Phase 1 merged |

Each phase is one PR. Phase 1 should close
[#146](https://github.com/chrissnell/graywolf/issues/146); Phase 2 is a
separate operator-facing feature with its own release note.
