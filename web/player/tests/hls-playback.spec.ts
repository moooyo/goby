import { expect, test, type Page } from '@playwright/test';
import { resolve } from 'node:path';
import type { PlaybackInfo } from '../src/types';

const ticks = 10_000_000;
const assets = resolve(import.meta.dirname, 'assets/hls');
const master = '/__fixture/growing/master.m3u8';
const playlist = '/__fixture/growing/main.m3u8';
const segment = (index: number) => `/__fixture/playback-0${index}.ts`;

test.beforeEach(async ({ request }) => { await request.post('/__fixture/reset'); });

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function growingHls(page: Page) {
  const negotiations: number[] = [];
  const segments: string[] = [];
  const prefixes: number[] = [];
  let available = 1;
  await page.route('**/emby/Items/guitu/PlaybackInfo*', async route => {
    const request = route.request().postDataJSON();
    const result = await (await route.fetch()).json() as PlaybackInfo;
    const source = result.MediaSources[0];
    negotiations.push(request.StartTimeTicks / ticks);
    Object.assign(source, { SupportsDirectPlay: false, SupportsDirectStream: false, SupportsTranscoding: true, TranscodingUrl: master, TranscodingSubProtocol: 'hls', RunTimeTicks: 9 * ticks });
    await route.fulfill({ json: result });
  });
  await page.route(`**${master}*`, route => route.fulfill({ contentType: 'application/vnd.apple.mpegurl', body: `#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=400000,RESOLUTION=320x180\n${playlist}\n` }));
  await page.route(`**${playlist}*`, async route => {
    prefixes.push(available);
    const body = ['#EXTM3U', '#EXT-X-VERSION:3', '#EXT-X-TARGETDURATION:3', '#EXT-X-MEDIA-SEQUENCE:0', '#EXT-X-PLAYLIST-TYPE:EVENT'];
    for (let index = 0; index < available; index += 1) body.push('#EXTINF:3.000,', segment(index));
    if (available === 3) body.push('#EXT-X-ENDLIST');
    await route.fulfill({ contentType: 'application/vnd.apple.mpegurl', body: `${body.join('\n')}\n` });
  });
  await page.route(/\/__fixture\/playback-0[0-2]\.ts$/, async route => {
    const pathname = new URL(route.request().url()).pathname;
    segments.push(pathname);
    await route.fulfill({ contentType: 'video/mp2t', path: resolve(assets, pathname.split('/').at(-1)!) });
  });
  return { negotiations, segments, prefixes, finish: () => { available = 3; } };
}

const videoState = (page: Page) => page.locator('.player-video').evaluate((video: HTMLVideoElement) => ({
  time: video.currentTime, paused: video.paused, ready: video.readyState, frames: video.getVideoPlaybackQuality().totalVideoFrames,
}));

test('waits for the saved position in a growing HLS playlist before showing or reporting playback', async ({ page, request }) => {
  const fixture = await growingHls(page);
  await signIn(page);
  await page.goto('/#/player/guitu');
  await expect.poll(() => fixture.prefixes.length).toBeGreaterThanOrEqual(2);
  expect(fixture.negotiations).toEqual([8]);
  expect(fixture.segments).toEqual([]);
  await expect(page.getByRole('status', { name: '正在缓冲' })).toBeVisible();
  expect((await (await request.get('/__fixture/state')).json()).events).toEqual([]);
  fixture.finish();
  await expect.poll(async () => (await videoState(page)).time).toBeGreaterThanOrEqual(7.9);
  await expect.poll(async () => (await videoState(page)).frames).toBeGreaterThan(0);
  expect(fixture.segments[0]).toBe(segment(2));
  await expect.poll(async () => (await (await request.get('/__fixture/state')).json()).events.find((event: { route: string }) => event.route === '/Sessions/Playing')?.PositionTicks).toBeGreaterThanOrEqual(79_000_000);
});

test('keeps a paused drag target beyond the current HLS prefix and opens the correct frame', async ({ page }) => {
  const fixture = await growingHls(page);
  await signIn(page);
  await page.goto('/#/detail/guitu');
  await page.locator('.deck-panel[data-active="true"] .restart-button').click();
  await expect.poll(async () => (await videoState(page)).frames).toBeGreaterThan(0);
  await page.locator('.player-toggle').click();
  await expect.poll(async () => (await videoState(page)).paused).toBe(true);
  const bounds = (await page.getByRole('slider', { name: '播放进度' }).boundingBox())!;
  const y = bounds.y + bounds.height / 2;
  await page.mouse.move(bounds.x + bounds.width * .1, y);
  await page.mouse.down();
  await page.mouse.move(bounds.x + bounds.width * 7 / 9, y, { steps: 6 });
  await page.mouse.up();
  await expect.poll(() => fixture.negotiations.length).toBe(2);
  expect(fixture.negotiations[1]).toBeCloseTo(7, 1);
  await expect(page.getByRole('slider', { name: '播放进度' })).toHaveAttribute('aria-valuenow', '7');
  fixture.finish();
  await expect.poll(async () => (await videoState(page)).ready).toBeGreaterThanOrEqual(2);
  const player = await videoState(page);
  expect(player.time).toBeCloseTo(7, 1);
  expect(player.paused).toBe(true);
  await expect(page.getByRole('status', { name: '正在缓冲' })).toHaveCount(0);
});
