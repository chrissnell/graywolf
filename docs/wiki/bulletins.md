# Bulletin board

APRS supports a shared bulletin board where any station can post short
public messages. Bulletins differ from directed messages in three ways:
they are addressed to a slot name rather than a callsign, they carry no
message ID and expect no ACK, and they are retransmitted on a scheduled
cycle rather than via retry-until-ACK.

## Slot taxonomy

| Format | Type | Initial burst | Stable interval | Max sends | Expiry (inbound) |
|---|---|---|---|---|---|
| `BLN0`–`BLN9` | Bulletin | 3 × 30 s | 20 min | 12 (~4 h) | 4 h |
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

The `Scheduler` runs a goroutine that:
- Polls every 15 seconds (`schedulerPollInterval`) and calls
  `Store.ListPendingSends` to find rows where
  `next_send_at <= now AND send_count < max_sends`.
- For each due row, calls `Sender.Send` which:
  - Encodes the bulletin as an APRS UI frame and submits to `txgovernor`
    at `PriorityBeacon` priority using the digipeater path from
    `MessagePreferences.DefaultPath` (operator-configurable in the
    Messaging settings page; defaults to `WIDE1-1,WIDE2-1`).
  - If an iGate sender is wired, also forwards directly to APRS-IS using
    TNC2 format with path `TCPIP*`, so the bulletin appears on aprs.fi
    regardless of whether a nearby iGate hears the RF. When IS is not
    configured, `ErrNotEnabled` is silently swallowed; the RF send always
    completes first and is unaffected.
- Increments `send_count` and advances `next_send_at`:
  - `send_count < BulletinBurstCount` (3): next in `BulletinBurstInterval` (30 s)
  - `send_count >= BulletinBurstCount`: uses the per-bulletin `interval_mins`
    set at compose time (0–20). When 0, `next_send_at` is cleared and no further
    retransmits are scheduled (burst-only mode). Default in the compose form: 20.
  - Announcements always use `AnnouncementInterval` (1 h), no burst phase.
- Responds to `Kick()` for immediate processing without waiting for the
  next tick.

When `send_count == max_sends` the row is exhausted and the scheduler
ignores it. Soft-deleting a row (via the UI or DELETE REST endpoint) also
stops future retransmits immediately, since `ListPendingSends` excludes
soft-deleted rows.

## Database (migrations 26–27)

Table `bulletins` columns:

| Column | Purpose |
|---|---|
| `id` | PK |
| `direction` | `'in'` or `'out'` |
| `slot` | `BLN0`–`BLN9` or `BLNA`–`BLNZ` |
| `from_call` | Source callsign (inbound) |
| `text` | Bulletin content |
| `source` | `'rf'` or `'is'` |
| `channel` | RX channel index |
| `raw_tnc2` | TNC-2 wire representation |
| `is_announcement` | Bool derived from slot letter |
| `expires_at` | When the UI should stop showing the row |
| `unread` | True until the operator dismisses |
| `send_count` | How many times this outbound row has been sent |
| `max_sends` | Send limit (12 for bulletins, 96 for announcements) |
| `next_send_at` | When the scheduler should next send this row |
| `created_at`, `updated_at`, `deleted_at` | GORM timestamps; soft-delete |

Indexes:
- `idx_bulletin_inbound_slot` — partial unique on `(from_call, slot)` WHERE
  `direction='in' AND deleted_at IS NULL` (enforces one active row per
  station/slot; SQLite partial index, cannot be used as ON CONFLICT target).
- `idx_bulletin_direction` — on `direction` for List queries.
- `idx_bulletin_next_send_at` — on `next_send_at` for scheduler queries.

## REST endpoints

All endpoints in `pkg/webapi/bulletins.go`. The server returns 503 until
`SetBulletinService` has been called (same pattern as other optional services).

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/bulletins` | List; `?direction=in\|out`, `?unread_only=true` |
| `POST` | `/api/bulletins` | Create outbound bulletin (`{slot, text}`) → 201 |
| `DELETE` | `/api/bulletins/{id}` | Soft-delete; 404 on missing row |
| `POST` | `/api/bulletins/{id}/read` | Mark single bulletin read |
| `POST` | `/api/bulletins/read-all` | Mark all inbound bulletins read |

DTOs live in `pkg/webapi/dto/bulletins.go` (`BulletinResponse`,
`SendBulletinRequest`). Slot validation (`validBulletinSlot`) and text
length check live in `SendBulletinRequest.Validate()`.

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

Digipeater path for outbound bulletins (and messages) is configurable in the
Messaging settings page (`MessagePreferences.DefaultPath`). The default is
`WIDE1-1,WIDE2-1`, suitable for most fixed stations.

The **retransmit interval** is set per-bulletin in the compose form on the
Bulletins page (`Bulletins.svelte`). The **Every N min** field defaults to
20 (APRS spec for 2-hop stations). Set to 0 to send the burst only with no
further retransmits. Range: 0–20. The value is stored as `interval_mins` on
the `bulletins` row (migration 28 adds the column with a DB default of 20
and backfills any pre-existing rows). Announcements (BLNA-Z) always use
1-hour retransmits regardless of this field.

Route registered in `web/src/App.svelte` as `'/bulletins': Bulletins`.
API client in `web/src/api/bulletins.js`.
