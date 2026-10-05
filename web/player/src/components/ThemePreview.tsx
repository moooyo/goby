import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useApp } from '../context';
import { Icon } from '../components';
import { api } from '../lib/api';
import type { BackgroundSource, MediaItem, MediaSource } from '../types';

const MOTION_QUERY = '(min-width: 720px) and (pointer: fine) and (prefers-reduced-motion: no-preference)';
const FADE_DELAY = 620;

type PreviewSoundState = {
  available: boolean;
  muted: boolean;
  toggle: () => void;
  mute: () => void;
  register: (video: HTMLVideoElement | null, hasAudio: boolean) => void;
};

const PreviewSoundContext = createContext<PreviewSoundState | null>(null);

function usePreviewSound() {
  const value = useContext(PreviewSoundContext);
  if (!value) throw new Error('Preview sound requires its provider.');
  return value;
}

export function PreviewSoundProvider({ children }: { children: ReactNode }) {
  const current = useRef<{ video: HTMLVideoElement | null; hasAudio: boolean }>({ video: null, hasAudio: false });
  const mutedPreference = useRef(true);
  const [muted, setMuted] = useState(true);
  const [available, setAvailable] = useState(false);
  const mute = useCallback(() => {
    mutedPreference.current = true;
    setMuted(true);
    if (current.current.video) current.current.video.muted = true;
  }, []);
  const register = useCallback((video: HTMLVideoElement | null, hasAudio: boolean) => {
    if (current.current.video && current.current.video !== video) current.current.video.muted = true;
    current.current = { video, hasAudio };
    if (video) video.muted = !hasAudio || mutedPreference.current;
    setAvailable(!!video && hasAudio);
  }, []);
  const toggle = useCallback(() => {
    const { video, hasAudio } = current.current;
    if (!video || !hasAudio) return;
    const next = !video.muted;
    mutedPreference.current = next;
    setMuted(next);
    // Apply the change during the user gesture so browsers may enable audio.
    video.muted = next;
    if (video.paused) void video.play().catch(() => {
      if (current.current.video === video) mute();
    });
  }, [mute]);
  const value = useMemo(() => ({ available, muted, toggle, mute, register }), [available, muted, toggle, mute, register]);
  return <PreviewSoundContext.Provider value={value}>{children}</PreviewSoundContext.Provider>;
}

export function PreviewSoundToggle() {
  const { available, muted, toggle } = usePreviewSound();
  if (!available) return null;
  const label = muted ? '开启预览声音' : '关闭预览声音';
  return <button type="button" className="preview-sound-toggle" aria-label={label} title={label} aria-pressed={!muted} onClick={toggle}><Icon name={muted ? 'muted' : 'volume'} size={18}/></button>;
}

function compatibleSource(source: MediaSource, item: MediaItem, video: HTMLVideoElement): boolean {
  if (source.SupportsDirectPlay === false || source.RequiresOpening || source.RequiresClosing || source.IsInfiniteStream) return false;
  const streams = source.MediaStreams ?? item.MediaStreams ?? [];
  const picture = streams.find(stream => stream.Type === 'Video');
  if (!picture || (picture.BitDepth ?? 8) > 8) return false;
  const range = picture.VideoRangeType || picture.VideoRange;
  if (range && !['sdr', 'unknown'].includes(range.toLowerCase())) return false;
  const sound = streams.find(stream => stream.Type === 'Audio' && stream.IsDefault) ?? streams.find(stream => stream.Type === 'Audio');
  const container = (source.Container || item.Container || '').toLowerCase().split(',');
  const codec = picture.Codec?.toLowerCase();
  const audio = sound?.Codec?.toLowerCase();
  if (container.some(value => ['mp4', 'm4v', 'mov'].includes(value)) && ['h264', 'avc'].includes(codec || '') && (!sound || ['aac', 'mp3'].includes(audio || ''))) {
    const codecs = ['avc1.640028', ...(sound ? [audio === 'aac' ? 'mp4a.40.2' : 'mp4a.6B'] : [])];
    return Boolean(video.canPlayType(`video/mp4; codecs="${codecs.join(', ')}"`));
  }
  if (container.includes('webm') && ['vp8', 'vp9'].includes(codec || '') && (!sound || ['opus', 'vorbis'].includes(audio || ''))) {
    return Boolean(video.canPlayType(`video/webm; codecs="${[codec, ...(sound ? [audio] : [])].join(', ')}"`));
  }
  return false;
}

