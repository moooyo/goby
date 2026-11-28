import { expect, test, type APIRequestContext, type Page, type Route, type WebSocketRoute } from '@playwright/test';
import type { MediaItem } from '../src/types';

const itemsPath = '/emby/Users/fixture-viewer/Items';
const playerToken = 'isolated-player-acceptance-token';
const results = (Items: MediaItem[]) => ({ Items, TotalRecordCount: Items.length });

function sendChanged(socket: WebSocketRoute, patch: Record<string, unknown> = {}, messageId = 'catalog-change') {
  socket.send(JSON.stringify({ MessageType: 'LibraryChanged', MessageId: messageId, Data: {
    FoldersAddedTo: [], FoldersRemovedFrom: [], ItemsAdded: [], ItemsRemoved: [], ItemsUpdated: [], CollectionFolders: [], IsEmpty: false, ...patch,
  } }));
}

function sendBarrier(socket: WebSocketRoute, header: string) {
  socket.send(JSON.stringify({ MessageType: 'GeneralCommand', Data: { Name: 'DisplayMessage', Arguments: { Header: header, Text: header, TimeoutMs: '60000' } } }));
}

async function mockSocket(page: Page) {
  const sockets: WebSocketRoute[] = [];
  await page.routeWebSocket(url => url.pathname === '/embywebsocket', socket => { sockets.push(socket); });
  return async (index = 0) => {
    await expect.poll(() => sockets.length).toBeGreaterThan(index);
    const socket = sockets[index];
    const url = new URL(socket.url());
    expect(url.searchParams.get('api_key')).toBe(playerToken);
    expect(url.searchParams.get('deviceId')).toBeTruthy();
    return socket;
  };
}

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function fixtureItems(request: APIRequestContext): Promise<MediaItem[]> {
  return (await (await request.get('/__fixture/state')).json()).items;
}

async function updateItems(request: APIRequestContext, items: MediaItem[]) {
  expect((await request.post('/__fixture/config', { data: { items } })).ok()).toBe(true);
}

test.beforeEach(async ({ page, request }) => {
  await request.post('/__fixture/reset');
  // Artwork is not part of these assertions; keep the run independent of fixture image downloads.
  await page.route(url => /^\/emby\/Items\/[^/]+\/Images\//.test(url.pathname), route => route.fulfill({ status: 404 }));
});

test('refreshes a filtered movie list without discarding loaded pages', async ({ page, request }) => {
  const ready = await mockSocket(page);
  const movies = Array.from({ length: 102 }, (_, index): MediaItem => ({
    Id: `catalog-${index}`, Type: 'Movie', Name: `Catalog ${String(index).padStart(3, '0')}`,
    ParentId: 'cn', Genres: ['Refresh test'], DateCreated: '2026-10-04T00:00:00Z',
  }));
  await updateItems(request, movies);
  await signIn(page);
  const socket = await ready();
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '电影', exact: true }).click();
  const library = page.getByRole('button', { name: '华语电影', exact: true });
  await library.click();
  const genre = page.getByRole('button', { name: 'Refresh test', exact: true });
  await genre.click();
  const sort = page.getByRole('button', { name: '排序', exact: true });
  await sort.click();
  await page.getByRole('menuitemradio', { name: '名称', exact: true }).click();
  const cards = page.locator('.poster-card');
  await expect(cards.first()).toContainText('Catalog 000');
  await page.getByRole('button', { name: '加载更多', exact: true }).click();
  await expect(cards).toHaveCount(96);
  const kept = page.getByRole('button', { name: /Catalog 070/ });
  const previous = await kept.elementHandle();
  const added = { ...movies[0], Id: 'catalog-committed', Name: 'Catalog 049 committed' };
  await updateItems(request, [...movies.filter(movie => movie.Id !== 'catalog-51'), added]);
  const refresh = page.waitForRequest(req => {
    const url = new URL(req.url());
    return url.pathname === itemsPath && url.searchParams.get('StartIndex') === '0' && Number(url.searchParams.get('Limit')) > 48;
  });
  sendChanged(socket, { ItemsAdded: [added.Id], CollectionFolders: ['cn'] });
  const refreshed = await refresh;
  expect(refreshed.headers()['x-emby-authorization']).toContain(`Token="${playerToken}"`);
  const parameters = new URL(refreshed.url()).searchParams;
  expect(parameters.get('ParentId')).toBe('cn');
  expect(parameters.get('Genres')).toBe('Refresh test');
  expect(parameters.get('SortBy')).toBe('SortName');
  await expect(page.getByRole('button', { name: /Catalog 049 committed/ })).toHaveCount(1);
  await expect(page.getByRole('button', { name: /Catalog 051/ })).toHaveCount(0);
  await expect(cards).toHaveCount(96);
  expect(await previous!.evaluate(node => node.isConnected)).toBe(true);
  await expect(library).toHaveAttribute('aria-pressed', 'true');
  await expect(genre).toHaveAttribute('aria-pressed', 'true');
  await expect(sort).toContainText('名称');
  await expect(page).toHaveURL(/#\/movies$/);
  await page.getByRole('button', { name: '加载更多', exact: true }).click();
  await expect(cards).toHaveCount(102);
});

