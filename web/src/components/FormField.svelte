<script>
  let { label = '', error = '', children, id = '' } = $props();
</script>

<div class="field" class:has-error={!!error}>
  {#if label}
    <label for={id} class="field-label">{label}</label>
  {/if}
  {@render children()}
  {#if error}
    <span class="field-error" role="alert">{error}</span>
  {/if}
</div>

<style>
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 12px;
  }
  .field-label {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .field-error {
    font-size: 12px;
    color: var(--error);
  }
</style>
