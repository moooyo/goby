import { createContext, useCallback, useContext, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { Icon, MediaImage } from '../components';
import { useApp } from '../context';
import { api } from '../lib/api';
import { formatDuration, mediaBadges, progressPercent } from '../lib/format';
import type { AuthSession, MediaItem, UserData } from '../types';
import './poster-preview.css';

const OPEN_DELAY = 620;
const INFO_HEIGHT = 188;
const POINTER_QUERY = '(min-width: 720px) and (hover: hover) and (pointer: fine)';
const clamp = (value: number, minimum: number, maximum: number) => Math.max(minimum, Math.min(maximum, value));

type InputKind = 'pointer' | 'keyboard';
type Geometry = { left: number; top: number; width: number; height: number; imageHeight: number; ghost: { left: number; top: number; width: number; height: number } };
type Preview = { key: string; epoch: number; scope: string; item: MediaItem; anchor: HTMLDivElement; input: InputKind; geometry: Geometry; visible: boolean };
type PreviewContextValue = { enter: (item: MediaItem, anchor: HTMLDivElement, input: InputKind) => void; leave: (anchor: HTMLDivElement, triggerBlur?: boolean) => void; dismiss: (anchor: HTMLDivElement) => void };
const PreviewContext = createContext<PreviewContextValue | null>(null);

function accountScope(session: AuthSession | null): string {
  return JSON.stringify([session?.serverUrl, session?.ServerId, session?.User.Id, session?.AccessToken]);
}

function itemKey(item: MediaItem): string {
  return JSON.stringify([item.Id, item.MediaSources?.map(source => [source.Id, source.ItemId, source.RunTimeTicks]), item.ImageTags, item.BackdropImageTags, item.ParentBackdropItemId, item.ParentBackdropImageTags]);
}

function geometryFor(anchor: HTMLElement): Geometry {
  const rect = anchor.getBoundingClientRect();
  const viewportWidth = document.documentElement.clientWidth || window.innerWidth;
  const viewportHeight = window.innerHeight;
  const width = Math.round(Math.min(clamp(rect.width * 1.85, 340, 430), viewportWidth - 24));
  const imageHeight = Math.round(width * 9 / 16);
  const height = Math.min(imageHeight + INFO_HEIGHT, viewportHeight - 24);
  const minimumTop = viewportHeight >= height + 76 ? 64 : 12;
  const left = Math.round(clamp(rect.left + rect.width / 2 - width / 2, 12, viewportWidth - width - 12));
  const top = Math.round(clamp(rect.top + rect.height / 2 - height / 2, minimumTop, viewportHeight - height - 12));
  return { left, top, width, height, imageHeight, ghost: { left: rect.left - left, top: rect.top - top, width: rect.width, height: rect.height } };
}

function stillUrls(item: MediaItem): string[] {
  const own = item.BackdropImageTags?.filter(Boolean) ?? [];
  const inherited = item.ParentBackdropItemId ? item.ParentBackdropImageTags?.filter(Boolean) ?? [] : [];
  const tags = own.length ? item.BackdropImageTags! : inherited.length ? item.ParentBackdropImageTags! : [];
  const owner = own.length ? item.Id : item.ParentBackdropItemId;
  const urls = tags.flatMap((tag, index) => tag && owner ? [api.mediaUrl(`/emby/Items/${encodeURIComponent(owner)}/Images/Backdrop/${index}?${new URLSearchParams({ tag, MaxWidth: '860', quality: '90' })}`)] : []).slice(0, 3);
  if (!urls.length) {
    const fallback = api.imageUrl(item, 'Backdrop', 860);
    if (fallback) urls.push(fallback);
  }
  return [...new Set(urls)];
}

export function PosterPreviewProvider({ children, routeKey, onPreviewChange }: { children: ReactNode; routeKey: string; onPreviewChange: (item: MediaItem | null) => void }) {
  const { session } = useApp();
  const scope = accountScope(session);
  const [preview, setPreview] = useState<Preview | null>(null);
  const current = useRef<Preview | null>(null);
  const card = useRef<HTMLDivElement>(null);
  const timers = useRef<{ open?: number; close?: number; frame?: number }>({});
  const pending = useRef<HTMLDivElement | null>(null);
  const epoch = useRef(0);
  const controller = useRef<AbortController | null>(null);
  const latestScope = useRef(scope);
  const favoriteResult = useRef<{ epoch: number; value: UserData } | null>(null);
  const dismissed = useRef<HTMLDivElement | null>(null);
  const previewChanged = useRef(onPreviewChange);
  latestScope.current = scope;
  previewChanged.current = onPreviewChange;

  const update = useCallback((value: Preview | null) => { current.current = value; setPreview(value); }, []);
  const clearTimers = useCallback(() => {
    window.clearTimeout(timers.current.open);
    window.clearTimeout(timers.current.close);
    if (timers.current.frame) window.cancelAnimationFrame(timers.current.frame);
    timers.current = {};
  }, []);
  const close = useCallback((immediate = false, restoreFocus = false) => {
    clearTimers();
    controller.current?.abort();
    controller.current = null;
    pending.current = null;
    epoch.current += 1;
    const previous = current.current;
    if (previous && restoreFocus && card.current?.contains(document.activeElement)) {
      dismissed.current = previous.anchor;
      previous.anchor.querySelector<HTMLElement>('button, a, [tabindex="0"]')?.focus({ preventScroll: true });
    }
    if (previous) previewChanged.current(null);
    if (!previous || immediate) { update(null); return; }
    update({ ...previous, visible: false });
    timers.current.close = window.setTimeout(() => update(null), 300);
  }, [clearTimers, update]);

  const enter = useCallback((item: MediaItem, anchor: HTMLDivElement, input: InputKind) => {
    if (document.hidden || !anchor.isConnected || input === 'pointer' && !matchMedia(POINTER_QUERY).matches || input === 'keyboard' && dismissed.current === anchor) return;
    if (window.innerWidth < 720 || window.innerHeight < 260) return;
    const active = current.current;
    if (active?.visible && active.anchor === anchor && active.key === itemKey(item)) { window.clearTimeout(timers.current.close); return; }
    close(true);
    pending.current = anchor;
    const expectedEpoch = epoch.current;
    const expectedScope = latestScope.current;
    timers.current.open = window.setTimeout(() => {
      if (pending.current !== anchor || expectedEpoch !== epoch.current || expectedScope !== latestScope.current || !anchor.isConnected || document.hidden) return;
      const value: Preview = { key: itemKey(item), epoch: expectedEpoch, scope: expectedScope, item, anchor, input, geometry: geometryFor(anchor), visible: false };
      update(value);
      previewChanged.current(item);
      const request = new AbortController();
      controller.current = request;
      favoriteResult.current = null;
      // Each opening owns its response. Metadata never starts playback or generation.
      void api.item(item.Id, request.signal).then(loaded => {
        const active = current.current;
        if (request.signal.aborted || expectedEpoch !== epoch.current || expectedScope !== latestScope.current || active?.epoch !== expectedEpoch || loaded.Id !== item.Id) return;
        const favorite = favoriteResult.current?.epoch === expectedEpoch ? favoriteResult.current.value : undefined;
        const nextItem = { ...item, ...loaded, ...(favorite ? { UserData: { ...loaded.UserData, ...favorite } } : {}) };
        update({ ...active, item: nextItem });
        previewChanged.current(nextItem);
      }).catch(() => { /* Available card metadata remains useful when enrichment is unavailable. */ });
      timers.current.frame = window.requestAnimationFrame(() => {
        timers.current.frame = window.requestAnimationFrame(() => {
          const active = current.current;
          if (active?.epoch !== expectedEpoch || expectedEpoch !== epoch.current) return;
          update({ ...active, visible: true });
          if (input === 'keyboard' && anchor.contains(document.activeElement)) {
            const control = card.current?.querySelector<HTMLButtonElement>('.poster-preview-play:not(:disabled)') ?? card.current?.querySelector<HTMLButtonElement>('.poster-preview-actions button:not(:disabled)');
            control?.focus({ preventScroll: true });
          }
        });
      });
    }, OPEN_DELAY);
  }, [close, update]);

  const leave = useCallback((anchor: HTMLDivElement, triggerBlur = false) => {
    if (triggerBlur && dismissed.current === anchor) dismissed.current = null;
    if (pending.current === anchor) { window.clearTimeout(timers.current.open); pending.current = null; }
    if (current.current?.anchor !== anchor) return;
    window.clearTimeout(timers.current.close);
    timers.current.close = window.setTimeout(() => {
      if (!card.current?.contains(document.activeElement) && !anchor.contains(document.activeElement) && !card.current?.matches(':hover')) close();
    }, 120);
  }, [close]);
  const dismiss = useCallback((anchor: HTMLDivElement) => {
    if (pending.current === anchor || current.current?.anchor === anchor) close(true);
  }, [close]);

  useLayoutEffect(() => { close(true); dismissed.current = null; }, [scope, routeKey, close]);
  useEffect(() => {
    const reset = () => close(true);
    const hidden = () => { if (document.hidden) reset(); };
    const scroll = (event: Event) => { if (!(event.target instanceof Node) || !card.current?.contains(event.target)) reset(); };
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && (current.current || pending.current)) {
        event.preventDefault();
        event.stopPropagation();
        const anchor = current.current?.anchor ?? pending.current;
        if (anchor) dismissed.current = anchor;
        close(true, true);
      }
    };
    const pointer = (event: PointerEvent) => {
      const target = event.target as Node | null;
      if (target && !card.current?.contains(target) && !current.current?.anchor.contains(target)) reset();
    };
    const focus = (event: FocusEvent) => {
      const target = event.target as Node | null;
      if (current.current && target && !card.current?.contains(target) && !current.current.anchor.contains(target)) reset();
    };
    window.addEventListener('blur', reset);
    window.addEventListener('resize', reset);
    window.addEventListener('scroll', scroll, true);
    document.addEventListener('visibilitychange', hidden);
    document.addEventListener('goby:page', reset);
    document.addEventListener('keydown', key, true);
    document.addEventListener('pointerdown', pointer, true);
    document.addEventListener('focusin', focus);
    return () => {
      clearTimers();
      controller.current?.abort();
      epoch.current += 1;
      if (current.current) previewChanged.current(null);
      window.removeEventListener('blur', reset);
      window.removeEventListener('resize', reset);
      window.removeEventListener('scroll', scroll, true);
      document.removeEventListener('visibilitychange', hidden);
      document.removeEventListener('goby:page', reset);
      document.removeEventListener('keydown', key, true);
      document.removeEventListener('pointerdown', pointer, true);
      document.removeEventListener('focusin', focus);
    };
  }, [close, clearTimers]);

  const context = useMemo(() => ({ enter, leave, dismiss }), [enter, leave, dismiss]);
  const rendered = preview?.scope === scope ? preview : null;
  return <PreviewContext.Provider value={context}>{children}{rendered && createPortal(<div ref={card} className={`poster-preview ${rendered.visible ? 'is-visible' : ''}`} data-poster-preview={rendered.item.Id}
    style={{ left: rendered.geometry.left, top: rendered.geometry.top, width: rendered.geometry.width, height: rendered.geometry.height }}
    onMouseEnter={() => window.clearTimeout(timers.current.close)} onMouseLeave={() => leave(rendered.anchor)}
    onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) leave(rendered.anchor); }}>
    <PreviewCard key={`${rendered.scope}/${rendered.epoch}`} preview={rendered} onClose={() => close(true)} onFavorite={value => {
      const active = current.current;
      if (active?.epoch !== rendered.epoch || active.scope !== latestScope.current || !active.visible) return;
      favoriteResult.current = { epoch: active.epoch, value };
      update({ ...active, item: { ...active.item, UserData: { ...active.item.UserData, ...value } } });
    }}/>
  </div>, document.body)}</PreviewContext.Provider>;
}

