import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

function developmentServer() {
  const script = fileURLToPath(new URL('../scripts/jarvis-instance', import.meta.url))
  const { api_base: apiBase } = JSON.parse(execFileSync(script, { encoding: 'utf8' })) as {
    api_base: string
  }
  const port = Number(new URL(apiBase).port) + 1
  if (port > 65535) throw new Error('server.addr leaves no adjacent frontend development port')
  return { port, strictPort: true, proxy: { '/api': apiBase, '/healthz': apiBase } }
}

export default defineConfig(({ command }) => ({
  plugins: [react(), tailwindcss()],
  server: command === 'serve' ? developmentServer() : undefined,
  // Cached entry chunks can still request an older lazy chunk after a deploy.
  // Keep content-hashed assets so those in-flight clients do not receive 404s.
  build: { emptyOutDir: false },
}))
