import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export interface DeletionFolder { Id: string; Name: string; Type: string; LibraryId: string; LibraryName: string; ParentId: string; Path: string }
export interface DeletionFolders { Items: DeletionFolder[]; TotalRecordCount: number; StartIndex: number; Limit: number }
export interface DeletionFoldersQuery { SearchTerm?: string; LibraryId?: string; StartIndex: number; Limit: number }
const types = ['CollectionFolder', 'Folder', 'Series', 'Season', 'MusicAlbum', 'MusicArtist'];

export async function getDeletionFolders(query: DeletionFoldersQuery, options: RequestOptions = {}): Promise<DeletionFolders> {
  const params = new URLSearchParams({ StartIndex: String(query.StartIndex), Limit: String(query.Limit) });
  if (query.SearchTerm) params.set('SearchTerm', query.SearchTerm);
  if (query.LibraryId) params.set('LibraryId', query.LibraryId);
  const value = await authenticatedRequest<DeletionFolders>(`/policy/deletion-folders?${params}`, options);
  if (!value || !Array.isArray(value.Items) || !Number.isSafeInteger(value.TotalRecordCount) || value.TotalRecordCount < 0 || value.StartIndex !== query.StartIndex || value.Limit !== query.Limit || value.Items.length > query.Limit
    || value.Items.some((item) => !item || ['Id', 'Name', 'Type', 'LibraryId', 'LibraryName', 'ParentId', 'Path'].some((key) => typeof item[key as keyof DeletionFolder] !== 'string') || !item.Id || !types.includes(item.Type))
    || new Set(value.Items.map((item) => item.Id)).size !== value.Items.length) throw new ApiError('The deletion folder list is incomplete. Reload before choosing a folder.', { code: 'invalid_response' });
  return value;
}
