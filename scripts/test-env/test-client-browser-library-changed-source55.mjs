#!/usr/bin/env node
/** Pure source, protocol, IPC and evidence guards; no browser, HTTP or database. */
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { libraryChangedCatalogRequest as coreCatalogRequest } from './client-browser-cross-user.mjs';
import { parseSource55JSON } from './client-library-changed-source55-fixture.mjs';
import { CHANGED_ROOT as ROOT, CHANGED_OUTPUT as OUTPUT, CHANGED_UNIT, CHANGED_CONTROLLER_UNIT, CHANGED_LIMITS,
  parseLibraryChangedArguments, validateLibraryChangedInput, validateLibraryChangedViewer, validateLibraryChangedBaseline,
  validateLibraryChangedControl, parseLibraryChangedJSON, projectLibraryChangedMessage, libraryChangedCatalogRequest,
  projectLibraryChangedItems, libraryChangedRoute, pairLibraryChangedReads, libraryChangedWindowEvidence, libraryChangedNavigationEvidence,
  libraryChangedPublication, encodeLibraryChangedRecord, publishLibraryChangedRecord, LibraryChangedObserver,
  observeLibraryChangedDOM, libraryChangedStageRecord, LibraryChangedWorkflow, libraryChangedObservationPassed,
  disposeLibraryChangedSecrets, validateLibraryChangedAbort, readLibraryChangedSnapshot,
  libraryChangedSetupFailure, observeLibraryChangedCandidates, captureLibraryChangedFailureScreenshot,
  publishLibraryChangedScreenshot, CHANGED_DIAGNOSTIC_LIMITS } from './client-browser-library-changed-source55.mjs';
import { collectLibraryChangedPublicDOM, observeLibraryChangedViewport, libraryChangedSecretVariants,
  libraryChangedResponseDiagnostic } from './client-browser-library-changed-source55.mjs';

const WORK = '/opt/goby-test/exec-work-m3e', TOOL = WORK + '/client-library-changed-source55-tool-04';
const SYNTHETIC_UPGRADE = WORK + '/client-schema28-source55-upgrade-20260912_120000_c0ffee123456';
const USER = 'ecbbe4cb82403879bc4b4f78894c5738', ITEM = '268051d3ca734aefcf94e245fb25ad55';
const LIBRARY = 'a9993591e72f0f2e7babcbf8b9c50790';
const TOKEN = 'synthetic-library-changed-token-only', START = Date.parse('2026-09-12T12:00:00.000Z');
const ROUTE = '/web/index.html#!/observed-movies?parentId=' + LIBRARY;
const SELF = fileURLToPath(import.meta.url), sha = value => createHash('sha256').update(value).digest('hex');
const clone = value => Array.isArray(value) ? value.map(clone) : value !== null && typeof value === 'object'
  ? Object.fromEntries(Object.entries(value).map(([key, child]) => [key, clone(child)])) : value;
const descriptor = (filename, hash = 'a'.repeat(64)) => ({ path: filename, sha256: hash });
const tests = [];
function test(name, run) { tests.push({ name, run }); }
function check(value, detail = 'synthetic_library_changed_assertion') { if (!value) throw new Error(detail); }
function equal(left, right) { check(JSON.stringify(left) === JSON.stringify(right)); }
function rejects(run) { let rejected = false; try { run(); } catch { rejected = true; } check(rejected); }
async function rejectsAsync(run) { let rejected = false; try { await run(); } catch { rejected = true; } check(rejected); }

export function libraryChangedInputFixture() {
  const names = ['client-browser-library-changed-source55.mjs', 'client-library-changed-source55-fixture.mjs', 'client-browser-cross-user.mjs',
    'client-browser-library-home.mjs', 'client-browser-session-proof.mjs', 'client-browser-goby-fixture.mjs', 'client-browser-special-features-fixture.mjs'];
  return { marker: 'goby-client-library-changed-input-v1', version: 1, mode: 'b-movies-name-automatic-refresh', root: ROOT, output: OUTPUT,
    actor: { slot: 'B', user_id: USER, credentials: descriptor(ROOT + '/viewer-credentials.json'), account_key: 'viewer',
      source_credentials_sha256: '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790' },
    candidate: { binary_sha256: sha('synthetic-source55-binary'),
      process: { pid: 2264063, start_ticks: 21104222, boot_id: '6bdfc486-7bc8-412f-82b5-70095a09dde7' },
      runtime_sha256: sha('synthetic-source55-runtime'), state_sha256: sha('synthetic-source55-state'),
      server_id: 'c7cfd76b1dee728b2bad523793a37ccb', base_url: 'http://127.0.0.1:18196', direct_url: 'http://127.0.0.1:18198',
      source: WORK + '/source-attempt-55', source_manifest_sha256: '7d2548603e209ebce6154853321147aeec33ccbb40cc12b3ca37418ad765937a',
      invocation_id: sha('synthetic-source55-invocation').slice(0, 32), publication: sha('synthetic-source55-publication').slice(0, 40) },
    fixture: Object.fromEntries(['profile_receipt', 'profile_report', 'profile_inspection', 'music_chain', 'music_scan_receipt']
      .map(name => [name, descriptor(WORK + '/synthetic/' + name + '.json')])),
    expected_libraries: [
      { id: LIBRARY, name: 'M3e Client Movies' },
      { id: '6383d20008836e137559698c29b10395', name: 'M3e Client Music' },
      { id: 'a34ce665fb75421ef7551570f353d705', name: 'M3e Client Television' },
      { id: '57a85c1ca5b6c7ae602c587755250b2f', name: 'M3e Client Special Features Movies' }],
    target: { id: ITEM, library_id: LIBRARY, name: 'M3e Client Movie', type: 'Movie', parent_id: LIBRARY,
      root_id: 'ba1501cff993a630b8d6a41f925cd37f', relative_path: 'M3e Client Movie.mp4' },
    source_closure: Object.fromEntries(names.map(name => [TOOL + '/' + name, 'b'.repeat(64)])),
    authority: { upgrade_intent: descriptor(WORK + '/client-schema28-source55-tool-05/intent.json', sha('synthetic-upgrade-intent')),
      upgrade_report: descriptor(SYNTHETIC_UPGRADE + '/report.json', sha('synthetic-upgrade-report')),
      upgrade_attestation: descriptor(SYNTHETIC_UPGRADE + '/attestation.json', sha('synthetic-upgrade-attestation')),
      current_snapshot: descriptor(SYNTHETIC_UPGRADE + '/after-full.json', sha('synthetic-current-snapshot')),
      before_snapshot: descriptor(ROOT + '/before-full.json'), history: [
        ['c88794dbd9bdedcb6c16fea4dd8a7d8af2d09012728084b30e24701db5872c73', 'c980edd73bbf08c6190391f2c5368abd7973c5c1381bcec70d7be93477f8c743',
          '467f473ef20b1b05bfc76c863b41f69eeb77f1f2a783f57d6549beaa46f28104', 'b23a1a156e781c771e3bb4b1ba31bfb048e29e77504569d129397c79445265d6',
          '36ff8634f85a58841c1c6e4558de5e4dcfea1a842bae8e40a26c1d37c12ff32f', 'a13f976b7097e33337527ef2cf10ad9203d755e0fd2cec43307edfa6efbfe8bc'],
        ['eab8c899d06b525271afec345d4137b3e0adec70bb5e8ecf911fdf4dae7e9f6e', '9360e3b5c60b16f714b80b2da3f1f7a31d58313e196abee99dfbb4f8e3bc9d1f',
          'e9b931c568c98e4438917d8c204922267b0931a2de4c9f6bba40f2155c196fb4', 'e53e9777337c8c0b33a0cc86dad2e22c79d5e7f2601aa64dd68c6ed91cd9ecf1',
          '10563f9b12e62a321bbda67c49bfbfc1a6e3d2c2e304d3c2e1e7d64502b2c897', 'a84e5e480a70b7d84e49001aaae791a522cd38c6b1055dcb503f0312f933c2dd']
      ].map((hashes, index) => {
        const version = index + 2, root = WORK + '/client-library-changed-ui-source55-v' + version;
        return { version, input: descriptor(root + '/input.json', hashes[0]), browser_report: descriptor(root + '/browser/report.json', hashes[1]),
          controller_report: descriptor(root + '/report.json', hashes[2]),
          terminal: descriptor(WORK + '/client-library-changed-source55-execution-0' + version + '/failed-terminal.json', hashes[3]),
          before_snapshot: descriptor(root + '/before-full.json', hashes[4]), after_snapshot: descriptor(root + '/after-full.json', hashes[5]) };
      }) },
    controller: { pid: 2000000, start_ticks: '12000000', boot_id: '6bdfc486-7bc8-412f-82b5-70095a09dde7', unit: CHANGED_CONTROLLER_UNIT } };
}

