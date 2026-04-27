// Messages feature reactive store — Svelte 5 runes + SvelteMap.
//
// Shape:
//   conversations   threadId -> Thread summary + unreadCount + muted/archived
//   tacticals       callsign -> {alias, enabled, id}
//   activeThreadId  string | null
//   filter          'all' | 'unread' | 'groups' | 'sent-only'
//   searchQuery     string
//   pendingByClientId  clientId -> optimistic outbound bubble (replaced on 202)
//
// The transport layer (messagesTransport.js) is the sole writer; the
// UI reads reactively. Muted or archived threads are excluded from
// `unreadTotal` so the sidebar badge is an actionable, not merely
// informational, signal.
//
// Module-level singleton (`messages`) so every import sees the same
// state — the sidebar badge and the messages route share state without
// any prop plumbing.
//
// Reactivity note: a plain `$state(new Map())` does NOT proxy
// Map.set/delete mutations. SvelteMap from svelte/reactivity triggers
// reactivity on set/delete/clear, which is what we need for the
// unread-total getter to recompute when individual threads change.

import { SvelteMap, SvelteSet } from 'svelte/reactivity';

/**
 * @typedef {object} Thread
 * @property {string} threadId          `${kind}:${key}` composite
 * @property {string} kind              'dm' | 'tactical'
 * @property {string} key               peer callsign (DM) or tactical label
 * @property {string} [alias]           tactical alias (tactical only)
 * @property {number} unreadCount
 * @property {number} totalCount
 * @property {string} [lastAt]          RFC3339
 * @property {string} [lastSnippet]
 * @property {string} [lastSenderCall]  who sent the last-heard bubble
 * @property {number} [participantCount] tactical only
 * @property {boolean} [muted]          monitoring toggle off
 * @property {boolean} [archived]
 */

class MessagesStore {
  // threadId -> Thread
  conversations = new SvelteMap();
  // callsign -> {alias, enabled, id}
  tacticals = new SvelteMap();
  // clientId -> optimistic outbound row (awaiting server reconciliation)
  pendingByClientId = new SvelteMap();
  // threadId set selected by the inbox bulk-delete UI. Reactive so the
  // toolbar count + per-row checkbox state both stay in sync without
  // prop plumbing.
  selectedThreadIds = new SvelteSet();
  // id -> MessageResponse (sparse). Populated by upsertMessage for every
  // persisted row the transport hands us. The invite modal subscribes to
  // this map to drive per-chip ack state without spinning up its own
  // EventSource. Unbounded in principle, but each entry is ~500B and a
  // full page reload reseeds via /conversations + /messages. Fine.
  messageById = new SvelteMap();

  activeThreadId = $state(null);
  filter = $state('all');
  searchQuery = $state('');

  // Cursor held by the transport; exposed here so reconnection logic can
  // see the last acknowledged point. Not meant for component reads.
  lastCursor = $state('');

  // Transport lifecycle status — visible in dev tools; components may
  // ignore it. 'idle' | 'polling' | 'sse' | 'error'.
  transportStatus = $state('idle');

  /** Sum of unreadCount across non-muted, non-archived threads. */
  get unreadTotal() {
    let n = 0;
    for (const t of this.conversations.values()) {
      if (t.muted) continue;
      if (t.archived) continue;
      n += t.unreadCount || 0;
    }
    return n;
  }

  threadIdFor(kind, key) {
    return `${kind}:${key}`;
  }

  isTactical(threadId) {
    return typeof threadId === 'string' && threadId.startsWith('tactical:');
  }

  // --- Writers called by the transport layer -------------------------

  /**
   * Upsert a single message into its thread bucket. Does not fetch any
   * data — only updates the rollup counters and lastAt/snippet from the
   * message envelope the transport already has.
   * @param {object} msg MessageResponse DTO
   */
  upsertMessage(msg) {
    if (!msg || !msg.thread_kind || !msg.thread_key) return;
    const threadId = this.threadIdFor(msg.thread_kind, msg.thread_key);
    const existing = this.conversations.get(threadId);
    const thread = existing ? { ...existing } : {
      threadId,
      kind: msg.thread_kind,
      key: msg.thread_key,
      unreadCount: 0,
      totalCount: 0,
    };

    // Advance last-seen fields when this message is newer than what we
    // currently have. RFC3339 lexical compare is safe for Z-suffixed
    // timestamps the backend emits.
    const when = msg.sent_at || msg.received_at || msg.created_at || '';
    if (!thread.lastAt || when > thread.lastAt) {
      thread.lastAt = when;
      thread.lastSnippet = msg.text || thread.lastSnippet;
      thread.lastSenderCall = msg.from_call || thread.lastSenderCall;
    }

    // Unread tracking: incoming + unread=true contributes. The
    // authoritative rollup comes from /conversations; we only nudge
    // the counter so the badge is responsive between polls.
    if (msg.direction === 'in' && msg.unread) {
      // Only bump if the transport isn't telling us about the same id
      // twice; we don't track per-id here, so refreshConversations is
      // the reconciliation point. This nudge can overshoot briefly.
      thread.unreadCount = (thread.unreadCount || 0) + 1;
    }

    this.conversations.set(threadId, thread);

    // Stash the full row by id so per-bubble consumers (e.g. the invite
    // modal's per-chip state machine) can react to ack status changes
    // without reopening a thread. Only persisted rows have an id — the
    // optimistic pending bubbles live separately on pendingByClientId.
    if (typeof msg.id === 'number' && msg.id > 0) {
      this.messageById.set(msg.id, msg);
    }

    // Clear any pending optimistic bubble that this message reconciles.
    if (msg.msg_id && this.pendingByClientId.has(msg.msg_id)) {
      this.pendingByClientId.delete(msg.msg_id);
    }
  }

