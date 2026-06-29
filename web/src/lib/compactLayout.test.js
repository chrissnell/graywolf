import { test } from 'node:test';
import assert from 'node:assert/strict';
import { COMPACT_LAYOUT_QUERY } from './compactLayout.js';

// The query is hand-mirrored into CSS (Sidebar.svelte, App.svelte,
// maplibre-map.svelte). Pinning the exact string makes any JS-side edit a
// deliberate, reviewed change that prompts a matching CSS update rather
// than silently drifting the two apart.
test('COMPACT_LAYOUT_QUERY matches the CSS-mirrored breakpoint exactly', () => {
  assert.equal(
    COMPACT_LAYOUT_QUERY,
    '(max-width: 768px), (orientation: landscape) and (max-height: 500px)',
  );
});

test('COMPACT_LAYOUT_QUERY covers narrow portrait via max-width', () => {
  assert.match(COMPACT_LAYOUT_QUERY, /max-width:\s*768px/);
});

test('COMPACT_LAYOUT_QUERY covers short landscape phones', () => {
  // The landscape clause is what lets a rotated phone stay in the compact
  // chrome instead of falling back to the desktop layout (GH #419).
  assert.match(
    COMPACT_LAYOUT_QUERY,
    /\(orientation:\s*landscape\)\s*and\s*\(max-height:\s*500px\)/,
  );
});

test('COMPACT_LAYOUT_QUERY is a comma-joined OR of the two cases', () => {
  const clauses = COMPACT_LAYOUT_QUERY.split(',').map((c) => c.trim());
  assert.equal(clauses.length, 2);
  assert.equal(clauses[0], '(max-width: 768px)');
  assert.equal(clauses[1], '(orientation: landscape) and (max-height: 500px)');
});
