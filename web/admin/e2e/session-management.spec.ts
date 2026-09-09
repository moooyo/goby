import { constants } from 'node:fs';
import { lstat, open, realpath } from 'node:fs/promises';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import type { APIRequestContext, Locator, Page, Response } from '@playwright/test';
import type { LoginSession, RevokeSessionResponse, SessionsResponse } from '../src/api';

interface FixtureIdentity {
  Id: string;
  Name: string;
}

interface FixtureLogin {
  Id: string;
  Token: string;
  Client: string;
  DeviceId: string;
  DeviceName: string;
}

interface SessionFixture {
  Marker: string;
  RunId: string;
  Origin: string;
  Administrator: FixtureIdentity;
  Member: FixtureIdentity & { Password: string };
  DisabledUser: FixtureIdentity;
  Target: FixtureLogin;
  Sibling: FixtureLogin;
  HistoricalCount: number;
  ExpiredCount: number;
  RevokedCount: number;
  DisabledCount: number;
  LiteralSearch: string;
  ReplacementClient: string;
}

const fixtureMarker = 'goby-session-browser-fixtures-v1';
const fixturePattern = /^\/opt\/goby-test\/exec-scratch\/goby-sessions-([0-9]{8}_[0-9]{6}_[0-9a-f]{10})\/browser\/session-fixture\.json$/;
const revokeDialog = (page: Page): Locator => page.getByRole('dialog', { name: 'Revoke this login?', exact: true });

async function loadFixture(filename: string, baseURL: string): Promise<SessionFixture> {
  const match = fixturePattern.exec(filename);
  if (process.platform !== 'linux' || process.getuid?.() !== 0 || !match
    || filename !== path.resolve(filename) || await realpath(filename) !== filename) {
    throw new Error('Session fixtures require the isolated Linux runner and its canonical private manifest.');
  }
  for (const directory of [path.dirname(filename), path.dirname(path.dirname(filename))]) {
    const status = await lstat(directory);
    if (!status.isDirectory() || status.isSymbolicLink() || status.uid !== 0 || (status.mode & 0o777) !== 0o700) {
      throw new Error('The session fixture parent directories must be private and root owned.');
    }
  }
  const file = await open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  let fixture: SessionFixture;
  try {
    const status = await file.stat();
    if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o600 || status.nlink !== 1
      || status.size > 64 * 1024 || await realpath(`/proc/self/fd/${file.fd}`) !== filename) {
      throw new Error('The session fixture must be a bounded private regular file.');
    }
    fixture = JSON.parse(await file.readFile('utf8')) as SessionFixture;
  } finally {
    await file.close();
  }
  const origin = new URL(baseURL);
  if (!fixture || fixture.Marker !== fixtureMarker || fixture.RunId !== match[1]
    || fixture.RunId !== process.env.GOBY_SESSIONS_RUN_ID || fixture.Origin !== origin.origin
    || origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || !origin.port
    || ['5432', '15432', '18096', '18097'].includes(origin.port)
    || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash
    || fixture.HistoricalCount !== 62 || fixture.ExpiredCount !== 52 || fixture.RevokedCount !== 8 || fixture.DisabledCount !== 2
    || ![fixture.Administrator, fixture.Member, fixture.DisabledUser].every((user) => user
      && /^[0-9a-f]{32}$/.test(user.Id) && typeof user.Name === 'string' && user.Name.includes(fixture.RunId))
    || typeof fixture.Member.Password !== 'string' || fixture.Member.Password.length < 32
    || ![fixture.Target, fixture.Sibling].every((login) => login && /^[0-9a-f]{32}$/.test(login.Id)
      && typeof login.Token === 'string' && login.Token.length >= 32
      && [login.Client, login.DeviceId, login.DeviceName].every((value) => typeof value === 'string' && value.length > 0))
    || fixture.Target.Id === fixture.Sibling.Id || fixture.Target.Token === fixture.Sibling.Token
    || fixture.Target.DeviceId !== fixture.Sibling.DeviceId || fixture.Target.Client === fixture.Sibling.Client
    || fixture.LiteralSearch !== '100%_\\ ' || fixture.ReplacementClient !== 'Session replacement client') {
    throw new Error('The private session fixture does not match the disposable run contract.');
  }
  return fixture;
}

