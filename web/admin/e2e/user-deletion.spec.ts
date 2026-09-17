import { randomBytes } from 'node:crypto';
import { expect, test } from '@playwright/test';
import type { APIRequestContext, BrowserContext, Dialog, Locator, Page, Request, Route } from '@playwright/test';

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

interface UserRequest {
  Method: string;
  Pathname: string;
  Body: string | null;
  HasCSRF: boolean;
  Origin: string | undefined;
}

const manageDialog = (page: Page): Locator => page.getByRole('dialog', { name: 'Manage user', exact: true });
const deleteDialog = (page: Page): Locator => page.getByRole('dialog', { name: 'Delete user', exact: true });
const userPath = (id: string): string => `/admin/v1/users/${encodeURIComponent(id)}`;

function userResponse(page: Page, method: string, id: string) {
  return page.waitForResponse((response) => response.request().method() === method
    && new URL(response.url()).pathname === userPath(id));
}

function targetRequests(requests: UserRequest[], id: string): UserRequest[] {
  return requests.filter((request) => request.Pathname === userPath(id));
}

function assertOwned(owned: Set<string>, id: string): void {
  if (!owned.has(id)) throw new Error('Only users created by this disposable run may be mutated.');
}

async function protectExistingUsers(context: BrowserContext, owned: Set<string>, requests: UserRequest[]): Promise<void> {
  context.on('request', (request: Request) => {
    const pathname = new URL(request.url()).pathname;
    if (!/^\/admin\/v1\/users\/[^/]+(?:\/password)?$/.test(pathname)
      && !(pathname === '/admin/v1/session' && request.method() === 'DELETE')) return;
    requests.push({
      Method: request.method(),
      Pathname: pathname,
      Body: request.method() === 'DELETE' ? request.postData() : null,
      HasCSRF: Boolean(request.headers()['x-csrf-token']),
      Origin: request.headers().origin,
    });
  });
  await context.route('**/admin/v1/users/**', async (route) => {
    const request = route.request();
    const match = /^\/admin\/v1\/users\/([^/]+)(?:\/password)?$/.exec(new URL(request.url()).pathname);
    if (match && ['DELETE', 'PUT', 'POST'].includes(request.method()) && !owned.has(decodeURIComponent(match[1]))) {
      await route.abort('blockedbyclient');
      throw new Error('A mutation of a pre-existing user was blocked.');
    }
    await route.continue();
  });
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

async function createUser(page: Page, owned: Set<string>, name: string, password: string, administrator = false): Promise<FixtureUser> {
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
  expect(user.IsDisabled).toBe(false);
  expect(owned.has(user.Id)).toBe(false);
  owned.add(user.Id);
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole('button', { name: `Manage ${name}`, exact: true })).toBeVisible();
  return user;
}

async function readUser(request: APIRequestContext, id: string): Promise<ManagedUser> {
  const response = await request.get(userPath(id));
  expect(response.status()).toBe(200);
  const { User: user } = await response.json() as { User: ManagedUser };
  expect(user.Id).toBe(id);
  return user;
}

