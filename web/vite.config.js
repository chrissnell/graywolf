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
    // vendor-map (maplibre-gl + pmtiles) is large; it's split into its own
    // chunk so an unrelated app-code change doesn't bust its cache, but all
    // routes (including the map) are statically imported in App.svelte, so
    // it loads eagerly on every page. Raise the limit so that expected
    // chunk isn't flagged.
    chunkSizeWarningLimit: 1700,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (
            id.includes('node_modules/maplibre-gl') ||
            id.includes('node_modules/pmtiles') ||
            id.includes('node_modules/@mapbox/') ||
            id.includes('/src/lib/map/') ||
            id.includes('/src/lib/maps/')
          ) {
            return 'vendor-map';
          }
        },
      },
    },
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
