<script>
  import { Badge } from '@chrissnell/chonky-ui';

  let { session } = $props();

  let stateName = $derived(session.state.stateName ?? 'DISCONNECTED');
  let stats = $derived(session.state.stats);
  let suspended = $derived(session.state.suspended);
  let errMsg = $derived(session.state.errorMessage);
  let viaText = $derived((session.state.via ?? []).join(' -> '));

  let badgeVariant = $derived(
    stateName === 'CONNECTED' ? 'success'
      : stateName === 'DISCONNECTED' ? 'danger'
      : 'warning'
  );
</script>

<div class="statusbar" role="status" aria-live="polite">
  <Badge variant={badgeVariant}>{stateName}</Badge>
  <span class="peer">{session.state.peer || '(no peer)'}</span>
  {#if viaText}<span class="via">via {viaText}</span>{/if}
  {#if stats}
    <span class="metric" title="round-trip time"><span class="metric-key">RTT</span> {stats.rtt_ms ?? 0} ms</span>
    <span class="metric" title="retry counter"><span class="metric-key">RC</span> {stats.rc ?? 0}</span>
    <span class="metric" title="frames TX/RX"><span class="metric-key">frames</span> {stats.frames_tx ?? 0}/{stats.frames_rx ?? 0}</span>
  {:else}
    <span class="metric muted">RTT --</span>
  {/if}
  {#if suspended}
    <span class="suspended">Suspended (tab in background)</span>
  {/if}
  {#if errMsg}
    <span class="err" role="alert">{errMsg}</span>
  {/if}
</div>

<style>
  .statusbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
    padding: 6px 10px;
    background: var(--color-surface, #f8f8f8);
    border-top: 1px solid var(--color-border, #ddd);
    font-size: 13px;
  }
  .peer { font-weight: 600; }
  .via { color: var(--color-text-muted, #666); }
  .metric { display: inline-flex; gap: 4px; align-items: baseline; }
  .metric-key { color: var(--color-text-muted, #666); font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; }
  .muted { color: var(--color-text-muted, #999); }
  .suspended { color: var(--color-warning, #d66e00); font-style: italic; }
  .err { color: var(--color-danger, #c41010); }
</style>
