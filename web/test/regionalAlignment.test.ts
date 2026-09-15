import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { copyRegionalAlignmentShareLink, launchRegionOptions, matchesRegionalPriority, regionalAlignmentShareURL, regionalDecisionSignature, REGIONAL_PRIORITY_FILTERS } from '../src/okr/emily/regionalAlignment.ts'
import type { Kr, RegionalPlanDecisionItem } from '../src/okr/emily/types.ts'

function kr(priority: 'p0' | 'p1' | 'p2' | ''): Kr {
  return {
    id: `kr-${priority || 'unset'}`,
    title: 'KR',
    metricNote: '',
    metrics: [],
    points: [],
    tags: priority ? [{ type: 'priority', value: priority }] : [],
  }
}

test('平台项目和上季度复盘共用完整的四档优先级筛选', () => {
  assert.deepEqual(REGIONAL_PRIORITY_FILTERS.map((item) => item.value), ['all', 'p0', 'p1', 'p2'])
  assert.equal(REGIONAL_PRIORITY_FILTERS[0].label, 'Focus item/P1/P2')
  assert.equal(matchesRegionalPriority(kr('p0'), 'all'), true)
  assert.equal(matchesRegionalPriority(kr('p0'), 'p0'), true)
  assert.equal(matchesRegionalPriority(kr('p1'), 'p0'), false)
  assert.equal(matchesRegionalPriority(kr('p2'), 'p2'), true)
  assert.equal(matchesRegionalPriority(kr(''), 'all'), true)
})

test('MENAT 上车地区提供 MENAT 选项并保留其它区域的建议值', () => {
  assert.deepEqual(launchRegionOptions('menat'), ['MENAT', 'MENA', 'TR'])
  assert.ok(launchRegionOptions('eu').includes('EU'))
  assert.ok(launchRegionOptions('sea-cca').includes('SEA&CCA'))
})

test('自动保存只比较可编辑内容，不把服务端版本递增当成再次修改', () => {
  const value: RegionalPlanDecisionItem = { planKrId: 'kr-1', version: 1, onboard: 'yes', launchRegions: ['MENAT'], regionalPocs: [{ name: 'Owner', email: 'owner@example.com' }], regionalOkr: 'O1', hidden: false }
  assert.equal(regionalDecisionSignature(value), regionalDecisionSignature({ ...value, version: 2 }))
  assert.notEqual(regionalDecisionSignature(value), regionalDecisionSignature({ ...value, launchRegions: ['MENAT', 'TR'] }))
})

test('区域对齐分享链接保留公开部署路径和当前季度、区域', () => {
  assert.equal(
    regionalAlignmentShareURL(
      'http://127.0.0.1:5173/#/biz-okr?tab=regional-alignment',
      'https://example.com/jarvis/',
      '2026-Q4',
      'sea-cca',
    ),
    'https://example.com/jarvis/#/biz-okr?tab=regional-alignment&quarter=2026-Q4&region=sea-cca',
  )
})

test('区域对齐分享操作把页面展示的同一链接写入剪贴板', async () => {
  const writes: string[] = []
  const link = 'https://example.com/jarvis/#/biz-okr?tab=regional-alignment&quarter=2026-Q4&region=eu'
  assert.equal(await copyRegionalAlignmentShareLink(link, { writeText: async (value) => { writes.push(value) } }), true)
  assert.deepEqual(writes, [link])
  assert.equal(await copyRegionalAlignmentShareLink(link), false)
})

test('区域对齐页面保留直接表格、自动保存、双语和刷新交互闭包', () => {
  const source = readFileSync(new URL('../src/okr/emily/RegionalAlignmentApp.tsx', import.meta.url), 'utf8')
  const demand = source.slice(source.indexOf('function DemandEditor'), source.indexOf('function DecisionEditor'))
  const decision = source.slice(source.indexOf('function DecisionEditor'), source.indexOf('function PlatformSection'))

  assert.equal([...demand.matchAll(/role="table"/g)].length, 1)
  assert.match(demand, /zh="优先级"/)
  assert.match(demand, /zh="区域负责人"/)
  assert.match(demand, /zh="是否承接"/)
  assert.match(demand, /zh="平台负责人"/)
  assert.doesNotMatch(demand, /zh="事项"/)
  assert.match(demand, /onPaste=\{pasteRequirement\}/)
  assert.match(demand, /pasteEnabled=\{false\}/)
  assert.match(demand, /maxDisplayWidth=\{120\}/)
  assert.equal([...demand.matchAll(/!bg-sky-50/g)].length, 3)
  assert.equal([...demand.matchAll(/!bg-sky-100/g)].length, 3)
  assert.match(demand, /区域 OKR、具体需求、优先级、区域负责人列由区域运营填写/)
  assert.match(demand, /Regional Operations fills in Regional OKR, Detailed Requirement, Priority, and Regional POC/)
  assert.doesNotMatch(demand, /xl:grid-cols-\[minmax\(0,5fr\)/)
  assert.match(decision, /bg-amber-50/)
  assert.match(decision, /grid items-end/)
  assert.match(decision, /mode="tags"/)
  assert.doesNotMatch(decision, /自动保存 \/ Auto-save/)
  assert.doesNotMatch(decision, />保存<\/button>/)
  assert.doesNotMatch(demand, /新增区域需求 \/ Add requirement/)
  assert.doesNotMatch(demand, /取消 \/ Cancel/)
  assert.doesNotMatch(demand, /创建需求 \/ Create/)
  assert.match(demand, /停止输入后自动保存/)
  assert.match(source, /全部业务方向 \/ All business directions/)
  assert.match(source, /setTimeout\(\(\) => setSavedNotice\(''\), 3000\)/)
  assert.match(source, /function OKRText/)
  assert.match(source, /<OKRText text=\{objective\.title\}/)
  assert.match(source, /<OKRText text=\{kr\.title\}/)
  assert.match(source, /<OKRText text=\{point\.title\}/)
  assert.match(source, /function CollapsibleBlock/)
  assert.equal([...source.matchAll(/level="primary"/g)].length, 3)
  assert.equal([...source.matchAll(/level="secondary"/g)].length, 2)
  assert.match(source, /Regional Ops Team 高优痛点&核心需求/)
  assert.match(source, /新增需求 \/ Add requirement/)
  assert.match(source, /onClick=\{addDemandDraft\}/)
  assert.match(source, /setDemandDrafts\(\(current\) => \[\.\.\.current/)
  assert.match(demand, /drafts\.map\(\(draft\) => <DemandEditor/)
  assert.match(demand, /isNew && !demandHasContent\(value\)/)
  assert.match(source, /flex cursor-pointer list-none items-start gap-2/)
  assert.match(source, /refreshRegionalAlignmentBoard/)
  assert.match(source, /setShareLink\(link\)[\s\S]*copyRegionalAlignmentShareLink\(link, navigator\.clipboard\)/)
  assert.match(source, /aria-label="分享链接 \/ Share link"/)
  assert.equal([...source.matchAll(/'刷新 \/ Refresh'/g)].length, 2)
  assert.equal([...source.matchAll(/<PriorityTabs /g)].length, 2)
})
