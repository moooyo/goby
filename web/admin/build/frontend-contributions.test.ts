import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, renameSync, rmSync, symlinkSync, unlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, relative, sep } from 'node:path';
import test from 'node:test';
import type { TestContext } from 'node:test';
import type { ResolvedConfig } from 'vite';
import { frontendContributions } from './frontend-contributions.ts';

// These tests call the plugin hooks with a finite synthetic bundle. The package
// entries are empty resolution fixtures: no Vite/Rolldown build is performed,
// and no report here establishes a successful frontend or product build.
type RenderedModule = { renderedLength: number; renderedExports: string[]; code: string | null };
type Chunk = {
  type: 'chunk'; code: string; facadeModuleId: string | null; imports: string[];
  dynamicImports: string[]; moduleIds: string[]; modules: Record<string, RenderedModule>;
};
type Bundle = Record<string, Chunk | { type: 'asset'; source: string | Uint8Array }>;
type ModuleInfo = { importedIds: string[]; dynamicallyImportedIds: string[] };
type HookContext = { getModuleInfo(id: string): ModuleInfo | null };
type Hooks = {
  configResolved(config: ResolvedConfig): void;
  buildStart(): void;
  moduleParsed(info: { id: string; code: string | null }): void;
  writeBundle: { order: string; handler(this: HookContext, options: { dir: string }, bundle: Bundle): void };
};
type Pin = { path: string; realPath: string; bytes: number; sha256: string };
type ModuleFact = {
  id: string; origin: string; renderedLengthUtf16: number; renderedExports: string[];
  parsed: { file: Pin | null; package: { name: string; version: string; manifest: Pin } | null;
    parsedCode: { bytes: number; sha256: string } | null } | null;
  importedIds: string[] | null; dynamicallyImportedIds: string[] | null;
  preFinalRenderCode: { bytes: number; sha256: string } | null;
};
type Report = {
  kind: string; version: number; phase: string; buildId: string; outputDirectory: string;
  buildSuccessClaimed: boolean; requiresSuccessfulBuildReceipt: boolean;
  tools: Record<string, { name: string; version: string; manifest: Pin }>;
  assets: { name: string; bytes: number; sha256: string }[];
  observedInputs: Pin[];
  chunks: { name: string; facadeModuleId: string | null; imports: string[]; dynamicImports: string[];
    moduleIds: string[]; modules: ModuleFact[] }[];
};

const rawSource = 'export const label = "\u03c0\u{1f600}";\n';
const parsedSource = 'export const label = "transformed \u{1f600}";\n';
const renderedSource = 'const label="\u{1f600}";';
const finalChunk = 'const e="\u{1f30f}";export{e as label};\n';
const virtualID = '\0synthetic:generated';
const unknownID = '\0synthetic:unobserved';
const virtualCode = 'export const generated="\u6f22";';

function hash(bytes: string | Uint8Array): string {
  return createHash('sha256').update(bytes).digest('hex');
}

function write(path: string, bytes: string | Uint8Array): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, bytes);
}

function packageFixture(directory: string, name: string, version: string): string {
  const manifest = join(directory, 'package.json');
  write(manifest, JSON.stringify({ name, version, main: 'index.js' }));
  write(join(directory, 'index.js'), '');
  return manifest;
}

