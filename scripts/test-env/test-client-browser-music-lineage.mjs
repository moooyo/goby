#!/usr/bin/env node
/** Read-only loader guards for an explicitly selected single or bounded Music upgrade chain. */

import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { TextDecoder } from 'node:util';
import vm from 'node:vm';

const ROOT = '/opt/goby-test/exec-work-m3e';
const SELF = fileURLToPath(import.meta.url);
const LOADER = fileURLToPath(new URL('./client-browser-goby-fixture.mjs', import.meta.url));
const CORE_FLAGS = ['candidate-sha256', 'music-scan-receipt-sha256', 'output'];
const SINGLE_FLAGS = ['music-upgrade-directory', 'music-upgrade-completed-sha256', 'music-upgrade-report', 'music-upgrade-report-sha256'];
const CHAIN_FLAGS = ['music-upgrade-chain', 'music-upgrade-chain-sha256'];
const FLAGS = [...CORE_FLAGS, ...SINGLE_FLAGS, ...CHAIN_FLAGS];
const CHAIN_PATH = new RegExp(`^${ROOT}/client-music-upgrade-chain-[A-Za-z0-9_-]{1,64}\\.json$`);
const digest = value => typeof value === 'string' && /^[0-9a-f]{64}$/.test(value);
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const hash = value => createHash('sha256').update(value).digest('hex');
const stable = value => Array.isArray(value) ? value.map(stable) : record(value)
  ? Object.fromEntries(Object.keys(value).sort().map(key => [key, stable(value[key])])) : value;
const same = (left, right) => JSON.stringify(stable(left)) === JSON.stringify(stable(right));
const wrongHash = value => (value[0] === '0' ? '1' : '0') + value.slice(1);
const LEGACY_OPERATOR_SHA = '85b1c7ac16646d0af19cd67d649b72f3df6cb7e08eab4357b94d5b6e6ab818de';
const LEGACY_OPERATOR_PATH = `${ROOT}/source-attempt-20/scripts/test-env/prepare-client-fixture.py`;
// This schema26 operator pin was independently frozen.
const SCHEMA_26_OPERATOR_SHA = 'df9f0d33b9ff3136a6958a91d855faf47d703e7f5368a953cb0d182aa12bbd3b';
const SCHEMA_26_OPERATOR_PROOF = `${ROOT}/client-schema26-retained-tool-01/prepare-client-fixture.py`;
const SCHEMA_27_OPERATOR_PROOF = `${ROOT}/client-schema27-tool-01/prepare-client-fixture.py`;
const SCHEMA_27_OPERATOR_SHA = '84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c';
const CATALOG_25_SHA = 'e269a7eb6b31d2eb3fff734896ca074a6113761f321f2f23f4b07441e7dc617b';
const CATALOG_26_SHA = 'e02c46a49dd4200bb67954f70ca0bbe90b3ef99d8821ea56a97e3dcb97e696de';
const CATALOG_27_SHA = '1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d';
const MIGRATION_24_SHA = '2a9c159f111073a1c01bbbda158cfe3d98d25f417d70c7eda14b70a7798a3972';
const MIGRATION_25_SHA = '3b21cac79173b4b9184d73b76066aff4199a4d6d4ad6fc64d2d0d826796b6eaf';
const MIGRATION_26_SHA = 'c4b7485174fe655524fe5de6973f9d75a6ce7917f4ebd82476ac1c13be5817e8';
const MIGRATION_27_SHA = 'b62d0422dddb9e258f46589898f672b08fc6e4c12fea7456059f855a6600353c';
const LEGACY_HOP_METHOD = 'Explicit single completed schema25 binary upgrade';
const SCHEMA_26_HOP_METHOD = 'Explicit completed schema26 fixture upgrade';
const SCHEMA_27_HOP_METHOD = 'Explicit completed schema27 fixture upgrade';
const LEGACY_CHAIN_METHOD = 'Explicit bounded completed schema25 upgrade chain';
const MIXED_CHAIN_METHOD = 'Explicit bounded completed schema25/schema26 upgrade chain';
const SCHEMA_27_CHAIN_METHOD = 'Explicit bounded completed schema25/schema26/schema27 upgrade chain';
const SCHEMA_25_TABLES = ['activity_entries', 'application_key_clients', 'application_key_devices', 'application_keys',
  'catalog_entities', 'client_playback_references', 'devices', 'encoding_jobs', 'item_entities', 'item_images',
  'item_metadata_state', 'item_subtitles', 'items', 'libraries', 'library_roots', 'managed_settings', 'play_sessions',
  'scan_jobs', 'schema_migrations', 'server_settings', 'sessions', 'task_definitions', 'task_occurrences',
  'task_run_children', 'task_run_requests', 'task_runs', 'task_triggers', 'user_item_data', 'user_settings', 'users'];
const THEME_TABLES = ['item_theme_resources', 'theme_owner_ids', 'theme_reserved_paths'];
const EXTRA_TABLES = ['extra_reserved_paths', 'item_extra_resources'];
const SUPPORT_27 = { path: `${ROOT}/backup-schema27-tool-01`, guard_count: 108, files: {
  'run-client-backup-tests.py': '7cf91591efaeca841907cfe1950d0bfcb6c33ef091a17fe54c69db167a74c22a',
  'guards.stdout': '9ecb9d07c61c61b7510c8ed16e895ad2c146d0244920914906663d1d40703ea9',
  'guards.stderr': '990ab0a20e53bd44e889ebd85482fb5bf15631275d8b187e056c324fd560f09c',
} };
const THEME_COUNTS = ['owner_count', 'virtual_root_count', 'mapped_item_count', 'reserved_path_count',
  'resource_count', 'active_resource_count'];
const THEME_HASHES = ['owner_rows_sha256', 'reserved_paths_sha256', 'resource_rows_sha256'];

function requireThat(value) {
  if (!value) throw new Error('music_lineage_guard_failed');
}

function parseArguments(argv) {
  requireThat(argv.length > 0 && argv.length % 2 === 0);
  const parsed = {};
  for (let index = 0; index < argv.length; index += 2) {
    const name = argv[index]?.slice(2), value = argv[index + 1];
    requireThat(argv[index]?.startsWith('--') && FLAGS.includes(name) && !Object.hasOwn(parsed, name) && typeof value === 'string' && value.length > 0);
    parsed[name] = value;
  }
  selectionMode(parsed);
  return parsed;
}

function selectionMode(options) {
  requireThat(record(options) && Object.keys(options).every(name => FLAGS.includes(name)) &&
    CORE_FLAGS.every(name => Object.hasOwn(options, name) && typeof options[name] === 'string' && options[name].length > 0));
  const single = SINGLE_FLAGS.some(name => Object.hasOwn(options, name));
  const chain = CHAIN_FLAGS.some(name => Object.hasOwn(options, name));
  requireThat(single !== chain);
  requireThat((chain ? CHAIN_FLAGS : SINGLE_FLAGS).every(name => Object.hasOwn(options, name) &&
    typeof options[name] === 'string' && options[name].length > 0));
  return chain ? 'chain' : 'single';
}

function validUpgradeEntry(entry) {
  return record(entry) && same(Object.keys(entry).sort(), ['completedSHA256', 'directory', 'reportPath', 'reportSHA256']) &&
    digest(entry.completedSHA256) && digest(entry.reportSHA256) &&
    typeof entry.directory === 'string' && new RegExp(`^${ROOT}/client-upgrade-[0-9a-f]{32}$`).test(entry.directory) &&
    typeof entry.reportPath === 'string' && new RegExp(`^${ROOT}/client-fixture-report-[0-9a-f]{24}\\.json$`).test(entry.reportPath);
}

