import assert from 'node:assert/strict'
import test from 'node:test'

import {
  formatMonitoringCount,
  formatMonitoringDuration,
  monitoringRangeBounds,
} from '../src/debug/monitoringPresentation.ts'

test('monitoring duration stays readable across scales', () => {
  assert.equal(formatMonitoringDuration(null), '—')
  assert.equal(formatMonitoringDuration(420), '420 ms')
  assert.equal(formatMonitoringDuration(1_250), '1.3 秒')
  assert.equal(formatMonitoringDuration(75_000), '1 分 15 秒')
  assert.equal(formatMonitoringDuration(3_720_000), '1 小时 2 分')
})

test('monitoring count and rolling ranges use coarse exact windows', () => {
  assert.equal(formatMonitoringCount(12_345), '12,345')
  const now = new Date('2026-08-07T12:00:00.000Z')
  assert.equal(monitoringRangeBounds('24h', now).from.toISOString(), '2026-08-06T12:00:00.000Z')
  assert.equal(monitoringRangeBounds('7d', now).from.toISOString(), '2026-07-31T12:00:00.000Z')
})
