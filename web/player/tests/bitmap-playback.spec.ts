import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import type { MediaStream } from '../src/types';

const ticksPerSecond = 10_000_000;
const bitmapStreams: MediaStream[] = [
  { Index: 65_536, Type: 'Subtitle', Codec: 'hdmv_pgs_subtitle', Language: 'chi', DisplayTitle: 'External SUP', Title: 'External SUP', IsDefault: true },
  { Index: 2_147_483_646, Type: 'Subtitle', Codec: 'dvd_subtitle', Language: 'chi', DisplayTitle: 'External IDX Chinese', Title: 'External IDX Chinese' },
  { Index: 2_147_483_647, Type: 'Subtitle', Codec: 'dvd_subtitle', Language: 'eng', DisplayTitle: 'External IDX English', Title: 'External IDX English' },
].map(stream => ({ ...stream, Type: 'Subtitle', IsExternal: true, IsTextSubtitleStream: false, SupportsExternalStream: false }));

type PlaybackRequest = {
  SubtitleStreamIndex: number;
  StartTimeTicks: number;
  EnableTranscoding: boolean;
  DeviceProfile: { SubtitleProfiles: { Format: string; Method: string; Protocol?: string; Container?: string }[] };
};
type PlaybackEvent = { route: string; ItemId: string; PlaySessionId: string; SubtitleStreamIndex: number; PositionTicks: number; PlayMethod: string };

async function fixtureState(request: APIRequestContext) {
  return (await request.get('/__fixture/state')).json();
}

async function configureBitmapPlayback(page: Page, request: APIRequestContext, rememberedIndex = bitmapStreams[0].Index) {
  const state = await fixtureState(request);
  await request.post('/__fixture/config', { data: { user: { ...state.user, Configuration: { ...state.user.Configuration, SubtitleMode: 'Default' } } } });
  await page.route(/\/emby\/Users\/[^/]+\/Items\/guitu\?/, async route => {
    const response = await route.fetch();
    const item = await response.json();
    const source = item.MediaSources[0];
    source.MediaStreams = [...source.MediaStreams.map((stream: MediaStream) => ({ ...stream, IsDefault: stream.Type === 'Subtitle' ? false : stream.IsDefault })), ...bitmapStreams];
    source.DefaultSubtitleStreamIndex = bitmapStreams[0].Index;
    item.MediaStreams = source.MediaStreams;
    item.UserData.PlaybackPositionTicks = 0;
    await route.fulfill({ response, json: item });
  });
  await page.route(/\/emby\/DisplayPreferences\/goby-player-item-guitu\?/, async route => {
    if (route.request().method() !== 'GET') { await route.continue(); return; }
    const response = await route.fetch();
    const body = await response.json();
    if (!body.CustomPrefs.selection) body.CustomPrefs.selection = JSON.stringify({ subtitleStreamIndex: rememberedIndex });
    await route.fulfill({ response, json: body });
  });
  const negotiations: PlaybackRequest[] = [];
  await page.route(/\/emby\/Items\/guitu\/PlaybackInfo(?:\?|$)/, async route => {
    const selection = route.request().postDataJSON() as PlaybackRequest;
    negotiations.push(selection);
    const response = await route.fetch();
    const body = await response.json();
    const source = body.MediaSources[0];
    source.MediaStreams = [...source.MediaStreams, ...bitmapStreams];
    source.DefaultSubtitleStreamIndex = selection.SubtitleStreamIndex;
    const encoded = bitmapStreams.some(stream => stream.Index === selection.SubtitleStreamIndex);
    source.SupportsDirectPlay = !encoded;
    source.SupportsDirectStream = false;
    source.SupportsTranscoding = encoded;
    // This browser contract fixture uses existing playable bytes; real HLS burn-in is verified against the backend separately.
    source.TranscodingSubProtocol = undefined;
    source.TranscodingUrl = encoded ? source.DirectStreamUrl : undefined;
    if (encoded) delete source.DirectStreamUrl;
    await route.fulfill({ response, json: body });
  });
  const subtitleDownloads: string[] = [];
  page.on('request', event => { if (/\/Subtitles\/\d+\/Stream\./.test(event.url())) subtitleDownloads.push(event.url()); });
  return { negotiations, subtitleDownloads };
}

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function pauseReadyVideo(page: Page) {
  await expect.poll(() => page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.readyState >= 2 && !video.paused)).toBe(true);
  await page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.pause());
  await page.mouse.move(650, 810);
}

async function subtitleMenu(page: Page) {
  await page.mouse.move(650, 810);
  await page.getByRole('button', { name: '字幕', exact: true }).click();
  return page.getByRole('dialog', { name: '字幕', exact: true });
}

test.beforeEach(async ({ request }) => { await request.post('/__fixture/reset'); });

