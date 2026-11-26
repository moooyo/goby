import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Locator, Page, Request } from '@playwright/test';
import type { TaskDefinition, TaskDefinitionsResponse, TaskRun } from '../src/api';
import { dashboardShellResponse, json, setDashboardPreferences } from './dashboard-fixture';
import { noOverflow } from './dashboard-review';

const timestamp = '2026-09-30T08:00:00Z';
const now = '2026-09-30T08:01:00Z';
const csrf = 'synthetic-dashboard-tasks-csrf';
const administrator = { Id: 'dashboard-tasks-admin', Name: 'Task administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const metadataFailure = 'The metadata provider could not be reached.';
const taskListPath = '/admin/v1/tasks';
const uuidV4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

function run(taskId: string, id: string, state: TaskRun['State'] = 'running'): TaskRun {
  return { Id: id, TaskId: taskId, State: state, Source: 'manual', RequestId: null, CreatedAt: timestamp, StartedAt: timestamp, FinishedAt: state === 'completed' || state === 'failed' ? timestamp : null, StopRequestedAt: null, StopReason: '', ErrorCode: '', ErrorMessage: '', TotalChildren: 4, TerminalChildren: state === 'running' ? 2 : 4, CompletedChildren: state === 'failed' ? 3 : state === 'running' ? 2 : 4, FailedChildren: state === 'failed' ? 1 : 0, CancelledChildren: 0, InterruptedChildren: 0, UnavailableChildren: 0, Scanned: 23_410, Added: 150, Updated: 28 };
}
function task(id: string, name: string, description: string): TaskDefinition {
  return { Id: id, Key: id, Name: name, Description: description, Category: 'Library', IsHidden: false, Enabled: true, Revision: '1', ScheduleTimezone: 'UTC', CurrentRun: null, LastRun: null, NextRunAt: null, Triggers: [{ Id: `${id}-trigger`, Kind: 'daily', IntervalTicks: null, TimeOfDayTicks: '144000000000', DayOfWeek: null, MaxRuntimeTicks: null, NextFireAt: null, CalculationError: '' }] };
}
function pageOf<T>(items: T[], query: URLSearchParams, filters: string[] = []) {
  const start = query.get('StartIndex') ?? '0';
  const limit = query.get('Limit') ?? '50';
  const StartIndex = Number(start), Limit = Number(limit);
  if (!Number.isSafeInteger(StartIndex) || StartIndex < 0 || String(StartIndex) !== start ||
    !Number.isInteger(Limit) || Limit < 1 || Limit > 200 || String(Limit) !== limit ||
    [...query.keys()].some((key) => !['StartIndex', 'Limit', ...filters].includes(key) || query.getAll(key).length !== 1)) return;
  return { Items: items.slice(StartIndex, StartIndex + Limit), TotalRecordCount: items.length, StartIndex, Limit };
}
function postBody(request: Request): Record<string, unknown> | undefined {
  try {
    const body: unknown = request.postDataJSON();
    if (body && typeof body === 'object' && !Array.isArray(body)) return body as Record<string, unknown>;
  } catch { /* Malformed writes are rejected by the fixture. */ }
}

class TasksAPI {
  tasks = [
    task('scan', 'Scan all libraries', 'Scan configured libraries for new and changed media.'),
    task('metadata', 'Refresh metadata', 'Refresh media details from local files and online providers.'),
    task('subtitles', 'Download subtitles', 'Download subtitles for movies and episodes.'),
    task('cleanup', 'Maintain caches', 'Remove expired metadata and subtitle cache entries.'),
  ];
  runs = new Map<string, TaskRun>();
  starts: { taskId: string; requestId: string }[] = [];
  cancels: string[] = [];
  unexpected: string[] = [];
  invalidCSRF: string[] = [];
  loseFirstStartResponse = false;
  private nextRunId = 1;

  constructor() {
    this.tasks[0].CurrentRun = run('scan', 'scan-original');
    this.tasks[1].LastRun = { ...run('metadata', 'metadata-last', 'failed'), ErrorMessage: metadataFailure };
    this.tasks[2].LastRun = run('subtitles', 'subtitles-last', 'completed');
    for (const definition of this.tasks) for (const value of [definition.CurrentRun, definition.LastRun]) if (value) this.runs.set(value.Id, value);
  }
  async install(context: BrowserContext) {
    // Every API request is isolated from any running Goby instance.
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const method = request.method();
      const url = new URL(request.url());
      const { pathname: path, searchParams: query } = url;
      if (method !== 'GET' && request.headers()['x-csrf-token'] !== csrf) {
        this.invalidCSRF.push(path);
        return json(route, { Error: { Code: 'invalid_synthetic_csrf', Message: 'A valid CSRF token is required.' } }, 403);
      }
      if (method === 'GET') {
        if (!url.search) {
          if (path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
          if (path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrf });
          if (path === taskListPath) return json(route, { Items: this.tasks, TotalRecordCount: this.tasks.length });
          const shell = dashboardShellResponse(path, []);
          if (shell !== undefined) return json(route, shell);
        }
        if (path === '/admin/v1/activity' && query.get('Action') === 'task.admitted' && Number.isFinite(Date.parse(query.get('MinDate') ?? ''))) {
          const page = pageOf([], query, ['Action', 'MinDate']);
          if (page) return json(route, { ...page, RetentionDays: 30 });
        }
      }

      const taskMatch = /^\/admin\/v1\/tasks\/([^/]+)\/runs$/.exec(path);
      const definition = taskMatch && this.tasks.find(({ Id }) => Id === taskMatch[1]);
      const input = method === 'POST' && !url.search ? postBody(request) : undefined;
      if (definition && method === 'GET') {
        const values = [...this.runs.values()].filter(({ TaskId }) => TaskId === definition.Id)
          .sort((left, right) => Date.parse(right.CreatedAt) - Date.parse(left.CreatedAt) || right.Id.localeCompare(left.Id));
        const page = pageOf(values, query);
        if (page) return json(route, page);
      }
      if (definition && input && Object.keys(input).length === 1 && typeof input.RequestId === 'string' && uuidV4.test(input.RequestId)) {
        const requestId = input.RequestId;
        this.starts.push({ taskId: definition.Id, requestId });
        let value = [...this.runs.values()].find((candidate) => candidate.TaskId === definition.Id && candidate.RequestId === requestId);
        const admitted = !value;
        if (!value) {
          value = { ...run(definition.Id, `${definition.Id}-started-${this.nextRunId++}`), RequestId: requestId };
          this.runs.set(value.Id, value);
          definition.CurrentRun = value;
        }
        if (this.loseFirstStartResponse && this.starts.length === 1) return route.abort('failed');
        return json(route, { Run: value, Admitted: admitted }, 202);
      }

      const runMatch = /^\/admin\/v1\/task-runs\/([^/]+)(\/cancel)?$/.exec(path);
      const previous = runMatch ? this.runs.get(runMatch[1]) : undefined;
      if (runMatch && previous) {
        if (!runMatch[2] && method === 'GET') {
          const children = pageOf([], query);
          if (children) return json(route, { Run: previous, Children: children });
        }
        if (runMatch[2] && input && Object.keys(input).length === 0) {
          this.cancels.push(previous.Id);
          const value: TaskRun = { ...previous, State: 'stopping', StopRequestedAt: now, StopReason: 'administrator' };
          this.runs.set(value.Id, value);
          const task = this.tasks.find(({ Id }) => Id === value.TaskId)!;
          if (task.CurrentRun?.Id === value.Id) task.CurrentRun = value;
          return json(route, { Run: value }, 202);
        }
      }
      this.unexpected.push(`${method} ${path}${url.search}`);
      return json(route, { Error: { Code: 'unexpected_synthetic_request', Message: 'No synthetic response was configured.' } }, 501);
    });
  }
}

