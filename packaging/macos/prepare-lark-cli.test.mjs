import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
import test from 'node:test'

const source = fileURLToPath(new URL('./prepare-lark-cli.sh', import.meta.url))

function fixture(t, { corrupt = false, downloadFails = false } = {}) {
  const root = mkdtempSync(join(tmpdir(), 'jarvis-lark-package-'))
  t.after(() => rmSync(root, { recursive: true, force: true }))
  const scripts = join(root, 'packaging')
  const release = join(root, 'release')
  const tools = join(root, 'tools')
  const runtime = join(root, 'runtime with spaces')
  for (const dir of [scripts, release, tools]) mkdirSync(dir)
  copyFileSync(source, join(scripts, 'prepare-lark-cli.sh'))
  writeFileSync(join(release, 'lark-cli'), '#!/bin/sh\nprintf "release-binary\\n"\n')
  writeFileSync(join(release, 'LICENSE'), 'release license\n')
  const archive = join(root, 'release.tar.gz')
  const packed = spawnSync('tar', ['-czf', archive, '-C', release, 'lark-cli', 'LICENSE'], { encoding: 'utf8' })
  assert.equal(packed.status, 0, packed.stderr)
  const hash = createHash('sha256').update(readFileSync(archive)).digest('hex')
  writeFileSync(join(scripts, 'runtime-manifest.sh'), [
    'JARVIS_LARK_CLI_VERSION=1.0.93',
    'JARVIS_LARK_CLI_URL=https://example.test/pinned-release.tar.gz',
    `JARVIS_LARK_CLI_SHA256=${corrupt ? '0'.repeat(64) : hash}`,
    '',
  ].join('\n'))
  writeFileSync(join(tools, 'curl'), downloadFails ? '#!/bin/sh\nexit 22\n' : `#!/bin/sh
test "$1" = '-fL' || exit 90
test "$2" = 'https://example.test/pinned-release.tar.gz' || exit 91
test "$3" = '-o' || exit 92
cp "$FIXTURE_ARCHIVE" "$4"
`, { mode: 0o755 })
  writeFileSync(join(tools, 'lark-cli'), '#!/bin/sh\necho host-cli-must-not-run >&2\nexit 99\n', { mode: 0o755 })
  return {
    runtime,
    run: () => spawnSync('zsh', [join(scripts, 'prepare-lark-cli.sh'), runtime], {
      encoding: 'utf8',
      env: {
        ...process.env,
        PATH: `${tools}:/usr/bin:/bin`,
        FIXTURE_ARCHIVE: archive,
        JARVIS_LARK_CLI_BIN: join(tools, 'lark-cli'),
      },
    }),
  }
}

test('installs the verified release and license without using the host CLI', t => {
  const f = fixture(t)
  const result = f.run()
  assert.equal(result.status, 0, result.stderr)
  assert.match(result.stdout, /official v1\.0\.93 darwin-arm64/)
  const binary = join(f.runtime, 'bin', 'lark-cli')
  assert.match(readFileSync(binary, 'utf8'), /release-binary/)
  assert.equal(statSync(binary).mode & 0o777, 0o755)
  assert.equal(readFileSync(join(f.runtime, 'licenses', 'lark-cli-LICENSE'), 'utf8'), 'release license\n')
})

test('rejects an incorrect checksum before installing the archive', t => {
  const f = fixture(t, { corrupt: true })
  const result = f.run()
  assert.notEqual(result.status, 0)
  assert.match(result.stderr, /sha256 mismatch/)
  assert.equal(existsSync(f.runtime), false)
})

test('a failed download fails the build even when a host CLI is available', t => {
  const f = fixture(t, { downloadFails: true })
  const result = f.run()
  assert.equal(result.status, 22)
  assert.equal(existsSync(f.runtime), false)
})
