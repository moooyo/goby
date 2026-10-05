import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

const ticks = 71_400_000_000n;
const levels = [512, 1024, 2048, 4096];
const activePanel = (page: Page) => page.locator('.deck-panel[data-active="true"]');

for (const fraction of [.001, .999]) {
  test(`keeps timeline labels readable and the resume bubble in bounds at ${fraction}`, async ({ page, request }) => {
    const state = await (await request.get('/__fixture/state')).json();
    const item = state.items.find((entry: { Id: string }) => entry.Id === 'guitu');
    item.UserData.PlaybackPositionTicks = Math.floor(item.RunTimeTicks * fraction);
    await request.post('/__fixture/config', { data: { items: state.items } });
    await mockWaveforms(page);
    await signIn(page);
    await detail(page);
    await stage(page, '媒体信息');
    for (const width of [320, 375, 393, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      const ruler = activePanel(page).locator('.detail-time-ruler');
      await expect.poll(async () => ruler.evaluate(element => {
        const bounds = element.getBoundingClientRect();
        const labels = Array.from(element.children).filter(child => getComputedStyle(child).display !== 'none').map(child => child.getBoundingClientRect());
        const bubble = element.parentElement!.querySelector('.detail-playhead > span')!.getBoundingClientRect();
        return labels.every((label, index) => label.left >= bounds.left - 1 && label.right <= bounds.right + 1
          && (!index || label.left - labels[index - 1].right >= 4))
          && bubble.left >= bounds.left - 1 && bubble.right <= bounds.right + 1
          && (bounds.width > 340 || bubble.bottom <= labels[0].top - 1);
      })).toBe(true);
      await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    }
  });
}

function descriptor(item = 'guitu', generation = 'one') {
  return {
    Available: true, MediaSourceId: `${item}-source`, SourceVersion: 'source-version-one', DurationTicks: Number(ticks),
    // The descriptor order intentionally differs from the catalogue order.
    Streams: [7, 1].map(StreamIndex => ({ StreamIndex, Channels: 6, SampleRate: 48000, ChannelLayout: '5.1', Levels: levels.map(BucketCount => ({
      BucketCount, Url: `/emby/Items/${item}/AudioWaveforms/${StreamIndex}/${BucketCount}.bin?tag=${generation}`,
    })) })),
  };
}

function payload(streamIndex: number, count: number, variant: 'normal' | 'silent' | 'missing' = 'normal') {
  const validityBytes = Math.ceil(count / 8);
  const body = Buffer.alloc(32 + count * 4 + validityBytes);
  body.write('GAWL', 0, 'ascii');
  body.writeUInt16LE(1, 4);
  body.writeUInt16LE(32, 6);
  body.writeUInt32LE(streamIndex, 8);
  body.writeUInt32LE(count, 12);
  body.writeBigUInt64LE(ticks, 16);
  body.writeUInt32LE(validityBytes, 24);
  for (let index = 0; index < count; index += 1) {
    const valid = variant !== 'missing' && !(variant === 'normal' && index >= count / 3 && index < count / 3 + count / 16);
    const peak = variant === 'silent' || !valid ? 0 : Math.round((.15 + Math.abs(Math.sin(index / count * 38 + streamIndex)) * .7) * 65535);
    body.writeUInt16LE(peak, 32 + index * 4);
    body.writeUInt16LE(Math.round(peak * .45), 34 + index * 4);
    if (valid) body[32 + count * 4 + (index >> 3)] |= 1 << (index & 7);
  }
  return body;
}

async function configureStreams(request: APIRequestContext) {
  const state = await (await request.get('/__fixture/state')).json();
  for (const item of state.items) {
    if (!['guitu', 'lantern'].includes(item.Id)) continue;
    item.MediaSources[0].MediaStreams.find((stream: { Index: number }) => stream.Index === 2).Index = 7;
  }
  await request.post('/__fixture/config', { data: { items: state.items } });
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

async function stage(page: Page, name: string) {
  // The handoff deliberately locks stage controls while their transition completes.
  await page.waitForTimeout(1100);
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name, exact: true }).click();
  await expect(activePanel(page)).toHaveAttribute('aria-label', name);
}

