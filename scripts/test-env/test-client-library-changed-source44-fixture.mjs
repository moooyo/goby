#!/usr/bin/env node
/** Exercise source44 fixture guards with synthetic documents and denied external effects. */
import fs from 'node:fs/promises';
import { constants as fsConstants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { TextDecoder } from 'node:util';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const WORK = '/opt/goby-test/exec-work-m3e';
const ROOT = WORK + '/client-library-changed-ui-source44-v1';
const TOOL = WORK + '/client-library-changed-source44-tool-02';
const MODULE = TOOL + '/client-library-changed-source44-fixture.mjs';
const SELF = fileURLToPath(import.meta.url);
const LOADER = fileURLToPath(new URL('./client-library-changed-source44-fixture.mjs', import.meta.url));
const hash = value => createHash('sha256').update(value).digest('hex');
const syntheticHash = value => hash('synthetic-source44-fixture:' + value);
const clone = value => JSON.parse(JSON.stringify(value));
const ordered = value => Array.isArray(value) ? value.map(ordered) : value !== null && typeof value === 'object'
  ? Object.fromEntries(Object.keys(value).sort().map(key => [key, ordered(value[key])])) : value;
const same = (left, right) => JSON.stringify(ordered(left)) === JSON.stringify(ordered(right));
function check(value) { if (!value) throw new Error('source44_fixture_test_failed'); }

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
  const deny = kind => { effects[kind] += 1; throw new Error('source44_fixture_external_effect'); };
  const denied = kind => new Proxy(function () {}, {
    get() { return deny(kind); }, apply() { return deny(kind); }, construct() { return deny(kind); },
  });
  const sandbox = {
    fs: mocks.filesystem ?? denied('filesystem'), constants: mocks.filesystem ? fsConstants : denied('filesystem'),
    path: { posix: path.posix }, createHash: mocks.createHash ?? createHash, TextDecoder,
    fileURLToPath, Buffer, URL, process: new Proxy(Object.freeze({ platform: 'linux', getuid: () => 0, argv: [], env: {} }), {
      get(target, key) { return Object.hasOwn(target, key) ? target[key] : deny('process'); },
    }), fetch: denied('http'), console: denied('other'), setTimeout: denied('other'), setInterval: denied('other'),
  };
  const context = vm.createContext(sandbox, { codeGeneration: { strings: false, wasm: false } });
  const publicNames = ['validateLibraryChangedSource44Input', 'validateLibraryChangedSource44Documents',
    'validateLibraryChangedSource44Viewer', 'parseSource44JSON', 'validateSource44ProxyArguments', 'loadLibraryChangedSource44Fixture'];
  new vm.Script(source + '\nglobalThis.result = {api: {' + publicNames.join(',') + '}, pins: {' + declarations.join(',') + '}};')
    .runInContext(context, { timeout: 1000 });
  check(Object.values(effects).every(value => value === 0));
  return { ...sandbox.result, effects };
}

function noEffects(loaded) { check(Object.values(loaded.effects).every(value => value === 0)); }
function rejects(action, loaded) {
  let rejected = false;
  try { action(); } catch (error) { rejected = error?.message !== 'source44_fixture_external_effect'; }
  check(rejected); noEffects(loaded);
}
async function rejectsAsync(action, loaded) {
  let rejected = false;
  try { await action(); } catch (error) { rejected = error?.message !== 'source44_fixture_external_effect'; }
  check(rejected); noEffects(loaded);
}

function viewerExample() {
  return { marker: 'goby-client-library-changed-viewer-v1', slot: 'B', user_id: 'ecbbe4cb82403879bc4b4f78894c5738',
    base_url: 'http://127.0.0.1:18196', direct_url: 'http://127.0.0.1:18198',
    viewer: { username: 'm3e-client-viewer', password: '0123456789abcdef'.repeat(3) } };
}

function inputExample(pins) {
  return { marker: 'goby-client-library-changed-input-v1', version: 1, mode: 'b-movies-name-automatic-refresh',
    root: ROOT, output: ROOT + '/browser',
    actor: { slot: 'B', user_id: 'ecbbe4cb82403879bc4b4f78894c5738', account_key: 'viewer',
      source_credentials_sha256: '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790',
      credentials: { path: ROOT + '/viewer-credentials.json', sha256: hash(JSON.stringify(viewerExample())) } },
    candidate: { binary_sha256: 'cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d',
      process: { pid: 1264063, start_ticks: 11104222, boot_id: '6bdfc486-7bc8-412f-82b5-70095a09dde7' },
      runtime_sha256: 'd8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df',
      state_sha256: 'd6b8872eab70848b834c50e26cc2b84e9d137dd68049a1be85d09afa811829d4',
      server_id: 'c7cfd76b1dee728b2bad523793a37ccb', base_url: 'http://127.0.0.1:18196', direct_url: 'http://127.0.0.1:18198',
      source: WORK + '/source-attempt-44',
      source_manifest_sha256: 'c2c9492589360b058c6533d0719219cf85ab5bc056bd76290cee51e7dc897f8b' },
    fixture: clone(pins.FIXTURES), expected_libraries: [
      { id: 'a9993591e72f0f2e7babcbf8b9c50790', name: 'M3e Client Movies' },
      { id: '6383d20008836e137559698c29b10395', name: 'M3e Client Music' },
      { id: 'a34ce665fb75421ef7551570f353d705', name: 'M3e Client TV' },
      { id: '57a85c1ca5b6c7ae602c587755250b2f', name: 'M3e Client Special Features Movies' },
    ], target: { id: '268051d3ca734aefcf94e245fb25ad55', library_id: 'a9993591e72f0f2e7babcbf8b9c50790',
      name: 'Synthetic Source44 Movie', type: 'Movie', parent_id: 'a'.repeat(32), root_id: 'b'.repeat(32),
      relative_path: 'Synthetic Source44 Movie/Synthetic Source44 Movie.mp4' },
    source_closure: Object.fromEntries(pins.SOURCES.map(name => [TOOL + '/' + name, syntheticHash(name)])),
    authority: { continuation_report: clone(pins.PINS.report), continuation_terminal: clone(pins.PINS.terminal),
      current_snapshot: clone(pins.PINS.current), before_snapshot: { path: ROOT + '/before-full.json', sha256: syntheticHash('before') } },
    controller: { pid: 900001, start_ticks: '10001', boot_id: '6bdfc486-7bc8-412f-82b5-70095a09dde7',
      unit: 'goby-client-library-changed-ui-source44-controller-v1.service' } };
}

