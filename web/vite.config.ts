import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 18801,
    strictPort: true,
    proxy: {
      '/api': 'http://127.0.0.1:18800',
      '/healthz': 'http://127.0.0.1:18800',
    },
  },
})
