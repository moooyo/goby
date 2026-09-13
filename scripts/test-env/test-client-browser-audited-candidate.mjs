/** Pure guards for synthetic candidate inputs; no browser, process, or transport is started. */
import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import {
  validateCandidateManifest,
  selectedCandidateItem,
  candidateRequestScope,
  validateCandidateLogin,
  candidateLogoutProven,
  candidateRemainingMilliseconds,
  validateCandidateGateway,
  GATEWAY_CLOSEOUT_SECONDS,
  candidatePlaybackEvidence,
} from './client-browser-audited-candidate.mjs';

const scenarios = ['movie', 'episode', 'mp3', 'flac', 'subtitles', 'tv-browse'];
const id = number => number.toString(16).padStart(32, '0');
const digest = number => number.toString(16).padStart(64, '0');
const hash = value => createHash('sha256').update(value).digest('hex');
const clone = value => structuredClone(value);
const zeroGatewayUsage = Object.freeze({ normal: 0, cleanup: 0 });

function syntheticManifest(scenario = 'movie') {
  const process = (name, number, port) => ({
    bootId: '00000000-0000-4000-8000-000000000001', pid: 870000 + number, startTicks: String(990000 + number), uid: 0,
    exe: '/opt/goby-test/synthetic-candidate/' + name, exeDevice: 101, exeInode: 200000 + number,
    cmdline: ['/opt/goby-test/synthetic-candidate/' + name],
    cgroup: '0::/system.slice/synthetic-' + name + '.service\n', networkNamespace: 'net:[4026500001]',
    listener: { host: '127.0.0.1', port, socketInode: String(300000 + number) },
  });
  const media = (number, type, name, runtimeTicks, extension) => ({
    id: id(number), type, name, runtimeTicks, mediaSha256: digest(500 + number),
    path: '/synthetic-media/' + number + '.' + extension,
  });
  const value = {
    kind: 'audited-candidate-client-input', version: 1, runId: 'synthetic-candidate-' + scenario, scenario,
    clientUrl: 'http://127.0.0.1:19196/web/index.html', browserOrigin: 'http://127.0.0.1:19196',
    directOrigin: 'http://127.0.0.1:19198', serverId: id(1), actor: { id: id(2), username: 'synthetic-ui-viewer' },
    credentials: { path: '/opt/goby-test/synthetic-candidate/credentials.json', sha256: digest(11) },
    source: { manifestSha256: digest(12), binarySha256: digest(13), schema: 32 },
    processes: { candidate: process('candidate', 1, 19198), gateway: process('gateway', 2, 19196) },
    catalog: {}, budgets: { maximumSeconds: 300, cleanupSeconds: 60 },
    output: '/opt/goby-test/synthetic-candidate/output-' + scenario,
    approval: { path: '/opt/goby-test/synthetic-candidate/approval.json', sha256: digest(14) },
    gatewayAttestation: { path: '/opt/goby-test/synthetic-candidate/gateway-attestation.json', sha256: digest(15) },
  };
  if (['movie', 'subtitles'].includes(scenario))
    value.catalog.movie = media(10, 'Movie', 'M3e Client Movie', 6000000000, 'mp4');
  if (scenario === 'subtitles') value.catalog.subtitles = [
    { index: 2, codec: 'srt', language: 'eng', external: true, sha256: digest(21) },
    { index: 3, codec: 'vtt', language: 'eng', external: true, sha256: digest(22) },
  ];
  if (['mp3', 'flac'].includes(scenario)) {
    value.catalog[scenario] = { ...media(scenario === 'mp3' ? 22 : 23, 'Audio',
      scenario === 'mp3' ? 'M3e MP3' : 'M3e FLAC', 1800000000, scenario), container: scenario };
    value.catalog.musicLibrary = { id: id(20), name: 'M3e Client Music' };
    value.catalog.album = { id: id(21), name: 'M3e Synthetic Album' };
  }
  if (['episode', 'tv-browse'].includes(scenario)) {
    value.catalog.tvLibrary = { id: id(30), name: 'M3e Client Television' };
    value.catalog.series = { id: id(31), type: 'Series', name: 'M3e Client Series', path: '/synthetic-media/series' };
    value.catalog.seasons = [1, 2].map(number => ({
      id: id(31 + number), type: 'Season', indexNumber: number, seriesId: id(31), parentId: id(31),
    }));
    value.catalog.episodes = [[1, 1], [1, 2], [2, 1]].map(([season, episode], index) => ({
      ...media(34 + index, 'Episode', 'Episode ' + season + '-' + episode, 1200000000, 'mp4'),
      seriesId: id(31), parentId: value.catalog.seasons[season - 1].id, parentIndexNumber: season, indexNumber: episode,
    }));
  }
  return value;
}

