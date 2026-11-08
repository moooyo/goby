import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { ComponentType, MouseEvent, ReactNode } from 'react';
import { adminApi, isAbortError } from './api';
import type { OverviewResponse, User } from './api';
import { backupPolicyApi } from './backupPolicyApi';
import { directoryReadAccess } from './directoryCapacity';
import { systemStatusApi } from './systemStatusApi';
import type { SystemStatus } from './systemStatusApi';
import { Brand } from './components';
import { pageURL } from './dashboardNavigation';
import type { Page } from './dashboardNavigation';
import { useAdminPreferences, useTranslation } from './i18n';
import { BackupOutlined, CloseRounded, CollectionsBookmarkRounded, MonitorHeartOutlined, SpaceDashboardOutlined, LightModeOutlined, DarkModeOutlined, LogoutRounded, MenuRounded, NotificationsOutlined, PeopleOutlineRounded, PlaylistAddCheckRounded, RefreshRounded, SearchRounded, SensorsRounded, SpeedOutlined, SubtitlesRounded, TuneRounded, VpnKeyOutlined, VideoLibraryOutlined, DesktopWindowsRounded, AudioWaveformOutlined, NavigateNextRounded, WarningAmberRounded } from './icons';
import type { IconProps } from './icons';
import './shell.css';

type Destination = { page: Page; label: string; icon: ComponentType<IconProps>; count?: keyof OverviewResponse['Counts'] };
const sections: { label?: string; items: Destination[] }[] = [
  { items: [{ page: 'overview', label: 'Overview', icon: SpaceDashboardOutlined }] },
  { label: 'Media', items: [{ page: 'libraries', label: 'Libraries', icon: VideoLibraryOutlined, count: 'Libraries' }, { page: 'collections', label: 'Collections', icon: CollectionsBookmarkRounded }, { page: 'media-analysis', label: 'Media analysis', icon: AudioWaveformOutlined }] },
  { label: 'Access', items: [{ page: 'users', label: 'Users', icon: PeopleOutlineRounded, count: 'Users' }, { page: 'devices', label: 'Devices', icon: DesktopWindowsRounded }, { page: 'sessions', label: 'Sessions', icon: SensorsRounded, count: 'ActiveSessions' }, { page: 'api-keys', label: 'API keys', icon: VpnKeyOutlined }] },
  { label: 'System', items: [{ page: 'tasks', label: 'Tasks', icon: PlaylistAddCheckRounded }, { page: 'observability', label: 'Activity & logs', icon: MonitorHeartOutlined }, { page: 'notifications', label: 'Notifications', icon: NotificationsOutlined }, { page: 'backups', label: 'Backups & recovery', icon: BackupOutlined }] },
  { label: 'Settings', items: [{ page: 'settings', label: 'General', icon: TuneRounded }, { page: 'settings-transcode', label: 'Transcoding', icon: SpeedOutlined }, { page: 'settings-metadata', label: 'Metadata & subtitles', icon: SubtitlesRounded }] },
];
const destinations = sections.flatMap((section) => section.items);
function selectedDestination(page: Page): Page {
  if (page === 'metadata' || page === 'artwork') return 'libraries';
  if (page === 'settings-hardware') return 'settings-transcode';
  if (page === 'providers') return 'settings-metadata';
  return page;
}
const focusable = 'a[href],button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),[tabindex="0"]';
const loadAttentionDirectories = (signal: AbortSignal) => adminApi.getLibraryStorageDirectories({ signal });
const loadAttentionBackupPolicy = (signal: AbortSignal) => backupPolicyApi.get({ signal });

