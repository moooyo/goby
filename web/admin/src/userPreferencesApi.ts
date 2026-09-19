import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export interface UserConfiguration {
  AudioLanguagePreference: string; SubtitleLanguagePreference: string;
  PlayDefaultAudioTrack: boolean; RememberAudioSelections: boolean; RememberSubtitleSelections: boolean;
  EnableNextEpisodeAutoPlay: boolean; HidePlayedInLatest: boolean; HidePlayedInMoreLikeThis: boolean;
  HidePlayedInSuggestions: boolean; DisplayMissingEpisodes: boolean;
  SubtitleMode: 'Default' | 'Always' | 'OnlyForced' | 'None' | 'Smart' | 'HearingImpaired';
  ResumeRewindSeconds: number; OrderedViews: string[]; LatestItemsExcludes: string[]; MyMediaExcludes: string[];
  IntroSkipMode: 'None' | 'ShowButton' | 'AutoSkip'; EnableLocalPassword: boolean; ProfilePin: string;
}
export interface UserPreferences { UserId: string; Revision: string; Configuration: UserConfiguration }
export type WritableUserConfiguration = Pick<UserConfiguration,
  'AudioLanguagePreference' | 'SubtitleLanguagePreference' | 'PlayDefaultAudioTrack' | 'RememberAudioSelections' | 'RememberSubtitleSelections'
  | 'SubtitleMode' | 'ResumeRewindSeconds' | 'HidePlayedInLatest' | 'HidePlayedInMoreLikeThis' | 'OrderedViews' | 'LatestItemsExcludes' | 'MyMediaExcludes'>;
export const preferenceBooleans = ['PlayDefaultAudioTrack', 'RememberAudioSelections', 'RememberSubtitleSelections', 'EnableNextEpisodeAutoPlay', 'HidePlayedInLatest', 'HidePlayedInMoreLikeThis', 'HidePlayedInSuggestions', 'DisplayMissingEpisodes'] as const;
export const subtitleModes = ['Default', 'Always', 'OnlyForced', 'None', 'Smart', 'HearingImpaired'] as const;
function decode(result: UserPreferences, id: string): UserPreferences {
  const config = result?.Configuration;
  if (!result || result.UserId !== id || typeof result.Revision !== 'string' || !/^[1-9]\d*$/.test(result.Revision) || BigInt(result.Revision) > 9223372036854775807n || !config
    || !preferenceBooleans.every((field) => typeof config[field] === 'boolean')
    || typeof config.AudioLanguagePreference !== 'string' || typeof config.SubtitleLanguagePreference !== 'string'
    || !subtitleModes.includes(config.SubtitleMode) || !['None', 'ShowButton', 'AutoSkip'].includes(config.IntroSkipMode)
    || !Number.isInteger(config.ResumeRewindSeconds) || config.ResumeRewindSeconds < 0 || config.ResumeRewindSeconds > 300
    || !(['OrderedViews', 'LatestItemsExcludes', 'MyMediaExcludes'] as const).every((field) => Array.isArray(config[field]) && config[field].every((value) => typeof value === 'string'))) throw new ApiError('The user preferences response is incomplete. Reload before editing.', { code: 'invalid_response' });
  return result;
}
export const userPreferencesApi = {
  async get(id: string, options: RequestOptions = {}): Promise<UserPreferences> { return decode(await authenticatedRequest<UserPreferences>(`/users/${encodeURIComponent(id)}/preferences`, options), id); },
  async update(id: string, revision: string, configuration: Partial<WritableUserConfiguration>): Promise<UserPreferences> { return decode(await authenticatedRequest<UserPreferences>(`/users/${encodeURIComponent(id)}/preferences`, { method: 'PUT', body: { Revision: revision, Configuration: configuration } }), id); },
};