function loginBody(manifest, number = 1) {
  return {
    AccessToken: 'synthetic-private-token-number-' + number, ServerId: manifest.serverId,
    User: { Id: manifest.actor.id, Name: manifest.actor.username, Policy: { IsAdministrator: false } },
    SessionInfo: { Id: 'synthetic-session-' + number, UserId: manifest.actor.id, DeviceId: 'synthetic-device-' + number,
      Client: 'Synthetic Original Client', ApplicationVersion: 'synthetic-version' },
  };
}

test('subtitle language keeps either observed English code without normalization', () => {
  for (const language of ['en', 'eng']) {
    const manifest = syntheticManifest('subtitles');
    manifest.catalog.subtitles.forEach(track => { track.language = language; });
    validateCandidateManifest(manifest);
    assert.deepEqual(manifest.catalog.subtitles.map(track => track.language), [language, language]);
  }
  for (const language of ['fra', 'english', 'EN', '', null]) {
    const manifest = syntheticManifest('subtitles');
    manifest.catalog.subtitles[0].language = language;
    assert.throws(() => validateCandidateManifest(manifest));
  }
});

function syntheticGateway(manifest, maxSeconds = 600, started = 9007199254740993000n) {
  const { listener: candidateListener, ...candidateProcess } = clone(manifest.processes.candidate);
  const { listener: gatewayListener, ...gatewayProcess } = clone(manifest.processes.gateway);
  return {
    runId: manifest.runId, proxyOrigin: manifest.browserOrigin, browserOrigin: manifest.browserOrigin,
    directOrigin: manifest.directOrigin, inputSha256: digest(71),
    sources: {
      gateway: { path: '/opt/goby-test/synthetic-candidate/gateway.py', sha256: digest(72) },
      proxy: { path: '/opt/goby-test/synthetic-candidate/proxy.py', sha256: digest(73) },
    },
    policy: { version: 'core-av-transparent-gateway-v1' }, ledgerRoot: '/opt/goby-test/synthetic-candidate/gateway-ledger',
    process: gatewayProcess, listener: gatewayListener,
    upstreams: { goby: { process: candidateProcess, listener: candidateListener, executableSha256: manifest.source.binarySha256 } },
    budgets: { maxRequests: 2000, cleanupRequests: 100, maxApiBodyBytes: 1048576, maxApiTotalBytes: 16777216,
      maxSeconds, idleSeconds: 60, maxConcurrent: 16 },
    startedMonotonicNs: String(started), deadlineMonotonicNs: String(started + BigInt(maxSeconds) * 1000000000n),
  };
}

function session(number = 1) {
  return { session_id: 'synthetic-session-' + number, token_sha256: hash('synthetic-private-token-number-' + number) };
}

function lifecycle(manifest, actorSession = session(), firstOrdinal = 1, playSession = 'synthetic-play-session-1') {
  const selected = selectedCandidateItem(manifest);
  const body = { ItemId: selected.id, SessionId: actorSession.session_id, PlaySessionId: playSession,
    MediaSourceId: 'synthetic-media-source-' + selected.id };
  const row = (ordinal, kind, route, content) => ({ ordinal, kind, route, completed: true, status: kind === 'media' ? 206 : 204,
    token_sha256: actorSession.token_sha256, ...(content ? { body: clone(content) } : {}) });
  return [
    row(firstOrdinal, 'playback_report', '/Sessions/Playing', body),
    row(firstOrdinal + 1, 'media', '/' + (selected.type === 'Audio' ? 'Audio' : 'Videos') + '/' + selected.id + '/stream'),
    row(firstOrdinal + 2, 'playback_report', '/Sessions/Playing/Progress', body),
    row(firstOrdinal + 3, 'playback_report', '/Sessions/Playing/Stopped', body),
  ];
}

test('all six scenarios accept complete minimal synthetic manifests without rewriting identities', () => {
  for (const scenario of scenarios) {
    const manifest = syntheticManifest(scenario), before = clone(manifest);
    assert.equal(validateCandidateManifest(manifest), manifest);
    assert.deepEqual(manifest, before);
  }
});

test('fresh process identity must be explicit and no missing field receives a historical default', () => {
  for (const name of ['candidate', 'gateway']) for (const field of [
    'bootId', 'pid', 'startTicks', 'uid', 'exe', 'exeDevice', 'exeInode', 'cmdline', 'cgroup', 'networkNamespace', 'listener',
  ]) {
    const manifest = syntheticManifest(); delete manifest.processes[name][field]; const before = clone(manifest);
    assert.throws(() => validateCandidateManifest(manifest), undefined, name + '.' + field);
    assert.deepEqual(manifest, before);
  }
  for (const name of ['candidate', 'gateway']) {
    const manifest = syntheticManifest(); delete manifest.processes[name];
    assert.throws(() => validateCandidateManifest(manifest));
    assert.equal(Object.hasOwn(manifest.processes, name), false);
  }
  for (const name of ['candidate', 'gateway']) for (const cmdline of [[], 'unbound-command', [1], ['contains\0nul'], ['x'.repeat(4097)]]) {
    const manifest = syntheticManifest(); manifest.processes[name].cmdline = cmdline;
    assert.throws(() => validateCandidateManifest(manifest));
  }
});

