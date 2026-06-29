<script>
  // InfoPanel: a reusable map overlay container.
  //   - Desktop (tall/wide): floating card anchored top-right (or top-left
  //     when anchor='left'), positioned absolutely over the map.
  //   - Compact (narrow portrait OR short landscape phone): bottom-sheet
  //     via chonky-ui Drawer.
  //
  // The mobile/desktop split is driven by a matchMedia listener so the
  // panel reflows live if the operator resizes their window or rotates
  // their device. Body content is provided as a snippet child.

  import { Drawer } from '@chrissnell/chonky-ui';
  import { COMPACT_LAYOUT_QUERY } from '../compactLayout.js';

  let {
    title,
    open = $bindable(false),
    anchor = 'right',
    children,
  } = $props();

  let isMobile = $state(false);
  $effect(() => {
    if (typeof window === 'undefined') return;
    const mq = window.matchMedia(COMPACT_LAYOUT_QUERY);
    isMobile = mq.matches;
    const handler = (e) => (isMobile = e.matches);
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  });
</script>

{#if isMobile}
  <Drawer bind:open anchor="bottom" aria-label={title}>
    <Drawer.Header>{title}</Drawer.Header>
    <Drawer.Body>{@render children?.()}</Drawer.Body>
  </Drawer>
{:else if open}
  <aside class="info-panel" data-anchor={anchor} aria-label={title}>
    <header class="info-panel-header">
      <h2>{title}</h2>
      <button
        type="button"
        class="info-panel-close"
        onclick={() => (open = false)}
        aria-label="Close panel"
      >
        &times;
      </button>
    </header>
    <div class="info-panel-body">{@render children?.()}</div>
  </aside>
{/if}

<style>
  .info-panel {
    position: absolute;
    /* Sit below the FAB (top:12 + 44 + 8) on desktop; the FAB itself
       sits to the right of MapLibre's NavigationControl. */
    top: 64px;
    right: 12px;
    width: 280px;
    background: var(--map-overlay-bg);
    -webkit-backdrop-filter: blur(var(--map-overlay-blur, 0));
    backdrop-filter: blur(var(--map-overlay-blur, 0));
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 8px;
    box-shadow: var(--map-overlay-shadow);
    z-index: 50;
  }
  .info-panel[data-anchor='left'] {
    right: auto;
    left: 12px;
  }
  .info-panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 12px;
    border-bottom: 1px solid var(--map-overlay-border);
  }
  .info-panel-header h2 {
    margin: 0;
    font-size: 13px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 1px;
  }
  .info-panel-close {
    background: transparent;
    border: none;
    color: var(--map-overlay-fg);
    width: 36px;
    height: 36px;
    cursor: pointer;
    font-size: 22px;
    line-height: 1;
  }
  .info-panel-close:hover {
    color: var(--color-text);
  }
  .info-panel-body {
    padding: 12px;
    max-height: 60vh;
    overflow-y: auto;
  }
</style>
