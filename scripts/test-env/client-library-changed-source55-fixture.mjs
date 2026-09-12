/** Read-only source55 deployment authority and ownership pins for one candidate-only UI scope. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { TextDecoder } from 'node:util';
import { fileURLToPath } from 'node:url';

const WORK = '/opt/goby-test/exec-work-m3e';
const ROOT = WORK + '/client-library-changed-ui-source55-v2';
const TOOL = WORK + '/client-library-changed-source55-tool-02';
const STATE = WORK + '/client-fixture.json';
const SELF = fileURLToPath(import.meta.url);
const ORIGIN = 'http://127.0.0.1:18196', DIRECT = 'http://127.0.0.1:18198';
const USER = 'ecbbe4cb82403879bc4b4f78894c5738';
const LIBRARY = 'a9993591e72f0f2e7babcbf8b9c50790', ITEM = '268051d3ca734aefcf94e245fb25ad55';
const SERVER = 'c7cfd76b1dee728b2bad523793a37ccb';
const SOURCE_CREDENTIALS_SHA = '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790';
const BOOT = '6bdfc486-7bc8-412f-82b5-70095a09dde7';
const SOURCE = WORK + '/source-attempt-55';
const SOURCE_SHA = '7d2548603e209ebce6154853321147aeec33ccbb40cc12b3ca37418ad765937a';
const CATALOG_SHA = '8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b';
const UPGRADE_TOOL = WORK + '/client-schema28-source55-tool-05';
const UPGRADE_MARKER = 'goby-client-schema28-source55-upgrade-v1';
const CONTROLLER_UNIT = 'goby-client-library-changed-ui-source55-controller-v2.service';
const PREVIOUS = Object.freeze({
  binary_sha256: 'cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d',
  process: Object.freeze({ pid: 1264063, start_ticks: 11104222, boot_id: BOOT }),
  runtime_sha256: 'd8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df',
  state_sha256: 'd6b8872eab70848b834c50e26cc2b84e9d137dd68049a1be85d09afa811829d4',
  server_id: SERVER, base_url: ORIGIN, direct_url: DIRECT,
  source: WORK + '/source-attempt-44',
  source_manifest_sha256: 'c2c9492589360b058c6533d0719219cf85ab5bc056bd76290cee51e7dc897f8b',
});
const PREVIOUS_PROCESS = Object.freeze({ ...PREVIOUS.process, executable: '/opt/goby-client-m3e/goby',
  sha256: PREVIOUS.binary_sha256, uid: 995, cgroup: '/system.slice/goby-client-m3e.service',
  invocation: 'c0a5244ae25646c8bd92c3e3c1636575' });
const PRIMARY = Object.freeze({ pid: 762090, start_ticks: 7637121, boot_id: BOOT,
  executable: '/opt/goby-dev/goby', sha256: 'af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620',
  uid: 995, cgroup: '/system.slice/goby-foundation-test.service', invocation: 'bb94d74b475f4382a6ec6f6df181dd74' });
const PROXY_PROCESS = Object.freeze({ pid: 334022, start_ticks: 378464, boot_id: BOOT });
const PINS = Object.freeze({
  proxy: { path: WORK + '/dual-proxy-status-02.json', sha256: '769684e6c84eb4ea83c01f86c671d94d488626581c8dde664fa6f7ab7c05310f' },
  catalog: { path: SOURCE + '/internal/backuppg/catalogs/schema-28-postgresql-17.json', sha256: CATALOG_SHA },
});
const FIXTURES = Object.freeze({
  profile_receipt: { path: WORK + '/client-special-features-protocol-finalization-v1/completed.json',
    sha256: 'b443e5f6d5faceb3486d68644298b0a1527f6dcc1e4ac1109ff6d0c252fdfb36' },
  profile_report: { path: WORK + '/client-special-features-protocol-finalization-v1/report.json',
    sha256: '929e6b6fc0014e6b3afc7ce023ea981b749ce884ed6eb33c34b3617ac116855d' },
  profile_inspection: { path: WORK + '/client-special-features-finalization-inspection-v1/report.json',
    sha256: 'fdc8a38f5937964d3dba8e0e47f897108475011e70f4802e30c1803068dc4b5e' },
  music_chain: { path: WORK + '/client-music-upgrade-chain-source32.json',
    sha256: 'c235d59a317fe1b95d8d78a51ab32ca556f3ca7872e96bfea51e710e26b0c67a' },
  music_scan_receipt: { path: WORK + '/client-music-scan-v1/receipt.json',
    sha256: 'e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b' },
});
const SOURCES = Object.freeze(['client-browser-library-changed-source55.mjs', 'client-library-changed-source55-fixture.mjs',
  'client-browser-library-home.mjs', 'client-browser-cross-user.mjs', 'client-browser-session-proof.mjs',
  'client-browser-special-features-fixture.mjs', 'client-browser-goby-fixture.mjs']);
const LIBRARIES = Object.freeze([LIBRARY, '6383d20008836e137559698c29b10395', 'a34ce665fb75421ef7551570f353d705',
  '57a85c1ca5b6c7ae602c587755250b2f']);
const JSON_LIMIT = 2 * 1024 * 1024, SNAPSHOT_LIMIT = 64 * 1024 * 1024;
const SHA = /^[0-9a-f]{64}$/, ID = /^[0-9a-f]{32}$/;
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const digest = value => typeof value === 'string' && SHA.test(value) && !/^([0-9a-f])\1{63}$/.test(value);
const canonical = value => typeof value === 'bigint' ? value.toString() + 'n' : Array.isArray(value)
  ? '[' + value.map(canonical).join(',') + ']' : record(value)
    ? '{' + Object.keys(value).sort().map(key => JSON.stringify(key) + ':' + canonical(value[key])).join(',') + '}' : JSON.stringify(value);
const same = (left, right) => canonical(left) === canonical(right);
const exact = (value, keys) => record(value) && same(Object.keys(value).sort(), [...keys].sort());
const hash = value => createHash('sha256').update(value).digest('hex');
function need(value) {
  if (!value) {
    const error = new Error('library_changed_source55_binding_rejected');
    Error.captureStackTrace?.(error, need);
    throw error;
  }
}
const text = (value, maximum = 256) => typeof value === 'string' && value.length > 0 && value.length <= maximum && !/[\x00-\x1f\x7f]/.test(value);
const ownedPath = value => typeof value === 'string' && value.startsWith(WORK + '/') &&
  /^[\x21-\x7e]+$/.test(value) && !value.includes('\\') && path.posix.normalize(value) === value &&
  !value.split('/').some(part => part === '.' || part === '..');
const descriptor = value => exact(value, ['path', 'sha256']) && ownedPath(value.path) && digest(value.sha256);
function freeze(value) {
  if (value && typeof value === 'object') { for (const child of Object.values(value)) freeze(child); Object.freeze(value); }
  return value;
}

const SETUP_PHASES = Object.freeze(['arguments', 'input', 'root_identity', 'process_identity', 'source_closure', 'baseline',
  'credentials', 'fixture', 'initial_pin', 'output', 'sources', 'authority', 'private_inputs']);
const STACK_GETTER = Object.getOwnPropertyDescriptor(new Error(), 'stack')?.get;

/** Return only bounded locations in this run's reviewed JavaScript closure. */
export function libraryChangedSetupDiagnostic(error, phase) {
  const selectedPhase = SETUP_PHASES.includes(phase) ? phase : 'arguments';
  const ownValue = (value, key) => {
    if (value === null || typeof value !== 'object') return undefined;
    const property = Object.getOwnPropertyDescriptor(value, key);
    return property && Object.hasOwn(property, 'value') ? property.value : undefined;
  };
  const safeFrames = value => {
    if (!record(value) || !same(Object.keys(value).sort(), ['frames', 'phase']) || !SETUP_PHASES.includes(ownValue(value, 'phase'))) return null;
    const frames = ownValue(value, 'frames');
    const length = ownValue(frames, 'length');
    if (!Array.isArray(frames) || !Number.isSafeInteger(length) || length < 0 || length > 3) return null;
    const result = [];
    for (let index = 0; index < length; index += 1) {
      const frame = ownValue(frames, String(index));
      if (!record(frame) || !same(Object.keys(frame).sort(), ['column', 'file', 'line'])) return null;
      const file = ownValue(frame, 'file'), line = ownValue(frame, 'line'), column = ownValue(frame, 'column');
      if (!SOURCES.includes(file) || !Number.isSafeInteger(line) || line < 1 || line > 10000000 ||
        !Number.isSafeInteger(column) || column < 1 || column > 10000000) return null;
      result.push({ file, line, column });
    }
    return { phase: ownValue(value, 'phase'), frames: result };
  };
  try {
    const preserved = safeFrames(ownValue(error, 'diagnostic'));
    if (preserved !== null) return freeze(preserved);
    const stackProperty = error !== null && typeof error === 'object' ? Object.getOwnPropertyDescriptor(error, 'stack') : undefined;
    let stack = ownValue(error, 'stack');
    // Only the exact accessor captured from this module's own Error may expose V8 stack data.
    if (stack === undefined && typeof STACK_GETTER === 'function' && stackProperty?.get === STACK_GETTER)
      stack = Reflect.apply(stackProperty.get, error, []);
    const frames = [];
    if (typeof stack === 'string' && stack.length <= 65536) {
      const prefix = TOOL + '/';
      for (const row of stack.split('\n').slice(1)) {
        if (!/^\s+at /.test(row)) continue;
        const matched = /(?:\(|\s)((?:file:\/\/)?\/[^\s()]+):(\d+):(\d+)\)?$/.exec(row);
        if (!matched) continue;
        const filename = matched[1].startsWith('file://') ? matched[1].slice('file://'.length) : matched[1];
        if (!filename.startsWith(prefix)) continue;
        const file = filename.slice(prefix.length), line = Number(matched[2]), column = Number(matched[3]);
        if (!SOURCES.includes(file) || !Number.isSafeInteger(line) || line < 1 || line > 10000000 ||
          !Number.isSafeInteger(column) || column < 1 || column > 10000000) continue;
        if (!frames.some(frame => frame.file === file && frame.line === line && frame.column === column)) frames.push({ file, line, column });
        if (frames.length === 3) break;
      }
    }
    return freeze({ phase: selectedPhase, frames });
  } catch { return freeze({ phase: selectedPhase, frames: [] }); }
}

