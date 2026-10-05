import { expect, test, type Page } from '@playwright/test';

async function openFirstSeason(page: Page) {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
  await page.goto('/#/detail/deep');
  await expect.poll(() => page.locator('.stage-deck').evaluate(element => performance.now() >= Number((element as HTMLElement).dataset.lockedUntil || 0))).toBe(true);
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: '剧集', exact: true }).click();
  await expect.poll(() => page.locator('.stage-deck').evaluate(element => performance.now() >= Number((element as HTMLElement).dataset.lockedUntil || 0))).toBe(true);
  await page.getByRole('tab', { name: '第 1 季', exact: true }).click();
  await expect(page.locator('.detail-episode-card.is-focused')).toHaveAttribute('data-episode-id', 'deep-1-1');
  await page.locator('[data-episode-id="deep-1-1"] .detail-episode-open').focus();
  // Freeze presentation timers so the input sequence cannot wait out an animation.
  await page.clock.install({ time: new Date('2026-10-05T04:00:00Z') });
  await page.clock.pauseAt(new Date('2026-10-05T04:00:01Z'));
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset');
});

test('keeps fast episode navigation and Enter independent of text transitions', async ({ page }) => {
  await openFirstSeason(page);
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  await expect(page.locator('.detail-episode-card.is-focused')).toHaveAttribute('data-episode-id', 'deep-1-3');
  await expect(page.locator('.detail-episode-transition')).toHaveClass(/is-exiting/);
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/#\/player\/deep-1-3$/);
});

test('plays the newly selected season before its information animation completes', async ({ page }) => {
  await openFirstSeason(page);
  // Dispatch the tab click without waiting for animation frames from the paused clock.
  await page.getByRole('tab', { name: '第 2 季', exact: true }).dispatchEvent('click');
  await expect(page.locator('.detail-episode-card.is-focused')).toHaveAttribute('data-episode-id', 'deep-2-1');
  await page.locator('.detail-episodes-page .play-disc').focus();
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/#\/player\/deep-2-1$/);
});

test('plays the newly focused resume card when Enter follows an arrow immediately', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const resumeResponse = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/Items/Resume'));
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  const resume = (await (await resumeResponse).json()).Items as Array<{ Id: string }>;
  expect(resume.length).toBeGreaterThan(1);
  await expect.poll(() => page.locator('.stage-deck').evaluate(element => performance.now() >= Number((element as HTMLElement).dataset.lockedUntil || 0))).toBe(true);
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: '继续观看', exact: true }).click();
  await expect.poll(() => page.locator('.stage-deck').evaluate(element => performance.now() >= Number((element as HTMLElement).dataset.lockedUntil || 0))).toBe(true);
  const panel = page.locator('.deck-panel[data-active="true"]');
  const cards = panel.locator('.stage-rail-cards > .landscape-card');
  await cards.first().focus();
  await page.clock.install({ time: new Date('2026-10-05T04:00:00Z') });
  // Advance past the existing deck lock, then hold all presentation timers.
  await page.clock.pauseAt(new Date('2026-10-05T04:01:00Z'));
  await page.keyboard.press('ArrowRight');
  await expect(cards.nth(1)).toBeFocused();
  await expect(cards.nth(1)).toHaveClass(/is-active/);
  await expect(panel.locator('.home-strip-info-content')).toHaveClass(/home-strip-info-out/);
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(new RegExp(`#/player/${resume[1].Id}$`));
});