function binding(input = libraryChangedInputFixture()) {
  return { input_sha256: '1'.repeat(64), source_closure_sha256: '2'.repeat(64), controller: clone(input.controller),
    node_process: { pid: 2000001, start_ticks: '12000001', boot_id: input.controller.boot_id, uid: 0, gid: 0,
      executable_path: '/synthetic/node', executable_sha256: '3'.repeat(64), cgroup: '/system.slice/' + CHANGED_UNIT } };
}
export function libraryChangedReservationFixture() {
  return { target_id: ITEM, library_id: LIBRARY, revision: '1', original_name: 'M3e Client Movie', marker_name: 'M3e Client Movie [LC source55 v4]',
    original_controls_sha256: '4'.repeat(64), forward_body_sha256: '5'.repeat(64), restore_body_sha256: '6'.repeat(64) };
}
function controlFixture(name = 'reserved', bound = binding()) {
  return { marker: 'goby-client-library-changed-control-v1', version: 1, ...clone(bound), name,
    previous_stage_sha256: '7'.repeat(64), reservation: libraryChangedReservationFixture(),
    commit: name === 'reserved' ? null : { revision: name === 'forward' ? '2' : '3', write_completed_at: new Date(START + 2000).toISOString(),
      native_result_sha256: '8'.repeat(64), readback_sha256: '9'.repeat(64) },
    restoration: name === 'restored' || name === 'close' ? 'confirmed' : 'pending' };
}
function stateFixture(name = 'reserved') {
  return { input: libraryChangedInputFixture(), started_at: START, aborted: false, stage_attempts: [],
    stages: [{ name: { reserved: 'discovery', forward: 'armed', restored: 'restore-armed', close: 'restored' }[name],
      sha256: '7'.repeat(64), publication_started_at: START + 1000 }],
    consumed: name === 'reserved' ? [] : ['reserved'], reservation: name === 'reserved' ? null : libraryChangedReservationFixture() };
}
function messageBytes(id = 'synthetic-catalog-event-1') {
  return Buffer.from(JSON.stringify({ MessageType: 'LibraryChanged', MessageId: id,
    Data: { ItemsAdded: [], ItemsRemoved: [], ItemsUpdated: [ITEM], FoldersAddedTo: [], FoldersRemovedFrom: [], CollectionFolders: [], IsEmpty: false } }));
}
function domObservation(expected, passed = true) {
  return { route: ROUTE, document_id: 'document-2', target_id: ITEM, expected_name: expected,
    visible_target_cards: 1, target_title_count: passed ? 1 : 0, forbidden_title_count: passed ? 0 : 1,
    identity_proven: true, media_inactive: true, passed,
    selector: { identity_attribute: 'data-id', title_mode: 'exact-visible-text', card_tag: 'div', card_class: 'card', context: 'current-visible-dom' } };
}

export function libraryChangedWindowFixture(name = 'forward') {
  const reservation = libraryChangedReservationFixture(), expected = name === 'forward' ? reservation.marker_name : reservation.original_name;
  const token = sha(TOKEN), projection = projectLibraryChangedMessage(messageBytes(name === 'forward' ? 'forward-event' : 'restored-event'));
  const physical = { id: 1, kind: 'items', phase: name, method: 'GET', route: `/Users/${USER}/Items`, query: [['ParentId', LIBRARY]],
    shape_sha256: 'a'.repeat(64), request_sha256: 'b'.repeat(64), token_sha256: token, request_elapsed_ms: 3150, request_sequence: 3150,
    status: 200, response_elapsed_ms: 3200, completed: true, terminal: 'completed', terminal_status: 200, finished_elapsed_ms: 3250,
    response_bytes: 100, projection: { target: { Id: ITEM, Name: expected, Type: 'Movie' }, count: 1, body_sha256: 'c'.repeat(64), body_bytes: 100 } };
  const frame = { index: 1, kind: 'items', phase: name, route: physical.route, shape_sha256: physical.shape_sha256,
    request_sha256: physical.request_sha256, token_sha256: token, request_elapsed_ms: 3100, request_sequence: 3100,
    source: 'page', main_frame: true, worker_id: null, document_id: 'document-2', page_route: ROUTE,
    status: 200, response_elapsed_ms: 3260, finished_elapsed_ms: 3300, content_type: 'application/json',
    from_service_worker: false, finished: true, failed: false };
  const result = { name, control_sha256: 'd'.repeat(64), commit: { revision: name === 'forward' ? '2' : '3',
    write_completed_at: new Date(START + 2000).toISOString(), native_result_sha256: '8'.repeat(64), readback_sha256: '9'.repeat(64) },
    boundary: { name, started_elapsed_ms: 1000, started_sequence: 900, response_completed_elapsed_ms: 2000, end_elapsed_ms: 122000,
      completed_elapsed_ms: 122010, duration_ms: 120000, route: ROUTE, document_id: 'document-2',
      connection_id: 'library-changed-ws-0', token_sha256: token, websocket_seen: 1, websocket_opened: 1 },
    events: { physical: [{ sequence: 2900, elapsed_ms: 2900, connection_id: 'library-changed-ws-0', token_sha256: token,
      complete: true, received: true, forwarded: true, message: clone(projection) }],
    browser: [{ sequence: 3000, elapsed_ms: 3000, connection_id: 'library-changed-ws-0', token_sha256: token,
      document_id: 'document-2', route: ROUTE, complete: true, received: true, forwarded: true, message: clone(projection) }] },
    http: { physical: [physical], frames: [frame], pairs: pairLibraryChangedReads([physical], [frame]) }, dom: [], actions: [], lifecycle: [] };
  for (let start = 1000; start <= 121500; start += 500) result.dom.push({ sequence: start + 10, started_elapsed_ms: start,
    elapsed_ms: start + 10, observation: domObservation(expected, start >= 3500) });
  return result;
}

function completedReportFixture() {
  const input = libraryChangedInputFixture(), token = sha(TOKEN), loginHash = 'e'.repeat(64);
  const forward = libraryChangedWindowFixture(), restored = libraryChangedWindowFixture('restored');
  const reservedControl = controlFixture('reserved'), forwardControl = controlFixture('forward'), restoredControl = controlFixture('restored');
  forwardControl.commit = clone(forward.commit); restoredControl.commit = clone(restored.commit);
  restored.control_sha256 = 'f'.repeat(64);
  Object.assign(forward, libraryChangedWindowEvidence(forward, input, reservedControl.reservation));
  Object.assign(restored, libraryChangedWindowEvidence(restored, input, reservedControl.reservation));
  return { failure: null, abort: null, restoration: 'confirmed', target: clone(input.target),
    stages: ['discovery', 'armed', 'restore-armed', 'restored'].map((name, index) => ({ name, sha256: String(index + 1).repeat(64) })),
    controls: [{ name: 'reserved', sha256: 'c'.repeat(64), value: reservedControl },
      { name: 'forward', sha256: forward.control_sha256, value: forwardControl },
      { name: 'restored', sha256: restored.control_sha256, value: restoredControl }],
    control_close: { value: { restoration: 'confirmed', previous_stage_sha256: '4'.repeat(64) } },
    discovery: libraryChangedNavigationFixture(),
    armed: { quiet: { passed: true, duration_ms: 20000, catalog_requests: 0, library_changed_messages: 0 } }, forward, restored,
    catalog_observation: { observer_failure: null }, login_proof: { token_sha256: token, session_id: '1'.repeat(32) },
    session_private: descriptor(OUTPUT + '/session-private.json'), capabilities_private: descriptor(OUTPUT + '/capabilities-private.json'),
    observation: { frames: [{ kind: 'login', finished: true, failed: false, status: 200, from_service_worker: false,
      content_type: 'application/json', request_sha256: loginHash }],
    physical: [{ kind: 'login', completed: true, terminal_status: 200, request_sha256: loginHash }] },
    actor: { login: { request_count: 1, status: 200 }, ordinary_authority_confirmed: true, page_error_count: 0, closed: true,
      logout: { status: 204, login_view_visible: true },
      proxy: { login: 1, logout: 1, preparation: 0, failed: 0, rejected: 0, active: 0, completed: 1, admitted: 1 },
      proxy_logout: { completed: true, status: 204, token_fingerprint: token },
      network: { external_blocked: 0, forbidden_mutations: 0, playback_attempts: 0, observer_errors: 0, overflow: 0, guard_errors: 0 },
      console_warning_error_count: 0, cleanup_failures: [], websocket: { failed: 0, control_attempts: 0 },
      session_proof: { outcome: 'all_observed_logout_tokens_rejected', entries: [{ result: 'logout_token_rejected', token_fingerprint: token }] } },
    closure: { context_closed: true, browser_closed: true, proxy_closed: true, http_pending: 0, websocket_pending: 0,
      websocket_active: 0, websocket_opened: 1, websocket_closed: 1, sockets_remaining: 0, cleanup_failures: [] } };
}

export function libraryChangedNavigationFixture() {
  const input = libraryChangedInputFixture(), read = clone(libraryChangedWindowFixture('restored').http.pairs[0]);
  const collection = clone(read), route = `/Users/${USER}/Items/${LIBRARY}`;
  const bytes = Buffer.from(JSON.stringify({ Id: LIBRARY, Name: 'M3e Client Movies', Type: 'CollectionFolder',
    IsFolder: true, CollectionType: 'movies', Subviews: ['movies', 'movies', 'folders'] }));
  Object.assign(collection.physical, { id: 20, kind: 'collection-folder', phase: 'discovery', route, query: [],
    shape_sha256: sha(JSON.stringify([route, []])), request_sha256: sha('synthetic-source55-collection-request'),
    response_bytes: bytes.length, projection: projectLibraryChangedItems(bytes, 'collection-folder') });
  Object.assign(collection.frame, { index: 20, kind: 'collection-folder', phase: 'discovery', route,
    shape_sha256: collection.physical.shape_sha256, request_sha256: collection.physical.request_sha256 });
  const discovery = { home: { passed: true }, dom: domObservation(input.target.name), reads: [read],
    collection_folder_reads: [collection], query_allowlist: [{ kind: read.physical.kind, route: read.physical.route,
      query: read.physical.query, shape_sha256: read.physical.shape_sha256 }], socket: { token_sha256: sha(TOKEN) },
    navigation: { before_route: '/web/index.html#!/synthetic-home', after_route: ROUTE, before_sequence: 1000 } };
  discovery.collection_folder = libraryChangedNavigationEvidence(discovery, input);
  return discovery;
}

