import type { Locator, Page } from '@playwright/test';
import { expect, json, test } from './dashboard-fixture';
import type { DashboardAPI } from './dashboard-fixture';
import { evidence } from './dashboard-review';
import type { TaskDefinition, TaskTrigger } from '../src/api';
import type { BackupPolicy, BackupPolicyInput } from '../src/backupPolicyApi';
import type { BackupView, OperationView, SourceView, StatusView } from '../src/backupsApi';

const policyPath = '/admin/v1/backups/policy';
const taskId = '9'.repeat(32);
const backupId = '1'.repeat(32);
const operationId = '2'.repeat(32);
const revision = '9007199254740993';
const timestamp = '2026-11-07T03:00:00Z';
const nextRun = '2026-11-08T03:00:00Z';
const digest = 'a'.repeat(64);

function source(overrides: Partial<SourceView> = {}): SourceView {
  return {
    ServerId: 'd'.repeat(32), ServerName: 'Archive server', GobyVersion: '0.0.9', SchemaVersion: '42',
    CreatedAt: timestamp, Tables: [{ Name: 'users', Rows: '6' }], ...overrides,
  };
}

function backup(overrides: Partial<BackupView> = {}): BackupView {
  return {
    Id: backupId, Kind: 'generated', State: 'ready', CreatedAt: timestamp, UpdatedAt: timestamp,
    SizeBytes: '1024', SHA256: digest, Verified: true, ErrorCode: '', Source: source(), ...overrides,
  };
}

function trigger(overrides: Partial<TaskTrigger> = {}): TaskTrigger {
  return {
    Id: 'backup-daily-rule', Kind: 'daily', IntervalTicks: null, TimeOfDayTicks: '108000000000', DayOfWeek: null,
    MaxRuntimeTicks: null, NextFireAt: nextRun, CalculationError: '', ...overrides,
  };
}

function task(overrides: Partial<TaskDefinition> = {}): TaskDefinition {
  return {
    Id: taskId, Key: 'backup.create', Name: 'Create automatic backup',
    Description: 'Create a backup with the saved automatic backup settings.', Category: 'System',
    IsHidden: false, Enabled: true, Revision: revision, ScheduleTimezone: 'UTC',
    Triggers: [trigger()], CurrentRun: null, LastRun: null, NextRunAt: nextRun, ...overrides,
  };
}

function policy(overrides: Partial<BackupPolicy> = {}): BackupPolicy {
  return {
    Revision: revision, Enabled: true, Schedule: { Kind: 'daily', DayOfWeek: null, TimeOfDay: '03:00', Timezone: 'UTC' },
    KeepCount: 7, IncludeImages: true, HasPassphrase: true, NextRunAt: nextRun,
    Available: true, UnavailableReason: '', TaskId: taskId, LastRunAt: timestamp, LastRunState: 'completed', LastBackupId: backupId,
    ...overrides,
  };
}

function status(overrides: Partial<StatusView> = {}): StatusView {
  return {
    Available: true, UnavailableReason: '', RestoreAvailable: true, RestoreUnavailableReason: '', Busy: false, ActiveOperationId: '', GenerationRevision: revision,
    Limits: { MaxBackupBytes: '1073741824', MaxStoredBytes: '4294967296', MaxBackups: 32, MinPassphraseBytes: 12, MaxPassphraseBytes: 1024 },
    Storage: { Bytes: '1024', Objects: 1 },
    Rollback: { Available: false, MustReplace: false, CreatedAt: null, ServerName: '', Generation: '', UnavailableReason: '' },
    ...overrides,
  };
}

function operation(requestId: string): OperationView {
  return {
    Id: operationId, RequestId: requestId, Revision: revision, Kind: 'create', State: 'completed', Phase: 'finished',
    BackupId: backupId, CreatedAt: timestamp, UpdatedAt: timestamp, ErrorCode: '', Source: source(),
    RestoreDefaults: false, ReplaceRollback: false, CanCancel: false, CanApply: false, GenerationRevision: revision,
  };
}

