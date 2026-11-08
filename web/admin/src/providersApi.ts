import { ApiError, authenticatedRequest } from './api';
import type { MetadataDetail, RequestOptions } from './api';
import { decodeArtworkCollection } from './artworkApi';
import type { ArtworkCollection, ImageType } from './artworkApi';

export interface OnlineProvider {
  Id: string;
  Name: string;
  Configured: boolean;
  Capabilities: string[];
  Attribution: string;
  Website: string;
}

export interface OnlineProviders { Enabled: boolean; Items: OnlineProvider[] }
export type ProviderId = 'tmdb' | 'musicbrainz' | 'opensubtitles';
export interface ProviderConfigurationField {
  Id: string; Label: string; Secret: boolean; HasValue: boolean; Source: 'deployment' | 'override';
}
export interface ConfigurableProvider {
  Id: ProviderId; Name: string; Configured: boolean; Fields: ProviderConfigurationField[];
}
export interface ProviderConfiguration {
  Revision: string; DeploymentEnabled: boolean; Enabled: boolean; Items: ConfigurableProvider[];
}
export type ProviderFieldChange = { Mode: 'replace'; Value: string } | { Mode: 'clear' | 'default' };
export interface ProviderConfigurationInput { Revision: string; Fields: Record<string, ProviderFieldChange> }
export type ProviderTestStatus = 'passed' | 'disabled' | 'not_configured' | 'authentication_failed' | 'quota' | 'unavailable';
export interface ProviderConnectionTest { Provider: ProviderId; Status: ProviderTestStatus; CheckedAt: string }
export interface MetadataCandidate {
  Provider: string; Id: string; Type: string; Language: string;
  Name: string; OriginalTitle?: string; Year?: number; Overview?: string; Score?: number;
}
export interface ImageCandidate {
  Provider: string; Id: string; Type: string; Language: string;
  ImageId: string; ImageType: string; Width: number; Height: number; PreviewUrl: string;
}
export interface ProviderImageInput {
  Provider: string; Id: string; Language: string; ImageId: string; ImageType: string; ImageIndex: number; Revision: string;
  ArtworkRevision?: string;
}
export interface ManagedProviderImageInput extends ProviderImageInput { ArtworkRevision: string }
export interface ManagedProviderImageResult {
  Provider: string; ProviderId: string; ImageId: string; ImageType: string; ImageIndex: number;
  Width: number; Height: number; Tag: string; Artwork: ArtworkCollection;
}
export type MissingImageStatus = 'downloaded' | 'no_candidate' | 'already_present' | 'managed' | 'unmatched' | 'unsupported';
export interface MissingImageResult {
  Status: MissingImageStatus; ItemId: string; LibraryId: string; ImageType: ImageType; Artwork: ArtworkCollection;
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
const configurationFields: Record<ProviderId, Record<string, boolean>> = {
  tmdb: { Token: true },
  musicbrainz: { UserAgent: false },
  opensubtitles: { APIKey: true, Username: false, Password: true, UserAgent: false },
};
function record(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function exact(value: Record<string, unknown>, keys: readonly string[]): boolean {
  return Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key));
}
function validRevision(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9]\d*$/.test(value) && value.length <= 19 && BigInt(value) <= 9223372036854775807n;
}
function validProviderId(value: unknown): value is ProviderId { return typeof value === 'string' && Object.hasOwn(configurationFields, value); }
function invalidConfiguration(): never {
  throw new ApiError('The provider configuration response is incomplete. Reload before editing.', { code: 'invalid_response' });
}
function decodeConfiguration(value: unknown): ProviderConfiguration {
  if (!record(value) || !exact(value, ['Revision', 'DeploymentEnabled', 'Enabled', 'Items']) || !validRevision(value.Revision)
    || typeof value.DeploymentEnabled !== 'boolean' || typeof value.Enabled !== 'boolean' || !Array.isArray(value.Items)
    || value.Items.length !== Object.keys(configurationFields).length) invalidConfiguration();
  if (value.Enabled && !value.DeploymentEnabled) invalidConfiguration();
  const seen = new Set<string>();
  for (const provider of value.Items) {
    if (!record(provider) || !exact(provider, ['Id', 'Name', 'Configured', 'Fields']) || !validProviderId(provider.Id)
      || seen.has(provider.Id) || typeof provider.Name !== 'string' || !provider.Name || provider.Name.length > 256
      || typeof provider.Configured !== 'boolean' || !Array.isArray(provider.Fields)) invalidConfiguration();
    seen.add(provider.Id);
    const expected = configurationFields[provider.Id];
    if (provider.Fields.length !== Object.keys(expected).length) invalidConfiguration();
    const fields = new Set<string>();
    for (const field of provider.Fields) {
      // Secret-bearing additions are rejected rather than retained in React state.
      if (!record(field) || !exact(field, ['Id', 'Label', 'Secret', 'HasValue', 'Source']) || typeof field.Id !== 'string'
        || !Object.hasOwn(expected, field.Id) || fields.has(field.Id) || field.Secret !== expected[field.Id]
        || typeof field.Label !== 'string' || !field.Label || field.Label.length > 256 || typeof field.HasValue !== 'boolean'
        || typeof field.Source !== 'string' || !['deployment', 'override'].includes(field.Source)) invalidConfiguration();
      fields.add(field.Id);
    }
  }
  return value as unknown as ProviderConfiguration;
}
function providerConfigurationPath(provider: ProviderId): string {
  if (!validProviderId(provider)) throw new ApiError('Select a supported provider before editing.', { code: 'invalid_provider' });
  return `/providers/${provider}`;
}
const selection = (value: MetadataCandidate | ImageCandidate) => Boolean(value) && strings(value.Provider, value.Id, value.Type, value.Language);
function itemPath(itemId: string, action: string): string { return `/items/${encodeURIComponent(itemId)}/providers/${action}`; }
function post<T>(itemId: string, action: string, body: unknown, options: RequestOptions): Promise<T> {
  return authenticatedRequest<T>(itemPath(itemId, action), { ...options, method: 'POST', body });
}

