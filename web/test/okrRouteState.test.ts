import assert from 'node:assert/strict'
import test from 'node:test'

import { activeQuarterForViewState, okrPlanDefaultQuarter, quarterFromViewState } from '../src/okr/routeState.ts'

test('okr route state reads the requested quarter from shared weekly links', () => {
  assert.equal(quarterFromViewState({ share: 'weekly', tab: 'okr-plan', quarter: '2026-Q4', week: '2026-W36' }), '2026-Q4')
  assert.equal(activeQuarterForViewState({ share: 'weekly', tab: 'okr-plan', quarter: '2026-Q4', week: '2026-W36' }, '2026-Q3'), '2026-Q4')
})

test('okr route state falls back to the workspace quarter when no route quarter exists outside OKR Plan', () => {
  assert.equal(activeQuarterForViewState({ tab: 'manage' }, '2026-Q3'), '2026-Q3')
})

test('OKR Plan defaults to next quarter in the last month of each quarter', () => {
  assert.equal(okrPlanDefaultQuarter(new Date(2026, 2, 1)), '2026-Q2')
  assert.equal(okrPlanDefaultQuarter(new Date(2026, 5, 30)), '2026-Q3')
  assert.equal(okrPlanDefaultQuarter(new Date(2026, 8, 3)), '2026-Q4')
  assert.equal(okrPlanDefaultQuarter(new Date(2026, 11, 31)), '2027-Q1')
  assert.equal(activeQuarterForViewState({ tab: 'okr-plan' }, '2026-Q3', new Date(2026, 8, 3)), '2026-Q4')
})

test('OKR Plan defaults to the current quarter outside the last month', () => {
  assert.equal(quarterFromViewState({ tab: 'okr-plan' }), '')
  assert.equal(okrPlanDefaultQuarter(new Date(2026, 6, 1)), '2026-Q3')
  assert.equal(okrPlanDefaultQuarter(new Date(2026, 7, 31)), '2026-Q3')
  assert.equal(activeQuarterForViewState({ tab: 'okr-plan' }, '2026-Q2', new Date(2026, 6, 1)), '2026-Q3')
})