test('source, credential, approval, and gateway bindings require explicit valid descriptors', () => {
  const mutations = [
    value => { delete value.source; }, value => { delete value.source.manifestSha256; },
    value => { value.source.binarySha256 = 'unbound'; }, value => { value.source.schema = 0; },
    value => { delete value.credentials.sha256; }, value => { value.credentials.path = '/opt/goby-test/../credentials.json'; },
    value => { value.approval.sha256 = 'unapproved'; }, value => { delete value.approval; },
    value => { delete value.gatewayAttestation; }, value => { delete value.gatewayAttestation.sha256; },
    value => { value.gatewayAttestation.sha256 = 'unbound-gateway'; },
    value => { value.gatewayAttestation.path = '/opt/goby-test/../gateway-attestation.json'; },
  ];
  for (const mutate of mutations) { const manifest = syntheticManifest(); mutate(manifest); assert.throws(() => validateCandidateManifest(manifest)); }
});

test('loopback client origins and both listener ports must match their declared processes', () => {
  for (const mutate of [
    value => { value.browserOrigin = 'https://127.0.0.1:19196'; },
    value => { value.browserOrigin = 'http://example.invalid:19196'; },
    value => { value.directOrigin = 'http://127.0.0.1:19198/path'; },
    value => { value.clientUrl += '?unbound=1'; }, value => { value.clientUrl += '#unbound'; },
    value => { value.clientUrl = value.directOrigin + '/web/index.html'; },
    value => { value.processes.candidate.listener.port = 19196; },
    value => { value.processes.gateway.listener.port = 19198; },
    value => { value.processes.gateway.listener.host = '0.0.0.0'; },
    value => { value.processes.candidate.listener.socketInode = 'socket:[300001]'; },
    value => { value.processes.gateway.listener = { address: '127.0.0.1', port: 19196, inode: '300002' }; },
  ]) { const manifest = syntheticManifest(); mutate(manifest); assert.throws(() => validateCandidateManifest(manifest)); }
  for (const name of ['candidate', 'gateway']) for (const field of ['host', 'port', 'socketInode']) {
    const manifest = syntheticManifest(); delete manifest.processes[name].listener[field];
    assert.throws(() => validateCandidateManifest(manifest));
  }
});

test('scenario, schema shape, output scope, and execution budgets are bounded', () => {
  for (const mutate of [
    value => { value.scenario = 'unknown'; }, value => { value.version = 2; }, value => { value.implicitLegacyFixture = true; },
    value => { value.output = '/tmp/unbound'; }, value => { value.output = '/opt/goby-test/../unbound'; },
    value => { value.budgets.maximumSeconds = 119; }, value => { value.budgets.maximumSeconds = 1201; },
    value => { value.budgets.cleanupSeconds = 59; }, value => { value.budgets.cleanupSeconds = value.budgets.maximumSeconds; },
  ]) { const manifest = syntheticManifest(); mutate(manifest); assert.throws(() => validateCandidateManifest(manifest)); }
});

test('remaining time uses the original phase deadline and never restores elapsed execution budget', () => {
  const budgets = { maximumSeconds: 300, cleanupSeconds: 60 };
  assert.equal(candidateRemainingMilliseconds(budgets, 0), 240000);
  assert.equal(candidateRemainingMilliseconds(budgets, 1250), 238750);
  assert.equal(candidateRemainingMilliseconds(budgets, 239999), 1);
  assert.throws(() => candidateRemainingMilliseconds(budgets, 240000), /time_budget_exhausted/);
  assert.throws(() => candidateRemainingMilliseconds(budgets, 240001), /time_budget_exhausted/);
  assert.equal(candidateRemainingMilliseconds(budgets, 0, true), 300000);
  assert.equal(candidateRemainingMilliseconds(budgets, 239999, true), 60001);
  assert.equal(candidateRemainingMilliseconds(budgets, 240000, true), 60000);
  assert.equal(candidateRemainingMilliseconds(budgets, 299999, true), 1);
  assert.throws(() => candidateRemainingMilliseconds(budgets, 300000, true), /time_budget_exhausted/);
  assert.throws(() => candidateRemainingMilliseconds(budgets, 300001, true), /time_budget_exhausted/);
  for (const cleanup of [false, true]) for (const elapsed of [-1, NaN, Infinity, -Infinity])
    assert.throws(() => candidateRemainingMilliseconds(budgets, elapsed, cleanup), /invalid_elapsed_time/);
});

