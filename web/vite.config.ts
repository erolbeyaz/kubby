import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

const API_TARGET = process.env.KUBBY_API_TARGET ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    // Bind to all interfaces: under WSL2 a 127.0.0.1-only listener is not reliably
    // reachable from the Windows browser.
    host: true,

    // WSL2 delivers no inotify events for files on /mnt/c, so the watcher has to poll.
    // Without this the dev server silently serves whatever it loaded at startup.
    watch: {
      usePolling: true,
      interval: 400,
    },
    port: 5173,
    strictPort: true,
    // The dev server runs on its own port, so the browser's Origin header would never
    // match the API's public URL and every mutating request would be rejected. The
    // proxy presents the API's own origin, which is what the browser sees in production
    // where the SPA is served from the same origin.
    proxy: Object.fromEntries(
      ['/api', '/healthz', '/readyz', '/version'].map((path) => [
        path,
        {
          target: API_TARGET,
          changeOrigin: true,
          headers: { Origin: API_TARGET },
        },
      ]),
    ),
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    // The bundle is embedded in the Go binary; a flat, hashed asset layout keeps the
    // embed directory predictable.
    assetsDir: 'assets',
  },
})