async function mockWaveforms(page: Page, options: { generation?: () => string; variant?: 'normal' | 'silent' | 'missing'; transform?: (body: Buffer) => Buffer; metadata?: unknown } = {}) {
  const metadataRequests: URL[] = [];
  const levelRequests: URL[] = [];
  await page.route(/\/emby\/Items\/[^/]+\/AudioWaveforms\?/, async route => {
    const url = new URL(route.request().url());
    metadataRequests.push(url);
    const item = url.pathname.split('/')[3];
    await route.fulfill({ json: options.metadata ?? descriptor(item, options.generation?.() ?? 'one') });
  });
  await page.route(/\/emby\/Items\/[^/]+\/AudioWaveforms\/\d+\/\d+\.bin\?/, async route => {
    const url = new URL(route.request().url());
    levelRequests.push(url);
    expect(url.searchParams.get('api_key')).toBe('isolated-player-acceptance-token');
    const parts = url.pathname.split('/');
    const body = payload(Number(parts[5]), Number(parts[6].split('.')[0]), options.variant);
    await route.fulfill({ contentType: 'application/octet-stream', body: options.transform ? options.transform(body) : body });
  });
  return { metadataRequests, levelRequests };
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset');
  await configureStreams(request);
});

test('loads only the active media stage and aligns independent waveforms by original stream index', async ({ page, request }) => {
  const reads = await mockWaveforms(page);
  await signIn(page);
  await detail(page);
  expect(reads.metadataRequests).toHaveLength(0);
  await stage(page, '媒体信息');
  await expect(activePanel(page).locator('.detail-waveform')).toHaveCount(2);
  expect(reads.metadataRequests).toHaveLength(1);
  expect(reads.metadataRequests[0].searchParams.has('MediaSourceId')).toBe(false);
  expect(reads.metadataRequests[0].searchParams.get('UserId')).toBe('fixture-viewer');
  const first = activePanel(page).locator('.detail-audio-lane[data-stream-index="1"]');
  const second = activePanel(page).locator('.detail-audio-lane[data-stream-index="7"]');
  await expect(first).toHaveClass(/is-selected/);
  await expect(second).not.toHaveClass(/is-selected/);
  await expect(first.getByRole('img')).toHaveAccessibleName(/当前音轨/);
  await expect(first.locator('.detail-waveform-peaks')).not.toHaveAttribute('d', '');
  expect(await first.locator('.detail-waveform-peaks').getAttribute('d')).not.toBe(await second.locator('.detail-waveform-peaks').getAttribute('d'));
  await expect(first.locator('.detail-waveform-gap')).toHaveCount(1);
  await expect(activePanel(page).locator('svg[aria-label="字幕时间轴"]')).toHaveCount(2);
  const state = await (await request.get('/__fixture/state')).json();
  expect(state.events).toEqual([]);
  expect(state.requests.some((entry: { path: string; method: string }) => /media-analysis|task-runs|PlaybackInfo/.test(entry.path))).toBe(false);
  await activePanel(page).locator('.detail-timeline').screenshot({ path: test.info().outputPath('waveforms-desktop.png') });
});

test('chooses a small screen resolution and reuses the same version when returning to the stage', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const reads = await mockWaveforms(page);
  await signIn(page);
  await detail(page);
  await stage(page, '媒体信息');
  await expect(activePanel(page).locator('.detail-waveform')).toHaveCount(2);
  await expect(activePanel(page).locator('.detail-waveform').first()).toHaveAttribute('data-bucket-count', '512');
  expect(reads.levelRequests).toHaveLength(2);
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const originalPath = await activePanel(page).locator('.detail-waveform-peaks').first().getAttribute('d');
  await activePanel(page).locator('.detail-timeline').screenshot({ path: test.info().outputPath('waveforms-mobile.png') });
  await stage(page, '概览');
  await stage(page, '媒体信息');
  await expect(activePanel(page).locator('.detail-waveform')).toHaveCount(2);
  expect(reads.metadataRequests).toHaveLength(2);
  expect(reads.levelRequests).toHaveLength(2);
  expect(await activePanel(page).locator('.detail-waveform-peaks').first().getAttribute('d')).toBe(originalPath);
});

