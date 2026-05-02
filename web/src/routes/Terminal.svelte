<script>
  import { onMount } from 'svelte';
  import { location } from 'svelte-spa-router';

  import { Button, Icon, AlertDialog } from '@chrissnell/chonky-ui';

  // Error codes the bridge surfaces that warrant an AlertDialog rather
  // than just a StatusBar entry. The link-establish path and a peer
  // rejection are non-recoverable for the operator's current attempt
  // and they often miss the bottom-of-route status text. Other codes
  // (rx_overflow, transcript_*) stay inline.
  const FATAL_ERROR_CODES = new Set([
    'link-establish-timeout',
    'peer-rejected',
  ]);

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

  let fatalDialog = $state({ open: false, code: '', message: '' });

  $effect(() => {
    const err = activeSession?.state?.lastError;
    if (!err || !FATAL_ERROR_CODES.has(err.code)) return;
    fatalDialog = { open: true, code: err.code, message: err.message };
  });

  function fatalErrorTitle(code) {
    if (code === 'link-establish-timeout') {
      return 'Link did not come up';
    }
    if (code === 'peer-rejected') return 'Peer refused the connection';
    return 'Session error';
  }

  function dismissFatal() {
    fatalDialog = { ...fatalDialog, open: false };
    activeSession?.clearLastError?.();
  }

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
    return { error: `Unknown command: ${head}. Try transcript or clear. Macros: use the toolbar button.` };
  }
</script>

<svelte:window onkeydown={handleKey} />

<div class="terminal-route">
  {#if terminalSessions.ids().length > 0 || activeSession}
    <div class="terminal-header">
      <TabBar onNew={onNewTab} onClose={onCloseTab} />
      {#if activeSession}
        <Button
          variant="default"
          size="sm"
          aria-label="Toggle link telemetry panel"
          onclick={() => (telemetryOpen = !telemetryOpen)}
        >
          <Icon name="radio" size="sm" /> Telemetry
        </Button>
      {/if}
    </div>
  {/if}

  <div class="terminal-body">
    {#if rawTailChannel}
      <div class="session-pane">
        <div class="session-back">
          <Button variant="default" onclick={exitRawTail} aria-label="Back to pre-connect form">
            <Icon name="chevron-left" size="sm" /> Back
          </Button>
        </div>
        <MacroToolbar session={null} onEdit={() => (macroEditorOpen = true)} />
        <RawPacketView channel={rawTailChannel} />
      </div>
    {:else if showForm}
      <div class="form-pane">
        <PreConnectForm onSubmit={onSubmitConnect} onRawTail={onRawTail} />
      </div>
    {:else if activeSession}
      {#key activeSession.state.id}
        <div class="session-pane">
          <div class="session-back">
            <Button variant="default" onclick={onNewTab} aria-label="Back to pre-connect form">
              <Icon name="chevron-left" size="sm" /> Back
            </Button>
          </div>
          <MacroToolbar session={activeSession} onEdit={() => (macroEditorOpen = true)} />
          <TerminalViewport session={activeSession} />
          <StatusBar session={activeSession} />
        </div>
      {/key}
    {/if}
    <CommandBar bind:open={commandBarOpen} onCommand={runCommand} />
  </div>

  {#if activeSession}
    <TelemetryPanel session={activeSession} bind:open={telemetryOpen} />
  {/if}
  <MacroEditor bind:open={macroEditorOpen} />

  <AlertDialog bind:open={fatalDialog.open}>
    <AlertDialog.Content>
      <AlertDialog.Title>{fatalErrorTitle(fatalDialog.code)}</AlertDialog.Title>
      <AlertDialog.Description>
        {fatalDialog.message || 'Link could not be established.'}
      </AlertDialog.Description>
      <div class="fatal-actions">
        <AlertDialog.Action onclick={dismissFatal}>Dismiss</AlertDialog.Action>
      </div>
    </AlertDialog.Content>
  </AlertDialog>
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
  .session-pane {
    flex: 1 1 auto;
    display: flex;
    flex-direction: column;
    min-height: 0;
    padding-top: 12px;
  }
  .session-back { padding: 0 14px 10px; }
  .fatal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 12px; }
  .terminal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding-right: 8px;
    border-bottom: 1px solid var(--color-border, #ddd);
  }
</style>
