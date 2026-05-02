<script>
  // Browse and prune persisted AX.25 transcripts. List view shows
  // peer, duration, size, and a click target that fetches the entry
  // detail. Delete + Delete-all buttons trip the matching DELETE
  // endpoints.

  import { onMount } from 'svelte';
  import { Button, EmptyState, AlertDialog, Icon } from '@chrissnell/chonky-ui';

  let rows = $state([]);
  let loading = $state(true);
  let loadError = $state('');
  let expandedID = $state(null);
  let detail = $state(null);
  let detailLoading = $state(false);
  let detailError = $state('');
  let confirmAll = $state(false);

  onMount(load);

  async function load() {
    loading = true;
    loadError = '';
    try {
      const r = await fetch('/api/ax25/transcripts', { credentials: 'same-origin' });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      rows = await r.json();
    } catch (err) {
      loadError = String(err.message ?? err);
    } finally {
      loading = false;
    }
  }

  async function expand(row) {
    if (expandedID === row.id) {
      expandedID = null;
      detail = null;
      return;
    }
    expandedID = row.id;
    detail = null;
    detailLoading = true;
    detailError = '';
    try {
      const r = await fetch(`/api/ax25/transcripts/${row.id}`, { credentials: 'same-origin' });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      detail = await r.json();
    } catch (err) {
      detailError = String(err.message ?? err);
    } finally {
      detailLoading = false;
    }
  }

  async function removeOne(row) {
    if (!confirm(`Delete transcript with ${labelFor(row)}?`)) return;
    try {
      const r = await fetch(`/api/ax25/transcripts/${row.id}`, {
        method: 'DELETE',
        credentials: 'same-origin',
      });
      if (!r.ok && r.status !== 204) throw new Error(`HTTP ${r.status}`);
    } catch (err) {
      loadError = String(err.message ?? err);
      return;
    }
    if (expandedID === row.id) {
      expandedID = null;
      detail = null;
    }
    await load();
  }

  async function removeAll() {
    confirmAll = false;
    try {
      const r = await fetch('/api/ax25/transcripts', {
        method: 'DELETE',
        credentials: 'same-origin',
      });
      if (!r.ok && r.status !== 204) throw new Error(`HTTP ${r.status}`);
    } catch (err) {
      loadError = String(err.message ?? err);
      return;
    }
    expandedID = null;
    detail = null;
    await load();
  }

  function labelFor(row) {
    const ssid = row.peer_ssid ? `-${row.peer_ssid}` : '';
    return `${row.peer_call}${ssid}`;
  }

  function durationOf(row) {
    if (!row.ended_at) return '(open)';
    const start = Date.parse(row.started_at);
    const end = Date.parse(row.ended_at);
    if (Number.isNaN(start) || Number.isNaN(end) || end <= start) return '--';
    const sec = Math.round((end - start) / 1000);
    if (sec < 60) return `${sec}s`;
    if (sec < 3600) return `${Math.floor(sec / 60)}m ${sec % 60}s`;
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    return `${h}h ${m}m`;
  }

  // decodePayload renders bytes back into a readable string. Bytes
  // < 0x20 (except CR/LF/TAB) and 0x7F render as \xNN escapes so the
  // operator can see control codes in the transcript.
  function decodePayload(b64) {
    if (!b64) return '';
    const bin = atob(b64);
    let out = '';
    for (let i = 0; i < bin.length; i++) {
      const c = bin.charCodeAt(i);
      if (c === 0x0d) { out += '\r'; continue; }
      if (c === 0x0a) { out += '\n'; continue; }
      if (c === 0x09) { out += '\t'; continue; }
      if (c < 0x20 || c === 0x7f) {
        out += '\\x' + c.toString(16).padStart(2, '0');
        continue;
      }
      out += String.fromCharCode(c);
    }
    return out;
  }
</script>

