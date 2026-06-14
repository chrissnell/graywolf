<script>
  // Deep packet inspection modal for the APRS Logs tab. Given a packet log
  // entry, decodes its raw AX.25 frame (Entry.raw, base64) into a direwolf-
  // style hex/ASCII dump, a parsed frame-structure summary, and a list of
  // detected anomalies (malformed Mic-E, invalid addresses, unexpected
  // control/PID). Intended for testing radio firmware and packet formatting.

  import Modal from './Modal.svelte';
  import { decodeRaw, hexDump, analyzeFrame } from '../lib/packetInspect.js';
  import { parseDisplay, formatTime } from '../lib/packetColumns.js';

  let { open = $bindable(false), packet = null } = $props();

  const bytes = $derived(decodeRaw(packet?.raw));
  const rows = $derived(hexDump(bytes));
  const frame = $derived(bytes.length ? analyzeFrame(bytes) : null);
  const calls = $derived(packet ? parseDisplay(packet) : { src: '', dst: '' });

  function addrLabel(a) {
    if (!a) return '—';
    return a.ssid ? `${a.callsign}-${a.ssid}` : a.callsign;
  }

  // Render a byte as 0xNN, or 0x?? when absent (e.g. a frame that ended
  // before its PID). Avoids a literal "0xundefined" in the summary.
  const fmtByte = (b) => '0x' + (b == null ? '??' : b.toString(16).padStart(2, '0').toUpperCase());
</script>

<Modal bind:open title="Packet Inspector">
  {#if packet}
    <div class="inspector">
      <div class="summary">
        <span class="route">
          <span class="src">{calls.src || '—'}</span>
          <span class="arrow" aria-hidden="true">→</span>
          <span class="dst">{calls.dst || '—'}</span>
        </span>
        {#if packet.type}<span class="meta-pill">{packet.type}</span>{/if}
        {#if packet.direction}<span class="meta-pill">{packet.direction}</span>{/if}
        {#if packet.timestamp}<span class="meta-time">{formatTime(packet.timestamp)}</span>{/if}
      </div>

      {#if frame && frame.issues.length}
        <ul class="issues">
          {#each frame.issues as issue}
            <li class="issue issue-{issue.severity}">
              <span class="issue-tag">{issue.severity === 'error' ? 'ERROR' : 'WARN'}</span>
              <span>{issue.text}</span>
            </li>
          {/each}
        </ul>
      {/if}

      {#if bytes.length === 0}
        <div class="empty">No raw frame is available for this packet.</div>
      {:else}
        {#if frame?.ok}
          <dl class="frame">
            <dt>Destination</dt><dd>{addrLabel(frame.dest)}</dd>
            <dt>Source</dt><dd>{addrLabel(frame.source)}</dd>
            <dt>Path</dt>
            <dd>{frame.digis.length ? frame.digis.map((d) => addrLabel(d) + (d.hbit ? '*' : '')).join(', ') : 'direct'}</dd>
            <dt>Control / PID</dt>
            <dd>{fmtByte(frame.control)} / {fmtByte(frame.pid)}{frame.isMicE ? ' · Mic-E' : ''}</dd>
            <dt>Length</dt><dd>{bytes.length} bytes</dd>
          </dl>
        {/if}

        <div class="dump" role="img" aria-label="Hex and ASCII dump of the raw packet">
          {#each rows as row}
            <div class="dump-row">
              <span class="off">{row.offset}</span>
              <span class="hex">{row.hex}</span>
              <span class="asc">{row.ascii}</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</Modal>

<style>
  .inspector {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: min(620px, 80vw);
  }

  .summary {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .route {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-weight: 600;
  }
  .src { color: var(--color-warning); }
  .dst { color: var(--color-info); }
  .arrow { color: var(--color-text-dim); }
  .meta-pill {
    font-size: 10px;
    font-weight: 600;
    padding: 2px 6px;
    border-radius: 3px;
    background: var(--color-surface-raised);
    color: var(--color-text-muted);
  }
  .meta-time {
    font-size: var(--text-xs);
    color: var(--color-text-dim);
    font-variant-numeric: tabular-nums;
    margin-left: auto;
  }

  .issues {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .issue {
    display: flex;
    gap: 8px;
    align-items: baseline;
    font-size: var(--text-xs);
    padding: 6px 8px;
    border-radius: 4px;
    line-height: 1.4;
  }
  .issue-error { background: color-mix(in srgb, var(--color-error) 14%, transparent); }
  .issue-warn { background: color-mix(in srgb, var(--color-warning) 14%, transparent); }
  .issue-tag {
    flex-shrink: 0;
    font-weight: 700;
    font-size: 9px;
    letter-spacing: 0.04em;
  }
  .issue-error .issue-tag { color: var(--color-error); }
  .issue-warn .issue-tag { color: var(--color-warning); }

  .frame {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 2px 16px;
    margin: 0;
    font-size: var(--text-xs);
  }
  .frame dt { color: var(--color-text-dim); }
  .frame dd {
    margin: 0;
    color: var(--color-text);
    font-variant-numeric: tabular-nums;
  }

  .dump {
    font-family: var(--font-mono, monospace);
    font-size: 0.72rem;
    line-height: 1.5;
    background: var(--color-surface-raised);
    border-radius: 6px;
    padding: 10px 12px;
    overflow-x: auto;
    white-space: pre;
  }
  .dump-row { display: flex; gap: 14px; }
  .off { color: var(--color-text-dim); }
  .hex { color: var(--color-text); }
  .asc { color: var(--color-text-muted); }
</style>