function useAttentionResource<T>(load: (signal: AbortSignal) => Promise<T>, timeoutMessage: string) {
  const [snapshot, setSnapshot] = useState<{ data?: T; error: unknown; loading: boolean }>({ error: null, loading: true });
  const [revision, setRevision] = useState(0);
  const reload = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let pending: AbortController | undefined;
    function stop() {
      clearTimeout(timer);
      const request = pending;
      pending = undefined;
      request?.abort();
    }
    async function read() {
      if (!active || document.hidden || pending) return;
      clearTimeout(timer);
      const request = new AbortController();
      pending = request;
      setSnapshot((current) => ({ ...current, loading: true }));
      let abort: (() => void) | undefined;
      const cancelled = new Promise<never>((_resolve, reject) => {
        abort = () => reject(request.signal.reason);
        request.signal.addEventListener('abort', abort, { once: true });
      });
      const deadline = setTimeout(() => request.abort(new Error(timeoutMessage)), 15_000);
      try {
        const data = await Promise.race([load(request.signal), cancelled]);
        if (active && pending === request) setSnapshot({ data, error: null, loading: false });
      } catch (error) {
        if (active && pending === request) setSnapshot((current) => ({ ...current, error: isAbortError(error) ? current.error : error, loading: false }));
      } finally {
        clearTimeout(deadline);
        if (abort) request.signal.removeEventListener('abort', abort);
        if (pending === request) {
          pending = undefined;
          if (active && !document.hidden) timer = setTimeout(() => { void read(); }, 30_000);
        }
      }
    }
    function visibilityChanged() {
      if (document.hidden) {
        stop();
        setSnapshot((current) => ({ ...current, loading: false }));
      } else void read();
    }
    document.addEventListener('visibilitychange', visibilityChanged);
    void read();
    return () => { active = false; stop(); document.removeEventListener('visibilitychange', visibilityChanged); };
  }, [load, timeoutMessage, revision]);
  return { ...snapshot, reload };
}

interface ShellProps {
  user: User;
  page: Page;
  libraryId?: string;
  onNavigate: (page: Page, event?: MouseEvent<HTMLAnchorElement>, libraryId?: string, state?: { tasksTab: 'history' | 'available' }, taskId?: string) => void;
  onSignOut: () => void;
  signingOut: boolean;
  navigationBusy?: boolean;
  children: ReactNode;
}

