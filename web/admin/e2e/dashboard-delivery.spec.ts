import { readFile, writeFile } from 'node:fs/promises';
import { expect, test, type Page } from '@playwright/test';
import type { ApplicationKeysResponse, CreateApplicationKeyResponse, Job, JobsResponse, LibraryResponse, MetadataDetail, MetadataItemsResponse, ServerSettings } from '../src/api';

interface MediaFixture {
  movie_root: string;
  tv_root: string;
  sample_path: string;
  movies: number;
  episodes: number;
}

const enabled = process.env.GOBY_DASHBOARD_DELIVERY === '1';
test.skip(!enabled, 'Enable GOBY_DASHBOARD_DELIVERY=1 for the isolated local delivery server.');
// Screenshots are captured only at reviewed points where passwords and API keys are absent.
test.use({ trace: 'off', video: 'off', screenshot: 'off', serviceWorkers: 'block', actionTimeout: 15_000 });

async function navigate(page: Page, group: string, tab?: string) {
  await page.getByRole('navigation', { name: 'Administration' }).getByRole('link', { name: group, exact: true }).click();
  if (tab) await page.getByRole('tab', { name: tab, exact: true }).click();
}

function responseFor(page: Page, method: string, path: string) {
  return page.waitForResponse((response) => response.request().method() === method && new URL(response.url()).pathname === path);
}

async function saveSettings(page: Page): Promise<ServerSettings> {
  const response = responseFor(page, 'PUT', '/admin/v1/settings');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  const saved = await response;
  expect(saved.status()).toBe(200);
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  return await saved.json() as ServerSettings;
}