test('loads a new payload after explicit regeneration changes the versioned URL', async ({ page }) => {
  let generation = 'one';
  const reads = await mockWaveforms(page, { generation: () => generation });
  await signIn(page);
  await detail(page);
  await stage(page, '媒体信息');
  await expect(activePanel(page).locator('.detail-waveform')).toHaveCount(2);
  await stage(page, '概览');
  generation = 'two';
  await stage(page, '媒体信息');
  await expect(activePanel(page).locator('.detail-waveform')).toHaveCount(2);
  expect(reads.levelRequests.filter(url => url.searchParams.get('tag') === 'two')).toHaveLength(2);
});

for (const status of ['missing', 'stale', 'error'] as const) {
  test(`shows the ${status} state without creating work or drawing a waveform`, async ({ page }) => {
    const reads = await mockWaveforms(page, { metadata: { Available: false, ...(status === 'stale' ? { Stale: true } : {}) } });
    if (status === 'error') await page.route(/\/emby\/Items\/[^/]+\/AudioWaveforms\?/, route => route.fulfill({ status: 503, json: {} }));
    await signIn(page);
    await detail(page);
    await stage(page, '媒体信息');
    const expected = status === 'stale' ? '片源已变化，请重新生成波形' : status === 'error' ? '波形暂时无法载入' : '暂无波形数据';
    await expect(activePanel(page).locator('.detail-audio-lane').first()).toHaveText(expected);
    await expect(activePanel(page).locator('.detail-waveform')).toHaveCount(0);
    expect(reads.levelRequests).toEqual([]);
  });
}

test('distinguishes known silence from a full interval without valid samples', async ({ page }) => {
  await mockWaveforms(page, { variant: 'silent' });
  await signIn(page);
  await detail(page);
  await stage(page, '媒体信息');
  const lane = activePanel(page).locator('.detail-audio-lane').first();
  await expect(lane.locator('.detail-waveform')).toHaveCount(1);
  await expect(lane.locator('.detail-waveform-silence')).not.toHaveAttribute('d', '');
  await expect(lane.locator('.detail-waveform-gap')).toHaveCount(0);
  await page.unroute(/\/emby\/Items\/[^/]+\/AudioWaveforms\/\d+\/\d+\.bin\?/);
  await mockWaveforms(page, { variant: 'missing', generation: () => 'missing' });
  await stage(page, '概览');
  await stage(page, '媒体信息');
  await expect(lane).toContainText('此音轨没有有效波形片段');
  await expect(lane.locator('.detail-waveform-silence')).toHaveAttribute('d', '');
  await expect(lane.locator('.detail-waveform-gap')).toHaveCount(1);
});

const invalidPayloads: { name: string; transform: (body: Buffer) => Buffer }[] = [
  { name: 'wrong stream index', transform: body => { body.writeUInt32LE(99, 8); return body; } },
  { name: 'wrong duration', transform: body => { body.writeBigUInt64LE(ticks + 1n, 16); return body; } },
  { name: 'RMS above its peak', transform: body => { body.writeUInt16LE(1, 32); body.writeUInt16LE(2, 34); return body; } },
  { name: 'truncated body', transform: body => body.subarray(0, body.length - 1) },
  { name: 'oversized body', transform: body => Buffer.concat([body, Buffer.from([0])]) },
  { name: 'unknown format version', transform: body => { body.writeUInt16LE(2, 4); return body; } },
];
for (const invalid of invalidPayloads) {
  test(`rejects ${invalid.name} instead of showing invented waveform data`, async ({ page }) => {
    await mockWaveforms(page, { transform: invalid.transform });
    await signIn(page);
    await detail(page);
    await stage(page, '媒体信息');
    await expect(activePanel(page).locator('.detail-audio-lane').first()).toHaveText('波形暂时无法载入');
    await expect(activePanel(page).locator('.detail-waveform')).toHaveCount(0);
  });
}

test('rejects off-origin level URLs before sending credentials or fetching any level', async ({ page }) => {
  const metadata = descriptor();
  metadata.Streams[0].Levels[0].Url = 'https://unexpected.invalid/steal';
  const reads = await mockWaveforms(page, { metadata });
  const external: string[] = [];
  await page.route('https://unexpected.invalid/**', route => { external.push(route.request().url()); return route.abort(); });
  await signIn(page);
  await detail(page);
  await stage(page, '媒体信息');
  await expect(activePanel(page).locator('.detail-audio-lane').first()).toHaveText('波形暂时无法载入');
  expect(reads.levelRequests).toEqual([]);
  expect(external).toEqual([]);
});

