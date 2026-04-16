// Reactive unit system preference backed by localStorage.
// Follows the same $state rune + getter/setter IIFE pattern as map-store.svelte.js.

export const unitsState = (() => {
  const stored = localStorage.getItem('units-system');
  let system = $state(stored === 'metric' ? 'metric' : 'imperial');

  return {
    get system() { return system; },
    set system(v) {
      system = v;
      localStorage.setItem('units-system', v);
    },

    get isMetric() { return system === 'metric'; },
  };
})();
