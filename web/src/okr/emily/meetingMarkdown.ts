import { appPath } from '../../appPath.ts'
import { isDone, statusOf, weeklyScoreLabel } from './template.ts'
import { priorityLabel, priorityOf } from './hierarchy.ts'
import type { DocLink, ImageRef, Objective, WeekTemplateKey } from './types'

export interface MeetingMarkdownExport {
	title: string
	content: string
}

const inlineMarkdown = /([\\`*_[\]$~<>])/g

function escapeInline(value: string): string {
	return value.trim().replace(inlineMarkdown, '\\$1')
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