function processIdentity(value) {
  return exact(value, ['pid', 'start_ticks', 'boot_id']) && Number.isSafeInteger(value.pid) && value.pid > 1 &&
    Number.isSafeInteger(value.start_ticks) && value.start_ticks > 0 && value.boot_id === BOOT;
}

function upgradeScope(authority) {
  need(record(authority) && descriptor(authority.upgrade_report));
  const prefix = WORK + '/client-schema28-source55-upgrade-';
  const filename = authority.upgrade_report.path;
  need(filename.startsWith(prefix) && filename.endsWith('/report.json'));
  const run = filename.slice(prefix.length, -'/report.json'.length);
  need(/^\d{8}_\d{6}_[0-9a-f]{12}$/.test(run));
  return { run, output: prefix + run, unit: 'goby-client-schema28-source55-' + run.replaceAll('_', '-') + '.service' };
}

export function validateLibraryChangedSource55Input(input) {
  need(exact(input, ['marker', 'version', 'mode', 'root', 'output', 'actor', 'candidate', 'fixture',
    'expected_libraries', 'target', 'source_closure', 'authority', 'controller']) &&
    input.marker === 'goby-client-library-changed-input-v1' && input.version === 1 &&
    input.mode === 'b-movies-name-automatic-refresh' && input.root === ROOT && input.output === ROOT + '/browser');
  const candidate = input.candidate;
  need(exact(candidate, ['binary_sha256', 'process', 'runtime_sha256', 'state_sha256', 'server_id', 'base_url', 'direct_url',
    'source', 'source_manifest_sha256', 'invocation_id', 'publication']) &&
    ['binary_sha256', 'runtime_sha256', 'state_sha256'].every(key => digest(candidate[key]) && candidate[key] !== PREVIOUS[key]) &&
    candidate.binary_sha256 !== PRIMARY.sha256 && processIdentity(candidate.process) &&
    ![PREVIOUS.process.pid, PRIMARY.pid, PROXY_PROCESS.pid].includes(candidate.process.pid) &&
    candidate.process.start_ticks > PREVIOUS.process.start_ticks && candidate.server_id === SERVER &&
    candidate.base_url === ORIGIN && candidate.direct_url === DIRECT && candidate.source === SOURCE &&
    candidate.source_manifest_sha256 === SOURCE_SHA && typeof candidate.invocation_id === 'string' && ID.test(candidate.invocation_id) &&
    !/^([0-9a-f])\1{31}$/.test(candidate.invocation_id) && ![PREVIOUS_PROCESS.invocation, PRIMARY.invocation].includes(candidate.invocation_id) &&
    typeof candidate.publication === 'string' && /^[0-9a-f]{40}$/.test(candidate.publication) &&
    !/^([0-9a-f])\1{39}$/.test(candidate.publication) && candidate.publication !== '35ae3d000f812fa18d234921cedb3c33d88190e0');
  need(same(input.fixture, FIXTURES));
  need(exact(input.actor, ['slot', 'user_id', 'credentials', 'account_key', 'source_credentials_sha256']) &&
    input.actor.slot === 'B' && input.actor.user_id === USER && input.actor.account_key === 'viewer' &&
    input.actor.source_credentials_sha256 === SOURCE_CREDENTIALS_SHA && descriptor(input.actor.credentials) &&
    input.actor.credentials.path === ROOT + '/viewer-credentials.json');
  const authority = input.authority;
  need(exact(authority, ['upgrade_intent', 'upgrade_report', 'upgrade_attestation', 'current_snapshot', 'before_snapshot']) &&
    Object.values(authority).every(descriptor));
  const scope = upgradeScope(authority);
  need(authority.upgrade_intent.path === UPGRADE_TOOL + '/intent.json' &&
    authority.upgrade_attestation.path === scope.output + '/attestation.json' &&
    authority.current_snapshot.path === scope.output + '/after-full.json' &&
    authority.before_snapshot.path === ROOT + '/before-full.json' &&
    authority.current_snapshot.sha256 !== '738eb9717506bec997456830e70d56f0867f9cf8159939ea3733e039cadb9811');
  need(exact(input.source_closure, SOURCES.map(name => TOOL + '/' + name)) && Object.values(input.source_closure).every(digest));
  need(Array.isArray(input.expected_libraries) && input.expected_libraries.length === 4 &&
    input.expected_libraries.every(value => exact(value, ['id', 'name']) && typeof value.id === 'string' && ID.test(value.id) && text(value.name)) &&
    same(input.expected_libraries.map(value => value.id).sort(), [...LIBRARIES].sort()) &&
    input.expected_libraries.find(value => value.id === LIBRARY)?.name === 'M3e Client Movies');
  const target = input.target;
  need(exact(target, ['id', 'library_id', 'name', 'type', 'parent_id', 'root_id', 'relative_path']) &&
    target.id === ITEM && target.library_id === LIBRARY && target.type === 'Movie' && text(target.name) &&
    typeof target.parent_id === 'string' && ID.test(target.parent_id) && typeof target.root_id === 'string' && ID.test(target.root_id) &&
    text(target.relative_path, 4096) &&
    !target.relative_path.startsWith('/') && !target.relative_path.includes('\\') &&
    !target.relative_path.split('/').some(part => part === '' || part === '.' || part === '..'));
  const controller = input.controller;
  need(exact(controller, ['pid', 'start_ticks', 'boot_id', 'unit']) && Number.isSafeInteger(controller.pid) && controller.pid > 1 &&
    ![candidate.process.pid, PREVIOUS.process.pid, PRIMARY.pid, PROXY_PROCESS.pid].includes(controller.pid) &&
    typeof controller.start_ticks === 'string' && /^[1-9]\d*$/.test(controller.start_ticks) &&
    Number.isSafeInteger(Number(controller.start_ticks)) && controller.boot_id === BOOT && controller.unit === CONTROLLER_UNIT);
  return true;
}
export function validateLibraryChangedSource55Viewer(value) {
  need(exact(value, ['marker', 'slot', 'user_id', 'base_url', 'direct_url', 'viewer']) &&
    value.marker === 'goby-client-library-changed-viewer-v1' && value.slot === 'B' && value.user_id === USER &&
    value.base_url === ORIGIN && value.direct_url === DIRECT && exact(value.viewer, ['username', 'password']) &&
    value.viewer.username === 'm3e-client-viewer' && typeof value.viewer.password === 'string' && /^[0-9a-f]{48}$/.test(value.viewer.password));
  return true;
}

