import { useEffect, useId, useRef, useState, type CSSProperties, type ReactNode, type SVGProps } from 'react';
import type { MediaItem } from './types';
import { api } from './lib/api';
import { formatDuration, mediaBadges, progressPercent } from './lib/format';
import { useApp } from './context';
import iinaIcon from './assets/players/iina.png';
import infuseIcon from './assets/players/infuse.png';
import mxIcon from './assets/players/mx.png';
import nplayerIcon from './assets/players/nplayer.png';
import vlcIcon from './assets/players/vlc.svg';
import potplayerIcon from './assets/players/potplayer.png';
import stellarIcon from './assets/players/stellar.png';
import mpvIcon from './assets/players/mpv.png';
import dandanplayIcon from './assets/players/dandanplay.png';
import { PosterPreviewTarget } from './components/PosterPreview';
import { useArtworkAccent } from './lib/artworkAccent';

const paths: Record<string, ReactNode> = {
  search: <><circle cx="11" cy="11" r="7"/><path d="M20 20l-3.6-3.6"/></>,
  home: <path d="M4 11l8-7 8 7v8.5a.5.5 0 0 1-.5.5H15v-6H9v6H4.5a.5.5 0 0 1-.5-.5z"/>,
  movie: <><rect x="3" y="4" width="18" height="16" rx="2.5"/><path d="M7.5 4v16M16.5 4v16M3 9.5h4.5M3 14.5h4.5M16.5 9.5H21M16.5 14.5H21"/></>,
  film: <><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M7 3v18M17 3v18M3 8h4M3 16h4M17 8h4M17 16h4"/></>,
  series: <><rect x="3" y="6" width="18" height="13" rx="2.5"/><path d="M8.5 2.5L12 6l3.5-3.5"/></>,
  tv: <><rect x="3" y="6" width="18" height="15" rx="2"/><path d="m8 2 4 4 4-4"/></>,
  heart: <path d="M12 20s-7.5-4.6-7.5-10.2A4.3 4.3 0 0 1 12 7a4.3 4.3 0 0 1 7.5 2.8C19.5 15.4 12 20 12 20z"/>,
  check: <path d="m5 12 4.5 4.5L19 7"/>,
  play: <path d="m8 4 13 8-13 8Z"/>,
  pause: <><path d="M8 4v16M16 4v16"/></>,
  info: <><circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7v.1"/></>,
  chevronDown: <path d="m6 9 6 6 6-6"/>,
  chevronUp: <path d="m6 15 6-6 6 6"/>,
  chevronLeft: <path d="m15 5-7 7 7 7"/>,
  chevronRight: <path d="m9 5 7 7-7 7"/>,
  down: <path d="m6 9 6 6 6-6"/>,
  back: <path d="m15 5-7 7 7 7"/>,
  close: <path d="m6 6 12 12M6 18 18 6"/>,
  volume: <><path d="m11 4-6 5H2v6h3l6 5Z"/><path d="M15 8a6 6 0 0 1 0 8M18 4a11 11 0 0 1 0 16"/></>,
  muted: <><path d="m11 4-6 5H2v6h3l6 5Z"/><path d="m16 9 5 6m0-6-5 6"/></>,
  fullscreen: <path d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5"/>,
  subtitles: <><rect x="2" y="4" width="20" height="16" rx="3"/><path d="M6 10h4m4 0h4M6 15h6m3 0h3"/></>,
  settings: <><path d="M4 7h9m4 0h3M4 17h3m4 0h9"/><circle cx="15" cy="7" r="2"/><circle cx="9" cy="17" r="2"/></>,
  filter: <><path d="M4 7h16M7 12h10M10 17h4"/></>,
  sort: <path d="M4 7h16M7 12h10M10 17h4"/>,
  star: <path d="m12 2 3 6.5 7 1-5 5 1.2 7L12 18l-6.2 3.5L7 14.5l-5-5 7-1Z"/>,
  external: <><path d="M14 3h7v7m0-7L10 14"/><path d="M10 3H3v18h18v-7"/></>,
  copy: <><rect x="8" y="8" width="13" height="13" rx="2"/><path d="M16 8V3H3v13h5"/></>,
  user: <><circle cx="12" cy="8" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2"/></>,
  users: <><circle cx="9" cy="8.5" r="3.5"/><path d="M2.8 19.5c.9-3 3.3-4.6 6.2-4.6s5.3 1.6 6.2 4.6M16 5.2a3.4 3.4 0 0 1 0 6.6M18.4 14.9c1.5.7 2.5 2.2 2.9 4.6"/></>,
  monitor: <><rect x="3.5" y="4" width="17" height="12.5" rx="2"/><path d="M8.5 20h7M12 16.5V20"/></>,
  episodes: <><rect x="3" y="5" width="18" height="15" rx="2"/><path d="M7 2h10M7 10h10M7 15h7"/></>,
  restart: <><path d="M3 4v6h6"/><path d="M3 10a9 9 0 1 1 2 8"/></>,
  skip: <><path d="m5 4 11 8-11 8ZM20 4v16"/></>,
  logout: <><path d="M14.5 4.5h3a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2h-3M10 16.5L5.5 12 10 7.5M5.5 12H15"/></>,
  audio: <><path d="M5 15V5l14-2v12M5 8l14-2"/><ellipse cx="3" cy="18" rx="3" ry="3"/><ellipse cx="17" cy="18" rx="3" ry="3"/></>,
  refresh: <><path d="M20 7v5h-5M4 17v-5h5"/><path d="M5 8a8 8 0 0 1 13-3l2 2M4 17l2 2a8 8 0 0 0 13-3"/></>,
};

