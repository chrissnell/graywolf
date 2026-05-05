<script>
  // CredentialPicker -- dropdown over the credentials cache + a final
  // "None" option (no OTP, or enter at fire time) + an inline "Manage
  // One-Time Passwords..." link that opens the credentials modal.
  //
  // Bound `value` is the credential id (number) or null for None. The
  // chonky Select itself only handles string values; we coerce at the
  // boundary using the sentinel '__none' for null.
  import { Select } from '@chrissnell/chonky-ui';
  import CredentialsModal from './CredentialsModal.svelte';
  import { remoteActionsStore } from '../../../lib/remote_actions/store.svelte.js';

  let { value = $bindable(null), label = 'OTP Secret' } = $props();

  let modalOpen = $state(false);

  const NONE = '__none';

  const options = $derived([
    ...remoteActionsStore.creds
      .slice()
      .sort((a, b) => (a.name ?? '').localeCompare(b.name ?? ''))
      .map((c) => ({ value: String(c.id), label: c.name })),
    { value: NONE, label: 'None (no OTP, or enter at fire time)' },
  ]);

  const stringValue = $derived(value == null ? NONE : String(value));

  function handleChange(v) {
    if (v === NONE || v == null) {
      value = null;
      return;
    }
    const n = Number(v);
    value = Number.isFinite(n) ? n : null;
  }
</script>

<div class="picker">
  {#if label}<span class="lbl">{label}</span>{/if}
  <Select
    options={options}
    value={stringValue}
    onValueChange={handleChange}
  />
  <button type="button" class="manage" onclick={() => (modalOpen = true)}>Manage One-time Passwords...</button>
</div>

<CredentialsModal bind:open={modalOpen} />

<style>
  .picker { display: flex; flex-direction: column; gap: 4px; }
  .lbl {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--color-text-dim, var(--text-muted));
  }
  .manage {
    background: transparent;
    border: none;
    color: var(--color-primary);
    font-size: 0.8125rem;
    padding: 0;
    cursor: pointer;
    text-align: left;
    align-self: flex-start;
  }
  .manage:hover { text-decoration: underline; }
</style>
