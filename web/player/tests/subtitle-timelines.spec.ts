import { expect, test, type Page, type Request } from '@playwright/test';
import type { MediaStream } from '../src/types';

const durationTicks = 10_000_000_000;
const activePanel = (page: Page) => page.locator('.deck-panel[data-active="true"]');
const descriptorRoute = /\/emby\/Items\/[^/]+\/SubtitleTimelines\?/;
const timelineRoute = /\/emby\/Items\/[^/]+\/SubtitleTimelines\/\d+\?/;
const textRoute = /\/Subtitles\/(\d+)\/(?:0\/)?Stream\.vtt\?/;
const textTrack = (Index: number): MediaStream => ({ Index, Type: 'Subtitle', Codec: 'srt', IsExternal: false, IsTextSubtitleStream: true, DeliveryUrl: `/emby/Videos/guitu/guitu-source/Subtitles/${Index}/0/Stream.srt` });
const readyText = 'WEBVTT\n\n00:01:40.000 --> 00:03:20.000\nA cue\n';
const pendingSubtitle = { 'X-Goby-Subtitle-Preparing': '1', 'Retry-After': '5' };
const pendingReads = new WeakMap<Page, Set<Request>>();
const bitmap = (Index: number, Codec = 'hdmv_pgs_subtitle', IsExternal = false): MediaStream => ({ Index, Type: 'Subtitle', Codec, IsExternal, IsTextSubtitleStream: false, DisplayTitle: Index === 8 ? '中文 PGS' : Index === 12 ? '英文 DVD' : `Bitmap ${Index}`, IsDefault: Index === 8 });
const bitmapStreams = [bitmap(8), bitmap(12, 'dvd_subtitle'), bitmap(18, 'hdmv_pgs_subtitle', true)];
const externalTimelines: MediaStream[] = [
  { ...bitmap(65_536, 'hdmv_pgs_subtitle', true), DisplayTitle: 'External SUP', Language: 'chi', IsDefault: true },
  { ...bitmap(2_147_483_646, 'dvd_subtitle', true), DisplayTitle: 'External IDX Chinese', Language: 'chi' },
  { ...bitmap(2_147_483_647, 'dvd_subtitle', true), DisplayTitle: 'External IDX English', Language: 'eng' },
].map(stream => ({ ...stream, Title: stream.DisplayTitle, GobySubtitleTimelineOnly: true, SupportsExternalStream: false }));
const externalIndices = externalTimelines.map(stream => stream.Index);

function descriptor(item = 'guitu', version = 'one', indices = [12, 8]) {
  return {
    Available: true, MediaSourceId: `${item}-source`, SourceVersion: `source-${version}`, DurationTicks: durationTicks,
    Streams: indices.map(StreamIndex => ({ StreamIndex, Codec: externalTimelines.find(stream => stream.Index === StreamIndex)?.Codec ?? (StreamIndex === 12 ? 'dvd_subtitle' : 'hdmv_pgs_subtitle'), IntervalCount: 2, Url: `/emby/Items/${item}/SubtitleTimelines/${StreamIndex}?tag=${version}` })),
  };
}

function payload(item = 'guitu', stream = 8, version = 'one') {
  return {
    MediaSourceId: `${item}-source`, SourceVersion: `source-${version}`, DurationTicks: durationTicks, StreamIndex: stream,
    Intervals: stream === 8 ? [{ StartTicks: 1_000_000_000, EndTicks: 2_000_000_000 }, { StartTicks: 3_500_000_000, EndTicks: 4_000_000_000 }]
      : [{ StartTicks: 0, EndTicks: 500_000_000 }, { StartTicks: 9_000_000_000, EndTicks: durationTicks }],
  };
}

async function configureItems(page: Page, tracks = bitmapStreams, defaultSubtitle = 8) {
  await page.route(/\/emby\/Users\/[^/]+\/Items\/(guitu|lantern)\?/, async route => {
    const response = await route.fetch();
    const item = await response.json();
    item.RunTimeTicks = durationTicks;
    const source = item.MediaSources[0];
    source.RunTimeTicks = durationTicks;
    source.DefaultSubtitleStreamIndex = defaultSubtitle;
    source.MediaStreams = [...source.MediaStreams.map((stream: { Type: string }) => stream.Type === 'Subtitle' ? { ...stream, IsDefault: false } : stream), ...tracks];
    item.MediaStreams = source.MediaStreams;
    await route.fulfill({ response, json: item });
  });
}

async function signIn(page: Page, username = 'reviewer') {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill(username);
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function detail(page: Page, item = 'guitu') {
  await page.goto(`/#/detail/${item}`);
  await expect(activePanel(page)).toHaveAttribute('aria-label', '概览');
}

async function stage(page: Page, name = '媒体信息') {
  await page.waitForTimeout(1100);
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name, exact: true }).click();
  await expect(activePanel(page)).toHaveAttribute('aria-label', name);
}

