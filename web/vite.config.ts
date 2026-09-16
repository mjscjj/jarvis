import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

function currentInstance() {
  const script = fileURLToPath(new URL('../scripts/jarvis-instance', import.meta.url))
  return JSON.parse(execFileSync(script, { encoding: 'utf8' })) as {
    api_base: string
    web_base_path: string
    okr_entry_path: string
    main_workbench_path: string
  }
}

function developmentServer(apiBase: string) {
  const port = Number(new URL(apiBase).port) + 1
  if (port > 65535) throw new Error('server.addr leaves no adjacent frontend development port')
  return { port, strictPort: true, proxy: { '/api': apiBase, '/healthz': apiBase } }
}

export default defineConfig(({ command }) => {
  const { api_base: apiBase, web_base_path: webBasePath, okr_entry_path: okrEntryPath, main_workbench_path: mainWorkbenchPath } = currentInstance()
  return {
    base: webBasePath,
    plugins: [react(), tailwindcss(), {
      name: 'instance-navigation',
      transformIndexHtml: () => [
        { tag: 'meta', attrs: { name: 'jarvis-okr-entry', content: okrEntryPath }, injectTo: 'head' as const },
        { tag: 'meta', attrs: { name: 'jarvis-main-workbench', content: mainWorkbenchPath }, injectTo: 'head' as const },
      ],
    }],
    server: command === 'serve' ? developmentServer(apiBase) : undefined,
    // Cached entry chunks can still request an older lazy chunk after a deploy.
    // Keep content-hashed assets so those in-flight clients do not receive 404s.
    build: { emptyOutDir: false },
  }
})
