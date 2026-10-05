import { api, ApiError } from './api';
import type { MediaStream } from '../types';

const maxDescriptorBytes = 64 * 1024;
const maxTimelineBytes = 1024 * 1024;
const maxTracks = 64;
const maxIntervals = 10_000;
const maxTicks = 9_223_372_036_854_775_807n;

export interface SubtitleTimelineStream {
  streamIndex: number;
  codec: string;
  intervalCount: number;
  url: string;
}

export interface SubtitleTimelines {
  available: boolean;
  mediaSourceId?: string;
  sourceVersion?: string;
  durationTicks?: bigint;
  streams: SubtitleTimelineStream[];
}

export interface SubtitleInterval { startTicks: bigint; endTicks: bigint }

const invalid = () => new ApiError(200, 'invalid_subtitle_timeline', 'The server returned invalid subtitle timeline data.');
const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const integer = (value: unknown, min: number, max: number): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= min && value <= max;
const identity = (value: unknown): value is string => typeof value === 'string' && value.length > 0 && value.length <= 256;
export const isTextSubtitle = (stream: MediaStream) => !!stream.DeliveryUrl && (stream.IsTextSubtitleStream || /^(srt|subrip|ass|ssa|vtt|webvtt|mov_text|ttml)$/i.test(stream.Codec ?? ''));
export const isBitmapSubtitle = (stream: MediaStream) => /^(hdmv_pgs_subtitle|dvd_subtitle|pgssub|dvdsub|pgs)$/i.test(stream.Codec ?? '');

function ticks(value: unknown, allowZero = false): bigint {
  if (!(integer(value, allowZero ? 0 : 1, Number.MAX_SAFE_INTEGER) || typeof value === 'string' && (allowZero ? /^(0|[1-9]\d{0,18})$/ : /^[1-9]\d{0,18}$/).test(value))) throw invalid();
  const result = BigInt(value);
  if (result > maxTicks) throw invalid();
  return result;
}

export function sourceClock(value: unknown): bigint | undefined {
  try { return ticks(value); } catch { return undefined; }
}

export function parseSubtitleTimelines(value: unknown, itemId: string, sourceId: string | undefined, sourceTicks: bigint | undefined): SubtitleTimelines {
  if (!record(value) || typeof value.Available !== 'boolean' || value.Stale !== undefined && typeof value.Stale !== 'boolean') throw invalid();
  if (!value.Available || value.Stale === true) return { available: false, streams: [] };
  if (!identity(value.MediaSourceId) || !identity(value.SourceVersion)) throw invalid();
  const durationTicks = ticks(value.DurationTicks);
  // Never draw results against a different source or a different source clock.
  if (!sourceId || value.MediaSourceId !== sourceId || sourceTicks === undefined || durationTicks !== sourceTicks) return { available: false, streams: [] };
  if (!Array.isArray(value.Streams) || value.Streams.length > maxTracks) throw invalid();
  const seen = new Set<number>();
  const streams = value.Streams.map(entry => {
    if (!record(entry) || !integer(entry.StreamIndex, 0, 0x7fff_ffff) || seen.has(entry.StreamIndex)
      || typeof entry.Codec !== 'string' || !/^(hdmv_pgs_subtitle|dvd_subtitle|pgssub|dvdsub|pgs)$/i.test(entry.Codec)
      || !integer(entry.IntervalCount, 0, maxIntervals) || typeof entry.Url !== 'string' || !entry.Url || entry.Url.length > 4096) throw invalid();
    seen.add(entry.StreamIndex);
    // Validate all paths before requesting any interval document.
    return { streamIndex: entry.StreamIndex, codec: entry.Codec.toLowerCase(), intervalCount: entry.IntervalCount, url: api.subtitleTimelineUrl(itemId, entry.StreamIndex, entry.Url) };
  });
  return { available: true, mediaSourceId: value.MediaSourceId, sourceVersion: value.SourceVersion, durationTicks, streams };
}

export function parseSubtitleTimeline(value: unknown, descriptor: SubtitleTimelines, stream: SubtitleTimelineStream): SubtitleInterval[] {
  if (!record(value) || !descriptor.available || value.MediaSourceId !== descriptor.mediaSourceId || value.SourceVersion !== descriptor.sourceVersion
    || value.StreamIndex !== stream.streamIndex || ticks(value.DurationTicks) !== descriptor.durationTicks
    || !Array.isArray(value.Intervals) || value.Intervals.length !== stream.intervalCount || value.Intervals.length > maxIntervals) throw invalid();
  let previousEnd = 0n;
  return value.Intervals.map(entry => {
    if (!record(entry)) throw invalid();
    const startTicks = ticks(entry.StartTicks, true);
    const endTicks = ticks(entry.EndTicks);
    if (startTicks < previousEnd || endTicks <= startTicks || endTicks > descriptor.durationTicks!) throw invalid();
    previousEnd = endTicks;
    return { startTicks, endTicks };
  });
}

