/** Bind the independently receipted nonempty profile without changing legacy fixture validators. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { TextDecoder } from 'node:util';

const ROOT = '/opt/goby-test/exec-work-m3e';
const STATE = ROOT + '/client-fixture.json';
const ORIGINAL_ROOT = ROOT + '/client-special-features-fixture-v1';
const CONTINUATION_ROOT = ROOT + '/client-special-features-fixture-continuation-v1';
const PROFILE_ROOT = ROOT + '/client-special-features-protocol-finalization-v1';
const INSPECT_ROOT = ROOT + '/client-special-features-finalization-inspection-v1';
const MEDIA = '/opt/goby-fixtures/client-special-features-m3e-v1/Movies';
const PROFILE_MARKER = 'goby-client-special-features-fixture-v1';
const CONTINUATION_MARKER = 'goby-client-special-features-fixture-continuation-v1';
const FINALIZATION_MARKER = 'goby-client-special-features-protocol-finalization-v1';
const SCAN_JOB = 'd7aa0acaee023dd4c82ea7a303c354ca';
const CONTINUATION_LIBRARY = '57a85c1ca5b6c7ae602c587755250b2f';
const CONTINUATION_ROOT_ID = '604d2c0f5c78919a6ee360cda2048066';
const ORIGIN_PINS = Object.freeze({
  origin_failure: { path: ORIGINAL_ROOT + '/failure.json', sha256: '9ce73d72d9ce002dce06230a73213294903800b3b49b06e9b9aa33ad69eda2c0' },
  origin_state: { path: CONTINUATION_ROOT + '/origin-state.json', sha256: '513d971260e18d24ada666a3ec942391d4679bf2a98c5c852742cc70033fccf1' },
  origin_before_state: { path: ORIGINAL_ROOT + '/before-state.json', sha256: '6d719ab6f3cd6c13ebf9fe3d6a927abaf5040e6e87e84be5d81a57318440cefe' },
  origin_before_snapshot: { path: ORIGINAL_ROOT + '/before-full.json', sha256: 'd65097b9916f99723b670faec5d58a01a5380a31c2a369c19db8fbb067f67376' },
  origin_current_snapshot: { path: ORIGINAL_ROOT + '/failed-current-full.json', sha256: '86cbffbc1b3c0d623ea894aab12843273adcce5c7b097f3b9878dfc276ee8275' },
  origin_library_ack: { path: ORIGINAL_ROOT + '/private/library-ack.json', sha256: '998dffd8122cc7921df1a21092a357881e676950c6b858cd0f7332a3b8ecb94d' },
});
const ORIGIN_SOURCE_PINS = Object.freeze({
  origin_setup_source: ['prepare-client-special-features-fixture.py', '84af95f1d34227dc9c465b73545c6de6939963d44fb3fc2d2df50e7f0f759465'],
  origin_profile_source: ['client-special-features-profile.py', '6bbc0bfd17b1c7eef44028fe34481436d39a3e6645442340189e2da35c382252'],
});
const FINALIZATION_PINS = Object.freeze({
  continuation_failure: { path: CONTINUATION_ROOT + '/failure.json', sha256: '5696a8f7b57f2fec0b2a6bc4d056426f02faaa860ef830c4abc7989e49a54f4d' },
  continuation_state: { path: PROFILE_ROOT + '/continuation-state.json', sha256: '0897f2bec4723d5a69df8b35978bc4be32e7ea91f1f11aebfda4c500c2116e83' },
  continuation_snapshot: { path: CONTINUATION_ROOT + '/failed-current-full.json', sha256: '550b6f83cd804485cf227ee3514fb6dec0af550d61ec5926303ce589047877f8' },
  retained_structure_proof: { path: ROOT + '/client-special-features-protocol-review-01/report.json', sha256: '3a6b4d35b27944335bf9838f64618dc41254a79447b7cb9bceb540be1ca540c0' },
  retained_protocol_proof: { path: ROOT + '/client-special-features-protocol-review-01/protocol-proof.json', sha256: 'db65b1016efed40297ab3014105d4748bd8febc5ba4a3d8284721872c3045c21' },
});
const CONTINUATION_SOURCE_PINS = Object.freeze({
  continuation_setup_source: ['continue-client-special-features-fixture-v2.py', 'f5b448b5715456e0539b79afa84fab0ad53acee00bc6e5fb77051ff49131fca8'],
  continuation_profile_source: ['client-special-features-continuation-profile-v2.py', '71d549fd60193d6813ad97a4cc587460c14d47fc0a96ce74c0367b4acf45019a'],
});
const MARKER = 'goby-m3e-client-acceptance-v1';
const BINARY = '/opt/goby-client-m3e/goby';
const CGROUP = '/system.slice/goby-client-m3e.service';
const PROXY = ROOT + '/dual-proxy-status-02.json';
const JSON_LIMIT = 2 * 1024 * 1024;
const REFERENCE = Object.freeze({ pid: 332054, start_ticks: 357218,
  executable: '/dev/shm/goby-emby-reference/package/opt/emby-server/system/EmbyServer',
  sha256: 'c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2' });
const PINNED_MUSIC = Object.freeze({
  chain_path: ROOT + '/client-music-upgrade-chain-source32.json',
  chain_sha256: 'c235d59a317fe1b95d8d78a51ab32ca556f3ca7872e96bfea51e710e26b0c67a',
  guard_path: ROOT + '/client-music-lineage-source32-01.json',
  guard_sha256: '31c455a5d41d04bc56163785c7cd4dcb70e00dfe060bbacab099d0dfe86a22b3',
  receipt_path: ROOT + '/client-music-scan-v1/receipt.json',
  receipt_sha256: 'e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b',
});
const FILES = Object.freeze({
  positive: ['M3e Special Features Positive (2026)/M3e Special Features Positive (2026).mp4', 'Movie'],
  empty: ['M3e Special Features Empty (2026)/M3e Special Features Empty (2026).mp4', 'Movie'],
  alpha: ['M3e Special Features Positive (2026)/featurettes/Alpha Bonus.mp4', 'Video'],
  deleted: ['M3e Special Features Positive (2026)/deleted scenes/Middle Deleted Scene.mp4', 'Video'],
  zeta: ['M3e Special Features Positive (2026)/featurettes/Zeta Bonus.mp4', 'Video'],
  trailer: ['M3e Special Features Positive (2026)/trailers/Delta Local Trailer.mp4', 'Video'],
});
const ID = /^[0-9a-f]{32}$/, SHA = /^[0-9a-f]{64}$/;
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const digest = value => typeof value === 'string' && SHA.test(value);
const id = value => typeof value === 'string' && ID.test(value);
const hash = value => createHash('sha256').update(value).digest('hex');
const stable = value => Array.isArray(value) ? value.map(stable) : record(value)
  ? Object.fromEntries(Object.keys(value).sort().map(key => [key, stable(value[key])])) : value;
const same = (a, b) => JSON.stringify(stable(a)) === JSON.stringify(stable(b));
const requireThat = value => { if (!value) throw new Error('positive_fixture_binding_mismatch'); };
const processIdentity = value => record(value) && same(Object.keys(value).sort(), ['boot_id', 'pid', 'start_ticks']) &&
  Number.isSafeInteger(value.pid) && value.pid > 1 && Number.isSafeInteger(value.start_ticks) && value.start_ticks > 0 &&
  typeof value.boot_id === 'string' && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(value.boot_id);
const protectedJSONPath = value => typeof value === 'string' && value.startsWith(ROOT + '/') &&
  path.posix.normalize(value) === value && value.endsWith('.json');
function freeze(value) {
  if (value && typeof value === 'object') { for (const child of Object.values(value)) freeze(child); Object.freeze(value); }
  return value;
}

export function validatePositiveItems(receipt) {
  const keys = ['library', 'positive_folder', 'empty_folder', ...Object.keys(FILES)];
  requireThat(record(receipt.items) && record(receipt.item_ids) && same(Object.keys(receipt.items).sort(), [...keys].sort()) &&
    same(Object.keys(receipt.item_ids).sort(), [...keys].sort()) && keys.every(key => id(receipt.item_ids[key]) &&
      receipt.items[key]?.id === receipt.item_ids[key]) && new Set(Object.values(receipt.item_ids)).size === keys.length);
  const library = receipt.library, items = receipt.items;
  requireThat(record(library) && id(library.id) && id(library.root_id) && library.id === receipt.item_ids.library &&
    library.name === 'M3e Client Special Features Movies' && library.collection_type === 'movies' && library.path === MEDIA);
  requireThat(items.library.type === 'CollectionFolder' && items.library.api_type === 'CollectionFolder' &&
    items.library.path === '' && items.library.relative_path === '' && items.library.parent_id === null &&
    items.library.name === library.name && items.library.api_name === library.name && !Object.hasOwn(items.library, 'media_source_id'));
  for (const [key, name] of [['positive_folder', 'M3e Special Features Positive (2026)'], ['empty_folder', 'M3e Special Features Empty (2026)']]) {
    const item = items[key];
    requireThat(item.type === 'Folder' && item.api_type === 'Folder' && item.name === name &&
      item.path === MEDIA + '/' + name && item.relative_path === name && item.parent_id === library.id &&
      !Object.hasOwn(item, 'media_source_id'));
  }
  for (const [key, [relative, type]] of Object.entries(FILES)) {
    const item = items[key], parent = key === 'positive' ? items.positive_folder.id : key === 'empty' ? items.empty_folder.id : items.positive.id;
    requireThat(item.type === type && item.path === MEDIA + '/' + relative && item.relative_path === relative && item.parent_id === parent &&
      typeof item.name === 'string' && item.name.length >= 1 && item.name.length <= 256 &&
      typeof item.api_name === 'string' && item.api_name.length >= 1 && item.api_name.length <= 256 &&
      (type === 'Movie' ? item.api_type === 'Movie' : key === 'trailer' ? ['Video', 'Trailer'].includes(item.api_type) : item.api_type === 'Video') &&
      typeof item.media_source_id === 'string' && /^[A-Za-z0-9_-]{1,256}$/.test(item.media_source_id));
  }
  return true;
}

export function validateContinuation(receipt, report, inspection) {
  const chain = receipt.continuation;
  const keys = [...Object.keys(ORIGIN_PINS), ...Object.keys(ORIGIN_SOURCE_PINS), 'origin_evidence', 'marker', 'library_id', 'root_id',
    'creation_admin_session_id', 'new_scan_admin_session_id', 'new_viewer_session_id', 'scan_job_id',
    'creation_requests', 'continuation_library_creates', 'continuation_scan_dispatches', 'original_failure_preserved',
    'creation_admin_revoked', 'continuation_sessions_revoked'];
  requireThat(receipt.profile_version === 3 && record(chain) && same(Object.keys(chain).sort(), keys.sort()) &&
    chain.marker === CONTINUATION_MARKER && same(report.continuation, chain) && same(inspection.continuation, chain) &&
    chain.library_id === CONTINUATION_LIBRARY && chain.library_id === receipt.library.id &&
    chain.root_id === CONTINUATION_ROOT_ID && chain.root_id === receipt.library.root_id &&
    chain.creation_requests === 5 && chain.continuation_library_creates === 0 && chain.continuation_scan_dispatches === 1 &&
    ['original_failure_preserved', 'creation_admin_revoked', 'continuation_sessions_revoked'].every(key => chain[key] === true));
  const ids = ['creation_admin_session_id', 'new_scan_admin_session_id', 'new_viewer_session_id', 'scan_job_id'].map(key => chain[key]);
  requireThat(ids.every(id) && new Set(ids).size === ids.length && chain.scan_job_id === receipt.job_id);
  for (const [key, expected] of Object.entries(ORIGIN_PINS)) requireThat(same(chain[key], expected));
  for (const [key, [name, sha]] of Object.entries(ORIGIN_SOURCE_PINS)) {
    requireThat(record(chain[key]) && same(Object.keys(chain[key]).sort(), ['path', 'sha256']) &&
      chain[key].path.startsWith(ROOT + '/') && path.posix.normalize(chain[key].path) === chain[key].path &&
      path.posix.basename(chain[key].path) === name && chain[key].sha256 === sha);
  }
  requireThat(record(chain.origin_evidence) && same(Object.keys(chain.origin_evidence).sort(), ['path', 'sha256']) &&
    chain.origin_evidence.path === CONTINUATION_ROOT + '/origin-evidence.json' && digest(chain.origin_evidence.sha256));
  return true;
}

export function validateFinalization(receipt, report, inspection) {
  const chain = receipt.finalization;
  const keys = [...Object.keys(FINALIZATION_PINS), ...Object.keys(CONTINUATION_SOURCE_PINS), 'continuation_evidence',
    'marker', 'scan_job_id', 'viewer_user_id', 'new_viewer_session_id', 'new_device_id', 'request_count',
    'full_resource_count', 'range_resource_count', 'library_creates', 'scan_dispatches',
    'retained_failures_preserved', 'new_viewer_revoked', 'parent_projection'];
  requireThat(receipt.profile_version === 3 && record(chain) && same(Object.keys(chain).sort(), keys.sort()) &&
    chain.marker === FINALIZATION_MARKER && same(report.finalization, chain) && same(inspection.finalization, chain) &&
    chain.scan_job_id === SCAN_JOB && chain.scan_job_id === receipt.job_id &&
    chain.viewer_user_id === receipt.users.av_user_id && id(chain.new_viewer_session_id) &&
    !['creation_admin_session_id', 'new_scan_admin_session_id', 'new_viewer_session_id', 'scan_job_id']
      .some(key => receipt.continuation[key] === chain.new_viewer_session_id) &&
    Number.isSafeInteger(chain.new_device_id) && chain.new_device_id > 0 && chain.request_count === 11 &&
    chain.full_resource_count === 4 && chain.range_resource_count === 4 && chain.library_creates === 0 && chain.scan_dispatches === 0 &&
    chain.retained_failures_preserved === true && chain.new_viewer_revoked === true &&
    same(chain.parent_projection, { special_feature_count_present: false, local_trailer_count: 1 }));
  for (const [key, expected] of Object.entries(FINALIZATION_PINS)) requireThat(same(chain[key], expected));
  for (const [key, [name, sha]] of Object.entries(CONTINUATION_SOURCE_PINS)) {
    requireThat(record(chain[key]) && same(Object.keys(chain[key]).sort(), ['path', 'sha256']) &&
      typeof chain[key].path === 'string' && chain[key].path.startsWith(ROOT + '/') &&
      path.posix.normalize(chain[key].path) === chain[key].path && path.posix.basename(chain[key].path) === name && chain[key].sha256 === sha);
  }
  requireThat(record(chain.continuation_evidence) && same(Object.keys(chain.continuation_evidence).sort(), ['path', 'sha256']) &&
    chain.continuation_evidence.path === PROFILE_ROOT + '/continuation-evidence.json' && digest(chain.continuation_evidence.sha256));
  const retained = receipt.retained_scan_proof;
  requireThat(record(retained) && retained.aggregate_session_count === 3 && retained.aggregate_audit_count === 9 &&
    retained.creation_audit_count === 3 && retained.continuation_audit_count === 6 && retained.table_count === 35 &&
    retained.old_item_count === 13 && retained.new_item_count === 9 && same(retained.item_ids, receipt.item_ids) &&
    retained.creation_admin_session_id === receipt.continuation.creation_admin_session_id &&
    retained.native_session_id === receipt.continuation.new_scan_admin_session_id &&
    retained.viewer_session_id === receipt.continuation.new_viewer_session_id &&
    Number.isSafeInteger(retained.new_device_id) && retained.new_device_id > 0 && retained.new_device_id !== chain.new_device_id &&
    retained.creation_admin_revoked === true && retained.old_rows_preserved === true && retained.expected_increment_preserved === true);
  const expectedProof = { ...retained, aggregate_session_count: 4, aggregate_audit_count: 11,
    finalization_session_count: 1, finalization_audit_count: 2, finalizer_session_id: chain.new_viewer_session_id,
    finalizer_device_id: chain.new_device_id, all_scanned_rows_preserved: true };
  requireThat(same(receipt.proof, expectedProof) && same(report.proof, expectedProof) && record(receipt.authentication) &&
    same(Object.keys(receipt.authentication), ['viewer']) && receipt.authentication.viewer?.login_status === 200 &&
    receipt.authentication.viewer.logout_status === 204 && receipt.authentication.viewer.exact_status === 401 &&
    receipt.authentication.viewer.session_id === chain.new_viewer_session_id && digest(receipt.authentication.viewer.token_sha256));
  return true;
}

export function validateRetainedContinuation(documents, receipt, currentState) {
  const failure = documents.continuation_failure, state = documents.continuation_state;
  requireThat(failure?.marker === CONTINUATION_MARKER && failure.result === 'retained_for_review' && failure.phase === 'scan_complete' &&
    failure.library_id === CONTINUATION_LIBRARY && failure.root_id === CONTINUATION_ROOT_ID && failure.job_id === SCAN_JOB &&
    failure.retry_permitted === false && same(failure.journal_failures, []) && same(failure.origin_failure, ORIGIN_PINS.origin_failure) &&
    ['admin', 'viewer'].every(role => failure.authentication?.[role]?.login_status === 200 &&
      failure.authentication[role].logout_status === 204 && failure.authentication[role].exact_status === 401) &&
    failure.authentication.viewer.session_id === receipt.continuation.new_viewer_session_id &&
    state?.phase === 'continuing_special_features_fixture' && state.stage === 'scan_complete' &&
    state.special_features_profile?.library_id === CONTINUATION_LIBRARY && state.special_features_profile.root_id === CONTINUATION_ROOT_ID &&
    state.special_features_profile.job_id === SCAN_JOB && state.special_features_profile.evidence_directory === CONTINUATION_ROOT);
  const baseState = value => Object.fromEntries(Object.entries(value).filter(([key]) =>
    !['phase', 'stage', 'special_features_profile'].includes(key)));
  requireThat(record(currentState) && same(baseState(state), baseState(currentState)) &&
    same(state.process, receipt.candidate.process));
  return true;
}

export function validateRetainedFailure(documents, receipt, currentState) {
  const failure = documents.origin_failure, state = documents.origin_state, before = documents.origin_before_state;
  requireThat(failure?.marker === PROFILE_MARKER && failure.result === 'retained_for_review' && failure.phase === 'library_acknowledged' &&
    failure.retry_permitted === false && failure.old_rows_preserved === true && failure.library_id === CONTINUATION_LIBRARY &&
    failure.root_id === null && failure.job_id === null && state?.phase === 'preparing_special_features_fixture' &&
    state.stage === 'library_acknowledged' && state.special_features_profile?.library_id === CONTINUATION_LIBRARY &&
    before?.phase === 'ready' && before.stage === 'complete' && same(state.process, before.process) &&
    same(state.process, receipt.candidate.process));
  const baseState = value => Object.fromEntries(Object.entries(value).filter(([key]) =>
    !['phase', 'stage', 'special_features_profile'].includes(key)));
  requireThat(record(currentState) && same(baseState(before), baseState(state)) &&
    same(baseState(before), baseState(currentState)));
  const auth = failure.authentication;
  requireThat(auth?.admin?.login_status === 200 && auth.admin.logout_status === 204 && auth.admin.exact_status === 401 &&
    ['login_status', 'logout_status', 'exact_status'].every(key => auth.viewer?.[key] === null));
  const ack = documents.origin_library_ack;
  requireThat(ack?.Id === CONTINUATION_LIBRARY && ack.CollectionType === 'movies' && same(ack.Paths, [MEDIA]));
  return true;
}

/** The caller pins three independent receipts; their claims remain setup-time evidence. */
export function validatePositiveBundle(bundle, expected) {
  const { state, receipt, report, inspection, upgrade, extension, musicGuard } = bundle;
  requireThat(record(expected) && ['candidateSHA256', 'sourceManifestSHA256', 'receiptSHA256', 'reportSHA256', 'inspectSHA256', 'stateSHA256']
    .every(key => digest(expected[key])) && expected.source === ROOT + '/source-attempt-32' &&
    expected.sourceManifestSHA256 === 'a65070315ce3b31dd70143267cbf774a838bc0e5b1ed99f34c3c759d392daa65');
  requireThat(record(state) && state.marker === MARKER && state.work === ROOT && state.schema === 27 &&
    state.phase === 'ready' && state.stage === 'complete' && state.binary_sha256 === expected.candidateSHA256 &&
    processIdentity(state.process) && record(state.special_features_profile) &&
    same(state.special_features_profile, { marker: PROFILE_MARKER, phase: 'complete', receipt_path: PROFILE_ROOT + '/completed.json',
      receipt_sha256: expected.receiptSHA256, library_id: receipt.library?.id, root_id: receipt.library?.root_id }) &&
    !['start_pending', 'credentials_pending', 'binary_pending', 'unit_pending', 'directory_pending'].some(key => state[key]));
  requireThat(id(state.server_id) && typeof state.tag === 'string' && state.tag.length > 0 &&
    record(state.work_identity) && ['device', 'inode'].every(key =>
      /^[1-9]\d*$/.test(String(state.work_identity[key] ?? ''))) &&
    same(state.cluster, { data: '/var/lib/postgresql/goby-workspace-v1/data', port: 15432, system_identifier: '7684040109719526738' }) &&
    ['database_oid', 'role_oid'].every(key => Number.isSafeInteger(state[key]) && state[key] > 0) &&
    record(state.libraries) && same(Object.keys(state.libraries).sort(), ['movies', 'music', 'tv']) &&
    Object.values(state.libraries).every(library => record(library) && id(library.id) &&
      library.creation_pending === false && library.scan?.Status === 'completed') &&
    new Set(Object.values(state.libraries).map(library => library.id)).size === 3);
  requireThat(receipt.marker === PROFILE_MARKER && receipt.phase === 'complete' && receipt.schema === 27 && receipt.profile_version === 3 &&
    receipt.server_id === state.server_id && receipt.fixture_tag === state.tag &&
    receipt.source_state_sha256 === FINALIZATION_PINS.continuation_state.sha256 &&
    record(receipt.candidate) && same(Object.keys(receipt.candidate).sort(), ['binary_sha256', 'process', 'runtime_sha256']) &&
    receipt.candidate.binary_sha256 === expected.candidateSHA256 &&
    same(receipt.candidate.process, state.process) && receipt.candidate.runtime_sha256 === state.runtime_sha256 &&
    digest(state.runtime_sha256) && receipt.source?.path === expected.source &&
    receipt.source.manifest_sha256 === expected.sourceManifestSHA256 && digest(receipt.source.full_report_sha256) &&
    protectedJSONPath(receipt.source.full_report_path));
  validatePositiveItems(receipt);
  requireThat(same(receipt.users, { admin_id: state.admin_id, viewer_id: state.viewer_id, av_user_id: state.added_viewer?.user_id }) &&
    Object.values(receipt.users).every(id) && new Set(Object.values(receipt.users)).size === 3 &&
    record(receipt.aliases) && same(Object.keys(receipt.aliases).sort(), ['av', 'primary']) &&
    same(receipt.aliases.primary, { path: ROOT + '/browser.json', sha256: state.browser_sha256, account_key: 'viewer' }) &&
    same(receipt.aliases.av, { path: ROOT + '/goby-av-browser.json', sha256: state.added_viewer.browser_sha256, account_key: 'viewer' }));
  requireThat(same(receipt.ordinary_access, { primary: { EnableAllFolders: true, EnableMediaPlayback: true },
    av: { EnableAllFolders: true, EnableMediaPlayback: true } }));
  requireThat(record(upgrade) && same(state.upgrade, upgrade) && upgrade.phase === 'complete' && upgrade.from_schema === 26 && upgrade.to_schema === 27 &&
    upgrade.to_sha256 === expected.candidateSHA256 && processIdentity(upgrade.new_process) &&
    receipt.upgrade?.completed_path === ROOT + '/client-upgrade-aaff5430e8e7fc52b1850fabe2097d89/completed.json' &&
    receipt.upgrade.completed_sha256 === '66c2b1f13113c5969887e963641e7ba04743f33da609e748a648117bf2edf26b' &&
    same(receipt.upgrade.old_process, upgrade.old_process) && same(receipt.upgrade.new_process, upgrade.new_process) &&
    upgrade.schema_artifacts?.source === expected.source && upgrade.schema_artifacts.source_manifest_sha256 === expected.sourceManifestSHA256);
  requireThat(extension?.marker === 'goby-client-special-features-root-extension-v1' && extension.phase === 'complete' &&
    extension.schema === 27 && extension.added_root === MEDIA && extension.binary_sha256 === expected.candidateSHA256 &&
    same(extension.old_process, upgrade.new_process) && same(extension.new_process, state.process) &&
    !same(extension.old_process, extension.new_process) && extension.old_process.boot_id === extension.new_process.boot_id &&
    extension.new_process.start_ticks > extension.old_process.start_ticks &&
    receipt.extension?.completed_path === ROOT + '/client-special-features-root-extension-v1/completed.json' &&
    same(receipt.extension.old_process, extension.old_process) && same(receipt.extension.new_process, extension.new_process) &&
    receipt.extension.old_runtime_sha256 === extension.old_runtime_sha256 && receipt.extension.new_runtime_sha256 === extension.new_runtime_sha256 &&
    extension.new_runtime_sha256 === state.runtime_sha256 && extension.old_runtime_sha256 !== extension.new_runtime_sha256 &&
    state.media_root_extension?.phase === 'complete' && state.media_root_extension.receipt_path === receipt.extension.completed_path &&
    state.media_root_extension.receipt_sha256 === receipt.extension.completed_sha256 &&
    ['old_database_rows_and_sequences_preserved', 'other_runtime_bytes_preserved', 'credentials_recovery_and_all_media_preserved']
      .every(key => extension[key] === true) && extension.http_requests === 0 && extension.library_creates === 0 && extension.scan_dispatches === 0);
  requireThat(report?.marker === 'goby-client-special-features-setup-report-v1' && report.result === 'passed' && report.phase === 'complete' &&
    report.state_sha256 === expected.stateSHA256 && report.receipt_path === PROFILE_ROOT + '/completed.json' &&
    report.receipt_sha256 === expected.receiptSHA256 && same(report.candidate, receipt.candidate));
  requireThat(inspection?.marker === 'goby-client-special-features-finalization-inspection-v1' && inspection.result === 'passed' &&
    inspection.phase === 'complete' && inspection.schema === 27 && inspection.profile_marker === PROFILE_MARKER &&
    inspection.state_sha256 === expected.stateSHA256 && inspection.receipt_path === PROFILE_ROOT + '/completed.json' &&
    inspection.receipt_sha256 === expected.receiptSHA256 && same(inspection.process, state.process) &&
    inspection.binary_sha256 === expected.candidateSHA256 && inspection.runtime_sha256 === state.runtime_sha256 &&
    inspection.inspection?.table_count === 35 && inspection.inspection.item_count === 22 && inspection.inspection.library_count === 4 &&
    digest(inspection.inspection.database_sha256) && digest(inspection.current_snapshot_sha256) &&
    protectedJSONPath(inspection.current_snapshot_path) && path.posix.dirname(inspection.current_snapshot_path) === INSPECT_ROOT &&
    record(inspection.inspection) && inspection.inspection.current_snapshot_path === inspection.current_snapshot_path &&
    inspection.inspection.current_snapshot_sha256 === inspection.current_snapshot_sha256);
  requireThat(record(inspection.verification) && same(Object.keys(inspection.verification).sort(),
    ['complete_schema27', 'exact_nonempty_profile', 'setup_old_rows_preserved', 'current_matches_completed',
      'credentials_recovery_media_preserved', 'new_setup_sessions_revoked'].sort()) &&
    Object.values(inspection.verification).every(value => value === true) &&
    inspection.profile?.receipt_path === PROFILE_ROOT + '/completed.json' && inspection.profile.receipt_sha256 === expected.receiptSHA256 &&
    inspection.profile.library_id === receipt.library.id && inspection.profile.root_id === receipt.library.root_id &&
    same(inspection.profile.item_ids, receipt.item_ids) && inspection.profile.resource_count === 4 && inspection.profile.reserved_path_count === 3);
  for (const key of ['setup_source', 'profile_source']) requireThat(record(report[key]) &&
    same(Object.keys(report[key]).sort(), ['path', 'sha256']) && same(report[key], receipt[key]) && same(report[key], inspection[key]) &&
    typeof report[key].path === 'string' && report[key].path.startsWith(ROOT + '/') && path.posix.normalize(report[key].path) === report[key].path &&
    report[key].path.endsWith('.py') && digest(report[key].sha256));
  for (const key of ['source', 'upgrade', 'extension']) requireThat(same(inspection[key], receipt[key]));
  for (const key of ['before_snapshot', 'after_snapshot', 'after_ledger']) requireThat(record(receipt[key]) &&
    protectedJSONPath(receipt[key].path) && path.posix.dirname(receipt[key].path) === PROFILE_ROOT && digest(receipt[key].sha256));
  requireThat(receipt.after_ledger.path === receipt.after_snapshot.path && receipt.after_ledger.sha256 === receipt.after_snapshot.sha256 &&
    musicGuard?.result === 'passed' && musicGuard.mode === 'chain' && musicGuard.harness_guards_only === true && musicGuard.client_acceptance === false &&
    same(musicGuard.counts, { executed: 6, passed: 6, failed: 0 }) && musicGuard.source_files_unchanged === true &&
    musicGuard.chain_file_unchanged === true && musicGuard.selected_sha256?.candidate === expected.candidateSHA256 &&
    musicGuard.selected_sha256.upgrade_chain === PINNED_MUSIC.chain_sha256 && musicGuard.selected_sha256.music_scan_receipt === PINNED_MUSIC.receipt_sha256);
  validateContinuation(receipt, report, inspection);
  validateFinalization(receipt, report, inspection);
  return true;
}

