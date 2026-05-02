<script>
  // Live APRS packet monitor for non-pure-packet channels. Uses the
  // same xterm.js viewport that LAPB sessions render in -- the only
  // differences are the data source (raw_tail WebSocket subscription
  // instead of LAPB I-frames) and that operator keystrokes are
  // discarded (see lib/terminal/monitor.svelte.js).
  //
  // The filter input narrows the server-side substring match; the
  // chosen filter is persisted via AX25TerminalConfig.RawTailFilter
  // so the operator's pick survives a route refresh.

  import { onMount, onDestroy } from 'svelte';
  import { Badge, Button, Input } from '@chrissnell/chonky-ui';
  import TerminalViewport from './TerminalViewport.svelte';

  import { createMonitorSession } from '../../lib/terminal/monitor.svelte.js';

  let { channel } = $props();

  let session = $state(null);
  let filterText = $state('');
  let savedFilter = $state('');

  async function loadSavedFilter() {
    try {
      const r = await fetch('/api/ax25/terminal-config', { credentials: 'same-origin' });
      if (!r.ok) return;
      const cfg = await r.json();
      savedFilter = cfg?.raw_tail_filter ?? '';
      filterText = savedFilter;
    } catch {
      // Non-fatal -- operator can still type a filter manually.
    }
  }

  async function applyFilter() {
    session?.setFilter?.(filterText);
    if (filterText !== savedFilter) {
      try {
        await fetch('/api/ax25/terminal-config', {
          method: 'PUT',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ raw_tail_filter: filterText.trim() }),
        });
        savedFilter = filterText;
      } catch {
        // Filter still works for this session, just won't persist.
      }
    }
  }

  function clearScreen() {
    session?.clearScreen?.();
  }

  onMount(async () => {
    await loadSavedFilter();
    session = createMonitorSession({ channel, initialFilter: filterText });
  });

  onDestroy(() => {
    session?.close?.();
  });

  let connectionState = $derived(session?.state?.status ?? 'connecting');
  let isAPRSOnly = $derived(channel?.mode === 'aprs');
</script>

<div class="raw-view">
  {#if isAPRSOnly}
    <header class="banner" role="status">
      <div class="banner-text">
        <strong>Channel {channel?.name ?? channel?.id} is APRS-only.</strong>
        Connected-mode AX.25 is disabled on this channel. Showing the live
        packet feed instead.
        <a href="#/channels">Change channel mode in settings -&gt;</a>
      </div>
      <Badge variant="info">APRS only</Badge>
    </header>
  {/if}

  <div class="filter-row">
    <Input
      bind:value={filterText}
      placeholder="Filter substring (callsign, payload text)"
      aria-label="Monitor filter"
    />
    <Button size="sm" variant="primary" onclick={applyFilter}>Apply</Button>
    <Button size="sm" variant="ghost" onclick={clearScreen}>Clear</Button>
    <span class="status">
      <Badge variant={connectionState === 'open' ? 'success' : connectionState === 'error' ? 'danger' : 'warning'}>
        {connectionState}
      </Badge>
    </span>
  </div>

  {#if session}
    <TerminalViewport {session} />
  {/if}
</div>

<style>
  .raw-view {
    display: flex;
    flex-direction: column;
    flex: 1 1 auto;
    min-height: 0;
    gap: 6px;
  }
  .banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 10px 14px;
    background: var(--color-info-bg, #fff8d4);
    border: 1px solid var(--color-warning, #d6a800);
    border-radius: 4px;
  }
  .banner-text { font-size: 13px; }
  .banner a { margin-left: 6px; }
  .filter-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 14px;
  }
  .filter-row .status { margin-left: auto; }
</style>
