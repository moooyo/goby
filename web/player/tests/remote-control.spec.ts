import { expect, test, type APIRequestContext, type Page, type Route, type WebSocketRoute } from '@playwright/test';

const ticks = 10_000_000;
const sessionId = 'fixture-session';

interface FixtureEvent {
  route: string;
  ItemId?: string;
  PlaySessionId?: string;
  PositionTicks?: number;
  IsPaused?: boolean;
}

interface FixtureState {
  user: { Configuration: Record<string, unknown>; [key: string]: unknown };
  events: FixtureEvent[];
  requests: Array<{ method: string; path: string; body: Record<string, unknown> }>;
}

interface ControlConnection {
  socket: WebSocketRoute;
  closed: boolean;
}

async function state(request: APIRequestContext): Promise<FixtureState> {
  return (await request.get('/__fixture/state')).json();
}

async function mockControl(page: Page) {
  const connections: ControlConnection[] = [];
  await page.routeWebSocket(url => url.pathname === '/embywebsocket', socket => {
    const connection = { socket, closed: false };
    connections.push(connection);
    socket.onClose(() => { connection.closed = true; });
  });
  return {
    connections,
    live: () => connections.filter(connection => !connection.closed),
    async ready() {
      await expect.poll(() => connections.filter(connection => !connection.closed).length).toBe(1);
      return connections.find(connection => !connection.closed)!;
    },
    async disconnect(connection: ControlConnection) {
      connection.closed = true;
      await connection.socket.close({ code: 1012, reason: 'Isolated fixture reconnect' });
    },
  };
}

function send(connection: ControlConnection, MessageType: string, Data: unknown, MessageId?: string) {
  connection.socket.send(JSON.stringify({ MessageType, Data, ...(MessageId ? { MessageId } : {}) }));
}

function playstate(connection: ControlConnection, Command: string, fields: Record<string, unknown> = {}, MessageId?: string) {
  send(connection, 'Playstate', { Id: sessionId, Command, ...fields }, MessageId);
}

function message(connection: ControlConnection, Header: string, Text = Header, TimeoutMs?: string) {
  send(connection, 'GeneralCommand', { Id: sessionId, Name: 'DisplayMessage', Arguments: { Header, Text, ...(TimeoutMs === undefined ? {} : { TimeoutMs }) } });
}

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function openEpisode(page: Page, id = 'deep-2-5') {
  await page.goto(`/#/player/${id}`);
  await page.waitForFunction(() => {
    const video = document.querySelector<HTMLVideoElement>('.player-video');
    return video && video.readyState >= 2 && !video.paused && video.currentTime > 0;
  });
}

function progress(events: FixtureEvent[], itemId = 'deep-2-5') {
  return events.filter(event => event.route === '/Sessions/Playing/Progress' && event.ItemId === itemId).at(-1);
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset');
  const fixture = await state(request);
  await request.post('/__fixture/config', { data: { user: { ...fixture.user, Configuration: { ...fixture.user.Configuration, EnableNextEpisodeAutoPlay: false } } } });
});

test('registers only implemented video controls and keeps one effective connection', async ({ page, request }) => {
  const control = await mockControl(page);
  await signIn(page);
  const connection = await control.ready();
  const url = new URL(connection.socket.url());
  expect(url.searchParams.get('api_key')).toBe('isolated-player-acceptance-token');
  expect(url.searchParams.get('deviceId')).toBeTruthy();
  await expect.poll(async () => (await state(request)).requests.filter(entry => entry.path === '/Sessions/Capabilities/Full').length).toBeGreaterThan(0);
  const capabilities = (await state(request)).requests.filter(entry => entry.path === '/Sessions/Capabilities/Full').at(-1)!.body;
  expect(capabilities.PlayableMediaTypes).toEqual(['Video']);
  expect(capabilities.SupportsMediaControl).toBe(true);
  expect(capabilities.SupportsSync).not.toBe(true);
  expect(capabilities.SupportedCommands).toEqual(expect.arrayContaining(['Stop', 'Pause', 'Unpause', 'PlayPause', 'Seek', 'DisplayMessage']));
  expect(capabilities.SupportedCommands).not.toContain('PlayNext');
  expect(capabilities.SupportedCommands).not.toContain('PlayLast');
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '个人中心', exact: true }).click();
  await expect(page.getByLabel('观看统计', { exact: true })).toBeVisible();
  expect(control.live()).toHaveLength(1);
  expect(control.connections).toHaveLength(1);
});

