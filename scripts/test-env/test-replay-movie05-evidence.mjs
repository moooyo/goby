#!/usr/bin/env node
/** Replay the complete saved movie05 record without changing it or performing client work. */
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { pathToFileURL, fileURLToPath } from 'node:url';

const R = '/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14';
const S = R + '/candidate-core-client-movie-05';
const sha = raw => createHash('sha256').update(raw).digest('hex');
const pin = (filename, sha256) => ({ path: filename, sha256 });
const PINS = {
  manifest: pin(S + '/private/adapter-input.json', '8b0a66d74d903fb209e868ad257c213b793c185ca2f90abbe83e91980fc37ff7'),
  observation: pin(S + '/browser/observation.json', 'bc5ee78f25e2bdb2dbac1c0d5ae19c39bd7370a3829285d11745792aaf8045ef'),
  summary: pin(S + '/browser/summary.json', '5eb0bb0b068e00c72d2b02d130157b14ea47699865c5092a5fd7b1e9a80c452c'),
  sourceBefore: pin(S + '/private/source-before.json', '9a3735122520974a40b86056d878a888bc1aa22bccbe654438c08a9380f3b8aa'),
  sourceAfter: pin(S + '/private/source-after.json', 'be0dbd70d4f7ea7d4343a3ea6259f80be216b9239e7acfa6552b2c7858b33bb0'),
  gatewayIndex: pin(S + '/gateway/index.json', '0d004a59b253c9a665190785a79741f32998213123b6c8988648d640fc23ad18'),
  failure: pin(S + '/private/failure.json', 'f6f9e2f869bc02689b0673f7623d803b8d36cb6022474df9e1caec4ff55bf249'),
  ownedState: pin(R + '/candidate-core-movie05-owned-state-closeout.json', '7c38d00bcb4ece5e056e06a22be848cbd5e76c77f7d5b642d06f656be3dc27ab'),
  serverLogs: pin(R + '/candidate-movie05-server-log-review-01/server-file-review.json', 'd7d2548ea614527f25717355637f78cd35cfeef4009f68e53d80a132a38cf3e9'),
  seed: pin(R + '/candidate-backup-limits-revision-01/private/seed-runtime-binding.json', '92ee92478475390e514f39e554619f322be81062a6d0256820c00bf4c8e0969f'),
  originalRetainedCloseout: pin(R + '/candidate-core-movie04-failure-closeout.json', '5843a790e3c4ba09109d145b64fbda58f94c7ee94092e898ab584bab37880e8d'),
  originalRetainedSnapshot: pin(R + '/candidate-core-client-movie-04/private/source-after.json', '7209b3845d8290cd3883c76f5a95f00066d85d61103b8c5765493472196ad01c'),
};

async function readProtected(filename, privateMode = true) {
  assert(filename.startsWith(R + '/') && !filename.split('/').includes('..'));
  assert.equal(await fs.realpath(filename), filename);
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const before = await handle.stat({ bigint: true });
    assert(before.isFile() && before.uid === 0n && before.nlink === 1n && before.size > 0n && before.size <= (32n << 20n));
    assert(privateMode ? (before.mode & 0o777n) === 0o600n : (before.mode & 0o022n) === 0n);
    const raw = await handle.readFile(), after = await handle.stat({ bigint: true });
    const current = await fs.lstat(filename, { bigint: true });
    for (const key of ['dev', 'ino', 'size', 'mtimeNs', 'ctimeNs', 'mode']) assert(before[key] === after[key] && before[key] === current[key]);
    return raw;
  } finally { await handle.close(); }
}
async function readPin(value) { const raw = await readProtected(value.path); assert.equal(sha(raw), value.sha256); return raw; }