async function openUser(page: Page, owned: Set<string>, user: FixtureUser): Promise<ManagedUser> {
  assertOwned(owned, user.Id);
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

async function openDelete(page: Page): Promise<Locator> {
  await manageDialog(page).getByRole('button', { name: 'Delete user', exact: true }).click();
  const dialog = deleteDialog(page);
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText(/account/i);
  await expect(dialog).toContainText(/sign-ins/i);
  await expect(dialog).toContainText(/personal playback history/i);
  await expect(dialog).toContainText(/media files.*kept|keep.*media files/i);
  await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeFocused();
  return dialog;
}

function expectDeleteRequest(request: UserRequest, user: ManagedUser, origin: string): void {
  expect(request.Method).toBe('DELETE');
  expect(request.Pathname).toBe(userPath(user.Id));
  expect(JSON.parse(request.Body ?? 'null')).toEqual({ Revision: user.Revision });
  expect(request.HasCSRF).toBe(true);
  expect(request.Origin).toBe(origin);
}

async function confirmDelete(page: Page, user: ManagedUser, self = false): Promise<void> {
  const response = userResponse(page, 'DELETE', user.Id);
  await deleteDialog(page).getByRole('button', { name: 'Delete user', exact: true }).click();
  const deleted = await response;
  expect(deleted.status()).toBe(200);
  expect(await deleted.json()).toEqual({ CurrentSessionRevoked: self });
  await expect(deleteDialog(page)).not.toBeVisible();
  await expect(manageDialog(page)).not.toBeVisible();
  if (!self) {
    await expect(page.getByRole('button', { name: `Manage ${user.Name}`, exact: true })).toHaveCount(0);
    await expect(page.getByRole('alert').filter({ hasText: `User ${user.Name} deleted.` })).toBeVisible();
  }
}

async function expectFrozenManagement(page: Page): Promise<void> {
  await expect(manageDialog(page)).toBeVisible();
  await expect(manageDialog(page).getByRole('button', { name: 'Delete user', exact: true })).toBeDisabled();
  await expect(manageDialog(page).getByRole('button', { name: 'Reset password', exact: true })).toBeDisabled();
  await expect(manageDialog(page).getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await expect(manageDialog(page).getByRole('button', { name: 'Reload latest user', exact: true })).toBeVisible();
}

async function reloadLatest(page: Page, user: ManagedUser): Promise<ManagedUser> {
  const response = userResponse(page, 'GET', user.Id);
  const activeDialog = await deleteDialog(page).isVisible() ? deleteDialog(page) : manageDialog(page);
  await activeDialog.getByRole('button', { name: 'Reload latest user', exact: true }).click();
  const loaded = await response;
  expect(loaded.status()).toBe(200);
  const { User: current } = await loaded.json() as { User: ManagedUser };
  expect(current.Id).toBe(user.Id);
  await expect(deleteDialog(page)).not.toBeVisible();
  await expect(manageDialog(page).getByRole('textbox', { name: /^Username/ })).toHaveValue(current.Name);
  await expect(manageDialog(page).getByRole('button', { name: 'Delete user', exact: true })).toBeEnabled();
  await expect(manageDialog(page).getByRole('button', { name: 'Reset password', exact: true })).toBeEnabled();
  return current;
}

async function memberToken(request: APIRequestContext, user: FixtureUser, password: string, deviceId: string): Promise<string> {
  const response = await request.post('/emby/Users/AuthenticateByName', {
    headers: { Authorization: `Emby Client="Goby user deletion E2E", DeviceId="${deviceId}", Device="Browser", Version="1.0"` },
    data: { Username: user.Name, Pw: password },
  });
  expect(response.status()).toBe(200);
  const result = await response.json() as { AccessToken: string };
  expect(Boolean(result.AccessToken)).toBe(true);
  return result.AccessToken;
}

async function loseDeleteResponse(page: Page, owned: Set<string>, user: ManagedUser, self: boolean) {
  assertOwned(owned, user.Id);
  // This interception is private to a newly created user in the admitted disposable fixture.
  // Never replay an intercepted DELETE, including if the browser retries after the abort.
  const url = new URL(userPath(user.Id), page.url()).href;
  let attempts = 0;
  let committed = false;
  let failure: unknown;
  const dropResponse = async (route: Route) => {
    if (route.request().method() !== 'DELETE') {
      await route.continue();
      return;
    }
    attempts += 1;
    try {
      assertOwned(owned, user.Id);
      if (attempts !== 1) return;
      expect(route.request().postDataJSON()).toEqual({ Revision: user.Revision });
      const response = await route.fetch({ maxRetries: 0, maxRedirects: 0, timeout: 15_000 });
      expect(response.status()).toBe(200);
      expect(await response.json()).toEqual({ CurrentSessionRevoked: self });
      committed = true;
    } catch (error) {
      failure = error;
    } finally {
      await route.abort('failed');
    }
  };
  await page.route(url, dropResponse);
  return {
    assertCommitted() {
      if (failure) throw failure;
      expect(committed).toBe(true);
      expect(attempts).toBe(1);
    },
    dispose: () => page.unroute(url, dropResponse),
  };
}

// Run only on test-env against a dedicated server with a disposable database.
// GOBY_SMOKE_USERS_DISPOSABLE_DATABASE=1 and GOBY_SMOKE_USERS_DEDICATED_ADMIN=1
// explicitly admit this destructive journey. GOBY_SMOKE_NAME/PASSWORD must identify
// the dedicated administrator, and GOBY_SMOKE_BASE_URL must match the browser origin.
// Only accounts created below may be changed or deleted. Dispose of the entire
// database after this run, including on failure; do not add destructive cleanup retries.
// Run separately from other login-heavy specs to respect the login limiter.
test.describe.configure({ retries: 0 });

test('isolated user deletion, explicit recovery, and temporary administrator self-deletion', async ({ page, context, browser }, testInfo) => {
  test.skip(process.env.GOBY_SMOKE_USERS_DISPOSABLE_DATABASE !== '1'
    || process.env.GOBY_SMOKE_USERS_DEDICATED_ADMIN !== '1',
  'Explicit disposable-database and dedicated-administrator confirmations are required.');
  test.setTimeout(240_000);

  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  const baseURL = process.env.GOBY_SMOKE_BASE_URL;
  if (!name || !password || !baseURL) {
    throw new Error('GOBY_SMOKE_BASE_URL, GOBY_SMOKE_NAME, and GOBY_SMOKE_PASSWORD are required.');
  }
  const origin = new URL(baseURL).origin;
  expect(new URL(testInfo.project.use.baseURL ?? '').origin).toBe(origin);

  const suffix = `${Date.now()}-${randomBytes(4).toString('hex')}`;
  const memberPassword = randomBytes(24).toString('base64url');
  const adminPassword = randomBytes(24).toString('base64url');
  const unknownAdminPassword = randomBytes(24).toString('base64url');
  const owned = new Set<string>();
  const requests: UserRequest[] = [];
  const pageErrors: string[] = [];
  const recordErrors = (observed: Page) => observed.on('pageerror', (error) => pageErrors.push(error.message));
  recordErrors(page);
  context.on('page', recordErrors);
  await protectExistingUsers(context, owned, requests);
  await signIn(page, name, password);
  const sessionResponse = await context.request.get('/admin/v1/session');
  expect(sessionResponse.status()).toBe(200);
  const session = await sessionResponse.json() as { User: FixtureUser };
  expect(session.User.Name).toBe(name);
  expect(session.User.IsAdministrator).toBe(true);
  expect(session.User.IsDisabled).toBe(false);
  const originalAdministrator = await readUser(context.request, session.User.Id);

  const ordinary = await createUser(page, owned, `delete-member-${suffix}`, memberPassword);
  const conflicting = await createUser(page, owned, `delete-conflict-${suffix}`, memberPassword);
  const unknown = await createUser(page, owned, `delete-unknown-${suffix}`, memberPassword);
  const administrator = await createUser(page, owned, `delete-admin-${suffix}`, adminPassword, true);
  const unknownAdministrator = await createUser(page, owned, `delete-unknown-admin-${suffix}`, unknownAdminPassword, true);
  expect(owned.size).toBe(5);
  expect(owned.has(originalAdministrator.Id)).toBe(false);
  // Use same-document history so Back exercises the application's popstate guard.
  await navigateToUsersThroughOverview(page);

  await test.step('preserve a dirty draft and cancel deletion without a request', async () => {
    const user = await openUser(page, owned, ordinary);
    const dialog = manageDialog(page);
    const checkpoint = targetRequests(requests, user.Id).length;
    const draftName = `delete-draft-${suffix}`;
    await dialog.getByRole('textbox', { name: /^Username/ }).fill(draftName);
    await expect(dialog.getByRole('button', { name: 'Delete user', exact: true })).toBeDisabled();
    await expect(deleteDialog(page)).not.toBeVisible();
    await dialog.getByRole('button', { name: 'Close', exact: true }).click();
    const discardDialog = page.getByRole('dialog', { name: 'Discard unsaved changes?', exact: true });
    await expect(discardDialog).toBeVisible();
    await discardDialog.getByRole('button', { name: 'Keep editing', exact: true }).click();
    await expect(dialog.getByRole('textbox', { name: /^Username/ })).toHaveValue(draftName);
    expect(targetRequests(requests, user.Id).slice(checkpoint)).toEqual([]);
    await dialog.getByRole('textbox', { name: /^Username/ }).fill(user.Name);
    const confirmation = await openDelete(page);
    await confirmation.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(confirmation).not.toBeVisible();
    await expect(dialog.getByRole('textbox', { name: /^Username/ })).toHaveValue(user.Name);
    await expect(dialog.getByRole('button', { name: 'Delete user', exact: true })).toBeEnabled();
    expect(targetRequests(requests, user.Id).slice(checkpoint)).toEqual([]);
  });

  await test.step('delete an owned member once while busy and keep the administrator signed in', async () => {
    const user = await readUser(context.request, ordinary.Id);
    const token = await memberToken(context.request, user, memberPassword, `deleted-member-${suffix}`);
    const cookie = (await context.cookies()).find((candidate) => candidate.name === 'goby_session');
    expect(Boolean(cookie)).toBe(true);
    const confirmation = await openDelete(page);
    const checkpoint = targetRequests(requests, user.Id).length;
    const url = new URL(userPath(user.Id), page.url()).href;
    let attempts = 0;
    let releaseDelete: () => void = () => undefined;
    const released = new Promise<void>((resolve) => { releaseDelete = resolve; });
    const holdDelete = async (route: Route) => {
      if (route.request().method() !== 'DELETE') {
        await route.continue();
        return;
      }
      assertOwned(owned, user.Id);
      attempts += 1;
      // A regression may produce another browser request, but must not delete twice.
      if (attempts !== 1) {
        await route.abort('blockedbyclient');
        return;
      }
      await released;
      await route.continue();
    };
    const prompts: string[] = [];
    const dismissUnexpectedPrompt = (prompt: Dialog) => {
      prompts.push(prompt.message());
      void prompt.dismiss().catch(() => undefined);
    };
    await page.route(url, holdDelete);
    const releaseTimeout = setTimeout(releaseDelete, 30_000);
    page.on('dialog', dismissUnexpectedPrompt);
    const response = userResponse(page, 'DELETE', user.Id);
    void response.catch(() => undefined);
    try {
      await confirmation.getByRole('button', { name: 'Delete user', exact: true }).dblclick();
      await expect.poll(() => attempts, { timeout: 5_000, message: 'The owned user deletion must be intercepted.' }).toBe(1);
      const busy = confirmation.getByRole('button', { name: 'Deleting user...', exact: true });
      await expect(busy).toBeVisible();
      await expect(busy).toBeDisabled();
      await expect(confirmation.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled();
      await page.keyboard.press('Enter');
      await page.keyboard.press('Escape');
      await expect(confirmation).toBeVisible();
      await page.goBack({ waitUntil: 'commit', timeout: 10_000 });
      await expect(page).toHaveURL(/\/admin\/users$/);
      await expect(confirmation).toBeVisible();
      await expect(busy).toBeDisabled();
      expect(prompts).toEqual([]);
      expect(attempts).toBe(1);
      releaseDelete();
      const deleted = await response;
      expect(deleted.status()).toBe(200);
      expect(await deleted.json()).toEqual({ CurrentSessionRevoked: false });
      await expect(confirmation).not.toBeVisible();
      await expect(manageDialog(page)).not.toBeVisible();
      await expect(page.getByRole('button', { name: `Manage ${user.Name}`, exact: true })).toHaveCount(0);
      await expect(page.getByRole('alert').filter({ hasText: `User ${user.Name} deleted.` })).toBeVisible();
      const deletions = targetRequests(requests, user.Id).slice(checkpoint);
      expect(deletions).toHaveLength(1);
      expectDeleteRequest(deletions[0], user, origin);
      expect(attempts).toBe(1);
    } finally {
      clearTimeout(releaseTimeout);
      releaseDelete();
      page.off('dialog', dismissUnexpectedPrompt);
      await page.unroute(url, holdDelete);
    }
    expect((await context.request.get(userPath(user.Id))).status()).toBe(404);
    const memberViews = await context.request.get(`/emby/Users/${encodeURIComponent(user.Id)}/Views`, {
      headers: { 'X-Emby-Token': token },
    });
    expect(memberViews.status()).toBe(401);
    const currentCookie = (await context.cookies()).find((candidate) => candidate.name === 'goby_session');
    expect(Boolean(currentCookie) && currentCookie?.value === cookie?.value).toBe(true);
    const active = await context.request.get('/admin/v1/session');
    expect(active.status()).toBe(200);
    expect((await active.json() as { User: FixtureUser }).User.Id).toBe(originalAdministrator.Id);
    await navigateToUsersThroughOverview(page);
    await expect(page.getByRole('button', { name: `Manage ${user.Name}`, exact: true })).toHaveCount(0);
  });

  await test.step('recover a real revision conflict with an explicit GET before a new deletion decision', async () => {
    const stale = await openUser(page, owned, conflicting);
    const otherTab = await context.newPage();
    try {
      await otherTab.goto('/admin/users');
      const concurrent = await openUser(otherTab, owned, conflicting);
      expect(concurrent.Revision).toBe(stale.Revision);
      const winningName = `delete-renamed-${suffix}`;
      await manageDialog(otherTab).getByRole('textbox', { name: /^Username/ }).fill(winningName);
      const saved = userResponse(otherTab, 'PUT', concurrent.Id);
      await manageDialog(otherTab).getByRole('button', { name: 'Save changes', exact: true }).click();
      const result = await saved;
      expect(result.status()).toBe(200);
      const winner = (await result.json() as { User: ManagedUser }).User;
      expect(winner.Revision).toBe((BigInt(stale.Revision) + 1n).toString());
      await expect(manageDialog(otherTab).getByRole('textbox', { name: /^Username/ })).toHaveValue(winningName);
      await otherTab.close();

      const checkpoint = targetRequests(requests, stale.Id).length;
      const confirmation = await openDelete(page);
      const response = userResponse(page, 'DELETE', stale.Id);
      await confirmation.getByRole('button', { name: 'Delete user', exact: true }).click();
      const rejected = await response;
      expect(rejected.status()).toBe(409);
      expect((await rejected.json() as { Error: { Code: string } }).Error.Code).toBe('revision_conflict');
      await expect(confirmation.getByRole('alert').filter({ hasText: /changed|conflict/i })).toBeVisible();
      await expect(confirmation.getByRole('button', { name: 'Delete user', exact: true })).toBeDisabled();
      await confirmation.getByRole('button', { name: 'Cancel', exact: true }).click();
      await expectFrozenManagement(page);
      expect(targetRequests(requests, stale.Id).slice(checkpoint).map((request) => request.Method)).toEqual(['DELETE']);
      const current = await reloadLatest(page, stale);
      expect(current.Name).toBe(winningName);
      expect(current.Revision).toBe(winner.Revision);
      const recovered = targetRequests(requests, stale.Id).slice(checkpoint);
      expect(recovered.map((request) => request.Method)).toEqual(['DELETE', 'GET']);
      expectDeleteRequest(recovered[0], stale, origin);
      expect((await readUser(context.request, stale.Id)).Revision).toBe(winner.Revision);
      await openDelete(page);
      await confirmDelete(page, current);
      const decisions = targetRequests(requests, stale.Id).slice(checkpoint);
      expect(decisions.map((request) => request.Method)).toEqual(['DELETE', 'GET', 'DELETE']);
      expectDeleteRequest(decisions[2], current, origin);
      expect((await context.request.get(userPath(stale.Id))).status()).toBe(404);
    } finally {
      if (!otherTab.isClosed()) await otherTab.close();
    }
  });

  await test.step('recover a committed deletion with a lost response through GET only after cancelling the confirmation', async () => {
    const user = await openUser(page, owned, unknown);
    const checkpoint = targetRequests(requests, user.Id).length;
    const interception = await loseDeleteResponse(page, owned, user, false);
    try {
      const confirmation = await openDelete(page);
      await confirmation.getByRole('button', { name: 'Delete user', exact: true }).click();
      await expect(confirmation.getByRole('alert').filter({ hasText: /could not be confirmed|may already/i })).toBeVisible();
      interception.assertCommitted();
      await expect(confirmation.getByRole('button', { name: 'Delete user', exact: true })).toBeDisabled();
      await confirmation.getByRole('button', { name: 'Cancel', exact: true }).click();
      await expectFrozenManagement(page);
      expect(targetRequests(requests, user.Id).slice(checkpoint).map((request) => request.Method)).toEqual(['DELETE']);
      const response = userResponse(page, 'GET', user.Id);
      await manageDialog(page).getByRole('button', { name: 'Reload latest user', exact: true }).click();
      const missing = await response;
      expect(missing.status()).toBe(404);
      expect((await missing.json() as { Error: { Code: string } }).Error.Code).toBe('not_found');
      await expect(manageDialog(page)).not.toBeVisible();
      await expect(page.getByRole('button', { name: `Manage ${user.Name}`, exact: true })).toHaveCount(0);
      await expect(page.getByRole('alert').filter({ hasText: 'This user is no longer available.' })).toBeVisible();
      const recovery = targetRequests(requests, user.Id).slice(checkpoint);
      expect(recovery.map((request) => request.Method)).toEqual(['DELETE', 'GET']);
      expectDeleteRequest(recovery[0], user, origin);
      interception.assertCommitted();
    } finally {
      await interception.dispose();
    }
  });

  await test.step('freeze every mutation after last-administrator rejection until the user is explicitly reloaded', async () => {
    const user = await openUser(page, owned, administrator);
    const checkpoint = targetRequests(requests, user.Id).length;
    const url = new URL(userPath(user.Id), page.url()).href;
    let rejectedDeletes = 0;
    const rejectDelete = async (route: Route) => {
      if (route.request().method() !== 'DELETE') {
        await route.continue();
        return;
      }
      assertOwned(owned, user.Id);
      rejectedDeletes += 1;
      // Exercise the guard without changing or deleting any pre-existing administrator.
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({ Error: { Code: 'last_administrator', Message: 'Keep at least one enabled administrator.' } }),
      });
    };
    await page.route(url, rejectDelete);
    try {
      const confirmation = await openDelete(page);
      const response = userResponse(page, 'DELETE', user.Id);
      await confirmation.getByRole('button', { name: 'Delete user', exact: true }).click();
      expect((await response).status()).toBe(409);
      await expect(confirmation.getByRole('alert').filter({ hasText: /at least one enabled administrator/i })).toBeVisible();
      await expect(confirmation.getByRole('button', { name: 'Delete user', exact: true })).toBeDisabled();
      await confirmation.getByRole('button', { name: 'Cancel', exact: true }).click();
      await expectFrozenManagement(page);
      const blockedDraft = `delete-blocked-admin-${suffix}`;
      await manageDialog(page).getByRole('textbox', { name: /^Username/ }).fill(blockedDraft);
      await expectFrozenManagement(page);
      await manageDialog(page).getByRole('textbox', { name: /^Username/ }).press('Enter');
      await expect(manageDialog(page).getByRole('textbox', { name: /^Username/ })).toHaveValue(blockedDraft);
      await manageDialog(page).getByRole('textbox', { name: /^Username/ }).fill(user.Name);
      expect(rejectedDeletes).toBe(1);
      expect(targetRequests(requests, user.Id).slice(checkpoint).map((request) => request.Method)).toEqual(['DELETE']);
      const reloaded = await reloadLatest(page, user);
      expect(reloaded).toEqual(user);
      await openDelete(page);
      const secondResponse = userResponse(page, 'DELETE', user.Id);
      await deleteDialog(page).getByRole('button', { name: 'Delete user', exact: true }).click();
      expect((await secondResponse).status()).toBe(409);
      await expect(deleteDialog(page).getByRole('button', { name: 'Delete user', exact: true })).toBeDisabled();
      const secondReload = await reloadLatest(page, user);
      expect(secondReload.Revision).toBe(user.Revision);
      expect(rejectedDeletes).toBe(2);
      const decisions = targetRequests(requests, user.Id).slice(checkpoint);
      expect(decisions.map((request) => request.Method)).toEqual(['DELETE', 'GET', 'DELETE', 'GET']);
      expectDeleteRequest(decisions[0], user, origin);
      expectDeleteRequest(decisions[2], user, origin);
      await manageDialog(page).getByRole('button', { name: 'Close', exact: true }).click();
      await expect(manageDialog(page)).not.toBeVisible();
    } finally {
      await page.unroute(url, rejectDelete);
    }
  });

  await test.step('delete only the temporary administrator itself and expire locally without another logout request', async () => {
    const selfContext = await browser.newContext({ baseURL });
    const selfRequests: UserRequest[] = [];
    await protectExistingUsers(selfContext, owned, selfRequests);
    selfContext.on('page', recordErrors);
    try {
      const selfPage = await selfContext.newPage();
      await signIn(selfPage, administrator.Name, adminPassword);
      const user = await openUser(selfPage, owned, administrator);
      expect((await selfContext.cookies()).some((cookie) => cookie.name === 'goby_session')).toBe(true);
      const checkpoint = targetRequests(selfRequests, user.Id).length;
      const confirmation = await openDelete(selfPage);
      await expect(confirmation).toContainText(/sign out|signed out|return to sign in/i);
      await confirmDelete(selfPage, user, true);
      await expect(selfPage.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
      expect((await selfContext.cookies()).some((cookie) => cookie.name === 'goby_session')).toBe(false);
      const deletions = targetRequests(selfRequests, user.Id).slice(checkpoint);
      expect(deletions).toHaveLength(1);
      expectDeleteRequest(deletions[0], user, origin);
      expect(selfRequests.filter((request) => request.Pathname === '/admin/v1/session')).toEqual([]);
      expect((await selfContext.request.get('/admin/v1/session')).status()).toBe(401);
      await selfPage.reload();
      await expect(selfPage.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
      expect(selfRequests.filter((request) => request.Pathname === '/admin/v1/session')).toEqual([]);
      expect((await context.request.get(userPath(user.Id))).status()).toBe(404);
    } finally {
      await selfContext.close();
    }
  });

  await test.step('recover an uncertain self-deletion through a single unauthorized GET without replay or logout', async () => {
    const selfContext = await browser.newContext({ baseURL });
    const selfRequests: UserRequest[] = [];
    await protectExistingUsers(selfContext, owned, selfRequests);
    selfContext.on('page', recordErrors);
    try {
      const selfPage = await selfContext.newPage();
      await signIn(selfPage, unknownAdministrator.Name, unknownAdminPassword);
      const user = await openUser(selfPage, owned, unknownAdministrator);
      const checkpoint = targetRequests(selfRequests, user.Id).length;
      const interception = await loseDeleteResponse(selfPage, owned, user, true);
      try {
        const confirmation = await openDelete(selfPage);
        await confirmation.getByRole('button', { name: 'Delete user', exact: true }).click();
        await expect(confirmation.getByRole('alert').filter({ hasText: /could not be confirmed|may already/i })).toBeVisible();
        interception.assertCommitted();
        await expect(confirmation.getByRole('button', { name: 'Delete user', exact: true })).toBeDisabled();
        expect(targetRequests(selfRequests, user.Id).slice(checkpoint).map((request) => request.Method)).toEqual(['DELETE']);
        const response = userResponse(selfPage, 'GET', user.Id);
        await confirmation.getByRole('button', { name: 'Reload latest user', exact: true }).click();
        expect((await response).status()).toBe(401);
        await expect(selfPage.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
        await expect(deleteDialog(selfPage)).not.toBeVisible();
        await expect(manageDialog(selfPage)).not.toBeVisible();
        const recovery = targetRequests(selfRequests, user.Id).slice(checkpoint);
        expect(recovery.map((request) => request.Method)).toEqual(['DELETE', 'GET']);
        expectDeleteRequest(recovery[0], user, origin);
        expect(selfRequests.filter((request) => request.Pathname === '/admin/v1/session')).toEqual([]);
        expect((await context.request.get(userPath(user.Id))).status()).toBe(404);
        interception.assertCommitted();
      } finally {
        await interception.dispose();
      }
    } finally {
      await selfContext.close();
    }
  });

  // Keep watching completed scenarios while later steps run so delayed retries also fail.
  for (const [user, count] of [[ordinary, 1], [conflicting, 2], [unknown, 1], [administrator, 2]] as const) {
    expect(targetRequests(requests, user.Id).filter((request) => request.Method === 'DELETE')).toHaveLength(count);
  }
  expect(requests.filter((request) => request.Pathname === '/admin/v1/session')).toEqual([]);
  expect(await readUser(context.request, originalAdministrator.Id)).toEqual(originalAdministrator);
  expect(pageErrors).toEqual([]);
});
