<script>
  import { onMount, onDestroy } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { WebLinksAddon } from '@xterm/addon-web-links';
  import { SearchAddon } from '@xterm/addon-search';
  import '@xterm/xterm/css/xterm.css';

  import { buildTheme } from '../../lib/terminal/theme.js';
  import { PRESETS } from '../../lib/terminal/presets.ts';

  // session is a createSession() result from lib/terminal/session.svelte.js.
  // preset is one of the keys in PRESETS (defaults to 'classic').
  // narrowMin is the viewport-width threshold below which the canvas
  // is hidden in favor of a "screen too narrow" message.
  let { session, preset = 'classic', narrowMin = 768 } = $props();

  let host = $state(null);
  let term = null;
  let mounted = false;
  let viewportWidth = $state(typeof window !== 'undefined' ? window.innerWidth : 1024);
  let mo = null;
  let contrastMQ = null;

  // Resize listener is a one-time subscription with no reactive
  // dependencies, so it lives in onMount rather than a $effect that
  // would re-evaluate on every reactive read.
  let resizeListener = null;

  // Apply the named preset by writing CSS custom properties on the
  // viewport host element. xterm later resolves --gw-* vars via
  // buildTheme() against this same node, so the overrides take effect.
  function applyPreset(node) {
    if (!node) return;
    const overrides = PRESETS[preset] ?? {};
    for (const [k, v] of Object.entries(overrides)) node.style.setProperty(k, v);
  }

  function reapplyTheme() {
    if (!term || !host) return;
    term.options.theme = buildTheme(host);
  }

  onMount(async () => {
    mounted = true;
    if (typeof window !== 'undefined') {
      resizeListener = () => { viewportWidth = window.innerWidth; };
      window.addEventListener('resize', resizeListener);
    }
    if (!host) return;
    applyPreset(host);

    // Gate construction on font load. xterm measures the cell box at
    // Terminal() construction; if SauceCodePro hasn't loaded the cell
    // sizes against fallback metrics and the swap causes glyph
    // misalignment plus clipped Nerd Font icons. Combined with
    // font-display: block in saucecodepro-nerd-font.css this
    // eliminates the FOUC.
    try {
      if (typeof document !== 'undefined' && document.fonts && document.fonts.load) {
        await document.fonts.load('12px "SauceCodeProNF"');
        await document.fonts.ready;
      }
    } catch {
      // Some browsers throw if the descriptor parse fails -- fine,
      // just construct with whatever the user agent picks.
    }

    if (!mounted) return;
    term = new Terminal({
      cols: 80,
      rows: 24,
      // convertEol must be false: packet-radio BBSes (FBB, JNOS) emit
      // bare CR. xterm's convertEol: true rewrites bare LF to CRLF
      // and corrupts mixed-line-ending streams. Pass bytes through
      // verbatim.
      convertEol: false,
      cursorBlink: false,
      // screenReaderMode populates xterm's off-screen accessibility
      // tree for SR users.
      screenReaderMode: true,
      fontFamily: '"SauceCodeProNF", "Source Code Pro", Menlo, Consolas, monospace',
      fontSize: 14,
      theme: buildTheme(host),
      scrollback: 5000,
    });
    term.loadAddon(new WebLinksAddon());
    term.loadAddon(new SearchAddon());
    term.open(host);
    // Pull keyboard focus into the canvas so operators can type
    // immediately on session open without an extra click. SR users
    // also need this for the off-screen accessibility tree (xterm's
    // screenReaderMode-driven mirror) to start reading.
    queueMicrotask(() => {
      try { term?.focus(); } catch { /* ignore */ }
    });

    // Inbound bytes from the bridge. session.svelte.js passes a
    // Uint8Array; xterm 5+ accepts that directly with no UTF-8
    // decoding -- byte-faithful from BBS to glyph.
    session.state.onDataRX = (bytes) => {
      try { term?.write(bytes); } catch { /* terminal disposed */ }
    };

    // Operator keystrokes -> session bytes. xterm emits UTF-8 already
    // for keyboard input, so wrap in a TextEncoder to get bytes.
    const enc = new TextEncoder();
    term.onData((s) => {
      const bytes = enc.encode(s);
      session.sendData(bytes);
    });

    // Re-resolve the theme on graywolf chrome theme changes.
    if (typeof MutationObserver !== 'undefined') {
      mo = new MutationObserver(reapplyTheme);
      mo.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
    }
    if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
      contrastMQ = window.matchMedia('(prefers-contrast: more)');
      try { contrastMQ.addEventListener('change', reapplyTheme); }
      catch { contrastMQ.addListener?.(reapplyTheme); /* legacy */ }
    }
  });

  onDestroy(() => {
    mounted = false;
    if (resizeListener && typeof window !== 'undefined') {
      window.removeEventListener('resize', resizeListener);
      resizeListener = null;
    }
    try { mo?.disconnect(); } catch { /* ignore */ }
    try {
      if (contrastMQ) {
        try { contrastMQ.removeEventListener('change', reapplyTheme); }
        catch { contrastMQ.removeListener?.(reapplyTheme); }
      }
    } catch { /* ignore */ }
    try { term?.dispose(); } catch { /* ignore */ }
    term = null;
    if (session?.state) session.state.onDataRX = null;
  });

  let isNarrow = $derived(viewportWidth < narrowMin);
</script>

{#if isNarrow}
  <div class="terminal-narrow" role="status" aria-live="polite">
    <strong>Terminal requires a wider screen.</strong>
    <span>Rotate or resize this window to at least {narrowMin}px wide.</span>
  </div>
{:else}
  <div class="terminal-letterbox">
    <div class="terminal-host" bind:this={host}></div>
  </div>
{/if}

<style>
  /* Wide viewports get the canvas pinned at min-content so xterm's
     intrinsic 80x24 size letterboxes against the surrounding chrome.
     Avoids full-bleed empty space on 4K displays. */
  .terminal-letterbox {
    display: flex;
    justify-content: center;
    width: 100%;
    background: var(--color-surface, #f8f8f8);
    padding: 8px 0;
  }
  .terminal-host {
    max-width: min-content;
    margin: 0 auto;
  }
  .terminal-narrow {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 16px;
    border: 1px solid var(--color-border, #ddd);
    background: var(--color-surface, #f8f8f8);
    color: var(--color-text, #111);
    border-radius: 4px;
    font-size: 14px;
  }
</style>
