import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Request, Route } from '@playwright/test';
import type { ActivityEntry, MetadataDetail, MetadataValues, ServerSettings, SettingsUpdateInput } from '../src/api';
import type { RuntimeSettings } from '../src/runtimeSettings';
import type { SystemStatus } from '../src/systemStatusApi';
import type { AnalysisItem } from '../src/mediaAnalysis';
import { mediaInfoFixture, mediaSummaryFixture } from './media-summary-data';
import { providerConfigurationFixture } from './provider-configuration-fixture';

// These tests exercise the complete dashboard against explicit synthetic API
// responses. Only frontend assets reach the server; no real mutation is sent.
export const stamp = '2026-11-04T08:00:00Z';
export const csrf = 'synthetic-dashboard-v2-csrf';
export const administrator = { Id: 'a'.repeat(32), Name: 'Dashboard administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: stamp };
export const library = { Id: 'b'.repeat(32), Name: 'Feature films', CollectionType: 'movies', Paths: ['/media/movies'], CreatedAt: stamp, LastScanAt: stamp,
  LibraryOptions: { EnableLocalMetadata: true, EnableLocalImages: true, EnableEmbeddedArtwork: true, EnableIntroDetection: false } };
export const seriesLibrary = { ...library, Id: 'c'.repeat(32), Name: 'TV series', CollectionType: 'tvshows', Paths: ['/media/series'], LibraryOptions: { ...library.LibraryOptions, EnableIntroDetection: true } };
const backupTask = {
  Id: '9'.repeat(32), Key: 'backup.create', Name: 'Create automatic backup', Description: 'Create an encrypted backup using saved settings.',
  Category: 'System', IsHidden: false, Enabled: true, Revision: '1', ScheduleTimezone: 'UTC',
  Triggers: [{ Id: 'backup-daily', Kind: 'daily', IntervalTicks: null, TimeOfDayTicks: '108000000000', DayOfWeek: null,
    MaxRuntimeTicks: null, NextFireAt: '2026-11-08T03:00:00Z', CalculationError: '' }],
  CurrentRun: null, LastRun: null, NextRunAt: '2026-11-08T03:00:00Z',
};
const movie = { Id: 'film-one', LibraryId: library.Id, ParentId: '', ParentName: '', Name: 'The feature film', Type: 'Movie', Path: '/media/movies/Feature.mkv', IsFolder: false };
const movieValues: MetadataValues = {
  Name: movie.Name, SortName: 'Feature film', Overview: 'A feature film maintained by the server administrator. Metadata edits, artwork, credits, and source information share this editing sheet.',
  OriginalTitle: 'The Feature Film', OfficialRating: 'PG-13', ProductionYear: 2025, PremiereDate: '2025-03-08T00:00:00Z', EndDate: null, Status: null,
  CommunityRating: 8.5, ProviderIds: { Tmdb: '12345' }, Genres: ['Adventure', 'Drama'], Tags: [], Studios: ['Independent studio'],
  People: [{ Name: 'Alex Morgan', Type: 'Director', Role: '', SortOrder: 0 }], IndexNumber: null, ParentIndexNumber: null,
  AirsBeforeSeasonNumber: null, AirsAfterSeasonNumber: null, AirsBeforeEpisodeNumber: null, Album: '', Artists: [], AlbumArtists: [],
};
export const movieMetadata: MetadataDetail = { Item: movie, Revision: '9007199254740993', Automatic: structuredClone(movieValues), Effective: structuredClone(movieValues), Overrides: {}, LockedValues: {}, LockedFields: [], EditableFields: ['Name', 'SortName', 'Overview', 'OriginalTitle', 'OfficialRating', 'ProductionYear', 'PremiereDate', 'CommunityRating', 'ProviderIds', 'Genres', 'Tags', 'Studios', 'People'], InactiveFields: [], LastEditedBy: '', LastEditedAt: null };
export const analysisItem: AnalysisItem = {
  Id: 'episode-one', Name: 'Opening episode', Type: 'Episode', LibraryId: seriesLibrary.Id, MediaSourceId: 'source-one', SourceRevision: 'source-revision-one',
  Detection: { ItemId: 'episode-one', Revision: '1', ManualRevision: '0', SourceRevision: 'source-revision-one', Status: 'qualified', Reasons: [],
    Candidate: null, IntroSkipperCandidate: null, Effective: { StartTicks: 100_000_000, EndTicks: 400_000_000, Provenance: 'Detected' }, Suppressed: false, UpdatedAt: stamp },
  Previews: [{ Width: 320, Height: 180, Size: 32768, FrameCount: 12, Status: 'ready', FailureCode: '', UpdatedAt: stamp }],
};

function runtime(): RuntimeSettings {
  const defaults = {
    Network: { BindHost: '127.0.0.1', HttpPort: 8096 }, Hardware: { Decode: 'software', DeviceId: '', Encode: 'software' }, Threads: 2,
    H264: { CRF: 23, Preset: 'veryfast' as const, RateControl: 'bitrate' as const, ThreadCount: null, X264Options: '' }, HEVC: { CRF: 28, Preset: 'fast' as const, RateControl: 'bitrate' as const, ThreadCount: null, X264Options: '' },
    SoftwareToneMapping: true, VulkanToneMapping: false,
    Transcoding: { EnableHevcEncoding: true, EnableHardwareEncoding: true, DownMixAudioBoost: 1, EnableSubtitleExtraction: true, EnableOnTheFlyAttachmentExtraction: true },
    TranscodingTempPath: '/cache/goby-runtime',
    SoftwareToneMapOptions: { ToneMapSizeLimit: 'None' as const, SoftwareToneMapAlgorithm: 'Hable' as const },
    CodecConfigurations: [{ CodecId: 'V-E-libx264' as const, IsEnabled: true, Priority: 0 }, { CodecId: 'V-E-libx265' as const, IsEnabled: true, Priority: 0 }],
    EnableThrottling: false,
    ExtractionThreadCount: 1,
    CodecProfileLimits: { H264: 'high' as const, HEVC: 'main10' as const },
    SubtitleOptions: { AllowAssToText: true },
  };
  const { Network: _network, TranscodingTempPath: _path, ...effective } = defaults;
  return {
    Defaults: defaults, Overrides: { Network: null, Hardware: null, Threads: null, H264: null, HEVC: null, SoftwareToneMapping: null, VulkanToneMapping: null, Transcoding: null, TranscodingTempPath: null, SoftwareToneMapOptions: null, CodecConfigurations: null, EnableThrottling: null, ExtractionThreadCount: null, CodecProfileLimits: null, SubtitleOptions: null }, Effective: effective,
    Sources: { Network: { BindHost: 'deployment', HttpPort: 'deployment' }, Hardware: 'deployment', Threads: 'deployment', H264: 'deployment', HEVC: 'deployment', SoftwareToneMapping: 'deployment', VulkanToneMapping: 'deployment', Transcoding: 'deployment', TranscodingTempPath: 'deployment', SoftwareToneMapOptions: 'deployment', CodecConfigurations: 'deployment', EnableThrottling: 'deployment', ExtractionThreadCount: 'deployment', CodecProfileLimits: 'deployment', SubtitleOptions: 'deployment' },
    Network: { Desired: { ...defaults.Network }, Active: { Configured: { ...defaults.Network }, BoundHost: '127.0.0.1', HttpPort: 8096, Revision: '1' }, RestartRequired: false, ReconnectURL: '' },
    Hardware: { Available: true, Code: '', Devices: [{ DeviceId: 'amd-device', Label: 'AMD Radeon GPU', Available: true, Code: '' }] },
    TranscodingTempPath: { Desired: defaults.TranscodingTempPath, Active: defaults.TranscodingTempPath, RestartRequired: false },
    Effects: { Network: 'restart', Hardware: 'next_admission', Threads: 'next_admission', H264: 'next_admission', HEVC: 'next_admission', SoftwareToneMapping: 'next_admission', VulkanToneMapping: 'next_admission', Transcoding: 'next_admission', TranscodingTempPath: 'restart', SoftwareToneMapOptions: 'next_admission', CodecConfigurations: 'next_admission', EnableThrottling: 'next_hls_playback', ExtractionThreadCount: 'next_image_extraction', CodecProfileLimits: 'next_admission', SubtitleOptions: 'next_admission' },
    Applicability: { H264: 'software_h264_output', HEVC: 'software_hevc_output', SoftwareToneMapping: 'software_filter', VulkanToneMapping: 'vulkan_filter', Transcoding: 'encoding_and_extraction', TranscodingTempPath: 'cache_root_after_restart', SoftwareToneMapOptions: 'software_tone_mapping', CodecConfigurations: 'software_video_encoding', EnableThrottling: 'file_hls_video_encoding', ExtractionThreadCount: 'image_extraction', CodecProfileLimits: 'new_video_encoding', SubtitleOptions: 'ass_text_negotiation' },
  };
}

export function settings(): ServerSettings {
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
    Timestamp: stamp, UptimeSeconds: 184200, Host: { OS: 'linux', Architecture: 'amd64', CPUCount: 8 },
    CPU: { UsagePercent: 28, Load1: 0.6, Load5: 0.8, Load15: 1.2 },
    Memory: { TotalBytes: 16 * 2 ** 30, UsedBytes: 6 * 2 ** 30, CachedBytes: 2 * 2 ** 30, SwapUsedBytes: 0 },
    Storage: { TotalBytes: 2 ** 40, UsedBytes: 420 * 2 ** 30, Complete: true, Volumes: [{ Path: '/media', TotalBytes: 2 ** 40, UsedBytes: 420 * 2 ** 30, Available: true }] },
    Transcoding: { Available: true, Active: 2, Limit: 4, HardwareActive: 1, SoftwareActive: 1 },
  };
}

