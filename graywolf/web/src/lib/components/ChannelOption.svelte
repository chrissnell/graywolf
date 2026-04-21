<script>
  // ChannelOption — single-row renderer for the ChannelListbox and
  // the Channels page card. Shows the three-state backing indicator
  // (● live / ○ down / — unbound) plus a two-line layout: bold
  // channel name on top, muted "KISS-TNC: loramod" backing detail
  // below. D8 in the KISS tcp-client plan.
  //
  // The glyph, text, aria-label, and tooltip all route through
  // ../channelBacking.js so one change updates every picker.
  //
  // Props:
  //   - channel: a row from the shared channelsStore (must include
  //     the backing object).
  //   - variant: 'option' renders the two-line layout used inside
  //     the listbox; 'summary' renders a single-line summary used on
  //     the Channels page card and the picker trigger's selected-
  //     value display. 'trigger-compact' is a tighter single-line
  //     form used inside a listbox trigger button.
  //   - unavailable: when true, the option renders the provided
  //     unavailableReason in place of the summary/health detail line
  //     so the operator sees *why* they can't pick this row (e.g.
  //     "no output device configured"). Aria is handled by the
  //     parent <li aria-disabled="true">; this component only owns
  //     the visible text.
  //   - unavailableReason: the reason string from the capability
  //     predicate. Ignored when `unavailable` is false.
  //
  // Pulse: when the channel's backing.health changes while the
  // component is mounted, a short (~800ms) CSS pulse is applied so
  // the operator notices live flaps without watching logs (D18).

  import {
    healthGlyph,
    healthText,
    summaryLabel,
    ariaLabel,
    tooltipText,
    HEALTH_LIVE,
    HEALTH_DOWN,
  } from '../channelBacking.js';

  let {
    channel,
    variant = 'option',
    unavailable = false,
    unavailableReason = '',
  } = $props();

  // Track the previous health value so a transition triggers a
  // one-shot pulse via a CSS class toggled off by a timer. prevHealth
  // is not $state — reading .backing.health at effect time is enough
  // and keeps us off the state-referenced-locally path.
  let pulse = $state(false);
  let prevHealth = null;

  $effect(() => {
    const h = channel?.backing?.health;
    if (prevHealth !== null && h !== prevHealth) {
      pulse = true;
      const t = setTimeout(() => {
        pulse = false;
      }, 800);
      prevHealth = h;
      return () => clearTimeout(t);
    }
    prevHealth = h ?? null;
  });

  let glyph = $derived(healthGlyph(channel?.backing?.health));
  let text = $derived(healthText(channel?.backing?.health));
  let sum = $derived(summaryLabel(channel?.backing));
  let aria = $derived(ariaLabel(channel));
  let tip = $derived(tooltipText(channel?.backing));
  let healthClass = $derived.by(() => {
    const h = channel?.backing?.health;
    if (h === HEALTH_LIVE) return 'live';
    if (h === HEALTH_DOWN) return 'down';
    return 'unbound';
  });

  // When `unavailable` is true, the detail line shows the reason
  // instead of the summary · health pair. Empty-string fallback so
  // the slot doesn't render "undefined" if a caller forgot the
  // reason.
  let detailLine = $derived(
    unavailable ? (unavailableReason || 'Unavailable') : `${sum} · ${text}`,
  );
</script>

{#if variant === 'summary'}
  <span class="row summary {healthClass}" class:pulse class:unavailable aria-label={aria} title={tip}>
    <span class="glyph {healthClass}" aria-hidden="true">{glyph}</span>
    <span class="summary-line">
      <span class="name">Channel {channel?.id} — {channel?.name}</span>
      <span class="detail">{detailLine}</span>
    </span>
  </span>
{:else if variant === 'trigger-compact'}
  <span class="row compact {healthClass}" class:pulse class:unavailable aria-label={aria} title={tip}>
    <span class="glyph {healthClass}" aria-hidden="true">{glyph}</span>
    <span class="name">{channel?.name ?? `Channel ${channel?.id ?? '?'}`}</span>
    <span class="detail-compact">({detailLine})</span>
  </span>
{:else}
  <span class="row option {healthClass}" class:pulse class:unavailable aria-label={aria} title={tip}>
    <span class="glyph {healthClass}" aria-hidden="true">{glyph}</span>
    <span class="two-line">
      <span class="name">Channel {channel?.id} — {channel?.name}</span>
      <span class="detail">{detailLine}</span>
    </span>
  </span>
{/if}

<style>
  .row {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .two-line, .summary-line {
    display: inline-flex;
    flex-direction: column;
    min-width: 0;
    line-height: 1.25;
  }
  .name {
    font-weight: 600;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .detail {
    font-size: 12px;
    color: var(--text-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .detail-compact {
    font-size: 12px;
    color: var(--text-secondary);
    white-space: nowrap;
  }
  .glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 14px;
    height: 14px;
    line-height: 1;
    font-size: 14px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    transition: transform 0.2s ease-out;
  }
  .glyph.live {
    color: var(--color-success, #2ea043);
  }
  .glyph.down {
    color: var(--color-warning, #d4a72c);
  }
  .glyph.unbound {
    color: var(--text-muted, #888);
  }
  /* Pulse when backing.health flips while the user is watching. Uses
     an opacity + scale keyframe on the glyph so it's obvious without
     being noisy. */
  .row.pulse .glyph {
    animation: glyph-pulse 800ms ease-out;
  }
  @keyframes glyph-pulse {
    0% {
      transform: scale(1);
      filter: drop-shadow(0 0 0 currentColor);
    }
    35% {
      transform: scale(1.6);
      filter: drop-shadow(0 0 6px currentColor);
    }
    100% {
      transform: scale(1);
      filter: drop-shadow(0 0 0 currentColor);
    }
  }
  .row.compact {
    gap: 6px;
  }
  /* When the option fails the capability predicate, tint the detail
     line with the danger colour so the reason reads as "this is why
     you can't pick me" rather than just a normal sub-label. The
     parent <li aria-disabled="true"> already dims the row via the
     chonky .listbox-item[aria-disabled] token (opacity 0.4) — we
     just colour-shift the reason text inside that dimmed stack.
     The detail-compact variant inside a trigger button also gets
     this treatment so a previously-selected-but-now-broken channel
     is readable in the closed trigger. */
  .row.unavailable .detail,
  .row.unavailable .detail-compact {
    color: var(--color-danger, #f85149);
  }
</style>
