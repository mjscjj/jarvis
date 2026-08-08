import assert from 'node:assert/strict'
import test from 'node:test'
import { personToUpdateInput } from '../src/persons.ts'
import type { Person } from '../src/types.ts'

test('person updates contain editable fields only', () => {
  const person: Person = {
    id: 112,
    open_id: 'ou_immutable',
    union_id: 'on_immutable',
    feishu_user_id: 'user_immutable',
    name: '李阳',
    en_name: 'Li Yang',
    avatar_url: 'https://example.com/avatar.png',
    department: '研发',
    title: '工程师',
    role: 'colleague',
    priority_weight: 0.4,
    relation: '同事',
    comm_style: '结论先行',
    p2p_chat_id: 'oc_immutable',
    notes: '备注',
    is_active: true,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-08T00:00:00Z',
  }

  const input = personToUpdateInput(person)
  assert.deepEqual(input, {
    name: '李阳',
    role: 'colleague',
    priority_weight: 0.4,
    department: '研发',
    title: '工程师',
    relation: '同事',
    comm_style: '结论先行',
    notes: '备注',
    is_active: true,
  })
  assert.equal('open_id' in input, false)
  assert.equal('p2p_chat_id' in input, false)
})