const activities: ActivityEntry[] = [
  { Id: '1', Date: stamp, Action: 'session.login', Severity: 'Info', Source: 'native', Actor: { Kind: 'user', Id: administrator.Id, Name: administrator.Name }, Resource: { Kind: 'session', Id: 'session-one' }, Revision: null, Count: '1', State: 'completed', ChangedFields: [], Name: 'Administrator signed in', Overview: 'A dashboard session started.' },
  { Id: '2', Date: stamp, Action: 'scan.finished', Severity: 'Info', Source: 'system', Actor: { Kind: 'system', Id: null, Name: null }, Resource: { Kind: 'library', Id: library.Id }, Revision: null, Count: '12', State: 'completed', ChangedFields: [], Name: 'Feature films scan completed', Overview: 'Twelve media items were added.' },
];

export async function json(route: Route, value: unknown, statusCode = 200): Promise<void> {
  await route.fulfill({ status: statusCode, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) });
}
export interface Captured { method: string; path: string; body: unknown; csrf?: string }
export class DashboardAPI {
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
        if (path === '/admin/v1/bootstrap') return json(route, { Initialized: true, UICulture: 'zh-CN' });
        if (path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrf });
        if (path === '/admin/v1/overview') return json(route, { Server: { Id: 'd'.repeat(32), Name: 'Goby living room', Version: '0.1.0' }, Database: { Status: 'ok' }, Counts: { Users: 2, Libraries: 2, Items: 1240, ActiveSessions: 3 }, Runtime: { GoVersion: 'go1.26.1' }, Features: { LibraryManagement: true, Playback: true, Transcoding: true, ApplicationKeys: true } });
        if (path === '/admin/v1/system/status') return json(route, this.status);
        if (path === '/admin/v1/activity') {
          const filtered = activities.filter((entry) => (!url.searchParams.get('Action') || entry.Action === url.searchParams.get('Action'))
            && (!url.searchParams.get('Severity') || entry.Severity === url.searchParams.get('Severity'))
            && (!url.searchParams.get('ActorId') || entry.Actor.Id === url.searchParams.get('ActorId'))
            && (!url.searchParams.get('MinDate') || new Date(entry.Date).getTime() >= new Date(url.searchParams.get('MinDate')!).getTime()));
          const start = Number(url.searchParams.get('StartIndex') ?? 0), limit = Number(url.searchParams.get('Limit') ?? 50);
          return json(route, { Items: filtered.slice(start, start + limit), TotalRecordCount: filtered.length, StartIndex: start, Limit: limit, RetentionDays: 30 });
        }
        if (path === '/admin/v1/logs') return json(route, { ...paged([]), Status: { Healthy: true, Degraded: false, Closed: false, MaxFileBytes: '1048576', MaxFiles: 10, RetentionDays: 30, MinFreeBytes: '0', Format: 'jsonl' } });
        if (path === '/admin/v1/settings') return json(route, this.settings);
        if (path === '/admin/v1/server-policy') return json(route, {
          Revision: '9007199254740993', EnableRemoteAccess: true, LocalNetworkSubnets: ['10.0.0.0/8', '192.168.0.0/16'],
          UICulture: 'zh-CN', UpdatedAt: stamp, Client: { Address: '192.168.0.2', IsLocal: true },
          Restart: { Available: true, InProgress: false },
          Updates: { CanSelfUpdate: false, Delivery: 'docker', ConfiguredSource: false, CurrentVersion: '0.1.0-fixture' },
        });
        if (path === '/admin/v1/server-updates') return json(route, {
          CanSelfUpdate: false, Delivery: 'docker', ConfiguredSource: false, CurrentVersion: '0.1.0-fixture',
          State: 'unconfigured', CheckedAt: null, Release: null, ErrorCode: null,
        });
        if (path === '/admin/v1/hardware/codecs') {
          const compiled = (managed: boolean, selected = false) => ({ Managed: managed, Compiled: null, Selected: selected, Implementations: [] });
          return json(route, {
            ObservedAt: stamp, CompiledAt: null, CompiledState: 'unknown', FFmpegVersion: '', TranscodingAvailable: true,
            Selected: { Decode: 'software', Encode: 'software', DeviceId: '' }, Devices: [],
            Items: [['h264', 'H.264'], ['hevc', 'HEVC'], ['av1', 'AV1']].map(([codec, name]) => ({
              Codec: codec, Name: name, SoftwareDecode: compiled(true, true), SoftwareEncode: compiled(true, true),
              HardwareDecode: compiled(true), HardwareEncode: compiled(true), EncodingProofs: [],
            })),
          });
        }
        if (path === '/admin/v1/users') return json(route, { Items: [administrator, { ...administrator, Id: 'c'.repeat(32), Name: 'Family viewer', IsAdministrator: false }], TotalRecordCount: 2 });
        if (path === '/admin/v1/users/activity') return json(route, {
          Items: (url.searchParams.get('UserIds') ?? '').split(',').filter(Boolean).map((id) => ({
            Id: id, LastLoginDate: null, LastActivityDate: null, LastPlayedDate: null, PlayedItemCount: 0,
          })), ObservedAt: stamp,
        });
        if (path === '/admin/v1/devices') return json(route, paged([{ Id: '101', Revision: '1', ReportedDeviceId: 'living-room-tv', Name: 'Living room TV', ReportedName: 'Android TV', CustomName: 'Living room TV', AppName: 'Goby TV', AppVersion: '1.0', LastUserId: administrator.Id, LastUserName: administrator.Name, CreatedAt: stamp, LastSeenAt: stamp, IpAddress: '192.0.2.4', ActiveLoginCount: 1 }]));
        if (path === '/admin/v1/sessions') return json(route, paged([{ Id: 'f'.repeat(32), UserId: administrator.Id, UserName: administrator.Name, UserIsAdministrator: true, UserIsDisabled: false, Kind: 'admin', Client: 'Goby dashboard', DeviceId: '', DeviceName: 'Desktop browser', ApplicationVersion: '0.1.0', CreatedAt: stamp, LastSeenAt: stamp, ExpiresAt: '2026-11-05T08:00:00Z', RevokedAt: null, Status: 'active', IsCurrent: true }]));
        if (path === '/admin/v1/playback/sessions') return json(route, { Items: [], ObservedAt: stamp, Limit: 256, AtCapacity: false });
        if (path === '/admin/v1/api-keys') return json(route, paged([{ Id: '201', AppName: 'Home automation', CreatedAt: stamp, LastUsedAt: stamp, RevokedAt: null, CreatedBy: administrator.Id, IPAddress: '192.0.2.5', Status: 'active' }]));
        if (path === '/admin/v1/libraries') return json(route, { Items: [library, seriesLibrary], TotalRecordCount: 2 });
        if (path === '/admin/v1/storage/roots') return json(route, { Configured: true, Items: [{ Path: '/media', Available: true, Readable: true, Writable: false }] });
        if (path === '/admin/v1/storage/library-directories') return json(route, {
          Items: [library.Paths[0], seriesLibrary.Paths[0]].map((Path) => ({
            Path, Available: true, Readable: true, Writable: false,
            TotalBytes: '1099511627776', UsedBytes: '549755813888', AvailableBytes: '548682072064',
          })), TotalRecordCount: 2,
        });
        if (path === `/admin/v1/libraries/${library.Id}/items`) return json(route, { Library: library, ...paged([{ Id: 'film-one', LibraryId: library.Id, ParentId: '', ParentName: '', Name: 'The feature film', Type: 'Movie', Path: '/media/movies/Feature.mkv', IsFolder: false, ProductionYear: 2025, IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0, Media: mediaSummaryFixture }]) });
        if (path === `/admin/v1/libraries/${seriesLibrary.Id}/items`) return json(route, { Library: seriesLibrary, ...paged([{ Id: 'series-one', LibraryId: seriesLibrary.Id, ParentId: '', ParentName: '', Name: 'Opening series', Type: 'Series', Path: '/media/series/Opening series', IsFolder: true, ProductionYear: 2025, IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0 }]) });
        if (path === '/admin/v1/entities') return json(route, paged([{ Id: 'person-one', Name: 'Alex Morgan', Type: 'Person' }]));
        if (/^\/admin\/v1\/items\/[^/]+\/images$/.test(path)) return json(route, { Revision: '1', Items: [] });
        if (path === '/admin/v1/items/film-one/metadata') return json(route, movieMetadata);
        if (path === '/admin/v1/items/film-one/providers/provenance') return json(route, { Items: [] });
        if (path === '/admin/v1/items/film-one/media-info') return json(route, mediaInfoFixture);
        if (/^\/admin\/v1\/series\/[^/]+\/display-order$/.test(path)) return json(route, {
          SeriesId: decodeURIComponent(path.split('/')[4]), SeriesName: 'Opening series', Revision: '0', DisplayOrder: 'Aired',
          ProviderSeriesId: '', GroupId: '', GroupName: '', MappingSHA256: '', Entries: [],
          Coverage: { PhysicalEpisodeCount: 0, ExpectedEpisodeCount: 0, UnmappedCount: 0, Unmapped: [] },
          SourceMatches: true, FetchedAt: null, UpdatedAt: null,
        });
        if (path === '/admin/v1/items/film-one/credits') return json(route, { ItemId: movie.Id, MediaSourceId: 'source-one', SourceRevision: 'revision-one', Revision: '1', DurationTicks: 72_000_000_000, Automatic: null, Effective: null, Override: null, OverrideStale: false, LastEditedBy: '', LastEditedAt: null });
        if (path === '/admin/v1/items/film-one/media-processing') return json(route, { ItemId: movie.Id, MediaSourceId: 'source-one', SourceRevision: 'revision-one', Container: 'mkv', Streams: [], Capabilities: { Enabled: false, Available: false, UnavailableReason: 'Not configured', MaxConcurrent: 1, MaxQueued: 1, WritableProfiles: [], OCR: { Available: false, UnavailableReason: 'Not configured', Models: [], OutputFormats: [] } } });
        if (path === '/admin/v1/playlists') return json(route, { Items: [{ Id: 'playlist-one', Name: 'Weekend favorites', Type: 'Playlist', IsFolder: true, ParentId: '', OwnerId: administrator.Id, MediaType: 'Video', IsPublic: false, IsLocked: false, ChildCount: 12, Shares: [] }], TotalRecordCount: 1 });
        if (path === '/admin/v1/collections') return json(route, { Items: [{ Id: 'collection-one', Name: 'Weekend favorites', Type: 'BoxSet', IsFolder: true, ParentId: '', OwnerId: administrator.Id, MediaType: 'Video', IsPublic: false, IsLocked: false, ChildCount: 12, Shares: [] }], TotalRecordCount: 1 });
        if (path === '/admin/v1/providers') return json(route, { Enabled: true, Items: [{ Id: 'tmdb', Name: 'The Movie Database', Configured: true, Capabilities: ['metadata', 'images'], Attribution: 'Movie and television metadata', Website: 'https://www.themoviedb.org/' }, { Id: 'opensubtitles', Name: 'OpenSubtitles', Configured: false, Capabilities: ['subtitles'], Attribution: 'Subtitles for your media', Website: 'https://www.opensubtitles.com/' }] });
        if (path === '/admin/v1/providers/configuration') return json(route, providerConfigurationFixture());
        if (path === '/admin/v1/tasks') return json(route, { Items: [{ Id: 'scan-library', Key: 'library.scan', Name: 'Scan media libraries', Description: 'Discover new files and update library metadata.', Category: 'Library', IsHidden: false, Enabled: true, Revision: '1', ScheduleTimezone: 'UTC', Triggers: [], CurrentRun: null, LastRun: null, NextRunAt: null }, backupTask], TotalRecordCount: 2 });
        if (/^\/admin\/v1\/tasks\/[^/]+\/runs$/.test(path)) return json(route, paged([]));
        if (path === '/admin/v1/backups/policy') return json(route, {
          Revision: '1', Enabled: true, Schedule: { Kind: 'daily', DayOfWeek: null, TimeOfDay: '03:00', Timezone: 'UTC' },
          KeepCount: 7, IncludeImages: true, HasPassphrase: true, NextRunAt: backupTask.NextRunAt, Available: true, UnavailableReason: '',
          TaskId: '9'.repeat(32), LastRunAt: null, LastRunState: '', LastBackupId: '',
        });
        if (path === '/admin/v1/tasks/' + backupTask.Id) return json(route, { Task: backupTask });
        if (path === '/admin/v1/jobs') return json(route, { Items: [], TotalRecordCount: 0 });
        if (path === '/admin/v1/notifications') return json(route, { Revision: '1', Enabled: false, Endpoint: '', AllowedNetworks: [], HasReceiverCredential: false, SupportedEvents: ['CatalogInvalidated', 'UserDataInvalidated'], PendingCount: 0 });
        if (path === '/admin/v1/notification-destinations') return json(route, {
          Items: [], SupportedEvents: [
            { Id: 'UserChanges', Name: 'User changes', Actions: ['user.created', 'user.updated'] },
            { Id: 'TaskFinished', Name: 'Tasks finished', Actions: ['task.finished'] },
          ], Limit: 16,
        });
        if (path === '/admin/v1/background-previews/configuration') {
          const profile = { DurationSeconds: 15, MaxWidth: 1280, VideoBitrate: 2_000_000, MaxItemRuntimeSeconds: 600 };
          return json(route, { Revision: '1', Profile: profile, Defaults: profile, UpdatedAt: stamp });
        }
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
      if (method === 'POST' && path === '/admin/v1/items/film-one/providers/images') return json(route, { Items: [] });
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

export interface DashboardPreferences { language: 'zh' | 'en'; theme: 'dark' | 'light' }
export const test = base.extend<{ api: DashboardAPI; preferences: DashboardPreferences }>({
  preferences: [{ language: 'en', theme: 'light' }, { option: true }],
  api: async ({ context, page, preferences }, use) => {
  await context.addInitScript((initial) => {
    if (!localStorage.getItem('goby.admin.language')) localStorage.setItem('goby.admin.language', initial.language);
    if (!localStorage.getItem('goby.admin.theme')) localStorage.setItem('goby.admin.theme', initial.theme);
  }, preferences);
  const api = new DashboardAPI(); const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message)); await api.install(context); await use(api);
  expect(api.unexpected, 'All API calls must use an explicit fixture.').toEqual([]);
  expect(errors, 'Dashboard routes must not throw browser errors.').toEqual([]);
  expect(api.writes().every((value) => value.path === '/admin/v1/bootstrap' || value.method === 'POST' && value.path === '/admin/v1/session' || value.csrf === csrf), 'Authenticated writes must preserve CSRF protection.').toBe(true);
} });

export { expect };