async function boundedJSON(response: Response, limit: number): Promise<unknown> {
  if (response.headers.get('Content-Type')?.split(';')[0].trim().toLowerCase() !== 'application/json') { await response.body?.cancel(); throw invalid(); }
  const length = response.headers.get('Content-Length');
  if (length && (!/^\d+$/.test(length) || Number(length) > limit)) { await response.body?.cancel(); throw invalid(); }
  if (!response.body) throw invalid();
  const reader = response.body.getReader();
  const parts: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > limit) { await reader.cancel(); throw invalid(); }
      parts.push(value);
    }
  } finally { reader.releaseLock(); }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const part of parts) { bytes.set(part, offset); offset += part.byteLength; }
  try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)); } catch { throw invalid(); }
}

function assertOwner(owner: string, signal: AbortSignal) {
  if (signal.aborted) throw new DOMException('The subtitle timeline request was cancelled.', 'AbortError');
  if (owner !== subtitleTimelineOwner()) throw new ApiError(401, 'session_changed', 'The active account changed before its subtitle timeline was loaded.');
}

export function subtitleTimelineOwner(): string {
  const session = api.getSession();
  if (!session) throw new ApiError(401, 'unauthorized', 'Sign in to continue.');
  return JSON.stringify([session.serverUrl, session.ServerId, session.User.Id, session.AccessToken]);
}

async function readJSON(url: string, limit: number, signal: AbortSignal): Promise<unknown> {
  const owner = subtitleTimelineOwner();
  assertOwner(owner, signal);
  const response = await fetch(url, { credentials: 'omit', cache: 'no-store', redirect: 'error', signal, headers: { Accept: 'application/json' } });
  assertOwner(owner, signal);
  if (!response.ok) { await response.body?.cancel(); throw new ApiError(response.status, 'subtitle_timeline_unavailable', 'The subtitle timeline could not be loaded.'); }
  const body = await boundedJSON(response, limit);
  assertOwner(owner, signal);
  return body;
}

export async function loadSubtitleTimelines(itemId: string, sourceId: string | undefined, sourceTicks: bigint | undefined, signal: AbortSignal): Promise<SubtitleTimelines> {
  return parseSubtitleTimelines(await readJSON(api.subtitleTimelinesUrl(itemId), maxDescriptorBytes, signal), itemId, sourceId, sourceTicks);
}

export async function loadSubtitleTimeline(descriptor: SubtitleTimelines, stream: SubtitleTimelineStream, signal: AbortSignal): Promise<SubtitleInterval[]> {
  return parseSubtitleTimeline(await readJSON(stream.url, maxTimelineBytes, signal), descriptor, stream);
}

export function subtitleIntervalPath(intervals: SubtitleInterval[], durationTicks: bigint): string {
  if (durationTicks <= 0n) return '';
  // Keep the actual coverage; minimum-width bars would fill genuine gaps.
  return intervals.map(interval => {
    const left = Number(interval.startTicks) / Number(durationTicks) * 1000;
    const right = Number(interval.endTicks) / Number(durationTicks) * 1000;
    return `M${left} 2H${right}V8H${left}Z`;
  }).join('');
}

export function textSubtitlePath(text: string, durationTicks: bigint): string {
  const toTicks = (value: string): bigint | undefined => {
    const parts = value.replace(',', '.').split(':');
    const seconds = Number(parts.at(-1));
    const minutes = Number(parts.at(-2));
    if (seconds >= 60 || minutes >= 60) return undefined;
    const valueTicks = Math.round(parts.reduce((result, part) => result * 60 + Number(part), 0) * 10_000_000);
    return Number.isSafeInteger(valueTicks) && valueTicks >= 0 ? BigInt(valueTicks) : undefined;
  };
  const pattern = /((?:\d+:)?\d{2}:\d{2}[.,]\d{3})\s+-->\s+((?:\d+:)?\d{2}:\d{2}[.,]\d{3})/g;
  const intervals: SubtitleInterval[] = [];
  for (const match of text.matchAll(pattern)) {
    const startTicks = toTicks(match[1]);
    const endTicks = toTicks(match[2]);
    if (startTicks === undefined || endTicks === undefined || startTicks >= durationTicks || endTicks <= startTicks) continue;
    intervals.push({ startTicks, endTicks: endTicks < durationTicks ? endTicks : durationTicks });
    if (intervals.length === maxIntervals) break;
  }
  return subtitleIntervalPath(intervals, durationTicks);
}
