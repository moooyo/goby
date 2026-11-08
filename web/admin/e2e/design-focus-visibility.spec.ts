import type { Locator, Page } from '@playwright/test';
import { expect, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'light' } });

async function overlapsSavebar(control: Locator): Promise<boolean> {
  return control.evaluate((element) => {
    const bar = element.closest('.settings-page')!.querySelector('.settings-savebar')!;
    const field = element.getBoundingClientRect(), footer = bar.getBoundingClientRect();
    return field.left < footer.right && field.right > footer.left && field.top < footer.bottom && field.bottom > footer.top;
  });
}

async function expectClearFocusedControl(page: Page, control: Locator): Promise<void> {
  await expect(control).toBeFocused();
  await expect.poll(() => control.evaluate((element) => {
    const viewport = element.closest('.dashboard-content')!.getBoundingClientRect();
    const field = element.getBoundingClientRect();
    return field.top >= viewport.top && field.bottom <= Math.min(viewport.bottom, innerHeight);
  }), 'The focused control must remain in the visible scrollport.').toBe(true);
  await expect.poll(() => overlapsSavebar(control), 'The action bar must not cover the focused control.').toBe(false);
  await noOverflow(page);
}

for (const width of [375, 1440]) {
  test.describe(width + 'px settings keyboard visibility', () => {
    test.use({ viewport: { width, height: width === 375 ? 844 : 900 } });
    test('sticky actions cover the bottom gutter while focused settings stay above them', async ({ page, api }, testInfo) => {
      await page.goto('/admin/settings/general');
      const language = page.getByRole('combobox', { name: 'Interface language', exact: true });
      await expect(language).toBeEnabled();
      await expect(page.getByRole('button', { name: 'Reload settings', exact: true })).toBeEnabled();
      const bar = page.locator('.settings-savebar');
      await expect(bar).toHaveCSS('position', 'sticky');
      await expect.poll(() => bar.evaluate((element) => {
        const footer = element.getBoundingClientRect();
        const scroller = element.closest('.dashboard-content')!.getBoundingClientRect();
        const bottom = Math.min(scroller.bottom, innerHeight);
        const hit = document.elementFromPoint(footer.left + footer.width / 2, bottom - 2);
        return footer.bottom >= bottom - 1 && hit !== null && element.contains(hit);
      }), 'The action surface must cover the scrollport gutter beneath its buttons.').toBe(true);

      // Reproduce a keyboard target inside the sticky bar's rectangle without
      // relying on the browser to recognize that a sticky sibling covers it.
      await language.evaluate((element) => {
        const scroller = element.closest('.dashboard-content')!;
        const footer = element.closest('.settings-page')!.querySelector('.settings-savebar')!.getBoundingClientRect();
        scroller.scrollBy({ top: element.getBoundingClientRect().top - footer.top - 8, behavior: 'instant' });
      });
      await expect.poll(() => overlapsSavebar(language)).toBe(true);
      await language.evaluate((element) => (element as HTMLSelectElement).focus({ preventScroll: true }));
      await expectClearFocusedControl(page, language);
      await evidence(page, testInfo, 'settings-language-focus-clear-' + width);

      await page.keyboard.press('Tab');
      const appearance = page.getByRole('combobox', { name: 'Appearance', exact: true });
      await expectClearFocusedControl(page, appearance);
      await page.setViewportSize({ width, height: 640 });
      await expectClearFocusedControl(page, appearance);
      await evidence(page, testInfo, 'settings-resized-focus-clear-' + width);
      expect(api.writes(), 'Focus correction must not submit or change settings.').toEqual([]);
    });
  });
}

test.describe('Chinese overview dates', () => {
  test.use({ viewport: { width: 375, height: 844 }, preferences: { language: 'zh', theme: 'dark' } });
  test('past month and day labels remain on one line on a phone', async ({ page, api }, testInfo) => {
    await page.clock.setFixedTime(new Date('2026-11-06T12:00:00Z'));
    await page.goto('/admin/');
    const dates = page.locator('.ov-activity-row time');
    await expect(dates).toHaveCount(2);
    await expect(dates.first()).toHaveText(/\d+月\d+日/);
    const lineMetrics = await dates.evaluateAll((elements) => elements.map((element) => ({
      height: element.getBoundingClientRect().height,
      lineHeight: Number.parseFloat(getComputedStyle(element).lineHeight),
      clipped: element.scrollWidth > element.clientWidth,
    })));
    for (const metrics of lineMetrics) {
      expect(metrics.height, 'A date label must occupy one line.').toBeLessThanOrEqual(metrics.lineHeight + 1);
      expect(metrics.clipped, 'The full date label must fit its column.').toBe(false);
    }
    await dates.first().scrollIntoViewIfNeeded();
    await noOverflow(page);
    await evidence(page, testInfo, 'overview-phone-date-one-line-zh-dark');
    expect(api.writes()).toEqual([]);
  });
});
