#!/usr/bin/env node
// Build a Linux candidate from the existing production administrator bundle.
// This entry point never installs dependencies, runs Goby, or publishes a release.
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { chmodSync, lstatSync, mkdirSync, readFileSync, readdirSync, realpathSync, writeFileSync } from 'node:fs';
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gunzipSync } from 'node:zlib';

const repository = realpathSync(fileURLToPath(new URL('../', import.meta.url)));
const bundle = join(repository, 'web', 'admin', 'dist');
const sourceDirectories = ['cmd', 'internal'];
const embeddedSource = 'web/admin/embedded.go';
const usage = 'Usage: node scripts/build-release.mjs --arch amd64|arm64 [--output-dir PATH] [--package systemd]';
const packageName = 'goby-linux-amd64-systemd';
const packageLimit = 128 * 1024 * 1024;
const packageSources = [
  ['scripts/build-release.mjs', 2 * 1024 * 1024],
  ['deploy/linux/goby.service', 64 * 1024],
  ['deploy/linux/.env.example', 64 * 1024],
  ['deploy/linux/INSTALL.md', 1024 * 1024],
];
const archiveEnvironment = { PATH: '/usr/bin:/bin', LANG: 'C', LC_ALL: 'C', TZ: 'UTC' };

function need(condition, message) {
  if (!condition) throw new Error(message);
}

function within(path, parent) {
  const name = relative(parent, path);
  return name === '' || (!name.startsWith(`..${sep}`) && name !== '..' && !isAbsolute(name));
}

function digest(bytes) {
  return createHash('sha256').update(bytes).digest('hex');
}

function fileRecord(path) {
  const before = lstatSync(path);
  need(before.isFile(), `Expected a regular build input: ${path}`);
  const bytes = readFileSync(path);
  const after = lstatSync(path);
  need(after.isFile() && bytes.length === before.size
    && ['dev', 'ino', 'size', 'mtimeMs', 'ctimeMs'].every((key) => before[key] === after[key]),
  `Build input changed while being read: ${path}`);
  return { bytes: bytes.length, sha256: digest(bytes) };
}