<div class="page">
  <header class="row-header">
    <h1>Transcripts</h1>
    <div class="actions">
      <Button variant="secondary" size="sm" onclick={load}>
        <Icon name="refresh-cw" size="sm" /> Refresh
      </Button>
      <Button variant="danger" size="sm" disabled={rows.length === 0} onclick={() => (confirmAll = true)}>
        <Icon name="trash" size="sm" /> Delete all
      </Button>
    </div>
  </header>

  {#if loadError}
    <p class="err" role="alert">{loadError}</p>
  {/if}

  {#if loading}
    <p class="muted">Loading...</p>
  {:else if rows.length === 0}
    <EmptyState
      title="No transcripts saved"
      description="Toggle transcript on inside a session (Ctrl+] then 'transcript on') to start recording."
    />
  {:else}
    <ul class="list">
      {#each rows as row (row.id)}
        <li class="row">
          <button type="button" class="row-toggle" onclick={() => expand(row)} aria-expanded={expandedID === row.id}>
            <span class="peer">{labelFor(row)}</span>
            <span class="meta">{new Date(row.started_at).toLocaleString()}</span>
            <span class="meta">{durationOf(row)}</span>
            <span class="meta">{row.byte_count} bytes / {row.frame_count} frames</span>
            {#if row.end_reason}<span class="reason">{row.end_reason}</span>{/if}
          </button>
          <Button variant="ghost" size="sm" onclick={() => removeOne(row)} aria-label={`Delete transcript ${labelFor(row)}`}>
            <Icon name="trash" size="sm" />
          </Button>
          {#if expandedID === row.id}
            <div class="detail">
              {#if detailLoading}
                <p class="muted">Loading entries...</p>
              {:else if detailError}
                <p class="err">{detailError}</p>
              {:else if detail}
                <pre>
{#each detail.entries as e (e.id)}{new Date(e.ts).toISOString()} {e.direction.toUpperCase()} {e.kind}: {decodePayload(e.payload)}
{/each}
                </pre>
              {/if}
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  <p class="back"><a href="#/terminal">Back to terminal</a></p>
</div>

<AlertDialog bind:open={confirmAll}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete every transcript?</AlertDialog.Title>
    <AlertDialog.Description>
      This permanently removes every saved transcript session and entry.
    </AlertDialog.Description>
    <div class="confirm-actions">
      <AlertDialog.Cancel onclick={() => (confirmAll = false)}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action onclick={removeAll}>Delete everything</AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<style>
  .page { padding: 16px 24px; max-width: 960px; display: flex; flex-direction: column; gap: 12px; }
  h1 { font-size: 1.4rem; margin: 0; }
  .row-header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .actions { display: flex; gap: 8px; }
  .list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
  .row {
    border: 1px solid var(--color-border, #ddd);
    border-radius: 4px;
    padding: 8px 10px;
    background: var(--color-surface, #f9f9f9);
    display: flex;
    align-items: flex-start;
    gap: 8px;
    flex-wrap: wrap;
  }
  .row-toggle {
    flex: 1 1 auto;
    text-align: left;
    background: transparent;
    border: none;
    padding: 0;
    font: inherit;
    cursor: pointer;
    color: var(--color-text, #222);
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
  }
  .peer { font-weight: 600; }
  .meta { color: var(--color-text-muted, #666); font-size: 13px; }
  .reason { font-size: 12px; padding: 2px 6px; border-radius: 3px; background: var(--color-bg, #fff); border: 1px solid var(--color-border, #ddd); }
  .detail { flex-basis: 100%; margin-top: 8px; }
  .detail pre {
    font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
    font-size: 12px;
    background: var(--color-bg, #fff);
    border: 1px solid var(--color-border, #ddd);
    padding: 8px;
    border-radius: 4px;
    overflow-x: auto;
    max-height: 480px;
  }
  .muted { color: var(--color-text-muted, #666); margin: 0; }
  .err { color: var(--color-danger, #c41010); margin: 0; }
  .back { font-size: 12px; }
  .confirm-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 12px; }
</style>
