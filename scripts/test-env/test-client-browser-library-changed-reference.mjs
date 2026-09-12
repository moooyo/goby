#!/usr/bin/env node
/** Pure reference protocol guards. Running this file never launches a browser. */
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { REFERENCE_TOOL as TOOL, REFERENCE_ROOT as ROOT, REFERENCE_OUTPUT as OUTPUT, REFERENCE_UNIT,
  REFERENCE_CONTROLLER_UNIT, REFERENCE_USER as USER, REFERENCE_SERVER as SERVER, REFERENCE_LIMITS,
  REFERENCE_PRIOR_RECOVERY, REFERENCE_PRIOR_RECOVERY_REPORT, validateReferencePriorRecovery, readReferencePriorRecovery,
  parseReferenceArguments, validateReferenceInput, validateReferencePublicBaseline, referenceRoute, referenceMoviesRoute,
  referenceFullMovieQuery, projectReferenceItems, pairReferenceReads, referencePairReferences, pairReferenceMessages,
  referenceMessageEvidence, referenceLibraryChangedMessages,
  bindReferenceDOM, referenceWindowEvidence, referenceAcceptedReads, observeReferenceDOM, normalizeReferenceSnapshot, validateReferenceTransportFacts,
  validateReferenceReservation, validateReferenceControl, validateReferenceAbort, referenceStageRecord, encodeReferenceRecord,
  publishReferenceRecord, canonicalReferenceCgroup, projectReferenceNodeProcess, ReferenceWorkflow } from './client-browser-library-changed-reference.mjs';

const SELF = fileURLToPath(import.meta.url), WORK = '/opt/goby-test/exec-work-m3e', ORIGIN = 'http://127.0.0.1:18197';
const ROUTE = '/web/index.html#!/videos?parentId=93&serverId=' + SERVER, START = Date.parse('2026-09-12T12:00:00Z');
const sha = value => createHash('sha256').update(value).digest('hex'), clone = value => JSON.parse(JSON.stringify(value));
const descriptor = filename => ({ path: filename, sha256: sha('synthetic:' + filename) });
const tests = [];
const test = (name, run) => tests.push({ name, run });
function check(value) { if (!value) throw new Error('synthetic_reference_assertion'); }
function rejects(run) { let caught = false; try { run(); } catch { caught = true; } check(caught); }
async function rejectsAsync(run) { let caught = false; try { await run(); } catch { caught = true; } check(caught); }
function equal(left, right) { check(JSON.stringify(left) === JSON.stringify(right)); }
/** Golden safe recovery records; private evidence paths remain opaque and are never opened. */
export function referenceRecoveryFixture() {
  return { terminal: {
  "administrator_logout204_and_same_token401_verified": true,
  "anchor_unchanged": true,
  "captured_at": "2026-09-12T16:07:32.096425+00:00",
  "full_target_restored_except_etag": true,
  "library_changed_client_acceptance": false,
  "main_acceptance": false,
  "marker": "goby-reference-ui-recovery-terminal-v3",
  "media_source_name_restored": true,
  "media_unchanged": true,
  "original_failed_report": {
    "path": "/opt/goby-test/exec-work-m3e/reference-library-changed-ui-v3/report.json",
    "sha256": "a7290d22100b63f919324b7dded7d15228458b9ebf456384b6b6b4de5fa4e23a"
  },
  "original_report_rewritten": false,
  "recursive_cgroup_members": [],
  "report": {
    "path": "/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/report.json",
    "sha256": "3e9a555f0a91e42df63657652a5a2f84b3104db76871a340ffca7001ebf67e3e"
  },
  "restore_posts": 1,
  "restore_status": 204,
  "source": {
    "path": "/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/recover.py",
    "sha256": "b230b22d29a34a154745c1d822ead6e2fcf68084ea78ff889eb0e1196aafea1b"
  },
  "status": "restoration_independently_confirmed",
  "systemd": {
    "ActiveState": "active",
    "ControlGroup": "",
    "ExecMainStatus": "0",
    "Id": "goby-reference-library-changed-ui-recovery-v3b.service",
    "InvocationID": "c7b4694665e841ce98024e61107a1d47",
    "MainPID": "0",
    "RemainAfterExit": "yes",
    "Result": "success",
    "SubState": "exited"
  },
  "unit": "goby-reference-library-changed-ui-recovery-v3b.service",
  "viewer_context_read_authentication": "administrator",
  "viewer_ui_login_performed": false
}, report: {"administrator_closed":true,"captured_at":"2026-09-12T16:03:07.659965+00:00","errors":[],"evidence":{"anchor-after-intent":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/anchor-after-intent.json","sha256":"e134c732bbc92796d551311a5f0ab160545f8a1d73e85b95d9a41b28340eb487"},"anchor-after-result":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/anchor-after-result.json","sha256":"444fc81aaf2f0bbdf8a2207f2389893ea01e3aa70293215844adbd74904e3568"},"anchor-before-intent":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/anchor-before-intent.json","sha256":"e134c732bbc92796d551311a5f0ab160545f8a1d73e85b95d9a41b28340eb487"},"anchor-before-result":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/anchor-before-result.json","sha256":"444fc81aaf2f0bbdf8a2207f2389893ea01e3aa70293215844adbd74904e3568"},"current-intent":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/current-intent.json","sha256":"8aa34f6bb3b50ac75d23452b22020d22184c522613624592c73d9f56a010a562"},"current-result":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/current-result.json","sha256":"61dfbf54b377c252c3935465900db5b80c07e3fb73f00da3e7d499c54ceaf677"},"exact401-intent":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/exact401-intent.json","sha256":"f272501010780ebe89841abbb30237cdc057da6b12b9f643737d1f3f1be9444c"},"exact401-result":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/exact401-result.json","sha256":"101071f78508ba3398fd3a25d1a78b22d19e2b10ee2af2b55f44c0e9fa1ed349"},"login-intent":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/login-intent.json","sha256":"f31046da2f2142161839f9b7da51a3bbf6b648c29e414318724a01674245f95c"},"login-result":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/login-result-private.json","sha256":"78118785d0aa08675deb752d4c340a28949cadc8c578167de94825f446c79bd0"},"logout-intent":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/logout-intent.json","sha256":"01d7fa8f324d2ffca8ec0aaed86a1a93d82939c5dcef4c16c596c45369b0cbe1"},"logout-result":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/logout-result.json","sha256":"9f41d255b56f6c2c35eeda4f670bacf0619065c8d33c332d3d048be0b479d6df"},"ownership":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/ownership.json","sha256":"8b830dcdbbffe55ca0f84449c907ae41b879571408b46d9145f3fc80b71c9bf6"},"restore-intent":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/restore-intent.json","sha256":"909769520e8d755b50a677a4bbdcd05fe0af06f6abc2ad5c2d6bdc90f2bcc6ec"},"restore-result":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/restore-result.json","sha256":"9f41d255b56f6c2c35eeda4f670bacf0619065c8d33c332d3d048be0b479d6df"},"restored-intent":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/restored-intent.json","sha256":"8aa34f6bb3b50ac75d23452b22020d22184c522613624592c73d9f56a010a562"},"restored-result":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/restored-result.json","sha256":"3466cb68b75e071ed281720e17be8189ecc6396a56c8d519eabc1901fbd9c3df"},"viewer-context-intent":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/viewer-context-intent.json","sha256":"a015bb15a1608735be8fd62c04808419799bb72ca732ac64276fdf53139b1a6c"},"viewer-context-result":{"path":"/opt/goby-test/exec-work-m3e/reference-library-changed-ui-recovery-v3b/viewer-context-result.json","sha256":"ecacbb04c8ed85a0dc45dbf41be414e1b9d063edf966206d4e2e33ba58d2356c"}},"http_requests":9,"library_changed_client_acceptance":false,"marker":"goby-reference-ui-name-recovery-v3","media_unchanged":true,"original_scope_replayed":false,"protocol_observation_complete":false,"restoration_confirmed":true,"restore_acknowledged":true,"restore_posts":1,"results":{"anchor-after":{"body_bytes":4740,"body_sha256":"eb7e85a2cd62689fb3eef6260b4b12bfa4f83368ae09be00428f75b24348ed9d","complete":true,"status":200,"token_sha256":"8c1f5d1d11d471242c8d00be3c073ecba5ed6e162a9f3e2b20578e3d62e18f3e"},"anchor-before":{"body_bytes":4740,"body_sha256":"eb7e85a2cd62689fb3eef6260b4b12bfa4f83368ae09be00428f75b24348ed9d","complete":true,"status":200,"token_sha256":"8c1f5d1d11d471242c8d00be3c073ecba5ed6e162a9f3e2b20578e3d62e18f3e"},"current":{"body_bytes":4814,"body_sha256":"152825f9cd3316f15cea578f1dce725ef58ecdd9df24f34f7aff81009dcbd8dd","complete":true,"status":200,"token_sha256":"8c1f5d1d11d471242c8d00be3c073ecba5ed6e162a9f3e2b20578e3d62e18f3e"},"exact401":{"body_bytes":35,"body_sha256":"64f610e896fbad1d2b9561c266036aad3ca7f64ef69effae892749fee5327255","complete":true,"status":401,"token_sha256":"8c1f5d1d11d471242c8d00be3c073ecba5ed6e162a9f3e2b20578e3d62e18f3e"},"login":{"body_bytes":2938,"body_sha256":"e1299144b4ea1680f24889656fa0bc6777ee6a5afa62051955da183992ab3a59","complete":true,"status":200,"token_sha256":null},"logout":{"body_bytes":0,"body_sha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","complete":true,"status":204,"token_sha256":"8c1f5d1d11d471242c8d00be3c073ecba5ed6e162a9f3e2b20578e3d62e18f3e"},"restore":{"body_bytes":0,"body_sha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","complete":true,"status":204,"token_sha256":"8c1f5d1d11d471242c8d00be3c073ecba5ed6e162a9f3e2b20578e3d62e18f3e"},"restored":{"body_bytes":4770,"body_sha256":"cfd2381e12d209decdd7ca89cbfc1f1ca256cd84f69655b2efd499e95593b216","complete":true,"status":200,"token_sha256":"8c1f5d1d11d471242c8d00be3c073ecba5ed6e162a9f3e2b20578e3d62e18f3e"},"viewer-context":{"body_bytes":4752,"body_sha256":"98bac71b0f0afa9d260ae21f62b982e8690d35b9f4a062bf46e6821e3355a55b","complete":true,"status":200,"token_sha256":"8c1f5d1d11d471242c8d00be3c073ecba5ed6e162a9f3e2b20578e3d62e18f3e"}},"status":"restored","viewer_projection_channel":"administrator_token_with_viewer_UserId","viewer_ui_login_performed":false} };
}

