<script>
  // Live APRS packet monitor for non-pure-packet channels. Uses the
  // same xterm.js viewport that LAPB sessions render in -- the only
  // differences are the data source (raw_tail WebSocket subscription
  // instead of LAPB I-frames) and that operator keystrokes are
  // discarded (see lib/terminal/monitor.svelte.js).
  //
  // The APRS-only warning banner is rendered by the route, not here,
  // so the route can sequence it with the macro toolbar.

  import { onMount, onDestroy } from 'svelte';
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
</script>

<div class="raw-view">
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
  }
</style>
