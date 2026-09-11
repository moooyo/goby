/** Pure decision guards; no browser, network, database, or fixture access.
 *
 * The locator double models the observed card/row relationships, not an HTML
 * renderer or XPath engine. These tests do not establish real-client selector
 * compatibility, runtime event timing, state preservation, or logout proof.
 * The AV runtime/finalizer remains responsible for those independent gates.
 */
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import test from 'node:test';
import { auditOwnedItemIdentity, runAuxiliaryAlbumUI } from './client-browser-auxiliary-album.mjs';

const REMOTE_ONLY = { skip: process.platform !== 'linux', concurrency: false, timeout: 5000 };
const TARGET = new URL('http://127.0.0.1:18196/web/index.html');
const ALBUM = 'M3e Synthetic Album';
const ALBUM_ID = '002c2ea2c77769ae9b0b84b6e788998b';
const OTHER_ID = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
const LIBRARY = 'M3e Client Music';

test('distinguishes a synthetic album omitted Path from real-media paths without relaxing identity', REMOTE_ONLY, () => {
  const expected = { id: ALBUM_ID, name: ALBUM, type: 'MusicAlbum', parentId: OTHER_ID,
    scope: 'auxiliary-album-owner', pathOmitted: true };
  const actual = { Id: ALBUM_ID, Name: ALBUM, Type: 'MusicAlbum', ParentId: OTHER_ID };
  assert.ok(Object.values(auditOwnedItemIdentity(actual, expected)).every(Boolean));
  for (const Path of ['', null, '/opt/goby-fixtures/client-m3e/Music']) {
    assert.equal(auditOwnedItemIdentity({ ...actual, Path }, expected).path_matches, false);
  }
  assert.equal(auditOwnedItemIdentity({ ...actual, Id: OTHER_ID }, expected).id_matches, false);
  assert.equal(auditOwnedItemIdentity({ ...actual, ParentId: ALBUM_ID }, expected).parent_matches, false);
  assert.equal(auditOwnedItemIdentity({ ...actual, Type: 'Folder' }, expected).type_matches, false);
  assert.equal(auditOwnedItemIdentity(null, expected).shape_valid, false);
  const physical = { id: ALBUM_ID, name: ALBUM, type: 'Audio', path: '/owned/track.flac' };
  assert.ok(Object.values(auditOwnedItemIdentity({ Id: ALBUM_ID, Name: ALBUM, Type: 'Audio', Path: physical.path }, physical)).every(Boolean));
  assert.equal(auditOwnedItemIdentity({ Id: ALBUM_ID, Name: ALBUM, Type: 'Audio' }, physical).path_matches, false);
  assert.throws(() => auditOwnedItemIdentity(actual, { ...expected, type: 'Audio' }), /invalid_owned_item_path_contract/);
});

const TRACKS = [
  { id: 'e2da060a0addfafde7f92c291c413f6d', name: 'M3e MP3' },
  { id: '4d2ec259d9ae5224f46aa6f56ca2b186', name: 'M3e FLAC' },
];

// Tests are sequential. Advance only the flow's Date.now deadline; there are
// no real sleeps, sockets, mock HTTP servers, or global patches left afterward.
async function withClock(run) {
  const original = Date.now;
  let now = 1700000000000;
  const started = now;
  const clock = { elapsed: () => now - started, advance: amount => { now += amount; } };
  Date.now = () => now;
  try { return await run(clock); }
  finally { Date.now = original; }
}

const visible = node => typeof node.visible === 'function' ? node.visible() : node.visible !== false;

