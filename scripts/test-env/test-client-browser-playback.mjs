/** Pure lifecycle regressions with synthetic controls and original-response stand-ins. */
import test from 'node:test';
import assert from 'node:assert/strict';
import { runInNewContext } from 'node:vm';
import { movieFailureCode, waitMovieControl, movieItemFromLocation, moviePlaybackIdentity,
  observeMovieResponse, completeMovieCleanup, runMovieWorkflow } from './client-browser-playback.mjs';

const TARGET = new URL('http://127.0.0.1:19196/web/index.html');
const ITEM = 'a'.repeat(32), OTHER = 'b'.repeat(32), SOURCE = 'mediasource_' + ITEM;
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const never = () => new Promise(() => {});
const tick = () => new Promise(resolve => setImmediate(resolve));

function control(count = 1) {
  return { first() { return this; }, async waitFor() {}, async count() { return count; } };
}

function request(event, body = {}, { origin = TARGET.origin, frame = 'main', failure = null } = {}) {
  const suffix = { started: 'Playing', stopped: 'Playing/Stopped', logout: 'Logout' }[event];
  return { url: () => origin + '/emby/Sessions/' + suffix, method: () => 'POST', frame: () => frame,
    postDataBuffer: () => Buffer.from(JSON.stringify(body)), failure: () => failure };
}

function response(event, body, options = {}) {
  const req = request(event, body, options);
  return { request: () => req, url: req.url, status: () => options.status ?? 204,
    finished: options.finished ?? (async () => null) };
}

function responsePage() {
  const result = deferred(), deadline = deferred(); let predicate;
  return { result, deadline, mainFrame: () => 'main',
    waitForResponse(match) { predicate = match; return result.promise; },
    waitForTimeout() { return deadline.promise; },
    accepts(value) { return predicate(value); } };
}

test('movie04 ordering waits through absent play controls before accepting the later unique Play button', async () => {
  const mounted = deferred(), waiting = deferred(), observations = []; let visible = false, counted = false;
  const play = { first() { return this; }, async waitFor() { waiting.resolve(); await mounted.promise; },
    async count() { counted = true; return visible ? 1 : 0; } };
  const beginning = { first() { return this; }, waitFor: never, async count() { return 0; } };
  const pending = waitMovieControl([{ key: 'from_beginning', locator: beginning }, { key: 'play', locator: play }],
    { record: value => observations.push(value) });
  await waiting.promise; await tick();
  assert.equal(counted, false);
  visible = true; mounted.resolve();
  assert.equal((await pending).key, 'play');
  assert.deepEqual(observations, [[{ control: 'from_beginning', count: 0 }, { control: 'play', count: 1 }]]);
});

test('ready controls preserve priority, record exact counts, and reject ambiguity or budget exhaustion', async () => {
  const observations = [];
  assert.equal((await waitMovieControl([{ key: 'beginning', locator: control() }, { key: 'play', locator: control() }])).key, 'beginning');
  await assert.rejects(waitMovieControl([{ key: 'play', locator: control(2) }], { record: value => observations.push(value) }),
    { message: 'movie_control_not_unique' });
  assert.deepEqual(observations, [[{ control: 'play', count: 2 }]]);
  const unavailable = { first() { return this; }, async waitFor() { throw new Error('candidate_time_budget_exhausted'); }, async count() { return 0; } };
  await assert.rejects(waitMovieControl([{ key: 'play', locator: unavailable }]), { message: 'candidate_time_budget_exhausted' });
  assert.equal(movieFailureCode(new AggregateError([new Error('private detail'), new Error('candidate_time_budget_exhausted')])), 'candidate_time_budget_exhausted');
  assert.equal(movieFailureCode(new Error('synthetic private transport detail')), 'movie_operation_failed');
});

test('the observed item route binds the same-origin item identity without retaining URL query values', () => {
  assert.equal(movieItemFromLocation(TARGET.href + '#!/item?id=' + ITEM + '&serverId=synthetic', TARGET), ITEM);
  for (const location of [TARGET.href + '#!/home', TARGET.href + '#!/item?id=bad',
    TARGET.href + '#!/item?id=' + ITEM + '&id=' + OTHER, 'https://external.invalid/web/index.html#!/item?id=' + ITEM])
    assert.equal(movieItemFromLocation(location, TARGET), null);
});

