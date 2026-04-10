<script>
  import { Label } from '@chrissnell/chonky-ui';

  let { label = '', hint = '', error = '', children, id = '' } = $props();
</script>

<div class="field" class:has-error={!!error}>
  {#if label}
    <Label for={id}>{label}</Label>
  {/if}
  {@render children()}
  {#if hint}
    <span class="field-hint">{hint}</span>
  {/if}
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
  .field-hint {
    font-size: 12px;
    color: var(--color-text-muted, #888);
    line-height: 1.4;
  }
  .field-error {
    font-size: 12px;
    color: var(--color-danger);
  }
</style>
