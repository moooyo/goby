import type { Locator, Page } from '@playwright/test';
import { expect, json, test } from './dashboard-fixture';
import { evidence } from './dashboard-review';
import type { DashboardAPI } from './dashboard-fixture';
import type { ActivityEntry, TaskDefinition, TaskRun, TaskScheduleInput, TaskScheduleUpdate, TaskTrigger } from '../src/api';

const timestamp = '2026-11-07T07:12:00Z';
const historyNow = '2026-11-07T08:00:00Z';
const nextFire = '2026-11-08T03:00:00Z';
const revision = '9007199254740993';
const tasksPath = '/admin/system/tasks';

function trigger(overrides: Partial<TaskTrigger> = {}): TaskTrigger {
  return { Id: 'daily-rule', Kind: 'daily', IntervalTicks: null, TimeOfDayTicks: '108000000000', DayOfWeek: null, MaxRuntimeTicks: null, NextFireAt: nextFire, CalculationError: '', ...overrides };
}

function task(id = 'backup-task', key = 'backup.create', name = 'Create automatic backup', category = 'Backup', triggers: TaskTrigger[] = []): TaskDefinition {
  return { Id: id, Key: key, Name: name, Description: 'A task backed by recorded server state.', Category: category, IsHidden: false, Enabled: true, Revision: revision, ScheduleTimezone: 'Asia/Shanghai', Triggers: triggers, CurrentRun: null, LastRun: null, NextRunAt: triggers.find((value) => value.NextFireAt)?.NextFireAt ?? null };
}

function run(definition: TaskDefinition, state: TaskRun['State'], id = definition.Id + '-run'): TaskRun {
  return { Id: id, TaskId: definition.Id, State: state, Source: 'manual', RequestId: null, CreatedAt: timestamp, StartedAt: state === 'pending' ? null : timestamp, FinishedAt: state === 'completed' ? timestamp : null, StopRequestedAt: null, StopReason: '', ErrorCode: '', ErrorMessage: '', TotalChildren: 3, TerminalChildren: state === 'completed' ? 3 : state === 'running' ? 1 : 0, CompletedChildren: state === 'completed' ? 3 : state === 'running' ? 1 : 0, FailedChildren: 0, CancelledChildren: 0, InterruptedChildren: 0, UnavailableChildren: 0, Scanned: state === 'pending' ? 0 : 1_204, Added: 18, Updated: 6 };
}

function runMinutesAgo(definition: TaskDefinition, id: string, minutes: number, state: TaskRun['State'] = 'completed'): TaskRun {
  const created = new Date(Date.parse(historyNow) - minutes * 60_000).toISOString();
  return { ...run(definition, state, id), CreatedAt: created, StartedAt: state === 'pending' ? null : created, FinishedAt: state === 'completed' ? created : null };
}

function admission(run: TaskRun, id: string, actor: ActivityEntry['Actor'], overrides: Partial<ActivityEntry> = {}): ActivityEntry {
  return { Id: id, Date: run.CreatedAt, Action: 'task.admitted', Severity: 'Info', Source: actor.Kind === 'system' ? 'system' : 'native', Actor: actor, Resource: { Kind: 'task_run', Id: run.Id }, Revision: '1', Count: '0', State: null, ChangedFields: [], Name: 'Task admitted', Overview: '', ...overrides };
}

function installAdmissions(api: DashboardAPI, entries: ActivityEntry[], filter = true) {
  const state = { entries: structuredClone(entries), failures: new Map<number, string>(), reads: [] as { start: number; limit: number; action: string | null; minDate: string | null }[] };
  api.handlers.set('GET /admin/v1/activity', (route, request) => {
    const query = new URL(request.url()).searchParams;
    const start = Number(query.get('StartIndex') ?? 0); const limit = Number(query.get('Limit') ?? 50);
    const action = query.get('Action'); const minDate = query.get('MinDate');
    if (action === 'task.admitted') state.reads.push({ start, limit, action, minDate });
    const failure = state.failures.get(start);
    if (failure && action === 'task.admitted') return json(route, { Error: { Code: 'activity_unavailable', Message: failure } }, 503);
    const values = state.entries.filter((entry) => !filter || (!action || entry.Action === action) && (!minDate || Date.parse(entry.Date) >= Date.parse(minDate)))
      .sort((left, right) => Date.parse(right.Date) - Date.parse(left.Date) || (BigInt(left.Id) < BigInt(right.Id) ? 1 : BigInt(left.Id) > BigInt(right.Id) ? -1 : 0));
    return json(route, { Items: values.slice(start, start + limit), TotalRecordCount: values.length, StartIndex: start, Limit: limit, RetentionDays: 30 });
  });
  return state;
}