async function mockTimelines(page: Page, options: {
  version?: () => string;
  metadata?: unknown;
  transform?: (body: ReturnType<typeof payload>) => unknown;
} = {}) {
  const metadataReads: URL[] = [];
  const timelineReads: URL[] = [];
  await page.route(descriptorRoute, async route => {
    const url = new URL(route.request().url());
    metadataReads.push(url);
    await route.fulfill({ json: options.metadata ?? descriptor(url.pathname.split('/')[3], options.version?.()) });
  });
  await page.route(timelineRoute, async route => {
    const url = new URL(route.request().url());
    timelineReads.push(url);
    const parts = url.pathname.split('/');
    const body = payload(parts[3], Number(parts[5]), url.searchParams.get('tag')!);
    await route.fulfill({ json: options.transform ? options.transform(body) : body });
  });
  return { metadataReads, timelineReads };
}

async function expectRows(page: Page, indices: number[], settled = true) {
  if (settled) {
    const commit = () => page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
    await commit();
    await expect.poll(() => pendingReads.get(page)?.size ?? 0).toBe(0);
    await commit();
  }
  const panel = activePanel(page);
  await expect(panel.locator('.detail-subtitle-lane')).toHaveCount(indices.length);
  await expect(panel.locator('.detail-track-label.subtitle')).toHaveCount(indices.length);
  expect(await panel.locator('.detail-subtitle-lane').evaluateAll(elements => elements.map(element => Number(element.getAttribute('data-stream-index'))))).toEqual(indices);
  expect(await panel.locator('.detail-track-label.subtitle').evaluateAll(elements => elements.map(element => Number(element.getAttribute('data-stream-index'))))).toEqual(indices);
  await expect(panel.getByText(/正在载入时间轴|暂无字幕时间轴|此轨道暂无字幕片段/)).toHaveCount(0);
}

test.beforeEach(async ({ request, page }) => {
  const pending = new Set<Request>();
  pendingReads.set(page, pending);
  page.on('request', request => { if (descriptorRoute.test(request.url()) || timelineRoute.test(request.url()) || textRoute.test(request.url())) pending.add(request); });
  page.on('requestfinished', request => pending.delete(request));
  page.on('requestfailed', request => pending.delete(request));
  await request.post('/__fixture/reset');
  await configureItems(page);
});

test('reads valid bitmap timelines only on the media stage, preserves gaps and track selection, and never generates work', async ({ page, request }) => {
  const reads = await mockTimelines(page);
  await signIn(page);
  await detail(page);
  expect(reads.metadataReads).toHaveLength(0);
  await activePanel(page).locator('button[title="选择字幕"]').click();
  await page.getByRole('menuitemradio', { name: /中文 PGS/ }).click();
  await stage(page);
  await expectRows(page, [3, 4, 8, 12]);
  expect(reads.metadataReads).toHaveLength(1);
  expect(reads.metadataReads[0].searchParams.get('UserId')).toBe('fixture-viewer');
  expect(reads.timelineReads).toHaveLength(2);
  expect(reads.timelineReads.every(url => url.searchParams.get('api_key') === 'isolated-player-acceptance-token')).toBe(true);
  const selected = activePanel(page).locator('.detail-subtitle-lane[data-stream-index="8"]');
  await expect(selected).toHaveClass(/is-selected/);
  await expect(selected.locator('path')).toHaveAttribute('d', 'M100 2H200V8H100ZM350 2H400V8H350Z');
  await expect(activePanel(page).locator('.detail-subtitle-lane[data-stream-index="12"] path')).toHaveAttribute('d', 'M0 2H50V8H0ZM900 2H1000V8H900Z');
  await expect(activePanel(page).locator('.detail-track-label.subtitle[data-stream-index="8"] code')).toHaveText('S3');
  await expect(activePanel(page).locator('.detail-track-label.subtitle[data-stream-index="8"] small')).toHaveText('PGS');
  await expect(activePanel(page).locator('.detail-track-label.subtitle[data-stream-index="12"] small')).toHaveText('DVD');
  await activePanel(page).locator('.detail-timeline').screenshot({ path: test.info().outputPath('subtitle-timelines-desktop.png') });
  await stage(page, '概览');
  await activePanel(page).locator('button[title="选择字幕"]').click();
  await page.getByRole('menuitemradio', { name: /英文 DVD/ }).click();
  await stage(page);
  await expectRows(page, [3, 4, 8, 12]);
  await expect(activePanel(page).locator('.detail-subtitle-lane[data-stream-index="12"]')).toHaveClass(/is-selected/);
  await expect(selected).not.toHaveClass(/is-selected/);
  const state = await (await request.get('/__fixture/state')).json();
  expect(state.events).toEqual([]);
  expect(state.requests.some((entry: { path: string }) => /media-analysis|task-runs|PlaybackInfo/.test(entry.path))).toBe(false);
});

