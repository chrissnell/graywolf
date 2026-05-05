<script>
  // EditCredentialModal -- create or edit one RemoteOTPCredential.
  //
  // Field labels follow the operator-facing rename: the secret input
  // is "Secret Key", not "Base32 secret". The handbook explains that
  // the value is the same RFC 4648 base32 string an authenticator app
  // would consume.
  import { Button, Icon, Input, Modal, toast } from '@chrissnell/chonky-ui';
  import { remoteCredsApi } from '../../../lib/remote_actions/api.js';
  import { remoteActionsStore } from '../../../lib/remote_actions/store.svelte.js';
  import ScrollHint from '../../ScrollHint.svelte';

  let { open = $bindable(false), cred = null, onSaved = () => {} } = $props();

  let name = $state('');
  let secret = $state('');
  let saving = $state(false);
  let err = $state('');

  let prevOpen = false;
  $effect(() => {
    if (open && !prevOpen) {
      name = cred?.name ?? '';
      secret = '';
      err = '';
    }
    prevOpen = open;
  });

  const isEdit = $derived(!!cred?.id);

  async function save() {
    err = '';
    if (!name.trim()) {
      err = 'Name required';
      return;
    }
    if (!isEdit && !secret.trim()) {
      err = 'Secret Key required';
      return;
    }
    saving = true;
    try {
      const body = { name: name.trim() };
      if (secret.trim()) body.secret_b32 = secret.trim();
      const res = isEdit
        ? await remoteCredsApi.update(cred.id, body)
        : await remoteCredsApi.create(body);
      if (res.error) {
        err = res.error.error ?? res.error.message ?? 'Save failed';
        return;
      }
      await remoteActionsStore.loadCreds();
      toast(isEdit ? `Updated "${name}"` : `Created "${name}"`, 'success');
      onSaved(res.data);
      open = false;
    } finally {
      saving = false;
    }
  }
</script>

<Modal bind:open>
  <Modal.Header>
    <h3 class="modal-title">{isEdit ? 'Edit Credential' : 'New Credential'}</h3>
    <Modal.Close aria-label="Close"><Icon name="x" size="lg" /></Modal.Close>
  </Modal.Header>
  <Modal.Body>
    <div class="form">
      <div class="field">
        <label for="rcred-name">Name</label>
        <Input id="rcred-name" bind:value={name} placeholder="NW5W OTP" />
      </div>
      <div class="field">
        <label for="rcred-secret">Secret Key</label>
        <textarea
          id="rcred-secret"
          class="secret"
          rows="2"
          bind:value={secret}
          placeholder={isEdit ? '(leave blank to keep current)' : 'paste base32 secret from authenticator'}
        ></textarea>
      </div>
      {#if err}<p class="err" role="alert">{err}</p>{/if}
    </div>
    <ScrollHint />
  </Modal.Body>
  <Modal.Footer>
    <Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
    <Button variant="primary" disabled={saving} onclick={save}>{isEdit ? 'Save' : 'Create'}</Button>
  </Modal.Footer>
</Modal>

<style>
  .modal-title { margin: 0; font-size: 14px; font-weight: 600; }
  .form { display: flex; flex-direction: column; gap: 12px; min-width: 320px; }
  .field { display: flex; flex-direction: column; gap: 4px; }
  .field label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--color-text-dim, var(--text-muted));
  }
  .secret {
    font-family: var(--font-mono);
    font-size: 13px;
    padding: 6px 8px;
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-surface);
    color: var(--color-text);
    resize: vertical;
  }
  .err { color: var(--color-danger); font-size: 0.875rem; margin: 0; }
</style>
