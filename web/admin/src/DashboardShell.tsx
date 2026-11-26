import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { ComponentType, MouseEvent, ReactNode } from 'react';
import type { OverviewResponse, User } from './api';
import { Brand } from './components';
import { AttentionChecks } from './AttentionChecks';
import { dashboardTargetURL, pageURL } from './dashboardNavigation';
import type { DashboardTarget, Page } from './dashboardNavigation';
import type { DashboardStatus } from './useDashboardStatus';
import { latestSuccessfulRead, sidebarStatus } from './dashboardServerStatus';
import { translateText, useAdminPreferences, useTranslation } from './i18n';
import { BackupOutlined, CloseRounded, CollectionsBookmarkRounded, MonitorHeartOutlined, SpaceDashboardOutlined, LightModeOutlined, DarkModeOutlined, LogoutRounded, MenuRounded, NotificationsOutlined, PeopleOutlineRounded, PlaylistAddCheckRounded, RefreshRounded, SearchRounded, SpeedOutlined, SubtitlesRounded, TuneRounded, VpnKeyOutlined, VideoLibraryOutlined, AudioWaveformOutlined, NavigateNextRounded, WarningAmberRounded } from './icons';
import type { IconProps } from './icons';
import './shell.css';

type Destination = { page: Page; label: string; icon: ComponentType<IconProps>; count?: keyof OverviewResponse['Counts'] };
const sections: { label?: string; items: Destination[] }[] = [
  { items: [{ page: 'overview', label: 'Overview', icon: SpaceDashboardOutlined }] },
  { label: 'Media', items: [{ page: 'libraries', label: 'Libraries', icon: VideoLibraryOutlined, count: 'Libraries' }, { page: 'collections', label: 'Collections', icon: CollectionsBookmarkRounded }, { page: 'media-analysis', label: 'Media analysis', icon: AudioWaveformOutlined }] },
  { label: 'Access', items: [{ page: 'users', label: 'Users', icon: PeopleOutlineRounded, count: 'Users' }, { page: 'api-keys', label: 'API keys', icon: VpnKeyOutlined }] },
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
interface ShellProps {
  user: User;
  page: Page;
  libraryId?: string;
  status: DashboardStatus;
  onAttentionVisibilityChange: (open: boolean) => void;
  onNavigate: (target: DashboardTarget, event?: MouseEvent<HTMLAnchorElement>) => void;
  onSignOut: () => void;
  signingOut: boolean;
  navigationBusy?: boolean;
  children: ReactNode;
}

export function DashboardShell({ user, page, libraryId, status, onAttentionVisibilityChange, onNavigate, onSignOut, signingOut, navigationBusy = false, children }: ShellProps) {
  const { t, locale } = useTranslation();
  const { theme, setTheme, language, setLanguage, toggleTheme } = useAdminPreferences();
  const [navOpen, setNavOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [selection, setSelection] = useState(0);
  const [menu, setMenu] = useState<'account' | 'attention' | null>(null);
  const { attention } = status;
  const overview = status.overview.data;
  const system = status.system.data;
  const serverStatus = sidebarStatus(status.overview, status.system);
  // Other pages do not publish their own read times to the shell. A successful
  // global status check is not a successful load of the page's actual data.
  const updatedAt = page === 'overview' ? latestSuccessfulRead(status.overview, status.system, status.directories, status.backup, status.tasks, status.jobs, status.libraries, status.settings) : undefined;
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
  const libraryHeading = status.libraries.data?.Items.find(({ Id }) => Id === libraryId)?.Name;
  const breadcrumbTitle = page === 'metadata' ? libraryHeading ?? t('Library items') : t(label);
  const attentionCount = attention.entries.length;
  const checksIncomplete = attention.checks.length > 0;
  const attentionOpen = menu === 'attention';
  useEffect(() => onAttentionVisibilityChange(attentionOpen), [attentionOpen, onAttentionVisibilityChange]);

  useEffect(() => {
    document.title = `${breadcrumbTitle} · Goby`;
  }, [breadcrumbTitle]);

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

  function navigate(target: DashboardTarget, event?: MouseEvent<HTMLAnchorElement>) {
    if (navigationBusy && target.page !== page) { event?.preventDefault(); return; }
    onNavigate(target, event);
    if (!event || !(event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0)) { setNavOpen(false); setMenu(null); setPaletteOpen(false); }
  }
  const commands = destinations.map((item) => ({ ...item, category: 'Pages', run: () => navigate({ page: item.page }) })).concat([
    { page: 'overview', label: 'Toggle theme', icon: theme === 'dark' ? LightModeOutlined : DarkModeOutlined, category: 'Commands', run: () => { toggleTheme(); setPaletteOpen(false); } },
    { page: 'overview', label: 'Sign out', icon: LogoutRounded, category: 'Commands', run: () => { setPaletteOpen(false); onSignOut(); } },
  ]);
  const matches = commands.filter((item) => {
    const aliases = item.page === 'users' ? ['devices', 'sessions', 'logins', 'sign-ins', 'login', translateText('Devices', 'zh'), translateText('Sessions', 'zh'), translateText('Sign in', 'zh')] : [];
    return [item.label, t(item.label), t(item.category), ...aliases].join(' ').toLocaleLowerCase(locale).includes(query.trim().toLocaleLowerCase(locale));
  });

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
        const count = item.count && serverStatus.complete ? overview?.Counts[item.count] : undefined;
        return <a key={item.page} href={pageURL(item.page)} onClick={(event) => navigate({ page: item.page }, event)} aria-current={selected === item.page ? 'page' : undefined} aria-disabled={navigationBusy && selected !== item.page || undefined}><Icon /><span>{t(item.label)}</span>{count !== undefined && <span className="nav-count">{new Intl.NumberFormat(locale).format(count)}</span>}</a>;
      })}</div>)}</nav>
      <div className="sidebar-server" data-state={serverStatus.state}>
        <strong><i className={`status-dot${serverStatus.state === 'failed' ? ' is-stale' : ''}`} aria-hidden="true" />
          {serverStatus.state === 'checking' ? t('Checking server status…') : overview?.Server.Name ? <span translate="no">{overview.Server.Name}</span> : t('Goby server')}
        </strong>
        {serverStatus.state !== 'checking' && (overview || system) && <span>{overview && `goby ${overview.Server.Version}`}{overview && system && ' · '}{system && `${t('Up')} ${Math.floor(system.UptimeSeconds / 86400).toLocaleString(locale)}${t('d')}`}</span>}
        {serverStatus.state === 'ready' && <span className="visually-hidden">{t('Server status checked')}</span>}
        {serverStatus.state === 'failed' && <span role="status">{serverStatus.retainedAt === undefined ? t('Server status refresh failed') : t('Refresh failed · showing results from {time}', {
          time: new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(serverStatus.retainedAt),
        })}</span>}
      </div>
    </aside>
    {navOpen && <button className="navigation-scrim" aria-label={t('Close navigation')} tabIndex={-1} onClick={() => setNavOpen(false)} />}
    <div className="admin-workspace" inert={navOpen || undefined}>
      <header className="admin-header">
        <button ref={navTrigger} className="shell-icon nav-toggle" onClick={() => { setNavOpen(true); setMenu(null); }} aria-label={t('Open navigation')} aria-controls="admin-navigation" aria-expanded={navOpen}><MenuRounded /></button>
        <div className="header-breadcrumb">{(page === 'metadata' || page === 'artwork') && <><a href={pageURL('libraries')} onClick={(event) => navigate({ page: 'libraries' }, event)}>{t('Libraries')}</a><NavigateNextRounded /></>}<strong>{breadcrumbTitle}</strong></div>
        <div className="header-spacer" />
        {updatedAt !== undefined && <time className="header-updated" dateTime={new Date(updatedAt).toISOString()}>{t('Data updated at {time}', {
          time: new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' }).format(updatedAt),
        })}</time>}
        <button className="shell-icon header-theme" aria-label={t('Toggle theme')} title={t('Toggle theme')} onClick={toggleTheme}>{theme === 'dark' ? <DarkModeOutlined /> : <LightModeOutlined />}</button>
        <button ref={attentionTrigger} className="shell-icon attention-trigger" aria-label={t('Needs attention')} aria-expanded={menu === 'attention'} aria-controls={menu === 'attention' ? 'attention-menu' : undefined} onClick={() => setMenu(menu === 'attention' ? null : 'attention')}><NotificationsOutlined />{(attentionCount > 0 || attention.checks.some((check) => check.state === 'failed' || check.state === 'unknown' && !['task-status', 'scan-status', 'library-status'].includes(check.id))) && <i aria-hidden="true" />}</button>
        <button ref={accountTrigger} className="account-avatar" aria-label={`${t('Account')}: ${user.Name}`} aria-haspopup="menu" aria-expanded={menu === 'account'} aria-controls={menu === 'account' ? 'account-menu' : undefined} onClick={() => setMenu(menu === 'account' ? null : 'account')}>{user.Name.trim().slice(0, 1).toLocaleUpperCase(locale)}</button>
        {menu === 'account' && <div ref={popup} className="header-popup account-popup" id="account-menu" role="menu" aria-label={t('Account')}>
          <div className="account-identity"><span className="account-avatar">{user.Name.slice(0, 1).toLocaleUpperCase(locale)}</span><div><strong>{user.Name}</strong><span>{t('Administrator')} · {overview?.Server.Name ?? 'Goby'}</span></div></div>
          <div className="account-preferences"><span>{t('Language')}</span><div className="small-segment"><button role="menuitemradio" aria-checked={language === 'zh'} aria-label="中文" onClick={() => setLanguage('zh')}>中</button><button role="menuitemradio" aria-checked={language === 'en'} aria-label="English" onClick={() => setLanguage('en')}>EN</button></div><span>{t('Appearance')}</span><div className="small-segment"><button role="menuitemradio" aria-checked={theme === 'dark'} onClick={() => setTheme('dark')}>{t('Dark')}</button><button role="menuitemradio" aria-checked={theme === 'light'} onClick={() => setTheme('light')}>{t('Light')}</button></div></div>
          <button role="menuitem" className="account-signout" disabled={signingOut} onClick={() => { setMenu(null); onSignOut(); }}><LogoutRounded />{t(signingOut ? 'Signing out…' : 'Sign out')}</button>
        </div>}
        {menu === 'attention' && <div ref={popup} className="header-popup attention-popup" id="attention-menu" role="dialog" aria-label={t('Needs attention')} aria-busy={attention.loading}>
          <h2>{t('Needs attention')} <span>{attentionCount.toLocaleString(locale)}</span></h2>
          {attention.entries.map((entry) => {
            const content = <><WarningAmberRounded /><span><strong>{entry.title}</strong>{entry.detail && <small>{entry.detail}</small>}{entry.stale && <small>{t('Stale')}</small>}</span>{entry.action && <NavigateNextRounded className="attention-arrow" />}</>;
            const attributes = { className: 'attention-entry', 'data-attention-id': entry.id, 'data-check': entry.check, 'data-severity': entry.severity, 'data-stale': entry.stale };
            const target = entry.action?.target;
            return target ? <a {...attributes} key={entry.id} href={dashboardTargetURL(target)} onClick={(event) => navigate(target, event)} aria-disabled={navigationBusy && page !== target.page || undefined}>{content}</a>
              : <div {...attributes} key={entry.id}>{content}</div>;
          })}
          <AttentionChecks checks={attention.checks} />
          {attentionCount === 0 && !checksIncomplete && <p className="attention-empty">{t('No issues reported by the available server status checks.')}</p>}
          <div className="attention-footer"><button onClick={status.refresh} disabled={attention.loading && attention.checks.every((check) => check.state === 'loading')}>{t('Refresh attention checks')}<RefreshRounded /></button><button onClick={() => navigate({ page: 'notifications' })}>{t('Notification settings')}<NavigateNextRounded /></button></div>
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