test('renders external SUP and both IDX language tracks with independent public indices and real interval gaps', async ({ page, request }) => {
  await configureItems(page, externalTimelines, externalIndices[0]);
  const intervals = [
    [{ StartTicks: 1_000_000_000, EndTicks: 2_000_000_000 }, { StartTicks: 4_000_000_000, EndTicks: 5_000_000_000 }],
    [{ StartTicks: 0, EndTicks: 500_000_000 }, { StartTicks: 8_000_000_000, EndTicks: durationTicks }],
    [{ StartTicks: 2_500_000_000, EndTicks: 3_000_000_000 }, { StartTicks: 6_000_000_000, EndTicks: 6_500_000_000 }],
  ];
  const reads = await mockTimelines(page, {
    metadata: descriptor('guitu', 'one', [...externalIndices].reverse()),
    transform: body => ({ ...body, Intervals: intervals[externalIndices.indexOf(body.StreamIndex)] }),
  });
  await signIn(page);
  await detail(page);
  await stage(page);
  await expectRows(page, [3, 4, ...externalIndices]);
  expect(reads.timelineReads.map(url => Number(url.pathname.split('/').at(-1))).sort((a, b) => a - b)).toEqual(externalIndices);
  const panel = activePanel(page);
  const paths = ['M100 2H200V8H100ZM400 2H500V8H400Z', 'M0 2H50V8H0ZM800 2H1000V8H800Z', 'M250 2H300V8H250ZM600 2H650V8H600Z'];
  for (const [ordinal, index] of externalIndices.entries()) {
    await expect(panel.locator(`.detail-subtitle-lane[data-stream-index="${index}"] path`)).toHaveAttribute('d', paths[ordinal]);
    await expect(panel.locator(`.detail-track-label.subtitle[data-stream-index="${index}"] small`)).toHaveText(ordinal === 0 ? 'PGS' : 'DVD');
  }
  await panel.locator('.detail-timeline').screenshot({ path: test.info().outputPath('external-subtitle-timelines.png') });
  const state = await (await request.get('/__fixture/state')).json();
  expect(state.events).toEqual([]);
  expect(state.requests.some((entry: { path: string }) => /media-analysis|task-runs|PlaybackInfo/.test(entry.path))).toBe(false);
});

const metadataLabelCases: Array<{ name: string; streams: MediaStream[]; labels: string[] }> = [
  {
    name: 'real IDX language titles and an unnamed SUP codec label',
    streams: [
      { ...externalTimelines[1], Title: 'en', Language: 'en', DisplayLanguage: 'English', DisplayTitle: 'English (DVD_SUBTITLE)' },
      { ...externalTimelines[2], Title: 'zh', Language: 'zh', DisplayLanguage: 'zh', DisplayTitle: 'zh (DVD_SUBTITLE)' },
      { ...externalTimelines[0], Title: '', Language: '', DisplayTitle: '(HDMV_PGS_SUBTITLE)' },
    ],
    labels: ['英语', '中文', '未知语言'],
  },
  {
    name: 'generic language codes without replacing custom titles',
    streams: [
      { ...externalTimelines[0], Title: 'pt', Language: 'pt', DisplayLanguage: 'Portuguese', DisplayTitle: 'Portuguese (HDMV_PGS_SUBTITLE)' },
      { ...externalTimelines[1], Title: 'ita', Language: 'ita', DisplayLanguage: 'Italian', DisplayTitle: 'Italian (DVD_SUBTITLE)' },
      { ...externalTimelines[2], Title: 'IT', Language: 'en', DisplayLanguage: 'English', DisplayTitle: 'IT (DVD_SUBTITLE)' },
      { ...externalTimelines[0], Index: 65_537, Title: 'Director commentary', Language: 'en', DisplayTitle: 'English (HDMV_PGS_SUBTITLE)' },
      { ...externalTimelines[0], Index: 65_538, Title: '', Language: 'en', DisplayTitle: 'Festival translation' },
    ],
    labels: ['葡萄牙语', '意大利语', 'IT', 'Director commentary', 'Festival translation'],
  },
];
for (const { name, streams, labels } of metadataLabelCases) {
  test(`normalizes subtitle metadata labels for ${name}`, async ({ page }) => {
    await configureItems(page, streams, streams[0].Index);
    await mockTimelines(page, { metadata: descriptor('guitu', 'one', streams.map(stream => stream.Index)) });
    await signIn(page);
    await detail(page);
    await stage(page);
    await expectRows(page, [3, 4, ...streams.map(stream => stream.Index)]);
    for (const [ordinal, stream] of streams.entries()) {
      const label = activePanel(page).locator(`.detail-track-label.subtitle[data-stream-index="${stream.Index}"]`);
      await expect(label.locator('strong')).toHaveText(labels[ordinal]);
      await expect(label.locator('strong')).toHaveAttribute('title', labels[ordinal]);
      await expect(label.locator('small')).toHaveText(stream.Codec === 'dvd_subtitle' ? 'DVD' : 'PGS');
    }
    await activePanel(page).locator('.detail-timeline').screenshot({ path: test.info().outputPath('subtitle-metadata-labels.png') });
  });
}

