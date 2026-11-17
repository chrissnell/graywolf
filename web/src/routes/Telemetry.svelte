<script>
  import { onMount } from 'svelte';
  import { Button, Toggle, Box, Input, Select } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import { telemetryRows, telemetryState, telemetryDeliveryError } from '../lib/telemetry.js';

  let config = $state({enabled:false, callsign:'', channel:0, delivery:'aprsis', rf_path:'', allow_above_255:false, metadata_is_seconds:7200, metadata_rf_seconds:21600});
  let channels = $state([]);
  let status = $state(null);
  let saving = $state(false);
  let loaded = $state(false);
  let error = $state('');
  let saveError = $state('');
  const rows = $derived(telemetryRows(status));
  const channelOptions = $derived([{value:'0', label:'Select RF delivery channel'}, ...channels.filter(c => c.mode !== 'packet').map(c => ({value:String(c.id), label:c.name || `Channel ${c.id}`}))]);
  let channel = $state('0');

  onMount(() => {
    let disposed = false;
    let polling = false;
    async function refresh() {
      if (polling || disposed) return;
      polling = true;
      try {
        const result = await api.get('/telemetry/status');
        if (!disposed) { status = result; error = ''; }
      } catch (err) { if (!disposed) { status = null; error = err.message; } }
      finally { polling = false; }
    }
    (async () => {
      try {
        const [c, list] = await Promise.all([api.get('/telemetry/config'), api.get('/channels')]);
        if (disposed) return;
        if (!c) throw new Error('Telemetry configuration unavailable');
        config = c; channel = String(c.channel || 0); channels = list || []; loaded = true;
        await refresh();
      } catch (err) { if (!disposed) error = err.message; }
    })();
    const timer = setInterval(refresh, 10000);
    return () => { disposed = true; clearInterval(timer); };
  });
  async function save(event) {
    event.preventDefault();
    saveError = telemetryDeliveryError(config.delivery, channel);
    if (saveError) { toasts.error(saveError); return; }
    saving = true;
    try {
      config = await api.put('/telemetry/config', {...config, channel:Number(channel)});
      status = await api.get('/telemetry/status');
      toasts.success('Telemetry settings saved; initial collection requires ten minutes');
    } catch (err) { toasts.error(err.message); }
    finally { saving = false; }
  }
  async function report() {
    saving = true;
    try { status = await api.post('/telemetry/report', {}); toasts.success('Telemetry submitted locally; this does not confirm RF reception'); }
    catch (err) { toasts.error(err.message); }
    finally { saving = false; }
  }
  function formatTime(value) { return !value || value.startsWith('0001-') ? '—' : new Date(value).toLocaleString(); }
</script>

<PageHeader title="Telemetry" subtitle="Station-wide ten-minute T# activity reports" />

<div class="info-box">
  <p><strong>Measurement scope: entire station — all channels.</strong> The RF delivery channel selects where reports are transmitted, not which activity is counted. Per-channel reporting and digital status telemetry are possible future work.</p>
  <p>Initial collection begins when counters are available and at least one enabled native RF input or KISS-TNC connection is ready. No first report is sent until ten minutes of continuous counter observations have been collected. Definitions follow the first report, spaced 30 seconds apart.</p>
</div>