export function referenceInputFixture() {
  return { marker: 'goby-reference-library-changed-input-v1', version: 1, mode: 'reference-movies-name-automatic-refresh', root: ROOT, output: OUTPUT,
    actor: { slot: 'B', user_id: USER, username: 'm3e-library-changed-viewer-v1', credentials: descriptor(ROOT + '/viewer-credentials.json'), account_key: 'viewer' },
    reference: { server_id: SERVER, base_url: ORIGIN, service_identity: { pid: 332054, startTicks: '357218', bootId: '12345678-1234-1234-1234-123456789abc',
      uid: 0, exe: '/synthetic/reference', cmdline: ['/synthetic/reference'], networkNamespace: 'net:[4026532602]',
      cgroup: '0::/system.slice/synthetic-reference.service\n', invocationId: sha('synthetic-service').slice(0, 32) } },
    expected_libraries: [{ id: '93', name: 'M3e Controlled LibraryChanged Movies' }],
    target: { id: '100', library_id: '93', parent_id: '99', name: 'LibraryChanged Observed',
      path: '/opt/goby-fixtures/client-library-changed-v1/Movies/LibraryChanged Observed (2031)/LibraryChanged Observed (2031).mp4', type: 'Movie' },
    anchor: { id: '96', name: 'LibraryChanged Anchor',
      path: '/opt/goby-fixtures/client-library-changed-v1/Movies/LibraryChanged Anchor (2030)/LibraryChanged Anchor (2030).mp4', type: 'Movie' },
    source_closure: Object.fromEntries(['client-browser-library-changed-reference.mjs', 'client-browser-library-changed-reference-runtime.mjs',
      'client-browser-session-proof.mjs'].map(name => [TOOL + '/' + name, sha('synthetic-source:' + name)])),
    authority: { owner: descriptor(WORK + '/reference-owner.json'), preflight: descriptor(WORK + '/reference-library-changed-ui-preflight-v4/report.json'),
      before_snapshot: descriptor(ROOT + '/before-public.json'), prior_recovery: clone(REFERENCE_PRIOR_RECOVERY) },
    controller: { pid: 12345, start_ticks: '23456', boot_id: '12345678-1234-1234-1234-123456789abc', unit: REFERENCE_CONTROLLER_UNIT,
      invocation_id: sha('synthetic-controller').slice(0, 32) } };
}
function reservation() { const input = referenceInputFixture(); return { target_id: '100', library_id: '93', original_name: input.target.name,
  marker_name: 'reference library changed ui four', original_body_sha256: sha('original'), forward_body_sha256: sha('forward'), restore_body_sha256: sha('original') }; }
function binding() { const input = referenceInputFixture(); return { input_sha256: sha('input'), source_closure_sha256: sha('sources'), controller: input.controller,
  node_process: projectReferenceNodeProcess({ pid: 12346, start_ticks: '34567', boot_id: input.controller.boot_id, uid: 0, gid: 0, executable_path: '/synthetic/node',
    executable_sha256: sha('node'), cgroup: '0::/system.slice/' + REFERENCE_UNIT + '\n' }, input.controller.boot_id) }; }