test('refreshes favorites and search without changing their selections', async ({ page, request }) => {
  const ready = await mockSocket(page);
  const items = await fixtureItems(request);
  const rename = (Name: string) => updateItems(request, items.map(item => item.Id === 'deep' ? { ...item, Name } : item));
  await signIn(page);
  const socket = await ready();
  const navigation = page.getByRole('navigation', { name: '主导航' });
  await navigation.getByRole('button', { name: '收藏', exact: true }).click();
  const series = page.locator('.catalog-segments').getByRole('button', { name: /^剧集/ });
  await series.click();
  const cards = page.locator('.poster-card');
  await expect(cards.filter({ hasText: '深海回响' })).toHaveCount(1);
  await rename('Live search series');
  sendChanged(socket, { ItemsUpdated: ['deep'] }, 'favorite-rename');
  await expect(cards.filter({ hasText: 'Live search series' })).toHaveCount(1);
  await expect(series).toHaveAttribute('aria-pressed', 'true');

  await navigation.getByRole('button', { name: '搜索', exact: true }).click();
  const search = page.getByRole('search').getByRole('textbox');
  await search.fill('Live search');
  await expect(cards).toHaveText(/Live search series/);
  await rename('Live search updated');
  sendChanged(socket, { ItemsUpdated: ['deep'] }, 'search-rename');
  await expect(cards).toHaveText(/Live search updated/);
  await expect(search).toHaveValue('Live search');
});

