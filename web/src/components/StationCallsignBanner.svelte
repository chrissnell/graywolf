<script>
  import { Icon } from '@chrissnell/chonky-ui';

  // Shared "station callsign not set" banner used by the iGate and
  // Digipeater settings pages. Purely presentational — the caller
  // decides when to render it (D7 predicate: StationConfig is empty or
  // N0CALL). The CTA links to the hash-routed Station Callsign page.
  //
  // Props:
  //   feature  Name of the feature (used verbatim in the sentence).
  //            Examples: "iGate", "Digipeater".
  //   id       Optional id for the banner element so the caller's
  //            Enable toggle can reference it via aria-describedby and
  //            screen readers announce the reason the control is
  //            inert.
  let { feature, id = undefined } = $props();
</script>

<div {id} class="station-banner" role="status">
  <div class="station-banner-icon" aria-hidden="true">
    <Icon name="alert-circle" size="md" />
  </div>
  <div class="station-banner-body">
    <strong>Station callsign not set.</strong>
    {feature} cannot be enabled without a station callsign.
  </div>
  <a class="station-banner-cta" href="#/callsign">
    Set station callsign →
  </a>
</div>

<style>
  /* Amber callout matching the pattern used by the RF-transmit danger
     panel in Igate.svelte and the no-rules warning in Digipeater.svelte
     so the settings surfaces share one visual vocabulary. role="status"
     (not "alert") because this is a persistent "you need to configure
     this before proceeding" state, not a real-time emergency. */
  .station-banner {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 0 0 16px;
    padding: 12px 14px;
    border: 1px solid var(--color-warning, #d4a72c);
    border-left-width: 4px;
    border-radius: 4px;
    background: var(--color-warning-bg, rgba(212, 167, 44, 0.12));
    color: var(--text-primary, inherit);
    line-height: 1.45;
    max-width: 720px;
  }
  .station-banner-icon {
    color: var(--color-warning, #d4a72c);
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    line-height: 1;
  }
  .station-banner-body {
    flex: 1 1 auto;
    font-size: 13px;
  }
  .station-banner-body strong {
    margin-right: 6px;
  }
  /* Styled as a button but implemented as an <a> so hash-routing works
     without JS and it remains keyboard-focusable out of the box. */
  .station-banner-cta {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    padding: 6px 12px;
    border-radius: 4px;
    background: var(--color-warning, #d4a72c);
    color: var(--color-warning-fg, #1a1a1a);
    font-size: 13px;
    font-weight: 600;
    text-decoration: none;
    white-space: nowrap;
    transition: filter 0.15s;
  }
  .station-banner-cta:hover,
  .station-banner-cta:focus-visible {
    filter: brightness(1.08);
    outline: none;
  }
  .station-banner-cta:focus-visible {
    box-shadow: 0 0 0 2px var(--color-warning, #d4a72c),
                0 0 0 4px var(--color-warning-bg, rgba(212, 167, 44, 0.4));
  }

  /* On narrow viewports stack the CTA under the message so it doesn't
     squeeze the copy. */
  @media (max-width: 480px) {
    .station-banner {
      flex-wrap: wrap;
    }
    .station-banner-cta {
      width: 100%;
      justify-content: center;
    }
  }
</style>