test('applies pause, seek and unpause to the actual media and reports observed state', async ({ page, request }) => {
  const control = await mockControl(page);
  await signIn(page);
  await openEpisode(page);
  const connection = await control.ready();
  const video = page.locator('.player-video');
  playstate(connection, 'Pause');
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true);
  await expect.poll(async () => progress((await state(request)).events)?.IsPaused).toBe(true);
  playstate(connection, 'Seek', { SeekPositionTicks: 7 * ticks });
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => Math.round(element.currentTime * 10))).toBe(70);
  await expect.poll(async () => Math.round((progress((await state(request)).events)?.PositionTicks ?? -1) / ticks * 10)).toBe(70);
  playstate(connection, 'Unpause');
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(false);
  await expect.poll(async () => progress((await state(request)).events)?.IsPaused).toBe(false);
  expect(await video.evaluate((element: HTMLVideoElement) => element.currentTime)).toBeGreaterThanOrEqual(7);
});

test('stops playback once, reports its final position and returns to the series detail', async ({ page, request }) => {
  const control = await mockControl(page);
  await signIn(page);
  await openEpisode(page);
  const connection = await control.ready();
  const before = await page.locator('.player-video').evaluate((element: HTMLVideoElement) => element.currentTime);
  playstate(connection, 'Stop');
  await expect(page).toHaveURL(/#\/detail\/deep$/);
  await expect(page.locator('.player-video')).toHaveCount(0);
  await expect.poll(async () => (await state(request)).events.filter(event => event.route === '/Sessions/Playing/Stopped' && event.ItemId === 'deep-2-5').length).toBe(1);
  const stopped = (await state(request)).events.find(event => event.route === '/Sessions/Playing/Stopped' && event.ItemId === 'deep-2-5')!;
  expect(stopped.PositionTicks).toBeGreaterThanOrEqual(Math.floor(before * ticks));
  expect(stopped.IsPaused).toBe(true);
});

test('shows remote messages as plain text and restores keyboard focus without exiting playback', async ({ page }) => {
  const control = await mockControl(page);
  await signIn(page);
  await openEpisode(page);
  const connection = await control.ready();
  const origin = page.getByRole('button', { name: '暂停', exact: true });
  await origin.focus();
  const text = '<img src=x onerror="window.__remoteMessageExecuted=true">Maintenance <b>soon</b>';
  message(connection, '管理员消息', text);
  const dialog = page.getByRole('dialog', { name: '管理员消息', exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText(text);
  await expect(dialog.locator('img, b, script')).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as { __remoteMessageExecuted?: boolean }).__remoteMessageExecuted)).toBeUndefined();
  await expect(dialog.getByRole('button', { name: '知道了', exact: true })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(page).toHaveURL(/#\/player\/deep-2-5$/);
  await expect(origin).toBeFocused();
});

test('keeps timed notices nonmodal and gives them a manual dismissal', async ({ page }) => {
  const control = await mockControl(page);
  await signIn(page);
  const connection = await control.ready();
  const origin = page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '搜索', exact: true });
  await origin.focus();
  message(connection, '维护提醒', '今晚维护，观看进度会保留。', '60000');
  const notice = page.getByRole('status').filter({ hasText: '今晚维护，观看进度会保留。' });
  await expect(notice).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(origin).toBeFocused();
  await notice.getByRole('button', { name: '关闭消息', exact: true }).click();
  await expect(notice).toBeHidden();
});

