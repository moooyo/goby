#!/usr/bin/env node
// Retain installed production-package license texts beside the player bundle.
import { createHash } from 'node:crypto';
import { lstatSync, mkdirSync, readFileSync, readdirSync, realpathSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = realpathSync(fileURLToPath(new URL('./', import.meta.url)));
const destination = join(root, 'notices');
const maximum = 2 * 1024 * 1024;
const digest = (bytes) => createHash('sha256').update(bytes).digest('hex');

function need(value, message) {
  if (!value) throw new Error(message);
}

function input(path) {
  need(realpathSync(path) === path, `Linked notice input is not supported: ${path}`);
  const before = lstatSync(path);
  need(before.isFile() && before.size > 0 && before.size <= maximum, `Invalid notice input: ${path}`);
  const bytes = readFileSync(path);
  const after = lstatSync(path);
  need(bytes.length === before.size && ['dev', 'ino', 'size', 'mtimeMs', 'ctimeMs'].every((key) => before[key] === after[key]),
    `Notice input changed: ${path}`);
  return bytes;
}

const lockBytes = input(join(root, 'package-lock.json'));
const lock = JSON.parse(lockBytes);
need(lock.lockfileVersion === 3 && lock.packages && typeof lock.packages === 'object', 'Unsupported player lockfile.');
const packages = [];
for (const [name, entry] of Object.entries(lock.packages).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0)) {
  if (name === '' || entry.dev === true) continue;
  // Nested production packages are retained under their complete lockfile path.
  need(/^node_modules\/(?:@[^/]+\/)?[^/]+(?:\/node_modules\/(?:@[^/]+\/)?[^/]+)*$/.test(name)
    && name.split('/').every((part) => /^[A-Za-z0-9@_.-]+$/.test(part) && !['.', '..'].includes(part)),
  `Unsupported production package path: ${name}`);
  const directory = resolve(root, name);
  need(realpathSync(directory) === directory && lstatSync(directory).isDirectory(), `Missing production package: ${name}`);
  const manifest = JSON.parse(input(join(directory, 'package.json')));
  need(manifest.version === entry.version && typeof manifest.name === 'string', `Installed package differs from lockfile: ${name}`);
  const texts = readdirSync(directory).sort().filter((file) => /^(?:licen[cs]e|copying|notice|ofl)(?:[._-].*)?$/i.test(file));
  need(texts.length > 0, `Production package has no retained license text: ${name}`);
  packages.push({ name: manifest.name, path: name, version: entry.version, license: entry.license ?? manifest.license,
    integrity: entry.integrity, files: texts.map((file) => {
      const bytes = input(join(directory, file));
      return { name: `${name}/${file}`, bytes, sha256: digest(bytes) };
    }) });
}
need(packages.length > 0, 'No production package licenses were selected.');
mkdirSync(destination);
for (const entry of packages) {
  for (const file of entry.files) {
    const target = join(destination, file.name);
    mkdirSync(resolve(target, '..'), { recursive: true });
    writeFileSync(target, file.bytes, { flag: 'wx', mode: 0o644 });
    file.bytes = file.bytes.length;
  }
}
writeFileSync(join(destination, 'THIRD-PARTY.md'), input(join(root, 'THIRD-PARTY.md')), { flag: 'wx', mode: 0o644 });
writeFileSync(join(destination, 'manifest.json'), `${JSON.stringify({ kind: 'goby-player-package-notices', version: 1,
  packageLock: { bytes: lockBytes.length, sha256: digest(lockBytes) }, packages,
  boundary: 'Installed production-package notices and application-mark attribution; not a complete legal determination.' }, null, 2)}\n`,
{ flag: 'wx', mode: 0o644 });
