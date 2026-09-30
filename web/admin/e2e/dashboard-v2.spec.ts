import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Page, Request, Route } from '@playwright/test';
import type { ActivityEntry, ServerSettings, SettingsUpdateInput } from '../src/api';
import type { RuntimeSettings } from '../src/runtimeSettings';
import type { SystemStatus } from '../src/systemStatusApi';
import type { AnalysisItem } from '../src/mediaAnalysis';

// These tests exercise the complete dashboard against explicit synthetic API
// responses. Only frontend assets reach the server; no real mutation is sent.
const stamp = '2026-09-30T08:00:00Z';
const csrf = 'synthetic-dashboard-v2-csrf';
const administrator = { Id: 'a'.repeat(32), Name: 'Dashboard administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: stamp };
const library = { Id: 'b'.repeat(32), Name: 'Feature films', CollectionType: 'movies', Paths: ['D:\\Media\\Movies'], CreatedAt: stamp, LastScanAt: stamp,
  LibraryOptions: { EnableLocalMetadata: true, EnableLocalImages: true, EnableEmbeddedArtwork: true, EnableIntroDetection: false } };
const seriesLibrary = { ...library, Id: 'c'.repeat(32), Name: 'TV series', CollectionType: 'tvshows', Paths: ['D:\\Media\\Series'], LibraryOptions: { ...library.LibraryOptions, EnableIntroDetection: true } };
const analysisItem: AnalysisItem = {
  Id: 'episode-one', Name: 'Opening episode', Type: 'Episode', LibraryId: seriesLibrary.Id, MediaSourceId: 'source-one', SourceRevision: 'source-revision-one',
  Detection: { ItemId: 'episode-one', Revision: '1', ManualRevision: '0', SourceRevision: 'source-revision-one', Status: 'qualified', Reasons: [],
    Candidate: null, IntroSkipperCandidate: null, Effective: { StartTicks: 100_000_000, EndTicks: 400_000_000, Provenance: 'Detected' }, Suppressed: false, UpdatedAt: stamp },
  Previews: [{ Width: 320, Height: 180, Size: 32768, FrameCount: 12, Status: 'ready', FailureCode: '', UpdatedAt: stamp }],
};

function runtime(): RuntimeSettings {
  const defaults = {
    Network: { BindHost: '127.0.0.1', HttpPort: 8096 }, Hardware: { Decode: 'software', Encode: 'software', DeviceId: '' }, Threads: 2,
    H264: { Preset: 'veryfast' as const, RateControl: 'bitrate' as const, CRF: 23 }, HEVC: { Preset: 'fast' as const, RateControl: 'bitrate' as const, CRF: 28 },
    SoftwareToneMapping: true, VulkanToneMapping: false,
  };
  const { Network: _network, ...effective } = defaults;
  return {
    Defaults: defaults, Overrides: { Network: null, Hardware: null, Threads: null, H264: null, HEVC: null, SoftwareToneMapping: null, VulkanToneMapping: null }, Effective: effective,
    Sources: { Network: { BindHost: 'deployment', HttpPort: 'deployment' }, Hardware: 'deployment', Threads: 'deployment', H264: 'deployment', HEVC: 'deployment', SoftwareToneMapping: 'deployment', VulkanToneMapping: 'deployment' },
    Network: { Desired: { ...defaults.Network }, Active: { Configured: { ...defaults.Network }, BoundHost: '127.0.0.1', HttpPort: 8096, Revision: '1' }, RestartRequired: false, ReconnectURL: '' },
    Hardware: { Available: true, Code: '', Devices: [{ DeviceId: 'amd-device', Label: 'AMD Radeon GPU', Available: true, Code: '' }] },
    Effects: { Network: 'restart', Hardware: 'next_admission', Threads: 'next_admission', H264: 'next_admission', HEVC: 'next_admission', SoftwareToneMapping: 'next_admission', VulkanToneMapping: 'next_admission' },
    Applicability: { H264: 'software_h264_output', HEVC: 'software_hevc_output', SoftwareToneMapping: 'software_filter', VulkanToneMapping: 'vulkan_filter' },
  };
}

function settings(): ServerSettings {
  const defaults = { ServerName: 'Goby living room', MaxBitrate: 20_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2 };
  const management = { Metadata: { EnableInternetProviders: true, PreferredMetadataLanguage: 'en', MetadataCountryCode: 'US' }, Subtitles: { DownloadLanguages: ['en'], DownloadMovieSubtitles: false, DownloadEpisodeSubtitles: false }, Tasks: { MaxConcurrent: 2, CacheRetentionDays: 30, CacheMaxEntries: 1000 } };
  return {
    Revision: '9007199254740993', Defaults: defaults, Effective: { ...defaults },
    Overrides: { ServerName: null, MaxBitrate: null, MaxWidth: null, MaxHeight: null, MaxAudioChannels: null },
    Sources: { ServerName: 'deployment', MaxBitrate: 'deployment', MaxWidth: 'deployment', MaxHeight: 'deployment', MaxAudioChannels: 'deployment' },
    UpdatedAt: stamp, ServerNameMode: 'deployment', Encoding: { TranscodingMaxWidth: 0 },
    Deployment: { HostName: 'dashboard-server', TranscodingEnabled: true, HardwareDecoder: 'software', HardwareEncoder: 'software', Threads: 2, MaxJobs: 4, MaxUserJobs: 2, MaxSessionJobs: 1 },
    Runtime: runtime(), Management: structuredClone(management), ManagementDefaults: structuredClone(management),
    Sorting: { SortRemoveWords: ['the', 'a', 'an'] }, SortingDefaults: { SortRemoveWords: [] },
  };
}

function status(): SystemStatus {
  return {
    Timestamp: stamp, UptimeSeconds: 184200, Host: { OS: 'windows', Architecture: 'amd64', CPUCount: 8 },
    CPU: { UsagePercent: 28, Load1: 0.6, Load5: 0.8, Load15: 1.2 },
    Memory: { TotalBytes: 16 * 2 ** 30, UsedBytes: 6 * 2 ** 30, CachedBytes: 2 * 2 ** 30, SwapUsedBytes: 0 },
    Storage: { TotalBytes: 2 ** 40, UsedBytes: 420 * 2 ** 30, Complete: true, Volumes: [{ Path: 'D:\\Media', TotalBytes: 2 ** 40, UsedBytes: 420 * 2 ** 30, Available: true }] },
    Transcoding: { Available: true, Active: 2, Limit: 4, HardwareActive: 1, SoftwareActive: 1 },
  };
}

const activities: ActivityEntry[] = [
  { Id: '1', Date: stamp, Action: 'session.login', Severity: 'Info', Source: 'native', Actor: { Kind: 'user', Id: administrator.Id, Name: administrator.Name }, Resource: { Kind: 'session', Id: 'session-one' }, Revision: null, Count: '1', State: 'completed', ChangedFields: [], Name: 'Administrator signed in', Overview: 'A dashboard session started.' },
  { Id: '2', Date: stamp, Action: 'scan.finished', Severity: 'Info', Source: 'system', Actor: { Kind: 'system', Id: null, Name: null }, Resource: { Kind: 'library', Id: library.Id }, Revision: null, Count: '12', State: 'completed', ChangedFields: [], Name: 'Feature films scan completed', Overview: 'Twelve media items were added.' },
];

async function json(route: Route, value: unknown, statusCode = 200): Promise<void> {
  await route.fulfill({ status: statusCode, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) });
}
interface Captured { method: string; path: string; body: unknown; csrf?: string }
class DashboardAPI {
  settings = settings(); status = status(); requests: Captured[] = []; unexpected: string[] = [];
  handlers = new Map<string, (route: Route, request: Request) => Promise<void>>();
  reads(path: string): Captured[] { return this.requests.filter((value) => value.method === 'GET' && value.path === path); }
  writes(): Captured[] { return this.requests.filter((value) => value.method !== 'GET'); }
  apply(input: SettingsUpdateInput): void {
    this.settings.Revision = (BigInt(input.Revision) + 1n).toString();
    this.settings.Overrides = input.Overrides; this.settings.ServerNameMode = input.ServerNameMode; this.settings.Encoding = input.Encoding;
    for (const field of ['MaxBitrate', 'MaxWidth', 'MaxHeight', 'MaxAudioChannels'] as const) {
      this.settings.Effective[field] = input.Overrides[field] ?? this.settings.Defaults[field];
      this.settings.Sources[field] = input.Overrides[field] === null ? 'deployment' : 'database';
    }
    this.settings.Effective.ServerName = input.ServerNameMode === 'custom' ? input.Overrides.ServerName! : input.ServerNameMode === 'deployment' ? this.settings.Defaults.ServerName : this.settings.Deployment.HostName;
    this.settings.Sources.ServerName = input.ServerNameMode === 'deployment' ? 'deployment' : 'database';
    if (input.Management) this.settings.Management = input.Management;
    if (input.Sorting) this.settings.Sorting = input.Sorting;
  }
  async install(context: BrowserContext): Promise<void> {
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const url = new URL(request.url()); const path = url.pathname; const method = request.method();
      const captured = { method, path, body: request.postData() ? request.postDataJSON() as unknown : null, csrf: request.headers()['x-csrf-token'] };
      this.requests.push(captured);
      const handler = this.handlers.get(`${method} ${path}`); if (handler) return handler(route, request);
      const paged = (items: unknown[]) => ({ Items: items, TotalRecordCount: items.length, StartIndex: Number(url.searchParams.get('StartIndex') ?? 0), Limit: Number(url.searchParams.get('Limit') ?? 25) });
      if (method === 'GET') {
        if (path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
        if (path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrf });
        if (path === '/admin/v1/overview') return json(route, { Server: { Id: 'd'.repeat(32), Name: 'Goby living room', Version: '0.1.0' }, Database: { Status: 'ok' }, Counts: { Users: 2, Libraries: 2, Items: 1240, ActiveSessions: 3 }, Runtime: { GoVersion: 'go1.26.1' }, Features: { LibraryManagement: true, Playback: true, Transcoding: true, ApplicationKeys: true } });
        if (path === '/admin/v1/system/status') return json(route, this.status);
        if (path === '/admin/v1/activity') return json(route, { ...paged(activities), RetentionDays: 30 });
        if (path === '/admin/v1/logs') return json(route, { ...paged([]), Status: { Healthy: true, Degraded: false, Closed: false, MaxFileBytes: '1048576', MaxFiles: 10, RetentionDays: 30, MinFreeBytes: '0', Format: 'jsonl' } });
        if (path === '/admin/v1/settings') return json(route, this.settings);
        if (path === '/admin/v1/users') return json(route, { Items: [administrator, { ...administrator, Id: 'c'.repeat(32), Name: 'Family viewer', IsAdministrator: false }], TotalRecordCount: 2 });
        if (path === '/admin/v1/devices') return json(route, paged([{ Id: '101', Revision: '1', ReportedDeviceId: 'living-room-tv', Name: 'Living room TV', ReportedName: 'Android TV', CustomName: 'Living room TV', AppName: 'Goby TV', AppVersion: '1.0', LastUserId: administrator.Id, LastUserName: administrator.Name, CreatedAt: stamp, LastSeenAt: stamp, IpAddress: '192.0.2.4', ActiveLoginCount: 1 }]));
        if (path === '/admin/v1/sessions') return json(route, paged([{ Id: 'f'.repeat(32), UserId: administrator.Id, UserName: administrator.Name, UserIsAdministrator: true, UserIsDisabled: false, Kind: 'admin', Client: 'Goby dashboard', DeviceId: '', DeviceName: 'Desktop browser', ApplicationVersion: '0.1.0', CreatedAt: stamp, LastSeenAt: stamp, ExpiresAt: '2026-10-01T08:00:00Z', RevokedAt: null, Status: 'active', IsCurrent: true }]));
        if (path === '/admin/v1/api-keys') return json(route, paged([{ Id: '201', AppName: 'Home automation', CreatedAt: stamp, LastUsedAt: stamp, RevokedAt: null, CreatedBy: administrator.Id, IPAddress: '192.0.2.5', Status: 'active' }]));
        if (path === '/admin/v1/libraries') return json(route, { Items: [library, seriesLibrary], TotalRecordCount: 2 });
        if (path === '/admin/v1/storage/roots') return json(route, { Configured: true, Items: [{ Path: 'D:\\Media', Available: true }] });
        if (path === `/admin/v1/libraries/${library.Id}/items`) return json(route, { Library: library, ...paged([{ Id: 'film-one', LibraryId: library.Id, ParentId: '', ParentName: '', Name: 'The feature film', Type: 'Movie', Path: 'D:\\Media\\Movies\\Feature.mkv', IsFolder: false, ProductionYear: 2025, IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0 }]) });
        if (path === '/admin/v1/entities') return json(route, paged([{ Id: 'person-one', Name: 'Alex Morgan', Type: 'Person' }]));
        if (path === '/admin/v1/playlists') return json(route, { Items: [{ Id: 'playlist-one', Name: 'Weekend favorites', Type: 'Playlist', IsFolder: true, ParentId: '', OwnerId: administrator.Id, MediaType: 'Video', IsPublic: false, IsLocked: false, ChildCount: 12, Shares: [] }], TotalRecordCount: 1 });
        if (path === '/admin/v1/collections') return json(route, { Items: [], TotalRecordCount: 0 });
        if (path === '/admin/v1/providers') return json(route, { Enabled: true, Items: [{ Id: 'tmdb', Name: 'The Movie Database', Configured: true, Capabilities: ['metadata', 'images'], Attribution: 'Movie and television metadata', Website: 'https://www.themoviedb.org/' }, { Id: 'opensubtitles', Name: 'OpenSubtitles', Configured: false, Capabilities: ['subtitles'], Attribution: 'Subtitles for your media', Website: 'https://www.opensubtitles.com/' }] });
        if (path === '/admin/v1/tasks') return json(route, { Items: [{ Id: 'scan-library', Key: 'library.scan', Name: 'Scan media libraries', Description: 'Discover new files and update library metadata.', Category: 'Library', IsHidden: false, Enabled: true, Revision: '1', ScheduleTimezone: 'UTC', Triggers: [], CurrentRun: null, LastRun: null, NextRunAt: null }], TotalRecordCount: 1 });
        if (path === '/admin/v1/jobs') return json(route, { Items: [], TotalRecordCount: 0 });
        if (path === '/admin/v1/notifications') return json(route, { Revision: '1', Enabled: false, Endpoint: '', AllowedNetworks: [], HasReceiverCredential: false, SupportedEvents: ['PlaybackStart', 'PlaybackStopped', 'LibraryChanged'], PendingCount: 0 });
        if (path === '/admin/v1/media-diagnostics') return json(route, { InstanceId: 'a'.repeat(32), StartToken: '', StartTokenExpiresAt: stamp, Available: false, UnavailableReason: 'No diagnostic fixture configured', HardwareConfigured: false, RetentionSeconds: 1800, MaxRetainedRuns: 32, Items: [] });
        if (path === '/admin/v1/backups/status') return json(route, { Available: true, UnavailableReason: '', RestoreAvailable: true, RestoreUnavailableReason: '', Busy: false, ActiveOperationId: '', GenerationRevision: '1', Limits: { MaxBackupBytes: '1073741824', MaxStoredBytes: '4294967296', MaxBackups: 10, MinPassphraseBytes: 12, MaxPassphraseBytes: 1024 }, Storage: { Bytes: '0', Objects: 0 }, Rollback: { Available: false, MustReplace: false, CreatedAt: null, ServerName: '', Generation: '', UnavailableReason: '' } });
        if (path === '/admin/v1/backups' || path === '/admin/v1/backup-operations') return json(route, paged([]));
        if (path === '/admin/v1/media-analysis') {
          const profile = { AutoPublishIntros: true, PreviewIntervalSeconds: 10, PreviewQuality: 80, MaxSourceBytes: 128 * 2 ** 30, MaxItemRuntimeSeconds: 1200, FeatureCacheMaxBytes: 128 * 2 ** 20,
            IntroSkipper: { AnalysisPercent: 25, AnalysisLengthLimit: 10, MinimumIntroDuration: 15, MaximumIntroDuration: 120, MaximumFingerprintPointDifferences: 6, MaximumTimeSkip: 3.5, InvertedIndexShift: 2 } };
          return json(route, { Configuration: { Revision: '1', Profile: profile, Defaults: profile, UpdatedAt: stamp }, Runtime: { Configured: true, IntroAvailable: true, PreviewAvailable: true, Reasons: [], Cache: { ReadyEntries: 3, BuildingEntries: 0, PendingPublications: 0, Readers: 0, ReadyBytes: 4 * 2 ** 20, ReservedBytes: 0, ControlBytes: 1024, TotalBytes: 4 * 2 ** 20 + 1024, MaxBytes: 128 * 2 ** 20 } } });
        }
        if (path === '/admin/v1/media-analysis/items') return json(route, paged(!url.searchParams.get('LibraryId') || url.searchParams.get('LibraryId') === seriesLibrary.Id ? [analysisItem] : []));
        if (path === `/admin/v1/media-analysis/items/${analysisItem.Id}`) return json(route, analysisItem);
      }
      if (method === 'PUT' && path === '/admin/v1/settings') {
        const input = captured.body as SettingsUpdateInput;
        if (input.Revision !== this.settings.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'Settings changed. Reload before saving.' } }, 409);
        this.apply(input); return json(route, this.settings);
      }
      if (method === 'DELETE' && path === '/admin/v1/session') return route.fulfill({ status: 204 });
      this.unexpected.push(`${method} ${path}`);
      return json(route, { Error: { Code: 'unexpected_synthetic_request', Message: 'No dashboard fixture response was configured.' } }, 501);
    });
  }
}

