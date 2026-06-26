# Bulletin board

APRS supports a shared bulletin board where any station can post short
public messages. Bulletins differ from directed messages in three ways:
they are addressed to a slot name rather than a callsign, they carry no
message ID and expect no ACK, and they are retransmitted on a scheduled
cycle rather than via retry-until-ACK.

## Slot taxonomy

| Format | Type | Initial burst | Stable interval | Max sends | Expiry (inbound) |
|---|---|---|---|---|---|
| `BLN0`–`BLN9` | Bulletin | 3 × 30 s | configurable 1–20 min (default 20) | 12 (~4 h); 3 for burst-only | 4 h |
| `BLNA`–`BLNZ` | Announcement | — | 1 h | 96 (~4 days) | 4 days |

Bulletins fire 3 times at 30-second intervals immediately after creation
(per Bruninga/APRS protocol: new information should be sent rapidly to
survive packet collisions), then settle into the 20-minute Net Cycle Time.

Slot `BLNA` through `BLNZ` are called *announcements*; they carry
longer-lived content (club events, net schedules). All other bulletin
slots (`BLN0`–`BLN9`) are ordinary bulletins.

Text max: 67 characters (APRS101 message-field limit for bulletins).

## Wire format

A bulletin info field looks like:

```
:BLN0     :Net tonight at 2000z
```

The addressee field is left-padded to 9 characters with spaces. The
message body follows the second colon. No `{msgid}` suffix is present,
so no ACK is sent and none is expected.

Graywolf encodes outbound bulletins via `aprs.EncodeMessage(slot, text, "")`.

## Ingest flow (inbound bulletins)

1. `pkg/aprs` parses the AX.25 UI frame. `DecodedAPRSPacket.Message.IsBulletin`
   is set when the addressee matches `BLN[0-9A-Z]`.
2. `pkg/messages/router.go` — step 5 of the classify switch — checks
   `effMsg.IsBulletin`. If a `BulletinSink` is wired, it calls
   `IngestBulletin(ctx, pkt, msg)`; otherwise the packet is counted as
   `not_for_us` and dropped silently.
3. `pkg/bulletins.Service.IngestBulletin` calls `Store.UpsertInbound`.
   The store does a select-then-update/insert keyed on `(from_call, slot)`
   among active inbound rows. Re-heard bulletins update the existing row
   in-place; they do NOT accumulate duplicates.
4. Inbound rows always have `direction='in'` and start with `unread=true`.

The `BulletinSink` interface lives in `pkg/messages/router.go` (not
`pkg/bulletins`) to avoid a circular import. The wiring layer
(`pkg/app/wiring.go`) imports both packages and connects them.

## Outbound send flow

`Service.Send(ctx, SendRequest{Slot, Text})`:
1. Validates slot format (`BLN` + digit or uppercase letter) and text length.
2. Inserts a row with `direction='out'`, `next_send_at=now`, and
   `max_sends` set from the slot type.
3. Calls `Scheduler.Kick()` so the first transmission happens immediately
   rather than waiting for the next tick.

The `Scheduler` goroutine polls `ListPendingSends` and encodes each due
row as an APRS UI frame, submitted via `txgovernor` at `PriorityBeacon`
priority. The digipeater path comes from `MessagePreferences.DefaultPath`
(the station-level setting shared with directed messages; configurable in
the Messaging settings page). When an iGate sender is wired the bulletin
is also forwarded to APRS-IS in TNC2 format (`TCPIP*` path) so it appears
on aprs.fi regardless of RF coverage; RF always fires first and
`ErrNotEnabled` is swallowed when IS is not configured.

The per-bulletin retransmit interval is set at compose time via
`interval_mins` (0 = burst-only, 1–20 min). See `pkg/bulletins/scheduler.go`
for the burst phase logic and `send_count`/`max_sends` lifecycle.

## Database (migrations 27, 29)

Migration 27 (`bulletins_table`) creates the `bulletins` table as raw SQL,
excluded from AutoMigrate — same pattern as actions/remoteactions migrations
15–16. Migration 29 (`bulletin_row_interval`) adds `interval_mins` with
`DEFAULT 20`.

