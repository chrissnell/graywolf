import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// Graywolf web UI — Svelte 5 scaffold. Output lands in web/dist/ where
// the Go embed.FS picks it up at compile time.
export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
});