const test = base.extend<{ api: DashboardAPI }>({ api: async ({ context, page }, use) => {
  const api = new DashboardAPI(); const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message)); await api.install(context); await use(api);
  expect(api.unexpected, 'All API calls must use an explicit fixture.').toEqual([]);
  expect(errors, 'Dashboard routes must not throw browser errors.').toEqual([]);
  expect(api.writes().every((value) => value.csrf === csrf), 'All writes must preserve CSRF protection.').toBe(true);
} });
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });

const destinations = [
  { path: '/admin/', legacy: '/admin/overview', group: 'Overview', tab: '', text: 'Goby living room' },
  { path: '/admin/media/libraries', legacy: '/admin/libraries', group: 'Media', tab: 'Libraries', text: 'Feature films' },
  { path: '/admin/media/artwork', legacy: '/admin/artwork', group: 'Media', tab: 'Catalog artwork', text: 'Alex Morgan' },
  { path: '/admin/media/collections', legacy: '/admin/collections', group: 'Media', tab: 'Playlists & collections', text: 'Weekend favorites' },
  { path: '/admin/media/analysis', legacy: '/admin/media-analysis', group: 'Media', tab: 'Media analysis', text: 'Analysis cache' },
  { path: '/admin/access/users', legacy: '/admin/users', group: 'Access', tab: 'Users', text: 'Family viewer' },
  { path: '/admin/access/devices', legacy: '/admin/devices', group: 'Access', tab: 'Devices', text: 'Living room TV' },
  { path: '/admin/access/sessions', legacy: '/admin/sessions', group: 'Access', tab: 'Sessions', text: 'Desktop browser' },
  { path: '/admin/access/api-keys', legacy: '/admin/api-keys', group: 'Access', tab: 'API keys', text: 'Home automation' },
  { path: '/admin/system/tasks', legacy: '/admin/tasks', group: 'System', tab: 'Tasks', text: 'Scan media libraries' },
  { path: '/admin/system/observability', legacy: '/admin/observability', group: 'System', tab: 'Activity & logs', text: 'Administrator signed in' },
  { path: '/admin/system/notifications', legacy: '/admin/notifications', group: 'System', tab: 'Notifications', text: 'Enable notifications' },
  { path: '/admin/system/backups', legacy: '/admin/backups', group: 'System', tab: 'Backups & recovery', text: 'Recovery readiness' },
  { path: '/admin/settings/general', legacy: '/admin/settings', group: 'Settings', tab: 'General', text: 'Server identity' },
  { path: '/admin/settings/transcode', legacy: '/admin/settings-transcode', group: 'Settings', tab: 'Transcode limits', text: 'Maximum bitrate' },
  { path: '/admin/settings/hardware', legacy: '/admin/settings-hardware', group: 'Settings', tab: 'Hardware acceleration', text: 'Hardware acceleration' },
  { path: '/admin/settings/metadata', legacy: '/admin/settings-metadata', group: 'Settings', tab: 'Metadata & subtitles', text: 'Metadata' },
  { path: '/admin/settings/providers', legacy: '/admin/providers', group: 'Settings', tab: 'Online providers', text: 'The Movie Database' },
  { path: `/admin/media/libraries/${library.Id}/items`, legacy: `/admin/libraries/${library.Id}/items`, group: 'Media', tab: 'Libraries', text: 'The feature film' },
];

