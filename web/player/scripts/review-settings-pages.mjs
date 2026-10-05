import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, readFile, realpath, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { chromium } from '@playwright/test';
import { createCatalogue, createUser, TOKEN } from '../tests/fixture-data.mjs';

// Visual fixtures replay the original handoff's state and artwork through API
// responses. Neither the reference nor the production DOM is rewritten.
assert.equal(process.platform, 'linux', 'Run visual verification on test-env.');
const root = await realpath(process.env.GOBY_LIVE_ROOT ?? '/opt/goby-test/player-live-20261004-165c');
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), 'goby-player-live-20261004-165c');
const phase = process.env.GOBY_SETTINGS_PHASE ?? 'before';
assert(['before', 'after'].includes(phase));
const accountOnly = process.env.GOBY_SETTINGS_SCOPE === 'account';
const output = resolve(root, 'artifacts/other-pages-review-20261005/settings', accountOnly ? 'account-final' : phase);
await mkdir(output, { recursive: true });
const origin = 'http://127.0.0.1:4224';
const apiOrigin = 'http://goby.local';
const widths = accountOnly ? [[1440, 900], [393, 852]] : [[1440, 900], [393, 852], [375, 852], [1040, 900]];
const ids = ['guitu', 'deep-2-5', 'rain-1-3', 'drift', 'lighthouse', 'typist'];
const keys = ['guitu', 'deep:2:5', 'rain:1:3', 'drift', 'lighthouse', 'typist'];
const epoch = Date.parse('2026-10-04T00:00:00Z');
const user = { ...createUser(), Name: '\u9648\u9ed8' };
user.Configuration.AudioLanguagePreference = '';
const state = {
  favs: {}, sp: { deep: { s: 2, e: 5 }, rain: { s: 1, e: 3 } },
  prog: Object.fromEntries(keys.map((key, index) => [key, { f: .35, at: epoch - index * 3600000 }])),
  watched: { lighthouse: 1, typist: 1, 'deep:1:1': 1, 'deep:1:2': 1 },
  recent: [], sel: {}, set: { motion: false, heroRotate: true },
};
const server = spawn(process.execPath, [resolve(import.meta.dirname, '../tests/fixture-server.mjs')], {
  env: { ...process.env, GOBY_FIXTURE_PORT: '4224', GOBY_HANDOFF_DIR: resolve(root, 'handoff-v3') }, stdio: ['ignore', 'pipe', 'pipe'],
});
let serverLog = '';
server.stdout.on('data', chunk => { serverLog += chunk; });
server.stderr.on('data', chunk => { serverLog += chunk; });
const browser = await chromium.launch({ headless: true });
const records = [];
const failures = [];
const interactions = [];

async function settle(page) {
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(900);
  await page.mouse.move(0, 0);
}

