<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Box, Dot } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import PageHeader from '../components/PageHeader.svelte';

  let packets = $state([]);
  let filter = $state('');
  let dirFilter = $state('all');
  let limit = $state('100');
  let loading = $state(true);

  const dirOptions = [
    { value: 'all', label: 'All' },
    { value: 'rx', label: 'RX Only' },
    { value: 'tx', label: 'TX Only' },
    { value: 'is', label: 'IS Only' },
  ];

  function originTag(pkt) {
    const src = pkt.source || '';
    const notes = pkt.notes || '';
    switch (src) {
      case 'digipeater': return { label: 'Digipeater', cls: 'digi' };
      case 'beacon':     return { label: 'Beacon',     cls: 'bcn' };
      case 'igate':
        if (notes === 'is2rf') return { label: 'iGate IS\u2192RF', cls: 'igate-is2rf' };
        if (notes === 'rf2is') return { label: 'iGate RF\u2192IS', cls: 'igate-rf2is' };
        return { label: 'iGate', cls: 'igate' };
      default: return null;
    }
  }

  let pollTimer;

  onMount(() => {
    loadPackets();
    pollTimer = setInterval(loadPackets, 2000);
    return () => clearInterval(pollTimer);
  });

  async function loadPackets() {
    try {
      packets = await api.get(`/packets?limit=${limit}`) || [];
    } catch (_) { /* mock fallback */ }
    loading = false;
  }

  function parseDisplay(pkt) {
    const d = pkt.decoded;
    if (d?.source) return { src: d.source, dst: d.dest || '' };
    const s = pkt.display || '';
    const gt = s.indexOf('>');
    if (gt < 0) return { src: '', dst: '' };
    const src = s.substring(0, gt);
    const rest = s.substring(gt + 1);
    const end = rest.search(/[,:]/);
    const dst = end >= 0 ? rest.substring(0, end) : rest;
    return { src, dst };
  }

  function deviceLabel(pkt) {
    const dev = pkt.device;
    if (!dev) return '';
    if (dev.vendor && dev.model) return `${dev.vendor} ${dev.model}`;
    return dev.model || dev.vendor || '';
  }

  function distanceLabel(pkt) {
    if (pkt.distance_mi == null) return '';
    const d = pkt.distance_mi;
    const label = d < 1 ? `${(d * 5280).toFixed(0)} ft` : `${d.toFixed(1)} mi`;
    return pkt.via ? `${label} via ${pkt.via}` : label;
  }

  function formatTime(ts) {
    const d = new Date(ts);
    const mo = d.getMonth() + 1;
    const day = d.getDate();
    const h = d.getHours().toString().padStart(2, '0');
    const m = d.getMinutes().toString().padStart(2, '0');
    const s = d.getSeconds().toString().padStart(2, '0');
    return `${mo}/${day} ${h}:${m}:${s}`;
  }

  let filtered = $derived.by(() => {
    let list = packets;
    if (dirFilter !== 'all') {
      const want = dirFilter.toUpperCase();
      list = list.filter((p) => (p.direction || '').toUpperCase() === want);
    }
    if (filter.trim()) {
      const q = filter.toLowerCase();
      list = list.filter((p) => {
        const { src, dst } = parseDisplay(p);
        return src.toLowerCase().includes(q) ||
          dst.toLowerCase().includes(q) ||
          (p.display || '').toLowerCase().includes(q);
      });
    }
    return list;
  });

  function exportCsv() {
    const rows = filtered.map((p) => {
      const { src, dst } = parseDisplay(p);
      return `"${p.timestamp}","${p.direction}","${src}","${dst}","${p.display || ''}"`;
    });
    const csv = 'Timestamp,Direction,Source,Destination,Display\n' + rows.join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'packets.csv';
    a.click();
    URL.revokeObjectURL(url);
  }

  let bodyEl = $state();
  let isAtBottom = $state(true);

  function checkAtBottom() {
    if (!bodyEl) return;
    isAtBottom = bodyEl.scrollHeight - bodyEl.scrollTop - bodyEl.clientHeight < 2;
  }

  function scrollToBottom() {
    if (bodyEl) {
      bodyEl.scrollTop = bodyEl.scrollHeight;
      isAtBottom = true;
    }
  }

  $effect(() => {
    void filtered;
    if (isAtBottom && bodyEl) {
      requestAnimationFrame(() => {
        if (bodyEl) bodyEl.scrollTop = bodyEl.scrollHeight;
      });
    }
  });
