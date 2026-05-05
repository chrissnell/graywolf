<script>
  // MacroEditRow -- row in the drawer's edit mode. Operator can edit
  // every field of one macro plus delete it.
  //
  // Drag re-order is intentionally not handled here: the parent
  // drawer owns ordering state (see RemoteActionsDrawer.svelte) and
  // exposes up/down arrow controls as the mobile fallback.
  import { Button, Icon, Input } from '@chrissnell/chonky-ui';
  import CredentialPicker from './CredentialPicker.svelte';

  let {
    macro,
    onChange = () => {},
    onDelete = () => {},
    onMoveUp = () => {},
    onMoveDown = () => {},
  } = $props();

  let label = $state('');
  let actionName = $state('');
  let argsString = $state('');
  let credId = $state(null);

  // Re-snapshot editable fields whenever the bound macro identity
  // changes. Plain `$state(macro.label ?? '')` would freeze at the
  // initial prop value (Svelte 5 state_referenced_locally warning) and
  // never reflect new bindings (e.g. Save-as-macro prefill, server
  // refresh swapping in a fresh row, parent re-keying a draft).
  let initialized = false;
  let lastMacroRef = null;
  $effect.pre(() => {
    if (!initialized || macro !== lastMacroRef) {
      label = macro?.label ?? '';
      actionName = macro?.action_name ?? '';
      argsString = macro?.args_string ?? '';
      credId = macro?.remote_otp_credential_id ?? null;
      lastMacroRef = macro;
      initialized = true;
    }
  });

  function commit() {
    onChange({
      ...macro,
      label,
      action_name: actionName,
      args_string: argsString,
      remote_otp_credential_id: credId,
    });
  }

  $effect(() => {
    // Propagate credential changes immediately since the picker has no
    // blur event to gate on.
    if (credId !== (macro?.remote_otp_credential_id ?? null)) {
      onChange({
        ...macro,
        label,
        action_name: actionName,
        args_string: argsString,
        remote_otp_credential_id: credId,
      });
    }
  });
</script>

<div class="row">
  <div class="reorder">
    <button type="button" aria-label="Move up" onclick={onMoveUp}><Icon name="chevron-up" size="sm" /></button>
    <button type="button" aria-label="Move down" onclick={onMoveDown}><Icon name="chevron-down" size="sm" /></button>
  </div>
  <div class="fields">
    <div class="field">
      <label for={`mer-label-${macro.id ?? 'new'}`}>Label</label>
      <Input id={`mer-label-${macro.id ?? 'new'}`} bind:value={label} onblur={commit} />
    </div>
    <div class="cmd-row">
      <div class="field">
        <label for={`mer-action-${macro.id ?? 'new'}`}>Action</label>
        <Input
          id={`mer-action-${macro.id ?? 'new'}`}
          bind:value={actionName}
          oninput={(e) => { actionName = e.currentTarget.value.toUpperCase(); }}
          onblur={commit}
        />
      </div>
      <div class="field">
        <label for={`mer-args-${macro.id ?? 'new'}`}>Args (k=v ...)</label>
        <Input id={`mer-args-${macro.id ?? 'new'}`} bind:value={argsString} onblur={commit} />
      </div>
    </div>
    <CredentialPicker bind:value={credId} />
  </div>
  <Button variant="danger" size="sm" onclick={onDelete}>Delete</Button>
</div>

<style>
  .row {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: start;
    gap: 12px;
    padding: 10px;
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-surface);
    margin-bottom: 8px;
  }
  .reorder { display: flex; flex-direction: column; gap: 2px; }
  .reorder button { background: transparent; border: none; cursor: pointer; color: var(--color-text-muted); padding: 2px; }
  .fields { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .field { display: flex; flex-direction: column; gap: 2px; }
  .field label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--color-text-dim, var(--text-muted));
  }
  .cmd-row { display: grid; grid-template-columns: 1fr 2fr; gap: 8px; }
</style>
