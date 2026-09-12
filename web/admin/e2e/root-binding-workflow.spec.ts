import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Page, Request, Route } from '@playwright/test';
import type { Library } from '../src/api';
import type { RegisteredRoot, RootBinding, StorageIdentity, StorageTopology } from '../src/rootBindingsApi';

const timestamp = '2026-09-12T08:00:00Z';
const csrfToken = 'root-binding-mock-csrf-token';
const fingerprint = 'a'.repeat(64);
const root: RegisteredRoot = { Id: 'registered-root-one', LibraryId: 'library-one', Path: '/media/movies', AllowedPath: '/media', RelativePath: 'movies', Revision: '9007199254740993' };
const secondRoot: RegisteredRoot = { ...root, Id: 'registered-root-two', Path: '/media/series', RelativePath: 'series', Revision: '7' };
const library: Library = { Id: root.LibraryId, Name: 'Storage review library', CollectionType: 'movies', Paths: [root.Path, secondRoot.Path], CreatedAt: timestamp, LastScanAt: null };

function identity(marker = 'c'): StorageIdentity {
  return { Profile: 'linux-fsuuid-filehandle-v1', FilesystemUUID: marker.repeat(32), Digest: marker.repeat(64) };
}

function topology(marker = 'c'): StorageTopology {
  return { Anchor: identity(marker), RegisteredRoot: identity(marker), Boundaries: [] };
}

function unbound(registered = root): RootBinding {
  return { ...registered, Status: 'unbound', ObservedFingerprint: fingerprint, Observed: topology() };
}

function mismatch(registered = root): RootBinding {
  return { ...unbound(registered), Status: 'mismatch', Approved: topology('d'), ApprovedFingerprint: 'b'.repeat(64), BoundAt: timestamp, BoundBy: 'previous-administrator' };
}

function accepted(value: RootBinding): RootBinding {
  return { ...value, Status: 'verified', Revision: (BigInt(value.Revision) + 1n).toString(), Approved: value.Observed, ApprovedFingerprint: value.ObservedFingerprint, BoundAt: timestamp, BoundBy: 'current-administrator' };
}

function bindingPath(id = root.Id): string {
  return `/admin/v1/libraries/${library.Id}/roots/${encodeURIComponent(id)}/binding`;
}

async function json(route: Route, value: unknown, status = 200): Promise<void> {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) });
}

async function failure(route: Route, code: string, status = 409): Promise<void> {
  await json(route, { Error: { Code: code, Message: 'The storage binding request was rejected.' }, RequestId: 'mock-binding-request' }, status);
}

interface CapturedRequest {
  method: string;
  path: string;
  headers: Record<string, string>;
  body: string | null;
}

class RootBindingMock {
  roots: RegisteredRoot[] = [root, secondRoot];
  bindings = new Map<string, unknown>([[root.Id, unbound()], [secondRoot.Id, unbound(secondRoot)]]);
  requests: CapturedRequest[] = [];
  unexpected: string[] = [];
  handlers = new Map<string, (route: Route, request: Request) => Promise<void>>();

  puts(): CapturedRequest[] { return this.requests.filter((request) => request.method === 'PUT'); }

