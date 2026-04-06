<script>
  import { onMount } from 'svelte';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Card from '../components/Card.svelte';
  import ToggleSwitch from '../components/ToggleSwitch.svelte';

  let enabled = $state(false);
  let packets = $state([]);

  onMount(async () => {
    const data = await api.get('/simulation');
    if (data) {
      enabled = data.enabled;
      packets = data.packets || [];
    }
  });

  async function toggle() {
    try {
      await api.put('/simulation', { enabled });
      toasts.success(enabled ? 'Simulation enabled' : 'Simulation disabled');
    } catch (err) {
      toasts.error(err.message);
    }
  }

  function formatTime(ts) {
    return new Date(ts).toLocaleTimeString();
  }
</script>

<PageHeader title="Simulation" subtitle="Simulation mode for testing without RF" />

<Card>
  <div class="sim-toggle">
    <ToggleSwitch bind:checked={enabled} label="Enable simulation mode" id="sim-on" />
    <button class="save-toggle" onclick={toggle}>Apply</button>
  </div>
  <p class="sim-note">
    When enabled, packets are processed without actual RF transmission/reception.
    Useful for testing configurations.
  </p>
</Card>

<div style="margin-top: 16px;">
  <Card title="Logged Packets">
    <div class="packet-log">
      {#if packets.length === 0}
        <div class="empty">No logged packets</div>
      {:else}
        {#each packets as pkt}
          <div class="log-row">
            <span class="log-time">{formatTime(pkt.timestamp)}</span>
            <span class="log-dir" class:rx={pkt.direction === 'rx'} class:tx={pkt.direction === 'tx'}>
              {pkt.direction.toUpperCase()}
            </span>
            <span class="log-raw">{pkt.raw}</span>
          </div>
        {/each}
      {/if}
    </div>
  </Card>
</div>

<style>
  .sim-toggle {
    display: flex;
    align-items: center;
    gap: 16px;
  }
  .save-toggle {
    background: var(--bg-tertiary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    color: var(--text-primary);
    padding: 6px 14px;
    cursor: pointer;
    font-size: 13px;
  }
  .save-toggle:hover {
    background: var(--bg-hover);
  }
  .sim-note {
    margin-top: 12px;
    font-size: 13px;
    color: var(--text-muted);
  }
  .packet-log {
    max-height: 400px;
    overflow-y: auto;
    font-size: 12px;
  }
  .log-row {
    display: flex;
    gap: 8px;
    padding: 4px 0;
    border-bottom: 1px solid var(--border-light);
  }
  .log-time { color: var(--text-muted); min-width: 70px; }
  .log-dir {
    font-weight: 700;
    font-size: 10px;
    padding: 1px 4px;
    border-radius: 3px;
  }
  .rx { background: rgba(63, 185, 80, 0.2); color: var(--success); }
  .tx { background: rgba(88, 166, 255, 0.2); color: var(--accent); }
  .log-raw { color: var(--text-secondary); word-break: break-all; }
  .empty { color: var(--text-muted); text-align: center; padding: 20px; }
</style>
