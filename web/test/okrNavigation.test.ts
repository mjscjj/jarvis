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
    'manage',
    'agent-flows',
    'weekly-fill',
    'weekly-meeting',
  ])
  assert.equal(DEFAULT_OKR_TAB, 'manage')
})

test('resolves removed, invalid and disabled child routes to OKR management', () => {
  assert.equal(isOKRTab('manage'), true)
  assert.equal(isOKRTab('structure'), false)
  assert.equal(isOKRTab('unknown'), false)
  assert.equal(resolveOKRTab('manage', { 'weekly-report': false }), 'manage')
  assert.equal(resolveOKRTab('weekly-fill', { 'weekly-report': true }), 'weekly-fill')
  assert.equal(resolveOKRTab('weekly-meeting', { 'weekly-report': false }), 'manage')
  assert.equal(resolveOKRTab('structure', { 'weekly-report': true }), 'manage')
  assert.equal(resolveOKRTab('unknown', { 'weekly-report': true }), 'manage')
})