function inputCases(test, loaded) {
  const valid = input => loaded.api.validateLibraryChangedSource44Input(input);
  test('complete_source44_input_uses_exact_seven_module_paths', () => {
    const input = inputExample(loaded.pins);
    valid(input); check(Object.keys(input.source_closure).length === 7); noEffects(loaded);
  });
  for (const [name, mutate] of [
    ['source32_candidate_binary', v => { v.candidate.binary_sha256 = 'af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620'; }],
    ['old_candidate_process', v => { v.candidate.process.pid = 748513; v.candidate.process.start_ticks = 6996875; }],
    ['changed_process_start_ticks', v => { v.candidate.process.start_ticks += 1; }],
    ['string_candidate_pid', v => { v.candidate.process.pid = String(v.candidate.process.pid); }],
    ['bigint_candidate_pid', v => { v.candidate.process.pid = BigInt(v.candidate.process.pid); }],
    ['bigint_candidate_start_ticks', v => { v.candidate.process.start_ticks = BigInt(v.candidate.process.start_ticks); }],
    ['changed_boot_identity', v => { v.candidate.process.boot_id = '00000000-0000-0000-0000-000000000001'; }],
    ['source32_source_directory', v => { v.candidate.source = WORK + '/source-attempt-32'; }],
    ['changed_source_manifest', v => { v.candidate.source_manifest_sha256 = syntheticHash('foreign-source'); }],
    ['changed_runtime_pin', v => { v.candidate.runtime_sha256 = syntheticHash('foreign-runtime'); }],
    ['changed_state_pin', v => { v.candidate.state_sha256 = syntheticHash('foreign-state'); }],
    ['changed_server_identity', v => { v.candidate.server_id = 'f'.repeat(32); }],
    ['reference_base_authority', v => { v.candidate.base_url = 'http://127.0.0.1:18197'; }],
    ['nonlocal_direct_authority', v => { v.candidate.direct_url = 'http://localhost:18198'; }],
    ['candidate_extra_authority', v => { v.candidate.reference_url = 'http://127.0.0.1:18197'; }],
    ['actor_slot_a', v => { v.actor.slot = 'A'; }],
    ['actor_foreign_user', v => { v.actor.user_id = 'f'.repeat(32); }],
    ['actor_admin_account', v => { v.actor.account_key = 'admin'; }],
    ['actor_inline_password', v => { v.actor.password = 'synthetic-forbidden-password'; }],
    ['legacy_browser_credentials', v => { v.actor.credentials.path = WORK + '/browser.json'; }],
    ['other_run_credentials', v => { v.actor.credentials.path = WORK + '/other-run/viewer-credentials.json'; }],
    ['wrong_source_credential_hash', v => { v.actor.source_credentials_sha256 = syntheticHash('wrong-source-credentials'); }],
    ['credentials_descriptor_extra_key', v => { v.actor.credentials.slot = 'B'; }],
    ['credentials_path_traversal', v => { v.actor.credentials.path = ROOT + '/child/../viewer-credentials.json'; }],
    ['credentials_path_backslash', v => { v.actor.credentials.path = ROOT + '\\viewer-credentials.json'; }],
    ['credentials_hash_uppercase', v => { v.actor.credentials.sha256 = v.actor.credentials.sha256.toUpperCase(); }],
    ['unknown_input_field', v => { v.reference = {}; }],
    ['wrong_input_marker', v => { v.marker = 'goby-client-library-home-input-v1'; }],
    ['string_input_version', v => { v.version = '1'; }],
    ['wrong_run_directory', v => { v.root = ROOT + '-other'; }],
    ['output_outside_run', v => { v.output = WORK + '/browser'; }],
    ['wrong_target_library', v => { v.target.library_id = v.expected_libraries[1].id; }],
    ['wrong_target_item', v => { v.target.id = 'f'.repeat(32); }],
    ['target_relative_escape', v => { v.target.relative_path = '../foreign.mp4'; }],
    ['target_absolute_path', v => { v.target.relative_path = '/foreign.mp4'; }],
    ['target_invalid_name', v => { v.target.name = 'synthetic\nname'; }],
    ['missing_expected_library', v => { v.expected_libraries.pop(); }],
    ['duplicate_expected_library', v => { v.expected_libraries[1] = clone(v.expected_libraries[0]); }],
    ['foreign_expected_library', v => { v.expected_libraries[1].id = 'f'.repeat(32); }],
    ['wrong_controller_unit', v => { v.controller.unit = 'goby-client-library-ui-baseline-v3.service'; }],
    ['numeric_controller_ticks', v => { v.controller.start_ticks = 10001; }],
    ['controller_uses_candidate_pid', v => { v.controller.pid = v.candidate.process.pid; }],
    ['controller_different_boot', v => { v.controller.boot_id = '00000000-0000-0000-0000-000000000001'; }],
    ['before_snapshot_outside_run', v => { v.authority.before_snapshot.path = WORK + '/foreign/before-full.json'; }],
    ['before_snapshot_missing_hash', v => { delete v.authority.before_snapshot.sha256; }],
    ['missing_source_dependency', v => { delete v.source_closure[TOOL + '/client-browser-goby-fixture.mjs']; }],
    ['missing_transitive_profile_dependency', v => { delete v.source_closure[TOOL + '/client-browser-special-features-fixture.mjs']; }],
    ['same_basename_in_another_directory', v => {
      const filename = TOOL + '/client-browser-session-proof.mjs';
      v.source_closure[WORK + '/foreign-tool/client-browser-session-proof.mjs'] = v.source_closure[filename]; delete v.source_closure[filename];
    }],
    ['additional_same_basename_dependency', v => {
      v.source_closure[WORK + '/foreign-tool/client-browser-session-proof.mjs'] = syntheticHash('foreign');
    }],
    ['unreviewed_extra_dependency', v => { v.source_closure[TOOL + '/extra.mjs'] = syntheticHash('extra'); }],
    ['source_dependency_path_traversal', v => {
      const filename = TOOL + '/client-browser-session-proof.mjs';
      v.source_closure[TOOL + '/nested/../client-browser-session-proof.mjs'] = v.source_closure[filename]; delete v.source_closure[filename];
    }],
  ]) {
    test('input_rejects_' + name, async () => {
      const input = inputExample(loaded.pins); mutate(input);
      rejects(() => valid(input), loaded);
      await rejectsAsync(() => loaded.api.loadLibraryChangedSource44Fixture({ input, inputSHA256: syntheticHash('input') }), loaded);
    });
  }
  for (const key of ['profile_receipt', 'profile_report', 'profile_inspection', 'music_chain', 'music_scan_receipt']) {
    test('input_rejects_changed_' + key + '_descriptor', () => {
      for (const field of ['path', 'sha256']) {
        const input = inputExample(loaded.pins);
        input.fixture[key][field] = field === 'path' ? WORK + '/foreign/' + path.posix.basename(input.fixture[key].path) : syntheticHash('foreign-' + key);
        rejects(() => valid(input), loaded);
      }
    });
  }
  for (const key of ['continuation_report', 'continuation_terminal', 'current_snapshot']) {
    test('input_rejects_changed_' + key + '_authority', () => {
      for (const field of ['path', 'sha256']) {
        const input = inputExample(loaded.pins);
        input.authority[key][field] = field === 'path' ? WORK + '/foreign/' + path.posix.basename(input.authority[key].path) : syntheticHash('foreign-' + key);
        rejects(() => valid(input), loaded);
      }
    });
  }
  test('invalid_loader_options_reject_before_external_effects', async () => {
    for (const options of [undefined, null, {}, { input: inputExample(loaded.pins) },
      { input: inputExample(loaded.pins), inputSHA256: 'invalid' }, { input: inputExample(loaded.pins), inputSHA256: 'A'.repeat(64) },
      { input: inputExample(loaded.pins), inputSHA256: syntheticHash('input'), reference: {} },
      { input: null, inputSHA256: syntheticHash('input') }]) {
      await rejectsAsync(() => loaded.api.loadLibraryChangedSource44Fixture(options), loaded);
    }
  });
}