function wirePair(phase = 'discovery', name = referenceInputFixture().target.name) {
  const input = referenceInputFixture(), query = { ParentId: '93', IncludeItemTypes: 'Movie', Recursive: 'true', StartIndex: '0', Limit: '50' };
  const body = { Items: [{ Id: '96', Name: input.anchor.name, Type: 'Movie', IsFolder: false, ParentId: '95' },
    { Id: '100', Name: name, Type: 'Movie', IsFolder: false, ParentId: '99' }], TotalRecordCount: 2 };
  const common = { method: 'GET', kind: 'items', route: '/Users/' + USER + '/Items', query, hidden_query: [],
    shape_sha256: sha('shape'), request_sha256: sha('request'), token_sha256: sha('synthetic-token'), phase, document_id: 'document-6', page_route: ROUTE,
    response_elapsed_ms: 4200, finished_elapsed_ms: 4250, sourceworker: false, main_frame: true, from_service_worker: false,
    content_type: 'application/json', status: 200, completed: true, failed: false };
  return { physical: { ...clone(common), id: 'physical-1', index: 1, request_sequence: 4010, start_elapsed_ms: 4010,
    transport_phase: 'completed', error_code: null, request_content_type: null, request_body_sha256: sha(''), upstream_create_attempted: true,
    upstream_created: true, upstream_end_attempted: true, upstream_end_returned: true, upstream_response_received: true,
    projection: projectReferenceItems({ body_sha256: sha(JSON.stringify(body)), bytes: Buffer.byteLength(JSON.stringify(body)), json: body }, 'items') },
  frame: { ...clone(common), id: 'frame-1', index: 1, request_sequence: 4000, start_elapsed_ms: 4000, finished_elapsed_ms: 4260, projection: null,
    transport_phase: null, error_code: null, request_content_type: null, request_body_sha256: null, upstream_create_attempted: null,
    upstream_created: null, upstream_end_attempted: null, upstream_end_returned: null, upstream_response_received: null } };
}
function dom(expected = referenceInputFixture().target.name, observed = expected, started = 4500) {
  const input = referenceInputFixture(); return { route: ROUTE, document_id: 'document-6', started_elapsed_ms: started, expected_name: expected,
    anchor_name: input.anchor.name, observed_title: observed, observed_anchor_title: input.anchor.name, visible_items_containers: 3,
    visible_card_containers: 1, visible_cards: 2, visible_title_buttons: 2, target_title_count: observed === expected ? 1 : 0,
    anchor_title_count: 1, forbidden_title_count: observed === expected ? 0 : 1, explicit_identity_consistent: true,
    structural_match: true, media_inactive: true, target_id: null, anchor_id: null, identity_mode: 'unbound', wire_identity: null, identity_proven: false, passed: false };
}
function discovery() {
  const pair = wirePair(), input = referenceInputFixture(), socket = { connection_id: 'ws-0', token_sha256: sha('synthetic-token'), seen: 1, opened: 1 };
  const observation = bindReferenceDOM(dom(), { phase: 'discovery', input, physical: [pair.physical], frames: [pair.frame], token_sha256: socket.token_sha256, after_sequence: 100 });
  return { home: { route: '/web/index.html#!/home', document_id: 'document-6', library_id: '93', library_name: input.expected_libraries[0].name,
    control_count: 1, identity_proven: true }, dom: observation, reads: [{ ...pair, complete: true, unambiguous: true }],
  query_allowlist: [{ kind: 'items', route: pair.physical.route, query: pair.physical.query, hidden_query: [], shape_sha256: pair.physical.shape_sha256 }],
  socket, navigation: { before_route: '/web/index.html#!/home', after_route: ROUTE, before_sequence: 100 } };
}
function events() {
  const json = { MessageType: 'LibraryChanged', MessageId: sha('message').slice(0, 32), Data: { ItemsUpdated: ['100'], ItemsAdded: [], ItemsRemoved: [],
    CollectionFolders: ['93'], FoldersAddedTo: ['99'], FoldersRemovedFrom: [], IsEmpty: false } }, text = JSON.stringify(json);
  const base = { connection_id: 'ws-0', token_sha256: sha('synthetic-token'), direction: 'server', original_message: true, payload_retained: true,
    body_sha256: sha(text), bytes: Buffer.byteLength(text), json_text: text, json, document_id: 'document-6', page_route: ROUTE, phase: 'forward' };
  return { physical: [{ ...clone(base), sequence: 3000, elapsed_ms: 3000, forwarded: true, forwarded_elapsed_ms: 3002 }],
    browser: [{ ...clone(base), sequence: 3010, elapsed_ms: 3010 }] };
}
export function referenceWindowFixture(name = 'forward', changed = true) {
  const input = referenceInputFixture(), reserve = reservation(), anchor = discovery(), expected = name === 'forward' ? reserve.marker_name : reserve.original_name;
  const beforeName = name === 'forward' ? reserve.original_name : reserve.marker_name, pair = wirePair(name, expected);
  const event = events(); for (const row of [...event.physical, ...event.browser]) row.phase = name;
  const value = { name, control_sha256: sha('control'), commit: { write_completed_at: new Date(START + 2000).toISOString(),
    native_result_sha256: sha('native'), admin_readback_sha256: sha('admin-readback'), viewer_readback_sha256: sha('viewer-readback') },
  boundary: { name, started_sequence: 1990, started_elapsed_ms: 2000, route: ROUTE, document_id: 'document-6', connection_id: 'ws-0',
    token_sha256: sha('synthetic-token'), websocket_seen: 1, websocket_opened: 1, response_completed_elapsed_ms: 2000,
    end_elapsed_ms: 122000, completed_elapsed_ms: 122000, duration_ms: 120000 }, events: event,
  http: { physical: [pair.physical], frames: [pair.frame], pairs: referencePairReferences([pair.physical], [pair.frame]) }, dom: [], actions: [], lifecycle: [] };
  for (let time = 2000; time <= 122000; time += 500) {
    const observed = changed && time >= 4500 ? expected : beforeName, raw = dom(expected, observed, time);
    const bound = bindReferenceDOM(raw, { phase: name, input, discovery: anchor, boundary: value.boundary, token_sha256: value.boundary.token_sha256,
      physical: value.http.physical, frames: value.http.frames, events: value.events });
    value.dom.push({ sequence: time + 1, started_elapsed_ms: time, elapsed_ms: time + 0.1, observation: bound });
  }
  value.dom.at(-1).elapsed_ms = 122000;
  return value;
}

