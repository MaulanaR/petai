import { defineConfig } from 'vite';

export default defineConfig({
  // preview.html reads the built-in animation JSON from ../internal/anim/builtin
  server: { fs: { allow: ['..'] } },
});
