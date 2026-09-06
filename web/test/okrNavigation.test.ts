import assert from 'node:assert/strict'
import test from 'node:test'
import {
  DEFAULT_OKR_TAB,
  OKR_TAB_DEFINITIONS,
  isOKRTab,
  isWeeklyWorkspaceTab,
  okrTabForWeeklyWorkspace,
  resolveOKRTab,
  weeklyWorkspace,
} from '../src/okr/navigation.ts'

test('defines the OKR directory children in their visible order', () => {
  assert.deepEqual(OKR_TAB_DEFINITIONS.map((item) => item.key), [
    'manage',
    'okr-plan',
    'agent-flows',
    'review-fill',
    'review-meeting',
    'weekly-fill',
    'weekly-meeting',
  ])
  assert.equal(OKR_TAB_DEFINITIONS.find((item) => item.key === 'okr-plan')?.label, 'OKR Plan')
  assert.equal(OKR_TAB_DEFINITIONS.find((item) => item.key === 'agent-flows')?.label, '自动化流程')
  assert.equal(DEFAULT_OKR_TAB, 'manage')
})

test('separates Review and weekly report into their own sidebar groups', () => {
  const groups = OKR_TAB_DEFINITIONS.map((item) => item.group)
  assert.deepEqual(groups, ['okr', 'okr', 'okr', 'review', 'review', 'weekly', 'weekly'])
})

test('resolves removed, invalid and disabled child routes to OKR management', () => {
  assert.equal(isOKRTab('manage'), true)
  assert.equal(isOKRTab('okr-plan'), true)
  assert.equal(isOKRTab('structure'), false)
  assert.equal(isOKRTab('unknown'), false)
  // The single-page Review tab was replaced by the fill/meeting pair.
  assert.equal(isOKRTab('okr-review'), false)
  assert.equal(resolveOKRTab('manage', { 'biz-okr': false }), 'manage')
  assert.equal(resolveOKRTab('weekly-fill', { 'biz-okr': true }), 'weekly-fill')
  assert.equal(resolveOKRTab('review-fill', { 'biz-okr': true }), 'review-fill')
  assert.equal(resolveOKRTab('review-meeting', { 'biz-okr': true }), 'review-meeting')
  assert.equal(resolveOKRTab('weekly-meeting', { 'biz-okr': false }), 'manage')
  assert.equal(resolveOKRTab('review-fill', { 'biz-okr': false }), 'manage')
  assert.equal(resolveOKRTab('okr-review', { 'biz-okr': true }), 'manage')
  assert.equal(resolveOKRTab('structure', { 'biz-okr': true }), 'manage')
  assert.equal(resolveOKRTab('unknown', { 'biz-okr': true }), 'manage')
})

test('maps every weekly tab to one dataset and one view, and back', () => {
  assert.equal(isWeeklyWorkspaceTab('manage'), false)
  assert.equal(isWeeklyWorkspaceTab('agent-flows'), false)
  for (const tab of ['review-fill', 'review-meeting', 'weekly-fill', 'weekly-meeting'] as const) {
    assert.equal(isWeeklyWorkspaceTab(tab), true)
    assert.equal(okrTabForWeeklyWorkspace(weeklyWorkspace(tab)), tab)
  }
  assert.deepEqual(weeklyWorkspace('review-fill'), { dataset: 'review', view: 'fill' })
  assert.deepEqual(weeklyWorkspace('review-meeting'), { dataset: 'review', view: 'meeting' })
  assert.deepEqual(weeklyWorkspace('weekly-fill'), { dataset: 'weekly', view: 'fill' })
  assert.deepEqual(weeklyWorkspace('weekly-meeting'), { dataset: 'weekly', view: 'meeting' })
  assert.throws(() => weeklyWorkspace('manage'), /not a weekly workspace/)
})
