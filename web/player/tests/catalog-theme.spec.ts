import { expect, test, type Page } from '@playwright/test';

const emptyThemes = { ThemeVideosResult: { OwnerId: '', Items: [], TotalRecordCount: 0 }, ThemeSongsResult: { OwnerId: '', Items: [], TotalRecordCount: 0 }, SoundtrackSongsResult: { OwnerId: '', Items: [], TotalRecordCount: 0 } };
const preview = { Id: 'theme-preview', Name: 'Theme preview', Type: 'Video', MediaSources: [{ Id: 'theme-preview-source', Container: 'webm', SupportsDirectPlay: true, Bitrate: 300_000, MediaStreams: [{ Index: 0, Type: 'Video', Codec: 'vp8', Width: 320, Height: 180, BitDepth: 8, VideoRangeType: 'SDR' }, { Index: 1, Type: 'Audio', Codec: 'opus', IsDefault: true }] }] };

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

test.beforeEach(async ({ request }) => { await request.post('/__fixture/reset'); });

test('pages mixed hints by received results and preserves colliding item and entity IDs', async ({ page }) => {
  const offsets: number[] = [];
  let failedSecondPage = false;
  const hints = Array.from({ length: 48 }, (_, index) => ({ Id: index ? String(index) : 'deep', ItemId: index ? String(index) : 'deep', Name: index === 47 ? '同名测试 | 类型' : index ? `同名测试 ${index}` : '同名测试', MatchedTerm: '同名测试', Type: index === 47 ? 'Genre' : 'Person', GobyReference: { Kind: 'Entity', Id: index ? String(index) : 'deep' } }));
  const movie = { Id: 'deep', ItemId: 'deep', Name: '同名测试', MatchedTerm: '同名测试', Type: 'Series', GobyReference: { Kind: 'Item', Id: 'deep' } };
  await page.route('**/emby/Search/Hints?*', async route => {
    const query = new URL(route.request().url()).searchParams;
    const offset = Number(query.get('StartIndex'));
    offsets.push(offset);
    expect(query.get('UserId')).toBe('fixture-viewer');
    expect(query.get('IncludeItemTypes')).toBe('Movie,Series,Person,Genre');
    expect(query.get('SearchTerm')).toBe('同名测试');
    if (offset === 48 && !failedSecondPage) {
      failedSecondPage = true;
      await route.fulfill({ status: 503, json: { ResponseStatus: { ErrorCode: 'unavailable' } } });
      return;
    }
    await route.fulfill({ json: { SearchHints: offset === 0 ? hints : [movie], TotalRecordCount: 49 } });
  });
  await signIn(page);
  await page.getByRole('button', { name: '搜索', exact: true }).click();
  await page.getByLabel('搜索片名、演员、导演、类型或年份', { exact: true }).fill('同名测试');
  await expect(page.locator('.catalog-search-count')).toContainText('49 条结果');
  await expect(page.locator('.catalog-people button')).toHaveCount(48);
  await expect(page.locator('.poster-card')).toHaveCount(0);
  await page.getByRole('button', { name: '加载更多', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('暂时无法加载影片');
  await page.getByRole('button', { name: '重新加载', exact: true }).click();
  await expect(page.locator('.poster-card')).toHaveCount(1);
  await expect(page.locator('.catalog-people button')).toHaveCount(48);
  expect(offsets).toEqual([0, 48, 48]);
  await expect(page.getByRole('button', { name: '加载更多', exact: true })).toHaveCount(0);
  const personQuery = page.waitForRequest(request => new URL(request.url()).searchParams.get('PersonIds') === 'deep');
  await page.getByRole('button', { name: '同名测试，演职员作品', exact: true }).click();
  await personQuery;
  await expect(page).toHaveURL(/#\/search$/);
  await expect(page.locator('.catalog-search-count')).toContainText('同名测试的作品');
  await page.getByRole('button', { name: '清除筛选', exact: true }).click();
  const genreQuery = page.waitForRequest(request => new URL(request.url()).searchParams.get('GenreIds') === '47');
  await page.getByRole('button', { name: '同名测试 | 类型，类型影片', exact: true }).click();
  expect(new URL((await genreQuery).url()).searchParams.has('Genres')).toBe(false);
  await expect(page.locator('.catalog-search-count')).toContainText('同名测试 | 类型 ·');
});

test('sends quality filters to the server and uses the filtered server total', async ({ page, request }) => {
  const state = await (await request.get('/__fixture/state')).json();
  const movie = state.items.find((item: { Type: string }) => item.Type === 'Movie');
  const movieCount = state.items.filter((item: { Type: string }) => item.Type === 'Movie').length;
  await page.route('**/emby/Users/fixture-viewer/Items?*', async route => {
    const query = new URL(route.request().url()).searchParams;
    if (query.has('Is4K') || query.has('ExtendedVideoTypes')) await route.fulfill({ json: { Items: [movie], TotalRecordCount: query.get('ExtendedVideoTypes') === 'HyperLogGamma' ? 7 : query.get('ExtendedVideoTypes') === 'DolbyVision' ? 9 : 1 } });
    else await route.continue();
  });
  await signIn(page);
  const movieGenres = page.waitForRequest(request => new URL(request.url()).pathname === '/emby/Genres');
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '电影', exact: true }).click();
  expect(new URL((await movieGenres).url()).searchParams.get('IncludeItemTypes')).toBe('Movie');
  await expect(page.locator('.catalog-count')).toHaveText(`${movieCount} 部影片`);
  await page.getByRole('button', { name: '筛选', exact: true }).click();
  const filters = page.getByRole('dialog', { name: '筛选', exact: true });
  const fourK = page.waitForRequest(request => new URL(request.url()).searchParams.get('Is4K') === 'true');
  await filters.getByRole('button', { name: '4K', exact: true }).click();
  expect(new URL((await fourK).url()).searchParams.has('GobyAggregateVideoFilters')).toBe(false);
  await expect(page.locator('.catalog-count')).toHaveText(`1 / ${movieCount} 部影片`);
  const hdr = page.waitForRequest(request => new URL(request.url()).searchParams.has('ExtendedVideoTypes'));
  await filters.getByRole('button', { name: 'HDR', exact: true }).click();
  const hdrQuery = new URL((await hdr).url()).searchParams;
  expect(hdrQuery.has('Is4K')).toBe(false);
  expect(hdrQuery.get('ExtendedVideoTypes')).toBe('Hdr10,Hdr10Plus');
  await expect(filters.getByRole('button', { name: 'HDR', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('.poster-card')).toHaveCount(1);
  for (const [label, range] of [['HLG', 'HyperLogGamma'], ['Dolby Vision', 'DolbyVision']]) {
    const request = page.waitForRequest(request => new URL(request.url()).searchParams.get('ExtendedVideoTypes') === range);
    await filters.getByRole('button', { name: label, exact: true }).click();
    const query = new URL((await request).url()).searchParams;
    expect(query.has('Is4K')).toBe(false);
    expect(query.has('GobyAggregateVideoFilters')).toBe(false);
    await expect(filters.getByRole('button', { name: label, exact: true })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('.catalog-count')).toHaveText(`${range === 'HyperLogGamma' ? 7 : 9} / ${movieCount} 部影片`);
  }
  const clearedFilters = page.waitForRequest(request => {
    const url = new URL(request.url());
    return url.pathname === '/emby/Users/fixture-viewer/Items' && url.searchParams.get('IncludeItemTypes') === 'Movie'
      && url.searchParams.get('Limit') === '48' && !url.searchParams.has('Is4K') && !url.searchParams.has('ExtendedVideoTypes');
  });
  await filters.getByRole('button', { name: '清除筛选', exact: true }).click();
  await clearedFilters;
  await expect(page.locator('.catalog-count')).toHaveText(`${movieCount} 部影片`);
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '剧集', exact: true }).click();
  await page.getByRole('button', { name: '筛选', exact: true }).click();
  const seriesFilter = page.waitForRequest(request => new URL(request.url()).searchParams.get('GobyAggregateVideoFilters') === 'true');
  await page.getByRole('dialog', { name: '筛选', exact: true }).getByRole('button', { name: '4K', exact: true }).click();
  const seriesQuery = new URL((await seriesFilter).url()).searchParams;
  expect(seriesQuery.get('IncludeItemTypes')).toBe('Series');
  expect(seriesQuery.get('Is4K')).toBe('true');
});

test('uses independent year and quality browsing without filtering a page of mixed hints', async ({ page, request }) => {
  const state = await (await request.get('/__fixture/state')).json();
  const movie = state.items.find((item: { Type: string }) => item.Type === 'Movie');
  const genreIds = new Set(state.items.filter((item: { Type: string }) => ['Movie', 'Series'].includes(item.Type))
    .flatMap((item: { Genres?: string[] }) => (item.Genres ?? []).map(name => `genre-${name}`)));
  const genreSummaryIds: string[] = [];
  const offsets: number[] = [];
  await page.route('**/emby/Users/fixture-viewer/Items?*', async route => {
    const query = new URL(route.request().url()).searchParams;
    if (query.get('GenreIds') && query.get('Limit') === '1') {
      // Summary totals must come from the genre-scoped API response, even
      // though only one representative item is requested for each tile.
      const genreId = query.get('GenreIds')!;
      expect(genreIds.has(genreId)).toBe(true);
      expect(query.has('Genres')).toBe(false);
      expect(query.get('IncludeItemTypes')).toBe('Movie,Series');
      expect(query.get('Recursive')).toBe('true');
      expect(query.has('ParentId')).toBe(false);
      expect(query.has('SearchTerm')).toBe(false);
      expect(query.has('Years')).toBe(false);
      expect(query.has('Is4K')).toBe(false);
      expect(query.has('ExtendedVideoTypes')).toBe(false);
      genreSummaryIds.push(genreId);
      await route.fulfill({ json: { Items: [movie], TotalRecordCount: 128 } });
      return;
    }
    if (query.get('Years') === '2024' && query.get('ExtendedVideoTypes') === 'DolbyVision') {
      expect(query.has('SearchTerm')).toBe(false);
      expect(query.get('GobyAggregateVideoFilters')).toBe('true');
      expect(query.get('IncludeItemTypes')).toBe('Movie,Series');
      const offset = Number(query.get('StartIndex'));
      offsets.push(offset);
      await route.fulfill({ json: { Items: Array.from({ length: offset === 0 ? 48 : 1 }, (_, index) => ({ ...movie, Id: `year-quality-${offset + index}`, Name: `Year quality ${offset + index}` })), TotalRecordCount: 49 } });
      return;
    }
    await route.continue();
  });
  await page.route('**/emby/Search/Hints?*', route => route.fulfill({ json: { SearchHints: [{ Id: movie.Id, ItemId: movie.Id, Name: movie.Name, Type: 'Movie', GobyReference: { Kind: 'Item', Id: movie.Id } }], TotalRecordCount: 1 } }));
  await signIn(page);
  await page.getByRole('button', { name: '搜索', exact: true }).click();
  await expect(page.locator('.catalog-genre-title small').first()).toHaveText('128 部');
  expect(new Set(genreSummaryIds).size).toBe(genreIds.size);
  const search = page.getByLabel('搜索片名、演员、导演、类型或年份', { exact: true });
  await search.fill('混合名称');
  await expect(page.locator('.catalog-search-scope')).toHaveText('名称搜索');
  await page.getByRole('button', { name: '年份筛选', exact: true }).click();
  const yearDialog = page.getByRole('dialog', { name: '年份筛选', exact: true });
  await expect(yearDialog).toContainText('退出名称搜索');
  await yearDialog.getByLabel('上映年份', { exact: true }).fill('2024');
  const yearRequest = page.waitForRequest(request => new URL(request.url()).searchParams.get('Years') === '2024');
  await yearDialog.getByRole('button', { name: '应用', exact: true }).click();
  expect(new URL((await yearRequest).url()).searchParams.has('SearchTerm')).toBe(false);
  await expect(search).toHaveValue('');
  await expect(page.locator('.catalog-search-scope')).toHaveText('影片筛选');
  await page.getByRole('button', { name: '画质筛选', exact: true }).click();
  await page.getByRole('dialog', { name: '画质筛选', exact: true }).getByRole('button', { name: 'Dolby Vision', exact: true }).click();
  await expect(page.locator('.catalog-search-count')).toContainText('2024 年 · Dolby Vision · 49 部影片');
  await expect(page.locator('.poster-card')).toHaveCount(48);
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '加载更多', exact: true }).click();
  await expect(page.locator('.poster-card')).toHaveCount(49);
  expect(offsets).toEqual([0, 48]);
  await expect(page.getByRole('button', { name: '加载更多', exact: true })).toHaveCount(0);
  await search.fill('再次搜索');
  await expect(page.locator('.catalog-search-scope')).toHaveText('名称搜索');
  await expect(page.getByRole('button', { name: '年份筛选', exact: true })).toHaveText('年份');
  await expect(page.getByRole('button', { name: '画质筛选', exact: true })).toHaveText('画质');
});

