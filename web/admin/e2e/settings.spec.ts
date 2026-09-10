import { constants } from 'node:fs';
import { lstat, open, realpath } from 'node:fs/promises';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import type { APIRequestContext, APIResponse, Locator, Page, Response, Route } from '@playwright/test';
import type { LoginSession, ServerNameMode, ServerSettings, SettingsEncoding, SettingsField, SettingsOverrides, SettingsResetField, SettingsValues } from '../src/api';

interface SettingsFixture {
  Marker: string;
  RunId: string;
  Origin: string;
  Administrator: { Id: string; Name: string };
  InitialDefaults: SettingsValues;
  ReplacementDefaults: SettingsValues;
  InitialSettings: ServerSettings;
  FinalOverrides: SettingsOverrides;
  FinalServerNameMode: 'unset';
  FinalEncoding: SettingsEncoding;
  ResultPath: string;
}

interface PrivateResult {
  Marker: string;
  RunId: string;
  Complete: boolean;
  BrowserSessionId?: string;
  BrowserCookie?: string;
  BrowserCSRF?: string;
  BrowserSecrets: string[];
  FinalSettings?: ServerSettings;
  Checks: Record<string, boolean>;
}

const fields: SettingsField[] = ['ServerName', 'MaxBitrate', 'MaxWidth', 'MaxHeight', 'MaxAudioChannels'];
type OutputField = Exclude<SettingsField, 'ServerName'>;
const outputFields: OutputField[] = ['MaxBitrate', 'MaxWidth', 'MaxHeight', 'MaxAudioChannels'];
const resetFields: SettingsResetField[] = [...fields, 'TranscodingMaxWidth'];
const labels: Record<SettingsResetField, string> = {
  ServerName: 'Server name', MaxBitrate: 'Maximum bitrate', MaxWidth: 'Maximum width',
  MaxHeight: 'Maximum height', MaxAudioChannels: 'Maximum audio channels',
  TranscodingMaxWidth: 'Additional video width limit',
};
const fixtureMarker = 'goby-configuration-settings-fixtures-v1';
const fixturePattern = /^\/opt\/goby-test\/exec-scratch\/goby-configuration-settings-([0-9]{8}_[0-9]{6}_[0-9a-f]{10})\/browser\/settings-fixture\.json$/;
const noOverrides: SettingsOverrides = { ServerName: null, MaxBitrate: null, MaxWidth: null, MaxHeight: null, MaxAudioChannels: null };
const fieldRegion = (page: Page, field: SettingsResetField): Locator => page.getByRole('region', { name: labels[field], exact: true });
const fieldInput = (page: Page, field: SettingsResetField): Locator => fieldRegion(page, field).getByRole('textbox', { name: field === 'MaxBitrate' ? 'Maximum bitrate (Mbps)' : labels[field], exact: true });
const defaultSwitch = (page: Page, field: OutputField): Locator => fieldRegion(page, field).getByRole('switch', { name: `Use deployment default for ${labels[field].toLowerCase()}`, exact: true });
const nameChoice = (page: Page, name: 'Deployment default' | 'Host name' | 'Custom name'): Locator => fieldRegion(page, 'ServerName').getByRole('radiogroup', { name: 'Name source', exact: true }).getByRole('radio', { name, exact: true });
const resetDialog = (page: Page): Locator => page.getByRole('dialog', { name: 'Reset saved overrides', exact: true });
const discardDialog = (page: Page): Locator => page.getByRole('dialog', { name: 'Discard unsaved settings changes?', exact: true });