async function readPage(page, reference) {
  return page.evaluate(reference => {
    const main = document.querySelector(reference ? 'main[data-screen-label]' : '.settings-page');
    const content = main.firstElementChild;
    const headings = [...content.querySelectorAll('h2')];
    const profile = reference ? content.firstElementChild : content.querySelector('.settings-profile');
    const recent = reference ? headings[0].nextElementSibling : content.querySelector('.settings-recent-grid');
    const playback = reference ? headings[1].nextElementSibling : content.querySelectorAll('.settings-card')[0];
    const subtitles = reference ? headings[2].nextElementSibling : content.querySelector('.settings-card--subtitles');
    const preview = subtitles.firstElementChild;
    const box = element => { const b = element.getBoundingClientRect(); return { x: b.x, y: b.y + scrollY, width: b.width, height: b.height, right: b.right, bottom: b.bottom + scrollY }; };
    const style = element => { const s = getComputedStyle(element); return { fontSize: s.fontSize, fontWeight: s.fontWeight, lineHeight: s.lineHeight, gap: s.gap, padding: s.padding, flexBasis: s.flexBasis, background: s.backgroundColor, textShadow: s.textShadow }; };
    const stats = reference ? [...profile.lastElementChild.children].map(e => ({ value: e.firstElementChild.textContent, ...box(e), ...style(e.firstElementChild) })) : [...profile.querySelectorAll('.settings-statistics > div')].map(e => ({ value: e.querySelector('strong').textContent, ...box(e), ...style(e.querySelector('strong')) }));
    const rows = reference ? [...playback.children].filter(e => getComputedStyle(e).display === 'flex') : [...playback.querySelectorAll('.settings-row')];
    const line = reference ? preview.lastElementChild : preview.querySelector('.settings-preview-line');
    const posterImages = [...recent.querySelectorAll('img')];
    return {
      viewport: [innerWidth, innerHeight], documentWidth: document.documentElement.scrollWidth,
      page: box(main), profile: box(profile), identity: box(profile.firstElementChild), stats,
      headings: headings.map(e => ({ text: e.textContent, ...box(e), ...style(e) })),
      recent: { ...box(recent), cards: posterImages.map(e => ({ src: e.src, loaded: e.complete && e.naturalWidth > 0, ...box(e), title: e.parentElement.parentElement.textContent.trim() })) },
      playback: { ...box(playback), rows: rows.map(e => ({ ...box(e), ...style(e), label: e.firstElementChild.textContent.trim(), labelBox: box(e.firstElementChild), controls: [...e.querySelectorAll('button')].map(b => ({ text: b.textContent.trim(), ...box(b), ...style(b), active: b.getAttribute('aria-pressed') ?? getComputedStyle(b).backgroundColor })) })) },
      preview: { ...box(preview), src: preview.querySelector('img')?.src, line: { ...box(line), ...style(line), text: line.textContent.trim(), spanStyle: style(line.firstElementChild) } },
      account: { ...box(reference ? headings.at(-1).nextElementSibling : content.querySelector('.settings-account-actions')) },
    };
  }, reference);
}

async function screenshotSection(page, reference, index, name) {
  const heading = page.locator(reference ? 'main[data-screen-label] h2' : '.settings-page h2').nth(index);
  await heading.evaluate(element => window.scrollTo(0, element.getBoundingClientRect().top + scrollY - 94));
  await settle(page);
  await page.screenshot({ path: resolve(output, `${name}.png`) });
}

async function capture(page, reference, name, extra) {
  await settle(page);
  await page.evaluate(() => window.scrollTo(0, 0));
  await settle(page);
  await page.screenshot({ path: resolve(output, `${name}-top.png`) });
  await screenshotSection(page, reference, 1, `${name}-playback`);
  await screenshotSection(page, reference, 2, `${name}-subtitles`);
  if (extra) {
    await screenshotSection(page, reference, 3, `${name}-interface`);
    await screenshotSection(page, reference, 4, `${name}-account`);
    await page.screenshot({ path: resolve(output, `${name}-full.png`), fullPage: true });
  }
  const metrics = await readPage(page, reference);
  records.push({ name, reference, ...metrics });
  return metrics;
}

async function captureAccount(page, reference, name) {
  await settle(page);
  await screenshotSection(page, reference, 4, `${name}-account`);
  const controls = reference ? page.locator('main[data-screen-label] h2').nth(4).locator('xpath=following-sibling::*[1]') : page.locator('.settings-account-actions');
  await controls.screenshot({ path: resolve(output, `${name}-account-controls.png`) });
  const metrics = await readPage(page, reference);
  const buttons = await controls.locator('button,a').evaluateAll(elements => elements.map(element => {
    const svg = element.querySelector('svg');
    const box = svg.getBoundingClientRect();
    const style = getComputedStyle(svg);
    return {
      text: element.textContent.trim(), width: box.width, height: box.height,
      viewBox: svg.getAttribute('viewBox'), strokeWidth: style.strokeWidth, linecap: style.strokeLinecap, linejoin: style.strokeLinejoin,
      shapes: [...svg.children].map(shape => ({ tag: shape.tagName, attributes: Object.fromEntries([...shape.attributes].map(attribute => [attribute.name, attribute.value])) })),
    };
  }));
  records.push({ name, reference, ...metrics, accountButtons: buttons });
  return metrics;
}

