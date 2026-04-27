import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { THEMES, DEFAULT_THEME_ID, isValidTheme } from './registry.js';
import manifest from '../../../themes/themes.json' with { type: 'json' };

describe('theme registry', () => {
  it('exposes every theme declared in themes.json', () => {
    assert.deepEqual(
      THEMES.map((t) => t.id),
      manifest.themes.map((t) => t.id),
    );
  });

  it('default matches manifest default', () => {
    assert.equal(DEFAULT_THEME_ID, manifest.default);
    assert.ok(THEMES.find((t) => t.id === DEFAULT_THEME_ID));
  });

  it('every theme has a human-readable name', () => {
    for (const t of THEMES) {
      assert.equal(typeof t.name, 'string');
      assert.ok(t.name.length > 0, `theme ${t.id} needs a name`);
    }
  });

  it('isValidTheme accepts only shipped ids', () => {
    for (const t of THEMES) {
      assert.equal(isValidTheme(t.id), true, t.id);
    }
    assert.equal(isValidTheme(''), false);
    assert.equal(isValidTheme(null), false);
    assert.equal(isValidTheme(undefined), false);
    assert.equal(isValidTheme(123), false);
    assert.equal(isValidTheme('Graywolf'), false); // case-sensitive
    assert.equal(isValidTheme('unicorn'), false);
  });
});
