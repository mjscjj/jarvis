import assert from 'node:assert/strict'
import test from 'node:test'

import {
  formatMonitoringCount,
  formatMonitoringDuration,
  formatMonitoringRate,
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
  assert.equal(formatMonitoringRate(0.034), '3.4%')
  const now = new Date(2026, 7, 7, 12, 34, 56)
  const day = monitoringRangeBounds('today', now)
  assert.deepEqual([day.from.getHours(), day.from.getMinutes(), day.from.getDate()], [0, 0, 7])
  const rolling = monitoringRangeBounds('24h', now)
  assert.deepEqual([rolling.from.getHours(), rolling.from.getMinutes(), rolling.from.getDate()], [12, 0, 6])
  const week = monitoringRangeBounds('7d', now)
  assert.deepEqual([week.from.getHours(), week.from.getMinutes(), week.from.getDate()], [0, 0, 1])
})
