<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';

  // Moved off the Beacons page (which can run long once an operator has
  // many beacons configured) so SmartBeaconing tuning is always one
  // screen, not a scroll away. See docs/wiki/code-map.md route table.
  let activeTab = $state('smart-beaconing');

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
</script>

<PageHeader title="Beacon Settings" subtitle="SmartBeaconing and ping tuning" />

<div class="tabs">
  <button class="tab" class:active={activeTab === 'smart-beaconing'} onclick={() => activeTab = 'smart-beaconing'}>Smart Beaconing</button>
  <button class="tab" class:active={activeTab === 'ping'} onclick={() => activeTab = 'ping'}>Ping Settings</button>
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

<div class="tab-panel" class:hidden={activeTab !== 'ping'}>
  <Box title="Ping Settings">
    <p class="placeholder-text">Feature coming soon.</p>
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

  .placeholder-text {
    color: var(--color-text-muted, #888);
    font-size: 14px;
    margin: 0;
  }
</style>
