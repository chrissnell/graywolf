<!-- web/src/routes/ptt/DialogChangeDevice.svelte -->
<script>
  import { Button, Input, Select, Toggle } from '@chrissnell/chonky-ui';
  import Modal from '../../components/Modal.svelte';
  import FormField from '../../components/FormField.svelte';
  import DevicePicker from './DevicePicker.svelte';
  import { api } from '../../lib/api.js';

  let {
    open = $bindable(),
    method,
    deviceSource,
    initialDevicePath,
    initialGpioLine,     // for gpio method
    initialGpioPin,      // for cm108 method
    initialInvert,       // any keying method
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

  let wireMethod = $derived(method?.wire?.method);
  let isGpio = $derived(wireMethod === 'gpio');
  let isCm108 = $derived(wireMethod === 'cm108');

  let wasOpen = false;
  $effect(() => {
    if (open && !wasOpen) {
      selectedPath = initialDevicePath || null;
      gpioLineSel = String(initialGpioLine ?? 0);
      gpioPinSel = String(initialGpioPin ?? 3);
      invert = !!initialInvert;
      wasOpen = true;
      void refresh();
    } else if (!open) {
      wasOpen = false;
    }
  });

  // Whenever the selected gpio chip path changes, refresh its line list.
  $effect(() => {
    if (isGpio && selectedPath) {
      void loadGpioLines(selectedPath);
    } else {
      gpioLines = [];
    }
  });

  async function loadGpioLines(chipPath) {
    loadingGpioLines = true;
    try {
      const result = await api.get('/ptt/gpio-chips/' + encodeURIComponent(chipPath) + '/lines');
      gpioLines = Array.isArray(result) ? result : [];
    } catch {
      gpioLines = [];
    } finally {
      loadingGpioLines = false;
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
    }
  }

  function handleRequestPermission(d) {
    deviceSource.requestPermission?.(d)?.then(refresh);
  }

  function buildPayload() {
    return {
      device: devices.find(d => d.path === selectedPath) || null,
      gpio_line: isGpio ? Number(gpioLineSel) : undefined,
      gpio_pin: isCm108 ? Number(gpioPinSel) : undefined,
      invert,
    };
  }

  let canSave = $derived(!!selectedPath);

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
  {:else}
    <DevicePicker
      {devices}
      {selectedPath}
      onSelect={(d) => { selectedPath = d.path || null; }}
      onRequestPermission={deviceSource.requestPermission ? handleRequestPermission : undefined}
    />
  {/if}

  {#if isGpio && selectedPath}
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
    <FormField label="Invert Polarity" id="dlg-invert">
      <Toggle bind:checked={invert} label="Key radio on LOW instead of HIGH" />
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
</style>
