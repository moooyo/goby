import { createHash } from 'node:crypto';
import { constants } from 'node:fs';
import { lstat, open, realpath } from 'node:fs/promises';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import type { APIRequestContext, Locator, Page, Response, Route } from '@playwright/test';
import type { Job, LoginSession, TaskAdmissionResponse, TaskDefinition, TaskDefinitionResponse, TaskRunDetail, TaskRunsResponse, TaskScheduleInput, TaskSchedulePreview, TaskTrigger, TaskTriggerInput } from '../src/api';

interface FixtureLibrary { Id: string; Name: string; Path: string; FileCount: number }
interface TasksFixture {
  Marker: string;
  RunId: string;
  Origin: string;
  Administrator: { Id: string; Name: string };
  TaskId: string;
  TaskKey: string;
  Libraries: FixtureLibrary[];
  MediaSHA256: Record<string, string>;
  ResultPath: string;
}

interface PrivateResult {
  Marker: string;
  RunId: string;
  Complete: boolean;
  TaskId: string;
  Libraries: FixtureLibrary[];
  BrowserSessionId?: string;
  BrowserCookie?: string;
  BrowserCSRF?: string;
  BrowserSecrets: string[];
  ManualRunId?: string;
  ReceiptRequestId?: string;
  ReceiptRunId?: string;
  IntervalRunIds: string[];
  FinalRevision?: string;
  FinalTriggers?: TaskTrigger[];
  FinalTimezone?: string;
  Checks: Record<string, boolean>;
  Observations: { ActiveStopDialogObserved: boolean };
}

const fixtureMarker = 'goby-tasks-browser-fixtures-v1';
const fixturePattern = /^\/opt\/goby-test\/exec-scratch\/goby-tasks-([0-9]{8}_[0-9]{6}_[0-9a-f]{10})\/browser\/tasks-fixture\.json$/;
const taskPath = (id: string) => `/admin/v1/tasks/${encodeURIComponent(id)}`;
const runPath = (id: string) => `/admin/v1/task-runs/${encodeURIComponent(id)}`;
const scheduleDialog = (page: Page) => page.getByRole('dialog', { name: /^Edit schedule/ });
const runDialog = (page: Page) => page.getByRole('dialog', { name: /^(Run details|Run history)/ });
const triggerRegion = (page: Page, index: number) => scheduleDialog(page).getByRole('region', { name: `Trigger ${index + 1}`, exact: true });
const active = (state: string) => ['pending', 'running', 'stopping'].includes(state);
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

async function loadFixture(filename: string, baseURL: string): Promise<TasksFixture> {
  const match = fixturePattern.exec(filename);
  if (process.platform !== 'linux' || process.getuid?.() !== 0 || !match
    || filename !== path.resolve(filename) || await realpath(filename) !== filename) {
    throw new Error('Scheduled-task fixtures require the isolated Linux runner and its canonical private manifest.');
  }
  for (const directory of [path.dirname(filename), path.dirname(path.dirname(filename))]) {
    const status = await lstat(directory);
    if (!status.isDirectory() || status.isSymbolicLink() || status.uid !== 0 || (status.mode & 0o777) !== 0o700) {
      throw new Error('The task fixture directories must be private and root owned.');
    }
  }
  const file = await open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  let fixture: TasksFixture;
  try {
    const status = await file.stat();
    if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o600 || status.nlink !== 1
      || status.size > 64 * 1024 || await realpath(`/proc/self/fd/${file.fd}`) !== filename) {
      throw new Error('The task fixture must be a bounded private regular file.');
    }
    fixture = JSON.parse(await file.readFile('utf8')) as TasksFixture;
  } finally { await file.close(); }
  const origin = new URL(baseURL);
  const root = `/dev/shm/goby-tasks-${match[1]}/media`;
  if (!fixture || fixture.Marker !== fixtureMarker || fixture.RunId !== match[1]
    || fixture.RunId !== process.env.GOBY_TASKS_RUN_ID || fixture.Origin !== origin.origin
    || origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || !origin.port
    || ['5432', '15432', '18096', '18097'].includes(origin.port)
    || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash
    || !fixture.Administrator || !/^[0-9a-f]{32}$/.test(fixture.Administrator.Id)
    || typeof fixture.Administrator.Name !== 'string' || !fixture.Administrator.Name.includes(fixture.RunId)
    || !/^[0-9a-f]{32}$/.test(fixture.TaskId) || fixture.TaskKey !== 'library.scan'
    || fixture.ResultPath !== path.join(path.dirname(filename), 'tasks-result.json')
    || !Array.isArray(fixture.Libraries) || fixture.Libraries.length !== 2
    || !fixture.Libraries.every((library) => /^[0-9a-f]{32}$/.test(library.Id) && typeof library.Name === 'string'
      && library.Name.includes(fixture.RunId) && [path.join(root, 'first'), path.join(root, 'second')].includes(library.Path) && library.FileCount === 1)
    || new Set(fixture.Libraries.map((library) => library.Id)).size !== 2
    || new Set(fixture.Libraries.map((library) => library.Path)).size !== 2
    || !fixture.MediaSHA256 || typeof fixture.MediaSHA256 !== 'object' || Array.isArray(fixture.MediaSHA256)
    || Object.keys(fixture.MediaSHA256).length !== 2) throw new Error('The task fixture does not match its isolated run contract.');
  await verifyMedia(fixture);
  return fixture;
}

