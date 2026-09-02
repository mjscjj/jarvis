// 上移/下移交换的是**当前看得见的**上下两行，所以调用方给出目标行的 id，
// 这里把它放回整份兄弟顺序里。筛选藏起来的行留在原位，不跟着挪。
export function swappedOrder(ids: string[], id: string, targetId: string): string[] {
  const from = ids.indexOf(id)
  const to = ids.indexOf(targetId)
  if (from < 0 || to < 0) throw new Error('要调换的行已不在当前顺序里，请重新载入。')
  const next = [...ids]
  next[from] = targetId
  next[to] = id
  return next
}
