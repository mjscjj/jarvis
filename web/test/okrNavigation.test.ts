import assert from 'node:assert/strict'
import test from 'node:test'
import {
  DEFAULT_OKR_TAB,
  OKR_TAB_DEFINITIONS,
  isOKRTab,
  resolveOKRTab,
} from '../src/okr/navigation.ts'

test('defines the OKR directory children in their visible order', () => {
  assert.deepEqual(OKR_TAB_DEFINITIONS.map((item) => item.key), [
    'structure',
    'manage',
    'agent-flows',
    'weekly-fill',
    'weekly-meeting',
  ])
  assert.equal(DEFAULT_OKR_TAB, 'structure')
})

test('resolves invalid and disabled child routes to OKR structure', () => {
  assert.equal(isOKRTab('manage'), true)
  assert.equal(isOKRTab('unknown'), false)
  assert.equal(resolveOKRTab('manage', { 'weekly-report': false }), 'manage')
  assert.equal(resolveOKRTab('weekly-fill', { 'weekly-report': true }), 'weekly-fill')
  assert.equal(resolveOKRTab('weekly-meeting', { 'weekly-report': false }), 'structure')
  assert.equal(resolveOKRTab('unknown', { 'weekly-report': true }), 'structure')
})
