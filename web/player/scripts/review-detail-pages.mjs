import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, readFile, realpath, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { chromium } from '@playwright/test';
import { createCatalogue, createUser, TOKEN } from '../tests/fixture-data.mjs';

// Render the unchanged handoff and production app against equivalent API fixtures.
// This harness is intentionally remote-only and never alters production DOM.
assert.equal(process.platform, 'linux');
const root = await realpath(process.env.GOBY_LIVE_ROOT ?? '/opt/goby-test/player-live-20261004-165c');
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), 'goby-player-live-20261004-165c');
const phase = process.env.GOBY_DETAIL_PHASE ?? 'before';
assert(['before', 'after'].includes(phase));
const finalOnly = process.env.GOBY_DETAIL_FINAL_ONLY === '1';
const output = resolve(root, 'artifacts/other-pages-review-20261005/detail', finalOnly ? 'after-final' : phase);
await mkdir(output, { recursive: true });
const port = 4223;
const origin = `http://127.0.0.1:${port}`;
const catalogue = createCatalogue();
const { raw } = JSON.parse(await readFile(new URL('../tests/data/catalogue.json', import.meta.url), 'utf8'));
const fixtureUser = { ...createUser(), Name: '\u9648\u9ed8' };
const hash = text => { let value = 2166136261; for (let index = 0; index < text.length; index++) { value ^= text.charCodeAt(index); value = Math.imul(value, 16777619); } return value >>> 0; };
const watched = Object.fromEntries([1, 2, 3, 4, 5, 6].map(episode => [`deep:1:${episode}`, 1]).concat([1, 2, 3, 4].map(episode => [`deep:2:${episode}`, 1])));
for (const item of catalogue.items) {
  const entry = raw.find(entry => entry[0] === (item.SeriesId || item.Id));
  if (!entry) continue;
  const progress = item.Id === 'deep-2-5' ? .52 : item.Id === 'guitu' ? .38 : 0;
  if (item.Type === 'Episode') item.RunTimeTicks = (42 + hash(`${item.SeriesId}${item.ParentIndexNumber - 1}-${item.IndexNumber - 1}`) % 17) * 600_000_000;
  Object.assign(item.UserData, { Played: !!watched[`${item.SeriesId}:${item.ParentIndexNumber}:${item.IndexNumber}`], PlaybackPositionTicks: Math.round(item.RunTimeTicks * progress), PlayedPercentage: progress * 100 });
  if (!item.MediaSources) continue;
  const source = item.MediaSources[0];
  source.RunTimeTicks = item.RunTimeTicks;
  const atmos = /全景声/.test(entry[11]);
  const stereo = /立体声/.test(entry[11]);
  source.MediaStreams = [
    { Index: 0, Type: 'Video', Codec: entry[9] === '4K' ? 'hevc' : 'h264', Width: entry[9] === '4K' ? 3840 : 1920, Height: entry[9] === '4K' ? 2160 : 1080, VideoRangeType: entry[10] === 'Dolby Vision' ? 'DOVI' : entry[10] || 'SDR' },
    { Index: 1, Type: 'Audio', Codec: atmos ? 'truehd' : stereo ? 'aac' : 'eac3', Profile: atmos ? 'Dolby Atmos' : '', Language: 'eng', Channels: atmos ? 8 : stereo ? 2 : 6, ChannelLayout: atmos ? '7.1' : stereo ? '2.0' : '5.1', IsDefault: true },
    { Index: 2, Type: 'Audio', Codec: 'ac3', Language: 'cmn', DisplayLanguage: '\u56fd\u8bed', Channels: 6, ChannelLayout: '5.1' },
    { Index: 3, Type: 'Subtitle', Codec: 'hdmv_pgs_subtitle', DisplayTitle: '\u7b80\u4f53\u4e2d\u6587', Language: 'chi', IsDefault: true },
    { Index: 4, Type: 'Subtitle', Codec: 'hdmv_pgs_subtitle', DisplayTitle: '\u7e41\u9ad4\u4e2d\u6587', Language: 'chi' },
    { Index: 5, Type: 'Subtitle', Codec: 'srt', DisplayTitle: '\u82f1\u6587', Language: 'eng', IsExternal: true },
    { Index: 6, Type: 'Subtitle', Codec: 'srt', DisplayTitle: '\u4e2d\u82f1\u53cc\u8bed', Language: 'chi', IsExternal: true },
  ];
  item.MediaStreams = source.MediaStreams;
}
const server = spawn(process.execPath, [resolve(import.meta.dirname, '../tests/fixture-server.mjs')], { env: { ...process.env, GOBY_FIXTURE_PORT: String(port), GOBY_HANDOFF_DIR: resolve(root, 'handoff-v3') }, stdio: ['ignore', 'pipe', 'pipe'] });
let serverLog = '';
server.stdout.on('data', chunk => { serverLog += chunk; });
server.stderr.on('data', chunk => { serverLog += chunk; });
const browser = await chromium.launch({ headless: true });
const records = [];
const failures = [];
const widths = finalOnly ? [[1440, 900], [393, 852], [375, 852]] : [[1440, 900], [393, 852], [375, 852], [1040, 780], [1040, 600]];
async function settle(page, delay = 450) { await page.evaluate(() => document.fonts.ready); await page.waitForTimeout(delay); }

