<script>
  import { onMount } from 'svelte';
  import { Button, Badge, AlertDialog } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import PttCard from './ptt/PttCard.svelte';
  import DialogChangeMethod from './ptt/DialogChangeMethod.svelte';
  import DialogChangeDevice from './ptt/DialogChangeDevice.svelte';
  import { key as methodKey } from './ptt/MethodPicker.svelte';
  import { DESKTOP_METHODS } from './ptt/devices/methodOptions.desktop.js';
  import { createDesktopDeviceSource } from './ptt/devices/desktopDeviceSource.js';

  let items = $state([]);
  let channels = $state([]);
  let available = $state([]);
  let loadingAvail = $state(false);
  let autoScanFailed = $state(false);
  let capabilities = $state(null);
  let deleteTarget = $state(null);
  let deleteOpen = $state(false);

  // Two-dialog flow state
  let dialogMethodOpen = $state(false);
  let dialogDeviceOpen = $state(false);
  let dialogMethodChosen = $state(null);   // method-option chosen from Dialog A
  let dialogContext = $state(null);        // { kind: 'add' | 'edit', item, channelId }
  const deviceSource = createDesktopDeviceSource(api);
  let methodOptionsForPlatform = $derived(DESKTOP_METHODS); // Phase 4 swaps in Android branch

  // Method label lookup retained for PttCard rendering. Keep in sync with
  // DESKTOP_METHODS plus Android-only labels for items the operator may have
  // configured on an Android device but is now viewing on desktop.
  const methodLabels = {
    none: 'None',
    serial_rts: 'Serial RTS',
    serial_dtr: 'Serial DTR',
    gpio: 'GPIO',
    cm108: 'CM108',
    rigctld: 'Hamlib rigctld (CAT)',
  };

  // Split detected hardware into two visually-distinct sections so the
  // eye lands on the right choice first. "Recommended" carries the
  // explicit badge from the server; everything else (unknown USB,
  // platform UARTs, bare gpiochips, demoted AIOC serial) is grouped
  // below as reference material.
  let recommendedDevices = $derived(available.filter(d => d.recommended));
  let otherDevices = $derived(available.filter(d => !d.recommended));

  // PTT only applies to modem-backed channels. KISS-TNC-backed channels
  // (input_device_id == null) have their PTT controlled by the TNC itself,
  // so adding a graywolf-driven PTT row would either be redundant or, worse,
  // key the wrong radio after the operator reassigns channels (issue #110).
  // The backend rejects upserts targeting these channels with HTTP 400; the
  // filter here keeps them out of the dropdown so the rejection is never
  // reached in normal use.
  let modemChannels = $derived(channels.filter(c => c.input_device_id != null));

  function channelName(id) {
    const c = channels.find(c => c.id === id);
    return c ? c.name : null;
  }

  onMount(async () => {
    // Load items, channels, capabilities in parallel. Capabilities gates the
    // GPIO method dropdown and must be loaded before the user opens the dialog.
    await Promise.all([loadItems(), loadChannels(), loadCapabilities()]);
    // Silent auto-scan so `available` is populated before the user opens the
    // Add PTT dialog. Failure is surfaced as an inline notice (see template),
    // not a toast — toasts are reserved for manual "Detect Devices" clicks.
    await autoScanSilently();
  });

  async function loadCapabilities() {
    try {
      const res = await api.get('/ptt/capabilities');
      capabilities = res || { platform_supports_gpio: false };
    } catch {
      // Mock-data fallback in api.js may return null for unknown paths;
      // treat as "unsupported" rather than failing the whole page.
      capabilities = { platform_supports_gpio: false };
    }
  }

  async function autoScanSilently() {
    loadingAvail = true;
    autoScanFailed = false;
    try {
      available = await api.get('/ptt/available') || [];
    } catch (err) {
      console.warn('ptt auto-scan failed:', err);
      available = [];
      autoScanFailed = true;
    } finally {
      loadingAvail = false;
    }
  }

  async function loadItems() {
    items = await api.get('/ptt') || [];
  }

  async function loadChannels() {
    channels = await api.get('/channels') || [];
  }

  async function refreshAvailable() {
    loadingAvail = true;
    autoScanFailed = false;
    try {
      available = await api.get('/ptt/available') || [];
      toasts.success(`Found ${available.length} PTT-capable device(s)`);
    } catch (err) {
      autoScanFailed = true;
      toasts.error(err.message);
    } finally {
      loadingAvail = false;
    }
  }

  function openAddPtt() {
    if (channels.length === 0) {
      toasts.error('Create a channel first on the Channels page');
      return;
    }
    if (modemChannels.length === 0) {
      toasts.error('PTT only applies to modem-backed channels. KISS-TNC channels manage PTT in the TNC itself.');
      return;
    }
    dialogContext = { kind: 'add', channelId: modemChannels[0].id, item: null };
    dialogMethodChosen = null;
    dialogMethodOpen = true;
  }

  function openChangeMethod(item) {
    dialogContext = { kind: 'edit', channelId: item.channel_id, item };
    dialogMethodChosen = methodOptionsForPlatform.find(m =>
      m.wire.method === item.method &&
      (m.wire.ppt_method == null || m.wire.ppt_method === item.ptt_method)
    ) || null;
    dialogMethodOpen = true;
  }

  function openChangeDevice(item) {
    dialogContext = { kind: 'edit', channelId: item.channel_id, item };
    dialogMethodChosen = methodOptionsForPlatform.find(m =>
      m.wire.method === item.method &&
      (m.wire.ppt_method == null || m.wire.ppt_method === item.ptt_method)
    ) || null;
    if (!dialogMethodChosen) {
      // Method isn't in the current platform's option list — bounce
      // through Dialog A so the operator can re-pick.
      dialogMethodOpen = true;
      return;
    }
    dialogDeviceOpen = true;
  }

  function needsDevice(method) {
    if (!method) return false;
    const w = method.wire.method;
    if (w === 'none') return false;
    if (w === 'android' && method.wire.ppt_method === 4) return false;
    return true;
  }

  function handleMethodSaveAndNext(method) {
    dialogMethodChosen = method;
    dialogMethodOpen = false;
    if (needsDevice(method)) {
      dialogDeviceOpen = true;
    } else {
      void persistFromDialogs({ device: null });
    }
  }

  function handleDeviceSave(device) {
    dialogDeviceOpen = false;
    void persistFromDialogs({ device });
  }

  function handleDeviceBack() {
    dialogDeviceOpen = false;
    dialogMethodOpen = true;
  }

  async function persistFromDialogs({ device }) {
    if (!dialogContext) return;
    const m = dialogMethodChosen;
    if (!m) return;
    const body = {
      channel_id: dialogContext.channelId,
      method: m.wire.method,
      device_path: device?.path || '',
      gpio_pin: 0,
      gpio_line: 0,
      invert: false,
    };
    if (m.wire.ppt_method != null) body.ppt_method = m.wire.ppt_method;
    try {
      if (dialogContext.kind === 'edit' && dialogContext.item) {
        await api.put(`/ptt/${dialogContext.item.channel_id}`, body);
        toasts.success('PTT config updated');
      } else {
        await api.post('/ptt', body);
        toasts.success('PTT config created');
      }
      dialogContext = null;
      dialogMethodChosen = null;
      await loadItems();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  function confirmDelete(item) {
    deleteTarget = item;
    deleteOpen = true;
  }

  async function executeDelete() {
    if (!deleteTarget) return;
    try {
      await api.delete(`/ptt/${deleteTarget.channel_id}`);
      toasts.success('PTT config deleted');
      await loadItems();
    } catch (err) {
      toasts.error(err.message);
    } finally {
      deleteOpen = false;
      deleteTarget = null;
    }
  }

  let hasPtt = $derived(items.some(p => p.method !== 'none'));

  function typeBadgeVariant(type) {
    if (type === 'serial') return 'info';
    if (type === 'gpio') return 'info';
    if (type === 'cm108') return 'success';
    return 'default';
  }

  function typeBadgeTitle(type) {
    if (type === 'serial') return 'Serial RTS/DTR PTT — keys the radio via a COM port control line. Use this for USB-serial cables (FTDI, CH340, Prolific) and the serial side of composite adapters like Digirig.';
    if (type === 'gpio') return 'GPIO PTT — keys the radio via a Linux gpiochip pin. Use this on Raspberry Pi and similar SBCs wired directly to the radio.';
    if (type === 'cm108') return 'CM108 HID GPIO PTT — keys the radio via the HID output pin on a CM108-family USB audio chip. Use this for Digirig, AIOC, and other C-Media-based adapters.';
    return '';
  }
</script>

<PageHeader title="PTT Configuration" subtitle="Push-to-talk settings per channel">
  <Button onclick={refreshAvailable} disabled={loadingAvail}>
    {loadingAvail ? 'Scanning...' : 'Detect Devices'}
  </Button>
  <Button variant="primary" onclick={openAddPtt}>+ Add PTT</Button>
</PageHeader>

<!-- PTT readiness -->
<div class="readiness">
  <div class="readiness-item" class:ready={hasPtt}>
    <div class="readiness-icon">{hasPtt ? '●' : '○'}</div>
    <div class="readiness-info">
      <span class="readiness-label">Push-to-Talk</span>
      {#if hasPtt}
        <span class="readiness-detail">{items.filter(p => p.method !== 'none').length} channel(s) with PTT configured</span>
      {:else}
        <span class="readiness-detail needs">No PTT configured — transmit requires a PTT method</span>
      {/if}
    </div>
  </div>
</div>

<!-- Configured PTT devices -->
<div class="section-label">Configured PTT</div>
{#if items.length === 0}
  <div class="empty-state">No PTT configurations. Detect devices below or add one manually.</div>
{:else}
  <div class="device-grid">
    {#each items as item}
      <PttCard
        {item}
        channelName={channelName(item.channel_id)}
        methodLabel={methodLabels[item.method] || item.method}
        onChangeMethod={() => openChangeMethod(item)}
        onChangeDevice={() => openChangeDevice(item)}
        onDelete={confirmDelete}
      />
    {/each}
  </div>
{/if}

<!-- Auto-scan failure notice (inline, not a toast — toasts are reserved for manual clicks) -->
{#if autoScanFailed}
  <div class="section-label" style="margin-top: 24px;">Detected Hardware</div>
  <p class="section-hint">Auto-scan failed — click Detect Devices to retry.</p>
{/if}

<!-- Available devices from hardware scan -->
{#if available.length > 0}
  <!-- Recommended section: prominent, visually distinct -->
  {#if recommendedDevices.length > 0}
    <section class="detected-section detected-recommended" style="margin-top: 24px;">
      <header class="detected-heading">
        <h3 class="detected-title">
          <span class="detected-title-dot" aria-hidden="true"></span>
          Recommended for PTT
        </h3>
        <p class="detected-subtitle">Best match for your hardware — click to configure.</p>
      </header>
      <div class="avail-grid avail-grid-prominent">
        {#each recommendedDevices as dev}
          <button class="avail-card avail-card-recommended" onclick={openAddPtt}>
            <div class="avail-header">
              <strong class="avail-name">{dev.description || dev.name}</strong>
              <Badge variant={typeBadgeVariant(dev.type)} title={typeBadgeTitle(dev.type)}>
                {dev.type}
              </Badge>
            </div>
            <span class="avail-path" title={dev.path}>{dev.path}</span>
            {#if dev.usb_vendor && dev.usb_product}
              <span class="avail-usb">USB {dev.usb_vendor}:{dev.usb_product}</span>
            {/if}
          </button>
        {/each}
      </div>
    </section>
  {/if}

  <!-- Other detected devices: muted, listed for completeness -->
  {#if otherDevices.length > 0}
    <section class="detected-section detected-others" style="margin-top: 44px;">
      <header class="detected-heading">
        <h3 class="detected-title detected-title-muted">Other detected devices</h3>
        <p class="detected-subtitle">Listed for completeness. Pick one only if you know it's correct for your adapter.</p>
      </header>
      <div class="avail-grid avail-grid-compact">
        {#each otherDevices as dev}
          <button class="avail-card avail-card-muted" onclick={openAddPtt}>
            <div class="avail-header">
              <strong class="avail-name">{dev.description || dev.name}</strong>
              <Badge variant={typeBadgeVariant(dev.type)} title={typeBadgeTitle(dev.type)}>
                {dev.type}
              </Badge>
            </div>
            <span class="avail-path" title={dev.path}>{dev.path}</span>
            {#if dev.usb_vendor && dev.usb_product}
              <span class="avail-usb">USB {dev.usb_vendor}:{dev.usb_product}</span>
            {/if}
            {#if dev.warning}
              <span class="avail-warning-text" title={dev.warning}>{dev.warning}</span>
            {/if}
          </button>
        {/each}
      </div>
    </section>
  {/if}
{/if}

<!-- Two-dialog PTT configuration flow -->
<DialogChangeMethod
  bind:open={dialogMethodOpen}
  methods={methodOptionsForPlatform}
  initialWireKey={dialogMethodChosen ? methodKey(dialogMethodChosen) : null}
  onSaveAndNext={handleMethodSaveAndNext}
  onCancel={() => { dialogMethodOpen = false; dialogContext = null; }}
/>

<DialogChangeDevice
  bind:open={dialogDeviceOpen}
  method={dialogMethodChosen}
  {deviceSource}
  initialDevicePath={dialogContext?.item?.device_path || null}
  onSave={handleDeviceSave}
  onBack={handleDeviceBack}
  onCancel={() => { dialogDeviceOpen = false; dialogContext = null; }}
/>

<!-- Delete confirmation -->
<AlertDialog bind:open={deleteOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete PTT Config</AlertDialog.Title>
    <AlertDialog.Description>
      Are you sure you want to delete the PTT configuration for {deleteTarget ? (channelName(deleteTarget.channel_id) || `Channel ${deleteTarget.channel_id}`) : ''}? This cannot be undone.
    </AlertDialog.Description>
    <div class="modal-footer">
      <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action class="danger-action" onclick={executeDelete}>Delete</AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<style>
  /* Readiness */
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
  .field-hint {
    font-size: 12px;
    color: var(--text-muted);
    margin: -6px 0 10px;
  }
  .field-hint code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 11px;
    padding: 1px 4px;
    background: var(--bg-secondary);
    border-radius: 3px;
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

  /* Detected hardware sections */
  .detected-section {
    display: block;
  }
  .detected-heading {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin-bottom: 12px;
    padding-bottom: 8px;
    border-bottom: 1px solid var(--border-color);
  }
  .detected-title {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--success, #3fb950);
  }
  .detected-title-dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--success, #3fb950);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--success, #3fb950) 20%, transparent);
  }
  .detected-title-muted {
    color: var(--text-secondary);
  }
  .detected-subtitle {
    margin: 0;
    font-size: 12px;
    color: var(--text-muted);
  }

  /* Shared card grid/base */
  .avail-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 10px;
  }
  .avail-grid-prominent {
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 12px;
  }
  .avail-grid-compact {
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    gap: 8px;
  }
  .avail-card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 80px;
    padding: 14px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    cursor: pointer;
    color: var(--text-primary);
    text-align: left;
    font-size: 13px;
    transition: border-color 0.15s, background 0.15s, box-shadow 0.15s, transform 0.08s;
  }
  .avail-card:hover {
    border-color: var(--accent);
    background: var(--bg-secondary);
  }
  .avail-card:active {
    transform: translateY(1px);
  }

  /* Recommended variant: visually prominent, success-tinted accent */
  .avail-card-recommended {
    border-left: 3px solid var(--success, #3fb950);
    background: color-mix(in srgb, var(--success, #3fb950) 5%, var(--bg-tertiary));
  }
  .avail-card-recommended .avail-name {
    color: var(--text-primary);
  }
  .avail-card-recommended:hover {
    border-color: var(--success, #3fb950);
    background: color-mix(in srgb, var(--success, #3fb950) 10%, var(--bg-secondary));
    box-shadow: 0 1px 4px color-mix(in srgb, var(--success, #3fb950) 20%, transparent);
  }

  /* Muted variant: lower visual weight so the eye skips them. Fixed
     height so every row is the same regardless of whether a card
     carries an advisory warning line — mixed heights make the grid
     feel busy and draw the eye unpredictably. */
  .avail-card-muted {
    height: 104px;
    padding: 10px 12px;
    background: var(--bg-secondary);
    opacity: 0.82;
    overflow: hidden;
  }
  .avail-card-muted .avail-name {
    font-size: 13px;
    font-weight: 500;
    color: var(--text-secondary);
  }
  .avail-card-muted .avail-path {
    font-size: 11px;
  }
  .avail-card-muted:hover {
    opacity: 1;
    background: var(--bg-tertiary);
    border-color: var(--border-color);
  }

  .avail-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
  }
  .avail-name {
    font-size: 14px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .avail-path {
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .avail-usb {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text-muted);
  }
  /* Inline warning text for muted cards — quiet, advisory, not a
     yellow stripe that steals attention. Clamped to fit the fixed
     card height; full text is available via the native title tooltip. */
  .avail-warning-text {
    font-size: 11px;
    color: var(--text-muted);
    font-style: italic;
    margin-top: 2px;
    line-height: 1.4;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
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

  /* rigctld Test Connection UI */
  .rigctld-test-row {
    display: flex;
    gap: 8px;
    margin-bottom: 8px;
  }
  .rigctld-result {
    min-height: 22px;
    margin-bottom: 4px;
  }
  .rigctld-badge {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 10px;
    border-radius: 999px;
    font-size: 12px;
    font-weight: 500;
    border: 1px solid transparent;
  }
  .rigctld-badge.testing {
    color: var(--text-secondary);
    background: var(--bg-secondary);
    border-color: var(--border-color);
  }
  .rigctld-badge.ok {
    --badge-c: var(--success, #3fb950);
    color: var(--badge-c);
    background: color-mix(in srgb, var(--badge-c) 12%, transparent);
    border-color: color-mix(in srgb, var(--badge-c) 40%, transparent);
  }
  .rigctld-badge.err {
    --badge-c: var(--color-danger, #f85149);
    color: var(--badge-c);
    background: color-mix(in srgb, var(--badge-c) 12%, transparent);
    border-color: color-mix(in srgb, var(--badge-c) 40%, transparent);
  }
  .rigctld-spinner {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    border: 2px solid currentColor;
    border-right-color: transparent;
    display: inline-block;
    animation: rigctld-spin 0.7s linear infinite;
    vertical-align: -2px;
    margin-right: 6px;
  }
  @keyframes rigctld-spin {
    to { transform: rotate(360deg); }
  }
</style>
