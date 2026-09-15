/** Exercise only the production subtitle entry; stop deliberately after its play action. */
import test from 'node:test';
import assert from 'node:assert/strict';
import { runSubtitleUI } from './client-browser-subtitle-flow.mjs';

const TARGET = new URL('http://127.0.0.1:19196/web/index.html');
const MOVIE_ID = 'a'.repeat(32), OTHER_ID = 'b'.repeat(32);
const deferred = () => { let resolve; const promise = new Promise(done => { resolve = done; }); return { promise, resolve }; };
const tick = () => new Promise(resolve => setImmediate(resolve));

function entryFixture({ counts = { 'From Beginning': 0, Play: 1 }, mounted = false, routeItem = MOVIE_ID } = {}) {
  const gate = deferred(), waiting = deferred(), events = [], clicks = [], report = { requests: [] };
  const stop = new Error('synthetic stop after subtitle entry'), wrongRoute = new Error('synthetic item URL wait expired');
  let location = TARGET.href + '#!/home', listener = null, detached = false, followupCalls = 0;
  const visible = name => mounted ? counts[name] ?? 0 : 0;
  const action = name => ({
    filter(options) { assert.deepEqual(options, { visible: true }); return this; },
    first() { return this; },
    async waitFor(options) {
      assert.deepEqual(options, { state: 'visible', timeout: 10000 });
      events.push('wait:' + name); waiting.resolve();
      if (!mounted) await gate.promise;
      if (visible(name) === 0) throw new Error('synthetic action is absent');
    },
    async count() { events.push('count:' + name); return visible(name); },
    async click(options) {
      assert.deepEqual(options, { timeout: 8000 }); assert.equal(visible(name), 1);
      clicks.push(name);
    },
  });
  const page = {
    getByText(text, options) {
      assert.equal(text, 'M3e Client Movie'); assert.deepEqual(options, { exact: true });
      return {
        filter(options) { assert.deepEqual(options, { visible: true }); return this; }, first() { return this; },
        async waitFor(options) { assert.deepEqual(options, { state: 'visible', timeout: 20000 }); },
        locator(selector) {
          assert.equal(selector, 'xpath=ancestor-or-self::*[self::button or self::a][1]');
          return { async click() { clicks.push('movie-card'); location = TARGET.href + '#!/item?id=' + routeItem; } };
        },
      };
    },
    getByRole(role, options) {
      assert.equal(role, 'button'); assert.equal(options.exact, true);
      assert.ok(['From Beginning', 'Play'].includes(options.name)); return action(options.name);
    },
    url() { return location; },
    async waitForURL(predicate, options) {
      events.push('item-url'); assert.deepEqual(options, { timeout: 20000 });
      if (!predicate(new URL(location))) throw wrongRoute;
    },
    async waitForTimeout(milliseconds) { events.push('fixed-delay:' + milliseconds); },
    locator() {
      return { async evaluateAll() {
        return mounted ? Object.entries(counts).flatMap(([name, count]) => Array.from({ length: count }, () => ({ tag: 'button', text: name })))
          : [{ tag: 'button', text: 'M3e Client Movie' }, { tag: 'button', text: 'M3e Client Movie' }];
      } };
    },
    async waitForFunction() { followupCalls++; throw stop; },
    async evaluate() { throw new Error('synthetic diagnostic media is unavailable'); },
  };
  const context = {
    on(event, callback) { assert.equal(event, 'response'); assert.equal(listener, null); listener = callback; },
    off(event, callback) { assert.equal(event, 'response'); assert.equal(callback, listener); detached = true; },
  };
  return { input: { page, context, report, target: TARGET, movieId: MOVIE_ID, async snapshot(label) { events.push('snapshot:' + label); } },
    page, report, events, clicks, stop, wrongRoute, waiting, visible,
    mount() { mounted = true; gate.resolve(); }, state: () => ({ mounted, detached, followupCalls }) };
}

