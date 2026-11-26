import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Page, Request, Route } from '@playwright/test';
import type { MediaDiagnosticRunDetail, MediaDiagnosticRunState, MediaDiagnosticRunSummary, MediaDiagnosticsResponse, ServerSettings, StartMediaDiagnosticInput } from '../src/api';
import type { SystemStatus } from '../src/systemStatusApi';
import { dashboardShellResponse, json, setDashboardPreferences } from './dashboard-fixture';

// These synthetic browser tests exercise the actual built administrator page.
// All native API traffic is intercepted, including unexpected requests. They
// establish no native identity, FFmpeg execution, hardware use, or media pass.
const instanceId = 'a'.repeat(32);
const administratorId = 'b'.repeat(32);
const retainedRunId = 'c'.repeat(32);
const csrfToken = 'synthetic-media-diagnostics-csrf';
const startToken = '0.' + 'd'.repeat(64);
const timestamp = '2026-09-16T08:00:00Z';
const endpoint = '/admin/v1/media-diagnostics';
const pendingStorageKey = `goby.media-diagnostics.pending.${administratorId}`;

const settings: ServerSettings = {
  Revision: '1',
  Defaults: { ServerName: 'Synthetic diagnostics browser', MaxBitrate: 20_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2 },
  Overrides: { ServerName: null, MaxBitrate: null, MaxWidth: null, MaxHeight: null, MaxAudioChannels: null },
  Effective: { ServerName: 'Synthetic diagnostics browser', MaxBitrate: 20_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2 },
  Sources: { ServerName: 'deployment', MaxBitrate: 'deployment', MaxWidth: 'deployment', MaxHeight: 'deployment', MaxAudioChannels: 'deployment' },
  UpdatedAt: timestamp, ServerNameMode: 'deployment', Encoding: { TranscodingMaxWidth: 0 },
  Deployment: { HostName: 'synthetic-browser', TranscodingEnabled: false, HardwareDecoder: 'software', HardwareEncoder: 'software', Threads: 1, MaxJobs: 0, MaxUserJobs: 0, MaxSessionJobs: 0 },
};

const systemStatus: SystemStatus = {
  Timestamp: timestamp, UptimeSeconds: 60, Host: { OS: 'linux', Architecture: 'amd64', CPUCount: 1 },
  CPU: { UsagePercent: null, Load1: null, Load5: null, Load15: null },
  Memory: { TotalBytes: null, UsedBytes: null, CachedBytes: null, SwapUsedBytes: null },
  Storage: { TotalBytes: null, UsedBytes: null, Complete: false, Volumes: [] },
  Transcoding: { Available: false, Active: 0, Limit: 0, HardwareActive: 0, SoftwareActive: 0 },
};

function summary(state: MediaDiagnosticRunState, revision: string, id = retainedRunId): MediaDiagnosticRunSummary {
  const terminal = state === 'failed' || state === 'cancelled';
  return {
    Id: id, InstanceId: instanceId, Revision: revision, Mode: 'software', State: state,
    Code: state === 'failed' ? 'diagnostic_execution_failed' : state === 'cancelled' ? 'diagnostic_cancelled' : '',
    CreatedAt: timestamp, UpdatedAt: terminal ? '2026-09-16T08:00:02Z' : timestamp,
    FinishedAt: terminal ? '2026-09-16T08:00:02Z' : null,
  };
}

function detail(run: MediaDiagnosticRunSummary): MediaDiagnosticRunDetail {
  // A null report deliberately avoids fabricating codec/content acceptance.
  return { ...run, Report: null };
}

function runPath(id = retainedRunId): string { return `${endpoint}/runs/${instanceId}/${id}`; }

interface CapturedRequest { method: string; path: string; body: string | null; csrf: string | undefined }

class MediaDiagnosticsMock {
  index: MediaDiagnosticsResponse = {
    InstanceId: instanceId, StartToken: startToken, StartTokenExpiresAt: new Date(Date.now() + 5 * 60_000).toISOString(),
    Available: true, UnavailableReason: '', HardwareConfigured: false, RetentionSeconds: 1800, MaxRetainedRuns: 32, Items: [],
  };
  requests: CapturedRequest[] = [];
  unexpected: string[] = [];
  handlers = new Map<string, (route: Route, request: Request) => Promise<void>>();

  mutations(): CapturedRequest[] { return this.requests.filter((request) => request.method !== 'GET'); }
  reads(path: string): CapturedRequest[] { return this.requests.filter((request) => request.method === 'GET' && request.path === path); }

