import { expect, test, type Page } from '@playwright/test';

const emptyThemes = { ThemeVideosResult: { OwnerId: '', Items: [], TotalRecordCount: 0 }, ThemeSongsResult: { OwnerId: '', Items: [], TotalRecordCount: 0 }, SoundtrackSongsResult: { OwnerId: '', Items: [], TotalRecordCount: 0 } };
const theme = { Id: 'handoff-theme', Name: 'Theme preview', Type: 'Video', MediaSources: [{ Id: 'handoff-theme-source', Container: 'webm', SupportsDirectPlay: true, Bitrate: 300_000, MediaStreams: [{ Index: 0, Type: 'Video', Codec: 'vp8', Width: 640, Height: 360, BitDepth: 8, VideoRangeType: 'SDR' }, { Index: 1, Type: 'Audio', Codec: 'opus', IsDefault: true }] }] };
const withTheme = { ...emptyThemes, ThemeVideosResult: { OwnerId: 'deep', Items: [theme], TotalRecordCount: 1 } };

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function waitForDeck(page: Page) {
  await expect.poll(() => page.locator('.stage-deck').evaluate(element => performance.now() >= Number((element as HTMLElement).dataset.lockedUntil || 0))).toBe(true);
}

test.beforeEach(async ({ request, page }) => {
  await request.post('/__fixture/reset');
  await page.emulateMedia({ reducedMotion: 'no-preference' });
});

test('retains the outgoing video until its opacity fade finishes on navigation', async ({ page }) => {
  await page.route('**/emby/Items/*/ThemeMedia?*', route => route.fulfill({ json: withTheme }));
  await signIn(page);
  const video = page.locator('.background-preview');
  await expect(video).toHaveClass(/is-visible/);
  await expect.poll(() => video.evaluate(element => getComputedStyle(element).opacity)).toBe('1');
  await video.evaluate(element => {
    const state = { fadedAt: 0, removedAt: 0, sourceAtFade: '', opacityAtFade: '' };
    (window as typeof window & { previewFade?: typeof state }).previewFade = state;
    const observer = new MutationObserver(records => {
      for (const record of records) {
        if (record.attributeName === 'class' && !element.classList.contains('is-visible') && !state.fadedAt) {
          state.fadedAt = performance.now();
          state.sourceAtFade = element.getAttribute('src') || '';
          state.opacityAtFade = getComputedStyle(element).opacity;
        }
        if (record.attributeName === 'src' && !element.hasAttribute('src')) {
          state.removedAt = performance.now();
          observer.disconnect();
        }
      }
    });
    observer.observe(element, { attributes: true, attributeFilter: ['class', 'src'] });
  });
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '电影', exact: true }).click();
  await expect(video).not.toHaveAttribute('src');
  const fade = await page.evaluate(() => (window as typeof window & { previewFade: { fadedAt: number; removedAt: number; sourceAtFade: string; opacityAtFade: string } }).previewFade);
  expect(fade.sourceAtFade).toContain('/Videos/handoff-theme/stream?');
  expect(fade.removedAt - fade.fadedAt).toBeGreaterThanOrEqual(590);
  expect(Number(fade.opacityAtFade)).toBeGreaterThan(0);
  expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true);
});

test('includes the departing video in depth motion without retaining its transform', async ({ page }) => {
  await page.route('**/emby/Items/*/ThemeMedia?*', route => route.fulfill({ json: withTheme }));
  await signIn(page);
  await page.goto('/#/detail/deep');
  const video = page.locator('.background-preview');
  await expect(video).toHaveClass(/is-visible/);
  await waitForDeck(page);
  await page.evaluate(() => {
    document.addEventListener('goby:page', event => {
      if (!(event as CustomEvent<{ direction: number }>).detail.direction) return;
      const video = document.querySelector<HTMLVideoElement>('.background-preview')!;
      const animations = video.getAnimations().filter(animation => animation.id === 'goby-depth');
      (window as typeof window & { previewDepth?: unknown }).previewDepth = {
        count: animations.length,
        duration: animations[0]?.effect?.getTiming().duration,
        frames: (animations[0]?.effect as KeyframeEffect)?.getKeyframes().map(frame => frame.transform),
        source: video.getAttribute('src'),
      };
    });
  });
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: '剧集', exact: true }).click();
  const depth = await page.evaluate(() => (window as typeof window & { previewDepth: { count: number; duration: number; frames: string[]; source: string } }).previewDepth);
  expect(depth.count).toBe(1);
  expect(depth.duration).toBe(1000);
  expect(depth.frames).toEqual(['scale(1)', 'scale(1.16)']);
  expect(depth.source).toContain('/Videos/handoff-theme/stream?');
  await expect(video).not.toHaveAttribute('data-preview-owner', 'deep');
  await expect(video).toHaveClass(/is-visible/);
  expect(await video.evaluate(element => ({ depth: element.getAnimations().filter(animation => animation.id === 'goby-depth').length, transform: getComputedStyle(element).transform }))).toEqual({ depth: 0, transform: 'none' });
});

