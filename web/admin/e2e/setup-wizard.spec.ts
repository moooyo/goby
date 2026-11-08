import type { Page } from '@playwright/test';
import type { Library, LibraryInput } from '../src/api';
import type { ServerPolicy, ServerPolicyInput } from '../src/serverPolicyApi';
import type { DashboardAPI } from './dashboard-fixture';
import { administrator, csrf, expect, json, stamp, test } from './dashboard-fixture';
import { evidence } from './dashboard-review';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'light' } });

const bootstrapPath = '/admin/v1/bootstrap';
const sessionPath = '/admin/v1/session';
const librariesPath = '/admin/v1/libraries';
const rootsPath = '/admin/v1/storage/roots';
const policyPath = '/admin/v1/server-policy';
const previewPath = `${policyPath}/preview`;
const setupToken = 'synthetic-first-run-token';
const password = 'synthetic-first-run-password';
const initialRevision = '9007199254740993';
const libraryOptions = {
  EnableLocalMetadata: true, EnableLocalImages: true, EnableEmbeddedArtwork: true, EnableOnlineImages: false,
  EnableIntroDetection: false, EnableCreditsDetection: false, EnablePreviewGeneration: false,
  EnableBackgroundPreviewGeneration: false, EnableAudioWaveformGeneration: false, EnableSubtitleTimelineGeneration: false,
};

interface SetupState {
  initialized: boolean;
  authenticated: boolean;
  libraries: Library[];
  policy: ServerPolicy;
  wouldDisconnect: boolean;
}

function fixtureLibrary(name = 'First movie library'): Library {
  return {
    Id: '1'.repeat(32), Name: name, CollectionType: 'movies', Paths: ['/media/movies'],
    CreatedAt: stamp, LastScanAt: null, LibraryOptions: { ...libraryOptions },
  };
}

function fixturePolicy(): ServerPolicy {
  return {
    Revision: initialRevision, EnableRemoteAccess: true, LocalNetworkSubnets: ['10.0.0.0/8', '192.168.0.0/16'],
    UICulture: 'zh-CN', UpdatedAt: stamp, Client: { Address: '192.168.10.2', IsLocal: true },
    Restart: { Available: true, InProgress: false },
    Updates: { CanSelfUpdate: false, Delivery: 'docker', ConfiguredSource: false, CurrentVersion: '0.1.0-setup-fixture' },
  };
}

