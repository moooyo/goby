import type { Route } from '@playwright/test';
import type { SettingsUpdateInput } from '../src/api';
import { administrator, csrf, expect, json, test } from './dashboard-fixture';
import { destinations, evidence, navigate, navigation, noOverflow, ready, sidebar } from './dashboard-review';

// Explicit API fixtures prove frontend behavior, including failed writes and
// stale reads. They do not establish server or media-processing acceptance.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });

for (const viewport of [{ width: 1440, height: 900 }, { width: 375, height: 812 }]) {
  test.describe(`${viewport.width}px dashboard`, () => {
    test.use({ viewport });
    for (const destination of destinations) test(`renders ${destination.path} without horizontal overflow`, async ({ page, api }, testInfo) => {
      await page.goto(destination.path);
      await ready(page, destination);
      await noOverflow(page);
      if (viewport.width < 1240) {
        await expect(page.getByRole('button', { name: 'Open navigation', exact: true })).toBeVisible();
        await expect(navigation(page)).not.toBeVisible();
      } else {
        const nav = await sidebar(page).boundingBox();
        expect(nav).toMatchObject({ x: 0, width: 232 });
      }
      const name = destination.path === '/admin/' ? 'overview' : destination.path.includes('/items') ? 'media-items' : destination.path.slice('/admin/'.length).replaceAll('/', '-');
      await evidence(page, testInfo, `${name}-${viewport.width}-en-light`);
      expect(api.writes()).toEqual([]);
    });
  });
}

test('legacy routes retain their destination, query, fragment and active navigation', async ({ page, api }) => {
  test.setTimeout(120_000);
  for (const destination of destinations) {
    await page.goto(`${destination.legacy}?delivery=1#details`);
    await ready(page, destination);
    const url = new URL(page.url());
    expect(url.pathname).toBe(destination.path);
    expect(url.search).toBe('?delivery=1');
    expect(url.hash).toBe('#details');
  }
  expect(api.writes()).toEqual([]);
});

test('sidebar pages and browser history keep the selected destination after reload', async ({ page, api }) => {
  await page.goto('/admin/media/artwork');
  await ready(page, destinations[2]);
  await navigate(page, '/admin/access/devices');
  await ready(page, destinations[6]);
  await navigate(page, '/admin/media/libraries');
  await ready(page, destinations[1]);
  await page.goBack();
  await ready(page, destinations[6]);
  await page.reload();
  await ready(page, destinations[6]);
  expect(api.writes()).toEqual([]);
});

test('obsolete saved group state cannot select unrelated pages and unknown URLs resolve to overview', async ({ page, api }) => {
  await page.addInitScript(() => sessionStorage.setItem('goby.dashboard.tabs', JSON.stringify({ media: 'users', access: '__proto__', settings: 'not-a-page' })));
  await page.goto('/admin/not-a-page');
  await ready(page, destinations[0]);
  await expect(page).toHaveURL(/\/admin\/$/);
  await navigate(page, '/admin/media/libraries');
  await ready(page, destinations[1]);
  expect(api.writes()).toEqual([]);
});

test('overview management counts open their corresponding pages', async ({ page, api }) => {
  for (const [name, destination] of [['View users', 5], ['View libraries', 1], ['View media items', 1], ['View active sessions', 7]] as const) {
    await page.goto('/admin/');
    await ready(page, destinations[0]);
    await page.getByRole('button', { name, exact: true }).click();
    await ready(page, destinations[destination]);
  }
  expect(api.writes()).toEqual([]);
});

