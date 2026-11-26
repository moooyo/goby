import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export interface UserActivity {
  Id: string;
  LastLoginDate: string | null;
  LastActivityDate: string | null;
  LastPlayedDate: string | null;
  PlayedItemCount: number;
}

interface UserActivityPage { Items: UserActivity[]; ObservedAt: string }
const fields = ['Id', 'LastLoginDate', 'LastActivityDate', 'LastPlayedDate', 'PlayedItemCount'];
export const maxUserActivityBatch = 200;
const timestamp = (value: unknown): value is string => typeof value === 'string'
  && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value)
  && value.slice(0, 4) !== '0000'
  && Number.isFinite(Date.parse(value));
const optionalTimestamp = (value: unknown) => value === null || timestamp(value);

function invalid(): never {
  throw new ApiError('The user activity response is incomplete. Refresh to try again.', { code: 'invalid_response' });
}

function decode(value: UserActivityPage, ids: readonly string[]): UserActivity[] {
  if (!value || typeof value !== 'object' || Array.isArray(value)
    || Object.keys(value).length !== 2 || !timestamp(value.ObservedAt)
    || !Array.isArray(value.Items) || value.Items.length > ids.length) invalid();
  const allowed = new Set(ids);
  const seen = new Set<string>();
  for (const item of value.Items) {
    if (!item || typeof item !== 'object' || Array.isArray(item)
      || Object.keys(item).length !== fields.length || Object.keys(item).some((field) => !fields.includes(field))
      || typeof item.Id !== 'string' || !allowed.has(item.Id) || seen.has(item.Id)
      || !optionalTimestamp(item.LastLoginDate) || !optionalTimestamp(item.LastActivityDate) || !optionalTimestamp(item.LastPlayedDate)
      || (item.LastLoginDate !== null && (item.LastActivityDate === null || Date.parse(item.LastActivityDate) < Date.parse(item.LastLoginDate)))
      || !Number.isSafeInteger(item.PlayedItemCount) || item.PlayedItemCount < 0) invalid();
    seen.add(item.Id);
  }
  return value.Items;
}

export const userActivityApi = {
  async getBatch(ids: readonly string[], options: RequestOptions = {}): Promise<UserActivityPage> {
    const query = new URLSearchParams({ UserIds: ids.join(',') });
    const page = await authenticatedRequest<UserActivityPage>(`/users/activity?${query}`, options);
    return { Items: decode(page, ids), ObservedAt: page.ObservedAt };
  },
};
