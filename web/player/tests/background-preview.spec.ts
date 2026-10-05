import { expect, test, type Page } from '@playwright/test';

const emptyThemes = { ThemeVideosResult: { OwnerId: '', Items: [], TotalRecordCount: 0 }, ThemeSongsResult: { OwnerId: '', Items: [], TotalRecordCount: 0 }, SoundtrackSongsResult: { OwnerId: '', Items: [], TotalRecordCount: 0 } };
const source = (id: string) => ({ Id: id, Name: id, Type: 'Video', MediaSources: [{ Id: `${id}-source`, Container: 'webm', SupportsDirectPlay: true, Bitrate: 300_000, MediaStreams: [{ Index: 0, Type: 'Video', Codec: 'vp8', Width: 320, Height: 180, BitDepth: 8, VideoRangeType: 'SDR' }] }] });
const withTheme = (id: string) => ({ ...emptyThemes, ThemeVideosResult: { OwnerId: 'deep', Items: [source(id)], TotalRecordCount: 1 } });
const defaultOrder = ['theme', 'generated', 'trailer'];

async function signIn(page: Page, username = 'reviewer') {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill(username);
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function settings(page: Page) {
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '个人中心', exact: true }).click();
  await expect(page.getByRole('list', { name: '背景素材优先级' })).toBeVisible();
}

async function order(page: Page) {
  return page.locator('.settings-background-order li[data-source]').evaluateAll(elements => elements.map(element => element.getAttribute('data-source')));
}

async function mockGenerated(page: Page, reads: string[], available = true) {
  await page.route('**/emby/Items/*/BackgroundPreview?*', async route => {
    const url = new URL(route.request().url());
    expect(route.request().method()).toBe('GET');
    expect(url.searchParams.get('UserId')).toBe('fixture-viewer');
    reads.push(url.pathname);
    await route.fulfill({ json: available ? { Available: true, StreamUrl: '/emby/Items/deep/BackgroundPreview/stream.mp4', RunTimeTicks: 30_000_000, Width: 320, Height: 180 } : { Available: false } });
  });
  await page.route('**/emby/Items/deep/BackgroundPreview/stream.mp4?*', async route => {
    expect(new URL(route.request().url()).searchParams.get('api_key')).toBe('isolated-player-acceptance-token');
    const response = await route.fetch({ url: new URL('/__fixture/background.mp4', route.request().url()).href });
    await route.fulfill({ response });
  });
}

test.beforeEach(async ({ request }) => { await request.post('/__fixture/reset'); });

test('uses the default theme priority without fetching or generating lower sources', async ({ page, request }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const generated: string[] = [];
  await mockGenerated(page, generated);
  await page.route('**/emby/Items/*/ThemeMedia?*', route => route.fulfill({ json: withTheme('theme-first') }));
  await signIn(page);
  await expect(page.locator('.background-preview')).toHaveClass(/is-visible/);
  await expect(page.locator('.background-preview')).toHaveAttribute('data-preview-kind', 'theme');
  expect(generated).toEqual([]);
  const state = await (await request.get('/__fixture/state')).json();
  expect(state.events).toEqual([]);
  expect(state.requests.some((entry: { path: string }) => /LocalTrailers|PlaybackInfo|task-runs|media-analysis/.test(entry.path))).toBe(false);
});