test('source55 input binds the exact seven-file new scope', () => {
  validateLibraryChangedInput(libraryChangedInputFixture());
  for (const change of [value => { value.candidate.process.pid = 1; }, value => { value.candidate.source = WORK + '/source-attempt-44'; },
    value => { value.candidate.state_sha256 = '0'.repeat(64); }, value => { delete value.authority.upgrade_attestation; },
    value => { value.actor.credentials.path = WORK + '/browser.json'; }, value => { value.actor.admin = {}; },
    value => { value.root = ROOT.replace('source55-v4', 'source55-v3'); },
    value => { value.source_closure = Object.fromEntries(Object.entries(value.source_closure)
      .map(([filename, hash]) => [filename.replace('source55-tool-04', 'source55-tool-03'), hash])); },
    value => { value.source_closure[TOOL + '/unexpected.mjs'] = 'a'.repeat(64); }, value => { delete value.source_closure[TOOL + '/client-browser-goby-fixture.mjs']; },
    value => { value.target.id = USER; }, value => { value.controller.unit = CHANGED_UNIT; }]) {
    const input = libraryChangedInputFixture(); change(input); rejects(() => validateLibraryChangedInput(input));
  }
});

test('source55 requires future process and upgrade authority inputs without defaults', () => {
  for (const key of ['binary_sha256', 'process', 'runtime_sha256', 'state_sha256', 'invocation_id', 'publication']) {
    const input = libraryChangedInputFixture(); delete input.candidate[key]; rejects(() => validateLibraryChangedInput(input));
  }
  for (const key of ['upgrade_intent', 'upgrade_report', 'upgrade_attestation', 'current_snapshot', 'before_snapshot']) {
    const input = libraryChangedInputFixture(); delete input.authority[key]; rejects(() => validateLibraryChangedInput(input));
  }
  for (const index of [0, 1]) for (const key of ['input', 'browser_report', 'controller_report', 'terminal', 'before_snapshot', 'after_snapshot']) {
    const input = libraryChangedInputFixture(); input.authority.history[index][key].sha256 = sha('synthetic-foreign-prior'); rejects(() => validateLibraryChangedInput(input));
  }
  const reordered = libraryChangedInputFixture(); reordered.authority.history.reverse(); rejects(() => validateLibraryChangedInput(reordered));
  for (const mutate of [value => { value.candidate.publication = 'pending'; }, value => { value.candidate.publication = 'a'.repeat(40); },
    value => { value.candidate.invocation_id = '0'.repeat(32); }, value => { value.candidate.process.start_ticks = 0; },
    value => { value.candidate.process.extra = true; }, value => { value.authority.current_snapshot.path = ROOT + '/before-full.json'; },
    value => { value.authority.upgrade_intent.path = WORK + '/client-schema28-source55-tool-02/intent.json'; },
    value => { value.authority.upgrade_intent.path = WORK + '/client-fixture.json'; },
    value => { value.authority.upgrade_report.path = WORK + '/synthetic-source55-upgrade/report.json'; },
    value => { value.authority.upgrade_report.path = SYNTHETIC_UPGRADE + '/other.json'; },
    value => { value.authority.upgrade_attestation.path = SYNTHETIC_UPGRADE.replace('c0ffee123456', 'c0ffee654321') + '/attestation.json'; },
    value => { value.authority.upgrade_attestation.path = SYNTHETIC_UPGRADE + '/report.json'; },
    value => { value.authority.current_snapshot.path = WORK + '/client-fixture.json'; }]) {
    const input = libraryChangedInputFixture(); mutate(input); rejects(() => validateLibraryChangedInput(input));
  }
  const input = libraryChangedInputFixture();
  input.candidate.binary_sha256 = sha('another-synthetic-required-binary'); input.candidate.process.pid++;
  validateLibraryChangedInput(input);
});

test('source55 CollectionFolder capture requires the exact real response fields', () => {
  const value = { Id: LIBRARY, Name: 'M3e Client Movies', Type: 'CollectionFolder', IsFolder: true,
    CollectionType: 'movies', Subviews: ['movies', 'movies', 'folders'] };
  const project = input => projectLibraryChangedItems(Buffer.from(JSON.stringify(input)), 'collection-folder');
  equal(project(value).collection_folder.Subviews, ['movies', 'movies', 'folders']);
  for (const mutate of [input => { input.Id = ITEM; }, input => { input.Name = 'Movies'; }, input => { input.Type = 'Folder'; },
    input => { input.IsFolder = false; }, input => { input.CollectionType = 'tvshows'; }, input => { delete input.Subviews; },
    input => { input.Subviews = ['movies', 'folders']; }, input => { input.Subviews = ['movies', 'folders', 'movies']; },
    input => { input.Subviews = ['movies', 'movies', 'folders', 'folders']; }, input => { input.Subviews = 'movies,movies,folders'; }]) {
    const altered = clone(value); mutate(altered); rejects(() => project(altered));
  }
});

test('source55 navigation rebinds the exact completed frame and physical exchange', () => {
  const input = libraryChangedInputFixture(), discovery = libraryChangedNavigationFixture();
  equal(libraryChangedNavigationEvidence(discovery, input), discovery.collection_folder);
  for (const mutate of [value => { value.collection_folder_reads = []; }, value => { value.collection_folder_reads[0].physical.status = 500; },
    value => { value.collection_folder_reads[0].physical.completed = false; }, value => { value.collection_folder_reads[0].frame.finished = false; },
    value => { value.collection_folder_reads[0].frame.from_service_worker = true; }, value => { value.collection_folder_reads[0].frame.failed = true; },
    value => { value.collection_folder_reads[0].frame.main_frame = false; }, value => { value.collection_folder_reads[0].frame.document_id = 'document-other'; },
    value => { value.collection_folder_reads[0].frame.page_route = '/web/index.html#!/other'; },
    value => { value.collection_folder_reads[0].frame.request_sha256 = sha('synthetic-other-request'); },
    value => { value.collection_folder_reads[0].physical.projection.collection_folder.Subviews.reverse(); },
    value => { value.collection_folder_reads[0].physical.method = 'POST'; },
    value => { value.collection_folder_reads[0].frame.request_sequence = value.navigation.before_sequence; },
    value => { value.navigation.after_route = value.navigation.before_route; },
    value => { value.collection_folder_reads.push(clone(value.collection_folder_reads[0])); },
    value => { value.collection_folder_reads = clone(value.reads); }]) {
    const altered = clone(discovery); mutate(altered); rejects(() => libraryChangedNavigationEvidence(altered, input));
  }
});

test('source55 accepts only the two routes actually observed around the library click', () => {
  const input = libraryChangedInputFixture(), discovery = libraryChangedNavigationFixture();
  discovery.collection_folder_reads[0].frame.page_route = discovery.navigation.before_route;
  check(libraryChangedNavigationEvidence(discovery, input).passed);
  discovery.collection_folder_reads[0].frame.page_route = '/web/index.html#!/unrelated';
  rejects(() => libraryChangedNavigationEvidence(discovery, input));
});

test('source55 CollectionFolder reads retain the existing owned worker provenance rule', () => {
  const input = libraryChangedInputFixture(), discovery = libraryChangedNavigationFixture();
  Object.assign(discovery.collection_folder_reads[0].frame, { source: 'worker', main_frame: false, worker_id: 'worker-1' });
  check(libraryChangedNavigationEvidence(discovery, input).passed);
  for (const mutate of [value => { value.worker_id = null; }, value => { value.main_frame = true; }, value => { value.source = 'foreign'; }]) {
    const altered = clone(discovery); mutate(altered.collection_folder_reads[0].frame);
    rejects(() => libraryChangedNavigationEvidence(altered, input));
  }
});

test('source55 navigation cannot stand in for either automatic refresh window', () => {
  const input = libraryChangedInputFixture(), reservation = libraryChangedReservationFixture();
  for (const name of ['forward', 'restored']) {
    const window = libraryChangedWindowFixture(name), pair = libraryChangedNavigationFixture().collection_folder_reads[0];
    window.http = { physical: [pair.physical], frames: [pair.frame], pairs: [pair] };
    check(libraryChangedWindowEvidence(window, input, reservation).outcome === 'automatic_http_not_observed_within_window');
  }
  const report = completedReportFixture(); report.discovery.collection_folder_reads[0].frame.finished = false;
  check(libraryChangedObservationPassed(report) === false);
});

test('source55 keeps CollectionFolder query shapes separate after discovery', () => {
  const input = libraryChangedInputFixture(), observer = new LibraryChangedObserver({ input, login: {} });
  const raw = `http://127.0.0.1:18196/Users/${USER}/Items/${LIBRARY}`, selected = libraryChangedCatalogRequest(raw, 'GET', input);
  const discovery = libraryChangedNavigationFixture();
  discovery.collection_folder_reads[0].physical.shape_sha256 = selected.shape_sha256;
  observer.discovery = discovery;
  check(observer.scope(raw, 'GET').kind === 'collection-folder');
  rejects(() => observer.scope(raw + '?Fields=Different', 'GET'));
  observer.discovery.collection_folder_reads = [];
  rejects(() => observer.scope(raw, 'GET'));
});

