<script>
  import { Button, Input, Modal, toast } from '@chrissnell/chonky-ui';
  import { actionsStore } from '../../lib/actions/store.svelte.js';
  import { credsApi } from '../../lib/actions/api.js';
  import QRDisplay from './QRDisplay.svelte';
  import CopyableInput from './CopyableInput.svelte';

  let { open = $bindable(false), onClose } = $props();

  // Stage 1 collects the operator-facing name; Issuer + Account are
  // intentionally not surfaced here. The single-user-station design
  // (memory: feedback_single_user_station) means Issuer is always
  // "Graywolf" and Account defaults to the credential name on the
  // backend, so editing them here would be operator-confusing churn.
  // Algorithm/Digits/Period are fixed in v1 and shown as a caption
  // for awareness only (no inputs).
  const NAME_RE = /^[A-Za-z0-9._-]+$/;

  let name = $state('');
  let nameError = $state(null);
  let topError = $state(null);
  let saving = $state(false);

  // Stage 2 is gated on a successful create response. The reveal blob
  // is component-local state on purpose: closing the modal forfeits
  // the secret forever, matching the backend invariant that
  // OTPCredentialCreated is the only response carrying the secret.
  let revealed = $state(null); // null | { name, secret_b32, otpauth_uri }

  let prevOpen = false;
  $effect(() => {
    if (open && !prevOpen) {
      name = '';
      nameError = null;
      topError = null;
      revealed = null;
      saving = false;
    }
    prevOpen = open;
  });

  function validateName() {
    const v = name.trim();
    if (!v) {
      nameError = 'Name is required.';
      return false;
    }
    if (!NAME_RE.test(v)) {
      nameError = 'Letters, digits, dot, dash, underscore only.';
      return false;
    }
    nameError = null;
    return true;
  }

  async function create() {
    topError = null;
    if (!validateName()) return;
    saving = true;
    try {
      const { data, error } = await credsApi.create({ name: name.trim() });
      if (error) {
        const msg = error?.error ?? error?.message ?? 'Create failed.';
        if (typeof msg === 'string' && msg.toLowerCase().includes('name')) {
          nameError = msg;
        } else {
          topError = msg;
        }
        return;
      }
      revealed = {
        name: data.name,
        secret_b32: data.secret_b32,
        otpauth_uri: data.otpauth_uri,
      };
    } catch (e) {
      topError = e?.message ?? String(e);
    } finally {
      saving = false;
    }
  }

  async function finish() {
    // The reveal panel is one-shot. Wipe the secret from JS memory
    // before doing anything async so a debugger pause / heap dump in
    // the loadAll round-trip cannot surface it. Awaiting loadAll keeps
    // us aligned with neighbor components (CredentialsTable, etc.) so
    // the toast and table state agree.
    const credName = revealed.name;
    revealed = null;
    toast(`Credential "${credName}" created.`, 'success');
    try {
      await actionsStore.loadAll();
    } finally {
      open = false;
    }
  }

  function cancel() {
    if (revealed) return; // stage 2 has no Cancel
    open = false;
  }

  // Any close path while `revealed` is set is a forfeiture. Wipe the
  // secret immediately and refresh the credentials table so the row
  // still appears even though the operator escaped via Esc / backdrop.
  // The Esc/backdrop dismissal itself is suppressed via the chonky
  // Modal `onEscapeKeydown` / `onInteractOutside` handlers below; this
  // hook is the safety net for any other close path (e.g. the host
  // page unmounting the modal).
  function handleClose() {
    if (revealed) {
      revealed = null;
      actionsStore.loadAll();
    }
    onClose?.();
  }
</script>

<Modal
  bind:open
  onClose={handleClose}
  onEscapeKeydown={(e) => { if (revealed) e.preventDefault(); }}
  onInteractOutside={(e) => { if (revealed) e.preventDefault(); }}
  class="new-credential-modal"
