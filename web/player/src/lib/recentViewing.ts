import { ApiError, api } from './api';
import type { AuthSession, MediaItem } from '../types';

const pageSize = 48;
const pageBudget = 12;
const displayLimit = 6;

export async function recentViewing(session: AuthSession, signal?: AbortSignal): Promise<MediaItem[]> {
  const recent: MediaItem[] = [];
  const seenWorks = new Set<string>();
  const seenItems = new Set<string>();
  let offset = 0;
  const assertActive = () => {
    signal?.throwIfAborted();
    const current = api.getSession();
    if (!current || current.AccessToken !== session.AccessToken || current.User.Id !== session.User.Id || current.serverUrl !== session.serverUrl) {
      throw new ApiError(401, 'session_changed', 'The active account changed before its viewing history was loaded.');
    }
  };
  // A long run of episodes must not consume all six work slots. The request
  // budget bounds reading for a large history without inventing missing works.
  for (let page = 0; page < pageBudget; page += 1) {
    assertActive();
    const result = await api.items({ IncludeItemTypes: 'Movie,Episode', SortBy: 'DatePlayed,SortName', SortOrder: 'Descending', Fields: 'PrimaryImageAspectRatio', StartIndex: offset, Limit: pageSize }, signal);
    assertActive();
    if (!Array.isArray(result?.Items) || !Number.isSafeInteger(result.TotalRecordCount) || result.TotalRecordCount < 0) {
      throw new ApiError(200, 'invalid_response', 'Viewing history is invalid.');
    }
    let newItems = 0;
    for (const item of result.Items) {
      if (!item?.Id || seenItems.has(item.Id)) continue;
      seenItems.add(item.Id);
      newItems += 1;
      if (!item.UserData?.LastPlayedDate || !['Movie', 'Episode'].includes(item.Type)) continue;
      const work = item.Type === 'Episode' && item.SeriesId ? `series:${item.SeriesId}` : `item:${item.Id}`;
      if (seenWorks.has(work)) continue;
      seenWorks.add(work);
      recent.push(item);
      if (recent.length === displayLimit) return recent;
    }
    offset += result.Items.length;
    if (!newItems || offset >= result.TotalRecordCount) break;
  }
  return recent;
}

export function recentPoster(item: MediaItem): MediaItem {
  // Keep the episode as the navigation/history representative while asking
  // MediaImage to use its actual series poster when one is available.
  if (item.Type !== 'Episode' || !item.SeriesId || !item.SeriesPrimaryImageTag) return item;
  const imageTags = { ...item.ImageTags };
  delete imageTags.Primary;
  return { ...item, ImageTags: imageTags };
}
