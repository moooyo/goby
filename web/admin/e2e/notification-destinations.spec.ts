import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Page, Request, Route } from '@playwright/test';
import type { NotificationDestination, NotificationDestinationInput, NotificationDestinations } from '../src/notificationDestinationsApi';
import { DashboardAPI } from './dashboard-fixture';

const root = '/admin/v1/notification-destinations';
const csrf = 'synthetic-notification-target-csrf';
const administrator = { Id: 'notification-target-admin', Name: 'Target administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: '2026-11-06T00:00:00Z' };
const emptySMTP = { Host: '', Port: 587, Security: 'starttls' as const, Username: '', From: '', To: [] as string[] };
const initialTarget = (): NotificationDestination => ({
  Id: '0123456789abcdef0123456789abcdef', Revision: '9007199254740993', Name: 'Server notices', Transport: 'webhook', Enabled: true,
  EventIds: ['TaskFinished'], AllowedNetworks: [], Endpoint: 'https://receiver.example/events', SMTP: { ...emptySMTP },
  HasSecret: true, LastOutcome: 'delivered', LastDeliveredAt: '2026-11-06T01:00:00Z', PendingCount: 0, DroppedCount: 0,
});
async function json(route: Route, value: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) });
}
class DestinationFixture {
  private readonly shell = new DashboardAPI();
  data: NotificationDestinations = { Items: [initialTarget()], SupportedEvents: [
    { Id: 'UserChanges', Name: 'User changes', Actions: ['user.created', 'user.updated'] },
    { Id: 'TaskFinished', Name: 'Tasks finished', Actions: ['task.finished'] },
  ], Limit: 16 };
  writes: { method: string; path: string; body: Record<string, unknown>; csrf?: string }[] = [];
  unexpected: string[] = [];
  handlers = new Map<string, (route: Route, request: Request) => Promise<void>>();
  private stalled = new Set<() => void>();
  private closing = false;
  async hold(route: Route, response?: unknown): Promise<void> {
    if (!this.closing) await new Promise<void>((resolve) => {
      const release = () => { this.stalled.delete(release); resolve(); };
      this.stalled.add(release);
    });
    if (response !== undefined) await json(route, response).catch(() => undefined);
    else await route.abort('failed').catch(() => undefined);
  }
  releaseHeldRequests(): void {
    this.closing = true;
    for (const release of [...this.stalled]) release();
  }
  update(input: NotificationDestinationInput, id: string): NotificationDestination {
    const saved = this.data.Items.find((value) => value.Id === id);
    const { Secret: secret, ...fields } = input;
    const value: NotificationDestination = { ...fields, Id: id, Revision: saved ? (BigInt(saved.Revision) + 1n).toString() : '1',
      HasSecret: secret === undefined ? Boolean(saved?.HasSecret) : secret !== '', LastOutcome: '', PendingCount: 0, DroppedCount: 0 };
    this.data.Items = saved ? this.data.Items.map((item) => item.Id === id ? value : item) : [...this.data.Items, value];
    return value;
  }
  async install(context: BrowserContext, language: 'en' | 'zh') {
    await context.addInitScript((value) => { localStorage.setItem('goby.admin.language', value); }, language);
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const method = request.method(), path = new URL(request.url()).pathname;
      if (method !== 'GET') this.writes.push({ method, path, body: request.postDataJSON() as Record<string, unknown>, csrf: request.headers()['x-csrf-token'] });
      const handler = this.handlers.get(`${method} ${path}`);
      if (handler) return handler(route, request);
      if (method === 'GET' && path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
      if (method === 'GET' && path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrf });
      if (method === 'GET' && path === '/admin/v1/overview') return json(route, {
        Server: { Id: 'notification-fixture-server', Name: 'Notification test server', Version: '0.1.0-fixture' },
        Database: { Status: 'ok' }, Counts: { Users: 1, Libraries: 0, Items: 0, ActiveSessions: 1 },
        Runtime: { GoVersion: 'go1.27.1' }, Features: { LibraryManagement: true, Playback: true, Transcoding: true, ApplicationKeys: true },
      });
      if (method === 'GET' && path === '/admin/v1/system/status') return json(route, this.shell.status);
      if (method === 'GET' && path === '/admin/v1/settings') return json(route, this.shell.settings);
      if (method === 'GET' && path === '/admin/v1/storage/library-directories') return json(route, { Items: [], TotalRecordCount: 0 });
      if (method === 'GET' && path === '/admin/v1/backups/policy') return json(route, {
        Revision: '1', Enabled: true, Schedule: { Kind: 'daily', DayOfWeek: null, TimeOfDay: '03:00', Timezone: 'UTC' },
        KeepCount: 7, IncludeImages: true, HasPassphrase: true, NextRunAt: '2026-11-08T03:00:00Z', Available: true, UnavailableReason: '',
        TaskId: '9'.repeat(32), LastRunAt: null, LastRunState: '', LastBackupId: '',
      });
      if (method === 'GET' && path === root) return json(route, this.data);
      if (method === 'PUT' && path.startsWith(root + '/')) {
        const id = path.slice(root.length + 1), input = request.postDataJSON() as NotificationDestinationInput;
        const saved = this.data.Items.find((item) => item.Id === id);
        if (!saved || saved.Revision !== input.Revision) return json(route, { Error: { Code: 'notification_conflict', Message: 'Reload notification targets before changing them.' } }, 409);
        return json(route, this.update(input, id));
      }
      if (method === 'POST' && path === root) return json(route, this.update(request.postDataJSON() as NotificationDestinationInput, 'fedcba9876543210fedcba9876543210'));
      if (method === 'POST' && path.endsWith('/test')) {
        const id = path.slice(root.length + 1, -5);
        this.data.Items = this.data.Items.map((item) => item.Id === id ? { ...item, LastOutcome: 'delivery_unconfirmed', PendingCount: 0 } : item);
        return json(route, { Id: 'synthetic-queued-test', State: 'pending' }, 202);
      }
      this.unexpected.push(`${method} ${path}`);
      return json(route, { Error: { Code: 'unexpected_synthetic_request', Message: 'No synthetic response was configured.' } }, 501);
    });
  }
}
const test = base.extend<{ fixture: DestinationFixture; language: 'en' | 'zh' }>({ language: ['en', { option: true }], fixture: async ({ context, page, language }, use) => {
  const fixture = new DestinationFixture(), errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await fixture.install(context, language);
  try { await use(fixture); } finally { fixture.releaseHeldRequests(); }
  expect(fixture.unexpected).toEqual([]); expect(errors).toEqual([]);
  expect(fixture.writes.every((write) => write.csrf === csrf)).toBe(true);
} });
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });

