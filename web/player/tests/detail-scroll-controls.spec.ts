import { expect, test, type Locator, type Page } from '@playwright/test';

const activePanel = (page: Page) => page.locator('.deck-panel[data-active="true"]');

async function waitForDeck(page: Page) {
  await expect.poll(() => page.locator('.stage-deck').evaluate(element => performance.now() >= Number((element as HTMLElement).dataset.lockedUntil || 0))).toBe(true);
}

async function openSection(page: Page, id: string, section: string) {
  await page.goto(`/#/detail/${id}`);
  await expect(page.locator('.detail-overview-info h1')).toBeVisible();
  await waitForDeck(page);
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: section, exact: true }).click();
  await expect(activePanel(page)).toHaveAttribute('aria-label', section);
  await waitForDeck(page);
}

async function clickToEnd(rail: Locator, next: Locator) {
  for (let step = 0; step < 20 && await next.isEnabled(); step++) {
    const target = await rail.evaluate(element => Math.min(element.scrollWidth - element.clientWidth, element.scrollLeft + element.clientWidth * .75));
    await next.click();
    await expect.poll(() => rail.evaluate(element => element.scrollLeft)).toBeGreaterThanOrEqual(target - 1);
  }
  await expect(next).toBeDisabled();
  await expect(next).toBeHidden();
  await expect.poll(() => rail.evaluate(element => element.scrollWidth - element.clientWidth - element.scrollLeft)).toBeLessThanOrEqual(1);
}

test.beforeEach(async ({ page, request }) => {
  await request.post('/__fixture/reset');
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
});

test('hides cast and season controls when their rows fit', async ({ page }) => {
  await openSection(page, 'canyon', '演职员');
  expect(await activePanel(page).locator('.detail-cast-rail').evaluate(element => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(1);
  for (const arrow of await activePanel(page).locator('.detail-cast-arrow').all()) {
    await expect(arrow).toBeDisabled();
    await expect(arrow).toBeHidden();
    await expect(arrow).toHaveAttribute('tabindex', '-1');
  }
  await openSection(page, 'deep', '剧集');
  expect(await activePanel(page).locator('.detail-seasons').evaluate(element => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(1);
  for (const arrow of await activePanel(page).locator('.detail-season-arrow').all()) {
    await expect(arrow).toBeDisabled();
    await expect(arrow).toBeHidden();
  }
});

test('keeps overflowing cast and recommendation controls reachable with a narrow mouse viewport', async ({ page }) => {
  await page.setViewportSize({ width: 393, height: 852 });
  await page.route('**/emby/Users/fixture-viewer/Items/canyon?*', async route => {
    const response = await route.fetch();
    const item = await response.json();
    item.People = Array.from({ length: 12 }, (_, index) => ({ Name: `Actor ${index + 1}`, Type: 'Actor', Role: `Role ${index + 1}` }));
    await route.fulfill({ response, json: item });
  });
  await openSection(page, 'canyon', '演职员');
  const cast = activePanel(page).locator('.detail-cast-rail');
  const next = activePanel(page).locator('.detail-cast-arrow.is-right');
  await cast.hover();
  await expect(next).toBeEnabled();
  await expect(next).toHaveCSS('opacity', '1');
  await expect(activePanel(page).locator('.detail-cast-arrow.is-left')).toBeHidden();
  await clickToEnd(cast, next);
  const end = await cast.evaluate(element => element.scrollLeft);
  await activePanel(page).getByRole('button', { name: '上一组演职员', exact: true }).click();
  await expect.poll(() => cast.evaluate(element => element.scrollLeft)).toBeLessThan(end - 100);
  const similar = activePanel(page).locator('.detail-similar-rail');
  await similar.hover();
  const similarNext = activePanel(page).getByRole('button', { name: '下一组相似影片', exact: true });
  await expect(similarNext).toHaveCSS('opacity', '1');
  await similarNext.click();
  await expect.poll(() => similar.evaluate(element => element.scrollLeft)).toBeGreaterThan(100);
  await expect(activePanel(page)).toHaveAttribute('aria-label', '演职员');
});

test('keeps overflowing season and episode controls reachable with a narrow mouse viewport', async ({ page }) => {
  await page.setViewportSize({ width: 393, height: 852 });
  await page.route('**/emby/Shows/deep/Seasons?*', route => route.fulfill({ json: {
    Items: Array.from({ length: 12 }, (_, index) => ({ Id: `deep-season-${index + 1}`, Name: `第 ${index + 1} 季`, Type: 'Season', SeriesId: 'deep', IndexNumber: index + 1 })), TotalRecordCount: 12,
  } }));
  await openSection(page, 'deep', '剧集');
  const episodes = activePanel(page).locator('.detail-episode-rail');
  const before = await episodes.evaluate(element => element.scrollLeft);
  const previousEpisodes = activePanel(page).getByRole('button', { name: '上一组剧集', exact: true });
  await expect(previousEpisodes).toBeEnabled();
  await expect(previousEpisodes).toHaveCSS('opacity', '1');
  await previousEpisodes.click();
  await expect.poll(() => episodes.evaluate(element => element.scrollLeft)).toBeLessThan(before - 100);
  const seasons = activePanel(page).locator('.detail-seasons');
  const next = activePanel(page).locator('.detail-season-arrow.is-right');
  await seasons.hover();
  await expect(next).toBeEnabled();
  await expect(next).toHaveCSS('opacity', '1');
  await clickToEnd(seasons, next);
  await activePanel(page).getByRole('tab', { name: '第 12 季', exact: true }).click();
  await expect(activePanel(page).getByRole('tab', { name: '第 12 季', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(activePanel(page)).toHaveAttribute('aria-label', '剧集');
});
