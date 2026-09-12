#!/usr/bin/env node
/** Exercise source55 fixture guards with explicit public catalog metadata and fenced external effects. */
import fs from 'node:fs/promises';
import { constants as fsConstants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { TextDecoder } from 'node:util';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const WORK = '/opt/goby-test/exec-work-m3e';
const ROOT = WORK + '/client-library-changed-ui-source55-v2';
const TOOL = WORK + '/client-library-changed-source55-tool-02';
const MODULE = TOOL + '/client-library-changed-source55-fixture.mjs';
const SELF = fileURLToPath(import.meta.url);
const LOADER = fileURLToPath(new URL('./client-library-changed-source55-fixture.mjs', import.meta.url));
const CATALOG = WORK + '/source-attempt-55/internal/backuppg/catalogs/schema-28-postgresql-17.json';
const CATALOG_SHA = '8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b';
const CATALOG_LIMIT = 64 * 1024 * 1024;
const hash = value => createHash('sha256').update(value).digest('hex');
const syntheticHash = value => hash('synthetic-source55-fixture:' + value);
const clone = value => JSON.parse(JSON.stringify(value));
const cloneExact = value => Array.isArray(value) ? value.map(cloneExact) : value !== null && typeof value === 'object'
  ? Object.fromEntries(Object.entries(value).map(([key, child]) => [key, cloneExact(child)])) : value;
const ordered = value => Array.isArray(value) ? value.map(ordered) : value !== null && typeof value === 'object'
  ? Object.fromEntries(Object.keys(value).sort().map(key => [key, ordered(value[key])])) : value;
const same = (left, right) => JSON.stringify(ordered(left)) === JSON.stringify(ordered(right));
function check(value) { if (!value) throw new Error('source55_fixture_test_failed'); }

function core(raw, mocks = {}) {
  let source = raw;
  for (const statement of ["import fs from 'node:fs/promises';", "import { constants } from 'node:fs';",
    "import path from 'node:path';", "import { createHash } from 'node:crypto';", "import { TextDecoder } from 'node:util';",
    "import { fileURLToPath } from 'node:url';"]) {
    check(source.split(statement).length === 2);
    source = source.replace(statement, '');
  }
  source = source.replace(/^export (?=(?:(?:async )?function|const|class) )/gm, '')
    .replaceAll('import.meta.url', JSON.stringify('file://' + MODULE));
  check(!/^\s*(?:import|export)\b/m.test(source) && !/\bimport\s*\(/.test(source));
  const declarations = [...new Set([...source.matchAll(/^const ([A-Z][A-Z0-9_]*)\s*=/gm)].map(match => match[1]).concat('SNAPSHOT_LIMIT'))];
  const effects = { filesystem: 0, http: 0, process: 0, other: 0 };
  const deny = kind => { effects[kind] += 1; throw new Error('source55_fixture_external_effect'); };
  const denied = kind => new Proxy(function () {}, {
    get() { return deny(kind); }, apply() { return deny(kind); }, construct() { return deny(kind); },
  });
  const sandbox = {
    fs: mocks.filesystem ?? denied('filesystem'), constants: mocks.filesystem ? fsConstants : denied('filesystem'),
    path: { posix: path.posix }, createHash: mocks.createHash ?? createHash, TextDecoder,
    fileURLToPath, Buffer, URL, process: new Proxy(Object.freeze({ platform: 'linux', getuid: () => 0, getgid: () => 0, argv: [], env: {} }), {
      get(target, key) { return Object.hasOwn(target, key) ? target[key] : deny('process'); },
    }), fetch: denied('http'), console: denied('other'), setTimeout: denied('other'), setInterval: denied('other'),
  };
  const context = vm.createContext(sandbox, { codeGeneration: { strings: false, wasm: false } });
  const publicNames = ['validateLibraryChangedSource55Input', 'validateLibraryChangedSource55Documents',
    'validateLibraryChangedSource55Viewer', 'parseSource55JSON', 'validateSource55ProxyArguments', 'loadLibraryChangedSource55Fixture',
    'readLibraryChangedSource55Snapshot', 'libraryChangedSetupDiagnostic', 'catalogColumns',
    'protectedFile', 'checkProcess', 'pinParents', 'hashExecutable', 'pinListeners'];
  new vm.Script(source + '\nglobalThis.result = {api: {' + publicNames.join(',') + '}, pins: {' + declarations.join(',') + '}};', { filename: MODULE })
    .runInContext(context, { timeout: 1000 });
  check(Object.values(effects).every(value => value === 0));
  return { ...sandbox.result, effects };
}

function noEffects(loaded) { check(Object.values(loaded.effects).every(value => value === 0)); }
function rejects(action, loaded) {
  let rejected = false;
  try { action(); } catch (error) { rejected = error?.message !== 'source55_fixture_external_effect'; }
  check(rejected); noEffects(loaded);
}
async function rejectsAsync(action, loaded) {
  let rejected = false;
  try { await action(); } catch (error) { rejected = error?.message !== 'source55_fixture_external_effect'; }
  check(rejected); noEffects(loaded);
}

function viewerExample() {
  return { marker: 'goby-client-library-changed-viewer-v1', slot: 'B', user_id: 'ecbbe4cb82403879bc4b4f78894c5738',
    base_url: 'http://127.0.0.1:18196', direct_url: 'http://127.0.0.1:18198',
    viewer: { username: 'm3e-client-viewer', password: '0123456789abcdef'.repeat(3) } };
}

function inputExample(pins) {
  const output = WORK + '/client-schema28-source55-upgrade-20260912_120000_abc123def456';
  return { marker: 'goby-client-library-changed-input-v1', version: 1, mode: 'b-movies-name-automatic-refresh',
    root: ROOT, output: ROOT + '/browser',
    actor: { slot: 'B', user_id: 'ecbbe4cb82403879bc4b4f78894c5738', account_key: 'viewer',
      source_credentials_sha256: '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790',
      credentials: { path: ROOT + '/viewer-credentials.json', sha256: hash(JSON.stringify(viewerExample())) } },
    candidate: { binary_sha256: syntheticHash('candidate-binary'), process: { pid: 1600001, start_ticks: 20000001, boot_id: pins.BOOT },
      runtime_sha256: syntheticHash('candidate-runtime'), state_sha256: syntheticHash('candidate-state'),
      server_id: 'c7cfd76b1dee728b2bad523793a37ccb', base_url: 'http://127.0.0.1:18196', direct_url: 'http://127.0.0.1:18198',
      source: pins.SOURCE, source_manifest_sha256: pins.SOURCE_SHA,
      invocation_id: syntheticHash('candidate-invocation').slice(0, 32), publication: syntheticHash('publication').slice(0, 40) },
    fixture: clone(pins.FIXTURES), expected_libraries: [
      { id: 'a9993591e72f0f2e7babcbf8b9c50790', name: 'M3e Client Movies' },
      { id: '6383d20008836e137559698c29b10395', name: 'M3e Client Music' },
      { id: 'a34ce665fb75421ef7551570f353d705', name: 'M3e Client TV' },
      { id: '57a85c1ca5b6c7ae602c587755250b2f', name: 'M3e Client Special Features Movies' },
    ], target: { id: '268051d3ca734aefcf94e245fb25ad55', library_id: 'a9993591e72f0f2e7babcbf8b9c50790',
      name: 'Synthetic Source55 Movie', type: 'Movie', parent_id: 'a'.repeat(32), root_id: 'b'.repeat(32),
      relative_path: 'Synthetic Source55 Movie/Synthetic Source55 Movie.mp4' },
    source_closure: Object.fromEntries(pins.SOURCES.map(name => [TOOL + '/' + name, syntheticHash(name)])),
    authority: { upgrade_intent: { path: pins.UPGRADE_TOOL + '/intent.json', sha256: syntheticHash('intent') },
      upgrade_report: { path: output + '/report.json', sha256: syntheticHash('report') },
      upgrade_attestation: { path: output + '/attestation.json', sha256: syntheticHash('attestation') },
      current_snapshot: { path: output + '/after-full.json', sha256: syntheticHash('current') },
      before_snapshot: { path: ROOT + '/before-full.json', sha256: syntheticHash('before') } },
    controller: { pid: 900001, start_ticks: '10001', boot_id: pins.BOOT,
      unit: 'goby-client-library-changed-ui-source55-controller-v2.service' } };
}

function inputCases(test, loaded) {
  const valid = input => loaded.api.validateLibraryChangedSource55Input(input);
  test('complete_source55_input_requires_fresh_authority_and_exact_seven_module_paths', () => {
    const input = inputExample(loaded.pins); valid(input); check(Object.keys(input.source_closure).length === 7); noEffects(loaded);
  });
  for (const [name, mutate] of [
    ['old_candidate_binary', v => { v.candidate.binary_sha256 = loaded.pins.PREVIOUS.binary_sha256; }],
    ['primary_binary', v => { v.candidate.binary_sha256 = loaded.pins.PRIMARY.sha256; }],
    ['old_candidate_process', v => { v.candidate.process = clone(loaded.pins.PREVIOUS.process); }],
    ['string_candidate_pid', v => { v.candidate.process.pid = String(v.candidate.process.pid); }],
    ['bigint_candidate_pid', v => { v.candidate.process.pid = BigInt(v.candidate.process.pid); }],
    ['candidate_pid_one', v => { v.candidate.process.pid = 1; }],
    ['stale_candidate_ticks', v => { v.candidate.process.start_ticks = loaded.pins.PREVIOUS.process.start_ticks; }],
    ['string_candidate_ticks', v => { v.candidate.process.start_ticks = '20000001'; }],
    ['changed_boot', v => { v.candidate.process.boot_id = '00000000-0000-0000-0000-000000000001'; }],
    ['old_source', v => { v.candidate.source = WORK + '/source-attempt-44'; }],
    ['changed_manifest', v => { v.candidate.source_manifest_sha256 = syntheticHash('foreign'); }],
    ['old_runtime', v => { v.candidate.runtime_sha256 = loaded.pins.PREVIOUS.runtime_sha256; }],
    ['old_state', v => { v.candidate.state_sha256 = loaded.pins.PREVIOUS.state_sha256; }],
    ['placeholder_binary', v => { v.candidate.binary_sha256 = '0'.repeat(64); }],
    ['placeholder_invocation', v => { v.candidate.invocation_id = 'a'.repeat(32); }],
    ['old_invocation', v => { v.candidate.invocation_id = loaded.pins.PREVIOUS_PROCESS.invocation; }],
    ['placeholder_publication', v => { v.candidate.publication = '0'.repeat(40); }],
    ['old_publication', v => { v.candidate.publication = '35ae3d000f812fa18d234921cedb3c33d88190e0'; }],
    ['foreign_server', v => { v.candidate.server_id = 'f'.repeat(32); }],
    ['reference_base', v => { v.candidate.base_url = 'http://127.0.0.1:18197'; }],
    ['nonlocal_direct', v => { v.candidate.direct_url = 'http://localhost:18198'; }],
    ['extra_candidate_field', v => { v.candidate.reference_url = 'http://127.0.0.1:18197'; }],
    ['actor_slot_a', v => { v.actor.slot = 'A'; }],
    ['foreign_actor', v => { v.actor.user_id = 'f'.repeat(32); }],
    ['admin_actor', v => { v.actor.account_key = 'admin'; }],
    ['inline_password', v => { v.actor.password = 'synthetic-private'; }],
    ['legacy_credentials', v => { v.actor.credentials.path = WORK + '/browser.json'; }],
    ['traversing_credentials', v => { v.actor.credentials.path = ROOT + '/child/../viewer-credentials.json'; }],
    ['backslash_credentials', v => { v.actor.credentials.path = ROOT + '\\viewer-credentials.json'; }],
    ['foreign_credential_origin', v => { v.actor.source_credentials_sha256 = syntheticHash('foreign'); }],
    ['uppercase_hash', v => { v.actor.credentials.sha256 = v.actor.credentials.sha256.toUpperCase(); }],
    ['extra_input', v => { v.reference = {}; }],
    ['wrong_marker', v => { v.marker = 'goby-client-library-home-input-v1'; }],
    ['string_version', v => { v.version = '1'; }],
    ['old_root', v => { v.root = WORK + '/client-library-changed-ui-source44-v1'; }],
    ['outside_output', v => { v.output = WORK + '/browser'; }],
    ['wrong_target_library', v => { v.target.library_id = v.expected_libraries[1].id; }],
    ['wrong_target_item', v => { v.target.id = 'f'.repeat(32); }],
    ['bigint_target_parent', v => { v.target.parent_id = 11111111111111111111111111111111n; }],
    ['bigint_target_root', v => { v.target.root_id = 11111111111111111111111111111111n; }],
    ['target_escape', v => { v.target.relative_path = '../foreign.mp4'; }],
    ['absolute_target', v => { v.target.relative_path = '/foreign.mp4'; }],
    ['target_control_character', v => { v.target.name = 'synthetic\nname'; }],
    ['missing_library', v => { v.expected_libraries.pop(); }],
    ['duplicate_library', v => { v.expected_libraries[1] = clone(v.expected_libraries[0]); }],
    ['old_controller_unit', v => { v.controller.unit = 'goby-client-library-changed-ui-source44-controller-v1.service'; }],
    ['numeric_controller_ticks', v => { v.controller.start_ticks = 10001; }],
    ['candidate_controller_pid', v => { v.controller.pid = v.candidate.process.pid; }],
    ['old_current_snapshot_hash', v => { v.authority.current_snapshot.sha256 = '738eb9717506bec997456830e70d56f0867f9cf8159939ea3733e039cadb9811'; }],
    ['missing_source_dependency', v => { delete v.source_closure[TOOL + '/client-browser-goby-fixture.mjs']; }],
    ['missing_transitive_profile', v => { delete v.source_closure[TOOL + '/client-browser-special-features-fixture.mjs']; }],
    ['extra_dependency', v => { v.source_closure[TOOL + '/extra.mjs'] = syntheticHash('extra'); }],
    ['old_driver_dependency', v => {
      delete v.source_closure[TOOL + '/client-browser-library-changed-source55.mjs'];
      v.source_closure[TOOL + '/client-browser-library-changed.mjs'] = syntheticHash('old-driver');
    }],
  ]) test('input_rejects_' + name, async () => {
    const input = inputExample(loaded.pins); mutate(input); rejects(() => valid(input), loaded);
    await rejectsAsync(() => loaded.api.loadLibraryChangedSource55Fixture({ input, inputSHA256: syntheticHash('input') }), loaded);
  });
  for (const key of Object.keys(inputExample(loaded.pins).candidate)) test('input_requires_candidate_' + key, () => {
    const input = inputExample(loaded.pins); delete input.candidate[key]; rejects(() => valid(input), loaded);
  });
  for (const key of Object.keys(inputExample(loaded.pins).authority)) {
    test('input_requires_authority_' + key, () => {
      const input = inputExample(loaded.pins); delete input.authority[key]; rejects(() => valid(input), loaded);
    });
    test('input_rejects_foreign_authority_path_' + key, () => {
      const input = inputExample(loaded.pins); input.authority[key].path = WORK + '/foreign/' + path.posix.basename(input.authority[key].path);
      rejects(() => valid(input), loaded);
    });
  }
  for (const key of Object.keys(loaded.pins.FIXTURES)) test('input_retains_historical_' + key, () => {
    const input = inputExample(loaded.pins); input.fixture[key].sha256 = syntheticHash('foreign'); rejects(() => valid(input), loaded);
  });
  test('invalid_loader_options_reject_before_external_effects', async () => {
    for (const options of [undefined, null, {}, { input: inputExample(loaded.pins) },
      { input: inputExample(loaded.pins), inputSHA256: 'invalid' }, { input: inputExample(loaded.pins), inputSHA256: 'A'.repeat(64) },
      { input: inputExample(loaded.pins), inputSHA256: syntheticHash('input'), reference: {} }])
      await rejectsAsync(() => loaded.api.loadLibraryChangedSource55Fixture(options), loaded);
  });
}
function viewerCases(test, loaded) {
  test('isolated_b_viewer_has_no_legacy_credential_carrier', () => {
    const viewer = viewerExample(), before = clone(viewer);
    loaded.api.validateLibraryChangedSource55Viewer(viewer);
    check(same(viewer, before)); noEffects(loaded);
  });
  for (const [name, mutate] of [
    ['slot_a', value => { value.slot = 'A'; }],
    ['foreign_user', value => { value.user_id = 'f'.repeat(32); }],
    ['wrong_marker', value => { value.marker = 'goby-client-browser-v1'; }],
    ['reference_authority', value => { value.base_url = 'http://127.0.0.1:18197'; }],
    ['direct_authority_conflict', value => { value.direct_url = value.base_url; }],
    ['admin_identity', value => { value.viewer.username = 'm3e-client-admin'; }],
    ['wrong_password_length', value => { value.viewer.password = 'a'.repeat(47); }],
    ['uppercase_password', value => { value.viewer.password = 'A'.repeat(48); }],
    ['password_control_character', value => { value.viewer.password = 'a'.repeat(47) + '\n'; }],
    ['empty_password', value => { value.viewer.password = ''; }],
    ['admin_credential_carrier', value => { value.admin = clone(value.viewer); }],
    ['recovery_credential_carrier', value => { value.recovery = { secret: 'synthetic-forbidden-recovery' }; }],
    ['competing_actor_carrier', value => { value.actors = { A: clone(value.viewer) }; }],
    ['token_credential_carrier', value => { value.viewer.token = 'synthetic-forbidden-token'; }],
    ['root_password_carrier', value => { value.password = value.viewer.password; }],
    ['nested_user_conflict', value => { value.viewer.user_id = 'f'.repeat(32); }],
  ]) {
    test('viewer_rejects_' + name, () => {
      const viewer = viewerExample(); mutate(viewer);
      rejects(() => loaded.api.validateLibraryChangedSource55Viewer(viewer), loaded);
    });
  }
}

function jsonCases(test, loaded) {
  const parse = loaded.api.parseSource55JSON;
  test('json_preserves_adjacent_unsafe_integer_ledger_identities', () => {
    const value = parse('{"safe":9007199254740991,"first":9007199254740992,"next":9007199254740993,"negative":-9007199254740993}');
    check(value.safe === 9007199254740991 && value.first === 9007199254740992n && value.next === 9007199254740993n &&
      value.negative === -9007199254740993n && value.first !== value.next); noEffects(loaded);
  });
  test('json_allows_bounded_objects_and_explicit_arrays', () => {
    const value = parse(' {"name":"synthetic","escaped":"\\u0041","items":[true,false,null,1.25,-2e2]} \n');
    check(value.name === 'synthetic' && value.escaped === 'A' && same(value.items, [true, false, null, 1.25, -200]));
    check(same(parse('[{"id":1}]', true), [{ id: 1 }])); noEffects(loaded);
  });
  for (const [name, text] of [
    ['duplicate_root_key', '{"id":1,"id":2}'],
    ['duplicate_nested_key', '{"state":{"id":1,"id":2}}'],
    ['duplicate_key_in_array', '{"states":[{"id":1,"id":2}]}'],
    ['escaped_duplicate_key', String.raw`{"id":1,"\u0069d":2}`],
    ['escaped_nested_duplicate_key', String.raw`{"state":{"name":"first","\u006eame":"second"}}`],
    ['escaped_backslash_duplicate_key', String.raw`{"a\\b":1,"a\u005cb":2}`],
    ['scalar_number_root', '1'],
    ['scalar_string_root', '"value"'],
    ['null_root', 'null'],
    ['implicit_array_root', '[]'],
    ['trailing_document', '{"id":1}{"id":2}'],
    ['trailing_token', '{"id":1} false'],
    ['trailing_object_comma', '{"id":1,}'],
    ['trailing_array_comma', '{"ids":[1,]}'],
    ['invalid_number_leading_zero', '{"id":01}'],
    ['invalid_number_infinity', '{"id":1e309}'],
    ['invalid_escape', String.raw`{"name":"\x41"}`],
    ['unescaped_control_character', '{"name":"line\nfeed"}'],
    ['unterminated_string', '{"name":"unterminated}'],
    ['empty_input', ''],
  ]) {
    test('json_rejects_' + name, () => rejects(() => parse(text), loaded));
  }
  test('json_rejects_non_text_input', () => {
    for (const value of [undefined, null, true, {}, Buffer.from('{"id":1}')]) rejects(() => parse(value), loaded);
  });
  test('json_syntax_errors_do_not_release_private_input_fragments', () => {
    let message;
    try { parse(String.raw`{"credential":"synthetic-private-fragment\q"}`); } catch (error) { message = error?.message; }
    check(message === 'library_changed_source55_binding_rejected' && !message.includes('synthetic-private-fragment')); noEffects(loaded);
  });
  test('json_bounds_input_bytes', () => {
    const limit = loaded.pins.SNAPSHOT_LIMIT; check(limit === 64 * 1024 * 1024);
    rejects(() => parse('{"value":"' + 'x'.repeat(limit) + '"}'), loaded);
  });
  test('json_bounds_nesting_and_node_count', () => {
    rejects(() => parse('{"items":' + '['.repeat(128) + '0' + ']'.repeat(128) + '}'), loaded);
    rejects(() => parse('{"items":[' + Array(500001).fill('0').join(',') + ']}'), loaded);
  });
}

function documentsExample(pins, input = inputExample(pins)) {
  const output = path.posix.dirname(input.authority.upgrade_report.path), run = path.posix.basename(output).replace('client-schema28-source55-upgrade-', '');
  const unit = 'goby-client-schema28-source55-' + run.replaceAll('_', '-') + '.service';
  const intent = { marker: pins.UPGRADE_MARKER, version: 1, run_id: run, tool: pins.UPGRADE_TOOL, output, controller: { unit },
    candidate: { state: { path: pins.STATE, sha256: pins.PREVIOUS.state_sha256 },
      baseline: { path: WORK + '/collection-folder-contract-v1/after-full.json',
        sha256: '2659f45dfa82d8568b07375d04c08cd4ba216defcfa2a4a1c269867b176573be' },
      process: clone(pins.PREVIOUS.process), invocation_id: pins.PREVIOUS_PROCESS.invocation },
    source: { root: pins.SOURCE, manifest_sha256: pins.SOURCE_SHA,
      full_report: { path: WORK + '/client-backup-run-20260912_084241_db776aacc1a7/report.json', sha256: syntheticHash('full-report') },
      terminal: { path: WORK + '/collection-folder-source55-full-execution-01/terminal.json', sha256: syntheticHash('full-terminal') },
      binary: { path: WORK + '/client-backup-run-20260912_084241_db776aacc1a7/tmp/goby-linux-amd64',
        sha256: input.candidate.binary_sha256, bytes: 2000000 },
      publication: { path: pins.UPGRADE_TOOL + '/publication.json', sha256: syntheticHash('publication-document') } } };
  const beforeState = { marker: 'goby-m3e-client-acceptance-v1', work: WORK, schema: 27, phase: 'ready', stage: 'complete',
    server_id: input.candidate.server_id, viewer_id: input.actor.user_id, binary_sha256: pins.PREVIOUS.binary_sha256,
    runtime_sha256: pins.PREVIOUS.runtime_sha256, browser_sha256: input.actor.source_credentials_sha256,
    process: clone(pins.PREVIOUS.process), work_identity: { device: '1', inode: '100' }, binary_identity: { inode: '500' },
    schema27_source: { source: pins.PREVIOUS.source, source_manifest_sha256: pins.PREVIOUS.source_manifest_sha256, schema: 27 },
    notifications_upgrade: { marker: 'goby-client-notifications-source44-continuation-v1', phase: 'complete', publication: 'historical' },
    special_features_profile: { receipt_path: pins.FIXTURES.profile_receipt.path, receipt_sha256: pins.FIXTURES.profile_receipt.sha256 },
    music: { scan_schema: 25, historical_record: 'synthetic-preserved-music' },
    upgrade: { marker: 'synthetic-historical-upgrade', phase: 'complete' },
    upgrade_history: [{ marker: 'synthetic-historical-upgrade', phase: 'complete' }] };
  const service = { MainPID: String(input.candidate.process.pid), InvocationID: input.candidate.invocation_id,
    ActiveState: 'active', SubState: 'running' };
  const upgrade = { marker: pins.UPGRADE_MARKER, phase: 'complete', from_schema: 27, to_schema: 28, output,
    intent_sha256: input.authority.upgrade_intent.sha256, old_process: clone(pins.PREVIOUS.process), new_process: clone(input.candidate.process),
    from_sha256: pins.PREVIOUS.binary_sha256, to_sha256: input.candidate.binary_sha256, publication: input.candidate.publication,
    backup_sha256: syntheticHash('backup'), rehearsal: { path: output + '/rehearsal-completed.json', sha256: syntheticHash('rehearsal') },
    service: clone(service) };
  const state = { ...clone(beforeState), schema: 28, process: clone(input.candidate.process), binary_sha256: input.candidate.binary_sha256,
    binary_identity: { inode: '501' }, runtime_sha256: input.candidate.runtime_sha256, upgrade: clone(upgrade),
    upgrade_history: [...clone(beforeState.upgrade_history), clone(upgrade)], schema28_upgrade: clone(upgrade),
    schema28_source: { marker: 'goby-client-schema28-source-v1', schema: 28, source: pins.SOURCE, source_manifest_sha256: pins.SOURCE_SHA,
      catalog_sha256: pins.CATALOG_SHA, publication: input.candidate.publication } };
  const controller = { unit, invocation_id: syntheticHash('upgrade-controller').slice(0, 32),
    process: { pid: 1600002, start_ticks: 20000000, boot_id: pins.BOOT, uid: 0, exe: '/usr/bin/python3.11',
      cgroup: '/system.slice/' + unit, namespace: 'mnt:[4026531841]' } };
  const report = { marker: pins.UPGRADE_MARKER, version: 1, run_id: run, intent_sha256: input.authority.upgrade_intent.sha256,
    status: 'awaiting_outer_attestation', controller, candidate_service: clone(service), new_process: clone(input.candidate.process),
    state_sha256: input.candidate.state_sha256, schema: 28, binary: clone(intent.source.binary), publication: input.candidate.publication,
    preserved_rows_sequences_credentials_recovery: true, primary_unchanged: true, candidate_web: '/opt/goby-client-m3e/admin',
    shared_web_unchanged: true, rehearsal_removed: true, hba_restored_exactly: true, service_actions: ['stop', 'start'], http_requests: 5,
    client_acceptance: false, automatic_retry: false, automatic_rollback: false,
    evidence: { 'before-state.json': { path: output + '/before-state.json', sha256: pins.PREVIOUS.state_sha256 },
      'after-full.json': clone(input.authority.current_snapshot), 'rehearsal-completed.json': clone(upgrade.rehearsal) } };
  const attestation = { marker: pins.UPGRADE_MARKER, version: 1, run_id: run, intent_sha256: input.authority.upgrade_intent.sha256,
    status: 'passed', report_sha256: input.authority.upgrade_report.sha256,
    controller: { MainPID: '0', InvocationID: controller.invocation_id, ActiveState: 'active', SubState: 'exited', Result: 'success',
      ExecMainStatus: '0', ControlGroup: '', Description: pins.UPGRADE_MARKER + ':' + run, RemainAfterExit: 'yes' },
    recursive_cgroup_empty: true, state_sha256: input.candidate.state_sha256, binary: clone(intent.source.binary), schema: 28,
    rows_sequences_credentials_recovery_preserved: true, primary_unchanged: true, rehearsal_removed: true,
    hba_restored_exactly: true, client_acceptance: false };
  const publication = { marker: 'goby-source55-publication-v1', commit: input.candidate.publication, source_manifest_sha256: pins.SOURCE_SHA,
    binary_sha256: input.candidate.binary_sha256, full_report_sha256: intent.source.full_report.sha256,
    full_terminal_sha256: intent.source.terminal.sha256 };
  const profileReceipt = { marker: 'goby-client-special-features-fixture-v1', phase: 'complete', schema: 27, profile_version: 3,
    candidate: { binary_sha256: pins.PRIMARY.sha256 },
    upgrade: { completed_sha256: syntheticHash('historical-completed'), report_sha256: syntheticHash('historical-report') } };
  const profileReport = { result: 'passed', phase: 'complete', receipt_sha256: pins.FIXTURES.profile_receipt.sha256 };
  const profileInspection = { result: 'passed', phase: 'complete', schema: 27, receipt_sha256: pins.FIXTURES.profile_receipt.sha256 };
  const musicChain = Array.from({ length: 5 }, (_, index) => ({ completedSHA256: syntheticHash('music-completed-' + index),
    reportSHA256: syntheticHash('music-report-' + index) }));
  musicChain[4] = { completedSHA256: profileReceipt.upgrade.completed_sha256, reportSHA256: profileReceipt.upgrade.report_sha256 };
  const musicScanReceipt = { schema: 25, phase: 'complete', result: 'passed', logout_status: 204, token_readback_status: 401 };
  const tables = Object.fromEntries(['sessions', 'devices', 'activity_entries', 'play_sessions', 'user_item_data', 'libraries', 'items',
    'users', 'library_roots', 'schema_migrations'].map(name => [name, []]));
  for (let index = 0; index < 25; index += 1) tables['synthetic_table_' + index] = [];
  for (const [name, count] of [['sessions', 75], ['devices', 64], ['activity_entries', 167], ['play_sessions', 26], ['user_item_data', 7]])
    tables[name] = Array.from({ length: count }, (_, index) => ({ id: name + '-' + index }));
  tables.activity_entries = tables.activity_entries.map(row => ({ ...row, previous_revision: 0, observation_fingerprint: '' }));
  tables.libraries = clone(input.expected_libraries);
  tables.users = [{ id: input.actor.user_id, is_administrator: false, is_disabled: false, management_revision: 5 }];
  tables.items = Array.from({ length: 22 }, (_, index) => ({ ...clone(input.target),
    id: index === 0 ? input.target.id : syntheticHash('item-' + index).slice(0, 32), is_folder: false }));
  tables.library_roots = Array.from({ length: 4 }, (_, index) => ({ id: syntheticHash('root-' + index).slice(0, 32),
    path: '/synthetic/root-' + index, allowed_path: '/synthetic/root-' + index, relative_path: '.',
    binding_revision: 1, storage_binding: null, bound_at: null, bound_by: null }));
  tables.schema_migrations = Array.from({ length: 28 }, (_, index) => ({ version: index + 1, name: 'synthetic_migration_' + (index + 1) }));
  tables.synthetic_table_0 = [{ id: 1, generated_value: 42 }];
  const projections = Object.fromEntries(Object.entries(tables).map(([name, rows]) => [name, rows.length ? Object.keys(rows[0]) : ['id']]));
  const sequenceNames = ['first', 'second', 'third', 'fourth', 'fifth'];
  const sequenceDefinitions = sequenceNames.map((Name, index) => ({ Name, Table: 'synthetic_table_' + index, Column: 'id',
    MinValue: 1, MaxValue: 1000000, Increment: 1, Consumers: [{ Table: 'synthetic_table_' + index, Column: 'id' }] }));
  // Synthetic catalog facts model complete table, generated-column and sequence contracts without loading product data.
  const catalog = { version: 28, postgresql_major: 17, migrations: clone(tables.schema_migrations),
    catalog: { Tables: Object.entries(projections).map(([Name, columns]) => ({ Name, Columns: columns.filter(value => value !== 'generated_value') })),
      Sequences: sequenceDefinitions },
    objects: [...Object.entries(projections).flatMap(([name, columns]) => [
      { kind: 'relation', name, value: { kind: 'r' } },
      ...columns.map((column, index) => ({ kind: 'column', name: name + '.' + String(index + 1).padStart(5, '0'),
        value: { name: column, dropped: false, generated: column === 'generated_value' ? 's' : '' } }))]),
    ...sequenceDefinitions.flatMap(sequence => [{ kind: 'relation', name: sequence.Name, value: { kind: 'S' } },
      { kind: 'sequence', name: sequence.Name, value: { min: sequence.MinValue, max: sequence.MaxValue, increment: sequence.Increment,
        owned_by: sequence.Table + '.' + sequence.Column } }])] };
  const current = { schema: 28, runtime_sha256: input.candidate.runtime_sha256, browser_sha256: input.actor.source_credentials_sha256,
    recovery: { record: 'synthetic-private-recovery' }, added_viewer_credentials: { record: 'synthetic-private-viewer' },
    database: { metadata: { captured_at: '2026-09-12T12:00:00.000000Z', database_oid: 100, role_oid: 101,
      columns: clone(projections) },
      catalog: clone(catalog.objects), unsupported: false,
      sequences: Object.fromEntries(sequenceNames.map((name, index) => [name, { last_value: 100 + index, is_called: true }])), tables } };
  const before = clone(current); before.database.metadata.captured_at = '2026-09-12T12:05:00.000000Z';
  return { state, beforeState, intent, report, attestation, publication, catalog, current, before,
    profileReceipt, profileReport, profileInspection, musicChain, musicScanReceipt,
    proxy: { pid: pins.PROXY_PROCESS.pid, start_ticks: String(pins.PROXY_PROCESS.start_ticks), reference_only: false,
      listen: ['127.0.0.1:18196', '127.0.0.1:18197'] } };
}

function documentCases(test, loaded) {
  const valid = (input, documents) => loaded.api.validateLibraryChangedSource55Documents(input, documents);
  const both = (documents, mutate) => { mutate(documents.current); mutate(documents.before); };
  const upgraded = (documents, mutate) => {
    mutate(documents.state.upgrade); documents.state.schema28_upgrade = clone(documents.state.upgrade);
    documents.state.upgrade_history[documents.state.upgrade_history.length - 1] = clone(documents.state.upgrade);
  };
  test('attested_schema28_chain_preserves_historical_schema27_profile_and_schema25_scan', () => {
    const input = inputExample(loaded.pins), documents = documentsExample(loaded.pins, input);
    check(valid(input, documents) === true && documents.report.status === 'awaiting_outer_attestation' &&
      documents.profileReceipt.schema === 27 && documents.musicScanReceipt.schema === 25); noEffects(loaded);
  });
  test('attestation_accepts_owned_empty_cgroup_path', () => {
    const input = inputExample(loaded.pins), documents = documentsExample(loaded.pins, input);
    documents.attestation.controller.ControlGroup = '/system.slice/' + documents.intent.controller.unit;
    check(valid(input, documents) === true); noEffects(loaded);
  });
  for (const [name, mutate] of [
    ['state_schema27', d => { d.state.schema = 27; }],
    ['string_schema28', d => { d.state.schema = '28'; }],
    ['state_not_ready', d => { d.state.phase = 'upgrading'; }],
    ['state_not_complete', d => { d.state.stage = 'started'; }],
    ['state_candidate_hash', d => { d.state.binary_sha256 = syntheticHash('foreign'); }],
    ['state_runtime_hash', d => { d.state.runtime_sha256 = syntheticHash('foreign'); }],
    ['state_process', d => { d.state.process.pid += 1; }],
    ['state_user', d => { d.state.viewer_id = 'f'.repeat(32); }],
    ['state_credentials', d => { d.state.browser_sha256 = syntheticHash('foreign'); }],
    ['state_work_identity', d => { delete d.state.work_identity; }],
    ['historical_schema27_relabel', d => { d.state.schema27_source.schema = 28; }],
    ['historical_notification_relabel', d => { d.state.notifications_upgrade.marker = 'source55'; }],
    ['historical_music_relabel', d => { d.state.music.scan_schema = 28; }],
    ['state_unrelated_new_field', d => { d.state.synthetic_extra = true; }],
    ['source28_extra_migration_field', d => { d.state.schema28_source.migration_27_sha256 = syntheticHash('foreign'); }],
    ['source28_wrong_catalog', d => { d.state.schema28_source.catalog_sha256 = syntheticHash('foreign'); }],
    ['source28_wrong_manifest', d => { d.state.schema28_source.source_manifest_sha256 = syntheticHash('foreign'); }],
    ['source28_wrong_publication', d => { d.state.schema28_source.publication = syntheticHash('foreign').slice(0, 40); }],
    ['legacy_upgrade_shape', d => upgraded(d, u => { u.schema_artifacts = {}; })],
    ['upgrade_wrong_from_schema', d => upgraded(d, u => { u.from_schema = 26; })],
    ['upgrade_wrong_to_schema', d => upgraded(d, u => { u.to_schema = 27; })],
    ['upgrade_wrong_intent', d => upgraded(d, u => { u.intent_sha256 = syntheticHash('foreign'); })],
    ['upgrade_wrong_old_process', d => upgraded(d, u => { u.old_process.pid += 1; })],
    ['upgrade_wrong_binary', d => upgraded(d, u => { u.to_sha256 = syntheticHash('foreign'); })],
    ['upgrade_wrong_rehearsal', d => upgraded(d, u => { u.rehearsal.sha256 = syntheticHash('foreign'); })],
    ['upgrade_history_replaced', d => { d.state.upgrade_history.shift(); }],
    ['upgrade_records_disagree', d => { d.state.schema28_upgrade.phase = 'started'; }],
    ['intent_wrong_output', d => { d.intent.output = WORK + '/foreign'; }],
    ['intent_wrong_old_state', d => { d.intent.candidate.state.sha256 = syntheticHash('foreign'); }],
    ['intent_wrong_baseline', d => { d.intent.candidate.baseline.sha256 = syntheticHash('foreign'); }],
    ['intent_wrong_source', d => { d.intent.source.root = WORK + '/source-attempt-44'; }],
    ['intent_wrong_binary', d => { d.intent.source.binary.sha256 = syntheticHash('foreign'); }],
    ['report_claims_passed', d => { d.report.status = 'passed'; }],
    ['report_old_readiness_count', d => { d.report.http_requests = 2; }],
    ['report_extra_service_action', d => { d.report.service_actions.push('restart'); }],
    ['report_state_conflict', d => { d.report.state_sha256 = syntheticHash('foreign'); }],
    ['report_snapshot_conflict', d => { d.report.evidence['after-full.json'].sha256 = syntheticHash('foreign'); }],
    ['report_before_state_conflict', d => { d.report.evidence['before-state.json'].sha256 = syntheticHash('foreign'); }],
    ['report_evidence_escape', d => { d.report.evidence.foreign = { path: WORK + '/foreign.json', sha256: syntheticHash('foreign') }; }],
    ['report_candidate_invocation', d => { d.report.candidate_service.InvocationID = syntheticHash('foreign').slice(0, 32); }],
    ['report_controller_not_root', d => { d.report.controller.process.uid = 995; }],
    ['report_controller_foreign_cgroup', d => { d.report.controller.process.cgroup = '/system.slice/foreign.service'; }],
    ['report_controller_candidate_pid', d => { d.report.controller.process.pid = d.state.process.pid; }],
    ['attestation_missing', d => { delete d.attestation; }],
    ['attestation_waiting', d => { d.attestation.status = 'awaiting_outer_attestation'; }],
    ['attestation_report_conflict', d => { d.attestation.report_sha256 = syntheticHash('foreign'); }],
    ['attestation_intent_conflict', d => { d.attestation.intent_sha256 = syntheticHash('foreign'); }],
    ['attestation_state_conflict', d => { d.attestation.state_sha256 = syntheticHash('foreign'); }],
    ['attestation_running_controller', d => { d.attestation.controller.MainPID = '1600002'; }],
    ['attestation_nonzero_exit', d => { d.attestation.controller.ExecMainStatus = '1'; }],
    ['attestation_failed_result', d => { d.attestation.controller.Result = 'exit-code'; }],
    ['attestation_wrong_invocation', d => { d.attestation.controller.InvocationID = syntheticHash('foreign').slice(0, 32); }],
    ['attestation_wrong_description', d => { d.attestation.controller.Description = 'foreign'; }],
    ['attestation_no_remain_after_exit', d => { d.attestation.controller.RemainAfterExit = 'no'; }],
    ['attestation_foreign_cgroup', d => { d.attestation.controller.ControlGroup = '/system.slice/foreign.service'; }],
    ['attestation_nonempty_cgroup', d => { d.attestation.recursive_cgroup_empty = false; }],
    ['publication_commit_conflict', d => { d.publication.commit = syntheticHash('foreign').slice(0, 40); }],
    ['publication_manifest_conflict', d => { d.publication.source_manifest_sha256 = syntheticHash('foreign'); }],
    ['publication_full_terminal_conflict', d => { d.publication.full_terminal_sha256 = syntheticHash('foreign'); }],
    ['profile_relabelled_schema28', d => { d.profileReceipt.schema = 28; }],
    ['profile_version_changed', d => { d.profileReceipt.profile_version = 2; }],
    ['profile_inspection_failed', d => { d.profileInspection.result = 'blocked'; }],
    ['music_scan_relabelled_schema28', d => { d.musicScanReceipt.schema = 28; }],
    ['music_history_missing', d => { d.musicChain.pop(); }],
    ['music_scan_token_live', d => { d.musicScanReceipt.token_readback_status = 200; }],
    ['proxy_wrong_lifetime', d => { d.proxy.start_ticks = '1'; }],
    ['proxy_reference_only', d => { d.proxy.reference_only = true; }],
    ['proxy_missing_candidate_listener', d => { d.proxy.listen = ['127.0.0.1:18197']; }],
    ['snapshot_schema27', d => both(d, s => { s.schema = 27; })],
    ['snapshot_runtime_conflict', d => both(d, s => { s.runtime_sha256 = syntheticHash('foreign'); })],
    ['snapshot_credentials_conflict', d => both(d, s => { s.browser_sha256 = syntheticHash('foreign'); })],
    ['snapshot_time_equal', d => { d.before.database.metadata.captured_at = d.current.database.metadata.captured_at; }],
    ['snapshot_time_reversed', d => { d.before.database.metadata.captured_at = '2026-09-12T11:59:00Z'; }],
    ['snapshot_bad_time', d => { d.before.database.metadata.captured_at = 'invalid'; }],
    ['snapshot_private_recovery_changed', d => { d.before.recovery.record = 'foreign'; }],
    ['snapshot_private_viewer_changed', d => { d.before.added_viewer_credentials.record = 'foreign'; }],
    ['snapshot_catalog_changed', d => { d.before.database.catalog.push({ kind: 'foreign' }); }],
    ['snapshot_unpublished_catalog', d => both(d, s => { s.database.catalog.push({ kind: 'foreign' }); })],
    ['snapshot_sequences_changed', d => { d.before.database.sequences.first.last_value += 1; }],
    ['snapshot_sequence_missing', d => both(d, s => { delete s.database.sequences.fifth; })],
    ['snapshot_column_missing', d => both(d, s => { s.database.metadata.columns.activity_entries.pop(); })],
    ['snapshot_column_and_row_extra', d => both(d, s => {
      s.database.metadata.columns.sessions.push('extra'); s.database.tables.sessions.forEach(row => { row.extra = true; });
    })],
    ['snapshot_generated_column_and_values_missing', d => both(d, s => {
      s.database.metadata.columns.synthetic_table_0.pop(); delete s.database.tables.synthetic_table_0[0].generated_value;
    })],
    ['snapshot_renamed_sequence', d => both(d, s => { s.database.sequences.foreign = s.database.sequences.first; delete s.database.sequences.first; })],
    ['snapshot_sequence_string_value', d => both(d, s => { s.database.sequences.first.last_value = '100'; })],
    ['snapshot_sequence_string_called', d => both(d, s => { s.database.sequences.first.is_called = 'true'; })],
    ['snapshot_sequence_collides', d => both(d, s => { s.database.sequences.first.last_value = 1; s.database.sequences.first.is_called = false; })],
    ['snapshot_sequence_exceeds_max', d => both(d, s => { s.database.sequences.first.last_value = 1000001; })],
    ['catalog_sequence_definition_disagrees', d => {
      d.catalog.catalog.Sequences[0].Increment = 2;
    }],
    ['snapshot_row_extra_field', d => both(d, s => { s.database.tables.sessions[0].extra = true; })],
    ['snapshot_table_missing', d => both(d, s => { delete s.database.tables.synthetic_table_0; })],
    ['snapshot_old_session_count', d => both(d, s => { s.database.tables.sessions.splice(73); })],
    ['snapshot_old_device_count', d => both(d, s => { s.database.tables.devices.splice(62); })],
    ['snapshot_old_activity_count', d => both(d, s => { s.database.tables.activity_entries.splice(163); })],
    ['snapshot_root_inferred_binding', d => both(d, s => { s.database.tables.library_roots[0].storage_binding = {}; })],
    ['snapshot_root_revision_changed', d => both(d, s => { s.database.tables.library_roots[0].binding_revision = 2; })],
    ['snapshot_root_bound_by', d => both(d, s => { s.database.tables.library_roots[0].bound_by = 'synthetic'; })],
    ['snapshot_root_bound_at', d => both(d, s => { s.database.tables.library_roots[0].bound_at = '2026-09-12'; })],
    ['snapshot_activity_previous_revision', d => both(d, s => { s.database.tables.activity_entries[0].previous_revision = 1; })],
    ['snapshot_activity_fingerprint', d => both(d, s => { s.database.tables.activity_entries[0].observation_fingerprint = syntheticHash('foreign'); })],
    ['snapshot_migration_missing', d => both(d, s => { s.database.tables.schema_migrations.pop(); })],
    ['snapshot_viewer_admin', d => both(d, s => { s.database.tables.users[0].is_administrator = true; })],
    ['snapshot_target_name', d => both(d, s => { s.database.tables.items[0].name = 'foreign'; })],
    ['snapshot_target_root', d => both(d, s => { s.database.tables.items[0].root_id = 'f'.repeat(32); })],
    ['snapshot_library_name', d => both(d, s => { s.database.tables.libraries[1].name = 'foreign'; })],
  ]) test('documents_reject_' + name, () => {
    const input = inputExample(loaded.pins), documents = documentsExample(loaded.pins, input); mutate(documents);
    rejects(() => valid(input, documents), loaded);
  });
  for (const key of ['preserved_rows_sequences_credentials_recovery', 'primary_unchanged', 'shared_web_unchanged', 'rehearsal_removed', 'hba_restored_exactly'])
    test('report_requires_' + key, () => {
      const input = inputExample(loaded.pins), documents = documentsExample(loaded.pins, input); documents.report[key] = false;
      rejects(() => valid(input, documents), loaded);
    });
  for (const key of ['client_acceptance', 'automatic_retry', 'automatic_rollback']) test('report_rejects_' + key, () => {
    const input = inputExample(loaded.pins), documents = documentsExample(loaded.pins, input); documents.report[key] = true;
    rejects(() => valid(input, documents), loaded);
  });
  test('snapshot_nanosecond_integer_identity_stays_exact', () => {
    const input = inputExample(loaded.pins), documents = documentsExample(loaded.pins, input);
    documents.current.database.metadata.observed_mtime_ns = 9007199254740992n;
    documents.before.database.metadata.observed_mtime_ns = 9007199254740993n;
    rejects(() => valid(input, documents), loaded);
  });
}
function proxyExample(pins) {
  return ['/usr/bin/python3', WORK + '/client-acceptance-proxy.py', '--status-file', pins.PINS.proxy.path,
    '--goby-listen', '18196', '--goby-port', '18198', '--idle-seconds', '300'];
}

function proxyCases(test, loaded) {
  const valid = loaded.api.validateSource55ProxyArguments;
  test('proxy_arguments_bind_candidate_ports_without_resolving_reference_options', () => {
    check(valid(proxyExample(loaded.pins)) === true);
    const argv = proxyExample(loaded.pins).concat('--reference-pid=synthetic-unresolvable-reference',
      '--reference-start-ticks=opaque-reference-lifetime', '--reference-sha256=opaque-reference-hash', '--reference-listen=18197');
    check(valid(argv) === true); noEffects(loaded);
  });
  for (const [name, mutate] of [
    ['foreign_status_path', v => { v[3] = WORK + '/foreign-status.json'; }],
    ['reference_candidate_listener', v => { v[5] = '18197'; }],
    ['reference_candidate_upstream', v => { v[7] = '18097'; }],
    ['external_upstream', v => { v[7] = 'example.invalid:18198'; }],
    ['unknown_option', v => { v.push('--database', 'synthetic'); }],
    ['duplicate_option', v => { v.push('--goby-port', '18198'); }],
    ['mixed_duplicate_option', v => { v.push('--goby-port=18198'); }],
    ['missing_option_value', v => { v.push('--goby-port'); }],
    ['option_as_value', v => { v[3] = '--goby-port'; }],
    ['idle_too_short', v => { v[9] = '29'; }],
    ['idle_too_long', v => { v[9] = '3601'; }],
    ['idle_nonnumeric', v => { v[9] = 'NaN'; }],
    ['wrong_script_name', v => { v[1] = WORK + '/other-proxy.py'; }],
    ['script_path_traversal', v => { v[1] = WORK + '/nested/../client-acceptance-proxy.py'; }],
    ['ambiguous_proxy_script', v => { v.splice(1, 0, WORK + '/other/client-acceptance-proxy.py'); }],
    ['nul_argument', v => { v[0] = '/usr/bin/python3\0'; }],
    ['oversized_argument', v => { v[0] = 'x'.repeat(4097); }],
    ['oversized_argument_count', v => { v.push(...Array(33).fill('x')); }],
  ]) test('proxy_rejects_' + name, () => {
    const argv = proxyExample(loaded.pins); mutate(argv); rejects(() => valid(argv), loaded);
  });
}

function memoryFilesystem() {
  const files = new Map(), directories = new Map(), links = new Map(), stats = new Map(), listings = new Map();
  const digests = new Map(), trace = [], handles = new Set();
  let nextInode = 200n;
  const metadata = (kind, mode, size = 0) => ({ dev: 1n, ino: nextInode++, mode: BigInt(mode), uid: 0n, gid: 0n, nlink: 1n,
    size: BigInt(size), mtimeNs: 1000000n, ctimeNs: 1000000n, isFile: () => kind === 'file',
    isDirectory: () => kind === 'directory', isSymbolicLink: () => false });
  function directory(filename) {
    if (!directories.has(filename)) {
      const value = metadata('directory', [WORK, ROOT, TOOL, WORK + '/client-schema28-source55-tool-05'].includes(filename) ? 0o40700 : 0o40755);
      if (filename === WORK) value.ino = 100n;
      directories.set(filename, value);
      if (filename !== '/') directory(path.posix.dirname(filename));
    }
    return directories.get(filename);
  }
  function put(filename, bytes, expected, mode = 0o100600) {
    bytes = Buffer.from(bytes); directory(path.posix.dirname(filename));
    files.set(filename, { bytes, info: metadata('file', mode, bytes.length) });
    if (expected) {
      const actual = hash(bytes); check(!digests.has(actual) || digests.get(actual) === expected); digests.set(actual, expected);
    }
  }
  function content(filename, bytes) {
    check(files.has(filename)); const file = files.get(filename); file.bytes = Buffer.from(bytes); file.info.size = BigInt(file.bytes.length);
  }
  const record = (operation, filename) => { check(typeof filename === 'string'); trace.push({ operation, path: filename }); };
  const info = filename => {
    const value = files.get(filename)?.info ?? stats.get(filename) ?? directories.get(filename);
    check(value); return { ...value };
  };
  const filesystem = {
    async lstat(filename) { record('lstat', filename); return info(filename); },
    async stat(filename) { record('stat', filename); return info(filename); },
    async realpath(filename) { record('realpath', filename); check(files.has(filename)); return filename; },
    async readlink(filename) { record('readlink', filename); check(links.has(filename)); return links.get(filename); },
    async readdir(filename) { record('readdir', filename); check(listings.has(filename)); return [...listings.get(filename)]; },
    async open(filename) {
      record('open', filename); check(files.has(filename)); const file = files.get(filename);
      const handle = {
        async stat() { check(handles.has(handle)); return { ...file.info }; },
        async read(buffer, offset, length, position) {
          check(handles.has(handle)); const count = Math.max(0, Math.min(length, file.bytes.length - position));
          file.bytes.copy(buffer, offset, position, position + count); return { bytesRead: count, buffer };
        },
        async close() { check(handles.delete(handle)); },
      };
      handles.add(handle); return handle;
    },
  };
  const syntheticCreateHash = algorithm => {
    check(algorithm === 'sha256'); const chunks = [];
    return { update(bytes) { chunks.push(Buffer.from(bytes)); return this; },
      digest(format) { check(format === 'hex'); const actual = hash(Buffer.concat(chunks)); return digests.get(actual) ?? actual; } };
  };
  return { filesystem, createHash: syntheticCreateHash, files, directories, links, stats, listings, trace, handles, put, content, directory };
}

async function runtimeExample(raw, observeEffects = () => {}, configure = () => {}) {
  const memory = memoryFilesystem(), loaded = core(raw, memory), pins = loaded.pins;
  observeEffects(loaded.effects);
  const input = inputExample(pins), documents = documentsExample(pins, input), inputSHA256 = hash(JSON.stringify(input));
  const candidateProcess = { ...input.candidate.process, executable: '/opt/goby-client-m3e/goby', sha256: input.candidate.binary_sha256,
    uid: 995, cgroup: '/system.slice/goby-client-m3e.service', invocation: input.candidate.invocation_id };
  memory.put(ROOT + '/input.json', JSON.stringify(input), inputSHA256);
  for (const [filename, digest] of Object.entries(input.source_closure)) memory.put(filename, 'synthetic source: ' + filename, digest);
  memory.put(pins.STATE, JSON.stringify(documents.state), input.candidate.state_sha256);
  for (const [key, name] of [['upgrade_intent', 'intent'], ['upgrade_report', 'report'], ['upgrade_attestation', 'attestation'],
    ['current_snapshot', 'current'], ['before_snapshot', 'before']])
    memory.put(input.authority[key].path, JSON.stringify(documents[name]), input.authority[key].sha256);
  memory.put(documents.report.evidence['before-state.json'].path, JSON.stringify(documents.beforeState), pins.PREVIOUS.state_sha256);
  memory.put(documents.intent.source.publication.path, JSON.stringify(documents.publication), documents.intent.source.publication.sha256);
  memory.put(pins.PINS.catalog.path, JSON.stringify(documents.catalog), pins.PINS.catalog.sha256, 0o100600);
  memory.put(pins.PINS.proxy.path, JSON.stringify(documents.proxy), pins.PINS.proxy.sha256);
  for (const [key, name] of [['profile_receipt', 'profileReceipt'], ['profile_report', 'profileReport'],
    ['profile_inspection', 'profileInspection'], ['music_chain', 'musicChain'], ['music_scan_receipt', 'musicScanReceipt']])
    memory.put(pins.FIXTURES[key].path, JSON.stringify(documents[name]), pins.FIXTURES[key].sha256);
  memory.put(input.actor.credentials.path, JSON.stringify(viewerExample()), input.actor.credentials.sha256);
  memory.put(WORK + '/runtime.env', 'SYNTHETIC_RUNTIME=1\n', input.candidate.runtime_sha256);
  memory.put(input.candidate.source + '/backup-source-inputs.json', '{"synthetic":"source55 manifest"}',
    input.candidate.source_manifest_sha256, 0o100600);
  memory.put('/proc/sys/kernel/random/boot_id', input.candidate.process.boot_id + '\n');
  for (const filename of ['/proc/self/ns/net', '/proc/1/ns/net']) memory.links.set(filename, 'net:[synthetic-candidate-namespace]');
  const processStat = expected => {
    const fields = Array(20).fill('0'); fields[0] = 'S'; fields[19] = String(expected.start_ticks);
    return `${expected.pid} (synthetic-owned-process) ${fields.join(' ')}\n`;
  };
  for (const expected of [candidateProcess, pins.PRIMARY, pins.PROXY_PROCESS]) {
    memory.put(`/proc/${expected.pid}/stat`, processStat(expected));
    memory.links.set(`/proc/${expected.pid}/ns/net`, 'net:[synthetic-candidate-namespace]');
  }
  for (const expected of [candidateProcess, pins.PRIMARY]) {
    memory.stats.set(`/proc/${expected.pid}`, { uid: BigInt(expected.uid) });
    memory.put(`/proc/${expected.pid}/cgroup`, '0::' + expected.cgroup + '\n');
    memory.put(`/proc/${expected.pid}/cmdline`, expected.executable + '\0');
    memory.put(`/proc/${expected.pid}/environ`, 'INVOCATION_ID=' + expected.invocation + '\0');
    memory.put(`/proc/${expected.pid}/exe`, 'synthetic executable: ' + expected.executable, expected.sha256, 0o100755);
    memory.links.set(`/proc/${expected.pid}/exe`, expected.executable);
  }
  memory.put(`/proc/${pins.PROXY_PROCESS.pid}/cmdline`, proxyExample(pins).concat('--reference-pid=opaque-unresolved-reference').join('\0') + '\0');
  const listener = (port, inode) => `0: 0100007F:${port.toString(16).toUpperCase()} 00000000:0000 0A 0 0 0 0 0 ${inode}`;
  memory.put('/proc/self/net/tcp', 'synthetic tcp header\n' + [listener(18196, 190001), listener(18198, 190002)].join('\n') + '\n');
  memory.put('/proc/self/net/tcp6', 'synthetic tcp6 header\n');
  for (const [pid, inode] of [[pins.PROXY_PROCESS.pid, 190001], [input.candidate.process.pid, 190002]]) {
    memory.listings.set(`/proc/${pid}/fd`, ['3']); memory.links.set(`/proc/${pid}/fd/3`, `socket:[${inode}]`);
  }
  const value = { memory, loaded, input, documents, candidateProcess };
  configure(value);
  const fixture = await loaded.api.loadLibraryChangedSource55Fixture({ input, inputSHA256 });
  check(memory.handles.size === 0); noEffects(loaded);
  return { ...value, fixture };
}
function protectedFileCases(test, raw, observeEffects) {
  const example = () => {
    const memory = memoryFilesystem(), loaded = core(raw, memory);
    observeEffects(loaded.effects);
    const filename = ROOT + '/synthetic-owned.json', bytes = '{"private":"synthetic-private-fixture"}';
    const expected = hash(bytes), directories = new Map();
    memory.put(filename, bytes);
    return { memory, loaded, filename, expected, directories };
  };
  const read = value => value.loaded.api.protectedFile(value.filename, value.expected, value.directories);
  test('protected_read_keeps_bounded_owned_bytes_and_rechecks_identity', async () => {
    const value = example(), result = await read(value);
    check(result.value.private === 'synthetic-private-fixture' && result.sha256 === value.expected && value.memory.handles.size === 0);
    await value.loaded.api.protectedFile(value.filename, value.expected, value.directories, false, 2 * 1024 * 1024, [0o600n], result.identity);
    noEffects(value.loaded);
  });
  for (const [name, mutate] of [
    ['parent_symlink', v => { v.memory.directories.get(ROOT).isSymbolicLink = () => true; }],
    ['parent_group_writable', v => { v.memory.directories.get(ROOT).mode |= 0o020n; }],
    ['parent_nonroot_owner', v => { v.memory.directories.get(ROOT).uid = 995n; }],
    ['parent_nonroot_group', v => { v.memory.directories.get(ROOT).gid = 995n; }],
    ['root_not_private', v => { v.memory.directories.get(ROOT).mode = 0o40755n; }],
    ['file_symlink', v => { v.memory.files.get(v.filename).info.isSymbolicLink = () => true; }],
    ['file_not_regular', v => { v.memory.files.get(v.filename).info.isFile = () => false; }],
    ['file_hardlink', v => { v.memory.files.get(v.filename).info.nlink = 2n; }],
    ['file_nonroot_owner', v => { v.memory.files.get(v.filename).info.uid = 995n; }],
    ['file_nonroot_group', v => { v.memory.files.get(v.filename).info.gid = 995n; }],
    ['file_world_readable', v => { v.memory.files.get(v.filename).info.mode = 0o100644n; }],
    ['file_setuid', v => { v.memory.files.get(v.filename).info.mode = 0o104600n; }],
    ['file_empty', v => { v.memory.content(v.filename, ''); }],
    ['file_oversized', v => { v.memory.files.get(v.filename).info.size = 2n * 1024n * 1024n + 1n; }],
    ['content_hash_mismatch', v => { v.memory.content(v.filename, '{"private":"changed"}'); }],
    ['realpath_alias', v => { v.memory.filesystem.realpath = async () => ROOT + '/foreign.json'; }],
    ['invalid_utf8', v => { const bytes = Buffer.from([0xc3, 0x28]); v.memory.content(v.filename, bytes); v.expected = hash(bytes); }],
    ['duplicate_private_key', v => { const bytes = '{"private":1,"private":2}'; v.memory.content(v.filename, bytes); v.expected = hash(bytes); }],
  ]) test('protected_read_rejects_' + name, async () => {
    const value = example(); mutate(value);
    await rejectsAsync(() => read(value), value.loaded); check(value.memory.handles.size === 0);
  });
  for (const key of ['dev', 'ino', 'mode', 'uid', 'gid', 'nlink', 'size', 'mtimeNs', 'ctimeNs']) {
    test('protected_read_rejects_pinned_' + key + '_change', async () => {
      const value = example(), first = await read(value);
      value.memory.files.get(value.filename).info[key] += 1n;
      await rejectsAsync(() => value.loaded.api.protectedFile(value.filename, value.expected, value.directories,
        false, 2 * 1024 * 1024, [0o600n], first.identity), value.loaded);
      check(value.memory.handles.size === 0);
    });
  }
  test('protected_read_closes_handle_when_bytes_change_during_read', async () => {
    const value = example(), open = value.memory.filesystem.open;
    value.memory.filesystem.open = async (...args) => {
      const handle = await open(...args), readBytes = handle.read;
      handle.read = async (...parameters) => {
        const part = await readBytes(...parameters); value.memory.files.get(value.filename).info.mtimeNs += 1n; return part;
      };
      return handle;
    };
    await rejectsAsync(() => read(value), value.loaded); check(value.memory.handles.size === 0);
  });
}

function runtimeCases(test, raw, observeEffects) {
  const scope = value => {
    const { memory, loaded, input } = value;
    const permitted = new Set([1, input.candidate.process.pid, loaded.pins.PRIMARY.pid, loaded.pins.PROXY_PROCESS.pid]);
    for (const item of memory.trace) {
      const processPath = /^\/proc\/(\d+)(?:\/|$)/.exec(item.path);
      check(!processPath || permitted.has(Number(processPath[1])));
      check(item.path !== WORK + '/browser.json' && !item.path.startsWith('/var/lib/postgresql/'));
      check(!item.path.includes('source-attempt-44') && !item.path.includes('client-notifications-source44-continuation'));
    }
    check(memory.handles.size === 0); noEffects(loaded);
  };
  test('memory_loader_rechecks_real_authority_descriptors_without_reference_or_database_access', async () => {
    const value = await runtimeExample(raw, observeEffects), { fixture, memory } = value;
    const first = await fixture.assertPinned(), second = await fixture.assertPinned();
    check(first.phase === 'pinned' && first.check === 2 && second.check === 3);
    check(fixture.evidence.schema === 28 && fixture.evidence.source === 'source-attempt-55' &&
      fixture.evidence.candidate_invocation_id === value.input.candidate.invocation_id && Object.isFrozen(fixture.evidence));
    for (const filename of [ROOT + '/input.json', value.loaded.pins.STATE, value.input.authority.upgrade_report.path,
      value.input.authority.upgrade_attestation.path, value.input.authority.current_snapshot.path, value.input.actor.credentials.path,
      ...Object.keys(value.input.source_closure)])
      check(memory.trace.filter(item => item.operation === 'open' && item.path === filename).length >= 4);
    scope(value);
  });
  for (const [name, mutate] of [
    ['input_inode_replaced', v => { v.memory.files.get(ROOT + '/input.json').info.ino += 1n; }],
    ['input_content_changed', v => { v.memory.content(ROOT + '/input.json', JSON.stringify({ ...v.input, version: 2 })); }],
    ['report_inode_replaced', v => { v.memory.files.get(v.input.authority.upgrade_report.path).info.ino += 1n; }],
    ['attestation_bytes_changed', v => { v.memory.content(v.input.authority.upgrade_attestation.path, '{"status":"passed"}'); }],
    ['before_state_inode_replaced', v => { v.memory.files.get(v.documents.report.evidence['before-state.json'].path).info.ino += 1n; }],
    ['catalog_bytes_changed', v => { v.memory.content(v.loaded.pins.PINS.catalog.path, '{"version":27}'); }],
    ['current_snapshot_bytes_changed', v => { v.memory.content(v.input.authority.current_snapshot.path, '{"schema":28,"changed":true}'); }],
    ['before_snapshot_replaced', v => { v.memory.files.get(v.input.authority.before_snapshot.path).info.ino += 1n; }],
    ['credential_bytes_changed', v => { const viewer = viewerExample(); viewer.viewer.password = 'f'.repeat(48);
      v.memory.content(v.input.actor.credentials.path, JSON.stringify(viewer)); }],
    ['dependency_inode_replaced', v => { v.memory.files.get(TOOL + '/client-browser-session-proof.mjs').info.ino += 1n; }],
    ['dependency_bytes_changed', v => { v.memory.content(TOOL + '/client-browser-goby-fixture.mjs', 'synthetic changed source'); }],
    ['tool_directory_replaced', v => { v.memory.directories.get(TOOL).ino += 1n; }],
    ['work_directory_replaced', v => { v.memory.directories.get(WORK).ino += 1n; }],
    ['upgrade_tool_directory_replaced', v => { v.memory.directories.get(v.loaded.pins.UPGRADE_TOOL).ino += 1n; }],
    ['candidate_process_reused', v => { const filename = `/proc/${v.input.candidate.process.pid}/stat`;
      v.memory.content(filename, v.memory.files.get(filename).bytes.toString('utf8').replace('20000001', '20000002')); }],
    ['primary_executable_replaced', v => { v.memory.files.get(`/proc/${v.loaded.pins.PRIMARY.pid}/exe`).info.ino += 1n; }],
    ['proxy_lifetime_changed', v => { const filename = `/proc/${v.loaded.pins.PROXY_PROCESS.pid}/stat`;
      v.memory.content(filename, v.memory.files.get(filename).bytes.toString('utf8').replace('378464', '378465')); }],
    ['candidate_listener_missing', v => { v.memory.content('/proc/self/net/tcp', 'synthetic tcp header\n'); }],
    ['candidate_listener_owner_changed', v => { v.memory.links.set(`/proc/${v.input.candidate.process.pid}/fd/3`, 'socket:[999999]'); }],
    ['candidate_namespace_changed', v => { v.memory.links.set(`/proc/${v.input.candidate.process.pid}/ns/net`, 'net:[foreign]'); }],
    ['candidate_invocation_changed', v => { v.memory.content(`/proc/${v.input.candidate.process.pid}/environ`, 'INVOCATION_ID=foreign\0'); }],
  ]) test('memory_assert_pinned_rejects_' + name, async () => {
    const value = await runtimeExample(raw, observeEffects); mutate(value);
    let rejected = false;
    try { await value.fixture.assertPinned(); } catch (error) { rejected = error?.message === 'library_changed_source55_live_pin_failed'; }
    check(rejected); scope(value);
  });
  for (const [name, mutate] of [
    ['setuid_credentials', v => { v.memory.files.get(v.input.actor.credentials.path).info.mode = 0o104600n; }],
    ['setgid_source_dependency', v => { v.memory.files.get(TOOL + '/client-browser-session-proof.mjs').info.mode = 0o102600n; }],
    ['setuid_candidate_executable', v => { v.memory.files.get(`/proc/${v.input.candidate.process.pid}/exe`).info.mode = 0o104755n; }],
    ['setgid_tool_directory', v => { v.memory.directories.get(TOOL).mode = 0o42700n; }],
    ['nonprivate_upgrade_tool', v => { v.memory.directories.get(v.loaded.pins.UPGRADE_TOOL).mode = 0o40755n; }],
    ['world_readable_source_catalog', v => { v.memory.files.get(v.loaded.pins.PINS.catalog.path).info.mode = 0o100644n; }],
    ['world_readable_source_manifest', v => { v.memory.files.get(v.input.candidate.source + '/backup-source-inputs.json').info.mode = 0o100644n; }],
  ]) test('memory_loader_rejects_initial_' + name, async () => {
    let value, rejected = false;
    try { await runtimeExample(raw, observeEffects, current => { value = current; mutate(current); }); }
    catch (error) { rejected = /^library_changed_source55_[a-z_]+_failed$/.test(error?.message ?? ''); }
    check(rejected && value); scope(value);
  });
}

function validateCatalogMetadataInput(value) {
  check(value !== null && typeof value === 'object' && !Array.isArray(value) &&
    same(Object.keys(value).sort(), ['catalogRaw', 'catalogSHA256']) && value.catalogSHA256 === CATALOG_SHA &&
    typeof value.catalogRaw === 'string' && Buffer.byteLength(value.catalogRaw, 'utf8') > 0 &&
    Buffer.byteLength(value.catalogRaw, 'utf8') <= CATALOG_LIMIT && hash(value.catalogRaw) === CATALOG_SHA);
}

/** Read the one explicit public metadata input before any fenced guard execution. */
async function readCatalogMetadata(filename, expected) {
  check(filename === CATALOG && expected === CATALOG_SHA);
  const unchanged = (left, right) => ['dev', 'ino', 'mode', 'uid', 'gid', 'nlink', 'size', 'mtimeNs', 'ctimeNs']
    .every(key => left[key] === right[key]);
  const before = await fs.lstat(filename, { bigint: true });
  check(before.isFile() && !before.isSymbolicLink() && before.uid === 0n && before.gid === 0n && before.nlink === 1n &&
    (before.mode & 0o7777n) === 0o600n && before.size > 0n && before.size <= BigInt(CATALOG_LIMIT) && await fs.realpath(filename) === filename);
  const handle = await fs.open(filename, fsConstants.O_RDONLY | fsConstants.O_NOFOLLOW), buffer = Buffer.alloc(65536), chunks = [];
  try {
    check(unchanged(before, await handle.stat({ bigint: true })));
    let size = 0;
    for (;;) {
      const part = await handle.read(buffer, 0, buffer.length, size);
      if (!part.bytesRead) break;
      size += part.bytesRead; check(size <= CATALOG_LIMIT); chunks.push(Buffer.from(buffer.subarray(0, part.bytesRead)));
    }
    check(size === Number(before.size) && unchanged(before, await handle.stat({ bigint: true })) &&
      unchanged(before, await fs.lstat(filename, { bigint: true })) && await fs.realpath(filename) === filename);
    const bytes = Buffer.concat(chunks);
    try {
      check(hash(bytes) === expected);
      return { catalogRaw: new TextDecoder('utf-8', { fatal: true }).decode(bytes), catalogSHA256: expected };
    } finally { bytes.fill(0); }
  } finally { buffer.fill(0); for (const chunk of chunks) chunk.fill(0); await handle.close(); }
}

function realCatalogCases(test, loaded, metadata) {
  const catalog = () => loaded.api.parseSource55JSON(metadata.catalogRaw);
  const valid = value => loaded.api.catalogColumns(value);
  test('real_catalog_metadata_input_is_explicit_and_sha256_pinned', () => {
    validateCatalogMetadataInput(metadata); noEffects(loaded);
  });
  test('real_catalog_has_35_table_projections_and_197_known_index_sequence_columns', () => {
    const value = catalog(), columns = valid(value);
    const relations = new Map(value.objects.filter(row => row.kind === 'relation').map(row => [row.name, row.value.kind]));
    const extras = value.objects.filter(row => row.kind === 'column' && !row.value.dropped &&
      ['i', 'S'].includes(relations.get(row.name.slice(0, row.name.lastIndexOf('.')))));
    check(Object.keys(columns).length === 35 && extras.length === 197 &&
      extras.every(row => !Object.hasOwn(columns, row.name.slice(0, row.name.lastIndexOf('.')))));
    noEffects(loaded);
  });
  test('real_catalog_full_table_projection_keeps_generated_columns', () => {
    const value = catalog(), columns = valid(value);
    const generated = value.objects.filter(row => row.kind === 'column' && !row.value.dropped && row.value.generated !== '' &&
      Object.hasOwn(columns, row.name.slice(0, row.name.lastIndexOf('.'))));
    check(generated.every(row => columns[row.name.slice(0, row.name.lastIndexOf('.'))].includes(row.value.name))); noEffects(loaded);
  });
  for (const [name, mutate] of [
    ['unknown_column_parent', value => {
      value.objects.push({ kind: 'column', name: 'unknown_owned_parent.00001', value: { dropped: false, name: 'id' } });
    }],
    ['unsupported_parent_relation_kind', value => {
      value.objects.find(row => row.kind === 'relation' && row.value.kind === 'i').value.kind = 'm';
    }],
    ['unsupported_relation_without_columns', value => {
      value.objects.push({ kind: 'relation', name: 'unsupported_empty_parent', value: { kind: 'v' } });
    }],
    ['table_parent_claims_index_kind', value => {
      value.objects.find(row => row.kind === 'relation' && row.value.kind === 'r').value.kind = 'i';
    }],
    ['duplicate_relation_identity', value => {
      value.objects.push(cloneExact(value.objects.find(row => row.kind === 'relation')));
    }],
    ['duplicate_known_non_table_column', value => {
      const parent = value.objects.find(row => row.kind === 'relation' && row.value.kind === 'i').name;
      value.objects.push(cloneExact(value.objects.find(row => row.kind === 'column' && row.name.startsWith(parent + '.'))));
    }],
    ['undeclared_ordinary_table_parent', value => {
      value.objects.push({ kind: 'relation', name: 'undeclared_table', value: { kind: 'r' } },
        { kind: 'column', name: 'undeclared_table.00001', value: { dropped: false, name: 'id' } });
    }],
    ['non_boolean_column_drop_flag', value => {
      value.objects.find(row => row.kind === 'column').value.dropped = 'false';
    }],
    ['invalid_column_ordinal', value => {
      const column = value.objects.find(row => row.kind === 'column'); column.name = column.name.slice(0, -5) + 'abcde';
    }],
  ]) test('real_catalog_rejects_' + name, () => {
    const value = catalog(); mutate(value); rejects(() => valid(value), loaded);
  });
  test('pure_catalog_input_rejects_missing_extra_or_unpinned_metadata', () => {
    for (const value of [undefined, {}, { ...metadata, catalogSHA256: syntheticHash('foreign') },
      { ...metadata, catalogRaw: '{}' }, { ...metadata, credentials: 'synthetic-private' }])
      rejects(() => validateCatalogMetadataInput(value), loaded);
  });
}

function diagnosticCases(test, loaded, raw) {
  const diagnose = loaded.api.libraryChangedSetupDiagnostic;
  const frame = { file: 'client-library-changed-source55-fixture.mjs', line: 180, column: 7 };
  const errorWithStack = stack => Object.defineProperty({}, 'stack', { value: stack });
  const ownStack = 'Error: synthetic-private-message\n    at catalogColumns (file://' + MODULE + ':180:7)';
  test('diagnostic_keeps_only_three_known_owned_locations', () => {
    const error = errorWithStack('Error: synthetic-private-message\n' + [
      '    at foreign (/private/credentials.json:2:3)',
      ...loaded.pins.SOURCES.slice(0, 4).map((name, index) => '    at privateArgument (file://' + TOOL + '/' + name + ':' + (10 + index) + ':4)'),
    ].join('\n'));
    const value = diagnose(error, 'fixture');
    check(value.phase === 'fixture' && value.frames.length === 3 && value.frames.every(item =>
      same(Object.keys(item).sort(), ['column', 'file', 'line']) && loaded.pins.SOURCES.includes(item.file)) &&
      !JSON.stringify(value).includes('private') && !JSON.stringify(value).includes(TOOL)); noEffects(loaded);
  });
  test('diagnostic_preserves_valid_original_phase_and_frames_across_wrapping', () => {
    const error = errorWithStack(ownStack.replace(':180:7', ':999:1'));
    error.diagnostic = { phase: 'authority', frames: [clone(frame)] };
    check(same(diagnose(error, 'fixture'), { phase: 'authority', frames: [frame] })); noEffects(loaded);
  });
  test('diagnostic_uses_arguments_for_unknown_phase', () => {
    check(same(diagnose(errorWithStack(ownStack), 'synthetic-private-phase'), { phase: 'arguments', frames: [frame] })); noEffects(loaded);
  });
  test('diagnostic_rejects_foreign_stack_paths_and_oversized_raw_stack', () => {
    for (const stack of ['Error: private\n    at fail (file:///private/fixture.mjs:1:2)',
      'Error: private\n    at fail (file://' + WORK + '/client-library-changed-source55-tool-01/client-library-changed-source55-fixture.mjs:1:2)',
      'Error: ' + 'x'.repeat(65537) + '\n    at fail (file://' + MODULE + ':180:7)'])
      check(same(diagnose(errorWithStack(stack), 'fixture'), { phase: 'fixture', frames: [] }));
    noEffects(loaded);
  });
  for (const [name, mutate] of [
    ['foreign_file', value => { value.frames[0].file = 'credentials.json'; }],
    ['full_path', value => { value.frames[0].file = MODULE; }],
    ['string_line', value => { value.frames[0].line = '180'; }],
    ['zero_column', value => { value.frames[0].column = 0; }],
    ['too_many_frames', value => { value.frames = Array.from({ length: 4 }, () => clone(frame)); }],
    ['extra_private_key', value => { value.private = 'synthetic-private'; }],
    ['foreign_phase', value => { value.phase = 'synthetic-private'; }],
  ]) test('diagnostic_revalidates_supplied_' + name, () => {
    const error = errorWithStack(ownStack), value = { phase: 'authority', frames: [clone(frame)] }; mutate(value); error.diagnostic = value;
    check(same(diagnose(error, 'fixture'), { phase: 'fixture', frames: [frame] })); noEffects(loaded);
  });
  test('diagnostic_does_not_invoke_custom_data_or_stack_getters', () => {
    let calls = 0;
    const error = Object.defineProperties({}, { diagnostic: { get() { calls += 1; return {}; } },
      stack: { get() { calls += 1; return ownStack; } }, message: { get() { calls += 1; return 'synthetic-private'; } } });
    check(same(diagnose(error, 'fixture'), { phase: 'fixture', frames: [] }) && calls === 0); noEffects(loaded);
  });
  test('diagnostic_does_not_invoke_bound_custom_stack_getter', () => {
    let calls = 0;
    const getter = function () { calls += 1; return ownStack; }.bind(null);
    const error = Object.defineProperty({}, 'stack', { get: getter });
    check(same(diagnose(error, 'fixture'), { phase: 'fixture', frames: [] }) && calls === 0); noEffects(loaded);
  });
  test('diagnostic_captures_original_guard_location_before_wrapping', () => {
    let error;
    try { loaded.api.validateLibraryChangedSource55Input({}); } catch (caught) { error = caught; }
    const value = diagnose(error, 'input'), needLine = raw.slice(0, raw.indexOf('function need(')).split(/\r?\n/).length;
    check(value.phase === 'input' && value.frames.length > 0 && value.frames[0].file === frame.file && value.frames[0].line !== needLine + 2);
    noEffects(loaded);
  });
}

function snapshotReaderCases(test, loaded, raw, observeEffects) {
  test('snapshot_reader_rejects_invalid_inputs_and_keys_before_external_effects', async () => {
    for (const input of [undefined, null, {}, { ...inputExample(loaded.pins), root: WORK + '/client-library-changed-ui-source55-v1' }])
      await rejectsAsync(() => loaded.api.readLibraryChangedSource55Snapshot(input, 'before_snapshot'), loaded);
    for (const key of [undefined, null, 0, {}, '../before-full.json', 'upgrade_report'])
      await rejectsAsync(() => loaded.api.readLibraryChangedSource55Snapshot(inputExample(loaded.pins), key), loaded);
  });
  test('snapshot_reader_reads_only_each_declared_snapshot', async () => {
    const value = await runtimeExample(raw, observeEffects);
    for (const [key, expected] of [['current_snapshot', value.documents.current], ['before_snapshot', value.documents.before]]) {
      const start = value.memory.trace.length, snapshot = await value.loaded.api.readLibraryChangedSource55Snapshot(value.input, key);
      const opened = value.memory.trace.slice(start).filter(item => item.operation === 'open').map(item => item.path);
      check(same(snapshot, expected) && same(opened, [value.input.authority[key].path]));
    }
    check(value.memory.handles.size === 0); noEffects(value.loaded);
  });
  test('snapshot_reader_preserves_adjacent_unsafe_integer_values', async () => {
    const value = await runtimeExample(raw, observeEffects), filename = value.input.authority.before_snapshot.path;
    const bytes = '{"first":9007199254740992,"next":9007199254740993}';
    value.memory.content(filename, bytes); value.input.authority.before_snapshot.sha256 = hash(bytes);
    const snapshot = await value.loaded.api.readLibraryChangedSource55Snapshot(value.input, 'before_snapshot');
    check(snapshot.first === 9007199254740992n && snapshot.next === 9007199254740993n && snapshot.first !== snapshot.next);
    check(value.memory.handles.size === 0); noEffects(value.loaded);
  });
  test('snapshot_reader_rejects_nonprivate_snapshot_metadata', async () => {
    const value = await runtimeExample(raw, observeEffects);
    value.memory.files.get(value.input.authority.before_snapshot.path).info.mode = 0o100644n;
    await rejectsAsync(() => value.loaded.api.readLibraryChangedSource55Snapshot(value.input, 'before_snapshot'), value.loaded);
    check(value.memory.handles.size === 0);
  });
}

export async function runLibraryChangedSource55FixtureGuards(raw, metadata) {
  validateCatalogMetadataInput(metadata);
  const loaded = core(raw), cases = [], tests = [], effects = { filesystem: 0, http: 0, process: 0, other: 0 };
  const runtimeEffects = [];
  const test = (name, action) => { check(!cases.some(value => value.name === name)); cases.push({ name, action }); };
  inputCases(test, loaded); viewerCases(test, loaded); jsonCases(test, loaded);
  documentCases(test, loaded); proxyCases(test, loaded); protectedFileCases(test, raw, value => runtimeEffects.push(value));
  runtimeCases(test, raw, value => runtimeEffects.push(value));
  realCatalogCases(test, loaded, metadata); diagnosticCases(test, loaded, raw);
  snapshotReaderCases(test, loaded, raw, value => runtimeEffects.push(value));
  for (const { name, action } of cases) {
    const runtimeStart = runtimeEffects.length;
    for (const key of Object.keys(loaded.effects)) loaded.effects[key] = 0;
    let pass = false;
    try { await action(); noEffects(loaded); pass = true; } catch { /* Report only public case names and pass flags. */ }
    for (const key of Object.keys(effects)) effects[key] += loaded.effects[key];
    for (const counter of runtimeEffects.slice(runtimeStart)) for (const key of Object.keys(effects)) effects[key] += counter[key];
    tests.push({ name, pass });
  }
  return { result: tests.every(value => value.pass) ? 'passed' : 'blocked', mode: 'pure', harness_guards_only: true,
    client_acceptance: false, synthetic_runtime: true, metadata_input_count: 1, metadata_sha256: metadata.catalogSHA256,
    external_effect_scope: 'fenced_guard_execution', external_effect_attempts: effects, source_sha256: hash(raw),
    counts: { executed: tests.length, passed: tests.filter(value => value.pass).length, failed: tests.filter(value => !value.pass).length }, tests };
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  let metadataInputCount = 0;
  try {
    check(process.platform === 'linux' && process.getuid?.() === 0 && process.getgid?.() === 0 && process.env.SSH_CONNECTION &&
      process.argv.length === 7 && process.argv[2] === '--pure' && process.argv[3] === '--catalog' && process.argv[5] === '--catalog-sha256');
    const metadata = await readCatalogMetadata(process.argv[4], process.argv[6]); metadataInputCount = 1;
    const report = await runLibraryChangedSource55FixtureGuards(await fs.readFile(LOADER, 'utf8'), metadata);
    process.stdout.write(JSON.stringify({ result: report.result, mode: 'pure', counts: report.counts,
      metadata_input_count: report.metadata_input_count, metadata_sha256: report.metadata_sha256,
      external_effect_scope: report.external_effect_scope, external_effect_attempts: report.external_effect_attempts,
      failed: report.tests.filter(value => !value.pass).map(value => value.name) }) + '\n');
    if (report.result !== 'passed') process.exitCode = 1;
  } catch { process.stdout.write(JSON.stringify({ result: 'blocked', mode: 'pure', metadata_input_count: metadataInputCount }) + '\n'); process.exitCode = 1; }
}