  async install(context: BrowserContext): Promise<void> {
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const url = new URL(request.url());
      const captured = { method: request.method(), path: url.pathname, body: request.postData(), csrf: request.headers()['x-csrf-token'] };
      this.requests.push(captured);
      const handler = this.handlers.get(`${captured.method} ${captured.path}`);
      if (handler && !url.search) return handler(route, request);
      if (captured.method === 'GET' && !url.search) {
        if (captured.path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
        if (captured.path === '/admin/v1/session') return json(route, {
          User: { Id: administratorId, Name: 'Synthetic administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp },
          CSRFToken: csrfToken,
        });
        if (captured.path === endpoint) return json(route, this.index);
        const shell = dashboardShellResponse(captured.path, [], settings, systemStatus);
        if (shell !== undefined) return json(route, shell);
      }
      this.unexpected.push(`${captured.method} ${captured.path}${url.search}`);
      // Never continue or fetch an unhandled request against a real backend.
      return json(route, { Error: { Code: 'unexpected_synthetic_request', Message: 'No synthetic response was defined.' }, RequestId: 'synthetic-request' }, 501);
    });
  }
}

const test = base.extend<{ api: MediaDiagnosticsMock }>({
  api: async ({ context }, use) => {
    const api = new MediaDiagnosticsMock();
    await setDashboardPreferences(context);
    await api.install(context);
    await use(api);
    expect(api.unexpected, 'Every native API request must use an explicit synthetic fixture.').toEqual([]);
  },
});

test.use({ serviceWorkers: 'block' });

const diagnosticsSection = (page: Page) => page.getByRole('main').getByRole('region', { name: 'Playback diagnostics', exact: true });
const panel = (page: Page) => diagnosticsSection(page).getByRole('region', { name: 'Media diagnostics', exact: true });
const history = (page: Page) => panel(page).getByRole('table', { name: 'Recent media diagnostic runs', exact: true });
const historyStatus = (page: Page, state: string) => history(page).getByRole('cell', { name: state, exact: true });
const diagnosticScope = (page: Page) => panel(page).getByRole('radiogroup', { name: 'Diagnostic scope', exact: true });
const softwareScope = (page: Page) => diagnosticScope(page).getByRole('radio', { name: 'Software video and audio baseline', exact: true });
const configuredScope = (page: Page) => diagnosticScope(page).getByRole('radio', { name: 'Configured hardware path, including the software baseline', exact: true });
const selectedRunStatus = (page: Page, state: string) => panel(page)
  .getByRole('heading', { name: 'Run results', exact: true }).locator('..').getByText(state, { exact: true });