test('playback identity reads only bounded fixed-route JSON fields and omits unrelated private fields', () => {
  const body = { ItemId: ITEM, MediaSourceId: SOURCE, PlaySessionId: 'play_first', PositionTicks: 123,
    AccessToken: 'synthetic-private-token', DeliveryUrl: 'https://private.invalid/?api_key=synthetic' };
  const value = moviePlaybackIdentity(request('started', body), TARGET, 'started');
  assert.deepEqual(value, { item_id: ITEM, media_source_id: SOURCE, play_session_id: 'play_first', session_id: null, user_id: null, position_ticks: 123 });
  assert.equal(JSON.stringify(value).includes('synthetic-private'), false);
  for (const mutate of [row => { delete row.PlaySessionId; }, row => { row.ItemId = 'wrong'; }, row => { row.PositionTicks = -1; }]) {
    const invalid = { ...body }; mutate(invalid);
    assert.throws(() => moviePlaybackIdentity(request('started', invalid), TARGET, 'started'));
  }
  assert.throws(() => moviePlaybackIdentity(request('started', body, { origin: 'https://external.invalid' }), TARGET, 'started'), /movie_report_scope_invalid/);
  const oversized = request('started', body); oversized.postDataBuffer = () => Buffer.alloc(65537);
  assert.throws(() => moviePlaybackIdentity(oversized, TARGET, 'started'), /movie_report_body_unavailable/);
});

test('a response acknowledgement remains pending until its actual finished result succeeds', async () => {
  const page = responsePage(), finished = deferred(), body = { ItemId: ITEM, MediaSourceId: SOURCE, PlaySessionId: 'play_first' };
  const armed = observeMovieResponse(page, TARGET, 'started');
  assert.equal(page.accepts(response('started', body, { frame: 'child' })), false);
  assert.equal(page.accepts(response('started', body, { origin: 'https://external.invalid' })), false);
  const actual = response('started', body, { finished: () => finished.promise }); assert.equal(page.accepts(actual), true);
  page.result.resolve(actual); let complete = false;
  const pending = armed.wait({ itemId: ITEM }).then(value => { complete = true; return value; });
  await tick(); assert.equal(complete, false);
  finished.resolve(null); const value = await pending;
  assert.equal(value.response_complete, true); assert.equal(value.identity.play_session_id, 'play_first');
  assert.equal(await armed.wait({ itemId: ITEM }), value);
});

test('completion rejection, returned errors, timeouts, wrong status, and mismatched Stopped identities stay failures', async () => {
  const identity = { item_id: ITEM, media_source_id: SOURCE, play_session_id: 'play_first', session_id: null, user_id: null };
  const body = { ItemId: ITEM, MediaSourceId: SOURCE, PlaySessionId: 'play_first' };
  for (const [options, expected] of [
    [{ finished: async () => new Error('private detail') }, 'movie_response_incomplete'],
    [{ finished: async () => { throw new Error('private detail'); } }, 'movie_response_completion_failed'],
    [{ status: 500 }, 'movie_stopped_response_not_204'],
  ]) {
    const page = responsePage(), armed = observeMovieResponse(page, TARGET, 'stopped'); page.result.resolve(response('stopped', body, options));
    await assert.rejects(armed.wait({ identity }), { message: expected });
  }
  for (const field of ['ItemId', 'MediaSourceId', 'PlaySessionId']) {
    const page = responsePage(), armed = observeMovieResponse(page, TARGET, 'stopped'), wrong = { ...body, [field]: field === 'ItemId' ? OTHER : 'different' };
    page.result.resolve(response('stopped', wrong));
    await assert.rejects(armed.wait({ identity }), { message: 'movie_stopped_identity_mismatch' });
  }
  const timed = responsePage(), armed = observeMovieResponse(timed, TARGET, 'stopped'); timed.result.resolve(response('stopped', body, { finished: never }));
  const pending = armed.wait({ identity }); timed.deadline.resolve();
  await assert.rejects(pending, { message: 'movie_response_completion_timeout' });
  const budget = responsePage(), budgeted = observeMovieResponse(budget, TARGET, 'stopped'); budget.result.resolve(response('stopped', body, { finished: never }));
  const bounded = budgeted.wait({ identity }); budget.deadline.reject(new Error('candidate_time_budget_exhausted'));
  await assert.rejects(bounded, { message: 'candidate_time_budget_exhausted' });
  const synchronous = responsePage(); synchronous.waitForTimeout = () => { throw new Error('candidate_time_budget_exhausted'); };
  const guarded = observeMovieResponse(synchronous, TARGET, 'stopped');
  synchronous.result.resolve(response('stopped', body, { finished: async () => { throw new Error('late private error'); } }));
  await assert.rejects(guarded.wait({ identity }), { message: 'candidate_time_budget_exhausted' });
  await tick();
});