test('CLI and viewer export cannot adopt consumed scopes or administrator credentials', () => {
  parseLibraryChangedArguments(['--input', ROOT + '/input.json', '--input-sha256', 'a'.repeat(64), '--output', OUTPUT]);
  rejects(() => parseLibraryChangedArguments(['--input', ROOT + '/input.json', '--input-sha256', 'a'.repeat(64), '--output', ROOT + '/old']));
  const viewer = { marker: 'goby-client-library-changed-viewer-v1', slot: 'B', user_id: USER, base_url: 'http://127.0.0.1:18196',
    direct_url: 'http://127.0.0.1:18198', viewer: { username: 'm3e-client-viewer', password: '0'.repeat(48) } };
  validateLibraryChangedViewer(viewer, libraryChangedInputFixture());
  for (const change of [value => { value.admin = {}; }, value => { value.version = 1; }, value => { value.viewer.token = TOKEN; },
    value => { value.user_id = ITEM; }, value => { value.direct_url = 'http://127.0.0.1:18197'; }]) {
    const value = clone(viewer); change(value); rejects(() => validateLibraryChangedViewer(value, libraryChangedInputFixture()));
  }
});

test('the complete before ledger must match current authority and its target', () => {
  const input = libraryChangedInputFixture(), names = ['activity_entries', 'application_key_clients', 'application_key_devices', 'application_keys',
    'catalog_entities', 'client_playback_references', 'devices', 'encoding_jobs', 'extra_reserved_paths', 'item_entities', 'item_extra_resources',
    'item_images', 'item_metadata_state', 'item_subtitles', 'item_theme_resources', 'items', 'libraries', 'library_roots', 'managed_settings',
    'play_sessions', 'scan_jobs', 'schema_migrations', 'server_settings', 'sessions', 'task_definitions', 'task_occurrences', 'task_run_children',
    'task_run_requests', 'task_runs', 'task_triggers', 'theme_owner_ids', 'theme_reserved_paths', 'user_item_data', 'user_settings', 'users'];
  const tables = Object.fromEntries(names.map(name => [name, []]));
  tables.sessions = Array.from({ length: 77 }, (_, index) => ({ id: String(index).padStart(32, '0'), token_hash: '\\x' + String(index).padStart(64, '0') }));
  tables.devices = Array.from({ length: 66 }, (_, index) => ({ reported_device_id: 'old-device-' + index }));
  for (const [name, count] of [['activity_entries', 171], ['play_sessions', 26], ['user_item_data', 7]]) tables[name] = Array.from({ length: count }, () => ({}));
  tables.libraries = clone(input.expected_libraries); tables.users = [{ id: USER, is_disabled: false, is_administrator: false, management_revision: 5 }];
  tables.items = [{ ...input.target, is_folder: false }, ...Array.from({ length: 21 }, () => ({}))]; tables.item_metadata_state = [{ item_id: ITEM }];
  const sequences = Object.fromEntries(['activity_entries_id_seq', 'application_keys_id_seq', 'catalog_entities_id_seq',
    'devices_id_seq', 'theme_owner_ids_id_seq'].map(name => [name, { last_value: '1', is_called: true }]));
  tables.library_roots = [{ id: '9'.repeat(32), binding_revision: 1, storage_binding: null, bound_at: null, bound_by: null }];
  Object.assign(tables.activity_entries[0], { previous_revision: 0, observation_fingerprint: '' });
  const current = { schema: 28, database: { tables, sequences, catalog: {}, metadata: { captured_at: new Date(START).toISOString() } } };
  current.database.sequences.activity_entries_id_seq.last_value = 9007199254740993n;
  tables.items[0].file_size = 9007199254740995n;
  const before = clone(current); before.database.metadata.captured_at = new Date(START + 1000).toISOString();
  check(validateLibraryChangedBaseline(input, before, current).session_ids.length === 77);
  for (const change of [value => { value.database.tables.items[0].name = 'Foreign Name'; }, value => { value.database.tables.devices.pop(); },
    value => { value.database.sequences.unknown = 1; }, value => { value.schema = 27; },
    value => { delete value.database.tables.library_roots[0].binding_revision; },
    value => { delete value.database.tables.activity_entries[0].observation_fingerprint; },
    value => { value.database.metadata.captured_at = current.database.metadata.captured_at; },
    value => { value.database.sequences.activity_entries_id_seq.last_value = 9007199254740992n; },
    value => { value.database.tables.items[0].file_size = 9007199254740994n; },
    value => { value.database.tables.items[0].file_size = '9007199254740995'; },
    value => { value.database.tables.items[0].file_size = 9007199254740995; },
    value => { value.database.unrecognized_projection = {}; }, value => { value.unrecognized_envelope = {}; }]) {
    const altered = clone(before); change(altered); rejects(() => validateLibraryChangedBaseline(input, altered, current));
  }
});

test('only the three sealed snapshots reach the lossless reader without numeric conversion', async () => {
  const input = libraryChangedInputFixture(), calls = [];
  const parsed = parseSource55JSON('{"first":9007199254740993,"adjacent":9007199254740994,"negative":-9007199254740993}');
  const read = async (bound, key) => { check(bound === input); calls.push(key); return parsed; };
  for (const key of ['current_snapshot', 'before_snapshot', 'history_after_snapshot']) {
    const value = await readLibraryChangedSnapshot(input, key, read);
    check(value === parsed && value.first === 9007199254740993n && value.adjacent === 9007199254740994n && value.negative === -9007199254740993n);
  }
  equal(calls, ['current_snapshot', 'before_snapshot', 'history_after_snapshot']);
  for (const key of ['upgrade_intent', 'credentials', 'prior_after_snapshot', input.authority.current_snapshot.path, '__proto__']) {
    await rejectsAsync(() => readLibraryChangedSnapshot(input, key, read));
  }
  for (const mutate of [value => { value.authority.current_snapshot.path = WORK + '/client-fixture.json'; },
    value => { value.authority.before_snapshot.path = ROOT.replace('source55-v4', 'source55-v3') + '/before-full.json'; },
    value => { value.root = ROOT.replace('source55-v4', 'source55-v3'); }]) {
    const altered = clone(input); mutate(altered);
    await rejectsAsync(() => readLibraryChangedSnapshot(altered, 'current_snapshot', read));
  }
  check(calls.length === 3);
});

test('setup failures retain only the safe phase and original owned source locations', () => {
  const original = new Error(TOKEN);
  Object.defineProperty(original, 'stack', { value: 'Error: ' + TOKEN + '\n    at synthetic (' + TOOL +
    '/client-browser-library-changed-source55.mjs:123:9)\n    at foreign (/private/credentials.json:14:2)', configurable: true });
  const wrapped = libraryChangedSetupFailure(original, 'baseline');
  equal(wrapped.diagnostic, { phase: 'baseline', frames: [{ file: 'client-browser-library-changed-source55.mjs', line: 123, column: 9 }] });
  const forwarded = libraryChangedSetupFailure(wrapped, 'fixture').diagnostic;
  equal(forwarded.frames, wrapped.diagnostic.frames); check(forwarded.phase === 'baseline');
  const encoded = JSON.stringify(forwarded); check(!encoded.includes(TOKEN) && !encoded.includes(WORK) && !encoded.includes('credentials'));
  check(libraryChangedSetupFailure(original, 'arbitrary-secret-phase').diagnostic.phase === 'arguments');
});

test('failed discovery keeps evidence without creating a successful stage and cannot block cleanup', async () => {
  const input = libraryChangedInputFixture(), window = libraryChangedWindowFixture(), writes = [];
  const actor = { started: START, page: { url: () => 'http://127.0.0.1:18196' + ROUTE },
    diagnosticSecrets: () => [TOKEN], closed: false, async close() { this.closed = true; } };
  const observer = { physical: window.http.physical, frames: window.http.frames, login: {} }, report = {};
  const flow = new LibraryChangedWorkflow({ input, binding: binding(input), actor, observer, report });
  const dom = domObservation(input.target.name, false);
  flow.retainDiscovery({ home: { passed: true }, dom, before_route: '/web/index.html#!/home', before_sequence: 1 });
  await flow.captureFailureDiagnostics('library_changed_target_card_not_observed', [TOKEN], {
    candidates: async () => ({ target_id: ITEM, entries: [], targets: [], total_target_nodes: 0, truncated: false }),
    screenshot: async () => ({ status: 'omitted', reason: 'visible_content_not_safe', artifact: null }),
    publish: async (filename, value) => { writes.push({ filename, value: clone(value) }); return descriptor(filename, sha(JSON.stringify(value))); } });
  check(writes.length === 1 && writes[0].filename === OUTPUT + '/discovery-failure.json' && !actor.closed);
  check(writes[0].value.last.dom.passed === false && writes[0].value.last.catalog.pairs.length === 1 && report.stages.length === 0 && !report.discovery);
  check(report.diagnostics.discovery_failure.bytes > 0 && report.diagnostics.screenshot === null);
  await flow.captureFailureDiagnostics('library_changed_target_card_not_observed', [TOKEN]); check(writes.length === 1);
  const failed = new LibraryChangedWorkflow({ input, binding: binding(input), actor, observer, report: {} });
  await failed.captureFailureDiagnostics('library_changed_target_card_not_observed', [TOKEN], {
    candidates: async () => { throw new Error(TOKEN); }, screenshot: async () => { throw new Error(TOKEN); },
    publish: async () => { throw new Error(TOKEN); } });
  await actor.close(); check(actor.closed && failed.report.diagnostics.reason === 'failure_diagnostic_unavailable');
});

