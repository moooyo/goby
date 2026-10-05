export type Quality = 'original' | '1080p' | '720p' | '480p';
export type SubtitleMode = 'Default' | 'Always' | 'OnlyForced' | 'None' | 'Smart' | 'HearingImpaired';
export type BackgroundSource = 'theme' | 'generated' | 'trailer';

export interface UserConfiguration {
  AudioLanguagePreference?: string;
  SubtitleLanguagePreference?: string;
  EnableNextEpisodeAutoPlay?: boolean;
  IntroSkipMode?: 'None' | 'ShowButton' | 'AutoSkip';
  PlayDefaultAudioTrack?: boolean;
  RememberAudioSelections?: boolean;
  RememberSubtitleSelections?: boolean;
  ResumeRewindSeconds?: number;
  SubtitleMode?: SubtitleMode;
  HidePlayedInLatest?: boolean;
  HidePlayedInMoreLikeThis?: boolean;
  HidePlayedInSuggestions?: boolean;
  DisplayMissingEpisodes?: boolean;
  OrderedViews?: string[];
  MyMediaExcludes?: string[];
  LatestItemsExcludes?: string[];
}

export interface User {
  Id: string;
  Name: string;
  ServerId?: string;
  HasPassword?: boolean;
  HasConfiguredPassword?: boolean;
  PrimaryImageTag?: string;
  Policy?: {
    IsAdministrator?: boolean;
    IsDisabled?: boolean;
    EnableMediaPlayback?: boolean;
    EnableVideoPlaybackTranscoding?: boolean;
    EnableAudioPlaybackTranscoding?: boolean;
    EnablePlaybackRemuxing?: boolean;
    EnableUserPreferenceAccess?: boolean;
  };
  Configuration?: UserConfiguration;
}

export interface AuthSession {
  serverUrl: string;
  AccessToken: string;
  User: User;
  ServerId: string;
  SessionInfo?: { Id: string; DeviceId?: string };
}

export interface SystemInfo {
  Id: string;
  ServerName: string;
  ProductName?: string;
  Version: string;
  GobyVersion?: string;
  LocalAddress?: string;
  StartupWizardCompleted?: boolean;
}

export interface UserData {
  ItemId?: string;
  PlaybackPositionTicks?: number;
  PlayedPercentage?: number;
  PlayCount?: number;
  IsFavorite?: boolean;
  Played?: boolean;
  LastPlayedDate?: string;
  UnplayedItemCount?: number;
  HideFromResume?: boolean;
}

export interface MediaPerson {
  Id?: string;
  Name: string;
  Type?: string;
  Role?: string;
  PrimaryImageTag?: string;
}

export interface Chapter {
  StartPositionTicks: number;
  Name?: string;
  MarkerType?: 'Chapter' | 'IntroStart' | 'IntroEnd' | string;
  ChapterIndex?: number;
  ImageTag?: string;
}

export interface MediaStream {
  Index: number;
  Type: 'Video' | 'Audio' | 'Subtitle' | 'Attachment' | string;
  Codec?: string;
  Language?: string;
  DisplayLanguage?: string;
  Title?: string;
  DisplayTitle?: string;
  Width?: number;
  Height?: number;
  BitRate?: number;
  BitDepth?: number;
  Channels?: number;
  ChannelLayout?: string;
  SampleRate?: number;
  Profile?: string;
  Level?: number;
  RealFrameRate?: number;
  AverageFrameRate?: number;
  VideoRange?: string;
  VideoRangeType?: string;
  ColorTransfer?: string;
  ColorPrimaries?: string;
  ColorSpace?: string;
  PixelFormat?: string;
  DvProfile?: number;
  DvLevel?: number;
  DvBlSignalCompatibilityId?: number;
  RpuPresentFlag?: boolean;
  ElPresentFlag?: boolean;
  BlPresentFlag?: boolean;
  IsDefault?: boolean;
  IsForced?: boolean;
  IsExternal?: boolean;
  IsTextSubtitleStream?: boolean;
  GobySubtitleTimelineOnly?: boolean;
  IsHearingImpaired?: boolean;
  DeliveryMethod?: string;
  DeliveryUrl?: string;
  SupportsExternalStream?: boolean;
}

export interface GobyCreditsInterval {
  StartPositionTicks: number;
  EndPositionTicks: number;
  Source: string;
}

export interface MediaSource {
  Id: string;
  ItemId?: string;
  Name?: string;
  Container?: string;
  RunTimeTicks?: number;
  Bitrate?: number;
  Size?: number;
  MediaStreams?: MediaStream[];
  Chapters?: Chapter[];
  GobyCreditsIntervals?: GobyCreditsInterval[];
  SupportsDirectPlay?: boolean;
  SupportsDirectStream?: boolean;
  SupportsTranscoding?: boolean;
  DirectStreamUrl?: string;
  TranscodingUrl?: string;
  TranscodingContainer?: string;
  TranscodingSubProtocol?: string;
  DefaultAudioStreamIndex?: number;
  DefaultSubtitleStreamIndex?: number;
  RequiresOpening?: boolean;
  RequiresClosing?: boolean;
  IsInfiniteStream?: boolean;
}

