# Digipeater Source Block List

**Date:** 2026-05-29
**Status:** Design approved, ready for planning
**Scope:** Add a per-source block list to the digipeater engine. Operators
can name stations (with or without SSID, with a trailing-SSID wildcard)
whose frames must not be digipeated.

---

## 1. Goal and invariant

Operators occasionally need to silence a misbehaving station from the
digipeater (malformed beacons, abusive flooding, etc.) without losing
the ability to see, log, or iGate that station's traffic.

This design adds a **Digipeater Block List**: a global (channel-agnostic)
deny list of source-address patterns. When the digipeater engine
receives a frame whose `Source` matches an enabled entry, the engine
drops the frame for digipeating only and returns without consulting any
rule, mutating any path, or invoking the TX submit.

**Hard invariant -- block list is digipeater-only.** The check lives
strictly inside `pkg/digipeater/digipeater.go`'s `Handle`. No other RX
consumer reads the block list: the iGate (RF->IS and IS->RF), packetlog,
dashboard live feed, station cache, messages router, and AGW/KISS
fanouts all keep receiving the frame as today. Any future code path
that wants similar behavior must add its own list -- sharing is
forbidden by convention and enforced by a test.

## 2. Out of scope

- Per-channel scoping (block list is global).
- Auto-expiry / time-bounded blocks.
- CreatedAt/UpdatedAt audit timestamps.
- Glob, regex, or anywhere-wildcards.
- Bulk import/export.
- Allow list / positive-only digipeater mode.
- Per-pattern statistics breakdown (only an aggregate `Blocked` counter).
- Packet-log annotation for blocked frames.

## 3. Data model

New GORM model in `pkg/configstore/models.go`:

```go
type DigipeaterBlocklist struct {
    ID      uint32 `gorm:"primaryKey;autoIncrement" json:"id"`
    Pattern string `gorm:"not null;uniqueIndex" json:"pattern"`
    Reason  string `json:"reason"`
    Enabled bool   `gorm:"not null;default:true" json:"enabled"`
}
```

- `Pattern` is stored uppercase and canonicalized at the API boundary
  (see Section 4).
- `Pattern` carries a unique index; duplicate inserts are rejected at
  the DB and surface as 409 in the API.
- No `CreatedAt`/`UpdatedAt` columns.
- Registered in the auto-migrate slice at `pkg/configstore/store.go:152`.

## 4. Pattern syntax and matcher

Three accepted forms, mirroring the igate filter convention already in
the codebase (`pkg/igate/filters/filters.go:matchPattern`):

| Pattern form | Canonical shape | Matches when |
|---|---|---|
| `CALL` (no `-`) | `Call=CALL, SSID=0, wild=false` | `src.Call==CALL && src.SSID==0` |
| `CALL-N` (N is 0-15) | `Call=CALL, SSID=N, wild=false` | `src.Call==CALL && src.SSID==N` |
| `CALL-*` | `Call=CALL, SSID=0, wild=true` | `src.Call==CALL && src.SSID>0` |

Notes:

- `N1ROG` matches the frame source `N1ROG` only -- *not* `N1ROG-9`.
- `N1ROG-*` matches `N1ROG-1` through `N1ROG-15`, *not* bare `N1ROG`.
- Matching is case-insensitive throughout (mirrors `addressEqual` in
  `digipeater.go`).
- `CALL-0` and bare `CALL` are equivalent under AX.25 SSID semantics but
  stored distinctly so the operator's intent round-trips. Both match the
  same set of frames.

### 4.1 Matcher package

New package `pkg/digipeater/blocklist/` with two files:

```go
// blocklist.go
package blocklist

type Entry struct {
    Pattern string  // uppercase canonical form
    Reason  string
}

type List struct {
    mu      sync.RWMutex
    entries []Entry
}

func New(entries []Entry) *List
func (l *List) Set(entries []Entry)
func (l *List) Matches(src ax25.Address) (Entry, bool)
```

- `Matches` does a linear scan; first hit wins. Block lists are tiny in
  practice (single-digit to low-double-digit entries); no indexing.
- The returned `Entry` is used for the debug log line so operators see
  which pattern (and reason) fired.

```go
// validate.go
func ValidatePattern(s string) (canonical string, err error)
```

`ValidatePattern` rejects:

- empty or whitespace-only
- lone `*`, lone `-`, lone `-*` (flooding guard)
- callsign part empty
- callsign part longer than 6 characters
- callsign part containing `*` (we only allow trailing-SSID wildcard)
- SSID outside `0..15`
- anything else AX.25 disallows in a callsign