  async install(context: BrowserContext): Promise<void> {
    // Every native API request is mocked, including unexpected mutations.
    // This suite exercises the shipped UI, not live storage binding or deletion.
    await context.route('**/admin/v1/**', async (route, request) => {
      const url = new URL(request.url());
      const captured = { method: request.method(), path: url.pathname, headers: request.headers(), body: request.postData() };
      this.requests.push(captured);
      const handler = this.handlers.get(`${captured.method} ${captured.path}`);
      if (handler && !url.search) return handler(route, request);
      if (captured.method === 'GET' && !url.search) {
        if (captured.path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
        if (captured.path === '/admin/v1/session') return json(route, {
          User: { Id: 'current-administrator', Name: 'Browser administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp }, CSRFToken: csrfToken,
        });
        if (captured.path === '/admin/v1/libraries') return json(route, { Items: [{ ...library, Paths: this.roots.map((item) => item.Path) }], TotalRecordCount: 1 });
        if (captured.path === '/admin/v1/storage/roots') return json(route, { Configured: true, Items: [{ Path: '/media', Available: true }] });
        if (captured.path === `/admin/v1/libraries/${library.Id}/roots`) return json(route, { Items: this.roots, TotalRecordCount: this.roots.length });
        const selected = this.roots.find((item) => bindingPath(item.Id) === captured.path);
        if (selected) return json(route, { Binding: this.bindings.get(selected.Id) });
      }
      this.unexpected.push(`${captured.method} ${captured.path}${url.search}`);
      return failure(route, 'unexpected_mock_request', 501);
    });
  }
}

const test = base.extend<{ api: RootBindingMock }>({
  api: async ({ context }, use) => {
    const api = new RootBindingMock();
    await api.install(context);
    await use(api);
    expect(api.unexpected, 'Every native API request must be explicitly mocked.').toEqual([]);
  },
});

test.use({ serviceWorkers: 'block' });

const dialog = (page: Page) => page.getByRole('dialog', { name: 'Storage bindings', exact: true });
const consent = (page: Page) => dialog(page).getByRole('checkbox', { name: /^I understand that a later complete scan/ });
const submit = (page: Page) => dialog(page).getByRole('button', { name: /^(Bind storage|Accept replacement)$/ });

async function openBindings(page: Page): Promise<void> {
  await page.goto('/admin/libraries');
  await page.getByRole('button', { name: `Storage bindings for ${library.Name}`, exact: true }).click();
  await expect(dialog(page)).toBeVisible();
}

test('an unbound root requires consent and sends one exact approval while controls are busy', async ({ page, api }) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  api.handlers.set(`PUT ${bindingPath()}`, async (route) => {
    await gate;
    await json(route, { Binding: accepted(unbound()) });
  });
  const pageErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await openBindings(page);
  await expect(dialog(page).getByText('Unbound', { exact: true })).toBeVisible();
  await expect(dialog(page).getByText(root.Revision, { exact: true })).toBeVisible();
  await expect(consent(page)).not.toBeChecked();
  await expect(submit(page)).toBeDisabled();
  expect(api.puts()).toHaveLength(0);
  await consent(page).check();
  await submit(page).click();
  try {
    await expect(dialog(page).getByRole('button', { name: 'Saving...', exact: true })).toBeDisabled();
    await expect(dialog(page).getByRole('button', { name: 'Close', exact: true })).toBeDisabled();
    await expect(dialog(page).getByRole('combobox', { name: /^Registered root/ })).toBeDisabled();
    await expect(dialog(page).getByRole('button', { name: 'Refresh observation', exact: true })).toBeDisabled();
    await expect.poll(() => api.puts().length).toBe(1);
    expect(JSON.parse(api.puts()[0].body!)).toEqual({ Revision: root.Revision, ObservedFingerprint: fingerprint, AcknowledgeMissingRemoval: true });
    expect(api.puts()[0].headers['x-csrf-token']).toBe(csrfToken);
    expect(new TextEncoder().encode(api.puts()[0].body!).length).toBeLessThanOrEqual(4096);
  } finally { release(); }
  await expect(dialog(page).getByText('Storage binding saved.', { exact: true })).toBeVisible();
  await expect(dialog(page).getByText('Verified', { exact: true })).toBeVisible();
  await expect(dialog(page).getByText('9007199254740994', { exact: true })).toBeVisible();
  await expect(dialog(page).getByText('current-administrator', { exact: true })).toBeVisible();
  await expect(dialog(page).locator(`time[datetime="${timestamp}"]`)).toBeVisible();
  await expect(submit(page)).toBeDisabled();
  await expect(consent(page)).toHaveCount(0);
  expect(api.requests.filter((request) => request.method !== 'GET')).toHaveLength(1);
  expect(pageErrors).toEqual([]);
});

test('replacement review shows all boundary changes and fits a narrow viewport with long paths and IDs', async ({ page, api }, testInfo) => {
  const longRoot = { ...root, Id: `root-${'x'.repeat(220)}`, Path: `/media/${'long-directory-'.repeat(24)}movies`, RelativePath: `${'long-directory-'.repeat(24)}movies` };
  const current = mismatch(longRoot);
  current.Approved!.Boundaries = [
    { RelativePath: 'archive/removed', Identity: identity('d') },
    { RelativePath: 'archive/replaced', Identity: identity('e') },
    { RelativePath: 'archive/unchanged', Identity: identity('f') },
  ];
  current.Observed!.Boundaries = [
    { RelativePath: 'archive/added', Identity: identity('a') },
    { RelativePath: 'archive/replaced', Identity: identity('b') },
    { RelativePath: 'archive/unchanged', Identity: identity('f') },
  ];
  api.roots = [longRoot];
  api.bindings.set(longRoot.Id, current);
  await openBindings(page);
  await expect(dialog(page).getByText('Storage changed', { exact: true })).toBeVisible();
  await expect(dialog(page).getByRole('button', { name: 'archive/added Added', exact: true })).toBeVisible();
  await expect(dialog(page).getByRole('button', { name: 'archive/removed Removed', exact: true })).toBeVisible();
  await expect(dialog(page).getByRole('button', { name: 'archive/replaced Changed', exact: true })).toBeVisible();
  await expect(dialog(page).getByRole('button', { name: 'archive/unchanged Unchanged', exact: true })).toBeVisible();
  await expect(dialog(page).getByText('e'.repeat(64), { exact: true })).toBeVisible();
  await expect(dialog(page).getByText('b'.repeat(64), { exact: true })).toBeVisible();
  await expect(dialog(page).getByText('previous-administrator', { exact: true })).toBeVisible();
  await expect(submit(page)).toHaveText('Accept replacement');
  await expect(submit(page)).toBeDisabled();
  await page.screenshot({ path: testInfo.outputPath('root-binding-desktop.png'), fullPage: true, animations: 'disabled' });
  await page.setViewportSize({ width: 390, height: 844 });
  await dialog(page).getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('button', { name: `Storage bindings for ${library.Name}`, exact: true }).click();
  const selection = dialog(page).getByRole('combobox', { name: /^Registered root/ });
  const storageStatus = dialog(page).getByText('Storage changed', { exact: true });
  const refresh = dialog(page).getByRole('button', { name: 'Refresh observation', exact: true });
  await expect(selection).toBeInViewport({ ratio: 1 });
  await expect(storageStatus).toBeInViewport({ ratio: 1 });
  await expect(dialog(page).getByRole('alert').filter({ hasText: 'The current storage differs from the approved binding.' })).toBeInViewport({ ratio: 1 });
  await expect(refresh).toBeInViewport({ ratio: 1 });
  expect(await selection.evaluate((element) => element.getBoundingClientRect().height)).toBeLessThanOrEqual(112);
  await expect(selection).toHaveAccessibleName('Registered root');
  await expect(selection).toHaveAccessibleDescription(new RegExp(longRoot.Path));
  await expect(selection).toHaveAccessibleDescription(new RegExp(longRoot.Id));
  await expect(selection).toHaveAccessibleDescription(/Each registration is identified by its root ID\./);
  await expect(selection).toContainText(longRoot.Path);
  await expect(selection).toContainText(longRoot.Id);
  const fullPath = dialog(page).locator('dd').filter({ hasText: longRoot.Path });
  await expect(fullPath).toHaveText(longRoot.Path);
  await expect(dialog(page).locator('dd').filter({ hasText: longRoot.Id })).toHaveText(longRoot.Id);
  const fullPathTop = await fullPath.evaluate((element) => element.getBoundingClientRect().top);
  expect(await storageStatus.evaluate((element) => element.getBoundingClientRect().bottom)).toBeLessThan(fullPathTop);
  expect(await refresh.evaluate((element) => element.getBoundingClientRect().bottom)).toBeLessThan(fullPathTop);
  const dimensions = await dialog(page).evaluate((element) => ({ width: element.getBoundingClientRect().width, scroll: element.scrollWidth, client: element.clientWidth, viewport: window.innerWidth }));
  expect(dimensions.width).toBeLessThanOrEqual(dimensions.viewport);
  expect(dimensions.scroll).toBeLessThanOrEqual(dimensions.client);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: testInfo.outputPath('root-binding-mobile.png'), fullPage: true, animations: 'disabled' });
  expect(api.puts()).toHaveLength(0);
});

