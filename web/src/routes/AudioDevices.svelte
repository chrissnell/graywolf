<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Badge, AlertDialog, Checkbox } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';

  let devices = $state([]);
  let channels = $state([]);
  let available = $state([]);
  let loadingAvail = $state(false);
  let scanLevels = $state({});
  let scanning = $state(false);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state(emptyForm());
  let errors = $state({});
  let deleteTarget = $state(null);
  let deleteOpen = $state(false);
  let deleteAffectedChannels = $state([]);
  let deleteCascadeAcked = $state(false);
  let deviceLevels = $state({});
  let testingTone = $state(null);
  let gainTimers = {};

  function emptyForm() {
    return { name: '', device_path: '', sample_rate: '48000', source_type: 'soundcard', direction: 'input' };
  }

  onMount(() => {
    loadDevices();
    loadChannels();
    const interval = setInterval(pollLevels, 200);
    const channelsInterval = setInterval(loadChannels, 5000);
    return () => {
      clearInterval(interval);
      clearInterval(channelsInterval);
      Object.values(gainTimers).forEach(clearTimeout);
    };
  });

  async function pollLevels() {
    try {
      deviceLevels = await api.get('/audio-devices/levels') || {};
    } catch (_) {}
  }

  async function playTestTone(dev) {
    testingTone = dev.id;
    try {
      await api.post(`/audio-devices/${dev.id}/test-tone`);
      toasts.success(`Test tone played on "${dev.name}"`);
    } catch (err) {
      toasts.error(`Test tone failed: ${err.message}`);
    } finally {
      testingTone = null;
    }
  }

  // Slider operates directly in dB: -60 to +12
  const GAIN_DB_MIN = -60;
  const GAIN_DB_MAX = 12;

  function handleGainChange(dev, sliderValue) {
    const gainDB = parseFloat(sliderValue);
    dev.gain_db = gainDB;
    // Debounce API call
    if (gainTimers[dev.id]) clearTimeout(gainTimers[dev.id]);
    gainTimers[dev.id] = setTimeout(async () => {
      try {
        await api.put(`/audio-devices/${dev.id}/gain`, { gain_db: gainDB });
      } catch (err) {
        toasts.error(`Gain update failed: ${err.message}`);
      }
    }, 300);
  }

  function levelToPercent(dbfs) {
    if (dbfs == null) return 0;
    const clamped = Math.max(-60, Math.min(0, dbfs));
    return ((clamped + 60) / 60) * 100;
  }

  function levelColor(dbfs) {
    if (dbfs == null) return 'var(--text-muted)';
    if (dbfs > -6) return 'var(--color-danger, #f85149)';
    if (dbfs > -20) return 'var(--color-warning, #d29922)';
    return 'var(--success, #3fb950)';
  }

  function formatLevel(dbfs) {
    if (dbfs == null) return '— dB';
    return `${dbfs.toFixed(0)} dB`;
  }

  async function loadDevices() {
    devices = await api.get('/audio-devices') || [];
  }

  async function loadChannels() {
    try {
      channels = await api.get('/channels') || [];
    } catch (_) {
      channels = [];
    }
  }

  async function refreshAvailable() {
    loadingAvail = true;
    try {
      available = await api.get('/audio-devices/available') || [];
      toasts.success(`Found ${available.length} audio device(s)`);
    } catch (err) {
      toasts.error(err.message);
    } finally {
      loadingAvail = false;
    }
  }

  async function scanInputLevels() {
    scanning = true;
    scanLevels = {};
    try {
      const results = await api.post('/audio-devices/scan-levels') || [];
      const map = {};
      for (const r of results) {
        map[r.name] = r;
      }
      scanLevels = map;
      const active = results.filter(r => r.has_signal).length;
      toasts.success(`Scan complete — ${active} input(s) with signal`);
    } catch (err) {
      toasts.error(`Level scan failed: ${err.message}`);
    } finally {
      scanning = false;
    }
  }

  function openCreate() {
    editing = null;
    form = emptyForm();
    errors = {};
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = { ...row, sample_rate: String(row.sample_rate) };
    errors = {};
    modalOpen = true;
  }

  function openCreateFromAvail(dev) {
    editing = null;
    form = {
      name: dev.description || dev.name,
      device_path: dev.path,
      sample_rate: String((dev.sample_rates || [48000]).at(-1)),
      source_type: 'soundcard',
      direction: dev.is_input ? 'input' : 'output',
    };
    errors = {};
    modalOpen = true;
  }

  function handleModalClose() {
    editing = null;
    form = emptyForm();
    errors = {};
  }

  function validate() {
    const e = {};
    if (!form.name.trim()) e.name = 'Required';
    if (!form.device_path.trim()) e.device_path = 'Required';
    errors = e;
    return Object.keys(e).length === 0;
  }

  async function handleSave() {
    if (!validate()) return;
    const data = { ...form, sample_rate: parseInt(form.sample_rate), channels: 1 };
    // Strip fields not in AudioDeviceRequest DTO (backend rejects unknown fields)
    delete data.id;
    try {
      if (editing) {
        await api.put(`/audio-devices/${editing.id}`, data);
        toasts.success('Audio device updated');
      } else {
        await api.post('/audio-devices', data);
        toasts.success('Audio device added');
      }
      modalOpen = false;
      await loadDevices();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  async function confirmDelete(row) {
    deleteTarget = row;
    deleteAffectedChannels = [];
    deleteCascadeAcked = false;
    try {
      const channels = await api.get('/channels') || [];
      deleteAffectedChannels = channels.filter(
        ch => ch.input_device_id === row.id || ch.output_device_id === row.id
      );
    } catch (_) {
      // If we can't fetch channels, still allow delete — backend will 409 if needed
    }
    deleteOpen = true;
  }

  async function executeDelete() {
    if (!deleteTarget) return;
    const cascade = deleteAffectedChannels.length > 0;
    try {
      const qs = cascade ? '?cascade=true' : '';
      await api.delete(`/audio-devices/${deleteTarget.id}${qs}`);
      const count = deleteAffectedChannels.length;
      const msg = count > 0
        ? `Device and ${count} channel${count !== 1 ? 's' : ''} deleted`
        : 'Device deleted';
      toasts.success(msg);
      await loadDevices();
    } catch (err) {
      toasts.error(err.message);
    } finally {
      deleteOpen = false;
      deleteTarget = null;
      deleteAffectedChannels = [];
      deleteCascadeAcked = false;
    }
  }

  let hasInput = $derived(devices.some(d => d.direction === 'input'));
  let hasOutput = $derived(devices.some(d => d.direction === 'output'));
  let inputDevices = $derived(devices.filter(d => d.direction === 'input'));
  let outputDevices = $derived(devices.filter(d => d.direction === 'output'));
  let configuredPaths = $derived(new Set(devices.map(d => d.device_path)));
  // Level meters in the modem only fire for devices that a channel
  // actively binds to. If no channel has audio assigned, the meters
  // here will sit at -inf no matter what hardware is plugged in.
  let hasAudioChannel = $derived(
    channels.some(ch => ch.input_device_id != null || (ch.output_device_id ?? 0) !== 0)
  );
  let showNoChannelBanner = $derived(devices.length > 0 && !hasAudioChannel);

  function truncatePath(p, max = 40) {
    if (!p || p.length <= max) return p || '—';
    return '...' + p.slice(-(max - 3));
  }
</script>

<PageHeader title="Audio Devices" subtitle="Sound card configuration">
  <Button onclick={refreshAvailable} disabled={loadingAvail}>
    {loadingAvail ? 'Scanning...' : 'Detect Devices'}
  </Button>
  {#if available.some(d => d.is_input)}
    <Button onclick={scanInputLevels} disabled={scanning}>
      {scanning ? 'Scanning...' : 'Scan Input Levels'}
    </Button>
  {/if}
  <Button variant="primary" onclick={openCreate}>+ Add Device</Button>
</PageHeader>

{#if showNoChannelBanner}
  <div class="no-channel-banner" role="alert">
    <strong>Level meters inactive:</strong>
    no channel has an audio interface assigned, so the modem isn't
    opening any of these devices. Create a channel on the
    <a href="#/channels">Channels page</a> and assign input/output
    interfaces to it, then return here to adjust levels.
  </div>
{/if}

<!-- Station readiness -->
<div class="readiness">
  <div class="readiness-item" class:ready={hasInput}>
    <div class="readiness-icon">{hasInput ? '●' : '○'}</div>
    <div class="readiness-info">
      <span class="readiness-label">Receive (RX)</span>
      {#if hasInput}
        <span class="readiness-detail">{inputDevices.length} input device{inputDevices.length !== 1 ? 's' : ''} configured</span>
      {:else}
        <span class="readiness-detail needs">Needs an input device (microphone / receiver audio)</span>
      {/if}
    </div>
  </div>
  <div class="readiness-item" class:ready={hasOutput}>
    <div class="readiness-icon">{hasOutput ? '●' : '○'}</div>
    <div class="readiness-info">
      <span class="readiness-label">Transmit (TX)</span>
      {#if hasOutput}
        <span class="readiness-detail">{outputDevices.length} output device{outputDevices.length !== 1 ? 's' : ''} configured — also requires PTT</span>
      {:else}
        <span class="readiness-detail needs">Needs an output device + PTT configuration</span>
      {/if}
    </div>
  </div>
</div>

<!-- Configured devices -->
<div class="section-label">Configured Devices</div>
{#if devices.length === 0}
  <div class="empty-state">No audio devices configured. Detect devices below or add one manually.</div>
{:else}
  <div class="device-grid">
    {#each devices as dev}
      <div class="device-card">
        <div class="device-header">
          <span class="device-name">{dev.name}</span>
          <div class="device-badges">
            <Badge variant={dev.direction === 'input' ? 'info' : 'success'}>
              {dev.direction === 'input' ? 'Input' : 'Output'}
            </Badge>
            <Badge variant="default">{dev.source_type}</Badge>
          </div>
        </div>
        <div class="device-details">
          <div class="detail-row">
            <span class="detail-label">Path</span>
            <span class="detail-value" title={dev.device_path}>{truncatePath(dev.device_path)}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Sample Rate</span>
            <span class="detail-value">{dev.sample_rate} Hz</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Channels</span>
            <span class="detail-value">Mono</span>
          </div>
        </div>
        <!-- Audio level meter -->
        <div class="level-section">
          <div class="level-row">
            <span class="level-label">Level</span>
            <div class="level-track">
              <div class="level-fill" style="width: {levelToPercent(deviceLevels[dev.id]?.peak_dbfs)}%; background: {levelColor(deviceLevels[dev.id]?.peak_dbfs)}"></div>
            </div>
            <span class="level-value" class:clipping={deviceLevels[dev.id]?.clipping}>
              {deviceLevels[dev.id]?.clipping ? 'CLIP' : formatLevel(deviceLevels[dev.id]?.peak_dbfs)}
            </span>
          </div>
          <!-- Gain slider -->
          <div class="gain-row">
            <span class="gain-label">Gain</span>
            <input
              type="range"
              class="gain-slider"
              min={GAIN_DB_MIN}
              max={GAIN_DB_MAX}
              step="0.5"
              value={dev.gain_db ?? 0}
              oninput={(e) => handleGainChange(dev, e.target.value)}
              title="Software gain (-60 to +12 dB)"
            />
            <button class="gain-value" onclick={() => handleGainChange(dev, 0)} title="Reset to 0 dB">
              {(dev.gain_db ?? 0).toFixed(1)} dB
            </button>
          </div>
        </div>
        <div class="device-actions">
          {#if dev.direction === 'output'}
            <Button
              variant="ghost"
              onclick={() => playTestTone(dev)}
              disabled={testingTone === dev.id}
            >
              {testingTone === dev.id ? 'Playing...' : 'Test Tone'}
            </Button>
          {/if}
          <Button variant="ghost" onclick={() => openEdit(dev)}>Edit</Button>
          <Button variant="danger" onclick={() => confirmDelete(dev)}>Delete</Button>
        </div>
      </div>
    {/each}
  </div>
{/if}

<!-- Available devices from hardware scan -->
{#if available.length > 0}
  <div class="section-label" style="margin-top: 24px;">Detected Hardware</div>
  <p class="section-hint">Click a device to add it to your configuration.</p>
  <div class="avail-grid">
    {#each available as dev}
      <button class="avail-card" class:added={configuredPaths.has(dev.path)} class:recommended={dev.recommended} onclick={() => openCreateFromAvail(dev)}>
        <div class="avail-header">
          <strong class="avail-name">{dev.description || dev.name}</strong>
          <div class="avail-badges">
            {#if configuredPaths.has(dev.path)}
              <Badge variant="success">Added</Badge>
            {/if}
            {#if dev.recommended}
              <Badge variant="warning">Recommended</Badge>
            {/if}
            <Badge variant={dev.is_input ? 'info' : 'success'}>
              {dev.is_input ? 'Input' : 'Output'}
            </Badge>
          </div>
        </div>
        {#if dev.host_api}
          <span class="avail-api">{dev.host_api}</span>
        {/if}
        <span class="avail-path" title={dev.path}>{dev.path}</span>
        <div class="avail-caps">
          <span>Rates: {(dev.sample_rates || []).join(', ')} Hz</span>
          <span>Channels: {(dev.channels || []).join(', ')}</span>
        </div>
        {#if dev.is_default}
          <Badge variant="success">System Default</Badge>
        {/if}
        {#if dev.is_input && scanLevels[dev.path]}
          {@const lev = scanLevels[dev.path]}
          {#if lev.error}
            <span class="scan-error">Error: {lev.error}</span>
          {:else}
            <div class="scan-level">
              <div class="scan-bar">
                <div class="scan-fill" class:has-signal={lev.has_signal} style="width: {Math.max(0, ((lev.peak_dbfs + 60) / 60) * 100)}%"></div>
              </div>
              <span class="scan-value">{lev.peak_dbfs.toFixed(0)} dB{lev.has_signal ? ' — signal detected' : ''}</span>
            </div>
          {/if}
        {/if}
      </button>
    {/each}
  </div>
{/if}

<!-- Add/Edit modal -->
<Modal bind:open={modalOpen} title={editing ? 'Edit Audio Device' : 'Add Audio Device'} onClose={handleModalClose}>
  <FormField label="Name" error={errors.name} id="ad-name">
    <Input id="ad-name" bind:value={form.name} placeholder="USB Sound Card" />
  </FormField>
  <FormField label="Device Path" error={errors.device_path} id="ad-path">
    <Input id="ad-path" bind:value={form.device_path} placeholder="hw:0,0" />
  </FormField>
  <FormField label="Direction" id="ad-dir">
    <Select id="ad-dir" bind:value={form.direction} options={[
      { value: 'input', label: 'Input (Microphone / Receiver)' },
      { value: 'output', label: 'Output (Speaker / Transmitter)' },
    ]} />
  </FormField>
  <FormField label="Source Type" id="ad-type">
    <Select id="ad-type" bind:value={form.source_type} options={[
      { value: 'soundcard', label: 'Sound Card' },
      { value: 'flac', label: 'FLAC File' },
      { value: 'stdin', label: 'Standard Input' },
      { value: 'sdr_udp', label: 'SDR UDP Stream' },
    ]} />
  </FormField>
  <FormField label="Sample Rate" id="ad-rate">
    <Select id="ad-rate" bind:value={form.sample_rate} options={[
      { value: '8000', label: '8000 Hz' },
      { value: '16000', label: '16000 Hz' },
      { value: '44100', label: '44100 Hz' },
      { value: '48000', label: '48000 Hz' },
      { value: '96000', label: '96000 Hz' },
    ]} />
  </FormField>
  <div class="modal-actions">
    <Button onclick={() => modalOpen = false}>Cancel</Button>
    <Button variant="primary" onclick={handleSave}>{editing ? 'Save' : 'Add'}</Button>
  </div>
</Modal>

<!-- Delete confirmation -->
<AlertDialog bind:open={deleteOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete Audio Device</AlertDialog.Title>
    <AlertDialog.Description>
      Are you sure you want to delete "{deleteTarget?.name}"? This cannot be undone.
    </AlertDialog.Description>
    {#if deleteAffectedChannels.length > 0}
      <div class="cascade-warning">
        <strong>The following channels use this device and will also be deleted:</strong>
        <ul class="affected-channels">
          {#each deleteAffectedChannels as ch}
            <li>{ch.name}</li>
          {/each}
        </ul>
        <label class="cascade-ack">
          <Checkbox bind:checked={deleteCascadeAcked} />
          <span>I understand — delete this device and its channels</span>
        </label>
      </div>
    {/if}
    <div class="modal-footer">
      <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action
        class="danger-action"
        onclick={executeDelete}
        disabled={deleteAffectedChannels.length > 0 && !deleteCascadeAcked}
      >Delete</AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<style>
  .no-channel-banner {
    margin: 0 0 16px;
    padding: 10px 14px;
    border: 1px solid var(--color-warning, #d29922);
    border-left-width: 4px;
    border-radius: var(--radius);
    background: var(--color-warning-muted, rgba(210, 153, 34, 0.15));
    color: var(--text-primary);
    font-size: 13px;
    line-height: 1.5;
  }
  .no-channel-banner a {
    color: var(--accent, #58a6ff);
    text-decoration: underline;
  }

  /* Station readiness */
  .readiness {
    display: flex;
    gap: 16px;
    margin-bottom: 24px;
    flex-wrap: wrap;
  }
  .readiness-item {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    flex: 1;
    min-width: 260px;
    padding: 12px 16px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    border-left: 3px solid var(--text-muted);
  }
  .readiness-item.ready {
    border-left-color: var(--success, #3fb950);
  }
  .readiness-icon {
    font-size: 16px;
    line-height: 1.2;
    color: var(--text-muted);
  }
  .readiness-item.ready .readiness-icon {
    color: var(--success, #3fb950);
  }
  .readiness-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .readiness-label {
    font-weight: 600;
    font-size: 14px;
  }
  .readiness-detail {
    font-size: 12px;
    color: var(--text-secondary);
  }
  .readiness-detail.needs {
    color: var(--text-muted);
    font-style: italic;
  }

  .section-label {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: 8px;
  }
  .section-hint {
    font-size: 13px;
    color: var(--text-muted);
    margin: -4px 0 10px;
  }

  .empty-state {
    text-align: center;
    color: var(--text-muted);
    padding: 32px;
    border: 1px dashed var(--border-color);
    border-radius: var(--radius);
    margin-bottom: 16px;
  }

  /* Configured device cards */
  .device-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 12px;
    margin-bottom: 16px;
  }
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
  /* Audio level meter + gain */
  .level-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }
  .level-row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
  }
  .level-label, .gain-label {
    color: var(--text-secondary);
    width: 36px;
    flex-shrink: 0;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }
  .level-track {
    flex: 1;
    height: 8px;
    background: var(--bg-tertiary, #161b22);
    border-radius: 4px;
    overflow: hidden;
  }
  .level-fill {
    height: 100%;
    border-radius: 4px;
    transition: width 0.15s ease-out, background 0.15s;
    min-width: 0;
  }
  .level-value {
    width: 48px;
    text-align: right;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text-secondary);
    flex-shrink: 0;
  }
  .level-value.clipping {
    color: var(--color-danger, #f85149);
    font-weight: 700;
  }
  .gain-row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
  }
  .gain-slider {
    flex: 1;
    height: 4px;
    -webkit-appearance: none;
    appearance: none;
    background: var(--bg-tertiary, #161b22);
    border-radius: 2px;
    outline: none;
    cursor: pointer;
  }
  .gain-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--accent, #58a6ff);
    border: 2px solid var(--bg-primary, #0d1117);
    cursor: pointer;
  }
  .gain-slider::-moz-range-thumb {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--accent, #58a6ff);
    border: 2px solid var(--bg-primary, #0d1117);
    cursor: pointer;
  }
  .gain-value {
    width: 56px;
    text-align: right;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text-secondary);
    flex-shrink: 0;
    background: none;
    border: none;
    padding: 2px 4px;
    border-radius: 3px;
    cursor: pointer;
  }
  .gain-value:hover {
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }

  .device-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }

  /* Available device cards */
  .avail-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 10px;
  }
  .avail-card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 120px;
    padding: 14px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    cursor: pointer;
    color: var(--text-primary);
    text-align: left;
    font-size: 13px;
    transition: border-color 0.15s, background 0.15s;
  }
  .avail-card:hover {
    border-color: var(--accent);
    background: var(--bg-secondary);
  }
  .avail-card.added {
    border-color: var(--success, #3fb950);
    opacity: 0.7;
  }
  .avail-card.recommended {
    border-color: var(--color-warning, #d29922);
    background: color-mix(in srgb, var(--color-warning, #d29922) 8%, var(--bg-tertiary));
  }
  .avail-card.recommended:hover {
    background: color-mix(in srgb, var(--color-warning, #d29922) 12%, var(--bg-secondary));
  }
  .avail-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .avail-badges {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }
  .avail-name {
    font-size: 14px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .avail-api {
    font-size: 11px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }
  .avail-path {
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .avail-caps {
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: 12px;
    color: var(--text-muted);
  }
  .scan-level {
    display: flex;
    flex-direction: column;
    gap: 3px;
    margin-top: 4px;
  }
  .scan-bar {
    height: 6px;
    background: var(--bg-primary);
    border-radius: 3px;
    overflow: hidden;
  }
  .scan-fill {
    height: 100%;
    border-radius: 3px;
    background: var(--text-muted);
    transition: width 0.3s;
  }
  .scan-fill.has-signal {
    background: var(--success, #3fb950);
  }
  .scan-value {
    font-size: 11px;
    color: var(--text-muted);
  }
  .scan-error {
    font-size: 11px;
    color: var(--color-danger, #f85149);
  }

  .modal-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 16px;
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
  :global(.danger-action:disabled) {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .cascade-warning {
    margin: 12px 1.5rem 0;
    padding: 12px;
    background: color-mix(in srgb, var(--color-danger) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--color-danger) 30%, transparent);
    border-radius: var(--radius);
    font-size: 13px;
  }
  .cascade-warning strong {
    display: block;
    margin-bottom: 6px;
  }
  .affected-channels {
    margin: 0 0 10px 18px;
    padding: 0;
  }
  .affected-channels li {
    color: var(--text-primary);
    font-weight: 500;
  }
  .cascade-ack {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: 13px;
    color: var(--text-secondary);
  }
</style>