function invalidImageResponse(): never {
  throw new ApiError('The image response is incomplete. Reload images before making changes.', { code: 'invalid_response' });
}

async function applyImage(itemId: string, input: ManagedProviderImageInput, options?: RequestOptions): Promise<ManagedProviderImageResult>;
async function applyImage(itemId: string, input: ProviderImageInput, options?: RequestOptions): Promise<unknown>;
async function applyImage(itemId: string, input: ProviderImageInput, options: RequestOptions = {}): Promise<unknown> {
  const result = await post<unknown>(itemId, 'image', input, options);
  if (input.ArtworkRevision === undefined) return result;
  if (!record(result) || result.Provider !== input.Provider || result.ProviderId !== input.Id || result.ImageId !== input.ImageId
    || result.ImageType !== input.ImageType || result.ImageIndex !== input.ImageIndex || typeof result.Tag !== 'string' || !result.Tag
    || !count(result.Width) || !result.Width || !count(result.Height) || !result.Height) invalidImageResponse();
  const artwork = decodeArtworkCollection(result.Artwork, { kind: 'items', id: itemId, name: '' });
  const effective = artwork.Items.find((image) => image.ImageType === input.ImageType && image.ImageIndex === input.ImageIndex);
  if (!effective || effective.Tag !== result.Tag || effective.Width !== result.Width || effective.Height !== result.Height) invalidImageResponse();
  return { Provider: result.Provider, ProviderId: result.ProviderId, ImageId: result.ImageId, ImageType: result.ImageType,
    ImageIndex: result.ImageIndex, Width: result.Width, Height: result.Height, Tag: result.Tag, Artwork: artwork };
}

export const providersApi = {
  async getConfiguration(options: RequestOptions = {}): Promise<ProviderConfiguration> {
    return decodeConfiguration(await authenticatedRequest<unknown>('/providers/configuration', options));
  },
  async updateConfiguration(provider: ProviderId, input: ProviderConfigurationInput, options: RequestOptions = {}): Promise<ProviderConfiguration> {
    if (!validRevision(input.Revision)) throw new ApiError('Reload provider configuration before saving.', { code: 'invalid_provider_revision' });
    return decodeConfiguration(await authenticatedRequest<unknown>(`${providerConfigurationPath(provider)}/configuration`, { ...options, method: 'PUT', body: input }));
  },
  async testConnection(provider: ProviderId, revision: string, options: RequestOptions = {}): Promise<ProviderConnectionTest> {
    if (!validRevision(revision)) throw new ApiError('Reload provider configuration before testing.', { code: 'invalid_provider_revision' });
    const result = await authenticatedRequest<unknown>(`${providerConfigurationPath(provider)}/test`, { ...options, method: 'POST', body: { Revision: revision } });
    if (!record(result) || !exact(result, ['Provider', 'Status', 'CheckedAt']) || result.Provider !== provider
      || typeof result.Status !== 'string' || !['passed', 'disabled', 'not_configured', 'authentication_failed', 'quota', 'unavailable'].includes(result.Status)
      || typeof result.CheckedAt !== 'string' || result.CheckedAt.length > 64 || !Number.isFinite(Date.parse(result.CheckedAt))) {
      throw new ApiError('The provider test response is incomplete. Reload before testing again.', { code: 'invalid_response' });
    }
    return result as unknown as ProviderConnectionTest;
  },
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
  applyImage,
  async downloadMissingImage(itemId: string, input: { LibraryId: string; ImageType: ImageType }, options: RequestOptions = {}): Promise<MissingImageResult> {
    const result = await post<unknown>(itemId, 'images/missing', input, options);
    const statuses: MissingImageStatus[] = ['downloaded', 'no_candidate', 'already_present', 'managed', 'unmatched', 'unsupported'];
    if (!record(result) || !statuses.includes(result.Status as MissingImageStatus) || result.ItemId !== itemId
      || result.LibraryId !== input.LibraryId || result.ImageType !== input.ImageType) invalidImageResponse();
    const artwork = decodeArtworkCollection(result.Artwork, { kind: 'items', id: itemId, name: '' });
    if (['downloaded', 'already_present'].includes(result.Status as string) && !artwork.Items.some((image) => image.ImageType === input.ImageType)) invalidImageResponse();
    return { Status: result.Status as MissingImageStatus, ItemId: itemId, LibraryId: input.LibraryId, ImageType: input.ImageType, Artwork: artwork };
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