test('overview retains last metrics on refresh failure, identifies stale data and recovers', async ({ page, api }) => {
  await page.goto('/admin/');
  await ready(page, destinations[0]);
  const cpu = page.getByRole('region', { name: 'CPU status', exact: true });
  await expect(cpu).toContainText('28');
  api.handlers.set('GET /admin/v1/system/status', (route) => json(route, { Error: { Code: 'telemetry_unavailable', Message: 'System metrics are temporarily unavailable.' } }, 503));
  await page.getByRole('button', { name: 'Refresh overview' }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'System metrics are temporarily unavailable.' })).toBeVisible();
  await expect(page.getByText('Showing the last successful update.', { exact: true })).toBeVisible();
  await expect(cpu).toContainText('28');
  await expect(page.getByRole('button', { name: 'View users', exact: true })).toContainText('2');
  api.handlers.delete('GET /admin/v1/system/status');
  api.status.CPU.UsagePercent = 86;
  await page.getByRole('button', { name: 'Refresh overview' }).click();
  await expect(cpu.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '86');
  await expect(page.getByText('Showing the last successful update.', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('unavailable metrics remain unknown instead of reporting a successful zero measurement', async ({ page, api }) => {
  api.status.CPU = { UsagePercent: null, Load1: null, Load5: null, Load15: null };
  api.status.Memory = { TotalBytes: null, UsedBytes: null, CachedBytes: null, SwapUsedBytes: null };
  api.status.Storage = { TotalBytes: null, UsedBytes: null, Complete: false, Volumes: [{ Path: '/media/offline', TotalBytes: null, UsedBytes: null, Available: false }] };
  api.status.Transcoding = { Available: false, Active: null, Limit: null, HardwareActive: null, SoftwareActive: null };
  api.handlers.set('GET /admin/v1/storage/library-directories', (route) => json(route, {
    Items: [{ Path: '/media/offline', Available: false, Readable: false, Writable: null, TotalBytes: null, UsedBytes: null, AvailableBytes: null }],
    TotalRecordCount: 1,
  }));
  await page.goto('/admin/');
  await ready(page, destinations[0]);
  for (const name of ['CPU', 'Memory', 'Transcoding']) {
    const region = page.getByRole('region', { name: `${name} status`, exact: true });
    await expect(region.getByLabel('Not available', { exact: true })).toBeVisible();
    await expect(region.getByRole('progressbar')).toHaveAttribute('aria-valuetext', 'Not available');
  }
  const directory = page.getByRole('region', { name: 'Directory with least available space status', exact: true });
  await expect(directory.getByLabel('Not available', { exact: true })).toBeVisible();
  await expect(directory.getByRole('progressbar')).toHaveCount(0);
  await expect(directory).toContainText('Directory capacity unavailable');
  await expect(directory).toContainText('1 unreadable');
});

test('stalled telemetry does not block counts or activity and is cancelled on timeout and navigation', async ({ page, api }) => {
  const held: Route[] = [];
  let cancelled = 0;
  page.on('requestfailed', (request) => { if (new URL(request.url()).pathname === '/admin/v1/system/status') cancelled += 1; });
  api.handlers.set('GET /admin/v1/system/status', async (route) => { held.push(route); });
  try {
    await page.goto('/admin/');
    await expect(page.getByRole('button', { name: 'View users', exact: true })).toContainText('2');
    await expect(page.getByRole('list', { name: 'Recent activity records', exact: true })).toContainText('Administrator signed in');
    const firstOverviewReads = api.reads('/admin/v1/overview').length;
    const firstCancelled = cancelled;
    await expect(page.getByRole('alert').filter({ hasText: 'System metrics did not respond within 15 seconds. Refresh to try again.' })).toBeVisible({ timeout: 20_000 });
    await expect.poll(() => cancelled).toBeGreaterThan(firstCancelled);
    expect(api.reads('/admin/v1/overview').length, 'Other resources must keep refreshing while telemetry is stalled.').toBeGreaterThan(firstOverviewReads);
    api.handlers.delete('GET /admin/v1/system/status');
    await page.getByRole('button', { name: 'Refresh overview', exact: true }).click();
    await expect(page.getByRole('region', { name: 'CPU status', exact: true }).getByRole('progressbar')).toHaveAttribute('aria-valuenow', '28');
    api.handlers.set('GET /admin/v1/system/status', async (route) => { held.push(route); });
    const beforeLeave = cancelled;
    const readsBeforeLeave = api.reads('/admin/v1/system/status').length;
    await page.getByRole('button', { name: 'Refresh overview', exact: true }).click();
    await expect.poll(() => api.reads('/admin/v1/system/status').length).toBeGreaterThan(readsBeforeLeave);
    await navigate(page, '/admin/media/libraries');
    // The destination may independently read the same stalled telemetry API.
    await ready(page, destinations[1], true, { allowPendingStorage: true });
    await expect.poll(() => cancelled).toBeGreaterThan(beforeLeave);
  } finally {
    await Promise.all(held.map((route) => route.abort().catch(() => {})));
  }
});

test('account menu restores keyboard focus and signs out with CSRF protection', async ({ page, api }) => {
  await page.goto('/admin/');
  await ready(page, destinations[0]);
  const account = page.getByRole('button', { name: `Account: ${administrator.Name}`, exact: true });
  await account.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('menu')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(account).toBeFocused();
  await account.click();
  await page.getByRole('menuitem', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('textbox', { name: /Username/i })).toBeVisible();
  expect(api.writes()).toEqual([{ method: 'DELETE', path: '/admin/v1/session', body: null, csrf }]);
});

test('settings pages retain one draft and save with the exact shared revision', async ({ page, api }) => {
  await page.goto('/admin/settings/general');
  await ready(page, destinations[13]);
  const initialReads = api.reads('/admin/v1/settings').length;
  const names = page.getByRole('region', { name: 'Server identity', exact: true });
  await names.getByRole('button', { name: 'Custom', exact: true }).click();
  await names.getByRole('textbox', { name: 'Server name', exact: true }).fill('Shared draft server');
  await navigate(page, '/admin/settings/transcode');
  await ready(page, destinations[14]);
  await page.getByRole('checkbox', { name: 'Use deployment default for maximum bitrate', exact: true }).uncheck();
  await page.getByRole('textbox', { name: 'Maximum bitrate', exact: true }).fill('12.5');
  await navigate(page, '/admin/settings/metadata');
  await ready(page, destinations[16]);
  await navigate(page, '/admin/settings/general');
  await ready(page, destinations[13]);
  await expect(names.getByRole('textbox', { name: 'Server name', exact: true })).toHaveValue('Shared draft server');
  expect(api.reads('/admin/v1/settings')).toHaveLength(initialReads);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByText('Settings saved.', { exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0].body).toMatchObject({ Revision: '9007199254740993', ServerNameMode: 'custom', Overrides: { ServerName: 'Shared draft server', MaxBitrate: 12_500_000, MaxWidth: null, MaxHeight: null, MaxAudioChannels: null }, Encoding: { TranscodingMaxWidth: 0 } });
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
});

test('settings draft allows internal pages and guards leaving the settings editor', async ({ page, api }) => {
  await page.goto('/admin/settings/general');
  await ready(page, destinations[13]);
  const names = page.getByRole('region', { name: 'Server identity', exact: true });
  await names.getByRole('button', { name: 'Custom', exact: true }).click();
  await names.getByRole('textbox', { name: 'Server name', exact: true }).fill('Keep this draft');
  let prompts = 0;
  page.on('dialog', async (dialog) => { prompts += 1; expect(dialog.type()).toBe('confirm'); expect(dialog.message()).toContain('unsaved'); await dialog.dismiss(); });
  await navigate(page, '/admin/settings/transcode');
  await ready(page, destinations[14]);
  expect(prompts).toBe(0);
  await navigate(page, '/admin/media/libraries');
  expect(prompts).toBe(1);
  await expect(page).toHaveURL(/\/settings\/transcode$/);
  await navigate(page, '/admin/settings/general');
  await expect(names.getByRole('textbox', { name: 'Server name', exact: true })).toHaveValue('Keep this draft');
  expect(api.writes()).toEqual([]);
});

test('sorting includes uncommitted text in the shared draft and save', async ({ page, api }) => {
  await page.goto('/admin/settings/general');
  await ready(page, destinations[13]);
  await page.getByRole('combobox', { name: 'Words removed from sort names', exact: true }).fill('of');
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeEnabled();
  await navigate(page, '/admin/settings/transcode');
  await ready(page, destinations[14]);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByText('Settings saved.', { exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0].body).toMatchObject({ Sorting: { SortRemoveWords: ['the', 'a', 'an', 'of'] } });
  await navigate(page, '/admin/settings/general');
  await expect(page.getByRole('region', { name: 'Library sorting', exact: true }).getByText('of', { exact: true })).toBeVisible();
  const words = page.getByRole('combobox', { name: 'Words removed from sort names', exact: true });
  await words.fill('and');
  await words.press('Enter');
  await expect(words).toHaveValue('');
  await expect(page.getByRole('region', { name: 'Library sorting', exact: true }).getByText('and', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeEnabled();
  expect(api.writes()).toHaveLength(1);
});

test('in-flight settings save blocks navigation until the mutation finishes', async ({ page, api }) => {
  let finish = () => {};
  const pending = new Promise<void>((resolve) => { finish = resolve; });
  api.handlers.set('PUT /admin/v1/settings', async (route, request) => {
    await pending;
    api.apply(request.postDataJSON() as SettingsUpdateInput);
    await json(route, api.settings);
  });
  await page.goto('/admin/settings/general');
  await ready(page, destinations[13]);
  await page.getByRole('group', { name: 'Name source', exact: true }).getByRole('button', { name: 'Custom', exact: true }).click();
  await page.getByRole('textbox', { name: 'Server name', exact: true }).fill('Pending save');
  const removeWord = page.getByRole('button', { name: 'Remove the', exact: true });
  await expect(removeWord).toBeEnabled();
  try {
    await page.getByRole('button', { name: 'Save changes', exact: true }).click();
    await expect.poll(() => api.writes().length).toBe(1);
    await expect(navigation(page).locator('a[href="/admin/settings/transcode"]')).toHaveAttribute('aria-disabled', 'true');
    await expect(page.getByRole('textbox', { name: 'Server name', exact: true })).toBeDisabled();
    await expect(removeWord).toHaveCount(0);
    await expect(page).toHaveURL(/\/settings\/general$/);
  } finally { finish(); }
  await expect(page.getByText('Settings saved.', { exact: true })).toBeVisible();
  await expect(removeWord).toBeEnabled();
  await navigate(page, '/admin/settings/transcode');
  await ready(page, destinations[14]);
});

test('a compare-and-swap conflict keeps the draft, blocks another write and requires a confirmed reload', async ({ page, api }) => {
  api.handlers.set('PUT /admin/v1/settings', (route) => json(route, { Error: { Code: 'revision_conflict', Message: 'Settings changed. Reload before saving.' } }, 409));
  await page.goto('/admin/settings/general');
  await ready(page, destinations[13]);
  await page.getByRole('group', { name: 'Name source', exact: true }).getByRole('button', { name: 'Custom', exact: true }).click();
  const name = page.getByRole('textbox', { name: 'Server name', exact: true });
  await name.fill('Conflict draft');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByText(/These settings changed after you loaded them/)).toBeVisible();
  await expect(name).toHaveValue('Conflict draft');
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
  api.settings.Revision = '9007199254740995';
  await page.getByRole('button', { name: 'Reload latest settings', exact: true }).click();
  const confirm = page.getByRole('dialog');
  await expect(confirm).toContainText('All unsaved edits will be lost.');
  await confirm.getByRole('button', { name: /Reload|Discard/, exact: false }).last().click();
  await expect(name).toHaveValue('Goby living room');
  expect(api.writes()).toHaveLength(1);
});

test('an unconfirmed user creation is never automatically replayed', async ({ page, api }) => {
  api.handlers.set('POST /admin/v1/users', (route) => route.abort('failed'));
  await page.goto('/admin/access/users');
  await ready(page, destinations[5]);
  await page.getByRole('button', { name: 'Create user', exact: true }).first().click();
  const sheet = page.getByRole('dialog', { name: 'Create user', exact: true });
  await sheet.getByRole('textbox', { name: 'Username', exact: true }).fill('Unconfirmed user');
  await sheet.getByLabel(/^Password/).fill('synthetic-only-password');
  await sheet.getByRole('button', { name: 'Create user', exact: true }).click();
  await expect(sheet).toContainText('The response could not be confirmed.');
  await expect(sheet.getByRole('button', { name: 'Create user', exact: true })).toHaveCount(0);
  expect(api.writes()).toHaveLength(1);
  await sheet.getByRole('button', { name: 'Check users', exact: true }).click();
  await expect(sheet).not.toBeVisible();
  expect(api.writes()).toHaveLength(1);
});

for (const width of [1239, 1240]) test(`navigation changes at the specified ${width}px breakpoint`, async ({ page, api }) => {
  await page.setViewportSize({ width, height: 900 });
  await page.goto('/admin/settings/metadata');
  await ready(page, destinations[16]);
  await noOverflow(page);
  if (width < 1240) {
    const opener = page.getByRole('button', { name: 'Open navigation', exact: true });
    await expect(navigation(page)).not.toBeVisible();
    await opener.click();
    await expect(sidebar(page)).toHaveCSS('transform', 'none');
    const nav = await sidebar(page).boundingBox();
    expect(nav).toMatchObject({ x: 0, width: 264 });
    await page.keyboard.press('Escape');
    await expect(navigation(page)).not.toBeVisible();
    await expect(opener).toBeFocused();
  } else {
    const nav = await sidebar(page).boundingBox();
    expect(nav).toMatchObject({ x: 0, width: 232 });
    await expect(page.getByRole('button', { name: 'Open navigation', exact: true })).not.toBeVisible();
  }
  expect(api.writes()).toEqual([]);
});
