<script>
  import { onMount, onDestroy } from 'svelte';
  import { Select } from '@chrissnell/chonky-ui';
  import { postChannelPtt } from '../../lib/api.js';
  import FormField from '../../components/FormField.svelte';

  let { method = $bindable(), channelId } = $props();

  // Android PTT method constants — must match PTT_METHOD_* in ptt_android.rs
  // and the Kotlin UsbPttAdapter dispatcher (Appendix B of the 4b spec).
  const PTT_METHOD_CP2102N_RTS = 1;
  const PTT_METHOD_CM108_HID   = 2;
  const PTT_METHOD_AIOC_CDC_DTR = 3;
  const PTT_METHOD_VOX          = 4;

  const androidPttOptions = [
    { value: PTT_METHOD_CP2102N_RTS,  label: 'CP2102N RTS (Digirig)' },
    { value: PTT_METHOD_AIOC_CDC_DTR, label: 'CDC-ACM DTR (AIOC)' },
    { value: PTT_METHOD_CM108_HID,    label: 'CM108 HID' },
    { value: PTT_METHOD_VOX,          label: 'VOX (no PTT wire)' },
  ];

  // USB role strings returned by GraywolfWebInterface.listUsbDevices()
  // keyed by PTT method int.
  const PTT_METHOD_USB_ROLE = {
    [PTT_METHOD_CP2102N_RTS]:  'CP2102N',
    [PTT_METHOD_AIOC_CDC_DTR]: 'AIOC',
    [PTT_METHOD_CM108_HID]:    'CM108',
  };

  let pttHeld = $state(false);
  let pttBusy = $state(false);
  let pttHeartbeatInterval = null;

  // USB hardware status state
  let usbDevice = $state(null);
  let usbStatusLoading = $state(false);
  let usbPollInterval = null;

  // Cleans up PTT hold state (called on release, cancel, unmount).
  export function clearPttHold() {
    if (pttHeartbeatInterval !== null) {
      clearInterval(pttHeartbeatInterval);
      pttHeartbeatInterval = null;
    }
    pttHeld = false;
  }

  async function startTestPtt() {
    if (pttBusy || !channelId) return;
    pttHeld = true;
    pttBusy = true;
    try {
      await postChannelPtt(channelId, true);
    } catch (err) {
      console.error('Test PTT key failed:', err);
      clearPttHold();
      pttBusy = false;
      return;
    }
    // Heartbeat: re-send keyed=true every 2s to keep Go-side watchdog alive.
    pttHeartbeatInterval = setInterval(async () => {
      try {
        await postChannelPtt(channelId, true);
      } catch (err) {
        console.error('Test PTT heartbeat failed:', err);
        clearPttHold();
        pttBusy = false;
      }
    }, 2000);
    pttBusy = false;
  }

  async function stopTestPtt() {
    if (!pttHeld) return;
    clearPttHold();
    pttBusy = true;
    try {
      await postChannelPtt(channelId, false);
    } catch (err) {
      console.error('Test PTT unkey failed:', err);
    } finally {
      pttBusy = false;
    }
  }

  // Poll USB device status via the Android JS bridge.
  export function startUsbPoll() {
    if (usbPollInterval !== null) return;
    usbStatusLoading = true;
    const poll = () => {
      try {
        const raw = globalThis.GraywolfWebInterface?.listUsbDevices?.();
        const devices = raw ? JSON.parse(raw) : [];
        const role = PTT_METHOD_USB_ROLE[method];
        usbDevice = role ? (devices.find(d => d.role === role) || null) : null;
      } catch {
        usbDevice = null;
      }
      usbStatusLoading = false;
    };
    poll();
    usbPollInterval = setInterval(poll, 2000);
  }

  export function stopUsbPoll() {
    if (usbPollInterval !== null) {
      clearInterval(usbPollInterval);
      usbPollInterval = null;
    }
    usbDevice = null;
    usbStatusLoading = false;
  }

  // Request USB permission via the Android JS bridge.
  // Registers a per-call callback on window.__usbResult so the Kotlin
  // side can fire it via evaluateJavascript("__usbResult(id, granted)").
  function requestGrant() {
    if (!usbDevice) return;
    // Prefix guarantees a non-empty alphanumeric id even if Math.random()
    // returns 0 (slice(2) of "0" is ""), which T9's Kotlin validator rejects.
    const callbackId = 'cb' + Math.random().toString(36).slice(2);
    // __usbResult is the global dispatcher Kotlin evaluateJavascript calls.
    if (!globalThis.__usbResult) {
      globalThis.__usbResult = (id, granted) => {
        const cb = globalThis.__usbCallbacks?.[id];
        if (cb) cb(granted);
        delete globalThis.__usbCallbacks?.[id];
      };
      globalThis.__usbCallbacks = {};
    }
    globalThis.__usbCallbacks[callbackId] = (granted) => {
      if (granted) {
        usbStatusLoading = true;
        // Re-poll immediately to refresh permission state.
        try {
          const raw = globalThis.GraywolfWebInterface?.listUsbDevices?.();
          const devices = raw ? JSON.parse(raw) : [];
          const role = PTT_METHOD_USB_ROLE[method];
          usbDevice = role ? (devices.find(d => d.role === role) || null) : null;
        } catch {
          usbDevice = null;
        }
        usbStatusLoading = false;
      }
    };
    try {
      globalThis.GraywolfWebInterface?.requestUsbPermission?.(
        usbDevice.vid,
        usbDevice.pid,
        callbackId,
      );
    } catch (err) {
      console.error('requestUsbPermission failed:', err);
    }
  }

  // Start polling as soon as the component is mounted (= Android modal is open).
  // The parent gate `{#if Platform.kind === 'android' && isModemType}` ensures
  // this only fires when the Android PTT section is relevant.
  onMount(() => {
    startUsbPoll();
  });

  onDestroy(() => {
    clearPttHold();
    stopUsbPoll();
    // Tear down the global USB-grant dispatcher so a late callback after
    // unmount can't fire into a dead component.
    if (globalThis.__usbResult) {
      delete globalThis.__usbResult;
      delete globalThis.__usbCallbacks;
    }
  });
