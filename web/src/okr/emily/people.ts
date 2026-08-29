export function splitOwnerNames(value?: string) {
  return (value ?? '').split(/[、,，;；]/).map((item) => item.trim()).filter(Boolean)
}

export function joinOwnerNames(values: string[]) {
  return [...new Set(values.map((item) => item.trim()).filter(Boolean))].join('、')
}

export function hasOwner(value: string | undefined, owner: string) {
  return splitOwnerNames(value).includes(owner)
}