async function ready(page: Page, destination: typeof destinations[number]): Promise<void> {
  await expect(page.getByRole('navigation', { name: 'Administration' }).getByRole('link', { name: destination.group, exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1);
  if (destination.tab) await expect(page.getByRole('tab', { name: destination.tab, exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('main').getByText(destination.text, { exact: false }).filter({ visible: true }).first()).toBeVisible();
  await expect(page.getByRole('main').locator('[aria-busy="true"]')).toHaveCount(0);
  if (destination.path === '/admin/media/analysis') await expect(page.getByRole('button', { name: `View analysis for ${analysisItem.Name}`, exact: true })).toBeVisible();
  await expect(page.getByRole('main')).not.toContainText(/invalid response|response is incomplete|No dashboard fixture|Loading settings/);
}

async function noOverflow(page: Page): Promise<void> {
  const dimensions = await page.evaluate(() => {
    const main = document.getElementById('main-content')!;
    return { document: document.documentElement.scrollWidth, width: window.innerWidth, main: main.scrollWidth, mainWidth: main.clientWidth };
  });
  expect(dimensions.document, 'The document must not scroll sideways.').toBeLessThanOrEqual(dimensions.width);
  expect(dimensions.main, 'Page content must fit its scroll container.').toBeLessThanOrEqual(dimensions.mainWidth + 1);
}

for (const viewport of [{ width: 1440, height: 834 }, { width: 375, height: 812 }]) {
  test.describe(`${viewport.width}px dashboard`, () => {
    test.use({ viewport });
    for (const destination of destinations) test(`renders ${destination.path} without horizontal overflow`, async ({ page, api }, testInfo) => {
      await page.goto(destination.path); await ready(page, destination); await noOverflow(page);
      const nav = await page.getByRole('navigation', { name: 'Administration' }).boundingBox();
      expect(nav).not.toBeNull();
      if (viewport.width < 840) { expect(nav!.y).toBeGreaterThan(viewport.height - 90); expect(nav!.width).toBe(viewport.width); }
      else { expect(nav!.x).toBe(0); expect(nav!.width).toBe(88); }
      await page.evaluate(() => document.fonts.ready);
      const name = destination.path === '/admin/' ? 'overview' : destination.path.includes('/items') ? 'media-items' : destination.path.slice('/admin/'.length).replaceAll('/', '-');
      await page.screenshot({ path: testInfo.outputPath(`${name}-${viewport.width === 1440 ? 'desktop' : 'mobile'}.png`), animations: 'disabled' });
      if (['/admin/settings/general', '/admin/settings/hardware'].includes(destination.path)) {
        await page.getByRole('main').evaluate((element) => { element.scrollTop = element.scrollHeight / 2; });
        await noOverflow(page);
        await page.screenshot({ path: testInfo.outputPath(`${name}-${viewport.width === 1440 ? 'desktop' : 'mobile'}-scrolled.png`), animations: 'disabled' });
      }
      expect(api.writes()).toEqual([]);
    });
  });
}

for (const viewport of [{ width: 1440, height: 834 }, { width: 375, height: 812 }]) test(`${viewport.width}px automatic intro results preserve the library and task workflows`, async ({ page, api }, testInfo) => {
  await page.setViewportSize(viewport);
  await page.goto('/admin/media/analysis'); await ready(page, destinations[4]);
  const automatic = page.getByRole('region', { name: 'Automatic intro detection', exact: true });
  const automation = page.getByRole('region', { name: 'Automatic media processing', exact: true });
  await expect(automatic).toContainText('If no intro is found, playback stays unchanged.');
  await expect(page.getByRole('button', { name: /Analyze library intros|Analyze selected episodes/ })).toHaveCount(0);
  await expect(page.getByRole('switch', { name: 'Automatically publish qualified detected intros', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: `View analysis for ${analysisItem.Name}`, exact: true }).click();
  const result = page.getByRole('dialog', { name: analysisItem.Name, exact: true });
  await expect(result).toContainText('Intro available');
  await expect(result).toContainText('0:10.00–0:40.00');
  const outputs = result.getByRole(viewport.width < 840 ? 'list' : 'table', { name: 'Preview outputs', exact: true });
  await expect(outputs).toContainText('320 × 180');
  await expect(outputs).toContainText('12');
  await expect(outputs).toContainText('32.0 KiB');
  await expect(outputs.getByText('ready', { exact: viewport.width < 840 })).toBeVisible();
  await expect(result.getByRole('button', { name: /Accept|Reject|Reset/ })).toHaveCount(0);
  await noOverflow(page);
  await page.screenshot({ path: testInfo.outputPath(`media-analysis-result-${viewport.width === 1440 ? 'desktop' : 'mobile'}.png`), animations: 'disabled' });
  await result.getByRole('button', { name: 'Close', exact: true }).click();
  await automation.getByRole('button', { name: 'Open library settings', exact: true }).click(); await ready(page, destinations[1]);
  await expect(page).toHaveURL(/\/admin\/media\/libraries$/);
  await page.getByRole('tab', { name: 'Media analysis', exact: true }).click(); await ready(page, destinations[4]);
  await automation.getByRole('button', { name: 'View background tasks', exact: true }).click(); await ready(page, destinations[9]);
  await expect(page).toHaveURL(/\/admin\/system\/tasks$/);
  expect(api.writes()).toEqual([]);
});

test('legacy routes retain their destination, query, fragment and active navigation', async ({ page, api }) => {
  test.setTimeout(120_000);
  for (const destination of destinations) {
    await page.goto(`${destination.legacy}?delivery=1#details`); await ready(page, destination);
    const url = new URL(page.url()); expect(url.pathname).toBe(destination.path); expect(url.search).toBe('?delivery=1'); expect(url.hash).toBe('#details');
  }
  await page.goto(`/admin/libraries/${library.Id}/items`);
  await expect(page).toHaveURL(new RegExp(`/admin/media/libraries/${library.Id}/items$`));
  await expect(page.getByRole('tab', { name: 'Libraries', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('main')).toContainText('The feature film');
  expect(api.writes()).toEqual([]);
});

test('navigation remembers the last tab per group across switches and reloads', async ({ page, api }) => {
  await page.goto('/admin/media/artwork'); await ready(page, destinations[2]);
  const nav = page.getByRole('navigation', { name: 'Administration' });
  await nav.getByRole('link', { name: 'Access', exact: true }).click();
  await page.getByRole('tab', { name: 'Devices', exact: true }).click(); await ready(page, destinations[6]);
  await nav.getByRole('link', { name: 'Media', exact: true }).click(); await ready(page, destinations[2]);
  await nav.getByRole('link', { name: 'Access', exact: true }).click(); await ready(page, destinations[6]);
  await page.reload(); await ready(page, destinations[6]);
  await nav.getByRole('link', { name: 'Media', exact: true }).click(); await ready(page, destinations[2]);
  await page.goBack(); await ready(page, destinations[6]);
  expect(api.writes()).toEqual([]);
});

test('invalid saved group tabs are ignored and unknown URLs resolve to overview', async ({ page, api }) => {
  await page.addInitScript(() => sessionStorage.setItem('goby.dashboard.tabs', JSON.stringify({ media: 'users', access: '__proto__', settings: 'not-a-page' })));
  await page.goto('/admin/not-a-page'); await ready(page, destinations[0]); await expect(page).toHaveURL(/\/admin\/$/);
  await page.getByRole('navigation', { name: 'Administration' }).getByRole('link', { name: 'Media', exact: true }).click(); await ready(page, destinations[1]);
  expect(api.writes()).toEqual([]);
});

test('overview count tiles and recent activity open the corresponding management pages', async ({ page, api }) => {
  for (const [name, destination] of [['View users', 5], ['View libraries', 1], ['View media items', 1], ['View active sessions', 7], ['View all', 10]] as const) {
    await page.goto('/admin/'); await ready(page, destinations[0]);
    await page.getByRole('button', { name, exact: true }).click(); await ready(page, destinations[destination]);
  }
  expect(api.writes()).toEqual([]);
});

test('overview retains last metrics on refresh failure, identifies stale data and recovers', async ({ page, api }) => {
  await page.goto('/admin/'); await ready(page, destinations[0]);
  const cpu = page.getByRole('region', { name: 'CPU status', exact: true }); await expect(cpu).toContainText('28');
  api.handlers.set('GET /admin/v1/system/status', (route) => json(route, { Error: { Code: 'telemetry_unavailable', Message: 'System metrics are temporarily unavailable.' } }, 503));
  await page.getByRole('button', { name: 'Refresh overview' }).click();
  await expect(page.getByRole('alert')).toContainText('System metrics are temporarily unavailable.');
  await expect(page.getByText(/System metrics show the last successful update/)).toBeVisible();
  await expect(cpu).toContainText('28'); await expect(page.getByRole('button', { name: 'View users', exact: true })).toContainText('2');
  api.handlers.delete('GET /admin/v1/system/status'); api.status.CPU.UsagePercent = 86;
  await page.getByRole('button', { name: 'Refresh overview' }).click();
  await expect(cpu.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '86');
  await expect(page.getByText(/System metrics show the last successful update/)).toHaveCount(0); await expect(page.getByRole('alert')).toHaveCount(0);
});

test('unavailable metrics are shown as unknown rather than a successful zero measurement', async ({ page, api }) => {
  api.status.CPU = { UsagePercent: null, Load1: null, Load5: null, Load15: null };
  api.status.Memory = { TotalBytes: null, UsedBytes: null, CachedBytes: null, SwapUsedBytes: null };
  api.status.Storage = { TotalBytes: null, UsedBytes: null, Complete: false, Volumes: [{ Path: 'D:\\Offline', TotalBytes: null, UsedBytes: null, Available: false }] };
  api.status.Transcoding = { Available: false, Active: null, Limit: null, HardwareActive: null, SoftwareActive: null };
  await page.goto('/admin/'); await ready(page, destinations[0]);
  for (const name of ['CPU', 'Memory', 'Media storage', 'Transcoding']) {
    const region = page.getByRole('region', { name: `${name} status`, exact: true });
    await expect(region.getByLabel('Not available', { exact: true })).toBeVisible();
    await expect(region.getByRole('progressbar')).toHaveAttribute('aria-valuetext', 'Not available');
  }
  await expect(page.getByRole('region', { name: 'Media storage status' })).toContainText('Some directories are unavailable');
});

test('stalled telemetry does not block counts or activity and is cancelled on timeout and navigation', async ({ page, api }) => {
  const held: Route[] = [];
  let cancelled = 0;
  page.on('requestfailed', (request) => { if (new URL(request.url()).pathname === '/admin/v1/system/status') cancelled += 1; });
  const stall = async (route: Route) => { held.push(route); };
  api.handlers.set('GET /admin/v1/system/status', stall);
  try {
    await page.goto('/admin/');
    await expect(page.getByRole('button', { name: 'View users', exact: true })).toContainText('2');
    await expect(page.getByRole('list', { name: 'Recent activity records', exact: true })).toContainText('Administrator signed in');
    const firstOverviewReads = api.reads('/admin/v1/overview').length;
    const firstCancelled = cancelled;
    await expect(page.getByRole('alert')).toContainText('System metrics did not respond within 15 seconds. Refresh to try again.', { timeout: 20_000 });
    await expect.poll(() => cancelled).toBeGreaterThan(firstCancelled);
    expect(api.reads('/admin/v1/overview').length, 'Other resources must keep refreshing while telemetry is stalled.').toBeGreaterThan(firstOverviewReads);
    api.handlers.delete('GET /admin/v1/system/status');
    await page.getByRole('button', { name: 'Refresh overview', exact: true }).click();
    await expect(page.getByRole('region', { name: 'CPU status', exact: true }).getByRole('progressbar')).toHaveAttribute('aria-valuenow', '28');
    await expect(page.getByRole('alert')).toHaveCount(0);

    api.handlers.set('GET /admin/v1/system/status', stall);
    const beforeLeave = cancelled; const readsBeforeLeave = api.reads('/admin/v1/system/status').length;
    await page.getByRole('button', { name: 'Refresh overview', exact: true }).click();
    await expect.poll(() => api.reads('/admin/v1/system/status').length).toBeGreaterThan(readsBeforeLeave);
    await page.getByRole('navigation', { name: 'Administration' }).getByRole('link', { name: 'Media', exact: true }).click();
    await ready(page, destinations[1]); await expect.poll(() => cancelled).toBeGreaterThan(beforeLeave);
  } finally {
    await Promise.all(held.map((route) => route.abort().catch(() => {})));
  }
});

test('account menu is keyboard accessible and sign out returns to authentication', async ({ page, api }) => {
  await page.goto('/admin/'); await ready(page, destinations[0]);
  const account = page.getByRole('button', { name: `Account: ${administrator.Name}`, exact: true });
  await account.focus(); await page.keyboard.press('Enter'); await expect(page.getByRole('menu')).toBeVisible();
  await page.keyboard.press('Escape'); await expect(account).toBeFocused();
  await account.click(); await page.getByRole('menuitem', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('textbox', { name: /Username/i })).toBeVisible();
  expect(api.writes()).toEqual([{ method: 'DELETE', path: '/admin/v1/session', body: null, csrf }]);
});

test('settings tabs retain one complete draft and save using the exact shared revision', async ({ page, api }) => {
  await page.goto('/admin/settings/general'); await ready(page, destinations[13]);
  const initialReads = api.reads('/admin/v1/settings').length;
  const names = page.getByRole('region', { name: 'Server identity', exact: true });
  await names.getByRole('button', { name: 'Custom', exact: true }).click();
  await names.getByRole('textbox', { name: 'Server name', exact: true }).fill('Shared draft server');
  await page.getByRole('tab', { name: 'Transcode limits', exact: true }).click(); await ready(page, destinations[14]);
  const bitrate = page.getByRole('main');
  await bitrate.getByRole('checkbox', { name: 'Use deployment default for maximum bitrate', exact: true }).uncheck();
  await bitrate.getByRole('textbox', { name: 'Maximum bitrate', exact: true }).fill('12.5');
  await page.getByRole('tab', { name: 'Hardware acceleration', exact: true }).click(); await ready(page, destinations[15]);
  await page.getByRole('tab', { name: 'Metadata & subtitles', exact: true }).click(); await ready(page, destinations[16]);
  await page.getByRole('tab', { name: 'General', exact: true }).click(); await ready(page, destinations[13]);
  await expect(names.getByRole('textbox', { name: 'Server name', exact: true })).toHaveValue('Shared draft server');
  expect(api.reads('/admin/v1/settings')).toHaveLength(initialReads);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click(); await expect(page.getByText('Settings saved.', { exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0].body).toMatchObject({ Revision: '9007199254740993', ServerNameMode: 'custom', Overrides: { ServerName: 'Shared draft server', MaxBitrate: 12_500_000, MaxWidth: null, MaxHeight: null, MaxAudioChannels: null }, Encoding: { TranscodingMaxWidth: 0 } });
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
});

test('settings draft guards group changes and online providers while allowing internal tabs', async ({ page, api }) => {
  await page.goto('/admin/settings/general'); await ready(page, destinations[13]);
  const names = page.getByRole('region', { name: 'Server identity', exact: true });
  await names.getByRole('button', { name: 'Custom', exact: true }).click(); await names.getByRole('textbox', { name: 'Server name', exact: true }).fill('Keep this draft');
  let prompts = 0;
  page.on('dialog', async (dialog) => { prompts += 1; expect(dialog.type()).toBe('confirm'); expect(dialog.message()).toContain('unsaved'); await dialog.dismiss(); });
  await page.getByRole('tab', { name: 'Transcode limits', exact: true }).click(); await ready(page, destinations[14]); expect(prompts).toBe(0);
  await page.getByRole('tab', { name: 'Online providers', exact: true }).click(); expect(prompts).toBe(1); await expect(page).toHaveURL(/\/settings\/transcode$/);
  await page.getByRole('navigation', { name: 'Administration' }).getByRole('link', { name: 'Media', exact: true }).click(); expect(prompts).toBe(2); await expect(page).toHaveURL(/\/settings\/transcode$/);
  await page.getByRole('tab', { name: 'General', exact: true }).click(); await expect(names.getByRole('textbox', { name: 'Server name', exact: true })).toHaveValue('Keep this draft');
  expect(api.writes()).toEqual([]);
});

test('sorting includes an uncommitted chip in the shared draft and save', async ({ page, api }) => {
  await page.goto('/admin/settings/general'); await ready(page, destinations[13]);
  await page.getByRole('combobox', { name: 'Words removed from sort names', exact: true }).fill('of');
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeEnabled();
  await page.getByRole('tab', { name: 'Transcode limits', exact: true }).click(); await ready(page, destinations[14]);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByText('Settings saved.', { exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0].body).toMatchObject({ Sorting: { SortRemoveWords: ['the', 'a', 'an', 'of'] } });
  await page.getByRole('tab', { name: 'General', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Library sorting', exact: true }).getByText('of', { exact: true })).toBeVisible();
});

test('an in-flight settings save blocks tab changes until the mutation finishes', async ({ page, api }) => {
  let finish = () => {};
  const pending = new Promise<void>((resolve) => { finish = resolve; });
  api.handlers.set('PUT /admin/v1/settings', async (route, request) => {
    await pending; api.apply(request.postDataJSON() as SettingsUpdateInput); await json(route, api.settings);
  });
  await page.goto('/admin/settings/general'); await ready(page, destinations[13]);
  await page.getByRole('group', { name: 'Name source', exact: true }).getByRole('button', { name: 'Custom', exact: true }).click();
  await page.getByRole('textbox', { name: 'Server name', exact: true }).fill('Pending save');
  try {
    await page.getByRole('button', { name: 'Save changes', exact: true }).click();
    await expect.poll(() => api.writes().length).toBe(1);
    await expect(page.getByRole('tab', { name: 'Transcode limits', exact: true })).toHaveAttribute('aria-disabled', 'true');
    await expect(page.getByRole('textbox', { name: 'Server name', exact: true })).toBeDisabled();
    await expect(page).toHaveURL(/\/settings\/general$/);
  } finally { finish(); }
  await expect(page.getByText('Settings saved.', { exact: true })).toBeVisible();
  await page.getByRole('tab', { name: 'Transcode limits', exact: true }).click(); await ready(page, destinations[14]);
});

for (const width of [839, 840]) test(`navigation changes at the specified ${width}px breakpoint`, async ({ page, api }) => {
  await page.setViewportSize({ width, height: 834 });
  await page.goto('/admin/settings/metadata'); await ready(page, destinations[16]); await noOverflow(page);
  const nav = await page.getByRole('navigation', { name: 'Administration' }).boundingBox();
  expect(nav).not.toBeNull();
  if (width < 840) { expect(nav!.width).toBe(width); expect(nav!.y).toBeGreaterThan(740); }
  else { expect(nav!.width).toBe(88); expect(nav!.y).toBe(0); }
  expect(api.writes()).toEqual([]);
});