function fixture(t: TestContext) {
  // Canonicalize the owned temporary parent so platforms with a tmpdir alias
  // do not accidentally make a normal fixture into a symbolic-link case.
  const scope = realpathSync(mkdtempSync(join(tmpdir(), 'goby-contributions-test-')));
  t.after(() => rmSync(scope, { recursive: true, force: true }));
  const root = join(scope, 'admin');
  const dist = join(root, 'dist');
  const report = join(root, '.artifacts', 'frontend-contributions.json');
  const source = join(root, 'src', 'entry.ts');
  const sourceID = `${source}?synthetic-transform`;
  const lockfile = join(root, 'package-lock.json');
  const lockBytes = JSON.stringify({ name: 'synthetic-admin', lockfileVersion: 3, packages: {} });
  write(join(root, 'package.json'), JSON.stringify({ name: 'synthetic-admin', version: '1.0.0-fixture', type: 'module' }));
  write(lockfile, lockBytes);
  write(source, rawSource);
  const configFile = join(root, 'vite.config.ts');
  const configDependency = join(root, 'build', 'options.ts');
  write(configFile, 'export default {};\n');
  write(configDependency, 'export const synthetic = true;\n');
  const viteDirectory = join(root, 'node_modules', 'vite');
  packageFixture(viteDirectory, 'vite', '1.0.0-fixture');
  const nestedRolldown = packageFixture(join(viteDirectory, 'node_modules', 'rolldown'), 'rolldown', '2.0.0-vite-fixture');
  const rootRolldown = packageFixture(join(root, 'node_modules', 'rolldown'), 'rolldown', '9.0.0-decoy');
  packageFixture(join(root, 'node_modules', '@vitejs', 'plugin-react'), '@vitejs/plugin-react', '3.0.0-fixture');
  const config = {
    root, base: '/admin/', mode: 'production', configFile, configFileDependencies: [configDependency],
    plugins: [{ name: 'synthetic-transform' }, { name: 'goby:frontend-contributions' }],
    build: { outDir: 'dist', watch: null, ssr: false, write: true, sourcemap: false, minify: true, target: ['es2022'] },
  } as unknown as ResolvedConfig;
  const hooks = frontendContributions() as unknown as Hooks;
  hooks.configResolved(config);
  const modules: Record<string, RenderedModule> = {
    [sourceID]: { renderedLength: renderedSource.length, renderedExports: ['label'], code: renderedSource },
    [virtualID]: { renderedLength: virtualCode.length, renderedExports: ['generated'], code: virtualCode },
    [unknownID]: { renderedLength: 17, renderedExports: [], code: null },
  };
  const bundle: Bundle = {
    'index.html': { type: 'asset', source: Buffer.from('<!doctype html><title>Encoding fixture</title>\n') },
    'assets/app.js': { type: 'chunk', code: finalChunk, facadeModuleId: sourceID,
      imports: [], dynamicImports: [], moduleIds: Object.keys(modules), modules },
  };
  for (const [name, output] of Object.entries(bundle)) {
    write(join(dist, name), output.type === 'chunk' ? output.code : output.source);
  }
  const context: HookContext = {
    getModuleInfo(id) {
      if (id === sourceID) return { importedIds: [virtualID], dynamicallyImportedIds: [unknownID] };
      if (id === virtualID) return { importedIds: [], dynamicallyImportedIds: [] };
      return null;
    },
  };
  return { scope, root, dist, report, source, sourceID, lockfile, lockBytes, nestedRolldown, rootRolldown, hooks, bundle, context,
    observe() {
      hooks.buildStart();
      hooks.moduleParsed({ id: sourceID, code: parsedSource });
      hooks.moduleParsed({ id: virtualID, code: virtualCode });
    },
    publish() { hooks.writeBundle.handler.call(context, { dir: dist }, bundle); },
    readReport(): Report { return JSON.parse(readFileSync(report, 'utf8')) as Report; },
  };
}