test('plays existing theme media quietly, loops through stills, and releases it on navigation', async ({ page, request }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const themeQueries: URLSearchParams[] = [];
  await page.route('**/emby/Items/*/ThemeMedia?*', async route => {
    themeQueries.push(new URL(route.request().url()).searchParams);
    await route.fulfill({ json: { ...emptyThemes, ThemeVideosResult: { OwnerId: 'deep', Items: [preview], TotalRecordCount: 1 } } });
  });
  await signIn(page);
  const video = page.locator('.background-preview');
  await expect(video).toHaveClass(/is-visible/);
  await expect(video).toHaveAttribute('data-preview-owner', 'deep');
  const playback = await video.evaluate((element: HTMLVideoElement) => ({ muted: element.muted, paused: element.paused, source: element.currentSrc }));
  expect(playback.muted).toBe(true);
  expect(playback.paused).toBe(false);
  expect(new URL(playback.source).searchParams.get('Static')).toBe('true');
  expect(new URL(playback.source).searchParams.has('PlaySessionId')).toBe(false);
  expect(themeQueries[0].get('Fields')).toBe('MediaSources,MediaStreams');
  expect(themeQueries[0].get('InheritFromParent')).toBe('true');
  await video.evaluate((element: HTMLVideoElement) => { element.currentTime = element.duration - .1; });
  await expect(video).not.toHaveClass(/is-visible/);
  await expect(video).toHaveClass(/is-visible/);
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '电影', exact: true }).click();
  await expect(video).not.toHaveAttribute('src');
  expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true);
  const state = await (await request.get('/__fixture/state')).json();
  expect(state.events).toEqual([]);
  expect(state.requests.some((entry: { path: string }) => entry.path.endsWith('/PlaybackInfo'))).toBe(false);
});