for (const selected of bitmapStreams) {
  test(`restores playable ${selected.DisplayTitle} in both subtitle menus without text delivery`, async ({ page, request }) => {
    const { negotiations, subtitleDownloads } = await configureBitmapPlayback(page, request, selected.Index);
    await signIn(page);
    await page.goto('/#/detail/guitu');
    const overview = page.locator('.deck-panel[data-active="true"]');
    await expect(overview).toHaveAttribute('aria-label', '概览');
    await overview.locator('button[title="选择字幕"]').click();
    for (const stream of bitmapStreams) {
      await expect(page.getByRole('menuitemradio', { name: new RegExp(`^${stream.DisplayTitle}`) })).toBeVisible();
    }
    await expect(page.getByRole('menuitemradio', { name: new RegExp(`^${selected.DisplayTitle}`) })).toHaveAttribute('aria-checked', 'true');
    await page.keyboard.press('Escape');
    await page.goto('/#/player/guitu');
    await expect.poll(() => negotiations.length).toBe(1);
    const negotiation = negotiations[0];
    expect(negotiation.SubtitleStreamIndex).toBe(selected.Index);
    expect(negotiation.EnableTranscoding).toBe(true);
    expect(negotiation.DeviceProfile.SubtitleProfiles.some(profile => profile.Format.split(',').includes(selected.Codec!) && profile.Method === 'Encode' && profile.Protocol === 'hls' && profile.Container === 'ts')).toBe(true);
    expect(negotiation.DeviceProfile.SubtitleProfiles.some(profile => profile.Format.split(',').includes(selected.Codec!) && profile.Method === 'External')).toBe(false);
    await pauseReadyVideo(page);
    const dialog = await subtitleMenu(page);
    for (const stream of bitmapStreams) {
      const option = dialog.getByRole('option', { name: new RegExp(`^${stream.DisplayTitle}`) });
      await expect(option).toBeVisible();
      await expect(option).toContainText('图形字幕 · 样式跟随片源');
    }
    await expect(dialog.getByRole('option', { name: new RegExp(`^${selected.DisplayTitle}`) })).toHaveAttribute('aria-selected', 'true');
    await expect(dialog.getByText('字幕样式跟随个人设置。')).toHaveCount(0);
    await expect(page.locator('.player-video track')).toHaveCount(0);
    expect(subtitleDownloads).toEqual([]);
    await expect.poll(async () => {
      const state = await fixtureState(request);
      return state.events.filter((event: PlaybackEvent) => event.route === '/Sessions/Playing').map((event: PlaybackEvent) => [event.SubtitleStreamIndex, event.PlayMethod]);
    }).toEqual([[selected.Index, 'Transcode']]);
  });
}

test('switches SUP and both IDX languages, then disables subtitles without losing source position', async ({ page, request }) => {
  const { negotiations, subtitleDownloads } = await configureBitmapPlayback(page, request);
  await signIn(page);
  await page.goto('/#/player/guitu');
  await pauseReadyVideo(page);
  const choices = [...bitmapStreams.slice(1), { Index: -1, DisplayTitle: '关闭' }];
  for (const [ordinal, selected] of choices.entries()) {
    const seconds = 8 + ordinal * 5;
    await page.locator('.player-video').evaluate((video: HTMLVideoElement, target: number) => { video.currentTime = target; }, seconds);
    await expect(page.getByRole('slider', { name: '播放进度' })).toHaveAttribute('aria-valuenow', String(seconds));
    const dialog = await subtitleMenu(page);
    await dialog.getByRole('option', { name: new RegExp(`^${selected.DisplayTitle}`) }).click();
    await expect.poll(() => negotiations.length).toBe(ordinal + 2);
    expect(negotiations.at(-1)?.SubtitleStreamIndex).toBe(selected.Index);
    expect(Math.abs(negotiations.at(-1)!.StartTimeTicks / ticksPerSecond - seconds)).toBeLessThan(0.5);
    await pauseReadyVideo(page);
    const position = await page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.currentTime);
    expect(position).toBeGreaterThanOrEqual(seconds - 0.5);
    expect(position).toBeLessThan(seconds + 3);
    await expect.poll(async () => {
      const state = await fixtureState(request);
      return state.events.filter((event: PlaybackEvent) => event.route === '/Sessions/Playing').length;
    }).toBe(ordinal + 2);
  }
  const state = await fixtureState(request);
  const events: PlaybackEvent[] = state.events;
  const starts = events.filter(event => event.route === '/Sessions/Playing');
  expect(starts.map(event => event.SubtitleStreamIndex)).toEqual([...bitmapStreams.map(stream => stream.Index), -1]);
  expect(starts.map(event => event.PlayMethod)).toEqual(['Transcode', 'Transcode', 'Transcode', 'DirectPlay']);
  expect(new Set(starts.map(event => event.PlaySessionId)).size).toBe(starts.length);
  for (const [ordinal, started] of starts.slice(0, -1).entries()) {
    const stoppedIndex = events.findIndex(event => event.route === '/Sessions/Playing/Stopped' && event.PlaySessionId === started.PlaySessionId);
    expect(stoppedIndex).toBeGreaterThan(events.indexOf(started));
    expect(stoppedIndex).toBeLessThan(events.indexOf(starts[ordinal + 1]));
    expect(events.slice(stoppedIndex + 1).some(event => event.PlaySessionId === started.PlaySessionId)).toBe(false);
    expect(events.filter(event => event.PlaySessionId === started.PlaySessionId).every(event => event.SubtitleStreamIndex === started.SubtitleStreamIndex)).toBe(true);
  }
  await expect(page.locator('.player-video track')).toHaveCount(0);
  expect(subtitleDownloads).toEqual([]);
  await expect.poll(async () => JSON.parse((await fixtureState(request)).display['goby-player-item-guitu'].CustomPrefs.selection).subtitleStreamIndex).toBe(-1);
});
