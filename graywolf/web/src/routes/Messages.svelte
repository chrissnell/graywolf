<script>
  // /messages — top-level Messages page.
  //
  // Responsibilities:
  //   - Read query params (`thread`, `compose`) and drive the
  //     active-thread selection + "new message" modal.
  //   - Own the two-column grid shell (desktop/tablet) that
  //     collapses to a single-pane slide-stack on mobile.
  //   - Toggle `body.messages-thread-open` on mobile so the sidebar
  //     60 px bottom-bar hides inside a thread view. The CSS rule is
  //     declared in app.css.
  //   - Manage the optimistic send flow: generate client_id,
  //     store the pending bubble, POST /messages, reconcile on 202.
  //   - Keyboard shortcuts: `/` to focus compose, Ctrl/Cmd+↑↓ to
  //     cycle visible threads, Ctrl/Cmd+Enter send (handled in
  //     ComposeBar), Esc close drawers. Registered on mount,
  //     unregistered on leave — other routes don't want the
  //     cycle-thread binding stealing their input.
  //   - Emit screen-reader-only announcements for new inbound
  //     messages, coalesced over a 3 s window.

  import { onMount, tick } from 'svelte';
  import { push, querystring, location } from 'svelte-spa-router';
  import ConversationList from '../components/messages/ConversationList.svelte';
  import MessageThread from '../components/messages/MessageThread.svelte';
  import TacticalSettings from '../components/messages/TacticalSettings.svelte';
  import ComposeNewModal from '../components/messages/ComposeNewModal.svelte';
  import EmptyStates from '../components/messages/EmptyStates.svelte';
  import { messages as store } from '../lib/messagesStore.svelte.js';
  import { sendMessage } from '../api/messages.js';
  import { DEFAULT_MAX_MESSAGE_TEXT } from '../lib/settings/messages-preferences-store.svelte.js';
  import { refreshNow } from '../lib/messagesTransport.js';
  import { toasts } from '../lib/stores.js';

  // ---------- query param parsing ----------
  let qs = $state('');
  let path = $state('');

  $effect(() => {
    const unsub = querystring.subscribe((v) => { qs = v || ''; });
    return unsub;
  });
  $effect(() => {
    const unsub = location.subscribe((v) => { path = v || ''; });
    return unsub;
  });

  const params = $derived.by(() => {
    const p = new URLSearchParams(qs);
    return {
      thread: p.get('thread') || '',
      compose: p.get('compose') || '',
    };
  });

  const inSettings = $derived(path === '/messages/tactical');

  // Drive active thread from the query param.
  $effect(() => {
    const t = params.thread;
    if (t) {
      store.setActiveThread(t);
    } else if (!inSettings) {
      store.setActiveThread(null);
    }
  });

  // ---------- prime data on mount ----------
  onMount(() => {
    refreshNow();
  });

  // ---------- responsive breakpoint ----------
  let isMobile = $state(false);
  onMount(() => {
    const mq = window.matchMedia('(max-width: 767px)');
    const apply = () => isMobile = mq.matches;
    apply();
    mq.addEventListener?.('change', apply);
    return () => mq.removeEventListener?.('change', apply);
  });

  // On mobile, hide the sidebar 60 px bottom-bar while inside a thread.
  $effect(() => {
    if (!document?.body) return;
    const inThreadOnMobile = isMobile && !!store.activeThreadId;
    document.body.classList.toggle('messages-thread-open', inThreadOnMobile);
    return () => document.body.classList.remove('messages-thread-open');
  });

  // ---------- compose modal ----------
  let composeOpen = $state(false);
  $effect(() => {
    // `?compose=1` → generic new-message modal.
    // `?compose=tactical:NET` → navigate into the tactical thread
    //   with compose pinned (the thread's own compose bar covers
    //   this; we just switch `?thread=` and strip compose).
    const c = params.compose;
    if (!c) return;
    if (c === '1') {
      composeOpen = true;
    } else if (c.startsWith('tactical:')) {
      const key = c.slice('tactical:'.length);
      const threadId = `tactical:${key}`;
      const sp = new URLSearchParams(qs);
      sp.delete('compose');
      sp.set('thread', threadId);
      push(`/messages?${sp.toString()}`);
    }
  });

  function closeCompose() {
    composeOpen = false;
    const sp = new URLSearchParams(qs);
    if (sp.has('compose')) {
      sp.delete('compose');
      const q = sp.toString();
      push(q ? `/messages?${q}` : '/messages');
    }
  }

  // ---------- thread selection ----------
  function selectThread(thread) {
    if (!thread?.threadId) return;
    if (path.startsWith('/messages/tactical')) {
      push(`/messages?thread=${encodeURIComponent(thread.threadId)}`);
    } else {
      const sp = new URLSearchParams(qs);
      sp.set('thread', thread.threadId);
      sp.delete('compose');
      push(`/messages?${sp.toString()}`);
    }
  }

  function openDm(callsign) {
    if (!callsign) return;
    const threadId = `dm:${callsign.toUpperCase()}`;
    push(`/messages?thread=${encodeURIComponent(threadId)}`);
  }

  function goBackToList() {
    // On mobile, back chevron pops us to the list (same URL minus ?thread).
    const sp = new URLSearchParams(qs);
    sp.delete('thread');
    const q = sp.toString();
    push(q ? `/messages?${q}` : '/messages');
  }

  function openCompose() {
    const sp = new URLSearchParams(qs);
    sp.set('compose', '1');
    push(`/messages?${sp.toString()}`);
  }

  function gotoTacticalSettings() {
    push('/messages/tactical');
  }

  // ---------- current thread ----------
  const activeThreadId = $derived(store.activeThreadId);
  const activeThread = $derived.by(() => {
    const id = activeThreadId;
    if (!id) return null;
    const found = store.conversations.get(id);
    if (found) return found;
    // If the store hasn't ingested this thread yet (deep link to a
    // brand-new peer), synthesize a minimal thread shell so the
    // MessageThread can render + fetch history.
    const [kind, ...rest] = id.split(':');
    const key = rest.join(':');
    if (!kind || !key) return null;
    return {
      threadId: id,
      kind,
      key,
      unreadCount: 0,
      totalCount: 0,
    };
  });

  // ---------- compose / optimistic send ----------
  async function optimisticSend(text, to) {
    if (!to || !text) return;
    const clientId = (typeof crypto !== 'undefined' && crypto.randomUUID)
      ? crypto.randomUUID()
      : `c-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

    // Figure out destination + thread for the optimistic bubble.
    const threadKind = store.tacticals.has(to.toUpperCase()) ? 'tactical' : 'dm';
    const threadKey = to.toUpperCase();
    const nowIso = new Date().toISOString();
    const optimistic = {
      id: undefined,
      msg_id: clientId,
      direction: 'out',
      from_call: '',
      to_call: threadKey,
      peer_call: threadKey,
      thread_kind: threadKind,
      thread_key: threadKey,
      text,
      unread: false,
      sent_at: nowIso,
      created_at: nowIso,
      status: 'pending',
      source: '',
      // Mirror the backend DTO derivation (len(text) > default cap) so
      // the outbound "extended" badge renders on the optimistic bubble
      // too — the server echo would set this anyway, but computing
      // it locally avoids a flicker-in on long sends.
      extended: (text?.length || 0) > DEFAULT_MAX_MESSAGE_TEXT,
    };
    store.addPendingSend(clientId, optimistic);

    try {
      const res = await sendMessage({ to: threadKey, text, client_id: clientId });
      store.reconcilePending(clientId, res);
      return res;
    } catch (err) {
      store.pendingByClientId.delete(clientId);
      toasts.error(err?.message || 'Send failed');
      throw err;
    }
  }

  async function handleThreadSend(text, to) {
    await optimisticSend(text, to);
    refreshNow();
  }

  async function handleNewSend(text, to) {
    const res = await optimisticSend(text, to);
    refreshNow();
    // Navigate to the thread we just opened.
    const threadKind = store.tacticals.has(to.toUpperCase()) ? 'tactical' : 'dm';
    const threadId = `${threadKind}:${to.toUpperCase()}`;
    push(`/messages?thread=${encodeURIComponent(threadId)}`);
    return res;
  }

  // ---------- visible threads (for keyboard nav + empty detection) ----------
  /** @type {Array<any>} */
  let visibleThreads = $state([]);
  /** @type {Map<string, HTMLElement>} */
  const rowRefs = new Map();

  const hasAnyThread = $derived.by(() => {
    return store.conversations.size > 0;
  });

  // ---------- keyboard shortcuts ----------
  function isTextInput(el) {
    if (!el) return false;
    const tag = el.tagName;
    if (tag === 'TEXTAREA' || tag === 'INPUT') return true;
    if (el.isContentEditable) return true;
    return false;
  }

  function cycleThread(delta) {
    const current = store.activeThreadId;
    const arr = visibleThreads;
    if (arr.length === 0) return;
    let idx = arr.findIndex(t => t.threadId === current);
    if (idx < 0) idx = 0;
    else idx = (idx + delta + arr.length) % arr.length;
    const next = arr[idx];
    if (!next) return;
    push(`/messages?thread=${encodeURIComponent(next.threadId)}`);
    // Flash the keyboard-focus outline on the row so the user can
    // see where they landed — without this, "did my shortcut do
    // anything?" is a real question on dense lists.
    tick().then(() => {
      const el = rowRefs.get(next.threadId);
      if (!el) return;
      el.classList.add('is-keyboard-focused');
      setTimeout(() => el.classList.remove('is-keyboard-focused'), 1500);
      el.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' });
    });
  }

  function handleKey(e) {
    // Never steal input from text fields.
    if (isTextInput(e.target) && !(e.ctrlKey || e.metaKey)) return;
    // `/` quick-focus compose.
    if (e.key === '/' && !isTextInput(e.target)) {
      e.preventDefault();
      const ta = document.querySelector('[data-testid="compose-textarea"]');
      if (ta) (ta).focus({ preventScroll: true });
      return;
    }
    // Ctrl/Cmd+↑↓ prev/next thread — only when focus is outside textarea.
    if ((e.ctrlKey || e.metaKey) && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
      if (isTextInput(e.target)) return; // don't collide with start/end of line
      e.preventDefault();
      cycleThread(e.key === 'ArrowDown' ? 1 : -1);
    }
  }

  onMount(() => {
    window.addEventListener('keydown', handleKey);
    return () => window.removeEventListener('keydown', handleKey);
  });

  // ---------- a11y inbound announcements ----------
  // Visually-hidden live-region node; we coalesce inbound events
  // into a single polite summary every 3 s. Muted tactical threads
  // never announce.
  let announcementText = $state('');
  /** @type {Map<string, number>} */
  const announceBuffer = new Map();
  let announceTimer = null;

  function announce() {
    if (announceBuffer.size === 0) {
      announcementText = '';
      announceTimer = null;
      return;
    }
    const entries = [...announceBuffer.entries()]; // [call, count]
    entries.sort((a, b) => b[1] - a[1]);
    const total = entries.reduce((n, [, c]) => n + c, 0);
    let msg;
    if (total === 1) {
      msg = `New message from ${entries[0][0]}`;
    } else if (entries.length === 1) {
      msg = `${total} new messages from ${entries[0][0]}`;
    } else {
      const first = entries.slice(0, 2).map(([c]) => c).join(' and ');
      const others = entries.length - 2;
      msg = others > 0
        ? `${total} new messages from ${first} and ${others} other${others === 1 ? '' : 's'}`
        : `${total} new messages from ${first}`;
    }
    announcementText = msg;
    announceBuffer.clear();
    announceTimer = null;
  }

  function noteInbound(call, threadId) {
    if (!call) return;
    // Muted threads don't announce.
    const th = store.conversations.get(threadId);
    if (th?.muted) return;
    announceBuffer.set(call, (announceBuffer.get(call) || 0) + 1);
    if (!announceTimer) {
      announceTimer = setTimeout(announce, 3_000);
    }
  }

  // Subscribe to store pending-size + conversation rollups and fire
  // announcements when an unread-count advances on a thread we're
  // not already viewing at the bottom.
  /** @type {Map<string, number>} */
  const unreadCache = new Map();
  $effect(() => {
    for (const [id, t] of store.conversations.entries()) {
      const prev = unreadCache.get(id) || 0;
      const cur = t.unreadCount || 0;
      if (cur > prev && t.lastSenderCall && t.threadId !== store.activeThreadId) {
        noteInbound(t.lastSenderCall, t.threadId);
      }
      unreadCache.set(id, cur);
    }
  });

  // When showing the thread pane at mobile breakpoint, we use a
  // slide transform; otherwise the grid renders both panes.
  const showList = $derived(!isMobile || !activeThreadId);
  const showThread = $derived(!isMobile || !!activeThreadId);
</script>

<div class="messages-shell" class:mobile={isMobile} class:has-thread={!!activeThreadId}>
  <!-- screen-reader-only live region for inbound summaries -->
  <div class="sr-only" role="status" aria-live="polite" data-testid="msg-announcer">
    {announcementText}
  </div>

  {#if showList}
    <div class="pane list-pane">
      <ConversationList
        activeThreadId={activeThreadId}
        onSelect={selectThread}
        onNew={openCompose}
        onManageTactical={gotoTacticalSettings}
        bind:visibleThreads
      />
    </div>
  {/if}

  {#if showThread}
    <div class="pane main-pane">
      {#if inSettings}
        <TacticalSettings />
      {:else if activeThread}
        <MessageThread
          thread={activeThread}
          onBack={goBackToList}
          onOpenDm={openDm}
          onCompose={handleThreadSend}
          {isMobile}
        />
      {:else if !hasAnyThread}
        <EmptyStates onNew={openCompose} onAddTactical={gotoTacticalSettings} />
      {:else}
        <div class="placeholder">
          <p>Select a conversation from the list.</p>
        </div>
      {/if}
    </div>
  {/if}
</div>

<ComposeNewModal
  bind:open={composeOpen}
  onSend={handleNewSend}
  onClose={closeCompose}
/>

<style>
  .messages-shell {
    display: grid;
    grid-template-columns: 340px minmax(0, 1fr);
    height: 100%;
    width: 100%;
    overflow: hidden;
    background: var(--color-bg);
  }
  @media (max-width: 1023px) {
    .messages-shell {
      grid-template-columns: 280px minmax(0, 1fr);
    }
  }
  @media (max-width: 767px) {
    .messages-shell {
      grid-template-columns: 1fr;
    }
  }

  .pane {
    min-width: 0;
    min-height: 0;
    height: 100%;
    position: relative;
    overflow: hidden;
  }
  .list-pane {
    display: flex;
    flex-direction: column;
  }
  .main-pane {
    display: flex;
    flex-direction: column;
    background: var(--color-bg);
    overflow: hidden;
  }

  @media (max-width: 767px) {
    .pane {
      grid-row: 1;
      grid-column: 1;
    }
  }

  .placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--color-text-muted);
    padding: 48px;
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
</style>