function installTasks(api: DashboardAPI, definitions = [task()]) {
  installAdmissions(api, []);
  const tasks = structuredClone(definitions);
  const previews: TaskScheduleInput[] = [];
  const saves: TaskScheduleUpdate[] = [];
  const starts: string[] = [];
  const histories = new Map(tasks.map((definition) => [definition.Id, [definition.CurrentRun, definition.LastRun].filter((value): value is TaskRun => value !== null).filter((value, index, values) => values.findIndex((other) => other.Id === value.Id) === index)]));
  const historyReads: { taskId: string; start: number; limit: number }[] = [];
  const historyFailures = new Map<string, string>();
  api.handlers.set('GET /admin/v1/tasks', (route) => json(route, { Items: tasks, TotalRecordCount: tasks.length }));

  function registerRun(value: TaskRun) {
    api.handlers.set('GET /admin/v1/task-runs/' + value.Id, (route, request) => {
      const query = new URL(request.url()).searchParams;
      return json(route, { Run: value, Children: { Items: [], TotalRecordCount: 0, StartIndex: Number(query.get('StartIndex') ?? 0), Limit: Number(query.get('Limit') ?? 50) } });
    });
  }

  function applySchedule(definition: TaskDefinition, input: TaskScheduleUpdate) {
    definition.Revision = (BigInt(input.Revision) + 1n).toString();
    definition.ScheduleTimezone = input.ScheduleTimezone;
    definition.Triggers = input.Triggers.map((value, index) => ({
      Id: 'saved-rule-' + index, Kind: value.Kind, SystemEvent: value.SystemEvent ?? null, IntervalTicks: value.IntervalTicks ?? null,
      TimeOfDayTicks: value.TimeOfDayTicks ?? null, DayOfWeek: value.DayOfWeek ?? null, MaxRuntimeTicks: value.MaxRuntimeTicks ?? null,
      NextFireAt: ['startup', 'system_event'].includes(value.Kind) ? null : nextFire, CalculationError: '',
    }));
    definition.NextRunAt = definition.Triggers.find((value) => value.NextFireAt)?.NextFireAt ?? null;
  }

  for (const definition of tasks) {
    const path = '/admin/v1/tasks/' + definition.Id;
    for (const value of [definition.CurrentRun, definition.LastRun]) if (value) registerRun(value);
    api.handlers.set('GET ' + path, (route) => json(route, { Task: definition }));
    api.handlers.set('GET ' + path + '/runs', (route, request) => {
      const query = new URL(request.url()).searchParams;
      const start = Number(query.get('StartIndex') ?? 0); const limit = Number(query.get('Limit') ?? 50);
      historyReads.push({ taskId: definition.Id, start, limit });
      const failure = historyFailures.get(definition.Id);
      if (failure) return json(route, { Error: { Code: 'history_unavailable', Message: failure } }, 503);
      const values = [...(histories.get(definition.Id) ?? [])].sort((left, right) => Date.parse(right.CreatedAt) - Date.parse(left.CreatedAt) || right.Id.localeCompare(left.Id));
      return json(route, { Items: values.slice(start, start + limit), TotalRecordCount: values.length, StartIndex: start, Limit: limit });
    });
    api.handlers.set('POST ' + path + '/triggers/preview', (route, request) => {
      const input = request.postDataJSON() as TaskScheduleInput;
      previews.push(input);
      return json(route, { ServerTime: timestamp, Items: input.Triggers.map((value, index) => ({ Index: index, Occurrences: ['startup', 'system_event'].includes(value.Kind) ? [] : [nextFire], Event: value.Kind === 'startup' ? 'Server startup' : value.Kind === 'system_event' ? 'Matching server event' : null })) });
    });
    api.handlers.set('PUT ' + path + '/triggers', (route, request) => {
      const input = request.postDataJSON() as TaskScheduleUpdate;
      saves.push(input);
      if (input.Revision !== definition.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'The saved schedule changed.' } }, 409);
      applySchedule(definition, input);
      return json(route, { Task: definition });
    });
    api.handlers.set('POST ' + path + '/runs', (route, request) => {
      const requestId = (request.postDataJSON() as { RequestId: string }).RequestId;
      starts.push(requestId);
      definition.CurrentRun = { ...run(definition, 'pending'), RequestId: requestId };
      registerRun(definition.CurrentRun);
      return json(route, { Run: definition.CurrentRun, Admitted: true });
    });
  }
  return { tasks, previews, saves, starts, applySchedule, histories, historyReads, historyFailures };
}

function taskRow(page: Page, name: string) {
  return page.getByRole('list', { name: 'Available server tasks', exact: true }).getByRole('listitem').filter({ has: page.getByRole('heading', { name, exact: true }) });
}

async function openRules(page: Page, id = 'backup-task'): Promise<Locator> {
  await page.goto(tasksPath + '?task=' + encodeURIComponent(id));
  const dialog = page.getByRole('dialog', { name: /^Trigger rules/ });
  await expect(dialog.getByRole('region', { name: 'Add a rule', exact: true })).toBeVisible();
  return dialog;
}

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });
test.beforeEach(async ({ page }) => { await page.clock.install({ time: new Date(historyNow) }); });