test('input and CLI bind only the fresh reference scope', () => {
  const input = referenceInputFixture(); validateReferenceInput(input);
  parseReferenceArguments(['--input', ROOT + '/input.json', '--input-sha256', sha('input'), '--output', OUTPUT]);
  for (const mutate of [value => { value.reference.base_url = 'http://127.0.0.1:18196'; }, value => { value.root += '-old'; },
    value => { value.actor.user_id = SERVER; }, value => { value.target.id = '98'; }, value => { value.anchor.name = value.target.name; },
    value => { delete value.authority.preflight; }, value => { value.authority.owner.path = WORK + '/other.json'; },
    value => { value.controller.unit = REFERENCE_UNIT; }, value => { value.source_closure[TOOL + '/old-actor.mjs'] = sha('other'); }]) {
    const changed = clone(input); mutate(changed); rejects(() => validateReferenceInput(changed));
  }
  rejects(() => parseReferenceArguments(['--input', ROOT + '/input.json']));
});
test('raw proc cgroup records publish the exact canonical worker path across the IPC boundary', () => {
  const input = referenceInputFixture(), rawLine = '0::/system.slice/goby-reference-library-changed-ui-v4.service\n';
  const proc = { pid: 12346, start_ticks: '34567', boot_id: input.controller.boot_id, uid: 0, gid: 0,
    executable_path: '/synthetic/node', executable_sha256: sha('node'), cgroup: rawLine };
  const projected = projectReferenceNodeProcess(proc, input.controller.boot_id);
  check(projected.cgroup === '/system.slice/goby-reference-library-changed-ui-v4.service' && proc.cgroup === rawLine);
  check(canonicalReferenceCgroup(rawLine.slice(0, -1), REFERENCE_UNIT) === projected.cgroup);
  const actualBinding = { ...binding(), node_process: projected }, session = descriptor(OUTPUT + '/session-private.json');
  const stage = referenceStageRecord(actualBinding, 'discovery', discovery(), sha('synthetic-token'), session, null);
  const decodedStage = JSON.parse(encodeReferenceRecord(stage).toString('utf8'));
  check(decodedStage.node_process.cgroup === '/system.slice/goby-reference-library-changed-ui-v4.service');
  const controllerChild = { pid: 12346, start_ticks: '34567', boot_id: input.controller.boot_id, uid: 0, gid: 0,
    executable_path: '/synthetic/node', executable_sha256: sha('node'), cgroup: '/system.slice/goby-reference-library-changed-ui-v4.service' };
  const control = { marker: 'goby-reference-library-changed-control-v1', version: 1, name: 'reserved', ...actualBinding,
    node_process: controllerChild, previous_stage_sha256: sha('stage'), reservation: reservation(), commit: null, restoration: 'pending' };
  validateReferenceControl(control, actualBinding, { input, previous_stage_sha256: sha('stage') });
  rejects(() => validateReferenceControl({ ...control, node_process: { ...controllerChild, cgroup: rawLine } }, actualBinding,
    { input, previous_stage_sha256: sha('stage') }));
  for (const raw of ['/system.slice/goby-reference-library-changed-ui-v4.service', rawLine.replace('\n', '\r\n'),
    rawLine + '1:name=systemd:/system.slice/other.service\n', rawLine.replace('ui-v4.service', 'ui-v3.service'),
    rawLine.replace('.service\n', '.service/child\n'), rawLine.replace('0::', '1:name=systemd:'),
    '0::/system.slice/goby-reference-library-changed-ui-controller-v4.service\n']) {
    rejects(() => projectReferenceNodeProcess({ ...proc, cgroup: raw }, input.controller.boot_id));
  }
  check(canonicalReferenceCgroup('0::/system.slice/goby-reference-library-changed-ui-controller-v4.service\n', REFERENCE_CONTROLLER_UNIT) ===
    '/system.slice/goby-reference-library-changed-ui-controller-v4.service');
  check(input.reference.service_identity.cgroup === '0::/system.slice/synthetic-reference.service\n');
  for (const mutate of [value => { value.root = WORK + '/reference-library-changed-ui-v3'; },
    value => { value.authority.preflight.path = WORK + '/reference-library-changed-ui-preflight-v3/report.json'; },
    value => { value.source_closure = Object.fromEntries(Object.entries(value.source_closure).map(([filename, hash]) =>
      [filename.replace('ui-tool-04', 'ui-tool-03'), hash])); }]) {
    const stale = clone(input); mutate(stale); rejects(() => validateReferenceInput(stale));
  }
});
test('the fresh public baseline includes both Movies and complete device ownership', () => {
  const input = referenceInputFixture(), movies = [input.anchor, input.target].map(value => ({ Id: value.id, Name: value.name, Type: 'Movie', IsFolder: false, Path: value.path,
    ParentId: value.id === '100' ? '99' : '95' }));
  const preflight = { marker: 'goby-reference-library-changed-preflight-v1', version: 1, mode: 'business-read-only-preflight',
    root: WORK + '/reference-library-changed-ui-preflight-v4', reference: input.reference, script_sha256: sha('controller'), source_closure_sha256: null,
    public_snapshot: descriptor(WORK + '/reference-library-changed-ui-preflight-v4/after-public.json'), prior_recovery: clone(REFERENCE_PRIOR_RECOVERY), target: input.target, anchor: input.anchor,
    expected_libraries: input.expected_libraries, admin: { closed: true, login_status: 200, login_complete: true, logout_status: 204,
      logout_complete: true, exact401_status: 401, exact401_complete: true }, ledger: { authentication_posts: 2, metadata_posts: 0, http_requests: 80 },
    preservation: { passed: true }, errors: [], status: 'passed', completed_at: new Date(START).toISOString(), evidence: {} };
  const before = { marker: 'goby-reference-library-changed-public-snapshot-v1', version: 1, captured_at: new Date(START + 1000).toISOString(),
    credential_context: { channel: 'controller_api', authenticated_user_id: 'efe2137dc3394ed4a23f9c337598f105',
      token_sha256: sha('synthetic-current-admin'), user_id_semantics: 'subject_projection' }, server: { Id: SERVER },
    roster: { [USER]: { Id: USER, Name: input.actor.username, Policy: { IsAdministrator: false, IsDisabled: false } },
      efe2137dc3394ed4a23f9c337598f105: { Id: 'efe2137dc3394ed4a23f9c337598f105', Policy: { IsAdministrator: true, IsDisabled: false } } }, configuration: {},
    libraries: { '93': { Name: input.expected_libraries[0].name } },
    catalog_by_library: { '93': Object.fromEntries(movies.map(value => [value.Id, value])) }, items_by_user: {}, preferences: {},
    details: { viewer: Object.fromEntries(movies.map(value => [value.Id, value])) }, devices: { '11': { Id: '11', ReportedDeviceId: 'old-device' } } };
  const owner = { serverId: SERVER, serviceIdentity: input.reference.service_identity };
  equal(validateReferencePublicBaseline(input, owner, preflight, before).device_ids, ['11', 'old-device']);
  for (const mutate of [value => { delete value.catalog_by_library['93']['96']; }, value => { value.details.viewer['100'].Path = '/foreign'; },
    value => { value.roster[USER].Policy.IsAdministrator = true; }, value => { delete value.credential_context; },
    value => { value.credential_context.authenticated_user_id = USER; }, value => { value.credential_context.channel = 'browser_ui'; },
    value => { value.credential_context.user_id_semantics = 'authenticated_viewer'; }, value => { value.credential_context.token_sha256 = 'unknown'; },
    value => { value.credential_context.extra = true; }, value => { value.roster.efe2137dc3394ed4a23f9c337598f105.Policy.IsAdministrator = false; }]) {
    const copy = clone(before); mutate(copy); rejects(() => validateReferencePublicBaseline(input, owner, preflight, copy));
  }
  const subjectOnly = clone(before);
  subjectOnly.details.viewer['100'].UserData = { IsFavorite: false, Key: 'synthetic-subject-projection' };
  equal(validateReferencePublicBaseline(input, owner, preflight, subjectOnly).device_ids, ['11', 'old-device']);
});
test('the actual recovery records remain closed restoration evidence rather than UI acceptance', () => {
  const actual = referenceRecoveryFixture(); validateReferencePriorRecovery(actual.terminal, actual.report);
  check(actual.terminal.systemd.ControlGroup === '' && actual.terminal.systemd.MainPID === '0' && actual.report.results.login.token_sha256 === null);
  for (const mutate of [value => { value.terminal.version = 1; }, value => { value.report.version = 1; },
    value => { value.terminal.original_report_rewritten = true; }, value => { value.terminal.library_changed_client_acceptance = true; },
    value => { value.terminal.viewer_ui_login_performed = true; }, value => { value.terminal.systemd.ControlGroup = '/system.slice/live.service'; },
    value => { value.terminal.original_failed_report.path = ROOT + '/report.json'; }, value => { value.report.restore_posts = 2; },
    value => { value.report.administrator_closed = false; }, value => { value.report.protocol_observation_complete = true; },
    value => { value.report.viewer_projection_channel = 'actual_ui_token'; }, value => { value.report.results.logout.status = 200; },
    value => { value.report.results.exact401.token_sha256 = sha('synthetic-other-token'); }]) {
    const changed = referenceRecoveryFixture(); mutate(changed); rejects(() => validateReferencePriorRecovery(changed.terminal, changed.report));
  }
});
test('recovery loading pins only its two safe files before any dependent or private read', async () => {
  const input = referenceInputFixture(), actual = referenceRecoveryFixture(), calls = [];
  const read = async item => { calls.push(clone(item));
    if (item.path === REFERENCE_PRIOR_RECOVERY.path) return clone(actual.terminal);
    if (item.path === REFERENCE_PRIOR_RECOVERY_REPORT.path) return clone(actual.report);
    throw new Error('unexpected_private_evidence_read');
  };
  await readReferencePriorRecovery(input, read); equal(calls, [REFERENCE_PRIOR_RECOVERY, REFERENCE_PRIOR_RECOVERY_REPORT]);
  for (const mutate of [value => { delete value.authority.prior_recovery; },
    value => { value.authority.prior_recovery.path = ROOT + '/report.json'; }, value => { value.authority.prior_recovery.sha256 = sha('synthetic-foreign-terminal'); }]) {
    const changed = referenceInputFixture(); mutate(changed); calls.length = 0;
    await rejectsAsync(() => readReferencePriorRecovery(changed, read)); check(calls.length === 0);
  }
  let reads = 0;
  await rejectsAsync(() => readReferencePriorRecovery(input, async () => { reads++; const terminal = clone(actual.terminal);
    terminal.report.path = ROOT + '/viewer-credentials.json'; return terminal; }));
  check(reads === 1);
});
test('only the exact Movie library query proves the two-item anchor', () => {
  const pair = wirePair(); check(referenceFullMovieQuery(pair.physical));
  for (const [key, value] of [['ParentId', '99'], ['IncludeItemTypes', 'Folder'], ['Limit', '1'], ['StartIndex', '1'], ['Ids', '100']]) {
    const copy = clone(pair.physical); copy.query[key] = value; check(!referenceFullMovieQuery(copy));
  }
  check(referenceMoviesRoute(ROUTE)); check(!referenceMoviesRoute(ROUTE.replace('parentId=93', 'parentId=99')));
  rejects(() => referenceRoute(ORIGIN + ROUTE + '&api_key=not-public'));
});
test('raw DTO projection preserves both real identities without inventing a parent', () => {
  const response = { json: { Items: [{ Id: '100', Name: 'Observed', Type: 'Movie' }, { Id: '96', Name: 'Anchor', Type: 'Movie' }] }, body_sha256: sha('body'), bytes: 64 };
  const result = projectReferenceItems(response, 'items'); check(result.count === 2 && result.items[0].ParentId === null && result.items[0].IsFolder === false);
  rejects(() => projectReferenceItems({ ...response, bytes: REFERENCE_LIMITS.json_bytes + 1 }, 'items'));
});
test('pairing requires complete uncached one-to-one wire evidence', () => {
  const pair = wirePair(); check(pairReferenceReads([pair.physical], [pair.frame])[0].complete);
  for (const mutate of [value => { value.failed = true; }, value => { value.completed = false; }, value => { value.status = 304; },
    value => { value.from_service_worker = true; }, value => { value.content_type = 'text/html'; }]) {
    const frame = clone(pair.frame); mutate(frame); check(pairReferenceReads([pair.physical], [frame]).length === 0);
  }
  const extra = { ...clone(pair.physical), id: 'physical-2', index: 2 };
  check(pairReferenceReads([pair.physical, extra], [pair.frame])[0].physical === null);
});
test('message evidence keeps parent arrays and requires exact original bytes and identity', () => {
  const window = referenceWindowFixture(); check(pairReferenceMessages(window.events, window.boundary).length === 1);
  for (const mutate of [value => { value.json.MessageId = null; }, value => { value.json.Data.ItemsUpdated = ['98']; },
    value => { value.body_sha256 = sha('foreign'); }, value => { value.json_text += ' '; }, value => { value.original_message = false; }]) {
    const events = clone(window.events); mutate(events.browser[0]); check(pairReferenceMessages(events, window.boundary).length === 0);
  }
});
test('discovery requires both Movie DTOs and never proves a card by title alone', () => {
  const input = referenceInputFixture(), pair = wirePair(), raw = dom();
  check(raw.target_id === null && !raw.identity_proven); check(discovery().dom.passed);
  for (const mutate of [value => { value.projection.items.pop(); value.projection.count = 1; }, value => { value.projection.items[0].Id = '97'; },
    value => { value.projection.items[0].Name = input.target.name; }]) {
    const wire = clone(pair.physical); mutate(wire); check(!bindReferenceDOM(raw, { phase: 'discovery', input, physical: [wire], frames: [pair.frame],
      token_sha256: pair.frame.token_sha256, after_sequence: 100 }).passed);
  }
});
test('forward and restoration observe independent real transitions', () => {
  for (const name of ['forward', 'restored']) { const result = referenceWindowEvidence(referenceWindowFixture(name), referenceInputFixture(), reservation(), discovery());
    check(result.result === 'changed' && result.status.visible_transition_observed && result.notification_context.parent_context); }
  const incomplete = referenceWindowFixture(); incomplete.dom.at(-1).observation.passed = false;
  check(referenceWindowEvidence(incomplete, referenceInputFixture(), reservation(), discovery()).result === 'not_observed');
});
test('missing refresh remains a complete negative observation rather than fabricated success', () => {
  const value = referenceWindowFixture('forward', false); value.http = { physical: [], frames: [], pairs: [] };
  const result = referenceWindowEvidence(value, referenceInputFixture(), reservation(), discovery());
  check(result.result === 'not_observed' && result.status.message_observed && !result.status.http_observed && !result.status.visible_transition_observed);
});
test('a restored page already showing the original name does not prove a transition', () => {
  const value = referenceWindowFixture('restored');
  for (const sample of value.dom) sample.observation = dom(reservation().original_name, reservation().original_name, sample.started_elapsed_ms);
  const result = referenceWindowEvidence(value, referenceInputFixture(), reservation(), discovery());
  check(result.result === 'not_observed' && result.status.dom_matches_expected && !result.status.visible_transition_observed);
});
test('HTTP and DOM observations are reported independently', () => {
  const value = referenceWindowFixture('forward', false), result = referenceWindowEvidence(value, referenceInputFixture(), reservation(), discovery());
  check(result.status.http_observed && !result.status.dom_matches_expected && result.result === 'not_observed');
});
test('old-phase and pre-message requests cannot stand in for automatic HTTP', () => {
  for (const mutate of [value => { value.phase = 'discovery'; }, value => { value.request_sequence = 2500; }, value => { value.start_elapsed_ms = 2990; },
    value => { value.token_sha256 = sha('foreign'); }, value => { value.projection.items[1].Name = reservation().original_name; }]) {
    const window = referenceWindowFixture(); mutate(window.http.physical[0]); check(referenceAcceptedReads(window, referenceInputFixture(), reservation().marker_name, discovery()).length === 0);
  }
});
test('compact pair references are independently recalculated', () => {
  const value = referenceWindowFixture(); value.http.pairs[0].physical_exchange_id = 'physical-999';
  rejects(() => referenceWindowEvidence(value, referenceInputFixture(), reservation(), discovery()));
});
test('an actual same-token target GET may refresh the proved two-Movie page', () => {
  const value = referenceWindowFixture(), pair = value.http;
  for (const row of [pair.physical[0], pair.frames[0]]) { row.kind = 'target'; row.route = '/Users/' + USER + '/Items/100'; }
  pair.physical[0].projection.items = [pair.physical[0].projection.items[1]]; pair.physical[0].projection.count = 1;
  pair.pairs = referencePairReferences(pair.physical, pair.frames);
  check(referenceAcceptedReads(value, referenceInputFixture(), reservation().marker_name, discovery()).length === 1);
});
test('control and stage envelopes keep one exact chain without DB fields', () => {
  const reserve = reservation(), bound = binding(); validateReferenceReservation(reserve, referenceInputFixture());
  const control = { marker: 'goby-reference-library-changed-control-v1', version: 1, name: 'reserved', ...bound,
    previous_stage_sha256: sha('stage'), reservation: reserve, commit: null, restoration: 'pending' };
  validateReferenceControl(control, bound, { input: referenceInputFixture(), previous_stage_sha256: sha('stage') });
  const stage = referenceStageRecord(bound, 'discovery', discovery(), sha('synthetic-token'), descriptor(OUTPUT + '/session-private.json'), null);
  check(stage.marker === 'goby-reference-library-changed-stage-v1' && stage.previous_control_sha256 === null);
  rejects(() => validateReferenceControl({ ...control, revision: '1' }, bound, { input: referenceInputFixture(), previous_stage_sha256: sha('stage') }));
});
test('raw HTTP normalization is bounded public projection only', () => {
  const pair = wirePair(), wire = { ...pair.physical, kind: 'read', response: { body_sha256: sha('body'), bytes: 128,
    json: { Items: pair.physical.projection.items, Path: '/private-not-exported' } } };
  const result = normalizeReferenceSnapshot({ http: { physical: [wire], frames: [pair.frame] }, events: { physical: [], browser: [] }, report: { private: 'discarded' } });
  check(result.http.physical[0].kind === 'items' && result.http.physical[0].projection.count === 2 && !JSON.stringify(result).includes('private-not-exported') && !('report' in result));
});
test('blocked physical transfers retain their real terminal failure and byte facts', () => {
  const pair = wirePair(), blocked = { ...pair.physical, status: null, response_elapsed_ms: null, completed: false, outcome: 'rejected', reason: 'http_admission_rejected', request_bytes: 0, response_bytes: 0,
    transport_phase: 'admission', error_code: 'admission_rejected', request_body_sha256: null, upstream_create_attempted: false,
    upstream_created: false, upstream_end_attempted: false, upstream_end_returned: false, upstream_response_received: false };
  delete blocked.failed;
  const result = normalizeReferenceSnapshot({ http: { physical: [blocked], frames: [] }, events: { physical: [], browser: [] } }).http.physical[0];
  check(result.failed === true && result.outcome === 'rejected' && result.reason === 'http_admission_rejected' && result.request_bytes === 0 && result.response_bytes === 0);
});
test('physical dispatch facts survive zero-response normalization without inventing non-dispatch', () => {
  const pair = wirePair(), body = 'Username=synthetic-viewer&Pw=synthetic-only-password';
  const failed = { ...pair.physical, method: 'POST', kind: 'login', route: '/Users/AuthenticateByName', status: null, completed: false,
    outcome: 'failed', failed: true, reason: 'http_upstream_idle', response_bytes: 0, request_bytes: Buffer.byteLength(body),
    transport_phase: 'upstream_end', error_code: 'upstream_idle_timeout', request_content_type: 'application/x-www-form-urlencoded; charset=UTF-8',
    request_body_sha256: sha(body), upstream_create_attempted: true, upstream_created: true, upstream_end_attempted: true,
    upstream_end_returned: true, upstream_response_received: false };
  const snapshot = { http: { physical: [failed], frames: [pair.frame] }, events: { physical: [], browser: [] } };
  const normalized = normalizeReferenceSnapshot(snapshot), actual = normalized.http.physical[0];
  check(Object.keys(actual).length === 38 && actual.status === null && actual.response_bytes === 0 && actual.failed &&
    actual.upstream_create_attempted && actual.upstream_created && actual.upstream_end_attempted && actual.upstream_end_returned &&
    actual.upstream_response_received === false && actual.transport_phase === 'upstream_end' && actual.error_code === 'upstream_idle_timeout' &&
    actual.request_body_sha256 === sha(body) && actual.request_content_type === 'application/x-www-form-urlencoded; charset=UTF-8');
  for (const key of ['transport_phase', 'error_code', 'request_content_type', 'request_body_sha256', 'upstream_create_attempted',
    'upstream_created', 'upstream_end_attempted', 'upstream_end_returned', 'upstream_response_received']) check(normalized.http.frames[0][key] === null);
  check(!JSON.stringify(normalized).includes('synthetic-only-password'));
  const pending = { ...actual, failed: false, outcome: null, error_code: null, reason: null }; validateReferenceTransportFacts(pending, true);
  check(pending.upstream_end_returned === true && pending.upstream_response_received === false && pending.status === null);
});
test('transport fact guards reject contradictory channels, stages, and local call ordering', () => {
  const pair = wirePair(); validateReferenceTransportFacts(pair.physical, true); validateReferenceTransportFacts(pair.frame, false);
  for (const mutate of [value => { value.upstream_create_attempted = false; }, value => { value.upstream_end_attempted = false; },
    value => { value.upstream_end_returned = 'true'; }, value => { value.request_body_sha256 = null; },
    value => { value.error_code = 'raw exception with a secret'; }, value => { value.transport_phase = 'unknown'; },
    value => { value.request_content_type = 'arbitrary/private'; }, value => { value.completed = false; }]) {
    const changed = clone(pair.physical); mutate(changed); rejects(() => validateReferenceTransportFacts(changed, true));
  }
  rejects(() => validateReferenceTransportFacts({ ...pair.frame, upstream_created: false }, false));
  rejects(() => validateReferenceTransportFacts({ ...pair.physical, failed: true }, true));
  const callbackBeforeReturn = { ...pair.physical, transport_phase: 'upstream_response', completed: false, failed: false,
    upstream_create_attempted: true, upstream_created: false, upstream_end_attempted: false, upstream_end_returned: false, upstream_response_received: true };
  validateReferenceTransportFacts(callbackBeforeReturn, true);
});
test('unsupported LibraryChanged schema is received and paired without fabricated empty Data', () => {
  const value = referenceWindowFixture();
  for (const row of [...value.events.physical, ...value.events.browser]) {
    delete row.json.Data; row.json_text = JSON.stringify(row.json); row.body_sha256 = sha(row.json_text); row.bytes = Buffer.byteLength(row.json_text);
  }
  const evidence = referenceMessageEvidence(value.events, value.boundary);
  check(evidence.received_count === 1 && evidence.transport_paired_count === 1 && evidence.qualified_count === 0 && evidence.unsupported_schema_count === 1 &&
    evidence.reasons.includes('data_shape_not_proven') && !Object.hasOwn(value.events.browser[0].json, 'Data'));
});
function subscription(time, phase = 'forward') {
  const json = { MessageType: 'SessionsStart', Data: '1000,1000' }, json_text = JSON.stringify(json);
  return { sequence: time + 20, elapsed_ms: time + 20, connection_id: 'ws-0', socket_id: 'ws-0', token_sha256: sha('synthetic-token'),
    direction: 'client', original_message: true, payload_retained: true, body_sha256: sha(json_text), bytes: Buffer.byteLength(json_text), json_text, json,
    phase, document_id: 'document-6', page_route: ROUTE, forwarded: true, forwarded_elapsed_ms: time + 21 };
}
test('periodic subscriptions coexist with LibraryChanged in the production window and bounded reports', () => {
  const input = referenceInputFixture(), reserve = reservation(), anchor = discovery(), windows = ['forward', 'restored'].map(name => referenceWindowFixture(name));
  for (const window of windows) {
    for (let time = 2000; time < 122000; time += 1000) for (const rows of Object.values(window.events)) rows.push(subscription(time, window.name));
    const result = referenceWindowEvidence(window, input, reserve, anchor); Object.assign(window, result);
    check(result.result === 'changed' && result.message_evidence.received_count === 1 && window.events.browser.length === 121 &&
      referenceLibraryChangedMessages(window.events).browser.length === 1);
    check(encodeReferenceRecord({ forward: window, boundary: window.boundary }).length <= REFERENCE_LIMITS.record_bytes);
  }
  const all = Array.from({ length: 900 }, (_, index) => subscription(index * 1000));
  const full = { forward: windows[0], restored: windows[1], observations: { events: { physical: all, browser: clone(all) } } };
  check(encodeReferenceRecord(full).length <= REFERENCE_LIMITS.report_bytes);
});
test('the production quiet interval retains periodic subscriptions and background GETs', async () => {
  let elapsed = 0, sequence = 1;
  const snapshot = () => {
    const count = Math.floor(elapsed / 1000), rows = Array.from({ length: count }, (_, index) => ({ id: 'background-' + index, index,
      kind: 'read', method: 'GET', route: '/System/Ping', query: {}, hidden_query: [], request_sequence: index * 10 + 2,
      start_elapsed_ms: index * 1000 + 20, completed: true, failed: false }));
    return { sequence, elapsed_ms: elapsed, document_id: 'document-6', route: ROUTE,
      socket: { connection_id: 'ws-0', token_sha256: sha('synthetic-token'), seen: 1, opened: 1 },
      http: { physical: rows, frames: clone(rows) }, events: { physical: Array.from({ length: count }, (_, index) => subscription(index * 1000, 'discovery')),
        browser: Array.from({ length: count }, (_, index) => subscription(index * 1000, 'discovery')) }, lifecycle: [] };
  };
  const flow = Object.create(ReferenceWorkflow.prototype);
  Object.assign(flow, { reservation: reservation(), actor: { elapsed: () => elapsed, stamp: () => ({ sequence: ++sequence, elapsed_ms: elapsed }) },
    stable: async () => snapshot(), snapshot, capture: async () => ({ passed: true }), wait: async duration => { elapsed += duration; } });
  const result = await flow.quiet();
  check(result.duration_ms === 75000 && result.completed_elapsed_ms === 75000 && result.samples.length === 150 &&
    result.catalog_requests === 0 && result.library_changed_messages === 0 && result.http.physical.length === 75 && result.events.browser.length === 75);
});
test('the production DOM callback keeps two identities, empty owners, orphan and clip boundaries', async () => {
  const input = referenceInputFixture(), names = ['document', 'NodeFilter', 'getComputedStyle', 'innerWidth', 'innerHeight'];
  const saved = names.map(name => [name, Object.getOwnPropertyDescriptor(globalThis, name)]);
  const box = (width = 100, height = 100) => ({ x: 10, y: 10, left: 10, top: 10, right: 10 + width, bottom: 10 + height, width, height });
  const element = (classes, parent = null, title = null) => ({ parentElement: parent, classList: classes.split(' '), tagName: 'DIV', attrs: {}, rect: box(), title,
    getBoundingClientRect() { return this.rect; }, getAttribute(name) { return this.attrs[name] ?? null; },
    closest(selector) { return this.classList.includes(selector.slice(1)) ? this : this.parentElement?.closest(selector) ?? null; } });
  const containers = [element('itemsContainer'), element('itemsContainer'), element('itemsContainer')], cards = [], buttons = [];
  function addCard(title, owner = containers[0]) {
    const card = element('card', owner), cardBox = element('cardBox', card), text = element('cardText', cardBox), button = element('cardTextActionButton', text, title);
    button.tagName = 'BUTTON'; cards.push(card); buttons.push(button); return { card, button };
  }
  const target = addCard(input.target.name); addCard(input.anchor.name);
  try {
    globalThis.innerWidth = 1000; globalThis.innerHeight = 1000; globalThis.NodeFilter = { SHOW_TEXT: 4 };
    globalThis.getComputedStyle = node => ({ display: 'block', visibility: 'visible', overflowX: node.overflow ?? 'visible', overflowY: node.overflow ?? 'visible' });
    globalThis.document = { querySelectorAll: selector => selector === '.card' ? cards : buttons,
      createTreeWalker(node) { let visited = false; return { nextNode() { if (visited) return null; visited = true; return { nodeValue: node.title, parentElement: node }; } }; },
      createRange() { let node; return { selectNodeContents(value) { node = value; }, getBoundingClientRect() { return node.parentElement.rect; }, detach() {} }; } };
    const page = { url: () => ORIGIN + ROUTE, locator: selector => ({ async evaluateAll(callback, value) { return callback(selector === '.itemsContainer' ? containers : [], value); } }) };
    const capture = () => observeReferenceDOM(page, input, input.target.name, null, 'document-6');
    const first = await capture(); check(first.structural_match && first.visible_items_containers === 3 && first.visible_card_containers === 1 && first.visible_cards === 2 &&
      first.target_title_count === 1 && first.anchor_title_count === 1 && first.target_id === null && !first.passed);
    containers[0].rect = box(0, 0); check((await capture()).structural_match);
    containers[0].overflow = 'hidden'; const clipped = await capture(); check(!clipped.structural_match && clipped.visible_cards === 0);
    containers[0].overflow = 'visible'; containers[0].rect = box();
    target.card.attrs['data-id'] = '999'; check(!(await capture()).structural_match); delete target.card.attrs['data-id'];
    const orphan = element('card'); cards.push(orphan); check(!(await capture()).structural_match); cards.pop();
    target.button.title = input.anchor.name; check(!(await capture()).structural_match); target.button.title = input.target.name;
    const extra = element('cardTextActionButton', containers[1], input.target.name); buttons.push(extra);
    check(!(await capture()).structural_match); buttons.pop();
  } finally { for (const [name, descriptor] of saved) { if (descriptor) Object.defineProperty(globalThis, name, descriptor); else delete globalThis[name]; } }
});
test('publication remains exclusive with compact bounded records', async () => {
  const calls = [], handle = { async writeFile(bytes) { calls.push(['write', bytes.length]); }, async sync() {}, async close() {} };
  const io = { async open(filename, flags, mode) { calls.push(['open', filename, flags, mode]); return handle; },
    async link(from, to) { calls.push(['link', from, to]); }, async unlink(filename) { calls.push(['unlink', filename]); } };
  await publishReferenceRecord(OUTPUT + '/stage-armed.json', { quiet: {} }, io); check(calls[0][3] === 0o600);
  await rejectsAsync(() => publishReferenceRecord(ROOT + '/report.json', {}, io));
  check(encodeReferenceRecord({ value: 'x' }).toString() === '{"value":"x"}\n');
});
test('the protocol continues both windows when forward reports non-observation', async () => {
  const order = [], flow = Object.create(ReferenceWorkflow.prototype);
  Object.assign(flow, { report: {}, discover: async () => order.push('discovery'), control: async name => { order.push(name); return {}; },
    quiet: async () => ({}), arm: async name => ({ name }), stage: async name => order.push('stage:' + name),
    finish: async name => { order.push('finish:' + name); return { result: 'not_observed' }; }, sample: async () => {} });
  await flow.execute(); check(flow.report.protocol_observation_complete && order.includes('stage:restore-armed') && order.includes('finish:restored') && order.at(-1) === 'close');
});
test('a bound controller abort interrupts a control wait before its timeout', async () => {
  const bound = binding(), session = descriptor(OUTPUT + '/session-private.json'), stage = { sha256: sha('stage') }, control = { sha256: sha('control') };
  const abort = { marker: 'goby-reference-library-changed-abort-v1', version: 1, ...bound, name: 'forward', failure: 'reference_controller_failed',
    previous_stage_sha256: stage.sha256, previous_control_sha256: control.sha256, token_sha256: sha('synthetic-token'), session_private: session };
  const state = { stages: [stage], controls: [control], token_sha256: abort.token_sha256, session_private: session };
  validateReferenceAbort(abort, bound, state); rejects(() => validateReferenceAbort({ ...abort, input_sha256: sha('foreign') }, bound, state));
  let reads = 0; const flow = Object.create(ReferenceWorkflow.prototype);
  Object.assign(flow, { binding: bound, report: { stages: [stage], controls: [control], login_proof: { token_sha256: abort.token_sha256 } },
    session: () => session, active() {}, now: () => 0, deadline: 90000,
    readAbort: async () => ({ path: ROOT + '/abort.json', sha256: sha('abort'), value: abort }), readControl: async () => { reads++; return null; } });
  await rejectsAsync(() => flow.control('forward')); check(reads === 0 && flow.report.controller_abort.sha256 === sha('abort'));
});

