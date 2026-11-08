import type { ManagedUser, UpdateUserInput, User, UserPolicy } from '../src/api';
import type { UserPreferences } from '../src/userPreferencesApi';
import { administrator, DashboardAPI, json, library, seriesLibrary, stamp } from './dashboard-fixture';

export const viewerId = 'c'.repeat(32);
export const initialUserRevision = '9007199254740993';
const policy: UserPolicy = {
  EnableAllFolders: true, EnabledFolders: [], EnableMediaPlayback: true, EnablePlaybackRemuxing: true,
  EnableAudioPlaybackTranscoding: true, EnableVideoPlaybackTranscoding: true,
  IsHidden: false, IsHiddenRemotely: false, IsHiddenFromUnusedDevices: false, MaxParentalRating: null,
  AllowTagOrRating: false, IsTagBlockingModeInclusive: false, BlockedTags: [], IncludeTags: [], BlockUnratedItems: [],
  EnableUserPreferenceAccess: true, AccessSchedules: [], EnableRemoteControlOfOtherUsers: false, EnableSharedDeviceControl: false,
  EnableRemoteAccess: true, AutoRemoteQuality: 0, EnableContentDeletion: false, RestrictedFeatures: [],
  EnableContentDeletionFromFolders: [], EnableContentDownloading: true, EnableSubtitleDownloading: true,
  EnableSubtitleManagement: false, RemoteClientBitrateLimit: 0, ExcludedSubFolders: [], SimultaneousStreamLimit: 0,
  EnabledDevices: [], EnableAllDevices: true,
};

export function installUserDesignFixture(api: DashboardAPI) {
  let user: ManagedUser = { ...administrator, Id: viewerId, Name: 'Family viewer', IsAdministrator: false, Revision: initialUserRevision, Policy: structuredClone(policy) };
  const preferences: UserPreferences = {
    UserId: viewerId, Revision: '9007199254741001', Configuration: {
      AudioLanguagePreference: 'en', SubtitleLanguagePreference: 'zh-CN', PlayDefaultAudioTrack: true,
      RememberAudioSelections: true, RememberSubtitleSelections: true, EnableNextEpisodeAutoPlay: true,
      HidePlayedInLatest: false, HidePlayedInMoreLikeThis: false, HidePlayedInSuggestions: false, DisplayMissingEpisodes: false,
      SubtitleMode: 'Default', ResumeRewindSeconds: 5, OrderedViews: [library.Id, seriesLibrary.Id],
      LatestItemsExcludes: [], MyMediaExcludes: [], IntroSkipMode: 'ShowButton', EnableLocalPassword: false,
    },
  };
  const summary = (): User => ({ Id: user.Id, Name: user.Name, IsAdministrator: user.IsAdministrator, IsDisabled: user.IsDisabled, HasPassword: user.HasPassword, CreatedAt: user.CreatedAt });
  api.handlers.set('GET /admin/v1/users', (route) => json(route, { Items: [administrator, summary()], TotalRecordCount: 2 }));
  api.handlers.set(`GET /admin/v1/users/${viewerId}`, (route) => json(route, { User: user }));
  api.handlers.set('GET /admin/v1/features', (route) => json(route, { Items: [{ Id: 'goby_playback', Name: 'Play media', FeatureType: 'User' }, { Id: 'goby_downloads', Name: 'Download media', FeatureType: 'User' }] }));
  api.handlers.set(`GET /admin/v1/users/${viewerId}/preferences`, (route) => json(route, preferences));
  api.handlers.set(`GET /admin/v1/users/${viewerId}/images`, (route) => json(route, { Revision: '1', Items: [] }));
  api.handlers.set('GET /admin/v1/sessions', (route, request) => {
    const url = new URL(request.url());
    const items = url.searchParams.get('UserId') === viewerId ? [{
      Id: 'f'.repeat(32), UserId: viewerId, UserName: user.Name, UserIsAdministrator: false, UserIsDisabled: false,
      Kind: 'emby', Client: 'Goby player', DeviceId: 'family-tablet', DeviceName: 'Family tablet', ApplicationVersion: '1.0',
      CreatedAt: stamp, LastSeenAt: stamp, ExpiresAt: '2026-11-05T08:00:00Z', RevokedAt: null, Status: 'active', IsCurrent: false,
    }] : [];
    return json(route, { Items: items, TotalRecordCount: items.length, StartIndex: Number(url.searchParams.get('StartIndex') ?? 0), Limit: Number(url.searchParams.get('Limit') ?? 25) });
  });
  api.handlers.set(`PUT /admin/v1/users/${viewerId}`, (route, request) => {
    const input = request.postDataJSON() as UpdateUserInput;
    if (input.Revision !== user.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'This user changed. Reload before saving.' } }, 409);
    user = { ...user, Name: input.Name, IsAdministrator: input.IsAdministrator, IsDisabled: input.IsDisabled, Policy: structuredClone(input.Policy), Revision: (BigInt(user.Revision) + 1n).toString() };
    return json(route, { User: user, CurrentSessionRevoked: false });
  });
  return { get user() { return user; }, preferences, initialPolicy: structuredClone(policy) };
}