export function Icon({ name, size = 22, ...props }: SVGProps<SVGSVGElement> & { name: string; size?: number }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" {...props}>{paths[name] ?? paths.info}</svg>;
}

const playerIcons: Record<string, string> = { iina: iinaIcon, infuse: infuseIcon, mx: mxIcon, nplayer: nplayerIcon, vlc: vlcIcon, potplayer: potplayerIcon, stellar: stellarIcon, mpv: mpvIcon, dandanplay: dandanplayIcon };
export function PlayerLogo({ id }: { id: string }) {
  if (id === 'goby') return <span aria-hidden="true">G</span>;
  return playerIcons[id] ? <img className="player-brand-icon" src={playerIcons[id]} alt="" width={30} height={30}/> : <Icon name="external" size={18}/>;
}

export function MediaImage({ item, kind = 'Primary', className = '' }: { item: MediaItem; kind?: 'Primary' | 'Backdrop' | 'Thumb'; className?: string }) {
  const [failed, setFailed] = useState(false);
  const src = api.imageUrl(item, kind, kind === 'Primary' ? 440 : 640);
  useEffect(() => setFailed(false), [src]);
  return src && !failed ? <img className={className} src={src} alt={item.Name} loading="lazy" decoding="async" onError={() => setFailed(true)} /> : <span className={`image-placeholder ${className}`} role="img" aria-label={`${item.Name}，暂无图片`}><Icon name={item.Type === 'Series' ? 'series' : 'film'} size={32}/><span>{item.Name}</span></span>;
}

export function Meta({ item }: { item: MediaItem }) {
  const bits = [item.ProductionYear, item.Genres?.slice(0, 2).join(' / '), item.Type === 'Series' ? (item.ChildCount ? `${item.ChildCount} 季` : '剧集') : item.RunTimeTicks ? formatDuration(item.RunTimeTicks) : undefined].filter(Boolean);
  return <div className="media-meta"><span>{bits.join(' · ')}</span>{item.CommunityRating != null && <span className="rating"><Icon name="star" size={15} fill="currentColor"/>{item.CommunityRating.toFixed(1)}</span>}{mediaBadges(item).map(b => <span key={b} className="spec-badge">{b}</span>)}</div>;
}

type PosterGridVariant = 'catalog' | 'favorites' | 'search';