for (const kind of ['missing', 'stale', 'failed', 'unsupported', 'empty'] as const) {
  test(`hides external bitmap labels and lanes for ${kind} generation results`, async ({ page }) => {
    await configureItems(page, externalTimelines, externalIndices[0]);
    const metadata = descriptor('guitu', 'one', externalIndices);
    if (kind === 'empty') metadata.Streams = metadata.Streams.map(stream => ({ ...stream, IntervalCount: 0 }));
    const reads = await mockTimelines(page, { metadata: kind === 'missing' ? { Available: false } : kind === 'stale' ? { ...metadata, Stale: true } : metadata });
    if (kind === 'failed' || kind === 'unsupported') await page.route(descriptorRoute, route => route.fulfill({ status: kind === 'failed' ? 503 : 404, json: {} }));
    await signIn(page);
    await detail(page);
    await stage(page);
    await expectRows(page, [3, 4]);
    expect(reads.timelineReads).toEqual([]);
  });
}

const playbackSelections = [
  ...[undefined, 65_536, 2_147_483_647, 4, -1].map(storedIndex => ({ mode: 'Default', storedIndex })),
  ...[undefined, 65_536, 2_147_483_647].map(storedIndex => ({ mode: 'Smart', storedIndex })),
];
for (const { mode, storedIndex } of playbackSelections) {
  test(`excludes timeline-only defaults and remembered selection ${storedIndex ?? 'unset'} in ${mode} mode from playback and both subtitle menus`, async ({ page, request }) => {
    const fixture = await (await request.get('/__fixture/state')).json();
    await request.post('/__fixture/config', { data: { user: { ...fixture.user, Configuration: { ...fixture.user.Configuration, SubtitleMode: mode } } } });
    await configureItems(page, externalTimelines, externalIndices[0]);
    await page.route(/\/emby\/DisplayPreferences\/goby-player-item-guitu\?/, route => route.fulfill({ json: {
      Id: 'goby-player-item-guitu', CustomPrefs: { selection: JSON.stringify(storedIndex === undefined ? {} : { subtitleStreamIndex: storedIndex }) },
    } }));
    await page.route(/\/emby\/Items\/guitu\/PlaybackInfo(?:\?|$)/, async route => {
      const response = await route.fetch();
      const body = await response.json();
      body.MediaSources[0].MediaStreams.push(...externalTimelines);
      body.MediaSources[0].DefaultSubtitleStreamIndex = externalIndices[0];
      await route.fulfill({ response, json: body });
    });
    // Default mode has no playable default; Smart mode can use the matching text track.
    const expectedIndex = storedIndex === 4 || storedIndex === -1 ? storedIndex : mode === 'Smart' ? 3 : -1;
    await signIn(page);
    await detail(page);
    await activePanel(page).locator('button[title="选择字幕"]').click();
    await expect(page.getByRole('menuitemradio', { name: /External (SUP|IDX)/ })).toHaveCount(0);
    await expect(page.getByRole('menuitemradio')).toHaveCount(3);
    const label = expectedIndex === -1 ? '关闭' : expectedIndex === 4 ? '英文' : '简体中文';
    await expect(page.getByRole('menuitemradio', { name: new RegExp(`^${label}`) })).toHaveAttribute('aria-checked', 'true');
    await page.keyboard.press('Escape');
    await page.goto('/#/player/guitu');
    await expect.poll(async () => {
      const state = await (await request.get('/__fixture/state')).json();
      return state.requests.filter((entry: { path: string }) => entry.path === '/Items/guitu/PlaybackInfo').map((entry: { body: { SubtitleStreamIndex: number } }) => entry.body.SubtitleStreamIndex);
    }).toEqual([expectedIndex]);
    await expect.poll(() => page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.readyState)).toBeGreaterThanOrEqual(2);
    await page.locator('.player-video').evaluate((video: HTMLVideoElement) => video.pause());
    await page.mouse.move(650, 810);
    await page.getByRole('button', { name: '字幕', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: '字幕', exact: true });
    await expect(dialog.getByRole('option', { name: /External (SUP|IDX)/ })).toHaveCount(0);
    await expect(dialog.getByRole('option')).toHaveCount(3);
    await expect(dialog.getByRole('option', { name: label, exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(dialog.getByRole('option', { name: '简体中文', exact: true })).toBeVisible();
    await expect(dialog.getByRole('option', { name: '英文', exact: true })).toBeVisible();
    const state = await (await request.get('/__fixture/state')).json();
    expect(state.events.every((event: { SubtitleStreamIndex: number }) => event.SubtitleStreamIndex === expectedIndex)).toBe(true);
  });
}

for (const kind of ['missing', 'stale', 'failed', 'unsupported', 'empty'] as const) {
  test(`hides both bitmap labels and lanes for ${kind} results without changing subtitle choices`, async ({ page }) => {
    const reads = await mockTimelines(page, { metadata: kind === 'empty' ? { ...descriptor(), Streams: [] } : kind === 'stale' ? { ...descriptor(), Stale: true } : { Available: false } });
    if (kind === 'failed' || kind === 'unsupported') await page.route(descriptorRoute, route => route.fulfill({ status: kind === 'failed' ? 503 : 404, json: {} }));
    await signIn(page);
    await detail(page);
    await stage(page);
    await expectRows(page, [3, 4]);
    expect(reads.timelineReads).toEqual([]);
    await stage(page, '概览');
    await activePanel(page).locator('button[title="选择字幕"]').click();
    await expect(page.getByRole('menuitemradio', { name: /中文 PGS/ })).toBeVisible();
    await expect(page.getByRole('menuitemradio', { name: /英文 DVD/ })).toBeVisible();
  });
}

const invalidTextResponses = {
  empty: { contentType: 'text/vtt', body: 'WEBVTT\n\n' },
  'malformed timings': { contentType: 'text/vtt', body: 'WEBVTT\n\n00:99:00.000 --> 00:01:00.000\nInvalid\n' },
  'non-VTT body': { contentType: 'text/vtt', body: readyText.replace('WEBVTT', 'Not WEBVTT') },
  'non-VTT content type': { contentType: 'text/html', body: readyText },
  'unmarked failure': { status: 503, headers: { 'Retry-After': '1' }, body: '' },
  'marked external track': { status: 429, headers: pendingSubtitle, body: '' },
};
for (const [kind, response] of Object.entries(invalidTextResponses)) {
  test(`hides a text subtitle timeline for ${kind} without retries or misaligned rows`, async ({ page }) => {
    const embedded = kind !== 'marked external track';
    const index = embedded ? 6 : 3;
    if (embedded) await configureItems(page, [...bitmapStreams, textTrack(index)]);
    await mockTimelines(page);
    await page.clock.install();
    let reads = 0;
    await page.route(textRoute, route => {
      if (Number(route.request().url().match(textRoute)?.[1]) !== index) return route.fallback();
      reads += 1;
      return route.fulfill(response);
    });
    await signIn(page);
    await detail(page);
    await stage(page);
    await expectRows(page, embedded ? [3, 4, 8, 12] : [4, 8, 12]);
    await page.clock.fastForward(5_100);
    expect(reads).toBe(1);
  });
}

test('adds a delayed embedded text lane after marked busy and preparation responses', async ({ page }) => {
  await configureItems(page, [...bitmapStreams, textTrack(6)]);
  await mockTimelines(page);
  await page.clock.install();
  const statuses: number[] = [];
  let ready = false;
  await page.route(/\/Subtitles\/6\/0\/Stream\.vtt\?/, route => {
    const status = ready ? 200 : statuses.length ? 503 : 429;
    statuses.push(status);
    return status === 200 ? route.fulfill({ contentType: 'text/vtt; charset=utf-8', body: readyText })
      : route.fulfill({ status, headers: pendingSubtitle, json: {} });
  });
  await signIn(page);
  await detail(page);
  expect(statuses).toEqual([]);
  await stage(page);
  await expectRows(page, [3, 4, 8, 12]);
  expect(statuses).toEqual([429]);
  // Marked preparation survives the 15-second timeout of an ordinary read.
  await page.clock.fastForward(16_000);
  await expect.poll(() => statuses).toEqual([429, 503]);
  await expectRows(page, [3, 4, 8, 12]);
  ready = true;
  await page.clock.fastForward(5_100);
  await expectRows(page, [3, 4, 8, 12, 6]);
  await expect(activePanel(page).locator('.detail-subtitle-lane[data-stream-index="6"] path')).toHaveAttribute('d', 'M100 2H200V8H100Z');
  expect(statuses).toEqual([429, 503, 200]);
});

test('leaving Media Info cancels text polling and in-flight reads and drops the queue', async ({ page }) => {
  const indices = [6, 7, 8, 9, 10];
  await configureItems(page, indices.map(textTrack));
  await page.clock.install();
  const runRoute = /\/Subtitles\/(6|7|8|9|10)\/0\/Stream\.vtt\?/;
  const reads: number[] = [];
  let fresh = false;
  let release!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  await page.route(runRoute, async route => {
    const index = Number(route.request().url().match(runRoute)?.[1]);
    reads.push(index);
    if (fresh) return route.fulfill({ contentType: 'text/vtt', body: readyText });
    if (index === 6) return route.fulfill({ status: 503, headers: pendingSubtitle, json: {} });
    await held;
    await route.fulfill({ contentType: 'text/vtt', body: readyText }).catch(() => undefined);
  });
  await signIn(page);
  await detail(page);
  try {
    await stage(page);
    const sortedReads = () => [...reads].sort((a, b) => a - b);
    await expect.poll(sortedReads).toEqual([6, 7, 8, 9]);
    await expectRows(page, [3, 4], false);
    const aborted = new Set<number>();
    page.on('requestfailed', request => {
      const match = request.url().match(runRoute);
      if (match && request.failure()?.errorText === 'net::ERR_ABORTED') aborted.add(Number(match[1]));
    });
    await stage(page, '概览');
    await expect.poll(() => [...aborted].sort((a, b) => a - b)).toEqual([7, 8, 9]);
    await page.clock.fastForward(5_100);
    expect(sortedReads()).toEqual([6, 7, 8, 9]);
    await expect(page.locator('.detail-subtitle-lane')).toHaveCount(0);
  } finally { release(); }
  fresh = true;
  reads.length = 0;
  await stage(page);
  await expectRows(page, [3, 4, ...indices]);
  expect(reads.sort((a, b) => a - b)).toEqual(indices);
});

const invalidBodies: { name: string; transform: (body: ReturnType<typeof payload>) => unknown }[] = [
  { name: 'foreign source', transform: body => ({ ...body, MediaSourceId: 'another-source' }) },
  { name: 'foreign version', transform: body => ({ ...body, SourceVersion: 'replaced' }) },
  { name: 'wrong track', transform: body => ({ ...body, StreamIndex: 19 }) },
  { name: 'wrong source clock', transform: body => ({ ...body, DurationTicks: durationTicks + 1 }) },
  { name: 'empty mismatched intervals', transform: body => ({ ...body, Intervals: [] }) },
  { name: 'out of order intervals', transform: body => ({ ...body, Intervals: [...body.Intervals].reverse() }) },
  { name: 'overlapping intervals', transform: body => ({ ...body, Intervals: [{ StartTicks: 0, EndTicks: 2_000_000_000 }, { StartTicks: 1_000_000_000, EndTicks: 3_000_000_000 }] }) },
  { name: 'interval beyond duration', transform: body => ({ ...body, Intervals: [{ StartTicks: 0, EndTicks: 500_000_000 }, { StartTicks: 9_000_000_000, EndTicks: durationTicks + 1 }] }) },
  { name: 'fractional ticks', transform: body => ({ ...body, Intervals: [{ StartTicks: .5, EndTicks: 500_000_000 }, body.Intervals[1]] }) },
  { name: 'unsafe integer ticks', transform: body => ({ ...body, Intervals: [{ StartTicks: 0, EndTicks: Number.MAX_SAFE_INTEGER + 1 }, body.Intervals[1]] }) },
];
for (const invalid of invalidBodies) {
  test(`hides only the damaged track for ${invalid.name}`, async ({ page }) => {
    const reads = await mockTimelines(page, { transform: body => body.StreamIndex === 12 ? invalid.transform(body) : body });
    await signIn(page);
    await detail(page);
    await stage(page);
    await expectRows(page, [3, 4, 8]);
    expect(reads.timelineReads).toHaveLength(2);
  });
}

for (const kind of ['source', 'duration', 'version', 'duplicate track', 'too many tracks', 'off origin', 'wrong item', 'wrong track'] as const) {
  test(`rejects descriptor ${kind} before fetching any interval body`, async ({ page }) => {
    const metadata = descriptor();
    if (kind === 'source') metadata.MediaSourceId = 'replaced-source';
    if (kind === 'duration') metadata.DurationTicks += 1;
    if (kind === 'version') metadata.SourceVersion = '';
    if (kind === 'duplicate track') metadata.Streams.push(metadata.Streams[0]);
    if (kind === 'too many tracks') metadata.Streams = Array.from({ length: 65 }, (_, index) => ({ ...metadata.Streams[0], StreamIndex: index, Url: `/emby/Items/guitu/SubtitleTimelines/${index}?tag=one` }));
    if (kind === 'off origin') metadata.Streams[0].Url = 'https://unexpected.invalid/steal';
    if (kind === 'wrong item') metadata.Streams[0].Url = '/emby/Items/lantern/SubtitleTimelines/12?tag=one';
    if (kind === 'wrong track') metadata.Streams[0].Url = '/emby/Items/guitu/SubtitleTimelines/8?tag=one';
    const reads = await mockTimelines(page, { metadata });
    const external: string[] = [];
    await page.route('https://unexpected.invalid/**', route => { external.push(route.request().url()); return route.abort(); });
    await signIn(page);
    await detail(page);
    await stage(page);
    await expectRows(page, [3, 4]);
    expect(reads.timelineReads).toEqual([]);
    expect(external).toEqual([]);
  });
}

test('accepts adjacent intervals and exact decimal string ticks without filling a later gap', async ({ page }) => {
  await mockTimelines(page, { transform: body => ({ ...body, DurationTicks: String(body.DurationTicks), Intervals: body.StreamIndex === 8 ? [{ StartTicks: '0', EndTicks: '1000000000' }, { StartTicks: '1000000000', EndTicks: '2000000000' }] : body.Intervals }) });
  await signIn(page);
  await detail(page);
  await stage(page);
  await expectRows(page, [3, 4, 8, 12]);
  await expect(activePanel(page).locator('.detail-subtitle-lane[data-stream-index="8"] path')).toHaveAttribute('d', 'M0 2H100V8H0ZM100 2H200V8H100Z');
});

test('hides a zero-interval track without requesting a body and rejects oversized interval bodies', async ({ page }) => {
  const metadata = descriptor();
  metadata.Streams.find(stream => stream.StreamIndex === 12)!.IntervalCount = 0;
  const reads = await mockTimelines(page, { metadata });
  await page.route(/\/SubtitleTimelines\/8\?/, route => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ ...payload(), padding: 'x'.repeat(1024 * 1024) }) }));
  await signIn(page);
  await detail(page);
  await stage(page);
  await expectRows(page, [3, 4]);
  expect(reads.timelineReads).toHaveLength(0);
});

