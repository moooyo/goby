import type { BackgroundSource, ItemPreferences, MediaItem, MediaStream, PlayerPreferences, UserConfiguration } from '../types';

export const DEFAULT_BACKGROUND_SOURCES: readonly BackgroundSource[] = ['theme', 'generated', 'trailer'];

export const DEFAULT_PREFERENCES: PlayerPreferences = {
  quality: 'original', audioLanguage: '', subtitleLanguage: 'chi', subtitleMode: 'Smart',
  autoNext: true, autoSkip: false, subtitleSize: 1, subtitleStyle: 'shadow', heroRotate: true, player: 'goby',
  backgroundMotion: true, backgroundSources: [...DEFAULT_BACKGROUND_SOURCES],
};

type PreferencePersistence = {
  read(scope: string): Promise<Record<string, string>>;
  write(scope: string, values: Record<string, string>): Promise<void>;
  readConfiguration(): Promise<UserConfiguration>;
  writeConfiguration(patch: Partial<UserConfiguration>): Promise<unknown>;
};

let persistence: PreferencePersistence | undefined;
const loadedItems = new Map<string, Promise<ItemPreferences>>();
let scope = 'anonymous';
const memory = new Map<string, unknown>();
const revisions = new Map<string, number>();

export function configurePreferencePersistence(adapter: PreferencePersistence): void { persistence = adapter; }
export function setPreferenceScope(value: string): void { scope = value; loadedItems.clear(); }

function key(suffix: string): string { return `goby.player.preferences.v1.${scope}.${suffix}`; }
function read<T>(suffix: string, fallback: T): T {
  const cacheKey = key(suffix);
  try { return JSON.parse(localStorage.getItem(cacheKey) || 'null') as T ?? memory.get(cacheKey) as T ?? fallback; }
  catch { return memory.get(cacheKey) as T ?? fallback; }
}
function write(suffix: string, value: unknown): void {
  memory.set(key(suffix), value);
  revisions.set(key(suffix), (revisions.get(key(suffix)) || 0) + 1);
  try { localStorage.setItem(key(suffix), JSON.stringify(value)); } catch { /* Preferences still work for this session. */ }
  if (typeof window !== 'undefined') window.dispatchEvent(new Event('goby:preferences'));
}

function validItem(value: unknown): ItemPreferences {
  if (!value || typeof value !== 'object') return {};
  const input = value as Partial<ItemPreferences>;
  const result: ItemPreferences = {};
  if (['original', '1080p', '720p', '480p'].includes(input.quality || '')) result.quality = input.quality;
  if (Number.isInteger(input.audioStreamIndex) && input.audioStreamIndex! >= 0) result.audioStreamIndex = input.audioStreamIndex;
  if (Number.isInteger(input.subtitleStreamIndex) && input.subtitleStreamIndex! >= -1) result.subtitleStreamIndex = input.subtitleStreamIndex;
  if (typeof input.player === 'string' && input.player.length <= 64) result.player = input.player;
  return result;
}

function validPreferences(value: unknown): PlayerPreferences {
  const result = { ...DEFAULT_PREFERENCES, backgroundSources: [...DEFAULT_BACKGROUND_SOURCES] };
  if (!value || typeof value !== 'object') return result;
  const input = value as Partial<PlayerPreferences>;
  Object.assign(result, validItem(input));
  for (const field of ['autoNext', 'autoSkip', 'heroRotate', 'backgroundMotion'] as const) {
    if (typeof input[field] === 'boolean') result[field] = input[field];
  }
  for (const field of ['audioLanguage', 'subtitleLanguage'] as const) {
    if (typeof input[field] === 'string' && input[field]!.length <= 32) result[field] = input[field]!;
  }
  if (['Default', 'Always', 'OnlyForced', 'None', 'Smart', 'HearingImpaired'].includes(input.subtitleMode || '')) result.subtitleMode = input.subtitleMode!;
  if ([0, 1, 2, 3].includes(input.subtitleSize!)) result.subtitleSize = input.subtitleSize!;
  if (['shadow', 'outline', 'background'].includes(input.subtitleStyle || '')) result.subtitleStyle = input.subtitleStyle!;
  if (Array.isArray(input.backgroundSources) && input.backgroundSources.length === DEFAULT_BACKGROUND_SOURCES.length
    && DEFAULT_BACKGROUND_SOURCES.every(source => input.backgroundSources!.includes(source))) result.backgroundSources = [...input.backgroundSources];
  return result;
}