test('failure screenshots require proven safe login and remain one bounded viewport image', async () => {
  const png = () => Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), Buffer.alloc(16)]);
  let safe = true, captures = 0, written = 0;
  const page = { url: () => 'http://127.0.0.1:18196' + ROUTE,
    locator: selector => ({ selector, nth: index => ({ selector, index }) }),
    getByText: value => ({ label: value }),
    screenshot: async options => { captures++; check(options.fullPage === false && options.type === 'png' && options.mask.length === 3 && options.timeout === 2000 && options.mask[0].selector.includes('iframe')); return png(); } };
  const actor = { page, proven: false, token: TOKEN, libraryChangedDocumentID: 'document-2' }, login = { bound: { token_sha256: sha(TOKEN) } };
  const publish = async bytes => { written++; return { path: OUTPUT + '/discovery-failure.png', sha256: sha(bytes), bytes: bytes.length }; };
  const observe = async () => ({ public: { flags: { viewport_valid: true, known_secret_found: !safe, password_visible: true,
    iframe_count: 1, visible_iframe_count: 0, elements_truncated: false, text_truncated: false, unmaskable_text: false, mask_limit_exceeded: false, masked_text_nodes: 1 } },
    mask_indices: [2], stability: 'fixed-public-structure' });
  check((await captureLibraryChangedFailureScreenshot(actor, login, [TOKEN], publish, observe)).reason === 'login_not_proven' && captures === 0);
  actor.proven = true; safe = false;
  check((await captureLibraryChangedFailureScreenshot(actor, login, [TOKEN], publish, observe)).reason === 'known_credential_visible' && captures === 0);
  safe = true; const result = await captureLibraryChangedFailureScreenshot(actor, login, [TOKEN], publish, observe);
  check(result.status === 'saved' && captures === 1 && written === 1 && result.artifact.bytes === 24);
  page.screenshot = async () => { throw new Error(TOKEN); };
  const failure = await captureLibraryChangedFailureScreenshot(actor, login, [TOKEN], publish, observe);
  check(failure.reason === 'screenshot_unavailable' && failure.flags.password_visible === true && written === 1);
});

test('viewport collector reads visible ranges and alternate identity keys without input values', async () => {
  const globals = ['document', 'NodeFilter', 'getComputedStyle', 'innerHeight', 'innerWidth', 'location'];
  const saved = new Map(globals.map(name => [name, { exists: Object.hasOwn(globalThis, name), value: globalThis[name] }]));
  let valueReads = 0;
  const element = (tag, attrs = {}, top = 10) => ({ tagName: tag, classList: ['cardText'], childElementCount: 0, parentElement: null, attrs,
    attributes: Object.entries(attrs).map(([name, value]) => ({ name, value })), getAttribute(name) { return this.attrs[name] ?? null; },
    getBoundingClientRect: () => ({ x: 10, y: top, top, left: 10, right: 110, bottom: top + 20, width: 100, height: 20 }),
    closest(selector) { if (selector.startsWith('input,')) return this.tagName === 'INPUT' || Object.hasOwn(this.attrs, 'data-userid') ? this : null; return null; } });
  const body = element('BODY'), target = element('SPAN', { 'data-itemid': ITEM }), link = element('A', { href: '#!/item?id=' + ITEM + '&api_key=' + TOKEN });
  const offscreen = element('SPAN', {}, 1000), input = element('INPUT', { type: 'password' }), iframe = element('IFRAME', { hidden: '' });
  const user = element('SPAN', { 'data-userid': USER }), server = element('SPAN');
  Object.defineProperty(input, 'value', { get() { valueReads++; throw new Error('input-value-read'); } });
  const elements = [target, link, offscreen, input, iframe, user, server]; elements.forEach(node => { node.parentElement = body; });
  const text = (owner, value) => ({ parentElement: owner, nodeValue: value });
  const texts = [text(body, 'Cannot render library'), text(target, 'M3e Client Movie'), text(link, 'Details'), text(offscreen, 'Offscreen hidden'),
    text(input, 'Input text is excluded'), text(user, 'm3e-client-viewer'), text(server, 'c7cfd76b1dee728b2bad523793a37ccb')];
  globalThis.innerWidth = 1000; globalThis.innerHeight = 800; globalThis.NodeFilter = { SHOW_ELEMENT: 1, SHOW_TEXT: 4 };
  globalThis.location = { href: 'http://127.0.0.1:18196/web/index.html' };
  globalThis.getComputedStyle = node => ({ display: node === iframe ? 'none' : 'block', visibility: 'visible' });
  globalThis.document = { createTreeWalker(_body, kind) { const nodes = kind === 1 ? elements : texts; let at = 0; return { nextNode: () => nodes[at++] ?? null }; },
    createRange() { let node; return { selectNodeContents(value) { node = value; }, getBoundingClientRect: () => node.parentElement.getBoundingClientRect(), detach() {} }; } };
  try {
    const page = { locator: () => ({ evaluateAll: async (run, args) => { check(!JSON.stringify(args).includes(TOKEN)); return run([body], args); } }) };
    const observed = await observeLibraryChangedViewport(page, 'M3e Client Movie', [TOKEN]);
    const result = observed.public, encoded = JSON.stringify(result);
    check(result.flags.iframe_count === 1 && result.flags.visible_iframe_count === 0 && result.flags.password_visible && valueReads === 0);
    check(result.fragments.some(value => value.element_index === null && value.text === 'Cannot render library') && result.fragments.some(value => value.text === 'M3e Client Movie'));
    check(!encoded.includes('Offscreen hidden') && !encoded.includes('Input text') && !encoded.includes('m3e-client-viewer') && !encoded.includes(TOKEN) && !encoded.includes('http://'));
    check(result.target_id_carriers.some(value => value.key === 'data-itemid') && result.target_id_carriers.some(value => value.kind === 'href-query' && value.key === 'id'));
    check(observed.mask_indices.length === 1 && observed.mask_indices[0] === 6 && result.target_name_matches[0] === 0);
    const limited = collectLibraryChangedPublicDOM([body], { ...CHANGED_DIAGNOSTIC_LIMITS, viewport_elements: 2, target_id: ITEM, target_name: 'M3e Client Movie' });
    check(limited.flags.elements_truncated && limited.element_count === 3 && limited.fragments.length > 0);
  } finally { for (const [name, prior] of saved) { if (prior.exists) globalThis[name] = prior.value; else delete globalThis[name]; } }
});

test('viewport redacts encoded credentials across leaves while keeping other public text', async () => {
  const secret = 'private value/with+encoding', encoded = encodeURIComponent(encodeURIComponent(secret)), cut = Math.floor(encoded.length / 2);
  const variants = libraryChangedSecretVariants([secret]); check(variants.includes(encoded));
  const page = { locator: () => ({ evaluateAll: async (_run, args) => { check(!JSON.stringify(args).includes(secret)); return {
    fragments: [{ element_index: 0, leaf_owner: true, text: 'M3e Client Movie' }, { element_index: 1, leaf_owner: true, text: encoded.slice(0, cut) },
      { element_index: 2, leaf_owner: true, text: encoded.slice(cut) }, { element_index: 3, leaf_owner: true, text: 'm3e-client-viewer' }],
    structures: [], target_name_matches: [], target_id_carriers: [], flags: { viewport_valid: true, elements_truncated: false, text_truncated: false },
    element_count: 4, text_nodes_scanned: 4, visible_text_chars: encoded.length + 36 }; } }) };
  const result = (await observeLibraryChangedViewport(page, 'M3e Client Movie', [secret])).public;
  check(result.flags.known_secret_found && result.fragments[0].text === 'M3e Client Movie' && result.fragments[3].text === 'm3e-client-viewer');
  check(result.fragments[1].text === '[text redacted: known credential]' && result.fragments[2].text === '[text redacted: known credential]');
  check(!JSON.stringify(result).includes(encoded.slice(0, cut)));
});

test('DTO diagnostics expose bounded safe field names and no private values', () => {
  const item = { Id: ITEM, Name: 'M3e Client Movie', Type: 'Movie', ServerId: 'c7cfd76b1dee728b2bad523793a37ccb', LocationType: 'FileSystem', IsFolder: false,
    MediaType: 'Video', ImageTags: { Primary: TOKEN }, UserData: { Token: TOKEN }, Path: '/private/' + TOKEN, ParentBackdropItemId: USER, [TOKEN]: TOKEN };
  const bytes = Buffer.from(JSON.stringify({ Items: [item] })), result = libraryChangedResponseDiagnostic(bytes, 'items', [TOKEN]);
  check(result.fields.ImageTags.type === 'object' && result.fields.UserData.present && result.public_values.LocationType === 'FileSystem');
  check(result.top_level_fields.some(value => value.key === 'ParentBackdropItemId' && value.type === 'string'));
  check(!JSON.stringify(result).includes(TOKEN) && !JSON.stringify(result).includes('/private/') && !JSON.stringify(result).includes(USER));
  check(projectLibraryChangedItems(bytes, 'items').target.Name === item.Name);
});