test('records physical, virtual and unknown contributions separately from final UTF-8 assets', (t) => {
  const f = fixture(t);
  f.observe();
  f.publish();
  const report = f.readReport();
  assert.equal(f.hooks.writeBundle.order, 'post');
  assert.equal(report.kind, 'goby-frontend-contributions');
  assert.equal(report.version, 1);
  assert.equal(report.phase, 'bundle_written');
  assert.equal(report.buildSuccessClaimed, false);
  assert.equal(report.requiresSuccessfulBuildReceipt, true);
  assert.match(report.buildId, /^[0-9a-f-]{36}$/);
  assert.equal(report.outputDirectory, f.dist);
  assert.equal(relative(f.dist, f.report).split(sep)[0], '..');
  assert.equal(existsSync(join(f.dist, 'frontend-contributions.json')), false);

  const assets = new Map(report.assets.map((asset) => [asset.name, asset]));
  assert.deepEqual(report.assets.map((asset) => asset.name), ['assets/app.js', 'index.html']);
  for (const [name, output] of Object.entries(f.bundle)) {
    const actual = readFileSync(join(f.dist, name));
    const emitted = output.type === 'chunk' ? Buffer.from(output.code) : Buffer.from(output.source);
    assert.deepEqual(actual, emitted);
    assert.deepEqual(assets.get(name), { name, bytes: actual.length, sha256: hash(actual) });
  }
  assert.equal(report.chunks.length, 1);
  assert.equal(report.chunks[0].facadeModuleId, f.sourceID);
  const contributions = new Map(report.chunks[0].modules.map((module) => [module.id, module]));
  const physical = contributions.get(f.sourceID)!;
  assert.equal(physical.origin, 'associated_file');
  assert.deepEqual(physical.parsed?.file, { path: f.source, realPath: f.source, bytes: Buffer.byteLength(rawSource), sha256: hash(rawSource) });
  assert.equal(physical.parsed?.package?.name, 'synthetic-admin');
  assert.deepEqual(physical.parsed?.parsedCode, { bytes: Buffer.byteLength(parsedSource), sha256: hash(parsedSource) });
  assert.deepEqual(physical.preFinalRenderCode, { bytes: Buffer.byteLength(renderedSource), sha256: hash(renderedSource) });
  assert.equal(physical.renderedLengthUtf16, renderedSource.length);
  assert.notEqual(physical.renderedLengthUtf16, physical.preFinalRenderCode?.bytes);
  assert.notEqual(physical.preFinalRenderCode?.sha256, assets.get('assets/app.js')?.sha256);
  assert.deepEqual(physical.importedIds, [virtualID]);
  assert.deepEqual(physical.dynamicallyImportedIds, [unknownID]);
  assert.deepEqual(contributions.get(virtualID)?.parsed, { file: null, package: null,
    parsedCode: { bytes: Buffer.byteLength(virtualCode), sha256: hash(virtualCode) } });
  const unknown = contributions.get(unknownID)!;
  assert.equal(unknown.origin, 'virtual_generated_or_unobserved');
  assert.equal(unknown.parsed, null);
  assert.equal(unknown.importedIds, null);
  assert.equal(unknown.dynamicallyImportedIds, null);
  assert.equal(unknown.preFinalRenderCode, null);
  assert.equal(unknown.renderedLengthUtf16, 17);
  assert.equal(report.tools.rolldown.version, '2.0.0-vite-fixture');
  assert.equal(report.tools.rolldown.manifest.path, f.nestedRolldown);
  assert.equal(report.observedInputs.some((pin) => pin.path === f.rootRolldown), false);
  assert.equal(report.observedInputs.some((pin) => pin.path === f.source), true);
  assert.equal(report.assets.some((asset) => asset.name.endsWith('frontend-contributions.json')), false);
  assert.throws(() => f.publish(), /exactly one output directory is supported/);
});

test('rejects changed or missing written assets without publishing a report', async (t) => {
  for (const failure of ['changed', 'missing']) {
    await t.test(failure, (subtest) => {
      const f = fixture(subtest);
      f.observe();
      const asset = join(f.dist, 'assets', 'app.js');
      if (failure === 'changed') writeFileSync(asset, finalChunk.replace('\u{1f30f}', '\u{1f30d}'));
      else unlinkSync(asset);
      assert.throws(() => f.publish(), /written asset differs from the bundler output/);
      assert.equal(existsSync(f.report), false);
    });
  }
});

