import { api, ApiError } from './api';

const bucketCounts = [512, 1024, 2048, 4096] as const;
const headerBytes = 32;
const maxDescriptorBytes = 256 * 1024;

export interface AudioWaveformLevel {
  bucketCount: number;
  url: string;
}

export interface AudioWaveformStream {
  streamIndex: number;
  channels: number;
  sampleRate: number;
  channelLayout?: string;
  levels: AudioWaveformLevel[];
}

export interface AudioWaveforms {
  available: boolean;
  stale: boolean;
  mediaSourceId?: string;
  sourceVersion?: string;
  durationTicks?: bigint;
  streams: AudioWaveformStream[];
}

export interface AudioWaveformData {
  bucketCount: number;
  peaks: Uint16Array;
  rms: Uint16Array;
  validity: Uint8Array;
}

const invalid = () => new ApiError(200, 'invalid_audio_waveform', 'The server returned invalid audio waveform data.');
const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const integer = (value: unknown, min: number, max: number): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= min && value <= max;

function durationTicks(value: unknown): bigint {
  if (!(integer(value, 1, Number.MAX_SAFE_INTEGER) || typeof value === 'string' && /^[1-9]\d{0,18}$/.test(value))) throw invalid();
  const ticks = BigInt(value);
  if (ticks > 9_223_372_036_854_775_807n) throw invalid();
  return ticks;
}

export function parseAudioWaveforms(value: unknown): AudioWaveforms {
  if (!record(value) || typeof value.Available !== 'boolean' || value.Stale !== undefined && typeof value.Stale !== 'boolean') throw invalid();
  if (value.MediaSourceId !== undefined && (typeof value.MediaSourceId !== 'string' || !value.MediaSourceId)
    || value.SourceVersion !== undefined && (typeof value.SourceVersion !== 'string' || !value.SourceVersion)) throw invalid();
  if (!value.Available) return { available: false, stale: value.Stale === true, streams: [] };
  const ticks = durationTicks(value.DurationTicks);
  if (!Array.isArray(value.Streams) || !value.Streams.length || value.Streams.length > 128) throw invalid();
  const indices = new Set<number>();
  const streams = value.Streams.map(entry => {
    if (!record(entry) || !integer(entry.StreamIndex, 0, 0xffff_ffff) || indices.has(entry.StreamIndex)
      || !integer(entry.Channels, 1, 128) || !integer(entry.SampleRate, 1, 768_000)
      || entry.ChannelLayout !== undefined && (typeof entry.ChannelLayout !== 'string' || entry.ChannelLayout.length > 128)
      || !Array.isArray(entry.Levels) || !entry.Levels.length || entry.Levels.length > bucketCounts.length) throw invalid();
    indices.add(entry.StreamIndex);
    const seen = new Set<number>();
    const levels = entry.Levels.map(level => {
      if (!record(level) || !integer(level.BucketCount, 512, 4096) || !bucketCounts.some(count => count === level.BucketCount)
        || seen.has(level.BucketCount) || typeof level.Url !== 'string' || !level.Url || level.Url.length > 4096) throw invalid();
      // Resolve every URL before any fetch so a malformed response cannot start partial requests.
      const url = api.mediaUrl(level.Url);
      seen.add(level.BucketCount);
      return { bucketCount: level.BucketCount, url };
    }).sort((left, right) => left.bucketCount - right.bucketCount);
    return { streamIndex: entry.StreamIndex, channels: entry.Channels, sampleRate: entry.SampleRate, channelLayout: entry.ChannelLayout as string | undefined, levels };
  });
  return { available: true, stale: value.Stale === true, durationTicks: ticks, mediaSourceId: value.MediaSourceId as string | undefined, sourceVersion: value.SourceVersion as string | undefined, streams };
}