function PreviewCard({ preview, onClose, onFavorite }: { preview: Preview; onClose: () => void; onFavorite: (data: UserData) => void }) {
  const { item, geometry, visible } = preview;
  const { play, navigate, notify, changed, session } = useApp();
  const heading = useId();
  const [saving, setSaving] = useState(false);
  const [slide, setSlide] = useState(0);
  const [failed, setFailed] = useState<string[]>([]);
  const [focused, setFocused] = useState(preview.input === 'keyboard');
  const [reducedMotion, setReducedMotion] = useState(() => matchMedia('(prefers-reduced-motion: reduce)').matches);
  const [rotationPaused, setRotationPaused] = useState(false);
  const mounted = useRef(true);
  const urls = stillUrls(item).filter(url => !failed.includes(url));
  const urlsKey = urls.join('\n');
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => {
    const query = matchMedia('(prefers-reduced-motion: reduce)');
    const update = () => setReducedMotion(query.matches);
    query.addEventListener('change', update);
    return () => query.removeEventListener('change', update);
  }, []);
  useEffect(() => {
    setSlide(0);
    if (!visible || focused || reducedMotion || rotationPaused || urls.length < 2) return;
    const cycle = window.setInterval(() => setSlide(value => (value + 1) % urls.length), 2200);
    return () => window.clearInterval(cycle);
  }, [visible, urlsKey, focused, reducedMotion, rotationPaused, urls.length]);
  const progress = progressPercent(item);
  const hasProgress = progress > 0 && progress < 100;
  const duration = item.RunTimeTicks || item.MediaSources?.[0]?.RunTimeTicks;
  const playLabel = hasProgress ? '继续播放' : '播放';
  const remain = hasProgress && duration ? `剩余 ${formatDuration(duration * (1 - progress / 100))}` : item.Type === 'Series' ? item.ChildCount ? `${item.ChildCount} 季` : '剧集' : formatDuration(duration);
  const meta = [item.ProductionYear, item.Genres?.slice(0, 2).join(' / '), item.Type === 'Series' ? '剧集' : duration ? formatDuration(duration) : undefined].filter(Boolean).join(' · ');
  const favorite = Boolean(item.UserData?.IsFavorite);
  const playable = (item.Type === 'Series' || item.CanPlay !== false) && !item.IsMissing && session.User.Policy?.EnableMediaPlayback !== false;
  const ghost = geometry.ghost;
  const clip = `inset(${ghost.top}px ${geometry.width - ghost.left - ghost.width}px ${geometry.height - ghost.top - ghost.height}px ${ghost.left}px round 14px)`;
  const open = () => { onClose(); navigate({ page: 'detail', id: item.SeriesId || item.Id }); };
  async function toggleFavorite() {
    if (saving) return;
    setSaving(true);
    const account = accountScope(api.getSession());
    try {
      const data = await api.setFavorite(item.Id, !favorite);
      if (accountScope(api.getSession()) !== account) return;
      if (mounted.current) onFavorite(data);
      changed();
    } catch {
      if (accountScope(api.getSession()) === account) notify('收藏状态保存失败，请重试');
    } finally { if (mounted.current) setSaving(false); }
  }
  return <>
    <div className="poster-preview-shadow"/>
    <div className="poster-preview-surface" role="dialog" aria-modal="false" aria-labelledby={heading} style={{ '--poster-preview-clip': clip } as CSSProperties}
      onFocus={() => setFocused(true)} onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false); }}>
      <div className="poster-preview-visual" style={{ height: geometry.imageHeight }}>
        <button className="poster-preview-image-link" tabIndex={-1} onClick={open} aria-label={`查看 ${item.Name} 详情`}>
          {urls.length ? urls.map((url, index) => <img key={url} className={index === slide % urls.length ? 'is-active' : ''} src={url} alt="" decoding="async" onError={() => setFailed(value => [...value, url])}/>) : <span className="image-placeholder" role="img" aria-label={`${item.Name}，暂无剧照`}><Icon name={item.Type === 'Series' ? 'series' : 'film'} size={32}/><span>{item.Name}</span></span>}
          <span className="poster-preview-image-gradient"/>
        </button>
        {urls.length > 1 && !reducedMotion && <button className="poster-preview-rotation" aria-label={rotationPaused ? '继续轮播剧照' : '暂停轮播剧照'} onClick={() => setRotationPaused(value => !value)}><Icon name={rotationPaused ? 'play' : 'pause'} size={12}/></button>}
        <div className="poster-preview-badges">{mediaBadges(item).map(badge => <span key={badge}>{badge}</span>)}</div>
        {hasProgress && <span className="poster-preview-progress"><span style={{ width: `${progress}%` }}/></span>}
      </div>
      <div className="poster-preview-info">
        <div className="poster-preview-heading">
          <MediaImage item={item} className="poster-preview-small-poster"/>
          <div className="poster-preview-title-group"><h2 id={heading} title={item.Name}>{item.Name}</h2><div className="poster-preview-meta">{item.CommunityRating != null && <span className="poster-preview-rating"><Icon name="star" size={12} fill="currentColor"/>{item.CommunityRating.toFixed(1)}</span>}<span>{meta}</span></div></div>
          <button className="poster-preview-play" aria-label={`${playLabel} ${item.Name}`} title={playable ? playLabel : '当前不可播放'} disabled={!playable} onClick={() => { onClose(); play(item); }}><Icon name="play" size={20} fill="currentColor"/></button>
        </div>
        <p className="poster-preview-overview">{item.Overview || '暂无简介'}</p>
        <div className="poster-preview-footer"><span>{[playLabel, remain].filter(Boolean).join(' · ')}</span><div className="poster-preview-actions"><button className={favorite ? 'is-favorite' : ''} aria-label={favorite ? '取消收藏' : '收藏'} aria-pressed={favorite} aria-busy={saving} disabled={saving} title={favorite ? '取消收藏' : '收藏'} onClick={() => void toggleFavorite()}><Icon name="heart" size={16} fill={favorite ? 'currentColor' : 'none'}/></button><button onClick={open}>详情</button></div></div>
      </div>
      <div className="poster-preview-ghost" aria-hidden="true" style={{ left: ghost.left, top: ghost.top, width: ghost.width, height: ghost.height }}><MediaImage item={item}/></div>
    </div>
  </>;
}

