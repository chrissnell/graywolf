// Sidebar-facing summary store for the AX.25 Terminal entry. Mirrors
// the Messages unread store: one $derived total recomputed whenever
// any session's unreadBytes / focused flags move.
//
// The actual session state is owned by lib/terminal/sessions.svelte.js
// (above the route component so it survives navigation). This file
// only projects a single number for the Sidebar NotificationBadge and
// a hasUnread boolean for any callers that just want the dot.

import { terminalSessions } from '../terminal/sessions.svelte.js';

export const terminalSidebar = {
  // unreadTotal is the sum of unreadBytes across every session that
  // is not currently focused. A session goes "focused" only when the
  // operator is on /terminal, on its tab, and the document is
  // visible -- so backgrounded tabs and other routes still accrue.
  get unreadTotal() {
    let n = 0;
    for (const sess of terminalSessions.list()) {
      if (!sess.state.focused) n += sess.state.unreadBytes;
    }
    return n;
  },

  get hasUnread() {
    return this.unreadTotal > 0;
  },
};
