import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export interface PlayingClientCount { count: number; atCapacity: boolean }
export interface PlaybackMessageInput { Text: string; Header?: string; TimeoutMs: number }
const dispositions = ['queued', 'unsupported', 'offline', 'unavailable'] as const;
export type PlaybackMessageCounts = Record<typeof dispositions[number], number>;

const sessionLimit = 256;
const record = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value);
const identifier = (value: unknown): value is string => typeof value === 'string' && value.length > 0 && value.length <= 256 && value.trim() === value && !/[\p{Cc}\uD800-\uDFFF]/u.test(value);
const timestamp = (value: unknown): value is string => typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T/.test(value) && Number.isFinite(Date.parse(value));
const disposition = (value: unknown): value is keyof PlaybackMessageCounts => typeof value === 'string' && (dispositions as readonly string[]).includes(value);

export const playbackApi = {
  async playingCount(options: RequestOptions = {}): Promise<PlayingClientCount> {
    const result = await authenticatedRequest<unknown>('/playback/sessions', options);
    function invalid(): never { throw new ApiError('The playback count could not be read from the server response.', { code: 'invalid_response' }); }
    if (!record(result) || !Array.isArray(result.Items) || result.Items.length > sessionLimit
      || result.TotalRecordCount !== result.Items.length || result.Limit !== sessionLimit
      || typeof result.AtCapacity !== 'boolean' || result.AtCapacity !== (result.Items.length === sessionLimit) || !timestamp(result.ObservedAt)) invalid();
    const ids = new Set<string>();
    let count = 0;
    for (const session of result.Items) {
      if (!record(session) || !identifier(session.Id) || ids.has(session.Id)) invalid();
      ids.add(session.Id);
      if (session.NowPlayingItem !== undefined) {
        if (!record(session.NowPlayingItem) || !identifier(session.NowPlayingItem.Id)) invalid();
        count += 1;
      }
    }
    // The existing endpoint returns a session snapshot. Do not retain its media,
    // user or device data when all the maintenance UI needs is an aggregate.
    return { count, atCapacity: result.AtCapacity };
  },
  async message(input: PlaybackMessageInput, options: RequestOptions = {}): Promise<PlaybackMessageCounts> {
    const result = await authenticatedRequest<unknown>('/playback/message', { ...options, method: 'POST', body: input });
    function invalid(): never { throw new ApiError('The server returned an invalid message receipt.', { code: 'invalid_response' }); }
    if (!record(result) || !timestamp(result.AcceptedAt) || !Array.isArray(result.Results) || result.Results.length > sessionLimit) invalid();
    const counts: PlaybackMessageCounts = { queued: 0, unsupported: 0, offline: 0, unavailable: 0 };
    const ids = new Set<string>();
    for (const entry of result.Results) {
      if (!record(entry) || !identifier(entry.SessionId) || ids.has(entry.SessionId) || !disposition(entry.Status)
        || entry.Command !== 'DisplayMessage' || !timestamp(entry.AcceptedAt)
        || typeof entry.DeliveryCount !== 'number' || !Number.isSafeInteger(entry.DeliveryCount)
        || (entry.Status === 'queued' ? entry.DeliveryCount < 1 : entry.DeliveryCount !== 0) || entry.ExecutionConfirmed !== false) invalid();
      ids.add(entry.SessionId);
      // DeliveryCount counts queues, not clients. Each receipt represents one client.
      counts[entry.Status] += 1;
    }
    return counts;
  },
};