test('subtitle entry waits beyond the movie06-style Home transition before choosing its actual play action', async () => {
  for (const expected of ['Play', 'From Beginning']) {
    const fixture = entryFixture({ counts: { 'From Beginning': expected === 'From Beginning' ? 1 : 0, Play: 1 } });
    const finished = runSubtitleUI(fixture.input).then(() => null, error => error);
    assert.equal(await Promise.race([fixture.waiting.promise.then(() => 'waiting'), finished.then(() => 'finished')]), 'waiting');
    await tick();
    assert.equal(fixture.page.url(), TARGET.href + '#!/item?id=' + MOVIE_ID);
    assert.equal(fixture.state().mounted, false);
    assert.deepEqual(fixture.report.subtitle_flow.steps[0].controls.map(row => row.text), ['M3e Client Movie', 'M3e Client Movie']);
    assert.equal(fixture.events.some(event => event.startsWith('count:')), false);
    // The former fixed delay still leaves both actions absent in this saved failure ordering.
    await fixture.page.waitForTimeout(500);
    assert.equal(fixture.visible('From Beginning'), 0); assert.equal(fixture.visible('Play'), 0);
    assert.deepEqual(fixture.clicks, ['movie-card']); assert.equal(fixture.state().followupCalls, 0);
    fixture.mount();
    assert.equal(await finished, fixture.stop);
    assert.deepEqual(fixture.clicks, ['movie-card', expected]);
    assert.equal(fixture.state().followupCalls, 1); assert.equal(fixture.state().detached, true);
  }
});

test('duplicate From Beginning cannot fall back to a unique Play button', async () => {
  const fixture = entryFixture({ counts: { 'From Beginning': 2, Play: 1 }, mounted: true });
  await assert.rejects(runSubtitleUI(fixture.input), { message: 'movie_control_not_unique' });
  assert.deepEqual(fixture.clicks, ['movie-card']); assert.equal(fixture.state().followupCalls, 0);
  assert.equal(fixture.report.subtitle_flow.phase, 'start_movie'); assert.equal(fixture.state().detached, true);
});

test('subtitle entry requires the declared movie item before considering playback controls', async () => {
  const fixture = entryFixture({ routeItem: OTHER_ID, mounted: true });
  await assert.rejects(runSubtitleUI(fixture.input), error => error === fixture.wrongRoute);
  assert.deepEqual(fixture.clicks, ['movie-card']); assert.equal(fixture.events.some(event => event.startsWith('wait:')), false);
  assert.equal(fixture.state().followupCalls, 0); assert.equal(fixture.state().detached, true);
});

function completeSubtitleFixture({ missingVttOpening = false, visibleAfterOff = false } = {}) {
  const report = { requests: [] }, snapshots = [], seeks = [], events = [];
  let location = TARGET.href + '#!/home', time = 1, paused = false, selected = 'Off', listener = null, detached = false;
  const response = index => {
    const bytes = Buffer.from('WEBVTT\n\n00:00.000 --> 00:05.000\nOpening subtitle\n\n01:58.000 --> 02:08.000\nForward seek subtitle\n');
    return { url: () => TARGET.origin + '/emby/Videos/' + MOVIE_ID + '/mediasource_' + MOVIE_ID + '/Subtitles/' + index + '/Stream.vtt',
      request: () => ({ method: () => 'GET' }), status: () => 200, headers: () => ({ 'content-type': 'text/vtt', 'content-length': String(bytes.length) }),
      body: async () => bytes };
  };
  const choose = name => {
    selected = name; events.push('select:' + name);
    if (name === 'English (SRT)') listener(response(1));
    if (name === 'English (VTT)') listener(response(2));
  };
  const page = {
    url: () => location,
    mouse: { async move() {} },
    getByText(name, options) {
      assert.deepEqual(options, { exact: true });
      const movie = name === 'M3e Client Movie', option = ['English (SRT)', 'English (VTT)', 'Off'].includes(name);
      const locator = { filter() { return this; }, first() { return this; }, async waitFor() {},
        async count() { return movie || option ? 1 : 0; },
        async click() { if (movie) location = TARGET.href + '#!/item?id=' + MOVIE_ID; else choose(name); },
        locator() { return this; } };
      return locator;
    },
    getByRole(role, options) {
      assert.equal(role, 'button');
      const name = options.name;
      return { filter() { return this; }, first() { return this; }, async waitFor() {}, async count() { return 1; },
        async click() {
          events.push('button:' + name);
          if (name === 'From Beginning' || name === 'Play') { time = 1; paused = false; }
          if (name === 'Pause') paused = true;
          if (name === 'Back') { paused = true; report.requests.push({ method: 'POST', route: '/Sessions/Playing/Stopped', status: 204 }); }
        } };
    },
    locator(selector) {
      return { first() { return this; }, async count() { return 1; },
        async boundingBox() { return { x: 0, y: 0, width: 500, height: selector.includes('PositionSlider') ? 10 : 240 }; },
        async evaluateAll() { return []; },
        async click(options) { time = options.position.x / 500 * 600; seeks.push({ selected, time }); } };
    },
    async waitForURL(predicate) { assert.equal(predicate(new URL(location)), true); },
    async waitForFunction() {},
    async waitForTimeout() {},
    async evaluate(_callback, input) {
      assert.equal(input.origin, TARGET.origin); assert.equal(input.itemId, MOVIE_ID); assert(input.allowed.includes('Opening subtitle'));
      const showing = selected !== 'Off' || visibleAfterOff && events.includes('select:Off');
      const cue = time < 5 ? 'Opening subtitle' : time >= 118 && time < 128 ? 'Forward seek subtitle' : null;
      const available = showing && cue && !(missingVttOpening && selected === 'English (VTT)' && time < 5);
      return [{ visible: true, source_matches_item: true, current_time: time, duration: 600, paused, ready_state: 4, network_state: 1,
        video_width: 640, video_height: 360, total_video_frames: 100, dropped_video_frames: 0,
        text_tracks: [{ kind: 'subtitles', label: selected, language: 'en', mode: showing ? 'showing' : 'disabled',
          active_cues: available ? [{ start: time < 5 ? 0 : 118, end: time < 5 ? 5 : 128, text: cue }] : [] }] }];
    },
  };
  const context = { on(name, callback) { assert.equal(name, 'response'); listener = callback; },
    off(name, callback) { assert.equal(name, 'response'); assert.equal(callback, listener); detached = true; } };
  return { input: { page, context, report, target: TARGET, movieId: MOVIE_ID, async snapshot(label) { snapshots.push(label); } },
    report, snapshots, seeks, events, detached: () => detached };
}