On success it returns the uppercase, trimmed canonical form so storage
and matching agree byte-for-byte.

## 5. Engine integration

In `pkg/digipeater/digipeater.go`:

### 5.1 Config additions

```go
type Config struct {
    // ... existing fields ...

    // Blocklist names source addresses that must never be digipeated.
    // Replaced under lock via SetBlocklist for live reconfig. Nil/empty
    // means no block list (current behavior).
    Blocklist []blocklist.Entry
}
```

### 5.2 Digipeater struct additions

```go
type Digipeater struct {
    // ... existing fields ...
    blocklist *blocklist.List
}
```

`New()` constructs `d.blocklist = blocklist.New(cfg.Blocklist)`.

### 5.3 New public method

```go
func (d *Digipeater) SetBlocklist(entries []blocklist.Entry) {
    d.blocklist.Set(entries)
}
```

### 5.4 Stats addition

```go
type Stats struct {
    Packets uint64
    Deduped uint64
    Blocked uint64 // new
}
```

### 5.5 Hook placement in `Handle`

The block-list check runs **immediately after** the existing packet-mode
short-circuit and mycall guard (`digipeater.go:259-264`), and **before**
the dedup block (`digipeater.go:266-283`).

Rationale: blocked sources must not consume dedup-window memory, and
the dedup counter is reserved for legitimate-but-duplicate frames -- a
blocked source is neither digipeated nor a true duplicate.

```go
// Block-list check. Source-address deny list, digipeater-scope only.
// Runs before dedup so blocked frames don't churn the dedup window.
if entry, hit := d.blocklist.Matches(frame.Source); hit {
    d.mu.Lock()
    d.stats.Blocked++
    d.mu.Unlock()
    d.logger.Debug("digipeater: source blocked",
        "source", frame.Source.String(),
        "pattern", entry.Pattern,
        "reason", entry.Reason)
    return false
}
```

That is the entire engine change. No other RX path touches the list.

## 6. Configstore CRUD

New methods on `*Store` in `pkg/configstore/store.go`, alongside the
existing `ListDigipeaterRules`/`CreateDigipeaterRule`/etc. block:

```go
func (s *Store) ListDigipeaterBlocklist(ctx context.Context) ([]DigipeaterBlocklist, error)
func (s *Store) GetDigipeaterBlocklistEntry(ctx context.Context, id uint32) (*DigipeaterBlocklist, error)
func (s *Store) CreateDigipeaterBlocklistEntry(ctx context.Context, e *DigipeaterBlocklist) error
func (s *Store) UpdateDigipeaterBlocklistEntry(ctx context.Context, e *DigipeaterBlocklist) error
func (s *Store) DeleteDigipeaterBlocklistEntry(ctx context.Context, id uint32) error
```

`ListDigipeaterBlocklist` orders by `id` ascending. The unique index on
`Pattern` makes Create/Update reject duplicates via DB error; the webapi
layer maps this to HTTP 409.

## 7. Wiring (live reconfig)

The wiring layer that already calls `digipeater.SetRules` on settings
save must also call `digipeater.SetBlocklist`. Concretely:

- Add a parallel helper next to `RulesFromStore` in `digipeater.go`:

  ```go
  func BlocklistFromStore(rows []configstore.DigipeaterBlocklist) []blocklist.Entry {
      out := make([]blocklist.Entry, 0, len(rows))
      for _, r := range rows {
          if !r.Enabled {
              continue
          }
          out = append(out, blocklist.Entry{Pattern: r.Pattern, Reason: r.Reason})
      }
      return out
  }
  ```

