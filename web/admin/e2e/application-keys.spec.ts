import { constants } from 'node:fs';
import { lstat, open, realpath } from 'node:fs/promises';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import type { APIRequestContext, Locator, Page, Response } from '@playwright/test';

interface ApplicationKey {
  Id: string;
  AppName: string;
  CreatedAt: string;
  LastUsedAt: string | null;
  RevokedAt: string | null;
  CreatedBy: string | null;
  IPAddress: string;
  Status: 'active' | 'revoked';
}

interface KeyPage {
  Items: ApplicationKey[];
  TotalRecordCount: number;
  StartIndex: number;
  Limit: number;
}

interface KeyFixture {
  Marker: string;
  RunId: string;
  Origin: string;
  Administrator: { Id: string; Name: string };
  Sibling: { Id: string; AppName: string; Token: string };
  SeedCount: number;
  RevokedCount: number;
  HistoricalRevokedIds: string[];
  LiteralName: string;
  LiteralSearch: string;
  TargetName: string;
}

interface PrivateResult {
  Marker: string;
  RunId: string;
  TargetId: string;
  TargetToken: string;
  Complete: boolean;
  AlphaSessionId?: string;
  BetaSessionId?: string;
  RevokedAt?: string;
}

interface ClientSession {
  Id: string;
  Client: string;
  DeviceId: string;
  [field: string]: unknown;
}

const fixtureMarker = 'goby-application-key-browser-fixtures-v1';
const fixturePattern = /^\/opt\/goby-test\/exec-scratch\/goby-application-keys-([0-9]{8}_[0-9]{6}_[0-9a-f]{10})\/browser\/application-key-fixture\.json$/;
const safeFields = ['Id', 'AppName', 'CreatedAt', 'LastUsedAt', 'RevokedAt', 'CreatedBy', 'IPAddress', 'Status'].sort();
const sharedDevice = 'browser-key-shared-device';
const alphaClient = 'Browser key alpha';
const betaClient = 'Browser key beta';

async function loadFixture(filename: string, baseURL: string): Promise<KeyFixture> {
  const match = fixturePattern.exec(filename);
  if (process.platform !== 'linux' || process.getuid?.() !== 0 || !match
    || filename !== path.resolve(filename) || await realpath(filename) !== filename) {
    throw new Error('Application-key fixtures require the isolated Linux runner and its canonical private manifest.');
  }
  for (const directory of [path.dirname(filename), path.dirname(path.dirname(filename))]) {
    const status = await lstat(directory);
    if (!status.isDirectory() || status.isSymbolicLink() || status.uid !== 0 || (status.mode & 0o777) !== 0o700) {
      throw new Error('The application-key fixture directories must be private and root owned.');
    }
  }
  const file = await open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  let fixture: KeyFixture;
  try {
    const status = await file.stat();
    if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o600 || status.nlink !== 1
      || status.size > 64 * 1024 || await realpath(`/proc/self/fd/${file.fd}`) !== filename) {
      throw new Error('The application-key fixture must be a bounded private regular file.');
    }
    fixture = JSON.parse(await file.readFile('utf8')) as KeyFixture;
  } finally {
    await file.close();
  }
  const origin = new URL(baseURL);
  if (!fixture || fixture.Marker !== fixtureMarker || fixture.RunId !== match[1]
    || fixture.RunId !== process.env.GOBY_APPLICATION_KEYS_RUN_ID || fixture.Origin !== origin.origin
    || origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || !origin.port
    || ['5432', '15432', '18096', '18097'].includes(origin.port)
    || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash
    || !fixture.Administrator || !/^[0-9a-f]{32}$/.test(fixture.Administrator.Id)
    || typeof fixture.Administrator.Name !== 'string' || !fixture.Administrator.Name.includes(fixture.RunId)
    || !fixture.Sibling || !/^[1-9][0-9]*$/.test(fixture.Sibling.Id)
    || fixture.Sibling.AppName !== 'Browser sibling application'
    || typeof fixture.Sibling.Token !== 'string' || fixture.Sibling.Token.length < 32
    || fixture.SeedCount !== 32 || fixture.RevokedCount !== 3
    || !Array.isArray(fixture.HistoricalRevokedIds) || fixture.HistoricalRevokedIds.length !== 3
    || !fixture.HistoricalRevokedIds.every((id) => /^[1-9][0-9]*$/.test(id))
    || fixture.LiteralName !== 'Archive 100%_\\ application' || fixture.LiteralSearch !== '100%_\\'
    || fixture.TargetName !== 'Browser \u754c application') {
    throw new Error('The private application-key fixture does not match this disposable run.');
  }
  return fixture;
}

