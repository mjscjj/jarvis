export const DEFAULT_CHAT_PANEL_WIDTH = 420
export const MIN_CHAT_PANEL_WIDTH = 360
export const CHAT_PANEL_WIDTH_STORAGE_KEY = 'jarvis.chatPanelWidth'

export function maxChatPanelWidth(viewportWidth: number, siderWidth: number) {
  const available = viewportWidth <= 900
    ? viewportWidth - 24
    : viewportWidth - siderWidth - 24
  return Math.max(MIN_CHAT_PANEL_WIDTH, available)
}

export function clampChatPanelWidth(width: number, viewportWidth: number, siderWidth: number) {
  return Math.min(
    Math.max(width, MIN_CHAT_PANEL_WIDTH),
    maxChatPanelWidth(viewportWidth, siderWidth),
  )
}

export function chatPanelWidthFromPointer(clientX: number, viewportWidth: number, siderWidth: number) {
  return clampChatPanelWidth(viewportWidth - clientX, viewportWidth, siderWidth)
}