test('prearmed response failures are handled even when the UI action fails before waiting', async () => {
  const page = responsePage(), armed = observeMovieResponse(page, TARGET, 'logout');
  page.result.reject(new Error('candidate_time_budget_exhausted')); await tick();
  await assert.rejects(armed.wait(), { message: 'candidate_time_budget_exhausted' });
});

test('cleanup attempts logout after a failed stop and retains safe budget failure codes', async () => {
  const calls = [], errors = [];
  await completeMovieCleanup({ async stop() { calls.push('stop'); throw new Error('candidate_time_budget_exhausted'); },
    async logout() { calls.push('logout'); }, onFailure(operation, code) { errors.push({ operation, code }); } });
  assert.deepEqual(calls, ['stop', 'logout']);
  assert.deepEqual(errors, [{ operation: 'stop', code: 'candidate_time_budget_exhausted' }]);
});

function movieFixture({ failedSnapshot = null, failFirstStopCompletion = false, failRepeatLogin = false, changedReloginItem = false,
  reusedPlaySession = false, mediaStopGate = null, menuGate = null } = {}) {
  let state = 'home', video = null, menu = false, selectedItem = ITEM, position = 0, playNumber = 0, loginNumber = 1;
  const waiters = [], calls = [], snapshots = [], report = { requests: [], logout: { attempted: false } };
  const mediaStopWaiting = deferred(), menuWaiting = deferred();
  const invoke = (fn, input) => runInNewContext('(' + fn.toString() + ')(input)', { document: document(), input });
  const visibleButton = key => {
    if (key === 'card' || key === 'image') return state === 'home' ? 1 : 0;
    if (key === 'Settings') return state !== 'login' ? 1 : 0;
    if (key === 'Sign Out') return menu === true && state !== 'login' ? 1 : 0;
    if (key === 'Play') return state === 'detail' && position === 0 || video?.paused ? 1 : 0;
    if (key === 'Resume') return state === 'detail' && position > 0 ? 1 : 0;
    if (key === 'From Beginning') return 0;
    if (key === 'Pause' || key === 'Back') return video ? 1 : 0;
    return 0;
  };
  const currentBody = () => ({ ItemId: selectedItem, MediaSourceId: 'mediasource_' + selectedItem,
    PlaySessionId: 'play_' + (reusedPlaySession ? 1 : playNumber), PositionTicks: Math.round((video?.currentTime ?? position) * 10000000) });
  const emit = (event, body, options) => {
    const actual = response(event, body, options);
    const index = waiters.findIndex(waiter => waiter.predicate(actual));
    assert.ok(index >= 0, event + ' must be observed before its UI action');
    waiters.splice(index, 1)[0].resolve(actual);
  };
  const click = async key => {
    calls.push(key);
    if (key === 'card' || key === 'image') { state = 'detail'; return; }
    if (key === 'Settings') { menu = menuGate && calls.filter(value => value === 'Settings').length === 1 ? 'pending' : !menu; return; }
    if (key === 'Sign Out') { emit('logout', {}); state = 'login'; menu = false; video = null; return; }
    if (key === 'Pause') { video.paused = true; return; }
    if (key === 'Play' && video) { video.paused = false; return; }
    if (key === 'Play' || key === 'Resume') {
      playNumber++; state = 'playing'; video = { currentTime: position || 1.1, duration: 600, paused: false, ended: false, seeking: false,
        readyState: 4, networkState: 2, videoWidth: 320, videoHeight: 180, frames: 40,
        getVideoPlaybackQuality() { return { totalVideoFrames: this.frames, droppedVideoFrames: 0 }; },
        getBoundingClientRect() { return { width: 800, height: 450 }; }, getClientRects() { return [{}]; } };
      emit('started', currentBody()); return;
    }
    if (key === 'Back') {
      position = video.currentTime; const body = currentBody();
      if (mediaStopGate && playNumber === 1) state = 'stopping';
      else { video = null; state = 'detail'; }
      emit('stopped', body, failFirstStopCompletion && playNumber === 1 ? { finished: async () => { throw new Error('synthetic transport detail'); } } : undefined);
    }
  };
  const slider = { className: 'videoOsdPositionSlider', getAttribute: () => 'Seek', getClientRects: () => video ? [{}] : [] };
  function document() {
    return { querySelectorAll(selector) {
      if (selector === 'video' || selector === 'video,audio') return video ? [video] : [];
      if (selector === 'input[type="range"]') return video ? [slider] : [];
      return ['Play', 'Resume', 'Pause', 'Back', 'Settings', 'Sign Out'].filter(key => visibleButton(key)).map(key => ({
        tagName: 'BUTTON', className: 'synthetic-control', innerText: key, getClientRects: () => [{}],
        getAttribute: name => ['aria-label', 'title'].includes(name) ? key : name === 'type' ? 'button' : null }));
    } };
  }
  const locator = (kind, key) => ({
    filter() { return this; }, first() { return this; }, nth() { return this; },
    async count() {
      if (kind === 'title') return ['home', 'detail'].includes(state) ? 1 : 0;
      if (kind === 'manual') return state === 'login' ? 1 : 0;
      if (kind === 'form') return 0;
      if (kind === 'video' || kind === 'ranges') return video ? 1 : 0;
      return visibleButton(key);
    },
    async waitFor() {
      if (kind === 'button' && key === 'Sign Out' && menu === 'pending') { menuWaiting.resolve(); await menuGate.promise; menu = true; }
      if (!await this.count()) await never();
    },
    locator(selector) {
      if (selector.startsWith('xpath=ancestor-or-self')) return locator('button', 'card');
      if (selector.startsWith('xpath=ancestor::')) return locator('card');
      assert.equal(selector, 'button.cardContent-button'); return locator('button', 'image');
    },
    async boundingBox() { return { x: 0, y: 0, width: kind === 'ranges' ? 100 : 800, height: 100 }; },
    async evaluate(fn) { return invoke(fn, slider); },
    async click(options) { if (kind === 'ranges') { video.currentTime = 600 * options.position.x / 100; return; } await click(key); },
  });
  const page = {
    mainFrame: () => 'main', mouse: { async move() {} },
    url: () => TARGET.href + (state === 'home' ? '#!/home' : state === 'login' ? '#!/login' : '#!/item?id=' + selectedItem),
    getByText(text) { return locator(text === 'Manual Login' ? 'manual' : 'title'); },
    getByRole(role, { name }) { assert.equal(role, 'button'); return locator('button', typeof name === 'string' ? name : /Resume/.test(name.source) ? 'Resume' : 'Sign Out'); },
    locator(selector) { return locator(selector.startsWith('video') ? 'video' : selector.startsWith('input') ? 'ranges' : 'form'); },
    async waitForURL(predicate) { assert.equal(predicate(new URL(this.url())), true); },
    async evaluate(fn, input) { return invoke(fn, input); },
    async waitForFunction(fn, input) {
      if (input && Object.hasOwn(input, 'seconds')) { video.currentTime = input.time + input.seconds + 0.1; video.frames = input.frames + 100; }
      if (!invoke(fn, input) && state === 'stopping') {
        mediaStopWaiting.resolve(); await mediaStopGate.promise; video.paused = true; state = 'detail';
      }
      assert.equal(invoke(fn, input), true, 'synthetic UI completion condition');
    },
    waitForResponse(predicate) { const item = deferred(); waiters.push({ predicate, resolve: item.resolve }); return item.promise; },
    waitForTimeout: never,
  };
  return { page, report, calls, snapshots, target: TARGET, mediaStopWaiting, menuWaiting,
    async snapshot(label) { snapshots.push(label); if (label === failedSnapshot || Array.isArray(failedSnapshot) && failedSnapshot.includes(label)) throw new Error('synthetic private screenshot detail'); },
    async repeatLogin() { loginNumber++; state = 'home'; menu = false; if (changedReloginItem) selectedItem = OTHER;
      if (failRepeatLogin) throw new Error('synthetic login completion detail'); },
    state: () => ({ state, video: Boolean(video), loginNumber, playNumber }),
  };
}

