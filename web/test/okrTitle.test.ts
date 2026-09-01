import assert from 'node:assert/strict'
import test from 'node:test'

import { normalizeKRTitle } from '../src/okr/emily/krTitle.ts'

test('normalizes manually entered numbered KR titles to the imported display style', () => {
	assert.equal(normalizeKRTitle('KR1：Reach Plaza 触达能力提升'), 'KR1 Reach Plaza 触达能力提升')
	assert.equal(normalizeKRTitle('KR2: Contact center'), 'KR2 Contact center')
})

test('preserves already-normalized and unnumbered KR titles', () => {
	assert.equal(normalizeKRTitle('KR1 公会 AI 工具'), 'KR1 公会 AI 工具')
	assert.equal(normalizeKRTitle('提升触达能力'), '提升触达能力')
})