test('notification target editing preserves exact revisions and never returns saved secrets', async ({ page, fixture }) => {
  await page.goto('/admin/notifications');
  await page.getByRole('button', { name: 'Edit target', exact: true }).click();
  const sheet = page.getByRole('dialog', { name: 'Edit notification target', exact: true });
  await expect(sheet.getByRole('heading', { level: 2, name: 'Server notices', exact: true })).toBeVisible();
  await expect(sheet.getByRole('textbox', { name: 'Target name', exact: true })).toBeFocused();
  await sheet.getByRole('textbox', { name: 'Target name', exact: true }).fill('Updated server notices');
  await expect(sheet.getByRole('heading', { level: 2, name: 'Updated server notices', exact: true })).toBeVisible();
  await expect(sheet).toHaveAccessibleName('Edit notification target');
  await sheet.getByRole('button', { name: 'Save target', exact: true }).click();
  await expect(sheet.getByText('Notification target saved.', { exact: true })).toBeVisible();
  expect(fixture.writes).toHaveLength(1);
  expect(fixture.writes[0].body.Revision).toBe('9007199254740993');
  expect(fixture.writes[0].body).not.toHaveProperty('Secret');
  await sheet.getByRole('combobox', { name: 'Saved secret action', exact: true }).selectOption('replace');
  const secret = sheet.getByLabel('Webhook signing secret', { exact: true });
  await expect(secret).toHaveAttribute('type', 'password'); await expect(secret).toHaveValue('');
  await secret.fill('synthetic-target-signing-secret');
  await sheet.getByRole('button', { name: 'Save target', exact: true }).click();
  await expect(secret).toHaveCount(0);
  await sheet.getByRole('combobox', { name: 'Saved secret action', exact: true }).selectOption('replace');
  await expect(sheet.getByLabel('Webhook signing secret', { exact: true })).toHaveValue('');
});