function unchanged(before, after) {
  return ['dev', 'ino', 'mode', 'uid', 'gid', 'nlink', 'size', 'mtimeNs', 'ctimeNs'].every(key => before[key] === after[key]);
}
async function directory(filename, expected) {
  requireThat(await fs.realpath(filename) === filename);
  const info = await fs.lstat(filename, { bigint: true });
  requireThat(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0n && info.gid === 0n && (info.mode & 0o777n) === 0o700n);
  if (expected) requireThat(String(expected.device) === String(info.dev) && String(expected.inode) === String(info.ino));
  return { device: String(info.dev), inode: String(info.ino) };
}
async function protectedFile(filename, expectedSHA, json = true, maximum = JSON_LIMIT, modes = [0o600n]) {
  requireThat(typeof filename === 'string' && filename.startsWith(ROOT + '/') && path.posix.normalize(filename) === filename &&
    await fs.realpath(filename) === filename && (expectedSHA === undefined || digest(expectedSHA)));
  const before = await fs.lstat(filename, { bigint: true });
  requireThat(before.isFile() && !before.isSymbolicLink() && before.uid === 0n && before.gid === 0n && before.nlink === 1n &&
    modes.includes(before.mode & 0o777n) && before.size > 0n && before.size <= BigInt(maximum));
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  const bytes = Buffer.alloc(json ? maximum + 1 : 65536), chunks = [], sha = createHash('sha256');
  try {
    requireThat(unchanged(before, await handle.stat({ bigint: true })));
    let size = 0;
    for (;;) {
      const read = await handle.read(bytes, 0, bytes.length, size);
      if (!read.bytesRead) break;
      size += read.bytesRead; requireThat(size <= maximum); sha.update(bytes.subarray(0, read.bytesRead));
      if (json) chunks.push(Buffer.from(bytes.subarray(0, read.bytesRead)));
    }
    const digestValue = sha.digest('hex');
    requireThat(size === Number(before.size) && (expectedSHA === undefined || expectedSHA === digestValue) &&
      unchanged(before, await handle.stat({ bigint: true })) && unchanged(before, await fs.lstat(filename, { bigint: true })));
    let value;
    if (json) {
      const raw = Buffer.concat(chunks);
      try { value = strictJSON(new TextDecoder('utf-8', { fatal: true }).decode(raw), true); }
      finally { raw.fill(0); }
    }
    return { path: filename, sha256: digestValue, value, modes, maximum };
  } finally { bytes.fill(0); for (const chunk of chunks) chunk.fill(0); await handle.close(); }
}

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

