import { isDone, statusOf } from './template'
import type { DocLink, ImageRef, Objective } from './types'

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
				? `${window.location.origin}${image.url}`
				: ''
		if (url) lines.push(`  - ![${escapeInline(image.name || '图片')}](${url})`)
	}
}

export function buildMeetingMarkdown(objectives: Objective[], quarter: string, week: string): MeetingMarkdownExport {
	const title = `${week} OKR 周报会议`
	const lines = [`# ${escapeInline(title)}`, '', `> ${escapeInline(quarter)} · ${escapeInline(week)}`, '']
	const lights = { green: '🟢', yellow: '🟡', red: '🔴' } as const

	for (const objective of objectives) {
		lines.push(`## ${escapeInline(objective.title)}`, '')
		for (const kr of objective.krs) {
			lines.push(`### ${escapeInline(kr.title)}`, '')
			const meta = [kr.ownerName && `负责人：${escapeInline(kr.ownerName)}`, kr.priority && `优先级：${kr.priority.toUpperCase()}`].filter(Boolean)
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
					for (const group of [
						{ title: '进展', entries: point.entries.filter((entry) => !isDone(entry.status)) },
						{ title: '已完成', entries: point.entries.filter((entry) => isDone(entry.status)) },
					]) {
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
