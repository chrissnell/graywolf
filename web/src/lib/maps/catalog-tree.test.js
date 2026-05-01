import { describe, it, expect } from 'vitest';
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
    expect(ca).toBeTruthy();
    expect(ca.children.map(c => c.slug)).toEqual([
      'province/ca/british-columbia',
      'province/ca/ontario',
    ]);
  });

  it('US states attach to the synthetic US country', () => {
    const tree = buildCountryTree(fix);
    const us = tree.find(n => n.iso2 === 'us');
    expect(us).toBeTruthy();
    expect(us.children.map(c => c.slug)).toEqual(['state/colorado', 'state/wyoming']);
  });

  it('countries with no children show an empty children array', () => {
    const tree = buildCountryTree(fix);
    const de = tree.find(n => n.iso2 === 'de');
    expect(de.children).toEqual([]);
  });

  it('US states attach to a synthetic US node when countries[] does not include US', () => {
    const tree = buildCountryTree({ ...fix, countries: fix.countries.filter(c => c.iso2 !== 'us') });
    const us = tree.find(n => n.iso2 === 'us');
    expect(us).toBeTruthy();
    expect(us.children.length).toBe(2);
    expect(us.synthetic).toBe(true);
  });

  it('sorts countries alphabetically by name', () => {
    const tree = buildCountryTree(fix);
    const names = tree.map(n => n.name);
    expect(names).toEqual([...names].sort());
  });

  it('attaches namespaced slug to every node', () => {
    const tree = buildCountryTree(fix);
    const ca = tree.find(n => n.iso2 === 'ca');
    expect(ca.slug).toBe('country/ca');
    expect(ca.children[0].slug).toBe('province/ca/british-columbia');
    const us = tree.find(n => n.iso2 === 'us');
    expect(us.children[0].slug).toBe('state/colorado');
  });
});
