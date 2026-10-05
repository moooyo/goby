import { ApiError, api } from './api';

export interface SeekThumbnail {
  PositionTicks: number;
  ImageTag: string;
}

export async function seekThumbnails(itemId: string, mediaSourceId: string, signal?: AbortSignal): Promise<SeekThumbnail[]> {
  const result = await api.thumbnailSet(itemId, mediaSourceId, 400, signal);
  if (!Array.isArray(result?.Thumbnails) || result.Thumbnails.length > 20_000) throw new ApiError(200, 'invalid_response', 'The seek preview index is invalid.');
  let previous = -1;
  for (const frame of result.Thumbnails) {
    if (!frame || !Number.isSafeInteger(frame.PositionTicks) || frame.PositionTicks <= previous || typeof frame.ImageTag !== 'string' || !frame.ImageTag.trim() || frame.ImageTag.length > 256 || /[\u0000-\u001f\u007f]/.test(frame.ImageTag)) {
      throw new ApiError(200, 'invalid_response', 'The seek preview index is invalid.');
    }
    previous = frame.PositionTicks;
  }
  return result.Thumbnails;
}

export function seekThumbnailAt(frames: SeekThumbnail[], positionTicks: number): SeekThumbnail | undefined {
  if (!Number.isFinite(positionTicks) || positionTicks < 0) return undefined;
  let first = 0;
  let last = frames.length;
  while (first < last) {
    const middle = (first + last) >>> 1;
    if (frames[middle].PositionTicks <= positionTicks) first = middle + 1;
    else last = middle;
  }
  return frames[first - 1];
}

export function seekThumbnailUrl(itemId: string, mediaSourceId: string, frame: SeekThumbnail): string {
  const url = new URL(api.itemThumbnailUrl(itemId, frame, 400));
  url.searchParams.set('MediaSourceId', mediaSourceId);
  return url.href;
}
