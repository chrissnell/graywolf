<script>
  import { toasts } from '../lib/stores.js';

  let items = $state([]);
  $effect(() => {
    const unsub = toasts.subscribe((v) => { items = v; });
    return unsub;
  });
</script>

<div class="toast-container" role="status" aria-live="polite">
  {#each items as toast (toast.id)}
    <div class="toast toast-{toast.type}">
      <span class="toast-icon">{toast.type === 'success' ? '✓' : '✗'}</span>
      <span class="toast-msg">{toast.message}</span>
      <button class="toast-close" onclick={() => toasts.dismiss(toast.id)} aria-label="Dismiss">×</button>
    </div>
  {/each}
</div>

<style>
  .toast-container {
    position: fixed;
    top: 16px;
    right: 16px;
    z-index: 9999;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: 400px;
  }
  .toast {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    border-radius: var(--radius);
    background: var(--bg-tertiary);
    border: 1px solid var(--border-color);
    color: var(--text-primary);
    font-size: 13px;
    animation: slideIn 0.2s ease-out;
  }
  .toast-success { border-color: var(--success); }
  .toast-error { border-color: var(--error); }
  .toast-icon { font-weight: 700; }
  .toast-success .toast-icon { color: var(--success); }
  .toast-error .toast-icon { color: var(--error); }
  .toast-msg { flex: 1; }
  .toast-close {
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 16px;
    padding: 0 4px;
  }
  @keyframes slideIn {
    from { transform: translateX(100%); opacity: 0; }
    to { transform: translateX(0); opacity: 1; }
  }
</style>
