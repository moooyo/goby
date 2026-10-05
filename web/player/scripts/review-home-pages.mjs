import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, readFile, realpath, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { chromium } from '@playwright/test';
import { createCatalogue, createUser, TOKEN } from '../tests/fixture-data.mjs';

// This review uses the original rendered handoff and isolated API fixtures.
// The production DOM is never rewritten, and no real media state is changed.
assert.equal(process.platform, 'linux', 'Run visual verification on test-env.');
const root = await realpath(process.env.GOBY_LIVE_ROOT ?? '/opt/goby-test/player-live-20261004-165c');
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), 'goby-player-live-20261004-165c');
const phase = process.env.GOBY_HOME_PHASE ?? 'before';
assert(['before', 'after'].includes(phase));
const resumeOnly = process.env.GOBY_HOME_FINAL_RESUME === '1';
const finalReview = resumeOnly || process.env.GOBY_HOME_FINAL === '1';
const output = resolve(root, 'artifacts/other-pages-review-20261005/home', finalReview ? 'final' : phase);
await mkdir(output, { recursive: true });
const origin = 'http://127.0.0.1:4221';
const fixtureUser = { ...createUser(), Name: '\u9648\u9ed8' };
const { raw } = JSON.parse(await readFile(new URL('../tests/data/catalogue.json', import.meta.url), 'utf8'));
const hash = value => { let result = 2166136261; for (const char of value) { result ^= char.charCodeAt(0); result = Math.imul(result, 16777619); } return result >>> 0; };
const progress = { 'deep:2:5': [.52, 1], guitu: [.38, 20], 'rain:1:3': [.3, 30], 'bamboo:2:4': [.78, 50], drift: [.15, 90], 'neon:1:2': [.6, 120] };
const at = hours => Date.parse('2026-10-04T00:00:00Z') - hours * 3600000;
function referenceState(empty = false) {
  return { favs: { deep: 1, lighthouse: 1, startrail: 1, plateau: 1, typist: 1, bamboo: 1 }, watched: {}, recent: [],
    sp: empty ? {} : { deep: { s: 2, e: 5 }, rain: { s: 1, e: 3 }, bamboo: { s: 2, e: 4 }, neon: { s: 1, e: 2 } },
    prog: empty ? {} : Object.fromEntries(Object.entries(progress).map(([key, [f, hours]]) => [key, { f, at: at(hours) }])),
    set: { motion: false, heroRotate: false } };
}
function fixtureCatalogue(empty = false) {
  const catalogue = createCatalogue();
  for (const item of catalogue.items) {
    const entry = raw.find(row => row[0] === (item.SeriesId || item.Id));
    if (!entry || item.Type === 'Season') continue;
    Object.assign(item.UserData, { PlaybackPositionTicks: 0, PlayedPercentage: 0, Played: false });
    if (item.Type === 'Episode') item.RunTimeTicks = (42 + hash(`${item.SeriesId}${item.ParentIndexNumber - 1}-${item.IndexNumber - 1}`) % 17) * 600000000;
    if (item.Type === 'Series') {
      const episode = catalogue.items.find(child => child.SeriesId === item.Id && child.Type === 'Episode');
      item.MediaStreams = structuredClone(episode.MediaStreams);
    }
    const streams = item.MediaSources?.[0]?.MediaStreams ?? item.MediaStreams;
    const audio = streams?.find(stream => stream.Type === 'Audio');
    if (audio) Object.assign(audio, { Codec: /全景声/.test(entry[11]) ? 'truehd' : /立体声/.test(entry[11]) ? 'aac' : 'ac3', Profile: /全景声/.test(entry[11]) ? 'Dolby Atmos' : undefined });
    if (item.MediaSources?.[0]) item.MediaSources[0].RunTimeTicks = item.RunTimeTicks;
    const key = item.Type === 'Episode' ? `${item.SeriesId}:${item.ParentIndexNumber}:${item.IndexNumber}` : item.Id;
    if (!empty && progress[key]) {
      const [fraction, hours] = progress[key];
      Object.assign(item.UserData, { PlaybackPositionTicks: Math.round(item.RunTimeTicks * fraction), PlayedPercentage: fraction * 100, LastPlayedDate: new Date(at(hours)).toISOString() });
    }
  }
  return catalogue;
}
const server = spawn(process.execPath, [resolve(import.meta.dirname, '../tests/fixture-server.mjs')], { env: { ...process.env, GOBY_FIXTURE_PORT: '4221', GOBY_HANDOFF_DIR: resolve(root, 'handoff-v3') }, stdio: ['ignore', 'pipe', 'pipe'] });
let serverLog = '';
server.stdout.on('data', chunk => { serverLog += chunk; }); server.stderr.on('data', chunk => { serverLog += chunk; });
const browser = await chromium.launch({ headless: true });
const retained = resumeOnly ? JSON.parse(await readFile(resolve(output, 'results.json'), 'utf8')).records.filter(record => !(record.kind === 'implementation' && record.name.endsWith('-mine-expanded'))) : [];
const records = [...retained], failures = [];
const labels = ['精选', '继续观看', '最新电影', '最新剧集'];
const keys = ['hero', 'mine', 'nm', 'ns'];
const widths = finalReview ? [[1440, 900], [393, 852]] : [[1440, 900], [1040, 780], [393, 852], [375, 852]];
async function settle(page) {
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(1100);
  await page.evaluate(async () => { await Promise.all([...document.images].filter(image => { const box = image.getBoundingClientRect(); return box.width > 0 && box.x < innerWidth && box.right > 0 && box.y < innerHeight && box.bottom > 0; }).map(image => image.complete ? undefined : new Promise(resolve => { image.onload = resolve; image.onerror = resolve; setTimeout(resolve, 5000); }))); });
}
async function capture(page, name, index, reference) {
  await settle(page);
  await page.screenshot({ path: resolve(output, `${name}.png`), fullPage: false });
  const metrics = await page.evaluate(({ index, reference, keys }) => {
    const active = reference ? document.querySelector(`[data-pgs="${keys[index]}"]`) : document.querySelector('.deck-panel[data-active="true"]');
    const info = reference ? index === 0 ? active?.firstElementChild?.firstElementChild : active?.querySelector('[data-cp-info]')?.firstElementChild : active?.querySelector(index === 0 ? '.feature-info' : '.strip-info, .stage-empty');
    const cards = reference ? active?.querySelector('[data-hscroll]') : active?.querySelector('.stage-rail-cards');
    const rail = reference ? cards?.parentElement?.parentElement : active?.querySelector('.stage-rail');
    const geometry = element => {
      if (!element) return null;
      const box = element.getBoundingClientRect(), style = getComputedStyle(element);
      return { x: box.x, y: box.y, width: box.width, height: box.height, bottom: box.bottom, right: box.right, color: style.color, opacity: style.opacity, fontSize: style.fontSize, lineHeight: style.lineHeight, fontFamily: style.fontFamily, margin: style.margin, text: element.textContent.trim().slice(0, 600) };
    };
    const navButtons = [...document.querySelectorAll(reference ? 'button' : '.top-nav button, .mobile-tools button')].filter(button => { const box = button.getBoundingClientRect(); return box.width > 0 && getComputedStyle(button).visibility !== 'hidden' && (box.top < 80 || innerWidth < 720 && box.top > innerHeight - 100); });
    return { viewport: [innerWidth, innerHeight], documentWidth: document.documentElement.scrollWidth, navigation: navButtons.map(button => ({ ...geometry(button), background: getComputedStyle(button).backgroundColor, borderRadius: getComputedStyle(button).borderRadius, fontWeight: getComputedStyle(button).fontWeight })), controls: [...(info?.querySelectorAll('button') ?? [])].filter(button => button.getBoundingClientRect().width > 0).map(geometry), info: geometry(info), title: geometry(info?.querySelector('h1,h3')), meta: geometry(reference ? info?.querySelector('h1,h3')?.nextElementSibling : info?.querySelector('.media-meta')), overview: geometry(info?.querySelector('p')), rail: geometry(rail), cards: [...(cards?.children ?? [])].slice(0, 6).map(geometry), images: [...(active?.querySelectorAll('img') ?? [])].map(image => ({ src: image.src, loaded: image.complete && image.naturalWidth > 0 })) };
  }, { index, reference, keys });
  records.push({ name, kind: reference ? 'reference' : 'implementation', ...metrics });
  console.log(JSON.stringify({ captured: name, title: metrics.title?.text, info: metrics.info && [metrics.info.x, metrics.info.y, metrics.info.width, metrics.info.height], rail: metrics.rail && [metrics.rail.y, metrics.rail.height] }));
}
async function createPage(width, height, reference, empty = false) {
  const context = await browser.newContext({ viewport: { width, height }, hasTouch: width < 720, isMobile: width < 720, locale: 'zh-CN', reducedMotion: 'reduce', timezoneId: 'Asia/Shanghai' });
  await context.addInitScript(({ reference, state, token, user, origin, empty }) => {
    if (reference) {
      localStorage.setItem('goby.proto.v1', JSON.stringify(state));
      localStorage.setItem('goby.route.A', JSON.stringify({ name: 'home', page: empty ? 1 : 0 }));
    } else {
      localStorage.setItem('goby.player.session.v1', JSON.stringify({ AccessToken: token, User: user, ServerId: 'fixture-server', serverUrl: origin }));
      localStorage.setItem('goby.player.preferences.v1.fixture-server.fixture-viewer.global', JSON.stringify({ heroRotate: false, backgroundMotion: false }));
      if (empty) sessionStorage.setItem('goby.player.page.fixture-server.fixture-viewer.home', '1');
    }
  }, { reference, state: referenceState(empty), token: TOKEN, user: fixtureUser, origin, empty });
  if (!reference) await context.route('**/Items/Resume?**', async route => {
    const items = fixtureCatalogue(empty).items.filter(item => item.UserData?.PlaybackPositionTicks > 0).sort((a, b) => b.UserData.LastPlayedDate.localeCompare(a.UserData.LastPlayedDate));
    await route.fulfill({ json: { Items: items, TotalRecordCount: items.length } });
  });
  const page = await context.newPage();
  page.on('pageerror', error => failures.push({ reference, width, message: error.message }));
  await page.goto(`${origin}/${reference ? 'reference/' : '#/'}`);
  await page.locator(reference ? `[data-pgs="${empty ? 'mine' : 'hero'}"] h1, [data-pgs="mine"] h3` : '.deck-panel[data-active="true"] h1').first().waitFor();
  await page.mouse.move(0, 0);
  return { context, page };
}
try {
  for (let attempt = 0; attempt < 50; attempt++) {
    if (server.exitCode !== null) throw new Error(serverLog);
    try { if ((await fetch(`${origin}/__fixture/health`)).ok) break; } catch { /* Wait for the owned fixture. */ }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  await fetch(`${origin}/__fixture/config`, { method: 'POST', body: JSON.stringify({ ...fixtureCatalogue(), user: fixtureUser }) });
  for (const [width, height] of widths) {
    for (const reference of resumeOnly ? [false] : [true, false]) {
      const { context, page } = await createPage(width, height, reference);
      const kind = reference ? 'reference' : 'implementation';
      if (finalReview && !reference) await page.waitForFunction(() => performance.now() >= Number(document.querySelector('.stage-deck')?.dataset.lockedUntil ?? 0));
      for (let index = 0; index < labels.length; index++) {
        if (finalReview && !index) continue;
        if (resumeOnly && index !== 1) continue;
        if (index) {
          if (reference) await page.getByTitle(labels[index], { exact: true }).click();
          else await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: labels[index], exact: true }).click();
          await page.mouse.move(0, 0);
        }
        if (finalReview) {
          if (width >= 720) {
            const row = reference ? page.locator(`[data-pgs="${keys[index]}"] [data-hscroll]`) : page.locator('.deck-panel[data-active="true"] .stage-rail-cards');
            const rail = reference ? row.locator('xpath=../..') : page.locator('.deck-panel[data-active="true"] .stage-rail');
            await rail.hover({ position: { x: 180, y: 15 } });
            await page.waitForTimeout(700);
          }
          await capture(page, `${width}x${height}-${kind}-${keys[index]}-expanded`, index, reference);
          await page.mouse.move(0, 0);
          await page.waitForTimeout(700);
          continue;
        }
        await capture(page, `${width}x${height}-${kind}-${keys[index]}-idle`, index, reference);
        if (!index) {
          const heroControls = reference ? page.locator('[data-pgs="hero"] button') : page.getByRole('button', { name: '精选：竹林客', exact: true });
          if (reference) await heroControls.nth(3).click();
          else await heroControls.click();
          await page.mouse.move(0, 0);
          await capture(page, `${width}x${height}-${kind}-hero-second`, 0, reference);
          if (reference) await heroControls.nth(2).click();
          else await page.getByRole('button', { name: '精选：深海回响', exact: true }).click();
          await page.mouse.move(0, 0);
        }
        if (index && width >= 720) {
          const row = reference ? page.locator(`[data-pgs="${keys[index]}"] [data-hscroll]`) : page.locator('.deck-panel[data-active="true"] .stage-rail-cards');
          const rail = reference ? row.locator('xpath=../..') : page.locator('.deck-panel[data-active="true"] .stage-rail');
          await rail.hover({ position: { x: 180, y: 15 } });
          await page.waitForTimeout(700);
          await capture(page, `${width}x${height}-${kind}-${keys[index]}-expanded`, index, reference);
          await row.locator(':scope > *').nth(1).hover();
          await capture(page, `${width}x${height}-${kind}-${keys[index]}-second`, index, reference);
          await page.mouse.move(0, 0); await page.waitForTimeout(700);
        }
      }
      await context.close();
    }
  }
  if (!resumeOnly) {
    await fetch(`${origin}/__fixture/config`, { method: 'POST', body: JSON.stringify({ ...fixtureCatalogue(true), user: fixtureUser }) });
    for (const [width, height] of [[1440, 900], [393, 852]]) {
      for (const reference of [true, false]) {
        const { context, page } = await createPage(width, height, reference, true);
        await capture(page, `${width}x${height}-${reference ? 'reference' : 'implementation'}-resume-empty`, 1, reference);
        await context.close();
      }
    }
  }
} catch (error) { failures.push({ phase: 'runner', message: error.stack }); process.exitCode = 1; }
finally {
  await browser.close(); server.kill('SIGTERM');
  const comparisons = records.filter(record => record.kind === 'implementation').map(record => {
    const reference = records.find(candidate => candidate.name === record.name.replace('-implementation-', '-reference-'));
    return { name: record.name, documentWidth: record.documentWidth, elements: Object.fromEntries(['title', 'meta', 'overview', 'rail'].map(key => [key, record[key] && reference?.[key] ? Object.fromEntries(['x', 'y', 'width', 'height'].map(dimension => [dimension, Number((record[key][dimension] - reference[key][dimension]).toFixed(3))])) : null])) };
  });
  await writeFile(resolve(output, 'results.json'), JSON.stringify({ phase, fixtureOnly: true, notes: ['The exact original handoff is rendered through its normal localStorage inputs.', 'Catalogue artwork, progress, episode duration, and stream capabilities are replayed as isolated API fixtures.', 'The implementation selects its featured set from recent additions; the reference uses six editorial picks. The first two entries are common and captured, while later thumbnails and editorial tags are documented as product-data differences.', 'Empty resume is rendered in both versions; an entirely empty catalogue has no equivalent original handoff state.', 'Geometry deltas are measured evidence, not an automated assertion of visual equivalence. Screenshots require direct review.', 'No production DOM or production API data is mutated.'], records, comparisons, failures }, null, 2));
  await writeFile(resolve(output, 'fixture-server.log'), serverLog);
  console.log(JSON.stringify({ phase, output, captures: records.length, updatedCaptures: records.length - retained.length, failures }));
}
