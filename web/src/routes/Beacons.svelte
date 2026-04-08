<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';
  import SymbolPicker from '../components/SymbolPicker.svelte';
  import {
    PRIMARY_TABLE, ALTERNATE_TABLE, SPRITE_URLS, CELL_PX,
    backgroundPosition, loadSymbols, describe,
  } from '../lib/aprsSymbols.js';

  let beacons = $state([]);
  let smartBeacon = $state({
    enabled: false, fast_speed: '60', fast_rate: '60', slow_speed: '5', slow_rate: '1800',
    min_turn_angle: '28', turn_slope: '26', min_turn_time: '30',
  });
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state({
    callsign: '', destination: 'APGW00', path: 'WIDE1-1,WIDE2-1',
    symbol_table: '/', symbol: '-', overlay: '',
    comment: '', interval: '600', enabled: true,
  });
  let savingSB = $state(false);
  let pickerOpen = $state(false);
  let symbolMeta = $state(null);
  loadSymbols().then((m) => symbolMeta = m);

  const columns = [
    { key: 'callsign', label: 'Callsign' },
    { key: 'destination', label: 'Destination' },
    { key: 'path', label: 'Path' },
    { key: 'interval', label: 'Interval (s)' },
    { key: 'enabled', label: 'Enabled' },
  ];

  onMount(async () => {
    beacons = await api.get('/beacons') || [];
    const sb = await api.get('/smart-beacon');
    if (sb) smartBeacon = {
      enabled: sb.enabled,
      fast_speed: String(sb.fast_speed), fast_rate: String(sb.fast_rate),
      slow_speed: String(sb.slow_speed), slow_rate: String(sb.slow_rate),
      min_turn_angle: String(sb.min_turn_angle), turn_slope: String(sb.turn_slope),
      min_turn_time: String(sb.min_turn_time),
    };
  });

  function openCreate() {
    editing = null;
    form = {
      callsign: '', destination: 'APGW00', path: 'WIDE1-1,WIDE2-1',
      symbol_table: '/', symbol: '-', overlay: '',
      comment: '', interval: '600', enabled: true,
    };
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = {
      ...row,
      symbol_table: row.symbol_table || '/',
      symbol: row.symbol || '-',
      overlay: row.overlay || '',
      interval: String(row.interval),
    };
    modalOpen = true;
  }

  async function handleSave() {
    if (!form.callsign.trim()) { toasts.error('Callsign required'); return; }
    const data = { ...form, interval: parseInt(form.interval) };
    try {
      if (editing) {
        await api.put(`/beacons/${editing.id}`, data);
        toasts.success('Beacon updated');
      } else {
        await api.post('/beacons', data);
        toasts.success('Beacon created');
      }
      modalOpen = false;
      beacons = await api.get('/beacons') || [];
    } catch (err) {
      toasts.error(err.message);
    }
  }

  async function handleDelete(row) {
    if (!confirm(`Delete beacon for ${row.callsign}?`)) return;
    await api.delete(`/beacons/${row.id}`);
    toasts.success('Deleted');
    beacons = await api.get('/beacons') || [];
  }

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

<PageHeader title="Beacons" subtitle="APRS beacon configuration">
  <Button variant="primary" onclick={openCreate}>+ Add Beacon</Button>
</PageHeader>

<DataTable {columns} rows={beacons} onEdit={openEdit} onDelete={handleDelete} />

<div style="margin-top: 24px;">
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

<Modal bind:open={modalOpen} title={editing ? 'Edit Beacon' : 'New Beacon'}>
    <FormField label="Callsign" id="bcn-call">
      <Input id="bcn-call" bind:value={form.callsign} placeholder="N0CALL-9" />
    </FormField>
    <FormField label="Destination" id="bcn-dest"
      hint="APRS tocall identifying the originating software. Leave as APGW00 unless you know you need to change it.">
      <Input id="bcn-dest" bind:value={form.destination} placeholder="APGW00" />
    </FormField>
    <FormField label="Path" id="bcn-path">
      <Input id="bcn-path" bind:value={form.path} placeholder="WIDE1-1,WIDE2-1" />
    </FormField>
    <FormField label="Symbol" id="bcn-symbol"
      hint="The icon shown for this station on aprs.fi and other APRS maps.">
      <div class="symbol-row">
        <span
          class="symbol-swatch"
          style="background-image: url({SPRITE_URLS[form.symbol_table] || SPRITE_URLS[PRIMARY_TABLE]}); background-position: {backgroundPosition(form.symbol || '-', CELL_PX)};"
          aria-hidden="true"
        >
          {#if form.overlay && form.symbol_table === ALTERNATE_TABLE}
            <span class="symbol-swatch-overlay">{form.overlay}</span>
          {/if}
        </span>
        <span class="symbol-name">
          {describe(symbolMeta, form.symbol_table || '/', form.symbol || '-') || '\u2014'}
        </span>
        <Button onclick={() => pickerOpen = true}>Choose&hellip;</Button>
      </div>
    </FormField>
    <FormField label="Comment" id="bcn-comment">
      <Input id="bcn-comment" bind:value={form.comment} placeholder="graywolf" />
    </FormField>
    <FormField label="Interval (seconds)" id="bcn-interval">
      <Input id="bcn-interval" bind:value={form.interval} type="number" placeholder="600" />
    </FormField>
    <Toggle bind:checked={form.enabled} label="Enabled" />
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" onclick={handleSave}>{editing ? 'Save' : 'Create'}</Button>
    </div>
</Modal>

<SymbolPicker
  bind:open={pickerOpen}
  bind:table={form.symbol_table}
  bind:symbol={form.symbol}
  bind:overlay={form.overlay}
/>

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
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }

  .symbol-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .symbol-swatch {
    flex: 0 0 auto;
    width: 24px;
    height: 24px;
    background-repeat: no-repeat;
    background-color: var(--color-bg-elevated, #1a1a1a);
    border: 1px solid var(--color-border);
    border-radius: 3px;
    position: relative;
  }
  .symbol-swatch-overlay {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    font-family: ui-monospace, SFMono-Regular, monospace;
    font-size: 12px;
    font-weight: 700;
    color: #000;
    text-shadow: 0 0 1px #fff, 0 0 1px #fff, 0 0 1px #fff;
    pointer-events: none;
  }
  .symbol-name {
    flex: 1 1 auto;
    font-size: 13px;
    color: var(--color-text, #ddd);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
