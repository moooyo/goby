export interface IntroSkipperOptions {
  AnalysisPercent: number;
  AnalysisLengthLimit: number;
  MinimumIntroDuration: number;
  MaximumIntroDuration: number;
  MaximumFingerprintPointDifferences: number;
  MaximumTimeSkip: number;
  InvertedIndexShift: number;
}
export interface AnalysisProfile {
  AutoPublishIntros: boolean;
  PreviewIntervalSeconds: number;
  PreviewQuality: number;
  MaxSourceBytes: number;
  MaxItemRuntimeSeconds: number;
  FeatureCacheMaxBytes: number;
  IntroSkipper: IntroSkipperOptions;
}
export interface AnalysisConfiguration { Revision: string; Profile: AnalysisProfile; Defaults: AnalysisProfile; UpdatedAt: string }
export interface AnalysisCache {
  ReadyEntries: number; BuildingEntries: number; PendingPublications: number; Readers: number;
  ReadyBytes: number; ReservedBytes: number; ControlBytes: number; TotalBytes: number; MaxBytes: number;
}
export interface AnalysisOverview {
  Configuration: AnalysisConfiguration;
  Runtime: { Configured: boolean; IntroAvailable: boolean; PreviewAvailable: boolean; CreditsAvailable?: boolean; CreditsReasons?: string[]; SubtitleTimelineAvailable?: boolean; SubtitleTimelineReasons?: string[]; Reasons: string[]; Cache: AnalysisCache | null };
}
export interface AnalysisInterval { StartTicks: number; EndTicks: number }
export interface AnalysisSupport { EpisodeKey: string; SourceKey: string; ContentIdentity: string; Interval: AnalysisInterval }
export interface AnalysisMetrics {
  AudioAgreementPermille: number; AudioInformativePermille: number; AudioSimilarityPermille: number; AudioSamples: number; AudioDistinct: number;
  VisualAgreementPermille: number; VisualSimilarityPermille: number; VisualCoveragePermille: number; VisualSamples: number; VisualTransitions: number;
  VisualChangeCoveragePermille: number; VisualDominancePermille: number; BoundaryUncertaintyTicks: number; PairCount: number;
  VisualAnchorCount: number; VisualMinBandMatchedPermille: number; VisualMatchedTimePermille: number;
  VisualContradictedTimePermille: number; VisualUnobservableTimePermille: number; VisualMaxUnconfirmedGapTicks: number;
  VisualStartAnchorGapTicks: number; VisualEndAnchorGapTicks: number; VisualDistinctStates: number; VisualDominantStatePermille: number;
}
export interface AnalysisCandidate { Interval: AnalysisInterval; GroupID: string; Status: string; Reasons: string[]; Metrics: AnalysisMetrics; Support: AnalysisSupport[] }
export interface IntroSkipperCandidate { Interval: AnalysisInterval; UpstreamCommit: string; Support: (AnalysisSupport & { AlgorithmProfile: string })[] }
export interface AnalysisDetection {
  ItemId: string; Revision: string; ManualRevision: string; SourceRevision: string; Status: string; Reasons: string[];
  Candidate: AnalysisCandidate | null; IntroSkipperCandidate: IntroSkipperCandidate | null;
  Effective: (AnalysisInterval & { Provenance: string }) | null; Suppressed: boolean; UpdatedAt: string;
}
export interface AnalysisItem {
  Id: string; Name: string; Type: string; LibraryId: string; MediaSourceId: string; SourceRevision: string;
  Detection: AnalysisDetection;
  Previews: { Width: number; Height: number; Size: number; FrameCount: number; Status: string; FailureCode: string; UpdatedAt: string }[];
}
export interface AnalysisItems { Items: AnalysisItem[]; TotalRecordCount: number; StartIndex: number; Limit: number }
export interface AnalysisRunInput { Kind: 'intro' | 'previews' | 'credits'; RequestId: string; LibraryIds: string[]; ItemIds: string[]; Force: boolean }
export interface AnalysisRunReceipt { RunId: string; TaskId: string; Admitted: boolean }
export interface AnalysisPrune { RemovedEntries: number; RemovedBytes: number; RemainingBytes: number; BusyEntries: number }
export type AnalysisAction = 'accept' | 'reject' | 'reset';
export type AnalysisNumberField = Exclude<keyof AnalysisProfile, 'AutoPublishIntros' | 'IntroSkipper'>;
export type IntroSkipperField = keyof IntroSkipperOptions;
export type AnalysisField = AnalysisNumberField | `IntroSkipper.${IntroSkipperField}`;
export type AnalysisDraft = Record<AnalysisNumberField, string> & { IntroSkipper: Record<IntroSkipperField, string> };
export type AnalysisErrors = Partial<Record<AnalysisField, string>>;
interface NumberField<Key extends string> { key: Key; label: string; min: number; max: number; help: string; decimal?: boolean; advanced?: boolean }

