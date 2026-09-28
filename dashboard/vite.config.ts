import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // During development, "/api/..." requests from the frontend are
      // forwarded to the real Safe Zone backend (localhost:8080), so the
      // backend address is never hardcoded in frontend code.
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        // "/api/patterns" -> "/patterns" (the backend does not know the /api prefix)
        rewrite: (path) => path.replace(/^\/api/, ''),
        configure: (proxy) => {
          // The browser adds an Origin header to POST/PATCH/DELETE requests.
          // Since the proxy makes these requests effectively same-origin,
          // strip it so the backend's CORS allowlist is not needed in dev.
          proxy.on('proxyReq', (proxyReq) => {
            proxyReq.removeHeader('origin')
          })
        },
      },
    },
  },
})
