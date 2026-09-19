import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Page, Request, Route } from '@playwright/test';
import type { ManagementSettings, MetadataDetail, MetadataItemSummary, MetadataValues, ServerSettings, SettingsUpdateInput, UpdateUserInput, UserPolicy } from '../src/api';
import type { CollectionMember, CollectionPatch, ManagedCollection } from '../src/collectionsApi';
import type { ImageCandidate, MetadataCandidate, SubtitleCandidate } from '../src/providersApi';

// Every native API request is synthetic, including rejected and lost mutations.
// These scenarios exercise browser workflows without writing to a live backend.
const timestamp = '2026-09-19T08:00:00Z';
const csrfToken = 'synthetic-management-wave-csrf';
const administrator = { Id: 'management-administrator', Name: 'Synthetic administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const library = { Id: 'library-one', Name: 'Synthetic movies', CollectionType: 'movies', Paths: ['/synthetic/media'], CreatedAt: timestamp, LastScanAt: timestamp };
const playlistPath = '/admin/v1/playlists/playlist-one';
const itemPath = '/admin/v1/items/movie-one';

function management(): ManagementSettings {
  return {
    Metadata: { EnableInternetProviders: true, PreferredMetadataLanguage: 'en', MetadataCountryCode: 'US' },
    Subtitles: { DownloadLanguages: ['en'], DownloadMovieSubtitles: false, DownloadEpisodeSubtitles: false },
    Tasks: { MaxConcurrent: 2, CacheRetentionDays: 30, CacheMaxEntries: 1000 },
  };
}

function settings(): ServerSettings {
  const defaults = { ServerName: 'Synthetic management server', MaxBitrate: 20_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2 };
  return {
    Revision: '1', Defaults: defaults, Effective: { ...defaults },
    Overrides: { ServerName: null, MaxBitrate: null, MaxWidth: null, MaxHeight: null, MaxAudioChannels: null },
    Sources: { ServerName: 'deployment', MaxBitrate: 'deployment', MaxWidth: 'deployment', MaxHeight: 'deployment', MaxAudioChannels: 'deployment' },
    UpdatedAt: timestamp, ServerNameMode: 'deployment', Encoding: { TranscodingMaxWidth: 0 },
    Deployment: { HostName: 'synthetic-server', TranscodingEnabled: false, HardwareDecoder: 'software', HardwareEncoder: 'software', Threads: 1, MaxJobs: 0, MaxUserJobs: 0, MaxSessionJobs: 0 },
    Management: management(), ManagementDefaults: management(),
  };
}

function metadata(): MetadataDetail {
  const values: MetadataValues = {
    Name: 'Catalog feature', SortName: '', Overview: '', OriginalTitle: '', OfficialRating: '', ProductionYear: 2020,
    PremiereDate: null, CommunityRating: null, ProviderIds: {}, Genres: [], Tags: [], Studios: [], People: [], IndexNumber: null, ParentIndexNumber: null,
    Album: '', Artists: [], AlbumArtists: [],
  };
  return {
    Item: { Id: 'movie-one', LibraryId: library.Id, ParentId: '', ParentName: '', Name: values.Name, Type: 'Movie', Path: '/synthetic/media/feature.mkv', IsFolder: false },
    Revision: '9007199254740993', Automatic: { ...values }, Effective: { ...values }, Overrides: {}, LockedValues: {},
    LockedFields: [], EditableFields: ['Name', 'Overview', 'ProviderIds'], InactiveFields: [], LastEditedBy: '', LastEditedAt: null,
  };
}

async function json(route: Route, value: unknown, status = 200): Promise<void> {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) });
}

interface CapturedRequest { method: string; path: string; query: string; body: unknown; csrf: string | undefined }

