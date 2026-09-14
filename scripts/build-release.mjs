#!/usr/bin/env node
// Build a Linux candidate from the existing production administrator bundle.
// This entry point never installs dependencies, runs Goby, or publishes a release.
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { lstatSync, mkdirSync, readFileSync, readdirSync, realpathSync, writeFileSync } from 'node:fs';
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const repository = realpathSync(fileURLToPath(new URL('../', import.meta.url)));
const bundle = join(repository, 'web', 'admin', 'dist');
const sourceDirectories = ['cmd', 'internal'];
const embeddedSource = 'web/admin/embedded.go';
const usage = 'Usage: node scripts/build-release.mjs --arch amd64|arm64 [--output-dir PATH]';

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

function separateOutput(path) {
  return [bundle, ...sourceDirectories.map((name) => join(repository, name))]
    .every((input) => !within(path, input) && !within(input, path));
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
    need((key === '--arch' || key === '--output-dir') && index + 1 < args.length && !Object.hasOwn(options, key), usage);
    options[key] = args[index + 1];
  }
  const arch = options['--arch'];
  need(arch === 'amd64' || arch === 'arm64', usage);
  need(process.platform === 'linux', 'Build Linux release candidates on Linux; project verification uses ssh test-env.');
  let assets;
  try {
    assets = assetInventory();
  } catch (error) {
    throw new Error(`Administrator bundle is unavailable. Build it first with npm --prefix web/admin run build. ${error.message}`);
  }
  const requestedOutput = resolve(repository, options['--output-dir'] ?? `bin/release-linux-${arch}`);
  need(separateOutput(requestedOutput),
    'The release output directory must be separate from administrator assets and local source trees.');
  mkdirSync(dirname(requestedOutput), { recursive: true });
  const output = join(realpathSync(dirname(requestedOutput)), basename(requestedOutput));
  need(separateOutput(output), 'The resolved release output must be separate from administrator assets and local source trees.');
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
  // A failed build leaves its fresh output directory for inspection. Existing
  // bundles, build caches, installation files and earlier artifacts are kept.
  execFileSync('go', ['build', '-mod=readonly', '-trimpath', '-tags=goby_embed_admin', '-o', binary, './cmd/goby'], {
    cwd: repository, env: environment, stdio: 'inherit',
  });
  need(JSON.stringify(assetInventory()) === JSON.stringify(assets),
    'Administrator assets changed during the build; this output is not complete.');
  need(JSON.stringify(sourceInventory()) === JSON.stringify(sources),
    'Local source inputs changed during the build; this output is not complete.');
  need(Object.entries(moduleInputs).every(([name, before]) =>
    JSON.stringify(fileRecord(join(repository, name))) === JSON.stringify(before)),
  'Go module inputs changed during the build; this output is not complete.');
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
  console.log(JSON.stringify({ outputDirectory: output, target: manifest.target, binary: manifest.binary,
    administratorAssets: assets.files.length, nativeRuntimeExecuted: false }));
}

try {
  main();
} catch (error) {
  console.error(`Release build failed: ${error.message}`);
  process.exitCode = 1;
}