async function loadFixture(filename: string, baseURL: string): Promise<SettingsFixture> {
  const match = fixturePattern.exec(filename);
  if (process.platform !== 'linux' || process.getuid?.() !== 0 || !match
    || filename !== path.resolve(filename) || await realpath(filename) !== filename) {
    throw new Error('Settings verification requires the isolated Linux runner and its canonical private manifest.');
  }
  for (const directory of [path.dirname(filename), path.dirname(path.dirname(filename))]) {
    const status = await lstat(directory);
    if (!status.isDirectory() || status.isSymbolicLink() || status.uid !== 0 || (status.mode & 0o777) !== 0o700) {
      throw new Error('The settings fixture parent directories must be private and root owned.');
    }
  }
  const file = await open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  let fixture: SettingsFixture;
  try {
    const status = await file.stat();
    if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o600 || status.nlink !== 1
      || status.size > 64 * 1024 || await realpath(`/proc/self/fd/${file.fd}`) !== filename) throw new Error('The settings fixture must be a bounded private regular file.');
    fixture = JSON.parse(await file.readFile('utf8')) as SettingsFixture;
  } finally { await file.close(); }
  const origin = new URL(baseURL);
  if (!fixture || fixture.Marker !== fixtureMarker || fixture.RunId !== match[1]
    || fixture.RunId !== process.env.GOBY_CONFIGURATION_SETTINGS_RUN_ID || fixture.Origin !== origin.origin
    || origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || !origin.port
    || ['5432', '15432', '18096', '18097'].includes(origin.port)
    || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash
    || !fixture.Administrator || !/^[0-9a-f]{32}$/.test(fixture.Administrator.Id)
    || typeof fixture.Administrator.Name !== 'string' || !fixture.Administrator.Name.includes(fixture.RunId)
    || fixture.ResultPath !== path.join(path.dirname(filename), 'settings-result.json')
    || !fixture.InitialDefaults || fixture.InitialDefaults.ServerName !== `Settings deployment ${fixture.RunId}`
    || fixture.InitialDefaults.MaxBitrate !== 20000000 || fixture.InitialDefaults.MaxWidth !== 1920
    || fixture.InitialDefaults.MaxHeight !== 1080 || fixture.InitialDefaults.MaxAudioChannels !== 8
    || !fixture.ReplacementDefaults || fixture.ReplacementDefaults.ServerName !== `Settings replacement ${fixture.RunId}`
    || fixture.ReplacementDefaults.MaxBitrate !== 20000000 || fixture.ReplacementDefaults.MaxWidth !== 2560
    || fixture.ReplacementDefaults.MaxHeight !== 1080 || fixture.ReplacementDefaults.MaxAudioChannels !== 6
    || !fixture.FinalOverrides || fixture.FinalOverrides.ServerName !== null || fixture.FinalOverrides.MaxBitrate !== 1234567
    || fixture.FinalOverrides.MaxWidth !== null || fixture.FinalOverrides.MaxHeight !== 720 || fixture.FinalOverrides.MaxAudioChannels !== null
    || fixture.FinalServerNameMode !== 'unset' || fixture.FinalEncoding?.TranscodingMaxWidth !== 640
    || !fixture.InitialSettings || fixture.InitialSettings.ServerNameMode !== 'unset'
    || fixture.InitialSettings.Overrides.ServerName !== null || fixture.InitialSettings.Encoding.TranscodingMaxWidth !== 0) {
    throw new Error('The settings fixture does not match the reviewed startup-default and override contract.');
  }
  return fixture;
}

async function saveResult(filename: string, value: PrivateResult, create = false) {
  const file = await open(filename, constants.O_WRONLY | constants.O_NOFOLLOW | (create ? constants.O_CREAT | constants.O_EXCL : 0), 0o600);
  try {
    const status = await file.stat();
    if (!status.isFile() || status.uid !== 0 || (status.mode & 0o777) !== 0o600 || status.nlink !== 1
      || await realpath(`/proc/self/fd/${file.fd}`) !== filename) throw new Error('The settings browser result must remain private.');
    await file.truncate(0);
    await file.writeFile(`${JSON.stringify(value)}\n`, 'utf8');
    await file.sync();
  } finally { await file.close(); }
}

function settingsResponse(page: Page, method: 'GET' | 'PUT' | 'POST', reset = false): Promise<Response> {
  return page.waitForResponse((response) => response.request().method() === method
    && new URL(response.url()).pathname === `/admin/v1/settings${reset ? '/reset' : ''}`);
}

async function settingsPayload(response: APIResponse | Response, secrets: string[]): Promise<ServerSettings> {
  expect(response.status()).toBe(200);
  expect(response.headers()['cache-control']).toBe('no-store');
  const raw = await response.text();
  expect(secrets.some((secret) => raw.includes(secret)), 'Settings responses must not expose credentials.').toBe(false);
  const value = JSON.parse(raw) as ServerSettings;
  expect(Object.keys(value).sort()).toEqual(['Revision', 'Defaults', 'Overrides', 'Effective', 'Sources', 'UpdatedAt', 'Deployment', 'ServerNameMode', 'Encoding'].sort());
  expect(value.Revision).toMatch(/^[1-9][0-9]*$/);
  expect(value.UpdatedAt).toMatch(/Z$/);
  for (const group of [value.Defaults, value.Overrides, value.Effective, value.Sources]) expect(Object.keys(group).sort()).toEqual([...fields].sort());
  for (const field of outputFields) {
    expect(value.Effective[field]).toBe(value.Overrides[field] ?? value.Defaults[field]);
    expect(value.Sources[field]).toBe(value.Overrides[field] === null ? 'deployment' : 'database');
  }
  expect(Object.keys(value.Deployment).sort()).toEqual(['TranscodingEnabled', 'HardwareDecoder', 'HardwareEncoder', 'Threads', 'MaxJobs', 'MaxUserJobs', 'MaxSessionJobs', 'HostName'].sort());
  expect(value.Deployment.TranscodingEnabled).toBe(false);
  expect(typeof value.Deployment.HostName).toBe('string');
  expect(value.Deployment.HostName.length).toBeGreaterThan(0);
  expect(value.Deployment.HostName.includes('\0')).toBe(false);
  expect(['deployment', 'custom', 'empty', 'unset']).toContain(value.ServerNameMode);
  const expectedName = value.ServerNameMode === 'deployment' ? value.Defaults.ServerName
    : value.ServerNameMode === 'custom' ? value.Overrides.ServerName : value.Deployment.HostName;
  expect(value.Effective.ServerName).toBe(expectedName);
  expect(value.Sources.ServerName).toBe(value.ServerNameMode === 'deployment' ? 'deployment' : 'database');
  if (value.ServerNameMode === 'empty') expect(value.Overrides.ServerName).toBe('');
  else if (value.ServerNameMode !== 'custom') expect(value.Overrides.ServerName).toBeNull();
  else expect(value.Overrides.ServerName).toMatch(/[^\p{White_Space}]/u);
  expect(Object.keys(value.Encoding)).toEqual(['TranscodingMaxWidth']);
  expect(Number.isInteger(value.Encoding.TranscodingMaxWidth)).toBe(true);
  expect(value.Encoding.TranscodingMaxWidth).toBeGreaterThanOrEqual(0);
  expect(value.Encoding.TranscodingMaxWidth).toBeLessThanOrEqual(8192);
  return value;
}

