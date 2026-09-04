// Messages transport — poll-based with an SSE upgrade hook.
//
// Responsibilities:
//   - On start(), do an initial snapshot fetch:
//       1. GET /api/messages/tactical   -> seeds the tactical directory
//       2. GET /api/messages/conversations -> seeds the thread rollup
//   - Then either poll GET /api/messages?cursor=... every 5 s, or
//     upgrade to an EventSource on /api/messages/events if the browser
//     supports it and the `?sse=1` URL flag is set.
//   - On each /messages response, feed {changes: [{id, kind, message}]}
//     into the store via `upsertMessage` / `markDeleted`.
//   - Periodically refresh /conversations (every 30 s) to reconcile
//     unread counts with the server's authoritative rollup — the
//     per-message upserts can drift because we don't deduplicate by id.
//
// Why poll-default (not SSE-default):
//   - The SSE handler on the backend is new in Phase 4; polling is the
//     fallback the plan explicitly calls for and what production
//     traffic will exercise first. Shipping polling by default means
//     the unread-badge signal path is exercised on every pageview.
//   - `?sse=1` is an opt-in that early-adopters (and Phase 8
//     Playwright scenarios) can flip to exercise the live path.
//   - If SSE proves stable, a one-line swap makes it the default.
//
// Backoff: 5s → 10s → 20s → 60s cap on 5xx/network errors. On the next
// successful request we reset back to the 5s baseline.
//
// Lifecycle: start() is called once at App startup (see App.svelte);
// stop() is only invoked by the hot-reload path in dev. Polling is
// always-on for the session so the sidebar unread badge stays fresh
// from any page.

import { messages } from './messagesStore.svelte.js';
import {
  listMessages,
  listConversations,
  listTacticals,
} from '../api/messages.js';
import { notifications } from './notificationsStore.svelte.js';
import { shouldNotifyMessage } from './notification-rules-core.js';
import { fireOsNotification } from './osNotify.js';
import { notificationPrefsState } from './settings/notification-prefs-store.svelte.js';
import { notificationSoundState } from './settings/notification-sound-store.svelte.js';
import { notificationsLogStore } from './notificationsLogStore.svelte.js';

const POLL_BASE_MS = 5_000;
const POLL_MAX_MS = 60_000;
const CONVERSATIONS_REFRESH_MS = 30_000;

let pollTimer = null;
let conversationsTimer = null;
let currentBackoff = POLL_BASE_MS;
let started = false;
let stopped = false;
let es = null; // EventSource when SSE is active
let esBackoff = POLL_BASE_MS;

/** Detect whether SSE should be used. Off by default; opt-in via ?sse=1. */
function shouldUseSSE() {
  if (typeof window === 'undefined') return false;
  if (typeof EventSource === 'undefined') return false;
  try {
    const url = new URL(window.location.href);
    return url.searchParams.get('sse') === '1';
  } catch {
    return false;
  }
}

// Dedup set so a message already seen (e.g. re-delivered on a
// reconnect, or present in both the delta and a conversations rollup
// window) doesn't raise a second popup. Bounded so a long session
// doesn't grow this unboundedly.
const notifiedMessageIds = new Set();
const MAX_NOTIFIED_IDS = 500;

/**
 * Raise a new-activity notification for a genuinely new inbound unread
 * message, subject to the mute/active-thread suppression rules and the
 * operator's notification mode (toast/os/both).
 */
function maybeNotifyInbound(msg) {
  if (!msg || msg.direction !== 'in' || !msg.unread || typeof msg.id !== 'number') return;
  if (notifiedMessageIds.has(msg.id)) return;
  notifiedMessageIds.add(msg.id);
  if (notifiedMessageIds.size > MAX_NOTIFIED_IDS) notifiedMessageIds.clear();

  if (!notificationPrefsState.messageEnabled) return;

  const threadId = messages.threadIdFor(msg.thread_kind, msg.thread_key);
  const thread = messages.conversations.get(threadId);
  if (!shouldNotifyMessage({ muted: !!thread?.muted, isActiveThread: messages.activeThreadId === threadId })) {
    return;
  }

  const label = messages.isTactical(threadId) ? (thread?.alias || msg.thread_key) : msg.from_call;
  const href = `#/messages?thread=${encodeURIComponent(threadId)}`;
  const title = `New message from ${label}`;
  const body = (msg.text || '').slice(0, 140);
  notificationsLogStore.add({ kind: 'message', title, body, href });
  if (notificationPrefsState.toastEnabled) {
    notifications.push({ kind: 'message', title, body, href });
  }
  fireOsNotification(title, msg.text, () => {
    window.location.hash = href;
  });
  notificationSoundState.message.play();
}

/** Apply a single MessageChange frame to the store. */
function applyChange(change) {
  if (!change || typeof change.id === 'undefined') return;
  if (change.kind === 'deleted') {
    messages.markDeleted(change.id);
    return;
  }
  if (change.message) {
    messages.upsertMessage(change.message);
    maybeNotifyInbound(change.message);
  }
}

async function fetchDelta() {
  const cursor = messages.lastCursor || '';
  const resp = await listMessages(cursor ? { cursor } : undefined);
  if (!resp) return;
  if (resp.cursor) messages.lastCursor = resp.cursor;
  if (Array.isArray(resp.changes)) {
    for (const c of resp.changes) applyChange(c);
  }
}

