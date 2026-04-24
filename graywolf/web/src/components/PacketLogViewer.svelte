<script>
  // Thin wrapper around Chonky's <LogViewer> that owns the per-cell snippets
  // and column config for APRS packets. Both Dashboard and Logs render this
  // component instead of duplicating the cell markup.
  //
  // Column ordering matters: Chonky 0.2.1 splits primary/secondary in card
  // mode by index (`columns.slice(0, 3)` = primary). Direction is encoded as
  // entry.level (so it colors the whole row/card via .log-ok/.log-warn/.log-dim)
  // rather than as a column. Origin and Device are intentionally dropped from
  // the columns: keeping them required carrying a `desktopOnly` filter we
  // don't have a clean place for until Chonky 0.2.2 adds LogColumn.priority.
  // Revisit when 0.2.2 ships.

  import { LogViewer } from '@chrissnell/chonky-ui';
  import { formatDistance } from '../lib/settings/units.js';
  import {
    parseDisplay,
    originTag,
    deviceLabel,
    formatTime,
    packetToEntry,
  } from '../lib/packetColumns.js';

  let {
    packets = [],
    height = '600px',
    live = true,
    showHeader = true,
    mobileBreakpoint = '768px',
  } = $props();

  // Project raw packets into LogEntry shape (adds .level for direction color).
  const entries = $derived(packets.map(packetToEntry));

  // Column definitions. ORDER IS LOAD-BEARING — first 3 are primary on mobile.
  // No `priority` field in Chonky 0.2.1; ordering is the only knob.
  const columns = [
    { key: 'timestamp', label: 'Time',    width: '130px', class: 'pkt-c-time',           render: timeCell    },
    { key: 'type',      label: 'Type',    width: '180px', class: 'pkt-c-type',           render: typeCell    },
    { key: 'srcdst',    label: 'Src→Dst', width: '1fr',   class: 'pkt-c-srcdst',         render: srcDstCell  },
    { key: 'channel',   label: 'Ch',      width: '50px',  class: 'pkt-c-channel', align: 'center', render: channelCell },
    { key: 'distance',  label: 'Distance',width: '120px', class: 'pkt-c-distance', align: 'right', render: distanceCell },
  ];
</script>

