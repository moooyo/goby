import { constants } from 'node:fs';
import { lstat, open, realpath } from 'node:fs/promises';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import type { APIRequestContext, Locator, Page, Response } from '@playwright/test';

interface Device {
  Id: string;
  Revision: string;
  ReportedDeviceId: string;
  Name: string;
  ReportedName: string;
  CustomName: string | null;
  AppName: string;
  AppVersion: string;
  LastUserId: string | null;
  LastUserName: string | null;
  CreatedAt: string;
  LastSeenAt: string;
  IpAddress: string;
  ActiveLoginCount: number;
}

interface DevicePage {
  Items: Device[];
  TotalRecordCount: number;
  StartIndex: number;
  Limit: number;
}

interface FixtureLogin {
  Id: string;
  SessionId: string;
  Token: string;
  ReportedDeviceId: string;
  ReportedName: string;
  AppName: string;
  AppVersion: string;
}

interface DeviceFixture {
  Marker: string;
  RunId: string;
  Origin: string;
  Administrator: { Id: string; Name: string };
  Member: { Id: string; Name: string; Password: string };
  SeedCount: number;
  Devices: FixtureLogin[];
  Target: FixtureLogin;
  SecondTarget: FixtureLogin;
  Sibling: FixtureLogin;
  DashboardCollision: FixtureLogin;
  Missing: FixtureLogin;
  LiteralName: string;
  LiteralSearch: string;
  PersistedName: string;
}

interface RemovedDevice {
  Id: string;
  Revision: string;
  DeletedAt: string;
  RevokedLoginCount: number;
}

interface PrivateResult {
  Marker: string;
  RunId: string;
  Complete: boolean;
  Removed: RemovedDevice[];
  BrowserSecrets?: string[];
  BrowserCookie?: string;
  BrowserSessionId?: string;
  ReplacementLogin?: Partial<FixtureLogin>;
  PersistedRevision?: string;
  ExpectedCount?: number;
  ExpectedDeviceIds?: string[];
}

const fixtureMarker = 'goby-device-browser-fixtures-v1';
const fixturePattern = /^\/opt\/goby-test\/exec-scratch\/goby-devices-([0-9]{8}_[0-9]{6}_[0-9a-f]{10})\/browser\/device-fixture\.json$/;
const safeFields = ['Id', 'Revision', 'ReportedDeviceId', 'Name', 'ReportedName', 'CustomName', 'AppName',
  'AppVersion', 'LastUserId', 'LastUserName', 'CreatedAt', 'LastSeenAt', 'IpAddress', 'ActiveLoginCount'].sort();

async function loadFixture(filename: string, baseURL: string): Promise<DeviceFixture> {
  const match = fixturePattern.exec(filename);
  if (process.platform !== 'linux' || process.getuid?.() !== 0 || !match
    || filename !== path.resolve(filename) || await realpath(filename) !== filename) {
    throw new Error('Device fixtures require the isolated Linux runner and its canonical private manifest.');
  }
  for (const directory of [path.dirname(filename), path.dirname(path.dirname(filename))]) {
    const status = await lstat(directory);
    if (!status.isDirectory() || status.isSymbolicLink() || status.uid !== 0 || (status.mode & 0o777) !== 0o700) {
      throw new Error('Device fixture directories must be private and root owned.');
    }
  }
  const file = await open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  let fixture: DeviceFixture;
  try {
    const status = await file.stat();
    if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o600 || status.nlink !== 1
      || status.size > 64 * 1024 || await realpath(`/proc/self/fd/${file.fd}`) !== filename) {
      throw new Error('The device fixture must be a bounded private regular file.');
    }
    fixture = JSON.parse(await file.readFile('utf8')) as DeviceFixture;
  } finally {
    await file.close();
  }
  const origin = new URL(baseURL);
  const validLogin = (login: FixtureLogin) => login && /^[1-9][0-9]*$/.test(login.Id)
    && /^[0-9a-f]{32}$/.test(login.SessionId) && typeof login.Token === 'string' && login.Token.length >= 32
    && [login.ReportedDeviceId, login.ReportedName, login.AppName, login.AppVersion]
      .every((value) => typeof value === 'string' && value.length > 0);
  if (!fixture || fixture.Marker !== fixtureMarker || fixture.RunId !== match[1]
    || fixture.RunId !== process.env.GOBY_DEVICES_RUN_ID || fixture.Origin !== origin.origin
    || origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || !origin.port
    || ['5432', '15432', '18096', '18097'].includes(origin.port)
    || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash
    || ![fixture.Administrator, fixture.Member].every((user) => user && /^[0-9a-f]{32}$/.test(user.Id)
      && typeof user.Name === 'string' && user.Name.includes(fixture.RunId))
    || typeof fixture.Member.Password !== 'string' || fixture.Member.Password.length < 32
    || fixture.SeedCount !== 32 || !Array.isArray(fixture.Devices) || fixture.Devices.length !== 32
    || !fixture.Devices.every(validLogin) || new Set(fixture.Devices.map((login) => login.Id)).size !== 32
    || new Set(fixture.Devices.map((login) => login.ReportedDeviceId)).size !== 32
    || ![fixture.Target, fixture.SecondTarget, fixture.Sibling, fixture.DashboardCollision, fixture.Missing].every(validLogin)
    || fixture.Target.Id !== fixture.SecondTarget.Id || fixture.Target.Token === fixture.SecondTarget.Token
    || fixture.Target.SessionId === fixture.SecondTarget.SessionId
    || fixture.Target.ReportedDeviceId !== fixture.SecondTarget.ReportedDeviceId
    || fixture.DashboardCollision.ReportedDeviceId !== 'goby-dashboard'
    || fixture.LiteralName !== 'Archive 100%_\\ display' || fixture.LiteralSearch !== '100%_\\'
    || fixture.PersistedName !== 'Study \u754c player') {
    throw new Error('The private device fixture does not match this disposable run.');
  }
  return fixture;
}

