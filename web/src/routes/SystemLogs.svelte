<script>
  import { onMount, tick } from 'svelte';
  import { Button, Select, Box, toast } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import PageHeader from '../components/PageHeader.svelte';
  import { formatLogsForClipboard, formatAttrs, shouldAutoscroll, levelClass } from '../lib/systemLogs.js';

  let logs = $state([]);
  let available = $state(true);
  let loading = $state(true);
  let limit = $state('250');
  let level = $state('info');
  let autoscroll = $state(true);

  let container = $state(null);
  let pollTimer;

  const limitOptions = [
    { value: '50', label: '50 lines' },
    { value: '100', label: '100 lines' },
    { value: '250', label: '250 lines' },
    { value: '500', label: '500 lines' },
  ];
  const levelOptions = [
    { value: 'info', label: 'Info and above' },
    { value: 'debug', label: 'Debug (everything)' },
  ];

  onMount(() => {
    loadLogs();
    pollTimer = setInterval(loadLogs, 2000);
    return () => clearInterval(pollTimer);
  });

  async function loadLogs() {
    try {
      const resp = await api.get(`/system-logs?limit=${limit}&level=${level}`);
      logs = resp?.logs || [];
      available = resp?.available ?? false;
    } catch (_) {
      // Network failure: keep last view, surface nothing noisy.
    }
    loading = false;
    if (autoscroll) {
      await tick();
      if (container) container.scrollTop = container.scrollHeight;
    }
  }

  // Select fires onValueChange with the new value; set state from it
  // (not via a separate bind) so loadLogs reads the fresh filter
  // synchronously instead of racing the binding update.
  function onLevelChange(v) {
    level = v;
    loadLogs();
  }
  function onLimitChange(v) {
    limit = v;
    loadLogs();
  }

  function onScroll() {
    if (!container) return;
    autoscroll = shouldAutoscroll(
      container.scrollTop,
      container.scrollHeight,
      container.clientHeight,
    );
  }

  // legacyCopy mirrors components/actions/CopyableInput.svelte: operators
  // frequently reach graywolf over plain HTTP on a LAN IP, where
  // navigator.clipboard is undefined. The hidden-textarea +
  // execCommand('copy') path keeps Copy working there.
  function legacyCopy(text) {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'absolute';
    ta.style.left = '-9999px';
    document.body.appendChild(ta);
    ta.select();
    let ok = false;
    try {
      ok = document.execCommand('copy');
    } catch {
      ok = false;
    } finally {
      document.body.removeChild(ta);
    }
    return ok;
  }

  async function copyLogs() {
    const text = formatLogsForClipboard(logs);
    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(text);
        toast(`Copied ${logs.length} lines`, 'success');
        return;
      } catch (_) { /* fall through to legacy path */ }
    }
    if (legacyCopy(text)) {
      toast(`Copied ${logs.length} lines`, 'success');
      return;
    }
    toast('Copy failed; select the log text and press Cmd/Ctrl+C', 'error');
  }
</script>

<PageHeader title="System Logs" subtitle="Daemon log output (the lines also printed to the console)">
  <Button onclick={loadLogs} disabled={loading}>Refresh</Button>
  <Button onclick={copyLogs} disabled={logs.length === 0}>Copy Logs</Button>
</PageHeader>

<Box>
  <div class="filter-bar">
    <div class="filter-group">
      <label class="filter-label" for="log-level-select">Minimum log level</label>
      <div class="filter-select">
        <Select id="log-level-select" aria-label="Minimum log level" value={level} options={levelOptions} onValueChange={onLevelChange} />
      </div>
    </div>
    <div class="filter-group">
      <label class="filter-label" for="log-lines-select">Lines to show</label>
      <div class="filter-select">
        <Select id="log-lines-select" aria-label="Lines to show" value={limit} options={limitOptions} onValueChange={onLimitChange} />
      </div>
    </div>
  </div>
</Box>

<div style="margin-top: 12px;">
  {#if loading}
    <Box><div class="empty">Loading...</div></Box>
  {:else if !available}
    <Box><div class="empty">System log buffer is not available on this host.</div></Box>
  {:else if logs.length === 0}
    <Box><div class="empty">No log entries.</div></Box>
  {:else}
    <!-- tabindex makes the scroll region keyboard-focusable so arrow/page
         keys can scroll the log history without a pointer. -->
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div
      class="log-box"
      bind:this={container}
      onscroll={onScroll}
      role="log"
      aria-live="polite"
      aria-label="System logs"
      tabindex="0"
    >
      {#each logs as entry}
        <div class="log-line">
          <span class="ts">{entry.timestamp}</span>
          <span class="lvl lvl-{levelClass(entry.level)}">{entry.level}</span>
          {#if entry.component}<span class="comp">[{entry.component}]</span>{/if}
          <span class="msg">{entry.message}</span>
          {#if entry.attrs}<span class="attrs">{formatAttrs(entry.attrs)}</span>{/if}
        </div>
      {/each}
    </div>
    <div class="log-foot">
      Showing {logs.length} lines{autoscroll ? '' : ' (auto-scroll paused)'}
    </div>
  {/if}
</div>

<style>
  .filter-bar { display: flex; gap: 16px; flex-wrap: wrap; }
  .filter-group { display: flex; flex-direction: column; gap: 4px; }
  .filter-label {
    font-size: var(--text-xs, 12px);
    color: var(--color-text-dim);
    font-weight: 600;
  }
  .filter-select { width: 180px; }
  .empty { color: var(--color-text-dim); text-align: center; padding: 24px; }

  .log-box {
    max-height: 600px;
    overflow-y: auto;
    background: var(--color-surface, #0b0b0b);
    border: 1px solid var(--color-border, #333);
    border-radius: 6px;
    padding: 8px 12px;
    font-family: var(--font-mono, monospace);
    font-size: var(--text-xs, 12px);
    line-height: 1.5;
  }
  .log-line { white-space: pre-wrap; word-break: break-word; }
  .ts { color: var(--color-text-dim); margin-right: 8px; }
  .lvl { margin-right: 8px; font-weight: 600; }
  .lvl-error { color: #ff6b6b; }
  .lvl-warn { color: #ffd166; }
  .lvl-info { color: #6bcB77; }
  .lvl-debug { color: var(--color-text-dim); }
  .comp { color: #7aa2f7; margin-right: 8px; }
  .attrs { color: var(--color-text-dim); margin-left: 8px; }

  .log-foot {
    padding: 7px 14px;
    font-size: var(--text-xs);
    color: var(--color-text-dim);
    text-align: right;
  }
</style>
