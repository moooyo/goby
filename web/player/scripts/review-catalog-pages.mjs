import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, readFile, realpath, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { chromium } from '@playwright/test';
import { createCatalogue, createUser, TOKEN } from '../tests/fixture-data.mjs';

// This isolated harness compares the unmodified v3 reference with production
// React, using the same catalogue, artwork, and supported persisted user state.
assert.equal(process.platform, 'linux', 'Run visual verification on test-env.');
const root = await realpath(process.env.GOBY_LIVE_ROOT ?? '/opt/goby-test/player-live-20261004-165c');
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), 'goby-player-live-20261004-165c');
const phase = process.env.GOBY_CATALOG_PHASE ?? 'before';
assert(['before', 'after'].includes(phase));
const output = resolve(root, 'artifacts/other-pages-review-20261005/catalog', phase);
await mkdir(output, { recursive: true });
const origin = 'http://127.0.0.1:4222';
const user = { ...createUser(), Name: '\u9648\u9ed8' };
const catalogue = createCatalogue();
const records = [];
const failures = [];
const interactionChecks = [];
const attemptedNames = new Set();
const previous = await readFile(resolve(output, 'results.json'), 'utf8').then(JSON.parse).catch(() => ({ records: [], failures: [] }));
const selectedCases = process.env.GOBY_CATALOG_CASES?.split(',');
const selectedWidths = (process.env.GOBY_CATALOG_WIDTHS ?? '1440,393').split(',').map(Number);
const favorites = ['deep', 'lighthouse', 'startrail', 'plateau', 'typist', 'bamboo'];
const recent = ['\u6df1\u6d77\u56de\u54cd', '\u60ac\u7591', '2024', '\u5468\u5c7f'];
const scenarios = [
  { name: 'movies', kind: 'movies' },
  { name: 'movies-library', kind: 'movies', library: '\u534e\u8bed\u7535\u5f71' },
  { name: 'movies-genre', kind: 'movies', genre: '\u5267\u60c5' },
  { name: 'movies-sort', kind: 'movies', menu: 'sort' },
  { name: 'movies-filter', kind: 'movies', menu: 'filter' },
  { name: 'movies-quality', kind: 'movies', quality: '4K' },
  { name: 'movies-empty', kind: 'movies', library: '\u534e\u8bed\u7535\u5f71', genre: '\u4f20\u8bb0', empty: true },
  { name: 'series', kind: 'series' },
  { name: 'favorites', kind: 'favorites' },
  { name: 'favorites-movies', kind: 'favorites', segment: '\u7535\u5f71' },
  { name: 'favorites-series', kind: 'favorites', segment: '\u5267\u96c6' },
  { name: 'favorites-empty', kind: 'favorites', empty: true },
  { name: 'search-idle', kind: 'search' },
  { name: 'search-result', kind: 'search', query: '\u706f\u5854' },
  { name: 'search-empty', kind: 'search', query: 'unmatched-catalog-review' },
  { name: 'search-year', kind: 'search', year: '2024' },
  { name: 'search-quality', kind: 'search', quality: '4K' },
].filter(scenario => !selectedCases || selectedCases.includes(scenario.name));

const server = spawn(process.execPath, [resolve(import.meta.dirname, '../tests/fixture-server.mjs')], {
  env: { ...process.env, GOBY_FIXTURE_PORT: '4222', GOBY_HANDOFF_DIR: resolve(root, 'handoff-v3') }, stdio: ['ignore', 'pipe', 'pipe'],
});
let serverLog = '';
server.stdout.on('data', chunk => { serverLog += chunk; });
server.stderr.on('data', chunk => { serverLog += chunk; });
const browser = await chromium.launch({ headless: true });