function sessionResponse(page: Page, parameters: Record<string, string> = {}) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === 'GET' && url.pathname === '/admin/v1/sessions'
      && Object.entries(parameters).every(([name, value]) => url.searchParams.get(name) === value);
  });
}

async function readSessions(page: Page, response: Response, secrets: string[]): Promise<SessionsResponse> {
  expect(response.status()).toBe(200);
  const raw = await response.text();
  expect(secrets.some((secret) => raw.includes(secret)), 'Session responses must not contain authentication secrets.').toBe(false);
  const result = JSON.parse(raw) as SessionsResponse;
  expect(Array.isArray(result.Items)).toBe(true);
  expect(result.TotalRecordCount).toBeGreaterThanOrEqual(result.Items.length);
  for (const item of result.Items) {
    expect(Object.keys(item).sort()).toEqual([
      'ApplicationVersion', 'Client', 'CreatedAt', 'DeviceId', 'DeviceName', 'ExpiresAt', 'Id', 'IsCurrent',
      'Kind', 'LastSeenAt', 'RevokedAt', 'Status', 'UserId', 'UserIsAdministrator', 'UserIsDisabled', 'UserName',
    ].sort());
  }
  await expect(page.getByRole('status', { name: 'Loading sessions', exact: true })).not.toBeVisible();
  if (result.Items.length > 0) {
    if ((page.viewportSize()?.width ?? 1440) >= 1200) {
      await expect(page.getByRole('table', { name: 'Login sessions', exact: true }).getByRole('row')).toHaveCount(result.Items.length + 1);
    } else {
      await expect(page.getByRole('list', { name: 'Login sessions', exact: true }).getByRole('listitem')).toHaveCount(result.Items.length);
    }
  }
  return result;
}

async function applyFilters(page: Page, secrets: string[], parameters: Record<string, string> = {}) {
  const response = sessionResponse(page, parameters);
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  return readSessions(page, await response, secrets);
}

async function clearFilters(page: Page, secrets: string[]) {
  const response = sessionResponse(page, { Status: 'active', StartIndex: '0' });
  await page.getByRole('button', { name: 'Clear filters', exact: true }).first().click();
  return readSessions(page, await response, secrets);
}

async function choose(page: Page, label: string, option: string) {
  await page.getByRole('combobox', { name: label, exact: true }).click();
  await page.getByRole('option', { name: option, exact: true }).click();
}

function loginRecord(page: Page, client: string): Locator {
  if ((page.viewportSize()?.width ?? 1440) >= 1200) {
    return page.getByRole('table', { name: 'Login sessions', exact: true }).getByRole('row').filter({ hasText: client });
  }
  return page.getByRole('list', { name: 'Login sessions', exact: true }).getByRole('listitem').filter({ hasText: client });
}

async function tokenStatus(request: APIRequestContext, fixture: SessionFixture, token: string): Promise<number> {
  return (await request.get(`/emby/Users/${fixture.Member.Id}/Views`, { headers: { 'X-Emby-Token': token } })).status();
}

async function checkLayoutAndSecrets(page: Page, secrets: string[]) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect(await page.locator('#main-content').evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  const text = await page.locator('body').innerText();
  expect(secrets.some((secret) => text.includes(secret)), 'The dashboard must not render authentication secrets.').toBe(false);
}

