import { chromium } from '@playwright/test';
import assert from 'node:assert/strict';
import { readFile, mkdir, writeFile, lstat, realpath, readdir } from 'node:fs/promises';
import { isAbsolute, relative, resolve } from 'node:path';
import { execFileSync } from 'node:child_process';

const root = process.env.GOBY_LIVE_ROOT;
assert(root, 'Provide the explicitly owned remote acceptance root.');
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), 'goby-player-live-20261004-165c');
const config = JSON.parse(await readFile(resolve(root, 'browser.private.json'), 'utf8'));
assert.equal(config.baseURL, 'http://127.0.0.1:38974');
const ownedRoot = await realpath(root);
function assertOwned(path) {
  const location = relative(ownedRoot, path);
  assert(location !== '..' && !location.startsWith('../') && !isAbsolute(location), 'An acceptance path escaped its owned root.');
}
assertOwned(await realpath(resolve(root, 'artifacts')));
const output = resolve(root, 'artifacts/live-capabilities');
await mkdir(output, { recursive: false }).catch(error => { if (error.code !== 'EEXIST') throw error; });
assertOwned(await realpath(output));
assert(!(await lstat(output)).isSymbolicLink(), 'Acceptance evidence must use a real directory.');
for (const name of await readdir(output)) assert(!(await lstat(resolve(output, name))).isSymbolicLink(), 'Acceptance evidence must not overwrite symbolic links.');
const fixture = JSON.parse(await readFile(resolve(output, 'fixture.json'), 'utf8'));
const results = [];
const failures = [];
const requests = [];
const reports = [];
const negotiations = [];
const browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
const consumer = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN', reducedMotion: 'reduce' });
const administrator = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN', reducedMotion: 'reduce' });
const page = await consumer.newPage();
const adminPage = await administrator.newPage();
let csrf = '';
let episode;
let previousConfiguration;
let cleaned = false;

