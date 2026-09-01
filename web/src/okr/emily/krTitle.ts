const NUMBERED_KR_PREFIX = /^\s*kr\s*(\d+)\s*[:：]\s*/i

/**
 * KR 序号属于标题的展示格式。统一中英文冒号，避免手动创建与导入
 * 数据在同一列表里呈现为两种标题样式。
 */
export function normalizeKRTitle(value: string): string {
	const title = value.trim()
	return title.replace(NUMBERED_KR_PREFIX, (_match, ordinal: string) => `KR${ordinal} `)
}