test('refreshes Series metadata, seasons and episodes while retaining selected season and focus', async ({ page, request }) => {
  const ready = await mockSocket(page);
  const items = await fixtureItems(request);
  await signIn(page);
  const socket = await ready();
  await page.goto('/#/detail/deep');
  await expect.poll(() => page.locator('.stage-deck').evaluate(element => performance.now() >= Number((element as HTMLElement).dataset.lockedUntil || 0))).toBe(true);
  const pages = page.getByRole('navigation', { name: '页面分页' });
  await pages.getByRole('button', { name: '剧集', exact: true }).click();
  await page.getByRole('tab', { name: '第 1 季', exact: true }).click();
  await page.mouse.move(4, 4);
  const focusedCard = page.locator('.detail-episode-card.is-focused');
  await expect(focusedCard).toHaveAttribute('data-episode-id', 'deep-1-1');
  const focus = page.locator('[data-episode-id="deep-1-2"] .detail-episode-open');
  await page.keyboard.press('ArrowRight');
  await expect(focusedCard).toHaveAttribute('data-episode-id', 'deep-1-2');
  await expect(focus).toBeFocused();
  const added = { ...items.find(item => item.Id === 'deep-1-2')!, Id: 'newly-visible-episode', IndexNumber: 99, Name: 'Newly visible episode' };
  await updateItems(request, [...items.map(item => item.Id === 'deep' ? { ...item, Name: 'Committed Series title' }
    : item.Id === 'deep-season-1' ? { ...item, Name: 'Updated season' } : item), added]);
  // The authorized projection can contain only a previously unknown episode ID.
  sendChanged(socket, { ItemsUpdated: [added.Id] });
  await expect(page.locator('.detail-overview-page h1')).toHaveText('Committed Series title');
  await expect(page.getByRole('tab', { name: 'Updated season', exact: true })).toHaveAttribute('aria-selected', 'true');
  const addedEpisode = page.getByRole('button', { name: '播放第 99 集 Newly visible episode', exact: true });
  await expect(addedEpisode).toHaveCount(1);
  await expect(addedEpisode).toBeEnabled();
  await expect(focusedCard).toHaveAttribute('data-episode-id', 'deep-1-2');
  await expect(focus).toBeFocused();
  await expect(page).toHaveURL(/#\/detail\/deep$/);

  await updateItems(request, items.filter(item => item.Id !== 'deep-1-2'));
  sendChanged(socket, { FoldersRemovedFrom: ['deep-season-1'] }, 'episode-removed');
  await expect(page.locator('[data-episode-id="deep-1-2"]')).toHaveCount(0);
  await expect(page.getByRole('tab', { name: '第 1 季', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(focusedCard).toHaveAttribute('data-episode-id', 'deep-1-1');

  await updateItems(request, items.filter(item => item.Id !== 'deep'));
  sendChanged(socket, { FoldersRemovedFrom: ['intltv'] }, 'series-removed');
  await expect(page.getByRole('heading', { name: '影片不可用', exact: true })).toBeVisible();
  await expect(page.locator('.detail-episode-card')).toHaveCount(0);
  await expect(page).toHaveURL(/#\/detail\/deep$/);
});

test('coalesces a burst and applies the trailing catalog change during an in-flight read', async ({ page }) => {
  const ready = await mockSocket(page);
  const movie = (Name: string): MediaItem => ({ Id: 'burst-movie', Name, Type: 'Movie' });
  let phase: 'initial' | 'stale' | 'committed' = 'initial';
  let pending: Route | undefined;
  let refreshed = 0;
  await page.route(url => url.pathname === itemsPath && url.searchParams.get('Limit') === '48', async route => {
    if (phase === 'stale') { pending = route; return; }
    if (phase === 'committed') refreshed += 1;
    await route.fulfill({ json: results([movie(phase === 'initial' ? 'Existing snapshot' : 'Latest commit')]) });
  });
  await signIn(page);
  const socket = await ready();
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '电影', exact: true }).click();
  const card = page.locator('.poster-card strong');
  await expect(card).toContainText('Existing snapshot');
  phase = 'stale';
  sendChanged(socket, { ItemsUpdated: ['burst-movie'] }, 'first-commit');
  await expect.poll(() => Boolean(pending)).toBe(true);
  for (let index = 0; index < 40; index++) sendChanged(socket, { ItemsUpdated: ['burst-movie'] }, `burst-${index}`);
  sendBarrier(socket, 'Event barrier');
  await expect(page.getByRole('heading', { name: 'Event barrier', exact: true })).toBeVisible();
  await expect(card).toContainText('Existing snapshot');
  await page.evaluate(() => {
    const root = document.querySelector('.catalog-results')!;
    const track = () => { if (root.textContent?.includes('Obsolete response')) root.setAttribute('data-obsolete-displayed', 'true'); };
    const observer = new MutationObserver(track);
    observer.observe(root, { subtree: true, childList: true, characterData: true });
  });
  phase = 'committed';
  await pending!.fulfill({ json: results([movie('Obsolete response')]) });
  await expect(card).toContainText('Latest commit');
  expect(await page.locator('.catalog-results').getAttribute('data-obsolete-displayed')).toBeNull();
  expect(refreshed).toBe(1);
});

test('ignores malformed and unrelated frames, keeps good data on failure, and refreshes on reconnect', async ({ page }) => {
  const ready = await mockSocket(page);
  let count = 0;
  let fail = false;
  let name = 'Last good catalog';
  await page.route(url => url.pathname === itemsPath && url.searchParams.get('Limit') === '48', async route => {
    count += 1;
    await (fail ? route.fulfill({ status: 503 }) : route.fulfill({ json: results([{ Id: 'mock-movie', Name: name, Type: 'Movie' }]) }));
  });
  await signIn(page);
  const socket = await ready();
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '电影', exact: true }).click();
  const card = page.locator('.poster-card strong');
  await expect(card).toHaveText(name);
  const initial = count;
  await page.clock.install();
  sendChanged(socket, { ItemsUpdated: [7] }, 'invalid-id');
  sendChanged(socket, { ItemsUpdated: ['fake-movie'], IsEmpty: true }, 'empty-flag');
  sendChanged(socket, { CatalogScopes: ['fake-movie'] }, 'no-projected-ids');
  sendChanged(socket, { ItemsUpdated: ['fake-movie'], CollectionFolders: null }, 'invalid-field');
  socket.send('{"MessageType":"LibraryChanged","Data":');
  socket.send(JSON.stringify({ MessageType: 'UserDataChanged', Data: { UserId: 'fixture-viewer', UserDataList: [{ ItemId: 'mock-movie' }] } }));
  sendBarrier(socket, 'Frame barrier');
  await expect(page.getByRole('heading', { name: 'Frame barrier', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '关闭消息', exact: true }).click();
  await page.clock.fastForward(750);
  expect(count).toBe(initial);

  fail = true;
  sendChanged(socket, { ItemsUpdated: ['mock-movie'] }, 'failed-refresh');
  await expect(page.getByRole('alert')).toContainText('暂时无法刷新影片');
  await expect(card).toHaveText(name);

  fail = false;
  name = 'Fresh after reconnect';
  await socket.close({ code: 1012, reason: 'Fixture catalog resync' });
  await ready(1);
  await expect(card).toHaveText(name);
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('cancels an old catalog read on navigation and refreshes the series catalog', async ({ page }) => {
  const ready = await mockSocket(page);
  let holdMovie = false;
  let pending: Route | undefined;
  let seriesTitle = 'Existing show';
  await page.route(url => url.pathname === itemsPath && url.searchParams.get('Limit') === '48', async route => {
    const kind = new URL(route.request().url()).searchParams.get('IncludeItemTypes');
    if (kind === 'Movie' && holdMovie) { pending = route; return; }
    await route.fulfill({ json: results(kind === 'Movie' ? [{ Id: 'old-movie', Type: 'Movie', Name: 'Existing movie' }]
      : [{ Id: 'show', Type: 'Series', Name: seriesTitle }]) });
  });
  await signIn(page);
  const socket = await ready();
  const navigation = page.getByRole('navigation', { name: '主导航' });
  await navigation.getByRole('button', { name: '电影', exact: true }).click();
  const card = page.locator('.poster-card strong');
  await expect(card).toHaveText('Existing movie');
  holdMovie = true;
  sendChanged(socket, { ItemsUpdated: ['old-movie'] });
  await expect.poll(() => Boolean(pending)).toBe(true);
  const cancelled = page.waitForEvent('requestfailed', { predicate: req => req === pending!.request() });
  await navigation.getByRole('button', { name: '剧集', exact: true }).click();
  await expect(card).toHaveText('Existing show');
  await cancelled;
  await pending!.fulfill({ json: results([{ Id: 'old-movie', Type: 'Movie', Name: 'Obsolete movie' }]) }).catch(() => undefined);

  seriesTitle = 'Fresh show';
  sendChanged(socket, { ItemsUpdated: ['new-show-episode'] }, 'series-changed');
  await expect(card).toHaveText(seriesTitle);
  await expect(page).toHaveURL(/#\/series$/);
});

test('keeps the active media element playing when a library notification arrives', async ({ page, request }) => {
  const ready = await mockSocket(page);
  await signIn(page);
  const socket = await ready();
  await page.goto('/#/player/deep-2-5');
  await page.waitForFunction(() => {
    const video = document.querySelector<HTMLVideoElement>('.player-video');
    return video && !video.paused && video.currentTime > 0;
  });
  const video = await page.locator('.player-video').elementHandle();
  const before = await video!.evaluate((element: HTMLVideoElement) => ({ time: element.currentTime, src: element.currentSrc }));
  sendChanged(socket, { ItemsUpdated: ['deep-2-5'] });
  sendBarrier(socket, 'Playback barrier');
  await expect(page.getByRole('heading', { name: 'Playback barrier', exact: true })).toBeVisible();
  await expect.poll(() => video!.evaluate((element: HTMLVideoElement) => element.currentTime)).toBeGreaterThan(before.time + .4);
  expect(await video!.evaluate((element: HTMLVideoElement) => ({ connected: element.isConnected, src: element.currentSrc, paused: element.paused }))).toEqual({ connected: true, src: before.src, paused: false });
  await expect(page).toHaveURL(/#\/player\/deep-2-5$/);
  const fixture = await (await request.get('/__fixture/state')).json();
  expect(fixture.requests.filter((entry: { path: string }) => entry.path === '/Items/deep-2-5/PlaybackInfo')).toHaveLength(1);
  expect(fixture.events.filter((entry: { route: string }) => entry.route === '/Sessions/Playing/Stopped')).toHaveLength(0);
});
