import type { Locator, Page, TestInfo } from '@playwright/test';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { expect, library } from './dashboard-fixture';

export const destinations = [
  { path: '/admin/', legacy: '/admin/overview', nav: '/admin/', label: 'Overview', text: 'Goby living room' },
  { path: '/admin/media/libraries', legacy: '/admin/libraries', nav: '/admin/media/libraries', label: 'Libraries', text: 'Feature films' },
  { path: '/admin/media/artwork', legacy: '/admin/artwork', nav: '/admin/media/libraries', label: 'Libraries', text: 'Alex Morgan' },
  { path: '/admin/media/collections', legacy: '/admin/collections', nav: '/admin/media/collections', label: 'Collections', text: 'Weekend favorites' },
  { path: '/admin/media/analysis', legacy: '/admin/media-analysis', nav: '/admin/media/analysis', label: 'Media analysis', text: 'Analysis cache' },
  { path: '/admin/access/users', legacy: '/admin/users', nav: '/admin/access/users', label: 'Users', text: 'Family viewer' },
  { path: '/admin/access/devices', legacy: '/admin/devices', nav: '/admin/access/devices', label: 'Devices', text: 'Living room TV' },
  { path: '/admin/access/sessions', legacy: '/admin/sessions', nav: '/admin/access/sessions', label: 'Sessions', text: 'Desktop browser' },
  { path: '/admin/access/api-keys', legacy: '/admin/api-keys', nav: '/admin/access/api-keys', label: 'API keys', text: 'Home automation' },
  { path: '/admin/system/tasks', legacy: '/admin/tasks', nav: '/admin/system/tasks', label: 'Tasks', text: 'Scan media libraries' },
  { path: '/admin/system/observability', legacy: '/admin/observability', nav: '/admin/system/observability', label: 'Activity & logs', text: 'Administrator signed in' },
  { path: '/admin/system/notifications', legacy: '/admin/notifications', nav: '/admin/system/notifications', label: 'Notifications', text: 'No notification targets yet' },
  { path: '/admin/system/backups', legacy: '/admin/backups', nav: '/admin/system/backups', label: 'Backups & recovery', text: 'Restore backup' },
  { path: '/admin/settings/general', legacy: '/admin/settings', nav: '/admin/settings/general', label: 'General', text: 'Server identity' },
  { path: '/admin/settings/transcode', legacy: '/admin/settings-transcode', nav: '/admin/settings/transcode', label: 'Transcoding', text: 'Maximum bitrate' },
  { path: '/admin/settings/hardware', legacy: '/admin/settings-hardware', nav: '/admin/settings/transcode', label: 'Transcoding', text: 'Hardware acceleration' },
  { path: '/admin/settings/metadata', legacy: '/admin/settings-metadata', nav: '/admin/settings/metadata', label: 'Metadata & subtitles', text: 'Metadata' },
  { path: '/admin/settings/providers', legacy: '/admin/providers', nav: '/admin/settings/metadata', label: 'Metadata & subtitles', text: 'The Movie Database' },
  { path: `/admin/media/libraries/${library.Id}/items`, legacy: `/admin/libraries/${library.Id}/items`, nav: '/admin/media/libraries', label: 'Libraries', text: 'The feature film' },
] as const;
export type Destination = typeof destinations[number];

export function navigation(page: Page): Locator { return page.locator('nav[aria-label="Administration"]'); }
export function sidebar(page: Page): Locator { return page.locator('aside').filter({ has: navigation(page) }); }

export async function ready(page: Page, destination: Destination, checkCopy = true, options: { allowPendingStorage?: boolean } = {}): Promise<void> {
  const main = page.getByRole('main');
  await expect(main.getByRole('heading', { level: 1 })).toHaveCount(1);
  await expect(main.getByRole('heading', { level: 1 })).toBeVisible();
  await expect(navigation(page).locator('a[aria-current="page"]')).toHaveAttribute('href', destination.nav);
  if (checkCopy) await expect(main.getByText(destination.text, { exact: false }).filter({ visible: true }).first()).toBeVisible();
  const pending = options.allowPendingStorage ? '[aria-busy="true"]:not(.media-storage-summary)' : '[aria-busy="true"]';
  await expect(main.locator(pending)).toHaveCount(0);
  await expect(main).not.toContainText(/invalid response|response is incomplete|No dashboard fixture|Loading settings/);
}

export async function navigate(page: Page, path: string): Promise<void> {
  const opener = page.getByRole('button', { name: /^(Open navigation|打开导航)$/ });
  if (await opener.isVisible()) await opener.click();
  await navigation(page).locator(`a[href="${path}"]`).click();
}

export async function noOverflow(page: Page): Promise<void> {
  const dimensions = await page.evaluate(() => {
    const main = document.getElementById('main-content')!;
    return { document: document.documentElement.scrollWidth, width: window.innerWidth, main: main.scrollWidth, mainWidth: main.clientWidth };
  });
  expect(dimensions.document, 'The document must not scroll sideways.').toBeLessThanOrEqual(dimensions.width);
  expect(dimensions.main, 'Page content must fit its scroll container.').toBeLessThanOrEqual(dimensions.mainWidth + 1);
}

export async function evidence(page: Page, testInfo: TestInfo, name: string): Promise<void> {
  await page.evaluate(() => document.fonts.ready);
  const directory = process.env.GOBY_DESIGN_EVIDENCE_DIR;
  const filename = directory ? resolve(directory, `${name}.png`) : testInfo.outputPath(`${name}.png`);
  if (directory) await mkdir(directory, { recursive: true });
  await page.screenshot({ path: filename, animations: 'disabled' });
  const geometry = await page.evaluate(() => {
    const box = (element: Element | null) => {
      if (!element) return null;
      const { x, y, width, height } = element.getBoundingClientRect();
      return { x, y, width, height };
    };
    const main = document.getElementById('main-content');
    const heading = main?.querySelector('h1');
    return {
      theme: document.documentElement.dataset.theme, language: document.documentElement.lang,
      viewport: { width: innerWidth, height: innerHeight }, main: box(main),
      navigation: box(document.querySelector('nav[aria-label="Administration"]')),
      sidebar: box(document.querySelector('nav[aria-label="Administration"]')?.closest('aside') ?? null),
      heading: box(heading ?? null), headingFont: heading ? getComputedStyle(heading).fontFamily : '',
      headingSize: heading ? getComputedStyle(heading).fontSize : '', background: getComputedStyle(document.body).backgroundColor,
      scroll: main ? { top: main.scrollTop, height: main.scrollHeight, visibleHeight: main.clientHeight } : null,
    };
  });
  await writeFile(filename.replace(/\.png$/, '.json'), `${JSON.stringify(geometry, null, 2)}\n`);
  await testInfo.attach(name, { path: filename, contentType: 'image/png' });
}
