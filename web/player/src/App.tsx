import { Component, lazy, Suspense, useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import type { AuthSession, MediaItem } from './types';
import { AppContext, type Route } from './context';
import { api } from './lib/api';
import { getItemPreferences, getPreferences, loadItemPreferences, loadPreferences, setPreferences } from './lib/preferences';
import { effectivePlayer, externalPlayerUrl } from './lib/players';
import { ExternalLaunchNotice, type ExternalLaunch } from './components/ExternalLaunchNotice';
import { savedRoute, saveRoute } from './lib/navigation';
import { Icon, PageState, useStageFocus } from './components';
import { HomePage } from './pages/HomePage';
import { PreviewSoundProvider, ThemePreview } from './components/ThemePreview';
import { PosterPreviewProvider } from './components/PosterPreview';
import { deriveArtworkAccent } from './lib/artworkAccent';
import './background-motion.css';

const DetailPage = lazy(() => import('./pages/DetailPage').then(module => ({ default: module.DetailPage })));
const PlayerPage = lazy(() => import('./pages/PlayerPage').then(module => ({ default: module.PlayerPage })));
const CatalogPage = lazy(() => import('./pages/CatalogPage').then(module => ({ default: module.CatalogPage })));
const SettingsPage = lazy(() => import('./pages/SettingsPage').then(module => ({ default: module.SettingsPage })));

function readRoute(): Route {
  const [page, id] = location.hash.replace(/^#\/?/, '').split('/');
  if (['home', 'movies', 'series', 'favorites', 'search', 'settings', 'detail', 'player'].includes(page)) return { page: page as Route['page'], id: id ? decodeURIComponent(id) : undefined };
  return savedRoute();
}

class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? <PageState error="页面暂时无法显示，请重新加载。" onRetry={() => location.reload()}/> : this.props.children; }
}

function Login({ onAuthenticated }: { onAuthenticated: (session: AuthSession) => void }) {
  const [server, setServer] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  return <main className="login-page"><form className="login-card" onSubmit={async event => { event.preventDefault(); setPending(true); setError(''); try { const session = await api.authenticate(server || location.origin, username, password); onAuthenticated(session); } catch { setError('登录失败，请检查服务器地址、用户名和密码。'); } finally { setPending(false); } }}><span className="login-symbol"><Icon name="play" size={30}/></span><h1>回到你的光影世界</h1><p>登录 Goby，继续你的故事。</p><label htmlFor="server">服务器地址</label><input id="server" type="url" value={server} onChange={event => setServer(event.target.value)} placeholder={location.origin} autoComplete="url"/><label htmlFor="username">用户名</label><input id="username" value={username} onChange={event => setUsername(event.target.value)} autoComplete="username" required autoFocus/><label htmlFor="password">密码</label><input id="password" type="password" value={password} onChange={event => setPassword(event.target.value)} autoComplete="current-password"/>{error && <p className="form-error" role="alert">{error}</p>}<button className="primary-button" type="submit" disabled={pending}>{pending ? '正在连接…' : '登录'}</button></form></main>;
}

function applyAccent(image: HTMLImageElement) {
  const accent = deriveArtworkAccent(image);
  if (accent) for (const [property, value] of Object.entries(accent)) document.documentElement.style.setProperty(property, value);
}