class Locator {
  constructor(page, resolve) { this.page = page; this.resolve = resolve; }
  filter(options) {
    return new Locator(this.page, () => this.resolve().filter(node =>
      (options.visible !== true || visible(node)) &&
      (options.hasText === undefined || node.text.includes(options.hasText))));
  }
  locator(expression) {
    return new Locator(this.page, () => this.resolve().flatMap(node => {
      if (node.kind === 'heading' && expression.includes('verticalSection')) return [this.page.section];
      if (node.kind === 'album-title' && expression.includes('@data-action="link"')) return [node.control];
      throw new Error('mock_relationship_not_declared');
    }));
  }
  getByText(value, options = {}) {
    return new Locator(this.page, () => this.resolve().flatMap(node => node.children ?? [])
      .filter(node => options.exact ? node.text === value : node.text.includes(value)));
  }
  first() { return new Locator(this.page, () => this.resolve().slice(0, 1)); }
  async count() { return this.resolve().length; }
  async innerText() {
    assert.equal(this.resolve().length, 1, 'The text action must select one declared node.');
    return this.resolve()[0].text;
  }
  async waitFor({ state }) {
    assert.equal(state, 'visible');
    if (!this.resolve().some(visible)) throw new Error('mock_visible_node_missing');
  }
  async evaluate(callback) {
    assert.equal(this.resolve().length, 1, 'The DOM read must select one declared node.');
    return callback(this.resolve()[0].element);
  }
  async click() {
    assert.equal(this.resolve().length, 1, 'The click must select one declared node.');
    await this.resolve()[0].click();
  }
}

