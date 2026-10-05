import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';
import { analysisRevision } from './mediaAnalysis';

export interface BackgroundPreviewProfile {
  DurationSeconds: number;
  MaxWidth: number;
  VideoBitrate: number;
  MaxItemRuntimeSeconds: number;
}

export interface BackgroundPreviewConfiguration {
  Revision: string;
  Profile: BackgroundPreviewProfile;
  Defaults: BackgroundPreviewProfile;
  UpdatedAt: string;
}

export interface BackgroundPreviewArtifact {
  Available: boolean;
  StartPositionTicks: number;
  RunTimeTicks: number;
  Width: number;
  Height: number;
  Size: number;
  SourceChanged: boolean;
}

export interface BackgroundPreviewDetail {
  ItemId: string;
  LibraryId: string;
  Name: string;
  Revision: string;
  SourceRevision: string;
  DurationTicks: number;
  StartTicks: number | null;
  State: 'missing' | 'pending' | 'running' | 'ready' | 'failed' | 'cancelled';
  RequestedRevision: string;
  CompletedRevision: string;
  RunId: string;
  Reused: boolean;
  ErrorCode: string;
  RequestedAt: string | null;
  StartedAt: string | null;
  FinishedAt: string | null;
  Artifact: BackgroundPreviewArtifact;
}

export interface BackgroundPreviewRunInput {
  Kind: 'background';
  RequestId: string;
  LibraryIds: string[];
  ItemIds: string[];
  Force: boolean;
}

export interface BackgroundPreviewRunReceipt { RunId: string; TaskId: string; Admitted: boolean; Queued: number }

const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const count = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
const nonempty = (value: unknown): value is string => typeof value === 'string' && value.length > 0;
const timestamp = (value: unknown) => value === null || typeof value === 'string' && Number.isFinite(Date.parse(value));
const invalid = () => new ApiError('背景短片响应不完整，请重新加载后再操作。', { code: 'invalid_response' });

export function validBackgroundPreviewProfile(value: unknown): value is BackgroundPreviewProfile {
  return record(value) && count(value.DurationSeconds) && value.DurationSeconds >= 5 && value.DurationSeconds <= 60
    && count(value.MaxWidth) && [640, 960, 1280, 1920].includes(value.MaxWidth)
    && count(value.VideoBitrate) && value.VideoBitrate >= 250000 && value.VideoBitrate <= 8000000
    && count(value.MaxItemRuntimeSeconds) && value.MaxItemRuntimeSeconds >= 30 && value.MaxItemRuntimeSeconds <= 7200;
}

function configuration(value: BackgroundPreviewConfiguration): BackgroundPreviewConfiguration {
  if (!record(value) || !analysisRevision(value.Revision) || !validBackgroundPreviewProfile(value.Profile)
    || !validBackgroundPreviewProfile(value.Defaults) || !nonempty(value.UpdatedAt) || !timestamp(value.UpdatedAt)) throw invalid();
  return value;
}

function artifact(value: unknown): BackgroundPreviewArtifact {
  if (!record(value) || typeof value.Available !== 'boolean'
    || 'SourceChanged' in value && typeof value.SourceChanged !== 'boolean') throw invalid();
  const numericFields = ['StartPositionTicks', 'RunTimeTicks', 'Width', 'Height', 'Size'] as const;
  if (numericFields.some((key) => key in value && !count(value[key]))) throw invalid();
  if (value.Available && ['RunTimeTicks', 'Width', 'Height', 'Size'].some((key) => !count(value[key]) || value[key] <= 0)) throw invalid();
  // The Go response omits zero values. Normalize only absent fields; malformed
  // explicit values must not be treated as a valid empty or ready artifact.
  const result: BackgroundPreviewArtifact = {
    Available: value.Available,
    StartPositionTicks: value.StartPositionTicks as number | undefined ?? 0,
    RunTimeTicks: value.RunTimeTicks as number | undefined ?? 0,
    Width: value.Width as number | undefined ?? 0,
    Height: value.Height as number | undefined ?? 0,
    Size: value.Size as number | undefined ?? 0,
    SourceChanged: value.SourceChanged as boolean | undefined ?? false,
  };
  if (!Number.isSafeInteger(result.StartPositionTicks + result.RunTimeTicks)) throw invalid();
  return result;
}

function detail(value: BackgroundPreviewDetail, itemId: string): BackgroundPreviewDetail {
  if (!record(value) || value.ItemId !== itemId || !nonempty(value.LibraryId) || typeof value.Name !== 'string'
    || !analysisRevision(value.Revision, true) || !analysisRevision(value.RequestedRevision, true) || !analysisRevision(value.CompletedRevision, true)
    || !nonempty(value.SourceRevision) || !count(value.DurationTicks)
    || value.StartTicks !== null && !count(value.StartTicks)
    || !['missing', 'pending', 'running', 'ready', 'failed', 'cancelled'].includes(value.State)
    || typeof value.RunId !== 'string' || typeof value.Reused !== 'boolean' || typeof value.ErrorCode !== 'string'
    || !timestamp(value.RequestedAt) || !timestamp(value.StartedAt) || !timestamp(value.FinishedAt)) throw invalid();
  return { ...value, Artifact: artifact(value.Artifact) };
}

export const backgroundPreviewApi = {
  async configuration(options: RequestOptions = {}): Promise<BackgroundPreviewConfiguration> {
    return configuration(await authenticatedRequest<BackgroundPreviewConfiguration>('/background-previews/configuration', options));
  },
  async configure(Revision: string, Profile: BackgroundPreviewProfile, options: RequestOptions = {}): Promise<BackgroundPreviewConfiguration> {
    return configuration(await authenticatedRequest<BackgroundPreviewConfiguration>('/background-previews/configuration', { ...options, method: 'PUT', body: { Revision, Profile } }));
  },
  async get(itemId: string, options: RequestOptions = {}): Promise<BackgroundPreviewDetail> {
    return detail(await authenticatedRequest<BackgroundPreviewDetail>(`/items/${encodeURIComponent(itemId)}/background-preview`, options), itemId);
  },
  async update(itemId: string, input: { Revision: string; SourceRevision: string; StartTicks: number | null }, options: RequestOptions = {}): Promise<BackgroundPreviewDetail> {
    return detail(await authenticatedRequest<BackgroundPreviewDetail>(`/items/${encodeURIComponent(itemId)}/background-preview`, { ...options, method: 'PUT', body: input }), itemId);
  },
  async start(input: BackgroundPreviewRunInput, options: RequestOptions = {}): Promise<BackgroundPreviewRunReceipt> {
    const value = await authenticatedRequest<BackgroundPreviewRunReceipt>('/media-analysis/runs', { ...options, method: 'POST', body: input });
    if (!record(value) || !nonempty(value.RunId) || !nonempty(value.TaskId) || typeof value.Admitted !== 'boolean' || !count(value.Queued)) throw invalid();
    return value;
  },
};
