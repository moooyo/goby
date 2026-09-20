import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Locator, Page, Request, Route } from '@playwright/test';
import type { MetadataDetail, MetadataValues } from '../src/api';
import type { EpisodeRoster, EpisodeRosterEntryInput, EpisodeRosterImport, EpisodeRosterInput } from '../src/episodeRosterApi';

// Every administrator API response is synthetic. Unknown requests are rejected
// rather than reaching the server that serves these browser assets.
const timestamp = '2026-09-20T00:00:00Z';
const csrfToken = 'synthetic-episode-roster-csrf';
const administrator = { Id: 'roster-admin', Name: 'Synthetic administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const library = { Id: 'roster-library', Name: 'Roster library', CollectionType: 'tvshows', Paths: ['/synthetic/roster'], CreatedAt: timestamp, LastScanAt: timestamp };
const seriesId = 'roster-series';
const seriesName = 'Expected episode series';
const rosterPath = `/admin/v1/series/${seriesId}/episode-roster`;
const metadataPath = `/admin/v1/items/${seriesId}/metadata`;
const source = { Key: 'series-guide', Label: 'Editorial guide', Revision: 'guide-v1' };
const first: EpisodeRosterEntryInput = { Key: 'pilot', SeasonNumber: 1, EpisodeNumber: 1, Name: 'Pilot', PremiereDate: '2024-02-29' };
const second: EpisodeRosterEntryInput = { Key: 'later', SeasonNumber: 1, EpisodeNumber: 3, Name: 'Later episode' };

function metadata(type = 'Series'): MetadataDetail {
  const values: MetadataValues = { Name: seriesName, SortName: '', Overview: '', OriginalTitle: '', OfficialRating: '', ProductionYear: 2026, PremiereDate: null, CommunityRating: null, ProviderIds: {}, Genres: [], Tags: [], Studios: [], People: [], IndexNumber: null, ParentIndexNumber: null, Album: '', Artists: [], AlbumArtists: [] };
  return { Item: { Id: seriesId, LibraryId: library.Id, ParentId: '', ParentName: '', Name: seriesName, Type: type, Path: '/synthetic/roster/series', IsFolder: type === 'Series' }, Revision: '1', Automatic: { ...values }, Effective: { ...values }, Overrides: {}, LockedValues: {}, LockedFields: [], EditableFields: ['Name', 'Overview'], InactiveFields: [], LastEditedBy: '', LastEditedAt: null };
}

function absent(): EpisodeRoster {
  return { SeriesId: seriesId, SeriesName: seriesName, Revision: '0', State: 'absent', Source: null, Entries: [], RetiredCount: 0 };
}

function active(revision = '1'): EpisodeRoster {
  return {
    ...absent(), Revision: revision, State: 'active', Source: { ...source, Kind: 'admin_import', ParserVersion: 1, SHA256: 'a'.repeat(64) },
    Entries: [
      { ...first, Id: 'expected-pilot', AvailableItemIds: ['physical-pilot'], AvailableItemCount: 1, Availability: 'available', Airing: 'aired' },
      { ...second, Id: 'expected-later', AvailableItemIds: [], AvailableItemCount: 0, Availability: 'missing', Airing: 'unknown' },
    ],
  };
}

async function json(route: Route, value: unknown, status = 200): Promise<void> {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) });
}

interface CapturedRequest { method: string; path: string; body: unknown; csrf: string | undefined }
class RosterAPI {
  item = metadata();
  roster = absent();
  requests: CapturedRequest[] = [];
  unexpected: string[] = [];
  handlers = new Map<string, (route: Route, request: Request) => Promise<void>>();
  writes(): CapturedRequest[] { return this.requests.filter((request) => request.method !== 'GET'); }