async function record(page, kind, id, stage, suffix, width, height) {
  const name = `${width}x${height}-${kind}-${id}-${stage}${suffix ? `-${suffix}` : ''}`;
  const section = kind === 'reference' ? page.locator(`[data-screen-label="详情 · ${stage === 'overview' ? '概览' : stage === 'episodes' ? '剧集' : '演职员'}"]`) : page.locator('.deck-panel[data-active="true"]');
  const metrics = await section.evaluate(element => {
    const box = element => { const b = element.getBoundingClientRect(); const s = getComputedStyle(element); return { tag: element.tagName, class: element.className, text: element.textContent.trim().slice(0, 120), x: b.x, y: b.y, width: b.width, height: b.height, bottom: b.bottom, right: b.right, fontSize: s.fontSize, fontWeight: s.fontWeight, opacity: s.opacity, display: s.display, padding: s.padding, color: s.color, background: s.backgroundColor, overflow: s.overflow }; };
    const visible = element => { const s = getComputedStyle(element); const b = element.getBoundingClientRect(); return s.visibility !== 'hidden' && s.display !== 'none' && b.width > 0 && b.height > 0; };
    const referencePlay = element.querySelector('[data-cpw]');
    const playDisc = element.querySelector('.play-disc') ?? referencePlay?.firstElementChild;
    return { viewport: [innerWidth, innerHeight], documentWidth: document.documentElement.scrollWidth, playDisc: playDisc ? box(playDisc) : null, nodes: [...element.querySelectorAll('h1,h2,h3,p,button,[data-cur],.detail-episode-card,.detail-person,.detail-similar-poster')].filter(visible).map(box), menus: [...element.querySelectorAll('.detail-menu')].filter(visible).map(box), scrolls: [...element.querySelectorAll('[data-pgs],.stage-scroll')].map(element => ({ ...box(element), scrollHeight: element.scrollHeight, clientHeight: element.clientHeight, scrollTop: element.scrollTop })), images: [...element.querySelectorAll('img')].filter(visible).map(image => ({ ...box(image), src: image.src, loaded: image.complete && image.naturalWidth > 0 })) };
  });
  await page.screenshot({ path: resolve(output, `${name}.png`), fullPage: false });
  records.push({ name, kind, id, stage, ...metrics });
  if (kind === 'implementation' && phase === 'after') {
    assert.equal(metrics.documentWidth, width, `${name}: page must stay within the viewport.`);
    for (const menu of metrics.menus) assert(menu.x >= 15 && menu.right <= width - 15 && menu.y >= 15 && menu.bottom <= height - 15, `${name}: menu must stay inside the viewport.`);
  }
}

