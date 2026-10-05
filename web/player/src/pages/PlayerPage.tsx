import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { CSSProperties, PointerEvent as ReactPointerEvent, ReactNode } from 'react';
import Hls from 'hls.js';
import { useApp } from '../context';
import { MediaImage } from '../components';
import { api } from '../lib/api';
import { seekThumbnailAt, seekThumbnails, seekThumbnailUrl, type SeekThumbnail } from '../lib/seekPreviews';
import { subtitleStyles } from '../lib/subtitleStyles';
import { isPlayableSubtitle, loadItemPreferences, resolveItemPreferences, setItemPreferences } from '../lib/preferences';
import type { MediaItem, PlaybackEvent, PlaybackOptions, PlaybackSession, Quality } from '../types';
import { canSeekTo, clamp, clock, CREDITS_COUNTDOWN_SECONDS, resolveCreditsIntervals, episodeLabel, introInterval, streamLabel, TICKS_PER_SECOND } from './playerUtils';
import './player.css';

type Panel = 'episodes' | 'subtitles' | 'audio' | 'quality' | 'rate' | 'settings' | null;
type ActivePlayback = {
  session: PlaybackSession;
  item: MediaItem;
  selection: PlaybackOptions;
  position: number;
  started: boolean;
  stopped: boolean;
  queue: Promise<void>;
};

function Glyph({ name, size = 24 }: { name: string; size?: number }) {
  const paths: Record<string, ReactNode> = {
    back: <path d="m14.5 6-6 6 6 6" />,
    play: <path d="M8 5.6v12.8a1 1 0 0 0 1.52.85l10.2-6.4a1 1 0 0 0 0-1.7L9.52 4.75A1 1 0 0 0 8 5.6Z" fill="currentColor" stroke="none" />,
    pause: <><rect x="6" y="5" width="4" height="14" rx="1.2" fill="currentColor" stroke="none" /><rect x="14" y="5" width="4" height="14" rx="1.2" fill="currentColor" stroke="none" /></>,
    rewind: <><path d="M4.5 12a7.5 7.5 0 1 0 2.2-5.3M4.5 4.5v3.6h3.6" /><text x="12.3" y="15.4" fill="currentColor" stroke="none" textAnchor="middle" fontSize="7.5" fontWeight="700">10</text></>,
    forward: <><path d="M19.5 12a7.5 7.5 0 1 1-2.2-5.3M19.5 4.5v3.6h-3.6" /><text x="11.7" y="15.4" fill="currentColor" stroke="none" textAnchor="middle" fontSize="7.5" fontWeight="700">10</text></>,
    next: <><path d="M6 5.8v12.4l9.5-6.2Z" fill="currentColor" stroke="none" /><path d="M18 5.5v13" /></>,
    volume: <><path d="M4 9.5v5h3.4l4.6 4V5.5l-4.6 4Z" fill="currentColor" /><path d="M15.6 9.2a4 4 0 0 1 0 5.6M18.4 6.5a7.8 7.8 0 0 1 0 11" /></>,
    mute: <><path d="M4 9.5v5h3.4l4.6 4V5.5l-4.6 4Z" fill="currentColor" /><path d="m16 9.5 5 5m0-5-5 5" /></>,
    episodes: <path d="M4 6.5h16M4 12h16M4 17.5h10" />,
    subtitles: <><rect x="3" y="5" width="18" height="14" rx="3" /><path d="M10.4 10.3a2.2 2.2 0 1 0 0 3.4M16.6 10.3a2.2 2.2 0 1 0 0 3.4" /></>,
    audio: <path d="M4 10v4M8 7v10M12 4.5v15M16 8v8M20 10.5v3" />,
    fullscreen: <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />,
    restore: <path d="M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5" />,
    close: <path d="m6 6 12 12M18 6 6 18" />,
    check: <path d="m5 12.5 4.2 4.2L19 7" />,
    skip: <path d="M5 6v12l7-6ZM12 6v12l7-6Z" fill="currentColor" stroke="none" />,
    more: <><circle cx="5" cy="12" r="1" /><circle cx="12" cy="12" r="1" /><circle cx="19" cy="12" r="1" /></>,
    restart: <path d="M4.5 12a7.5 7.5 0 1 0 2.2-5.3M4.5 4.5v3.6h3.6" />,
  };
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name] ?? paths.more}</svg>;
}