async function refreshConversations() {
  try {
    const summaries = await listConversations({ limit: 200 });
    messages.refreshConversations(summaries || []);
  } catch (err) {
    // Non-fatal — the per-message delta path carries real-time
    // updates; the rollup is a periodic reconciliation.
    console.warn('[messagesTransport] conversations refresh failed:', err);
  }
}

async function refreshTacticals() {
  try {
    const list = await listTacticals();
    messages.setTacticals(list || []);
  } catch (err) {
    console.warn('[messagesTransport] tacticals refresh failed:', err);
  }
}

// --- Polling path --------------------------------------------------

function schedulePoll(delayMs) {
  if (stopped) return;
  clearTimeout(pollTimer);
  pollTimer = setTimeout(runPoll, delayMs);
}

async function runPoll() {
  if (stopped) return;
  try {
    await fetchDelta();
    currentBackoff = POLL_BASE_MS; // reset on success
    messages.transportStatus = 'polling';
  } catch (err) {
    currentBackoff = Math.min(currentBackoff * 2, POLL_MAX_MS);
    messages.transportStatus = 'error';
    console.warn(`[messagesTransport] poll failed; retrying in ${currentBackoff}ms`, err);
  }
  schedulePoll(currentBackoff);
}

// --- SSE path ------------------------------------------------------

function openSSE() {
  if (stopped) return;
  try {
    es = new EventSource('/api/messages/events');
  } catch (err) {
    console.warn('[messagesTransport] SSE construct failed, falling back to polling:', err);
    startPolling();
    return;
  }

  // The backend emits named events (`event: message.received` etc.)
  // carrying a MessageChange JSON payload. We handle the catch-all
  // onmessage AND the named-event listeners so either wire shape works.
  const onEvent = (ev) => {
    try {
      const data = JSON.parse(ev.data);
      applyChange(data);
    } catch (err) {
      console.warn('[messagesTransport] SSE parse failed:', err, ev.data);
    }
  };
  es.onmessage = onEvent;
  for (const name of ['message.received', 'message.updated', 'message.deleted', 'message.acked']) {
    es.addEventListener(name, onEvent);
  }

  es.onopen = () => {
    messages.transportStatus = 'sse';
    esBackoff = POLL_BASE_MS;
  };
  es.onerror = () => {
    // Browser will auto-reconnect on connection drops (retry: interval
    // from the server), but we close + schedule an explicit reopen to
    // apply our own exponential backoff on persistent failures.
    messages.transportStatus = 'error';
    try { es?.close(); } catch { /* ignore */ }
    es = null;
    if (stopped) return;
    esBackoff = Math.min(esBackoff * 2, POLL_MAX_MS);
    setTimeout(openSSE, esBackoff);
    // While SSE is down, run one catch-up poll against the cursor so
    // we don't miss a delta window — but don't spin up the poll loop.
    fetchDelta().catch(() => {});
  };
}

function startPolling() {
  schedulePoll(0);
}

// --- Public API ----------------------------------------------------

/**
 * Start the transport. Safe to call multiple times; subsequent calls
 * are no-ops. Launches an initial snapshot (/tactical + /conversations)
 * then either an SSE connection or a polling loop against /messages.
 */
export function start() {
  if (started || stopped) return;
  started = true;

  // Kick off snapshots concurrently; components can render partial
  // state as each promise resolves.
  refreshTacticals();
  refreshConversations();

  // Periodic rollup reconciliation — independent of delta cadence.
  conversationsTimer = setInterval(refreshConversations, CONVERSATIONS_REFRESH_MS);

  if (shouldUseSSE()) {
    openSSE();
    // One initial cursor fetch so a freshly-loaded tab is in sync even
    // if the first SSE frame takes a beat to arrive.
    fetchDelta().catch(() => {});
  } else {
    startPolling();
  }
}

/**
 * Stop the transport and tear down all timers + the EventSource.
 * Exposed mainly for Vite HMR / tests — production never calls stop().
 */
export function stop() {
  stopped = true;
  started = false;
  clearTimeout(pollTimer);
  pollTimer = null;
  clearInterval(conversationsTimer);
  conversationsTimer = null;
  if (es) {
    try { es.close(); } catch { /* ignore */ }
    es = null;
  }
  messages.transportStatus = 'idle';
}

/** Expose an explicit SSE entrypoint so callers can opt in programmatically. */
export function connectSSE() {
  if (typeof EventSource === 'undefined') {
    console.warn('[messagesTransport] EventSource unavailable; staying on poll');
    return false;
  }
  openSSE();
  return true;
}

/** Re-fetch the /conversations rollup on demand (e.g. after compose). */
export function refreshNow() {
  refreshTacticals();
  refreshConversations();
  fetchDelta().catch(() => {});
}

// Vite HMR: without this, editing this file (or anything it imports)
// while `npm run dev` is running leaves the OLD module instance's
// timers/EventSource running forever alongside the new one — started
// never gets reset to false on the stale copy, so every edit stacks
// another live poll loop, each independently firing its own
// notifications for the same events (2026-08-01, found via
// stationNewTransport.js's identical gap producing duplicate
// notifications during a long dev-testing session). A production
// build has no import.meta.hot, so this is a no-op there.
if (import.meta.hot) {
  import.meta.hot.dispose(() => stop());
}