function installBackups(api: DashboardAPI, options: { policy?: BackupPolicy; task?: TaskDefinition; backups?: BackupView[]; status?: StatusView } = {}) {
  let saved = structuredClone(options.policy ?? policy());
  const savedTask = structuredClone(options.task ?? task());
  const items = structuredClone(options.backups ?? [backup()]);
  const operations: OperationView[] = [];
  api.handlers.set(`GET ${policyPath}`, (route) => json(route, saved));
  api.handlers.set(`PATCH ${policyPath}`, async (route, request) => {
    const input = request.postDataJSON() as BackupPolicyInput;
    if (input.Revision !== saved.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'Settings changed.' } }, 409);
    saved = {
      ...saved, KeepCount: input.KeepCount, IncludeImages: input.IncludeImages, Revision: (BigInt(saved.Revision) + 1n).toString(),
      HasPassphrase: input.Passphrase === undefined ? saved.HasPassphrase : input.Passphrase !== '',
    };
    return json(route, saved);
  });
  api.handlers.set('GET /admin/v1/tasks', (route) => json(route, { Items: [savedTask], TotalRecordCount: 1 }));
  api.handlers.set(`GET /admin/v1/tasks/${taskId}`, (route) => json(route, { Task: savedTask }));
  api.handlers.set('GET /admin/v1/backups/status', (route) => json(route, options.status ?? status()));
  api.handlers.set('GET /admin/v1/backups', (route, request) => {
    const query = new URL(request.url()).searchParams;
    const start = Number(query.get('StartIndex') ?? 0); const limit = Number(query.get('Limit') ?? 25);
    return json(route, { Items: items.slice(start, start + limit), TotalRecordCount: items.length, StartIndex: start, Limit: limit });
  });
  api.handlers.set('GET /admin/v1/backup-operations', (route, request) => {
    const query = new URL(request.url()).searchParams;
    const start = Number(query.get('StartIndex') ?? 0); const limit = Number(query.get('Limit') ?? 25);
    return json(route, { Items: operations.slice(start, start + limit), TotalRecordCount: operations.length, StartIndex: start, Limit: limit });
  });
  api.handlers.set(`GET /admin/v1/backup-operations/${operationId}`, (route) => json(route, { Operation: operations[0] }));
  return { getPolicy: () => saved, setPolicy: (next: BackupPolicy) => { saved = structuredClone(next); }, task: savedTask, operations };
}

