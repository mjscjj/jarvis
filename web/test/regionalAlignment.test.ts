import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { buildRegionalCommentDocumentOrder, copyRegionalAlignmentShareLink, launchRegionOptions, matchesRegionalPriority, regionalAlignmentShareURL, regionalDecisionSignature, REGIONAL_PRIORITY_FILTERS } from '../src/okr/emily/regionalAlignment.ts'
import { projectRegionalAlignment } from '../src/okr/emily/regionalAlignmentProjection.ts'
import type { Kr, RegionalAlignmentBoard, RegionalPlanDecisionItem } from '../src/okr/emily/types.ts'

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

test('Part 1 按关系、承接和上车状态逐 KR 投影', () => {
  const makeKr = (id: string, priority: 'p0' | 'p1'): Kr => ({ ...kr(priority), id, title: id })
  const board = {
    plan: { objectives: [{ id: 'o1', title: 'O1', version: 1, owners: [], krs: [makeKr('kr-1', 'p0'), makeKr('kr-2', 'p1'), makeKr('kr-3', 'p1'), makeKr('kr-4', 'p1')] }] },
    decisions: [{ planKrId: 'kr-1', onboard: 'yes' }, { planKrId: 'kr-2', onboard: 'no' }, { planKrId: 'kr-3', onboard: 'yes' }, { planKrId: 'kr-4', onboard: 'no' }],
    demands: [
      { id: 'd1', acceptance: 'yes', planKrIds: ['kr-1'] },
      { id: 'd2', acceptance: 'yes', planKrIds: ['kr-2'] },
      { id: 'd3', acceptance: 'no', planKrIds: ['kr-3'] },
      { id: 'd4', acceptance: 'yes', planKrIds: [] },
      { id: 'd5', acceptance: 'no', planKrIds: [] },
    ],
  } as unknown as RegionalAlignmentBoard
  const projection = projectRegionalAlignment(board)
  assert.deepEqual(projection.aligned.map((item) => [item.kr.id, item.demands.map((demand) => demand.id)]), [['kr-1', ['d1']]])
  assert.deepEqual(projection.pendingDemands.map((item) => item.demand.id), ['d2', 'd3', 'd4', 'd5'])
  assert.deepEqual(projection.pendingPlatform.map((item) => [item.kr.id, item.reason]), [['kr-2', 'region_not_onboard'], ['kr-3', 'unmatched'], ['kr-4', 'region_not_onboard']])
})

test('区域评论按完整页面语义排序，不受当前筛选影响', () => {
  const objective = { id: 'o-1', title: 'O1', krs: [{ ...kr('p0'), id: 'kr-1', points: [{ id: 'point-1', kind: 'product' as const, title: 'P1', owners: [], entries: [{ id: 'entry-1', status: 'in_progress' as const, text: '进展', docs: [], images: [] }] }] }] }
  const board = {
    alignment: { id: 'alignment-1', quarter: '2026-Q4', planId: 'plan-1', recapQuarter: '2026-Q3', version: 1 },
    region: { regionCode: 'eu', version: 1, categoryOrder: [] },
    plan: { id: 'plan-1', quarter: '2026-Q4', title: 'Plan', objectives: [objective] },
    recap: { quarter: '2026-Q3', objectives: [] },
    demands: [{ id: 'demand-1', version: 1, regionalOkr: '', item: '', requirement: '需求', docs: [], images: [], priority: 'p0', regionalPocs: [], platformPocs: [], acceptance: 'tbd', planKrIds: [], deliverable: '', sortOrder: 0 }],
    decisions: [], recapOverlays: [], translations: {},
  } as RegionalAlignmentBoard
  const order = buildRegionalCommentDocumentOrder(board)
  assert.ok(order.get('alignment_item:d:demand-1:requirement')! < order.get('objective:o-1')!)
  assert.ok(order.get('kr:kr-1')! < order.get('alignment_item:pd:kr-1:onboard')!)
  assert.ok(order.get('point:point-1')! < order.get('entry:entry-1')!)
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
  assert.equal([...source.matchAll(/公会业务 \/ Agency/g)].length, 2)
  assert.match(source, /Regional Ops Team 高优痛点&核心需求/)
  assert.match(source, /新增需求 \/ Add requirement/)
  assert.match(source, /onClick=\{addDemandDraft\}/)
  assert.match(source, /setDemandDrafts\(\(current\) => \[\.\.\.current/)
  assert.match(demand, /drafts\.map\(\(draft\) => <DemandEditor/)
  assert.match(demand, /isNew && !demandHasContent\(value\)/)
  assert.match(source, /flex cursor-pointer list-none items-start gap-2/)
  assert.match(source, /refreshRegionalAlignmentBoard/)
  assert.match(source, /startRegionalAutoMatch\(quarter, region\)/)
  assert.match(source, /autoMatchStartingRef\.current/)
  assert.match(source, /disabled=\{loading \|\| busy \|\| Object\.keys\(part0Pending\)\.length > 0 \|\| autoMatchStarting \|\| Boolean\(autoMatchTask\)\}/)
  assert.match(source, /getTask\(autoMatchTask\.id, controller\.signal\)/)
  assert.match(source, /projectRegionalAlignment\(board\)/)
  assert.match(source, /key=\{objective\.alignmentCardKey\}/)
  assert.doesNotMatch(source, /load\(\)\.then\(\(\) => setMatchNotice/)
  assert.match(source, /const copyShare = async \(\) => \{[\s\S]{0,240}setError\(''\)/)
  assert.match(source, /\{error && <div role="alert"/)
  assert.match(source, /\{\(shareNotice \|\| shareLink\) && <div role="status"/)
  assert.doesNotMatch(source, /error \|\| shareNotice/)
  assert.match(source, /setShareLink\(link\)[\s\S]*copyRegionalAlignmentShareLink\(link, navigator\.clipboard\)/)
  assert.match(source, /aria-label="分享链接 \/ Share link"/)
  assert.equal([...source.matchAll(/'刷新 \/ Refresh'/g)].length, 2)
  assert.equal([...source.matchAll(/<PriorityTabs /g)].length, 2)
  assert.match(source, /triggerMode: 'surface'/)
  assert.match(source, /pendingSelection: pendingCommentSelection/)
  assert.match(source, /<CommentableText /)
  assert.match(source, /<CommentableTextarea /)
  assert.match(source, /<CommentableField /)
  assert.match(source, /targetOrder=\{commentDocumentOrder\}/)

  const commenting = readFileSync(new URL('../src/okr/emily/commenting.tsx', import.meta.url), 'utf8')
  assert.match(commenting, /createPortal/)
  assert.match(commenting, /data-comment-target-key/)
  assert.match(commenting, /data-comment-selection-id/)
  assert.match(commenting, /resolveCommentSelection/)

  const api = readFileSync(new URL('../src/okr/emily/api.ts', import.meta.url), 'utf8')
  assert.match(api, /const owners = \(items: Array<\{ email: string; name: string \}> \| null \| undefined\) => \(items \?\? \[\]\)\.map/)
  assert.match(api, /\/refresh-status\?/)
  assert.match(api, /result\.pending/)
})
