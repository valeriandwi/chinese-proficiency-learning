import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// The SPA and the API must be SAME-ORIGIN. In development this proxy is what makes
// that true: the browser only ever talks to localhost:5174, so the session cookie
// is first-party and is sent on every request. Pointing VITE_API_BASE_URL at an
// absolute http://127.0.0.1:8081 would make them different sites, and the session
// cookie would be silently dropped.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    host: 'localhost',
    port: 5174,
    strictPort: true,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8081',
        changeOrigin: false,
      },
      // Reference audio is served by the API as a same-origin path. Without this
      // second rule an <audio src="/media/…"> would be answered by Vite itself and
      // 404, so every listening exercise would look broken in development only.
      // Production must route both paths the same way (see deploy/Caddyfile).
      '/media': {
        target: 'http://127.0.0.1:8081',
        changeOrigin: false,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    // Process CSS in tests. Without this every CSS import (including `?raw`) is
    // stubbed to an empty string, so src/lib/design-tokens.test.ts could not read
    // the tokens it asserts on and would silently pass on nothing.
    css: true,
  },
})
