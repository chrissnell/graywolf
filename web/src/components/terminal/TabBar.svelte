<script>
  import { Tabs, Badge } from '@chrissnell/chonky-ui';
  import { terminalSessions } from '../../lib/terminal/sessions.svelte.js';

  let { onNew, onClose } = $props();

  let ids = $derived(terminalSessions.ids());
  let active = $derived(terminalSessions.activeId() ?? '__new');

  function setActive(value) {
    if (value === '__new') {
      onNew?.();
      return;
    }
    terminalSessions.setActive(value);
  }

  function closeTab(e, id) {
    e.stopPropagation();
    e.preventDefault();
    onClose?.(id);
  }

  function badgeVariantFor(stateName) {
    if (stateName === 'CONNECTED') return 'success';
    if (stateName === 'DISCONNECTED') return 'danger';
    return 'warning';
  }

  function compact(n) {
    if (n < 1000) return String(n);
    if (n < 1_000_000) return (n / 1000).toFixed(1) + 'k';
    return (n / 1_000_000).toFixed(1) + 'M';
  }
</script>

{#if ids.length > 0}
  <Tabs.Root value={active} onValueChange={setActive}>
    <Tabs.List class="terminal-tabs">
      {#each ids as id (id)}
        {@const sess = terminalSessions.get(id)}
        {#if sess}
          <Tabs.Trigger value={id}>
            <span class="tab-row">
              <Badge variant={badgeVariantFor(sess.state.stateName)}>{sess.state.stateName?.[0] ?? '?'}</Badge>
              <span class="peer">{sess.state.peer || 'session'}</span>
              {#if !sess.state.focused && sess.state.unreadBytes > 0}
                <span class="badge-unread" title="{sess.state.unreadBytes} bytes unread">{compact(sess.state.unreadBytes)}</span>
              {/if}
              <button
                type="button"
                class="close"
                aria-label={`Close session ${sess.state.peer}`}
                onclick={(e) => closeTab(e, id)}
              >x</button>
            </span>
          </Tabs.Trigger>
        {/if}
      {/each}
      <Tabs.Trigger value="__new">
        <span aria-label="New connection">+ New connection</span>
      </Tabs.Trigger>
    </Tabs.List>
  </Tabs.Root>
{/if}

<style>
  :global(.terminal-tabs) { gap: 4px; }
  .tab-row { display: inline-flex; align-items: center; gap: 6px; }
  .peer { font-weight: 600; }
  .badge-unread {
    background: var(--color-primary, #ffaa00);
    color: var(--color-primary-fg, #000);
    border-radius: 9999px;
    padding: 1px 8px;
    font-size: 11px;
    font-weight: 600;
  }
  .close {
    margin-left: 4px;
    border: none;
    background: transparent;
    cursor: pointer;
    color: var(--color-text-muted, #666);
    font-size: 12px;
    padding: 0 2px;
  }
  .close:hover { color: var(--color-danger, #c41010); }
</style>