test('aborts a late descriptor when leaving the stage and never displays its retired result', async ({ page }) => {
  let intercepted = false;
  let release: () => void = () => {};
  const held = new Promise<void>(resolve => { release = resolve; });
  const reads = await mockTimelines(page);
  await page.route(descriptorRoute, async route => {
    intercepted = true;
    await held;
    await route.fulfill({ json: descriptor() }).catch(() => {});
  });
  await signIn(page);
  await detail(page);
  await stage(page);
  await expect.poll(() => intercepted).toBe(true);
  await expectRows(page, [3, 4], false);
  const failed = page.waitForEvent('requestfailed', request => descriptorRoute.test(request.url()));
  await stage(page, '概览');
  release();
  await failed;
  expect(reads.timelineReads).toEqual([]);
  await expect(page.locator('.detail-subtitle-lane')).toHaveCount(0);
});

test('aborts interval reads on item navigation and cannot reuse results for the next source', async ({ page }) => {
  let intercepted = false;
  let release: () => void = () => {};
  const held = new Promise<void>(resolve => { release = resolve; });
  const reads = await mockTimelines(page);
  await page.route(/\/Items\/guitu\/SubtitleTimelines\/\d+\?/, async route => {
    intercepted = true;
    await held;
    const index = Number(new URL(route.request().url()).pathname.split('/')[5]);
    await route.fulfill({ json: payload('guitu', index) }).catch(() => {});
  });
  await page.route(/\/Items\/lantern\/SubtitleTimelines\?/, route => route.fulfill({ json: { Available: false } }));
  await signIn(page);
  await detail(page);
  await stage(page);
  await expect.poll(() => intercepted).toBe(true);
  const failed = page.waitForEvent('requestfailed', request => timelineRoute.test(request.url()));
  await detail(page, 'lantern');
  release();
  await failed;
  await stage(page);
  await expectRows(page, [3, 4]);
  expect(reads.timelineReads).toEqual([]);
});