function sourceBinding(value, publication) {
  need(exact(value, ['marker', 'schema', 'source', 'source_manifest_sha256', 'catalog_sha256', 'publication']) &&
    value.marker === 'goby-client-schema28-source-v1' && value.schema === 28 && value.source === SOURCE &&
    value.source_manifest_sha256 === SOURCE_SHA && value.catalog_sha256 === CATALOG_SHA && value.publication === publication);
}

function catalogColumns(catalog) {
  need(record(catalog) && record(catalog.catalog) && Array.isArray(catalog.catalog.Tables) && catalog.catalog.Tables.length === 35 &&
    Array.isArray(catalog.objects));
  const relations = new Map(), columnFacts = new Map();
  for (const row of catalog.objects.filter(value => value?.kind === 'relation')) {
    need(record(row) && typeof row.name === 'string' && /^[a-z][a-z0-9_]*$/.test(row.name) && !relations.has(row.name) &&
      record(row.value) && ['r', 'i', 'S'].includes(row.value.kind));
    relations.set(row.name, row.value.kind);
  }
  for (const row of catalog.objects.filter(value => value?.kind === 'column')) {
    const matched = typeof row.name === 'string' ? /^([a-z][a-z0-9_]*)\.([0-9]{5})$/.exec(row.name) : null;
    need(matched && Number(matched[2]) > 0 && relations.has(matched[1]) && !columnFacts.has(row.name) &&
      record(row.value) && typeof row.value.dropped === 'boolean');
    // Index and sequence columns remain catalog evidence but are not table row projections.
    columnFacts.set(row.name, { parent: matched[1], kind: relations.get(matched[1]), fact: row });
  }
  const result = Object.create(null);
  for (const table of catalog.catalog.Tables) {
    need(record(table) && typeof table.Name === 'string' && /^[a-z][a-z0-9_]*$/.test(table.Name) && !Object.hasOwn(result, table.Name) &&
      Array.isArray(table.Columns));
    const prefix = table.Name + '.';
    need(relations.get(table.Name) === 'r');
    const facts = [...columnFacts.values()].filter(value => value.parent === table.Name && value.kind === 'r' &&
      value.fact.value.dropped === false).map(value => value.fact).sort((left, right) => left.name.localeCompare(right.name));
    // Full ordinal catalog facts include generated columns omitted by restore projections.
    need(facts.length > 0 && facts.every(row => /^\d{5}$/.test(row.name.slice(prefix.length))) &&
      new Set(facts.map(row => row.name)).size === facts.length);
    const columns = facts.map(row => row.value.name);
    need(columns.every(value => typeof value === 'string' && /^[a-z][a-z0-9_]*$/.test(value)) &&
      new Set(columns).size === columns.length && table.Columns.every(value => columns.includes(value)));
    result[table.Name] = columns;
  }
  need([...columnFacts.values()].every(value => value.kind === 'r' ? Object.hasOwn(result, value.parent) : ['i', 'S'].includes(value.kind)));
  need(same([...relations.entries()].filter(([, kind]) => kind === 'r').map(([name]) => name).sort(),
    Object.keys(result).sort()));
  return result;
}

