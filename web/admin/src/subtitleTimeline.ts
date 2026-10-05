export const subtitleTimelineTaskKey = 'media.subtitle_timeline_generation';
export const subtitleTimelineStates = ['missing', 'pending', 'running', 'ready', 'failed', 'cancelled'] as const;
export type SubtitleTimelineState = typeof subtitleTimelineStates[number];
export const subtitleTimelineStateLabels: Record<SubtitleTimelineState, string> = { missing: '尚未生成', pending: '等待生成', running: '正在生成', ready: '任务完成', failed: '生成失败', cancelled: '已取消' };

export function subtitleTimelineFailure(code: string): string {
  const labels: Record<string, string> = {
    unsupported_source: '当前输入包含不支持的位图字幕格式或显示指令。支持内封 PGS/DVD、外挂 SUP 及多语言 IDX/SUB 配对。',
    decode_failed: '字幕轨解析失败，未保存新的时间轴。请检查片源和任务详情后重试。',
    source_changed: '生成期间片源发生变化，本次结果未发布。请重新扫描片源后再生成。',
    artifact_stale: '已有成品对应的片源已变化。请明确重新生成以更新字幕时间轴。',
  };
  return labels[code] ?? '任务未能完成，请在任务中心查看详情。';
}

export interface SubtitleTimelineTrack {
  StreamIndex: number;
  Codec: string;
  IntervalCount: number;
  Warnings: string[];
}
export interface SubtitleTimelineArtifact { Available: boolean; Stale: boolean; Profile: string; Generation: string; DurationTicks: number; Size: number; Tracks: SubtitleTimelineTrack[] }
export interface SubtitleTimelineItem {
  ItemId: string; LibraryId: string; Name: string; SourceRevision: string; DurationTicks: number; SubtitleStreamCount: number;
  State: SubtitleTimelineState; RequestedRevision: string; CompletedRevision: string; RunId: string; Reused: boolean; ErrorCode: string;
  RequestedAt: string | null; StartedAt: string | null; FinishedAt: string | null;
}
export interface SubtitleTimelineDetail extends SubtitleTimelineItem { Artifact: SubtitleTimelineArtifact }
export interface SubtitleTimelinePage { Items: SubtitleTimelineItem[]; TotalRecordCount: number; StartIndex: number; Limit: number }
export interface SubtitleTimelineRunInput { Kind: 'subtitle-timeline'; RequestId: string; LibraryIds: string[]; ItemIds: string[]; Force: boolean }
export interface SubtitleTimelineRunReceipt { RunId: string; TaskId: string; Admitted: boolean; Queued: number }
export interface SubtitleTimelineActivity { pending: boolean; busy: boolean }

const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const count = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
const nonempty = (value: unknown): value is string => typeof value === 'string' && value.length > 0;
const revision = (value: unknown): value is string => typeof value === 'string' && /^(0|[1-9]\d*)$/.test(value) && value.length <= 19 && (value.length < 19 || value <= '9223372036854775807');
const timestamp = (value: unknown) => value === null || typeof value === 'string' && Number.isFinite(Date.parse(value));
export const activeSubtitleTimeline = (value?: SubtitleTimelineItem) => value?.State === 'pending' || value?.State === 'running';

export function decodeSubtitleTimelineItem(value: unknown): SubtitleTimelineItem | undefined {
  if (!record(value) || !nonempty(value.ItemId) || !nonempty(value.LibraryId) || typeof value.Name !== 'string' || !nonempty(value.SourceRevision)
    || !count(value.DurationTicks) || !count(value.SubtitleStreamCount) || typeof value.State !== 'string' || !subtitleTimelineStates.includes(value.State as SubtitleTimelineState)
    || !revision(value.RequestedRevision) || !revision(value.CompletedRevision) || typeof value.RunId !== 'string' || typeof value.Reused !== 'boolean' || typeof value.ErrorCode !== 'string'
    || !timestamp(value.RequestedAt) || !timestamp(value.StartedAt) || !timestamp(value.FinishedAt)) return undefined;
  return value as unknown as SubtitleTimelineItem;
}

