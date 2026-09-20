export interface AnalysisProfile {
  AutoPublishIntros: boolean;
  PreviewIntervalSeconds: number;
  PreviewQuality: number;
  MaxSourceBytes: number;
  MaxItemRuntimeSeconds: number;
  FeatureCacheMaxBytes: number;
}
export interface AnalysisConfiguration { Revision: string; Profile: AnalysisProfile; Defaults: AnalysisProfile; UpdatedAt: string }
export interface AnalysisCache {
  ReadyEntries: number; BuildingEntries: number; PendingPublications: number; Readers: number;
  ReadyBytes: number; ReservedBytes: number; ControlBytes: number; TotalBytes: number; MaxBytes: number;
}
export interface AnalysisOverview {
  Configuration: AnalysisConfiguration;
  Runtime: { Configured: boolean; IntroAvailable: boolean; PreviewAvailable: boolean; Reasons: string[]; Cache: AnalysisCache | null };
}
export interface AnalysisInterval { StartTicks: number; EndTicks: number }
export interface AnalysisSupport { EpisodeKey: string; SourceKey: string; ContentIdentity: string; Interval: AnalysisInterval }
export interface AnalysisMetrics {
  AudioAgreementPermille: number; AudioInformativePermille: number; AudioSimilarityPermille: number; AudioSamples: number; AudioDistinct: number;
  VisualAgreementPermille: number; VisualSimilarityPermille: number; VisualCoveragePermille: number; VisualSamples: number; VisualTransitions: number;
  VisualChangeCoveragePermille: number; VisualDominancePermille: number; BoundaryUncertaintyTicks: number; PairCount: number;
}
export interface AnalysisCandidate { Interval: AnalysisInterval; GroupID: string; Status: string; Reasons: string[]; Metrics: AnalysisMetrics; Support: AnalysisSupport[] }
export interface AnalysisDetection {
  ItemId: string; Revision: string; ManualRevision: string; SourceRevision: string; Status: string; Reasons: string[];
  Candidate: AnalysisCandidate | null; Effective: (AnalysisInterval & { Provenance: string }) | null; Suppressed: boolean; UpdatedAt: string;
}
export interface AnalysisItem {
  Id: string; Name: string; Type: string; LibraryId: string; MediaSourceId: string; SourceRevision: string;
  Detection: AnalysisDetection;
  Previews: { Width: number; Height: number; Size: number; FrameCount: number; Status: string; FailureCode: string; UpdatedAt: string }[];
}
export interface AnalysisItems { Items: AnalysisItem[]; TotalRecordCount: number; StartIndex: number; Limit: number }
export interface AnalysisRunInput { Kind: 'intro' | 'previews'; RequestId: string; LibraryIds: string[]; ItemIds: string[]; Force: boolean }
export interface AnalysisRunReceipt { RunId: string; TaskId: string; Admitted: boolean }
export interface AnalysisPrune { RemovedEntries: number; RemovedBytes: number; RemainingBytes: number; BusyEntries: number }
export type AnalysisAction = 'accept' | 'reject' | 'reset';
export type AnalysisNumberField = Exclude<keyof AnalysisProfile, 'AutoPublishIntros'>;
export type AnalysisDraft = { AutoPublishIntros: boolean } & Record<AnalysisNumberField, string>;

export const analysisNumberFields: { key: AnalysisNumberField; label: string; min: number; max: number; help: string }[] = [
  { key: 'PreviewIntervalSeconds', label: 'Preview interval (seconds)', min: 2, max: 120, help: 'Minimum requested spacing: 2–120 seconds. Long media uses a larger interval to stay within 4096 frames per preview file.' },
  { key: 'PreviewQuality', label: 'Preview image quality', min: 40, max: 95, help: 'Image quality from 40 to 95. Applies to new preview jobs.' },
  { key: 'MaxSourceBytes', label: 'Maximum source size (bytes)', min: 1, max: 2 ** 40, help: 'Largest media source admitted for analysis: 1–1,099,511,627,776 bytes.' },
  { key: 'MaxItemRuntimeSeconds', label: 'Per-item processing timeout (seconds)', min: 1, max: 7200, help: 'Processing budget per item: 1–7200 seconds.' },
  { key: 'FeatureCacheMaxBytes', label: 'Feature cache budget (bytes)', min: 2 ** 20, max: 512 * 2 ** 20, help: 'Feature cache budget: 1,048,576–536,870,912 bytes.' },
];

