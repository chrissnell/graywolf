<script>
  // /messages/tactical — rendered inside the Messages shell in place
  // of the thread pane. The conversation list stays visible.
  //
  // Uses chonky's <Modal> for the add/edit dialog and <AlertDialog>
  // for destructive delete confirm. Inline validation mirrors the
  // backend rules:
  //   - callsign ^[A-Z0-9-]{1,9}$
  //   - not equal to the iGate's primary callsign
  //   - no duplicate callsign in the tactical set
  //   - not a well-known bot name (backend 400s anyway; we check
  //     a small built-in list for snappy UX)
  //
  // Bot-name set is hard-coded here (not imported from chonky or
  // the generated client; those don't ship it). The server-side
  // check is authoritative — our client list only improves the
  // local feedback loop. Keeping it short and obvious is fine.

  import { onMount } from 'svelte';
  import {
    AlertDialog, Box, Button, EmptyState, Icon, Input, Modal, ScrollArea, Toggle,
  } from '@chrissnell/chonky-ui';
  import { messages as store } from '../../lib/messagesStore.svelte.js';
  import { api } from '../../lib/api.js';
  import {
    listTacticals, createTactical, updateTactical, deleteTactical,
  } from '../../api/messages.js';
  import { toasts } from '../../lib/stores.js';

  const WELL_KNOWN_BOTS_SAMPLE = [
    'BLN', 'BLN1', 'BLN2', 'BLN3', 'BLN4', 'BLN5', 'BLN6', 'BLN7', 'BLN8', 'BLN9',
    'NWS', 'SKYWARN', 'SMSGTE', 'EMAIL', 'EMAIL-2', 'APRSM', 'APRSTT', 'BOT',
  ];

  /** @type {Array<any>} */
  let list = $state([]);
  let ourCall = $state('');

  /** Dialog state */
  let modalOpen = $state(false);
  let editing = $state(null); // row being edited, or null for "new"
  let form = $state({ callsign: '', alias: '', enabled: true });
  let formError = $state('');
  let saving = $state(false);

  /** Delete confirm */
  let deleteOpen = $state(false);
  let deleteTarget = $state(null);

  async function refresh() {
    try {
      const rows = await listTacticals();
      list = rows || [];
      store.setTacticals(list);
    } catch {
      // non-fatal
    }
  }

  onMount(async () => {
    await refresh();
    try {
      const ig = await api.get('/igate');
      ourCall = (ig?.callsign || '').toUpperCase();
    } catch { /* ignore */ }
  });

  function openCreate() {
    editing = null;
    form = { callsign: '', alias: '', enabled: true };
    formError = '';
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = { callsign: row.callsign || '', alias: row.alias || '', enabled: row.enabled !== false };
    formError = '';
    modalOpen = true;
  }

  function validate() {
    const call = (form.callsign || '').trim().toUpperCase();
    if (!call) return 'Callsign is required.';
    if (!/^[A-Z0-9-]{1,9}$/.test(call)) return 'Invalid format — use up to 9 characters: A-Z, 0-9, -.';
    if (ourCall && call === ourCall) return `Cannot use your primary callsign (${ourCall}).`;
    if (WELL_KNOWN_BOTS_SAMPLE.includes(call)) return `${call} is a well-known APRS service — choose a different label.`;
    const dup = list.find(r => (r.callsign || '').toUpperCase() === call && r.id !== editing?.id);
    if (dup) return `${call} is already configured.`;
    return '';
  }

  async function handleSave() {
    const err = validate();
    if (err) { formError = err; return; }
    saving = true;
    const call = (form.callsign || '').trim().toUpperCase();
    const payload = { callsign: call, alias: (form.alias || '').trim(), enabled: !!form.enabled };
    try {
      if (editing) {
        await updateTactical(editing.id, payload);
        toasts.success(`Tactical updated: ${call}`);
      } else {
        await createTactical(payload);
        toasts.success(`Tactical added: ${call}`);
      }
      modalOpen = false;
      await refresh();
    } catch (e) {
      formError = e?.message || 'Save failed.';
    } finally {
      saving = false;
    }
  }

  async function toggleEnabled(row, next) {
    try {
      await updateTactical(row.id, { callsign: row.callsign, alias: row.alias || '', enabled: next });
      await refresh();
    } catch (e) {
      toasts.error(e?.message || 'Update failed');
    }
  }

  function confirmDelete(row) {
    deleteTarget = row;
    deleteOpen = true;
  }

  async function runDelete() {
    if (!deleteTarget) return;
    try {
      await deleteTactical(deleteTarget.id);
      toasts.success(`Tactical removed: ${deleteTarget.callsign}`);
      deleteOpen = false;
      deleteTarget = null;
      await refresh();
    } catch (e) {
      toasts.error(e?.message || 'Delete failed');
    }
  }

  function onCallsignInput(e) {
    form.callsign = (e.target.value || '').toUpperCase();
    if (e.target.value !== form.callsign) e.target.value = form.callsign;
    formError = '';
  }
</script>

