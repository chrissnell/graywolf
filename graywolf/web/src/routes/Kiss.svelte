<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Badge } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import FormField from '../components/FormField.svelte';

  let items = $state([]);
  let channels = $state([]);
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

  const columns = [
    { key: 'type', label: 'Type' },
    { key: 'tcp_port', label: 'TCP Port' },
    { key: 'serial_device', label: 'Serial Device' },
    { key: 'baud_rate', label: 'Baud Rate' },
    { key: 'channel', label: 'Channel' },
    { key: 'mode', label: 'Mode' },
  ];

  const typeOptions = [
    { value: 'tcp', label: 'TCP' },
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
      tcp_port: '8001',
      serial_device: '',
      baud_rate: '9600',
      channel: '1',
      mode: 'modem',
      tnc_ingress_rate_hz: '50',
      tnc_ingress_burst: '100',
    };
  }

  onMount(async () => {
    items = await api.get('/kiss') || [];
    channels = (await api.get('/channels') || []).map(c => ({ value: String(c.id), label: c.name || `Channel ${c.id}` }));
  });

  function openCreate() {
    editing = null;
    form = emptyForm();
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = {
      ...row,
      tcp_port: String(row.tcp_port),
      baud_rate: String(row.baud_rate),
      channel: String(row.channel || 1),
      mode: row.mode || 'modem',
      tnc_ingress_rate_hz: String(row.tnc_ingress_rate_hz || 50),
      tnc_ingress_burst: String(row.tnc_ingress_burst || 100),
    };
    modalOpen = true;
  }

  function buildPayload() {
    const data = {
      ...form,
      tcp_port: parseInt(form.tcp_port),
      baud_rate: parseInt(form.baud_rate),
      channel: parseInt(form.channel),
      tnc_ingress_rate_hz: parseInt(form.tnc_ingress_rate_hz) || 0,
      tnc_ingress_burst: parseInt(form.tnc_ingress_burst) || 0,
    };
    // Strip fields not in KissRequest DTO (backend rejects unknown fields).
    delete data.id;
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
      items = await api.get('/kiss') || [];
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
    if (row.type === 'tcp') return `TCP port ${row.tcp_port}, ${mode}`;
    if (row.type === 'serial') {
      const dev = (row.serial_device || '').trim();
      return dev ? `serial ${dev}, ${mode}` : `serial, ${mode}`;
    }
    return `#${row.id}, ${mode}`;
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
      items = await api.get('/kiss') || [];
    } catch (err) {
      toasts.error(err.message);
    }
  }
</script>

<PageHeader title="KISS Interfaces" subtitle="KISS interface configuration">
  <Button variant="primary" onclick={openCreate}>+ Add KISS</Button>
</PageHeader>

<DataTable
  {columns}
  rows={items}
  onEdit={openEdit}
  onDelete={handleDelete}
  cells={{ mode: modeCell }}
/>

{#snippet modeCell(value, _row)}
  <Badge variant={value === 'tnc' ? 'success' : 'info'}>{modeLabels[value] || 'Modem'}</Badge>
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
        <Select id="kiss-channel" bind:value={form.channel} options={channels} />
      {:else}
        <Input id="kiss-channel" bind:value={form.channel} type="number" placeholder="1" />
      {/if}
    </FormField>
    {#if form.mode === 'tnc'}
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
</style>