function Background({ item, accentItem, mode, routeKey }: { item: MediaItem | null; accentItem: MediaItem | null; mode: 'stage' | 'blur' | 'none'; routeKey: string }) {
  const [layers, setLayers] = useState<{ src: string; key: number }[]>([]);
  const [railState, setRailState] = useState({ present: false, expanded: false, dimmed: false, page: 0, hero: false });
  const root = useRef<HTMLDivElement>(null);
  const transition = useRef({ direction: 0, at: 0, outgoing: '' });
  const src = item && mode !== 'none' ? api.imageUrl(item, 'Backdrop', mode === 'blur' ? 192 : 1920) : '';
  const poster = accentItem ? api.imageUrl(accentItem, 'Primary', 320) : '';
  useEffect(() => {
    const sync = () => {
      const active = document.querySelector('.deck-panel[data-active="true"]');
      const rail = active?.querySelector('.stage-rail,.detail-episode-strip');
      const hasStrip = !!rail || !!active?.querySelector('.home-strip-page');
      const next = { present: hasStrip, expanded: hasStrip && (rail?.classList.contains('is-expanded') || matchMedia('(pointer:coarse),(max-width:719px)').matches), dimmed: !!rail && rail.classList.contains('is-expanded') && matchMedia('(min-width:720px)').matches, page: Math.max(0, Array.from(document.querySelectorAll('.stage-deck > .deck-panel')).indexOf(active!)), hero: !!active?.querySelector('.home-featured,.detail-overview-page') };
      setRailState(old => old.present === next.present && old.expanded === next.expanded && old.dimmed === next.dimmed && old.page === next.page && old.hero === next.hero ? old : next);
    };
    const observer = new MutationObserver(sync);
    observer.observe(document.getElementById('root')!, { childList: true, subtree: true, attributes: true, attributeFilter: ['class', 'data-active'] });
    const depth = (event: Event) => {
      const direction = (event as CustomEvent<{ direction: number }>).detail?.direction ?? 0;
      transition.current = { direction, at: performance.now(), outgoing: root.current?.querySelector<HTMLElement>('.background-layer:last-child')?.dataset.backgroundKey ?? '' };
      if (!direction || matchMedia('(prefers-reduced-motion:reduce)').matches) return;
      root.current?.querySelectorAll<HTMLElement>('.background-layer:last-child,.background-preview.is-visible').forEach(layer => {
        layer.getAnimations().filter(animation => animation.id === 'goby-depth').forEach(animation => animation.cancel());
        const animation = layer.animate([{ transform: 'scale(1)' }, { transform: `scale(${direction > 0 ? 1.16 : 1.05})` }], { duration: 1000, easing: 'cubic-bezier(.45,0,.25,1)', composite: 'add' });
        animation.id = 'goby-depth';
      });
    };
    document.addEventListener('goby:page', depth); window.addEventListener('resize', sync); sync();
    return () => { observer.disconnect(); document.removeEventListener('goby:page', depth); window.removeEventListener('resize', sync); };
  }, []);
  useEffect(() => {
    const layer = root.current?.querySelector<HTMLElement>('.background-layer:last-child');
    const { direction, at, outgoing } = transition.current;
    if (layer && direction && layer.dataset.backgroundKey !== outgoing && performance.now() - at < 2500 && !matchMedia('(prefers-reduced-motion:reduce)').matches) {
      layer.getAnimations().filter(animation => animation.id === 'goby-depth').forEach(animation => animation.cancel());
      const animation = layer.animate([{ transform: `scale(${direction > 0 ? 1.08 : 1.18})` }, { transform: 'scale(1)' }], { duration: 1300, easing: 'cubic-bezier(.16,1,.3,1)', composite: 'add' });
      animation.id = 'goby-depth';
      transition.current.direction = 0;
    }
  }, [layers]);
  useEffect(() => {
    let alive = true;
    if (!src) { setLayers([]); return; }
    const image = new Image(); image.crossOrigin = 'anonymous';
    image.onload = () => { if (alive) setLayers(old => old.at(-1)?.src === src ? old : [...old.slice(-1), { src, key: Date.now() }]); };
    image.onerror = () => { if (alive) setLayers([]); };
    image.src = src;
    return () => { alive = false; };
  }, [src]);
  useEffect(() => {
    for (const variable of ['--accent', '--accent-soft', '--accent-glow', '--accent-line']) document.documentElement.style.removeProperty(variable);
    if (!poster) return;
    let alive = true;
    const image = new Image(); image.crossOrigin = 'anonymous'; image.onload = () => { if (alive) applyAccent(image); }; image.src = poster;
    return () => { alive = false; };
  }, [poster]);
  const hero = mode === 'stage' && railState.hero;
  return <div className={`background-stage mode-${mode}${hero ? ' is-hero' : ''}`} ref={root} aria-hidden="true"><div className="background-images" style={mode === 'blur' ? { transform: `translate3d(0, ${-railState.page * 1.6}%, 0)` } : undefined}>{layers.map(layer => <div className="background-layer" data-background-key={layer.key} key={layer.key} style={{ backgroundImage: `url("${layer.src}")` }}/>)}</div><ThemePreview item={item} enabled={mode === 'stage' && /^(home|detail)\//.test(routeKey)} routeKey={routeKey}/><div className="background-scrim"/><div className="background-vignette"/><div className="background-ambient"/><div className="background-glow"/><div className="background-rail-gradient" style={{ opacity: railState.present ? railState.expanded ? 1 : .3 : 0 }}/><div className="background-rail-dim" style={{ opacity: railState.dimmed ? .42 : 0 }}/><div className="background-focus"/></div>;
}

function Navigation({ route, navigate, session }: { route: Route; navigate: (route: Route) => void; session: AuthSession }) {
  const links = [{ page: 'home', name: '首页', icon: 'home' }, { page: 'movies', name: '电影', icon: 'movie' }, { page: 'series', name: '剧集', icon: 'series' }, { page: 'favorites', name: '收藏', icon: 'heart' }] as const;
  const index = links.findIndex(link => link.page === route.page);
  const [scrolled, setScrolled] = useState(false);
  useEffect(() => {
    const update = () => setScrolled((window.scrollY || document.scrollingElement?.scrollTop || 0) > 40);
    update(); window.addEventListener('scroll', update, { passive: true });
    return () => window.removeEventListener('scroll', update);
  }, [route.page, route.id]);
  const goBack = () => { if (history.state?.goby) history.back(); else navigate({ page: 'home' }); };
  return <><div className={`top-scrim ${scrolled ? 'is-visible' : ''}`}/>{route.page === 'detail' && <button className="back-button" data-chrome aria-label="返回" onClick={goBack}><Icon name="chevronLeft"/></button>}<nav className="top-nav" data-chrome aria-label="主导航"><span className="nav-indicator" style={{ transform: `translateX(${Math.max(0, index) * 78}px)`, opacity: index < 0 ? 0 : 1 }}/>{links.map(link => <button key={link.page} className={`nav-tab ${route.page === link.page ? 'is-active' : ''}`} aria-current={route.page === link.page ? 'page' : undefined} onClick={() => navigate({ page: link.page })}><Icon name={link.icon}/><span>{link.name}</span></button>)}<span className="nav-divider"/><div className="nav-tools"><button className={route.page === 'search' ? 'is-active' : ''} title="搜索（/）" aria-label="搜索" onClick={() => navigate({ page: 'search' })}><Icon name="search" size={20}/></button><button className={route.page === 'settings' ? 'is-active' : ''} title="个人中心" aria-label="个人中心" onClick={() => navigate({ page: 'settings' })}><span className="avatar">{session.User.Name.slice(0, 1)}</span></button></div></nav><div className="mobile-tools"><button aria-label="搜索" title="搜索（/）" onClick={() => navigate({ page: 'search' })}><Icon name="search" size={20}/></button><button className={route.page === 'settings' ? 'is-active' : ''} aria-label="个人中心" title="个人中心" onClick={() => navigate({ page: 'settings' })}><span className="avatar">{session.User.Name.slice(0, 1)}</span></button></div></>;
}

export function App() {
  const [session, setSession] = useState(() => api.getSession());
  const [route, setRoute] = useState<Route>(readRoute);
  const [prefs, updatePrefs] = useState(getPreferences);
  const [revision, setRevision] = useState(0);
  const [toast, setToast] = useState('');
  const [backdrop, updateBackdrop] = useState<{ item: MediaItem | null; accentItem: MediaItem | null; mode: 'stage' | 'blur' | 'none' }>({ item: null, accentItem: null, mode: 'none' });
  const [previewItem, setPreviewItem] = useState<MediaItem | null>(null);
  const [externalLaunch, setExternalLaunch] = useState<ExternalLaunch | null>(null);
  const playAttempt = useRef(0);
  const routeKey = `${route.page}/${route.id ?? ''}`;
  const notify = useCallback((message: string) => setToast(message), []);
  const changed = useCallback(() => setRevision(value => value + 1), []);
  const setBackdrop = useCallback((item: MediaItem | null, mode: 'stage' | 'blur' | 'none' = 'stage', accentItem: MediaItem | null = item) => updateBackdrop({ item, accentItem, mode }), []);
  const navigate = useCallback((next: Route) => {
    const hash = `#/${next.page}${next.id ? `/${encodeURIComponent(next.id)}` : ''}`;
    if (location.hash !== hash) history.pushState({ goby: true }, '', hash);
    saveRoute(next);
    setRoute(next); window.scrollTo(0, 0);
  }, []);
  const logout = useCallback(async () => { try { await api.logout(); } finally { setSession(null); setRoute({ page: 'home' }); history.replaceState({}, '', '#/home'); updateBackdrop({ item: null, accentItem: null, mode: 'none' }); } }, []);
  useEffect(() => { const pop = () => { setRoute(readRoute()); window.scrollTo(0, 0); }; window.addEventListener('popstate', pop); return () => window.removeEventListener('popstate', pop); }, []);
  useEffect(() => { if (!toast) return; const timer = setTimeout(() => setToast(''), 3500); return () => clearTimeout(timer); }, [toast]);
  useEffect(() => { setPreviewItem(null); }, [routeKey]);
  useEffect(() => { playAttempt.current += 1; setExternalLaunch(null); }, [routeKey, session?.AccessToken]);
  useEffect(() => { if (!session) return; let alive = true; updatePrefs(getPreferences()); loadPreferences().then(value => { if (alive) updatePrefs(value); }).catch(() => notify('暂时无法同步设置，已使用此设备保存的设置。')); return () => { alive = false; }; }, [session?.User.Id, notify]);
  useEffect(() => { const sync = () => { const next = api.getSession(); if (!next) { setSession(null); updateBackdrop({ item: null, accentItem: null, mode: 'none' }); } }; window.addEventListener('goby:session', sync); return () => window.removeEventListener('goby:session', sync); }, []);
  useEffect(() => { const key = (event: KeyboardEvent) => { if (event.key === '/' && !(event.target as HTMLElement).closest('input,textarea,select,[contenteditable]') && route.page !== 'player') { event.preventDefault(); navigate({ page: 'search' }); } }; document.addEventListener('keydown', key); return () => document.removeEventListener('keydown', key); }, [navigate, route.page]);
  useStageFocus(routeKey);
  if (!session) return <ErrorBoundary><Login onAuthenticated={value => { setSession(value); navigate({ page: 'home' }); }}/>{toast && <div className="toast" role="status">{toast}</div>}</ErrorBoundary>;
  const setPrefs = (patch: Partial<typeof prefs>) => { updatePrefs(current => ({ ...current, ...patch })); setPreferences(patch).catch(() => notify('设置已保存在此设备，服务器同步失败。')); };
  const play = async (item: MediaItem, restart = false) => {
    const attempt = ++playAttempt.current;
    const owner = session.AccessToken;
    const current = () => attempt === playAttempt.current && api.getSession()?.AccessToken === owner;
    setExternalLaunch(null);
    try {
      let playable = item;
      if (item.Type === 'Series') { const next = await api.nextUp(item.Id); playable = next.Items[0] ?? (await api.episodes(item.Id)).Items[0]; if (!playable) { notify('这部剧还没有可播放的剧集。'); return; } }
      await loadItemPreferences(playable.SeriesId || playable.Id).catch(() => undefined);
      if (!current()) return;
      const option = effectivePlayer(getItemPreferences(playable.SeriesId || playable.Id).player, prefs.player);
      const choice = option.id;
      if (choice !== 'goby') {
        const session = await api.negotiatePlayback(playable.Id, { external: true, startTimeTicks: restart ? 0 : playable.UserData?.PlaybackPositionTicks });
        if (!current()) return;
        const url = externalPlayerUrl(choice, session.url, playable.Name);
        setExternalLaunch({ name: option.name, launchUrl: url, streamUrl: session.url });
        if (url) window.location.href = url;
      } else navigate({ page: 'player', id: playable.Id, restart });
    } catch { if (current()) notify('暂时无法开始播放，请检查媒体和服务器连接。'); }
  };
  return <ErrorBoundary><AppContext.Provider value={{ session, prefs, setPrefs, navigate, play, notify, revision, changed, setBackdrop, logout }}><PreviewSoundProvider><PosterPreviewProvider routeKey={routeKey} onPreviewChange={setPreviewItem}><Background item={previewItem ?? backdrop.item} accentItem={previewItem ?? (['home', 'detail', 'player'].includes(route.page) ? backdrop.accentItem : null)} mode={previewItem ? 'blur' : backdrop.mode} routeKey={routeKey}/>{route.page !== 'player' && <Navigation route={route} navigate={navigate} session={session}/>}<main className={`app-main route-${route.page}`}><Suspense fallback={<PageState loading/>}>{route.page === 'home' ? <HomePage/> : route.page === 'detail' && route.id ? <DetailPage key={route.id} id={route.id}/> : route.page === 'player' && route.id ? <PlayerPage key={`${route.id}-${route.restart}`} id={route.id} restart={route.restart}/> : route.page === 'settings' ? <SettingsPage/> : ['movies', 'series', 'favorites', 'search'].includes(route.page) ? <CatalogPage key={route.page} kind={route.page as 'movies' | 'series' | 'favorites' | 'search'}/> : <PageState error="找不到这个页面。" onRetry={() => navigate({ page: 'home' })}/>}</Suspense></main>{externalLaunch && <ExternalLaunchNotice value={externalLaunch} close={() => setExternalLaunch(null)} notify={notify}/>} {toast && <div className="toast" role="status">{toast}</div>}</PosterPreviewProvider></PreviewSoundProvider></AppContext.Provider></ErrorBoundary>;
}