async function saveResult(filename: string, result: PrivateResult, create: boolean) {
  const flags = constants.O_WRONLY | constants.O_NOFOLLOW | (create ? constants.O_CREAT | constants.O_EXCL : 0);
  const file = await open(filename, flags, 0o600);
  try {
    const status = await file.stat();
    if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o600 || status.nlink !== 1
      || await realpath(`/proc/self/fd/${file.fd}`) !== filename) {
      throw new Error('The application-key result must remain a private regular file.');
    }
    await file.truncate(0);
    await file.writeFile(`${JSON.stringify(result)}\n`, 'utf8');
    await file.sync();
  } finally {
    await file.close();
  }
}

function keyResponse(page: Page, parameters: Record<string, string> = {}) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === 'GET' && url.pathname === '/admin/v1/api-keys'
      && Object.entries(parameters).every(([name, value]) => url.searchParams.get(name) === value);
  });
}

function keyMutation(page: Page, pathname: string) {
  return page.waitForResponse((response) => response.request().method() === 'POST'
    && new URL(response.url()).pathname === pathname);
}

function assertKey(key: ApplicationKey) {
  expect(Object.keys(key).sort()).toEqual(safeFields);
  expect(key.Id).toMatch(/^[1-9][0-9]*$/);
  expect(key.AppName.length).toBeGreaterThan(0);
  expect(key.CreatedAt).toMatch(/Z$/);
  expect(key.CreatedBy === null || /^[0-9a-f]{32}$/.test(key.CreatedBy)).toBe(true);
  expect(key.LastUsedAt === null || /Z$/.test(key.LastUsedAt)).toBe(true);
  expect(['active', 'revoked']).toContain(key.Status);
  expect(key.Status === 'revoked' ? typeof key.RevokedAt === 'string' : key.RevokedAt === null).toBe(true);
}

async function readKeys(page: Page, response: Response, secrets: string[], listVisible = true): Promise<KeyPage> {
  expect(response.status()).toBe(200);
  expect(response.headers()['cache-control']).toBe('no-store');
  const raw = await response.text();
  expect(secrets.some((secret) => raw.includes(secret)), 'Native metadata responses must exclude raw authentication secrets.').toBe(false);
  const result = JSON.parse(raw) as KeyPage;
  expect(Object.keys(result).sort()).toEqual(['Items', 'Limit', 'StartIndex', 'TotalRecordCount']);
  expect(Array.isArray(result.Items)).toBe(true);
  expect(result.TotalRecordCount).toBeGreaterThanOrEqual(result.Items.length);
  expect(new Set(result.Items.map((item) => item.Id)).size).toBe(result.Items.length);
  result.Items.forEach(assertKey);
  await expect(page.getByRole('status', { name: 'Loading API keys', exact: true })).not.toBeVisible();
  if (listVisible && result.Items.length > 0) {
    if ((page.viewportSize()?.width ?? 1440) >= 1200) {
      await expect(page.getByRole('table', { name: 'Application API keys', exact: true }).getByRole('row')).toHaveCount(result.Items.length + 1);
    } else {
      await expect(page.getByRole('list', { name: 'Application API keys', exact: true }).getByRole('listitem')).toHaveCount(result.Items.length);
    }
  }
  return result;
}

async function searchKeys(page: Page, text: string, secrets: string[], parameters: Record<string, string> = {}) {
  const response = keyResponse(page, { SearchTerm: text.trim(), StartIndex: '0', ...parameters });
  await page.getByRole('textbox', { name: 'Search API keys', exact: true }).fill(text);
  return readKeys(page, await response, secrets);
}

async function clearFilters(page: Page, secrets: string[]) {
  const response = keyResponse(page, { IncludeRevoked: 'false', StartIndex: '0' });
  await page.getByRole('button', { name: 'Clear filters', exact: true }).first().click();
  return readKeys(page, await response, secrets);
}

