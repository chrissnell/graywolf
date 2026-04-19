<script>
  // A single chat bubble.
  //
  // Props:
  //   - msg               — MessageResponse
  //   - isTactical        — thread kind flag from the parent
  //   - showSenderLabel   — computed by parent clustering logic:
  //                         true on the first bubble of an incoming
  //                         tactical cluster AND on "label repeat"
  //                         bubbles (every 5th in a long cluster).
  //                         Parent owns the break conditions.
  //   - showMonogramInStripe — parallel to showSenderLabel; monogram
  //                         is drawn inside the 2 px left stripe on
  //                         bubble 1 + label-repeat bubbles (not
  //                         every bubble — would be visually loud).
  //   - onMetaClick       — open meta drawer for this bubble
  //   - onReplyPrivate    — inline reply-privately action (tactical
  //                         incoming only)
  //   - onContextMenu     — (x, y, msg) => void — open the context
  //                         menu at pointer
  //
  // Typography override: a local `.bubble-text` class renders the
  // message body in proportional system fallback — otherwise bubbles
  // feel like a terminal log. Callsigns and timestamps stay mono.

  import { Badge, Icon, Tooltip } from '@chrissnell/chonky-ui';
  import { callsignColors, callsignMonogram } from '../../lib/callsignColor.js';
  import { timeOfDay } from './time.js';

  /** @type {{
   *    msg: any,
   *    isTactical?: boolean,
   *    showSenderLabel?: boolean,
   *    showMonogramInStripe?: boolean,
   *    onMetaClick?: (msg: any) => void,
   *    onReplyPrivate?: (fromCall: string) => void,
   *    onContextMenu?: (x: number, y: number, msg: any) => void,
   *    onResend?: (msg: any) => void,
   *    registerRef?: (el: HTMLElement | null) => void,
   *  }}
   */
  let {
    msg,
    isTactical = false,
    showSenderLabel = false,
    showMonogramInStripe = false,
    onMetaClick,
    onReplyPrivate,
    onContextMenu,
    onResend,
    registerRef,
  } = $props();

  const isOut = $derived(msg?.direction === 'out');
  const sender = $derived(msg?.from_call || '');
  const colors = $derived(callsignColors(sender));
  const monogram = $derived(callsignMonogram(sender));

  // Split "{N/M} text" label out of the body so the fragment tag
  // renders as a small badge instead of polluting the bubble text.
  const fragMatch = $derived.by(() => {
    const txt = msg?.text || '';
    const m = txt.match(/^\{(\d+)\/(\d+)\}\s*(.*)$/);
    if (!m) return null;
    return { n: m[1], total: m[2], body: m[3] };
  });
  const bodyText = $derived(fragMatch ? fragMatch.body : (msg?.text || ''));

  const status = $derived(msg?.status || '');
  const source = $derived(msg?.source || '');

  const statusInfo = $derived.by(() => {
    // Tactical outgoing gets the broadcast visual regardless of
    // raw status; acked/received reveals the check-check secondary.
    if (isOut && isTactical) {
      return {
        primary: { name: 'radio-tower', label: 'Broadcast sent — acks not expected for group messages.' },
        secondary: msg?.received_by_call ? { name: 'check-check', label: `received by ${msg.received_by_call}` } : null,
      };
    }
    if (isOut) {
      switch (status) {
        case 'pending':
        case 'queued':   return { primary: { name: 'clock', label: 'Waiting for transmit' } };
        case 'sent_rf':
        case 'sent_is':
        case 'sent':     return { primary: { name: 'radio', label: 'Sent — awaiting ack' } };
        case 'acked':    return { primary: { name: 'check-check', label: 'Delivered — ack received' } };
        case 'rejected': return { primary: { name: 'alert-circle', label: 'Rejected by recipient — click to resend' }, failed: true };
        case 'failed':   return { primary: { name: 'alert-circle', label: 'Send failed — click to resend' }, failed: true };
        case 'timeout':  return { primary: { name: 'alert-circle', label: 'Retry budget exhausted — click to resend' }, failed: true };
        default:         return { primary: { name: 'clock', label: status || 'Unknown state' } };
      }
    }
    return null;
  });

  // Resend is available on rejected/failed/timeout outbound DM rows.
  // Tactical outbound doesn't reach those states (it terminates at
  // `broadcast`), so this flag stays false for tactical regardless.
  const canResend = $derived(
    isOut && !isTactical && !!statusInfo?.failed && !!onResend
  );

  function handleStatusClick() {
    if (canResend) onResend?.(msg);
  }

  const sourceBadge = $derived.by(() => {
    if (!source) return null;
    if (source === 'rf') return { variant: 'success', label: 'RF' };
    if (source === 'is') return { variant: 'info', label: 'IS' };
    if (source === 'sim') return { variant: 'warning', label: 'Sim' };
    return { variant: 'default', label: source };
  });

  let bubbleEl = $state(null);
  $effect(() => {
    registerRef?.(bubbleEl);
    return () => registerRef?.(null);
  });

  function handleContextMenu(e) {
    if (!onContextMenu) return;
    e.preventDefault();
    onContextMenu(e.clientX, e.clientY, msg);
  }

  // Long-press on mobile → same context menu. 500 ms hold.
  let longPressTimer = null;
  function onPointerDown(e) {
    if (e.pointerType !== 'touch' || !onContextMenu) return;
    clearTimeout(longPressTimer);
    longPressTimer = setTimeout(() => {
      onContextMenu(e.clientX, e.clientY, msg);
    }, 500);
  }
  function onPointerUpOrCancel() {
    clearTimeout(longPressTimer);
  }

  function handleMetaClick(e) {
    e.stopPropagation();
    onMetaClick?.(msg);
  }

  const showReplyPrivately = $derived(isTactical && !isOut && !!sender);
