<script>
  // Sticky banner that appears at the bottom of a scrollable parent when
  // there is hidden content below the viewport. Drop as the last child
  // inside the scroll container (e.g. Modal.Body). It walks up the DOM
  // until it finds an element with overflow scroll/auto and observes it.
  let { label = 'Scroll down for more' } = $props();

  let sentinel = $state(null);
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

  $effect(() => {
    if (!sentinel) return;
    const el = findScrollParent(sentinel);
    if (!el) return;

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
    };
  });
</script>

<div
  bind:this={sentinel}
  class="scroll-hint"
  class:visible
  aria-hidden="true"
>
  <span class="scroll-hint__label">{label}</span>
  <span class="scroll-hint__arrow">&#8595;</span>
</div>

<style>
  .scroll-hint {
    position: sticky;
    bottom: 0;
    left: 0;
    right: 0;
    margin: 0 calc(-1 * var(--scroll-hint-pad-x, 1.5rem)) calc(-1 * var(--scroll-hint-pad-y, 1.5rem));
    padding: 10px 1.5rem 12px;
    display: flex;
    justify-content: center;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--color-text, #111827);
    background: linear-gradient(
      to bottom,
      transparent 0%,
      var(--color-surface, #fff) 50%,
      var(--color-surface, #fff) 100%
    );
    pointer-events: none;
    opacity: 0;
    transform: translateY(4px);
    transition: opacity 120ms ease, transform 120ms ease;
    z-index: 5;
  }
  .scroll-hint.visible {
    opacity: 1;
    transform: translateY(0);
  }
  .scroll-hint__arrow {
    display: inline-block;
    font-size: 14px;
    animation: scroll-hint-bounce 1.4s ease-in-out infinite;
  }
  @keyframes scroll-hint-bounce {
    0%, 100% { transform: translateY(0); }
    50%      { transform: translateY(3px); }
  }
  @media (prefers-reduced-motion: reduce) {
    .scroll-hint__arrow { animation: none; }
    .scroll-hint { transition: none; }
  }
</style>
