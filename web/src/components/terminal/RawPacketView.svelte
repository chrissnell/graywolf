<script>
  // Live APRS packet monitor for non-pure-packet channels. Uses the
  // same xterm.js viewport that LAPB sessions render in -- the only
  // differences are the data source (raw_tail WebSocket subscription
  // instead of LAPB I-frames) and that operator keystrokes are
  // discarded (see lib/terminal/monitor.svelte.js).

  import { onMount, onDestroy } from 'svelte';
  import { Badge } from '@chrissnell/chonky-ui';
  import TerminalViewport from './TerminalViewport.svelte';

  import { createMonitorSession } from '../../lib/terminal/monitor.svelte.js';

  let { channel } = $props();

  let session = $state(null);

  onMount(() => {
    session = createMonitorSession({ channel });
  });

  onDestroy(() => {
    session?.close?.();
  });

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

  {#if session}
    <TerminalViewport {session} fitToWidth />
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
</style>
