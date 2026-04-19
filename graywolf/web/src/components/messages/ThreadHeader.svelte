<script>
  // Thread header. Branches on kind:
  //   - DM:       peer callsign + last-heard relative + APRS symbol (if known)
  //   - Tactical: tactical label + alias + broadcast icon + participant
  //               chip row + monitoring <Toggle>
  // Back chevron is rendered on mobile (<768 px) so the user can pop
  // back to the conversation list.

  import { Icon, Toggle, Tooltip } from '@chrissnell/chonky-ui';
  import ParticipantChips from './ParticipantChips.svelte';
  import InviteToTacticalModal from './InviteToTacticalModal.svelte';
  import { relativeLong } from './time.js';

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
            class="invite-btn"
            onclick={openInvite}
            aria-label={`Invite stations to ${tacticalKey}`}
            data-testid="thread-invite-btn"
          >
            <Icon name="users" size="md" />
          </button>
        </Tooltip.Trigger>
        <Tooltip.Content>Invite</Tooltip.Content>
      </Tooltip>
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
  .monitor {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
  }
  .invite-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 32px;
    height: 32px;
    padding: 0;
    border: 1px solid transparent;
    border-radius: var(--radius);
    background: transparent;
    color: var(--color-text-muted);
    cursor: pointer;
    transition: background 0.15s, color 0.15s, border-color 0.15s;
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
  .chips {
    padding-left: 34px;
    min-width: 0;
    overflow: hidden;
  }

  @media (max-width: 767px) {
    .chips { padding-left: 0; }
  }
</style>
