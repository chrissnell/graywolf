<script>
  import { Button } from '@chrissnell/chonky-ui';
  import Modal from './Modal.svelte';

  let {
    open = $bindable(false),
    title = 'Confirm',
    message = '',
    confirmLabel = 'Delete',
    cancelLabel = 'Cancel',
    confirmVariant = 'danger',
    onConfirm = undefined,
  } = $props();

  function handleConfirm() {
    open = false;
    onConfirm?.();
  }

  function handleCancel() {
    open = false;
  }
</script>

<Modal bind:open {title} onClose={handleCancel}>
  <p class="confirm-message">{message}</p>
  <div class="modal-actions">
    <Button onclick={handleCancel}>{cancelLabel}</Button>
    <Button variant={confirmVariant} onclick={handleConfirm}>{confirmLabel}</Button>
  </div>
</Modal>

<style>
  .confirm-message {
    font-size: 13px;
    color: var(--text-primary);
    line-height: 1.5;
    margin: 0 0 16px;
  }
  .modal-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
  }
</style>
