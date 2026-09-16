import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Page, Request, Route } from '@playwright/test';
import type { MediaDiagnosticRunDetail, MediaDiagnosticRunState, MediaDiagnosticRunSummary, MediaDiagnosticsResponse, ServerSettings, StartMediaDiagnosticInput } from '../src/api';

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

async function json(route: Route, value: unknown, status = 200): Promise<void> {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) });
}

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
        if (captured.path === '/admin/v1/settings') return json(route, settings);
        if (captured.path === endpoint) return json(route, this.index);
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
    await api.install(context);
    await use(api);
    expect(api.unexpected, 'Every native API request must use an explicit synthetic fixture.').toEqual([]);
  },
});

test.use({ serviceWorkers: 'block' });

const panel = (page: Page) => page.getByRole('region', { name: 'Media diagnostics', exact: true });
const history = (page: Page) => panel(page).getByRole('table', { name: 'Recent media diagnostic runs', exact: true });

async function openDiagnostics(page: Page): Promise<void> {
  await page.goto('/admin/settings');
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible();
  await expect(panel(page)).toBeVisible();
  await expect(panel(page).getByRole('button', { name: 'Refresh diagnostics', exact: true })).toBeEnabled();
}

test('unknown start retains its original identifiers and only reads the outcome after reload', async ({ page, api }) => {
  api.handlers.set(`POST ${endpoint}/runs`, async (route) => { await route.abort('failed'); });
  await openDiagnostics(page);
  await panel(page).getByRole('button', { name: 'Start diagnostic', exact: true }).click();
  await expect(panel(page).getByText(/The start result has not been confirmed/)).toBeVisible();
  expect(api.mutations()).toHaveLength(1);
  const first = api.mutations()[0];
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
  expect(await page.evaluate((key) => JSON.parse(sessionStorage.getItem(key) ?? 'null'), pendingStorageKey)).toEqual(retained);
  expect(api.mutations()).toHaveLength(1);
  expect(api.reads(runPath(submitted.RequestId))).toHaveLength(0);

  api.handlers.set(`GET ${runPath(submitted.RequestId)}`, async (route) => {
    await json(route, { Run: detail(summary('failed', '7', submitted.RequestId)) });
  });
  await panel(page).getByRole('button', { name: 'Check start result', exact: true }).click();
  await expect(history(page).getByText('Failed', { exact: true })).toBeVisible();
  await expect(panel(page).getByText(/The start result has not been confirmed/)).toHaveCount(0);
  await expect(panel(page).getByRole('button', { name: 'Start diagnostic', exact: true })).toBeEnabled();
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
  api.handlers.set(`GET ${endpoint}`, async (route) => {
    indexStarted = true;
    await released;
    await json(route, { ...api.index, Items: [stale] });
  });
  const indexResponse = page.waitForResponse((response) => response.request().method() === 'GET' && new URL(response.url()).pathname === endpoint);
  await panel(page).getByRole('button', { name: 'Refresh diagnostics', exact: true }).click();
  try {
    await expect.poll(() => indexStarted).toBe(true);
    api.handlers.set(`GET ${runPath()}`, async (route) => { await json(route, { Run: detail(terminal) }); });
    await panel(page).getByRole('button', { name: 'Refresh run', exact: true }).click();
    await expect(history(page).getByText('Cancelled', { exact: true })).toBeVisible();
  } finally { releaseIndex(); }
  await indexResponse;
  await expect(panel(page).getByRole('button', { name: 'Refresh diagnostics', exact: true })).toBeEnabled();
  await expect(history(page).getByText('Cancelled', { exact: true })).toBeVisible();
  await expect(history(page).getByText('Running', { exact: true })).toHaveCount(0);
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
  await panel(page).getByRole('button', { name: 'Cancel run', exact: true }).click();
  await expect(history(page).getByText('Cancelled', { exact: true })).toBeVisible();
  await expect(panel(page).getByRole('button', { name: 'Cancel run', exact: true })).toHaveCount(0);
  await expect(panel(page).getByText(/No stage results have been reported/)).toBeVisible();

  api.handlers.set(`GET ${runPath()}`, async (route) => {
    await json(route, { Error: { Code: 'synthetic_read_unavailable', Message: 'Synthetic diagnostic read is unavailable.' }, RequestId: 'synthetic-read' }, 503);
  });
  await panel(page).getByRole('button', { name: 'Refresh run', exact: true }).click();
  await expect(panel(page).getByRole('alert').filter({ hasText: 'Synthetic diagnostic read is unavailable.' })).toBeVisible();
  await expect(panel(page).getByText(/The last recorded result is retained; a read failure does not establish the run's outcome/)).toBeVisible();
  await expect(history(page).getByText('Cancelled', { exact: true })).toBeVisible();
  await expect(panel(page).getByText(/Software baseline · Created:.*diagnostic cancelled/)).toBeVisible();
  await expect(panel(page).getByRole('button', { name: 'Start diagnostic', exact: true })).toBeDisabled();
  expect(api.mutations()).toEqual([{ method: 'POST', path: `${runPath()}/cancel`, body: '{}', csrf: csrfToken }]);
});
