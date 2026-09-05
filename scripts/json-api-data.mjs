// Extract an API data value without parsing/re-encoding its numeric evidence.
// JSON.parse validates syntax; the token spans preserve original number literals
// on every supported Node version, including versions without JSON.rawJSON.
import { readFileSync } from 'node:fs'

const raw = readFileSync(0, 'utf8')
const envelope = JSON.parse(raw)
if (!envelope || Array.isArray(envelope) || typeof envelope !== 'object' || !Object.hasOwn(envelope, 'data')) {
  throw new Error('API response is missing data')
}
const tokens = [...raw.matchAll(/"(?:\\[\s\S]|[^"\\])*"|[{}\[\]:,]|true|false|null|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g)]
let cursor = 1 // Opening root brace; syntax has already been validated.
while (tokens[cursor][0] !== '}') {
  const key = JSON.parse(tokens[cursor][0])
  cursor += 2 // Key and colon.
  const start = cursor
  let depth = 0
  do {
    const token = tokens[cursor++][0]
    if (token === '{' || token === '[') depth++
    if (token === '}' || token === ']') depth--
  } while (depth > 0)
  if (key === 'data') {
    process.stdout.write(tokens.slice(start, cursor).map((token) => token[0]).join('') + '\n')
    break
  }
  cursor++ // Comma separating root fields.
}
