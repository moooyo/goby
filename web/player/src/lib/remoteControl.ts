import type { PlaybackOptions } from '../types';

export const remoteCommands = ['Stop', 'Pause', 'Unpause', 'PlayPause', 'Seek', 'DisplayMessage'] as const;
export type RemoteConnectionState = 'connecting' | 'connected' | 'reconnecting' | 'offline' | 'unavailable';
export type RemotePlaybackCommand = { kind: 'playstate'; command: 'Stop' | 'Pause' | 'Unpause' | 'PlayPause' | 'Seek'; positionTicks?: number };
export type RemoteCommand = RemotePlaybackCommand
  | { kind: 'play'; itemId: string; options: PlaybackOptions }
  | { kind: 'message'; header: string; text: string; timeoutMs?: number };
type SocketEnvelope = { command: RemoteCommand; messageId?: string } | { libraryChanged: true };

const maxRemoteMessageBytes = 64 * 1024;
const maxSocketMessageBytes = 128 * 1024;
const libraryChangeFields = ['FoldersAddedTo', 'FoldersRemovedFrom', 'ItemsAdded', 'ItemsRemoved', 'ItemsUpdated', 'CollectionFolders'] as const;
const encoder = new TextEncoder();
const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const boundedText = (value: unknown, limit: number, required = false): value is string => typeof value === 'string'
  && value.length <= limit && encoder.encode(value).byteLength <= limit && (!required || value.trim().length > 0);
const identifier = (value: unknown): value is string => boundedText(value, 512, true) && !/[\p{Cc}]/u.test(value);
const nonnegativeInteger = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;

function decodeSocketEnvelope(input: unknown, sessionId?: string): SocketEnvelope | null {
  if (typeof input !== 'string' || input.length > maxSocketMessageBytes) return null;
  const size = encoder.encode(input).byteLength;
  if (size > maxSocketMessageBytes) return null;
  let envelope: unknown;
  try { envelope = JSON.parse(input); } catch { return null; }
  if (!record(envelope) || !record(envelope.Data)) return null;
  const data = envelope.Data;
  const messageId = envelope.MessageId;
  if (messageId !== undefined && !identifier(messageId)) return null;
  if (envelope.MessageType === 'LibraryChanged') {
    // The server emits a nonempty, recipient-filtered projection. The IDs
    // only validate the envelope; the page must fetch its own authorized data.
    if (!messageId || data.IsEmpty !== false) return null;
    let count = 0;
    for (const field of libraryChangeFields) {
      const ids = data[field];
      if (!Array.isArray(ids) || (count += ids.length) > 4096
        || !ids.every(id => boundedText(id, 256, true) && id.trim() === id && !/[\p{Cc}\p{Cs}]/u.test(id))) return null;
    }
    return count ? { libraryChanged: true } : null;
  }
  // The authenticated socket determines authority. An explicit command target
  // is only an additional fence, never a way to select an account or device.
  if (size > maxRemoteMessageBytes || data.Id !== undefined && (!identifier(data.Id) || sessionId !== undefined && data.Id !== sessionId)) return null;
  if (envelope.MessageType === 'Playstate') {
    if (typeof data.Command !== 'string' || !['Stop', 'Pause', 'Unpause', 'PlayPause', 'Seek'].includes(data.Command)) return null;
    if (data.Command === 'Seek' && !nonnegativeInteger(data.SeekPositionTicks)) return null;
    return { command: { kind: 'playstate', command: data.Command as RemotePlaybackCommand['command'], ...(data.Command === 'Seek' ? { positionTicks: data.SeekPositionTicks as number } : {}) }, messageId };
  }
  if (envelope.MessageType === 'Play') {
    // Queuing and multi-item playlists are not advertised or silently reduced
    // to the first item. The sender must issue a supported single-item PlayNow.
    if (!Array.isArray(data.ItemIds) || data.ItemIds.length !== 1 || !identifier(data.ItemIds[0])
      || data.PlayCommand !== undefined && data.PlayCommand !== 'PlayNow'
      || data.StartIndex !== undefined && data.StartIndex !== 0
      || data.StartPositionTicks !== undefined && !nonnegativeInteger(data.StartPositionTicks)
      || data.MediaSourceId !== undefined && !identifier(data.MediaSourceId)) return null;
    for (const [field, minimum] of [['AudioStreamIndex', 0], ['SubtitleStreamIndex', -1]] as const) {
      const value = data[field];
      if (value !== undefined && (typeof value !== 'number' || !Number.isSafeInteger(value) || value < minimum || value > 2_147_483_647)) return null;
    }
    return { command: { kind: 'play', itemId: data.ItemIds[0], options: {
      startTimeTicks: data.StartPositionTicks as number | undefined ?? 0,
      ...(data.MediaSourceId === undefined ? {} : { mediaSourceId: data.MediaSourceId as string }),
      ...(data.AudioStreamIndex === undefined ? {} : { audioStreamIndex: data.AudioStreamIndex as number }),
      ...(data.SubtitleStreamIndex === undefined ? {} : { subtitleStreamIndex: data.SubtitleStreamIndex as number }),
    } }, messageId };
  }
  if (envelope.MessageType === 'GeneralCommand' && data.Name === 'DisplayMessage' && record(data.Arguments)) {
    const arguments_ = data.Arguments;
    if (!boundedText(arguments_.Text, 2048, true) || arguments_.Header !== undefined && !boundedText(arguments_.Header, 2048)) return null;
    const timeout = arguments_.TimeoutMs;
    if (timeout !== undefined && !(typeof timeout === 'string' && /^\d{1,10}$/.test(timeout) || nonnegativeInteger(timeout))) return null;
    const milliseconds = timeout === undefined ? undefined : Number(timeout);
    if (milliseconds !== undefined && (!Number.isSafeInteger(milliseconds) || milliseconds > 2_147_483_647)) return null;
    return { command: { kind: 'message', header: (arguments_.Header as string | undefined)?.trim() || '管理员消息', text: arguments_.Text,
      ...(milliseconds ? { timeoutMs: milliseconds } : {}) }, messageId };
  }
  return null;
}

