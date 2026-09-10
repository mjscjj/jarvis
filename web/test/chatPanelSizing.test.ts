import assert from 'node:assert/strict'
import test from 'node:test'

import {
  MIN_CHAT_PANEL_WIDTH,
  chatPanelWidthFromPointer,
  clampChatPanelWidth,
  maxChatPanelWidth,
} from '../src/chatPanelSizing.ts'

test('chat panel width follows its left edge and stays inside the desktop viewport', () => {
  assert.equal(chatPanelWidthFromPointer(900, 1440, 216), 540)
  assert.equal(chatPanelWidthFromPointer(1200, 1440, 216), MIN_CHAT_PANEL_WIDTH)
  assert.equal(chatPanelWidthFromPointer(0, 1440, 216), 1200)
})

test('chat panel sizing keeps the existing narrow-screen overlay allowance', () => {
  assert.equal(maxChatPanelWidth(880, 216), 856)
  assert.equal(clampChatPanelWidth(900, 880, 216), 856)
})
