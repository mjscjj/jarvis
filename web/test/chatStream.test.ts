import assert from 'node:assert/strict'
import test from 'node:test'
import { ChatConnectionError, readChatStream } from '../src/chatStream.ts'

const encoder = new TextEncoder()
function stream(text: string, close = true) {
  return new ReadableStream<Uint8Array>({ start(controller) {
    // Split every UTF-8 byte, including CRLF delimiters and Chinese text.
    for (const byte of encoder.encode(text)) controller.enqueue(Uint8Array.of(byte))
    if (close) controller.close()
  } })
}

test('fragmented UTF-8, CRLF, multiline data and heartbeats retain exact text', async () => {
  const events: unknown[] = []
  await readChatStream(stream(': ping\r\n\r\nevent: delta\r\ndata: 你好\r\ndata: 世界\r\n\r\nevent: done\r\ndata: {}\r\n\r\n'), (ev) => events.push(ev), new AbortController().signal)
  assert.deepEqual(events, [{ event: 'delta', data: '你好\n世界' }, { event: 'done', data: '{}' }])
})

test('done completes immediately even when transport remains open', async () => {
  await readChatStream(stream('event: done\ndata: {}\n\n', false), () => {}, new AbortController().signal, 25)
})

test('EOF without done preserves partial text and reports an interruption', async () => {
  let text = ''
  await assert.rejects(readChatStream(stream('event: delta\ndata: partial\n\n'), (ev) => { text += ev.data }, new AbortController().signal), ChatConnectionError)
  assert.equal(text, 'partial')
})

test('an unterminated done frame does not falsely complete the turn', async () => {
  await assert.rejects(readChatStream(stream('event: done\ndata: {}'), () => {}, new AbortController().signal), ChatConnectionError)
})

test('idle connection times out and cancels its reader', async () => {
  let cancelled = false
  const body = new ReadableStream<Uint8Array>({ cancel() { cancelled = true } })
  await assert.rejects(readChatStream(body, () => {}, new AbortController().signal, 15), ChatConnectionError)
  assert.equal(cancelled, true)
  assert.equal(body.locked, false)
})

test('heartbeats keep a working but quiet Agent connected', async () => {
  let timer: ReturnType<typeof setInterval>
  let count = 0
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      timer = setInterval(() => {
        controller.enqueue(encoder.encode(++count < 6 ? ': ping\n\n' : 'event: done\ndata: {}\n\n'))
      }, 10)
    },
    cancel() { clearInterval(timer) },
  })
  await readChatStream(body, () => {}, new AbortController().signal, 40)
  assert.ok(count >= 6)
})

test('server error is preserved, and explicit cancellation unblocks a pending read', async () => {
  const diagnostic = new Error('Agent failed')
  await assert.rejects(readChatStream(stream('event: error\ndata: {}\n\n', false), () => { throw diagnostic }, new AbortController().signal), (cause) => cause === diagnostic)
  const controller = new AbortController()
  const pending = readChatStream(stream('', false), () => {}, controller.signal)
  controller.abort()
  await assert.rejects(pending, { name: 'AbortError' })
})
