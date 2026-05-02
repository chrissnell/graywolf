<script>
  // Chonky toolbar of operator-defined macros. Click sends the macro's
  // base64-decoded payload bytes via the active session. Per-button
  // disable when the session isn't CONNECTED -- macros mid-handshake
  // would race the link state and the bytes would be dropped silently.

  import { onMount } from 'svelte';
  import { Button } from '@chrissnell/chonky-ui';

  import { macrosStore, payloadBytes } from '../../lib/terminal/macros.svelte.js';

  let { session, onEdit } = $props();

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
  {#each macros as m (m.label)}
    <Button
      size="sm"
      variant="secondary"
      disabled={!connected}
      onclick={() => fireMacro(m)}
      aria-label={`Send macro ${m.label}`}
      title={`Send macro: ${m.label}`}
    >
      {m.label}
    </Button>
  {/each}
  {#if macros.length === 0 && macrosStore.loaded}
    <span class="hint">No macros saved. Click <em>Edit macros</em> to add one.</span>
  {/if}
  <Button size="sm" variant="ghost" onclick={() => onEdit?.()} aria-label="Edit macros">
    Edit macros
  </Button>
</div>

<style>
  .macro-toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
    padding: 4px 8px;
    background: var(--color-surface, #f8f8f8);
    border-bottom: 1px solid var(--color-border, #ddd);
    font-size: 13px;
  }
  .hint {
    color: var(--color-text-muted, #666);
    font-size: 12px;
    margin-left: 4px;
  }
  .hint em {
    font-style: italic;
    color: var(--color-text, #111);
  }
</style>
