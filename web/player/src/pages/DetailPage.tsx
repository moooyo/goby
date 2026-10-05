import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { Icon, MediaImage, PlayerLogo, PlayButton, StageDeck } from '../components';
import { useAudioWaveforms, WaveformLane } from '../components/WaveformLane';
import { SubtitleTimelineLane, useSubtitleTimelines } from '../components/SubtitleTimeline';
import { PreviewSoundToggle } from '../components/ThemePreview';
import { PosterPreviewTarget } from '../components/PosterPreview';
import { useApp } from '../context';
import { api } from '../lib/api';
import { subtitleLabel } from '../lib/format';
import { useArtworkAccent } from '../lib/artworkAccent';
import { savedPage, savePage } from '../lib/navigation';
import { availablePlayers, effectivePlayer } from '../lib/players';
import { mediaFormatMarks, videoDynamicRange, videoHUD, videoResolution } from '../lib/mediaFormat';
import { getItemPreferences, isPlayableSubtitle, loadItemPreferences, resolveItemPreferences, setItemPreferences } from '../lib/preferences';
import type { ItemPreferences, MediaItem, MediaPerson, MediaStream, Quality } from '../types';
import { clock } from './playerUtils';
import './detail.css';

const ticksPerSecond = 10_000_000;
const clamp = (value: number, min = 0, max = 1) => Math.min(max, Math.max(min, value));
const progressOf = (item: MediaItem) => clamp(item.UserData?.PlayedPercentage != null
  ? item.UserData.PlayedPercentage / 100
  : (item.UserData?.PlaybackPositionTicks ?? 0) / (item.RunTimeTicks || 1));
const runtime = (ticks?: number) => ticks ? `${Math.round(ticks / ticksPerSecond / 60)} 分钟` : '';
const movieRuntime = (ticks?: number) => {
  if (!ticks) return '';
  const minutes = Math.max(1, Math.round(ticks / ticksPerSecond / 60));
  const hours = Math.floor(minutes / 60);
  return hours ? `${hours} 小时${minutes % 60 ? ` ${String(minutes % 60).padStart(2, '0')} 分` : ''}` : `${minutes} 分钟`;
};
const timecode = (seconds: number) => {
  const value = Math.max(0, Math.floor(seconds));
  return [Math.floor(value / 3600), Math.floor(value / 60) % 60, value % 60].map(part => String(part).padStart(2, '0')).join(':');
};
const bitrate = (value?: number) => value ? `${Number((value / 1_000_000).toFixed(1))} Mbps` : '';
const subtitleCodecNames: Record<string, string> = { hdmv_pgs_subtitle: 'PGS', pgssub: 'PGS', pgs: 'PGS', dvd_subtitle: 'DVD', dvdsub: 'DVD', subrip: 'SRT', webvtt: 'WebVTT', mov_text: 'TX3G' };
const subtitleCodec = (codec = '') => subtitleCodecNames[codec.toLowerCase()] ?? codec.toUpperCase();
const streamList = (item: MediaItem) => item.MediaSources?.[0]?.MediaStreams ?? item.MediaStreams ?? [];
const resolution = videoResolution;
const languageNames: Record<string, string> = { chi: '中文', zho: '中文', zh: '中文', 'zh-cn': '简体中文', 'zh-tw': '繁體中文', eng: '英语', en: '英语', jpn: '日语', ja: '日语', kor: '韩语', ko: '韩语', fre: '法语', fra: '法语', fr: '法语', ger: '德语', deu: '德语', de: '德语', spa: '西班牙语', es: '西班牙语', und: '未标注语言' };
const language = (stream: MediaStream) => stream.DisplayLanguage || languageNames[(stream.Language ?? '').toLowerCase()] || stream.Language || '未标注语言';
const channels = (stream: MediaStream) => stream.ChannelLayout ?? (stream.Channels === 6 ? '5.1' : stream.Channels === 8 ? '7.1' : stream.Channels === 2 ? '立体声' : stream.Channels ? `${stream.Channels} 声道` : '');
const timelineAudioSpec = (stream: MediaStream) => {
  const codec = stream.Codec?.toLowerCase() === 'truehd'
    ? `TrueHD${/atmos/i.test([stream.Profile, stream.Title, stream.DisplayTitle].join(' ')) ? ' Atmos' : ''}`
    : stream.Codec?.toUpperCase();
  return [codec, channels(stream)].filter(Boolean).join(' ');
};
const audioCodecLabel = (stream: MediaStream, compact = false) => {
  const atmos = /\batmos\b|\bjoc\b/i.test([stream.Profile, stream.Title, stream.DisplayTitle].filter(Boolean).join(' '));
  const codec = stream.Codec?.toLowerCase() === 'truehd' ? 'TrueHD' : stream.Codec?.toUpperCase();
  return atmos ? compact ? 'Atmos' : [codec, 'Atmos'].filter(Boolean).join(' ') : codec;
};
const audioLabel = (stream: MediaStream) => [language(stream), audioCodecLabel(stream), channels(stream)].filter(Boolean).join(' ');
const audioSummary = (stream: MediaStream) => [language(stream), [audioCodecLabel(stream, true), channels(stream)].filter(Boolean).join(' ')].filter(Boolean).join(' · ');
const personRole = (person: MediaPerson) => person.Type === 'Director' ? '导演' : person.Type === 'Writer' ? '编剧' : person.Role ? /旁白/.test(person.Role) ? '旁白' : `饰 ${person.Role}` : person.Type === 'Producer' ? '制片人' : '演职员';

function useRailBoundaries(contentKey: string) {
  const rail = useRef<HTMLDivElement>(null);
  const [boundaries, setBoundaries] = useState({ left: false, right: false });
  useEffect(() => {
    const element = rail.current;
    if (!element) return;
    const update = () => {
      const left = element.scrollLeft > 1;
      const right = element.scrollWidth - element.clientWidth - element.scrollLeft > 1;
      setBoundaries(current => current.left === left && current.right === right ? current : { left, right });
    };
    const observer = new ResizeObserver(update);
    observer.observe(element);
    Array.from(element.children).forEach(child => observer.observe(child));
    element.addEventListener('scroll', update, { passive: true });
    update();
    return () => { observer.disconnect(); element.removeEventListener('scroll', update); };
  }, [contentKey]);
  return { rail, ...boundaries };
}

