import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { buildCountryTree } from './catalog-tree.js';
import { groupDownloadedByCountry } from './downloaded-groups.js';

const fix = {
  countries: [
    { iso2: 'us', name: 'United States', sizeBytes: 100 },
    { iso2: 'ca', name: 'Canada', sizeBytes: 200 },
    { iso2: 'de', name: 'Germany', sizeBytes: 300 },
  ],
  provinces: [
    { iso2: 'ca', slug: 'british-columbia', name: 'British Columbia', code: 'BC', sizeBytes: 50 },
    { iso2: 'ca', slug: 'ontario', name: 'Ontario', code: 'ON', sizeBytes: 60 },
  ],
  states: [
    { slug: 'colorado', name: 'Colorado', code: 'CO', sizeBytes: 70 },
    { slug: 'wyoming', name: 'Wyoming', code: 'WY', sizeBytes: 80 },
  ],
};

describe('groupDownloadedByCountry', () => {
  it('groups a single downloaded child under its country with correct counts', () => {
    const tree = buildCountryTree(fix);
    const rows = [{ slug: 'state/colorado', name: 'Colorado', bytes_total: 70 }];
    const entries = groupDownloadedByCountry(tree, rows);
    assert.equal(entries.length, 1);
    assert.equal(entries[0].kind, 'group');
    assert.equal(entries[0].name, 'United States');
    assert.equal(entries[0].downloadedCount, 1);
    assert.equal(entries[0].totalCount, 2);
    assert.deepEqual(entries[0].rows.map((r) => r.slug), ['state/colorado']);
  });

  it('groups multiple downloaded children in one group, sorted by name', () => {
    const tree = buildCountryTree(fix);
    const rows = [
      { slug: 'province/ca/ontario', name: 'Ontario', bytes_total: 60 },
      { slug: 'province/ca/british-columbia', name: 'British Columbia', bytes_total: 50 },
    ];
    const entries = groupDownloadedByCountry(tree, rows);
    assert.equal(entries.length, 1);
    assert.equal(entries[0].downloadedCount, 2);
    assert.equal(entries[0].totalCount, 2);
    assert.deepEqual(entries[0].rows.map((r) => r.name), ['British Columbia', 'Ontario']);
  });

  it('a downloaded country-level slug with no catalog children stays a single', () => {
    const tree = buildCountryTree(fix);
    const rows = [{ slug: 'country/de', name: 'Germany', bytes_total: 300 }];
    const entries = groupDownloadedByCountry(tree, rows);
    assert.deepEqual(entries, [{ kind: 'single', slug: 'country/de', name: 'Germany', bytes_total: 300 }]);
  });

  it('a downloaded slug absent from the tree falls back to single without throwing', () => {
    const tree = buildCountryTree(fix);
    const rows = [{ slug: 'world', name: 'World (low detail)', bytes_total: 314572800 }];
    const entries = groupDownloadedByCountry(tree, rows);
    assert.equal(entries.length, 1);
    assert.equal(entries[0].kind, 'single');
  });

  it('falls back to all singles when the catalog tree is empty (not yet loaded)', () => {
    const rows = [
      { slug: 'state/colorado', name: 'Colorado', bytes_total: 70 },
      { slug: 'country/de', name: 'Germany', bytes_total: 300 },
    ];
    const entries = groupDownloadedByCountry([], rows);
    assert.equal(entries.length, 2);
    assert.ok(entries.every((e) => e.kind === 'single'));
  });

  it('returns an empty array for no downloaded rows', () => {
    const tree = buildCountryTree(fix);
    assert.deepEqual(groupDownloadedByCountry(tree, []), []);
  });

  it('every input row appears exactly once across groups and singles', () => {
    const tree = buildCountryTree(fix);
    const rows = [
      { slug: 'state/colorado', name: 'Colorado', bytes_total: 70 },
      { slug: 'state/wyoming', name: 'Wyoming', bytes_total: 80 },
      { slug: 'country/de', name: 'Germany', bytes_total: 300 },
    ];
    const entries = groupDownloadedByCountry(tree, rows);
    const seenSlugs = new Set();
    for (const e of entries) {
      if (e.kind === 'single') seenSlugs.add(e.slug);
      else for (const r of e.rows) seenSlugs.add(r.slug);
    }
    assert.deepEqual([...seenSlugs].sort(), rows.map((r) => r.slug).sort());
  });
});
