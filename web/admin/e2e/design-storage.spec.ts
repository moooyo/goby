import type { Route } from '@playwright/test';
import { expect, json, library, seriesLibrary, test } from './dashboard-fixture';
import { evidence } from './dashboard-review';
import type { LibraryStorageDirectoriesResponse, LibraryStorageDirectory } from '../src/api';

// Directory access and byte measurements have separate contracts. These explicit
// frontend fixtures never establish filesystem or backend telemetry behavior.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'light' } });
const directoryPath = '/admin/v1/storage/library-directories';
const GiB = 1024n ** 3n;
const moviePath = library.Paths[0];
const childPath = moviePath + '/subdir';
const seriesPath = seriesLibrary.Paths[0];

function measurement(path: string, overrides: Partial<LibraryStorageDirectory> = {}): LibraryStorageDirectory {
  return {
    Path: path, Available: true, Readable: true, Writable: false,
    TotalBytes: (100n * GiB).toString(), UsedBytes: (40n * GiB).toString(), AvailableBytes: (55n * GiB).toString(),
    ...overrides,
  };
}

function observations(items: LibraryStorageDirectory[]): LibraryStorageDirectoriesResponse {
  return { Items: items, TotalRecordCount: items.length };
}

test('registered directory measurements match exact paths and preserve unknown or partial values without summing shared capacity', async ({ page, api }, testInfo) => {
  api.handlers.set('GET /admin/v1/libraries', (route) => json(route, {
    Items: [{ ...library, Paths: [moviePath, childPath] }, seriesLibrary], TotalRecordCount: 2,
  }));
  // Conflicting legacy telemetry must never fill a missing registered observation.
  api.status.Storage = {
    TotalBytes: 900 * 2 ** 30, UsedBytes: 800 * 2 ** 30, Complete: true,
    Volumes: [{ Path: moviePath, TotalBytes: 900 * 2 ** 30, UsedBytes: 800 * 2 ** 30, Available: true }],
  };
  let response = observations([measurement(moviePath), measurement(childPath), measurement(seriesPath)]);
  api.handlers.set('GET ' + directoryPath, (route) => json(route, response));
  await page.goto('/admin/media/libraries');
  const main = page.getByRole('main');
  const directories = main.getByRole('region', { name: 'Media directories', exact: true });
  const parent = directories.getByRole('listitem', { name: moviePath, exact: true });
  const child = directories.getByRole('listitem', { name: childPath, exact: true });
  const series = directories.getByRole('listitem', { name: seriesPath, exact: true });
  const refresh = directories.getByRole('button', { name: 'Refresh directory capacity', exact: true });
  await expect(directories.getByRole('listitem')).toHaveCount(3);
  for (const row of [parent, child, series]) await expect(row).toContainText('40 / 100 GiB');
  await expect(parent.getByRole('progressbar', { name: 'Directory usage for ' + moviePath, exact: true })).toHaveAttribute('aria-valuenow', '40');
  await expect(directories.getByRole('listitem', { name: '/media', exact: true })).toHaveCount(0);
  await expect(directories.getByRole('region', { name: 'Storage totals', exact: true })).toHaveCount(0);
  await expect(directories).not.toContainText('300 GiB');
  await expect(directories).not.toContainText('800 / 900 GiB');
  await expect(directories).toContainText('these values must not be added together');
  await evidence(page, testInfo, 'storage-exact-registered-paths-without-aggregate');

  // A partial response contains the registered child but no exact parent entry.
  response = observations([measurement(childPath), measurement(seriesPath)]);
  await refresh.click();
  await expect(parent).toContainText('Capacity unavailable');
  await expect(parent).toContainText('Read access unknown');
  await expect(parent.getByRole('progressbar')).toHaveCount(0);
  await expect(parent).not.toContainText('40 / 100 GiB');
  await expect(parent).not.toContainText('800 / 900 GiB');
  await expect(child).toContainText('40 / 100 GiB');
  await expect(series).toContainText('40 / 100 GiB');
  await expect(directories).toContainText('Some directory details are unavailable. Known values are shown.');
  await expect(directories.getByRole('alert')).toHaveCount(0);
  await evidence(page, testInfo, 'storage-child-measurement-never-fills-parent');

  const samples = [
    { value: measurement(moviePath, { TotalBytes: null, UsedBytes: null, AvailableBytes: null }), text: 'Capacity unavailable', percent: null },
    { value: measurement(moviePath, { UsedBytes: null, AvailableBytes: null }), text: '— / 100 GiB', percent: null },
    { value: measurement(moviePath, { TotalBytes: null, AvailableBytes: null }), text: '40 GiB / —', percent: null },
    { value: measurement(moviePath, { TotalBytes: null, UsedBytes: null, AvailableBytes: (35n * GiB).toString() }), text: '35 GiB available', percent: null },
    { value: measurement(moviePath, { AvailableBytes: null }), text: '40 / 100 GiB', percent: '40' },
    { value: measurement(moviePath, { Available: false, Readable: false }), text: '40 / 100 GiB', percent: '40' },
  ];
  for (const sample of samples) {
    response = observations([sample.value, measurement(childPath), measurement(seriesPath)]);
    const updated = page.waitForResponse((result) => new URL(result.url()).pathname === directoryPath);
    await refresh.click();
    await updated;
    await expect(refresh).toBeEnabled();
    await expect(parent).toContainText(sample.text);
    if (sample.percent == null) {
      await expect(parent.getByRole('progressbar')).toHaveCount(0);
      await expect(parent).not.toContainText('0%');
    } else await expect(parent.getByRole('progressbar')).toHaveAttribute('aria-valuenow', sample.percent);
    if (sample.value.Available === false) {
      await expect(parent).toContainText('Unavailable');
      await expect(parent).toHaveAttribute('data-access', 'warning');
    }
    await expect(directories.getByRole('alert')).toHaveCount(0);
    await expect(directories).not.toContainText('invalid response');
    await expect(directories).toHaveAttribute('data-stale', 'false');
    await expect(child).toContainText('40 / 100 GiB');
  }
  await expect(main.getByRole('button', { name: 'Create library', exact: true }).first()).toBeEnabled();
  await evidence(page, testInfo, 'storage-partial-values-and-access-remain-independent');

  // Missing available bytes cannot be reconstructed from total minus used.
  response = observations([
    measurement(moviePath, { UsedBytes: (90n * GiB).toString(), AvailableBytes: null }),
    measurement(childPath), measurement(seriesPath, { AvailableBytes: (35n * GiB).toString() }),
  ]);
  await page.goto('/admin/');
  const minimum = main.getByRole('region', { name: 'Directory with least available space status', exact: true });
  await expect(minimum.locator('.ov-metric-value')).toHaveText('35GiB');
  await expect(minimum).toContainText(seriesPath);
  await expect(minimum).toContainText('Among directories with known available space');
  await expect(minimum.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '40');
  expect(api.writes()).toEqual([]);
});

