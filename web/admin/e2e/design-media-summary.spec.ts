import { expect, json, library, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';
import { installMediaSummaryFixture, mediaInfoFixture, mediaSummaryFixture } from './media-summary-fixture';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'dark' } });
const itemsPath = `/admin/media/libraries/${library.Id}/items`;

test('catalog technical columns and complete file facts use existing media data without mutations', async ({ page, api }, testInfo) => {
  installMediaSummaryFixture(api);
  await page.goto(itemsPath);
  const row = page.getByRole('row').filter({ hasText: 'The feature film' });
  await expect(row.getByRole('button', { name: 'View media details', exact: true })).toContainText('HEVC · 3840 × 2160');
  await expect(row).toContainText('40 GiB');
  await expect(row).toContainText('2:00:00');
  await expect(row.getByRole('button', { name: 'View indexed analysis' })).toHaveText('Failed');
  await noOverflow(page);
  await evidence(page, testInfo, 'media-summary-list-en-dark');
  await row.getByRole('button', { name: 'View media details', exact: true }).click();
  const sheet = page.getByRole('dialog', { name: /Edit metadata/ });
  await expect(sheet.getByRole('tab', { name: 'File', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(sheet.getByText('40 GiB', { exact: true })).toBeVisible();
  await expect(sheet.locator('.media-stream-details')).toHaveCount(3);
  await sheet.locator('.media-stream-details').first().locator('summary').click();
  await expect(sheet.getByText('smpte2084', { exact: true })).toBeVisible();
  await expect(sheet.getByText('24000/1001', { exact: true })).toBeVisible();
  const subtitles = sheet.locator('.media-stream-details').last();
  await subtitles.locator('summary').click();
  await expect(subtitles.getByText('External', { exact: true })).toBeVisible();
  await expect(subtitles.getByText('Hearing impaired', { exact: true })).toBeVisible();
  await expect(subtitles.getByText('Translated subtitles', { exact: true })).toBeVisible();
  await expect(sheet.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await noOverflow(page);
  await evidence(page, testInfo, 'media-summary-file-en-dark');
  await sheet.getByRole('tab', { name: 'Chapters & credits', exact: true }).click();
  const chapters = sheet.getByRole('table', { name: 'Recorded chapters' });
  await expect(chapters.getByRole('row')).toHaveCount(3);
  await expect(chapters).toContainText('Opening sequence');
  await expect(chapters).toContainText('A new beginning');
  await noOverflow(page);
  await evidence(page, testInfo, 'media-summary-chapters-en-dark');
  expect(api.writes()).toEqual([]);
});

test('unscanned media stays unknown instead of displaying invented zero facts', async ({ page, api }) => {
  const summary = { ...mediaSummaryFixture, State: 'not_scanned' as const, DurationTicks: null, Size: null, Bitrate: null, Container: '', Video: null, Audio: null };
  installMediaSummaryFixture(api, summary);
  await page.goto(itemsPath);
  const row = page.getByRole('row').filter({ hasText: 'The feature film' });
  await expect(row.getByRole('button', { name: 'View media details', exact: true })).toContainText('Not scanned');
  await expect(row).not.toContainText('0 bps');
  await expect(row).not.toContainText('0 B');
  await row.getByRole('button', { name: 'View media details', exact: true }).click();
  await expect(page.getByRole('dialog').getByText('Media details have not been scanned. Scan this library to read the source information.')).toBeVisible();
  expect(api.writes()).toEqual([]);
});

test('chapter pagination preserves the complete list and refresh failures retain the previous facts', async ({ page, api }) => {
  installMediaSummaryFixture(api);
  const chapters = Array.from({ length: 205 }, (_, index) => ({ Index: index, Name: `Chapter ${index + 1}`, StartTicks: String(BigInt(index) * 100000000n), EndTicks: null }));
  let failing = false;
  api.handlers.set('GET /admin/v1/items/film-one/media-info', async (route) => failing
    ? json(route, { Error: { Code: 'temporarily_unavailable', Message: 'Media summary temporarily unavailable.' } }, 503)
    : json(route, { ...mediaInfoFixture, Chapters: chapters, ChapterCount: chapters.length }));
  await page.goto(itemsPath);
  await page.getByRole('row').filter({ hasText: 'The feature film' }).getByRole('button', { name: 'View indexed analysis' }).click();
  const sheet = page.getByRole('dialog', { name: /Edit metadata/ });
  const table = sheet.getByRole('table', { name: 'Recorded chapters' });
  await expect(table.getByRole('row')).toHaveCount(101);
  await sheet.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(table.getByText('Chapter 101', { exact: true })).toBeVisible();
  await sheet.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(table.getByRole('row')).toHaveCount(6);
  await expect(table.getByText('Chapter 205', { exact: true })).toBeVisible();
  failing = true;
  await sheet.getByRole('button', { name: 'Refresh chapters', exact: true }).click();
  await expect(sheet.getByText('Showing the last successful media details update.')).toBeVisible();
  await expect(table.getByText('Chapter 205', { exact: true })).toBeVisible();
  expect(api.writes()).toEqual([]);
});

test.describe('Chinese compact media facts', () => {
  test.use({ viewport: { width: 390, height: 844 }, preferences: { language: 'zh', theme: 'light' } });
  test('compact list and file details fit the drawer and preserve literal source names', async ({ page, api }, testInfo) => {
    installMediaSummaryFixture(api);
    await page.goto(itemsPath);
    await expect(page.locator('.media-summary-compact').first()).toContainText('HEVC · 3840 × 2160');
    await noOverflow(page);
    await evidence(page, testInfo, 'media-summary-list-390-zh-light');
    const row = page.getByRole('row').filter({ hasText: 'The feature film' });
    await row.locator('.media-item-menu > summary').click();
    await row.getByRole('button', { name: '查看媒体信息', exact: true }).click();
    const sheet = page.getByRole('dialog');
    await expect(sheet.getByText('文件大小', { exact: true })).toBeVisible();
    await expect(sheet.getByText('40 GiB', { exact: true })).toBeVisible();
    await expect(sheet.getByText('Original HDR picture', { exact: true })).toBeVisible();
    const catalogUpdateLabel = sheet.locator('.metadata-file-panel dt').filter({ hasText: /^最近目录更新$/ });
    await expect(catalogUpdateLabel).toHaveText('最近目录更新');
    await expect(catalogUpdateLabel).toBeVisible();
    const labelLayout = await catalogUpdateLabel.evaluate((element) => {
      const range = document.createRange();
      range.selectNodeContents(element);
      const label = element.getBoundingClientRect();
      const value = element.parentElement!.querySelector('dd')!.getBoundingClientRect();
      return { label: { left: label.left, right: label.right }, valueLeft: value.left,
        text: [...range.getClientRects()].filter((rect) => rect.width > 0 && rect.height > 0).map((rect) => ({ left: rect.left, right: rect.right })) };
    });
    expect(labelLayout.text, 'The complete catalog update label must fit on one line.').toHaveLength(1);
    expect(labelLayout.text[0].left).toBeGreaterThanOrEqual(labelLayout.label.left - 1);
    expect(labelLayout.text[0].right).toBeLessThanOrEqual(labelLayout.label.right + 1);
    expect(labelLayout.text[0].right, 'The label must not overlap its value.').toBeLessThanOrEqual(labelLayout.valueLeft - 8);
    await noOverflow(page);
    await evidence(page, testInfo, 'media-summary-file-390-zh-light');
    expect(api.writes()).toEqual([]);
  });
});