// Every native API request is intercepted by DashboardAPI, including unexpected
// mutations. These fixtures exercise the shipped frontend without a real write.
function installSetup(api: DashboardAPI): SetupState {
  const state: SetupState = { initialized: false, authenticated: false, libraries: [], policy: fixturePolicy(), wouldDisconnect: false };
  api.handlers.set(`GET ${bootstrapPath}`, (route) => json(route, { Initialized: state.initialized, UICulture: 'en-US' }));
  api.handlers.set(`POST ${bootstrapPath}`, (route, request) => {
    expect(state.initialized).toBe(false);
    expect(request.postDataJSON()).toEqual({ SetupToken: setupToken, Name: administrator.Name, Password: password });
    state.initialized = true;
    return json(route, { User: administrator }, 201);
  });
  api.handlers.set(`GET ${sessionPath}`, (route) => state.authenticated
    ? json(route, { User: administrator, CSRFToken: csrf })
    : json(route, { Error: { Code: 'unauthorized', Message: 'Sign in to continue.' } }, 401));
  api.handlers.set(`POST ${sessionPath}`, (route, request) => {
    expect(state.initialized).toBe(true);
    expect(request.postDataJSON()).toEqual({ Name: administrator.Name, Password: password });
    state.authenticated = true;
    return json(route, { User: administrator, CSRFToken: csrf });
  });
  api.handlers.set(`DELETE ${sessionPath}`, (route) => {
    expect(state.authenticated).toBe(true);
    state.authenticated = false;
    return route.fulfill({ status: 204 });
  });
  api.handlers.set(`GET ${librariesPath}`, (route) => {
    expect(state.authenticated, 'Library setup must use the acknowledged administrator session.').toBe(true);
    return json(route, { Items: state.libraries, TotalRecordCount: state.libraries.length });
  });
  api.handlers.set(`GET ${rootsPath}`, (route) => {
    expect(state.authenticated, 'Allowed directories must not be read before administrator authentication.').toBe(true);
    return json(route, { Configured: true, Items: [{ Path: '/media', Available: true, Readable: true, Writable: false }] });
  });
  api.handlers.set(`POST ${librariesPath}`, (route, request) => {
    expect(state.authenticated).toBe(true);
    const input = request.postDataJSON() as LibraryInput;
    const saved: Library = { ...fixtureLibrary(), Name: input.Name, CollectionType: input.CollectionType, Paths: [...input.Paths], LibraryOptions: input.LibraryOptions };
    state.libraries.push(saved);
    return json(route, {
      Library: saved,
      ...(input.Scan ? { Job: { Id: 'setup-scan', LibraryId: saved.Id, ForceProbe: false, Status: 'queued', Error: '', Scanned: 0, Added: 0, Updated: 0, CreatedAt: stamp, StartedAt: null, FinishedAt: null } } : {}),
    }, 201);
  });
  api.handlers.set('GET /admin/v1/storage/directories', (route, request) => {
    expect(state.authenticated).toBe(true);
    const query = new URL(request.url()).searchParams;
    expect(query.get('StartIndex')).toBe('0');
    expect(query.get('Limit')).toBe('100');
    const path = query.get('Path') ?? '';
    expect(['', '/media', '/media/movies']).toContain(path);
    const items = path === '' ? [{ Name: 'media', Path: '/media' }] : path === '/media' ? [{ Name: 'movies', Path: '/media/movies' }] : [];
    return json(route, { Path: path, ...(path === '/media/movies' ? { ParentPath: '/media' } : {}), Items: items, TotalRecordCount: items.length, StartIndex: 0, Limit: 100 });
  });
  api.handlers.set('POST /admin/v1/storage/directories/validate', (route, request) => {
    expect(state.authenticated).toBe(true);
    expect(request.postDataJSON()).toEqual({ Path: '/media/movies' });
    return json(route, { Path: '/media/movies', Available: true });
  });
  api.handlers.set(`GET ${policyPath}`, (route) => {
    expect(state.authenticated).toBe(true);
    return json(route, state.policy);
  });
  api.handlers.set(`POST ${previewPath}`, (route, request) => {
    expect(state.authenticated).toBe(true);
    const input = request.postDataJSON() as ServerPolicyInput;
    expect(input.Revision).toBe(state.policy.Revision);
    return json(route, { Revision: input.Revision, WouldDisconnect: state.wouldDisconnect, Client: state.policy.Client });
  });
  api.handlers.set(`PUT ${policyPath}`, (route, request) => {
    expect(state.authenticated).toBe(true);
    const input = request.postDataJSON() as ServerPolicyInput & { ConfirmDisconnect: boolean };
    expect(input.Revision).toBe(state.policy.Revision);
    expect(input.ConfirmDisconnect).toBe(false);
    state.policy = {
      ...state.policy, Revision: (BigInt(input.Revision) + 1n).toString(), EnableRemoteAccess: input.EnableRemoteAccess,
      LocalNetworkSubnets: [...input.LocalNetworkSubnets], UICulture: input.UICulture, DisconnectExpected: false,
    };
    return json(route, state.policy);
  });
  return state;
}

function writes(api: DashboardAPI, method: string, path: string) {
  return api.writes().filter((request) => request.method === method && request.path === path);
}

async function submitAdministrator(page: Page) {
  await page.goto('/admin/');
  await expect(page.locator('#setup-token')).toBeVisible();
  await expectWizardFits(page);
  await page.locator('#setup-token').fill(setupToken);
  await page.locator('#account-name').fill(administrator.Name);
  await page.locator('#account-password').fill(password);
  await page.locator('.auth-form button[type="submit"]').click();
}

