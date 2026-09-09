import { constants } from 'node:fs';
import { lstat, open, realpath } from 'node:fs/promises';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import type { APIRequestContext, Locator, Page, Response } from '@playwright/test';

interface RefreshFixture {
  Marker: string;
  RunId: string;
  Origin: string;
  Library: { Id: string; Name: string };
  ItemId: string;
  InitialJobIds: string[];
  ResultPath: string;
}

interface RefreshJob {
  Id: string;
  LibraryId: string;
  ForceProbe: boolean;
  Status: string;
  Error: string;
  Scanned: number;
  Added: number;
  Updated: number;
  FinishedAt: string | null;
}

const marker = 'goby-media-refresh-fixtures-v1';
const fixturePattern = /^\/opt\/goby-test\/exec-scratch\/goby-media-refresh-([0-9]{8}_[0-9]{6}_[0-9a-f]{10})\/browser\/media-refresh-fixture\.json$/;
const identifier = /^[0-9a-f]{32}$/;
const refreshDialog = (page: Page): Locator => page.getByRole('dialog', { name: 'Refresh media details?', exact: true });

async function loadFixture(filename: string, baseURL: string): Promise<RefreshFixture> {
  const match = fixturePattern.exec(filename);
  if (process.platform !== 'linux' || process.getuid?.() !== 0 || !match
    || filename !== path.resolve(filename) || await realpath(filename) !== filename) {
    throw new Error('Media refresh requires the isolated Linux runner and its canonical private manifest.');
  }
  for (const directory of [path.dirname(filename), path.dirname(path.dirname(filename))]) {
    const status = await lstat(directory);
    if (!status.isDirectory() || status.isSymbolicLink() || status.uid !== 0 || (status.mode & 0o777) !== 0o700) {
      throw new Error('The refresh fixture directories must be private and root owned.');
    }
  }
  const file = await open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  let fixture: RefreshFixture;
  try {
    const status = await file.stat();
    if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o600 || status.nlink !== 1
      || status.size > 64 * 1024 || await realpath(`/proc/self/fd/${file.fd}`) !== filename) {
      throw new Error('The refresh manifest must be a bounded private regular file.');
    }
    fixture = JSON.parse(await file.readFile('utf8')) as RefreshFixture;
  } finally {
    await file.close();
  }
  const origin = new URL(baseURL);
  if (!fixture || fixture.Marker !== marker || fixture.RunId !== match[1]
    || fixture.RunId !== process.env.GOBY_MEDIA_REFRESH_RUN_ID || fixture.Origin !== origin.origin
    || origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || !origin.port
    || ['5432', '15432', '18096', '18097'].includes(origin.port)
    || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash
    || !fixture.Library || !identifier.test(fixture.Library.Id)
    || fixture.Library.Name !== `Media refresh ${fixture.RunId}` || !identifier.test(fixture.ItemId)
    || !Array.isArray(fixture.InitialJobIds) || fixture.InitialJobIds.length !== 3
    || !fixture.InitialJobIds.every((id) => identifier.test(id)) || new Set(fixture.InitialJobIds).size !== 3
    || fixture.ResultPath !== path.join(path.dirname(filename), 'media-refresh-result.json')) {
    throw new Error('The refresh fixture does not match the disposable run contract.');
  }
  return fixture;
}

async function readJobs(request: APIRequestContext, fixture: RefreshFixture): Promise<RefreshJob[]> {
  const response = await request.get('/admin/v1/jobs');
  expect(response.status()).toBe(200);
  const result = await response.json() as { Items: RefreshJob[]; TotalRecordCount: number };
  expect(Array.isArray(result.Items)).toBe(true);
  expect(result.TotalRecordCount).toBe(result.Items.length);
  for (const job of result.Items) {
    expect(identifier.test(job.Id)).toBe(true);
    expect(job.LibraryId).toBe(fixture.Library.Id);
    expect(typeof job.ForceProbe).toBe('boolean');
  }
  return result.Items;
}

async function readStarted(response: Response, fixture: RefreshFixture, forceProbe: boolean): Promise<RefreshJob> {
  expect(response.status()).toBe(202);
  const result = await response.json() as { Job: RefreshJob };
  expect(identifier.test(result.Job.Id)).toBe(true);
  expect(result.Job.LibraryId).toBe(fixture.Library.Id);
  expect(result.Job.ForceProbe).toBe(forceProbe);
  return result.Job;
}

