<script>
  let { open = $bindable(false), title = '', children } = $props();

  function onKeydown(e) {
    if (e.key === 'Escape') open = false;
  }
</script>

{#if open}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div class="modal-backdrop" onclick={() => open = false} onkeydown={onKeydown} role="dialog" aria-modal="true" aria-label={title}>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-content" onclick={(e) => e.stopPropagation()}>
      <div class="modal-header">
        <h3 class="modal-title">{title}</h3>
        <button class="modal-close" onclick={() => open = false} aria-label="Close">×</button>
      </div>
      <div class="modal-body">
        {@render children()}
      </div>
    </div>
  </div>
{/if}

<style>
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
    padding: 16px;
  }
  .modal-content {
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    max-width: 560px;
    width: 100%;
    max-height: 85vh;
    overflow-y: auto;
  }
  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 16px 20px;
    border-bottom: 1px solid var(--border-color);
  }
  .modal-title {
    font-size: 16px;
    font-weight: 600;
  }
  .modal-close {
    background: none;
    border: none;
    color: var(--text-muted);
    font-size: 20px;
    cursor: pointer;
    padding: 0 4px;
  }
  .modal-close:hover {
    color: var(--text-primary);
  }
  .modal-body {
    padding: 20px;
  }
</style>
