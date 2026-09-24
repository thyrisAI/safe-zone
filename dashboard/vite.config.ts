import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // Frontend'den atılan "/api/..." istekleri, development sırasında
      // gerçek Safe Zone backend'ine (localhost:8080) yönlendirilir.
      // Böylece frontend kodunda backend adresi hiç hardcode edilmez,
      // ve tarayıcı açısından istekler "aynı origin"den gidiyormuş gibi
      // görünür (CORS sorunlarını development'ta önler).
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        // "/api/patterns" -> "/patterns" (backend /api önekini tanımıyor)
        rewrite: (path) => path.replace(/^\/api/, ''),
      },
    },
  },
})