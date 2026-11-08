import type { Locator, Page } from '@playwright/test';
import type { Library } from '../src/api';
import type { AnalysisOverview, AnalysisProfile } from '../src/mediaAnalysis';
import { csrf, DashboardAPI, expect, json, library, seriesLibrary, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off' });

const librariesPath = '/admin/v1/libraries';
const overviewPath = '/admin/v1/media-analysis';
const configurationPath = '/admin/v1/media-analysis/configuration';
const longName = 'International feature films, restorations, and archival director editions — OfficeSettings';
const coverageLibraries: Library[] = [
  { ...library, Name: longName, LibraryOptions: { ...library.LibraryOptions,
    EnablePreviewGeneration: true, EnableCreditsDetection: false, EnableBackgroundPreviewGeneration: true, EnableAudioWaveformGeneration: false } },
  { ...seriesLibrary, Name: 'Settings', LibraryOptions: undefined },
  { ...library, Id: 'd'.repeat(32), Name: 'Audio recordings — second floor', CollectionType: 'music', LibraryOptions: { ...library.LibraryOptions,
    EnablePreviewGeneration: true, EnableIntroDetection: true, EnableCreditsDetection: true, EnableBackgroundPreviewGeneration: true,
    EnableAudioWaveformGeneration: true, EnableSubtitleTimelineGeneration: true } },
];

function installCoverage(api: DashboardAPI, libraries = coverageLibraries): void {
  api.handlers.set(`GET ${librariesPath}`, (route) => json(route, { Items: libraries, TotalRecordCount: libraries.length }));
}

async function openCoverage(page: Page, label = 'Coverage'): Promise<Locator> {
  await page.goto('/admin/media/analysis');
  await expect(page.locator('.analysis-page')).toHaveAttribute('aria-busy', 'false');
  const coverage = page.getByRole('region', { name: label, exact: true });
  await expect(coverage).toBeVisible();
  await page.evaluate(() => document.fonts.ready);
  return coverage;
}

async function expectReadableText(label: Locator): Promise<void> {
  await expect(label).toBeVisible();
  const layout = await label.evaluate((element) => {
    const panel = element.closest('.analysis-panel')!;
    const panelBox = panel.getBoundingClientRect();
    const range = document.createRange();
    range.selectNodeContents(element);
    return {
      left: Math.max(0, panelBox.left), right: Math.min(document.documentElement.clientWidth, panelBox.right),
      scrollWidth: element.scrollWidth, clientWidth: element.clientWidth,
      lines: Array.from(range.getClientRects(), (rect) => ({ left: rect.left, right: rect.right })),
    };
  });
  expect(layout.lines.length, 'The complete label must render text.').toBeGreaterThan(0);
  expect(layout.scrollWidth, 'The label must not clip its own text.').toBeLessThanOrEqual(layout.clientWidth + 1);
  for (const line of layout.lines) {
    expect(line.left, 'Every text line must stay inside the coverage panel and viewport.').toBeGreaterThanOrEqual(layout.left - 1);
    expect(line.right, 'Every text line must stay inside the coverage panel and viewport.').toBeLessThanOrEqual(layout.right + 1);
  }
}

for (const preferences of [{ language: 'en', theme: 'light' }, { language: 'zh', theme: 'dark' }] as const) {
  test.describe(`${preferences.language} ${preferences.theme} mobile coverage`, () => {
    test.use({ viewport: { width: preferences.language === 'en' ? 375 : 390, height: 844 }, preferences });
    test('all libraries retain complete names and six readable states without horizontal discovery', async ({ page, api }, testInfo) => {
      installCoverage(api);
      const chinese = preferences.language === 'zh';
      const coverage = await openCoverage(page, chinese ? '覆盖率' : 'Coverage');
      const cards = coverage.getByRole('article');
      await expect(cards).toHaveCount(3);
      await expect(coverage.getByRole('table')).toHaveCount(0);
      const film = coverage.getByRole('article', { name: longName, exact: true });
      const heading = film.getByRole('heading', { name: longName, exact: true });
      await expect(heading).toHaveAttribute('translate', 'no');
      await expectReadableText(heading);
      await expect(film.locator('dt strong')).toHaveText(chinese
        ? ['进度预览', '片头检测', '片尾检测', '背景短片', '音轨波形', '字幕时间轴']
        : ['Seek previews', 'Intro detection', 'Credits detection', 'Background previews', 'Audio waveforms', 'Subtitle timelines']);
      await expect(film.locator('dd')).toHaveText(chinese
        ? ['已启用', '不适用', '未启用', '已启用', '未启用', '未提供']
        : ['Enabled', 'Not applicable', 'Disabled', 'Enabled', 'Disabled', 'Not reported']);
      const series = coverage.getByRole('article', { name: 'Settings', exact: true });
      await expect(series.getByRole('heading')).toHaveText('Settings');
      await expect(series.locator('dd')).toHaveText(Array(6).fill(chinese ? '未提供' : 'Not reported'));
      const music = coverage.getByRole('article', { name: coverageLibraries[2].Name, exact: true });
      await expect(music.locator('dd')).toHaveText(Array(6).fill(chinese ? '不适用' : 'Not applicable'));
      for (const card of await cards.all()) {
        await expect(card.locator('dt')).toHaveCount(6);
        for (const label of await card.locator('h3, dt strong, dt > span, .analysis-feature-state').all()) await expectReadableText(label);
      }
      await expect(coverage.getByRole('switch')).toHaveCount(0);
      await noOverflow(page);
      expect(api.writes()).toEqual([]);
      await evidence(page, testInfo, `design2-coverage-mobile-${preferences.language}-${preferences.theme}`);
    });
  });
}

test.describe('desktop coverage and existing controls', () => {
  test.use({ viewport: { width: 1440, height: 900 }, preferences: { language: 'en', theme: 'light' } });

  test('the matrix retains every library and status while viewport changes remain read only', async ({ page, api }, testInfo) => {
    installCoverage(api);
    const coverage = await openCoverage(page);
    const matrix = coverage.getByRole('table', { name: 'Library automation', exact: true });
    await expect(matrix).toBeVisible();
    await expect(matrix.locator('thead th')).toHaveText(['Analysis', ...coverageLibraries.map((value) => value.Name)]);
    await expect(matrix.locator('tbody tr')).toHaveCount(6);
    await expect(matrix.locator('tbody tr').first().locator('td')).toHaveText(['Enabled', 'Not reported', 'Not applicable']);
    await expect(matrix.locator('tbody tr').nth(1).locator('td')).toHaveText(['Not applicable', 'Not reported', 'Not applicable']);
    await expect(matrix.locator('tbody tr').last().locator('td')).toHaveText(['Not reported', 'Not reported', 'Not applicable']);
    await expect(coverage.getByRole('article')).toHaveCount(0);
    const reads = { libraries: api.reads(librariesPath).length, overview: api.reads(overviewPath).length };
    await page.setViewportSize({ width: 375, height: 844 });
    await expect(coverage.getByRole('article')).toHaveCount(3);
    await expect(coverage.getByRole('table')).toHaveCount(0);
    await expectReadableText(coverage.getByRole('article', { name: longName, exact: true }).getByRole('heading'));
    await page.setViewportSize({ width: 1440, height: 900 });
    await expect(matrix).toBeVisible();
    expect(api.reads(librariesPath)).toHaveLength(reads.libraries);
    expect(api.reads(overviewPath)).toHaveLength(reads.overview);
    expect(api.writes()).toEqual([]);
    await noOverflow(page);
    await evidence(page, testInfo, 'design2-coverage-desktop-matrix');
    await coverage.getByRole('button', { name: 'Library settings', exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/media\/libraries$/);
    expect(api.writes()).toEqual([]);
  });

  test('a failed refresh keeps the previous mobile states and guards configuration fields until recovery', async ({ page, api }) => {
    await page.setViewportSize({ width: 375, height: 844 });
    installCoverage(api);
    const coverage = await openCoverage(page);
    const film = coverage.getByRole('article', { name: longName, exact: true });
    const previousStates = await film.locator('dd').allTextContents();
    api.handlers.set(`GET ${librariesPath}`, (route) => json(route, { Error: { Code: 'unavailable', Message: 'Library settings could not be refreshed.' } }, 503));
    await page.getByRole('button', { name: 'Refresh analysis status', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('Library settings could not be refreshed.');
    await expect(page.getByText('Showing the last successful update.', { exact: true })).toBeVisible();
    await expect(film.locator('dd')).toHaveText(previousStates);
    await expectReadableText(film.getByRole('heading'));
    await page.getByRole('button', { name: 'Configure', exact: true }).click();
    const configuration = page.getByRole('dialog', { name: 'Analysis configuration', exact: true });
    await expect(configuration.getByRole('textbox', { name: 'Preview interval (seconds)', exact: true })).toBeDisabled();
    await expect(configuration.getByRole('button', { name: 'Save analysis configuration', exact: true })).toBeDisabled();
    await configuration.getByRole('button', { name: 'Close', exact: true }).click();
    installCoverage(api);
    await page.getByRole('button', { name: 'Refresh analysis status', exact: true }).click();
    await expect(page.getByText('Showing the last successful update.', { exact: true })).toHaveCount(0);
    await expect(page.locator('.analysis-page')).toHaveAttribute('aria-busy', 'false');
    await expect(film.locator('dd')).toHaveText(previousStates);
    expect(api.writes()).toEqual([]);
  });

  test('a denied library read exposes its error without inventing coverage states', async ({ page, api }) => {
    await page.setViewportSize({ width: 375, height: 844 });
    api.handlers.set(`GET ${librariesPath}`, (route) => json(route, { Error: { Code: 'forbidden', Message: 'Library settings are not authorized.' } }, 403));
    const coverage = await openCoverage(page);
    await expect(page.getByRole('alert')).toContainText('Library settings are not authorized.');
    await expect(coverage.getByText('Library settings unavailable', { exact: true })).toBeVisible();
    await expect(coverage.getByRole('article')).toHaveCount(0);
    await expect(coverage.getByRole('table')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Configure', exact: true })).toBeDisabled();
    expect(api.writes()).toEqual([]);
  });

  test('mobile coverage preserves the configuration revision, profile payload, and CSRF protocol', async ({ page, api }) => {
    await page.setViewportSize({ width: 375, height: 844 });
    installCoverage(api);
    const initialResponse = page.waitForResponse((response) => new URL(response.url()).pathname === overviewPath && response.request().method() === 'GET');
    await openCoverage(page);
    const initial = await (await initialResponse).json() as AnalysisOverview;
    const overview: AnalysisOverview = { ...initial, Configuration: { ...initial.Configuration, Revision: '9007199254740993' } };
    api.handlers.set(`GET ${overviewPath}`, (route) => json(route, overview));
    api.handlers.set(`PUT ${configurationPath}`, (route, request) => {
      const input = request.postDataJSON() as { Revision: string; Profile: AnalysisProfile };
      overview.Configuration = { ...overview.Configuration, Revision: '9007199254740994', Profile: structuredClone(input.Profile) };
      return json(route, overview.Configuration);
    });
    await page.getByRole('button', { name: 'Refresh analysis status', exact: true }).click();
    await expect(page.getByText('Configuration revision 9007199254740993', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Configure', exact: true }).click();
    const configuration = page.getByRole('dialog', { name: 'Analysis configuration', exact: true });
    await configuration.getByRole('textbox', { name: 'Preview interval (seconds)', exact: true }).fill('20');
    await configuration.getByRole('button', { name: 'Save analysis configuration', exact: true }).click();
    await expect(configuration.getByText('Analysis configuration saved. New runs use this profile.', { exact: true })).toBeVisible();
    expect(api.writes()).toEqual([{ method: 'PUT', path: configurationPath,
      body: { Revision: '9007199254740993', Profile: { ...initial.Configuration.Profile, PreviewIntervalSeconds: 20 } }, csrf }]);
  });
});