test('reloads versioned interval data after regeneration and keeps mobile label and lane rows aligned', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  let version = 'one';
  const reads = await mockTimelines(page, { version: () => version });
  await signIn(page);
  await detail(page);
  await stage(page);
  await expectRows(page, [3, 4, 8, 12]);
  await stage(page, '概览');
  version = 'two';
  await stage(page);
  await expectRows(page, [3, 4, 8, 12]);
  expect(reads.timelineReads.filter(url => url.searchParams.get('tag') === 'two')).toHaveLength(2);
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const offsets = await activePanel(page).evaluate(panel => Array.from(panel.querySelectorAll('.detail-track-label.subtitle')).map(label => {
    const lane = panel.querySelector(`.detail-subtitle-lane[data-stream-index="${label.getAttribute('data-stream-index')}"]`)!;
    return Math.abs(lane.getBoundingClientRect().top - label.getBoundingClientRect().top);
  }));
  expect(offsets.every(offset => offset < 1)).toBe(true);
  await activePanel(page).locator('.detail-secondary-scroll').evaluate(element => { element.scrollTop = element.scrollHeight; });
  await expect(activePanel(page).locator('.detail-subtitle-lane[data-stream-index="12"]')).toBeInViewport();
  expect(await activePanel(page).locator('.detail-track-label.subtitle small').evaluateAll(elements => elements.every(element => element.scrollWidth <= element.clientWidth + 1))).toBe(true);
  await activePanel(page).locator('.detail-timeline').screenshot({ path: test.info().outputPath('subtitle-timelines-mobile.png') });
});