export function getPreferences(): PlayerPreferences { return validPreferences(read('global', DEFAULT_PREFERENCES)); }

export function applyUserConfiguration(config: UserConfiguration): PlayerPreferences {
  const result = getPreferences();
  if (config.AudioLanguagePreference !== undefined) result.audioLanguage = config.AudioLanguagePreference;
  if (config.SubtitleLanguagePreference !== undefined) result.subtitleLanguage = config.SubtitleLanguagePreference;
  if (config.SubtitleMode !== undefined) result.subtitleMode = config.SubtitleMode;
  if (config.EnableNextEpisodeAutoPlay !== undefined) result.autoNext = config.EnableNextEpisodeAutoPlay;
  if (config.IntroSkipMode !== undefined) result.autoSkip = config.IntroSkipMode === 'AutoSkip';
  write('global', result);
  return result;
}

export async function loadPreferences(): Promise<PlayerPreferences> {
  if (!persistence) return getPreferences();
  const requestedScope = scope;
  const requestedRevision = revisions.get(key('global')) || 0;
  const [custom, config] = await Promise.all([persistence.read('goby-player'), persistence.readConfiguration()]);
  if (requestedScope !== scope || (revisions.get(key('global')) || 0) !== requestedRevision) return getPreferences();
  if (custom.preferences) {
    try { write('global', validPreferences(JSON.parse(custom.preferences))); } catch { /* Ignore malformed client preferences. */ }
  }
  return applyUserConfiguration(config);
}

export async function setPreferences(patch: Partial<PlayerPreferences>): Promise<PlayerPreferences> {
  const requestedScope = scope;
  const result = validPreferences({ ...getPreferences(), ...patch });
  write('global', result);
  if (persistence) {
    const configuration: Partial<UserConfiguration> = {};
    if (patch.audioLanguage !== undefined) { configuration.AudioLanguagePreference = result.audioLanguage; configuration.PlayDefaultAudioTrack = !result.audioLanguage; }
    if (patch.subtitleLanguage !== undefined) configuration.SubtitleLanguagePreference = result.subtitleLanguage;
    if (patch.subtitleMode !== undefined) configuration.SubtitleMode = result.subtitleMode;
    if (patch.autoNext !== undefined) configuration.EnableNextEpisodeAutoPlay = result.autoNext;
    if (patch.autoSkip !== undefined) configuration.IntroSkipMode = result.autoSkip ? 'AutoSkip' : 'ShowButton';
    await persistence.write('goby-player', { preferences: JSON.stringify(result) });
    if (requestedScope !== scope) throw new Error('The active account changed before its preferences could be synchronized.');
    if (Object.keys(configuration).length) await persistence.writeConfiguration(configuration);
  }
  return result;
}

export function getItemPreferences(id: string): ItemPreferences { return validItem(read(`item.${id}`, {})); }

export function loadItemPreferences(id: string): Promise<ItemPreferences> {
  const cacheKey = `${scope}:${id}`;
  const existing = loadedItems.get(cacheKey);
  if (existing) return existing;
  const task = (async () => {
    if (!persistence) return getItemPreferences(id);
    const requestedScope = scope;
    const requestedRevision = revisions.get(key(`item.${id}`)) || 0;
    const values = await persistence.read(`goby-player-item-${id}`);
    if (requestedScope !== scope || (revisions.get(key(`item.${id}`)) || 0) !== requestedRevision) return getItemPreferences(id);
    if (values.selection) {
      try { write(`item.${id}`, validItem(JSON.parse(values.selection))); } catch { /* Ignore malformed client preferences. */ }
    }
    return getItemPreferences(id);
  })();
  loadedItems.set(cacheKey, task);
  task.catch(() => { if (loadedItems.get(cacheKey) === task) loadedItems.delete(cacheKey); });
  return task;
}

export async function setItemPreferences(id: string, patch: Partial<ItemPreferences>): Promise<ItemPreferences> {
  const result = validItem({ ...getItemPreferences(id), ...patch });
  write(`item.${id}`, result);
  loadedItems.set(`${scope}:${id}`, Promise.resolve(result));
  if (persistence) await persistence.write(`goby-player-item-${id}`, { selection: JSON.stringify(result) });
  return result;
}