function catalogSequences(catalog, database) {
  const sequences = catalog.catalog.Sequences, facts = catalog.objects.filter(row => row?.kind === 'sequence');
  need(Array.isArray(sequences) && sequences.length === 5 && facts.length === 5 && record(database.sequences));
  const names = sequences.map(value => value.Name);
  need(names.every(value => typeof value === 'string' && /^[a-z][a-z0-9_]*$/.test(value)) && new Set(names).size === 5 &&
    same(Object.keys(database.sequences).sort(), [...names].sort()) && same(facts.map(value => value.name).sort(), [...names].sort()) &&
    same(catalog.objects.filter(row => row?.kind === 'relation' && row.value?.kind === 'S').map(row => row.name).sort(), [...names].sort()));
  const integral = value => typeof value === 'bigint' || Number.isSafeInteger(value);
  for (const sequence of sequences) {
    const definition = facts.find(value => value.name === sequence.Name).value, value = database.sequences[sequence.Name];
    need(record(definition) && definition.owned_by === sequence.Table + '.' + sequence.Column &&
      same(definition.min, sequence.MinValue) && same(definition.max, sequence.MaxValue) && same(definition.increment, sequence.Increment) &&
      [sequence.MinValue, sequence.MaxValue, sequence.Increment].every(integral) && sequence.Increment > 0 &&
      Array.isArray(sequence.Consumers) && sequence.Consumers.length > 0 &&
      exact(value, ['last_value', 'is_called']) && integral(value.last_value) && typeof value.is_called === 'boolean');
    const last = BigInt(value.last_value), minimum = BigInt(sequence.MinValue), maximum = BigInt(sequence.MaxValue);
    const next = last + (value.is_called ? BigInt(sequence.Increment) : 0n);
    need(last >= minimum && last <= maximum && next <= maximum && sequence.Consumers.every(consumer =>
      exact(consumer, ['Table', 'Column']) && Array.isArray(database.tables[consumer.Table]) &&
      database.tables[consumer.Table].every(row => integral(row[consumer.Column]) && BigInt(row[consumer.Column]) < next)));
  }
}

function validateSchema28Snapshots(current, before, catalog, candidate) {
  need(current?.schema === 28 && before?.schema === 28 && current.runtime_sha256 === candidate.runtime_sha256 &&
    current.browser_sha256 === SOURCE_CREDENTIALS_SHA && record(current.database) && record(before.database) &&
    record(catalog) && catalog.version === 28 && catalog.postgresql_major === 17 && Array.isArray(catalog.objects) &&
    same(current.database.catalog, catalog.objects) && current.database.unsupported === false);
  const withoutTime = snapshot => {
    need(record(snapshot.database.metadata) && typeof snapshot.database.metadata.captured_at === 'string' &&
      Number.isFinite(Date.parse(snapshot.database.metadata.captured_at)));
    const metadata = { ...snapshot.database.metadata }; delete metadata.captured_at;
    return { ...snapshot, database: { ...snapshot.database, metadata } };
  };
  need(same(withoutTime(current), withoutTime(before)) &&
    Date.parse(before.database.metadata.captured_at) > Date.parse(current.database.metadata.captured_at));
  const tables = current.database.tables, columns = current.database.metadata.columns, trustedColumns = catalogColumns(catalog);
  need(record(tables) && Object.keys(tables).length === 35 && record(columns) && same(columns, trustedColumns) &&
    same(Object.keys(columns).sort(), Object.keys(tables).sort()));
  for (const [name, rows] of Object.entries(tables)) {
    const projection = columns[name];
    need(Array.isArray(rows) && Array.isArray(projection) && projection.length > 0 &&
      projection.every(value => typeof value === 'string' && /^[a-z][a-z0-9_]*$/.test(value)) &&
      new Set(projection).size === projection.length && rows.every(row => exact(row, projection)));
  }
  catalogSequences(catalog, current.database);
  need(tables.library_roots?.length === 4 && tables.libraries?.length === 4 && tables.items?.length === 22 &&
    tables.play_sessions?.length === 26 && tables.user_item_data?.length === 7 &&
    tables.sessions?.length === 75 && tables.devices?.length === 64 && tables.activity_entries?.length === 167);
  need(same(columns.library_roots.slice(-4), ['binding_revision', 'storage_binding', 'bound_at', 'bound_by']) &&
    same(columns.activity_entries.slice(-2), ['previous_revision', 'observation_fingerprint']) &&
    tables.library_roots.every(row => row.binding_revision === 1 && row.storage_binding === null &&
      row.bound_at === null && row.bound_by === null && row.relative_path === '.' && row.allowed_path === row.path) &&
    tables.activity_entries.every(row => row.previous_revision === 0 && row.observation_fingerprint === ''));
  need(Array.isArray(catalog.migrations) && catalog.migrations.length === 28 &&
    Array.isArray(tables.schema_migrations) && tables.schema_migrations.length === 28 &&
    same([...tables.schema_migrations].sort((left, right) => left.version - right.version).map(row => ({ version: row.version, name: row.name })),
      catalog.migrations.map(row => ({ version: row.version, name: row.name }))));
  return tables;
}

function validateUpgradeIntent(input, intent) {
  const scope = upgradeScope(input.authority), candidate = input.candidate;
  need(record(intent) && intent.marker === UPGRADE_MARKER && intent.version === 1 && intent.run_id === scope.run &&
    intent.tool === UPGRADE_TOOL && intent.output === scope.output && exact(intent.controller, ['unit']) && intent.controller.unit === scope.unit &&
    exact(intent.candidate, ['state', 'baseline', 'process', 'invocation_id']) &&
    same(intent.candidate.state, { path: STATE, sha256: PREVIOUS.state_sha256 }) &&
    same(intent.candidate.baseline, { path: WORK + '/collection-folder-contract-v1/after-full.json',
      sha256: '2659f45dfa82d8568b07375d04c08cd4ba216defcfa2a4a1c269867b176573be' }) &&
    same(intent.candidate.process, PREVIOUS.process) && intent.candidate.invocation_id === PREVIOUS_PROCESS.invocation);
  const source = intent.source;
  need(exact(source, ['root', 'manifest_sha256', 'full_report', 'terminal', 'binary', 'publication']) &&
    source.root === SOURCE && source.manifest_sha256 === SOURCE_SHA &&
    descriptor(source.full_report) && source.full_report.path === WORK + '/client-backup-run-20260912_084241_db776aacc1a7/report.json' &&
    descriptor(source.terminal) && source.terminal.path === WORK + '/collection-folder-source55-full-execution-01/terminal.json' &&
    descriptor(source.publication) && source.publication.path === UPGRADE_TOOL + '/publication.json' &&
    exact(source.binary, ['path', 'sha256', 'bytes']) && source.binary.sha256 === candidate.binary_sha256 &&
    source.binary.path === WORK + '/client-backup-run-20260912_084241_db776aacc1a7/tmp/goby-linux-amd64' &&
    Number.isSafeInteger(source.binary.bytes) && source.binary.bytes > 1024 * 1024 && source.binary.bytes <= 128 * 1024 * 1024);
  return scope;
}