</script>

<!-- Android PTT section: method picker, Test PTT toggle, USB status,
     audio routing. Gated on platform at runtime — desktop is unaffected. -->
<div class="android-ptt-section">
  <h4 class="section-label">PTT (Android)</h4>
  <div class="android-ptt-row">
    <FormField label="PTT method" id="ch-android-ptt">
      <Select
        id="ch-android-ptt"
        bind:value={method}
        options={androidPttOptions}
      />
    </FormField>
    <button
      type="button"
      class="test-ptt-btn"
      class:ptt-held={pttHeld}
      onpointerdown={startTestPtt}
      onpointerup={stopTestPtt}
      onpointercancel={stopTestPtt}
      onpointerleave={stopTestPtt}
      disabled={!channelId || pttBusy}
      aria-label={pttHeld ? 'PTT keyed — release to unkey' : 'Press and hold to key transmitter'}
    >
      {pttHeld ? 'KEYED' : '⚡ Test PTT'}
    </button>
  </div>

  <div class="usb-status">
    <span class="usb-status-label">USB hardware:</span>
    {#if usbStatusLoading}
      <span class="usb-status-value">…</span>
    {:else if usbDevice}
      <span class="usb-status-value">
        {usbDevice.name}
        ({usbDevice.permission_granted ? 'Granted ✓' : 'Not granted'})
      </span>
      {#if !usbDevice.permission_granted}
        <button type="button" class="grant-btn" onclick={requestGrant}>Grant access</button>
      {/if}
    {:else}
      <span class="usb-status-value usb-none">none detected</span>
    {/if}
  </div>

  <div class="audio-routing">
    <span class="usb-status-label">Audio routing:</span>
    <!-- TODO: future ticket to pull from a status endpoint once available -->
    <span class="usb-status-value">USB audio (auto)</span>
  </div>
</div>

<style>
  /* Android PTT section */
  .android-ptt-section {
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }
  .android-ptt-row {
    display: flex;
    align-items: flex-end;
    gap: 10px;
    flex-wrap: wrap;
  }
  .android-ptt-row :global(.form-field) {
    flex: 1;
    min-width: 180px;
  }
  .test-ptt-btn {
    padding: 8px 14px;
    border: 2px solid var(--border-color);
    border-radius: 6px;
    background: var(--bg-surface, #fff);
    font-size: 14px;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
    margin-bottom: 1px; /* align with Select baseline */
    touch-action: none; /* prevent scroll on pointer hold */
  }
  .test-ptt-btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .test-ptt-btn.ptt-held {
    border-color: #e53e3e;
    color: #e53e3e;
    background: #fff5f5;
  }
  .usb-status,
  .audio-routing {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 6px;
    font-size: 13px;
  }
  .usb-status-label {
    font-weight: 600;
    color: var(--text-secondary, #555);
  }
  .usb-status-value {
    color: var(--text-primary, #111);
  }
  .usb-none {
    color: var(--text-secondary, #888);
    font-style: italic;
  }
  .grant-btn {
    padding: 2px 8px;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    background: transparent;
    font-size: 12px;
    cursor: pointer;
  }
  .section-label {
    margin: 0 0 6px 0;
    font-size: 15px;
    font-weight: 600;
  }
</style>