> **Note:** Migration 28 (`bulletin_interval`) adds `bulletin_interval_mins`
> to `messages_preferences` but no Go struct field or code reads it. It is an
> orphaned iteration artifact superseded by the per-row `interval_mins` on the
> `bulletins` table added in migration 29.

Model: `configstore.Bulletin` in `pkg/configstore/models.go`. The partial
unique index `idx_bulletin_inbound_slot` on `(from_call, slot)` WHERE
`direction='in' AND deleted_at IS NULL` enforces one active inbound row per
station/slot; because SQLite partial indexes cannot be used as ON CONFLICT
targets, the upsert logic is handled in Go (`Store.UpsertInbound`).

## REST endpoints

Five endpoints in `pkg/webapi/bulletins.go` (list, create, soft-delete,
mark-read, mark-all-read). The server returns 503 until `SetBulletinService`
has been called — same pattern as other optional services. Paths are listed
in the code-map's `webapi` section. DTOs: `pkg/webapi/dto/bulletins.go`
(`BulletinResponse`, `SendBulletinRequest`). Operator API reference belongs
in the handbook.

## App wiring

`pkg/app/wiring.go` (`wireBulletins`):
- Resolves `ourCall` from `configstore`, `txChannel` from `MessagesConfig`, and
  `txPath` from `MessagePreferences.DefaultPath` (default `WIDE1-1,WIDE2-1`).
- Calls `bulletins.NewService` with `DB`, `TxSink=txgovernor`, `OurCall`,
  `TxChannel`, `Path`, and `IGateSender=a.igateLineSender`.
- Stores result in `App.bulletinSvc`.
- `bulletinsComponent()` is added to `startOrder` (before `messagesComponent`);
  its `Start` calls `Scheduler.Start(ctx)` and its `Stop` calls `Scheduler.Stop()`.
- `wireMessages` passes `BulletinSink: a.bulletinSvc` to `messages.ServiceConfig`.
- HTTP wiring calls `apiSrv.SetBulletinService(a.bulletinSvc)` when non-nil.

## Frontend

`web/src/routes/Bulletins.svelte` — the bulletin board page:
- Compose form with slot selector (`<optgroup>` for bulletins BLN0–9 and
  announcements BLNA–Z) and 67-char-limited text input with character counter.
- Tab switcher: Received / Sent, with unread badge count.
- Mark-all-read button on the Received tab.
- Per-row: slot tag, from_call (inbound) or send status (outbound), timestamp,
  delete button.
- 30-second poll for new inbound data.

Sidebar in `web/src/components/Sidebar.svelte` shows a Bulletins nav item
with an inline SVG clipboard icon (the chonky-ui `rss` icon is not in the
component's allowlist, so `svgIcon: 'bulletins'` is used instead). An unread
badge count is polled every 30 s via `GET /api/bulletins?direction=in&unread_only=true`.

**Digipeater path** is a station-level setting shared by all outbound APRS
traffic — both directed messages and bulletins. It reflects your antenna,
location, and local network topology, so it is set once in the Messaging
settings page (`MessagePreferences.DefaultPath`, default `WIDE1-1,WIDE2-1`)
rather than per-bulletin. `WIDE1-1,WIDE2-1` is appropriate for most fixed
2-hop stations; `WIDE1-1` for portable/mobile.

The **retransmit interval** is set per-bulletin in the compose form on the
Bulletins page (`Bulletins.svelte`). The **Every N min** field defaults to
20 (APRS spec for 2-hop stations). Set to 0 to send the burst only with no
further retransmits. Range: 0–20. The value is stored as `interval_mins` on
the `bulletins` row (migration 28 adds the column with a DB default of 20
and backfills any pre-existing rows). Announcements (BLNA-Z) always use
1-hour retransmits regardless of this field.

Route registered in `web/src/App.svelte` as `'/bulletins': Bulletins`.
API client in `web/src/api/bulletins.js`.
