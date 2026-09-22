import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: 'dist',
    // Clean dist/ on every build. With this false, stale hashed chunks from
    // prior builds piled up under dist/assets/ (100+ found in one repo) and
    // got embedded into the Go binary forever (web/embed.go's go:embed),
    // needlessly bloating it and inviting confusion about which bundle is
    // live.
    emptyOutDir: true,
    // vendor-map (maplibre-gl + pmtiles) is large. Every route (not just the
    // map ones) is dynamically imported now -- App.svelte wraps all of them
    // in svelte-spa-router's wrap() -- so Rollup's own automatic
    // code-splitting gives each route, and maplibre-gl/lib/map(s)/*, their
    // own async chunk(s), fetched only when the operator visits that route.
    // A manualChunks override that forced an explicit 'vendor-map' name here
    // was tried and reverted: Rollup's manual-chunk claiming of its own
    // dependencies pulled in genuinely shared modules (api.js, stores/,
    // settings/) that every page also needs, which forced them to be
    // statically imported *from* that chunk -- silently making the "lazy"
    // chunk load eagerly on every page anyway. Letting Rollup split
    // everything automatically avoids that trap; verified by inspecting the
    // built dist/index.html for stray <link rel=modulepreload> references
    // to route chunks (there should be none).
    // Raise the warning limit so the expected large map chunk isn't flagged.
    chunkSizeWarningLimit: 1700,
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8081',
        ws: true,
        changeOrigin: false,
      },
    },
  },
});
