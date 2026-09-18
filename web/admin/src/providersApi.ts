import { ApiError, authenticatedRequest } from './api';
import type { MetadataDetail, RequestOptions } from './api';

export interface OnlineProvider {
  Id: string;
  Name: string;
  Configured: boolean;
  Capabilities: string[];
  Attribution: string;
  Website: string;
}

export interface OnlineProviders { Enabled: boolean; Items: OnlineProvider[] }
export interface MetadataCandidate {
  Provider: string; Id: string; Type: string; Language: string;
  Name: string; OriginalTitle?: string; Year?: number; Overview?: string; Score?: number;
}
export interface ImageCandidate {
  Provider: string; Id: string; Type: string; Language: string;
  ImageId: string; ImageType: string; Width: number; Height: number; PreviewUrl: string;
}
export interface SubtitleCandidate {
  Provider: string; Id: string; FileId: number; Language: string; Name: string;
  HearingImpaired: boolean; IsForced?: boolean; DownloadCount: number; MovieHashMatch: boolean;
}
export interface ProviderProvenance { Provider: string; ProviderId: string; SourceUrl: string; FetchedAt: string; Fields: string[] }

function invalid(): never { throw new ApiError('The server returned an invalid provider response.', { code: 'invalid_response' }); }
function list<T>(value: { Items: T[] }, valid?: (entry: T) => boolean): { Items: T[] } {
  if (!value || !Array.isArray(value.Items)) invalid();
  if (valid && !value.Items.every(valid)) invalid();
  return value;
}
const strings = (...values: unknown[]) => values.every((value) => typeof value === 'string');
const count = (value: unknown) => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
const selection = (value: MetadataCandidate | ImageCandidate) => Boolean(value) && strings(value.Provider, value.Id, value.Type, value.Language);
function itemPath(itemId: string, action: string): string { return `/items/${encodeURIComponent(itemId)}/providers/${action}`; }
function post<T>(itemId: string, action: string, body: unknown, options: RequestOptions): Promise<T> {
  return authenticatedRequest<T>(itemPath(itemId, action), { ...options, method: 'POST', body });
}

export const providersApi = {
  async getProviders(options: RequestOptions = {}): Promise<OnlineProviders> {
    const result = await authenticatedRequest<OnlineProviders>('/providers', options);
    if (typeof result.Enabled !== 'boolean') invalid();
    list(result);
    if (!result.Items.every((provider) => provider && typeof provider.Id === 'string' && typeof provider.Name === 'string'
      && typeof provider.Configured === 'boolean' && Array.isArray(provider.Capabilities)
      && provider.Capabilities.every((capability) => typeof capability === 'string'))) invalid();
    return result;
  },
  async search(itemId: string, input: { Provider: string; Name: string; Year?: number; Language: string }, options: RequestOptions = {}) {
    return list(await post<{ Items: MetadataCandidate[] }>(itemId, 'search', input, options), (value) => selection(value) && strings(value.Name)
      && (value.Year === undefined || count(value.Year)) && (value.OriginalTitle === undefined || typeof value.OriginalTitle === 'string')
      && (value.Overview === undefined || typeof value.Overview === 'string'));
  },
  apply(itemId: string, input: { Provider: string; Id: string; Language: string; Revision: string }, options: RequestOptions = {}) {
    return post<MetadataDetail>(itemId, 'apply', input, options);
  },
  refresh(itemId: string, input: { Provider: string; Language: string; Revision: string }, options: RequestOptions = {}) {
    return post<MetadataDetail>(itemId, 'refresh', input, options);
  },
  async images(itemId: string, input: { Provider: string; Id: string; Language: string }, options: RequestOptions = {}) {
    return list(await post<{ Items: ImageCandidate[] }>(itemId, 'images', input, options), (value) => selection(value)
      && strings(value.ImageId, value.ImageType, value.PreviewUrl) && count(value.Width) && count(value.Height));
  },
  applyImage(itemId: string, input: { Provider: string; Id: string; Language: string; ImageId: string; ImageType: string; ImageIndex: number; Revision: string }, options: RequestOptions = {}) {
    return post<unknown>(itemId, 'image', input, options);
  },
  async provenance(itemId: string, options: RequestOptions = {}) {
    return list(await authenticatedRequest<{ Items: ProviderProvenance[] }>(itemPath(itemId, 'provenance'), options), (value) => Boolean(value)
      && strings(value.Provider, value.ProviderId, value.SourceUrl, value.FetchedAt) && Array.isArray(value.Fields) && value.Fields.every((field) => typeof field === 'string'));
  },
  async searchSubtitles(itemId: string, input: { Languages: string[]; HearingImpaired: boolean }, options: RequestOptions = {}) {
    return list(await post<{ Items: SubtitleCandidate[] }>(itemId, 'subtitles/search', input, options), (value) => Boolean(value)
      && strings(value.Provider, value.Id, value.Language, value.Name) && count(value.FileId) && count(value.DownloadCount)
      && typeof value.HearingImpaired === 'boolean' && typeof value.MovieHashMatch === 'boolean'
      && (value.IsForced === undefined || typeof value.IsForced === 'boolean'));
  },
  downloadSubtitle(itemId: string, input: Pick<SubtitleCandidate, 'Provider' | 'Id' | 'FileId' | 'Language' | 'Name'>, options: RequestOptions = {}) {
    return post<unknown>(itemId, 'subtitles/download', input, options);
  },
};

export function providerWebsite(value: string): string | undefined {
  try { const url = new URL(value); return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : undefined; } catch { return undefined; }
}

export function providerImagePreview(value: string): string | undefined {
  return /^\/admin\/v1\/items\/[^/?#]+\/providers\/image-preview(?:\?|$)/.test(value) ? value : undefined;
}
