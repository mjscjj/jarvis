export interface ProjectProgressDraft {
  focus: string
  progress: string
  risk: string
  next: string
}

export const emptyProjectProgressDraft: ProjectProgressDraft = {
  focus: '',
  progress: '',
  risk: '',
  next: '',
}

type ProjectProgressSection = keyof ProjectProgressDraft

function progressSection(title: string): ProjectProgressSection | undefined {
  const normalized = title.replace(/\s+/g, '')
  if (normalized === '本周重点' || normalized === '本周') return 'focus'
  if (normalized === '当前进展' || normalized === '进展') return 'progress'
  if (normalized === '风险' || normalized === '风险/需要支持' || normalized === '风险与支持') return 'risk'
  if (normalized === '下周计划') return 'next'
  return undefined
}

export function parseProjectProgress(summary: string | null | undefined): ProjectProgressDraft {
  const source = summary?.trim()
  if (!source) return { ...emptyProjectProgressDraft }

  const draft = { ...emptyProjectProgressDraft }
  const looseLines: string[] = []
  let active: ProjectProgressSection | undefined
  let foundSection = false

  for (const line of source.split(/\r?\n/)) {
    const heading = line.match(/^#{1,6}\s+(.+?)\s*$/)
    const section = heading ? progressSection(heading[1]) : undefined
    if (section) {
      active = section
      foundSection = true
      continue
    }
    if (active) {
      draft[active] += `${draft[active] ? '\n' : ''}${line}`
    } else {
      looseLines.push(line)
    }
  }

  if (!foundSection) return { ...draft, progress: source }
  const loose = looseLines.join('\n').trim()
  if (loose) draft.progress = [loose, draft.progress].filter(Boolean).join('\n\n')
  for (const key of Object.keys(draft) as ProjectProgressSection[]) draft[key] = draft[key].trim()
  return draft
}

export function serializeProjectProgress(draft: ProjectProgressDraft): string {
  return [
    ['本周重点', draft.focus],
    ['当前进展', draft.progress],
    ['风险 / 需要支持', draft.risk],
    ['下周计划', draft.next],
  ].map(([title, content]) => `## ${title}\n\n${content.trim()}`).join('\n\n')
}

export function hasProjectProgress(draft: ProjectProgressDraft): boolean {
  return Object.values(draft).some((value) => value.trim() !== '')
}
