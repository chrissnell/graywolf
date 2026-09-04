# Notification sounds + per-type toggles + own Notifications page — manual test plan

**Why manual:** `node --test` has no `window`, `AudioContext`, `Audio`,
or `IndexedDB`, so the pure decision logic (which preset wins, upload
validation, permission fallback) is unit-tested in
`notification-prefs-core.test.js`, `notification-sound-core.test.js`,
and `soundPresets.test.js`, but actually hearing a sound, persisting an
upload across a reload, and the OS permission prompt can only be
verified in a real browser. This mirrors the existing split described
in `docs/wiki/notifications.md`'s "Test coverage" section.

Automated: 546/546 passing (`node --test`), `npm run build` clean.

## Setup

Serve the built app (or `npm run dev`) and open it in a browser that
supports `Notification` (desktop Chrome/Firefox/Edge — Android's
in-process WebView doesn't, which is covered separately below).

## Own Notifications page

- [ ] Sidebar → Settings no longer shows a "Notifications" box under
      "General" (`/preferences`) — only Theme, Display size, Units,
      Updates remain there.
- [ ] Sidebar → Settings shows a "Notifications" entry between
      "Messaging" and "Position Log"; clicking it lands on
      `/preferences/notifications`.

## Shipped defaults (out of the box, fresh browser profile / cleared localStorage)

- [ ] "Message sounds" box: enabled is ON, sound picker shows "APRS
      Message" selected.
- [ ] "Bulletin sounds" box: enabled is ON, sound picker shows "APRS
      Bulletin" selected.
- [ ] "Popup notifications" box: both "Notify me about new messages" and
      "Notify me about new bulletins" toggles are ON; mode defaults to
      "In-app popup".

## Sound picker + built-in presets

- [ ] Message sound picker lists: APRS Message, APRS Bulletin, Chime,
      Ping, Alert. Selecting each and clicking "Test sound" plays an
      audibly distinct sound; APRS Message/APRS Bulletin play the actual
      wav files (not synthesized tones).
- [ ] Same for the Bulletin sound picker.
- [ ] "Test sound" plays even when "Play a sound for..." is toggled OFF
      (preview bypasses the enabled gate).

## Custom upload

- [ ] "Upload custom sound" on Message sounds → pick a small .wav/.mp3 →
      sound picker now shows "Custom: <filename>" and auto-selects it;
      success toast appears.
- [ ] Click "Test sound" — the uploaded file plays, not a preset.
- [ ] Reload the page — the custom selection and filename persist (confirms
      IndexedDB round-trip, not just in-memory state).
- [ ] Try uploading a non-audio file (e.g. a .png) — rejected with "Please
      choose an audio file." and nothing changes.
- [ ] Try uploading a file over 2 MB — rejected with "Sound file is too
      large (2 MB max)."
- [ ] Click "Remove custom sound" — picker falls back to the kind's
      default preset (APRS Message / APRS Bulletin); reload confirms the
      custom sound stays gone.
- [ ] Repeat upload/remove independently for Bulletin sounds; confirm
      Message and Bulletin custom sounds don't clobber each other (two
      separate IndexedDB keys).

## Per-type enable toggles (popups + OS + sound together)

- [ ] Turn OFF "Notify me about new messages" only. Receive a new inbound
      message (or trigger via the API/simulation): no toast, no OS
      notification, no sound. Receive a new bulletin: toast/OS/sound
      still fire normally.
- [ ] Turn "Notify me about new messages" back ON, turn OFF "Notify me
      about new bulletins" only: inverse of the above.
- [ ] Turn OFF both: neither type notifies at all (toast, OS, or sound).
- [ ] Confirm the sound-box "Play a sound for..." toggle still works
      *within* an enabled type — e.g. messages enabled + popups showing,
      but message sound toggled off: toast/OS still fire, no sound.

## Send test notification button

- [ ] With mode "In-app popup" and both master toggles ON: clicking
      "Send test notification" shows the in-app toast AND plays both the
      message sound and the bulletin sound (staggered, not overlapping).
- [ ] Switch mode to "OS notification" (grants permission if prompted):
      clicking the button shows an OS-level notification instead of a
      toast, sounds still play.
- [ ] Switch mode to "Both": both toast and OS notification appear.
- [ ] Turn off "Notify me about new messages": clicking the button no
      longer plays the message sound, but still plays the bulletin sound
      (and vice versa).
- [ ] Deny the browser's notification permission when prompted for
      "OS"/"Both": mode falls back to "In-app popup" rather than silently
      doing nothing (pre-existing behavior, re-verify it still holds).

## Android (if available)

- [ ] Notifications page hides the OS/Both mode options (Android's
      in-process WebView has no `Notification` API) — same as before this
      change. Sound presets and custom upload still work (IndexedDB and
      `Audio` are both available there).

## Regression spot-check (unrelated to this change, but adjacent code touched)

- [ ] Muting a thread still suppresses that thread's popup (and now its
      sound) even with the master "Notify me about new messages" toggle
      ON.
- [ ] Being on `/bulletins` still suppresses bulletin popups/sound for
      newly-unread rows arriving while the page is open.
