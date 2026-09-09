import assert from 'node:assert/strict'
import test from 'node:test'

import { pageHash, routeFromHash } from '../src/pageRoutes.ts'

test('maps the public Biz OKR hash to the registered Biz OKR page', () => {
  assert.deepEqual(routeFromHash('#/biz-okr?quarter=2026-Q3&tab=manage', 'overview'), {
    key: 'biz-okr',
    selection: null,
    viewState: { quarter: '2026-Q3', tab: 'manage' },
  })
  assert.equal(pageHash('biz-okr', null, { quarter: '2026-Q3', tab: 'manage' }), '#/biz-okr?quarter=2026-Q3&tab=manage')
})

test('keeps legacy OKR hashes readable after the Biz OKR rename', () => {
  assert.deepEqual(routeFromHash('#/okr?quarter=2026-Q3&tab=manage', 'overview'), {
    key: 'biz-okr',
    selection: null,
    viewState: { quarter: '2026-Q3', tab: 'manage' },
  })
})

test('redirects legacy agent and security settings hashes to their current pages', () => {
  assert.deepEqual(routeFromHash('#/agents?stage=m3', 'overview'), {
    key: 'settings',
    selection: null,
    viewState: { stage: 'm3', view: 'agents' },
  })
  assert.deepEqual(routeFromHash('#/manage/settings?view=security', 'overview'), {
    key: 'security',
    selection: null,
    viewState: {},
  })
})

test('maps a weekly share hash to the Biz OKR page and preserves its public path', () => {
  assert.deepEqual(routeFromHash('#/weekly-report?quarter=2026-Q3&week=2026-W36', 'overview'), {
    key: 'biz-okr',
    selection: null,
    viewState: { quarter: '2026-Q3', week: '2026-W36', share: 'weekly', tab: 'weekly-fill' },
  })
  assert.equal(
    pageHash('biz-okr', null, { quarter: '2026-Q3', week: '2026-W36', share: 'weekly', tab: 'weekly-fill' }),
    '#/weekly-report?quarter=2026-Q3&tab=weekly-fill&week=2026-W36',
  )
})
