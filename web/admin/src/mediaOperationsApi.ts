import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export const mediaOperationKinds = ['remove_embedded_subtitle', 'subtitle_ocr'] as const;
export const mediaOperationStates = ['queued', 'running', 'ready', 'applying', 'completed', 'failed', 'cancelled', 'interrupted', 'stale', 'recovery_required'] as const;
export type MediaOperationKind = typeof mediaOperationKinds[number];
export type MediaOperationState = typeof mediaOperationStates[number];
export interface MediaOperationParameters {
  Profile?: string; ModelIds?: string[]; OutputFormat?: string; Language?: string; Title?: string;
  IsDefault?: boolean; IsForced?: boolean; IsHearingImpaired?: boolean;
}
export interface MediaOperation {
  Id: string; Kind: MediaOperationKind; State: MediaOperationState; Revision: string;
  ItemId: string; LibraryId: string; MediaSourceId: string; SourceRevision: string; StreamIndex: number;
  Parameters: MediaOperationParameters; Progress: { Stage: string; Processed: number; Total: number };
  ResultSummary: Record<string, unknown>; ResultHash: string; PublicationPhase: string;
  CancelRequestedAt: string | null; CreatedAt: string; UpdatedAt: string; StartedAt: string | null; FinishedAt: string | null;
  ErrorCode: string; ErrorMessage: string; CanCancel: boolean; CanReview: boolean; CanApply: boolean;
  CanRecover: boolean; Applied: boolean; TargetPresent: boolean;
}
export interface MediaOperationPage { Items: MediaOperation[]; TotalRecordCount: number; StartIndex: number; Limit: number }
export interface MediaOperationCue {
  Ordinal: number; OriginalStartTicks: string; OriginalEndTicks: string; OriginalText: string;
  StartTicks: string; EndTicks: string; Text: string; Included: boolean; Confidence: number | null;
  Warnings: string[]; ImageSHA256: string; IsForced: boolean; IsHearingImpaired: boolean;
}
export interface MediaOperationCueEdit { Ordinal: number; StartTicks: string; EndTicks: string; Text: string; Included: boolean }
export interface MediaOperationCuePage { Operation: MediaOperation; Items: MediaOperationCue[]; TotalRecordCount: number; StartIndex: number; Limit: number }
export interface MediaProcessingCapabilities {
  Enabled: boolean; Available: boolean; UnavailableReason: string; MaxConcurrent: number; MaxQueued: number; WritableProfiles: string[];
  OCR: { Available: boolean; UnavailableReason: string; Models: { Id: string; Language: string }[]; OutputFormats: string[] };
}
export interface MediaProcessingStream {
  Index: number; Codec: string; CodecType: string; Language: string; Title: string;
  IsDefault: boolean; IsForced: boolean; IsHearingImpaired: boolean; IsExternal: boolean; IsTextSubtitleStream: boolean;
}
export interface MediaProcessingTarget {
  ItemId: string; MediaSourceId: string; SourceRevision: string; Container: string;
  Streams: MediaProcessingStream[]; Capabilities: MediaProcessingCapabilities;
}
export interface MediaOperationRequest {
  RequestId: string; Kind: MediaOperationKind; MediaSourceId: string; SourceRevision: string;
  StreamIndex: number; Parameters: MediaOperationParameters;
}
export interface MediaOperationAdmission { Operation: MediaOperation; Admitted: boolean }
export interface MediaOperationApplyRequest { Revision: string; SourceRevision: string; ResultHash: string; RequestId: string }
export interface MediaOperationQuery { StartIndex?: number; Limit?: number; ItemId?: string; Kind?: MediaOperationKind; State?: MediaOperationState }