test('plays one requested item at the supplied position in the built-in player', async ({ page, request }) => {
  const control = await mockControl(page);
  await page.addInitScript(() => {
    localStorage.setItem('goby.player.preferences.v1.fixture-server.fixture-viewer.global', JSON.stringify({ player: 'mpv', quality: 'original' }));
  });
  await signIn(page);
  const connection = await control.ready();
  send(connection, 'Play', { ItemIds: ['deep-2-6'], PlayCommand: 'PlayNow', StartPositionTicks: 8 * ticks, MediaSourceId: 'deep-2-6-source', AudioStreamIndex: 2, SubtitleStreamIndex: -1 });
  await expect(page).toHaveURL(/#\/player\/deep-2-6$/);
  await page.waitForFunction(() => {
    const video = document.querySelector<HTMLVideoElement>('.player-video');
    return video && !video.paused && video.currentTime >= 8;
  });
  const negotiation = (await state(request)).requests.filter(entry => entry.path === '/Items/deep-2-6/PlaybackInfo').at(-1)!;
  expect(negotiation.body).toMatchObject({ StartTimeTicks: 8 * ticks, MediaSourceId: 'deep-2-6-source', AudioStreamIndex: 2, SubtitleStreamIndex: -1 });
  await expect.poll(async () => (await state(request)).events.some(event => event.route === '/Sessions/Playing' && event.ItemId === 'deep-2-6' && (event.PositionTicks ?? 0) >= 8 * ticks)).toBe(true);
  expect(await page.locator('.player-video').evaluate((element: HTMLVideoElement) => element.currentTime)).toBeLessThan(12);
});

test('preserves an early remote pause and seek until the requested media becomes ready', async ({ page, request }) => {
  const control = await mockControl(page);
  const pendingMedia: Route[] = [];
  let releaseMedia = false;
  await page.route('**/emby/Videos/deep-2-6/stream.webm?*', async route => {
    if (releaseMedia) await route.continue();
    else pendingMedia.push(route);
  });
  const release = async () => {
    releaseMedia = true;
    await Promise.all(pendingMedia.splice(0).map(route => route.continue().catch(() => undefined)));
  };
  try {
    await signIn(page);
    const connection = await control.ready();
    send(connection, 'Play', { ItemIds: ['deep-2-6'], PlayCommand: 'PlayNow', StartPositionTicks: 8 * ticks, MediaSourceId: 'deep-2-6-source' });
    await expect(page).toHaveURL(/#\/player\/deep-2-6$/);
    await expect.poll(() => pendingMedia.length).toBeGreaterThan(0);
    const video = page.locator('.player-video');
    expect(await video.evaluate((element: HTMLVideoElement) => element.readyState)).toBe(0);
    playstate(connection, 'Pause');
    playstate(connection, 'Seek', { SeekPositionTicks: 12 * ticks });
    message(connection, 'Media readiness barrier', 'The early playback controls have been received.', '60000');
    await expect(page.getByRole('status').filter({ hasText: 'The early playback controls have been received.' })).toBeVisible();
    expect((await state(request)).events.filter(event => event.ItemId === 'deep-2-6')).toHaveLength(0);
    await release();
    await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.readyState)).toBeGreaterThanOrEqual(2);
    await expect.poll(() => video.evaluate((element: HTMLVideoElement) => Math.round(element.currentTime * 10))).toBe(120);
    expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true);
    await expect.poll(async () => (await state(request)).events.some(event => event.route === '/Sessions/Playing'
      && event.ItemId === 'deep-2-6' && event.IsPaused === true && Math.round((event.PositionTicks ?? 0) / ticks * 10) === 120)).toBe(true);
  } finally {
    await release();
  }
});

