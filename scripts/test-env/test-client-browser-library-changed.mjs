#!/usr/bin/env node
/** Pure source, protocol, IPC and evidence guards; no browser, HTTP or database. */
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { libraryChangedCatalogRequest as coreCatalogRequest } from './client-browser-cross-user.mjs';
import { CHANGED_ROOT as ROOT, CHANGED_OUTPUT as OUTPUT, CHANGED_UNIT, CHANGED_CONTROLLER_UNIT, CHANGED_LIMITS,
  parseLibraryChangedArguments, validateLibraryChangedInput, validateLibraryChangedViewer, validateLibraryChangedBaseline,
  validateLibraryChangedControl, parseLibraryChangedJSON, projectLibraryChangedMessage, libraryChangedCatalogRequest,
  projectLibraryChangedItems, libraryChangedRoute, pairLibraryChangedReads, libraryChangedWindowEvidence,
  libraryChangedPublication, encodeLibraryChangedRecord, publishLibraryChangedRecord, LibraryChangedObserver,
  observeLibraryChangedDOM, libraryChangedStageRecord, LibraryChangedWorkflow, libraryChangedObservationPassed,
  disposeLibraryChangedSecrets, validateLibraryChangedAbort } from './client-browser-library-changed.mjs';

const WORK = '/opt/goby-test/exec-work-m3e', TOOL = WORK + '/client-library-changed-source44-tool-02';
const USER = 'ecbbe4cb82403879bc4b4f78894c5738', ITEM = '268051d3ca734aefcf94e245fb25ad55';
const LIBRARY = 'a9993591e72f0f2e7babcbf8b9c50790';
const TOKEN = 'synthetic-library-changed-token-only', START = Date.parse('2026-09-12T12:00:00.000Z');
const ROUTE = '/web/index.html#!/observed-movies?parentId=' + LIBRARY;
const SELF = fileURLToPath(import.meta.url), sha = value => createHash('sha256').update(value).digest('hex');
const clone = value => JSON.parse(JSON.stringify(value));
const descriptor = (filename, hash = 'a'.repeat(64)) => ({ path: filename, sha256: hash });
const tests = [];
function test(name, run) { tests.push({ name, run }); }
function check(value, detail = 'synthetic_library_changed_assertion') { if (!value) throw new Error(detail); }
function equal(left, right) { check(JSON.stringify(left) === JSON.stringify(right)); }
function rejects(run) { let rejected = false; try { run(); } catch { rejected = true; } check(rejected); }
async function rejectsAsync(run) { let rejected = false; try { await run(); } catch { rejected = true; } check(rejected); }

export function libraryChangedInputFixture() {
  const names = ['client-browser-library-changed.mjs', 'client-library-changed-source44-fixture.mjs', 'client-browser-cross-user.mjs',
    'client-browser-library-home.mjs', 'client-browser-session-proof.mjs', 'client-browser-goby-fixture.mjs', 'client-browser-special-features-fixture.mjs'];
  return { marker: 'goby-client-library-changed-input-v1', version: 1, mode: 'b-movies-name-automatic-refresh', root: ROOT, output: OUTPUT,
    actor: { slot: 'B', user_id: USER, credentials: descriptor(ROOT + '/viewer-credentials.json'), account_key: 'viewer',
      source_credentials_sha256: '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790' },
    candidate: { binary_sha256: 'cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d',
      process: { pid: 1264063, start_ticks: 11104222, boot_id: '6bdfc486-7bc8-412f-82b5-70095a09dde7' },
      runtime_sha256: 'd8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df',
      state_sha256: 'd6b8872eab70848b834c50e26cc2b84e9d137dd68049a1be85d09afa811829d4',
      server_id: 'c7cfd76b1dee728b2bad523793a37ccb', base_url: 'http://127.0.0.1:18196', direct_url: 'http://127.0.0.1:18198',
      source: WORK + '/source-attempt-44', source_manifest_sha256: 'c2c9492589360b058c6533d0719219cf85ab5bc056bd76290cee51e7dc897f8b' },
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
    authority: { continuation_report: descriptor(WORK + '/client-notifications-source44-continuation-v1/report.json'),
      continuation_terminal: descriptor(WORK + '/client-notifications-source44-continuation-execution-01/terminal.json'),
      current_snapshot: descriptor(WORK + '/client-notifications-source44-continuation-v1/after-full.json',
        '738eb9717506bec997456830e70d56f0867f9cf8159939ea3733e039cadb9811'),
      before_snapshot: descriptor(ROOT + '/before-full.json') },
    controller: { pid: 2000000, start_ticks: '12000000', boot_id: '6bdfc486-7bc8-412f-82b5-70095a09dde7', unit: CHANGED_CONTROLLER_UNIT } };
}

