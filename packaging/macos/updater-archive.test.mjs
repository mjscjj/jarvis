import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { createUpdaterArchive, findAppleDoubleEntry } from './updater-archive.mjs';

function fixture() {
  const root = mkdtempSync(join(tmpdir(), 'jarvis-updater-archive-'));
  const app = join(root, 'Jarvis.app');
  mkdirSync(join(app, 'Contents'), { recursive: true });
  writeFileSync(join(app, 'Contents', 'Info.plist'), 'fixture');
  // A signed bundle always carries extended attributes; reproduce one here.
  execFileSync('xattr', ['-w', 'com.apple.metadata:fixture', 'value', app]);
  return { root, app };
}

test('archives built for the updater carry no AppleDouble metadata', async () => {
  const { root } = fixture();
  try {
    const archive = join(root, 'clean.tar.gz');
    await createUpdaterArchive(root, 'Jarvis.app', archive);
    assert.equal(await findAppleDoubleEntry(archive), null);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('a plain macOS tar archive is rejected', async () => {
  const { root } = fixture();
  try {
    const archive = join(root, 'apple-double.tar.gz');
    execFileSync('tar', ['-czf', archive, '-C', root, 'Jarvis.app']);
    assert.equal(await findAppleDoubleEntry(archive), '._Jarvis.app');
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
