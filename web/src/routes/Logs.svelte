<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import PageHeader from '../components/PageHeader.svelte';
  import PacketLogViewer from '../components/PacketLogViewer.svelte';
  import { parseDisplay } from '../lib/packetColumns.js';

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
    <PacketLogViewer
      packets={filtered}
      height="600px"
      live
      showHeader
      mobileBreakpoint="768px"
    />
    <div class="log-foot">Showing {filtered.length} of {packets.length} packets</div>
  {/if}
</div>

<style>
  .filter-bar { display: flex; gap: 10px; flex-wrap: wrap; }
  .filter-input { flex: 1; min-width: 200px; }
  .filter-select { width: 140px; }
  .empty { color: var(--color-text-dim); text-align: center; padding: 24px; }

  .log-foot {
    padding: 7px 14px;
    font-size: var(--text-xs);
    color: var(--color-text-dim);
    text-align: right;
  }
</style>