test('public candidate diagnostics retain duplicate siblings and redact private text', async () => {
  const globals = ['getComputedStyle', 'innerHeight', 'innerWidth'];
  const saved = new Map(globals.map(name => [name, { exists: Object.hasOwn(globalThis, name), value: globalThis[name] }]));
  globalThis.getComputedStyle = () => ({ display: 'block', visibility: 'visible' }); globalThis.innerWidth = 1000; globalThis.innerHeight = 800;
  const node = (id, text, type = 'Movie') => ({ tagName: 'DIV', classList: ['card', 'cardText'], innerText: text, parentElement: null,
    getAttribute: name => name === 'data-id' ? id : name === 'data-type' ? type : null,
    getBoundingClientRect: () => ({ x: 10, y: 10, top: 10, left: 10, bottom: 30, right: 100, width: 90, height: 20 }),
    closest() { return this.parentElement ?? this; } });
  try {
    const parent = node(null, 'Public title ' + TOKEN), first = node(ITEM, 'M3e Client Movie'), second = node(ITEM, 'M3e Client Movie 2026', 'movie');
    const missing = node(ITEM, 'M3e Client Movie', null), other = node(ITEM, 'M3e Client Movie', 'other');
    const nodes = [first, second, missing, other]; for (const value of nodes) value.parentElement = parent;
    const page = { locator: () => ({ count: async () => nodes.length, evaluateAll: async (run, values) => run(nodes, values) }) };
    const value = await observeLibraryChangedCandidates(page, [TOKEN]);
    check(value.total_target_nodes === 4 && value.targets.length === 4 && value.entries[value.targets[0]].parent_index === value.entries[value.targets[1]].parent_index);
    equal(value.targets.map(index => { const entry = value.entries[index];
      return [entry.data_type, entry.data_type_present, entry.type_matches_expected, entry.type_casefold_matches_movie]; }),
    [['Movie', true, true, true], [null, true, false, true], [null, false, false, false], [null, true, false, false]]);
    check(!JSON.stringify(value).includes(TOKEN) && value.entries.some(entry => entry.text.includes('2026')) && value.entries.length <= CHANGED_DIAGNOSTIC_LIMITS.structure_nodes);
    check(!JSON.stringify(value).includes('href') && !JSON.stringify(value).includes('outerHTML'));
  } finally { for (const [name, prior] of saved) { if (prior.exists) globalThis[name] = prior.value; else delete globalThis[name]; } }
});

test('stage and control barriers bind exact phase, native receipt and reservation', () => {
  for (const name of ['reserved', 'forward', 'restored', 'close']) validateLibraryChangedControl(controlFixture(name), binding(), stateFixture(name), START + 3000);
  const observed = { home: {}, dom: {}, reads: [], query_allowlist: [], socket: {},
    collection_folder_reads: [], collection_folder: {}, navigation: {} };
  const stage = libraryChangedStageRecord('discovery', observed, { ...binding(), token_sha256: sha(TOKEN) }, descriptor(OUTPUT + '/session-private.json'), null);
  check(stage.name === 'discovery');
  rejects(() => libraryChangedStageRecord('armed', observed, { ...binding(), token_sha256: sha(TOKEN) }, descriptor(OUTPUT + '/session-private.json'), 'a'.repeat(64)));
  for (const change of [value => { value.previous_stage_sha256 = '0'.repeat(64); }, value => { value.node_process.pid++; },
    value => { value.reservation.target_id = USER; }, value => { value.commit.revision = '3'; }, value => { value.commit.write_completed_at = new Date(START - 2000).toISOString(); },
    value => { value.reservation.marker_name = value.reservation.original_name; }, value => { value.commit.native_result_sha256 = 'bad'; }]) {
    const value = controlFixture('forward'); change(value); rejects(() => validateLibraryChangedControl(value, binding(), stateFixture('forward'), START + 3000));
  }
  const state = stateFixture('forward'); state.consumed.push('forward');
  rejects(() => validateLibraryChangedControl(controlFixture('forward'), binding(), state, START + 3000));
});

test('abort permits only a bound cleanup close with a proven restoration state', () => {
  const state = stateFixture('forward'); state.aborted = true;
  validateLibraryChangedControl(controlFixture('close'), binding(), state, START + 3000);
  const empty = { ...state, stages: [], reservation: null, consumed: [] }, control = controlFixture('close');
  Object.assign(control, { previous_stage_sha256: null, reservation: null, commit: null, restoration: 'not_required' });
  validateLibraryChangedControl(control, binding(), empty, START + 3000);
  control.restoration = 'pending'; rejects(() => validateLibraryChangedControl(control, binding(), empty, START + 3000));
  rejects(() => validateLibraryChangedControl(controlFixture('forward'), binding(), state, START + 3000));
  const acknowledged = stateFixture('forward'); acknowledged.aborted = true; acknowledged.consumed.push('forward');
  const omittedRestore = controlFixture('close'); omittedRestore.commit = null; omittedRestore.restoration = 'not_required';
  rejects(() => validateLibraryChangedControl(omittedRestore, binding(), acknowledged, START + 3000));
});

test('controller abort is a bound stop signal and cannot confirm restoration', async () => {
  const input = libraryChangedInputFixture(), bound = binding(input), state = stateFixture('forward');
  const value = { marker: 'goby-client-library-changed-abort-v1', version: 1, ...bound, name: 'forward',
    failure: 'native_acknowledgement_unresolved', previous_stage_sha256: state.stages[0].sha256,
    previous_control_sha256: null, token_sha256: sha(TOKEN), session_private: descriptor(OUTPUT + '/session-private.json') };
  validateLibraryChangedAbort(value, bound, state, { token_sha256: sha(TOKEN) }, value.session_private);
  const wrong = clone(value); wrong.input_sha256 = '0'.repeat(64);
  rejects(() => validateLibraryChangedAbort(wrong, bound, state, { token_sha256: sha(TOKEN) }, value.session_private));
  const report = { login_proof: { token_sha256: sha(TOKEN) }, session_private: value.session_private };
  const flow = new LibraryChangedWorkflow({ input, binding: bound, actor: { started: START }, observer: {}, report,
    now: () => START + 3000, read: async name => name === 'abort'
      ? { publication: 'ready', path: ROOT + '/abort.json', sha256: 'a'.repeat(64), value } : { publication: 'missing' } });
  flow.state = state;
  await rejectsAsync(() => flow.noticeAbort()); check(report.controller_abort && report.restoration === 'pending');
});

test('only complete bounded strict public JSON messages are projected', () => {
  const projected = projectLibraryChangedMessage(messageBytes()); check(projected.MessageType === 'LibraryChanged' && projected.Data.ItemsUpdated[0] === ITEM);
  for (const bytes of [Buffer.from('{"a":1,"a":2}'), Buffer.from('{"a":{"x":1,"x":2}}'), Buffer.from('[1,]'),
    Buffer.from('{"MessageType":"LibraryChanged"'), Buffer.alloc(CHANGED_LIMITS.message_bytes + 1, 32), Buffer.from([0xff])]) {
    rejects(() => projectLibraryChangedMessage(bytes));
  }
  equal(parseLibraryChangedJSON(Buffer.from('{"text":"quote \\"","value":[true,null,1.2]}')).value, [true, null, 1.2]);
  for (const change of [value => { value.MessageId = ''; }, value => { value.Data.ItemsUpdated = [USER]; }, value => { value.Data.ItemsAdded = [ITEM]; },
    value => { value.Data.IsEmpty = true; }, value => { value.Data.Unknown = []; }, value => { value.MessageType = 'GeneralCommand'; }]) {
    const value = JSON.parse(messageBytes().toString()); change(value); rejects(() => projectLibraryChangedMessage(Buffer.from(JSON.stringify(value))));
  }
  rejects(() => parseLibraryChangedJSON(Buffer.from('['.repeat(34) + '0' + ']'.repeat(34))));
});

test('physical catalog scopes are fixed and never export token carriers', () => {
  const input = libraryChangedInputFixture(), url = `http://127.0.0.1:18196/emby/Users/${USER}/Items?ParentId=${LIBRARY}&api_key=${TOKEN}`;
  const selected = libraryChangedCatalogRequest(url, 'GET', input); check(selected.kind === 'items' && !JSON.stringify(selected).includes(TOKEN));
  for (const carrier of ['api_key', 'x-emby-token', 'x-mediabrowser-token']) {
    const observed = libraryChangedCatalogRequest(url.replace('api_key=', carrier + '='), 'GET', input);
    check(observed !== null && !JSON.stringify(observed).includes(TOKEN));
  }
  for (const raw of [url.replace(USER, ITEM), url.replace(LIBRARY, USER), url.replace(':18196', ':18197'), url + '&Limit=999',
    `http://127.0.0.1:18196/emby/Items/${ITEM}/PlaybackInfo?UserId=${USER}`]) check(libraryChangedCatalogRequest(raw, 'GET', input) === null);
  check(libraryChangedCatalogRequest(url, 'POST', input) === null);
  rejects(() => libraryChangedCatalogRequest(url + '&ParentId=' + LIBRARY, 'GET', input));
  rejects(() => libraryChangedCatalogRequest(url + '&Fields=' + 'x'.repeat(4096), 'GET', input));
  const bytes = Buffer.from(JSON.stringify({ Items: [{ Id: ITEM, Name: input.target.name, Type: 'Movie' }], TotalRecordCount: 1 }));
  check(projectLibraryChangedItems(bytes, 'items').target.Name === input.target.name);
  rejects(() => projectLibraryChangedItems(Buffer.from(JSON.stringify({ Items: [] })), 'items'));
  rejects(() => libraryChangedRoute('http://127.0.0.1:18197/web/index.html'));
  rejects(() => libraryChangedRoute('http://127.0.0.1:18196/web/index.html?api_key=' + TOKEN));
});

