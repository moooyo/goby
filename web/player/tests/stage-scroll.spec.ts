import { expect, test, type Locator, type Page } from '@playwright/test';

const active = (page: Page) => page.locator('.deck-panel[data-active="true"]');
const unlocked = async (page: Page) => {
  await expect.poll(() => page.locator('.stage-deck').evaluate(element => performance.now() >= Number((element as HTMLElement).dataset.lockedUntil))).toBe(true);
};
async function openPanel(page: Page, path: string, label: string) {
  await page.goto(`/#/${path}`);
  await expect(page.locator('.stage-deck')).toBeVisible();
  await unlocked(page);
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: label, exact: true }).click();
  await expect(active(page)).toHaveAttribute('aria-label', label);
  await unlocked(page);
}
async function wheelOver(page: Page, target: Locator, deltaY: number) {
  const box = (await target.boundingBox())!;
  await page.mouse.move(box.x + Math.min(20, box.width / 2), box.y + box.height / 2);
  await page.mouse.wheel(0, deltaY);
}

test.beforeEach(async ({ page, request }) => {
  await request.post('/__fixture/reset');
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
});

for (const scenario of [
  { path: 'home', page: '继续观看', target: '.stage-rail-cards', next: '最新电影' },
  { path: 'home', page: '最新电影', target: '.stage-rail-cards', next: '最新剧集' },
  { path: 'detail/deep', page: '剧集', target: '.detail-episode-rail', next: '演职员' },
  { path: 'detail/deep', page: '剧集', target: '.detail-seasons', next: '演职员' },
]) {
  test(`keeps vertical wheels vertical on ${scenario.path} ${scenario.page} ${scenario.target}`, async ({ page }) => {
    await openPanel(page, scenario.path, scenario.page);
    const rail = active(page).locator(scenario.target);
    if (scenario.target === '.stage-rail-cards') {
      await active(page).locator('.stage-rail').hover();
      await expect(active(page).locator('.stage-rail')).toHaveClass(/is-expanded/);
    } else {
      await active(page).locator('.detail-episode-strip').hover();
      await expect(active(page).locator('.detail-episode-strip')).toHaveClass(/is-expanded/);
    }
    await wheelOver(page, rail, 120);
    await expect(active(page)).toHaveAttribute('aria-label', scenario.next);
  });
}

test('lets small deliberate wheel steps page without requiring a large detent', async ({ page }) => {
  await unlocked(page);
  await page.mouse.move(950, 300);
  await page.mouse.wheel(0, 8);
  await expect(active(page)).toHaveAttribute('aria-label', '继续观看');
});

test('leaves zoom, shift and horizontal wheel defaults untouched', async ({ page }) => {
  await openPanel(page, 'detail/canyon', '演职员');
  const result = await active(page).locator('.detail-similar-rail').evaluate(element => [
    { deltaY: 120, ctrlKey: true }, { deltaY: 120, metaKey: true },
    { deltaY: 120, shiftKey: true }, { deltaX: 120, deltaY: 0 },
  ].map(init => {
    const event = new WheelEvent('wheel', { bubbles: true, cancelable: true, ...init });
    return element.dispatchEvent(event);
  }));
  expect(result).toEqual([true, true, true, true]);
  await expect(active(page)).toHaveAttribute('aria-label', '演职员');
});

test('routes a wheel over the central play overlay to overflowing detail content first', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 520 });
  await page.goto('/#/detail/deep');
  await expect(active(page)).toHaveAttribute('aria-label', '概览');
  await unlocked(page);
  const scroll = page.locator('.detail-overview-scroll');
  expect(await scroll.evaluate(element => element.scrollHeight - element.clientHeight)).toBeGreaterThan(0);
  await wheelOver(page, active(page).locator('.play-disc'), 60);
  await expect.poll(() => scroll.evaluate(element => element.scrollTop)).toBeGreaterThan(0);
  await expect(active(page)).toHaveAttribute('aria-label', '概览');
});

test('lets a wheel on a portal poster preview reach the detail page', async ({ page }) => {
  await openPanel(page, 'detail/canyon', '演职员');
  await active(page).locator('.detail-similar-target').first().hover();
  const preview = page.locator('.poster-preview.is-visible');
  await expect(preview).toBeVisible();
  await wheelOver(page, preview, 120);
  await expect(active(page)).toHaveAttribute('aria-label', '媒体信息');
  await expect(preview).toHaveCount(0);
});

test.describe('touch directions', () => {
  test.use({ viewport: { width: 393, height: 852 }, hasTouch: true });
  test('keeps a horizontal cast swipe in its row and lets a vertical swipe advance', async ({ page }) => {
    await openPanel(page, 'detail/canyon', '演职员');
    const rail = active(page).locator('.detail-cast-rail');
    const box = (await rail.boundingBox())!;
    const y = box.y + 50;
    const cdp = await page.context().newCDPSession(page);
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: 300, y }] });
    for (const x of [270, 230, 190, 150]) await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x, y }] });
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    await expect.poll(() => rail.evaluate(element => element.scrollLeft)).toBeGreaterThan(0);
    await expect(active(page)).toHaveAttribute('aria-label', '演职员');
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: 260, y }] });
    for (const offset of [20, 45, 75, 105]) await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: 260, y: y - offset }] });
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    await expect(active(page)).toHaveAttribute('aria-label', '媒体信息');
    await cdp.detach();
  });
});