async function completedTask(page: Page, request: APIRequestContext, fixture: RefreshFixture,
  job: RefreshJob, forceProbe: boolean): Promise<RefreshJob> {
  const card = page.locator(`[data-job-id="${job.Id}"]`);
  await expect(card.getByRole('heading', { name: fixture.Library.Name, exact: true })).toBeVisible();
  await expect(card.getByText(forceProbe ? 'Media details refresh' : 'Library scan', { exact: true })).toBeVisible();
  await expect(card.getByText('Completed', { exact: true })).toBeVisible({ timeout: 65_000 });
  await expect(card.getByRole('button', { name: 'Cancel task', exact: true })).toHaveCount(0);
  const result = (await readJobs(request, fixture)).find((entry) => entry.Id === job.Id);
  expect(result).toBeDefined();
  if (!result) throw new Error('An acknowledged refresh job disappeared.');
  expect(result.Status).toBe('completed');
  expect(result.ForceProbe).toBe(forceProbe);
  expect(result.Error).toBe('');
  expect(result.Scanned).toBe(1);
  expect(result.FinishedAt).toBeTruthy();
  return result;
}

async function checkLayout(page: Page, dialog?: Locator): Promise<void> {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  if (dialog) {
    const bounds = await dialog.boundingBox();
    expect(bounds).not.toBeNull();
    if (!bounds) throw new Error('The refresh dialog has no visible bounds.');
    const viewport = page.viewportSize();
    if (!viewport) throw new Error('The browser viewport is unavailable.');
    expect(bounds.x).toBeGreaterThanOrEqual(0);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(viewport.width);
    expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  }
}

