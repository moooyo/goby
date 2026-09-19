import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export type EntityKind = 'Person' | 'Genre' | 'Studio' | 'Tag' | 'MusicArtist';
export interface CatalogEntity { Id: string; Name: string; Type: string }
export interface CatalogEntityPage { Items: CatalogEntity[]; TotalRecordCount: number; StartIndex: number; Limit: number }
export type EntityView = EntityKind | 'AlbumArtist' | 'MusicGenre';
export const catalogEntitiesApi = {
  async list(view: EntityView, search: string, start: number, options: RequestOptions = {}): Promise<CatalogEntityPage> {
    const query = new URLSearchParams({ SearchTerm: search, StartIndex: String(start), Limit: '25' });
    let path = '/entities';
    if (view === 'MusicArtist' || view === 'AlbumArtist') { path = '/music/artists'; query.set('Role', view === 'AlbumArtist' ? 'AlbumArtist' : 'Artist'); }
    else if (view === 'MusicGenre') path = '/music/genres';
    else query.set('Kind', view);
    const result = await authenticatedRequest<CatalogEntityPage>(`${path}?${query}`, options);
    if (!result || !Array.isArray(result.Items) || result.Items.length > 25 || !result.Items.every((item) => typeof item.Id === 'string' && item.Id !== '' && typeof item.Name === 'string' && typeof item.Type === 'string')
      || !Number.isSafeInteger(result.TotalRecordCount) || result.TotalRecordCount < result.Items.length
      || (result.StartIndex !== undefined && result.StartIndex !== start) || (result.Limit !== undefined && result.Limit !== 25)) throw new ApiError('The catalog list is incomplete. Reload before selecting an entry.', { code: 'invalid_response' });
    return { ...result, StartIndex: start, Limit: 25 };
  },
};
