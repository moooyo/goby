/** Pure contract guards for the bounded NextUp browser driver. */
import test from 'node:test';
import assert from 'node:assert/strict';
import { validateInput, validateFreshOutput, ownedStateRoutes, compareOwnedState, summarizeNextUp, assertSafeExport, WORK, ROOT, TOOL, FILES, EPISODES, SUMMARIES }
  from './client-browser-nextup-discovery.mjs';

const checksum = 'a'.repeat(64);
const description = filename => ({ path: WORK + '/' + filename, sha256: checksum });
function input() {
  return { kind: 'nextup-reference-client-discovery-input', version: 1, runId: 'nextup-reference-client-discovery-01', root: ROOT,
    execution: description('execution.json'), release: description('release.json'),
    sourceClosure: Object.fromEntries(FILES.map(name => [name, { path: TOOL + '/' + name, sha256: checksum }])),
    existingDeviceIds: ['old-device'], libraries: { LA: { viewId: '133', name: 'Library A' }, LB: { viewId: '135', name: 'Library B' } },
    budgets: { maximumSeconds: 1200, normalSeconds: 840, maximumPlaybackAttempts: 2, permittedPlaybackAttempts: 0,
      recorderRequests: 34, recorderBodyBytes: 2097152, browserNavigationActions: 3 } };
}
test('accept the exact zero-playback scope', () => assert.equal(validateInput(input()).budgets.permittedPlaybackAttempts, 0));
test('admit only a precreated empty owned output and reject consumed scopes', () => {
  const output = { isDirectory: true, isSymbolicLink: false, uid: 0, mode: 0o40700, realpath: ROOT, entries: [] };
  assert.doesNotThrow(() => validateFreshOutput(output));
  for (const mutation of [{ entries: ['private'] }, { uid: 1 }, { mode: 0o40755 }, { isSymbolicLink: true }, { realpath: ROOT + '-old' }])
    assert.throws(() => validateFreshOutput({ ...output, ...mutation }));
});
test('reject scope, source, credential, and budget widening', () => {
  for (const change of [value => value.budgets.permittedPlaybackAttempts = 1, value => value.root += '-retry',
    value => value.budgets.maximumSeconds += 1, value => value.sourceClosure[FILES[0]].path += '.other',
    value => value.credentials = description('credentials.json'), value => value.existingDeviceIds.push('old-device')]) {
    const value = input(); change(value); assert.throws(() => validateInput(value));
  }
});
test('enumerate fourteen owned reads and no playback or mutation route', () => {
  const matrix = { actors: { P: { userId: 'owned' } }, items: Object.fromEntries([...EPISODES, ...SUMMARIES].map(symbol => [symbol, { id: symbol }])) };
  const routes = ownedStateRoutes(matrix);
  assert.equal(routes.length, 14); assert.equal(new Set(routes.map(row => row.route)).size, 14);
  assert.ok(routes.every(row => row.route.startsWith('/emby/Users/owned') || row.route === '/emby/UserSettings/owned'));
});
function state() {
  return { ...Object.fromEntries([...EPISODES, ...SUMMARIES].map(symbol => [symbol, { Id: symbol,
    UserData: { Played: false, PlaybackPositionTicks: 0, PlayCount: 0 } }])),
  profile: { Id: 'owned', Name: 'owner', Policy: { IsAdministrator: false }, Configuration: {}, LastActivityDate: 'old' }, preferences: {} };
}
test('preserve absent fields and reject unrelated state drift', () => {
  const before = state(), after = structuredClone(before);
  after.profile.LastActivityDate = 'new'; assert.equal(compareOwnedState(before, after).preserved, true);
  assert.equal(compareOwnedState(before, after).full_profile_equal, false);
  after.A1.UserData.LastPlayedDate = null; assert.equal(compareOwnedState(before, after).preserved, false);
  delete after.A1.UserData.LastPlayedDate; after.profile.Configuration.New = true;
  assert.deepEqual(compareOwnedState(before, after).changed, ['profile.Configuration']);
});
function exchange() {
  const physical = { id: 'physical-1', route: '/Shows/NextUp', method: 'GET', completed: true, status: 200,
    token_sha256: checksum, request_sha256: checksum, exact_public_query: [['UserId', 'owned'], ['Limit', '0']],
    document_id: 'document-1', phase: 'navigate_la', response: { decoded_body_sha256: checksum, decoded_bytes: 100,
      json: { Items: [{ Id: 'second' }, { Id: 'first' }], TotalRecordCount: 2 } } };
  const frame = { ...physical, id: 'frame-1', sourceworker: false, from_service_worker: false, main_frame: true,
    frame_body: { decoded_body_sha256: checksum, decoded_body_bytes: 100 } };
  return { physical, frame };
}
test('retain original response order and query without a SeriesId', () => {
  const { physical, frame } = exchange(); const [result] = summarizeNextUp([physical], [frame], checksum);
  assert.deepEqual(result.ordered_ids, ['second', 'first']); assert.equal(result.actual_client_response_bound, true);
  assert.deepEqual(result.exact_public_query, physical.exact_public_query);
});
test('ignore nullable rejected routes and never accept series queries as global', () => {
  const { physical, frame } = exchange();
  physical.exact_public_query.push(['sErIeSiD', 'series']);
  assert.deepEqual(summarizeNextUp([{ route: null }, physical], [frame], checksum), []);
});
test('reject ambiguous, cached, subframe, incomplete, and foreign-token client association', () => {
  for (const change of [value => value.from_service_worker = true, value => value.main_frame = false,
    value => value.completed = false, value => value.token_sha256 = 'b'.repeat(64), value => value.sourceworker = true,
    value => value.frame_body.decoded_body_sha256 = 'b'.repeat(64), value => value.frame_body.decoded_body_bytes = 99]) {
    const { physical, frame } = exchange(); change(frame);
    assert.equal(summarizeNextUp([physical], [frame], checksum)[0].actual_client_response_bound, false);
  }
  const { physical, frame } = exchange();
  assert.equal(summarizeNextUp([physical], [frame, { ...frame, id: 'frame-2' }], checksum)[0].actual_client_response_bound, false);
  assert.equal(summarizeNextUp([physical, { ...physical, id: 'physical-2' }], [frame], checksum)[0].actual_client_response_bound, false);
});
test('reject direct and encoded credentials or native paths in export', () => {
  const secret = 'owned-secret-0123456789-with-characters';
  for (const value of [secret, encodeURIComponent(secret), Buffer.from(secret).toString('base64'),
    Buffer.from(secret).toString('hex'), '/opt/goby-test/private']) assert.throws(() => assertSafeExport({ text: value }, [secret]));
  assert.doesNotThrow(() => assertSafeExport({ route: '/Shows/NextUp', ordered_ids: ['140', '141'] }, [secret]));
});
