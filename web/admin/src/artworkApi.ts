import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export const imageTypes = ['Primary', 'Backdrop', 'Thumb', 'Banner', 'Logo', 'Art', 'Disc', 'Box', 'BoxRear', 'Menu', 'Screenshot'] as const;
export type ImageType = typeof imageTypes[number];
export interface ArtworkTarget { kind: 'items' | 'entities' | 'users'; id: string; name: string }
export interface ArtworkImage { ImageType: ImageType; ImageIndex: number; Tag: string; Width: number; Height: number; Size: string; PreviewUrl: string; Source: string }
export interface ArtworkCollection { Revision: string; Items: ArtworkImage[] }
export function multipleImages(type: ImageType): boolean { return type === 'Backdrop' || type === 'Screenshot'; }
function path(target: ArtworkTarget): string { return `/${target.kind}/${encodeURIComponent(target.id)}/${target.kind === 'users' ? 'image' : 'images'}`; }
function imagePath(target: ArtworkTarget, type: ImageType, index: number): string { return target.kind === 'users' ? path(target) : `${path(target)}/${type}/${index}`; }
function decode(result: ArtworkCollection, target: ArtworkTarget): ArtworkCollection {
  const prefix = `/admin/v1/${target.kind}/${encodeURIComponent(target.id)}/${target.kind === 'users' ? 'image/content?' : 'images/'}`;
  if (!result || typeof result.Revision !== 'string' || !/^(0|[1-9]\d*)$/.test(result.Revision) || !Array.isArray(result.Items) || result.Items.length > 73
    || !result.Items.every((item) => imageTypes.includes(item.ImageType) && Number.isInteger(item.ImageIndex) && item.ImageIndex >= 0 && item.ImageIndex < (multipleImages(item.ImageType) ? 32 : 1)
      && typeof item.Tag === 'string' && item.Tag.length > 0 && Number.isSafeInteger(item.Width) && item.Width > 0 && Number.isSafeInteger(item.Height) && item.Height > 0
      && typeof item.Size === 'string' && /^\d+$/.test(item.Size) && typeof item.Source === 'string' && typeof item.PreviewUrl === 'string'
      && item.PreviewUrl.startsWith(prefix) && !item.PreviewUrl.includes('..') && !item.PreviewUrl.includes('\\'))
    || new Set(result.Items.map((item) => `${item.ImageType}:${item.ImageIndex}`)).size !== result.Items.length) throw new ApiError('The image response is incomplete. Reload images before making changes.', { code: 'invalid_response' });
  return result;
}
export const artworkApi = {
  async list(target: ArtworkTarget, options: RequestOptions = {}): Promise<ArtworkCollection> { return decode(await authenticatedRequest<ArtworkCollection>(path(target), options), target); },
  async upload(target: ArtworkTarget, type: ImageType, index: number, file: File, revision: string): Promise<ArtworkCollection> {
    return decode(await authenticatedRequest<ArtworkCollection>(imagePath(target, type, index), { method: 'PUT', rawBody: file, headers: { 'Content-Type': file.type, 'If-Match': JSON.stringify(revision) } }), target);
  },
  async remove(target: ArtworkTarget, image: ArtworkImage, revision: string): Promise<ArtworkCollection> {
    return decode(await authenticatedRequest<ArtworkCollection>(imagePath(target, image.ImageType, image.ImageIndex), { method: 'DELETE', headers: { 'If-Match': JSON.stringify(revision) } }), target);
  },
  async reorder(target: ArtworkTarget, type: ImageType, indexes: number[], revision: string): Promise<ArtworkCollection> {
    return decode(await authenticatedRequest<ArtworkCollection>(`${path(target)}/${type}/reorder`, { method: 'POST', body: { Revision: revision, Indexes: indexes } }), target);
  },
  async reset(target: ArtworkTarget, type: ImageType, revision: string): Promise<ArtworkCollection> {
    return decode(await authenticatedRequest<ArtworkCollection>(`${path(target)}/${type}/reset`, { method: 'POST', body: { Revision: revision } }), target);
  },
};
