import assert from 'node:assert/strict'
import test from 'node:test'

import { pageHash, routeFromHash } from '../src/pageRoutes.ts'

test('maps the public OKR hash to the registered Agency OKR page', () => {
  assert.deepEqual(routeFromHash('#/okr?quarter=2026-Q3&tab=manage', 'overview'), {
    key: 'agency-okr',
    selection: null,
    viewState: { quarter: '2026-Q3', tab: 'manage' },
  })
  assert.equal(pageHash('agency-okr', null, { quarter: '2026-Q3', tab: 'manage' }), '#/okr?quarter=2026-Q3&tab=manage')
})

test('maps a weekly share hash to the Agency OKR page and preserves its public path', () => {
  assert.deepEqual(routeFromHash('#/weekly-report?quarter=2026-Q3&week=2026-W36', 'overview'), {
    key: 'agency-okr',
    selection: null,
    viewState: { quarter: '2026-Q3', week: '2026-W36', share: 'weekly', tab: 'weekly-fill' },
  })
  assert.equal(
    pageHash('agency-okr', null, { quarter: '2026-Q3', week: '2026-W36', share: 'weekly', tab: 'weekly-fill' }),
    '#/weekly-report?quarter=2026-Q3&tab=weekly-fill&week=2026-W36',
  )
})
