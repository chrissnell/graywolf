# Unread-badge lag fix + clickable new-activity popup notifications — design

**Status:** implemented (feature/aprs-bulletin-board, 2026-07-27)

## Goal

Two problems, reported together and fixed together because they share a
root cause and touch the same unread-tracking code paths:

1. **Bug:** the sidebar/top-bar unread dot (Messages and Bulletins) lags
   up to 30 seconds behind what the operator already read.
2. **Feature:** a clickable popup notification when a new message or
   bulletin arrives, that jumps straight to it — with a choice of
   delivery (in-app toast, OS-level browser notification, or both) and a
   "Send test notification" button so any operator can preview their
   choice.

## Root cause of the lag bug

Read-state changes were only ever reconciled by a periodic poll, never
pushed to the shared unread counters at the moment they happened:

- **Messages:** `MessageThread.svelte`'s dwell-to-read flow called
  `POST /messages/{id}/read` but never touched
  `messagesStore.svelte.js`'s `conversations` map. The sidebar/top-bar
  `unreadTotal` getter only got corrected by `messagesTransport.js`'s
  30s `refreshConversations()` rollup.
- **Bulletins:** `Bulletins.svelte` updated its own local list instantly
  on mark-read, but `Sidebar.svelte` polled
  `GET /api/bulletins?direction=in&unread_only=true` on a **completely
  independent** 30s timer with zero shared state.

Confirmed there is no backend push signal to shortcut this either:
`pkg/messages/service.go`'s `MarkRead`/`MarkUnread` and
`pkg/webapi/bulletins.go`'s `markBulletinRead`/`markAllBulletinsRead` are
plain REST — no `EventHub.Publish` call in either path. So the fix is
entirely frontend: update the shared unread stores optimistically at the
point of the state change, with rollback on a failed request. This is
now [invariant 63](../../wiki/invariants.md).

## Decisions locked in with the operator

- Trigger scope: **both** new DMs/tactical messages and new bulletins.
- Delivery: **both** an always-available in-app toast and an opt-in
  OS-level browser notification — as a single **mode** the operator
  picks (`toast` / `os` / `both`), not a simple on/off toggle, because
  the operator explicitly wants to choose which channel(s) they get.
- A **"Send test notification" button**, usable by any operator (not a
  developer-only debug affordance), that fires a real sample through
  whichever mode is currently selected — added after an initial "opt
  into OS notifications" toggle proposal was rejected in favor of the
  richer mode picker + live preview.
- Bundled into the same PR as the bug fix (`feature/aprs-bulletin-board`)
  rather than split, since both touch the same store code.
- No backend changes anywhere in this feature — bulletins stay
  poll-only (no new SSE/event-hub work), and the messages fix is a pure
  frontend optimistic update on top of the existing delta/SSE transport.

## Rejected alternatives

- **Reusing chonky-ui's `toast()`/`<Toaster/>`.** Its API is
  `toast(message, variant, duration)` — a plain string, no click
  handler, no markup (confirmed by reading
  `@chrissnell/chonky-ui`'s `Toast/toast.svelte.js` and
  `Toaster.svelte`). A clickable, kind-aware popup needs its own small
  component (`NotificationPopup.svelte`), styled to match but not built
  on top of the primitive.
- **Backend SSE/event-hub push for bulletins.** Messages already have an
  opt-in SSE path (`pkg/messages/event_hub.go`,
  `messagesTransport.js`'s `?sse=1`); bulletins have no push mechanism at
  all. Adding one was explicitly out of scope — the existing 30s poll is
  reused as-is, just consolidated into one shared poll (`bulletinsStore`
  + `bulletinsTransport.js`) instead of two independent ones, and the
  same poll doubles as the new-bulletin notification trigger by diffing
  snapshots (`bulletins-diff-core.js`).
- **A single OS-notification on/off toggle.** Superseded mid-design by
  the operator's explicit ask for a toast/OS/both mode picker with a
  live test button (see decisions above).

## Architecture summary

**Unread-lag fix:**
- `messagesStore.svelte.js` gains `decrementUnread`/`incrementUnread`;
  `MessageThread.svelte`'s `flushBatch` calls them around the existing
  `markRead` POST, keyed by thread (captured from the message row
  itself, not an outer prop, since the component persists across thread
  switches).
- `Messages.svelte` now clears `store.activeThreadId` on unmount — found
  during implementation review to be required for the popup feature's
  "don't notify about the thread I'm already viewing" rule to work once
  the operator navigates away.
- New `bulletinsStore.svelte.js` (shared `SvelteMap`-backed singleton,
  mirroring `messagesStore.svelte.js`) replaces both `Sidebar.svelte`'s
  independent poll and `Bulletins.svelte`'s page-local list.
- New `bulletinsTransport.js` runs the single shared 30s poll, started
  app-wide from `App.svelte`.

**Popup notifications:**
- `notifications-core.js` (pure) + `notificationsStore.svelte.js` (runes
  singleton) — a capped stack (4 entries, 8s auto-dismiss).
- `NotificationPopup.svelte` — mounted unconditionally in `App.svelte`.
- `notification-rules-core.js` (pure) — `shouldNotifyMessage` (muted /
  active-thread suppression), `shouldNotifyBulletin` (page-active
  suppression), `shouldFireOsNotification` (enabled + granted +
  (hidden or forced)).
- Trigger wiring: `messagesTransport.js`'s `applyChange()` (the single
  funnel both the poll and SSE paths already call) and
  `bulletinsTransport.js`'s `poll()`.
- Deep-links: `#/messages?thread=<kind>:<key>` (pre-existing convention)
  and the new `#/bulletins?focus=<id>` (mirrors the pre-existing
  `#/map?focus=CALL&lat=…&lon=…` convention) — `Bulletins.svelte` scrolls
  to and briefly highlights the target row.
- `notification-prefs-store.svelte.js` — device-local (localStorage,
  **not** server-synced) tri-state `mode`. `osNotify.js`'s
  `fireOsNotification(title, body, onClick, {force})` fires only when
  the tab is hidden/unfocused, unless forced (the test-button path).
- `Preferences.svelte` gains a "Notifications" `Box`: the mode picker
  (with `os`/`both` hidden when `Notification` is undefined, e.g. inside
  the Android build's in-process WebView) plus the test button.

## Full detail

See `docs/wiki/notifications.md` for the file-by-file reference, and
`docs/superpowers/plans/2026-07-27-unread-notifications.md` for the
task-by-task build record and the manual test-plan checklist.
