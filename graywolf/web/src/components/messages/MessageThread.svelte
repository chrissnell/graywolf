<script>
  // The right pane — shows a thread's bubbles, renders the sticky
  // compose bar, owns the IntersectionObserver that marks inbound
  // bubbles read.
  //
  // Props:
  //   - thread        — store Thread (or null if nothing selected)
  //   - onBack        — mobile back chevron handler
  //   - onOpenDm      — navigate to a DM thread (for participant
  //                     tap-through and "reply privately")
  //   - onCompose     — compose callback (text, to) — parent wires
  //                     optimistic send + refreshNow()
  //   - onOpenMeta    — (msg) => void
  //   - announceInbound — optional hook the parent wires to the
  //                     a11y live-region coalescer so a new
  //                     incoming bubble contributes to the 3 s
  //                     summary. We don't manage announcement
  //                     inside the thread because the user may not
  //                     be on the thread when the message lands.

  import { onMount, tick } from 'svelte';
  import { Button, EmptyState, Icon, ScrollArea } from '@chrissnell/chonky-ui';
  import ThreadHeader from './ThreadHeader.svelte';
  import MessageBubble from './MessageBubble.svelte';
  import MessageContextMenu from './MessageContextMenu.svelte';
  import MessageMetaPanel from './MessageMetaPanel.svelte';
  import ComposeBar from './ComposeBar.svelte';
  import { dayHeader, dayKey } from './time.js';
  import { messages as store } from '../../lib/messagesStore.svelte.js';
  import {
    listMessages, markRead, markUnread, resendMessage,
  } from '../../api/messages.js';
  import { refreshNow } from '../../lib/messagesTransport.js';

  /** @type {{
   *    thread: any | null,
   *    onBack?: () => void,
   *    onOpenDm?: (call: string) => void,
   *    onCompose?: (text: string, to: string) => Promise<any>,
   *    isMobile?: boolean,
   *  }}
   */
  let {
    thread,
    onBack,
    onOpenDm,
    onCompose,
    isMobile = false,
  } = $props();

  const isTactical = $derived(thread?.kind === 'tactical');
  const threadId = $derived(thread?.threadId || '');

  // Stable, unique key for {#each}. Persisted rows use their DB primary
  // key (unique); optimistic bubbles use the client-generated msg_id
  // (UUID). APRS msg_id alone is NOT unique across a thread — peers
  // recycle the "001"..."999" counter, so two distinct rows from the
  // same peer can legitimately share the same msg_id and a naive key
  // would throw `each_key_duplicate` and blank the bubble list.
  function bubbleKey(m, i) {
    if (m?.id != null) return `id:${m.id}`;
    if (m?.msg_id) return `mid:${m.msg_id}`;
    return `i:${i}`;
  }

  // --- Local messages state (per-thread, not part of global store).
  /** @type {Array<any>} */
  let msgs = $state([]);
  let loading = $state(false);
  let scrollEl = $state(null);
  let scrolledUp = $state(false);

  async function fetchThread() {
    if (!thread) { msgs = []; return; }
    loading = true;
    try {
      const env = await listMessages({
        thread_kind: thread.kind,
        thread_key: thread.key,
        limit: 200,
      });
      const list = (env?.changes || []).map(c => c.message).filter(Boolean);
      // Newest-first? API returns newest-first per cursor docs; we
      // want chronological ascending, so reverse.
      list.sort((a, b) => {
        const at = Date.parse(a.sent_at || a.received_at || a.created_at || 0) || 0;
        const bt = Date.parse(b.sent_at || b.received_at || b.created_at || 0) || 0;
        return at - bt;
      });
      msgs = list;
      await tick();
      scrollToBottom(false);
    } catch {
      // leave existing msgs; surface later if needed
    } finally {
      loading = false;
    }
  }

  // Re-fetch on thread change.
  $effect(() => {
    void threadId;
    fetchThread();
  });

  // Merge optimistic pending bubbles whose thread matches.
  const allBubbles = $derived.by(() => {
    const arr = [...msgs];
    if (thread) {
      for (const p of store.pendingByClientId.values()) {
        if (p && p.thread_kind === thread.kind && p.thread_key === thread.key) {
          // Only add if not already represented by a server row.
          const exists = arr.some(m => m.msg_id && p.msg_id && m.msg_id === p.msg_id);
          if (!exists) arr.push(p);
        }
      }
    }
    arr.sort((a, b) => {
      const at = Date.parse(a.sent_at || a.received_at || a.created_at || 0) || 0;
      const bt = Date.parse(b.sent_at || b.received_at || b.created_at || 0) || 0;
      return at - bt;
    });
    return arr;
  });

  // Apply cluster rules to each bubble for sender-label / stripe-monogram rendering.
  // Cluster break: same sender + incoming + within 120s + no day-sep +
  // no intervening other-sender or outgoing bubble. Repeat label every
  // 5 bubbles inside long clusters.
  const clusterInfo = $derived.by(() => {
    const info = new Map();
    let clusterSender = '';
    let clusterCount = 0;
    let lastTs = 0;
    let lastDay = '';
    for (let i = 0; i < allBubbles.length; i++) {
      const m = allBubbles[i];
      const inc = m.direction === 'in';
      const sender = m.from_call || '';
      const t = Date.parse(m.sent_at || m.received_at || m.created_at || 0) || 0;
      const thisDay = dayKey(m.sent_at || m.received_at || m.created_at);
      const sameCluster = inc
        && isTactical
        && sender === clusterSender
        && clusterSender !== ''
        && thisDay === lastDay
        && (t - lastTs) <= 120_000;
      if (!sameCluster) {
        clusterSender = inc ? sender : '';
        clusterCount = inc ? 1 : 0;
        const showLabel = inc && isTactical; // first in cluster
        info.set(i, { showSenderLabel: !!showLabel, showAvatar: !!showLabel });
      } else {
        clusterCount++;
        // Repeat label every 5 bubbles: indexes 5, 10, 15 inside cluster
        // (cluster is 1-indexed: 1st was the first, so 5th = count 5)
        const repeat = clusterCount % 5 === 0;
        info.set(i, { showSenderLabel: repeat, showAvatar: repeat });
      }
      lastTs = t;
      lastDay = thisDay;
    }
    return info;
  });

  // Build day-separator positions.
  const daySeps = $derived.by(() => {
    const seps = new Set();
    let lastDay = '';
    for (let i = 0; i < allBubbles.length; i++) {
      const m = allBubbles[i];
      const key = dayKey(m.sent_at || m.received_at || m.created_at);
      if (key !== lastDay) {
        seps.add(i);
        lastDay = key;
      }
    }
    return seps;
  });

  function scrollToBottom(smooth = true) {
    if (!scrollEl) return;
    const viewport = scrollEl.querySelector?.('[data-scroll-viewport]') || scrollEl;
    viewport.scrollTo?.({ top: viewport.scrollHeight, behavior: smooth ? 'smooth' : 'auto' });
  }

  function onScroll(e) {
    const el = e.currentTarget;
    const gap = el.scrollHeight - (el.scrollTop + el.clientHeight);
    scrolledUp = gap > 120;
  }

  // Observe near-bottom on new bubble to auto-scroll when user is pinned.
  $effect(() => {
    void allBubbles.length;
    if (!scrollEl) return;
    if (!scrolledUp) {
      tick().then(() => scrollToBottom(true));
    }
  });

  // --- IntersectionObserver: mark inbound messages as read on dwell.
  /** @type {Map<number, number>} */
  const dwellStart = new Map();
  /** @type {Set<number>} */
  const batchedIds = new Set();
  let batchTimer = null;

  function flushBatch() {
    for (const id of batchedIds) {
      markRead(id).catch(() => { /* ignore */ });
    }
    batchedIds.clear();
    batchTimer = null;
  }

  function scheduleFlush() {
    if (batchTimer) return;
    batchTimer = setTimeout(flushBatch, 2_000);
  }

  let io = null;
  /** @type {Map<Element, any>} */
  const bubbleByEl = new Map();

  function rebuildIO() {
    io?.disconnect?.();
    if (!scrollEl) return;
    const root = scrollEl.querySelector?.('[data-scroll-viewport]') || scrollEl;
    io = new IntersectionObserver((entries) => {
      if (typeof document !== 'undefined' && document.visibilityState !== 'visible') return;
      for (const entry of entries) {
        const m = bubbleByEl.get(entry.target);
        if (!m || m.direction !== 'in' || !m.unread) continue;
        if (entry.isIntersecting && entry.intersectionRatio >= 0.6) {
          if (!dwellStart.has(m.id)) dwellStart.set(m.id, Date.now());
          const started = dwellStart.get(m.id) || 0;
          setTimeout(() => {
            if (!dwellStart.has(m.id)) return;
            if (Date.now() - started < 500) return;
            batchedIds.add(m.id);
            dwellStart.delete(m.id);
            scheduleFlush();
          }, 520);
        } else {
          dwellStart.delete(m.id);
        }
      }
    }, { root, threshold: 0.6 });
    for (const [el, m] of bubbleByEl) {
      if (el && m) io.observe(el);
    }
  }

  function observe(el, msg) {
    if (!el || !msg) return;
    bubbleByEl.set(el, msg);
    io?.observe?.(el);
  }
  function unobserve(el) {
    if (!el) return;
    io?.unobserve?.(el);
    bubbleByEl.delete(el);
  }

  onMount(() => {
    rebuildIO();
    const visHandler = () => {
      if (document.visibilityState === 'visible') rebuildIO();
    };
    document.addEventListener('visibilitychange', visHandler);
    return () => {
      io?.disconnect?.();
      document.removeEventListener('visibilitychange', visHandler);
      if (batchTimer) {
        clearTimeout(batchTimer);
        flushBatch();
      }
    };
  });

  // Rebuild observer when scrollEl binds.
  $effect(() => {
    if (scrollEl) rebuildIO();
  });

  // --- Send flow (wired to parent).
  async function handleSend(body, to) {
    await onCompose?.(body, to);
    // Refresh local msgs from server shortly after 202 lands —
    // the optimistic bubble already appears via store.pendingByClientId.
    refreshNow();
    fetchThread();
  }

  // --- Context menu wiring.
  let menuOpen = $state(false);
  let menuX = $state(0);
  let menuY = $state(0);
  /** @type {any} */
  let menuMsg = $state(null);
  function openMenu(x, y, msg) {
    menuMsg = msg;
    menuX = x;
    menuY = y;
    menuOpen = true;
  }
  async function onCopyText()  { try { await navigator.clipboard?.writeText?.(menuMsg?.text || ''); } catch {} }
  async function onCopyRaw()   { try { await navigator.clipboard?.writeText?.(menuMsg?.text || ''); } catch {} }
  async function onCopyCall()  { try { await navigator.clipboard?.writeText?.(menuMsg?.from_call || menuMsg?.to_call || ''); } catch {} }
  async function onMarkUnread(){
    if (menuMsg?.id != null) {
      await markUnread(menuMsg.id).catch(() => {});
      refreshNow();
    }
  }
  async function onResend()    {
    if (menuMsg?.id != null) {
      await resendMessage(menuMsg.id).catch(() => {});
      refreshNow();
      fetchThread();
    }
  }
  // Direct-click resend from the bubble's status icon (not via context
  // menu). Same as onResend but takes the msg arg directly, bypassing
  // the menu-state closure.
  async function resendDirect(/** @type {any} */ m) {
    if (m?.id == null) return;
    await resendMessage(m.id).catch(() => {});
    refreshNow();
    fetchThread();
  }
  // --- Meta drawer wiring.
  let metaOpen = $state(false);
  /** @type {any} */
  let metaMsg = $state(null);
  function openMeta(msg) {
    metaMsg = msg;
    metaOpen = true;
  }

  // --- Mute toggle
  function onMuteToggle(muted) {
    if (thread) store.muteThread(thread.threadId, muted);
  }

  // --- Reply privately
  function replyPrivately(call) {
    if (call) onOpenDm?.(call);
  }