function viewerCases(test, loaded) {
  test('isolated_b_viewer_has_no_legacy_credential_carrier', () => {
    const viewer = viewerExample(), before = clone(viewer);
    loaded.api.validateLibraryChangedSource44Viewer(viewer);
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
      rejects(() => loaded.api.validateLibraryChangedSource44Viewer(viewer), loaded);
    });
  }
}

function jsonCases(test, loaded) {
  const parse = loaded.api.parseSource44JSON;
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
    check(message === 'library_changed_source44_binding_rejected' && !message.includes('synthetic-private-fragment')); noEffects(loaded);
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
  const binding = { marker: 'goby-client-schema27-source-m3e-v1', schema: 27, source: input.candidate.source,
    source_manifest_sha256: input.candidate.source_manifest_sha256,
    catalog_sha256: '1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d',
    migration_27_sha256: 'b62d0422dddb9e258f46589898f672b08fc6e4c12fea7456059f855a6600353c' };
  const service = value => ({ MainPID: String(value.pid), InvocationID: value.invocation, ActiveState: 'active', SubState: 'running' });
  const continuation = { original_state_sha256: syntheticHash('original-state'), durable_phase: 'staged', dispatched_service_actions: [] };
  const oldProcess = { pid: 748513, start_ticks: 6996875, boot_id: input.candidate.process.boot_id };
  const completed = { marker: pins.CONTINUATION_MARKER, id: pins.CONTINUATION_MARKER, phase: 'complete', from_schema: 27, to_schema: 27,
    to_sha256: input.candidate.binary_sha256, installed_binary_sha256: input.candidate.binary_sha256,
    new_process: clone(input.candidate.process), old_process: oldProcess, new_service: service(pins.CANDIDATE_PROCESS),
    evidence_directory: pins.CONTINUATION, publication: pins.PUBLICATION, continuation,
    schema_artifacts: { source: binding.source, source_manifest_sha256: binding.source_manifest_sha256, schema27_binding: clone(binding) } };
  completed.new_service.ControlGroup = pins.CANDIDATE_PROCESS.cgroup;
  const state = { marker: 'goby-m3e-client-acceptance-v1', work: WORK, schema: 27, phase: 'ready', stage: 'complete',
    server_id: input.candidate.server_id, viewer_id: input.actor.user_id, binary_sha256: input.candidate.binary_sha256,
    runtime_sha256: input.candidate.runtime_sha256, browser_sha256: input.actor.source_credentials_sha256,
    process: clone(input.candidate.process), work_identity: { device: 1, inode: 100 }, schema27_source: clone(binding),
    upgrade: clone(completed), upgrade_history: [clone(completed)],
    notifications_upgrade: { marker: pins.CONTINUATION_MARKER, phase: 'complete', evidence_directory: pins.CONTINUATION,
      publication: pins.PUBLICATION, binary_sha256: input.candidate.binary_sha256, continuation: clone(continuation) },
    special_features_profile: { receipt_path: pins.FIXTURES.profile_receipt.path, receipt_sha256: pins.FIXTURES.profile_receipt.sha256 } };
  const report = { marker: pins.CONTINUATION_MARKER, status: 'passed', schema: 27, state_sha256: input.candidate.state_sha256,
    source_manifest_sha256: input.candidate.source_manifest_sha256, binary: { sha256: input.candidate.binary_sha256 },
    publication: pins.PUBLICATION, new_process: clone(input.candidate.process), old_process: clone(oldProcess),
    continuation: clone(continuation), http_requests: 2, service_actions: ['stop', 'start'],
    complete_rows_sequences_catalog_preserved: true, credentials_recovery_and_three_media_groups_preserved: true,
    primary_identity_unchanged: true, original_failure_trees_preserved: true, web_assets_replaced: false,
    schema_migration_performed: false, client_acceptance: false, automatic_retry: false, automatic_rollback: false,
    evidence: { 'completed.json': clone(pins.PINS.completed), 'after-full.json': clone(pins.PINS.current) } };
  const terminal = { marker: 'goby-client-notifications-source44-continuation-terminal-v1', status: 'passed', candidate_source: 44,
    unit: pins.CONTINUATION_UNIT, fixture_phase: 'ready', fixture_stage: 'complete',
    candidate_schema: 27, client_acceptance: false, candidate_binary_sha256: input.candidate.binary_sha256,
    primary_binary_sha256: pins.PRIMARY.sha256, state_sha256: input.candidate.state_sha256, report_path: pins.PINS.report.path,
    report_sha256: pins.PINS.report.sha256, completed_sha256: pins.PINS.completed.sha256, recursive_cgroup_empty: true,
    candidate_service: service(pins.CANDIDATE_PROCESS), primary_service: service(pins.PRIMARY),
    state: { Result: 'success', ExecMainStatus: '0', MainPID: '0', SubState: 'exited', ControlGroup: '',
      InvocationID: pins.CONTINUATION_INVOCATION } };
  const profileReceipt = { marker: 'goby-client-special-features-fixture-v1', phase: 'complete', schema: 27, profile_version: 3,
    candidate: { binary_sha256: pins.PRIMARY.sha256, process: clone(oldProcess) },
    upgrade: { completed_sha256: syntheticHash('historical-completed'), report_sha256: syntheticHash('historical-report') } };
  const profileReport = { result: 'passed', phase: 'complete', receipt_sha256: pins.FIXTURES.profile_receipt.sha256 };
  const profileInspection = { result: 'passed', phase: 'complete', schema: 27, receipt_sha256: pins.FIXTURES.profile_receipt.sha256 };
  const musicChain = Array.from({ length: 5 }, (_, index) => ({ completedSHA256: syntheticHash('music-completed-' + index),
    reportSHA256: syntheticHash('music-report-' + index) }));
  musicChain[4] = { completedSHA256: profileReceipt.upgrade.completed_sha256, reportSHA256: profileReceipt.upgrade.report_sha256 };
  const musicScanReceipt = { schema: 25, phase: 'complete', result: 'passed', logout_status: 204, token_readback_status: 401 };
  const tables = Object.fromEntries(['sessions', 'devices', 'activity_entries', 'play_sessions', 'user_item_data', 'libraries', 'items', 'users']
    .map(name => [name, []]));
  for (let index = 0; index < 27; index += 1) tables['synthetic_table_' + index] = [];
  for (const [name, count] of [['sessions', 73], ['devices', 62], ['activity_entries', 163], ['play_sessions', 26], ['user_item_data', 7]])
    tables[name] = Array.from({ length: count }, (_, index) => ({ id: name + '-' + index }));
  tables.libraries = clone(input.expected_libraries);
  tables.users = [{ id: input.actor.user_id, is_administrator: false, is_disabled: false, management_revision: 5 }];
  tables.items = [{ ...clone(input.target), is_folder: false },
    ...Array.from({ length: 21 }, (_, index) => ({ id: syntheticHash('item-' + index).slice(0, 32), name: 'Synthetic item ' + index }))];
  const current = { schema: 27, runtime_sha256: input.candidate.runtime_sha256, browser_sha256: input.actor.source_credentials_sha256,
    database: { metadata: { captured_at: '2026-09-12T04:51:00.000000Z', database_oid: 100, role_oid: 101 },
      catalog: { sha256: syntheticHash('catalog') }, sequences: { synthetic_sequence: 100 }, tables } };
  const before = clone(current); before.database.metadata.captured_at = '2026-09-12T05:00:00.000000Z';
  return { state, report, completed, terminal, current, before, profileReceipt, profileReport, profileInspection, musicChain, musicScanReceipt,
    proxy: { pid: pins.PROXY_PROCESS.pid, start_ticks: String(pins.PROXY_PROCESS.start_ticks), reference_only: false,
      listen: ['127.0.0.1:18196', '127.0.0.1:18197'] } };
}