test('gateway proof binds complete process and listener identities with an exact closeout reserve', () => {
  assert.equal(GATEWAY_CLOSEOUT_SECONDS, 30);
  for (const maximumSeconds of [120, 300]) {
    const manifest = syntheticManifest(); manifest.budgets.maximumSeconds = maximumSeconds;
    const gateway = syntheticGateway(manifest, maximumSeconds + GATEWAY_CLOSEOUT_SECONDS), before = clone(gateway);
    const current = BigInt(gateway.startedMonotonicNs);
    assert.equal(validateCandidateManifest(manifest), manifest);
    assert.equal(validateCandidateGateway(gateway, manifest, current, zeroGatewayUsage), gateway);
    assert.deepEqual(gateway, before);
    assert.equal(Object.hasOwn(gateway.process, 'listener'), false);
    assert.equal(Object.hasOwn(gateway.upstreams.goby.process, 'listener'), false);
    assert.deepEqual(gateway.listener, manifest.processes.gateway.listener);
    assert.deepEqual(gateway.upstreams.goby.listener, manifest.processes.candidate.listener);
    assert.throws(() => validateCandidateGateway(gateway, manifest, current + 1n, zeroGatewayUsage), /remaining_lifetime_insufficient/);
  }
});

test('two individually valid candidate bindings cannot share each other gateway upstream proof', () => {
  const first = syntheticManifest(), second = clone(first);
  second.source.binarySha256 = digest(81);
  second.source.manifestSha256 = digest(82);
  Object.assign(second.processes.candidate, { pid: 870011, startTicks: '990011', exe: '/opt/goby-test/synthetic-candidate/candidate-b',
    exeInode: 200011, cmdline: ['/opt/goby-test/synthetic-candidate/candidate-b'] });
  second.processes.candidate.listener.socketInode = '300011';
  const firstGateway = syntheticGateway(first), secondGateway = syntheticGateway(second);
  const current = BigInt(firstGateway.startedMonotonicNs) + 1000000000n;
  assert.equal(validateCandidateManifest(first), first); assert.equal(validateCandidateManifest(second), second);
  assert.equal(validateCandidateGateway(firstGateway, first, current, zeroGatewayUsage), firstGateway);
  assert.equal(validateCandidateGateway(secondGateway, second, current, zeroGatewayUsage), secondGateway);
  assert.throws(() => validateCandidateGateway(firstGateway, second, current, zeroGatewayUsage), /gateway_binding_required/);
  assert.throws(() => validateCandidateGateway(secondGateway, first, current, zeroGatewayUsage), /gateway_binding_required/);
  for (const mutate of [
    value => { value.upstreams.goby.process.pid += 1; },
    value => { value.upstreams.goby.listener.socketInode = '399999'; },
    value => { value.upstreams.goby.executableSha256 = digest(99); },
    value => { value.upstreams.goby.process.listener = clone(value.upstreams.goby.listener); },
    value => { delete value.upstreams.goby.process.cmdline; },
  ]) {
    const gateway = clone(firstGateway); mutate(gateway);
    assert.throws(() => validateCandidateGateway(gateway, first, current, zeroGatewayUsage), /gateway_binding_required/);
  }
});

test('gateway self identity and listener must equal the manifest without omitted or extra process fields', () => {
  const manifest = syntheticManifest(), original = syntheticGateway(manifest), current = BigInt(original.startedMonotonicNs);
  for (const mutate of [
    value => { value.process.bootId = '00000000-0000-4000-8000-000000000002'; },
    value => { value.process.pid += 1; }, value => { value.process.startTicks = '991111'; },
    value => { value.process.uid = 1; }, value => { value.process.exe += '-other'; },
    value => { value.process.exeDevice += 1; }, value => { value.process.exeInode += 1; },
    value => { value.process.cmdline.push('--different'); },
    value => { value.process.cgroup = '0::/system.slice/other-gateway.service\n'; },
    value => { value.process.networkNamespace = 'net:[4026500999]'; },
    value => { value.process.listener = clone(value.listener); }, value => { delete value.process.cmdline; },
    value => { value.listener.host = '0.0.0.0'; }, value => { value.listener.port += 1; },
    value => { value.listener.socketInode = '399999'; },
  ]) {
    const gateway = clone(original); mutate(gateway);
    assert.throws(() => validateCandidateGateway(gateway, manifest, current, zeroGatewayUsage), /gateway_binding_required/);
  }
});