test('persists source reordering and plays an existing generated file while generation is disabled', async ({ page, request }) => {
  const generated: string[] = [];
  const generationRequests: string[] = [];
  page.on('request', entry => { if (/\/admin\/v1\/(?:media-analysis|task-runs|tasks)(?:\/|\?|$)/.test(entry.url())) generationRequests.push(entry.url()); });
  await mockGenerated(page, generated);
  await page.route('**/emby/Items/*/ThemeMedia?*', route => route.fulfill({ json: withTheme('theme-second') }));
  await signIn(page);
  await settings(page);
  await expect.poll(() => order(page)).toEqual(defaultOrder);
  await expect(page.getByRole('button', { name: '提高主题视频优先级' })).toBeDisabled();
  await expect(page.getByRole('button', { name: '降低本地预告片优先级' })).toBeDisabled();
  const saved = page.waitForResponse(response => response.request().method() === 'POST' && response.url().includes('/DisplayPreferences/goby-player'));
  await page.getByRole('button', { name: '提高本地生成短片优先级' }).click();
  await saved;
  await expect.poll(() => order(page)).toEqual(['generated', 'theme', 'trailer']);
  await expect(page.locator('.settings-priority-status')).toContainText('本地生成短片已调整为第 1 优先');
  await page.reload();
  await expect.poll(() => order(page)).toEqual(['generated', 'theme', 'trailer']);
  await page.locator('.settings-section').filter({ has: page.getByRole('heading', { name: '界面', exact: true }) }).screenshot({ path: test.info().outputPath('background-settings-desktop.png') });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.locator('.settings-section').filter({ has: page.getByRole('heading', { name: '界面', exact: true }) }).scrollIntoViewIfNeeded();
  await page.evaluate(() => window.scrollBy(0, -72));
  await page.locator('.settings-section').filter({ has: page.getByRole('heading', { name: '界面', exact: true }) }).screenshot({ path: test.info().outputPath('background-settings-mobile.png') });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '首页', exact: true }).click();
  const video = page.locator('.background-preview');
  await expect(video).toHaveAttribute('data-preview-kind', 'generated');
  await expect(video).toHaveClass(/is-visible/);
  const playback = await video.evaluate((element: HTMLVideoElement) => ({ width: element.videoWidth, muted: element.muted, paused: element.paused }));
  expect(playback).toEqual({ width: 320, muted: true, paused: false });
  expect(generated.length).toBeGreaterThan(0);
  const state = await (await request.get('/__fixture/state')).json();
  const preferences = JSON.parse(state.display['goby-player'].CustomPrefs.preferences);
  expect(preferences.backgroundSources).toEqual(['generated', 'theme', 'trailer']);
  expect(state.events).toEqual([]);
  expect(state.requests.some((entry: { path: string }) => /PlaybackInfo|task-runs|media-analysis/.test(entry.path))).toBe(false);
  expect(generationRequests).toEqual([]);
});

test('falls through actual decode failures and does not retry failed files on return', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const generated: string[] = [];
  const attempts: string[] = [];
  await mockGenerated(page, generated);
  await page.route('**/emby/Items/*/ThemeMedia?*', route => route.fulfill({ json: withTheme('broken-theme') }));
  await page.route('**/emby/Videos/broken-theme/stream?*', async route => {
    attempts.push('theme');
    await route.fulfill({ contentType: 'video/webm', body: 'This is deliberately invalid video data.' });
  });
  await page.route('**/emby/Items/deep/BackgroundPreview/stream.mp4?*', async route => {
    attempts.push('generated');
    await route.fulfill({ contentType: 'video/mp4', body: 'This generated file is deliberately invalid.' });
  });
  await page.route('**/emby/Users/fixture-viewer/Items/*/LocalTrailers?*', route => route.fulfill({ json: [source('working-trailer')] }));
  await signIn(page);
  const video = page.locator('.background-preview');
  await expect(video).toHaveAttribute('data-preview-kind', 'trailer');
  await expect(video).toHaveClass(/is-visible/);
  expect(attempts).toEqual(['theme', 'generated']);
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '电影', exact: true }).click();
  await expect(video).not.toHaveAttribute('src');
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '首页', exact: true }).click();
  await expect(video).toHaveAttribute('data-preview-kind', 'trailer');
  await expect(video).toHaveClass(/is-visible/);
  expect(attempts).toEqual(['theme', 'generated']);
});

test('falls through failed metadata requests and an unavailable generated file', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const generated: string[] = [];
  await mockGenerated(page, generated, false);
  await page.route('**/emby/Items/*/ThemeMedia?*', route => route.fulfill({ status: 503, json: { ResponseStatus: { ErrorCode: 'unavailable' } } }));
  await page.route('**/emby/Users/fixture-viewer/Items/*/LocalTrailers?*', route => route.fulfill({ json: [source('available-trailer')] }));
  await signIn(page);
  await expect(page.locator('.background-preview')).toHaveAttribute('data-preview-kind', 'trailer');
  await expect(page.locator('.background-preview')).toHaveClass(/is-visible/);
  expect(generated.length).toBeGreaterThan(0);
});

test('keeps a still-only preference across reload and never reads video sources', async ({ page }) => {
  let reads = 0;
  await page.route(/\/emby\/(?:Items\/[^/]+\/(?:ThemeMedia|BackgroundPreview)|Users\/[^/]+\/Items\/[^/]+\/LocalTrailers)\?/, async route => { reads++; await route.continue(); });
  await signIn(page);
  await settings(page);
  const saved = page.waitForResponse(response => response.request().method() === 'POST' && response.url().includes('/DisplayPreferences/goby-player'));
  await page.getByRole('switch', { name: /^动态背景/ }).click();
  await saved;
  await expect(page.getByRole('switch', { name: /^动态背景/ })).toHaveAttribute('aria-checked', 'false');
  await page.reload();
  await expect(page.getByRole('switch', { name: /^动态背景/ })).toHaveAttribute('aria-checked', 'false');
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '首页', exact: true }).click();
  await page.waitForTimeout(1400);
  expect(reads).toBe(0);
  await expect(page.locator('.background-preview')).not.toHaveAttribute('src');
});