function safe(value) {
  let text = String(value);
  for (const secret of [config.password, config.token, csrf].filter(Boolean)) text = text.replaceAll(secret, '[redacted]');
  return text.replace(/(https?:\/\/[^\s?'"<>]+)\?[^\s'"<>]+/g, '$1?[redacted]');
}

function observe(target, surface) {
  target.on('pageerror', error => failures.push({ surface, kind: 'page', message: safe(error.message) }));
  target.on('request', request => {
    const url = new URL(request.url());
    const query = Object.fromEntries([...url.searchParams].filter(([key]) => [
      'SearchTerm', 'StartIndex', 'Limit', 'Is4K', 'ExtendedVideoTypes', 'GobyAggregateVideoFilters',
      'PersonIds', 'GenreIds', 'Genres', 'Years', 'IncludeItemTypes', 'Static',
    ].includes(key)));
    requests.push({ surface, path: url.pathname, method: request.method(), query });
  });
  target.on('response', async response => {
    const path = new URL(response.url()).pathname;
    if (response.status() >= 400 && /^\/(emby|admin\/v1)\//.test(path)) failures.push({ surface, kind: 'http', path, status: response.status() });
    if (/\/Sessions\/Playing/.test(path)) {
      const body = response.request().postDataJSON() || {};
      reports.push({ path, status: response.status(), itemId: body.ItemId, positionTicks: body.PositionTicks,
        isPaused: body.IsPaused, playSessionId: body.PlaySessionId });
    }
    if (/\/PlaybackInfo$/.test(path) && response.ok()) {
      const value = await response.json().catch(() => ({}));
      negotiations.push({ path, playSessionId: value.PlaySessionId, chapters: value.MediaSources?.flatMap(source => source.Chapters || []) || [] });
    }
  });
}
observe(page, 'consumer');
observe(adminPage, 'administrator');

async function api(path, body) {
  const response = await consumer.request.fetch(config.baseURL + path, {
    method: body === undefined ? 'GET' : 'POST', headers: { 'X-Emby-Token': config.token }, data: body,
  });
  assert(response.ok(), `Consumer API failed: ${path.split('?')[0]} (${response.status()})`);
  const payload = await response.text();
  return payload ? JSON.parse(payload) : undefined;
}

async function admin(method, path, body) {
  const response = await administrator.request.fetch(config.baseURL + path, {
    method, headers: { Origin: config.baseURL, 'X-CSRF-Token': csrf }, data: body,
  });
  assert(response.ok(), `Administrator API failed: ${path} (${response.status()})`);
  const payload = await response.text();
  return payload ? JSON.parse(payload) : undefined;
}

async function persist() {
  await writeFile(resolve(output, 'results.json'), JSON.stringify({ results, failures, requests, negotiations, reports }, null, 2));
}

async function phase(name, action) {
  const evidence = await action();
  results.push({ name, passed: true, ...(evidence || {}) });
  console.log(JSON.stringify({ phase: name, passed: true }));
  await persist();
}

async function waitFor(check, message, timeout = 20_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (await check()) return;
    await new Promise(resolve => setTimeout(resolve, 200));
  }
  throw new Error(message);
}

async function playing(minimum = 0) {
  await page.waitForFunction(value => {
    const video = document.querySelector('.player-video');
    return Boolean(document.querySelector('.player-error')) || video && video.readyState >= 2 && !video.paused && video.currentTime > value;
  }, minimum, { timeout: 90_000 });
  if (await page.locator('.player-error').count()) throw new Error(await page.locator('.player-error p').innerText());
}

async function screenshot(target, name) {
  await target.screenshot({ path: resolve(output, `${name}.png`), fullPage: true });
}

function noEncoder() {
  return !execFileSync('docker', ['top', 'goby-player-165c-goby-1', '-eo', 'pid,comm'], { encoding: 'utf8' })
    .split('\n').some(line => /\bffmpeg$/.test(line.trim()));
}

function userHistory(item) {
  const state = item.UserData || {};
  return Object.fromEntries(['PlaybackPositionTicks', 'PlayCount', 'Played', 'LastPlayedDate'].map(key => [key, state[key]]));
}

async function creditsReset() {
  if (!episode) return;
  const current = await admin('GET', `/admin/v1/items/${episode.Id}/credits`);
  if (current.Override) await admin('DELETE', `/admin/v1/items/${episode.Id}/credits`, {
    Revision: current.Revision, SourceRevision: current.SourceRevision,
  });
}

async function startFirstEpisode() {
  const negotiationIndex = negotiations.length;
  await page.goto(`${config.baseURL}/#/player/${episode.Id}`);
  await playing(0);
  await page.mouse.move(700, 810);
  await page.getByRole('slider', { name: '播放进度', exact: true }).focus();
  await page.keyboard.press('Home');
  await page.waitForFunction(() => {
    const video = document.querySelector('.player-video');
    return video && !video.seeking && video.currentTime < 1;
  });
  await playing(0.2);
  await waitFor(() => negotiations.slice(negotiationIndex).some(value => value.playSessionId), 'The episode did not establish a new play session.');
  return negotiations.at(-1).playSessionId;
}

try {
  const catalog = await api('/emby/Items?IncludeItemTypes=Movie,Series,Episode&Recursive=true&Fields=MediaSources,Chapters');
  const movie = catalog.Items.find(item => item.Type === 'Movie' && item.Name === 'Player Integration');
  const series = catalog.Items.find(item => item.Type === 'Series' && item.Name === 'Player Series');
  episode = catalog.Items.find(item => item.Type === 'Episode' && item.SeriesId === series?.Id && item.IndexNumber === 1);
  const next = catalog.Items.find(item => item.Type === 'Episode' && item.SeriesId === series?.Id && item.IndexNumber === 2);
  assert(movie && series && episode && next, 'Expected the owned real movie and two-episode series.');
  assert.equal(movie.Id, fixture.movieId);
  csrf = (await admin('POST', '/admin/v1/session', { Name: config.username, Password: config.password })).CSRFToken;
  const libraries = await admin('GET', '/admin/v1/libraries');
  const showLibrary = libraries.Items.find(item => item.Paths.includes('/media/Shows'));
  assert(showLibrary, 'Expected the owned show library.');
  previousConfiguration = await api(`/emby/Users/${config.userId}/Configuration`);
  await api(`/emby/Users/${config.userId}/Configuration/Partial`, { EnableNextEpisodeAutoPlay: true });
  await creditsReset();

  await phase('real consumer authentication', async () => {
    await page.bringToFront();
    await page.goto(config.baseURL);
    await page.getByLabel('用户名', { exact: true }).fill(config.username);
    await page.getByLabel('密码', { exact: true }).fill(config.password);
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await page.getByRole('navigation', { name: '主导航' }).waitFor();
  });

  await phase('Search Hints media, person and genre navigation', async () => {
    await page.goto(`${config.baseURL}/#/search`);
    const search = page.getByRole('textbox', { name: '搜索片名、演员、导演、类型或年份', exact: true });
    await search.fill('Player Integration');
    await page.locator('.poster-card').filter({ hasText: 'Player Integration' }).waitFor();
    assert(requests.some(request => request.path === '/emby/Search/Hints' && request.query.SearchTerm === 'Player Integration'));
    await search.fill('Acceptance Actor');
    const actor = page.getByRole('button', { name: 'Acceptance Actor，演职员作品', exact: true });
    await actor.waitFor();
    await screenshot(page, 'search-person');
    await actor.click();
    await page.locator('.poster-card').filter({ hasText: 'Player Integration' }).waitFor();
    assert(requests.some(request => request.query.PersonIds));
    await search.fill('Documentary');
    await page.getByRole('button', { name: 'Documentary，类型影片', exact: true }).click();
    await page.locator('.poster-card').filter({ hasText: 'Player Integration' }).waitFor();
    assert(requests.some(request => request.query.GenreIds));
    await screenshot(page, 'search-genre');
    await search.fill('2026');
    await page.getByRole('button', { name: '浏览 2026 年影片', exact: true }).click();
    await page.locator('.poster-card').filter({ hasText: 'Player Integration' }).waitFor();
    assert(requests.some(request => request.query.Years === '2026'));
    return { personNavigation: true, genreNavigation: true, explicitYearNavigation: true };
  });

  await phase('4K and HDR filters match real indexed streams', async () => {
    await page.goto(`${config.baseURL}/#/movies`);
    await page.getByRole('button', { name: '筛选', exact: true }).click();
    const panel = page.getByRole('dialog', { name: '筛选', exact: true });
    const evidence = [];
    for (const choice of [
      { name: '4K', query: 'Is4K=true', field: 'is4K' },
      { name: 'HDR', query: 'ExtendedVideoTypes=Hdr10,Hdr10Plus,HyperLogGamma,DolbyVision', field: 'isHDR' },
    ]) {
      const expected = await api(`/emby/Items?IncludeItemTypes=Movie&Recursive=true&${choice.query}`);
      assert(expected.Items.length > 0, `${choice.name} must have real positive matches.`);
      for (const item of fixture.filters) assert.equal(expected.Items.some(candidate => candidate.Name === item.name), item[choice.field]);
      const responsePromise = page.waitForResponse(response => {
        const url = new URL(response.url());
        return /\/Items$/.test(url.pathname) && (choice.name === '4K' ? url.searchParams.get('Is4K') === 'true' : Boolean(url.searchParams.get('ExtendedVideoTypes')));
      });
      await panel.getByRole('button', { name: choice.name, exact: true }).click();
      const response = await responsePromise;
      const actual = await response.json();
      assert.equal(actual.TotalRecordCount, expected.TotalRecordCount);
      assert.deepEqual(actual.Items.map(item => item.Id).sort(), expected.Items.map(item => item.Id).sort());
      await waitFor(async () => (await page.locator('.poster-card strong').allTextContents()).sort().join('|') === expected.Items.map(item => item.Name).sort().join('|'), `${choice.name} wall did not match the server result.`);
      await screenshot(page, `filter-${choice.name.toLowerCase()}`);
      evidence.push({ filter: choice.name, count: actual.TotalRecordCount });
    }
    return { filters: evidence };
  });

  await phase('theme video decodes silently without playback sessions or history', async () => {
    const before = userHistory(await api(`/emby/Users/${config.userId}/Items/${movie.Id}`));
    await waitFor(noEncoder, 'An encoder was active before the theme preview test.');
    const requestIndex = requests.length;
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await page.goto(`${config.baseURL}/#/detail/${movie.Id}`);
    await page.locator('.detail-overview-info').waitFor();
    await page.waitForFunction(id => {
      const video = document.querySelector('.background-preview');
      return video?.dataset.previewOwner === id && video.readyState >= 2 && !video.paused && video.videoWidth === 1280
        && video.currentTime >= 0.8 && video.currentTime <= 2.2 && Number(getComputedStyle(video).opacity) >= 0.95;
    }, movie.Id, { timeout: 30_000 });
    const state = await page.locator('.background-preview').evaluate(video => ({ muted: video.muted, width: video.videoWidth, height: video.videoHeight, duration: video.duration }));
    assert.equal(state.muted, true);
    assert(state.duration >= 2.9 && state.duration <= 3.1);
    await screenshot(page, 'theme-video');
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.waitForFunction(() => {
      const video = document.querySelector('.background-preview');
      return video?.paused && !video.hasAttribute('src');
    });
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await page.waitForFunction(() => document.querySelector('.background-preview')?.videoWidth === 1280 && !document.querySelector('.background-preview')?.paused);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.waitForFunction(() => {
      const video = document.querySelector('.background-preview');
      return video?.paused && !video.hasAttribute('src');
    });
    await screenshot(page, 'theme-mobile-still');
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.setViewportSize({ width: 1440, height: 900 });
    const themeRequests = requests.slice(requestIndex).filter(request => request.surface === 'consumer');
    assert(themeRequests.some(request => /\/ThemeMedia$/.test(request.path)));
    assert(themeRequests.some(request => /\/Videos\/.+\/stream$/.test(request.path) && request.query.Static === 'true'));
    assert(!themeRequests.some(request => /\/PlaybackInfo$|\/Sessions\/Playing/.test(request.path)));
    assert.deepEqual(userHistory(await api(`/emby/Users/${config.userId}/Items/${movie.Id}`)), before);
    assert(noEncoder(), 'Theme preview started an encoder.');
    return { ...state, reducedMotionReleasesSource: true, mobileReleasesSource: true, createsPlaybackSession: false };
  });

  await phase('administrator saves a source-bound credits marker', async () => {
    await adminPage.bringToFront();
    await adminPage.goto(`${config.baseURL}/admin/media/libraries/${showLibrary.Id}/items`);
    await adminPage.getByRole('button', { name: `More actions for ${episode.Name}`, exact: true }).click();
    await adminPage.getByRole('menuitem', { name: '片尾标记', exact: true }).click();
    const dialog = adminPage.getByRole('dialog', { name: '片尾标记', exact: true });
    await dialog.waitFor();
    assert.equal(await adminPage.locator('#credits-editor-title').count(), 1, 'The credits dialog must have one unambiguous accessible title.');
    await dialog.getByLabel('片尾开始时间（秒）', { exact: true }).fill('5');
    await dialog.getByRole('button', { name: '保存片尾标记', exact: true }).click();
    await dialog.getByText('片尾开始时间已保存到当前片源。', { exact: true }).waitFor();
    const saved = await admin('GET', `/admin/v1/items/${episode.Id}/credits`);
    assert.equal(saved.Effective.StartTicks, 50_000_000);
    assert.equal(saved.Effective.Provenance, 'Manual');
    assert.equal(saved.OverrideStale, false);
    const detail = await api(`/emby/Users/${config.userId}/Items/${episode.Id}?Fields=Chapters,MediaSources`);
    assert(detail.Chapters.some(chapter => chapter.MarkerType === 'CreditsStart' && chapter.StartPositionTicks === 50_000_000));
    await screenshot(adminPage, 'credits-saved');
    return { startTicks: saved.Effective.StartTicks, provenance: saved.Effective.Provenance };
  });

  await phase('credits countdown pauses and cancellation keeps the current episode', async () => {
    await page.bringToFront();
    await page.goto(`${config.baseURL}/#/detail/${series.Id}`);
    await page.locator('.detail-overview-info').waitFor();
    await page.getByTitle('选择画质', { exact: true }).click();
    await page.getByRole('menuitemradio').filter({ hasText: /^原画/ }).click();
    await page.getByTitle('选择字幕', { exact: true }).click();
    await page.getByRole('menuitemradio', { name: /^关闭/ }).click();
    const session = await startFirstEpisode();
    await page.locator('.player-next').waitFor({ timeout: 30_000 });
    assert(negotiations.at(-1).chapters.some(chapter => chapter.MarkerType === 'CreditsStart' && chapter.StartPositionTicks === 50_000_000));
    await page.mouse.move(700, 810);
    await page.locator('.player-toggle').click();
    await page.waitForFunction(() => document.querySelector('.player-video')?.paused);
    await page.waitForTimeout(250);
    const paused = await page.locator('.player-next small').innerText();
    await page.waitForTimeout(2200);
    assert.equal(await page.locator('.player-next small').innerText(), paused);
    await screenshot(page, 'credits-paused');
    const cancelledAt = await page.locator('.player-video').evaluate(video => video.currentTime);
    await page.locator('.player-next').getByRole('button', { name: '取消', exact: true }).click();
    await page.locator('.player-next').waitFor({ state: 'hidden' });
    await page.locator('.player-toggle').click();
    await playing(cancelledAt + 12);
    assert.equal(page.url().split('#')[1], `/player/${episode.Id}`);
    assert.equal(await page.locator('.player-next').count(), 0);
    await page.keyboard.press('Escape');
    await page.locator('.detail-overview-info').waitFor();
    await waitFor(() => reports.some(report => /Stopped$/.test(report.path) && report.playSessionId === session), 'Cancelled credits playback did not retire its session.');
    return { pausedCountdown: paused, cancelledAtLeastSeconds: 12 };
  });

  await phase('credits automatically advance while preserving the actual stop position', async () => {
    const session = await startFirstEpisode();
    await page.locator('.player-next').waitFor({ timeout: 30_000 });
    await screenshot(page, 'credits-countdown');
    await page.waitForURL(`${config.baseURL}/#/player/${next.Id}`, { timeout: 25_000 });
    await playing(0.2);
    await waitFor(() => reports.some(report => /Stopped$/.test(report.path) && report.itemId === episode.Id && report.playSessionId === session), 'The previous episode did not send its terminal report.');
    const stopped = reports.find(report => /Stopped$/.test(report.path) && report.itemId === episode.Id && report.playSessionId === session);
    assert(stopped.positionTicks >= 140_000_000 && stopped.positionTicks < 250_000_000, 'Credits advancement must report the watched source position.');
    assert(stopped.positionTicks < episode.RunTimeTicks);
    await screenshot(page, 'credits-next-episode');
    await page.keyboard.press('Escape');
    await page.locator('.detail-overview-info').waitFor();
    await waitFor(noEncoder, 'An encoder remained after episode retirement.', 30_000);
    return { stoppedPositionTicks: stopped.positionTicks, durationTicks: episode.RunTimeTicks, nextEpisode: next.Name };
  });

  await phase('administrator clears the marker and public chapters return to normal', async () => {
    await adminPage.bringToFront();
    const dialog = adminPage.getByRole('dialog', { name: '片尾标记', exact: true });
    await dialog.getByRole('button', { name: '清除人工标记', exact: true }).click();
    await adminPage.getByRole('dialog', { name: '清除人工片尾标记？', exact: true }).getByRole('button', { name: '清除标记', exact: true }).click();
    await dialog.getByText('人工片尾标记已清除。若片源包含明确的片尾章节标记，将使用该标记。', { exact: true }).waitFor();
    const cleared = await admin('GET', `/admin/v1/items/${episode.Id}/credits`);
    assert.equal(cleared.Override, null);
    assert.equal(cleared.Effective, null);
    const detail = await api(`/emby/Users/${config.userId}/Items/${episode.Id}?Fields=Chapters,MediaSources`);
    assert(!(detail.Chapters || []).some(chapter => chapter.MarkerType === 'CreditsStart'));
    await screenshot(adminPage, 'credits-cleared');
    cleaned = true;
  });
  assert.deepEqual(failures, []);
  assert(reports.every(report => report.status < 400));
  await persist();
  console.log(JSON.stringify({ complete: true, phases: results.length, realBackend: true, docker: true }));
} catch (error) {
  await screenshot(page, 'consumer-failure').catch(() => undefined);
  await screenshot(adminPage, 'administrator-failure').catch(() => undefined);
  await writeFile(resolve(output, 'failure.json'), JSON.stringify({ message: safe(error.message), results, failures, requests, negotiations, reports }, null, 2));
  console.error(safe(error.message));
  process.exitCode = 1;
} finally {
  if (!cleaned && csrf) await creditsReset().catch(error => { console.error(safe(error.message)); process.exitCode = 1; });
  if (previousConfiguration?.EnableNextEpisodeAutoPlay !== undefined) await api(`/emby/Users/${config.userId}/Configuration/Partial`, {
    EnableNextEpisodeAutoPlay: previousConfiguration.EnableNextEpisodeAutoPlay,
  }).catch(error => { console.error(safe(error.message)); process.exitCode = 1; });
  await consumer.close();
  await administrator.close();
  await browser.close();
}
