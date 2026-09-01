import assert from 'node:assert/strict'
import test from 'node:test'
import type { WeeklyReportWeek } from '../src/okr/emily/api.ts'
import { filterWeekCatalog, previousWeekInCatalog, templateKeyForWeeklyMode } from '../src/okr/emily/weekCatalog.ts'

const weeks: WeeklyReportWeek[] = [
  { quarter: '2026-Q3', week: '2026-W37', templateKey: 'okr_weekly_preview_v1', openedBy: 'a', openedAt: '' },
  { quarter: '2026-Q3', week: '2026-W36', templateKey: 'classic', openedBy: 'a', openedAt: '' },
  { quarter: '2026-Q3', week: '2026-W35', templateKey: 'classic', openedBy: 'a', openedAt: '' },
]

test('weekly tabs map to independent week templates', () => {
  assert.equal(templateKeyForWeeklyMode('fill'), 'classic')
  assert.equal(templateKeyForWeeklyMode('meeting'), 'classic')
  assert.equal(templateKeyForWeeklyMode('review'), 'okr_weekly_preview_v1')
})

test('week catalog only exposes weeks owned by the active workspace', () => {
  assert.deepEqual(filterWeekCatalog(weeks, 'classic'), ['2026-W36', '2026-W35'])
  assert.deepEqual(filterWeekCatalog(weeks, 'okr_weekly_preview_v1'), ['2026-W37'])
  assert.equal(previousWeekInCatalog(['2026-W36', '2026-W35'], '2026-W36'), '2026-W35')
})