/** The explicit DOM mode is an isolated synthetic fixture, never a reference client run. */
export async function runReferenceDOMGuards(argv) {
  const domTool = WORK + '/reference-library-changed-ui-js-tool-04';
  check(process.platform === 'linux' && process.getuid?.() === 0 && SELF === domTool + '/test-client-browser-library-changed-reference.mjs');
  check(Array.isArray(argv) && argv.length === 9 && argv[0] === '--dom'); const values = {};
  for (let index = 1; index < argv.length; index += 2) {
    check(['--driver-sha256', '--runtime-sha256', '--session-proof-sha256', '--outer-netns'].includes(argv[index]) && !Object.hasOwn(values, argv[index]));
    values[argv[index]] = argv[index + 1];
  }
  check(/^net:\[\d+\]$/.test(values['--outer-netns']));
  const fs = await import('node:fs/promises'), ownNS = await fs.readlink('/proc/self/ns/net');
  check(ownNS !== values['--outer-netns'] && ownNS !== await fs.readlink('/proc/1/ns/net'));
  const routes = (await fs.readFile('/proc/self/net/route', 'utf8')).trim().split(/\r?\n/);
  const devices = (await fs.readFile('/proc/self/net/dev', 'utf8')).trim().split(/\r?\n/).slice(2).map(line => line.split(':')[0].trim());
  check(routes.length === 1 && devices.every(name => name === 'lo'));
  for (const [key, name] of [['--driver-sha256', 'client-browser-library-changed-reference.mjs'],
    ['--runtime-sha256', 'client-browser-library-changed-reference-runtime.mjs'], ['--session-proof-sha256', 'client-browser-session-proof.mjs']]) {
    check(/^[0-9a-f]{64}$/.test(values[key])); const filename = domTool + '/' + name, info = await fs.lstat(filename);
    check(info.isFile() && !info.isSymbolicLink() && info.uid === 0 && info.nlink === 1 && [0o600, 0o644].includes(info.mode & 0o777));
    const bytes = await fs.readFile(filename); try { check(sha(bytes) === values[key]); } finally { bytes.fill(0); }
  }
  const { createRequire } = await import('node:module');
  const { chromium } = createRequire(import.meta.url)('/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright');
  const input = referenceInputFixture(), escape = value => value.replace(/[&<>"']/g, character => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[character]);
  const button = name => '<button class="cardTextActionButton">' + escape(name) + '</button>';
  const card = (name, extra = '', id = '') => '<div class="card"' + (id ? ' data-id="' + id + '"' : '') + '><div class="cardBox"><div class="cardText">' +
    (name === null ? '' : button(name)) + extra + '</div></div></div>';
  const target = input.target.name, anchor = input.anchor.name, marker = reservation().marker_name;
  const container = (content = '', classes = '') => '<div class="itemsContainer ' + classes + '">' + content + '</div>';
  const pageHTML = content => '<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0} .itemsContainer{width:1000px;height:24px}' +
    '.itemsContainer.owned{height:240px}.card{display:inline-block;vertical-align:top;width:240px;height:220px}.cardBox{width:240px;height:220px}' +
    '.cardText{width:230px;min-height:40px}button{display:block;width:220px;height:36px}.itemsContainer.zero{width:0;height:0;overflow:visible}' +
    '.itemsContainer.zero.clipped{overflow:hidden}</style></head><body>' + content + '</body></html>';
  const standard = (name = target, extra = '', classes = 'owned') => container() + container() + container(card(name) + card(anchor) + extra, classes);
  const cases = [
    { name: 'three containers with one two-card owner', html: standard(), expected: target, match: true, containers: 3, cards: 2, buttons: 2 },
    { name: 'the exact target rename', html: standard(marker), expected: marker, match: true, containers: 3, cards: 2, buttons: 2 },
    { name: 'the exact target restoration', html: standard(), expected: target, match: true, containers: 3, cards: 2, buttons: 2 },
    { name: 'zero-sized overflow-visible owner', html: standard(target, '', 'owned zero'), expected: target, match: true, containers: 2, cards: 2, buttons: 2 },
    { name: 'zero-sized clipped owner', html: standard(target, '', 'owned zero clipped'), expected: target, match: false, containers: 2, cards: 0, buttons: 0, clipped: true },
    { name: 'an extra visible Movie card', html: standard(target, card('Third Movie')), expected: target, match: false, containers: 3, cards: 3, buttons: 3 },
    { name: 'duplicate exact target cards', html: standard(target, card(target)), expected: target, match: false, containers: 3, cards: 3, buttons: 3 },
    { name: 'a duplicate title inside a card', html: container() + container() + container(card(target, button(target)) + card(anchor), 'owned'), expected: target,
      match: false, containers: 3, cards: 2, buttons: 3 },
    { name: 'an orphan card remains globally counted', html: standard() + '<div class="card"></div>', expected: target, match: false, containers: 3, cards: 3, buttons: 2 },
    { name: 'an orphan title remains globally counted', html: standard() + button(target), expected: target, match: false, containers: 3, cards: 2, buttons: 3 },
    { name: 'a title in another container cannot bind the card', html: container(button(target)) + container() + container(card(null) + card(anchor), 'owned'),
      expected: target, match: false, containers: 3, cards: 2, buttons: 2 },
    { name: 'an explicit foreign target identity', html: container() + container() + container(card(target, '', '999') + card(anchor), 'owned'),
      expected: target, match: false, containers: 3, cards: 2, buttons: 2 },
    { name: 'duplicate anchor titles cannot invent a target', html: container() + container() + container(card(anchor) + card(anchor), 'owned'),
      expected: target, match: false, containers: 3, cards: 2, buttons: 2 },
  ];
  const results = []; let browser;
  try {
    browser = await chromium.launch({ headless: true, timeout: 30000, args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking'] });
    for (const entry of cases) {
      let context, unexpected = 0, documents = 0;
      try {
        context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, serviceWorkers: 'block' });
        await context.route('**/*', async route => {
          const request = route.request();
          if (request.method() === 'GET' && request.resourceType() === 'document' && request.url() === ORIGIN + '/web/index.html') {
            documents++; await route.fulfill({ status: 200, contentType: 'text/html', body: pageHTML(entry.html) });
          } else { unexpected++; await route.abort(); }
        });
        const page = await context.newPage(); await page.goto(ORIGIN + ROUTE, { waitUntil: 'domcontentloaded', timeout: 15000 });
        if (entry.clipped) { const bounds = await page.locator('.card').first().boundingBox(); check(bounds.width > 0 && bounds.height > 0); }
        const actual = await observeReferenceDOM(page, input, entry.expected, null, 'document-fixture');
        check(actual.structural_match === entry.match && actual.visible_items_containers === entry.containers && actual.visible_cards === entry.cards &&
          actual.visible_title_buttons === entry.buttons && actual.target_id === null && actual.anchor_id === null && actual.identity_mode === 'unbound' &&
          actual.wire_identity === null && actual.identity_proven === false && actual.passed === false && documents === 1 && unexpected === 0);
        if (entry.match) check(actual.visible_card_containers === 1 && actual.target_title_count === 1 && actual.anchor_title_count === 1 && actual.observed_title === entry.expected);
        results.push({ name: entry.name, result: 'passed', visible_cards: actual.visible_cards, visible_title_buttons: actual.visible_title_buttons,
          visible_items_containers: actual.visible_items_containers, structural_match: actual.structural_match });
      } catch (error) { results.push({ name: entry.name, result: 'failed', error: error?.name ?? 'Error' }); }
      finally { await context?.close(); }
    }
  } finally { await browser?.close(); }
  return { marker: 'goby-reference-library-changed-synthetic-dom-guards-v1', result: results.every(row => row.result === 'passed') ? 'passed' : 'failed',
    tests: results.length, failures: results.filter(row => row.result === 'failed').length, isolated_network_namespace: true,
    synthetic_html_only: true, reference_client_acceptance: false, business_http: false, results };
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  if (process.argv[2] === '--dom') {
    try { const result = await runReferenceDOMGuards(process.argv.slice(2)); process.stdout.write(JSON.stringify(result) + '\n'); if (result.result === 'failed') process.exitCode = 1; }
    catch { process.stdout.write(JSON.stringify({ marker: 'goby-reference-library-changed-synthetic-dom-guards-v1', result: 'failed', failure: 'isolated_dom_guard_failed' }) + '\n'); process.exitCode = 1; }
  } else {
  check(process.argv.length === 2);
  const results = [];
  for (const entry of tests) { try { await entry.run(); results.push({ name: entry.name, result: 'passed' }); }
    catch (error) { results.push({ name: entry.name, result: 'failed', error: error?.name ?? 'Error' }); } }
  const failures = results.filter(value => value.result === 'failed').length;
  process.stdout.write(JSON.stringify({ marker: 'goby-reference-library-changed-browser-guards-v1', tests: results.length, failures,
    result: failures ? 'failed' : 'passed', browser: false, network: false, database: false, results }) + '\n'); if (failures) process.exitCode = 1;
  }
}
