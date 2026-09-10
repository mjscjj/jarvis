import assert from 'node:assert/strict'
import { mkdtempSync, writeFileSync, rmSync, readFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
import test from 'node:test'

const script = fileURLToPath(new URL('./check-lark-skills.sh', import.meta.url))
const required = ['lark-shared', 'lark-contact', 'lark-drive', 'lark-doc', 'lark-im']

for (const failure of ['', 'missing-skill', 'missing-references', 'unreadable-reference', 'empty-content']) {
  test(`embedded Skills check: ${failure || 'complete binary'}`, () => {
    const dir = mkdtempSync(join(tmpdir(), 'jarvis-lark-skills-'))
    try {
      const binary = join(dir, 'lark cli')
      const log = join(dir, 'calls.jsonl')
      writeFileSync(binary, `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.appendFileSync(process.env.TEST_CALLS, JSON.stringify(args) + '\\n');
const name = args[2];
const failure = process.env.TEST_FAILURE;
if (args[0] !== 'skills') process.exit(2);
if (args[1] === 'list') {
  const entries = failure === 'missing-references' && name.startsWith('lark-doc/')
    ? [] : [{ path: name + '/read.md', is_dir: false }];
  console.log(JSON.stringify({ ok: true, entries }));
} else if (args[1] === 'read') {
  if (failure === 'missing-skill' && name === 'lark-drive') process.exit(1);
  if (failure === 'unreadable-reference' && name === 'lark-im/references/read.md') process.exit(1);
  console.log(failure === 'empty-content' && name === 'lark-shared' ? '  ' : '# ' + name);
} else process.exit(2);
`, { mode: 0o755 })
      const result = spawnSync('bash', [script, binary], {
        encoding: 'utf8', env: { ...process.env, TEST_FAILURE: failure, TEST_CALLS: log },
      })
      assert.equal(result.status, failure ? 1 : 0, result.stderr)
      if (!failure) {
        const calls = readFileSync(log, 'utf8').trim().split('\n').map(JSON.parse)
        for (const name of required) {
          assert.ok(calls.some(args => args.join(' ') === `skills read ${name}`))
          assert.ok(calls.some(args => args.join(' ') === `skills read ${name}/references/read.md`))
        }
      }
    } finally {
      rmSync(dir, { recursive: true, force: true })
    }
  })
}
