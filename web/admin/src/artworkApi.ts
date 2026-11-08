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
function validArtworkRevision(value: unknown): value is string {
  // Artwork revisions are opaque decimal tokens, not machine-sized counters.
  return typeof value === 'string' && value.length > 0 && value.length <= 78
    && !/[^0-9]/.test(value) && (value === '0' || value[0] !== '0');
}
export function decodeArtworkCollection(value: unknown, target: ArtworkTarget): ArtworkCollection {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new ApiError('The image response is incomplete. Reload images before making changes.', { code: 'invalid_response' });
  const result = value as ArtworkCollection;
  const prefix = `/admin/v1/${target.kind}/${encodeURIComponent(target.id)}/${target.kind === 'users' ? 'image/content?' : 'images/'}`;
  if (!validArtworkRevision(result.Revision) || !Array.isArray(result.Items) || result.Items.length > 73
    || !result.Items.every((item) => item && typeof item === 'object' && imageTypes.includes(item.ImageType) && Number.isInteger(item.ImageIndex) && item.ImageIndex >= 0 && item.ImageIndex < (multipleImages(item.ImageType) ? 32 : 1)
      && (target.kind !== 'users' || item.ImageType === 'Primary' && item.ImageIndex === 0)
      && typeof item.Tag === 'string' && item.Tag.length > 0 && Number.isSafeInteger(item.Width) && item.Width > 0 && Number.isSafeInteger(item.Height) && item.Height > 0
      && typeof item.Size === 'string' && /^\d+$/.test(item.Size) && typeof item.Source === 'string' && typeof item.PreviewUrl === 'string'
      && item.PreviewUrl.startsWith(prefix) && !item.PreviewUrl.includes('..') && !item.PreviewUrl.includes('\\'))
    || new Set(result.Items.map((item) => `${item.ImageType}:${item.ImageIndex}`)).size !== result.Items.length) throw new ApiError('The image response is incomplete. Reload images before making changes.', { code: 'invalid_response' });
  return result;
}
export const artworkApi = {
  async list(target: ArtworkTarget, options: RequestOptions = {}): Promise<ArtworkCollection> { return decodeArtworkCollection(await authenticatedRequest<unknown>(path(target), options), target); },
  async upload(target: ArtworkTarget, type: ImageType, index: number, file: File, revision: string): Promise<ArtworkCollection> {
    return decodeArtworkCollection(await authenticatedRequest<unknown>(imagePath(target, type, index), { method: 'PUT', rawBody: file, headers: { 'Content-Type': file.type, 'If-Match': JSON.stringify(revision) } }), target);
  },
  async remove(target: ArtworkTarget, image: ArtworkImage, revision: string): Promise<ArtworkCollection> {
    return decodeArtworkCollection(await authenticatedRequest<unknown>(imagePath(target, image.ImageType, image.ImageIndex), { method: 'DELETE', headers: { 'If-Match': JSON.stringify(revision) } }), target);
  },
  async reorder(target: ArtworkTarget, type: ImageType, indexes: number[], revision: string): Promise<ArtworkCollection> {
    return decodeArtworkCollection(await authenticatedRequest<unknown>(`${path(target)}/${type}/reorder`, { method: 'POST', body: { Revision: revision, Indexes: indexes } }), target);
  },
  async reset(target: ArtworkTarget, type: ImageType, revision: string): Promise<ArtworkCollection> {
    return decodeArtworkCollection(await authenticatedRequest<unknown>(`${path(target)}/${type}/reset`, { method: 'POST', body: { Revision: revision } }), target);
  },
};