async function saveResult(filename: string, result: PrivateResult, create = false) {
  const flags = constants.O_WRONLY | constants.O_NOFOLLOW | (create ? constants.O_CREAT | constants.O_EXCL : 0);
  const file = await open(filename, flags, 0o600);
  try {
    const status = await file.stat();
    if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o600 || status.nlink !== 1
      || await realpath(`/proc/self/fd/${file.fd}`) !== filename) {
      throw new Error('The device browser result must remain a private regular file.');
    }
    await file.truncate(0);
    await file.writeFile(`${JSON.stringify(result)}\n`, 'utf8');
    await file.sync();
  } finally {
    await file.close();
  }
}

function deviceResponse(page: Page, parameters: Record<string, string> = {}) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === 'GET' && url.pathname === '/admin/v1/devices'
      && Object.entries(parameters).every(([name, value]) => url.searchParams.get(name) === value);
  });
}

function deviceMutation(page: Page, pathname: string) {
  return page.waitForResponse((response) => response.request().method() === 'POST'
    && new URL(response.url()).pathname === pathname);
}

function assertDevice(device: Device) {
  expect(Object.keys(device).sort()).toEqual(safeFields);
  expect(device.Id).toMatch(/^[1-9][0-9]*$/);
  expect(device.Revision).toMatch(/^[1-9][0-9]*$/);
  expect(device.ReportedDeviceId.length).toBeGreaterThan(0);
  expect(device.Name.length).toBeGreaterThan(0);
  expect([device.ReportedName, device.AppName, device.AppVersion, device.IpAddress].every((value) => typeof value === 'string')).toBe(true);
  expect(device.CustomName === null || (typeof device.CustomName === 'string' && device.Name === device.CustomName)).toBe(true);
  expect(device.LastUserId === null || /^[0-9a-f]{32}$/.test(device.LastUserId)).toBe(true);
  expect(device.LastUserName === null || typeof device.LastUserName === 'string').toBe(true);
  expect(device.CreatedAt).toMatch(/Z$/);
  expect(device.LastSeenAt).toMatch(/Z$/);
  expect(Number.isSafeInteger(device.ActiveLoginCount) && device.ActiveLoginCount >= 0).toBe(true);
}

async function readDevices(page: Page, response: Response, sensitive: string[], visible = true): Promise<DevicePage> {
  expect(response.status()).toBe(200);
  expect(response.headers()['cache-control']).toBe('no-store');
  const raw = await response.text();
  expect(sensitive.some((secret) => raw.includes(secret)), 'Device metadata must exclude raw authentication secrets.').toBe(false);
  const result = JSON.parse(raw) as DevicePage;
  expect(Object.keys(result).sort()).toEqual(['Items', 'Limit', 'StartIndex', 'TotalRecordCount']);
  expect(Array.isArray(result.Items)).toBe(true);
  expect(result.Items).toHaveLength(Math.min(result.Limit, Math.max(0, result.TotalRecordCount - result.StartIndex)));
  expect(new Set(result.Items.map((device) => device.Id)).size).toBe(result.Items.length);
  result.Items.forEach(assertDevice);
  if (visible) {
    await expect(page.getByRole('status', { name: 'Loading devices', exact: true })).not.toBeVisible();
    if (result.Items.length > 0) {
      if ((page.viewportSize()?.width ?? 1440) >= 1200) {
        await expect(page.getByRole('table', { name: 'Registered devices', exact: true }).getByRole('row')).toHaveCount(result.Items.length + 1);
      } else {
        await expect(page.getByRole('list', { name: 'Registered devices', exact: true }).getByRole('listitem')).toHaveCount(result.Items.length);
      }
    }
  }
  return result;
}

