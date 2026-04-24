<script>
  // A floating context-menu panel for a message bubble.
  //
  // We render a plain floating menu (not chonky's <ContextMenu>,
  // which is a right-click-on-a-trigger composable) because we need
  // programmatic open-at-point support so both right-click and
  // long-press on mobile funnel through the same path. The wrapper
  // keeps the menu dismissable on outside click / Escape.
  //
  // Actions (per plan):
  //   - Reply privately to [sender]   (tactical incoming only — always top)
  //   - Copy text / Copy callsign / Copy raw
  //   - Mark unread                   (incoming only)
  //   - Resend                        (outgoing + terminal state only)
  //
  // Deletion is intentionally NOT in this menu — the only delete path is
  // the inbox checkboxes + Delete toolbar (ConversationList). Adding a
  // per-bubble shortcut here would create a parallel UX with weaker
  // confirmation guarantees.

  import { onMount } from 'svelte';
  import { Icon } from '@chrissnell/chonky-ui';

  /** @type {{
   *    open: boolean,
   *    x?: number,
   *    y?: number,
   *    msg?: any,
   *    isTactical?: boolean,
   *    onClose?: () => void,
   *    onCopyText?: (msg: any) => void,
   *    onCopyRaw?: (msg: any) => void,
   *    onCopyCall?: (msg: any) => void,
   *    onReplyPrivate?: (fromCall: string) => void,
   *    onMarkUnread?: (msg: any) => void,
   *    onResend?: (msg: any) => void,
   *  }}
   */
  let {
    open = $bindable(false),
    x = 0,
    y = 0,
    msg = null,
    isTactical = false,
    onClose,
    onCopyText,
    onCopyRaw,
    onCopyCall,
    onReplyPrivate,
    onMarkUnread,
    onResend,
  } = $props();

  const isOut = $derived(msg?.direction === 'out');
  const status = $derived(msg?.status || '');
  const canResend = $derived(isOut && (status === 'rejected' || status === 'failed'));
  const sender = $derived(msg?.from_call || '');
  const showReply = $derived(isTactical && !isOut && !!sender);

  function close() {
    open = false;
    onClose?.();
  }

  function pick(action) {
    action?.(msg);
    close();
  }

  function handleKey(e) {
    if (e.key === 'Escape') {
      e.preventDefault();
      close();
    }
  }

  onMount(() => {
    const handler = () => { if (open) close(); };
    window.addEventListener('scroll', handler, { capture: true });
    window.addEventListener('resize', handler);
    return () => {
      window.removeEventListener('scroll', handler, { capture: true });
      window.removeEventListener('resize', handler);
    };
  });

  // Clamp (x, y) inside the viewport so the menu doesn't render
  // under the scroll edge. Rough: 240x220 menu budget.
  const left = $derived.by(() => {
    if (typeof window === 'undefined') return x;
    return Math.min(x, Math.max(0, window.innerWidth - 240));
  });
  const top = $derived.by(() => {
    if (typeof window === 'undefined') return y;
    return Math.min(y, Math.max(0, window.innerHeight - 240));
  });
</script>

<svelte:window onkeydown={handleKey} />

{#if open}
  <button
    type="button"
    class="overlay"
    aria-label="Close context menu"
    onclick={close}
  ></button>
  <div
    class="menu"
    role="menu"
    style="left:{left}px;top:{top}px"
    data-testid="message-context-menu"
  >
    {#if showReply}
      <button type="button" class="item primary" role="menuitem" onclick={() => { onReplyPrivate?.(sender); close(); }}>
        <Icon name="reply" size="sm" />
        <span>Reply privately to {sender}</span>
      </button>
      <div class="sep" role="separator"></div>
    {/if}
    <button type="button" class="item" role="menuitem" onclick={() => pick(onCopyText)}>
      <Icon name="copy" size="sm" />
      <span>Copy text</span>
    </button>
    <button type="button" class="item" role="menuitem" onclick={() => pick(onCopyCall)} disabled={!(msg?.from_call || msg?.to_call)}>
      <Icon name="user" size="sm" />
      <span>Copy callsign</span>
    </button>
    <button type="button" class="item" role="menuitem" onclick={() => pick(onCopyRaw)}>
      <Icon name="copy" size="sm" />
      <span>Copy raw</span>
    </button>
    {#if !isOut}
      <div class="sep" role="separator"></div>
      <button type="button" class="item" role="menuitem" onclick={() => pick(onMarkUnread)}>
        <Icon name="eye-off" size="sm" />
        <span>Mark unread</span>
      </button>
    {/if}
    {#if canResend}
      <div class="sep" role="separator"></div>
      <button type="button" class="item" role="menuitem" onclick={() => pick(onResend)}>
        <Icon name="refresh-cw" size="sm" />
        <span>Resend</span>
      </button>
    {/if}
  </div>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: transparent;
    border: none;
    padding: 0;
    margin: 0;
    cursor: default;
    z-index: 100;
  }
  .menu {
    position: fixed;
    z-index: 101;
    min-width: 220px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
    padding: 4px 0;
    font-family: var(--font-mono);
    font-size: 13px;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 12px;
    background: transparent;
    border: none;
    color: var(--color-text);
    cursor: pointer;
    font-family: inherit;
    font-size: inherit;
    text-align: left;
  }
  .item:hover:not([disabled]) {
    background: var(--color-surface-raised);
  }
  .item[disabled] {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .item.primary {
    color: var(--color-primary);
    font-weight: 600;
  }
  .sep {
    height: 1px;
    background: var(--color-border);
    margin: 4px 0;
  }
</style>