export function validateLibraryChangedSource55Documents(input, documents) {
  validateLibraryChangedSource55Input(input);
  need(exact(documents, ['state', 'beforeState', 'intent', 'report', 'attestation', 'publication', 'catalog', 'current', 'before',
    'profileReceipt', 'profileReport', 'profileInspection', 'musicChain', 'musicScanReceipt', 'proxy']));
  const { state, beforeState, intent, report, attestation, publication, catalog, current, before, profileReceipt, profileReport,
    profileInspection, musicChain, musicScanReceipt, proxy } = documents;
  const candidate = input.candidate, authority = input.authority, scope = validateUpgradeIntent(input, intent);
  need(record(state) && state.marker === 'goby-m3e-client-acceptance-v1' && state.work === WORK && state.schema === 28 &&
    state.phase === 'ready' && state.stage === 'complete' && state.server_id === SERVER && state.viewer_id === USER &&
    state.binary_sha256 === candidate.binary_sha256 && state.runtime_sha256 === candidate.runtime_sha256 &&
    state.browser_sha256 === SOURCE_CREDENTIALS_SHA && same(state.process, candidate.process) && record(state.work_identity));
  sourceBinding(state.schema28_source, candidate.publication);
  need(record(beforeState) && beforeState.schema === 27 && beforeState.phase === 'ready' && beforeState.stage === 'complete' &&
    beforeState.binary_sha256 === PREVIOUS.binary_sha256 && beforeState.runtime_sha256 === PREVIOUS.runtime_sha256 &&
    same(beforeState.process, PREVIOUS.process) && beforeState.schema27_source?.source === PREVIOUS.source &&
    beforeState.schema27_source.source_manifest_sha256 === PREVIOUS.source_manifest_sha256 &&
    beforeState.notifications_upgrade?.marker === 'goby-client-notifications-source44-continuation-v1');
  const allowed = new Set(['schema', 'phase', 'stage', 'process', 'binary_sha256', 'binary_identity', 'runtime_sha256', 'upgrade', 'upgrade_history']);
  need(same(Object.keys(state).sort(), [...Object.keys(beforeState), 'schema28_upgrade', 'schema28_source'].sort()) &&
    Object.entries(beforeState).every(([key, value]) => allowed.has(key) || same(state[key], value)));
  const upgrade = state.upgrade;
  need(exact(upgrade, ['marker', 'phase', 'from_schema', 'to_schema', 'output', 'intent_sha256', 'old_process', 'new_process',
    'from_sha256', 'to_sha256', 'publication', 'backup_sha256', 'rehearsal', 'service']) &&
    upgrade.marker === UPGRADE_MARKER && upgrade.phase === 'complete' && upgrade.from_schema === 27 && upgrade.to_schema === 28 &&
    upgrade.output === scope.output && upgrade.intent_sha256 === authority.upgrade_intent.sha256 &&
    same(upgrade.old_process, PREVIOUS.process) && same(upgrade.new_process, candidate.process) &&
    upgrade.from_sha256 === PREVIOUS.binary_sha256 && upgrade.to_sha256 === candidate.binary_sha256 &&
    upgrade.publication === candidate.publication && digest(upgrade.backup_sha256) &&
    descriptor(upgrade.rehearsal) && upgrade.rehearsal.path === scope.output + '/rehearsal-completed.json' &&
    same(state.schema28_upgrade, upgrade) && Array.isArray(beforeState.upgrade_history) &&
    same(state.upgrade_history, [...beforeState.upgrade_history, upgrade]));
  need(record(report) && report.marker === UPGRADE_MARKER && report.version === 1 && report.run_id === scope.run &&
    report.intent_sha256 === authority.upgrade_intent.sha256 && report.status === 'awaiting_outer_attestation' && report.schema === 28 &&
    report.state_sha256 === candidate.state_sha256 && same(report.binary, intent.source.binary) &&
    report.publication === candidate.publication && same(report.new_process, candidate.process) &&
    report.http_requests === 5 && same(report.service_actions, ['stop', 'start']) && report.candidate_web === '/opt/goby-client-m3e/admin' &&
    ['preserved_rows_sequences_credentials_recovery', 'primary_unchanged', 'shared_web_unchanged', 'rehearsal_removed',
      'hba_restored_exactly'].every(key => report[key] === true) &&
    ['client_acceptance', 'automatic_retry', 'automatic_rollback'].every(key => report[key] === false) &&
    record(report.evidence) && same(report.evidence['after-full.json'], authority.current_snapshot) &&
    same(report.evidence['before-state.json'], { path: scope.output + '/before-state.json', sha256: PREVIOUS.state_sha256 }) &&
    same(report.evidence['rehearsal-completed.json'], upgrade.rehearsal));
  need(Object.entries(report.evidence).every(([name, item]) => /^[a-z0-9][a-z0-9.-]{0,95}$/.test(name) && descriptor(item) &&
    [scope.output, scope.output + '/private'].includes(path.posix.dirname(item.path))));
  const service = report.candidate_service;
  need(exact(service, ['MainPID', 'InvocationID', 'ActiveState', 'SubState']) && service.MainPID === String(candidate.process.pid) &&
    service.InvocationID === candidate.invocation_id && service.ActiveState === 'active' && service.SubState === 'running' &&
    same(upgrade.service, service));
  const controller = report.controller, processFact = controller?.process;
  need(exact(controller, ['unit', 'invocation_id', 'process']) && controller.unit === scope.unit &&
    typeof controller.invocation_id === 'string' && ID.test(controller.invocation_id) &&
    !/^([0-9a-f])\1{31}$/.test(controller.invocation_id) && controller.invocation_id !== candidate.invocation_id &&
    exact(processFact, ['pid', 'start_ticks', 'boot_id', 'uid', 'exe', 'cgroup', 'namespace']) &&
    processIdentity({ pid: processFact.pid, start_ticks: processFact.start_ticks, boot_id: processFact.boot_id }) &&
    ![input.controller.pid, candidate.process.pid, PREVIOUS.process.pid, PRIMARY.pid, PROXY_PROCESS.pid].includes(processFact.pid) &&
    processFact.uid === 0 && text(processFact.exe, 4096) && processFact.exe.startsWith('/') &&
    path.posix.normalize(processFact.exe) === processFact.exe && !processFact.exe.includes('\\') &&
    processFact.cgroup === '/system.slice/' + scope.unit && /^mnt:\[\d+\]$/.test(processFact.namespace));
  need(exact(attestation, ['marker', 'version', 'run_id', 'intent_sha256', 'status', 'report_sha256', 'controller',
    'recursive_cgroup_empty', 'state_sha256', 'binary', 'schema', 'rows_sequences_credentials_recovery_preserved',
    'primary_unchanged', 'rehearsal_removed', 'hba_restored_exactly', 'client_acceptance']) &&
    attestation.marker === UPGRADE_MARKER && attestation.version === 1 && attestation.run_id === scope.run &&
    attestation.intent_sha256 === authority.upgrade_intent.sha256 && attestation.status === 'passed' &&
    attestation.report_sha256 === authority.upgrade_report.sha256 && attestation.state_sha256 === candidate.state_sha256 &&
    same(attestation.binary, intent.source.binary) && attestation.schema === 28 && attestation.client_acceptance === false &&
    ['recursive_cgroup_empty', 'rows_sequences_credentials_recovery_preserved', 'primary_unchanged', 'rehearsal_removed',
      'hba_restored_exactly'].every(key => attestation[key] === true));
  const terminal = attestation.controller;
  need(exact(terminal, ['MainPID', 'InvocationID', 'ActiveState', 'SubState', 'Result', 'ExecMainStatus', 'ControlGroup',
    'Description', 'RemainAfterExit']) && terminal.MainPID === '0' && terminal.InvocationID === controller.invocation_id &&
    terminal.ActiveState === 'active' && terminal.SubState === 'exited' && terminal.Result === 'success' && terminal.ExecMainStatus === '0' &&
    terminal.RemainAfterExit === 'yes' && terminal.Description === UPGRADE_MARKER + ':' + scope.run &&
    ['', '/system.slice/' + scope.unit].includes(terminal.ControlGroup));
  need(exact(publication, ['marker', 'commit', 'source_manifest_sha256', 'binary_sha256', 'full_report_sha256', 'full_terminal_sha256']) &&
    publication.marker === 'goby-source55-publication-v1' && publication.commit === candidate.publication &&
    publication.source_manifest_sha256 === SOURCE_SHA && publication.binary_sha256 === candidate.binary_sha256 &&
    publication.full_report_sha256 === intent.source.full_report.sha256 && publication.full_terminal_sha256 === intent.source.terminal.sha256);
  // These are historical receipt anchors, not current source32 runtime authority.
  need(profileReceipt?.marker === 'goby-client-special-features-fixture-v1' && profileReceipt.phase === 'complete' &&
    profileReceipt.schema === 27 && profileReceipt.profile_version === 3 &&
    state.special_features_profile?.receipt_path === FIXTURES.profile_receipt.path &&
    state.special_features_profile.receipt_sha256 === FIXTURES.profile_receipt.sha256 &&
    profileReport?.result === 'passed' && profileReport.phase === 'complete' &&
    profileReport.receipt_sha256 === FIXTURES.profile_receipt.sha256 && profileInspection?.result === 'passed' &&
    profileInspection.phase === 'complete' && profileInspection.schema === 27 &&
    profileInspection.receipt_sha256 === FIXTURES.profile_receipt.sha256 &&
    Array.isArray(musicChain) && musicChain.length === 5 &&
    musicChain[4].completedSHA256 === profileReceipt.upgrade?.completed_sha256 &&
    musicChain[4].reportSHA256 === profileReceipt.upgrade?.report_sha256 &&
    musicScanReceipt?.schema === 25 && musicScanReceipt.phase === 'complete' && musicScanReceipt.result === 'passed' &&
    musicScanReceipt.logout_status === 204 && musicScanReceipt.token_readback_status === 401);
  need(proxy?.pid === PROXY_PROCESS.pid && String(proxy.start_ticks) === String(PROXY_PROCESS.start_ticks) &&
    proxy.reference_only === false && Array.isArray(proxy.listen) && proxy.listen.length <= 4 &&
    proxy.listen.filter(value => value === '127.0.0.1:18196').length === 1);
  const tables = validateSchema28Snapshots(current, before, catalog, candidate);
  const user = tables.users?.find(value => value.id === USER), target = tables.items.find(value => value.id === ITEM);
  need(user?.is_administrator === false && user.is_disabled === false && user.management_revision === 5 && target?.type === 'Movie' &&
    target.is_folder === false && target.library_id === LIBRARY && target.name === input.target.name &&
    target.parent_id === input.target.parent_id && target.root_id === input.target.root_id && target.relative_path === input.target.relative_path &&
    same(tables.libraries.map(value => ({ id: value.id, name: value.name })).sort((a, b) => a.id.localeCompare(b.id)),
      [...input.expected_libraries].sort((a, b) => a.id.localeCompare(b.id))));
  return true;
}
/** Reject duplicate decoded keys and retain exact large integral ledger values. */
export function parseSource55JSON(raw, allowArray = false) {
  need(typeof raw === 'string' && Buffer.byteLength(raw, 'utf8') <= SNAPSHOT_LIMIT);
  let position = 0, nodes = 0;
  const whitespace = () => { while (position < raw.length && /[ \t\r\n]/.test(raw[position])) position += 1; };
  function string() {
    const start = position++; let finished = false;
    while (position < raw.length) {
      if (raw[position] === '\\') { position += 2; continue; }
      if (raw[position++] === '"') { finished = true; break; }
    }
    need(finished);
    try { return JSON.parse(raw.slice(start, position)); } catch { need(false); }
  }
  function value(depth) {
    need(depth <= 64 && ++nodes <= 500000); whitespace();
    if (raw[position] === '"') return string();
    const opening = raw[position];
    if (opening === '{' || opening === '[') {
      position += 1; whitespace();
      const closing = opening === '{' ? '}' : ']', result = opening === '{' ? Object.create(null) : [];
      const keys = new Set();
      if (raw[position] === closing) { position += 1; return result; }
      for (;;) {
        let key;
        if (opening === '{') {
          need(raw[position] === '"'); key = string(); need(!keys.has(key)); keys.add(key);
          whitespace(); need(raw[position++] === ':');
        }
        const child = value(depth + 1);
        if (opening === '{') result[key] = child; else result.push(child);
        whitespace(); if (raw[position] === closing) { position += 1; return result; }
        need(raw[position++] === ','); whitespace();
      }
    }
    const token = /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(raw.slice(position));
    need(token); position += token[0].length;
    if (token[0] === 'true') return true;
    if (token[0] === 'false') return false;
    if (token[0] === 'null') return null;
    const number = Number(token[0]); need(Number.isFinite(number));
    return /^-?\d+$/.test(token[0]) && !Number.isSafeInteger(number) ? BigInt(token[0]) : number;
  }
  const result = value(0); whitespace();
  need(position === raw.length && (record(result) || allowArray && Array.isArray(result)));
  return result;
}