function documentCases(test, loaded) {
  const valid = (input, documents) => loaded.api.validateLibraryChangedSource44Documents(input, documents);
  const bothSnapshots = (documents, mutate) => { mutate(documents.current); mutate(documents.before); };
  const completed = (documents, mutate) => {
    mutate(documents.completed); documents.state.upgrade = clone(documents.completed);
    documents.state.upgrade_history[documents.state.upgrade_history.length - 1] = clone(documents.completed);
  };
  test('complete_continuation_chain_retains_historical_source32_anchors', () => {
    const input = inputExample(loaded.pins), documents = documentsExample(loaded.pins, input);
    check(documents.profileReceipt.candidate.binary_sha256 !== input.candidate.binary_sha256);
    check(valid(input, documents) === true); noEffects(loaded);
  });
  for (const [name, mutate] of [
    ['state_schema26', d => { d.state.schema = 26; }],
    ['string_state_schema', d => { d.state.schema = '27'; }],
    ['incomplete_state_phase', d => { d.state.phase = 'upgrading'; }],
    ['incomplete_state_stage', d => { d.state.stage = 'notifications_staged'; }],
    ['foreign_work_directory', d => { d.state.work = WORK + '-foreign'; }],
    ['wrong_state_candidate', d => { d.state.binary_sha256 = syntheticHash('foreign'); }],
    ['wrong_state_process', d => { d.state.process.pid += 1; }],
    ['wrong_state_viewer', d => { d.state.viewer_id = 'f'.repeat(32); }],
    ['wrong_state_credential_origin', d => { d.state.browser_sha256 = syntheticHash('foreign'); }],
    ['wrong_state_runtime', d => { d.state.runtime_sha256 = syntheticHash('foreign'); }],
    ['unbound_state_work_identity', d => { delete d.state.work_identity; }],
    ['source32_schema_binding', d => { d.state.schema27_source.source = WORK + '/source-attempt-32'; }],
    ['wrong_schema_catalog', d => { d.state.schema27_source.catalog_sha256 = syntheticHash('catalog'); }],
    ['wrong_schema_migration', d => { d.state.schema27_source.migration_27_sha256 = syntheticHash('migration'); }],
    ['schema_binding_extra_key', d => { d.state.schema27_source.foreign = true; }],
    ['retained_failure_report', d => { d.report.status = 'retained_for_review'; }],
    ['wrong_report_schema', d => { d.report.schema = 26; }],
    ['report_state_hash_conflict', d => { d.report.state_sha256 = syntheticHash('foreign'); }],
    ['report_candidate_hash_conflict', d => { d.report.binary.sha256 = syntheticHash('foreign'); }],
    ['report_wrong_publication', d => { d.report.publication = '0'.repeat(40); }],
    ['report_wrong_new_process', d => { d.report.new_process.start_ticks += 1; }],
    ['report_extra_http_request', d => { d.report.http_requests = 3; }],
    ['report_extra_service_action', d => { d.report.service_actions.push('restart'); }],
    ['report_unpreserved_database', d => { d.report.complete_rows_sequences_catalog_preserved = false; }],
    ['report_unpreserved_credentials', d => { d.report.credentials_recovery_and_three_media_groups_preserved = false; }],
    ['report_unpreserved_failure_trees', d => { d.report.original_failure_trees_preserved = false; }],
    ['report_changed_primary', d => { d.report.primary_identity_unchanged = false; }],
    ['report_replaced_web_assets', d => { d.report.web_assets_replaced = true; }],
    ['report_migrated_schema', d => { d.report.schema_migration_performed = true; }],
    ['report_claims_client_acceptance', d => { d.report.client_acceptance = true; }],
    ['report_allows_retry', d => { d.report.automatic_retry = true; }],
    ['report_allows_rollback', d => { d.report.automatic_rollback = true; }],
    ['report_completed_hash_conflict', d => { d.report.evidence['completed.json'].sha256 = syntheticHash('foreign'); }],
    ['report_current_path_conflict', d => { d.report.evidence['after-full.json'].path = WORK + '/foreign/after-full.json'; }],
    ['completed_schema26_transition', d => completed(d, value => { value.from_schema = 26; })],
    ['completed_incomplete_phase', d => completed(d, value => { value.phase = 'started'; })],
    ['completed_install_hash_conflict', d => completed(d, value => { value.installed_binary_sha256 = syntheticHash('foreign'); })],
    ['completed_continuation_conflict', d => completed(d, value => { value.continuation.original_state_sha256 = syntheticHash('foreign'); })],
    ['completed_old_process_conflict', d => completed(d, value => { value.old_process.pid += 1; })],
    ['completed_source_manifest_conflict', d => completed(d, value => { value.schema_artifacts.source_manifest_sha256 = syntheticHash('foreign'); })],
    ['completed_current_service_conflict', d => completed(d, value => { value.new_service.InvocationID = 'f'.repeat(32); })],
    ['completed_candidate_cgroup_conflict', d => completed(d, value => { value.new_service.ControlGroup = '/system.slice/foreign.service'; })],
    ['state_upgrade_receipt_conflict', d => { d.state.upgrade.id = 'foreign'; }],
    ['state_upgrade_history_missing', d => { d.state.upgrade_history = []; }],
    ['state_upgrade_history_terminal_conflict', d => { d.state.upgrade_history.push({ phase: 'complete' }); }],
    ['notification_phase_conflict', d => { d.state.notifications_upgrade.phase = 'staged'; }],
    ['notification_continuation_conflict', d => { d.state.notifications_upgrade.continuation = {}; }],
    ['terminal_wrong_source', d => { d.terminal.candidate_source = 32; }],
    ['terminal_wrong_schema', d => { d.terminal.candidate_schema = 26; }],
    ['terminal_report_path_conflict', d => { d.terminal.report_path = WORK + '/foreign/report.json'; }],
    ['terminal_completed_hash_conflict', d => { d.terminal.completed_sha256 = syntheticHash('foreign'); }],
    ['terminal_report_hash_conflict', d => { d.terminal.report_sha256 = syntheticHash('foreign'); }],
    ['terminal_state_hash_conflict', d => { d.terminal.state_sha256 = syntheticHash('foreign'); }],
    ['terminal_active_controller', d => { d.terminal.state.MainPID = '9001'; }],
    ['terminal_wrong_unit', d => { d.terminal.unit = 'foreign-controller.service'; }],
    ['terminal_wrong_controller_invocation', d => { d.terminal.state.InvocationID = 'f'.repeat(32); }],
    ['terminal_controller_cgroup_retained', d => { d.terminal.state.ControlGroup = '/system.slice/foreign-controller.service'; }],
    ['terminal_fixture_not_ready', d => { d.terminal.fixture_phase = 'upgrading'; }],
    ['terminal_fixture_not_complete', d => { d.terminal.fixture_stage = 'started'; }],
    ['terminal_nonempty_cgroup', d => { d.terminal.recursive_cgroup_empty = false; }],
    ['terminal_candidate_invocation_conflict', d => { d.terminal.candidate_service.InvocationID = 'f'.repeat(32); }],
    ['terminal_primary_pid_conflict', d => { d.terminal.primary_service.MainPID = '762091'; }],
    ['terminal_primary_hash_conflict', d => { d.terminal.primary_binary_sha256 = syntheticHash('foreign'); }],
    ['profile_incomplete', d => { d.profileReceipt.phase = 'scan_complete'; }],
    ['profile_wrong_schema', d => { d.profileReceipt.schema = 26; }],
    ['profile_wrong_version', d => { d.profileReceipt.profile_version = 2; }],
    ['profile_state_receipt_conflict', d => { d.state.special_features_profile.receipt_sha256 = syntheticHash('foreign'); }],
    ['profile_report_receipt_conflict', d => { d.profileReport.receipt_sha256 = syntheticHash('foreign'); }],
    ['profile_inspection_failed', d => { d.profileInspection.result = 'blocked'; }],
    ['profile_inspection_wrong_schema', d => { d.profileInspection.schema = 26; }],
    ['music_history_length_conflict', d => { d.musicChain.pop(); }],
    ['music_history_completed_conflict', d => { d.musicChain[4].completedSHA256 = syntheticHash('foreign'); }],
    ['music_history_report_conflict', d => { d.musicChain[4].reportSHA256 = syntheticHash('foreign'); }],
    ['music_scan_wrong_schema', d => { d.musicScanReceipt.schema = 27; }],
    ['music_scan_incomplete', d => { d.musicScanReceipt.phase = 'running'; }],
    ['music_session_not_logged_out', d => { d.musicScanReceipt.logout_status = 200; }],
    ['music_token_not_revoked', d => { d.musicScanReceipt.token_readback_status = 200; }],
    ['proxy_process_conflict', d => { d.proxy.pid += 1; }],
    ['proxy_lifetime_conflict', d => { d.proxy.start_ticks = String(Number(d.proxy.start_ticks) + 1); }],
    ['proxy_reference_only', d => { d.proxy.reference_only = true; }],
    ['proxy_missing_candidate_listener', d => { d.proxy.listen = ['127.0.0.1:18197']; }],
    ['proxy_duplicate_candidate_listener', d => { d.proxy.listen.push('127.0.0.1:18196'); }],
    ['snapshot_schema26', d => bothSnapshots(d, value => { value.schema = 26; })],
    ['snapshot_runtime_conflict', d => bothSnapshots(d, value => { value.runtime_sha256 = syntheticHash('foreign'); })],
    ['snapshot_credentials_conflict', d => bothSnapshots(d, value => { value.browser_sha256 = syntheticHash('foreign'); })],
    ['snapshot_baseline_predates_current', d => { d.before.database.metadata.captured_at = '2026-09-12T04:00:00Z'; }],
    ['snapshot_bad_capture_time', d => { d.before.database.metadata.captured_at = 'invalid'; }],
    ['snapshot_catalog_conflict', d => { d.before.database.catalog.sha256 = syntheticHash('foreign'); }],
    ['snapshot_sequence_conflict', d => { d.before.database.sequences.synthetic_sequence += 1; }],
    ['snapshot_metadata_identity_conflict', d => { d.before.database.metadata.database_oid += 1; }],
    ['snapshot_table_membership_conflict', d => bothSnapshots(d, value => { delete value.database.tables.synthetic_table_0; })],
    ['snapshot_session_count_conflict', d => bothSnapshots(d, value => { value.database.tables.sessions.pop(); })],
    ['snapshot_viewer_admin', d => bothSnapshots(d, value => { value.database.tables.users[0].is_administrator = true; })],
    ['snapshot_viewer_disabled', d => bothSnapshots(d, value => { value.database.tables.users[0].is_disabled = true; })],
    ['snapshot_viewer_revision_conflict', d => bothSnapshots(d, value => { value.database.tables.users[0].management_revision = 6; })],
    ['snapshot_target_name_conflict', d => bothSnapshots(d, value => { value.database.tables.items[0].name = 'Foreign movie'; })],
    ['snapshot_target_root_conflict', d => bothSnapshots(d, value => { value.database.tables.items[0].root_id = 'f'.repeat(32); })],
    ['snapshot_target_library_conflict', d => bothSnapshots(d, value => { value.database.tables.items[0].library_id = 'f'.repeat(32); })],
    ['snapshot_target_folder_conflict', d => bothSnapshots(d, value => { value.database.tables.items[0].is_folder = true; })],
    ['snapshot_expected_library_name_conflict', d => bothSnapshots(d, value => { value.database.tables.libraries[1].name = 'Foreign music'; })],
  ]) {
    test('documents_reject_' + name, () => {
      const input = inputExample(loaded.pins), documents = documentsExample(loaded.pins, input); mutate(documents);
      rejects(() => valid(input, documents), loaded);
    });
  }
  test('snapshot_nanosecond_difference_remains_distinct', () => {
    const input = inputExample(loaded.pins), documents = documentsExample(loaded.pins, input);
    documents.current.database.metadata.observed_mtime_ns = 9007199254740992n;
    documents.before.database.metadata.observed_mtime_ns = 9007199254740993n;
    rejects(() => valid(input, documents), loaded);
  });
  test('snapshot_bigint_and_number_types_are_not_coerced', () => {
    const input = inputExample(loaded.pins), documents = documentsExample(loaded.pins, input);
    documents.current.database.metadata.role_oid = 101n;
    rejects(() => valid(input, documents), loaded);
  });
}