test('rejects a same-length source mutation after moduleParsed', (t) => {
  const f = fixture(t);
  f.observe();
  const changed = rawSource.replace('\u03c0', '\u03bb');
  assert.equal(Buffer.byteLength(changed), Buffer.byteLength(rawSource));
  writeFileSync(f.source, changed);
  assert.throws(() => f.publish(), /observed input changed/);
  assert.equal(existsSync(f.report), false);
});

test('buildStart removes the previous report before a new input failure', (t) => {
  const f = fixture(t);
  f.observe();
  f.publish();
  const previous = f.readReport();
  assert.equal(existsSync(f.report), true);
  unlinkSync(f.lockfile);
  assert.throws(() => f.hooks.buildStart(), { code: 'ENOENT' });
  assert.equal(existsSync(f.report), false);
  // The deleted prior observation is not a success receipt for the failed run.
  assert.equal(previous.buildSuccessClaimed, false);
  assert.equal(previous.requiresSuccessfulBuildReceipt, true);
});

test('preserves incomplete package identity boundaries and type-only scopes', async (t) => {
  for (const incomplete of [true, false]) {
    await t.test(incomplete ? 'named package without version' : 'type-only subdirectory', (subtest) => {
      const f = fixture(subtest);
      const manifest = join(dirname(f.source), 'package.json');
      write(manifest, JSON.stringify(incomplete ? { name: 'versionless-workspace' } : { type: 'module' }));
      f.observe();
      f.publish();
      const report = f.readReport();
      const source = report.chunks[0].modules.find((module) => module.id === f.sourceID)!;
      if (incomplete) assert.equal(source.parsed?.package, null);
      else assert.equal(source.parsed?.package?.name, 'synthetic-admin');
      assert.equal(report.observedInputs.some((pin) => pin.path === manifest), true);
    });
  }
});

test('rejects symbolic-link output and sidecar paths without writing through them', async (t) => {
  // These real symbolic-link fixtures are intended for the Linux test host.
  // No symlink is followed for cleanup outside this test-owned temporary scope.
  await t.test('output directory replaced after buildStart', (subtest) => {
    const f = fixture(subtest);
    f.observe();
    const moved = join(f.scope, 'moved-assets');
    renameSync(f.dist, moved);
    symlinkSync(moved, f.dist, 'dir');
    assert.throws(() => f.publish(), /asset directories cannot contain symbolic links/);
    assert.equal(existsSync(f.report), false);
  });
  await t.test('output file points outside the asset directory', (subtest) => {
    const f = fixture(subtest);
    f.observe();
    const outside = join(f.scope, 'external-asset.js');
    writeFileSync(outside, finalChunk);
    const asset = join(f.dist, 'assets', 'app.js');
    unlinkSync(asset);
    symlinkSync(outside, asset, 'file');
    assert.throws(() => f.publish(), /asset entries cannot be symbolic links/);
    assert.equal(readFileSync(outside, 'utf8'), finalChunk);
    assert.equal(existsSync(f.report), false);
  });
  await t.test('sidecar file already links to an external record', (subtest) => {
    const f = fixture(subtest);
    const outside = join(f.scope, 'external-report.json');
    writeFileSync(outside, 'external sentinel\n');
    mkdirSync(dirname(f.report));
    symlinkSync(outside, f.report, 'file');
    assert.throws(() => f.hooks.buildStart(), /the old report must be a regular file/);
    assert.equal(readFileSync(outside, 'utf8'), 'external sentinel\n');
  });
  await t.test('sidecar parent replaced between buildStart and publication', (subtest) => {
    const f = fixture(subtest);
    f.observe();
    const outside = join(f.scope, 'external-private');
    renameSync(dirname(f.report), outside);
    symlinkSync(outside, dirname(f.report), 'dir');
    // Publication must recheck this boundary; checking only at buildStart
    // would create a valid-looking observation in a replaced external parent.
    assert.throws(() => f.publish(), /Frontend contributions:/);
    assert.equal(existsSync(join(outside, 'frontend-contributions.json')), false);
  });
});
