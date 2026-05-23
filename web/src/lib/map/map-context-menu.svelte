<script>
  // MapContextMenu: positioned overlay shown on right-click (or long-press)
  // of the map background. Three items: Copy GPS, Copy grid, Add fixed
  // beacon here. The parent owns visibility and position; this component
  // is purely presentational and emits clicks back via the items prop.
  //
  // Closing is the parent's responsibility -- pan/zoom listeners live on
  // the map and are easier to wire there. This component closes itself
  // on outside-click and Escape, both of which call onclose.

  import { onDestroy } from 'svelte';

  let {
    open = false,
    x = 0,
    y = 0,
    items = [],
    onclose = () => {},
  } = $props();

  let menuEl = $state(null);

  // Clamp the menu inside the viewport so it doesn't get cut off when
  // right-clicking near the right or bottom edge. Measured after mount;
  // until then the menu sits at the raw cursor position. The clamp uses
  // the menu's own rect, so it adapts to the longest visible item.
  let adjustedX = $derived(x);
  let adjustedY = $derived(y);
  $effect(() => {
    if (!open || !menuEl || typeof window === 'undefined') return;
    const rect = menuEl.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const pad = 8;
    let nx = x;
    let ny = y;
    if (nx + rect.width + pad > vw) nx = Math.max(pad, vw - rect.width - pad);
    if (ny + rect.height + pad > vh) ny = Math.max(pad, vh - rect.height - pad);
    if (nx !== adjustedX) adjustedX = nx;
    if (ny !== adjustedY) adjustedY = ny;
  });

  function onWindowDown(ev) {
    if (!open) return;
    if (menuEl && menuEl.contains(ev.target)) return;
    onclose();
  }
  function onKeyDown(ev) {
    if (!open) return;
    if (ev.key === 'Escape') onclose();
  }
  $effect(() => {
    if (typeof window === 'undefined') return;
    if (!open) return;
    // pointerdown (rather than click) so the menu closes on the same
    // physical event that initiates a new map click/drag -- waiting for
    // 'click' lets a drag start with the menu still painted.
    window.addEventListener('pointerdown', onWindowDown, true);
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('pointerdown', onWindowDown, true);
      window.removeEventListener('keydown', onKeyDown);
    };
  });

  onDestroy(() => {
    if (typeof window === 'undefined') return;
    window.removeEventListener('pointerdown', onWindowDown, true);
    window.removeEventListener('keydown', onKeyDown);
  });
</script>

{#if open}
  <div
    bind:this={menuEl}
    class="map-context-menu"
    role="menu"
    style="left: {adjustedX}px; top: {adjustedY}px;"
  >
    {#each items as item}
      <button
        type="button"
        class="menu-item"
        role="menuitem"
        disabled={item.disabled}
        onclick={() => {
          if (item.disabled) return;
          onclose();
          item.onSelect?.();
        }}
      >
        {item.label}
      </button>
    {/each}
  </div>
{/if}

<style>
  .map-context-menu {
    position: fixed;
    z-index: 80;
    min-width: 200px;
    padding: 4px;
    background: var(--map-overlay-bg);
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 6px;
    box-shadow: var(--map-overlay-shadow);
    font-family: var(--font-mono);
    font-size: 13px;
    user-select: none;
  }
  .menu-item {
    display: block;
    width: 100%;
    padding: 6px 10px;
    border: none;
    background: transparent;
    color: inherit;
    text-align: left;
    font: inherit;
    cursor: pointer;
    border-radius: 4px;
    white-space: nowrap;
  }
  .menu-item:hover:not(:disabled),
  .menu-item:focus-visible:not(:disabled) {
    background: var(--color-surface-hover, rgba(255, 255, 255, 0.08));
    color: var(--color-text);
    outline: none;
  }
  .menu-item:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
