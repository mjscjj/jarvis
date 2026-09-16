export interface ProjectProgressDraft {
  focus: string
  progress: string
  next: string
}

export const emptyProjectProgressDraft: ProjectProgressDraft = {
  focus: '',
  progress: '',
  next: '',
}

type ProjectProgressSection = keyof ProjectProgressDraft

function progressSection(title: string): ProjectProgressSection | undefined {
  const normalized = title.replace(/\s+/g, '')
  if (normalized === '本周重点' || normalized === '本周') return 'focus'
  if (normalized === '当前进展' || normalized === '进展') return 'progress'
  if (normalized === '下周计划') return 'next'
  return undefined
}

export function parseProjectProgress(summary: string | null | undefined): ProjectProgressDraft {
  const source = summary?.trim()
  if (!source) return { ...emptyProjectProgressDraft }

  const draft = { ...emptyProjectProgressDraft }
  const looseLines: string[] = []
  let active: ProjectProgressSection | undefined
  let skippingLegacyRisk = false
  let foundSection = false

  for (const line of source.split(/\r?\n/)) {
    const heading = line.match(/^#{1,6}\s+(.+?)\s*$/)
    const section = heading ? progressSection(heading[1]) : undefined
    if (section) {
      active = section
      skippingLegacyRisk = false
      foundSection = true
      continue
    }
    // Older progress snapshots may contain a risk section. Risk now has its
    // own ProjectRisk entity, so ignore that legacy section instead of moving
    // it into current progress or writing it back as a duplicate truth source.
    if (heading && /^(风险|风险\s*[/／]\s*需要支持|风险与支持)$/.test(heading[1].trim())) {
      active = undefined
      skippingLegacyRisk = true
      foundSection = true
      continue
    }
    if (skippingLegacyRisk) continue
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
    ['下周计划', draft.next],
  ].map(([title, content]) => `## ${title}\n\n${content.trim()}`).join('\n\n')
}

export function hasProjectProgress(draft: ProjectProgressDraft): boolean {
  return Object.values(draft).some((value) => value.trim() !== '')
}