const unchanged = (before, after) => ['dev', 'ino', 'mode', 'uid', 'gid', 'nlink', 'size', 'mtimeNs', 'ctimeNs']
  .every(key => before[key] === after[key]);
const directoryIdentity = info => ({ device: String(info.dev), inode: String(info.ino), mode: String(info.mode), uid: String(info.uid), gid: String(info.gid) });

async function pinParents(filename, directories) {
  const names = []; let parent = path.posix.dirname(filename);
  for (;;) { names.unshift(parent); if (parent === '/') break; parent = path.posix.dirname(parent); }
  for (const name of names) {
    const info = await fs.lstat(name, { bigint: true });
    need(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0n && info.gid === 0n && (info.mode & 0o022n) === 0n);
    if ([WORK, ROOT, TOOL, UPGRADE_TOOL].includes(name)) need((info.mode & 0o7777n) === 0o700n);
    const identity = directoryIdentity(info);
    if (directories.has(name)) need(same(directories.get(name), identity)); else directories.set(name, identity);
  }
}

async function protectedFile(filename, expectedSHA, directories, json = true, maximum = JSON_LIMIT, modes = [0o600n], expectedIdentity) {
  need(ownedPath(filename) && digest(expectedSHA));
  await pinParents(filename, directories);
  need(await fs.realpath(filename) === filename);
  const before = await fs.lstat(filename, { bigint: true });
  need(before.isFile() && !before.isSymbolicLink() && before.uid === 0n && before.gid === 0n && before.nlink === 1n &&
    modes.includes(before.mode & 0o7777n) && before.size > 0n && before.size <= BigInt(maximum));
  if (expectedIdentity) need(unchanged(expectedIdentity, before));
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  const bytes = Buffer.alloc(65536), chunks = [], sha = createHash('sha256');
  try {
    need(unchanged(before, await handle.stat({ bigint: true })));
    let size = 0;
    for (;;) {
      const part = await handle.read(bytes, 0, bytes.length, size);
      if (!part.bytesRead) break;
      size += part.bytesRead; need(size <= maximum); sha.update(bytes.subarray(0, part.bytesRead));
      if (json) chunks.push(Buffer.from(bytes.subarray(0, part.bytesRead)));
    }
    need(size === Number(before.size) && sha.digest('hex') === expectedSHA &&
      unchanged(before, await handle.stat({ bigint: true })) && unchanged(before, await fs.lstat(filename, { bigint: true })) &&
      await fs.realpath(filename) === filename);
    await pinParents(filename, directories);
    let value;
    if (json) {
      const raw = Buffer.concat(chunks);
      try { value = parseSource55JSON(new TextDecoder('utf-8', { fatal: true }).decode(raw), true); } finally { raw.fill(0); }
    }
    return { path: filename, sha256: expectedSHA, value, identity: before, modes, maximum };
  } finally { bytes.fill(0); for (const chunk of chunks) chunk.fill(0); await handle.close(); }
}