// Only verify-sessions.py may supply this fixture. It owns the whole temporary
// database, starts the application on a private ephemeral HTTP port, creates the
// real tokens, and removes the private browser manifest and logs during cleanup.
// Traces, video, and network attachments remain disabled because login and
// authorization requests contain credentials. Assertions never print tokens.
test('isolated login-session filters, paging, targeted revocation, and administrator self-revocation', async ({ page, context }, testInfo) => {
  test.skip(process.env.GOBY_SMOKE_SESSIONS_DISPOSABLE_DATABASE !== '1'
    || process.env.GOBY_SMOKE_SESSIONS_DEDICATED_ADMIN !== '1',
  'The isolated runner must explicitly confirm its disposable database and dedicated administrator.');
  test.setTimeout(240_000);
  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  const baseURL = process.env.GOBY_SMOKE_BASE_URL;
  const manifestPath = process.env.GOBY_SMOKE_SESSIONS_FIXTURE_MANIFEST;
  if (!name || !password || !baseURL || !manifestPath) throw new Error('The private session runner environment is incomplete.');
  const fixture = await loadFixture(manifestPath, baseURL);
  expect(new URL(testInfo.project.use.baseURL ?? '').origin).toBe(fixture.Origin);
  expect(name).toBe(fixture.Administrator.Name);
  const sensitive = [password, fixture.Member.Password, fixture.Target.Token, fixture.Sibling.Token];
  const pageErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  const mutations: { pathname: string; body: string | null; csrf: boolean; query: string }[] = [];
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (request.method() === 'POST' && /^\/admin\/v1\/sessions\/[^/]+\/revoke$/.test(url.pathname)) {
      mutations.push({ pathname: url.pathname, body: request.postData(), csrf: Boolean(request.headers()['x-csrf-token']), query: url.search });
    }
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto('/admin/sessions');
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
  await page.getByLabel(/^Username/).fill(name);
  await page.getByLabel(/^Password/).fill(password);
  const initialResponse = sessionResponse(page, { Status: 'active', StartIndex: '0', Limit: '50' });
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Sessions', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Sessions', exact: true })).toHaveAttribute('href', '/admin/sessions');
  await expect(page.getByRole('link', { name: 'Sessions', exact: true })).toHaveAttribute('aria-current', 'page');
  const initial = await readSessions(page, await initialResponse, sensitive);
  expect(initial.TotalRecordCount).toBe(4);
  expect(initial.Items.every((item) => item.Status === 'active')).toBe(true);
  expect(initial.Items.filter((item) => item.IsCurrent)).toHaveLength(1);
  expect(initial.Items.some((item) => item.Id === fixture.Target.Id)).toBe(true);
  expect(initial.Items.some((item) => item.Id === fixture.Sibling.Id)).toBe(true);
  await expect(page.getByText(/Active means a login is authorized; it does not show whether a user is online/)).toBeVisible();
  await checkLayoutAndSecrets(page, sensitive);
  await page.screenshot({ path: testInfo.outputPath('sessions-desktop.png'), fullPage: true, animations: 'disabled' });

  await test.step('page a real result set with stable nonoverlapping records', async () => {
    await choose(page, 'Status', 'All statuses');
    const first = await applyFilters(page, sensitive, { Status: 'all', StartIndex: '0', Limit: '50' });
    expect(first.TotalRecordCount).toBe(fixture.HistoricalCount + 4);
    expect(first.Items).toHaveLength(50);
    const nextResponse = sessionResponse(page, { StartIndex: '50', Limit: '50' });
    await page.getByRole('button', { name: 'Go to next page', exact: true }).click();
    const second = await readSessions(page, await nextResponse, sensitive);
    expect(second.TotalRecordCount).toBe(first.TotalRecordCount);
    expect(second.Items).toHaveLength(first.TotalRecordCount - 50);
    const firstIds = new Set(first.Items.map((item) => item.Id));
    expect(second.Items.some((item) => firstIds.has(item.Id))).toBe(false);
    await expect(page.getByRole('button', { name: 'Go to next page', exact: true })).toBeDisabled();
    const previousResponse = sessionResponse(page, { StartIndex: '0', Limit: '50' });
    await page.getByRole('button', { name: 'Go to previous page', exact: true }).click();
    expect((await readSessions(page, await previousResponse, sensitive)).Items.map((item) => item.Id)).toEqual(first.Items.map((item) => item.Id));
    const resizedResponse = sessionResponse(page, { StartIndex: '0', Limit: '25' });
    await page.getByRole('combobox', { name: /Sessions per page/ }).click();
    await page.getByRole('option', { name: '25', exact: true }).click();
    const resized = await readSessions(page, await resizedResponse, sensitive);
    expect(resized.Items).toHaveLength(25);
    const refreshResponse = sessionResponse(page, { Status: 'all', StartIndex: '0', Limit: '25' });
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    const refreshed = await readSessions(page, await refreshResponse, sensitive);
    expect(refreshed.Items.map((item) => item.Id)).toEqual(resized.Items.map((item) => item.Id));
  });

  await test.step('filter by login kind, exact user, and each authorization status', async () => {
    await clearFilters(page, sensitive);
    await choose(page, 'Login type', 'Administrator dashboard');
    const administrators = await applyFilters(page, sensitive, { Kind: 'admin', Status: 'active' });
    expect(administrators.TotalRecordCount).toBe(2);
    expect(administrators.Items.every((item) => item.Kind === 'admin')).toBe(true);
    await choose(page, 'Login type', 'Emby client');
    const clients = await applyFilters(page, sensitive, { Kind: 'emby' });
    expect(new Set(clients.Items.map((item) => item.Id))).toEqual(new Set([fixture.Target.Id, fixture.Sibling.Id]));
    await page.getByRole('combobox', { name: 'User', exact: true }).fill(fixture.Member.Name);
    await page.getByRole('option', { name: fixture.Member.Name, exact: true }).click();
    const member = await applyFilters(page, sensitive, { UserId: fixture.Member.Id });
    expect(member.TotalRecordCount).toBe(2);
    expect(member.Items.every((item) => item.UserId === fixture.Member.Id)).toBe(true);
    await clearFilters(page, sensitive);
    for (const [label, status, count] of [
      ['Expired', 'expired', fixture.ExpiredCount], ['Revoked', 'revoked', fixture.RevokedCount],
      ['Disabled', 'disabled', fixture.DisabledCount],
    ] as const) {
      await choose(page, 'Status', label);
      const result = await applyFilters(page, sensitive, { Status: status });
      expect(result.TotalRecordCount).toBe(count);
      expect(result.Items.every((item) => item.Status === status)).toBe(true);
      if (status === 'disabled') {
        expect(result.Items.some((item) => item.UserIsDisabled)).toBe(true);
        expect(result.Items.some((item) => item.Kind === 'admin' && !item.UserIsAdministrator && !item.UserIsDisabled)).toBe(true);
      }
    }
    await page.getByRole('combobox', { name: 'User', exact: true }).fill(fixture.DisabledUser.Name);
    await page.getByRole('option', { name: fixture.DisabledUser.Name, exact: true }).click();
    const disabled = await applyFilters(page, sensitive, { UserId: fixture.DisabledUser.Id, Status: 'disabled' });
    expect(disabled.TotalRecordCount).toBe(1);
    expect(disabled.Items[0].UserIsDisabled).toBe(true);
  });

  await test.step('preserve literal wildcard characters and spaces and enforce the UTF-8 search bound', async () => {
    await clearFilters(page, sensitive);
    await choose(page, 'Status', 'All statuses');
    const search = page.getByRole('textbox', { name: 'Search user, client, or device', exact: true });
    await search.fill(fixture.LiteralSearch);
    const literal = await applyFilters(page, sensitive, { SearchTerm: fixture.LiteralSearch, Status: 'all' });
    expect(literal.TotalRecordCount).toBe(1);
    expect(literal.Items[0].Client).toBe('Archive 100%_\\ session');
    await search.fill('\u754c'.repeat(86));
    await expect(page.getByRole('button', { name: 'Apply filters', exact: true })).toBeDisabled();
    await expect(page.getByText('Use at most 256 UTF-8 bytes. Some characters use more than one byte.', { exact: true })).toBeVisible();
    await search.fill('\u754c'.repeat(85));
    const empty = await applyFilters(page, sensitive, { SearchTerm: '\u754c'.repeat(85) });
    expect(empty.TotalRecordCount).toBe(0);
    await expect(page.getByRole('heading', { name: 'No matching login sessions', exact: true })).toBeVisible();
  });

  await test.step('cancel a single-login revocation without changing either real login', async () => {
    await clearFilters(page, sensitive);
    await page.getByRole('textbox', { name: 'Device ID', exact: true }).fill('session-shared-');
    expect((await applyFilters(page, sensitive, { DeviceId: 'session-shared-' })).TotalRecordCount).toBe(0);
    await page.getByRole('textbox', { name: 'Device ID', exact: true }).fill(fixture.Target.DeviceId);
    const device = await applyFilters(page, sensitive, { DeviceId: fixture.Target.DeviceId, Status: 'active' });
    expect(new Set(device.Items.map((item) => item.Id))).toEqual(new Set([fixture.Target.Id, fixture.Sibling.Id]));
    await loginRecord(page, fixture.Target.Client).getByRole('button', { name: /^Revoke login for/ }).click();
    const dialog = revokeDialog(page);
    await expect(dialog.getByText(fixture.Member.Name, { exact: true })).toBeVisible();
    await expect(dialog.getByText(fixture.Target.Client, { exact: true })).toBeVisible();
    await expect(dialog.getByText(fixture.Target.DeviceName, { exact: true })).toBeVisible();
    await expect(dialog.getByText('This ends only the selected login. The user can sign in again on the same device.', { exact: true })).toBeVisible();
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect(mutations).toHaveLength(0);
    expect(await tokenStatus(context.request, fixture, fixture.Target.Token)).toBe(200);
    expect(await tokenStatus(context.request, fixture, fixture.Sibling.Token)).toBe(200);
  });

  await test.step('revoke the selected login on mobile and preserve its same-device sibling', async () => {
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.getByRole('list', { name: 'Login sessions', exact: true }).getByRole('listitem')).toHaveCount(2);
    await checkLayoutAndSecrets(page, sensitive);
    await page.screenshot({ path: testInfo.outputPath('sessions-mobile.png'), fullPage: true, animations: 'disabled' });
    await loginRecord(page, fixture.Target.Client).getByRole('button', { name: /^Revoke login for/ }).click();
    const dialog = revokeDialog(page);
    const bounds = await dialog.boundingBox();
    expect(bounds).not.toBeNull();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(391);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath('sessions-revoke-mobile.png'), fullPage: true, animations: 'disabled' });
    const revokedResponse = page.waitForResponse((response) => response.request().method() === 'POST'
      && new URL(response.url()).pathname === `/admin/v1/sessions/${fixture.Target.Id}/revoke`);
    const refreshedResponse = sessionResponse(page, { DeviceId: fixture.Target.DeviceId, Status: 'active' });
    await dialog.getByRole('button', { name: 'Revoke login', exact: true }).click();
    const response = await revokedResponse;
    expect(response.status()).toBe(200);
    const revoked = await response.json() as RevokeSessionResponse;
    expect(revoked.SessionId).toBe(fixture.Target.Id);
    expect(revoked.CurrentSessionRevoked).toBe(false);
    expect(revoked.RevokedAt).toMatch(/Z$/);
    const refreshed = await readSessions(page, await refreshedResponse, sensitive);
    expect(refreshed.Items.map((item) => item.Id)).toEqual([fixture.Sibling.Id]);
    await expect(dialog).not.toBeVisible();
    await expect(page.getByRole('alert').filter({ hasText: `Login revoked for ${fixture.Member.Name}.` })).toBeVisible();
    expect(mutations).toEqual([{ pathname: `/admin/v1/sessions/${fixture.Target.Id}/revoke`, body: '{}', csrf: true, query: '' }]);
    expect(await tokenStatus(context.request, fixture, fixture.Target.Token)).toBe(401);
    expect(await tokenStatus(context.request, fixture, fixture.Sibling.Token)).toBe(200);
    await choose(page, 'Status', 'Revoked');
    const history = await applyFilters(page, sensitive, { Status: 'revoked' });
    expect(history.Items.map((item) => item.Id)).toEqual([fixture.Target.Id]);
    expect(history.Items[0].RevokedAt).toBe(revoked.RevokedAt);
    await expect(loginRecord(page, fixture.Target.Client).getByRole('button', { name: /^Revoke login for/ })).toHaveCount(0);
  });

  await test.step('allow a later normal login on the same device', async () => {
    const response = await context.request.post('/emby/Users/AuthenticateByName', {
      headers: { Authorization: `Emby Client="${fixture.ReplacementClient}", DeviceId="${fixture.Target.DeviceId}", Device="${fixture.Target.DeviceName}", Version="5.3-session"` },
      data: { Username: fixture.Member.Name, Pw: fixture.Member.Password },
    });
    expect(response.status()).toBe(200);
    const replacement = await response.json() as { AccessToken: string; SessionInfo: { Id: string } };
    expect(typeof replacement.AccessToken === 'string' && replacement.AccessToken.length >= 32).toBe(true);
    sensitive.push(replacement.AccessToken);
    expect(replacement.AccessToken === fixture.Target.Token || replacement.AccessToken === fixture.Sibling.Token).toBe(false);
    expect(await tokenStatus(context.request, fixture, replacement.AccessToken)).toBe(200);
    expect(await tokenStatus(context.request, fixture, fixture.Target.Token)).toBe(401);
    expect(await tokenStatus(context.request, fixture, fixture.Sibling.Token)).toBe(200);
    await choose(page, 'Status', 'Active');
    const active = await applyFilters(page, sensitive, { Status: 'active', DeviceId: fixture.Target.DeviceId });
    expect(new Set(active.Items.map((item) => item.Id))).toEqual(new Set([fixture.Sibling.Id, replacement.SessionInfo.Id]));
    expect(active.Items.some((item) => item.Client === fixture.ReplacementClient)).toBe(true);
    await checkLayoutAndSecrets(page, sensitive);
  });

  await test.step('confirm self-revocation and return to sign-in with no stale administrator UI', async () => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    const active = await clearFilters(page, sensitive);
    const current = active.Items.filter((item: LoginSession) => item.IsCurrent);
    expect(current).toHaveLength(1);
    const currentRecord = page.getByRole('table', { name: 'Login sessions', exact: true }).getByRole('row')
      .filter({ has: page.getByText('This login', { exact: true }) });
    await currentRecord.getByRole('button', { name: /^Revoke login for/ }).click();
    await expect(revokeDialog(page).getByText('This is your current administrator login. Revoking it signs you out of this dashboard.', { exact: true })).toBeVisible();
    const revokedResponse = page.waitForResponse((response) => response.request().method() === 'POST'
      && new URL(response.url()).pathname === `/admin/v1/sessions/${current[0].Id}/revoke`);
    await revokeDialog(page).getByRole('button', { name: 'Revoke login', exact: true }).click();
    const response = await revokedResponse;
    expect(response.status()).toBe(200);
    const result = await response.json() as RevokeSessionResponse;
    expect(result.SessionId).toBe(current[0].Id);
    expect(result.CurrentSessionRevoked).toBe(true);
    await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Sessions', exact: true })).toHaveCount(0);
    await expect(page.getByRole('table', { name: 'Login sessions', exact: true })).toHaveCount(0);
    await expect(revokeDialog(page)).toHaveCount(0);
    expect((await context.cookies()).some((cookie) => cookie.name === 'goby_session')).toBe(false);
    expect((await context.request.get('/admin/v1/sessions')).status()).toBe(401);
    expect(mutations).toHaveLength(2);
    await page.screenshot({ path: testInfo.outputPath('sessions-signed-out.png'), fullPage: true, animations: 'disabled' });
  });
  expect(pageErrors).toEqual([]);
});
