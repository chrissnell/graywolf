<script>
  import { Button, Input } from '@chrissnell/chonky-ui';

  let {
    value = '',
    label = '',
    id = '',
    monospace = false,
    onCopied,
  } = $props();

  let copied = $state(false);
  let copyError = $state(null);
  let resetTimer = null;

  async function copy() {
    copyError = null;
    try {
      await navigator.clipboard.writeText(value);
      copied = true;
      onCopied?.();
      clearTimeout(resetTimer);
      resetTimer = setTimeout(() => {
        copied = false;
      }, 1200);
    } catch (e) {
      copyError = e?.message || 'Copy failed.';
    }
  }
</script>

<div class="copyable">
  {#if label}
    <label for={id}>{label}</label>
  {/if}
  <div class="row">
    <Input
      {id}
      type="text"
      readonly
      {value}
      class={monospace ? 'mono' : ''}
      onclick={(e) => e.currentTarget.select()}
    />
    <Button variant="secondary" onclick={copy} aria-label="Copy {label || 'value'} to clipboard">
      {copied ? 'Copied!' : 'Copy'}
    </Button>
  </div>
  <p class="sr-only" aria-live="polite">
    {copied ? 'Copied to clipboard' : ''}
  </p>
  {#if copyError}
    <p class="error" role="alert">{copyError}</p>
  {/if}
</div>

<style>
  .copyable {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  label {
    font-size: 0.85rem;
    color: var(--text-secondary);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .row :global(input) {
    margin: 0 !important;
    flex: 1 1 auto;
    min-width: 0;
  }
  .row :global(input.mono) {
    font-family: 'SauceCodePro Nerd Font', ui-monospace, monospace;
    letter-spacing: 0.04em;
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
  .error {
    margin: 4px 0 0;
    font-size: 0.8rem;
    color: var(--color-danger, #b91c1c);
  }
</style>
