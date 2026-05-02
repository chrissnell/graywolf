// Multi-session manager for the AX.25 terminal route.
//
// One createSession() result per active link, keyed by its id. Lives
// above any route component so navigating away from /terminal does
// NOT close active sessions: the WebSockets stay open, RX continues
// accumulating into unreadBytes, and the sidebar badge keeps counting.
//
// Concurrency cap is 6 -- matches every common BBS client and stays
// well under browser per-host WebSocket concurrency limits. Six
// keeps an active operator visually managable while preventing a
// runaway loop from exhausting browser handles.

import { toast } from '@chrissnell/chonky-ui';

import { createSession } from './session.svelte.js';

export const MAX_SESSIONS = 6;

const sessionsMap = new Map();
const route = $state({ value: '/' });
const activeTab = $state({ value: null });
const visibility = $state({ visible: true });
const sessionsState = $state({ ids: [] });

function refreshFocusFlags() {
  for (const [id, sess] of sessionsMap) {
    const focused =
      route.value === '/terminal' &&
      activeTab.value === id &&
      visibility.visible;
    if (focused && !sess.state.focused) {
      sess.clearUnread();
    }
    sess.state.focused = focused;
  }
}

function refreshIds() {
  sessionsState.ids = Array.from(sessionsMap.keys());
}

if (typeof document !== 'undefined') {
  visibility.visible = document.visibilityState === 'visible';
  document.addEventListener('visibilitychange', () => {
    visibility.visible = document.visibilityState === 'visible';
    if (!visibility.visible) {
      // Mark every session suspended so the StatusBar can show the
      // "Suspended" subtitle. iOS aggressively reaper-kills
      // backgrounded WS, so this signals to the operator that the
      // badge counter is authoritative even if the WS later silently
      // dies. On visibility return we attempt to ping; if the WS is
      // dead the next message attempt surfaces the error.
      for (const sess of sessionsMap.values()) {
        sess.state.suspended = true;
      }
    } else {
      for (const sess of sessionsMap.values()) {
        sess.state.suspended = false;
      }
    }
    refreshFocusFlags();
  });
}

if (typeof window !== 'undefined') {
  // Clean disconnect on browser close / tab close. The backend bridge
  // issues a clean DISC frame on the LAPB layer, then closes the WS.
  // This avoids leaking goroutines and gives the peer a proper
  // teardown rather than a TCP RST.
  window.addEventListener('beforeunload', () => {
    for (const sess of sessionsMap.values()) {
      try { sess.disconnect(); } catch { /* ignore */ }
    }
  });
}

export const terminalSessions = {
  ids: () => sessionsState.ids,
  count: () => sessionsMap.size,

  // open creates a new session bound to `initial`. Returns the new
  // session's id, or null if the cap has been reached (a chonky toast
  // is surfaced in that case so the caller does not have to).
  open(initial) {
    if (sessionsMap.size >= MAX_SESSIONS) {
      try {
        toast('Connection limit reached -- close a session to open another.', {
          duration: 4000,
        });
      } catch {
        // toast() requires a <Toaster /> mount; tolerate its absence.
      }
      return null;
    }
    const sess = createSession(initial);
    const id = sess.state.id;
    sessionsMap.set(id, sess);
    refreshIds();
    if (activeTab.value === null) activeTab.value = id;
    refreshFocusFlags();
    return id;
  },

  close(id) {
    const sess = sessionsMap.get(id);
    if (!sess) return;
    try { sess.disconnect(); } catch { /* ignore */ }
    try { sess.close(); } catch { /* ignore */ }
    sessionsMap.delete(id);
    refreshIds();
    if (activeTab.value === id) {
      const next = sessionsMap.keys().next();
      activeTab.value = next.done ? null : next.value;
      refreshFocusFlags();
    }
  },

  get(id) {
    return sessionsMap.get(id) ?? null;
  },

  list() {
    return Array.from(sessionsMap.values());
  },

  setActive(id) {
    if (id !== null && !sessionsMap.has(id)) return;
    activeTab.value = id;
    refreshFocusFlags();
  },

  activeId: () => activeTab.value,

  setRoute(path) {
    route.value = path;
    refreshFocusFlags();
  },
};