function PosterCard({ item, variant }: { item: MediaItem; variant: PosterGridVariant }) {
  const { navigate } = useApp();
  const progress = item.UserData?.Played ? 0 : progressPercent(item);
  const accent = useArtworkAccent(variant !== 'search' && progress > 0 ? item : null);
  const subtitle = variant === 'catalog'
    ? [item.ProductionYear, item.Genres?.slice(0, 2).join(' / ')].filter(Boolean).join(' · ')
    : [['Series', 'Season', 'Episode'].includes(item.Type) ? '剧集' : '电影', item.ProductionYear].filter(Boolean).join(' · ');
  return <PosterPreviewTarget item={item}><button className="poster-card" style={accent} onClick={() => navigate({ page: 'detail', id: item.SeriesId || item.Id })}><span className="poster-art"><MediaImage item={item}/>{variant === 'catalog' && item.UserData?.Played && <span className="watched-mark"><Icon name="check" size={14} strokeWidth={2.6}/></span>}{variant !== 'search' && progress > 0 && <span className="card-progress"><span style={{ width: `${progress}%` }}/></span>}</span><strong>{item.Name}</strong><small>{subtitle}</small></button></PosterPreviewTarget>;
}

export function PosterGrid({ items, variant = 'catalog' }: { items: MediaItem[]; variant?: PosterGridVariant }) {
  return <div className="poster-grid">{items.map(item => <PosterCard item={item} variant={variant} key={item.Id}/>)}</div>;
}

export function PageState({ loading, error, onRetry, children }: { loading?: boolean; error?: string; onRetry?: () => void; children?: ReactNode }) {
  return <div className="page-state" role={error ? 'alert' : 'status'}>{loading ? <><span className="spinner"/><p>正在加载片库</p></> : error ? <><Icon name="info" size={34}/><p>{error}</p>{onRetry && <button className="glass-button" onClick={onRetry}>重试</button>}</> : children}</div>;
}

export function SettingMenu({ label, value, options, onChange }: { label: string; value: string; options: { value: string; label: string; description?: string }[]; onChange: (value: string) => void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const id = useId();
  useEffect(() => {
    if (!open) return;
    const down = (e: PointerEvent) => { if (!ref.current?.contains(e.target as Node)) setOpen(false); };
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.stopPropagation(); setOpen(false); ref.current?.querySelector('button')?.focus(); } };
    document.addEventListener('pointerdown', down); document.addEventListener('keydown', key);
    return () => { document.removeEventListener('pointerdown', down); document.removeEventListener('keydown', key); };
  }, [open]);
  return <div className="setting-menu" ref={ref} data-menu><button className="setting-trigger" aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}><span>{label}</span><strong>{options.find(o => o.value === value)?.label ?? value}<Icon name="chevronDown" size={15}/></strong></button>{open && <div className="menu-popover" id={id}>{options.map(option => <button key={option.value} aria-pressed={option.value === value} onClick={() => { onChange(option.value); setOpen(false); ref.current?.querySelector('button')?.focus(); }}><span>{option.label}{option.description && <small>{option.description}</small>}</span>{option.value === value && <Icon name="check" size={16}/>}</button>)}</div>}</div>;
}

const clamp = (value: number, low: number, high: number) => Math.max(low, Math.min(high, value));
const smooth = (value: number) => value * value * (3 - 2 * value);

