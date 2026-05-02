<script>
  import { onMount } from 'svelte';
  import { location } from 'svelte-spa-router';

  import { Button, Icon } from '@chrissnell/chonky-ui';

  import TabBar from '../components/terminal/TabBar.svelte';
  import TerminalViewport from '../components/terminal/TerminalViewport.svelte';
  import StatusBar from '../components/terminal/StatusBar.svelte';
  import PreConnectForm from '../components/terminal/PreConnectForm.svelte';
  import TelemetryPanel from '../components/terminal/TelemetryPanel.svelte';

  import {
    terminalSessions,
  } from '../lib/terminal/sessions.svelte.js';
  import { start as startChannelsStore } from '../lib/stores/channels.svelte.js';

  // Track active route into the multi-session focus state.
  $effect(() => {
    const unsub = location.subscribe((v) => {
      terminalSessions.setRoute(v);
    });
    return unsub;
  });

  // Make sure the channels store is running so PreConnectForm has
  // something to render.
  onMount(() => {
    try { startChannelsStore({ pollMs: 5000 }); } catch { /* idempotent */ }
  });

  let activeId = $derived(terminalSessions.activeId());
  let activeSession = $derived(activeId ? terminalSessions.get(activeId) : null);

  // showForm: true when no session is active, OR when the operator
  // explicitly clicked the "+" tab (handled by TabBar -> onNew).
  let forceForm = $state(false);
  let showForm = $derived(forceForm || !activeSession);

  function onNewTab() {
    forceForm = true;
    terminalSessions.setActive(null);
  }

  function onCloseTab(id) {
    terminalSessions.close(id);
  }

  function onSubmitConnect(id) {
    forceForm = false;
    terminalSessions.setActive(id);
  }

  let telemetryOpen = $state(false);
</script>

<div class="terminal-route">
  <div class="terminal-header">
    <TabBar onNew={onNewTab} onClose={onCloseTab} />
    {#if activeSession}
      <Button
        variant="secondary"
        size="sm"
        aria-label="Toggle link telemetry panel"
        onclick={() => (telemetryOpen = !telemetryOpen)}
      >
        <Icon name="activity" size="sm" /> Telemetry
      </Button>
    {/if}
  </div>

  <div class="terminal-body">
    {#if showForm}
      <div class="form-pane">
        <PreConnectForm onSubmit={onSubmitConnect} />
      </div>
    {:else if activeSession}
      {#key activeSession.state.id}
        <TerminalViewport session={activeSession} />
        <StatusBar session={activeSession} />
      {/key}
    {/if}
  </div>

  {#if activeSession}
    <TelemetryPanel session={activeSession} bind:open={telemetryOpen} />
  {/if}
</div>

<style>
  .terminal-route {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 480px;
    background: var(--color-bg, #ffffff);
  }
  .terminal-body {
    flex: 1 1 auto;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .form-pane { padding: 16px 24px; }
  .terminal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding-right: 8px;
    border-bottom: 1px solid var(--color-border, #ddd);
  }
</style>
