import { adminApi, ApiError, authenticatedRequest } from './api';
import type { MetadataItemsQuery, MetadataItemsResponse, MetadataItemSummary, RequestOptions } from './api';

export interface MediaStreamInfo {
  Index: number;
  Type: string;
  Codec: string;
  Language: string;
  Title: string;
  Width: number | null;
  Height: number | null;
  Channels: number | null;
  SampleRate: number | null;
  Bitrate: string | null;
  Profile: string;
  Level: number | null;
  BitDepth: number | null;
  PixelFormat: string;
  FrameRate: string;
  ChannelLayout: string;
  VideoRange: string;
  ColorSpace: string;
  ColorTransfer: string;
  ColorPrimaries: string;
  IsDefault: boolean;
  IsForced: boolean;
  IsExternal: boolean;
  IsAttachedPicture: boolean;
  IsHearingImpaired: boolean;
}

export interface IndexedMediaAnalysis {
  Intro: string | null;
  Credits: string | null;
  PreviewCount: number;
  BackgroundPreview: string | null;
  AudioWaveform: string | null;
  SubtitleTimeline: string | null;
}

export interface MediaTechnicalSummary {
  State: 'available' | 'not_scanned' | 'not_applicable';
  DurationTicks: string | null;
  Size: string | null;
  Bitrate: string | null;
  Container: string;
  Video: MediaStreamInfo | null;
  Audio: MediaStreamInfo | null;
  Images: { Count: number; Types: string[] };
  Analysis: IndexedMediaAnalysis;
}

export interface MediaChapterInfo {
  Index: number;
  Name: string;
  StartTicks: string;
  EndTicks: string | null;
}

export interface MediaInfoResponse {
  Id: string;
  LibraryId: string;
  Summary: MediaTechnicalSummary;
  ProbeVersion: number | null;
  UpdatedAt: string;
  Streams: MediaStreamInfo[];
  Chapters: MediaChapterInfo[];
  StreamCount: number;
  ChapterCount: number;
  StreamsTruncated: boolean;
  ChaptersTruncated: boolean;
}

export interface MediaMetadataItem extends MetadataItemSummary { Media?: MediaTechnicalSummary }
export interface MediaMetadataItemsResponse extends MetadataItemsResponse { Items: MediaMetadataItem[] }

const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const count = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
const optionalCount = (value: unknown): value is number | null => value === null || count(value);
const text = (value: unknown): value is string => typeof value === 'string' && value.length <= 8192;
const optionalStatus = (value: unknown): value is string | null => value === null || typeof value === 'string' && /^[a-z][a-z0-9_]{0,63}$/.test(value);

function decimal(value: unknown): value is string {
  return typeof value === 'string' && /^(0|[1-9]\d{0,18})$/.test(value) && (value.length < 19 || value <= '9223372036854775807');
}
const optionalDecimal = (value: unknown): value is string | null => value === null || decimal(value);

function invalid(): never {
  throw new ApiError('The media details response is incomplete. Refresh to try again.', { code: 'invalid_response' });
}

function stream(value: unknown): value is MediaStreamInfo {
  return record(value) && count(value.Index)
    && ['Type', 'Codec', 'Language', 'Title', 'Profile', 'PixelFormat', 'FrameRate', 'ChannelLayout', 'VideoRange', 'ColorSpace', 'ColorTransfer', 'ColorPrimaries'].every((key) => text(value[key]))
    && ['Width', 'Height', 'Channels', 'SampleRate', 'Level', 'BitDepth'].every((key) => optionalCount(value[key]))
    && optionalDecimal(value.Bitrate)
    && ['IsDefault', 'IsForced', 'IsExternal', 'IsAttachedPicture', 'IsHearingImpaired'].every((key) => typeof value[key] === 'boolean');
}

function indexedAnalysis(value: unknown): value is IndexedMediaAnalysis {
  return record(value) && count(value.PreviewCount)
    && ['Intro', 'Credits', 'BackgroundPreview', 'AudioWaveform', 'SubtitleTimeline'].every((key) => optionalStatus(value[key]));
}

