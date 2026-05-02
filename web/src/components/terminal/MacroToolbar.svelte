<script>
  // Persistent toolbar of operator-defined macros. Sits above the
  // terminal viewport in every state of the route -- pre-connect,
  // monitor, and active LAPB session -- so the operator always sees
  // the macro buttons + the Edit macros action. Macro buttons are
  // disabled when there's no CONNECTED session: macros mid-handshake
  // (or with no peer at all) would race the link state and the bytes
  // would be dropped silently.

  import { onMount } from 'svelte';
  import { Button } from '@chrissnell/chonky-ui';

  import { macrosStore, payloadBytes } from '../../lib/terminal/macros.svelte.js';

  let { session = null, onEdit } = $props();

  onMount(() => {
    if (!macrosStore.loaded && !macrosStore.loading) {
      macrosStore.load();
    }
  });

  let macros = $derived(macrosStore.macros);
  let connected = $derived(session?.state?.stateName === 'CONNECTED');

  function fireMacro(m) {
    if (!session || !connected) return;
    const bytes = payloadBytes(m);
    if (bytes.length === 0) return;
    session.sendData(bytes);
  }
</script>

<div class="macro-toolbar" role="toolbar" aria-label="Operator macros">
  <Button variant="accent" onclick={() => onEdit?.()} aria-label="Edit macros">
    Edit macros
  </Button>
  <span class="divider" aria-hidden="true"></span>
  {#each macros as m (m.label)}
    <button
      type="button"
      class="macro-btn"
      disabled={!connected}
      onclick={() => fireMacro(m)}
      aria-label={`Send macro ${m.label}`}
      title={connected ? `Send macro: ${m.label}` : `${m.label} (connect to send)`}
    >
      {m.label}
    </button>
  {/each}
  {#if macros.length === 0 && macrosStore.loaded}
    <span class="hint">No macros yet. Click <em>Edit macros</em> to add one.</span>
  {/if}
</div>

<style>
  .macro-toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    padding: 8px 14px;
    background: var(--color-surface, #f8f8f8);
    border-bottom: 1px solid var(--color-border, #ddd);
    font-size: 14px;
  }
  .divider {
    width: 1px;
    height: 22px;
    background: var(--color-border, #ddd);
    margin: 0 4px;
  }
  .hint {
    color: var(--color-text-muted, #666);
    font-size: 13px;
    font-style: italic;
  }
  .hint em {
    font-style: normal;
    font-weight: 600;
    color: var(--color-text, #111);
  }
  /* Macro buttons: solid yellow with black text. The chonky primary
     variant ships outline-only-until-hover, which made the yellow
     label barely readable on the light toolbar background. */
  .macro-btn {
    font: inherit;
    font-weight: 600;
    padding: 6px 14px;
    border-radius: 4px;
    border: 1px solid var(--color-primary, #ffaa00);
    background: var(--color-primary, #ffaa00);
    color: var(--color-primary-fg, #000);
    cursor: pointer;
    transition: background 0.12s, border-color 0.12s;
  }
  .macro-btn:hover:not(:disabled) {
    background: var(--color-primary-hover, #ffbb33);
    border-color: var(--color-primary-hover, #ffbb33);
  }
  .macro-btn:focus-visible {
    outline: 2px solid var(--color-primary, #ffaa00);
    outline-offset: 2px;
  }
  .macro-btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
</style>
