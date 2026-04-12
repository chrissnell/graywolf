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
      case 'digipeater': return { label: 'DIGI', cls: 'digi' };
      case 'beacon':     return { label: 'BCN',  cls: 'bcn' };
      case 'igate':
        if (notes === 'is2rf') return { label: 'IS→RF', cls: 'igate-is2rf' };
        if (notes === 'rf2is') return { label: 'RF→IS', cls: 'igate-rf2is' };
        return { label: 'IGATE', cls: 'igate' };
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

  // Autoscroll logic
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

<div class="log-wrapper" style="margin-top: 12px;">
  {#if loading}
    <Box><div class="empty">Loading...</div></Box>
  {:else if filtered.length === 0}
    <Box><div class="empty">No packets match filter</div></Box>
  {:else}
    <div class="log-viewer">
      <div class="log-toolbar">
        <Dot on={true} />
        <span class="toolbar-label">live</span>
        <span class="toolbar-count">{filtered.length} entries</span>
      </div>
      <div
        class="log-body"
        bind:this={bodyEl}
        onscroll={checkAtBottom}
      >
        {#key filtered}
          {#each filtered as pkt, i}
            {@const calls = parseDisplay(pkt)}
            {@const origin = originTag(pkt)}
            {@const device = deviceLabel(pkt)}
            {@const dist = distanceLabel(pkt)}
            <div class="log-entry" class:odd={i % 2 === 1}>
              <div class="entry-line1">
                <span class="time">{formatTime(pkt.timestamp)}</span>
                <span class="dir-badge" class:rx={pkt.direction === 'RX'} class:tx={pkt.direction === 'TX'} class:is={pkt.direction === 'IS'}>{pkt.direction || ''}</span>
                {#if origin}
                  <span class="origin-badge origin-{origin.cls}">{origin.label}</span>
                {/if}
                <span class="ch-badge">{pkt.channel || '—'}</span>
                <span class="callsigns">
                  <span class="col-src">{calls.src}</span>
                  <span class="arrow">→</span>
                  <span class="col-dst">{calls.dst}</span>
                </span>
                {#if pkt.type}
                  <span class="type-badge">{pkt.type}</span>
                {/if}
                {#if device}
                  <span class="device">{device}</span>
                {/if}
                {#if dist}
                  <span class="distance">{dist}</span>
                {/if}
              </div>
              <div class="entry-line2">{pkt.display || ''}</div>
            </div>
          {/each}
        {/key}
      </div>
      {#if !isAtBottom}
        <button class="log-jump-bottom" onclick={scrollToBottom}>
          ↓ Jump to bottom
        </button>
      {/if}
    </div>
    <div class="log-footer">
      Showing {filtered.length} of {packets.length} packets
    </div>
  {/if}
</div>

<style>
  .filter-bar {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }
  .filter-input { flex: 1; min-width: 200px; }
  .filter-select { width: 140px; }
  .empty { color: var(--text-muted); text-align: center; padding: 24px; }
  .log-footer {
    padding: 8px;
    font-size: 11px;
    color: var(--text-muted);
    text-align: right;
    border-top: 1px solid var(--border-light);
  }

  /* Log viewer container */
  .log-viewer {
    position: relative;
    border: 1px solid var(--border-light);
    border-radius: 8px;
    background: var(--bg-primary);
    overflow: hidden;
  }

  .log-toolbar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px;
    font-size: 12px;
    color: var(--text-secondary);
    border-bottom: 1px solid var(--border-light);
    background: var(--bg-secondary);
  }
  .toolbar-label { font-size: 11px; }
  .toolbar-count { margin-left: auto; font-size: 11px; }

  .log-body {
    height: 600px;
    overflow-y: auto;
    font-family: 'SF Mono', 'Fira Code', 'JetBrains Mono', monospace;
  }

  .log-jump-bottom {
    position: absolute;
    bottom: 8px;
    left: 50%;
    transform: translateX(-50%);
    padding: 4px 12px;
    font-size: 11px;
    border: 1px solid var(--border-light);
    border-radius: 4px;
    background: var(--bg-secondary);
    color: var(--text-secondary);
    cursor: pointer;
    z-index: 2;
  }
  .log-jump-bottom:hover {
    background: var(--bg-tertiary);
  }

  /* Entry block (two lines) */
  .log-entry {
    padding: 5px 10px 4px;
    border-bottom: 1px solid var(--border-light, rgba(255,255,255,0.06));
  }
  .log-entry.odd {
    background: var(--bg-secondary, rgba(255,255,255,0.02));
  }

  /* Line 1: metadata */
  .entry-line1 {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    flex-wrap: wrap;
  }

  .time {
    color: var(--text-muted);
    font-size: 11px;
    white-space: nowrap;
    min-width: 100px;
  }

  .dir-badge, .ch-badge {
    font-weight: 700;
    font-size: 10px;
    padding: 1px 5px;
    border-radius: 3px;
    display: inline-block;
    min-width: 22px;
    text-align: center;
  }
  .dir-badge.rx { background: rgba(63, 185, 80, 0.2); color: var(--success); }
  .dir-badge.tx { background: #ffd968; color: #000; }
  .dir-badge.is { background: rgba(137, 87, 229, 0.25); color: #c39bff; }
  .ch-badge {
    background: var(--bg-tertiary);
    color: var(--text-secondary);
  }

  .origin-badge {
    font-weight: 700;
    font-size: 10px;
    padding: 1px 5px;
    border-radius: 3px;
    display: inline-block;
    text-align: center;
  }
  .origin-digi        { background: rgba(137, 87, 229, 0.25); color: #c39bff; }
  .origin-bcn         { background: rgba(63, 185, 80, 0.25);  color: #7ee787; }
  .origin-igate       { background: rgba(88, 166, 255, 0.25); color: #79c0ff; }
  .origin-igate-is2rf { background: rgba(255, 170, 0, 0.25);  color: #ffc863; }
  .origin-igate-rf2is { background: rgba(88, 166, 255, 0.25); color: #79c0ff; }

  .callsigns {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .col-src {
    color: #d4a040;
    font-weight: 600;
  }
  .arrow {
    color: var(--text-muted);
    font-size: 11px;
  }
  .col-dst {
    color: #58a6ff;
    font-weight: 500;
  }

  .type-badge {
    font-size: 10px;
    padding: 1px 5px;
    border-radius: 3px;
    background: rgba(255, 255, 255, 0.06);
    color: var(--text-secondary);
  }

  .device {
    font-size: 10px;
    color: #8b949e;
    font-style: italic;
  }

  .distance {
    font-size: 10px;
    color: #7ee787;
    white-space: nowrap;
  }

  /* Line 2: raw packet */
  .entry-line2 {
    font-size: 11px;
    color: var(--text-muted);
    margin-top: 2px;
    padding-left: 2px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
