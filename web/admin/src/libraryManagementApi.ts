import { ApiError, authenticatedRequest } from './api';
import type { Library, LibraryResponse, RequestOptions } from './api';

export interface LibraryOptions {
  EnableLocalMetadata: boolean;
  EnableLocalImages: boolean;
}
export interface EditableLibrary extends Library {
  Revision: string;
  LibraryOptions: LibraryOptions;
  RegisteredPaths: { Id: string; Path: string; ItemCount: number }[];
}
export interface LibraryUpdate {
  Revision: string; Name: string; Paths: string[];
  PathReplacements: { From: string; To: string }[];
  LibraryOptions: LibraryOptions; Scan: boolean; AcknowledgePathRemoval: boolean;
}
export interface DirectoryPage {
  Path: string; ParentPath?: string;
  Items: { Name: string; Path: string }[];
  TotalRecordCount: number; StartIndex: number; Limit: number;
}
function editable(result: { Library: EditableLibrary }, id: string): EditableLibrary {
  const library = result?.Library;
  if (!library || library.Id !== id || typeof library.Name !== 'string' || typeof library.Revision !== 'string' || !/^[1-9]\d*$/.test(library.Revision)
    || !Array.isArray(library.Paths) || !library.Paths.every((path) => typeof path === 'string')
    || !Array.isArray(library.RegisteredPaths) || !library.RegisteredPaths.every((path) => typeof path.Id === 'string' && typeof path.Path === 'string' && Number.isSafeInteger(path.ItemCount) && path.ItemCount >= 0)
    || !library.LibraryOptions || typeof library.LibraryOptions.EnableLocalMetadata !== 'boolean' || typeof library.LibraryOptions.EnableLocalImages !== 'boolean') throw new ApiError('The library response is incomplete. Reload before editing.', { code: 'invalid_response' });
  return library;
}
export const libraryManagementApi = {
  async get(id: string, options: RequestOptions = {}): Promise<EditableLibrary> {
    return editable(await authenticatedRequest<{ Library: EditableLibrary }>(`/libraries/${encodeURIComponent(id)}`, options), id);
  },
  async update(id: string, body: LibraryUpdate): Promise<LibraryResponse> {
    const result = await authenticatedRequest<LibraryResponse & { Library: EditableLibrary }>(`/libraries/${encodeURIComponent(id)}`, { method: 'PATCH', body });
    editable(result, id);
    return result;
  },
  async directories(path: string, start: number, options: RequestOptions = {}): Promise<DirectoryPage> {
    const query = new URLSearchParams({ Path: path, StartIndex: String(start), Limit: '100' });
    const result = await authenticatedRequest<DirectoryPage>(`/storage/directories?${query}`, options);
    if (!result || typeof result.Path !== 'string' || (result.ParentPath !== undefined && typeof result.ParentPath !== 'string')
      || !Array.isArray(result.Items) || result.Items.length > 100 || !result.Items.every((item) => typeof item.Name === 'string' && typeof item.Path === 'string' && item.Path.startsWith('/'))
      || result.StartIndex !== start || result.Limit !== 100 || !Number.isSafeInteger(result.TotalRecordCount) || result.TotalRecordCount < result.Items.length) throw new ApiError('The directory listing is incomplete. Reload it before choosing a directory.', { code: 'invalid_response' });
    return result;
  },
  async validate(path: string): Promise<string> {
    const result = await authenticatedRequest<{ Path: string; Available: boolean }>('/storage/directories/validate', { method: 'POST', body: { Path: path } });
    if (result.Available !== true || typeof result.Path !== 'string' || !result.Path.startsWith('/')) throw new ApiError('This directory could not be validated.', { code: 'invalid_response' });
    return result.Path;
  },
};
