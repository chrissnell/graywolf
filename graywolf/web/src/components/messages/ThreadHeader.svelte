<script>
  // Thread header. Branches on kind:
  //   - DM:       peer callsign + last-heard relative + APRS symbol (if known)
  //   - Tactical: tactical label + alias + broadcast icon + participant
  //               chip row + monitoring <Toggle>
  // Back chevron is rendered on mobile (<768 px) so the user can pop
  // back to the conversation list.

  import { AlertDialog, Icon, Toggle, Tooltip } from '@chrissnell/chonky-ui';
  import { push } from 'svelte-spa-router';
  import ParticipantChips from './ParticipantChips.svelte';
  import InviteToTacticalModal from './InviteToTacticalModal.svelte';
  import { relativeLong } from './time.js';
  import { messages as store } from '../../lib/messagesStore.svelte.js';
  import { deleteTactical } from '../../api/messages.js';
  import { refreshNow } from '../../lib/messagesTransport.js';
  import { toasts } from '../../lib/stores.js';

  /** @type {{
   *    thread: any,
   *    isTactical?: boolean,
   *    isMobile?: boolean,
   *    onBack?: () => void,
   *    onMuteToggle?: (muted: boolean) => void,
   *    onOpenDm?: (callsign: string) => void,
   *  }}
   */
  let {
    thread,
    isTactical = false,
    isMobile = false,
    onBack,
    onMuteToggle,
    onOpenDm,
  } = $props();

  let inviteOpen = $state(false);
  const tacticalKey = $derived(thread?.key || '');

  function openInvite() {
    inviteOpen = true;
  }
  function closeInvite() {
    inviteOpen = false;
  }

  const lastHeard = $derived(thread?.lastAt ? relativeLong(thread.lastAt) : '');
  const muted = $derived(!!thread?.muted);

  function handleMute(checked) {
    onMuteToggle?.(!checked);
  }

  // --- Leave chat ---------------------------------------------------
  // Unsubscribes the operator from the tactical (DELETE
  // /messages/tactical/{id}). Historical message rows persist on the
  // server but the conversation row falls out of the next /conversations
  // rollup; we trigger that refresh explicitly so the list updates
  // without waiting for the periodic poll.
  let leaveOpen = $state(false);
  let leaving = $state(false);

  function openLeave() {
    leaveOpen = true;
  }
  async function runLeave() {
    if (leaving) return;
    const entry = store.tacticals.get(tacticalKey);
    if (!entry?.id) {
      toasts.error(`Could not find tactical record for ${tacticalKey}`);
      leaveOpen = false;
      return;
    }
    leaving = true;
    try {
      await deleteTactical(entry.id);
      store.tacticals.delete(tacticalKey);
      // Drop the conversation immediately so the row vanishes from the
      // list before the rollup completes. The server-side rollup will
      // confirm on the next refresh.
      const threadId = `tactical:${tacticalKey}`;
      store.conversations.delete(threadId);
      refreshNow();
      toasts.success(`Left ${tacticalKey}`);
      leaveOpen = false;
      push('/messages');
    } catch (e) {
      toasts.error(e?.message || `Couldn't leave ${tacticalKey}`);
    } finally {
      leaving = false;
    }
  }
</script>

