export function imageFilesFromClipboard<T extends { type: string }>(files: ArrayLike<T>): T[] {
  return Array.from(files).filter((file) => file.type.startsWith('image/'))
}
