<!-- web/src/routes/ptt/DialogChangeMethod.svelte -->
<script>
  import { Button } from '@chrissnell/chonky-ui';
  import Modal from '../../components/Modal.svelte';
  import MethodPicker, { key as methodKey } from './MethodPicker.svelte';

  let {
    open = $bindable(),
    methods,            // array of method-option objects (from methodOptions.{android,desktop}.js)
    initialWireKey,     // string | null; currently-selected method key
    onSaveAndNext,      // (chosen) => void; chosen is the full method-option
    onCancel,
  } = $props();

  let selected = $state(null);

  // On open, seed selection from the initial wire key. We snapshot once
  // per closed→open transition so reactive parents don't repeatedly
  // override the in-dialog selection while the operator is choosing.
  let wasOpen = false;
  $effect(() => {
    if (open && !wasOpen) {
      selected = methods.find(m => methodKey(m) === initialWireKey) || null;
      wasOpen = true;
    } else if (!open) {
      wasOpen = false;
    }
  });

  function handleSaveAndNext() {
    if (!selected) return;
    onSaveAndNext(selected);
  }
</script>

<Modal bind:open title="Change PTT Method" onClose={onCancel}>
  <MethodPicker
    {methods}
    selectedWireKey={selected ? methodKey(selected) : null}
    onSelect={(m) => { selected = m; }}
  />
  <div class="modal-actions">
    <Button onclick={onCancel}>Cancel</Button>
    <Button variant="primary" disabled={!selected} onclick={handleSaveAndNext}>
      Save & next ›
    </Button>
  </div>
</Modal>

<style>
  .modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
</style>