>
  <Modal.Header>
    <h3 class="modal-title">
      {revealed ? 'Save your one-time-password secret' : 'New OTP Credential'}
    </h3>
    {#if !revealed}
      <Modal.Close aria-label="Close">x</Modal.Close>
    {/if}
  </Modal.Header>
  <Modal.Body>
    {#if topError}
      <div class="error-banner" role="alert">{topError}</div>
    {/if}

    {#if !revealed}
      <div class="form">
        <div class="field">
          <label for="cred-name">Name <span class="req">*</span></label>
          <Input
            id="cred-name"
            type="text"
            bind:value={name}
            onblur={validateName}
            class={nameError ? 'field-invalid' : ''}
          />
          <p class="hint">
            Used in the credential picker on each Action. Letters, digits,
            dot, dash, underscore.
          </p>
          {#if nameError}<p class="field-error">{nameError}</p>{/if}
        </div>

        <p class="caption">
          Algorithm parameters are fixed in v1: TOTP, SHA1, 6 digits, 30s period.
        </p>
      </div>
    {:else}
      <div class="reveal">
        <div class="warning-banner" role="alert">
          <strong>Save this now -- it will not be shown again.</strong>
          Closing this dialog discards the secret. There is no way to
          re-display it; you would have to delete the credential and
          create a new one.
        </div>

        <div class="qr-row">
          <QRDisplay
            uri={revealed.otpauth_uri}
            alt="Scan to enroll {revealed.name} in your authenticator app"
          />
          <p class="qr-help">
            Scan with any TOTP authenticator (Aegis, 1Password, Google
            Authenticator, etc.), or copy the values below.
          </p>
        </div>

        <CopyableInput
          id="cred-secret"
          label="Base32 secret"
          value={revealed.secret_b32}
          monospace
        />
        <CopyableInput
          id="cred-uri"
          label="otpauth:// URI"
          value={revealed.otpauth_uri}
          monospace
        />
      </div>
    {/if}
  </Modal.Body>
  <Modal.Footer>
    {#if !revealed}
      <Button variant="ghost" onclick={cancel} disabled={saving}>Cancel</Button>
      <Button variant="primary" onclick={create} disabled={saving}>
        {saving ? 'Creating...' : 'Create credential'}
      </Button>
    {:else}
      <Button variant="primary" onclick={finish}>I've saved it -- close</Button>
    {/if}
  </Modal.Footer>
</Modal>

<style>
  .modal-title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
  }
  .form {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .field label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--color-text-dim, var(--text-muted));
  }
  .req {
    color: var(--color-danger, #b91c1c);
  }
  .hint {
    margin: 0;
    font-size: 11px;
    color: var(--color-text-muted, var(--text-muted));
  }
  .field-error {
    margin: 0;
    font-size: 11px;
    color: var(--color-danger, #b91c1c);
  }
  .caption {
    margin: 0;
    font-size: 11px;
    color: var(--color-text-muted, var(--text-muted));
    font-style: italic;
  }
  .error-banner {
    background: var(--color-danger-muted, rgba(220, 38, 38, 0.12));
    color: var(--color-danger, #b91c1c);
    border-left: 3px solid var(--color-danger, #b91c1c);
    padding: 8px 12px;
    border-radius: 4px;
    margin-bottom: 12px;
    font-size: 13px;
  }
  .reveal {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .warning-banner {
    background: var(--color-warning-muted, rgba(234, 179, 8, 0.15));
    color: var(--color-warning-strong, #92400e);
    border-left: 3px solid var(--color-warning, #ca8a04);
    padding: 10px 12px;
    border-radius: 4px;
    font-size: 13px;
    line-height: 1.45;
  }
  .warning-banner strong {
    display: block;
    margin-bottom: 4px;
    font-size: 14px;
  }
  .qr-row {
    display: flex;
    gap: 14px;
    align-items: center;
    flex-wrap: wrap;
  }
  .qr-help {
    margin: 0;
    flex: 1 1 220px;
    font-size: 12px;
    color: var(--text-secondary);
    line-height: 1.5;
  }
</style>