async function readAuthorizedChain(filename, expectedSHA256) {
  const limit = 2 * 1024 * 1024;
  requireThat(CHAIN_PATH.test(filename) && digest(expectedSHA256) && await fs.realpath(filename) === filename);
  const before = await fs.lstat(filename, { bigint: true });
  requireThat(before.isFile() && !before.isSymbolicLink() && before.uid === 0n && before.nlink === 1n &&
    (before.mode & 0o777n) === 0o600n && before.size > 0n && before.size <= BigInt(limit));
  const unchanged = after => ['dev', 'ino', 'uid', 'gid', 'mode', 'nlink', 'size', 'mtimeNs', 'ctimeNs'].every(key => before[key] === after[key]);
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  const bytes = Buffer.alloc(limit + 1);
  try {
    requireThat(unchanged(await handle.stat({ bigint: true })));
    let size = 0;
    while (size < bytes.length) {
      const read = await handle.read(bytes, size, bytes.length - size, size);
      if (!read.bytesRead) break;
      size += read.bytesRead;
    }
    requireThat(size === Number(before.size) && size <= limit && unchanged(await handle.stat({ bigint: true })) &&
      unchanged(await fs.lstat(filename, { bigint: true })));
    const raw = bytes.subarray(0, size);
    // Approved byte identity is checked before any JSON values are interpreted.
    requireThat(hash(raw) === expectedSHA256);
    const entries = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(raw));
    requireThat(Array.isArray(entries) && entries.length >= 1 && entries.length <= 8 && entries.every(validUpgradeEntry));
    return entries;
  } finally { bytes.fill(0); await handle.close(); }
}

async function privateDirectory(filename) {
  const info = await fs.lstat(filename);
  requireThat(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0 && (info.mode & 0o777) === 0o700 &&
    await fs.realpath(filename) === filename);
}

async function reserveReport(output) {
  requireThat(typeof output === 'string' && path.isAbsolute(output) && path.resolve(output) === output &&
    path.dirname(output) === ROOT && /^[A-Za-z0-9][A-Za-z0-9_.-]{0,95}$/.test(path.basename(output)));
  await privateDirectory(ROOT);
  let filename = output;
  if (!output.endsWith('.json')) {
    await fs.mkdir(output, { mode: 0o700 });
    await privateDirectory(output);
    filename = path.join(output, 'music-lineage-guards.json');
  }
  return fs.open(filename, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
}

async function sourceHashes() {
  const result = {};
  for (const filename of [SELF, LOADER]) {
    const info = await fs.lstat(filename);
    requireThat(info.isFile() && !info.isSymbolicLink() && info.uid === 0 && info.nlink === 1 && info.size > 0 && info.size <= 2 * 1024 * 1024);
    const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const opened = await handle.stat();
      requireThat(opened.dev === info.dev && opened.ino === info.ino && opened.size === info.size);
      const bytes = await handle.readFile();
      requireThat(bytes.length === info.size);
      result[path.basename(filename)] = hash(bytes);
    } finally { await handle.close(); }
  }
  return result;
}

function validProcess(value) {
  return record(value) && Number.isSafeInteger(value.pid) && value.pid > 1 && Number.isSafeInteger(value.start_ticks) &&
    value.start_ticks > 0 && typeof value.boot_id === 'string' && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(value.boot_id);
}

function validHopOperator(hop) {
  if (!record(hop)) return false;
  if (hop.from_schema === 25 && hop.to_schema === 25) return hop.method === LEGACY_HOP_METHOD &&
    hop.operator_path === LEGACY_OPERATOR_PATH && hop.operator_sha256 === LEGACY_OPERATOR_SHA && hop.operator_proof_path === LEGACY_OPERATOR_PATH;
  if ([25, 26].includes(hop.from_schema) && hop.to_schema === 26) return hop.method === SCHEMA_26_HOP_METHOD &&
    digest(SCHEMA_26_OPERATOR_SHA) && hop.operator_path === `${ROOT}/prepare-client-fixture.py` &&
    hop.operator_sha256 === SCHEMA_26_OPERATOR_SHA && hop.operator_proof_path === SCHEMA_26_OPERATOR_PROOF;
  return [26, 27].includes(hop.from_schema) && hop.to_schema === 27 && hop.method === SCHEMA_27_HOP_METHOD &&
    digest(SCHEMA_27_OPERATOR_SHA) && hop.operator_path === `${ROOT}/prepare-client-fixture.py` &&
    hop.operator_sha256 === SCHEMA_27_OPERATOR_SHA && hop.operator_proof_path === SCHEMA_27_OPERATOR_PROOF;
}

