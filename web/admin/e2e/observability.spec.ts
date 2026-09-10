import { constants } from 'node:fs';
import { lstat, open, realpath } from 'node:fs/promises';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import type { APIResponse, BrowserContext, Page, Request, Response, Route } from '@playwright/test';
import type { ActivityResponse, LoginSession, ServerLogFile, ServerLogLinesResponse, ServerLogsResponse } from '../src/api';

interface ObservabilityFixture {
  Marker: string;
  RunId: string;
  Origin: string;
  Administrator: { Id: string; Name: string };
  Activity: { Action: 'settings.updated'; ActorId: string; MinimumCount: number };
  Diagnostics: { MinimumFiles: number; Sentinels: string[] };
}

interface PrivateResult {
  Marker: string;
  RunId: string;
  Complete: boolean;
  BrowserSessionId?: string;
  BrowserCookie?: string;
  BrowserCSRF?: string;
  BrowserSecrets: string[];
  Checks: Record<string, boolean>;
}

const fixturePattern = /^\/opt\/goby-test\/exec-scratch\/goby-observability-([0-9]{8}_[0-9]{6}_[0-9a-f]{10})\/browser\/observability-fixture\.json$/;
const activityURL = /\/admin\/v1\/activity\?/;
const logsURL = /\/admin\/v1\/logs\?/;

function parseJSON<T>(text: string): T {
  try { return JSON.parse(text) as T; }
  catch { throw new Error('The response was not valid JSON. Private response content was omitted.'); }
}

function assertNoSecrets(text: string, secrets: string[]) {
  expect(secrets.some((secret) => text.includes(secret) || text.includes(JSON.stringify(secret).slice(1, -1))),
    'Diagnostic evidence must not contain credentials or the private request sentinels.').toBe(false);
}

async function loadFixture(filename: string, baseURL: string): Promise<ObservabilityFixture> {
  const match = fixturePattern.exec(filename);
  if (process.platform !== 'linux' || process.getuid?.() !== 0 || !match
    || filename !== path.resolve(filename) || await realpath(filename) !== filename) {
    throw new Error('Observability verification requires the isolated Linux runner and its canonical private manifest.');
  }
  for (const directory of [path.dirname(filename), path.dirname(path.dirname(filename))]) {
    const status = await lstat(directory);
    if (!status.isDirectory() || status.isSymbolicLink() || status.uid !== 0 || (status.mode & 0o777) !== 0o700) {
      throw new Error('The observability fixture parent directories must be private and root owned.');
    }
  }
  const file = await open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  let fixture: ObservabilityFixture;
  try {
    const status = await file.stat();
    if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o600 || status.nlink !== 1
      || status.size > 64 * 1024 || await realpath(`/proc/self/fd/${file.fd}`) !== filename) {
      throw new Error('The observability fixture must be a bounded private regular file.');
    }
    fixture = parseJSON<ObservabilityFixture>(await file.readFile('utf8'));
  } finally { await file.close(); }
  const origin = new URL(baseURL);
  if (!fixture || fixture.Marker !== 'goby-observability-browser-fixtures-v1'
    || fixture.RunId !== match[1] || fixture.RunId !== process.env.GOBY_OBSERVABILITY_RUN_ID
    || fixture.Origin !== origin.origin || origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || !origin.port
    || ['5432', '15432', '18096', '18097'].includes(origin.port)
    || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash
    || !fixture.Administrator || !/^[0-9a-f]{32}$/.test(fixture.Administrator.Id)
    || typeof fixture.Administrator.Name !== 'string' || !fixture.Administrator.Name.includes(fixture.RunId)
    || fixture.Activity?.Action !== 'settings.updated' || fixture.Activity.ActorId !== fixture.Administrator.Id
    || !Number.isSafeInteger(fixture.Activity.MinimumCount) || fixture.Activity.MinimumCount < 61
    || !fixture.Diagnostics || !Number.isSafeInteger(fixture.Diagnostics.MinimumFiles) || fixture.Diagnostics.MinimumFiles < 2
    || !Array.isArray(fixture.Diagnostics.Sentinels) || fixture.Diagnostics.Sentinels.length === 0
    || !fixture.Diagnostics.Sentinels.every((value) => typeof value === 'string' && value.length >= 16)) {
    throw new Error('The observability fixture does not match the disposable run contract.');
  }
  return fixture;
}

async function saveResult(filename: string, value: PrivateResult, create = false) {
  const file = await open(filename, constants.O_WRONLY | constants.O_NOFOLLOW | (create ? constants.O_CREAT | constants.O_EXCL : 0), 0o600);
  try {
    const status = await file.stat();
    if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o600 || status.nlink !== 1
      || await realpath(`/proc/self/fd/${file.fd}`) !== filename) throw new Error('The browser result must remain a private regular file.');
    await file.truncate(0);
    await file.writeFile(`${JSON.stringify(value)}\n`, 'utf8');
    await file.sync();
  } finally { await file.close(); }
}

function responseFor(page: Page, pathname: string, parameters: Record<string, string> = {}): Promise<Response> {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === 'GET' && url.pathname === pathname
      && Object.entries(parameters).every(([name, value]) => url.searchParams.get(name) === value);
  });
}

