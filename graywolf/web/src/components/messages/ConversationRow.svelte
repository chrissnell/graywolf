<script>
  // A single thread entry in the ConversationList.
  //
  // Layout has two states for the leading column:
  //   - icon mode (default)         : kind icon (radio-tower / user)
  //   - select mode (Fastmail-style): checkbox replaces the icon in the
  //                                   same slot, no row reflow
  //
  // Select mode kicks in when ANY row in the inbox is selected (parent
  // passes `selectionMode`), or when this specific row is hovered with
  // a pointer. The two paths converge on the same checkbox slot so the
  // user sees the row content shift exactly zero pixels — the icon and
  // checkbox occupy a single 28px square together.

  import { Icon, NotificationBadge } from '@chrissnell/chonky-ui';
  import { relativeShort } from './time.js';

  /** @type {{
   *    thread: any,
   *    active?: boolean,
   *    selected?: boolean,
   *    selectionMode?: boolean,
   *    onclick?: (t: any) => void,
   *    onToggleSelect?: (t: any, on: boolean) => void,
   *    registerRef?: (el: HTMLElement | null) => void,
   *  }}
   */
  let {
    thread,
    active = false,
    selected = false,
    selectionMode = false,
    onclick,
    onToggleSelect,
    registerRef,
  } = $props();

  let rowEl = $state(null);
  $effect(() => {
    registerRef?.(rowEl);
    return () => registerRef?.(null);
  });

  const isTactical = $derived(thread?.kind === 'tactical');
  const title = $derived.by(() => {
    if (!thread) return '';
    if (isTactical && thread.alias) return thread.key;
    return thread.key || '';
  });
  const subtitle = $derived.by(() => {
    if (isTactical && thread?.alias) return thread.alias;
    return '';
  });
  const snippet = $derived.by(() => {
    const s = thread?.lastSnippet || '';
    if (!s) return '';
    if (isTactical && thread?.lastSenderCall) {
      return `${thread.lastSenderCall}: ${s}`;
    }
    return s;
  });
  const unread = $derived(thread?.unreadCount || 0);
  const ariaLabel = $derived.by(() => {
    const parts = [thread?.key || ''];
    if (subtitle) parts.push(subtitle);
    if (unread > 0) parts.push(`${unread} unread`);
    if (thread?.muted) parts.push('muted');
    return parts.join(', ');
  });

  function handleClick(e) {
    // In selection mode the row click toggles the checkbox instead of
    // navigating. This matches Fastmail/Gmail behavior — once you're in
    // select mode, clicking anywhere on a row picks it.
    if (selectionMode) {
      e?.preventDefault?.();
      onToggleSelect?.(thread, !selected);
      return;
    }
    e?.preventDefault?.();
    onclick?.(thread);
  }

  function handleKey(e) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      if (selectionMode) onToggleSelect?.(thread, !selected);
      else onclick?.(thread);
    }
  }

  function handleSelectChange(e) {
    onToggleSelect?.(thread, !!e.currentTarget.checked);
  }

  // Stop the row's onclick from firing when the user clicks the
  // checkbox directly — the checkbox's onchange already handles it.
  function handleCheckboxClick(e) {
    e.stopPropagation();
  }
</script>

<div
  bind:this={rowEl}
  class="row"
  class:active
  class:unread={unread > 0}
  class:muted={thread?.muted}
  class:selected
  class:select-mode={selectionMode}
  data-testid="conversation-row"
  data-thread-id={thread?.threadId}