export function DashboardShell({ user, page, libraryId, onNavigate, onSignOut, signingOut, navigationBusy = false, children }: ShellProps) {
  const { t, locale } = useTranslation();
  const { theme, setTheme, language, setLanguage, toggleTheme } = useAdminPreferences();
  const [navOpen, setNavOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [selection, setSelection] = useState(0);
  const [menu, setMenu] = useState<'account' | 'attention' | null>(null);
  const [overview, setOverview] = useState<OverviewResponse>();
  const [status, setStatus] = useState<SystemStatus>();
  const [restartRequired, setRestartRequired] = useState(false);
  const [stale, setStale] = useState(false);
  const directories = useAttentionResource(loadAttentionDirectories, 'Media directory checks did not respond within 15 seconds.');
  const backupPolicy = useAttentionResource(loadAttentionBackupPolicy, 'Automatic backup rules did not respond within 15 seconds.');
  const [libraryHeading, setLibraryHeading] = useState<string>();
  const [now, setNow] = useState(() => new Date());
  const nav = useRef<HTMLElement>(null);
  const navTrigger = useRef<HTMLButtonElement>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const paletteInput = useRef<HTMLInputElement>(null);
  const popup = useRef<HTMLDivElement>(null);
  const accountTrigger = useRef<HTMLButtonElement>(null);
  const attentionTrigger = useRef<HTMLButtonElement>(null);
  const selected = selectedDestination(page);
  const destination = destinations.find((item) => item.page === selected)!;
  const label = page === 'artwork' ? 'Catalog artwork' : page === 'providers' ? 'Online providers' : destination.label;
  const breadcrumbTitle = page === 'metadata' ? libraryHeading ?? t('Library items') : t(label);
  const registeredDirectories = [...new Map((directories.data?.Items ?? []).map((directory) => [directory.Path, directory])).values()];
  const unreadable = registeredDirectories.filter((directory) => directoryReadAccess(directory) === false);
  const unknownDirectories = registeredDirectories.some((directory) => directoryReadAccess(directory) == null);
  const noAutomaticBackups = backupPolicy.data?.Available === true && backupPolicy.data.Enabled === false;
  const backupUnavailable = backupPolicy.data?.Available === false;
  const checksIncomplete = !directories.data || directories.error != null || unknownDirectories || !backupPolicy.data || backupPolicy.error != null || backupUnavailable;
  const attentionCount = unreadable.length + Number(restartRequired) + Number(stale) + Number(noAutomaticBackups)
    + Number(directories.error != null || unknownDirectories) + Number(backupPolicy.error != null || backupUnavailable);

  useEffect(() => {
    document.title = `${breadcrumbTitle} · Goby`;
  }, [breadcrumbTitle]);

  useEffect(() => {
    setLibraryHeading(undefined);
    if (page !== 'metadata' || !libraryId) return;
    const controller = new AbortController();
    void adminApi.getLibraries({ signal: controller.signal }).then((result) => {
      if (!controller.signal.aborted) setLibraryHeading(result.Items.find((library) => library.Id === libraryId)?.Name);
    }).catch(() => { /* The item page reports its own load errors. */ });
    return () => controller.abort();
  }, [page, libraryId]);

  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    function scheduleRead() {
      timer = setTimeout(() => {
        if (!active) return;
        if (document.visibilityState === 'visible') void readServer();
        else scheduleRead();
      }, 30_000);
    }
    async function readServer() {
      const request = new AbortController();
      const signal = AbortSignal.any([controller.signal, request.signal]);
      const deadline = setTimeout(() => request.abort(new Error('Server status did not respond within 15 seconds.')), 15_000);
      const results = await Promise.allSettled([
        adminApi.getOverview({ signal }),
        systemStatusApi.getStatus({ signal }),
        adminApi.getSettings({ signal }),
      ]);
      clearTimeout(deadline);
      if (!active) return;
      if (results[0].status === 'fulfilled') setOverview(results[0].value);
      if (results[1].status === 'fulfilled') setStatus(results[1].value);
      if (results[2].status === 'fulfilled') setRestartRequired(Boolean(results[2].value.Runtime?.Network.RestartRequired));
      setStale(results.some((result) => result.status === 'rejected' && !isAbortError(result.reason)));
      scheduleRead();
    }
    void readServer();
    return () => { active = false; controller.abort(); clearTimeout(timer); };
  }, []);

  useEffect(() => {
    const timer = setInterval(() => { if (document.visibilityState === 'visible') setNow(new Date()); }, 1000);
    return () => clearInterval(timer);
  }, []);

  useEffect(() => {
    const desktop = window.matchMedia('(min-width: 1240px)');
    const close = () => { if (desktop.matches) setNavOpen(false); };
    desktop.addEventListener('change', close);
    return () => desktop.removeEventListener('change', close);
  }, []);

  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
        if (document.querySelector('dialog[open]:not(.command-dialog)')) return;
        event.preventDefault(); setPaletteOpen((value) => !value); setNavOpen(false); setMenu(null);
      }
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  }, []);

  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    if (paletteOpen && !element.open) {
      element.showModal(); setQuery(''); setSelection(0);
      element.querySelector<HTMLElement>('.command-results')?.scrollTo({ top: 0 });
      paletteInput.current?.focus();
    }
    if (!paletteOpen && element.open) element.close();
  }, [paletteOpen]);

  useEffect(() => {
    if (!navOpen) return;
    nav.current?.querySelector<HTMLElement>('a[aria-current="page"]')?.focus();
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); setNavOpen(false); }
      if (event.key === 'Tab') {
        const nodes = [...(nav.current?.querySelectorAll<HTMLElement>(focusable) ?? [])].filter((node) => node.getClientRects().length);
        const first = nodes[0], last = nodes.at(-1);
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
        if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
      }
    };
    window.addEventListener('keydown', key);
    return () => { window.removeEventListener('keydown', key); navTrigger.current?.focus(); };
  }, [navOpen]);

  useEffect(() => {
    if (!menu) return;
    const close = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!popup.current?.contains(target) && !accountTrigger.current?.contains(target) && !attentionTrigger.current?.contains(target)) setMenu(null);
    };
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); setMenu(null); (menu === 'account' ? accountTrigger : attentionTrigger).current?.focus(); }
      if (event.key === 'Tab') setMenu(null);
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        const items = [...(popup.current?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href]:not([aria-disabled="true"])') ?? [])];
        const index = items.indexOf(document.activeElement as HTMLElement);
        event.preventDefault(); items[(index + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length]?.focus();
      }
    };
    document.addEventListener('pointerdown', close);
    window.addEventListener('keydown', key);
    return () => { document.removeEventListener('pointerdown', close); window.removeEventListener('keydown', key); };
  }, [menu]);

  function navigate(next: Page, event?: MouseEvent<HTMLAnchorElement>, taskId?: string) {
    if (navigationBusy && next !== page) { event?.preventDefault(); return; }
    onNavigate(next, event, undefined, undefined, taskId);
    if (!event || !(event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0)) { setNavOpen(false); setMenu(null); setPaletteOpen(false); }
  }
  const commands = destinations.map((item) => ({ ...item, category: 'Pages', run: () => navigate(item.page) })).concat([
    { page: 'overview', label: 'Toggle theme', icon: theme === 'dark' ? LightModeOutlined : DarkModeOutlined, category: 'Commands', run: () => { toggleTheme(); setPaletteOpen(false); } },
    { page: 'overview', label: 'Sign out', icon: LogoutRounded, category: 'Commands', run: () => { setPaletteOpen(false); onSignOut(); } },
  ]);
  const matches = commands.filter((item) => `${item.label} ${t(item.label)} ${t(item.category)}`.toLocaleLowerCase(locale).includes(query.trim().toLocaleLowerCase(locale)));

  useLayoutEffect(() => {
    if (!paletteOpen || !dialog.current?.open || matches.length === 0) return;
    dialog.current.querySelector<HTMLElement>(`#command-${Math.min(selection, matches.length - 1)}`)?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'instant' });
  }, [paletteOpen, selection, query, language, matches.length]);

  return <div className="admin-shell" data-nav={navOpen ? 'open' : 'closed'}>
    <a className="skip-link" href="#main-content">{t('Skip to content')}</a>
    <aside ref={nav} className="admin-sidebar" id="admin-navigation" role={navOpen ? 'dialog' : undefined} aria-modal={navOpen ? true : undefined} aria-label={navOpen ? t('Navigation') : undefined}>
      <div className="sidebar-brand"><Brand /><button className="shell-icon nav-close" aria-label={t('Close navigation')} onClick={() => setNavOpen(false)}><CloseRounded /></button></div>
      <button className="sidebar-search" onClick={() => { setNavOpen(false); setPaletteOpen(true); }} aria-label={t('Search or jump to')}><SearchRounded /><span>{t('Search or jump to')}</span><kbd>⌘K</kbd></button>
      <nav aria-label="Administration">{sections.map((group, index) => <div className="nav-section" key={index}>{group.label && <div className="nav-section-label">{t(group.label)}</div>}{group.items.map((item) => {
        const Icon = item.icon;
        const count = item.count ? overview?.Counts[item.count] : undefined;
        return <a key={item.page} href={pageURL(item.page)} onClick={(event) => navigate(item.page, event)} aria-current={selected === item.page ? 'page' : undefined} aria-disabled={navigationBusy && selected !== item.page || undefined}><Icon /><span>{t(item.label)}</span>{count !== undefined && <span className="nav-count">{item.page === 'sessions' && count > 0 && <i className="status-dot"/>}{new Intl.NumberFormat(locale).format(count)}</span>}</a>;
      })}</div>)}</nav>
      <div className="sidebar-server"><strong><i className={`status-dot ${stale ? 'is-stale' : ''}`}/>{overview?.Server.Name ?? t('Goby server')}</strong><span>goby {overview?.Server.Version ?? '—'}{status && ` · ${t('Up')} ${new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(Math.floor(status.UptimeSeconds / 86400))}${t('d')}`}{stale && ` · ${t('Last update')}`}</span></div>
    </aside>
    {navOpen && <button className="navigation-scrim" aria-label={t('Close navigation')} tabIndex={-1} onClick={() => setNavOpen(false)} />}
    <div className="admin-workspace" inert={navOpen || undefined}>
      <header className="admin-header">
        <button ref={navTrigger} className="shell-icon nav-toggle" onClick={() => { setNavOpen(true); setMenu(null); }} aria-label={t('Open navigation')} aria-controls="admin-navigation" aria-expanded={navOpen}><MenuRounded /></button>
        <div className="header-breadcrumb">{(page === 'metadata' || page === 'artwork') && <><a href={pageURL('libraries')} onClick={(event) => navigate('libraries', event)}>{t('Libraries')}</a><NavigateNextRounded /></>}<strong>{breadcrumbTitle}</strong></div>
        <div className="header-spacer" />
        {page === 'overview' && <span className="header-clock">{t('Updates every 5 s')} · {new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(now)}</span>}
        <button className="shell-icon header-theme" aria-label={t('Toggle theme')} title={t('Toggle theme')} onClick={toggleTheme}>{theme === 'dark' ? <DarkModeOutlined /> : <LightModeOutlined />}</button>
        <button ref={attentionTrigger} className="shell-icon attention-trigger" aria-label={t('Needs attention')} aria-expanded={menu === 'attention'} aria-controls={menu === 'attention' ? 'attention-menu' : undefined} onClick={() => setMenu(menu === 'attention' ? null : 'attention')}><NotificationsOutlined />{attentionCount > 0 && <i />}</button>
        <button ref={accountTrigger} className="account-avatar" aria-label={`${t('Account')}: ${user.Name}`} aria-haspopup="menu" aria-expanded={menu === 'account'} aria-controls={menu === 'account' ? 'account-menu' : undefined} onClick={() => setMenu(menu === 'account' ? null : 'account')}>{user.Name.trim().slice(0, 1).toLocaleUpperCase(locale)}</button>
        {menu === 'account' && <div ref={popup} className="header-popup account-popup" id="account-menu" role="menu" aria-label={t('Account')}>
          <div className="account-identity"><span className="account-avatar">{user.Name.slice(0, 1).toLocaleUpperCase(locale)}</span><div><strong>{user.Name}</strong><span>{t('Administrator')} · {overview?.Server.Name ?? 'Goby'}</span></div></div>
          <div className="account-preferences"><span>{t('Language')}</span><div className="small-segment"><button role="menuitemradio" aria-checked={language === 'zh'} aria-label="中文" onClick={() => setLanguage('zh')}>中</button><button role="menuitemradio" aria-checked={language === 'en'} aria-label="English" onClick={() => setLanguage('en')}>EN</button></div><span>{t('Appearance')}</span><div className="small-segment"><button role="menuitemradio" aria-checked={theme === 'dark'} onClick={() => setTheme('dark')}>{t('Dark')}</button><button role="menuitemradio" aria-checked={theme === 'light'} onClick={() => setTheme('light')}>{t('Light')}</button></div></div>
          <button role="menuitem" className="account-signout" disabled={signingOut} onClick={() => { setMenu(null); onSignOut(); }}><LogoutRounded />{t(signingOut ? 'Signing out…' : 'Sign out')}</button>
        </div>}
        {menu === 'attention' && <div ref={popup} className="header-popup attention-popup" id="attention-menu" role="dialog" aria-label={t('Needs attention')} aria-busy={directories.loading || backupPolicy.loading}>
          <h2>{t('Needs attention')} <span>{attentionCount}</span></h2>
          {unreadable.map((directory) => <button className="attention-entry" key={directory.Path} data-check="directory" data-stale={directories.error != null} onClick={() => navigate('libraries')}><WarningAmberRounded /><span><strong>{t('Media directory cannot be read')}</strong><code>{directory.Path}</code></span><NavigateNextRounded /></button>)}
          {restartRequired && <button className="attention-entry" onClick={() => navigate('settings')}><RefreshRounded /><span><strong>{t('Settings require a restart')}</strong><small>{t('Saved network settings are not active yet.')}</small></span><NavigateNextRounded /></button>}
          {noAutomaticBackups && <a className="attention-entry" data-check="backup-rules" data-stale={backupPolicy.error != null} href={`${pageURL('tasks')}${backupPolicy.data!.TaskId ? `?task=${encodeURIComponent(backupPolicy.data!.TaskId)}` : ''}`} onClick={(event) => navigate('tasks', event, backupPolicy.data!.TaskId || undefined)} aria-disabled={navigationBusy && page !== 'tasks' || undefined}><BackupOutlined /><span><strong>{t('No automatic backup rules')}</strong><small>{t('The backup task has no active trigger rules. Backups must be started manually.')}</small></span><NavigateNextRounded /></a>}
          {stale && <div className="attention-entry"><WarningAmberRounded /><span><strong>{t('Server status could not be refreshed')}</strong><small>{t('Showing the last successful update.')}</small></span></div>}
          {directories.error != null && <div className="attention-entry" data-check="directory-status" data-stale={!!directories.data} role="status"><WarningAmberRounded /><span><strong>{t('Media directory checks unavailable')}</strong><small>{t(directories.data ? 'Showing the last successful directory checks.' : 'Media directory read access is unknown.')}</small></span></div>}
          {directories.error == null && unknownDirectories && <div className="attention-entry" data-check="directory-status" role="status"><WarningAmberRounded /><span><strong>{t('Media directory read access is unknown.')}</strong><small>{t('Some registered directories have not been checked.')}</small></span></div>}
          {(backupPolicy.error != null || backupUnavailable) && <div className="attention-entry" data-check="backup-status" data-stale={backupPolicy.error != null && !!backupPolicy.data} role="status"><WarningAmberRounded /><span><strong>{t('Automatic backup rules check unavailable')}</strong><small>{t(backupPolicy.error != null && backupPolicy.data ? 'Showing the last successful backup rules check.' : backupUnavailable ? 'The automatic backup service is unavailable.' : 'Automatic backup trigger rules are unknown.')}</small></span></div>}
          {!directories.data && directories.loading && <p className="attention-check-status" role="status">{t('Checking registered media directories…')}</p>}
          {!backupPolicy.data && backupPolicy.loading && <p className="attention-check-status" role="status">{t('Checking automatic backup rules…')}</p>}
          {attentionCount === 0 && !checksIncomplete && <p className="attention-empty">{t('No issues reported by the available server status checks.')}</p>}
          <div className="attention-footer"><button onClick={() => { directories.reload(); backupPolicy.reload(); }} disabled={directories.loading || backupPolicy.loading}>{t('Refresh attention checks')}<RefreshRounded /></button><button onClick={() => navigate('notifications')}>{t('Notification settings')}<NavigateNextRounded /></button></div>
        </div>}
      </header>
      {children}
    </div>
    <dialog ref={dialog} className="command-dialog" aria-label={t('Search or jump to')} onCancel={() => setPaletteOpen(false)} onClick={(event) => { if (event.target === event.currentTarget) setPaletteOpen(false); }}>
      <div className="command-search"><SearchRounded /><input ref={paletteInput} value={query} aria-label={t('Search pages and commands')} placeholder={t('Search pages and commands…')} role="combobox" aria-expanded="true" aria-autocomplete="list" aria-controls="command-results" aria-activedescendant={matches.length ? `command-${Math.min(selection, matches.length - 1)}` : undefined} onChange={(event) => { setQuery(event.target.value); setSelection(0); }} onKeyDown={(event) => {
        if (event.nativeEvent.isComposing || event.keyCode === 229) return;
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); setSelection((value) => matches.length ? (value + (event.key === 'ArrowDown' ? 1 : -1) + matches.length) % matches.length : 0); }
        if (event.key === 'Enter' && matches.length) { event.preventDefault(); matches[Math.min(selection, matches.length - 1)].run(); }
      }} /><button className="shell-icon" aria-label={t('Close search')} onClick={() => setPaletteOpen(false)}><CloseRounded /></button></div>
      <div className="command-results" id="command-results" role="listbox" aria-label={t('Pages and commands')}>{matches.length ? matches.map((command, index) => <button id={`command-${index}`} role="option" aria-selected={selection === index} key={`${command.category}-${command.label}`} onMouseMove={() => setSelection(index)} onClick={command.run}><command.icon /><span>{t(command.label)}</span><small>{t(command.category)}</small>{selection === index && <kbd>↵</kbd>}</button>) : <p>{t('No matching pages or commands.')}</p>}</div>
      <footer><span><kbd>↑</kbd><kbd>↓</kbd> {t('Navigate')}</span><span><kbd>↵</kbd> {t('Open')}</span><span><kbd>esc</kbd> {t('Close')}</span></footer>
    </dialog>
  </div>;
}