test('gateway monotonic lifetime rejects short leases, expired leases, future starts, and malformed deadlines', () => {
  for (const maximumSeconds of [120, 300]) {
    const manifest = syntheticManifest(); manifest.budgets.maximumSeconds = maximumSeconds;
    const short = syntheticGateway(manifest, 30);
    assert.throws(() => validateCandidateGateway(short, manifest, BigInt(short.startedMonotonicNs), zeroGatewayUsage), /remaining_lifetime_insufficient/);
  }
  const manifest = syntheticManifest(), original = syntheticGateway(manifest);
  const start = BigInt(original.startedMonotonicNs), deadline = BigInt(original.deadlineMonotonicNs);
  assert.equal(validateCandidateGateway(original, manifest, start + 200000000000n, zeroGatewayUsage), original);
  for (const current of [start - 1n, deadline, deadline + 1n])
    assert.throws(() => validateCandidateGateway(original, manifest, current, zeroGatewayUsage), /remaining_lifetime_insufficient/);
  for (const current of [0n, -1n, Number(start), String(start), null])
    assert.throws(() => validateCandidateGateway(original, manifest, current, zeroGatewayUsage), /monotonic_binding_required/);
  for (const field of ['startedMonotonicNs', 'deadlineMonotonicNs']) for (const invalid of [undefined, '0', '-1', '1.5', '1e12', '0001', 1000000]) {
    const gateway = clone(original); gateway[field] = invalid;
    assert.throws(() => validateCandidateGateway(gateway, manifest, start, zeroGatewayUsage), /monotonic_binding_required/);
  }
  for (const mutate of [
    value => { value.deadlineMonotonicNs = String(deadline + 1n); },
    value => { value.deadlineMonotonicNs = String(start); },
    value => { value.deadlineMonotonicNs = String(start - 1n); },
    value => { value.startedMonotonicNs = String(start + 1n); value.deadlineMonotonicNs = String(deadline + 1n); },
  ]) {
    const gateway = clone(original); mutate(gateway);
    assert.throws(() => validateCandidateGateway(gateway, manifest, start, zeroGatewayUsage), /remaining_lifetime_insufficient/);
  }
});

test('gateway budgets require an exact bounded cleanup reservation below the total request budget', () => {
  const manifest = syntheticManifest(), original = syntheticGateway(manifest), current = BigInt(original.startedMonotonicNs);
  for (const cleanupRequests of [6, 1000]) {
    const gateway = clone(original); gateway.budgets.cleanupRequests = cleanupRequests;
    assert.equal(validateCandidateGateway(gateway, manifest, current, zeroGatewayUsage), gateway);
  }
  for (const mutate of [
    value => { delete value.budgets.cleanupRequests; }, value => { value.budgets.extraBudget = 1; },
    value => { value.budgets.cleanupRequests = 0; }, value => { value.budgets.cleanupRequests = 1001; },
    value => { value.budgets.cleanupRequests = 1.5; },
    value => { value.budgets.maxRequests = 100; value.budgets.cleanupRequests = 100; },
    value => { value.budgets.maxRequests = 1; }, value => { value.budgets.maxRequests = 10001; },
    value => { value.budgets.maxApiBodyBytes = 1023; }, value => { value.budgets.maxApiBodyBytes = 1048577; },
    value => { value.budgets.maxApiTotalBytes = 67108865; },
    value => { value.budgets.maxApiTotalBytes = value.budgets.maxApiBodyBytes - 1; },
    value => { value.budgets.maxSeconds = 29; }, value => { value.budgets.maxSeconds = 7201; },
    value => { value.budgets.idleSeconds = 9; }, value => { value.budgets.idleSeconds = 601; },
    value => { value.budgets.maxConcurrent = 0; }, value => { value.budgets.maxConcurrent = 65; },
  ]) {
    const gateway = clone(original); mutate(gateway);
    assert.throws(() => validateCandidateGateway(gateway, manifest, current, zeroGatewayUsage), /gateway_budgets_invalid/);
  }
});

test('gateway admission requires sufficient current cleanup capacity after exact recorded usage', () => {
  for (const scenario of scenarios) {
    const manifest = syntheticManifest(scenario), gateway = syntheticGateway(manifest);
    const minimum = scenario === 'movie' ? 6 : scenario === 'tv-browse' ? 2 : 3;
    gateway.budgets.cleanupRequests = minimum + 4;
    const current = BigInt(gateway.startedMonotonicNs), normalCap = gateway.budgets.maxRequests - gateway.budgets.cleanupRequests;
    assert.equal(validateCandidateGateway(gateway, manifest, current, { normal: normalCap, cleanup: 4 }), gateway);
    assert.throws(() => validateCandidateGateway(gateway, manifest, current, { normal: 0, cleanup: 5 }),
      /cleanup_reserve_insufficient/);
    assert.throws(() => validateCandidateGateway(gateway, manifest, current, { normal: normalCap + 1, cleanup: 0 }),
      /cleanup_reserve_insufficient/);
    gateway.budgets.cleanupRequests = minimum;
    assert.equal(validateCandidateGateway(gateway, manifest, current, zeroGatewayUsage), gateway);
    assert.throws(() => validateCandidateGateway(gateway, manifest, current, { normal: 0, cleanup: 1 }),
      /cleanup_reserve_insufficient/);
  }
  const manifest = syntheticManifest(), gateway = syntheticGateway(manifest), current = BigInt(gateway.startedMonotonicNs);
  for (const usage of [
    undefined, null, [], {}, { normal: 0 }, { cleanup: 0 }, { normal: 0, cleanup: 0, extra: 0 },
    { normal: -1, cleanup: 0 }, { normal: 0, cleanup: -1 }, { normal: 0.5, cleanup: 0 },
    { normal: 0, cleanup: 0.5 }, { normal: '0', cleanup: 0 }, { normal: 0, cleanup: '0' },
    { normal: Infinity, cleanup: 0 }, { normal: 0, cleanup: NaN }, { normal: 0n, cleanup: 0 },
    { normal: 0, cleanup: gateway.budgets.cleanupRequests + 1 },
  ]) assert.throws(() => validateCandidateGateway(gateway, manifest, current, usage), /cleanup_reserve_insufficient/);
});

