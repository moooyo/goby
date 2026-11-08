import type { Route } from '@playwright/test';
import { expect, json, library, seriesLibrary, test } from './dashboard-fixture';
import { evidence } from './dashboard-review';
import type { DashboardAPI } from './dashboard-fixture';
import type { LibraryStorageDirectory } from '../src/api';
import { capacityBytes, directoryCapacity, formatDirectoryCapacity, lowestAvailableDirectory, uniqueDirectoryPaths } from '../src/directoryCapacity';

const directoryPath = '/admin/v1/storage/library-directories';
const GiB = 1024n ** 3n;
const bytes = (value: bigint) => value.toString();

function directory(path: string, overrides: Partial<LibraryStorageDirectory> = {}): LibraryStorageDirectory {
  return { Path: path, Available: true, Readable: true, Writable: false, TotalBytes: bytes(100n * GiB), UsedBytes: bytes(40n * GiB), AvailableBytes: bytes(55n * GiB), ...overrides };
}

function installDirectories(api: DashboardAPI, initial: LibraryStorageDirectory[]) {
  let items = initial;
  api.handlers.set('GET ' + directoryPath, (route) => json(route, { Items: items, TotalRecordCount: items.length }));
  return (next: LibraryStorageDirectory[]) => { items = next; };
}

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'light' } });

test('capacity arithmetic preserves uint64 values, exact paths, and the 90 percent boundary', () => {
  const total = 18446744073709551615n;
  const below = total * 9n / 10n;
  const first = directory('/media/movies', { TotalBytes: bytes(total), UsedBytes: bytes(below), AvailableBytes: '9007199254740993' });
  const second = directory('/media/series', { TotalBytes: bytes(total), UsedBytes: bytes(below + 1n), AvailableBytes: '9007199254740992' });
  expect(capacityBytes(bytes(total))).toBe(total);
  expect(capacityBytes('18446744073709551616')).toBeNull();
  expect(capacityBytes('01')).toBeNull();
  expect(directoryCapacity(first)).toMatchObject({ total, used: below, available: 9007199254740993n, percent: 89.9, warning: false });
  expect(directoryCapacity(second)).toMatchObject({ used: below + 1n, percent: 90, warning: true });
  expect(formatDirectoryCapacity(directoryCapacity(first), 'en-US')).toBe('14.4 / 16 EiB');
  expect(lowestAvailableDirectory([first, second])?.Path).toBe(second.Path);
  expect(uniqueDirectoryPaths(['/media/movies', '/media/movies/', '/media/movies', '/Media/movies'])).toEqual(['/media/movies', '/media/movies/', '/Media/movies']);
  expect(directoryCapacity(directory('/media/empty', { TotalBytes: '0', UsedBytes: '0', AvailableBytes: '0' }))).toMatchObject({ percent: null, warning: false });
});

