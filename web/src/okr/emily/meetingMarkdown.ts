import { appPath } from '../../appPath.ts'
import { isDone, statusOf, weeklyScoreLabel } from './template.ts'
import { buildKRHierarchy, businessCategoryLabel, businessCategoryOf, priorityLabel, priorityOf } from './hierarchy.ts'
import type { DocLink, ImageRef, Kr, Objective, OKRPlan, WeekTemplateKey } from './types'

export interface MeetingMarkdownExport {
	title: string
	content: string
}

const inlineMarkdown = /([\\`*_[\]$~<>])/g

function escapeInline(value: string): string {
	return value.trim().replace(inlineMarkdown, '\\$1')
}

function escapeXML(value: string): string {
	return value.trim()
		.replaceAll('&', '&amp;')
		.replaceAll('<', '&lt;')
		.replaceAll('>', '&gt;')
		.replaceAll('\n', '<br/>')
}

function appendAssets(lines: string[], docs: DocLink[], images: ImageRef[]) {
	for (const doc of docs) {
		if (/^https?:\/\//i.test(doc.url)) lines.push(`  - [${escapeInline(doc.title || '链接')}](${doc.url})`)
	}
	for (const image of images) {
		const url = /^https?:\/\//i.test(image.url)
			? image.url
			: image.url.startsWith('/') && typeof window !== 'undefined'
				? `${window.location.origin}${appPath(image.url)}`
				: ''
		if (url) lines.push(`  - ![${escapeInline(image.name || '图片')}](${url})`)
	}
}

export function buildFullMeetingMarkdown(objectives: Objective[], quarter: string, week: string, templateKey: WeekTemplateKey = 'classic'): MeetingMarkdownExport {
	const preview = templateKey === 'okr_weekly_preview_v1'
	const title = preview ? `${week} OKR Review` : `${week} OKR 周报会议`
	const lines = [`# ${escapeInline(title)}`, '', `> ${escapeInline(quarter)} · ${escapeInline(week)}`, '']
	const lights = { green: '🟢', yellow: '🟡', red: '🔴' } as const

	for (const objective of objectives) {
		lines.push(`## ${escapeInline(objective.title)}`, '')
		for (const kr of objective.krs) {
			lines.push(`### ${escapeInline(kr.title)}`, '')
			const meta = [kr.ownerName && `负责人：${escapeInline(kr.ownerName)}`, `优先级：${priorityLabel(priorityOf(kr))}`, preview && `评分：${weeklyScoreLabel(kr.score)}`].filter(Boolean)
			if (meta.length > 0) lines.push(meta.join(' · '), '')
			if (kr.metrics.length > 0) {
				lines.push('#### 核心数据', '')
				if (kr.metricNote.trim()) lines.push(`> ${escapeInline(kr.metricNote)}`, '')
				for (const metric of kr.metrics) {
					lines.push(`- ${lights[metric.light ?? 'green']} ${escapeInline(metric.text)}`)
					appendAssets(lines, [], metric.images ?? [])
				}
				lines.push('')
			}

			for (const kind of ['strategy', 'product'] as const) {
				const points = kr.points.filter((point) => point.kind === kind)
				if (points.length === 0) continue
				lines.push(`#### ${kind === 'strategy' ? '策略具体 KR' : '产品具体 KR'}`, '')
				for (const [index, point] of points.entries()) {
					lines.push(`##### KR${index + 1} ${escapeInline(point.title)}`, '')
					if (preview) lines.push(`评分：${weeklyScoreLabel(point.score)}`, '')
					const groups = preview
						? [{ title: '本周进展', entries: point.entries }]
						: [
							{ title: '进展', entries: point.entries.filter((entry) => !isDone(entry.status)) },
							{ title: '已完成', entries: point.entries.filter((entry) => isDone(entry.status)) },
						]
					for (const group of groups) {
						if (group.entries.length === 0) continue
						lines.push(`**${group.title}**`, '')
						for (const entry of group.entries) {
							lines.push(`- **${statusOf(entry.status)?.label ?? entry.status}** ${escapeInline(entry.text)}`)
							appendAssets(lines, entry.docs, entry.images)
						}
						lines.push('')
					}
				}
			}
		}
	}

	return { title, content: `${lines.join('\n').trimEnd()}\n` }
}

interface PlanTableRow {
	objectiveTitle?: string
	objectiveRowspan?: number
	krTitle?: string
	krRowspan?: number
	priority?: string
	priorityRowspan?: number
	strategyTitle?: string
	productTitle?: string
}

function planPriorityLabel(kr: Kr): string {
	switch (priorityOf(kr)) {
		case 'p0': return 'Focus item'
		case 'p1': return 'P1'
		case 'p2': return 'P2'
		default: return '未标注'
	}
}

function planTableRows(objectives: Objective[]): PlanTableRow[] {
	const rows: PlanTableRow[] = []
	for (const objective of objectives) {
		if (objective.krs.length === 0) {
			rows.push({ objectiveTitle: objective.title, objectiveRowspan: 1, krTitle: '', krRowspan: 1, priority: '', priorityRowspan: 1, strategyTitle: '', productTitle: '' })
			continue
		}
		const objectiveRows = objective.krs.reduce((count, kr) => {
			const strategyCount = kr.points.filter((point) => point.kind === 'strategy').length
			const productCount = kr.points.filter((point) => point.kind === 'product').length
			return count + Math.max(strategyCount, productCount, 1)
		}, 0)
		let firstObjectiveRow = true
		for (const kr of objective.krs) {
			const strategies = kr.points.filter((point) => point.kind === 'strategy')
			const products = kr.points.filter((point) => point.kind === 'product')
			const krRows = Math.max(strategies.length, products.length, 1)
			for (let index = 0; index < krRows; index += 1) {
				rows.push({
					...(firstObjectiveRow ? { objectiveTitle: objective.title, objectiveRowspan: objectiveRows } : {}),
					...(index === 0 ? { krTitle: kr.title, krRowspan: krRows, priority: planPriorityLabel(kr), priorityRowspan: krRows } : {}),
					strategyTitle: strategies[index]?.title,
					productTitle: products[index]?.title,
				})
				firstObjectiveRow = false
			}
		}
	}
	return rows
}

function tableCell(value = '', rowspan = 1): string {
	const merge = rowspan > 1 ? ` rowspan="${rowspan}"` : ''
	return `<td${merge} vertical-align="top"><p>${escapeXML(value)}</p></td>`
}

// Plan export contains only the planning hierarchy requested by the product.
// Business category is presentation metadata used solely to split the tables.
export function buildPlanMarkdown(plan: Pick<OKRPlan, 'title' | 'quarter' | 'objectives'>): MeetingMarkdownExport {
	const title = plan.title.trim() || `${plan.quarter} OKR Plan`
	const lines = [`# ${escapeInline(title)}`, '', `> ${escapeInline(plan.quarter)}`, '']
	const navigation = buildKRHierarchy(plan.objectives)
	if (plan.objectives.some((objective) => objective.krs.length === 0) && !navigation.some((business) => business.value === '')) {
		navigation.push({ value: '', label: businessCategoryLabel(''), priorities: [] })
	}

	for (const business of navigation) {
		const objectives = plan.objectives.filter((objective) =>
			objective.krs.some((kr) => businessCategoryOf(kr) === business.value) ||
			(business.value === '' && objective.krs.length === 0),
		).map((objective) => ({
			...objective,
			krs: objective.krs.filter((kr) => businessCategoryOf(kr) === business.value),
		}))
		const rows = planTableRows(objectives)
		if (rows.length === 0) continue

		lines.push(`## ${escapeInline(business.label)}`, '')
		lines.push('<table>')
		lines.push('<thead><tr><th background-color="light-gray"><p>O</p></th><th background-color="light-gray"><p>KR</p></th><th background-color="light-gray"><p>优先级</p></th><th background-color="light-gray"><p>策略具体KR</p></th><th background-color="light-gray"><p>产品具体KR</p></th></tr></thead>')
		lines.push('<tbody>')
		for (const row of rows) {
			const cells = [
				row.objectiveTitle === undefined ? '' : tableCell(row.objectiveTitle, row.objectiveRowspan),
				row.krTitle === undefined ? '' : tableCell(row.krTitle, row.krRowspan),
				row.priority === undefined ? '' : tableCell(row.priority, row.priorityRowspan),
				tableCell(row.strategyTitle),
				tableCell(row.productTitle),
			].join('')
			lines.push(`<tr>${cells}</tr>`)
		}
		lines.push('</tbody>')
		lines.push('</table>', '')
	}

	return { title, content: `${lines.join('\n').trimEnd()}\n` }
}