export function useStageFocus(routeKey: string) {
  useEffect(() => {
    let raf = 0;
    let point: { x: number; y: number } | null = null;
    const restore = () => {
      document.documentElement.style.setProperty('--focus-dim', '0');
      const stage = document.querySelector('.deck-panel[data-active="true"] [data-stage]');
      document.querySelectorAll<HTMLElement>('[data-stage-info],[data-chrome]').forEach(el => { if (stage && matchMedia('(pointer:fine) and (min-width:720px)').matches) el.style.opacity = '.66'; else el.style.removeProperty('opacity'); });
      document.querySelectorAll<HTMLElement>('.central-play').forEach(el => {
        for (const property of ['--play-opacity', '--play-scale', '--play-disc-opacity', '--play-label-opacity', '--play-label-shift']) el.style.removeProperty(property);
        delete el.dataset.labelsInteractive;
      });
    };
    const update = () => {
      raf = 0;
      const stage = document.querySelector<HTMLElement>('.deck-panel[data-active="true"] [data-stage]');
      if (!stage || !point || !matchMedia('(pointer:fine) and (min-width:720px)').matches) { restore(); return; }
      if (stage.querySelector('.stage-rail:hover,.detail-episode-strip:hover')) { restore(); return; }
      const play = stage.querySelector<HTMLElement>('.central-play');
      const info = stage.querySelector<HTMLElement>('[data-stage-info]');
      const disc = play?.querySelector<HTMLElement>('.play-disc');
      let proximityToPlay = 0;
      if (disc && !stage.querySelector('.stage-rail:hover,.detail-episode-strip:hover')) {
        const bounds = disc.getBoundingClientRect();
        const radius = bounds.width / 2;
        const dx = point.x - (bounds.left + radius), dy = point.y - (bounds.top + bounds.height / 2);
        const rx = Math.max(220, .26 * innerWidth), ry = Math.max(150, .21 * innerHeight);
        const normalized = Math.hypot(dx / rx, dy / ry), inner = radius / Math.min(rx, ry);
        proximityToPlay = Math.hypot(dx, dy) <= radius ? 1 : clamp((1 - normalized) / (1 - inner), 0, 1);
      }
      const cp = smooth(proximityToPlay);
      if (play) {
        const label = clamp((proximityToPlay - .55) / .4, 0, 1);
        play.style.setProperty('--play-opacity', String(cp));
        play.style.setProperty('--play-scale', String(.9 + .1 * cp));
        play.style.setProperty('--play-disc-opacity', String(.06 + .16 * cp));
        play.style.setProperty('--play-label-opacity', String(label));
        play.style.setProperty('--play-label-shift', `${(1 - label) * 6}px`);
        play.dataset.labelsInteractive = String(label > .6);
      }
      const proximity = (el: HTMLElement) => { const r = el.getBoundingClientRect(); const d = Math.hypot(Math.max(r.left - point!.x, 0, point!.x - r.right), Math.max(r.top - point!.y, 0, point!.y - r.bottom)); return smooth(clamp(1 - d / 140, 0, 1)); };
      const ie = info ? proximity(info) : 0;
      document.querySelectorAll<HTMLElement>('[data-chrome]').forEach(el => el.style.opacity = String(clamp(.66 + .34 * proximity(el) - .36 * cp, .3, 1)));
      if (info) info.style.opacity = String(clamp(.66 + .34 * ie - .36 * cp, .3, 1));
      document.documentElement.style.setProperty('--focus-dim', String(Math.max(cp * .3, ie * .42)));
    };
    const move = (event: MouseEvent) => { point = { x: event.clientX, y: event.clientY }; if (!raf) raf = requestAnimationFrame(update); };
    const leave = () => { point = null; restore(); };
    document.addEventListener('mousemove', move); document.documentElement.addEventListener('mouseleave', leave);
    document.addEventListener('goby:page', leave);
    return () => { cancelAnimationFrame(raf); restore(); document.removeEventListener('mousemove', move); document.documentElement.removeEventListener('mouseleave', leave); document.removeEventListener('goby:page', leave); };
  }, [routeKey]);
}

