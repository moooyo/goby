import { expect, test, type Page, type Route } from '@playwright/test';

const thumbnailPath = '**/emby/Items/deep-2-5/ThumbnailSet?*';
const imagePath = '**/emby/Items/deep-2-5/Images/Thumbnail?*';
const historyPath = '**/emby/Users/fixture-viewer/Items?*';
const artwork = '<svg xmlns="http://www.w3.org/2000/svg" width="400" height="225"><rect width="400" height="225" fill="#487392"/></svg>';
const tag = (version: string) => `goby-preview-400-${version.repeat(64)}`;
const frames = (version = 'a') => ({ AspectRatio: 16 / 9, Thumbnails: [0, 100_000_000, 200_000_000].map(PositionTicks => ({ PositionTicks, ImageTag: tag(version) })) });

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function openEpisode(page: Page) {
  await page.goto('/#/player/deep-2-5');
  await page.waitForFunction(() => {
    const video = document.querySelector<HTMLVideoElement>('.player-video');
    return video && video.readyState >= 2 && !video.paused;
  });
  await page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.pause());
}

async function hoverProgress(page: Page, fraction: number) {
  const progress = page.getByRole('slider', { name: '播放进度' });
  const bounds = await progress.boundingBox();
  expect(bounds).not.toBeNull();
  await progress.evaluate(element => {
    element.addEventListener('pointermove', event => {
      const bounds = element.getBoundingClientRect();
      element.setAttribute('data-test-hover-fraction', String(((event as PointerEvent).clientX - bounds.left) / bounds.width));
    }, { once: true });
  });
  await page.mouse.move(bounds!.x + bounds!.width * fraction, bounds!.y + bounds!.height / 2);
  return Number(await progress.getAttribute('data-test-hover-fraction'));
}

async function hoverTime(page: Page, fraction: number) {
  const seconds = await page.locator('.player-video').evaluate((video: HTMLVideoElement, value) => Math.floor(video.duration * value), fraction);
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor(seconds / 60) % 60;
  return `${hours ? `${hours}:${String(minutes).padStart(2, '0')}` : minutes}:${String(seconds % 60).padStart(2, '0')}`;
}

async function expectFrameAt(page: Page, fraction: number) {
  const position = await page.locator('.player-video').evaluate((video: HTMLVideoElement, value) => video.duration * value * 10_000_000, fraction);
  const expected = frames().Thumbnails.filter(frame => frame.PositionTicks <= position).at(-1);
  expect(expected).toBeDefined();
  await expect(page.locator('.player-seek-frame img')).toHaveAttribute('src', new RegExp(`[?&]PositionTicks=${expected!.PositionTicks}(?:&|$)`));
}

async function settings(page: Page) {
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '个人中心', exact: true }).click();
  await expect(page.getByLabel('观看统计', { exact: true })).toBeVisible();
}

function episode(index: number, seriesId = 'series-one') {
  return { Id: `recent-${seriesId}-${index}`, Name: `Episode ${index}`, Type: 'Episode', SeriesId: seriesId, SeriesName: `Series ${seriesId}`, SeriesPrimaryImageTag: `poster-${seriesId}`, UserData: { LastPlayedDate: '2026-10-05T10:00:00Z' } };
}

test.beforeEach(async ({ request }) => { await request.post('/__fixture/reset'); });

test('uses existing source-bound seek frames on hover and drag without negotiating or scheduling more work', async ({ page, request }) => {
  const reads: URL[] = [];
  const imageReads: URL[] = [];
  await page.route(thumbnailPath, async route => { reads.push(new URL(route.request().url())); await route.fulfill({ json: frames() }); });
  await page.route(imagePath, async route => { imageReads.push(new URL(route.request().url())); await route.fulfill({ contentType: 'image/svg+xml', body: artwork }); });
  await signIn(page);
  await openEpisode(page);
  expect(reads).toHaveLength(0);
  const before = await (await request.get('/__fixture/state')).json();
  const middle = await hoverProgress(page, .5);
  const preview = page.locator('.player-seek-frame img');
  await expect(preview).toBeVisible();
  await expectFrameAt(page, middle);
  expect(reads).toHaveLength(1);
  expect(reads[0].searchParams.get('MediaSourceId')).toBe('deep-2-5-source');
  expect(reads[0].searchParams.get('Width')).toBe('400');
  expect(imageReads[0].searchParams.get('MediaSourceId')).toBe('deep-2-5-source');
  expect(imageReads[0].searchParams.get('tag')).toBe(tag('a'));
  await page.mouse.down();
  const dragged = await hoverProgress(page, .9);
  await expectFrameAt(page, dragged);
  await page.mouse.up();
  await page.mouse.move(100, 200);
  const earlier = await hoverProgress(page, .1);
  await expectFrameAt(page, earlier);
  expect(reads).toHaveLength(1);
  const after = await (await request.get('/__fixture/state')).json();
  const negotiations = (state: typeof after) => state.requests.filter((entry: { path: string }) => /PlaybackInfo$/.test(entry.path)).length;
  expect(negotiations(after)).toBe(negotiations(before));
  expect(after.requests.some((entry: { path: string }) => /media-analysis|task-runs/.test(entry.path))).toBe(false);
});

