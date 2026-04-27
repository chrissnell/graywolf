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
  //   - showAvatar        — parallel to showSenderLabel; render the
  //                         sender's monogram avatar circle in the
  //                         left avatar slot on bubble 1 + label-repeat
  //                         bubbles. Other bubbles in the cluster get
  //                         an empty (but width-reserved) slot so the
  //                         left edge of every bubble in the cluster
  //                         stays aligned.
  //   - onMetaClick       — open meta drawer for this bubble
  //   - onReplyPrivate    — inline reply-privately action (tactical
  //                         incoming only)
  //   - onContextMenu     — (x, y, msg) => void — open the context
  //                         menu at pointer
  //
  // Typography override: a local `.bubble-text` class renders the
  // message body in proportional system fallback — otherwise bubbles
  // feel like a terminal log. Callsigns and timestamps stay mono.

  import { tick } from 'svelte';
  import { push } from 'svelte-spa-router';
  import { Badge, Button, Icon, Tooltip } from '@chrissnell/chonky-ui';
  import { callsignColors, callsignMonogram } from '../../lib/callsignColor.js';
  import { messages as store } from '../../lib/messagesStore.svelte.js';
  import { acceptTactical } from '../../api/messages.js';
  import { toasts } from '../../lib/stores.js';
  import {
    ignoredInviteIds,
    markAutoNavDone,
    hasAutoNavFired,
  } from '../../lib/stores/ignoredInvites.js';
  import { timeOfDay } from './time.js';

  /** @type {{
   *    msg: any,
   *    isTactical?: boolean,
   *    showSenderLabel?: boolean,
   *    showAvatar?: boolean,
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
    showAvatar = false,
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
        // Retry budget exhausted with no ack and no explicit rej.
        // APRS doesn't mandate acks — plenty of legitimate recipients
        // (older TNCs, monitoring-only stations, operators AFK) simply
        // don't send them. Render this state identically to a normal
        // "sent" bubble — no alarm, no nag. If the operator wants to
        // try again, right-click → Resend is still available via the
        // context menu. No inline click-to-resend affordance.
        case 'timeout':  return { primary: { name: 'check', label: 'Sent' } };
        // Peer explicitly sent a rejNNN packet — they got the message
        // and actively refused it. Red alarm is correct.
        case 'rejected': return { primary: { name: 'alert-circle', label: 'Rejected by recipient — click to resend' }, failed: true };
        // Send-path failure (encode error, governor stopped mid-retry,
        // etc.) — never reached the wire or never completed. Red alarm.
        case 'failed':   return { primary: { name: 'alert-circle', label: 'Send failed — click to resend' }, failed: true };
        default:         return { primary: { name: 'clock', label: status || 'Unknown state' } };
      }
    }
    return null;
  });

  // Inline resend via the status icon is only offered for genuine
  // failure states (explicit REJ from peer, or send-path error).
  // Timeout is not a failure — operator can still right-click →
  // Resend if they want, but we don't nag with an inline button.
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

  // Extended-length badge: DTO-derived flag set true when body > 67 chars
  // (the default APRS message cap). Shown on outbound rows only — the plan
  // frames this as an operator signal for "did my contact receive it all?",
  // which has no equivalent meaning for inbound (the receiver already got
  // the full frame if it's in the list).
  const showExtendedBadge = $derived(isOut && msg?.extended === true);

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

  // --- Invite-branch state ------------------------------------------
  // An invite bubble (kind === 'invite') renders differently from a
  // normal DM bubble: inbound gets an Accept CTA, outbound gets
  // "You invited …" copy. "Joined" rendering is driven by tactical-set
  // membership, not by `msg.invite_accepted_at` (per plan — refresh-safe
  // on first paint, race-free with SSE set updates).

  const isInvite = $derived(msg?.kind === 'invite');
  const inviteTactical = $derived(msg?.invite_tactical || '');
  const toCall = $derived(msg?.to_call || msg?.peer_call || '');

  // Accept button state machine: idle → accepting → joined | failed.
  // Local to this bubble instance — a second bubble for the same TAC
  // starts at `joined` naturally because tactSet.has(tactical) flips.
  let acceptState = $state('idle'); // 'idle' | 'accepting' | 'failed'
  let acceptError = $state('');
  let openTacBtnWrap = $state(null);

  // Membership in the tactical set IS the source of truth for "Joined"
  // — `invite_accepted_at` is audit-only (see Phase 1 handoff §"For
  // Phase 3"). Checking this reactively means that accepting from
  // another tab or receiving an invite for an already-subscribed
  // tactical both render "Joined" on first paint.
  const isJoined = $derived.by(() => {
    if (!isInvite) return false;
    const t = inviteTactical;
    if (!t) return false;
    const entry = store.tacticals.get(t);
    return !!entry && entry.enabled !== false;
  });

  // Narrow viewport stacking for the inbound Accept row.
  let narrowViewport = $state(false);
  $effect(() => {
    if (typeof window === 'undefined') return;
    const mq = window.matchMedia('(max-width: 359px)');
    const apply = () => { narrowViewport = mq.matches; };
    apply();
    mq.addEventListener?.('change', apply);
    return () => mq.removeEventListener?.('change', apply);
  });

  // Is this bubble in the user's local ignored set? The parent (thread
  // view) usually filters these out, but we defensively render a
  // collapsed placeholder here as well.
  const isThisIgnored = $derived.by(() => {
    if (!msg?.id) return false;
    // Touch the subscribed store so Svelte picks up changes.
    const set = $ignoredInviteIds;
    return set?.has?.(msg.id) ?? false;
  });

  async function handleAccept() {
    if (!isInvite || !inviteTactical || acceptState === 'accepting') return;
    acceptState = 'accepting';
    acceptError = '';
    try {
      const res = await acceptTactical({
        callsign: inviteTactical,
        source_message_id: msg?.id || 0,
      });
      // Server returns { tactical, already_member }. Fold the tactical
      // into the store so the "Joined" derivation picks it up without
      // waiting for the next /tactical rollup.
      if (res?.tactical?.callsign) {
        store.tacticals.set(res.tactical.callsign, {
          id: res.tactical.id,
          alias: res.tactical.alias || '',
          enabled: res.tactical.enabled !== false,
        });
      }
      acceptState = 'idle';

      if (res?.already_member) {
        toasts.success(`Already a member of ${inviteTactical}`);
        // Do not auto-nav — they already had this tactical enabled.
        await tick();
        focusOpenLink();
      } else {
        const firstAccept = !hasAutoNavFired(msg?.id);
        if (firstAccept) {
          markAutoNavDone(msg?.id);
          // Toast with Stay-here undo is shown via chonky's toast;
          // since the base toast helper doesn't expose an action slot,
          // degrade to a plain success message. Undo-via-toast would
          // require a richer toast API — left as follow-up.
          toasts.success(`Joined ${inviteTactical}`);
          const threadId = `tactical:${inviteTactical}`;
          push(`/messages?thread=${encodeURIComponent(threadId)}`);
        } else {
          toasts.success(`Joined ${inviteTactical}`);
          await tick();
          focusOpenLink();
        }
      }
    } catch (err) {
      acceptState = 'failed';
      acceptError = err?.message || "Couldn't join";
    }
  }

  function focusOpenLink() {
    const link = openTacBtnWrap?.querySelector?.('a, button');
    link?.focus?.();
  }

  function openTacticalThread() {
    if (!inviteTactical) return;
    const threadId = `tactical:${inviteTactical}`;
    push(`/messages?thread=${encodeURIComponent(threadId)}`);
  }
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
  <!-- Meta lives to the LEFT of the bubble (for both in and out). The
       reserved min-width prevents the bubble row from jumping when meta
       fades in on pane hover. -->
  <aside class="bubble-meta" aria-label="Message metadata">
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
    {#if sourceBadge}
      <Badge variant={sourceBadge.variant} class="src-badge">{sourceBadge.label}</Badge>
    {/if}
    {#if showExtendedBadge}
      <Badge
        variant="info"
        class="src-badge ext-badge"
        title="Longer than 67 chars — some receivers may truncate."
        aria-label="Extended: longer than 67 chars, some receivers may truncate."
        data-testid="bubble-ext-badge"
      >extended</Badge>
    {/if}
    {#if fragMatch}
      <span class="frag-tag">{fragMatch.n}/{fragMatch.total}</span>
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
  </aside>

  {#if isTactical && !isOut}
    <div class="avatar-slot" aria-hidden={!showAvatar}>
      {#if showAvatar}
        <span
          class="avatar"
          style="background:{colors.stripe};color:{colors.avatarFg}"
          aria-label={`From ${sender}`}
          title={sender}
        >{monogram}</span>
      {/if}
    </div>
  {/if}

  <div class="bubble-column">
    {#if isTactical && !isOut && showSenderLabel}
      <div
        class="sender-label"
        style="color:{colors.stripe}"
        aria-label={`From ${sender}`}
      >
        {sender}
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
      {#if isInvite}
      {#if isThisIgnored}
        <p class="bubble-text invite-dismissed" data-testid="invite-dismissed">
          Invitation hidden.
        </p>
      {:else if isOut}
        <!-- Outbound invite: "You invited CALL to TAC". The ack-state
             pill is reused from the normal DM bubble machinery below. -->
        <p class="bubble-text invite-text" data-testid="invite-outbound">
          <span class="invite-emoji" aria-hidden="true">📻</span>
          You invited <strong class="invite-call">{toCall}</strong> to
          <strong class="invite-tac">{inviteTactical}</strong>
        </p>
      {:else}
        <!-- Inbound invite: broadcast line + Accept CTA (or Joined state). -->
        <div
          class="invite-inbound"
          class:narrow={narrowViewport}
          data-testid="invite-inbound"
        >
          <p class="bubble-text invite-text">
            <span class="invite-emoji" aria-hidden="true">📻</span>
            <strong class="invite-call">{sender}</strong>
            invites you to
            <strong class="invite-tac">{inviteTactical}</strong>
          </p>
          {#if isJoined}
            <div
              class="invite-actions joined"
              role="group"
              aria-label={`Invitation to ${inviteTactical}`}
              aria-live="polite"
            >
              <!-- Joined state is not Tab-focusable: skip the pill (it's
                   non-interactive text anyway) and keep Open reachable
                   only via mouse/touch when the bubble isn't the
                   operator's immediate task. Per plan: "Viewed/accepted
                   invites are not Tab-focusable." -->
              <span class="joined-pill" aria-hidden="true">
                <Icon name="check" size="xs" />
                Joined
              </span>
              <span class="open-wrap" bind:this={openTacBtnWrap}>
                <button
                  type="button"
                  class="open-tac-btn"
                  onclick={openTacticalThread}
                  tabindex="-1"
                  aria-label={`Open ${inviteTactical}`}
                  data-testid="invite-open-tac"
                >
                  Open {inviteTactical}
                  <Icon name="chevron-right" size="xs" />
                </button>
              </span>
            </div>
          {:else}
            <div
              class="invite-actions"
              role="group"
              aria-label={`Invitation to ${inviteTactical}`}
              aria-live="polite"
            >
              <button
                type="button"
                class="accept-btn"
                onclick={handleAccept}
                disabled={acceptState === 'accepting'}
                aria-label={`Accept invitation and join ${inviteTactical}`}
                data-testid="invite-accept"
              >
                {#if acceptState === 'accepting'}
                  <span class="accept-spin" aria-hidden="true">
                    <Icon name="refresh-cw" size="sm" />
                  </span>
                  Joining…
                {:else}
                  <Icon name="check" size="sm" />
                  Accept · Join {inviteTactical}
                {/if}
              </button>
              {#if acceptState === 'failed'}
                <div class="accept-error" role="alert">
                  <span>{acceptError || "Couldn't join."} Retry.</span>
                  <button
                    type="button"
                    class="accept-retry"
                    onclick={handleAccept}
                    aria-label={`Retry joining ${inviteTactical}`}
                    data-testid="invite-accept-retry"
                  >
                    <Icon name="refresh-cw" size="xs" />
                    Retry
                  </button>
                </div>
              {/if}
            </div>
          {/if}
        </div>
      {/if}
    {:else}
      <p class="bubble-text">{bodyText}</p>
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
    flex-direction: row;
    align-items: flex-end;
    gap: 8px;
    position: relative;
    max-width: 92%;
    margin: 2px 0;
  }
  .bubble-wrap.out {
    align-self: flex-end;
  }
  .bubble-wrap.in {
    align-self: flex-start;
  }

  /* Column holding sender-label (tactical incoming only) + bubble. Lives
     to the right of the meta sidebar. */
  .bubble-column {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    min-width: 0;
  }
  .bubble-wrap.out .bubble-column {
    align-items: flex-end;
  }

  /* Sender callsign sits above the first bubble of an incoming
     tactical cluster. Plain bold colored text (in the sender's stripe
     color) — no pill background, which made the prior design read as
     blue-on-blue / teal-on-teal in light mode. */
  .sender-label {
    display: inline-block;
    margin: 0 0 3px 4px;
    font-size: 11px;
    font-family: var(--font-mono);
    font-weight: 700;
    letter-spacing: 0.4px;
  }

  /* Avatar slot is a fixed-width column to the left of the bubble for
     tactical incoming messages. Width is reserved on every bubble in a
     cluster so the bubble left edges line up; only the cluster head
     (and the every-5th repeat) actually paints the avatar. */
  .avatar-slot {
    width: 30px;
    flex-shrink: 0;
    align-self: flex-end;
    display: flex;
    align-items: flex-end;
    justify-content: center;
    /* Bottom-align with bubble baseline (bubble has 7 px bottom pad). */
    padding-bottom: 1px;
  }
  .avatar {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    border-radius: 999px;
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 800;
    letter-spacing: 0.5px;
    /* Subtle ring to lift the circle off both light and dark surfaces
       without depending on the page background. */
    box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.08);
    user-select: none;
  }

  .bubble {
    position: relative;
    padding: 7px 12px;
    border-radius: 12px;
    border: 1px solid var(--color-border);
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
    border-color: var(--color-primary);
    border-radius: 12px 12px 4px 12px;
  }
  .bubble-wrap.in .bubble {
    border-radius: 12px 12px 12px 4px;
  }
  .bubble.has-stripe {
    border-left: 2px solid var(--stripe-color, var(--color-primary));
    padding-left: 14px;
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

  /* Meta sits to the LEFT of the bubble for both in/out, packed flush
     against the bubble via justify-content: flex-end. */
  .bubble-meta {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 6px;
    flex-shrink: 0;
    min-width: 78px;
    padding-bottom: 6px;
    font-family: var(--font-mono);
  }

  /* bits-ui's Tooltip.Trigger renders a native <button>; strip the
     browser default chrome so only the icon shows. */
  :global(.status-tt) {
    background: transparent;
    border: none;
    padding: 0;
    margin: 0;
    cursor: default;
    color: inherit;
    font: inherit;
    line-height: 0;
  }
  :global(.status-tt:focus-visible) {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
    border-radius: 4px;
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

  /* Failed outbound gets a subtle red border on the whole bubble so
     the operator notices at a glance. (Meta is always visible now.) */
  .bubble-wrap.failed .bubble {
    border-color: var(--color-danger);
    box-shadow: 0 0 0 1px var(--color-danger-muted) inset;
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

  /* --- Invite branch ------------------------------------------------ */
  .invite-inbound {
    display: flex;
    flex-direction: row;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .invite-inbound.narrow {
    flex-direction: column;
    align-items: flex-start;
  }
  .invite-text {
    margin: 0;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui,
      'Helvetica Neue', Arial, sans-serif;
    font-size: 14px;
    line-height: 1.35;
  }
  .invite-emoji {
    margin-right: 4px;
  }
  .invite-call,
  .invite-tac {
    font-family: var(--font-mono);
    letter-spacing: 0.3px;
  }
  .invite-dismissed {
    margin: 0;
    font-style: italic;
    color: var(--color-text-dim);
    font-size: 12px;
  }
  .invite-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .invite-actions.joined {
    margin-top: 2px;
  }

  .accept-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px;
    border-radius: 8px;
    border: 1px solid var(--color-primary);
    background: var(--color-primary);
    color: var(--color-primary-foreground, #fff);
    font-family: var(--font-mono);
    font-size: 13px;
    font-weight: 600;
    letter-spacing: 0.3px;
    cursor: pointer;
    transition: filter 0.15s, background 0.15s;
  }
  .accept-btn:hover:not(:disabled) {
    filter: brightness(1.08);
  }
  .accept-btn:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }
  .accept-btn:disabled {
    opacity: 0.7;
    cursor: wait;
  }
  .accept-spin {
    display: inline-flex;
    align-items: center;
  }
  .accept-spin :global(svg) {
    animation: bubble-invite-spin 1s linear infinite;
  }
  @keyframes bubble-invite-spin {
    from { transform: rotate(0deg); }
    to   { transform: rotate(360deg); }
  }
  .accept-error {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 4px 8px;
    background: var(--color-danger-muted);
    border: 1px solid var(--color-danger);
    border-radius: 6px;
    color: var(--color-danger);
    font-size: 12px;
  }
  .accept-retry {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 8px;
    border-radius: 4px;
    background: transparent;
    border: 1px solid var(--color-danger);
    color: var(--color-danger);
    font-family: var(--font-mono);
    font-size: 11px;
    cursor: pointer;
  }
  .accept-retry:hover {
    background: var(--color-danger);
    color: var(--color-primary-foreground, #fff);
  }
  .accept-retry:focus-visible {
    outline: 2px solid var(--color-danger);
    outline-offset: 1px;
  }

  .joined-pill {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 8px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: 999px;
    color: var(--color-text-muted);
    font-family: var(--font-mono);
    font-size: 11px;
    letter-spacing: 0.5px;
  }
  .open-wrap {
    display: inline-flex;
  }
  .open-tac-btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 10px;
    border-radius: 6px;
    background: var(--color-surface-raised);
    border: 1px solid var(--color-primary);
    color: var(--color-primary);
    font-family: var(--font-mono);
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.3px;
    cursor: pointer;
    text-decoration: none;
  }
  .open-tac-btn:hover {
    background: var(--color-primary);
    color: var(--color-primary-foreground, #fff);
  }
  .open-tac-btn:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }
</style>
