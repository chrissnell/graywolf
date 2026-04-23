<script>
  // Slide-out message-details drawer.
  //
  // Desktop (≥768 px): anchor right, 320 px.
  // Mobile (<768 px): anchor bottom, ~70 vh.
  //
  // The per-breakpoint choice is selected on open via
  // `window.matchMedia`. We don't re-anchor mid-open — the drawer
  // component re-mounts with the chosen anchor when `open` flips.

  import { onMount } from 'svelte';
  import { Button, Drawer, Icon } from '@chrissnell/chonky-ui';
  import { relativeLong, timeOfDay } from './time.js';

  /** @type {{
   *    open: boolean,
   *    msg?: any,
   *    onClose?: () => void,
   *  }}
   */
  let { open = $bindable(false), msg = null, onClose } = $props();

  let anchor = $state('right');
  function updateAnchor() {
    if (typeof window === 'undefined') return;
    anchor = window.matchMedia('(max-width: 767px)').matches ? 'bottom' : 'right';
  }
  onMount(() => {
    updateAnchor();
    const mq = window.matchMedia('(max-width: 767px)');
    const on = () => updateAnchor();
    mq.addEventListener?.('change', on);
    return () => mq.removeEventListener?.('change', on);
  });

  function close() {
    open = false;
    onClose?.();
  }

  async function copy(text) {
    if (!text) return;
    try {
      await navigator.clipboard?.writeText?.(text);
    } catch {
      // ignore — feature-flag clipboard unavailable
    }
  }

  // Best-effort raw TNC-2 line reconstruction from fields we have.
  // The backend stores a `via` string and `path`; a full-fidelity
  // raw reproduction would require round-tripping the router, which
  // is out of scope. We assemble the useful fields the user cares
  // about instead.
  const rawLine = $derived.by(() => {
    if (!msg) return '';
    const from = msg.direction === 'out' ? (msg.our_call || '') : (msg.from_call || '');
    const to = msg.direction === 'out' ? (msg.to_call || msg.peer_call || '') : (msg.our_call || '');
    const via = msg.path || msg.via || '';
    const addr = (msg.peer_call || msg.to_call || '').padEnd(9);
    const suffix = msg.msg_id ? `{${msg.msg_id}` : '';
    return `${from}>${to}${via ? ',' + via : ''}::${addr}:${msg.text || ''}${suffix}`;
  });

  const hops = $derived.by(() => {
    if (!msg?.path) return 0;
    return msg.path.split(',').filter(Boolean).length;
  });
</script>

<Drawer bind:open {anchor} onClose={close}>
  <Drawer.Header>
    <h3 class="title">Message details</h3>
    <Drawer.Close aria-label="Close details">
      <Icon name="x" size="sm" />
    </Drawer.Close>
  </Drawer.Header>
  <Drawer.Body class="meta-body">
    {#if msg}
      <dl class="meta">
        <dt>ID</dt><dd class="mono">#{msg.id}</dd>
        <dt>Direction</dt><dd>{msg.direction === 'out' ? 'Outgoing' : 'Incoming'}</dd>
        <dt>Thread</dt><dd class="mono">{msg.thread_kind}:{msg.thread_key}</dd>
        <dt>Peer</dt><dd class="mono">{msg.peer_call || '—'}</dd>
        {#if msg.msg_id}
          <dt>APRS msgid</dt><dd class="mono">{msg.msg_id}</dd>
        {/if}
        {#if msg.channel != null}
          <dt>Channel</dt><dd class="mono">{msg.channel}</dd>
        {/if}
        <dt>Source</dt><dd class="mono">{msg.source || '—'}</dd>
        <dt>Status</dt><dd class="mono">{msg.status || '—'}</dd>
        {#if msg.path}
          <dt>Path</dt><dd class="mono">{msg.path}</dd>
        {/if}
        <dt>Digi hops</dt><dd class="mono">{hops}</dd>
        {#if msg.attempts != null}
          <dt>Retry count</dt><dd class="mono">{msg.attempts}</dd>
        {/if}
        {#if msg.sent_at}
          <dt>Sent</dt><dd>{timeOfDay(msg.sent_at)} · {relativeLong(msg.sent_at)}</dd>
        {/if}
        {#if msg.received_at}
          <dt>Received</dt><dd>{timeOfDay(msg.received_at)} · {relativeLong(msg.received_at)}</dd>
        {/if}
        {#if msg.acked_at}
          <dt>Acked</dt><dd>{relativeLong(msg.acked_at)}</dd>
        {/if}
        {#if msg.failure_reason}
          <dt>Failure</dt><dd class="mono err">{msg.failure_reason}</dd>
        {/if}
        {#if msg.direction === 'out' && msg.extended}
          <dt>Length</dt>
          <dd title="Longer than 67 chars — some receivers may truncate.">
            Extended ({(msg.text || '').length} chars)
          </dd>
        {/if}
      </dl>
      <div class="raw-block">
        <div class="raw-head">
          <span>Raw TNC-2</span>
          <Button size="sm" variant="ghost" onclick={() => copy(rawLine)} aria-label="Copy raw line">
            <Icon name="copy" size="xs" />
            <span>Copy</span>
          </Button>
        </div>
        <pre class="raw-line">{rawLine}</pre>
      </div>
    {/if}
  </Drawer.Body>
</Drawer>

<style>
  .title {
    font-size: 14px;
    font-weight: 600;
    margin: 0;
    font-family: var(--font-mono);
  }
  :global(.meta-body) {
    padding: 12px 16px 24px;
  }
  .meta {
    display: grid;
    grid-template-columns: 100px 1fr;
    gap: 6px 12px;
    margin: 0 0 12px;
  }
  .meta dt {
    font-size: 11px;
    color: var(--color-text-dim);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .meta dd {
    margin: 0;
    font-size: 12px;
    color: var(--color-text);
    overflow-wrap: anywhere;
  }
  .mono { font-family: var(--font-mono); }
  .err { color: var(--color-danger); }

  .raw-block {
    background: var(--color-surface-raised);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    padding: 8px;
  }
  .raw-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 1px;
    text-transform: uppercase;
    color: var(--color-text-dim);
    margin-bottom: 6px;
  }
  .raw-line {
    margin: 0;
    padding: 0;
    font-family: var(--font-mono);
    font-size: 11px;
    line-height: 1.4;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    color: var(--color-text);
  }
</style>
