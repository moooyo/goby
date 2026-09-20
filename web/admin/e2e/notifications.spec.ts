import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Page, Request, Route } from '@playwright/test';
import type { NotificationSettings, NotificationSettingsInput } from '../src/notificationsApi';

const timestamp = '2026-09-20T00:00:00Z';
const csrf = 'synthetic-notification-csrf';
const administrator = { Id: 'notification-admin', Name: 'Notification administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const settingsPath = '/admin/v1/notifications';
const replacement = 'synthetic-receiver-credential';
async function json(route: Route, value: unknown, status = 200): Promise<void> { await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) }); }
class NotificationsAPI {
  settings: NotificationSettings = { Revision: '9007199254740993', Enabled: false, Endpoint: '', AllowedNetworks: [], HasReceiverCredential: false, SupportedEvents: ['CatalogInvalidated', 'UserDataInvalidated'], PendingCount: 0 };
  writes: { body: NotificationSettingsInput; csrf?: string }[] = []; unexpected: string[] = [];
  handlers = new Map<string, (route: Route, request: Request) => Promise<void>>();
  apply(input: NotificationSettingsInput): void { this.settings = { ...this.settings, Revision: (BigInt(this.settings.Revision) + 1n).toString(), Enabled: input.Enabled, Endpoint: input.Endpoint, AllowedNetworks: input.AllowedNetworks, HasReceiverCredential: input.ReceiverCredential === undefined ? this.settings.HasReceiverCredential : input.ReceiverCredential !== '' }; }
  async install(context: BrowserContext): Promise<void> {
    // Synthetic routes never forward a mutation or an unrecognized read.
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const method = request.method(); const pathname = new URL(request.url()).pathname;
      if (method !== 'GET' && pathname === settingsPath) this.writes.push({ body: request.postDataJSON() as NotificationSettingsInput, csrf: request.headers()['x-csrf-token'] });
      const handler = this.handlers.get(`${method} ${pathname}`); if (handler) return handler(route, request);
      if (method === 'GET' && pathname === '/admin/v1/bootstrap') return json(route, { Initialized: true });
      if (method === 'GET' && pathname === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrf });
      if (method === 'GET' && pathname === settingsPath) return json(route, this.settings);
      if (method === 'PUT' && pathname === settingsPath) {
        const input = request.postDataJSON() as NotificationSettingsInput;
        if (input.Revision !== this.settings.Revision) return json(route, { Error: { Code: 'notification_conflict', Message: 'Notification settings changed. Reload before saving.' } }, 409);
        this.apply(input); return json(route, this.settings);
      }
      this.unexpected.push(`${method} ${pathname}`); return json(route, { Error: { Code: 'unexpected_synthetic_request', Message: 'No synthetic response was configured.' } }, 501);
    });
  }
}
const test = base.extend<{ api: NotificationsAPI }>({ api: async ({ context, page }, use) => {
  const api = new NotificationsAPI(); const errors: string[] = []; page.on('pageerror', (error) => errors.push(error.message));
  await api.install(context); await use(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); expect(api.writes.every((request) => request.csrf === csrf)).toBe(true);
} });
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });
async function open(page: Page): Promise<void> { await page.goto('/admin/notifications'); await expect(page.getByRole('region', { name: 'Notification receiver status', exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Reload notifications', exact: true })).toBeEnabled(); }
async function credentialAction(page: Page, label: string): Promise<void> { await page.getByRole('combobox', { name: 'Receiver credential action', exact: true }).click(); await page.getByRole('option', { name: label, exact: true }).click(); }
async function save(page: Page, expected = 200): Promise<void> {
  const [response] = await Promise.all([page.waitForResponse((value) => value.request().method() === 'PUT' && new URL(value.url()).pathname === settingsPath), page.getByRole('button', { name: 'Save notifications', exact: true }).click()]);
  expect(response.status()).toBe(expected);
  if (expected === 200) await expect(page.getByText('Notification settings saved. Receiver credentials are never returned.', { exact: true })).toBeVisible();
  else await expect(page.getByRole('alert').filter({ hasText: 'Reload the saved configuration' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reload notifications', exact: true })).toBeEnabled();
}

test('receiver enrollment uses exact CAS and never refills the saved credential', async ({ page, api }) => {
  await open(page); await page.getByRole('textbox', { name: 'Receiver endpoint', exact: true }).fill('https://receiver.example/goby');
  await credentialAction(page, 'Replace credential'); const secret = page.getByLabel('New receiver credential', { exact: true });
  await expect(secret).toHaveAttribute('type', 'password'); await secret.fill(replacement);
  await page.getByRole('switch', { name: 'Enable notifications', exact: true }).check(); await save(page);
  expect(api.writes[0].body).toEqual({ Revision: '9007199254740993', Enabled: true, Endpoint: 'https://receiver.example/goby', AllowedNetworks: [], ReceiverCredential: replacement });
  await expect(secret).toHaveCount(0); await credentialAction(page, 'Replace credential'); await expect(secret).toHaveValue('');
  await expect(page.getByRole('region', { name: 'Notification receiver status', exact: true })).toContainText('Receiver credential configured');
});

test('keeping and clearing the receiver credential use distinct request shapes', async ({ page, api }) => {
  api.settings = { ...api.settings, Enabled: true, Endpoint: 'https://receiver.example/goby', HasReceiverCredential: true, PendingCount: 3 };
  await open(page); await page.getByRole('textbox', { name: 'Allowed private networks', exact: true }).fill('10.0.0.0/8\nfd00::/8'); await save(page);
  expect(api.writes[0].body).not.toHaveProperty('ReceiverCredential'); expect(api.writes[0].body.AllowedNetworks).toEqual(['10.0.0.0/8', 'fd00::/8']);
  await credentialAction(page, 'Clear credential'); await expect(page.getByRole('button', { name: 'Save notifications', exact: true })).toBeDisabled();
  await page.getByRole('switch', { name: 'Enable notifications', exact: true }).uncheck(); await save(page);
  expect(api.writes[1].body.ReceiverCredential).toBe(''); expect(api.settings.HasReceiverCredential).toBe(false);
  await expect(page.getByRole('region', { name: 'Notification receiver status', exact: true })).toContainText('No receiver credential configured');
});

test('invalid receiver URLs, missing credentials, and oversized allowlists prevent writes', async ({ page, api }) => {
  await open(page); await page.getByRole('switch', { name: 'Enable notifications', exact: true }).check();
  await expect(page.getByRole('button', { name: 'Save notifications', exact: true })).toBeDisabled();
  await credentialAction(page, 'Replace credential'); await page.getByLabel('New receiver credential', { exact: true }).fill('short');
  await page.getByRole('textbox', { name: 'Receiver endpoint', exact: true }).fill('https://receiver.example/goby'); await expect(page.getByRole('button', { name: 'Save notifications', exact: true })).toBeDisabled();
  await page.getByLabel('New receiver credential', { exact: true }).fill(replacement);
  for (const endpoint of ['http://receiver.example/goby', 'https://user:password@receiver.example/goby', 'https://receiver.example/goby?token=private', 'https://receiver.example/goby#fragment']) {
    await page.getByRole('textbox', { name: 'Receiver endpoint', exact: true }).fill(endpoint); await expect(page.getByRole('button', { name: 'Save notifications', exact: true })).toBeDisabled();
  }
  await page.getByRole('textbox', { name: 'Receiver endpoint', exact: true }).fill('https://receiver.example/goby');
  await page.getByRole('textbox', { name: 'Allowed private networks', exact: true }).fill(Array.from({ length: 33 }, (_, index) => `10.${index}.0.0/16`).join('\n'));
  await expect(page.getByRole('button', { name: 'Save notifications', exact: true })).toBeDisabled(); expect(api.writes).toEqual([]);
});

test('a stale notification revision preserves the draft and reloads after confirmation', async ({ page, api }) => {
  await open(page); const endpoint = page.getByRole('textbox', { name: 'Receiver endpoint', exact: true }); await endpoint.fill('https://receiver.example/new');
  api.settings = { ...api.settings, Revision: '9007199254740994', Endpoint: 'https://receiver.example/saved' }; await save(page, 409);
  await expect(endpoint).toHaveValue('https://receiver.example/new'); await expect(endpoint).toBeDisabled(); expect(api.writes).toHaveLength(1);
  page.once('dialog', (prompt) => { void prompt.accept(); }); await page.getByRole('button', { name: 'Reload notifications', exact: true }).click();
  await expect(endpoint).toHaveValue('https://receiver.example/saved'); await expect(endpoint).toBeEnabled();
});

test('an unconfirmed save requires readback without automatic write retries', async ({ page, api }) => {
  api.handlers.set(`PUT ${settingsPath}`, async (route, request) => { api.apply(request.postDataJSON() as NotificationSettingsInput); await route.abort('failed'); });
  await open(page); await page.getByRole('textbox', { name: 'Receiver endpoint', exact: true }).fill('https://receiver.example/goby');
  await page.getByRole('button', { name: 'Save notifications', exact: true }).click(); await expect(page.getByRole('alert').filter({ hasText: 'result could not be confirmed' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save notifications', exact: true })).toBeDisabled(); expect(api.writes).toHaveLength(1);
  page.once('dialog', (prompt) => { void prompt.accept(); }); await page.getByRole('button', { name: 'Reload notifications', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Receiver endpoint', exact: true })).toHaveValue('https://receiver.example/goby');
  await expect(page.getByRole('textbox', { name: 'Receiver endpoint', exact: true })).toBeEnabled(); expect(api.writes).toHaveLength(1);
});

test('a response containing a credential cannot be displayed as editable settings', async ({ page, api }) => {
  api.handlers.set(`GET ${settingsPath}`, async (route) => json(route, { ...api.settings, ReceiverCredential: replacement }));
  await page.goto('/admin/notifications'); await expect(page.getByRole('alert')).toContainText('notification settings response is incomplete');
  await expect(page.getByRole('region', { name: 'Notification receiver settings', exact: true })).toHaveCount(0); await expect(page.locator('body')).not.toContainText(replacement); expect(api.writes).toEqual([]);
});
