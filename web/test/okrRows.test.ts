import assert from 'node:assert/strict'
import test from 'node:test'
import { buildRows, collapseAllIds } from '../src/okr/emily/rows.ts'
import type { Objective } from '../src/okr/emily/types.ts'

const objectives: Objective[] = [{
  id: 'o-1',
  title: '方向一',
  krs: [{
    id: 'kr-1',
    title: '一级 KR',
    metricNote: '',
    metrics: [],
    points: [{
      id: 'point-1',
      kind: 'strategy',
      title: '具体 KR',
      entries: [],
    }],
  }],
}]

test('全部折叠保留 O 展开，并显示到一级 KR', () => {
  const closed = collapseAllIds(objectives)

  assert.deepEqual([...closed], ['kr-1'])
  assert.deepEqual(buildRows(objectives, closed).map((row) => row.type), ['objective', 'kr'])
})
