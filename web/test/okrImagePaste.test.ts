import assert from 'node:assert/strict'
import test from 'node:test'

import { imageFilesFromClipboard } from '../src/okr/emily/imagePaste.ts'

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
