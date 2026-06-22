<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { online } from '../lib/stores/connection.js';
  import PageHeader from '../components/PageHeader.svelte';
  import PacketLogViewer from '../components/PacketLogViewer.svelte';
  import { parseDisplay } from '../lib/packetColumns.js';
  import { start as startChannelsStore, getChannel } from '../lib/stores/channels.svelte.js';
  import { logPrefsState } from '../lib/settings/log-prefs-store.svelte.js';

  // Resolve a packet's channel id to its operator-given name for the
  // CSV export; fall back to the raw id when the channel list hasn't
  // loaded or the channel was deleted.
  function channelName(p) {
    if (p.channel == null) return '';
    return getChannel(p.channel)?.name ?? String(p.channel);
  }

  // RFC 4180 cell: wrap in quotes and double any embedded quotes so
  // user-defined channel names with commas or quotes don't corrupt rows.
  const csvCell = (v) => `"${String(v ?? '').replace(/"/g, '""')}"`;

  let packets = $state([]);
  let filter = $state('');
  let dirFilter = $state('all');
  let limit = $state('100');
  let loading = $state(true);

  let offline = $derived(!$online);

  // Drop the held packets when contact is lost so the viewer shows no
  // entries alongside the error indicator, rather than a frozen log that
  // looks current (GH #365).
  $effect(() => {
    if (!$online) packets = [];
  });

  const dirOptions = [
    { value: 'all', label: 'All' },
    { value: 'rx', label: 'RX Only' },
    { value: 'tx', label: 'TX Only' },
    { value: 'is', label: 'IS Only' },
  ];

  // Seed the search box from ?callsign=… on the hash route. The map's
  // station popup deep-links here ("APRS logs") so the operator lands on
  // a log already scoped to that station. svelte-spa-router doesn't hand
  // us the query string directly; read it off the hash like Beacons.svelte.
  function callsignFromHash() {
    if (typeof window === 'undefined') return '';
    const h = window.location.hash || '';
    const qIdx = h.indexOf('?');
    if (qIdx < 0) return '';
    return new URLSearchParams(h.slice(qIdx + 1)).get('callsign') || '';
  }

  onMount(() => {
    const seed = callsignFromHash();
    if (seed) filter = seed;
    // Idempotent — keeps the shared channel list fresh so the CSV
    // export can map channel ids to names.
    startChannelsStore();
    loadPackets();
  });

  // Poll for new packets only while auto-refresh is enabled. Turning the
  // toggle off clears the timer so the displayed list freezes and stops
  // shifting under the operator while they read a packet path; the manual
  // Refresh button still fetches on demand. (GH #373)
  $effect(() => {
    if (!logPrefsState.autoRefresh) return;
    const timer = setInterval(loadPackets, 2000);
    return () => clearInterval(timer);
  });

  async function loadPackets() {
    try {
      packets = await api.get(`/packets?limit=${limit}`) || [];
    } catch (_) {
      // On a lost connection api.get throws; the `offline` state (driven by
      // the connection store) renders the error indicator, and the $effect
      // above clears any stale packets.
    }
    loading = false;
  }

  // Auto-refresh / auto-scroll switches live in the log viewer's own toolbar
  // (Chonky's LogViewer toolbarToggles), so the controls sit in the table
  // header next to the live indicator rather than in the filter bar (GH #373).
  const toolbarToggles = $derived([
    {
      label: 'Auto-refresh',
      checked: logPrefsState.autoRefresh,
      onChange: (v) => logPrefsState.setAutoRefresh(v),
      title: 'Poll for new packets every few seconds',
    },
    {
      label: 'Auto-scroll',
      checked: logPrefsState.autoScroll,
      onChange: (v) => logPrefsState.setAutoScroll(v),
      title: 'Follow new packets to the bottom',
    },
    {
      label: 'Non-ASCII data',
      checked: logPrefsState.showNonPrintable,
      onChange: (v) => logPrefsState.setShowNonPrintable(v),
      title: 'Show non-printable bytes as <0x7f> hex tokens',
    },
  ]);

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
      return [p.timestamp, p.direction, channelName(p), src, dst, p.display || '']
        .map(csvCell).join(',');
    });
    const csv = 'Timestamp,Direction,Channel,Source,Destination,Display\n' + rows.join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'packets.csv';
    a.click();
    URL.revokeObjectURL(url);
  }
</script>

<PageHeader title="APRS Logs" subtitle="Packet log viewer with filter/search">
  <span class="conn-status" class:error={offline} aria-live="polite">
    <span class="conn-dot"></span>{offline ? 'error' : 'live'}
  </span>
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
  {#if offline}
    <Box><div class="empty">No log entries — connection to the Graywolf server lost.</div></Box>
  {:else if loading}
    <Box><div class="empty">Loading...</div></Box>
  {:else if filtered.length === 0}
    <Box><div class="empty">No packets match filter</div></Box>
  {:else}
    <PacketLogViewer
      packets={filtered}
      height="600px"
      live={logPrefsState.autoRefresh}
      autoscroll={logPrefsState.autoScroll}
      {toolbarToggles}
      showNonPrintable={logPrefsState.showNonPrintable}
      showHeader
      mobileBreakpoint="768px"
      inspectable
    />
    <div class="log-foot">Showing {filtered.length} of {packets.length} packets</div>
  {/if}
</div>

<style>
  .conn-status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--text-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--color-success);
  }
  .conn-status.error { color: var(--color-danger); }
  .conn-status .conn-dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--color-success);
  }
  .conn-status.error .conn-dot {
    background: var(--color-danger);
    box-shadow: 0 0 8px var(--color-danger);
  }

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