  apply(input: EpisodeRosterInput): void {
    const old = this.roster;
    const keys = new Set(input.Entries.map((entry) => entry.Key));
    this.roster = {
      ...old, Revision: (BigInt(old.Revision) + 1n).toString(), State: 'active',
      Source: { ...input.Source, Kind: 'admin_import', ParserVersion: 1, SHA256: 'b'.repeat(64) },
      Entries: [...input.Entries].sort((left, right) => left.SeasonNumber - right.SeasonNumber || left.EpisodeNumber - right.EpisodeNumber || left.Key.localeCompare(right.Key)).map((entry) => ({
        ...entry, Id: old.Entries.find((saved) => saved.Key === entry.Key)?.Id ?? `expected-${entry.Key}`,
        AvailableItemIds: entry.Key === 'pilot' ? ['physical-pilot'] : [], AvailableItemCount: entry.Key === 'pilot' ? 1 : 0, Availability: entry.Key === 'pilot' ? 'available' as const : 'missing' as const,
        Airing: entry.PremiereDate === undefined ? 'unknown' as const : entry.PremiereDate > '2026-09-20' ? 'unaired' as const : 'aired' as const,
      })),
      RetiredCount: old.RetiredCount + old.Entries.filter((entry) => !keys.has(entry.Key)).length,
    };
  }

  async install(context: BrowserContext): Promise<void> {
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const url = new URL(request.url());
      const captured = { method: request.method(), path: url.pathname, body: request.postData() ? request.postDataJSON() as unknown : null, csrf: request.headers()['x-csrf-token'] };
      this.requests.push(captured);
      const handler = this.handlers.get(`${captured.method} ${captured.path}`);
      if (handler) return handler(route, request);
      if (captured.method === 'GET') {
        if (captured.path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
        if (captured.path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrfToken });
        if (captured.path === '/admin/v1/libraries') return json(route, { Items: [library], TotalRecordCount: 1 });
        if (captured.path === `/admin/v1/libraries/${library.Id}/items`) return json(route, { Library: library, Items: [{ ...this.item.Item, ProductionYear: 2026, IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0 }], TotalRecordCount: 1, StartIndex: Number(url.searchParams.get('StartIndex')), Limit: Number(url.searchParams.get('Limit')) });
        if (captured.path === metadataPath) return json(route, this.item);
        if (captured.path === rosterPath) return json(route, this.roster);
      }
      if (captured.path === rosterPath && ['PUT', 'DELETE'].includes(captured.method)) {
        const input = captured.body as EpisodeRosterInput;
        if (input.Revision !== this.roster.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'The roster changed. Reload before saving.' } }, 409);
        if (captured.method === 'PUT') this.apply(input);
        else this.roster = { ...this.roster, Revision: (BigInt(this.roster.Revision) + 1n).toString(), State: 'withdrawn', RetiredCount: this.roster.RetiredCount + this.roster.Entries.length, Entries: [] };
        return json(route, this.roster);
      }
      this.unexpected.push(`${captured.method} ${captured.path}${url.search}`);
      return json(route, { Error: { Code: 'unexpected_synthetic_request', Message: 'No synthetic response was configured.' } }, 501);
    });
  }
}

const test = base.extend<{ api: RosterAPI }>({
  api: async ({ context, page }, use) => {
    const api = new RosterAPI();
    const pageErrors: string[] = [];
    page.on('pageerror', (error) => pageErrors.push(error.message));
    await api.install(context);
    await use(api);
    expect(api.unexpected, 'Every administrator API request must be explicitly mocked.').toEqual([]);
    expect(api.writes().every((request) => request.csrf === csrfToken), 'Every write must carry the administrator CSRF token.').toBe(true);
    expect(pageErrors).toEqual([]);
  },
});
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });

