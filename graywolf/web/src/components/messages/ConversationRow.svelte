<script>
  // A single thread entry in the ConversationList.
  //
  // Props:
  //   thread   — store Thread shape ({threadId, kind, key, alias,
  //              unreadCount, lastAt, lastSnippet, lastSenderCall,
  //              muted, archived})
  //   active   — is this row the currently-open thread?
  //   onclick  — (thread) => void — navigate to thread
  //   rowRef   — optional $bindable ref the parent stores for focus
  //              restoration (lastActiveRow)
  //
  // Layout is identical for DM and tactical; only the leading icon,
  // title line, and snippet differ. Kept pure — no store reads inside
  // so the parent ConversationList can iterate a $derived sorted
  // array without each row subscribing separately.

  import { Icon, NotificationBadge } from '@chrissnell/chonky-ui';
  import { relativeShort } from './time.js';

  /** @type {{
   *    thread: any,
   *    active?: boolean,
   *    onclick?: (t: any) => void,
   *    registerRef?: (el: HTMLElement | null) => void,
   *  }}
   */
  let {
    thread,
    active = false,
    onclick,
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
    e?.preventDefault?.();
    onclick?.(thread);
  }

  function handleKey(e) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onclick?.(thread);
    }
  }
</script>

<button
  bind:this={rowEl}
  type="button"
  class="row"
  class:active
  class:unread={unread > 0}
  class:muted={thread?.muted}
  aria-current={active ? 'true' : undefined}
  aria-label={ariaLabel}
  data-testid="conversation-row"
  data-thread-id={thread?.threadId}
  onclick={handleClick}
  onkeydown={handleKey}
>
  <span class="accent" aria-hidden="true"></span>
  <span class="lead-icon" aria-hidden="true">
    <Icon name={isTactical ? 'radio-tower' : 'user'} size="md" />
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
        <span class="badge">
          <NotificationBadge count={unread} />
        </span>
      {/if}
    </div>
  </div>
</button>

<style>
  .row {
    position: relative;
    display: grid;
    grid-template-columns: 24px 1fr;
    gap: 10px;
    align-items: center;
    /* padding-left clears the 4px accent stripe + a 10px gutter so
       content alignment doesn't shift when the stripe appears or
       disappears. The row background fills the full row box
       including the stripe area — the stripe paints on top. */
    padding: 10px 12px 10px 14px;
    cursor: pointer;
    border-bottom: 1px solid var(--color-border-subtle);
    outline: none;
    transition: background 0.12s;
    /* button reset */
    background: transparent;
    border-left: none;
    border-right: none;
    border-top: none;
    width: 100%;
    text-align: left;
    color: inherit;
    font: inherit;
  }
  .row:hover {
    background: var(--color-surface-raised);
  }
  .row:focus-visible {
    background: var(--color-surface-raised);
    box-shadow: inset 0 0 0 2px var(--color-primary);
  }
  .row.active {
    background: var(--color-primary-muted);
  }
  .row.muted .title,
  .row.muted .snippet {
    opacity: 0.55;
  }
  :global(.row.is-keyboard-focused) {
    box-shadow: inset 0 0 0 2px var(--color-primary);
  }

  /* The accent stripe sits absolutely inside the row so it can span
     the row's full height (not just the text area) without disturbing
     flex/grid alignment. Transparent until the row is unread or
     active. */
  .accent {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 4px;
    background: transparent;
  }
  .row.unread .accent {
    background: var(--color-primary);
  }
  .row.active .accent {
    background: var(--color-primary);
  }

  .lead-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    color: var(--color-text-muted);
    flex-shrink: 0;
  }
  .row.active .lead-icon {
    color: var(--color-primary);
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
  .badge {
    flex-shrink: 0;
    display: inline-flex;
  }
</style>