export function StageDeck({ labels, page, onPage, children }: { labels: string[]; page: number; onPage: (page: number) => void; children: ReactNode[] }) {
  const ref = useRef<HTMLDivElement>(null);
  const lock = useRef(0);
  const previousPage = useRef(page);
  const state = useRef({ page, onPage, count: labels.length });
  state.current = { page, onPage, count: labels.length };
  useEffect(() => {
    lock.current = performance.now() + 1050;
    if (ref.current) ref.current.dataset.lockedUntil = String(lock.current);
    document.dispatchEvent(new CustomEvent('goby:page', { detail: { direction: Math.sign(page - previousPage.current) } }));
    previousPage.current = page;
    const active = ref.current?.querySelector<HTMLElement>('.deck-panel[data-active="true"]');
    if (ref.current?.contains(document.activeElement) && !active?.contains(document.activeElement)) active?.focus({ preventScroll: true });
  }, [page]);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    let sum = 0, last = 0, lastInner = 0, armed = true;
    let touch: { x: number; y: number; scroll: HTMLElement | null; top: number; bottom: number; inside: boolean } | null = null;
    const activeScroll = () => el.querySelector<HTMLElement>('.deck-panel[data-active="true"] .stage-scroll');
    const canScroll = (scroll: HTMLElement | null, direction: number) => !!scroll && /^(auto|scroll)$/.test(getComputedStyle(scroll).overflowY) && (direction > 0 ? scroll.scrollTop + scroll.clientHeight < scroll.scrollHeight - 2 : scroll.scrollTop > 1);
    const turn = (n: number) => { if (performance.now() < lock.current) return; const s = state.current; const next = clamp(n, 0, s.count - 1); if (next !== s.page) { lock.current = performance.now() + 1050; s.onPage(next); } };
    const wheel = (e: WheelEvent) => {
      const target = e.target;
      if (!(target instanceof Element) || e.defaultPrevented || e.ctrlKey || e.metaKey || e.shiftKey || Math.abs(e.deltaY) <= Math.abs(e.deltaX)) return;
      if (!el.contains(target) && !target.closest('.poster-preview')) return;
      if (target.closest('[data-menu]')) return;
      const unit = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? el.clientHeight : 1;
      const deltaY = e.deltaY * unit;
      const now = performance.now();
      const scroll = [target.closest<HTMLElement>('.poster-preview-surface'), target.closest<HTMLElement>('.stage-scroll') ?? activeScroll()].find(element => canScroll(element, deltaY));
      if (scroll) {
        sum = 0; last = now; lastInner = now; armed = false;
        // Overlay controls and portal previews cannot natively scroll the stage beneath them.
        if (!scroll.contains(target)) { e.preventDefault(); scroll.scrollBy({ top: deltaY }); }
        return;
      }
      e.preventDefault();
      if (now - last > 160) sum = 0;
      const fresh = now - last > 160;
      if (fresh) armed = true;
      last = now;
      if (now < lock.current || now - lastInner < 260) { sum = 0; return; }
      if (!armed) return;
      if (Math.sign(sum) !== Math.sign(deltaY)) sum = 0;
      sum += deltaY;
      if ((fresh && Math.abs(deltaY) >= 4) || Math.abs(sum) >= 75) { turn(state.current.page + Math.sign(sum)); sum = 0; armed = false; }
    };
    const key = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      if (target.closest('input,textarea,select,[contenteditable],[data-menu]') || (e.key === ' ' && target.closest('button,a'))) return;
      const map: Record<string, number> = { ArrowDown: 1, PageDown: 1, ' ': e.shiftKey ? -1 : 1, ArrowUp: -1, PageUp: -1 };
      if (e.key in map) {
        e.preventDefault();
        const scroll = target.closest<HTMLElement>('.stage-scroll') ?? el.querySelector<HTMLElement>('.deck-panel[data-active="true"] .stage-scroll');
        const direction = map[e.key];
        if (scroll && ((direction > 0 && scroll.scrollTop + scroll.clientHeight < scroll.scrollHeight - 2) || (direction < 0 && scroll.scrollTop > 1))) {
          scroll.scrollBy({ top: direction * (e.key.startsWith('Page') || e.key === ' ' ? scroll.clientHeight * .85 : 64), behavior: 'smooth' }); return;
        }
        turn(state.current.page + direction);
      }
      else if (e.key === 'Home' || e.key === 'End') { e.preventDefault(); turn(e.key === 'Home' ? 0 : state.current.count - 1); }
    };
    const touchStart = (e: TouchEvent) => {
      touch = null;
      if (e.touches.length !== 1 || !(e.target instanceof Element) || e.target.closest('[data-menu]')) return;
      const scroll = e.target.closest<HTMLElement>('.stage-scroll') ?? activeScroll();
      touch = { x: e.touches[0].clientX, y: e.touches[0].clientY, scroll, top: scroll?.scrollTop ?? 0, bottom: scroll ? scroll.scrollHeight - scroll.clientHeight - scroll.scrollTop : 0, inside: !!scroll?.contains(e.target) };
    };
    const touchEnd = (e: TouchEvent) => {
      const gesture = touch;
      touch = null;
      if (!gesture || e.touches.length || !e.changedTouches.length) return;
      const dx = e.changedTouches[0].clientX - gesture.x, dy = e.changedTouches[0].clientY - gesture.y;
      if (Math.abs(dy) <= 50 || Math.abs(dy) <= Math.abs(dx) * 1.3) return;
      if (dy < 0 && gesture.bottom > 2 || dy > 0 && gesture.top > 1) {
        if (!gesture.inside) gesture.scroll?.scrollBy({ top: -dy });
        return;
      }
      turn(state.current.page + (dy < 0 ? 1 : -1));
    };
    const touchCancel = () => { touch = null; };
    document.addEventListener('wheel', wheel, { passive: false }); document.addEventListener('keydown', key); el.addEventListener('touchstart', touchStart, { passive: true }); el.addEventListener('touchend', touchEnd, { passive: true }); el.addEventListener('touchcancel', touchCancel, { passive: true });
    return () => { document.removeEventListener('wheel', wheel); document.removeEventListener('keydown', key); el.removeEventListener('touchstart', touchStart); el.removeEventListener('touchend', touchEnd); el.removeEventListener('touchcancel', touchCancel); };
  }, []);
  return <div className="stage-deck" ref={ref}>{children.map((child, i) => <section key={labels[i]} className={`deck-panel ${i < page ? 'passed' : ''}`} data-active={i === page} inert={i !== page} tabIndex={-1} aria-label={labels[i]}>{child}</section>)}<nav className="page-rail" aria-label="页面分页" data-chrome>{labels.map((label, i) => <button key={label} title={label} aria-label={label} aria-current={page === i ? 'step' : undefined} onClick={() => { if (i !== page && performance.now() >= lock.current) onPage(i); }}><span/><em>{label}</em></button>)}</nav>{page === 0 && labels.length > 1 && <button className="next-page" onClick={() => { if (performance.now() >= lock.current) onPage(page + 1); }}>{labels[page + 1]}<Icon name="chevronDown" size={17}/></button>}</div>;
}

