import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Component browser tests have no backend proxy and cannot write product data.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  cacheDir: '/tmp/emily-vite-fixture-cache',
  server: { host: '0.0.0.0', port: 4178, strictPort: true },
})
