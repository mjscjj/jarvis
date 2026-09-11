import assert from 'node:assert/strict'
import test from 'node:test'

import { downloadPercent, formatBytes, updateStatusLine } from '../src/updateStatus.ts'
import type { UpdateStatus } from '../src/updateStatus.ts'

test('download percent stays inside 0..100', () => {
  assert.equal(downloadPercent(0, 0), 0)
  assert.equal(downloadPercent(500, 0), 0)
  assert.equal(downloadPercent(500, -1), 0)
  assert.equal(downloadPercent(0, 1000), 0)
  assert.equal(downloadPercent(333, 1000), 33)
  assert.equal(downloadPercent(1000, 1000), 100)
  assert.equal(downloadPercent(5000, 1000), 100)
})

test('formats downloaded bytes as megabytes', () => {
  assert.equal(formatBytes(0), '0.0 MB')
  assert.equal(formatBytes(1024 * 1024), '1.0 MB')
  assert.equal(formatBytes(12 * 1024 * 1024 + 512 * 1024), '12.5 MB')
})

test('every status maps to its own line', () => {
  assert.equal(updateStatusLine('idle', '0.2.0'), null)
  assert.deepEqual(updateStatusLine('checking', ''), { tone: 'text', title: '正在检查更新…' })
  assert.deepEqual(updateStatusLine('latest', ''), { tone: 'success', title: '已是最新版本' })
  assert.deepEqual(updateStatusLine('available', '0.2.0'), { tone: 'info', title: '发现新版本 0.2.0' })
  assert.equal(updateStatusLine('check-failed', '')?.tone, 'error')
  assert.equal(updateStatusLine('check-failed', '')?.title, '检查更新失败')
  assert.equal(updateStatusLine('installing', '0.2.0')?.tone, 'text')
  assert.equal(updateStatusLine('install-failed', '0.2.0')?.tone, 'error')
  assert.equal(updateStatusLine('install-failed', '0.2.0')?.title, '升级失败')
})

test('a failed check and a failed install never share one line', () => {
  const checkFailed = updateStatusLine('check-failed', '')
  const installFailed = updateStatusLine('install-failed', '0.2.0')
  assert.notEqual(checkFailed?.title, installFailed?.title)
})

test('only the statuses that carry information produce a line', () => {
  const statuses: UpdateStatus[] = ['idle', 'checking', 'latest', 'available', 'check-failed', 'installing', 'install-failed']
  const lines = statuses.map((status) => updateStatusLine(status, '0.2.0'))
  assert.equal(lines.filter((line) => line === null).length, 1)
  assert.equal(new Set(lines.map((line) => line?.title)).size, statuses.length)
})
