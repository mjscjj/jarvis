import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { createReadStream } from 'node:fs';
import { copyFile, mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const semver = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?$/;
const names = version => [`Jarvis_${version}_aarch64.app.tar.gz`, `Jarvis_${version}_aarch64.dmg`];

async function fingerprint(path) {
  const info = await stat(path);
  if (!info.isFile() || info.size === 0) throw new Error(`missing or empty artifact: ${path}`);
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(path)) hash.update(chunk);
  return { size: info.size, sha256: hash.digest('hex') };
}

export async function verifyCandidate(directory) {
  const record = JSON.parse(await readFile(join(directory, 'candidate.json'), 'utf8'));
  if (record.schema !== 1 || !semver.test(record.version) || !/^[a-f0-9]{40}$/.test(record.commit)) {
    throw new Error('invalid candidate identity');
  }
  if (JSON.stringify(Object.keys(record.artifacts).sort()) !== JSON.stringify(names(record.version).sort())) {
    throw new Error('candidate must contain exactly the versioned updater archive and DMG');
  }
  for (const [name, expected] of Object.entries(record.artifacts)) {
    const actual = await fingerprint(join(directory, name));
    if (actual.size !== expected.size || actual.sha256 !== expected.sha256) {
      throw new Error(`candidate artifact changed: ${name}; rebuild and repeat acceptance`);
    }
  }
  return record;
}

// Records artifact identity, not acceptance. The Skill owns the evidence and
// runs the build in a dedicated detached checkout before sealing.
export async function sealCandidate(checkout, directory) {
  const git = (...args) => execFileSync('git', ['-C', checkout, ...args], { encoding: 'utf8' }).trim();
  if (git('rev-parse', '--abbrev-ref', 'HEAD') !== 'HEAD') {
    throw new Error('build and seal in a dedicated detached checkout');
  }
  if (git('status', '--porcelain', '--untracked-files=no')) throw new Error('candidate checkout has tracked changes');
  const commit = git('rev-parse', 'HEAD');
  const config = JSON.parse(await readFile(join(checkout, 'desktop/src-tauri/tauri.conf.json'), 'utf8'));
  const pkg = JSON.parse(await readFile(join(checkout, 'desktop/package.json'), 'utf8'));
  const cargo = await readFile(join(checkout, 'desktop/src-tauri/Cargo.toml'), 'utf8');
  const version = config.version;
  if (!semver.test(version) || pkg.version !== version || cargo.match(/^version\s*=\s*"([^"]+)"/m)?.[1] !== version) {
    throw new Error('desktop versions do not match');
  }
  const bundle = join(checkout, 'desktop/src-tauri/target/release/bundle');
  const sources = [join(bundle, 'macos/Jarvis.app.tar.gz'), join(bundle, `dmg/Jarvis_${version}_aarch64.dmg`)];
  const fingerprints = await Promise.all(sources.map(fingerprint));
  // A new build gets a new directory and a new acceptance record.
  await mkdir(directory);
  const artifacts = {};
  for (const [index, name] of names(version).entries()) {
    await copyFile(sources[index], join(directory, name));
    artifacts[name] = fingerprints[index];
  }
  const record = { schema: 1, version, commit, createdAt: new Date().toISOString(), artifacts };
  await writeFile(join(directory, 'candidate.json'), `${JSON.stringify(record, null, 2)}\n`, { flag: 'wx' });
  await verifyCandidate(directory);
  return record;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const [command, first, second, ...extra] = process.argv.slice(2);
  try {
    let record;
    if (command === 'seal' && first && second && !extra.length) {
      record = await sealCandidate(resolve(first), resolve(second));
    } else if (command === 'verify' && first && !second) {
      record = await verifyCandidate(resolve(first));
    } else {
      throw new Error('usage: release-candidate.mjs seal <detached-checkout> <new-directory> | verify <directory>');
    }
    console.log(JSON.stringify(record, null, 2));
  } catch (error) {
    console.error(`release-candidate: ${error.message}`);
    process.exitCode = 1;
  }
}
