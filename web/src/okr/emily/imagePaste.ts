export function imageFilesFromClipboard<T extends { type: string }>(files: ArrayLike<T>): T[] {
  return Array.from(files).filter((file) => file.type.startsWith('image/'))
}

export const MAX_IMAGE_UPLOAD_BYTES = 10 * 1024 * 1024
export const IMAGE_COMPRESSION_THRESHOLD_BYTES = 3 * 1024 * 1024

const IMAGE_MAX_DIMENSION = 3200
const IMAGE_WEBP_QUALITY = 0.82

type ImageCompressor = (file: File) => Promise<File>

export async function prepareImageForUpload(
  file: File,
  compress: ImageCompressor = compressImageForUpload,
): Promise<File> {
  let prepared = file
  const shouldCompress = file.size > IMAGE_COMPRESSION_THRESHOLD_BYTES && file.type !== 'image/gif'
  if (shouldCompress) {
    try {
      const compressed = await compress(file)
      if (compressed.size < file.size) prepared = compressed
    } catch (error) {
      if (file.size > MAX_IMAGE_UPLOAD_BYTES) {
        const reason = error instanceof Error ? `：${error.message}` : ''
        throw new Error(`图片为 ${formatMegabytes(file.size)} MB，超过 10 MB，且自动压缩失败${reason}`)
      }
    }
  }
  if (prepared.size > MAX_IMAGE_UPLOAD_BYTES) {
    const prefix = shouldCompress ? '图片压缩后仍有' : '图片为'
    throw new Error(`${prefix} ${formatMegabytes(prepared.size)} MB，超过 10 MB，请裁剪后重试`)
  }
  return prepared
}

async function compressImageForUpload(file: File): Promise<File> {
  const decoded = await decodeImage(file)
  try {
    const scale = Math.min(1, IMAGE_MAX_DIMENSION / Math.max(decoded.width, decoded.height))
    const width = Math.max(1, Math.round(decoded.width * scale))
    const height = Math.max(1, Math.round(decoded.height * scale))
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const context = canvas.getContext('2d')
    if (!context) throw new Error('浏览器无法创建图片画布')
    context.drawImage(decoded.source, 0, 0, width, height)
    const blob = await canvasToBlob(canvas)
    return new File([blob], compressedImageName(file.name, blob.type), {
      type: blob.type,
      lastModified: file.lastModified,
    })
  } finally {
    decoded.close()
  }
}

async function decodeImage(file: File): Promise<{
  source: CanvasImageSource
  width: number
  height: number
  close: () => void
}> {
  if (typeof createImageBitmap === 'function') {
    const bitmap = await createImageBitmap(file)
    return {
      source: bitmap,
      width: bitmap.width,
      height: bitmap.height,
      close: () => bitmap.close(),
    }
  }
  if (typeof Image === 'undefined' || typeof URL === 'undefined') {
    throw new Error('当前浏览器不支持图片压缩')
  }
  const url = URL.createObjectURL(file)
  const image = new Image()
  try {
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve()
      image.onerror = () => reject(new Error('无法读取图片'))
      image.src = url
    })
    return {
      source: image,
      width: image.naturalWidth,
      height: image.naturalHeight,
      close: () => URL.revokeObjectURL(url),
    }
  } catch (error) {
    URL.revokeObjectURL(url)
    throw error
  }
}

function canvasToBlob(canvas: HTMLCanvasElement): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => {
        if (blob) resolve(blob)
        else reject(new Error('浏览器无法编码压缩图片'))
      },
      'image/webp',
      IMAGE_WEBP_QUALITY,
    )
  })
}

function compressedImageName(name: string, mimeType: string): string {
  const extension = mimeType === 'image/png' ? '.png' : mimeType === 'image/jpeg' ? '.jpg' : '.webp'
  const base = name.replace(/\.[^.]+$/, '') || 'image'
  return `${base}${extension}`
}

function formatMegabytes(bytes: number): string {
  return (bytes / (1024 * 1024)).toFixed(1)
}