export const analysisNumberFields: NumberField<AnalysisNumberField>[] = [
  { key: 'PreviewIntervalSeconds', label: 'Preview interval (seconds)', min: 2, max: 120, help: 'Minimum requested spacing: 2–120 seconds. Long media uses a larger interval to stay within 4096 frames per preview file.' },
  { key: 'PreviewQuality', label: 'Preview image quality', min: 40, max: 95, help: 'Image quality from 40 to 95. Applies to new preview jobs.' },
  { key: 'MaxSourceBytes', label: 'Maximum source size (bytes)', min: 1, max: 2 ** 40, help: 'Largest media source admitted for analysis: 1–1,099,511,627,776 bytes.' },
  { key: 'MaxItemRuntimeSeconds', label: 'Per-item processing timeout (seconds)', min: 1, max: 7200, help: 'Processing budget per item: 1–7200 seconds.' },
  { key: 'FeatureCacheMaxBytes', label: 'Feature cache budget (bytes)', min: 2 ** 20, max: 512 * 2 ** 20, help: 'Feature cache budget: 1,048,576–536,870,912 bytes.' },
];
export const introSkipperFields: NumberField<IntroSkipperField>[] = [
  { key: 'AnalysisPercent', label: 'Episode audio to analyze (%)', min: 1, max: 50, help: 'Analyze the first 1–50% of each episode, up to the time limit. Episodes shorter than 5 minutes use their full length, up to that limit.' },
  { key: 'AnalysisLengthLimit', label: 'Maximum audio to analyze (minutes)', min: 1, max: 10, help: 'Cap analyzed audio at 1–10 minutes per episode. Default: 10 minutes.' },
  { key: 'MinimumIntroDuration', label: 'Minimum intro duration (seconds)', min: 1, max: 600, help: 'Shortest matching audio run considered an intro: 1–600 seconds. Default: 15 seconds.' },
  { key: 'MaximumIntroDuration', label: 'Maximum intro duration (seconds)', min: 1, max: 600, help: 'Upstream duration limit used when accepting a match: 1–600 seconds, at least the minimum. Default: 120 seconds.' },
  { key: 'MaximumFingerprintPointDifferences', label: 'Maximum differing fingerprint bits', min: 0, max: 32, advanced: true, help: 'Allowed differing bits in a 32-bit audio fingerprint: 0–32. Default: 6.' },
  { key: 'MaximumTimeSkip', label: 'Maximum match gap (seconds)', min: 0, max: 30, decimal: true, advanced: true, help: 'Time allowed between matching fingerprint points in one run: 0–30 seconds. Default: 3.5 seconds.' },
  { key: 'InvertedIndexShift', label: 'Fingerprint index search radius', min: 0, max: 32, advanced: true, help: 'Search this many neighboring fingerprint index keys: 0–32. Default: 2.' },
];