interface MenuOption {
  value: string;
  label: string;
  selectedLabel?: string;
  description?: string;
  isDefault?: boolean;
  icon?: ReactNode;
}

function DetailMenu({ label, value, options, onChange, footer }: {
  label: string;
  value: string;
  options: MenuOption[];
  onChange: (value: string) => void;
  footer?: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const [menuStyle, setMenuStyle] = useState<CSSProperties>({});
  const container = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const selected = options.find(option => option.value === value);
  const positionMenu = () => {
    const bounds = container.current?.getBoundingClientRect();
    if (!bounds) return;
    const width = Math.min(300, window.innerWidth - 32);
    const mobile = window.innerWidth < 720;
    const navigation = document.querySelector<HTMLElement>('.top-nav')?.getBoundingClientRect();
    const topChrome = mobile ? document.querySelector<HTMLElement>('.mobile-tools')?.getBoundingClientRect() : navigation;
    const topLimit = Math.max(16, (topChrome?.bottom ?? 8) + 8);
    const bottomLimit = mobile && navigation ? navigation.top - 8 : window.innerHeight - 16;
    const above = bounds.top - 8 - topLimit;
    const below = bottomLimit - bounds.bottom - 8;
    const opensAbove = above >= Math.min(300, below);
    setMenuStyle({
      left: Math.max(16, Math.min(bounds.left, window.innerWidth - 16 - width)) - bounds.left,
      maxHeight: Math.min(460, Math.max(44, opensAbove ? above : below)),
      ...(opensAbove ? { bottom: 'calc(100% + 8px)', top: 'auto' } : { top: 'calc(100% + 8px)', bottom: 'auto' }),
    });
  };
  useEffect(() => {
    if (!open) return;
    const close = (event: PointerEvent) => {
      if (!container.current?.contains(event.target as Node)) setOpen(false);
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation();
        setOpen(false);
        trigger.current?.focus();
      }
    };
    const scroll = (event: Event) => {
      if (event.target instanceof Element && event.target.contains(container.current)) setOpen(false);
    };
    document.addEventListener('pointerdown', close);
    document.addEventListener('keydown', escape, true);
    document.addEventListener('scroll', scroll, true);
    window.addEventListener('resize', positionMenu);
    return () => {
      document.removeEventListener('pointerdown', close);
      document.removeEventListener('keydown', escape, true);
      document.removeEventListener('scroll', scroll, true);
      window.removeEventListener('resize', positionMenu);
    };
  }, [open]);
  return <div className="detail-menu-anchor" data-menu="true" ref={container} onClick={event => event.stopPropagation()}>
    <button ref={trigger} className={`detail-setting ${open ? 'is-open' : ''}`} aria-expanded={open} aria-haspopup="menu" onClick={() => { positionMenu(); setOpen(!open); }} title={`选择${label}`}>
      <span className="detail-setting-label">{label}</span>
      <span className="detail-setting-value"><span>{selected?.selectedLabel ?? selected?.label ?? '自动选择'}</span><Icon name="chevronDown" size={15} /></span>
    </button>
    {open && <div className="detail-menu" style={menuStyle} role="menu" aria-label={label} onKeyDown={event => {
      if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
      event.preventDefault();
      const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'));
      const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
      buttons[(index + (event.key === 'ArrowDown' ? 1 : buttons.length - 1)) % buttons.length]?.focus();
    }}>
      <div className="detail-menu-heading">{label}</div>
      {options.map(option => <button key={option.value} role="menuitemradio" aria-checked={value === option.value} className={`detail-menu-option ${option.icon ? 'has-icon' : ''}`} onClick={() => { onChange(option.value); setOpen(false); trigger.current?.focus(); }}>
        {option.icon}
        <span className="detail-option-copy"><span className="detail-option-title">{option.label}{option.isDefault && <small>默认</small>}</span>{option.description && <span className="detail-option-description">{option.description}</span>}</span>
        {value === option.value && <Icon name="check" size={16} className="detail-accent" />}
      </button>)}
      {footer && <div className="detail-menu-footer" onClick={() => setOpen(false)}>{footer}</div>}
    </div>}
  </div>;
}

