// 16-color ANSI palette for the AX.25 terminal viewport.
//
// Values use CSS custom properties so the active graywolf theme can
// override them. The default fallbacks form a classic white-on-black
// terminal that works on any chrome theme out of the box; phosphor
// presets (see ./presets.ts) selectively override a subset of these
// vars to deliver the green/amber CRT looks.
//
// xterm cannot resolve var(...) itself -- buildTheme() in ./theme.js
// resolves these strings against the active document at construction
// time and re-resolves on theme/contrast changes.

export const ANSI_PALETTE = {
  black:         'var(--gw-ansi-black,         #000000)',
  red:           'var(--gw-ansi-red,           #cc0000)',
  green:         'var(--gw-ansi-green,         #00aa00)',
  yellow:        'var(--gw-ansi-yellow,        #aaaa00)',
  blue:          'var(--gw-ansi-blue,          #0000aa)',
  magenta:       'var(--gw-ansi-magenta,       #aa00aa)',
  cyan:          'var(--gw-ansi-cyan,          #00aaaa)',
  white:         'var(--gw-ansi-white,         #ffffff)',
  brightBlack:   'var(--gw-ansi-bright-black,  #555555)',
  brightRed:     'var(--gw-ansi-bright-red,    #ff5555)',
  brightGreen:   'var(--gw-ansi-bright-green,  #55ff55)',
  brightYellow:  'var(--gw-ansi-bright-yellow, #ffff55)',
  brightBlue:    'var(--gw-ansi-bright-blue,   #5555ff)',
  brightMagenta: 'var(--gw-ansi-bright-magenta,#ff55ff)',
  brightCyan:    'var(--gw-ansi-bright-cyan,   #55ffff)',
  brightWhite:   'var(--gw-ansi-bright-white,  #ffffff)',
} as const;

export const TERMINAL_DEFAULTS = {
  background: 'var(--gw-term-bg,     #000000)',
  foreground: 'var(--gw-term-fg,     #ffffff)',
  cursor:     'var(--gw-term-cursor, #ffffff)',
} as const;