test('the complete original contract runs two independent play lifecycles without caller body or completed fields', async () => {
  const fixture = movieFixture(); await runMovieWorkflow(fixture);
  assert.equal(fixture.report.playback.outcome, 'movie_ui_flow_completed');
  assert.equal(fixture.report.playback.phase, 'complete'); assert.equal(fixture.report.playback.item_id, ITEM);
  assert.deepEqual(fixture.report.playback.lifecycles.map(row => [row.play_session_id, row.started_complete, row.stopped_complete]),
    [['play_1', true, true], ['play_2', true, true]]);
  assert.equal(fixture.calls.filter(key => key === 'Back').length, 2); assert.equal(fixture.calls.filter(key => key === 'Sign Out').length, 2);
  assert.equal(fixture.calls.filter(key => key === 'card').length, 2); assert.deepEqual(fixture.report.requests, []);
  for (const label of ['movie-home', 'movie-detail', 'movie-playing', 'movie-seek-forward', 'movie-seek-backward', 'movie-stopped',
    'movie-before-relogin', 'movie-relogin-home', 'movie-before-resume', 'movie-resumed', 'movie-sign-out-after']) assert.ok(fixture.snapshots.includes(label), label);
  assert.equal(fixture.report.logout_history.length, 2); assert.equal(fixture.state().state, 'login');
});

