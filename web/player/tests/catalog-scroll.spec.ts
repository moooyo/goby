import { expect, test, type Page } from '@playwright/test';

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function catalogOptions(page: Page, count: number) {
  const libraries = Array.from({ length: count }, (_, index) => ({ Id: `library-${index}`, Name: `电影库 ${index + 1}`, Type: 'CollectionFolder', CollectionType: 'movies' }));
  const genres = Array.from({ length: count }, (_, index) => ({ Id: `genre-${index}`, Name: `类型 ${index + 1}`, ItemCount: count - index }));
  await page.route('**/emby/Users/fixture-viewer/Views', route => route.fulfill({ json: { Items: libraries, TotalRecordCount: count } }));
  await page.route('**/emby/Genres?*', route => route.fulfill({ json: { Items: genres, TotalRecordCount: count } }));
}

test.beforeEach(async ({ request }) => { await request.post('/__fixture/reset'); });

test('hides catalog rail arrows when every option fits', async ({ page }) => {
  await catalogOptions(page, 2);
  await signIn(page);
  await page.goto('/#/movies');
  await expect(page.locator('.catalog-libraries button')).toHaveCount(3);
  await expect(page.locator('.catalog-genres button')).toHaveCount(3);
  await expect(page.locator('.catalog-rail-arrow:visible')).toHaveCount(0);
});

test('makes overflowing library and genre options reachable with visible arrows', async ({ page }) => {
  await page.setViewportSize({ width: 393, height: 852 });
  await catalogOptions(page, 10);
  await signIn(page);
  await page.goto('/#/movies');
  for (const [selector, label] of [['.catalog-libraries', '媒体库'], ['.catalog-genres', '类型']]) {
    const rail = page.locator(selector);
    const next = page.getByRole('button', { name: `向右浏览${label}`, exact: true });
    const previous = page.getByRole('button', { name: `向左浏览${label}`, exact: true });
    await expect(next).toBeVisible();
    await expect(previous).toBeHidden();
    await next.click();
    await expect.poll(() => rail.evaluate(element => element.scrollLeft)).toBeGreaterThan(0);
    await expect(previous).toBeVisible();
    for (let index = 0; index < 20 && await rail.evaluate(element => element.scrollWidth - element.clientWidth - element.scrollLeft > 2); index += 1) {
      const before = await rail.evaluate(element => element.scrollLeft);
      await next.click();
      await expect.poll(() => rail.evaluate(element => element.scrollLeft)).toBeGreaterThan(before);
    }
    await expect(next).toBeHidden();
    expect(await rail.evaluate(element => element.scrollWidth - element.clientWidth - element.scrollLeft)).toBeLessThanOrEqual(2);
    for (let index = 0; index < 20 && await rail.evaluate(element => element.scrollLeft > 1); index += 1) {
      const before = await rail.evaluate(element => element.scrollLeft);
      await previous.click();
      await expect.poll(() => rail.evaluate(element => element.scrollLeft)).toBeLessThan(before);
    }
    await expect(previous).toBeHidden();
    expect(await rail.evaluate(element => element.scrollLeft)).toBeLessThanOrEqual(1);
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(393);
});

for (const selector of ['.catalog-libraries', '.catalog-genres']) {
  test(`keeps vertical wheels vertical and accepts native horizontal gestures on ${selector}`, async ({ page }) => {
    await page.setViewportSize({ width: 1000, height: 500 });
    await catalogOptions(page, 10);
    await signIn(page);
    await page.goto('/#/movies');
    const rail = page.locator(selector);
    await expect(page.locator('.poster-card').first()).toBeVisible();
    expect(await rail.evaluate(element => element.scrollWidth - element.clientWidth)).toBeGreaterThan(2);
    const box = (await rail.boundingBox())!;
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.wheel(0, 160);
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(0);
    expect(await rail.evaluate(element => element.scrollLeft)).toBe(0);
    await page.evaluate(() => window.scrollTo(0, 0));
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.wheel(180, 0);
    await expect.poll(() => rail.evaluate(element => element.scrollLeft)).toBeGreaterThan(0);
    expect(await page.evaluate(() => window.scrollY)).toBe(0);
  });
}

test('wraps narrow settings options so each subtitle choice can be clicked without horizontal scrolling', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 740 });
  await signIn(page);
  await page.goto('/#/settings');
  const subtitles = page.getByRole('group', { name: '默认字幕', exact: true });
  await subtitles.scrollIntoViewIfNeeded();
  const layout = await subtitles.evaluate(element => {
    const bounds = element.getBoundingClientRect();
    const buttons = Array.from(element.querySelectorAll('button')).map(button => button.getBoundingClientRect());
    return { overflow: element.scrollWidth - element.clientWidth, rows: new Set(buttons.map(button => button.top)).size, inside: buttons.every(button => button.left >= bounds.left && button.right <= bounds.right) };
  });
  expect(layout.overflow).toBeLessThanOrEqual(1);
  expect(layout.rows).toBeGreaterThan(1);
  expect(layout.inside).toBe(true);
  await subtitles.getByRole('button', { name: '双语', exact: true }).click();
  await expect(subtitles.getByRole('button', { name: '双语', exact: true })).toHaveAttribute('aria-pressed', 'true');
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
});