test('a new notification target heading follows its draft while keeping the operation name', async ({ page, fixture }, testInfo) => {
  fixture.data.Items = [];
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/notifications');
  await page.getByRole('button', { name: 'Add notification target', exact: true }).first().click();
  const sheet = page.getByRole('dialog', { name: 'Add notification target', exact: true });
  const name = sheet.getByRole('textbox', { name: 'Target name', exact: true });
  await expect(name).toBeFocused();
  await expect(sheet.getByRole('heading', { level: 2, name: 'New notification target', exact: true })).toBeVisible();
  await expect(sheet.locator('#notification-destination-title')).toHaveText('Add notification target');
  await name.fill('Settings');
  await expect(sheet.getByRole('heading', { level: 2, name: 'Settings', exact: true })).toBeVisible();
  await expect(sheet.locator('strong')).toHaveAttribute('translate', 'no');
  await expect(sheet).toHaveAccessibleName('Add notification target');
  await page.screenshot({ path: testInfo.outputPath('notification-target-draft-title-1440.png'), fullPage: true });
  await name.fill('   ');
  await expect(name).toHaveValue('   ');
  await expect(sheet.getByRole('heading', { level: 2, name: 'New notification target', exact: true })).toBeVisible();
  await expect(sheet.getByRole('button', { name: 'Save target', exact: true })).toBeDisabled();
  expect(fixture.writes).toHaveLength(0);
});

test.describe('notification target names in Chinese', () => {
  test.use({ language: 'zh', viewport: { width: 390, height: 844 } });
  test('the editor preserves raw target names and localizes an empty draft title', async ({ page, fixture }, testInfo) => {
    fixture.data.Items[0].Name = 'Settings';
    await page.goto('/admin/notifications');
    await page.getByRole('button', { name: '\u7f16\u8f91\u76ee\u6807', exact: true }).click();
    const operation = '\u7f16\u8f91\u901a\u77e5\u76ee\u6807';
    const sheet = page.getByRole('dialog', { name: operation, exact: true });
    const heading = sheet.getByRole('heading', { level: 2, name: 'Settings', exact: true });
    const name = sheet.getByRole('textbox', { name: '\u76ee\u6807\u540d\u79f0', exact: true });
    await expect(heading).toBeVisible();
    await expect(heading.locator('strong')).toHaveText('Settings');
    await expect(heading.locator('strong')).toHaveAttribute('translate', 'no');
    await expect(name).toBeFocused();
    await expect(sheet.locator('#notification-destination-title')).toHaveText(operation);
    await name.fill('Notifications');
    await expect(sheet.getByRole('heading', { level: 2, name: 'Notifications', exact: true })).toBeVisible();
    await expect(sheet).toHaveAccessibleName(operation);
    await page.screenshot({ path: testInfo.outputPath('notification-target-raw-title-zh-390.png'), fullPage: true });
    await name.fill('');
    await expect(sheet.getByRole('heading', { level: 2, name: '\u672a\u547d\u540d\u901a\u77e5\u76ee\u6807', exact: true })).toBeVisible();
    await expect(sheet).toHaveAccessibleName(operation);
    expect(fixture.writes).toHaveLength(0);
  });
});

test('changing a target address cannot silently reuse the saved signing secret', async ({ page, fixture }) => {
  await page.goto('/admin/notifications'); await page.getByRole('button', { name: 'Edit target', exact: true }).click();
  const sheet = page.getByRole('dialog', { name: 'Edit notification target', exact: true });
  await sheet.getByRole('textbox', { name: 'Webhook endpoint', exact: true }).fill('https://other.example/events');
  await expect(sheet.getByRole('button', { name: 'Save target', exact: true })).toBeDisabled();
  await sheet.getByRole('combobox', { name: 'Saved secret action', exact: true }).selectOption('replace');
  await sheet.getByLabel('Webhook signing secret', { exact: true }).fill('synthetic-replacement-secret');
  await expect(sheet.getByRole('button', { name: 'Save target', exact: true })).toBeEnabled();
  expect(fixture.writes).toHaveLength(0);
});

