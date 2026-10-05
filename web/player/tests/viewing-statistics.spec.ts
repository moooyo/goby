import { expect, test, type Page, type Route } from '@playwright/test';

const statisticsPath = '**/emby/Users/fixture-viewer/ViewingStatistics';
const hours = (page: Page) => page.locator('.settings-statistics > div').last();

async function signIn(page: Page) {
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
}

async function settings(page: Page) {
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '个人中心', exact: true }).click();
  await expect(page.getByLabel('观看统计', { exact: true })).toBeVisible();
}

test.beforeEach(async ({ request }) => { await request.post('/__fixture/reset'); });

test('shows server-aggregated content hours with the handoff layout and an honest estimate label', async ({ page, request }) => {
  let reads = 0;
  await page.route(statisticsPath, async route => {
    reads += 1;
    expect(route.request().method()).toBe('GET');
    expect(route.request().headers()['x-emby-authorization']).toContain('isolated-player-acceptance-token');
    await route.fulfill({ json: { EstimatedContentHours: 4, EstimatedContentTicks: '126000000000', IsEstimate: true } });
  });
  await signIn(page);
  await settings(page);
  await expect(hours(page).locator('strong')).toHaveText('4');
  await expect(hours(page).locator('span')).toHaveText('小时');
  await expect(hours(page)).toHaveAttribute('aria-label', '已看内容时长估算 4 小时');
  await expect(hours(page)).toHaveAttribute('title', /重复观看不重复累计/);
  expect(reads).toBe(1);
  const state = await (await request.get('/__fixture/state')).json();
  const items = state.requests.filter((entry: { path: string }) => entry.path === '/Users/fixture-viewer/Items');
  expect(items.every((entry: { query: { Limit: string } }) => ['0', '48'].includes(entry.query.Limit))).toBe(true);
  expect(state.events).toEqual([]);
  expect(state.unknownRequests).toEqual([]);
});

test('renders a measured empty estimate as zero instead of an unavailable placeholder', async ({ page }) => {
  await page.route(statisticsPath, route => route.fulfill({ json: { EstimatedContentHours: 0, EstimatedContentTicks: '0', IsEstimate: true } }));
  await signIn(page);
  await settings(page);
  await expect(hours(page).locator('strong')).toHaveText('0');
  await expect(hours(page)).toHaveAttribute('aria-label', '已看内容时长估算 0 小时');
});

test('keeps hours unavailable when statistics fail or conflict with their precise total', async ({ page }) => {
  let failed = true;
  await page.route(statisticsPath, route => failed
    ? route.fulfill({ status: 503, json: { ResponseStatus: { ErrorCode: 'unavailable' } } })
    : route.fulfill({ json: { EstimatedContentHours: 42, EstimatedContentTicks: '0', IsEstimate: true } }));
  await signIn(page);
  await settings(page);
  await expect(hours(page).locator('strong')).toHaveText('—');
  await expect(hours(page)).toHaveAttribute('aria-label', '已看内容时长估算暂时不可用');
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
  failed = false;
  await page.reload();
  await expect(hours(page).locator('strong')).toHaveText('—');
  await expect(hours(page)).toHaveAttribute('aria-label', '已看内容时长估算暂时不可用');
});

test('does not let an earlier page request replace current viewing hours', async ({ page }) => {
  let first: Route | undefined;
  let reads = 0;
  await page.route(statisticsPath, async route => {
    reads += 1;
    if (reads === 1) { first = route; return; }
    await route.fulfill({ json: { EstimatedContentHours: 2, EstimatedContentTicks: '72000000000', IsEstimate: true } });
  });
  await signIn(page);
  await settings(page);
  await expect.poll(() => reads).toBe(1);
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '首页', exact: true }).click();
  await settings(page);
  await expect(hours(page).locator('strong')).toHaveText('2');
  await first!.fulfill({ json: { EstimatedContentHours: 9, EstimatedContentTicks: '324000000000', IsEstimate: true } }).catch(() => {});
  await expect(hours(page).locator('strong')).toHaveText('2');
});