test('media scenarios reject missing mappings, wrong item types, containers, and required durations', () => {
  for (const scenario of ['movie', 'episode', 'mp3', 'flac', 'subtitles']) {
    const missing = syntheticManifest(scenario); missing.catalog = {};
    assert.throws(() => validateCandidateManifest(missing));
    const wrongType = syntheticManifest(scenario); selectedCandidateItem(wrongType).type = 'Folder';
    assert.throws(() => validateCandidateManifest(wrongType));
    const wrongDuration = syntheticManifest(scenario); selectedCandidateItem(wrongDuration).runtimeTicks = 1;
    assert.throws(() => validateCandidateManifest(wrongDuration));
    const missingHash = syntheticManifest(scenario); delete selectedCandidateItem(missingHash).mediaSha256;
    assert.throws(() => validateCandidateManifest(missingHash));
  }
  for (const scenario of ['mp3', 'flac']) {
    const manifest = syntheticManifest(scenario); manifest.catalog[scenario].container = scenario === 'mp3' ? 'flac' : 'mp3';
    assert.throws(() => validateCandidateManifest(manifest));
  }
});

test('TV mappings preserve season order, parent relations, episode numbers, and distinct identities', () => {
  for (const scenario of ['episode', 'tv-browse']) for (const mutate of [
    value => { delete value.catalog.tvLibrary; }, value => { value.catalog.seasons.pop(); },
    value => { value.catalog.episodes.reverse(); }, value => { value.catalog.episodes[2].parentId = id(32); },
    value => { value.catalog.episodes[1].seriesId = id(999); }, value => { value.catalog.seasons[1].indexNumber = 1; },
    value => { value.catalog.episodes[0].id = value.catalog.series.id; },
  ]) { const manifest = syntheticManifest(scenario); mutate(manifest); assert.throws(() => validateCandidateManifest(manifest)); }
});

test('subtitle mappings require distinct external English SRT and VTT tracks with hashes', () => {
  for (const mutate of [
    value => { value.catalog.subtitles.reverse(); }, value => { value.catalog.subtitles[1].index = value.catalog.subtitles[0].index; },
    value => { value.catalog.subtitles[0].external = false; }, value => { value.catalog.subtitles[0].language = 'fra'; },
    value => { value.catalog.subtitles[1].sha256 = 'unbound'; }, value => { value.catalog.subtitles.pop(); },
  ]) { const manifest = syntheticManifest('subtitles'); mutate(manifest); assert.throws(() => validateCandidateManifest(manifest)); }
});

test('scenario selection uses the mapped item and browse never acquires a playback item', () => {
  for (const scenario of scenarios) {
    const manifest = syntheticManifest(scenario);
    const expected = scenario === 'episode' ? manifest.catalog.episodes[2] : ['movie', 'subtitles'].includes(scenario)
      ? manifest.catalog.movie : scenario === 'tv-browse' ? null : manifest.catalog[scenario];
    assert.equal(selectedCandidateItem(manifest), expected);
  }
  const browse = syntheticManifest('tv-browse'); browse.catalog['tv-browse'] = { id: id(999), type: 'Movie' };
  assert.equal(selectedCandidateItem(browse), null);
});

test('request admission rejects foreign actors, origins, and unrelated state mutations', () => {
  const manifest = syntheticManifest(), origin = manifest.browserOrigin;
  for (const raw of [
    manifest.directOrigin + '/System/Info', 'https://127.0.0.1:19196/System/Info',
    origin + '/Users/' + id(999) + '/Items', origin + '/Items?UserId=' + id(999),
    origin + '/Items?UserId=' + manifest.actor.id + '&userid=' + id(999),
  ]) assert.equal(candidateRequestScope(raw, 'GET', manifest).allowed, false);
  for (const method of ['POST', 'PUT', 'PATCH', 'DELETE'])
    assert.equal(candidateRequestScope(origin + '/Users/' + manifest.actor.id, method, manifest).allowed, false);
  for (const [route, kind] of [['/Users/AuthenticateByName', 'login'], ['/Sessions/Logout', 'logout'],
    ['/Sessions/Capabilities/Full', 'capabilities']])
    assert.deepEqual(candidateRequestScope(origin + '/emby' + route, 'POST', manifest), { allowed: true, kind, route });
  assert.equal(candidateRequestScope(origin + '/Users/' + manifest.actor.id + '/Items', 'GET', manifest).allowed, true);
  assert.equal(candidateRequestScope('data:text/plain,synthetic', 'GET', manifest).kind, 'non_network');
});

