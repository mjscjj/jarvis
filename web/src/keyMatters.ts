import type { KeyMatter, KeyMatterInput } from './types'

export function keyMatterToInput(matter: KeyMatter, patch: Partial<KeyMatterInput> = {}): KeyMatterInput {
  return {
    title: matter.title,
    status: matter.status,
    summary: matter.summary,
    project_id: matter.project_id,
    due_at: matter.due_at,
    ...patch,
  }
}

export function replaceKeyMatter(items: KeyMatter[], saved: KeyMatter): KeyMatter[] {
  return items.map((item) => item.id === saved.id ? saved : item)
}