>
  <span class="accent" aria-hidden="true"></span>
  <button
    type="button"
    class="row-btn"
    aria-current={active ? 'true' : undefined}
    aria-label={ariaLabel}
    onclick={handleClick}
    onkeydown={handleKey}
  >
    <span class="lead" aria-hidden={selectionMode ? 'true' : undefined}>
      <span class="lead-icon">
        <Icon name={isTactical ? 'radio-tower' : 'user'} size="md" />
      </span>
      <label
        class="lead-checkbox"
        aria-label={`Select ${thread?.key || 'conversation'}`}
      >
        <input
          type="checkbox"
          checked={selected}
          onchange={handleSelectChange}
          onclick={handleCheckboxClick}
          data-testid="conversation-row-checkbox"
        />
      </label>
    </span>
    <div class="body">
      <div class="title-line">
        <span class="title">{title}</span>
        {#if subtitle}
          <span class="subtitle" title={subtitle}>{subtitle}</span>
        {/if}
        <span class="ts">{relativeShort(thread?.lastAt)}</span>
      </div>
      <div class="snippet-line">
        <span class="snippet" title={snippet}>{snippet || (isTactical ? 'No messages yet' : '')}</span>
        {#if unread > 0}
          <span class="unread-badge">
            <NotificationBadge count={unread} />
          </span>
        {/if}
      </div>
    </div>
  </button>
</div>

<style>
  .row {
    position: relative;
    border-bottom: 1px solid var(--color-border-subtle);
    background: transparent;
    transition: background 0.12s;
  }
  .row:hover {
    background: var(--color-surface-raised);
  }
  .row.active {
    background: var(--color-primary-muted);
  }
  .row.selected {
    background: var(--color-primary-muted);
  }
  .row.muted .title,
  .row.muted .snippet {
    opacity: 0.55;
  }
  :global(.row.is-keyboard-focused) {
    box-shadow: inset 0 0 0 2px var(--color-primary);
  }

  .accent {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 4px;
    background: transparent;
    pointer-events: none;
  }
  .row.unread .accent {
    background: var(--color-primary);
  }
  .row.active .accent {
    background: var(--color-primary);
  }

  .row-btn {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr);
    gap: 10px;
    align-items: center;
    /* padding-left clears the 4px accent stripe (which paints on top
       of the row's background) plus a 10px gutter so the lead column
       sits at the same x-coordinate as the master checkbox in the
       toolbar above. */
    padding: 10px 12px 10px 14px;
    cursor: pointer;
    outline: none;
    background: transparent;
    border: none;
    width: 100%;
    text-align: left;
    color: inherit;
    font: inherit;
  }
  .row-btn:focus-visible {
    box-shadow: inset 0 0 0 2px var(--color-primary);
  }

  /* Lead column: icon and checkbox stacked into a single 28x28 cell,
     only one rendered at a time. CSS — not the template — picks which
     so the swap is purely visual (no DOM thrash). */
  .lead {
    position: relative;
    width: 28px;
    height: 28px;
    flex-shrink: 0;
  }
  .lead-icon,
  .lead-checkbox {
    position: absolute;
    inset: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .lead-icon {
    color: var(--color-text-muted);
  }
  .row.active .lead-icon {
    color: var(--color-primary);
  }
  .lead-checkbox {
    cursor: pointer;
    /* Hidden by default; revealed in select-mode OR on row hover. The
       hover reveal lets a user pick a single row without first clicking
       the master checkbox — same affordance as Fastmail/Gmail. */
    opacity: 0;
    pointer-events: none;
  }
  .lead-checkbox input {
    width: 18px;
    height: 18px;
    margin: 0;
    cursor: pointer;
    accent-color: var(--color-primary);
  }
  .row.select-mode .lead-icon,
  .row:hover .lead-icon {
    opacity: 0;
  }
  .row.select-mode .lead-checkbox,
  .row:hover .lead-checkbox {
    opacity: 1;
    pointer-events: auto;
  }

  .body {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .title-line {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
  }
  .title {
    font-weight: 600;
    font-family: var(--font-mono);
    color: var(--color-text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex-shrink: 0;
    max-width: 60%;
  }
  .subtitle {
    font-size: 12px;
    color: var(--color-text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1 1 auto;
    min-width: 0;
  }
  .ts {
    font-size: 11px;
    color: var(--color-text-dim);
    font-family: var(--font-mono);
    flex-shrink: 0;
    margin-left: auto;
  }
  .snippet-line {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .snippet {
    font-size: 12px;
    color: var(--color-text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1 1 auto;
    min-width: 0;
  }
  .row.unread .snippet {
    color: var(--color-text);
    font-weight: 500;
  }
  /* Unique class (not `.badge`) — chonky-ui ships a global `.badge`
     rule that adds border + padding, which leaks through Svelte's
     scoped styles because we don't override those properties. */
  .unread-badge {
    flex-shrink: 0;
    display: inline-flex;
  }
</style>