async function open(kind, id, stage, width, height) {
  const pageIndex = stage === 'overview' ? 0 : stage === 'episodes' ? 1 : id === 'deep' ? 2 : 1;
  const context = await browser.newContext({ viewport: { width, height }, locale: 'zh-CN', timezoneId: 'Asia/Shanghai', reducedMotion: 'reduce' });
  await context.addInitScript(({ kind, id, pageIndex, origin, token, user, watched }) => {
    if (kind === 'reference') {
      localStorage.setItem('goby.proto.v1', JSON.stringify({ favs: { deep: 1 }, sp: { deep: { s: 2, e: 5 } }, prog: { 'deep:2:5': { f: .52 }, guitu: { f: .38 } }, watched, recent: [], sel: {}, set: { motion: false } }));
      localStorage.setItem('goby.route.A', JSON.stringify({ name: 'detail', id, page: pageIndex }));
    } else {
      localStorage.setItem('goby.player.session.v1', JSON.stringify({ AccessToken: token, User: user, ServerId: 'fixture-server', serverUrl: origin }));
      sessionStorage.setItem(`goby.player.page.fixture-server.fixture-viewer.detail.${id}`, String(pageIndex));
    }
  }, { kind, id, pageIndex, origin, token: TOKEN, user: fixtureUser, watched });
  if (kind === 'implementation') {
    await context.route(/\/emby\/Items\/[^/]+\/Similar\?/, route => {
      const item = catalogue.items.find(item => item.Id === id);
      const list = catalogue.items.filter(entry => ['Movie', 'Series'].includes(entry.Type) && entry.Id !== id).map(entry => ({ entry, score: entry.Genres.filter(genre => item.Genres.includes(genre)).length * 2 + (entry.Type === item.Type ? 1 : 0) + entry.CommunityRating / 10 })).sort((a, b) => b.score - a.score).slice(0, 10).map(entry => entry.entry);
      return route.fulfill({ json: { Items: list, TotalRecordCount: list.length } });
    });
    await context.route(/\/emby\/Items\/(deep|guitu)\/Images\/Primary\?/, async route => {
      const target = new URL(route.request().url());
      if (Number(target.searchParams.get('MaxWidth')) > 320) return route.continue();
      const imageId = catalogue.imageIds[target.pathname.split('/')[3]].primary;
      await route.fulfill({ response: await context.request.get(`${origin}/__fixture/picsum/id/${imageId}/64/64`) });
    });
  }
  const page = await context.newPage();
  page.on('pageerror', error => failures.push({ kind, id, stage, message: error.message }));
  await page.clock.setFixedTime(new Date('2026-10-05T04:00:00.000Z'));
  await page.goto(kind === 'reference' ? `${origin}/reference/` : `${origin}/#/detail/${id}`);
  const ready = kind === 'reference' ? page.locator(`[data-pgs="${stage === 'overview' ? 'hero' : stage === 'episodes' ? 'eps' : 'people'}"]`) : page.locator(`.deck-panel[data-active="true"] .detail-${stage === 'overview' ? 'overview-info' : stage === 'episodes' ? 'episode-info' : 'people-section'}`);
  await ready.waitFor();
  await page.mouse.move(0, 0);
  await settle(page, 1800);
  return { context, page };
}