/** Read only one of the two already declared complete snapshots without numeric coercion. */
export async function readLibraryChangedSource55Snapshot(input, key) {
  try {
    validateLibraryChangedSource55Input(input);
    need(key === 'current_snapshot' || key === 'before_snapshot');
    need(process.platform === 'linux' && process.getuid?.() === 0 && process.getgid?.() === 0 &&
      SELF === TOOL + '/client-library-changed-source55-fixture.mjs');
    const item = input.authority[key];
    const file = await protectedFile(item.path, item.sha256, new Map(), true, SNAPSHOT_LIMIT, [0o600n]);
    need(record(file.value));
    return file.value;
  } catch (error) {
    throw Object.assign(new Error('library_changed_source55_snapshot_read_failed'),
      { diagnostic: libraryChangedSetupDiagnostic(error, 'baseline') });
  }
}

async function procText(filename, maximum = 65536) {
  const handle = await fs.open(filename, constants.O_RDONLY), bytes = Buffer.alloc(maximum + 1);
  try {
    let size = 0;
    while (size < bytes.length) {
      const part = await handle.read(bytes, size, bytes.length - size, size);
      if (!part.bytesRead) break; size += part.bytesRead;
    }
    need(size <= maximum);
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes.subarray(0, size));
  } finally { bytes.fill(0); await handle.close(); }
}

async function checkProcess(expected) {
  const raw = await procText(`/proc/${expected.pid}/stat`), end = raw.lastIndexOf(')');
  need(raw.startsWith(`${expected.pid} (`) && end > 0);
  const fields = raw.slice(end + 1).trim().split(/\s+/);
  need(fields[19] === String(expected.start_ticks) && !['Z', 'X', 'x'].includes(fields[0]));
}

async function hashExecutable(expected, identities) {
  const filename = `/proc/${expected.pid}/exe`;
  need(await fs.readlink(filename) === expected.executable);
  const handle = await fs.open(filename, constants.O_RDONLY), bytes = Buffer.alloc(65536), sha = createHash('sha256');
  try {
    const before = await handle.stat({ bigint: true });
    need(before.isFile() && before.uid === 0n && before.gid === 0n && before.nlink === 1n &&
      (before.mode & 0o7777n) === 0o755n && before.size > 0n && before.size <= 128n * 1024n * 1024n);
    if (identities.has(filename)) need(unchanged(identities.get(filename), before)); else identities.set(filename, before);
    let size = 0;
    for (;;) {
      const part = await handle.read(bytes, 0, bytes.length, size);
      if (!part.bytesRead) break;
      size += part.bytesRead; need(size <= 128 * 1024 * 1024); sha.update(bytes.subarray(0, part.bytesRead));
    }
    need(size === Number(before.size) && sha.digest('hex') === expected.sha256 &&
      unchanged(before, await handle.stat({ bigint: true })) && await fs.readlink(filename) === expected.executable);
  } finally { bytes.fill(0); await handle.close(); }
}

export function validateSource55ProxyArguments(argv) {
  need(Array.isArray(argv) && argv.length >= 3 && argv.length <= 32 && argv.every(value => text(value, 4096)));
  const start = argv.findIndex(value => value.startsWith('--'));
  need(start > 0 && start <= 4 && argv.slice(0, start).filter(value => value.startsWith('/opt/goby-test/') &&
    path.posix.normalize(value) === value && path.posix.basename(value) === 'client-acceptance-proxy.py').length === 1);
  const allowed = new Set(['reference-pid', 'reference-start-ticks', 'reference-sha256', 'reference-port', 'reference-listen',
    'goby-listen', 'goby-port', 'idle-seconds', 'status-file']);
  const options = new Map();
  for (let index = start; index < argv.length; index += 1) {
    const matched = /^--([a-z0-9-]+)(?:=(.*))?$/.exec(argv[index]);
    need(matched && allowed.has(matched[1]) && !options.has(matched[1]));
    const value = matched[2] ?? argv[++index]; need(text(value, 4096) && !value.startsWith('--'));
    options.set(matched[1], value);
  }
  // Reference options are opaque launch arguments. Never resolve their values.
  need(options.get('status-file') === PINS.proxy.path &&
    (!options.has('goby-listen') || options.get('goby-listen') === '18196') &&
    (!options.has('goby-port') || options.get('goby-port') === '18198'));
  if (options.has('idle-seconds')) need(/^\d+$/.test(options.get('idle-seconds')) &&
    Number(options.get('idle-seconds')) >= 30 && Number(options.get('idle-seconds')) <= 3600);
  return true;
}

