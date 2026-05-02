<script>
  // Live link telemetry side panel. Reads session.state.stats which
  // updates at 1Hz via OutLinkStats events from the backend session
  // ticker (see pkg/ax25conn/session.go statsTick). Holds a 60-sample
  // ring buffer for the RTT sparkline + frame-arrival timeline.
  //
  // Inline SVG only (no D3 dep): the panel must be cheap to mount
  // because operators flip it on and off.

  import { Drawer, Badge, Icon } from '@chrissnell/chonky-ui';

  let { session, open = $bindable(false) } = $props();

  const HISTORY_LEN = 60;
  let rttHistory = $state(new Array(HISTORY_LEN).fill(0));
  let rxRateHistory = $state(new Array(HISTORY_LEN).fill(0));
  let lastFramesRX = 0;
  let sampleCount = $state(0);

  // Track the most recently observed stats payload by reference so we
  // only push a new sample when the backend tick actually refreshes
  // (i.e. don't push every reactive read).
  let lastStatsRef = null;

  $effect(() => {
    const stats = session?.state?.stats;
    if (!stats) return;
    if (stats === lastStatsRef) return;
    lastStatsRef = stats;
    rttHistory = pushSample(rttHistory, stats.rtt_ms ?? 0);
    const framesRX = stats.frames_rx ?? 0;
    const delta = Math.max(0, framesRX - lastFramesRX);
    lastFramesRX = framesRX;
    rxRateHistory = pushSample(rxRateHistory, delta);
    sampleCount = Math.min(HISTORY_LEN, sampleCount + 1);
  });

  function pushSample(buf, v) {
    const next = buf.slice(1);
    next.push(v);
    return next;
  }

  let stats = $derived(session?.state?.stats ?? null);
  let n2 = $derived(session?.state?.n2 ?? 10);
  let rcCritical = $derived((stats?.rc ?? 0) > Math.floor(n2 / 2));
  let connected = $derived(session?.state?.stateName === 'CONNECTED');

  // Sparkline geometry: 0..1 normalized, mapped to 220x40.
  const SPARK_W = 220;
  const SPARK_H = 40;

  let rttPath = $derived(buildPath(rttHistory, SPARK_W, SPARK_H));
  let rttPeak = $derived(Math.max(1, ...rttHistory));

  function buildPath(samples, w, h) {
    if (!samples?.length) return '';
    const peak = Math.max(1, ...samples);
    const dx = w / (samples.length - 1);
    let d = '';
    for (let i = 0; i < samples.length; i++) {
      const x = (i * dx).toFixed(2);
      const y = (h - (samples[i] / peak) * h).toFixed(2);
      d += (i === 0 ? 'M' : 'L') + x + ' ' + y + ' ';
    }
    return d.trim();
  }

  // Frame timeline: render 60 thin bars whose height is the per-tick
  // RX-frame delta normalized to the local peak.
  const TIMELINE_W = 220;
  const TIMELINE_H = 28;
  let timelinePeak = $derived(Math.max(1, ...rxRateHistory));
  let timelineBars = $derived(
    rxRateHistory.map((v, i) => {
      const bw = TIMELINE_W / rxRateHistory.length;
      const h = (v / timelinePeak) * TIMELINE_H;
      return {
        x: (i * bw).toFixed(2),
        y: (TIMELINE_H - h).toFixed(2),
        w: Math.max(1, bw - 0.5).toFixed(2),
        h: h.toFixed(2),
      };
    })
  );

  function close() {
    open = false;
  }
</script>

<Drawer bind:open anchor="right" onClose={close}>
  <Drawer.Header>
    <h3 class="title">Link telemetry</h3>
    <Drawer.Close aria-label="Close telemetry panel">
      <Icon name="x" size="sm" />
    </Drawer.Close>
  </Drawer.Header>

  <Drawer.Body>
    <div class="telemetry-body">
    {#if !connected}
      <p class="muted">No live link. Telemetry resumes once the session connects.</p>
    {/if}

    <section aria-label="Round-trip time">
      <header class="section-header">
        <span class="section-label">RTT</span>
        <span class="section-value mono" aria-live="polite">
          {stats?.rtt_ms ?? 0} ms
          <span class="peak">peak {rttPeak} ms</span>
        </span>
      </header>
      <svg class="spark" viewBox="0 0 {SPARK_W} {SPARK_H}" role="img" aria-label="RTT sparkline, last 60 samples">
        <path d={rttPath} fill="none" stroke="currentColor" stroke-width="1.5" />
      </svg>
    </section>

    <section aria-label="Frame arrival timeline">
      <header class="section-header">
        <span class="section-label">RX frames / sec</span>
        <span class="section-value mono">peak {timelinePeak}</span>
      </header>
      <svg class="timeline" viewBox="0 0 {TIMELINE_W} {TIMELINE_H}" role="img" aria-label="Per-second received-frame count, last 60 samples">
        {#each timelineBars as bar, i (i)}
          <rect x={bar.x} y={bar.y} width={bar.w} height={bar.h} fill="currentColor" />
        {/each}
      </svg>
    </section>

    <section class="grid">
      <div class="cell">
        <span class="section-label">Retry counter</span>
        <span class="mono cell-value" class:critical={rcCritical}>
          {stats?.rc ?? 0} / {n2}
        </span>
      </div>
      <div class="cell">
        <span class="section-label">V(S) / V(R) / V(A)</span>
        <span class="mono cell-value">
          <Badge variant="info">{stats?.vs ?? 0}</Badge>
          <Badge variant="info">{stats?.vr ?? 0}</Badge>
          <Badge variant="info">{stats?.va ?? 0}</Badge>
        </span>
      </div>
      <div class="cell">
        <span class="section-label">Frames TX / RX</span>
        <span class="mono cell-value">{stats?.frames_tx ?? 0} / {stats?.frames_rx ?? 0}</span>
      </div>
      <div class="cell">
        <span class="section-label">Bytes TX / RX</span>
        <span class="mono cell-value">{stats?.bytes_tx ?? 0} / {stats?.bytes_rx ?? 0}</span>
      </div>
      <div class="cell wide">
        <span class="section-label">Busy state</span>
        <span class="mono cell-value">
          {#if stats?.peer_busy}<Badge variant="warning">Peer busy</Badge>{/if}
          {#if stats?.own_busy}<Badge variant="warning">Own busy</Badge>{/if}
          {#if !stats?.peer_busy && !stats?.own_busy}<span class="muted">idle</span>{/if}
        </span>
      </div>
    </section>
    </div>
  </Drawer.Body>
</Drawer>

<style>
  .telemetry-body {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 12px;
    color: var(--color-text, #222);
  }
  .title {
    font-size: 15px;
    font-weight: 600;
    margin: 0;
  }
  .section-header {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    margin-bottom: 4px;
    font-size: 12px;
  }
  .section-label {
    color: var(--color-text-muted, #666);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .section-value { font-weight: 600; }
  .peak {
    margin-left: 6px;
    color: var(--color-text-muted, #888);
    font-weight: 400;
  }
  .spark, .timeline {
    width: 100%;
    height: auto;
    color: var(--color-accent, #0a84ff);
  }
  .timeline { color: var(--color-success, #0a8054); }
  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }
  .cell { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
  .cell.wide { grid-column: 1 / -1; }
  .cell-value { font-size: 14px; display: inline-flex; gap: 4px; align-items: center; }
  .cell-value.critical { color: var(--color-danger, #c41010); font-weight: 700; }
  .mono { font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace); }
  .muted { color: var(--color-text-muted, #888); }
</style>
