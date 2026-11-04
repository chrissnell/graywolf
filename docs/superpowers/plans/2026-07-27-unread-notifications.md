# Unread-badge lag fix + clickable new-activity popup notifications — implementation plan

**Goal:** Fix the up-to-30s lag between marking a message/bulletin read
and the sidebar/top-bar unread dot updating, and add a clickable popup
notification (in-app toast and/or OS-level browser notification, per
operator preference) for new messages and bulletins that jumps straight
to the item — plus a "Send test notification" button any operator can
use to preview their chosen mode.

**Architecture:** Frontend-only. Two shared reactive stores
(`messagesStore.svelte.js`, new `bulletinsStore.svelte.js`) get
optimistic mark-read updates instead of relying solely on their 30s
rollup polls. A new notification queue (`notificationsStore.svelte.js`)
feeds a purpose-built popup component (`NotificationPopup.svelte`,
mounted app-wide), triggered from the existing messages transport and a
new consolidated bulletins transport, gated by pure suppression-rule
helpers and a device-local toast/OS/both preference.

**Tech Stack:** Svelte 5 (runes), `svelte-spa-router`, `node --test` for
pure-JS unit tests. No backend changes.

Design spec: `docs/superpowers/specs/2026-07-27-unread-notifications-design.md`.

---

## File structure

**New:**
- `web/src/lib/notifications-core.js` + `.test.js`
- `web/src/lib/notificationsStore.svelte.js`
- `web/src/lib/notification-rules-core.js` + `.test.js`
- `web/src/lib/settings/notification-prefs-store.svelte.js`
- `web/src/lib/osNotify.js`
- `web/src/lib/bulletins-diff-core.js` + `.test.js`
- `web/src/lib/bulletinsStore.svelte.js`
- `web/src/lib/bulletinsTransport.js`
- `web/src/components/NotificationPopup.svelte`

**Modified:**
- `web/src/lib/messagesStore.svelte.js` — `decrementUnread`/`incrementUnread`
- `web/src/components/messages/MessageThread.svelte` — batch-by-thread `flushBatch`
- `web/src/routes/Messages.svelte` — clear `activeThreadId` on unmount
- `web/src/lib/messagesTransport.js` — `maybeNotifyInbound` wired into `applyChange`
- `web/src/routes/Bulletins.svelte` — consumes `bulletinsStore`, adds `#/bulletins?focus=<id>`
- `web/src/components/Sidebar.svelte` — consumes `bulletinsStore` instead of its own poll
- `web/src/App.svelte` — mounts `NotificationPopup`, starts `bulletinsTransport`
- `web/src/routes/Preferences.svelte` — "Notifications" Box (mode picker + test button)

**Docs:**
- `docs/wiki/code-map.md`, `docs/wiki/bulletins.md`, `docs/wiki/README.md`, `docs/wiki/invariants.md` (modified)
- `docs/wiki/notifications.md` (new)
- `docs/handbook/preferences.html` (modified)

---

## Task 1: Messages unread-lag fix

- [x] Add `decrementUnread(threadId, n)` / `incrementUnread(threadId, n)`
      to `messagesStore.svelte.js`, clamped at 0.
- [x] `MessageThread.svelte`: change `batchedIds` from `Set<number>` to
      `Map<number, {kind, key}>`, capturing thread identity from the
      message row itself (not an outer `threadId`, since the component
      persists across thread switches).
- [x] Rewrite `flushBatch` to decrement per-thread immediately, then
      `Promise.allSettled` the `markRead` calls and roll back per-thread
      on any rejection.
- [x] `Messages.svelte`: add an `onMount` cleanup that clears
      `store.setActiveThread(null)` — required so a thread's
      "currently open" state doesn't stick after navigating away.

## Task 2: Bulletins shared store

- [x] `bulletins-diff-core.js`: pure `diffNewlyUnread(prevById, freshRows)`.
- [x] `bulletins-diff-core.test.js`: new/unchanged/re-unread/read/empty cases.
- [x] `bulletinsStore.svelte.js`: `SvelteMap`-backed singleton —
      `inbound`, `loaded`, `pageActive`, `unreadTotal`, `inboundList`,
      `replaceInbound`, `markRead`, `markUnreadLocal`, `markAllRead`,
      `removeLocal`, `setPageActive`.
- [x] `bulletinsTransport.js`: single 30s poll, `start`/`stop`.
- [x] `Sidebar.svelte`: drop the independent poll; read
      `bulletinsStore.unreadTotal`.
- [x] `Bulletins.svelte`: `inbound` becomes `$derived(bulletinsStore.inboundList)`;
      `onMount` only loads outbound + toggles `pageActive`; mark-read/
      mark-all-read/delete route through the store with rollback.
- [x] `App.svelte`: start `bulletinsTransport` alongside `messagesTransport`.

## Task 3: Notification queue + popup UI

