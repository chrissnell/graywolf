<script>
  // Global "no conversations, no tacticals" empty state rendered
  // inside the thread pane when the inbox is completely empty.
  //
  // Tactical-thread empty state (variant 2) and DM empty state
  // (variant 3) live inside MessageThread.svelte — they have to
  // share scroll + compose context with the bubbles container.

  import { Button, EmptyState, Icon } from '@chrissnell/chonky-ui';

  /** @type {{ onNew?: () => void, onAddTactical?: () => void }} */
  let { onNew, onAddTactical } = $props();
</script>

<div class="wrap" data-testid="messages-empty-inbox">
  <EmptyState>
    <div class="inner">
      <Icon name="message-square" size="xl" />
      <h3>No messages yet</h3>
      <p>
        Send your first APRS message to another station, or add a
        tactical callsign to monitor a group net.
      </p>
      <div class="actions">
        <Button variant="primary" onclick={() => onNew?.()} data-testid="empty-new-message">
          <Icon name="plus" size="sm" />
          New message
        </Button>
        <Button variant="ghost" onclick={() => onAddTactical?.()} data-testid="empty-add-tactical">
          <Icon name="radio-tower" size="sm" />
          Add tactical
        </Button>
      </div>
    </div>
  </EmptyState>
</div>

<style>
  .wrap {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    padding: 24px;
  }
  .inner {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    max-width: 380px;
    text-align: center;
  }
  h3 {
    margin: 8px 0 0;
    font-size: 16px;
    font-weight: 600;
  }
  p {
    margin: 0;
    color: var(--color-text-muted);
    font-size: 13px;
    line-height: 1.5;
  }
  .actions {
    display: inline-flex;
    gap: 8px;
    margin-top: 12px;
  }
  :global(.wrap .actions button) {
    gap: 4px;
  }
</style>
