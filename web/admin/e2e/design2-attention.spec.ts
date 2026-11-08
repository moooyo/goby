import type { Page, Route } from '@playwright/test';
import { expect, json, library, seriesLibrary, test } from './dashboard-fixture';
import type { DashboardAPI } from './dashboard-fixture';
import type { LibraryStorageDirectory, TaskDefinition } from '../src/api';
import type { BackupPolicy } from '../src/backupPolicyApi';

const directoryPath = '/admin/v1/storage/library-directories';
const policyPath = '/admin/v1/backups/policy';
const taskId = '9'.repeat(32);

function directory(path: string, overrides: Partial<LibraryStorageDirectory> = {}): LibraryStorageDirectory {
  return { Path: path, Available: true, Readable: true, Writable: false, TotalBytes: '107374182400', UsedBytes: '42949672960', AvailableBytes: '59055800320', ...overrides };
}

function policy(overrides: Partial<BackupPolicy> = {}): BackupPolicy {
  return {
    Revision: '9007199254740993', Enabled: false,
    Schedule: { Kind: 'daily', DayOfWeek: null, TimeOfDay: '03:00', Timezone: 'UTC' },
    KeepCount: 7, IncludeImages: true, HasPassphrase: true, NextRunAt: null,
    Available: true, UnavailableReason: '', TaskId: taskId, LastRunAt: null, LastRunState: '', LastBackupId: '',
    ...overrides,
  };
}

function installDirectories(api: DashboardAPI, items: LibraryStorageDirectory[]) {
  api.handlers.set('GET ' + directoryPath, (route) => json(route, { Items: items, TotalRecordCount: items.length }));
}

function installBackupTask(api: DashboardAPI) {
  const task: TaskDefinition = {
    Id: taskId, Key: 'backup.create', Name: 'Create automatic backup',
    Description: 'Create an encrypted backup using saved settings.', Category: 'System',
    IsHidden: false, Enabled: true, Revision: '9007199254740993', ScheduleTimezone: 'UTC',
    Triggers: [], CurrentRun: null, LastRun: null, NextRunAt: null,
  };
  api.handlers.set('GET /admin/v1/tasks', (route) => json(route, { Items: [task], TotalRecordCount: 1 }));
  api.handlers.set('GET /admin/v1/tasks/' + taskId, (route) => json(route, { Task: task }));
  api.handlers.set('GET /admin/v1/tasks/' + taskId + '/runs', (route, request) => {
    const query = new URL(request.url()).searchParams;
    return json(route, { Items: [], TotalRecordCount: 0, StartIndex: Number(query.get('StartIndex') ?? 0), Limit: Number(query.get('Limit') ?? 50) });
  });
}

async function openAttention(page: Page) {
  await page.getByRole('button', { name: 'Needs attention', exact: true }).click();
  const popup = page.getByRole('dialog', { name: 'Needs attention', exact: true });
  await expect(popup).toBeVisible();
  return popup;
}

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'light' } });

test('global directory warnings use exact registered paths rather than allowed storage roots', async ({ page, api }) => {
  api.status.Storage.Volumes = [{ Path: '/media', Available: false, TotalBytes: null, UsedBytes: null }];
  api.handlers.set('GET /admin/v1/libraries', (route) => json(route, {
    Items: [{ ...library, Paths: ['/media/archive'] }, { ...seriesLibrary, Paths: ['/media/archive', '/media/archive/'] }], TotalRecordCount: 2,
  }));
  installDirectories(api, [
    directory('/media/archive', { Readable: false }),
    directory('/media/archive/', { Available: false, Readable: false }),
    directory('/Media/archive', { Readable: null }),
    directory('/media/movies'),
  ]);
  api.handlers.set('GET ' + policyPath, (route) => json(route, policy({ Enabled: true })));
  await page.goto('/admin/access/users');
  const popup = await openAttention(page);
  const warnings = popup.locator('[data-check="directory"]');
  await expect(warnings).toHaveCount(2);
  await expect(warnings.locator('code')).toHaveText(['/media/archive', '/media/archive/']);
  await expect(warnings.locator('code').getByText('/media', { exact: true })).toHaveCount(0);
  await expect(popup.getByText('Media directory read access is unknown.', { exact: true })).toBeVisible();
  await expect(popup.getByText('No issues reported by the available server status checks.', { exact: true })).toHaveCount(0);
  expect(api.reads(directoryPath)).toHaveLength(1);
  expect(api.reads('/admin/v1/storage/roots')).toHaveLength(0);
  expect(api.writes()).toEqual([]);
});