{#snippet timeCell(_value, entry)}
  <span class="pkt-time">{formatTime(entry.timestamp)}</span>
{/snippet}

{#snippet typeCell(_value, entry)}
  {@const origin = originTag(entry)}
  {#if entry.type || origin}
    <div class="pkt-type-stack">
      {#if entry.type}
        <span class="pkt-badge pkt-b-type" data-type={entry.type}>{entry.type}</span>
      {/if}
      {#if origin}
        <span class="pkt-badge pkt-b-origin" data-origin={origin.cls}>{origin.label}</span>
      {/if}
    </div>
  {:else}
    <span class="pkt-dim">—</span>
  {/if}
{/snippet}

{#snippet srcDstCell(_value, entry)}
  {@const calls = parseDisplay(entry)}
  <span class="pkt-srcdst">
    <span class="pkt-src">{calls.src || '—'}</span>
    <span class="pkt-arrow" aria-hidden="true">→</span>
    <span class="pkt-dst">{calls.dst || '—'}</span>
  </span>
{/snippet}

{#snippet channelCell(_value, entry)}
  {entry.channel ?? '—'}
{/snippet}

{#snippet distanceCell(_value, entry)}
  {#if entry.distance_mi != null}
    <span class="pkt-distance">{formatDistance(entry.distance_mi)}</span>
  {:else}
    <span class="pkt-dim">—</span>
  {/if}
{/snippet}

{#snippet rawPacketFooter(entry)}
  <code class="pkt-raw">{entry.display || ''}</code>
{/snippet}

<LogViewer
  entries={entries}
  {columns}
  {live}
  {showHeader}
  {height}
  {mobileBreakpoint}
  footer={rawPacketFooter}
/>

<style>
  /* Cell-level styles. Chonky owns layout (.log-grid-cell / .log-card etc);
     we only theme the values & badges that used to live in the routes. */

  .pkt-time {
    font-variant-numeric: tabular-nums;
  }

  .pkt-srcdst {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .pkt-src {
    color: var(--color-warning);
    font-weight: 600;
  }
  .pkt-arrow {
    color: var(--color-text-dim);
    flex-shrink: 0;
  }
  .pkt-dst {
    color: var(--color-info);
  }

  .pkt-distance {
    font-size: var(--text-xs);
    color: var(--color-success);
  }

  .pkt-dim {
    color: var(--color-text-dim);
  }

  .pkt-badge {
    display: inline-block;
    font-weight: 700;
    font-size: 10px;
    padding: 2px 6px;
    border-radius: 3px;
    white-space: nowrap;
    text-align: center;
    line-height: 1.4;
  }
  .pkt-b-type {
    background: var(--color-surface-raised);
    color: var(--color-text-muted);
    font-weight: 500;
  }

  /* Lay the Type + Origin badges side-by-side in a single row so every
     row stays the same height. The cell is sized wide enough that neither
     badge needs to wrap in practice; nowrap prevents wrapping even when
     content edges out (rare). */
  .pkt-type-stack {
    display: inline-flex;
    flex-direction: row;
    align-items: center;
    gap: 4px;
    flex-wrap: nowrap;
  }

  .pkt-b-origin {
    font-size: 9px;
    padding: 1px 5px;
    background: var(--color-surface-raised);
    color: var(--color-text-muted);
    font-weight: 500;
  }

  /* Per-type badge colors are owned by the active theme -- see
     graywolf/web/themes/*.css. The base .pkt-b-type style above provides
     the neutral fallback; each theme layers on solid or muted-tint rules
     keyed by [data-type]. Light themes use solid-bg + white-text for
     legibility on white; dark themes use muted-tint + bright text. */

  /* Footer raw-packet line: wraps inside container, never forces overflow. */
  .pkt-raw {
    display: block;
    font-size: 0.65rem;
    color: var(--color-text-dim);
    line-height: 1.5;
    white-space: normal;
    overflow-wrap: anywhere;
    word-break: break-all;
  }

  /* Desktop density override: chonky's grid defaults are terminal-tight,
     which reads as cramped at desktop widths. Scoped to data-mode="grid" so
     card mode (mobile) keeps chonky's compact defaults. */
  :global(.log-viewer[data-mode='grid'] .log-grid) {
    font-size: 0.8rem;
    line-height: 1.4;
  }
  :global(.log-viewer[data-mode='grid'] .log-grid-cell) {
    padding: 0.4rem 0.75rem;
    line-height: 1.4;
  }
  :global(.log-viewer[data-mode='grid'] .log-grid-header) {
    padding: 0.5rem 0.75rem 0.35rem;
    font-size: 0.7rem;
  }
  :global(.log-viewer[data-mode='grid'] .log-grid-footer) {
    padding: 0 0.75rem 0.5rem;
  }
  :global(.log-viewer[data-mode='grid']) .pkt-raw {
    font-size: 0.75rem;
    line-height: 1.45;
  }
  :global(.log-viewer[data-mode='grid']) .pkt-badge {
    font-size: 11px;
    padding: 3px 8px;
  }
  :global(.log-viewer[data-mode='grid']) .pkt-distance {
    font-size: 0.8rem;
  }

  /* Direction-as-accent: paint a left border on each row/card driven by the
     level class Chonky adds. Color is informational only; the badge inside
     the Type cell already carries the textual direction. */
  :global(.log-viewer .log-grid-cell.log-ok)   { box-shadow: inset 3px 0 0 var(--color-success); }
  :global(.log-viewer .log-grid-cell.log-warn) { box-shadow: inset 3px 0 0 var(--color-warning); }
  :global(.log-viewer .log-grid-cell.log-dim)  { box-shadow: inset 3px 0 0 var(--color-text-dim); }
  :global(.log-viewer .log-grid-cell.log-ok:not(:first-child)),
  :global(.log-viewer .log-grid-cell.log-warn:not(:first-child)),
  :global(.log-viewer .log-grid-cell.log-dim:not(:first-child)) {
    box-shadow: none;
  }

  /* Cards in mobile mode: full-width left border accent. */
  :global(.log-viewer .log-card.log-ok)   { border-left: 3px solid var(--color-success); padding-left: calc(0.5rem - 3px); }
  :global(.log-viewer .log-card.log-warn) { border-left: 3px solid var(--color-warning); padding-left: calc(0.5rem - 3px); }
  :global(.log-viewer .log-card.log-dim)  { border-left: 3px solid var(--color-text-dim); padding-left: calc(0.5rem - 3px); }
</style>
