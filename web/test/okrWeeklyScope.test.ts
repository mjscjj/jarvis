import assert from 'node:assert/strict'
import test from 'node:test'

import type { BoardData, BoardSurface, WeeklyReportWeekList } from '../src/okr/emily/api.ts'
import type { Objective } from '../src/okr/emily/types.ts'
import { resolveWeeklyBoardScope, type WeeklyScopeLoader } from '../src/okr/emily/weeklyScope.ts'

const reviewWeek = {
  quarter: '2026-Q3', week: '2026-W36', templateKey: 'okr_weekly_preview_v1' as const, openedBy: 'local', openedAt: '',
}

function board(quarter: string, availableQuarters: string[], week = '', objectives: Objective[] = []): BoardData {
  return {
    quarter,
    week,
    templateKey: week ? 'okr_weekly_preview_v1' : 'classic',
    availableQuarters,
    availableWeeks: week ? [week] : [],
    objectives,
  }
}

test('Review opened from a next-quarter Plan restores the latest formal OKR quarter', async () => {
  const calls: string[] = []
  const objectives = [{ id: 'o-q3', title: 'Q3', krs: [] }]
  const loader: WeeklyScopeLoader = {
    listWeeks: async (quarter: string): Promise<WeeklyReportWeekList> => {
      calls.push(`weeks:${quarter}`)
      return { quarter, weeks: quarter === '2026-Q3' ? [reviewWeek] : [] }
    },
    getBoard: async (quarter: string, week: string, surface: BoardSurface) => {
      calls.push(`board:${quarter}:${week}:${surface}`)
      return quarter === '2026-Q3'
        ? board(quarter, ['2026-Q3', '2026-Q2'], week, objectives)
        : board(quarter, ['2026-Q3', '2026-Q2'])
    },
  }

  const resolved = await resolveWeeklyBoardScope('2026-Q4', undefined, 'okr_weekly_preview_v1', loader)

  assert.deepEqual(calls, [
    'weeks:2026-Q4',
    'board:2026-Q4::okr',
    'weeks:2026-Q3',
    'board:2026-Q3:2026-W36:weekly-report',
  ])
  assert.equal(resolved.quarter, '2026-Q3')
  assert.equal(resolved.week, '2026-W36')
  assert.deepEqual(resolved.availableWeeks, ['2026-W36'])
  assert.equal(resolved.objectives[0]?.id, 'o-q3')
})

test('a formal quarter with no Review week remains selected so a Review can be created', async () => {
  const calls: string[] = []
  const loader: WeeklyScopeLoader = {
    listWeeks: async (quarter: string) => {
      calls.push(`weeks:${quarter}`)
      return { quarter, weeks: [] }
    },
    getBoard: async (quarter: string, week: string, surface: BoardSurface) => {
      calls.push(`board:${quarter}:${week}:${surface}`)
      return board(quarter, ['2026-Q4', '2026-Q3'], '', [{ id: 'o-q4', title: 'Q4', krs: [] }])
    },
  }

  const resolved = await resolveWeeklyBoardScope('2026-Q4', undefined, 'okr_weekly_preview_v1', loader)

  assert.deepEqual(calls, ['weeks:2026-Q4', 'board:2026-Q4::okr'])
  assert.equal(resolved.quarter, '2026-Q4')
  assert.equal(resolved.week, '')
  assert.deepEqual(resolved.availableWeeks, [])
  assert.deepEqual(resolved.objectives, [])
})