test('missing backup rules require a successful available policy readback', async ({ page, api }) => {
  let release: (() => void) | undefined;
  const pending = new Promise<void>((resolve) => { release = resolve; });
  let held: Route | undefined;
  api.handlers.set('GET ' + policyPath, async (route) => { held = route; await pending; await json(route, policy()); });
  try {
    await page.goto('/admin/');
    const popup = await openAttention(page);
    await expect(popup.getByText('Checking automatic backup rules…', { exact: true })).toBeVisible();
    await expect(popup.locator('[data-check="backup-rules"]')).toHaveCount(0);
    await expect(page.getByRole('main').getByRole('region', { name: 'CPU status', exact: true })).toContainText('28');
    release!();
    const warning = popup.locator('[data-check="backup-rules"]');
    await expect(warning).toContainText('No automatic backup rules');
    await expect(warning).toHaveAttribute('href', '/admin/system/tasks?task=' + taskId);

    api.handlers.set('GET ' + policyPath, (route) => json(route, policy({ Enabled: true })));
    await popup.getByRole('button', { name: 'Refresh attention checks', exact: true }).click();
    await expect(warning).toHaveCount(0);

    api.handlers.set('GET ' + policyPath, (route) => json(route, policy({ Available: false, UnavailableReason: 'service_unavailable' })));
    await popup.getByRole('button', { name: 'Refresh attention checks', exact: true }).click();
    await expect(popup.getByText('The automatic backup service is unavailable.', { exact: true })).toBeVisible();
    await expect(warning).toHaveCount(0);

    api.handlers.set('GET ' + policyPath, (route) => json(route, { Error: { Code: 'database_unavailable', Message: 'Policy state is unknown.' } }, 503));
    await page.reload();
    const unknownPopup = await openAttention(page);
    await expect(unknownPopup.getByText('Automatic backup trigger rules are unknown.', { exact: true })).toBeVisible();
    await expect(unknownPopup.locator('[data-check="backup-rules"]')).toHaveCount(0);
    await expect(page.getByRole('main').getByRole('region', { name: 'CPU status', exact: true })).toContainText('28');
    expect(api.writes()).toEqual([]);
  } finally {
    release?.();
    if (held) await held.abort().catch(() => {});
  }
});

test('the backup warning keeps its task deep link and honors the global draft guard', async ({ page, api }) => {
  api.handlers.set('GET ' + policyPath, (route) => json(route, policy()));
  installBackupTask(api);
  await page.goto('/admin/settings/general');
  const identity = page.getByRole('region', { name: 'Server identity', exact: true });
  await identity.getByRole('button', { name: 'Custom', exact: true }).click();
  await identity.getByRole('textbox', { name: 'Server name', exact: true }).fill('Preserved server draft');
  const trigger = page.getByRole('button', { name: 'Needs attention', exact: true });
  await trigger.focus();
  await page.keyboard.press('Enter');
  const popup = page.getByRole('dialog', { name: 'Needs attention', exact: true });
  const warning = popup.getByRole('link', { name: /No automatic backup rules/ });
  await expect(warning).toHaveAttribute('href', '/admin/system/tasks?task=' + taskId);
  await page.keyboard.press('ArrowDown');
  await expect(warning).toBeFocused();
  const dismissed = page.waitForEvent('dialog');
  page.once('dialog', (dialog) => void dialog.dismiss());
  await page.keyboard.press('Enter');
  expect((await dismissed).type()).toBe('confirm');
  await expect(page).toHaveURL(/\/admin\/settings\/general$/);
  await expect(identity.getByRole('textbox', { name: 'Server name', exact: true })).toHaveValue('Preserved server draft');

  await openAttention(page);
  page.once('dialog', (dialog) => void dialog.accept());
  await warning.click();
  await expect(page).toHaveURL(new RegExp('/admin/system/tasks\\?task=' + taskId + '$'));
  await expect(page.getByRole('dialog', { name: /^Trigger rules/ })).toBeVisible();
  expect(api.reads('/admin/v1/tasks/' + taskId).length).toBeGreaterThan(0);
  expect(api.writes()).toEqual([]);
});

test('optional refresh failures retain stale warnings without invalidating server status', async ({ page, api }) => {
  installDirectories(api, [directory('/media/archive', { Readable: false })]);
  api.handlers.set('GET ' + policyPath, (route) => json(route, policy()));
  await page.goto('/admin/');
  const popup = await openAttention(page);
  await expect(popup.locator('[data-check="directory"] code')).toHaveText('/media/archive');
  await expect(popup.locator('[data-check="backup-rules"]')).toBeVisible();
  api.handlers.set('GET ' + directoryPath, (route) => json(route, { Error: { Code: 'storage_unavailable', Message: 'Directory checks failed.' } }, 503));
  api.handlers.set('GET ' + policyPath, (route) => json(route, { Error: { Code: 'database_unavailable', Message: 'Policy checks failed.' } }, 503));
  const refresh = popup.getByRole('button', { name: 'Refresh attention checks', exact: true });
  await refresh.click();
  await expect(popup.locator('[data-check="directory"]')).toHaveAttribute('data-stale', 'true');
  await expect(popup.locator('[data-check="backup-rules"]')).toHaveAttribute('data-stale', 'true');
  await expect(popup.getByText('Showing the last successful directory checks.', { exact: true })).toBeVisible();
  await expect(popup.getByText('Showing the last successful backup rules check.', { exact: true })).toBeVisible();
  await expect(popup.locator('[data-check="directory"] code')).toHaveText('/media/archive');
  await expect(page.locator('.sidebar-server .is-stale')).toHaveCount(0);
  await expect(page.getByRole('main').getByRole('region', { name: 'CPU status', exact: true })).toContainText('28');

  installDirectories(api, [directory('/media/archive')]);
  api.handlers.set('GET ' + policyPath, (route) => json(route, policy({ Enabled: true })));
  await refresh.click();
  await expect(popup.locator('[data-check="directory"], [data-check="backup-rules"], [data-check="directory-status"], [data-check="backup-status"]')).toHaveCount(0);
  await expect(popup.getByText('No issues reported by the available server status checks.', { exact: true })).toBeVisible();
  expect(api.writes()).toEqual([]);
});
