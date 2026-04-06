<script>
  let { variant = 'default', disabled = false, type = 'button', onclick = undefined, children } = $props();
</script>

<button class="btn btn-{variant}" {type} {disabled} {onclick}>
  {@render children()}
</button>

<style>
  .btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 8px 16px;
    border-radius: var(--radius);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    border: 1px solid var(--border-color);
    background: var(--bg-tertiary);
    color: var(--text-primary);
    transition: background 0.15s, border-color 0.15s;
    white-space: nowrap;
  }
  .btn:hover:not(:disabled) {
    background: var(--bg-hover);
  }
  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .btn-primary {
    background: var(--accent);
    border-color: var(--accent);
    color: #0d1117;
  }
  .btn-primary:hover:not(:disabled) {
    background: var(--accent-hover);
    border-color: var(--accent-hover);
  }
  .btn-danger {
    color: var(--error);
    border-color: var(--error);
    background: transparent;
  }
  .btn-danger:hover:not(:disabled) {
    background: rgba(248, 81, 73, 0.15);
  }
  .btn-success {
    color: var(--success);
    border-color: var(--success);
    background: transparent;
  }
  .btn-success:hover:not(:disabled) {
    background: rgba(63, 185, 80, 0.15);
  }
</style>