</script>

<PageHeader title="Packet Logs" subtitle="Packet log viewer with filter/search">
  <Button onclick={loadPackets} disabled={loading}>Refresh</Button>
  <Button onclick={exportCsv}>Export CSV</Button>
</PageHeader>

<Box>
  <div class="filter-bar">
    <div class="filter-input">
      <Input bind:value={filter} placeholder="Search callsign, destination, raw..." />
    </div>
    <div class="filter-select">
      <Select bind:value={dirFilter} options={dirOptions} />
    </div>
    <div class="filter-select">
      <Select bind:value={limit} options={[
        { value: '50', label: '50 packets' },
        { value: '100', label: '100 packets' },
        { value: '500', label: '500 packets' },
        { value: '1000', label: '1000 packets' },
      ]} />
    </div>
  </div>
</Box>

<div style="margin-top: 12px;">
  {#if loading}
    <Box><div class="empty">Loading...</div></Box>
  {:else if filtered.length === 0}
    <Box><div class="empty">No packets match filter</div></Box>
  {:else}
    <div class="log-panel">
      <!-- Toolbar -->
      <div class="log-toolbar">
        <Dot on={true} />
        <span class="tb-label">live</span>
        <span class="tb-count">{filtered.length} entries</span>
      </div>

      <!-- Scrollable body -->
      <div class="log-scroll" bind:this={bodyEl} onscroll={checkAtBottom}>
        <!-- Sticky column header -->
        <div class="pkt-hdr">
          <span class="c-time">Time</span>
          <span class="c-dir">Direction</span>
          <span class="c-origin">Origin</span>
          <span class="c-ch">Ch</span>
          <span class="c-src">Source</span>
          <span class="c-dst">Dest</span>
          <span class="c-type">Type</span>
          <span class="c-device">Device</span>
          <span class="c-dist">Distance</span>
        </div>

        {#key filtered}
          {#each filtered as pkt, i}
            {@const calls = parseDisplay(pkt)}
            {@const origin = originTag(pkt)}
            {@const device = deviceLabel(pkt)}
            {@const dist = distanceLabel(pkt)}
            {@const dir = (pkt.direction || '').toUpperCase()}
            <div class="pkt-entry" class:pkt-alt={i % 2 === 1}>
              <!-- Row 1: structured fields -->
              <div class="pkt-row">
                <span class="c-time">{formatTime(pkt.timestamp)}</span>
                <span class="c-dir">
                  <span class="badge" class:b-rx={dir === 'RX'} class:b-tx={dir === 'TX'} class:b-is={dir === 'IS'}>
                    {dir === 'RX' ? 'Received' : dir === 'TX' ? 'Transmitted' : dir === 'IS' ? 'APRS-IS' : dir}
                  </span>
                </span>
                <span class="c-origin">
                  {#if origin}
                    <span class="badge o-{origin.cls}">{origin.label}</span>
                  {:else}
                    <span class="dim">&mdash;</span>
                  {/if}
                </span>
                <span class="c-ch">{pkt.channel ?? '\u2014'}</span>
                <span class="c-src val-src">{calls.src}</span>
                <span class="c-dst val-dst">{calls.dst}</span>
                <span class="c-type">
                  {#if pkt.type}<span class="badge b-type">{pkt.type}</span>{:else}<span class="dim">&mdash;</span>{/if}
                </span>
                <span class="c-device dim">{device || '\u2014'}</span>
                <span class="c-dist">
                  {#if dist}<span class="val-dist">{dist}</span>{:else}<span class="dim">&mdash;</span>{/if}
                </span>
              </div>
              <!-- Row 2: raw packet -->
              <div class="pkt-raw">{pkt.display || ''}</div>
            </div>
          {/each}
        {/key}
      </div>

      {#if !isAtBottom}
        <button class="jump-btn" onclick={scrollToBottom}>↓ Jump to bottom</button>
      {/if}
    </div>
    <div class="log-foot">Showing {filtered.length} of {packets.length} packets</div>
  {/if}
</div>

<style>
  /* ── layout ─────────────────────────────────────── */
  .filter-bar { display: flex; gap: 10px; flex-wrap: wrap; }
  .filter-input { flex: 1; min-width: 200px; }
  .filter-select { width: 140px; }
  .empty { color: var(--color-text-dim); text-align: center; padding: 24px; }

  .log-panel {
    position: relative;
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-bg);
    overflow: hidden;
  }

  .log-toolbar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 7px 14px;
    border-bottom: 1px solid var(--color-border);
    background: var(--color-surface);
  }
  .tb-label {
    font-size: var(--text-xs);
    color: var(--color-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .tb-count {
    margin-left: auto;
    font-size: var(--text-xs);
    color: var(--color-text-dim);
  }

  .log-scroll {
    height: 600px;
    overflow-y: auto;
    overflow-x: auto;
  }

  .log-foot {
    padding: 7px 14px;
    font-size: var(--text-xs);
    color: var(--color-text-dim);
    text-align: right;
    border-top: 1px solid var(--color-border-subtle);
  }

  .jump-btn {
    position: absolute;
    bottom: 10px;
    left: 50%;
    transform: translateX(-50%);
    padding: 5px 14px;
    font-size: var(--text-xs);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-surface-raised);
    color: var(--color-text-muted);
    cursor: pointer;
    z-index: 2;
    box-shadow: 0 2px 8px rgba(0,0,0,0.4);
  }
  .jump-btn:hover { background: var(--color-btn-bg-hover); }

  /* ── column widths (shared between header + rows) ── */
  .c-time   { width: 100px; flex-shrink: 0; }
  .c-dir    { width: 100px; flex-shrink: 0; }
  .c-origin { width: 115px; flex-shrink: 0; }
  .c-ch     { width: 30px;  flex-shrink: 0; text-align: center; }
  .c-src    { width: 100px; flex-shrink: 0; }
  .c-dst    { width: 80px;  flex-shrink: 0; }
  .c-type   { width: 85px;  flex-shrink: 0; }
  .c-device { flex: 1 1 0;  min-width: 80px; }
  .c-dist   { width: 175px; flex-shrink: 0; text-align: right; }

  /* ── sticky header ──────────────────────────────── */
  .pkt-hdr {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    position: sticky;
    top: 0;
    z-index: 1;
    background: var(--color-bg);
    border-bottom: 1px solid var(--color-border);
    font-size: var(--text-xs);
    font-weight: 500;
    color: var(--color-text-dim);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  /* ── entry block ────────────────────────────────── */
  .pkt-entry {
    border-bottom: 1px solid var(--color-border-subtle);
  }
  .pkt-alt {
    background: var(--color-surface);
  }

  .pkt-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 12px 2px;
    font-size: var(--text-sm);
  }

  .pkt-raw {
    padding: 0 12px 5px 12px;
    font-size: 0.65rem;
    color: var(--color-text-dim);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    line-height: 1.5;
  }

  /* ── badges ─────────────────────────────────────── */
  .badge {
    display: inline-block;
    font-weight: 700;
    font-size: 10px;
    padding: 2px 6px;
    border-radius: 3px;
    white-space: nowrap;
    text-align: center;
    line-height: 1.4;
  }

  /* Direction badges */
  .b-rx { background: var(--color-success-muted); color: var(--color-success); }
  .b-tx { background: var(--color-warning-muted); color: var(--color-warning); }
  .b-is { background: var(--color-info-muted);    color: var(--color-info); }

  /* Origin badges */
  .o-digi        { background: var(--color-info-muted);    color: var(--color-info); }
  .o-bcn         { background: var(--color-success-muted); color: var(--color-success); }
  .o-igate       { background: var(--color-info-muted);    color: var(--color-info); }
  .o-igate-is2rf { background: var(--color-warning-muted); color: var(--color-warning); }
  .o-igate-rf2is { background: var(--color-info-muted);    color: var(--color-info); }

  /* Type badge */
  .b-type {
    background: var(--color-surface-raised);
    color: var(--color-text-muted);
    font-weight: 500;
  }

  /* ── value colors ───────────────────────────────── */
  .val-src {
    color: var(--color-warning);
    font-weight: 600;
  }
  .val-dst {
    color: var(--color-info);
  }
  .val-dist {
    font-size: var(--text-xs);
    color: var(--color-success);
  }
  .dim {
    color: var(--color-text-dim);
  }
</style>
