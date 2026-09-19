import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Page, Route } from '@playwright/test';

// These cases isolate administrator-session error handling in the shipped UI.
// Every native API request is mocked, including unexpected writes.
const csrf = 'phase3-synthetic-csrf';
const library = {
  Id: 'phase3-library', Name: 'Permission library', CollectionType: 'music',
  Paths: ['/media/music'], CreatedAt: '2026-09-19T00:00:00Z', LastScanAt: null,
  Revision: '9007199254740993', LibraryOptions: { EnableLocalMetadata: true, EnableLocalImages: true },
  RegisteredPaths: [{ Id: 'phase3-root', Path: '/media/music', ItemCount: 1 }],
};
interface Captured { method: string; path: string; csrf?: string; body: unknown }
async function json(route: Route, value: unknown, status = 200): Promise<void> { await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) }); }
class PermissionAPI {
  failure: 'administrator_required' | 'csrf_invalid' | undefined;
  requests: Captured[] = [];
  unexpected: string[] = [];
  savedName = library.Name;
  async install(context: BrowserContext): Promise<void> {
    await context.route('**/admin/v1/**', async (route, request) => {
      const url = new URL(request.url());
      const captured: Captured = { method: request.method(), path: url.pathname, csrf: request.headers()['x-csrf-token'], body: request.postData() ? request.postDataJSON() : undefined };
      this.requests.push(captured);
      if (captured.method === 'GET') {
        if (captured.path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
        if (captured.path === '/admin/v1/session') return json(route, { User: { Id: 'phase3-admin', Name: 'Synthetic administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: library.CreatedAt }, CSRFToken: csrf });
        if (captured.path === '/admin/v1/libraries') return json(route, { Items: [library], TotalRecordCount: 1 });
        if (captured.path === '/admin/v1/storage/roots') return json(route, { Configured: true, Items: [{ Path: '/media', Available: true }] });
        if (captured.path === `/admin/v1/libraries/${library.Id}`) return json(route, { Library: library });
        if (captured.path === '/admin/v1/storage/directories') {
          if (url.searchParams.get('Path')) return json(route, { Error: { Code: 'access_denied', Message: 'This directory is outside the allowed media paths.' } }, 403);
          return json(route, { Path: '', Items: [{ Name: 'media', Path: '/media' }], TotalRecordCount: 1, StartIndex: 0, Limit: 100 });
        }
      }
      if (captured.method === 'PATCH' && captured.path === `/admin/v1/libraries/${library.Id}`) {
        if (this.failure) return json(route, { Error: { Code: this.failure, Message: this.failure === 'csrf_invalid' ? 'Refresh the session and try again.' : 'Administrator access is required.' } }, 403);
        const input = captured.body as { Name: string; Revision: string };
        expect(input.Revision).toBe(library.Revision);
        this.savedName = input.Name;
        return json(route, { Library: { ...library, Name: this.savedName, Revision: '9007199254740994' } });
      }
      this.unexpected.push(`${captured.method} ${captured.path}`);
      return json(route, { Error: { Code: 'unexpected_test_request', Message: 'This test request was not mocked.' } }, 500);
    });
  }
  writes(): Captured[] { return this.requests.filter((request) => request.method === 'PATCH'); }
  sessionReads(): number { return this.requests.filter((request) => request.path === '/admin/v1/session').length; }
}
const test = base.extend<{ api: PermissionAPI }>({ api: async ({ context }, use) => { const api = new PermissionAPI(); await api.install(context); await use(api); expect(api.unexpected).toEqual([]); expect(api.writes().every((request) => request.csrf === csrf)).toBe(true); } });
test.use({ serviceWorkers: 'block' });
async function edit(page: Page): Promise<void> {
  await page.goto('/admin/libraries');
  await page.getByRole('button', { name: 'Edit library Permission library', exact: true }).click();
  // The anonymous role locator collects diagnostics only. All workflow actions
  // remain scoped to the dialog's exact accessible name below.
  const mountedDialog = page.getByRole('dialog');
  await expect(mountedDialog).toHaveCount(1);
  await expect(mountedDialog).toBeVisible();
  const diagnostic = await mountedDialog.evaluate((dialog) => {
    const facts = (element: Element) => ({
      Tag: element.tagName, Id: element.id, Text: element.textContent?.trim().slice(0, 500) ?? '',
      AriaHidden: element.getAttribute('aria-hidden'), Hidden: element.hasAttribute('hidden'),
    });
    const labelledBy = dialog.getAttribute('aria-labelledby');
    const referencedIds = labelledBy?.trim().split(/\s+/).filter(Boolean) ?? [];
    return {
      Role: dialog.getAttribute('role'), LabelledBy: labelledBy, Label: dialog.getAttribute('aria-label'),
      Titles: [...dialog.querySelectorAll('h1,h2,h3,h4,h5,h6')].map(facts),
      ExpectedTitleMatches: [...document.querySelectorAll('[id="library-editor-title"]')].map(facts),
      References: referencedIds.map((id) => ({ Id: id, Matches: [...document.querySelectorAll('[id]')].filter((element) => element.id === id).map(facts) })),
    };
  });
  await test.info().attach('library-dialog-aria', { body: Buffer.from(JSON.stringify(diagnostic, null, 2)), contentType: 'application/json' });
  expect(diagnostic.LabelledBy, 'The dialog must reference its visible library editor title.').toBe('library-editor-title');
  expect(diagnostic.ExpectedTitleMatches, 'The library editor title ID must be unique.').toHaveLength(1);
  expect(diagnostic.ExpectedTitleMatches[0].Text).toBe('Edit library');
  expect(diagnostic.References.every((reference) => reference.Matches.length === 1), 'Every accessible name reference must identify exactly one element.').toBe(true);
  const editor = page.getByRole('dialog', { name: 'Edit library', exact: true });
  await expect(editor, 'The library dialog must expose its visible title as its accessible name.').toBeVisible({ timeout: 5000 });
  await editor.getByRole('textbox', { name: 'Library name', exact: true }).fill('Unsaved library draft');
}

test('directory access denial preserves the administrator session and library draft', async ({ page, api }) => {
  await edit(page);
  const editor = page.getByRole('dialog', { name: 'Edit library', exact: true });
  const initialSessionReads = api.sessionReads();
  await editor.getByRole('button', { name: 'Browse directory 1', exact: true }).click();
  const picker = page.getByRole('dialog', { name: 'Choose a media directory', exact: true });
  await picker.getByLabel('Directory path', { exact: true }).fill('/outside/allowed/media');
  await picker.getByRole('button', { name: 'Browse', exact: true }).click();
  await expect(picker.getByText('This directory is outside the allowed media paths.', { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toHaveCount(0);
  await picker.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(editor.getByRole('textbox', { name: 'Library name', exact: true })).toHaveValue('Unsaved library draft');
  await editor.getByRole('button', { name: 'Save library', exact: true }).click();
  await expect(editor).not.toBeVisible();
  expect(api.savedName).toBe('Unsaved library draft');
  expect(api.writes()).toHaveLength(1);
  expect(api.sessionReads()).toBe(initialSessionReads);
});

for (const failure of ['administrator_required', 'csrf_invalid'] as const) {
  test(`${failure} ends the session and never retries the draft mutation`, async ({ page, api }) => {
    await edit(page);
    const initialSessionReads = api.sessionReads();
    api.failure = failure;
    await page.getByRole('dialog', { name: 'Edit library', exact: true }).getByRole('button', { name: 'Save library', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    await expect(page.getByRole('dialog', { name: 'Edit library', exact: true })).toHaveCount(0);
    expect(api.savedName).toBe(library.Name);
    expect(api.writes()).toHaveLength(1);
    expect(api.writes()[0].body).toMatchObject({ Name: 'Unsaved library draft', Revision: library.Revision });
    expect(api.sessionReads()).toBe(initialSessionReads);
  });
}
