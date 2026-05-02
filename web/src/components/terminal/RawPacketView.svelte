<script>
  // Read-only TNC2 tail for APRS-only channels. Opens its own
  // WebSocket against /api/ax25/terminal, sends raw_tail_subscribe,
  // and renders incoming raw_tail envelopes. Filter input narrows
  // server-side via the substring/source/type match. The filter is
  // persisted per channel via AX25TerminalConfig.RawTailFilter so the
  // operator's pick survives a route refresh.

  import { onMount, onDestroy } from 'svelte';
  import { Badge, Button, Icon, Input } from '@chrissnell/chonky-ui';

  let { channel } = $props();

  let entries = $state([]);
  let connectionState = $state('connecting'); // connecting|open|closed|error
  let filterText = $state('');
  let savedFilter = $state('');

  let ws = null;
  const MAX_ENTRIES = 500;

  function url() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${proto}//${location.host}/api/ax25/terminal`;
  }

  async function loadSavedFilter() {
    try {
      const r = await fetch('/api/ax25/terminal-config', { credentials: 'same-origin' });
      if (!r.ok) return;
      const cfg = await r.json();
      savedFilter = cfg?.raw_tail_filter ?? '';
      filterText = savedFilter;
    } catch {
      // Non-fatal — operator can still type a filter manually.
    }
  }

  function open() {
    try {
      ws = new WebSocket(url());
    } catch (err) {
      connectionState = 'error';
      return;
    }
    ws.binaryType = 'arraybuffer';
    ws.onopen = () => {
      connectionState = 'open';
      sendSubscribe();
    };
    ws.onmessage = (ev) => {
      let env;
      try {
        env = JSON.parse(typeof ev.data === 'string' ? ev.data : new TextDecoder().decode(ev.data));
      } catch {
        return;
      }
      if (env.kind === 'raw_tail' && env.raw_tail) {
        entries = [...entries.slice(-(MAX_ENTRIES - 1)), env.raw_tail];
      }
    };
    ws.onerror = () => { connectionState = 'error'; };
    ws.onclose = () => { connectionState = 'closed'; };
  }

  function sendSubscribe() {
    if (!ws || ws.readyState !== 1) return;
    const args = { channel_id: channel?.id ?? 0 };
    if (filterText.trim()) args.substring = filterText.trim();
    try {
      ws.send(JSON.stringify({ kind: 'raw_tail_subscribe', raw_tail_sub: args }));
    } catch {
      // ignore — onclose will resurface error
    }
  }

  async function applyFilter() {
    sendSubscribe();
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
        // Non-fatal: filter still works for this session, just won't persist.
      }
    }
  }

  function clearEntries() {
    entries = [];
  }

  onMount(async () => {
    await loadSavedFilter();
    open();
  });

  onDestroy(() => {
    try { ws?.close(1000, 'view closed'); } catch {}
  });

  function renderEntry(e) {
    const ts = new Date(e.ts).toISOString().slice(11, 19);
    const src = e.from || '';
    const head = src ? `${src}` : (e.source ?? '');
    const raw = (e.raw ?? '').replace(/[\x00-\x1f\x7f]/g, '?');
    return `${ts} ${head} ${raw}`.trim();
  }
</script>

<div class="raw-view">
  <header class="banner" role="status">
    <div class="banner-text">
      <strong>Channel {channel?.name ?? channel?.id} is APRS-only.</strong>
      Connected-mode AX.25 is disabled on this channel. Showing the live
      packet feed instead.
      <a href={`#/channels/${channel?.id ?? ''}`}>Change channel mode in settings -&gt;</a>
    </div>
    <Badge variant="info">APRS only</Badge>
  </header>

  <div class="filter-row">
    <Input
      bind:value={filterText}
      placeholder="Filter substring (callsign, payload text)"
      aria-label="Raw-tail filter"
    />
    <Button size="sm" variant="primary" onclick={applyFilter}>Apply</Button>
    <Button size="sm" variant="ghost" onclick={clearEntries}>Clear</Button>
    <span class="status">
      <Badge variant={connectionState === 'open' ? 'success' : connectionState === 'error' ? 'danger' : 'warning'}>
        {connectionState}
      </Badge>
    </span>
  </div>

  <ul class="log" aria-live="polite" aria-relevant="additions">
    {#each entries as e (e.ts + (e.from ?? '') + (e.raw ?? ''))}
      <li class="entry">
        <span class="ts">{new Date(e.ts).toISOString().slice(11, 19)}</span>
        <span class="from mono">{e.from ?? e.source ?? ''}</span>
        <span class="raw mono">{(e.raw ?? '').replace(/[\x00-\x1f\x7f]/g, '?')}</span>
      </li>
    {/each}
    {#if entries.length === 0}
      <li class="empty">Waiting for packets...</li>
    {/if}
  </ul>
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
  .log {
    list-style: none;
    margin: 0;
    padding: 0 14px 14px;
    overflow-y: auto;
    flex: 1 1 auto;
    min-height: 0;
    background: var(--color-bg, #ffffff);
    font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
    font-size: 12px;
  }
  .entry {
    display: grid;
    grid-template-columns: 80px 120px 1fr;
    gap: 8px;
    padding: 2px 4px;
    border-bottom: 1px solid var(--color-border, #ececec);
  }
  .entry:hover { background: var(--color-surface, #f4f4f4); }
  .ts { color: var(--color-text-muted, #888); }
  .from { font-weight: 600; }
  .empty { color: var(--color-text-muted, #888); padding: 12px 4px; font-family: inherit; }
</style>