export function PlayButton({ item, onPlay, label }: { item: MediaItem; onPlay?: () => void; label?: string }) {
  const { play } = useApp();
  const progress = item.UserData?.Played ? 0 : progressPercent(item);
  return <div className="central-play"><div className="play-core"><button className="play-disc" aria-label={label ?? (progress > 0 ? '继续播放' : '播放')} onClick={event => { event.stopPropagation(); onPlay ? onPlay() : play(item); }}><svg viewBox="0 0 24 24" width="36" height="36" fill="currentColor" aria-hidden="true"><path d="M8 5.6v12.8a1 1 0 0 0 1.52.85l10.2-6.4a1 1 0 0 0 0-1.7L9.52 4.75A1 1 0 0 0 8 5.6z"/></svg></button>{progress > 0 && <svg className="play-progress" width="104" height="104" viewBox="0 0 104 104" aria-hidden="true"><circle cx="52" cy="52" r="50" fill="none" stroke="rgba(255,255,255,.18)" strokeWidth="3"/><circle cx="52" cy="52" r="50" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeDasharray={`${Math.min(100, progress) * Math.PI} 314.16`} transform="rotate(-90 52 52)"/></svg>}</div><div className="central-play-copy central-play-label"><strong>{label ?? (progress > 0 ? '继续播放' : '播放')}</strong>{progress > 0 && <span>已看 {Math.round(progress)}%{item.RunTimeTicks ? ` · 剩余 ${formatDuration(item.RunTimeTicks * (1 - progress / 100))}` : ''}</span>}</div>{progress > 0 && <button className="restart-button central-play-label" onClick={event => { event.stopPropagation(); play(item, true); }}><Icon name="restart" size={15}/>从头播放</button>}</div>;
}

function ArtworkProgress({ item }: { item: MediaItem }) {
  return <span className="card-progress"><span style={{ width: `${progressPercent(item)}%` }}/></span>;
}

