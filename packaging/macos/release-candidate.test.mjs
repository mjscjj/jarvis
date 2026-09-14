import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { sealCandidate, verifyCandidate } from './release-candidate.mjs';

test('sealing binds a detached clean commit to immutable copies, not later build output', async () => {
  const root = await mkdtemp(join(tmpdir(), 'jarvis-candidate-'));
  const checkout = join(root, 'checkout');
  const candidate = join(root, 'candidate');
  const put = async (path, data) => {
    await mkdir(join(checkout, path, '..'), { recursive: true });
    await writeFile(join(checkout, path), data);
  };
  const git = (...args) => execFileSync('git', ['-C', checkout, ...args], { encoding: 'utf8' }).trim();
  try {
    await put('desktop/src-tauri/tauri.conf.json', '{"version":"0.1.2"}');
    await put('desktop/package.json', '{"version":"0.1.2"}');
    await put('desktop/src-tauri/Cargo.toml', '[package]\nversion = "0.1.2"\n');
    await put('.gitignore', '/desktop/src-tauri/target/\n');
    git('init', '-q');
    git('add', '.');
    git('-c', 'user.name=Test', '-c', 'user.email=test@example.com', '-c', 'commit.gpgsign=false', 'commit', '-qm', 'fixture');
    await assert.rejects(sealCandidate(checkout, candidate), /detached/);
    git('checkout', '--detach', '-q');
    const archive = 'desktop/src-tauri/target/release/bundle/macos/Jarvis.app.tar.gz';
    await put(archive, 'archive');
    await put('desktop/src-tauri/target/release/bundle/dmg/Jarvis_0.1.2_aarch64.dmg', 'dmg');
    await put('desktop/package.json', '{"version":"0.1.3"}');
    await assert.rejects(sealCandidate(checkout, candidate), /tracked changes/);
    git('restore', 'desktop/package.json');
    const record = await sealCandidate(checkout, candidate);
    assert.equal(record.commit, git('rev-parse', 'HEAD'));
    assert.equal(record.version, '0.1.2');
    await put(archive, 'a different build');
    assert.equal(await readFile(join(candidate, 'Jarvis_0.1.2_aarch64.app.tar.gz'), 'utf8'), 'archive');
    assert.deepEqual(await verifyCandidate(candidate), record);
    await assert.rejects(sealCandidate(checkout, candidate), /EEXIST/);
    await writeFile(join(candidate, 'Jarvis_0.1.2_aarch64.app.tar.gz'), 'changed');
    await assert.rejects(verifyCandidate(candidate), /artifact changed/);
    await writeFile(join(candidate, 'Jarvis_0.1.2_aarch64.app.tar.gz'), 'archive');
    await rm(join(candidate, 'Jarvis_0.1.2_aarch64.dmg'));
    await assert.rejects(verifyCandidate(candidate), /ENOENT/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