function binding(input = libraryChangedInputFixture()) {
  return { input_sha256: '1'.repeat(64), source_closure_sha256: '2'.repeat(64), controller: clone(input.controller),
    node_process: { pid: 2000001, start_ticks: '12000001', boot_id: input.controller.boot_id, uid: 0, gid: 0,
      executable_path: '/synthetic/node', executable_sha256: '3'.repeat(64), cgroup: '/system.slice/' + CHANGED_UNIT } };
}
export function libraryChangedReservationFixture() {
  return { target_id: ITEM, library_id: LIBRARY, revision: '1', original_name: 'M3e Client Movie', marker_name: 'M3e Client Movie [LC source44 v1]',
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
    discovery: { dom: { passed: true }, reads: [{ complete: true }] },
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

test('source44 input binds the exact seven-file new scope', () => {
  validateLibraryChangedInput(libraryChangedInputFixture());
  for (const change of [value => { value.candidate.process.pid = 748513; }, value => { value.candidate.source = WORK + '/source-attempt-32'; },
    value => { value.candidate.state_sha256 = '0'.repeat(64); }, value => { value.authority.current_snapshot.sha256 = '0'.repeat(64); },
    value => { value.actor.credentials.path = WORK + '/browser.json'; }, value => { value.actor.admin = {}; },
    value => { value.source_closure[TOOL + '/unexpected.mjs'] = 'a'.repeat(64); }, value => { delete value.source_closure[TOOL + '/client-browser-goby-fixture.mjs']; },
    value => { value.target.id = USER; }, value => { value.controller.unit = CHANGED_UNIT; }]) {
    const input = libraryChangedInputFixture(); change(input); rejects(() => validateLibraryChangedInput(input));
  }
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
  tables.sessions = Array.from({ length: 73 }, (_, index) => ({ id: String(index).padStart(32, '0'), token_hash: '\\x' + String(index).padStart(64, '0') }));
  tables.devices = Array.from({ length: 62 }, (_, index) => ({ reported_device_id: 'old-device-' + index }));
  for (const [name, count] of [['activity_entries', 163], ['play_sessions', 26], ['user_item_data', 7]]) tables[name] = Array.from({ length: count }, () => ({}));
  tables.libraries = clone(input.expected_libraries); tables.users = [{ id: USER, is_disabled: false, is_administrator: false, management_revision: 5 }];
  tables.items = [{ ...input.target, is_folder: false }, ...Array.from({ length: 21 }, () => ({}))]; tables.item_metadata_state = [{ item_id: ITEM }];
  const current = { schema: 27, database: { tables, sequences: {}, catalog: {}, metadata: { captured_at: new Date(START).toISOString() } } };
  const before = clone(current); before.database.metadata.captured_at = new Date(START + 1000).toISOString();
  check(validateLibraryChangedBaseline(input, before, current).session_ids.length === 73);
  for (const change of [value => { value.database.tables.items[0].name = 'Foreign Name'; }, value => { value.database.tables.devices.pop(); },
    value => { value.database.sequences.unknown = 1; }, value => { value.schema = 28; }]) {
    const altered = clone(before); change(altered); rejects(() => validateLibraryChangedBaseline(input, altered, current));
  }
});

test('stage and control barriers bind exact phase, native receipt and reservation', () => {
  for (const name of ['reserved', 'forward', 'restored', 'close']) validateLibraryChangedControl(controlFixture(name), binding(), stateFixture(name), START + 3000);
  const observed = { home: {}, dom: {}, reads: [], query_allowlist: [], socket: {} };
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
    [`/Items?ParentId=${LIBRARY}`, true], [`/Items?Ids=${ITEM}`, true],
    [`/Items?ParentId=${USER}&Ids=${ITEM}`, false], [`/Users/${USER}/Items?ParentId=${USER}`, false]]) {
    const url = 'http://127.0.0.1:18196/emby' + route;
    check(Boolean(libraryChangedCatalogRequest(url, 'GET', input)) === expected);
    check(Boolean(coreCatalogRequest(url, 'GET', USER)) === expected);
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
