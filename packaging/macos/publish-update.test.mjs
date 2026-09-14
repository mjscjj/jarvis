import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';

for (const candidateMode of [false, true]) {
test(`publishing ${candidateMode ? 'the sealed candidate without building' : 'after building'} refuses an existing immutable version`, () => {
  const root = mkdtempSync(join(tmpdir(), 'jarvis-publish-test-'));
  const put = (path, content, mode = 0o644) => {
    const target = join(root, path);
    mkdirSync(resolve(target, '..'), { recursive: true });
    writeFileSync(target, content, { mode });
  };
  try {
    for (const name of ['publish-update.sh', 'build-dmg.sh', 'runtime-manifest.sh', 'release-candidate.mjs']) {
      put(`packaging/macos/${name}`, readFileSync(new URL(name, import.meta.url)), 0o755);
    }
    put('packaging/macos/fixture.test.mjs', '// No nested packaging tests.\n');
    put('desktop/src-tauri/tauri.conf.json', '{"version":"0.1.2"}');
    put('desktop/package.json', '{"version":"0.1.2"}');
    put('desktop/src-tauri/Cargo.toml', 'version = "0.1.2"\n');
    put('key', 'fixture key');
    const bundle = join(root, 'desktop/src-tauri/target/release/bundle');
    put('bin/uname', '#!/bin/sh\ncase "$1" in -s) echo Darwin;; -m) echo arm64;; esac\n', 0o755);
    put('bin/cargo', '#!/bin/sh\nexit 0\n', 0o755);
    put('bin/npm', `#!/bin/sh
set -eu
test -z "\${TAURI_SIGNING_PRIVATE_KEY+x}"
test -z "\${TAURI_SIGNING_PRIVATE_KEY_PASSWORD+x}"
test -z "\${TAURI_SIGNING_PRIVATE_KEY_PATH+x}"
if [ "$3" = run ]; then
  mkdir -p '${bundle}/macos' '${bundle}/dmg'
  printf artifact > '${bundle}/macos/Jarvis.app.tar.gz'
  printf dmg > '${bundle}/dmg/Jarvis_0.1.2_aarch64.dmg'
fi
`, 0o755);
    put('desktop/node_modules/.bin/tauri', `#!/bin/sh
set -eu
test "$1 $2 $3" = 'signer sign --private-key-path'
test -f "$4"
test -z "\${TAURI_SIGNING_PRIVATE_KEY+x}"
for arg in "$@"; do artifact="$arg"; done
test -s "$artifact"
printf signature > "$artifact.sig"
`, 0o755);
    put('bin/ssh', '#!/bin/sh\nexec sh -c "$2"\n', 0o755);
    put('bin/scp', `#!/usr/bin/env node
const fs=require('node:fs'), path=require('node:path');
const args=process.argv.slice(2), dest=args.pop().split(':').slice(1).join(':');
for(const file of args)fs.copyFileSync(file,path.join(dest,path.basename(file)));
`, 0o755);
    const remoteRoot = join(root, 'remote');
    const env = { ...process.env, PATH: `${join(root, 'bin')}:${process.env.PATH}`,
      TAURI_SIGNING_PRIVATE_KEY: 'must-not-reach-build', TAURI_SIGNING_PRIVATE_KEY_PASSWORD: '',
      TAURI_SIGNING_PRIVATE_KEY_PATH: join(root, 'key'),
      JARVIS_UPDATE_REMOTE: 'fixture', JARVIS_UPDATE_REMOTE_ROOT: remoteRoot,
      JARVIS_UPDATE_BASE_URL: `file://${remoteRoot}` };
    delete env.NODE_TEST_CONTEXT;
    const run = () => spawnSync('zsh', [join(root, 'packaging/macos/publish-update.sh'), 'test release'], { env, encoding: 'utf8', timeout: 30000 });
    let publish = run;
    if (candidateMode) {
      const artifacts = {};
      for (const [name, content] of [['Jarvis_0.1.2_aarch64.app.tar.gz', 'artifact'], ['Jarvis_0.1.2_aarch64.dmg', 'dmg']]) {
        put(`candidate/${name}`, content);
        artifacts[name] = { size: Buffer.byteLength(content), sha256: createHash('sha256').update(content).digest('hex') };
      }
      put('candidate/candidate.json', JSON.stringify({ schema: 1, version: '0.1.2', commit: 'a'.repeat(40), artifacts }));
      // The development checkout can advance; publishing must use candidate identity.
      put('desktop/src-tauri/tauri.conf.json', '{"version":"9.9.9"}');
      put('packaging/macos/build-dmg.sh', '#!/bin/sh\necho unexpected-build >&2\nexit 90\n', 0o755);
      publish = () => spawnSync('zsh', [join(root, 'packaging/macos/publish-update.sh'), '--candidate', join(root, 'candidate'), 'test release'], { env, encoding: 'utf8', timeout: 30000 });
      put('candidate/Jarvis_0.1.2_aarch64.app.tar.gz', 'modified');
      const rejected = publish();
      assert.notEqual(rejected.status, 0);
      assert.match(rejected.stderr, /candidate artifact changed/);
      put('candidate/Jarvis_0.1.2_aarch64.app.tar.gz', 'artifact');
    }
    const first = publish();
    assert.equal(first.status, 0, first.stdout + first.stderr);
    const manifest = JSON.parse(readFileSync(join(remoteRoot, 'latest.json'), 'utf8'));
    assert.equal(manifest.version, '0.1.2');
    assert.equal(manifest.platforms['darwin-aarch64'].signature, 'signature');
    const artifact = join(remoteRoot, 'Jarvis_0.1.2_aarch64.app.tar.gz');
    assert.equal(readFileSync(artifact, 'utf8'), 'artifact');
    const second = publish();
    assert.notEqual(second.status, 0);
    assert.match(second.stderr, /already exists/);
    assert.equal(readFileSync(artifact, 'utf8'), 'artifact');
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
}
