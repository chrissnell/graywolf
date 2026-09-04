# New-activity notifications

Clickable popup notifications for new DMs/tactical messages, new
bulletins, station Emergency status, newly heard stations, and favorite
stations, plus the unread-badge lag fix that motivated the original
(message/bulletin/emergency) change. A persisted **Notifications Log**
page (sidebar, between APRS Logs and System Logs) keeps a device-local
history of every one of these that actually fired; an **exclusion list**
(the inverse of favorites) lets the operator silence a specific callsign
regardless of what the digipeater/repeater/weather-station heuristics
decide.
Design rationale: `docs/superpowers/specs/2026-07-27-unread-notifications-design.md`.
Execution plan + manual test checklist:
`docs/superpowers/plans/2026-07-27-unread-notifications.md`.

## Why this exists (the bug)

The sidebar/top-bar unread dot used to lag up to 30 seconds behind what
the operator had actually just read:

- **Messages**: `MessageThread.svelte`'s dwell-to-read flow called
  `POST /messages/{id}/read` but never updated
  `messagesStore.svelte.js`'s `conversations` map — the badge only got
  corrected by `messagesTransport.js`'s 30s `refreshConversations()`
  rollup.
- **Bulletins**: `Bulletins.svelte` updated its own local list instantly,
  but `Sidebar.svelte` polled `GET /api/bulletins?...unread_only=true` on
  a completely independent 30s timer with no shared state.

Neither backend mark-read endpoint publishes a push event
(`pkg/messages/service.go`'s `MarkRead`/`MarkUnread`,
`pkg/webapi/bulletins.go`'s `markBulletinRead`/`markAllBulletinsRead` are
plain REST), so the fix is frontend-only: update the shared unread
counters optimistically at the point of the state change. See
[invariant 63](invariants.md).

## Unread-lag fix

| Concern | Where |
|---|---|
| Messages optimistic decrement/rollback | `web/src/lib/messagesStore.svelte.js` (`decrementUnread`, `incrementUnread`) |
| Messages dwell-to-read batching | `web/src/components/messages/MessageThread.svelte` `flushBatch` — decrements per-thread immediately, rolls back on a rejected `markRead` |
| `activeThreadId` cleanup on route leave | `web/src/routes/Messages.svelte` `onMount` cleanup — without this, leaving `/messages` for another route left `activeThreadId` stale, which would permanently suppress popups for that thread (see `shouldNotifyMessage` below) |
| Bulletins shared store | `web/src/lib/bulletinsStore.svelte.js` — single source of truth for inbound bulletins + unread count, replacing Sidebar's old independent poll |
| Bulletins shared poll | `web/src/lib/bulletinsTransport.js` — one 30s poll (started app-wide from `App.svelte`), consumed by both `Sidebar.svelte` and `Bulletins.svelte` |
| "Newly unread" diffing | `web/src/lib/bulletins-diff-core.js` (`diffNewlyUnread`) — pure, unit-tested; also feeds the bulletin notification trigger below |

`Bulletins.svelte` now only polls its own outbound list
(`loadOutboundOnly`); inbound comes from `bulletinsStore.inboundList`.
Mark-read/mark-all-read/delete route through the store's methods so the
Sidebar badge and the page update in the same tick.

## Popup notifications

Two independent axes: **what triggers a notification** (suppression
rules) and **how it's delivered** (in-app toast vs. OS notification, per
operator preference).

