import { useEffect, useState } from 'react';
import { useApp } from '../context';
import { api } from '../lib/api';
import { isBitmapSubtitle, isTextSubtitle, loadSubtitleTimeline, loadSubtitleTimelines, sourceClock, subtitleIntervalPath, subtitleTimelineOwner, textSubtitlePath } from '../lib/subtitleTimelines';
import type { MediaStream } from '../types';

export function useSubtitleTimelines(itemId: string, sourceId: string | undefined, duration: number | undefined, streams: MediaStream[], active: boolean): ReadonlyMap<number, string> {
  const { session } = useApp();
  const streamKey = JSON.stringify(streams.map(stream => [stream.Index, stream.Codec, stream.IsExternal, stream.IsTextSubtitleStream, stream.DeliveryUrl]));
  const scope = JSON.stringify([session.serverUrl, session.ServerId, session.User.Id, session.AccessToken, itemId, sourceId, duration, streamKey]);
  const [state, setState] = useState<{ scope: string; paths: ReadonlyMap<number, string> }>({ scope: '', paths: new Map() });
  useEffect(() => {
    setState({ scope, paths: new Map() });
    const durationTicks = sourceClock(duration);
    if (!active || !durationTicks) return;
    const controller = new AbortController();
    let owner: string;
    try { owner = subtitleTimelineOwner(); } catch { return; }
    let retired = false;
    let running = 0;
    const pending: ((signal: AbortSignal) => Promise<void>)[] = [];
    const ownsRequest = () => {
      if (retired) return false;
      try { return owner === subtitleTimelineOwner(); } catch { return false; }
    };
    const publish = (streamIndex: number, path: string) => {
      if (!path || !ownsRequest()) return;
      setState(current => current.scope === scope ? { scope, paths: new Map(current.paths).set(streamIndex, path) } : current);
    };
    const drain = () => {
      while (ownsRequest() && running < 4 && pending.length) {
        const task = pending.shift()!;
        const request = new AbortController();
        const abort = () => request.abort();
        controller.signal.addEventListener('abort', abort, { once: true });
        const timeout = setTimeout(abort, 15_000);
        running += 1;
        void task(request.signal).catch(() => {}).finally(() => {
          clearTimeout(timeout);
          controller.signal.removeEventListener('abort', abort);
          running -= 1;
          drain();
        });
      }
    };
    const bitmap = streams.filter(isBitmapSubtitle);
    if (sourceId && bitmap.length) pending.push(async signal => {
      const descriptor = await loadSubtitleTimelines(itemId, sourceId, durationTicks, signal);
      if (!ownsRequest() || !descriptor.available) return;
      for (const entry of descriptor.streams) {
        if (!entry.intervalCount || !bitmap.some(stream => stream.Index === entry.streamIndex && stream.Codec?.toLowerCase() === entry.codec)) continue;
        pending.push(async trackSignal => publish(entry.streamIndex, subtitleIntervalPath(await loadSubtitleTimeline(descriptor, entry, trackSignal), durationTicks)));
      }
      drain();
    });
    for (const stream of streams.filter(stream => isTextSubtitle(stream) && !isBitmapSubtitle(stream))) pending.push(async signal => publish(stream.Index, textSubtitlePath(await api.subtitleText(stream, signal), durationTicks)));
    drain();
    // There is no cross-stage cache: every visit rechecks the source version.
    return () => { retired = true; pending.length = 0; controller.abort(); };
  }, [scope, active]);
  return active && state.scope === scope ? state.paths : new Map();
}

export function SubtitleTimelineLane({ streamIndex, path, selected }: { streamIndex: number; path: string; selected: boolean }) {
  return <div className={`detail-subtitle-lane ${selected ? 'is-selected' : ''}`} data-stream-index={streamIndex}>
    <svg viewBox="0 0 1000 10" preserveAspectRatio="none" role="img" aria-label="字幕时间轴"><path d={path} fill="currentColor" /></svg>
  </div>;
}
