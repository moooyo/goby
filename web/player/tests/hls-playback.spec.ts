import { expect, test, type Page, type Route } from '@playwright/test';
import { resolve } from 'node:path';
import type { MediaItem, PlaybackInfo } from '../src/types';

const ticks = 10_000_000;
const assets = resolve(import.meta.dirname, 'assets/hls');
const master = '/__fixture/growing/master.m3u8';
const playlist = '/__fixture/growing/main.m3u8';
const segment = (index: number) => `/__fixture/playback-0${index}.ts`;
const subtitlePlaylist = '/__fixture/growing/subtitles-0.m3u8';
const subtitleFragment = (index: number) => `/__fixture/growing/subtitles-0-${index}.vtt`;
const directSubtitle = '/emby/Videos/guitu/guitu-source/Subtitles/3/Stream.vtt';
const playlistContentType = 'application/vnd.apple.mpegurl';

test.beforeEach(async ({ request }) => { await request.post('/__fixture/reset'); });

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

function mediaPlaylist(urls: string[], growing = false) {
  const lines = ['#EXTM3U', '#EXT-X-VERSION:3', '#EXT-X-TARGETDURATION:3', '#EXT-X-MEDIA-SEQUENCE:0', `#EXT-X-PLAYLIST-TYPE:${growing ? 'EVENT' : 'VOD'}`];
  for (const url of urls) lines.push('#EXTINF:3.000,', url);
  if (urls.length === 3) lines.push('#EXT-X-ENDLIST');
  return `${lines.join('\n')}\n`;
}

async function playbackFixture(page: Page, mode: 'event' | 'hls' | 'direct') {
  const negotiations: number[] = [];
  const selections: number[] = [];
  const segments: string[] = [];
  const prefixes: number[] = [];
  let available = mode === 'event' ? 1 : 3;
  if (mode !== 'event') {
    await page.route(/\/emby\/Users\/[^/]+\/Items\/guitu\?/, async route => {
      const response = await route.fetch();
      const item = await response.json() as MediaItem;
      item.UserData!.PlaybackPositionTicks = 0;
      await route.fulfill({ response, json: item });
    });
  }
  await page.route('**/emby/Items/guitu/PlaybackInfo*', async route => {
    const request = route.request().postDataJSON();
    const result = await (await route.fetch()).json() as PlaybackInfo;
    const source = result.MediaSources[0];
    negotiations.push(request.StartTimeTicks / ticks);
    selections.push(request.SubtitleStreamIndex);
    if (mode !== 'direct') Object.assign(source, { SupportsDirectPlay: false, SupportsDirectStream: false, SupportsTranscoding: true, TranscodingUrl: `${master}?SubtitleStreamIndex=${request.SubtitleStreamIndex}`, TranscodingSubProtocol: 'hls', RunTimeTicks: 9 * ticks });
    if (mode !== 'event') Object.assign(source.MediaStreams!.find(stream => stream.Index === 3)!, { Codec: 'subrip', IsExternal: false });
    await route.fulfill({ json: result });
  });
  if (mode !== 'direct') {
    await page.route(`**${master}*`, route => {
      const selected = new URL(route.request().url()).searchParams.get('SubtitleStreamIndex') === '3' ? 'YES' : 'NO';
      const tracks = mode === 'hls' ? `#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="简体中文 [3]",LANGUAGE="chi",DEFAULT=${selected},AUTOSELECT=${selected},URI="${subtitlePlaylist}"\n` : '';
      return route.fulfill({ contentType: playlistContentType, body: `#EXTM3U\n${tracks}#EXT-X-STREAM-INF:BANDWIDTH=400000,RESOLUTION=320x180${tracks ? ',SUBTITLES="subs"' : ''}\n${playlist}\n` });
    });
    await page.route(`**${playlist}*`, route => {
      prefixes.push(available);
      return route.fulfill({ contentType: playlistContentType, body: mediaPlaylist(Array.from({ length: available }, (_, index) => segment(index)), mode === 'event') });
    });
    await page.route(/\/__fixture\/playback-0[0-2]\.ts$/, async route => {
      const pathname = new URL(route.request().url()).pathname;
      segments.push(pathname);
      await route.fulfill({ contentType: 'video/mp2t', path: resolve(assets, pathname.split('/').at(-1)!) });
    });
    if (mode === 'hls') await page.route(`**${subtitlePlaylist}*`, route => route.fulfill({ contentType: playlistContentType, body: mediaPlaylist([0, 1, 2].map(subtitleFragment)) }));
  }
  return { negotiations, selections, segments, prefixes, finish: () => { available = 3; } };
}

