import assert from 'node:assert/strict'
import test from 'node:test'
import { buildBusinessNavigation, businessKrCount } from '../src/okr/emily/hierarchy.ts'
import type { Objective } from '../src/okr/emily/types.ts'

function objective(id: string, title: string, krCount: number): Objective {
  return {
    id,
    title,
    krs: Array.from({ length: krCount }, (_, index) => ({
      id: `${id}-kr-${index}`,
      title: `KR ${index + 1}`,
      metricNote: '',
      metrics: [],
      points: [],
    })),
  }
}

test('maps known objectives into the meeting hierarchy and keeps unknown objectives visible', () => {
  const navigation = buildBusinessNavigation([
    objective('guild', 'O1：B端市场增长', 2),
    objective('ai', 'O3：Rena 建设', 1),
    objective('other', '临时跨团队方向', 3),
  ])

  const guild = navigation.find((business) => business.id === 'guild')
  assert.deepEqual(guild?.subgroups[0].objectives.map((item) => item.id), ['guild'])
  assert.equal(businessKrCount(guild!), 2)

  const ai = navigation.find((business) => business.id === 'ai')
  assert.deepEqual(ai?.subgroups[0].objectives.map((item) => item.id), ['ai'])

  const other = navigation.find((business) => business.id === 'other')
  assert.deepEqual(other?.subgroups[0].objectives.map((item) => item.id), ['other'])
  assert.equal(businessKrCount(other!), 3)
})