<section class="settings-pane" data-testid="tactical-settings-pane">
  <header class="head">
    <div class="title-block">
      <h2 class="title">Tactical callsigns</h2>
      <p class="sub">Short labels like NET, EOC, or ARES-WX broadcast to every station monitoring that label.</p>
    </div>
    <Button variant="primary" onclick={openCreate} data-testid="tactical-add">
      <Icon name="plus" size="sm" />
      Add tactical
    </Button>
  </header>

  <Box class="list-box">
    <ScrollArea class="list-scroll">
      {#if list.length === 0}
        <div class="empty" data-testid="tactical-empty">
          <EmptyState>
            <div class="empty-inner">
              <Icon name="radio-tower" size="xl" />
              <h3>No tactical callsigns yet</h3>
              <p>Add a label to monitor a group net. Everyone with the same label sees each other's messages.</p>
              <Button variant="primary" onclick={openCreate}>
                <Icon name="plus" size="sm" />
                Add your first tactical
              </Button>
            </div>
          </EmptyState>
        </div>
      {:else}
        <ul class="rows">
          {#each list as row (row.id)}
            <li class="row">
              <div class="row-text">
                <span class="row-call">{row.callsign}</span>
                {#if row.alias}
                  <span class="row-alias">{row.alias}</span>
                {/if}
              </div>
              <div class="row-actions">
                <Toggle
                  checked={row.enabled !== false}
                  onCheckedChange={(v) => toggleEnabled(row, v)}
                  label="Enabled"
                  aria-label={`Toggle monitoring for ${row.callsign}`}
                />
                <Button variant="ghost" size="sm" onclick={() => openEdit(row)} aria-label={`Edit ${row.callsign}`}>
                  <Icon name="pencil" size="sm" />
                </Button>
                <Button variant="ghost" size="sm" onclick={() => confirmDelete(row)} aria-label={`Delete ${row.callsign}`}>
                  <Icon name="trash-2" size="sm" />
                </Button>
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </ScrollArea>
  </Box>
</section>

<Modal bind:open={modalOpen}>
  <Modal.Header>
    <h3 class="modal-title">{editing ? 'Edit tactical' : 'Add tactical'}</h3>
    <Modal.Close>
      <Icon name="x" size="sm" />
    </Modal.Close>
  </Modal.Header>
  <Modal.Body>
    <div class="field">
      <label for="tactical-call">Callsign</label>
      <Input
        id="tactical-call"
        type="text"
        value={form.callsign}
        oninput={onCallsignInput}
        placeholder="NET, EOC, ARES-WX..."
        aria-describedby="tactical-call-help"
      />
      <p id="tactical-call-help" class="help">Use a short label like NET, EOC, or ARES-WX. Coordinate with other operators so you share the same label.</p>
    </div>
    <div class="field">
      <label for="tactical-alias">Alias (optional)</label>
      <Input
        id="tactical-alias"
        type="text"
        bind:value={form.alias}
        placeholder="Main Ops Net"
      />
    </div>
    <div class="field toggle-field">
      <Toggle bind:checked={form.enabled} label="Monitor now" />
    </div>
    {#if formError}
      <p class="err" role="alert">{formError}</p>
    {/if}
  </Modal.Body>
  <Modal.Footer>
    <Button variant="ghost" onclick={() => modalOpen = false}>Cancel</Button>
    <Button variant="primary" onclick={handleSave} disabled={saving}>
      {editing ? 'Save' : 'Add tactical'}
    </Button>
  </Modal.Footer>
</Modal>

<AlertDialog bind:open={deleteOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete tactical callsign</AlertDialog.Title>
    <AlertDialog.Description>
      Remove <strong>{deleteTarget?.callsign}</strong>? You'll stop seeing messages on this
      tactical. Existing history is not affected.
    </AlertDialog.Description>
    <div class="alert-footer">
      <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action class="danger-action" onclick={runDelete}>Delete</AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<style>
  .settings-pane {
    display: flex;
    flex-direction: column;
    height: 100%;
    padding: 16px 20px;
    overflow: hidden;
    background: var(--color-bg);
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 16px;
  }
  .title-block { flex: 1 1 auto; min-width: 0; }
  .title {
    margin: 0;
    font-size: 16px;
    font-weight: 600;
    font-family: var(--font-mono);
  }
  .sub {
    margin: 4px 0 0;
    color: var(--color-text-muted);
    font-size: 12px;
    line-height: 1.5;
  }
  :global(.list-box) {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  :global(.list-scroll) {
    flex: 1 1 auto;
    min-height: 0;
  }

  .empty {
    padding: 48px 16px;
    display: flex;
    justify-content: center;
  }
  .empty-inner {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    max-width: 360px;
    text-align: center;
  }
  .empty-inner h3 {
    margin: 8px 0 0;
    font-size: 15px;
    font-weight: 600;
  }
  .empty-inner p {
    margin: 0 0 8px;
    color: var(--color-text-muted);
    font-size: 13px;
    line-height: 1.5;
  }

  .rows {
    list-style: none;
    padding: 0;
    margin: 0;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    border-bottom: 1px solid var(--color-border-subtle);
  }
  .row-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1 1 auto;
    min-width: 0;
  }
  .row-call {
    font-family: var(--font-mono);
    font-weight: 700;
    color: var(--color-text);
    letter-spacing: 0.5px;
  }
  .row-alias {
    font-size: 12px;
    color: var(--color-text-muted);
  }
  .row-actions {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 12px;
  }
  .field label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 1px;
    text-transform: uppercase;
    color: var(--color-text-dim);
  }
  .toggle-field { margin-top: 4px; }
  .help {
    margin: 0;
    font-size: 11px;
    color: var(--color-text-dim);
    line-height: 1.4;
  }
  .err {
    margin: 0;
    color: var(--color-danger);
    font-size: 12px;
  }

  .modal-title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    font-family: var(--font-mono);
  }

  .alert-footer {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    padding: 1rem 1.5rem 1.25rem;
  }
  :global(.danger-action) {
    background: var(--color-danger) !important;
    color: white !important;
  }
</style>