function viteEntryReferences(entries) {
  const files = new Map(entries.map((entry) => [entry.name, entry]));
  const index = files.get('index.html');
  const bytes = readFileSync(join(bundle, 'index.html'));
  need(bytes.length === index.bytes && digest(bytes) === index.sha256,
    'Administrator index.html changed before its entry references were read.');
  const html = new TextDecoder('utf-8', { fatal: true }).decode(bytes).replace(/<!--[\s\S]*?-->/g, '');
  need(!html.includes('<!--'), 'Administrator index.html contains an unterminated comment.');
  // This recognizes the quoted attributes emitted by the current Vite HTML
  // build. It does not interpret HTML, JavaScript imports or CSS dependency graphs.
  function attributes(text) {
    const values = new Map();
    let rest = text.trim();
    while (rest) {
      const match = /^([A-Za-z_:][A-Za-z0-9_:.-]*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'))?(?:\s+|$)/.exec(rest);
      need(match !== null, 'Administrator script/link attributes must use the supported quoted Vite form.');
      const key = match[1].toLowerCase();
      need(!values.has(key), `Duplicate administrator script/link attribute: ${key}`);
      values.set(key, match[2] ?? match[3] ?? null);
      rest = rest.slice(match[0].length);
    }
    return values;
  }
  const tags = [...html.matchAll(/<([A-Za-z][A-Za-z0-9:-]*)(?=[\s/>])((?:"[^"]*"|'[^']*'|[^"'<>])*)>/g)];
  const relevant = tags.filter((tag) => ['script', 'link'].includes(tag[1].toLowerCase()));
  need(relevant.length === (html.match(/<(?:script|link)(?=[\s/>])/gi) ?? []).length,
    'Administrator index.html contains an unsupported script/link tag.');
  const references = [];
  for (const tag of relevant) {
    const element = tag[1].toLowerCase();
    const values = attributes(element === 'link' ? tag[2].replace(/\/\s*$/, '') : tag[2]);
    let kind;
    let url;
    if (element === 'script') {
      need(values.get('type')?.toLowerCase() === 'module' && typeof values.get('src') === 'string'
        && /^\s*<\/script\s*>/i.test(html.slice(tag.index + tag[0].length)),
      'Administrator entry scripts must be external Vite modules with an empty script body.');
      kind = 'module-script';
      url = values.get('src');
    } else {
      const relations = (values.get('rel') ?? '').toLowerCase().split(/\s+/);
      const selected = relations.filter((relation) => relation === 'stylesheet' || relation === 'modulepreload');
      need(selected.length <= 1, 'Administrator stylesheet/modulepreload relations must be unambiguous.');
      if (selected.length === 0) continue;
      [kind] = selected;
      url = values.get('href');
    }
    need(typeof url === 'string' && url.startsWith('/admin/assets/'),
      `Administrator ${kind} must reference a local /admin/assets/ file.`);
    const name = url.slice('/admin/'.length);
    need(name.split('/').every((part) => /^[A-Za-z0-9_.-]+$/.test(part) && part !== '.' && part !== '..')
      && name.endsWith(kind === 'stylesheet' ? '.css' : '.js'),
    `Administrator ${kind} has an unsupported asset URL: ${url}`);
    const file = files.get(name);
    need(file !== undefined && file.bytes > 0,
      `Administrator HTML references a missing or empty ${kind} file: ${url}`);
    references.push({ kind, url, name, bytes: file.bytes, sha256: file.sha256 });
  }
  need(references.some((reference) => reference.kind === 'module-script'),
    'Administrator index.html must reference its actual Vite module entry.');
  return references;
}

function assetInventory() {
  need(lstatSync(bundle).isDirectory() && realpathSync(bundle) === bundle,
    'web/admin/dist must be a real directory without symbolic-link components.');
  const entries = [];
  function visit(directory) {
    for (const name of readdirSync(directory).sort()) {
      const path = join(directory, name);
      const value = lstatSync(path);
      need(!value.isSymbolicLink(), `Administrator assets cannot contain symbolic links: ${path}`);
      if (value.isDirectory()) {
        visit(path);
      } else {
        entries.push({ name: relative(bundle, path).split(sep).join('/'), ...fileRecord(path) });
      }
    }
  }
  visit(bundle);
  need(entries.some((entry) => entry.name === 'index.html' && entry.bytes > 0),
    'The production administrator bundle needs a nonempty dist/index.html.');
  return { files: entries, entryReferences: viteEntryReferences(entries) };
}

function sourceInventory() {
  const entries = [];
  function visit(directory) {
    need(lstatSync(directory).isDirectory() && realpathSync(directory) === directory,
      `Local source directories cannot contain symbolic links: ${directory}`);
    for (const name of readdirSync(directory).sort()) {
      const path = join(directory, name);
      const value = lstatSync(path);
      need(!value.isSymbolicLink(), `Local source inputs cannot contain symbolic links: ${path}`);
      if (value.isDirectory()) {
        visit(path);
      } else {
        entries.push({ name: relative(repository, path).split(sep).join('/'), ...fileRecord(path) });
      }
    }
  }
  for (const directory of sourceDirectories) visit(join(repository, directory));
  const provider = join(repository, embeddedSource);
  need(realpathSync(provider) === provider, 'The embedded administrator provider cannot contain symbolic-link components.');
  entries.push({ name: embeddedSource, ...fileRecord(provider) });
  return entries.sort((left, right) => left.name < right.name ? -1 : left.name > right.name ? 1 : 0);
}

function separateOutput(path, withPackage = false) {
  const additional = withPackage ? ['deploy/linux', 'scripts', 'go.mod', 'go.sum'].map((name) => join(repository, name)) : [];
  return [bundle, ...sourceDirectories.map((name) => join(repository, name)), ...additional]
    .every((input) => !within(path, input) && !within(input, path));
}

function packageFile(path, maximum) {
  need(realpathSync(path) === path, `Package input has a symbolic-link component: ${path}`);
  const before = lstatSync(path, { bigint: true });
  need(before.isFile() && before.nlink === 1n && before.size > 0n && before.size <= BigInt(maximum),
    `Package input must be one bounded regular file: ${path}`);
  const bytes = readFileSync(path);
  const after = lstatSync(path, { bigint: true });
  const fields = ['dev', 'ino', 'uid', 'gid', 'mode', 'size', 'nlink', 'mtimeNs', 'ctimeNs'];
  need(BigInt(bytes.length) === before.size && fields.every((key) => before[key] === after[key]),
    `Package input changed while being read: ${path}`);
  return { bytes, record: { bytes: bytes.length, sha256: digest(bytes), mode: Number(before.mode & 0o777n),
    identity: Object.fromEntries(fields.map((key) => [key, before[key].toString()])) } };
}

function deploymentSnapshot() {
  return packageSources.map(([name, maximum]) => ({ name, ...packageFile(join(repository, name), maximum).record }));
}

function archiveTool(path, banner) {
  const before = packageFile(path, 16 * 1024 * 1024).record;
  need((before.mode & 0o111) !== 0, `Archive tool is not executable: ${path}`);
  const version = execFileSync(path, ['--version'], {
    env: archiveEnvironment, encoding: 'utf8', timeout: 10000, maxBuffer: 65536,
    stdio: ['ignore', 'pipe', 'pipe'],
  }).trim();
  need(banner.test(version.split('\n')[0]) && JSON.stringify(packageFile(path, 16 * 1024 * 1024).record) === JSON.stringify(before),
    `Archive tool identity or implementation is unsupported: ${path}`);
  return { path, resolvedPath: realpathSync(path), version, ...before };
}

function checkTools(tools) {
  for (const tool of Object.values(tools)) {
    const { path, resolvedPath, version: _version, ...before } = tool;
    need(realpathSync(path) === resolvedPath && JSON.stringify(packageFile(path, 16 * 1024 * 1024).record) === JSON.stringify(before),
      `Archive tool changed: ${path}`);
  }
}

function verifyPackageTar(raw, members) {
  // This accepts only this package's six fixed USTAR headers. It never extracts
  // an archive and rejects extension headers, links and unexpected members.
  need(raw.length <= packageLimit && raw.length % 512 === 0, 'Invalid package tar size.');
  const directory = `${packageName}/`;
  const order = [directory, ...members.map((member) => member.name)];
  const expected = new Map(members.map((member) => [member.name, member]));
  const seen = [];
  let offset = 0;
  function text(field) {
    const zero = field.indexOf(0);
    const end = zero < 0 ? field.length : zero;
    need(field.subarray(0, end).every((byte) => byte >= 32 && byte <= 126)
      && (zero < 0 || field.subarray(zero).every((byte) => byte === 0)), 'Invalid package tar text field.');
    return field.subarray(0, end).toString('ascii');
  }
  function octal(field, allowEmpty = false) {
    need(field.every((byte) => byte === 0 || byte === 32 || (byte >= 48 && byte <= 55)), 'Invalid package tar numeric field.');
    const value = field.toString('ascii').replaceAll('\0', ' ').trim();
    need((allowEmpty && value === '') || /^[0-7]+$/.test(value), 'Invalid package tar octal value.');
    const result = value === '' ? 0 : Number.parseInt(value, 8);
    need(Number.isSafeInteger(result), 'Unsafe package tar number.');
    return result;
  }
  while (offset < raw.length) {
    const header = raw.subarray(offset, offset + 512);
    if (header.every((byte) => byte === 0)) {
      need(raw.length - offset >= 1024 && raw.subarray(offset).every((byte) => byte === 0)
        && JSON.stringify(seen) === JSON.stringify(order), 'Incomplete or noncanonical package tar trailer.');
      return { regularFiles: members.length, directories: 1, bytes: raw.length, sha256: digest(raw) };
    }
    const name = text(header.subarray(0, 100));
    const size = octal(header.subarray(124, 136));
    const mode = octal(header.subarray(100, 108));
    const checksum = header.reduce((sum, byte, index) => sum + (index >= 148 && index < 156 ? 32 : byte), 0);
    need(name === order[seen.length] && !seen.includes(name) && size <= packageLimit
      && octal(header.subarray(148, 156)) === checksum
      && header.subarray(257, 263).equals(Buffer.from('ustar\0')) && header.subarray(263, 265).equals(Buffer.from('00'))
      && text(header.subarray(157, 257)) === '' && text(header.subarray(265, 297)) === ''
      && text(header.subarray(297, 329)) === '' && text(header.subarray(345, 500)) === ''
      && octal(header.subarray(108, 116)) === 0 && octal(header.subarray(116, 124)) === 0
      && octal(header.subarray(136, 148)) === 0 && octal(header.subarray(329, 337), true) === 0
      && octal(header.subarray(337, 345), true) === 0 && header.subarray(500).every((byte) => byte === 0),
    'Package tar header does not match the fixed member contract.');
    const end = offset + 512 + size;
    const next = Math.ceil(end / 512) * 512;
    need(next <= raw.length && raw.subarray(end, next).every((byte) => byte === 0), 'Invalid package tar payload padding.');
    if (name === directory) {
      need(header[156] === 53 && size === 0 && mode === 0o755, 'Invalid package top directory.');
    } else {
      const member = expected.get(name);
      need(member !== undefined && header[156] === 48 && size === member.bytes && mode === member.mode
        && digest(raw.subarray(offset + 512, end)) === member.sha256, 'Package member content or mode changed.');
    }
    seen.push(name);
    offset = next;
  }
  throw new Error('Package tar is missing its complete zero trailer.');
}

function systemdPackage(output, buildManifest, deploymentInputs, assertBuildInputs) {
  need(JSON.stringify(deploymentSnapshot()) === JSON.stringify(deploymentInputs), 'Deployment inputs changed during the build.');
  const tools = { tar: archiveTool('/usr/bin/tar', /^tar \(GNU tar\) /), gzip: archiveTool('/usr/bin/gzip', /^gzip /) };
  const files = [
    ['goby', join(output, 'goby'), 0o755, packageLimit],
    ['manifest.json', join(output, 'manifest.json'), 0o644, 16 * 1024 * 1024],
    ['goby.service', join(repository, 'deploy/linux/goby.service'), 0o644, 64 * 1024],
    ['goby.env.example', join(repository, 'deploy/linux/.env.example'), 0o644, 64 * 1024],
    ['INSTALL.md', join(repository, 'deploy/linux/INSTALL.md'), 0o644, 1024 * 1024],
  ].sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0);
  const top = join(output, packageName);
  mkdirSync(top, { mode: 0o755 });
  chmodSync(top, 0o755);
  const payload = files.map(([name, source, mode, maximum]) => {
    const input = packageFile(source, maximum);
    const target = join(top, name);
    writeFileSync(target, input.bytes, { flag: 'wx', mode });
    chmodSync(target, mode);
    const saved = packageFile(target, maximum).record;
    need(saved.bytes === input.record.bytes && saved.sha256 === input.record.sha256 && saved.mode === mode,
      `Copied package member changed: ${name}`);
    return { name: `${packageName}/${name}`, bytes: saved.bytes, sha256: saved.sha256, mode };
  });
  const paddedPayloadBytes = payload.reduce((total, member) => total + Math.ceil(member.bytes / 512) * 512, 0);
  const expectedTarBytes = Math.ceil((6 * 512 + paddedPayloadBytes + 1024) / 10240) * 10240;
  need(payload.find((member) => member.name === `${packageName}/goby`).sha256 === buildManifest.binary.sha256
    && expectedTarBytes <= packageLimit, 'Package payload exceeds its bound or binary binding.');
  const tarArguments = ['--format=ustar', '--sort=name', '--mtime=@0', '--owner=0', '--group=0', '--numeric-owner',
    '--blocking-factor=20', '-cf', '-', '--', packageName];
  const tar = execFileSync(tools.tar.path, tarArguments, {
    cwd: output, env: archiveEnvironment, timeout: 60000, maxBuffer: packageLimit, stdio: ['ignore', 'pipe', 'pipe'],
  });
  need(tar.length === expectedTarBytes, 'Package tar length differs from its fixed header and padding budget.');
  const tarReadback = verifyPackageTar(tar, payload);
  checkTools(tools);
  const gzipArguments = ['-n', '-c'];
  const compressed = execFileSync(tools.gzip.path, gzipArguments, {
    input: tar, env: archiveEnvironment, timeout: 60000, maxBuffer: packageLimit, stdio: ['pipe', 'pipe', 'pipe'],
  });
  const archiveName = `${packageName}.tar.gz`;
  const archivePath = join(output, archiveName);
  writeFileSync(archivePath, compressed, { flag: 'wx', mode: 0o644 });
  const savedArchive = packageFile(archivePath, packageLimit);
  need(savedArchive.record.sha256 === digest(compressed), 'Written package archive changed.');
  const expanded = gunzipSync(savedArchive.bytes, { maxOutputLength: packageLimit });
  need(expanded.equals(tar) && JSON.stringify(verifyPackageTar(expanded, payload)) === JSON.stringify(tarReadback),
    'Saved package archive failed complete readback.');
  need(JSON.stringify(readdirSync(top).sort()) === JSON.stringify(files.map(([name]) => name)), 'Package staging membership changed.');
  for (const member of payload) {
    const current = packageFile(join(output, member.name), packageLimit).record;
    need(current.sha256 === member.sha256 && current.bytes === member.bytes && current.mode === member.mode,
      `Staged package member changed: ${member.name}`);
  }
  assertBuildInputs();
  need(JSON.stringify(deploymentSnapshot()) === JSON.stringify(deploymentInputs), 'Deployment inputs changed during packaging.');
  checkTools(tools);
  const manifestFile = fileRecord(join(output, 'manifest.json'));
  const binaryFile = fileRecord(join(output, 'goby'));
  need(binaryFile.sha256 === buildManifest.binary.sha256 && binaryFile.bytes === buildManifest.binary.bytes
    && manifestFile.sha256 === payload.find((member) => member.name === `${packageName}/manifest.json`).sha256,
  'Original build outputs changed during packaging.');
  const packageManifest = {
    version: 1, kind: 'goby-linux-systemd-package', target: buildManifest.target,
    distribution: 'internal_candidate_only', projectLicenseResolved: false,
    archive: { name: archiveName, bytes: savedArchive.record.bytes, sha256: savedArchive.record.sha256 },
    topDirectory: packageName, members: payload, directoryMode: 0o755,
    buildManifest: { name: 'manifest.json', ...manifestFile }, deploymentInputs,
    archiveTools: tools, archiveCommands: { tar: tarArguments, gzip: gzipArguments },
    archiveEnvironment, tarReadback, completeReadback: true,
    buildInputsUnchanged: true, deploymentInputsUnchanged: true,
    nativeRuntimeExecuted: false, serviceInstalled: false, ociImageBuilt: false,
    boundary: 'Internal amd64 embedded systemd payload only; no install, upgrade, runtime or distribution acceptance.',
  };
  const name = 'package-manifest.json';
  writeFileSync(join(output, name), `${JSON.stringify(packageManifest, null, 2)}\n`, { flag: 'wx' });
  return { manifest: { name, ...fileRecord(join(output, name)) }, archive: packageManifest.archive };
}

function main() {
  const args = process.argv.slice(2);
  if (args.length === 1 && args[0] === '--help') {
    console.log(usage);
    return;
  }
  const options = {};
  for (let index = 0; index < args.length; index += 2) {
    const key = args[index];
    need((key === '--arch' || key === '--output-dir' || key === '--package') && index + 1 < args.length && !Object.hasOwn(options, key), usage);
    options[key] = args[index + 1];
  }
  const arch = options['--arch'];
  need(arch === 'amd64' || arch === 'arm64', usage);
  const withPackage = Object.hasOwn(options, '--package');
  need(!withPackage || (options['--package'] === 'systemd' && arch === 'amd64'),
    'Only --package systemd with --arch amd64 is supported.');
  need(process.platform === 'linux', 'Build Linux release candidates on Linux; project verification uses ssh test-env.');
  const deploymentInputs = withPackage ? deploymentSnapshot() : null;
  let assets;
  try {
    assets = assetInventory();
  } catch (error) {
    throw new Error(`Administrator bundle is unavailable. Build it first with npm --prefix web/admin run build. ${error.message}`);
  }
  const requestedOutput = resolve(repository, options['--output-dir'] ?? `bin/release-linux-${arch}`);
  need(separateOutput(requestedOutput, withPackage),
    'The release output directory must be separate from administrator assets and local source trees.');
  mkdirSync(dirname(requestedOutput), { recursive: true });
  const output = join(realpathSync(dirname(requestedOutput)), basename(requestedOutput));
  need(separateOutput(output, withPackage), 'The resolved release output must be separate from administrator assets and local source trees.');
  try {
    mkdirSync(output);
  } catch (error) {
    if (error.code === 'EEXIST') throw new Error(`Release output already exists; select a fresh --output-dir: ${output}`);
    throw error;
  }
  const environment = {
    ...process.env,
    GOOS: 'linux',
    GOARCH: arch,
    CGO_ENABLED: '0',
    GOENV: 'off',
    GOFLAGS: '',
    GOPROXY: 'off',
    GONOPROXY: 'none',
    GOTOOLCHAIN: 'local',
    GOWORK: 'off',
  };
  const binary = join(output, 'goby');
  const moduleInputs = Object.fromEntries(['go.mod', 'go.sum'].map((name) => [name, fileRecord(join(repository, name))]));
  const sources = sourceInventory();
  function assertBuildInputs() {
    need(JSON.stringify(assetInventory()) === JSON.stringify(assets),
      'Administrator assets changed during the build; this output is not complete.');
    need(JSON.stringify(sourceInventory()) === JSON.stringify(sources),
      'Local source inputs changed during the build; this output is not complete.');
    need(Object.entries(moduleInputs).every(([name, before]) =>
      JSON.stringify(fileRecord(join(repository, name))) === JSON.stringify(before)),
    'Go module inputs changed during the build; this output is not complete.');
  }
  // A failed build leaves its fresh output directory for inspection. Existing
  // bundles, build caches, installation files and earlier artifacts are kept.
  execFileSync('go', ['build', '-mod=readonly', '-trimpath', '-tags=goby_embed_admin', '-o', binary, './cmd/goby'], {
    cwd: repository, env: environment, stdio: 'inherit',
  });
  assertBuildInputs();
  const metadata = execFileSync('go', ['version', '-m', binary], {
    cwd: repository, env: environment, encoding: 'utf8',
  });
  const manifest = {
    version: 1,
    kind: 'goby-linux-embedded-administrator-build',
    target: { os: 'linux', arch, cgoEnabled: false },
    binary: { name: 'goby', ...fileRecord(binary) },
    buildMetadata: metadata,
    moduleInputs,
    sourceInventory: {
      scope: [...sourceDirectories.map((name) => `${name}/`), embeddedSource],
      files: sources,
      fileCount: sources.length,
      totalBytes: sources.reduce((sum, entry) => sum + entry.bytes, 0),
      sha256: digest(Buffer.from(JSON.stringify(sources), 'utf8')),
      digestEncoding: 'UTF-8 JSON.stringify(files), sorted by repository-relative name.',
      boundary: 'Snapshot of all regular files in the stated local scope, not a compiled dependency graph.',
    },
    administratorAssets: assets.files,
    administratorAssetBytes: assets.files.reduce((sum, entry) => sum + entry.bytes, 0),
    administratorEntryReferences: assets.entryReferences,
    frontendInput: 'Existing web/admin/dist, observed unchanged before and after the Go build.',
    nativeRuntimeExecuted: false,
    ociImageBuilt: false,
  };
  writeFileSync(join(output, 'manifest.json'), `${JSON.stringify(manifest, null, 2)}\n`, { flag: 'wx' });
  const result = { outputDirectory: output, target: manifest.target, binary: manifest.binary,
    administratorAssets: assets.files.length, nativeRuntimeExecuted: false };
  if (withPackage) result.package = systemdPackage(output, manifest, deploymentInputs, assertBuildInputs);
  console.log(JSON.stringify(result));
}

try {
  main();
} catch (error) {
  console.error(`Release build failed: ${error.message}`);
  process.exitCode = 1;
}
