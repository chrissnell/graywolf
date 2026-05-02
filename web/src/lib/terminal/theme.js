// buildTheme() returns an xterm ITheme whose color values are the
// resolved values of the --gw-ansi-* and --gw-term-* CSS custom
// properties at call time. xterm cannot resolve var(...) itself, so we
// must compute concrete hex strings.
//
// The caller (TerminalViewport.svelte) re-runs buildTheme() and calls
// term.options.theme = ... whenever the active theme changes (a
// MutationObserver on <html data-theme>) or when prefers-contrast
// flips, so the terminal palette tracks chrome-theme switches.

import { ANSI_PALETTE, TERMINAL_DEFAULTS } from './palette.ts';

// extractFallback parses 'var(--name, #rrggbb)' -> '#rrggbb'. Used
// when the live document has not defined the var (e.g. SSR or before
// the theme stylesheet attaches).
function extractFallback(spec) {
  const m = /var\([^,]+,\s*([^)]+)\)/.exec(spec);
  return m ? m[1].trim() : spec;
}

// readVar returns the resolved value of a 'var(--name, fallback)' spec
// against the given root element. Falls back to the inline fallback in
// the spec when the var is unset.
function readVar(rootStyle, spec) {
  const m = /var\(\s*(--[a-z0-9-]+)/i.exec(spec);
  if (!m) return spec;
  const live = rootStyle.getPropertyValue(m[1]).trim();
  if (live) return live;
  return extractFallback(spec);
}

// buildTheme resolves every palette entry against the active document
// and returns an object xterm accepts as ITheme.
export function buildTheme(root = document.documentElement) {
  // High-contrast tweak: when prefers-contrast: more is set, force
  // pure-black background + pure-white foreground regardless of theme
  // overrides so the terminal stays readable. Operators with that
  // preference on are explicitly asking for maximum contrast.
  const wantsHighContrast =
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-contrast: more)').matches;

  if (typeof window === 'undefined' || typeof getComputedStyle !== 'function') {
    // SSR / non-browser path: return the baked fallbacks.
    return {
      background: extractFallback(TERMINAL_DEFAULTS.background),
      foreground: extractFallback(TERMINAL_DEFAULTS.foreground),
      cursor:     extractFallback(TERMINAL_DEFAULTS.cursor),
      black:         extractFallback(ANSI_PALETTE.black),
      red:           extractFallback(ANSI_PALETTE.red),
      green:         extractFallback(ANSI_PALETTE.green),
      yellow:        extractFallback(ANSI_PALETTE.yellow),
      blue:          extractFallback(ANSI_PALETTE.blue),
      magenta:       extractFallback(ANSI_PALETTE.magenta),
      cyan:          extractFallback(ANSI_PALETTE.cyan),
      white:         extractFallback(ANSI_PALETTE.white),
      brightBlack:   extractFallback(ANSI_PALETTE.brightBlack),
      brightRed:     extractFallback(ANSI_PALETTE.brightRed),
      brightGreen:   extractFallback(ANSI_PALETTE.brightGreen),
      brightYellow:  extractFallback(ANSI_PALETTE.brightYellow),
      brightBlue:    extractFallback(ANSI_PALETTE.brightBlue),
      brightMagenta: extractFallback(ANSI_PALETTE.brightMagenta),
      brightCyan:    extractFallback(ANSI_PALETTE.brightCyan),
      brightWhite:   extractFallback(ANSI_PALETTE.brightWhite),
    };
  }
  const cs = getComputedStyle(root);
  const theme = {
    background: readVar(cs, TERMINAL_DEFAULTS.background),
    foreground: readVar(cs, TERMINAL_DEFAULTS.foreground),
    cursor:     readVar(cs, TERMINAL_DEFAULTS.cursor),
    black:         readVar(cs, ANSI_PALETTE.black),
    red:           readVar(cs, ANSI_PALETTE.red),
    green:         readVar(cs, ANSI_PALETTE.green),
    yellow:        readVar(cs, ANSI_PALETTE.yellow),
    blue:          readVar(cs, ANSI_PALETTE.blue),
    magenta:       readVar(cs, ANSI_PALETTE.magenta),
    cyan:          readVar(cs, ANSI_PALETTE.cyan),
    white:         readVar(cs, ANSI_PALETTE.white),
    brightBlack:   readVar(cs, ANSI_PALETTE.brightBlack),
    brightRed:     readVar(cs, ANSI_PALETTE.brightRed),
    brightGreen:   readVar(cs, ANSI_PALETTE.brightGreen),
    brightYellow:  readVar(cs, ANSI_PALETTE.brightYellow),
    brightBlue:    readVar(cs, ANSI_PALETTE.brightBlue),
    brightMagenta: readVar(cs, ANSI_PALETTE.brightMagenta),
    brightCyan:    readVar(cs, ANSI_PALETTE.brightCyan),
    brightWhite:   readVar(cs, ANSI_PALETTE.brightWhite),
  };
  if (wantsHighContrast) {
    theme.background = '#000000';
    theme.foreground = '#ffffff';
    theme.cursor = '#ffffff';
  }
  return theme;
}