function scenario(clock, options = {}) {
  const context = new EventEmitter();
  const observations = new WeakMap();
  const report = { requests: [], request_overflow: 0 };
  const snapshots = [], phases = [], clicks = [];
  let action = 'before-flow';
  const phase = () => `workflow:${action}`;
  const urls = { home: `${TARGET.origin}/web/index.html#!/home`,
    album: options.detailURL ?? `${TARGET.origin}/web/index.html#!/item?id=${ALBUM_ID}` };
  const page = { scene: 'home', location: urls.home, waits: 0, globalTextReads: 0,
    url: () => page.location,
    waitForTimeout: async amount => {
      page.waits += 1; clock.advance(amount);
      for (const entry of emitted) {
        if (entry.specification.finishAfterWaits === page.waits) context.emit('requestfinished', entry.request);
      }
    } };

  // This ledger supplies the same Request-object association as the runtime.
  // All transport events are emitted synchronously in memory by the scenario.
  context.on('request', request => {
    const url = new URL(request.url());
    const entry = { index: report.requests.length, elapsed_ms: clock.elapsed(), phase: phase(),
      method: request.method(), origin: url.origin === TARGET.origin ? 'target' : 'external',
      route: url.pathname.replace(/\/Items\/[^/]+\//, '/Items/{value}/'), status: null };
    report.requests.push(entry);
    if (!request.omitObservation) observations.set(request, entry);
  });
  context.on('response', ({ request, status }) => {
    const entry = observations.get(request);
    if (entry) Object.assign(entry, { status, response_elapsed_ms: clock.elapsed(), response_phase: phase() });
  });
  context.on('requestfinished', request => {
    const entry = observations.get(request);
    if (entry) Object.assign(entry, { finished_elapsed_ms: clock.elapsed(), finished_phase: phase() });
  });
  context.on('requestfailed', request => {
    const entry = observations.get(request);
    if (entry) Object.assign(entry, { failure: 'net::ERR_CONNECTION_CLOSED',
      failure_elapsed_ms: clock.elapsed(), failure_phase: phase() });
  });
  const emitted = [];
  function emit(specification) {
    const { kind, status = 200, itemId = ALBUM_ID, origin = TARGET.origin, method = 'GET',
      finished = true, failed = false, omitObservation = false } = specification;
    const raw = `${origin}/emby/Items/${itemId}/${kind}?api_key=guard-token-not-a-secret`;
    const request = { url: () => raw, method: () => method, omitObservation };
    context.emit('request', request);
    clock.advance(1);
    context.emit('response', { request, status });
    clock.advance(1);
    if (failed) context.emit('requestfailed', request);
    else if (finished) context.emit('requestfinished', request);
    emitted.push({ specification, request });
    return request;
  }
  const card = { getAttribute: name => ({ 'data-id': Object.hasOwn(options, 'cardId') ? options.cardId : ALBUM_ID,
    'data-type': Object.hasOwn(options, 'cardType') ? options.cardType : 'MusicAlbum' })[name] ?? null };
  const albumControl = { kind: 'album-control', text: ALBUM, visible: () => page.scene === 'home',
    element: { closest: selector => { assert.equal(selector, '.card'); return card; } },
    click: async () => {
      clicks.push('album-title');
      if (!options.stayHome) { page.scene = 'album'; page.location = urls.album; }
      for (const request of options.requests ?? [{ kind: 'Similar', status: 200 }, { kind: 'ThemeMedia', status: 404 }]) emit(request);
    } };
  const albumTitle = { kind: 'album-title', text: ALBUM, control: albumControl, visible: () => page.scene === 'home' };
  page.section = { kind: 'section', text: '', visible: () => page.scene === 'home',
    children: options.duplicateAlbum ? [albumTitle, { ...albumTitle }] : [albumTitle] };
  const heading = { kind: 'heading', text: `Latest ${LIBRARY}`, visible: () => page.scene === 'home' };
  const homeControl = { kind: 'home-control', text: 'Home', visible: true, click: async () => {
    clicks.push('home');
    if (options.failDuringReturn) {
      context.emit('requestfailed', emitted.find(entry => entry.specification.kind === 'Similar').request);
    }
    page.scene = 'home'; page.location = options.returnURL ?? urls.home;
  } };
  const rows = TRACKS.map((track, index) => ({ kind: 'track-row', id: options.wrongRowIndex === index ? OTHER_ID : track.id,
    text: track.name, visible: () => page.scene === 'album' || options.staleTrackRows === true,
    children: [{ kind: 'track-title', text: options.wrongTitleIndex === index ? 'Another track' : track.name,
      visible: () => page.scene === 'album' || options.staleTrackRows === true }] }));
  const history = TRACKS.map(track => ({ kind: 'history-title', text: track.name, visible: () => page.scene === 'home' }));
  const historyAlbum = { kind: 'history-album-title', text: ALBUM, visible: () => page.scene === 'home' };
  page.locator = selector => {
    if (selector === 'a.sectionTitleTextButton') return new Locator(page, () => [heading]);
    if (selector.startsWith('.listItem[data-id=')) {
      const ids = [...selector.matchAll(/data-id="([a-f0-9]{32})"/g)].map(match => match[1]);
      assert.equal(new Set(ids).size, 1, 'Both row selector alternatives must refer to one item.');
      return new Locator(page, () => rows.filter(row => row.id === ids[0]));
    }
    throw new Error('mock_page_selector_not_declared');
  };
  page.getByRole = (role, { name, exact }) => {
    assert.equal(name, 'Home'); assert.equal(exact, true);
    return new Locator(page, () => role === 'link' ? [homeControl] : []);
  };
  page.getByText = (value, { exact } = {}) => {
    page.globalTextReads += 1;
    return new Locator(page, () => [albumTitle, historyAlbum, ...history, ...rows.flatMap(row => row.children)]
      .filter(node => exact ? node.text === value : node.text.includes(value)));
  };
  const args = { page, context, report, target: TARGET, album: ALBUM, albumId: ALBUM_ID, libraryName: LIBRARY,
    tracks: TRACKS.map(track => ({ ...track })), requestObservation: request => observations.get(request),
    setActionPhase: value => { action = value; phases.push({ phase: phase(), elapsed_ms: clock.elapsed() }); },
    snapshot: async name => { snapshots.push({ name, phase: phase(), elapsed_ms: clock.elapsed() }); } };
  return { args, page, context, report, snapshots, phases, clicks, emitted, observations,
    run: () => runAuxiliaryAlbumUI(args),
    assertDetached: () => assert.equal(context.listenerCount('request'), 1, 'The flow must remove its request listener.') };
}

test('binds the owned album, ignores history titles, and retains a completed ThemeMedia 404', REMOTE_ONLY, async () => {
  await withClock(async clock => {
    const run = scenario(clock);
    await run.run();
    const result = run.report.auxiliary_album;
    assert.equal(result.outcome, 'completed');
    assert.deepEqual(run.clicks, ['album-title', 'home']);
    assert.equal(run.page.globalTextReads, 0, 'History text cannot establish the album track rows.');
    assert.deepEqual(result.detail, { navigation_observed: true, album_id_matches_receipt: true, visible_receipted_track_rows: 2 });
    assert.deepEqual(result.responses.map(entry => entry.status), [200, 404]);
    assert.ok(result.responses.every(entry => entry.seed_matches_receipted_album && entry.failure === null &&
      entry.request_elapsed_ms <= entry.response_elapsed_ms && entry.response_elapsed_ms <= entry.finished_elapsed_ms &&
      entry.request_phase === 'workflow:auxiliary_album_click' && entry.response_phase === 'workflow:auxiliary_album_click'));
    assert.deepEqual(run.snapshots.map(entry => entry.name), ['auxiliary-home-before', 'auxiliary-album-visible', 'auxiliary-home-after']);
    assert.equal(result.return_home.hash_route, '#!/home');
    assert.ok(!JSON.stringify(result).includes('guard-token-not-a-secret'));
    run.assertDetached();
  });
});

test('rejects an unchanged Home URL despite stale matching track rows and successful responses', REMOTE_ONLY, async () => {
  await withClock(async clock => {
    const run = scenario(clock, { stayHome: true, staleTrackRows: true });
    await assert.rejects(run.run(), { message: 'auxiliary_album_navigation_unconfirmed' });
    assert.deepEqual(run.clicks, ['album-title']);
    assert.notEqual(run.report.auxiliary_album.outcome, 'completed');
    run.assertDetached();
  });
});

test('rejects a foreign or duplicated detail ID and missing receipted track identity', REMOTE_ONLY, async () => {
  await withClock(async clock => {
    for (const options of [
      { detailURL: `${TARGET.origin}/web/index.html#!/item?id=${OTHER_ID}` },
      { detailURL: `${TARGET.origin}/web/index.html#!/item?id=${ALBUM_ID}&Id=${ALBUM_ID}` },
      { wrongRowIndex: 0 }, { wrongTitleIndex: 1 },
    ]) {
      const run = scenario(clock, options);
      await assert.rejects(run.run());
      assert.notEqual(run.report.auxiliary_album.outcome, 'completed');
      assert.ok(!run.clicks.includes('home'));
      run.assertDetached();
    }
  });
});

test('does not use another seed, origin, or HTTP method to replace the owned Similar failure', REMOTE_ONLY, async () => {
  await withClock(async clock => {
    for (const foreign of [{ itemId: OTHER_ID }, { origin: 'https://outside.invalid' }, { method: 'POST' }]) {
      const run = scenario(clock, { requests: [
        { kind: 'Similar', status: 404 }, { kind: 'Similar', status: 200, ...foreign },
        { kind: 'ThemeMedia', status: 404 },
      ] });
      await assert.rejects(run.run(), { message: 'auxiliary_expected_responses_missing' });
      assert.deepEqual(run.report.auxiliary_album.responses.map(entry => entry.request_index), [0, 2]);
      assert.deepEqual(run.report.auxiliary_album.responses.map(entry => entry.status), [404, 404]);
      assert.equal(run.report.requests.length, 3, 'The unrelated observation remains in the runtime ledger.');
      run.assertDetached();
    }
  });
});

test('rejects ambiguous or foreign album cards before any navigation request', REMOTE_ONLY, async () => {
  await withClock(async clock => {
    for (const options of [{ cardId: OTHER_ID }, { cardType: 'Audio' }, { duplicateAlbum: true }]) {
      const run = scenario(clock, options);
      await assert.rejects(run.run());
      assert.deepEqual(run.clicks, []);
      assert.equal(run.report.requests.length, 0);
      run.assertDetached();
    }
  });
});

test('keeps missing card attributes unknown and requires subsequent exact owner evidence', REMOTE_ONLY, async () => {
  await withClock(async clock => {
    const run = scenario(clock, { cardId: null, cardType: null });
    await run.run();
    assert.deepEqual(run.report.auxiliary_album.card, { card_present: true, id_present: false, id_matches_receipt: null, type: null });
    assert.equal(run.report.auxiliary_album.detail.album_id_matches_receipt, true);
    assert.equal(run.report.auxiliary_album.detail.visible_receipted_track_rows, 2);
    assert.ok(run.report.auxiliary_album.responses.every(entry => entry.seed_matches_receipted_album));
    run.assertDetached();
    const foreign = scenario(clock, { cardId: null, cardType: null,
      detailURL: `${TARGET.origin}/web/index.html#!/item?id=${OTHER_ID}` });
    await assert.rejects(foreign.run(), { message: 'auxiliary_album_navigation_unconfirmed' });
    foreign.assertDetached();
  });
});

test('rejects a matching request that lacks its runtime Request-object observation', REMOTE_ONLY, async () => {
  await withClock(async clock => {
    const run = scenario(clock, { requests: [
      { kind: 'Similar', status: 200 }, { kind: 'ThemeMedia', status: 404 },
      { kind: 'Similar', status: 200, omitObservation: true },
    ] });
    await assert.rejects(run.run(), { message: 'auxiliary_expected_responses_missing' });
    assert.equal(run.report.auxiliary_album.request_binding_error, true);
    run.assertDetached();
  });
});

test('does not accept 200 headers when the owned transfer fails or never finishes', REMOTE_ONLY, async () => {
  await withClock(async clock => {
    for (const transfer of [{ finished: false, failed: true }, { finished: false }]) {
      const run = scenario(clock, { requests: [
        { kind: 'Similar', status: 200, ...transfer }, { kind: 'ThemeMedia', status: 404 },
      ] });
      await assert.rejects(run.run(), { message: 'auxiliary_expected_responses_missing' });
      const response = run.report.auxiliary_album.responses[0];
      assert.equal(response.status, 200);
      assert.equal(response.finished_elapsed_ms, undefined);
      assert.equal(response.failure, transfer.failed ? 'net::ERR_CONNECTION_CLOSED' : null);
      const minimumWaits = transfer.failed ? 0 : 1;
      assert.ok(run.page.waits >= minimumWaits && run.page.waits <= 100,
        'A known failure may terminate immediately; an unfinished transfer must consume only its bounded wait.');
      assert.ok(!run.clicks.includes('home'));
      run.assertDetached();
    }
  });
});

test('waits for a third normally completing duplicate transfer before accepting the album', REMOTE_ONLY, async () => {
  await withClock(async clock => {
    const run = scenario(clock, { requests: [
      { kind: 'Similar', status: 200 }, { kind: 'ThemeMedia', status: 404 },
      { kind: 'Similar', status: 200, finished: false, finishAfterWaits: 2 },
    ] });
    await run.run();
    const result = run.report.auxiliary_album;
    assert.equal(result.outcome, 'completed');
    assert.equal(run.page.waits, 2, 'The first finished request of each kind cannot bypass a pending duplicate.');
    assert.deepEqual(result.responses.map(entry => entry.request_index), [0, 1, 2]);
    assert.deepEqual(result.responses.map(entry => entry.status), [200, 404, 200]);
    assert.ok(result.responses.every(entry => entry.seed_matches_receipted_album &&
      Number.isInteger(entry.finished_elapsed_ms) && entry.failure === null));
    const duplicate = run.report.requests[2];
    assert.equal(duplicate.finished_phase, 'workflow:auxiliary_album_wait');
    assert.ok(run.snapshots.find(entry => entry.name === 'auxiliary-album-visible').elapsed_ms >= duplicate.finished_elapsed_ms);
    assert.deepEqual(run.clicks, ['album-title', 'home']);
    run.assertDetached();
  });
});

test('retains a failure arriving during Home navigation for the separate finalizer', REMOTE_ONLY, async () => {
  await withClock(async clock => {
    const run = scenario(clock, { failDuringReturn: true });
    await run.run();
    // UI navigation can finish before its surrounding AV run is classified.
    // The real finalizer must reject this ledger entry; that code is not
    // imported here because the CLI performs fixture reads at module load.
    assert.equal(run.report.auxiliary_album.outcome, 'completed');
    const response = run.report.auxiliary_album.responses.find(entry => entry.route.endsWith('/Similar'));
    assert.equal(response.failure, 'net::ERR_CONNECTION_CLOSED');
    assert.equal(run.report.requests[response.request_index].failure, response.failure);
    assert.equal(run.report.requests[response.request_index].failure_phase, 'workflow:auxiliary_return_home');
    run.assertDetached();
  });
});
