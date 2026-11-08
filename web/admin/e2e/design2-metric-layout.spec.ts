import { expect, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });

for (const preferences of [{ language: 'en', theme: 'light' }, { language: 'zh', theme: 'dark' }] as const) {
  test.describe(`${preferences.language} ${preferences.theme} compact directory metric`, () => {
    test.use({ viewport: { width: preferences.language === 'en' ? 375 : 390, height: 844 }, preferences });
    test('the directory title and count do not overlap or expand the compact card', async ({ page, api }, testInfo) => {
      await page.goto('/admin/');
      const card = page.locator('.ov-directory-capacity');
      await expect(card.locator('.ov-metric-value strong')).not.toHaveText('—');
      await page.evaluate(() => document.fonts.ready);
      const layout = await card.evaluate((element) => {
        const region = element.getBoundingClientRect();
        const title = element.querySelector('.ov-metric-heading > span')!;
        const count = element.querySelector('.ov-metric-heading small')!;
        const titleBox = title.getBoundingClientRect(), countBox = count.getBoundingClientRect();
        const lineHeight = Number.parseFloat(getComputedStyle(title).lineHeight);
        return {
          titleHeight: titleBox.height, lineHeight,
          overlap: titleBox.left < countBox.right && titleBox.right > countBox.left && titleBox.top < countBox.bottom && titleBox.bottom > countBox.top,
          countContained: countBox.left >= region.left && countBox.right <= region.right && countBox.top >= region.top && countBox.bottom <= region.bottom,
          countClipped: count.scrollWidth > count.clientWidth,
          fullTitle: title.getAttribute('title'),
        };
      });
      expect(layout.titleHeight).toBeLessThanOrEqual(layout.lineHeight + 1);
      expect(layout.overlap).toBe(false);
      expect(layout.countContained).toBe(true);
      expect(layout.countClipped).toBe(false);
      expect(layout.fullTitle).toBeTruthy();
      await noOverflow(page);
      expect(api.writes()).toEqual([]);
      await evidence(page, testInfo, `design2-directory-metric-compact-${preferences.language}-${preferences.theme}`);
    });
  });
}