function accountGeometry(buttons) {
  return buttons.map(({ shapes, ...button }) => {
    const cleanShapes = shapes.map(shape => ({ ...shape, attributes: Object.fromEntries(Object.entries(shape.attributes).filter(([name]) => !name.startsWith('data-'))) }));
    const paths = cleanShapes.filter(shape => shape.tag === 'path');
    for (const path of paths) assert.deepEqual(Object.keys(path.attributes), ['d']);
    // A new move command preserves each subpath when equivalent paths are joined.
    return { ...button, shapes: cleanShapes.filter(shape => shape.tag !== 'path'), pathData: paths.map(shape => shape.attributes.d).join('') };
  });
}

async function configureFixture(reference) {
  const catalogue = createCatalogue();
  for (const item of catalogue.items) {
    Object.assign(item.UserData, { Played: ['lighthouse', 'typist', 'deep-1-1', 'deep-1-2'].includes(item.Id), PlaybackPositionTicks: 0, LastPlayedDate: undefined, PlayedPercentage: 0 });
  }
  ids.forEach((id, index) => {
    const item = catalogue.items.find(item => item.Id === id);
    Object.assign(item.UserData, { PlaybackPositionTicks: item.UserData.Played ? 0 : item.RunTimeTicks * .35, PlayedPercentage: item.UserData.Played ? 100 : 35, LastPlayedDate: new Date(epoch - index * 3600000).toISOString() });
  });
  const response = await fetch(`${origin}/__fixture/config`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...catalogue, user, display: {} }) });
  assert(response.ok);
  return catalogue;
}

async function installAPI(context, reference, catalogue) {
  await context.route(`${apiOrigin}/**`, async route => {
    const url = new URL(route.request().url());
    let target = `${origin}${url.pathname}${url.search}`;
    if (url.pathname.endsWith('/ViewingStatistics')) return route.fulfill({ json: { EstimatedContentHours: Number(reference.stats[2].value), EstimatedContentTicks: String(BigInt(reference.stats[2].value) * 36_000_000_000n), IsEstimate: true } });
    const match = url.pathname.match(/\/Items\/([^/]+)\/Images\/(Primary|Backdrop)/);
    if (match) {
      if (match[2] === 'Primary') {
        const work = ids.findIndex(id => id === match[1] || catalogue.items.find(item => item.Id === id)?.SeriesId === match[1]);
        if (work >= 0) target = reference.recent.cards[work].src;
      } else if (match[1] === ids[0] && Number(url.searchParams.get('MaxWidth')) === 640) target = reference.preview.src;
    }
    const response = await context.request.fetch(target, { method: route.request().method(), headers: route.request().headers(), data: route.request().postData() ?? undefined });
    await route.fulfill({ response });
  });
}

async function exercisePreview(page, reference, name) {
  const main = page.locator(reference ? 'main[data-screen-label]' : '.settings-page');
  const card = reference ? main.locator('h2').nth(2).locator('xpath=following-sibling::*[1]') : main.locator('.settings-card--subtitles');
  await card.getByRole('button', { name: '\u7279\u5927', exact: true }).click();
  await card.getByRole('button', { name: '\u5e95\u6846', exact: true }).click();
  await screenshotSection(page, reference, 2, `${name}-subtitles-large-background`);
  records.push({ name: `${name}-subtitles-large-background`, reference, ...(await readPage(page, reference)) });
  await card.getByRole('button', { name: '\u5c0f', exact: true }).click();
  await card.getByRole('button', { name: '\u63cf\u8fb9', exact: true }).click();
  await screenshotSection(page, reference, 2, `${name}-subtitles-small-outline`);
  records.push({ name: `${name}-subtitles-small-outline`, reference, ...(await readPage(page, reference)) });
}