test('driver and shared transport agree on the single library and exact target scope', () => {
  const input = libraryChangedInputFixture(); input.target.parent_id = USER;
  for (const [route, expected] of [[`/Items/${ITEM}`, true], [`/Items/${ITEM}?UserId=${USER}`, true],
    [`/Items/${LIBRARY}`, true], [`/Users/${USER}/Items/${LIBRARY}`, true], [`/Items?Ids=${LIBRARY}`, false],
    [`/Items?ParentId=${LIBRARY}`, true], [`/Items?Ids=${ITEM}`, true],
    [`/Items?ParentId=${USER}&Ids=${ITEM}`, false], [`/Users/${USER}/Items?ParentId=${USER}`, false]]) {
    const url = 'http://127.0.0.1:18196/emby' + route;
    check(Boolean(libraryChangedCatalogRequest(url, 'GET', input)) === expected);
    check(Boolean(coreCatalogRequest(url, 'GET', USER, 'library-changed-ui-source55-v4')) === expected);
  }
});

test('real ordered forward and restoration evidence pass independently', () => {
  for (const name of ['forward', 'restored']) {
    const result = libraryChangedWindowEvidence(libraryChangedWindowFixture(name), libraryChangedInputFixture(), libraryChangedReservationFixture());
    check(result.result === 'passed' && result.proof.ordered && result.proof.identity_bound);
  }
});

for (const [label, change] of [
  ['no original-client receive', value => { value.events.browser = []; }],
  ['no physical server delivery', value => { value.events.physical = []; }],
  ['HTTP started before the message', value => { value.http.frames[0].request_elapsed_ms = 2800; value.http.frames[0].request_sequence = 2800; }],
  ['HTTP response arrives after the window', value => { value.http.frames[0].finished_elapsed_ms = 122001; }],
  ['only a cached page response', value => { value.http.frames[0].from_service_worker = true; }],
  ['DOM updates without HTTP', value => { value.http.physical = []; value.http.frames = []; }],
  ['stale automatic response', value => { value.http.physical[0].projection.target.Name = 'Old Name'; }],
  ['a different HTTP token', value => { value.http.frames[0].token_sha256 = '0'.repeat(64); }],
  ['an iframe mislabeled as the page', value => { value.http.frames[0].main_frame = false; }],
  ['no visible final card', value => { value.dom.at(-1).observation.passed = false; }],
]) test('incomplete chain is never accepted: ' + label, () => {
  const value = libraryChangedWindowFixture(); change(value);
  check(libraryChangedWindowEvidence(value, libraryChangedInputFixture(), libraryChangedReservationFixture()).result !== 'passed');
});

for (const [label, change] of [
  ['unforwarded server payload', value => { value.events.physical[0].forwarded = false; }],
  ['partial message fragment', value => { value.events.browser[0].complete = false; }],
  ['new handshake', value => { value.lifecycle.push({ kind: 'handshake' }); }],
  ['window click', value => { value.actions.push({ kind: 'click' }); }],
  ['window reload', value => { value.lifecycle.push({ kind: 'document' }); }],
  ['route changed', value => { value.dom[5].observation.route = '/web/index.html#!/other'; }],
  ['document changed', value => { value.events.browser[0].document_id = 'document-3'; }],
  ['new connection identity', value => { value.events.browser[0].connection_id = 'library-changed-ws-1'; }],
  ['foreign event token', value => { value.events.browser[0].token_sha256 = '0'.repeat(64); }],
  ['late event', value => { value.events.browser[0].elapsed_ms = 122001; }],
  ['duplicate event', value => { value.events.browser.push(clone(value.events.browser[0])); }],
  ['missing sample coverage', value => { value.dom.splice(10, 30); }],
  ['tampered projection hash', value => { value.events.browser[0].message.projection_sha256 = '0'.repeat(64); }],
  ['message size overflow', value => { value.events.browser[0].message.body_bytes = CHANGED_LIMITS.message_bytes + 1; }],
]) test('invalid bound evidence fails closed: ' + label, () => {
  const value = libraryChangedWindowFixture(); change(value);
  rejects(() => libraryChangedWindowEvidence(value, libraryChangedInputFixture(), libraryChangedReservationFixture()));
});

test('worker-originated physical reads require an explicit worker identity', () => {
  const value = libraryChangedWindowFixture(), frame = value.http.frames[0];
  Object.assign(frame, { source: 'worker', main_frame: false, worker_id: 'worker-1' });
  check(libraryChangedWindowEvidence(value, libraryChangedInputFixture(), libraryChangedReservationFixture()).result === 'passed');
  frame.worker_id = null;
  check(libraryChangedWindowEvidence(value, libraryChangedInputFixture(), libraryChangedReservationFixture()).result !== 'passed');
});

test('identical concurrent requests cannot share a physical exchange proof', () => {
  const value = libraryChangedWindowFixture(), frames = [value.http.frames[0], { ...clone(value.http.frames[0]), index: 2 }];
  check(pairLibraryChangedReads(value.http.physical, frames).every(pair => !pair.complete));
  rejects(() => pairLibraryChangedReads([value.http.physical[0], clone(value.http.physical[0])], value.http.frames));
});

test('a callback cannot substitute transport counters or partial buffers for protocol identity', () => {
  let ticks = 100;
  const login = { bound: { token_sha256: sha(TOKEN) }, hooks: () => ({}) };
  const observer = new LibraryChangedObserver({ input: libraryChangedInputFixture(), login, now: () => START + ticks, monotonic: () => ticks });
  observer.started = START; observer.origin = 0; observer.route = ROUTE; observer.documentEpoch = 2;
  const value = { connection_id: 'library-changed-ws-0', token_sha256: sha(TOKEN), complete: true, received: true, forwarded: true };
  observer.physicalLibraryChanged(value, messageBytes()); ticks++;
  observer.browserLibraryChanged({ ...value, document_id: 'document-2', route: ROUTE }, messageBytes());
  check(observer.wireMessages.length === 1 && observer.browserMessages.length === 1);
  rejects(() => observer.browserLibraryChanged(value, messageBytes()));
  rejects(() => observer.physicalLibraryChanged({ ...value, complete: false }, messageBytes('partial')));
  rejects(() => observer.physicalLibraryChanged({ ...value, message_id: 3 }, messageBytes('counter')));
  rejects(() => observer.physicalLibraryChanged({ ...value, token_sha256: '0'.repeat(64) }, messageBytes('foreign')));
});

test('page requests must be the actual main frame rather than a sibling frame', async () => {
  const input = libraryChangedInputFixture(), main = {}, other = {}, page = { mainFrame: () => main, url: () => 'http://127.0.0.1:18196' + ROUTE };
  const login = { bound: { token_sha256: sha(TOKEN) }, hooks: () => ({}) };
  const observer = new LibraryChangedObserver({ input, login, now: () => START, monotonic: () => 0 }); observer.started = START; observer.actor = { page, token: TOKEN };
  const request = { url: () => `http://127.0.0.1:18196/emby/Users/${USER}/Items?ParentId=${LIBRARY}&api_key=${TOKEN}`,
    method: () => 'GET', serviceWorker: () => null, frame: () => other, allHeaders: async () => ({}) };
  rejects(() => observer.frameRequest({ request, index: 1 })); request.frame = () => main;
  await observer.frameRequest({ request, index: 2 }); check(observer.frames[0].main_frame === true);
});

test('an armed observer rejects recorded actions and complete state changes', () => {
  const observer = new LibraryChangedObserver({ input: libraryChangedInputFixture(), login: { hooks: () => ({}) } });
  observer.window = {}; rejects(() => observer.noteAction('reload'));
  const complete = completedReportFixture(); check(libraryChangedObservationPassed(complete));
  complete.catalog_observation.observer_failure = 'late-route-change'; check(!libraryChangedObservationPassed(complete));
});

test('a browser-side socket close invalidates the held physical handshake immediately', () => {
  const login = { bound: { token_sha256: sha(TOKEN) }, hooks: () => ({}) };
  const observer = new LibraryChangedObserver({ input: libraryChangedInputFixture(), login });
  const entry = { index: 0, browser_observation_id: 0, browser_handshake_observed: true, upstream_status: 101,
    handshake_delivered: true, closed: false, token_fingerprint: sha(TOKEN), connection_id: 'library-changed-ws-0' };
  observer.actor = { report: { websocket: { active: 1, failed: 0, seen: 1, opened: 1, entries: [entry] } } };
  check(observer.socket().connection_id === entry.connection_id);
  entry.browser_closed = true; rejects(() => observer.socket()); entry.browser_closed = false;
  entry.browser_socket_error = true; rejects(() => observer.socket()); entry.browser_socket_error = false;
  entry.browser_handshake_observed = false; rejects(() => observer.socket());
});