function pureLoaderCore(loaderSource, unfrozenOperator = false) {
  requireThat(typeof loaderSource === 'string' && loaderSource.length > 0 && loaderSource.length <= 2 * 1024 * 1024);
  let source = loaderSource;
  for (const statement of ["import fs from 'node:fs/promises';", "import { constants } from 'node:fs';",
    "import path from 'node:path';", "import { createHash } from 'node:crypto';", "import { TextDecoder } from 'node:util';"]) {
    requireThat(source.split(statement).length === 2);
    source = source.replace(statement, '');
  }
  const exported = 'export async function loadGobyAVFixture(';
  requireThat(source.split(exported).length === 2);
  source = source.replace(exported, 'async function loadGobyAVFixture(');
  requireThat(!/^\s*(?:import|export)\b/m.test(source) && !/\bimport\s*\(/.test(source));
  if (unfrozenOperator) {
    const schema = unfrozenOperator === 27 ? 27 : 26;
    const declaration = `const SCHEMA_${schema}_OPERATOR_SHA = '${schema === 26 ? SCHEMA_26_OPERATOR_SHA : SCHEMA_27_OPERATOR_SHA}';`;
    requireThat(source.split(declaration).length === 2);
    source = source.replace(declaration, `const SCHEMA_${schema}_OPERATOR_SHA = null;`);
  }
  let effects = 0;
  const sandbox = Object.create(null);
  for (const name of ['fs', 'constants', 'path', 'createHash', 'TextDecoder', 'process', 'Buffer', 'fetch', 'require',
    'console', 'setTimeout', 'setInterval', 'setImmediate', 'queueMicrotask', 'WebSocket', 'XMLHttpRequest']) {
    Object.defineProperty(sandbox, name, { get() { effects += 1; throw new Error('pure_guard_external_effect'); } });
  }
  const context = vm.createContext(sandbox, { codeGeneration: { strings: false, wasm: false } });
  new vm.Script(source + '\nglobalThis.__musicCore = Object.freeze({schemaBinding, musicPreservationBinding, upgradeOperatorBinding, operatorProofBinding, uniqueOperatorBindings});',
    { filename: 'client-browser-goby-fixture.pure.mjs' }).runInContext(context, { timeout: 1000 });
  requireThat(effects === 0);
  return {
    invoke(name, ...args) {
      requireThat(['schemaBinding', 'musicPreservationBinding', 'upgradeOperatorBinding', 'operatorProofBinding', 'uniqueOperatorBindings'].includes(name));
      const input = JSON.stringify(args);
      sandbox.__musicArgs = input;
      try {
        const raw = new vm.Script(`(() => { const args = JSON.parse(__musicArgs); const result = __musicCore.${name}(...args); ` +
          'return JSON.stringify({result, args}); })()').runInContext(context, { timeout: 1000 });
        const output = JSON.parse(raw);
        requireThat(effects === 0 && same(output.args, JSON.parse(input)));
        return output.result;
      } finally { delete sandbox.__musicArgs; }
    },
    get effects() { return effects; },
  };
}

const clone = value => JSON.parse(JSON.stringify(value));
const syntheticHash = value => hash(`synthetic-music-lineage-guard:${value}`);

function syntheticSource(schema) {
  return { marker: `goby-client-schema${schema}-source-m3e-v1`, schema,
    source: `${ROOT}/source-attempt-${schema === 25 ? 9001 : schema === 26 ? 9002 : 9003}`,
    source_manifest_sha256: syntheticHash(`manifest-${schema}`),
    catalog_sha256: ({ 25: CATALOG_25_SHA, 26: CATALOG_26_SHA, 27: CATALOG_27_SHA })[schema],
    [`migration_${schema}_sha256`]: ({ 25: MIGRATION_25_SHA, 26: MIGRATION_26_SHA, 27: MIGRATION_27_SHA })[schema] };
}

function syntheticState(schema, fromSchema = schema === 27 ? 26 : 25) {
  const binding = syntheticSource(schema);
  const state = { schema, binary_sha256: syntheticHash(`binary-${schema}`),
    process: { pid: 900000 + schema, start_ticks: 9000000 + schema, boot_id: '00000000-0000-0000-0000-000000000001' },
    runtime_sha256: syntheticHash('runtime'), browser_sha256: syntheticHash('browser'), schema25_source: syntheticSource(25) };
  const artifacts = { source: binding.source, source_manifest_sha256: binding.source_manifest_sha256,
    catalog_sha256: binding.catalog_sha256, migration_count: schema, migration_24_sha256: MIGRATION_24_SHA,
    [`schema${schema}_binding`]: clone(binding) };
  if (schema >= 26) {
    state.schema26_source = syntheticSource(26);
    if (schema === 27) state.schema27_source = clone(binding);
    artifacts.migration_25_sha256 = MIGRATION_25_SHA;
    artifacts.operator = { path: `${ROOT}/prepare-client-fixture.py`,
      sha256: schema === 26 ? SCHEMA_26_OPERATOR_SHA : SCHEMA_27_OPERATOR_SHA };
    artifacts.product_verification = { report_path: `${ROOT}/client-backup-run-20990101_000000_000000000001/report.json`,
      report_sha256: syntheticHash('product-report') };
    if (schema === 27) {
      artifacts.migration_26_sha256 = MIGRATION_26_SHA;
      artifacts.product_verification.schema27_support = clone(SUPPORT_27);
    }
  }
  state.upgrade = { phase: 'complete', from_schema: fromSchema, to_schema: schema,
    to_sha256: state.binary_sha256, new_process: clone(state.process), schema_artifacts: artifacts };
  return state;
}

function syntheticSummary(schema) {
  const rowCounts = Object.fromEntries(SCHEMA_25_TABLES.map(name => [name, 0]));
  Object.assign(rowCounts, { schema_migrations: schema, items: 6, libraries: 3, library_roots: 3, users: 3 });
  const summary = { schema, database_sha256: syntheticHash(`database-${schema}`), recovery_sha256: syntheticHash('recovery'),
    runtime_sha256: syntheticHash('runtime'), browser_sha256: syntheticHash('browser'), table_count: schema === 25 ? 30 : schema === 26 ? 33 : 35,
    row_counts: rowCounts, item_count: 6, library_count: 3,
    added_viewer_credentials: Object.fromEntries(['receipt_sha256', 'browser_sha256', 'credentials_sha256']
      .map(name => [name, syntheticHash(`viewer-${name}`)])) };
  if (schema >= 26) {
    Object.assign(rowCounts, { theme_owner_ids: 7, theme_reserved_paths: 2, item_theme_resources: 0 });
    summary.theme = { owner_count: 7, virtual_root_count: 1, mapped_item_count: 6, reserved_path_count: 2,
      resource_count: 0, active_resource_count: 0,
      ...Object.fromEntries(THEME_HASHES.map(name => [name, syntheticHash(`theme-${name}`)])) };
  }
  if (schema === 27) {
    summary.extras = Object.fromEntries(EXTRA_TABLES.map(name => [name, 0]));
    Object.assign(rowCounts, summary.extras);
  }
  return summary;
}

function syntheticHop(from, to) {
  const before = syntheticState(from, from === 25 ? 24 : from === 26 ? 25 : 26), state = syntheticState(to, from);
  const completed = clone(state.upgrade);
  completed.preservation = syntheticSummary(from);
  completed.after_preservation = syntheticSummary(to);
  // Capture times are included in database hashes, so equality is not required.
  completed.after_preservation.database_sha256 = syntheticHash(`after-database-${to}`);
  return { before, state, completed };
}

/** Test the actual loader predicates using synthetic inputs and no external effects. */
export function runPureMusicLineageGuards(loaderSource) {
  const report = { format: 1, mode: 'pure', result: 'blocked', harness_guards_only: true, client_acceptance: false,
    boundary: 'Synthetic VM inputs only; no fixture files, process inspection, network, database, or mutation.', tests: [] };
  const cases = [];
  const test = (name, action) => cases.push({ name, action });
  const core = pureLoaderCore(loaderSource);
  const rejected = (activeCore, name, ...args) => {
    let rejectedByBinding = false;
    try { activeCore.invoke(name, ...args); }
    catch (error) { rejectedByBinding = error?.message === 'fixture_binding_mismatch'; }
    requireThat(rejectedByBinding && activeCore.effects === 0);
  };
  const bindSchema = state => core.invoke('schemaBinding', state, state.binary_sha256);
  const preserve = fixture => core.invoke('musicPreservationBinding', fixture.before, fixture.state, fixture.completed);
  const schemaNegative = (name, mutate, schema = 26) => test(name, () => {
    const state = syntheticState(schema); mutate(state);
    rejected(core, 'schemaBinding', state, state.binary_sha256);
  });
  const preservationNegative = (name, mutate, from = 25, to = 26) => test(name, () => {
    const fixture = syntheticHop(from, to); mutate(fixture);
    rejected(core, 'musicPreservationBinding', fixture.before, fixture.state, fixture.completed);
  });
  test('independently_frozen_schema26_operator_pin', () => requireThat(digest(SCHEMA_26_OPERATOR_SHA) &&
    SCHEMA_26_OPERATOR_SHA !== LEGACY_OPERATOR_SHA));
  test('legacy_schema24_binding', () => requireThat(same(core.invoke('schemaBinding', { schema: 24 }, syntheticHash('legacy')),
    { schema: 24, binding: 'existing_receipted_fixture' })));
  for (const [from, to] of [[24, 25], [25, 25], [25, 26], [26, 26]]) test(`schema_${from}_to_${to}`, () => {
    const state = syntheticState(to, from), binding = syntheticSource(to);
    requireThat(same(bindSchema(state), { schema: to, binding: `completed_upgrade_and_pinned_schema${to}_artifacts`,
      catalog_sha256: binding.catalog_sha256, migration_sha256: binding[`migration_${to}_sha256`],
      source: binding.source, source_manifest_sha256: binding.source_manifest_sha256 }));
  });
  for (const [from, to] of [[25, 25], [25, 26], [26, 26]]) {
    test(`preservation_${from}_to_${to}`, () => requireThat(preserve(syntheticHop(from, to)) === true));
    test(`operator_${from}_to_${to}`, () => {
      const completed = syntheticState(to, from).upgrade;
      requireThat(same(core.invoke('upgradeOperatorBinding', completed), to === 25
        ? { path: LEGACY_OPERATOR_PATH, sha256: LEGACY_OPERATOR_SHA }
        : { path: `${ROOT}/prepare-client-fixture.py`, sha256: SCHEMA_26_OPERATOR_SHA }));
    });
  }
  test('schema26_unchanged_existing_resources', () => {
    const fixture = syntheticHop(26, 26);
    for (const summary of [fixture.completed.preservation, fixture.completed.after_preservation]) {
      summary.row_counts.item_theme_resources = 2;
      summary.theme.resource_count = 2;
      summary.theme.active_resource_count = 1;
    }
    requireThat(preserve(fixture) === true);
  });
  test('unfrozen_schema26_pin_rejects_operator_and_schema', () => {
    const unfrozen = pureLoaderCore(loaderSource, true), state = syntheticState(26);
    rejected(unfrozen, 'upgradeOperatorBinding', state.upgrade);
    rejected(unfrozen, 'schemaBinding', state, state.binary_sha256);
    requireThat(same(unfrozen.invoke('upgradeOperatorBinding', syntheticState(25).upgrade),
      { path: LEGACY_OPERATOR_PATH, sha256: LEGACY_OPERATOR_SHA }));
  });
  for (const schema of [23, 28, '26', null]) schemaNegative(`unsupported_state_schema_${schema}`, state => { state.schema = schema; });
  for (const [name, mutate] of [
    ['incomplete_upgrade', state => { state.upgrade.phase = 'pending'; }],
    ['schema24_to26', state => { state.upgrade.from_schema = 24; }],
    ['to_schema_disagrees', state => { state.upgrade.to_schema = 25; }],
    ['candidate_disagrees', state => { state.upgrade.to_sha256 = syntheticHash('wrong-candidate'); }],
    ['process_disagrees', state => { state.upgrade.new_process.pid += 1; }],
    ['wrong_migration_count', state => { state.upgrade.schema_artifacts.migration_count = 25; }],
    ['string_migration_count', state => { state.upgrade.schema_artifacts.migration_count = '26'; }],
    ['old_catalog_relabelled', state => { state.upgrade.schema_artifacts.catalog_sha256 = CATALOG_25_SHA; }],
    ['migration24_changed', state => { state.upgrade.schema_artifacts.migration_24_sha256 = syntheticHash('wrong-migration24'); }],
    ['migration25_changed', state => { state.upgrade.schema_artifacts.migration_25_sha256 = syntheticHash('wrong-migration25'); }],
    ['extra_artifact', state => { state.upgrade.schema_artifacts.extra = true; }],
    ['missing_schema25_source', state => { delete state.schema25_source; }],
    ['changed_schema25_catalog', state => { state.schema25_source.catalog_sha256 = CATALOG_26_SHA; }],
    ['extra_schema25_source_key', state => { state.schema25_source.extra = true; }],
    ['state_source_disagrees', state => { state.schema26_source.source_manifest_sha256 = syntheticHash('wrong-state-source'); }],
    ['artifact_source_disagrees', state => { state.upgrade.schema_artifacts.source = `${ROOT}/source-attempt-9003`; }],
    ['artifact_manifest_disagrees', state => { state.upgrade.schema_artifacts.source_manifest_sha256 = syntheticHash('wrong-manifest'); }],
    ['product_report_escape', state => { state.upgrade.schema_artifacts.product_verification.report_path = '/tmp/report.json'; }],
    ['product_report_wrong_kind', state => { state.upgrade.schema_artifacts.product_verification.report_path = `${ROOT}/client-fixture-report-${'a'.repeat(24)}.json`; }],
    ['product_report_digest_invalid', state => { state.upgrade.schema_artifacts.product_verification.report_sha256 = 'invalid'; }],
    ['extra_product_key', state => { state.upgrade.schema_artifacts.product_verification.extra = true; }],
  ]) schemaNegative(`schema26_${name}`, mutate);
  for (const key of Object.keys(syntheticState(26).upgrade.schema_artifacts)) {
    schemaNegative(`schema26_missing_artifact_${key}`, state => { delete state.upgrade.schema_artifacts[key]; });
  }
  for (const key of Object.keys(syntheticSource(26))) {
    schemaNegative(`schema26_missing_source_${key}`, state => {
      delete state.upgrade.schema_artifacts.schema26_binding[key];
      state.schema26_source = clone(state.upgrade.schema_artifacts.schema26_binding);
    });
  }
  for (const [name, mutate] of [
    ['source_escape', binding => { binding.source = `${ROOT}/source-attempt-9002/..`; }],
    ['source_zero_attempt', binding => { binding.source = `${ROOT}/source-attempt-0`; }],
    ['catalog_bootstrap_source21', binding => { binding.source = `${ROOT}/source-attempt-21`; }],
    ['database_only_source22', binding => { binding.source = `${ROOT}/source-attempt-22`; }],
    ['source_marker_relabelled', binding => { binding.marker = 'goby-client-schema25-source-m3e-v1'; }],
    ['source_schema_relabelled', binding => { binding.schema = 25; }],
    ['source_catalog_relabelled', binding => { binding.catalog_sha256 = CATALOG_25_SHA; }],
    ['source_migration_relabelled', binding => { binding.migration_26_sha256 = MIGRATION_25_SHA; }],
    ['source_digest_invalid', binding => { binding.source_manifest_sha256 = 'invalid'; }],
    ['extra_source_key', binding => { binding.extra = true; }],
  ]) schemaNegative(`schema26_${name}`, state => {
    const artifacts = state.upgrade.schema_artifacts, binding = artifacts.schema26_binding;
    mutate(binding);
    state.schema26_source = clone(binding);
    artifacts.source = binding.source;
    artifacts.source_manifest_sha256 = binding.source_manifest_sha256;
  });
  for (const key of ['report_path', 'report_sha256']) {
    schemaNegative(`schema26_missing_product_${key}`, state => { delete state.upgrade.schema_artifacts.product_verification[key]; });
  }
  for (const key of ['catalog_sha256', 'migration_25_sha256']) schemaNegative(`schema25_wrong_${key}`,
    state => { state.upgrade.schema_artifacts.schema25_binding[key] = syntheticHash(`wrong-${key}`); }, 25);
  schemaNegative('schema25_wrong_table_generation', state => { state.upgrade.schema_artifacts.migration_count = 26; }, 25);
  for (const [name, mutate] of [
    ['legacy_sha_relabelled', operator => { operator.sha256 = LEGACY_OPERATOR_SHA; }],
    ['unknown_sha', operator => { operator.sha256 = syntheticHash('unknown-operator'); }],
    ['legacy_path_relabelled', operator => { operator.path = LEGACY_OPERATOR_PATH; }],
    ['outside_path', operator => { operator.path = '/tmp/prepare-client-fixture.py'; }],
    ['extra_key', operator => { operator.extra = true; }],
    ['missing_path', operator => { delete operator.path; }],
    ['missing_sha', operator => { delete operator.sha256; }],
  ]) test(`operator26_${name}`, () => {
    const state = syntheticState(26); mutate(state.upgrade.schema_artifacts.operator);
    rejected(core, 'upgradeOperatorBinding', state.upgrade);
    rejected(core, 'schemaBinding', state, state.binary_sha256);
  });
  test('legacy_operator_cannot_acquire_schema26_artifact', () => {
    const completed = syntheticState(25).upgrade;
    completed.schema_artifacts.operator = { path: `${ROOT}/prepare-client-fixture.py`, sha256: SCHEMA_26_OPERATOR_SHA };
    rejected(core, 'upgradeOperatorBinding', completed);
  });
  for (const [from, to] of [[24, 25], [24, 26], [26, 25], [25, 27]]) test(`operator_unsupported_${from}_to_${to}`, () => {
    const completed = syntheticState(26).upgrade; completed.from_schema = from; completed.to_schema = to;
    rejected(core, 'upgradeOperatorBinding', completed);
  });
  for (const [name, mutate] of [
    ['completed_from_disagrees', fixture => { fixture.completed.from_schema = 26; }],
    ['completed_to_disagrees', fixture => { fixture.completed.to_schema = 25; }],
    ['before_schema_relabelled', fixture => { fixture.before.schema = 26; }],
    ['after_schema_relabelled', fixture => { fixture.state.schema = 25; }],
    ['legacy_theme_present', fixture => { fixture.completed.preservation.theme = clone(fixture.completed.after_preservation.theme); }],
    ['missing_schema25_source', fixture => { delete fixture.before.schema25_source; }],
    ['schema25_source_changed', fixture => { fixture.state.schema25_source.source_manifest_sha256 = syntheticHash('relabelled-old-source'); }],
    ['runtime_state_disagrees', fixture => { fixture.state.runtime_sha256 = syntheticHash('wrong-runtime-state'); }],
    ['browser_state_disagrees', fixture => { fixture.state.browser_sha256 = syntheticHash('wrong-browser-state'); }],
    ['new_active_resource', fixture => { fixture.completed.after_preservation.theme.active_resource_count = 1; }],
    ['new_resource_with_matching_rows', fixture => {
      fixture.completed.after_preservation.row_counts.item_theme_resources = 1;
      fixture.completed.after_preservation.theme.resource_count = 1;
    }],
  ]) preservationNegative(`preservation_${name}`, mutate);
  for (const side of ['preservation', 'after_preservation']) {
    for (const [name, mutate] of [
      ['wrong_schema', summary => { summary.schema = summary.schema === 25 ? 26 : 25; }],
      ['wrong_table_count', summary => { summary.table_count += 1; }],
      ['string_table_count', summary => { summary.table_count = String(summary.table_count); }],
      ['missing_old_table', summary => { delete summary.row_counts.user_item_data; }],
      ['extra_old_table', summary => { summary.row_counts.user_data = 0; }],
      ['wrong_schema_migrations', summary => { summary.row_counts.schema_migrations += 1; }],
      ['negative_row_count', summary => { summary.row_counts.sessions = -1; }],
      ['boolean_row_count', summary => { summary.row_counts.sessions = false; }],
      ['fractional_row_count', summary => { summary.row_counts.sessions = 0.5; }],
      ['unsafe_row_count', summary => { summary.row_counts.sessions = Number.MAX_SAFE_INTEGER + 1; }],
      ['invalid_database_digest', summary => { summary.database_sha256 = 'invalid'; }],
      ['items_disagree', summary => { summary.row_counts.items += 1; }],
      ['libraries_disagree', summary => { summary.row_counts.libraries += 1; }],
    ]) preservationNegative(`${side}_${name}`, fixture => mutate(fixture.completed[side]));
  }
  for (const key of ['recovery_sha256', 'runtime_sha256', 'browser_sha256', 'item_count', 'library_count', 'added_viewer_credentials']) {
    preservationNegative(`preservation_changed_${key}`, fixture => {
      const summary = fixture.completed.after_preservation;
      if (key === 'item_count') {
        summary.item_count += 1; summary.row_counts.items += 1;
        summary.theme.mapped_item_count += 1; summary.theme.owner_count += 1; summary.row_counts.theme_owner_ids += 1;
      } else if (key === 'library_count') { summary.library_count += 1; summary.row_counts.libraries += 1; }
      else if (key === 'added_viewer_credentials') summary[key].credentials_sha256 = syntheticHash('changed-viewer-credentials');
      else summary[key] = syntheticHash(`changed-${key}`);
    });
    for (const side of ['preservation', 'after_preservation']) preservationNegative(`${side}_missing_${key}`,
      fixture => { delete fixture.completed[side][key]; });
  }
  for (const name of SCHEMA_25_TABLES.filter(name => !['schema_migrations', 'items', 'libraries'].includes(name))) {
    preservationNegative(`schema26_old_row_drift_${name}`, fixture => { fixture.completed.after_preservation.row_counts[name] += 1; });
  }
  for (const name of THEME_TABLES) {
    preservationNegative(`schema26_missing_table_${name}`, fixture => { delete fixture.completed.after_preservation.row_counts[name]; });
    preservationNegative(`schema26_wrong_table_count_${name}`, fixture => { fixture.completed.after_preservation.row_counts[name] += 1; });
    preservationNegative(`schema25_unexpected_table_${name}`, fixture => { fixture.completed.preservation.row_counts[name] = 0; });
  }
  preservationNegative('schema26_missing_theme', fixture => { delete fixture.completed.after_preservation.theme; });
  preservationNegative('schema26_extra_theme_key', fixture => { fixture.completed.after_preservation.theme.extra = true; });
  for (const key of [...THEME_COUNTS, ...THEME_HASHES]) {
    preservationNegative(`schema26_missing_theme_${key}`, fixture => { delete fixture.completed.after_preservation.theme[key]; });
  }
  for (const key of THEME_COUNTS) {
    preservationNegative(`schema26_wrong_theme_${key}`, fixture => { fixture.completed.after_preservation.theme[key] += 1; });
    preservationNegative(`schema26_boolean_theme_${key}`, fixture => { fixture.completed.after_preservation.theme[key] = false; });
  }
  for (const key of THEME_HASHES) {
    preservationNegative(`schema26_invalid_${key}`, fixture => { fixture.completed.after_preservation.theme[key] = 'invalid'; });
    preservationNegative(`schema26_reupgrade_changed_${key}`, fixture => {
      fixture.completed.after_preservation.theme[key] = syntheticHash(`changed-${key}`);
    }, 26, 26);
  }
  preservationNegative('schema25_reupgrade_old_row_drift', fixture => { fixture.completed.after_preservation.row_counts.sessions += 1; }, 25, 25);
  preservationNegative('schema26_reupgrade_old_row_drift', fixture => { fixture.completed.after_preservation.row_counts.sessions += 1; }, 26, 26);
  preservationNegative('schema26_reupgrade_schema25_source_changed', fixture => {
    fixture.state.schema25_source.source_manifest_sha256 = syntheticHash('reupgrade-changed-schema25-source');
  }, 26, 26);
  preservationNegative('schema26_reupgrade_reserved_count_drift', fixture => {
    fixture.completed.after_preservation.row_counts.theme_reserved_paths += 1;
    fixture.completed.after_preservation.theme.reserved_path_count += 1;
  }, 26, 26);
  preservationNegative('schema26_reupgrade_resource_count_drift', fixture => {
    fixture.completed.after_preservation.row_counts.item_theme_resources += 1;
    fixture.completed.after_preservation.theme.resource_count += 1;
  }, 26, 26);
  preservationNegative('schema26_reupgrade_active_count_drift', fixture => {
    for (const summary of [fixture.completed.preservation, fixture.completed.after_preservation]) {
      summary.row_counts.item_theme_resources = 1; summary.theme.resource_count = 1;
    }
    fixture.completed.after_preservation.theme.active_resource_count = 1;
  }, 26, 26);
  for (const from of [26, 27]) test(`schema27_explicit_${from}_to27_empty_extras`, () => {
    const fixture = syntheticHop(from, 27), binding = syntheticSource(27);
    requireThat(same(bindSchema(fixture.state), { schema: 27, binding: 'completed_upgrade_and_pinned_schema27_artifacts',
      catalog_sha256: CATALOG_27_SHA, migration_sha256: MIGRATION_27_SHA,
      source: binding.source, source_manifest_sha256: binding.source_manifest_sha256 }));
    requireThat(preserve(fixture) === true && same(core.invoke('upgradeOperatorBinding', fixture.completed),
      { path: `${ROOT}/prepare-client-fixture.py`, sha256: SCHEMA_27_OPERATOR_SHA }));
    requireThat(fixture.completed.after_preservation.table_count === 35 &&
      same(fixture.completed.after_preservation.extras, { extra_reserved_paths: 0, item_extra_resources: 0 }));
  });
  test('retained_operator_proofs_preserve_original_paths_and_both_same_path_versions', () => {
    const old = core.invoke('upgradeOperatorBinding', syntheticState(26).upgrade);
    const current = core.invoke('upgradeOperatorBinding', syntheticState(27).upgrade);
    requireThat(old.path === current.path && old.sha256 !== current.sha256);
    requireThat(same(core.invoke('operatorProofBinding', old), { path: SCHEMA_26_OPERATOR_PROOF, sha256: SCHEMA_26_OPERATOR_SHA }));
    requireThat(same(core.invoke('operatorProofBinding', current), { path: SCHEMA_27_OPERATOR_PROOF, sha256: SCHEMA_27_OPERATOR_SHA }));
    const legacy = core.invoke('upgradeOperatorBinding', syntheticState(25).upgrade);
    requireThat(same(core.invoke('operatorProofBinding', legacy), legacy));
    requireThat(same(core.invoke('uniqueOperatorBindings', [legacy, old, current, old]), [legacy, old, current]));
    for (const path of [SCHEMA_26_OPERATOR_PROOF, SCHEMA_27_OPERATOR_PROOF,
      `${ROOT}/source-attempt-28/scripts/test-env/prepare-client-fixture.py`, '/tmp/prepare-client-fixture.py']) {
      rejected(core, 'operatorProofBinding', { path, sha256: SCHEMA_26_OPERATOR_SHA });
    }
    rejected(core, 'operatorProofBinding', { ...current, sha256: syntheticHash('unreviewed-operator') });
    rejected(core, 'uniqueOperatorBindings', []);
    rejected(core, 'uniqueOperatorBindings', Array(9).fill(current));
  });
  test('schema27_operator_pin_cannot_be_missing_or_relabelled_schema26', () => {
    requireThat(digest(SCHEMA_27_OPERATOR_SHA) && SCHEMA_27_OPERATOR_SHA !== SCHEMA_26_OPERATOR_SHA &&
      SCHEMA_27_OPERATOR_SHA !== LEGACY_OPERATOR_SHA);
    const unfrozen = pureLoaderCore(loaderSource, 27), state = syntheticState(27);
    rejected(unfrozen, 'schemaBinding', state, state.binary_sha256);
    rejected(unfrozen, 'upgradeOperatorBinding', state.upgrade);
    requireThat(unfrozen.invoke('schemaBinding', syntheticState(26), syntheticState(26).binary_sha256).schema === 26);
    for (const value of [SCHEMA_26_OPERATOR_SHA, LEGACY_OPERATOR_SHA, syntheticHash('foreign-operator')]) {
      const bad = syntheticState(27); bad.upgrade.schema_artifacts.operator.sha256 = value;
      rejected(core, 'schemaBinding', bad, bad.binary_sha256);
    }
    const relabelled = syntheticState(26); relabelled.upgrade.schema_artifacts.operator.sha256 = SCHEMA_27_OPERATOR_SHA;
    rejected(core, 'schemaBinding', relabelled, relabelled.binary_sha256);
  });
  for (const [name, mutate] of [
    ['skip_schema25', state => { state.upgrade.from_schema = 25; }],
    ['future_from_schema', state => { state.upgrade.from_schema = 28; }],
    ['string_schema', state => { state.schema = '27'; }],
    ['boolean_schema', state => { state.schema = true; }],
    ['incomplete_upgrade', state => { state.upgrade.phase = 'pending'; }],
    ['old_target_schema', state => { state.upgrade.to_schema = 26; }],
    ['wrong_candidate', state => { state.upgrade.to_sha256 = syntheticHash('other-candidate'); }],
    ['wrong_process', state => { state.upgrade.new_process.pid += 1; }],
    ['old_catalog', state => { state.upgrade.schema_artifacts.catalog_sha256 = CATALOG_26_SHA; }],
    ['old_migration_count', state => { state.upgrade.schema_artifacts.migration_count = 26; }],
    ['boolean_migration_count', state => { state.upgrade.schema_artifacts.migration_count = true; }],
    ['old_migration26_changed', state => { state.upgrade.schema_artifacts.migration_26_sha256 = MIGRATION_27_SHA; }],
    ['old_migration25_changed', state => { state.upgrade.schema_artifacts.migration_25_sha256 = MIGRATION_27_SHA; }],
    ['old_migration24_changed', state => { state.upgrade.schema_artifacts.migration_24_sha256 = MIGRATION_27_SHA; }],
    ['missing_history25', state => { delete state.schema25_source; }],
    ['missing_history26', state => { delete state.schema26_source; }],
    ['history26_relabelled', state => { state.schema26_source.catalog_sha256 = CATALOG_27_SHA; }],
    ['state_source_disagrees', state => { state.schema27_source.source_manifest_sha256 = syntheticHash('wrong-state-source27'); }],
    ['artifact_source_disagrees', state => { state.upgrade.schema_artifacts.source = `${ROOT}/source-attempt-9999`; }],
    ['artifact_manifest_disagrees', state => { state.upgrade.schema_artifacts.source_manifest_sha256 = syntheticHash('wrong-manifest27'); }],
    ['extra_artifact', state => { state.upgrade.schema_artifacts.extra = true; }],
    ['proof_path_replaces_receipt_path', state => { state.upgrade.schema_artifacts.operator.path = SCHEMA_27_OPERATOR_PROOF; }],
    ['extra_operator_field', state => { state.upgrade.schema_artifacts.operator.proof_path = SCHEMA_27_OPERATOR_PROOF; }],
  ]) schemaNegative(`schema27_${name}`, mutate, 27);
  for (const key of Object.keys(syntheticState(27).upgrade.schema_artifacts)) {
    schemaNegative(`schema27_missing_artifact_${key}`, state => { delete state.upgrade.schema_artifacts[key]; }, 27);
  }
  for (const key of Object.keys(syntheticSource(27))) {
    schemaNegative(`schema27_missing_source_${key}`, state => {
      delete state.upgrade.schema_artifacts.schema27_binding[key];
      state.schema27_source = clone(state.upgrade.schema_artifacts.schema27_binding);
    }, 27);
  }
  test('schema27_source_and_support_proofs_require_exact_shapes_and_pins', () => {
    for (const mutate of [binding => { binding.marker = 'goby-client-schema26-source-m3e-v1'; },
      binding => { binding.schema = 26; }, binding => { binding.catalog_sha256 = CATALOG_26_SHA; },
      binding => { binding.migration_27_sha256 = MIGRATION_26_SHA; }, binding => { binding.extra = true; },
      binding => { binding.source = `${ROOT}/source-attempt-21`; }, binding => { binding.source += '/..'; },
      binding => { binding.source_manifest_sha256 = true; }]) {
      const state = syntheticState(27), artifacts = state.upgrade.schema_artifacts;
      mutate(artifacts.schema27_binding); state.schema27_source = clone(artifacts.schema27_binding);
      artifacts.source = state.schema27_source.source; artifacts.source_manifest_sha256 = state.schema27_source.source_manifest_sha256;
      rejected(core, 'schemaBinding', state, state.binary_sha256);
    }
    for (const mutate of [product => { delete product.schema27_support; }, product => { product.extra = true; },
      product => { product.report_path = '/tmp/report.json'; }, product => { product.report_sha256 = true; },
      product => { product.schema27_support.path = `${ROOT}/unreviewed-tool`; },
      product => { product.schema27_support.guard_count = 107; }, product => { product.schema27_support.guard_count = '108'; },
      product => { product.schema27_support.extra = true; }, product => { product.schema27_support.files.extra = syntheticHash('extra'); }]) {
      const state = syntheticState(27); mutate(state.upgrade.schema_artifacts.product_verification);
      rejected(core, 'schemaBinding', state, state.binary_sha256);
    }
    for (const name of Object.keys(SUPPORT_27.files)) for (const missing of [false, true]) {
      const state = syntheticState(27), files = state.upgrade.schema_artifacts.product_verification.schema27_support.files;
      if (missing) delete files[name]; else files[name] = syntheticHash('wrong-support-file');
      rejected(core, 'schemaBinding', state, state.binary_sha256);
    }
  });
  test('schema27_transition_preserves_every_old_table_count_and_theme_hash', () => {
    for (const name of [...SCHEMA_25_TABLES, ...THEME_TABLES]) {
      const fixture = syntheticHop(26, 27); fixture.completed.after_preservation.row_counts[name] += 1;
      rejected(core, 'musicPreservationBinding', fixture.before, fixture.state, fixture.completed);
    }
    for (const key of THEME_HASHES) for (const from of [26, 27]) {
      const fixture = syntheticHop(from, 27); fixture.completed.after_preservation.theme[key] = syntheticHash('changed-theme-hash');
      rejected(core, 'musicPreservationBinding', fixture.before, fixture.state, fixture.completed);
    }
    for (const key of ['schema25_source', 'schema26_source']) {
      const fixture = syntheticHop(26, 27); fixture.state[key].source_manifest_sha256 = syntheticHash('changed-history');
      rejected(core, 'musicPreservationBinding', fixture.before, fixture.state, fixture.completed);
    }
  });
  for (const from of [26, 27]) test(`schema27_${from}_to27_rejects_extra_rows_and_unowned_shape`, () => {
    for (const side of ['preservation', 'after_preservation']) for (const name of EXTRA_TABLES) {
      for (const value of [1, -1, false, '0', 0.5]) {
        const fixture = syntheticHop(from, 27), summary = fixture.completed[side];
        summary.extras ??= {}; summary.extras[name] = value; summary.row_counts[name] = value;
        rejected(core, 'musicPreservationBinding', fixture.before, fixture.state, fixture.completed);
      }
    }
    for (const mutate of [summary => { delete summary.extras; }, summary => { summary.extras.extra = 0; },
      summary => { delete summary.extras.extra_reserved_paths; }, summary => { delete summary.row_counts.item_extra_resources; },
      summary => { summary.row_counts.unowned_table = 0; }, summary => { summary.table_count = 33; },
      summary => { summary.table_count = '35'; }, summary => { summary.schema = '27'; }]) {
      const fixture = syntheticHop(from, 27); mutate(fixture.completed.after_preservation);
      rejected(core, 'musicPreservationBinding', fixture.before, fixture.state, fixture.completed);
    }
    const active = syntheticHop(from, 27);
    for (const summary of [active.completed.preservation, active.completed.after_preservation]) {
      summary.row_counts.item_theme_resources = 1; summary.theme.resource_count = 1; summary.theme.active_resource_count = 1;
    }
    rejected(core, 'musicPreservationBinding', active.before, active.state, active.completed);
  });
  test('extras_summary_is_not_accepted_on_historical_schemas_or_skipped_transitions', () => {
    for (const schema of [25, 26]) {
      const fixture = syntheticHop(schema, schema); fixture.completed.after_preservation.extras = clone(syntheticSummary(27).extras);
      rejected(core, 'musicPreservationBinding', fixture.before, fixture.state, fixture.completed);
    }
    for (const [from, to] of [[25, 27], [27, 26], [27, 25]]) {
      const fixture = syntheticHop(from, to);
      rejected(core, 'musicPreservationBinding', fixture.before, fixture.state, fixture.completed);
    }
  });
  report.planned_test_count = cases.length;
  report.source_sha256 = { 'client-browser-goby-fixture.mjs': hash(loaderSource) };
  for (const { name, action } of cases) {
    const result = { name, pass: false };
    try { action(); requireThat(core.effects === 0); result.pass = true; }
    catch { /* Keep reports free of exception text and input values. */ }
    report.tests.push(result);
  }
  report.counts = { executed: report.tests.length, passed: report.tests.filter(test => test.pass).length,
    failed: report.tests.filter(test => !test.pass).length };
  report.result = report.counts.failed === 0 && report.counts.executed === report.planned_test_count ? 'passed' : 'blocked';
  return report;
}

/** Importing this module performs no guard run. Full explicit inputs are required. */
export async function runMusicLineageGuards(options) {
  requireThat(process.platform === 'linux' && process.getuid?.() === 0 && typeof process.env.SSH_CONNECTION === 'string' &&
    process.env.SSH_CONNECTION.length <= 512 && process.env.SSH_CONNECTION.trim().split(/\s+/).length === 4);
  const mode = selectionMode(options), chainMode = mode === 'chain';
  for (const field of ['candidate-sha256', 'music-scan-receipt-sha256', ...(chainMode
    ? ['music-upgrade-chain-sha256'] : ['music-upgrade-completed-sha256', 'music-upgrade-report-sha256'])]) {
    requireThat(digest(options[field]));
  }
  if (chainMode) requireThat(CHAIN_PATH.test(options['music-upgrade-chain']));
  else requireThat(new RegExp(`^${ROOT}/client-upgrade-[0-9a-f]{32}$`).test(options['music-upgrade-directory']) &&
    new RegExp(`^${ROOT}/client-fixture-report-[0-9a-f]{24}\\.json$`).test(options['music-upgrade-report']));
  process.umask(0o077);
  const output = await reserveReport(options.output);
  const report = { format: 1, mode, result: 'blocked', harness_guards_only: true, client_acceptance: false,
    boundary: 'Only loader file and process guards; no browser, database, HTTP, rescan, or user mutation.',
    planned_test_count: chainMode ? 6 : 7, tests: [], source_sha256: {},
    selected_sha256: { candidate: options['candidate-sha256'], music_scan_receipt: options['music-scan-receipt-sha256'],
      ...(chainMode ? { upgrade_chain: options['music-upgrade-chain-sha256'] }
        : { upgrade_completed: options['music-upgrade-completed-sha256'], upgrade_report: options['music-upgrade-report-sha256'] }) } };
  try {
    report.source_sha256 = await sourceHashes();
    const { loadGobyAVFixture } = await import('./client-browser-goby-fixture.mjs');
    const base = { expectedSHA256: options['candidate-sha256'], expectedMusicScanReceiptSHA256: options['music-scan-receipt-sha256'],
      ...(chainMode ? { musicUpgradeChain: { path: options['music-upgrade-chain'], sha256: options['music-upgrade-chain-sha256'] } }
        : { musicUpgrade: { directory: options['music-upgrade-directory'], completedSHA256: options['music-upgrade-completed-sha256'],
          reportPath: options['music-upgrade-report'], reportSHA256: options['music-upgrade-report-sha256'] } }) };
    let fixture, chainEntries;
    const positive = { name: chainMode ? 'explicit_bounded_upgrade_chain' : 'explicit_single_upgrade_lineage', pass: false, count: 0 };
    report.tests.push(positive);
    const check = value => { positive.count += 1; requireThat(value); };
    try {
      if (chainMode) {
        chainEntries = await readAuthorizedChain(base.musicUpgradeChain.path, base.musicUpgradeChain.sha256);
        check(chainEntries.length >= 1 && chainEntries.length <= 8);
      }
      fixture = await loadGobyAVFixture(base);
      check((await fixture.assertPinned()).phase === 'pinned');
      const evidence = fixture.evidence, scan = evidence?.music_scan, lineage = scan?.upgrade_lineage;
      check(record(scan) && record(lineage));
      check(evidence.binary_sha256 === base.expectedSHA256 && scan.receipt_sha256 === base.expectedMusicScanReceiptSHA256);
      if (chainMode) {
        check(lineage.chain_path === base.musicUpgradeChain.path && lineage.chain_sha256 === base.musicUpgradeChain.sha256);
        check(lineage.hop_count === chainEntries.length && Array.isArray(lineage.hops) && lineage.hops.length === chainEntries.length);
        check(lineage.method === (lineage.to_schema === 27 ? SCHEMA_27_CHAIN_METHOD
          : lineage.hops.every(hop => hop?.from_schema === 25 && hop?.to_schema === 25) ? LEGACY_CHAIN_METHOD : MIXED_CHAIN_METHOD));
        for (let index = 0; index < chainEntries.length; index += 1) {
          const entry = chainEntries[index], hop = lineage.hops[index];
          check(validHopOperator(hop));
          check(hop.directory === entry.directory && hop.completed_sha256 === entry.completedSHA256 &&
            hop.report_path === entry.reportPath && hop.report_sha256 === entry.reportSHA256);
          check(digest(hop.before_state_sha256) && digest(hop.from_sha256) && digest(hop.to_sha256) &&
            hop.from_sha256 !== hop.to_sha256 && digest(hop.operator_sha256));
          check(validProcess(hop.old_process) && validProcess(hop.new_process) && !same(hop.old_process, hop.new_process));
          check(hop.full_snapshot_read === false && hop.automatic_history_discovery === false && hop.scan_repeated === false);
          if (index > 0) check(lineage.hops[index - 1].to_sha256 === hop.from_sha256 &&
            lineage.hops[index - 1].to_schema === hop.from_schema && same(lineage.hops[index - 1].new_process, hop.old_process));
        }
        const first = lineage.hops[0], last = lineage.hops[lineage.hops.length - 1];
        const operatorHashes = [...new Set(lineage.hops.map(hop => hop.operator_sha256))];
        check(same(lineage.operator_sha256s, operatorHashes) &&
          lineage.operator_sha256 === (operatorHashes.length === 1 ? operatorHashes[0] : null));
        check(lineage.before_state_sha256 === first.before_state_sha256 && lineage.from_sha256 === first.from_sha256 &&
          lineage.to_sha256 === last.to_sha256 && lineage.from_schema === first.from_schema && lineage.to_schema === last.to_schema &&
          same(lineage.old_process, first.old_process) && same(lineage.new_process, last.new_process));
      } else {
        check(validHopOperator(lineage));
        check(lineage.directory === base.musicUpgrade.directory && lineage.report_path === base.musicUpgrade.reportPath);
        check(lineage.completed_sha256 === base.musicUpgrade.completedSHA256 && lineage.report_sha256 === base.musicUpgrade.reportSHA256);
      }
      check(digest(lineage.before_state_sha256) && lineage.before_state_sha256 === scan.fixture_state_sha256 &&
        (chainMode || digest(lineage.operator_sha256)));
      check(lineage.from_schema === 25 && [25, 26, 27].includes(lineage.to_schema) && scan.schema === 25 && evidence.schema === lineage.to_schema);
      check(lineage.from_sha256 === scan.candidate_sha256 && lineage.to_sha256 === base.expectedSHA256 && lineage.from_sha256 !== lineage.to_sha256);
      check(validProcess(scan.process) && validProcess(evidence.process) && validProcess(lineage.old_process) && validProcess(lineage.new_process));
      check(same(scan.process, lineage.old_process) && same(evidence.process, lineage.new_process) && !same(scan.process, evidence.process));
      check(lineage.automatic_history_discovery === false && lineage.scan_repeated === false && lineage.full_snapshot_read === false);
      const album = fixture.music?.album, tracks = fixture.music?.tracks;
      check(fixture.album === 'M3e Synthetic Album' && album?.name === 'M3e Synthetic Album' && album.path === '/opt/goby-fixtures/client-m3e/Music');
      check(album.id === '002c2ea2c77769ae9b0b84b6e788998b' && same(album, scan.album));
      check(record(tracks) && same(Object.keys(tracks).sort(), ['flac', 'mp3']) && same(tracks, scan.tracks));
      for (const [container, name, expectedId] of [['mp3', 'M3e MP3', 'e2da060a0addfafde7f92c291c413f6d'],
        ['flac', 'M3e FLAC', '4d2ec259d9ae5224f46aa6f56ca2b186']]) {
        const track = tracks[container];
        check(record(track) && track.name === name && track.container === container && track.parentId === album.id &&
          track.id === expectedId &&
          track.path === `/opt/goby-fixtures/client-m3e/Music/M3e Client Audio.${container}`);
      }
      check(tracks.mp3.id !== tracks.flac.id && tracks.mp3.id !== album.id && tracks.flac.id !== album.id);
      positive.evidence_sha256 = hash(JSON.stringify(stable({ lineage, scan_process: scan.process, current_process: evidence.process, album, tracks })));
      positive.pass = true;
    } catch { /* Never retain exception text or continue into negative cases after a failed positive. */ }

    if (positive.pass) {
      const negatives = chainMode ? [
        ['wrong_chain_sha256', 'music_scan_binding', () => ({ ...base,
          musicUpgradeChain: { ...base.musicUpgradeChain, sha256: wrongHash(base.musicUpgradeChain.sha256) } })],
        ['missing_explicit_music_upgrade_chain', 'music_scan_binding', () => ({ expectedSHA256: base.expectedSHA256,
          expectedMusicScanReceiptSHA256: base.expectedMusicScanReceiptSHA256 })],
        ['outside_music_upgrade_chain_path', 'music_scan_binding', () => ({ ...base,
          musicUpgradeChain: { ...base.musicUpgradeChain, path: path.join(path.dirname(ROOT), path.basename(base.musicUpgradeChain.path)) } })],
        ['single_and_chain_selections_conflict', 'music_scan_binding', () => ({ ...base, musicUpgrade: { ...chainEntries[0] } })],
        ['wrong_candidate_sha256', 'candidate_sha256', () => ({ ...base, expectedSHA256: wrongHash(base.expectedSHA256) })],
      ] : [
        ['missing_explicit_music_upgrade', 'music_scan_binding', () => ({ expectedSHA256: base.expectedSHA256, expectedMusicScanReceiptSHA256: base.expectedMusicScanReceiptSHA256 })],
        ['wrong_completed_sha256', 'music_scan_binding', () => ({ ...base, musicUpgrade: { ...base.musicUpgrade, completedSHA256: wrongHash(base.musicUpgrade.completedSHA256) } })],
        ['wrong_report_sha256', 'music_scan_binding', () => ({ ...base, musicUpgrade: { ...base.musicUpgrade, reportSHA256: wrongHash(base.musicUpgrade.reportSHA256) } })],
        ['outside_upgrade_evidence_directory', 'music_scan_binding', () => ({ ...base, musicUpgrade: { ...base.musicUpgrade, directory: ROOT } })],
        ['partial_music_upgrade_object', 'music_scan_binding', () => ({ ...base, musicUpgrade: { directory: base.musicUpgrade.directory,
          completedSHA256: base.musicUpgrade.completedSHA256, reportPath: base.musicUpgrade.reportPath } })],
        ['wrong_candidate_sha256', 'candidate_sha256', () => ({ ...base, expectedSHA256: wrongHash(base.expectedSHA256) })],
      ];
      for (const [name, expectedPhase, selection] of negatives) {
        const entry = { name, pass: false, count: 0 };
        report.tests.push(entry);
        try {
          requireThat((await fixture.assertPinned()).phase === 'pinned'); entry.count += 1;
          let rejected = false;
          try { await loadGobyAVFixture(selection()); }
          catch (error) { rejected = error?.phase === expectedPhase; }
          entry.count += 1;
          requireThat((await fixture.assertPinned()).phase === 'pinned'); entry.count += 1;
          entry.pass = rejected;
        } catch { break; }
      }
    }
    if (chainMode && positive.pass) {
      requireThat(same(chainEntries, await readAuthorizedChain(base.musicUpgradeChain.path, base.musicUpgradeChain.sha256)));
      report.chain_file_unchanged = true;
    }
    report.source_files_unchanged = same(report.source_sha256, await sourceHashes());
    report.result = report.source_files_unchanged && (!chainMode || report.chain_file_unchanged) &&
      report.tests.length === report.planned_test_count && report.tests.every(entry => entry.pass) ? 'passed' : 'blocked';
  } catch { report.result = 'blocked'; }
  finally {
    report.counts = { executed: report.tests.length, passed: report.tests.filter(entry => entry.pass).length,
      failed: report.tests.filter(entry => !entry.pass).length };
    try { await output.writeFile(JSON.stringify(report, null, 2) + '\n'); await output.sync(); }
    finally { await output.close(); }
  }
  return report;
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  try {
    const argv = process.argv.slice(2);
    const pureMode = argv.length === 1 && argv[0] === '--pure';
    if (pureMode) requireThat(process.platform === 'linux' && process.getuid?.() === 0 &&
      typeof process.env.SSH_CONNECTION === 'string' && process.env.SSH_CONNECTION.length <= 512 &&
      process.env.SSH_CONNECTION.trim().split(/\s+/).length === 4);
    const report = pureMode
      ? runPureMusicLineageGuards(await fs.readFile(LOADER, 'utf8'))
      : await runMusicLineageGuards(parseArguments(argv));
    process.stdout.write(JSON.stringify({ result: report.result, mode: report.mode, harness_guards_only: true, counts: report.counts,
      ...(report.mode === 'pure' ? { failed: report.tests.filter(test => !test.pass).map(test => test.name) } : {}) }) + '\n');
    if (report.result !== 'passed') process.exitCode = 1;
  } catch {
    process.stdout.write(JSON.stringify({ result: 'blocked', harness_guards_only: true }) + '\n');
    process.exitCode = 1;
  }
}
