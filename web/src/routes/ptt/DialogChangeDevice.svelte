<!-- web/src/routes/ptt/DialogChangeDevice.svelte -->
<script>
  import { Button, Input, Select, Toggle } from '@chrissnell/chonky-ui';
  import Modal from '../../components/Modal.svelte';
  import FormField from '../../components/FormField.svelte';
  import DevicePicker from './DevicePicker.svelte';
  import { api } from '../../lib/api.js';
  import { resolveDevice } from './devicePayload.js';

  let {
    open = $bindable(),
    method,
    deviceSource,
    initialDevicePath,
    initialGpioLine,     // for gpio method
    initialGpioPin,      // for cm108 method
    initialDtrInvert,
    initialInvert,       // any keying method
    allowManualEntry = true, // desktop: let the operator type a udev path (GH #511)
    onSave,              // (payload) => void; payload: { device, gpio_line?, gpio_pin?, invert }
    onBack,
    onCancel,
  } = $props();

  let devices = $state([]);
  let loading = $state(false);
  let error = $state(null);
  let selectedPath = $state(null);
  let gpioLines = $state([]);
  let loadingGpioLines = $state(false);
  let gpioLineSel = $state('0');
  let gpioPinSel = $state('3');
  let invert = $state(false);
  let dtrInvert = $state(false);

  // Manual free-form device path (GH #511): udev-renamed devices such as
  // /dev/aioc-aprs-ptt never appear in enumeration, so the operator needs
  // to type the path directly instead of only picking a detected card.
  let manualMode = $state(false);
  let manualPath = $state('');
  let probe = $state(null);      // { exists, char_device, message } | null
  let probing = $state(false);
  // Set while the open effect seeds state, so refresh()'s mode-detection
  // only runs on open — a manual Refresh click must not yank the operator
  // out of the mode they chose.
  let initializing = false;

  let wireMethod = $derived(method?.wire?.method);
  let isGpio = $derived(wireMethod === 'gpio');
  let isCm108 = $derived(wireMethod === 'cm108');

  // The path in effect regardless of mode — drives GPIO line loading,
  // the save payload, and the save-enable check.
  let effectivePath = $derived(manualMode ? manualPath.trim() : selectedPath);

  let wasOpen = false;
  $effect(() => {
    if (open && !wasOpen) {
      selectedPath = initialDevicePath || null;
      manualPath = initialDevicePath || '';
      manualMode = false;
      probe = null;
      probing = false;
      gpioLineSel = String(initialGpioLine ?? 0);
      gpioPinSel = String(initialGpioPin ?? 3);
      invert = !!initialInvert;
      dtrInvert = !!initialDtrInvert;
      wasOpen = true;
      initializing = true;
      void refresh();
    } else if (!open) {
      wasOpen = false;
    }
  });

  // Whenever the effective gpio chip path changes, refresh its line list.
  // In manual mode the path changes per keystroke, so debounce there (and
  // loadGpioLines is stale-guarded) to avoid a burst of requests and
  // out-of-order results. A discrete list selection loads immediately.
  $effect(() => {
    if (!(isGpio && effectivePath)) { gpioLines = []; return; }
    const path = effectivePath;
    if (!manualMode) { void loadGpioLines(path); return; }
    const handle = setTimeout(() => { void loadGpioLines(path); }, 400);
    return () => clearTimeout(handle);
  });

  // Non-blocking advisory for a manually-entered path. Debounced so we
  // don't probe on every keystroke. Never gates Save — a not-yet-present
  // udev name is expressly allowed.
  $effect(() => {
    if (!manualMode) { probe = null; return; }
    const p = manualPath.trim();
    if (!p) { probe = null; probing = false; return; }
    probing = true;
    const handle = setTimeout(() => { void checkDevice(p); }, 400);
    return () => clearTimeout(handle);
  });

  async function checkDevice(path) {
    try {
      const res = await api.post('/ptt/check-device', { device_path: path });
      // Ignore a stale result if the operator kept typing.
      if (manualMode && manualPath.trim() === path) probe = res || null;
    } catch {
      if (manualMode && manualPath.trim() === path) probe = null;
    } finally {
      if (manualMode && manualPath.trim() === path) probing = false;
    }
  }

  function enterManualMode() {
    manualMode = true;
    // Seed from any list selection so switching modes doesn't lose work.
    if (!manualPath && selectedPath) manualPath = selectedPath;
  }

  function leaveManualMode() {
    manualMode = false;
    probe = null;
    probing = false;
  }

  let gpioLoadSeq = 0;
  async function loadGpioLines(chipPath) {
    const seq = ++gpioLoadSeq;
    loadingGpioLines = true;
    try {
      const result = await api.get('/ptt/gpio-chips/' + encodeURIComponent(chipPath) + '/lines');
      if (seq === gpioLoadSeq) gpioLines = Array.isArray(result) ? result : [];
    } catch {
      if (seq === gpioLoadSeq) gpioLines = [];
    } finally {
      if (seq === gpioLoadSeq) loadingGpioLines = false;
    }
  }

  async function refresh() {
    if (!method) return;
    loading = true;
    error = null;
    try {
      devices = await deviceSource.list(method);
    } catch (e) {
      devices = [];
      error = e?.message || 'Failed to list devices';
    } finally {
      loading = false;
      // On open only: if we're editing a path that enumeration doesn't
      // know about (a previously-saved manual/udev path), start in manual
      // mode so the operator sees and can edit their path rather than an
      // empty picker.
      if (initializing) {
        if (allowManualEntry && initialDevicePath &&
            !devices.some(d => d.path === initialDevicePath)) {
          manualMode = true;
        }
        initializing = false;
      }
    }
  }

  function buildPayload() {
    return {
      device: resolveDevice(devices, effectivePath, wireMethod),
      gpio_line: isGpio ? Number(gpioLineSel) : undefined,
      gpio_pin: isCm108 ? Number(gpioPinSel) : undefined,
      invert,
      dtr_invert: wireMethod === 'serial_rts_dtr' && dtrInvert,
    };
  }

  let canSave = $derived(!!effectivePath);

  const cm108Pins = [
    { value: '1', label: 'GPIO 1 (pin 11)' },
    { value: '2', label: 'GPIO 2 (pin 12) — not on CM108AH/B' },
    { value: '3', label: 'GPIO 3 (pin 13) — most common' },
    { value: '4', label: 'GPIO 4 (pin 14)' },
    { value: '5', label: 'GPIO 5 — CM109/CM119 only' },
    { value: '6', label: 'GPIO 6 — CM109/CM119 only' },
    { value: '7', label: 'GPIO 7 — CM109/CM119 only' },
    { value: '8', label: 'GPIO 8 — CM109/CM119 only' },
  ];
