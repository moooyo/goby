import type { AuthSession, BackgroundPreviewResponse, DisplayPreferences, ItemQuery, ItemsResponse, MediaItem, MediaStream, PlaybackEvent, PlaybackInfo, PlaybackOptions, PlaybackSession, SearchHintsResponse, SystemInfo, ThemeMediaResponse, User, UserConfiguration, UserData } from '../types';
import { applyUserConfiguration, configurePreferencePersistence, resolveItemPreferences, setPreferenceScope } from './preferences';

const SESSION_KEY = 'goby.player.session.v1';
const DEVICE_KEY = 'goby.player.device.v1';
const FIELDS = 'Overview,OriginalTitle,ProductionYear,CommunityRating,OfficialRating,Genres,People,MediaSources,MediaStreams,Chapters,PrimaryImageAspectRatio,ChildCount,DateCreated';
const CLIENT = 'Goby Player';
type Query = Record<string, string | number | boolean | undefined>;

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  constructor(status: number, code: string, message: string) {
    super(message); this.name = 'ApiError'; this.status = status; this.code = code;
  }
}

function savedSession(): AuthSession | null {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(SESSION_KEY) || 'null');
    if (value && typeof value === 'object') {
      const session = value as AuthSession;
      if (typeof session.AccessToken === 'string' && typeof session.serverUrl === 'string' && session.User?.Id && session.User?.Name) return session;
    }
  } catch { /* An inaccessible or expired browser store requires a fresh login. */ }
  return null;
}

function newDeviceId(): string {
  const id = globalThis.crypto?.randomUUID?.() ?? `web-${Date.now()}-${Math.random().toString(36).slice(2)}`;
  try { const stored = localStorage.getItem(DEVICE_KEY); if (stored) return stored; localStorage.setItem(DEVICE_KEY, id); } catch { /* Keep an in-memory device ID. */ }
  return id;
}

export function normalizeServerUrl(value: string): string {
  const input = value.trim().replace(/\/+$/, '').replace(/\/emby$/i, '');
  if (!input) return '';
  const url = new URL(input.startsWith('/') ? input : /^https?:\/\//i.test(input) ? input : `http://${input}`, window.location.origin);
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) throw new ApiError(0, 'invalid_server_url', 'Use an HTTP or HTTPS server address without credentials, query, or fragment.');
  return `${url.origin}${url.pathname.replace(/\/+$/, '')}`;
}

export function browserDeviceProfile(options: PlaybackOptions = {}): object {
  const preset = ({ '1080p': [1920, 1080, 20_000_000], '720p': [1280, 720, 8_000_000], '480p': [854, 480, 3_000_000] } as const)[options.quality as '1080p' | '720p' | '480p'];
  const video = typeof document !== 'undefined' ? document.createElement('video') : undefined;
  const direct = [{ Type: 'Video', Container: 'mp4,m4v,mov', VideoCodec: 'h264', AudioCodec: 'aac,mp3' }];
  if (video?.canPlayType('video/webm; codecs="vp9, opus"')) direct.push({ Type: 'Video', Container: 'webm', VideoCodec: 'vp8,vp9', AudioCodec: 'opus,vorbis' });
  return {
    Name: CLIENT, SupportedMediaTypes: 'Video', MaxStreamingBitrate: preset?.[2] ?? 120_000_000,
    DirectPlayProfiles: direct,
    TranscodingProfiles: [{ Type: 'Video', Container: 'ts', Protocol: 'hls', VideoCodec: 'h264', AudioCodec: 'aac', Context: 'Streaming',
      MaxAudioChannels: '2', MinSegments: 1, SegmentLength: 6, BreakOnNonKeyFrames: false, MaxWidth: preset?.[0], MaxHeight: preset?.[1], ManifestSubtitles: 'vtt' }],
    CodecProfiles: [{ Type: 'Video', Codec: 'h264', Conditions: [
      { Condition: 'LessThanEqual', Property: 'VideoBitDepth', Value: '8', IsRequired: false },
      { Condition: 'Equals', Property: 'VideoRangeType', Value: 'SDR', IsRequired: false },
    ] }],
    SubtitleProfiles: [{ Format: 'vtt', Method: 'Hls', Container: 'ts', Protocol: 'hls' }, { Format: 'vtt', Method: 'External' },
      { Format: 'hdmv_pgs_subtitle,dvd_subtitle', Method: 'Encode', Container: 'ts', Protocol: 'hls' }],
  };
}

