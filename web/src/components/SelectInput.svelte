<script>
  let { value = $bindable(''), options = [], id = '', disabled = false } = $props();
</script>

<select bind:value {id} {disabled} class="select-input">
  {#each options as opt}
    <option value={opt.value}>{opt.label}</option>
  {/each}
</select>

<style>
  .select-input {
    background: var(--bg-primary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    color: var(--text-primary);
    padding: 8px 12px;
    font-size: 14px;
    width: 100%;
    outline: none;
    transition: border-color 0.15s;
    cursor: pointer;
    appearance: none;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%238b949e' d='M6 8L1 3h10z'/%3E%3C/svg%3E");
    background-repeat: no-repeat;
    background-position: right 10px center;
    padding-right: 30px;
  }
  .select-input:focus {
    border-color: var(--accent);
  }
  .select-input:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .select-input option {
    background: var(--bg-secondary);
    color: var(--text-primary);
  }
</style>
