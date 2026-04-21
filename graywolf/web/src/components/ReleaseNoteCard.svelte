<script>
  import { Icon } from '@chrissnell/chonky-ui';
  import Sparkles from 'lucide-svelte/icons/sparkles';
  import ArrowRightCircle from 'lucide-svelte/icons/arrow-right-circle';

  // Renders a single release-note entry. Two visual variants driven by
  // note.style: "info" (neutral announcement) and "cta" (action
  // required). Both use --accent — CTA is deliberately NOT the amber
  // warning palette (reserved for real error states like the station
  // callsign banner and RF-transmit panel).
  //
  // Props:
  //   note     { version, date, style, title, body } — body is
  //            server-sanitized HTML, bound via {@html}.
  //   compact  tightens padding and, for entries >90 days old,
  //            collapses the body behind a "Show details" toggle.
  //            Used by the About-page list.
  let { note, compact = false } = $props();

  const MS_PER_DAY = 86_400_000;
  const COLLAPSE_AFTER_DAYS = 90;

  function daysOld(iso) {
    if (!iso) return 0;
    const then = Date.parse(iso);
    if (Number.isNaN(then)) return 0;
    return Math.floor((Date.now() - then) / MS_PER_DAY);
  }

  const isCta = $derived(note?.style === 'cta');
  const collapsible = $derived(compact && daysOld(note?.date) > COLLAPSE_AFTER_DAYS);
  let expanded = $state(false);

  // If the body contains an internal hash link, surface the first one
  // as an accent-filled CTA button at the bottom of the card. Only for
  // cta-style notes; info cards rely on the inline link alone.
  // Link text may contain inline emphasis (<strong>/<em>) — [\s\S]*?
  // accepts that, and we strip the tags for the button label so the
  // button itself stays plain text.
  const ctaLink = $derived.by(() => {
    if (!isCta || !note?.body) return null;
    const match = note.body.match(/<a href="(#\/[^"]+)">([\s\S]*?)<\/a>/);
    if (!match) return null;
    const text = match[2].replace(/<\/?(strong|em)>/g, '').trim();
    return { href: match[1], text };
  });
</script>

<article
  class="release-card"
  class:compact
  class:cta={isCta}
  class:info={!isCta}
>
  <div class="card-header">
    <div class="icon" aria-hidden="true">
      {#if isCta}
        <Icon icon={ArrowRightCircle} size="md" />
      {:else}
        <Icon icon={Sparkles} size="md" />
      {/if}
    </div>
    <h3 class="card-title">{note?.title || ''}</h3>
    <span class="version-pill">v{note?.version || ''} · {note?.date || ''}</span>
  </div>

  {#if collapsible && !expanded}
    <button
      type="button"
      class="show-details"
      onclick={() => (expanded = true)}
    >
      Show details
    </button>
  {:else}
    <div class="card-body">{@html note?.body || ''}</div>
    {#if ctaLink}
      <a class="card-cta" href={ctaLink.href}>{ctaLink.text} →</a>
    {/if}
  {/if}
</article>

<style>
  .release-card {
    position: relative;
    padding: 16px 18px;
    border-radius: var(--radius);
    border: 1px solid var(--border-color);
    border-left-width: 3px;
    background: var(--bg-secondary);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .release-card.compact {
    padding: 12px;
  }
  .release-card.info {
    border-left-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 6%, var(--bg-secondary));
  }
  .release-card.cta {
    border-left-width: 4px;
    border-left-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 10%, var(--bg-secondary));
  }

  .card-header {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: 10px;
  }
  .icon {
    color: var(--accent);
    display: flex;
    align-items: center;
    line-height: 1;
  }
  .card-title {
    margin: 0;
    font-size: 15px;
    font-weight: 600;
    line-height: 1.3;
    color: var(--text-primary);
  }
  .version-pill {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-muted);
    background: transparent;
    white-space: nowrap;
  }

  .card-body {
    font-size: 14px;
    line-height: 1.55;
    color: var(--text-secondary);
  }
  .card-body :global(strong) {
    color: var(--text-primary);
    font-weight: 600;
  }
  .card-body :global(a) {
    color: var(--accent);
    text-decoration: none;
  }
  .card-body :global(a:hover),
  .card-body :global(a:focus-visible) {
    text-decoration: underline;
    outline: none;
  }
  .card-body :global(p) {
    margin: 0 0 8px;
  }
  .card-body :global(p:last-child) {
    margin-bottom: 0;
  }

  .card-cta {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    margin-top: 4px;
    padding: 6px 12px;
    border-radius: 4px;
    background: var(--accent);
    color: var(--color-bg, #0d1117);
    font-size: 13px;
    font-weight: 600;
    text-decoration: none;
    white-space: nowrap;
    transition: filter 0.15s;
  }
  .card-cta:hover,
  .card-cta:focus-visible {
    filter: brightness(1.08);
    outline: none;
  }
  .card-cta:focus-visible {
    box-shadow: 0 0 0 2px var(--accent),
                0 0 0 4px color-mix(in srgb, var(--accent) 40%, transparent);
  }

  .show-details {
    align-self: flex-start;
    background: transparent;
    border: none;
    padding: 0;
    color: var(--accent);
    font-size: 13px;
    cursor: pointer;
    text-decoration: none;
  }
  .show-details:hover,
  .show-details:focus-visible {
    text-decoration: underline;
    outline: none;
  }

  @media (prefers-reduced-motion: reduce) {
    .release-card,
    .release-card *,
    .release-card *::before,
    .release-card *::after {
      animation-duration: 0s !important;
      transition-duration: 0s !important;
    }
  }
</style>