function languageKey(language: string | undefined): string {
  const value = (language || '').toLowerCase().split(/[-_]/)[0];
  return ({ zh: 'chi', zho: 'chi', cmn: 'chi', en: 'eng', ja: 'jpn', ko: 'kor', fr: 'fre', fra: 'fre', de: 'ger', deu: 'ger' } as Record<string, string>)[value] || value;
}

function preferredSubtitle(stream: MediaStream, language: string): number {
  const title = `${stream.Title || ''} ${stream.DisplayTitle || ''} ${stream.DisplayLanguage || ''}`;
  if (language === 'mul') return /bilingual|dual|\u53cc\u8bed|\u96d9\u8a9e|\u4e2d\u82f1/i.test(title) ? 16 : 0;
  if (/tw|hant/i.test(language)) return /traditional|\bcht\b|\u7e41\u4f53|\u7e41\u9ad4/i.test(title) || /tw|hant/i.test(stream.Language || '') ? 16 : 0;
  if (languageKey(language) === 'chi') return /simplified|\bchs\b|\u7b80\u4f53|\u7c21\u9ad4/i.test(title) || /cn|hans/i.test(stream.Language || '') ? 8 : 0;
  return 0;
}

export const isPlayableSubtitle = (stream: MediaStream) => stream.Type === 'Subtitle' && !stream.GobySubtitleTimelineOnly;

export function resolveItemPreferences(item: MediaItem): ItemPreferences {
  const defaults = getPreferences();
  const remembered = getItemPreferences(item.SeriesId || item.Id);
  const result: ItemPreferences = { quality: defaults.quality, player: defaults.player, ...remembered };
  const source = item.MediaSources?.[0];
  const streams = source?.MediaStreams ?? item.MediaStreams ?? [];
  const playableSubtitles = streams.filter(isPlayableSubtitle);
  // Timeline metadata may reuse a remembered index, but is not a playback track.
  if (streams.some(stream => stream.Type === 'Subtitle' && stream.GobySubtitleTimelineOnly && stream.Index === result.subtitleStreamIndex)) delete result.subtitleStreamIndex;
  if (result.audioStreamIndex === undefined && defaults.audioLanguage) {
    result.audioStreamIndex = streams.find(stream => stream.Type === 'Audio' && languageKey(stream.Language) === languageKey(defaults.audioLanguage))?.Index;
  }
  if (result.audioStreamIndex === undefined) result.audioStreamIndex = source?.DefaultAudioStreamIndex
    ?? streams.find(stream => stream.Type === 'Audio' && stream.IsDefault)?.Index ?? streams.find(stream => stream.Type === 'Audio')?.Index;
  if (result.subtitleStreamIndex === undefined && defaults.subtitleMode === 'None') result.subtitleStreamIndex = -1;
  if (result.subtitleStreamIndex === undefined) {
    const preferredLanguage = languageKey(defaults.subtitleLanguage || defaults.audioLanguage);
    const audio = streams.find(stream => stream.Type === 'Audio' && stream.Index === result.audioStreamIndex);
    const forcedOnly = defaults.subtitleMode === 'OnlyForced' || defaults.subtitleMode === 'Smart' && (!preferredLanguage || preferredLanguage === languageKey(audio?.Language));
    const subtitles = playableSubtitles.filter(stream => (!forcedOnly || stream.IsForced)
      && (defaults.subtitleMode !== 'Default' || stream.IsDefault)
      && (defaults.subtitleMode !== 'HearingImpaired' || !playableSubtitles.some(candidate => candidate.IsHearingImpaired) || stream.IsHearingImpaired)
      && (defaults.subtitleMode !== 'Smart' || forcedOnly || languageKey(stream.Language) === preferredLanguage));
    const score = (stream: MediaStream) => preferredSubtitle(stream, defaults.subtitleLanguage)
      + (preferredLanguage && languageKey(stream.Language) === preferredLanguage ? 4 : 0) + (stream.IsDefault ? 2 : 0) + (stream.IsForced ? 1 : 0);
    subtitles.sort((a, b) => score(b) - score(a));
    result.subtitleStreamIndex = subtitles[0]?.Index ?? -1;
  }
  return result;
}

export function getLibrarySelection(type: string): string { return read(`library.${type}`, ''); }
export function setLibrarySelection(type: string, id: string): void { write(`library.${type}`, id); }