test('visible title evidence belongs to the same card and visible text range', async () => {
  const changed = libraryChangedReservationFixture().marker_name;
  const rectangle = top => ({ top, bottom: top + 30, left: 10, right: 110, width: 100, height: 30 });
  function element(id, top, text = null, textTop = top) {
    const value = { nodeType: 1, tagName: 'DIV', className: 'card', parentElement: null, childNodes: [], box: rectangle(top),
      getAttribute(name) { return name === 'data-id' ? id : name === 'data-type' && id ? 'Movie' : null; },
      getBoundingClientRect() { return this.box; },
      contains(other) { for (let cursor = other; cursor; cursor = cursor.parentElement) if (cursor === this) return true; return false; },
      closest() { for (let cursor = this; cursor; cursor = cursor.parentElement) if (cursor.getAttribute('data-id')) return cursor; return null; } };
    if (text !== null) value.childNodes.push({ nodeType: 3, nodeValue: text, parentElement: value, box: rectangle(textTop) });
    return value;
  }
  const globals = ['document', 'NodeFilter', 'getComputedStyle', 'innerHeight', 'innerWidth'];
  const saved = new Map(globals.map(name => [name, { exists: Object.hasOwn(globalThis, name), value: globalThis[name] }]));
  globalThis.innerHeight = 800; globalThis.innerWidth = 1000; globalThis.NodeFilter = { SHOW_ELEMENT: 1 };
  globalThis.getComputedStyle = () => ({ display: 'block', visibility: 'visible' });
  globalThis.document = {
    createTreeWalker(root) {
      const nodes = [];
      const collect = parent => { for (const child of parent.childNodes) if (child.nodeType === 1) { nodes.push(child); collect(child); } };
      collect(root); let index = 0; return { nextNode: () => nodes[index++] ?? null };
    },
    createRange() { let selected; return { selectNodeContents(node) { selected = node; }, getBoundingClientRect: () => selected.box, detach() {} }; },
  };
  try {
    const visible = element(ITEM, 10), title = element(null, 20, changed); title.parentElement = visible; visible.childNodes.push(title);
    const outside = element(ITEM, 1000, changed), nodes = [visible, outside];
    const page = { url: () => 'http://127.0.0.1:18196' + ROUTE,
      locator: selector => selector === 'audio,video' ? { evaluateAll: async () => false }
        : { count: async () => nodes.length, evaluateAll: async (run, args) => run(nodes, args) } };
    check((await observeLibraryChangedDOM(page, { id: ITEM }, changed, 'Old Name', 'document-2')).passed);
    title.childNodes[0].nodeValue = 'Old Name';
    check(!(await observeLibraryChangedDOM(page, { id: ITEM }, changed, 'Old Name', 'document-2')).passed);
    title.childNodes[0].nodeValue = changed; title.childNodes[0].box = rectangle(1000);
    check(!(await observeLibraryChangedDOM(page, { id: ITEM }, changed, 'Old Name', 'document-2')).passed);
  } finally {
    for (const [name, previous] of saved) {
      if (previous.exists) globalThis[name] = previous.value; else delete globalThis[name];
    }
  }
});

function memoryIO() {
  const files = new Map(); let next = 1;
  const snapshot = file => ({ dev: 1, ino: file.ino, uid: 0, gid: 0, mode: 0o600, size: file.bytes.length,
    nlink: [...files.values()].filter(value => value === file).length, isFile: () => true, isSymbolicLink: () => false });
  return { files, async open(filename, flags) {
    if (filename === OUTPUT) return { async sync() {}, async close() {} };
    if (files.has(filename)) throw Object.assign(new Error('exists'), { code: 'EEXIST' });
    const file = { ino: next++, bytes: Buffer.alloc(0) }; files.set(filename, file);
    return { async writeFile(bytes) { file.bytes = Buffer.from(bytes); }, async sync() {}, async stat() { return snapshot(file); }, async close() {} };
  }, async link(source, target) { check(files.has(source)); if (files.has(target)) throw new Error('exists'); files.set(target, files.get(source)); },
  async lstat(filename) { check(files.has(filename)); return snapshot(files.get(filename)); },
  async unlink(filename) { check(files.has(filename)); files.delete(filename); } };
}

test('failure screenshot publication is exclusive bounded PNG with no JSON encoding', async () => {
  const io = memoryIO(), bytes = Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), Buffer.alloc(16)]), expected = sha(bytes);
  const source = await publishLibraryChangedScreenshot(bytes, io);
  check(source.path === OUTPUT + '/discovery-failure.png' && source.sha256 === expected && source.bytes === 24 && bytes.every(value => value === 0));
  check(io.files.get(source.path).bytes.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10])));
  await rejectsAsync(() => publishLibraryChangedScreenshot(Buffer.from('not-png'), memoryIO()));
  const oversized = Buffer.alloc(CHANGED_DIAGNOSTIC_LIMITS.screenshot_bytes + 1); Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]).copy(oversized);
  await rejectsAsync(() => publishLibraryChangedScreenshot(oversized, memoryIO()));
});

test('exclusive IPC publication rejects partial and foreign ownership states', async () => {
  const owned = { file: true, symbolic: false, uid: 0, gid: 0, mode: 0o600, nlink: 1, size: 12, dev: 1, ino: 1 };
  check(libraryChangedPublication(null, null) === 'missing'); check(libraryChangedPublication(null, owned) === 'publishing');
  check(libraryChangedPublication(owned, null) === 'ready');
  check(libraryChangedPublication({ ...owned, nlink: 2 }, { ...owned, nlink: 2 }) === 'publishing');
  rejects(() => libraryChangedPublication({ ...owned, uid: 1 }, null));
  rejects(() => libraryChangedPublication({ ...owned, nlink: 2 }, { ...owned, ino: 2, nlink: 2 }));
  const io = memoryIO(), source = await publishLibraryChangedRecord(OUTPUT + '/stage-discovery.json', { safe: true }, io);
  check(source.sha256 === sha(io.files.get(source.path).bytes) && !io.files.has(source.path + '.pending'));
  await rejectsAsync(() => publishLibraryChangedRecord(OUTPUT + '/stage-discovery.json', { safe: false }, io));
  await rejectsAsync(() => publishLibraryChangedRecord(ROOT + '/foreign.json', {}, io));
});

test('full reports have a separate bounded budget while IPC remains small', async () => {
  const io = memoryIO(), large = { evidence: 'x'.repeat(CHANGED_LIMITS.record_bytes) };
  await rejectsAsync(() => publishLibraryChangedRecord(OUTPUT + '/stage-armed.json', large, io));
  await publishLibraryChangedRecord(OUTPUT + '/report.json', large, memoryIO());
  await rejectsAsync(() => publishLibraryChangedRecord(OUTPUT + '/report.json', { evidence: 'x'.repeat(CHANGED_LIMITS.report_bytes) }, memoryIO()));
  const forward = libraryChangedWindowFixture(), restored = libraryChangedWindowFixture('restored');
  for (const value of [forward, restored]) {
    const first = value.http.physical[0], frame = value.http.frames[0];
    value.http.physical = Array.from({ length: 20 }, (_, id) => ({ ...clone(first), id, query: [['Fields', 'x'.repeat(3900)]] }));
    value.http.frames = Array.from({ length: 20 }, (_, index) => ({ ...clone(frame), index }));
    value.http.pairs = value.http.physical.map((physical, index) => ({ physical, frame: value.http.frames[index], unambiguous: true, complete: true }));
    check(encodeLibraryChangedRecord({ forward: value, boundary: value.boundary }).length <= CHANGED_LIMITS.record_bytes);
  }
  const report = { forward, restored, actor: {
    requests: Array.from({ length: 2000 }, (_, index) => ({ index, diagnostic: 'x'.repeat(512), route: ROUTE })),
    proxy_requests: Array.from({ length: 2000 }, (_, index) => ({ index, diagnostic: 'x'.repeat(256) })) } };
  check(encodeLibraryChangedRecord(report).length > CHANGED_LIMITS.record_bytes && encodeLibraryChangedRecord(report).length <= CHANGED_LIMITS.report_bytes);
});

test('publication and disposal failures still clear owned in-memory credentials', async () => {
  let encoded, closed = false;
  const io = { async open() { return { async writeFile(bytes) { encoded = bytes; throw new Error('synthetic_write_failure'); },
    async close() { closed = true; } }; } };
  await rejectsAsync(() => publishLibraryChangedRecord(OUTPUT + '/session-private.json', { token: TOKEN }, io));
  check(closed && encoded.length > 0 && encoded.every(byte => byte === 0));
  const secrets = [TOKEN, 'synthetic-password'], credentials = { viewer: { password: 'synthetic-password' } };
  rejects(() => disposeLibraryChangedSecrets({ dispose() { throw new Error('synthetic_disposal_failure'); } }, secrets, credentials));
  check(secrets.every(value => value === null) && credentials.viewer.password === null);
});

test('cancelled or missing controllers cannot leave an unbounded control loop', async () => {
  let current = START, reads = 0;
  const input = libraryChangedInputFixture(), report = {}, actor = { started: START, async assertPinned() {} };
  const flow = new LibraryChangedWorkflow({ input, binding: binding(input), actor, observer: {}, report,
    now: () => current, wait: async milliseconds => { current += milliseconds; }, read: async () => { reads++; return { publication: 'missing' }; } });
  flow.state.stages = stateFixture('reserved').stages;
  await rejectsAsync(() => flow.control('reserved')); check(reads > 0 && reads <= 3000 && current - START <= CHANGED_LIMITS.control_wait_ms + 100);
  flow.state.aborted = true; const before = reads;
  await rejectsAsync(() => flow.control('forward')); check(reads === before);
});

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  const results = [];
  for (const entry of tests) {
    try { await entry.run(); results.push({ name: entry.name, result: 'passed' }); }
    catch (error) { results.push({ name: entry.name, result: 'failed', error: error?.name ?? 'Error' }); }
  }
  const failures = results.filter(value => value.result === 'failed').length;
  process.stdout.write(JSON.stringify({ marker: 'goby-client-library-changed-browser-guards-v1',
    result: failures ? 'failed' : 'passed', tests: results.length, failures, browser: false, network: false, database: false, results }) + '\n');
  if (failures) process.exitCode = 1;
}
