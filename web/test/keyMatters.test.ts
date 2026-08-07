import assert from 'node:assert/strict'
import test from 'node:test'
import { keyMatterToInput, replaceKeyMatter } from '../src/keyMatters.ts'
import type { KeyMatter } from '../src/types.ts'

function matter(id: number): KeyMatter {
  return {
    id,
    title: `事项 ${id}`,
    status: '等待回复',
    summary: '已完成第一轮对齐',
    project_id: 3,
    due_at: '2026-08-08T10:00:00Z',
    closed_at: null,
    last_progress_at: '2026-08-06T08:00:00Z',
    last_active_at: '2026-08-06T09:00:00Z',
    created_at: '2026-08-01T08:00:00Z',
    updated_at: '2026-08-06T08:00:00Z',
    project: null,
  }
}

test('inline key matter edits preserve the complete editable payload', () => {
  const current = matter(7)
  assert.deepEqual(keyMatterToInput(current, { status: '本周收口' }), {
    title: '事项 7',
    status: '本周收口',
    summary: '已完成第一轮对齐',
    project_id: 3,
    due_at: '2026-08-08T10:00:00Z',
  })
  assert.equal(keyMatterToInput(current, { summary: null }).summary, null)
  assert.equal(keyMatterToInput(current, { due_at: null }).due_at, null)
})

test('inline save replaces only the persisted list row', () => {
  const first = matter(1)
  const second = matter(2)
  const saved = { ...second, status: '已拿到回复' }
  const result = replaceKeyMatter([first, second], saved)
  assert.equal(result[0], first)
  assert.equal(result[1], saved)
})