async function readPayload<T>(response: APIResponse | Response, secrets: string[]): Promise<T> {
  expect(response.status()).toBe(200);
  expect(response.headers()['cache-control']).toBe('no-store');
  const raw = await response.text();
  assertNoSecrets(raw, secrets);
  return parseJSON<T>(raw);
}

async function readActivity(response: APIResponse | Response, secrets: string[]): Promise<ActivityResponse> {
  const value = await readPayload<ActivityResponse>(response, secrets);
  expect(Object.keys(value).sort()).toEqual(['Items', 'Limit', 'RetentionDays', 'StartIndex', 'TotalRecordCount']);
  expect(Number.isSafeInteger(value.TotalRecordCount) && value.TotalRecordCount >= 0).toBe(true);
  expect(value.Items.length).toBe(Math.min(value.Limit, Math.max(0, value.TotalRecordCount - value.StartIndex)));
  expect(new Set(value.Items.map((item) => item.Id)).size).toBe(value.Items.length);
  for (const item of value.Items) {
    expect(item.Id).toMatch(/^[1-9][0-9]*$/);
    expect(item.Count).toMatch(/^(0|[1-9][0-9]*)$/);
    expect(item.Revision === null || typeof item.Revision === 'string' && /^[1-9][0-9]*$/.test(item.Revision)).toBe(true);
    expect(Number.isFinite(Date.parse(item.Date))).toBe(true);
    expect(['native', 'emby', 'system']).toContain(item.Source);
    expect(['Info', 'Debug', 'Warn', 'Error', 'Fatal']).toContain(item.Severity);
    expect(Object.keys(item.Actor).sort()).toEqual(['Id', 'Kind', 'Name']);
    expect(Object.keys(item.Resource).sort()).toEqual(['Id', 'Kind']);
    expect(item.ChangedFields.every((field) => typeof field === 'string')).toBe(true);
  }
  return value;
}

async function visibleActivity(page: Page, value: ActivityResponse) {
  await expect(page.getByRole('status', { name: 'Loading activity', exact: true })).toHaveCount(0);
  if (value.Items.length === 0) return;
  if ((page.viewportSize()?.width ?? 1440) >= 1200) {
    const table = page.getByRole('table', { name: 'Activity records', exact: true });
    const controls = table.getByRole('button', { name: /^Details for activity / });
    await expect(controls).toHaveCount(value.Items.length);
    expect(await controls.evaluateAll((elements) => elements.map((element) => element.getAttribute('aria-label')?.replace('Details for activity ', ''))))
      .toEqual(value.Items.map((item) => item.Id));
  } else {
    await expect(page.getByRole('list', { name: 'Activity records', exact: true }).getByRole('listitem')).toHaveCount(value.Items.length);
  }
}

async function choose(page: Page, label: string, option: string) {
  await page.getByRole('combobox', { name: label, exact: true }).click();
  await page.getByRole('option', { name: option, exact: true }).click();
}

async function applyFilters(page: Page, secrets: string[], parameters: Record<string, string> = {}) {
  const pending = responseFor(page, '/admin/v1/activity', { StartIndex: '0', ...parameters });
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  const response = await pending;
  const value = await readActivity(response, secrets);
  await visibleActivity(page, value);
  return { value, response };
}

async function clearFilters(page: Page, secrets: string[]) {
  const pending = responseFor(page, '/admin/v1/activity', { StartIndex: '0' });
  await page.getByRole('button', { name: 'Clear filters', exact: true }).first().click();
  const value = await readActivity(await pending, secrets);
  await visibleActivity(page, value);
  return value;
}

function assertJSONLines(lines: string[], secrets: string[]) {
  for (const line of lines) {
    assertNoSecrets(line, secrets);
    expect(Buffer.byteLength(line, 'utf8')).toBeLessThanOrEqual(8192);
    const value = parseJSON<unknown>(line);
    expect(typeof value === 'object' && value !== null && !Array.isArray(value), 'Each diagnostic line must be a JSON object.').toBe(true);
  }
}

async function readLogs(response: APIResponse | Response, secrets: string[]): Promise<ServerLogsResponse> {
  const value = await readPayload<ServerLogsResponse>(response, secrets);
  expect(Object.keys(value).sort()).toEqual(['Items', 'Limit', 'StartIndex', 'Status', 'TotalRecordCount']);
  expect(value.Items.length).toBe(Math.min(value.Limit, Math.max(0, value.TotalRecordCount - value.StartIndex)));
  expect(value.Status.Format).toBe('jsonl');
  expect(value.Status.MaxFileBytes).toMatch(/^[1-9][0-9]*$/);
  expect(value.Status.MinFreeBytes).toMatch(/^(0|[1-9][0-9]*)$/);
  expect(value.Status.RetentionDays).toBeGreaterThan(0);
  expect(new Set(value.Items.map((file) => file.Name)).size).toBe(value.Items.length);
  for (const file of value.Items) {
    expect(typeof file.Name === 'string' && file.Name.length > 0 && !/[\\/\u0000-\u001f]/.test(file.Name)).toBe(true);
    expect(file.Size).toMatch(/^(0|[1-9][0-9]*)$/);
    expect(Number.isFinite(Date.parse(file.DateCreated)) && Number.isFinite(Date.parse(file.DateModified))).toBe(true);
  }
  return value;
}