export class GobyApi {
  private session = savedSession();
  readonly deviceId = newDeviceId();
  private preferenceWrites: Promise<void> = Promise.resolve();

  constructor() {
    this.updateScope();
    configurePreferencePersistence({
      read: async scope => (await this.displayPreferences(scope)).CustomPrefs,
      write: (scope, values) => this.saveDisplayPreferences(scope, values),
      readConfiguration: () => this.getPreferences(),
      writeConfiguration: patch => this.savePreferences(patch),
    });
  }

  getSession(): AuthSession | null { return this.session; }
  private updateScope(): void { setPreferenceScope(this.session ? `${this.session.ServerId || this.session.serverUrl}.${this.session.User.Id}` : 'anonymous'); }
  private persistSession(): void {
    try { if (this.session) localStorage.setItem(SESSION_KEY, JSON.stringify(this.session)); else localStorage.removeItem(SESSION_KEY); } catch { /* Authentication remains usable in memory. */ }
    this.updateScope();
    if (typeof window !== 'undefined') window.dispatchEvent(new Event('goby:session'));
  }
  private base(): string { return this.session?.serverUrl ?? ''; }
  private userId(): string { if (!this.session) throw new ApiError(401, 'unauthorized', 'Sign in to continue.'); return this.session.User.Id; }
  private authorization(token = this.session?.AccessToken): string {
    const fields = { Client: CLIENT, Device: 'Web Browser', DeviceId: this.deviceId, Version: '1.0.0', ...(token ? { Token: token } : {}) };
    return `Emby ${Object.entries(fields).map(([key, value]) => `${key}="${value.replace(/["\\\r\n]/g, '')}"`).join(', ')}`;
  }
  private url(path: string, query: Query = {}, base = this.base()): URL {
    const url = new URL(`${base}${path}`, window.location.origin);
    for (const [key, value] of Object.entries(query)) if (value !== undefined && value !== '') url.searchParams.set(key, String(value));
    return url;
  }
  private async request<T>(path: string, options: { method?: string; body?: unknown; query?: Query; base?: string; anonymous?: boolean; keepalive?: boolean; signal?: AbortSignal } = {}): Promise<T> {
    const requestToken = this.session?.AccessToken;
    let response: Response;
    try {
      response = await fetch(this.url(path, options.query, options.base), {
        method: options.method || 'GET', credentials: 'omit', cache: 'no-store', keepalive: options.keepalive, signal: options.signal,
        headers: { 'X-Emby-Authorization': this.authorization(options.anonymous ? '' : undefined), Accept: 'application/json', ...(options.body === undefined ? {} : { 'Content-Type': 'application/json' }) },
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
      });
    } catch (error) {
      if (error instanceof Error && error.name === 'AbortError') throw error;
      throw new ApiError(0, 'network_error', 'The server could not be reached. Check its address and network connection.');
    }
    const text = await response.text();
    if (!options.anonymous && requestToken !== this.session?.AccessToken) throw new ApiError(401, 'session_changed', 'The active account changed before this request completed.');
    let value: unknown;
    try { value = text ? JSON.parse(text) : undefined; } catch { value = undefined; }
    if (!response.ok) {
      const failure = value as { ResponseStatus?: { ErrorCode?: string; Message?: string }; Error?: { Code?: string; Message?: string } } | undefined;
      const code = failure?.ResponseStatus?.ErrorCode || failure?.Error?.Code || `http_${response.status}`;
      const message = failure?.ResponseStatus?.Message || failure?.Error?.Message || (text && !text.includes('<') ? text.slice(0, 500) : response.statusText) || 'The server rejected the request.';
      if (response.status === 401 && !options.anonymous && this.session?.AccessToken === requestToken) { this.session = null; this.persistSession(); }
      throw new ApiError(response.status, code, message);
    }
    if (text && value === undefined) throw new ApiError(response.status, 'invalid_response', 'The server returned an invalid JSON response.');
    return value as T;
  }

  async authenticate(serverUrl: string, username: string, password: string): Promise<AuthSession> {
    const base = normalizeServerUrl(serverUrl);
    const result = await this.request<Omit<AuthSession, 'serverUrl'>>('/emby/Users/AuthenticateByName', { method: 'POST', base, anonymous: true, body: { Username: username, Pw: password } });
    if (!result?.AccessToken || !result.User?.Id) throw new ApiError(200, 'invalid_response', 'The server returned an incomplete authentication response.');
    this.session = { ...result, serverUrl: base }; this.persistSession();
    if (result.User.Configuration) applyUserConfiguration(result.User.Configuration);
    return this.session;
  }
  async logout(): Promise<void> {
    try { if (this.session) await this.request('/emby/Sessions/Logout', { method: 'POST' }); }
    finally { this.session = null; this.persistSession(); }
  }
  systemInfo(serverUrl?: string): Promise<SystemInfo> { return this.request('/emby/System/Info/Public', { base: serverUrl === undefined ? undefined : normalizeServerUrl(serverUrl), anonymous: true }); }
  endpoint(): Promise<{ IsLocal?: boolean; IsInNetwork?: boolean }> { return this.request('/emby/System/Endpoint'); }
  async currentUser(): Promise<User> {
    const owner = this.session?.AccessToken;
    const user = await this.request<User>(`/emby/Users/${encodeURIComponent(this.userId())}`);
    if (!owner || this.session?.AccessToken !== owner) throw new ApiError(401, 'session_changed', 'The active account changed before its profile was loaded.');
    this.session = { ...this.session, User: user }; this.persistSession();
    return user;
  }
  publicUsers(serverUrl?: string): Promise<User[]> { return this.request('/emby/Users/Public', { base: serverUrl === undefined ? undefined : normalizeServerUrl(serverUrl), anonymous: true }); }
  views(): Promise<ItemsResponse> { return this.request(`/emby/Users/${encodeURIComponent(this.userId())}/Views`); }
  items(query: ItemQuery = {}, signal?: AbortSignal): Promise<ItemsResponse> { return this.request(`/emby/Users/${encodeURIComponent(this.userId())}/Items`, { query: { Recursive: true, IncludeItemTypes: 'Movie,Series', Fields: FIELDS, ...query }, signal }); }
  latest(query: ItemQuery = {}): Promise<MediaItem[]> {
    const types = query.IncludeItemTypes?.split(',');
    const groupsSeries = types?.some(type => type.trim().toLowerCase() === 'series');
    const adjusted = groupsSeries ? { ...query, IncludeItemTypes: [...new Set(types!.map(type => type.trim().toLowerCase() === 'series' ? 'Episode' : type.trim()))].join(','), GroupItems: true } : query;
    return this.request(`/emby/Users/${encodeURIComponent(this.userId())}/Items/Latest`, { query: { Fields: FIELDS, Limit: 18, ...adjusted } });
  }
  resume(limit = 24): Promise<ItemsResponse> { return this.request(`/emby/Users/${encodeURIComponent(this.userId())}/Items/Resume`, { query: { Fields: FIELDS, IncludeItemTypes: 'Movie,Episode', Limit: limit } }); }
  favorites(query: ItemQuery = {}): Promise<ItemsResponse> { return this.items({ ...query, IsFavorite: true }); }
  search(term: string, query: ItemQuery = {}): Promise<ItemsResponse> { return this.items({ ...query, SearchTerm: term }); }
  searchHints(term: string, startIndex = 0, limit = 48): Promise<SearchHintsResponse> {
    return this.request('/emby/Search/Hints', { query: { UserId: this.userId(), SearchTerm: term, StartIndex: startIndex, Limit: limit, IncludeItemTypes: 'Movie,Series,Person,Genre', IncludeMedia: true, IncludePeople: true, IncludeGenres: true, IncludeStudios: false, IncludeArtists: false } });
  }
  themeMedia(id: string, signal?: AbortSignal): Promise<ThemeMediaResponse> {
    return this.request(`/emby/Items/${encodeURIComponent(id)}/ThemeMedia`, { signal, query: { UserId: this.userId(), InheritFromParent: true, EnableThemeSongs: false, EnableThemeVideos: true, Fields: 'MediaSources,MediaStreams' } });
  }
  backgroundPreview(id: string, signal?: AbortSignal): Promise<BackgroundPreviewResponse> {
    return this.request(`/emby/Items/${encodeURIComponent(id)}/BackgroundPreview`, { query: { UserId: this.userId() }, signal });
  }
  localTrailers(id: string, signal?: AbortSignal): Promise<MediaItem[]> {
    return this.request(`/emby/Users/${encodeURIComponent(this.userId())}/Items/${encodeURIComponent(id)}/LocalTrailers`, { signal, query: { Fields: 'MediaSources,MediaStreams' } });
  }
  item(id: string, signal?: AbortSignal): Promise<MediaItem> { return this.request(`/emby/Users/${encodeURIComponent(this.userId())}/Items/${encodeURIComponent(id)}`, { signal, query: { Fields: FIELDS } }); }
  seasons(seriesId: string): Promise<ItemsResponse> { return this.request(`/emby/Shows/${encodeURIComponent(seriesId)}/Seasons`, { query: { UserId: this.userId(), Fields: FIELDS, Limit: 1000 } }); }
  episodes(seriesId: string, seasonId?: string): Promise<ItemsResponse> { return this.request(`/emby/Shows/${encodeURIComponent(seriesId)}/Episodes`, { query: { UserId: this.userId(), SeasonId: seasonId, Fields: FIELDS, Limit: 1000, SortBy: 'ParentIndexNumber,IndexNumber' } }); }
  nextUp(seriesId?: string): Promise<ItemsResponse> { return this.request('/emby/Shows/NextUp', { query: { UserId: this.userId(), SeriesId: seriesId, Fields: FIELDS, Limit: 24 } }); }
  similar(id: string, limit = 12): Promise<ItemsResponse> { return this.request(`/emby/Items/${encodeURIComponent(id)}/Similar`, { query: { UserId: this.userId(), Fields: FIELDS, Limit: limit } }); }
  genres(parentId?: string, includeItemTypes = 'Movie,Series'): Promise<ItemsResponse> { return this.request('/emby/Genres', { query: { UserId: this.userId(), ParentId: parentId, IncludeItemTypes: includeItemTypes, Limit: 1000 } }); }
  people(query: ItemQuery = {}): Promise<ItemsResponse> { return this.request('/emby/Persons', { query: { UserId: this.userId(), ...query } }); }
  counts(): Promise<{ MovieCount: number; EpisodeCount: number; SeriesCount: number }> { return this.request('/emby/Items/Counts', { query: { UserId: this.userId() } }); }
  setFavorite(id: string, value: boolean): Promise<UserData> { return this.request(`/emby/Users/${encodeURIComponent(this.userId())}/FavoriteItems/${encodeURIComponent(id)}`, { method: value ? 'POST' : 'DELETE' }); }
  setPlayed(id: string, value: boolean): Promise<UserData> { return this.request(`/emby/Users/${encodeURIComponent(this.userId())}/PlayedItems/${encodeURIComponent(id)}`, { method: value ? 'POST' : 'DELETE' }); }
  getPreferences(): Promise<UserConfiguration> { return this.request(`/emby/Users/${encodeURIComponent(this.userId())}/Configuration`); }
  async savePreferences(patch: Partial<UserConfiguration>): Promise<UserConfiguration> {
    await this.request(`/emby/Users/${encodeURIComponent(this.userId())}/Configuration/Partial`, { method: 'POST', body: patch });
    return this.getPreferences();
  }
  displayPreferences(id = 'goby-player'): Promise<DisplayPreferences> { return this.request(`/emby/DisplayPreferences/${encodeURIComponent(id)}`, { query: { UserId: this.userId(), Client: CLIENT } }); }
  saveDisplayPreferences(id: string, values: Record<string, string>): Promise<void> {
    const owner = this.session?.AccessToken;
    const task = this.preferenceWrites.catch(() => undefined).then(async () => {
      if (!owner || this.session?.AccessToken !== owner) throw new ApiError(401, 'session_changed', 'The active account changed before its preferences could be saved.');
      const current = await this.displayPreferences(id);
      if (this.session?.AccessToken !== owner) throw new ApiError(401, 'session_changed', 'The active account changed before its preferences could be saved.');
      await this.request(`/emby/DisplayPreferences/${encodeURIComponent(id)}`, { method: 'POST', query: { UserId: this.userId(), Client: CLIENT }, body: { CustomPrefs: { ...current.CustomPrefs, ...values }, ...(current.Revision === undefined ? {} : { Revision: String(current.Revision) }) } });
    });
    this.preferenceWrites = task; return task;
  }

  mediaUrl(path: string): string {
    if (!path) return '';
    path = path.replace(/^\/(Videos|Audio|Items|Users)\//i, '/emby/$1/');
    const base = this.url('/');
    const absolute = /^https?:\/\//i.test(path) ? new URL(path) : new URL(`${this.base()}${path.startsWith('/') ? '' : '/'}${path}`, window.location.origin);
    if (absolute.origin !== base.origin) throw new ApiError(0, 'untrusted_media_url', 'The server returned media on an unexpected origin.');
    absolute.pathname = absolute.pathname.replace(/^\/(Videos|Audio|Items|Users)\//i, '/emby/$1/');
    if (this.session && !absolute.searchParams.has('api_key')) absolute.searchParams.set('api_key', this.session.AccessToken);
    return absolute.href;
  }
  streamUrl(id: string, mediaSourceId?: string): string { return this.mediaUrl(`/emby/Videos/${encodeURIComponent(id)}/stream?${new URLSearchParams({ Static: 'true', DeviceId: this.deviceId, ...(mediaSourceId ? { MediaSourceId: mediaSourceId } : {}) })}`); }
  imageUrl(item: MediaItem, type: 'Primary' | 'Backdrop' | 'Thumb' = 'Primary', width = 800): string {
    let id = item.Id;
    let tag = type === 'Backdrop' ? item.BackdropImageTags?.[0] : item.ImageTags?.[type];
    let kind = type;
    if (!tag && type === 'Backdrop' && item.ParentBackdropItemId && item.ParentBackdropImageTags?.length) { id = item.ParentBackdropItemId; tag = item.ParentBackdropImageTags[0]; }
    if (!tag && type === 'Primary' && item.SeriesId && item.SeriesPrimaryImageTag) { id = item.SeriesId; tag = item.SeriesPrimaryImageTag; }
    if (!tag && type === 'Backdrop' && item.ImageTags?.Thumb) { kind = 'Thumb'; tag = item.ImageTags.Thumb; }
    if (!tag && item.ImageTags?.Primary) { kind = 'Primary'; tag = item.ImageTags.Primary; }
    if (!tag) return '';
    const query = new URLSearchParams({ tag, MaxWidth: String(Math.max(64, Math.min(3840, width))), quality: '90' });
    return this.mediaUrl(`/emby/Items/${encodeURIComponent(id)}/Images/${kind}${kind === 'Backdrop' ? '/0' : ''}?${query}`);
  }
  userImageUrl(user: User, width = 96): string { return user.PrimaryImageTag ? this.mediaUrl(`/emby/Users/${encodeURIComponent(user.Id)}/Images/Primary?tag=${encodeURIComponent(user.PrimaryImageTag)}&MaxWidth=${width}`) : ''; }
  thumbnailSet(id: string, mediaSourceId?: string, width: 240 | 320 | 400 = 400, signal?: AbortSignal): Promise<{ AspectRatio: number; Thumbnails: { ImageTag: string; PositionTicks: number }[] }> {
    return this.request(`/emby/Items/${encodeURIComponent(id)}/ThumbnailSet`, { query: { Width: width, MediaSourceId: mediaSourceId }, signal });
  }
  itemThumbnailUrl(id: string, thumbnail: { ImageTag: string; PositionTicks: number }, width = 400): string {
    return this.mediaUrl(`/emby/Items/${encodeURIComponent(id)}/Images/Thumbnail?${new URLSearchParams({ PositionTicks: String(thumbnail.PositionTicks), tag: thumbnail.ImageTag, MaxWidth: String(width), quality: '90' })}`);
  }
  async subtitleText(stream: MediaStream, signal?: AbortSignal): Promise<string> {
    if (!stream.DeliveryUrl || !stream.IsTextSubtitleStream && !['srt', 'subrip', 'ass', 'ssa', 'webvtt', 'vtt', 'mov_text', 'ttml'].includes(stream.Codec?.toLowerCase() || '')) throw new ApiError(422, 'text_subtitle_unavailable', 'This track does not expose text subtitles.');
    const url = new URL(this.mediaUrl(stream.DeliveryUrl));
    url.pathname = url.pathname.replace(/\/Stream\.[^/]+$/i, '/Stream.vtt');
    const response = await fetch(url, { credentials: 'omit', signal, headers: { Accept: 'text/vtt' } });
    if (!response.ok) throw new ApiError(response.status, 'subtitle_unavailable', 'The subtitle track could not be loaded.');
    return response.text();
  }
  subtitleTimelinesUrl(id: string): string {
    return this.mediaUrl(`/emby/Items/${encodeURIComponent(id)}/SubtitleTimelines?${new URLSearchParams({ UserId: this.userId() })}`);
  }
  subtitleTimelineUrl(id: string, streamIndex: number, path: string): string {
    const url = new URL(this.mediaUrl(path));
    const expected = new URL(this.subtitleTimelinesUrl(id));
    if (url.pathname !== `${expected.pathname}/${streamIndex}` || url.hash || !url.searchParams.get('tag')) throw new ApiError(200, 'invalid_subtitle_timeline_url', 'The server returned an invalid subtitle timeline URL.');
    return url.href;
  }

  playbackInfo(id: string, options: PlaybackOptions = {}): Promise<PlaybackInfo> {
    const bitrate = ({ '1080p': 20_000_000, '720p': 8_000_000, '480p': 3_000_000 } as const)[options.quality as '1080p' | '720p' | '480p'];
    return this.request(`/emby/Items/${encodeURIComponent(id)}/PlaybackInfo`, { method: 'POST', body: {
      UserId: this.userId(), IsPlayback: true, StartTimeTicks: options.startTimeTicks ?? 0, MediaSourceId: options.mediaSourceId,
      AudioStreamIndex: options.audioStreamIndex, SubtitleStreamIndex: options.subtitleStreamIndex,
      CurrentPlaySessionId: options.currentPlaySessionId, MaxStreamingBitrate: options.external ? undefined : bitrate ?? 120_000_000,
      EnableDirectPlay: options.external || !bitrate, EnableDirectStream: options.external || !bitrate, EnableTranscoding: !options.external,
      AllowVideoStreamCopy: !bitrate, DeviceProfile: options.external ? undefined : browserDeviceProfile(options),
    } });
  }
  async negotiatePlayback(id: string, options: PlaybackOptions = {}): Promise<PlaybackSession> {
    if (!options.external && (options.audioStreamIndex === undefined || options.subtitleStreamIndex === undefined)) {
      const item = await this.item(id);
      const defaults = resolveItemPreferences(item);
      options = { ...defaults, ...Object.fromEntries(Object.entries(options).filter(([, value]) => value !== undefined)) };
    }
    const info = await this.playbackInfo(id, options);
    const source = info.MediaSources?.find(candidate => candidate.Id === options.mediaSourceId) ?? info.MediaSources?.[0];
    if (info.ErrorCode || !source) throw new ApiError(422, info.ErrorCode || 'NoCompatibleStream', 'No compatible playback stream is available for this browser and account.');
    let url: string;
    let method: PlaybackSession['method'];
    if (source.SupportsDirectPlay) { method = 'DirectPlay'; url = source.DirectStreamUrl ? this.mediaUrl(source.DirectStreamUrl) : this.streamUrl(id, source.Id); }
    else if (source.SupportsDirectStream && source.DirectStreamUrl) { method = 'DirectStream'; url = this.mediaUrl(source.DirectStreamUrl); }
    else if (source.SupportsTranscoding && source.TranscodingUrl) { method = 'Transcode'; url = this.mediaUrl(source.TranscodingUrl); }
    else throw new ApiError(422, 'NoCompatibleStream', 'The server cannot provide a playable stream for this browser.');
    const ownedUrl = new URL(url); ownedUrl.searchParams.set('PlaySessionId', info.PlaySessionId); ownedUrl.searchParams.set('DeviceId', this.deviceId);
    const selectedSubtitle = options.subtitleStreamIndex ?? source.DefaultSubtitleStreamIndex;
    if (source.DefaultSubtitleStreamIndex === undefined && selectedSubtitle !== undefined) source.DefaultSubtitleStreamIndex = selectedSubtitle;
    const subtitle = source.MediaStreams?.find(stream => stream.Type === 'Subtitle' && stream.Index === selectedSubtitle && stream.DeliveryMethod === 'External');
    return { info, source, url: ownedUrl.href, method, startTimeTicks: options.startTimeTicks ?? 0, timelineOffsetSeconds: 0,
      subtitleUrl: method !== 'Transcode' && subtitle?.DeliveryUrl ? this.mediaUrl(subtitle.DeliveryUrl) : undefined };
  }
  reportPlayback(event: 'Started' | 'Progress' | 'Stopped', payload: PlaybackEvent): Promise<void> {
    return this.request(`/emby/Sessions/Playing${event === 'Started' ? '' : `/${event}`}`, { method: 'POST', body: payload, keepalive: event === 'Stopped' });
  }
  stopActiveEncoding(playSessionId: string): Promise<void> { return this.request('/emby/Videos/ActiveEncodings', { method: 'DELETE', query: { PlaySessionId: playSessionId, DeviceId: this.deviceId }, keepalive: true }); }
}

export const api = new GobyApi();
export const imageUrl = (item: MediaItem, type: 'Primary' | 'Backdrop' | 'Thumb' = 'Primary', width?: number): string => api.imageUrl(item, type, width);
