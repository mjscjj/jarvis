import assert from 'node:assert/strict'
import test from 'node:test'

import {
  IMAGE_COMPRESSION_THRESHOLD_BYTES,
  MAX_IMAGE_UPLOAD_BYTES,
  imageFilesFromClipboard,
  prepareImageForUpload,
} from '../src/okr/emily/imagePaste.ts'

test('clipboard image selection preserves image order and ignores text files', () => {
  const clipboard = [
    { name: 'first.png', type: 'image/png' },
    { name: 'notes.txt', type: 'text/plain' },
    { name: 'second.webp', type: 'image/webp' },
  ]

  assert.deepEqual(imageFilesFromClipboard(clipboard).map((file) => file.name), ['first.png', 'second.webp'])
})

test('text-only clipboard stays untouched for the textarea', () => {
  assert.deepEqual(imageFilesFromClipboard([{ name: 'plain', type: 'text/plain' }]), [])
})

function imageFile(size: number, name = 'shot.png', type = 'image/png'): File {
  return new File([new Uint8Array(size)], name, { type, lastModified: 123 })
}

test('small images upload unchanged without compression', async () => {
  const original = imageFile(IMAGE_COMPRESSION_THRESHOLD_BYTES)
  let calls = 0

  const prepared = await prepareImageForUpload(original, async () => {
    calls++
    return imageFile(1)
  })

  assert.equal(prepared, original)
  assert.equal(calls, 0)
})

test('large screenshots use a smaller compressed file', async () => {
  const original = imageFile(IMAGE_COMPRESSION_THRESHOLD_BYTES + 1)
  const compressed = imageFile(800_000, 'shot.webp', 'image/webp')

  const prepared = await prepareImageForUpload(original, async (file) => {
    assert.equal(file, original)
    return compressed
  })

  assert.equal(prepared, compressed)
})

test('compression never replaces an image with a larger result', async () => {
  const original = imageFile(IMAGE_COMPRESSION_THRESHOLD_BYTES + 1)
  const prepared = await prepareImageForUpload(
    original,
    async () => imageFile(original.size + 1, 'shot.webp', 'image/webp'),
  )

  assert.equal(prepared, original)
})

test('oversized images report failed compression before upload', async () => {
  const original = imageFile(MAX_IMAGE_UPLOAD_BYTES + 1)

  await assert.rejects(
    prepareImageForUpload(original, async () => {
      throw new Error('无法读取图片')
    }),
    /超过 10 MB，且自动压缩失败：无法读取图片/,
  )
})

test('images that remain oversized after compression are rejected', async () => {
  const original = imageFile(MAX_IMAGE_UPLOAD_BYTES + 2)

  await assert.rejects(
    prepareImageForUpload(
      original,
      async () => imageFile(MAX_IMAGE_UPLOAD_BYTES + 1, 'shot.webp', 'image/webp'),
    ),
    /压缩后仍有 10\.0 MB，超过 10 MB/,
  )
})

test('oversized GIFs are rejected without destroying animation', async () => {
  const original = imageFile(MAX_IMAGE_UPLOAD_BYTES + 1, 'demo.gif', 'image/gif')
  let calls = 0

  await assert.rejects(
    prepareImageForUpload(original, async () => {
      calls++
      return imageFile(1)
    }),
    /图片为 10\.0 MB，超过 10 MB/,
  )
  assert.equal(calls, 0)
})
