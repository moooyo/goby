export type CreditsDetectionSource = 'Chapter' | 'BlackFrame' | 'Chromaprint' | 'Combined';
export interface CreditsPoint { StartTicks: number; Provenance: 'Chapter' | 'Manual' | 'Import' | 'Detected' }
export interface CreditsInterval { StartTicks: number; EndTicks: number; Source: CreditsDetectionSource }
export interface ItemCredits {
  ItemId: string; MediaSourceId: string; SourceRevision: string; Revision: string; DurationTicks: number;
  Automatic: CreditsPoint | null; Effective: CreditsPoint | null; Override: CreditsPoint | null; OverrideStale: boolean;
  LastEditedBy: string; LastEditedAt: string | null;
  Detected: CreditsInterval[]; DetectedStale: boolean; DetectedRevision: string; DetectedStatus: string; DetectedReason: string; DetectedUpdatedAt: string | null;
}
export interface CreditsEditorActivity { dirty: boolean; pending: boolean; busy: boolean }
export const creditsSourceLabels: Record<CreditsDetectionSource, string> = { Chapter: '章节标记', BlackFrame: '黑场分析', Chromaprint: '重复音频', Combined: '综合分析' };
const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const count = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
const revision = (value: unknown): value is string => typeof value === 'string' && /^(0|[1-9]\d*)$/.test(value) && value.length <= 19 && (value.length < 19 || value <= '9223372036854775807');
const timestamp = (value: unknown) => value === null || typeof value === 'string' && Number.isFinite(Date.parse(value));
function point(value: unknown, duration?: number): value is CreditsPoint {
  return record(value) && count(value.StartTicks) && (duration === undefined || value.StartTicks < duration)
    && typeof value.Provenance === 'string' && ['Chapter', 'Manual', 'Import', 'Detected'].includes(value.Provenance);
}

export function decodeItemCredits(input: unknown, itemId: string): ItemCredits | undefined {
  if (!record(input) || input.ItemId !== itemId || typeof input.MediaSourceId !== 'string' || !input.MediaSourceId
    || typeof input.SourceRevision !== 'string' || !input.SourceRevision || !revision(input.Revision)
    || !count(input.DurationTicks) || input.DurationTicks <= 0 || typeof input.OverrideStale !== 'boolean'
    || typeof input.LastEditedBy !== 'string' || !timestamp(input.LastEditedAt)
    || input.Automatic !== null && (!point(input.Automatic, input.DurationTicks) || !['Chapter', 'Detected'].includes(input.Automatic.Provenance))
    || input.Effective !== null && !point(input.Effective, input.DurationTicks)
    || input.Override !== null && (!point(input.Override, input.OverrideStale ? undefined : input.DurationTicks) || !['Manual', 'Import'].includes(input.Override.Provenance))
    || input.Override === null && input.OverrideStale) return undefined;
  const detectionFields = ['Detected', 'DetectedStale', 'DetectedRevision', 'DetectedStatus', 'DetectedReason', 'DetectedUpdatedAt'];
  const legacy = detectionFields.every((key) => !(key in input));
  const value = legacy ? { ...input, Detected: [], DetectedStale: false, DetectedRevision: '0', DetectedStatus: 'not_analyzed', DetectedReason: '', DetectedUpdatedAt: null } : input;
  if (!Array.isArray(value.Detected) || value.Detected.length > 256 || typeof value.DetectedStale !== 'boolean'
    || !revision(value.DetectedRevision) || typeof value.DetectedStatus !== 'string' || !value.DetectedStatus
    || typeof value.DetectedReason !== 'string' || !timestamp(value.DetectedUpdatedAt)) return undefined;
  const segments = value.Detected;
  let previous = -1;
  for (const segment of segments) {
    if (!record(segment) || !count(segment.StartTicks) || !count(segment.EndTicks) || segment.EndTicks <= segment.StartTicks
      || !value.DetectedStale && segment.EndTicks > input.DurationTicks || segment.StartTicks < previous
      || typeof segment.Source !== 'string' || !['Chapter', 'BlackFrame', 'Chromaprint', 'Combined'].includes(segment.Source)) return undefined;
    previous = segment.StartTicks;
  }
  if ([input.Automatic, input.Effective].some((entry) => point(entry) && entry.Provenance === 'Detected'
    && (value.DetectedStale || value.DetectedStatus !== 'qualified' || segments.length === 0 || entry.StartTicks !== segments[0].StartTicks))) return undefined;
  return value as unknown as ItemCredits;
}

export function creditsDetectionLabel(value: ItemCredits): string {
  if (value.DetectedStale) return '检测结果已过期';
  switch (value.DetectedStatus) {
    case 'not_analyzed': return '尚未识别';
    case 'qualified': return '已识别到片尾区间';
    case 'no_result': return '未识别到片尾区间';
    case 'pending': case 'queued': return '等待识别';
    case 'running': return '正在识别';
    case 'failed': case 'error': return '识别失败';
    default: return `检测状态：${value.DetectedStatus}`;
  }
}
