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
  //   - list rendering, with a "Tactical" section header above
  //     tactical threads when the `All` filter is active AND at
  //     least one tactical thread exists
  //   - the "Manage tactical callsigns →" footer link
  //
  // Emits:
  //   - onSelect(thread)  — row clicked / keyboard-activated
  //   - onNew()           — "+" compose button clicked
  //   - onManageTactical()— footer link clicked
  //   - visibleThreads    — bound; parent consumes for keyboard
  //                          prev/next thread navigation so the
  //                          shortcut cycles the same order the user
  //                          sees (not the unfiltered list).

  import { Button, Icon } from '@chrissnell/chonky-ui';
  import { messages } from '../../lib/messagesStore.svelte.js';
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

  // Group into Tactical / DM buckets ONLY when the All filter is
  // active and at least one tactical exists; otherwise flat list.
  const sections = $derived.by(() => {
    const tacticals = [];
    const dms = [];
    for (const t of filteredThreads) {
      (t.kind === 'tactical' ? tacticals : dms).push(t);
    }
    const sep = filter === 'all' && tacticals.length > 0;
    if (sep) {
      return [
        { heading: 'Tactical', items: tacticals },
        { heading: '', items: dms },
      ];
    }
    return [{ heading: '', items: filteredThreads }];
  });

  // Keep the parent's visible-order mirror in sync so Ctrl/Cmd+↑↓
  // cycles the same list the user sees.
  $effect(() => {
    const arr = [];
    for (const s of sections) for (const t of s.items) arr.push(t);
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
</script>

<section class="list" aria-label="Conversations">
  <header class="list-header">
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
      <Button variant="ghost" size="sm" class="new-btn" onclick={() => onNew?.()} aria-label="New message" data-testid="new-message">
        <Icon name="plus" size="sm" />
        New
      </Button>
    </div>
    <div class="search">
      <span class="search-icon" aria-hidden="true">
        <Icon name="search" size="sm" />
      </span>
      <input
        type="text"
        class="search-input"
        value={searchInput}
        placeholder="Search..."
        oninput={onSearchInput}
        aria-label="Search conversations"
      />
    </div>
  </header>

  <div class="rows" role="group" aria-label="Thread list">
    {#if filteredThreads.length === 0}
      <div class="empty" role="status">
        {#if q || filter !== 'all'}
          No matches.
        {:else}
          No conversations yet.
        {/if}
      </div>
    {:else}
      {#each sections as section (section.heading || 'flat')}
        {#if section.heading}
          <h3 class="section-heading">{section.heading}</h3>
        {/if}
        {#each section.items as thread (thread.threadId)}
          <ConversationRow
            {thread}
            active={thread.threadId === activeThreadId}
            onclick={handleSelect}
            registerRef={(el) => registerRow(thread.threadId, el)}
          />
        {/each}
      {/each}
    {/if}
  </div>

  <footer class="list-footer">
    <button type="button" class="manage" onclick={() => onManageTactical?.()} data-testid="manage-tactical">
      <Icon name="radio-tower" size="sm" />
      <span>Manage tactical callsigns →</span>
    </button>
  </footer>
</section>

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
    padding: 10px 10px 6px;
    border-bottom: 1px solid var(--color-border-subtle);
    flex-shrink: 0;
  }
  .filters {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px;
    margin-bottom: 8px;
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
  :global(.filters .new-btn) {
    margin-left: auto;
    gap: 4px;
  }

  .search {
    position: relative;
    display: flex;
    align-items: center;
  }
  .search-icon {
    position: absolute;
    left: 8px;
    top: 50%;
    transform: translateY(-50%);
    display: inline-flex;
    color: var(--color-text-dim);
    pointer-events: none;
    z-index: 1;
  }
  .search-input {
    width: 100%;
    padding: 7px 8px 7px 28px;
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

  .rows {
    flex: 1 1 auto;
    overflow-y: auto;
    min-height: 0;
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
    position: sticky;
    top: 0;
    z-index: 1;
  }

  .empty {
    padding: 24px 12px;
    text-align: center;
    font-size: 12px;
    color: var(--color-text-muted);
  }

  .list-footer {
    border-top: 1px solid var(--color-border);
    padding: 8px 12px;
    flex-shrink: 0;
  }
  .manage {
    width: 100%;
    background: transparent;
    border: none;
    color: var(--color-text-muted);
    font-family: var(--font-mono);
    font-size: 12px;
    padding: 6px 4px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    cursor: pointer;
    border-radius: var(--radius);
  }
  .manage:hover {
    background: var(--color-surface-raised);
    color: var(--color-text);
  }
</style>