class ManagementMock {
  collection: ManagedCollection = {
    Id: 'playlist-one', Name: 'Repeatable playlist', Type: 'Playlist', IsFolder: true, ParentId: '', OwnerId: administrator.Id,
    MediaType: 'Audio', IsPublic: false, IsLocked: false, ChildCount: 3, Shares: [],
  };
  members: CollectionMember[] = [
    { Id: 'track-repeat', PlaylistItemId: 'entry-first', Name: 'Repeat track', Type: 'Audio' },
    { Id: 'track-other', PlaylistItemId: 'entry-middle', Name: 'Other track', Type: 'Audio' },
    { Id: 'track-repeat', PlaylistItemId: 'entry-last', Name: 'Repeat track', Type: 'Audio' },
  ];
  item = metadata();
  settings = settings();
  requests: CapturedRequest[] = [];
  unexpected: string[] = [];
  handlers = new Map<string, (route: Route, request: Request) => Promise<void>>();

  writes(): CapturedRequest[] { return this.requests.filter((request) => request.method !== 'GET'); }
  reads(path: string): CapturedRequest[] { return this.requests.filter((request) => request.method === 'GET' && request.path === path); }

  async install(context: BrowserContext): Promise<void> {
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const url = new URL(request.url());
      const captured: CapturedRequest = {
        method: request.method(), path: url.pathname, query: url.search,
        body: request.postData() ? request.postDataJSON() as unknown : null,
        csrf: request.headers()['x-csrf-token'],
      };
      this.requests.push(captured);
      const handler = this.handlers.get(`${captured.method} ${captured.path}`);
      if (handler) return handler(route, request);
      if (captured.method === 'GET') {
        if (captured.path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
        if (captured.path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrfToken });
        if (captured.path === '/admin/v1/users') return json(route, { Items: [administrator], TotalRecordCount: 1 });
        if (captured.path === '/admin/v1/features') return json(route, { Items: [
          { Id: 'playback', Name: 'Playback', FeatureType: 'User' },
          { Id: 'downloads', Name: 'Downloads', FeatureType: 'User' },
          { Id: 'playlists', Name: 'Playlists', FeatureType: 'User' },
          { Id: 'collections', Name: 'Collections', FeatureType: 'User' },
          { Id: 'subtitle_downloads', Name: 'Subtitle downloads', FeatureType: 'User' },
          { Id: 'subtitle_management', Name: 'Subtitle management', FeatureType: 'User' },
          { Id: 'remote_control', Name: 'Remote control', FeatureType: 'User' },
          { Id: 'preferences', Name: 'Preferences', FeatureType: 'User' },
        ] });
        if (captured.path === '/admin/v1/playlists') return json(route, { Items: [this.collection], TotalRecordCount: 1 });
        if (captured.path === playlistPath) return json(route, this.collection);
        if (captured.path === `${playlistPath}/items`) return json(route, { Items: this.members, TotalRecordCount: this.members.length });
        if (captured.path === '/admin/v1/libraries') return json(route, { Items: [library], TotalRecordCount: 1 });
        if (captured.path === `/admin/v1/libraries/${library.Id}/items`) {
          const item: MetadataItemSummary = { ...this.item.Item, ProductionYear: this.item.Effective.ProductionYear, IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0 };
          return json(route, { Library: library, Items: [item], TotalRecordCount: 1, StartIndex: Number(url.searchParams.get('StartIndex')), Limit: Number(url.searchParams.get('Limit')) });
        }
        if (captured.path === `${itemPath}/metadata`) return json(route, this.item);
        if (captured.path === '/admin/v1/providers') return json(route, {
          Enabled: true,
          Items: [
            { Id: 'tmdb', Name: 'TMDB', Configured: true, Capabilities: ['metadata', 'images'], Attribution: 'Synthetic movie provider', Website: '' },
            { Id: 'opensubtitles', Name: 'OpenSubtitles', Configured: true, Capabilities: ['subtitles'], Attribution: 'Synthetic subtitle provider', Website: '' },
          ],
        });
        if (captured.path === `${itemPath}/providers/provenance`) return json(route, { Items: [] });
        if (captured.path === '/admin/v1/settings') return json(route, this.settings);
        if (captured.path === '/admin/v1/media-diagnostics') return json(route, {
          InstanceId: 'a'.repeat(32), StartToken: '', StartTokenExpiresAt: timestamp, Available: false,
          UnavailableReason: 'Synthetic fixture', HardwareConfigured: false, RetentionSeconds: 1800, MaxRetainedRuns: 32, Items: [],
        });
      }
      this.unexpected.push(`${captured.method} ${captured.path}${captured.query}`);
      // Never forward an unhandled mutation or read to the server.
      return json(route, { Error: { Code: 'unexpected_synthetic_request', Message: 'No synthetic response was configured.' }, RequestId: 'synthetic-request' }, 501);
    });
  }
}