export function parseAudioWaveform(buffer: ArrayBuffer, streamIndex: number, level: AudioWaveformLevel, ticks: bigint): AudioWaveformData {
  const count = level.bucketCount;
  const validityBytes = Math.ceil(count / 8);
  if (buffer.byteLength !== headerBytes + count * 4 + validityBytes) throw invalid();
  const view = new DataView(buffer);
  if (view.getUint32(0, false) !== 0x4741574c || view.getUint16(4, true) !== 1 || view.getUint16(6, true) !== headerBytes
    || view.getUint32(8, true) !== streamIndex || view.getUint32(12, true) !== count || view.getBigUint64(16, true) !== ticks
    || view.getUint32(24, true) !== validityBytes || view.getUint32(28, true) !== 0) throw invalid();
  const peaks = new Uint16Array(count);
  const rms = new Uint16Array(count);
  const validity = new Uint8Array(buffer, headerBytes + count * 4, validityBytes);
  for (let index = 0; index < count; index += 1) {
    peaks[index] = view.getUint16(headerBytes + index * 4, true);
    rms[index] = view.getUint16(headerBytes + index * 4 + 2, true);
    if (rms[index] > peaks[index]) throw invalid();
  }
  return { bucketCount: count, peaks, rms, validity };
}

async function boundedBody(response: Response, limit: number): Promise<ArrayBuffer> {
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
  const body = new Uint8Array(size);
  let offset = 0;
  for (const part of parts) { body.set(part, offset); offset += part.byteLength; }
  return body.buffer;
}

function assertOwner(token: string | undefined, signal: AbortSignal) {
  if (signal.aborted) throw new DOMException('The waveform request was cancelled.', 'AbortError');
  if (!token || api.getSession()?.AccessToken !== token) throw new ApiError(401, 'session_changed', 'The active account changed before its waveform was loaded.');
}

export async function loadAudioWaveforms(itemId: string, sourceId: string | undefined, signal: AbortSignal): Promise<AudioWaveforms> {
  const session = api.getSession();
  assertOwner(session?.AccessToken, signal);
  const query = new URLSearchParams({ UserId: session!.User.Id });
  const response = await fetch(api.mediaUrl(`/emby/Items/${encodeURIComponent(itemId)}/AudioWaveforms?${query}`), { credentials: 'omit', cache: 'no-store', signal, headers: { Accept: 'application/json' } });
  assertOwner(session?.AccessToken, signal);
  if (!response.ok) throw new ApiError(response.status, 'audio_waveforms_unavailable', 'Audio waveforms could not be loaded.');
  const body = await boundedBody(response, maxDescriptorBytes);
  assertOwner(session?.AccessToken, signal);
  let value: unknown;
  try { value = JSON.parse(new TextDecoder().decode(body)); } catch { throw invalid(); }
  const descriptor = parseAudioWaveforms(value);
  if (descriptor.available && descriptor.mediaSourceId && sourceId && descriptor.mediaSourceId !== sourceId) return { available: false, stale: true, streams: [] };
  return descriptor;
}

export async function loadAudioWaveform(streamIndex: number, level: AudioWaveformLevel, ticks: bigint, signal: AbortSignal): Promise<AudioWaveformData> {
  const token = api.getSession()?.AccessToken;
  assertOwner(token, signal);
  const response = await fetch(level.url, { credentials: 'omit', cache: 'no-store', signal, headers: { Accept: 'application/octet-stream' } });
  assertOwner(token, signal);
  if (!response.ok) throw new ApiError(response.status, 'audio_waveform_unavailable', 'The audio waveform could not be loaded.');
  if (response.headers.get('Content-Type')?.split(';')[0].trim().toLowerCase() !== 'application/octet-stream') { await response.body?.cancel(); throw invalid(); }
  const body = await boundedBody(response, headerBytes + level.bucketCount * 4 + Math.ceil(level.bucketCount / 8));
  assertOwner(token, signal);
  return parseAudioWaveform(body, streamIndex, level, ticks);
}

export function chooseWaveformLevel(levels: AudioWaveformLevel[], width: number): AudioWaveformLevel | undefined {
  const target = Math.max(512, Math.min(4096, width || 1024));
  return levels.find(level => level.bucketCount >= target) ?? levels.at(-1);
}

export function waveformBucketValid(data: AudioWaveformData, index: number): boolean {
  return (data.validity[index >> 3] & (1 << (index & 7))) !== 0;
}
