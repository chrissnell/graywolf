<script>
  import { Label } from '@chrissnell/chonky-ui';

  // The `children` snippet receives a `describedBy` string callers can
  // spread onto the inner control via `aria-describedby={describedBy}`
  // so screen readers announce the hint and/or error alongside the
  // control. Callers that don't need the hookup can keep ignoring the
  // snippet argument; existing usages continue to work unchanged.
  let { label = '', hint = '', error = '', children, id = '' } = $props();

  // Deterministic ids derived from the field's `id` so the child control
  // can reference them without extra plumbing from the caller.
  let hintId = $derived(id && hint ? `${id}-hint` : '');
  let errorId = $derived(id && error ? `${id}-error` : '');
  let describedBy = $derived([hintId, errorId].filter(Boolean).join(' ') || undefined);
</script>

<div class="field" class:has-error={!!error}>
  {#if label}
    <Label for={id}>{label}</Label>
  {/if}
  {@render children(describedBy)}
  {#if hint}
    <span class="field-hint" id={hintId || undefined}>{hint}</span>
  {/if}
  {#if error}
    <span class="field-error" id={errorId || undefined} role="alert">{error}</span>
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
