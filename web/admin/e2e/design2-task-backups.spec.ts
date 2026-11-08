import type { Page } from '@playwright/test';
import { expect, json, test } from './dashboard-fixture';
import type { DashboardAPI } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';
import type { TaskBackupResult, TaskDefinition, TaskRun } from '../src/api';

const now = '2026-11-07T08:00:00Z';
const taskId = 'b'.repeat(32);
const backupId = '0123456789abcdef0123456789abcdef';
const checksum = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
const tasksPath = '/admin/system/tasks';

function backupResult(overrides: Partial<TaskBackupResult> = {}): TaskBackupResult {
  return { Status: 'available', BackupId: backupId, State: 'ready', SizeBytes: '2480343613', SHA256: checksum, Verified: true, ...overrides };
}

function installBackupTasks(api: DashboardAPI, results: (TaskBackupResult | undefined)[], active = false) {
  const definition: TaskDefinition = {
    Id: taskId, Key: 'backup.create', Name: 'Create automatic backup', Description: 'Create an encrypted backup using the saved policy.',
    Category: 'Backup', IsHidden: false, Enabled: true, Revision: '9007199254740993', ScheduleTimezone: 'Asia/Shanghai',
    Triggers: [], CurrentRun: null, LastRun: null, NextRunAt: null,
  };
  const runs: TaskRun[] = results.map((result, index) => {
    const timestamp = new Date(Date.parse(now) - (index + 1) * 60_000).toISOString();
    return {
      Id: (index + 1).toString(16).padStart(32, '0'), TaskId: taskId, State: active && index === 0 ? 'running' : 'completed',
      Source: 'schedule', RequestId: null, CreatedAt: timestamp, StartedAt: timestamp, FinishedAt: active && index === 0 ? null : timestamp,
      StopRequestedAt: null, StopReason: '', ErrorCode: '', ErrorMessage: '', TotalChildren: 1, TerminalChildren: active && index === 0 ? 0 : 1,
      CompletedChildren: active && index === 0 ? 0 : 1, FailedChildren: 0, CancelledChildren: 0, InterruptedChildren: 0, UnavailableChildren: 0,
      Scanned: active && index === 0 ? 0 : 1, Added: active && index === 0 ? 0 : 1, Updated: 0, ...(result ? { BackupResult: result } : {}),
    };
  });
  definition.LastRun = runs[0] ?? null;
  if (active) definition.CurrentRun = runs[0] ?? null;
  api.handlers.set('GET /admin/v1/tasks', (route) => json(route, { Items: [definition], TotalRecordCount: 1 }));
  api.handlers.set('GET /admin/v1/tasks/' + taskId, (route) => json(route, { Task: definition }));
  api.handlers.set('GET /admin/v1/tasks/' + taskId + '/runs', (route, request) => {
    const query = new URL(request.url()).searchParams;
    const start = Number(query.get('StartIndex') ?? 0); const limit = Number(query.get('Limit') ?? 50);
    return json(route, { Items: runs.slice(start, start + limit), TotalRecordCount: runs.length, StartIndex: start, Limit: limit });
  });
  api.handlers.set('GET /admin/v1/activity', (route, request) => {
    const query = new URL(request.url()).searchParams;
    return json(route, { Items: [], TotalRecordCount: 0, StartIndex: Number(query.get('StartIndex') ?? 0), Limit: Number(query.get('Limit') ?? 50), RetentionDays: 30 });
  });
  for (const run of runs) api.handlers.set('GET /admin/v1/task-runs/' + run.Id, (route, request) => {
    const query = new URL(request.url()).searchParams;
    return json(route, { Run: run, Children: { Items: [], TotalRecordCount: 0, StartIndex: Number(query.get('StartIndex') ?? 0), Limit: Number(query.get('Limit') ?? 50) } });
  });
  return { definition, runs };
}

async function loadedHistory(page: Page) {
  const history = page.locator('.system-run-table');
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(history).toHaveAttribute('data-initiators-loading', 'false');
  return history;
}

function expectNoBackupGuessing(api: DashboardAPI) {
  const requests = api.requests.filter((request) => request.path.startsWith('/admin/v1/backups'));
  expect(requests.map(({ method, path, body }) => ({ method, path, body }))).toEqual([
    { method: 'GET', path: '/admin/v1/backups/policy', body: null },
  ]);
  expect(api.writes()).toEqual([]);
}

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });
test.beforeEach(async ({ page }) => { await page.clock.install({ time: new Date(now) }); });