test('refreshing the same observation clears consent and never approves automatically', async ({ page, api }) => {
  await openBindings(page);
  await consent(page).check();
  await expect(submit(page)).toBeEnabled();
  const response = page.waitForResponse((result) => result.request().method() === 'GET' && new URL(result.url()).pathname === bindingPath());
  await dialog(page).getByRole('button', { name: 'Refresh observation', exact: true }).click();
  await response;
  await expect(consent(page)).not.toBeChecked();
  await expect(submit(page)).toBeDisabled();
  expect(api.puts()).toHaveLength(0);
});

for (const code of ['root_binding_conflict', 'scan_busy']) {
  test(`${code} requires an explicit refresh and fresh consent before a new approval`, async ({ page, api }) => {
    api.handlers.set(`PUT ${bindingPath()}`, async (route) => failure(route, code));
    await openBindings(page);
    await consent(page).check();
    await submit(page).click();
    const message = code === 'scan_busy' ? /A scan is active for this library/ : /The registered root or storage changed after this observation/;
    await expect(dialog(page).getByText(message)).toBeVisible();
    await expect(consent(page)).not.toBeChecked();
    await expect(consent(page)).toBeDisabled();
    await expect(submit(page)).toBeDisabled();
    expect(api.puts()).toHaveLength(1);
    const refreshed = { ...unbound(), Revision: '9007199254740997', ObservedFingerprint: 'e'.repeat(64) };
    api.bindings.set(root.Id, refreshed);
    api.handlers.set(`PUT ${bindingPath()}`, async (route) => json(route, { Binding: accepted(refreshed) }));
    await dialog(page).getByRole('button', { name: 'Refresh observation', exact: true }).click();
    await expect(dialog(page).getByText(refreshed.Revision, { exact: true })).toBeVisible();
    await expect(consent(page)).not.toBeChecked();
    await expect(submit(page)).toBeDisabled();
    expect(api.puts()).toHaveLength(1);
    await consent(page).check();
    await submit(page).click();
    await expect(dialog(page).getByText('Storage binding saved.', { exact: true })).toBeVisible();
    expect(api.puts()).toHaveLength(2);
    expect(JSON.parse(api.puts()[1].body!)).toEqual({ Revision: refreshed.Revision, ObservedFingerprint: refreshed.ObservedFingerprint, AcknowledgeMissingRemoval: true });
  });
}