test('subtitle flow observes VTT opening after its seek cue and before returning to Off', async () => {
  const fixture = completeSubtitleFixture();
  const result = await runSubtitleUI(fixture.input);
  assert.equal(result.outcome, 'external_srt_vtt_ui_selection_and_seek_completed');
  assert.deepEqual(result.selections.map(row => row.name), ['English (SRT)', 'English (VTT)', 'Off']);
  assert.deepEqual(fixture.seeks.map(row => row.selected), ['English (SRT)', 'English (VTT)']);
  assert(Math.abs(fixture.seeks[0].time - 123) < 0.000001); assert(Math.abs(fixture.seeks[1].time - 1.8) < 0.000001);
  for (const label of ['subtitle-srt-opening-cue', 'subtitle-srt-forward-cue', 'subtitle-vtt-forward-cue', 'subtitle-vtt-opening-cue']) assert(fixture.snapshots.includes(label));
  assert(result.steps.findIndex(row => row.label.startsWith('subtitle-vtt-opening-cue-')) < result.steps.findIndex(row => row.label === 'subtitle-off-verified'));
  assert.equal(result.network_subtitles.length, 2); assert(result.network_subtitles.every(row => row.body_result === 'owned_subtitle_hashed'));
  assert.equal(result.restored_selection, true); assert.equal(fixture.detached(), true);
});

test('subtitle flow cannot substitute the VTT seek cue for opening or accept visible cues after Off', async () => {
  for (const options of [{ missingVttOpening: true }, { visibleAfterOff: true }]) {
    const fixture = completeSubtitleFixture(options);
    await assert.rejects(runSubtitleUI(fixture.input), /subtitle_ui_/);
    assert.equal(fixture.report.subtitle_flow.outcome, 'blocked_at_observed_ui_step');
    assert.equal(fixture.report.subtitle_flow.phase, options.missingVttOpening ? 'seek_with_vtt' : 'restore_off');
    assert.equal(fixture.report.subtitle_flow.failure_reason, options.missingVttOpening ? 'expected_synthetic_subtitle_cue_not_observed' : 'subtitle_off_state_not_restored');
    assert.equal(fixture.detached(), true);
    assert.equal(fixture.events.includes('button:Back'), false);
  }
});