test('an unconfirmed target save requires readback without an automatic write retry', async ({ page, fixture }) => {
  const id = fixture.data.Items[0].Id;
  fixture.handlers.set(`PUT ${root}/${id}`, async (route, request) => { fixture.update(request.postDataJSON() as NotificationDestinationInput, id); await route.abort('failed'); });
  await page.goto('/admin/notifications'); await page.getByRole('button', { name: 'Edit target', exact: true }).click();
  const sheet = page.getByRole('dialog', { name: 'Edit notification target', exact: true });
  await sheet.getByRole('textbox', { name: 'Target name', exact: true }).fill('Saved before interruption');
  await sheet.getByRole('button', { name: 'Save target', exact: true }).click();
  await expect(sheet.getByText('The result could not be confirmed. Reload targets before trying again.', { exact: true })).toBeVisible();
  await expect(sheet.getByRole('textbox', { name: 'Target name', exact: true })).toBeDisabled();
  expect(fixture.writes).toHaveLength(1);
  page.once('dialog', (dialog) => { void dialog.accept(); });
  await sheet.getByRole('button', { name: 'Reload saved targets', exact: true }).click();
  await expect(sheet).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Saved before interruption', exact: true })).toBeVisible();
  expect(fixture.writes).toHaveLength(1);
});

test('a queued test is not presented as confirmed delivery and capacity loss stays visible', async ({ page, fixture }) => {
  fixture.data.Items[0].DroppedCount = 3;
  await page.goto('/admin/notifications');
  await expect(page.getByText('3 events were not queued because the delivery queue was full.', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Send test notification', exact: true }).click();
  await expect(page.getByText('Test notification queued. Delivery status updates after the receiver responds.', { exact: true })).toBeVisible();
  await expect(page.getByText('Delivery could not be confirmed', { exact: true })).toBeVisible();
  expect(fixture.writes).toHaveLength(1);
  expect(fixture.writes[0].body).toEqual({ Revision: '9007199254740993' });
});

test('SMTP validation rejects duplicate and non-ASCII recipients and keeps TLS mandatory', async ({ page, fixture }) => {
  fixture.data.Items = [];
  await page.goto('/admin/notifications'); await page.getByRole('button', { name: 'Add notification target', exact: true }).first().click();
  const sheet = page.getByRole('dialog', { name: 'Add notification target', exact: true });
  await sheet.getByRole('textbox', { name: 'Target name', exact: true }).fill('Mail alerts');
  await sheet.getByRole('combobox', { name: 'Delivery channel', exact: true }).selectOption('smtp');
  await sheet.getByRole('textbox', { name: 'Mail server', exact: true }).fill('smtp.example.com');
  await sheet.getByRole('textbox', { name: 'Sender email address', exact: true }).fill('goby@example.com');
  await sheet.getByRole('checkbox', { name: 'Tasks finished', exact: true }).check();
  const recipients = sheet.getByRole('textbox', { name: 'Recipient email addresses', exact: true });
  await recipients.fill('Admin@example.com\nadmin@example.com'); await expect(sheet.getByRole('button', { name: 'Save target', exact: true })).toBeDisabled();
  await recipients.fill('管理员@example.com'); await expect(sheet.getByRole('button', { name: 'Save target', exact: true })).toBeDisabled();
  await recipients.fill('admin@example.com');
  await expect(sheet.getByRole('button', { name: 'Save target', exact: true })).toBeEnabled();
  await expect(sheet.getByRole('combobox', { name: 'Connection security', exact: true }).locator('option')).toHaveCount(2);
  await sheet.getByRole('button', { name: 'Save target', exact: true }).click();
  const savedSheet = page.getByRole('dialog', { name: 'Edit notification target', exact: true });
  await expect(savedSheet.getByText('Notification target saved.', { exact: true })).toBeVisible();
  await expect(savedSheet.getByRole('textbox', { name: 'Target name', exact: true })).toHaveValue('Mail alerts');
  await expect(savedSheet.getByRole('combobox', { name: 'Connection security', exact: true })).toHaveValue('starttls');
  expect(fixture.writes).toHaveLength(1);
  expect(fixture.writes[0].body.Revision).toBe('0');
  expect(fixture.writes[0].body.SMTP).toEqual({ Host: 'smtp.example.com', Port: 587, Security: 'starttls', Username: '', From: 'goby@example.com', To: ['admin@example.com'] });
});

test('notification target decoding rejects a response that exposes a secret', async ({ page, fixture }) => {
  fixture.handlers.set(`GET ${root}`, async (route) => json(route, { ...fixture.data, Items: [{ ...initialTarget(), Secret: 'private-sentinel' }] }));
  await page.goto('/admin/notifications');
  await expect(page.getByRole('alert')).toContainText('notification targets response is incomplete');
  await expect(page.locator('body')).not.toContainText('private-sentinel');
  expect(fixture.writes).toHaveLength(0);
});

const timeoutMessage = 'The notification request timed out. Its result could not be confirmed.';
async function installNotificationClock(page: Page) {
  await page.clock.install({ time: new Date('2026-11-06T00:00:00Z') });
}
async function pauseReadyNotificationClock(page: Page) {
  const addTarget = page.getByRole('button', { name: 'Add notification target', exact: true }).first();
  await expect(addTarget).toBeVisible();
  await expect(addTarget).toBeEnabled();
  // Let the lazy route finish using its real scheduling before controlling
  // notification deadlines. Read the browser clock rather than assuming how
  // much virtual time elapsed during navigation and initial API responses.
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1000));
}
async function fillNewTarget(page: Page, name: string) {
  await page.getByRole('button', { name: 'Add notification target', exact: true }).first().click();
  const sheet = page.getByRole('dialog', { name: 'Add notification target', exact: true });
  await sheet.getByRole('textbox', { name: 'Target name', exact: true }).fill(name);
  await sheet.getByRole('textbox', { name: 'Webhook endpoint', exact: true }).fill('https://new-receiver.example/events');
  await sheet.getByRole('checkbox', { name: 'Tasks finished', exact: true }).check();
  return sheet;
}
async function openUnconfirmedCreation(page: Page, fixture: DestinationFixture) {
  await installNotificationClock(page);
  fixture.data.Items = [];
  fixture.handlers.set(`POST ${root}`, async (route, request) => {
    fixture.update(request.postDataJSON() as NotificationDestinationInput, 'fedcba9876543210fedcba9876543210');
    await route.abort('failed');
  });
  await page.goto('/admin/notifications');
  await pauseReadyNotificationClock(page);
  const sheet = await fillNewTarget(page, 'Created before connection loss');
  await sheet.getByRole('button', { name: 'Save target', exact: true }).click();
  await expect(sheet.getByText('The result could not be confirmed. Reload targets before trying again.', { exact: true })).toBeVisible();
  return sheet;
}

