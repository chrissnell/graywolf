<!-- web/src/routes/ptt/DialogChangeMethod.svelte -->
<script>
  import { Button, Input } from '@chrissnell/chonky-ui';
  import Modal from '../../components/Modal.svelte';
  import FormField from '../../components/FormField.svelte';
  import MethodPicker, { key as methodKey } from './MethodPicker.svelte';

  let {
    open = $bindable(),
    methods,
    initialWireKey,
    initialDevicePath,      // for rigctld: parse host:port; otherwise unused
    onSaveAndNext,
    onCancel,
  } = $props();

  let selected = $state(null);
  let rigctldHost = $state('localhost');
  let rigctldPort = $state(4532);

  let wasOpen = false;
  $effect(() => {
    if (open && !wasOpen) {
      selected = methods.find(m => methodKey(m) === initialWireKey) || null;
      // Seed rigctld host/port if the existing config was rigctld.
      if (selected?.wire?.method === 'rigctld' && initialDevicePath && !initialDevicePath.includes('[')) {
        const idx = initialDevicePath.lastIndexOf(':');
        if (idx > 0) {
          rigctldHost = initialDevicePath.slice(0, idx) || 'localhost';
          const p = parseInt(initialDevicePath.slice(idx + 1), 10);
          if (Number.isInteger(p) && p >= 1 && p <= 65535) rigctldPort = p;
        }
      } else {
        rigctldHost = 'localhost';
        rigctldPort = 4532;
      }
      wasOpen = true;
    } else if (!open) {
      wasOpen = false;
    }
  });

  let isRigctld = $derived(selected?.wire?.method === 'rigctld');
  let hostValid = $derived(rigctldHost.trim().length > 0 && !rigctldHost.includes(':'));
  let portValid = $derived(Number.isInteger(Number(rigctldPort)) && Number(rigctldPort) >= 1 && Number(rigctldPort) <= 65535);
  let canSave = $derived(!!selected && (!isRigctld || (hostValid && portValid)));

  let testState = $state({ kind: 'idle' });

  async function testConnection() {
    if (testState.kind === 'testing') return;
    if (!hostValid || !portValid) return;
    testState = { kind: 'testing' };
    try {
      const res = await fetch('/api/ptt/test-rigctld', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ host: rigctldHost.trim(), port: Number(rigctldPort) }),
      });
      if (!res.ok) {
        let msg = res.statusText || `HTTP ${res.status}`;
        try {
          const body = await res.json();
          if (body?.message) msg = body.message;
          else if (body?.error) msg = body.error;
        } catch { /* non-JSON body */ }
        testState = { kind: 'error', message: msg };
        return;
      }
      const body = await res.json();
      if (body?.ok) {
        const latency = Number.isFinite(body.latency_ms) ? body.latency_ms : 0;
        testState = { kind: 'success', latencyMs: latency };
      } else {
        testState = { kind: 'error', message: body?.message || 'rigctld reported failure' };
      }
    } catch (err) {
      testState = { kind: 'error', message: err?.message || 'Network error' };
    }
  }

  // Reset test result when the rigctld fields change.
  $effect(() => {
    void rigctldHost;
    void rigctldPort;
    if (testState.kind !== 'idle' && testState.kind !== 'testing') testState = { kind: 'idle' };
  });

  function handleSaveAndNext() {
    if (!canSave) return;
    if (isRigctld) {
      onSaveAndNext(selected, { rigctld: { host: rigctldHost.trim(), port: Number(rigctldPort) } });
    } else {
      onSaveAndNext(selected, null);
    }
  }
</script>

<Modal bind:open title="Change PTT Method" onClose={onCancel}>
  <MethodPicker
    {methods}
    selectedWireKey={selected ? methodKey(selected) : null}
    onSelect={(m) => { selected = m; }}
  />
  {#if isRigctld}
    <div class="rigctld-extras">
      <div class="rigctld-fields">
        <FormField label="rigctld Hostname" id="dlg-rigctld-host">
          <Input id="dlg-rigctld-host" bind:value={rigctldHost} placeholder="localhost" />
        </FormField>
        <FormField label="rigctld Port" id="dlg-rigctld-port">
          <Input id="dlg-rigctld-port" type="number" min={1} max={65535} bind:value={rigctldPort} />
        </FormField>
      </div>
      <div class="rigctld-test-row">
        <Button onclick={testConnection} disabled={!hostValid || !portValid || testState.kind === 'testing'}>
          {#if testState.kind === 'testing'}Testing…{:else}Test Connection{/if}
        </Button>
      </div>
      <div class="rigctld-result" role="status" aria-live="polite">
        {#if testState.kind === 'success'}
          <span class="rigctld-badge ok">Success: Connected ({testState.latencyMs} ms)</span>
        {:else if testState.kind === 'error'}
          <span class="rigctld-badge err">Failed: {testState.message}</span>
        {/if}
      </div>
    </div>
  {/if}
  <div class="modal-actions">
    <Button onclick={onCancel}>Cancel</Button>
    <Button variant="primary" disabled={!canSave} onclick={handleSaveAndNext}>
      {#if isRigctld}Save{:else}Save & next ›{/if}
    </Button>
  </div>
</Modal>

<style>
  .modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
  .rigctld-extras { display: flex; flex-direction: column; gap: 8px; margin-top: 12px; }
  .rigctld-fields { display: flex; gap: 8px; }
  .rigctld-fields :global(.form-field) { flex: 1; }
  .rigctld-test-row { display: flex; justify-content: flex-end; margin-top: 8px; }
  .rigctld-result { margin-top: 6px; font-size: 13px; }
  .rigctld-badge { padding: 2px 8px; border-radius: 4px; }
  .rigctld-badge.ok { background: #ecfdf5; color: #047857; }
  .rigctld-badge.err { background: #fef2f2; color: #b91c1c; }
</style>
