import test from 'node:test';
import assert from 'node:assert/strict';

import { buildTheme } from './theme.js';

// node has no DOM so buildTheme falls into the SSR branch by default
// (window/getComputedStyle undefined). For the high-contrast assertion
// we install a tiny stub matchMedia that flips the prefers-contrast: more
// query on, plus a minimal getComputedStyle so the live-resolved branch
// runs.

function withMockedDOM(fn) {
  const origWindow = globalThis.window;
  const origGCS = globalThis.getComputedStyle;
  const origDoc = globalThis.document;
  globalThis.window = {
    matchMedia: (q) => ({ matches: q.includes('prefers-contrast: more') }),
  };
  globalThis.document = { documentElement: { style: {} } };
  globalThis.getComputedStyle = () => ({ getPropertyValue: () => '' });
  try {
    fn();
  } finally {
    globalThis.window = origWindow;
    globalThis.getComputedStyle = origGCS;
    globalThis.document = origDoc;
  }
}

test('buildTheme honors prefers-contrast: more by applying high-contrast preset', () => {
  withMockedDOM(() => {
    const theme = buildTheme();
    assert.equal(theme.background, '#000000');
    assert.equal(theme.foreground, '#ffffff');
    assert.equal(theme.brightWhite, '#ffffff');
    // ANSI colors come from the high-contrast preset, not the classic
    // defaults. red is brightened from #cd3232 -> #ff5555.
    assert.equal(theme.red, '#ff5555');
  });
});