test('automatic task history and details show only the exact returned archive metadata', async ({ page, api }) => {
  const state = installBackupTasks(api, [backupResult(), backupResult({ BackupId: 'f'.repeat(32), SizeBytes: '9007199254740993', Verified: false })]);
  await page.goto(tasksPath);
  const history = await loadedHistory(page);
  const verified = history.locator('[data-run-id="' + state.runs[0].Id + '"]');
  const unverified = history.locator('[data-run-id="' + state.runs[1].Id + '"]');
  await expect(verified.locator('.system-run-result')).toHaveText('2.31 GiB · Archive verified');
  await expect(unverified.locator('.system-run-result')).toHaveText('8 PiB · Archive not verified');
  await evidence(page, test.info(), 'design2-task-backups-history');
  await verified.getByRole('button').click();
  const dialog = page.getByRole('dialog');
  const result = dialog.getByRole('region', { name: 'Backup result', exact: true });
  await expect(result.getByText(backupId, { exact: true })).toBeVisible();
  await expect(result.getByText(checksum, { exact: true })).toBeVisible();
  await expect(result.getByText('2,480,343,613 bytes', { exact: true })).toBeVisible();
  await evidence(page, test.info(), 'design2-task-backups-verified-details');
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await unverified.getByRole('button').click();
  const unverifiedResult = page.getByRole('region', { name: 'Backup result', exact: true });
  await expect(unverifiedResult.getByText('9,007,199,254,740,993 bytes', { exact: true })).toBeVisible();
  await expect(unverifiedResult.getByText('Archive not verified', { exact: true })).toBeVisible();
  await expect(unverifiedResult).not.toContainText('Archive verified');
  expect(api.reads('/admin/v1/task-runs/' + state.runs[0].Id)).toHaveLength(1);
  expect(api.reads('/admin/v1/task-runs/' + state.runs[1].Id)).toHaveLength(1);
  expectNoBackupGuessing(api);
});

test('unassociated deleted missing and unavailable results keep distinct history without guessing a latest backup', async ({ page, api }) => {
  const results: (TaskBackupResult | undefined)[] = [undefined, { Status: 'not_recorded' }, { Status: 'deleted', BackupId: backupId },
    { Status: 'missing', BackupId: backupId }, { Status: 'deleting', BackupId: backupId }, { Status: 'mismatch' }, { Status: 'unavailable' }];
  const labels = ['Backup result unknown', 'Backup result not recorded', 'Backup archive deleted', 'Backup archive missing', 'Backup archive deleting', 'Backup association mismatch', 'Backup result unavailable'];
  const state = installBackupTasks(api, results);
  await page.goto(tasksPath);
  const history = await loadedHistory(page);
  for (let index = 0; index < labels.length; index += 1) {
    const row = history.locator('[data-run-id="' + state.runs[index].Id + '"]');
    await expect(row.locator('.system-run-result')).toHaveText(labels[index]);
    await expect(row.locator('.system-run-result')).not.toContainText(/GiB|PiB|Archive verified/);
  }
  await evidence(page, test.info(), 'design2-task-backups-unassociated-history');
  await history.locator('[data-run-id="' + state.runs[2].Id + '"]').getByRole('button').click();
  const result = page.getByRole('region', { name: 'Backup result', exact: true });
  await expect(result).toContainText('The backup was deleted. Its run history has been kept.');
  await expect(result.getByText('Archive size', { exact: true })).toHaveCount(0);
  await expect(result.getByText('Verification', { exact: true })).toHaveCount(0);
  await expect(result.getByText('SHA-256', { exact: true })).toHaveCount(0);
  expectNoBackupGuessing(api);
});

test('malformed optional backup projections never become verified measurements', async ({ page, api }) => {
  const valid = backupResult();
  const malformed: unknown[] = [{ ...valid, BackupId: undefined }, { ...valid, SizeBytes: 2480343613 }, { ...valid, SHA256: 'not-a-digest' },
    { ...valid, SizeBytes: '0' }, { ...valid, State: 'writing' }, { ...valid, Verified: 'true' }, { ...valid, SizeBytes: '9007199254740993.0' }];
  const state = installBackupTasks(api, malformed as TaskBackupResult[]);
  await page.goto(tasksPath);
  const history = await loadedHistory(page);
  for (const run of state.runs) await expect(history.locator('[data-run-id="' + run.Id + '"] .system-run-result')).toHaveText('Backup result unknown');
  await expect(history).not.toContainText(/Archive verified|GiB|PiB/);
  await history.locator('[data-run-id="' + state.runs[0].Id + '"]').getByRole('button').click();
  const result = page.getByRole('region', { name: 'Backup result', exact: true });
  await expect(result).toContainText('The backup result for this run is unknown.');
  await expect(result.getByText('Archive size', { exact: true })).toHaveCount(0);
  await expect(result.getByText('Archive verified', { exact: true })).toHaveCount(0);
});