test('manual-only tasks remain runnable beside recorded runs and task type filters', async ({ page, api }) => {
  const scan = task('scan-task', 'library.scan', 'Scan media libraries', 'Library');
  scan.CurrentRun = run(scan, 'running');
  const analysis = task('analysis-task', 'analysis.intros', 'Analyze episode introductions', 'Media analysis');
  analysis.CurrentRun = run(analysis, 'pending');
  const backup = task();
  backup.LastRun = run(backup, 'completed', 'backup-last-run');
  const state = installTasks(api, [scan, analysis, backup]);
  await page.goto(tasksPath);
  await expect(page.getByText('1 running · 1 queued · 3 tasks', { exact: true })).toBeVisible();
  const row = taskRow(page, backup.Name);
  await expect(row).toContainText('No triggers · manual only');
  await expect(row.getByRole('button', { name: 'Start task', exact: true })).toBeEnabled();
  await expect(row.getByRole('button', { name: 'Edit schedule', exact: true })).toBeVisible();
  await expect(row.getByRole('switch')).toHaveCount(0);
  const history = page.getByRole('table', { name: 'Recent task runs', exact: true });
  await page.getByRole('group', { name: 'Task type filter', exact: true }).getByRole('button', { name: 'Backups', exact: true }).click();
  await expect(history.getByRole('row')).toHaveCount(2);
  await expect(history).toContainText(backup.Name);
  await expect(history).not.toContainText(scan.Name);
  await row.getByRole('button', { name: 'Edit schedule', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(tasksPath + '\\?task=backup-task$'));
  await page.getByRole('dialog', { name: /^Trigger rules/ }).getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(tasksPath + '$'));
  await row.getByRole('button', { name: 'Start task', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Run details · ' + backup.Name, exact: true })).toBeVisible();
  expect(state.starts).toEqual([expect.stringMatching(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)]);
  expect(state.saves).toEqual([]);
});

test('a task deep link retains system events and exact tick values when adding another OR rule', async ({ page, api }) => {
  const definition = task('analysis-task', 'analysis.intros', 'Analyze episode introductions', 'Media analysis', [
    trigger({ Id: 'event-rule', Kind: 'system_event', SystemEvent: 'LibraryChanged', TimeOfDayTicks: null, MaxRuntimeTicks: '10000001', NextFireAt: null }),
    trigger({ Id: 'interval-rule', Kind: 'interval', IntervalTicks: '37000000001', TimeOfDayTicks: null }),
    trigger({ Id: 'daily-rule', TimeOfDayTicks: '108001234567' }),
    trigger({ Id: 'startup-rule', Kind: 'startup', TimeOfDayTicks: null, MaxRuntimeTicks: '90000000001', NextFireAt: null }),
  ]);
  const state = installTasks(api, [definition]);
  const dialog = await openRules(page, definition.Id);
  await expect(dialog).toContainText('4 rules · runs when any rule matches');
  expect(api.writes()).toEqual([]);
  const ruleComposer = dialog.getByRole('region', { name: 'Add a rule', exact: true });
  await evidence(page, test.info(), 'design2-tasks-trigger-rules-daily');
  await ruleComposer.getByRole('button', { name: 'Interval', exact: true }).click();
  await evidence(page, test.info(), 'design2-tasks-trigger-rules-interval');
  await ruleComposer.getByRole('button', { name: 'Startup', exact: true }).click();
  await evidence(page, test.info(), 'design2-tasks-trigger-rules-startup');
  await ruleComposer.getByRole('button', { name: 'Reset new rule', exact: true }).click();
  await dialog.getByRole('button', { name: 'Edit trigger 1', exact: true }).click();
  const eventRule = dialog.getByRole('region', { name: 'Trigger 1', exact: true });
  await expect(eventRule.getByLabel('Maximum run time', { exact: true })).toHaveValue('1.0000001');
  await eventRule.getByLabel('System event', { exact: true }).selectOption('ConfigurationChanged');
  await eventRule.getByRole('button', { name: 'Done editing rule', exact: true }).click();
  const composer = dialog.getByRole('region', { name: 'Add a rule', exact: true });
  await composer.getByRole('button', { name: 'Weekly', exact: true }).click();
  await composer.getByLabel('Day of week', { exact: true }).selectOption('1');
  await composer.getByLabel('Time of day', { exact: true }).fill('04:05:06.1234567');
  await composer.getByRole('button', { name: 'Add trigger', exact: true }).click();
  await expect(dialog).toContainText('5 rules · runs when any rule matches');
  await dialog.getByRole('button', { name: 'Preview schedule', exact: true }).click();
  await expect(dialog.getByText('Schedule preview is current.', { exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Save schedule', exact: true }).click();
  await expect(dialog.getByText('Schedule saved.', { exact: true })).toBeVisible();
  expect(state.saves).toEqual([{ Revision: revision, ScheduleTimezone: 'Asia/Shanghai', Triggers: [
    { Kind: 'system_event', MaxRuntimeTicks: '10000001', SystemEvent: 'ConfigurationChanged' },
    { Kind: 'interval', MaxRuntimeTicks: null, IntervalTicks: '37000000001' },
    { Kind: 'daily', MaxRuntimeTicks: null, TimeOfDayTicks: '108001234567' },
    { Kind: 'startup', MaxRuntimeTicks: '90000000001' },
    { Kind: 'weekly', MaxRuntimeTicks: null, TimeOfDayTicks: '147061234567', DayOfWeek: 1 },
  ] }]);
  expect(state.tasks[0].Revision).toBe('9007199254740994');
});

test('pending new rules and edited rules invalidate the preview without saving automatically', async ({ page, api }) => {
  const state = installTasks(api, [task('backup-task', 'backup.create', 'Create automatic backup', 'Backup', [trigger()])]);
  const dialog = await openRules(page);
  await dialog.getByRole('button', { name: 'Remove trigger 1', exact: true }).click();
  await expect(dialog).toContainText('No triggers · manual only');
  await dialog.getByRole('button', { name: 'Preview schedule', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Save schedule', exact: true })).toBeEnabled();
  const composer = dialog.getByRole('region', { name: 'Add a rule', exact: true });
  await composer.getByLabel('Time of day', { exact: true }).fill('04:05:06.1234567');
  await expect(dialog.getByRole('button', { name: 'Preview schedule', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Save schedule', exact: true })).toBeDisabled();
  await expect(dialog).toContainText('Add the new rule or reset it before previewing and saving.');
  await composer.getByRole('button', { name: 'Add trigger', exact: true }).click();
  await dialog.getByRole('button', { name: 'Preview schedule', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Save schedule', exact: true })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Edit trigger 1', exact: true }).click();
  await dialog.getByRole('region', { name: 'Trigger 1', exact: true }).getByLabel('Time of day', { exact: true }).fill('05:00');
  await expect(dialog.getByRole('button', { name: 'Save schedule', exact: true })).toBeDisabled();
  expect(state.previews).toHaveLength(2);
  expect(state.previews[0].Triggers).toEqual([]);
  expect(state.previews[1].Triggers).toEqual([{ Kind: 'daily', MaxRuntimeTicks: null, TimeOfDayTicks: '147061234567' }]);
  expect(state.saves).toEqual([]);
});

test('backup credential validation displays structured errors and keeps the unsaved rules', async ({ page, api }) => {
  const state = installTasks(api);
  const message = 'Save an encryption passphrase in backup settings before adding automatic backup triggers.';
  api.handlers.set('PUT /admin/v1/tasks/backup-task/triggers', (route) => json(route, { Error: { Code: 'invalid_task_schedule', Message: 'Check the backup credentials.', Fields: { Triggers: message } }, RequestId: 'schedule-credential-error' }, 400));
  const dialog = await openRules(page);
  await dialog.getByRole('button', { name: 'Add trigger', exact: true }).click();
  await dialog.getByRole('button', { name: 'Preview schedule', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Save schedule', exact: true })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Save schedule', exact: true }).click();
  const fields = dialog.getByRole('group', { name: 'Schedule field errors', exact: true });
  await expect(fields).toContainText('Trigger rules');
  await expect(fields).toContainText(message);
  await expect(fields).toBeFocused();
  await expect(fields.getByRole('link', { name: 'Open backup settings', exact: true })).toHaveAttribute('target', '_blank');
  await expect(dialog.getByRole('button', { name: 'Edit trigger 1', exact: true })).toBeVisible();
  await expect(dialog.getByText('Schedule saved.', { exact: true })).toHaveCount(0);
  expect(state.tasks[0].Triggers).toEqual([]);
  expect(api.writes().filter((value) => value.method === 'PUT')).toHaveLength(1);
});

test('an unknown save outcome requires an explicit reload before another schedule write', async ({ page, api }) => {
  const state = installTasks(api);
  api.handlers.set('PUT /admin/v1/tasks/backup-task/triggers', (route, request) => {
    const input = request.postDataJSON() as TaskScheduleUpdate;
    state.saves.push(input);
    state.applySchedule(state.tasks[0], input);
    return json(route, { Error: { Code: 'database_unavailable', Message: 'The save response could not be confirmed.' } }, 503);
  });
  const dialog = await openRules(page);
  await dialog.getByRole('button', { name: 'Add trigger', exact: true }).click();
  await dialog.getByRole('button', { name: 'Preview schedule', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Save schedule', exact: true })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Save schedule', exact: true }).click();
  await expect(dialog).toContainText('The save result could not be confirmed.');
  await expect(dialog.getByRole('button', { name: 'Preview schedule', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Save schedule', exact: true })).toBeDisabled();
  await dialog.getByRole('button', { name: 'Reload latest schedule', exact: true }).click();
  const discard = page.getByRole('dialog', { name: 'Discard schedule changes?', exact: true });
  await expect(discard).toBeVisible();
  await discard.getByRole('button', { name: 'Discard draft and reload', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Edit trigger 1', exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Preview schedule', exact: true })).toBeEnabled();
  await expect(dialog.getByRole('button', { name: 'Save schedule', exact: true })).toBeDisabled();
  await expect(dialog.getByText('Schedule saved.', { exact: true })).toHaveCount(0);
  expect(state.saves).toHaveLength(1);
  expect(state.saves[0].Revision).toBe(revision);
});

test('recent history aggregates pages, filters the 24-hour window and overlays live records', async ({ page, api }) => {
  const scan = task('scan-task', 'library.scan', 'Scan media libraries', 'Library');
  const recent = Array.from({ length: 51 }, (_value, index) => runMinutesAgo(scan, 'scan-history-' + index, index + 1));
  scan.LastRun = { ...recent[0], Scanned: 9_999 };
  const backup = task();
  const backupRuns = [runMinutesAgo(backup, 'backup-history-one', 2), runMinutesAgo(backup, 'backup-history-two', 3)];
  backup.LastRun = backupRuns[0];
  const analysis = task('long-task', 'analysis.long', 'Long running analysis', 'Media analysis');
  analysis.CurrentRun = runMinutesAgo(analysis, 'long-active-run', 2 * 24 * 60, 'running');
  const state = installTasks(api, [scan, backup, analysis]);
  state.histories.set(scan.Id, [runMinutesAgo(scan, 'future-run', -1), ...recent, runMinutesAgo(scan, 'old-completed-run', 24 * 60 + 1)]);
  state.histories.set(backup.Id, backupRuns);
  await page.goto(tasksPath);
  const history = page.getByRole('table', { name: 'Recent task runs', exact: true });
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(page.getByText('Last 24 hours', { exact: true })).toBeVisible();
  await expect(history.getByRole('row')).toHaveCount(55);
  await expect(history.locator('[data-run-id="scan-history-0"]')).toHaveCount(1);
  await expect(history.locator('[data-run-id="scan-history-0"]')).toContainText('Scanned 9,999');
  await expect(history.locator('[data-run-id="scan-history-50"]')).toBeVisible();
  await expect(history.locator('[data-run-id="backup-history-two"]')).toBeVisible();
  await expect(history.locator('[data-run-id="long-active-run"]')).toContainText('Running');
  await expect(history.locator('[data-run-id="old-completed-run"]')).toHaveCount(0);
  await expect(history.locator('[data-run-id="future-run"]')).toHaveCount(0);
  expect(state.historyReads.filter((value) => value.taskId === scan.Id).map((value) => value.start)).toEqual([0, 50]);
  expect(state.historyReads.every((value) => value.limit === 50)).toBe(true);
  await expect(page.getByText('Recent history is incomplete.', { exact: true })).toHaveCount(0);
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(history).toHaveAttribute('data-initiators-loading', 'false');
  await evidence(page, test.info(), 'design2-tasks-history-24h');

  state.tasks[2].CurrentRun = { ...state.tasks[2].CurrentRun!, Scanned: 9_876 };
  await page.clock.fastForward(5_001);
  await expect(page.getByRole('region', { name: analysis.Name, exact: true }).getByText('9,876', { exact: true })).toBeVisible();
  await expect(history).toHaveAttribute('aria-busy', 'false');
  expect(state.historyReads).toHaveLength(4);

  const newLast = runMinutesAgo(scan, 'scan-new-last-run', 0.25);
  const newlyDiscovered = runMinutesAgo(scan, 'scan-new-history-run', 0.5);
  state.tasks[0].LastRun = newLast;
  state.histories.set(scan.Id, [newLast, newlyDiscovered, ...state.histories.get(scan.Id)!]);
  await page.clock.fastForward(5_001);
  await expect(history.locator('[data-run-id="scan-new-history-run"]')).toBeVisible();
  await expect(history).toHaveAttribute('aria-busy', 'false');
  expect(state.historyReads.filter((value) => value.taskId === scan.Id).map((value) => value.start)).toEqual([0, 50, 0, 50]);
  expect(state.historyReads.filter((value) => value.taskId === backup.Id)).toHaveLength(1);
  expect(state.historyReads.filter((value) => value.taskId === analysis.Id)).toHaveLength(1);
  expect(api.writes()).toEqual([]);
});

test('run identifiers are deduplicated across tasks and conflicting history is disclosed', async ({ page, api }) => {
  const scan = task('scan-task', 'library.scan', 'Scan media libraries', 'Library');
  const backup = task();
  scan.CurrentRun = { ...runMinutesAgo(scan, 'shared-run-id', 1, 'running'), Scanned: 8_888 };
  const state = installTasks(api, [scan, backup]);
  state.histories.set(scan.Id, [runMinutesAgo(scan, 'shared-run-id', 1), runMinutesAgo(scan, 'scan-unique', 2)]);
  state.histories.set(backup.Id, [runMinutesAgo(backup, 'shared-run-id', 1), runMinutesAgo(backup, 'backup-unique', 2)]);
  await page.goto(tasksPath);
  const history = page.getByRole('table', { name: 'Recent task runs', exact: true });
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(history.getByRole('row')).toHaveCount(4);
  await expect(history.locator('[data-run-id="shared-run-id"]')).toHaveCount(1);
  await expect(history.locator('[data-run-id="shared-run-id"]')).toContainText('Running');
  await expect(history.locator('[data-run-id="shared-run-id"]')).toContainText('Scanned 8,888');
  await expect(page.getByText('Recent history is incomplete.', { exact: true })).toBeVisible();
  await page.getByText('History loading details', { exact: true }).click();
  await expect(page.getByText('Run identifiers conflict across task responses. Each identifier is shown once; reload to confirm the records.', { exact: true })).toBeVisible();
  expect(api.writes()).toEqual([]);
});

test('history limits and failed tasks retain other records and disclose incomplete results', async ({ page, api }) => {
  const scan = task('scan-task', 'library.scan', 'Scan media libraries', 'Library');
  const backup = task();
  backup.LastRun = runMinutesAgo(backup, 'backup-confirmed-run', 3);
  const state = installTasks(api, [scan, backup]);
  state.histories.set(scan.Id, Array.from({ length: 201 }, (_value, index) => runMinutesAgo(scan, 'bounded-run-' + index, index + 1)));
  state.historyFailures.set(backup.Id, 'Backup history is unavailable.');
  await page.goto(tasksPath);
  const history = page.getByRole('table', { name: 'Recent task runs', exact: true });
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(page.getByText('Recent history is incomplete.', { exact: true })).toBeVisible();
  await page.getByText('History loading details', { exact: true }).click();
  await expect(page.getByText('Only the newest 200 records per task were checked. More runs may fall within the last 24 hours.', { exact: true })).toBeVisible();
  await expect(page.getByText('Backup history is unavailable.', { exact: true })).toBeVisible();
  await expect(history.locator('[data-run-id="backup-confirmed-run"]')).toBeVisible();
  await expect(page.getByText('Showing 100 of 201 loaded runs', { exact: true })).toBeVisible();
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(history).toHaveAttribute('data-initiators-loading', 'false');
  await evidence(page, test.info(), 'design2-tasks-history-partial-details');
  await page.getByRole('button', { name: 'Show more runs', exact: true }).click();
  await page.getByRole('button', { name: 'Show more runs', exact: true }).click();
  await expect(history.getByRole('row')).toHaveCount(202);
  await expect(history.locator('[data-run-id="bounded-run-199"]')).toBeVisible();
  await expect(history.locator('[data-run-id="bounded-run-200"]')).toHaveCount(0);
  expect(state.historyReads.filter((value) => value.taskId === scan.Id).map((value) => value.start)).toEqual([0, 50, 100, 150]);

  state.historyFailures.delete(backup.Id);
  await page.getByRole('button', { name: 'Reload recent history', exact: true }).click();
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(page.getByText('Backup history is unavailable.', { exact: true })).toHaveCount(0);
  await expect(page.getByText('Recent history is incomplete.', { exact: true })).toBeVisible();
  expect(state.historyReads.filter((value) => value.taskId === scan.Id)).toHaveLength(8);
  expect(state.historyReads.filter((value) => value.taskId === backup.Id)).toHaveLength(2);
  expect(api.writes()).toEqual([]);
});

test('aggregate history admits at most two concurrent requests', async ({ page, api }) => {
  const definitions = Array.from({ length: 4 }, (_value, index) => task('task-' + index, 'analysis.' + index, 'Analysis task ' + index, 'Media analysis'));
  installTasks(api, definitions);
  const releases: (() => void)[] = [];
  let releaseAll = false;
  let active = 0;
  let peak = 0;
  for (const definition of definitions) api.handlers.set('GET /admin/v1/tasks/' + definition.Id + '/runs', async (route, request) => {
    active += 1; peak = Math.max(peak, active);
    try {
      await new Promise<void>((resolve) => { if (releaseAll) resolve(); else releases.push(resolve); });
      const query = new URL(request.url()).searchParams;
      await json(route, { Items: [], TotalRecordCount: 0, StartIndex: Number(query.get('StartIndex') ?? 0), Limit: Number(query.get('Limit') ?? 50) });
    } finally { active -= 1; }
  });
  try {
    await page.goto(tasksPath);
    await expect.poll(() => releases.length).toBe(2);
    expect(active).toBe(2);
    releases[0]();
    await expect.poll(() => releases.length).toBe(3);
    releases[1]();
    await expect.poll(() => releases.length).toBe(4);
    releases[2](); releases[3]();
    await expect(page.getByRole('table', { name: 'Recent task runs', exact: true })).toHaveAttribute('aria-busy', 'false');
    expect(peak).toBe(2);
    expect(active).toBe(0);
    expect(api.writes()).toEqual([]);
  } finally { releaseAll = true; for (const release of releases) release(); }
});

test('a failed history refresh cannot keep a stale run marked as running', async ({ page, api }) => {
  const definition = task('scan-task', 'library.scan', 'Scan media libraries', 'Library');
  definition.CurrentRun = runMinutesAgo(definition, 'formerly-active-run', 1, 'running');
  definition.LastRun = runMinutesAgo(definition, 'earlier-completed-run', 2);
  const state = installTasks(api, [definition]);
  await page.goto(tasksPath);
  const history = page.getByRole('table', { name: 'Recent task runs', exact: true });
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(history.locator('[data-run-id="formerly-active-run"]')).toContainText('Running');
  const completed = { ...runMinutesAgo(definition, 'formerly-active-run', 1), FinishedAt: '2026-11-07T08:00:01Z' };
  const latest = { ...runMinutesAgo(definition, 'newest-completed-run', 0), CreatedAt: '2026-11-07T08:00:02Z', StartedAt: '2026-11-07T08:00:02Z', FinishedAt: '2026-11-07T08:00:03Z' };
  state.tasks[0].CurrentRun = null;
  state.tasks[0].LastRun = latest;
  state.histories.set(definition.Id, [latest, completed, definition.LastRun!]);
  state.historyFailures.set(definition.Id, 'History refresh failed.');
  await page.clock.fastForward(5_001);
  await expect(history.locator('[data-run-id="newest-completed-run"]')).toContainText('Completed');
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(history.locator('[data-run-id="formerly-active-run"]')).toHaveCount(0);
  await expect(page.getByText('Recent history is incomplete.', { exact: true })).toBeVisible();
  await page.getByText('History loading details', { exact: true }).click();
  await expect(page.getByText('A recorded active run is no longer confirmed by current task status. Reload to check its final state.', { exact: true })).toBeVisible();
  state.historyFailures.delete(definition.Id);
  await page.getByRole('button', { name: 'Reload recent history', exact: true }).click();
  await expect(history.locator('[data-run-id="formerly-active-run"]')).toContainText('Completed');
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(page.getByText('Recent history is incomplete.', { exact: true })).toHaveCount(0);
  expect(api.writes()).toEqual([]);
});

test('initiators join exact run identifiers and preserve user, system and API key origins', async ({ page, api }) => {
  const definition = task('scan-task', 'library.scan', 'Scan media libraries', 'Library');
  const userRun = runMinutesAgo(definition, 'user-run', 1);
  const systemRun = { ...runMinutesAgo(definition, 'system-run', 2), Source: 'schedule' };
  const keyRun = runMinutesAgo(definition, 'key-run', 3);
  const namelessRun = runMinutesAgo(definition, 'nameless-user-run', 4);
  definition.LastRun = userRun;
  const state = installTasks(api, [definition]);
  state.histories.set(definition.Id, [userRun, systemRun, keyRun, namelessRun]);
  const activities = installAdmissions(api, [
    admission(userRun, '1', { Kind: 'user', Id: 'operator-id', Name: 'Current operator' }, { Date: keyRun.CreatedAt }),
    admission(systemRun, '2', { Kind: 'system', Id: null, Name: null }, { Date: userRun.CreatedAt }),
    admission(keyRun, '3', { Kind: 'application_key', Id: '9007199254740993', Name: null }, { Source: 'emby' }),
    admission(namelessRun, '4', { Kind: 'user', Id: 'retained-user-id', Name: null }),
  ]);
  await page.goto(tasksPath);
  await expect(page.getByText('0 running · 0 queued · 1 task', { exact: true })).toBeVisible();
  const history = page.getByRole('table', { name: 'Recent task runs', exact: true });
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(history).toHaveAttribute('data-initiators-loading', 'false');
  const user = history.locator('[data-run-id="user-run"]');
  await expect(user.locator('.system-task-initiator')).toHaveText('Current operator');
  await expect(user.locator('.system-task-run-source')).toHaveText('Manual');
  await expect(history.locator('[data-run-id="system-run"] .system-task-initiator')).toHaveText('System');
  await expect(history.locator('[data-run-id="system-run"] .system-task-run-source')).toHaveText('Scheduled');
  await expect(history.locator('[data-run-id="key-run"] .system-task-initiator')).toHaveText('API key #9007199254740993');
  await expect(history.locator('[data-run-id="key-run"] .system-task-run-source')).toHaveText('Manual');
  await expect(history.locator('[data-run-id="nameless-user-run"] .system-task-initiator')).toHaveText('User name unavailable');
  expect(activities.reads).toHaveLength(1);
  expect(activities.reads[0]).toMatchObject({ start: 0, limit: 200, action: 'task.admitted' });
  expect(activities.reads[0].minDate).toMatch(/^2026-11-06T08:/);
  await evidence(page, test.info(), 'design2-tasks-initiators-known');

  activities.entries[0].Actor.Name = 'Renamed operator';
  await page.getByRole('button', { name: 'Refresh tasks', exact: true }).click();
  await expect(user.locator('.system-task-initiator')).toHaveText('Renamed operator');
  expect(activities.reads).toHaveLength(2);
  state.tasks[0].LastRun = { ...state.tasks[0].LastRun!, Scanned: 7_777 };
  await page.clock.fastForward(5_001);
  await expect(user).toContainText('Scanned 7,777');
  expect(activities.reads).toHaveLength(2);
  expect(api.writes()).toEqual([]);
});

test('current user names remain literal while system labels are localized', async ({ page, api }) => {
  await page.addInitScript(() => localStorage.setItem('goby.admin.language', 'zh'));
  const definition = task('scan-task', 'library.scan', 'Scan media libraries', 'Library');
  const userRun = runMinutesAgo(definition, 'literal-user-name', 1);
  const systemRun = { ...runMinutesAgo(definition, 'localized-system', 2), Source: 'schedule' };
  const state = installTasks(api, [definition, task('localized-other-task', 'analysis.long', 'Long running analysis', 'Media analysis')]);
  state.histories.set(definition.Id, [userRun, systemRun]);
  installAdmissions(api, [
    admission(userRun, '1', { Kind: 'user', Id: 'literal-name-user', Name: 'System' }),
    admission(systemRun, '2', { Kind: 'system', Id: null, Name: null }),
  ]);
  await page.goto(tasksPath);
  const history = page.locator('.system-run-table');
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(history).toHaveAttribute('data-initiators-loading', 'false');
  await expect(page.getByText('0 个运行中 · 0 个排队 · 2 个任务', { exact: true })).toBeVisible();
  const user = history.locator('[data-run-id="literal-user-name"] .system-task-initiator');
  await expect(user).toHaveText('System');
  await expect(user).toHaveAttribute('translate', 'no');
  await expect(history.locator('[data-run-id="localized-system"] .system-task-initiator')).toHaveText('系统');
  await expect(history.locator('[data-run-id="literal-user-name"] .system-task-run-source')).toHaveText('手动');
  expect(api.writes()).toEqual([]);
});

test('expired or unrelated activity leaves an initiator unknown without inferring it from a schedule', async ({ page, api }) => {
  const definition = task('analysis-task', 'analysis.long', 'Long running analysis', 'Media analysis');
  const current = { ...runMinutesAgo(definition, 'older-active-run', 2 * 24 * 60, 'running'), Source: 'schedule' };
  definition.CurrentRun = current;
  installTasks(api, [definition]);
  const activities = installAdmissions(api, [
    admission(current, '1', { Kind: 'system', Id: null, Name: null }),
    admission(current, '2', { Kind: 'user', Id: 'unrelated-user', Name: 'Unrelated operator' }, { Date: timestamp, Resource: { Kind: 'task_run', Id: definition.Id } }),
  ]);
  await page.goto(tasksPath);
  await expect(page.getByText('1 running · 0 queued · 1 task', { exact: true })).toBeVisible();
  const history = page.getByRole('table', { name: 'Recent task runs', exact: true });
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(history).toHaveAttribute('data-initiators-loading', 'false');
  const row = history.locator('[data-run-id="older-active-run"]');
  await expect(row.locator('.system-task-initiator')).toHaveText('Initiator unknown');
  await expect(row.locator('.system-task-initiator')).toHaveAttribute('data-actor-state', 'unknown');
  await expect(row.locator('.system-task-run-source')).toHaveText('Scheduled');
  await expect(row).not.toContainText('Unrelated operator');
  await expect(page.getByText('Some run initiators could not be confirmed.', { exact: true })).toHaveCount(0);
  expect(activities.reads).toHaveLength(1);
  await evidence(page, test.info(), 'design2-tasks-initiators-unknown');
  expect(api.writes()).toEqual([]);
});

test('a failed admission page keeps only exact confirmed actors and can reload current names', async ({ page, api }) => {
  const definition = task('scan-task', 'library.scan', 'Scan media libraries', 'Library');
  const confirmed = runMinutesAgo(definition, 'confirmed-actor-run', 1);
  const unconfirmed = { ...runMinutesAgo(definition, 'unconfirmed-actor-run', 2), Source: 'schedule' };
  definition.LastRun = confirmed;
  const state = installTasks(api, [definition]);
  state.histories.set(definition.Id, [confirmed, unconfirmed]);
  const activities = installAdmissions(api, [
    admission(confirmed, '1', { Kind: 'user', Id: 'alice-id', Name: 'Alice' }),
    ...Array.from({ length: 199 }, (_value, index) => admission(runMinutesAgo(definition, 'unrelated-' + index, index + 2), String(index + 2), { Kind: 'system', Id: null, Name: null })),
    admission(unconfirmed, '201', { Kind: 'user', Id: 'bob-id', Name: 'Bob' }, { Date: runMinutesAgo(definition, 'time-only', 201).CreatedAt }),
  ]);
  activities.failures.set(200, 'The next admission page is unavailable.');
  await page.goto(tasksPath);
  const history = page.getByRole('table', { name: 'Recent task runs', exact: true });
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(history).toHaveAttribute('data-initiators-loading', 'false');
  await expect(history.locator('[data-run-id="confirmed-actor-run"] .system-task-initiator')).toHaveText('Alice');
  await expect(history.locator('[data-run-id="unconfirmed-actor-run"] .system-task-initiator')).toHaveText('Initiator unknown');
  await expect(history.locator('[data-run-id="unconfirmed-actor-run"] .system-task-run-source')).toHaveText('Scheduled');
  await expect(page.getByText('Some run initiators could not be confirmed.', { exact: true })).toBeVisible();
  await page.getByText('Initiator lookup details', { exact: true }).click();
  await expect(page.getByText('The next admission page is unavailable.', { exact: true })).toBeVisible();
  expect(activities.reads.map((value) => value.start)).toEqual([0, 200]);
  activities.failures.clear(); activities.entries[0].Actor.Name = 'Alice current';
  await page.getByRole('button', { name: 'Reload initiators', exact: true }).click();
  await expect(history.locator('[data-run-id="confirmed-actor-run"] .system-task-initiator')).toHaveText('Alice current');
  await expect(history.locator('[data-run-id="unconfirmed-actor-run"] .system-task-initiator')).toHaveText('Bob');
  await expect(page.getByText('Some run initiators could not be confirmed.', { exact: true })).toHaveCount(0);
  expect(activities.reads.map((value) => value.start)).toEqual([0, 200, 0, 200]);
  expect(api.writes()).toEqual([]);
});

test('admission lookup has a fixed cap and leaves uncovered actors unknown', async ({ page, api }) => {
  const definition = task('scan-task', 'library.scan', 'Scan media libraries', 'Library');
  const known = runMinutesAgo(definition, 'bounded-known-actor', 1);
  const uncovered = runMinutesAgo(definition, 'bounded-unknown-actor', 2);
  const state = installTasks(api, [definition]);
  state.histories.set(definition.Id, [known, uncovered]);
  const activities = installAdmissions(api, [
    admission(known, '1', { Kind: 'system', Id: null, Name: null }),
    ...Array.from({ length: 999 }, (_value, index) => admission(runMinutesAgo(definition, 'padding-' + index, index + 2), String(index + 2), { Kind: 'system', Id: null, Name: null })),
    admission(uncovered, '1001', { Kind: 'user', Id: 'outside-cap-user', Name: 'Outside cap operator' }, { Date: runMinutesAgo(definition, 'late-time', 1001).CreatedAt }),
  ]);
  await page.goto(tasksPath);
  const history = page.getByRole('table', { name: 'Recent task runs', exact: true });
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(history).toHaveAttribute('data-initiators-loading', 'false');
  await expect(history.locator('[data-run-id="bounded-known-actor"] .system-task-initiator')).toHaveText('System');
  await expect(history.locator('[data-run-id="bounded-unknown-actor"] .system-task-initiator')).toHaveText('Initiator unknown');
  await page.getByText('Initiator lookup details', { exact: true }).click();
  await expect(page.getByText('Only the newest 1000 admission activities were checked. Other initiators remain unknown.', { exact: true })).toBeVisible();
  expect(activities.reads.map((value) => value.start)).toEqual([0, 200, 400, 600, 800]);
  expect(api.writes()).toEqual([]);
});

test('conflicting actors and wrong activity scopes cannot be assigned to a run', async ({ page, api }) => {
  const definition = task('scan-task', 'library.scan', 'Scan media libraries', 'Library');
  const conflicted = runMinutesAgo(definition, 'conflicting-actor-run', 1);
  const wrongScope = runMinutesAgo(definition, 'wrong-scope-run', 2);
  const state = installTasks(api, [definition]);
  state.histories.set(definition.Id, [conflicted, wrongScope]);
  installAdmissions(api, [
    admission(conflicted, '1', { Kind: 'user', Id: 'first-user', Name: 'First actor' }),
    admission(conflicted, '2', { Kind: 'user', Id: 'second-user', Name: 'Second actor' }),
    admission(wrongScope, '3', { Kind: 'user', Id: 'wrong-action-user', Name: 'Wrong action actor' }, { Action: 'task.finished' }),
    admission(wrongScope, '4', { Kind: 'user', Id: 'wrong-resource-user', Name: 'Wrong resource actor' }, { Resource: { Kind: 'task', Id: wrongScope.Id } }),
  ], false);
  await page.goto(tasksPath);
  const history = page.getByRole('table', { name: 'Recent task runs', exact: true });
  await expect(history).toHaveAttribute('aria-busy', 'false');
  await expect(history).toHaveAttribute('data-initiators-loading', 'false');
  await expect(history.locator('[data-run-id="conflicting-actor-run"] .system-task-initiator')).toHaveText('Initiator unknown');
  await expect(history.locator('[data-run-id="wrong-scope-run"] .system-task-initiator')).toHaveText('Initiator unknown');
  await expect(history).not.toContainText('First actor');
  await expect(history).not.toContainText('Second actor');
  await expect(history).not.toContainText('Wrong action actor');
  await expect(history).not.toContainText('Wrong resource actor');
  await page.getByText('Initiator lookup details', { exact: true }).click();
  await expect(page.getByText('Conflicting admission actors were returned for a run. Its initiator remains unknown.', { exact: true })).toBeVisible();
  await expect(page.getByText('Some activity records did not match the admission query and were not associated with a run.', { exact: true })).toBeVisible();
  expect(api.writes()).toEqual([]);
});

test('leaving tasks cancels a pending initiator lookup', async ({ page, api }) => {
  const definition = task('scan-task', 'library.scan', 'Scan media libraries', 'Library');
  definition.LastRun = runMinutesAgo(definition, 'pending-actor-run', 1);
  installTasks(api, [definition]);
  let started = 0;
  let aborted = 0;
  const delayed: { release?: () => void } = {};
  page.on('requestfailed', (request) => { const url = new URL(request.url()); if (url.pathname === '/admin/v1/activity' && url.searchParams.get('Action') === 'task.admitted') aborted += 1; });
  api.handlers.set('GET /admin/v1/activity', async (route, request) => {
    const query = new URL(request.url()).searchParams;
    if (query.get('Action') === 'task.admitted') {
      started += 1;
      await new Promise<void>((resolve) => { delayed.release = resolve; });
    }
    await json(route, { Items: [], TotalRecordCount: 0, StartIndex: Number(query.get('StartIndex') ?? 0), Limit: Number(query.get('Limit') ?? 50), RetentionDays: 30 }).catch(() => undefined);
  });
  try {
    await page.goto(tasksPath);
    await expect.poll(() => started).toBe(1);
    await page.locator('nav[aria-label="Administration"] a[href="/admin/media/libraries"]').click();
    await expect(page.getByRole('main').getByRole('heading', { name: 'Libraries', exact: true })).toBeVisible();
    await expect.poll(() => aborted).toBe(1);
    expect(api.writes()).toEqual([]);
  } finally { delayed.release?.(); }
});
