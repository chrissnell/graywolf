<script>
  // The left pane of the Messages shell.
  //
  // Owns:
  //   - the 4-way mutually-exclusive filter pills
  //     (All | Unread | Groups | Sent-only). Mutually exclusive is a
  //     deliberate v1 constraint — flattens type × unread × direction
  //     into one row and gives up the "unread tactical" combo for
  //     simplicity. If operators need it, split into two pill groups
  //     in v2; DO NOT add a fourth axis here without revisiting the
  //     UX.
  //   - a throttled search input
  //   - list rendering, always split into a "Tactical" section
  //     (heading + Manage button + tactical threads) and a "Direct
  //     Messages" section (heading + DM threads). The Tactical heading
  //     and Manage button are always rendered so the management entry
  //     point is discoverable even when the operator has no tactical
  //     chats yet. The DMs section is hidden when the active filter
  //     is "Groups" (it would always be empty).
  //
  // Emits:
  //   - onSelect(thread)  — row clicked / keyboard-activated
  //   - onNew()           — "+" compose button clicked
  //   - onManageTactical()— footer link clicked
  //   - visibleThreads    — bound; parent consumes for keyboard
  //                          prev/next thread navigation so the
  //                          shortcut cycles the same order the user
  //                          sees (not the unfiltered list).

  import { AlertDialog, Button, Icon } from '@chrissnell/chonky-ui';
  // Note: the bulk-delete button is a hand-rolled <button>, NOT a
  // chonky Button — chonky's Button has internal padding that fights
  // align-items: center inside this toolbar row. Hand-rolling lets us
  // pin its height to the row height so checkbox + label + button line
  // up exactly on the row's vertical centerline.
  import { push } from 'svelte-spa-router';
  import { messages } from '../../lib/messagesStore.svelte.js';
  import { deleteMessageThread, deleteTactical } from '../../api/messages.js';
  import { refreshNow } from '../../lib/messagesTransport.js';
  import { toasts } from '../../lib/stores.js';
  import ConversationRow from './ConversationRow.svelte';

  /** @type {{
   *    activeThreadId?: string | null,
   *    onSelect?: (t: any) => void,
   *    onNew?: () => void,
   *    onManageTactical?: () => void,
   *    visibleThreads?: any[],
   *    rowRefs?: Map<string, HTMLElement>,
   * }}
   */
  let {
    activeThreadId = null,
    onSelect,
    onNew,
    onManageTactical,
    visibleThreads = $bindable([]),
    rowRefs = $bindable(new Map()),
  } = $props();

  // Local throttled mirror of store.searchQuery so typing doesn't
  // thrash a re-sort on every keystroke.
  let searchInput = $state(messages.searchQuery || '');
  let searchTimer;
  function onSearchInput(e) {
    searchInput = e.target.value;
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      messages.setSearchQuery(searchInput);
    }, 150);
  }

  const FILTERS = [
    { id: 'all',       label: 'All' },
    { id: 'unread',    label: 'Unread' },
    { id: 'groups',    label: 'Groups' },
    { id: 'sent-only', label: 'Sent' },
  ];

  function setFilter(f) {
    messages.setFilter(f);
  }

  // Derive a sorted array from the SvelteMap. Sort: lastAt desc,
  // unread-first tiebreak, then alpha on key.
  const allThreads = $derived.by(() => {
    const arr = [];
    for (const t of messages.conversations.values()) arr.push(t);
    arr.sort((a, b) => {
      const bt = b.lastAt ? Date.parse(b.lastAt) : 0;
      const at = a.lastAt ? Date.parse(a.lastAt) : 0;
      if (bt !== at) return bt - at;
      const bu = b.unreadCount || 0;
      const au = a.unreadCount || 0;
      if (bu !== au) return bu - au;
      return (a.key || '').localeCompare(b.key || '');
    });
    return arr;
  });

  const filter = $derived(messages.filter);
  const q = $derived((messages.searchQuery || '').trim().toUpperCase());

  const filteredThreads = $derived.by(() => {
    return allThreads.filter((t) => {
      if (t.archived) return false;
      if (filter === 'unread' && (!t.unreadCount || t.unreadCount <= 0)) return false;
      if (filter === 'groups' && t.kind !== 'tactical') return false;
      if (filter === 'sent-only') {
        // "Sent-only" = threads where our last action was outgoing.
        // We don't have a per-thread direction flag from the rollup;
        // approximate by "lastSenderCall matches our_call" — the
        // server resolves our_call into lastSenderCall when we sent
        // the last visible bubble. If lastSenderCall is empty, skip.
        // (This is a best-effort UX for v1 — see plan notes.)
        if (!t.lastSenderCall) return false;
      }
      if (q) {
        const hay = `${t.key || ''} ${t.alias || ''} ${t.lastSnippet || ''}`.toUpperCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });
  });

  // Always split into Tactical / DM buckets. The Tactical section
  // header + Manage button render even when there are zero tactical
  // threads — that's the entry point for creating one. The DMs
  // section is only rendered when the current filter can include DMs.
  const buckets = $derived.by(() => {
    const tacticals = [];
    const dms = [];
    for (const t of filteredThreads) {
      (t.kind === 'tactical' ? tacticals : dms).push(t);
    }
    return { tacticals, dms };
  });
  const showDmsSection = $derived(filter !== 'groups');

  // Keep the parent's visible-order mirror in sync so Ctrl/Cmd+↑↓
  // cycles the same list the user sees.
  $effect(() => {
    const arr = [];
    for (const t of buckets.tacticals) arr.push(t);
    if (showDmsSection) for (const t of buckets.dms) arr.push(t);
    visibleThreads = arr;
  });

  function handleSelect(t) {
    onSelect?.(t);
  }

  function registerRow(threadId, el) {
    if (!threadId) return;
    if (el) rowRefs.set(threadId, el);
    else rowRefs.delete(threadId);
  }

  // --- Selection / bulk delete --------------------------------------

  function isSelected(threadId) {
    return messages.selectedThreadIds.has(threadId);
  }

  function toggleRowSelected(thread, on) {
    messages.toggleSelected(thread?.threadId, on);
  }

  // Selection mode flips the per-row leading slot from icon → checkbox
  // (Fastmail-style). Active any time at least one thread is selected.
  const selectionMode = $derived(messages.selectedThreadIds.size > 0);

  // Apply select-all over the *currently visible* threads — mirrors how
  // email apps scope select-all to the active view (filter + search),
  // not the entire underlying inbox. Off-view threads stay untouched.
  const visibleThreadIds = $derived.by(() => {
    const ids = [];
    for (const t of buckets.tacticals) ids.push(t.threadId);
    if (showDmsSection) for (const t of buckets.dms) ids.push(t.threadId);
    return ids;
  });

  const visibleSelectedCount = $derived.by(() => {
    let n = 0;
    for (const id of visibleThreadIds) if (messages.selectedThreadIds.has(id)) n++;
    return n;
  });
  const allVisibleSelected = $derived(
    visibleThreadIds.length > 0 && visibleSelectedCount === visibleThreadIds.length,
  );
  const someVisibleSelected = $derived(
    visibleSelectedCount > 0 && visibleSelectedCount < visibleThreadIds.length,
  );

  // Set the master checkbox's `indeterminate` property — HTML attribute
  // form is reflected via the `indeterminate` IDL property only.
  let masterEl = $state(null);
  $effect(() => {
    if (masterEl) masterEl.indeterminate = someVisibleSelected;
  });

  function toggleSelectAll(e) {
    const want = !!e.currentTarget.checked;
    if (want) {
      // Add all visible threads to the existing selection (don't clobber
      // off-view selections).
      for (const id of visibleThreadIds) messages.selectedThreadIds.add(id);
    } else {
      // Drop only the visible ones.
      for (const id of visibleThreadIds) messages.selectedThreadIds.delete(id);
    }
  }

  let confirmOpen = $state(false);
  let deleting = $state(false);

  function openDeleteConfirm() {
    if (messages.selectedThreadIds.size === 0) return;
    confirmOpen = true;
  }

  // For tactical threads we both unsubscribe (deleteTactical) and wipe
  // server-side history (deleteMessageThread). For DMs we just wipe
  // history. Errors are aggregated into one toast per failure so a
  // partial failure doesn't silently abort the rest of the batch.
  async function runDelete() {
    if (deleting) return;
    deleting = true;
    const ids = [...messages.selectedThreadIds];
    let okCount = 0;
    const failures = [];
    for (const threadId of ids) {
      const thread = messages.conversations.get(threadId);
      if (!thread) continue;
      try {
        if (thread.kind === 'tactical') {
          const entry = messages.tacticals.get(thread.key);
          if (entry?.id) {
            await deleteTactical(entry.id);
            messages.tacticals.delete(thread.key);
          }
        }
        await deleteMessageThread(thread.kind, thread.key);
        messages.conversations.delete(threadId);
        messages.selectedThreadIds.delete(threadId);
        if (messages.activeThreadId === threadId) {
          messages.setActiveThread(null);
          push('/messages');
        }
        okCount++;
      } catch (e) {
        failures.push(`${thread.key}: ${e?.message || 'failed'}`);
      }
    }
    deleting = false;
    confirmOpen = false;
    refreshNow();
    if (okCount > 0) {
      toasts.success(okCount === 1 ? 'Conversation deleted' : `${okCount} conversations deleted`);
    }
    for (const f of failures) toasts.error(`Delete failed - ${f}`);
  }

  // Phrasing for the confirm dialog body. Tactical-aware so the user
  // knows the unsubscribe side-effect before they commit.
  const confirmSummary = $derived.by(() => {
    const ids = [...messages.selectedThreadIds];
    const threads = ids.map((id) => messages.conversations.get(id)).filter(Boolean);
    const n = threads.length;
    const tacticals = threads.filter((t) => t.kind === 'tactical');
    const tacticalCount = tacticals.length;
    return { n, tacticalCount, single: n === 1 ? threads[0] : null };
  });
</script>

<section class="list" aria-label="Conversations">
  <header class="list-header">
    <div class="search">
      <input
        type="text"
        class="search-input"
        value={searchInput}
        placeholder="Search..."
        oninput={onSearchInput}
        aria-label="Search conversations"
      />
    </div>
    <div class="toolbar" data-testid="conversation-toolbar">
      <label class="select-all" aria-label="Select all visible conversations">
        <input
          bind:this={masterEl}
          type="checkbox"
          checked={allVisibleSelected}
          onchange={toggleSelectAll}
          disabled={visibleThreadIds.length === 0}
          data-testid="select-all-checkbox"
        />
      </label>
      <div class="filters" role="radiogroup" aria-label="Filter conversations">
        {#each FILTERS as f}
          <button
            type="button"
            class="pill"
            class:active={filter === f.id}
            role="radio"
            aria-checked={filter === f.id}
            onclick={() => setFilter(f.id)}
            data-testid="filter-pill-{f.id}"
          >
            {f.label}
          </button>
        {/each}
      </div>
      {#if visibleSelectedCount > 0}
        <button
          type="button"
          class="delete-btn"
          onclick={openDeleteConfirm}
          disabled={deleting}
          aria-label={`Delete ${visibleSelectedCount} selected conversation${visibleSelectedCount === 1 ? '' : 's'}`}
          title={`Delete ${visibleSelectedCount} selected`}
          data-testid="bulk-delete-btn"
        >
          <Icon name="trash-2" size="sm" />
        </button>
      {/if}
      <button
        type="button"
        class="new-btn"
        onclick={() => onNew?.()}
        aria-label="New message"
        title="New message"
        data-testid="new-message"
      >
        <Icon name="plus" size="sm" />
      </button>
    </div>
  </header>

  <div class="rows" role="group" aria-label="Thread list">
    <div class="rows-section" aria-labelledby="sec-tactical">
      <h3 class="section-heading" id="sec-tactical">Tactical</h3>
      <button
        type="button"
        class="section-manage"
        onclick={() => onManageTactical?.()}
        data-testid="manage-tactical"
      >
        <Icon name="radio-tower" size="sm" />
        <span>Manage Tactical Chats</span>
      </button>
      {#each buckets.tacticals as thread (thread.threadId)}
        <ConversationRow
          {thread}
          active={thread.threadId === activeThreadId}
          selected={isSelected(thread.threadId)}
          {selectionMode}
          onclick={handleSelect}
          onToggleSelect={toggleRowSelected}
          registerRef={(el) => registerRow(thread.threadId, el)}
        />
      {/each}
    </div>

    {#if showDmsSection}
      <div class="rows-section" aria-labelledby="sec-dms">
        <h3 class="section-heading" id="sec-dms">Direct Messages</h3>
        {#each buckets.dms as thread (thread.threadId)}
          <ConversationRow
            {thread}
            active={thread.threadId === activeThreadId}
            selected={isSelected(thread.threadId)}
            {selectionMode}
            onclick={handleSelect}
            onToggleSelect={toggleRowSelected}
            registerRef={(el) => registerRow(thread.threadId, el)}
          />
        {/each}
        {#if buckets.dms.length === 0}
          <div class="section-empty" role="status">
            {#if q || filter !== 'all'}
              No matches.
            {:else}
              No direct messages yet.
            {/if}
          </div>
        {/if}
      </div>
    {/if}
  </div>
</section>

<AlertDialog bind:open={confirmOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>
      {#if confirmSummary.n === 1}
        Delete this conversation?
      {:else}
        Delete {confirmSummary.n} conversations?
      {/if}
    </AlertDialog.Title>
    <AlertDialog.Description>
      {#if confirmSummary.single}
        {#if confirmSummary.single.kind === 'tactical'}
          You'll be unsubscribed from tactical
          <strong>{confirmSummary.single.key}</strong> and every message
          in this conversation will be deleted from the server.
        {:else}
          Every message in your conversation with
          <strong>{confirmSummary.single.key}</strong> will be deleted
          from the server.
        {/if}
      {:else}
        Every message in the {confirmSummary.n} selected conversations
        will be deleted from the server.
        {#if confirmSummary.tacticalCount > 0}
          You'll also be unsubscribed from
          {confirmSummary.tacticalCount} tactical
          chat{confirmSummary.tacticalCount === 1 ? '' : 's'}.
        {/if}
      {/if}
      This cannot be undone.
    </AlertDialog.Description>
    <div class="alert-footer">
      <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action
        class="bulk-delete-confirm"
        onclick={runDelete}
        disabled={deleting}
        data-testid="bulk-delete-confirm"
      >
        {deleting ? 'Deleting…' : 'Delete'}
      </AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<style>
  .list {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--color-surface);
    border-right: 1px solid var(--color-border);
    overflow: hidden;
  }

  .list-header {
    padding: 10px 10px 0;
    border-bottom: 1px solid var(--color-border-subtle);
    flex-shrink: 0;
  }

  .search {
    display: flex;
    align-items: center;
    margin-bottom: 8px;
  }
  .search-input {
    width: 100%;
    padding: 7px 8px;
    background: var(--color-bg);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    color: var(--color-text);
    font-family: var(--font-mono);
    font-size: 14px;
  }
  .search-input:focus {
    outline: none;
    border-color: var(--color-primary);
    box-shadow: 0 0 0 2px var(--color-primary-muted);
  }

  /* Single consolidated toolbar — Fastmail-style. Master checkbox first
     (sized to align with the per-row checkboxes that take its place
     when select-mode kicks in), then filter pills, then bulk-delete
     (only when something is selected), then New. */
  .toolbar {
    display: flex;
    align-items: center;
    gap: 6px;
    height: 36px;
    padding-bottom: 4px;
  }
  .select-all {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 100%;
    cursor: pointer;
    flex-shrink: 0;
  }
  .select-all input {
    width: 14px;
    height: 14px;
    margin: 0;
    cursor: pointer;
    accent-color: var(--color-primary);
  }
  .select-all input:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }

  .filters {
    display: flex;
    align-items: center;
    flex-wrap: nowrap;
    gap: 4px;
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
  }
  .pill {
    font-family: var(--font-mono);
    font-size: 11px;
    padding: 4px 10px;
    border-radius: 999px;
    background: transparent;
    color: var(--color-text-muted);
    border: 1px solid var(--color-border);
    cursor: pointer;
    transition: background 0.12s, color 0.12s, border-color 0.12s;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .pill:hover {
    background: var(--color-surface-raised);
    color: var(--color-text);
  }
  .pill.active {
    background: var(--color-primary-muted);
    color: var(--color-primary);
    border-color: var(--color-primary);
  }

  .delete-btn,
  .new-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    padding: 0;
    border: 1px solid transparent;
    border-radius: var(--radius);
    background: transparent;
    cursor: pointer;
    flex-shrink: 0;
    color: var(--color-text-muted);
    transition: background 0.12s, color 0.12s, border-color 0.12s;
  }
  .delete-btn {
    color: var(--color-danger);
  }
  .delete-btn:hover:not(:disabled) {
    background: var(--color-danger-muted);
    border-color: var(--color-danger);
  }
  .delete-btn:focus-visible {
    outline: 2px solid var(--color-danger);
    outline-offset: 2px;
  }
  .delete-btn:disabled {
    color: var(--color-text-dim);
    cursor: not-allowed;
    opacity: 0.6;
  }
  .new-btn:hover {
    background: var(--color-surface-raised);
    color: var(--color-primary);
    border-color: var(--color-border);
  }
  .new-btn:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .rows {
    flex: 1 1 auto;
    overflow-y: auto;
    min-height: 0;
  }

  .rows-section {
    display: flex;
    flex-direction: column;
  }
  .rows-section + .rows-section {
    margin-top: 6px;
    border-top: 1px solid var(--color-border-subtle);
    padding-top: 4px;
  }

  .section-heading {
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 1px;
    text-transform: uppercase;
    color: var(--color-text-dim);
    padding: 10px 14px 4px;
    margin: 0;
    background: var(--color-surface);
  }

  .section-manage {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin: 2px 8px 6px;
    padding: 9px 10px;
    background: transparent;
    border: 1px dashed var(--color-border);
    border-radius: var(--radius);
    color: var(--color-text-muted);
    font-family: var(--font-mono);
    font-size: 12px;
    font-weight: 700;
    cursor: pointer;
    transition: background 0.12s, color 0.12s, border-color 0.12s;
  }
  .section-manage:hover {
    background: var(--color-surface-raised);
    color: var(--color-text);
    border-color: var(--color-primary);
  }
  .section-manage:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .section-empty {
    padding: 12px 14px;
    text-align: center;
    font-size: 12px;
    color: var(--color-text-muted);
  }

  .alert-footer {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    padding: 1rem 1.5rem 1.25rem;
  }
  :global(.bulk-delete-confirm) {
    background: var(--color-danger) !important;
    color: white !important;
    border-color: var(--color-danger) !important;
  }
  :global(.bulk-delete-confirm:disabled) {
    opacity: 0.6;
    cursor: not-allowed;
  }
</style>
