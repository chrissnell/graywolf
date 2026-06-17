<script>
  // Sticky cue that appears at the bottom of a scrollable parent when there
  // is hidden content below the viewport. Drop as the last child inside the
  // scroll container (e.g. Modal.Body). It walks up the DOM until it finds
  // an element with overflow scroll/auto and observes it. Clicking the pill
  // scrolls that container to the bottom.
  let { label = 'Scroll down for more' } = $props();

  let sentinel = $state(null);
  let scrollEl = $state(null);
  let visible = $state(false);

  function findScrollParent(el) {
    let cur = el?.parentElement;
    while (cur && cur !== document.body) {
      const oy = getComputedStyle(cur).overflowY;
      if (oy === 'auto' || oy === 'scroll') return cur;
      cur = cur.parentElement;
    }
    return null;
  }

  function scrollDown() {
    if (!scrollEl) return;
    scrollEl.scrollTo({ top: scrollEl.scrollHeight, behavior: 'smooth' });
  }

  $effect(() => {
    if (!sentinel) return;
    const el = findScrollParent(sentinel);
    if (!el) return;
    scrollEl = el;

    const update = () => {
      const overflow = el.scrollHeight > el.clientHeight + 1;
      const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - 4;
      visible = overflow && !atBottom;
    };

    el.addEventListener('scroll', update, { passive: true });
    const ro = new ResizeObserver(update);
    ro.observe(el);
    const mo = new MutationObserver(update);
    mo.observe(el, { childList: true, subtree: true, attributes: true });
    update();

    return () => {
      el.removeEventListener('scroll', update);
      ro.disconnect();
      mo.disconnect();
      scrollEl = null;
    };
  });
</script>

<!-- The outer band is sticky with NO negative bottom margin: a negative
     bottom margin extends the sticky constraint box below the viewport and
     clips the element, which is why it used to drift out of sight. Horizontal
     bleed keeps the fade spanning the full width of the padded container. -->
<div
  bind:this={sentinel}
  class="scroll-hint"
  class:visible
>
  <button
    type="button"
    class="scroll-hint__pill"
    onclick={scrollDown}
    tabindex={visible ? 0 : -1}
    aria-hidden={!visible}
  >
    <span class="scroll-hint__label">{label}</span>
    <span class="scroll-hint__arrow">&#8595;</span>
  </button>
</div>

<style>
  .scroll-hint {
    position: sticky;
    bottom: 0;
    left: 0;
    right: 0;
    margin: 0 calc(-1 * var(--scroll-hint-pad-x, 1.5rem));
    padding: 28px 1.5rem 12px;
    display: flex;
    justify-content: center;
    align-items: flex-end;
    background: linear-gradient(
      to bottom,
      transparent 0%,
      color-mix(in srgb, var(--color-surface, #fff) 85%, transparent) 45%,
      var(--color-surface, #fff) 100%
    );
    pointer-events: none;
    opacity: 0;
    transform: translateY(6px);
    transition: opacity 140ms ease, transform 140ms ease;
    z-index: 5;
  }
  .scroll-hint.visible {
    opacity: 1;
    transform: translateY(0);
  }
  .scroll-hint__pill {
    pointer-events: auto;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 9px 18px;
    border: none;
    border-radius: 999px;
    background: var(--color-primary, #2563eb);
    color: var(--color-primary-fg, #fff);
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    cursor: pointer;
    box-shadow: 0 6px 18px rgba(0, 0, 0, 0.35);
    transition: background 120ms ease, transform 120ms ease;
  }
  .scroll-hint__pill:hover {
    background: var(--color-primary-hover, #1d4ed8);
    transform: translateY(-1px);
  }
  .scroll-hint__pill:focus-visible {
    outline: 2px solid var(--color-primary-fg, #fff);
    outline-offset: 2px;
  }
  .scroll-hint__arrow {
    display: inline-block;
    font-size: 15px;
    line-height: 1;
    animation: scroll-hint-bounce 1.4s ease-in-out infinite;
  }
  @keyframes scroll-hint-bounce {
    0%, 100% { transform: translateY(0); }
    50%      { transform: translateY(3px); }
  }
  @media (prefers-reduced-motion: reduce) {
    .scroll-hint__arrow { animation: none; }
    .scroll-hint,
    .scroll-hint__pill { transition: none; }
  }
</style>