function EpisodeRail({ series, seasons, episodes, selectedSeason, focused, active, watchedPending, onSeason, onFocus, onPlay, onWatched }: {
  series: MediaItem;
  seasons: MediaItem[];
  episodes: MediaItem[];
  selectedSeason: string;
  focused: string;
  active: boolean;
  watchedPending: ReadonlySet<string>;
  onSeason: (id: string) => void;
  onFocus: (item: MediaItem) => void;
  onPlay: (item: MediaItem) => void;
  onWatched: (item: MediaItem) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const { rail, left, right } = useRailBoundaries(`${selectedSeason}:${episodes.map(episode => episode.Id).join(',')}`);
  const seasonRail = useRailBoundaries(seasons.map(season => season.Id).join(','));
  const hoverTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const collapseTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const keyboardTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const lockTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const lockUntil = useRef(0);
  const pointerInside = useRef(false);
  const currentFocus = useRef(focused);
  currentFocus.current = focused;
  const visible = episodes.filter(episode => !selectedSeason || episode.SeasonId === selectedSeason || episode.ParentId === selectedSeason);
  useEffect(() => {
    setExpanded(false);
    clearTimeout(hoverTimer.current);
    clearTimeout(collapseTimer.current);
    clearTimeout(keyboardTimer.current);
    clearTimeout(lockTimer.current);
    pointerInside.current = false;
    lockUntil.current = performance.now() + 1050;
  }, [active]);
  useEffect(() => {
    const element = rail.current?.querySelector<HTMLElement>(`[data-episode-id="${CSS.escape(currentFocus.current)}"]`);
    if (element && rail.current) rail.current.scrollTo({ left: element.offsetLeft - rail.current.clientWidth / 2 + element.clientWidth / 2, behavior: 'auto' });
  }, [active, selectedSeason]);
  useEffect(() => {
    if (!active) return;
    const keydown = (event: KeyboardEvent) => {
      if ((event.target as HTMLElement).closest('input,textarea,select,[data-menu]')) return;
      if (event.key === 'Enter' && !(event.target as HTMLElement).closest('button,a')) {
        const selected = visible.find(episode => episode.Id === focused);
        if (selected) { event.preventDefault(); onPlay(selected); }
        return;
      }
      const direction = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
      if (!direction || !visible.length) return;
      event.preventDefault();
      const index = visible.findIndex(episode => episode.Id === focused);
      const next = visible[Math.max(0, Math.min(visible.length - 1, index + direction))];
      onFocus(next);
      rail.current?.querySelector<HTMLElement>(`[data-episode-id="${CSS.escape(next.Id)}"] .detail-episode-open`)?.focus({ preventScroll: true });
      const nextCard = rail.current?.querySelector<HTMLElement>(`[data-episode-id="${CSS.escape(next.Id)}"]`);
      if (nextCard && rail.current) rail.current.scrollTo({ left: nextCard.offsetLeft - rail.current.clientWidth / 2 + nextCard.clientWidth / 2, behavior: 'smooth' });
      setExpanded(true);
      clearTimeout(keyboardTimer.current);
      keyboardTimer.current = setTimeout(() => setExpanded(false), 2600);
    };
    document.addEventListener('keydown', keydown);
    return () => document.removeEventListener('keydown', keydown);
  }, [active, visible, focused, onFocus]);
  useEffect(() => () => {
    clearTimeout(hoverTimer.current);
    clearTimeout(collapseTimer.current);
    clearTimeout(keyboardTimer.current);
    clearTimeout(lockTimer.current);
  }, []);
  return <div className={`detail-episode-strip ${expanded ? 'is-expanded' : ''}`} onClick={event => event.stopPropagation()} onPointerEnter={event => {
    if (event.pointerType === 'touch') return;
    pointerInside.current = true;
    clearTimeout(collapseTimer.current);
    clearTimeout(lockTimer.current);
    lockTimer.current = setTimeout(() => { if (pointerInside.current) setExpanded(true); }, Math.max(150, lockUntil.current - performance.now()));
  }} onPointerLeave={() => {
    pointerInside.current = false;
    clearTimeout(hoverTimer.current);
    clearTimeout(lockTimer.current);
    collapseTimer.current = setTimeout(() => setExpanded(false), 280);
  }} onFocus={event => { if (event.target.matches(':focus-visible')) setExpanded(true); }}>
    <div className="detail-season-heading">
      <div className="detail-season-rail-wrap">
        <div ref={seasonRail.rail} className="detail-seasons" role="tablist" aria-label={`${series.Name}的季`}>
          {seasons.map(season => <button key={season.Id} role="tab" aria-selected={selectedSeason === season.Id} className={`${selectedSeason === season.Id ? 'is-selected' : ''} ${seasons.length < 2 ? 'only-season' : ''}`} onClick={() => onSeason(season.Id)}>{season.Name || `第 ${season.IndexNumber} 季`}</button>)}
          <span className="detail-episode-count">{visible.length} 集</span>
        </div>
        <button className="detail-season-arrow is-left" aria-label="上一组季" aria-hidden={!seasonRail.left} disabled={!seasonRail.left} tabIndex={seasonRail.left ? 0 : -1} onClick={() => seasonRail.rail.current?.scrollBy({ left: -(seasonRail.rail.current.clientWidth * .75), behavior: 'smooth' })}><Icon name="chevronLeft" size={18} /></button>
        <button className="detail-season-arrow is-right" aria-label="下一组季" aria-hidden={!seasonRail.right} disabled={!seasonRail.right} tabIndex={seasonRail.right ? 0 : -1} onClick={() => seasonRail.rail.current?.scrollBy({ left: seasonRail.rail.current.clientWidth * .75, behavior: 'smooth' })}><Icon name="chevronRight" size={18} /></button>
      </div>
      <PreviewSoundToggle />
    </div>
    <div className="detail-episode-rail-wrap">
      <div className="detail-episode-rail" ref={rail}>
        {visible.map(episode => <div className={`detail-episode-card ${focused === episode.Id ? 'is-focused' : ''}`} key={episode.Id} data-episode-id={episode.Id} onPointerEnter={event => {
          if (event.pointerType === 'touch') return;
          clearTimeout(hoverTimer.current);
          hoverTimer.current = setTimeout(() => onFocus(episode), 160);
        }} onPointerLeave={() => clearTimeout(hoverTimer.current)}>
          <div className="detail-episode-picture">
            <button className="detail-episode-open" onClick={() => onPlay(episode)} onFocus={() => onFocus(episode)} aria-label={`播放第 ${episode.IndexNumber ?? ''} 集 ${episode.Name}`}>
              <MediaImage item={episode} kind="Primary" />
              <span className="detail-episode-shade" />
              <span className="detail-episode-number">{String(episode.IndexNumber ?? '').padStart(2, '0')}</span>
              {progressOf(episode) > 0 && !episode.UserData?.Played && <span className="detail-card-progress"><i style={{ width: `${progressOf(episode) * 100}%` }} /></span>}
            </button>
            <button className={`detail-episode-watched ${episode.UserData?.Played ? 'is-watched' : ''}`} aria-label={episode.UserData?.Played ? `将第 ${episode.IndexNumber} 集标记为未看` : `将第 ${episode.IndexNumber} 集标记为已看`} aria-pressed={!!episode.UserData?.Played} disabled={watchedPending.has(episode.Id)} title={episode.UserData?.Played ? '已看过' : '标记已看'} onClick={event => { event.stopPropagation(); onWatched(episode); }}><Icon name="check" size={15} /></button>
          </div>
          <div className="detail-episode-title">{episode.Name}</div>
          <div className="detail-episode-status"><span>{runtime(episode.RunTimeTicks)}</span>{episode.UserData?.Played ? <span>已看</span> : progressOf(episode) > 0 ? <span className="detail-accent">看到 {Math.round(progressOf(episode) * 100)}%</span> : null}</div>
        </div>)}
        {!visible.length && <p className="detail-empty-inline">这一季还没有可播放的剧集</p>}
      </div>
      <button className="detail-rail-arrow is-left" aria-label="上一组剧集" aria-hidden={!left} disabled={!left} tabIndex={left ? 0 : -1} onClick={() => rail.current?.scrollBy({ left: -(rail.current.clientWidth * .75), behavior: 'smooth' })}><Icon name="chevronLeft" size={20} /></button>
      <button className="detail-rail-arrow is-right" aria-label="下一组剧集" aria-hidden={!right} disabled={!right} tabIndex={right ? 0 : -1} onClick={() => rail.current?.scrollBy({ left: rail.current.clientWidth * .75, behavior: 'smooth' })}><Icon name="chevronRight" size={20} /></button>
    </div>
    <div className="detail-strip-fade" />
  </div>;
}

function EpisodeInformation({ episode, series, seasonNumber }: { episode: MediaItem; series: MediaItem; seasonNumber?: number }) {
  const [snapshot, setSnapshot] = useState({ episode, seasonNumber });
  const exiting = snapshot.episode.Id !== episode.Id;
  const displayed = exiting ? snapshot.episode : episode;
  const displayedSeason = exiting ? snapshot.seasonNumber : seasonNumber;
  useEffect(() => {
    if (snapshot.episode.Id === episode.Id) {
      if (snapshot.episode !== episode || snapshot.seasonNumber !== seasonNumber) setSnapshot({ episode, seasonNumber });
      return;
    }
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
      setSnapshot({ episode, seasonNumber });
      return;
    }
    // The delayed snapshot affects text only; selection and playback stay current.
    const timer = window.setTimeout(() => setSnapshot({ episode, seasonNumber }), 150);
    return () => window.clearTimeout(timer);
  }, [episode, seasonNumber, snapshot.episode.Id]);
  return <div className="detail-episode-info" data-stage-info="true"><div className={`detail-episode-transition ${exiting ? 'is-exiting' : ''}`} key={displayed.Id}>
    <div className="detail-episode-heading"><div className="detail-focus-number">{String(displayed.IndexNumber ?? '').padStart(2, '0')}</div><div><div className="detail-episode-kicker">{series.Name} · 第 {displayed.ParentIndexNumber ?? displayedSeason ?? ''} 季</div><h3>{displayed.Name}</h3><div className="detail-focus-meta">{[runtime(displayed.RunTimeTicks), displayed.UserData?.Played ? '已看过' : progressOf(displayed) > 0 ? `已看 ${Math.round(progressOf(displayed) * 100)}%` : ''].filter(Boolean).join(' · ')}</div></div></div>
    {displayed.Overview && <p>{displayed.Overview}</p>}
  </div></div>;
}