test('registered directories are deduplicated exactly and shared disk capacity is never summed', async ({ page, api }) => {
  api.handlers.set('GET /admin/v1/libraries', (route) => json(route, {
    Items: [library, { ...seriesLibrary, Paths: [library.Paths[0], ...seriesLibrary.Paths] }], TotalRecordCount: 2,
  }));
  installDirectories(api, [directory('/media/movies'), directory('/media/series')]);
  await page.goto('/admin/media/libraries');
  const main = page.getByRole('main');
  const panel = main.getByRole('region', { name: 'Media directories', exact: true });
  await expect(panel.getByRole('listitem')).toHaveCount(2);
  await expect(panel.getByRole('listitem', { name: '/media/movies', exact: true })).toContainText('40 / 100 GiB');
  await expect(panel.getByRole('listitem', { name: '/media/series', exact: true })).toContainText('40 / 100 GiB');
  await expect(panel.getByRole('listitem', { name: '/media', exact: true })).toHaveCount(0);
  await expect(panel.getByRole('region', { name: 'Storage totals', exact: true })).toHaveCount(0);
  await expect(panel).not.toContainText('200 GiB');
  await expect(panel).toContainText('these values must not be added together');
  await expect(main.getByRole('button', { name: 'Create library', exact: true }).first()).toBeEnabled();
  await panel.scrollIntoViewIfNeeded();
  await evidence(page, test.info(), 'design2-storage-registered-directories');
  await main.getByRole('button', { name: 'Create library', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Create library', exact: true });
  await expect(dialog.getByRole('listitem', { name: '/media', exact: true })).toBeVisible();
  await expect(dialog.getByRole('listitem', { name: '/media/movies', exact: true })).toHaveCount(0);
  await page.keyboard.press('Escape');
  expect(api.writes()).toEqual([]);
});

test('unknown capacity and access remain unknown while capacity is independent of readability', async ({ page, api }) => {
  const replace = installDirectories(api, [
    directory('/media/movies', { Readable: false, Writable: true, TotalBytes: null, UsedBytes: null, AvailableBytes: null }),
    directory('/media/series', { Readable: null, Writable: null, TotalBytes: null, UsedBytes: null, AvailableBytes: null }),
  ]);
  await page.goto('/admin/media/libraries');
  const panel = page.getByRole('main').getByRole('region', { name: 'Media directories', exact: true });
  const movies = panel.getByRole('listitem', { name: '/media/movies', exact: true });
  const series = panel.getByRole('listitem', { name: '/media/series', exact: true });
  await expect(movies).toContainText('Not readable');
  await expect(movies).toHaveAttribute('data-access', 'warning');
  await expect(series).toContainText('Read access unknown');
  await expect(series).toHaveAttribute('data-access', 'unknown');
  await expect(panel.getByRole('progressbar')).toHaveCount(0);
  await expect(panel).toContainText('Capacity unavailable');
  await expect(panel).not.toContainText('0%');

  replace([
    directory('/media/movies', { Available: false, Readable: false, UsedBytes: bytes(90n * GiB), AvailableBytes: bytes(5n * GiB) }),
    directory('/media/series', { UsedBytes: null, AvailableBytes: bytes(3n * GiB) }),
  ]);
  await panel.getByRole('button', { name: 'Refresh directory capacity', exact: true }).click();
  await expect(movies).toContainText('Unavailable');
  await expect(movies).toHaveAttribute('data-access', 'warning');
  await expect(movies).toHaveAttribute('data-warning', 'true');
  await expect(movies.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '90');
  await expect(movies).toContainText('90 / 100 GiB');
  await expect(series).toContainText('— / 100 GiB');
  await expect(series).toContainText('3 GiB available');
  await expect(series.getByRole('progressbar')).toHaveCount(0);
  await expect(panel).toContainText('Some directory details are unavailable. Known values are shown.');
  await panel.scrollIntoViewIfNeeded();
  await evidence(page, test.info(), 'design2-storage-ninety-percent-and-unknown-usage');
  expect(api.writes()).toEqual([]);
});

test('overview selects real available bytes including reserved space and preserves other live resources', async ({ page, api }) => {
  installDirectories(api, [
    directory('/media/movies', { UsedBytes: bytes(10n * GiB), AvailableBytes: bytes(2n * GiB) }),
    directory('/media/series', { UsedBytes: bytes(90n * GiB), AvailableBytes: bytes(8n * GiB) }),
  ]);
  await page.goto('/admin/');
  const main = page.getByRole('main');
  const metric = main.getByRole('region', { name: 'Directory with least available space status', exact: true });
  await expect(metric.locator('.ov-metric-value')).toHaveText('2GiB');
  await expect(metric).toContainText('/media/movies');
  await expect(metric.getByRole('progressbar', { name: 'Directory usage for /media/movies', exact: true })).toHaveAttribute('aria-valuenow', '10');
  await expect(metric).not.toContainText('/media/series');
  await expect(main.getByRole('region', { name: 'CPU status', exact: true })).toContainText('28');
  await expect(main.getByRole('region', { name: 'Memory status', exact: true })).toContainText('6');
  await expect(main.getByRole('region', { name: 'Transcoding status', exact: true })).toContainText('2');
  await expect(main.getByRole('region', { name: 'Recent activity', exact: true })).toContainText('Administrator signed in');
  await expect(main.getByRole('region', { name: 'Needs attention', exact: true })).toContainText('Media directory is nearly full');
  await evidence(page, test.info(), 'design2-storage-overview-real-minimum-available');
  expect(api.writes()).toEqual([]);
});

test('failed directory refresh retains visibly stale data and leaves library access usable', async ({ page, api }) => {
  installDirectories(api, [directory('/media/movies'), directory('/media/series')]);
  await page.goto('/admin/media/libraries');
  const main = page.getByRole('main');
  const panel = main.getByRole('region', { name: 'Media directories', exact: true });
  const movies = panel.getByRole('listitem', { name: '/media/movies', exact: true });
  await expect(movies).toContainText('40 / 100 GiB');
  const libraryReads = api.reads('/admin/v1/libraries').length;
  const rootReads = api.reads('/admin/v1/storage/roots').length;
  api.handlers.set('GET ' + directoryPath, (route) => json(route, { Error: { Code: 'capacity_unavailable', Message: 'Directory capacity is temporarily unavailable.' } }, 503));
  await panel.getByRole('button', { name: 'Refresh directory capacity', exact: true }).click();
  await expect(panel).toHaveAttribute('data-stale', 'true');
  await expect(panel.getByRole('alert')).toContainText('Directory observations are stale.');
  await expect(movies).toContainText('40 / 100 GiB');
  await expect(movies).toContainText('Last successful sample');
  await expect(main.getByRole('button', { name: 'Create library', exact: true }).first()).toBeEnabled();
  expect(api.reads('/admin/v1/libraries')).toHaveLength(libraryReads);
  expect(api.reads('/admin/v1/storage/roots')).toHaveLength(rootReads);

  await page.reload();
  await expect(panel.getByRole('alert')).toContainText('Directory observations unavailable.');
  await expect(panel).toHaveAttribute('data-stale', 'false');
  await expect(panel.getByRole('progressbar')).toHaveCount(0);
  await expect(movies).toContainText('Capacity unavailable');
  expect(api.writes()).toEqual([]);
});

test('overview retains a stale minimum and removes the capacity bar for a later unknown observation', async ({ page, api }) => {
  installDirectories(api, [directory('/media/movies', { AvailableBytes: bytes(2n * GiB) }), directory('/media/series')]);
  await page.goto('/admin/');
  const main = page.getByRole('main');
  const metric = main.getByRole('region', { name: 'Directory with least available space status', exact: true });
  await expect(metric.locator('.ov-metric-value')).toHaveText('2GiB');
  api.handlers.set('GET ' + directoryPath, (route) => json(route, { Error: { Code: 'capacity_unavailable', Message: 'Directory capacity is temporarily unavailable.' } }, 503));
  await main.getByRole('button', { name: 'Refresh overview', exact: true }).click();
  await expect(metric).toHaveAttribute('data-stale', 'true');
  await expect(metric).toContainText('Stale');
  await expect(metric.locator('.ov-metric-value')).toHaveText('2GiB');
  await expect(metric).toContainText('/media/movies');
  await evidence(page, test.info(), 'design2-storage-overview-stale-minimum');

  installDirectories(api, [directory('/media/movies', { TotalBytes: null, UsedBytes: null, AvailableBytes: null }), directory('/media/series', { TotalBytes: null, UsedBytes: null, AvailableBytes: null })]);
  await main.getByRole('button', { name: 'Refresh overview', exact: true }).click();
  await expect(metric).toHaveAttribute('data-stale', 'false');
  await expect(metric.locator('.ov-metric-value')).toHaveText('—');
  await expect(metric.getByRole('progressbar')).toHaveCount(0);
  await expect(metric).toContainText('2 available-space observations unknown');
  await expect(main.getByRole('region', { name: 'Needs attention', exact: true })).toContainText('Checks incomplete');

  installDirectories(api, [directory('/media/movies', { TotalBytes: null, UsedBytes: null, AvailableBytes: bytes(2n * GiB) })]);
  await main.getByRole('button', { name: 'Refresh overview', exact: true }).click();
  await expect(metric.locator('.ov-metric-value')).toHaveText('2GiB');
  await expect(metric.getByRole('progressbar')).toHaveCount(0);
  await expect(metric).toContainText('Used space unknown');
});

for (const path of ['/admin/media/libraries', '/admin/']) test(`pending directory requests abort when leaving ${path}`, async ({ page, api }) => {
  const held: Route[] = [];
  await page.addInitScript(() => {
    const fetch = window.fetch.bind(window);
    (window as unknown as { directoryAborts: number }).directoryAborts = 0;
    window.fetch = (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      if (url.includes('/storage/library-directories')) init?.signal?.addEventListener('abort', () => {
        (window as unknown as { directoryAborts: number }).directoryAborts += 1;
      }, { once: true });
      return fetch(input, init);
    };
  });
  api.handlers.set('GET ' + directoryPath, async (route) => { held.push(route); });
  try {
    await page.goto(path);
    const main = page.getByRole('main');
    await expect.poll(() => held.length).toBeGreaterThan(0);
    if (path.includes('/libraries')) {
      await expect(main.getByText('Feature films', { exact: true })).toBeVisible();
      await expect(main.getByRole('button', { name: 'Create library', exact: true }).first()).toBeEnabled();
    } else await expect(main.getByRole('region', { name: 'CPU status', exact: true })).toContainText('28');
    await page.getByRole('navigation', { name: 'Administration', exact: true }).getByRole('link', { name: /^Users(?:\s|$)/ }).click();
    await expect(main.getByRole('heading', { name: 'Users', exact: true })).toBeVisible();
    await expect.poll(() => page.evaluate(() => (window as unknown as { directoryAborts: number }).directoryAborts)).toBeGreaterThan(0);
    await Promise.all(held.map((route) => json(route, { Items: [directory('/media/movies')], TotalRecordCount: 1 }).catch(() => {})));
    await expect(main.getByRole('region', { name: 'Media directories', exact: true })).toHaveCount(0);
    expect(api.writes()).toEqual([]);
  } finally {
    await Promise.all(held.map((route) => route.abort().catch(() => {})));
  }
});