async function configure(scenario) {
  const data = structuredClone(catalogue);
  for (const item of data.items) {
    if (item.UserData) Object.assign(item.UserData, { Played: false, PlaybackPositionTicks: 0, PlayedPercentage: 0, IsFavorite: scenario.name === 'favorites-empty' ? false : favorites.includes(item.Id) });
    if (['lighthouse', 'typist'].includes(item.Id)) Object.assign(item.UserData, { Played: true, PlayedPercentage: 100 });
    if (item.Id === 'guitu') Object.assign(item.UserData, { PlaybackPositionTicks: item.RunTimeTicks * .38, PlayedPercentage: 38 });
    if (item.Id === 'drift') Object.assign(item.UserData, { PlaybackPositionTicks: item.RunTimeTicks * .15, PlayedPercentage: 15 });
  }
  const response = await fetch(`${origin}/__fixture/config`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...data, user, empty: false, failures: {} }) });
  assert(response.ok);
}

async function settle(page) {
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(1500);
  await page.mouse.move(0, 0);
}

async function interact(page, scenario, reference) {
  const main = page.locator(reference ? 'main[data-screen-label]' : '.catalog-page');
  await main.waitFor();
  await settle(page);
  if (scenario.library) await main.getByRole('button', { name: scenario.library, exact: true }).click();
  if (scenario.genre) await main.getByRole('button', { name: scenario.genre, exact: true }).first().click();
  if (scenario.segment) await main.getByRole('button', { name: new RegExp(`^${scenario.segment} `) }).click();
  if (scenario.query || scenario.year || scenario.kind === 'search' && scenario.quality) {
    if (scenario.year && !reference) {
      await page.getByRole('button', { name: '\u5e74\u4efd\u7b5b\u9009', exact: true }).click();
      await page.getByLabel('\u4e0a\u6620\u5e74\u4efd').fill(scenario.year);
      await page.getByRole('button', { name: '\u5e94\u7528', exact: true }).click();
    } else if (scenario.quality && !reference) {
      await page.getByRole('button', { name: '\u753b\u8d28\u7b5b\u9009', exact: true }).click();
      await page.getByRole('button', { name: scenario.quality, exact: true }).click();
      await page.mouse.click(2, 150);
    } else {
      await main.locator('input').first().fill(scenario.query || scenario.year || scenario.quality);
      await main.locator('input').first().press('Enter');
    }
  }
  if (scenario.menu || scenario.name === 'movies-empty' || scenario.kind === 'movies' && scenario.quality) {
    const menu = scenario.menu || 'filter';
    if (reference && menu === 'sort') await main.locator('button[title="\u6392\u5e8f"]').click();
    else await main.getByRole('button', { name: menu === 'sort' ? '\u6392\u5e8f' : '\u7b5b\u9009', exact: true }).click();
    if (scenario.quality) {
      await main.getByRole('button', { name: scenario.quality, exact: true }).click();
      await page.mouse.click(2, 150);
    }
    if (scenario.name === 'movies-empty') {
      // The biography title in the Chinese movie library is already watched
      // in both clients, so the same unwatched filter produces an empty grid.
      await main.getByRole('switch').click();
      await page.mouse.click(2, 150);
    }
  }
  await settle(page);
  if (!reference) await page.locator('.catalog-loading').waitFor({ state: 'hidden' });
}

async function metrics(page, reference) {
  return page.locator(reference ? 'main[data-screen-label]' : '.catalog-page').evaluate((main, isReference) => {
    const box = element => { const r = element.getBoundingClientRect(); const s = getComputedStyle(element); return { x: r.x, y: r.y, width: r.width, height: r.height, font: s.fontFamily, size: s.fontSize, lineHeight: s.lineHeight, weight: s.fontWeight, color: s.color, gap: s.gap }; };
    const visible = element => element.getBoundingClientRect().width > 0 && element.getBoundingClientRect().height > 0;
    const images = [...main.querySelectorAll('img')].filter(visible);
    const grids = [...main.querySelectorAll('div')].filter(element => getComputedStyle(element).display === 'grid' && element.querySelector('img'));
    return {
      viewport: [innerWidth, innerHeight], scrollWidth: document.documentElement.scrollWidth,
      main: box(main), text: main.innerText, grids: grids.map(element => ({ ...box(element), columns: getComputedStyle(element).gridTemplateColumns })),
      images: images.map(element => ({ ...box(element), src: element.src, loaded: element.complete && element.naturalWidth > 0 })),
      titles: images.filter(element => getComputedStyle(element.parentElement).aspectRatio === '2 / 3').map(element => element.parentElement.parentElement.children[1].textContent.trim()),
      controls: [...main.querySelectorAll('button,input')].filter(visible).map(element => ({ label: element.innerText || element.getAttribute('placeholder') || element.getAttribute('aria-label') || element.getAttribute('title'), ...box(element), active: element.getAttribute('aria-pressed') || element.getAttribute('aria-checked') })),
    };
  }, reference);
}