function DetailSimilarCard({ item, onOpen }: { item: MediaItem; onOpen: () => void }) {
  const accent = useArtworkAccent(item);
  return <PosterPreviewTarget item={item} className="detail-similar-target">
    <button className="detail-similar-card" style={accent} onClick={onOpen}>
      <span className="detail-similar-poster"><MediaImage item={item} />{item.UserData?.Played && <span className="detail-similar-watched"><Icon name="check" size={14} /></span>}{progressOf(item) > 0 && !item.UserData?.Played && <span className="detail-card-progress"><i style={{ width: `${progressOf(item) * 100}%` }} /></span>}</span>
      <strong>{item.Name}</strong><small>{[item.ProductionYear, item.Genres?.[0]].filter(Boolean).join(' · ')}</small>
    </button>
  </PosterPreviewTarget>;
}

function MediaInformation({ item, series, selection, active }: { item: MediaItem; series?: MediaItem; selection: ItemPreferences; active: boolean }) {
  const source = item.MediaSources?.[0];
  const waveforms = useAudioWaveforms(item.Id, source?.Id, active);
  const streams = streamList(item);
  const video = streams.find(stream => stream.Type === 'Video');
  const audio = streams.filter(stream => stream.Type === 'Audio');
  const subtitles = streams.filter(stream => stream.Type === 'Subtitle');
  const sourceDuration = source?.RunTimeTicks ?? item.RunTimeTicks;
  const duration = (sourceDuration ?? 0) / ticksPerSecond;
  const subtitlePaths = useSubtitleTimelines(item.Id, source?.Id, sourceDuration, subtitles, active);
  const subtitleRows = subtitles.map((stream, index) => ({ stream, index, path: subtitlePaths.get(stream.Index) })).filter(row => row.path);
  const position = (item.UserData?.PlaybackPositionTicks ?? 0) / ticksPerSecond;
  const videoSpec = videoHUD(video);
  const frameRate = video?.RealFrameRate ?? video?.AverageFrameRate;
  const primaryAudio = audio.find(stream => stream.Index === selection.audioStreamIndex) ?? audio.find(stream => stream.IsDefault) ?? audio[0];
  const marks = mediaFormatMarks(video, primaryAudio, source);
  const chapters = (item.Chapters ?? source?.Chapters ?? []).filter(chapter => chapter.StartPositionTicks >= 0);
  const [thumbnails, setThumbnails] = useState<{ ImageTag: string; PositionTicks: number }[]>([]);
  useEffect(() => {
    if (!active) return;
    let cancelled = false;
    setThumbnails([]);
    api.thumbnailSet(item.Id, source?.Id).then(result => {
      if (cancelled || !result.Thumbnails.length) return;
      const frames = result.Thumbnails;
      const count = Math.min(12, frames.length);
      setThumbnails(Array.from({ length: count }, (_, index) => frames[Math.round(index * (frames.length - 1) / Math.max(1, count - 1))]));
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [active, item.Id, source?.Id]);
  return <section className="stage-page detail-media-page">
    <div className="stage-scroll detail-secondary-scroll">
      <div className="detail-secondary-content">
        <div className="detail-media-heading"><h2>媒体信息</h2>{series && <span>第 {item.ParentIndexNumber ?? ''} 季第 {item.IndexNumber ?? ''} 集 · {item.Name}</span>}</div>
        <div className="detail-media-top">
          <div className="detail-viewfinder" style={{ '--media-aspect': video?.Width && video.Height ? video.Width / video.Height : 16 / 9 } as CSSProperties}>
            <MediaImage item={item} kind="Backdrop" />
            <span className="detail-viewfinder-shade" />
            <i className="viewfinder-corner top-left" /><i className="viewfinder-corner top-right" /><i className="viewfinder-corner bottom-left" /><i className="viewfinder-corner bottom-right" />
            <span className="viewfinder-cross" />
            <div className="detail-hud top"><span><i />{selection.quality && selection.quality !== 'original' ? `转码目标 · ${selection.quality}` : '原画'}</span><span>TC {timecode(position)}</span></div>
            <div className="detail-hud bottom"><span>{videoSpec || '暂无编码信息'}</span><span>{[frameRate ? `${frameRate} fps` : '', bitrate(source?.Bitrate)].filter(Boolean).join(' · ')}</span></div>
          </div>
          <div className="detail-format-marks">{marks.length ? marks.map((mark, index) => <div key={`${mark.big}-${index}`}><div className="detail-format-title">{mark.big}</div>{mark.small && <div className="detail-format-spec">{mark.small}</div>}</div>) : <p className="detail-empty-inline">暂无媒体规格</p>}</div>
        </div>
        <div className="detail-timeline">
          <div className="detail-track-labels">
            <div className="detail-track-ruler-spacer" />
            <div className="detail-track-label video"><div><code>V1</code><strong>视频</strong></div><small>{[video?.Codec?.toUpperCase(), bitrate(video?.BitRate)].filter(Boolean).join(' · ') || '剧照'}</small></div>
            {audio.map((stream, index) => <div className={`detail-track-label audio ${stream.Index === primaryAudio?.Index ? 'is-selected' : ''}`} key={stream.Index}><div><code>A{index + 1}</code><strong>{language(stream)}</strong></div><small>{timelineAudioSpec(stream)}</small></div>)}
            {subtitleRows.map(({ stream, index }) => <div className={`detail-track-label subtitle ${stream.Index === selection.subtitleStreamIndex ? 'is-selected' : ''}`} data-stream-index={stream.Index} key={stream.Index}><code>S{index + 1}</code><strong title={subtitleLabel(stream)}>{subtitleLabel(stream)}</strong><small title={stream.Codec}>{subtitleCodec(stream.Codec)}</small></div>)}
          </div>
          <div className="detail-track-lanes">
            <div className="detail-time-ruler">{[0, .25, .5, .75, 1].map(fraction => <span key={fraction} style={{ left: `${fraction * 100}%`, transform: fraction === 0 ? 'none' : fraction === 1 ? 'translateX(-100%)' : 'translateX(-50%)' }}>{timecode(duration * fraction)}</span>)}</div>
            <div className="detail-filmstrip" title={thumbnails.length ? '影片预览时间轴' : '影片剧照，暂无时间轴缩略图'}>{thumbnails.length ? thumbnails.map(frame => <img src={api.itemThumbnailUrl(item.Id, frame)} key={frame.PositionTicks} alt={timecode(frame.PositionTicks / ticksPerSecond)} loading="lazy" />) : Array.from({ length: 12 }, (_, index) => <MediaImage item={item} kind="Backdrop" key={index} />)}{chapters.filter(chapter => chapter.MarkerType === 'Chapter' && duration > 0).map((chapter, index) => <i className="detail-chapter-mark" key={index} style={{ left: `${clamp(chapter.StartPositionTicks / ticksPerSecond / duration) * 100}%` }} title={chapter.Name || `章节 ${index + 1}`} />)}</div>
            {audio.map(stream => <WaveformLane key={stream.Index} streamIndex={stream.Index} label={audioLabel(stream)} selected={stream.Index === primaryAudio?.Index} active={active} waveforms={waveforms} />)}
            {subtitleRows.map(({ stream, path }) => <SubtitleTimelineLane key={`${item.Id}-${stream.Index}`} streamIndex={stream.Index} path={path!} selected={stream.Index === selection.subtitleStreamIndex} />)}
            {position > 0 && duration > 0 && <div className="detail-playhead" style={{ left: `${clamp(position / duration) * 100}%`, '--head-fraction': clamp(position / duration) } as CSSProperties}><span>上次看到 {clock(position).padStart(5, '0')}</span></div>}
          </div>
        </div>
      </div>
    </div>
  </section>;
}

export function DetailPage({ id }: { id: string }) {
  const { prefs, play, navigate, notify, revision, changed, setBackdrop } = useApp();
  const [item, setItem] = useState<MediaItem | null>(null);
  const [episodes, setEpisodes] = useState<MediaItem[]>([]);
  const [seasons, setSeasons] = useState<MediaItem[]>([]);
  const [similar, setSimilar] = useState<MediaItem[]>([]);
  const [focusedId, setFocusedId] = useState('');
  const [resumeId, setResumeId] = useState('');
  const [seasonId, setSeasonId] = useState('');
  const [page, setPage] = useState(() => savedPage(`detail.${id}`, 4));
  const [error, setError] = useState('');
  const [selection, setSelection] = useState<ItemPreferences>(() => getItemPreferences(id));
  const [pending, setPending] = useState(false);
  const [watchedPending, setWatchedPending] = useState<ReadonlySet<string>>(new Set());
  const watchedRequests = useRef(new Set<string>());
  const selectionEdits = useRef(0);
  const similarRail = useRailBoundaries(similar.map(movie => movie.Id).join(','));
  const castRail = useRailBoundaries(`${item?.Id}:${item?.People?.map(person => `${person.Name}:${person.Type}`).join(',')}`);
  useEffect(() => savePage(`detail.${id}`, page), [id, page]);
  useEffect(() => { if (item?.Type && item.Type !== 'Series') setPage(current => Math.min(2, current)); }, [item?.Type]);

  useEffect(() => {
    let cancelled = false;
    setItem(null);
    setError('');
    setEpisodes([]);
    setSeasons([]);
    setSimilar([]);
    setFocusedId('');
    setResumeId('');
    setSeasonId('');
    setPage(savedPage(`detail.${id}`, 4));
    setSelection(getItemPreferences(id));
    selectionEdits.current = 0;
    api.item(id).then(result => {
      if (cancelled) return;
      setItem(result);
      const preferenceId = result.SeriesId || result.Id;
      setSelection(getItemPreferences(preferenceId));
      loadItemPreferences(preferenceId).then(saved => { if (!cancelled && selectionEdits.current === 0) setSelection(saved); }).catch(() => {});
    }).catch(() => { if (!cancelled) setError('无法载入影片，请检查服务器连接或访问权限后重试。'); });
    return () => { cancelled = true; };
  }, [id]);

  useEffect(() => {
    if (!revision) return;
    let cancelled = false;
    api.item(id).then(result => { if (!cancelled) setItem(result); }).catch(() => {});
    return () => { cancelled = true; };
  }, [id, revision]);

  useEffect(() => {
    if (!item) return;
    let cancelled = false;
    api.similar(item.Id, 16).then(result => { if (!cancelled) setSimilar(result.Items); }).catch(() => {});
    if (item.Type === 'Series') {
      Promise.all([api.seasons(item.Id), api.episodes(item.Id), api.nextUp(item.Id).catch(() => ({ Items: [], TotalRecordCount: 0 }))]).then(([seasonResult, episodeResult, nextResult]) => {
        if (cancelled) return;
        setSeasons(seasonResult.Items);
        setEpisodes(episodeResult.Items);
        const resume = nextResult.Items[0] ?? episodeResult.Items.find(episode => (episode.UserData?.PlaybackPositionTicks ?? 0) > 0 && !episode.UserData?.Played) ?? episodeResult.Items.find(episode => !episode.UserData?.Played) ?? episodeResult.Items[0];
        if (resume) {
          setFocusedId(resume.Id);
          setResumeId(resume.Id);
          setSeasonId(resume.SeasonId ?? resume.ParentId ?? seasonResult.Items[0]?.Id ?? '');
        } else setSeasonId(seasonResult.Items[0]?.Id ?? '');
      }).catch(() => { if (!cancelled) notify('无法载入剧集，请检查服务器连接后重试。'); });
    }
    return () => { cancelled = true; };
  }, [item?.Id, item?.Type]);

  const series = item?.Type === 'Series';
  const focused = episodes.find(episode => episode.Id === focusedId && (!seasonId || episode.SeasonId === seasonId || episode.ParentId === seasonId)) ?? episodes.find(episode => !seasonId || episode.SeasonId === seasonId || episode.ParentId === seasonId);
  const playable = series ? focused : item;
  const overviewPlayable = series ? episodes.find(episode => episode.Id === resumeId) ?? focused : item;
  const preferenceItem = page === 0 ? overviewPlayable : playable;
  const streams = preferenceItem ? streamList(preferenceItem) : [];
  const source = preferenceItem?.MediaSources?.[0];
  const audio = streams.filter(stream => stream.Type === 'Audio');
  const subtitles = streams.filter(isPlayableSubtitle);
  const video = streams.find(stream => stream.Type === 'Video');
  const resolved = preferenceItem ? resolveItemPreferences(preferenceItem) : item ? resolveItemPreferences(item) : selection;
  const quality = selection.quality ?? resolved.quality ?? prefs.quality;
  const player = effectivePlayer(selection.player, prefs.player).id;
  const audioIndex = selection.audioStreamIndex ?? resolved.audioStreamIndex ?? source?.DefaultAudioStreamIndex ?? audio.find(stream => stream.IsDefault)?.Index ?? audio[0]?.Index ?? -1;
  const selectedSubtitle = streams.some(stream => stream.Type === 'Subtitle' && stream.GobySubtitleTimelineOnly && stream.Index === selection.subtitleStreamIndex) ? undefined : selection.subtitleStreamIndex;
  const defaultSubtitle = subtitles.find(stream => stream.Index === source?.DefaultSubtitleStreamIndex) ?? subtitles.find(stream => stream.IsDefault);
  const subtitleIndex = selectedSubtitle ?? resolved.subtitleStreamIndex ?? defaultSubtitle?.Index ?? -1;
  const effectiveSelection: ItemPreferences = { ...selection, quality, player, audioStreamIndex: audioIndex, subtitleStreamIndex: subtitleIndex };
  const players = availablePlayers();
  const selectedPlayer = players.find(option => option.id === player);
  const typeName = series ? '剧集' : item?.Type === 'Episode' ? '剧集' : '电影';
  const labels = series ? ['概览', '剧集', '演职员', '媒体信息'] : ['概览', '演职员', '媒体信息'];

  const focusEpisode = (episode: MediaItem) => setFocusedId(episode.Id);

  useEffect(() => {
    if (!item) return;
    setBackdrop(page === 1 && series && focused ? focused : item, page > (series ? 1 : 0) ? 'blur' : 'stage', item);
  }, [item, focused, page, series, setBackdrop]);

  const updateSelection = (patch: Partial<ItemPreferences>) => {
    selectionEdits.current += 1;
    setSelection(current => ({ ...current, ...patch }));
    void setItemPreferences(item?.SeriesId || id, patch).catch(() => notify('选择已保存在此浏览器，暂时无法同步到服务器'));
  };
  const toggleFavorite = async () => {
    if (!item || pending) return;
    setPending(true);
    try {
      const userData = await api.setFavorite(item.Id, !item.UserData?.IsFavorite);
      setItem(current => current ? { ...current, UserData: userData } : current);
      changed();
    } catch { notify('收藏未能保存，请重试。'); }
    finally { setPending(false); }
  };
  const toggleWatched = async (target: MediaItem) => {
    if (watchedRequests.current.has(target.Id)) return;
    watchedRequests.current.add(target.Id);
    setWatchedPending(new Set(watchedRequests.current));
    try {
      const userData = await api.setPlayed(target.Id, !target.UserData?.Played);
      const next = { ...target, UserData: userData };
      if (target.Id === item?.Id) setItem(next);
      setEpisodes(current => current.map(episode => episode.Id === target.Id ? next : episode));
      if (target.Type === 'Series') {
        const result = await api.episodes(target.Id);
        setEpisodes(result.Items);
      }
      changed();
    } catch { notify('观看状态未能保存，请重试。'); }
    finally { watchedRequests.current.delete(target.Id); setWatchedPending(new Set(watchedRequests.current)); }
  };
  const copyLink = async () => {
    if (!overviewPlayable) return;
    const url = api.streamUrl(overviewPlayable.Id, overviewPlayable.MediaSources?.[0]?.Id);
    try { await navigator.clipboard.writeText(url); notify('串流链接已复制'); }
    catch { window.prompt('请复制下方串流链接：', url); }
  };
  const playbackLabel = (target: MediaItem | null | undefined) => target ? `${player !== 'goby' && selectedPlayer ? `用 ${selectedPlayer.name} ` : ''}${progressOf(target) > 0 && !target.UserData?.Played ? '继续播放' : '播放'}${series && target.IndexNumber ? `第 ${target.IndexNumber} 集` : ''}` : '暂无可播放剧集';
  const playLabel = playbackLabel(playable);
  const overviewPlayLabel = playbackLabel(overviewPlayable);

  if (!item) return <div className="detail-loading" role="status">{error ? <><h1>无法载入影片</h1><p>{error}</p><button onClick={() => navigate({ page: 'home' })}>返回首页</button></> : <><span className="detail-loading-ring" /><p>正在载入影片</p></>}</div>;

  const qualityOptions: MenuOption[] = [
    { value: 'original', label: '原画', selectedLabel: ['原画', [resolution(video), videoDynamicRange(video)].filter(Boolean).join(' ')].filter(Boolean).join(' · '), description: [[resolution(video), videoDynamicRange(video)].filter(Boolean).join(' '), bitrate(source?.Bitrate)].filter(Boolean).join(' · ') || '保留片源画质，按设备能力匹配播放方式', isDefault: prefs.quality === 'original' },
    ...(((video?.Width ?? 0) >= 3800 || (video?.Height ?? 0) >= 2100 || quality === '1080p') ? [{ value: '1080p', label: '1080p', selectedLabel: '1080p · 20 Mbps', description: '20 Mbps · 服务器转码', isDefault: prefs.quality === '1080p' }] : []),
    { value: '720p', label: '720p', selectedLabel: '720p · 8 Mbps', description: '8 Mbps · 服务器转码', isDefault: prefs.quality === '720p' },
    { value: '480p', label: '480p', selectedLabel: '480p · 3 Mbps', description: '3 Mbps · 服务器转码', isDefault: prefs.quality === '480p' },
  ];
  const directors = item.People?.filter(person => person.Type === 'Director').map(person => person.Name).join(' / ');
  const stars = item.People?.filter(person => person.Type === 'Actor').slice(0, 3).map(person => person.Name).join(' / ');
  const remaining = overviewPlayable?.RunTimeTicks ? Math.max(0, overviewPlayable.RunTimeTicks - (overviewPlayable.UserData?.PlaybackPositionTicks ?? 0)) / ticksPerSecond : 0;
  const finishTime = remaining ? new Date(Date.now() + remaining * 1000).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false }) : '';

  const overviewPage = <section className="stage-page detail-overview-page" data-stage="true" key="overview">
    {overviewPlayable && <><button className="detail-stage-hit" aria-label={overviewPlayLabel} onClick={() => play(overviewPlayable)} tabIndex={-1} /><PlayButton item={overviewPlayable} label={overviewPlayLabel} /></>}
    <div className="stage-scroll detail-overview-scroll" onClick={() => { if (overviewPlayable) play(overviewPlayable); }}>
      <div className="detail-overview-info" data-stage-info="true">
        <div className="detail-kicker"><strong>{typeName}</strong>{!!item.Genres?.length && <><i /> <span>{item.Genres.join(' / ')}</span></>}</div>
        <h1>{item.Name}</h1>
        <div className="detail-meta">
          {item.CommunityRating != null && <span className="detail-rating"><Icon name="star" size={17} />{item.CommunityRating.toFixed(1)}</span>}
          {item.ProductionYear && <span>{item.ProductionYear}</span>}
          <span>{series ? seasons.length ? `${seasons.length} 季` : '' : movieRuntime(item.RunTimeTicks)}</span>
          {item.OfficialRating && <span className="detail-certificate">{item.OfficialRating}</span>}
          {finishTime && <span className="detail-finish">预计 {finishTime} 结束</span>}
        </div>
        {item.Overview && <p className="detail-overview-description">{item.Overview}</p>}
        {(directors || stars) && <div className="detail-credit">{[directors && `导演 ${directors}`, stars && `主演 ${stars}`].filter(Boolean).join('　·　')}</div>}
        <div className="detail-actions" onClick={event => event.stopPropagation()}>
          <DetailMenu label="画质" value={quality} options={qualityOptions} onChange={value => updateSelection({ quality: value as Quality })} />
          <DetailMenu label="音轨" value={String(audioIndex)} options={audio.length ? audio.map(stream => ({ value: String(stream.Index), label: audioLabel(stream), selectedLabel: audioSummary(stream), description: stream.Title, isDefault: stream.IsDefault })) : [{ value: '-1', label: '自动选择', description: '播放时匹配可用音轨' }]} onChange={value => updateSelection({ audioStreamIndex: Number(value) })} />
          <DetailMenu label="字幕" value={String(subtitleIndex)} options={[{ value: '-1', label: '关闭', isDefault: prefs.subtitleMode === 'None' }, ...subtitles.map(stream => ({ value: String(stream.Index), label: subtitleLabel(stream), description: [subtitleCodec(stream.Codec), stream.IsExternal ? '外挂' : '内封'].filter(Boolean).join(' · '), isDefault: stream.IsDefault }))]} onChange={value => updateSelection({ subtitleStreamIndex: Number(value) })} />
          <DetailMenu label="播放器" value={player} options={players.map(option => ({ value: option.id, label: option.name, isDefault: prefs.player === option.id, description: option.id === 'goby' ? '网页内播放 · 支持转码与字幕样式' : option.description, icon: <span className={`detail-player-icon ${option.id === 'goby' ? 'is-native' : ''}`}><PlayerLogo id={option.id}/></span> }))} onChange={value => updateSelection({ player: value })} footer={<button className="detail-copy-link" onClick={copyLink} disabled={!playable}><span className="detail-player-icon"><Icon name="copy" size={17} /></span>复制串流链接</button>} />
          <span className="detail-actions-divider" />
          <button className="detail-setting" title={item.UserData?.IsFavorite ? '已收藏' : '收藏'} aria-pressed={!!item.UserData?.IsFavorite} onClick={toggleFavorite} disabled={pending}><span className="detail-setting-label">收藏</span><span className={`detail-action-icon ${item.UserData?.IsFavorite ? 'is-selected' : ''}`}><Icon name="heart" size={20} fill={item.UserData?.IsFavorite ? 'currentColor' : 'none'} /></span></button>
          <button className="detail-setting" title={item.UserData?.Played ? '已看过' : '标记已看'} aria-pressed={!!item.UserData?.Played} disabled={watchedPending.has(item.Id)} onClick={() => toggleWatched(item)}><span className="detail-setting-label">看过</span><span className={`detail-action-icon ${item.UserData?.Played ? 'is-selected' : ''}`}><Icon name="check" size={20} /></span></button>
        </div>
      </div>
    </div>
  </section>;

  const episodePage = <section className="stage-page detail-episodes-page" data-stage="true" key="episodes">
    {focused && <><button className="detail-stage-hit" aria-label={`播放${focused.Name}`} onClick={() => play(focused)} tabIndex={-1} /><PlayButton item={focused} label={playLabel} /></>}
    <div className="stage-scroll detail-episodes-scroll" onClick={() => { if (focused) play(focused); }}>
      <div className="detail-episodes-bottom">
        {focused && <EpisodeInformation key={item.Id} episode={focused} series={item} seasonNumber={seasons.find(season => season.Id === seasonId)?.IndexNumber} />}
        <EpisodeRail series={item} seasons={seasons} episodes={episodes} selectedSeason={seasonId} focused={focusedId} active={page === 1} watchedPending={watchedPending} onSeason={season => { setSeasonId(season); const first = episodes.find(episode => episode.SeasonId === season || episode.ParentId === season); setFocusedId(first?.Id ?? ''); }} onFocus={focusEpisode} onPlay={episode => play(episode)} onWatched={toggleWatched} />
      </div>
    </div>
  </section>;

  const peoplePage = <section className="stage-page detail-people-page" key="people">
    <div className="stage-scroll detail-secondary-scroll">
      <div className="detail-secondary-content">
        <div className="detail-people-section"><h2>演职员</h2><div className="detail-cast-rail-wrap"><div ref={castRail.rail} className="detail-cast-rail">{item.People?.length ? [...item.People].sort((a, b) => Number(b.Type === 'Director') - Number(a.Type === 'Director')).map((person, index) => <div className="detail-person" key={`${person.Name}-${index}`}><div className="detail-person-avatar">{person.Name.trim().charAt(0)}</div><div className="detail-person-name">{person.Name}</div><div className="detail-person-role">{personRole(person)}</div></div>) : <p className="detail-empty-inline">暂无演职员信息</p>}</div><button className="detail-cast-arrow is-left" aria-label="上一组演职员" disabled={!castRail.left} aria-hidden={!castRail.left} tabIndex={castRail.left ? 0 : -1} onClick={() => castRail.rail.current?.scrollBy({ left: -(castRail.rail.current.clientWidth * .75), behavior: 'smooth' })}><Icon name="chevronLeft" size={20} /></button><button className="detail-cast-arrow is-right" aria-label="下一组演职员" disabled={!castRail.right} aria-hidden={!castRail.right} tabIndex={castRail.right ? 0 : -1} onClick={() => castRail.rail.current?.scrollBy({ left: castRail.rail.current.clientWidth * .75, behavior: 'smooth' })}><Icon name="chevronRight" size={20} /></button></div></div>
        <div className="detail-similar-section"><h2>相似推荐</h2><div className="detail-similar-rail-wrap"><div ref={similarRail.rail} className="detail-similar-rail">{similar.length ? similar.map(movie => <DetailSimilarCard key={movie.Id} item={movie} onOpen={() => navigate({ page: 'detail', id: movie.Id })} />) : <p className="detail-empty-inline">暂时没有相似影片</p>}</div><button className="detail-similar-arrow is-left" aria-label="上一组相似影片" disabled={!similarRail.left} aria-hidden={!similarRail.left} tabIndex={similarRail.left ? 0 : -1} onClick={() => similarRail.rail.current?.scrollBy({ left: -(similarRail.rail.current.clientWidth * .75), behavior: 'smooth' })}><Icon name="chevronLeft" size={20} /></button><button className="detail-similar-arrow is-right" aria-label="下一组相似影片" disabled={!similarRail.right} aria-hidden={!similarRail.right} tabIndex={similarRail.right ? 0 : -1} onClick={() => similarRail.rail.current?.scrollBy({ left: similarRail.rail.current.clientWidth * .75, behavior: 'smooth' })}><Icon name="chevronRight" size={20} /></button></div></div>
      </div>
    </div>
  </section>;

  const mediaPage = <MediaInformation key="media" item={playable ?? item} series={series ? item : undefined} selection={effectiveSelection} active={page === labels.length - 1} />;
  return <StageDeck labels={labels} page={page} onPage={setPage}>{series ? [overviewPage, episodePage, peoplePage, mediaPage] : [overviewPage, peoplePage, mediaPage]}</StageDeck>;
}

export default DetailPage;