| Concern | Where |
|---|---|
| Notification queue (push/cap/dismiss) | `web/src/lib/notifications-core.js` (pure) + `web/src/lib/notificationsStore.svelte.js` (runes wrapper, module singleton) |
| Popup UI | `web/src/components/NotificationPopup.svelte` — mounted unconditionally in `App.svelte` (not gated like `NewsPopup`), since it must be live on every route including the full-bleed map/messages views. Chonky-ui's `toast()`/`<Toaster/>` only accepts a plain string (no click handler, no markup), so this is a small purpose-built component rather than a wrapper around that primitive. |
| Suppression rules (pure) | `web/src/lib/notification-rules-core.js` — `shouldNotifyMessage` (false if the thread is muted or is the currently-open thread), `shouldNotifyBulletin` (false while `Bulletins.svelte` is mounted), `shouldFireOsNotification` (enabled + granted + (hidden or forced)) |
| Messages trigger | `web/src/lib/messagesTransport.js` `applyChange()` -> `maybeNotifyInbound(msg)` — the single funnel both the poll and SSE paths already call. Dedups by message id (bounded `Set`, cleared past 500 entries) so a redelivered message doesn't double-notify. Gated first by `notificationPrefsState.messageEnabled` (the per-type master switch, below), then by `shouldNotifyMessage`. |
| Bulletins trigger | `web/src/lib/bulletinsTransport.js` `poll()` — diffs via `bulletinsStore.replaceInbound()` (uses `diffNewlyUnread`), gated first by `notificationPrefsState.bulletinEnabled`, then by `shouldNotifyBulletin` |
| Station emergency trigger | `web/src/lib/stationAlertsTransport.js` `poll()` — 30s poll of `GET /api/stations/alerts` (server pre-filters to Mic-E message code 0 / Emergency, APRS101 ch 10 table 8, regardless of the poller's map viewport), diffs via the pure `diffNewlyAlerting` in `web/src/lib/station-alerts-diff-core.js` so a notification fires once per transition into Emergency (not every poll while it persists), gated by `notificationPrefsState.stationEmergencyEnabled`. Deliberately **Emergency-only** — Priority/Special/Committed/etc. show only as a passive badge on the map (`web/src/lib/map/popup.js`), matching what real APRS hardware (Kenwood/Yaesu/Icom) actually alarms on. See [`glossary.md`](glossary.md) for the Mic-E status/message-code terminology and the outbound side (setting your own beacon's status). |
| New-station / favorite trigger | `web/src/lib/stationNewTransport.js` `poll()` — 30s poll of `GET /api/stations/roster` (`pkg/webapi/stations.go`'s `listStationRoster`; every currently-heard station, compact `StationRosterDTO`, world-scope like the alerts endpoint, objects/items excluded, each row flagged `is_digipeater` via `isDigipeaterHeuristic` -- three independent signals ORed together, since no single one catches every real digipeater: an H-bit path entry ("*"-suffixed) elsewhere in the roster window (reuses `collectDigiCallsigns`, but only catches a digipeater *observed actively repeating* within the window), symbol code `#` (Digipeater) in *either* table, or an overlaid numbered-digi icon (table byte replaced by the overlay char `1`..`9`, code still `#`) -- an earlier version of this check required table == `/` too, which wrongly excluded the alternate table and missed overlaid-numbered digis (caught 2026-07-31 via WA4HR-2, an alternate-table `#` station that slipped through), or a case-insensitive `"digi"` substring in the beacon comment (added the same day after W4LEE-1 -- comment `"East Alabama ARC APRS Digi (UIV32N)"` -- slipped past the path-only check because it hadn't repeated anything within the 1h roster window at poll time)), diffed via `web/src/lib/station-new-diff-core.js`'s `diffNewlyHeard` against a **persisted staleness map** (`web/src/lib/known-stations-core.js`, callsign -> last-recorded-heard epoch ms, localStorage key `gw-known-stations`, evicted by *oldest-heard* rather than insertion order once over `MAX_KNOWN_STATIONS` = 20,000). "New" is **staleness-based, not once-per-lifetime**: a callsign counts as new again once the gap since its last recorded timestamp reaches `notificationPrefsState.stationNewThresholdSecs` (operator-configurable, default 2h; a 0/"Never" sentinel maps to `thresholdMs = Infinity`, reproducing the original once-ever behavior with no extra code path in `diffNewlyHeard`). `diffNewlyHeard` itself is filter-agnostic — it just returns threshold-crossers and always advances every present row's timestamp, favorite or not, filtered or not — the transport applies policy per row afterward. **First, unconditionally: exclusions** (`excludedStationsStore.has(row.callsign)`) — an excluded callsign never notifies, full stop, checked before anything else including favorites (the more specific "no" wins). Then two mutually exclusive branches for a non-excluded row: (1) **favorite** (`favoriteStationsStore.has(row.callsign)`) — always notifies (bypasses both the map filter below and the `is_digipeater`/`is_repeater` exclusion; an operator who favorites their own digipeater still wants to know it's alive), gated by `notificationPrefsState.stationFavoriteEnabled` (**on by default** — harmless since an empty favorites list means it never fires); (2) **general** — skips `is_digipeater` and `is_repeater` rows outright (both unconditional, no toggle — "non-human infrastructure that can message", 2026-07-31), skips `is_weather_station` rows unless `notificationPrefsState.stationNewIncludeWeather` is on (**off by default**, but unlike digipeater/repeater this one IS a toggle — "some may want to see weather stations"), then requires `notificationPrefsState.stationNewEnabled` (**off by default**, the one exception among the five master switches: a busy APRS-IS-gated system can hear a genuinely new callsign every few minutes) and the Live Map's RF Only / Direct RX Only filter, read fresh from `localStorage['gw_map_layer_toggles']` on every poll (not dependent on the map being mounted) with the same precedence as `LiveMapV2.svelte`'s filter effect (Direct RX Only wins when both are on). A favorite takes the favorite path only, even with both switches on — never a double notification; a favorite is also never filtered by `is_weather_station`/`is_digipeater`/`is_repeater`. The first poll ever run on a device seeds the staleness map from whatever's currently active **without** notifying (grandfathering), so enabling this doesn't flood an existing roster as if every station just crossed the threshold. **Resume catch-up (2026-07-31):** the per-station staleness gap alone is blind to the *client* being closed -- it measures time between two packet timestamps, so a station that happened to beacon shortly before the tab closed and again shortly after it reopened could stay under threshold even though the operator was gone the whole time in between and never saw either packet (KV4S-7 report: reopened after a few hours away, no notification, even though the beacon wasn't old). `stationNewTransport.js` now also persists `lastPollAtMs` (localStorage key `gw-station-new-last-poll`); when a poll detects the prior poll was more than `RESUME_GAP_MS` (5 min) ago -- a real close/suspend, not a normal 30s tick -- it passes that prior timestamp to `diffNewlyHeard` as `resumeSinceMs`, which also crosses any row heard at/after that cutoff regardless of its own per-station gap. Self-limiting: `lastPollAtMs` is refreshed every poll, so `resumeSinceMs` is null again on the very next tick and a continuously-active station still can't re-fire every threshold interval just by existing. |
| Weather-station detection | `pkg/webapi/stations.go`'s `isWeatherStationHeuristic` — true when the station has reported weather telemetry (`stationcache.Station.Weather != nil`, the same "nil if not a weather station" signal the type already documents) OR its symbol code is `_` (Weather Station) in *either* table -- table-restricted to `/` in an earlier version, broadened preemptively the same day the digipeater check's identical table-restriction mistake was found (see the trigger row above), before a matching bug report showed up. Surfaced as `StationRosterDTO.is_weather_station`. Motivating case (2026-07-31): the operator got a new-station notification for `AJ4FJ-13` (comment `"WXTrak"`, a fixed weather station) and wanted a way to exclude that class of station -- but, unlike digipeaters, explicitly wants it **toggleable** ("some may want to see weather stations"), not an unconditional exclusion; see `notificationPrefsState.stationNewIncludeWeather` above. |
| Repeater detection | `pkg/webapi/stations.go`'s `isRepeaterHeuristic` — voice-repeater infrastructure that "can message" (autopatch/remote-base) but isn't a human operator, same reasoning as digipeaters, **unconditional exclusion** (no toggle). Three signals ORed: symbol code `r` (best-effort, not cross-checked against a second source the way `#`/`_` were -- this session already found the table-restriction mistake twice for other icons, so this one is deliberately not load-bearing alone); comment contains "repeater"/"rptr"; or comment matches `repeaterOffsetPattern` (a frequency immediately followed by a `+`/`-` offset direction, e.g. `147.180+` -- the single most universal convention in ham repeater listings) or `repeaterFreqTonePattern` (frequency followed within a few characters by a tone/CTCSS marker, e.g. `146.84 T 123.0`). Both regexes are verified against real operator examples (2026-07-31): `W4AP-2` (`"W4AP 146.84 T 123.0"`, tone-adjacent shape) and `WR4VR-3` (`"147.18+ p1-127.3 NET-Wed8pm D-STAR145.24-"`, offset-sign shape -- the original regex only handled the first shape and missed this one). Surfaced as `StationRosterDTO.is_repeater`. |
| Favorites list | `web/src/lib/favoriteStationsStore.svelte.js` — module singleton (`entries`, `has`, `find`, `load`, `add`, `remove`, `toggle`) over `GET/POST/DELETE /api/stations/favorites` (`pkg/webapi/stations_favorites.go`, `configstore.FavoriteStation` — callsign + optional note, uppercased via `BeforeSave` same as `BlockedCallsign`). **Server-side, not device-local** (unlike the mode/sound/threshold prefs below) — a deliberate favorites list is operator data worth syncing across every device pointed at this instance, mirroring the blocked-call-signs precedent rather than the "new per-device UI preference" default. No PUT endpoint: editing a favorite's note is delete-and-re-add. `has()`/`find()` match with base-callsign wildcard semantics via `web/src/lib/callsign-match-core.js`'s `callsignMatches` (mirrors `pkg/messages/blocklist_set.go`'s `BlocklistSet.Blocked()`): a bare entry like `KV4S` matches every SSID of that base call (`KV4S-1`, `KV4S-9`, ...) but **not** an unrelated callsign that merely shares a prefix (`KV4SM`) -- exact string equality on either the full callsign or the dash-stripped base, never a substring/prefix check. An SSID-qualified entry (`KV4S-9`) matches only that exact station. Two UI entry points: the map station popup's star toggle (`web/src/lib/map/popup.js`'s `renderStationActionsHTML`, a `<button class="stn-fav-btn">` — not an `<a>`, since it mutates state rather than navigating — wired via `LiveMapV2.svelte`'s popup click delegation the same way `.path-link` clicks are, redrawing the same popup in place after the toggle resolves; `toggle()` removes whichever entry `find()` matched, so un-favoriting a specific SSID currently covered by a bare wildcard entry removes the wildcard) and a full list/add/remove box in `NotificationsSettings.svelte`. `App.svelte` primes `favoriteStationsStore.load()` at startup so the popup's star state is correct before the first `stationNewTransport.js` poll; the transport also reloads it every poll cycle so a favorite added on another device is picked up within 30s. |
| Excluded stations list | `web/src/lib/excludedStationsStore.svelte.js` — the inverse of the favorites store above, structurally identical (`GET/POST/DELETE /api/stations/exclusions`, `pkg/webapi/stations_exclusions.go`, `configstore.ExcludedStation`, same wildcard `callsignMatches` semantics, same server-side/shared-across-devices rationale, and now also a `toggle(callsign)` convenience method mirroring the favorites store's). Added 2026-07-31 as the manual escape hatch for a station the digipeater/repeater/weather-station heuristics don't catch (or anything else the operator just doesn't want to hear about) -- checked before the favorites branch in `stationNewTransport.js`, so an exclusion wins even over a favorite. Two UI entry points as of 2026-08-01, mirroring favorites: `NotificationsSettings.svelte`'s "Excluded stations" list/add/remove box, and the map station popup's bell-off "Exclude"/"Excluded" toggle (`web/src/lib/map/popup.js`'s `renderStationActionsHTML`, a `<button class="stn-exclude-btn">` wired via `LiveMapV2.svelte`'s popup click delegation exactly like the `.stn-fav-btn` star, redrawing the same popup in place after the toggle resolves). |
| Notifications Log | `web/src/routes/NotificationsLog.svelte` at `/notifications-log` (sidebar, between APRS Logs and System Logs) — device-local, capped history of every notification actually raised, backed by `web/src/lib/notificationsLogStore.svelte.js` (module singleton, localStorage key `gw-notifications-log`) over the pure `web/src/lib/notifications-log-core.js` (`parseLogEntries`/`addLogEntry`, capped at `MAX_LOG_ENTRIES` = 200, oldest dropped, unit-tested). Device-local rather than server-side for the same reason as the sound/threshold prefs (see `notification-prefs-store.svelte.js`'s header comment): each device already makes its own independent decision about whether to notify (muted threads, active thread, map filters), so a per-device log matches what that device actually showed. Every firing site (`messagesTransport.js`'s `maybeNotifyInbound`, `bulletinsTransport.js`/`stationAlertsTransport.js`'s poll loops, `stationNewTransport.js`'s shared `fire()` helper) calls `notificationsLogStore.add({kind, title, body, href})` right alongside its toast/OS/sound calls -- logged whenever a notification actually fires (same gating as the toast/OS calls), not merely whenever the underlying event happens. Rows are clickable (same `href` the toast used) and show a relative timestamp; a "Clear log" button empties it. |
| Bulletin deep-link | `#/bulletins?focus=<id>` — mirrors the `#/map?focus=CALL&lat=…&lon=…` convention. `Bulletins.svelte` parses `focus` from `querystring`, scrolls the matching row into view, and applies a transient `.is-focused` highlight. |
| Message deep-link | `#/messages?thread=<kind>:<key>` — the existing convention already used throughout `Messages.svelte`. |
| Station emergency / new-station / favorite deep-link | `#/map?focus=<callsign>&lat=<lat>&lon=<lon>` — `lat`/`lon` are **required**, not optional: `LiveMapV2.svelte`'s `parseFocusFromHash()` discards the entire deep-link (even the callsign) if either is missing/non-finite, since the same parse result also drives the initial camera fly-to before the station has loaded from a poll (`map.easeTo({center: [pendingFocus.lon, pendingFocus.lat], ...})`). A 2026-07-29 regression shipped `stationAlertsTransport.js`'s href without them, so clicking a real alert did nothing (map didn't recenter, popup never opened) -- caught by the operator, not by tests (no e2e coverage of the click-through). Fixed by carrying `StationAlertDTO.Lat`/`.Lon` (already on the wire) through into the href. `stationNewTransport.js`'s href (both the general and favorite paths) follows the identical shape (`StationRosterDTO.Lat`/`.Lon` are always populated — the roster endpoint excludes positionless stations the same way the alerts endpoint does). **Also required:** the click-through must work when `LiveMapV2.svelte` is *already* mounted on a different station's popup, not just on first navigation — see [invariant 68](invariants.md), a second click-through bug the operator found the same day (2026-07-31) via manual testing. |

### Notification mode (toast / OS / both)

Device-local preference (localStorage, **not** server-synced — this is a
per-device browser-permission concept):

| Concern | Where |
|---|---|
| Mode store | `web/src/lib/settings/notification-prefs-store.svelte.js` — `mode`: `'toast'` (default) \| `'os'` \| `'both'`. `setMode('os'|'both')` triggers the browser permission prompt; a denial falls back to `'toast'` so the operator never lands silently in a dead mode (decision logic factored into the pure `resolveModeAfterPermission` in `notification-prefs-core.js`, unit-tested in `notification-prefs-core.test.js` alongside `parseMode`/`parseEnabledFlag`). `supported` feature-detects `window.Notification` rather than hardcoding a platform list — false inside the Android build's in-process WebView (see code-map.md's Android section), so `os`/`both` are hidden from the picker there while toast stays available. |
| Per-type master switch | Same store — `messageEnabled`/`bulletinEnabled`/`stationEmergencyEnabled`/`stationNewEnabled`/`stationFavoriteEnabled` (`setMessageEnabled`/etc.). Four of the five are **on by default**; `stationNewEnabled` is the one exception and **defaults off** (see the new-station trigger row above for why) — `stationFavoriteEnabled` defaults back on since it only ever fires for a callsign the operator deliberately favorited. Independent of `mode`: gates whether a type notifies at all (popup + OS + sound), ahead of the mute/active-thread/page-open/staleness-map suppression rules. Turning all five off is "notifications off"; turning off one mutes just that type. |
| Re-notify threshold | Same store — `stationNewThresholdSecs` (`setStationNewThresholdSecs`, localStorage key `gw-notification-station-new-threshold-secs`, default `DEFAULT_STATION_NEW_THRESHOLD_SECS` = 7200 = 2h). Shared by both the general new-station and favorite paths in `stationNewTransport.js`. `STATION_NEW_THRESHOLDS` (`notification-prefs-core.js`) is the preset option list shown in `NotificationsSettings.svelte`'s picker: 1h/2h/4h/6h/8h/12h/24h/3d/1wk, plus a `0` = "Never" sentinel that `stationNewThresholdMs()` maps to `Infinity`. `parseStationNewThresholdSecs` now accepts **any** non-negative integer up to `MAX_CUSTOM_THRESHOLD_SECS` (52 weeks) rather than only the preset list, clamping anything above that ceiling — this is what backs the picker's "Custom…" entry (`NotificationsSettings.svelte`'s `thresholdMode` local state, `secsToCustomInput`/`customInputToSecs` converting between seconds and an hours-or-weeks count/unit pair the operator types directly). `isPresetThresholdSecs` is how the Select decides whether to show a fixed preset or fall through to "Custom…" for a value that isn't one. |
| Weather-station filter | Same store — `stationNewIncludeWeather` (`setStationNewIncludeWeather`, localStorage key `gw-notification-station-new-include-weather`, **defaults off**). General-path-only; never applied to favorites. See the weather-station-detection row above. |
| OS notification firer | `web/src/lib/osNotify.js` `fireOsNotification(title, body, onClick, {force})` — only fires when the tab is hidden/unfocused, unless `force: true`. This gate exists so the operator isn't double-signaled (in-app popup + OS banner) while already looking at the tab. |
| Preferences UI | `web/src/routes/NotificationsSettings.svelte` at `/preferences/notifications` (own sidebar entry, not under General/`Preferences.svelte`) — "Popup notifications" `Box`: the five per-type toggles, the re-notify threshold picker, a mode picker, plus **"Send test notification"**, **"Send test emergency notification"**, **"Send test new-station notification"**, and **"Send test favorite notification"** buttons, usable by any operator (not a dev-only affordance), that fire a real sample through whichever mode is currently selected via `{force: true}` — so the operator can preview exactly what they'll get before relying on it. Each station-flavored test button fetches `GET /api/station/config` + `GET /api/position` on mount and deep-links to `#/map?focus=<the operator's own callsign>` (falling back to a plain `#/map` link if unset) — the exact same href shape the real thing uses — so clicking it exercises the real click-through (camera fly-to, popup open, the popup's Message/Logs/QRZ/Favorite action row) without a real packet needing to go out over RF to test it. A separate "Favorite stations" `Box` on the same page holds the list/add/remove UI described in the trigger row above. |

### Notification sounds (per-type, device-local, custom upload)

Separate sound settings for messages, bulletins, station emergencies,
new stations, and favorite stations, alongside the mode picker in
`NotificationsSettings.svelte`'s "Message sounds" / "Bulletin sounds" /
"Station emergency sounds" / "New station sounds" / "Favorite station
sounds" boxes. Device-local like the toast/OS mode above (see
`notification-prefs-store.svelte.js`'s header comment for why) — no
backend involved, so a custom upload doesn't sync across an operator's
other devices. (This is deliberately narrower than the favorites *list*
itself, which is server-side — see the trigger table above; only the
*sound choice* for hearing a favorite is per-device.)

| Concern | Where |
|---|---|
| Built-in sounds | `web/src/lib/soundPresets.js` — `SOUND_PRESETS`. `aprs-message`/`aprs-bulletin` are shipped wav files (`web/public/sounds/aprs-message.wav`, `aprs-bulletin.wav` — the station operator's own recordings) and are the **shipped defaults** for message/bulletin sound respectively (`DEFAULT_PRESET` in `notification-sound-store.svelte.js`); station emergency defaults to the shipped `emergency-mp3` file (`web/public/sounds/emergency.mp3`); new-station defaults to the synthesized `ping` preset and favorite-station to `chime` (swapped from an earlier build per operator preference, 2026-07-31) — both deliberately quiet, and deliberately *different* presets from each other so a favorite is audibly distinguishable from a plain new-station hit without either being loud (these two types can fire far more often than the other three); favorite gets the slightly more present two-tone `chime` since it's the rarer, more-wanted event of the two. `chime`/`ping`/`alert` are quieter synthesized tones (peak gain 0.25); `siren`/`klaxon`/`urgent-beeps` are louder, more insistent variants (peak gain 0.6, `alternating()`/`repeated()` generator helpers in `soundPresets.js` build their tone sequences, `klaxon` also uses a `sawtooth` oscillator instead of `sine` for a harsher timbre) added 2026-07-29 after the shipped `alert` preset proved too quiet/generic for an Emergency alert in practice -- all of these are picked from the same per-type Sound dropdown as everything else, so an operator can use any preset for any type. Every tone entry supports optional `gain` (peak amplitude, default 0.25) and `type` (oscillator waveform, default `sine`) overrides, consumed by `playTones` in the same file. `playPreset(id)` plays either kind — url-based presets via `new Audio(url).play()`, tone-based via oscillators — and no-ops (rather than throwing) outside a browser, so it's callable from `node --test`. `resolvePreset(id)` is the pure id->preset lookup, unit-tested in `soundPresets.test.js`. |
| Custom upload storage | `web/src/lib/notificationSoundsDb.js` — tiny IndexedDB wrapper (`putCustomSound`/`getCustomSound`/`deleteCustomSound`), keyed by `'message'` \| `'bulletin'` \| `'stationEmergency'` \| `'stationNew'` \| `'stationFavorite'`. IndexedDB rather than localStorage because the value is a binary `Blob` (the uploaded audio file) and localStorage can't hold one / has too small a quota. Untested (no `fake-indexeddb` dependency in this repo) — covered by the manual checklist below, same as `osNotify.js`'s browser-API calls. |
| Pure decision logic | `web/src/lib/settings/notification-sound-core.js` — runes-free so it's unit-testable under `node --test` (mirrors `channelsCore.js`/`releaseNotesCore.js`): `isValidPresetId`, `parsePresetId`/`parseEnabledFlag` (localStorage value parsing with on-by-default/preset-default fallbacks), `validateUploadFile` (audio/* MIME + `MAX_SOUND_BYTES` 2 MB cap), `fallbackPresetId` (what to actually play/show when `'custom'` is selected but the upload is missing — e.g. IndexedDB cleared independently of localStorage). See `notification-sound-core.test.js`. |
| Settings + play logic | `web/src/lib/settings/notification-sound-store.svelte.js` — the `$state` wrapper around the core above. `notificationSoundState.message` / `.bulletin` / `.stationEmergency` / `.stationNew` / `.stationFavorite`, each exposing `enabled`, `presetId` (a `SOUND_PRESETS` id \| `'custom'`), `hasCustom`, `customName`, `isDefault` (true when `presetId` already equals the kind's shipped default), and methods `setEnabled`, `setPreset`, `upload(file)`, `removeCustom`, `resetToDefault()` (switches back to the shipped default preset without deleting an uploaded custom sound — added 2026-07-29 so an operator who tries a preset/upload and doesn't like it isn't stuck hunting for which option was original), `play()` (real trigger path, no-ops when disabled), `preview()` (always plays — used by the Test sound button, mirrors `fireOsNotification`'s `force` convention). `enabled`/`presetId` persist to localStorage; the audio bytes live in IndexedDB. `NotificationsSettings.svelte` shows a "Reset to default" button per sound box, only when `!isDefault`. |
| Trigger wiring | `messagesTransport.js`'s `maybeNotifyInbound` calls `notificationSoundState.message.play()` right after the toast/OS calls, gated by `notificationPrefsState.messageEnabled` then `shouldNotifyMessage` (muted thread / active thread suppress the sound too). `bulletinsTransport.js`'s `poll()` calls `notificationSoundState.bulletin.play()` inside the `bulletinEnabled` + `shouldNotifyBulletin` loop, same gating as the popup. `stationAlertsTransport.js`'s `poll()` calls `notificationSoundState.stationEmergency.play()` for each newly-alerting station, gated by `stationEmergencyEnabled`. `stationNewTransport.js`'s `poll()` calls `notificationSoundState.stationFavorite.play()` or `.stationNew.play()` depending on which of the two branches (see the trigger table above) a threshold-crossing row falls into, gated by the matching per-type switch. The "Send test notification" button (`NotificationsSettings.svelte`) also plays both `notificationSoundState.message.play()` and `.bulletin.play()` (staggered 350ms) alongside the toast/OS preview, each gated by its own master switch — so the button demonstrates the exact combination the operator has configured, not just the popup half. Separate "Send test emergency notification", "Send test new-station notification", and "Send test favorite notification" buttons do the same for `stationEmergency`, `stationNew`, and `stationFavorite` respectively. |

## Troubleshooting: no new-station/favorite notification on a fresh session

`stationNewTransport.js`'s staleness map (`gw-known-stations` in
localStorage) is per-device, per-browser-profile. When that key doesn't
exist yet -- a private/incognito window, a browser whose site data was
cleared, or a genuinely new device -- the very first poll takes the
`!hasBaseline` branch and seeds every currently-active station (anyone
already on the roster, including a favorite) into the map **without**
notifying, by design (grandfathering; see the new-station/favorite
trigger row above). That station will only notify from that point
forward once it goes stale past the configured threshold and reappears.
This is a distinct scenario from the `own-station-core.js` exclusion
(exact `CALL[-SSID]` match against the configured station callsign/beacon
overrides only -- a different SSID under the same base call, e.g. a
handheld, is deliberately still notified) and from the resume-catch-up
fix above (which only helps once a baseline already exists on that
device). If an operator reports "no notification for a station I know is
active" right after opening the app on a browser/device it hasn't run on
before, this grandfathering is the first thing to check, not a bug.

## Troubleshooting: OS notification granted but nothing appears

The Notification API reporting `permission === 'granted'` only means the
*browser* will let the page ask for a notification — it does not mean
the OS will actually display one. `osNotify.js`'s `fireOsNotification`
now logs to the console in both failure shapes so this is diagnosable
from DevTools instead of a silent no-op:

- `[osNotify] suppressed: {...}` — `shouldFireOsNotification` gated it
  (wrong mode, permission not granted, or the tab is focused and `force`
  wasn't set). The logged object shows exactly which condition failed.
- `[osNotify] Notification construction failed: ...` — `new
  Notification()` itself threw.

If neither line appears (the call reached `new Notification(...)`
without error) but no banner shows up, the browser handed the
notification to the OS and the OS or the desktop environment is the one
suppressing it — nothing left in our code to check. On Windows this is
almost always one of:

- **Focus Assist** (Action Center) set to "Priority only" or "Alarms
  only" — silently drops normal app notifications with no error
  anywhere.
- **Settings → System → Notifications** — notifications off globally,
  or specifically disabled for the browser.
- **Chrome's own per-site notification permission** (padlock icon →
  Notifications) showing something other than "Allow", independent of
  what `Notification.permission` reports to the page in some edge
  cases.

None of these are bugs in graywolf's notification code — they're
OS/browser configuration outside a web page's control, which is exactly
why the in-app toast (always available regardless of `mode`) is the
delivery path that doesn't depend on any of this.

## Test coverage

Pure logic is unit-tested (`node --test`, no Playwright/e2e suite in this
repo):

- `bulletins-diff-core.test.js`
- `station-alerts-diff-core.test.js`
- `station-new-diff-core.test.js`
- `known-stations-core.test.js`
- `callsign-match-core.test.js`
- `map/focus-hash-core.test.js` — pure `#/map?focus=` parsing/comparison, covers invariants #66 and #68
- `map/view-radius-core.test.js` — pure "N miles around a center point" bounding-box math
- `notifications-log-core.test.js` — pure load/cap logic for the device-local notifications log
- `notifications-core.test.js`
- `notification-rules-core.test.js`
- `settings/notification-prefs-core.test.js`
- `settings/notification-sound-core.test.js`
- `soundPresets.test.js`

UI/permission behavior (popup placement, OS-notification permission
flows, Android graceful degradation) is covered by the manual checklist
in `docs/superpowers/plans/2026-07-27-unread-notifications.md`. Sound
playback, custom-upload persistence across a reload, and the per-type
master toggles are covered by
`docs/superpowers/plans/2026-07-29-notification-sounds-manual-test-plan.md` —
neither is automatable without a real `AudioContext`/`IndexedDB`.

Backend Go tests for the roster/digipeater/repeater/weather-station
flagging and favorites/exclusions CRUD endpoints live in
`pkg/webapi/stations_test.go` (`TestStationRoster_*`),
`pkg/webapi/stations_favorites_test.go` (`TestFavoriteStations_*`), and
`pkg/webapi/stations_exclusions_test.go` (`TestExcludedStations_*`).
The map popup's star and exclude (bell-off) toggles
(`popup.js`'s `renderStationActionsHTML`) are untested at the unit level
like the rest of `popup.js` — it depends on `unitsState`, a
runes-based `.svelte.js` store, which isn't importable under plain
`node --test` without the Svelte compiler; covered by the same
no-e2e-coverage caveat as the deep-link click-through elsewhere in this
page.
