import { ApiError, authenticatedRequest } from './api';
import type { AuthenticatedRequestOptions, RequestOptions } from './api';

export type SeriesOrder = 'Aired' | 'Dvd' | 'Absolute';
export interface SeriesOrderEntry {
  ProviderEpisodeId: string; SeasonNumber: number; EpisodeNumber: number;
  GroupOrder: number; EpisodeOrder: number; Ordinal: number; GroupName: string;
}
export interface SeriesOrderCoverage {
  PhysicalEpisodeCount: number; ExpectedEpisodeCount: number; UnmappedCount: number;
  Unmapped: { Kind: 'physical' | 'expected'; Id: string; Name: string; SeasonNumber: number | null; EpisodeNumber: number | null }[];
}
export interface SeriesOrderDetail {
  SeriesId: string; SeriesName: string; Revision: string; DisplayOrder: SeriesOrder;
  ProviderSeriesId: string; GroupId: string; GroupName: string; MappingSHA256: string;
  Entries: SeriesOrderEntry[]; Coverage: SeriesOrderCoverage; SourceMatches: boolean;
  FetchedAt: string | null; UpdatedAt: string | null;
}
export interface SeriesOrderGroup { Id: string; Name: string; DisplayOrder: Exclude<SeriesOrder, 'Aired'>; EpisodeCount: number; GroupCount: number }
export interface SeriesOrderPreview {
  Revision: string; DisplayOrder: Exclude<SeriesOrder, 'Aired'>; GroupId: string; GroupName: string;
  MappingSHA256: string; Entries: SeriesOrderEntry[]; Coverage: SeriesOrderCoverage;
}
export interface SeriesOrderInput { Revision: string; DisplayOrder: SeriesOrder; GroupId: string; MappingSHA256: string }

