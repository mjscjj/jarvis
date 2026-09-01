import assert from 'node:assert/strict'
import test from 'node:test'
import {
  DEFAULT_OKR_TAB,
  OKR_TAB_DEFINITIONS,
  isOKRTab,
  isWeeklyWorkspaceTab,
  okrTabForWeeklyMode,
  resolveOKRTab,
  weeklyWorkspaceMode,
} from '../src/okr/navigation.ts'

test('defines the OKR directory children in their visible order', () => {
  assert.deepEqual(OKR_TAB_DEFINITIONS.map((item) => item.key), [
    'manage',
    'agent-flows',
    'weekly-fill',
    'weekly-meeting',
    'okr-review',
  ])
  assert.equal(OKR_TAB_DEFINITIONS.find((item) => item.key === 'agent-flows')?.label, '自动化流程')
  assert.equal(DEFAULT_OKR_TAB, 'manage')
})

test('resolves removed, invalid and disabled child routes to OKR management', () => {
  assert.equal(isOKRTab('manage'), true)
  assert.equal(isOKRTab('structure'), false)
  assert.equal(isOKRTab('unknown'), false)
  assert.equal(resolveOKRTab('manage', { 'weekly-report': false }), 'manage')
  assert.equal(resolveOKRTab('weekly-fill', { 'weekly-report': true }), 'weekly-fill')
	assert.equal(resolveOKRTab('okr-review', { 'weekly-report': true }), 'okr-review')
  assert.equal(resolveOKRTab('weekly-meeting', { 'weekly-report': false }), 'manage')
	assert.equal(resolveOKRTab('okr-review', { 'weekly-report': false }), 'manage')
  assert.equal(resolveOKRTab('structure', { 'weekly-report': true }), 'manage')
  assert.equal(resolveOKRTab('unknown', { 'weekly-report': true }), 'manage')
})

test('maps the three shared-data weekly pages without inventing another surface', () => {
	assert.equal(isWeeklyWorkspaceTab('manage'), false)
	assert.equal(isWeeklyWorkspaceTab('weekly-fill'), true)
	assert.equal(isWeeklyWorkspaceTab('weekly-meeting'), true)
	assert.equal(isWeeklyWorkspaceTab('okr-review'), true)
	assert.equal(weeklyWorkspaceMode('weekly-fill'), 'fill')
	assert.equal(weeklyWorkspaceMode('weekly-meeting'), 'meeting')
	assert.equal(weeklyWorkspaceMode('okr-review'), 'review')
	assert.equal(okrTabForWeeklyMode('fill'), 'weekly-fill')
	assert.equal(okrTabForWeeklyMode('meeting'), 'weekly-meeting')
	assert.equal(okrTabForWeeklyMode('review'), 'okr-review')
	assert.throws(() => weeklyWorkspaceMode('manage'), /not a weekly workspace/)
})