<header class="thread-header" class:tactical={isTactical} data-testid="thread-header">
  <div class="row primary">
    {#if isMobile && onBack}
      <button type="button" class="back" onclick={() => onBack?.()} aria-label="Back to conversations" data-testid="thread-back">
        <Icon name="chevron-left" size="md" />
      </button>
    {/if}
    <span class="lead" aria-hidden="true">
      <Icon name={isTactical ? 'radio-tower' : 'user'} size="md" />
    </span>
    <div class="title-block">
      <div class="title-line">
        <span class="title">{thread?.key || ''}</span>
        {#if isTactical && thread?.alias}
          <span class="subtitle">{thread.alias}</span>
        {/if}
      </div>
      {#if !isTactical && lastHeard}
        <span class="sub">Last heard {lastHeard}</span>
      {/if}
    </div>
    {#if isTactical}
      <div class="actions">
        <div class="monitor">
          <Toggle
            checked={!muted}
            onCheckedChange={handleMute}
            label="Monitor"
            aria-label={muted ? 'Unmute tactical monitoring' : 'Mute tactical monitoring'}
          />
        </div>
        <Tooltip>
          <Tooltip.Trigger>
            <button
              type="button"
              class="leave-btn"
              onclick={openLeave}
              aria-label={`Leave ${tacticalKey}`}
              data-testid="thread-leave-btn"
            >
              <Icon name="log-out" size="md" />
              <span class="action-label">Leave Chat</span>
            </button>
          </Tooltip.Trigger>
          <Tooltip.Content>Leave chat</Tooltip.Content>
        </Tooltip>
        <Tooltip>
          <Tooltip.Trigger>
            <button
              type="button"
              class="invite-btn"
              onclick={openInvite}
              aria-label={`Invite stations to ${tacticalKey}`}
              data-testid="thread-invite-btn"
            >
              <Icon name="users" size="md" />
              <span class="action-label">Invite Users</span>
            </button>
          </Tooltip.Trigger>
          <Tooltip.Content>Invite</Tooltip.Content>
        </Tooltip>
      </div>
    {/if}
  </div>
  {#if isTactical}
    <div class="row chips">
      <ParticipantChips tacticalKey={tacticalKey} {onOpenDm} />
    </div>
  {/if}
</header>

{#if isTactical}
  <InviteToTacticalModal
    tactical={tacticalKey}
    bind:open={inviteOpen}
    onClose={closeInvite}
  />

  <AlertDialog bind:open={leaveOpen}>
    <AlertDialog.Content>
      <AlertDialog.Title>Leave {tacticalKey}?</AlertDialog.Title>
      <AlertDialog.Description>
        You'll stop seeing messages on <strong>{tacticalKey}</strong> and
        the conversation will be removed from your messages list.
        Existing message history is preserved on the server and other
        members are not notified.
      </AlertDialog.Description>
      <div class="alert-footer">
        <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
        <AlertDialog.Action
          class="danger-action"
          onclick={runLeave}
          disabled={leaving}
          data-testid="thread-leave-confirm"
        >
          {leaving ? 'Leaving…' : 'Leave Chat'}
        </AlertDialog.Action>
      </div>
    </AlertDialog.Content>
  </AlertDialog>
{/if}

<style>
  .thread-header {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px 16px;
    background: var(--color-surface);
    border-bottom: 1px solid var(--color-border);
    flex-shrink: 0;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .primary {
    flex-wrap: nowrap;
  }
  .back {
    background: transparent;
    border: none;
    color: var(--color-text-muted);
    cursor: pointer;
    padding: 4px;
    border-radius: var(--radius);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }
  .back:hover { background: var(--color-surface-raised); color: var(--color-text); }
  .lead {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--color-text-muted);
    flex-shrink: 0;
  }
  .tactical .lead { color: var(--color-primary); }
  .title-block {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1 1 auto;
    min-width: 0;
  }
  .title-line {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
  }
  .title {
    font-family: var(--font-mono);
    font-weight: 700;
    font-size: 15px;
    color: var(--color-text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .subtitle {
    font-size: 12px;
    color: var(--color-text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }
  .sub {
    font-size: 11px;
    color: var(--color-text-dim);
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
  }
  .monitor {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
  }
  .invite-btn,
  .leave-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    flex-shrink: 0;
    height: 32px;
    padding: 0 10px;
    border: 1px solid transparent;
    border-radius: var(--radius);
    background: transparent;
    color: var(--color-text-muted);
    cursor: pointer;
    transition: background 0.15s, color 0.15s, border-color 0.15s;
    font: inherit;
    line-height: 1;
  }
  .action-label {
    font-size: 0.875rem;
    white-space: nowrap;
  }
  .invite-btn:hover {
    background: var(--color-surface-raised);
    color: var(--color-primary);
    border-color: var(--color-border);
  }
  .invite-btn:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }
  .leave-btn:hover {
    background: var(--color-danger-muted);
    color: var(--color-danger);
    border-color: var(--color-danger);
  }
  .leave-btn:focus-visible {
    outline: 2px solid var(--color-danger);
    outline-offset: 2px;
  }

  .alert-footer {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    padding: 1rem 1.5rem 1.25rem;
  }
  :global(.danger-action) {
    background: var(--color-danger) !important;
    color: white !important;
    border-color: var(--color-danger) !important;
  }
  :global(.danger-action:disabled) {
    opacity: 0.6;
    cursor: not-allowed;
  }
  .chips {
    padding-left: 34px;
    min-width: 0;
    overflow: hidden;
  }

  @media (max-width: 767px) {
    .chips { padding-left: 0; }
    /* Let actions wrap to their own row below the title so the
       tactical name isn't crushed to "GR…" next to a Monitor toggle
       and "Invite Users" button. */
    .primary { flex-wrap: wrap; }
    .actions {
      flex: 1 1 100%;
      justify-content: flex-end;
      margin-top: 2px;
    }
  }
</style>