// All writes target the fresh run-owned database. The Python runner prepares
// real media and nonempty metadata/user state, proves unchanged normal scans
// retain a deliberately missing same-version index, and verifies restoration
// and preservation after this journey and a real application restart.
test('explicit media refresh confirms intent, preserves normal scan, and persists task modes', async ({ page, context }, testInfo) => {
  test.skip(process.env.GOBY_SMOKE_MEDIA_REFRESH_DISPOSABLE_DATABASE !== '1'
    || process.env.GOBY_SMOKE_MEDIA_REFRESH_DEDICATED_ADMIN !== '1',
  'The isolated runner must confirm its disposable database and dedicated administrator.');
  test.setTimeout(210_000);
  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  const baseURL = process.env.GOBY_SMOKE_BASE_URL;
  const manifest = process.env.GOBY_SMOKE_MEDIA_REFRESH_FIXTURE_MANIFEST;
  if (!name || !password || !baseURL || !manifest) throw new Error('The private refresh runner environment is incomplete.');
  const fixture = await loadFixture(manifest, baseURL);
  expect(new URL(testInfo.project.use.baseURL ?? '').origin).toBe(fixture.Origin);
  expect(name).toBe(`m4g-admin-${fixture.RunId}`);
  const scanPath = `/admin/v1/libraries/${fixture.Library.Id}/scan`;
  const mutations: { body: string | null; csrf: boolean; pathname: string; query: string }[] = [];
  const pageErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (request.method() === 'POST' && /^\/admin\/v1\/libraries\/[^/]+\/scan$/.test(url.pathname)) {
      mutations.push({ body: request.postData(), csrf: Boolean(request.headers()['x-csrf-token']),
        pathname: url.pathname, query: url.search });
    }
  });
  const scanResponse = () => page.waitForResponse((response) => response.request().method() === 'POST'
    && new URL(response.url()).pathname === scanPath);
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto('/admin/libraries');
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
  await page.getByLabel(/^Username/).fill(name);
  await page.getByLabel(/^Password/).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Libraries', exact: true })).toBeVisible();
  const library = page.getByRole('list', { name: 'Media libraries' }).getByRole('listitem')
    .filter({ has: page.getByRole('heading', { name: fixture.Library.Name, exact: true }) });
  const refresh = () => library.getByRole('button', { name: `Refresh media details for ${fixture.Library.Name}`, exact: true });
  await expect(library).toBeVisible();
  await expect(library.getByRole('button', { name: 'Scan library', exact: true })).toBeEnabled();
  await expect(refresh()).toBeEnabled();
  const metadataRoute = `/admin/v1/items/${fixture.ItemId}/metadata`;
  const initialMetadataResponse = await context.request.get(metadataRoute);
  expect(initialMetadataResponse.status()).toBe(200);
  const initialMetadata = await initialMetadataResponse.json() as Record<string, unknown>;
  expect(Object.keys(initialMetadata.Overrides as object).length).toBeGreaterThan(0);
  expect(Object.keys(initialMetadata.LockedValues as object).length).toBeGreaterThan(0);
  const initialJobs = await readJobs(context.request, fixture);
  expect(initialJobs.map((job) => job.Id).sort()).toEqual([...fixture.InitialJobIds].sort());
  expect(initialJobs.every((job) => job.ForceProbe === false && job.Status === 'completed')).toBe(true);
  await checkLayout(page);
  await page.screenshot({ path: testInfo.outputPath('media-refresh-libraries-desktop.png'), fullPage: true, animations: 'disabled' });

  await test.step('cancel and escape the mobile confirmation without submitting any scan', async () => {
    await page.setViewportSize({ width: 390, height: 844 });
    await checkLayout(page);
    await refresh().click();
    const dialog = refreshDialog(page);
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText(fixture.Library.Name, { exact: true })).toBeVisible();
    await expect(dialog.getByText(/re-reads every media file/)).toBeVisible();
    await expect(dialog.getByText(/edited metadata and play history are kept/)).toBeVisible();
    await expect(dialog.getByText(/Some formats do not support playback indexes/)).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeFocused();
    await checkLayout(page, dialog);
    await page.screenshot({ path: testInfo.outputPath('media-refresh-dialog-mobile.png'), fullPage: true, animations: 'disabled' });
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await expect(refresh()).toBeFocused();
    await refresh().click();
    await expect(dialog).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(dialog).not.toBeVisible();
    expect(await readJobs(context.request, fixture)).toEqual(initialJobs);
    expect(mutations).toHaveLength(0);
  });

  await page.setViewportSize({ width: 1440, height: 1000 });
  const normalResponse = scanResponse();
  await library.getByRole('button', { name: 'Scan library', exact: true }).click();
  const normal = await readStarted(await normalResponse, fixture, false);
  expect(mutations).toHaveLength(1);
  expect(mutations[0]?.body).toBeNull();
  await expect(refreshDialog(page)).not.toBeVisible();
  await page.getByRole('link', { name: 'Tasks', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Tasks', exact: true })).toBeVisible();
  await completedTask(page, context.request, fixture, normal, false);
  await page.getByRole('button', { name: 'Manage libraries', exact: true }).click();
  await expect(library).toBeVisible();

  await page.setViewportSize({ width: 390, height: 844 });
  await refresh().click();
  const dialog = refreshDialog(page);
  await expect(dialog).toBeVisible();
  const explicitResponse = scanResponse();
  await dialog.getByRole('button', { name: 'Refresh media details', exact: true }).click();
  const forced = await readStarted(await explicitResponse, fixture, true);
  expect(forced.Id).not.toBe(normal.Id);
  expect(mutations).toHaveLength(2);
  expect(JSON.parse(mutations[1]?.body ?? 'null')).toEqual({ ForceProbe: true });
  expect(mutations.every((mutation) => mutation.pathname === scanPath && mutation.query === '' && mutation.csrf)).toBe(true);
  await expect(dialog).not.toBeVisible();
  await page.getByRole('button', { name: 'View tasks', exact: true }).first().click();
  await expect(page.getByRole('heading', { name: 'Tasks', exact: true })).toBeVisible();
  const completed = await completedTask(page, context.request, fixture, forced, true);
  expect(completed.Updated).toBe(1);
  await checkLayout(page);
  await page.screenshot({ path: testInfo.outputPath('media-refresh-tasks-mobile.png'), fullPage: true, animations: 'disabled' });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await checkLayout(page);
  await page.screenshot({ path: testInfo.outputPath('media-refresh-tasks-desktop.png'), fullPage: true, animations: 'disabled' });
  await page.reload();
  await completedTask(page, context.request, fixture, forced, true);
  await completedTask(page, context.request, fixture, normal, false);
  const finalJobs = await readJobs(context.request, fixture);
  expect(finalJobs).toHaveLength(5);
  expect(finalJobs.filter((job) => job.ForceProbe).map((job) => job.Id)).toEqual([forced.Id]);
  const finalMetadata = await context.request.get(metadataRoute);
  expect(finalMetadata.status()).toBe(200);
  expect(await finalMetadata.json()).toEqual(initialMetadata);
  expect(mutations).toHaveLength(2);
  expect((await page.locator('body').innerText()).includes(password)).toBe(false);
  expect(pageErrors).toEqual([]);

  const result = await open(fixture.ResultPath, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try {
    await result.writeFile(`${JSON.stringify({ Marker: marker, RunId: fixture.RunId, CancelPosts: 0,
      ScanPostCount: mutations.length, NormalJobId: normal.Id, NormalForceProbe: false,
      RefreshJobId: forced.Id, RefreshForceProbe: true, MobileOverflow: false,
      DialogCancelAndEscape: true, TaskModesAfterReload: true, NonemptyMetadataPreserved: true })}\n`);
    await result.sync();
  } finally {
    await result.close();
  }
});
