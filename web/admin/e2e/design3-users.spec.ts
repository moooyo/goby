import type { Locator, Page } from '@playwright/test';
import type { LoginSession, ManagedUser, UpdateUserInput, User } from '../src/api';
import { administrator, expect, json, stamp, test } from './dashboard-fixture';
import { initialUserRevision, installUserDesignFixture, viewerId } from './user-design-fixture';

// All writes in these cases go through the synthetic dashboard transport.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off' });
const userURL = '/admin/access/users';
const userList = '/admin/v1/users';
const loginActivity = `${userList}/activity`;
const sessionsURL = '/admin/v1/sessions';
const stale = '2026-07-01T04:30:00Z';

function account(index: number, changes: Partial<User> = {}): User {
  return { ...administrator, Id: `account-${index}`, Name: `Account ${String(index).padStart(3, '0')}`, IsAdministrator: false, ...changes };
}
function activity(Id: string, LastLoginDate: string | null) {
  return { Id, LastLoginDate, LastActivityDate: LastLoginDate, LastPlayedDate: stamp, PlayedItemCount: 123 }; // Playback projections must not appear in Users.
}
function deferred() {
  let release = () => {};
  const promise = new Promise<void>((resolve) => { release = resolve; });
  return { promise, release };
}
const row = (page: Page, name: string) => page.getByRole('row').filter({ has: page.getByRole('button', { name: `Manage ${name}`, exact: true }) });
async function openUser(page: Page, name: string): Promise<Locator> {
  await page.getByRole('button', { name: `Manage ${name}`, exact: true }).click();
  return page.getByRole('dialog', { name: `Manage user ${name}`, exact: true });
}