try {
  for (let attempt = 0; attempt < 50; attempt++) {
    if (server.exitCode !== null) throw new Error(serverLog);
    try { if ((await fetch(`${origin}/__fixture/health`)).ok) break; } catch { /* Wait for the owned fixture server. */ }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  const configured = await fetch(`${origin}/__fixture/config`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...catalogue, user: fixtureUser }) });
  assert(configured.ok);
  for (const [width, height] of widths) {
    for (const id of finalOnly ? ['deep'] : ['deep', 'guitu']) {
      for (const stage of finalOnly ? ['overview', 'episodes'] : id === 'deep' ? ['overview', 'episodes', 'people'] : ['overview', 'people']) {
        for (const kind of ['reference', 'implementation']) {
          const { context, page } = await open(kind, id, stage, width, height);
          try {
            await record(page, kind, id, stage, 'idle', width, height);
            if (stage === 'overview' && id === 'deep' && !finalOnly) {
              for (const label of ['画质', '音轨', '字幕', '播放器']) {
                await page.getByTitle(`选择${label}`, { exact: true }).click();
                await settle(page);
                await record(page, kind, id, stage, label === '画质' ? 'quality' : label === '音轨' ? 'audio' : label === '字幕' ? 'subtitles' : 'player', width, height);
                await page.keyboard.press('Escape');
              }
            }
            if (stage === 'episodes') {
              const strip = kind === 'reference' ? page.locator('[data-pgs="eps"] [data-hscroll="1"]') : page.locator('.detail-episode-rail');
              await strip.hover({ position: { x: 20, y: 12 } });
              await settle(page, 1600);
              await record(page, kind, id, stage, 'expanded', width, height);
              const card = kind === 'reference' ? page.locator('[data-pgs="eps"] [data-foc="1"]') : page.locator('.detail-episode-card.is-focused');
              await card.hover();
              await settle(page, 900);
              await record(page, kind, id, stage, 'focused', width, height);
              const next = kind === 'reference' ? page.locator('[data-pgs="eps"] [data-foc]').nth(5) : page.locator('.detail-episode-card').nth(5);
              const railPosition = kind === 'implementation' ? await strip.evaluate(element => element.scrollLeft) : null;
              await next.hover();
              await settle(page, 1000);
              await record(page, kind, id, stage, 'hover-next', width, height);
              if (kind === 'implementation' && phase === 'after') assert.equal(await page.locator('.detail-episode-card.is-focused').getAttribute('data-episode-id'), 'deep-2-6', 'Hover must stay on the requested episode without recentering onto a neighbor.');
              if (kind === 'implementation' && phase === 'after' && width >= 1040) assert.equal(await strip.evaluate(element => element.scrollLeft), railPosition, 'Hovering a visible episode must not move the rail.');
              await page.mouse.move(0, 0);
              await settle(page, 800);
              await page.getByTitle('概览', { exact: true }).click();
              await page.mouse.move(0, 0);
              await settle(page, 1100);
              await record(page, kind, id, 'overview', 'after-episode-hover', width, height);
              if (kind === 'implementation' && phase === 'after') {
                assert.match(await page.locator('.detail-overview-page .play-disc').getAttribute('aria-label'), /继续播放第 5 集/);
                assert.equal(await page.locator('.detail-finish').textContent(), '预计 12:21 结束', 'Overview must retain the saved resume target after an episode hover.');
              }
              await page.getByTitle('剧集', { exact: true }).click();
              await settle(page, 1100);
              if (!finalOnly) {
                const season = kind === 'reference' ? page.locator('[data-pgs="eps"]').getByRole('button', { name: '第 1 季', exact: true }) : page.getByRole('tab', { name: '第 1 季', exact: true });
                await season.click();
                await settle(page, 1000);
                await record(page, kind, id, stage, 'season-one', width, height);
              }
            }
            if (width < 720 && !finalOnly) {
              const scroll = kind === 'reference' ? page.locator(`[data-pgs="${stage === 'overview' ? 'hero' : stage === 'episodes' ? 'eps' : 'people'}"]`) : page.locator('.deck-panel[data-active="true"] .stage-scroll');
              await scroll.evaluate(element => { element.scrollTop = element.scrollHeight; });
              await settle(page);
              await record(page, kind, id, stage, 'bottom', width, height);
            }
          } finally { await context.close(); }
        }
      }
    }
  }
} catch (error) { failures.push({ kind: 'runner', message: error.stack }); process.exitCode = 1; }
finally {
  await browser.close(); server.kill('SIGTERM');
  await writeFile(resolve(output, 'results.json'), JSON.stringify({ phase, fixtureOnly: true, notes: ['Reference HTML is unchanged; both clients receive equivalent catalogue, resume, watched, and preference state.', 'Similar items use the reference ranking through isolated API fixtures.', 'Screenshots are visual evidence; technical assertions alone are not evidence of visual agreement.'], records, failures }, null, 2));
  await writeFile(resolve(output, 'fixture-server.log'), serverLog);
  console.log(JSON.stringify({ phase, output, captures: records.length, failures }));
}