test('a failed creation readback retains the draft and ordinary polling cannot reopen writes', async ({ page, fixture }) => {
  const sheet = await openUnconfirmedCreation(page, fixture);
  let firstReadback = true;
  fixture.handlers.set(`GET ${root}`, async (route) => {
    if (firstReadback) { firstReadback = false; return json(route, { Error: { Code: 'notification_unavailable', Message: 'Synthetic readback unavailable.' } }, 503); }
    return json(route, fixture.data);
  });
  page.once('dialog', (dialog) => { void dialog.accept(); });
  await sheet.getByRole('button', { name: 'Reload saved targets', exact: true }).click();
  await expect(sheet.getByText('Synthetic readback unavailable.', { exact: true })).toBeVisible();
  await expect(sheet.getByRole('textbox', { name: 'Target name', exact: true })).toHaveValue('Created before connection loss');
  await expect(sheet.getByRole('textbox', { name: 'Target name', exact: true })).toBeDisabled();
  await expect(sheet.getByRole('button', { name: 'Reload saved targets', exact: true })).toBeEnabled();
  // The automatic refresh can read the committed create, but it does not
  // release a review gate owned by the failed explicit readback.
  await expect(page.locator('.nd-target h3')).toHaveText('Created before connection loss');
  await expect(page.locator('.nd-panel-heading button').first()).toBeDisabled();
  expect(fixture.writes).toHaveLength(1);
  page.once('dialog', (dialog) => { void dialog.accept(); });
  await sheet.getByRole('button', { name: 'Reload saved targets', exact: true }).click();
  await expect(sheet).toHaveCount(0);
  await expect(page.locator('.nd-panel-heading button').first()).toBeEnabled();
  expect(fixture.writes).toHaveLength(1);
});