async function verifyMedia(fixture: TasksFixture) {
  for (const [filename, expected] of Object.entries(fixture.MediaSHA256)) {
    if (!fixture.Libraries.some((library) => path.dirname(filename) === library.Path)
      || filename !== path.resolve(filename) || path.extname(filename) !== '.mp4' || !/^[0-9a-f]{64}$/.test(expected)
      || await realpath(filename) !== filename) throw new Error('The task media manifest contains an unowned file.');
    const file = await open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const status = await file.stat();
      if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o444 || status.nlink !== 1
        || status.size > 128 * 1024 || status.size === 0 || await realpath(`/proc/self/fd/${file.fd}`) !== filename) {
        throw new Error('The task media must be a bounded immutable fixture.');
      }
      if (createHash('sha256').update(await file.readFile()).digest('hex') !== expected) throw new Error('A task media fixture changed.');
    } finally { await file.close(); }
  }
}

async function saveResult(filename: string, value: PrivateResult, create = false) {
  const file = await open(filename, constants.O_WRONLY | constants.O_NOFOLLOW | (create ? constants.O_CREAT | constants.O_EXCL : 0), 0o600);
  try {
    const status = await file.stat();
    if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o600 || status.nlink !== 1
      || await realpath(`/proc/self/fd/${file.fd}`) !== filename) throw new Error('The task browser result must remain private.');
    await file.truncate(0);
    await file.writeFile(`${JSON.stringify(value)}\n`, 'utf8');
    await file.sync();
  } finally { await file.close(); }
}

function responseFor(page: Page, method: string, pathname: string, query: Record<string, string> = {}): Promise<Response> {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === method && url.pathname === pathname
      && Object.entries(query).every(([name, value]) => url.searchParams.get(name) === value);
  });
}

async function readTask(request: APIRequestContext, fixture: TasksFixture): Promise<TaskDefinition> {
  const response = await request.get(taskPath(fixture.TaskId));
  expect(response.status()).toBe(200);
  const result = await response.json() as TaskDefinitionResponse;
  expect(result.Task.Id).toBe(fixture.TaskId);
  expect(result.Task.Key).toBe(fixture.TaskKey);
  return result.Task;
}

async function readRuns(request: APIRequestContext, fixture: TasksFixture): Promise<TaskRunsResponse> {
  const response = await request.get(`${taskPath(fixture.TaskId)}/runs?StartIndex=0&Limit=200`);
  expect(response.status()).toBe(200);
  const result = await response.json() as TaskRunsResponse;
  expect(result.Items.length).toBe(result.TotalRecordCount);
  return result;
}

async function readRun(request: APIRequestContext, id: string): Promise<TaskRunDetail> {
  const response = await request.get(`${runPath(id)}?StartIndex=0&Limit=200`);
  expect(response.status()).toBe(200);
  const result = await response.json() as TaskRunDetail;
  expect(result.Run.Id).toBe(id);
  return result;
}

async function completeRun(request: APIRequestContext, fixture: TasksFixture, id: string): Promise<TaskRunDetail> {
  await expect.poll(async () => active((await readRun(request, id)).Run.State), {
    timeout: 90_000, intervals: [100, 250, 500, 1000], message: 'The real fixture task must reach a terminal state.',
  }).toBe(false);
  const observed = await readRun(request, id);
  expect(observed.Run.State).toBe('completed');
  expect(observed.Run.TaskId).toBe(fixture.TaskId);
  expect(observed.Run.TotalChildren).toBe(2);
  expect(observed.Run.TerminalChildren).toBe(2);
  expect(observed.Run.CompletedChildren).toBe(2);
  expect(observed.Run.FailedChildren + observed.Run.CancelledChildren + observed.Run.InterruptedChildren + observed.Run.UnavailableChildren).toBe(0);
  expect(observed.Children.TotalRecordCount).toBe(2);
  expect(observed.Children.Items.map((child) => child.LibraryId).sort()).toEqual(fixture.Libraries.map((library) => library.Id).sort());
  for (const child of observed.Children.Items) {
    expect(child.State).toBe('completed');
    expect(child.ScanJobId).toMatch(/^[0-9a-f]{32}$/);
    expect(child.Scanned).toBeGreaterThanOrEqual(1);
    expect(child.ErrorCode).toBe('');
    expect(child.ErrorMessage).toBe('');
  }
  for (const key of ['Scanned', 'Added', 'Updated'] as const) expect(observed.Run[key]).toBe(observed.Children.Items.reduce((sum, child) => sum + child[key], 0));
  return observed;
}

async function waitForIdle(request: APIRequestContext, fixture: TasksFixture) {
  await expect.poll(async () => (await readRuns(request, fixture)).Items.some((run) => active(run.State)), {
    timeout: 90_000, intervals: [100, 250, 500, 1000], message: 'All owned task runs must settle before continuing.',
  }).toBe(false);
  expect((await readTask(request, fixture)).CurrentRun).toBeNull();
}

function taskCard(page: Page, name: string) {
  return page.getByRole('list', { name: 'Available server tasks', exact: true }).getByRole('listitem')
    .filter({ has: page.getByRole('heading', { name, exact: true }) });
}

async function refreshTasks(page: Page) {
  await page.getByRole('button', { name: 'Refresh tasks', exact: true }).click();
  await expect(page.getByRole('status', { name: 'Loading available tasks', exact: true })).not.toBeVisible();
}

