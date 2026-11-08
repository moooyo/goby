import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export type PlaybackTicks = number | string;
export type PlaybackCommand = 'Stop' | 'Pause' | 'Unpause' | 'Seek' | 'DisplayMessage' | 'PlayNow' | 'PlayNext' | 'PlayLast';
export interface PlaybackControls { Stop: boolean; Pause: boolean; Unpause: boolean; Seek: boolean; Message: boolean; Play: boolean }
export interface PlaybackMediaStream { Type?: string; Codec?: string; Width?: number; Height?: number; BitRate?: number; Language?: string; DisplayTitle?: string }
export interface PlaybackItem {
  Id: string; Name: string; Type: string; SeriesName?: string; IndexNumber?: number; ParentIndexNumber?: number;
  RunTimeTicks?: PlaybackTicks; ArtworkUrl?: string; MediaStreams?: PlaybackMediaStream[]; Container?: string;
}
export interface PlaybackSession {
  Id: string; UserId?: string; UserName?: string; Client: string; DeviceId: string; DeviceName: string;
  ApplicationVersion: string; LastActivityDate: string; RemoteEndPoint?: string;
  SupportedCommands: string[]; PlayableMediaTypes: string[]; SupportsRemoteControl: boolean;
  TransportConnected: boolean; Controls: PlaybackControls; PlaybackUpdatedAt?: string; PlaySessionId?: string;
  NowPlayingItem?: PlaybackItem;
  PlayState: { IsPaused: boolean; IsMuted: boolean; CanSeek: boolean; PositionTicks?: PlaybackTicks; PlayMethod?: string; MediaSourceId?: string };
}
export interface PlaybackSessionsResponse { Items: PlaybackSession[]; ObservedAt: string; Limit: number; AtCapacity: boolean }
export interface PlaybackCommandInput {
  Command: PlaybackCommand; SeekPositionTicks?: string; ItemIds?: string[]; StartPositionTicks?: string;
  Text?: string; Header?: string; TimeoutMs?: number; PlaySessionId?: string;
}
export interface PlaybackCommandReceipt {
  SessionId: string; Command: PlaybackCommand; Status: 'queued'; DeliveryCount: number; ExecutionConfirmed: false; AcceptedAt: string;
}
export interface PlaybackMessageInput { Text: string; Header?: string; TimeoutMs?: number }
export interface PlaybackMessageReceipt {
  Results: { SessionId: string; Status: 'queued' | 'offline' | 'unsupported' | 'unavailable'; DeliveryCount: number; ExecutionConfirmed: false }[];
  AcceptedAt: string;
}

const commands: PlaybackCommand[] = ['Stop', 'Pause', 'Unpause', 'Seek', 'DisplayMessage', 'PlayNow', 'PlayNext', 'PlayLast'];
const controlNames = ['Stop', 'Pause', 'Unpause', 'Seek', 'Message', 'Play'] as const;
const record = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value);
const text = (value: unknown): value is string => typeof value === 'string';
const nonempty = (value: unknown): value is string => text(value) && value.length > 0 && value.length <= 2048;
const strings = (value: unknown): value is string[] => Array.isArray(value) && value.length <= 256 && value.every(text);
const timestamp = (value: unknown): value is string => text(value) && /^\d{4}-\d{2}-\d{2}T/.test(value) && Number.isFinite(Date.parse(value));
const count = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
const optional = (value: unknown, valid: (entry: unknown) => boolean) => value === undefined || valid(value);

export function playbackTicks(value: unknown): bigint | undefined {
  if (typeof value === 'number') return Number.isSafeInteger(value) && value >= 0 ? BigInt(value) : undefined;
  if (typeof value !== 'string' || !/^(?:0|[1-9]\d{0,18})$/.test(value)) return undefined;
  const parsed = BigInt(value);
  return parsed <= 9223372036854775807n ? parsed : undefined;
}