const record = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value);
const text = (value: unknown): value is string => typeof value === 'string';
const nonempty = (value: unknown): value is string => text(value) && value.length > 0;
const count = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
const strings = (value: unknown): value is string[] => Array.isArray(value) && value.every(text);
const timestamp = (value: unknown): value is string => text(value) && Number.isFinite(Date.parse(value));
export const analysisRevision = (value: unknown, allowZero = false): value is string => text(value) && (allowZero && value === '0' || /^[1-9]\d*$/.test(value) && value.length <= 19 && (value.length < 19 || value <= '9223372036854775807'));
export function validAnalysisProfile(value: unknown): value is AnalysisProfile {
  return record(value) && typeof value.AutoPublishIntros === 'boolean' && analysisNumberFields.every(({ key, min, max }) => count(value[key]) && value[key] >= min && value[key] <= max);
}
export function validAnalysisConfiguration(value: unknown): value is AnalysisConfiguration {
  return record(value) && analysisRevision(value.Revision) && validAnalysisProfile(value.Profile) && validAnalysisProfile(value.Defaults) && timestamp(value.UpdatedAt);
}
export function validAnalysisOverview(value: unknown): value is AnalysisOverview {
  if (!record(value) || !validAnalysisConfiguration(value.Configuration) || !record(value.Runtime)) return false;
  const runtime = value.Runtime;
  return ['Configured', 'IntroAvailable', 'PreviewAvailable'].every((key) => typeof runtime[key] === 'boolean') && strings(runtime.Reasons)
    && (runtime.Cache === null || record(runtime.Cache) && ['ReadyEntries', 'BuildingEntries', 'PendingPublications', 'Readers', 'ReadyBytes', 'ReservedBytes', 'ControlBytes', 'TotalBytes', 'MaxBytes'].every((key) => count((runtime.Cache as Record<string, unknown>)[key])));
}
function interval(value: unknown): value is AnalysisInterval { return record(value) && count(value.StartTicks) && count(value.EndTicks) && value.EndTicks > value.StartTicks; }
export function validAnalysisDetection(value: unknown): value is AnalysisDetection {
  if (!record(value) || !nonempty(value.ItemId) || !analysisRevision(value.Revision, true) || !analysisRevision(value.ManualRevision, true) || !nonempty(value.SourceRevision)
    || !nonempty(value.Status) || !strings(value.Reasons) || typeof value.Suppressed !== 'boolean' || !timestamp(value.UpdatedAt)
    || value.Effective !== null && (!interval(value.Effective) || !record(value.Effective) || !nonempty(value.Effective.Provenance))) return false;
  if (value.Candidate === null) return true;
  const candidate = value.Candidate;
  return record(candidate) && interval(candidate.Interval) && nonempty(candidate.GroupID) && nonempty(candidate.Status) && strings(candidate.Reasons)
    && record(candidate.Metrics) && ['AudioAgreementPermille', 'AudioInformativePermille', 'AudioSimilarityPermille', 'AudioSamples', 'AudioDistinct', 'VisualAgreementPermille', 'VisualSimilarityPermille', 'VisualCoveragePermille', 'VisualSamples', 'VisualTransitions', 'VisualChangeCoveragePermille', 'VisualDominancePermille', 'BoundaryUncertaintyTicks', 'PairCount'].every((key) => count((candidate.Metrics as Record<string, unknown>)[key]))
    && Array.isArray(candidate.Support) && candidate.Support.every((support) => record(support) && nonempty(support.EpisodeKey) && nonempty(support.SourceKey) && nonempty(support.ContentIdentity) && interval(support.Interval));
}
export function validAnalysisItem(value: unknown): value is AnalysisItem {
  return record(value) && nonempty(value.Id) && text(value.Name) && nonempty(value.Type) && nonempty(value.LibraryId) && nonempty(value.MediaSourceId) && nonempty(value.SourceRevision)
    && validAnalysisDetection(value.Detection) && value.Detection.ItemId === value.Id && value.Detection.SourceRevision === value.SourceRevision
    && Array.isArray(value.Previews) && value.Previews.every((preview) => record(preview) && count(preview.Width) && count(preview.Height) && count(preview.Size) && count(preview.FrameCount)
      && nonempty(preview.Status) && text(preview.FailureCode) && timestamp(preview.UpdatedAt));
}
export function validAnalysisItems(value: unknown, start: number, limit: number): value is AnalysisItems {
  return record(value) && value.StartIndex === start && value.Limit === limit && count(value.TotalRecordCount) && Array.isArray(value.Items) && value.Items.length <= limit
    && value.TotalRecordCount >= value.Items.length && value.Items.every(validAnalysisItem) && new Set(value.Items.map((item) => item.Id)).size === value.Items.length;
}
export function analysisDraft(profile: AnalysisProfile): AnalysisDraft {
  return { AutoPublishIntros: profile.AutoPublishIntros, PreviewIntervalSeconds: String(profile.PreviewIntervalSeconds), PreviewQuality: String(profile.PreviewQuality),
    MaxSourceBytes: String(profile.MaxSourceBytes), MaxItemRuntimeSeconds: String(profile.MaxItemRuntimeSeconds), FeatureCacheMaxBytes: String(profile.FeatureCacheMaxBytes) };
}
export function parseAnalysisDraft(draft: AnalysisDraft): { profile?: AnalysisProfile; errors: Partial<Record<AnalysisNumberField, string>> } {
  const errors: Partial<Record<AnalysisNumberField, string>> = {};
  const values: Partial<Record<AnalysisNumberField, number>> = {};
  for (const field of analysisNumberFields) {
    const raw = draft[field.key].trim(); const value = Number(raw);
    if (!/^\d+$/.test(raw) || !Number.isSafeInteger(value) || value < field.min || value > field.max) errors[field.key] = `Use a whole number from ${field.min.toLocaleString('en-US')} to ${field.max.toLocaleString('en-US')}.`;
    else values[field.key] = value;
  }
  return Object.keys(errors).length > 0 ? { errors } : { errors, profile: { AutoPublishIntros: draft.AutoPublishIntros, ...values } as AnalysisProfile };
}
export function analysisTime(ticks: number): string { const seconds = ticks / 10_000_000; return `${Math.floor(seconds / 60)}:${(seconds % 60).toFixed(2).padStart(5, '0')}`; }
export function analysisBytes(bytes: number): string { if (bytes < 1024) return `${bytes} B`; const power = Math.min(4, Math.floor(Math.log(bytes) / Math.log(1024))); return `${(bytes / 1024 ** power).toFixed(1)} ${['B', 'KiB', 'MiB', 'GiB', 'TiB'][power]}`; }
export function analysisCurrentCandidate(item: AnalysisItem): boolean {
  return item.Type === 'Episode' && item.Detection.Candidate !== null && item.Detection.SourceRevision === item.SourceRevision && ['qualified', 'review'].includes(item.Detection.Status);
}