async function openDiagnostics(page: Page): Promise<void> {
  await page.goto('/admin/settings/transcode');
  await expect(page).toHaveURL(/\/admin\/settings\/transcode$/);
  const navigation = page.getByRole('navigation', { name: 'Administration', exact: true });
  await expect(navigation.getByRole('link', { name: 'Transcoding', exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('main').getByRole('heading', { name: 'Transcoding', level: 1, exact: true })).toBeVisible();
  await expect(diagnosticsSection(page).getByRole('heading', { name: 'Playback diagnostics', level: 2, exact: true })).toBeVisible();
  await expect(panel(page)).toBeVisible();
  await expect(panel(page).getByRole('button', { name: 'Refresh diagnostics', exact: true })).toBeEnabled();
  await expect(softwareScope(page)).toBeChecked();
}

test('configured diagnostics are submitted only after selecting the available hardware scope', async ({ page, api }) => {
  api.index.HardwareConfigured = true;
  api.handlers.set(`POST ${endpoint}/runs`, async (route, request) => {
    const input = request.postDataJSON() as StartMediaDiagnosticInput;
    const run = { ...detail(summary('failed', '1', input.RequestId)), Mode: 'configured' as const };
    api.handlers.set(`GET ${runPath(input.RequestId)}`, (route) => json(route, { Run: run }));
    await json(route, { Run: run }, 202);
  });

  await openDiagnostics(page);
  const software = softwareScope(page);
  const configured = configuredScope(page);
  await expect(configured).toBeEnabled();
  await configured.check();
  await expect(configured).toBeChecked();
  await expect(software).not.toBeChecked();
  expect(api.mutations()).toEqual([]);

  const [response] = await Promise.all([
    page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === `${endpoint}/runs`),
    panel(page).getByRole('button', { name: 'Start diagnostic', exact: true }).click(),
  ]);
  expect(response.status()).toBe(202);
  await expect(selectedRunStatus(page, 'Failed')).toBeVisible();
  await expect(history(page).getByRole('cell', { name: 'Configured + software', exact: true })).toBeVisible();
  await expect(panel(page).getByText(/No stage results have been reported/)).toBeVisible();
  expect(api.mutations()).toHaveLength(1);
  const [start] = api.mutations();
  expect(start.csrf).toBe(csrfToken);
  expect(JSON.parse(start.body!)).toEqual({ InstanceId: instanceId, RequestId: expect.stringMatching(/^[0-9a-f]{32}$/), StartToken: startToken, Mode: 'configured' });
});

test('unknown start retains its original identifiers and only reads the outcome after reload', async ({ page, api }) => {
  api.handlers.set(`POST ${endpoint}/runs`, async (route) => { await route.abort('failed'); });
  await openDiagnostics(page);
  await expect(configuredScope(page)).toBeDisabled();
  await panel(page).getByRole('button', { name: 'Start diagnostic', exact: true }).click();
  await expect(panel(page).getByText(/The start result has not been confirmed/)).toBeVisible();
  const checkStart = panel(page).getByRole('button', { name: 'Check start result', exact: true });
  await expect(checkStart).toBeEnabled();
  expect(api.mutations()).toHaveLength(1);
  const first = api.mutations()[0];
  expect(first.method).toBe('POST');
  expect(first.path).toBe(`${endpoint}/runs`);
  expect(first.csrf).toBe(csrfToken);
  const submitted = JSON.parse(first.body!) as StartMediaDiagnosticInput;
  expect(Object.keys(submitted).sort()).toEqual(['InstanceId', 'Mode', 'RequestId', 'StartToken']);
  expect(submitted).toMatchObject({ InstanceId: instanceId, Mode: 'software', StartToken: startToken });
  expect(submitted.RequestId).toMatch(/^[0-9a-f]{32}$/);
  const retained = { InstanceId: instanceId, RequestId: submitted.RequestId, Mode: 'software' };
  expect(await page.evaluate((key) => JSON.parse(sessionStorage.getItem(key) ?? 'null'), pendingStorageKey)).toEqual(retained);
  expect(api.reads(runPath(submitted.RequestId))).toHaveLength(0);
  await expect(panel(page).getByRole('button', { name: 'Start another diagnostic', exact: true })).toBeDisabled();

  await page.reload();
  await expect(panel(page).getByText(/The start result has not been confirmed/)).toBeVisible();
  await expect(panel(page).getByRole('button', { name: 'Refresh diagnostics', exact: true })).toBeEnabled();
  expect(await page.evaluate((key) => JSON.parse(sessionStorage.getItem(key) ?? 'null'), pendingStorageKey)).toEqual(retained);
  expect(api.mutations()).toHaveLength(1);
  expect(api.reads(runPath(submitted.RequestId))).toHaveLength(0);
  await expect(panel(page).getByRole('button', { name: 'Start another diagnostic', exact: true })).toBeDisabled();

  api.handlers.set(`GET ${runPath(submitted.RequestId)}`, async (route) => {
    await json(route, { Run: detail(summary('failed', '7', submitted.RequestId)) });
  });
  await checkStart.click();
  await expect(historyStatus(page, 'Failed')).toBeVisible();
  await expect(selectedRunStatus(page, 'Failed')).toBeVisible();
  await expect(panel(page).getByText(/The start result has not been confirmed/)).toHaveCount(0);
  await expect(panel(page).getByRole('button', { name: 'Start diagnostic', exact: true })).toBeEnabled();
  expect(await page.evaluate((key) => sessionStorage.getItem(key), pendingStorageKey)).toBeNull();
  await panel(page).getByRole('button', { name: 'Refresh run', exact: true }).click();
  await expect.poll(() => api.reads(runPath(submitted.RequestId)).length).toBeGreaterThanOrEqual(2);
  expect(api.mutations()).toHaveLength(1);
  expect(api.requests.filter((request) => request.path.startsWith(`${endpoint}/runs/`)).every((request) => request.method === 'GET' && request.path === runPath(submitted.RequestId))).toBe(true);
});

test('a late older index cannot replace a newer terminal detail or keep Start locked', async ({ page, api }) => {
  const running = summary('running', '9007199254740992');
  // The old snapshot has a later wall-clock timestamp. Only Revision orders it.
  const stale = { ...running, UpdatedAt: '2026-09-16T08:00:09Z' };
  const terminal = summary('cancelled', '9007199254740993');
  api.index.Items = [running];
  api.handlers.set(`GET ${runPath()}`, async (route) => { await json(route, { Run: detail(running) }); });
  await openDiagnostics(page);
  await expect(panel(page).getByRole('button', { name: 'Cancel run', exact: true })).toBeEnabled();
  await expect(panel(page).getByRole('button', { name: 'Start diagnostic', exact: true })).toBeDisabled();

  let releaseIndex!: () => void;
  let indexStarted = false;
  const released = new Promise<void>((resolve) => { releaseIndex = resolve; });
  const refreshDiagnostics = panel(page).getByRole('button', { name: 'Refresh diagnostics', exact: true });
  api.handlers.set(`GET ${endpoint}`, async (route) => {
    indexStarted = true;
    await released;
    await json(route, { ...api.index, Items: [stale] });
  });
  try {
    await refreshDiagnostics.click();
    await expect.poll(() => indexStarted).toBe(true);
    await expect(refreshDiagnostics).toBeDisabled();
    api.handlers.set(`GET ${runPath()}`, async (route) => { await json(route, { Run: detail(terminal) }); });
    await panel(page).getByRole('button', { name: 'Refresh run', exact: true }).click();
    await expect(historyStatus(page, 'Cancelled')).toBeVisible();
    await expect(selectedRunStatus(page, 'Cancelled')).toBeVisible();
    const response = page.waitForResponse((response) => response.request().method() === 'GET' && new URL(response.url()).pathname === endpoint);
    releaseIndex();
    expect((await response).status()).toBe(200);
  } finally { releaseIndex(); }
  // Wait for the browser to finish applying the now-released index response.
  await expect(refreshDiagnostics).toBeEnabled();
  await expect(historyStatus(page, 'Cancelled')).toBeVisible();
  await expect(selectedRunStatus(page, 'Cancelled')).toBeVisible();
  await expect(historyStatus(page, 'Running')).toHaveCount(0);
  await expect(panel(page).getByText(/A diagnostic is already active/)).toHaveCount(0);
  await expect(panel(page).getByRole('button', { name: 'Start diagnostic', exact: true })).toBeEnabled();
  expect(api.mutations()).toHaveLength(0);
});

test('confirmed cancellation remains visible when a later run read fails without retrying a mutation', async ({ page, api }) => {
  let current = summary('running', '2');
  api.index.Items = [current];
  api.handlers.set(`GET ${runPath()}`, async (route) => { await json(route, { Run: detail(current) }); });
  api.handlers.set(`POST ${runPath()}/cancel`, async (route, request) => {
    expect(request.postDataJSON()).toEqual({});
    current = summary('cancelled', '3');
    await json(route, { Run: detail(current) });
  });
  await openDiagnostics(page);
  const cancel = panel(page).getByRole('button', { name: 'Cancel run', exact: true });
  await cancel.click();
  await expect(historyStatus(page, 'Cancelled')).toBeVisible();
  await expect(selectedRunStatus(page, 'Cancelled')).toBeVisible();
  await expect(cancel).toHaveCount(0);
  await expect(panel(page).getByText('No stage results have been reported. No decoding or encoding result is confirmed yet.', { exact: true })).toBeVisible();

  const refreshRun = panel(page).getByRole('button', { name: 'Refresh run', exact: true });
  // Let the post-cancellation read settle before failing the explicit refresh.
  await expect(refreshRun).toBeEnabled();
  api.handlers.set(`GET ${runPath()}`, async (route) => {
    await json(route, { Error: { Code: 'synthetic_read_unavailable', Message: 'Synthetic diagnostic read is unavailable.' }, RequestId: 'synthetic-read' }, 503);
  });
  const [failedRead] = await Promise.all([
    page.waitForResponse((response) => response.request().method() === 'GET' && new URL(response.url()).pathname === runPath()),
    refreshRun.click(),
  ]);
  expect(failedRead.status()).toBe(503);
  const alert = panel(page).getByRole('alert').filter({ hasText: 'Synthetic diagnostic read is unavailable.' });
  await expect(alert).toBeVisible();
  await expect(alert.getByText('synthetic-read', { exact: true })).toBeVisible();
  await expect(alert.getByRole('button', { name: 'Retry', exact: true })).toBeEnabled();
  await expect(panel(page).getByText("Automatic updates stopped. The last recorded result is retained; a read failure does not establish the run's outcome.", { exact: true })).toBeVisible();
  await expect(historyStatus(page, 'Cancelled')).toBeVisible();
  await expect(selectedRunStatus(page, 'Cancelled')).toBeVisible();
  await expect(panel(page).getByText(/^Software baseline · Created:.* · diagnostic cancelled$/)).toBeVisible();
  await expect(cancel).toHaveCount(0);
  await expect(panel(page).getByRole('button', { name: 'Start diagnostic', exact: true })).toBeDisabled();
  expect(api.mutations()).toEqual([{ method: 'POST', path: `${runPath()}/cancel`, body: '{}', csrf: csrfToken }]);
});
