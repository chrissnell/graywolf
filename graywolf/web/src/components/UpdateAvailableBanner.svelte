<script>
  import { Icon } from '@chrissnell/chonky-ui';
  import { updates } from '../lib/updatesStore.svelte.js';

  // Move focus to the "What's new" heading BEFORE dismissal flips the
  // banner out of the DOM. Order matters — otherwise focus lands on
  // <body>. About.svelte marks the heading tabindex="-1" so it's
  // programmatically focusable without being in the tab sequence.
  function focusNextSection() {
    const h = document.getElementById('whats-new-heading');
    h?.focus({ preventScroll: true });
  }

  function onDismiss() {
    focusNextSection();
    updates.dismiss();
  }

  function onKeydown(e) {
    if (e.key === 'Escape') {
      e.preventDefault();
      onDismiss();
    }
  }

  // Relative-time formatter for the stale-check tooltip + SR <time>
  // element. Coarse buckets (minutes/hours/days) are fine — operators
  // use this to spot "my machine's been offline for a week," not to
  // measure seconds.
  function relativeTime(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    const ms = Date.now() - d.getTime();
    const mins = Math.round(ms / 60_000);
    if (mins < 2) return 'just now';
    if (mins < 90) return mins + ' minutes ago';
    const hrs = Math.round(mins / 60);
    if (hrs < 36) return hrs + ' hours ago';
    const days = Math.round(hrs / 24);
    return days + ' days ago';
  }
</script>

{#if updates.hasUnseenUpdate}
  <!--
    svelte-ignore a11y_no_noninteractive_element_interactions
    Intentional: the banner root captures Escape so keyboard users inside
    the banner (on the link or the dismiss button) can dismiss without
    reaching for the mouse. The dismiss button is the canonical
    interactive affordance; the aside-level onkeydown is additive.
  -->
  <aside
    class="update-banner"
    aria-labelledby="update-banner-title"
    title={updates.checkedAt ? 'Last checked ' + relativeTime(updates.checkedAt) : undefined}
    tabindex="-1"
    onkeydown={onKeydown}
  >
    <h3 id="update-banner-title" class="sr-only">Update available</h3>
    <span class="banner-icon" aria-hidden="true">
      <Icon name="arrow-up" size="sm" />
    </span>
    <div class="banner-body">
      <p class="banner-text">
        <strong>Graywolf v{updates.latest}</strong> is now available.
        <a
          href={updates.url}
          target="_blank"
          rel="noopener"
          class="banner-link"
        >
          View release →
        </a>
      </p>
      {#if updates.checkedAt}
        <time datetime={updates.checkedAt} class="sr-only">
          Last checked {relativeTime(updates.checkedAt)}
        </time>
      {/if}
    </div>
    <button
      type="button"
      class="banner-dismiss"
      aria-label={"Dismiss update notice for v" + updates.latest}
      onclick={onDismiss}
    >
      <Icon name="x" size="sm" />
    </button>
  </aside>
{/if}

<style>
  /* Matches StationCallsignBanner's structural layout (full 1px border +
     4px left accent stripe) but with the accent tokens instead of the
     warning tokens — informational, not a warning. */
  .update-banner {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    margin: 0 0 16px;
    padding: 12px 14px;
    border: 1px solid var(--accent);
    border-left-width: 4px;
    border-radius: var(--radius, 4px);
    background: var(--accent-bg);
    color: var(--text-primary, inherit);
    line-height: 1.45;
    max-width: 720px;
    outline: none;
  }
  .update-banner:focus-visible {
    box-shadow: 0 0 0 2px var(--accent);
  }
  .banner-icon {
    flex: 0 0 auto;
    color: var(--accent);
    display: inline-flex;
    align-items: center;
    margin-top: 2px;
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
  .banner-link {
    margin-left: 8px;
    color: var(--accent);
    text-decoration: none;
    font-weight: 600;
    white-space: nowrap;
  }
  .banner-link:hover,
  .banner-link:focus-visible {
    text-decoration: underline;
  }
  .banner-dismiss {
    flex: 0 0 auto;
    appearance: none;
    background: transparent;
    border: 0;
    padding: 4px;
    margin: -4px -6px -4px 0;
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

  /* Visually-hidden utility, component-scoped. Matches the declaration
     in NewsPopup.svelte — no global .sr-only exists in app.css. */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }

  /* Forced-colors (Windows High Contrast, macOS Increase Contrast).
     Drop custom colors in favor of system tokens so the banner remains
     legible under forced theming. */
  @media (forced-colors: active) {
    .update-banner {
      border: 1px solid CanvasText;
      background: Canvas;
      color: CanvasText;
    }
    .banner-link {
      color: LinkText;
    }
    .banner-icon,
    .banner-dismiss {
      color: CanvasText;
    }
  }

  /* No transitions in the default spec, but this blocks any future
     additions from violating the reduced-motion contract. */
  @media (prefers-reduced-motion: reduce) {
    .banner-dismiss {
      transition: none !important;
    }
  }

  /* On narrow viewports, stack the dismiss button under the message so
     the link has room to breathe. Mirrors StationCallsignBanner. */
  @media (max-width: 480px) {
    .update-banner {
      flex-wrap: wrap;
    }
    .banner-body {
      flex: 1 1 100%;
    }
  }
</style>
