export const audioWaveformTaskKey = 'media.audio_waveform_generation';
export const audioWaveformStates = ['missing', 'pending', 'running', 'ready', 'failed', 'cancelled'] as const;
export type AudioWaveformState = typeof audioWaveformStates[number];
export const audioWaveformStateLabels: Record<AudioWaveformState, string> = { missing: '尚未生成', pending: '等待生成', running: '正在生成', ready: '任务完成', failed: '生成失败', cancelled: '已取消' };

export interface AudioWaveformTrack {
  StreamIndex: number;
  Channels: number;
  SampleRate: number;
  ChannelLayout: string;
  SampleCount: number;
  CoverageStartTicks: number;
  CoverageEndTicks: number;
}
export interface AudioWaveformArtifact { Available: boolean; Stale: boolean; DurationTicks: number; Tracks: AudioWaveformTrack[] }
export interface AudioWaveformItem {
  ItemId: string; LibraryId: string; Name: string; SourceRevision: string; DurationTicks: number; AudioStreamCount: number;
  State: AudioWaveformState; RequestedRevision: string; CompletedRevision: string; RunId: string; Reused: boolean; ErrorCode: string;
  RequestedAt: string | null; StartedAt: string | null; FinishedAt: string | null;
}
export interface AudioWaveformDetail extends AudioWaveformItem { Artifact: AudioWaveformArtifact }
export interface AudioWaveformPage { Items: AudioWaveformItem[]; TotalRecordCount: number; StartIndex: number; Limit: number }
export interface AudioWaveformRunInput { Kind: 'waveform'; RequestId: string; LibraryIds: string[]; ItemIds: string[]; Force: boolean }
export interface AudioWaveformRunReceipt { RunId: string; TaskId: string; Admitted: boolean; Queued: number }
export interface AudioWaveformActivity { pending: boolean; busy: boolean }

const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const count = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
const nonempty = (value: unknown): value is string => typeof value === 'string' && value.length > 0;
const revision = (value: unknown): value is string => typeof value === 'string' && /^(0|[1-9]\d*)$/.test(value) && value.length <= 19 && (value.length < 19 || value <= '9223372036854775807');
const timestamp = (value: unknown) => value === null || typeof value === 'string' && Number.isFinite(Date.parse(value));
export const activeAudioWaveform = (value?: AudioWaveformItem) => value?.State === 'pending' || value?.State === 'running';

export function decodeAudioWaveformItem(value: unknown): AudioWaveformItem | undefined {
  if (!record(value) || !nonempty(value.ItemId) || !nonempty(value.LibraryId) || typeof value.Name !== 'string' || !nonempty(value.SourceRevision)
    || !count(value.DurationTicks) || !count(value.AudioStreamCount) || typeof value.State !== 'string' || !audioWaveformStates.includes(value.State as AudioWaveformState)
    || !revision(value.RequestedRevision) || !revision(value.CompletedRevision) || typeof value.RunId !== 'string' || typeof value.Reused !== 'boolean' || typeof value.ErrorCode !== 'string'
    || !timestamp(value.RequestedAt) || !timestamp(value.StartedAt) || !timestamp(value.FinishedAt)) return undefined;
  return value as unknown as AudioWaveformItem;
}

export function decodeAudioWaveformArtifact(value: unknown): AudioWaveformArtifact | undefined {
  if (!record(value) || typeof value.Available !== 'boolean' || 'Stale' in value && typeof value.Stale !== 'boolean'
    || 'DurationTicks' in value && !count(value.DurationTicks) || 'Tracks' in value && !Array.isArray(value.Tracks)) return undefined;
  const duration = value.DurationTicks as number | undefined ?? 0;
  const tracks = value.Tracks as unknown[] | undefined ?? [];
  if (value.Available && (duration <= 0 || tracks.length === 0) || tracks.length > 64) return undefined;
  const output: AudioWaveformTrack[] = [];
  const indices = new Set<number>();
  for (const track of tracks) {
    if (!record(track) || !count(track.StreamIndex) || indices.has(track.StreamIndex) || !count(track.Channels) || track.Channels === 0
      || !count(track.SampleRate) || track.SampleRate === 0 || !count(track.SampleCount) || track.SampleCount === 0 || 'ChannelLayout' in track && typeof track.ChannelLayout !== 'string'
      || 'CoverageStartTicks' in track && !count(track.CoverageStartTicks) || 'CoverageEndTicks' in track && !count(track.CoverageEndTicks)) return undefined;
    const start = track.CoverageStartTicks as number | undefined ?? 0;
    const end = track.CoverageEndTicks as number | undefined ?? duration;
    if (start > end || end > duration) return undefined;
    indices.add(track.StreamIndex);
    output.push({ StreamIndex: track.StreamIndex, Channels: track.Channels, SampleRate: track.SampleRate,
      ChannelLayout: track.ChannelLayout as string | undefined ?? '', SampleCount: track.SampleCount, CoverageStartTicks: start, CoverageEndTicks: end });
  }
  return { Available: value.Available, Stale: value.Stale as boolean | undefined ?? false, DurationTicks: duration, Tracks: output };
}

export function decodeAudioWaveformDetail(value: unknown, itemId: string): AudioWaveformDetail | undefined {
  const item = decodeAudioWaveformItem(value);
  if (!item || item.ItemId !== itemId || !record(value)) return undefined;
  const artifact = decodeAudioWaveformArtifact(value.Artifact);
  return artifact ? { ...item, Artifact: artifact } : undefined;
}

export function decodeAudioWaveformPage(value: unknown, start: number, limit: number, libraryId = ''): AudioWaveformPage | undefined {
  if (!record(value) || !count(value.TotalRecordCount) || value.StartIndex !== start || value.Limit !== limit || !Array.isArray(value.Items)
    || value.Items.length > limit || value.TotalRecordCount < value.Items.length) return undefined;
  const items = value.Items.map(decodeAudioWaveformItem);
  if (items.some((item) => !item || libraryId && item.LibraryId !== libraryId) || new Set(items.map((item) => item?.ItemId)).size !== items.length) return undefined;
  return { Items: items as AudioWaveformItem[], TotalRecordCount: value.TotalRecordCount, StartIndex: start, Limit: limit };
}

export function validAudioWaveformRunInput(value: unknown): value is AudioWaveformRunInput {
  return record(value) && value.Kind === 'waveform' && typeof value.Force === 'boolean' && typeof value.RequestId === 'string'
    && /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value.RequestId)
    && Array.isArray(value.LibraryIds) && value.LibraryIds.length <= 64 && value.LibraryIds.every(nonempty)
    && Array.isArray(value.ItemIds) && value.ItemIds.length <= 256 && value.ItemIds.every(nonempty);
}

export function decodeAudioWaveformReceipt(value: unknown): AudioWaveformRunReceipt | undefined {
  return record(value) && nonempty(value.RunId) && nonempty(value.TaskId) && typeof value.Admitted === 'boolean' && count(value.Queued)
    ? value as unknown as AudioWaveformRunReceipt : undefined;
}