async function openSchedule(page: Page, task: TaskDefinition): Promise<TaskDefinition> {
  const response = responseFor(page, 'GET', taskPath(task.Id));
  await taskCard(page, task.Name).getByRole('button', { name: 'Edit schedule', exact: true }).click();
  const loaded = await response;
  expect(loaded.status()).toBe(200);
  const latest = (await loaded.json() as TaskDefinitionResponse).Task;
  await expect(scheduleDialog(page).getByRole('combobox', { name: 'Schedule time zone', exact: true })).toHaveValue(latest.ScheduleTimezone);
  return latest;
}

async function choose(page: Page, region: Locator, label: string, option: string) {
  await region.getByRole('combobox', { name: label, exact: true }).click();
  await page.getByRole('option', { name: option, exact: true }).click();
}

async function timezone(page: Page, value: string) {
  const input = scheduleDialog(page).getByRole('combobox', { name: 'Schedule time zone', exact: true });
  await input.fill(value);
  await input.press('Escape');
}

async function previewSchedule(page: Page, fixture: TasksFixture): Promise<{ result: TaskSchedulePreview; input: TaskScheduleInput }> {
  const response = responseFor(page, 'POST', `${taskPath(fixture.TaskId)}/triggers/preview`);
  await scheduleDialog(page).getByRole('button', { name: 'Preview schedule', exact: true }).click();
  const preview = await response;
  expect(preview.status()).toBe(200);
  const result = await preview.json() as TaskSchedulePreview;
  const input = preview.request().postDataJSON() as TaskScheduleInput;
  expect(Object.keys(input).sort()).toEqual(['ScheduleTimezone', 'Triggers']);
  expect(result.Items).toHaveLength(input.Triggers.length);
  await expect(scheduleDialog(page).getByRole('region', { name: 'Schedule preview', exact: true })).toBeVisible();
  return { result, input };
}

async function saveSchedule(page: Page, fixture: TasksFixture, previous: TaskDefinition): Promise<TaskDefinition> {
  const response = responseFor(page, 'PUT', `${taskPath(fixture.TaskId)}/triggers`);
  await scheduleDialog(page).getByRole('button', { name: 'Save schedule', exact: true }).click();
  const saved = await response;
  expect(saved.status()).toBe(200);
  const result = (await saved.json() as TaskDefinitionResponse).Task;
  expect(result.Id).toBe(previous.Id);
  expect(BigInt(result.Revision)).toBe(BigInt(previous.Revision) + 1n);
  expect((saved.request().postDataJSON() as { Revision: string }).Revision).toBe(previous.Revision);
  await expect(scheduleDialog(page).getByText('Schedule saved.', { exact: true })).toBeVisible();
  await expect(scheduleDialog(page).getByRole('button', { name: 'Save schedule', exact: true })).toBeDisabled();
  return result;
}

async function removeTriggers(page: Page) {
  const remove = scheduleDialog(page).getByRole('button', { name: /^Remove trigger [0-9]+$/ });
  while (await remove.count()) await remove.first().click();
}

function triggerInput(trigger: TaskTrigger): TaskTriggerInput {
  return { Kind: trigger.Kind, IntervalTicks: trigger.IntervalTicks, TimeOfDayTicks: trigger.TimeOfDayTicks,
    DayOfWeek: trigger.DayOfWeek, MaxRuntimeTicks: trigger.MaxRuntimeTicks };
}

async function reloadSchedule(page: Page, fixture: TasksFixture) {
  await scheduleDialog(page).getByRole('button', { name: 'Reload latest schedule', exact: true }).click();
  const discard = page.getByRole('dialog', { name: 'Discard schedule changes?', exact: true });
  await expect(discard).toBeVisible();
  const response = responseFor(page, 'GET', taskPath(fixture.TaskId));
  await discard.getByRole('button', { name: 'Discard draft and reload', exact: true }).click();
  const loaded = await response;
  expect(loaded.status()).toBe(200);
  const task = (await loaded.json() as TaskDefinitionResponse).Task;
  await expect(scheduleDialog(page).getByRole('combobox', { name: 'Schedule time zone', exact: true })).toHaveValue(task.ScheduleTimezone);
  await expect(scheduleDialog(page).getByRole('button', { name: 'Save schedule', exact: true })).toBeDisabled();
  return task;
}

async function loseCommittedResponse<T>(page: Page, pathname: string, method: 'POST' | 'PUT', expectedStatus: number, action: () => Promise<unknown>): Promise<{ result: T; body: unknown }> {
  const url = new URL(pathname, page.url()).href;
  let committed: { result: T; body: unknown } | undefined;
  let forwarded = 0;
  let failure = '';
  const intercept = async (route: Route) => {
    if (route.request().method() !== method || forwarded > 0) { await route.continue(); return; }
    forwarded += 1;
    const response = await route.fetch({ maxRedirects: 0 });
    if (response.status() !== expectedStatus) {
      failure = `The real mutation returned HTTP ${response.status()}.`;
      await route.fulfill({ response });
      return;
    }
    committed = { result: await response.json() as T, body: route.request().postDataJSON() };
    await route.abort('failed');
  };
  await page.route(url, intercept);
  try {
    await action();
    await expect.poll(() => Boolean(committed) || Boolean(failure), { timeout: 20_000, message: 'The native mutation must really commit before its response is lost.' }).toBe(true);
    if (failure || !committed) throw new Error(failure || 'The committed response was not observed.');
    expect(forwarded).toBe(1);
    return committed;
  } finally { await page.unroute(url, intercept); }
}

