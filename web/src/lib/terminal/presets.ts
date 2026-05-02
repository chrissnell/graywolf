// Named ANSI palette presets. The operator selects one via
// AX25TerminalConfig.Theme (added in Phase 3); TerminalViewport.svelte
// emits a <style> block scoped to the viewport that sets the listed
// CSS custom properties. Vars not listed here fall through to the
// classic white-on-black defaults baked into ANSI_PALETTE / TERMINAL_DEFAULTS.

export type PresetName = 'classic' | 'phosphor-green' | 'phosphor-amber';

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
};