function proxyExample(pins) {
  return ['/usr/bin/python3', WORK + '/client-acceptance-proxy.py', '--status-file', pins.PINS.proxy.path,
    '--goby-listen', '18196', '--goby-port', '18198', '--idle-seconds', '300'];
}

function proxyCases(test, loaded) {
  const valid = loaded.api.validateSource44ProxyArguments;
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
      const value = metadata('directory', [WORK, ROOT, TOOL].includes(filename) ? 0o40700 : 0o40755);
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
  memory.put(ROOT + '/input.json', JSON.stringify(input), inputSHA256);
  for (const [filename, digest] of Object.entries(input.source_closure)) memory.put(filename, 'synthetic source: ' + filename, digest);
  memory.put(pins.STATE, JSON.stringify(documents.state), input.candidate.state_sha256);
  for (const key of ['report', 'completed', 'terminal', 'current', 'proxy'])
    memory.put(pins.PINS[key].path, JSON.stringify(documents[key]), pins.PINS[key].sha256);
  memory.put(input.authority.before_snapshot.path, JSON.stringify(documents.before), input.authority.before_snapshot.sha256);
  for (const [key, name] of [['profile_receipt', 'profileReceipt'], ['profile_report', 'profileReport'],
    ['profile_inspection', 'profileInspection'], ['music_chain', 'musicChain'], ['music_scan_receipt', 'musicScanReceipt']])
    memory.put(pins.FIXTURES[key].path, JSON.stringify(documents[name]), pins.FIXTURES[key].sha256);
  memory.put(input.actor.credentials.path, JSON.stringify(viewerExample()), input.actor.credentials.sha256);
  memory.put(WORK + '/runtime.env', 'SYNTHETIC_RUNTIME=1\n', input.candidate.runtime_sha256);
  memory.put(input.candidate.source + '/backup-source-inputs.json', '{"synthetic":"source44 manifest"}',
    input.candidate.source_manifest_sha256, 0o100644);
  memory.put('/proc/sys/kernel/random/boot_id', input.candidate.process.boot_id + '\n');
  for (const filename of ['/proc/self/ns/net', '/proc/1/ns/net']) memory.links.set(filename, 'net:[synthetic-candidate-namespace]');
  const processStat = process => {
    const fields = Array(20).fill('0'); fields[0] = 'S'; fields[19] = String(process.start_ticks);
    return `${process.pid} (synthetic-owned-process) ${fields.join(' ')}\n`;
  };
  for (const expected of [pins.CANDIDATE_PROCESS, pins.PRIMARY, pins.PROXY_PROCESS]) {
    memory.put(`/proc/${expected.pid}/stat`, processStat(expected));
    memory.links.set(`/proc/${expected.pid}/ns/net`, 'net:[synthetic-candidate-namespace]');
  }
  for (const expected of [pins.CANDIDATE_PROCESS, pins.PRIMARY]) {
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
  for (const [pid, inode] of [[pins.PROXY_PROCESS.pid, 190001], [pins.CANDIDATE.process.pid, 190002]]) {
    memory.listings.set(`/proc/${pid}/fd`, ['3']); memory.links.set(`/proc/${pid}/fd/3`, `socket:[${inode}]`);
  }
  configure({ memory, loaded, input, documents });
  const fixture = await loaded.api.loadLibraryChangedSource44Fixture({ input, inputSHA256 });
  check(memory.handles.size === 0); noEffects(loaded);
  return { memory, loaded, input, documents, fixture };
}

function runtimeCases(test, raw, observeEffects) {
  const scope = value => {
    const { memory, loaded } = value;
    const permitted = new Set([1, loaded.pins.CANDIDATE.process.pid, loaded.pins.PRIMARY.pid, loaded.pins.PROXY_PROCESS.pid]);
    for (const item of memory.trace) {
      const process = /^\/proc\/(\d+)(?:\/|$)/.exec(item.path);
      check(!process || permitted.has(Number(process[1])));
      check(item.path !== WORK + '/browser.json' && !item.path.startsWith('/var/lib/postgresql/'));
    }
    check(memory.handles.size === 0); noEffects(loaded);
  };
  test('memory_loader_rechecks_all_pins_without_reference_or_database_access', async () => {
    const value = await runtimeExample(raw, observeEffects), { fixture, memory } = value;
    const first = await fixture.assertPinned(), second = await fixture.assertPinned();
    check(first.phase === 'pinned' && first.check === 2 && second.check === 3);
    check(fixture.evidence.schema === 27 && fixture.evidence.source === 'source-attempt-44' && Object.isFrozen(fixture.evidence));
    const liveDatabase = { targetName: value.input.target.name, metadataRevision: 5 };
    liveDatabase.targetName = 'Synthetic authorized metadata edit'; liveDatabase.metadataRevision += 1;
    check((await fixture.assertPinned()).check === 4 && liveDatabase.metadataRevision === 6);
    for (const filename of [ROOT + '/input.json', value.loaded.pins.STATE, value.loaded.pins.PINS.report.path,
      value.loaded.pins.PINS.completed.path, value.loaded.pins.PINS.current.path, value.input.actor.credentials.path,
      ...Object.keys(value.input.source_closure)])
      check(memory.trace.filter(item => item.operation === 'open' && item.path === filename).length >= 5);
    scope(value);
  });
  for (const [name, mutate] of [
    ['input_inode_replaced', v => { v.memory.files.get(ROOT + '/input.json').info.ino += 1n; }],
    ['input_content_changed', v => { v.memory.content(ROOT + '/input.json', JSON.stringify({ ...v.input, version: 2 })); }],
    ['authority_inode_replaced', v => { v.memory.files.get(v.loaded.pins.PINS.report.path).info.ino += 1n; }],
    ['authority_bytes_changed', v => { v.memory.content(v.loaded.pins.PINS.current.path, '{"schema":27,"changed":true}'); }],
    ['before_snapshot_replaced', v => { v.memory.files.get(v.input.authority.before_snapshot.path).info.ino += 1n; }],
    ['credential_bytes_changed', v => { const viewer = viewerExample(); viewer.viewer.password = 'f'.repeat(48);
      v.memory.content(v.input.actor.credentials.path, JSON.stringify(viewer)); }],
    ['dependency_inode_replaced', v => { v.memory.files.get(TOOL + '/client-browser-session-proof.mjs').info.ino += 1n; }],
    ['dependency_bytes_changed', v => { v.memory.content(TOOL + '/client-browser-goby-fixture.mjs', 'synthetic changed source'); }],
    ['tool_directory_replaced', v => { v.memory.directories.get(TOOL).ino += 1n; }],
    ['work_directory_replaced', v => { v.memory.directories.get(WORK).ino += 1n; }],
    ['candidate_process_reused', v => { const pid = v.loaded.pins.CANDIDATE.process.pid;
      const bytes = v.memory.files.get(`/proc/${pid}/stat`).bytes.toString('utf8').replace('11104222', '11104223');
      v.memory.content(`/proc/${pid}/stat`, bytes); }],
    ['primary_executable_replaced', v => { v.memory.files.get(`/proc/${v.loaded.pins.PRIMARY.pid}/exe`).info.ino += 1n; }],
    ['proxy_lifetime_changed', v => { const pid = v.loaded.pins.PROXY_PROCESS.pid;
      const bytes = v.memory.files.get(`/proc/${pid}/stat`).bytes.toString('utf8').replace('378464', '378465');
      v.memory.content(`/proc/${pid}/stat`, bytes); }],
    ['candidate_listener_missing', v => { v.memory.content('/proc/self/net/tcp', 'synthetic tcp header\n'); }],
    ['candidate_listener_owner_changed', v => { v.memory.links.set(`/proc/${v.loaded.pins.CANDIDATE.process.pid}/fd/3`, 'socket:[999999]'); }],
    ['candidate_namespace_changed', v => { v.memory.links.set(`/proc/${v.loaded.pins.CANDIDATE.process.pid}/ns/net`, 'net:[foreign]'); }],
    ['candidate_invocation_changed', v => { v.memory.content(`/proc/${v.loaded.pins.CANDIDATE.process.pid}/environ`, 'INVOCATION_ID=foreign\0'); }],
  ]) test('memory_assert_pinned_rejects_' + name, async () => {
    const value = await runtimeExample(raw, observeEffects); mutate(value);
    let rejected = false;
    try { await value.fixture.assertPinned(); } catch (error) { rejected = error?.message === 'library_changed_source44_live_pin_failed'; }
    check(rejected); scope(value);
  });
  for (const [name, mutate] of [
    ['setuid_credentials', v => { v.memory.files.get(v.input.actor.credentials.path).info.mode = 0o104600n; }],
    ['setgid_source_dependency', v => { v.memory.files.get(TOOL + '/client-browser-session-proof.mjs').info.mode = 0o102600n; }],
    ['setuid_candidate_executable', v => { v.memory.files.get(`/proc/${v.loaded.pins.CANDIDATE.process.pid}/exe`).info.mode = 0o104755n; }],
    ['setgid_tool_directory', v => { v.memory.directories.get(TOOL).mode = 0o42700n; }],
  ]) test('memory_loader_rejects_initial_' + name, async () => {
    let value, rejected = false;
    try { await runtimeExample(raw, observeEffects, current => { value = current; mutate(current); }); }
    catch (error) { rejected = /^library_changed_source44_[a-z_]+_failed$/.test(error?.message ?? ''); }
    check(rejected && value); scope(value);
  });
}

export async function runLibraryChangedSource44FixtureGuards(raw) {
  const loaded = core(raw), cases = [], tests = [], effects = { filesystem: 0, http: 0, process: 0, other: 0 };
  const runtimeEffects = [];
  const test = (name, action) => { check(!cases.some(value => value.name === name)); cases.push({ name, action }); };
  inputCases(test, loaded); viewerCases(test, loaded); jsonCases(test, loaded);
  documentCases(test, loaded); proxyCases(test, loaded); runtimeCases(test, raw, value => runtimeEffects.push(value));
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
    client_acceptance: false, synthetic_runtime: true, external_effect_attempts: effects, source_sha256: hash(raw),
    counts: { executed: tests.length, passed: tests.filter(value => value.pass).length, failed: tests.filter(value => !value.pass).length }, tests };
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  try {
    check(process.platform === 'linux' && process.getuid?.() === 0 && process.env.SSH_CONNECTION &&
      process.argv.length === 3 && process.argv[2] === '--pure');
    const report = await runLibraryChangedSource44FixtureGuards(await fs.readFile(LOADER, 'utf8'));
    process.stdout.write(JSON.stringify({ result: report.result, mode: 'pure', counts: report.counts,
      external_effect_attempts: report.external_effect_attempts, failed: report.tests.filter(value => !value.pass).map(value => value.name) }) + '\n');
    if (report.result !== 'passed') process.exitCode = 1;
  } catch { process.stdout.write('{"result":"blocked","mode":"pure"}\n'); process.exitCode = 1; }
}
