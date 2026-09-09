import { randomBytes } from 'node:crypto';
import { expect, test } from '@playwright/test';
import type { APIRequestContext, Dialog, Locator, Page, Route } from '@playwright/test';

interface FixtureUser {
  Id: string;
  Name: string;
  IsAdministrator: boolean;
  IsDisabled: boolean;
  HasPassword: boolean;
}

interface ManagedUser extends FixtureUser {
  Revision: string;
  Policy: {
    EnableAllFolders: boolean;
    EnabledFolders: string[];
    EnableMediaPlayback: boolean;
    EnablePlaybackRemuxing: boolean;
    EnableAudioPlaybackTranscoding: boolean;
    EnableVideoPlaybackTranscoding: boolean;
  };
}

interface UserMutation {
  User: ManagedUser;
  CurrentSessionRevoked: boolean;
}

interface FixtureLibrary {
  Id: string;
  Name: string;
}

const manageDialog = (page: Page): Locator => page.getByRole('dialog', { name: 'Manage user', exact: true });
const userPath = (id: string): string => `/admin/v1/users/${encodeURIComponent(id)}`;

function userResponse(page: Page, method: string, id: string, suffix = '') {
  return page.waitForResponse((response) => response.request().method() === method
    && new URL(response.url()).pathname === `${userPath(id)}${suffix}`);
}

function expectNextRevision(previous: ManagedUser, next: ManagedUser): void {
  expect(next.Id).toBe(previous.Id);
  expect(next.Revision).toMatch(/^[1-9][0-9]*$/);
  expect(next.Revision).toBe((BigInt(previous.Revision) + 1n).toString());
}

async function signIn(page: Page, name: string, password: string): Promise<void> {
  await page.goto('/admin/users');
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
  await page.getByLabel(/^Username/).fill(name);
  await page.getByLabel(/^Password/).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Users', exact: true })).toBeVisible();
}