test('an unconfirmed approval cannot be retried until the observation is refreshed', async ({ page, api }) => {
  api.handlers.set(`PUT ${bindingPath()}`, async (route) => { await route.abort('failed'); });
  await openBindings(page);
  await consent(page).check();
  await submit(page).click();
  await expect(dialog(page).getByText(/The result could not be confirmed. The binding may have been saved/)).toBeVisible();
  await expect(submit(page)).toBeDisabled();
  await expect(consent(page)).toBeDisabled();
  expect(api.puts()).toHaveLength(1);
  api.bindings.set(root.Id, accepted(unbound()));
  await dialog(page).getByRole('button', { name: 'Refresh observation', exact: true }).click();
  await expect(dialog(page).getByText('Verified', { exact: true })).toBeVisible();
  await expect(consent(page)).toHaveCount(0);
  expect(api.puts()).toHaveLength(1);
});

test('unavailable storage keeps its prior approval and cannot be accepted', async ({ page, api }) => {
  const unavailable: RootBinding = { ...mismatch(), Status: 'unavailable', Observed: undefined, ObservedFingerprint: undefined };
  unavailable.Approved!.Boundaries = [{ RelativePath: 'archive/previously-mounted', Identity: identity('d') }];
  api.bindings.set(root.Id, unavailable);
  await openBindings(page);
  await expect(dialog(page).getByText(/The server could not fully observe this root/)).toBeVisible();
  await expect(dialog(page).getByRole('button', { name: 'archive/previously-mounted Not observed', exact: true })).toBeVisible();
  await expect(dialog(page).getByText('Removed', { exact: true })).toHaveCount(0);
  await expect(dialog(page).getByText('previous-administrator', { exact: true })).toBeVisible();
  await expect(submit(page)).toBeDisabled();
  await expect(consent(page)).toHaveCount(0);
  expect(api.puts()).toHaveLength(0);
});

