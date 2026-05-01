// Pure function: project the catalog into a country-rooted tree.
//
// Output node shape:
//   { iso2, name, slug: 'country/<iso2>', sizeBytes,
//     children: [
//       // for non-US countries: provinces as { iso2, slug: 'province/<iso2>/<x>', name, code, sizeBytes }
//       // for US: states as { slug: 'state/<x>', name, code, sizeBytes }
//     ],
//     synthetic: true if this country was injected purely to host
//                states and not present in catalog.countries[]
//   }
//
// The picker uses this as the renderable source-of-truth; everything
// downstream (display, search, downloads) operates on namespaced slugs
// from the start.

const US_NAME = 'United States';

export function buildCountryTree(catalog) {
  const byIso = new Map();

  for (const c of catalog.countries ?? []) {
    byIso.set(c.iso2, {
      iso2: c.iso2,
      name: c.name,
      slug: `country/${c.iso2}`,
      sizeBytes: c.sizeBytes,
      bbox: c.bbox,
      children: [],
      synthetic: false,
    });
  }

  for (const p of catalog.provinces ?? []) {
    const parent = byIso.get(p.iso2);
    if (!parent) continue; // orphan province -- silently skip
    parent.children.push({
      iso2: p.iso2,
      slug: `province/${p.iso2}/${p.slug}`,
      name: p.name,
      code: p.code,
      sizeBytes: p.sizeBytes,
      bbox: p.bbox,
    });
  }

  // US states attach to the country-level "us" node, synthesised if absent.
  const states = catalog.states ?? [];
  if (states.length > 0) {
    let us = byIso.get('us');
    if (!us) {
      us = {
        iso2: 'us',
        name: US_NAME,
        slug: 'country/us',
        sizeBytes: 0,
        children: [],
        synthetic: true,
      };
      byIso.set('us', us);
    }
    for (const s of states) {
      us.children.push({
        slug: `state/${s.slug}`,
        name: s.name,
        code: s.code,
        sizeBytes: s.sizeBytes,
        bbox: s.bbox,
      });
    }
  }

  // Sort children alphabetically per country.
  for (const node of byIso.values()) {
    node.children.sort((a, b) => a.name.localeCompare(b.name));
  }

  // Sort countries alphabetically by display name.
  return [...byIso.values()].sort((a, b) => a.name.localeCompare(b.name));
}