const test = base.extend<{ api: TasksAPI }>({ api: async ({ context, page }, use) => {
  await setDashboardPreferences(context);
  const api = new TasksAPI();
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await api.install(context);
  await use(api);
  expect(api.unexpected).toEqual([]);
  expect(api.invalidCSRF).toEqual([]);
  expect(errors).toEqual([]);
} });
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });
test.beforeEach(async ({ page }) => {
  await page.clock.install({ time: new Date(now) });
});

function taskList(page: Page) { return page.getByRole('list', { name: 'Available server tasks', exact: true }); }
function row(page: Page, name: string) { return taskList(page).getByRole('listitem').filter({ has: page.getByRole('heading', { name, exact: true }) }); }
function scanCard(page: Page, includeHidden = false) { return page.getByRole('region', { name: 'Scan all libraries', exact: true, includeHidden }); }
async function open(page: Page) { await page.goto('/admin/system/tasks'); await expect(row(page, 'Scan all libraries')).toBeVisible(); }
async function readAfter(page: Page, path: string, action: () => Promise<unknown>) {
  const [response] = await Promise.all([
    page.waitForResponse((response) => response.request().method() === 'GET' && new URL(response.url()).pathname === path),
    action(),
  ]);
  expect(response.ok()).toBe(true);
}
async function pollTasks(page: Page, expectedRunId: string) {
  // Match the wanted snapshot even if a previous poll was already in flight.
  const [response] = await Promise.all([
    page.waitForResponse(async (response) => response.request().method() === 'GET'
      && new URL(response.url()).pathname === taskListPath && response.ok()
      && ((await response.json()) as TaskDefinitionsResponse).Items.some((task) => task.CurrentRun?.Id === expectedRunId)),
    page.clock.fastForward(5_001),
  ]);
  expect(response.status()).toBe(200);
}