async function exercisePlayback(page, reference, name) {
  const main = page.locator(reference ? 'main[data-screen-label]' : '.settings-page');
  const card = reference ? main.locator('h2').nth(1).locator('xpath=following-sibling::*[1]') : main.locator('.settings-card').first();
  for (const label of ['1080p', '\u56fd\u8bed', '\u82f1\u6587', 'PotPlayer']) await card.getByRole('button', { name: reference && label === 'PotPlayer' ? /PotPlayer$/ : label, exact: true }).click();
  await card.getByRole('switch').nth(0).click();
  await card.getByRole('switch').nth(1).click();
  await screenshotSection(page, reference, 1, `${name}-playback-selected`);
  if (!reference) {
    await page.waitForTimeout(300);
    const persisted = await (await fetch(`${origin}/__fixture/state`)).json();
    assert.equal(persisted.user.Configuration.AudioLanguagePreference, 'zho');
    assert.equal(persisted.user.Configuration.SubtitleLanguagePreference, 'eng');
    assert.equal(persisted.user.Configuration.EnableNextEpisodeAutoPlay, false);
    assert.equal(persisted.user.Configuration.IntroSkipMode, 'AutoSkip');
    assert.match(await card.getByRole('button', { name: 'VLC', exact: true }).getAttribute('title'), /\u590d\u5236/);
    await page.reload();
    await page.locator('.settings-recent-card').nth(5).waitFor();
    await settle(page);
    for (const label of ['1080p', '\u56fd\u8bed', '\u82f1\u6587', 'PotPlayer']) assert.equal(await card.getByRole('button', { name: label, exact: true }).getAttribute('aria-pressed'), 'true');
    assert.equal(await card.getByRole('switch').nth(0).getAttribute('aria-checked'), 'false');
    assert.equal(await card.getByRole('switch').nth(1).getAttribute('aria-checked'), 'true');
    assert.equal(await page.locator('.settings-account-actions a').getAttribute('href'), `${apiOrigin}/admin/`);
    interactions.push({ name, result: 'Playback defaults, supported external-player selection, manual VLC hint, and preference persistence match actual controls.' });
  }
}

