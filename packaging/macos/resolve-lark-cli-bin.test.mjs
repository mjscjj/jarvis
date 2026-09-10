import assert from 'node:assert/strict'
import { chmodSync, mkdirSync, mkdtempSync, realpathSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
import test from 'node:test'

const script = fileURLToPath(new URL('./resolve-lark-cli-bin.sh', import.meta.url))

function fixture() {
  const dir = mkdtempSync(join(tmpdir(), 'jarvis-lark-bin-'))
  const tools = join(dir, 'tools')
  mkdirSync(tools)
  writeFileSync(join(tools, 'lipo'), `#!/bin/sh
case "$2" in
  *native-arm64) printf 'arm64\\n' ;;
  *) exit 1 ;;
esac
`, { mode: 0o755 })
  return { dir, tools }
}

function resolve(entry, tools) {
  return spawnSync('zsh', [script, entry], {
    encoding: 'utf8',
    env: { ...process.env, PATH: `${tools}:/usr/bin:/bin` },
  })
}

test('accepts a native arm64 lark-cli binary', () => {
  const { dir, tools } = fixture()
  try {
    const binary = join(dir, 'native-arm64')
    writeFileSync(binary, '')
    chmodSync(binary, 0o755)
    const result = resolve(binary, tools)
    assert.equal(result.status, 0, result.stderr)
    assert.equal(result.stdout.trim(), realpathSync(binary))
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

test('resolves the official npm launcher to its native binary', () => {
  const { dir, tools } = fixture()
  try {
    const scripts = join(dir, 'package', 'scripts')
    const bin = join(dir, 'package', 'bin')
    const commandDirectory = join(dir, '.local', 'bin')
    mkdirSync(scripts, { recursive: true })
    mkdirSync(bin, { recursive: true })
    mkdirSync(commandDirectory, { recursive: true })
    const launcher = join(scripts, 'run.js')
    writeFileSync(launcher, '#!/usr/bin/env node\n')
    chmodSync(launcher, 0o755)
    const command = join(commandDirectory, 'lark-cli')
    symlinkSync('../../package/scripts/run.js', command)

    const expected = join(bin, 'lark-cli')
    writeFileSync(expected, '')
    chmodSync(expected, 0o755)
    writeFileSync(join(tools, 'lipo'), `#!/bin/sh
case "$2" in
  */package/bin/lark-cli) printf 'arm64\\n' ;;
  *) exit 1 ;;
esac
`, { mode: 0o755 })

    const result = resolve(command, tools)
    assert.equal(result.status, 0, result.stderr)
    assert.equal(result.stdout.trim(), realpathSync(expected))
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

test('rejects a launcher without an arm64 native binary', () => {
  const { dir, tools } = fixture()
  try {
    const scripts = join(dir, 'package', 'scripts')
    mkdirSync(scripts, { recursive: true })
    const launcher = join(scripts, 'run.js')
    writeFileSync(launcher, '#!/usr/bin/env node\n')
    chmodSync(launcher, 0o755)

    const result = resolve(launcher, tools)
    assert.notEqual(result.status, 0)
    assert.match(result.stderr, /native lark-cli binary not found/)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})
