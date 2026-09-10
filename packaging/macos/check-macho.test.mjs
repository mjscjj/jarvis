import assert from 'node:assert/strict'
import { chmodSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
import test from 'node:test'

const script = fileURLToPath(new URL('./check-macho.sh', import.meta.url))

function fixture() {
  const dir = mkdtempSync(join(tmpdir(), 'jarvis-macho-check-'))
  const tools = join(dir, 'tools')
  mkdirSync(tools)
  writeFileSync(join(tools, 'lipo'), `#!/bin/sh
case "$2" in
  *x86*) printf 'x86_64\\n' ;;
  *) printf 'arm64\\n' ;;
esac
`, { mode: 0o755 })
  writeFileSync(join(tools, 'vtool'), `#!/bin/sh
case "$2" in
  *min26*) minos=26.0 ;;
  *) minos=14.0 ;;
esac
printf 'Load command 1\\n      cmd LC_BUILD_VERSION\\n platform MACOS\\n    minos %s\\n      sdk 26.0\\n' "$minos"
`, { mode: 0o755 })
  writeFileSync(join(tools, 'otool'), `#!/bin/sh
printf '%s:\\n' "$2"
case "$2" in
  *homebrew*) printf '\\t/opt/homebrew/opt/oniguruma/lib/libonig.5.dylib (compatibility version 1.0.0, current version 1.0.0)\\n' ;;
  *) printf '\\t/usr/lib/libSystem.B.dylib (compatibility version 1.0.0, current version 1.0.0)\\n' ;;
esac
`, { mode: 0o755 })
  return { dir, tools }
}

function check(name, tools, dir) {
  const binary = join(dir, name)
  writeFileSync(binary, '')
  chmodSync(binary, 0o755)
  return spawnSync('zsh', [script, binary, name], {
    encoding: 'utf8',
    env: { ...process.env, PATH: `${tools}:/usr/bin:/bin` },
  })
}

test('accepts a thin arm64 binary targeting macOS 14 with system dependencies', () => {
  const { dir, tools } = fixture()
  try {
    const result = check('arm64-min14', tools, dir)
    assert.equal(result.status, 0, result.stderr)
    assert.match(result.stdout, /arm64 minos=14\.0 dependencies=system-only/)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

test('rejects a binary whose minimum macOS exceeds the supported baseline', () => {
  const { dir, tools } = fixture()
  try {
    const result = check('arm64-min26', tools, dir)
    assert.notEqual(result.status, 0)
    assert.match(result.stderr, /minimum macOS 26\.0 exceeds supported baseline 14\.0/)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

test('rejects a Homebrew dynamic dependency', () => {
  const { dir, tools } = fixture()
  try {
    const result = check('arm64-homebrew', tools, dir)
    assert.notEqual(result.status, 0)
    assert.match(result.stderr, /non-system dynamic dependency: \/opt\/homebrew/)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

test('rejects a non-arm64 binary', () => {
  const { dir, tools } = fixture()
  try {
    const result = check('x86-min14', tools, dir)
    assert.notEqual(result.status, 0)
    assert.match(result.stderr, /expected thin arm64 Mach-O, got: x86_64/)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})