const receiptPrefix = 'goby.task-start.';
const metadataReceipt = `${receiptPrefix}${administrator.Id}.metadata`;
function readMetadataReceipt(page: Page) { return page.evaluate((key) => sessionStorage.getItem(key), metadataReceipt); }

test('an unconfirmed start keeps its receipt across reload and does not admit another run', async ({ page, api }) => {
  api.loseFirstStartResponse = true;
  await open(page);
  const metadata = row(page, 'Refresh metadata');
  const checkStart = metadata.getByRole('button', { name: 'Check start result', exact: true });
  const details = page.getByRole('dialog', { name: 'Run details · Refresh metadata', exact: true });
  expect(await readMetadataReceipt(page)).toBeNull();

  await metadata.getByRole('button', { name: 'Start task', exact: true }).click();
  await expect(checkStart).toBeEnabled();
  await expect(metadata).toContainText('A start request has not been confirmed');
  expect(api.starts).toEqual([{ taskId: 'metadata', requestId: expect.stringMatching(uuidV4) }]);
  const start = api.starts[0];
  expect(await readMetadataReceipt(page)).toBe(start.requestId);
  expect(api.runs.get('metadata-started-1')?.RequestId).toBe(start.requestId);

  await page.reload();
  await expect(checkStart).toBeEnabled();
  await expect(metadata).toContainText('A start request has not been confirmed');
  expect(api.starts).toEqual([start]);
  await pollTasks(page, 'metadata-started-1');
  await expect(checkStart).toBeEnabled();
  await expect(details).toHaveCount(0);
  expect(await readMetadataReceipt(page)).toBe(start.requestId);
  expect(api.starts).toEqual([start]);

  await checkStart.click();
  await expect(details).toBeVisible();
  await expect(details).toContainText('The start request is confirmed. Showing its recorded run.');
  await expect(details.getByText('metadata-started-1', { exact: true })).toBeVisible();
  expect(api.starts).toEqual([start, start]);
  expect([...api.runs.values()].filter(({ TaskId, RequestId }) => TaskId === 'metadata' && RequestId !== null))
    .toMatchObject([{ Id: 'metadata-started-1', RequestId: start.requestId }]);
  await expect.poll(() => page.evaluate((prefix) => Object.keys(sessionStorage).filter((key) => key.startsWith(prefix)), receiptPrefix)).toEqual([]);
  await details.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(metadata.getByRole('button', { name: 'Stop run', exact: true })).toBeEnabled();
  await expect(checkStart).toHaveCount(0);
  expect(api.cancels).toEqual([]);
});

