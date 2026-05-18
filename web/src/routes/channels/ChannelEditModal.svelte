<script>
  import { untrack } from 'svelte';
  import { Button, Input, Select, Toggle } from '@chrissnell/chonky-ui';
  import { Platform } from '../../lib/platform.js';
  import Modal from '../../components/Modal.svelte';
  import FormField from '../../components/FormField.svelte';
  import { blankForm, rowToForm, formToPayload, validateForm } from '../../lib/channelForm.js';
  import AndroidPttFields from './AndroidPttFields.svelte';

  // Android PTT method constant — must match PTT_METHOD_* in ptt_android.rs
  // and the Kotlin UsbPttAdapter dispatcher (Appendix B of the 4b spec).
  const PTT_METHOD_CP2102N_RTS = 1;

  let { open = $bindable(), editing, audioDevices, txTimings, onSave, onCancel } = $props();

  // Form state lives here; populated via openCreate/openEdit semantics
  // triggered by the `open` + `editing` prop pair from the parent.
  let form = $state(blankForm());
  let errors = $state({});

  let isModemType = $derived(form.channel_type === 'modem');
  let isTxEnabled = $derived(isModemType && form.output_device_id !== '0');

  let inputDevices = $derived((audioDevices || []).filter(d => d.direction === 'input'));
  let outputDevices = $derived((audioDevices || []).filter(d => d.direction === 'output'));
  let inputDeviceOptions = $derived(inputDevices.map(d => ({ value: String(d.id), label: d.name })));
  let outputDeviceOptions = $derived([
    { value: '0', label: 'None (RX only)' },
    ...outputDevices.map(d => ({ value: String(d.id), label: d.name })),
  ]);

  const modemOptions = [
    { value: 'afsk', label: 'AFSK' },
    { value: 'psk', label: 'PSK' },
  ];

  const channelOptions = [
    { value: '0', label: '0 (Left/Mono)' },
    { value: '1', label: '1 (Right)' },
  ];

  // Android PTT form state (only used when Platform.kind === 'android')
  let androidPttMethod = $state(PTT_METHOD_CP2102N_RTS);
  // Reference to the AndroidPttFields component instance for cleanup calls.
  // $state only to silence Svelte's non_reactive_update warning on the
  // bind:this target; the ref is used imperatively, never read reactively.
  let androidPttComp = $state(null);

  // Edge-triggered open: fire ONCE per closed→open transition, then
  // hold. All init inputs (editing, txTimings, inputDevices, Platform)
  // are read inside untrack() so they do NOT subscribe this effect —
  // only `open` is tracked.  This reproduces the original imperative
  // openCreate / openEdit semantics: a snapshot at open time, not a
  // live reactive binding.  Consequences:
  //   • editing reassigned while open (confirmForcePut 409 path) → NO re-init
  //   • audioDevices / txTimings change while open → NO re-init
  //   • closing then reopening (same or different row) → re-inits once
  let wasOpen = false;
  $effect(() => {
    const isOpen = open; // subscribe to `open` only
    if (isOpen && !wasOpen) {
      // closed→open transition: initialise form exactly once
      untrack(() => {
        const row = editing;
        if (row) {
          form = rowToForm(row, (txTimings || {})[row.id]);
          errors = {};
          if (Platform.kind === 'android') {
            if (row.ptt?.method === 'android' && row.ptt?.gpio_pin) {
              androidPttMethod = row.ptt.gpio_pin;
            } else {
              androidPttMethod = PTT_METHOD_CP2102N_RTS;
            }
          }
        } else {
          // Create path: snapshot inputDevices once at open time.
          const defaultInput = inputDevices.length > 0 ? String(inputDevices[0].id) : '0';
          form = { ...blankForm(), input_device_id: defaultInput };
          errors = {};
          if (Platform.kind === 'android') {
            androidPttMethod = PTT_METHOD_CP2102N_RTS;
          }
        }
      });
      wasOpen = true;
    } else if (!isOpen) {
      wasOpen = false;
    }
  });

  function validate() {
    errors = validateForm(form);
    return Object.keys(errors).length === 0;
  }

  function handleSave() {
    if (!validate()) return;
    const payload = formToPayload(form);
    // Pass the full context the parent's persistSave path needs:
    // channel payload, tx-timing fields, Android PTT method, and
    // isTxEnabled flag. The parent keeps the PUT/POST + referrer-confirm
    // logic (B6/B7) and runs the tx-timing + PTT saves itself.
    onSave({
      payload,
      isTxEnabled,
      txTiming: {
        tx_delay_ms: parseInt(form.tx_delay_ms, 10),
        tx_tail_ms: parseInt(form.tx_tail_ms, 10),
        slot_ms: parseInt(form.slot_ms, 10),
        persist: parseInt(form.persist, 10),
        full_dup: form.full_dup,
      },
      androidPttMethod,
    });
  }

  function handleCancel() {
    androidPttComp?.clearPttHold();
    androidPttComp?.stopUsbPoll();
    onCancel();
  }