test('limits concurrent timeline reads and keeps pending tracks out of both columns', async ({ page }) => {
  const indices = Array.from({ length: 9 }, (_, index) => 30 + index);
  await configureItems(page, indices.map(index => bitmap(index)));
  await mockTimelines(page, { metadata: descriptor('guitu', 'one', indices) });
  let active = 0;
  let maximum = 0;
  let intercepted = 0;
  let release: () => void = () => {};
  const held = new Promise<void>(resolve => { release = resolve; });
  await page.route(timelineRoute, async route => {
    active += 1;
    maximum = Math.max(maximum, active);
    intercepted += 1;
    await held;
    const index = Number(new URL(route.request().url()).pathname.split('/')[5]);
    active -= 1;
    await route.fulfill({ json: payload('guitu', index) });
  });
  await signIn(page);
  await detail(page);
  await stage(page);
  await expect.poll(() => intercepted).toBe(4);
  await expectRows(page, [3, 4], false);
  release();
  await expectRows(page, [3, 4, ...indices]);
  expect(maximum).toBeLessThanOrEqual(4);
  expect(intercepted).toBe(9);
});

test('does not count text tracks against the 64 bitmap descriptor limit', async ({ page }) => {
  const indices = Array.from({ length: 64 }, (_, index) => 30 + index);
  await configureItems(page, indices.map(index => bitmap(index)));
  const reads = await mockTimelines(page, { metadata: descriptor('guitu', 'one', indices) });
  await signIn(page);
  await detail(page);
  await stage(page);
  await expectRows(page, [3, 4, ...indices]);
  expect(reads.timelineReads).toHaveLength(64);
});

