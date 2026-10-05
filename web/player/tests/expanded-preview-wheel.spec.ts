import { expect, test, type Page } from '@playwright/test';

const active = (page: Page) => page.locator('.deck-panel[data-active="true"]');
const preview = (page: Page) => page.locator('.poster-preview.is-visible[data-poster-preview="bamboo"]');
const unlocked = async (page: Page) => {
  await expect.poll(() => page.locator('.stage-deck').evaluate(element => performance.now() >= Number((element as HTMLElement).dataset.lockedUntil))).toBe(true);
};

async function state(page: Page) {
  return page.evaluate(() => {
    const panel = document.querySelector('.deck-panel[data-active="true"]');
    const describe = (element: Element | null) => element ? {
      className: element.className,
      top: element.scrollTop,
      left: element.scrollLeft,
      height: element.clientHeight,
      contentHeight: element.scrollHeight,
      overflowY: getComputedStyle(element).overflowY,
    } : null;
    return {
      panel: panel?.getAttribute('aria-label'),
      preview: describe(document.querySelector('.poster-preview-surface')),
      stage: describe(panel?.querySelector('.stage-scroll') ?? null),
      rail: describe(panel?.querySelector('.detail-similar-rail') ?? null),
    };
  });
}

test.use({ reducedMotion: 'no-preference' });

test.beforeEach(async ({ page, request }) => {
  await request.post('/__fixture/reset');
  await page.addInitScript(() => {
    const observations: unknown[] = [];
    Object.defineProperty(window, '__previewWheelObservations', { value: observations });
    window.addEventListener('wheel', event => {
      const target = event.target instanceof Element ? event.target : null;
      observations.push({
        target: target?.tagName,
        className: target?.getAttribute('class'),
        defaultPrevented: event.defaultPrevented,
        deltaX: event.deltaX,
        deltaY: event.deltaY,
        x: event.clientX,
        y: event.clientY,
        preview: target?.closest('.poster-preview')?.getAttribute('data-poster-preview'),
        activePanel: document.querySelector('.deck-panel[data-active="true"]')?.getAttribute('aria-label'),
      });
    }, { passive: true });
  });
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible();
});

test.afterEach(async ({ page }, info) => {
  const diagnostics = await page.evaluate(() => ({
    viewport: { width: innerWidth, height: innerHeight, devicePixelRatio },
    scripts: [...document.scripts].map(script => new URL(script.src || location.href).pathname),
    wheels: (window as unknown as { __previewWheelObservations: unknown[] }).__previewWheelObservations,
  })).catch(() => undefined);
  await info.attach('expanded-preview-wheel-diagnostics', {
    body: JSON.stringify({ ...diagnostics, state: await state(page).catch(() => undefined) }, null, 2),
    contentType: 'application/json',
  });
});

async function openBamboo(page: Page) {
  await page.goto('/#/detail/canyon');
  await expect(page.locator('.stage-deck')).toBeVisible();
  await unlocked(page);
  await page.getByRole('navigation', { name: '页面分页' }).getByRole('button', { name: '演职员', exact: true }).click();
  await expect(active(page)).toHaveAttribute('aria-label', '演职员');
  await unlocked(page);
  const second = active(page).locator('.detail-similar-target').nth(1);
  await expect(second.locator('strong')).toHaveText('竹林客');
  await second.scrollIntoViewIfNeeded();
  await second.hover();
  await expect(preview(page)).toBeVisible();
  await expect(preview(page).locator('.poster-preview-surface')).toHaveCSS('clip-path', 'inset(0px round 18px)');
  await expect(preview(page).locator('.poster-preview-ghost')).toHaveCSS('opacity', '0');
}

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 2048, height: 1174 },
  { width: 1440, height: 400 },
]) {
  for (const region of [
    { name: 'image', selector: '.poster-preview-image-link' },
    { name: 'title', selector: '.poster-preview-title-group h2' },
    { name: 'overview', selector: '.poster-preview-overview' },
    { name: 'play', selector: '.poster-preview-play' },
    { name: 'favorite', selector: '.poster-preview-actions > button:first-child' },
    { name: 'details', selector: '.poster-preview-actions > button:last-child' },
  ]) {
    test(`scrolls vertically over fully expanded second recommendation ${region.name} at ${viewport.width}x${viewport.height}`, async ({ page }, info) => {
      await page.setViewportSize(viewport);
      await openBamboo(page);
      const target = preview(page).locator(region.selector);
      await target.scrollIntoViewIfNeeded();
      await target.hover();
      const before = await state(page);
      await info.attach('before-wheel', { body: JSON.stringify(before, null, 2), contentType: 'application/json' });
      const box = (await target.boundingBox())!;
      const x = box.x + box.width / 2, y = box.y + box.height / 2;
      const hit = await page.evaluate(({ x, y }) => document.elementFromPoint(x, y)?.closest('.poster-preview')?.getAttribute('data-poster-preview'), { x, y });
      expect(hit).toBe('bamboo');
      await page.mouse.wheel(0, 120);
      const canPreviewScroll = before.preview?.overflowY === 'auto' && before.preview.top + before.preview.height < before.preview.contentHeight - 2;
      const canStageScroll = before.stage && before.stage.top + before.stage.height < before.stage.contentHeight - 2;
      if (canPreviewScroll) {
        await expect.poll(async () => (await state(page)).preview?.top).toBeGreaterThan(before.preview!.top);
        await expect(active(page)).toHaveAttribute('aria-label', '演职员');
      } else if (canStageScroll) {
        await expect.poll(async () => (await state(page)).stage?.top).toBeGreaterThan(before.stage!.top);
        await expect(active(page)).toHaveAttribute('aria-label', '演职员');
      } else {
        await expect(active(page)).toHaveAttribute('aria-label', '媒体信息');
        await expect(preview(page)).toHaveCount(0);
      }
      await expect.poll(async () => (await state(page)).rail?.left ?? before.rail!.left).toBe(before.rail!.left);
    });
  }
}