</script>

<div class="wide-modal">
<Modal bind:open title={editing ? 'Edit Channel' : 'New Channel'}>
  <!-- ── Identity ──────────────────────────────────────────────── -->
  <!-- Channel type picker (D11). Segmented on create; read-only badge
       on edit. -->
  <div class="channel-type-row">
    <span class="channel-type-label" id="channel-type-label">Channel type</span>
    {#if editing}
      <span class="channel-type-badge">
        {#if form.channel_type === 'modem'}Modem-backed{:else}KISS-TNC only{/if}
      </span>
    {:else}
      <div class="segmented" role="radiogroup" aria-labelledby="channel-type-label">
        <button type="button"
                role="radio"
                aria-checked={form.channel_type === 'modem'}
                class="segment"
                class:active={form.channel_type === 'modem'}
                onclick={() => form.channel_type = 'modem'}>
          Modem-backed
        </button>
        <button type="button"
                role="radio"
                aria-checked={form.channel_type === 'kiss-tnc'}
                class="segment"
                class:active={form.channel_type === 'kiss-tnc'}
                onclick={() => form.channel_type = 'kiss-tnc'}>
          KISS-TNC only
        </button>
      </div>
    {/if}
  </div>

  <div class="form-section">
    <h4 class="section-label">Identity</h4>
    <div class="form-grid-2">
      <FormField label="Name" error={errors.name} id="ch-name">
        <Input id="ch-name" bind:value={form.name} placeholder="VHF APRS" />
      </FormField>
      {#if isModemType}
        <FormField label="Modem Type" id="ch-modem">
          <Select id="ch-modem" bind:value={form.modem_type} options={modemOptions} />
        </FormField>
      {/if}
    </div>

    <FormField
      label="Mode"
      hint="APRS only: beacon, digipeater, iGate, and messages may transmit. Packet only: AX.25 connected-mode terminal sessions only; APRS subsystems are blocked. APRS + Packet: both, on a shared channel."
      id="ch-mode"
    >
      <Select
        id="ch-mode"
        bind:value={form.mode}
        aria-label="Channel mode"
        options={[
          { value: 'aprs', label: 'APRS only' },
          { value: 'packet', label: 'Packet only' },
          { value: 'aprs+packet', label: 'APRS + Packet' },
        ]}
      />
    </FormField>
  </div>

  {#if isModemType}
    <!-- ── Receive ────────────────────────────────────────────────── -->
    <div class="form-section">
      <h4 class="section-label">Receive</h4>
      <div class="form-grid-4">
        <FormField label="Input Device" error={errors.input_device_id} id="ch-indev">
          <Select id="ch-indev" bind:value={form.input_device_id} options={inputDeviceOptions} />
        </FormField>
        <FormField label="Input Channel" id="ch-inch">
          <Select id="ch-inch" bind:value={form.input_channel} options={channelOptions} />
        </FormField>
      </div>
    </div>

    <!-- ── Transmit ───────────────────────────────────────────────── -->
    <div class="form-section">
      <h4 class="section-label">Transmit</h4>
      <div class="form-grid-4">
        <FormField label="Output Device" id="ch-outdev">
          <Select id="ch-outdev" bind:value={form.output_device_id} options={outputDeviceOptions} />
        </FormField>
        {#if isTxEnabled}
          <FormField label="Output Channel" id="ch-outch">
            <Select id="ch-outch" bind:value={form.output_channel} options={channelOptions} />
          </FormField>
        {/if}
      </div>
      <div class="form-grid-3">
        <FormField label="Bit Rate" id="ch-baud">
          <Input id="ch-baud" bind:value={form.bit_rate} type="number" placeholder="1200" />
        </FormField>
        <FormField label="Mark Freq (Hz)" id="ch-mark">
          <Input id="ch-mark" bind:value={form.mark_freq} type="number" placeholder="1200" />
        </FormField>
        <FormField label="Space Freq (Hz)" id="ch-space">
          <Input id="ch-space" bind:value={form.space_freq} type="number" placeholder="2200" />
        </FormField>
      </div>
    </div>

    <!-- ── PTT ───────────────────────────────────────────────────── -->
    {#if Platform.kind === 'android'}
      <AndroidPttFields bind:this={androidPttComp} bind:method={androidPttMethod} channelId={editing?.id} />
    {/if}

    <!-- ── Timing ─────────────────────────────────────────────────── -->
    {#if isTxEnabled}
      <div class="tx-timing-section">
        <h4 class="section-label">Timing</h4>
        <div class="form-grid-4">
          <FormField label="TX Delay (ms)" id="ch-txd"
            hint="Key-up time before sending. 300ms typical.">
            <Input id="ch-txd" bind:value={form.tx_delay_ms} type="number" placeholder="300" />
          </FormField>
          <FormField label="TX Tail (ms)" id="ch-txt"
            hint="Hold time after last byte. 100ms typical.">
            <Input id="ch-txt" bind:value={form.tx_tail_ms} type="number" placeholder="100" />
          </FormField>
          <FormField label="Slot Time (ms)" id="ch-slot"
            hint="CSMA listen interval. 100ms is standard.">
            <Input id="ch-slot" bind:value={form.slot_ms} type="number" placeholder="100" />
          </FormField>
          <FormField label="Persistence (0-255)" id="ch-persist" error={errors.persist}
            hint="TX probability = (val+1)/256. 63 ≈ 25%.">
            <Input id="ch-persist" bind:value={form.persist} type="number" placeholder="63" />
          </FormField>
        </div>
        <Toggle bind:checked={form.full_dup} label="Full Duplex" />
      </div>
    {/if}
  {:else}
    <div class="kiss-only-explainer">
      This channel is serviced by a KISS TNC interface (configured on
      the <a href="#/kiss">KISS page</a>). No audio device, modem, or
      CSMA timing is required — frames route through the attached
      KISS-TNC backend.
    </div>
  {/if}

  <div class="modal-actions">
    <Button onclick={handleCancel}>Cancel</Button>
    <Button variant="primary" onclick={handleSave}>{editing ? 'Save' : 'Create'}</Button>
  </div>
</Modal>
</div>

<style>
  /* Wider modal for channel editor */
  .wide-modal :global(.modal) {
    width: min(860px, 94vw);
  }
  .wide-modal :global(.modal-body) {
    overflow-y: auto;
  }

  /* Form section: border-top + header + content grouping. Matches the
     existing tx-timing-section pattern from Channels.svelte. */
  .form-section {
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }

  .form-grid-2 {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 0 16px;
  }
  .form-grid-3 {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 0 16px;
  }
  .form-grid-4 {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 0 16px;
  }

  /* TX Timing section in modal */
  .tx-timing-section {
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }
  .section-label {
    margin: 0 0 6px 0;
    font-size: 15px;
    font-weight: 600;
  }

  .modal-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 16px;
  }

  /* D11 channel-type segmented control + edit-time read-only badge. */
  .channel-type-row {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 12px;
    flex-wrap: wrap;
  }
  .channel-type-label {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-secondary);
    min-width: 110px;
  }
  .segmented {
    display: inline-flex;
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    overflow: hidden;
  }
  .segment {
    padding: 8px 14px;
    min-height: 40px;
    background: var(--bg-secondary);
    border: none;
    border-right: 1px solid var(--border-color);
    color: var(--text-primary);
    font: inherit;
    cursor: pointer;
  }
  .segment:last-child {
    border-right: none;
  }
  .segment.active {
    background: var(--color-info-muted, rgba(56, 139, 253, 0.15));
    color: var(--color-info, #388bfd);
    font-weight: 600;
  }
  .segment:focus-visible {
    outline: 2px solid var(--color-info, #388bfd);
    outline-offset: -2px;
  }

  .channel-type-badge {
    display: inline-block;
    padding: 4px 10px;
    border-radius: var(--radius);
    background: var(--bg-tertiary);
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .kiss-only-explainer {
    padding: 10px 12px;
    background: var(--bg-tertiary);
    border-left: 3px solid var(--color-info, #388bfd);
    border-radius: var(--radius);
    font-size: 13px;
    color: var(--text-secondary);
    margin-bottom: 8px;
  }
  .kiss-only-explainer a {
    color: var(--color-info, #388bfd);
  }
</style>