export function decodeSubtitleTimelineArtifact(value: unknown): SubtitleTimelineArtifact | undefined {
  if (!record(value) || typeof value.Available !== 'boolean' || 'Stale' in value && typeof value.Stale !== 'boolean'
    || 'Profile' in value && typeof value.Profile !== 'string' || 'Generation' in value && typeof value.Generation !== 'string'
    || 'Size' in value && !count(value.Size)
    || 'DurationTicks' in value && !count(value.DurationTicks) || 'Tracks' in value && !Array.isArray(value.Tracks)) return undefined;
  const duration = value.DurationTicks as number | undefined ?? 0;
  const tracks = value.Tracks as unknown[] | undefined ?? [];
  if (value.Available && (duration <= 0 || tracks.length === 0) || tracks.length > 64) return undefined;
  const output: SubtitleTimelineTrack[] = [];
  const indices = new Set<number>();
  for (const track of tracks) {
    if (!record(track) || !count(track.StreamIndex) || indices.has(track.StreamIndex) || !nonempty(track.Codec)
      || !count(track.IntervalCount)) return undefined;
    const warnings = track.Warnings === null ? [] : track.Warnings;
    if (!Array.isArray(warnings) || !warnings.every(nonempty)) return undefined;
    indices.add(track.StreamIndex);
    output.push({ StreamIndex: track.StreamIndex, Codec: track.Codec, IntervalCount: track.IntervalCount, Warnings: [...warnings] });
  }
  return { Available: value.Available, Stale: value.Stale as boolean | undefined ?? false, Profile: value.Profile as string | undefined ?? '',
    Generation: value.Generation as string | undefined ?? '', DurationTicks: duration, Size: value.Size as number | undefined ?? 0, Tracks: output };
}

export function decodeSubtitleTimelineDetail(value: unknown, itemId: string): SubtitleTimelineDetail | undefined {
  const item = decodeSubtitleTimelineItem(value);
  if (!item || item.ItemId !== itemId || !record(value)) return undefined;
  const artifact = decodeSubtitleTimelineArtifact(value.Artifact);
  return artifact ? { ...item, Artifact: artifact } : undefined;
}

export function decodeSubtitleTimelinePage(value: unknown, start: number, limit: number, libraryId = ''): SubtitleTimelinePage | undefined {
  if (!record(value) || !count(value.TotalRecordCount) || value.StartIndex !== start || value.Limit !== limit || !Array.isArray(value.Items)
    || value.Items.length > limit || value.TotalRecordCount < value.Items.length) return undefined;
  const items = value.Items.map(decodeSubtitleTimelineItem);
  if (items.some((item) => !item || libraryId && item.LibraryId !== libraryId) || new Set(items.map((item) => item?.ItemId)).size !== items.length) return undefined;
  return { Items: items as SubtitleTimelineItem[], TotalRecordCount: value.TotalRecordCount, StartIndex: start, Limit: limit };
}

export function validSubtitleTimelineRunInput(value: unknown): value is SubtitleTimelineRunInput {
  return record(value) && value.Kind === 'subtitle-timeline' && typeof value.Force === 'boolean' && typeof value.RequestId === 'string'
    && /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value.RequestId)
    && Array.isArray(value.LibraryIds) && value.LibraryIds.length <= 64 && value.LibraryIds.every(nonempty)
    && Array.isArray(value.ItemIds) && value.ItemIds.length <= 256 && value.ItemIds.every(nonempty);
}

export function decodeSubtitleTimelineReceipt(value: unknown): SubtitleTimelineRunReceipt | undefined {
  return record(value) && nonempty(value.RunId) && nonempty(value.TaskId) && typeof value.Admitted === 'boolean' && count(value.Queued)
    ? value as unknown as SubtitleTimelineRunReceipt : undefined;
}