test('enables audio only by request and keeps generated previews silent', async ({ page, request }) => {
  let generatedOnly = false;
  await page.route('**/emby/Items/*/ThemeMedia?*', route => route.fulfill({ json: generatedOnly ? emptyThemes : withTheme }));
  await page.route('**/emby/Items/*/BackgroundPreview?*', route => route.fulfill({ json: { Available: true, StreamUrl: '/emby/Items/deep/BackgroundPreview/stream.mp4', RunTimeTicks: 30_000_000, Width: 320, Height: 180 } }));
  await page.route('**/emby/Items/deep/BackgroundPreview/stream.mp4?*', async route => {
    const response = await route.fetch({ url: new URL('/__fixture/background.mp4', route.request().url()).href });
    await route.fulfill({ response });
  });
  await signIn(page);
  await page.goto('/#/detail/deep');
  await waitForDeck(page);
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: '剧集', exact: true }).click();
  const video = page.locator('.background-preview');
  await expect(video).toHaveClass(/is-visible/);
  await expect(page.getByRole('button', { name: '开启预览声音', exact: true })).toBeVisible();
  expect(await video.evaluate((element: HTMLVideoElement) => element.muted)).toBe(true);
  await page.getByRole('button', { name: '开启预览声音', exact: true }).click();
  await expect(page.getByRole('button', { name: '关闭预览声音', exact: true })).toHaveAttribute('aria-pressed', 'true');
  expect(await video.evaluate((element: HTMLVideoElement) => element.muted)).toBe(false);
  generatedOnly = true;
  const next = page.locator('.detail-episode-card:not(.is-focused) .detail-episode-open').first();
  await next.focus();
  await expect(video).toHaveAttribute('data-preview-kind', 'generated');
  await expect(video).toHaveClass(/is-visible/);
  await expect(page.locator('.preview-sound-toggle')).toHaveCount(0);
  expect(await video.evaluate((element: HTMLVideoElement) => element.muted)).toBe(true);
  const state = await (await request.get('/__fixture/state')).json();
  expect(state.events).toEqual([]);
  expect(state.requests.some((entry: { path: string }) => /PlaybackInfo|task-runs|media-analysis/.test(entry.path))).toBe(false);
});

test('uses the handoff hero duration and preserves bounded artwork lightness', async ({ page }) => {
  await page.route('**/emby/Items/*/ThemeMedia?*', route => route.fulfill({ json: emptyThemes }));
  await page.route('**/emby/Items/*/Images/Primary?*', route => {
    const color = new URL(route.request().url()).pathname.includes('/deep/') ? '#ff8080' : '#800000';
    return route.fulfill({ contentType: 'image/svg+xml', body: `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><rect width="32" height="32" fill="${color}"/></svg>` });
  });
  await signIn(page);
  await expect(page.locator('.background-stage')).toHaveClass(/is-hero/);
  const background = page.locator('.background-layer').last();
  await expect(background).toBeAttached();
  const motion = await background.evaluate(element => element.getAnimations().find(animation => animation instanceof CSSAnimation && animation.animationName === 'hero-ken-burns')?.effect?.getTiming());
  expect(motion?.duration).toBe(10_000);
  await expect.poll(() => page.evaluate(() => document.documentElement.style.getPropertyValue('--accent'))).toMatch(/ 70%\)$/);
  await page.locator('.hero-thumbnails button').nth(1).click();
  await expect.poll(() => page.evaluate(() => document.documentElement.style.getPropertyValue('--accent'))).toMatch(/ 60%\)$/);
});
