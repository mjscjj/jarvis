export function planOptionLabel(title: string, quarter: string): string {
  const cleanTitle = title.trim()
  const cleanQuarter = quarter.trim().toUpperCase()
  const matched = /^(\d{4})[\s-]?Q([1-4])(?=$|[\s·:：/_-])[\s·:：/_-]*/i.exec(cleanTitle)
  if (!matched || `${matched[1]}-Q${matched[2]}` !== cleanQuarter) return cleanTitle
  return cleanTitle.slice(matched[0].length).trim() || 'Plan'
}