function newestReadableFile(files: ServerLogFile[]): ServerLogFile {
  const file = files.filter((item) => BigInt(item.Size) > 0n)
    .sort((a, b) => Date.parse(b.DateCreated) - Date.parse(a.DateCreated) || b.Name.localeCompare(a.Name))[0];
  if (!file) throw new Error('The real server did not provide a readable diagnostic file.');
  return file;
}

async function realFileForPaging(context: BrowserContext, files: ServerLogFile[], secrets: string[]): Promise<ServerLogFile> {
  const candidates = files.filter((item) => BigInt(item.Size) > 0n)
    .sort((a, b) => Date.parse(b.DateCreated) - Date.parse(a.DateCreated) || b.Name.localeCompare(a.Name));
  for (const file of candidates) {
    const response = await context.request.get(`/admin/v1/logs/${encodeURIComponent(file.Name)}/lines?StartIndex=0&Limit=10`);
    if (response.status() === 404) continue;
    const lines = await readLines(response, secrets);
    if (lines.TotalRecordCount > 10) return file;
  }
  throw new Error('The rotated real logs must include a retained file with more than ten lines for browser pagination.');
}

async function readLines(response: APIResponse | Response, secrets: string[]): Promise<ServerLogLinesResponse> {
  const value = await readPayload<ServerLogLinesResponse>(response, secrets);
  expect(Object.keys(value).sort()).toEqual(['Items', 'NextIndex', 'SnapshotSize', 'StartIndex', 'TotalRecordCount']);
  expect(value.NextIndex).toBe(value.StartIndex + value.Items.length);
  expect(value.Items.length).toBeLessThanOrEqual(Math.max(0, value.TotalRecordCount - value.StartIndex));
  expect(value.SnapshotSize).toMatch(/^(0|[1-9][0-9]*)$/);
  assertJSONLines(value.Items, secrets);
  return value;
}

async function visibleLines(page: Page, value: ServerLogLinesResponse) {
  await expect(page.getByRole('status', { name: 'Loading log lines', exact: true })).toHaveCount(0);
  if (value.Items.length) await expect(page.getByLabel('Log text', { exact: true })).toHaveText(value.Items.join('\n'));
}