async function readSettings(request: APIRequestContext, secrets: string[]) {
  return settingsPayload(await request.get('/admin/v1/settings'), secrets);
}

async function assertServerName(request: APIRequestContext, expected: string) {
  const publicInfo = await request.get('/emby/System/Info/Public');
  expect(publicInfo.status()).toBe(200);
  expect((await publicInfo.json() as { ServerName: string }).ServerName).toBe(expected);
  const overview = await request.get('/admin/v1/overview');
  expect(overview.status()).toBe(200);
  expect((await overview.json() as { Server: { Name: string } }).Server.Name).toBe(expected);
}

async function setOverride(page: Page, field: SettingsField, value: string) {
  if (field === 'ServerName') await nameChoice(page, 'Custom name').check();
  else await defaultSwitch(page, field).uncheck();
  await expect(fieldInput(page, field)).toBeEnabled();
  await fieldInput(page, field).fill(value);
}

async function assertWidthLimits(page: Page, settings: ServerSettings) {
  const additional = settings.Encoding.TranscodingMaxWidth;
  const combined = additional > 0 ? Math.min(settings.Effective.MaxWidth, additional) : settings.Effective.MaxWidth;
  await expect(fieldInput(page, 'TranscodingMaxWidth')).toHaveValue(String(additional));
  await expect(fieldRegion(page, 'TranscodingMaxWidth').getByText(`Current combined maximum width: ${combined} px`, { exact: true })).toBeVisible();
  await expect(fieldRegion(page, 'TranscodingMaxWidth').getByText(`Combined maximum width after save: ${combined} px`, { exact: true })).toBeVisible();
}