test('media and PlaybackInfo admission bind the selected item while browse stays nonplaying', () => {
  for (const scenario of ['movie', 'episode', 'mp3', 'flac', 'subtitles']) {
    const manifest = syntheticManifest(scenario), selected = selectedCandidateItem(manifest), origin = manifest.browserOrigin;
    const family = selected.type === 'Audio' ? 'Audio' : 'Videos';
    for (const method of ['GET', 'HEAD']) {
      assert.equal(candidateRequestScope(origin + '/' + family + '/' + selected.id + '/stream', method, manifest).allowed, true);
      assert.equal(candidateRequestScope(origin + '/' + family + '/' + id(999) + '/stream', method, manifest).allowed, false);
    }
    assert.equal(candidateRequestScope(origin + '/Items/' + selected.id + '/PlaybackInfo', 'POST', manifest).allowed, true);
    assert.equal(candidateRequestScope(origin + '/Items/' + id(999) + '/PlaybackInfo', 'POST', manifest).allowed, false);
  }
  const browse = syntheticManifest('tv-browse'), origin = browse.browserOrigin;
  for (const episode of browse.catalog.episodes) {
    assert.equal(candidateRequestScope(origin + '/Items/' + episode.id + '/PlaybackInfo', 'POST', browse).allowed, true);
    assert.equal(candidateRequestScope(origin + '/Videos/' + episode.id + '/stream', 'GET', browse).allowed, false);
  }
  assert.equal(candidateRequestScope(origin + '/Items/' + id(999) + '/PlaybackInfo', 'POST', browse).allowed, false);
  for (const route of ['/Sessions/Playing', '/Sessions/Playing/Progress', '/Sessions/Playing/Stopped'])
    assert.equal(candidateRequestScope(origin + route, 'POST', browse).allowed, false);
  const movie = syntheticManifest('movie'), selected = movie.catalog.movie.id;
  for (const method of ['GET', 'HEAD', 'OPTIONS']) for (const route of [
    '/Videos/' + selected + '/unknown-resource', '/Audio/' + selected + '/unknown-resource',
    '/Videos/' + id(999) + '/unknown-resource', '/Audio/' + id(999) + '/unknown-resource',
    '/Items/' + selected + '/Download', '/Items/' + id(999) + '/Download', '/Download/' + selected,
  ]) assert.equal(candidateRequestScope(movie.browserOrigin + route, method, movie).allowed, false);
});

test('login proof binds the ordinary actor and server while exposing only the token digest', () => {
  const manifest = syntheticManifest(), login = loginBody(manifest), before = clone(login);
  const proof = validateCandidateLogin(login, manifest);
  assert.deepEqual(proof, { user_id: manifest.actor.id, server_id: manifest.serverId, session_id: login.SessionInfo.Id,
    device_id: login.SessionInfo.DeviceId, client: login.SessionInfo.Client, client_version: login.SessionInfo.ApplicationVersion,
    token_sha256: hash(login.AccessToken) });
  assert.equal(JSON.stringify(proof).includes(login.AccessToken), false);
  assert.deepEqual(login, before);
});

test('login rejects another actor, server, administrator, incomplete session, or invalid token', () => {
  const manifest = syntheticManifest();
  for (const mutate of [
    value => { value.ServerId = id(999); }, value => { value.User.Id = id(999); }, value => { value.User.Name = 'foreign-viewer'; },
    value => { value.User.Policy.IsAdministrator = true; }, value => { delete value.User.Policy; },
    value => { value.SessionInfo.UserId = id(999); }, value => { value.SessionInfo.Id = ''; },
    value => { value.SessionInfo.DeviceId = ''; }, value => { value.AccessToken = 'short'; },
    value => { value.AccessToken = 'x'.repeat(4097); },
  ]) { const login = loginBody(manifest); mutate(login); assert.throws(() => validateCandidateLogin(login, manifest), /identity_rejected/); }
});