  /**
   * Remove a message's contribution to its thread. Because the store
   * only holds rollup counters (not per-message rows), the most reliable
   * correction is to trigger a conversations refresh at the transport
   * layer. For now we drop the thread if we know its id belongs there.
   * @param {number} id server message id
   */
  markDeleted(id) {
    // Rely on the next conversations rollup to reconcile counts. This
    // method exists so the transport can notify components that subscribe
    // to per-message state in Phase 7 without crashing today.
    void id;
  }

  /**
   * Record an optimistic outbound bubble before the server 202 lands.
   * @param {string} clientId  correlation id echoed by the server
   * @param {object} optimisticBubble shape matches MessageResponse
   */
  addPendingSend(clientId, optimisticBubble) {
    if (!clientId) return;
    this.pendingByClientId.set(clientId, optimisticBubble);
    // Also nudge the thread's lastAt/snippet so the conversation list
    // reorders immediately.
    this.upsertMessage(optimisticBubble);
  }

  /**
   * Reconcile a pending bubble with its persisted server row.
   * @param {string} clientId
   * @param {object} serverMessage MessageResponse
   */
  reconcilePending(clientId, serverMessage) {
    if (clientId && this.pendingByClientId.has(clientId)) {
      this.pendingByClientId.delete(clientId);
    }
    if (serverMessage) this.upsertMessage(serverMessage);
  }

  setActiveThread(threadId) {
    this.activeThreadId = threadId;
  }

  setFilter(f) {
    this.filter = f;
  }

  setSearchQuery(q) {
    this.searchQuery = q || '';
  }

  /**
   * Replace/merge conversation summaries from the authoritative rollup
   * endpoint. Existing local fields like `muted` (client-side monitoring
   * toggle) are preserved across refreshes.
   * @param {Array} summaries  dto.ConversationSummary[]
   */
  refreshConversations(summaries) {
    if (!Array.isArray(summaries)) return;
    const seen = new Set();
    for (const s of summaries) {
      if (!s.thread_kind || !s.thread_key) continue;
      const threadId = this.threadIdFor(s.thread_kind, s.thread_key);
      seen.add(threadId);
      const existing = this.conversations.get(threadId);
      /** @type {Thread} */
      const thread = {
        threadId,
        kind: s.thread_kind,
        key: s.thread_key,
        alias: s.alias,
        unreadCount: s.unread_count || 0,
        totalCount: s.total_count || 0,
        lastAt: s.last_at,
        lastSnippet: s.last_snippet,
        lastSenderCall: s.last_sender_call,
        participantCount: s.participant_count,
        archived: s.archived || false,
        // Preserve client-side muted flag across rollups.
        muted: existing?.muted ?? false,
      };
      this.conversations.set(threadId, thread);
    }
    // Drop threads the server no longer knows about. Keep pending
    // composes (pendingByClientId) regardless — they haven't been
    // persisted yet.
    for (const threadId of this.conversations.keys()) {
      if (!seen.has(threadId)) {
        this.conversations.delete(threadId);
        this.selectedThreadIds.delete(threadId);
      }
    }
  }

  /**
   * Install the tactical-callsigns directory. Called on transport init
   * and after any CRUD mutation on /api/messages/tactical*.
   * @param {Array} list dto.TacticalCallsignResponse[]
   */
  setTacticals(list) {
    this.tacticals.clear();
    if (!Array.isArray(list)) return;
    for (const t of list) {
      if (!t.callsign) continue;
      this.tacticals.set(t.callsign, {
        id: t.id,
        alias: t.alias || '',
        enabled: t.enabled !== false,
      });
    }
  }

  /**
   * Mute/unmute a thread locally. Muted threads don't contribute to
   * `unreadTotal`. Persistence is a Phase 7 concern (either via a
   * future preferences field or localStorage) — this method makes the
   * in-session toggle reactive today.
   */
  muteThread(threadId, muted) {
    const t = this.conversations.get(threadId);
    if (!t) return;
    this.conversations.set(threadId, { ...t, muted: !!muted });
  }

  // --- Selection (inbox bulk-delete) --------------------------------

  toggleSelected(threadId, on) {
    if (!threadId) return;
    if (on) this.selectedThreadIds.add(threadId);
    else this.selectedThreadIds.delete(threadId);
  }

  /** Replace the selection set with the given thread IDs (or empty). */
  setSelection(threadIds) {
    this.selectedThreadIds.clear();
    if (!threadIds) return;
    for (const id of threadIds) this.selectedThreadIds.add(id);
  }

  clearSelection() {
    this.selectedThreadIds.clear();
  }
}

export const messages = new MessagesStore();
