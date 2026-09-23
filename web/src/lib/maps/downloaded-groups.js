// Pure function: bucket downloaded rows under their parent country from
// the catalog tree, so the Downloaded list can mirror the picker's
// country-grouped layout (buildCountryTree in catalog-tree.js).
//
// tree: output of buildCountryTree(catalog); [] when the catalog hasn't
//   loaded yet, which simply yields all rows as singles.
// downloadedRows: array of { slug, name, ...download status fields } for
//   slugs currently in the 'complete' state.
//
// Returns a flat array sorted by name, each entry either:
//   { kind: 'single', ...row }
//   { kind: 'group', iso2, name, slug, downloadedCount, totalCount, rows }
// Every input row appears exactly once (a country-level whole-country
// download is never folded into its own children's group).
export function groupDownloadedByCountry(tree, downloadedRows) {
  const childToCountry = new Map(); // child slug -> owning country node
  for (const country of tree) {
    for (const child of country.children) {
      childToCountry.set(child.slug, country);
    }
  }

  const groups = new Map(); // country.slug -> group entry
  const entries = [];

  for (const row of downloadedRows) {
    const country = childToCountry.get(row.slug);
    if (!country) {
      entries.push({ kind: 'single', ...row });
      continue;
    }
    let group = groups.get(country.slug);
    if (!group) {
      group = {
        kind: 'group',
        iso2: country.iso2,
        name: country.name,
        slug: country.slug,
        downloadedCount: 0,
        totalCount: country.children.length,
        rows: [],
      };
      groups.set(country.slug, group);
      entries.push(group);
    }
    group.rows.push(row);
    group.downloadedCount += 1;
  }

  for (const group of groups.values()) {
    group.rows.sort((a, b) => a.name.localeCompare(b.name));
  }

  entries.sort((a, b) => a.name.localeCompare(b.name));
  return entries;
}