test.describe('Chinese mobile backup results', () => {
  test.use({ preferences: { language: 'zh', theme: 'dark' }, viewport: { width: 390, height: 844 } });
  test('long archive identifiers and checksums stay readable on a narrow screen', async ({ page, api }) => {
    const state = installBackupTasks(api, [backupResult({ SizeBytes: '9007199254740993' })]);
    await page.goto(tasksPath);
    const history = await loadedHistory(page);
    await expect(page.getByText('0 个运行中 · 0 个排队 · 1 个任务', { exact: true })).toBeVisible();
    await expect(history.locator('.system-run-result')).toHaveText('8 PiB · 已校验');
    await history.locator('[data-run-id="' + state.runs[0].Id + '"]').getByRole('button').click();
    await expect(page.getByRole('dialog').locator('.system-task-run-source')).toHaveText('计划');
    const result = page.getByRole('region', { name: '备份结果', exact: true });
    await expect(result.getByText('9,007,199,254,740,993 字节', { exact: true })).toBeVisible();
    await expect(result.getByText(checksum, { exact: true })).toBeVisible();
    await noOverflow(page);
    const width = await page.getByRole('dialog').evaluate((element) => ({ scroll: element.scrollWidth, client: element.clientWidth }));
    expect(width.scroll).toBeLessThanOrEqual(width.client + 1);
    await evidence(page, test.info(), 'design2-task-backups-mobile-dark-zh');
    await page.getByRole('dialog').getByRole('button', { name: '返回运行记录', exact: true }).click();
    await expect(page.getByRole('dialog').getByRole('list', { name: '任务运行记录', exact: true }).locator('.system-task-run-source')).toHaveText('计划');
    expect(api.writes()).toEqual([]);
  });
});

test.describe('Chinese run source labels', () => {
  test.use({ preferences: { language: 'zh', theme: 'dark' } });
  for (const width of [1440, 390]) test(`${width}px known sources are localized and unknown sources remain exact in every run view`, async ({ page, api }) => {
    await page.setViewportSize({ width, height: width < 640 ? 844 : 900 });
    const sources = ['manual', 'schedule', 'startup', 'system_event', 'manual_schedule', 'Manual'];
    const labels = ['手动', '计划', '服务器启动', '系统事件', 'manual_schedule', 'Manual'];
    const state = installBackupTasks(api, sources.map(() => undefined));
    state.runs.forEach((run, index) => { run.Source = sources[index]; });
    await page.goto(tasksPath);
    const recent = await loadedHistory(page);
    for (let index = 0; index < sources.length; index += 1) {
      const row = recent.locator('[data-run-id="' + state.runs[index].Id + '"]');
      await expect(row.locator('.system-task-run-source')).toHaveText(labels[index]);
      await expect(row.locator('.system-task-run-source')).toHaveAttribute('translate', 'no');
      await expect(row.locator('.system-task-initiator')).toHaveText('发起者未知');
      await expect(row.locator('.system-task-initiator')).toHaveAttribute('data-actor-state', 'unknown');
    }
    await recent.locator('[data-run-id="' + state.runs[0].Id + '"]').getByRole('button').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog.locator('.system-task-run-source')).toHaveText(labels[0]);
    await dialog.getByRole('button', { name: '返回运行记录', exact: true }).click();
    const history = dialog.getByRole(width < 840 ? 'list' : 'table', { name: '任务运行记录', exact: true });
    await expect(history.locator('.system-task-run-source')).toHaveText(labels);
    for (let index = 0; index < sources.length; index += 1) {
      const row = history.getByRole(width < 840 ? 'listitem' : 'row').filter({ has: page.getByText(labels[index], { exact: true }) });
      await row.getByRole('button', { name: '查看运行', exact: true }).click();
      await expect(dialog.locator('.system-task-run-source')).toHaveText(labels[index]);
      await expect(dialog.locator('.system-task-run-source')).toHaveAttribute('translate', 'no');
      await dialog.getByRole('button', { name: '返回运行记录', exact: true }).click();
    }
    for (let index = 0; index < state.runs.length; index += 1) expect(api.reads('/admin/v1/task-runs/' + state.runs[index].Id)).toHaveLength(index === 0 ? 2 : 1);
    expectNoBackupGuessing(api);
  });
});