- [x] `notifications-core.js`: pure `addEntry`/`removeEntry` (cap at 4).
- [x] `notifications-core.test.js`.
- [x] `notificationsStore.svelte.js`: runes wrapper, 8s auto-dismiss.
- [x] `NotificationPopup.svelte`: fixed-position stack, click-to-navigate
      + dismiss, kind icon (message/bulletin), mounted unconditionally in
      `App.svelte` next to `ServerUpdatedBanner`.

## Task 4: Suppression rules + trigger wiring

- [x] `notification-rules-core.js`: `shouldNotifyMessage`,
      `shouldNotifyBulletin`, `shouldFireOsNotification` (with `force`).
- [x] `notification-rules-core.test.js`.
- [x] `messagesTransport.js`: `maybeNotifyInbound` called from
      `applyChange()` right after `upsertMessage`; dedup by message id
      (bounded `Set`, cleared past 500).
- [x] `bulletinsTransport.js`: notify on `diffNewlyUnread` results, gated
      by `shouldNotifyBulletin`.
- [x] `Bulletins.svelte`: `#/bulletins?focus=<id>` — parse `querystring`,
      scroll + transient `.is-focused` highlight on the target row.

## Task 5: Notification mode (toast / OS / both) + test button

- [x] `notification-prefs-store.svelte.js`: device-local `mode`
      (`toast`/`os`/`both`), `supported` feature-detect, `setMode` with
      permission-request + deny-fallback.
- [x] `osNotify.js`: `fireOsNotification(title, body, onClick, {force})`.
- [x] `Preferences.svelte`: "Notifications" Box — mode `Select` (hides
      `os`/`both` when unsupported), hint text, "Send test notification"
      `Button` firing through whichever mode is active with `force: true`.

## Task 6: Docs

- [x] `docs/wiki/code-map.md` — `src/lib/settings/` and `src/components/`
      rows extended.
- [x] `docs/wiki/bulletins.md` — Frontend section rewritten for the
      shared store + focus deep-link.
- [x] `docs/wiki/notifications.md` (new) — full file-by-file reference.
- [x] `docs/wiki/README.md` — page index entry.
- [x] `docs/wiki/invariants.md` — invariant 63 (optimistic unread updates).
- [x] `docs/handbook/preferences.html` — operator-facing "Notifications" section.
- [x] This design/plan doc pair.

## Task 7: Automated tests

- [x] `web/src/lib/bulletins-diff-core.test.js`
- [x] `web/src/lib/notifications-core.test.js`
- [x] `web/src/lib/notification-rules-core.test.js`
- [x] `cd web && npm test` — run the full suite, confirm no regressions.

---

## Test Plan

Automated coverage is pure-logic only (`node --test`, no Playwright/e2e
suite in this repo): `bulletins-diff-core.test.js`,
`notifications-core.test.js`, `notification-rules-core.test.js`. Popup
placement, real browser permission flows, and Android behavior need
manual verification:

- [ ] Dwell-read a DM bubble; confirm the sidebar/top-bar dot updates
      within ~2s, not 30s.
- [ ] Go offline (DevTools) mid-dwell so `markRead` fails; confirm the
      badge rolls back to its pre-decrement value.
- [ ] Receive a bulletin while on the Dashboard; confirm the Sidebar
      badge updates without visiting `/bulletins`.
- [ ] Mark a bulletin read on `/bulletins`; confirm the Sidebar badge
      decrements immediately and survives navigating away and back.
- [ ] Receive a new DM while on `/map`; confirm an in-app popup appears,
      clicking it navigates to the thread and dismisses.
- [ ] Receive a new DM while that thread is already open; confirm no
      popup fires. Then navigate away to `/map` and receive another
      message on that same thread; confirm a popup now DOES fire
      (verifies the `activeThreadId` cleanup on route leave).
- [ ] Mute a thread, receive a message on it; confirm no popup fires.
- [ ] Receive a bulletin while on `/bulletins`; confirm no popup fires
      but the row still appears.
- [ ] Click a bulletin popup; confirm it scrolls to and highlights the
      right row on `/bulletins`.
- [ ] On Preferences, click "Send test notification" in each of the
      three modes (Toast / OS / Both) and confirm each shows exactly
      what it claims to — this is the operator-facing preview, not a
      dev tool.
- [ ] Set mode to "OS", background the tab, receive a real message;
      confirm an OS notification appears (and no in-app popup) and
      clicking it refocuses + deep-links.
- [ ] Set mode to "OS" or "Both" with the tab focused, receive a real
      message; confirm no OS banner fires (only the in-app popup, if
      "Both") — verifies the `document.hidden`/`hasFocus()` gate still
      applies to real notifications even though the test button
      bypasses it.
- [ ] Choose "OS" or "Both" when the browser denies permission; confirm
      it falls back to "Toast" rather than silently doing nothing.
- [ ] Simulate `Notification === undefined` (or test on Android);
      confirm the mode picker only offers "In-app popup" (OS/Both
      options absent), not that the whole Box disappears — toast
      notifications must still work there.
- [ ] Stack 5+ rapid messages/bulletins; confirm the popup stack caps at
      4 (oldest drop off).
- [ ] Reload the page; confirm the notification mode persists and
      unread badges rehydrate correctly from the initial snapshots.
