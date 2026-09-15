import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

// CLI contract tests only. Each child runs a copy of the actual release script
// against test-owned files and an explicitly synthetic Go substitute. No real
// Go compiler, product executable, Vite build, package builder, or service runs.
const releaseSource = fileURLToPath(new URL('./build-release.mjs', import.meta.url));
const binaryBytes = Buffer.from('SYNTHETIC BINARY: NOT A GOBY BUILD\n', 'ascii');
const buildId = '11111111-2222-3333-4444-555555555555';
const legacyFields = ['version', 'kind', 'target', 'binary', 'buildMetadata', 'moduleInputs', 'sourceInventory',
  'administratorAssets', 'administratorAssetBytes', 'administratorEntryReferences', 'frontendInput', 'nativeRuntimeExecuted', 'ociImageBuilt'];

function digest(bytes) { return createHash('sha256').update(bytes).digest('hex'); }

function write(path, bytes, mode = 0o600) {
  mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
  writeFileSync(path, bytes, { mode });
}

const fakeGoSource = String.raw`
const assert = require('node:assert/strict');
const fs = require('node:fs');
const args = process.argv.slice(2);
const binary = process.env.CONTRIBUTIONS_TEST_BINARY;
assert.equal(process.env.GOOS, 'linux');
assert.equal(process.env.GOARCH, 'amd64');
assert.equal(process.env.CGO_ENABLED, '0');
assert.equal(process.env.GOTOOLCHAIN, 'local');
assert.equal(process.env.GOPROXY, 'off');
if (args[0] === 'build') {
  assert.deepEqual(args, ['build', '-mod=readonly', '-trimpath', '-tags=goby_embed_admin', '-o', binary, './cmd/goby']);
  fs.appendFileSync(process.env.CONTRIBUTIONS_TEST_CALLS, JSON.stringify(args) + '\n');
  fs.writeFileSync(binary, 'SYNTHETIC BINARY: NOT A GOBY BUILD\n', { flag: 'wx', mode: 0o700 });
} else if (args[0] === 'version') {
  assert.deepEqual(args, ['version', '-m', binary]);
  fs.appendFileSync(process.env.CONTRIBUTIONS_TEST_CALLS, JSON.stringify(args) + '\n');
  if (process.env.CONTRIBUTIONS_TEST_MUTATE_REPORT === '1') {
    fs.appendFileSync(process.env.CONTRIBUTIONS_TEST_REPORT, ' ');
  }
  process.stdout.write('SYNTHETIC GO METADATA: no Go compilation was performed\n');
} else {
  throw new Error('The synthetic Go substitute received an unsupported command.');
}
`;

const copyMutationHook = String.raw`
const fs = require('node:fs');
const { syncBuiltinESMExports } = require('node:module');
const path = require('node:path');
const output = process.env.CONTRIBUTIONS_TEST_OUTPUT;
const manifest = path.join(output, 'manifest.json');
const sidecar = path.join(output, 'frontend-contributions.json');
const original = fs.writeFileSync;
fs.writeFileSync = function (file, ...args) {
  const result = original.call(this, file, ...args);
  if (file === manifest) {
    // The copy's immediate readback has already passed. Mutate only this owned
    // sidecar after the manifest write to exercise the final consumer guard.
    fs.appendFileSync(sidecar, ' ');
  }
  return result;
};
syncBuiltinESMExports();
`;