export function PlayerPage({ id, restart = false }: { id: string; restart?: boolean }) {
  const app = useApp();
  const appRef = useRef(app);
  appRef.current = app;
  const rootRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const activeRef = useRef<ActivePlayback | null>(null);
  const generationRef = useRef(0);
  const hideTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const flashTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const controlHeld = useRef(false);
  const panelRef = useRef<HTMLDivElement>(null);
  const panelTriggerRef = useRef<HTMLElement | null>(null);
  const episodeRow = useRef<HTMLDivElement>(null);
  const dragRef = useRef(false);
  const reportFailure = useRef(false);
  const nextStarted = useRef(false);
  const skipDone = useRef(false);
  const itemOwner = useRef<MediaItem | null>(null);
  const [item, setItem] = useState<MediaItem | null>(null);
  const [playback, setPlayback] = useState<PlaybackSession | null>(null);
  const [selection, setSelection] = useState<PlaybackOptions>({});
  const [seasons, setSeasons] = useState<MediaItem[]>([]);
  const [episodes, setEpisodes] = useState<MediaItem[]>([]);
  const [seasonId, setSeasonId] = useState<string>();
  const [episodesLoading, setEpisodesLoading] = useState(false);
  const [episodesError, setEpisodesError] = useState(false);
  const [episodeEdges, setEpisodeEdges] = useState({ start: true, end: false });
  const [nextEpisode, setNextEpisode] = useState<MediaItem | null>(null);
  const [position, setPosition] = useState(0);
  const [duration, setDuration] = useState(0);
  const [buffered, setBuffered] = useState<{ from: number; to: number }[]>([]);
  const [playing, setPlaying] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [visible, setVisible] = useState(true);
  const [panel, setPanel] = useState<Panel>(null);
  const [volume, setVolume] = useState(0.8);
  const [muted, setMuted] = useState(false);
  const [rate, setRate] = useState(1);
  const [full, setFull] = useState(false);
  const [hover, setHover] = useState<number | null>(null);
  const [drag, setDrag] = useState<number | null>(null);
  const [resumeAt, setResumeAt] = useState<number | null>(null);
  const [flash, setFlash] = useState<'forward' | 'rewind' | null>(null);
  const [nextCancelled, setNextCancelled] = useState(false);
  const [creditsCountdown, setCreditsCountdown] = useState({ intervalKey: '', remaining: CREDITS_COUNTDOWN_SECONDS });
  const [ended, setEnded] = useState(false);
  const [cues, setCues] = useState<string[]>([]);
  const [previewIndex, setPreviewIndex] = useState<{ playback: PlaybackSession; frames: SeekThumbnail[] } | null>(null);
  const [failedPreview, setFailedPreview] = useState('');
  const statusRef = useRef({ playing, panel, volume, muted, rate });
  statusRef.current = { playing, panel, volume, muted, rate };

  const previewRequested = hover !== null;
  useEffect(() => {
    if (!playback?.source.Id || !previewRequested || previewIndex?.playback === playback) return;
    const controller = new AbortController();
    const owner = playback;
    void seekThumbnails(id, owner.source.Id, controller.signal).then(frames => {
      if (!controller.signal.aborted) setPreviewIndex({ playback: owner, frames });
    }).catch(() => {
      // Unavailable derivatives remain a time-only preview for this session.
      if (!controller.signal.aborted) setPreviewIndex({ playback: owner, frames: [] });
    });
    return () => controller.abort();
  }, [id, playback, previewRequested, previewIndex?.playback, app.session.AccessToken, app.session.User.Id, app.session.serverUrl]);

  const reveal = useCallback(() => {
    setVisible(true);
    clearTimeout(hideTimer.current);
    hideTimer.current = setTimeout(() => {
      const status = statusRef.current;
      const keyboardFocus = rootRef.current?.contains(document.activeElement) && document.activeElement?.matches(':focus-visible');
      if (status.playing && !status.panel && !controlHeld.current && !keyboardFocus && !dragRef.current) setVisible(false);
    }, 2800);
  }, []);

  const report = useCallback((active: ActivePlayback, event: 'Started' | 'Progress' | 'Stopped', immediate = false) => {
    if (event !== 'Stopped' && active.stopped) return Promise.resolve();
    const video = videoRef.current;
    const payload: PlaybackEvent = {
      ItemId: active.item.Id, MediaSourceId: active.session.source.Id,
      PlaySessionId: active.session.info.PlaySessionId,
      PositionTicks: active.started ? Math.round(Math.max(0, active.position) * TICKS_PER_SECOND) : undefined,
      IsPaused: video?.paused ?? true, PlayMethod: active.session.method,
      AudioStreamIndex: active.selection.audioStreamIndex,
      SubtitleStreamIndex: active.selection.subtitleStreamIndex,
      CanSeek: true, IsMuted: video?.muted ?? false,
      VolumeLevel: Math.round((video?.volume ?? 0.8) * 100), PlaybackRate: video?.playbackRate ?? 1,
    };
    const send = async () => {
      try {
        await api.reportPlayback(event, payload);
        reportFailure.current = false;
      } catch {
        if (!reportFailure.current) appRef.current.notify('观看进度暂未同步，网络恢复后将再次保存。');
        reportFailure.current = true;
      }
    };
    if (immediate) return send();
    active.queue = active.queue.then(send, send);
    return active.queue;
  }, []);

  const stop = useCallback(async (active: ActivePlayback | null, immediate = false) => {
    if (!active || active.stopped) return;
    active.stopped = true;
    await report(active, 'Stopped', immediate);
    try { await api.stopActiveEncoding(active.session.info.PlaySessionId); } catch { /* The backend also expires abandoned encodings. */ }
  }, [report]);

  const begin = useCallback(async (target: MediaItem, at: number, options: PlaybackOptions) => {
    const generation = ++generationRef.current;
    const previous = activeRef.current;
    activeRef.current = null;
    videoRef.current?.pause();
    hlsRef.current?.destroy();
    hlsRef.current = null;
    setPlayback(null);
    setLoading(true);
    setError('');
    setPlaying(false);
    setEnded(false);
    setCues([]);
    setBuffered([]);
    setPosition(at);
    setHover(null);
    setDrag(null);
    setFailedPreview('');
    await stop(previous);
    if (generation !== generationRef.current) return;
    try {
      const session = await api.negotiatePlayback(target.Id, { ...options, startTimeTicks: Math.round(at * TICKS_PER_SECOND) });
      if (generation !== generationRef.current) {
        void api.stopActiveEncoding(session.info.PlaySessionId).catch(() => undefined);
        return;
      }
      const resolved = {
        ...options,
        audioStreamIndex: options.audioStreamIndex ?? session.source.DefaultAudioStreamIndex,
        subtitleStreamIndex: options.subtitleStreamIndex ?? session.source.DefaultSubtitleStreamIndex ?? -1,
      };
      activeRef.current = { session, item: target, selection: resolved, position: at, started: false, stopped: false, queue: Promise.resolve() };
      setSelection(resolved);
      setDuration((session.source.RunTimeTicks ?? target.RunTimeTicks ?? 0) / TICKS_PER_SECOND);
      setPlayback(session);
      reveal();
    } catch {
      if (generation !== generationRef.current) return;
      setError('无法开始播放，请检查服务器连接，或尝试其他画质。');
      setLoading(false);
      setVisible(true);
    }
  }, [reveal, stop]);

  useEffect(() => {
    let disposed = false;
    itemOwner.current = null;
    nextStarted.current = false;
    skipDone.current = false;
    setItem(null);
    setNextEpisode(null);
    setNextCancelled(false);
    setCreditsCountdown({ intervalKey: '', remaining: CREDITS_COUNTDOWN_SECONDS });
    setPanel(null);
    setSeasons([]);
    setEpisodes([]);
    setSeasonId(undefined);
    setResumeAt(null);
    setLoading(true);
    setError('');
    api.item(id).then(async (target) => {
      if (disposed) return;
      itemOwner.current = target;
      setItem(target);
      appRef.current.setBackdrop(target, 'stage', target.SeriesId ? null : target);
      await loadItemPreferences(target.SeriesId || target.Id).catch(() => undefined);
      if (disposed) return;
      const preferences = resolveItemPreferences(target);
      const options: PlaybackOptions = { quality: preferences.quality ?? appRef.current.prefs.quality, audioStreamIndex: preferences.audioStreamIndex, subtitleStreamIndex: preferences.subtitleStreamIndex };
      setSelection(options);
      const at = restart ? 0 : (target.UserData?.PlaybackPositionTicks ?? 0) / TICKS_PER_SECOND;
      if (at > 0) setResumeAt(at);
      void begin(target, at, options);
      if (target.SeriesId) {
        try {
          const [seasonResult, episodeResult] = await Promise.all([api.seasons(target.SeriesId), api.episodes(target.SeriesId)]);
          if (disposed) return;
          setSeasons(seasonResult.Items);
          setSeasonId(target.SeasonId || seasonResult.Items[0]?.Id);
          const ordered = [...episodeResult.Items].filter((entry) => !entry.IsMissing).sort((a, b) => (a.ParentIndexNumber ?? 0) - (b.ParentIndexNumber ?? 0) || (a.IndexNumber ?? 0) - (b.IndexNumber ?? 0));
          const current = ordered.findIndex((entry) => entry.Id === target.Id);
          setNextEpisode(current >= 0 ? ordered[current + 1] ?? null : null);
        } catch { if (!disposed) setEpisodesError(true); }
      }
    }).catch(() => {
      if (!disposed) { setError('无法载入影片，请检查服务器连接后重试。'); setLoading(false); }
    });
    return () => {
      disposed = true;
      generationRef.current += 1;
      const active = activeRef.current;
      activeRef.current = null;
      void stop(active);
      hlsRef.current?.destroy();
      hlsRef.current = null;
      videoRef.current?.pause();
      clearTimeout(hideTimer.current);
      clearTimeout(flashTimer.current);
    };
  }, [id, restart, begin, stop, app.session.AccessToken, app.session.User.Id, app.session.serverUrl]);

  useEffect(() => {
    if (!item?.SeriesId || itemOwner.current !== item) return;
    if (item.SeriesPrimaryImageTag) {
      const series: MediaItem = { Id: item.SeriesId, Name: item.SeriesName || item.Name, Type: 'Series', ImageTags: { Primary: item.SeriesPrimaryImageTag } };
      appRef.current.setBackdrop(item, 'stage', series);
      return;
    }
    const controller = new AbortController();
    void api.item(item.SeriesId, controller.signal).then(series => {
      if (!controller.signal.aborted && series.Id === item.SeriesId && series.Type === 'Series' && series.ImageTags?.Primary) appRef.current.setBackdrop(item, 'stage', series);
    }).catch(() => { /* Missing series artwork keeps the neutral playback accent. */ });
    return () => controller.abort();
  }, [item, app.session.AccessToken, app.session.User.Id, app.session.serverUrl]);

  useEffect(() => {
    if (!item?.SeriesId || panel !== 'episodes') return;
    let disposed = false;
    setEpisodesLoading(true);
    setEpisodesError(false);
    api.episodes(item.SeriesId, seasonId).then((result) => {
      if (disposed) return;
      setEpisodes(result.Items);
      setEpisodesLoading(false);
    }).catch(() => {
      if (!disposed) { setEpisodesError(true); setEpisodesLoading(false); }
    });
    return () => { disposed = true; };
  }, [item?.SeriesId, seasonId, panel]);

  useEffect(() => {
    if (panel !== 'episodes' || episodesLoading) return;
    const row = episodeRow.current;
    if (!row) return;
    const current = row?.querySelector<HTMLElement>('[data-current="true"]');
    const updateEdges = () => setEpisodeEdges({ start: row.scrollLeft <= 1, end: row.scrollLeft + row.clientWidth >= row.scrollWidth - 1 });
    if (current) row.scrollTo({ left: Math.max(0, current.offsetLeft - (row.clientWidth - current.offsetWidth) / 2), behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
    updateEdges();
    row.addEventListener('scroll', updateEdges, { passive: true });
    const observer = new ResizeObserver(updateEdges);
    observer.observe(row);
    return () => { row.removeEventListener('scroll', updateEdges); observer.disconnect(); };
  }, [panel, episodes, episodesLoading]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !playback) return;
    const active = activeRef.current;
    if (!active) return;
    let disposed = false;
    let mediaRecovery = 0;
    const wanted = playback.startTimeTicks / TICKS_PER_SECOND;
    video.volume = statusRef.current.volume;
    video.muted = statusRef.current.muted;
    video.playbackRate = statusRef.current.rate;
    const play = () => {
      if (disposed) return;
      void video.play().catch(() => {
        if (!disposed) { setLoading(false); setPlaying(false); setVisible(true); }
      });
    };
    const isHls = /\.m3u8(?:\?|$)/i.test(playback.url)
      || playback.method === 'Transcode' && playback.source.TranscodingSubProtocol === 'hls';
    const metadata = () => {
      if (disposed) return;
      if ((!isHls || !playback.source.RunTimeTicks) && Number.isFinite(video.duration) && video.duration > 0) setDuration(video.duration);
      if (wanted > 0 && Math.abs(video.currentTime - wanted) > 0.5) video.currentTime = wanted;
      play();
    };
    video.addEventListener('loadedmetadata', metadata, { once: true });
    if (isHls && Hls.isSupported()) {
      const hls = new Hls({ startPosition: wanted, backBufferLength: 45, renderTextTracksNatively: true });
      hlsRef.current = hls;
      hls.subtitleDisplay = false;
      hls.on(Hls.Events.SUBTITLE_TRACKS_UPDATED, () => {
        if ((active.selection.subtitleStreamIndex ?? -1) < 0) { hls.subtitleTrack = -1; return; }
        const selected = active.session.source.MediaStreams?.find((stream) => stream.Type === 'Subtitle' && stream.Index === active.selection.subtitleStreamIndex);
        const preferred = hls.subtitleTracks.findIndex((track) => track.default);
        const matching = hls.subtitleTracks.findIndex((track) => track.lang === selected?.Language || track.name === selected?.DisplayTitle);
        hls.subtitleTrack = preferred >= 0 ? preferred : matching >= 0 ? matching : 0;
        hls.subtitleDisplay = false;
      });
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (!data.fatal || disposed) return;
        if (data.type === Hls.ErrorTypes.MEDIA_ERROR && mediaRecovery < 1) {
          mediaRecovery += 1;
          hls.recoverMediaError();
        } else {
          video.pause();
          setError(data.type === Hls.ErrorTypes.NETWORK_ERROR ? '播放连接已中断，请重试。' : '当前影片无法解码，请尝试其他画质。');
          setLoading(false);
          setPlaying(false);
          setVisible(true);
        }
      });
      hls.attachMedia(video);
      hls.loadSource(playback.url);
      hls.on(Hls.Events.MANIFEST_PARSED, play);
    } else if (!isHls || video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = playback.url;
      video.load();
    } else {
      setError('当前浏览器不支持此播放格式，请更换浏览器或使用外部播放器。');
      setLoading(false);
    }
    return () => {
      disposed = true;
      video.removeEventListener('loadedmetadata', metadata);
      hlsRef.current?.destroy();
      hlsRef.current = null;
      video.pause();
      video.removeAttribute('src');
      video.load();
    };
  }, [playback]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !playback) return;
    const bindings = new Map<TextTrack, () => void>();
    const update = () => {
      const values: string[] = [];
      for (const track of Array.from(video.textTracks)) {
        if (track.mode !== 'hidden') continue;
        for (const cue of Array.from(track.activeCues ?? [])) {
          const text = 'getCueAsHTML' in cue ? (cue as VTTCue).getCueAsHTML().textContent ?? '' : '';
          if (text && !values.includes(text)) values.push(text);
        }
      }
      setCues(values);
    };
    const bind = () => {
      for (const track of Array.from(video.textTracks)) {
        if (track.kind !== 'subtitles' && track.kind !== 'captions') continue;
        if (!hlsRef.current) track.mode = (selection.subtitleStreamIndex ?? -1) >= 0 ? 'hidden' : 'disabled';
        if (!bindings.has(track)) { track.addEventListener('cuechange', update); bindings.set(track, update); }
      }
      update();
    };
    video.textTracks.addEventListener('addtrack', bind);
    video.textTracks.addEventListener('change', update);
    video.addEventListener('loadeddata', bind);
    bind();
    return () => {
      video.textTracks.removeEventListener('addtrack', bind);
      video.textTracks.removeEventListener('change', update);
      video.removeEventListener('loadeddata', bind);
      for (const [track, listener] of bindings) track.removeEventListener('cuechange', listener);
    };
  }, [playback, selection.subtitleStreamIndex]);

  useEffect(() => {
    const timer = setInterval(() => {
      const active = activeRef.current;
      if (active?.started && !active.stopped && !videoRef.current?.paused) void report(active, 'Progress');
    }, 4000);
    const pageHide = () => { void stop(activeRef.current, true); };
    const visibility = () => {
      if (document.visibilityState === 'hidden') {
        const active = activeRef.current;
        if (active?.started && !active.stopped) void report(active, 'Progress', true);
      }
    };
    const fullChange = () => setFull(Boolean(document.fullscreenElement));
    window.addEventListener('pagehide', pageHide);
    document.addEventListener('visibilitychange', visibility);
    document.addEventListener('fullscreenchange', fullChange);
    return () => {
      clearInterval(timer);
      window.removeEventListener('pagehide', pageHide);
      document.removeEventListener('visibilitychange', visibility);
      document.removeEventListener('fullscreenchange', fullChange);
    };
  }, [report, stop]);

  useEffect(() => {
    if (resumeAt == null) return;
    const timer = setTimeout(() => setResumeAt(null), 8000);
    return () => clearTimeout(timer);
  }, [resumeAt]);

  const exit = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
    appRef.current.changed();
    appRef.current.navigate({ page: 'detail', id: item?.SeriesId || item?.Id || id });
  }, [id, item]);

  const toggle = useCallback(() => {
    const video = videoRef.current;
    const active = activeRef.current;
    if (!video || !active || error) return;
    if (active.stopped) { void begin(active.item, 0, active.selection); return; }
    if (video.paused) void video.play().catch(() => { setVisible(true); appRef.current.notify('浏览器未能开始播放，请重试。'); });
    else video.pause();
    reveal();
  }, [begin, reveal, error]);

  const seek = useCallback((requested: number) => {
    const video = videoRef.current;
    const active = activeRef.current;
    if (!video || !active || !duration || active.stopped) return;
    const target = clamp(requested, 0, Math.max(0, duration - 0.05));
    setPosition(target);
    setEnded(false);
    setResumeAt(null);
    if (target < position) skipDone.current = false;
    if (active.session.method === 'DirectPlay' || hlsRef.current || canSeekTo(video.seekable, target)) {
      video.currentTime = target;
    } else {
      void begin(active.item, target, active.selection);
    }
    reveal();
  }, [begin, duration, position, reveal]);

  const jump = useCallback((delta: number) => {
    seek((activeRef.current?.position ?? position) + delta);
    setFlash(delta > 0 ? 'forward' : 'rewind');
    clearTimeout(flashTimer.current);
    flashTimer.current = setTimeout(() => setFlash(null), 650);
  }, [position, seek]);

  const setPlayerVolume = useCallback((value: number) => {
    const next = clamp(value, 0, 1);
    const video = videoRef.current;
    if (video) { video.volume = next; video.muted = next === 0; }
    setVolume(next);
    setMuted(next === 0);
    reveal();
  }, [reveal]);

  const toggleMute = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    if (video.muted && video.volume === 0) { video.volume = 0.6; setVolume(0.6); }
    video.muted = !video.muted;
    setMuted(video.muted);
    reveal();
  }, [reveal]);

  const toggleFull = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => appRef.current.notify('无法退出全屏，请使用浏览器控制。'));
    else if (rootRef.current?.requestFullscreen) void rootRef.current.requestFullscreen().catch(() => appRef.current.notify('当前浏览器未允许全屏。'));
    else appRef.current.notify('当前浏览器不支持全屏。');
    reveal();
  }, [reveal]);

  const openPanel = useCallback((value: Panel) => {
    panelTriggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setPanel((current) => current === value ? null : value);
    reveal();
  }, [reveal]);

  const closePanel = useCallback(() => {
    setPanel(null);
    panelTriggerRef.current?.focus();
    reveal();
  }, [reveal]);

  useEffect(() => {
    if (!panel) return;
    const target = panelRef.current?.querySelector<HTMLElement>('[aria-selected="true"], [aria-pressed="true"]') ?? panelRef.current?.querySelector<HTMLElement>('button:not([disabled]):not([tabindex="-1"]), input:not([disabled])');
    target?.focus();
  }, [panel, episodesLoading]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      if (!panel && event.key === 'Tab' && !visible) {
        event.preventDefault();
        reveal();
        requestAnimationFrame(() => {
          const controls = Array.from(rootRef.current?.querySelectorAll<HTMLElement>('.player-interface button, .player-interface [tabindex="0"]') ?? []).filter(control => control.getBoundingClientRect().width > 0);
          (event.shiftKey ? controls.at(-1) : controls[0])?.focus();
        });
        return;
      }
      if (panel && event.key === 'Tab') {
        const controls = Array.from(panelRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]):not([tabindex="-1"]), input:not([disabled])') ?? []).filter(control => control.getBoundingClientRect().width > 0 && getComputedStyle(control).visibility !== 'hidden');
        const first = controls[0];
        const last = controls.at(-1);
        if (!controls.includes(document.activeElement as HTMLElement)) { event.preventDefault(); (event.shiftKey ? last : first)?.focus(); }
        else if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
        return;
      }
      if (event.key === 'Escape') {
        event.preventDefault();
        if (panel) closePanel();
        else if (!document.fullscreenElement) exit();
        return;
      }
      if (target.matches('input, textarea, select') || target.isContentEditable) return;
      if (panel) return;
      const key = event.key.toLowerCase();
      if ((key === ' ' || key === 'k') && !target.closest('button')) { event.preventDefault(); toggle(); }
      else if (key === 'arrowleft') { event.preventDefault(); jump(-10); }
      else if (key === 'arrowright') { event.preventDefault(); jump(10); }
      else if (key === 'arrowup') { event.preventDefault(); setPlayerVolume((videoRef.current?.volume ?? volume) + 0.05); }
      else if (key === 'arrowdown') { event.preventDefault(); setPlayerVolume((videoRef.current?.volume ?? volume) - 0.05); }
      else if (key === 'm') { event.preventDefault(); toggleMute(); }
      else if (key === 'f') { event.preventDefault(); toggleFull(); }
      else if (key === 'c') { event.preventDefault(); openPanel('subtitles'); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [closePanel, exit, jump, openPanel, panel, reveal, setPlayerVolume, toggle, toggleFull, toggleMute, visible, volume]);

  const changeSelection = (patch: PlaybackOptions) => {
    const target = activeRef.current?.item ?? item;
    if (!target) return;
    const next = { ...selection, ...patch };
    setSelection(next);
    closePanel();
    void setItemPreferences(target.SeriesId || target.Id, { quality: next.quality, audioStreamIndex: next.audioStreamIndex, subtitleStreamIndex: next.subtitleStreamIndex }).catch(() => appRef.current.notify('播放偏好已保存在本机，服务器同步失败。'));
    void begin(target, activeRef.current?.position ?? position, next);
  };

  const playNext = useCallback(() => {
    if (!nextEpisode || nextStarted.current) return;
    nextStarted.current = true;
    const active = activeRef.current;
    const video = videoRef.current;
    if (active?.started && !active.stopped && video && Number.isFinite(video.currentTime)) active.position = Math.max(0, video.currentTime);
    appRef.current.changed();
    void appRef.current.play(nextEpisode, true);
  }, [nextEpisode]);

  const credits = useMemo(() => resolveCreditsIntervals(playback?.source, item, duration), [playback, item, duration]);
  const currentCredits = credits?.find(interval => position >= interval.from && (position < interval.to || ended && interval.to === duration && position === duration));
  const creditsKey = currentCredits ? `${id}:${playback?.source.Id ?? ''}:${currentCredits.from}:${currentCredits.to}` : '';
  const inCredits = currentCredits !== undefined;
  const creditsRemaining = creditsCountdown.intervalKey === creditsKey ? creditsCountdown.remaining : CREDITS_COUNTDOWN_SECONDS;

  useEffect(() => {
    setCreditsCountdown({ intervalKey: creditsKey, remaining: CREDITS_COUNTDOWN_SECONDS });
  }, [creditsKey, id]);

  useEffect(() => {
    if (!currentCredits || !nextEpisode || nextCancelled || !app.prefs.autoNext || !playing || loading || error || ended) return;
    let previous = performance.now();
    const timer = setInterval(() => {
      const now = performance.now();
      const elapsed = (now - previous) / 1000;
      previous = now;
      const video = videoRef.current;
      // Count only time spent playing the current source, never paused or seeking time.
      if (!video || video.paused || video.seeking || video.readyState < 3) return;
      // Native time can cross a credits boundary before the next timeupdate event.
      if (video.currentTime < currentCredits.from || video.currentTime >= currentCredits.to) {
        setCreditsCountdown({ intervalKey: '', remaining: CREDITS_COUNTDOWN_SECONDS });
        setPosition(video.currentTime);
        return;
      }
      setCreditsCountdown(value => ({ intervalKey: creditsKey, remaining: Math.max(0, (value.intervalKey === creditsKey ? value.remaining : CREDITS_COUNTDOWN_SECONDS) - elapsed) }));
    }, 100);
    return () => clearInterval(timer);
  }, [creditsKey, currentCredits, nextEpisode, nextCancelled, app.prefs.autoNext, playing, loading, error, ended]);

  useEffect(() => {
    const video = videoRef.current;
    if (currentCredits && creditsRemaining <= 0 && app.prefs.autoNext && !nextCancelled && playing && !loading && !error && !ended
      && video && !video.paused && !video.seeking && video.readyState >= 3 && video.currentTime >= currentCredits.from && video.currentTime < currentCredits.to) playNext();
  }, [currentCredits, creditsRemaining, app.prefs.autoNext, nextCancelled, playing, loading, error, ended, playNext]);

  useEffect(() => {
    if (ended && app.prefs.autoNext && !nextCancelled && nextEpisode) playNext();
  }, [ended, app.prefs.autoNext, nextCancelled, nextEpisode, playNext]);

  const intro = useMemo(() => introInterval(playback?.source.Chapters ?? item?.Chapters, duration), [playback, item, duration]);
  const inIntro = Boolean(intro && position >= intro.from && position < intro.to);
  useEffect(() => {
    if (!app.prefs.autoSkip || !playing || !intro || !inIntro || skipDone.current) return;
    skipDone.current = true;
    seek(intro.to);
    appRef.current.notify('已自动跳过片头');
  }, [app.prefs.autoSkip, inIntro, intro, playing, seek]);

  const updateTime = () => {
    const video = videoRef.current;
    const active = activeRef.current;
    if (!video || !active || active.stopped) return;
    active.position = clamp(video.currentTime, 0, duration || Number.MAX_SAFE_INTEGER);
    if (!dragRef.current) setPosition(active.position);
  };

  const updateBuffer = () => {
    const video = videoRef.current;
    if (!video) return;
    const ranges = [];
    for (let index = 0; index < video.buffered.length; index += 1) ranges.push({ from: video.buffered.start(index), to: video.buffered.end(index) });
    setBuffered(ranges);
  };

  const onPlaying = () => {
    const active = activeRef.current;
    if (!active || active.stopped) return;
    setLoading(false);
    setPlaying(true);
    updateTime();
    if (!active.started) { active.started = true; void report(active, 'Started'); }
    else void report(active, 'Progress');
    reveal();
  };

  const onPause = () => {
    setPlaying(false);
    setVisible(true);
    const active = activeRef.current;
    if (active?.started && !active.stopped) { updateTime(); void report(active, 'Progress'); }
  };

  const onEnded = () => {
    const active = activeRef.current;
    if (active) { active.position = duration || videoRef.current?.currentTime || active.position; void stop(active); }
    setPosition(duration);
    setCues([]);
    setPlaying(false);
    setLoading(false);
    setEnded(true);
    setVisible(true);
    appRef.current.changed();
  };

  const hoverFraction = (event: ReactPointerEvent<HTMLElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect();
    return clamp((event.clientX - bounds.left) / bounds.width, 0, 1);
  };

  const sourceStreams = playback?.source.MediaStreams ?? item?.MediaStreams ?? item?.MediaSources?.[0]?.MediaStreams ?? [];
  const audioStreams = sourceStreams.filter((stream) => stream.Type === 'Audio');
  const subtitleStreams = sourceStreams.filter(isPlayableSubtitle);
  const videoStream = sourceStreams.find((stream) => stream.Type === 'Video');
  const height = videoStream?.Height ?? 0;
  const qualities: { value: Quality; label: string; detail: string }[] = [
    { value: 'original', label: `原画${height ? ` · ${height >= 2160 ? '4K' : `${height}p`}` : ''}`, detail: '保留片源画质' },
    ...(height > 1080 || !height ? [{ value: '1080p' as const, label: '1080p · 20 Mbps', detail: '服务器转码' }] : []),
    ...(height > 720 || !height ? [{ value: '720p' as const, label: '720p · 8 Mbps', detail: '服务器转码' }] : []),
    ...(height > 480 || !height ? [{ value: '480p' as const, label: '480p · 3 Mbps', detail: '服务器转码' }] : []),
  ];
  const displayPosition = drag == null ? position : drag * duration;
  const percentage = duration > 0 ? clamp(displayPosition / duration, 0, 1) * 100 : 0;
  const remaining = Math.max(0, duration - position);
  const showNext = nextEpisode && !nextCancelled && duration > 0 && (credits == null ? remaining < 15 : inCredits);
  const nextCountdown = credits == null ? remaining / rate : creditsRemaining;
  const nextProgress = credits == null ? clamp(1 - remaining / 15, 0, 1) : clamp(1 - creditsRemaining / CREDITS_COUNTDOWN_SECONDS, 0, 1);
  const title = item?.SeriesName || item?.Name || '正在载入';
  const subtitle = item?.Type === 'Episode' ? [episodeLabel(item), item.Name].filter(Boolean).join(' · ') : [item?.ProductionYear, item?.Genres?.join(' / ')].filter(Boolean).join(' · ');
  const panelTitle = { episodes: '选集', subtitles: '字幕', audio: '音轨', quality: '画质', rate: '播放速度', settings: '播放设置' };
  const activeSubtitle = subtitleStreams.find((stream) => stream.Index === selection.subtitleStreamIndex);
  const activeAudio = audioStreams.find((stream) => stream.Index === selection.audioStreamIndex);
  const subtitleName = (selection.subtitleStreamIndex ?? -1) < 0 ? '关闭' : activeSubtitle ? streamLabel(activeSubtitle).split(' · ')[0] : '字幕';
  const subtitleShort = ({ '关闭': '关', '简体中文': '简中', '繁体中文': '繁中', '繁體中文': '繁中', '英语': '英', '英文': '英', '中英双语': '双语' } as Record<string, string>)[subtitleName] ?? subtitleName;
  const audioShort = activeAudio ? streamLabel(activeAudio).split(' · ')[0] : '音轨';
  const previewFrame = previewIndex && previewIndex.playback === playback && hover !== null ? seekThumbnailAt(previewIndex.frames, hover * duration * TICKS_PER_SECOND) : undefined;
  const previewUrl = previewFrame && playback ? seekThumbnailUrl(id, playback.source.Id, previewFrame) : '';
  const showPreviewFrame = Boolean(previewUrl && previewUrl !== failedPreview);

  const panelOption = (label: string, selected: boolean, action: () => void, detail?: string) => <button key={label} className="player-option" role="option" aria-selected={selected} onClick={action}><span>{label}{detail && <small>{detail}</small>}</span>{selected && <Glyph name="check" size={18} />}</button>;
  const iconButton = (name: string, label: string, action: () => void, className = '') => <button type="button" className={`player-icon ${className}`} title={label} aria-label={label} onClick={action}><Glyph name={name} /></button>;
  const scrollEpisodes = (direction: number) => {
    const row = episodeRow.current;
    row?.scrollBy({ left: direction * row.clientWidth * 0.8, behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
  };

  return <div ref={rootRef} className={`player-page ${visible || panel || error ? 'is-visible' : 'is-hidden'}`} onPointerMove={reveal} onFocusCapture={reveal} style={subtitleStyles(app.prefs)}>
    <video ref={videoRef} className="player-video" crossOrigin="anonymous" playsInline preload="auto" onClick={() => { if (panel) closePanel(); else if (window.matchMedia('(hover: hover)').matches) toggle(); else setVisible((value) => !value); }} onDoubleClick={toggleFull} onTimeUpdate={updateTime} onProgress={updateBuffer} onPlaying={onPlaying} onPause={onPause} onWaiting={() => { if (activeRef.current && !error) setLoading(true); }} onSeeked={() => { updateTime(); const active = activeRef.current; if (active?.started && !active.stopped) void report(active, 'Progress'); }} onEnded={onEnded} onError={() => { if (activeRef.current && !hlsRef.current) { setError('当前影片无法播放，请尝试其他画质。'); setLoading(false); setVisible(true); } }} aria-label={item?.Name ?? '影片'}>
      {playback?.subtitleUrl && <track key={playback.subtitleUrl} kind="subtitles" src={playback.subtitleUrl} label="字幕" default />}
    </video>
    {cues.length > 0 && <div className={`player-subtitles subtitle-${app.prefs.subtitleStyle}`} aria-live="off">{cues.map((cue, index) => <span key={`${index}-${cue}`}>{cue}</span>)}</div>}
    {loading && !error && <div className="player-loading" role="status" aria-label="正在缓冲"><span /><span className="sr-only">正在缓冲</span></div>}
    {!playing && !loading && !error && !ended && <button className="player-big-play" aria-label="播放" onClick={toggle}><Glyph name="play" size={40} /></button>}
    {flash && <div className={`player-flash ${flash}`}>{flash === 'forward' ? '快进 10 秒 ›' : '‹ 后退 10 秒'}</div>}
    {error && <div className="player-error" role="alert"><h1>暂时无法播放</h1><p>{error}</p><div><button onClick={() => { if (item) void begin(item, position, selection); else window.location.reload(); }}>重试</button><button onClick={() => openPanel('quality')}>选择画质</button><button onClick={exit}>返回详情</button></div></div>}
    {ended && !showNext && !error && <div className="player-ended"><h2>播放结束</h2><button onClick={() => { if (item) void begin(item, 0, selection); }}>重新播放</button><button onClick={exit}>返回详情</button></div>}
    <div className="player-interface" aria-hidden={!visible && !panel && !error} inert={!visible && !panel && !error}>
      <div className="player-top-scrim" /><div className="player-bottom-scrim" />
      <header className="player-header">{iconButton('back', '返回（Esc）', exit, 'player-back')}<div><h1>{title}</h1><p>{subtitle}</p></div>{iconButton('more', '更多播放设置', () => openPanel('settings'), 'player-mobile-settings')}</header>
      <div className="player-controls" onPointerEnter={() => { controlHeld.current = true; }} onPointerLeave={() => { controlHeld.current = false; reveal(); }}>
        <div className={`player-progress ${drag != null ? 'is-dragging' : ''}`} role="slider" tabIndex={0} aria-label="播放进度" aria-valuemin={0} aria-valuemax={Math.round(duration)} aria-valuenow={Math.round(displayPosition)} aria-valuetext={`${clock(displayPosition)}，共 ${clock(duration)}`} onKeyDown={(event) => { if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) { event.preventDefault(); event.stopPropagation(); seek(event.key === 'Home' ? 0 : event.key === 'End' ? duration : position + (event.key === 'ArrowRight' ? 10 : -10)); } }} onPointerDown={(event) => { if (!duration) return; dragRef.current = true; event.currentTarget.setPointerCapture(event.pointerId); const fraction = hoverFraction(event); setDrag(fraction); setHover(fraction);  }} onPointerMove={(event) => { const fraction = hoverFraction(event); setHover(fraction); if (dragRef.current) setDrag(fraction);  }} onPointerUp={(event) => { if (!dragRef.current) return; dragRef.current = false; seek(hoverFraction(event) * duration); setDrag(null); event.currentTarget.releasePointerCapture(event.pointerId); }} onPointerCancel={() => { dragRef.current = false; setDrag(null); setHover(null); }} onPointerLeave={() => { if (!dragRef.current) setHover(null); }}>
          <div className="player-progress-track">{buffered.map((range, index) => <span key={index} className="player-buffered" style={{ left: `${duration ? range.from / duration * 100 : 0}%`, width: `${duration ? (range.to - range.from) / duration * 100 : 0}%` }} />)}{intro && <span className="player-intro-marker" style={{ left: `${intro.from / duration * 100}%`, width: `${(intro.to - intro.from) / duration * 100}%` }} />}<span className="player-progress-played" style={{ width: `${percentage}%` }} /><span className="player-progress-thumb" style={{ left: `${percentage}%` }} /></div>
          {hover != null && duration > 0 && <div className="player-seek-preview" style={{ left: `clamp(${showPreviewFrame ? 94 : 32}px, ${hover * 100}%, calc(100% - ${showPreviewFrame ? 94 : 32}px))` }}>{showPreviewFrame && <div className="player-seek-frame"><img key={previewUrl} src={previewUrl} alt={`${clock(previewFrame!.PositionTicks / TICKS_PER_SECOND)} 处的预览画面`} onError={() => setFailedPreview(previewUrl)} /></div>}<span>{clock(hover * duration)}</span></div>}
        </div>
        <div className="player-control-row">
          <button className="player-icon player-toggle" title="播放 / 暂停（空格）" aria-label={playing ? '暂停' : '播放'} onClick={toggle}><Glyph name={playing ? 'pause' : 'play'} size={28} /></button>
          {iconButton('rewind', '后退 10 秒（←）', () => jump(-10), 'player-desktop')}{iconButton('forward', '快进 10 秒（→）', () => jump(10), 'player-desktop')}
          {nextEpisode && iconButton('next', '下一集', playNext, 'player-next-control')}
          <div className="player-volume player-desktop">{iconButton(muted || volume === 0 ? 'mute' : 'volume', '静音 / 取消静音（M）', toggleMute)}<input type="range" aria-label="音量" min="0" max="1" step="0.01" value={muted ? 0 : volume} onChange={(event) => setPlayerVolume(Number(event.target.value))} style={{ '--range-fill': `${(muted ? 0 : volume) * 100}%` } as CSSProperties} /></div>
          <div className="player-time">{clock(displayPosition)}<span> / {clock(duration)}</span></div><div className="player-control-spacer" />
          {item?.SeriesId && <button className="player-pill" title="选集" aria-label="选集" aria-expanded={panel === 'episodes'} onClick={() => openPanel('episodes')}><Glyph name="episodes" size={22} /><span className="player-control-label">选集</span></button>}
          <button className={`player-pill ${(selection.subtitleStreamIndex ?? -1) >= 0 ? 'is-selected' : ''}`} title={`字幕（C） · ${subtitleName}`} aria-label="字幕" aria-expanded={panel === 'subtitles'} onClick={() => openPanel('subtitles')}><Glyph name="subtitles" size={22} /><span className="player-control-label">{subtitleShort}</span></button>
          <button className="player-pill player-desktop" title={`音轨 · ${activeAudio ? streamLabel(activeAudio) : audioShort}`} aria-label="音轨" aria-expanded={panel === 'audio'} onClick={() => openPanel('audio')}><Glyph name="audio" size={22} /><span className="player-control-label">{audioShort}</span></button>
          <button className="player-pill player-rate player-desktop" title="播放速度" aria-label="播放速度" aria-expanded={panel === 'rate'} onClick={() => openPanel('rate')}>{rate}×</button>
          <button className="player-quality" title="画质" aria-label="画质" aria-expanded={panel === 'quality'} onClick={() => openPanel('quality')}>{selection.quality === 'original' || !selection.quality ? height >= 2160 ? '4K' : '原画' : selection.quality}</button>
          {iconButton(full ? 'restore' : 'fullscreen', full ? '退出全屏（F）' : '全屏（F）', toggleFull)}
        </div>
      </div>
    </div>
    {resumeAt != null && !error && <div className="player-resume player-float"><span>已从 <strong>{clock(resumeAt)}</strong> 继续播放</span><button onClick={() => { seek(0); setResumeAt(null); }}><Glyph name="restart" size={15} />从头播放</button></div>}
    {inIntro && intro && !error && <button className="player-skip player-float" onClick={() => { skipDone.current = true; seek(intro.to); }}><span className="player-skip-fill" style={{ width: `${(position - intro.from) / (intro.to - intro.from) * 100}%` }} />跳过片头<Glyph name="skip" size={18} /></button>}
    {showNext && !error && <div className="player-next player-float"><div className="player-next-image"><MediaImage item={nextEpisode} kind="Thumb" />{app.prefs.autoNext && <span className="player-next-progress" aria-hidden="true" style={{ width: `${nextProgress * 100}%` }} />}</div><div className="player-next-body"><small>{app.prefs.autoNext ? `下一集 · ${Math.ceil(nextCountdown)} 秒后自动播放` : '下一集'}</small><h2>{episodeLabel(nextEpisode)} · {nextEpisode.Name}</h2><div><button onClick={playNext}>立即播放</button><button onClick={() => setNextCancelled(true)}>取消</button></div></div></div>}
    {panel && panel !== 'episodes' && <><button className="player-panel-dismiss" tabIndex={-1} aria-label="关闭播放菜单" onClick={closePanel} /><div ref={panelRef} className="player-panel" role="dialog" aria-modal="true" aria-label={panelTitle[panel]}><div className="player-panel-heading"><span>{panelTitle[panel]}</span>{iconButton('close', '关闭', closePanel)}</div><div className="player-panel-options" role={panel === 'settings' ? undefined : 'listbox'} aria-label={panelTitle[panel]}>
      {panel === 'subtitles' && <>{panelOption('关闭', (selection.subtitleStreamIndex ?? -1) < 0, () => changeSelection({ subtitleStreamIndex: -1 }))}{subtitleStreams.map((stream) => panelOption(streamLabel(stream), stream.Index === selection.subtitleStreamIndex, () => changeSelection({ subtitleStreamIndex: stream.Index }), stream.IsTextSubtitleStream === false ? '图形字幕 · 样式跟随片源' : undefined))}{subtitleStreams.length === 0 && <p className="player-panel-note">此片源没有可用字幕。</p>}{activeSubtitle?.IsTextSubtitleStream !== false && subtitleStreams.length > 0 && <p className="player-panel-note">字幕样式跟随个人设置。</p>}</>}
      {panel === 'audio' && <>{audioStreams.map((stream) => panelOption(streamLabel(stream), stream.Index === selection.audioStreamIndex, () => changeSelection({ audioStreamIndex: stream.Index })))}{audioStreams.length === 0 && <p className="player-panel-note">此片源没有可用音轨。</p>}</>}
      {panel === 'quality' && qualities.map((quality) => panelOption(quality.label, quality.value === (selection.quality ?? 'original'), () => changeSelection({ quality: quality.value }), quality.detail))}
      {panel === 'rate' && [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2].map((value) => panelOption(value === 1 ? '正常' : `${value}×`, value === rate, () => { setRate(value); if (videoRef.current) videoRef.current.playbackRate = value; closePanel(); }))}
      {panel === 'settings' && <>{nextEpisode && <button className="player-option" onClick={playNext}><span>下一集</span><Glyph name="next" size={20} /></button>}<button className="player-option" onClick={() => setPanel('audio')}><span>音轨</span><Glyph name="audio" size={20} /></button><button className="player-option" onClick={() => setPanel('rate')}><span>播放速度</span><span>{rate}×</span></button><div className="player-panel-volume">{iconButton(muted ? 'mute' : 'volume', '静音 / 取消静音', toggleMute)}<input type="range" aria-label="音量" min="0" max="1" step="0.01" value={muted ? 0 : volume} onChange={(event) => setPlayerVolume(Number(event.target.value))} /></div></>}
    </div></div></>}
    {panel === 'episodes' && <div ref={panelRef} className="player-episodes" role="dialog" aria-modal="true" aria-label="选集"><button className="player-episodes-scrim" tabIndex={-1} aria-label="关闭选集" onClick={closePanel} /><div className="player-episodes-content"><div className="player-episodes-heading"><div><h2>选集</h2>{seasons.length > 1 && <div className="player-seasons">{seasons.map((season) => <button key={season.Id} className={seasonId === season.Id ? 'is-active' : ''} aria-pressed={seasonId === season.Id} onClick={() => setSeasonId(season.Id)}>{season.IndexNumber != null ? `第 ${season.IndexNumber} 季` : season.Name}</button>)}</div>}<span>{episodesLoading ? '正在载入' : `${episodes.length} 集`}</span></div>{iconButton('close', '关闭选集（Esc）', closePanel)}</div>{episodesError ? <p className="player-episodes-empty" role="status">暂时无法载入剧集，请关闭后重试。</p> : episodesLoading ? <p className="player-episodes-empty" role="status">正在载入剧集…</p> : episodes.length === 0 ? <p className="player-episodes-empty">这一季还没有可播放的剧集。</p> : <div className="player-episodes-strip"><div ref={episodeRow} className="player-episodes-row">{episodes.map((episode) => <button key={episode.Id} data-current={episode.Id === id} className={`player-episode ${episode.Id === id ? 'is-current' : ''}`} aria-label={`${episodeLabel(episode)} ${episode.Name}${episode.Id === id ? '，正在播放' : ''}`} disabled={episode.IsMissing || episode.CanPlay === false} onClick={() => { if (episode.Id === id) closePanel(); else { appRef.current.changed(); void appRef.current.play(episode); } }}><div className="player-episode-image"><MediaImage item={episode} kind="Thumb" /><div className="player-episode-gradient" /><span className="player-episode-number">{String(episode.IndexNumber ?? '').padStart(2, '0')}</span>{episode.Id === id && <span className="player-episode-current"><Glyph name="audio" size={13} />正在播放</span>}{episode.UserData?.Played && <span className="player-episode-watched"><Glyph name="check" size={14} /></span>}{(episode.UserData?.PlaybackPositionTicks ?? 0) > 0 && <span className="player-episode-progress" style={{ width: `${clamp((episode.UserData?.PlaybackPositionTicks ?? 0) / (episode.RunTimeTicks || 1) * 100, 0, 100)}%` }} />}</div><div className="player-episode-title"><strong>{episode.Name}</strong><span>{episode.RunTimeTicks ? `${Math.round(episode.RunTimeTicks / TICKS_PER_SECOND / 60)} 分钟` : episode.IsMissing ? '尚未入库' : ''}</span></div><p>{episode.Overview}</p></button>)}</div><button className="player-episodes-arrow is-previous" aria-label="向前浏览剧集" disabled={episodeEdges.start} onClick={() => scrollEpisodes(-1)}><Glyph name="back" size={20} /></button><button className="player-episodes-arrow is-next" aria-label="向后浏览剧集" disabled={episodeEdges.end} onClick={() => scrollEpisodes(1)}><Glyph name="back" size={20} /></button></div>}</div></div>}
  </div>;
}