async function openSettings(page: Page) {
  await page.goto('/admin/backups');
  await page.getByRole('button', { name: 'Backup settings', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Backup settings', exact: true });
  await expect(dialog.getByLabel('Automatic backups to keep', { exact: true })).toBeEnabled();
  return dialog;
}

async function expectWholeWordsWithinCell(label: Locator) {
  const layout = await label.evaluate((element) => {
    const cell = element.closest('td');
    if (!cell) throw new Error('The verification label must belong to a table cell.');
    const bounds = cell.getBoundingClientRect();
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
    const segments: { node: Node; start: number; end: number; text: string }[] = [];
    let offset = 0;
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const text = node.textContent ?? '';
      segments.push({ node, start: offset, end: offset + text.length, text });
      offset += text.length;
    }
    const text = segments.map((segment) => segment.text).join('');
    const words = [...text.matchAll(/\S+/g)].map((match) => {
      const start = match.index; const end = start + match[0].length;
      const first = segments.find((segment) => segment.start <= start && segment.end > start);
      const last = segments.find((segment) => segment.start < end && segment.end >= end);
      if (!first || !last) throw new Error('A verification word could not be measured.');
      const range = document.createRange();
      range.setStart(first.node, start - first.start);
      range.setEnd(last.node, end - last.start);
      const rects = [...range.getClientRects()].filter((rect) => rect.width > 0 && rect.height > 0)
        .map((rect) => ({ left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom }));
      return { text: match[0], rects };
    });
    return { cell: { left: bounds.left, right: bounds.right, top: bounds.top, bottom: bounds.bottom }, words };
  });
  expect(layout.words.map((word) => word.text)).toEqual(['Pending', 'verification']);
  for (const word of layout.words) {
    expect(word.rects, `The complete word "${word.text}" must not split across lines.`).toHaveLength(1);
    for (const rect of word.rects) {
      expect(rect.left, `The word "${word.text}" must stay within its cell.`).toBeGreaterThanOrEqual(layout.cell.left - 1);
      expect(rect.right, `The word "${word.text}" must stay within its cell.`).toBeLessThanOrEqual(layout.cell.right + 1);
      expect(rect.top, `The word "${word.text}" must stay within its cell.`).toBeGreaterThanOrEqual(layout.cell.top - 1);
      expect(rect.bottom, `The word "${word.text}" must stay within its cell.`).toBeLessThanOrEqual(layout.cell.bottom + 1);
    }
  }
}

async function expectTimestampSpace(row: Locator) {
  const layout = await row.evaluate((element) => {
    const cell = element.querySelector('td');
    const button = cell?.querySelector('.backup-date-button');
    const type = element.querySelector('.backup-type-cell > .MuiTypography-root');
    if (!cell || !button || !type) throw new Error('A backup timestamp and its adjacent type must be rendered.');
    const range = document.createRange();
    range.selectNodeContents(button);
    const timestamps = [...range.getClientRects()].filter((rect) => rect.width > 0 && rect.height > 0)
      .map((rect) => ({ left: rect.left, right: rect.right }));
    range.selectNodeContents(type);
    const typeLeft = range.getBoundingClientRect().left;
    const bounds = cell.getBoundingClientRect();
    return { cell: { left: bounds.left, right: bounds.right }, timestamps, typeLeft };
  });
  expect(layout.timestamps.length).toBeGreaterThan(0);
  for (const timestamp of layout.timestamps) {
    expect(timestamp.left).toBeGreaterThanOrEqual(layout.cell.left - 1);
    expect(timestamp.right, 'The rendered timestamp must remain inside its own column.').toBeLessThanOrEqual(layout.cell.right - 1);
    expect(layout.typeLeft - timestamp.right, 'Timestamp and type text need visible separation.').toBeGreaterThanOrEqual(8);
  }
}

test('application versions come from checked backup sources and version warnings do not replace restore readiness', async ({ page, api }) => {
  const unverifiedId = '3'.repeat(32); const missingSourceId = '4'.repeat(32);
  installBackups(api, { backups: [
    backup(),
    backup({ Id: unverifiedId, Verified: false, Source: source({ GobyVersion: 'untrusted-version' }) }),
    backup({ Id: missingSourceId, Kind: 'imported', Verified: false, Source: null }),
  ] });
  await page.goto('/admin/backups');
  await expect(page.getByRole('columnheader', { name: 'Version', exact: true })).toBeVisible();
  await expect(page.getByTestId(`backup-version-${backupId}`)).toHaveText('0.0.9');
  await expect(page.getByTestId(`backup-version-${unverifiedId}`)).toHaveText('Pending verification');
  await expect(page.getByTestId(`backup-version-${missingSourceId}`)).toHaveText('Pending verification');
  await page.evaluate(async () => { await document.fonts.ready; });
  for (const id of [unverifiedId, missingSourceId]) await expectWholeWordsWithinCell(page.getByTestId(`backup-version-${id}`));
  for (const id of [backupId, unverifiedId, missingSourceId]) await expectTimestampSpace(page.getByTestId(`backup-${id}`));
  await expect(page.getByTestId('backup-version-review')).toContainText('Goby 0.0.9; this server runs 0.1.0');
  await expect(page.getByTestId('backup-version-review')).toContainText('Cross-version restore is not guaranteed.');
  await expect(page.getByRole('region', { name: 'Restore backup', exact: true }).getByRole('button', { name: 'Plan restore', exact: true })).toBeEnabled();
  await evidence(page, test.info(), 'design2-backups-application-versions-cross-version');
  await page.getByTestId(`backup-${unverifiedId}`).getByRole('button', { name: /Select backup from/ }).click();
  await expect(page.getByTestId('backup-version-review')).toContainText('The application version is pending verification.');
  await expect(page.getByText('untrusted-version', { exact: true })).toHaveCount(0);
  expect(api.writes()).toEqual([]);
  await evidence(page, test.info(), 'design2-backups-unverified-source');
});

test('custom trigger rules survive a policy-only PATCH with an exact revision', async ({ page, api }) => {
  const customTask = task({ Triggers: [
    trigger({ Id: 'hourly-backup', Kind: 'interval', IntervalTicks: '36000000000', TimeOfDayTicks: null }),
    trigger({ Id: 'startup-backup', Kind: 'startup', TimeOfDayTicks: null, NextFireAt: null }),
  ] });
  const state = installBackups(api, { policy: policy({ Schedule: { Kind: 'custom', DayOfWeek: null, TimeOfDay: '', Timezone: 'UTC' } }), task: customTask });
  const before = structuredClone(state.task.Triggers);
  const dialog = await openSettings(page);
  await expect(dialog.getByText('Every 1 hour', { exact: true })).toBeVisible();
  await expect(dialog.getByText('At server startup', { exact: true })).toBeVisible();
  await expect(dialog.getByLabel('Frequency', { exact: true })).toHaveCount(0);
  await expect(dialog.getByLabel('Enable automatic backups', { exact: true })).toHaveCount(0);
  await expect(dialog.getByLabel('Schedule time zone', { exact: true })).toHaveCount(0);
  await evidence(page, test.info(), 'design2-backups-settings-custom-triggers-initial');
  await dialog.getByRole('button', { name: 'Keep 14', exact: true }).click();
  await dialog.getByLabel('Include library images and avatars', { exact: true }).uncheck();
  await dialog.getByRole('button', { name: 'Save backup settings', exact: true }).click();
  await expect(dialog.getByText('Backup settings saved.', { exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0]).toMatchObject({ method: 'PATCH', path: policyPath, body: { Revision: revision, KeepCount: 14, IncludeImages: false } });
  expect(api.writes()[0].body).toEqual({ Revision: revision, KeepCount: 14, IncludeImages: false });
  expect(state.task.Triggers).toEqual(before);
  expect(state.getPolicy().Schedule.Kind).toBe('custom');
  await expect(dialog.getByText('Retention removes only completed automatic backups created by this deployment. Manual and imported backups are kept.', { exact: true })).toBeVisible();
  await dialog.getByText('Backup settings saved.', { exact: true }).scrollIntoViewIfNeeded();
  await evidence(page, test.info(), 'design2-backups-settings-custom-triggers-saved');
});

test('the backup trigger link opens the linked task without a policy or schedule mutation', async ({ page, api }) => {
  installBackups(api);
  const dialog = await openSettings(page);
  const link = dialog.getByRole('link', { name: 'Trigger rules', exact: true });
  await expect(link).toHaveAttribute('href', `/admin/system/tasks?task=${taskId}`);
  await link.click();
  await expect(page).toHaveURL(new RegExp(`/admin/system/tasks\\?task=${taskId}$`));
  await expect(page.getByRole('dialog').last()).toBeVisible();
  expect(api.reads(`/admin/v1/tasks/${taskId}`).length).toBeGreaterThan(0);
  expect(api.writes()).toEqual([]);
});

test('manual creation sends the saved image selection and a separately entered passphrase', async ({ page, api }) => {
  const state = installBackups(api, { policy: policy({ IncludeImages: false }) });
  api.handlers.set('POST /admin/v1/backups', async (route, request) => {
    const input = request.postDataJSON() as { RequestId: string; Passphrase: string; IncludeImages: boolean };
    const result = operation(input.RequestId); state.operations.push(result);
    return json(route, { Operation: result }, 202);
  });
  await page.goto('/admin/backups');
  await page.getByRole('button', { name: 'Create backup', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Create backup', exact: true });
  const passphrase = dialog.getByLabel(/^Passphrase(?:\s*\*)?$/);
  const confirmation = dialog.getByLabel(/^Confirm passphrase(?:\s*\*)?$/);
  await expect(passphrase).toHaveValue('');
  await expect(confirmation).toHaveValue('');
  await expect(dialog.getByText('This manual backup excludes library images and avatars, using the saved backup contents setting.', { exact: true })).toBeVisible();
  await passphrase.fill('manual-only-fixture-passphrase');
  await confirmation.fill('manual-only-fixture-passphrase');
  await dialog.getByRole('button', { name: 'Create backup', exact: true }).click();
  await expect(dialog).toBeHidden();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0].body).toEqual({ RequestId: expect.stringMatching(/^[a-f0-9]{32}$/), Passphrase: 'manual-only-fixture-passphrase', IncludeImages: false });
  expect(state.getPolicy().HasPassphrase).toBe(true);
  await expect(page.getByTestId(`operation-${operationId}`)).toContainText('Completed');
});

test('an unknown policy write requires reloading confirmed state before another save', async ({ page, api }) => {
  const state = installBackups(api);
  api.handlers.set(`PATCH ${policyPath}`, async (route, request) => {
    const input = request.postDataJSON() as BackupPolicyInput;
    state.setPolicy({ ...state.getPolicy(), KeepCount: input.KeepCount, Revision: (BigInt(input.Revision) + 1n).toString() });
    return json(route, { Error: { Code: 'database_unavailable', Message: 'The result could not be confirmed.' } }, 503);
  });
  const dialog = await openSettings(page);
  await dialog.getByLabel('Automatic backups to keep', { exact: true }).fill('9');
  await dialog.getByRole('button', { name: 'Save backup settings', exact: true }).click();
  await expect(dialog.getByText('The settings changed or the save result could not be confirmed. Reload the saved settings before making another change. This request is not submitted again automatically.', { exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Save backup settings', exact: true })).toBeDisabled();
  await expect(dialog.getByLabel('Automatic backups to keep', { exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
  page.once('dialog', (prompt) => void prompt.accept());
  await dialog.getByRole('button', { name: 'Reload saved settings', exact: true }).click();
  await expect(dialog.getByLabel('Automatic backups to keep', { exact: true })).toHaveValue('9');
  await expect(dialog.getByLabel('Automatic backups to keep', { exact: true })).toBeEnabled();
  expect(api.writes()).toHaveLength(1);
});

test('the server rejects clearing a passphrase while automatic triggers remain', async ({ page, api }) => {
  installBackups(api);
  api.handlers.set(`PATCH ${policyPath}`, (route) => json(route, {
    Error: { Code: 'invalid_backup_policy', Message: 'Check the backup settings.', Fields: { Passphrase: 'Remove all automatic backup triggers on Tasks before clearing the saved passphrase.' } },
  }, 400));
  const dialog = await openSettings(page);
  await dialog.getByLabel('Saved passphrase action', { exact: true }).selectOption('clear');
  await dialog.getByRole('button', { name: 'Save backup settings', exact: true }).click();
  await expect(dialog.getByText('Remove all automatic backup triggers on Tasks before clearing the saved passphrase.', { exact: true })).toBeVisible();
  await expect(dialog.getByLabel('Saved passphrase action', { exact: true })).toBeFocused();
  expect(api.writes()[0].body).toEqual({ Revision: revision, KeepCount: 7, IncludeImages: true, Passphrase: '' });
  expect(api.writes()).toHaveLength(1);
});

test('matching application versions cannot bypass the separate recovery database requirement', async ({ page, api }) => {
  installBackups(api, {
    backups: [backup({ Source: source({ GobyVersion: '0.1.0' }) })],
    status: status({ RestoreAvailable: false, RestoreUnavailableReason: 'recovery_database_not_configured' }),
  });
  await page.goto('/admin/backups');
  await expect(page.getByTestId('backup-version-review')).toContainText('A restore plan must still pass the server compatibility checks.');
  await expect(page.getByRole('region', { name: 'Restore backup', exact: true }).getByRole('button', { name: 'Plan restore', exact: true })).toBeDisabled();
  await expect(page.getByText('Restore needs a separate recovery database. Ask the server operator to configure it before planning a restore.', { exact: true })).toBeVisible();
  expect(api.writes()).toEqual([]);
  await evidence(page, test.info(), 'design2-backups-recovery-database-required');
});

test.describe('Chinese backup settings on a narrow screen', () => {
  test.use({ viewport: { width: 390, height: 844 }, preferences: { language: 'zh', theme: 'dark' } });
  test('keeps unsaved settings on a cancelled close and restores focus after discard', async ({ page, api }) => {
    installBackups(api);
    await page.goto('/admin/backups');
    const triggerButton = page.getByRole('button', { name: '备份设置', exact: true });
    await triggerButton.click();
    const dialog = page.getByRole('dialog', { name: '备份设置', exact: true });
    await dialog.getByLabel('自动备份保留份数', { exact: true }).fill('8');
    page.once('dialog', (prompt) => void prompt.dismiss());
    await page.keyboard.press('Escape');
    await expect(dialog).toBeVisible();
    await expect(dialog.getByLabel('自动备份保留份数', { exact: true })).toHaveValue('8');
    expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
    page.once('dialog', (prompt) => void prompt.accept());
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect(triggerButton).toBeFocused();
    expect(api.writes()).toEqual([]);
  });
});