test('switching registrations at the same path resets consent and uses the selected root ID', async ({ page, api }) => {
  api.roots = [root, { ...root, Id: secondRoot.Id, Revision: secondRoot.Revision }];
  api.bindings.set(secondRoot.Id, unbound(api.roots[1]));
  api.handlers.set(`PUT ${bindingPath(secondRoot.Id)}`, async (route) => json(route, { Binding: accepted(unbound(api.roots[1])) }));
  await openBindings(page);
  await consent(page).check();
  await dialog(page).getByRole('combobox', { name: /^Registered root/ }).click();
  await page.getByRole('option').filter({ hasText: secondRoot.Id }).click();
  await expect(dialog(page).getByText(secondRoot.Revision, { exact: true })).toBeVisible();
  await expect(dialog(page).getByRole('combobox', { name: 'Registered root', exact: true })).toHaveAccessibleDescription(new RegExp(secondRoot.Id));
  await expect(consent(page)).not.toBeChecked();
  await expect(submit(page)).toBeDisabled();
  await consent(page).check();
  await submit(page).click();
  await expect(dialog(page).getByText('Storage binding saved.', { exact: true })).toBeVisible();
  expect(api.puts()).toHaveLength(1);
  expect(api.puts()[0].path).toBe(bindingPath(secondRoot.Id));
  expect(JSON.parse(api.puts()[0].body!).Revision).toBe(secondRoot.Revision);
});

test('a late observation cannot replace the selected registration', async ({ page, api }) => {
  let release!: () => void;
  let completed!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  const finished = new Promise<void>((resolve) => { completed = resolve; });
  api.handlers.set(`GET ${bindingPath()}`, async (route) => {
    await gate;
    try { await json(route, { Binding: mismatch() }); }
    finally { completed(); }
  });
  await openBindings(page);
  await expect.poll(() => api.requests.filter((request) => request.path === bindingPath()).length).toBe(1);
  await dialog(page).getByRole('combobox', { name: /^Registered root/ }).click();
  await page.getByRole('option').filter({ hasText: secondRoot.Id }).click();
  try {
    await expect(dialog(page).getByText(secondRoot.Revision, { exact: true })).toBeVisible();
    await expect(consent(page)).toBeVisible();
  } finally { release(); }
  await finished;
  await expect(dialog(page).getByText(secondRoot.Revision, { exact: true })).toBeVisible();
  await expect(dialog(page).getByText(root.Revision, { exact: true })).toHaveCount(0);
  await expect(dialog(page).getByText('Storage changed', { exact: true })).toHaveCount(0);
  await expect(consent(page)).not.toBeChecked();
  expect(api.puts()).toHaveLength(0);
});

test('an incomplete boundary response blocks approval until a valid observation is reviewed', async ({ page, api }) => {
  api.bindings.set(root.Id, { ...unbound(), Observed: { ...topology(), Boundaries: undefined } });
  await openBindings(page);
  await expect(dialog(page).getByText(/The storage binding response is incomplete or invalid/)).toBeVisible();
  await expect(submit(page)).toBeDisabled();
  await expect(consent(page)).toHaveCount(0);
  api.bindings.set(root.Id, unbound());
  await dialog(page).getByRole('button', { name: 'Refresh observation', exact: true }).click();
  await expect(consent(page)).not.toBeChecked();
  await expect(submit(page)).toBeDisabled();
  expect(api.puts()).toHaveLength(0);
});