const count = (value: unknown) => Number.isSafeInteger(value) && (value as number) >= 0;
const text = (value: unknown, maximum: number) => typeof value === 'string' && new TextEncoder().encode(value).length <= maximum;
const order = (value: unknown): value is SeriesOrder => value === 'Aired' || value === 'Dvd' || value === 'Absolute';
const revision = (value: unknown) => typeof value === 'string' && /^(0|[1-9]\d{0,18})$/.test(value) && BigInt(value) <= 9223372036854775807n;
const hash = (value: unknown, empty = false) => typeof value === 'string' && (empty && value === '' || /^[a-f0-9]{64}$/.test(value));
const time = (value: unknown) => value === null || typeof value === 'string' && Number.isFinite(Date.parse(value));
function invalid(): never { throw new ApiError('The series order response is incomplete. Reload before editing.', { code: 'invalid_response' }); }
async function request<T>(target: string, options: AuthenticatedRequestOptions = {}): Promise<T> {
  const controller = new AbortController();
  const abort = () => controller.abort(options.signal?.reason);
  if (options.signal?.aborted) abort();
  else options.signal?.addEventListener('abort', abort, { once: true });
  const timer = setTimeout(() => controller.abort(), 40_000);
  try { return await authenticatedRequest<T>(target, { ...options, signal: controller.signal }); }
  catch (error) {
    if (controller.signal.aborted && !options.signal?.aborted) throw new ApiError('The series order request timed out. Reload to check the saved state.', { code: 'network_error' });
    throw error;
  } finally { clearTimeout(timer); options.signal?.removeEventListener('abort', abort); }
}
function entries(value: SeriesOrderEntry[]): boolean {
  if (!Array.isArray(value) || value.length > 2000) return false;
  const pairs = new Set<string>(), ranks = new Set<string>(), ids = new Set<string>();
  return value.every((entry, index) => {
    if (!entry || !/^[1-9]\d{0,18}$/.test(entry.ProviderEpisodeId) || !count(entry.SeasonNumber) || entry.SeasonNumber > 9999
      || !count(entry.EpisodeNumber) || entry.EpisodeNumber > 9999 || !count(entry.GroupOrder) || entry.GroupOrder > 2000
      || !count(entry.EpisodeOrder) || entry.EpisodeOrder > 2000 || entry.Ordinal !== index || !text(entry.GroupName, 512)) return false;
    const pair = `${entry.SeasonNumber}:${entry.EpisodeNumber}`, rank = `${entry.GroupOrder}:${entry.EpisodeOrder}`;
    if (pairs.has(pair) || ranks.has(rank) || ids.has(entry.ProviderEpisodeId)) return false;
    pairs.add(pair); ranks.add(rank); ids.add(entry.ProviderEpisodeId);
    return true;
  });
}
function coverage(value: SeriesOrderCoverage): boolean {
  return Boolean(value) && count(value.PhysicalEpisodeCount) && count(value.ExpectedEpisodeCount) && count(value.UnmappedCount)
    && Array.isArray(value.Unmapped) && value.Unmapped.length <= 50 && value.Unmapped.length <= value.UnmappedCount
    && value.Unmapped.every((entry) => entry && ['physical', 'expected'].includes(entry.Kind) && text(entry.Id, 256) && text(entry.Name, 4096)
      && (entry.SeasonNumber === null || count(entry.SeasonNumber)) && (entry.EpisodeNumber === null || count(entry.EpisodeNumber)));
}
function detail(value: SeriesOrderDetail, seriesId: string): SeriesOrderDetail {
  if (!value || value.SeriesId !== seriesId || !text(value.SeriesName, 4096) || !revision(value.Revision) || !order(value.DisplayOrder)
    || !text(value.ProviderSeriesId, 32) || !text(value.GroupId, 128) || !text(value.GroupName, 512) || !hash(value.MappingSHA256, value.DisplayOrder === 'Aired')
    || !entries(value.Entries) || !coverage(value.Coverage) || typeof value.SourceMatches !== 'boolean' || !time(value.FetchedAt) || !time(value.UpdatedAt)
    || value.DisplayOrder !== 'Aired' && (!value.GroupId || !value.Entries.length) || value.DisplayOrder === 'Aired' && (value.GroupId !== '' || value.Entries.length !== 0)) invalid();
  return value;
}
const path = (seriesId: string) => `/series/${encodeURIComponent(seriesId)}/display-order`;
export const seriesOrderApi = {
  async get(seriesId: string, options: RequestOptions = {}) { return detail(await request<SeriesOrderDetail>(path(seriesId), options), seriesId); },
  async groups(seriesId: string, options: RequestOptions = {}): Promise<SeriesOrderGroup[]> {
    const result = await request<{ Items: SeriesOrderGroup[] }>(path(seriesId) + '/groups', options);
    if (!result || !Array.isArray(result.Items) || result.Items.length > 200 || !result.Items.every((group) => group
      && text(group.Id, 128) && group.Id !== '' && text(group.Name, 512) && group.Name !== '' && ['Dvd', 'Absolute'].includes(group.DisplayOrder)
      && count(group.EpisodeCount) && group.EpisodeCount > 0 && group.EpisodeCount <= 2000 && count(group.GroupCount) && group.GroupCount > 0)) invalid();
    if (new Set(result.Items.map((group) => group.Id)).size !== result.Items.length) invalid();
    return result.Items;
  },
  async preview(seriesId: string, groupId: string, options: RequestOptions = {}): Promise<SeriesOrderPreview> {
    const result = await request<SeriesOrderPreview>(path(seriesId) + '/groups/' + encodeURIComponent(groupId), options);
    if (!result || !revision(result.Revision) || !['Dvd', 'Absolute'].includes(result.DisplayOrder) || result.GroupId !== groupId
      || !text(result.GroupName, 512) || !hash(result.MappingSHA256) || !entries(result.Entries) || !coverage(result.Coverage)) invalid();
    return result;
  },
  async update(seriesId: string, input: SeriesOrderInput, options: RequestOptions = {}) {
    const result = detail(await request<SeriesOrderDetail>(path(seriesId), { ...options, method: 'PUT', body: input }), seriesId);
    if (result.DisplayOrder !== input.DisplayOrder || result.GroupId !== input.GroupId || result.MappingSHA256 !== input.MappingSHA256) invalid();
    return result;
  },
};
