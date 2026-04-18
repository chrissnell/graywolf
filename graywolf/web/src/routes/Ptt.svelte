<script>
  import { onMount, onDestroy } from 'svelte';
  import { Button, Input, Select, Badge, Toggle, AlertDialog } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';

  let items = $state([]);
  let channels = $state([]);
  let available = $state([]);
  let loadingAvail = $state(false);
  let autoScanFailed = $state(false);
  let capabilities = $state(null);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state(emptyForm());
  let errors = $state({});
  let deleteTarget = $state(null);
  let deleteOpen = $state(false);

  // GPIO line selector state
  let gpioLines = $state([]);
  let loadingGpioLines = $state(false);
  let gpioLinesLoadError = $state(null);
  let gpioLinesReqId = $state(0);
  let gpioLinesDebounce;

  onDestroy(() => {
    if (gpioLinesDebounce) clearTimeout(gpioLinesDebounce);
  });

  // All method values. GPIO is filtered in/out dynamically via methodOptions.
  const allMethods = [
    { value: 'none', label: 'None' },
    { value: 'serial_rts', label: 'Serial RTS' },
    { value: 'serial_dtr', label: 'Serial DTR' },
    { value: 'gpio', label: 'GPIO' },
    { value: 'cm108', label: 'CM108' },
    { value: 'rigctld', label: 'Hamlib rigctld (CAT)' },
  ];

  const methodLabels = Object.fromEntries(allMethods.map(o => [o.value, o.label]));

  let platformSupportsGpio = $derived(capabilities?.platform_supports_gpio === true);
  let editingGpio = $derived(editing?.method === 'gpio');
  // Keep GPIO visible when the platform supports it OR the user is editing
  // an existing GPIO config (prevents silent data loss if the gpiochip
  // temporarily disappears — kernel module unload, SD-card reshuffle,
  // container without bind-mount).
  let methodOptions = $derived(
    (platformSupportsGpio || editingGpio)
      ? allMethods
      : allMethods.filter(m => m.value !== 'gpio')
  );
  let hasGpioDevices = $derived(available.some(d => d.type === 'gpio'));

  // Split detected hardware into two visually-distinct sections so the
  // eye lands on the right choice first. "Recommended" carries the
  // explicit badge from the server; everything else (unknown USB,
  // platform UARTs, bare gpiochips, demoted AIOC serial) is grouped
  // below as reference material.
  let recommendedDevices = $derived(available.filter(d => d.recommended));
  let otherDevices = $derived(available.filter(d => !d.recommended));

  let channelOptions = $derived(
    channels.map(c => ({ value: String(c.id), label: `${c.name} (ch ${c.id})` }))
  );

  function channelName(id) {
    const c = channels.find(c => c.id === id);
    return c ? c.name : null;
  }

  const cm108GpioPins = ['1', '2', '3', '4', '5', '6', '7', '8'];

  // Default gpio_pin to 3 when switching to cm108 with an out-of-range value
  $effect(() => {
    if (form.method === 'cm108' && !cm108GpioPins.includes(form.gpio_pin)) {
      form.gpio_pin = '3';
    }
  });

  // Scrub fields that don't apply when entering/leaving rigctld/gpio. Prevents
  // stale device_path/gpio/invert from smuggling into a rigctld save, prevents
  // stale rigctld_host/port from leaking back in if the user flips away and
  // back, and seeds gpio_line to '0' on a fresh transition into gpio (but not
  // when opening an existing GPIO config for edit — openEdit sets lastMethod
  // to the current method so no transition fires).
  let lastMethod = $state(null);
  $effect(() => {
    const m = form.method;
    if (m === lastMethod) return;
    if (m === 'rigctld') {
      form.gpio_pin = '0';
      form.invert = false;
      form.device_path = '';
    } else if (lastMethod === 'rigctld') {
      form.rigctld_host = 'localhost';
      form.rigctld_port = 4532;
      form.device_path = '';
    }
    if (lastMethod !== 'gpio' && m === 'gpio') {
      form.gpio_line = '0';
      // Scrub a stale device_path that isn't a gpiochip. Common trigger:
      // user clicks a detected /dev/ttyACM* (setting method=serial_rts),
      // then flips the dropdown to gpio. Without this, the load-lines
      // $effect fires against a non-gpiochip path and the server 400s.
      if (form.device_path && !form.device_path.startsWith('/dev/gpiochip')) {
        form.device_path = '';
      }
    }
    lastMethod = m;
  });

  // Load GPIO lines whenever method is gpio and a chip path is present.
  // Race-guarded + debounced inside loadGpioLines.
  $effect(() => {
    if (form.method === 'gpio' && form.device_path) {
      loadGpioLines(form.device_path);
    } else {
      gpioLines = [];
      gpioLinesLoadError = null;
      loadingGpioLines = false;
    }
  });

  // Test Connection FSM for rigctld. Kinds: 'idle' | 'testing' | 'success' | 'error'.
  let testState = $state({ kind: 'idle' });

  // Reset test result to idle whenever the target or modal context changes.
  // Symmetric for success/error so stale badges never linger across edits.
  $effect(() => {
    // Touch dependencies so $effect re-runs on any change.
    void form.method;
    void form.rigctld_host;
    void form.rigctld_port;
    void modalOpen;
    if (testState.kind !== 'idle' && testState.kind !== 'testing') {
      testState = { kind: 'idle' };
    }
  });

  function emptyForm() {
    return {
      channel_id: '',
      method: 'none',
      device_path: '',
      gpio_pin: '0',
      gpio_line: '0',
      invert: false,
      rigctld_host: 'localhost',
      rigctld_port: 4532,
    };
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

  // Fetches gpiochip lines for the given chip path with a 150ms debounce and
  // a monotonic request-id guard so rapid chip-switching discards stale
  // out-of-order responses.
  async function loadGpioLines(chipPath) {
    clearTimeout(gpioLinesDebounce);
    if (!chipPath) {
      gpioLines = [];
      gpioLinesLoadError = null;
      loadingGpioLines = false;
      return;
    }
    gpioLinesDebounce = setTimeout(async () => {
      const myId = ++gpioLinesReqId;
      loadingGpioLines = true;
      gpioLinesLoadError = null;
      try {
        // Encoded {chip} path parameter — see the listGpioLines
        // handler docblock for the URL-encoding contract.
        const result = await api.get('/ptt/gpio-chips/' + encodeURIComponent(chipPath) + '/lines');
        if (myId !== gpioLinesReqId) return;
        gpioLines = Array.isArray(result) ? result : [];
      } catch (e) {
        if (myId !== gpioLinesReqId) return;
        gpioLines = [];
        gpioLinesLoadError = e?.message || 'Failed to load GPIO lines';
      } finally {
        if (myId === gpioLinesReqId) loadingGpioLines = false;
      }
    }, 150);
  }

  function openCreate() {
    if (channels.length === 0) {
      toasts.error('Create a channel first on the Channels page');
      return;
    }
    editing = null;
    form = emptyForm();
    form.channel_id = String(channels[0].id);
    lastMethod = form.method;
    errors = {};
    modalOpen = true;
  }

  function openEdit(item) {
    editing = item;
    let rigctldHost = 'localhost';
    let rigctldPort = 4532;
    if (item.method === 'rigctld' && item.device_path && !item.device_path.includes('[')) {
      // Split on the LAST ':' so hostnames that (shouldn't but might) contain
      // a colon don't derail parsing. IPv6 literals rejected via '[' guard.
      const idx = item.device_path.lastIndexOf(':');
      if (idx > 0) {
        const h = item.device_path.slice(0, idx);
        const p = parseInt(item.device_path.slice(idx + 1), 10);
        if (h) rigctldHost = h;
        if (Number.isInteger(p) && p >= 1 && p <= 65535) rigctldPort = p;
      }
    }
    form = {
      channel_id: String(item.channel_id),
      method: item.method,
      device_path: item.device_path || '',
      gpio_pin: String(item.gpio_pin || 0),
      gpio_line: String(item.gpio_line || 0),
      invert: !!item.invert,
      rigctld_host: rigctldHost,
      rigctld_port: rigctldPort,
    };
    // Prevent the method-change $effect from clobbering freshly loaded
    // rigctld/gpio values on open — lastMethod is already this method, so no
    // transition fires and gpio_line is preserved as loaded.
    lastMethod = item.method;
    errors = {};
    modalOpen = true;
  }

  function openCreateFromAvail(dev) {
    if (channels.length === 0) {
      toasts.error('Create a channel first on the Channels page');
      return;
    }
    editing = null;
    const method = dev.type === 'gpio' ? 'gpio'
      : dev.type === 'cm108' ? 'cm108'
      : 'serial_rts';
    form = {
      channel_id: String(channels[0].id),
      method,
      device_path: dev.path,
      gpio_pin: method === 'cm108' ? '3' : '0',
      // Line 0 is a real GPIO line on every chip; use as the starting point.
      // The user must explicitly pick their wired line from the dropdown.
      gpio_line: '0',
      invert: false,
      rigctld_host: 'localhost',
      rigctld_port: 4532,
    };
    lastMethod = method;
    errors = {};
    modalOpen = true;
  }

  function handleModalClose() {
    editing = null;
    form = emptyForm();
    lastMethod = form.method;
    errors = {};
  }

  function validate() {
    const e = {};
    if (form.method === 'rigctld') {
      const host = (form.rigctld_host || '').trim();
      if (!host) {
        e.rigctld_host = 'Hostname or IPv4 address required';
      } else if (host.includes(':')) {
        e.rigctld_host = 'IPv6 not supported, use hostname or IPv4';
      }
      const port = Number(form.rigctld_port);
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        e.rigctld_port = 'Port must be an integer between 1 and 65535';
      }
    } else if (form.method !== 'none') {
      if (!form.device_path.trim()) e.device_path = 'Device path required';
    }
    if (form.method === 'gpio' && Array.isArray(gpioLines) && gpioLines.length > 0) {
      const line = gpioLines.find(l => String(l.offset) === String(form.gpio_line));
      if (line?.used) {
        e.gpio_line = `Line ${line.offset} is claimed by ${line.consumer || 'another driver'}. Pick another line.`;
      }
    }
    errors = e;
    return Object.keys(e).length === 0;
  }

  async function handleSave() {
    if (!validate()) return;
    const data = {
      ...form,
      channel_id: parseInt(form.channel_id, 10),
      gpio_pin: parseInt(form.gpio_pin, 10),
      gpio_line: parseInt(form.gpio_line, 10),
      invert: !!form.invert,
    };
    // For rigctld, unconditionally pack host:port into device_path and zero
    // out unused fields. Don't trust that the method-change $effect already
    // ran — belt + suspenders, per plan.
    if (form.method === 'rigctld') {
      data.device_path = `${form.rigctld_host}:${form.rigctld_port}`;
      data.gpio_pin = 0;
      data.gpio_line = 0;
      data.invert = false;
    }
    // Strip UI-only keys that shouldn't hit the API.
    delete data.rigctld_host;
    delete data.rigctld_port;
    try {
      if (editing) {
        await api.put(`/ptt/${editing.channel_id}`, data);
        toasts.success('PTT config updated');
      } else {
        await api.post('/ptt', data);
        toasts.success('PTT config created');
      }
      // Reset test result on successful save.
      testState = { kind: 'idle' };
      modalOpen = false;
      await loadItems();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  // Test Connection: calls POST /api/ptt/test-rigctld. We use raw fetch (not
  // the api helper) because the helper silently falls back to mock data on
  // network errors — the wrong behavior for a diagnostic test button.
  async function testConnection() {
    if (testState.kind === 'testing') return;
    if (!canTest) return;
    testState = { kind: 'testing' };
    try {
      const res = await fetch('/api/ptt/test-rigctld', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          host: form.rigctld_host.trim(),
          port: Number(form.rigctld_port),
        }),
      });
      if (!res.ok) {
        // Try to parse body for a message, else fall back to statusText.
        let msg = res.statusText || `HTTP ${res.status}`;
        try {
          const body = await res.json();
          if (body && typeof body.message === 'string' && body.message) msg = body.message;
          else if (body && typeof body.error === 'string' && body.error) msg = body.error;
        } catch { /* non-JSON body */ }
        testState = { kind: 'error', message: msg };
        return;
      }
      const body = await res.json();
      if (body && body.ok) {
        const latency = Number.isFinite(body.latency_ms) ? body.latency_ms : 0;
        testState = { kind: 'success', latencyMs: latency };
      } else {
        const msg = (body && body.message) || 'rigctld reported failure';
        testState = { kind: 'error', message: msg };
      }
    } catch (err) {
      testState = { kind: 'error', message: err?.message || 'Network error' };
    }
  }

  let portValid = $derived.by(() => {
    const p = Number(form.rigctld_port);
    return Number.isInteger(p) && p >= 1 && p <= 65535;
  });
  let hostValid = $derived.by(() => {
    const h = (form.rigctld_host || '').trim();
    return h.length > 0 && !h.includes(':');
  });
  let canTest = $derived(hostValid && portValid);
  let testDisabledReason = $derived.by(() => {
    if (testState.kind === 'testing') return 'Test in progress';
    if (!hostValid) {
      if (!(form.rigctld_host || '').trim()) return 'Enter a hostname to test';
      return 'Remove the colon from the hostname (IPv6 not supported)';
    }
    if (!portValid) return 'Enter a valid port (1–65535)';
    return '';
  });

  function handleRigctldPortBlur() {
    // Restore default if user cleared the field or typed a non-number.
    if (form.rigctld_port === '' || form.rigctld_port === null || form.rigctld_port === undefined) {
      form.rigctld_port = 4532;
      return;
    }
    const n = Number(form.rigctld_port);
    if (!Number.isFinite(n)) {
      form.rigctld_port = 4532;
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

  function truncatePath(p, max = 40) {
    if (!p || p.length <= max) return p || '—';
    return '...' + p.slice(-(max - 3));
  }

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
  <Button variant="primary" onclick={openCreate}>+ Add PTT</Button>
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
      <div class="device-card">
        <div class="device-header">
          <span class="device-name">{channelName(item.channel_id) || `Channel ${item.channel_id}`}</span>
          <div class="device-badges">
            <Badge variant={item.method === 'none' ? 'default' : 'success'}>
              {methodLabels[item.method] || item.method}
            </Badge>
          </div>
        </div>
        <div class="device-details">
          {#if item.method !== 'none'}
            <div class="detail-row">
              <span class="detail-label">Device</span>
              <span class="detail-value" title={item.device_path}>{truncatePath(item.device_path)}</span>
            </div>
          {/if}
          {#if item.method === 'cm108'}
            <div class="detail-row">
              <span class="detail-label">GPIO Pin</span>
              <span class="detail-value">GPIO {item.gpio_pin} (pin {item.gpio_pin + 10})</span>
            </div>
          {/if}
          {#if item.method === 'gpio'}
            <div class="detail-row">
              <span class="detail-label">GPIO Line</span>
              <span class="detail-value">Line {item.gpio_line ?? 0}</span>
            </div>
          {/if}
          {#if item.method === 'none'}
            <div class="detail-row">
              <span class="detail-label">Status</span>
              <span class="detail-value muted">No PTT method set</span>
            </div>
          {/if}
        </div>
        <div class="device-actions">
          <Button variant="ghost" onclick={() => openEdit(item)}>Edit</Button>
          <Button variant="danger" onclick={() => confirmDelete(item)}>Delete</Button>
        </div>
      </div>
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
          <button class="avail-card avail-card-recommended" onclick={() => openCreateFromAvail(dev)}>
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
    <section class="detected-section detected-others" style="margin-top: 24px;">
      <header class="detected-heading">
        <h3 class="detected-title detected-title-muted">Other detected devices</h3>
        <p class="detected-subtitle">Listed for completeness. Pick one only if you know it's correct for your adapter.</p>
      </header>
      <div class="avail-grid avail-grid-compact">
        {#each otherDevices as dev}
          <button class="avail-card avail-card-muted" onclick={() => openCreateFromAvail(dev)}>
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
              <span class="avail-warning-text">{dev.warning}</span>
            {/if}
          </button>
        {/each}
      </div>
    </section>
  {/if}
{/if}

<!-- Add/Edit modal -->
<Modal bind:open={modalOpen} title={editing ? 'Edit PTT Config' : 'New PTT Config'} onClose={handleModalClose}>
    <FormField label="Channel" id="ptt-ch"
      hint="Radio channel this PTT controls. Defined on the Channels page.">
      <Select id="ptt-ch" bind:value={form.channel_id} options={channelOptions} />
    </FormField>
    <FormField label="Method" id="ptt-method">
      <Select id="ptt-method" bind:value={form.method} options={methodOptions} />
    </FormField>
    {#if form.method !== 'none' && form.method !== 'rigctld'}
      <FormField label="Device Path" error={errors.device_path} id="ptt-dev">
        <Input id="ptt-dev" bind:value={form.device_path} placeholder="Select a detected device or enter path" />
      </FormField>
      {#if form.method === 'serial_rts' || form.method === 'serial_dtr'}
        <p class="field-hint">On macOS use <code>/dev/cu.usbserial-*</code>, not <code>/dev/tty.usbserial-*</code> (the tty.* variant blocks forever on DCD).</p>
      {/if}
    {/if}
    {#if form.method === 'gpio'}
      {#if platformSupportsGpio && !hasGpioDevices}
        <p class="field-hint">No GPIO chips detected. Check that you have access to <code>/dev/gpiochip*</code> (typically the <code>gpio</code> group on Raspberry Pi OS).</p>
      {/if}
      {#if loadingGpioLines}
        <FormField label="GPIO Line" id="ptt-gpio-line"
          hint="Loading lines from the selected gpiochip…">
          <div aria-busy="true">
            <Select id="ptt-gpio-line" disabled
              value="__loading__"
              options={[{ value: '__loading__', label: 'Loading lines…' }]} />
          </div>
        </FormField>
      {:else if gpioLines.length > 0}
        <FormField label="GPIO Line" error={errors.gpio_line} id="ptt-gpio-line"
          hint="Select the GPIO line wired to your radio's PTT input. Lines marked 'in use' are claimed by a kernel driver.">
          <Select id="ptt-gpio-line" bind:value={form.gpio_line}
            options={gpioLines.map(l => ({
              value: String(l.offset),
              label: l.used
                ? `Line ${l.offset} — ${l.name || `Line ${l.offset}`} [in use: ${l.consumer || 'unknown'}]`
                : `Line ${l.offset} — ${l.name || `Line ${l.offset}`}`,
            }))} />
        </FormField>
      {:else if gpioLinesLoadError}
        <FormField label="GPIO Line" error={errors.gpio_line} id="ptt-gpio-line"
          hint={`Couldn't load lines: ${gpioLinesLoadError}. Enter the line number manually.`}>
          <Input id="ptt-gpio-line" bind:value={form.gpio_line} type="number" min={0} />
        </FormField>
      {:else if form.device_path}
        <FormField label="GPIO Line" error={errors.gpio_line} id="ptt-gpio-line"
          hint="No lines reported by this chip — enter the line number manually.">
          <Input id="ptt-gpio-line" bind:value={form.gpio_line} type="number" min={0} />
        </FormField>
      {:else}
        <FormField label="GPIO Line" error={errors.gpio_line} id="ptt-gpio-line"
          hint="Enter a device path above to see available lines.">
          <Input id="ptt-gpio-line" bind:value={form.gpio_line} type="number" min={0} />
        </FormField>
      {/if}
    {/if}
    {#if form.method === 'cm108'}
      <FormField label="GPIO Pin" id="ptt-cm108-gpio"
        hint="GPIO 3 is used by nearly all homebrew designs and commercial products (e.g., AIOC). Only change this if you know your adapter uses a different pin.">
        <Select id="ptt-cm108-gpio" bind:value={form.gpio_pin} options={[
          { value: '1', label: 'GPIO 1 (pin 11)' },
          { value: '2', label: 'GPIO 2 (pin 12) — not on CM108AH/B' },
          { value: '3', label: 'GPIO 3 (pin 13) — most common' },
          { value: '4', label: 'GPIO 4 (pin 14)' },
          { value: '5', label: 'GPIO 5 — CM109/CM119 only' },
          { value: '6', label: 'GPIO 6 — CM109/CM119 only' },
          { value: '7', label: 'GPIO 7 — CM109/CM119 only' },
          { value: '8', label: 'GPIO 8 — CM109/CM119 only' },
        ]} />
      </FormField>
    {/if}
    {#if form.method === 'rigctld'}
      <FormField label="Hostname" error={errors.rigctld_host} id="ptt-rigctld-host">
        <Input id="ptt-rigctld-host" bind:value={form.rigctld_host} placeholder="localhost" />
      </FormField>
      <FormField label="Port" error={errors.rigctld_port} id="ptt-rigctld-port">
        <Input id="ptt-rigctld-port"
               type="number"
               min={1}
               max={65535}
               bind:value={form.rigctld_port}
               onblur={handleRigctldPortBlur} />
      </FormField>
      <div class="rigctld-test-row">
        <Button
          onclick={testConnection}
          disabled={!canTest || testState.kind === 'testing'}
          aria-disabled={(!canTest || testState.kind === 'testing') ? 'true' : 'false'}
          aria-busy={testState.kind === 'testing' ? 'true' : 'false'}
          title={testDisabledReason || 'Probe the rigctld daemon without keying the radio'}
        >
          {#if testState.kind === 'testing'}
            <span class="rigctld-spinner" aria-hidden="true"></span>
            Testing…
          {:else}
            Test Connection
          {/if}
        </Button>
      </div>
      <div
        class="rigctld-result"
        role="status"
        aria-live="polite"
        aria-atomic="true"
      >
        {#if testState.kind === 'testing'}
          <span class="rigctld-badge testing">Testing connection…</span>
        {:else if testState.kind === 'success'}
          <span class="rigctld-badge ok">Success: Connected ({testState.latencyMs} ms)</span>
        {:else if testState.kind === 'error'}
          <span class="rigctld-badge err">Failed: {testState.message}</span>
        {/if}
      </div>
      <p class="field-hint">
        Commands your radio's PTT over CAT via a running <code>rigctld</code>. Polarity inversion does not apply. IPv4 / hostnames only.
      </p>
    {/if}
    {#if form.method !== 'none' && form.method !== 'rigctld'}
      <FormField label="Invert Polarity" id="ptt-invert">
        <Toggle bind:checked={form.invert} label="Key radio on LOW instead of HIGH" />
      </FormField>
    {/if}
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" onclick={handleSave}>{editing ? 'Save' : 'Create'}</Button>
    </div>
</Modal>

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
  .detail-value.muted {
    color: var(--text-muted);
    font-style: italic;
    font-family: inherit;
  }
  .device-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
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
    color: var(--text-muted);
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

  /* Muted variant: lower visual weight so the eye skips them */
  .avail-card-muted {
    min-height: 68px;
    padding: 10px 12px;
    background: var(--bg-secondary);
    opacity: 0.82;
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
     yellow stripe that steals attention. */
  .avail-warning-text {
    font-size: 11px;
    color: var(--text-muted);
    font-style: italic;
    margin-top: 2px;
    line-height: 1.4;
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