test('does not silently play the first version when the requested media source is absent', async ({ page, request }) => {
  const control = await mockControl(page);
  await signIn(page);
  const connection = await control.ready();
  send(connection, 'Play', { ItemIds: ['deep-2-6'], PlayCommand: 'PlayNow', MediaSourceId: 'absent-source', StartPositionTicks: 0 });
  await expect(page).toHaveURL(/#\/player\/deep-2-6$/);
  await expect(page.getByRole('heading', { name: '暂时无法播放', exact: true })).toBeVisible();
  const fixture = await state(request);
  const negotiation = fixture.requests.filter(entry => entry.path === '/Items/deep-2-6/PlaybackInfo').at(-1)!;
  expect(negotiation.body.MediaSourceId).toBe('absent-source');
  expect(fixture.requests.filter(entry => /^\/Videos\/deep-2-6\/stream(?:\.|$)/.test(entry.path))).toHaveLength(0);
  expect(fixture.events.filter(event => event.ItemId === 'deep-2-6')).toHaveLength(0);
  await expect(page.locator('.player-video')).not.toHaveAttribute('src');
});

test('rejects other-session, malformed, unsupported and queued commands without changing playback', async ({ page, request }) => {
  const control = await mockControl(page);
  await signIn(page);
  await openEpisode(page);
  const connection = await control.ready();
  const video = page.locator('.player-video');
  const before = await state(request);
  const negotiationsBefore = before.requests.filter(entry => /\/PlaybackInfo$/.test(entry.path)).length;
  playstate(connection, 'Pause', { Id: 'different-session' });
  playstate(connection, 'Seek', { SeekPositionTicks: -1 });
  playstate(connection, 'Seek', { SeekPositionTicks: '0' });
  playstate(connection, 'UnsupportedCommand');
  connection.socket.send('{"MessageType":"Playstate","Data":');
  send(connection, 'Play', { ItemIds: ['deep-2-6', 'deep-2-7'], PlayCommand: 'PlayNow' });
  send(connection, 'Play', { ItemIds: ['deep-2-6'], PlayCommand: 'PlayNext' });
  message(connection, 'Command barrier');
  await expect(page.getByRole('dialog', { name: 'Command barrier', exact: true })).toBeVisible();
  await expect(page).toHaveURL(/#\/player\/deep-2-5$/);
  expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(false);
  expect(await video.evaluate((element: HTMLVideoElement) => element.currentTime)).toBeGreaterThanOrEqual(12);
  const after = await state(request);
  expect(after.requests.filter(entry => /\/PlaybackInfo$/.test(entry.path))).toHaveLength(negotiationsBefore);
  expect(after.events.filter(event => event.route === '/Sessions/Playing/Stopped' && event.ItemId === 'deep-2-5')).toHaveLength(0);
});

test('reconnects with fresh capabilities and executes an identified toggle only once', async ({ page, request }) => {
  const control = await mockControl(page);
  await signIn(page);
  await openEpisode(page);
  const previous = await control.ready();
  const count = (await state(request)).requests.filter(entry => entry.path === '/Sessions/Capabilities/Full').length;
  await control.disconnect(previous);
  await expect.poll(() => control.connections.length).toBeGreaterThanOrEqual(2);
  const connection = await control.ready();
  expect(connection).not.toBe(previous);
  await expect.poll(async () => (await state(request)).requests.filter(entry => entry.path === '/Sessions/Capabilities/Full').length).toBeGreaterThan(count);
  playstate(connection, 'PlayPause', {}, 'isolated-toggle');
  playstate(connection, 'PlayPause', {}, 'isolated-toggle');
  message(connection, 'Reconnect barrier');
  await expect(page.getByRole('dialog', { name: 'Reconnect barrier', exact: true })).toBeVisible();
  expect(await page.locator('.player-video').evaluate((element: HTMLVideoElement) => element.paused)).toBe(true);
  await expect.poll(async () => progress((await state(request)).events)?.IsPaused).toBe(true);
  expect(control.live()).toHaveLength(1);
});

test('closes the receiver when signing out and does not reconnect without a login', async ({ page, request }) => {
  await page.clock.install();
  const control = await mockControl(page);
  await signIn(page);
  await control.ready();
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '个人中心', exact: true }).click();
  await page.getByRole('button', { name: '退出登录', exact: true }).click();
  await expect(page.getByLabel('用户名', { exact: true })).toBeVisible();
  await expect.poll(() => control.live().length).toBe(0);
  const connectionsAfterLogout = control.connections.length;
  const registrationsAfterLogout = (await state(request)).requests.filter(entry => entry.path === '/Sessions/Capabilities/Full').length;
  await page.evaluate(() => window.dispatchEvent(new Event('online')));
  await page.clock.fastForward(60_000);
  await page.getByLabel('用户名', { exact: true }).fill('still-signed-out');
  expect(control.connections).toHaveLength(connectionsAfterLogout);
  expect((await state(request)).requests.filter(entry => entry.path === '/Sessions/Capabilities/Full')).toHaveLength(registrationsAfterLogout);
});

test('clears an expired login instead of reconnecting with rejected credentials', async ({ page, request }) => {
  const control = await mockControl(page);
  await signIn(page);
  const connection = await control.ready();
  await request.post('/__fixture/config', { data: { failures: { '/Sessions/Capabilities/Full': 401 } } });
  await control.disconnect(connection);
  await expect(page.getByLabel('用户名', { exact: true })).toBeVisible();
  await expect.poll(() => control.live().length).toBe(0);
  expect(await page.evaluate(() => localStorage.getItem('goby.player.session.v1'))).toBeNull();
});
