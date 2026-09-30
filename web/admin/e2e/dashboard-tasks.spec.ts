import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Page, Route } from '@playwright/test';
import type { TaskDefinition, TaskRun } from '../src/api';

const timestamp = '2026-09-30T08:00:00Z';
const csrf = 'synthetic-dashboard-tasks-csrf';
const administrator = { Id: 'dashboard-tasks-admin', Name: 'Task administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
function run(taskId: string, id: string, state: TaskRun['State'] = 'running'): TaskRun {
  return { Id: id, TaskId: taskId, State: state, Source: 'manual', RequestId: null, CreatedAt: timestamp, StartedAt: timestamp, FinishedAt: state === 'completed' || state === 'failed' ? timestamp : null, StopRequestedAt: null, StopReason: '', ErrorCode: '', ErrorMessage: '', TotalChildren: 4, TerminalChildren: state === 'running' ? 2 : 4, CompletedChildren: state === 'failed' ? 3 : state === 'running' ? 2 : 4, FailedChildren: state === 'failed' ? 1 : 0, CancelledChildren: 0, InterruptedChildren: 0, UnavailableChildren: 0, Scanned: 23_410, Added: 150, Updated: 28 };
}
function task(id: string, name: string, description: string): TaskDefinition {
  return { Id: id, Key: id, Name: name, Description: description, Category: 'Library', IsHidden: false, Enabled: true, Revision: '1', ScheduleTimezone: 'UTC', CurrentRun: null, LastRun: null, NextRunAt: null, Triggers: [{ Id: `${id}-trigger`, Kind: 'daily', IntervalTicks: null, TimeOfDayTicks: '144000000000', DayOfWeek: null, MaxRuntimeTicks: null, NextFireAt: null, CalculationError: '' }] };
}
async function json(route: Route, value: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) });
}
class TasksAPI {
  tasks = [
    task('scan', 'Scan all libraries', 'Scan configured libraries for new and changed media.'),
    task('metadata', 'Refresh metadata', 'Refresh media details from local files and online providers.'),
    task('subtitles', 'Download subtitles', 'Download subtitles for movies and episodes.'),
    task('cleanup', 'Maintain caches', 'Remove expired metadata and subtitle cache entries.'),
  ];
  runs = new Map<string, TaskRun>();
  starts: string[] = [];
  cancels: string[] = [];
  unexpected: string[] = [];
  invalidCSRF: string[] = [];
  loseFirstStartResponse = false;
  constructor() {
    this.tasks[0].CurrentRun = run('scan', 'scan-original');
    this.tasks[1].LastRun = run('metadata', 'metadata-last', 'failed');
    this.tasks[2].LastRun = run('subtitles', 'subtitles-last', 'completed');
    for (const definition of this.tasks) for (const value of [definition.CurrentRun, definition.LastRun]) if (value) this.runs.set(value.Id, value);
  }
  async install(context: BrowserContext) {
    // Every API request is isolated from any running Goby instance.
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const method = request.method();
      const url = new URL(request.url());
      const path = url.pathname;
      if (method !== 'GET' && request.headers()['x-csrf-token'] !== csrf) this.invalidCSRF.push(path);
      if (method === 'GET' && path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
      if (method === 'GET' && path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrf });
      if (method === 'GET' && path === '/admin/v1/tasks') return json(route, { Items: this.tasks, TotalRecordCount: this.tasks.length });
      const startMatch = /^\/admin\/v1\/tasks\/([^/]+)\/runs$/.exec(path);
      if (startMatch && method === 'POST') {
        const requestId = request.postDataJSON().RequestId as string;
        this.starts.push(requestId);
        let admitted = false;
        let value = [...this.runs.values()].find((candidate) => candidate.RequestId === requestId);
        if (!value) {
          admitted = true;
          value = { ...run(startMatch[1], `${startMatch[1]}-new`), RequestId: requestId };
          this.runs.set(value.Id, value);
          this.tasks.find((definition) => definition.Id === startMatch[1])!.CurrentRun = value;
        }
        if (this.loseFirstStartResponse && this.starts.length === 1) return route.abort('failed');
        return json(route, { Run: value, Admitted: admitted });
      }
      const cancelMatch = /^\/admin\/v1\/task-runs\/([^/]+)\/cancel$/.exec(path);
      if (cancelMatch && method === 'POST') {
        this.cancels.push(cancelMatch[1]);
        const previous = this.runs.get(cancelMatch[1])!;
        const value: TaskRun = { ...previous, State: 'cancelled', FinishedAt: timestamp, StopRequestedAt: timestamp, TerminalChildren: previous.TotalChildren, CancelledChildren: previous.TotalChildren - previous.CompletedChildren };
        this.runs.set(value.Id, value);
        const definition = this.tasks.find((candidate) => candidate.Id === value.TaskId)!;
        if (definition.CurrentRun?.Id === value.Id) { definition.CurrentRun = null; definition.LastRun = value; }
        return json(route, { Run: value });
      }
      const detailMatch = /^\/admin\/v1\/task-runs\/([^/]+)$/.exec(path);
      if (detailMatch && method === 'GET' && this.runs.has(detailMatch[1])) return json(route, { Run: this.runs.get(detailMatch[1]), Children: { Items: [], TotalRecordCount: 0, StartIndex: Number(url.searchParams.get('StartIndex') ?? 0), Limit: Number(url.searchParams.get('Limit') ?? 50) } });
      this.unexpected.push(`${method} ${path}`);
      return json(route, { Error: { Code: 'unexpected_synthetic_request', Message: 'No synthetic response was configured.' } }, 501);
    });
  }
}
const test = base.extend<{ api: TasksAPI }>({ api: async ({ context, page }, use) => {
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
function row(page: Page, name: string) { return page.getByRole('list', { name: 'Available server tasks', exact: true }).getByRole('listitem').filter({ has: page.getByRole('heading', { name, exact: true }) }); }
async function open(page: Page) { await page.goto('/admin/system/tasks'); await expect(row(page, 'Scan all libraries')).toBeVisible(); }

test('an unconfirmed start keeps its receipt across reload and does not admit another run', async ({ page, api }) => {
  api.loseFirstStartResponse = true;
  await open(page);
  await row(page, 'Refresh metadata').getByRole('button', { name: 'Start task', exact: true }).click();
  await expect(row(page, 'Refresh metadata')).toContainText('A start request has not been confirmed');
  expect(api.starts).toHaveLength(1);
  await page.reload();
  await row(page, 'Refresh metadata').getByRole('button', { name: 'Check start result', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Run details · Refresh metadata', exact: true })).toBeVisible();
  expect(api.starts).toEqual([api.starts[0], api.starts[0]]);
  expect([...api.runs.values()].filter((value) => value.RequestId === api.starts[0])).toHaveLength(1);
  await expect.poll(() => page.evaluate(() => Object.keys(sessionStorage).filter((key) => key.startsWith('goby.task-start.')))).toEqual([]);
});

test('direct stop remains bound to the confirmed run when another run appears', async ({ page, api }) => {
  await open(page);
  await row(page, 'Scan all libraries').getByRole('button', { name: 'Stop run', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Stop this run?', exact: true });
  await expect(confirmation).toBeVisible();
  expect(api.cancels).toEqual([]);
  const next = run('scan', 'scan-newer');
  api.runs.set(next.Id, next);
  api.tasks[0].CurrentRun = next;
  await page.waitForResponse((response) => new URL(response.url()).pathname === '/admin/v1/tasks' && response.request().method() === 'GET');
  await confirmation.getByRole('button', { name: 'Stop run', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Run details · Scan all libraries', exact: true })).toBeVisible();
  expect(api.cancels).toEqual(['scan-original']);
  expect(api.tasks[0].CurrentRun?.Id).toBe('scan-newer');
});

for (const viewport of [{ width: 1440, height: 1000 }, { width: 375, height: 812 }]) {
  test(`task rows and segmented controls fit at ${viewport.width}px`, async ({ page, api }, testInfo) => {
    await page.setViewportSize(viewport);
    await open(page);
    await expect(row(page, 'Scan all libraries').getByRole('progressbar')).toHaveAttribute('aria-valuenow', '50');
    await expect(row(page, 'Refresh metadata').getByText('Failed', { exact: true })).toBeVisible();
    const bounds = await page.getByRole('tablist', { name: 'Task views', exact: true }).boundingBox();
    for (const name of ['Available tasks', 'Scan history', 'Media processing']) {
      const tab = await page.getByRole('tab', { name, exact: true }).boundingBox();
      expect(tab!.x).toBeGreaterThanOrEqual(bounds!.x);
      expect(tab!.x + tab!.width).toBeLessThanOrEqual(bounds!.x + bounds!.width);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath(`tasks-${viewport.width}.png`), fullPage: true });
    expect(api.starts).toEqual([]);
    expect(api.cancels).toEqual([]);
  });
}