async function main() {
  assert(process.platform === 'linux' && process.getuid?.() === 0 && process.env.SSH_CONNECTION, 'remote_only_replay');
  const args = process.argv.slice(2);
  assert(args.length === 4 && args[0] === '--source' && args[2] === '--output');
  const source = path.resolve(args[1]), output = path.resolve(args[3]), directory = path.dirname(source);
  assert(source.startsWith(R + '/') && output.startsWith(R + '/') && path.basename(source) === 'close-audited-candidate-client.mjs');
  assert.equal(await fs.realpath(path.dirname(output)), path.dirname(output));
  const info = await fs.lstat(path.dirname(output)); assert(info.isDirectory() && info.uid === 0 && (info.mode & 0o777) === 0o700);
  process.umask(0o077);
  const filenames = [source, path.join(directory, 'client-browser-audited-candidate.mjs'), fileURLToPath(import.meta.url),
    ...['client-browser-session-proof.mjs', 'client-browser-playback.mjs', 'client-browser-audio-flow.mjs',
      'client-browser-subtitle-flow.mjs', 'client-browser-tv-flow.mjs'].map(filename => path.join(directory, filename))];
  const sources = Object.fromEntries(await Promise.all(filenames.map(async filename => [filename, sha(await readProtected(filename, false))])));
  const closer = await import(pathToFileURL(source)), adapter = await import(pathToFileURL(filenames[1]));
  const raw = Object.fromEntries(await Promise.all(Object.entries(PINS).map(async ([key, value]) => [key, await readPin(value)])));
  const manifest = closer.strictJSON(raw.manifest), observation = closer.observationJSON(raw.observation, manifest);
  const summary = closer.strictJSON(raw.summary), after = closer.sourceSnapshotJSON(raw.sourceAfter);
  const before = closer.sourceSnapshotJSON(raw.sourceBefore), index = closer.strictJSON(raw.gatewayIndex);
  const failure = closer.strictJSON(raw.failure), owned = closer.strictJSON(raw.ownedState), logs = closer.strictJSON(raw.serverLogs);
  const gateway = closer.strictJSON(await readPin(manifest.gatewayAttestation)), rows = [];
  const expectedFiles = [];
  for (const entry of index.entries) {
    for (const kind of ['intent', 'result']) {
      const name = 'request-' + String(entry.ordinal).padStart(6, '0') + '-' + kind + '.json';
      assert.equal(entry[kind].path, S + '/gateway/private/' + name); expectedFiles.push(name);
    }
    rows.push({ entry, intent: closer.strictJSON(await readPin(entry.intent)), result: closer.strictJSON(await readPin(entry.result)) });
  }
  assert.deepEqual((await fs.readdir(S + '/gateway/private')).sort(), expectedFiles.sort());
  const exchanges = closer.verifyLedgerRows(gateway, index, rows, manifest), tests = [];
  const check = async (name, run) => {
    try { await run(); tests.push({ name, outcome: 'passed' }); }
    catch (error) { tests.push({ name, outcome: 'failed', errorType: error.name,
      failedCheck: /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'saved_replay_assertion_failed' }); }
  };

  await check('all_355_physical_exchanges_keep_their_original_framing_and_counts', () => {
    assert.equal(exchanges.length, 355);
    assert.equal(exchanges.filter(row => row.rejected && !row.handshake).length, 6);
    const media = exchanges.filter(row => row.scope?.kind === 'media');
    assert.equal(media.length, 8); assert.equal(media.filter(row => row.deliveredBodyBytes > 0).length, 7);
    assert.equal(media.find(row => row.ordinal === 292).complete, true);
    assert.equal(media.find(row => row.ordinal === 292).deliveredBodyBytes, 4816896);
  });
  await check('two_real_urlencoded_logins_decode_without_rewriting_wire_bodies', () => {
    const logins = exchanges.filter(row => row.scope?.kind === 'login');
    assert.equal(logins.length, 2);
    for (const exchange of logins) {
      assert.match(exchange.original.get('content-type'), /^application\/x-www-form-urlencoded/i);
      const body = closer.loginRequestBody(exchange);
      assert.equal(body.Username, manifest.actor.username); assert.equal(typeof body.Pw, 'string');
      assert.deepEqual(Object.keys(body).sort(), ['Pw', 'Username']);
      assert(!exchange.requestEntity.toString().startsWith('{'));
    }
  });
  await check('all_eleven_report_bodies_preserve_exact_playback_start_integers', () => {
    const reports = exchanges.filter(row => row.scope?.kind === 'playback_report'); assert.equal(reports.length, 11);
    const observed = observation.requests.filter(row => row.kind === 'playback_report'); assert.equal(observed.length, 11);
    const expected = new Set(['17893014658780000', '17893014737860000']);
    for (const exchange of reports) {
      const body = closer.playbackRequestBody(exchange);
      assert(expected.has(body.PlaybackStartTimeTicks)); assert.equal(typeof body.PositionTicks, 'number');
      assert(observed.some(row => row.payload_base64 === exchange.requestEntity.toString('base64') &&
        row.body.PlaybackStartTimeTicks === body.PlaybackStartTimeTicks));
      assert.throws(() => closer.strictJSON(exchange.requestEntity), /json_unsafe_number/);
    }
    assert.throws(() => closer.strictJSON(raw.observation), /json_unsafe_number/);
  });
  await check('browser_media_candidates_do_not_require_transfer_completion_or_claim_physical_acceptance', () => {
    const media = observation.requests.filter(row => row.kind === 'media');
    assert.equal(media.length, 7); assert(media.every(row => row.completed === false && row.failure_error_text === 'net::ERR_ABORTED'));
    const proof = adapter.candidatePlaybackEvidence(observation.requests, manifest, observation.sessions);
    assert.equal(proof.passed, true); assert.equal(proof.lifecycles.length, 2);
    assert.equal(proof.media_request_ordinals.length, 7);
    assert.equal(proof.evidence_stage, 'owned_browser_context_candidates');
    assert.equal(proof.physical_validation, 'pending_outer_gateway_closure');
    assert.equal(observation.playback_evidence.passed, false);
    assert.equal(summary.playback_evidence.media_event_count, 0);
  });
  await check('recorded_movie_metrics_show_frames_pause_both_seeks_and_resume', () => {
    const video = label => {
      const step = observation.playback.steps.filter(row => row.label === label); assert.equal(step.length, 1);
      const visible = step[0].videos.filter(row => row.visible); assert.equal(visible.length, 1); return visible[0];
    };
    assert.equal(observation.playback.outcome, 'movie_ui_flow_completed');
    for (const [first, last] of [['playing-start', 'playing-advanced'], ['resumed', 'resumed-advanced']]) {
      const a = video(first), b = video(last); assert(b.current_time > a.current_time && b.total_video_frames > a.total_video_frames && !b.paused);
    }
    assert.equal(video('paused').paused, true);
    assert(video('movie-seek-forward-after').current_time > video('movie-seek-forward-before').current_time + 10);
    assert(video('movie-seek-backward-after').current_time < video('movie-seek-backward-before').current_time - 10);
    assert(Math.abs(video('before-stop').current_time - video('resumed').current_time) <= 15);
  });
  await check('saved_server_request_id_proves_empty_response_was_cancelled_without_proving_delivery', async () => {
    const prefix = await readPin(logs.capturedPrefix);
    const exchange = exchanges.find(row => row.ordinal === 339), id = exchange.response.get('x-request-id');
    const lines = prefix.toString('utf8').split('\n').filter(line => line.includes(id)); assert.equal(lines.length, 1);
    const matched = JSON.parse(lines[0]); assert.equal(matched.request_id, id);
    assert.equal(matched.outcome, 'cancelled'); assert.equal(matched.status, 200); assert.equal(matched.bytes, 0);
    assert.equal(exchange.complete, true); assert.equal(exchange.deliveredBodyBytes, 0);
    assert.throws(() => closer.requireMediaGET(exchange), /actual_media_entity_bytes_missing/);
  });
  await check('contained_resume_range_has_one_matching_terminal_context_without_rewriting_either_range', () => {
    const exchange = exchanges.find(row => row.ordinal === 342), matches = closer.mediaContextEvidence(exchange, observation);
    assert.equal(matches.length, 1); assert.equal(matches[0].row.ordinal, 336);
    assert.equal(matches[0].association, 'unique_contained_range_with_terminal_time');
    assert.equal(exchange.original.get('range'), 'bytes=11038080-48786887');
    assert.equal(matches[0].row.headers.range, 'bytes=10682368-');
    for (const ordinal of [288, 289]) {
      const exact = closer.mediaContextEvidence(exchanges.find(row => row.ordinal === ordinal), observation);
      assert.equal(exact.length, 1); assert.equal(exact[0].row.ordinal, 286); assert.equal(exact[0].association, 'exact_range');
    }
  });
  await check('contained_range_matching_rejects_foreign_failed_or_ambiguous_candidates', () => {
    const exchange = exchanges.find(row => row.ordinal === 342);
    for (const status of [401, 403, 500]) assert.equal(closer.mediaContextEvidence({ ...exchange, response: { ...exchange.response, status } }, observation).length, 0);
    assert.equal(closer.mediaContextEvidence({ ...exchange, scope: { ...exchange.scope, allowed: false } }, observation).length, 0);
    for (const mutate of [
      row => { row.token_sha256 = '0'.repeat(64); }, row => { row.headers.range = 'bytes=20000000-'; },
      row => { row.failed_elapsed_ms = row.elapsed_ms - 1; }, row => { row.status = 500; },
      row => { row.allowed = false; }, row => { row.failure_error_text = 'net::ERR_FAILED'; },
      row => { row.headers.range = 'bytes=0-100'; }, row => { row.headers.range = 'bytes=-100'; },
      row => { row.headers.range = 'bytes=0-100,200-300'; }, row => { row.headers.range = 'bytes=9007199254740992-'; },
      row => { row.failed_elapsed_ms = row.elapsed_ms - 5; row.elapsed_ms -= 20; },
    ]) {
      const altered = structuredClone(observation); mutate(altered.requests.find(row => row.ordinal === 336));
      assert.equal(closer.mediaContextEvidence(exchange, altered).length, 0);
    }
    const duplicate = structuredClone(observation);
    duplicate.requests.push({ ...structuredClone(duplicate.requests.find(row => row.ordinal === 336)), ordinal: 999 });
    assert.equal(closer.mediaContextEvidence(exchange, duplicate).length, 0);
  });
  await check('all_six_physical_partial_responses_keep_distinct_ordinals_and_partial_flags', () => {
    const media = exchanges.filter(row => row.scope?.kind === 'media'), partial = media.filter(row => !row.complete);
    assert.equal(partial.length, 6);
    for (const exchange of partial) {
      const stopped = exchanges.filter(row => row.tokenHash === exchange.tokenHash && row.scope?.kind === 'playback_report' && /\/Stopped\/?$/i.test(row.scope.route));
      assert.equal(stopped.length, 1); assert.equal(stopped[0].complete, true);
      const login = { chains: [{ stopped: stopped.map(row => ({ exchange: row })) }], media: media.filter(row => row.tokenHash === exchange.tokenHash) };
      const explanation = closer.explainMediaPartial(exchange, observation, login);
      assert.equal(explanation.ordinal, exchange.ordinal); assert.equal(explanation.completeHTTP, false);
      assert.equal(explanation.responseForwardedComplete, false); assert(explanation.deliveredBodyBytes > 0);
    }
  });
  await check('full_physical_acceptance_stays_closed_without_an_explicit_cancelled_empty_response_contract', () => {
    assert.throws(() => closer.analyzePhysical(exchanges, manifest, observation, after), /completed_media_get_has_no_entity_bytes/);
  });
  await check('saved_cancellation_evidence_closes_physical_and_durable_analysis_without_changing_failed_ui', async () => {
    const prefix = await readPin(logs.capturedPrefix), lines = prefix.toString('utf8').split('\n'), events = [];
    for (const media of logs.mediaRequests) {
      const matched = lines.filter(line => line.includes(media.requestId)); assert.equal(matched.length, 1);
      const event = JSON.parse(matched[0]); assert.equal(event.request_id, media.requestId); events.push(event);
    }
    const savedServerLog = { events, byRequestId: new Map(events.map(event => [event.request_id, event])) };
    // This reuses the prior immutable log review. It does not manufacture
    // pre-run log captures or a version-3 input for the old version-2 run.
    const physical = closer.analyzePhysical(exchanges, manifest, observation, after, savedServerLog);
    assert.equal(physical.exchanges.length, 355); assert.equal(physical.logins.length, 2); assert.equal(physical.chains.length, 2);
    assert.equal(physical.cancelledEmptyResponses.length, 1); assert.equal(physical.cancelledEmptyResponses[0].ordinal, 339);
    assert.equal(physical.cancelledEmptyResponses[0].contributesMediaDelivery, false);
    assert(physical.chains.every(chain => chain.media.every(exchange => exchange.deliveredBodyBytes > 0 && exchange.ordinal !== 339)));
    const reused = exchanges.map(exchange => exchange.ordinal !== 338 ? exchange : { ...exchange, response: { ...exchange.response,
      headers: new Map([...exchange.response.headers, ['x-request-id', [physical.cancelledEmptyResponses[0].requestId]]]) } });
    assert.throws(() => closer.analyzePhysical(reused, manifest, observation, after, savedServerLog), /cancelled_media_request_id_reused/);
    const retained = { closeout: closer.strictJSON(raw.originalRetainedCloseout), snapshot: closer.sourceSnapshotJSON(raw.originalRetainedSnapshot) };
    const durable = closer.verifyDurable(before, after, manifest, closer.strictJSON(raw.seed), physical, retained);
    assert.equal(durable.countedPlays, 2); assert.equal(durable.selectedUserdata.play_count, 2);
    assert.equal(durable.selectedUserdata.playback_position_ticks, 1217878390);
    assert.equal(durable.retainedBaselineExpiration.playSessionId, closer.RETAINED_PLAY);
    assert.throws(() => closer.verifyUI(observation, manifest, physical), /client_ui_or_cleanup_incomplete/);
    assert.equal(observation.outcome, 'failed'); assert.equal(failure.workers.browser.terminal.ExecMainStatus, '1');
  });
  await check('unknown_page_errors_and_original_failed_exit_are_preserved', () => {
    assert.equal(observation.outcome, 'failed'); assert.equal(observation.failure, 'candidate_playback_chain_incomplete');
    assert.deepEqual(observation.page_errors.map(row => row.elapsed_ms), [16877, 16993, 24867, 24902]);
    assert(observation.page_errors.every(row => Object.keys(row).length === 1));
    assert.equal(failure.workers.browser.terminal.ExecMainStatus, '1');
    assert.throws(() => closer.verifyUI(observation, manifest, { exchanges, logins: [] }), /client_ui_or_cleanup_incomplete/);
  });
  await check('owned_state_closure_is_reused_without_changing_the_saved_database_snapshots', () => {
    assert.equal(owned.status, 'owned_state_closed_client_acceptance_pending'); assert.equal(owned.clientAcceptance, false);
    assert.equal(owned.inputEvidence.before.sha256, PINS.sourceBefore.sha256);
    assert.equal(owned.inputEvidence.after.sha256, PINS.sourceAfter.sha256);
    assert.equal(Object.keys(before.tables).length, 35); assert.equal(Object.keys(after.tables).length, 35);
    assert(after.tables.sessions.every(row => row.revoked_at !== null));
    assert.equal(after.tables.play_sessions.filter(row => row.state === 'Stopped' && row.counted).length, 2);
    assert.equal(after.tables.user_item_data[0].play_count, 2);
  });
  const unchanged = (await Promise.all(Object.entries(sources).map(async ([filename, digest]) => sha(await readProtected(filename, false)) === digest))).every(Boolean);
  for (const value of Object.values(PINS)) await readPin(value);
  const result = { kind: 'saved-movie05-evidence-regression', source: pin(source, sources[source]), sources, replayedPins: PINS,
    testCount: tests.length, passed: tests.filter(row => row.outcome === 'passed').length, failed: tests.filter(row => row.outcome !== 'passed').length,
    sourceUnchanged: unchanged, tests, originalOutcome: observation.outcome, originalBrowserExitCode: 1,
    clientAcceptance: false, newBrowserRuns: 0, httpCalls: 0, sqlCalls: 0, serviceActions: 0,
    remainingAcceptanceGaps: ['Unknown historical page-error causes.', 'A fresh version-3 controller run with admitted occupied state and real before/after server log captures.'] };
  const bytes = Buffer.from(JSON.stringify(result, null, 2) + '\n');
  await fs.writeFile(output, bytes, { flag: 'wx', mode: 0o600 });
  process.stdout.write(JSON.stringify({ output: pin(output, sha(bytes)), testCount: result.testCount, passed: result.passed,
    failed: result.failed, sourceUnchanged: unchanged, clientAcceptance: false }) + '\n');
  if (result.failed || !unchanged) process.exitCode = 1;
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try { await main(); } catch (error) { process.stderr.write(JSON.stringify({ status: 'replay_setup_failed',
    errorType: error.name, failedCheck: /^[a-z0-9_]+$/.test(error.message ?? '') ? error.message : 'saved_replay_setup_failed',
    clientAcceptance: false }) + '\n'); process.exitCode = 1; }
}