test('direct stop remains bound to the confirmed run when another run appears', async ({ page, api }) => {
  await open(page);
  await expect(scanCard(page).getByText('23,410', { exact: true })).toBeVisible();
  await row(page, 'Scan all libraries').getByRole('button', { name: 'Stop run', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Stop this run?', exact: true });
  await expect(confirmation).toBeVisible();
  expect(api.cancels).toEqual([]);

  const next = { ...run('scan', 'scan-newer'), CreatedAt: '2026-09-30T08:00:30Z', StartedAt: '2026-09-30T08:00:30Z', Scanned: 45_678 };
  api.runs.set(next.Id, next);
  api.tasks[0].CurrentRun = next;
  await pollTasks(page, next.Id);
  // The modal keeps the overview inert; verify that the replacement actually rendered before confirming the stop.
  const background = scanCard(page, true);
  await expect(background.getByText('45,678', { exact: true })).toBeVisible();
  await expect(background.getByText('23,410', { exact: true })).toHaveCount(0);
  await expect(confirmation).toBeVisible();
  expect(api.cancels).toEqual([]);
  await confirmation.getByRole('button', { name: 'Stop run', exact: true }).click();

  const details = page.getByRole('dialog', { name: 'Run details · Scan all libraries', exact: true });
  await expect(details).toBeVisible();
  await expect(details.getByText('scan-original', { exact: true })).toBeVisible();
  await expect(details.getByText('Stopping', { exact: true })).toBeVisible();
  await expect(details.getByText('Stop reason: administrator', { exact: true })).toBeVisible();
  await expect(details.getByText('No work items were recorded for this run.', { exact: true })).toBeVisible();
  const refresh = details.getByRole('button', { name: 'Refresh run', exact: true });
  await expect(refresh).toBeEnabled();
  const original = api.runs.get('scan-original')!;
  const cancelled: TaskRun = { ...original, State: 'cancelled', FinishedAt: '2026-09-30T08:01:10Z', TerminalChildren: original.TotalChildren, CancelledChildren: original.TotalChildren - original.CompletedChildren };
  // The coordinator finishes the original run after the stop was acknowledged.
  api.runs.set(original.Id, cancelled);
  api.tasks[0].LastRun = cancelled;
  await readAfter(page, '/admin/v1/task-runs/scan-original', () => refresh.click());
  await expect(details.getByText('Stop reason: administrator', { exact: true })).toBeVisible();
  await expect(details.getByText('scan-original', { exact: true })).toBeVisible();
  await expect(details.getByText('scan-newer', { exact: true })).toHaveCount(0);
  await expect(details.getByText('Cancelled', { exact: true })).toBeVisible();
  expect(api.cancels).toEqual(['scan-original']);
  expect(api.tasks[0].CurrentRun).toMatchObject({ Id: 'scan-newer', State: 'running' });
  expect(api.starts).toEqual([]);
});

async function horizontalBounds(locator: Locator) {
  await expect(locator).toBeVisible();
  const bounds = (await locator.boundingBox())!;
  expect(bounds.width).toBeGreaterThan(0);
  return { left: bounds.x, right: bounds.x + bounds.width };
}
function expectInside(inner: { left: number; right: number }, outer: { left: number; right: number }) {
  expect(inner.left).toBeGreaterThanOrEqual(outer.left);
  expect(inner.right).toBeLessThanOrEqual(outer.right);
}

for (const viewport of [{ width: 1440, height: 1000 }, { width: 375, height: 812 }]) {
  test(`task rows and the task views disclosure fit at ${viewport.width}px`, async ({ page, api }, testInfo) => {
    await page.setViewportSize(viewport);
    await open(page);
    const card = scanCard(page);
    await expect(card.getByRole('button', { name: 'View current run · Scan all libraries', exact: true })).toBeVisible();
    const progress = card.getByRole('progressbar', { name: 'Task work progress', exact: true });
    await expect(progress).toHaveAttribute('aria-valuetext', 'Scanned 23410 · added 150 · updated 28');
    await expect(progress).not.toHaveAttribute('aria-valuenow');
    await expect(card.locator('dl dt')).toHaveText(['Scanned', 'Added', 'Updated']);
    await expect(card.locator('dl dd')).toHaveText(['23,410', '150', '28']);
    await expect(row(page, 'Scan all libraries').getByText('Running', { exact: true })).toBeVisible();
    await expect(row(page, 'Refresh metadata').getByText('Failed', { exact: true })).toHaveCount(0);
    const history = page.getByRole('table', { name: 'Recent task runs', exact: true });
    await expect(history).toHaveAttribute('aria-busy', 'false');
    await expect(history).toHaveAttribute('data-initiators-loading', 'false');
    const failed = history.getByRole('row').filter({ has: page.getByRole('button', { name: 'View runs · Refresh metadata', exact: true }) });
    await expect(failed.getByRole('cell', { name: 'Failed', exact: true })).toBeVisible();
    await expect(failed.getByRole('cell', { name: metadataFailure, exact: true })).toBeVisible();

    const list = await horizontalBounds(taskList(page));
    const viewportBounds = { left: 0, right: viewport.width };
    expectInside(list, viewportBounds);
    for (const item of await taskList(page).getByRole('listitem').all()) {
      const bounds = await horizontalBounds(item);
      expectInside(bounds, list);
      const edit = item.getByRole('button', { name: 'Edit schedule', exact: true });
      expectInside(await horizontalBounds(edit), bounds);
    }

    const disclosure = page.locator('summary[aria-label="More task views"]');
    const views = page.getByRole('group', { name: 'Task views', exact: true });
    await expect(views).toBeHidden();
    expectInside(await horizontalBounds(disclosure), viewportBounds);
    await noOverflow(page);
    await disclosure.click();
    await expect(views).toBeVisible();
    const names = ['Available tasks', 'Scan history', 'Media processing'];
    await expect(views.getByRole('button')).toHaveText(names);
    const menu = await horizontalBounds(views);
    expectInside(menu, viewportBounds);
    for (const name of names) {
      const button = views.getByRole('button', { name, exact: true });
      await expect(button).toHaveAttribute('aria-pressed', name === 'Available tasks' ? 'true' : 'false');
      expectInside(await horizontalBounds(button), menu);
    }
    await noOverflow(page);
    await page.screenshot({ path: testInfo.outputPath(`tasks-${viewport.width}.png`), fullPage: true });
    await views.getByRole('button', { name: 'Available tasks', exact: true }).click();
    await expect(views).toBeHidden();
    await expect(disclosure).toBeFocused();
    expect(api.starts).toEqual([]);
    expect(api.cancels).toEqual([]);
  });
}