function summary(value: unknown): value is MediaTechnicalSummary {
  if (!record(value) || !['available', 'not_scanned', 'not_applicable'].includes(String(value.State))
    || !optionalDecimal(value.DurationTicks) || !optionalDecimal(value.Size) || !optionalDecimal(value.Bitrate) || !text(value.Container)
    || !(value.Video === null || stream(value.Video)) || !(value.Audio === null || stream(value.Audio))
    || !record(value.Images) || !count(value.Images.Count) || !Array.isArray(value.Images.Types) || value.Images.Types.length > 32
    || !value.Images.Types.every((entry) => typeof entry === 'string' && /^[A-Za-z]{1,32}$/.test(entry))
    || new Set(value.Images.Types).size !== value.Images.Types.length || !indexedAnalysis(value.Analysis)) return false;
  return value.Images.Count >= value.Images.Types.length;
}

export const mediaSummaryApi = {
  async items(libraryId: string, query: MetadataItemsQuery = {}, options: RequestOptions = {}): Promise<MediaMetadataItemsResponse> {
    const result = await adminApi.getLibraryItems(libraryId, query, options) as MediaMetadataItemsResponse;
    // Older servers omit this additive projection; absence remains unknown.
    if (result.Items.some((item) => item.Media !== undefined && !summary(item.Media))) invalid();
    return result;
  },
  async info(itemId: string, options: RequestOptions = {}): Promise<MediaInfoResponse> {
    const result = await authenticatedRequest<MediaInfoResponse>(`/items/${encodeURIComponent(itemId)}/media-info`, options);
    if (!record(result) || result.Id !== itemId || !text(result.LibraryId) || !result.LibraryId || !summary(result.Summary)
      || !optionalCount(result.ProbeVersion) || typeof result.UpdatedAt !== 'string' || !Number.isFinite(Date.parse(result.UpdatedAt))
      || !Array.isArray(result.Streams) || result.Streams.length > 256 || !result.Streams.every(stream)
      || new Set(result.Streams.map((entry) => entry.Index)).size !== result.Streams.length
      || !Array.isArray(result.Chapters) || result.Chapters.length > 4096
      || !result.Chapters.every((chapter) => record(chapter) && count(chapter.Index) && text(chapter.Name) && decimal(chapter.StartTicks)
        && optionalDecimal(chapter.EndTicks) && (chapter.EndTicks === null || BigInt(chapter.EndTicks) >= BigInt(chapter.StartTicks)))
      || new Set(result.Chapters.map((chapter) => chapter.Index)).size !== result.Chapters.length
      || !count(result.StreamCount) || !count(result.ChapterCount) || typeof result.StreamsTruncated !== 'boolean' || typeof result.ChaptersTruncated !== 'boolean'
      || result.StreamCount < result.Streams.length || result.ChapterCount < result.Chapters.length
      || result.StreamsTruncated !== (result.StreamCount > result.Streams.length) || result.ChaptersTruncated !== (result.ChapterCount > result.Chapters.length)) invalid();
    return result;
  },
};

export function mediaDuration(ticks: string | null | undefined): string | null {
  if (ticks === undefined || ticks === null) return null;
  const seconds = BigInt(ticks) / 10_000_000n;
  const hours = seconds / 3600n;
  const minutes = String(seconds % 3600n / 60n).padStart(2, '0');
  const remainder = String(seconds % 60n).padStart(2, '0');
  return hours > 0n ? `${hours}:${minutes}:${remainder}` : `${minutes}:${remainder}`;
}

function scaledDecimal(value: string, base: bigint, units: readonly string[], locale: string): string {
  const quantity = BigInt(value);
  let divisor = 1n, index = 0;
  while (quantity >= divisor * base && index < units.length - 1) { divisor *= base; index++; }
  const whole = quantity / divisor;
  const tenth = quantity % divisor * 10n / divisor;
  const formatted = index === 0 || tenth === 0n
    ? new Intl.NumberFormat(locale).format(whole)
    : new Intl.NumberFormat(locale, { minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(Number(whole) + Number(tenth) / 10);
  return `${formatted} ${units[index]}`;
}

export function mediaSize(value: string | null | undefined, locale: string): string | null {
  return value === undefined || value === null ? null : scaledDecimal(value, 1024n, ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB'], locale);
}

export function mediaBitrate(value: string | null | undefined, locale: string): string | null {
  return value === undefined || value === null ? null : scaledDecimal(value, 1000n, ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps', 'Pbps', 'Ebps'], locale);
}