test('a stalled creation readback times out while keeping the editor and write gate', async ({ page, fixture }) => {
  const sheet = await openUnconfirmedCreation(page, fixture);
  fixture.handlers.set(`GET ${root}`, async (route) => fixture.hold(route));
  const started = page.waitForRequest((request) => request.method() === 'GET' && new URL(request.url()).pathname === root);
  page.once('dialog', (dialog) => { void dialog.accept(); });
  await sheet.getByRole('button', { name: 'Reload saved targets', exact: true }).click();
  await started;
  await expect(sheet.getByRole('button', { name: 'Reloading saved targets...', exact: true })).toBeDisabled();
  await page.clock.fastForward(15_001);
  await expect(sheet.getByText('Notification targets did not respond. Refresh to try again.', { exact: true })).toBeVisible();
  await expect(sheet.getByRole('button', { name: 'Reload saved targets', exact: true })).toBeEnabled();
  await expect(sheet.getByRole('textbox', { name: 'Target name', exact: true })).toHaveValue('Created before connection loss');
  await expect(sheet.getByRole('textbox', { name: 'Target name', exact: true })).toBeDisabled();
  await expect(page.locator('.nd-panel-heading button').first()).toBeDisabled();
  expect(fixture.writes).toHaveLength(1);
});

test('a late background response cannot replace a successful explicit creation readback', async ({ page, fixture }) => {
  const sheet = await openUnconfirmedCreation(page, fixture);
  let reads = 0;
  let markBackgroundStarted: (() => void) | undefined;
  const backgroundStarted = new Promise<void>((resolve) => { markBackgroundStarted = resolve; });
  let finishLateRead: (() => void) | undefined;
  const lateReadFinished = new Promise<void>((resolve) => { finishLateRead = resolve; });
  fixture.handlers.set(`GET ${root}`, async (route) => {
    if (reads++ === 0) {
      markBackgroundStarted?.();
      try { await fixture.hold(route, { ...fixture.data, Items: [] }); }
      finally { finishLateRead?.(); }
      return;
    }
    return json(route, fixture.data);
  });
  await page.clock.fastForward(5_001); await backgroundStarted;
  page.once('dialog', (dialog) => { void dialog.accept(); });
  await sheet.getByRole('button', { name: 'Reload saved targets', exact: true }).click();
  await expect(sheet).toHaveCount(0);
  await expect(page.locator('.nd-target h3')).toHaveText('Created before connection loss');
  fixture.releaseHeldRequests(); await lateReadFinished;
  await expect(page.locator('.nd-target h3')).toHaveText('Created before connection loss');
  await expect(page.locator('.nd-panel-heading button').first()).toBeEnabled();
  expect(fixture.writes).toHaveLength(1);
});

