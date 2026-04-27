import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: 'dist',
    emptyOutDir: false,
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
      '/api': 'http://localhost:8080',
    },
  },
});