try {
  for (let attempt = 0; attempt < 50; attempt++) {
    if (server.exitCode !== null) throw new Error(serverLog);
    try { if ((await fetch(`${origin}/__fixture/health`)).ok) break; } catch { /* Wait for the owned fixture. */ }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  for (const [width, height] of widths) {
    const options = { viewport: { width, height }, locale: 'zh-CN', reducedMotion: 'reduce', timezoneId: 'Asia/Shanghai', userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36' };
    const referenceContext = await browser.newContext(options);
    await referenceContext.addInitScript(state => {
      localStorage.setItem('goby.proto.v1', JSON.stringify(state));
      localStorage.setItem('goby.route.A', JSON.stringify({ name: 'me' }));
    }, state);
    const original = await referenceContext.newPage();
    original.on('pageerror', error => failures.push({ reference: true, message: error.message }));
    await original.goto(`${origin}/reference/`);
    await original.locator('main[data-screen-label] h1').waitFor();
    const reference = accountOnly ? await captureAccount(original, true, `${width}x${height}-reference`) : await capture(original, true, `${width}x${height}-reference`, width === 1440 || width === 393);
    assert.equal(reference.recent.cards.length, 6);
    if (!accountOnly && (width === 1440 || width === 393)) {
      await exercisePreview(original, true, `${width}x${height}-reference`);
      if (phase === 'after') await exercisePlayback(original, true, `${width}x${height}-reference`);
    }
    await referenceContext.close();

    const catalogue = await configureFixture(reference);
    const implementationContext = await browser.newContext(options);
    await installAPI(implementationContext, reference, catalogue);
    await implementationContext.addInitScript(({ user, token, apiOrigin }) => {
      localStorage.setItem('goby.player.session.v1', JSON.stringify({ AccessToken: token, User: user, ServerId: 'fixture-server', serverUrl: apiOrigin }));
      localStorage.setItem('goby.player.preferences.v1.fixture-server.fixture-viewer.global', JSON.stringify({ backgroundMotion: false }));
    }, { user, token: TOKEN, apiOrigin });
    const current = await implementationContext.newPage();
    current.on('pageerror', error => failures.push({ reference: false, message: error.message }));
    await current.goto(`${origin}/#/settings`);
    await current.locator('.settings-recent-card').nth(5).waitFor();
    const implementation = accountOnly ? await captureAccount(current, false, `${width}x${height}-implementation`) : await capture(current, false, `${width}x${height}-implementation`, width === 1440 || width === 393);
    assert.equal(implementation.documentWidth, width);
    assert.deepEqual(implementation.stats.map(stat => stat.value), reference.stats.map(stat => stat.value));
    assert.deepEqual(implementation.recent.cards.map(card => card.title), reference.recent.cards.map(card => card.title));
    if (accountOnly) assert.deepEqual(accountGeometry(records.at(-1).accountButtons), accountGeometry(records.at(-2).accountButtons));
    if (!accountOnly && (width === 1440 || width === 393)) {
      await exercisePreview(current, false, `${width}x${height}-implementation`);
      if (phase === 'after') await exercisePlayback(current, false, `${width}x${height}-implementation`);
    }
    await implementationContext.close();
    if (!accountOnly && phase === 'after' && width === 393) {
      await configureFixture(reference);
      const limitedUser = { ...user, Policy: { ...user.Policy, IsAdministrator: false } };
      await fetch(`${origin}/__fixture/config`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ user: limitedUser }) });
      const fallbackContext = await browser.newContext(options);
      await installAPI(fallbackContext, reference, catalogue);
      await fallbackContext.addInitScript(({ user, token, apiOrigin }) => {
        localStorage.setItem('goby.player.session.v1', JSON.stringify({ AccessToken: token, User: user, ServerId: 'fixture-server', serverUrl: apiOrigin }));
        localStorage.setItem('goby.player.preferences.v1.fixture-server.fixture-viewer.global', JSON.stringify({ player: 'iina', backgroundMotion: false }));
      }, { user: limitedUser, token: TOKEN, apiOrigin });
      const fallback = await fallbackContext.newPage();
      await fallback.goto(`${origin}/#/settings`);
      await fallback.locator('.settings-recent-card').nth(5).waitFor();
      await settle(fallback);
      assert.equal(await fallback.locator('.settings-players button[aria-pressed="true"]').count(), 1);
      assert.equal(await fallback.getByRole('button', { name: '\u5185\u7f6e\u64ad\u653e\u5668', exact: true }).getAttribute('aria-pressed'), 'true');
      assert.equal(await fallback.locator('.settings-account-actions a').count(), 0);
      const priority = fallback.locator('.settings-background-order');
      assert(await priority.locator('li').nth(0).getByRole('button').nth(0).isDisabled());
      assert(await priority.locator('li').nth(2).getByRole('button').nth(1).isDisabled());
      await priority.locator('li').nth(0).getByRole('button').nth(1).click();
      assert.equal(await priority.locator('li').nth(0).getAttribute('data-source'), 'generated');
      assert.match(await fallback.locator('.settings-priority-status').innerText(), /2/);
      await screenshotSection(fallback, false, 3, '393x852-implementation-background-order');
      await screenshotSection(fallback, false, 4, '393x852-implementation-account-non-admin');
      await fallback.locator('.settings-account-actions button').last().click();
      await fallback.locator('input[type="password"]').waitFor();
      interactions.push({ name: '393x852-platform-and-account', result: 'An unavailable saved IINA preference selects the effective built-in fallback on Windows. Non-admin accounts do not expose the admin link. Background source reordering, disabled boundary buttons, live status, account scrolling, and logout work through the real controls.' });
      await fallbackContext.close();
    }
  }
} finally {
  await writeFile(resolve(output, 'metrics.json'), JSON.stringify(records, null, 2));
  await writeFile(resolve(output, 'provenance.json'), JSON.stringify({ phase, scope: accountOnly ? 'account' : 'full', origin, apiOrigin, state, ids, explanation: 'The original handoff is opened unchanged with its persisted state. The React application receives the same profile, recent work order, poster and preview artwork, and estimated content hours through isolated API fixtures. Additional interface preferences and real account actions are preserved. Windows user agent is used consistently at each viewport to keep external-player choices comparable.', interactions, failures }, null, 2));
  await writeFile(resolve(output, 'fixture.log'), serverLog);
  await browser.close();
  server.kill('SIGTERM');
}
assert.deepEqual(failures, []);
console.log(JSON.stringify({ phase, output, records: records.length }));