async function socketOwners(pid) {
  const names = await fs.readdir(`/proc/${pid}/fd`); need(names.length <= 4096);
  const result = new Set();
  for (const name of names) {
    need(/^\d+$/.test(name));
    try { const match = /^socket:\[(\d+)\]$/.exec(await fs.readlink(`/proc/${pid}/fd/${name}`)); if (match) result.add(match[1]); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
  }
  return result;
}

async function pinListeners(candidate) {
  const bindings = [[18196, PROXY_PROCESS.pid], [18198, candidate.process.pid]], selected = [];
  for (const table of ['tcp', 'tcp6']) {
    const raw = await procText(`/proc/self/net/${table}`, JSON_LIMIT);
    for (const line of raw.trim().split('\n').slice(1)) {
      const fields = line.trim().split(/\s+/);
      if (fields[3] === '0A' && bindings.some(([port]) => fields[1]?.endsWith(':' + port.toString(16).toUpperCase())))
        selected.push({ table, address: fields[1], inode: fields[9] });
    }
  }
  for (const [port, pid] of bindings) {
    const suffix = ':' + port.toString(16).toUpperCase(), matches = selected.filter(row => row.address.endsWith(suffix));
    need(matches.length === 1 && matches[0].table === 'tcp' && matches[0].address === '0100007F' + suffix &&
      /^\d+$/.test(matches[0].inode) && (await socketOwners(pid)).has(matches[0].inode));
  }
}

export async function loadLibraryChangedSource55Fixture(options = {}) {
  let phase = 'input';
  try {
    need(exact(options, ['input', 'inputSHA256']) && digest(options.inputSHA256));
    validateLibraryChangedSource55Input(options.input);
    need(process.platform === 'linux' && process.getuid?.() === 0 && process.getgid?.() === 0 &&
      SELF === TOOL + '/client-library-changed-source55-fixture.mjs');
    const directories = new Map(), pins = [], executableIdentities = new Map();
    async function read(value, json = true, maximum = JSON_LIMIT, modes = [0o600n]) {
      const file = await protectedFile(value.path, value.sha256, directories, json, maximum, modes);
      const { value: ignored, ...pin } = file; pins.push(pin); return file.value;
    }
    const input = await read({ path: ROOT + '/input.json', sha256: options.inputSHA256 });
    need(same(input, options.input)); validateLibraryChangedSource55Input(input);
    const candidate = input.candidate, authority = input.authority, scope = upgradeScope(authority);
    const candidateProcess = { ...candidate.process, executable: '/opt/goby-client-m3e/goby', sha256: candidate.binary_sha256,
      uid: 995, cgroup: '/system.slice/goby-client-m3e.service', invocation: candidate.invocation_id };
    phase = 'sources';
    for (const [filename, digestValue] of Object.entries(input.source_closure))
      await read({ path: filename, sha256: digestValue }, false, JSON_LIMIT, [0o600n, 0o644n, 0o700n, 0o755n]);
    phase = 'authority';
    const intent = await read(authority.upgrade_intent);
    validateUpgradeIntent(input, intent);
    const documents = {
      state: await read({ path: STATE, sha256: candidate.state_sha256 }),
      beforeState: await read({ path: scope.output + '/before-state.json', sha256: PREVIOUS.state_sha256 }), intent,
      report: await read(authority.upgrade_report), attestation: await read(authority.upgrade_attestation),
      publication: await read(intent.source.publication), catalog: await read(PINS.catalog, true, SNAPSHOT_LIMIT, [0o600n]),
      current: await read(authority.current_snapshot, true, SNAPSHOT_LIMIT), before: await read(authority.before_snapshot, true, SNAPSHOT_LIMIT),
      profileReceipt: await read(FIXTURES.profile_receipt), profileReport: await read(FIXTURES.profile_report),
      profileInspection: await read(FIXTURES.profile_inspection), musicChain: await read(FIXTURES.music_chain),
      musicScanReceipt: await read(FIXTURES.music_scan_receipt), proxy: await read(PINS.proxy),
    };
    validateLibraryChangedSource55Documents(input, documents);
    const work = directories.get(WORK);
    need(work && String(documents.state.work_identity.device) === work.device && String(documents.state.work_identity.inode) === work.inode);
    phase = 'private_inputs';
    validateLibraryChangedSource55Viewer(await read(input.actor.credentials, true, 16384));
    await read({ path: WORK + '/runtime.env', sha256: candidate.runtime_sha256 }, false, 65536);
    await read({ path: SOURCE + '/backup-source-inputs.json', sha256: SOURCE_SHA }, false, JSON_LIMIT, [0o600n]);
    const evidence = freeze({ schema: 28, source: 'source-attempt-55', process: { ...candidate.process },
      binary_sha256: candidate.binary_sha256, fixture_state_sha256: candidate.state_sha256,
      source_manifest_sha256: SOURCE_SHA, catalog_sha256: CATALOG_SHA, publication: candidate.publication,
      candidate_invocation_id: candidate.invocation_id, input_sha256: options.inputSHA256,
      source_closure_sha256: hash(canonical(input.source_closure)), authority: { ...input.authority },
      historical_anchors: FIXTURES, account_id: USER, credentials_sha256: input.actor.credentials.sha256,
      proxy_process: { ...PROXY_PROCESS }, primary_process: { pid: PRIMARY.pid, start_ticks: PRIMARY.start_ticks, boot_id: BOOT },
      boundary: 'Read-only Goby files, source closure, process lifetimes and two listener owners; live database deltas belong to the controller ledger' });
    let checks = 0;
    async function assertPinned() {
      try {
        for (const pin of pins) await protectedFile(pin.path, pin.sha256, directories, false, pin.maximum, pin.modes, pin.identity);
        need((await procText('/proc/sys/kernel/random/boot_id')).trim() === BOOT);
        const network = await fs.readlink('/proc/self/ns/net');
        need(network === await fs.readlink('/proc/1/ns/net'));
        const processes = [candidateProcess, PRIMARY, PROXY_PROCESS];
        for (const expected of processes) {
          await checkProcess(expected); need(await fs.readlink(`/proc/${expected.pid}/ns/net`) === network);
        }
        for (const expected of [candidateProcess, PRIMARY]) {
          need((await fs.stat(`/proc/${expected.pid}`, { bigint: true })).uid === BigInt(expected.uid) &&
            (await procText(`/proc/${expected.pid}/cgroup`)).trim() === '0::' + expected.cgroup &&
            await procText(`/proc/${expected.pid}/cmdline`) === expected.executable + '\0');
          const environment = await procText(`/proc/${expected.pid}/environ`);
          need(environment.endsWith('\0') && environment.slice(0, -1).split('\0')
            .filter(value => value.startsWith('INVOCATION_ID=')).join('') === 'INVOCATION_ID=' + expected.invocation);
          await hashExecutable(expected, executableIdentities);
        }
        const argv = await procText(`/proc/${PROXY_PROCESS.pid}/cmdline`); need(argv.endsWith('\0'));
        validateSource55ProxyArguments(argv.slice(0, -1).split('\0'));
        await pinListeners(candidate);
        for (const expected of processes) await checkProcess(expected);
        need((await procText('/proc/sys/kernel/random/boot_id')).trim() === BOOT);
        for (const pin of pins) await pinParents(pin.path, directories);
        return { phase: 'pinned', check: ++checks, fixture_state_sha256: candidate.state_sha256 };
      } catch (error) {
        throw Object.assign(new Error('library_changed_source55_live_pin_failed'),
          { diagnostic: libraryChangedSetupDiagnostic(error, 'initial_pin') });
      }
    }
    phase = 'initial_pin'; await assertPinned();
    return Object.freeze({ serverId: SERVER, evidence, assertPinned });
  } catch (error) {
    throw Object.assign(new Error('library_changed_source55_' + phase + '_failed'),
      { phase, diagnostic: libraryChangedSetupDiagnostic(error, phase) });
  }
}