interface RemoteTransportOptions {
  sessionId?: string;
  current: () => boolean;
  register: (signal: AbortSignal) => Promise<void>;
  socketUrl: () => string;
  command: (command: RemoteCommand, signal: AbortSignal) => Promise<void>;
  invalidateLibrary: (signal: AbortSignal) => void;
  status: (state: RemoteConnectionState) => void;
}

// One transport belongs to one captured authentication scope. Reconnection
// repeats capability registration, so a revoked token is checked over HTTP
// instead of retrying an unauthenticated WebSocket forever.
export function connectRemoteControl(options: RemoteTransportOptions): () => void {
  let disposed = false;
  let suspended = false;
  let opening = false;
  let connectedBefore = false;
  let generation = 0;
  let attempt = 0;
  let pending = 0;
  let socket: WebSocket | null = null;
  let registration: AbortController | null = null;
  let delivery = new AbortController();
  let retry: ReturnType<typeof setTimeout> | undefined;
  let handshake: ReturnType<typeof setTimeout> | undefined;
  let chain = Promise.resolve();
  const delivered = new Set<string>();
  const current = () => !disposed && !suspended && options.current();
  const clearTimers = () => { clearTimeout(retry); clearTimeout(handshake); retry = undefined; handshake = undefined; };
  const closeSocket = () => {
    clearTimeout(handshake); handshake = undefined;
    delivery.abort(); delivery = new AbortController();
    const previous = socket; socket = null;
    if (previous) { previous.onopen = null; previous.onclose = null; previous.onerror = null; previous.onmessage = null; previous.close(1000, 'Player disconnected'); }
  };
  const schedule = () => {
    if (!current() || retry !== undefined) return;
    if (navigator.onLine === false) { options.status('offline'); return; }
    options.status('reconnecting');
    const delay = Math.min(30_000, 1000 * 2 ** Math.min(attempt++, 5));
    retry = setTimeout(() => { retry = undefined; void open(); }, delay);
  };
  async function open() {
    if (!current() || opening || socket) return;
    if (typeof WebSocket === 'undefined') { options.status('unavailable'); return; }
    if (navigator.onLine === false) { options.status('offline'); return; }
    opening = true;
    const owner = ++generation;
    const controller = new AbortController(); registration = controller;
    options.status(attempt ? 'reconnecting' : 'connecting');
    let deadline: ReturnType<typeof setTimeout> | undefined;
    try {
      deadline = setTimeout(() => controller.abort(), 15_000);
      await options.register(controller.signal);
      if (!current() || controller.signal.aborted || owner !== generation) return;
      const next = new WebSocket(options.socketUrl());
      socket = next;
      const ownerDelivery = delivery.signal;
      handshake = setTimeout(() => {
        if (socket !== next) return;
        closeSocket(); schedule();
      }, 10_000);
      next.onopen = () => {
        if (!current() || socket !== next) { next.close(); return; }
        clearTimeout(handshake); handshake = undefined;
        const reconnecting = connectedBefore || attempt > 0;
        attempt = 0;
        connectedBefore = true;
        options.status('connected');
        // There is no replay cursor; Goby can close the socket to request a
        // fresh snapshot without sending IDs that no longer have an ACL.
        if (reconnecting) options.invalidateLibrary(ownerDelivery);
      };
      next.onerror = () => {
        // Browser errors may include a URL containing the authentication token.
        // They are deliberately neither logged nor copied into visible errors.
      };
      next.onclose = () => {
        if (socket !== next) return;
        closeSocket(); schedule();
      };
      next.onmessage = (event: MessageEvent<unknown>) => {
        if (!current() || socket !== next || next.readyState !== WebSocket.OPEN || ownerDelivery.aborted) return;
        const envelope = decodeSocketEnvelope(event.data, options.sessionId);
        if (!envelope) return;
        // Refresh hints must not wait for, or be dropped by, the playback command queue.
        if ('libraryChanged' in envelope) { options.invalidateLibrary(ownerDelivery); return; }
        if (pending >= 32 || envelope.messageId && delivered.has(envelope.messageId)) return;
        if (envelope.messageId) {
          delivered.add(envelope.messageId);
          if (delivered.size > 128) delivered.delete(delivered.values().next().value!);
        }
        pending += 1;
        chain = chain.catch(() => undefined).then(async () => {
          if (current() && socket === next && !ownerDelivery.aborted) await options.command(envelope.command, ownerDelivery);
        }).catch(() => undefined).finally(() => { pending -= 1; });
      };
    } catch {
      if (current() && owner === generation) schedule();
    } finally {
      clearTimeout(deadline);
      if (registration === controller) registration = null;
      if (owner === generation) opening = false;
      // A transport that ignores abort still cannot acquire this owner's socket.
      if (controller.signal.aborted && owner === generation && current() && !socket && retry === undefined) schedule();
    }
  }
  const offline = () => {
    generation += 1; opening = false;
    registration?.abort(); registration = null;
    clearTimers(); closeSocket();
    if (current()) options.status('offline');
  };
  const online = () => { if (current()) { clearTimeout(retry); retry = undefined; void open(); } };
  const pageHide = () => { suspended = true; offline(); };
  const pageShow = () => { if (!disposed) { suspended = false; online(); } };
  window.addEventListener('offline', offline);
  window.addEventListener('online', online);
  window.addEventListener('pagehide', pageHide);
  window.addEventListener('pageshow', pageShow);
  if (typeof WebSocket === 'undefined') options.status('unavailable');
  else void open();
  return () => {
    disposed = true; generation += 1;
    registration?.abort(); registration = null;
    clearTimers(); closeSocket();
    window.removeEventListener('offline', offline);
    window.removeEventListener('online', online);
    window.removeEventListener('pagehide', pageHide);
    window.removeEventListener('pageshow', pageShow);
  };
}
