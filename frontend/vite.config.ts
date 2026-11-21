import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

// The control plane the dev and preview servers proxy to. Set
// BABELSUITE_API_URL to point the UI at a backend on another address.
const env = loadEnv('development', '..', '')
const apiTarget = env.BABELSUITE_API_URL || 'http://localhost:8090'

export default defineConfig({
  envDir: '..',
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
        ws: true,
      },
      '/auth': {
        target: apiTarget,
        changeOrigin: true,
        // /auth/callback is a page of this app (the single sign-on landing
        // route), not an API path, so it must be served by the SPA.
        bypass: (req) => (req.url?.startsWith('/auth/callback') ? '/index.html' : undefined),
      },
    },
  },
})
