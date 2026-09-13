// Extract JSON token spans, never parse/re-encode numeric evidence.
// JSON.parse validates syntax only; all values below retain their raw literals.
import { readFileSync } from 'node:fs'

const raw = readFileSync(0, 'utf8')
JSON.parse(raw)
const tokens = [...raw.matchAll(/"(?:\\[\s\S]|[^"\\])*"|[{}\[\]:,]|true|false|null|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g)].map(match => match[0])
let cursor = 0
function value() {
  const start = cursor
  const opening = tokens[cursor++]
  const children = []
  if (opening === '{' || opening === '[') {
    const closing = opening === '{' ? '}' : ']'
    while (tokens[cursor] !== closing) {
      let key = String(children.length)
      if (opening === '{') {
        key = JSON.parse(tokens[cursor++])
        cursor++ // colon
      }
      children.push({ key, node: value() })
      if (tokens[cursor] === ',') cursor++
    }
    cursor++
  }
  return { start, end: cursor, opening, children }
}
const root = value()
const text = node => tokens.slice(node.start, node.end).join('')
const args = process.argv.slice(2)
const child = (node, key) => node?.children.filter(entry => entry.key === key).at(-1)?.node
if (args[0] === '--project-items') {
  // Thin list summaries keep selected fields, including loose nested values,
  // without converting any numeric literals. Domain field lists live in Shell.
  const [, outputKey, rootKeys, itemKeys] = args
  const data = child(root, 'data')
  const items = child(data, 'items')
  if (!items || items.opening !== '[') throw new Error('API response is missing items')
  const fields = (node, keys) => keys.split(',').filter(Boolean).map(key => {
    const selected = child(node, key)
    return JSON.stringify(key) + ':' + (selected ? text(selected) : 'null')
  })
  const projected = items.children.map(entry => '{' + fields(entry.node, itemKeys).join(',') + '}')
  process.stdout.write('{' + [...fields(data, rootKeys), JSON.stringify(outputKey) + ':[' + projected.join(',') + ']'].join(',') + '}\n')
} else if (args[0] === '--set') {
  const [, key, replacement] = args
  if (root.opening !== '{' || args.length !== 3) throw new Error('--set requires an object, key and JSON value')
  JSON.parse(replacement)
  const entries = root.children.filter(child => child.key !== key).map(child => JSON.stringify(child.key) + ':' + text(child.node))
  entries.push(JSON.stringify(key) + ':' + replacement)
  process.stdout.write('{' + entries.join(',') + '}\n')
} else {
  const path = args.length ? args : ['data']
  if (root.opening !== '{' || !root.children.some(child => child.key === 'data')) throw new Error('API response is missing data')
  let node = root
  for (const key of path) {
    // Last duplicate key wins, matching JSON object semantics without losing numbers.
    node = node?.children.filter(child => child.key === key).at(-1)?.node
  }
  process.stdout.write((node ? text(node) : 'null') + '\n')
}