async function navigateToUsersThroughOverview(page: Page): Promise<void> {
  await page.getByRole('link', { name: 'Overview', exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/$/);
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Users', exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/users$/);
  await expect(page.getByRole('heading', { name: 'Users', exact: true })).toBeVisible();
}

async function createUser(page: Page, name: string, password: string, administrator: boolean): Promise<FixtureUser> {
  await page.getByRole('button', { name: 'Create user', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Create user', exact: true });
  await dialog.getByLabel(/^Username/).fill(name);
  await dialog.getByLabel(/^Password/).fill(password);
  await dialog.getByRole('switch', { name: 'Administrator access', exact: true }).setChecked(administrator);
  const response = page.waitForResponse((result) => result.request().method() === 'POST'
    && new URL(result.url()).pathname === '/admin/v1/users');
  await dialog.getByRole('button', { name: 'Create user', exact: true }).click();
  const created = await response;
  expect(created.status()).toBe(201);
  const { User: user } = await created.json() as { User: FixtureUser };
  expect(user.Id).toBeTruthy();
  expect(user.Name).toBe(name);
  expect(user.IsAdministrator).toBe(administrator);
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole('button', { name: `Manage ${name}`, exact: true })).toBeVisible();
  return user;
}

async function createLibrary(page: Page, name: string, mediaPath: string): Promise<FixtureLibrary> {
  await page.getByRole('button', { name: 'Create library', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Create library', exact: true });
  await dialog.getByLabel(/^Library name/).fill(name);
  await dialog.getByLabel(/^Media directories/).fill(mediaPath);
  await dialog.getByRole('checkbox', { name: /^Scan after creating/ }).uncheck();
  const response = page.waitForResponse((result) => result.request().method() === 'POST'
    && new URL(result.url()).pathname === '/admin/v1/libraries');
  await dialog.getByRole('button', { name: 'Create library', exact: true }).click();
  const created = await response;
  expect(created.status()).toBe(201);
  const result = await created.json() as { Library: FixtureLibrary; Job?: unknown };
  expect(result.Library.Name).toBe(name);
  expect(result.Job).toBeUndefined();
  await expect(dialog).not.toBeVisible();
  return result.Library;
}

async function readUser(request: APIRequestContext, id: string): Promise<ManagedUser> {
  const response = await request.get(userPath(id));
  expect(response.status()).toBe(200);
  const { User: user } = await response.json() as { User: ManagedUser };
  expect(user.Id).toBe(id);
  return user;
}

async function openUser(page: Page, user: FixtureUser): Promise<ManagedUser> {
  const response = userResponse(page, 'GET', user.Id);
  await page.getByRole('button', { name: `Manage ${user.Name}`, exact: true }).click();
  const loaded = await response;
  expect(loaded.status()).toBe(200);
  const { User: current } = await loaded.json() as { User: ManagedUser };
  expect(current.Id).toBe(user.Id);
  expect(current.Revision).toMatch(/^[1-9][0-9]*$/);
  await expect(manageDialog(page)).toBeVisible();
  await expect(manageDialog(page).getByRole('textbox', { name: /^Username/ })).toHaveValue(current.Name);
  return current;
}

async function saveUser(page: Page, previous: ManagedUser): Promise<ManagedUser> {
  const [saved] = await Promise.all([
    userResponse(page, 'PUT', previous.Id),
    manageDialog(page).getByRole('button', { name: 'Save changes', exact: true }).click(),
  ]);
  expect(saved.status()).toBe(200);
  const result = await saved.json() as UserMutation;
  expectNextRevision(previous, result.User);
  expect(result.CurrentSessionRevoked).toBe(false);
  await expect(manageDialog(page)).toBeVisible();
  await expect(manageDialog(page).getByRole('textbox', { name: /^Username/ })).toHaveValue(result.User.Name);
  await expect(manageDialog(page).getByRole('alert').filter({ hasText: `User ${result.User.Name} updated.` })).toBeVisible();
  return result.User;
}

async function saveWhileBlockingNavigation(page: Page, previous: ManagedUser): Promise<ManagedUser> {
  const mutationURL = new URL(userPath(previous.Id), page.url()).href;
  let releaseSave: () => void = () => undefined;
  const released = new Promise<void>((resolve) => { releaseSave = resolve; });
  let mutations = 0;
  const holdSave = async (route: Route) => {
    if (route.request().method() !== 'PUT') {
      await route.continue();
      return;
    }
    mutations += 1;
    await released;
    await route.continue();
  };
  const prompts: string[] = [];
  const dismissUnexpectedPrompt = (prompt: Dialog) => {
    prompts.push(prompt.message());
    void prompt.dismiss().catch(() => undefined);
  };
  await page.route(mutationURL, holdSave);
  // A failed assertion must never leave an intercepted mutation waiting forever.
  const releaseTimeout = setTimeout(releaseSave, 30_000);
  let saving: Promise<ManagedUser> | undefined;
  page.on('dialog', dismissUnexpectedPrompt);
  try {
    saving = saveUser(page, previous);
    // Keep an early save rejection handled until the navigation assertions finish.
    void saving.catch(() => undefined);
    await expect.poll(() => mutations, { timeout: 5_000, message: 'The temporary member update must be intercepted.' }).toBe(1);
    const savingButton = manageDialog(page).getByRole('button', { name: 'Saving changes...', exact: true });
    await expect(savingButton).toBeVisible();
    await page.goBack({ waitUntil: 'commit', timeout: 10_000 });
    await expect(page).toHaveURL(/\/admin\/users$/);
    await expect(manageDialog(page)).toBeVisible();
    await expect(savingButton).toBeDisabled();
    await expect(manageDialog(page).getByRole('textbox', { name: /^Username/ })).toBeDisabled();
    expect(prompts).toEqual([]);
    releaseSave();
    const result = await saving;
    expect(mutations).toBe(1);
    return result;
  } finally {
    clearTimeout(releaseTimeout);
    releaseSave();
    page.off('dialog', dismissUnexpectedPrompt);
    await page.unroute(mutationURL, holdSave);
    if (saving) await Promise.allSettled([saving]);
  }
}

async function resetPassword(page: Page, previous: ManagedUser, password: string, self = false): Promise<ManagedUser> {
  await manageDialog(page).getByRole('button', { name: 'Reset password', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Reset password', exact: true });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel(/^New password/).fill(password);
  await dialog.getByLabel(/^Confirm new password/).fill(password);
  const response = userResponse(page, 'POST', previous.Id, '/password');
  await dialog.getByRole('button', { name: 'Reset password', exact: true }).click();
  const reset = await response;
  expect(reset.status()).toBe(200);
  const result = await reset.json() as UserMutation;
  expectNextRevision(previous, result.User);
  expect(result.CurrentSessionRevoked).toBe(self);
  await expect(dialog).not.toBeVisible();
  if (!self) {
    await expect(manageDialog(page)).toBeVisible();
    await expect(manageDialog(page).getByRole('alert').filter({ hasText: /password.*reset/i })).toBeVisible();
  }
  return result.User;
}

async function authenticateMember(request: APIRequestContext, name: string, password: string, deviceId: string) {
  return request.post('/emby/Users/AuthenticateByName', {
    headers: { Authorization: `Emby Client="Goby user management E2E", DeviceId="${deviceId}", Device="Browser", Version="1.0"` },
    data: { Username: name, Pw: password },
  });
}

async function memberToken(request: APIRequestContext, name: string, password: string, deviceId: string): Promise<string> {
  const response = await authenticateMember(request, name, password, deviceId);
  expect(response.status()).toBe(200);
  const result = await response.json() as { AccessToken: string };
  expect(result.AccessToken).toBeTruthy();
  return result.AccessToken;
}

async function memberViews(request: APIRequestContext, userId: string, token: string) {
  return request.get(`/emby/Users/${encodeURIComponent(userId)}/Views`, { headers: { 'X-Emby-Token': token } });
}

// Run this file only on test-env against a newly started server with a disposable
// database and an existing test administrator:
// GOBY_SMOKE_USERS_DISPOSABLE_DATABASE=1 confirms the whole database is disposable.
// GOBY_SMOKE_USERS_DEDICATED_ADMIN=1 confirms GOBY_SMOKE_NAME/PASSWORD identify a test account.
// Also set GOBY_SMOKE_BASE_URL and GOBY_SMOKE_MEDIA_PATH to the dedicated server and
// an allowed media fixture directory. Both libraries use that directory with Scan=false.
// Run this file separately from other login-heavy specs to respect the login limiter.
// There is no user deletion API. Dispose of the entire test database after this run,
// including on failure. This journey never deletes data or edits pre-existing users.
test('isolated user management, library access, conflict recovery, and temporary administrator reset', async ({ page, context }, testInfo) => {
  test.skip(process.env.GOBY_SMOKE_USERS_DISPOSABLE_DATABASE !== '1'
    || process.env.GOBY_SMOKE_USERS_DEDICATED_ADMIN !== '1',
  'Explicit disposable-database and dedicated-administrator confirmations are required.');
  test.setTimeout(180_000);

  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  const baseURL = process.env.GOBY_SMOKE_BASE_URL;
  const mediaPath = process.env.GOBY_SMOKE_MEDIA_PATH;
  if (!name || !password || !baseURL || !mediaPath) {
    throw new Error('GOBY_SMOKE_BASE_URL, GOBY_SMOKE_NAME, GOBY_SMOKE_PASSWORD, and GOBY_SMOKE_MEDIA_PATH are required.');
  }
  expect(new URL(testInfo.project.use.baseURL ?? '').origin).toBe(new URL(baseURL).origin);

  const suffix = `${Date.now()}-${randomBytes(4).toString('hex')}`;
  const memberPassword = randomBytes(24).toString('base64url');
  const adminPassword = randomBytes(24).toString('base64url');
  const nextMemberPassword = randomBytes(24).toString('base64url');
  const nextAdminPassword = randomBytes(24).toString('base64url');
  const pageErrors: string[] = [];
  const recordErrors = (observed: Page) => observed.on('pageerror', (error) => pageErrors.push(error.message));
  recordErrors(page);
  context.on('page', recordErrors);

  await signIn(page, name, password);
  const sessionResponse = await context.request.get('/admin/v1/session');
  expect(sessionResponse.status()).toBe(200);
  const session = await sessionResponse.json() as { User: FixtureUser };
  expect(session.User.Name).toBe(name);
  expect(session.User.IsAdministrator).toBe(true);
  const originalAdministrator = await readUser(context.request, session.User.Id);

  const member = await createUser(page, `managed-member-${suffix}`, memberPassword, false);
  const administrator = await createUser(page, `managed-admin-${suffix}`, adminPassword, true);
  expect(new Set([member.Id, administrator.Id, originalAdministrator.Id]).size).toBe(3);
  await page.goto('/admin/libraries');
  const allowedLibrary = await createLibrary(page, `Allowed user fixture ${suffix}`, mediaPath);
  const deniedLibrary = await createLibrary(page, `Denied user fixture ${suffix}`, mediaPath);
  expect(allowedLibrary.Id).not.toBe(deniedLibrary.Id);
  // Use same-document history so Back exercises the application's popstate guard.
  await navigateToUsersThroughOverview(page);

  let current = await openUser(page, member);
  const dialog = manageDialog(page);
  await test.step('save a name and a restricted library and playback policy', async () => {
    await expect(dialog.getByRole('switch', { name: 'Administrator access', exact: true })).not.toBeChecked();
    await expect(dialog.getByRole('switch', { name: 'Disable account', exact: true })).not.toBeChecked();
    await dialog.getByRole('textbox', { name: /^Username/ }).fill(`managed-renamed-${suffix}`);
    await test.step('keep the unsaved user draft when browser Back is rejected', async () => {
      const confirmation = page.waitForEvent('dialog', { timeout: 10_000 }).then(async (prompt) => {
        const details = { type: prompt.type(), message: prompt.message() };
        await prompt.dismiss();
        return details;
      });
      const [, dismissed] = await Promise.all([
        page.goBack({ waitUntil: 'commit', timeout: 10_000 }),
        confirmation,
      ]);
      expect(dismissed).toEqual({ type: 'confirm', message: 'Discard unsaved user changes and leave this page?' });
      await expect(page).toHaveURL(/\/admin\/users$/);
      await expect(dialog).toBeVisible();
      await expect(dialog.getByRole('textbox', { name: /^Username/ })).toHaveValue(`managed-renamed-${suffix}`);
    });
    await dialog.getByRole('switch', { name: 'All libraries', exact: true }).uncheck();
    await dialog.getByRole('checkbox', { name: allowedLibrary.Name, exact: true }).check();
    await dialog.getByRole('checkbox', { name: deniedLibrary.Name, exact: true }).uncheck();
    for (const setting of ['Remuxing', 'Audio transcoding', 'Video transcoding', 'Media playback']) {
      await dialog.getByRole('switch', { name: setting, exact: true }).uncheck();
    }
    current = await test.step('keep browser Back on Users while a save is pending', () => saveWhileBlockingNavigation(page, current));
    expect(current.Name).toBe(`managed-renamed-${suffix}`);
    expect(current.Policy).toEqual({
      EnableAllFolders: false,
      EnabledFolders: [allowedLibrary.Id],
      EnableMediaPlayback: false,
      EnablePlaybackRemuxing: false,
      EnableAudioPlaybackTranscoding: false,
      EnableVideoPlaybackTranscoding: false,
    });
    await dialog.getByRole('switch', { name: 'Media playback', exact: true }).check();
    current = await saveUser(page, current);
    expect(current.Policy.EnableMediaPlayback).toBe(true);
    await expect(dialog.getByRole('checkbox', { name: allowedLibrary.Name, exact: true })).toBeChecked();
    const token = await memberToken(context.request, current.Name, memberPassword, `acl-${suffix}`);
    const views = await memberViews(context.request, member.Id, token);
    expect(views.status()).toBe(200);
    const visible = await views.json() as { Items: FixtureLibrary[] };
    expect(visible.Items.map((library) => library.Id)).toEqual([allowedLibrary.Id]);

    await dialog.getByRole('switch', { name: 'Disable account', exact: true }).check();
    current = await saveUser(page, current);
    expect(current.IsDisabled).toBe(true);
    expect((await memberViews(context.request, member.Id, token)).status()).toBe(401);
    expect((await authenticateMember(context.request, current.Name, memberPassword, `disabled-${suffix}`)).status()).toBe(401);
    await dialog.getByRole('switch', { name: 'Disable account', exact: true }).uncheck();
    current = await saveUser(page, current);
    expect(current.IsDisabled).toBe(false);
    expect(current.Policy.EnabledFolders).toEqual([allowedLibrary.Id]);
  });

  await test.step('reset only the temporary member and invalidate its old sessions', async () => {
    const token = await memberToken(context.request, current.Name, memberPassword, `before-reset-${suffix}`);
    current = await resetPassword(page, current, nextMemberPassword);
    expect((await memberViews(context.request, member.Id, token)).status()).toBe(401);
    expect((await authenticateMember(context.request, current.Name, memberPassword, `old-password-${suffix}`)).status()).toBe(401);
    const replacement = await memberToken(context.request, current.Name, nextMemberPassword, `after-reset-${suffix}`);
    const views = await memberViews(context.request, member.Id, replacement);
    expect(views.status()).toBe(200);
    const visible = await views.json() as { Items: FixtureLibrary[] };
    expect(visible.Items.map((library) => library.Id)).toEqual([allowedLibrary.Id]);
    await page.keyboard.press('Escape');
    await expect(dialog).not.toBeVisible();
  });

  await test.step('navigate normally after closing the saved user dialog', async () => {
    await navigateToUsersThroughOverview(page);
    await expect(page.getByRole('button', { name: `Manage ${current.Name}`, exact: true })).toBeVisible();
  });

  const otherTab = await context.newPage();
  await otherTab.goto('/admin/users');
  await test.step('preserve a stale draft and explicitly reload the winning revision', async () => {
    current = await openUser(page, current);
    const concurrent = await openUser(otherTab, current);
    expect(concurrent.Revision).toBe(current.Revision);
    const draftName = `managed-draft-${suffix}`;
    const winningName = `managed-concurrent-${suffix}`;
    await dialog.getByRole('textbox', { name: /^Username/ }).fill(draftName);
    await manageDialog(otherTab).getByRole('textbox', { name: /^Username/ }).fill(winningName);
    const winner = await saveUser(otherTab, concurrent);
    const response = userResponse(page, 'PUT', member.Id);
    await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();
    const rejected = await response;
    expect(rejected.status()).toBe(409);
    expect((await rejected.json() as { Error: { Code: string } }).Error.Code).toBe('revision_conflict');
    await expect(dialog.getByRole('alert').filter({ hasText: /changed|conflict/i })).toBeVisible();
    await expect(dialog.getByRole('textbox', { name: /^Username/ })).toHaveValue(draftName);
    expect((await readUser(context.request, member.Id)).Name).toBe(winningName);

    await dialog.getByRole('button', { name: 'Reload latest user', exact: true }).click();
    const discard = page.getByRole('button', { name: 'Discard draft and reload', exact: true });
    await expect(page.getByRole('dialog').filter({ has: discard })).toBeVisible();
    const reloaded = userResponse(page, 'GET', member.Id);
    await discard.click();
    const latestResponse = await reloaded;
    expect(latestResponse.status()).toBe(200);
    current = (await latestResponse.json() as { User: ManagedUser }).User;
    expect(current.Revision).toBe(winner.Revision);
    await expect(dialog.getByRole('textbox', { name: /^Username/ })).toHaveValue(winningName);
    await dialog.getByRole('textbox', { name: /^Username/ }).fill(`managed-final-${suffix}`);
    current = await saveUser(page, current);
    await page.screenshot({ path: testInfo.outputPath('users-management-desktop.png'), fullPage: true, animations: 'disabled' });
    await otherTab.keyboard.press('Escape');
    await expect(manageDialog(otherTab)).not.toBeVisible();
  });

  await test.step('edit the temporary member on a mobile viewport', async () => {
    await page.setViewportSize({ width: 390, height: 844 });
    for (const setting of ['Remuxing', 'Audio transcoding', 'Video transcoding']) {
      await dialog.getByRole('switch', { name: setting, exact: true }).check();
    }
    current = await saveUser(page, current);
    expect(current.Policy.EnablePlaybackRemuxing).toBe(true);
    expect(current.Policy.EnableAudioPlaybackTranscoding).toBe(true);
    expect(current.Policy.EnableVideoPlaybackTranscoding).toBe(true);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    const bounds = await dialog.boundingBox();
    expect(bounds).not.toBeNull();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(391);
    await page.screenshot({ path: testInfo.outputPath('users-management-mobile.png'), fullPage: true, animations: 'disabled' });
    await page.keyboard.press('Escape');
    await expect(dialog).not.toBeVisible();
    await expect(page.getByRole('list', { name: 'Server users', exact: true }).getByText(current.Name, { exact: true })).toBeVisible();
    current = await openUser(page, current);
  });

  await test.step('reject a stale management form after another tab changes administrators', async () => {
    await dialog.getByRole('textbox', { name: /^Username/ }).fill(`managed-rejected-${suffix}`);
    await otherTab.getByRole('button', { name: 'Sign out', exact: true }).click();
    await signIn(otherTab, administrator.Name, adminPassword);
    let mutations = 0;
    page.on('request', (request) => {
      if (request.method() === 'PUT' && new URL(request.url()).pathname === userPath(member.Id)) mutations += 1;
    });
    const response = userResponse(page, 'PUT', member.Id);
    await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();
    expect((await response).status()).toBe(403);
    await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    await expect(page.getByText('Your session has changed or expired. Sign in again to continue.', { exact: true })).toBeVisible();
    expect(mutations).toBe(1);
    const persisted = await readUser(context.request, member.Id);
    expect(persisted.Name).toBe(current.Name);
    expect(persisted.Revision).toBe(current.Revision);
  });

  await test.step('reset the temporary administrator itself and require a new login', async () => {
    let self = await openUser(otherTab, administrator);
    await manageDialog(otherTab).getByRole('textbox', { name: /^Username/ }).fill(`managed-self-${suffix}`);
    self = await saveUser(otherTab, self);
    await expect(otherTab.getByRole('banner', { includeHidden: true }).getByText(self.Name, { exact: true })).toHaveText(self.Name);
    await resetPassword(otherTab, self, nextAdminPassword, true);
    await expect(otherTab.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    expect((await context.cookies()).some((cookie) => cookie.name === 'goby_session')).toBe(false);
    const rejected = await context.request.post('/admin/v1/session', { data: { Name: self.Name, Password: adminPassword } });
    expect(rejected.status()).toBe(401);
    await signIn(otherTab, self.Name, nextAdminPassword);
    const activeResponse = await context.request.get('/admin/v1/session');
    expect(activeResponse.status()).toBe(200);
    const active = await activeResponse.json() as { User: FixtureUser };
    expect(active.User.Id).toBe(administrator.Id);
    expect(await readUser(context.request, originalAdministrator.Id)).toEqual(originalAdministrator);
    await otherTab.close();
  });

  expect(pageErrors).toEqual([]);
});
