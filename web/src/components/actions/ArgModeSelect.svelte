<script>
  import { Select } from '@chrissnell/chonky-ui';

  /** @type {'kv'|'freeform'} */
  let { value = $bindable('kv') } = $props();

  const options = [
    { value: 'kv', label: 'Key/value (default)' },
    { value: 'freeform', label: 'Freeform (everything after the verb is one payload)' },
  ];
</script>

<div class="arg-mode">
  <label class="arg-mode__label" for="arg-mode-select">Argument mode</label>
  <Select id="arg-mode-select" bind:value {options} />
  <p class="arg-mode__help">
    {#if value === 'kv'}
      Senders write <code>@@&lt;otp&gt;#name k1=v1 k2=v2</code>. Each
      argument is validated against its own schema row. Use this for
      structured commands.
    {:else}
      Senders write <code>@@&lt;otp&gt;#name &lt;anything&gt;</code> and
      everything after the verb is one payload. Your handler is
      responsible for parsing and revalidating it.
      <a href="/handbook/actions-handler-safety-shell.html#freeform" target="_blank" rel="noopener">
        Read the safety guide before enabling.
      </a>
    {/if}
  </p>
</div>

<style>
  .arg-mode { margin-bottom: 1rem; }
  .arg-mode__label {
    display: block;
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--color-text-dim, var(--text-muted));
    margin-bottom: 0.25rem;
  }
  .arg-mode__help {
    font-size: 11px;
    color: var(--color-text-muted, var(--text-muted));
    margin: 0.5rem 0 0;
  }
  .arg-mode__help code {
    font-family: ui-monospace, monospace;
    background: var(--accent-bg, rgba(0, 0, 0, 0.05));
    padding: 1px 4px;
    border-radius: 3px;
  }
  .arg-mode :global(select) {
    margin: 0 !important;
  }
</style>