test('loads the visible page first and reprioritizes unread accounts in a 20,000-account directory', async ({ api, page }) => {
  const lastAccount = 19_999;
  const lastAccountId = `account-${lastAccount}`;
  const accounts = [administrator, ...Array.from({ length: lastAccount }, (_, offset) => account(offset + 1))];
  api.handlers.set(`GET ${userList}`, (route) => json(route, { Items: accounts, TotalRecordCount: accounts.length }));
  const requested: string[][] = [];
  const background = deferred();
  api.handlers.set(`GET ${loginActivity}`, async (route, request) => {
    const ids = new URL(request.url()).searchParams.get('UserIds')!.split(',');
    requested.push(ids);
    if (requested.length === 2) await background.promise;
    await json(route, { Items: ids.map((id) => activity(id, id === 'account-1' || id === lastAccountId ? null : stamp)), ObservedAt: stamp });
  });

  try {
    await page.goto(userURL);
    const groups = page.getByRole('group', { name: 'Account groups' });
    const idle = groups.getByRole('button', { name: /No login in 90 days/ });
    await expect(row(page, 'Account 001')).toContainText('No login recorded');
    await expect(row(page, 'Account 002').locator(`time[datetime="${stamp}"]`)).toHaveCount(2); // last sign-in + created date
    await expect(idle).toContainText('Calculating');
    await expect(groups.getByRole('button', { name: 'All 20,000', exact: true })).toBeVisible();
    await expect(page.getByRole('progressbar', { name: 'Login records loaded' })).toBeVisible();
    await expect.poll(() => requested.length).toBe(2);
    expect(requested[0]).toEqual(accounts.slice(0, 50).map((user) => user.Id));
    expect(requested[1]).toHaveLength(200);
    await expect(page.getByRole('table', { name: 'Server users' }).getByRole('row')).toHaveCount(51);
    await expect(page.getByRole('columnheader', { name: 'Watched items' })).toHaveCount(0);
    await idle.click();
    await expect(page.getByText('The list may be incomplete.', { exact: false })).toBeVisible();
    await row(page, 'Account 001').getByRole('checkbox').check();
    await expect(page.getByRole('button', { name: 'Disable', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: /Disable these accounts/ })).toHaveCount(0);

    await groups.getByRole('button', { name: /^All / }).click();
    await page.getByRole('button', { name: 'Go to next page' }).click();
    await expect(page.getByRole('button', { name: 'Manage Account 050', exact: true })).toBeVisible();
    await page.getByRole('searchbox', { name: 'Find accounts' }).fill(`ACCOUNT ${lastAccount}`);
    await expect(page.getByRole('button', { name: `Manage Account ${lastAccount}`, exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Go to next page' })).toBeDisabled();
    expect(requested).toHaveLength(2);
    background.release();
    await expect(row(page, `Account ${lastAccount}`)).toContainText('No login recorded');
    expect(requested[2]).toEqual([lastAccountId]);
    await expect(idle).toHaveText('No login in 90 days 2', { timeout: 35_000 });
    expect(requested.every((batch) => batch.length <= 200)).toBe(true);
    expect(new Set(requested.flat()).size).toBe(20_000);
    expect(api.reads(userList)).toHaveLength(1);
    expect(api.writes()).toHaveLength(0);
  } finally { background.release(); }
});

test('batch disabling preserves the latest complete policy, skips conflicts, and stops without cancelling a write', async ({ api, page }) => {
  const policy = { ...installUserDesignFixture(api).initialPolicy, RemoteClientBitrateLimit: 12_345_678, RestrictedFeatures: ['goby_downloads'] };
  const accounts: ManagedUser[] = [1, 2, 3, 4].map((index) => ({ ...account(index), Policy: policy, Revision: initialUserRevision }));
  const latest = new Map(accounts.map((user) => [user.Id, user]));
  const paused = deferred();
  const secondRequest = deferred();
  let reads = 0;
  const calls: string[] = [];
  api.handlers.set(`GET ${userList}`, (route) => {
    reads += 1;
    return json(route, { Items: [administrator, ...latest.values()], TotalRecordCount: latest.size + 1 });
  });
  api.handlers.set(`GET ${loginActivity}`, (route) => json(route, { Items: [administrator, ...accounts].map((user) => activity(user.Id, stale)), ObservedAt: stamp }));
  for (const user of accounts) {
    const path = `${userList}/${user.Id}`;
    api.handlers.set(`GET ${path}`, (route) => { calls.push(`GET ${user.Id}`); return json(route, { User: latest.get(user.Id) }); });
    api.handlers.set(`PUT ${path}`, async (route, request) => {
      calls.push(`PUT ${user.Id}`);
      expect(request.postDataJSON()).toEqual({ Revision: initialUserRevision, Name: user.Name, IsAdministrator: false, IsDisabled: true, Policy: policy });
      if (user.Id === accounts[0].Id) return json(route, { Error: { Code: 'revision_conflict', Message: 'Changed elsewhere' } }, 409);
      if (user.Id === accounts[1].Id) { secondRequest.release(); await paused.promise; }
      const saved = { ...user, IsDisabled: true, Revision: (BigInt(user.Revision) + 1n).toString() };
      latest.set(user.Id, saved);
      await json(route, { User: saved, CurrentSessionRevoked: false });
    });
  }
  await page.goto(userURL);
  await page.getByRole('group', { name: 'Account groups' }).getByRole('button', { name: /No login in 90 days/ }).click();
  try {
    await page.getByRole('button', { name: 'Disable these accounts (4)', exact: true }).click();
    const confirmation = page.getByRole('dialog', { name: 'Disable accounts (4)?', exact: true });
    await expect(confirmation).toContainText('Administrator accounts are excluded.');
    await confirmation.getByRole('button', { name: 'Disable accounts (4)', exact: true }).click();
    await secondRequest.promise;
    const progress = page.getByRole('dialog', { name: 'Disabling accounts', exact: true });
    await expect(progress.getByRole('progressbar')).toHaveAttribute('aria-valuetext', '1 of 4 processed');
    expect(calls).toEqual(['GET account-1', 'PUT account-1', 'GET account-2', 'PUT account-2']);
    await progress.getByRole('button', { name: 'Stop', exact: true }).click();
    await expect(progress).toContainText('Stopping after the current request');
    await page.keyboard.press('Escape');
    await expect(progress).toBeVisible();
    paused.release();
    const summary = page.getByRole('dialog', { name: 'Stopped', exact: true });
    await expect(summary).toContainText('Accounts disabled: 1');
    await expect(summary).toContainText('another administrator changed the account: 1');
    await expect(summary.getByRole('progressbar')).toHaveAttribute('aria-valuetext', '2 of 4 processed');
    await summary.getByRole('button', { name: 'Done', exact: true }).click();
    await expect(row(page, 'Account 002')).toContainText('Disabled');
    await expect(page.getByRole('button', { name: 'Disable these accounts (3)', exact: true })).toBeVisible();
    expect(calls).toEqual(['GET account-1', 'PUT account-1', 'GET account-2', 'PUT account-2']);
    expect(reads).toBe(2);
  } finally { paused.release(); }
});

test('checked batches report the last enabled administrator without replaying the rejected change', async ({ api, page }) => {
  const policy = installUserDesignFixture(api).initialPolicy;
  api.handlers.set(`GET ${userList}/${administrator.Id}`, (route) => json(route, { User: { ...administrator, Revision: initialUserRevision, Policy: policy } }));
  api.handlers.set(`PUT ${userList}/${administrator.Id}`, (route) => json(route, { Error: { Code: 'last_administrator', Message: 'Keep one enabled administrator' } }, 409));
  await page.goto(userURL);
  await row(page, administrator.Name).getByRole('checkbox').check();
  await expect(page.getByRole('checkbox', { name: 'Select all accounts on this page' })).toHaveAttribute('aria-checked', 'mixed');
  await page.getByRole('button', { name: 'Disable', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Disable accounts (1)?', exact: true });
  await expect(confirmation).toContainText('This selection includes your own account.');
  await confirmation.getByRole('button', { name: 'Disable accounts (1)', exact: true }).click();
  const summary = page.getByRole('dialog', { name: 'Batch complete', exact: true });
  await expect(summary).toContainText('Accounts disabled: 0');
  await expect(summary).toContainText('Skipped to keep at least one enabled administrator: 1.');
  expect(api.writes()).toHaveLength(1);
});

test('a lost batch write receipt stops later submissions and requires a refreshed list', async ({ api, page }) => {
  const { initialPolicy } = installUserDesignFixture(api);
  const users = [account(1), account(2)];
  const path = `${userList}/${users[0].Id}`;
  api.handlers.set(`GET ${userList}`, (route) => json(route, { Items: users, TotalRecordCount: users.length }));
  api.handlers.set(`GET ${path}`, (route) => json(route, { User: { ...users[0], Revision: initialUserRevision, Policy: initialPolicy } }));
  api.handlers.set(`PUT ${path}`, (route) => route.abort('failed'));
  await page.goto(userURL);
  await page.getByRole('checkbox', { name: 'Select all accounts on this page' }).check();
  await page.getByRole('button', { name: 'Disable', exact: true }).click();
  await page.getByRole('button', { name: 'Disable accounts (2)', exact: true }).click();
  const summary = page.getByRole('dialog', { name: 'Review the result', exact: true });
  await expect(summary).toContainText('The result of 1 request(s) could not be confirmed.');
  await expect.poll(() => api.reads(userList).length).toBe(2);
  expect(api.writes().map((entry) => entry.path)).toEqual([path]);
  expect(api.reads(`${userList}/${users[1].Id}`)).toHaveLength(0);
});

test('uses role cards and enforces the password length in UTF-8 bytes before creating a user', async ({ api, page }) => {
  const accounts: User[] = [administrator];
  const createInput = { Name: 'New operator', Password: '猫'.repeat(24), IsAdministrator: true };
  api.handlers.set(`GET ${userList}`, (route) => json(route, { Items: accounts, TotalRecordCount: accounts.length }));
  api.handlers.set(`POST ${userList}`, (route, request) => {
    expect(request.postDataJSON()).toEqual(createInput);
    const created = account(1, { Name: createInput.Name, IsAdministrator: true });
    accounts.push(created);
    return json(route, { User: created }, 201);
  });
  await page.goto(userURL);
  await page.getByRole('button', { name: 'Add user', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Add user', exact: true });
  await expect(dialog.getByRole('radio', { name: 'Member', exact: true })).toBeChecked();
  await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill(` ${createInput.Name} `);
  await dialog.getByLabel(/^Password/).fill('猫'.repeat(25));
  await dialog.getByRole('radio', { name: 'Administrator', exact: true }).check();
  const submit = dialog.getByRole('button', { name: 'Add user', exact: true });
  await expect(submit).toBeDisabled();
  expect(api.writes()).toHaveLength(0);
  await dialog.getByLabel(/^Password/).fill(createInput.Password);
  await submit.click();
  await expect(page.getByText('Added administrator New operator.', { exact: true })).toBeVisible();
  await expect(row(page, createInput.Name)).toContainText('Administrator');
});

test('blocks passwordless promotion and protects the only enabled administrator', async ({ api, page }) => {
  const { user, initialPolicy } = installUserDesignFixture(api);
  api.handlers.set(`GET ${userList}/${viewerId}`, (route) => json(route, { User: { ...user, HasPassword: false } }));
  api.handlers.set(`GET ${userList}/${administrator.Id}`, (route) => json(route, { User: { ...administrator, Revision: initialUserRevision, Policy: { ...initialPolicy, EnableAllFolders: false } } }));
  await page.goto(userURL);
  const viewer = await openUser(page, user.Name);
  await expect(viewer.getByRole('radio', { name: 'Administrator', exact: true })).toBeDisabled();
  await expect(viewer.getByText(/This account has no password/)).toBeVisible();
  await viewer.getByRole('button', { name: 'Close', exact: true }).click();

  const self = await openUser(page, administrator.Name);
  await expect(self.getByRole('radio', { name: 'Member', exact: true })).toBeDisabled();
  await expect(self.getByRole('switch', { name: 'All libraries', exact: true })).toBeDisabled();
  await expect(self.getByRole('switch', { name: 'Feature films', exact: true })).toBeDisabled();
  await self.getByRole('tab', { name: 'Profile' }).click();
  await expect(self.getByRole('switch', { name: 'Disable account' })).toBeDisabled();
  expect(api.writes()).toHaveLength(0);
});

test('confirms self-demotion and handles CurrentSessionRevoked without a logout request', async ({ api, page }) => {
  const { initialPolicy, user } = installUserDesignFixture(api);
  const administratorDetail = { ...administrator, Revision: initialUserRevision, Policy: initialPolicy };
  api.handlers.set(`GET ${userList}`, (route) => json(route, { Items: [administrator, { ...user, IsAdministrator: true }], TotalRecordCount: 2 }));
  api.handlers.set(`GET ${userList}/${administrator.Id}`, (route) => json(route, { User: administratorDetail }));
  api.handlers.set(`PUT ${userList}/${administrator.Id}`, (route, request) => {
    const input = request.postDataJSON() as UpdateUserInput;
    expect(input).toEqual({ Revision: initialUserRevision, Name: administrator.Name, IsAdministrator: false, IsDisabled: false, Policy: initialPolicy });
    return json(route, { User: { ...administratorDetail, ...input, Revision: '9007199254740994' }, CurrentSessionRevoked: true });
  });
  await page.goto(userURL);
  const self = await openUser(page, administrator.Name);
  await self.getByRole('radio', { name: 'Member', exact: true }).check();
  await self.getByRole('button', { name: 'Save changes', exact: true }).click();
  const confirmation = page.getByRole('alertdialog', { name: `Change ${administrator.Name} to a member?`, exact: true });
  await expect(confirmation).toContainText('Administrator logins will end immediately. Client logins remain active');
  await expect(confirmation).toContainText('You are changing your own account.');
  expect(api.writes()).toHaveLength(0);
  await confirmation.getByRole('button', { name: 'Change to member and sign out', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
  expect(api.writes().map((entry) => [entry.method, entry.path])).toEqual([['PUT', `${userList}/${administrator.Id}`]]);
});

function login(index: number, changes: Partial<LoginSession> = {}): LoginSession {
  return { Id: `login-${index}`, UserId: administrator.Id, UserName: administrator.Name, UserIsAdministrator: true, UserIsDisabled: false,
    Kind: 'emby', Client: 'Test client', DeviceId: 'shared-device', DeviceName: `Screen ${index}`, ApplicationVersion: '1.0', CreatedAt: stamp, LastSeenAt: stamp,
    ExpiresAt: '2026-11-05T08:00:00Z', RevokedAt: null, Status: 'active', IsCurrent: false, ...changes };
}

test('remembers a witnessed login after revocation even when the directory snapshot had no record', async ({ api, page }) => {
  const { user } = installUserDesignFixture(api);
  const createdAt = '2026-11-04T08:05:00Z';
  const session = login(1, { UserId: user.Id, UserName: user.Name, UserIsAdministrator: false, CreatedAt: createdAt, LastSeenAt: createdAt });
  let revoked = false;
  api.handlers.set(`GET ${sessionsURL}`, (route, request) => {
    const query = new URL(request.url()).searchParams;
    expect(query.get('UserId')).toBe(user.Id);
    expect(query.get('Status')).toBe('active');
    const items = revoked ? [] : [session];
    return json(route, { Items: items, TotalRecordCount: items.length, StartIndex: 0, Limit: Number(query.get('Limit')) });
  });
  api.handlers.set(`POST ${sessionsURL}/${session.Id}/revoke`, (route) => {
    revoked = true;
    return json(route, { SessionId: session.Id, UserId: user.Id, Kind: session.Kind, RevokedAt: createdAt, CurrentSessionRevoked: false });
  });
  await page.goto(userURL);
  await expect(row(page, user.Name)).toContainText('No login recorded');
  const sheet = await openUser(page, user.Name);
  const devices = sheet.getByRole('tab', { name: 'Login devices', exact: true });
  await devices.click();
  await sheet.getByRole('button', { name: 'Sign out all logins', exact: true }).click();
  await page.getByRole('dialog', { name: 'Sign out all listed logins?', exact: true }).getByRole('button', { name: 'Sign out all logins', exact: true }).click();
  await page.getByRole('dialog', { name: 'Batch complete', exact: true }).getByRole('button', { name: 'Done', exact: true }).click();
  await expect(sheet.getByText('This account has no active logins.', { exact: true })).toBeVisible();
  await sheet.getByRole('tab', { name: 'Profile', exact: true }).click();
  await expect(sheet.locator(`time[datetime="${createdAt}"]`)).toBeVisible();
  await sheet.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(row(page, user.Name).locator(`time[datetime="${createdAt}"]`)).toBeVisible();
  expect(api.reads(userList)).toHaveLength(1);
  expect(api.writes()).toHaveLength(1);
});

test('login devices use only scoped active sessions and sign out the current login last', async ({ api, page }) => {
  const { initialPolicy } = installUserDesignFixture(api);
  api.handlers.set(`GET ${userList}/${administrator.Id}`, (route) => json(route, { User: { ...administrator, Policy: initialPolicy, Revision: initialUserRevision } }));
  const current = login(1, { Kind: 'admin', DeviceName: 'This browser', IsCurrent: true });
  const sessions = [current, login(2), login(3)];
  const revoked: string[] = [];
  api.handlers.set(`GET ${sessionsURL}`, (route, request) => {
    const query = new URL(request.url()).searchParams;
    expect(query.get('UserId')).toBe(administrator.Id);
    expect(query.get('Status')).toBe('active');
    const items = sessions.filter((session) => !revoked.includes(session.Id));
    return json(route, { Items: items, TotalRecordCount: items.length, StartIndex: 0, Limit: Number(query.get('Limit')) });
  });
  for (const session of sessions) {
    api.handlers.set(`POST ${sessionsURL}/${session.Id}/revoke`, (route, request) => {
      expect(request.postDataJSON()).toEqual({});
      revoked.push(session.Id);
      return json(route, { SessionId: session.Id, UserId: session.UserId, Kind: session.Kind, RevokedAt: stamp, CurrentSessionRevoked: session.IsCurrent });
    });
  }
  await page.goto(userURL);
  const self = await openUser(page, administrator.Name);
  expect(api.reads(sessionsURL)).toHaveLength(0);
  await self.getByRole('tab', { name: 'Login devices', exact: true }).click();
  const list = self.getByRole('list', { name: 'Active logins', exact: true });
  await expect(list.getByRole('listitem')).toHaveCount(3);
  await expect(list.getByRole('listitem').filter({ hasText: 'This browser' })).toContainText('Current login');
  await expect(list.getByRole('listitem').filter({ hasText: 'This browser' })).toContainText('Administrator login');
  await list.getByRole('listitem').filter({ hasText: 'Screen 2' }).getByRole('button', { name: 'Sign out', exact: true }).click();
  await page.getByRole('dialog', { name: 'Sign out the login on Screen 2?' }).getByRole('button', { name: 'Sign out', exact: true }).click();
  await page.getByRole('dialog', { name: 'Batch complete' }).getByRole('button', { name: 'Done', exact: true }).click();
  await expect(list.getByRole('listitem')).toHaveCount(2);
  await self.getByRole('button', { name: 'Sign out all logins', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Sign out all listed logins?', exact: true });
  await expect(confirmation).toContainText('This includes your current administrator login.');
  await confirmation.getByRole('button', { name: 'Sign out all logins', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
  expect(revoked).toEqual(['login-2', 'login-3', 'login-1']);
  expect(api.writes().some((entry) => entry.method === 'DELETE')).toBe(false);
  expect(api.reads('/admin/v1/playback/sessions')).toHaveLength(0);
  expect(api.reads('/admin/v1/devices')).toHaveLength(0);
});