/** Read files and Linux ownership only. A fresh run-specific database ledger remains separate. */
export async function loadSpecialFeaturesFixture(options = {}) {
  let phase = 'environment';
  try {
    requireThat(process.platform === 'linux' && process.getuid?.() === 0 &&
      ['candidateSHA256', 'sourceManifestSHA256', 'receiptSHA256', 'reportSHA256', 'inspectSHA256', 'musicChainSHA256']
        .every(key => digest(options[key])) && options.musicChainPath === PINNED_MUSIC.chain_path &&
      options.musicChainSHA256 === PINNED_MUSIC.chain_sha256 && options.musicScanReceiptSHA256 === PINNED_MUSIC.receipt_sha256);
    const roots = new Map();
    for (const name of [ROOT, ORIGINAL_ROOT, CONTINUATION_ROOT, PROFILE_ROOT, INSPECT_ROOT]) roots.set(name, await directory(name));
    const pins = [];
    async function read(filename, expected, json = true, maximum = JSON_LIMIT, modes = [0o600n]) {
      const file = await protectedFile(filename, expected, json, maximum, modes);
      pins.push(file); return file;
    }
    phase = 'profile_documents';
    const stateFile = await read(STATE);
    const receiptFile = await read(PROFILE_ROOT + '/completed.json', options.receiptSHA256);
    const reportFile = await read(PROFILE_ROOT + '/report.json', options.reportSHA256);
    const inspectFile = await read(INSPECT_ROOT + '/report.json', options.inspectSHA256);
    const state = stateFile.value, receipt = receiptFile.value, report = reportFile.value, inspection = inspectFile.value;
    requireThat(record(receipt) && record(receipt.upgrade) && record(receipt.extension));
    const upgradeFile = await read(receipt.upgrade.completed_path, receipt.upgrade.completed_sha256);
    const extensionFile = await read(receipt.extension.completed_path, receipt.extension.completed_sha256);
    const upgradeReport = await read(receipt.upgrade.report_path, receipt.upgrade.report_sha256);
    const extensionReport = await read(receipt.extension.report_path, receipt.extension.report_sha256);
    const musicGuard = await read(PINNED_MUSIC.guard_path, PINNED_MUSIC.guard_sha256);
    const musicChain = await read(PINNED_MUSIC.chain_path, PINNED_MUSIC.chain_sha256);
    const musicReceipt = await read(PINNED_MUSIC.receipt_path, PINNED_MUSIC.receipt_sha256);
    const expected = { ...options, stateSHA256: stateFile.sha256 };
    validatePositiveBundle({ state, receipt, report, inspection, upgrade: upgradeFile.value,
      extension: extensionFile.value, musicGuard: musicGuard.value }, expected);
    phase = 'continuation_origins';
    const origins = {};
    for (const [key, origin] of Object.entries(ORIGIN_PINS)) {
      const snapshot = key.endsWith('_snapshot');
      const file = await read(origin.path, origin.sha256, !snapshot, snapshot ? 64 * 1024 * 1024 : JSON_LIMIT);
      if (!snapshot) origins[key] = file.value;
    }
    for (const key of Object.keys(ORIGIN_SOURCE_PINS)) {
      const origin = receipt.continuation[key];
      await read(origin.path, origin.sha256, false, JSON_LIMIT, [0o600n, 0o644n, 0o700n, 0o755n]);
    }
    await read(receipt.continuation.origin_evidence.path, receipt.continuation.origin_evidence.sha256, false);
    validateRetainedFailure(origins, receipt, state);
    phase = 'retained_scan';
    const continuation = {};
    for (const [key, source] of Object.entries(FINALIZATION_PINS)) {
      const json = ['continuation_failure', 'continuation_state'].includes(key);
      const file = await read(source.path, source.sha256, json, key === 'continuation_snapshot' ? 64 * 1024 * 1024 : JSON_LIMIT);
      if (json) continuation[key] = file.value;
    }
    for (const key of Object.keys(CONTINUATION_SOURCE_PINS)) {
      const source = receipt.finalization[key];
      await read(source.path, source.sha256, false, JSON_LIMIT, [0o600n, 0o644n, 0o700n, 0o755n]);
    }
    await read(receipt.finalization.continuation_evidence.path, receipt.finalization.continuation_evidence.sha256, false);
    validateRetainedContinuation(continuation, receipt, state);
    await directory(ROOT, state.work_identity);
    requireThat(upgradeReport.value.result === 'ready' && upgradeReport.value.schema === 27 &&
      same(upgradeReport.value.process, upgradeFile.value.new_process) && upgradeReport.value.binary_sha256 === options.candidateSHA256 &&
      extensionReport.value.result === 'passed' && extensionReport.value.phase === 'complete' &&
      same(extensionReport.value.completed, extensionFile.value) && Array.isArray(musicChain.value) && musicChain.value.length === 5 &&
      musicChain.value[4].completedSHA256 === receipt.upgrade.completed_sha256 &&
      musicChain.value[4].reportSHA256 === receipt.upgrade.report_sha256 &&
      musicReceipt.value.schema === 25 && musicReceipt.value.phase === 'complete' && musicReceipt.value.result === 'passed' &&
      musicReceipt.value.logout_status === 204 && musicReceipt.value.token_readback_status === 401);
    phase = 'profile_sources';
    for (const key of ['setup_source', 'profile_source']) {
      await read(report[key].path, report[key].sha256, false, JSON_LIMIT, [0o600n, 0o644n, 0o700n, 0o755n]);
    }
    for (const source of [receipt.before_snapshot, receipt.after_snapshot,
      { path: inspection.current_snapshot_path, sha256: inspection.current_snapshot_sha256 }]) {
      await read(source.path, source.sha256, false, 64 * 1024 * 1024);
    }
    const fullReport = await read(receipt.source.full_report_path, receipt.source.full_report_sha256);
    const full = fullReport.value;
    requireThat(full.status === 'passed' && full.mode === 'full' && full.schema === 27 &&
      full.source === options.source && full.source_manifest_sha256 === options.sourceManifestSHA256 &&
      full.binary?.sha256 === options.candidateSHA256 && full.tests?.failures === 0 && full.tests.skips === 0 &&
      full.unit_exit === 0 && record(full.cleanup) && ['unit_terminal', 'hba_restored_exactly', 'goby_backup_m3e_source_removed',
        'goby_backup_m3e_target_removed', 'preexisting_catalog_unchanged', 'receipt_saved'].every(key => full.cleanup[key] === true) &&
      Object.values(full.cleanup).every(value => value === true));
    await read(options.source + '/backup-source-inputs.json', options.sourceManifestSHA256, false, JSON_LIMIT, [0o644n]);
    await read(ROOT + '/runtime.env', state.runtime_sha256, false, 65536);
    const primaryAlias = await read(receipt.aliases.primary.path, receipt.aliases.primary.sha256, false);
    const avAlias = await read(receipt.aliases.av.path, receipt.aliases.av.sha256, false);
    const addedReceipt = await read(state.added_viewer.receipt_path, state.added_viewer.receipt_sha256);
    requireThat(addedReceipt.value.user_id === receipt.users.av_user_id && addedReceipt.value.is_administrator === false &&
      addedReceipt.value.is_disabled === false && addedReceipt.value.admin_session_revoked === true &&
      addedReceipt.value.browser_sha256 === avAlias.sha256 && state.browser_sha256 === primaryAlias.sha256);
    phase = 'proxy_binding';
    const proxyFile = await read(PROXY), proxy = proxyFile.value;
    requireThat(proxy.reference_pid === REFERENCE.pid && proxy.reference_start_ticks === String(REFERENCE.start_ticks) &&
      proxy.reference_executable_sha256 === REFERENCE.sha256 && proxy.reference_only === false &&
      same([...proxy.listen].sort(), ['127.0.0.1:18196', '127.0.0.1:18197']) &&
      Number.isSafeInteger(proxy.pid) && proxy.pid > 1 && typeof proxy.start_ticks === 'string' && /^[1-9]\d*$/.test(proxy.start_ticks) &&
      Number.isSafeInteger(Number(proxy.start_ticks)) && Number.isSafeInteger(proxy.reference_namespace_inode));
    const pinned = freeze({ process: { ...state.process }, binary_sha256: state.binary_sha256,
      proxy_process: { pid: proxy.pid, start_ticks: Number(proxy.start_ticks), boot_id: state.process.boot_id },
      reference_namespace_inode: proxy.reference_namespace_inode });
    const profile = freeze({ receipt_path: receiptFile.path, receipt_sha256: receiptFile.sha256,
      report_path: reportFile.path, report_sha256: reportFile.sha256, inspect_path: inspectFile.path,
      inspect_sha256: inspectFile.sha256, state_sha256: stateFile.sha256 });
    const music = freeze({ album: { id: '002c2ea2c77769ae9b0b84b6e788998b', name: 'M3e Synthetic Album',
      parentId: state.libraries.music.id, pathOmitted: true, type: 'MusicAlbum' },
    tracks: { mp3: { id: 'e2da060a0addfafde7f92c291c413f6d', name: 'M3e MP3', type: 'Audio',
      path: '/opt/goby-fixtures/client-m3e/Music/M3e Client Audio.mp3', parentId: '002c2ea2c77769ae9b0b84b6e788998b' },
    flac: { id: '4d2ec259d9ae5224f46aa6f56ca2b186', name: 'M3e FLAC', type: 'Audio',
      path: '/opt/goby-fixtures/client-m3e/Music/M3e Client Audio.flac', parentId: '002c2ea2c77769ae9b0b84b6e788998b' } } });
    const evidence = freeze({ schema: 27, process: { ...pinned.process }, binary_sha256: pinned.binary_sha256,
      schema_binding: { schema: 27, source: options.source, source_manifest_sha256: options.sourceManifestSHA256 },
      profile, fixture_state_sha256: stateFile.sha256, browser_alias_sha256: avAlias.sha256,
      receipt_sha256: addedReceipt.sha256, account_id: receipt.users.av_user_id,
      upgrade: { ...receipt.upgrade }, extension: { ...receipt.extension },
      continuation: { marker: CONTINUATION_MARKER, original_failure_preserved: true,
        origin_failure_sha256: ORIGIN_PINS.origin_failure.sha256, origin_state_sha256: ORIGIN_PINS.origin_state.sha256,
        origin_evidence_sha256: receipt.continuation.origin_evidence.sha256 },
      finalization: { marker: FINALIZATION_MARKER, profile_version: 3, retained_failures_preserved: true,
        continuation_failure_sha256: FINALIZATION_PINS.continuation_failure.sha256,
        continuation_state_sha256: FINALIZATION_PINS.continuation_state.sha256,
        continuation_evidence_sha256: receipt.finalization.continuation_evidence.sha256,
        request_count: 11, full_resource_count: 4, range_resource_count: 4, library_creates: 0, scan_dispatches: 0 },
      music_lineage: { ...PINNED_MUSIC, candidate_process: { ...upgradeFile.value.new_process } },
      libraries: Object.fromEntries(Object.entries(state.libraries).map(([key, value]) => [key, value.id])),
      boundary: 'Receipted nonempty setup and repeated file/process pins; subsequent database changes require the separate fresh UI ledger' });
    let checks = 0;
    async function assertPinned() {
      try {
        for (const [name, identity] of roots) await directory(name, identity);
        for (const file of pins) await protectedFile(file.path, file.sha256, false, file.maximum, file.modes);
        const boot = (await procText('/proc/sys/kernel/random/boot_id')).trim();
        requireThat(boot === pinned.process.boot_id && await fs.readlink('/proc/self/ns/net') === await fs.readlink('/proc/1/ns/net'));
        const processes = [pinned.process, pinned.proxy_process, { pid: REFERENCE.pid, start_ticks: REFERENCE.start_ticks, boot_id: boot }];
        for (const expectedProcess of processes) requireThat(same(await currentProcess(expectedProcess.pid, boot), expectedProcess));
        requireThat((await fs.stat('/proc/' + pinned.process.pid)).uid === 995 &&
          (await procText('/proc/' + pinned.process.pid + '/cgroup')).trim().split('\n')
            .some(line => /^\d+:[^:]*:/.test(line) && line.split(':')[2] === CGROUP) &&
          await procText('/proc/' + pinned.process.pid + '/cmdline') === BINARY + '\0' &&
          (await fs.stat('/proc/' + REFERENCE.pid + '/ns/net')).ino === pinned.reference_namespace_inode);
        await hashExecutable(pinned.process.pid, BINARY, pinned.binary_sha256, true);
        await hashExecutable(REFERENCE.pid, REFERENCE.executable, REFERENCE.sha256, false);
        await pinProxyArguments(pinned.proxy_process.pid);
        await pinListeners([[18198, pinned.process.pid], [18196, pinned.proxy_process.pid], [18197, pinned.proxy_process.pid]]);
        for (const expectedProcess of processes) requireThat(same(await currentProcess(expectedProcess.pid, boot), expectedProcess));
        requireThat((await procText('/proc/sys/kernel/random/boot_id')).trim() === boot);
        for (const file of pins.slice(0, 4)) await protectedFile(file.path, file.sha256, false, file.maximum, file.modes);
        return { phase: 'pinned', check: ++checks, fixture_state_sha256: stateFile.sha256 };
      } catch { throw new Error('positive_fixture_live_pin_failed'); }
    }
    phase = 'initial_pin'; await assertPinned();
    return Object.freeze({ origin: 'http://127.0.0.1:18196', accountId: receipt.users.av_user_id, serverId: state.server_id,
      library: freeze({ ...receipt.library }), items: freeze({ ...receipt.items }), music, evidence, assertPinned });
  } catch { throw Object.assign(new Error('positive_fixture_' + phase + '_failed'), { phase }); }
}
