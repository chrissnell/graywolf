// Named ANSI palette presets. The operator selects one via
// AX25TerminalConfig.Theme; TerminalViewport.svelte emits a <style>
// block scoped to the viewport that sets the listed CSS custom
// properties. Vars not listed here fall through to the classic
// white-on-black defaults baked into ANSI_PALETTE / TERMINAL_DEFAULTS.

export type PresetName = 'classic' | 'phosphor-green' | 'phosphor-amber' | 'high-contrast';

export const PRESETS: Record<PresetName, Record<string, string>> = {
  classic: {
    // Empty -- defaults in palette.ts apply.
  },
  'phosphor-green': {
    '--gw-term-bg':           '#001000',
    '--gw-term-fg':           '#33ff66',
    '--gw-term-cursor':       '#33ff66',
    '--gw-ansi-green':        '#33ff66',
    '--gw-ansi-bright-green': '#7fffa0',
  },
  'phosphor-amber': {
    '--gw-term-bg':            '#100800',
    '--gw-term-fg':            '#ffb000',
    '--gw-term-cursor':        '#ffb000',
    '--gw-ansi-yellow':        '#ffb000',
    '--gw-ansi-bright-yellow': '#ffd060',
  },
  // WCAG AAA-contrast palette (>= 7:1 for body text). Pure-saturated
  // FG/BG pairs with pure-white text on pure-black background; ANSI
  // colors are bumped to the brightest variants so even small fonts
  // pass AAA at default xterm.js cell sizes.
  'high-contrast': {
    '--gw-term-bg':              '#000000',
    '--gw-term-fg':              '#ffffff',
    '--gw-term-cursor':          '#ffff00',
    '--gw-term-selection-bg':    '#ffff00',
    '--gw-term-selection-fg':    '#000000',
    '--gw-ansi-black':           '#000000',
    '--gw-ansi-red':             '#ff5555',
    '--gw-ansi-green':           '#55ff55',
    '--gw-ansi-yellow':          '#ffff55',
    '--gw-ansi-blue':            '#7fbfff',
    '--gw-ansi-magenta':         '#ff7fff',
    '--gw-ansi-cyan':            '#7fffff',
    '--gw-ansi-white':           '#ffffff',
    '--gw-ansi-bright-black':    '#bfbfbf',
    '--gw-ansi-bright-red':      '#ff8080',
    '--gw-ansi-bright-green':    '#80ff80',
    '--gw-ansi-bright-yellow':   '#ffff80',
    '--gw-ansi-bright-blue':     '#a0c8ff',
    '--gw-ansi-bright-magenta':  '#ffa0ff',
    '--gw-ansi-bright-cyan':     '#a0ffff',
    '--gw-ansi-bright-white':    '#ffffff',
  },
};