const videoState = (page: Page) => page.locator('.player-video').evaluate((video: HTMLVideoElement & { webkitAudioDecodedByteCount: number }) => ({
  time: video.currentTime, paused: video.paused, ready: video.readyState, frames: video.getVideoPlaybackQuality().totalVideoFrames,
  audioBytes: video.webkitAudioDecodedByteCount, src: video.currentSrc,
}));

test('waits for the saved position in a growing HLS playlist before showing or reporting playback', async ({ page, request }) => {
  const fixture = await playbackFixture(page, 'event');
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
  const fixture = await playbackFixture(page, 'event');
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

async function nativeTrackFixture(page: Page, model?: { resumeAt: number; dynamic?: boolean }) {
  const selections: number[] = [];
  const negotiations: { id: string; seconds: number }[] = [];
  await page.addInitScript(() => {
    // Chromium exercises real media and TextTrack events, not native HLS networking.
    for (const name of ['MediaSource', 'ManagedMediaSource', 'WebKitMediaSource']) Object.defineProperty(window, name, { value: undefined });
    const canPlayType = HTMLMediaElement.prototype.canPlayType;
    HTMLMediaElement.prototype.canPlayType = function (type) {
      return type === 'application/vnd.apple.mpegurl' ? 'maybe' : canPlayType.call(this, type);
    };
  });
  await page.route(/\/emby\/Users\/[^/]+\/Items\/guitu\?/, async route => {
    const response = await route.fetch();
    const item = await response.json() as MediaItem;
    item.UserData!.PlaybackPositionTicks = (model?.resumeAt ?? 0) * ticks;
    await route.fulfill({ response, json: item });
  });
  await page.route('**/emby/Items/guitu/PlaybackInfo*', async route => {
    const request = route.request().postDataJSON();
    const result = await (await route.fetch()).json() as PlaybackInfo;
    const source = result.MediaSources[0];
    const first = source.MediaStreams!.find(stream => stream.Index === 3)!;
    Object.assign(first, { Codec: 'subrip', IsExternal: false });
    source.MediaStreams!.push({ ...first, Index: 13 });
    selections.push(request.SubtitleStreamIndex);
    Object.assign(source, {
      SupportsDirectPlay: false, SupportsDirectStream: false, SupportsTranscoding: true,
      TranscodingUrl: source.DirectStreamUrl,
      TranscodingSubProtocol: 'hls',
    });
    if (model) {
      result.PlaySessionId = `native-model-${selections.length}`;
      source.TranscodingUrl += `&nativeModel=${selections.length}`;
      if (model.dynamic) source.RequiresClosing = true;
    }
    negotiations.push({ id: result.PlaySessionId, seconds: request.StartTimeTicks / ticks });
    await route.fulfill({ json: result });
  });
  return { selections, negotiations };
}

const nativeModes = (page: Page) => page.locator('.player-video').evaluate((video: HTMLVideoElement) =>
  Array.from(video.textTracks, track => track.mode));

function addNativeTrack(page: Page, index: number, text: string, initialMode: TextTrackMode = 'disabled') {
  return page.locator('.player-video').evaluate((video: HTMLVideoElement, track) => {
    const created = video.addTextTrack('subtitles', `Same language [${track.index}]`, 'chi');
    created.addCue(new VTTCue(0, 12, track.text));
    created.addCue(new VTTCue(19, 28, `${track.text} after seek`));
    created.mode = track.initialMode;
  }, { index, text, initialMode });
}

test('native HLS reads only the selected source index and refreshes captions during a paused seek', async ({ page }) => {
  const { selections } = await nativeTrackFixture(page);
  await signIn(page);
  await page.goto('/#/player/guitu');
  await expect.poll(async () => (await videoState(page)).frames).toBeGreaterThan(0);
  expect(selections).toEqual([3]);
  await addNativeTrack(page, 13, 'Wrong track', 'showing');
  await expect.poll(() => nativeModes(page)).toEqual(['disabled']);
  await expect(page.locator('.player-subtitles')).toHaveCount(0);
  await addNativeTrack(page, 3, 'Selected track');
  await expect.poll(() => nativeModes(page)).toEqual(['disabled', 'hidden']);
  await expect(page.locator('.player-subtitles')).toHaveText('Selected track');
  await page.locator('.player-video').evaluate((video: HTMLVideoElement) => { video.textTracks[0].mode = 'showing'; });
  await expect.poll(() => nativeModes(page)).toEqual(['disabled', 'hidden']);
  await expect(page.locator('.player-subtitles')).toHaveText('Selected track');

  await page.mouse.move(650, 810);
  await page.locator('.player-toggle').click();
  await expect.poll(async () => (await videoState(page)).paused).toBe(true);
  const progress = page.getByRole('slider', { name: '播放进度' });
  const bounds = (await progress.boundingBox())!;
  await progress.click({ position: { x: bounds.width * 21 / 30, y: 8 } });
  await expect.poll(async () => (await videoState(page)).time).toBeCloseTo(21, 0);
  await expect(page.locator('.player-subtitles')).toHaveText('Selected track after seek');
  expect((await videoState(page)).paused).toBe(true);
  expect(selections).toEqual([3]);

  await page.getByRole('button', { name: '字幕', exact: true }).click();
  await page.getByRole('option').filter({ hasText: '#13' }).click();
  await expect.poll(() => selections).toEqual([3, 13]);
  await expect(page.locator('.player-subtitles')).toHaveCount(0);
  // Programmatic tracks survive load(), unlike the active rendition. Old events
  // must not activate a stale track even if it matches the new selection.
  await page.locator('.player-video').evaluate((video: HTMLVideoElement) => {
    video.textTracks[0].dispatchEvent(new Event('cuechange'));
    video.textTracks.dispatchEvent(new Event('change'));
  });
  await expect.poll(() => nativeModes(page)).toEqual(['disabled', 'disabled']);
  await addNativeTrack(page, 13, 'New track');
  await expect.poll(() => nativeModes(page)).toEqual(['disabled', 'disabled', 'hidden']);
  await expect(page.locator('.player-subtitles')).toHaveText('New track after seek');

  await page.mouse.move(650, 810);
  await page.getByRole('button', { name: '字幕', exact: true }).click();
  await page.getByRole('option', { name: '关闭', exact: true }).click();
  await expect.poll(() => selections).toEqual([3, 13, -1]);
  await addNativeTrack(page, 3, 'Late caption', 'showing');
  await expect.poll(() => nativeModes(page)).toEqual(['disabled', 'disabled', 'disabled', 'disabled']);
  await expect(page.locator('.player-subtitles')).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(/#\/detail\/guitu$/);
});

test('native HLS disables ambiguous subtitle matches instead of choosing one', async ({ page }) => {
  await nativeTrackFixture(page);
  await signIn(page);
  await page.goto('/#/player/guitu');
  await expect.poll(async () => (await videoState(page)).frames).toBeGreaterThan(0);
  await addNativeTrack(page, 3, 'First match');
  await expect(page.locator('.player-subtitles')).toHaveText('First match');
  await addNativeTrack(page, 3, 'Ambiguous match', 'showing');
  await expect.poll(() => nativeModes(page)).toEqual(['disabled', 'disabled']);
  await expect(page.locator('.player-subtitles')).toHaveCount(0);
});

type NativeClock = {
  time: number;
  paused: boolean;
  seeking: boolean;
  ready: number;
  ranges: [number, number][];
};
type NativeObservation = { clock: NativeClock; seeks: number[]; plays: number[] };
type NativeModelWindow = typeof window & {
  nativeModel: {
    current: () => NativeObservation | undefined;
    amend: (clock: Partial<NativeClock>, events: string[]) => void;
  };
};

async function nativePositionFixture(page: Page, resumeAt = 0, dynamic = false) {
  const fixture = await nativeTrackFixture(page, { resumeAt, dynamic });
  await page.addInitScript(() => {
    // Model Safari's short initial seekable window separately from decoding and
    // seek completion. The native track tests above still use real video frames.
    const media = HTMLMediaElement.prototype;
    let nextSource: string | undefined;
    let current: (NativeObservation & { element: HTMLMediaElement; source: string }) | undefined;
    const managed = new WeakSet<HTMLMediaElement>();
    const model = (element: HTMLMediaElement) => current?.element === element ? current : undefined;
    const fire = (element: HTMLMediaElement, events: string[]) => {
      for (const event of events) element.dispatchEvent(new Event(event));
    };
    const source = Object.getOwnPropertyDescriptor(media, 'src')!;
    Object.defineProperty(media, 'src', { ...source, set(value: string) {
      const url = new URL(value, location.href);
      if (!url.searchParams.has('nativeModel')) { source.set!.call(this, value); return; }
      managed.add(this);
      nextSource = url.href;
    } });
    const load = media.load;
    media.load = function () {
      if (!managed.has(this)) { load.call(this); return; }
      current = nextSource ? { element: this, source: nextSource, seeks: [], plays: [], clock: {
        time: 0, paused: true, seeking: false, ready: 2, ranges: [[0, 3]],
      } } : undefined;
      nextSource = undefined;
      const opened = current;
      if (opened) queueMicrotask(() => {
        if (opened === current) fire(this, ['durationchange', 'loadedmetadata', 'loadeddata', 'timeupdate']);
      });
    };
    const getters = {
      currentSrc: (state: NonNullable<typeof current>) => state.source,
      paused: (state: NonNullable<typeof current>) => state.clock.paused,
      readyState: (state: NonNullable<typeof current>) => state.clock.ready,
      seeking: (state: NonNullable<typeof current>) => state.clock.seeking,
      currentTime: (state: NonNullable<typeof current>) => state.clock.time,
      seekable: (state: NonNullable<typeof current>) => ({ length: state.clock.ranges.length,
        start: (index: number) => state.clock.ranges[index][0], end: (index: number) => state.clock.ranges[index][1] }),
    };
    for (const [field, read] of Object.entries(getters)) {
      const descriptor = Object.getOwnPropertyDescriptor(media, field)!;
      Object.defineProperty(media, field, { ...descriptor,
        get() { const state = model(this); return state ? read(state) : descriptor.get!.call(this); },
        ...(field === 'currentTime' && { set(value: number) {
          const state = model(this);
          if (!state) { descriptor.set!.call(this, value); return; }
          state.seeks.push(value);
          // WebKit can echo an assignment while seeking; tests deliver seeked separately.
          Object.assign(state.clock, { time: value, seeking: true });
          fire(this, ['seeking']);
        } }),
      });
    }
    const play = media.play;
    media.play = function () {
      const state = model(this);
      if (!state) return play.call(this);
      state.plays.push(state.clock.time);
      state.clock.paused = false;
      queueMicrotask(() => { if (state === current && !state.clock.paused) fire(this, ['playing', 'timeupdate']); });
      return Promise.resolve();
    };
    const pause = media.pause;
    media.pause = function () {
      const state = model(this);
      if (!state) { pause.call(this); return; }
      if (!state.clock.paused) {
        state.clock.paused = true;
        queueMicrotask(() => { if (state === current) fire(this, ['pause']); });
      }
    };
    (window as NativeModelWindow).nativeModel = {
      current: () => current && { clock: current.clock, seeks: current.seeks, plays: current.plays },
      amend: (changes, events) => {
        if (!current) throw new Error('No active native media model');
        Object.assign(current.clock, changes);
        fire(current.element, events);
      },
    };
  });
  return fixture;
}

const nativeObservation = (page: Page) => page.evaluate(() => (window as NativeModelWindow).nativeModel.current());
const nativeAmend = (page: Page, clock: Partial<NativeClock>, events: string[] = []) => page.evaluate(
  ({ clock, events }) => (window as NativeModelWindow).nativeModel.amend(clock, events), { clock, events });
const nativeBuffered = (page: Page) => page.getByRole('status', { name: '正在缓冲' });

async function selectNativeSubtitle(page: Page, index: number) {
  await page.getByRole('button', { name: '字幕', exact: true }).click();
  await (index < 0 ? page.getByRole('option', { name: '关闭', exact: true }) : page.getByRole('option').filter({ hasText: `#${index}` })).click();
}

type NativeReport = { route: string; PlaySessionId: string; PositionTicks?: number; IsPaused: boolean };
async function nativeReports(request: import('@playwright/test').APIRequestContext, id: string): Promise<NativeReport[]> {
  const { events } = await (await request.get('/__fixture/state')).json() as { events: NativeReport[] };
  return events.filter(event => event.PlaySessionId === id);
}

test('native HLS waits for the real position through a track change and Off while playing', async ({ page, request }) => {
  const { selections, negotiations } = await nativePositionFixture(page);
  await signIn(page);
  await page.goto('/#/player/guitu');
  await expect.poll(async () => (await nativeObservation(page))?.plays).toEqual([0]);
  await expect.poll(async () => (await nativeReports(request, negotiations[0].id)).length).toBe(1);
  await nativeAmend(page, { time: 7 }, ['timeupdate']);
  await selectNativeSubtitle(page, 13);
  const progress = page.getByRole('slider', { name: '播放进度' });
  for (const [ordinal, track] of [[1, 13], [2, -1]] as const) {
    if (track < 0) await selectNativeSubtitle(page, track);
    await expect.poll(() => selections[ordinal]).toBe(track);
    await expect(page.locator('.is-positioning-native')).toHaveCount(1);
    await expect(nativeBuffered(page)).toBeVisible();
    const { id, seconds } = negotiations[ordinal];
    expect(seconds).toBe(7);
    await nativeAmend(page, { ranges: [[0, 3], [8, 9]] }, ['progress', 'timeupdate', 'ended']);
    expect(await nativeObservation(page)).toMatchObject({ clock: { time: 0, paused: true }, seeks: [], plays: [] });
    await expect(progress).toHaveAttribute('aria-valuenow', '7');
    expect(await nativeReports(request, id)).toEqual([]);

    // A longer seekable range can arrive with no DOM event.
    await nativeAmend(page, { ranges: [[0, 9]] });
    await expect.poll(async () => (await nativeObservation(page))?.seeks).toEqual([7]);
    await nativeAmend(page, { seeking: false }, ['progress']);
    expect((await nativeObservation(page))?.plays).toEqual([]);
    await nativeAmend(page, { ready: 1 }, ['seeked']);
    await expect(progress).toHaveAttribute('aria-valuenow', '7');
    expect(await nativeReports(request, id)).toEqual([]);
    await nativeAmend(page, { ready: 2 }, ['canplay']);
    await expect.poll(async () => (await nativeObservation(page))?.plays).toEqual([7]);
    await expect(nativeBuffered(page)).toHaveCount(0);
    await expect.poll(() => nativeReports(request, id)).toMatchObject([{ route: '/Sessions/Playing', PositionTicks: 7 * ticks, IsPaused: false }]);
  }
  expect(selections).toEqual([3, 13, -1]);
});

test('native HLS applies the latest paused target without reporting the superseded seek', async ({ page, request }) => {
  const { negotiations } = await nativePositionFixture(page);
  await signIn(page);
  await page.goto('/#/player/guitu');
  await expect.poll(async () => (await nativeObservation(page))?.plays).toEqual([0]);
  await nativeAmend(page, { time: 7, ranges: [[0, 30]] }, ['timeupdate']);
  await page.locator('.player-toggle').click();
  await expect.poll(async () => (await nativeObservation(page))?.clock.paused).toBe(true);
  await selectNativeSubtitle(page, 13);
  await expect(page.locator('.is-positioning-native')).toHaveCount(1);
  expect(negotiations[1].seconds).toBe(7);
  const id = negotiations[1].id;
  await nativeAmend(page, { ranges: [[0, 9]] }, ['progress']);
  await expect.poll(async () => (await nativeObservation(page))?.seeks).toEqual([7]);
  await page.getByRole('button', { name: '快进 10 秒（→）' }).evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect(page.getByRole('slider', { name: '播放进度' })).toHaveAttribute('aria-valuenow', '27');
  await nativeAmend(page, { ranges: [[0, 30]] }, ['progress']);
  expect((await nativeObservation(page))?.seeks).toEqual([7]);
  await nativeAmend(page, { seeking: false }, ['seeked']);
  await expect.poll(async () => (await nativeObservation(page))?.seeks).toEqual([7, 27]);
  expect(await nativeReports(request, id)).toEqual([]);
  await nativeAmend(page, { seeking: false }, ['seeked']);
  await expect(nativeBuffered(page)).toHaveCount(0);
  expect(await nativeObservation(page)).toMatchObject({ clock: { time: 27, paused: true }, plays: [] });
  await expect.poll(() => nativeReports(request, id)).toMatchObject([{ route: '/Sessions/Playing', PositionTicks: 27 * ticks, IsPaused: true }]);
  await page.locator('.player-toggle').click();
  await expect.poll(async () => (await nativeObservation(page))?.plays).toEqual([27]);
});

test('native HLS cancels obsolete position waits on replacement and exit', async ({ page, request }) => {
  await page.clock.install();
  const { negotiations } = await nativePositionFixture(page, 7);
  await signIn(page);
  await page.goto('/#/player/guitu');
  await expect(page.locator('.is-positioning-native')).toHaveCount(1);
  await nativeAmend(page, { ranges: [[0, 9]] }, ['progress']);
  await expect.poll(async () => (await nativeObservation(page))?.seeks).toEqual([7]);
  await selectNativeSubtitle(page, -1);
  await expect.poll(() => negotiations.length).toBe(2);
  await expect(page.locator('.is-positioning-native')).toHaveCount(1);
  await nativeAmend(page, {}, ['seeked', 'canplay', 'timeupdate']);
  expect(await nativeObservation(page)).toMatchObject({ clock: { time: 0, paused: true }, seeks: [], plays: [] });
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(/#\/detail\/guitu$/);
  await page.clock.fastForward(31_000);
  for (const { id } of negotiations) {
    await expect.poll(() => nativeReports(request, id)).toMatchObject([{ route: '/Sessions/Playing/Stopped' }]);
    expect((await nativeReports(request, id))[0]).not.toHaveProperty('PositionTicks');
  }
});

test('native HLS can retry an unavailable position without losing the updated pause intent', async ({ page, request }) => {
  await page.clock.install();
  const { negotiations } = await nativePositionFixture(page, 7);
  await signIn(page);
  await page.goto('/#/player/guitu');
  await expect(page.locator('.is-positioning-native')).toHaveCount(1);
  await expect(page.locator('.player-toggle')).toHaveAttribute('aria-label', '暂停');
  await page.locator('.player-toggle').click();
  await expect(page.locator('.player-toggle')).toHaveAttribute('aria-label', '播放');
  expect(await nativeObservation(page)).toMatchObject({ clock: { time: 0, paused: true }, seeks: [], plays: [] });
  await page.clock.fastForward(31_000);
  await expect(page.getByRole('alert')).toContainText('无法跳转到所选播放位置');
  await expect.poll(() => nativeReports(request, negotiations[0].id)).toMatchObject([{ route: '/Sessions/Playing/Stopped' }]);
  expect((await nativeReports(request, negotiations[0].id))[0]).not.toHaveProperty('PositionTicks');
  await page.getByRole('button', { name: '重试', exact: true }).click();
  await expect.poll(() => negotiations.map(value => value.seconds)).toEqual([7, 7]);
  await expect(page.locator('.is-positioning-native')).toHaveCount(1);
  await nativeAmend(page, { ranges: [[0, 9]] }, ['progress']);
  await expect.poll(async () => (await nativeObservation(page))?.seeks).toEqual([7]);
  await nativeAmend(page, { seeking: false }, ['seeked']);
  await expect(nativeBuffered(page)).toHaveCount(0);
  expect((await nativeObservation(page))?.plays).toEqual([]);
  await expect.poll(() => nativeReports(request, negotiations[1].id)).toMatchObject([{ route: '/Sessions/Playing', PositionTicks: 7 * ticks, IsPaused: true }]);
});

test('native HLS leaves leased dynamic sources out of the finite position wait', async ({ page }) => {
  await nativePositionFixture(page, 7, true);
  await signIn(page);
  await page.goto('/#/player/guitu');
  await expect.poll(async () => (await nativeObservation(page))?.plays).toEqual([7]);
  await expect(page.locator('.is-positioning-native')).toHaveCount(0);
});

const vttType = 'text/vtt; charset=utf-8';
const earlyCue = 'Caption while playing';
const soughtCue = 'Caption after seek';
const subtitleRoute = (mode: 'direct' | 'hls') => mode === 'hls' ? '**/__fixture/growing/subtitles-0-*.vtt' : `**${directSubtitle}?*`;

function pendingSubtitle(route: Route, status: 429 | 503, retryAfter = '2') {
  return route.fulfill({ status, headers: { 'Retry-After': retryAfter, 'Cache-Control': 'no-store', 'X-Goby-Subtitle-Preparing': '1' },
    json: { ResponseStatus: { ErrorCode: status === 429 ? 'subtitle_extraction_busy' : 'subtitle_preparing' } } });
}

for (const mode of ['hls', 'direct'] as const) {
  test(`${mode} renders delayed VTT cues while A/V plays and seeks without another PlaybackInfo`, async ({ page }) => {
    const fixture = await playbackFixture(page, mode);
    const attempts: number[] = [];
    let asyncPreference: string | undefined;
    const pending = mode === 'hls' ? 503 : 429;
    const vtt = (index: number) => index < 2
      ? `WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:139080\n\n00:00:00.000 --> 00:00:05.900\n${earlyCue}\n`
      : `WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:139080\n\n00:00:06.200 --> 00:00:08.900\n${soughtCue}\n`;
    let released = false;
    let sought = false;
    await page.route(subtitleRoute(mode), route => {
      const index = Number(new URL(route.request().url()).pathname.match(/-(\d)\.vtt$/)?.[1] ?? 0);
      asyncPreference = route.request().headers().prefer;
      attempts.push(index);
      if (!released || index === 2 && !sought) return pendingSubtitle(route, pending);
      const body = mode === 'hls' ? vtt(index) : `WEBVTT\n\n00:00:00.000 --> 00:00:12.000\n${earlyCue}\n\n00:00:20.000 --> 00:00:28.000\n${soughtCue}\n`;
      return route.fulfill({ contentType: vttType, body });
    });

    await signIn(page);
    await page.goto('/#/player/guitu');
    await expect.poll(() => attempts.length).toBeGreaterThan(0);
    expect(asyncPreference).toBe(mode === 'hls' ? 'respond-async' : undefined);
    await expect.poll(async () => (await videoState(page)).frames).toBeGreaterThan(0);
    const first = await videoState(page);
    await expect.poll(async () => (await videoState(page)).time).toBeGreaterThan(first.time + 0.3);
    expect((await videoState(page)).audioBytes).toBeGreaterThan(0);
    await expect(page.locator('.player-subtitles')).toHaveCount(0);

    released = true;
    await expect(page.locator('.player-subtitles')).toHaveText(earlyCue);
    expect(attempts.filter(index => index === 0).length).toBeGreaterThanOrEqual(2);
    expect((await videoState(page)).frames).toBeGreaterThan(first.frames);

    const target = mode === 'hls' ? 6.7 : 21;
    await page.mouse.move(650, 810);
    const progress = page.getByRole('slider', { name: '播放进度' });
    const width = (await progress.boundingBox())!.width;
    const duration = Number(await progress.getAttribute('aria-valuemax'));
    await progress.click({ position: { x: width * target / duration, y: 8 } });
    sought = true;
    await expect(page.locator('.player-subtitles')).toHaveText(soughtCue);
    const after = await videoState(page);
    expect(after.time).toBeGreaterThan(target - 0.15);
    expect(after.paused).toBe(false);
    expect(after.src).toBe(first.src);
    expect(fixture.negotiations).toEqual([0]);
    await expect(page.locator('.player-error')).toHaveCount(0);
  });
}

for (const terminal of ['unmarked 503', 'HTML 200', 'empty 204', 'login redirect'] as const) {
  test(`${terminal} ends HLS subtitle requests without restarting video`, async ({ page }) => {
    const fixture = await playbackFixture(page, 'hls');
    const login = '/__fixture/growing/subtitle-login';
    let requests = 0;
    let loginRequests = 0;
    page.on('request', request => { if (new URL(request.url()).pathname === login) loginRequests += 1; });
    await page.route(subtitleRoute('hls'), route => {
      requests += 1;
      if (terminal === 'HTML 200') return route.fulfill({ contentType: 'text/html', body: '<form>Sign in</form>' });
      if (terminal === 'empty 204') return route.fulfill({ status: 204 });
      if (terminal === 'login redirect') return route.fulfill({ status: 302, headers: { Location: login } });
      return route.fulfill({ status: 503, headers: { 'Retry-After': '2', 'Cache-Control': 'no-store' },
        json: { ResponseStatus: { ErrorCode: 'subtitle_extraction_failed' } } });
    });
    await signIn(page);
    await page.goto('/#/player/guitu');
    await expect.poll(() => requests).toBe(1);
    await expect.poll(async () => (await videoState(page)).frames).toBeGreaterThan(0);
    const first = await videoState(page);
    await expect.poll(async () => (await videoState(page)).time).toBeGreaterThan(first.time + 2.2);
    expect(requests).toBe(1);
    expect(loginRequests).toBe(0);
    expect((await videoState(page)).src).toBe(first.src);
    expect(fixture.negotiations).toEqual([0]);
    await expect(page.locator('.player-error')).toHaveCount(0);
  });
}

for (const mode of ['hls', 'direct'] as const) {
  test(`${mode} cancels pending subtitle retries when the track is turned off`, async ({ page }) => {
    const fixture = await playbackFixture(page, mode);
    const pattern = mode === 'hls' ? '**/__fixture/growing/subtitles-0-*.vtt' : `**${directSubtitle}?*`;
    let requests = 0;
    await page.route(pattern, route => {
      requests += 1;
      return pendingSubtitle(route, 503);
    });
    await signIn(page);
    await page.goto('/#/player/guitu');
    await expect.poll(() => requests).toBeGreaterThan(0);
    await expect.poll(async () => (await videoState(page)).frames).toBeGreaterThan(0);
    await page.getByRole('button', { name: '字幕', exact: true }).click();
    await page.getByRole('option', { name: '关闭', exact: true }).click();
    const previousRequests = requests;
    await expect.poll(() => fixture.selections).toEqual([3, -1]);
    await expect.poll(async () => (await videoState(page)).time).toBeGreaterThan(2.2);
    expect(requests).toBe(previousRequests);
    await expect(page.locator('.player-subtitles')).toHaveCount(0);
    await expect(page.locator('.player-error')).toHaveCount(0);
  });
}

test('clamps Retry-After and expires marked subtitle failures without disturbing playback', async ({ page }) => {
  const fixture = await playbackFixture(page, 'direct');
  await page.clock.install();
  let requests = 0;
  await page.route(subtitleRoute('direct'), route => {
    requests += 1;
    return pendingSubtitle(route, 503, '999');
  });
  await signIn(page);
  await page.goto('/#/player/guitu');
  await expect.poll(() => requests).toBe(1);
  await expect.poll(async () => (await videoState(page)).frames).toBeGreaterThan(0);
  await page.clock.fastForward(5_100);
  await expect.poll(() => requests).toBe(2);
  await page.clock.fastForward(185_000);
  const at = (await videoState(page)).time;
  await expect.poll(async () => (await videoState(page)).time).toBeGreaterThan(at + 0.3);
  expect(requests).toBe(2);
  expect(fixture.negotiations).toEqual([0]);
  await expect(page.locator('.player-error')).toHaveCount(0);
});

test('leaving playback aborts its in-flight standalone subtitle request', async ({ page }) => {
  await playbackFixture(page, 'direct');
  let release!: () => void;
  const response = new Promise<void>(resolve => { release = resolve; });
  await page.route(subtitleRoute('direct'), async route => {
    await response;
    await route.fulfill({ contentType: vttType, body: `WEBVTT\n\n00:00:00.000 --> 00:00:12.000\n${earlyCue}\n` });
  });
  await signIn(page);
  const started = page.waitForEvent('request', request => request.url().includes(directSubtitle));
  await page.goto('/#/player/guitu');
  const subtitleRequest = await started;
  await expect.poll(async () => (await videoState(page)).frames).toBeGreaterThan(0);
  const aborted = page.waitForEvent('requestfailed', request => request === subtitleRequest);
  try {
    await page.keyboard.press('Escape');
    await expect(page).toHaveURL(/#\/detail\/guitu$/);
    expect((await aborted).failure()?.errorText).toBe('net::ERR_ABORTED');
    await expect(page.locator('.player-video')).toHaveCount(0);
  } finally {
    release();
  }
});