export function StageRail({ items, active, onFocus, onActivate, title, children, action, resume = false }: { items: MediaItem[]; active: number; onFocus: (index: number) => void; onActivate: (item: MediaItem) => void; title: string; children?: ReactNode; action?: ReactNode; resume?: boolean }) {
  const [expanded, setExpanded] = useState(false);
  const [edges, setEdges] = useState({ left: false, right: false });
  const ref = useRef<HTMLDivElement>(null);
  const timeout = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const hover = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const hovered = useRef(false);
  const latest = useRef({ active, onFocus, onActivate, items });
  latest.current = { active, onFocus, onActivate, items };
  useEffect(() => {
    const row = ref.current;
    if (!row) return;
    const update = () => {
      const next = { left: row.scrollLeft > 2, right: row.scrollWidth - row.clientWidth - row.scrollLeft > 2 };
      setEdges(previous => previous.left === next.left && previous.right === next.right ? previous : next);
    };
    const observer = new ResizeObserver(update);
    observer.observe(row);
    for (const card of row.children) observer.observe(card);
    row.addEventListener('scroll', update, { passive: true });
    update();
    return () => { observer.disconnect(); row.removeEventListener('scroll', update); };
  }, [items]);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (!ref.current?.closest('.deck-panel[data-active="true"]') || performance.now() < Number(ref.current?.closest<HTMLElement>('.stage-deck')?.dataset.lockedUntil ?? 0) || (e.target as HTMLElement).closest('input,select,textarea,[data-menu]')) return;
      const s = latest.current;
      if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
        e.preventDefault();
        const next = clamp(s.active + (e.key === 'ArrowRight' ? 1 : -1), 0, s.items.length - 1);
        latest.current.active = next;
        s.onFocus(next);
        (ref.current?.children[next] as HTMLElement | undefined)?.focus({ preventScroll: true });
        setExpanded(true); clearTimeout(timeout.current); timeout.current = setTimeout(() => setExpanded(false), 2600);
      }
      else if (e.key === 'Enter' && !(e.target as HTMLElement).closest('button,a') && s.items[s.active]) s.onActivate(s.items[s.active]);
    };
    const reset = () => { clearTimeout(timeout.current); clearTimeout(hover.current); hovered.current = false; setExpanded(false); };
    document.addEventListener('keydown', key); document.addEventListener('goby:page', reset);
    return () => { document.removeEventListener('keydown', key); document.removeEventListener('goby:page', reset); clearTimeout(timeout.current); clearTimeout(hover.current); };
  }, []);
  useEffect(() => { const row = ref.current; const card = row?.children[active] as HTMLElement | undefined; if (row && card) row.scrollTo({ left: card.offsetLeft - row.clientWidth / 2 + card.clientWidth / 2, behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth' }); }, [active]);
  const unlockDelay = () => Math.max(0, Number(ref.current?.closest<HTMLElement>('.stage-deck')?.dataset.lockedUntil ?? 0) - performance.now());
  return <div className={`stage-rail ${expanded ? 'is-expanded' : ''}`} onMouseEnter={() => { hovered.current = true; clearTimeout(timeout.current); timeout.current = setTimeout(() => { if (hovered.current) setExpanded(true); }, Math.max(150, unlockDelay())); }} onMouseLeave={() => { hovered.current = false; clearTimeout(hover.current); clearTimeout(timeout.current); timeout.current = setTimeout(() => setExpanded(false), 280); }} onFocus={event => { if (event.target.matches(':focus-visible') && !unlockDelay()) setExpanded(true); }}><div className="rail-heading"><h2>{title}<span>{items.length}{resume ? ' 项' : ' 部'}</span></h2>{children}{action}</div><div className="stage-rail-cards" ref={ref}>{items.map((item, index) => <button className={`landscape-card ${active === index ? 'is-active' : ''}`} key={item.Id} onMouseEnter={() => { clearTimeout(hover.current); hover.current = setTimeout(() => { if (hovered.current) onFocus(index); }, Math.max(160, unlockDelay())); }} onFocus={() => onFocus(index)} onClick={() => onActivate(item)}><span className="landscape-art"><MediaImage item={item} kind="Backdrop"/>{resume && progressPercent(item) > 0 && <ArtworkProgress item={item}/>}</span><strong>{item.Type === 'Episode' ? item.SeriesName || item.Name : item.Name}</strong><small>{item.Type === 'Episode' ? `第 ${item.ParentIndexNumber ?? 1} 季第 ${item.IndexNumber ?? 1} 集 · ${item.Name}` : resume ? `剩余 ${formatDuration((item.RunTimeTicks || item.MediaSources?.[0]?.RunTimeTicks || 0) * (1 - progressPercent(item) / 100)) || '未知'}` : [item.ProductionYear, item.Genres?.[0]].filter(Boolean).join(' · ')}</small></button>)}</div><button className="rail-arrow rail-left" hidden={!edges.left} aria-label="向左浏览" onClick={() => ref.current?.scrollBy({ left: -innerWidth * .65, behavior: 'smooth' })}><Icon name="chevronLeft"/></button><button className="rail-arrow rail-right" hidden={!edges.right} aria-label="向右浏览" onClick={() => ref.current?.scrollBy({ left: innerWidth * .65, behavior: 'smooth' })}><Icon name="chevronRight"/></button></div>;
}