- Call sites: initial wire-up at startup (set on `New`'s `Config`), and
  the existing reconfig signal that fires when the operator saves
  digipeater settings -- broadcast a "blocklist changed" event too, then
  call `SetBlocklist` on the engine. The exact signal name and broadcast
  surface follow the same pattern `SetRules` already uses.

## 8. REST API

New endpoints in `pkg/webapi/digipeater.go` alongside the existing rule
endpoints:

| Method | Path | Description |
|---|---|---|
| `GET`    | `/digipeater/blocklist`       | List all entries, id-ascending |
| `POST`   | `/digipeater/blocklist`       | Create entry (validate pattern -> canonical) |
| `PUT`    | `/digipeater/blocklist/{id}`  | Update entry |
| `DELETE` | `/digipeater/blocklist/{id}`  | Delete entry |

### 8.1 DTOs

In `pkg/webapi/dto/digipeater.go`:

```go
type BlocklistEntryRequest struct {
    Pattern string `json:"pattern"`
    Reason  string `json:"reason"`
    Enabled *bool  `json:"enabled,omitempty"` // nil = true on create
}

type BlocklistEntryResponse struct {
    ID      uint32 `json:"id"`
    Pattern string `json:"pattern"`
    Reason  string `json:"reason"`
    Enabled bool   `json:"enabled"`
}
```

### 8.2 Validation chain on POST and PUT

1. `blocklist.ValidatePattern(req.Pattern)` -> canonical form or 400
   with `{"error":"<reason>"}`.
2. `Reason` trimmed and capped at 256 chars.
3. On DB unique-index violation -> 409 with
   `{"error":"pattern already exists"}`.

After any successful mutation, the existing digipeater-reconfig signal
fires so the wiring layer calls `SetBlocklist` on the live engine.

### 8.3 OpenAPI

Add op-IDs in `pkg/webapi/docs/op_ids.go`. Regenerate Swagger and the TS
client via `make swagger` (or the project's standard target). The
docs-lint test will fail until both regeneration steps are committed.

## 9. UI

`web/src/routes/Digipeater.svelte` gets a new section **below** the
existing Rules table, titled **"Blocked Stations"**.

### 9.1 Layout (mockup)

```
+---------------------------------------------------------+
| Blocked Stations                                        |
| Frames from these stations will not be digipeated.      |
| The iGate, packet log, and other receivers are          |
| unaffected.                                             |
|                                                         |
| +-Pattern----+-Reason-------------+-Enabled-+----------+ |
| | N1ROG-*    | Malformed beacons  |   [v]   | Edit Del | |
| | KK6XYZ-9   |                    |   [v]   | Edit Del | |
| +------------+--------------------+---------+----------+ |
|                                                         |
| [ + Add blocked station ]                               |
+---------------------------------------------------------+
```

### 9.2 Add and edit modal

Uses the existing `FormField`, `Modal`, and `Select` components:

- `Pattern` text input with inline hint:
  `e.g. N1ROG, N1ROG-9, or N1ROG-*`
- `Reason` text input (optional, free text, 256-char cap)
- `Enabled` toggle (defaults on)

Client-side mirror of `ValidatePattern` for instant feedback; the server
is the authority and is what produces the error message displayed on
submit.

### 9.3 Row interactions

- The `Enabled` toggle saves immediately on flip (auto-save per
  `feedback_master_toggle_autosave`); toggling the column does not open
  the edit modal.
- Delete uses a plain red confirm button (no type-the-name gate, per
  `feedback_no_typed_name_delete_gate`).
- After any successful mutation the page refetches `/digipeater/blocklist`.

### 9.4 File placement

The new section lives inside `Digipeater.svelte` directly. The file is
currently 854 lines; if adding the section pushes it past ~1100 lines
(or if review surfaces complexity), extract the section to
`web/src/components/digipeater/BlockedStationsTable.svelte`. Otherwise
inline is fine and matches the existing structure.

## 10. Tests

### 10.1 Go - `pkg/digipeater/blocklist/`

- `validate_test.go`: table-driven valid/invalid patterns,
  canonicalization, all three forms, edge cases (`*`, `-`, `-*`, empty,
  oversized SSID, lowercase canonicalized to uppercase).
- `blocklist_test.go`: `Matches` table -- base-call vs SSID forms,
  wildcard vs bare base, multiple entries with first-hit semantics, case
  insensitivity.

### 10.2 Go - `pkg/digipeater/digipeater_test.go`

- `TestHandle_BlockedSourceShortCircuits`: block list has `N1ROG-*`,
  frame with source `N1ROG-9` and path `WIDE2-2`. `Handle` returns
  false, `Stats.Blocked == 1`, `Stats.Packets == 0`, `Stats.Deduped == 0`,
  submit never called, dedup window unchanged (assert by sending a
  second identical frame and confirming `Stats.Deduped` still 0).
- `TestHandle_BlockedSourceBeforeDedup`: assert block check precedes
  dedup by sending a frame that would have hit dedup if not blocked.
- `TestHandle_NonBlockedFrameStillDigis`: regression -- block list with
  `OTHERCALL-*` does not affect `N1ROG-9`.
- `TestSetBlocklist`: live reconfig replaces atomically; a frame that
  was blocked before reconfig is allowed after, and vice versa.

### 10.3 Go - iGate isolation test (the hard invariant)

In `pkg/igate/` or the closest existing wiring-layer test fixture: feed
a frame whose source is on the digipeater block list into the RX
fanout and assert the iGate's RF->IS path still gates it. This is the
regression test that protects the digipeater-only invariant.

### 10.4 Go - `pkg/configstore/store_test.go`

- CRUD round-trip on `DigipeaterBlocklist`.
- Unique-index conflict on duplicate pattern returns the GORM
  unique-violation error.

### 10.5 Go - `pkg/webapi/digipeater_test.go`

- POST happy path returns the canonicalized pattern.
- POST with bad pattern returns 400 with the validator's error string.
- POST with duplicate pattern returns 409.
- GET/PUT/DELETE round-trip.

### 10.6 Frontend

Skip unless `Digipeater.svelte` already has a component-test harness in
this branch; the existing pattern in the repo wins.

## 11. Wiki and invariants updates

Same PR.

### 11.1 `docs/wiki/code-map.md`

Extend the existing `digipeater` row's blurb:

> `digipeater` -- WIDEn-N / TRACEn-N digipeater with preemptive digi,
> per-channel dedup, and a source-address block list
> (digipeater-only). [handbook/digipeater.html](../handbook/digipeater.html)

Add a new code-map line pointing at `pkg/digipeater/blocklist/`.

### 11.2 `docs/wiki/invariants.md`

New invariant:

> **Digipeater block list is digipeater-only.**
> `pkg/digipeater/blocklist` is consulted only by
> `pkg/digipeater/digipeater.go`'s `Handle`. Frames whose source
> matches an enabled entry are not digipeated; every other RX consumer
> (iGate, packetlog, dashboard, station cache, messages, AGW/KISS
> fanouts) is unaffected and still sees the frame.
>
> *Why:* operators commonly want to silence a misbehaving station's
> digipeated copies without losing the ability to see, log, or gate
> that station's original RF or APRS-IS appearances.
>
> *How to apply:* never read the block list outside `pkg/digipeater/`.
> If another subsystem grows a similar need, give it its own list.

## 12. Release note

Prepend an `info` entry to `pkg/releasenotes/notes.yaml` against the
next patch version. Plain ASCII only.

```yaml
- version: vX.Y.Z
  date: 2026-05-29
  style: info
  title: Block a station from being digipeated
  body: |
    The Digipeater settings page now has a Blocked Stations list.
    Add a callsign (with or without SSID, wildcards like N1ROG-*
    accepted) and the digipeater will stop repeating that station's
    frames. The iGate and packet log are unaffected -- you'll still
    see and log the original traffic.
```

## 13. Migration and rollout

- New table; no data migration. Existing installs auto-migrate on
  startup and come up with an empty block list (no behavior change).
- Live reconfig fires on first save, same plumbing as the existing
  rules table.
- Downgrade: the new table is left in place; older binaries ignore it.

## 14. File-by-file summary

| File | Change |
|---|---|
| `pkg/digipeater/blocklist/blocklist.go` | new -- `Entry`, `List`, `New`, `Set`, `Matches` |
| `pkg/digipeater/blocklist/validate.go` | new -- `ValidatePattern` |
| `pkg/digipeater/blocklist/blocklist_test.go` | new |
| `pkg/digipeater/blocklist/validate_test.go` | new |
| `pkg/digipeater/digipeater.go` | add `Config.Blocklist`, `Digipeater.blocklist`, `SetBlocklist`, `Stats.Blocked`, hook in `Handle`, `BlocklistFromStore` helper |
| `pkg/digipeater/digipeater_test.go` | new tests for block path |
| `pkg/configstore/models.go` | add `DigipeaterBlocklist` |
| `pkg/configstore/store.go` | register model in auto-migrate; add CRUD |
| `pkg/configstore/store_test.go` | CRUD + unique-index test |
| `pkg/webapi/dto/digipeater.go` | add request/response DTOs |
| `pkg/webapi/digipeater.go` | add four handlers, validation, reconfig signal |
| `pkg/webapi/digipeater_test.go` | new endpoint tests |
| `pkg/webapi/docs/op_ids.go` | register new op IDs |
| `pkg/webapi/docs/gen/swagger.{json,yaml}` | regenerated |
| `web/ui` TS client | regenerated |
| `app/wiring/...` (existing digipeater wiring) | call `BlocklistFromStore` + `SetBlocklist` on init and reconfig |
| `web/src/routes/Digipeater.svelte` | add Blocked Stations section + modal |
| `pkg/igate/...` (closest test fixture) | invariant regression test |
| `docs/wiki/code-map.md` | extend digipeater row + new line for `blocklist/` |
| `docs/wiki/invariants.md` | new invariant block |
| `pkg/releasenotes/notes.yaml` | new entry, next patch version |
