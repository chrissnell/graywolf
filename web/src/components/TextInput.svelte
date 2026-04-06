<script>
  let { value = $bindable(''), type = 'text', placeholder = '', id = '', disabled = false, ...rest } = $props();
</script>

<input
  {type}
  {id}
  {placeholder}
  {disabled}
  bind:value
  class="text-input"
  {...rest}
/>

<style>
  .text-input {
    background: var(--bg-primary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    color: var(--text-primary);
    padding: 8px 12px;
    font-size: 14px;
    width: 100%;
    outline: none;
    transition: border-color 0.15s;
  }
  .text-input:focus {
    border-color: var(--accent);
  }
  .text-input:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .text-input::placeholder {
    color: var(--text-muted);
  }
</style>
