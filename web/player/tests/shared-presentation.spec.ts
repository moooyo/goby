import { expect, test, type Page } from '@playwright/test';

const panel = (page: Page) => page.locator('.deck-panel[data-active="true"]');

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

test.beforeEach(async ({ request }) => { await request.post('/__fixture/reset'); });

async function openCast(page: Page) {
  await signIn(page);
  await page.goto('/#/detail/canyon');
  await expect(page.getByRole('heading', { name: '回声谷', exact: true })).toBeVisible();
  await expect.poll(() => page.locator('.stage-deck').evaluate(element => performance.now() >= Number((element as HTMLElement).dataset.lockedUntil))).toBe(true);
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: '演职员', exact: true }).click();
  await expect(panel(page)).toHaveAttribute('aria-label', '演职员');
  await expect.poll(() => page.locator('.stage-deck').evaluate(element => performance.now() >= Number((element as HTMLElement).dataset.lockedUntil))).toBe(true);
}

test('lets the empty part of a non-overflowing cast row turn the page', async ({ page }) => {
  await openCast(page);
  const cast = panel(page).locator('.detail-cast-rail');
  expect(await cast.evaluate(element => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(2);
  const box = (await cast.boundingBox())!;
  await page.mouse.move(box.x + box.width * .75, box.y + box.height / 2);
  await page.mouse.wheel(0, 120);
  await expect(panel(page)).toHaveAttribute('aria-label', '媒体信息');
});

test('scrolls overflowing page content before turning from a non-overflowing cast row', async ({ page }) => {
  await page.setViewportSize({ width: 1000, height: 420 });
  await openCast(page);
  const scroll = panel(page).locator('.stage-scroll');
  expect(await scroll.evaluate(element => element.scrollHeight - element.clientHeight)).toBeGreaterThan(20);
  const cast = panel(page).locator('.detail-cast-rail');
  expect(await cast.evaluate(element => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(2);
  const box = (await cast.boundingBox())!;
  await page.mouse.move(box.x + box.width * .8, box.y + box.height / 2);
  await page.mouse.wheel(0, 80);
  await expect.poll(() => scroll.evaluate(element => element.scrollTop)).toBeGreaterThan(0);
  await expect(panel(page)).toHaveAttribute('aria-label', '演职员');
});

test('preserves native horizontal gestures without redirecting vertical wheels into a rail', async ({ page }) => {
  await openCast(page);
  const rail = panel(page).locator('.detail-similar-rail');
  const box = (await rail.boundingBox())!;
  await page.mouse.move(box.x + 16, box.y + box.height / 2);
  await page.mouse.wheel(300, 0);
  await expect.poll(() => rail.evaluate(element => element.scrollLeft)).toBeGreaterThan(0);
  await expect(panel(page)).toHaveAttribute('aria-label', '演职员');
  await page.mouse.wheel(0, 120);
  await expect(panel(page)).toHaveAttribute('aria-label', '媒体信息');
});

test('accepts physical line and page wheel steps without leaving prompts over the episode rail', async ({ page }) => {
  await signIn(page);
  await expect(panel(page)).toHaveAttribute('aria-label', '精选');
  await expect(page.locator('.next-page')).toBeVisible();
  await page.waitForTimeout(1150);
  const accent = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--accent'));
  await page.locator('.stage-deck').evaluate(element => element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, cancelable: true, deltaY: 3, deltaMode: 1 })));
  await expect(panel(page)).toHaveAttribute('aria-label', '继续观看');
  await expect(page.locator('.next-page')).toHaveCount(0);
  await page.waitForTimeout(1150);
  expect(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--accent'))).toBe(accent);
  const colors = await panel(page).locator('.card-progress > span').evaluateAll(elements => elements.map(element => getComputedStyle(element).backgroundColor));
  expect(colors.length).toBeGreaterThan(0);
  expect(colors.every(color => color === accent.trim())).toBe(true);
  await page.locator('.stage-deck').evaluate(element => element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, cancelable: true, deltaY: 1, deltaMode: 2 })));
  await expect(panel(page)).toHaveAttribute('aria-label', '最新电影');
  await expect(page.locator('.next-page')).toHaveCount(0);
  await panel(page).locator('.stage-rail').hover();
  await expect(page.locator('.page-rail')).toHaveCSS('pointer-events', 'none');
  await page.locator('.page-rail button').first().focus();
  await expect(page.locator('.page-rail')).toHaveCSS('pointer-events', 'auto');
});

test('keeps mobile navigation at the handoff size with transparent tools and no added active dot', async ({ page }) => {
  await page.setViewportSize({ width: 393, height: 852 });
  await signIn(page);
  const layout = await page.locator('.top-nav').evaluate(element => {
    const tabs = Array.from(element.querySelectorAll<HTMLElement>('.nav-tab'));
    return tabs.map(tab => {
      const box = tab.getBoundingClientRect(), style = getComputedStyle(tab);
      return { x: box.x, y: box.y, width: box.width, height: box.height, weight: style.fontWeight, dot: getComputedStyle(tab, '::after').content };
    });
  });
  expect(layout).toHaveLength(4);
  for (const [index, tab] of layout.entries()) {
    expect(tab.x).toBeCloseTo(13 + index * 91.75, 1);
    expect(tab.y).toBeCloseTo(777, 1);
    expect(tab.width).toBeCloseTo(91.75, 1);
    expect(tab.height).toBeCloseTo(62, 1);
    expect(tab.weight).toBe('700');
    expect(['none', 'normal']).toContain(tab.dot);
  }
  const search = page.locator('.mobile-tools button').first();
  await expect(search).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  expect((await search.boundingBox())!.x).toBe(293);
});

test('shows collection-specific poster metadata and activates the header scrim only after scrolling', async ({ page, request }) => {
  const state = await (await request.get('/__fixture/state')).json();
  const target = state.items.find((item: { Id: string }) => item.Id === 'guitu');
  target.UserData.IsFavorite = true;
  target.UserData.Played = false;
  target.UserData.PlaybackPositionTicks = target.RunTimeTicks / 2;
  await request.post('/__fixture/config', { data: { items: state.items } });
  await signIn(page);
  await page.goto('/#/favorites');
  const favorite = page.locator('.poster-card').filter({ has: page.locator('strong', { hasText: '归途' }) });
  await expect(favorite.locator('small')).toContainText('电影 ·');
  await expect(favorite.locator('.card-progress')).toHaveCount(1);
  await expect(page.locator('.poster-grid .watched-mark')).toHaveCount(0);
  await expect(page.locator('.top-scrim')).not.toHaveClass(/is-visible/);
  await page.goto('/#/movies');
  await expect(page.locator('.poster-card').first()).toBeVisible();
  await page.mouse.wheel(0, 600);
  await expect(page.locator('.top-scrim')).toHaveClass(/is-visible/);
  await page.goto('/#/search');
  await page.getByPlaceholder('搜索片名、演员、导演、类型或年份').fill('归途');
  const found = page.locator('.poster-card').filter({ has: page.locator('strong', { hasText: '归途' }) });
  await expect(found.locator('small')).toContainText('电影 ·');
  await expect(found.locator('.card-progress,.watched-mark')).toHaveCount(0);
});