async function safeScreenshot(page: Page, filename: string, secrets: string[], fixture: TasksFixture) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  const visibleText = await page.locator('body').innerText();
  expect(secrets.some((secret) => visibleText.includes(secret)), 'Screenshots must exclude authentication secrets.').toBe(false);
  expect(fixture.Libraries.some((library) => visibleText.includes(library.Path)), 'Task screenshots must exclude media paths.').toBe(false);
  await page.screenshot({ path: filename, fullPage: true, animations: 'disabled' });
}

function futureCalendar(serverTime: string) {
  const date = new Date(Date.parse(serverTime) + 12 * 60 * 60 * 1000);
  const time = `${String(date.getUTCHours()).padStart(2, '0')}:${String(date.getUTCMinutes()).padStart(2, '0')}:${String(date.getUTCSeconds()).padStart(2, '0')}`;
  return { daily: `${time}.1234567`, weekly: `${time}.7654321`, day: (date.getUTCDay() + 3) % 7 };
}

// The isolated runner creates real libraries and media, and owns both restarts
// and complete credential/database cleanup. This journey never uses mock data.
// Fault injection drops a real committed response; subsequent assertions query
// the real server. Authentication traces and private fixture files are not exported.
test('isolated native scheduled tasks, durable admissions, schedule editing, and legacy scan history', async ({ page, context }, testInfo) => {
  test.skip(process.env.GOBY_SMOKE_TASKS_DISPOSABLE_DATABASE !== '1' || process.env.GOBY_SMOKE_TASKS_DEDICATED_ADMIN !== '1',
    'The task runner must confirm its disposable database and dedicated administrator.');
  test.setTimeout(360_000);
  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  const baseURL = process.env.GOBY_SMOKE_BASE_URL;
  const manifestPath = process.env.GOBY_SMOKE_TASKS_FIXTURE_MANIFEST;
  if (!name || !password || !baseURL || !manifestPath) throw new Error('The private task runner environment is incomplete.');
  const fixture = await loadFixture(manifestPath, baseURL);
  expect(new URL(testInfo.project.use.baseURL ?? '').origin).toBe(fixture.Origin);
  expect(name).toBe(fixture.Administrator.Name);
  const result: PrivateResult = { Marker: 'goby-tasks-browser-result-v1', RunId: fixture.RunId, Complete: false,
    TaskId: fixture.TaskId, Libraries: fixture.Libraries, BrowserSecrets: [], IntervalRunIds: [], Checks: {},
    Observations: { ActiveStopDialogObserved: false } };
  await saveResult(fixture.ResultPath, result, true);
  const sensitive = [password];
  const pageErrors: string[] = [];
  const scheduleWrites: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('request', (request) => { if (request.method() === 'PUT' && new URL(request.url()).pathname === `${taskPath(fixture.TaskId)}/triggers`) scheduleWrites.push(request.postData() ?? ''); });
  let nativeHeaders: Record<string, string> | undefined;
  let keepFutureRules = false;
  let task: TaskDefinition;
  try {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto('/admin/tasks');
    await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    await page.getByLabel(/^Username/).fill(name);
    await page.getByLabel(/^Password/).fill(password);
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(page.getByRole('tab', { name: 'Available tasks', exact: true })).toHaveAttribute('aria-selected', 'true');
    const sessionResponse = await context.request.get('/admin/v1/session');
    expect(sessionResponse.status()).toBe(200);
    const session = await sessionResponse.json() as { CSRFToken: string; User: { Id: string } };
    expect(session.User.Id).toBe(fixture.Administrator.Id);
    const cookie = (await context.cookies()).find((value) => value.name === 'goby_session');
    if (!cookie || typeof session.CSRFToken !== 'string' || !session.CSRFToken) throw new Error('The browser did not receive its independent administrator session.');
    result.BrowserCookie = `goby_session=${cookie.value}`;
    result.BrowserCSRF = session.CSRFToken;
    result.BrowserSecrets = [cookie.value, result.BrowserCookie, session.CSRFToken];
    sensitive.push(...result.BrowserSecrets);
    const sessionsResponse = await context.request.get('/admin/v1/sessions?Kind=admin&Status=active&Limit=200');
    expect(sessionsResponse.status()).toBe(200);
    const current = (await sessionsResponse.json() as { Items: LoginSession[] }).Items.filter((login) => login.IsCurrent);
    expect(current).toHaveLength(1);
    result.BrowserSessionId = current[0].Id;
    await saveResult(fixture.ResultPath, result);
    nativeHeaders = { Origin: fixture.Origin, 'X-CSRF-Token': session.CSRFToken };
    task = await readTask(context.request, fixture);
    expect(task.Triggers).toEqual([]);
    expect(task.CurrentRun).toBeNull();
    expect((await readRuns(context.request, fixture)).TotalRecordCount).toBe(0);
    await expect(taskCard(page, task.Name)).toBeVisible();

    await test.step('start a real task and verify its two library children and counters', async () => {
      const admissionResponse = responseFor(page, 'POST', `${taskPath(task.Id)}/runs`);
      await taskCard(page, task.Name).getByRole('button', { name: 'Start task', exact: true }).click();
      const response = await admissionResponse;
      expect(response.status()).toBe(202);
      const admission = await response.json() as TaskAdmissionResponse;
      expect(admission.Admitted).toBe(true);
      expect(admission.Run.Source).toBe('manual');
      expect((response.request().postDataJSON() as { RequestId: string }).RequestId).toMatch(uuidPattern);
      result.ManualRunId = admission.Run.Id;
      await saveResult(fixture.ResultPath, result);
      await expect(runDialog(page)).toBeVisible();
      const actual = await readRun(context.request, admission.Run.Id);
      if (['pending', 'running'].includes(actual.Run.State) && await runDialog(page).getByRole('button', { name: 'Stop run', exact: true }).isVisible()) {
        await runDialog(page).getByRole('button', { name: 'Stop run', exact: true }).click();
        const stop = page.getByRole('dialog', { name: 'Stop this run?', exact: true });
        await expect(stop).toBeVisible();
        await stop.getByRole('button', { name: /^(Keep run|Close)$/ }).click();
        result.Observations.ActiveStopDialogObserved = true;
      }
      const completed = await completeRun(context.request, fixture, admission.Run.Id);
      await expect(runDialog(page).getByRole('progressbar', { name: 'Library progress', exact: true })).toHaveAttribute('aria-valuetext', '2 of 2 libraries finished', { timeout: 15_000 });
      await expect(runDialog(page).getByRole('list', { name: 'Task library work', exact: true }).getByRole('listitem')).toHaveCount(2);
      const jobsResponse = await context.request.get('/admin/v1/jobs');
      expect(jobsResponse.status()).toBe(200);
      const jobs = (await jobsResponse.json() as { Items: Job[] }).Items;
      for (const child of completed.Children.Items) {
        const job = jobs.find((item) => item.Id === child.ScanJobId);
        expect(job?.Status).toBe('completed');
        expect(job?.ForceProbe).toBe(false);
        expect(job?.Scanned).toBe(child.Scanned);
        expect(job?.Added).toBe(child.Added);
        expect(job?.Updated).toBe(child.Updated);
      }
      const first = await context.request.get(`${runPath(admission.Run.Id)}?StartIndex=0&Limit=1`);
      const second = await context.request.get(`${runPath(admission.Run.Id)}?StartIndex=1&Limit=1`);
      expect(first.status()).toBe(200); expect(second.status()).toBe(200);
      const firstPage = (await first.json() as TaskRunDetail).Children;
      const secondPage = (await second.json() as TaskRunDetail).Children;
      expect(firstPage.TotalRecordCount).toBe(2); expect(secondPage.TotalRecordCount).toBe(2);
      expect(firstPage.Items).toHaveLength(1); expect(secondPage.Items).toHaveLength(1);
      expect(firstPage.Items[0].Id).not.toBe(secondPage.Items[0].Id);
      const resizedChildren = responseFor(page, 'GET', runPath(admission.Run.Id), { StartIndex: '0', Limit: '25' });
      await runDialog(page).getByRole('combobox', { name: /Libraries per page/ }).click();
      await page.getByRole('option', { name: '25', exact: true }).click();
      const childResponse = await resizedChildren;
      expect(childResponse.status()).toBe(200);
      expect((await childResponse.json() as TaskRunDetail).Children.Limit).toBe(25);
      await expect(runDialog(page).getByRole('button', { name: 'Go to next page', exact: true })).toBeDisabled();
      await safeScreenshot(page, testInfo.outputPath('tasks-desktop.png'), sensitive, fixture);
      await runDialog(page).getByRole('button', { name: 'Back to run history', exact: true }).click();
      await expect(runDialog(page).getByRole('table', { name: 'Task runs', exact: true }).getByRole('row')).toHaveCount(2);
      const resizedHistory = responseFor(page, 'GET', `${taskPath(task.Id)}/runs`, { StartIndex: '0', Limit: '25' });
      await runDialog(page).getByRole('combobox', { name: /Runs per page/ }).click();
      await page.getByRole('option', { name: '25', exact: true }).click();
      expect((await resizedHistory).status()).toBe(200);
      await expect(runDialog(page).getByRole('button', { name: 'Go to next page', exact: true })).toBeDisabled();
      await runDialog(page).getByRole('button', { name: 'Close', exact: true }).click();
      result.Checks.ManualRunAndLibraryCounters = true;
    });

    await test.step('lose a committed start response and reuse its receipt after the run has finished', async () => {
      await waitForIdle(context.request, fixture);
      await refreshTasks(page);
      const committed = await loseCommittedResponse<TaskAdmissionResponse>(page, `${taskPath(task.Id)}/runs`, 'POST', 202,
        () => taskCard(page, task.Name).getByRole('button', { name: 'Start task', exact: true }).click());
      expect(committed.result.Admitted).toBe(true);
      const requestId = (committed.body as { RequestId: string }).RequestId;
      expect(requestId).toMatch(uuidPattern);
      result.ReceiptRequestId = requestId; result.ReceiptRunId = committed.result.Run.Id;
      await saveResult(fixture.ResultPath, result);
      await expect(taskCard(page, task.Name).getByText(/A start request has not been confirmed/)).toBeVisible();
      await completeRun(context.request, fixture, committed.result.Run.Id);
      const before = await readRuns(context.request, fixture);
      const retriedResponse = responseFor(page, 'POST', `${taskPath(task.Id)}/runs`);
      await taskCard(page, task.Name).getByRole('button', { name: 'Check start result', exact: true }).click();
      const retried = await retriedResponse;
      expect(retried.status()).toBe(202);
      expect((retried.request().postDataJSON() as { RequestId: string }).RequestId).toBe(requestId);
      const received = await retried.json() as TaskAdmissionResponse;
      expect(received.Run.Id).toBe(committed.result.Run.Id);
      expect(received.Run.State).toBe('completed');
      await expect(runDialog(page).getByText('The start request is confirmed. Showing its recorded run.', { exact: true })).toBeVisible();
      expect((await readRuns(context.request, fixture)).TotalRecordCount).toBe(before.TotalRecordCount);
      const storageKey = `goby.task-start.${encodeURIComponent(fixture.Administrator.Id)}.${encodeURIComponent(task.Id)}`;
      await expect.poll(() => page.evaluate((key) => sessionStorage.getItem(key), storageKey)).toBeNull();
      await runDialog(page).getByRole('button', { name: 'Close', exact: true }).click();
      result.Checks.CommittedStartResponseLossAndDurableReceipt = true;
    });

    let calendar: ReturnType<typeof futureCalendar>;
    await test.step('preview a real two-second interval, observe its scheduled run, then remove it', async () => {
      await refreshTasks(page);
      task = await openSchedule(page, task);
      await scheduleDialog(page).getByRole('button', { name: 'Add trigger', exact: true }).click();
      await choose(page, triggerRegion(page, 0), 'When to run', 'At an interval');
      await choose(page, triggerRegion(page, 0), 'Interval unit', 'Seconds');
      await triggerRegion(page, 0).getByRole('textbox', { name: 'Interval', exact: true }).fill('0.5');
      await expect(scheduleDialog(page).getByRole('button', { name: 'Preview schedule', exact: true })).toBeDisabled();
      await triggerRegion(page, 0).getByRole('textbox', { name: 'Interval', exact: true }).fill('2');
      await triggerRegion(page, 0).getByRole('switch', { name: 'Limit run time', exact: true }).check();
      await choose(page, triggerRegion(page, 0), 'Maximum run time unit', 'Seconds');
      await triggerRegion(page, 0).getByRole('textbox', { name: 'Maximum run time', exact: true }).fill('120.0000001');
      await expect(scheduleDialog(page).getByRole('button', { name: 'Save schedule', exact: true })).toBeDisabled();
      const preview = await previewSchedule(page, fixture);
      expect(preview.input.Triggers[0].IntervalTicks).toBe('20000000');
      expect(preview.input.Triggers[0].MaxRuntimeTicks).toBe('1200000001');
      expect(preview.result.Items[0].Occurrences).toHaveLength(3);
      expect(preview.result.Items[0].Occurrences.every((time) => Date.parse(time) > Date.parse(preview.result.ServerTime))).toBe(true);
      calendar = futureCalendar(preview.result.ServerTime);
      await triggerRegion(page, 0).getByRole('textbox', { name: 'Interval', exact: true }).fill('3');
      await expect(scheduleDialog(page).getByRole('region', { name: 'Schedule preview', exact: true })).toHaveCount(0);
      await expect(scheduleDialog(page).getByRole('button', { name: 'Save schedule', exact: true })).toBeDisabled();
      await triggerRegion(page, 0).getByRole('textbox', { name: 'Interval', exact: true }).fill('2');
      await previewSchedule(page, fixture);
      const before = new Set((await readRuns(context.request, fixture)).Items.map((run) => run.Id));
      task = await saveSchedule(page, fixture, task);
      expect(task.Triggers[0].IntervalTicks).toBe('20000000');
      await expect.poll(async () => (await readRuns(context.request, fixture)).Items.some((run) => !before.has(run.Id) && run.Source === 'schedule'), {
        timeout: 45_000, intervals: [250, 500, 1000], message: 'The saved interval must admit a real scheduler run.',
      }).toBe(true);
      await removeTriggers(page);
      await previewSchedule(page, fixture);
      task = await saveSchedule(page, fixture, task);
      expect(task.Triggers).toEqual([]);
      await scheduleDialog(page).getByRole('button', { name: 'Close', exact: true }).click();
      await waitForIdle(context.request, fixture);
      const scheduled = (await readRuns(context.request, fixture)).Items.filter((run) => !before.has(run.Id) && run.Source === 'schedule');
      expect(scheduled.length).toBeGreaterThan(0);
      expect(scheduled.length).toBeLessThanOrEqual(20);
      for (const run of scheduled) await completeRun(context.request, fixture, run.Id);
      result.IntervalRunIds = scheduled.map((run) => run.Id);
      result.Checks.RealIntervalTriggerAndRemoval = true;
      await saveResult(fixture.ResultPath, result);
    });

    await test.step('edit startup, daily, and weekly schedules with lossless calendar times and a real revision conflict', async () => {
      task = await openSchedule(page, task);
      await scheduleDialog(page).getByRole('button', { name: 'Add trigger', exact: true }).click();
      await choose(page, triggerRegion(page, 0), 'When to run', 'At server startup');
      await scheduleDialog(page).getByRole('button', { name: 'Add trigger', exact: true }).click();
      await triggerRegion(page, 1).getByRole('textbox', { name: 'Time of day', exact: true }).fill(calendar.daily);
      await scheduleDialog(page).getByRole('button', { name: 'Add trigger', exact: true }).click();
      await choose(page, triggerRegion(page, 2), 'When to run', 'Every week');
      await choose(page, triggerRegion(page, 2), 'Day of week', ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'][calendar.day]);
      await triggerRegion(page, 2).getByRole('textbox', { name: 'Time of day', exact: true }).fill(calendar.weekly);
      const preview = await previewSchedule(page, fixture);
      expect(preview.result.Items[0].Event).toBe('startup');
      expect(preview.result.Items[0].Occurrences).toEqual([]);
      expect(preview.result.Items[1].Occurrences).toHaveLength(3);
      expect(preview.result.Items[2].Occurrences).toHaveLength(3);
      const dailyTicks = preview.input.Triggers[1].TimeOfDayTicks;
      const weeklyTicks = preview.input.Triggers[2].TimeOfDayTicks;
      expect(dailyTicks?.endsWith('1234567')).toBe(true);
      expect(weeklyTicks?.endsWith('7654321')).toBe(true);
      await safeScreenshot(page, testInfo.outputPath('tasks-schedule-desktop.png'), sensitive, fixture);
      task = await saveSchedule(page, fixture, task);
      expect(task.Triggers.map((trigger) => trigger.Kind)).toEqual(['startup', 'daily', 'weekly']);
      expect(task.Triggers[1].TimeOfDayTicks).toBe(dailyTicks);
      expect(task.Triggers[2].TimeOfDayTicks).toBe(weeklyTicks);
      expect((await readRuns(context.request, fixture)).Items.some((run) => run.Source === 'startup')).toBe(false);
      const draftTime = calendar.daily.replace('1234567', '2345678');
      await triggerRegion(page, 1).getByRole('textbox', { name: 'Time of day', exact: true }).fill(draftTime);
      await previewSchedule(page, fixture);
      const concurrentResponse = await context.request.put(`${taskPath(task.Id)}/triggers`, {
        headers: nativeHeaders, data: { Revision: task.Revision, ScheduleTimezone: 'Europe/London', Triggers: task.Triggers.map(triggerInput) },
      });
      expect(concurrentResponse.status()).toBe(200);
      const winner = (await concurrentResponse.json() as TaskDefinitionResponse).Task;
      const writesBefore = scheduleWrites.length;
      const conflict = responseFor(page, 'PUT', `${taskPath(task.Id)}/triggers`);
      await scheduleDialog(page).getByRole('button', { name: 'Save schedule', exact: true }).click();
      expect((await conflict).status()).toBe(409);
      await expect(scheduleDialog(page).getByText(/This schedule changed after you opened it/)).toBeVisible();
      await expect(scheduleDialog(page).getByRole('button', { name: 'Save schedule', exact: true })).toBeDisabled();
      await expect(triggerRegion(page, 1).getByRole('textbox', { name: 'Time of day', exact: true })).toHaveValue(draftTime);
      expect(scheduleWrites.length).toBe(writesBefore + 1);
      expect((await readTask(context.request, fixture)).Revision).toBe(winner.Revision);
      task = await reloadSchedule(page, fixture);
      expect(task.Revision).toBe(winner.Revision);
      expect(task.ScheduleTimezone).toBe('Europe/London');
      await expect(triggerRegion(page, 1).getByRole('textbox', { name: 'Time of day', exact: true })).toHaveValue(calendar.daily);
      expect(scheduleWrites.length).toBe(writesBefore + 1);
      result.Checks.CalendarPrecisionAndRevisionConflict = true;
    });

    await test.step('lose a real committed schedule save and require explicit reload before editing again', async () => {
      await timezone(page, 'UTC');
      await previewSchedule(page, fixture);
      const writesBefore = scheduleWrites.length;
      const committed = await loseCommittedResponse<TaskDefinitionResponse>(page, `${taskPath(task.Id)}/triggers`, 'PUT', 200,
        () => scheduleDialog(page).getByRole('button', { name: 'Save schedule', exact: true }).click());
      await expect(scheduleDialog(page).getByText(/The save result could not be confirmed/)).toBeVisible();
      await expect(scheduleDialog(page).getByRole('button', { name: 'Save schedule', exact: true })).toBeDisabled();
      const stored = await readTask(context.request, fixture);
      expect(stored.Revision).toBe(committed.result.Task.Revision);
      expect(stored.ScheduleTimezone).toBe('UTC');
      expect(scheduleWrites.length).toBe(writesBefore + 1);
      task = await reloadSchedule(page, fixture);
      expect(task.Revision).toBe(stored.Revision);
      expect(scheduleWrites.length).toBe(writesBefore + 1);
      await triggerRegion(page, 0).getByRole('button', { name: 'Remove trigger 1', exact: true }).click();
      const finalPreview = await previewSchedule(page, fixture);
      expect(finalPreview.input.Triggers.map((trigger) => trigger.Kind)).toEqual(['daily', 'weekly']);
      expect(finalPreview.result.Items.every((item) => Date.parse(item.Occurrences[0]) - Date.parse(finalPreview.result.ServerTime) > 10 * 60 * 60 * 1000)).toBe(true);
      task = await saveSchedule(page, fixture, task);
      expect(task.Triggers.map((trigger) => trigger.Kind)).toEqual(['daily', 'weekly']);
      await scheduleDialog(page).getByRole('button', { name: 'Close', exact: true }).click();
      result.Checks.CommittedScheduleResponseLossAndExplicitReload = true;
    });

    await test.step('retain native scan history and route library task links directly to it', async () => {
      const before = await readRuns(context.request, fixture);
      await page.getByRole('tab', { name: 'Scan history', exact: true }).click();
      await expect(page.getByRole('heading', { name: 'Scan and refresh history', exact: true })).toBeVisible();
      await expect(page.getByRole('list', { name: 'Library scan tasks', exact: true }).getByRole('listitem').first()).toBeVisible();
      await safeScreenshot(page, testInfo.outputPath('tasks-scan-history.png'), sensitive, fixture);
      await page.getByRole('button', { name: 'Manage libraries', exact: true }).click();
      const library = fixture.Libraries[0];
      const card = page.getByRole('list', { name: 'Media libraries', exact: true }).getByRole('listitem').filter({ has: page.getByRole('heading', { name: library.Name, exact: true }) });
      const response = responseFor(page, 'POST', `/admin/v1/libraries/${library.Id}/scan`);
      await card.getByRole('button', { name: 'Scan library', exact: true }).click();
      const requested = await response;
      expect(requested.status()).toBe(202);
      const job = (await requested.json() as { Job: Job }).Job;
      await page.getByRole('button', { name: 'View tasks', exact: true }).first().click();
      await expect(page.getByRole('tab', { name: 'Scan history', exact: true })).toHaveAttribute('aria-selected', 'true');
      const record = page.locator(`[data-job-id="${job.Id}"]`);
      await expect(record).toBeVisible();
      await expect(record.getByText('Completed', { exact: true })).toBeVisible({ timeout: 45_000 });
      const jobsResponse = await context.request.get('/admin/v1/jobs');
      expect(jobsResponse.status()).toBe(200);
      const persisted = (await jobsResponse.json() as { Items: Job[] }).Items.find((item) => item.Id === job.Id);
      expect(persisted?.Status).toBe('completed');
      expect(persisted?.Scanned).toBeGreaterThanOrEqual(1);
      expect(persisted?.ForceProbe).toBe(false);
      expect((await readRuns(context.request, fixture)).TotalRecordCount).toBe(before.TotalRecordCount);
      await page.getByRole('tab', { name: 'Available tasks', exact: true }).click();
      result.Checks.LegacyScanHistoryAndLibraryLink = true;
    });

    await test.step('keep an unsaved schedule on mobile and discard it only after confirmation', async () => {
      await page.setViewportSize({ width: 390, height: 844 });
      task = await openSchedule(page, task);
      const before = task;
      const input = triggerRegion(page, 0).getByRole('textbox', { name: 'Time of day', exact: true });
      await input.fill('04:05:06.3456789');
      await scheduleDialog(page).getByRole('button', { name: 'Close', exact: true }).click();
      let discard = page.getByRole('dialog', { name: 'Discard schedule changes?', exact: true });
      await expect(discard).toBeVisible();
      await discard.getByRole('button', { name: 'Keep editing', exact: true }).click();
      const navigation = page.waitForEvent('dialog', { timeout: 10_000 }).then(async (prompt) => {
        const description = { type: prompt.type(), message: prompt.message() };
        await prompt.dismiss();
        return description;
      });
      const [, dismissed] = await Promise.all([page.goBack({ waitUntil: 'commit', timeout: 10_000 }), navigation]);
      expect(dismissed).toEqual({ type: 'confirm', message: 'Discard unsaved schedule changes and leave this page?' });
      await expect(page).toHaveURL(/\/admin\/tasks$/);
      await expect(input).toHaveValue('04:05:06.3456789');
      const bounds = await scheduleDialog(page).boundingBox();
      expect(bounds).not.toBeNull(); expect(bounds!.x).toBeGreaterThanOrEqual(0); expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(391);
      await safeScreenshot(page, testInfo.outputPath('tasks-mobile.png'), sensitive, fixture);
      await scheduleDialog(page).getByRole('button', { name: 'Close', exact: true }).click();
      discard = page.getByRole('dialog', { name: 'Discard schedule changes?', exact: true });
      await discard.getByRole('button', { name: 'Discard changes', exact: true }).click();
      await expect(scheduleDialog(page)).toHaveCount(0);
      const after = await readTask(context.request, fixture);
      expect(after.Revision).toBe(before.Revision);
      expect(after.Triggers).toEqual(before.Triggers);
      result.Checks.MobileLayoutAndUnsavedScheduleGuard = true;
    });

    await waitForIdle(context.request, fixture);
    await verifyMedia(fixture);
    task = await readTask(context.request, fixture);
    expect(task.Triggers.map((trigger) => trigger.Kind)).toEqual(['daily', 'weekly']);
    expect(task.ScheduleTimezone).toBe('UTC');
    expect(task.Triggers.every((trigger) => !trigger.CalculationError && trigger.NextFireAt !== null && Date.parse(trigger.NextFireAt) > Date.now() + 60 * 60 * 1000)).toBe(true);
    for (const library of fixture.Libraries) {
      const response = await context.request.get(`/admin/v1/libraries/${library.Id}/items?StartIndex=0&Limit=200`);
      expect(response.status()).toBe(200);
      const items = await response.json() as { TotalRecordCount: number };
      expect(items.TotalRecordCount).toBe(library.FileCount);
    }
    expect(pageErrors).toEqual([]);
    result.FinalRevision = task.Revision; result.FinalTriggers = task.Triggers; result.FinalTimezone = task.ScheduleTimezone;
    result.Checks.OwnedMediaAndStableTaskIdentity = true;
    result.Complete = true;
    await saveResult(fixture.ResultPath, result);
    keepFutureRules = true;
  } finally {
    // Failure must not leave the short interval running. The runner separately
    // repeats cleanup and revokes every owned login, including this browser's.
    if (!keepFutureRules && nativeHeaders) {
      const current = await readTask(context.request, fixture);
      const cleared = await context.request.put(`${taskPath(fixture.TaskId)}/triggers`, {
        headers: nativeHeaders, data: { Revision: current.Revision, ScheduleTimezone: 'UTC', Triggers: [] },
      });
      expect(cleared.status()).toBe(200);
    }
    await saveResult(fixture.ResultPath, result);
  }
});
