<script>
  // MapContextMenu: positioned overlay shown on right-click (or long-press)
  // of the map background. A muted coordinate header sits at the top, then
  // the primary "Add fixed beacon here" action, then a copy group. The
  // parent owns visibility/position and the item model; this component is
  // purely presentational and emits clicks back via each item's onSelect.
  //
  // Items are plain objects: { label, icon?, hint?, primary?, disabled?,
  // onSelect? } or { divider: true } for a separator rule. Closing is the
  // parent's responsibility for pan/zoom (those listeners live on the map);
  // this component closes itself on outside-click and Escape via onclose.

  import { onDestroy } from 'svelte';

  let {
    open = false,
    x = 0,
    y = 0,
    header = '',
    items = [],
    onclose = () => {},
  } = $props();

  let menuEl = $state(null);

  // Clamp the menu inside the viewport so it doesn't get cut off when
  // right-clicking near the right or bottom edge. Computed in the effect
  // after the menu mounts (so we can measure its rect); until then it
  // sits at the raw cursor position. Seeded with 0 (not x/y) so it isn't
  // a non-reactive capture of the prop -- the effect below sets the real
  // value before paint, resetting to raw x/y until the rect is measurable.
  let adjustedX = $state(0);
  let adjustedY = $state(0);
  $effect(() => {
    if (!open || !menuEl || typeof window === 'undefined') {
      adjustedX = x;
      adjustedY = y;
      return;
    }
    const rect = menuEl.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const pad = 8;
    let nx = x;
    let ny = y;
    if (nx + rect.width + pad > vw) nx = Math.max(pad, vw - rect.width - pad);
    if (ny + rect.height + pad > vh) ny = Math.max(pad, vh - rect.height - pad);
    adjustedX = nx;
    adjustedY = ny;
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
    {#if header}
      <div class="menu-header">{header}</div>
    {/if}
    {#each items as item}
      {#if item.divider}
        <div class="menu-divider" role="separator"></div>
      {:else}
        {@const IconCmp = item.icon}
        <button
          type="button"
          class="menu-item"
          class:menu-item--primary={item.primary}
          role="menuitem"
          disabled={item.disabled}
          onclick={() => {
            if (item.disabled) return;
            onclose();
            item.onSelect?.();
          }}
        >
          {#if IconCmp}
            <span class="menu-icon"><IconCmp size={14} strokeWidth={2} /></span>
          {/if}
          <span class="menu-label">{item.label}</span>
          {#if item.hint}
            <span class="menu-hint">{item.hint}</span>
          {/if}
        </button>
      {/if}
    {/each}
  </div>
{/if}

<style>
  .map-context-menu {
    position: fixed;
    z-index: 80;
    min-width: 184px;
    padding: 4px;
    background: var(--map-overlay-bg);
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 8px;
    box-shadow: var(--map-overlay-shadow);
    font-family: var(--font-mono);
    font-size: 13px;
    user-select: none;
  }
  .menu-header {
    padding: 5px 10px 6px;
    font-size: 12px;
    line-height: 1.2;
    color: var(--map-overlay-muted);
    white-space: nowrap;
  }
  .menu-divider {
    height: 1px;
    margin: 4px 6px;
    background: var(--map-overlay-border);
  }
  .menu-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 6px 10px;
    border: none;
    background: transparent;
    color: inherit;
    text-align: left;
    font: inherit;
    cursor: pointer;
    border-radius: 5px;
    white-space: nowrap;
  }
  .menu-icon {
    display: inline-flex;
    flex: 0 0 auto;
    color: var(--map-overlay-muted);
  }
  .menu-item--primary .menu-icon {
    color: var(--color-primary);
  }
  .menu-label {
    flex: 1 1 auto;
  }
  .menu-hint {
    flex: 0 0 auto;
    padding-left: 8px;
    font-size: 12px;
    color: var(--map-overlay-muted);
  }
  .menu-item:hover:not(:disabled),
  .menu-item:focus-visible:not(:disabled) {
    /* Tint toward text color so the hover shows in both light and dark
       themes (the bare white-alpha fallback vanishes on light surfaces). */
    background: var(
      --color-surface-hover,
      color-mix(in srgb, var(--color-text) 9%, transparent)
    );
    color: var(--color-text);
    outline: none;
  }
  .menu-item:hover:not(:disabled) .menu-hint,
  .menu-item:focus-visible:not(:disabled) .menu-hint {
    color: var(--color-text);
  }
  .menu-item:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