test('an incomplete existing Stop is not fabricated or replaced by another Back during cleanup', async () => {
  const fixture = movieFixture({ failFirstStopCompletion: true });
  await assert.rejects(runMovieWorkflow(fixture), { message: 'movie_response_completion_failed' });
  assert.equal(fixture.calls.filter(key => key === 'Back').length, 1); assert.equal(fixture.calls.filter(key => key === 'Sign Out').length, 1);
  assert.equal(fixture.report.playback.lifecycles[0].stopped_complete, false);
  assert.equal(fixture.report.playback.cleanup_result, 'ui_logout_completed_stop_evidence_incomplete');
  assert.deepEqual(fixture.report.playback.failure, { phase: 'stop', operation: 'stopped_complete', code: 'movie_response_completion_failed' });
});

test('an observation failure preserves its original phase while cleanup still stops and logs out', async () => {
  const fixture = movieFixture({ failedSnapshot: ['movie-before-stop', 'movie-blocked-cleanup-before', 'movie-blocked-cleanup-menu', 'movie-blocked-cleanup-after'] });
  await assert.rejects(runMovieWorkflow(fixture), { message: 'movie_snapshot_movie_before_stop_failed' });
  assert.equal(fixture.calls.filter(key => key === 'Back').length, 1); assert.equal(fixture.calls.filter(key => key === 'Sign Out').length, 1);
  assert.equal(fixture.report.playback.failure.phase, 'stop'); assert.equal(fixture.report.playback.failure.operation, 'snapshot_movie_before_stop');
  assert.equal(fixture.report.playback.lifecycles[0].stopped_complete, true);
  assert.equal(fixture.report.playback.observation_errors.length, 3);
});

test('a failed relogin callback cannot reuse the first session logout state', async () => {
  const fixture = movieFixture({ failRepeatLogin: true });
  await assert.rejects(runMovieWorkflow(fixture), { message: 'movie_repeat_login_failed' });
  assert.equal(fixture.calls.filter(key => key === 'Sign Out').length, 2); assert.equal(fixture.report.logout_history.length, 2);
  assert.equal(fixture.report.playback.failure.phase, 'relogin'); assert.equal(fixture.state().playNumber, 1);
});

test('the shared relogin navigation rejects another item before resuming playback', async () => {
  const fixture = movieFixture({ changedReloginItem: true });
  await assert.rejects(runMovieWorkflow(fixture), { message: 'movie_relogin_item_changed' });
  assert.equal(fixture.state().playNumber, 1); assert.equal(fixture.calls.filter(key => key === 'Sign Out').length, 2);
});

test('relogin cannot count a reused play session as a second independent lifecycle', async () => {
  const fixture = movieFixture({ reusedPlaySession: true });
  await assert.rejects(runMovieWorkflow(fixture), { message: 'movie_relogin_play_session_reused' });
  assert.equal(fixture.report.playback.lifecycles.length, 1);
  assert.equal(fixture.calls.filter(key => key === 'Back').length, 2); assert.equal(fixture.calls.filter(key => key === 'Sign Out').length, 2);
});

test('a complete Stopped response does not advance to logout until retained media is actually paused', async () => {
  const gate = deferred(), fixture = movieFixture({ mediaStopGate: gate });
  const pending = runMovieWorkflow(fixture);
  await fixture.mediaStopWaiting.promise; await tick();
  assert.equal(fixture.report.playback.lifecycles[0].stopped_complete, false);
  assert.equal(fixture.calls.includes('Sign Out'), false);
  gate.resolve(); await pending;
  assert.equal(fixture.report.playback.outcome, 'movie_ui_flow_completed');
});

test('cleanup waits for an already requested account menu instead of toggling Settings again after snapshot failure', async () => {
  const gate = deferred(), fixture = movieFixture({ menuGate: gate, failedSnapshot: 'movie-relogin-sign-out-menu' });
  const pending = runMovieWorkflow(fixture);
  const rejected = assert.rejects(pending, { message: 'movie_snapshot_movie_relogin_sign_out_menu_failed' });
  await fixture.menuWaiting.promise; await tick();
  assert.equal(fixture.calls.filter(key => key === 'Settings').length, 1);
  assert.equal(fixture.calls.includes('Sign Out'), false);
  gate.resolve(); await rejected;
  assert.equal(fixture.calls.filter(key => key === 'Sign Out').length, 1);
  assert.equal(fixture.report.playback.failure.phase, 'relogin');
});