</script>

<article
  class="bubble-wrap"
  class:out={isOut}
  class:in={!isOut}
  class:tactical={isTactical}
  class:failed={!!statusInfo?.failed}
  data-testid="message-bubble"
  data-msg-id={msg?.id}
  data-status={status}
>
  {#if isTactical && !isOut && showSenderLabel}
    <div
      class="sender-label"
      style="background:{colors.bg};color:{colors.fg};border-color:{colors.stripe}"
      aria-label={`From ${sender}`}
    >
      <span class="monogram-mini">{monogram}</span>
      <span class="sender-call">{sender}</span>
    </div>
  {/if}

  <div
    bind:this={bubbleEl}
    class="bubble"
    class:has-stripe={isTactical && !isOut}
    style={isTactical && !isOut ? `--stripe-color:${colors.stripe}` : undefined}
    role="group"
    aria-label={isOut ? 'Your message' : `Message from ${sender}`}
    oncontextmenu={handleContextMenu}
    onpointerdown={onPointerDown}
    onpointerup={onPointerUpOrCancel}
    onpointercancel={onPointerUpOrCancel}
  >
    {#if isTactical && !isOut && showMonogramInStripe}
      <span
        class="stripe-monogram"
        style="color:{colors.fg};background:{colors.stripe}"
        aria-hidden="true"
      >{monogram}</span>
    {/if}
    <p class="bubble-text">{bodyText}</p>
    <div class="bubble-meta">
      <button
        type="button"
        class="ts-btn"
        onclick={handleMetaClick}
        aria-label="View message details"
        data-testid="bubble-meta-open"
        title="View details"
      >
        <span class="ts">{timeOfDay(msg?.sent_at || msg?.received_at || msg?.created_at)}</span>
      </button>
      {#if fragMatch}
        <span class="frag-tag">Part {fragMatch.n}/{fragMatch.total}</span>
      {/if}
      {#if sourceBadge}
        <Badge variant={sourceBadge.variant} class="src-badge">{sourceBadge.label}</Badge>
      {/if}
      {#if statusInfo?.primary}
        <Tooltip>
          <Tooltip.Trigger class="status-tt">
            {#if canResend}
              <button
                type="button"
                class="status-ico status-btn failed"
                aria-label={statusInfo.primary.label}
                onclick={handleStatusClick}
                data-testid="bubble-resend"
              >
                <Icon name={statusInfo.primary.name} size="sm" />
              </button>
            {:else}
              <span
                class="status-ico"
                class:failed={!!statusInfo.failed}
                aria-label={statusInfo.primary.label}
              >
                <Icon
                  name={statusInfo.primary.name}
                  size={statusInfo.failed ? 'sm' : 'xs'}
                />
              </span>
            {/if}
          </Tooltip.Trigger>
          <Tooltip.Content>{statusInfo.primary.label}</Tooltip.Content>
        </Tooltip>
      {/if}
      {#if statusInfo?.secondary}
        <Tooltip>
          <Tooltip.Trigger class="status-tt">
            <span class="status-ico secondary" aria-label={statusInfo.secondary.label}>
              <Icon name={statusInfo.secondary.name} size="xs" />
            </span>
          </Tooltip.Trigger>
          <Tooltip.Content>{statusInfo.secondary.label}</Tooltip.Content>
        </Tooltip>
      {/if}
    </div>
  </div>

  {#if showReplyPrivately}
    <button
      type="button"
      class="reply-private"
      onclick={() => onReplyPrivate?.(sender)}
      aria-label={`Reply privately to ${sender}`}
      data-testid="reply-private"
    >
      <Icon name="reply" size="sm" />
    </button>
  {/if}
</article>

<style>
  .bubble-wrap {
    display: flex;
    align-items: flex-end;
    gap: 6px;
    position: relative;
    max-width: 78%;
    margin: 2px 0;
  }
  .bubble-wrap.out {
    align-self: flex-end;
    flex-direction: row-reverse;
  }
  .bubble-wrap.in {
    align-self: flex-start;
  }
  .bubble-wrap.tactical.in {
    flex-direction: column;
    align-items: flex-start;
    width: fit-content;
  }
  .bubble-wrap.tactical.in .bubble {
    align-self: flex-start;
  }

  .sender-label {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin: 0 0 2px 6px;
    padding: 1px 8px 1px 2px;
    font-size: 11px;
    font-family: var(--font-mono);
    border: 1px solid;
    border-radius: 999px;
    font-weight: 600;
  }
  .monogram-mini {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    border-radius: 999px;
    background: var(--color-bg);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.5px;
  }
  .sender-call {
    letter-spacing: 0.3px;
  }

  .bubble {
    position: relative;
    padding: 7px 12px;
    border-radius: 12px;
    background: var(--color-surface-raised);
    color: var(--color-text);
    word-wrap: break-word;
    overflow-wrap: anywhere;
    line-height: 1.35;
    min-width: 0;
    max-width: 100%;
  }
  .bubble-wrap.out .bubble {
    background: var(--color-primary-muted);
    border-left: 1px solid var(--color-primary);
    border-radius: 12px 12px 4px 12px;
  }
  .bubble-wrap.in .bubble {
    border-radius: 12px 12px 12px 4px;
  }
  .bubble.has-stripe {
    border-left: 2px solid var(--stripe-color, var(--color-primary));
    padding-left: 14px;
  }

  .stripe-monogram {
    position: absolute;
    top: -2px;
    left: -2px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border-radius: 0 0 8px 0;
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.5px;
    pointer-events: none;
  }

  .bubble-text {
    margin: 0;
    /* Override the app's monospace body font for bubble content only;
       keeps bubbles feeling like messaging, not a terminal. */
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui,
      'Helvetica Neue', Arial, sans-serif;
    font-size: 14px;
    white-space: pre-wrap;
  }

  .bubble-meta {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 4px;
    opacity: 0;
    transition: opacity 0.15s;
    font-family: var(--font-mono);
  }
  .bubble:hover .bubble-meta,
  .bubble:focus-within .bubble-meta {
    opacity: 1;
  }
  @media (max-width: 767px) {
    .bubble-meta { opacity: 1; }
  }

  .ts-btn {
    background: transparent;
    border: none;
    padding: 0;
    margin: 0;
    cursor: pointer;
    color: inherit;
  }
  .ts {
    font-size: 10px;
    color: var(--color-text-dim);
    font-family: var(--font-mono);
  }
  .frag-tag {
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.5px;
    color: var(--color-text-dim);
    border: 1px solid var(--color-border);
    border-radius: 3px;
    padding: 0 4px;
    text-transform: uppercase;
  }
  :global(.bubble-meta .src-badge) {
    font-size: 9px !important;
    padding: 0 4px !important;
  }
  .status-ico {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--color-text-muted);
  }
  .bubble-wrap.out .status-ico { color: var(--color-primary); }
  .status-ico.secondary { color: var(--color-success); }
  .status-ico.failed { color: var(--color-danger); }

  .status-btn {
    background: transparent;
    border: none;
    padding: 2px;
    margin: -2px;
    cursor: pointer;
    border-radius: 4px;
    transition: background 0.15s;
  }
  .status-btn:hover {
    background: var(--color-danger-muted);
  }
  .status-btn:focus-visible {
    outline: 2px solid var(--color-danger);
    outline-offset: 1px;
  }

  /* Failed outbound gets a subtle red border on the whole bubble plus
     always-visible meta so the operator notices at a glance. */
  .bubble-wrap.failed .bubble {
    border-color: var(--color-danger);
    box-shadow: 0 0 0 1px var(--color-danger-muted) inset;
  }
  .bubble-wrap.failed .bubble-meta {
    opacity: 1;
  }

  .reply-private {
    background: var(--color-surface-raised);
    border: 1px solid var(--color-border);
    color: var(--color-text-muted);
    width: 28px;
    height: 28px;
    border-radius: 999px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    flex-shrink: 0;
    transition: opacity 0.2s, background 0.15s, color 0.15s;
    opacity: 0;
  }
  .bubble-wrap.tactical.in:hover .reply-private,
  .bubble-wrap.tactical.in:focus-within .reply-private {
    opacity: 0.8;
  }
  .reply-private:hover {
    opacity: 1 !important;
    background: var(--color-primary-muted);
    color: var(--color-primary);
  }
  @media (max-width: 767px) {
    .reply-private {
      opacity: 1;
      width: 32px;
      height: 32px;
    }
  }
</style>