</script>

{#if !thread}
  <div class="empty-shell" data-testid="thread-empty-shell">
    <EmptyState>
      <Icon name="message-square" size="xl" />
      <h3>Select a conversation</h3>
      <p>Pick a thread from the list, or start a new one.</p>
    </EmptyState>
  </div>
{:else}
  <section class="thread-pane" data-testid="thread-pane">
    <ThreadHeader
      {thread}
      {isTactical}
      {isMobile}
      {onBack}
      {onMuteToggle}
      onOpenDm={replyPrivately}
    />

    <div class="scroll-wrap">
      <div
        bind:this={scrollEl}
        class="scroll"
        data-scroll-viewport
        onscroll={onScroll}
      >
        {#if allBubbles.length === 0 && !loading}
          {#if isTactical}
            <div class="thread-empty" data-testid="tactical-empty-state">
              <EmptyState>
                <Icon name="radio-tower" size="xl" />
                <h3>No messages to {thread.key} yet</h3>
                <p>Your first reply will broadcast to everyone monitoring this tactical. Type below to get started.</p>
              </EmptyState>
            </div>
          {/if}
        {:else}
          <div class="bubbles">
            {#each allBubbles as m, i (bubbleKey(m, i))}
              {#if daySeps.has(i)}
                <div class="day-sep" role="separator">
                  <span>{dayHeader(m.sent_at || m.received_at || m.created_at)}</span>
                </div>
              {/if}
              {@const info = clusterInfo.get(i) || { showSenderLabel: false, showAvatar: false }}
              <MessageBubble
                msg={m}
                {isTactical}
                showSenderLabel={info.showSenderLabel}
                showAvatar={info.showAvatar}
                onMetaClick={openMeta}
                onReplyPrivate={replyPrivately}
                onContextMenu={openMenu}
                onResend={resendDirect}
                registerRef={(el) => el ? observe(el, m) : null}
              />
            {/each}
          </div>
        {/if}
      </div>

      {#if scrolledUp}
        <button
          type="button"
          class="jump-pill"
          onclick={() => scrollToBottom(true)}
          aria-label="Jump to latest"
          data-testid="jump-to-latest"
        >
          <Icon name="arrow-down" size="sm" />
          <span>Latest</span>
        </button>
      {/if}
    </div>

    <ComposeBar
      mode="thread"
      {isTactical}
      tacticalKey={isTactical ? thread.key : ''}
      tacticalAlias={isTactical ? (thread.alias || '') : ''}
      dmPeer={!isTactical ? thread.key : ''}
      threadHasMessages={allBubbles.length > 0}
      onSend={handleSend}
      autoFocus={true}
    />
  </section>

  <MessageContextMenu
    bind:open={menuOpen}
    x={menuX}
    y={menuY}
    msg={menuMsg}
    {isTactical}
    {onCopyText}
    {onCopyRaw}
    {onCopyCall}
    {onMarkUnread}
    {onResend}
    onReplyPrivate={replyPrivately}
  />
  <MessageMetaPanel bind:open={metaOpen} msg={metaMsg} />
{/if}

<style>
  .empty-shell {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    padding: 48px;
  }
  .empty-shell h3 {
    margin: 12px 0 4px;
    font-size: 16px;
    font-weight: 600;
  }
  .empty-shell p {
    color: var(--color-text-muted);
    font-size: 13px;
    margin: 0;
  }
  .thread-pane {
    position: relative;
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--color-bg);
    overflow: hidden;
  }

  .scroll-wrap {
    position: relative;
    flex: 1 1 auto;
    min-height: 0;
    overflow: hidden;
  }
  .scroll {
    height: 100%;
    overflow-y: auto;
    padding: 12px 0 140px;
  }
  .bubbles {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-width: 720px;
    margin: 0 auto;
    padding: 0 12px;
  }
  .thread-empty {
    padding: 48px 24px 140px;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .thread-empty h3 {
    margin: 12px 0 4px;
    font-size: 16px;
    font-weight: 600;
  }
  .thread-empty p {
    color: var(--color-text-muted);
    font-size: 13px;
    margin: 0;
    max-width: 360px;
    text-align: center;
  }

  .day-sep {
    display: flex;
    align-items: center;
    justify-content: center;
    margin: 12px 0 4px;
  }
  .day-sep span {
    padding: 2px 10px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: 999px;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 1px;
    text-transform: uppercase;
    color: var(--color-text-muted);
  }

  .jump-pill {
    position: absolute;
    bottom: 150px;
    left: 50%;
    transform: translateX(-50%);
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 12px 4px 10px;
    background: var(--color-surface-raised);
    color: var(--color-text);
    border: 1px solid var(--color-border);
    border-radius: 999px;
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: 12px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.4);
  }
  .jump-pill:hover { background: var(--color-primary-muted); color: var(--color-primary); }
</style>
