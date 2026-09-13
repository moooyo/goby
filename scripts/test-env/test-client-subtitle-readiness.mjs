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
