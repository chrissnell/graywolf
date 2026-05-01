import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { buildCountryTree } from './catalog-tree.js';

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

describe('buildCountryTree', () => {
  it('groups provinces under their country', () => {
    const tree = buildCountryTree(fix);
    const ca = tree.find(n => n.iso2 === 'ca');
    assert.ok(ca);
    assert.deepEqual(ca.children.map(c => c.slug), [
      'province/ca/british-columbia',
      'province/ca/ontario',
    ]);
  });

  it('US states attach to the synthetic US country', () => {
    const tree = buildCountryTree(fix);
    const us = tree.find(n => n.iso2 === 'us');
    assert.ok(us);
    assert.deepEqual(us.children.map(c => c.slug), ['state/colorado', 'state/wyoming']);
  });

  it('countries with no children show an empty children array', () => {
    const tree = buildCountryTree(fix);
    const de = tree.find(n => n.iso2 === 'de');
    assert.deepEqual(de.children, []);
  });

  it('US states attach to a synthetic US node when countries[] does not include US', () => {
    const tree = buildCountryTree({ ...fix, countries: fix.countries.filter(c => c.iso2 !== 'us') });
    const us = tree.find(n => n.iso2 === 'us');
    assert.ok(us);
    assert.equal(us.children.length, 2);
    assert.equal(us.synthetic, true);
  });

  it('sorts countries alphabetically by name', () => {
    const tree = buildCountryTree(fix);
    const names = tree.map(n => n.name);
    assert.deepEqual(names, [...names].sort());
  });

  it('attaches namespaced slug to every node', () => {
    const tree = buildCountryTree(fix);
    const ca = tree.find(n => n.iso2 === 'ca');
    assert.equal(ca.slug, 'country/ca');
    assert.equal(ca.children[0].slug, 'province/ca/british-columbia');
    const us = tree.find(n => n.iso2 === 'us');
    assert.equal(us.children[0].slug, 'state/colorado');
  });
});
