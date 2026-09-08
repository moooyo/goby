import { randomBytes } from 'node:crypto';
import { expect, test } from '@playwright/test';

test('administrator setup, users, persistent session, and mobile navigation', async ({ page, context }, testInfo) => {
  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  const setupToken = process.env.GOBY_SETUP_TOKEN;
  if (!name || !password) throw new Error('GOBY_SMOKE_NAME and GOBY_SMOKE_PASSWORD are required.');

  const pageErrors: string[] = [];
  const consoleErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('console', (message) => { if (message.type() === 'error') consoleErrors.push(message.text()); });
  const capture = async (file: string) => page.screenshot({ path: testInfo.outputPath(file), fullPage: true, animations: 'disabled' });

  await page.goto('/admin/');
  await expect(page.getByRole('heading', { name: /Set up your server|Sign in to Goby/ })).toBeVisible();
  if (await page.getByRole('heading', { name: 'Set up your server' }).isVisible()) {
    if (!setupToken) throw new Error('GOBY_SETUP_TOKEN is required for a fresh server.');
    await capture('setup-desktop.png');
    await page.getByLabel(/^Setup token/).fill(setupToken);
    await page.getByLabel(/^Administrator username/).fill(name);
    await page.getByLabel(/^Password/).fill(password);
    await page.getByRole('button', { name: 'Create administrator', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Sign in to Goby' })).toBeVisible();
    await expect(page.getByText('Your administrator account is ready. Sign in to continue.')).toBeVisible();
  }

  await capture('login-desktop.png');
  await page.getByLabel(/^Username/).fill(name);
  await page.getByLabel(/^Password/).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
  await expect(page.getByText('PostgreSQL', { exact: true })).toBeVisible();
  await expect(page.getByText('connected', { exact: true })).toBeVisible();
  await capture('overview-desktop.png');

  const cookie = (await context.cookies()).find((item) => item.name === 'goby_session');
  expect(cookie?.httpOnly).toBe(true);
  expect(cookie?.sameSite).toBe('Strict');
  expect(await page.evaluate(() => Object.keys(localStorage))).toEqual([]);
  expect(await page.evaluate(() => Object.keys(sessionStorage))).toEqual([]);

  await page.getByRole('link', { name: 'Users', exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/users$/);
  await expect(page.getByRole('heading', { name: 'Users', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Create user', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Create user' });
  await expect(dialog).toBeVisible();
  const memberName = `browser-smoke-${Date.now()}`;
  await dialog.getByLabel(/^Username/).fill(memberName);
  await dialog.getByLabel(/^Password/).fill(randomBytes(24).toString('base64url'));
  await expect(dialog.getByRole('switch', { name: 'Administrator access' })).not.toBeChecked();
  await capture('create-user-desktop.png');
  await dialog.getByRole('button', { name: 'Create user', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const memberRow = page.getByRole('row').filter({ has: page.getByText(memberName, { exact: true }) });
  await expect(memberRow.getByText('Member', { exact: true })).toBeVisible();
  await expect(memberRow.getByText('Active', { exact: true })).toBeVisible();
  await capture('users-desktop.png');

  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Sign in to Goby' })).toBeVisible();
  expect((await context.cookies()).some((item) => item.name === 'goby_session')).toBe(false);
  await page.getByLabel(/^Username/).fill(name);
  await page.getByLabel(/^Password/).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Users', exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Users', exact: true })).toBeVisible();
  await expect(page.getByRole('row').filter({ has: page.getByText(memberName, { exact: true }) })).toBeVisible();

  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Open navigation' }).click();
  await capture('navigation-mobile.png');
  await page.getByRole('link', { name: 'Overview', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
  await expect(page.getByText('PostgreSQL', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await capture('overview-mobile.png');
  await page.getByRole('button', { name: 'Open navigation' }).click();
  await page.getByRole('link', { name: 'Users', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Users', exact: true })).toBeVisible();
  await expect(page.getByRole('list', { name: 'Server users' }).getByText(memberName, { exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await capture('users-mobile.png');

  console.log(JSON.stringify({ pageErrors, consoleErrors, screenshots: testInfo.outputDir }));
  expect(pageErrors).toEqual([]);
  expect(consoleErrors.filter((message) => !message.includes('401 (Unauthorized)'))).toEqual([]);
});

test('a stale tab cannot replay a mutation under another administrator', async ({ page, context }, testInfo) => {
  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  if (!name || !password) throw new Error('GOBY_SMOKE_NAME and GOBY_SMOKE_PASSWORD are required.');

  const pageErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await page.goto('/admin/');
  await page.getByLabel(/^Username/).fill(name);
  await page.getByLabel(/^Password/).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('link', { name: 'Users', exact: true }).click();

  const otherName = `browser-admin-${Date.now()}`;
  const otherPassword = randomBytes(24).toString('base64url');
  await page.getByRole('button', { name: 'Create user', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Create user' });
  await dialog.getByLabel(/^Username/).fill(otherName);
  await dialog.getByLabel(/^Password/).fill(otherPassword);
  await dialog.getByRole('switch', { name: 'Administrator access' }).check();
  await dialog.getByRole('button', { name: 'Create user', exact: true }).click();
  await expect(dialog).not.toBeVisible();

  const rejectedName = `browser-stale-${Date.now()}`;
  await page.getByRole('button', { name: 'Create user', exact: true }).click();
  await dialog.getByLabel(/^Username/).fill(rejectedName);
  await dialog.getByLabel(/^Password/).fill(randomBytes(24).toString('base64url'));

  const otherTab = await context.newPage();
  otherTab.on('pageerror', (error) => pageErrors.push(error.message));
  await otherTab.goto('/admin/');
  await otherTab.getByRole('button', { name: 'Sign out', exact: true }).click();
  await otherTab.getByLabel(/^Username/).fill(otherName);
  await otherTab.getByLabel(/^Password/).fill(otherPassword);
  await otherTab.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(otherTab.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();

  let mutationRequests = 0;
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/admin/v1/users') mutationRequests += 1;
  });
  const rejected = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/admin/v1/users');
  await dialog.getByRole('button', { name: 'Create user', exact: true }).click();
  expect((await rejected).status()).toBe(403);
  await expect(page.getByRole('heading', { name: 'Sign in to Goby' })).toBeVisible();
  await expect(page.getByText('Your session has changed or expired. Sign in again to continue.')).toBeVisible();
  await expect(page.getByLabel(/^Username/)).toHaveValue('');
  expect(mutationRequests).toBe(1);

  const usersResponse = await context.request.get('/admin/v1/users');
  expect(usersResponse.status()).toBe(200);
  const users = await usersResponse.json() as { Items: { Name: string }[] };
  expect(users.Items.some((user) => user.Name === rejectedName)).toBe(false);
  const sessionResponse = await context.request.get('/admin/v1/session');
  expect(sessionResponse.status()).toBe(200);
  const session = await sessionResponse.json() as { User: { Name: string } };
  expect(session.User.Name).toBe(otherName);
  await page.screenshot({ path: testInfo.outputPath('changed-session-requires-login.png'), fullPage: true, animations: 'disabled' });
  console.log(JSON.stringify({ pageErrors, rejectedMutationRequests: mutationRequests, unintendedUserCreated: false }));
  expect(pageErrors).toEqual([]);
});