{#if error}<p role="alert" class="error">{error}</p>{/if}
<Box>
  <form onsubmit={save}>
    {#if saveError}<p role="alert" class="error">{saveError}</p>{/if}
    <Toggle bind:checked={config.enabled} label="Enable T# activity telemetry" />
    <p><strong>Reporting period:</strong> 10 minutes. Each report measures the preceding ten minutes.</p>
    <FormField label="Telemetry callsign/SSID (optional)" id="telemetry-callsign" hint="Leave blank to inherit and follow the station identity. Use only an identity you are authorized to transmit.">
      <Input id="telemetry-callsign" bind:value={config.callsign} placeholder="Inherit station identity" />
    </FormField>
    <FormField label="Delivery" id="telemetry-delivery">
      <Select id="telemetry-delivery" bind:value={config.delivery} options={[{value:'aprsis',label:'APRS-IS only (recommended)'}, {value:'auto',label:'APRS-IS preferred; RF when disconnected'}, {value:'rf',label:'RF only'}]} />
    </FormField>
    <p>APRS-IS only is the default and recommended for Internet-connected stations; it avoids extra RF airtime. Existing saved delivery choices are preserved.</p>
    <FormField label="RF delivery channel" id="telemetry-channel" hint="Required to save RF or automatic delivery, even while telemetry is disabled. Counters always cover the entire station.">
      <Select id="telemetry-channel" bind:value={channel} options={channelOptions} />
    </FormField>
    <FormField label="RF digipeater path" id="telemetry-path" hint="Blank sends direct. This path is separate from position beacons.">
      <Input id="telemetry-path" bind:value={config.rf_path} placeholder="Direct RF" />
    </FormField>
    <details>
      <summary>Advanced</summary>
      <Toggle bind:checked={config.allow_above_255} label="Allow counts above 255" />
      <p>Off: transmitted values saturate at 255, with a clipping warning below. On: exact larger counts are sent using extended T# telemetry. Supported by aprs.fi; strict older receivers and clients may reject these reports. Local counts stay exact in either mode.</p>
      <FormField label="APRS-IS definition refresh (seconds)" id="telemetry-is-refresh">
        <Input id="telemetry-is-refresh" type="number" min="3600" max="604800" bind:value={config.metadata_is_seconds} />
      </FormField>
      <FormField label="RF definition refresh (seconds)" id="telemetry-rf-refresh">
        <Input id="telemetry-rf-refresh" type="number" min="3600" max="604800" bind:value={config.metadata_rf_seconds} />
      </FormField>
    </details>
    <div class="form-actions"><Button type="submit" variant="primary" disabled={!loaded || saving}>{saving ? 'Working...' : 'Save'}</Button></div>
    <p>Saving does not send immediately. Enabling starts collection; changing settings restarts the initial baseline. APRS-IS-only never falls back to RF. An uncertain submission is not immediately retried on another route.</p>
  </form>
</Box>

<Box>
  <h2>Current activity</h2>
  <p role="status">{telemetryState(status)}</p>
  <dl>
    <dt>Reporting identity</dt><dd>{status?.effective_callsign || '—'}</dd>
    <dt>Digipeater counter</dt><dd>{status?.digi_counter_source === 'forwarded' ? 'Forwarded — backend acceptances' : status?.digi_counter_source === 'legacy' ? 'Legacy — processed submissions, including TX duplicates' : 'Unavailable'}</dd>
    <dt>Any RF input ready</dt><dd>{status ? (status.input_ready ? 'Yes' : 'No') : 'Unknown'}</dd>
    <dt>Delivery route</dt><dd>{status?.route || 'Waiting'}</dd>
    <dt>Last report submission</dt><dd>{formatTime(status?.last_submission)}</dd>
    <dt>Next report</dt><dd>{formatTime(status?.next_report)}</dd>
  </dl>
  {#if status?.last_error}<p role="alert" class="error">{status.last_error}</p>{/if}
  {#if status?.clipped}<p role="alert"><strong>Clipped:</strong> one or more counts exceed 255. Local exact counts are shown below.</p>{/if}
  <table><thead><tr><th>Counter</th><th>Packets / 10 minutes</th><th>Encoded value</th></tr></thead>
    <tbody>{#each rows as row}<tr><td>{row.label}</td><td>{row.count}</td><td>{row.encoded}</td></tr>{/each}</tbody>
  </table>
  <h3>Report preview</h3><pre>{status?.preview || 'Available after a complete baseline'}</pre>
  <Button onclick={report} disabled={!status?.ready || saving}>Send report now</Button>
  <p>This button submits a real packet using the saved settings. A valid baseline and delivery route are required; manual reports are limited to one per 30 seconds.</p>
</Box>
<Box>
  <h2>Recent report submissions</h2>
  <p>The last ten locally submitted data reports, newest first. This history is held in RAM and clears when Graywolf restarts. An RF entry means queued locally, not confirmed reception.</p>
  {#if status?.recent?.length}
    <div class="history-scroll"><table>
      <thead><tr><th>Submitted</th><th>Identity</th><th>Route</th><th>RF Rx</th><th>Digi</th><th>RF Tx</th><th>RF → IS</th><th>IS → RF</th></tr></thead>
      <tbody>{#each status.recent as entry}
        <tr><td>{formatTime(entry.submitted_at)}</td><td>{entry.identity}</td><td>{entry.route}</td>
          {#each entry.counts as count}<td>{count}</td>{/each}
        </tr>
        <tr><td colspan="8"><code>{entry.payload}</code>{#if entry.clipped} — transmitted counts clipped at 255{/if}</td></tr>
      {/each}</tbody>
    </table></div>
  {:else}<p>No reports have been submitted in this process session.</p>{/if}
</Box>
<div class="info-box">
  <p>Disconnected inputs contribute no received packets; telemetry does not prove continuous receiver availability. The same packet received on two channels counts as two reception events. RF Tx includes telemetry transmissions and counts local dispatches. Digi prefers backend-forwarded frames when supported, otherwise legacy submissions. IS → RF counts requests, including suppressed duplicates. None of these counts confirms RF radiation or remote reception.</p>
  <p>Definitions are addressed to the telemetry identity. Receivers may initially show generic labels until definitions arrive. No active digital flags are reported; the unused digital field is 00000000.</p>
</div>
<style>
  .info-box { margin-block: 1rem; padding: 1rem; border: 1px solid var(--border-color, #888); border-radius: 4px; }
  p { margin-block: .65rem; }
  details { margin-block: 1rem; }
  summary { cursor: pointer; margin-bottom: .75rem; }
  .form-actions { margin-block: 1rem; }
  dl { display: grid; grid-template-columns: minmax(10rem, 1fr) 2fr; gap: .4rem 1rem; }
  dd { margin: 0; overflow-wrap: anywhere; }
  table { width: 100%; border-collapse: collapse; margin-block: 1rem; }
  th, td { text-align: left; padding: .5rem; border-bottom: 1px solid var(--border-color, #888); }
  .history-scroll { overflow-x: auto; }
  pre { white-space: pre-wrap; overflow-wrap: anywhere; }
  .error { color: var(--color-danger); }
  @media (max-width: 600px) { dl { grid-template-columns: 1fr; } dd { margin-bottom: .5rem; } }
</style>
