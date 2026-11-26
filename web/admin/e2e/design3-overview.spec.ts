import type { Route } from '@playwright/test';
import type { Job, Library, LibraryStorageDirectory, TaskDefinition, TaskRun } from '../src/api';
import type { BackupPolicy } from '../src/backupPolicyApi';
import type { BackupView } from '../src/backupsApi';
import { expect, json, library, seriesLibrary, stamp, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

const ago = (hours: number) => new Date(Date.now() - hours * 3_600_000).toISOString();
const directoriesPath = '/admin/v1/storage/library-directories';
const tasksPath = '/admin/v1/tasks';
const policyPath = '/admin/v1/backups/policy';
const backupsPath = '/admin/v1/backups';
const id = (number: number) => number.toString(16).padStart(32, '0');
const third = { ...library, Id: id(51), Name: 'Documentary shelf', Paths: ['/media/docs'] };
const directory = (Path: string, UsedBytes = '400', Readable: boolean | null = true): LibraryStorageDirectory => ({ Path, Readable, Available: Readable === true, Writable: null,
  TotalBytes: Readable ? '1000' : null, UsedBytes: Readable ? UsedBytes : null, AvailableBytes: Readable ? String(1000 - Number(UsedBytes)) : null });
const job = (number: number, entry: Library, hours: number, Status = 'completed', Error = ''): Job => ({ Id: id(number), LibraryId: entry.Id, ForceProbe: false, Status, Error, Scanned: 15, Added: 4, Updated: 11, CreatedAt: ago(hours + .1), StartedAt: ago(hours + .05), FinishedAt: ago(hours) });
const run = (number: number, State: TaskRun['State'], hours = 2): TaskRun => ({ Id: id(number), TaskId: id(number + 100), State, Source: 'manual', RequestId: null,
  CreatedAt: ago(hours + .1), StartedAt: ago(hours + .05), FinishedAt: State === 'running' ? null : ago(hours), StopRequestedAt: null, StopReason: '', ErrorCode: '', ErrorMessage: '',
  TotalChildren: 20, TerminalChildren: 11, CompletedChildren: 10, FailedChildren: 1, UnavailableChildren: 0, InterruptedChildren: 0, CancelledChildren: 0, Scanned: 10, Added: 0, Updated: 0 });
const task = (summary: TaskRun, Key: string, Name: string): TaskDefinition => ({ Id: summary.TaskId, Key, Name, Description: '', Category: 'System', IsHidden: false, Enabled: true,
  Revision: '1', ScheduleTimezone: 'UTC', Triggers: [], CurrentRun: summary.State === 'running' ? summary : null, LastRun: summary.State === 'running' ? null : summary, NextRunAt: null });
const policy = (Enabled: boolean): BackupPolicy => ({ Revision: '1', Enabled, Schedule: { Kind: 'daily', DayOfWeek: null, TimeOfDay: '03:00', Timezone: 'UTC' },
  KeepCount: 7, IncludeImages: true, HasPassphrase: true, NextRunAt: null, Available: true, UnavailableReason: '', TaskId: id(333), LastRunAt: stamp, LastRunState: 'completed', LastBackupId: id(334) });

function items<T>(values: T[]) { return { Items: values, TotalRecordCount: values.length }; }

const archive = (index: number, CreatedAt: string, patch: Partial<BackupView> = {}): BackupView => ({
  Id: id(index), Kind: 'generated', State: 'ready', Verified: true, CreatedAt, UpdatedAt: CreatedAt,
  SizeBytes: '1024', SHA256: 'a'.repeat(64), ErrorCode: '', Source: null, ...patch,
});

test('overview finds a retained successful backup beyond the first page without using policy timestamps', async ({ page, api }) => {
  const latest = archive(2, '2026-11-25T18:00:00Z');
  const failedPolicy = { ...policy(false), LastRunAt: '2026-11-26T22:00:00Z', LastRunState: 'failed' };
  api.handlers.set(`GET ${policyPath}`, (route) => json(route, failedPolicy));
  const ineligible = Array.from({ length: 100 }, (_, index) => archive(200 - index, '2026-11-26T19:00:00Z', {
    ...(index === 0 ? { State: 'failed', Verified: false } : { Kind: 'imported' }),
  }));
  const archives = [...ineligible, latest, archive(1, stamp, { UpdatedAt: '2026-11-26T20:00:00Z' })];
  const requests: number[] = [];
  api.handlers.set(`GET ${backupsPath}`, (route, request) => {
    const query = new URL(request.url()).searchParams;
    const start = Number(query.get('StartIndex'));
    const limit = Number(query.get('Limit'));
    requests.push(start);
    expect(limit).toBe(100);
    return json(route, { Items: archives.slice(start, start + limit), StartIndex: start, Limit: limit, TotalRecordCount: archives.length });
  });
  await page.goto('/admin/');
  const backup = page.getByRole('region', { name: 'Service status' }).getByRole('link', { name: /^Backups/ });
  await expect(backup).toContainText('No automatic backup rules');
  await expect(backup).toContainText('Latest successful backup:');
  await expect(backup.locator('time')).toHaveAttribute('datetime', latest.CreatedAt);
  await expect(page.getByRole('main')).not.toContainText('Last automatic backup run');
  expect(requests).toEqual([0, 100]);

  await page.getByRole('navigation', { name: 'Administration' }).getByRole('link', { name: 'Tasks', exact: true }).click();
  await page.getByRole('button', { name: 'Needs attention', exact: true }).click();
  const bell = page.getByRole('dialog', { name: 'Needs attention', exact: true });
  await bell.getByRole('button', { name: 'Refresh attention checks' }).click();
  await expect(bell.getByRole('link', { name: /^No automatic backup rules/ })).toBeVisible();
  expect(requests).toEqual([0, 100]);
  expect(api.writes()).toHaveLength(0);
});

test('backup lookup failure stays distinct from a confirmed empty catalog and a stale result', async ({ page, api }) => {
  const latest = archive(2, '2026-11-25T18:00:00Z');
  let response: 'offline' | 'empty' | 'ready' = 'offline';
  api.handlers.set(`GET ${backupsPath}`, (route) => response === 'offline' ? json(route, { Error: { Code: 'storage_unavailable', Message: 'Cannot read archives' } }, 503)
    : json(route, { ...items(response === 'ready' ? [latest] : []), StartIndex: 0, Limit: 100 }));
  await page.goto('/admin/');
  const backup = page.getByRole('region', { name: 'Service status' }).getByRole('link', { name: /^Backups/ });
  const refresh = page.getByRole('button', { name: 'Refresh overview' });
  await expect(backup).toContainText('Could not check the latest successful backup.');
  await expect(backup).toContainText('Automatic backup scheduled');
  response = 'empty';
  await refresh.click();
  await expect(backup).toContainText('No successful backups are retained.');
  response = 'ready';
  await refresh.click();
  await expect(backup.locator('time')).toHaveAttribute('datetime', latest.CreatedAt);
  response = 'offline';
  await refresh.click();
  await expect(backup).toContainText('Showing the last successful check.');
  await expect(backup.locator('time')).toHaveAttribute('datetime', latest.CreatedAt);
});

test.describe('Chinese overview backup status on a phone', () => {
  test.use({ preferences: { language: 'zh', theme: 'dark' }, viewport: { width: 390, height: 844 } });
  test('shows a real archive date in the existing backup cell', async ({ page, api }, testInfo) => {
    const latest = archive(2, '2026-11-25T18:00:00Z');
    api.handlers.set(`GET ${backupsPath}`, (route) => json(route, { ...items([latest]), StartIndex: 0, Limit: 100 }));
    await page.goto('/admin/');
    const backup = page.getByRole('region', { name: '服务状态' }).getByRole('link', { name: /最近成功备份：/ });
    await expect(backup.locator('time')).toHaveAttribute('datetime', latest.CreatedAt);
    await noOverflow(page);
    await evidence(page, testInfo, 'design3-latest-successful-backup-mobile');
  });
});

test('the sidebar stays neutral until both reads and retains dated results across a failure', async ({ page, api }) => {
  const systemPath = '/admin/v1/system/status';
  let release = () => {};
  const firstStatus = new Promise<void>((resolve) => { release = resolve; });
  api.handlers.set(`GET ${systemPath}`, async (route) => { await firstStatus; await json(route, api.status); });
  try {
    await page.goto('/admin/');
    const sidebar = page.locator('.sidebar-server');
    await expect(sidebar).toHaveAttribute('data-state', 'checking');
    await expect(sidebar).toContainText('Checking server status…');
    await expect(page.getByRole('main').getByText('Goby living room', { exact: true })).toBeVisible();
    const counts = page.locator('.admin-sidebar .nav-count');
    await expect(counts).toHaveCount(0);
    expect(await sidebar.locator('.status-dot').evaluate((dot) => {
      const color = getComputedStyle(dot).backgroundColor;
      const swatch = document.createElement('span');
      swatch.style.backgroundColor = 'var(--ink3)';
      dot.append(swatch);
      const neutral = color === getComputedStyle(swatch).backgroundColor;
      swatch.remove();
      return neutral;
    })).toBe(true);
    release();
    await expect(sidebar).toHaveAttribute('data-state', 'ready');
    await expect(counts).toHaveText(['2', '2']);
    api.handlers.set(`GET ${systemPath}`, (route) => json(route, { Error: { Code: 'offline', Message: 'No status returned' } }, 503));
    await page.getByRole('button', { name: 'Refresh overview' }).click();
    await expect(sidebar).toHaveAttribute('data-state', 'failed');
    await expect(sidebar).toContainText(/Refresh failed · showing results from \d{2}:\d{2}/);
    await expect(sidebar).toContainText('Goby living room');
    await expect(counts).toHaveText(['2', '2']);
  } finally { release(); }
});

test('the phone attention panel fills the available width with eight-pixel gutters', async ({ page, api }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/admin/');
  await page.getByRole('button', { name: 'Needs attention', exact: true }).click();
  const bell = page.getByRole('dialog', { name: 'Needs attention', exact: true });
  await expect(bell.getByRole('heading', { name: 'Needs attention 0' })).toBeVisible();
  await expect.poll(async () => {
    const rect = await bell.boundingBox();
    return rect && { left: Math.round(rect.x), right: Math.round(390 - rect.x - rect.width) };
  }).toEqual({ left: 8, right: 8 });
  await noOverflow(page);
  await evidence(page, testInfo, 'design3-phone-attention');
  expect(api.writes()).toHaveLength(0);
});

test('the overview timestamps successful reads rather than ticking the browser clock', async ({ page, api }) => {
  const start = new Date('2026-11-26T14:00:00Z');
  await page.clock.install({ time: start });
  await page.clock.pauseAt(new Date(start.getTime() + 1_000));
  const paths = ['/admin/v1/overview', '/admin/v1/system/status', directoriesPath, policyPath, backupsPath, tasksPath, '/admin/v1/jobs', '/admin/v1/libraries', '/admin/v1/settings'];
  for (const path of paths) api.handlers.set(`GET ${path}`, (route) => json(route, { Error: { Code: 'offline', Message: 'No observation' } }, 503));
  await page.goto('/admin/');
  const updated = page.locator('.admin-header time');
  await expect(page.getByText('Could not check service status', { exact: true })).toBeVisible();
  await expect(updated).toHaveCount(0);

  for (const path of paths) api.handlers.delete(`GET ${path}`);
  await page.getByRole('button', { name: 'Refresh overview' }).click();
  await expect(updated).toHaveAttribute('datetime', '2026-11-26T14:00:01.000Z');
  await expect(updated).toHaveText(/^Data updated at \d{2}:\d{2}:\d{2}$/);
  await page.clock.runFor(2_000);
  await expect(updated).toHaveAttribute('datetime', '2026-11-26T14:00:01.000Z');
  await page.getByRole('button', { name: 'Refresh overview' }).click();
  await expect(updated).toHaveAttribute('datetime', '2026-11-26T14:00:03.000Z');
  await page.getByRole('button', { name: 'Account: Dashboard administrator' }).click();
  await page.getByRole('menuitemradio', { name: '中文' }).click();
  await expect(updated).toHaveText(/^数据更新于 \d{2}:\d{2}:\d{2}$/);
  await page.getByRole('navigation', { name: 'Administration' }).getByRole('link', { name: /^用户/ }).click();
  await expect(updated).toHaveCount(0);
});

test('attention shares ordered issues, independent checks, and recovery between the overview and bell', async ({ page, api }) => {
  const clockStart = Date.now();
  await page.clock.install({ time: clockStart });
  await page.clock.pauseAt(clockStart + 1_000);
  const partial = { ...run(85, 'completed', 3), TerminalChildren: 20, CompletedChildren: 19 };
  const failing = { ...run(84, 'failed'), ErrorMessage: 'Work item failed' };
  const old = run(83, 'failed', 25);
  const observedTasks = [task(partial, 'images.missing', 'Fetch artwork'), task(failing, 'credits', 'Identify credits'), task(old, 'old', 'Old failure')];
  let database = 'degraded';
  const db = (Status: string) => ({ Server: { Id: id(300), Name: 'Test media host', Version: '0.1.0' }, Database: { Engine: 'PostgreSQL', Status }, Counts: {} });
  api.handlers.set('GET /admin/v1/overview', (route) => json(route, db(database)));
  api.handlers.set('GET /admin/v1/libraries', (route) => json(route, items([library, seriesLibrary, third])));
  api.handlers.set('GET ' + directoriesPath, (route) => json(route, items([directory(library.Paths[0], '900'), directory(seriesLibrary.Paths[0]), directory(third.Paths[0], '0', false)])));
  api.handlers.set('GET /admin/v1/jobs', (route) => json(route, items([job(1, library, 4, 'failed', 'Older failure'), job(2, library, 3.5),
    job(3, third, 2, 'failed', '1 media directories could not be scanned; existing catalog records were retained'), job(4, seriesLibrary, 1, 'failed', 'A different scan failure')])));
  api.handlers.set('GET ' + tasksPath, (route) => json(route, items(observedTasks)));
  api.handlers.set('GET ' + policyPath, (route) => json(route, policy(false)));
  api.settings.Runtime!.Network.RestartRequired = true;
  api.settings.Runtime!.Network.Desired.HttpPort = 9011;
  await page.goto('/admin/');
  const todo = page.getByRole('region', { name: 'To do' });
  const rows = todo.locator('[data-attention-id]');
  await expect(rows).toHaveCount(8);
  await expect(todo).toContainText('8 total, 3 critical');
  await expect(todo).not.toContainText(/Older failure|Old failure|Documentary shelf: scan failed/);
  await expect(todo).toContainText('Identify credits: run failed');
  await expect(todo).toContainText('Fetch artwork: some work failed');
  await expect(todo.locator('[data-severity="critical"]')).toHaveCount(3);
  expect((await rows.evaluateAll((elements) => elements.map((row) => row.getAttribute('data-attention-id')))).slice(3, 6)).toEqual([
    `scan:${seriesLibrary.Id}`, `task:${failing.TaskId}`, `task:${partial.TaskId}`,
  ]);
  await page.getByRole('button', { name: 'Needs attention', exact: true }).click();
  const bell = page.getByRole('dialog', { name: 'Needs attention', exact: true });
  await expect(bell.getByRole('heading', { name: 'Needs attention 8' })).toBeVisible();
  expect(await bell.locator('[data-attention-id]').evaluateAll((elements) => elements.map((row) => row.getAttribute('data-attention-id')))).toEqual(
    await rows.evaluateAll((elements) => elements.map((row) => row.getAttribute('data-attention-id'))));
  expect(api.reads('/admin/v1/jobs')).toHaveLength(1);
  expect(api.reads('/admin/v1/playback/sessions')).toHaveLength(0);
  expect(api.reads('/admin/v1/activity')).toHaveLength(0);

  api.handlers.set('GET ' + directoriesPath, (route) => json(route, { Error: { Code: 'offline', Message: 'Observation failed.' } }, 503));
  await bell.getByRole('button', { name: 'Refresh attention checks' }).click();
  await expect(bell.locator('[data-check="directory-status"]')).toHaveAttribute('data-state', 'failed');
  await expect(todo.locator('[data-check="directory-status"]')).toHaveAttribute('data-stale', 'true');
  await expect(rows.filter({ hasText: '/media/movies is 90% full' })).toHaveAttribute('data-stale', 'true');
  await expect(todo.getByText('Nothing needs attention')).toHaveCount(0);

  api.handlers.set('GET ' + directoriesPath, (route) => json(route, items([directory(library.Paths[0]), directory(seriesLibrary.Paths[0]), directory(third.Paths[0])])));
  api.handlers.set('GET /admin/v1/jobs', (route) => json(route, items([job(11, library, 1), job(12, seriesLibrary, 1), job(13, third, 1)])));
  api.handlers.set('GET ' + tasksPath, (route) => json(route, items([])));
  api.handlers.set('GET ' + policyPath, (route) => json(route, policy(true)));
  api.settings.Runtime!.Network.RestartRequired = false;
  database = 'connected';
  await bell.getByRole('button', { name: 'Refresh attention checks' }).click();
  await expect(bell.getByRole('heading', { name: 'Needs attention 0' })).toBeVisible();
  await expect(todo.getByText('Nothing needs attention')).toBeVisible();
  await expect(rows).toHaveCount(0);
  expect(api.writes()).toEqual([]);
});

test('recent scans show six distinct latest finished results and activity shows real work progress', async ({ page, api }, testInfo) => {
  const list = [library, seriesLibrary, third, ...Array.from({ length: 5 }, (_, index) => ({ ...library, Id: id(430 + index), Name: `Shelf ${index + 1}`, Paths: [library.Paths[0]] }))];
  const scan = task({ ...run(95, 'completed', 20), FailedChildren: 0 }, 'library.scan', 'Scan media libraries');
  scan.NextRunAt = '2027-01-02T03:04:00Z';
  const current = task(run(96, 'running'), 'images.missing', 'Download missing images');
  api.handlers.set('GET /admin/v1/libraries', (route) => json(route, items(list)));
  api.handlers.set('GET ' + directoriesPath, (route) => json(route, items([directory(library.Paths[0], '890'), directory(seriesLibrary.Paths[0]), directory(third.Paths[0])])));
  api.handlers.set('GET /admin/v1/jobs', (route) => json(route, items([...list.map((entry, index) => job(200 + index, entry, index + 1, index === 1 ? 'failed' : 'completed', index === 1 ? 'Storage ownership changed' : '')),
    job(296, library, 9), { ...job(298, third, 0), Status: 'running', FinishedAt: null }])));
  api.handlers.set('GET ' + tasksPath, (route) => json(route, items([scan, current])));
  await page.goto('/admin/');
  const recent = page.getByRole('region', { name: 'Recent scans' });
  await expect(recent.locator('.ov-scan-row')).toHaveCount(6);
  await expect(recent.locator('.ov-scan-row').first()).toContainText(library.Name);
  await expect(recent).toContainText('Added 4 · updated 11');
  await expect(recent).toContainText('Storage ownership changed');
  await expect(recent).not.toContainText('Shelf 5');
  const inProgress = page.getByRole('region', { name: 'In progress' });
  await expect(inProgress.getByRole('progressbar', { name: 'Download missing images · Work progress' })).toHaveAttribute('aria-valuetext', '11 of 20 work items finished');
  await expect(inProgress).toContainText('11 / 20');
  await expect(inProgress).toContainText(`Scanning ${third.Name}`);
  await expect(inProgress.locator('time')).toHaveAttribute('datetime', scan.NextRunAt);
  await expect(page.getByRole('region', { name: 'Service status' }).getByRole('link')).toHaveCount(5);
  await expect(page.getByRole('main')).not.toContainText(/Recent activity|Playback sessions|CPU|Memory/);
  await expect(page.locator('.ov-status-strip')).toBeVisible();
  await noOverflow(page);
  await evidence(page, testInfo, 'design3-overview-desktop-light');
  await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await noOverflow(page);
  await evidence(page, testInfo, 'design3-overview-phone-dark');
  await recent.getByRole('link', { name: new RegExp(library.Name) }).click();
  await expect(page).toHaveURL(new RegExp(`/admin/media/libraries/${library.Id}/items$`));
  expect(api.writes()).toEqual([]);
});

test('failed status check times out without preventing other independent observations', async ({ page, api }) => {
  const pending: Route[] = [];
  api.handlers.set('GET /admin/v1/system/status', async (route) => { pending.push(route); });
  try {
    await page.goto('/admin/');
    const todo = page.getByRole('region', { name: 'To do' });
    await expect(todo).toContainText('Checking service status');
    await expect(page.getByRole('region', { name: 'Service status' }).getByRole('link', { name: /^Database/ })).toContainText('Normal');
    await expect(todo.getByText('Could not check service status')).toBeVisible({ timeout: 20_000 });
    await expect(todo.getByText('Nothing needs attention')).toHaveCount(0);
    expect(api.reads('/admin/v1/tasks').length).toBeGreaterThan(1);
    api.handlers.delete('GET /admin/v1/system/status');
    await page.getByRole('button', { name: 'Refresh overview' }).click();
    await expect(todo.getByText('Nothing needs attention')).toBeVisible();
  } finally { await Promise.all(pending.map((route) => route.abort().catch(() => undefined))); }
});

test('an uncertain overview scan retains the same task request ID for explicit recovery', async ({ page, api }) => {
  const scanRun = { ...run(319, 'completed'), Source: 'manual' };
  const scan = task(scanRun, 'library.scan', 'Scan media libraries');
  const requestPath = `${tasksPath}/${scan.Id}/runs`;
  api.handlers.set('GET ' + tasksPath, (route) => json(route, items([scan])));
  api.handlers.set('POST ' + requestPath, (route) => route.abort('failed'));
  await page.goto('/admin/');
  const start = page.getByRole('button', { name: 'Scan all libraries', exact: true });
  await start.click();
  await expect(page.getByRole('alert')).toContainText('The task outcome is unknown.');
  await page.reload();
  await expect(page.getByRole('alert')).toContainText('The task outcome is unknown.');
  await expect(start).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
  const { RequestId } = api.writes()[0].body as { RequestId: string };
  expect(RequestId).toMatch(/^[a-f0-9-]{36}$/);
  api.handlers.set('POST ' + requestPath, (route) => json(route, { Run: { ...scanRun, RequestId }, Admitted: false }, 202));
  api.handlers.set('GET /admin/v1/task-runs/' + scanRun.Id, (route, request) => {
    const query = new URL(request.url()).searchParams;
    return json(route, { Run: { ...scanRun, RequestId }, Children: { ...items([]), StartIndex: Number(query.get('StartIndex') ?? 0), Limit: Number(query.get('Limit') ?? 50) } });
  });
  await page.getByRole('link', { name: 'Open tasks', exact: true }).click();
  await page.getByRole('button', { name: 'Check start result', exact: true }).click();
  await expect(page.getByRole('dialog', { name: /Run details/ })).toBeVisible();
  expect(api.writes().map(({ path, body }) => [path, body])).toEqual([[requestPath, { RequestId }], [requestPath, { RequestId }]]);
});

test('all new search aliases rank the corresponding destination first and Enter opens it', async ({ page, api }) => {
  await page.goto('/admin/');
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
  for (const [query, label, route] of [
    ['账户', 'Users', '/admin/access/users'], ['计划', 'Tasks', '/admin/system/tasks'],
    ['硬件加速', 'Transcoding', '/admin/settings/transcode'], ['在线提供方', 'Metadata & subtitles', '/admin/settings/metadata'], ['gailan', 'Overview', '/admin/'],
  ]) {
    await page.keyboard.press('Control+k');
    const palette = page.getByRole('dialog', { name: 'Search or jump to' });
    const search = palette.getByRole('combobox');
    await search.fill(query);
    const first = palette.getByRole('option').first();
    await expect(first).toContainText(label);
    await expect(first).not.toContainText(query);
    await expect(first).toHaveAttribute('aria-selected', 'true');
    await search.press('Enter');
    await expect(page).toHaveURL(new RegExp(`${route}$`));
  }
  expect(api.writes()).toHaveLength(0);
});

test('old access paths and cached tabs fall back to Users; the palette offers only the Users destination', async ({ page, api }) => {
  await page.addInitScript(() => { if (!sessionStorage.getItem('goby.dashboard.tabs')) sessionStorage.setItem('goby.dashboard.tabs', JSON.stringify({ access: 'sessions' })); });
  for (const old of ['access/sessions', 'access/devices', 'sessions', 'devices']) {
    await page.goto(`/admin/${old}?source=old#login`);
    await expect(page).toHaveURL(/\/admin\/access\/users\?source=old#login$/);
    await expect(page.getByRole('heading', { name: 'Users', exact: true })).toBeVisible();
  }
  expect(await page.evaluate(() => JSON.parse(sessionStorage.getItem('goby.dashboard.tabs')!).access)).toBe('users');
  const nav = page.getByRole('navigation', { name: 'Administration' });
  await expect(nav.getByRole('link')).toHaveCount(13);
  await expect(nav.getByRole('link', { name: /^(Devices|Sessions)$/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'Search or jump to', exact: true }).click();
  const palette = page.getByRole('dialog', { name: 'Search or jump to' });
  await expect(palette.getByRole('option', { name: /^(Devices|Sessions)/ })).toHaveCount(0);
  const search = palette.getByRole('combobox');
  for (const term of ['devices', 'sessions', 'login', '设备', '会话', '登录']) {
    await search.fill(term);
    await expect(palette.getByRole('option')).toHaveCount(1);
    await expect(palette.getByRole('option', { name: /^Users/ })).toBeVisible();
  }
  await search.press('Enter');
  await expect(palette).not.toBeVisible();
  expect(api.reads('/admin/v1/sessions')).toHaveLength(0);
  expect(api.reads('/admin/v1/devices')).toHaveLength(0);
  expect(api.writes()).toEqual([]);
});
