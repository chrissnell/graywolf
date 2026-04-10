<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Box, LogViewer } from '@chrissnell/chonky-ui';
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
  ];

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

  let logEntries = $derived(filtered.map(pkt => {
    const calls = parseDisplay(pkt);
    return {
      time: new Date(pkt.timestamp).toLocaleString(),
      direction: pkt.direction || '',
      channel: pkt.channel || '—',
      source: calls.src,
      dest: calls.dst,
      type: pkt.type || '—',
      info: pkt.display || '',
    };
  }));

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
</script>

{#snippet dirBadge(value)}
  <span class="dir-badge" class:rx={value === 'RX'} class:tx={value === 'TX'}>{value}</span>
{/snippet}

{#snippet chBadge(value)}
  <span class="ch-badge">{value}</span>
{/snippet}

{#snippet srcCall(value)}
  <span class="col-src">{value}</span>
{/snippet}

{#snippet dstCall(value)}
  <span class="col-dst">{value}</span>
{/snippet}

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
    <LogViewer
      entries={logEntries}
      columns={[
        { key: 'time', label: 'Time', width: '140px' },
        { key: 'direction', label: 'Dir', width: '50px', render: dirBadge },
        { key: 'channel', label: 'Ch', width: '50px', render: chBadge },
        { key: 'source', label: 'Source', width: '100px', render: srcCall },
        { key: 'dest', label: 'Dest', width: '100px', render: dstCall },
        { key: 'type', label: 'Type', width: '70px' },
        { key: 'info', label: 'Info', width: '1fr' },
      ]}
      live={true}
      autoscroll={true}
      height="600px"
      showHeader={true}
      class="logs-viewer"
    />
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
  :global(.logs-viewer .log-grid-cell) {
    padding-top: 5px;
    padding-bottom: 5px;
    font-size: 13px;
  }
  :global(.logs-viewer .log-grid-header) {
    font-size: 11px;
  }
  :global(.logs-viewer .dir-badge),
  :global(.logs-viewer .ch-badge) {
    font-weight: 700;
    font-size: 10px;
    padding: 2px 5px;
    border-radius: 3px;
    display: inline-block;
    min-width: 24px;
    text-align: center;
  }
  :global(.logs-viewer .dir-badge.rx) { background: rgba(63, 185, 80, 0.2); color: var(--success); }
  :global(.logs-viewer .dir-badge.tx) { background: #ffaa00; color: #000; }
  :global(.logs-viewer .ch-badge) {
    background: var(--bg-tertiary);
    color: var(--text-secondary);
  }
  :global(.logs-viewer .col-src) {
    color: #d4a040;
    font-weight: 500;
  }
  :global(.logs-viewer .col-dst) {
    color: #58a6ff;
  }
</style>