export function PosterPreviewTarget({ item, className = '', children }: { item: MediaItem; className?: string; children: ReactNode }) {
  const preview = useContext(PreviewContext);
  const ref = useRef<HTMLDivElement>(null);
  const focusFrame = useRef<number | undefined>(undefined);
  const sourceKey = itemKey(item);
  useEffect(() => {
    const anchor = ref.current;
    return () => { if (focusFrame.current) window.cancelAnimationFrame(focusFrame.current); if (anchor) preview?.dismiss(anchor); };
  }, [preview, sourceKey]);
  return <div className={`poster-preview-target ${className}`} ref={ref}
    onPointerEnter={event => { if (event.pointerType === 'mouse' || event.pointerType === 'pen') preview?.enter(item, event.currentTarget, 'pointer'); }}
    onPointerLeave={event => preview?.leave(event.currentTarget)}
    onFocus={event => {
      if (!event.target.matches(':focus-visible')) return;
      const anchor = event.currentTarget;
      if (focusFrame.current) window.cancelAnimationFrame(focusFrame.current);
      // Native keyboard focus can scroll the trigger into view before the next paint.
      focusFrame.current = window.requestAnimationFrame(() => { if (anchor.contains(document.activeElement)) preview?.enter(item, anchor, 'keyboard'); });
    }}
    onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) { if (focusFrame.current) window.cancelAnimationFrame(focusFrame.current); preview?.leave(event.currentTarget, true); } }}
  >{children}</div>;
}