test('slow or failed directory observations never block library creation and retain a visibly stale last measurement', async ({ page, api }, testInfo) => {
  const response = observations([measurement(moviePath), measurement(seriesPath)]);
  const held: Route[] = [];
  let mode: 'held' | 'good' | 'failed' = 'held';
  api.handlers.set('GET ' + directoryPath, async (route) => {
    if (mode === 'held') { held.push(route); return; }
    if (mode === 'failed') return json(route, { Error: { Code: 'capacity_unavailable', Message: 'Directory capacity is temporarily unavailable.' } }, 503);
    return json(route, response);
  });
  try {
    await page.goto('/admin/media/libraries');
    const main = page.getByRole('main');
    const directories = main.getByRole('region', { name: 'Media directories', exact: true });
    const parent = directories.getByRole('listitem', { name: moviePath, exact: true });
    const create = main.getByRole('button', { name: 'Create library', exact: true }).first();
    await expect.poll(() => held.length).toBeGreaterThan(0);
    await expect(main.getByText('Feature films', { exact: true })).toBeVisible();
    await expect(parent).toContainText('Checking capacity…');
    await expect(parent.getByRole('progressbar')).toHaveCount(0);
    await expect(create).toBeEnabled({ timeout: 5000 });
    const directoryReads = api.reads(directoryPath).length;
    await create.click();
    const sheet = page.getByRole('dialog', { name: 'Create library', exact: true });
    await expect(sheet.getByRole('textbox', { name: 'Library name', exact: true })).toBeVisible();
    // Creation still presents deployment-allowed roots rather than capacity rows.
    await expect(sheet.getByRole('listitem', { name: '/media', exact: true })).toBeVisible();
    await expect(sheet.getByRole('listitem', { name: moviePath, exact: true })).toHaveCount(0);
    await page.keyboard.press('Escape');
    await expect(sheet).not.toBeVisible();
    expect(api.reads(directoryPath)).toHaveLength(directoryReads);
    await evidence(page, testInfo, 'storage-capacity-pending-library-creation-remains-usable');

    mode = 'good';
    await Promise.all(held.map((route) => json(route, response).catch(() => {})));
    await expect(parent).toContainText('40 / 100 GiB');
    await expect(parent.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '40');
    await expect(directories.getByRole('listitem', { name: seriesPath, exact: true })).toContainText('40 / 100 GiB');
    const libraryReads = api.reads('/admin/v1/libraries').length;
    const rootReads = api.reads('/admin/v1/storage/roots').length;
    mode = 'failed';
    await directories.getByRole('button', { name: 'Refresh directory capacity', exact: true }).click();
    await expect(directories.getByRole('alert')).toContainText('Directory capacity is temporarily unavailable.');
    await expect(directories.getByRole('alert')).toContainText('Directory observations are stale.');
    await expect(directories).toHaveAttribute('data-stale', 'true');
    await expect(parent).toContainText('40 / 100 GiB');
    await expect(parent).toContainText('Last successful sample');
    await expect(create).toBeEnabled();
    expect(api.reads('/admin/v1/libraries')).toHaveLength(libraryReads);
    expect(api.reads('/admin/v1/storage/roots')).toHaveLength(rootReads);
    await evidence(page, testInfo, 'storage-directory-stale-last-measurement');

    await page.reload();
    await expect(main.getByText('Feature films', { exact: true })).toBeVisible();
    await expect(directories.getByRole('alert')).toContainText('Directory capacity is temporarily unavailable.');
    await expect(directories.getByRole('alert')).toContainText('Directory observations unavailable.');
    await expect(directories).toHaveAttribute('data-stale', 'false');
    await expect(create).toBeEnabled();
    await expect(parent).toContainText('Capacity unavailable');
    await expect(parent.getByRole('progressbar')).toHaveCount(0);
    await expect(parent).not.toContainText('Last successful sample');
    expect(api.writes()).toEqual([]);
  } finally {
    await Promise.all(held.map((route) => route.abort().catch(() => {})));
  }
});
