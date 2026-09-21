<script>
  import { onMount } from 'svelte';
  import { Button, Input, Box, Radio, RadioGroup } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';
  import ChannelListbox from '../lib/components/ChannelListbox.svelte';
  import { channelsStore, start as startChannelsStore } from '../lib/stores/channels.svelte.js';
  import { txPredicate } from '../lib/channelBacking.js';
  import { computeCotSchedule } from '../lib/cotSchedule.js';

  // Channel picker needs the shared channels store (same pattern as
  // Beacons.svelte); idempotent to start alongside another page that
  // already kicked it.
  startChannelsStore();
  let channels = $derived(channelsStore.list);

  // --- Cursor-on-Target settings ---------------------------------------
  // Global parameters every CoT target (created from the live map's
  // "Add CoT" dialog) snapshots onto itself at creation time -- editing
  // these never changes an already-created target's schedule.
  let cot = $state({
    type: 'object', send_path: 'rf', channel: 0,
    destination: 'APGRWO', path: 'WIDE1-1,WIDE2-1',
    num_transmits: '4', second_tx_delay_seconds: '300', decay_factor: '2',
  });
  let savingCot = $state(false);

  onMount(async () => {
    const c = await api.get('/cot-settings');
    if (c) cot = {
      type: c.type, send_path: c.send_path, channel: c.channel,
      destination: c.destination, path: c.path,
      num_transmits: String(c.num_transmits),
      second_tx_delay_seconds: String(c.second_tx_delay_seconds),
      decay_factor: String(c.decay_factor),
    };
  });

  // Only "Object" exists today; the picker is a locked single-option
  // radio so the settings shape (and this UI) don't need to change when
  // a future CoT kind is added.
  const needsChannel = $derived(cot.send_path !== 'is_only');
  const cotSchedule = $derived(
    computeCotSchedule(parseInt(cot.num_transmits) || 0, parseInt(cot.second_tx_delay_seconds) || 0, parseFloat(cot.decay_factor) || 0)
  );

  async function saveCot(e) {
    e.preventDefault();
    const numTransmits = parseInt(cot.num_transmits);
    const secondDelay = parseInt(cot.second_tx_delay_seconds);
    const decay = parseFloat(cot.decay_factor);
    if (!Number.isFinite(numTransmits) || numTransmits < 1) {
      toasts.error('Number of Transmits must be at least 1');
      return;
    }
    if (!Number.isFinite(secondDelay) || secondDelay < 1) {
      toasts.error('2nd TX Delay must be at least 1 second');
      return;
    }
    if (!Number.isFinite(decay) || decay <= 0) {
      toasts.error('Decay Factor must be greater than 0');
      return;
    }
    savingCot = true;
    try {
      await api.put('/cot-settings', {
        type: 'object',
        send_path: cot.send_path,
        channel: needsChannel ? (parseInt(cot.channel) || 0) : 0,
        destination: cot.destination,
        path: cot.path,
        num_transmits: numTransmits,
        second_tx_delay_seconds: secondDelay,
        decay_factor: decay,
      });
      toasts.success('Cursor-on-Target settings saved');
      if (cot.send_path === 'is_only') await ensureIgateEnabledForCot();
    } catch (err) {
      toasts.error(err.message);
    } finally {
      savingCot = false;
    }
  }

  // Mirrors Beacons.svelte's ensureIgateEnabled: an is_only CoT target
  // only reaches APRS-IS once the iGate is connected, so auto-enable it
  // on save. Best-effort -- settings are already saved either way.
  async function ensureIgateEnabledForCot() {
    try {
      const igCfg = await api.get('/igate/config');
      if (!igCfg || igCfg.enabled) return;
      await api.put('/igate/config', { ...igCfg, enabled: true });
      toasts.success('iGate enabled so APRS-IS-only Cursor-on-Targets can reach the network');
    } catch (err) {
      toasts.error(`Settings saved, but the iGate could not be enabled automatically: ${err.message || 'enable it on the iGate page.'}`);
    }
  }
</script>

<PageHeader title="Cursor on Target" subtitle="Cursor-on-Target (CoT) tuning" />

