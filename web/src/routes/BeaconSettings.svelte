<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle, Box, Radio, RadioGroup } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';
  import ChannelListbox from '../lib/components/ChannelListbox.svelte';
  import { channelsStore, start as startChannelsStore } from '../lib/stores/channels.svelte.js';
  import { txPredicate } from '../lib/channelBacking.js';
  import { computeCotSchedule } from '../lib/cotSchedule.js';

  // Moved off the Beacons page (which can run long once an operator has
  // many beacons configured) so SmartBeaconing tuning is always one
  // screen, not a scroll away. See docs/wiki/code-map.md route table.
  let activeTab = $state('smart-beaconing');

  // Channel picker needs the shared channels store (same pattern as
  // Beacons.svelte); idempotent to start alongside another page that
  // already kicked it.
  startChannelsStore();
  let channels = $derived(channelsStore.list);

  let smartBeacon = $state({
    enabled: false, fast_speed: '60', fast_rate: '60', slow_speed: '5', slow_rate: '1800',
    min_turn_angle: '28', turn_slope: '26', min_turn_time: '30',
  });
  let savingSB = $state(false);

  onMount(async () => {
    const sb = await api.get('/smart-beacon');
    if (sb) smartBeacon = {
      enabled: sb.enabled,
      fast_speed: String(sb.fast_speed), fast_rate: String(sb.fast_rate),
      slow_speed: String(sb.slow_speed), slow_rate: String(sb.slow_rate),
      min_turn_angle: String(sb.min_turn_angle), turn_slope: String(sb.turn_slope),
      min_turn_time: String(sb.min_turn_time),
    };
    const c = await api.get('/cot-settings');
    if (c) cot = {
      type: c.type, send_path: c.send_path, channel: c.channel,
      destination: c.destination, path: c.path,
      num_transmits: String(c.num_transmits),
      second_tx_delay_seconds: String(c.second_tx_delay_seconds),
      decay_factor: String(c.decay_factor),
    };
  });

  async function saveSmartBeacon(e) {
    e.preventDefault();
    savingSB = true;
    try {
      await api.put('/smart-beacon', {
        enabled: smartBeacon.enabled,
        fast_speed: parseInt(smartBeacon.fast_speed),
        fast_rate: parseInt(smartBeacon.fast_rate),
        slow_speed: parseInt(smartBeacon.slow_speed),
        slow_rate: parseInt(smartBeacon.slow_rate),
        min_turn_angle: parseInt(smartBeacon.min_turn_angle),
        turn_slope: parseInt(smartBeacon.turn_slope),
        min_turn_time: parseInt(smartBeacon.min_turn_time),
      });
      toasts.success('SmartBeaconing saved');
    } catch (err) {
      toasts.error(err.message);
    } finally {
      savingSB = false;
    }
  }

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

<PageHeader title="Beacon Settings" subtitle="SmartBeaconing and Cursor-on-Target tuning" />

<div class="tabs">
  <button class="tab" class:active={activeTab === 'smart-beaconing'} onclick={() => activeTab = 'smart-beaconing'}>Smart Beaconing</button>
  <button class="tab" class:active={activeTab === 'cot'} onclick={() => activeTab = 'cot'}>Cursor-on-Target Settings</button>
</div>

<div class="tab-panel" class:hidden={activeTab !== 'smart-beaconing'}>
  <Box title="SmartBeaconing">
    <p class="sb-intro">
      SmartBeaconing adjusts your beacon rate based on how you're moving.
      When you're driving fast or turning, it beacons more often so trackers can follow your path accurately.
      When you're slow or stopped, it beacons less often to avoid cluttering the frequency.
      The settings below control how aggressively it adapts.
    </p>
    <form onsubmit={saveSmartBeacon}>
      <Toggle bind:checked={smartBeacon.enabled} label="Enable SmartBeaconing" />
      <h4 class="sb-section-label">Speed-based beaconing</h4>
      <p class="sb-section-desc">
        These control how often you beacon based on your speed.
        At or above Fast Speed, you beacon at the Fast Rate.
        At or below Slow Speed, you beacon at the Slow Rate.
        In between, the rate scales proportionally.
      </p>
      <div class="sb-grid">
        <FormField label="Fast Speed (mph)" id="sb-fspd"
          hint="Above this speed, you beacon at the fast rate. Typical: 60 mph for highway driving.">
          <Input id="sb-fspd" bind:value={smartBeacon.fast_speed} type="number" />
        </FormField>
        <FormField label="Fast Rate (s)" id="sb-frate"
          hint="Seconds between beacons at high speed. Lower = more frequent. 60s is common for active tracking.">
          <Input id="sb-frate" bind:value={smartBeacon.fast_rate} type="number" />
        </FormField>
        <FormField label="Slow Speed (mph)" id="sb-sspd"
          hint="Below this speed, you're considered nearly stopped and beacon at the slow rate. Typical: 5 mph.">
          <Input id="sb-sspd" bind:value={smartBeacon.slow_speed} type="number" />
        </FormField>
        <FormField label="Slow Rate (s)" id="sb-srate"
          hint="Seconds between beacons when slow or stopped. 1800s (30 min) is typical to avoid unnecessary transmissions.">
          <Input id="sb-srate" bind:value={smartBeacon.slow_rate} type="number" />
        </FormField>
      </div>
      <h4 class="sb-section-label">Turn-based beaconing</h4>
      <p class="sb-section-desc">
        These trigger an extra beacon when you make a turn, so your tracked path shows corners accurately.
        A beacon fires when your heading change exceeds a threshold calculated as:
        Min Turn Angle + (Turn Slope &div; your speed).
        This means sharper turns are needed at higher speeds, and gentle curves trigger beacons at low speeds.
      </p>
      <div class="sb-grid">
        <FormField label="Min Turn Angle (°)" id="sb-angle"
          hint="The fixed part of the turn threshold. At very high speeds, you must turn at least this many degrees to trigger a beacon. Typical: 28°.">
          <Input id="sb-angle" bind:value={smartBeacon.min_turn_angle} type="number" />
        </FormField>
        <FormField label="Turn Slope" id="sb-slope"
          hint="Controls how sensitive turns are at lower speeds. Higher values make slow-speed turns trigger beacons more easily. Typical: 26.">
          <Input id="sb-slope" bind:value={smartBeacon.turn_slope} type="number" />
        </FormField>
        <FormField label="Min Turn Time (s)" id="sb-ttime"
          hint="Minimum seconds between turn-triggered beacons. Prevents excessive beaconing during winding roads. Typical: 30s.">
          <Input id="sb-ttime" bind:value={smartBeacon.min_turn_time} type="number" />
        </FormField>
      </div>
      <div class="form-actions">
        <Button variant="primary" type="submit" disabled={savingSB}>Save SmartBeaconing</Button>
      </div>
    </form>
  </Box>
</div>

<div class="tab-panel" class:hidden={activeTab !== 'cot'}>
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
</div>

<style>
  /* Tab bar — same look as Igate.svelte's Connection/Filters tabs; no
     shared Tabs component exists yet, so this is duplicated per-page. */
  .tabs {
    display: flex;
    gap: 0;
    margin-bottom: 16px;
    border-bottom: 1px solid var(--border-color);
  }
  .tab {
    padding: 8px 20px;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--text-secondary);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;
  }
  .tab:hover {
    color: var(--text-primary);
  }
  .tab.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }
  .tab-panel.hidden { display: none; }

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