const test = base.extend<{ api: ManagementMock }>({
  api: async ({ context }, use) => {
    const api = new ManagementMock();
    await api.install(context);
    await use(api);
    expect(api.unexpected, 'Every administrator API request must be explicitly mocked.').toEqual([]);
    expect(api.writes().every((request) => request.csrf === csrfToken), 'Every mutation must carry the administrator CSRF token.').toBe(true);
  },
});

test.use({ serviceWorkers: 'block' });

async function openPlaylist(page: Page): Promise<void> {
  await page.goto('/admin/collections');
  await expect(page.getByRole('heading', { name: 'Playlists and collections', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Manage Repeatable playlist', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Manage playlist', exact: true }).getByRole('textbox', { name: /^Playlist name/ })).toHaveValue('Repeatable playlist');
}

async function openSources(page: Page): Promise<void> {
  await page.goto(`/admin/libraries/${library.Id}/items`);
  await page.getByRole('button', { name: 'Online sources for Catalog feature', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Online sources · Catalog feature', exact: true }).getByRole('button', { name: 'Search metadata', exact: true })).toBeEnabled();
}

test('playlist removal and reordering target the selected entry when media repeats', async ({ page, api }) => {
  api.handlers.set(`DELETE ${playlistPath}/items`, async (route, request) => {
    const query = new URL(request.url()).searchParams;
    expect([...query.entries()]).toEqual([['EntryIds', 'entry-last']]);
    api.members = api.members.filter((member) => member.PlaylistItemId !== query.get('EntryIds'));
    api.collection.ChildCount = api.members.length;
    await json(route, {});
  });
  api.handlers.set(`POST ${playlistPath}/items/entry-middle/move/0`, async (route) => {
    api.members = [api.members[1], api.members[0]];
    await json(route, {});
  });
  await openPlaylist(page);
  const dialog = page.getByRole('dialog', { name: 'Manage playlist', exact: true });
  await dialog.getByRole('tab', { name: 'Items', exact: true }).click();
  const rows = dialog.getByRole('list', { name: 'Collection items', exact: true }).getByRole('listitem');
  await expect(rows).toHaveCount(3);
  await expect(rows.nth(0).getByRole('button', { name: 'Move Repeat track up', exact: true })).toBeDisabled();
  await rows.nth(2).getByRole('button', { name: 'Remove Repeat track from playlist', exact: true }).click();
  await expect(rows).toHaveCount(2);
  await expect(rows.nth(0)).toContainText('Repeat track');
  await expect(rows.nth(1)).toContainText('Other track');
  await rows.nth(1).getByRole('button', { name: 'Move Other track up', exact: true }).click();
  await expect(rows.nth(0)).toContainText('Other track');
  await expect(rows.nth(1)).toContainText('Repeat track');
  expect(api.members.map((member) => member.PlaylistItemId)).toEqual(['entry-middle', 'entry-first']);
  expect(api.writes().map(({ method, path, query, body }) => ({ method, path, query, body }))).toEqual([
    { method: 'DELETE', path: `${playlistPath}/items`, query: '?EntryIds=entry-last', body: null },
    { method: 'POST', path: `${playlistPath}/items/entry-middle/move/0`, query: '', body: null },
  ]);
});

test('an unknown playlist save retains the draft and reloads without replaying the write', async ({ page, api }) => {
  api.handlers.set(`POST ${playlistPath}`, async (route, request) => {
    api.collection = { ...api.collection, ...request.postDataJSON() as CollectionPatch };
    // Model a committed write whose response never reached the browser.
    await route.abort('failed');
  });
  await openPlaylist(page);
  const dialog = page.getByRole('dialog', { name: 'Manage playlist', exact: true });
  const name = dialog.getByRole('textbox', { name: /^Playlist name/ });
  await name.fill('Saved after response loss');
  await dialog.getByRole('button', { name: 'Save details', exact: true }).click();
  await expect(dialog.getByText(/The result could not be confirmed or the saved details changed/)).toBeVisible();
  await expect(name).toHaveValue('Saved after response loss');
  await expect(name).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Save details', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0].body).toEqual({ Name: 'Saved after response loss' });

  const readsBeforeReload = api.reads(playlistPath).length;
  await dialog.getByRole('button', { name: 'Reload latest playlist', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Discard unsaved changes?', exact: true });
  await confirmation.getByRole('button', { name: 'Keep editing', exact: true }).click();
  await expect(name).toHaveValue('Saved after response loss');
  expect(api.reads(playlistPath)).toHaveLength(readsBeforeReload);
  await dialog.getByRole('button', { name: 'Reload latest playlist', exact: true }).click();
  await confirmation.getByRole('button', { name: 'Discard changes', exact: true }).click();
  await expect(name).toBeEnabled();
  await expect(dialog.getByRole('heading', { name: 'Saved after response loss', exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Save details', exact: true })).toBeDisabled();
  expect(api.reads(playlistPath).length).toBeGreaterThan(readsBeforeReload);
  expect(api.writes()).toHaveLength(1);
});

test('selected metadata and image candidates send their own identifiers and the latest revision', async ({ page, api }) => {
  const matches: MetadataCandidate[] = [
    { Provider: 'tmdb', Id: 'movie-first', Type: 'Movie', Language: 'en', Name: 'First match', OriginalTitle: '', Year: 2019, Overview: 'First candidate.', Score: 1 },
    { Provider: 'tmdb', Id: 'movie-selected', Type: 'Movie', Language: 'fr', Name: 'Selected match', OriginalTitle: '', Year: 2020, Overview: 'Selected candidate.', Score: 0.8 },
  ];
  const images: ImageCandidate[] = [
    { Provider: 'tmdb', Id: 'movie-selected', Type: 'Movie', Language: 'en', ImageId: 'image-first', ImageType: 'Primary', Width: 500, Height: 750, PreviewUrl: '' },
    { Provider: 'tmdb', Id: 'movie-selected', Type: 'Movie', Language: 'fr', ImageId: 'image-selected', ImageType: 'Backdrop', Width: 1920, Height: 1080, PreviewUrl: '' },
  ];
  api.handlers.set(`POST ${itemPath}/providers/search`, async (route) => { await json(route, { Items: matches }); });
  api.handlers.set(`POST ${itemPath}/providers/apply`, async (route) => {
    api.item.Revision = '9007199254740994';
    await json(route, api.item);
  });
  api.handlers.set(`POST ${itemPath}/providers/images`, async (route) => { await json(route, { Items: images }); });
  api.handlers.set(`POST ${itemPath}/providers/image`, async (route) => {
    api.item.Revision = '9007199254740995';
    await json(route, {});
  });
  await openSources(page);
  const dialog = page.getByRole('dialog', { name: 'Online sources · Catalog feature', exact: true });
  await dialog.getByRole('button', { name: 'Search metadata', exact: true }).click();
  await expect(dialog.getByRole('heading', { name: 'Selected match (2020)', exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Apply metadata', exact: true }).nth(1).click();
  await expect(dialog.getByText('Metadata from Selected match applied.', { exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Find images', exact: true }).nth(1)).toBeEnabled();
  await dialog.getByRole('button', { name: 'Find images', exact: true }).nth(1).click();
  await expect(dialog.getByRole('button', { name: 'Use image', exact: true })).toHaveCount(2);
  await dialog.getByRole('spinbutton', { name: 'Image index', exact: true }).fill('2');
  await dialog.getByRole('button', { name: 'Use image', exact: true }).nth(1).click();
  await expect(dialog.getByText('Backdrop image saved.', { exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeEnabled();
  expect(api.writes().map(({ path, body }) => ({ path, body }))).toEqual([
    { path: `${itemPath}/providers/search`, body: { Provider: 'tmdb', Name: 'Catalog feature', Year: 2020, Language: 'en' } },
    { path: `${itemPath}/providers/apply`, body: { Provider: 'tmdb', Id: 'movie-selected', Language: 'fr', Revision: '9007199254740993' } },
    { path: `${itemPath}/providers/images`, body: { Provider: 'tmdb', Id: 'movie-selected', Language: 'fr' } },
    { path: `${itemPath}/providers/image`, body: { Provider: 'tmdb', Id: 'movie-selected', Language: 'fr', ImageId: 'image-selected', ImageType: 'Backdrop', ImageIndex: 2, Revision: '9007199254740994' } },
  ]);
  expect(api.reads(`${itemPath}/metadata`).length).toBeGreaterThanOrEqual(3);
  expect(api.reads(`${itemPath}/providers/provenance`).length).toBeGreaterThanOrEqual(3);
});

test('subtitle selection downloads the chosen provider file and normalized search languages', async ({ page, api }) => {
  const candidates: SubtitleCandidate[] = [
    { Provider: 'opensubtitles', Id: 'subtitle-first', FileId: 101, Language: 'en', Name: 'English subtitle', HearingImpaired: false, DownloadCount: 50, MovieHashMatch: false },
    { Provider: 'opensubtitles', Id: 'subtitle-selected', FileId: 202, Language: 'fr', Name: 'French accessible subtitle', HearingImpaired: true, DownloadCount: 25, MovieHashMatch: true },
  ];
  api.handlers.set(`POST ${itemPath}/providers/subtitles/search`, async (route) => { await json(route, { Items: candidates }); });
  api.handlers.set(`POST ${itemPath}/providers/subtitles/download`, async (route) => { await json(route, {}); });
  await openSources(page);
  const dialog = page.getByRole('dialog', { name: 'Online sources · Catalog feature', exact: true });
  await dialog.getByRole('tab', { name: 'Subtitles', exact: true }).click();
  await dialog.getByRole('textbox', { name: 'Subtitle languages', exact: true }).fill('en, fr, en');
  await dialog.getByRole('switch', { name: 'Hearing impaired subtitles', exact: true }).check();
  await dialog.getByRole('button', { name: 'Search subtitles', exact: true }).click();
  await expect(dialog.getByText('French accessible subtitle', { exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Download subtitle', exact: true }).nth(1).click();
  await expect(dialog.getByText('Subtitle downloaded: French accessible subtitle.', { exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeEnabled();
  expect(api.writes().map(({ path, body }) => ({ path, body }))).toEqual([
    { path: `${itemPath}/providers/subtitles/search`, body: { Languages: ['en', 'fr'], HearingImpaired: true } },
    { path: `${itemPath}/providers/subtitles/download`, body: { Provider: 'opensubtitles', Id: 'subtitle-selected', FileId: 202, Language: 'fr', Name: 'French accessible subtitle' } },
  ]);
});

test('management settings retain every draft field after a rejected save and submit the complete settings', async ({ page, api }) => {
  const expectedManagement: ManagementSettings = {
    Metadata: { EnableInternetProviders: false, PreferredMetadataLanguage: 'fr', MetadataCountryCode: 'FR' },
    Subtitles: { DownloadLanguages: ['fr', 'en'], DownloadMovieSubtitles: true, DownloadEpisodeSubtitles: true },
    Tasks: { MaxConcurrent: 4, CacheRetentionDays: 60, CacheMaxEntries: 2500 },
  };
  let attempts = 0;
  api.handlers.set('PUT /admin/v1/settings', async (route, request) => {
    attempts += 1;
    if (attempts === 1) {
      await json(route, { Error: { Code: 'validation_failed', Message: 'The management settings could not be saved.', Fields: { 'Management.Tasks.MaxConcurrent': 'Review task concurrency before saving again.' } }, RequestId: 'synthetic-settings-request' }, 422);
      return;
    }
    const input = request.postDataJSON() as SettingsUpdateInput;
    api.settings = { ...api.settings, Revision: '2', Management: input.Management };
    await json(route, api.settings);
  });
  await page.goto('/admin/settings');
  const panel = page.getByRole('region', { name: 'Metadata, subtitles, and tasks', exact: true });
  await expect(panel).toBeVisible();
  await panel.getByRole('switch', { name: 'Enable internet providers', exact: true }).uncheck();
  await panel.getByRole('textbox', { name: 'Preferred metadata language', exact: true }).fill('fr');
  await panel.getByRole('textbox', { name: 'Metadata country', exact: true }).fill('FR');
  await panel.getByRole('textbox', { name: 'Subtitle download languages', exact: true }).fill('fr\n\nen\nfr');
  await panel.getByRole('switch', { name: 'Download movie subtitles', exact: true }).check();
  await panel.getByRole('switch', { name: 'Download episode subtitles', exact: true }).check();
  await panel.getByRole('textbox', { name: 'Concurrent provider and cache workers', exact: true }).fill('4');
  await panel.getByRole('textbox', { name: 'Cache retention (days)', exact: true }).fill('60');
  await panel.getByRole('textbox', { name: 'Maximum cache entries', exact: true }).fill('2500');
  const save = page.getByRole('button', { name: 'Save settings', exact: true });
  await save.click();
  await expect(panel.getByText('Review task concurrency before saving again.', { exact: true })).toBeVisible();
  await expect(panel.getByRole('switch', { name: 'Enable internet providers', exact: true })).not.toBeChecked();
  await expect(panel.getByRole('textbox', { name: 'Preferred metadata language', exact: true })).toHaveValue('fr');
  await expect(panel.getByRole('textbox', { name: 'Metadata country', exact: true })).toHaveValue('FR');
  await expect(panel.getByRole('textbox', { name: 'Subtitle download languages', exact: true })).toHaveValue('fr\n\nen\nfr');
  await expect(panel.getByRole('switch', { name: 'Download movie subtitles', exact: true })).toBeChecked();
  await expect(panel.getByRole('switch', { name: 'Download episode subtitles', exact: true })).toBeChecked();
  await expect(panel.getByRole('textbox', { name: 'Concurrent provider and cache workers', exact: true })).toHaveValue('4');
  await expect(panel.getByRole('textbox', { name: 'Cache retention (days)', exact: true })).toHaveValue('60');
  await expect(panel.getByRole('textbox', { name: 'Maximum cache entries', exact: true })).toHaveValue('2500');
  await expect(save).toBeEnabled();
  expect(api.writes()).toHaveLength(1);
  const expectedInput: SettingsUpdateInput = {
    Revision: '1', Overrides: api.settings.Overrides, ServerNameMode: 'deployment', Encoding: { TranscodingMaxWidth: 0 }, Management: expectedManagement,
  };
  expect(api.writes()[0].body).toEqual(expectedInput);
  await save.click();
  await expect(page.getByText('Settings saved.', { exact: true })).toBeVisible();
  await expect(page.getByText('No unsaved changes', { exact: true })).toBeVisible();
  await expect(save).toBeDisabled();
  await expect(panel.getByRole('textbox', { name: 'Subtitle download languages', exact: true })).toHaveValue('fr\nen');
  expect(api.writes().map((request) => request.body)).toEqual([expectedInput, expectedInput]);
});

test('legacy user policies preserve safe defaults and expanded drafts until valid values can be saved', async ({ page, api }) => {
  const userPath = `/admin/v1/users/${administrator.Id}`;
  const legacyPolicy = {
    EnableAllFolders: false, EnabledFolders: [library.Id], EnableMediaPlayback: true,
    EnablePlaybackRemuxing: false, EnableAudioPlaybackTranscoding: true, EnableVideoPlaybackTranscoding: false,
  };
  let user = { ...administrator, Revision: '1', Policy: legacyPolicy };
  api.handlers.set(`GET ${userPath}`, async (route) => { await json(route, { User: user }); });
  api.handlers.set(`PUT ${userPath}`, async (route, request) => {
    const input = request.postDataJSON() as UpdateUserInput;
    user = { ...administrator, ...input, Revision: '2' };
    await json(route, { User: user, CurrentSessionRevoked: false });
  });
  await page.goto('/admin/users');
  await page.getByRole('button', { name: 'Manage Synthetic administrator', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Manage user', exact: true });
  const save = dialog.getByRole('button', { name: 'Save changes', exact: true });
  const rating = dialog.getByLabel('Maximum parental rating', { exact: true });
  const streamLimit = dialog.getByLabel('Simultaneous stream limit', { exact: true });
  const remoteQuality = dialog.getByLabel('Automatic remote quality (bits per second)', { exact: true });
  await expect(rating).toHaveValue('');
  await expect(dialog.getByRole('switch', { name: 'Control shared devices', exact: true })).not.toBeChecked();
  await expect(dialog.getByRole('switch', { name: 'Allow remote access', exact: true })).toBeChecked();
  await expect(save).toBeDisabled();

  await rating.fill('16');
  await dialog.getByRole('switch', { name: 'Allow all devices', exact: true }).uncheck();
  const devices = dialog.getByRole('textbox', { name: 'Allowed device identifiers', exact: true });
  const tags = dialog.getByRole('textbox', { name: 'Included tags', exact: true });
  await devices.fill(' living-room \n\nbedroom\nliving-room ');
  await tags.fill(' family \n\nfavorites\nfamily ');
  await remoteQuality.fill('12000000');
  await dialog.getByRole('group', { name: 'Feature access', exact: true }).getByRole('checkbox', { name: 'Downloads', exact: true }).uncheck();
  await dialog.getByRole('button', { name: 'Add access interval', exact: true }).click();
  await dialog.getByRole('combobox', { name: 'Access day 1', exact: true }).click();
  await page.getByRole('option', { name: 'Weekday', exact: true }).click();
  await dialog.getByLabel('Start hour 1', { exact: true }).fill('8.5');
  await dialog.getByLabel('End hour 1', { exact: true }).fill('18.25');
  await streamLimit.fill('');
  await expect(streamLimit).toHaveValue('');
  await expect(save).toBeDisabled();
  await expect(rating).toHaveValue('16');
  await expect(devices).toHaveValue(' living-room \n\nbedroom\nliving-room ');
  await expect(tags).toHaveValue(' family \n\nfavorites\nfamily ');
  await expect(remoteQuality).toHaveValue('12000000');
  await expect(dialog.getByRole('group', { name: 'Feature access', exact: true }).getByRole('checkbox', { name: 'Downloads', exact: true })).not.toBeChecked();
  await expect(dialog.getByLabel('Start hour 1', { exact: true })).toHaveValue('8.5');
  await expect(dialog.getByLabel('End hour 1', { exact: true })).toHaveValue('18.25');
  expect(api.writes()).toEqual([]);

  await streamLimit.fill('2');
  await expect(save).toBeEnabled();
  await save.click();
  await expect(dialog.getByText('User Synthetic administrator updated.', { exact: true })).toBeVisible();
  await expect(dialog.getByText('All changes saved', { exact: true })).toBeVisible();
  await expect(save).toBeDisabled();
  await expect(devices).toHaveValue('bedroom\nliving-room');
  await expect(tags).toHaveValue('family\nfavorites');
  const expectedPolicy: UserPolicy = {
    ...legacyPolicy,
    IsHidden: false, IsHiddenRemotely: false, IsHiddenFromUnusedDevices: false,
    MaxParentalRating: 16, AllowTagOrRating: false, IsTagBlockingModeInclusive: false,
    BlockedTags: [], IncludeTags: ['family', 'favorites'], BlockUnratedItems: [], EnableUserPreferenceAccess: true,
    AccessSchedules: [{ DayOfWeek: 'Weekday', StartHour: 8.5, EndHour: 18.25 }],
    EnableRemoteControlOfOtherUsers: false, EnableSharedDeviceControl: false, EnableRemoteAccess: true,
    AutoRemoteQuality: 12000000, EnableContentDeletion: false, RestrictedFeatures: ['downloads'], EnableContentDeletionFromFolders: [],
    EnableContentDownloading: true, EnableSubtitleDownloading: true, EnableSubtitleManagement: false,
    RemoteClientBitrateLimit: 0, ExcludedSubFolders: [], SimultaneousStreamLimit: 2,
    EnabledDevices: ['bedroom', 'living-room'], EnableAllDevices: false,
  };
  expect(api.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([{
    method: 'PUT', path: userPath,
    body: { Revision: '1', Name: administrator.Name, IsAdministrator: true, IsDisabled: false, Policy: expectedPolicy },
  }]);
  expect(user.Revision).toBe('2');
});

test('an unavailable feature catalog preserves unlisted restrictions while other policy changes can be saved', async ({ page, api }) => {
  const userPath = `/admin/v1/users/${administrator.Id}`;
  let user = {
    ...administrator, Revision: '1',
    Policy: {
      EnableAllFolders: true, EnabledFolders: [] as string[], EnableMediaPlayback: true,
      EnablePlaybackRemuxing: true, EnableAudioPlaybackTranscoding: true, EnableVideoPlaybackTranscoding: true,
      RestrictedFeatures: ['legacy_feature'],
    },
  };
  api.handlers.set(`GET ${userPath}`, async (route) => { await json(route, { User: user }); });
  api.handlers.set('GET /admin/v1/features', async (route) => {
    await json(route, { Error: { Code: 'feature_catalog_unavailable', Message: 'Feature catalog temporarily unavailable.' }, RequestId: 'synthetic-feature-request' }, 503);
  });
  api.handlers.set(`PUT ${userPath}`, async (route, request) => {
    const input = request.postDataJSON() as UpdateUserInput;
    user = { ...administrator, ...input, Revision: '2' };
    await json(route, { User: user, CurrentSessionRevoked: false });
  });
  await page.goto('/admin/users');
  await page.getByRole('button', { name: 'Manage Synthetic administrator', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Manage user', exact: true });
  await expect(dialog.getByText('Feature choices are unavailable. Existing restrictions are retained, and other account settings can still be edited.', { exact: true })).toBeVisible();
  const features = dialog.getByRole('group', { name: 'Feature access', exact: true });
  const unlisted = features.getByRole('checkbox', { name: 'Unlisted feature (legacy_feature)', exact: true });
  await expect(features.getByRole('checkbox')).toHaveCount(1);
  await expect(unlisted).toBeDisabled();
  await expect(unlisted).not.toBeChecked();
  await expect(dialog.getByLabel('Maximum parental rating', { exact: true })).toHaveValue('');
  await dialog.getByLabel('Simultaneous stream limit', { exact: true }).fill('3');
  const save = dialog.getByRole('button', { name: 'Save changes', exact: true });
  await expect(save).toBeEnabled();
  await save.click();
  await expect(dialog.getByText('User Synthetic administrator updated.', { exact: true })).toBeVisible();
  await expect(dialog.getByText('All changes saved', { exact: true })).toBeVisible();
  await expect(save).toBeDisabled();
  await expect(unlisted).toBeDisabled();
  await expect(unlisted).not.toBeChecked();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0]).toMatchObject({
    method: 'PUT', path: userPath,
    body: {
      Revision: '1', Name: administrator.Name, IsAdministrator: true, IsDisabled: false,
      Policy: { RestrictedFeatures: ['legacy_feature'], SimultaneousStreamLimit: 3, MaxParentalRating: null, AutoRemoteQuality: 0, EnableSharedDeviceControl: false },
    },
  });
  expect(user.Policy.RestrictedFeatures).toEqual(['legacy_feature']);
  expect(user.Revision).toBe('2');
  expect(api.reads('/admin/v1/features').length).toBeGreaterThan(0);
});
