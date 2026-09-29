import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Locator, Page, Request, Route } from '@playwright/test';
import type { Library, LibraryInput, ManagedUser, UpdateUserInput, UserPolicy } from '../src/api';
import type { DeletionFolder } from '../src/deletionFoldersApi';
import type { EditableLibrary, LibraryUpdate } from '../src/libraryManagementApi';

const timestamp = '2026-09-20T00:00:00Z';
const csrf = 'synthetic-management-configuration-csrf';
const administrator = { Id: 'configuration-admin', Name: 'Configuration administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const library: Library = { Id: 'configuration-library', Name: 'Configuration movies', CollectionType: 'movies', Paths: ['/synthetic/media'], CreatedAt: timestamp, LastScanAt: null };
const member = { Id: 'configuration-member', Name: 'Configuration member', IsAdministrator: false, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const userPath = `/admin/v1/users/${member.Id}`;
const folderPath = '/admin/v1/policy/deletion-folders';
const policy: UserPolicy = {
  EnableAllFolders: true, EnabledFolders: [], EnableMediaPlayback: true, EnablePlaybackRemuxing: true, EnableAudioPlaybackTranscoding: true, EnableVideoPlaybackTranscoding: true,
  IsHidden: false, IsHiddenRemotely: false, IsHiddenFromUnusedDevices: false, MaxParentalRating: null, AllowTagOrRating: false, IsTagBlockingModeInclusive: false,
  BlockedTags: [], IncludeTags: [], BlockUnratedItems: ['Game', 'LiveTvChannel'], EnableUserPreferenceAccess: true, AccessSchedules: [], EnableRemoteControlOfOtherUsers: false,
  EnableSharedDeviceControl: false, EnableRemoteAccess: true, AutoRemoteQuality: 0, EnableContentDeletion: false, RestrictedFeatures: [], EnableContentDeletionFromFolders: ['/legacy/media/path', 'unresolved-folder-id'],
  EnableContentDownloading: true, EnableSubtitleDownloading: true, EnableSubtitleManagement: false, RemoteClientBitrateLimit: 0, ExcludedSubFolders: [], SimultaneousStreamLimit: 0, EnabledDevices: [], EnableAllDevices: true,
};
async function json(route: Route, value: unknown, status = 200): Promise<void> { await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) }); }
class ConfigurationAPI {
  libraries: Library[] = [library]; user: ManagedUser = { ...member, Revision: '9007199254740993', Policy: structuredClone(policy) };
  folders: DeletionFolder[] = [
    { Id: library.Id, Name: library.Name, Type: 'CollectionFolder', LibraryId: library.Id, LibraryName: library.Name, ParentId: '', Path: '' },
    { Id: 'current-folder', Name: 'Current folder', Type: 'Folder', LibraryId: library.Id, LibraryName: library.Name, ParentId: library.Id, Path: '/synthetic/media/current' },
  ];
  writes: { path: string; method: string; body: unknown; csrf?: string }[] = []; searches: URLSearchParams[] = []; unexpected: string[] = [];
  handlers = new Map<string, (route: Route, request: Request) => Promise<void>>();
  async install(context: BrowserContext): Promise<void> {
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const url = new URL(request.url()); const method = request.method(); const path = url.pathname;
      if (method !== 'GET') this.writes.push({ path, method, body: request.postData() ? request.postDataJSON() as unknown : null, csrf: request.headers()['x-csrf-token'] });
      const handler = this.handlers.get(`${method} ${path}`); if (handler) return handler(route, request);
      if (method === 'GET') {
        if (path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
        if (path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrf });
        if (path === '/admin/v1/libraries') return json(route, { Items: this.libraries, TotalRecordCount: this.libraries.length });
        if (path === '/admin/v1/storage/roots') return json(route, { Configured: true, Items: [{ Path: '/synthetic/media', Available: true }] });
        if (path === '/admin/v1/users') return json(route, { Items: [member], TotalRecordCount: 1 });
        if (path === userPath) return json(route, { User: this.user });
        if (path === '/admin/v1/features') return json(route, { Items: [{ Id: 'playback', Name: 'Playback', FeatureType: 'User' }] });
        if (path === folderPath) {
          this.searches.push(url.searchParams); const search = (url.searchParams.get('SearchTerm') ?? '').toLowerCase(); const libraryId = url.searchParams.get('LibraryId');
          const items = this.folders.filter((folder) => (!libraryId || folder.LibraryId === libraryId) && folder.Name.toLowerCase().includes(search));
          const start = Number(url.searchParams.get('StartIndex')); const limit = Number(url.searchParams.get('Limit'));
          return json(route, { Items: items.slice(start, start + limit), TotalRecordCount: items.length, StartIndex: start, Limit: limit });
        }
      }
      if (method === 'POST' && path === '/admin/v1/libraries') {
        const input = request.postDataJSON() as LibraryInput; const created = { ...library, Id: 'created-library', Name: input.Name, CollectionType: input.CollectionType, Paths: input.Paths, LibraryOptions: input.LibraryOptions };
        this.libraries.push(created); return json(route, { Library: created }, 201);
      }
      if (method === 'PUT' && path === userPath) {
        const input = request.postDataJSON() as UpdateUserInput; this.user = { ...this.user, ...input, Revision: (BigInt(this.user.Revision) + 1n).toString() }; return json(route, { User: this.user, CurrentSessionRevoked: false });
      }
      this.unexpected.push(`${method} ${path}`); return json(route, { Error: { Code: 'unexpected_synthetic_request', Message: 'No synthetic response was configured.' } }, 501);
    });
  }
}
const test = base.extend<{ api: ConfigurationAPI }>({ api: async ({ context, page }, use) => {
  const api = new ConfigurationAPI(); const errors: string[] = []; page.on('pageerror', (error) => errors.push(error.message));
  await api.install(context); await use(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); expect(api.writes.every((request) => request.csrf === csrf)).toBe(true);
} });
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });
async function openUser(page: Page): Promise<Locator> { await page.goto('/admin/users'); await page.getByRole('button', { name: `Manage ${member.Name}`, exact: true }).click(); const dialog = page.getByRole('dialog', { name: 'Manage user', exact: true }); await expect(dialog.getByRole('textbox', { name: 'Username', exact: true })).toHaveValue(member.Name); return dialog; }
async function saveUser(page: Page, dialog: Locator): Promise<void> {
  const [response] = await Promise.all([page.waitForResponse((value) => value.request().method() === 'PUT' && new URL(value.url()).pathname === userPath), dialog.getByRole('button', { name: 'Save changes', exact: true }).click()]);
  expect(response.status()).toBe(200); await expect(dialog.getByText(`User ${member.Name} updated.`, { exact: true })).toBeVisible(); await expect(dialog.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
}

test('new libraries explicitly save both local import choices before any scan', async ({ page, api }) => {
  await page.goto('/admin/libraries'); await page.getByRole('button', { name: 'Create library', exact: true }).first().click(); const dialog = page.getByRole('dialog', { name: 'Create library', exact: true });
  await expect(dialog.getByRole('checkbox', { name: 'Import local metadata files', exact: true })).toBeChecked(); await expect(dialog.getByRole('checkbox', { name: 'Import local artwork', exact: true })).toBeChecked();
  await dialog.getByRole('textbox', { name: 'Library name', exact: true }).fill('Local import choices'); await dialog.getByRole('textbox', { name: 'Media directories', exact: true }).fill('/synthetic/media/choice');
  await dialog.getByRole('checkbox', { name: 'Import local metadata files', exact: true }).uncheck(); await dialog.getByRole('checkbox', { name: 'Import local artwork', exact: true }).uncheck(); await dialog.getByRole('checkbox', { name: /^Scan after creating/ }).uncheck();
  await dialog.getByRole('button', { name: 'Create library', exact: true }).click(); await expect(dialog).not.toBeVisible();
  expect(api.writes).toHaveLength(1); expect(api.writes[0].body).toEqual({ Name: 'Local import choices', CollectionType: 'movies', Paths: ['/synthetic/media/choice'], Scan: false, LibraryOptions: { EnableLocalMetadata: false, EnableLocalImages: false, EnableEmbeddedArtwork: true, EnableIntroDetection: false } });
});

test('deletion grants come from current folder choices while legacy values and inactive categories survive unrelated edits', async ({ page, api }) => {
  const dialog = await openUser(page); await expect(dialog.getByRole('textbox', { name: 'Folders allowing media deletion', exact: true })).toHaveCount(0);
  await expect(dialog.getByRole('checkbox', { name: 'Game', exact: true })).toHaveCount(0); await expect(dialog.getByRole('checkbox', { name: 'LiveTvChannel', exact: true })).toHaveCount(0);
  await expect(dialog).toContainText('Game · Inactive saved category');
  await dialog.getByRole('button', { name: 'Choose deletion folders', exact: true }).click(); const picker = page.getByRole('dialog', { name: 'Choose deletion folders', exact: true });
  await picker.getByRole('textbox', { name: 'Search deletion folders', exact: true }).fill('Current'); await picker.getByRole('button', { name: 'Search folders', exact: true }).click();
  await picker.getByRole('checkbox', { name: 'Allow deletion in Current folder', exact: true }).check(); await picker.getByRole('button', { name: 'Done choosing folders', exact: true }).click();
  expect(api.writes).toEqual([]); await saveUser(page, dialog);
  const input = api.writes[0].body as UpdateUserInput;
  expect(input.Revision).toBe('9007199254740993'); expect(input.Policy.EnableContentDeletionFromFolders).toEqual(['/legacy/media/path', 'current-folder', 'unresolved-folder-id']);
  expect(input.Policy.BlockUnratedItems).toEqual(['Game', 'LiveTvChannel']); expect(api.searches.some((query) => query.get('SearchTerm') === 'Current')).toBe(true);
});

test('legacy grants and inactive unrated categories can be removed explicitly', async ({ page, api }) => {
  const dialog = await openUser(page); await dialog.getByRole('button', { name: 'Remove deletion grant /legacy/media/path', exact: true }).click();
  await dialog.getByRole('button', { name: 'Remove inactive unrated category LiveTvChannel', exact: true }).click(); await saveUser(page, dialog);
  expect((api.writes[0].body as UpdateUserInput).Policy.EnableContentDeletionFromFolders).toEqual(['unresolved-folder-id']);
  expect((api.writes[0].body as UpdateUserInput).Policy.BlockUnratedItems).toEqual(['Game']);
  await expect(dialog.getByRole('checkbox', { name: 'LiveTvChannel', exact: true })).toHaveCount(0);
});

test('a malformed folder response cannot introduce file grants and does not erase retained identifiers', async ({ page, api }) => {
  api.handlers.set(`GET ${folderPath}`, async (route) => json(route, { Items: [{ ...api.folders[1], Type: 'Movie' }], TotalRecordCount: 1, StartIndex: 0, Limit: 50 }));
  const dialog = await openUser(page); await dialog.getByRole('button', { name: 'Choose deletion folders', exact: true }).click(); const picker = page.getByRole('dialog', { name: 'Choose deletion folders', exact: true });
  await expect(picker.getByRole('alert')).toContainText('deletion folder list is incomplete'); await expect(picker.getByRole('checkbox')).toHaveCount(0);
  await picker.getByRole('button', { name: 'Done choosing folders', exact: true }).click();
  await dialog.getByRole('textbox', { name: 'Simultaneous stream limit', exact: true }).fill('2'); await saveUser(page, dialog);
  expect((api.writes[0].body as UpdateUserInput).Policy.EnableContentDeletionFromFolders).toEqual(['/legacy/media/path', 'unresolved-folder-id']);
});

test('TV library creation opts into background intro detection without a separate analysis action', async ({ page, api }) => {
  await page.goto('/admin/libraries');
  await page.getByRole('button', { name: 'Create library', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Create library', exact: true });
  const toggle = dialog.getByRole('checkbox', { name: 'Automatic intro detection', exact: true });
  await expect(toggle).toHaveCount(0);
  await dialog.getByRole('combobox', { name: 'Content type', exact: true }).click();
  await page.getByRole('option', { name: 'TV shows', exact: true }).click();
  await expect(toggle).not.toBeChecked();
  await toggle.check();
  await expect(dialog).toContainText('If no intro is found, playback stays unchanged.');
  await dialog.getByRole('combobox', { name: 'Content type', exact: true }).click();
  await page.getByRole('option', { name: 'Movies', exact: true }).click();
  await expect(toggle).toHaveCount(0);
  await dialog.getByRole('combobox', { name: 'Content type', exact: true }).click();
  await page.getByRole('option', { name: 'TV shows', exact: true }).click();
  await expect(toggle).not.toBeChecked();
  await toggle.check();
  await dialog.getByRole('textbox', { name: 'Library name', exact: true }).fill('Automatic series');
  await dialog.getByRole('textbox', { name: 'Media directories', exact: true }).fill('/synthetic/media/series');
  await dialog.getByRole('checkbox', { name: /^Scan after creating/ }).uncheck();
  expect(api.writes).toHaveLength(0);
  await dialog.getByRole('button', { name: 'Create library', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole('alert').filter({ hasText: 'Automatic intro detection is enabled.' })).toBeVisible();
  await expect(page.getByRole('alert').filter({ hasText: 'Automatic intro detection is enabled.' }).getByRole('button', { name: 'View tasks', exact: true })).toBeVisible();
  expect(api.writes).toHaveLength(1);
  expect(api.writes[0]).toMatchObject({ method: 'POST', path: '/admin/v1/libraries', body: {
    Name: 'Automatic series', CollectionType: 'tvshows', Paths: ['/synthetic/media/series'], Scan: false,
    LibraryOptions: { EnableLocalMetadata: true, EnableLocalImages: true, EnableEmbeddedArtwork: true, EnableIntroDetection: true },
  } });
});

test('TV library settings preserve and disable intro automation through the library revision', async ({ page, api }) => {
  let saved: EditableLibrary = { ...library, Name: 'Series settings', CollectionType: 'tvshows', Revision: '9007199254740993',
    LibraryOptions: { EnableLocalMetadata: false, EnableLocalImages: true, EnableEmbeddedArtwork: false, EnableIntroDetection: false },
    RegisteredPaths: [{ Id: 'series-root', Path: '/synthetic/media', ItemCount: 3 }],
  };
  api.libraries = [saved];
  const target = `/admin/v1/libraries/${saved.Id}`;
  api.handlers.set(`GET ${target}`, async (route) => json(route, { Library: saved }));
  api.handlers.set(`PATCH ${target}`, async (route, request) => {
    const input = request.postDataJSON() as LibraryUpdate;
    expect(input.Revision).toBe(saved.Revision);
    saved = { ...saved, ...input, Revision: (BigInt(saved.Revision) + 1n).toString() };
    api.libraries = [saved];
    return json(route, { Library: saved });
  });
  await page.goto('/admin/libraries');
  await page.getByRole('button', { name: 'More actions for Series settings', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Edit library', exact: true }).click();
  let dialog = page.getByRole('dialog', { name: 'Edit library', exact: true });
  await expect(dialog.getByRole('checkbox', { name: 'Automatic intro detection', exact: true })).not.toBeChecked();
  await dialog.getByRole('checkbox', { name: 'Automatic intro detection', exact: true }).check();
  await dialog.getByRole('button', { name: 'Save library', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole('alert').filter({ hasText: 'Automatic intro detection is enabled.' })).toBeVisible();
  expect(api.writes[0].body).toMatchObject({ Revision: '9007199254740993', Scan: false,
    LibraryOptions: { EnableLocalMetadata: false, EnableLocalImages: true, EnableEmbeddedArtwork: false, EnableIntroDetection: true },
  });
  await page.getByRole('button', { name: 'More actions for Series settings', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Edit library', exact: true }).click();
  dialog = page.getByRole('dialog', { name: 'Edit library', exact: true });
  await expect(dialog.getByRole('checkbox', { name: 'Automatic intro detection', exact: true })).toBeChecked();
  await dialog.getByRole('checkbox', { name: 'Automatic intro detection', exact: true }).uncheck();
  await dialog.getByRole('button', { name: 'Save library', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(api.writes).toHaveLength(2);
  expect(api.writes[1]).toMatchObject({ method: 'PATCH', path: target, body: { Revision: '9007199254740994', LibraryOptions: { EnableIntroDetection: false } } });
});