test('dashboard delivery uses real media, durable settings, application keys, and responsive navigation', async ({ page, context, baseURL }, testInfo) => {
  test.setTimeout(240_000);
  const origin = new URL(baseURL ?? '');
  if (origin.href !== 'http://127.0.0.1:18096/') throw new Error('Delivery verification requires the isolated loopback server on port 18096.');
  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  if (!name || !password) throw new Error('Provide the isolated delivery administrator through GOBY_SMOKE_NAME and GOBY_SMOKE_PASSWORD.');
  const fixture = JSON.parse(await readFile(new URL('../../../.artifacts/dashboard-v2/media-fixture.json', import.meta.url), 'utf8')) as MediaFixture;
  expect(fixture.movie_root).toBe('/media/delivery');
  expect(fixture.tv_root).toBe('/media/delivery-series');
  expect(fixture.movies).toBe(2);
  expect(fixture.episodes).toBe(1);

  const run = Date.now().toString(36);
  const libraryName = `Delivery movies ${run}`;
  const seriesName = `Delivery series ${run}`;
  const pageErrors: string[] = [];
  const failedAPI: string[] = [];
  const consoleErrors: string[] = [];
  const checks: Record<string, unknown> = {};
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('console', (message) => {
    if (message.type() === 'error' && !message.text().includes('401 (Unauthorized)')) consoleErrors.push(message.text());
  });
  page.on('response', (response) => {
    const path = new URL(response.url()).pathname;
    if (path.startsWith('/admin/v1/') && response.status() >= 400 && !(path === '/admin/v1/session' && response.status() === 401)) {
      failedAPI.push(`${response.request().method()} ${path}: ${response.status()}`);
    }
  });
  const capture = async (filename: string) => {
    await page.screenshot({ path: testInfo.outputPath(filename), fullPage: true, animations: 'disabled' });
  };
  const noOverflow = async () => {
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'The dashboard must fit the viewport.').toBe(true);
    expect(await page.locator('#main-content').evaluate((element) => element.scrollWidth <= element.clientWidth + 1), 'Page content must not overflow horizontally.').toBe(true);
  };

  await page.goto('/admin/');
  await expect(page.getByRole('heading', { name: 'Sign in to Goby' })).toBeVisible();
  await page.getByLabel(/^Username/).fill(name);
  await page.getByLabel(/^Password/).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByText('Database healthy', { exact: true })).toBeVisible();
  await capture('delivery-overview-desktop.png');

  async function createLibrary(title: string, path: string, type: 'Movies' | 'TV shows'): Promise<LibraryResponse> {
    await navigate(page, 'Media', 'Libraries');
    await page.getByRole('button', { name: 'Create library', exact: true }).first().click();
    const dialog = page.getByRole('dialog', { name: 'Create library', exact: true });
    await dialog.getByLabel(/^Library name/).fill(title);
    if (type !== 'Movies') {
      await dialog.getByRole('combobox', { name: /^Content type/ }).click();
      await page.getByRole('option', { name: type, exact: true }).click();
    }
    await dialog.getByLabel(/^Media directories/).fill(path);
    await expect(dialog.getByRole('checkbox', { name: /^Scan after creating/ })).toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Import local metadata files', exact: true })).toBeChecked();
    if (type === 'Movies') await capture('delivery-create-library-desktop.png');
    const pending = responseFor(page, 'POST', '/admin/v1/libraries');
    await dialog.getByRole('button', { name: 'Create library', exact: true }).click();
    const response = await pending;
    expect(response.status()).toBe(201);
    const created = await response.json() as LibraryResponse;
    expect(created.ScanError).toBeUndefined();
    expect(Boolean(created.Job?.Id)).toBe(true);
    await expect(dialog).not.toBeVisible();
    await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
    return created;
  }

  const movies = await createLibrary(libraryName, fixture.movie_root, 'Movies');
  const series = await createLibrary(seriesName, fixture.tv_root, 'TV shows');
  await navigate(page, 'System', 'Tasks');
  await page.getByRole('tab', { name: 'Scan history', exact: true }).click();
  for (const created of [movies, series]) {
    const row = page.locator(`[data-job-id="${created.Job!.Id}"]`);
    await expect(row.getByText('Completed', { exact: true })).toBeVisible({ timeout: 60_000 });
    await expect(row.getByText('Scanned', { exact: true })).toBeVisible();
    await expect(row.getByText('Added', { exact: true })).toBeVisible();
  }
  const jobsResponse = await context.request.get('/admin/v1/jobs');
  expect(jobsResponse.status()).toBe(200);
  const jobs = await jobsResponse.json() as JobsResponse;
  const movieJob = jobs.Items.find((job) => job.Id === movies.Job!.Id) as Job;
  const seriesJob = jobs.Items.find((job) => job.Id === series.Job!.Id) as Job;
  expect(movieJob.Status).toBe('completed');
  expect(movieJob.Scanned).toBe(fixture.movies);
  expect(movieJob.Added).toBeGreaterThanOrEqual(fixture.movies);
  expect(seriesJob.Status).toBe('completed');
  expect(seriesJob.Scanned).toBe(fixture.episodes);
  checks.scanCounts = { movies: movieJob.Scanned, episodes: seriesJob.Scanned };
  await capture('delivery-scan-history-desktop.png');
  await page.locator(`[data-job-id="${movieJob.Id}"]`).getByRole('button', { name: /^Details for scan/ }).click();
  const scanDialog = page.getByRole('dialog', { name: 'Scan details', exact: true });
  await expect(scanDialog.getByText('Completed', { exact: true })).toBeVisible();
  await expect(scanDialog.getByText('Scanned', { exact: true }).locator('..').getByRole('definition')).toHaveText(String(fixture.movies));
  await capture('delivery-scan-details-desktop.png');
  await scanDialog.getByRole('button', { name: 'Close', exact: true }).click();

  await navigate(page, 'Media', 'Libraries');
  const libraryCard = page.getByRole('list', { name: 'Media libraries' }).getByRole('listitem').filter({ has: page.getByRole('heading', { name: libraryName, exact: true }) });
  await expect(libraryCard.getByText('Not scanned yet', { exact: false })).not.toBeVisible();
  await capture('delivery-libraries-desktop.png');
  const itemsPending = responseFor(page, 'GET', `/admin/v1/libraries/${movies.Library.Id}/items`);
  await libraryCard.getByRole('button', { name: `Manage items in ${libraryName}`, exact: true }).click();
  const itemsResponse = await itemsPending;
  expect(itemsResponse.status()).toBe(200);
  const items = await itemsResponse.json() as MetadataItemsResponse;
  const movieItems = items.Items.filter((item) => item.Type === 'Movie');
  expect(movieItems).toHaveLength(fixture.movies);
  expect(movieItems.some((item) => item.Path === fixture.sample_path)).toBe(true);
  await expect(page.getByRole('tab', { name: 'Libraries', exact: true })).toHaveAttribute('aria-selected', 'true');
  for (const item of movieItems) await expect(page.getByRole('table', { name: 'Library items', exact: true }).getByText(item.Name, { exact: true })).toBeVisible();
  await capture('delivery-library-items-desktop.png');

  const item = movieItems.find((entry) => entry.Path === fixture.sample_path)!;
  const metadataPath = `/admin/v1/items/${item.Id}/metadata`;
  const originalResponse = await context.request.get(metadataPath);
  expect(originalResponse.status()).toBe(200);
  const original = await originalResponse.json() as MetadataDetail;
  expect(original.EditableFields).toContain('Name');
  expect(original.LockedFields).not.toContain('Name');
  await page.getByRole('button', { name: `Edit metadata for ${item.Name}`, exact: true }).click();
  const editor = page.getByRole('dialog', { name: /^Edit metadata/ });
  const editedTitle = `Orbit delivery ${run}`;
  await editor.getByRole('textbox', { name: 'Title', exact: true }).fill(editedTitle);
  let metadataPending = responseFor(page, 'PUT', metadataPath);
  await editor.getByRole('button', { name: 'Save changes', exact: true }).click();
  let metadataResponse = await metadataPending;
  expect(metadataResponse.status()).toBe(200);
  expect((await metadataResponse.json() as MetadataDetail).Effective.Name).toBe(editedTitle);
  await expect(editor.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await capture('delivery-metadata-saved-desktop.png');
  if (Object.hasOwn(original.Overrides, 'Name')) await editor.getByRole('textbox', { name: 'Title', exact: true }).fill(original.Effective.Name);
  else await editor.getByRole('button', { name: 'Use automatic Title', exact: true }).click();
  metadataPending = responseFor(page, 'PUT', metadataPath);
  await editor.getByRole('button', { name: 'Save changes', exact: true }).click();
  metadataResponse = await metadataPending;
  expect(metadataResponse.status()).toBe(200);
  const restoredMetadata = await metadataResponse.json() as MetadataDetail;
  expect(restoredMetadata.Overrides).toEqual(original.Overrides);
  expect(restoredMetadata.LockedFields).toEqual(original.LockedFields);
  await editor.locator('.MuiDialogActions-root').getByRole('button', { name: 'Close', exact: true }).click();
  checks.metadataRestored = true;

  await page.getByRole('button', { name: 'Back to libraries', exact: true }).click();
  const seriesItemsPending = responseFor(page, 'GET', `/admin/v1/libraries/${series.Library.Id}/items`);
  await page.getByRole('button', { name: `Manage items in ${seriesName}`, exact: true }).click();
  const seriesItemsResponse = await seriesItemsPending;
  expect(seriesItemsResponse.status()).toBe(200);
  const seriesItems = await seriesItemsResponse.json() as MetadataItemsResponse;
  expect(seriesItems.Items.filter((entry) => entry.Type === 'Episode')).toHaveLength(fixture.episodes);
  expect(seriesItems.Items.some((entry) => entry.Type === 'Series')).toBe(true);
  await expect(page.getByRole('table', { name: 'Library items', exact: true })).toBeVisible();
  await capture('delivery-series-items-desktop.png');

  await navigate(page, 'Access', 'API keys');
  await page.getByRole('button', { name: 'Create API key', exact: true }).first().click();
  const keyDialog = page.getByRole('dialog', { name: 'Create API key', exact: true });
  const appName = `Delivery application ${run}`;
  await keyDialog.getByRole('textbox', { name: 'Application name', exact: true }).fill(appName);
  const keyPending = responseFor(page, 'POST', '/admin/v1/api-keys');
  await keyDialog.getByRole('button', { name: 'Create API key', exact: true }).click();
  const keyResponse = await keyPending;
  expect(keyResponse.status()).toBe(201);
  const key = await keyResponse.json() as CreateApplicationKeyResponse;
  const createdKeyDialog = page.getByRole('dialog', { name: 'API key created', exact: true });
  await expect(createdKeyDialog.getByRole('textbox', { name: 'Access token', exact: true })).toBeVisible();
  expect((await createdKeyDialog.getByRole('textbox', { name: 'Access token', exact: true }).inputValue()) === key.AccessToken).toBe(true);
  await createdKeyDialog.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('textbox', { name: 'Search API keys', exact: true }).fill(appName);
  const keyRow = page.getByRole('table', { name: 'Application API keys', exact: true }).getByRole('row').filter({ has: page.getByText(appName, { exact: true }) });
  await keyRow.getByRole('button', { name: `Reveal key for ${appName}`, exact: true }).click();
  const reveal = page.getByRole('dialog', { name: 'Reveal API key', exact: true });
  await expect(reveal.getByRole('textbox', { name: 'Access token', exact: true })).toBeVisible();
  expect((await reveal.getByRole('textbox', { name: 'Access token', exact: true }).inputValue()) === key.AccessToken).toBe(true);
  await reveal.getByRole('button', { name: 'Close', exact: true }).click();
  await keyRow.getByRole('button', { name: `Revoke key for ${appName}`, exact: true }).click();
  const revoke = page.getByRole('dialog', { name: 'Revoke API key?', exact: true });
  const revokedPending = responseFor(page, 'POST', `/admin/v1/api-keys/${key.Key.Id}/revoke`);
  await revoke.getByRole('button', { name: 'Revoke key', exact: true }).click();
  expect((await revokedPending).status()).toBe(200);
  await expect(revoke).not.toBeVisible();
  await expect(keyRow.getByText('Revoked', { exact: true })).toBeVisible();
  await expect(keyRow.getByRole('button', { name: /^Reveal key/ })).toHaveCount(0);
  const keyList = await context.request.get(`/admin/v1/api-keys?IncludeRevoked=true&SearchTerm=${encodeURIComponent(appName)}`);
  expect(keyList.status()).toBe(200);
  expect((await keyList.json() as ApplicationKeysResponse).Items.find((entry) => entry.Id === key.Key.Id)?.Status).toBe('revoked');
  key.AccessToken = '';
  checks.applicationKeyRevoked = true;
  await capture('delivery-api-keys-desktop.png');

  const originalSettingsResponse = await context.request.get('/admin/v1/settings');
  expect(originalSettingsResponse.status()).toBe(200);
  const originalSettings = await originalSettingsResponse.json() as ServerSettings;
  await navigate(page, 'Settings', 'General');
  const identity = page.getByRole('region', { name: 'Server identity', exact: true });
  await identity.getByRole('button', { name: 'Custom', exact: true }).click();
  const editedName = `Goby delivery ${run}`;
  await identity.getByRole('textbox', { name: 'Server name', exact: true }).fill(editedName);
  await page.getByRole('tab', { name: 'Transcode limits', exact: true }).click();
  const widthDefault = page.getByRole('checkbox', { name: 'Use deployment default for maximum width', exact: true });
  await widthDefault.uncheck();
  const editedWidth = originalSettings.Effective.MaxWidth === 1280 ? 1920 : 1280;
  await page.getByRole('textbox', { name: 'Maximum width', exact: true }).fill(String(editedWidth));
  await page.getByRole('tab', { name: 'General', exact: true }).click();
  await expect(identity.getByRole('textbox', { name: 'Server name', exact: true })).toHaveValue(editedName);
  await page.getByRole('tab', { name: 'Transcode limits', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Maximum width', exact: true })).toHaveValue(String(editedWidth));
  await capture('delivery-settings-unsaved-desktop.png');
  const savedSettings = await saveSettings(page);
  expect(savedSettings.Effective.ServerName).toBe(editedName);
  expect(savedSettings.Effective.MaxWidth).toBe(editedWidth);
  expect(savedSettings.Revision).not.toBe(originalSettings.Revision);
  await page.reload();
  await expect(page.getByRole('textbox', { name: 'Maximum width', exact: true })).toHaveValue(String(editedWidth));
  await page.getByRole('tab', { name: 'General', exact: true }).click();
  await expect(identity.getByRole('textbox', { name: 'Server name', exact: true })).toHaveValue(editedName);
  await capture('delivery-settings-saved-desktop.png');
  const nameModes = { deployment: 'Deployment default', custom: 'Custom', empty: 'Empty', unset: 'Unset' };
  await identity.getByRole('button', { name: nameModes[originalSettings.ServerNameMode], exact: true }).click();
  if (originalSettings.ServerNameMode === 'custom') await identity.getByRole('textbox', { name: 'Server name', exact: true }).fill(originalSettings.Overrides.ServerName!);
  await page.getByRole('tab', { name: 'Transcode limits', exact: true }).click();
  if (originalSettings.Overrides.MaxWidth === null) await widthDefault.check();
  else await page.getByRole('textbox', { name: 'Maximum width', exact: true }).fill(String(originalSettings.Overrides.MaxWidth));
  const restoredSettings = await saveSettings(page);
  expect(restoredSettings.Overrides).toEqual(originalSettings.Overrides);
  expect(restoredSettings.ServerNameMode).toBe(originalSettings.ServerNameMode);
  checks.settingsDraftSurvivedTabs = true;
  checks.settingsPersistedAfterReload = true;
  checks.settingsRestored = true;

  for (const tab of ['Tasks', 'Activity & logs', 'Notifications', 'Backups & recovery']) {
    await navigate(page, 'System', tab);
    await expect(page.getByRole('tab', { name: tab, exact: true })).toHaveAttribute('aria-selected', 'true');
    if (tab === 'Tasks') {
      await page.getByRole('tab', { name: 'Available tasks', exact: true }).click();
      await expect(page.getByRole('list', { name: 'Available server tasks', exact: true })).toBeVisible();
    } else if (tab === 'Activity & logs') await expect(page.getByRole('table', { name: 'Activity records', exact: true })).toBeVisible();
    else if (tab === 'Notifications') await expect(page.getByRole('region', { name: 'Notification receiver status', exact: true })).toBeVisible();
    else await expect(page.getByRole('heading', { name: 'Recovery readiness', exact: true })).toBeVisible();
    await expect(page.locator('#main-content [aria-busy="true"]')).toHaveCount(0);
    await expect(page.locator('#main-content .MuiAlert-standardError')).toHaveCount(0);
    await capture(`delivery-system-${tab.toLowerCase().replaceAll(/[^a-z]+/g, '-')}-desktop.png`);
  }
  await navigate(page, 'Settings', 'Online providers');
  await expect(page.getByRole('region', { name: 'Online providers', exact: true })).toHaveAttribute('aria-busy', 'false');
  await expect(page.getByRole('button', { name: 'Refresh providers', exact: true })).toBeEnabled();
  await capture('delivery-providers-desktop.png');

  await page.setViewportSize({ width: 390, height: 844 });
  for (const [group, tab, filename] of [
    ['Overview', '', 'overview'], ['Media', 'Libraries', 'libraries'], ['Access', 'API keys', 'api-keys'],
    ['System', 'Tasks', 'tasks'], ['Settings', 'General', 'settings'], ['Settings', 'Online providers', 'providers'],
  ]) {
    await navigate(page, group, tab || undefined);
    if (group === 'Overview') await expect(page.getByText('Database healthy', { exact: true })).toBeVisible();
    else if (tab === 'Libraries') await expect(page.getByRole('button', { name: `Manage items in ${libraryName}`, exact: true })).toBeVisible();
    else if (tab === 'API keys') await expect(page.getByRole('list', { name: 'Application API keys', exact: true })).toBeVisible();
    else if (tab === 'Tasks') await expect(page.getByRole('list', { name: 'Available server tasks', exact: true })).toBeVisible();
    else if (tab === 'General') await expect(page.getByRole('textbox', { name: 'Server name', exact: true })).toBeVisible();
    else await expect(page.getByRole('region', { name: 'Online providers', exact: true })).toHaveAttribute('aria-busy', 'false');
    await expect(page.locator('#main-content [aria-busy="true"]')).toHaveCount(0);
    await noOverflow();
    await capture(`delivery-${filename}-mobile.png`);
    if (tab === 'Libraries') {
      await page.getByRole('button', { name: `Manage items in ${libraryName}`, exact: true }).click();
      await expect(page.getByRole('list', { name: 'Library items', exact: true })).toBeVisible();
      await expect(page.getByRole('list', { name: 'Library items', exact: true }).getByRole('listitem')).toHaveCount(items.TotalRecordCount);
      await noOverflow();
      await capture('delivery-library-items-mobile.png');
    }
  }
  checks.mobileWidths = [390];
  checks.fixtureLibraries = [movies.Library.Id, series.Library.Id];
  checks.catalogPathsMatchFixture = movieItems.every((entry) => entry.Path.startsWith(fixture.movie_root + '/'));
  expect(pageErrors).toEqual([]);
  expect(failedAPI).toEqual([]);
  expect(consoleErrors).toEqual([]);
  const evidencePath = testInfo.outputPath('delivery-evidence.json');
  await writeFile(evidencePath, JSON.stringify({ checks, pageErrors, failedAPI, consoleErrors }, null, 2) + '\n', 'utf8');
  await testInfo.attach('delivery-evidence.json', { path: evidencePath, contentType: 'application/json' });
});
