<script>
  import { Label } from '@chrissnell/chonky-ui';

  let { label = '', error = '', children, id = '' } = $props();
</script>

<div class="field" class:has-error={!!error}>
  {#if label}
    <Label for={id}>{label}</Label>
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
  .field-error {
    font-size: 12px;
    color: var(--color-danger);
  }
</style>
