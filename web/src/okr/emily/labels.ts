export const TAG_TYPE_LABEL: Record<string, string> = {
  management_focus: '管理重点',
  biweekly: '双周会',
  platform_report: '中台周报',
  region: '区域',
  topic: '主题',
  custom: '自定义',
}

export const TAG_VALUE_LABEL: Record<string, string> = {
  guild_conference: '公会大会',
}

export function tagLabel(type: string, value: string) {
  const typeLabel = TAG_TYPE_LABEL[type]
  const valueLabel = TAG_VALUE_LABEL[value] ?? value
  return typeLabel ? `${typeLabel} · ${valueLabel}` : valueLabel
}