function keyRecord(page: Page, name: string): Locator {
  const container = (page.viewportSize()?.width ?? 1440) >= 1200
    ? page.getByRole('table', { name: 'Application API keys', exact: true }).getByRole('row')
    : page.getByRole('list', { name: 'Application API keys', exact: true }).getByRole('listitem');
  return container.filter({ has: page.getByText(name, { exact: true }) });
}

function clientHeaders(token: string, client: string, queryToken = false): Record<string, string> {
  return {
    ...(queryToken ? {} : { 'X-Emby-Token': token }),
    'X-Emby-Client': client, 'X-Emby-Device-Id': sharedDevice,
    'X-Emby-Device-Name': 'Browser fixture', 'X-Emby-Client-Version': 'application-key-browser-1',
  };
}

async function keyStatus(request: APIRequestContext, token: string, client: string, queryToken = false) {
  return (await request.get('/emby/Users', {
    headers: clientHeaders(token, client, queryToken), params: queryToken ? { api_key: token } : undefined,
  })).status();
}

async function inspectClient(request: APIRequestContext, token: string, client: string, queryToken: boolean) {
  const options = { headers: clientHeaders(token, client, queryToken), params: queryToken ? { api_key: token } : undefined };
  const users = await request.get('/emby/Users', options);
  expect(users.status()).toBe(200);
  expect(Array.isArray(await users.json())).toBe(true);
  const items = await request.get('/emby/Items', options);
  expect(items.status()).toBe(200);
  const catalog = await items.json() as { Items: unknown[]; TotalRecordCount: number };
  expect(catalog.Items).toEqual([]);
  expect(catalog.TotalRecordCount).toBe(0);
  const sessions = await request.get('/emby/Sessions', options);
  expect(sessions.status()).toBe(200);
  const rows = await sessions.json() as ClientSession[];
  const own = rows.filter((row) => row.Client === client && row.DeviceId === sharedDevice);
  expect(own).toHaveLength(1);
  expect(own[0].Id).toMatch(/^[0-9a-f]{32}$/);
  expect(['UserId', 'UserName', 'ExpiresAt', 'CredentialId', 'ApplicationKeyId', 'AccessToken'].some((field) => field in own[0])).toBe(false);
  return own[0].Id;
}

