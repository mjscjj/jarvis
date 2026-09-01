import assert from 'node:assert/strict'
import test from 'node:test'

import { OKR_PREVIEW_REVIEW_PROMPT_KEY, previewReviewContent, previewReviewTaskInput } from '../src/okr/emily/aiReview.ts'
import type { Task } from '../src/types.ts'

test('Preview point review preserves the exact target scope for the Agent', () => {
  const input = previewReviewTaskInput('2026-Q3', '2026-W37', {
    kind: 'point',
    objectiveId: 'o-1',
    krId: 'kr-1',
    pointId: 'point-1',
    title: '提升结算效率',
  })
  assert.equal(input.action_type, 'agent_task')
  assert.deepEqual(input.background, {
    module: 'weekly-report',
    skill: 'okr-agent-orchestrator',
    action_key: 'preview_review',
    prompt_key: OKR_PREVIEW_REVIEW_PROMPT_KEY,
    review_scope: {
      quarter: '2026-Q3',
      week: '2026-W37',
      kind: 'point',
      objectiveId: 'o-1',
      krId: 'kr-1',
      pointId: 'point-1',
      title: '提升结算效率',
    },
  })
})

test('Preview full review uses one all-scope Task instead of inventing per-item workflow state', () => {
  const input = previewReviewTaskInput('2026-Q3', '2026-W37', { kind: 'all', title: '全部 OKR' })
  assert.match(input.title, /全部/)
  assert.deepEqual((input.background.review_scope as Record<string, unknown>), {
    quarter: '2026-Q3',
    week: '2026-W37',
    kind: 'all',
    title: '全部 OKR',
  })
})

test('Preview review renders the full open semantic report instead of only the short Task summary', () => {
	const task = {
		summary: '简短结论',
		execution_result: {
			enrichments: [{ kind: 'context', label: 'OKR Preview 评审报告', content: '## 详细评审\n\n需要补充业务结果。' }],
		},
	} as Task
	assert.equal(previewReviewContent(task), '## 详细评审\n\n需要补充业务结果。')
})
