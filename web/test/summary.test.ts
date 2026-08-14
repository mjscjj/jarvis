import assert from 'node:assert/strict'
import test from 'node:test'
import { countChars, summaryIndexLine, summaryMeterTone } from '../src/world/summary.ts'

test('countChars counts unicode scalars, not UTF-16 units', () => {
  assert.equal(countChars(''), 0)
  assert.equal(countChars('abc'), 3)
  assert.equal(countChars('公会'), 2)
  assert.equal(countChars('🙂'), 1)
})

test('summaryIndexLine keeps the first line only', () => {
  assert.equal(summaryIndexLine(null), '')
  assert.equal(summaryIndexLine('这是公会 Agent 基建。\n当前在做灰度。'), '这是公会 Agent 基建。')
})

test('summaryMeterTone warns near the ceiling and flags overflow', () => {
  assert.equal(summaryMeterTone(100, 8000), 'ok')
  assert.equal(summaryMeterTone(6000, 8000), 'warn')
  assert.equal(summaryMeterTone(7200, 8000), 'danger')
  assert.equal(summaryMeterTone(8001, 8000), 'danger')
})
