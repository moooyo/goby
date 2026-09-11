#!/usr/bin/env node
/** Pure receipt and lineage guards. No fixture, process, HTTP or database access is permitted. */
import fs from 'node:fs/promises';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { TextDecoder } from 'node:util';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const ROOT = '/opt/goby-test/exec-work-m3e';
const ORIGINAL = ROOT + '/client-special-features-fixture-v1';
const CONTINUATION = ROOT + '/client-special-features-fixture-continuation-v1';
const PROFILE = ROOT + '/client-special-features-protocol-finalization-v1';
const INSPECT = ROOT + '/client-special-features-finalization-inspection-v1';
const SCAN_JOB = 'd7aa0acaee023dd4c82ea7a303c354ca';
const MEDIA = '/opt/goby-fixtures/client-special-features-m3e-v1/Movies';
const SELF = fileURLToPath(import.meta.url);
const LOADER = fileURLToPath(new URL('./client-browser-special-features-fixture.mjs', import.meta.url));
const hash = value => createHash('sha256').update(value).digest('hex');
const syntheticHash = value => hash('synthetic-positive-fixture:' + value);
const clone = value => JSON.parse(JSON.stringify(value));
function requireThat(value) { if (!value) throw new Error('positive_fixture_guard_failed'); }

function core(raw) {
  let source = raw;
  for (const statement of ["import fs from 'node:fs/promises';", "import { constants } from 'node:fs';",
    "import path from 'node:path';", "import { createHash } from 'node:crypto';", "import { TextDecoder } from 'node:util';"]) {
    requireThat(source.split(statement).length === 2); source = source.replace(statement, '');
  }
  source = source.replace(/^export (?=(?:async )?function )/gm, '');
  requireThat(!/^\s*(?:import|export)\b/m.test(source) && !/\bimport\s*\(/.test(source));
  let effects = 0;
  const denied = new Proxy({}, { get() { effects += 1; throw new Error('positive_fixture_external_effect'); } });
  const sandbox = { fs: denied, constants: denied, path: { posix: path.posix }, createHash, TextDecoder, Buffer,
    process: Object.freeze({ platform: 'linux', getuid: () => 0 }), fetch: denied,
    console: denied, setTimeout: denied, setInterval: denied };
  const context = vm.createContext(sandbox, { codeGeneration: { strings: false, wasm: false } });
  new vm.Script(source + '\nglobalThis.result = {validatePositiveItems,validatePositiveBundle,validateContinuation,validateFinalization,validateRetainedFailure,validateRetainedContinuation,loadSpecialFeaturesFixture};')
    .runInContext(context, { timeout: 1000 });
  requireThat(effects === 0);
  return { api: sandbox.result, get effects() { return effects; } };
}

function example() {
  const source = ROOT + '/source-attempt-32';
  const manifest = 'a65070315ce3b31dd70143267cbf774a838bc0e5b1ed99f34c3c759d392daa65';
  const expected = { candidateSHA256: syntheticHash('binary'), source, sourceManifestSHA256: manifest,
    receiptSHA256: syntheticHash('receipt'), reportSHA256: syntheticHash('report'), inspectSHA256: syntheticHash('inspection'),
    stateSHA256: syntheticHash('state') };
  const process = { pid: 9001, start_ticks: 900001, boot_id: '00000000-0000-0000-0000-000000000001' };
  const oldProcess = { ...process, pid: 8001, start_ticks: 800001 };
  const upgrade = { phase: 'complete', from_schema: 26, to_schema: 27, to_sha256: expected.candidateSHA256,
    old_process: { ...process, pid: 7001, start_ticks: 700001 }, new_process: clone(oldProcess),
    schema_artifacts: { source, source_manifest_sha256: manifest } };
  const extension = { marker: 'goby-client-special-features-root-extension-v1', phase: 'complete', schema: 27,
    added_root: MEDIA, binary_sha256: expected.candidateSHA256, old_process: clone(oldProcess), new_process: clone(process),
    old_runtime_sha256: syntheticHash('old-runtime'), new_runtime_sha256: syntheticHash('new-runtime'),
    old_database_rows_and_sequences_preserved: true, other_runtime_bytes_preserved: true,
    credentials_recovery_and_all_media_preserved: true, http_requests: 0, library_creates: 0, scan_dispatches: 0 };
  const item_ids = Object.fromEntries(['library', 'positive_folder', 'empty_folder', 'positive', 'empty', 'alpha', 'deleted', 'zeta', 'trailer']
    .map((key, index) => [key, String(index + 1).repeat(32)]));
  item_ids.library = '57a85c1ca5b6c7ae602c587755250b2f';
  const names = { positive: 'M3e Special Features Positive (2026)', empty: 'M3e Special Features Empty (2026)' };
  const library = { id: item_ids.library, root_id: '604d2c0f5c78919a6ee360cda2048066', name: 'M3e Client Special Features Movies', collection_type: 'movies', path: MEDIA };
  const items = { library: { id: item_ids.library, type: 'CollectionFolder', api_type: 'CollectionFolder', name: library.name,
    api_name: library.name, path: '', relative_path: '', parent_id: null } };
  for (const key of ['positive', 'empty']) {
    items[key + '_folder'] = { id: item_ids[key + '_folder'], type: 'Folder', api_type: 'Folder', name: names[key],
      api_name: names[key], path: MEDIA + '/' + names[key], relative_path: names[key], parent_id: library.id };
    items[key] = { id: item_ids[key], type: 'Movie', api_type: 'Movie', name: names[key], api_name: names[key],
      path: MEDIA + '/' + names[key] + '/' + names[key] + '.mp4', relative_path: names[key] + '/' + names[key] + '.mp4',
      parent_id: item_ids[key + '_folder'], media_source_id: 'source_' + item_ids[key] };
  }
  for (const [key, directory, name] of [['alpha', 'featurettes', 'Alpha Bonus'], ['deleted', 'deleted scenes', 'Middle Deleted Scene'],
    ['zeta', 'featurettes', 'Zeta Bonus'], ['trailer', 'trailers', 'Delta Local Trailer']]) {
    const relative = names.positive + '/' + directory + '/' + name + '.mp4';
    items[key] = { id: item_ids[key], type: 'Video', api_type: key === 'trailer' ? 'Trailer' : 'Video',
      name, api_name: name, relative_path: relative, path: MEDIA + '/' + relative,
      parent_id: item_ids.positive, media_source_id: 'source_' + item_ids[key] };
  }
  const users = { admin_id: 'a'.repeat(32), viewer_id: 'b'.repeat(32), av_user_id: 'c'.repeat(32) };
  const receipt = { marker: 'goby-client-special-features-fixture-v1', phase: 'complete', schema: 27, profile_version: 3,
    library, item_ids, items, users, job_id: SCAN_JOB,
    ordinary_access: { primary: { EnableAllFolders: true, EnableMediaPlayback: true }, av: { EnableAllFolders: true, EnableMediaPlayback: true } },
    candidate: { binary_sha256: expected.candidateSHA256, process: clone(process), runtime_sha256: extension.new_runtime_sha256 },
    source: { path: source, manifest_sha256: manifest, full_report_path: ROOT + '/client-backup-run-20990101_000000_000000000001/report.json',
      full_report_sha256: syntheticHash('full-report') },
    upgrade: { completed_path: ROOT + '/client-upgrade-aaff5430e8e7fc52b1850fabe2097d89/completed.json',
      completed_sha256: '66c2b1f13113c5969887e963641e7ba04743f33da609e748a648117bf2edf26b',
      old_process: clone(upgrade.old_process), new_process: clone(upgrade.new_process) },
    extension: { completed_path: ROOT + '/client-special-features-root-extension-v1/completed.json',
      completed_sha256: syntheticHash('extension'), old_process: clone(oldProcess), new_process: clone(process),
      old_runtime_sha256: extension.old_runtime_sha256, new_runtime_sha256: extension.new_runtime_sha256 },
    aliases: { primary: { path: ROOT + '/browser.json', sha256: syntheticHash('primary'), account_key: 'viewer' },
      av: { path: ROOT + '/goby-av-browser.json', sha256: syntheticHash('av'), account_key: 'viewer' } },
    before_snapshot: { path: PROFILE + '/before-full.json', sha256: syntheticHash('before') },
    after_snapshot: { path: PROFILE + '/after-full.json', sha256: syntheticHash('after') },
    after_ledger: { path: PROFILE + '/after-full.json', sha256: syntheticHash('after') } };
  const state = { marker: 'goby-m3e-client-acceptance-v1', work: ROOT, schema: 27, phase: 'ready', stage: 'complete',
    server_id: '1'.repeat(32), tag: 'synthetic-positive-fixture', work_identity: { device: 2049, inode: 900 },
    cluster: { data: '/var/lib/postgresql/goby-workspace-v1/data', port: 15432, system_identifier: '7684040109719526738' },
    database_oid: 100, role_oid: 101,
    libraries: Object.fromEntries(['movies', 'music', 'tv'].map((key, index) => [key,
      { id: String(index + 1).repeat(32), creation_pending: false, scan: { Status: 'completed' } }])),
    binary_sha256: expected.candidateSHA256, runtime_sha256: extension.new_runtime_sha256, process: clone(process),
    admin_id: users.admin_id, viewer_id: users.viewer_id, browser_sha256: receipt.aliases.primary.sha256,
    added_viewer: { user_id: users.av_user_id, browser_sha256: receipt.aliases.av.sha256 },
    upgrade: clone(upgrade), media_root_extension: { phase: 'complete', receipt_path: receipt.extension.completed_path, receipt_sha256: receipt.extension.completed_sha256 },
    special_features_profile: { marker: receipt.marker, phase: 'complete', receipt_path: PROFILE + '/completed.json',
      receipt_sha256: expected.receiptSHA256, library_id: library.id, root_id: library.root_id } };
  const setup_source = { path: ROOT + '/positive-input/setup.py', sha256: syntheticHash('setup-source') };
  const profile_source = { path: ROOT + '/positive-input/client-special-features-profile.py', sha256: syntheticHash('profile-source') };
  receipt.server_id = state.server_id; receipt.fixture_tag = state.tag;
  receipt.source_state_sha256 = '0897f2bec4723d5a69df8b35978bc4be32e7ea91f1f11aebfda4c500c2116e83';
  receipt.setup_source = clone(setup_source); receipt.profile_source = clone(profile_source);
  const report = { marker: 'goby-client-special-features-setup-report-v1', result: 'passed', phase: 'complete',
    state_sha256: expected.stateSHA256, receipt_path: PROFILE + '/completed.json', receipt_sha256: expected.receiptSHA256,
    candidate: clone(receipt.candidate), setup_source, profile_source };
  const inspection = { marker: 'goby-client-special-features-finalization-inspection-v1', result: 'passed', phase: 'complete', schema: 27,
    profile_marker: receipt.marker, state_sha256: expected.stateSHA256, receipt_path: PROFILE + '/completed.json',
    receipt_sha256: expected.receiptSHA256, process: clone(process), binary_sha256: expected.candidateSHA256,
    runtime_sha256: extension.new_runtime_sha256, setup_source: clone(setup_source), profile_source: clone(profile_source),
    current_snapshot_path: INSPECT + '/current-full.json', current_snapshot_sha256: syntheticHash('current-full'),
    verification: Object.fromEntries(['complete_schema27', 'exact_nonempty_profile', 'setup_old_rows_preserved', 'current_matches_completed',
      'credentials_recovery_media_preserved', 'new_setup_sessions_revoked'].map(key => [key, true])),
    profile: { receipt_path: PROFILE + '/completed.json', receipt_sha256: expected.receiptSHA256, library_id: library.id, root_id: library.root_id,
      item_ids: clone(item_ids), resource_count: 4, reserved_path_count: 3 } };
  inspection.inspection = { table_count: 35, item_count: 22, library_count: 4, database_sha256: syntheticHash('database'),
    current_snapshot_path: inspection.current_snapshot_path, current_snapshot_sha256: inspection.current_snapshot_sha256 };
  for (const key of ['source', 'upgrade', 'extension']) inspection[key] = clone(receipt[key]);
  receipt.continuation = { marker: 'goby-client-special-features-fixture-continuation-v1',
    origin_failure: { path: ORIGINAL + '/failure.json', sha256: '9ce73d72d9ce002dce06230a73213294903800b3b49b06e9b9aa33ad69eda2c0' },
    origin_state: { path: CONTINUATION + '/origin-state.json', sha256: '513d971260e18d24ada666a3ec942391d4679bf2a98c5c852742cc70033fccf1' },
    origin_before_state: { path: ORIGINAL + '/before-state.json', sha256: '6d719ab6f3cd6c13ebf9fe3d6a927abaf5040e6e87e84be5d81a57318440cefe' },
    origin_before_snapshot: { path: ORIGINAL + '/before-full.json', sha256: 'd65097b9916f99723b670faec5d58a01a5380a31c2a369c19db8fbb067f67376' },
    origin_current_snapshot: { path: ORIGINAL + '/failed-current-full.json', sha256: '86cbffbc1b3c0d623ea894aab12843273adcce5c7b097f3b9878dfc276ee8275' },
    origin_library_ack: { path: ORIGINAL + '/private/library-ack.json', sha256: '998dffd8122cc7921df1a21092a357881e676950c6b858cd0f7332a3b8ecb94d' },
    origin_setup_source: { path: ROOT + '/original-input/prepare-client-special-features-fixture.py', sha256: '84af95f1d34227dc9c465b73545c6de6939963d44fb3fc2d2df50e7f0f759465' },
    origin_profile_source: { path: ROOT + '/original-input/client-special-features-profile.py', sha256: '6bbc0bfd17b1c7eef44028fe34481436d39a3e6645442340189e2da35c382252' },
    origin_evidence: { path: CONTINUATION + '/origin-evidence.json', sha256: syntheticHash('origin-evidence') },
    library_id: library.id, root_id: library.root_id, creation_admin_session_id: 'e'.repeat(32),
    new_scan_admin_session_id: 'f'.repeat(32), new_viewer_session_id: '0'.repeat(32), scan_job_id: receipt.job_id,
    creation_requests: 5, continuation_library_creates: 0, continuation_scan_dispatches: 1,
    original_failure_preserved: true, creation_admin_revoked: true, continuation_sessions_revoked: true };
  report.continuation = clone(receipt.continuation); inspection.continuation = clone(receipt.continuation);
  receipt.finalization = { marker: 'goby-client-special-features-protocol-finalization-v1',
    continuation_failure: { path: CONTINUATION + '/failure.json', sha256: '5696a8f7b57f2fec0b2a6bc4d056426f02faaa860ef830c4abc7989e49a54f4d' },
    continuation_state: { path: PROFILE + '/continuation-state.json', sha256: '0897f2bec4723d5a69df8b35978bc4be32e7ea91f1f11aebfda4c500c2116e83' },
    continuation_snapshot: { path: CONTINUATION + '/failed-current-full.json', sha256: '550b6f83cd804485cf227ee3514fb6dec0af550d61ec5926303ce589047877f8' },
    continuation_evidence: { path: PROFILE + '/continuation-evidence.json', sha256: syntheticHash('continuation-evidence') },
    continuation_setup_source: { path: ROOT + '/continuation-input/continue-client-special-features-fixture-v2.py',
      sha256: 'f5b448b5715456e0539b79afa84fab0ad53acee00bc6e5fb77051ff49131fca8' },
    continuation_profile_source: { path: ROOT + '/continuation-input/client-special-features-continuation-profile-v2.py',
      sha256: '71d549fd60193d6813ad97a4cc587460c14d47fc0a96ce74c0367b4acf45019a' },
    retained_structure_proof: { path: ROOT + '/client-special-features-protocol-review-01/report.json',
      sha256: '3a6b4d35b27944335bf9838f64618dc41254a79447b7cb9bceb540be1ca540c0' },
    retained_protocol_proof: { path: ROOT + '/client-special-features-protocol-review-01/protocol-proof.json',
      sha256: 'db65b1016efed40297ab3014105d4748bd8febc5ba4a3d8284721872c3045c21' },
    scan_job_id: SCAN_JOB, viewer_user_id: users.av_user_id, new_viewer_session_id: '9'.repeat(32), new_device_id: 1001,
    request_count: 11, full_resource_count: 4, range_resource_count: 4, library_creates: 0, scan_dispatches: 0,
    retained_failures_preserved: true, new_viewer_revoked: true,
    parent_projection: { special_feature_count_present: false, local_trailer_count: 1 } };
  report.finalization = clone(receipt.finalization); inspection.finalization = clone(receipt.finalization);
  receipt.retained_scan_proof = { item_ids: clone(item_ids), table_count: 35, old_item_count: 13, new_item_count: 9,
    creation_admin_session_id: receipt.continuation.creation_admin_session_id, creation_admin_revoked: true, creation_audit_count: 3,
    native_session_id: receipt.continuation.new_scan_admin_session_id, viewer_session_id: receipt.continuation.new_viewer_session_id,
    new_device_id: 1000, continuation_audit_count: 6, aggregate_session_count: 3, aggregate_audit_count: 9,
    old_rows_preserved: true, expected_increment_preserved: true };
  receipt.proof = { ...clone(receipt.retained_scan_proof), aggregate_session_count: 4, aggregate_audit_count: 11,
    finalization_session_count: 1, finalization_audit_count: 2, finalizer_session_id: receipt.finalization.new_viewer_session_id,
    finalizer_device_id: receipt.finalization.new_device_id, all_scanned_rows_preserved: true };
  report.proof = clone(receipt.proof);
  receipt.authentication = { viewer: { login_status: 200, logout_status: 204, exact_status: 401,
    session_id: receipt.finalization.new_viewer_session_id, token_sha256: syntheticHash('finalizer-token-fingerprint') } };
  const musicGuard = { result: 'passed', mode: 'chain', harness_guards_only: true, client_acceptance: false,
    counts: { executed: 6, passed: 6, failed: 0 }, source_files_unchanged: true, chain_file_unchanged: true,
    selected_sha256: { candidate: expected.candidateSHA256, upgrade_chain: 'c235d59a317fe1b95d8d78a51ab32ca556f3ca7872e96bfea51e710e26b0c67a',
      music_scan_receipt: 'e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b' } };
  return { bundle: { state, receipt, report, inspection, upgrade, extension, musicGuard }, expected };
}

export async function runPositiveFixtureGuards(raw) {
  const loaded = core(raw), tests = [], cases = [];
  const test = (name, action) => cases.push({ name, action });
  const valid = value => loaded.api.validatePositiveBundle(value.bundle, value.expected);
  const reject = value => {
    let rejected = false;
    try { valid(value); } catch (error) { rejected = error?.message === 'positive_fixture_binding_mismatch'; }
    requireThat(rejected && loaded.effects === 0);
  };
  test('complete_nonempty_receipt_chain', () => requireThat(valid(example()) === true));
  for (const [name, mutate] of [
    ['missing_profile_pin', v => { delete v.expected.receiptSHA256; }],
    ['wrong_schema', v => { v.bundle.state.schema = 26; }],
    ['string_schema', v => { v.bundle.state.schema = '27'; }],
    ['pending_profile', v => { v.bundle.state.special_features_profile.phase = 'pending'; }],
    ['wrong_candidate', v => { v.bundle.receipt.candidate.binary_sha256 = syntheticHash('foreign'); }],
    ['foreign_candidate_field', v => { v.bundle.receipt.candidate.foreign = true; }],
    ['wrong_cluster', v => { v.bundle.state.cluster.port = 5432; }],
    ['invalid_database_identity', v => { v.bundle.state.database_oid = true; }],
    ['wrong_server', v => { v.bundle.receipt.server_id = '2'.repeat(32); }],
    ['extra_original_library', v => { v.bundle.state.libraries.extra = clone(v.bundle.state.libraries.movies); }],
    ['wrong_current_process', v => { v.bundle.receipt.candidate.process.pid += 1; }],
    ['rewritten_upgrade_process', v => { v.bundle.state.upgrade.new_process = clone(v.bundle.state.process); }],
    ['broken_extension_chain', v => { v.bundle.extension.old_process.pid += 1; }],
    ['same_extension_process', v => { v.bundle.extension.new_process = clone(v.bundle.extension.old_process); }],
    ['changed_runtime', v => { v.bundle.extension.new_runtime_sha256 = syntheticHash('foreign-runtime'); }],
    ['unpreserved_old_rows', v => { v.bundle.extension.old_database_rows_and_sequences_preserved = false; }],
    ['unexpected_extension_scan', v => { v.bundle.extension.scan_dispatches = 1; }],
    ['wrong_state_receipt', v => { v.bundle.state.special_features_profile.receipt_sha256 = syntheticHash('foreign'); }],
    ['wrong_inspection_state', v => { v.bundle.inspection.state_sha256 = syntheticHash('foreign'); }],
    ['wrong_table_count', v => { v.bundle.inspection.inspection.table_count = 33; }],
    ['wrong_item_count', v => { v.bundle.inspection.inspection.item_count = 21; }],
    ['wrong_resource_count', v => { v.bundle.inspection.profile.resource_count = 3; }],
    ['wrong_reservation_count', v => { v.bundle.inspection.profile.reserved_path_count = 4; }],
    ['snapshot_alias_conflict', v => { v.bundle.inspection.inspection.current_snapshot_sha256 = syntheticHash('foreign'); }],
    ['snapshot_path_escape', v => { v.bundle.inspection.current_snapshot_path = '/tmp/current-full.json'; }],
    ['changed_setup_source', v => { v.bundle.inspection.setup_source.sha256 = syntheticHash('foreign'); }],
    ['changed_alias', v => { v.bundle.receipt.aliases.primary.account_key = 'admin'; }],
    ['changed_actor', v => { v.bundle.receipt.users.av_user_id = v.bundle.receipt.users.admin_id; }],
    ['old_music_proof_failed', v => { v.bundle.musicGuard.counts.failed = 1; }],
    ['old_music_proof_relabelled', v => { v.bundle.musicGuard.client_acceptance = true; }],
    ['ledger_snapshot_conflict', v => { v.bundle.receipt.after_ledger.sha256 = syntheticHash('foreign'); }],
  ]) test(name, () => { const value = example(); mutate(value); reject(value); });
  for (const key of Object.keys(example().bundle.inspection.verification)) test('missing_verification_' + key, () => {
    const value = example(); delete value.bundle.inspection.verification[key]; reject(value);
  });
  for (const [name, mutate] of [
    ['missing_extra', v => { delete v.bundle.receipt.items.alpha; }],
    ['unowned_extra', v => { v.bundle.receipt.item_ids.foreign = 'e'.repeat(32); }],
    ['duplicate_id', v => { v.bundle.receipt.item_ids.zeta = v.bundle.receipt.item_ids.alpha; }],
    ['foreign_parent', v => { v.bundle.receipt.items.alpha.parent_id = v.bundle.receipt.library.id; }],
    ['escaped_path', v => { v.bundle.receipt.items.alpha.path = '/tmp/Alpha Bonus.mp4'; }],
    ['wrong_kind', v => { v.bundle.receipt.items.alpha.type = 'Audio'; }],
    ['wrong_api_kind', v => { v.bundle.receipt.items.alpha.api_type = 'Movie'; }],
    ['missing_received_source', v => { delete v.bundle.receipt.items.positive.media_source_id; }],
    ['bad_library_root', v => { v.bundle.receipt.library.path = '/opt/goby-fixtures'; }],
  ]) test(name, () => { const value = example(); mutate(value); reject(value); });
  test('loader_rejects_missing_inputs_before_filesystem_access', async () => {
    let rejected = false;
    try { await loaded.api.loadSpecialFeaturesFixture({}); } catch (error) { rejected = error?.phase === 'environment'; }
    requireThat(rejected && loaded.effects === 0);
  });
  test('failed_profiles_cannot_be_relabelled_as_completed_version1_or_2', () => {
    for (const version of [undefined, 1, 2, '3', true]) {
      const value = example(); value.bundle.receipt.profile_version = version; reject(value);
    }
    for (const oldRoot of [ORIGINAL, CONTINUATION]) {
      const value = example(); value.bundle.state.special_features_profile.receipt_path = oldRoot + '/completed.json'; reject(value);
    }
  });
  for (const [name, mutate] of [
    ['missing_origin', c => { delete c.origin_failure; }],
    ['changed_origin_hash', c => { c.origin_failure.sha256 = syntheticHash('changed-failure'); }],
    ['changed_origin_path', c => { c.origin_state.path = ORIGINAL + '/origin-state.json'; }],
    ['unowned_library', c => { c.library_id = 'a'.repeat(32); }],
    ['unowned_root', c => { c.root_id = 'b'.repeat(32); }],
    ['duplicate_actor', c => { c.new_viewer_session_id = c.new_scan_admin_session_id; }],
    ['wrong_job', c => { c.scan_job_id = 'a'.repeat(32); }],
    ['another_create', c => { c.continuation_library_creates = 1; }],
    ['another_scan', c => { c.continuation_scan_dispatches = 2; }],
    ['changed_creation_budget', c => { c.creation_requests = 6; }],
    ['failure_not_retained', c => { c.original_failure_preserved = false; }],
    ['creation_not_revoked', c => { c.creation_admin_revoked = false; }],
    ['continuation_not_revoked', c => { c.continuation_sessions_revoked = false; }],
    ['origin_source_changed', c => { c.origin_profile_source.sha256 = syntheticHash('changed-source'); }],
    ['origin_source_relabelled', c => { c.origin_setup_source.path = ROOT + '/foreign.py'; }],
    ['inventory_path_changed', c => { c.origin_evidence.path = ORIGINAL + '/origin-evidence.json'; }],
    ['unknown_chain_field', c => { c.unowned = true; }],
  ]) test('continuation_' + name, () => {
    const value = example(); mutate(value.bundle.receipt.continuation);
    value.bundle.report.continuation = clone(value.bundle.receipt.continuation);
    value.bundle.inspection.continuation = clone(value.bundle.receipt.continuation); reject(value);
  });
  test('continuation_three_receipt_copies_must_agree', () => {
    for (const key of ['report', 'inspection']) {
      const value = example(); value.bundle[key].continuation.scan_job_id = 'a'.repeat(32); reject(value);
    }
  });
  test('new_profile_source_is_explicit_without_a_fabricated_future_pin', () => {
    const value = example();
    for (const document of [value.bundle.receipt, value.bundle.report, value.bundle.inspection]) {
      document.setup_source = { path: ROOT + '/new-input/finalize-client-special-features-fixture.py',
        sha256: syntheticHash('new-reviewed-operator') };
      document.profile_source = { path: ROOT + '/new-input/client-special-features-finalization-profile.py',
        sha256: syntheticHash('new-reviewed-source') };
    }
    requireThat(valid(value) === true);
  });
  test('ordinary_access_claims_are_explicit_for_both_actors', () => {
    const value = example(); value.bundle.receipt.ordinary_access.primary.EnableAllFolders = false; reject(value);
  });
  for (const [name, mutate] of [
    ['missing_failure', c => { delete c.continuation_failure; }],
    ['changed_failure', c => { c.continuation_failure.sha256 = syntheticHash('foreign-failure'); }],
    ['changed_state', c => { c.continuation_state.sha256 = syntheticHash('foreign-state'); }],
    ['relabelled_state_path', c => { c.continuation_state.path = CONTINUATION + '/continuation-state.json'; }],
    ['changed_snapshot', c => { c.continuation_snapshot.sha256 = syntheticHash('foreign-snapshot'); }],
    ['changed_structure', c => { c.retained_structure_proof.sha256 = syntheticHash('foreign-structure'); }],
    ['changed_protocol', c => { c.retained_protocol_proof.sha256 = syntheticHash('foreign-protocol'); }],
    ['changed_source', c => { c.continuation_setup_source.sha256 = syntheticHash('foreign-source'); }],
    ['relabelled_profile_source', c => { c.continuation_profile_source.path = ROOT + '/foreign.py'; }],
    ['unowned_inventory', c => { c.continuation_evidence.path = CONTINUATION + '/continuation-evidence.json'; }],
    ['another_job', c => { c.scan_job_id = 'd'.repeat(32); }],
    ['wrong_actor', c => { c.viewer_user_id = 'b'.repeat(32); }],
    ['reused_viewer_session', c => { c.new_viewer_session_id = '0'.repeat(32); }],
    ['invalid_device', c => { c.new_device_id = true; }],
    ['wrong_request_count', c => { c.request_count = 12; }],
    ['missing_full', c => { c.full_resource_count = 3; }],
    ['missing_range', c => { c.range_resource_count = 3; }],
    ['another_create', c => { c.library_creates = 1; }],
    ['another_scan', c => { c.scan_dispatches = 1; }],
    ['failure_not_retained', c => { c.retained_failures_preserved = false; }],
    ['viewer_not_revoked', c => { c.new_viewer_revoked = false; }],
    ['invented_parent_sf_count', c => { c.parent_projection.special_feature_count_present = true; }],
    ['missing_parent_trailer_count', c => { c.parent_projection.local_trailer_count = 0; }],
    ['unowned_field', c => { c.unowned = true; }],
  ]) test('finalization_' + name, () => {
    const value = example(); mutate(value.bundle.receipt.finalization);
    value.bundle.report.finalization = clone(value.bundle.receipt.finalization);
    value.bundle.inspection.finalization = clone(value.bundle.receipt.finalization); reject(value);
  });
  test('finalization_three_copies_and_new_source_state_must_agree', () => {
    for (const key of ['report', 'inspection']) {
      const value = example(); value.bundle[key].finalization.request_count = 10; reject(value);
    }
    const value = example(); value.bundle.receipt.source_state_sha256 = value.bundle.receipt.continuation.origin_state.sha256; reject(value);
  });
  test('four_stage_auth_and_audit_counts_keep_original_scan_actor_ids', () => {
    for (const mutate of [r => { r.retained_scan_proof.aggregate_session_count = 4; }, r => { r.retained_scan_proof.aggregate_audit_count = 11; },
      r => { r.retained_scan_proof.native_session_id = r.finalization.new_viewer_session_id; },
      r => { r.retained_scan_proof.viewer_session_id = r.finalization.new_viewer_session_id; },
      r => { r.retained_scan_proof.new_device_id = r.finalization.new_device_id; },
      r => { r.proof.aggregate_session_count = 3; }, r => { r.proof.aggregate_audit_count = 9; },
      r => { r.proof.finalization_session_count = 2; }, r => { r.proof.all_scanned_rows_preserved = false; },
      r => { r.authentication.admin = clone(r.authentication.viewer); }, r => { r.authentication.viewer.exact_status = 200; },
      r => { r.authentication.viewer.session_id = r.continuation.new_viewer_session_id; }]) {
      const value = example(); mutate(value.bundle.receipt); value.bundle.report.proof = clone(value.bundle.receipt.proof); reject(value);
    }
  });
  test('retained_completed_scan_and_closed_actors_remain_distinct_from_finalizer', () => {
    const value = example();
    const documents = {
      continuation_failure: { marker: 'goby-client-special-features-fixture-continuation-v1', result: 'retained_for_review', phase: 'scan_complete',
        library_id: value.bundle.receipt.library.id, root_id: value.bundle.receipt.library.root_id, job_id: SCAN_JOB,
        retry_permitted: false, journal_failures: [], origin_failure: clone(value.bundle.receipt.continuation.origin_failure),
        authentication: { admin: { login_status: 200, logout_status: 204, exact_status: 401 },
          viewer: { login_status: 200, logout_status: 204, exact_status: 401, session_id: value.bundle.receipt.continuation.new_viewer_session_id } } },
      continuation_state: { ...clone(value.bundle.state), phase: 'continuing_special_features_fixture', stage: 'scan_complete',
        special_features_profile: { library_id: value.bundle.receipt.library.id, root_id: value.bundle.receipt.library.root_id,
          job_id: SCAN_JOB, evidence_directory: CONTINUATION } },
    };
    requireThat(loaded.api.validateRetainedContinuation(documents, value.bundle.receipt, value.bundle.state) === true);
    for (const mutate of [d => { d.continuation_failure.result = 'passed'; }, d => { d.continuation_failure.job_id = 'd'.repeat(32); },
      d => { d.continuation_failure.authentication.admin.logout_status = 200; }, d => { d.continuation_failure.authentication.viewer.exact_status = 200; },
      d => { d.continuation_failure.authentication.viewer.session_id = value.bundle.receipt.finalization.new_viewer_session_id; },
      d => { d.continuation_failure.journal_failures = ['incomplete']; }, d => { d.continuation_state.phase = 'ready'; },
      d => { d.continuation_state.special_features_profile.evidence_directory = PROFILE; },
      d => { d.continuation_state.process.pid += 1; }, d => { delete d.continuation_state.libraries.music; }]) {
      const bad = clone(documents); mutate(bad); let refused = false;
      try { loaded.api.validateRetainedContinuation(bad, value.bundle.receipt, value.bundle.state); }
      catch (error) { refused = error?.message === 'positive_fixture_binding_mismatch'; }
      requireThat(refused);
    }
  });
  test('retained_creation_failure_ack_and_cleanup_remain_distinct', () => {
    const value = example();
    const documents = {
      origin_failure: { marker: 'goby-client-special-features-fixture-v1', result: 'retained_for_review', phase: 'library_acknowledged',
        retry_permitted: false, old_rows_preserved: true, library_id: value.bundle.receipt.library.id, root_id: null, job_id: null,
        authentication: { admin: { login_status: 200, logout_status: 204, exact_status: 401 },
          viewer: { login_status: null, logout_status: null, exact_status: null } } },
      origin_state: { ...clone(value.bundle.state), phase: 'preparing_special_features_fixture', stage: 'library_acknowledged',
        special_features_profile: { library_id: value.bundle.receipt.library.id } },
      origin_before_state: { ...clone(value.bundle.state), phase: 'ready', stage: 'complete' },
      origin_library_ack: { Id: value.bundle.receipt.library.id, CollectionType: 'movies', Paths: [MEDIA] },
    };
    delete documents.origin_before_state.special_features_profile;
    requireThat(loaded.api.validateRetainedFailure(documents, value.bundle.receipt, value.bundle.state) === true);
    for (const mutate of [d => { d.origin_failure.result = 'passed'; }, d => { d.origin_failure.job_id = 'd'.repeat(32); },
      d => { d.origin_failure.root_id = value.bundle.receipt.library.root_id; }, d => { d.origin_failure.authentication.admin.logout_status = 200; },
      d => { d.origin_failure.authentication.viewer.login_status = 200; }, d => { d.origin_state.phase = 'ready'; },
      d => { d.origin_before_state.process.pid += 1; }, d => { d.origin_library_ack.Paths = ['/opt/goby-fixtures']; },
      d => { d.origin_state.database_oid += 1; }, d => { d.origin_before_state.added_viewer.user_id = '2'.repeat(32); },
      d => { d.origin_state.unowned = true; }, d => { delete d.origin_before_state.libraries.music; }]) {
      const bad = clone(documents); mutate(bad); let refused = false;
      try { loaded.api.validateRetainedFailure(bad, value.bundle.receipt, value.bundle.state); }
      catch (error) { refused = error?.message === 'positive_fixture_binding_mismatch'; }
      requireThat(refused);
    }
    const changedCurrent = clone(value.bundle.state); changedCurrent.runtime_sha256 = syntheticHash('changed-current-runtime');
    let refused = false;
    try { loaded.api.validateRetainedFailure(documents, value.bundle.receipt, changedCurrent); }
    catch (error) { refused = error?.message === 'positive_fixture_binding_mismatch'; }
    requireThat(refused);
  });
  for (const { name, action } of cases) {
    let pass = false;
    try { await action(); requireThat(loaded.effects === 0); pass = true; } catch { /* Never release raw inputs or exception text. */ }
    tests.push({ name, pass });
  }
  return { result: tests.every(test => test.pass) ? 'passed' : 'blocked', mode: 'pure', harness_guards_only: true,
    client_acceptance: false, source_sha256: hash(raw), counts: { executed: tests.length,
      passed: tests.filter(test => test.pass).length, failed: tests.filter(test => !test.pass).length }, tests };
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  try {
    requireThat(process.platform === 'linux' && process.getuid?.() === 0 && process.env.SSH_CONNECTION &&
      process.argv.length === 3 && process.argv[2] === '--pure');
    const report = await runPositiveFixtureGuards(await fs.readFile(LOADER, 'utf8'));
    process.stdout.write(JSON.stringify({ result: report.result, mode: 'pure', counts: report.counts,
      failed: report.tests.filter(test => !test.pass).map(test => test.name) }) + '\n');
    if (report.result !== 'passed') process.exitCode = 1;
  } catch { process.stdout.write('{"result":"blocked","mode":"pure"}\n'); process.exitCode = 1; }
}