test('aborts an in-flight descriptor when leaving the media stage', async ({ page }) => {
  let intercepted = false;
  let release: () => void = () => {};
  const held = new Promise<void>(resolve => { release = resolve; });
  const reads = await mockWaveforms(page);
  await page.route(/\/emby\/Items\/[^/]+\/AudioWaveforms\?/, async route => {
    intercepted = true;
    await held;
    await route.fulfill({ json: descriptor() }).catch(() => {});
  });
  await signIn(page);
  await detail(page);
  await stage(page, '媒体信息');
  await expect.poll(() => intercepted).toBe(true);
  const failed = page.waitForEvent('requestfailed', request => /\/AudioWaveforms\?/.test(request.url()));
  await stage(page, '概览');
  release();
  await failed;
  expect(reads.levelRequests).toEqual([]);
  await expect(activePanel(page).locator('.detail-waveform')).toHaveCount(0);
});

test('does not reuse data between source items', async ({ page }) => {
  const reads = await mockWaveforms(page);
  await signIn(page);
  await detail(page);
  await stage(page, '媒体信息');
  await expect(activePanel(page).locator('.detail-waveform')).toHaveCount(2);
  await detail(page, 'lantern');
  await stage(page, '媒体信息');
  await expect(activePanel(page).locator('.detail-waveform')).toHaveCount(2);
  expect(reads.metadataRequests.map(url => url.pathname)).toEqual(['/emby/Items/guitu/AudioWaveforms', '/emby/Items/lantern/AudioWaveforms']);
  expect(reads.levelRequests.filter(url => url.pathname.includes('/Items/lantern/'))).toHaveLength(2);
});

test('rejects a descriptor that belongs to a different media source', async ({ page }) => {
  const metadata = descriptor();
  metadata.MediaSourceId = 'guitu-replaced-source';
  const reads = await mockWaveforms(page, { metadata });
  await signIn(page);
  await detail(page);
  await stage(page, '媒体信息');
  await expect(activePanel(page).locator('.detail-audio-lane').first()).toHaveText('片源已变化，请重新生成波形');
  expect(reads.levelRequests).toEqual([]);
});

test('aborts an in-flight level when navigating away from the item', async ({ page }) => {
  let intercepted = false;
  let release: () => void = () => {};
  const held = new Promise<void>(resolve => { release = resolve; });
  await mockWaveforms(page);
  await page.route(/\/emby\/Items\/[^/]+\/AudioWaveforms\/\d+\/\d+\.bin\?/, async route => {
    intercepted = true;
    await held;
    await route.abort().catch(() => {});
  });
  await signIn(page);
  await detail(page);
  await stage(page, '媒体信息');
  await expect.poll(() => intercepted).toBe(true);
  const failed = page.waitForEvent('requestfailed', request => /\/AudioWaveforms\/\d+\/\d+\.bin\?/.test(request.url()));
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '首页', exact: true }).click();
  release();
  await failed;
  await expect(page.locator('.detail-waveform')).toHaveCount(0);
});

test('keeps waveform state isolated between signed-in users', async ({ page, request }) => {
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
    await route.fulfill({ response });
  });
  const reads = await mockWaveforms(page);
  await page.route(/\/emby\/Items\/[^/]+\/AudioWaveforms\?/, async route => {
    if (new URL(route.request().url()).searchParams.get('UserId') !== 'fixture-other') return route.fallback();
    await route.fulfill({ json: { Available: false } });
  });
  await signIn(page);
  await detail(page);
  await stage(page, '媒体信息');
  await expect(activePanel(page).locator('.detail-waveform')).toHaveCount(2);
  const previousReads = reads.levelRequests.length;
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '个人中心', exact: true }).click();
  await page.getByRole('button', { name: '退出登录', exact: true }).click();
  await expect(page.getByLabel('用户名', { exact: true })).toBeVisible();
  await signIn(page, 'other');
  await detail(page);
  await stage(page, '媒体信息');
  await expect(activePanel(page).locator('.detail-audio-lane').first()).toHaveText('暂无波形数据');
  await expect(activePanel(page).locator('.detail-waveform')).toHaveCount(0);
  expect(reads.levelRequests).toHaveLength(previousReads);
});
