<script>
  import { onMount, onDestroy } from 'svelte';
  import { Button, Input, Select, Badge, Checkbox } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import FormField from '../components/FormField.svelte';
  import ChannelListbox from '../lib/components/ChannelListbox.svelte';
  import { channelsStore, start as startChannelsStore, invalidate as refreshChannels } from '../lib/stores/channels.svelte.js';
  import {
    countdownText,
    stateLabel,
    stateBadgeVariant,
    canRetryNow,
    formatLocalTime,
  } from '../lib/kissCountdown.js';

  // Module-level constants for poll cadence and tcp-client defaults.
  // Kept in one place so create / edit / payload code paths can't
  // drift. Values match plan D16 and Phase 4 tcp-client defaults.
  const KISS_POLL_MS = 2000;
  const CLOCK_TICK_MS = 1000;
  const CLOCK_TICK_MOD = 1_000_000;
  const RECONNECT_INIT_DEFAULT_MS = 1000;
  const RECONNECT_MAX_DEFAULT_MS = 300000;
  const TCP_PORT_DEFAULT = '8001';
  const TNC_INGRESS_RATE_DEFAULT = '50';
  const TNC_INGRESS_BURST_DEFAULT = '100';
  const BAUD_RATE_DEFAULT = '9600';
  // Show a "stale" indicator if the 2 s poller has failed this many
  // consecutive times. At 2 s cadence, five consecutive failures ≈
  // 10 s of silence — long enough to be a real problem, short enough
  // that operators notice before the state is grossly out of date.
  const KISS_STALE_AFTER_FAILURES = 5;

  let items = $state([]);
  // Shared channelsStore subscription (D9). Direct list access —
  // ChannelListbox accepts objects with {id, name, backing}.
  let channels = $derived(channelsStore.list);
  // Consecutive /api/kiss poll failures. Reset on success.
  let pollFailures = $state(0);
  let pollIsStale = $derived(pollFailures >= KISS_STALE_AFTER_FAILURES);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state(emptyForm());

  // Delete confirmation (bound to the Delete-flavored ConfirmDialog).
  let confirmOpen = $state(false);
  let confirmMessage = $state('');
  let pendingDeleteId = $state(null);

  // Mode-change confirmation (distinct dialog so the operator isn't shown a
  // "Delete" button when the action is actually a routing flip).
  let modeChangeOpen = $state(false);
  let modeChangeMessage = $state('');

  // Row id whose status detail panel is currently expanded. Null when
  // none — clicking the status badge toggles.
  let expandedId = $state(null);

  // Clock tick (Phase 4 D16). We keep a rune-backed mutable number
  // bumped by setInterval(1000); every $derived countdown call reads
  // this so the UI re-renders each second without a separate store
  // per row.
  let clockTick = $state(0);
  let pollTimer = null;
  let clockTimer = null;

  const columns = [
    { key: 'type', label: 'Type' },
    { key: 'endpoint', label: 'Endpoint' },
    { key: 'channel', label: 'Channel' },
    { key: 'mode', label: 'Mode' },
    { key: 'status', label: 'Status' },
  ];

  const typeOptions = [
    { value: 'tcp', label: 'TCP (server)' },
    { value: 'tcp-client', label: 'TCP Client' },
    { value: 'serial', label: 'Serial' },
  ];

  const modeOptions = [
    { value: 'modem', label: 'Modem' },
    { value: 'tnc', label: 'TNC' },
  ];

  // Hint text is the primary explanation of Mode — option labels are
  // deliberately terse. Wired to the <Select> via aria-describedby so
  // screen readers announce it when the field gains focus.
  let modeHint = $derived(
    form.mode === 'tnc'
      ? "Peer is a hardware TNC supplying off-air RX. Frames are routed to igate, digipeater, messages, and map — never auto-retransmitted."
      : "Peer is an APRS app. Frames it sends are queued for transmission on graywolf's radio."
  );

  const modeLabels = { modem: 'Modem', tnc: 'TNC' };

  function emptyForm() {
    return {
      type: 'tcp',
      tcp_port: TCP_PORT_DEFAULT,
      // tcp-client fields:
      remote_host: '',
      remote_port: TCP_PORT_DEFAULT,
      reconnect_init_ms: String(RECONNECT_INIT_DEFAULT_MS),
      reconnect_max_ms: String(RECONNECT_MAX_DEFAULT_MS),
      serial_device: '',
      baud_rate: BAUD_RATE_DEFAULT,
      channel: '1',
      mode: 'modem',
      tnc_ingress_rate_hz: TNC_INGRESS_RATE_DEFAULT,
      tnc_ingress_burst: TNC_INGRESS_BURST_DEFAULT,
      // Phase 3 D4: governor-TX opt-in. Default false for new rows —
      // operators must knowingly enable cross-channel digipeat / beacon
      // / iGate transmission via a KISS-TNC link. Only meaningful when
      // Mode == "tnc".
      allow_tx_from_governor: false,
    };
  }

  // Phase 4 D16: poll /api/kiss every 2s while the page is open so
  // the Status column reflects live supervisor state. The shared
  // channelsStore handles the channel list; this is the per-page
  // interface poller.
  async function refreshItems() {
    try {
      items = (await api.get('/kiss')) || [];
      pollFailures = 0;
    } catch (err) {
      // Leave the old list in place; surfacing a toast on every
      // failed 2 s poll would be obnoxious. Instead bump the failure
      // counter — once we cross KISS_STALE_AFTER_FAILURES the header
      // renders a muted "stale" pill so operators know the data is
      // no longer fresh.
      pollFailures = pollFailures + 1;
      if (pollFailures === KISS_STALE_AFTER_FAILURES) {
        console.warn('kiss: /api/kiss poll has failed', pollFailures, 'times in a row');
      }
    }
  }

  onMount(async () => {
    await refreshItems();
    startChannelsStore();
    pollTimer = setInterval(refreshItems, KISS_POLL_MS);
    clockTimer = setInterval(() => {
      clockTick = (clockTick + 1) % CLOCK_TICK_MOD;
    }, CLOCK_TICK_MS);
  });

  onDestroy(() => {
    if (pollTimer) clearInterval(pollTimer);
    if (clockTimer) clearInterval(clockTimer);
  });

  function openCreate() {
    editing = null;
    form = emptyForm();
    // Default AllowTxFromGovernor=true for new tcp-client rows per
    // plan D4. When the operator flips to tcp-client we pre-check
    // the governor-TX checkbox so the common case (outbound TNC for
    // digipeat/beacon) works one-click.
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = {
      ...row,
      tcp_port: String(row.tcp_port ?? ''),
      remote_host: row.remote_host || '',
      remote_port: String(row.remote_port || ''),
      reconnect_init_ms: String(row.reconnect_init_ms || RECONNECT_INIT_DEFAULT_MS),
      reconnect_max_ms: String(row.reconnect_max_ms || RECONNECT_MAX_DEFAULT_MS),
      baud_rate: String(row.baud_rate),
      channel: String(row.channel || 1),
      mode: row.mode || 'modem',
      tnc_ingress_rate_hz: String(row.tnc_ingress_rate_hz || 50),
      tnc_ingress_burst: String(row.tnc_ingress_burst || 100),
      allow_tx_from_governor: !!row.allow_tx_from_governor,
    };
    modalOpen = true;
  }

  // Phase 4 D4: new tcp-client rows default governor-TX on. Watch
  // the form.type field and flip the checkbox when the operator
  // selects tcp-client for the first time on a new row. (Not fired
  // on edit — operator's existing choice is preserved.)
  $effect(() => {
    if (editing) return;
    if (form.type === 'tcp-client' && !form._clientDefaultsApplied) {
      form.allow_tx_from_governor = true;
      form.mode = 'tnc'; // tcp-client + governor TX only makes sense in TNC mode
      form._clientDefaultsApplied = true;
    }
  });

  function buildPayload() {
    const data = {
      type: form.type,
      channel: parseInt(form.channel) || 1,
      mode: form.mode,
      tnc_ingress_rate_hz: parseInt(form.tnc_ingress_rate_hz) || 0,
      tnc_ingress_burst: parseInt(form.tnc_ingress_burst) || 0,
      // Only send allow_tx_from_governor when Mode=tnc. The backend
      // validator rejects the combination (Mode!=tnc with allow=true)
      // so defensively coerce here to match UI visibility.
      allow_tx_from_governor: form.mode === 'tnc' ? !!form.allow_tx_from_governor : false,
    };
    switch (form.type) {
      case 'tcp':
        data.tcp_port = parseInt(form.tcp_port) || 0;
        break;
      case 'tcp-client':
        data.remote_host = (form.remote_host || '').trim();
        data.remote_port = parseInt(form.remote_port) || 0;
        data.reconnect_init_ms = parseInt(form.reconnect_init_ms) || RECONNECT_INIT_DEFAULT_MS;
        data.reconnect_max_ms = parseInt(form.reconnect_max_ms) || RECONNECT_MAX_DEFAULT_MS;
        break;
      case 'serial':
      case 'bluetooth':
        data.serial_device = form.serial_device || '';
        data.baud_rate = parseInt(form.baud_rate) || 9600;
        break;
    }
    return data;
  }

  async function commitSave() {
    const data = buildPayload();
    try {
      if (editing) {
        await api.put(`/kiss/${editing.id}`, data);
        toasts.success('KISS config updated');
      } else {
        await api.post('/kiss', data);
        toasts.success('KISS config created');
      }
      modalOpen = false;
      await refreshItems();
      refreshChannels();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  function handleSave() {
    // Mode changes on an existing interface take effect the instant the
    // server restarts the per-interface KISS server — connected peers see
    // routing behavior flip under them. Make the operator confirm it.
    if (editing && form.mode !== editing.mode) {
      modeChangeMessage = `Change mode from ${modeLabels[editing.mode] || editing.mode} to ${modeLabels[form.mode] || form.mode}? Connected peers will see routing behavior change immediately.`;
      modeChangeOpen = true;
      return;
    }
    commitSave();
  }

  function describeRow(row) {
    const mode = modeLabels[row.mode] || 'Modem';
    if (row.type === 'tcp') return `TCP server on port ${row.tcp_port}, ${mode}`;
    if (row.type === 'tcp-client') return `TCP client → ${row.remote_host}:${row.remote_port}, ${mode}`;
    if (row.type === 'serial') {
      const dev = (row.serial_device || '').trim();
      return dev ? `serial ${dev}, ${mode}` : `serial, ${mode}`;
    }
    return `#${row.id}, ${mode}`;
  }

  function endpointText(row) {
    if (row.type === 'tcp') return `:${row.tcp_port}`;
    if (row.type === 'tcp-client') return `${row.remote_host}:${row.remote_port}`;
    if (row.type === 'serial') return row.serial_device || '—';
    return '—';
  }

  function handleDelete(row) {
    pendingDeleteId = row.id;
    confirmMessage = `Delete KISS interface (${describeRow(row)}) on channel ${row.channel}?`;
    confirmOpen = true;
  }

  async function confirmDelete() {
    const id = pendingDeleteId;
    pendingDeleteId = null;
    if (id == null) return;
    try {
      await api.delete(`/kiss/${id}`);
      toasts.success('Interface deleted');
      await refreshItems();
      refreshChannels();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  function toggleExpanded(id) {
    expandedId = expandedId === id ? null : id;
  }

  async function handleRetryNow(row) {
    try {
      // Use api.post so 401 → login redirect and structured ApiError
      // handling (body JSON parsing, status propagation) match every
      // other write on this page. No body required for this endpoint.
      await api.post(`/kiss/${row.id}/reconnect`, null);
      toasts.success('Reconnect triggered');
      // Refresh immediately so the operator sees state move.
      await refreshItems();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  // Healthglyph: live / down based on state. Client-only rows (never
  // server-listen) are what really benefit from this; server-listen
  // reports StateListening which is always "live" (the listener bound
  // successfully), so a green dot.
  function healthGlyph(state) {
    const connected = state === 'connected' || state === 'listening';
    return connected ? '●' : '○';
  }
  function healthClass(state) {
    return (state === 'connected' || state === 'listening') ? 'health-live' : 'health-down';
  }
</script>

<PageHeader title="KISS Interfaces" subtitle="KISS interface configuration">
  {#if pollIsStale}
    <!-- Stale indicator (D16 polish): /api/kiss has failed ≥5 times
         consecutively; status columns may be out of date. Muted on
         purpose — this is an FYI, not an alarm. -->
    <span class="poll-stale-pill" role="status" aria-live="polite" title="Status updates paused because /api/kiss is unreachable. Current rows may be out of date.">
      <span aria-hidden="true">○</span>
      Status stale
    </span>
  {/if}
  <Button variant="primary" onclick={openCreate}>+ Add KISS</Button>
</PageHeader>

{#each items.filter(i => i.needs_reconfig) as reconfigRow (reconfigRow.id)}
  <!-- Phase 5 D12: a cascade delete of a referenced channel nulls
       this interface's Channel and flags NeedsReconfig. The operator
       is expected to pick a new channel and save to clear the flag. -->
  <div class="needs-reconfig-banner" role="status" aria-live="polite">
    <span class="needs-reconfig-icon" aria-hidden="true">⚠</span>
    <span>
      <strong>{reconfigRow.name || `KISS interface #${reconfigRow.id}`}</strong>'s channel was deleted.
      Reassign a channel and save to clear this warning.
    </span>
    <Button variant="ghost" onclick={() => openEdit(reconfigRow)}>Edit</Button>
  </div>
{/each}

<DataTable
  {columns}
  rows={items}
  onEdit={openEdit}
  onDelete={handleDelete}
  cells={{ type: typeCell, endpoint: endpointCell, mode: modeCell, status: statusCell }}
/>

{#snippet modeCell(value, _row)}
  <Badge variant={value === 'tnc' ? 'success' : 'info'}>{modeLabels[value] || 'Modem'}</Badge>
{/snippet}

{#snippet typeCell(value, _row)}
  {#if value === 'tcp-client'}
    <Badge variant="info">TCP Client</Badge>
  {:else if value === 'tcp'}
    <Badge>TCP Server</Badge>
  {:else}
    <Badge>{value || '—'}</Badge>
  {/if}
{/snippet}

{#snippet endpointCell(_value, row)}
  <span class="endpoint">{endpointText(row)}</span>
{/snippet}

{#snippet statusCell(_value, row)}
  <div class="status-cell" data-tick={clockTick}>
    <!-- clockTick is in data-tick so any change triggers snippet
         re-render; that propagates to the countdownText call below. -->
    <button
      type="button"
      class="status-btn"
      aria-expanded={expandedId === row.id}
      aria-controls={`status-detail-${row.id}`}
      aria-label={`Status for KISS interface ${row.id}: ${stateLabel(row.state)}`}
      onclick={(e) => { e.stopPropagation(); toggleExpanded(row.id); }}
    >
      <span class={healthClass(row.state)} aria-hidden="true">{healthGlyph(row.state)}</span>
      <Badge variant={stateBadgeVariant(row.state)}>{stateLabel(row.state)}</Badge>
      {#if row.state === 'backoff'}
        <span class="countdown">{countdownText(row.retry_at_unix_ms)}</span>
      {/if}
    </button>
    {#if expandedId === row.id}
      <div id={`status-detail-${row.id}`} class="status-detail" role="region" aria-label="Status detail">
        {#if row.type === 'tcp-client'}
          <div class="detail-row"><span class="detail-label">Peer:</span> <span>{row.peer_addr || `${row.remote_host}:${row.remote_port}`}</span></div>
          <div class="detail-row"><span class="detail-label">Connected since:</span> <span>{formatLocalTime(row.connected_since) || '—'}</span></div>
          <div class="detail-row"><span class="detail-label">Reconnect count:</span> <span>{row.reconnect_count || 0}</span></div>
          <div class="detail-row"><span class="detail-label">Backoff:</span> <span>{row.backoff_seconds || 0}s</span></div>
          {#if row.last_error}
            <div class="detail-row detail-err"><span class="detail-label">Last error:</span> <span>{row.last_error}</span></div>
          {/if}
          {#if canRetryNow(row.state)}
            <div class="detail-actions">
              <Button variant="primary" onclick={(e) => { e.stopPropagation?.(); handleRetryNow(row); }}>Retry now</Button>
            </div>
          {/if}
        {:else if row.type === 'tcp'}
          <div class="detail-row"><span class="detail-label">Listening:</span> <span>:{row.tcp_port}</span></div>
        {:else}
          <div class="detail-row"><span class="detail-label">Device:</span> <span>{row.serial_device || '—'}</span></div>
        {/if}
      </div>
    {/if}
  </div>
{/snippet}

<Modal bind:open={modalOpen} title={editing ? 'Edit KISS' : 'New KISS Interface'}>
    <FormField label="Mode" id="kiss-mode" hint={modeHint}>
      {#snippet children(describedBy)}
        <Select id="kiss-mode" bind:value={form.mode} options={modeOptions} aria-describedby={describedBy} />
      {/snippet}
    </FormField>
    <FormField label="Type" id="kiss-type">
      <Select id="kiss-type" bind:value={form.type} options={typeOptions} />
    </FormField>
    {#if form.type === 'tcp'}
      <FormField label="TCP Port" id="kiss-port">
        <Input id="kiss-port" bind:value={form.tcp_port} type="number" placeholder="8001" />
      </FormField>
    {:else if form.type === 'tcp-client'}
      <FormField
        label="Remote Host"
        id="kiss-remote-host"
        hint="Hostname or IP of the remote KISS TNC to dial."
      >
        <Input id="kiss-remote-host" bind:value={form.remote_host} placeholder="lora.example.com" />
      </FormField>
      <FormField
        label="Remote Port"
        id="kiss-remote-port"
        hint="TCP port on the remote KISS TNC."
      >
        <Input id="kiss-remote-port" bind:value={form.remote_port} type="number" min={1} max={65535} placeholder="8001" />
      </FormField>
    {:else}
      <FormField label="Serial Device" id="kiss-serial">
        <Input id="kiss-serial" bind:value={form.serial_device} placeholder="/dev/ttyUSB0" />
      </FormField>
      <FormField label="Baud Rate" id="kiss-baud">
        <Input id="kiss-baud" bind:value={form.baud_rate} type="number" placeholder="9600" />
      </FormField>
    {/if}
    <FormField label="Channel" id="kiss-channel">
      {#if channels.length > 0}
        <ChannelListbox
          id="kiss-channel"
          bind:value={form.channel}
          valueType="string"
          channels={channels}
        />
      {:else}
        <Input id="kiss-channel" bind:value={form.channel} type="number" placeholder="1" />
      {/if}
    </FormField>
    {#if form.mode === 'tnc'}
      <!-- Governor-TX opt-in (Phase 3 D4). Default unchecked — the
           operator must explicitly enable transmission via this
           interface to avoid surprise loops. -->
      <FormField
        label="Transmit from digipeater/beacon/iGate to this interface"
        id="kiss-allow-tx"
        hint="Only enable if you want graywolf's digipeater, beacon, and iGate to be able to transmit via this interface. Leave unchecked to use this as an RX-only TNC (safer; prevents loops)."
      >
        <Checkbox id="kiss-allow-tx" bind:checked={form.allow_tx_from_governor} />
      </FormField>
      <!-- Per-interface ingress rate limiter. Only meaningful in TNC mode
           (Modem-mode ingest goes to the TxGovernor, not the RX fanout). -->
      <div class="advanced-section">
        <div class="advanced-label">Advanced</div>
        <FormField label="Ingress Rate (frames/sec)" id="kiss-rate"
          hint="Token-bucket refill rate for inbound frames. Default 50.">
          <Input id="kiss-rate" bind:value={form.tnc_ingress_rate_hz} type="number" min={0} max={10000} placeholder="50" />
        </FormField>
        <FormField label="Ingress Burst" id="kiss-burst"
          hint="Maximum burst size before rate limiting kicks in. Default 100.">
          <Input id="kiss-burst" bind:value={form.tnc_ingress_burst} type="number" min={0} max={100000} placeholder="100" />
        </FormField>
        {#if form.type === 'tcp-client'}
          <FormField label="Reconnect initial delay (ms)" id="kiss-reconnect-init"
            hint="First backoff delay on dial failure. Subsequent failures grow exponentially up to the max.">
            <Input id="kiss-reconnect-init" bind:value={form.reconnect_init_ms} type="number" min={250} max={3600000} placeholder="1000" />
          </FormField>
          <FormField label="Reconnect max delay (ms)" id="kiss-reconnect-max"
            hint="Upper bound on the backoff schedule.">
            <Input id="kiss-reconnect-max" bind:value={form.reconnect_max_ms} type="number" min={250} max={3600000} placeholder="300000" />
          </FormField>
        {/if}
      </div>
    {/if}
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" onclick={handleSave}>{editing ? 'Save' : 'Create'}</Button>
    </div>
</Modal>

<ConfirmDialog
  bind:open={confirmOpen}
  title="Delete Interface"
  message={confirmMessage}
  confirmLabel="Delete"
  onConfirm={confirmDelete}
/>

<ConfirmDialog
  bind:open={modeChangeOpen}
  title="Change Interface Mode"
  message={modeChangeMessage}
  confirmLabel="Change Mode"
  confirmVariant="primary"
  onConfirm={commitSave}
/>

<style>
  .poll-stale-pill {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.125rem 0.5rem;
    margin-right: 0.5rem;
    font-size: 0.75rem;
    color: var(--text-muted, #888);
    background: var(--surface-muted, rgba(128, 128, 128, 0.08));
    border: 1px solid var(--border-muted, rgba(128, 128, 128, 0.2));
    border-radius: 999px;
  }

  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
  .advanced-section {
    margin-top: 8px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }
  .advanced-label {
    font-size: 11px;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: 8px;
  }
  .endpoint {
    font-family: var(--font-mono, monospace);
    font-size: 13px;
  }
  .status-cell {
    display: flex;
    flex-direction: column;
    gap: 6px;
    align-items: flex-start;
  }
  .status-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 4px;
    padding: 2px 6px;
    cursor: pointer;
    color: inherit;
    font: inherit;
  }
  .status-btn:hover,
  .status-btn:focus-visible {
    border-color: var(--border-color);
    outline: none;
  }
  .health-live { color: var(--color-success, #4caf50); font-size: 14px; }
  .health-down { color: var(--color-warning, #ffa000); font-size: 14px; }
  .countdown {
    font-size: 12px;
    color: var(--text-secondary);
    margin-left: 4px;
  }
  .status-detail {
    background: var(--bg-elevated, #fafafa);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    padding: 8px 10px;
    font-size: 13px;
    min-width: 260px;
  }
  .detail-row {
    display: flex;
    gap: 8px;
    margin-bottom: 4px;
  }
  .detail-label {
    color: var(--text-secondary);
    min-width: 120px;
  }
  .detail-err {
    color: var(--color-error, #d32f2f);
  }
  .detail-actions {
    margin-top: 8px;
  }
  /* Phase 5 D12: NeedsReconfig banner. Yellow/warning row that
     advertises a KISS interface whose channel was swept by a cascade
     delete. Clearing happens via the edit form when the operator
     saves a valid channel. */
  .needs-reconfig-banner {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    margin-bottom: 8px;
    background: var(--color-warning-muted, rgba(212, 167, 44, 0.15));
    border-left: 3px solid var(--color-warning, #d4a72c);
    border-radius: var(--radius);
    color: var(--text-primary);
    font-size: 13px;
  }
  .needs-reconfig-icon {
    font-size: 16px;
    color: var(--color-warning, #d4a72c);
    flex-shrink: 0;
  }
</style>