export function playbackArtworkURL(value: unknown): string | undefined {
  if (!text(value) || value.length > 2048 || !value.startsWith('/admin/v1/') || value.startsWith('//') || /[\u0000-\u0020\u007f]/.test(value)) return undefined;
  try {
    const url = new URL(value, window.location.origin);
    if (url.origin !== window.location.origin || url.username || url.password || url.hash) return undefined;
    if (!/^\/admin\/v1\/(?:playback|items)\//.test(url.pathname)) return undefined;
    return value;
  } catch { return undefined; }
}

function invalid(): never { throw new ApiError('The playback response is incomplete. Refresh before sending another command.', { code: 'invalid_response' }); }

function item(value: unknown): value is PlaybackItem {
  if (!record(value) || !nonempty(value.Id) || !text(value.Name) || !text(value.Type)
    || !optional(value.SeriesName, text) || !optional(value.Container, text)
    || !optional(value.IndexNumber, count) || !optional(value.ParentIndexNumber, count)
    || !optional(value.RunTimeTicks, (entry) => playbackTicks(entry) !== undefined)
    || !optional(value.ArtworkUrl, (entry) => playbackArtworkURL(entry) !== undefined)) return false;
  return value.MediaStreams === undefined || (Array.isArray(value.MediaStreams) && value.MediaStreams.length <= 256 && value.MediaStreams.every((stream) => record(stream)
    && ['Type', 'Codec', 'Language', 'DisplayTitle'].every((key) => optional(stream[key], text))
    && ['Width', 'Height', 'BitRate'].every((key) => optional(stream[key], count))));
}

function session(value: unknown): value is PlaybackSession {
  if (!(record(value) && nonempty(value.Id)
    && ['Client', 'DeviceId', 'DeviceName', 'ApplicationVersion'].every((key) => text(value[key]))
    && ['UserId', 'UserName', 'RemoteEndPoint', 'PlaySessionId'].every((key) => optional(value[key], text))
    && timestamp(value.LastActivityDate) && optional(value.PlaybackUpdatedAt, timestamp)
    && strings(value.SupportedCommands) && strings(value.PlayableMediaTypes)
    && typeof value.SupportsRemoteControl === 'boolean' && typeof value.TransportConnected === 'boolean'
    && record(value.Controls) && controlNames.every((key) => typeof (value.Controls as Record<string, unknown>)[key] === 'boolean')
    && record(value.PlayState) && ['IsPaused', 'IsMuted', 'CanSeek'].every((key) => typeof (value.PlayState as Record<string, unknown>)[key] === 'boolean')
    && optional(value.PlayState.PositionTicks, (entry) => playbackTicks(entry) !== undefined)
    && optional(value.PlayState.PlayMethod, text) && optional(value.PlayState.MediaSourceId, text)
    && optional(value.NowPlayingItem, item))) return false;
  const controls = value.Controls as unknown as PlaybackControls;
  const state = value.PlayState as unknown as PlaybackSession['PlayState'];
  if (controlNames.some((key) => controls[key]) && (!value.TransportConnected || !value.SupportsRemoteControl)) return false;
  if ((controls.Stop || controls.Pause || controls.Unpause || controls.Seek) && !value.NowPlayingItem) return false;
  if (controls.Seek && !state.CanSeek || controls.Message && !(value.SupportedCommands as string[]).includes('DisplayMessage')
    || controls.Play && !(value.PlayableMediaTypes as string[]).length) return false;
  return true;
}

export const playbackApi = {
  async sessions(options: RequestOptions = {}): Promise<PlaybackSessionsResponse> {
    const result = await authenticatedRequest<unknown>('/playback/sessions', options);
    if (!record(result) || !Array.isArray(result.Items) || result.Items.length > 256 || !result.Items.every(session)
      || new Set(result.Items.map((entry) => entry.Id)).size !== result.Items.length
      || !timestamp(result.ObservedAt) || result.Limit !== 256 || typeof result.AtCapacity !== 'boolean') invalid();
    return result as unknown as PlaybackSessionsResponse;
  },
  async command(sessionId: string, input: PlaybackCommandInput, options: RequestOptions = {}): Promise<PlaybackCommandReceipt> {
    const result = await authenticatedRequest<unknown>(`/playback/sessions/${encodeURIComponent(sessionId)}/command`, { ...options, method: 'POST', body: input });
    if (!record(result) || result.SessionId !== sessionId || result.Command !== input.Command || !commands.includes(result.Command as PlaybackCommand)
      || result.Status !== 'queued' || !count(result.DeliveryCount) || result.ExecutionConfirmed !== false || !timestamp(result.AcceptedAt)) invalid();
    return result as unknown as PlaybackCommandReceipt;
  },
  async message(input: PlaybackMessageInput, options: RequestOptions = {}): Promise<PlaybackMessageReceipt> {
    const result = await authenticatedRequest<unknown>('/playback/message', { ...options, method: 'POST', body: input });
    if (!record(result) || !timestamp(result.AcceptedAt) || !Array.isArray(result.Results) || result.Results.length > 256
      || !result.Results.every((entry) => record(entry) && nonempty(entry.SessionId) && ['queued', 'offline', 'unsupported', 'unavailable'].includes(String(entry.Status))
        && count(entry.DeliveryCount) && entry.ExecutionConfirmed === false)
      || new Set(result.Results.map((entry) => entry.SessionId)).size !== result.Results.length) invalid();
    return result as unknown as PlaybackMessageReceipt;
  },
};