</script>

<Modal bind:open title="Change PTT Device" onClose={onCancel}>
  {#if loading}
    <div class="state">Loading devices…</div>
  {:else if error}
    <div class="state error">{error}</div>
  {:else if manualMode}
    <FormField label="Device Path" id="dlg-manual-path"
      hint="Full path to the PTT device, e.g. /dev/aioc-aprs-ptt. A stable udev name is allowed even if the device isn't plugged in yet.">
      <Input id="dlg-manual-path" bind:value={manualPath}
        placeholder="/dev/aioc-aprs-ptt" spellcheck={false} autocomplete="off" />
    </FormField>
    {#if manualPath.trim() && (probing || probe)}
      <p class="probe"
        class:ok={!probing && probe && probe.exists && probe.char_device}
        class:bad={!probing && probe && probe.exists && !probe.char_device}
        class:warn={!probing && probe && !probe.exists}>
        {#if probing}
          Checking path…
        {:else if probe && probe.exists && probe.char_device}
          Device present.
        {:else if probe && probe.exists}
          Path exists but is not a character device — double-check it points at the PTT device.
        {:else}
          Not present yet. That's fine for a udev-named device — Graywolf will use it once it appears.
        {/if}
      </p>
    {/if}
    {#if allowManualEntry}
      <button type="button" class="mode-toggle" onclick={leaveManualMode}>‹ Choose from detected devices</button>
    {/if}
  {:else}
    <DevicePicker
      {devices}
      {selectedPath}
      onSelect={(d) => { selectedPath = d.path || null; }}
    />
    {#if allowManualEntry}
      <button type="button" class="mode-toggle" onclick={enterManualMode}>Enter a path manually…</button>
    {/if}
  {/if}

  {#if isGpio && effectivePath}
    {#if loadingGpioLines}
      <FormField label="GPIO Line" id="dlg-gpio-line" hint="Loading lines…">
        <Select id="dlg-gpio-line" disabled value="__loading__"
          options={[{ value: '__loading__', label: 'Loading lines…' }]} />
      </FormField>
    {:else if gpioLines.length > 0}
      <FormField label="GPIO Line" id="dlg-gpio-line">
        <Select id="dlg-gpio-line" bind:value={gpioLineSel}
          options={gpioLines.map(l => ({
            value: String(l.offset),
            label: l.used
              ? `Line ${l.offset} — ${l.name || `Line ${l.offset}`} [in use: ${l.consumer || 'unknown'}]`
              : `Line ${l.offset} — ${l.name || `Line ${l.offset}`}`,
          }))} />
      </FormField>
    {:else}
      <FormField label="GPIO Line" id="dlg-gpio-line" hint="No lines reported — enter manually">
        <Input id="dlg-gpio-line" bind:value={gpioLineSel} type="number" min={0} />
      </FormField>
    {/if}
  {/if}

  {#if isCm108}
    <FormField label="GPIO Pin" id="dlg-cm108-pin"
      hint="GPIO 3 is used by nearly all homebrew designs and commercial products.">
      <Select id="dlg-cm108-pin" bind:value={gpioPinSel} options={cm108Pins} />
    </FormField>
  {/if}

  {#if wireMethod !== 'none' && wireMethod !== 'rigctld'}
    <FormField label={wireMethod === 'serial_rts_dtr' ? 'RTS Polarity' : 'Invert Polarity'} id="dlg-invert">
      <Toggle bind:checked={invert} label="Key radio on LOW instead of HIGH" />
    </FormField>
  {/if}

  {#if wireMethod === 'serial_rts_dtr'}
    <FormField label="DTR Polarity" id="dlg-dtr-invert"
      hint="Each line reverses on receive. For Dire Wolf RTS -DTR: RTS toggle off, DTR toggle on.">
      <Toggle bind:checked={dtrInvert} label="Key DTR on LOW instead of HIGH" />
    </FormField>
  {/if}

  <div class="modal-actions">
    <Button onclick={onBack}>‹ Back</Button>
    <Button onclick={refresh}>Refresh</Button>
    <Button variant="primary" disabled={!canSave} onclick={() => onSave(buildPayload())}>
      Save
    </Button>
  </div>
</Modal>

<style>
  .state { padding: 16px; text-align: center; color: var(--text-secondary, #555); }
  .state.error { color: #b91c1c; }
  .modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
  .mode-toggle {
    margin-top: 8px;
    padding: 0;
    background: none;
    border: none;
    color: var(--accent, #3b82f6);
    font-size: 13px;
    cursor: pointer;
  }
  .mode-toggle:hover { text-decoration: underline; }
  .probe {
    margin: 6px 0 0;
    font-size: 12px;
    line-height: 1.4;
    color: var(--text-secondary, #555);
  }
  .probe.ok { color: var(--success, #3fb950); }
  .probe.bad { color: #b91c1c; }
  .probe.warn { color: #b45309; }
</style>