async function openLibraryStep(page: Page) {
  await submitAdministrator(page);
  await expect(page.getByRole('heading', { name: 'Add your first library', exact: true })).toBeVisible();
}

async function skipLibrary(page: Page) {
  await page.getByRole('button', { name: 'Skip for now', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Remote access', exact: true })).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Default interface language', exact: true })).toBeEnabled();
}

async function expectNoStoredCredentials(page: Page) {
  const storage = await page.evaluate(() => JSON.stringify([Object.entries(localStorage), Object.entries(sessionStorage)]));
  for (const secret of [setupToken, password, csrf, administrator.Name]) expect(storage).not.toContain(secret);
}

async function expectClearedLogin(page: Page) {
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Username', exact: true })).toHaveValue('');
  await expect(page.getByLabel('Password', { exact: true })).toHaveValue('');
  await expectNoStoredCredentials(page);
}

async function expectWizardFits(page: Page) {
  const dimensions = await page.evaluate(() => {
    const main = document.querySelector('main')!;
    const dialog = document.querySelector('[role="dialog"]');
    const card = document.querySelector('.auth-card')!.getBoundingClientRect();
    return {
      width: innerWidth, documentWidth: document.documentElement.scrollWidth,
      mainWidth: main.clientWidth, mainScrollWidth: main.scrollWidth,
      dialogWidth: dialog?.clientWidth, dialogScrollWidth: dialog?.scrollWidth,
      cardLeft: card.left, cardRight: card.right,
    };
  });
  expect(dimensions.documentWidth, 'First-run pages must fit the viewport.').toBeLessThanOrEqual(dimensions.width);
  expect(dimensions.mainScrollWidth).toBeLessThanOrEqual(dimensions.mainWidth + 1);
  expect(dimensions.cardLeft, 'The setup card must remain inside the left viewport edge.').toBeGreaterThanOrEqual(0);
  expect(dimensions.cardRight, 'The setup card must remain inside the right viewport edge.').toBeLessThanOrEqual(dimensions.width);
  if (dimensions.dialogWidth !== undefined) expect(dimensions.dialogScrollWidth).toBeLessThanOrEqual(dimensions.dialogWidth + 1);
}

