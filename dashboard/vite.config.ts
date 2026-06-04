import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/conversation': {
        target: 'http://127.0.0.1:2112',
        changeOrigin: true,
      },
      '/metrics': {
        target: 'http://127.0.0.1:2112',
        changeOrigin: true,
      },
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
})