test('keeps a time-only preview when no derivative is available', async ({ page }) => {
  let reads = 0;
  await page.route(thumbnailPath, async route => { reads += 1; await route.fulfill({ status: 404, json: { ResponseStatus: { ErrorCode: 'not_found' } } }); });
  await signIn(page);
  await openEpisode(page);
  const middle = await hoverProgress(page, .5);
  await expect(page.locator('.player-seek-preview')).toHaveText(await hoverTime(page, middle));
  await expect(page.locator('.player-seek-frame')).toHaveCount(0);
  await page.mouse.move(100, 200);
  const later = await hoverProgress(page, .7);
  await expect(page.locator('.player-seek-preview')).toHaveText(await hoverTime(page, later));
  expect(reads).toBe(1);
});

test('does not display a delayed index from the previous playback source', async ({ page }) => {
  let first: Route | undefined;
  let negotiations = 0;
  const reads: URL[] = [];
  await page.route('**/emby/Items/deep-2-5/PlaybackInfo*', async route => {
    const response = await route.fetch();
    const result = await response.json();
    negotiations += 1;
    if (negotiations > 1) result.MediaSources[0].Id = 'replacement-source';
    await route.fulfill({ response, json: result });
  });
  await page.route(thumbnailPath, async route => {
    reads.push(new URL(route.request().url()));
    if (reads.length === 1) { first = route; return; }
    await route.fulfill({ json: frames('b') });
  });
  await page.route(imagePath, route => route.fulfill({ contentType: 'image/svg+xml', body: artwork }));
  await signIn(page);
  await openEpisode(page);
  await hoverProgress(page, .5);
  await expect.poll(() => reads.length).toBe(1);
  await page.getByRole('button', { name: '音轨', exact: true }).click();
  await page.getByRole('option', { name: '国语 · AC3 5.1' }).click();
  await expect.poll(() => negotiations).toBe(2);
  await page.waitForFunction(() => {
    const video = document.querySelector<HTMLVideoElement>('.player-video');
    return video && video.readyState >= 2 && !video.paused;
  });
  await page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.pause());
  await hoverProgress(page, .5);
  const preview = page.locator('.player-seek-frame img');
  await expect(preview).toHaveAttribute('src', /MediaSourceId=replacement-source/);
  await expect(preview).toHaveAttribute('src', new RegExp(tag('b')));
  await first!.fulfill({ json: frames('a') }).catch(() => {});
  await expect(preview).toHaveAttribute('src', new RegExp(tag('b')));
  expect(reads.at(-1)!.searchParams.get('MediaSourceId')).toBe('replacement-source');
});

test('reads further history pages until six distinct works are present', async ({ page }) => {
  const history = [...Array.from({ length: 60 }, (_, index) => episode(index)), ...Array.from({ length: 5 }, (_, index) => ({ Id: `movie-${index}`, Name: `Movie ${index}`, Type: 'Movie', UserData: { LastPlayedDate: '2026-10-04T10:00:00Z' } }))];
  const offsets: number[] = [];
  await page.route(historyPath, async route => {
    const query = new URL(route.request().url()).searchParams;
    if (!query.get('SortBy')?.startsWith('DatePlayed')) { await route.continue(); return; }
    const offset = Number(query.get('StartIndex'));
    offsets.push(offset);
    await route.fulfill({ json: { Items: history.slice(offset, offset + Number(query.get('Limit'))), TotalRecordCount: history.length } });
  });
  await signIn(page);
  await settings(page);
  const cards = page.locator('.settings-recent-card');
  await expect(cards).toHaveCount(6);
  await expect(cards.locator(':scope > span')).toHaveText(['Series series-one', 'Movie 0', 'Movie 1', 'Movie 2', 'Movie 3', 'Movie 4']);
  expect(offsets).toEqual([0, 48]);
  await cards.first().click();
  await expect(page).toHaveURL(/#\/detail\/series-one$/);
});

test('bounds history work and ignores an abandoned page response', async ({ page }) => {
  let first: Route | undefined;
  let reads = 0;
  await page.route(historyPath, async route => {
    const query = new URL(route.request().url()).searchParams;
    if (!query.get('SortBy')?.startsWith('DatePlayed')) { await route.continue(); return; }
    reads += 1;
    if (reads === 1) { first = route; return; }
    const offset = Number(query.get('StartIndex'));
    await route.fulfill({ json: { Items: Array.from({ length: 48 }, (_, index) => episode(offset + index, 'current-series')), TotalRecordCount: 100_000 } });
  });
  await signIn(page);
  await settings(page);
  await expect.poll(() => reads).toBe(1);
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '首页', exact: true }).click();
  await settings(page);
  await expect(page.locator('.settings-recent-card')).toHaveCount(1);
  await expect(page.locator('.settings-recent-card')).toContainText('Series current-series');
  expect(reads).toBe(13);
  await first!.fulfill({ json: { Items: [episode(0, 'old-series')], TotalRecordCount: 1 } }).catch(() => {});
  await expect(page.locator('.settings-recent-card')).toContainText('Series current-series');
});