<Box title="Cursor-on-Target Settings">
  <p class="sb-intro">
    These parameters apply to every Cursor-on-Target (CoT) object you drop
    from the live map's "Add CoT" dialog. A CoT transmits once
    immediately, then retransmits on the decaying schedule below until it
    reaches the transmit count -- each target snapshots these settings for
    itself when it's created, so changing them here never alters a target
    that already exists.
  </p>
  <form onsubmit={saveCot}>
    <FormField label="Type" id="cot-type">
      <RadioGroup value="object">
        <Radio value="object" label="Object" />
      </RadioGroup>
    </FormField>
    <FormField label="Send To" id="cot-send-path"
      hint="Where every new CoT target is transmitted.">
      <RadioGroup bind:value={cot.send_path}>
        <div class="cot-radio-row">
          <Radio value="rf" label="RF only" />
          <Radio value="both" label="RF + APRS-IS" />
          <Radio value="is_only" label="APRS-IS only (no radio)" />
        </div>
      </RadioGroup>
    </FormField>
    {#if needsChannel}
      <FormField label="Channel" id="cot-channel"
        hint="Radio channel new CoT targets transmit on. Auto always uses the first APRS-eligible channel.">
        <ChannelListbox
          id="cot-channel"
          bind:value={cot.channel}
          valueType="number"
          channels={channels}
          capabilityFilter={txPredicate}
          allowNone
          noneLabel="Auto (first APRS-eligible channel)"
        />
      </FormField>
    {/if}
    <div class="sb-grid">
      <FormField label="Destination" id="cot-dest">
        <Input id="cot-dest" bind:value={cot.destination} placeholder="APGRWO" />
      </FormField>
      <FormField label="Path" id="cot-path">
        <Input id="cot-path" bind:value={cot.path} placeholder="WIDE1-1,WIDE2-1" />
      </FormField>
    </div>

    <h4 class="sb-section-label">Retransmit schedule</h4>
    <p class="sb-section-desc">
      The first transmit is always immediate. Every later one fires this
      many seconds after the previous offset, multiplied by the decay
      factor each time -- so the interval grows the longer a target has
      been on the air.
    </p>
    <div class="sb-grid">
      <FormField label="Number of Transmits" id="cot-num-tx"
        hint="Total sends including the immediate first one. Default 4.">
        <Input id="cot-num-tx" bind:value={cot.num_transmits} type="number" min="1" />
      </FormField>
      <FormField label="2nd TX Delay (seconds)" id="cot-2nd-delay"
        hint="Delay before the second transmit. Default 300 (5 minutes).">
        <Input id="cot-2nd-delay" bind:value={cot.second_tx_delay_seconds} type="number" min="1" />
      </FormField>
      <FormField label="Decay Factor" id="cot-decay"
        hint="Multiplies each successive delay. Default 2 (each interval doubles).">
        <Input id="cot-decay" bind:value={cot.decay_factor} type="number" step="0.1" min="0.1" />
      </FormField>
    </div>

    <h4 class="sb-section-label">Preview</h4>
    <table class="cot-preview-table">
      <thead>
        <tr><th>TX</th><th>Offset (s)</th><th>Time</th></tr>
      </thead>
      <tbody>
        {#each cotSchedule as row (row.n)}
          <tr>
            <td>TX {row.n}</td>
            <td>{row.offsetSeconds}</td>
            <td>({row.hms})</td>
          </tr>
        {/each}
      </tbody>
    </table>

    <div class="form-actions">
      <Button variant="primary" type="submit" disabled={savingCot}>Save</Button>
    </div>
  </form>
</Box>

<style>
  .sb-intro {
    font-size: 14px;
    line-height: 1.5;
    color: var(--color-text-muted, #888);
    margin: 0 0 16px 0;
  }
  .sb-section-label {
    margin: 20px 0 4px 0;
    font-size: 14px;
    font-weight: 600;
  }
  .sb-section-desc {
    font-size: 13px;
    line-height: 1.5;
    color: var(--color-text-muted, #888);
    margin: 0 0 8px 0;
  }
  .sb-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 0 16px;
    margin-top: 12px;
  }
  .form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }

  .cot-radio-row {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .cot-preview-table {
    border-collapse: collapse;
    margin-top: 8px;
    font-size: 13px;
    font-family: var(--font-mono);
  }
  .cot-preview-table th,
  .cot-preview-table td {
    text-align: left;
    padding: 4px 16px 4px 0;
  }
  .cot-preview-table th {
    color: var(--color-text-muted, #888);
    font-weight: 600;
    font-family: inherit;
    border-bottom: 1px solid var(--border-color);
  }
</style>