export interface MediaItem {
  Id: string;
  Name: string;
  Type: 'Movie' | 'Series' | 'Season' | 'Episode' | 'CollectionFolder' | string;
  SortName?: string;
  OriginalTitle?: string;
  Overview?: string;
  ProductionYear?: number;
  PremiereDate?: string;
  CommunityRating?: number;
  OfficialRating?: string;
  Genres?: string[];
  Tags?: string[];
  RunTimeTicks?: number;
  DateCreated?: string;
  IsFolder?: boolean;
  CanPlay?: boolean;
  IsMissing?: boolean;
  CollectionType?: string | null;
  ChildCount?: number;
  RecursiveItemCount?: number;
  ParentId?: string;
  SeriesId?: string;
  SeriesName?: string;
  SeasonId?: string;
  SeasonName?: string;
  IndexNumber?: number;
  ParentIndexNumber?: number;
  UserData?: UserData;
  ImageTags?: Record<string, string>;
  BackdropImageTags?: string[];
  ParentBackdropItemId?: string;
  ParentBackdropImageTags?: string[];
  ParentThumbItemId?: string;
  ParentThumbImageTag?: string;
  SeriesPrimaryImageTag?: string;
  PrimaryImageAspectRatio?: number;
  MediaSources?: MediaSource[];
  MediaStreams?: MediaStream[];
  Chapters?: Chapter[];
  GobyCreditsIntervals?: GobyCreditsInterval[];
  People?: MediaPerson[];
  Studios?: { Id?: string; Name: string }[];
  Container?: string;
  LocalTrailerCount?: number;
}

export interface ItemsResponse {
  Items: MediaItem[];
  TotalRecordCount: number;
}

export interface SearchHint {
  Id: string;
  ItemId: string;
  Name: string;
  Type: 'Movie' | 'Series' | 'Person' | 'Genre';
  MatchedTerm: string;
  GobyReference: { Kind: 'Item' | 'Entity'; Id: string };
  GobyNavigationUrl?: string;
  ProductionYear?: number;
  PrimaryImageTag?: string;
  BackdropImageTag?: string;
  ThumbImageTag?: string;
  PrimaryImageUrl?: string;
  BackdropImageUrl?: string;
  ThumbImageUrl?: string;
  PrimaryImageAspectRatio?: number;
}

export interface SearchHintsResponse {
  SearchHints: SearchHint[];
  TotalRecordCount: number;
}

export interface ThemeMediaResponse {
  ThemeVideosResult: ItemsResponse & { OwnerId: string };
  ThemeSongsResult: ItemsResponse & { OwnerId: string };
  SoundtrackSongsResult: ItemsResponse & { OwnerId: string };
}

export interface BackgroundPreviewResponse {
  Available: boolean;
  StreamUrl?: string;
  RunTimeTicks?: number;
  StartPositionTicks?: number;
  Width?: number;
  Height?: number;
  Size?: number;
}

export interface ItemQuery {
  ParentId?: string;
  IncludeItemTypes?: string;
  MediaTypes?: string;
  Ids?: string;
  Recursive?: boolean;
  Limit?: number;
  StartIndex?: number;
  SortBy?: string;
  SortOrder?: 'Ascending' | 'Descending';
  SearchTerm?: string;
  Genres?: string;
  GenreIds?: string;
  Person?: string;
  PersonIds?: string;
  PersonTypes?: string;
  Years?: string;
  Filters?: string;
  IsPlayed?: boolean;
  IsHD?: boolean;
  Is4K?: boolean;
  ExtendedVideoTypes?: string;
  GobyAggregateVideoFilters?: boolean;
  MinWidth?: number;
  MaxWidth?: number;
  MinHeight?: number;
  MaxHeight?: number;
  IsFavorite?: boolean;
  IsFolder?: boolean;
  Fields?: string;
  GroupItems?: boolean;
  EnableImages?: boolean;
}

export interface PlaybackInfo {
  PlaySessionId: string;
  MediaSources: MediaSource[];
  ErrorCode?: string;
}

export interface PlaybackOptions {
  quality?: Quality;
  audioStreamIndex?: number;
  subtitleStreamIndex?: number;
  startTimeTicks?: number;
  mediaSourceId?: string;
  currentPlaySessionId?: string;
  external?: boolean;
}

export interface PlaybackSession {
  info: PlaybackInfo;
  source: MediaSource;
  url: string;
  method: 'DirectPlay' | 'DirectStream' | 'Transcode';
  startTimeTicks: number;
  timelineOffsetSeconds: number;
  subtitleUrl?: string;
}

export interface PlaybackEvent {
  ItemId: string;
  MediaSourceId: string;
  PlaySessionId: string;
  PositionTicks?: number;
  IsPaused?: boolean;
  PlayMethod?: 'DirectPlay' | 'DirectStream' | 'Transcode';
  AudioStreamIndex?: number;
  SubtitleStreamIndex?: number;
  CanSeek?: boolean;
  IsMuted?: boolean;
  VolumeLevel?: number;
  PlaybackRate?: number;
  EventName?: string;
}

export interface PlayerPreferences {
  quality: Quality;
  audioLanguage: string;
  subtitleLanguage: string;
  subtitleMode: SubtitleMode;
  autoNext: boolean;
  autoSkip: boolean;
  subtitleSize: 0 | 1 | 2 | 3;
  subtitleStyle: 'shadow' | 'outline' | 'background';
  heroRotate: boolean;
  backgroundMotion: boolean;
  backgroundSources: BackgroundSource[];
  player: string;
}

export interface ItemPreferences {
  quality?: Quality;
  audioStreamIndex?: number;
  subtitleStreamIndex?: number;
  player?: string;
}

export interface DisplayPreferences {
  Id?: string;
  Client?: string;
  Revision?: string;
  CustomPrefs: Record<string, string>;
  SortBy?: string;
  SortOrder?: string;
}
