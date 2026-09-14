// All browser API and attachment URLs belong to the current installation.
export function appPath(path: string, base?: string): string {
  const prefix = base ?? (typeof document === 'undefined' ? '/' : document.querySelector<HTMLMetaElement>('meta[name="jarvis-base"]')?.content || '/')
  if (prefix === '/' || !path.startsWith('/') || path.startsWith('//') || path.startsWith(prefix)) return path
  return prefix.replace(/\/$/, '') + path
}