function previewCandidates(items: MediaItem[], video: HTMLVideoElement): Array<{ url: string; hasAudio: boolean }> {
  const candidates = items.flatMap(item => (item.MediaSources ?? []).filter(source => compatibleSource(source, item, video)).map(source => ({ item, source })));
  candidates.sort((a, b) => (a.source.Bitrate || Number.MAX_SAFE_INTEGER) - (b.source.Bitrate || Number.MAX_SAFE_INTEGER));
  // Static delivery avoids creating playback sessions, encoders, or viewing history.
  return candidates.map(({ item, source }) => ({ url: api.streamUrl(item.Id, source.Id), hasAudio: (source.MediaStreams ?? item.MediaStreams ?? []).some(stream => stream.Type === 'Audio') }));
}

function releaseSource(video: HTMLVideoElement) {
  video.pause();
  video.getAnimations().filter(animation => animation.id === 'goby-depth').forEach(animation => animation.cancel());
  video.removeAttribute('src');
  video.removeAttribute('data-preview-owner');
  video.removeAttribute('data-preview-kind');
  video.removeAttribute('data-preview-audio');
  video.load();
}

export function ThemePreview({ item, enabled, routeKey }: { item: MediaItem | null; enabled: boolean; routeKey: string }) {
  const { prefs, session } = useApp();
  const { register, mute } = usePreviewSound();
  const ref = useRef<HTMLVideoElement>(null);
  const failedSources = useRef(new Map<string, Set<string>>());
  const [allowed, setAllowed] = useState(() => !document.hidden && matchMedia(MOTION_QUERY).matches);
  const [visible, setVisible] = useState(false);
  const [pageRevision, setPageRevision] = useState(0);
  const priority = prefs.backgroundSources.join(',');

  useEffect(() => {
    const query = matchMedia(MOTION_QUERY);
    const update = () => {
      const next = !document.hidden && query.matches;
      if (!next && ref.current) { register(null, false); releaseSource(ref.current); setVisible(false); }
      setAllowed(next);
    };
    query.addEventListener('change', update);
    document.addEventListener('visibilitychange', update);
    const pageChanged = () => setPageRevision(value => value + 1);
    document.addEventListener('goby:page', pageChanged);
    update();
    return () => { query.removeEventListener('change', update); document.removeEventListener('visibilitychange', update); document.removeEventListener('goby:page', pageChanged); };
  }, [register]);

  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    const video: HTMLVideoElement = element;
    const controller = new AbortController();
    const owner = `${session.ServerId || session.serverUrl}/${session.User.Id}/${item?.Id}`;
    const failures = failedSources.current.get(owner) ?? new Set<string>();
    failedSources.current.set(owner, failures);
    let active = true;
    let loadTimeout = 0;
    let loopTimeout = 0;
    let selectedSource: string | undefined;
    let pending: Array<{ url: string; kind: BackgroundSource; hasAudio: boolean }> = [];
    const sources = priority.split(',') as BackgroundSource[];
    let advancing = false;
    setVisible(false);
    register(null, false);
    const eligible = enabled && prefs.backgroundMotion && allowed && item && session.User.Policy?.EnableMediaPlayback !== false;
    const lockedUntil = Number(document.querySelector<HTMLElement>('.stage-deck')?.dataset.lockedUntil || 0);
    const delay = Math.max(FADE_DELAY, lockedUntil - performance.now());
    const fail = (source: string | undefined) => {
      if (!active || !source || source !== selectedSource) return;
      failures.add(source);
      selectedSource = undefined;
      setVisible(false);
      window.clearTimeout(loadTimeout);
      window.clearTimeout(loopTimeout);
      register(null, false);
      releaseSource(video);
      void advance();
    };
    const play = () => {
      if (!active || document.hidden || !selectedSource) return;
      const source = selectedSource;
      video.play().catch(error => {
        if (!active || source !== selectedSource || document.hidden || error instanceof Error && error.name === 'AbortError') return;
        if (!video.muted && error instanceof Error && error.name === 'NotAllowedError') {
          mute();
          void video.play().catch(() => fail(source));
        } else fail(source);
      });
    };
    const playing = () => {
      if (!active || document.hidden || !selectedSource || !video.hasAttribute('src')) return;
      window.clearTimeout(loadTimeout);
      setVisible(true);
    };
    const ended = () => {
      if (!active || !selectedSource) return;
      setVisible(false);
      loopTimeout = window.setTimeout(() => {
        if (!active || document.hidden) return;
        video.currentTime = 0;
        play();
      }, FADE_DELAY);
    };
    const error = () => fail(selectedSource);
    async function advance() {
      if (!active || !eligible || !item || advancing) return;
      advancing = true;
      try {
        while (active) {
          const candidate = pending.shift();
          if (candidate) {
            if (failures.has(candidate.url)) continue;
            selectedSource = candidate.url;
            video.dataset.previewOwner = item.Id;
            video.dataset.previewKind = candidate.kind;
            video.dataset.previewAudio = String(candidate.hasAudio);
            register(video, candidate.hasAudio);
            video.src = candidate.url;
            video.load();
            loadTimeout = window.setTimeout(() => fail(candidate.url), 12_000);
            play();
            return;
          }
          const kind = sources.shift();
          if (!kind) return;
          try {
            let candidates: Array<{ url: string; hasAudio: boolean }>;
            if (kind === 'theme') {
              const themes = await api.themeMedia(item.Id, controller.signal);
              candidates = previewCandidates(themes.ThemeVideosResult.Items, video);
            } else if (kind === 'generated') {
              const generated = await api.backgroundPreview(item.Id, controller.signal);
              candidates = generated.Available && generated.StreamUrl && video.canPlayType('video/mp4; codecs="avc1.640028"') ? [{ url: api.mediaUrl(generated.StreamUrl), hasAudio: false }] : [];
            } else {
              candidates = previewCandidates(await api.localTrailers(item.Id, controller.signal), video);
            }
            if (!active) return;
            pending = candidates.map(candidate => ({ ...candidate, kind }));
          } catch (error) {
            if (!active || error instanceof Error && error.name === 'AbortError') return;
            // An unavailable source does not prevent the next configured source from playing.
          }
        }
      } finally {
        advancing = false;
      }
    }
    video.addEventListener('playing', playing);
    video.addEventListener('ended', ended);
    video.addEventListener('error', error);
    // Keep the outgoing frame attached throughout its fade and page transition.
    // Accessibility and permission changes stop delivery immediately.
    const stopImmediately = !allowed || !prefs.backgroundMotion || session.User.Policy?.EnableMediaPlayback === false;
    if (stopImmediately) releaseSource(video);
    const timeout = window.setTimeout(() => {
      releaseSource(video);
      void advance();
    }, stopImmediately ? 0 : delay);
    return () => {
      active = false;
      controller.abort();
      window.clearTimeout(timeout);
      window.clearTimeout(loadTimeout);
      window.clearTimeout(loopTimeout);
      video.removeEventListener('playing', playing);
      video.removeEventListener('ended', ended);
      video.removeEventListener('error', error);
    };
  }, [item?.Id, enabled, allowed, routeKey, pageRevision, priority, prefs.backgroundMotion, session.ServerId, session.serverUrl, session.User.Id, session.User.Policy?.EnableMediaPlayback, register, mute]);

  useEffect(() => {
    const video = ref.current;
    return () => { register(null, false); if (video) releaseSource(video); };
  }, [register]);

  return <video ref={ref} className={`background-preview${visible ? ' is-visible' : ''}`} muted playsInline preload="none" disablePictureInPicture disableRemotePlayback tabIndex={-1} aria-hidden="true" />;
}
