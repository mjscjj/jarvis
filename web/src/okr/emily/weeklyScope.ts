import type { BoardData, BoardSurface, WeeklyReportWeekList } from './api'
import { fallbackQuarterForUnavailableScope } from './quarterCatalog.ts'
import type { WeekTemplateKey } from './types'
import { filterWeekCatalog, previousWeekInCatalog } from './weekCatalog.ts'

export interface WeeklyScopeLoader {
  listWeeks: (quarter: string) => Promise<WeeklyReportWeekList>
  getBoard: (quarter: string, week: string, surface: BoardSurface) => Promise<BoardData>
}

interface LoadedQuarter {
  board: BoardData
  selectedWeek: string
  availableWeeks: string[]
}

async function loadQuarter(
  quarter: string,
  requestedWeek: string,
  templateKey: WeekTemplateKey,
  loader: WeeklyScopeLoader,
): Promise<LoadedQuarter> {
  const catalog = await loader.listWeeks(quarter)
  const availableWeeks = filterWeekCatalog(catalog.weeks, templateKey)
  const selectedWeek = requestedWeek && availableWeeks.includes(requestedWeek)
    ? requestedWeek
    : availableWeeks[0] ?? ''
  const board = selectedWeek
    ? await loader.getBoard(catalog.quarter, selectedWeek, 'weekly-report')
    : await loader.getBoard(catalog.quarter, '', 'okr')
  return { board, selectedWeek, availableWeeks }
}

export async function resolveWeeklyBoardScope(
  quarter: string,
  requestedWeek: string | undefined,
  templateKey: WeekTemplateKey,
  loader: WeeklyScopeLoader,
): Promise<BoardData> {
  const requested = requestedWeek?.trim() ?? ''
  let loaded = await loadQuarter(quarter, requested, templateKey, loader)
  const fallbackQuarter = fallbackQuarterForUnavailableScope(
    loaded.board.quarter,
    loaded.board.availableQuarters,
  )
  if (fallbackQuarter) loaded = await loadQuarter(fallbackQuarter, requested, templateKey, loader)

  if (!loaded.selectedWeek) {
    return {
      ...loaded.board,
      week: '',
      templateKey,
      previousWeek: undefined,
      availableWeeks: [],
      objectives: [],
    }
  }
  return {
    ...loaded.board,
    week: loaded.selectedWeek,
    templateKey,
    previousWeek: previousWeekInCatalog(loaded.availableWeeks, loaded.selectedWeek),
    availableWeeks: loaded.availableWeeks,
  }
}