for (const operation of ['create', 'update', 'delete', 'test'] as const) {
  test(`a stalled notification ${operation} reaches review after its deadline without replay`, async ({ page, fixture }) => {
    await installNotificationClock(page);
    const target = fixture.data.Items[0];
    const method = operation === 'delete' ? 'DELETE' : operation === 'update' ? 'PUT' : 'POST';
    const path = operation === 'create' ? root : `${root}/${target.Id}${operation === 'test' ? '/test' : ''}`;
    if (operation === 'create') fixture.data.Items = [];
    fixture.handlers.set(`${method} ${path}`, async (route, request) => {
      if (operation === 'create' || operation === 'update') fixture.update(request.postDataJSON() as NotificationDestinationInput, operation === 'create' ? 'fedcba9876543210fedcba9876543210' : target.Id);
      if (operation === 'delete') fixture.data.Items = [];
      await fixture.hold(route);
    });
    await page.goto('/admin/notifications');
    await pauseReadyNotificationClock(page);
    const started = page.waitForRequest((request) => request.method() === method && new URL(request.url()).pathname === path);
    if (operation === 'create') {
      const sheet = await fillNewTarget(page, 'Possibly committed creation');
      await sheet.getByRole('button', { name: 'Save target', exact: true }).click();
    } else if (operation === 'update') {
      await page.getByRole('button', { name: 'Edit target', exact: true }).click();
      const sheet = page.getByRole('dialog', { name: 'Edit notification target', exact: true });
      await sheet.getByRole('textbox', { name: 'Target name', exact: true }).fill('Possibly committed update');
      await sheet.getByRole('button', { name: 'Save target', exact: true }).click();
    } else if (operation === 'delete') {
      await page.getByRole('button', { name: 'Delete notification target ' + target.Name, exact: true }).click();
      await page.getByRole('dialog', { name: 'Delete notification target?', exact: true }).getByRole('button', { name: 'Delete target', exact: true }).click();
    } else await page.getByRole('button', { name: 'Send test notification', exact: true }).click();
    await started;
    await page.clock.fastForward(20_001);
    await expect(page.getByText(timeoutMessage, { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Reload saved targets', exact: true })).toBeEnabled();
    await expect(page.locator('.nd-panel-heading button').first()).toBeDisabled();
    expect(fixture.writes).toHaveLength(1);
    await page.clock.fastForward(60_000);
    expect(fixture.writes).toHaveLength(1);
    fixture.releaseHeldRequests();
    await expect(page.getByRole('button', { name: 'Reload saved targets', exact: true })).toBeEnabled();
  });
}

test('the client receiver save deadline and a failed reload keep its original draft protected', async ({ page, fixture }) => {
  await installNotificationClock(page);
  const settings = { Revision: '9007199254740993', Enabled: false, Endpoint: 'https://receiver.example/old', AllowedNetworks: [], HasReceiverCredential: false, SupportedEvents: ['CatalogInvalidated', 'UserDataInvalidated'], PendingCount: 0 };
  let failReadback = false;
  fixture.handlers.set('GET /admin/v1/notifications', async (route) => failReadback
    ? json(route, { Error: { Code: 'notification_unavailable', Message: 'Synthetic receiver readback unavailable.' } }, 503)
    : json(route, settings));
  fixture.handlers.set('PUT /admin/v1/notifications', async (route, request) => {
    settings.Revision = (BigInt(settings.Revision) + 1n).toString();
    settings.Endpoint = (request.postDataJSON() as { Endpoint: string }).Endpoint;
    await fixture.hold(route);
  });
  await page.goto('/admin/notifications');
  await pauseReadyNotificationClock(page);
  await page.locator('.nd-receiver-details > summary').click();
  await page.getByRole('button', { name: 'Configure receiver', exact: true }).click();
  const sheet = page.getByRole('dialog', { name: 'HTTPS receiver', exact: true });
  await sheet.getByRole('textbox', { name: 'Receiver endpoint', exact: true }).fill('https://receiver.example/unconfirmed');
  const started = page.waitForRequest((request) => request.method() === 'PUT' && new URL(request.url()).pathname === '/admin/v1/notifications');
  await sheet.getByRole('button', { name: 'Save notifications', exact: true }).click(); await started;
  await page.clock.fastForward(20_001);
  await expect(sheet.getByText(timeoutMessage, { exact: true })).toBeVisible();
  failReadback = true;
  page.once('dialog', (dialog) => { void dialog.accept(); });
  await sheet.getByRole('button', { name: 'Reload notifications', exact: true }).click();
  await expect(sheet.getByText('Synthetic receiver readback unavailable.', { exact: true })).toBeVisible();
  await expect(sheet.getByRole('textbox', { name: 'Receiver endpoint', exact: true })).toHaveValue('https://receiver.example/unconfirmed');
  await expect(sheet.getByRole('textbox', { name: 'Receiver endpoint', exact: true })).toBeDisabled();
  await expect(sheet.getByRole('button', { name: 'Reload notifications', exact: true })).toBeEnabled();
  expect(fixture.writes).toHaveLength(1);
  failReadback = false;
  page.once('dialog', (dialog) => { void dialog.accept(); });
  await sheet.getByRole('button', { name: 'Reload notifications', exact: true }).click();
  await expect(sheet.getByRole('textbox', { name: 'Receiver endpoint', exact: true })).toBeEnabled();
  await expect(sheet.getByRole('textbox', { name: 'Receiver endpoint', exact: true })).toHaveValue(settings.Endpoint);
  expect(fixture.writes).toHaveLength(1);
});