async function searchDevices(page: Page, text: string, sensitive: string[]) {
  await page.getByRole('textbox', { name: 'Search devices', exact: true }).fill(text);
  const response = deviceResponse(page, { SearchTerm: text, StartIndex: '0' });
  await page.getByRole('button', { name: 'Search devices', exact: true }).click();
  return readDevices(page, await response, sensitive);
}

async function clearSearch(page: Page, sensitive: string[]) {
  const response = deviceResponse(page, { StartIndex: '0', Limit: '25' });
  await page.getByRole('button', { name: 'Clear search', exact: true }).first().click();
  return readDevices(page, await response, sensitive);
}

function deviceRecord(page: Page, reportedId: string): Locator {
  const container = (page.viewportSize()?.width ?? 1440) >= 1200
    ? page.getByRole('table', { name: 'Registered devices', exact: true }).getByRole('row')
    : page.getByRole('list', { name: 'Registered devices', exact: true }).getByRole('listitem');
  return container.filter({ has: page.getByText(reportedId, { exact: true }) });
}

async function tokenStatus(request: APIRequestContext, fixture: DeviceFixture, token: string) {
  return (await request.get(`/emby/Users/${fixture.Member.Id}/Views`, { headers: { 'X-Emby-Token': token } })).status();
}

async function checkSecretsAndLayout(page: Page, sensitive: string[]) {
  const snapshot = await page.evaluate(() => ({
    dom: document.documentElement.outerHTML,
    values: [...document.querySelectorAll('input, textarea')].map((element) => (element as HTMLInputElement).value),
    local: JSON.stringify(localStorage), session: JSON.stringify(sessionStorage), url: location.href,
  }));
  expect(sensitive.some((secret) => JSON.stringify(snapshot).includes(secret)),
    'Device pages must exclude raw secrets from DOM, form values, browser storage, and URL.').toBe(false);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect(await page.locator('#main-content').evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
}

// This isolated scenario never exports network traces, videos, automatic
// screenshots, credentials, or private result files. Screenshots are explicit
// and taken only after checking the visible application for raw secrets.
test.use({ screenshot: 'off', trace: 'off', video: 'off' });

test('isolated devices page, rename, recover concurrent changes, remove logins, and register again', async ({ page, context, request }, testInfo) => {
  test.skip(process.env.GOBY_SMOKE_DEVICES_DISPOSABLE_DATABASE !== '1'
    || process.env.GOBY_SMOKE_DEVICES_DEDICATED_ADMIN !== '1',
  'The isolated runner must confirm its disposable database and dedicated administrator.');
  test.setTimeout(270_000);
  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  const baseURL = process.env.GOBY_SMOKE_BASE_URL;
  const manifestPath = process.env.GOBY_SMOKE_DEVICES_FIXTURE_MANIFEST;
  if (!name || !password || !baseURL || !manifestPath) throw new Error('The private device environment is incomplete.');
  const fixture = await loadFixture(manifestPath, baseURL);
  expect(new URL(testInfo.project.use.baseURL ?? '').origin).toBe(fixture.Origin);
  expect(name).toBe(fixture.Administrator.Name);
  const sensitive = [password, fixture.Member.Password, ...fixture.Devices.map((login) => login.Token), fixture.SecondTarget.Token];
  const resultPath = path.join(path.dirname(manifestPath), 'device-result.json');
  const result: PrivateResult = { Marker: 'goby-device-browser-result-v1', RunId: fixture.RunId, Complete: false, Removed: [] };
  await saveResult(resultPath, result, true);
  const pageErrors: string[] = [];
  const consoleMessages: string[] = [];
  const browserURLs: string[] = [];
  const listQueries: string[] = [];
  const mutations: { pathname: string; body: string | null; csrf: boolean; query: string }[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('console', (message) => consoleMessages.push(message.text()));
  page.on('request', (outgoing) => {
    const url = new URL(outgoing.url());
    browserURLs.push(outgoing.url());
    if (outgoing.method() === 'GET' && url.pathname === '/admin/v1/devices') listQueries.push(url.search);
    if (outgoing.method() === 'POST' && /^\/admin\/v1\/devices\/[1-9][0-9]*\/(?:options|delete)$/.test(url.pathname)) {
      mutations.push({ pathname: url.pathname, body: outgoing.postData(), csrf: Boolean(outgoing.headers()['x-csrf-token']), query: url.search });
    }
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto('/admin/devices');
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
  await page.getByLabel(/^Username/).fill(name);
  await page.getByLabel(/^Password/).fill(password);
  const initialResponse = deviceResponse(page, { StartIndex: '0', Limit: '25' });
  const signedInResponse = page.waitForResponse((response) => response.request().method() === 'POST'
    && new URL(response.url()).pathname === '/admin/v1/session');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const sessionResponse = await signedInResponse;
  const session = await sessionResponse.json() as { CSRFToken: string };
  // Learn the browser's own cookie and CSRF token before any dashboard
  // assertion, so early failures retain the same private redaction coverage.
  result.BrowserSecrets = [session.CSRFToken, ...(await context.cookies()).map((cookie) => cookie.value)]
    .filter((value) => typeof value === 'string' && value.length > 0);
  sensitive.push(...result.BrowserSecrets);
  await saveResult(resultPath, result);
  expect(sessionResponse.status()).toBe(200);
  expect(typeof session.CSRFToken === 'string' && session.CSRFToken.length >= 32).toBe(true);
  const browserCookie = (await context.cookies()).find((cookie) => cookie.name === 'goby_session');
  expect(browserCookie !== undefined).toBe(true);
  result.BrowserCookie = `goby_session=${browserCookie!.value}`;
  await saveResult(resultPath, result);
  await expect(page.getByRole('heading', { name: 'Devices', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Devices', exact: true })).toHaveAttribute('href', '/admin/devices');
  await expect(page.getByRole('link', { name: 'Devices', exact: true })).toHaveAttribute('aria-current', 'page');
  const initial = await readDevices(page, await initialResponse, sensitive);
  expect(initial.TotalRecordCount).toBe(fixture.SeedCount);
  expect(initial.Items).toHaveLength(25);
  const nativeHeaders = { 'X-CSRF-Token': session.CSRFToken, Origin: fixture.Origin };
  const nativeSessions = await context.request.get('/admin/v1/sessions?Kind=admin&Status=active&Limit=200');
  expect(nativeSessions.status()).toBe(200);
  const nativeRecords = await nativeSessions.json() as { Items: { Id: string; IsCurrent: boolean }[]; TotalRecordCount: number };
  expect(nativeRecords.TotalRecordCount).toBe(2);
  const current = nativeRecords.Items.filter((item) => item.IsCurrent);
  expect(current).toHaveLength(1);
  result.BrowserSessionId = current[0].Id;
  await saveResult(resultPath, result);

  async function persistRemoval(device: Device, receipt: { Id: string; DeletedAt: string; RevokedLoginCount: number }) {
    expect(Object.keys(receipt).sort()).toEqual(['DeletedAt', 'Id', 'RevokedLoginCount']);
    expect(receipt.Id).toBe(device.Id);
    expect(receipt.DeletedAt).toMatch(/Z$/);
    expect(Number.isSafeInteger(receipt.RevokedLoginCount) && receipt.RevokedLoginCount >= 0).toBe(true);
    result.Removed.push({ ...receipt, Revision: device.Revision });
    await saveResult(resultPath, result);
  }

  await test.step('page all real devices without overlap and search literal text with UTF-8 bounds', async () => {
    const nextResponse = deviceResponse(page, { StartIndex: '25', Limit: '25' });
    await page.getByRole('button', { name: 'Go to next page', exact: true }).click();
    const second = await readDevices(page, await nextResponse, sensitive);
    expect(second.Items).toHaveLength(7);
    expect(second.TotalRecordCount).toBe(initial.TotalRecordCount);
    expect(new Set([...initial.Items, ...second.Items].map((item) => item.Id)).size).toBe(32);
    expect([...initial.Items, ...second.Items].map((item) => item.Id).sort()).toEqual(fixture.Devices.map((login) => login.Id).sort());
    await expect(page.getByRole('button', { name: 'Go to next page', exact: true })).toBeDisabled();
    const previousResponse = deviceResponse(page, { StartIndex: '0', Limit: '25' });
    await page.getByRole('button', { name: 'Go to previous page', exact: true }).click();
    expect((await readDevices(page, await previousResponse, sensitive)).Items.map((item) => item.Id)).toEqual(initial.Items.map((item) => item.Id));
    const literal = await searchDevices(page, fixture.LiteralSearch, sensitive);
    expect(literal.TotalRecordCount).toBe(1);
    expect(literal.Items[0].Name).toBe(fixture.LiteralName);
    expect((await searchDevices(page, fixture.LiteralName.toUpperCase(), sensitive)).Items[0].Id).toBe(literal.Items[0].Id);
    expect((await searchDevices(page, fixture.Sibling.AppName, sensitive)).TotalRecordCount).toBe(32);
    expect((await searchDevices(page, fixture.Member.Name, sensitive)).TotalRecordCount).toBe(32);
    const before = listQueries.length;
    const search = page.getByRole('textbox', { name: 'Search devices', exact: true });
    await search.fill('\u754c'.repeat(86));
    await expect(page.getByText('Use at most 256 UTF-8 bytes. Some characters use more than one byte.', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Search devices', exact: true })).toBeDisabled();
    await search.press('Enter');
    expect(listQueries.length).toBe(before);
    expect((await searchDevices(page, '\u754c'.repeat(85), sensitive)).TotalRecordCount).toBe(0);
    await expect(page.getByRole('heading', { name: 'No matching devices', exact: true })).toBeVisible();
    await clearSearch(page, sensitive);
  });

  await test.step('cancel rename without mutation, then save, repeat, and clear the real custom name', async () => {
    let target = (await searchDevices(page, fixture.Target.ReportedDeviceId, sensitive)).Items[0];
    expect(target.ActiveLoginCount).toBe(2);
    const row = deviceRecord(page, fixture.Target.ReportedDeviceId);
    await row.getByRole('button', { name: `Rename ${target.Name}`, exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Rename device', exact: true });
    const input = dialog.getByRole('textbox', { name: 'Custom name', exact: true });
    await input.fill('\u754c'.repeat(86));
    await expect(dialog.getByRole('button', { name: 'Save name', exact: true })).toBeDisabled();
    await input.fill('\u754c'.repeat(85));
    await expect(dialog.getByRole('button', { name: 'Save name', exact: true })).toBeEnabled();
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(mutations).toHaveLength(0);

    async function saveName(value: string, clear = false) {
      await row.getByRole('button', { name: `Rename ${target.Name}`, exact: true }).click();
      if (clear) await dialog.getByRole('button', { name: 'Clear custom name', exact: true }).click();
      else await input.fill(value);
      const mutation = deviceMutation(page, `/admin/v1/devices/${target.Id}/options`);
      const refreshed = deviceResponse(page, { SearchTerm: fixture.Target.ReportedDeviceId });
      await dialog.getByRole('button', { name: 'Save name', exact: true }).click();
      const response = await mutation;
      expect(response.status()).toBe(200);
      expect(response.headers()['cache-control']).toBe('no-store');
      const updated = await response.json() as Device;
      assertDevice(updated);
      expect(updated.Id).toBe(target.Id);
      expect(mutations.at(-1)?.body).toBe(JSON.stringify({ Revision: target.Revision, CustomName: value.trim() }));
      const listed = (await readDevices(page, await refreshed, sensitive)).Items[0];
      expect(listed.Name).toBe(updated.Name);
      expect(listed.Revision).toBe(updated.Revision);
      await expect(dialog).toHaveCount(0);
      return updated;
    }
    const originalRevision = target.Revision;
    target = await saveName('  Cinema \u754c display  ');
    expect(target.CustomName).toBe('Cinema \u754c display');
    expect(BigInt(target.Revision) > BigInt(originalRevision)).toBe(true);
    await expect(row.getByText(`Reported name: ${fixture.Target.ReportedName}`, { exact: true })).toBeVisible();
    const savedRevision = target.Revision;
    target = await saveName(target.CustomName!);
    expect(target.Revision).toBe(savedRevision);
    target = await saveName('', true);
    expect(target.CustomName).toBeNull();
    expect(target.Name).toBe(fixture.Target.ReportedName);
    expect(BigInt(target.Revision) > BigInt(savedRevision)).toBe(true);
  });

  await test.step('recover an actual stale revision through explicit refresh without replay', async () => {
    const target = (await searchDevices(page, fixture.Target.ReportedDeviceId, sensitive)).Items[0];
    await deviceRecord(page, fixture.Target.ReportedDeviceId).getByRole('button', { name: `Rename ${target.Name}`, exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Rename device', exact: true });
    await dialog.getByRole('textbox', { name: 'Custom name', exact: true }).fill('Superseded browser name');
    const concurrent = await context.request.post(`/admin/v1/devices/${target.Id}/options`, {
      headers: nativeHeaders, data: { Revision: target.Revision, CustomName: 'Living room display' },
    });
    expect(concurrent.status()).toBe(200);
    const currentDevice = await concurrent.json() as Device;
    const before = mutations.length;
    const rejected = deviceMutation(page, `/admin/v1/devices/${target.Id}/options`);
    await dialog.getByRole('button', { name: 'Save name', exact: true }).click();
    expect((await rejected).status()).toBe(409);
    await expect(dialog.getByText(/This device changed since you opened it/)).toBeVisible();
    await expect(dialog.getByRole('textbox', { name: 'Custom name', exact: true })).toBeDisabled();
    await expect(dialog.getByRole('button', { name: 'Save name', exact: true })).toHaveCount(0);
    expect(mutations.length).toBe(before + 1);
    const refreshed = deviceResponse(page, { SearchTerm: fixture.Target.ReportedDeviceId });
    await dialog.getByRole('button', { name: 'Refresh devices', exact: true }).click();
    expect((await readDevices(page, await refreshed, sensitive)).Items[0].Revision).toBe(currentDevice.Revision);
    await expect(dialog).toHaveCount(0);
    await expect(deviceRecord(page, fixture.Target.ReportedDeviceId).getByText('Living room display', { exact: true })).toBeVisible();
    expect(mutations.length).toBe(before + 1);
  });

  await test.step('recover a genuinely removed record without replaying a stale rename', async () => {
    const missing = (await searchDevices(page, fixture.Missing.ReportedDeviceId, sensitive)).Items[0];
    await deviceRecord(page, missing.ReportedDeviceId).getByRole('button', { name: `Rename ${missing.Name}`, exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Rename device', exact: true });
    await dialog.getByRole('textbox', { name: 'Custom name', exact: true }).fill('No longer present');
    const removed = await context.request.post(`/admin/v1/devices/${missing.Id}/delete`, { headers: nativeHeaders, data: { Revision: missing.Revision } });
    expect(removed.status()).toBe(200);
    await persistRemoval(missing, await removed.json() as RemovedDevice);
    const before = mutations.length;
    const rejected = deviceMutation(page, `/admin/v1/devices/${missing.Id}/options`);
    await dialog.getByRole('button', { name: 'Save name', exact: true }).click();
    expect((await rejected).status()).toBe(404);
    await expect(dialog.getByText(/This device is no longer available/)).toBeVisible();
    const refreshed = deviceResponse(page, { SearchTerm: fixture.Missing.ReportedDeviceId });
    await dialog.getByRole('button', { name: 'Refresh devices', exact: true }).click();
    expect((await readDevices(page, await refreshed, sensitive)).TotalRecordCount).toBe(0);
    expect(mutations.length).toBe(before + 1);
    expect(await tokenStatus(request, fixture, fixture.Missing.Token)).toBe(401);
  });

  await test.step('lose one real committed rename response and recover the persisted custom name', async () => {
    const sibling = (await searchDevices(page, fixture.Sibling.ReportedDeviceId, sensitive)).Items[0];
    await deviceRecord(page, sibling.ReportedDeviceId).getByRole('button', { name: `Rename ${sibling.Name}`, exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Rename device', exact: true });
    await dialog.getByRole('textbox', { name: 'Custom name', exact: true }).fill(fixture.PersistedName);
    await checkSecretsAndLayout(page, sensitive);
    await page.screenshot({ path: testInfo.outputPath('devices-rename-desktop.png'), fullPage: true, animations: 'disabled' });
    let committed: Device | undefined;
    let forwarded = 0;
    const routeURL = `${fixture.Origin}/admin/v1/devices/${sibling.Id}/options`;
    await page.route(routeURL, async (route) => {
      forwarded += 1;
      // Commit through the actual server first, then discard only the wire
      // response. This does not fabricate a success payload or server state.
      const response = await route.fetch();
      if (response.status() !== 200) throw new Error('The real rename did not commit before its response was discarded.');
      committed = await response.json() as Device;
      await response.dispose();
      await route.abort('failed');
    }, { times: 1 });
    const before = mutations.length;
    await dialog.getByRole('button', { name: 'Save name', exact: true }).click();
    await expect(dialog.getByText(/The result could not be confirmed/)).toBeVisible();
    await expect(dialog.getByRole('textbox', { name: 'Custom name', exact: true })).toBeDisabled();
    expect(forwarded).toBe(1);
    expect(committed?.CustomName).toBe(fixture.PersistedName);
    expect(mutations.length).toBe(before + 1);
    const refreshed = deviceResponse(page, { SearchTerm: fixture.Sibling.ReportedDeviceId });
    await dialog.getByRole('button', { name: 'Refresh devices', exact: true }).click();
    const saved = (await readDevices(page, await refreshed, sensitive)).Items[0];
    expect(saved.CustomName).toBe(fixture.PersistedName);
    expect(saved.Revision).toBe(committed?.Revision);
    result.PersistedRevision = saved.Revision;
    await saveResult(resultPath, result);
    expect(mutations.length).toBe(before + 1);
    await page.unroute(routeURL);
    await checkSecretsAndLayout(page, sensitive);
    await page.screenshot({ path: testInfo.outputPath('devices-desktop.png'), fullPage: true, animations: 'disabled' });
  });

  await test.step('cancel removal, then remove both target logins on mobile and register a new generation', async () => {
    const target = (await searchDevices(page, fixture.Target.ReportedDeviceId, sensitive)).Items[0];
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.getByRole('list', { name: 'Registered devices', exact: true }).getByRole('listitem')).toHaveCount(1);
    await checkSecretsAndLayout(page, sensitive);
    await page.screenshot({ path: testInfo.outputPath('devices-mobile.png'), fullPage: true, animations: 'disabled' });
    const row = deviceRecord(page, target.ReportedDeviceId);
    await row.getByRole('button', { name: `Remove device ${target.Name}`, exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Remove device?', exact: true });
    await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeFocused();
    await expect(dialog.getByText(/sign out its associated client logins/)).toBeVisible();
    await expect(dialog.getByText('2 authorized logins', { exact: true })).toBeVisible();
    const bounds = await dialog.boundingBox();
    expect(bounds).not.toBeNull();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(391);
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true, value: { writeText: async () => { throw new Error('Clipboard denied by isolated test'); } },
      });
    });
    await dialog.getByRole('button', { name: 'Copy device ID', exact: true }).click();
    await expect(dialog.getByRole('status').filter({ hasText: 'Clipboard access is unavailable.' })).toBeVisible();
    expect(await dialog.getByRole('textbox', { name: 'Client device ID', exact: true }).evaluate((element) => {
      const input = element as HTMLTextAreaElement;
      return document.activeElement === input && input.selectionStart === 0 && input.selectionEnd === input.value.length;
    })).toBe(true);
    await checkSecretsAndLayout(page, sensitive);
    await page.screenshot({ path: testInfo.outputPath('devices-remove-mobile.png'), fullPage: true, animations: 'disabled' });
    const before = mutations.length;
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(mutations.length).toBe(before);
    expect(await tokenStatus(request, fixture, fixture.Target.Token)).toBe(200);
    expect(await tokenStatus(request, fixture, fixture.SecondTarget.Token)).toBe(200);
    await row.getByRole('button', { name: `Remove device ${target.Name}`, exact: true }).click();
    const deletedResponse = deviceMutation(page, `/admin/v1/devices/${target.Id}/delete`);
    const refreshed = deviceResponse(page, { SearchTerm: target.ReportedDeviceId });
    await dialog.getByRole('button', { name: 'Remove device', exact: true }).click();
    const removed = await deletedResponse;
    expect(removed.status()).toBe(200);
    const receipt = await removed.json() as RemovedDevice;
    expect(receipt.RevokedLoginCount).toBe(2);
    await persistRemoval(target, receipt);
    expect((await readDevices(page, await refreshed, sensitive)).TotalRecordCount).toBe(0);
    expect(await tokenStatus(request, fixture, fixture.Target.Token)).toBe(401);
    expect(await tokenStatus(request, fixture, fixture.SecondTarget.Token)).toBe(401);
    expect(await tokenStatus(request, fixture, fixture.Sibling.Token)).toBe(200);
    expect((await context.request.get('/admin/v1/session')).status()).toBe(200);
    const repeated = await context.request.post(`/admin/v1/devices/${target.Id}/delete`, { headers: nativeHeaders, data: { Revision: target.Revision } });
    expect(repeated.status()).toBe(200);
    expect(await repeated.json()).toEqual({ Id: target.Id, DeletedAt: receipt.DeletedAt, RevokedLoginCount: 0 });

    const replacementResponse = await request.post('/emby/Users/AuthenticateByName', {
      headers: { 'X-Emby-Client': fixture.Target.AppName, 'X-Emby-Device-Id': fixture.Target.ReportedDeviceId,
        'X-Emby-Device-Name': fixture.Target.ReportedName, 'X-Emby-Client-Version': fixture.Target.AppVersion },
      data: { Username: fixture.Member.Name, Pw: fixture.Member.Password },
    });
    const replacement = await replacementResponse.json() as { AccessToken?: string; SessionInfo?: { Id?: string } };
    // Persist the new token before any assertion can fail or publish an error.
    result.ReplacementLogin = { Token: replacement.AccessToken, SessionId: replacement.SessionInfo?.Id };
    if (typeof replacement.AccessToken === 'string' && replacement.AccessToken) sensitive.push(replacement.AccessToken);
    await saveResult(resultPath, result);
    expect(replacementResponse.status()).toBe(200);
    expect(typeof replacement.AccessToken === 'string' && replacement.AccessToken.length >= 32).toBe(true);
    expect(replacement.SessionInfo?.Id).toMatch(/^[0-9a-f]{32}$/);
    const registeredResponse = deviceResponse(page, { SearchTerm: target.ReportedDeviceId });
    await page.getByRole('button', { name: 'Refresh devices', exact: true }).click();
    const registered = (await readDevices(page, await registeredResponse, sensitive)).Items[0];
    expect(registered.Id === target.Id).toBe(false);
    expect(registered.CustomName).toBeNull();
    expect(registered.ActiveLoginCount).toBe(1);
    result.ReplacementLogin = { ...result.ReplacementLogin, Id: registered.Id, ReportedDeviceId: registered.ReportedDeviceId };
    await saveResult(resultPath, result);
    expect(await tokenStatus(request, fixture, replacement.AccessToken!)).toBe(200);
    expect(await tokenStatus(request, fixture, fixture.Target.Token)).toBe(401);
  });

  await test.step('remove an ordinary goby-dashboard device while preserving the exact native administrator cookie', async () => {
    const collision = (await searchDevices(page, fixture.DashboardCollision.ReportedDeviceId, sensitive)).Items[0];
    expect(collision.Id).toBe(fixture.DashboardCollision.Id);
    const cookiesBefore = await context.cookies();
    await deviceRecord(page, collision.ReportedDeviceId).getByRole('button', { name: `Remove device ${collision.Name}`, exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Remove device?', exact: true });
    const deletedResponse = deviceMutation(page, `/admin/v1/devices/${collision.Id}/delete`);
    const refreshed = deviceResponse(page, { SearchTerm: collision.ReportedDeviceId });
    await dialog.getByRole('button', { name: 'Remove device', exact: true }).click();
    const response = await deletedResponse;
    expect(response.status()).toBe(200);
    const receipt = await response.json() as RemovedDevice;
    expect(receipt.RevokedLoginCount).toBe(1);
    await persistRemoval(collision, receipt);
    expect((await readDevices(page, await refreshed, sensitive)).TotalRecordCount).toBe(0);
    expect(await tokenStatus(request, fixture, fixture.DashboardCollision.Token)).toBe(401);
    expect((await context.request.get('/admin/v1/session')).status()).toBe(200);
    const cookiesAfter = await context.cookies();
    expect(JSON.stringify(cookiesAfter) === JSON.stringify(cookiesBefore), 'Ordinary device removal must preserve the existing native cookie.').toBe(true);
    await expect(page.getByRole('heading', { name: 'Devices', exact: true })).toBeVisible();
  });

  await test.step('refresh an emptied final page and clamp to the remaining first page', async () => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    const all = await clearSearch(page, sensitive);
    expect(all.TotalRecordCount).toBe(30);
    const nextResponse = deviceResponse(page, { StartIndex: '25', Limit: '25' });
    await page.getByRole('button', { name: 'Go to next page', exact: true }).click();
    const last = await readDevices(page, await nextResponse, sensitive);
    expect(last.Items).toHaveLength(5);
    const protectedIds = [fixture.Sibling.Id, result.ReplacementLogin!.Id];
    expect(last.Items.some((device) => protectedIds.includes(device.Id))).toBe(false);
    for (const device of last.Items) {
      const response = await context.request.post(`/admin/v1/devices/${device.Id}/delete`, { headers: nativeHeaders, data: { Revision: device.Revision } });
      expect(response.status()).toBe(200);
      await persistRemoval(device, await response.json() as RemovedDevice);
    }
    const obsoletePage = deviceResponse(page, { StartIndex: '25', Limit: '25' });
    const clampedPage = deviceResponse(page, { StartIndex: '0', Limit: '25' });
    await page.getByRole('button', { name: 'Refresh devices', exact: true }).click();
    const obsolete = await readDevices(page, await obsoletePage, sensitive, false);
    expect(obsolete.Items).toHaveLength(0);
    expect(obsolete.TotalRecordCount).toBe(25);
    const clamped = await readDevices(page, await clampedPage, sensitive);
    expect(clamped.Items).toHaveLength(25);
    await expect(page.getByRole('button', { name: 'Go to previous page', exact: true })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Go to next page', exact: true })).toBeDisabled();
    result.ExpectedCount = clamped.TotalRecordCount;
    result.ExpectedDeviceIds = clamped.Items.map((device) => device.Id);
    expect(result.Removed).toHaveLength(8);
    const retained = new Set(result.ExpectedDeviceIds);
    expect(result.Removed.some((device) => retained.has(device.Id))).toBe(false);
    for (const device of last.Items) {
      const login = fixture.Devices.find((item) => item.Id === device.Id);
      expect(login !== undefined).toBe(true);
      expect(await tokenStatus(request, fixture, login!.Token)).toBe(401);
    }
    await checkSecretsAndLayout(page, sensitive);
  });

  expect(pageErrors.length, 'The browser journey must not raise uncaught page errors.').toBe(0);
  expect(sensitive.some((secret) => [...pageErrors, ...consoleMessages, ...browserURLs].some((value) => value.includes(secret))),
    'Browser errors, console messages, and request URLs must exclude raw authentication secrets.').toBe(false);
  expect(mutations.every((mutation) => mutation.csrf && mutation.query === '')).toBe(true);
  for (const mutation of mutations) {
    const body = JSON.parse(mutation.body ?? '{}') as Record<string, unknown>;
    expect(Object.keys(body).sort()).toEqual(mutation.pathname.endsWith('/options') ? ['CustomName', 'Revision'] : ['Revision']);
    expect(body.Revision).toMatch(/^[1-9][0-9]*$/);
  }
  result.Complete = true;
  await saveResult(resultPath, result);
});