async function saveSettings(page: Page, previous: ServerSettings, overrides: SettingsOverrides, secrets: string[], changes: { mode?: ServerNameMode; additionalWidth?: number } = {}) {
  const response = settingsResponse(page, 'PUT');
  await page.getByRole('button', { name: 'Save settings', exact: true }).click();
  const saved = await response;
  const mode = changes.mode ?? previous.ServerNameMode;
  const encoding = { TranscodingMaxWidth: changes.additionalWidth ?? previous.Encoding.TranscodingMaxWidth };
  expect(saved.request().postDataJSON()).toEqual({ Revision: previous.Revision, Overrides: overrides, ServerNameMode: mode, Encoding: encoding });
  const value = await settingsPayload(saved, secrets);
  expect(value.Overrides).toEqual(overrides);
  expect(value.ServerNameMode).toBe(mode);
  expect(value.Encoding).toEqual(encoding);
  expect(BigInt(value.Revision)).toBe(BigInt(previous.Revision) + 1n);
  await expect(page.getByText('Settings saved.', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
  await assertWidthLimits(page, value);
  return value;
}

async function resetSelected(page: Page, previous: ServerSettings, selected: SettingsResetField[], secrets: string[]) {
  const response = settingsResponse(page, 'POST', true);
  await resetDialog(page).getByRole('button', { name: 'Reset selected settings', exact: true }).click();
  const reset = await response;
  const body = reset.request().postDataJSON() as { Revision: string; Fields: SettingsResetField[] };
  expect(Object.keys(body).sort()).toEqual(['Fields', 'Revision']);
  expect(body.Revision).toBe(previous.Revision);
  expect([...body.Fields].sort()).toEqual([...selected].sort());
  const value = await settingsPayload(reset, secrets);
  expect(BigInt(value.Revision)).toBe(BigInt(previous.Revision) + 1n);
  for (const field of fields) expect(value.Overrides[field]).toBe(selected.includes(field) ? null : previous.Overrides[field]);
  expect(value.ServerNameMode).toBe(selected.includes('ServerName') ? 'deployment' : previous.ServerNameMode);
  expect(value.Encoding.TranscodingMaxWidth).toBe(selected.includes('TranscodingMaxWidth') ? 0 : previous.Encoding.TranscodingMaxWidth);
  await expect(resetDialog(page)).toHaveCount(0);
  return value;
}

async function reloadAfterFailure(page: Page, secrets: string[]) {
  await page.getByRole('button', { name: 'Reload latest settings', exact: true }).click();
  await expect(discardDialog(page)).toBeVisible();
  const response = settingsResponse(page, 'GET');
  await discardDialog(page).getByRole('button', { name: 'Discard draft and reload', exact: true }).click();
  const value = await settingsPayload(await response, secrets);
  await expect(page.getByRole('status', { name: 'Loading settings', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
  return value;
}

async function safeScreenshot(page: Page, filename: string, secrets: string[]) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  const text = await page.locator('body').innerText();
  expect(secrets.some((secret) => text.includes(secret)), 'Settings screenshots must exclude credentials.').toBe(false);
  expect(/postgres(?:ql)?:\/\/|\/dev\/shm\/|\/opt\/goby-test\//.test(text), 'Settings screenshots must exclude deployment paths and connection strings.').toBe(false);
  await page.screenshot({ path: filename, fullPage: true, animations: 'disabled' });
}

// This fixture is created only by verify-configuration-settings.py. The runner retains both
// original native logins for its two restart checks, then revokes them and
// removes its entire owned database and all private credential-bearing files.
// Response loss is injected only after a real PUT commits; no success is mocked.
test('isolated name modes, shared width limits, resets, and explicit conflict recovery', async ({ page, context }, testInfo) => {
  test.skip(process.env.GOBY_SMOKE_CONFIGURATION_SETTINGS_DISPOSABLE_DATABASE !== '1' || process.env.GOBY_SMOKE_CONFIGURATION_SETTINGS_DEDICATED_ADMIN !== '1',
    'The settings runner must confirm its disposable database and dedicated administrator.');
  test.setTimeout(300_000);
  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  const baseURL = process.env.GOBY_SMOKE_BASE_URL;
  const manifestPath = process.env.GOBY_SMOKE_CONFIGURATION_SETTINGS_FIXTURE_MANIFEST;
  if (!name || !password || !baseURL || !manifestPath) throw new Error('The private settings runner environment is incomplete.');
  const fixture = await loadFixture(manifestPath, baseURL);
  expect(new URL(testInfo.project.use.baseURL ?? '').origin).toBe(fixture.Origin);
  expect(name).toBe(fixture.Administrator.Name);
  const result: PrivateResult = { Marker: 'goby-configuration-settings-result-v1', RunId: fixture.RunId, Complete: false, BrowserSecrets: [], Checks: {} };
  await saveResult(fixture.ResultPath, result, true);
  const sensitive = [password];
  const pageErrors: string[] = [];
  const writes: { method: string; pathname: string; csrf: boolean }[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('request', (request) => {
    const pathname = new URL(request.url()).pathname;
    if (['PUT', 'POST'].includes(request.method()) && ['/admin/v1/settings', '/admin/v1/settings/reset'].includes(pathname)) {
      writes.push({ method: request.method(), pathname, csrf: Boolean(request.headers()['x-csrf-token']) });
    }
  });
  try {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto('/admin/settings');
    await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    await page.getByLabel(/^Username/).fill(name);
    await page.getByLabel(/^Password/).fill(password);
    const initialResponse = settingsResponse(page, 'GET');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible();
    let current = await settingsPayload(await initialResponse, sensitive);
    const sessionResponse = await context.request.get('/admin/v1/session');
    expect(sessionResponse.status()).toBe(200);
    const session = await sessionResponse.json() as { CSRFToken: string; User: { Id: string } };
    expect(session.User.Id).toBe(fixture.Administrator.Id);
    const cookie = (await context.cookies()).find((value) => value.name === 'goby_session');
    if (!cookie || typeof session.CSRFToken !== 'string' || !session.CSRFToken) throw new Error('The browser did not receive its independent native administrator credentials.');
    result.BrowserCookie = `goby_session=${cookie.value}`;
    result.BrowserCSRF = session.CSRFToken;
    result.BrowserSecrets = [cookie.value, result.BrowserCookie, session.CSRFToken];
    sensitive.push(...result.BrowserSecrets);
    const sessionsResponse = await context.request.get('/admin/v1/sessions?Kind=admin&Status=active&StartIndex=0&Limit=200');
    expect(sessionsResponse.status()).toBe(200);
    const sessions = (await sessionsResponse.json() as { Items: LoginSession[] }).Items;
    expect(sessions).toHaveLength(2);
    const owned = sessions.filter((login) => login.IsCurrent);
    expect(owned).toHaveLength(1);
    result.BrowserSessionId = owned[0].Id;
    await saveResult(fixture.ResultPath, result);
    const nativeHeaders = { Origin: fixture.Origin, 'X-CSRF-Token': session.CSRFToken };

    await test.step('show the control-seeded unset name as Host name and retain startup defaults', async () => {
      expect(current).toEqual(fixture.InitialSettings);
      expect(current.Defaults).toEqual(fixture.InitialDefaults);
      expect(current.Overrides).toEqual(noOverrides);
      expect(current.Effective).toEqual({ ...fixture.InitialDefaults, ServerName: current.Deployment.HostName });
      expect(current.ServerNameMode).toBe('unset');
      expect(current.Encoding.TranscodingMaxWidth).toBe(0);
      await expect(page.getByRole('link', { name: 'Settings', exact: true })).toHaveAttribute('aria-current', 'page');
      await expect(nameChoice(page, 'Host name')).toBeChecked();
      await expect(fieldInput(page, 'ServerName')).toBeDisabled();
      await expect(fieldInput(page, 'ServerName')).toHaveValue(current.Deployment.HostName);
      for (const field of outputFields) {
        await expect(defaultSwitch(page, field)).toBeChecked();
        await expect(fieldInput(page, field)).toBeDisabled();
        await expect(fieldRegion(page, field).getByText('Deployment default', { exact: true })).toBeVisible();
      }
      await expect(fieldInput(page, 'MaxBitrate')).toHaveValue('20');
      const deployment = page.getByRole('region', { name: 'Deployment configuration', exact: true });
      await expect(deployment.getByText('Read only', { exact: true })).toBeVisible();
      await expect(deployment.getByRole('switch')).toHaveCount(0);
      await expect(deployment.getByRole('textbox')).toHaveCount(0);
      await expect(page.getByText(/Transcoding is currently disabled in deployment configuration/)).toBeVisible();
      await assertServerName(context.request, current.Deployment.HostName);
      await assertWidthLimits(page, current);
      await safeScreenshot(page, testInfo.outputPath('settings-defaults-desktop.png'), sensitive);
      result.Checks.InitialSourcesDefaultsAndReadonlyDeployment = true;
      result.Checks.UnsetNamePresentedAsHostName = true;
    });

    await test.step('preserve unset while saving a precise bitrate and positive or zero additional width', async () => {
      await setOverride(page, 'MaxBitrate', '1.234567');
      await fieldInput(page, 'TranscodingMaxWidth').fill('-1');
      await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
      await fieldInput(page, 'TranscodingMaxWidth').fill('8193');
      await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
      await fieldInput(page, 'TranscodingMaxWidth').fill('0.5');
      await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
      await fieldInput(page, 'TranscodingMaxWidth').fill('640');
      await expect(fieldRegion(page, 'TranscodingMaxWidth').getByText('Combined maximum width after save: 640 px', { exact: true })).toBeVisible();
      current = await saveSettings(page, current, { ...noOverrides, MaxBitrate: 1234567 }, sensitive, { additionalWidth: 640 });
      expect(current.ServerNameMode).toBe('unset');
      expect(current.Overrides.ServerName).toBeNull();
      expect(current.Overrides.MaxWidth).toBeNull();
      expect(current.Effective.MaxWidth).toBe(1920);
      await expect(nameChoice(page, 'Host name')).toBeChecked();
      await assertServerName(context.request, current.Deployment.HostName);
      await fieldInput(page, 'TranscodingMaxWidth').fill('2560');
      await expect(fieldRegion(page, 'TranscodingMaxWidth').getByText('Combined maximum width after save: 1920 px', { exact: true })).toBeVisible();
      current = await saveSettings(page, current, current.Overrides, sensitive, { additionalWidth: 2560 });
      expect(current.ServerNameMode).toBe('unset');
      expect(current.Overrides.MaxWidth).toBeNull();
      expect(current.Effective.MaxWidth).toBe(1920);
      await fieldInput(page, 'TranscodingMaxWidth').fill('0');
      await expect(fieldRegion(page, 'TranscodingMaxWidth').getByText('Combined maximum width after save: 1920 px', { exact: true })).toBeVisible();
      current = await saveSettings(page, current, current.Overrides, sensitive, { additionalWidth: 0 });
      expect(current.ServerNameMode).toBe('unset');
      expect(current.Effective.MaxWidth).toBe(1920);
      expect((await readSettings(context.request, sensitive)).Encoding.TranscodingMaxWidth).toBe(0);
      result.Checks.UnsetNameSurvivesOtherFieldEdits = true;
      result.Checks.PositiveAndZeroAdditionalWidthPreserveNativeWidth = true;
    });

    const customName = `Settings custom ${fixture.RunId}`;
    await test.step('save all five real overrides and preserve exact integer bitrate', async () => {
      await setOverride(page, 'ServerName', customName);
      await setOverride(page, 'MaxBitrate', '1.2345678');
      await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
      await expect(fieldRegion(page, 'MaxBitrate').getByText('Enter 0.000001 to 1000 Mbps using at most 6 decimal places.', { exact: true })).toBeVisible();
      await fieldInput(page, 'MaxBitrate').fill('0.0000001');
      await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
      await fieldInput(page, 'MaxBitrate').fill('1.234567');
      await setOverride(page, 'MaxWidth', '1280');
      await setOverride(page, 'MaxHeight', '720');
      await setOverride(page, 'MaxAudioChannels', '6');
      await fieldInput(page, 'TranscodingMaxWidth').fill('960');
      current = await saveSettings(page, current, { ServerName: customName, MaxBitrate: 1234567, MaxWidth: 1280, MaxHeight: 720, MaxAudioChannels: 6 }, sensitive, { mode: 'custom', additionalWidth: 960 });
      for (const field of fields) await expect(fieldRegion(page, field).getByText('Database override', { exact: true })).toBeVisible();
      await expect(fieldRegion(page, 'MaxBitrate').getByText('1.234567 Mbps', { exact: true })).toBeVisible();
      await assertServerName(context.request, customName);
      expect((await readSettings(context.request, sensitive)).Overrides.MaxBitrate).toBe(1234567);
      await safeScreenshot(page, testInfo.outputPath('settings-overrides-desktop.png'), sensitive);
      result.Checks.FiveOverridesAndExactMbpsInteger = true;
      result.Checks.ServerNamePublishesToPublicInfoAndOverview = true;
    });

    await test.step('reset a subset while preserving other valid and invalid draft fields, then reset all saved overrides', async () => {
      const draftName = `Unsubmitted settings ${fixture.RunId}`;
      await fieldInput(page, 'ServerName').fill(draftName);
      await fieldInput(page, 'MaxHeight').fill('0.5');
      await fieldInput(page, 'MaxWidth').fill('1440');
      await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
      await page.getByRole('button', { name: 'Reset saved overrides', exact: true }).click();
      const dialog = resetDialog(page);
      await dialog.getByRole('checkbox', { name: 'Select all saved overrides', exact: true }).uncheck();
      await dialog.getByRole('checkbox', { name: /^Additional video width limit/ }).check();
      current = await resetSelected(page, current, ['TranscodingMaxWidth'], sensitive);
      expect(current.Encoding.TranscodingMaxWidth).toBe(0);
      expect(current.Overrides.MaxWidth).toBe(1280);
      expect(current.Effective.MaxWidth).toBe(1280);
      await expect(fieldInput(page, 'TranscodingMaxWidth')).toHaveValue('0');
      await expect(fieldInput(page, 'MaxWidth')).toHaveValue('1440');
      await expect(fieldInput(page, 'ServerName')).toHaveValue(draftName);
      await expect(fieldInput(page, 'MaxHeight')).toHaveValue('0.5');
      await expect(fieldRegion(page, 'TranscodingMaxWidth').getByText('Current combined maximum width: 1280 px', { exact: true })).toBeVisible();
      const afterWidthReset = await readSettings(context.request, sensitive);
      expect(afterWidthReset.Encoding.TranscodingMaxWidth).toBe(0);
      expect(afterWidthReset.Overrides.MaxWidth).toBe(1280);
      await page.getByRole('button', { name: 'Reset saved overrides', exact: true }).click();
      await dialog.getByRole('checkbox', { name: 'Select all saved overrides', exact: true }).uncheck();
      await dialog.getByRole('checkbox', { name: /^Maximum width/ }).check();
      current = await resetSelected(page, current, ['MaxWidth'], sensitive);
      await expect(defaultSwitch(page, 'MaxWidth')).toBeChecked();
      await expect(fieldInput(page, 'MaxWidth')).toHaveValue('1920');
      await expect(fieldInput(page, 'ServerName')).toHaveValue(draftName);
      await expect(fieldInput(page, 'MaxHeight')).toHaveValue('0.5');
      await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
      const persisted = await readSettings(context.request, sensitive);
      expect(persisted.Overrides.ServerName).toBe(customName);
      expect(persisted.Overrides.MaxHeight).toBe(720);
      await page.getByRole('button', { name: 'Discard changes', exact: true }).click();
      await discardDialog(page).getByRole('button', { name: 'Discard changes', exact: true }).click();
      await expect(fieldInput(page, 'ServerName')).toHaveValue(customName);
      await expect(fieldInput(page, 'MaxHeight')).toHaveValue('720');
      await setOverride(page, 'MaxWidth', '1280');
      await fieldInput(page, 'TranscodingMaxWidth').fill('960');
      current = await saveSettings(page, current, { ...current.Overrides, MaxWidth: 1280 }, sensitive, { additionalWidth: 960 });
      await page.getByRole('button', { name: 'Reset saved overrides', exact: true }).click();
      for (const field of resetFields) await expect(dialog.getByRole('checkbox', { name: new RegExp(`^${labels[field]}`) })).toBeChecked();
      await safeScreenshot(page, testInfo.outputPath('settings-reset-desktop.png'), sensitive);
      const beforeCancel = writes.length;
      await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
      expect(writes).toHaveLength(beforeCancel);
      expect((await readSettings(context.request, sensitive)).Revision).toBe(current.Revision);
      await page.getByRole('button', { name: 'Reset saved overrides', exact: true }).click();
      current = await resetSelected(page, current, resetFields, sensitive);
      expect(current.Overrides).toEqual(noOverrides);
      expect(current.Effective).toEqual(fixture.InitialDefaults);
      expect(current.ServerNameMode).toBe('deployment');
      expect(current.Encoding.TranscodingMaxWidth).toBe(0);
      await expect(nameChoice(page, 'Deployment default')).toBeChecked();
      await assertWidthLimits(page, current);
      await assertServerName(context.request, fixture.InitialDefaults.ServerName);
      result.Checks.SubsetResetPreservesUnselectedDraft = true;
      result.Checks.ResetCancellationAndFullReset = true;
      result.Checks.AdditionalWidthSubsetResetPreservesNativeDraftAndSavedLimit = true;
      result.Checks.AllSixSettingsResetTogether = true;
    });

    await test.step('save Custom name, Host name, and Deployment default through the three name choices', async () => {
      await setOverride(page, 'ServerName', customName);
      current = await saveSettings(page, current, { ...current.Overrides, ServerName: customName }, sensitive, { mode: 'custom' });
      await nameChoice(page, 'Host name').check();
      await expect(fieldInput(page, 'ServerName')).toHaveValue(current.Deployment.HostName);
      current = await saveSettings(page, current, { ...current.Overrides, ServerName: '' }, sensitive, { mode: 'empty' });
      expect(current.Effective.ServerName).toBe(current.Deployment.HostName);
      await assertServerName(context.request, current.Deployment.HostName);
      await nameChoice(page, 'Deployment default').check();
      current = await saveSettings(page, current, { ...current.Overrides, ServerName: null }, sensitive, { mode: 'deployment' });
      expect(current.Effective.ServerName).toBe(fixture.InitialDefaults.ServerName);
      await assertServerName(context.request, fixture.InitialDefaults.ServerName);
      result.Checks.ThreeNameChoicesAndFourStoredModes = true;
    });

    await test.step('recover a genuine stale revision only after explicit reload', async () => {
      const draftName = `Settings conflict draft ${fixture.RunId}`;
      const winnerName = `Settings concurrent ${fixture.RunId}`;
      await setOverride(page, 'ServerName', draftName);
      const response = await context.request.put('/admin/v1/settings', { headers: nativeHeaders,
        data: { Revision: current.Revision, Overrides: { ...current.Overrides, ServerName: winnerName }, ServerNameMode: 'custom', Encoding: current.Encoding } });
      const winner = await settingsPayload(response, sensitive);
      const before = writes.length;
      const conflict = settingsResponse(page, 'PUT');
      await page.getByRole('button', { name: 'Save settings', exact: true }).click();
      expect((await conflict).status()).toBe(409);
      await expect(page.getByText(/These settings changed after you loaded them/)).toBeVisible();
      await expect(fieldInput(page, 'ServerName')).toHaveValue(draftName);
      await expect(fieldInput(page, 'ServerName')).toBeDisabled();
      await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
      expect(writes).toHaveLength(before + 1);
      expect((await readSettings(context.request, sensitive)).Revision).toBe(winner.Revision);
      await page.getByRole('button', { name: 'Reload latest settings', exact: true }).click();
      await discardDialog(page).getByRole('button', { name: 'Keep editing', exact: true }).click();
      await expect(fieldInput(page, 'ServerName')).toHaveValue(draftName);
      current = await reloadAfterFailure(page, sensitive);
      expect(current.Revision).toBe(winner.Revision);
      await expect(fieldInput(page, 'ServerName')).toHaveValue(winnerName);
      expect(writes).toHaveLength(before + 1);
      result.Checks.RevisionConflictRequiresExplicitReload = true;
    });

    await test.step('discard a real committed PUT response without rewriting the saved settings', async () => {
      const savedName = `Settings after response loss ${fixture.RunId}`;
      await fieldInput(page, 'ServerName').fill(savedName);
      await setOverride(page, 'MaxBitrate', '2.345678');
      await fieldInput(page, 'TranscodingMaxWidth').fill('480');
      const url = `${fixture.Origin}/admin/v1/settings`;
      let committed: ServerSettings | undefined;
      let failure = '';
      let forwarded = 0;
      const intercept = async (route: Route) => {
        if (route.request().method() !== 'PUT' || forwarded > 0) { await route.continue(); return; }
        forwarded += 1;
        const response = await route.fetch({ maxRedirects: 0 });
        if (response.status() !== 200) {
          failure = `The real settings PUT returned HTTP ${response.status()}.`;
          await route.fulfill({ response });
          return;
        }
        committed = await settingsPayload(response, sensitive);
        await response.dispose();
        await route.abort('failed');
      };
      await page.route(url, intercept);
      const before = writes.length;
      try {
        await page.getByRole('button', { name: 'Save settings', exact: true }).click();
        await expect.poll(() => Boolean(committed) || Boolean(failure), { timeout: 20_000 }).toBe(true);
        if (failure) throw new Error(failure);
        await expect(page.getByText(/The result could not be confirmed/)).toBeVisible();
        await expect(page.getByRole('button', { name: 'Save settings', exact: true })).toBeDisabled();
        await expect(fieldInput(page, 'MaxBitrate')).toBeDisabled();
        const stored = await readSettings(context.request, sensitive);
        expect(stored.Revision).toBe(committed?.Revision);
        expect(stored.Overrides.ServerName).toBe(savedName);
        expect(stored.Overrides.MaxBitrate).toBe(2345678);
        expect(stored.ServerNameMode).toBe('custom');
        expect(stored.Encoding.TranscodingMaxWidth).toBe(480);
        await assertServerName(context.request, savedName);
        expect(forwarded).toBe(1);
        expect(writes).toHaveLength(before + 1);
        current = await reloadAfterFailure(page, sensitive);
        expect(current.Revision).toBe(stored.Revision);
        await expect(fieldInput(page, 'MaxBitrate')).toHaveValue('2.345678');
        await assertWidthLimits(page, current);
        expect(writes).toHaveLength(before + 1);
      } finally { await page.unroute(url, intercept); }
      result.Checks.CommittedResponseLossRequiresReloadWithoutRewrite = true;
    });

    await test.step('preserve a mobile draft when navigation and discard are rejected', async () => {
      await page.getByRole('link', { name: 'Overview', exact: true }).click();
      await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
      await page.getByRole('link', { name: 'Settings', exact: true }).click();
      await expect(fieldInput(page, 'ServerName')).toBeEnabled();
      await page.setViewportSize({ width: 390, height: 844 });
      const draftName = `Settings mobile draft ${fixture.RunId}`;
      await fieldInput(page, 'ServerName').fill(draftName);
      const before = writes.length;
      const navigation = page.waitForEvent('dialog', { timeout: 10_000 }).then(async (prompt) => {
        const details = { type: prompt.type(), message: prompt.message() };
        await prompt.dismiss();
        return details;
      });
      const [, dismissed] = await Promise.all([page.goBack({ waitUntil: 'commit', timeout: 10_000 }), navigation]);
      expect(dismissed).toEqual({ type: 'confirm', message: 'Discard unsaved settings changes and leave this page?' });
      await expect(page).toHaveURL(/\/admin\/settings$/);
      await expect(fieldInput(page, 'ServerName')).toHaveValue(draftName);
      await page.getByRole('button', { name: 'Discard changes', exact: true }).click();
      await discardDialog(page).getByRole('button', { name: 'Keep editing', exact: true }).click();
      await expect(fieldInput(page, 'ServerName')).toHaveValue(draftName);
      await safeScreenshot(page, testInfo.outputPath('settings-mobile.png'), sensitive);
      await page.getByRole('button', { name: 'Discard changes', exact: true }).click();
      await discardDialog(page).getByRole('button', { name: 'Discard changes', exact: true }).click();
      expect((await readSettings(context.request, sensitive)).Revision).toBe(current.Revision);
      expect(writes).toHaveLength(before);
      result.Checks.MobileLayoutAndDirtyNavigationGuard = true;
    });

    await test.step('reload a real unset name and preserve it with explicit overrides for both restart checks', async () => {
      // The UI deliberately groups both host-name representations under one
      // choice. Restore the unset representation through the same native API,
      // then prove an ordinary UI save keeps that exact representation.
      const response = await context.request.put('/admin/v1/settings', { headers: nativeHeaders,
        data: { Revision: current.Revision, Overrides: { ...current.Overrides, ServerName: null }, ServerNameMode: 'unset', Encoding: current.Encoding } });
      const unset = await settingsPayload(response, sensitive);
      expect(unset.ServerNameMode).toBe('unset');
      const reloaded = settingsResponse(page, 'GET');
      await page.getByRole('button', { name: 'Reload settings', exact: true }).click();
      current = await settingsPayload(await reloaded, sensitive);
      expect(current).toEqual(unset);
      await expect(nameChoice(page, 'Host name')).toBeChecked();
      await expect(fieldInput(page, 'ServerName')).toBeDisabled();
      await setOverride(page, 'MaxBitrate', '1.234567');
      await defaultSwitch(page, 'MaxWidth').check();
      await setOverride(page, 'MaxHeight', '720');
      await defaultSwitch(page, 'MaxAudioChannels').check();
      await fieldInput(page, 'TranscodingMaxWidth').fill(String(fixture.FinalEncoding.TranscodingMaxWidth));
      current = await saveSettings(page, current, fixture.FinalOverrides, sensitive, { additionalWidth: fixture.FinalEncoding.TranscodingMaxWidth });
      expect(current.Defaults).toEqual(fixture.InitialDefaults);
      expect(current.ServerNameMode).toBe(fixture.FinalServerNameMode);
      expect(current.Encoding).toEqual(fixture.FinalEncoding);
      expect(current.Sources).toEqual({ ServerName: 'database', MaxBitrate: 'database', MaxWidth: 'deployment', MaxHeight: 'database', MaxAudioChannels: 'deployment' });
      await assertServerName(context.request, current.Deployment.HostName);
      expect(await readSettings(context.request, sensitive)).toEqual(current);
      expect(writes.every((write) => write.csrf)).toBe(true);
      expect(pageErrors).toEqual([]);
      result.FinalSettings = current;
      result.Checks.MixedOverridesPreparedForRestartChecks = true;
      result.Checks.UnsetAndPositiveAdditionalWidthPreparedForRestartChecks = true;
      result.Complete = true;
      await saveResult(fixture.ResultPath, result);
    });
  } finally {
    // Keep the original cookie for the runner's restart proof. Its inherited
    // cleanup revokes both native sessions and deletes this private result.
    await saveResult(fixture.ResultPath, result);
  }
});