async function verifyInteractions(page, scenario, name) {
  if (scenario.genre) {
    const visibility = await page.locator('.catalog-genres').evaluate(element => {
      const selected = element.querySelector('button[aria-pressed="true"]');
      const label = selected.getBoundingClientRect();
      const container = element.getBoundingClientRect();
      return { left: label.left, right: label.right, containerLeft: container.left, visibleRight: container.right - 40 };
    });
    assert(visibility.left >= visibility.containerLeft - .5 && visibility.right <= visibility.visibleRight + .5, `${name}: the active genre must remain before the trailing fade`);
    interactionChecks.push({ name, check: 'The selected genre stays visible after genre ordering and tool-width changes.', passed: true });
  }
  if (scenario.name === 'movies-genre') {
    const trigger = page.getByRole('button', { name: '\u7b5b\u9009', exact: true });
    await trigger.click();
    const toggle = page.getByRole('switch');
    await toggle.click();
    await page.getByRole('button', { name: '\u6e05\u9664\u7b5b\u9009', exact: true }).click();
    assert.equal(await toggle.getAttribute('aria-checked'), 'false');
    assert.equal(await page.locator('.catalog-genres').getByRole('button', { name: scenario.genre, exact: true }).getAttribute('aria-pressed'), 'true');
    await page.keyboard.press('Escape');
    assert.equal(await trigger.getAttribute('aria-expanded'), 'false');
    assert(await trigger.evaluate(element => element === document.activeElement));
    interactionChecks.push({ name, check: 'Clear quality/viewing filters preserves the selected genre; Escape closes the menu and restores trigger focus.', passed: true });
  }
  if (scenario.year || scenario.kind === 'search' && scenario.quality) {
    assert.equal(await page.locator('.catalog-search-box input').inputValue(), '');
    interactionChecks.push({ name, check: 'Independent year/quality browsing leaves the name search field empty.', passed: true });
  }
}

