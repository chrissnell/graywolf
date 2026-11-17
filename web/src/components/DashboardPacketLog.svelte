<script>
  import { onMount, tick } from 'svelte';
  import PacketLogViewer from './PacketLogViewer.svelte';
  import {
    DEFAULT_LOG_HEIGHT, MIN_LOG_HEIGHT, maxLogHeight,
    draggedLogHeight, resizeEdgeStep, keyedLogHeight, readLogHeight, saveLogHeight,
  } from '../lib/packetLogResize.js';

  let { packets = [], autoscroll = true, newestFirst = false, ...viewerProps } = $props();
  let root = $state();
  let desiredHeight = $state(readLogHeight());
  let maximum = $state(DEFAULT_LOG_HEIGHT);
  const height = $derived(Math.min(desiredHeight, maximum));
  let drag = $state(null);
  let measureFrame;
  let dragFrame;
  let mounted = false;

  // Query afresh: Chonky replaces the body when switching grid/card layouts.
  function body() { return root?.querySelector('.log-body'); }

  function measure() {
    const el = body();
    const last = el?.lastElementChild;
    if (!el || !last || !el.offsetHeight) return;
    const rect = el.getBoundingClientRect();
    const scale = rect.height / el.offsetHeight || 1;
    // scrollHeight cannot measure content shorter than an expanded viewport.
    // The last entry's edge works even when scrolled or the header is sticky.
    const content = (last.getBoundingClientRect().bottom - rect.top) / scale
      + el.scrollTop - el.clientTop + parseFloat(getComputedStyle(el).paddingBottom || '0');
    maximum = maxLogHeight(content);
  }

  function scheduleMeasure() {
    if (!mounted) return;
    cancelAnimationFrame(measureFrame);
    measureFrame = requestAnimationFrame(measure);
  }

  onMount(() => {
    mounted = true;
    const observer = new ResizeObserver(scheduleMeasure);
    observer.observe(root);
    scheduleMeasure();
    return () => {
      stopDrag();
      mounted = false;
      observer.disconnect();
      cancelAnimationFrame(measureFrame);
    };
  });

  $effect(() => {
    packets;
    tick().then(scheduleMeasure);
  });

  // Height changes keep a reader's position, or keep the bottom in view when
  // already following oldest-first traffic. Never jump to an edge from mid-log.
  $effect.pre(() => {
    height;
    const el = body();
    if (!el) return;
    const top = el.scrollTop;
    const tolerance = el.classList.contains('log-body-card') ? 40 : 2;
    const followBottom = autoscroll && !newestFirst
      && el.scrollHeight - top - el.clientHeight < tolerance;
    tick().then(() => {
      const current = body();
      if (current) current.scrollTop = followBottom ? current.scrollHeight : top;
    });
  });

  function startDrag(event) {
    if (!event.isPrimary || event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.focus({ preventScroll: true });
    const el = body();
    drag = {
      id: event.pointerId, y: event.clientY, lastY: event.clientY,
      scrollY: window.scrollY, time: performance.now(),
      scale: el?.offsetHeight ? el.getBoundingClientRect().height / el.offsetHeight : 1,
    };
    event.currentTarget.setPointerCapture(event.pointerId);
    dragFrame = requestAnimationFrame(continueDrag);
  }

  async function continueDrag(now) {
    const active = drag;
    if (!active) return;
    const edge = resizeEdgeStep(active.y, window.innerHeight, now - active.time);
    const delta = active.y - active.lastY + window.scrollY - active.scrollY;
    const before = height;
    desiredHeight = draggedLogHeight(height, delta + edge, active.scale, maximum);
    const growth = desiredHeight - before;
    active.lastY = active.y;
    active.time = now;
    await tick();
    if (drag !== active) return;
    // Grow first so there is page space to scroll into, even at the old end.
    // Only scroll for actual growth/shrinkage; stop at the content/height cap.
    if (edge && growth * edge > 0) {
      window.scrollBy(0, Math.sign(edge) * Math.min(Math.abs(edge), Math.abs(growth * active.scale)));
    }
    active.scrollY = window.scrollY;
    dragFrame = requestAnimationFrame(continueDrag);
  }

  function moveDrag(event) {
    if (drag?.id !== event.pointerId) return;
    if (event.buttons === 0) { stopDrag(); return; }
    drag.y = event.clientY;
  }

  function stopDrag() {
    if (!drag) return;
    drag = null;
    cancelAnimationFrame(dragFrame);
    saveLogHeight(desiredHeight);
  }

  function finishDrag(event) {
    if (drag?.id !== event.pointerId) return;
    stopDrag();
  }

  function resizeByKey(event) {
    const next = keyedLogHeight(event.key, height, maximum);
    if (next === null) return;
    event.preventDefault();
    desiredHeight = next;
    saveLogHeight(next);
  }

  function reset() {
    desiredHeight = DEFAULT_LOG_HEIGHT;
    saveLogHeight(desiredHeight);
  }
</script>

<svelte:window onblur={stopDrag} />

<div bind:this={root} class="dashboard-packet-log">
  <PacketLogViewer {packets} {autoscroll} {newestFirst} {...viewerProps} height={`${height}px`} />
  <div class="resize-controls">
    <div
      class="resize-grip"
      class:dragging={drag !== null}
      role="slider"
      tabindex="0"
      aria-label="Packet log height"
      aria-orientation="vertical"
      aria-valuemin={MIN_LOG_HEIGHT}
      aria-valuemax={maximum}
      aria-valuenow={height}
      aria-valuetext={`${height} pixels`}
      title="Drag to resize; hold near the page edge to keep resizing. Arrow keys: 40 px; Page Up/Down: 200 px; Home/End: minimum/maximum."
      onpointerdown={startDrag}
      onpointermove={moveDrag}
      onpointerup={finishDrag}
      onpointercancel={finishDrag}
      onlostpointercapture={finishDrag}
      onkeydown={resizeByKey}
    ><span aria-hidden="true"></span></div>
    <button type="button" class="reset-height" onclick={reset} title="Restore the default 400 px packet log height">Reset height</button>
  </div>
</div>

<style>
  .dashboard-packet-log { min-width: 0; }
  .resize-controls { display: flex; align-items: stretch; gap: 0.5rem; }
  .resize-grip {
    display: flex; align-items: center; justify-content: center;
    flex: 1; min-height: 28px; cursor: ns-resize; touch-action: none;
    user-select: none; border-radius: 0 0 var(--radius) var(--radius);
  }
  .resize-grip span { width: 36px; height: 5px; border-block: 1px solid var(--color-text-dim); }
  .resize-grip:hover, .resize-grip.dragging { background: var(--color-surface-raised); }
  .resize-grip:focus-visible, .reset-height:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; }
  .reset-height { border: 0; background: transparent; color: var(--color-text-muted); font: inherit; font-size: 0.75rem; cursor: pointer; }
  .reset-height:hover { color: var(--color-text); }
  @media (pointer: coarse) { .resize-grip, .reset-height { min-height: 44px; } }
</style>
