// The site is a project Pages site: it serves under /opcode/, so
// BASE_URL carries the subpath and every internal link goes through
// it — the lesson from the root-absolute 404s of the first version.
import { defineConfig } from 'astro/config';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  site: 'https://chmgx81.github.io',
  base: '/opcode/',
  vite: {
    plugins: [tailwindcss()],
  },
});
