export class ChatConnectionError extends Error {}

export interface ChatStreamEvent {
  event: string
  data: string
}

// A chat turn succeeds only after the server's terminal event. EOF alone can
// also mean a proxy or network dropped the connection halfway through a reply.
export async function readChatStream(
  body: ReadableStream<Uint8Array>,
  consume: (event: ChatStreamEvent) => void,
  signal: AbortSignal,
  idleTimeoutMs = 45_000,
): Promise<void> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let terminal = false
  let timer: ReturnType<typeof setTimeout> | undefined
  let rejectInterrupted: (reason: unknown) => void = () => {}
  const interrupted = new Promise<never>((_, reject) => { rejectInterrupted = reject })
  const onAbort = () => rejectInterrupted(signal.reason)
  const touch = () => {
    clearTimeout(timer)
    timer = setTimeout(() => rejectInterrupted(new ChatConnectionError('连接长时间没有响应，请恢复连接后继续。')), idleTimeoutMs)
  }
  const consumeBlock = (block: string) => {
    let event = 'message'
    const data: string[] = []
    for (const line of block.split(/\r?\n/)) {
      if (line.startsWith('event:')) event = line.slice(6).trim()
      if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, ''))
    }
    if (data.length === 0) return // Heartbeats still reset the idle deadline.
    consume({ event, data: data.join('\n') })
    terminal = event === 'done' || event === 'error'
  }

  try {
    signal.throwIfAborted()
    signal.addEventListener('abort', onAbort, { once: true })
    touch()
    while (!terminal) {
      const { done, value } = await Promise.race([reader.read(), interrupted])
      if (done) throw new ChatConnectionError('连接已中断，尚未收到完整回复。')
      touch()
      buffer += decoder.decode(value, { stream: true })
      let separator: RegExpExecArray | null
      while (!terminal && (separator = /\r?\n\r?\n/.exec(buffer))) {
        const block = buffer.slice(0, separator.index)
        buffer = buffer.slice(separator.index + separator[0].length)
        consumeBlock(block)
      }
    }
  } finally {
    clearTimeout(timer)
    signal.removeEventListener('abort', onAbort)
    // Do not wait for a broken transport to acknowledge cancellation.
    void reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}
