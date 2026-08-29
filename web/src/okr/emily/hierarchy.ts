import type { Objective } from './types'

export interface DirectionSlot {
  label: string
  matches: string[]
}

export interface BusinessSubgroup {
  id: string
  label: string
  directions: DirectionSlot[]
}

export interface BusinessDefinition {
  id: string
  label: string
  subgroups: BusinessSubgroup[]
}

export interface NavigationSubgroup extends BusinessSubgroup {
  objectives: Objective[]
  missing: DirectionSlot[]
}

export interface BusinessNavigation extends Omit<BusinessDefinition, 'subgroups'> {
  subgroups: NavigationSubgroup[]
}

// 这是展示层的固定业务目录，只负责把周会原文中的 O 归到三级导航里；
// KR、标签和后端模型仍然完全沿用 emily 本项目的数据。
export const BUSINESS_DEFINITIONS: BusinessDefinition[] = [
  {
    id: 'guild', label: '1. 公会业务', subgroups: [
      { id: 'guild-focus', label: '1.1 Focus', directions: [
        { label: 'O1：B端市场', matches: ['b端市场'] },
        { label: 'O2：平台政策', matches: ['平台政策'] },
        { label: 'O3：平台活动', matches: ['平台活动'] },
        { label: 'O：公会移动端', matches: ['公会移动端'] },
      ] },
      { id: 'guild-p1', label: '1.2 P1', directions: [
        { label: 'O1：公会基建', matches: ['公会基建'] },
        { label: 'O2：公会教育', matches: ['公会教育'] },
        { label: 'O3：公会治理', matches: ['公会治理'] },
      ] },
    ],
  },
  {
    id: 'operations', label: '2. 运营效率', subgroups: [
      { id: 'operations-focus', label: '2.1 Focus', directions: [
        { label: 'O1：分层运营工作台', matches: ['分层运营工作台'] },
        { label: 'O2：供给增长：流量激励工具扩量', matches: ['流量激励工具扩量'] },
        { label: 'O3：供给增长：触达效率提升', matches: ['触达效率提升'] },
        { label: 'O4：供给增长：Match plaza', matches: ['matchplaza'] },
        { label: 'O5：服务体验提升：智能客服', matches: ['智能客服'] },
        { label: 'O6：运营教育', matches: ['运营教育'] },
        { label: 'O7：GTM', matches: ['gtm'] },
      ] },
      { id: 'operations-p1', label: '2.2 P1', directions: [
        { label: 'O1：主播管理基础工具', matches: ['主播管理基础工具'] },
        { label: 'O2：主播教育基础工具', matches: ['主播教育基础工具', '主播教育基础工作'] },
        { label: 'O3：数据平台', matches: ['数据平台'] },
      ] },
    ],
  },
  {
    id: 'ai', label: '3. AI提效', subgroups: [
      { id: 'ai-all', label: '全部方向', directions: [
        { label: 'O1：Backstage AI助手（Bax）', matches: ['backstageai助手', 'bax完成基础能力建设'] },
        { label: 'O2：对内 Bax AI 助手', matches: ['对内baxai助手'] },
        { label: 'O3：Arena AI助手（Rena）', matches: ['arenaai助手', 'rena建设'] },
      ] },
    ],
  },
  {
    id: 'quality-content', label: '4. 优质主播 & 内容专项', subgroups: [
      { id: 'quality-content-all', label: '全部方向', directions: [
        { label: 'O1：优质主播计划', matches: ['优质主播计划'] },
        { label: 'O2：公会项目透传与调优', matches: ['面向公会做好项目透传'] },
        { label: 'O3：基础能力及 To C 工具', matches: ['基础能力建设及toc工具'] },
      ] },
    ],
  },
  {
    id: 'algorithm', label: '5. 算法进展', subgroups: [
      { id: 'algorithm-all', label: '全部方向', directions: [
        { label: 'O1：Improve Operation Efficiency', matches: ['improveoperationefficiency'] },
        { label: 'O2：Scale TCN Business', matches: ['scaletcnbusiness'] },
      ] },
    ],
  },
]

function normalizedTitle(value: string) {
  return value.toLowerCase().replace(/[：:\s&（）()_-]/g, '')
}

function matchesDirection(objective: Objective, slot: DirectionSlot) {
  const title = normalizedTitle(objective.title)
  return slot.matches.some((match) => title.includes(normalizedTitle(match)))
}

export function buildBusinessNavigation(objectives: Objective[]): BusinessNavigation[] {
  const claimed = new Set<string>()
  const groups: BusinessNavigation[] = BUSINESS_DEFINITIONS.map((business) => ({
    ...business,
    subgroups: business.subgroups.map((subgroup) => {
      const matched: Objective[] = []
      const missing: DirectionSlot[] = []
      for (const slot of subgroup.directions) {
        const found = objectives.find((objective) => !claimed.has(objective.id) && matchesDirection(objective, slot))
        if (!found) missing.push(slot)
        else {
          claimed.add(found.id)
          matched.push(found)
        }
      }
      return { ...subgroup, objectives: matched, missing }
    }),
  }))

  const other = objectives.filter((objective) => !claimed.has(objective.id))
  if (other.length > 0) {
    groups.push({
      id: 'other',
      label: '其他 / 未分类',
      subgroups: [{ id: 'other-all', label: '全部方向', directions: [], objectives: other, missing: [] }],
    })
  }
  return groups
}

export function krCount(objectives: Objective[]) {
  return objectives.reduce((sum, objective) => sum + objective.krs.length, 0)
}

export function businessKrCount(business: BusinessNavigation) {
  return krCount(business.subgroups.flatMap((subgroup) => subgroup.objectives))
}

export function subgroupTone(label: string): 'focus' | 'p1' | 'standard' {
  const value = label.toLowerCase()
  if (value.includes('focus')) return 'focus'
  if (value.includes('p1')) return 'p1'
  return 'standard'
}
