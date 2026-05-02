<script>
  import { onMount } from 'svelte';
  import { location } from 'svelte-spa-router';

  import { Button, Icon } from '@chrissnell/chonky-ui';

  import TabBar from '../components/terminal/TabBar.svelte';
  import TerminalViewport from '../components/terminal/TerminalViewport.svelte';
  import StatusBar from '../components/terminal/StatusBar.svelte';
  import PreConnectForm from '../components/terminal/PreConnectForm.svelte';
  import TelemetryPanel from '../components/terminal/TelemetryPanel.svelte';
  import MacroToolbar from '../components/terminal/MacroToolbar.svelte';
  import MacroEditor from '../components/terminal/MacroEditor.svelte';
  import CommandBar from '../components/terminal/CommandBar.svelte';
  import RawPacketView from '../components/terminal/RawPacketView.svelte';

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
  // explicitly clicked the "+" tab (handled by TabBar -> onNew). Also
  // false when raw-tail view has taken over the right pane.
  let forceForm = $state(false);
  let showForm = $derived(!rawTailChannel && (forceForm || !activeSession));

  function onNewTab() {
    forceForm = true;
    terminalSessions.setActive(null);
  }

  function onCloseTab(id) {
    terminalSessions.close(id);
  }

  function onSubmitConnect(id) {
    forceForm = false;
    rawTailChannel = null;
    terminalSessions.setActive(id);
  }

  function onRawTail(channel) {
    rawTailChannel = channel ?? null;
    forceForm = false;
    terminalSessions.setActive(null);
  }

  function exitRawTail() {
    rawTailChannel = null;
    forceForm = true;
  }

  let telemetryOpen = $state(false);
  let macroEditorOpen = $state(false);
  let commandBarOpen = $state(false);
  let rawTailChannel = $state(null);

  function handleKey(e) {
    // Ctrl-] (or Cmd-]) opens the command bar from anywhere on the
    // route.
    if ((e.ctrlKey || e.metaKey) && e.key === ']') {
      e.preventDefault();
      commandBarOpen = true;
      return;
    }
    // Ctrl-PageUp / Ctrl-PageDown cycle the active tab. PageUp goes
    // backward, PageDown forward; the operator never has to click a
    // tab head to cycle through open links.
    if ((e.ctrlKey || e.metaKey) && (e.key === 'PageUp' || e.key === 'PageDown')) {
      const ids = terminalSessions.ids();
      if (ids.length === 0) return;
      const cur = terminalSessions.activeId();
      const idx = cur ? ids.indexOf(cur) : -1;
      const delta = e.key === 'PageUp' ? -1 : 1;
      const next = (idx + delta + ids.length) % ids.length;
      terminalSessions.setActive(ids[next]);
      e.preventDefault();
      return;
    }
    // Esc closes any open overlay (CommandBar, MacroEditor,
    // TelemetryPanel) without disrupting the underlying session.
    if (e.key === 'Escape') {
      let consumed = false;
      if (commandBarOpen) { commandBarOpen = false; consumed = true; }
      if (macroEditorOpen) { macroEditorOpen = false; consumed = true; }
      if (telemetryOpen) { telemetryOpen = false; consumed = true; }
      if (consumed) e.preventDefault();
    }
  }

  function runCommand(cmd) {
    const parts = cmd.split(/\s+/);
    const head = parts[0];
    if (head === 'macros') {
      macroEditorOpen = true;
      return { ok: true };
    }
    if (head === 'transcript') {
      if (!activeSession) return { error: 'No active session.' };
      const arg = (parts[1] ?? '').toLowerCase();
      if (arg === 'on' || arg === '') {
        activeSession.setTranscript?.(true);
        return { ok: true };
      }
      if (arg === 'off') {
        activeSession.setTranscript?.(false);
        return { ok: true };
      }
      return { error: 'transcript on|off' };
    }
    if (head === 'clear') {
      return { error: 'Use Ctrl-L (or your terminal’s clear) to wipe the canvas.' };
    }
    return { error: `Unknown command: ${head}. Try macros, transcript, or clear.` };
  }
</script>

<svelte:window onkeydown={handleKey} />

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
    {#if rawTailChannel}
      <div class="raw-pane">
        <div class="raw-pane-actions">
          <Button size="sm" variant="ghost" onclick={exitRawTail} aria-label="Back to pre-connect form">
            <Icon name="arrow-left" size="sm" /> Back
          </Button>
        </div>
        <RawPacketView channel={rawTailChannel} />
      </div>
    {:else if showForm}
      <div class="form-pane">
        <PreConnectForm onSubmit={onSubmitConnect} onRawTail={onRawTail} />
      </div>
    {:else if activeSession}
      {#key activeSession.state.id}
        <MacroToolbar session={activeSession} onEdit={() => (macroEditorOpen = true)} />
        <TerminalViewport session={activeSession} />
        <StatusBar session={activeSession} />
      {/key}
    {/if}
    <CommandBar bind:open={commandBarOpen} onCommand={runCommand} />
  </div>

  {#if activeSession}
    <TelemetryPanel session={activeSession} bind:open={telemetryOpen} />
  {/if}
  <MacroEditor bind:open={macroEditorOpen} />
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
    position: relative; /* anchor for the CommandBar overlay */
  }
  .form-pane { padding: 16px 24px; }
  .raw-pane {
    flex: 1 1 auto;
    display: flex;
    flex-direction: column;
    min-height: 0;
    padding-top: 8px;
  }
  .raw-pane-actions { padding: 0 14px 6px; }
  .terminal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding-right: 8px;
    border-bottom: 1px solid var(--color-border, #ddd);
  }
</style>
