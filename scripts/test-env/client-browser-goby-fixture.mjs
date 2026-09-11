/** Load only the receipted Goby AV account and repeatedly pin its live fixture. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { TextDecoder } from 'node:util';

const ROOT = '/opt/goby-test/exec-work-m3e';
const MARKER = 'goby-m3e-client-acceptance-v1';
const ACCOUNT_MARKER = 'goby-m3e-goby-av-user-v1';
const USERNAME = 'm3e-goby-av-client';
const ORIGIN = 'http://127.0.0.1:18196';
const DIRECT = 'http://127.0.0.1:18198';
const BINARY = '/opt/goby-client-m3e/goby';
const CGROUP = '/system.slice/goby-client-m3e.service';
const STATE = `${ROOT}/client-fixture.json`;
const ALIAS = `${ROOT}/goby-av-browser.json`;
const RECEIPT = `${ROOT}/goby-av-user-receipt.json`;
const ACCOUNT_ROOT = `${ROOT}/goby-av-user-v1`;
const PROXY = `${ROOT}/dual-proxy-status-02.json`;
const MUSIC_SCAN_ROOT = `${ROOT}/client-music-scan-v1`;
const MUSIC_SCAN_RECEIPT = `${MUSIC_SCAN_ROOT}/receipt.json`;
const MUSIC_SCAN_REPORT = `${MUSIC_SCAN_ROOT}/report.json`;
const MUSIC_SCAN_OWNER = `${MUSIC_SCAN_ROOT}/OWNER.json`;
const MUSIC_SCAN_RECEIPT_SHA = 'e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b';
const MUSIC_SCAN_REPORT_SHA = '11d6f781476093f14b0f1942037add52d00b08bd7d3f78fb8cec8acbbea9b0a4';
const MUSIC_SCAN_OWNER_SHA = '40c7d2b27eaa6b1e0cc1c9fdb4668fb8fcb1d6edd93ebe3a3bb77b6e2405342e';
const MUSIC_SCAN_JOB = '5bbac57fc1bd21a5aebef85a2537624e';
const FIXTURE_OPERATOR = `${ROOT}/prepare-client-fixture.py`;
const FIXTURE_OPERATOR_SHA = '85b1c7ac16646d0af19cd67d649b72f3df6cb7e08eab4357b94d5b6e6ab818de';
const LEGACY_FIXTURE_OPERATOR = `${ROOT}/source-attempt-20/scripts/test-env/prepare-client-fixture.py`;
// The schema26 fixture operator was independently frozen before this binding.
const SCHEMA_26_OPERATOR_SHA = 'df9f0d33b9ff3136a6958a91d855faf47d703e7f5368a953cb0d182aa12bbd3b';
const SCHEMA_26_OPERATOR_PROOF = `${ROOT}/client-schema26-retained-tool-01/prepare-client-fixture.py`;
const SCHEMA_27_OPERATOR_PROOF = `${ROOT}/client-schema27-tool-01/prepare-client-fixture.py`;
// These schema27 bytes and the retained schema26 proof were independently frozen.
const SCHEMA_27_OPERATOR_SHA = '84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c';
const INPUTS = new Set([STATE, ALIAS, RECEIPT, PROXY, MUSIC_SCAN_RECEIPT, MUSIC_SCAN_REPORT, MUSIC_SCAN_OWNER]);
const JSON_LIMIT = 2 * 1024 * 1024;
const CATALOG_25_SHA = 'e269a7eb6b31d2eb3fff734896ca074a6113761f321f2f23f4b07441e7dc617b';
const MIGRATION_25_SHA = '3b21cac79173b4b9184d73b76066aff4199a4d6d4ad6fc64d2d0d826796b6eaf';
const CATALOG_26_SHA = 'e02c46a49dd4200bb67954f70ca0bbe90b3ef99d8821ea56a97e3dcb97e696de';
const MIGRATION_26_SHA = 'c4b7485174fe655524fe5de6973f9d75a6ce7917f4ebd82476ac1c13be5817e8';
const CATALOG_27_SHA = '1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d';
const MIGRATION_27_SHA = 'b62d0422dddb9e258f46589898f672b08fc6e4c12fea7456059f855a6600353c';
const MIGRATION_24_SHA = '2a9c159f111073a1c01bbbda158cfe3d98d25f417d70c7eda14b70a7798a3972';
const SCHEMA_25_TABLES = Object.freeze(['activity_entries', 'application_key_clients', 'application_key_devices',
  'application_keys', 'catalog_entities', 'client_playback_references', 'devices', 'encoding_jobs', 'item_entities',
  'item_images', 'item_metadata_state', 'item_subtitles', 'items', 'libraries', 'library_roots', 'managed_settings',
  'play_sessions', 'scan_jobs', 'schema_migrations', 'server_settings', 'sessions', 'task_definitions',
  'task_occurrences', 'task_run_children', 'task_run_requests', 'task_runs', 'task_triggers', 'user_item_data',
  'user_settings', 'users']);
const THEME_TABLES = Object.freeze(['item_theme_resources', 'theme_owner_ids', 'theme_reserved_paths']);
const EXTRA_TABLES = Object.freeze(['extra_reserved_paths', 'item_extra_resources']);
const SCHEMA_27_SUPPORT = `${ROOT}/backup-schema27-tool-01`;
const SCHEMA_27_SUPPORT_HASHES = Object.freeze({
  'run-client-backup-tests.py': '7cf91591efaeca841907cfe1950d0bfcb6c33ef091a17fe54c69db167a74c22a',
  'guards.stdout': '9ecb9d07c61c61b7510c8ed16e895ad2c146d0244920914906663d1d40703ea9',
  'guards.stderr': '990ab0a20e53bd44e889ebd85482fb5bf15631275d8b187e056c324fd560f09c',
});
const REFERENCE = Object.freeze({ pid: 332054, start_ticks: 357218,
  executable: '/dev/shm/goby-emby-reference/package/opt/emby-server/system/EmbyServer',
  sha256: 'c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2' });
const sha = bytes => createHash('sha256').update(bytes).digest('hex');
const digest = value => typeof value === 'string' && /^[0-9a-f]{64}$/.test(value);
const identifier = value => typeof value === 'string' && /^[0-9a-f]{32}$/.test(value);
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const requireThat = condition => { if (!condition) throw new Error('fixture_binding_mismatch'); };
const processIdentity = value => record(value) && Number.isSafeInteger(value.pid) && value.pid > 1 &&
  Number.isSafeInteger(value.start_ticks) && value.start_ticks > 0 &&
  typeof value.boot_id === 'string' && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(value.boot_id);

function stable(value) {
  if (Array.isArray(value)) return value.map(stable);
  if (record(value)) return Object.fromEntries(Object.keys(value).sort().map(key => [key, stable(value[key])]));
  return value;
}
const same = (left, right) => JSON.stringify(stable(left)) === JSON.stringify(stable(right));
function sourceBinding(binding, schema) {
  requireThat([25, 26, 27].includes(schema));
  const migrationKey = `migration_${schema}_sha256`;
  requireThat(record(binding) && same(Object.keys(binding).sort(),
    ['marker', 'schema', 'source', 'source_manifest_sha256', 'catalog_sha256', migrationKey].sort()) &&
    binding.marker === `goby-client-schema${schema}-source-m3e-v1` && binding.schema === schema &&
    binding.catalog_sha256 === ({ 25: CATALOG_25_SHA, 26: CATALOG_26_SHA, 27: CATALOG_27_SHA })[schema] &&
    binding[migrationKey] === ({ 25: MIGRATION_25_SHA, 26: MIGRATION_26_SHA, 27: MIGRATION_27_SHA })[schema] &&
    typeof binding.source === 'string' && /^\/opt\/goby-test\/exec-work-m3e\/source-attempt-[1-9]\d*$/.test(binding.source) &&
    digest(binding.source_manifest_sha256));
  if (schema >= 26) {
    // Source21 is the catalog bootstrap and source22 contains database gates
    // only. Full-source operators require a later complete product snapshot.
    const number = Number(binding.source.slice(binding.source.lastIndexOf('-') + 1));
    requireThat(Number.isSafeInteger(number) && number > 22);
  }
  return binding;
}
function schemaBinding(state, expectedSHA256) {
  if (state.schema === 24) return { schema: 24, binding: 'existing_receipted_fixture' };
  requireThat([25, 26, 27].includes(state.schema) && record(state.upgrade));
  if (state.schema >= 26) {
    const schema = state.schema, catalog = schema === 26 ? CATALOG_26_SHA : CATALOG_27_SHA;
    const upgrade = state.upgrade, artifacts = upgrade.schema_artifacts;
    requireThat(upgrade.phase === 'complete' && (schema === 26 ? [25, 26] : [26, 27]).includes(upgrade.from_schema) && upgrade.to_schema === schema &&
      upgrade.to_sha256 === expectedSHA256 && same(upgrade.new_process, state.process) && record(artifacts) &&
      same(Object.keys(artifacts).sort(), ['source', 'catalog_sha256', 'source_manifest_sha256', 'migration_count',
        'migration_24_sha256', 'migration_25_sha256', `schema${schema}_binding`, 'operator', 'product_verification',
        ...(schema === 27 ? ['migration_26_sha256'] : [])].sort()) &&
      artifacts.migration_count === schema && artifacts.catalog_sha256 === catalog &&
      artifacts.migration_24_sha256 === MIGRATION_24_SHA && artifacts.migration_25_sha256 === MIGRATION_25_SHA &&
      (schema !== 27 || artifacts.migration_26_sha256 === MIGRATION_26_SHA));
    const binding = sourceBinding(artifacts[`schema${schema}_binding`], schema);
    requireThat(same(binding, state[`schema${schema}_source`]) && binding.source === artifacts.source &&
      binding.source_manifest_sha256 === artifacts.source_manifest_sha256);
    sourceBinding(state.schema25_source, 25);
    if (schema === 27) sourceBinding(state.schema26_source, 26);
    upgradeOperatorBinding(upgrade);
    const product = artifacts.product_verification;
    requireThat(record(product) && same(Object.keys(product).sort(),
      ['report_path', 'report_sha256', ...(schema === 27 ? ['schema27_support'] : [])].sort()) &&
      digest(product.report_sha256) && typeof product.report_path === 'string' &&
      new RegExp(`^${ROOT}/client-backup-run-[0-9]{8}_[0-9]{6}_[0-9a-f]{12}/report\\.json$`).test(product.report_path));
    if (schema === 27) {
      const support = product.schema27_support;
      requireThat(record(support) && same(Object.keys(support).sort(), ['files', 'guard_count', 'path']) &&
        support.path === SCHEMA_27_SUPPORT && support.guard_count === 108 && same(support.files, SCHEMA_27_SUPPORT_HASHES));
    }
    return { schema, binding: `completed_upgrade_and_pinned_schema${schema}_artifacts`,
      catalog_sha256: catalog, migration_sha256: schema === 26 ? MIGRATION_26_SHA : MIGRATION_27_SHA,
      source: binding.source, source_manifest_sha256: binding.source_manifest_sha256 };
  }
  const upgrade = state.upgrade, artifacts = upgrade.schema_artifacts, binding = artifacts?.schema25_binding;
  requireThat(upgrade.phase === 'complete' && [24, 25].includes(upgrade.from_schema) && upgrade.to_schema === 25 &&
    upgrade.to_sha256 === expectedSHA256 && same(upgrade.new_process, state.process) && record(artifacts) &&
    artifacts.migration_count === 25 && artifacts.catalog_sha256 === CATALOG_25_SHA && record(binding) &&
    binding.marker === 'goby-client-schema25-source-m3e-v1' && binding.schema === 25 &&
    binding.catalog_sha256 === CATALOG_25_SHA && binding.migration_25_sha256 === MIGRATION_25_SHA &&
    typeof binding.source === 'string' && /^\/opt\/goby-test\/exec-work-m3e\/source-attempt-[1-9]\d*$/.test(binding.source) &&
    binding.source === artifacts.source && digest(binding.source_manifest_sha256) &&
    binding.source_manifest_sha256 === artifacts.source_manifest_sha256);
  return { schema: 25, binding: 'completed_upgrade_and_pinned_schema25_artifacts',
    catalog_sha256: CATALOG_25_SHA, migration_sha256: MIGRATION_25_SHA,
    source: binding.source, source_manifest_sha256: binding.source_manifest_sha256 };
}

function upgradeOperatorBinding(completed) {
  requireThat(record(completed) && record(completed.schema_artifacts));
  if (completed.from_schema === 25 && completed.to_schema === 25) {
    requireThat(!Object.hasOwn(completed.schema_artifacts, 'operator'));
    return { path: LEGACY_FIXTURE_OPERATOR, sha256: FIXTURE_OPERATOR_SHA };
  }
  const schema = completed.to_schema;
  requireThat(schema === 26 && [25, 26].includes(completed.from_schema) || schema === 27 && [26, 27].includes(completed.from_schema));
  const expectedSHA = schema === 26 ? SCHEMA_26_OPERATOR_SHA : SCHEMA_27_OPERATOR_SHA;
  requireThat(digest(expectedSHA));
  const operator = completed.schema_artifacts.operator;
  requireThat(record(operator) && same(Object.keys(operator).sort(), ['path', 'sha256']) &&
    operator.path === FIXTURE_OPERATOR && operator.sha256 === expectedSHA);
  return { ...operator };
}

function operatorProofBinding(binding) {
  requireThat(record(binding) && same(Object.keys(binding).sort(), ['path', 'sha256']));
  if (binding.path === LEGACY_FIXTURE_OPERATOR && binding.sha256 === FIXTURE_OPERATOR_SHA) return { ...binding };
  if (binding.path === FIXTURE_OPERATOR && digest(SCHEMA_26_OPERATOR_SHA) && binding.sha256 === SCHEMA_26_OPERATOR_SHA) {
    return { path: SCHEMA_26_OPERATOR_PROOF, sha256: SCHEMA_26_OPERATOR_SHA };
  }
  requireThat(binding.path === FIXTURE_OPERATOR && digest(SCHEMA_27_OPERATOR_SHA) && binding.sha256 === SCHEMA_27_OPERATOR_SHA);
  return { path: SCHEMA_27_OPERATOR_PROOF, sha256: SCHEMA_27_OPERATOR_SHA };
}

function uniqueOperatorBindings(bindings) {
  requireThat(Array.isArray(bindings) && bindings.length >= 1 && bindings.length <= 8);
  for (const binding of bindings) operatorProofBinding(binding);
  return [...new Map(bindings.map(binding => [`${binding.path}:${binding.sha256}`, binding])).values()];
}

function musicPreservationBinding(before, state, completed) {
  const from = before.schema, to = state.schema, preserved = completed.preservation, after = completed.after_preservation;
  requireThat((from === 25 && [25, 26].includes(to) || from === 26 && [26, 27].includes(to) || from === 27 && to === 27) &&
    completed.from_schema === from && completed.to_schema === to && record(preserved) && record(after));
  for (const [summary, schema] of [[preserved, from], [after, to]]) {
    const tables = [...SCHEMA_25_TABLES, ...(schema >= 26 ? THEME_TABLES : []), ...(schema === 27 ? EXTRA_TABLES : [])];
    requireThat(summary.schema === schema && digest(summary.database_sha256) && record(summary.row_counts) &&
      summary.table_count === tables.length && same(Object.keys(summary.row_counts).sort(), tables.sort()) &&
      Object.values(summary.row_counts).every(value => Number.isSafeInteger(value) && value >= 0) &&
      Number.isSafeInteger(summary.item_count) && summary.item_count > 0 && summary.item_count === summary.row_counts.items &&
      Number.isSafeInteger(summary.library_count) && summary.library_count > 0 && summary.library_count === summary.row_counts.libraries &&
      summary.row_counts.schema_migrations === schema);
    for (const key of ['recovery_sha256', 'runtime_sha256', 'browser_sha256']) requireThat(digest(summary[key]));
    if (schema === 25) requireThat(!Object.hasOwn(summary, 'theme'));
    else {
      const theme = summary.theme;
      requireThat(record(theme) && same(Object.keys(theme).sort(), ['owner_count', 'virtual_root_count', 'mapped_item_count',
        'reserved_path_count', 'resource_count', 'active_resource_count', 'owner_rows_sha256',
        'reserved_paths_sha256', 'resource_rows_sha256'].sort()) &&
        ['owner_count', 'virtual_root_count', 'mapped_item_count', 'reserved_path_count', 'resource_count', 'active_resource_count']
          .every(key => Number.isSafeInteger(theme[key]) && theme[key] >= 0) &&
        ['owner_rows_sha256', 'reserved_paths_sha256', 'resource_rows_sha256'].every(key => digest(theme[key])) &&
        theme.virtual_root_count === 1 && theme.mapped_item_count === summary.item_count &&
        theme.owner_count === summary.item_count + 1 && theme.owner_count === summary.row_counts.theme_owner_ids &&
        theme.reserved_path_count === summary.row_counts.theme_reserved_paths &&
        theme.resource_count === summary.row_counts.item_theme_resources && theme.resource_count <= summary.item_count &&
        theme.active_resource_count <= theme.resource_count);
    }
    if (schema === 27) {
      requireThat(record(summary.extras) && same(Object.keys(summary.extras).sort(), [...EXTRA_TABLES].sort()) &&
        EXTRA_TABLES.every(name => summary.extras[name] === 0 && summary.row_counts[name] === 0) &&
        summary.theme.active_resource_count === 0);
    } else requireThat(!Object.hasOwn(summary, 'extras'));
  }
  for (const key of ['recovery_sha256', 'runtime_sha256', 'browser_sha256', 'item_count', 'library_count', 'added_viewer_credentials']) {
    requireThat(Object.hasOwn(preserved, key) && Object.hasOwn(after, key) && same(preserved[key], after[key]));
  }
  requireThat(preserved.runtime_sha256 === state.runtime_sha256 && preserved.browser_sha256 === state.browser_sha256);
  if (to >= 26) requireThat(Object.hasOwn(before, 'schema25_source') && same(before.schema25_source, state.schema25_source));
  if (to === 27) requireThat(Object.hasOwn(before, 'schema26_source') && same(before.schema26_source, state.schema26_source));
  if (from === to) {
    requireThat(same(preserved.row_counts, after.row_counts));
    if (to >= 26) requireThat(same(preserved.theme, after.theme));
    if (to === 27) requireThat(same(preserved.extras, after.extras));
  } else if (to === 26) {
    for (const name of SCHEMA_25_TABLES) {
      requireThat(after.row_counts[name] === preserved.row_counts[name] + (name === 'schema_migrations' ? 1 : 0));
    }
    requireThat(after.theme.resource_count === 0 && after.theme.active_resource_count === 0);
  } else {
    for (const name of [...SCHEMA_25_TABLES, ...THEME_TABLES]) {
      requireThat(after.row_counts[name] === preserved.row_counts[name] + (name === 'schema_migrations' ? 1 : 0));
    }
    requireThat(preserved.theme.active_resource_count === 0 && same(preserved.theme, after.theme));
  }
  return true;
}
const safeErrors = new WeakSet();
function safeError(phase) {
  const error = Object.assign(new Error(`goby_av_fixture_${phase}_failed`), { phase });
  error.stack = undefined;
  safeErrors.add(error);
  return error;
}
function freeze(value) {
  if (value && typeof value === 'object') { for (const child of Object.values(value)) freeze(child); Object.freeze(value); }
  return value;
}

// JSON.parse alone accepts duplicate object keys. Scan the bounded document
// first so a receipt cannot give different identities to different readers.
function strictJSON(text, allowArray = false) {
  let position = 0, nodes = 0;
  const whitespace = () => { while (/\s/.test(text[position] ?? '') && position < text.length) position += 1; };
  function string() {
    const start = position++;
    while (position < text.length) {
      if (text[position] === '\\') { position += 2; continue; }
      if (text[position++] === '"') return JSON.parse(text.slice(start, position));
    }
    throw new Error('invalid_private_json');
  }
  function value(depth) {
    requireThat(depth <= 64 && ++nodes <= 100000);
    whitespace();
    if (text[position] === '"') { string(); return; }
    const opening = text[position];
    if (opening === '{' || opening === '[') {
      position += 1; whitespace();
      const closing = opening === '{' ? '}' : ']';
      const keys = new Set();
      if (text[position] === closing) { position += 1; return; }
      for (;;) {
        if (opening === '{') {
          requireThat(text[position] === '"');
          const key = string(); requireThat(!keys.has(key)); keys.add(key);
          whitespace(); requireThat(text[position++] === ':');
        }
        value(depth + 1); whitespace();
        if (text[position] === closing) { position += 1; return; }
        requireThat(text[position++] === ','); whitespace();
      }
    }
    const token = /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(text.slice(position));
    requireThat(token);
    if (/^-?\d/.test(token[0])) requireThat(Number.isFinite(Number(token[0])));
    position += token[0].length;
  }
  value(0); whitespace(); requireThat(position === text.length);
  const parsed = JSON.parse(text);
  requireThat(record(parsed) || allowArray && Array.isArray(parsed));
  return parsed;
}

async function directory(filename, expected) {
  requireThat(await fs.realpath(filename) === filename);
  const info = await fs.lstat(filename, { bigint: true });
  requireThat(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0n && info.gid === 0n && (info.mode & 0o777n) === 0o700n);
  if (expected) requireThat(record(expected) && String(expected.device) === String(info.dev) && String(expected.inode) === String(info.ino));
  return { device: String(info.dev), inode: String(info.ino) };
}
function unchanged(before, after) {
  return ['dev', 'ino', 'mode', 'uid', 'gid', 'nlink', 'size', 'mtimeNs', 'ctimeNs'].every(key => before[key] === after[key]);
}
async function privateDocument(filename, extraInputs = [], allowArray = false) {
  requireThat((INPUTS.has(filename) || extraInputs.includes(filename)) && await fs.realpath(filename) === filename);
  const before = await fs.lstat(filename, { bigint: true });
  requireThat(before.isFile() && !before.isSymbolicLink() && before.uid === 0n && (before.mode & 0o777n) === 0o600n &&
    before.nlink === 1n && before.size > 0n && before.size <= BigInt(JSON_LIMIT));
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  const bytes = Buffer.alloc(JSON_LIMIT + 1);
  try {
    requireThat(unchanged(before, await handle.stat({ bigint: true })));
    let size = 0;
    while (size < bytes.length) {
      const read = await handle.read(bytes, size, bytes.length - size, size);
      if (!read.bytesRead) break;
      size += read.bytesRead;
    }
    requireThat(size === Number(before.size) && size <= JSON_LIMIT && unchanged(before, await handle.stat({ bigint: true })) &&
      unchanged(before, await fs.lstat(filename, { bigint: true })));
    const raw = bytes.subarray(0, size);
    return { value: strictJSON(new TextDecoder('utf-8', { fatal: true }).decode(raw), allowArray), sha256: sha(raw) };
  } finally { bytes.fill(0); await handle.close(); }
}

async function operatorDigest(binding) {
  const proof = operatorProofBinding(binding), filename = proof.path;
  requireThat(await fs.realpath(filename) === filename);
  const before = await fs.lstat(filename, { bigint: true });
  const modes = filename === LEGACY_FIXTURE_OPERATOR ? [0o600n, 0o644n] : [0o600n, 0o644n, 0o700n, 0o755n];
  requireThat(before.isFile() && !before.isSymbolicLink() && before.uid === 0n && before.gid === 0n &&
    modes.includes(before.mode & 0o777n) && before.nlink === 1n && before.size > 0n && before.size <= BigInt(JSON_LIMIT));
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  const bytes = Buffer.alloc(65536), hash = createHash('sha256');
  try {
    requireThat(unchanged(before, await handle.stat({ bigint: true })));
    let size = 0;
    for (;;) {
      const read = await handle.read(bytes, 0, bytes.length, size);
      if (!read.bytesRead) break;
      size += read.bytesRead; requireThat(size <= JSON_LIMIT); hash.update(bytes.subarray(0, read.bytesRead));
    }
    requireThat(size === Number(before.size) && unchanged(before, await handle.stat({ bigint: true })) &&
      unchanged(before, await fs.lstat(filename, { bigint: true })));
    const actual = hash.digest('hex'); requireThat(actual === binding.sha256); return actual;
  } finally { bytes.fill(0); await handle.close(); }
}

async function musicUpgradeBinding(state, selection) {
  if (selection === undefined) return null;
  requireThat(record(selection) && same(Object.keys(selection).sort(), ['completedSHA256', 'directory', 'reportPath', 'reportSHA256']) &&
    digest(selection.completedSHA256) && digest(selection.reportSHA256) &&
    typeof selection.directory === 'string' && new RegExp(`^${ROOT}/client-upgrade-[0-9a-f]{32}$`).test(selection.directory) &&
    typeof selection.reportPath === 'string' && new RegExp(`^${ROOT}/client-fixture-report-[0-9a-f]{24}\\.json$`).test(selection.reportPath));
  const upgrade = state.upgrade;
  requireThat([25, 26, 27].includes(state.schema) && record(upgrade) && upgrade.phase === 'complete' &&
    typeof upgrade.id === 'string' && /^[0-9a-f]{32}$/.test(upgrade.id) &&
    selection.directory === `${ROOT}/client-upgrade-${upgrade.id}` && upgrade.evidence_directory === selection.directory &&
    [25, 26, 27].includes(upgrade.from_schema) && upgrade.to_schema === state.schema);
  const identity = await directory(selection.directory, upgrade.evidence_identity);
  const beforePath = `${selection.directory}/before-state.json`, completedPath = `${selection.directory}/completed.json`;
  const inputs = [beforePath, completedPath, selection.reportPath];
  const beforeFile = await privateDocument(beforePath, inputs), completedFile = await privateDocument(completedPath, inputs),
    reportFile = await privateDocument(selection.reportPath, inputs);
  requireThat(completedFile.sha256 === selection.completedSHA256 && reportFile.sha256 === selection.reportSHA256);
  const before = beforeFile.value, completed = completedFile.value, report = reportFile.value;
  requireThat(same(completed, upgrade) && before.marker === MARKER && before.phase === 'ready' && before.stage === 'complete' &&
    [25, 26, 27].includes(before.schema) && processIdentity(before.process) && digest(before.binary_sha256) &&
    completed.from_sha256 === before.binary_sha256 && completed.to_sha256 === state.binary_sha256 &&
    completed.from_sha256 !== completed.to_sha256 && same(completed.old_process, before.process) &&
    same(completed.new_process, state.process) && !same(completed.old_process, completed.new_process) &&
    completed.installed_binary_sha256 === state.binary_sha256);
  schemaBinding(before, before.binary_sha256);
  schemaBinding(state, state.binary_sha256);
  for (const key of ['marker', 'work', 'work_identity', 'tag', 'server_id', 'admin_id', 'viewer_id', 'cluster',
    'database_oid', 'role_oid', 'media', 'libraries', 'added_viewer', 'runtime_sha256', 'browser_sha256']) {
    requireThat(Object.hasOwn(before, key) && Object.hasOwn(state, key) && same(before[key], state[key]));
  }
  // The fixed operator compares full rows, sequences, and catalog before it
  // publishes completed.json. Its database digests include captured_at, so
  // comparing those digests or row counts alone is not a preservation proof.
  musicPreservationBinding(before, state, completed);
  const reportedUpgradeKeys = ['id', 'phase', 'from_sha256', 'to_sha256', 'old_process', 'new_process', 'rollback_binary',
    'evidence_directory', 'protocol_version', 'product_version', 'preservation', 'after_preservation',
    'from_schema', 'to_schema', 'schema_artifacts'];
  requireThat(report.marker === MARKER && report.result === 'ready' && report.phase === 'ready' && report.stage === 'complete' &&
    report.schema === state.schema && report.binary_sha256 === state.binary_sha256 && same(report.process, state.process) &&
    same(report.cluster, state.cluster) && report.server_id === state.server_id &&
    report.database?.oid === state.database_oid && report.database?.role_oid === state.role_oid &&
    report.accounts?.admin === state.admin_id && report.accounts?.viewer === state.viewer_id &&
    same(report.upgrade, Object.fromEntries(reportedUpgradeKeys.map(key => [key, completed[key]]))));
  for (const key of ['movies', 'music', 'tv']) requireThat(report.libraries?.[key]?.id === state.libraries[key].id);
  const addedKeys = ['phase', 'username', 'user_id', 'receipt_path', 'receipt_sha256', 'browser_alias', 'browser_sha256', 'admin_session_revoked'];
  requireThat(same(report.added_viewer, Object.fromEntries(addedKeys.map(key => [key, state.added_viewer[key]]))));
  const operator = upgradeOperatorBinding(completed), operatorSHA = await operatorDigest(operator);
  return { beforeState: before, beforeSHA256: beforeFile.sha256, identity, directory: selection.directory, inputs,
    operators: [operator],
    files: [[beforePath, beforeFile.sha256], [completedPath, completedFile.sha256], [selection.reportPath, reportFile.sha256]],
    evidence: { method: state.schema === 25 ? 'Explicit single completed schema25 binary upgrade' : `Explicit completed schema${state.schema} fixture upgrade`,
      directory: selection.directory,
      before_state_sha256: beforeFile.sha256, completed_sha256: completedFile.sha256,
      report_path: selection.reportPath, report_sha256: reportFile.sha256, operator_sha256: operatorSHA, operator_path: operator.path,
      operator_proof_path: operatorProofBinding(operator).path,
      from_schema: completed.from_schema, to_schema: completed.to_schema, from_sha256: completed.from_sha256, to_sha256: completed.to_sha256,
      old_process: { ...completed.old_process }, new_process: { ...completed.new_process },
      preservation_authority: 'Explicitly approved completed/report hashes and the pinned operator full preservation comparison',
      full_snapshot_read: false, automatic_history_discovery: false, scan_repeated: false } };
}

async function musicUpgradeChainBinding(state, selection) {
  requireThat(record(selection) && same(Object.keys(selection).sort(), ['path', 'sha256']) && digest(selection.sha256) &&
    typeof selection.path === 'string' &&
    new RegExp(`^${ROOT}/client-music-upgrade-chain-[A-Za-z0-9_-]{1,64}\\.json$`).test(selection.path));
  const chainFile = await privateDocument(selection.path, [selection.path], true);
  requireThat(chainFile.sha256 === selection.sha256 && Array.isArray(chainFile.value) &&
    chainFile.value.length >= 1 && chainFile.value.length <= 8);
  const entries = chainFile.value;
  const selectedDirectories = new Set(), selectedReports = new Set(), selectedCompletedHashes = new Set();
  for (const entry of entries) {
    requireThat(record(entry) && same(Object.keys(entry).sort(), ['completedSHA256', 'directory', 'reportPath', 'reportSHA256']) &&
      !selectedDirectories.has(entry.directory) && !selectedReports.has(entry.reportPath) && !selectedCompletedHashes.has(entry.completedSHA256));
    selectedDirectories.add(entry.directory); selectedReports.add(entry.reportPath); selectedCompletedHashes.add(entry.completedSHA256);
  }
  // Walk only the explicitly supplied chain. Each preceding completed object
  // must equal the following hop's saved before-state.upgrade; no history scan
  // or implicit intermediate upgrade is permitted.
  let cursor = state;
  const steps = new Array(entries.length);
  for (let index = entries.length - 1; index >= 0; index -= 1) {
    const step = await musicUpgradeBinding(cursor, entries[index]);
    steps[index] = step;
    cursor = step.beforeState;
  }
  const hops = steps.map(step => step.evidence), first = hops[0], last = hops[hops.length - 1];
  const binaries = new Set([first.from_sha256]), processes = new Set([JSON.stringify(stable(first.old_process))]);
  for (let index = 0; index < hops.length; index += 1) {
    const hop = hops[index], processKey = JSON.stringify(stable(hop.new_process));
    requireThat(!binaries.has(hop.to_sha256) && !processes.has(processKey));
    if (index) requireThat(hops[index - 1].to_sha256 === hop.from_sha256 && hops[index - 1].to_schema === hop.from_schema &&
      same(hops[index - 1].new_process, hop.old_process));
    binaries.add(hop.to_sha256); processes.add(processKey);
  }
  const operators = uniqueOperatorBindings(steps.flatMap(step => step.operators));
  const operatorSHAs = [...new Set(hops.map(hop => hop.operator_sha256))];
  return { beforeState: steps[0].beforeState, beforeSHA256: steps[0].beforeSHA256, operators,
    directories: steps.map(step => ({ path: step.directory, identity: step.identity })),
    inputs: [selection.path, ...steps.flatMap(step => step.inputs)], arrayInputs: [selection.path],
    files: [[selection.path, chainFile.sha256], ...steps.flatMap(step => step.files)],
    evidence: { method: last.to_schema === 25 ? 'Explicit bounded completed schema25 upgrade chain'
      : last.to_schema === 26 ? 'Explicit bounded completed schema25/schema26 upgrade chain'
        : 'Explicit bounded completed schema25/schema26/schema27 upgrade chain', chain_path: selection.path,
      chain_sha256: chainFile.sha256, hop_count: hops.length, hops,
      before_state_sha256: first.before_state_sha256, from_schema: first.from_schema, to_schema: last.to_schema,
      from_sha256: first.from_sha256, to_sha256: last.to_sha256,
      old_process: { ...first.old_process }, new_process: { ...last.new_process },
      operator_sha256: operatorSHAs.length === 1 ? operatorSHAs[0] : null, operator_sha256s: operatorSHAs,
      full_snapshot_read: false, automatic_history_discovery: false, scan_repeated: false } };
}

// This mapping belongs only to the exact completed scan authorized for this
// fixture. The live catalog must independently match it before UI selection.
async function musicScanBinding(state, stateSHA, expectedReceiptSHA, upgradeSelection, chainSelection) {
  requireThat(upgradeSelection === undefined || chainSelection === undefined);
  if (expectedReceiptSHA === undefined) { requireThat(upgradeSelection === undefined && chainSelection === undefined); return null; }
  requireThat(expectedReceiptSHA === MUSIC_SCAN_RECEIPT_SHA && [25, 26, 27].includes(state.schema));
  const lineage = chainSelection === undefined ? await musicUpgradeBinding(state, upgradeSelection)
    : await musicUpgradeChainBinding(state, chainSelection);
  const scannedState = lineage?.beforeState ?? state, scannedStateSHA = lineage?.beforeSHA256 ?? stateSHA;
  requireThat(scannedState.schema === 25);
  const identity = await directory(MUSIC_SCAN_ROOT);
  const receiptFile = await privateDocument(MUSIC_SCAN_RECEIPT), reportFile = await privateDocument(MUSIC_SCAN_REPORT),
    ownerFile = await privateDocument(MUSIC_SCAN_OWNER);
  requireThat(receiptFile.sha256 === expectedReceiptSHA && reportFile.sha256 === MUSIC_SCAN_REPORT_SHA && ownerFile.sha256 === MUSIC_SCAN_OWNER_SHA);
  const receipt = receiptFile.value, report = reportFile.value, owner = ownerFile.value;
  const marker = 'goby-client-music-scan-m3e-v1';
  requireThat(receipt.marker === marker && receipt.phase === 'complete' && receipt.result === 'passed' && receipt.error === null &&
    receipt.schema === 25 && receipt.candidate_sha256 === scannedState.binary_sha256 && same(receipt.process, scannedState.process) &&
    receipt.fixture_tag === scannedState.tag && receipt.server_id === scannedState.server_id && receipt.fixture_state_sha256 === scannedStateSHA &&
    same(receipt.cluster, scannedState.cluster) && receipt.music_id === scannedState.libraries.music.id &&
    receipt.media_manifest_sha256 === scannedState.media?.manifest_sha256 && receipt.job_id === MUSIC_SCAN_JOB &&
    receipt.logout_status === 204 && receipt.token_readback_status === 401 && record(receipt.proof) &&
    receipt.proof.job_id === MUSIC_SCAN_JOB && receipt.proof.music_items_updated === 3 && receipt.proof.music_role_rows_added === 4 &&
    receipt.proof.audit_rows_added === 4 && receipt.proof.new_music_artist_id === 1);
  requireThat(report.marker === marker && report.result === 'passed' && report.error === null && report.job_id === MUSIC_SCAN_JOB &&
    same(report.proof, receipt.proof) && report.logout_status === 204 && report.token_readback_status === 401 &&
    report.rollback_performed === false && report.user_data_rewrites === 0 &&
    owner.marker === marker && owner.fixture_tag === scannedState.tag && owner.candidate_sha256 === scannedState.binary_sha256 &&
    same(owner.process, scannedState.process));
  await directory(MUSIC_SCAN_ROOT, owner.identity);
  const album = { id: '002c2ea2c77769ae9b0b84b6e788998b', name: 'M3e Synthetic Album',
    path: '/opt/goby-fixtures/client-m3e/Music' };
  const tracks = {
    mp3: { id: 'e2da060a0addfafde7f92c291c413f6d', name: 'M3e MP3', container: 'mp3',
      path: '/opt/goby-fixtures/client-m3e/Music/M3e Client Audio.mp3', parentId: album.id },
    flac: { id: '4d2ec259d9ae5224f46aa6f56ca2b186', name: 'M3e FLAC', container: 'flac',
      path: '/opt/goby-fixtures/client-m3e/Music/M3e Client Audio.flac', parentId: album.id },
  };
  return { identity, album, tracks, lineage,
    files: [[MUSIC_SCAN_RECEIPT, receiptFile.sha256], [MUSIC_SCAN_REPORT, reportFile.sha256], [MUSIC_SCAN_OWNER, ownerFile.sha256], ...(lineage?.files ?? [])],
    evidence: { marker, job_id: MUSIC_SCAN_JOB, receipt_sha256: receiptFile.sha256, report_sha256: reportFile.sha256,
      owner_sha256: ownerFile.sha256, directory: MUSIC_SCAN_ROOT, candidate_sha256: receipt.candidate_sha256,
      process: { ...receipt.process }, schema: 25, fixture_state_sha256: scannedStateSHA,
      upgrade_lineage: lineage?.evidence ?? null,
      album, tracks, source: 'Exact completed native Music scan receipt and approved fixture mapping',
      is_ui_acceptance: false } };
}
async function procText(filename, maximum = 16384) {
  const handle = await fs.open(filename, constants.O_RDONLY);
  const bytes = Buffer.alloc(maximum + 1);
  try {
    let size = 0;
    while (size < bytes.length) {
      const read = await handle.read(bytes, size, bytes.length - size, size);
      if (!read.bytesRead) break;
      size += read.bytesRead;
    }
    requireThat(size <= maximum);
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes.subarray(0, size));
  } finally { bytes.fill(0); await handle.close(); }
}
async function currentProcess(pid, bootId) {
  const raw = await procText(`/proc/${pid}/stat`);
  const end = raw.lastIndexOf(')');
  requireThat(end > 0 && raw.startsWith(`${pid} (`));
  const fields = raw.slice(end + 1).trim().split(/\s+/);
  requireThat(/^\d+$/.test(fields[19] ?? '') && Number.isSafeInteger(Number(fields[19])));
  return { pid, start_ticks: Number(fields[19]), boot_id: bootId };
}
async function hashExecutable(pid, expectedPath, expectedSHA, owned) {
  const filename = `/proc/${pid}/exe`;
  requireThat(await fs.readlink(filename) === expectedPath);
  const handle = await fs.open(filename, constants.O_RDONLY);
  const bytes = Buffer.alloc(65536), hash = createHash('sha256');
  try {
    const before = await handle.stat({ bigint: true });
    requireThat(before.isFile() && before.size > 0n && before.size <= 64n * 1024n * 1024n);
    if (owned) requireThat(before.uid === 0n && (before.mode & 0o777n) === 0o755n && before.nlink === 1n);
    let total = 0;
    for (;;) {
      const read = await handle.read(bytes, 0, bytes.length, total);
      if (!read.bytesRead) break;
      total += read.bytesRead; requireThat(total <= 64 * 1024 * 1024); hash.update(bytes.subarray(0, read.bytesRead));
    }
    requireThat(total === Number(before.size) && unchanged(before, await handle.stat({ bigint: true })) &&
      hash.digest('hex') === expectedSHA && await fs.readlink(filename) === expectedPath);
  } finally { bytes.fill(0); await handle.close(); }
}
async function socketOwners(pid) {
  const names = await fs.readdir(`/proc/${pid}/fd`);
  requireThat(names.length <= 4096);
  const inodes = new Set();
  for (const name of names) {
    requireThat(/^\d+$/.test(name));
    try {
      const match = /^socket:\[(\d+)\]$/.exec(await fs.readlink(`/proc/${pid}/fd/${name}`));
      if (match) inodes.add(match[1]);
    } catch (error) { if (error.code !== 'ENOENT') throw error; }
  }
  return inodes;
}
async function pinListeners(bindings) {
  const rows = [];
  for (const table of ['tcp', 'tcp6']) {
    const raw = await procText(`/proc/self/net/${table}`, JSON_LIMIT);
    for (const line of raw.trim().split('\n').slice(1)) {
      const fields = line.trim().split(/\s+/);
      if (fields[3] === '0A') rows.push({ table, address: fields[1], inode: fields[9] });
    }
  }
  for (const [port, pid] of bindings) {
    const suffix = `:${port.toString(16).toUpperCase().padStart(4, '0')}`;
    const matches = rows.filter(row => row.address?.endsWith(suffix));
    requireThat(matches.length === 1 && matches[0].table === 'tcp' && matches[0].address === `0100007F${suffix}` &&
      /^\d+$/.test(matches[0].inode ?? '') && (await socketOwners(pid)).has(matches[0].inode));
  }
}
async function pinProxyArguments(pid) {
  const raw = await procText(`/proc/${pid}/cmdline`, 65536);
  requireThat(raw.endsWith('\0'));
  const argv = raw.slice(0, -1).split('\0');
  const start = argv.findIndex(argument => argument.startsWith('--'));
  requireThat(start > 0 && argv.slice(0, start).some(argument => path.posix.basename(argument) === 'client-acceptance-proxy.py'));
  const permitted = new Set(['reference-pid', 'reference-start-ticks', 'reference-sha256', 'reference-port',
    'reference-listen', 'goby-listen', 'goby-port', 'idle-seconds', 'status-file']);
  const options = new Map();
  for (let index = start; index < argv.length; index += 1) {
    const match = /^--([a-z0-9-]+)(?:=(.*))?$/.exec(argv[index]);
    requireThat(match && permitted.has(match[1]) && !options.has(match[1]));
    const value = match[2] ?? argv[++index];
    requireThat(typeof value === 'string' && value.length > 0 && value.length <= 4096 && !value.startsWith('--'));
    options.set(match[1], value);
  }
  requireThat(options.get('reference-pid') === String(REFERENCE.pid) && options.get('reference-start-ticks') === String(REFERENCE.start_ticks) &&
    options.get('reference-sha256') === REFERENCE.sha256 && options.get('status-file') === PROXY);
  for (const [name, expected] of [['reference-port', '18097'], ['reference-listen', '18197'], ['goby-listen', '18196'], ['goby-port', '18198']]) {
    requireThat(!options.has(name) || options.get(name) === expected);
  }
  if (options.has('idle-seconds')) requireThat(/^\d+$/.test(options.get('idle-seconds')) &&
    Number(options.get('idle-seconds')) >= 30 && Number(options.get('idle-seconds')) <= 3600);
}

/** No HTTP, database reads, user creation, fixture mutation, or automatic CLI. */
export async function loadGobyAVFixture({ expectedSHA256, expectedMusicScanReceiptSHA256, musicUpgrade, musicUpgradeChain } = {}) {
  let phase = 'environment';
  try {
    requireThat(process.platform === 'linux' && process.getuid?.() === 0 && digest(expectedSHA256));
    await directory(ROOT);
    phase = 'private_documents';
    const stateFile = await privateDocument(STATE);
    phase = 'candidate_sha256';
    requireThat(stateFile.value.binary_sha256 === expectedSHA256);
    phase = 'private_documents';
    const aliasFile = await privateDocument(ALIAS);
    const receiptFile = await privateDocument(RECEIPT), proxyFile = await privateDocument(PROXY);
    const state = stateFile.value, alias = aliasFile.value, receipt = receiptFile.value, proxy = proxyFile.value;
    phase = 'fixture_binding';
    requireThat(state.marker === MARKER && state.work === ROOT && typeof state.tag === 'string' &&
      new RegExp(`^${MARKER}:[0-9a-f]{32}$`).test(state.tag) && state.phase === 'ready' && state.stage === 'complete' &&
      [24, 25, 26, 27].includes(state.schema) && identifier(state.server_id) && identifier(state.admin_id) && identifier(state.viewer_id) &&
      state.admin_id !== state.viewer_id && state.binary_sha256 === expectedSHA256 && processIdentity(state.process));
    const schemaEvidence = schemaBinding(state, expectedSHA256);
    requireThat(record(state.work_identity) && ['database_oid', 'role_oid'].every(key => Number.isSafeInteger(state[key]) && state[key] > 0) &&
      record(state.cluster) && same(Object.keys(state.cluster).sort(), ['data', 'port', 'system_identifier']) &&
      state.cluster.data === '/var/lib/postgresql/goby-workspace-v1/data' && state.cluster.port === 15432 &&
      typeof state.cluster.system_identifier === 'string' && /^[1-9]\d{0,19}$/.test(state.cluster.system_identifier));
    requireThat(!['start_pending', 'credentials_pending', 'binary_pending', 'unit_pending', 'directory_pending'].some(key => state[key]) &&
      (!Object.hasOwn(state, 'upgrade') || state.upgrade?.phase === 'complete'));
    await directory(ROOT, state.work_identity);
    requireThat(record(state.libraries) && same(Object.keys(state.libraries).sort(), ['movies', 'music', 'tv']) &&
      Object.values(state.libraries).every(library => record(library) && identifier(library.id) && library.creation_pending === false &&
        library.scan?.Status === 'completed') && new Set(Object.values(state.libraries).map(library => library.id)).size === 3);
    const added = state.added_viewer;
    requireThat(record(added) && added.marker === ACCOUNT_MARKER && added.phase === 'complete' && added.username === USERNAME &&
      identifier(added.user_id) && ![state.admin_id, state.viewer_id].includes(added.user_id) && added.evidence_directory === ACCOUNT_ROOT &&
      added.receipt_path === RECEIPT && added.browser_alias === ALIAS && added.admin_session_revoked === true &&
      digest(added.receipt_sha256) && digest(added.browser_sha256) && digest(added.credentials_sha256) &&
      record(added.evidence_identity) && processIdentity(added.source_process));
    await directory(ACCOUNT_ROOT, added.evidence_identity);
    requireThat(added.receipt_sha256 === receiptFile.sha256 && added.browser_sha256 === aliasFile.sha256 &&
      receipt.marker === ACCOUNT_MARKER && receipt.fixture_tag === state.tag && receipt.server_id === state.server_id &&
      receipt.user_id === added.user_id && receipt.username === USERNAME && receipt.is_administrator === false &&
      receipt.is_disabled === false && receipt.has_password === true && receipt.admin_session_revoked === true &&
      receipt.browser_alias === ALIAS && receipt.browser_sha256 === aliasFile.sha256 &&
      receipt.credentials_sha256 === added.credentials_sha256 && digest(receipt.creation_response_sha256) &&
      digest(receipt.before_snapshot_sha256) && digest(receipt.after_snapshot_sha256) && same(receipt.source_process, added.source_process) &&
      ['cluster', 'database_oid', 'role_oid'].every(key => Object.hasOwn(state, key) && same(receipt[key], state[key])));
    const created = receipt.creation_user;
    requireThat(record(created) && created.Id === added.user_id && created.Name === USERNAME && created.IsAdministrator === false &&
      created.IsDisabled === false && created.HasPassword === true);
    requireThat(alias.marker === MARKER && alias.account_marker === ACCOUNT_MARKER && alias.base_url === ORIGIN && alias.direct_url === DIRECT &&
      alias.receipt_path === RECEIPT && record(alias.admin) && alias.admin.username === 'm3e-client-administrator' &&
      typeof alias.admin.password === 'string' && /^[0-9a-f]{48}$/.test(alias.admin.password) &&
      record(alias.viewer) && alias.viewer.username === USERNAME && typeof alias.viewer.password === 'string' && /^[0-9a-f]{48}$/.test(alias.viewer.password) &&
      alias.viewer.userId === added.user_id && alias.viewer.serverId === state.server_id);
    phase = 'music_scan_binding';
    const musicScan = await musicScanBinding(state, stateFile.sha256, expectedMusicScanReceiptSHA256, musicUpgrade, musicUpgradeChain);
    phase = 'proxy_binding';
    requireThat(proxy.reference_pid === REFERENCE.pid && proxy.reference_start_ticks === String(REFERENCE.start_ticks) &&
      proxy.reference_executable_sha256 === REFERENCE.sha256 && proxy.reference_only === false &&
      Array.isArray(proxy.listen) && same([...proxy.listen].sort(), ['127.0.0.1:18196', '127.0.0.1:18197']) &&
      Number.isSafeInteger(proxy.pid) && proxy.pid > 1 && typeof proxy.start_ticks === 'string' && /^[1-9]\d*$/.test(proxy.start_ticks) &&
      Number.isSafeInteger(Number(proxy.start_ticks)) && Number.isSafeInteger(proxy.reference_namespace_inode));
    const pinned = freeze({ process: { ...state.process }, binary_sha256: state.binary_sha256,
      work_identity: { ...state.work_identity }, account_directory_identity: { ...added.evidence_identity },
      proxy_process: { pid: proxy.pid, start_ticks: Number(proxy.start_ticks), boot_id: state.process.boot_id },
      reference_namespace_inode: proxy.reference_namespace_inode,
      music_scan_directory_identity: musicScan?.identity ?? null,
      music_upgrade_directories: musicScan?.lineage ? musicScan.lineage.directories ??
        [{ path: musicScan.lineage.directory, identity: musicScan.lineage.identity }] : [],
      music_upgrade_operators: musicScan?.lineage?.operators ?? [],
      extra_inputs: musicScan?.lineage?.inputs ?? [],
      array_inputs: musicScan?.lineage?.arrayInputs ?? [],
      files: [[STATE, stateFile.sha256], [ALIAS, aliasFile.sha256], [RECEIPT, receiptFile.sha256], [PROXY, proxyFile.sha256], ...(musicScan?.files ?? [])] });
    const evidence = freeze({ fixture_marker: MARKER, fixture_tag: state.tag, schema: state.schema, schema_binding: schemaEvidence, server_id: state.server_id,
      account_marker: ACCOUNT_MARKER, account_id: added.user_id, process: { ...pinned.process }, binary_sha256: pinned.binary_sha256,
      fixture_state_sha256: stateFile.sha256, browser_alias_sha256: aliasFile.sha256, receipt_sha256: receiptFile.sha256,
      creation_response_sha256: receipt.creation_response_sha256, proxy_status_sha256: proxyFile.sha256,
      libraries: Object.fromEntries(Object.entries(state.libraries).map(([key, value]) => [key, value.id])),
      reference: { ...REFERENCE }, proxy_process: { ...pinned.proxy_process },
      music_scan: musicScan?.evidence ?? null,
      boundary: 'Read-only private receipt and Linux process pins; no HTTP, database, or client acceptance claim' });
    let checks = 0;
    async function assertPinned() {
      let pinPhase = 'private_documents';
      try {
        requireThat(process.platform === 'linux' && process.getuid?.() === 0);
        await directory(ROOT, pinned.work_identity); await directory(ACCOUNT_ROOT, pinned.account_directory_identity);
        if (pinned.music_scan_directory_identity) await directory(MUSIC_SCAN_ROOT, pinned.music_scan_directory_identity);
        for (const selected of pinned.music_upgrade_directories) await directory(selected.path, selected.identity);
        for (const operator of pinned.music_upgrade_operators) await operatorDigest(operator);
        for (const [filename, expected] of pinned.files) {
          requireThat((await privateDocument(filename, pinned.extra_inputs, pinned.array_inputs.includes(filename))).sha256 === expected);
        }
        pinPhase = 'process_identity';
        const bootId = (await procText('/proc/sys/kernel/random/boot_id')).trim();
        requireThat(bootId === pinned.process.boot_id && await fs.readlink('/proc/self/ns/net') === await fs.readlink('/proc/1/ns/net'));
        const referenceProcess = { pid: REFERENCE.pid, start_ticks: REFERENCE.start_ticks, boot_id: bootId };
        const processes = [pinned.process, pinned.proxy_process, referenceProcess];
        for (const expected of processes) requireThat(same(await currentProcess(expected.pid, bootId), expected));
        requireThat((await fs.stat(`/proc/${pinned.process.pid}`)).uid === 995 &&
          (await procText(`/proc/${pinned.process.pid}/cgroup`)).trim().split('\n').some(line => /^\d+:[^:]*:/.test(line) && line.split(':')[2] === CGROUP) &&
          await procText(`/proc/${pinned.process.pid}/cmdline`) === `${BINARY}\0` &&
          (await fs.stat(`/proc/${REFERENCE.pid}/ns/net`)).ino === pinned.reference_namespace_inode);
        pinPhase = 'executable_identity';
        await hashExecutable(pinned.process.pid, BINARY, pinned.binary_sha256, true);
        await hashExecutable(REFERENCE.pid, REFERENCE.executable, REFERENCE.sha256, false);
        pinPhase = 'proxy_arguments';
        await pinProxyArguments(pinned.proxy_process.pid);
        pinPhase = 'listener_identity';
        await pinListeners([[18198, pinned.process.pid], [18196, pinned.proxy_process.pid], [18197, pinned.proxy_process.pid]]);
        pinPhase = 'final_identity';
        for (const expected of processes) requireThat(same(await currentProcess(expected.pid, bootId), expected));
        requireThat((await procText('/proc/sys/kernel/random/boot_id')).trim() === bootId);
        for (const [filename, expected] of pinned.files) {
          requireThat((await privateDocument(filename, pinned.extra_inputs, pinned.array_inputs.includes(filename))).sha256 === expected);
        }
        for (const selected of pinned.music_upgrade_directories) await directory(selected.path, selected.identity);
        for (const operator of pinned.music_upgrade_operators) await operatorDigest(operator);
        checks += 1;
        return { phase: 'pinned', check: checks, fixture_state_sha256: pinned.files[0][1] };
      } catch { throw safeError(pinPhase); }
    }
    phase = 'initial_pin';
    await assertPinned();
    return Object.freeze({ origin: ORIGIN, credentialsPath: ALIAS, accountMarker: ACCOUNT_MARKER, accountId: added.user_id,
      username: USERNAME, serverId: state.server_id, album: musicScan?.album.name ?? 'Music',
      music: musicScan ? freeze({ album: musicScan.album, tracks: musicScan.tracks }) : null,
      tvLibrary: 'M3e Client Television', evidence, assertPinned });
  } catch (error) { throw safeErrors.has(error) ? error : safeError(phase); }
}
