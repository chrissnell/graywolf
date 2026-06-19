<!-- web/src/routes/ptt/PttCard.svelte -->
<script>
  import { Button, Badge } from '@chrissnell/chonky-ui';
  import { postChannelPtt } from '../../lib/api.js';
  import { truncatePath } from './format.js';

  let {
    item,
    channelName,
    methodLabel,
    onChangeMethod,
    onChangeDevice,
    onDelete,
  } = $props();

  let testing = $state(false);

  async function testPtt() {
    if (!item.channel_id || testing) return;
    testing = true;
    try {
      await postChannelPtt(item.channel_id, true);
      // Hold for ~1s, then unkey. Single shot — no heartbeat.
      await new Promise(r => setTimeout(r, 1000));
      await postChannelPtt(item.channel_id, false);
    } catch (err) {
      console.error('Test PTT failed:', err);
      // Best-effort unkey on error
      try { await postChannelPtt(item.channel_id, false); } catch { /* ignore */ }
    } finally {
      testing = false;
    }
  }

</script>

<div class="device-card">
  <div class="device-header">
    <span class="device-name">{channelName || `Channel ${item.channel_id}`}</span>
    <Badge variant={item.method === 'none' ? 'default' : 'success'}>
      {methodLabel}
    </Badge>
  </div>

  <dl class="device-details">
    <dt>Method</dt>
    <dd class="value-text">{methodLabel}</dd>

    {#if item.method !== 'none' && item.method !== 'vox' && item.method !== 'digirig_tone'}
      <dt>Device</dt>
      <dd title={item.device_path}>{truncatePath(item.device_path)}</dd>
    {/if}
    {#if item.method === 'vox'}
      <dt>Lead-in tone</dt>
      <dd class="muted">500 ms before each packet</dd>
    {/if}
    {#if item.method === 'digirig_tone'}
      <dt>Keying</dt>
      <dd class="muted">Right-channel tone, left-channel audio</dd>
    {/if}
    {#if item.method === 'cm108'}
      <dt>GPIO Pin</dt>
      <dd>GPIO {item.gpio_pin} (pin {item.gpio_pin + 10})</dd>
    {/if}
    {#if item.method === 'gpio'}
      <dt>GPIO Line</dt>
      <dd>Line {item.gpio_line ?? 0}</dd>
    {/if}
    {#if item.method === 'none'}
      <dt>Status</dt>
      <dd class="muted">No PTT method set</dd>
    {/if}
  </dl>

  <div class="device-actions">
    <Button variant="primary" onclick={() => onChangeMethod(item)}>Change Method</Button>
    <Button variant="primary" onclick={() => onChangeDevice(item)}>Change Device</Button>
    <Button disabled={testing || item.method === 'none' || item.method === 'vox' || item.method === 'digirig_tone'} onclick={testPtt}>
      {testing ? 'Keying…' : 'Test PTT (1s)'}
    </Button>
    <Button variant="danger" onclick={() => onDelete(item)}>Delete</Button>
  </div>
</div>

<style>
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
    gap: 12px;
    padding-bottom: 12px;
    margin-bottom: 12px;
    border-bottom: 1px solid var(--border-color);
  }
  .device-name {
    font-weight: 600;
    font-size: 15px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .device-details {
    display: grid;
    grid-template-columns: auto 1fr;
    column-gap: 16px;
    row-gap: 6px;
    margin: 0 0 16px;
    font-size: 13px;
  }
  .device-details dt {
    color: var(--text-secondary);
    font-weight: 500;
  }
  .device-details dd {
    margin: 0;
    font-family: var(--font-mono);
    color: var(--text-primary);
    text-align: right;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .device-details dd.value-text {
    font-family: inherit;
  }
  .device-details dd.muted {
    color: var(--text-muted);
    font-style: italic;
    font-family: inherit;
    text-align: left;
  }

  .device-actions {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
  }
  .device-actions :global(.btn) {
    width: 100%;
    justify-content: center;
  }
</style>