const record = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value);
const text = (value: unknown): value is string => typeof value === 'string';
const nonempty = (value: unknown): value is string => text(value) && value.length > 0;
const count = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
const strings = (value: unknown): value is string[] => Array.isArray(value) && value.every(text);
const timestamp = (value: unknown): value is string => text(value) && Number.isFinite(Date.parse(value));
export const analysisRevision = (value: unknown, allowZero = false): value is string => text(value) && (allowZero && value === '0' || /^[1-9]\d*$/.test(value) && value.length <= 19 && (value.length < 19 || value <= '9223372036854775807'));
export function validIntroSkipperOptions(value: unknown): value is IntroSkipperOptions {
  if (!record(value)) return false;
  return introSkipperFields.every(({ key, min, max, decimal }) => typeof value[key] === 'number' && Number.isFinite(value[key])
    && (decimal || Number.isSafeInteger(value[key])) && value[key] >= min && value[key] <= max)
    && (value.MinimumIntroDuration as number) <= (value.MaximumIntroDuration as number);
}
export function validAnalysisProfile(value: unknown): value is AnalysisProfile {
  return record(value) && typeof value.AutoPublishIntros === 'boolean' && validIntroSkipperOptions(value.IntroSkipper)
    && analysisNumberFields.every(({ key, min, max }) => count(value[key]) && value[key] >= min && value[key] <= max);
}
export function validAnalysisConfiguration(value: unknown): value is AnalysisConfiguration {
  return record(value) && analysisRevision(value.Revision) && validAnalysisProfile(value.Profile) && validAnalysisProfile(value.Defaults) && timestamp(value.UpdatedAt);
}
export function validAnalysisOverview(value: unknown): value is AnalysisOverview {
  if (!record(value) || !validAnalysisConfiguration(value.Configuration) || !record(value.Runtime)) return false;
  const runtime = value.Runtime;
  return ['Configured', 'IntroAvailable', 'PreviewAvailable'].every((key) => typeof runtime[key] === 'boolean') && strings(runtime.Reasons)
    && (!('CreditsAvailable' in runtime) || typeof runtime.CreditsAvailable === 'boolean')
    && (!('CreditsReasons' in runtime) || strings(runtime.CreditsReasons))
    && (!('SubtitleTimelineAvailable' in runtime) || typeof runtime.SubtitleTimelineAvailable === 'boolean')
    && (!('SubtitleTimelineReasons' in runtime) || strings(runtime.SubtitleTimelineReasons))
    && (runtime.Cache === null || record(runtime.Cache) && ['ReadyEntries', 'BuildingEntries', 'PendingPublications', 'Readers', 'ReadyBytes', 'ReservedBytes', 'ControlBytes', 'TotalBytes', 'MaxBytes'].every((key) => count((runtime.Cache as Record<string, unknown>)[key])));
}
function interval(value: unknown): value is AnalysisInterval { return record(value) && count(value.StartTicks) && count(value.EndTicks) && value.EndTicks > value.StartTicks; }
function metrics(value: unknown): value is AnalysisMetrics {
  return record(value)
    && ['AudioAgreementPermille', 'AudioInformativePermille', 'AudioSimilarityPermille', 'VisualAgreementPermille', 'VisualSimilarityPermille', 'VisualCoveragePermille', 'VisualChangeCoveragePermille', 'VisualDominancePermille', 'VisualMinBandMatchedPermille', 'VisualMatchedTimePermille', 'VisualContradictedTimePermille', 'VisualUnobservableTimePermille', 'VisualDominantStatePermille'].every((key) => count(value[key]) && value[key] <= 1000)
    && ['AudioSamples', 'AudioDistinct', 'VisualSamples', 'VisualTransitions', 'BoundaryUncertaintyTicks', 'PairCount'].every((key) => count(value[key]))
    && ['VisualAnchorCount', 'VisualDistinctStates'].every((key) => count(value[key]) && value[key] <= 4096)
    && ['VisualMaxUnconfirmedGapTicks', 'VisualStartAnchorGapTicks', 'VisualEndAnchorGapTicks'].every((key) => count(value[key]) && value[key] <= 6_000_000_000);
}
export function validAnalysisDetection(value: unknown): value is AnalysisDetection {
  if (!record(value) || !nonempty(value.ItemId) || !analysisRevision(value.Revision, true) || !analysisRevision(value.ManualRevision, true) || !nonempty(value.SourceRevision)
    || !nonempty(value.Status) || !strings(value.Reasons) || typeof value.Suppressed !== 'boolean' || !timestamp(value.UpdatedAt)
    || value.Effective !== null && (!interval(value.Effective) || !record(value.Effective) || !nonempty(value.Effective.Provenance))) return false;
  if (value.IntroSkipperCandidate !== null) {
    const candidate = value.IntroSkipperCandidate;
    if (value.Candidate !== null || !record(candidate) || !interval(candidate.Interval) || !text(candidate.UpstreamCommit) || !/^[0-9a-f]{40}$/.test(candidate.UpstreamCommit)
      || !Array.isArray(candidate.Support) || candidate.Support.length !== 2
      || !candidate.Support.every((support) => record(support) && nonempty(support.EpisodeKey) && nonempty(support.SourceKey) && nonempty(support.ContentIdentity) && nonempty(support.AlgorithmProfile) && interval(support.Interval))
      || new Set(candidate.Support.map((support) => support.EpisodeKey)).size !== 2
      || new Set(candidate.Support.map((support) => support.SourceKey)).size !== 2) return false;
  }
  if (value.Candidate === null) return true;
  const candidate = value.Candidate;
  return record(candidate) && interval(candidate.Interval) && nonempty(candidate.GroupID) && nonempty(candidate.Status) && strings(candidate.Reasons)
    && metrics(candidate.Metrics)
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
  return { PreviewIntervalSeconds: String(profile.PreviewIntervalSeconds), PreviewQuality: String(profile.PreviewQuality),
    MaxSourceBytes: String(profile.MaxSourceBytes), MaxItemRuntimeSeconds: String(profile.MaxItemRuntimeSeconds), FeatureCacheMaxBytes: String(profile.FeatureCacheMaxBytes),
    IntroSkipper: Object.fromEntries(introSkipperFields.map(({ key }) => [key, String(profile.IntroSkipper[key])])) as AnalysisDraft['IntroSkipper'] };
}
function parseNumberField<Key extends string>(raw: string, field: NumberField<Key>): { value?: number; error?: string } {
  const input = raw.trim(); const value = Number(input);
  if (!(field.decimal ? /^\d+(?:\.\d+)?$/ : /^\d+$/).test(input) || !Number.isFinite(value) || !field.decimal && !Number.isSafeInteger(value) || value < field.min || value > field.max) {
    return { error: `Use ${field.decimal ? 'a number' : 'a whole number'} from ${field.min.toLocaleString('en-US')} to ${field.max.toLocaleString('en-US')}.` };
  }
  return { value };
}
export function parseAnalysisDraft(draft: AnalysisDraft): { profile?: AnalysisProfile; errors: AnalysisErrors } {
  const errors: AnalysisErrors = {};
  const values: Partial<Record<AnalysisNumberField, number>> = {};
  for (const field of analysisNumberFields) {
    const parsed = parseNumberField(draft[field.key], field);
    if (parsed.error) errors[field.key] = parsed.error; else values[field.key] = parsed.value;
  }
  const intro: Partial<IntroSkipperOptions> = {};
  for (const field of introSkipperFields) {
    const parsed = parseNumberField(draft.IntroSkipper[field.key], field);
    if (parsed.error) errors[`IntroSkipper.${field.key}`] = parsed.error; else intro[field.key] = parsed.value;
  }
  if (intro.MinimumIntroDuration !== undefined && intro.MaximumIntroDuration !== undefined && intro.MinimumIntroDuration > intro.MaximumIntroDuration) {
    errors['IntroSkipper.MinimumIntroDuration'] = 'The minimum must not exceed the maximum intro duration.';
    errors['IntroSkipper.MaximumIntroDuration'] = 'The maximum must be at least the minimum intro duration.';
  }
  return Object.keys(errors).length > 0 ? { errors } : { errors, profile: { AutoPublishIntros: true, ...values, IntroSkipper: intro } as AnalysisProfile };
}
export function analysisTime(ticks: number): string { const seconds = ticks / 10_000_000; return `${Math.floor(seconds / 60)}:${(seconds % 60).toFixed(2).padStart(5, '0')}`; }
export function analysisBytes(bytes: number): string { if (bytes < 1024) return `${bytes} B`; const power = Math.min(4, Math.floor(Math.log(bytes) / Math.log(1024))); return `${(bytes / 1024 ** power).toFixed(1)} ${['B', 'KiB', 'MiB', 'GiB', 'TiB'][power]}`; }
export function analysisIntroStatus(item: AnalysisItem): string {
  if (item.Detection.Effective) return 'Intro available';
  if (item.Type !== 'Episode') return 'Not applicable';
  switch (item.Detection.Status) {
    case 'review': case 'no_result': case 'qualified': return 'No intro detected';
    case 'not_analyzed': return 'Not analyzed';
    case 'stale': return 'Needs analysis';
    case 'failed': case 'error': return 'Analysis failed';
    case 'unavailable': return 'Analysis unavailable';
    default: return item.Detection.Status.replaceAll('_', ' ');
  }
}
