<!-- web/src/routes/ptt/PttCard.svelte -->
<script>
  import { Button, Badge } from '@chrissnell/chonky-ui';

  let {
    item,
    channelName,
    methodLabel,
    onEdit,
    onDelete,
  } = $props();

  function truncatePath(p, max = 40) {
    if (!p || p.length <= max) return p || '—';
    return '...' + p.slice(-(max - 3));
  }
</script>

<div class="device-card">
  <div class="device-header">
    <span class="device-name">{channelName || `Channel ${item.channel_id}`}</span>
    <div class="device-badges">
      <Badge variant={item.method === 'none' ? 'default' : 'success'}>
        {methodLabel}
      </Badge>
    </div>
  </div>
  <div class="device-details">
    {#if item.method !== 'none'}
      <div class="detail-row">
        <span class="detail-label">Device</span>
        <span class="detail-value" title={item.device_path}>{truncatePath(item.device_path)}</span>
      </div>
    {/if}
    {#if item.method === 'cm108'}
      <div class="detail-row">
        <span class="detail-label">GPIO Pin</span>
        <span class="detail-value">GPIO {item.gpio_pin} (pin {item.gpio_pin + 10})</span>
      </div>
    {/if}
    {#if item.method === 'gpio'}
      <div class="detail-row">
        <span class="detail-label">GPIO Line</span>
        <span class="detail-value">Line {item.gpio_line ?? 0}</span>
      </div>
    {/if}
    {#if item.method === 'none'}
      <div class="detail-row">
        <span class="detail-label">Status</span>
        <span class="detail-value muted">No PTT method set</span>
      </div>
    {/if}
  </div>
  <div class="device-actions">
    <Button variant="ghost" onclick={() => onEdit(item)}>Edit</Button>
    <Button variant="danger" onclick={() => onDelete(item)}>Delete</Button>
  </div>
</div>

<style>
  /* Card styles copied verbatim from Ptt.svelte to preserve visual parity. */
  .device-card {
    display: flex;
    flex-direction: column;
    padding: 16px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
  }
  .device-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
    gap: 8px;
  }
  .device-name {
    font-weight: 600;
    font-size: 15px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .device-badges {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }
  .device-details {
    display: flex;
    flex-direction: column;
    gap: 6px;
    flex: 1;
  }
  .detail-row {
    display: flex;
    justify-content: space-between;
    font-size: 13px;
    gap: 12px;
  }
  .detail-label {
    color: var(--text-secondary);
    flex-shrink: 0;
  }
  .detail-value {
    font-family: var(--font-mono);
    color: var(--text-primary);
    text-align: right;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .detail-value.muted {
    color: var(--text-muted);
    font-style: italic;
    font-family: inherit;
  }
  .device-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }
</style>