async function openRoster(page: Page): Promise<Locator> {
  await page.goto(`/admin/libraries/${library.Id}/items`);
  await page.getByRole('button', { name: `Episode roster for ${seriesName}`, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Episode roster', exact: true });
  await expect(dialog.getByRole('region', { name: 'Saved episode roster', exact: true })).toBeVisible();
  return dialog;
}

async function importRoster(dialog: Locator, value: EpisodeRosterImport | string): Promise<void> {
  const input = dialog.getByLabel('Import roster JSON', { exact: true });
  await expect(input).toBeEnabled();
  await input.setInputFiles({ name: 'episode-roster.json', mimeType: 'application/json', buffer: Buffer.from(typeof value === 'string' ? value : JSON.stringify(value)) });
}

async function save(page: Page, dialog: Locator, status = 200): Promise<void> {
  const [response] = await Promise.all([
    page.waitForResponse((result) => result.request().method() === 'PUT' && new URL(result.url()).pathname === rosterPath),
    dialog.getByRole('button', { name: 'Save roster', exact: true }).click(),
  ]);
  expect(response.status()).toBe(status);
  await expect(dialog.getByRole('button', { name: 'Reload', exact: true }).last()).toBeEnabled();
}

test('a declared roster is previewed before saving and numbering gaps do not add entries', async ({ page, api }) => {
  const dialog = await openRoster(page);
  await expect(dialog.getByRole('region', { name: 'Saved episode roster', exact: true })).toContainText('No roster');
  await importRoster(dialog, { Source: source, Entries: [second, first] });
  const preview = dialog.getByRole('region', { name: 'Episode roster preview', exact: true });
  await expect(preview).toContainText('2 entries in this draft');
  await expect(preview.getByRole('table')).toContainText('Later episode');
  await expect(preview.getByRole('table')).not.toContainText('Episode 2');
  expect(api.writes()).toEqual([]);
  await save(page, dialog);
  expect(api.writes().map((request) => request.body)).toEqual([{ Revision: '0', Source: source, Entries: [second, first] }]);
  await expect(dialog.getByRole('region', { name: 'Saved episode roster', exact: true })).toContainText('2 expected episodes · 1 available · 1 missing');
  await expect(dialog.getByRole('region', { name: 'Saved episode roster', exact: true })).toContainText('1 aired · 0 unaired · 1 unknown air date');
  await expect(dialog.getByRole('button', { name: 'Save roster', exact: true })).toBeDisabled();
});

test('metadata opens the same roster only after its draft is clean and other item types have no roster action', async ({ page, api }) => {
  await page.goto(`/admin/libraries/${library.Id}/items`);
  await page.getByRole('button', { name: `Edit metadata for ${seriesName}`, exact: true }).click();
  const metadataDialog = page.getByRole('dialog', { name: /^Edit metadata/ });
  const rosterButton = metadataDialog.getByRole('button', { name: 'Episode roster', exact: true });
  await expect(rosterButton).toBeEnabled();
  await metadataDialog.locator('#metadata-Name-input').fill('Unsaved series title');
  await expect(rosterButton).toBeDisabled();
  await metadataDialog.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('dialog', { name: 'Discard unsaved metadata changes?', exact: true }).getByRole('button', { name: 'Discard changes', exact: true }).click();
  await page.getByRole('button', { name: `Edit metadata for ${seriesName}`, exact: true }).click();
  await rosterButton.click();
  await expect(page.getByRole('dialog', { name: 'Episode roster', exact: true })).toBeVisible();
  await page.getByRole('dialog', { name: 'Episode roster', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
  await metadataDialog.getByRole('button', { name: 'Close', exact: true }).click();
  api.item = metadata('Movie');
  await page.reload();
  await expect(page.getByRole('button', { name: `Edit metadata for ${seriesName}`, exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: `Episode roster for ${seriesName}`, exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: `Edit metadata for ${seriesName}`, exact: true }).click();
  await expect(metadataDialog.getByRole('button', { name: 'Episode roster', exact: true })).toHaveCount(0);
  expect(api.writes()).toEqual([]);
});

test('entry validation rejects duplicate identities, invalid dates, unsupported fields, and invalid text', async ({ page, api }) => {
  const dialog = await openRoster(page);
  await importRoster(dialog, { Source: source, Entries: [first] });
  const entries = dialog.getByLabel('Episode entries JSON', { exact: true });
  const invalid = [
    [first, { ...second, Key: first.Key }],
    [first, { ...second, EpisodeNumber: first.EpisodeNumber }],
    [{ ...first, PremiereDate: '2025-02-29' }],
    [{ ...first, PremiereDate: '0000-01-01' }],
    [{ ...first, PremiereDate: null }],
    [{ ...first, SeasonNumber: -1 }],
    [{ ...first, EpisodeNumber: 1.5 }],
    [{ ...first, Name: ' trailing ' }],
    [{ ...first, Name: 'control\ntext' }],
    [{ ...first, Key: '\ud800' }],
    [{ ...first, AvailableItemIds: ['invented-file'] }],
  ];
  for (const value of invalid) {
    await entries.fill(JSON.stringify(value));
    await expect(entries).toHaveAttribute('aria-invalid', 'true');
    await expect(dialog.getByRole('button', { name: 'Save roster', exact: true })).toBeDisabled();
  }
  await entries.fill('[{"Key":"first","Key":"last","SeasonNumber":1,"EpisodeNumber":1,"Name":"Pilot"}]');
  await expect(dialog).toContainText('repeats the property Key');
  await entries.fill(JSON.stringify(Array.from({ length: 2001 }, (_, index) => ({ Key: `episode-${index}`, SeasonNumber: 1, EpisodeNumber: index, Name: '' }))));
  await expect(dialog).toContainText('at most 2,000 episodes');
  await expect(dialog.getByRole('button', { name: 'Save roster', exact: true })).toBeDisabled();
  expect(api.writes()).toEqual([]);
});

test('invalid and oversized imports preserve the existing draft and never write', async ({ page, api }) => {
  const dialog = await openRoster(page);
  await importRoster(dialog, { Source: source, Entries: [first] });
  await expect(dialog.getByRole('alert')).toHaveText('Imported roster ready for review. Save roster to replace the current expected episode list.');
  const preview = dialog.getByRole('table', { name: 'Episode roster preview', exact: true });
  await expect(preview).toBeVisible();
  await expect(preview.getByRole('row')).toHaveCount(2);
  await expect(preview).toContainText(first.Name);
  const before = await dialog.getByLabel('Episode entries JSON', { exact: true }).inputValue();
  const invalid = [
    { value: '{', message: 'Enter valid JSON before reviewing the roster.' },
    { value: JSON.stringify({ Source: source, Entries: [first], Revision: '123' }), message: 'Import an object containing Source {Key, Label, Revision} and Entries. Source Key and Revision are required and limited to 128 UTF-8 bytes; Label is limited to 256. Text must have no surrounding whitespace or control characters.' },
    { value: '{"Source":{"Key":"first","Key":"last","Label":"","Revision":"v1"},"Entries":[]}', message: 'The roster JSON repeats the property Key.' },
    { value: ' '.repeat(512 * 1024 + 1), message: 'Choose a roster JSON file of at most 512 KiB.' },
  ];
  for (const { value, message } of invalid) {
    await importRoster(dialog, value);
    await expect(dialog.getByRole('alert')).toHaveText(message);
    await expect(dialog.getByRole('alert')).toBeVisible();
    await expect(dialog.getByLabel('Episode entries JSON', { exact: true })).toHaveValue(before);
    await expect(dialog.getByRole('textbox', { name: 'Source key', exact: true })).toHaveValue(source.Key);
  }
  await expect(dialog.getByLabel('Import roster JSON', { exact: true })).toBeEnabled();
  await dialog.getByLabel('Import roster JSON', { exact: true }).setInputFiles({ name: 'invalid-utf8.json', mimeType: 'application/json', buffer: Buffer.from([0x7b, 0x22, 0x80, 0x22, 0x3a, 0x31, 0x7d]) });
  await expect(dialog).toContainText('encoded as valid UTF-8');
  await expect(dialog.getByLabel('Episode entries JSON', { exact: true })).toHaveValue(before);
  expect(api.writes()).toEqual([]);
});

test('source validation uses UTF-8 byte limits and empty names retain a readable preview', async ({ page, api }) => {
  const dialog = await openRoster(page);
  await importRoster(dialog, { Source: source, Entries: [{ ...first, Name: '' }] });
  await expect(dialog.getByRole('table', { name: 'Episode roster preview', exact: true })).toContainText('Episode 1');
  await dialog.getByRole('textbox', { name: 'Source key', exact: true }).fill('𐍈'.repeat(33));
  await expect(dialog.getByRole('textbox', { name: 'Source key', exact: true })).toHaveAttribute('aria-invalid', 'true');
  await expect(dialog.getByRole('button', { name: 'Save roster', exact: true })).toBeDisabled();
  await dialog.getByRole('textbox', { name: 'Source key', exact: true }).fill('series-世界');
  await dialog.getByLabel('Source label', { exact: true }).fill('');
  await dialog.getByRole('textbox', { name: 'Source version', exact: true }).fill('v2 ');
  await expect(dialog.getByRole('textbox', { name: 'Source version', exact: true })).toHaveAttribute('aria-invalid', 'true');
  await dialog.getByRole('textbox', { name: 'Source version', exact: true }).fill('v2');
  await save(page, dialog);
  expect((api.writes()[0].body as EpisodeRosterInput).Entries[0].Name).toBe('');
  expect((api.writes()[0].body as EpisodeRosterInput).Source).toEqual({ Key: 'series-世界', Label: '', Revision: 'v2' });
});

test('preview pagination is bounded while the save retains every declared entry', async ({ page, api }) => {
  const dialog = await openRoster(page);
  const entries = Array.from({ length: 26 }, (_, index) => ({ Key: `episode-${index + 1}`, SeasonNumber: 1, EpisodeNumber: index + 1, Name: `Declared episode ${index + 1}` }));
  await importRoster(dialog, { Source: source, Entries: entries });
  const table = dialog.getByRole('table', { name: 'Episode roster preview', exact: true });
  await expect(table.getByRole('row')).toHaveCount(26);
  await expect(table).not.toContainText('Declared episode 26');
  await dialog.getByRole('button', { name: 'Go to next page', exact: true }).click();
  await expect(table.getByRole('row')).toHaveCount(2);
  await expect(table).toContainText('Declared episode 26');
  await save(page, dialog);
  expect((api.writes()[0].body as EpisodeRosterInput).Entries).toEqual(entries);
});

test('revision conflicts retain the draft and reload preserves a revision above the JavaScript safe integer limit', async ({ page, api }) => {
  api.roster = active();
  const dialog = await openRoster(page);
  await dialog.getByRole('textbox', { name: 'Source version', exact: true }).fill('guide-v2');
  await dialog.getByLabel('Episode entries JSON', { exact: true }).fill(JSON.stringify([first]));
  api.roster = { ...api.roster, Revision: '9007199254740993' };
  await save(page, dialog, 409);
  await expect(dialog.getByRole('textbox', { name: 'Source version', exact: true })).toHaveValue('guide-v2');
  await expect(dialog.getByLabel('Episode entries JSON', { exact: true })).toHaveValue(JSON.stringify([first]));
  await expect(dialog.getByRole('textbox', { name: 'Source version', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Withdraw roster', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Save roster', exact: true })).toBeDisabled();
  page.once('dialog', (prompt) => { void prompt.accept(); });
  await dialog.getByRole('button', { name: 'Reload', exact: true }).last().click();
  await expect(dialog.getByRole('textbox', { name: 'Source version', exact: true })).toHaveValue('guide-v1');
  await expect(dialog.getByRole('textbox', { name: 'Source version', exact: true })).toBeEnabled();
  await dialog.getByRole('textbox', { name: 'Source version', exact: true }).fill('guide-v3');
  await save(page, dialog);
  expect((api.writes().at(-1)!.body as EpisodeRosterInput).Revision).toBe('9007199254740993');
  await expect(dialog.getByRole('region', { name: 'Saved episode roster', exact: true })).toContainText('Saved revision 9007199254740994');
});

test('a reused source version conflict requires review and does not silently rename the source version', async ({ page, api }) => {
  api.roster = active();
  api.handlers.set(`PUT ${rosterPath}`, async (route) => json(route, { Error: { Code: 'source_revision_conflict', Message: 'This source version already identifies different content.' } }, 409));
  const dialog = await openRoster(page);
  await dialog.getByLabel('Episode entries JSON', { exact: true }).fill(JSON.stringify([first]));
  await save(page, dialog, 409);
  await expect(dialog).toContainText('source version conflicts');
  await expect(dialog.getByRole('textbox', { name: 'Source version', exact: true })).toHaveValue('guide-v1');
  await expect(dialog.getByRole('button', { name: 'Save roster', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
});

test('an unconfirmed mutation response requires reload and never retries the write automatically', async ({ page, api }) => {
  api.handlers.set(`PUT ${rosterPath}`, async (route, request) => { api.apply(request.postDataJSON() as EpisodeRosterInput); await route.abort('failed'); });
  const dialog = await openRoster(page);
  await importRoster(dialog, { Source: source, Entries: [first] });
  await dialog.getByRole('button', { name: 'Save roster', exact: true }).click();
  await expect(dialog).toContainText('save response could not be confirmed');
  await expect(dialog.getByRole('button', { name: 'Save roster', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
  page.once('dialog', (prompt) => { void prompt.accept(); });
  await dialog.getByRole('button', { name: 'Reload', exact: true }).last().click();
  await expect(dialog.getByRole('region', { name: 'Saved episode roster', exact: true })).toContainText('1 expected episodes');
  await expect(dialog.getByRole('textbox', { name: 'Source version', exact: true })).toBeEnabled();
  expect(api.writes()).toHaveLength(1);
});

test('withdrawal is explicit, sends only the saved revision, and retains source provenance', async ({ page, api }) => {
  api.roster = active('9007199254740993');
  const dialog = await openRoster(page);
  await dialog.getByRole('textbox', { name: 'Source version', exact: true }).fill('unsaved-version');
  await dialog.getByRole('button', { name: 'Withdraw roster', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Withdraw episode roster?', exact: true });
  await expect(confirmation).toContainText('unsaved roster draft will also be discarded');
  await confirmation.getByRole('button', { name: 'Keep roster', exact: true }).click();
  expect(api.writes()).toEqual([]);
  await expect(dialog.getByRole('textbox', { name: 'Source version', exact: true })).toHaveValue('unsaved-version');
  await dialog.getByRole('button', { name: 'Withdraw roster', exact: true }).click();
  const [response] = await Promise.all([
    page.waitForResponse((result) => result.request().method() === 'DELETE' && new URL(result.url()).pathname === rosterPath),
    confirmation.getByRole('button', { name: 'Withdraw expected episodes', exact: true }).click(),
  ]);
  expect(response.status()).toBe(200);
  await expect(confirmation).not.toBeVisible();
  expect(api.writes().map(({ method, body }) => ({ method, body }))).toEqual([{ method: 'DELETE', body: { Revision: '9007199254740993' } }]);
  const saved = dialog.getByRole('region', { name: 'Saved episode roster', exact: true });
  await expect(saved).toContainText('Withdrawn');
  await expect(saved).toContainText('2 retired entries');
  await expect(saved).toContainText('Editorial guide');
  await expect(dialog.getByRole('button', { name: 'Withdraw roster', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('textbox', { name: 'Source version', exact: true })).toHaveValue('guide-v1');
});

test('malformed roster reads cannot be edited and retry admits a complete response', async ({ page, api }) => {
  api.handlers.set(`GET ${rosterPath}`, async (route) => json(route, { ...active(), Entries: [null] }));
  await page.goto(`/admin/libraries/${library.Id}/items`);
  await page.getByRole('button', { name: `Episode roster for ${seriesName}`, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Episode roster', exact: true });
  await expect(dialog).toContainText('roster response is incomplete');
  await expect(dialog.getByRole('button', { name: 'Save roster', exact: true })).toBeDisabled();
  await expect(dialog.getByLabel('Episode entries JSON', { exact: true })).toHaveCount(0);
  api.handlers.delete(`GET ${rosterPath}`);
  await dialog.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(dialog.getByRole('region', { name: 'Saved episode roster', exact: true })).toContainText('No roster');
  expect(api.writes()).toEqual([]);
});

test('closing a dirty roster keeps or discards the draft only after an explicit choice', async ({ page, api }) => {
  const dialog = await openRoster(page);
  await importRoster(dialog, { Source: source, Entries: [first] });
  page.once('dialog', (prompt) => { void prompt.dismiss(); });
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('textbox', { name: 'Source key', exact: true })).toHaveValue(source.Key);
  page.once('dialog', (prompt) => { void prompt.accept(); });
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await page.getByRole('button', { name: `Episode roster for ${seriesName}`, exact: true }).click();
  await expect(dialog.getByRole('textbox', { name: 'Source key', exact: true })).toHaveValue('');
  expect(api.writes()).toEqual([]);
});