function object(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === 'string'; }
function nonempty(value: unknown): value is string { return text(value) && value.length > 0; }
function natural(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0; }
function decimal(value: unknown, positive = false): value is string {
  return text(value) && (positive ? /^[1-9]\d{0,18}$/ : /^(0|[1-9]\d{0,18})$/).test(value) && BigInt(value) <= 9223372036854775807n;
}
function timestamp(value: unknown): value is string { return nonempty(value) && !Number.isNaN(Date.parse(value)); }
function strings(value: unknown): value is string[] { return Array.isArray(value) && value.every(text); }
function invalid(): never { throw new ApiError('The media processing response is incomplete. Reload before making changes.', { code: 'invalid_response' }); }
function operation(value: MediaOperation, id?: string): MediaOperation {
  if (!object(value) || !/^[0-9a-f]{32}$/.test(value.Id) || (id !== undefined && value.Id !== id)
    || !mediaOperationKinds.includes(value.Kind) || !mediaOperationStates.includes(value.State) || !decimal(value.Revision, true)
    || ![value.ItemId, value.LibraryId, value.MediaSourceId, value.SourceRevision].every(nonempty) || !natural(value.StreamIndex)
    || !object(value.Parameters) || !object(value.Progress) || !text(value.Progress.Stage) || !natural(value.Progress.Processed) || !natural(value.Progress.Total)
    || !object(value.ResultSummary) || !text(value.ResultHash) || (value.ResultHash !== '' && !/^[0-9a-f]{64}$/.test(value.ResultHash))
    || !['none', 'prepared', 'catalog_committed', 'done'].includes(value.PublicationPhase)
    || !timestamp(value.CreatedAt) || !timestamp(value.UpdatedAt)
    || ![value.CancelRequestedAt, value.StartedAt, value.FinishedAt].every((entry) => entry === null || timestamp(entry))
    || !text(value.ErrorCode) || !text(value.ErrorMessage)
    || ![value.CanCancel, value.CanReview, value.CanApply, value.CanRecover, value.Applied, value.TargetPresent].every((entry) => typeof entry === 'boolean')) invalid();
  return value;
}
function operationResponse(value: { Operation: MediaOperation }, id?: string): MediaOperation {
  if (!object(value)) invalid();
  return operation(value.Operation, id);
}
function pagination(value: { TotalRecordCount: number; StartIndex: number; Limit: number }, start: number, limit: number): void {
  if (!natural(value.TotalRecordCount) || value.StartIndex !== start || value.Limit !== limit) invalid();
}
function capabilities(value: MediaProcessingCapabilities): void {
  if (!object(value) || ![value.Enabled, value.Available].every((entry) => typeof entry === 'boolean') || !text(value.UnavailableReason)
    || !natural(value.MaxConcurrent) || !natural(value.MaxQueued) || !strings(value.WritableProfiles) || !object(value.OCR)
    || typeof value.OCR.Available !== 'boolean' || !text(value.OCR.UnavailableReason) || !strings(value.OCR.OutputFormats)
    || !Array.isArray(value.OCR.Models) || !value.OCR.Models.every((entry) => object(entry) && nonempty(entry.Id) && nonempty(entry.Language))) invalid();
}
export function mediaOperationIsActive(value: MediaOperation): boolean { return ['queued', 'running', 'applying'].includes(value.State); }
export function mediaOperationNeedsReload(error: unknown): boolean { return !(error instanceof ApiError) || error.status === 409 || error.status === 0 || error.status >= 500 || error.code === 'invalid_response'; }
export function mediaOperationRequestId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 15) | 64; bytes[8] = (bytes[8] & 63) | 128;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
export function mediaTicksToSeconds(value: string): string {
  const ticks = BigInt(value); const fraction = (ticks % 10000000n).toString().padStart(7, '0').replace(/0+$/, '');
  return `${ticks / 10000000n}${fraction ? `.${fraction}` : ''}`;
}
export function mediaSecondsToTicks(value: string): string | undefined {
  if (!/^\d{1,12}(?:\.\d{1,7})?$/.test(value)) return undefined;
  const [whole, fraction = ''] = value.split('.'); const ticks = BigInt(whole) * 10000000n + BigInt(fraction.padEnd(7, '0'));
  return ticks <= 9223372036854775807n ? ticks.toString() : undefined;
}
export function mediaCueImageUrl(operationId: string, ordinal: number): string {
  if (!/^[0-9a-f]{32}$/.test(operationId) || !natural(ordinal) || ordinal >= 50000) invalid();
  return `/admin/v1/media-operations/${operationId}/cues/${ordinal}/image`;
}
export const mediaOperationsApi = {
  async target(itemId: string, options: RequestOptions = {}): Promise<MediaProcessingTarget> {
    const result = await authenticatedRequest<MediaProcessingTarget>(`/items/${encodeURIComponent(itemId)}/media-processing`, options);
    if (!object(result) || result.ItemId !== itemId || !nonempty(result.MediaSourceId) || !nonempty(result.SourceRevision) || !text(result.Container)
      || !Array.isArray(result.Streams) || !result.Streams.every((entry) => object(entry) && natural(entry.Index) && [entry.Codec, entry.CodecType, entry.Language, entry.Title].every(text)
        && [entry.IsDefault, entry.IsForced, entry.IsHearingImpaired, entry.IsExternal, entry.IsTextSubtitleStream].every((flag) => typeof flag === 'boolean'))) invalid();
    capabilities(result.Capabilities); return result;
  },
  async list(query: MediaOperationQuery = {}, options: RequestOptions = {}): Promise<MediaOperationPage> {
    const start = query.StartIndex ?? 0; const limit = query.Limit ?? 25;
    const params = new URLSearchParams({ StartIndex: String(start), Limit: String(limit) });
    for (const field of ['ItemId', 'Kind', 'State'] as const) if (query[field]) params.set(field, query[field]);
    const result = await authenticatedRequest<MediaOperationPage>(`/media-operations?${params}`, options);
    if (!object(result) || !Array.isArray(result.Items) || result.Items.length > limit) invalid();
    pagination(result, start, limit); result.Items.forEach((entry) => operation(entry));
    if (new Set(result.Items.map((entry) => entry.Id)).size !== result.Items.length
      || result.Items.some((entry) => (query.ItemId && entry.ItemId !== query.ItemId) || (query.Kind && entry.Kind !== query.Kind) || (query.State && entry.State !== query.State))) invalid();
    return result;
  },
  async get(id: string, options: RequestOptions = {}): Promise<MediaOperation> {
    return operationResponse(await authenticatedRequest<{ Operation: MediaOperation }>(`/media-operations/${encodeURIComponent(id)}`, options), id);
  },
  async prepare(itemId: string, input: MediaOperationRequest): Promise<MediaOperationAdmission> {
    const result = await authenticatedRequest<MediaOperationAdmission>(`/items/${encodeURIComponent(itemId)}/media-operations`, { method: 'POST', body: input });
    operationResponse(result); if (typeof result.Admitted !== 'boolean' || result.Operation.ItemId !== itemId) invalid(); return result;
  },
  async review(id: string, start = 0, limit = 20, options: RequestOptions = {}): Promise<MediaOperationCuePage> {
    const result = await authenticatedRequest<MediaOperationCuePage>(`/media-operations/${encodeURIComponent(id)}/review?${new URLSearchParams({ StartIndex: String(start), Limit: String(limit) })}`, options);
    operationResponse(result, id); pagination(result, start, limit);
    if (!Array.isArray(result.Items) || result.Items.length > limit || !result.Items.every((cue, index) => object(cue) && natural(cue.Ordinal) && cue.Ordinal === start + index && cue.Ordinal < 50000
      && [cue.OriginalStartTicks, cue.OriginalEndTicks, cue.StartTicks, cue.EndTicks].every((ticks) => decimal(ticks))
      && BigInt(cue.OriginalEndTicks) > BigInt(cue.OriginalStartTicks) && BigInt(cue.EndTicks) > BigInt(cue.StartTicks)
      && text(cue.OriginalText) && text(cue.Text) && [cue.Included, cue.IsForced, cue.IsHearingImpaired].every((flag) => typeof flag === 'boolean')
      && (cue.Confidence === null || (typeof cue.Confidence === 'number' && Number.isFinite(cue.Confidence) && cue.Confidence >= 0 && cue.Confidence <= 100))
      && strings(cue.Warnings) && text(cue.ImageSHA256) && (cue.ImageSHA256 === '' || /^[0-9a-f]{64}$/.test(cue.ImageSHA256)))) invalid();
    return result;
  },
  async saveReview(id: string, revision: string, edits: MediaOperationCueEdit[]): Promise<MediaOperation> {
    return operationResponse(await authenticatedRequest<{ Operation: MediaOperation }>(`/media-operations/${encodeURIComponent(id)}/review`, { method: 'PUT', body: { Revision: revision, Edits: edits } }), id);
  },
  async cancel(id: string, revision: string): Promise<MediaOperation> {
    return operationResponse(await authenticatedRequest<{ Operation: MediaOperation }>(`/media-operations/${encodeURIComponent(id)}/cancel`, { method: 'POST', body: { Revision: revision } }), id);
  },
  async publish(id: string, action: 'apply' | 'recover', input: MediaOperationApplyRequest): Promise<MediaOperationAdmission> {
    const result = await authenticatedRequest<MediaOperationAdmission>(`/media-operations/${encodeURIComponent(id)}/${action}`, { method: 'POST', body: input });
    operationResponse(result, id); if (typeof result.Admitted !== 'boolean') invalid(); return result;
  },
};