test('skipping the first library completes setup through one temporary administrator session', async ({ page, api }, testInfo) => {
  const state = installSetup(api);
  await openLibraryStep(page);
  await expect(page.getByRole('navigation', { name: 'Administration', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Add library', exact: true })).toBeEnabled();
  await expect.poll(() => api.reads(librariesPath).length).toBeGreaterThan(0);
  await expect.poll(() => api.reads(rootsPath).length).toBeGreaterThan(0);
  await expectNoStoredCredentials(page);
  await evidence(page, testInfo, 'setup-first-library-empty-en-light');
  await skipLibrary(page);
  await expect(page.getByRole('button', { name: 'Continue', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Your server is ready', exact: true })).toBeVisible();
  await evidence(page, testInfo, 'setup-ready-without-library-en-light');
  await page.getByRole('button', { name: 'Finish setup', exact: true }).click();
  await expectClearedLogin(page);
  await expect(page.getByText('Your administrator account is ready. Sign in to continue.', { exact: true })).toBeVisible();
  expect(state.authenticated).toBe(false);
  expect(api.writes()).toEqual([
    { method: 'POST', path: bootstrapPath, body: { SetupToken: setupToken, Name: administrator.Name, Password: password }, csrf: undefined },
    { method: 'POST', path: sessionPath, body: { Name: administrator.Name, Password: password }, csrf: undefined },
    { method: 'DELETE', path: sessionPath, body: null, csrf },
  ]);
});

test('acknowledged bootstrap clears sensitive inputs while the automatic sign-in is pending', async ({ page, api }) => {
  const state = installSetup(api);
  let releaseLogin = () => {};
  const loginResponse = new Promise<void>((resolve) => { releaseLogin = resolve; });
  api.handlers.set(`POST ${sessionPath}`, async (route, request) => {
    expect(state.initialized).toBe(true);
    expect(request.postDataJSON()).toEqual({ Name: administrator.Name, Password: password });
    await loginResponse;
    state.authenticated = true;
    await json(route, { User: administrator, CSRFToken: csrf });
  });
  await submitAdministrator(page);
  try {
    await expect.poll(() => writes(api, 'POST', sessionPath).length).toBe(1);
    await expect(page.locator('#setup-token')).toHaveValue('');
    await expect(page.locator('#account-password')).toHaveValue('');
    expect(api.reads(librariesPath)).toEqual([]);
    expect(api.reads(rootsPath)).toEqual([]);
    await expectNoStoredCredentials(page);
  } finally { releaseLogin(); }
  await expect(page.getByRole('heading', { name: 'Add your first library', exact: true })).toBeVisible();
  expect(writes(api, 'POST', bootstrapPath)).toHaveLength(1);
  expect(writes(api, 'POST', sessionPath)).toHaveLength(1);
});

test('the first library reuses directory browsing, validation, scan defaults, and the existing creation contract', async ({ page, api }, testInfo) => {
  const state = installSetup(api);
  await openLibraryStep(page);
  await page.getByRole('button', { name: 'Add library', exact: true }).click();
  const create = page.getByRole('dialog', { name: 'Create library', exact: true });
  await create.getByRole('textbox', { name: 'Library name', exact: true }).fill('First movie library');
  await expect(create.getByRole('checkbox', { name: /^Scan after creating/ })).toBeChecked();
  await create.getByRole('button', { name: 'Browse directories', exact: true }).click();
  const picker = page.getByRole('dialog', { name: 'Choose a media directory', exact: true });
  await picker.getByRole('list', { name: 'Subdirectories', exact: true }).getByRole('button').first().click();
  await expect(picker.getByRole('textbox', { name: 'Directory path', exact: true })).toHaveValue('/media');
  await picker.getByRole('list', { name: 'Subdirectories', exact: true }).getByRole('button', { name: /movies/ }).click();
  await expect(picker.getByRole('textbox', { name: 'Directory path', exact: true })).toHaveValue('/media/movies');
  await evidence(page, testInfo, 'setup-directory-picker-en-light');
  await picker.getByRole('button', { name: 'Use directory', exact: true }).click();
  await expect(picker).not.toBeVisible();
  await expect(create.getByRole('textbox', { name: 'Media directories', exact: true })).toHaveValue('/media/movies');
  await create.getByRole('button', { name: 'Create library', exact: true }).click();
  await expect(create).not.toBeVisible();
  await expect(page.getByText('First movie library', { exact: true }).first()).toBeVisible();
  await expect(page.getByRole('button', { name: 'Continue', exact: true })).toBeEnabled();
  await evidence(page, testInfo, 'setup-created-first-library-en-light');
  expect(writes(api, 'POST', '/admin/v1/storage/directories/validate')).toEqual([{ method: 'POST', path: '/admin/v1/storage/directories/validate', body: { Path: '/media/movies' }, csrf }]);
  expect(writes(api, 'POST', librariesPath)).toEqual([{
    method: 'POST', path: librariesPath,
    body: { Name: 'First movie library', CollectionType: 'movies', Paths: ['/media/movies'], Scan: true, LibraryOptions: libraryOptions }, csrf,
  }]);
  expect(state.libraries).toHaveLength(1);
  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Remote access', exact: true })).toBeVisible();
});

test('an existing library can be selected without submitting another creation', async ({ page, api }) => {
  const state = installSetup(api);
  state.libraries = [fixtureLibrary('Existing collection')];
  await openLibraryStep(page);
  await page.getByRole('button', { name: 'Use Existing collection', exact: true }).click();
  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Remote access', exact: true })).toBeVisible();
  expect(writes(api, 'POST', librariesPath)).toEqual([]);
});

test('network setup previews and saves the exact CAS revision with CSRF before advancing', async ({ page, api }, testInfo) => {
  const state = installSetup(api);
  await openLibraryStep(page);
  await skipLibrary(page);
  await page.getByRole('textbox', { name: 'Local network subnets', exact: true }).fill('10.0.0.0/8\n172.16.0.0/12');
  await page.getByRole('combobox', { name: 'Default interface language', exact: true }).selectOption('en-US');
  await evidence(page, testInfo, 'setup-network-policy-draft-en-light');
  await page.getByRole('button', { name: 'Save and continue', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Your server is ready', exact: true })).toBeVisible();
  const input = { Revision: initialRevision, EnableRemoteAccess: true, LocalNetworkSubnets: ['10.0.0.0/8', '172.16.0.0/12'], UICulture: 'en-US' };
  expect(writes(api, 'POST', previewPath)).toEqual([{ method: 'POST', path: previewPath, body: input, csrf }]);
  expect(writes(api, 'PUT', policyPath)).toEqual([{ method: 'PUT', path: policyPath, body: { ...input, ConfirmDisconnect: false }, csrf }]);
  expect(state.policy.Revision).toBe('9007199254740994');
  expect(writes(api, 'PUT', '/admin/v1/settings')).toEqual([]);
});

test('an unconfirmed network save must read back the saved revision before setup can continue', async ({ page, api }, testInfo) => {
  const state = installSetup(api);
  api.handlers.set(`PUT ${policyPath}`, async (route, request) => {
    expect(state.authenticated).toBe(true);
    const input = request.postDataJSON() as ServerPolicyInput & { ConfirmDisconnect: boolean };
    expect(input.Revision).toBe(state.policy.Revision);
    expect(input.ConfirmDisconnect).toBe(false);
    state.policy = {
      ...state.policy, Revision: (BigInt(input.Revision) + 1n).toString(), EnableRemoteAccess: input.EnableRemoteAccess,
      LocalNetworkSubnets: [...input.LocalNetworkSubnets], UICulture: input.UICulture, DisconnectExpected: false,
    };
    await route.abort('connectionreset');
  });
  await openLibraryStep(page);
  await skipLibrary(page);
  const subnets = page.getByRole('textbox', { name: 'Local network subnets', exact: true });
  const language = page.getByRole('combobox', { name: 'Default interface language', exact: true });
  await subnets.fill('10.0.0.0/8\n172.16.0.0/12');
  await language.selectOption('en-US');
  await page.getByRole('button', { name: 'Save and continue', exact: true }).click();
  const review = page.getByText('The saved policy must be read again before another change. Your draft has not been resubmitted.', { exact: true });
  await expect(review).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Remote access', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save and continue', exact: true })).toHaveCount(0);
  await expect(subnets).toBeDisabled();
  await expect(language).toBeDisabled();
  await expect(subnets).toHaveValue('10.0.0.0/8\n172.16.0.0/12');
  await expect(language).toHaveValue('en-US');
  expect(writes(api, 'PUT', policyPath)).toHaveLength(1);
  expect(state.policy.Revision).toBe('9007199254740994');
  await evidence(page, testInfo, 'setup-network-save-unconfirmed-en-light');
  const readsBeforeReload = api.reads(policyPath).length;
  await page.getByRole('button', { name: 'Reload saved settings', exact: true }).click();
  await expect(review).not.toBeVisible();
  await expect(language).toBeEnabled();
  await expect(language).toHaveValue('en-US');
  await expect(subnets).toHaveValue('10.0.0.0/8\n172.16.0.0/12');
  expect(api.reads(policyPath).length).toBeGreaterThan(readsBeforeReload);
  await expect(page.getByRole('button', { name: 'Save and continue', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Your server is ready', exact: true })).toBeVisible();
  expect(writes(api, 'POST', previewPath)).toHaveLength(1);
  expect(writes(api, 'PUT', policyPath)).toHaveLength(1);
});

test('network setup refuses a previewed self-disconnection and can keep the saved settings', async ({ page, api }, testInfo) => {
  const state = installSetup(api);
  state.wouldDisconnect = true;
  state.policy.Client = { Address: '203.0.113.17', IsLocal: false };
  await openLibraryStep(page);
  await skipLibrary(page);
  await page.getByRole('switch', { name: 'Allow remote access', exact: true }).uncheck();
  await page.getByRole('button', { name: 'Save and continue', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Remote access', exact: true })).toBeVisible();
  await expect(page.getByRole('alert')).toContainText(/disconnect|block/i);
  expect(writes(api, 'POST', previewPath)).toHaveLength(1);
  expect(writes(api, 'PUT', policyPath)).toEqual([]);
  await evidence(page, testInfo, 'setup-network-policy-connection-preserved-en-light');
  await page.getByRole('button', { name: 'Keep current settings', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Your server is ready', exact: true })).toBeVisible();
  expect(state.policy.EnableRemoteAccess).toBe(true);
  expect(writes(api, 'PUT', policyPath)).toEqual([]);
});

test('an unconfirmed finish checks the ended session before returning to a cleared login', async ({ page, api }, testInfo) => {
  const state = installSetup(api);
  api.handlers.set(`DELETE ${sessionPath}`, async (route) => {
    expect(state.authenticated).toBe(true);
    state.authenticated = false;
    await route.abort('connectionreset');
  });
  await openLibraryStep(page);
  await skipLibrary(page);
  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Your server is ready', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Finish setup', exact: true }).click();
  await expect(page.getByText('Finishing setup could not be confirmed. Check your session before trying again.', { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Your server is ready', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Finish setup', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Back', exact: true })).toBeDisabled();
  expect(writes(api, 'DELETE', sessionPath)).toHaveLength(1);
  await evidence(page, testInfo, 'setup-finish-unconfirmed-en-light');
  const readsBeforeCheck = api.reads(sessionPath).length;
  const checkedSession = page.waitForResponse((response) => response.request().method() === 'GET' && new URL(response.url()).pathname === sessionPath);
  await page.getByRole('button', { name: 'Check session', exact: true }).click();
  expect((await checkedSession).status()).toBe(401);
  await expectClearedLogin(page);
  expect(api.reads(sessionPath)).toHaveLength(readsBeforeCheck + 1);
  expect(state.authenticated).toBe(false);
  expect(writes(api, 'DELETE', sessionPath)).toEqual([{ method: 'DELETE', path: sessionPath, body: null, csrf }]);
});

test('an unconfirmed library creation can be checked and selected without creating it again', async ({ page, api }, testInfo) => {
  const state = installSetup(api);
  api.handlers.set(`POST ${librariesPath}`, async (route) => {
    state.libraries = [fixtureLibrary()];
    await route.abort('connectionreset');
  });
  await openLibraryStep(page);
  await page.getByRole('button', { name: 'Add library', exact: true }).click();
  const create = page.getByRole('dialog', { name: 'Create library', exact: true });
  await create.getByRole('textbox', { name: 'Library name', exact: true }).fill('First movie library');
  await create.getByRole('textbox', { name: 'Media directories', exact: true }).fill('/media/movies');
  await create.getByRole('button', { name: 'Create library', exact: true }).click();
  await expect(create).toContainText('This library may already have been created. Check the library list before trying again.');
  await expect(create.getByRole('button', { name: 'Create library', exact: true })).toHaveCount(0);
  const readsBeforeCheck = api.reads(librariesPath).length;
  await create.getByRole('button', { name: 'Check libraries', exact: true }).click();
  await expect(create).not.toBeVisible();
  await expect.poll(() => api.reads(librariesPath).length).toBeGreaterThan(readsBeforeCheck);
  await expect(page.getByRole('button', { name: 'Add library', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Use First movie library', exact: true }).click();
  await evidence(page, testInfo, 'setup-unconfirmed-library-reviewed-en-light');
  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Remote access', exact: true })).toBeVisible();
  expect(writes(api, 'POST', librariesPath)).toHaveLength(1);
});

test('a submitted library creation followed by HTTP 503 must be read back without another creation', async ({ page, api }) => {
  const state = installSetup(api);
  api.handlers.set(`POST ${librariesPath}`, (route) => {
    state.libraries = [fixtureLibrary()];
    return json(route, { Error: { Code: 'service_unavailable', Message: 'The proxy could not confirm the library creation result.' } }, 503);
  });
  await openLibraryStep(page);
  await page.getByRole('button', { name: 'Add library', exact: true }).click();
  const create = page.getByRole('dialog', { name: 'Create library', exact: true });
  await create.getByRole('textbox', { name: 'Library name', exact: true }).fill('First movie library');
  await create.getByRole('textbox', { name: 'Media directories', exact: true }).fill('/media/movies');
  await create.getByRole('button', { name: 'Create library', exact: true }).click();
  await expect(create).toContainText('This library may already have been created. Check the library list before trying again.');
  await expect(create.getByRole('button', { name: 'Create library', exact: true })).toHaveCount(0);
  expect(writes(api, 'POST', librariesPath)).toHaveLength(1);
  const readsBeforeCheck = api.reads(librariesPath).length;
  await create.getByRole('button', { name: 'Check libraries', exact: true }).click();
  await expect(create).not.toBeVisible();
  await expect.poll(() => api.reads(librariesPath).length).toBeGreaterThan(readsBeforeCheck);
  await expect(page.getByRole('button', { name: 'Add library', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Use First movie library', exact: true }).click();
  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Remote access', exact: true })).toBeVisible();
  expect(writes(api, 'POST', librariesPath)).toHaveLength(1);
});

test('an unconfirmed bootstrap blocks resubmission and checks server state without replaying setup', async ({ page, api }, testInfo) => {
  const state = installSetup(api);
  api.handlers.set(`POST ${bootstrapPath}`, async (route) => {
    state.initialized = true;
    await route.abort('connectionreset');
  });
  await submitAdministrator(page);
  await expect(page.getByText('Administrator creation could not be confirmed. Check the server status or sign in with the account you entered. Do not submit setup again.', { exact: true })).toBeVisible();
  await expect(page.locator('#setup-token')).toHaveValue('');
  await expect(page.locator('#account-password')).toHaveValue('');
  await expect(page.getByRole('button', { name: 'Create administrator', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Sign in to continue', exact: true })).toBeVisible();
  await expectNoStoredCredentials(page);
  await evidence(page, testInfo, 'setup-bootstrap-result-unconfirmed-en-light');
  const readsBeforeCheck = api.reads(bootstrapPath).length;
  await page.getByRole('button', { name: 'Already set up? Check server', exact: true }).click();
  await expect(page.getByText('The server reports that initial setup is complete. Sign in to continue.', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Create administrator', exact: true })).toBeDisabled();
  expect(api.reads(bootstrapPath).length).toBeGreaterThan(readsBeforeCheck);
  expect(writes(api, 'POST', bootstrapPath)).toHaveLength(1);
  expect(writes(api, 'POST', sessionPath)).toEqual([]);
  expect(api.reads(librariesPath)).toEqual([]);
  await page.getByRole('button', { name: 'Sign in to continue', exact: true }).click();
  await expectClearedLogin(page);
});

test('an unconfirmed bootstrap can open a cleared login without another setup attempt', async ({ page, api }) => {
  installSetup(api);
  api.handlers.set(`POST ${bootstrapPath}`, (route) => route.abort('connectionreset'));
  await submitAdministrator(page);
  await page.getByRole('button', { name: 'Sign in to continue', exact: true }).click();
  await expectClearedLogin(page);
  expect(writes(api, 'POST', bootstrapPath)).toHaveLength(1);
  expect(writes(api, 'POST', sessionPath)).toEqual([]);
});

test('an automatic sign-in rejection returns to a cleared login without replaying acknowledged bootstrap', async ({ page, api }) => {
  installSetup(api);
  api.handlers.set(`POST ${sessionPath}`, (route) => json(route, { Error: { Code: 'invalid_credentials', Message: 'The username or password is incorrect.' } }, 401));
  await submitAdministrator(page);
  await expectClearedLogin(page);
  await expect(page.getByText('Your administrator account was created. Sign in to manage your libraries and finish configuration.', { exact: true })).toBeVisible();
  expect(writes(api, 'POST', bootstrapPath)).toHaveLength(1);
  expect(writes(api, 'POST', sessionPath)).toHaveLength(1);
  expect(api.reads(librariesPath)).toEqual([]);
  await page.reload();
  await expectClearedLogin(page);
  expect(writes(api, 'POST', bootstrapPath)).toHaveLength(1);
  expect(writes(api, 'POST', sessionPath)).toHaveLength(1);
});

test('an unconfirmed automatic sign-in recovers its matching session with one read and no credential replay', async ({ page, api }) => {
  const state = installSetup(api);
  api.handlers.set(`POST ${sessionPath}`, async (route) => {
    state.authenticated = true;
    await route.abort('connectionreset');
  });
  await openLibraryStep(page);
  expect(api.reads(sessionPath)).toHaveLength(1);
  expect(writes(api, 'POST', bootstrapPath)).toHaveLength(1);
  expect(writes(api, 'POST', sessionPath)).toHaveLength(1);
  await expectNoStoredCredentials(page);
});

test('an uncertain automatic sign-in does not enter setup under another administrator session', async ({ page, api }) => {
  installSetup(api);
  api.handlers.set(`POST ${sessionPath}`, (route) => route.abort('connectionreset'));
  api.handlers.set(`GET ${sessionPath}`, (route) => json(route, { User: { ...administrator, Id: '2'.repeat(32), Name: 'Another administrator' }, CSRFToken: csrf }));
  await submitAdministrator(page);
  await expectClearedLogin(page);
  expect(api.reads(sessionPath)).toHaveLength(1);
  expect(api.reads(librariesPath)).toEqual([]);
  expect(api.reads(rootsPath)).toEqual([]);
  expect(writes(api, 'POST', bootstrapPath)).toHaveLength(1);
  expect(writes(api, 'POST', sessionPath)).toHaveLength(1);
});

test('reloading an initialized server keeps the acknowledged administrator session usable', async ({ page, api }) => {
  installSetup(api);
  await openLibraryStep(page);
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Administration', exact: true })).toBeVisible();
  expect(writes(api, 'POST', bootstrapPath)).toHaveLength(1);
  expect(writes(api, 'POST', sessionPath)).toHaveLength(1);
  await expectNoStoredCredentials(page);
});

for (const language of ['en', 'zh'] as const) for (const theme of ['light', 'dark'] as const) {
  test.describe(`${language} ${theme} mobile first-run setup`, () => {
    test.use({ viewport: { width: 390, height: 844 }, preferences: { language, theme } });
    test('the library, network, and finish steps fit the phone and retain the selected preferences', async ({ page, api }, testInfo) => {
      installSetup(api);
      await submitAdministrator(page);
      await expect(page.getByRole('button', { name: language === 'zh' ? '暂时跳过' : 'Skip for now', exact: true })).toBeVisible();
      await expect(page.locator('.setup-add-library')).toBeEnabled();
      await expect(page.locator('html')).toHaveAttribute('lang', language === 'zh' ? 'zh-CN' : 'en');
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
      await expectWizardFits(page);
      await evidence(page, testInfo, `setup-library-mobile-${language}-${theme}`);
      await page.getByRole('button', { name: language === 'zh' ? '暂时跳过' : 'Skip for now', exact: true }).click();
      await expect(page.getByRole('combobox', { name: language === 'zh' ? '默认界面语言' : 'Default interface language', exact: true })).toBeEnabled();
      await expectWizardFits(page);
      await evidence(page, testInfo, `setup-network-mobile-${language}-${theme}`);
      await page.getByRole('button', { name: language === 'zh' ? '继续' : 'Continue', exact: true }).click();
      await expect(page.getByRole('button', { name: language === 'zh' ? '完成设置' : 'Finish setup', exact: true })).toBeEnabled();
      await expectWizardFits(page);
      await evidence(page, testInfo, `setup-finish-mobile-${language}-${theme}`);
      await expectNoStoredCredentials(page);
      expect(writes(api, 'POST', librariesPath)).toEqual([]);
      expect(writes(api, 'PUT', policyPath)).toEqual([]);
    });
  });
}
