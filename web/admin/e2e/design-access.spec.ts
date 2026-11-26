import type { ApplicationKey } from '../src/api';
import { administrator, csrf, expect, json, stamp, test } from './dashboard-fixture';
import { destinations, evidence, noOverflow, ready } from './dashboard-review';

// Every API response below is synthetic. These checks cover browser workflows
// and mutation safety without asserting backend or compatible-client behavior.
// Never capture the one-time token, even though its value is synthetic.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off', preferences: { language: 'en', theme: 'light' } });

test('creating an API key displays its complete value once and clears it on close', async ({ page, api }, testInfo) => {
  const syntheticToken = 'synthetic-only-once-token-4c64e987e3ed47ec';
  const created: ApplicationKey = { Id: '202', AppName: 'Synthetic media client', CreatedAt: stamp, LastUsedAt: null, RevokedAt: null, CreatedBy: administrator.Id, IPAddress: '', Status: 'active' };
  const existing: ApplicationKey = { ...created, Id: '201', AppName: 'Home automation' };
  let keys = [existing];
  api.handlers.set('GET /admin/v1/api-keys', (route, request) => {
    const url = new URL(request.url());
    return json(route, { Items: keys, TotalRecordCount: keys.length, StartIndex: Number(url.searchParams.get('StartIndex') ?? 0), Limit: Number(url.searchParams.get('Limit') ?? 50) });
  });
  api.handlers.set('POST /admin/v1/api-keys', async (route) => {
    keys = [created, existing];
    await json(route, { Key: created, AccessToken: syntheticToken });
  });
  await page.goto('/admin/access/api-keys');
  await ready(page, destinations[6]);
  await page.getByRole('button', { name: 'Create API key', exact: true }).first().click();
  const create = page.getByRole('dialog', { name: 'Create API key', exact: true });
  await create.getByRole('textbox', { name: 'Application name', exact: true }).fill('  Synthetic media client  ');
  await evidence(page, testInfo, 'api-key-create-draft');
  await create.getByRole('button', { name: 'Create API key', exact: true }).click();
  const secret = page.getByRole('dialog', { name: 'API key created', exact: true });
  const token = secret.getByRole('textbox', { name: 'Access token', exact: true });
  await expect(token).toHaveValue(syntheticToken);
  await expect(token).toHaveJSProperty('readOnly', true);
  await expect(secret).toContainText('The complete key is shown only in this dialog');
  await expect(secret.getByRole('button', { name: 'Create API key', exact: true })).toHaveCount(0);
  expect(api.writes()).toEqual([{ method: 'POST', path: '/admin/v1/api-keys', body: { AppName: 'Synthetic media client' }, csrf }]);
  await secret.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(secret).not.toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Access token', exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => Array.from(document.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>('input,textarea')).map((element) => element.value))).not.toContain(syntheticToken);
  expect(await page.evaluate(() => JSON.stringify([Object.entries(localStorage), Object.entries(sessionStorage)]))).not.toContain(syntheticToken);
  await page.reload();
  await expect(page.getByRole('main')).toContainText(created.AppName);
  await expect(page.getByRole('textbox', { name: 'Access token', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /Reveal|Show complete key/ })).toHaveCount(0);
  expect(api.writes()).toHaveLength(1);
  await noOverflow(page);
  await evidence(page, testInfo, 'api-key-created-value-cleared');
});

test('revoking an API key requires confirmation and resolves an unconfirmed result without replay', async ({ page, api }, testInfo) => {
  let key: ApplicationKey = { Id: '201', AppName: 'Home automation', CreatedAt: stamp, LastUsedAt: stamp, RevokedAt: null, CreatedBy: administrator.Id, IPAddress: '192.0.2.5', Status: 'active' };
  api.handlers.set('GET /admin/v1/api-keys', (route, request) => {
    const url = new URL(request.url());
    const items = url.searchParams.get('IncludeRevoked') === 'true' || key.Status === 'active' ? [key] : [];
    return json(route, { Items: items, TotalRecordCount: items.length, StartIndex: Number(url.searchParams.get('StartIndex') ?? 0), Limit: Number(url.searchParams.get('Limit') ?? 50) });
  });
  api.handlers.set('POST /admin/v1/api-keys/201/revoke', async (route) => {
    // The operation has been applied, but its response is unavailable. The UI
    // must query the list rather than repeat a potentially completed mutation.
    key = { ...key, Status: 'revoked', RevokedAt: stamp };
    await json(route, { Error: { Code: 'operation_unconfirmed', Message: 'The revocation response could not be confirmed.' } }, 503);
  });
  await page.goto('/admin/access/api-keys');
  await ready(page, destinations[6]);
  const trigger = page.getByRole('button', { name: 'Revoke key Home automation', exact: true });
  await trigger.click();
  const confirm = page.getByRole('dialog', { name: 'Revoke API key?', exact: true });
  await expect(confirm).toContainText('All copies of the key stop working.');
  await evidence(page, testInfo, 'api-key-revoke-confirmation');
  expect(api.writes()).toEqual([]);
  await confirm.getByRole('button', { name: 'Cancel', exact: true }).click();
  expect(api.writes()).toEqual([]);
  await trigger.click();
  await confirm.getByRole('button', { name: 'Revoke key', exact: true }).click();
  await expect(confirm).toContainText('It may already be revoked.');
  await expect(confirm.getByRole('button', { name: 'Revoke key', exact: true })).toHaveCount(0);
  expect(api.writes()).toEqual([{ method: 'POST', path: '/admin/v1/api-keys/201/revoke', body: {}, csrf }]);
  await confirm.getByRole('button', { name: 'Refresh API keys', exact: true }).click();
  await expect(confirm).not.toBeVisible();
  await expect(page.getByRole('main')).toContainText('Revoked');
  await expect(trigger).toHaveCount(0);
  await page.getByRole('button', { name: 'Refresh API keys', exact: true }).click();
  await expect(page.getByRole('main')).toContainText('Revoked');
  expect(api.writes()).toHaveLength(1);
  await noOverflow(page);
  await evidence(page, testInfo, 'api-key-revoked-after-confirmation');
});