test('logout proof requires one matching completed UI 204 and independent token rejection with no observer loss', () => {
  const tokenHash = session().token_sha256;
  const proof = { observer_errors: 0, logout_overflow: 0, entries: [{
    token_fingerprint: tokenHash, result: 'logout_token_rejected',
    ui_request: { response_status: 204, client_request_finished: true, client_request_failed: false },
    verification: { status: 401, result: 'token_rejected' },
  }] };
  assert.equal(candidateLogoutProven(proof, tokenHash), true);
  assert.equal(candidateLogoutProven(proof, session(2).token_sha256), false);
  assert.equal(candidateLogoutProven(undefined, tokenHash), false);
  const anotherSession = clone(proof.entries[0]); anotherSession.token_fingerprint = session(2).token_sha256;
  assert.equal(candidateLogoutProven({ ...proof, entries: [...proof.entries, anotherSession] }, tokenHash), true);
  for (const mutate of [
    value => { value.entries = []; }, value => { value.entries.push(clone(value.entries[0])); },
    value => { value.entries[0].token_fingerprint = session(2).token_sha256; },
    value => { value.entries[0].result = 'logout_token_proof_incomplete'; },
    value => { value.entries[0].ui_request.response_status = 200; },
    value => { value.entries[0].ui_request.response_status = 500; },
    value => { value.entries[0].ui_request.client_request_finished = false; },
    value => { delete value.entries[0].ui_request.client_request_finished; },
    value => { value.entries[0].ui_request.client_request_failed = true; },
    value => { delete value.entries[0].ui_request.client_request_failed; },
    value => { delete value.entries[0].ui_request; },
    value => { value.entries[0].verification.status = 200; },
    value => { value.entries[0].verification.status = 403; },
    value => { value.entries[0].verification.result = 'token_accepted'; },
    value => { delete value.entries[0].verification; },
    value => { value.observer_errors = 1; }, value => { delete value.observer_errors; },
    value => { value.logout_overflow = 1; }, value => { delete value.logout_overflow; },
  ]) {
    const incomplete = clone(proof); mutate(incomplete);
    assert.equal(candidateLogoutProven(incomplete, tokenHash), false);
  }
});

test('each single-playback scenario needs a successful media request and matched start and stop', () => {
  for (const scenario of ['episode', 'mp3', 'flac', 'subtitles']) {
    const manifest = syntheticManifest(scenario), actorSession = session(), rows = lifecycle(manifest, actorSession);
    const proof = candidatePlaybackEvidence(rows, manifest, [actorSession]);
    assert.equal(proof.required, true); assert.equal(proof.passed, true);
    assert.equal(proof.item_id, selectedCandidateItem(manifest).id);
    assert.equal(proof.lifecycles.length, 1); assert.deepEqual(proof.media_request_ordinals, [2]);
    assert.deepEqual(proof.progress_request_ordinals, [3]);
    assert.equal(candidatePlaybackEvidence(rows.filter(row => row.kind !== 'media'), manifest, [actorSession]).passed, false);
    assert.equal(candidatePlaybackEvidence([], manifest, [actorSession]).passed, false);
  }
});

test('movie requires two independent login tokens and playback lifecycles', () => {
  const manifest = syntheticManifest('movie'), first = session(1), second = session(2);
  const initial = lifecycle(manifest, first), repeated = lifecycle(manifest, second, 5, 'synthetic-play-session-2');
  const proof = candidatePlaybackEvidence([...initial, ...repeated], manifest, [first, second]);
  assert.equal(proof.passed, true); assert.equal(proof.lifecycles.length, 2);
  assert.equal(candidatePlaybackEvidence(initial, manifest, [first, second]).passed, false);
  const duplicateStop = { ...clone(initial[3]), ordinal: 5 };
  assert.equal(candidatePlaybackEvidence([...initial, duplicateStop, clone(initial[0])], manifest, [first, second]).passed, false);
  const sameLogin = lifecycle(manifest, first, 5, 'synthetic-play-session-2');
  assert.equal(candidatePlaybackEvidence([...initial, ...sameLogin], manifest, [first, second]).passed, false);
});

test('playback evidence rejects mismatched item, media source, play session, actor session, token, and ordering', () => {
  const manifest = syntheticManifest('episode'), actorSession = session();
  for (const mutate of [
    rows => { rows[3].body.ItemId = id(999); }, rows => { rows[3].body.MediaSourceId = 'foreign-source'; },
    rows => { rows[3].body.PlaySessionId = 'foreign-play-session'; }, rows => { rows[3].body.SessionId = 'foreign-actor-session'; },
    rows => { rows[3].token_sha256 = digest(999); }, rows => { rows[0].completed = false; },
    rows => { rows[3].status = 500; }, rows => { rows[3].ordinal = 0; },
    rows => { rows[1].route = '/Videos/' + id(999) + '/stream'; }, rows => { rows[1].token_sha256 = digest(999); },
    rows => { rows[1].completed = false; }, rows => { delete rows[0].body.MediaSourceId; },
  ]) {
    const rows = lifecycle(manifest, actorSession); mutate(rows);
    assert.equal(candidatePlaybackEvidence(rows, manifest, [actorSession]).passed, false);
  }
});

test('TV browse requires no playback chain or media traffic', () => {
  const manifest = syntheticManifest('tv-browse');
  assert.deepEqual(candidatePlaybackEvidence([], manifest, []), { required: false, passed: true });
  manifest.catalog['tv-browse'] = { id: id(999), type: 'Movie' };
  assert.deepEqual(candidatePlaybackEvidence([], manifest, []), { required: false, passed: true });
});
