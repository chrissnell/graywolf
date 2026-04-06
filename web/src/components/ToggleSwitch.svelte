<script>
  let { checked = $bindable(false), label = '', id = '', disabled = false } = $props();
</script>

<label class="toggle" for={id}>
  <input type="checkbox" {id} bind:checked {disabled} class="toggle-input" />
  <span class="toggle-track">
    <span class="toggle-thumb"></span>
  </span>
  {#if label}
    <span class="toggle-label">{label}</span>
  {/if}
</label>

<style>
  .toggle {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    user-select: none;
  }
  .toggle-input {
    position: absolute;
    opacity: 0;
    width: 0;
    height: 0;
  }
  .toggle-track {
    width: 36px;
    height: 20px;
    background: var(--bg-hover);
    border-radius: 10px;
    position: relative;
    transition: background 0.2s;
    border: 1px solid var(--border-color);
  }
  .toggle-input:checked + .toggle-track {
    background: var(--accent);
    border-color: var(--accent);
  }
  .toggle-thumb {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    background: var(--text-primary);
    border-radius: 50%;
    transition: transform 0.2s;
  }
  .toggle-input:checked + .toggle-track .toggle-thumb {
    transform: translateX(16px);
  }
  .toggle-label {
    font-size: 13px;
    color: var(--text-secondary);
  }
  .toggle-input:disabled + .toggle-track {
    opacity: 0.5;
  }
</style>