test('keeps timeline results isolated between accounts even when their test tokens match', async ({ page, request }) => {
  const state = await (await request.get('/__fixture/state')).json();
  const secondUser = { ...state.user, Id: 'fixture-other', Name: 'other' };
  await page.route('**/emby/Users/AuthenticateByName', async route => {
    if (route.request().postDataJSON().Username !== 'other') return route.continue();
    await route.fulfill({ json: { AccessToken: 'isolated-player-acceptance-token', User: secondUser, ServerId: 'fixture-server' } });
  });
  await page.route(/\/emby\/Users\/fixture-other(?:\/|\?|$)/, async route => {
    const url = new URL(route.request().url());
    if (url.pathname === '/emby/Users/fixture-other') return route.fulfill({ json: secondUser });
    const response = await route.fetch({ url: url.href.replace('/Users/fixture-other', '/Users/fixture-viewer') });
    if (/\/Items\/(guitu|lantern)$/.test(url.pathname)) {
      const item = await response.json();
      item.RunTimeTicks = durationTicks;
      item.MediaSources[0].RunTimeTicks = durationTicks;
      item.MediaSources[0].MediaStreams.push(...bitmapStreams);
      return route.fulfill({ response, json: item });
    }
    await route.fulfill({ response });
  });
  const reads = await mockTimelines(page);
  await page.route(descriptorRoute, async route => {
    if (new URL(route.request().url()).searchParams.get('UserId') !== 'fixture-other') return route.fallback();
    await route.fulfill({ json: { Available: false } });
  });
  await signIn(page);
  await detail(page);
  await stage(page);
  await expectRows(page, [3, 4, 8, 12]);
  const previousReads = reads.timelineReads.length;
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '个人中心', exact: true }).click();
  await page.getByRole('button', { name: '退出登录', exact: true }).click();
  await expect(page.getByLabel('用户名', { exact: true })).toBeVisible();
  await signIn(page, 'other');
  await detail(page);
  await stage(page);
  await expectRows(page, [3, 4]);
  expect(reads.timelineReads).toHaveLength(previousReads);
});
