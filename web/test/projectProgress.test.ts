import assert from 'node:assert/strict'
import test from 'node:test'
import { hasProjectProgress, parseProjectProgress, serializeProjectProgress } from '../src/world/projectProgress.ts'

test('project progress round-trips the three weekly sections', () => {
  const draft = {
    focus: '完成灰度',
    progress: '流量已切换\n回测通过',
    next: '完成问题收口',
  }

  assert.deepEqual(parseProjectProgress(serializeProjectProgress(draft)), draft)
})

test('project progress ignores the legacy risk section and keeps loose context', () => {
  assert.deepEqual(parseProjectProgress(`背景说明

## 本周
完成联调

## 进展
已上线

## 风险
暂无

## 下周计划
开始回测`), {
    focus: '完成联调',
    progress: '背景说明\n\n已上线',
    next: '开始回测',
  })
})

test('project progress treats unstructured legacy text as current progress', () => {
  const draft = parseProjectProgress('旧版的一整段项目更新')
  assert.equal(draft.progress, '旧版的一整段项目更新')
  assert.equal(hasProjectProgress(draft), true)
  assert.equal(hasProjectProgress(parseProjectProgress('')), false)
})
