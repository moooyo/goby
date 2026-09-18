import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export type CollectionKind = 'playlists' | 'collections';
export interface CollectionShare { UserId: string; CanEdit: boolean }
export interface ManagedCollection {
  Id: string; Name: string; Type: 'Playlist' | 'BoxSet'; IsFolder: boolean;
  ParentId: string; OwnerId: string; MediaType: string; IsPublic: boolean; IsLocked: boolean;
  ChildCount: number; Shares: CollectionShare[];
}
export interface CollectionMember { Id: string; Name: string; Type: string; PlaylistItemId?: string }
export interface CollectionPage<T> { Items: T[]; TotalRecordCount: number }
export interface CollectionPatch { Name?: string; IsPublic?: boolean; IsLocked?: boolean; Shares?: CollectionShare[] }
export interface CollectionCreate { Name: string; MediaType?: string; IsPublic: boolean; IsLocked: boolean }

function invalid(): never { throw new ApiError('The server returned an invalid collection response.', { code: 'invalid_response' }); }
function validEmptyResult(value: unknown): void {
  if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).length !== 0) invalid();
}
function validCollection(value: ManagedCollection, kind: CollectionKind, id?: string): ManagedCollection {
  if (!value || ![value.Id, value.Name, value.Type, value.OwnerId, value.ParentId, value.MediaType].every((entry) => typeof entry === 'string')
    || !value.Id || (id !== undefined && value.Id !== id) || value.Type !== (kind === 'playlists' ? 'Playlist' : 'BoxSet')
    || value.IsFolder !== true || typeof value.IsPublic !== 'boolean' || typeof value.IsLocked !== 'boolean'
    || !Number.isSafeInteger(value.ChildCount) || value.ChildCount < 0 || !Array.isArray(value.Shares)
    || !value.Shares.every((share) => share && typeof share.UserId === 'string' && share.UserId && typeof share.CanEdit === 'boolean')
    || new Set(value.Shares.map((share) => share.UserId)).size !== value.Shares.length) invalid();
  return value;
}
function validPage<T>(value: CollectionPage<T>, start: number, limit: number, validate: (value: T) => void, key: (value: T) => string): CollectionPage<T> {
  if (!value || !Array.isArray(value.Items) || !Number.isSafeInteger(value.TotalRecordCount) || value.TotalRecordCount < 0
    || value.Items.length > Math.min(limit, Math.max(0, value.TotalRecordCount - start))) invalid();
  value.Items.forEach(validate);
  if (new Set(value.Items.map(key)).size !== value.Items.length) invalid();
  return value;
}
function path(kind: CollectionKind, id?: string) { return `/${kind}${id ? `/${encodeURIComponent(id)}` : ''}`; }
function pageQuery(start: number, limit: number, search?: string) { const query = new URLSearchParams({ StartIndex: String(start), Limit: String(limit) }); if (search) query.set('SearchTerm', search); return query; }

export const collectionsApi = {
  async list(kind: CollectionKind, start: number, limit: number, search: string, options: RequestOptions = {}) {
    return validPage(await authenticatedRequest<CollectionPage<ManagedCollection>>(`${path(kind)}?${pageQuery(start, limit, search)}`, options), start, limit, (value) => validCollection(value, kind), (value) => value.Id);
  },
  async get(kind: CollectionKind, id: string, options: RequestOptions = {}) {
    return validCollection(await authenticatedRequest<ManagedCollection>(path(kind, id), options), kind, id);
  },
  async create(kind: CollectionKind, input: CollectionCreate, options: RequestOptions = {}) {
    const result = await authenticatedRequest<{ Id: string; Name: string; ItemAddedCount?: number }>(path(kind), { ...options, method: 'POST', body: input });
    if (!result || typeof result.Id !== 'string' || !result.Id || typeof result.Name !== 'string' || !result.Name
      || (kind === 'playlists' && (typeof result.ItemAddedCount !== 'number' || !Number.isSafeInteger(result.ItemAddedCount) || result.ItemAddedCount < 0))) invalid();
    return result;
  },
  async update(kind: CollectionKind, id: string, input: CollectionPatch, options: RequestOptions = {}) {
    return validCollection(await authenticatedRequest<ManagedCollection>(path(kind, id), { ...options, method: 'POST', body: input }), kind, id);
  },
  async remove(kind: CollectionKind, id: string, options: RequestOptions = {}) {
    validEmptyResult(await authenticatedRequest<unknown>(path(kind, id), { ...options, method: 'DELETE' }));
  },
  async members(kind: CollectionKind, id: string, start: number, limit: number, options: RequestOptions = {}) {
    return validPage(await authenticatedRequest<CollectionPage<CollectionMember>>(`${path(kind, id)}/items?${pageQuery(start, limit)}`, options), start, limit, (member) => {
      if (!member || ![member.Id, member.Name, member.Type].every((value) => typeof value === 'string') || !member.Id || (kind === 'playlists' && (typeof member.PlaylistItemId !== 'string' || !member.PlaylistItemId))) invalid();
    }, (member) => kind === 'playlists' ? member.PlaylistItemId! : member.Id);
  },
  async addMembers(kind: CollectionKind, id: string, ids: string[], options: RequestOptions = {}) {
    const query = new URLSearchParams({ Ids: ids.join(',') });
    const result = await authenticatedRequest<{ Id: string; ItemAddedCount: number }>(`${path(kind, id)}/items?${query}`, { ...options, method: 'POST' });
    if (kind === 'playlists') {
      if (!result || result.Id !== id || !Number.isSafeInteger(result.ItemAddedCount) || result.ItemAddedCount < 0) invalid();
    } else validEmptyResult(result);
  },
  async removeMember(kind: CollectionKind, id: string, member: CollectionMember, options: RequestOptions = {}) {
    const query = new URLSearchParams({ [kind === 'playlists' ? 'EntryIds' : 'Ids']: kind === 'playlists' ? member.PlaylistItemId! : member.Id });
    validEmptyResult(await authenticatedRequest<unknown>(`${path(kind, id)}/items?${query}`, { ...options, method: 'DELETE' }));
  },
  async move(id: string, entryId: string, index: number, options: RequestOptions = {}) {
    validEmptyResult(await authenticatedRequest<unknown>(`${path('playlists', id)}/items/${encodeURIComponent(entryId)}/move/${index}`, { ...options, method: 'POST' }));
  },
};
