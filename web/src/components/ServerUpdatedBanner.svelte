<script>
  import { Icon } from '@chrissnell/chonky-ui';
  import { serverVersion } from '../lib/stores/server-version.svelte.js';

  // The server build changed underneath this tab. We prompt rather than
  // auto-reload so the operator doesn't lose in-flight work (a half-typed
  // message, an open AX.25 terminal session, map pan/zoom). Dismiss is
  // session-local: the watcher's latch stays set, but the operator has
  // acknowledged it and we stop nagging until the next full load.
  let dismissed = $state(false);

  function reload() {
    window.location.reload();
  }

  function onKeydown(e) {
    if (e.key === 'Escape') {
      e.preventDefault();
      dismissed = true;
    }
  }
</script>

{#if serverVersion.updateAvailable && !dismissed}
  <!--
    svelte-ignore a11y_no_noninteractive_element_interactions
    The aside captures Escape so keyboard users can dismiss without reaching
    for the mouse; the explicit buttons remain the canonical affordances.
  -->
  <aside
    class="server-updated-banner"
    role="alert"
    aria-labelledby="server-updated-title"
    tabindex="-1"
    onkeydown={onKeydown}
  >
    <span class="banner-icon" aria-hidden="true">
      <Icon name="refresh-cw" size="sm" />
    </span>
    <div class="banner-body">
      <p class="banner-text" id="server-updated-title">
        <strong>Graywolf was updated on the server.</strong>
        Reload to get the latest version.
      </p>
    </div>
    <button type="button" class="banner-reload" onclick={reload}>
      Reload
    </button>
    <button
      type="button"
      class="banner-dismiss"
      aria-label="Dismiss server update notice"
      onclick={() => (dismissed = true)}
    >
      <Icon name="x" size="lg" />
    </button>
  </aside>
{/if}

<style>
  /* App-wide sticky bar: this should follow the operator across pages and
     stay visible while scrolling, unlike the in-page UpdateAvailableBanner.
     Accent tokens (informational), matching the update-available banner. */
  .server-updated-banner {
    position: sticky;
    top: 0;
    z-index: 50;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 16px;
    border-bottom: 1px solid var(--accent);
    background: var(--accent-bg);
    color: var(--text-primary, inherit);
    line-height: 1.45;
    outline: none;
  }
  .server-updated-banner:focus-visible {
    box-shadow: inset 0 0 0 2px var(--accent);
  }
  .banner-icon {
    flex: 0 0 auto;
    color: var(--accent);
    display: inline-flex;
    align-items: center;
    line-height: 1;
  }
  .banner-body {
    flex: 1 1 auto;
    min-width: 0;
  }
  .banner-text {
    margin: 0;
    font-size: 13px;
    line-height: 1.45;
  }
  .banner-text strong {
    margin-right: 4px;
  }
  .banner-reload {
    flex: 0 0 auto;
    appearance: none;
    cursor: pointer;
    font: inherit;
    font-size: 13px;
    font-weight: 600;
    padding: 6px 14px;
    border-radius: var(--radius, 4px);
    border: 1px solid var(--accent);
    background: var(--accent);
    color: var(--accent-fg, #fff);
  }
  .banner-reload:hover,
  .banner-reload:focus-visible {
    filter: brightness(1.08);
    outline: none;
  }
  .banner-dismiss {
    flex: 0 0 auto;
    appearance: none;
    background: transparent;
    border: 0;
    padding: 0;
    width: 32px;
    height: 32px;
    color: var(--text-secondary);
    cursor: pointer;
    border-radius: var(--radius, 4px);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font: inherit;
  }
  .banner-dismiss:hover,
  .banner-dismiss:focus-visible {
    color: var(--text-primary);
    background: color-mix(in srgb, var(--text-primary) 8%, transparent);
    outline: none;
  }

  /* Forced-colors (Windows High Contrast / macOS Increase Contrast). */
  @media (forced-colors: active) {
    .server-updated-banner {
      border-bottom: 1px solid CanvasText;
      background: Canvas;
      color: CanvasText;
    }
    .banner-reload {
      border: 1px solid ButtonText;
      background: ButtonFace;
      color: ButtonText;
    }
    .banner-icon,
    .banner-dismiss {
      color: CanvasText;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .banner-reload {
      transition: none !important;
    }
  }
</style>