try {
  for (let attempt = 0; attempt < 50; attempt++) {
    if (server.exitCode !== null) throw new Error(serverLog);
    try { if ((await fetch(`${origin}/__fixture/health`)).ok) break; } catch { /* Wait for the owned fixture server. */ }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  for (const width of selectedWidths) {
    const height = width < 720 ? 852 : 900;
    for (const scenario of scenarios) {
      await configure(scenario);
      for (const reference of [true, false]) {
        const context = await browser.newContext({ viewport: { width, height }, locale: 'zh-CN', reducedMotion: 'reduce', timezoneId: 'Asia/Shanghai' });
        await context.addInitScript(({ reference, scenario, favorites, recent, origin, token, user }) => {
          if (reference) {
            const route = scenario.kind === 'movies' || scenario.kind === 'series' ? { name: 'wall', kind: scenario.kind === 'movies' ? 'movie' : 'series' } : { name: scenario.kind === 'favorites' ? 'favs' : 'search' };
            localStorage.setItem('goby.route.A', JSON.stringify(route));
            localStorage.setItem('goby.proto.v1', JSON.stringify({ favs: scenario.name === 'favorites-empty' ? {} : Object.fromEntries(favorites.map(id => [id, 1])), sp: {}, prog: { guitu: { f: .38 }, drift: { f: .15 } }, watched: { lighthouse: 1, typist: 1 }, recent, set: { motion: false } }));
          } else {
            localStorage.setItem('goby.player.session.v1', JSON.stringify({ AccessToken: token, User: user, ServerId: 'fixture-server', serverUrl: origin }));
            localStorage.setItem('goby.catalog.fixture-server.fixture-viewer.searches', JSON.stringify(recent));
          }
        }, { reference, scenario, favorites, recent, origin, token: TOKEN, user });
        const page = await context.newPage();
        if (!reference) {
          await page.route(/\/emby\/(?:Users\/[^/]+\/)?Items\?/, async route => {
            const query = new URL(route.request().url()).searchParams;
            if (query.get('Is4K') !== 'true') return route.continue();
            const response = await route.fetch();
            const data = await response.json();
            const entries = data.Items.filter(item => {
              const sources = item.Type === 'Series' ? catalogue.items.filter(child => child.SeriesId === item.Id && child.Type === 'Episode') : [item];
              return sources.some(source => (source.MediaStreams ?? []).some(stream => stream.Type === 'Video' && stream.Width >= 3800));
            });
            return route.fulfill({ json: { Items: entries, TotalRecordCount: entries.length } });
          });
        }
        const name = `${width}x${height}-${scenario.name}-${reference ? 'reference' : 'implementation'}`;
        attemptedNames.add(name);
        page.on('pageerror', error => failures.push({ name, message: error.message }));
        try {
          await page.goto(reference ? `${origin}/reference/` : `${origin}/#/${scenario.kind}`);
          await interact(page, scenario, reference);
          await page.screenshot({ path: resolve(output, `${name}.png`), fullPage: false });
          const data = await metrics(page, reference);
          records.push({ name, reference, scenario, ...data });
          assert.equal(data.scrollWidth, width, `${name}: no document horizontal overflow`);
          if (!reference && phase === 'after') await verifyInteractions(page, scenario, name);
        } catch (error) { failures.push({ name, message: error.stack }); }
        await context.close();
        console.log(name);
      }
    }
  }
} catch (error) { failures.push({ phase: 'runner', message: error.stack }); }
finally {
  await browser.close(); server.kill('SIGTERM');
  const capturedNames = new Set(records.map(record => record.name));
  const allRecords = [...previous.records.filter(record => !capturedNames.has(record.name)), ...records];
  const allFailures = [...previous.failures.filter(failure => !attemptedNames.has(failure.name)), ...failures];
  const allChecks = [...(previous.interactionChecks ?? []).filter(check => !attemptedNames.has(check.name)), ...interactionChecks];
  const comparisons = allRecords.filter(record => !record.reference).map(implementation => {
    const reference = allRecords.find(record => record.name === implementation.name.replace('-implementation', '-reference'));
    const haveTitles = reference?.titles && implementation.titles;
    const titleSetMatches = haveTitles ? JSON.stringify([...reference.titles].sort()) === JSON.stringify([...implementation.titles].sort()) : null;
    return { name: implementation.name.replace('-implementation', ''), titleSetMatches, orderMatches: haveTitles ? JSON.stringify(reference.titles) === JSON.stringify(implementation.titles) : null, referenceColumns: reference?.grids[0]?.columns, implementationColumns: implementation.grids[0]?.columns };
  });
  await writeFile(resolve(output, 'results.json'), JSON.stringify({ phase, fixtureOnly: true, notes: [
    'Reference is the original rendered handoff with supported localStorage state; neither reference nor production DOM is modified.',
    'Both clients receive the source catalogue and corresponding original artwork through the isolated fixture.',
    'The movies-empty capture uses the same Chinese-library, biography-genre, and unwatched filters in both clients.',
    'Search-year compares the reference year query with the accepted independent production year filter; mixed name/year intersections are outside scope.',
    'Differences in favorite or search ordering and available quality controls are reported rather than hidden by DOM adjustment.',
    'Quality fixtures apply Is4K to actual source dimensions, including episodes for aggregated series results.',
  ], comparisons, interactionChecks: allChecks, records: allRecords, failures: allFailures }, null, 2));
  await writeFile(resolve(output, 'fixture-server.log'), serverLog);
  console.log(JSON.stringify({ phase, output, captures: records.length, failures: allFailures }));
  if (allFailures.length) process.exitCode = 1;
}
