<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle, Box, Radio, RadioGroup, Badge, Checkbox, AlertDialog } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import { unitsState } from '../lib/settings/units-store.svelte.js';
  import { FT_PER_M } from '../lib/settings/units.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';
  import SymbolPicker from '../components/SymbolPicker.svelte';
  import ChannelListbox from '../lib/components/ChannelListbox.svelte';
  import { channelsStore, start as startChannelsStore, invalidate as refreshChannels } from '../lib/stores/channels.svelte.js';
  import { getChannel as lookupChannel } from '../lib/stores/channels.svelte.js';
  import { unboundWarning, SUMMARY_UNBOUND } from '../lib/channelBacking.js';
  import {
    PRIMARY_TABLE, ALTERNATE_TABLE, SPRITE_URLS, CELL_PX,
    backgroundPosition, loadSymbols, describe,
  } from '../lib/aprsSymbols.js';

  // Altitude input: local unit selector + text value, converting to/from form.alt_ft
  let altUnit = $state(unitsState.isMetric ? 'meters' : 'feet');
  let altInput = $state('');
  let altError = $state('');

  function altInputFromFeet(ft) {
    if (ft == null || ft === '') return '';
    const n = parseFloat(ft);
    if (Number.isNaN(n)) return '';
    if (altUnit === 'meters') return String(Math.round(n / FT_PER_M * 100) / 100);
    return String(n);
  }

  function toggleAltUnit(unit) {
    if (unit === altUnit) return;
    // Convert current input value to the new unit
    const raw = altInput.replace(',', '.').trim();
    if (raw !== '' && !Number.isNaN(parseFloat(raw))) {
      const v = parseFloat(raw);
      if (altUnit === 'feet' && unit === 'meters') {
        altInput = String(Math.round(v / FT_PER_M * 100) / 100);
      } else {
        altInput = String(Math.round(v * FT_PER_M * 100) / 100);
      }
    }
    altUnit = unit;
    altError = '';
  }

  // Station callsign loaded once on mount. When empty/unset, the
  // placeholder reads "(not set)" and the list renders inherited rows
  // with the muted fallback — operators can still author beacons; the
  // backend runtime guard (D6) is what ultimately refuses to transmit.
  let stationCallsign = $state('');

  let beacons = $state([]);
  // Channels come from the shared channelsStore (D9) so every picker
  // page sees coherent backing state. Legacy local `channels` array is
  // retained only as a $derived view on top of the store so
  // channelName() and the modal channel-defaulting code paths keep
  // working without changes.
  let channels = $derived(channelsStore.list);
  let smartBeacon = $state({
    enabled: false, fast_speed: '60', fast_rate: '60', slow_speed: '5', slow_rate: '1800',
    min_turn_angle: '28', turn_slope: '26', min_turn_time: '30',
  });
  // The server expands {{version}} through text/template at beacon
  // send time, so storing the literal template lets the comment track
  // upgrades without edits.
  const defaultComment = 'Graywolf/{{version}}';
  let modalOpen = $state(false);
  let editing = $state(null);
  let deleteTarget = $state(null);
  let deleteOpen = $state(false);
  // `callsign_override` drives the D3 compact checkbox pattern. It's
  // UI-only state: on save it gates whether `callsign` is sent as the
  // trimmed/uppercased override or as the empty "inherit" sentinel.
  // Keeping it on `form` (rather than as a separate `$state`) means
  // openEdit's Object.assign still covers the full form snapshot and
  // we don't need a parallel lifecycle for the checkbox.
  let form = $state({
    channel: '', callsign: '', callsign_override: false,
    destination: 'APGRWO', path: 'WIDE1-1,WIDE2-1',
    symbol_table: '/', symbol: '-', overlay: '',
    pos_source: 'gps', latitude: '', longitude: '', alt_ft: '',
    comment: '', interval: '600', send_to_aprs_is: false, enabled: true,
  });

  let callsignError = $state('');
  // Placeholder shown in the disabled input when "override" is unchecked.
  // Mirrors the D3 copy: "Uses station callsign (KE7XYZ-9)" when loaded,
  // "Uses station callsign (not set)" when StationConfig is empty.
  let inheritedPlaceholder = $derived(
    stationCallsign
      ? `Uses station callsign (${stationCallsign})`
      : 'Uses station callsign (not set)'
  );

  // Unbound-channel warning (D17). Surfaces above the submit button
  // when the selected channel has no backend attached so the
  // operator knows the beacon will be accepted but silent.
  let selectedBackingSummary = $derived.by(() => {
    const n = parseInt(form.channel, 10);
    const c = lookupChannel(n);
    return c?.backing?.summary;
  });
  let selectedChannelObj = $derived.by(() => {
    const n = parseInt(form.channel, 10);
    return lookupChannel(n);
  });
  let savingSB = $state(false);
  let pickerOpen = $state(false);
  let symbolMeta = $state(null);
  loadSymbols().then((m) => symbolMeta = m);

  function channelName(id) {
    const c = channels.find(c => c.id === id);
    return c ? c.name : `Channel #${id}`;
  }

  function formatInterval(seconds) {
    if (!seconds) return '—';
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) {
      const m = Math.floor(seconds / 60);
      const s = seconds % 60;
      return s === 0 ? `${m}m` : `${m}m ${s}s`;
    }
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    return m === 0 ? `${h}h` : `${h}h ${m}m`;
  }

  function formatCoords(row) {
    if (row.use_gps) return 'Live GPS fix';
    if (row.latitude === 0 && row.longitude === 0) return '—';
    return `${row.latitude.toFixed(4)}, ${row.longitude.toFixed(4)}`;
  }

  // Kick the shared channels store (idempotent — safe if another
  // page already started it). The store's poller + focus listener
  // now owns channel freshness; this page just subscribes.
  startChannelsStore();

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
    // Station callsign drives the inherited-placeholder and the list's
    // "inherited" rendering. Failure is non-fatal — the page stays
    // usable, beacons are still authorable, and the placeholder just
    // reads "(not set)".
    try {
      const st = await api.get('/station/config');
      stationCallsign = (st && st.callsign) || '';
    } catch {
      stationCallsign = '';
    }
  });

  function openCreate() {
    if (channels.length === 0) {
      toasts.error('Create a channel first on the Channels page');
      return;
    }
    editing = null;
    form.channel = String(channels[0].id);
    form.callsign = '';
    form.callsign_override = false;
    callsignError = '';
    form.destination = 'APGRWO';
    form.path = 'WIDE1-1,WIDE2-1';
    form.symbol_table = '/';
    form.symbol = '-';
    form.overlay = '';
    form.pos_source = 'gps';
    form.latitude = '';
    form.longitude = '';
    form.alt_ft = '';
    altInput = '';
    altError = '';
    form.comment = defaultComment;
    form.interval = '600';
    form.send_to_aprs_is = false;
    form.enabled = true;
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    // `row.callsign` from the API is always a plain string. Empty
    // string means "inherits from StationConfig" (phase 2 migration
    // normalized matching-call rows down to empty; all existing
    // overrides are non-empty). This is the single source of truth for
    // the checkbox's initial state.
    const rowCall = row.callsign || '';
    // Mutate form in place (rather than reassigning) so nested bind:value
    // on the RadioGroup picks up the new value reliably.
    Object.assign(form, row, {
      channel: String(row.channel),
      callsign: rowCall,
      callsign_override: rowCall !== '',
      symbol_table: row.symbol_table || '/',
      symbol: row.symbol || '-',
      overlay: row.overlay || '',
      pos_source: row.use_gps ? 'gps' : 'fixed',
      latitude: row.latitude != null ? String(row.latitude) : '',
      longitude: row.longitude != null ? String(row.longitude) : '',
      alt_ft: row.alt_ft != null ? String(row.alt_ft) : '',
      interval: String(row.interval),
    });
    altInput = altInputFromFeet(form.alt_ft);
    altError = '';
    callsignError = '';
    modalOpen = true;
  }

  async function handleSave() {
    // D3 override semantics. Unchecked → always send empty string so
    // the backend treats it as "inherit from StationConfig". Checked →
    // require a non-empty value (can't override with nothing) and send
    // the trimmed/uppercased value. Never send `undefined` — the form
    // always makes the operator's intent explicit on the wire.
    let callsignToSend;
    if (form.callsign_override) {
      const trimmed = form.callsign.trim();
      if (!trimmed) {
        callsignError = 'Enter an override callsign or uncheck the box';
        toasts.error('Enter an override callsign or uncheck the box');
        return;
      }
      callsignToSend = trimmed.toUpperCase();
    } else {
      callsignToSend = '';
    }
    callsignError = '';
    const channelId = parseInt(form.channel);
    if (!Number.isFinite(channelId) || channelId <= 0) {
      toasts.error('Channel required');
      return;
    }
    const useGps = form.pos_source === 'gps';
    const latStr = form.latitude.trim();
    const lonStr = form.longitude.trim();
    const lat = latStr === '' ? 0 : parseFloat(latStr);
    const lon = lonStr === '' ? 0 : parseFloat(lonStr);
    // Convert altitude input to feet for the API
    const altNorm = altInput.replace(',', '.').trim();
    let altFt = null;
    if (altNorm !== '') {
      const altVal = parseFloat(altNorm);
      if (Number.isNaN(altVal)) {
        altError = 'Altitude must be a number';
        toasts.error('Altitude must be numeric');
        return;
      }
      altFt = altUnit === 'meters' ? altVal * FT_PER_M : altVal;
    }
    if (Number.isNaN(lat) || Number.isNaN(lon)) {
      toasts.error('Latitude and longitude must be numeric');
      return;
    }
    if (!useGps && lat === 0 && lon === 0) {
      toasts.error('Latitude/longitude required when not using GPS');
      return;
    }
    const data = {
      ...form,
      callsign: callsignToSend,
      channel: channelId,
      use_gps: useGps,
      interval: parseInt(form.interval),
      latitude: lat,
      longitude: lon,
      alt_ft: altFt,
    };
    delete data.pos_source;
    delete data.callsign_override;
    delete data.id;
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
      refreshChannels();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  function confirmDelete(row) {
    deleteTarget = row;
    deleteOpen = true;
  }

  async function executeDelete() {
    if (!deleteTarget) return;
    try {
      await api.delete(`/beacons/${deleteTarget.id}`);
      toasts.success('Beacon deleted');
      beacons = await api.get('/beacons') || [];
    } catch (err) {
      toasts.error(err.message);
    } finally {
      deleteOpen = false;
      deleteTarget = null;
    }
  }

  async function handleSendNow(row) {
    try {
      await api.post(`/beacons/${row.id}/send`, {});
      toasts.success(`Beacon sent: ${row.callsign || stationCallsign || '(unset)'}`);
    } catch (err) {
      toasts.error(err.message);
    }
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

{#if beacons.length === 0}
  <div class="empty-state">No beacons configured. Add a beacon to start transmitting position reports.</div>
{:else}
  <div class="beacon-grid">
    {#each beacons as b}
      <div class="beacon-card">
        <div class="beacon-header">
          <div class="beacon-identity">
            <span
              class="symbol-swatch"
              style="background-image: url({SPRITE_URLS[b.symbol_table] || SPRITE_URLS[PRIMARY_TABLE]}); background-position: {backgroundPosition(b.symbol || '-', CELL_PX)};"
              aria-hidden="true"
            >
              {#if b.overlay && b.symbol_table === ALTERNATE_TABLE}
                <span class="symbol-swatch-overlay">{b.overlay}</span>
              {/if}
            </span>
            {#if b.callsign}
              <span class="beacon-callsign">{b.callsign}</span>
            {:else if stationCallsign}
              <span class="beacon-callsign">{stationCallsign}</span>
              <span class="beacon-callsign-inherited">(inherited)</span>
            {:else}
              <span class="beacon-callsign beacon-callsign-unset">(not set)</span>
            {/if}
          </div>
          <div class="beacon-badges">
            <Badge variant={b.enabled ? 'success' : 'default'}>{b.enabled ? 'Enabled' : 'Disabled'}</Badge>
            {#if b.send_to_aprs_is}
              <Badge variant="info">APRS-IS</Badge>
            {/if}
          </div>
        </div>

        <div class="beacon-channel">
          <span class="channel-label">Channel</span>
          <span class="channel-value">{channelName(b.channel)}</span>
        </div>

        <div class="beacon-details">
          <div class="detail-row">
            <span class="detail-label">Destination</span>
            <span class="detail-value">{b.destination}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Path</span>
            <span class="detail-value">{b.path || '—'}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Position</span>
            <span class="detail-value">{formatCoords(b)}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Interval</span>
            <span class="detail-value">{formatInterval(b.interval)}</span>
          </div>
          {#if b.comment}
            <div class="detail-row">
              <span class="detail-label">Comment</span>
              <span class="detail-value detail-comment">{b.comment}</span>
            </div>
          {/if}
        </div>

        <div class="beacon-actions">
          <Button variant="ghost" onclick={() => handleSendNow(b)}>Beacon Now</Button>
          <Button variant="ghost" onclick={() => openEdit(b)}>Edit</Button>
          <Button variant="danger" onclick={() => confirmDelete(b)}>Delete</Button>
        </div>
      </div>
    {/each}
  </div>
{/if}

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

<Modal bind:open={modalOpen} title={editing ? 'Edit Beacon' : 'New Beacon'} class="beacon-modal">
  <div class="beacon-form-grid">
    <div class="beacon-form-col">
      <div style="margin-bottom: 12px;">
        <Toggle bind:checked={form.enabled} label="Enabled" />
      </div>
      <FormField label="Channel" id="bcn-channel"
        hint="Radio channel this beacon transmits on. Defined on the Channels page.">
        <ChannelListbox
          id="bcn-channel"
          bind:value={form.channel}
          valueType="string"
          channels={channels}
        />
      </FormField>
      <FormField label="Callsign" id="bcn-call" error={callsignError}>
        <div class="callsign-row">
          <Input
            id="bcn-call"
            bind:value={form.callsign}
            placeholder={form.callsign_override ? 'N0CALL-9' : inheritedPlaceholder}
            disabled={!form.callsign_override}
            class="callsign-input"
          />
          <label class="callsign-override-label" for="bcn-call-override">
            <Checkbox id="bcn-call-override" bind:checked={form.callsign_override} />
            <span>Override station callsign</span>
          </label>
        </div>
      </FormField>
      <FormField label="Destination" id="bcn-dest"
        hint="APRS tocall identifying the originating software. Leave as APGRWO unless you know you need to change it.">
        <Input id="bcn-dest" bind:value={form.destination} placeholder="APGRWO" />
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
      <FormField label="Comment" id="bcn-comment"
        hint={"Tip: use {{version}} to insert the running graywolf version."}>
        <Input id="bcn-comment" bind:value={form.comment} placeholder={defaultComment} />
      </FormField>
    </div>

    <div class="beacon-form-col">
      <FormField label="Position source" id="bcn-pos-source"
        hint="Choose whether this beacon's coordinates come from the live GPS fix or from fixed values you enter below.">
        <RadioGroup bind:value={form.pos_source}>
          <div class="pos-source-row">
            <Radio value="gps" label="Use latest fix from GPS" />
            <Radio value="fixed" label="Use fixed coordinates" />
          </div>
        </RadioGroup>
      </FormField>
      {#if form.pos_source === 'fixed'}
        <FormField label="Latitude" id="bcn-lat"
          hint="Decimal degrees, north positive (e.g. 37.5 for Half Moon Bay; -33.86 for Sydney).">
          <Input id="bcn-lat" bind:value={form.latitude} placeholder="37.5" />
        </FormField>
        <FormField label="Longitude" id="bcn-lon"
          hint="Decimal degrees, east positive (e.g. -122.4 for San Francisco; 151.2 for Sydney).">
          <Input id="bcn-lon" bind:value={form.longitude} placeholder="-122.4" />
        </FormField>
        <FormField label="Altitude" id="bcn-alt"
          hint="Antenna height above sea level in {altUnit}. Optional; leave blank or 0 to omit.">
          <div class="alt-row">
            <Input id="bcn-alt" bind:value={altInput} placeholder={altUnit === 'feet' ? '0 ft' : '0 m'}
              type="text" inputmode="decimal" error={altError} oninput={() => altError = ''} />
            <div class="unit-toggle" role="group" aria-label="Altitude unit">
              <button type="button" class="unit-btn" class:unit-active={altUnit === 'feet'}
                onclick={() => toggleAltUnit('feet')}>ft</button>
              <button type="button" class="unit-btn" class:unit-active={altUnit === 'meters'}
                onclick={() => toggleAltUnit('meters')}>m</button>
            </div>
          </div>
        </FormField>
      {/if}
      <FormField label="Interval (seconds)" id="bcn-interval">
        <Input id="bcn-interval" bind:value={form.interval} type="number" placeholder="600" />
      </FormField>
      <Toggle bind:checked={form.send_to_aprs_is} label="Also send to APRS-IS" />
    </div>
  </div>
  {#if selectedBackingSummary === SUMMARY_UNBOUND}
    <div class="unbound-warning" role="note">
      {unboundWarning(selectedChannelObj)}
    </div>
  {/if}
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

<!-- Delete confirmation -->
<AlertDialog bind:open={deleteOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete Beacon</AlertDialog.Title>
    <AlertDialog.Description>
      Are you sure you want to delete the beacon for "{deleteTarget?.callsign || stationCallsign || '(unset)'}"? This cannot be undone.
    </AlertDialog.Description>
    <div class="modal-footer">
      <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action class="danger-action" onclick={executeDelete}>Delete</AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<style>
  .empty-state {
    text-align: center;
    color: var(--text-muted);
    padding: 32px;
    border: 1px dashed var(--border-color);
    border-radius: var(--radius);
  }

  .beacon-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: 12px;
  }

  .beacon-card {
    display: flex;
    flex-direction: column;
    padding: 16px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
  }

  .beacon-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
    gap: 8px;
  }
  .beacon-identity {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .beacon-callsign {
    font-weight: 600;
    font-size: 15px;
    font-family: var(--font-mono);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .beacon-badges {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }

  .beacon-channel {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 12px;
    padding: 10px;
    background: var(--bg-tertiary);
    border-radius: var(--radius);
  }
  .channel-label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    color: var(--color-info);
    background: var(--color-info-muted);
    padding: 2px 6px;
    border-radius: 3px;
    flex-shrink: 0;
  }
  .channel-value {
    font-size: 13px;
    color: var(--text-primary);
    font-weight: 500;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .beacon-details {
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
  .detail-comment {
    font-family: inherit;
  }

  .beacon-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }

  .modal-footer {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    padding: 1.25rem 1.5rem 1.5rem;
  }
  :global(.danger-action) {
    background: var(--color-danger) !important;
    color: white !important;
  }

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

  /* Wider modal for the two-column beacon form. */
  :global(.modal.beacon-modal) {
    width: min(820px, 92vw);
  }
  .beacon-form-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0 24px;
  }
  @media (max-width: 640px) {
    .beacon-form-grid { grid-template-columns: 1fr; }
  }
  .beacon-form-col {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .symbol-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .pos-source-row {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .symbol-swatch {
    flex: 0 0 auto;
    width: 24px;
    height: 24px;
    background-repeat: no-repeat;
    background-color: #fff;
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
    line-height: 1;
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

  .alt-row {
    display: flex;
    align-items: flex-start;
    gap: 6px;
  }
  .alt-row :global(.input-wrapper) {
    flex: 1 1 auto;
    min-width: 0;
  }
  .unit-toggle {
    display: flex;
    flex-shrink: 0;
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    overflow: hidden;
  }
  .unit-btn {
    padding: 6px 10px;
    font-size: 13px;
    font-weight: 500;
    line-height: 1;
    border: none;
    cursor: pointer;
    background: var(--bg-secondary);
    color: var(--text-secondary);
    transition: background 0.15s, color 0.15s;
  }
  .unit-btn:not(:last-child) {
    border-right: 1px solid var(--border-color);
  }
  .unit-active {
    background: var(--color-primary, #3b82f6);
    color: #fff;
  }

  /* D3 compact override pattern: input + checkbox on one row.
     The input's wrapper stretches to fill remaining space so the
     checkbox sits flush right. Typed text appears uppercase via
     text-transform while the underlying value is normalized on save. */
  .callsign-row {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .callsign-row :global(.input-wrapper) {
    flex: 1 1 200px;
    min-width: 0;
  }
  .callsign-row :global(.callsign-input) {
    text-transform: uppercase;
  }
  .callsign-override-label {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    color: var(--text-secondary, var(--color-text-muted, #888));
    white-space: nowrap;
    cursor: pointer;
    user-select: none;
  }

  .beacon-callsign-inherited {
    margin-left: 6px;
    font-size: 12px;
    font-style: italic;
    color: var(--text-muted, var(--color-text-muted, #888));
  }
  .beacon-callsign-unset {
    font-style: italic;
    color: var(--text-muted, var(--color-text-muted, #888));
  }

  /* D17 — unbound-channel save warning. Non-blocking; matches the
     amber-bordered callout used elsewhere in the app so it reads as
     a caution, not an error. */
  .unbound-warning {
    margin: 12px 0 0 0;
    padding: 10px 12px;
    border: 1px solid var(--color-warning, #d4a72c);
    border-left-width: 4px;
    border-radius: 4px;
    background: var(--color-warning-bg, rgba(212, 167, 44, 0.12));
    color: var(--text-primary, inherit);
    font-size: 13px;
    line-height: 1.45;
  }
</style>