test('uses a compatible local trailer when theme media needs transcoding', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const incompatible = { ...preview, MediaSources: [{ ...preview.MediaSources[0], Container: 'mkv', MediaStreams: [{ Index: 0, Type: 'Video', Codec: 'hevc', BitDepth: 10, VideoRangeType: 'HDR10' }] }] };
  await page.route('**/emby/Items/*/ThemeMedia?*', route => route.fulfill({ json: { ...emptyThemes, ThemeVideosResult: { OwnerId: 'deep', Items: [incompatible], TotalRecordCount: 1 } } }));
  await page.route('**/emby/Users/fixture-viewer/Items/*/LocalTrailers?*', route => route.fulfill({ json: [preview] }));
  await signIn(page);
  await expect(page.locator('.background-preview')).toHaveClass(/is-visible/);
  await expect(page.locator('.background-preview')).toHaveAttribute('src', /Videos\/theme-preview\/stream\?/);
});

test('keeps stills on small screens and when reduced motion is enabled', async ({ page }) => {
  let requests = 0;
  await page.route('**/emby/Items/*/ThemeMedia?*', async route => { requests++; await route.fulfill({ json: emptyThemes }); });
  await signIn(page);
  await page.waitForTimeout(1400);
  expect(requests).toBe(0);
  await expect(page.locator('.background-preview')).not.toHaveAttribute('src');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.waitForTimeout(1400);
  expect(requests).toBe(0);
  await expect(page.locator('.background-preview')).not.toHaveAttribute('src');
});

test('falls back after a media failure and does not retry that preview', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  let attempts = 0;
  await page.route('**/emby/Items/*/ThemeMedia?*', route => route.fulfill({ json: { ...emptyThemes, ThemeVideosResult: { OwnerId: 'deep', Items: [preview], TotalRecordCount: 1 } } }));
  await page.route('**/emby/Videos/theme-preview/stream?*', async route => { attempts++; await route.abort('failed'); });
  await signIn(page);
  await expect.poll(() => attempts).toBe(1);
  await expect(page.locator('.background-preview')).not.toHaveAttribute('src');
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '电影', exact: true }).click();
  await expect(page.locator('.catalog-title,.catalog-libraries')).toBeVisible();
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '首页', exact: true }).click();
  await page.waitForTimeout(1400);
  expect(attempts).toBe(1);
  await expect(page.locator('.background-preview')).not.toHaveAttribute('src');
});