async function checkSecretsAndLayout(page: Page, secrets: string[]) {
  const snapshot = await page.evaluate(() => ({
    dom: document.documentElement.outerHTML,
    values: [...document.querySelectorAll('input, textarea')].map((element) => (element as HTMLInputElement).value),
    local: JSON.stringify(localStorage), session: JSON.stringify(sessionStorage), url: location.href,
  }));
  const serialized = JSON.stringify(snapshot);
  expect(secrets.some((secret) => serialized.includes(secret)), 'Closed key dialogs must leave no raw secret in DOM, form values, URL, or browser storage.').toBe(false);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect(await page.locator('#main-content').evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
}

// Automatic screenshots can capture a secret dialog at an assertion failure.
// Export only the four explicit screenshots below. Traces, videos, and network
// attachments remain disabled; the runner removes all private transient files.
test.use({ screenshot: 'off', trace: 'off', video: 'off' });

test('isolated application keys create, reveal, filter, page, authorize clients, and revoke', async ({ page, context, request }, testInfo) => {
  test.skip(process.env.GOBY_SMOKE_APPLICATION_KEYS_DISPOSABLE_DATABASE !== '1'
    || process.env.GOBY_SMOKE_APPLICATION_KEYS_DEDICATED_ADMIN !== '1',
  'The isolated runner must confirm its disposable database and dedicated administrator.');
  test.setTimeout(210_000);
  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  const baseURL = process.env.GOBY_SMOKE_BASE_URL;
  const manifestPath = process.env.GOBY_SMOKE_APPLICATION_KEYS_FIXTURE_MANIFEST;
  if (!name || !password || !baseURL || !manifestPath) throw new Error('The private application-key environment is incomplete.');
  const fixture = await loadFixture(manifestPath, baseURL);
  expect(new URL(testInfo.project.use.baseURL ?? '').origin).toBe(fixture.Origin);
  expect(name).toBe(fixture.Administrator.Name);
  const resultPath = path.join(path.dirname(manifestPath), 'application-key-result.json');
  const sensitive = [password, fixture.Sibling.Token];
  const pageErrors: string[] = [];
  const consoleMessages: string[] = [];
  const browserURLs: string[] = [];
  const mutations: { pathname: string; body: string | null; csrf: boolean; query: string }[] = [];
  const listQueries: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('console', (message) => consoleMessages.push(message.text()));
  page.on('request', (outgoing) => {
    const url = new URL(outgoing.url());
    browserURLs.push(outgoing.url());
    if (outgoing.method() === 'GET' && url.pathname === '/admin/v1/api-keys') listQueries.push(url.search);
    if (outgoing.method() === 'POST' && /^\/admin\/v1\/api-keys(?:\/[1-9][0-9]*\/(?:reveal|revoke))?$/.test(url.pathname)) {
      mutations.push({ pathname: url.pathname, body: outgoing.postData(), csrf: Boolean(outgoing.headers()['x-csrf-token']), query: url.search });
    }
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto('/admin/api-keys');
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
  await page.getByLabel(/^Username/).fill(name);
  await page.getByLabel(/^Password/).fill(password);
  const initialResponse = keyResponse(page, { IncludeRevoked: 'false', StartIndex: '0', Limit: '50' });
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'API keys', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'API keys', exact: true })).toHaveAttribute('href', '/admin/api-keys');
  await expect(page.getByRole('link', { name: 'API keys', exact: true })).toHaveAttribute('aria-current', 'page');
  const initial = await readKeys(page, await initialResponse, sensitive);
  expect(initial.TotalRecordCount).toBe(fixture.SeedCount - fixture.RevokedCount);
  expect(initial.Items.every((item) => item.Status === 'active')).toBe(true);
  const sessionResponse = await context.request.get('/admin/v1/session');
  expect(sessionResponse.status()).toBe(200);
  const session = await sessionResponse.json() as { CSRFToken: string };
  expect(typeof session.CSRFToken === 'string' && session.CSRFToken.length >= 32).toBe(true);
  sensitive.push(session.CSRFToken);
  const nativeHeaders = { 'X-CSRF-Token': session.CSRFToken, Origin: fixture.Origin };
  let result!: PrivateResult;

  await test.step('cancel creation and enforce the application-name UTF-8 byte limit without a POST', async () => {
    await page.getByRole('button', { name: 'Create API key', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Create API key', exact: true });
    const input = dialog.getByRole('textbox', { name: 'Application name', exact: true });
    await input.fill('\u754c'.repeat(86));
    await expect(dialog.getByText('Use at most 256 UTF-8 bytes. Some characters use more than one byte.', { exact: true })).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Create API key', exact: true })).toBeDisabled();
    await input.fill('\u754c'.repeat(85));
    await expect(dialog.getByRole('button', { name: 'Create API key', exact: true })).toBeEnabled();
    await input.fill('Canceled browser application');
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(mutations).toHaveLength(0);
    const current = await context.request.get('/admin/v1/api-keys?IncludeRevoked=true&Limit=200');
    expect(current.status()).toBe(200);
    expect((await current.json() as KeyPage).TotalRecordCount).toBe(fixture.SeedCount);
  });

  await test.step('create one real key and safely handle an unavailable clipboard', async () => {
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true, value: { writeText: async () => { throw new Error('Clipboard denied by isolated test'); } },
      });
    });
    await page.getByRole('button', { name: 'Create API key', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Create API key', exact: true });
    await dialog.getByRole('textbox', { name: 'Application name', exact: true }).fill(`  ${fixture.TargetName}  `);
    const createdResponse = keyMutation(page, '/admin/v1/api-keys');
    const refreshedResponse = keyResponse(page, { StartIndex: '0', Limit: '50' });
    await dialog.getByRole('button', { name: 'Create API key', exact: true }).click();
    const response = await createdResponse;
    expect(response.status()).toBe(201);
    expect(response.headers()['cache-control']).toBe('no-store');
    const created = await response.json() as { Key: ApplicationKey; AccessToken: string };
    expect(Object.keys(created).sort()).toEqual(['AccessToken', 'Key']);
    assertKey(created.Key);
    expect(created.Key.AppName).toBe(fixture.TargetName);
    expect(created.Key.CreatedBy).toBe(fixture.Administrator.Id);
    expect(created.Key.Status).toBe('active');
    expect(typeof created.AccessToken === 'string' && created.AccessToken.length >= 32).toBe(true);
    expect(created.AccessToken === fixture.Sibling.Token).toBe(false);
    sensitive.push(created.AccessToken);
    result = { Marker: 'goby-application-key-browser-result-v1', RunId: fixture.RunId,
      TargetId: created.Key.Id, TargetToken: created.AccessToken, Complete: false };
    await saveResult(resultPath, result, true);
    expect((await readKeys(page, await refreshedResponse, sensitive, false)).TotalRecordCount).toBe(fixture.SeedCount - fixture.RevokedCount + 1);
    const confirmation = page.getByRole('dialog', { name: 'API key created', exact: true });
    const secret = confirmation.getByRole('textbox', { name: 'Access token', exact: true });
    await expect(secret).toBeVisible();
    expect(await secret.inputValue() === created.AccessToken).toBe(true);
    await expect(secret).toHaveAttribute('readonly', '');
    await confirmation.getByRole('button', { name: 'Copy key', exact: true }).click();
    await expect(confirmation.getByRole('status').filter({ hasText: 'Clipboard access is unavailable.' })).toBeVisible();
    expect(await secret.evaluate((element) => {
      const input = element as HTMLTextAreaElement;
      return document.activeElement === input && input.selectionStart === 0 && input.selectionEnd === input.value.length;
    })).toBe(true);
    await confirmation.getByRole('button', { name: 'Select key', exact: true }).click();
    expect(await secret.evaluate((element) => {
      const input = element as HTMLTextAreaElement;
      return input.selectionStart === 0 && input.selectionEnd === input.value.length;
    })).toBe(true);
    await page.screenshot({ path: testInfo.outputPath('application-keys-created-masked.png'), fullPage: true,
      animations: 'disabled', mask: [page.locator('input'), page.locator('textarea')] });
    await confirmation.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(confirmation).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Access token', exact: true })).toHaveCount(0);
    await checkSecretsAndLayout(page, sensitive);
    expect(mutations).toEqual([{ pathname: '/admin/v1/api-keys', body: JSON.stringify({ AppName: fixture.TargetName }), csrf: true, query: '' }]);
  });

  await test.step('page real keys in stable nonoverlapping 25-record pages and include revoked history', async () => {
    const resizedResponse = keyResponse(page, { StartIndex: '0', Limit: '25' });
    await page.getByRole('combobox', { name: /Keys per page/ }).click();
    await page.getByRole('option', { name: '25', exact: true }).click();
    const first = await readKeys(page, await resizedResponse, sensitive);
    expect(first.Items).toHaveLength(25);
    const nextResponse = keyResponse(page, { StartIndex: '25', Limit: '25' });
    await page.getByRole('button', { name: 'Go to next page', exact: true }).click();
    const second = await readKeys(page, await nextResponse, sensitive);
    expect(second.TotalRecordCount).toBe(first.TotalRecordCount);
    expect(second.Items).toHaveLength(5);
    const firstIds = new Set(first.Items.map((item) => item.Id));
    expect(second.Items.some((item) => firstIds.has(item.Id))).toBe(false);
    await expect(page.getByRole('button', { name: 'Go to next page', exact: true })).toBeDisabled();
    const previousResponse = keyResponse(page, { StartIndex: '0', Limit: '25' });
    await page.getByRole('button', { name: 'Go to previous page', exact: true }).click();
    expect((await readKeys(page, await previousResponse, sensitive)).Items.map((item) => item.Id)).toEqual(first.Items.map((item) => item.Id));
    const historyResponse = keyResponse(page, { IncludeRevoked: 'true', StartIndex: '0', Limit: '25' });
    await page.getByRole('switch', { name: 'Include revoked', exact: true }).check();
    const history = await readKeys(page, await historyResponse, sensitive);
    expect(history.TotalRecordCount).toBe(fixture.SeedCount + 1);
    expect(history.Items.filter((item) => item.Status === 'revoked').map((item) => item.Id).sort()).toEqual([...fixture.HistoricalRevokedIds].sort());
    const refreshResponse = keyResponse(page, { IncludeRevoked: 'true', StartIndex: '0', Limit: '25' });
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    expect((await readKeys(page, await refreshResponse, sensitive)).Items.map((item) => item.Id)).toEqual(history.Items.map((item) => item.Id));
  });

  await test.step('search literal wildcard characters and enforce the UTF-8 search bound', async () => {
    const literal = await searchKeys(page, fixture.LiteralSearch, sensitive);
    expect(literal.TotalRecordCount).toBe(1);
    expect(literal.Items[0].AppName).toBe(fixture.LiteralName);
    const before = listQueries.length;
    const search = page.getByRole('textbox', { name: 'Search API keys', exact: true });
    await search.fill('\u754c'.repeat(86));
    await expect(page.getByText('Use at most 256 UTF-8 bytes. Some characters use more than one byte.', { exact: true })).toBeVisible();
    await search.press('Enter');
    // Wait beyond the page's 300 ms debounce to prove an invalid multibyte
    // input cannot emit a delayed native list request.
    await page.waitForTimeout(400);
    expect(listQueries.length).toBe(before);
    const empty = await searchKeys(page, '\u754c'.repeat(85), sensitive);
    expect(empty.TotalRecordCount).toBe(0);
    await expect(page.getByRole('heading', { name: 'No matching API keys', exact: true })).toBeVisible();
    const target = await searchKeys(page, '\u754c', sensitive);
    expect(target.Items.map((item) => item.Id)).toEqual([result.TargetId]);
    await checkSecretsAndLayout(page, sensitive);
    await page.screenshot({ path: testInfo.outputPath('application-keys-desktop.png'), fullPage: true, animations: 'disabled' });
  });

  await test.step('reveal the original issued secret and clear it when the dialog closes', async () => {
    const revealedResponse = keyMutation(page, `/admin/v1/api-keys/${result.TargetId}/reveal`);
    await keyRecord(page, fixture.TargetName).getByRole('button', { name: `Reveal key for ${fixture.TargetName}`, exact: true }).click();
    const response = await revealedResponse;
    expect(response.status()).toBe(200);
    expect(response.headers()['cache-control']).toBe('no-store');
    const revealed = await response.json() as { Id: string; AccessToken: string };
    expect(Object.keys(revealed).sort()).toEqual(['AccessToken', 'Id']);
    expect(revealed.Id).toBe(result.TargetId);
    expect(revealed.AccessToken === result.TargetToken).toBe(true);
    const dialog = page.getByRole('dialog', { name: 'Reveal API key', exact: true });
    expect(await dialog.getByRole('textbox', { name: 'Access token', exact: true }).inputValue() === result.TargetToken).toBe(true);
    await dialog.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await checkSecretsAndLayout(page, sensitive);
    expect(mutations[1]).toEqual({ pathname: `/admin/v1/api-keys/${result.TargetId}/reveal`, body: '{}', csrf: true, query: '' });
  });

  await test.step('authorize separate userless client contexts with headers and query credentials while excluding native administration', async () => {
    result.AlphaSessionId = await inspectClient(request, result.TargetToken, alphaClient, false);
    result.BetaSessionId = await inspectClient(request, result.TargetToken, betaClient, true);
    expect(result.AlphaSessionId === result.BetaSessionId).toBe(false);
    expect(await inspectClient(request, result.TargetToken, alphaClient, true)).toBe(result.AlphaSessionId);
    expect(await inspectClient(request, result.TargetToken, betaClient, false)).toBe(result.BetaSessionId);
    expect(await keyStatus(request, fixture.Sibling.Token, 'Browser key sibling')).toBe(200);
    const rejectedHeaders: Record<string, string>[] = [
      { 'X-Emby-Token': result.TargetToken }, { Cookie: `goby_session=${result.TargetToken}` },
    ];
    for (const headers of rejectedHeaders) {
      expect((await request.get('/admin/v1/api-keys', { headers })).status()).toBe(401);
      expect((await request.post('/admin/v1/api-keys', { headers, data: { AppName: 'Denied key application' } })).status()).toBe(401);
      expect((await request.post(`/admin/v1/api-keys/${fixture.Sibling.Id}/reveal`, { headers, data: {} })).status()).toBe(401);
      expect((await request.post(`/admin/v1/api-keys/${fixture.Sibling.Id}/revoke`, { headers, data: {} })).status()).toBe(401);
    }
    const usedResponse = keyResponse(page, { SearchTerm: '\u754c' });
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    const used = await readKeys(page, await usedResponse, sensitive);
    expect(used.Items[0].LastUsedAt).toMatch(/Z$/);
    await checkSecretsAndLayout(page, sensitive);
  });

  await test.step('cancel revocation, then revoke every target client on mobile while preserving the sibling key', async () => {
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.getByRole('list', { name: 'Application API keys', exact: true }).getByRole('listitem')).toHaveCount(1);
    await checkSecretsAndLayout(page, sensitive);
    await page.screenshot({ path: testInfo.outputPath('application-keys-mobile.png'), fullPage: true, animations: 'disabled' });
    await keyRecord(page, fixture.TargetName).getByRole('button', { name: `Revoke key for ${fixture.TargetName}`, exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Revoke API key?', exact: true });
    await expect(dialog.getByText(fixture.TargetName, { exact: true })).toBeVisible();
    await expect(dialog.getByText(/This permanently removes access for every client using this key, including playback requests/)).toBeVisible();
    const bounds = await dialog.boundingBox();
    expect(bounds).not.toBeNull();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(391);
    await page.screenshot({ path: testInfo.outputPath('application-keys-revoke-mobile.png'), fullPage: true, animations: 'disabled' });
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(mutations.filter((mutation) => mutation.pathname.endsWith('/revoke'))).toHaveLength(0);
    expect(await keyStatus(request, result.TargetToken, alphaClient)).toBe(200);
    expect(await keyStatus(request, result.TargetToken, betaClient, true)).toBe(200);
    await keyRecord(page, fixture.TargetName).getByRole('button', { name: `Revoke key for ${fixture.TargetName}`, exact: true }).click();
    const revokedResponse = keyMutation(page, `/admin/v1/api-keys/${result.TargetId}/revoke`);
    const refreshedResponse = keyResponse(page, { SearchTerm: '\u754c', IncludeRevoked: 'true' });
    await dialog.getByRole('button', { name: 'Revoke key', exact: true }).click();
    const response = await revokedResponse;
    expect(response.status()).toBe(200);
    const revoked = await response.json() as { Id: string; RevokedAt: string };
    expect(Object.keys(revoked).sort()).toEqual(['Id', 'RevokedAt']);
    expect(revoked.Id).toBe(result.TargetId);
    expect(revoked.RevokedAt).toMatch(/Z$/);
    result.RevokedAt = revoked.RevokedAt;
    const refreshed = await readKeys(page, await refreshedResponse, sensitive);
    expect(refreshed.Items.map((item) => item.Id)).toEqual([result.TargetId]);
    expect(refreshed.Items[0].Status).toBe('revoked');
    expect(refreshed.Items[0].RevokedAt).toBe(revoked.RevokedAt);
    await expect(dialog).toHaveCount(0);
    await expect(keyRecord(page, fixture.TargetName).getByRole('button')).toHaveCount(0);
    expect(mutations[2]).toEqual({ pathname: `/admin/v1/api-keys/${result.TargetId}/revoke`, body: '{}', csrf: true, query: '' });
    expect(await keyStatus(request, result.TargetToken, alphaClient)).toBe(401);
    expect(await keyStatus(request, result.TargetToken, betaClient, true)).toBe(401);
    expect(await keyStatus(request, fixture.Sibling.Token, 'Browser key sibling')).toBe(200);
    const repeated = await context.request.post(`/admin/v1/api-keys/${result.TargetId}/revoke`, { headers: nativeHeaders, data: {} });
    expect(repeated.status()).toBe(200);
    expect(await repeated.json()).toEqual(revoked);
    expect((await context.request.post(`/admin/v1/api-keys/${result.TargetId}/reveal`, { headers: nativeHeaders, data: {} })).status()).toBe(409);
    expect((await clearFilters(page, sensitive)).TotalRecordCount).toBe(fixture.SeedCount - fixture.RevokedCount);
    const active = await searchKeys(page, fixture.TargetName, sensitive, { IncludeRevoked: 'false' });
    expect(active.TotalRecordCount).toBe(0);
    await expect(page.getByRole('heading', { name: 'No matching API keys', exact: true })).toBeVisible();
    await checkSecretsAndLayout(page, sensitive);
  });

  expect(pageErrors.length, 'The browser journey must not raise uncaught page errors.').toBe(0);
  expect(sensitive.some((secret) => [...pageErrors, ...consoleMessages, ...browserURLs].some((value) => value.includes(secret))),
    'Page errors, console messages, and browser request URLs must exclude raw authentication secrets.').toBe(false);
  expect((await context.cookies()).some((cookie) => cookie.value === result.TargetToken || cookie.value === fixture.Sibling.Token)).toBe(false);
  expect(mutations).toHaveLength(3);
  result.Complete = true;
  await saveResult(resultPath, result, false);
});
