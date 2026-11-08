import type { Locator } from '@playwright/test';
import { analysisItem, expect, json, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off', preferences: { language: 'zh', theme: 'dark' } });

const detailPath = `/admin/v1/media-analysis/items/${analysisItem.Id}`;

async function expectReadableTitle(heading: Locator): Promise<void> {
  const layout = await heading.evaluate((element) => {
    const header = element.closest('.goby-dialog-heading');
    const dialog = element.closest('dialog');
    const close = header?.querySelector('.goby-dialog-close');
    if (!header || !dialog || !close) throw new Error('The analysis title must have a header, dialog, and close control.');
    const range = document.createRange();
    range.selectNodeContents(element);
    const box = (rect: DOMRect) => ({ left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom });
    return {
      header: box(header.getBoundingClientRect()), dialog: box(dialog.getBoundingClientRect()), close: box(close.getBoundingClientRect()),
      text: [...range.getClientRects()].filter((rect) => rect.width > 0 && rect.height > 0).map(box),
    };
  });
  expect(layout.text.length, 'The analysis title must contain rendered text.').toBeGreaterThan(0);
  for (const rect of layout.text) {
    for (const bounds of [layout.header, layout.dialog]) {
      expect(rect.left).toBeGreaterThanOrEqual(bounds.left - 1);
      expect(rect.right).toBeLessThanOrEqual(bounds.right + 1);
      expect(rect.top).toBeGreaterThanOrEqual(bounds.top - 1);
      expect(rect.bottom).toBeLessThanOrEqual(bounds.bottom + 1);
    }
    expect(rect.right, 'The analysis title must not overlap the close control.').toBeLessThanOrEqual(layout.close.left - 8);
  }
}

for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
  test(`${viewport.width}px analysis identities preserve literal names in the localized result`, async ({ page, api }, testInfo) => {
    api.handlers.set(`GET ${detailPath}`, (route) => json(route, { ...analysisItem, Name: 'System' }));
    await page.setViewportSize(viewport);
    await page.goto('/admin/media/analysis');
    await page.getByRole('button', { name: `查看${analysisItem.Name}的分析`, exact: true }).click();
    const result = page.getByRole('dialog', { name: 'System', exact: true });
    const heading = result.getByRole('heading', { level: 2, name: 'System', exact: true });
    await expect(heading).toHaveAttribute('id', 'analysis-item-title');
    await expect(heading).toHaveText('System');
    await expect(heading.locator('strong')).toHaveText('System');
    await expect(heading.locator('strong')).toHaveAttribute('translate', 'no');
    await expect(result).toContainText('片头检测可用');
    await expect(result).toContainText('0:10.00–0:40.00');
    const outputs = result.getByRole(viewport.width < 840 ? 'list' : 'table', { name: '预览产物', exact: true });
    await expect(outputs).toContainText('320 × 180');
    await expect(outputs).toContainText('12');
    await expect(outputs).toContainText('32.0 KiB');
    await expect(outputs.getByText('就绪', { exact: true })).toBeVisible();
    await expect(outputs.getByText('ready', { exact: true })).toHaveCount(0);
    await page.evaluate(() => document.fonts.ready);
    await expectReadableTitle(heading);
    await noOverflow(page);
    await evidence(page, testInfo, `design2-analysis-title-${viewport.width}-zh-dark`);
    expect(api.reads(detailPath)).toHaveLength(1);
    expect(api.writes()).toEqual([]);
  });
}

test('unnamed analysis results retain the localized generic title', async ({ page, api }) => {
  api.handlers.set(`GET ${detailPath}`, (route) => json(route, { ...analysisItem, Name: '' }));
  await page.goto('/admin/media/analysis');
  await page.getByRole('button', { name: `查看${analysisItem.Name}的分析`, exact: true }).click();
  const result = page.getByRole('dialog', { name: '媒体分析结果', exact: true });
  await expect(result).toContainText('0:10.00–0:40.00');
  await expect(result.getByRole('heading', { level: 2, name: '媒体分析结果', exact: true })).toHaveText('媒体分析结果');
  expect(api.reads(detailPath)).toHaveLength(1);
  expect(api.writes()).toEqual([]);
});

for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
  test(`${viewport.width}px preview output states localize known values and preserve unknown identifiers`, async ({ page, api }) => {
    const states = [
      ['ready', '就绪'], ['qualified', '已通过'], ['published', '已发布'],
      ['review', '核对'], ['stale', '已过期'], ['suppressed', '已停用'],
      ['failed', '失败'], ['error', '错误'], ['building', '生成中'],
      ['running', '运行中'], ['pending', '待处理'], ['queued', '排队中'],
      ['cancelled', '已停止'], ['interrupted', '已中断'], ['unavailable', '不可用'],
      ['Ready', 'Ready'], ['provider_custom_phase', 'provider_custom_phase'],
    ];
    api.handlers.set(`GET ${detailPath}`, (route) => json(route, {
      ...analysisItem, Previews: states.map(([Status], index) => ({ ...analysisItem.Previews[0], Width: 320 + index, Status })),
    }));
    await page.setViewportSize(viewport);
    await page.goto('/admin/media/analysis');
    await page.getByRole('button', { name: `查看${analysisItem.Name}的分析`, exact: true }).click();
    const result = page.getByRole('dialog', { name: analysisItem.Name, exact: true });
    const outputs = result.getByRole(viewport.width < 840 ? 'list' : 'table', { name: '预览产物', exact: true });
    for (const [, label] of states) await expect(outputs.getByText(label, { exact: true })).toHaveCount(1);
    await expect(outputs.getByText('ready', { exact: true })).toHaveCount(0);
    await expect(outputs.getByText('Provider custom phase', { exact: true })).toHaveCount(0);
    expect(api.reads(detailPath)).toHaveLength(1);
    expect(api.writes()).toEqual([]);
  });
}
