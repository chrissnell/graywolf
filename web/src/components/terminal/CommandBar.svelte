<script>
  // Minimal Ctrl-] command bar. Operators press Ctrl-] anywhere in
  // the terminal route to surface this prompt; commands route to
  // callbacks. Supported:
  //
  //   transcript on|off -> toggle transcript recording for the active
  //                        session
  //   clear             -> hint to the operator to use the terminal's
  //                        own clear (Ctrl-L)
  //
  // Macros have a dedicated UI button (MacroToolbar -> Edit macros),
  // not a Ctrl-] command, so they live outside this prompt.
  //
  // Unknown commands surface an inline error rather than disappearing
  // silently.

  import { Input, Button } from '@chrissnell/chonky-ui';

  let { open = $bindable(false), onCommand } = $props();

  let value = $state('');
  let error = $state('');
  let inputEl = $state(null);

  $effect(() => {
    if (open && inputEl) {
      // Defer to next tick so the input is mounted before focus.
      queueMicrotask(() => inputEl?.focus?.());
    }
    if (!open) {
      value = '';
      error = '';
    }
  });

  function submit(e) {
    e?.preventDefault?.();
    const cmd = value.trim();
    if (!cmd) {
      open = false;
      return;
    }
    const result = onCommand?.(cmd);
    if (result?.error) {
      error = result.error;
      return;
    }
    open = false;
  }

  function onKey(e) {
    if (e.key === 'Escape') {
      open = false;
      e.preventDefault();
    }
  }
</script>

{#if open}
  <div
    class="command-bar"
    role="dialog"
    tabindex="-1"
    aria-modal="true"
    aria-label="Terminal command bar"
    onkeydown={onKey}
  >
    <form onsubmit={submit}>
      <span class="prompt">:</span>
      <Input
        bind:value
        bind:this={inputEl}
        placeholder="transcript on|off / clear"
        aria-label="Command"
      />
      <Button type="submit" variant="primary" size="sm">Run</Button>
      <Button type="button" variant="ghost" size="sm" onclick={() => (open = false)}>Cancel</Button>
    </form>
    {#if error}
      <p class="err" role="alert">{error}</p>
    {/if}
  </div>
{/if}

<style>
  .command-bar {
    position: absolute;
    left: 0;
    right: 0;
    top: 0;
    z-index: 10;
    background: var(--color-surface, #fff);
    border: 1px solid var(--color-border, #ccc);
    border-bottom-width: 2px;
    padding: 8px 12px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    box-shadow: 0 4px 8px rgba(0, 0, 0, 0.06);
  }
  form {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .prompt {
    font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
    font-weight: 700;
    font-size: 16px;
    color: var(--color-text-muted, #666);
  }
  .err { color: var(--color-danger, #c41010); margin: 0; font-size: 12px; }
</style>