test('retires a delayed generated lookup when its route leaves the background stage', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  let finish: (() => void) | undefined;
  let lookupStarted: () => void = () => undefined;
  const started = new Promise<void>(resolve => { lookupStarted = resolve; });
  await page.route('**/emby/Items/*/BackgroundPreview?*', async route => {
    lookupStarted();
    await new Promise<void>(done => { finish = done; });
    await route.fulfill({ json: { Available: true, StreamUrl: '/emby/Items/deep/BackgroundPreview/stream.mp4' } }).catch(() => undefined);
  });
  let streams = 0;
  await page.route('**/emby/Items/*/BackgroundPreview/stream.mp4?*', async route => { streams++; await route.abort(); });
  await signIn(page);
  await started;
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '电影', exact: true }).click();
  finish?.();
  await expect(page.locator('.catalog-title,.catalog-libraries')).toBeVisible();
  await page.waitForTimeout(900);
  expect(streams).toBe(0);
  await expect(page.locator('.background-preview')).not.toHaveAttribute('src');
});

for (const [name, preferences] of [
  ['older preferences', { quality: 'original', heroRotate: false }],
  ['duplicated source entries', { backgroundSources: ['theme', 'theme', 'trailer'], backgroundMotion: 'yes' }],
  ['unknown source entries', { backgroundSources: ['theme', 'network', 'trailer'] }],
] as const) {
  test(`restores the default source order from ${name}`, async ({ page, request }) => {
    await request.post('/__fixture/config', { data: { display: { 'goby-player': { Revision: '1', CustomPrefs: { preferences: JSON.stringify(preferences) } } } } });
    await signIn(page);
    await settings(page);
    await expect.poll(() => order(page)).toEqual(defaultOrder);
    await expect(page.getByRole('switch', { name: /^动态背景/ })).toHaveAttribute('aria-checked', 'true');
  });
}

test('keeps custom source priorities isolated between signed-in users', async ({ page, request }) => {
  const state = await (await request.get('/__fixture/state')).json();
  const secondUser = { ...state.user, Id: 'fixture-other', Name: 'other' };
  const displays = new Map<string, { Revision: string; CustomPrefs: Record<string, string> }>();
  await page.route('**/emby/DisplayPreferences/*?*', async route => {
    const user = new URL(route.request().url()).searchParams.get('UserId')!;
    const value = displays.get(user) ?? { Revision: '1', CustomPrefs: {} };
    if (route.request().method() === 'POST') {
      value.CustomPrefs = route.request().postDataJSON().CustomPrefs;
      value.Revision = String(Number(value.Revision) + 1);
      displays.set(user, value);
      await route.fulfill({ status: 200, body: '' });
    } else await route.fulfill({ json: value });
  });
  await page.route('**/emby/Users/AuthenticateByName', async route => {
    if (route.request().postDataJSON().Username !== 'other') return route.continue();
    await route.fulfill({ json: { AccessToken: 'isolated-player-acceptance-token', User: secondUser, ServerId: 'fixture-server' } });
  });
  await page.route(/\/emby\/Users\/fixture-other(?:\/|\?|$)/, async route => {
    const url = new URL(route.request().url());
    if (url.pathname === '/emby/Users/fixture-other') return route.fulfill({ json: secondUser });
    const response = await route.fetch({ url: url.href.replace('/Users/fixture-other', '/Users/fixture-viewer') });
    await route.fulfill({ response });
  });
  await signIn(page);
  await settings(page);
  const saved = page.waitForResponse(response => response.request().method() === 'POST' && response.url().includes('/DisplayPreferences/goby-player'));
  await page.getByRole('button', { name: '提高本地生成短片优先级' }).click();
  await saved;
  await page.getByRole('button', { name: '退出登录', exact: true }).click();
  await expect(page.getByLabel('用户名', { exact: true })).toBeVisible();
  await signIn(page, 'other');
  await settings(page);
  await expect.poll(() => order(page)).toEqual(defaultOrder);
  await page.getByRole('button', { name: '退出登录', exact: true }).click();
  await expect(page.getByLabel('用户名', { exact: true })).toBeVisible();
  await signIn(page);
  await settings(page);
  await expect.poll(() => order(page)).toEqual(['generated', 'theme', 'trailer']);
});
