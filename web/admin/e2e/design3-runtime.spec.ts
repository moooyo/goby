import { randomBytes } from 'node:crypto';
import { expect, test } from '@playwright/test';

// Only enable on a new, task-owned loopback deployment with disposable accounts.
test.skip(process.env.GOBY_DESIGN3_LIVE !== '1', 'Requires an isolated Goby deployment and a dedicated administrator.');
test.use({ screenshot: 'off', trace: 'off', video: 'off' });

test('native Design 3 account details revoke real client logins without exposing playback', async ({ context, page, baseURL }, testInfo) => {
  const administrator = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  if (!baseURL || !/^http:\/\/127\.0\.0\.1:[1-9]\d{3,4}\/?$/.test(baseURL) || !administrator || !password) throw new Error('Use an isolated loopback deployment and provide the fixture administrator.');
  const origin = new URL(baseURL).origin;
  const suffix = randomBytes(5).toString('hex');
  const memberName = `design3-member-${suffix}`;
  const memberPassword = randomBytes(24).toString('base64url');
  await page.addInitScript(() => localStorage.setItem('goby.admin.language', 'en'));
  await page.goto('/admin/access/sessions');
  await page.getByLabel('Username', { exact: true }).fill(administrator);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/access\/users$/);
  await page.getByRole('button', { name: 'Add user', exact: true }).click();
  const add = page.getByRole('dialog', { name: 'Add user', exact: true });
  await add.getByRole('textbox', { name: 'Name', exact: true }).fill(memberName);
  await add.getByLabel(/^Password/).fill(memberPassword);
  const created = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/admin/v1/users');
  await add.getByRole('button', { name: 'Add user', exact: true }).click();
  const creation = await created;
  expect(creation.status()).toBe(201);
  const { User: member } = await creation.json() as { User: { Id: string; IsAdministrator: boolean } };
  expect(member.IsAdministrator).toBe(false);

  const clientLogins: { device: string; token: string }[] = [];
  for (const [index, device] of ['Test tablet', 'Test TV'].entries()) {
    const response = await context.request.post('/emby/Users/AuthenticateByName', {
      headers: { Origin: origin, Authorization: `Emby Client="Design 3 live test", DeviceId="design3-${suffix}-${index}", Device="${device}", Version="1.0"` },
      data: { Username: memberName, Pw: memberPassword },
    });
    expect(response.status()).toBe(200);
    clientLogins.push({ device, token: (await response.json() as { AccessToken: string }).AccessToken });
  }
  const view = (token: string) => context.request.get(`/emby/Users/${member.Id}/Views`, { headers: { 'X-Emby-Token': token } });
  for (const login of clientLogins) expect((await view(login.token)).status()).toBe(200);
  await page.getByRole('button', { name: `Manage ${memberName}`, exact: true }).click();
  const user = page.getByRole('dialog', { name: `Manage user ${memberName}` });
  const loginsRequest = page.waitForRequest((request) => new URL(request.url()).pathname === '/admin/v1/sessions');
  await user.getByRole('tab', { name: 'Login devices', exact: true }).click();
  const params = new URL((await loginsRequest).url()).searchParams;
  expect(params.get('UserId')).toBe(member.Id);
  expect(params.get('Status')).toBe('active');
  const logins = user.getByRole('list', { name: 'Active logins', exact: true });
  await expect(logins.getByRole('listitem')).toHaveCount(2);
  await expect(logins).not.toContainText(/Now playing|Playback progress/);
  await page.screenshot({ path: testInfo.outputPath('real-account-login-devices.png'), animations: 'disabled' });

  for (const [index, login] of clientLogins.entries()) {
    const batch = index > 0;
    await (batch ? user : logins.getByRole('listitem').filter({ hasText: login.device }))
      .getByRole('button', { name: batch ? 'Sign out all logins' : 'Sign out', exact: true }).click();
    const confirm = page.getByRole('dialog', { name: batch ? 'Sign out all listed logins?' : `Sign out the login on ${login.device}?` });
    const revoked = page.waitForResponse((response) => response.request().method() === 'POST' && /\/admin\/v1\/sessions\/[^/]+\/revoke$/.test(new URL(response.url()).pathname));
    await confirm.getByRole('button', { name: batch ? 'Sign out all logins' : 'Sign out', exact: true }).click();
    expect((await revoked).status()).toBe(200);
    const summary = page.getByRole('dialog', { name: 'Batch complete', exact: true });
    await expect(summary).toContainText('Logins signed out: 1');
    await summary.getByRole('button', { name: 'Done', exact: true }).click();
    expect((await view(login.token)).status()).toBe(401);
    if (!batch) expect((await view(clientLogins[1].token)).status()).toBe(200);
  }
  await expect(user.getByRole('button', { name: 'Sign out all logins', exact: true })).toBeDisabled();
  await expect(user.getByText('This account has no active logins.', { exact: true })).toBeVisible();
});

test('native maintenance reminder sends to the real empty playback audience without restarting', async ({ page, baseURL }, testInfo) => {
  const administrator = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  if (!baseURL || !/^http:\/\/127\.0\.0\.1:[1-9]\d{3,4}\/?$/.test(baseURL) || !administrator || !password) throw new Error('Use an isolated loopback deployment and provide the fixture administrator.');
  await page.addInitScript(() => localStorage.setItem('goby.admin.language', 'en'));
  await page.goto('/admin/settings/general');
  await page.getByLabel('Username', { exact: true }).fill(administrator);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  let restarts = 0;
  page.on('request', (request) => { if (new URL(request.url()).pathname === '/admin/v1/server/restart') restarts++; });
  await page.getByRole('button', { name: 'Restart server', exact: true }).click();
  const restart = page.getByRole('dialog', { name: 'Restart server?', exact: true });
  await expect(restart).toContainText('0 clients are currently playing.');
  await restart.getByRole('button', { name: 'Send reminder first', exact: true }).click();
  const message = page.getByRole('dialog', { name: 'Send a message to playing clients', exact: true });
  await expect(message.getByRole('textbox', { name: 'Title (optional)', exact: true })).toHaveValue('Server maintenance');
  await expect(message.getByRole('combobox', { name: 'Display duration', exact: true })).toHaveValue('30000');
  await expect(message).toContainText('0 clients are playing.');
  await page.screenshot({ path: testInfo.outputPath('real-maintenance-reminder.png'), animations: 'disabled' });
  const queued = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/admin/v1/playback/message');
  await message.getByRole('button', { name: 'Send message', exact: true }).click();
  const receipt = await queued;
  expect(receipt.status()).toBe(202);
  expect((await receipt.json()).Results).toEqual([]);
  await expect(page.getByText('Queued for 0 clients.', { exact: false })).toBeVisible();
  expect(restarts).toBe(0);
});