function fixture(t, { vendor = false, extension = 'js' } = {}) {
  assert.equal(process.platform, 'linux', 'These Linux release CLI fixtures must run on the remote Linux test host.');
  assert.equal(/\s/.test(process.execPath), false, 'The synthetic Go shebang requires the selected Node executable path without whitespace.');
  const scope = realpathSync(mkdtempSync(join(tmpdir(), 'goby-release-contributions-test-')));
  t.after(() => rmSync(scope, { recursive: true, force: true }));
  const repository = join(scope, 'repository');
  const script = join(repository, 'scripts', 'build-release.mjs');
  const output = join(scope, 'output');
  const binary = join(output, 'goby');
  const reportPath = join(repository, 'web', 'admin', '.artifacts', 'frontend-contributions.json');
  const callsPath = join(scope, 'fake-go-calls.jsonl');
  const fakeBin = join(scope, 'fake-bin');
  const hookPath = join(scope, 'mutate-owned-copy.cjs');
  write(script, readFileSync(releaseSource));
  write(join(repository, 'cmd', 'goby', 'main.go'), 'package main\nfunc main() {}\n');
  write(join(repository, 'internal', 'fixture', 'source.go'), 'package fixture\nconst Marker = "synthetic"\n');
  write(join(repository, 'web', 'admin', 'embedded.go'), 'package admin\n');
  write(join(repository, 'go.mod'), 'module example.test/synthetic\n\ngo 1.22.0\n');
  write(join(repository, 'go.sum'), '');
  write(join(fakeBin, 'go'), `#!${process.execPath}\n${fakeGoSource}`, 0o700);
  write(hookPath, copyMutationHook);
  const assetBytes = new Map([
    ['assets/app.js', Buffer.from('export const synthetic = "\\u2603";\n')],
    ['index.html', Buffer.from('<!doctype html>\n<script type="module" src="/admin/assets/app.js"></script>\n')],
  ]);
  const vendorName = `public/vendor.${extension}`;
  if (vendor) assetBytes.set(vendorName, Buffer.from('/* synthetic copied JavaScript; no contribution map */\n'));
  const assets = [...assetBytes.entries()].sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0)
    .map(([name, bytes]) => {
      write(join(repository, 'web', 'admin', 'dist', name), bytes);
      return { name, bytes: bytes.length, sha256: digest(bytes) };
    });
  const report = {
    kind: 'goby-frontend-contributions', version: 1, phase: 'bundle_written', buildId,
    buildSuccessClaimed: false, requiresSuccessfulBuildReceipt: true,
    assets, chunks: [{ name: 'assets/app.js', modules: [], moduleIds: [] }],
    javascriptWithoutChunkModules: vendor ? [vendorName] : [],
  };
  function saveReport(value = report) {
    write(reportPath, `${JSON.stringify(value, null, 2)}\n`);
  }
  saveReport();
  return { scope, repository, script, output, binary, reportPath, callsPath, vendorName, report, assets, saveReport,
    run({ contributions = true, mutateReport = false, mutateCopy = false } = {}) {
      const arguments_ = [...(mutateCopy ? ['--require', hookPath] : []), script, '--arch', 'amd64', '--output-dir', output];
      if (contributions) arguments_.push('--frontend-contributions', reportPath);
      const result = spawnSync(process.execPath, arguments_, {
        cwd: repository, encoding: 'utf8', timeout: 15_000, maxBuffer: 2 * 1024 * 1024,
        // PATH has no fallback to a real Go installation. Clear Node injection
        // variables; the optional owned preload is an explicit Node CLI flag.
        env: { ...process.env, PATH: fakeBin, NODE_OPTIONS: '', NODE_PATH: '',
          CONTRIBUTIONS_TEST_OUTPUT: output, CONTRIBUTIONS_TEST_BINARY: binary,
          CONTRIBUTIONS_TEST_CALLS: callsPath, CONTRIBUTIONS_TEST_REPORT: reportPath,
          CONTRIBUTIONS_TEST_MUTATE_REPORT: mutateReport ? '1' : '0' },
      });
      assert.equal(result.error, undefined, 'The finite synthetic CLI child must complete without a timeout or spawn error.');
      assert.equal(result.signal, null, 'The synthetic CLI must terminate normally.');
      return result;
    },
    calls() {
      return existsSync(callsPath) ? readFileSync(callsPath, 'utf8').trim().split('\n').filter(Boolean).map((line) => JSON.parse(line)) : [];
    },
    manifest() { return JSON.parse(readFileSync(join(output, 'manifest.json'), 'utf8')); },
  };
}

function assertSyntheticSuccess(f, result) {
  assert.equal(result.status, 0, result.stderr);
  const resultLines = result.stdout.trim().split('\n');
  assert.equal(resultLines.length, 1);
  const summary = JSON.parse(resultLines[0]);
  assert.equal(summary.outputDirectory, f.output);
  assert.equal(summary.nativeRuntimeExecuted, false);
  assert.deepEqual(f.calls(), [
    ['build', '-mod=readonly', '-trimpath', '-tags=goby_embed_admin', '-o', f.binary, './cmd/goby'],
    ['version', '-m', f.binary],
  ]);
  assert.deepEqual(readFileSync(f.binary), binaryBytes);
}

function assertRejected(result, message) {
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Release build failed:/);
  if (message) assert.match(result.stderr, message);
  assert.equal(result.stdout.trim(), '', 'A rejected CLI must not print its final success JSON.');
}