async function checkSecretsAndLayout(page: Page, secrets: string[]) {
  const snapshot = await page.evaluate(() => ({
    dom: document.documentElement.outerHTML, text: document.body.innerText,
    values: [...document.querySelectorAll('input, textarea')].map((element) => (element as HTMLInputElement).value),
    local: JSON.stringify(localStorage), session: JSON.stringify(sessionStorage), url: location.href,
  }));
  assertNoSecrets(JSON.stringify(snapshot), secrets);
  expect(/postgres(?:ql)?:\/\/|\/dev\/shm\/|\/opt\/goby-test\//.test(JSON.stringify(snapshot)), 'The page must exclude connection strings and deployment paths.').toBe(false);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await expect(page.locator('#main-content')).toBeVisible();
  expect(await page.locator('#main-content').evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
}

async function rememberSession(context: BrowserContext, result: PrivateResult, secrets: string[]) {
  const response = await context.request.get('/admin/v1/sessions?Kind=admin&Status=active&StartIndex=0&Limit=200');
  const sessions = await readPayload<{ Items: LoginSession[] }>(response, secrets);
  const current = sessions.Items.filter((item) => item.IsCurrent);
  expect(current).toHaveLength(1);
  result.BrowserSessionId = current[0].Id;
}

// The runner owns the disposable database and private credentials. Successful
// records and downloads come from its real server; only recovery states are
// injected. Explicit screenshots are captured after secret and layout checks.
test.use({ screenshot: 'off', trace: 'off', video: 'off' });

test('isolated activity, safe diagnostic files, native downloads, and read-only recovery', async ({ page, context }, testInfo) => {
  test.skip(process.env.GOBY_SMOKE_OBSERVABILITY_DISPOSABLE_DATABASE !== '1'
    || process.env.GOBY_SMOKE_OBSERVABILITY_DEDICATED_ADMIN !== '1',
  'The observability runner must confirm its disposable database and dedicated administrator.');
  test.setTimeout(300_000);
  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  const baseURL = process.env.GOBY_SMOKE_BASE_URL;
  const manifestPath = process.env.GOBY_SMOKE_OBSERVABILITY_FIXTURE;
  if (!name || !password || !baseURL || !manifestPath) throw new Error('The private observability runner environment is incomplete.');
  const fixture = await loadFixture(manifestPath, baseURL);
  expect(new URL(testInfo.project.use.baseURL ?? '').origin).toBe(fixture.Origin);
  expect(name).toBe(fixture.Administrator.Name);
  const resultPath = path.join(path.dirname(manifestPath), 'observability-result.json');
  const result: PrivateResult = { Marker: 'goby-observability-result-v1', RunId: fixture.RunId, Complete: false, BrowserSecrets: [], Checks: {} };
  const secrets = [password, ...fixture.Diagnostics.Sentinels];
  await saveResult(resultPath, result, true);
  let pageErrorCount = 0;
  let browserMutationCount = 0;
  let activePageRoutes = 0;
  const browserURLs: string[] = [];
  page.on('pageerror', () => { pageErrorCount += 1; });
  page.on('request', (request) => {
    const url = new URL(request.url());
    browserURLs.push(request.url());
    if (['POST', 'PUT', 'DELETE', 'PATCH'].includes(request.method()) && url.pathname.startsWith('/admin/v1/') && url.pathname !== '/admin/v1/session') browserMutationCount += 1;
  });
  const capture = async (filename: string, fullPage = false) => {
    await checkSecretsAndLayout(page, secrets);
    await page.screenshot({ path: testInfo.outputPath(filename), fullPage, animations: 'disabled' });
  };

  try {
    await page.goto('/admin/observability');
    await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    await page.getByLabel(/^Username/).fill(name);
    await page.getByLabel(/^Password/).fill(password);
    const initialResponse = responseFor(page, '/admin/v1/activity', { StartIndex: '0', Limit: '50' });
    const loginResponse = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/admin/v1/session');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    const signedIn = await loginResponse;
    const session = parseJSON<{ CSRFToken?: unknown; User?: { Id?: string } }>(await signedIn.text());
    const cookie = (await context.cookies()).find((value) => value.name === 'goby_session');
    if (typeof session.CSRFToken === 'string' && session.CSRFToken) {
      result.BrowserCSRF = session.CSRFToken;
      result.BrowserSecrets.push(session.CSRFToken);
    }
    if (cookie) {
      result.BrowserCookie = `goby_session=${cookie.value}`;
      result.BrowserSecrets.push(cookie.value, result.BrowserCookie);
    }
    secrets.push(...result.BrowserSecrets);
    await saveResult(resultPath, result);
    expect(signedIn.status()).toBe(200);
    if (!result.BrowserCSRF || !result.BrowserCookie) throw new Error('The browser did not receive its independent native credentials.');
    expect(session.User?.Id).toBe(fixture.Administrator.Id);
    expect(cookie?.httpOnly).toBe(true);
    expect(cookie?.sameSite).toBe('Strict');
    await rememberSession(context, result, secrets);
    await saveResult(resultPath, result);
    let activity = await readActivity(await initialResponse, secrets);
    await visibleActivity(page, activity);
    await expect(page.getByRole('heading', { name: 'Activity & logs', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Activity & logs', exact: true })).toHaveAttribute('href', '/admin/observability');
    await expect(page.getByRole('link', { name: 'Activity & logs', exact: true })).toHaveAttribute('aria-current', 'page');
    await expect(page.getByRole('tab', { name: 'Activity', exact: true })).toHaveAttribute('aria-selected', 'true');

    await test.step('read real settings activity through filters, stable paging, and manual refresh', async () => {
      await choose(page, 'Action', 'Settings updated');
      activity = (await applyFilters(page, secrets, { Action: fixture.Activity.Action })).value;
      expect(activity.TotalRecordCount).toBeGreaterThanOrEqual(fixture.Activity.MinimumCount);
      expect(activity.Items.every((item) => item.Action === fixture.Activity.Action)).toBe(true);
      const actorResponse = responseFor(page, '/admin/v1/activity', { ActorId: fixture.Activity.ActorId, Action: fixture.Activity.Action, StartIndex: '0' });
      await page.getByRole('table', { name: 'Activity records', exact: true }).getByRole('button', { name: `Filter by actor ${fixture.Activity.ActorId}`, exact: true }).first().click();
      activity = await readActivity(await actorResponse, secrets);
      await visibleActivity(page, activity);
      expect(activity.Items.every((item) => item.Actor.Id === fixture.Activity.ActorId)).toBe(true);
      await choose(page, 'Severity', 'Info');
      await choose(page, 'Date range', 'Last 24 hours');
      const filtered = await applyFilters(page, secrets, { Action: fixture.Activity.Action, ActorId: fixture.Activity.ActorId, Severity: 'Info' });
      activity = filtered.value;
      expect(activity.TotalRecordCount).toBeGreaterThanOrEqual(fixture.Activity.MinimumCount);
      const since = new URL(filtered.response.url()).searchParams.get('MinDate');
      expect(Boolean(since) && Number.isFinite(Date.parse(since!))).toBe(true);
      expect(activity.Items.every((item) => item.Action === fixture.Activity.Action && item.Actor.Id === fixture.Activity.ActorId && item.Severity === 'Info' && Date.parse(item.Date) >= Date.parse(since!))).toBe(true);
      const independent = await readActivity(await context.request.get(filtered.response.url()), secrets);
      expect(independent.Items.map((item) => item.Id)).toEqual(activity.Items.map((item) => item.Id));
      const firstIds = activity.Items.map((item) => item.Id);
      const nextResponse = responseFor(page, '/admin/v1/activity', { StartIndex: '50', Limit: '50', ActorId: fixture.Activity.ActorId });
      await page.getByRole('button', { name: 'Go to next page', exact: true }).click();
      const second = await readActivity(await nextResponse, secrets);
      await visibleActivity(page, second);
      expect(second.TotalRecordCount).toBe(activity.TotalRecordCount);
      expect(second.Items.some((item) => firstIds.includes(item.Id))).toBe(false);
      const previousResponse = responseFor(page, '/admin/v1/activity', { StartIndex: '0', Limit: '50' });
      await page.getByRole('button', { name: 'Go to previous page', exact: true }).click();
      const previous = await readActivity(await previousResponse, secrets);
      await visibleActivity(page, previous);
      expect(previous.Items.map((item) => item.Id)).toEqual(firstIds);
      const resizedResponse = responseFor(page, '/admin/v1/activity', { StartIndex: '0', Limit: '25' });
      await choose(page, 'Activities per page:', '25');
      activity = await readActivity(await resizedResponse, secrets);
      await visibleActivity(page, activity);
      expect(activity.Items.map((item) => item.Id)).toEqual(firstIds.slice(0, 25));
      const refreshResponse = responseFor(page, '/admin/v1/activity', { StartIndex: '0', Limit: '25', MinDate: since! });
      await page.getByRole('button', { name: 'Refresh activity', exact: true }).click();
      const refreshed = await readActivity(await refreshResponse, secrets);
      await visibleActivity(page, refreshed);
      expect(refreshed.Items.map((item) => item.Id)).toEqual(activity.Items.map((item) => item.Id));
      await expect(page.getByText(/Retention is managed by the server/)).toBeVisible();
      await capture('observability-activity-desktop.png');
      const details = page.getByRole('button', { name: `Details for activity ${activity.Items[0].Id}`, exact: true });
      await details.click();
      await expect(details).toHaveAttribute('aria-expanded', 'true');
      const detailsPanel = page.locator(`#activity-table-${activity.Items[0].Id}`);
      await expect(detailsPanel.getByText('Activity ID', { exact: true })).toBeVisible();
      await expect(detailsPanel.getByText(activity.Items[0].Id, { exact: true }).first()).toBeVisible();
      await expect(detailsPanel.getByText('Affected count', { exact: true })).toBeVisible();
      if (activity.Items[0].ChangedFields.length) await expect(detailsPanel.getByText('Changed fields', { exact: true })).toBeVisible();
      await capture('observability-details-desktop.png');
      await details.click();
      result.Checks.RealActivityFiltersPagingRefreshAndDetails = true;
    });

    await test.step('show a real empty date range and recover from a temporary activity read failure', async () => {
      await choose(page, 'Date range', 'Since a date and time');
      await page.getByLabel('Activity since', { exact: true }).fill('2099-01-01T00:00');
      const empty = await applyFilters(page, secrets, { Action: fixture.Activity.Action });
      expect(empty.value.TotalRecordCount).toBe(0);
      await expect(page.getByRole('heading', { name: 'No matching activity', exact: true })).toBeVisible();
      activity = await clearFilters(page, secrets);
      expect(activity.TotalRecordCount).toBeGreaterThanOrEqual(fixture.Activity.MinimumCount);
      const message = 'Activity storage is temporarily unavailable.';
      const intercept = async (route: Route) => route.fulfill({ status: 503, contentType: 'application/json', headers: { 'cache-control': 'no-store' }, body: JSON.stringify({ Error: { Code: 'activity_unavailable', Message: message } }) });
      await page.route(activityURL, intercept);
      activePageRoutes += 1;
      try {
        const failed = responseFor(page, '/admin/v1/activity');
        await page.getByRole('button', { name: 'Refresh activity', exact: true }).click();
        expect((await failed).status()).toBe(503);
        await expect(page.getByRole('alert').filter({ hasText: message })).toBeVisible();
        await expect(page.getByRole('table', { name: 'Activity records', exact: true })).toHaveCount(0);
        await expect(page.getByRole('heading', { name: 'No recorded activity', exact: true })).toHaveCount(0);
      } finally { await page.unroute(activityURL, intercept); activePageRoutes -= 1; }
      const recovered = responseFor(page, '/admin/v1/activity');
      await page.getByRole('button', { name: 'Retry', exact: true }).click();
      activity = await readActivity(await recovered, secrets);
      await visibleActivity(page, activity);
      result.Checks.RealEmptyActivityAndInjectedErrorRecovery = true;
    });

    await test.step('cancel an older real activity response when a newer filter wins', async () => {
      let heldRequest: Request | undefined;
      let cancelled = false;
      let handlerFailure = false;
      let fulfilFailure = false;
      let ready = false;
      let release!: () => void;
      let finished!: () => void;
      const releaseGate = new Promise<void>((resolve) => { release = resolve; });
      const handlerDone = new Promise<void>((resolve) => { finished = resolve; });
      const cancelledRequest = (request: Request) => { if (request === heldRequest && request.failure()?.errorText === 'net::ERR_ABORTED') cancelled = true; };
      page.on('requestfailed', cancelledRequest);
      const intercept = async (route: Route) => {
        if (heldRequest || new URL(route.request().url()).searchParams.get('Severity') !== 'Debug') { await route.continue(); return; }
        heldRequest = route.request();
        let response: APIResponse | undefined;
        try {
          response = await route.fetch({ maxRedirects: 0 });
          await readActivity(response, secrets);
          ready = true;
          await releaseGate;
          try { await route.fulfill({ response }); } catch { fulfilFailure = true; }
        } catch { handlerFailure = true; }
        finally {
          ready = true;
          try { await response?.dispose(); } finally { finished(); }
        }
      };
      const releaseTimeout = setTimeout(release, 20_000);
      await page.route(activityURL, intercept);
      activePageRoutes += 1;
      try {
        await choose(page, 'Severity', 'Debug');
        await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
        await expect.poll(() => ready, { timeout: 20_000 }).toBe(true);
        expect(handlerFailure).toBe(false);
        await expect(page.getByRole('status', { name: 'Loading activity', exact: true })).toBeVisible();
        await choose(page, 'Severity', 'Info');
        const current = await applyFilters(page, secrets, { Severity: 'Info' });
        expect(current.value.Items.length).toBeGreaterThan(0);
        expect(current.value.Items.every((item) => item.Severity === 'Info')).toBe(true);
        release();
        await handlerDone;
        await expect.poll(() => cancelled).toBe(true);
        expect(handlerFailure || fulfilFailure && !cancelled).toBe(false);
        await visibleActivity(page, current.value);
        activity = current.value;
        result.Checks.ObsoleteRealActivityResponseCancelled = true;
      } finally {
        clearTimeout(releaseTimeout);
        release();
        try { if (heldRequest) await handlerDone; }
        finally {
          try { await page.unroute(activityURL, intercept); activePageRoutes -= 1; }
          finally { page.off('requestfailed', cancelledRequest); }
        }
      }
    });

    let logs!: ServerLogsResponse;
    let selected!: ServerLogFile;
    await test.step('open real retained files and read a fixed log page', async () => {
      const pending = responseFor(page, '/admin/v1/logs', { StartIndex: '0', Limit: '50' });
      await page.getByRole('tab', { name: 'Server logs', exact: true }).click();
      logs = await readLogs(await pending, secrets);
      expect(logs.TotalRecordCount).toBeGreaterThanOrEqual(fixture.Diagnostics.MinimumFiles);
      await expect(page.getByRole('status', { name: 'Loading log files', exact: true })).toHaveCount(0);
      await expect(page.getByRole('table', { name: 'Server log files', exact: true }).getByRole('row')).toHaveCount(logs.Items.length + 1);
      await expect(page.getByText(/This policy is read-only/)).toBeVisible();
      await capture('observability-logs-desktop.png');
      selected = await realFileForPaging(context, logs.Items, secrets);
      const pathname = `/admin/v1/logs/${encodeURIComponent(selected.Name)}/lines`;
      const linesResponse = responseFor(page, pathname, { StartIndex: '0', Limit: '10' });
      const view = page.getByRole('button', { name: `View ${selected.Name}`, exact: true });
      await view.click();
      const lines = await readLines(await linesResponse, secrets);
      expect(lines.Items).toHaveLength(10);
      expect(lines.TotalRecordCount).toBeGreaterThan(10);
      await visibleLines(page, lines);
      await expect(page.getByRole('button', { name: 'Previous lines', exact: true })).toBeDisabled();
      const moreResponse = responseFor(page, pathname, { StartIndex: '10', Limit: '10' });
      await page.getByRole('button', { name: 'Read more', exact: true }).click();
      const more = await readLines(await moreResponse, secrets);
      expect(more.Items.length).toBeGreaterThan(0);
      await visibleLines(page, more);
      const previousResponse = responseFor(page, pathname, { StartIndex: '0', Limit: '10' });
      await page.getByRole('button', { name: 'Previous lines', exact: true }).click();
      const previous = await readLines(await previousResponse, secrets);
      await visibleLines(page, previous);
      expect(previous.Items).toEqual(lines.Items);
      for (const limit of ['50', '200']) {
        const resizedResponse = responseFor(page, pathname, { StartIndex: '0', Limit: limit });
        await choose(page, 'Lines per page', limit);
        const resized = await readLines(await resizedResponse, secrets);
        await visibleLines(page, resized);
        expect(resized.Items.slice(0, 10)).toEqual(lines.Items);
      }
      const reloadResponse = responseFor(page, pathname, { StartIndex: '0', Limit: '200' });
      await page.getByRole('button', { name: 'Reload log', exact: true }).click();
      const reloaded = await readLines(await reloadResponse, secrets);
      await visibleLines(page, reloaded);
      expect(reloaded.Items.slice(0, 10)).toEqual(lines.Items);
      await page.getByRole('button', { name: 'Close log preview', exact: true }).click();
      await expect(view).toBeFocused();
      await expect(page.getByLabel('Log text', { exact: true })).toHaveCount(0);
      result.Checks.RealLogPreviewReadMore = true;
      result.Checks.RealLogFilesPreviewPagingReloadAndCloseFocus = true;
    });

    await test.step('download native cookie-authenticated JSONL bytes without credentials in the URL', async () => {
      const link = page.getByRole('link', { name: `Download ${selected.Name}`, exact: true });
      const pathname = `/admin/v1/logs/${encodeURIComponent(selected.Name)}/download`;
      await expect(link).toHaveAttribute('href', pathname);
      expect(activePageRoutes, 'Temporary Playwright routes must be removed before observing the native download.').toBe(0);
      // Chromium downloads use a dedicated URLLoaderFactory. Fetch can observe
      // requests without networkId, which Playwright does not expose as Request
      // events. Observe this one native request and resume it without overrides.
      const cdp = await context.newCDPSession(page);
      const paused = new Set<string>();
      const continuations = new Set<Promise<void>>();
      const observations: { matchingURL: boolean; get: boolean; query: boolean; urlCredentials: boolean; authorization: boolean; tokenHeader: boolean }[] = [];
      let observerFailed = false;
      const observe = (event: { requestId: string; request: { url: string; method: string; headers: Record<string, unknown> } }) => {
        paused.add(event.requestId);
        try {
          const url = new URL(event.request.url);
          const names = Object.keys(event.request.headers).map((name) => name.toLowerCase());
          observations.push({
            matchingURL: url.origin === fixture.Origin && url.pathname === pathname,
            get: event.request.method === 'GET', query: Boolean(url.search), urlCredentials: Boolean(url.username || url.password),
            authorization: names.includes('authorization') || names.includes('proxy-authorization'),
            tokenHeader: names.some((name) => ['x-emby-token', 'x-mediabrowser-token', 'x-emby-authorization', 'x-csrf-token', 'x-api-key', 'x-access-token'].includes(name)),
          });
        } catch { observerFailed = true; }
        const continuation = cdp.send('Fetch.continueRequest', { requestId: event.requestId })
          .then(() => { paused.delete(event.requestId); })
          .catch(() => { observerFailed = true; });
        continuations.add(continuation);
        void continuation.then(() => { continuations.delete(continuation); });
      };
      cdp.on('Fetch.requestPaused', observe);
      try {
        await cdp.send('Fetch.enable', { patterns: [{ urlPattern: `${fixture.Origin}${pathname}`, requestStage: 'Request' }] });
        const pending = page.waitForEvent('download');
        await link.click();
        const download = await pending;
        try {
          assertNoSecrets(download.url(), secrets);
          const url = new URL(download.url());
          expect(url.origin).toBe(fixture.Origin);
          expect(url.pathname).toBe(pathname);
          expect(url.search).toBe('');
          expect(Boolean(url.username || url.password)).toBe(false);
          expect(download.suggestedFilename()).toBe(selected.Name);
          expect(await download.failure()).toBeNull();
          const stream = await download.createReadStream();
          const chunks: Buffer[] = [];
          let size = 0;
          for await (const chunk of stream) {
            const bytes = Buffer.from(chunk);
            size += bytes.length;
            if (size > 64 * 1024 * 1024) { stream.destroy(); throw new Error('The diagnostic download exceeded its bounded size.'); }
            chunks.push(bytes);
          }
          const bytes = Buffer.concat(chunks);
          expect(bytes.length).toBeGreaterThan(0);
          let text: string;
          try { text = new TextDecoder('utf-8', { fatal: true }).decode(bytes); }
          catch { throw new Error('The diagnostic download was not valid UTF-8.'); }
          assertNoSecrets(text, secrets);
          expect(text.endsWith('\n')).toBe(true);
          assertJSONLines(text.slice(0, -1).split('\n'), secrets);
          expect(observations.length, 'The actual native download must be observed by Chromium Fetch.').toBeGreaterThan(0);
          expect(observations.every((request) => request.matchingURL && request.get && !request.query && !request.urlCredentials && !request.authorization && !request.tokenHeader)).toBe(true);
        } finally { await download.delete(); }
      } finally {
        try {
          await Promise.all([...continuations]);
          for (const requestId of paused) {
            try { await cdp.send('Fetch.continueRequest', { requestId }); }
            catch { observerFailed = true; }
          }
        } finally {
          try { await cdp.send('Fetch.disable'); } catch { observerFailed = true; }
          cdp.off('Fetch.requestPaused', observe);
          try { await cdp.detach(); } catch { observerFailed = true; }
        }
        if (observerFailed) throw new Error('The native download request observer failed to observe or resume a request, or to clean up its CDP session.');
      }
      result.Checks.ActualNativeCookieDownloadAndRedactedJSONL = true;
    });

    await test.step('recover missing files and unavailable storage without showing fake empty results', async () => {
      const pathname = `/admin/v1/logs/${encodeURIComponent(selected.Name)}/lines`;
      const pending = responseFor(page, pathname, { StartIndex: '0' });
      await page.getByRole('button', { name: `View ${selected.Name}`, exact: true }).click();
      await visibleLines(page, await readLines(await pending, secrets));
      const linesURL = new RegExp(`${pathname.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\?`);
      const missing = async (route: Route) => route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ Error: { Code: 'log_not_found', Message: 'The requested log file is no longer available.' } }) });
      await page.route(linesURL, missing);
      activePageRoutes += 1;
      try {
        const failed = responseFor(page, pathname);
        await page.getByRole('button', { name: 'Reload log', exact: true }).click();
        expect((await failed).status()).toBe(404);
        await expect(page.getByRole('alert').filter({ hasText: 'This log file is no longer available.' })).toBeVisible();
        await expect(page.getByLabel('Log text', { exact: true })).toHaveCount(0);
      } finally { await page.unroute(linesURL, missing); activePageRoutes -= 1; }
      const refreshed = responseFor(page, '/admin/v1/logs');
      await page.getByRole('alert').filter({ hasText: 'This log file is no longer available.' }).getByRole('button', { name: 'Refresh files', exact: true }).click();
      logs = await readLogs(await refreshed, secrets);
      await expect(page.getByRole('button', { name: 'Close log preview', exact: true })).toHaveCount(0);
      const message = 'Diagnostic storage is temporarily unavailable.';
      const unavailable = async (route: Route) => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ Error: { Code: 'diagnostics_unavailable', Message: message } }) });
      await page.route(logsURL, unavailable);
      activePageRoutes += 1;
      try {
        const failed = responseFor(page, '/admin/v1/logs');
        await page.getByRole('button', { name: 'Refresh files', exact: true }).click();
        expect((await failed).status()).toBe(503);
        await expect(page.getByRole('alert').filter({ hasText: message })).toBeVisible();
        await expect(page.getByRole('table', { name: 'Server log files', exact: true })).toHaveCount(0);
        await expect(page.getByRole('heading', { name: 'No log files', exact: true })).toHaveCount(0);
      } finally { await page.unroute(logsURL, unavailable); activePageRoutes -= 1; }
      const recovered = responseFor(page, '/admin/v1/logs');
      await page.getByRole('button', { name: 'Retry', exact: true }).click();
      logs = await readLogs(await recovered, secrets);
      await expect(page.getByRole('table', { name: 'Server log files', exact: true })).toBeVisible();
      result.Checks.InjectedMissingFileAndUnavailableStorageRecovery = true;
    });

    await test.step('read logs on mobile with no overflow or sensitive content', async () => {
      await page.setViewportSize({ width: 390, height: 844 });
      await expect(page.getByRole('list', { name: 'Server log files', exact: true }).getByRole('listitem')).toHaveCount(logs.Items.length);
      selected = newestReadableFile(logs.Items);
      const pending = responseFor(page, `/admin/v1/logs/${encodeURIComponent(selected.Name)}/lines`, { StartIndex: '0', Limit: '10' });
      await page.getByRole('button', { name: `View ${selected.Name}`, exact: true }).click();
      await visibleLines(page, await readLines(await pending, secrets));
      await capture('observability-mobile.png', true);
      await page.getByRole('button', { name: 'Close log preview', exact: true }).click();
      await expect(page.getByRole('button', { name: `View ${selected.Name}`, exact: true })).toBeFocused();
      const activityResponse = responseFor(page, '/admin/v1/activity', { StartIndex: '0', Limit: '50' });
      await page.getByRole('tab', { name: 'Activity', exact: true }).click();
      await visibleActivity(page, await readActivity(await activityResponse, secrets));
      await checkSecretsAndLayout(page, secrets);
      const logsResponse = responseFor(page, '/admin/v1/logs', { StartIndex: '0', Limit: '50' });
      await page.getByRole('tab', { name: 'Server logs', exact: true }).click();
      logs = await readLogs(await logsResponse, secrets);
      selected = newestReadableFile(logs.Items);
      const linesResponse = responseFor(page, `/admin/v1/logs/${encodeURIComponent(selected.Name)}/lines`, { StartIndex: '0', Limit: '10' });
      await page.getByRole('button', { name: `View ${selected.Name}`, exact: true }).click();
      await visibleLines(page, await readLines(await linesResponse, secrets));
      result.Checks.MobileActivityAndLogsWithoutOverflow = true;
    });

    await test.step('clear retained administrator content after the browser credential is revoked', async () => {
      await saveResult(resultPath, result);
      if (!result.BrowserSessionId || !result.BrowserCSRF) throw new Error('The browser credential identity was not retained for self-revocation.');
      const revoked = await context.request.post(`/admin/v1/sessions/${encodeURIComponent(result.BrowserSessionId)}/revoke`, {
        headers: { Origin: fixture.Origin, 'X-CSRF-Token': result.BrowserCSRF }, data: {},
      });
      expect(revoked.status()).toBe(200);
      const raw = await revoked.text();
      assertNoSecrets(raw, secrets);
      const value = parseJSON<{ SessionId: string; CurrentSessionRevoked: boolean }>(raw);
      expect(value.SessionId).toBe(result.BrowserSessionId);
      expect(value.CurrentSessionRevoked).toBe(true);
      expect((await context.cookies()).some((cookie) => cookie.name === 'goby_session')).toBe(false);
      const expired = responseFor(page, `/admin/v1/logs/${encodeURIComponent(selected.Name)}/lines`, { StartIndex: '0' });
      await page.getByRole('button', { name: 'Reload log', exact: true }).click();
      expect((await expired).status()).toBe(401);
      await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Activity & logs', exact: true })).toHaveCount(0);
      await expect(page.getByRole('tabpanel')).toHaveCount(0);
      await expect(page.getByLabel('Log text', { exact: true })).toHaveCount(0);
      await expect(page.getByRole('button', { name: 'Close log preview', exact: true })).toHaveCount(0);
      expect((await context.request.get('/admin/v1/activity')).status()).toBe(401);
      result.Checks.RealBrowserRevocationClearsActivityAndLogContent = true;
    });
    assertNoSecrets(JSON.stringify(browserURLs), secrets);
    expect(browserMutationCount, 'The observability page must remain read-only.').toBe(0);
    expect(pageErrorCount, 'The browser must not report uncaught application errors.').toBe(0);
    result.Checks.ReadOnlyBrowserRequests = true;
    result.Checks.CredentialFreeURLsAndScreenshots = true;
    result.Complete = true;
  } finally {
    // Only this private result contains cleanup credentials. The runner owns
    // revocation auditing, safe evidence projection, and disposal of this file.
    await saveResult(resultPath, result);
  }
});