test('explicit contribution input preserves private bytes and the separate frontend success boundary', { timeout: 20_000 }, (t) => {
  const f = fixture(t, { vendor: true, extension: 'MJS' });
  const original = readFileSync(f.reportPath);
  const result = f.run();
  assertSyntheticSuccess(f, result);
  const manifest = f.manifest();
  assert.deepEqual(Object.keys(manifest).sort(), [...legacyFields, 'administratorContributions'].sort());
  const binding = manifest.administratorContributions;
  assert.equal(binding.name, 'frontend-contributions.json');
  assert.equal(binding.bytes, original.length);
  assert.equal(binding.sha256, digest(original));
  assert.equal(binding.buildId, buildId);
  assert.equal(binding.privateSidecar, true);
  assert.equal(binding.includedInSystemdPackage, false);
  assert.equal(binding.requiresSuccessfulFrontendBuildReceipt, true);
  assert.match(binding.boundary, /frontend success and legal completeness require separate evidence/);
  const sidecar = join(f.output, binding.name);
  assert.deepEqual(readFileSync(sidecar), original);
  assert.equal(lstatSync(sidecar).mode & 0o777, 0o600);
  assert.deepEqual(readFileSync(f.reportPath), original);
  assert.deepEqual(manifest.administratorAssets, f.assets);
  assert.equal(manifest.nativeRuntimeExecuted, false);
  assert.equal(manifest.ociImageBuilt, false);
  assert.match(manifest.buildMetadata, /^SYNTHETIC GO METADATA:/);
  assert.equal(JSON.parse(readFileSync(sidecar, 'utf8')).buildSuccessClaimed, false);
  assert.deepEqual(JSON.parse(readFileSync(sidecar, 'utf8')).javascriptWithoutChunkModules, ['public/vendor.MJS']);
  assert.equal(existsSync(join(f.repository, 'web', 'admin', 'dist', binding.name)), false);
});

test('omitting the flag preserves the legacy build manifest shape', { timeout: 20_000 }, (t) => {
  const f = fixture(t);
  // Merely finding an adjacent report must not silently opt a legacy build in.
  f.saveReport({ unsupported: true });
  const result = f.run({ contributions: false });
  assertSyntheticSuccess(f, result);
  assert.deepEqual(Object.keys(f.manifest()).sort(), [...legacyFields].sort());
  assert.equal(Object.hasOwn(f.manifest(), 'administratorContributions'), false);
  assert.equal(existsSync(join(f.output, 'frontend-contributions.json')), false);
});

test('invalid report and JavaScript partitions reject before any fake Go command', { timeout: 120_000 }, async (t) => {
  const cases = [
    { name: 'asset digest mismatch', alter: (report) => { report.assets[0].sha256 = '0'.repeat(64); } },
    { name: 'unsupported report version', alter: (report) => { report.version = 2; } },
    { name: 'duplicate chunk name', alter: (report) => { report.chunks.push({ ...report.chunks[0] }); } },
    { name: 'required unknown-JavaScript field missing', alter: (report) => { delete report.javascriptWithoutChunkModules; } },
    { name: 'chunk also assigned to unknown list', alter: (report) => { report.javascriptWithoutChunkModules.push('assets/app.js'); } },
    { name: 'duplicate unknown JavaScript name', vendor: true, alter: (report) => { report.javascriptWithoutChunkModules.push(report.javascriptWithoutChunkModules[0]); } },
    ...['js', 'mjs', 'cjs'].map((extension) => ({ name: `unlisted ${extension} asset`, vendor: true, extension,
      alter: (report) => { report.javascriptWithoutChunkModules = []; } })),
  ];
  for (const scenario of cases) {
    await t.test(scenario.name, { timeout: 15_000 }, (subtest) => {
      const f = fixture(subtest, { vendor: scenario.vendor, extension: scenario.extension });
      scenario.alter(f.report);
      f.saveReport();
      const result = f.run();
      assertRejected(result);
      assert.deepEqual(f.calls(), []);
      assert.equal(existsSync(f.output), false, 'Report admission failure must precede even the fresh output directory.');
    });
  }
});

test('a report modified by the fake version command cannot produce final success', { timeout: 20_000 }, (t) => {
  const f = fixture(t);
  const original = readFileSync(f.reportPath);
  const result = f.run({ mutateReport: true });
  assertRejected(result, /Frontend contribution report changed during the build/);
  assert.deepEqual(f.calls().map((args) => args[0]), ['build', 'version']);
  assert.deepEqual(readFileSync(f.reportPath), Buffer.concat([original, Buffer.from(' ')]));
  // A failed CLI may retain intermediate outputs for inspection. Their mere
  // existence is not the successful command receipt required by this binding.
  assert.equal(f.manifest().administratorContributions.requiresSuccessfulFrontendBuildReceipt, true);
  assert.deepEqual(readFileSync(join(f.output, 'frontend-contributions.json')), original);
});

test('a retained copy modified after manifest publication fails the final sidecar guard', { timeout: 20_000 }, (t) => {
  const f = fixture(t);
  const original = readFileSync(f.reportPath);
  const result = f.run({ mutateCopy: true });
  assertRejected(result, /Retained frontend contribution sidecar changed during the build/);
  assert.deepEqual(f.calls().map((args) => args[0]), ['build', 'version']);
  assert.deepEqual(readFileSync(f.reportPath), original);
  assert.deepEqual(readFileSync(join(f.output, 'frontend-contributions.json')), Buffer.concat([original, Buffer.from(' ')]));
  const manifest = f.manifest();
  assert.equal(manifest.administratorContributions.sha256, digest(original));
  assert.equal(manifest.administratorContributions.requiresSuccessfulFrontendBuildReceipt, true);
});
