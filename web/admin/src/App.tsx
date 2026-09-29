import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react';
import type { MouseEvent } from 'react';
import { Avatar, Box, Button, ButtonBase, Divider, Menu, MenuItem, Paper, Skeleton, Stack, Tab, Tabs, Typography } from '@mui/material';
import SpaceDashboardOutlined from '@mui/icons-material/SpaceDashboardOutlined';
import SpaceDashboardRounded from '@mui/icons-material/SpaceDashboardRounded';
import VideoLibraryOutlined from '@mui/icons-material/VideoLibraryOutlined';
import VideoLibraryRounded from '@mui/icons-material/VideoLibraryRounded';
import PeopleOutlineRounded from '@mui/icons-material/PeopleOutlineRounded';
import PeopleRounded from '@mui/icons-material/PeopleRounded';
import MonitorHeartOutlined from '@mui/icons-material/MonitorHeartOutlined';
import MonitorHeartRounded from '@mui/icons-material/MonitorHeartRounded';
import SettingsOutlined from '@mui/icons-material/SettingsOutlined';
import SettingsRounded from '@mui/icons-material/SettingsRounded';
import LogoutRounded from '@mui/icons-material/LogoutRounded';
import { adminApi, ApiError, clearSession, isAbortError, onSessionExpired } from './api';
import type { User } from './api';
import { Brand, ErrorNotice, LoadingView } from './components';
import { colors } from './theme';
import type { UserNavigationGuard } from './userDraftNavigation';
import { groups, groupForPage, metadataLibraryFromLocation, pageFromLocation, pageURL, readLastTabs, settingsSection } from './dashboardNavigation';
import type { Group, LastTabs, Page } from './dashboardNavigation';
const AuthPage = lazy(() => import('./AuthPage').then((module) => ({ default: module.AuthPage })));
const OverviewPage = lazy(() => import('./OverviewPage').then((module) => ({ default: module.OverviewPage })));
const UsersPage = lazy(() => import('./UsersPage').then((module) => ({ default: module.UsersPage })));
const LibrariesPage = lazy(() => import('./LibrariesPage').then((module) => ({ default: module.LibrariesPage })));
const TasksPage = lazy(() => import('./TasksPage').then((module) => ({ default: module.TasksPage })));
const SessionsPage = lazy(() => import('./SessionsPage').then((module) => ({ default: module.SessionsPage })));
const DevicesPage = lazy(() => import('./DevicesPage').then((module) => ({ default: module.DevicesPage })));
const ApiKeysPage = lazy(() => import('./ApiKeysPage').then((module) => ({ default: module.ApiKeysPage })));
const SettingsPage = lazy(() => import('./SettingsPage').then((module) => ({ default: module.SettingsPage })));
const ObservabilityPage = lazy(() => import('./ObservabilityPage').then((module) => ({ default: module.ObservabilityPage })));
const BackupsPage = lazy(() => import('./BackupsPage').then((module) => ({ default: module.BackupsPage })));
const MetadataItemsPage = lazy(() => import('./MetadataItemsPage').then((module) => ({ default: module.MetadataItemsPage })));
const ProvidersPage = lazy(() => import('./ProvidersPage').then((module) => ({ default: module.ProvidersPage })));
const CollectionsPage = lazy(() => import('./CollectionsPage').then((module) => ({ default: module.CollectionsPage })));
const CatalogArtworkPage = lazy(() => import('./CatalogArtworkPage').then((module) => ({ default: module.CatalogArtworkPage })));
const NotificationsPage = lazy(() => import('./NotificationsPage').then((module) => ({ default: module.NotificationsPage })));
const MediaAnalysisPage = lazy(() => import('./MediaAnalysisPage').then((module) => ({ default: module.MediaAnalysisPage })));

type AppState =
  | { mode: 'loading' }
  | { mode: 'error'; error: unknown }
  | { mode: 'setup' }
  | { mode: 'login'; name?: string; notice?: string }
  | { mode: 'ready'; user: User };
const groupIcons = {
  overview: [SpaceDashboardOutlined, SpaceDashboardRounded],
  media: [VideoLibraryOutlined, VideoLibraryRounded],
  access: [PeopleOutlineRounded, PeopleRounded],
  system: [MonitorHeartOutlined, MonitorHeartRounded],
  settings: [SettingsOutlined, SettingsRounded],
};

function Navigation({ selected, lastTabs, navigate }: { selected: Group; lastTabs: LastTabs; navigate: (page: Page, event?: MouseEvent<HTMLAnchorElement>) => void }) {
  return <Box component="nav" aria-label="Administration" sx={{ display: 'flex', flexDirection: { xs: 'row', md: 'column' }, alignItems: 'center', flexShrink: 0, width: { xs: '100%', md: 88 }, pt: { xs: 0, md: 2.5 }, pb: { xs: 'env(safe-area-inset-bottom)', md: 0 }, gap: { xs: 0, md: 1 }, bgcolor: colors.canvas }}>
    <Box aria-label="Goby" sx={{ display: { xs: 'none', md: 'grid' }, placeItems: 'center', width: 44, height: 44, borderRadius: '14px', bgcolor: colors.primary, color: 'white', fontSize: 20, fontWeight: 700, mb: 1.5 }}>G</Box>
    {groups.map((group) => {
      const active = selected === group.id;
      const Icon = groupIcons[group.id][active ? 1 : 0];
      return <ButtonBase key={group.id} component="a" href={pageURL(lastTabs[group.id])} aria-current={active ? 'page' : undefined} onClick={(event: MouseEvent<HTMLAnchorElement>) => navigate(lastTabs[group.id], event)} sx={{ minWidth: 0, width: { xs: '20%', md: 80 }, minHeight: 64, display: 'flex', flexDirection: 'column', gap: 0.5, py: 1, borderRadius: 2, color: colors.muted, '&:hover .rail-indicator': { bgcolor: active ? '#C7D8F5' : 'rgba(24,28,35,.08)' }, '&.Mui-focusVisible': { outline: `3px solid ${colors.primary}`, outlineOffset: -3 } }}>
        <Box className="rail-indicator" sx={{ width: 56, height: 32, borderRadius: '16px', display: 'grid', placeItems: 'center', bgcolor: active ? colors.secondaryContainer : 'transparent', color: active ? colors.deep : colors.muted }}><Icon sx={{ fontSize: 24 }} /></Box>
        <Typography component="span" sx={{ fontSize: 12, lineHeight: '16px', fontWeight: active ? 700 : 500, color: active ? colors.ink : 'inherit' }}>{group.label}</Typography>
      </ButtonBase>;
    })}
  </Box>;
}

function Dashboard({ user, onLogout, onUserUpdated }: { user: User; onLogout: () => void; onUserUpdated: (user: User) => void }) {
  const [page, setPage] = useState<Page>(pageFromLocation);
  const [metadataLibraryId, setMetadataLibraryId] = useState<string | undefined>(metadataLibraryFromLocation);
  const currentPage = useRef(page);
  const currentMetadataLibrary = useRef(metadataLibraryId);
  const navigationGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const setNavigationGuard = useCallback((guard: UserNavigationGuard | undefined) => { navigationGuard.current = guard; }, []);
  const settingsBusyRef = useRef(false);
  const [settingsBusy, setSettingsBusy] = useState(false);
  const onSettingsBusyChange = useCallback((busy: boolean) => { settingsBusyRef.current = busy; setSettingsBusy(busy); }, []);
  const [lastTabs, setLastTabs] = useState<LastTabs>(readLastTabs);
  const [accountAnchor, setAccountAnchor] = useState<HTMLElement | null>(null);
  const [serverName, setServerName] = useState('Goby server');
  const [signingOut, setSigningOut] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const content = useRef<HTMLElement>(null);
  const group = groupForPage(page);
  const section = settingsSection(page);
  const activeTab = page === 'metadata' ? 'libraries' : page;

  useEffect(() => {
    if (!accountAnchor) return;
    const controller = new AbortController();
    void adminApi.getOverview({ signal: controller.signal }).then((overview) => {
      if (!controller.signal.aborted) setServerName(overview.Server.Name);
    }).catch(() => { /* Account actions remain available if server details cannot load. */ });
    return () => controller.abort();
  }, [accountAnchor]);

  useEffect(() => {
    const canonical = pageURL(currentPage.current, currentMetadataLibrary.current);
    if (window.location.pathname !== canonical) window.history.replaceState(window.history.state, '', canonical + window.location.search + window.location.hash);
    const changed = () => {
      const next = pageFromLocation();
      const nextLibrary = metadataLibraryFromLocation();
      const internalSettings = settingsSection(currentPage.current) && settingsSection(next);
      if (internalSettings && settingsBusyRef.current && next !== currentPage.current) {
        window.history.pushState(null, '', pageURL(currentPage.current, currentMetadataLibrary.current));
        return;
      }
      if ((next !== currentPage.current || nextLibrary !== currentMetadataLibrary.current) && !internalSettings && navigationGuard.current && !navigationGuard.current()) {
        window.history.pushState(null, '', pageURL(currentPage.current, currentMetadataLibrary.current));
        return;
      }
      currentPage.current = next;
      currentMetadataLibrary.current = nextLibrary;
      setPage(next);
      setMetadataLibraryId(nextLibrary);
      content.current?.scrollTo({ top: 0 });
    };
    window.addEventListener('popstate', changed);
    return () => window.removeEventListener('popstate', changed);
  }, []);

  useEffect(() => {
    document.title = `${group.tabs.find((tab) => tab.page === activeTab)?.label ?? group.title} · Goby administration`;
    setLastTabs((previous) => {
      const next = { ...previous, [group.id]: activeTab };
      try { window.sessionStorage.setItem('goby.dashboard.tabs', JSON.stringify(next)); } catch { /* Storage is optional. */ }
      return next;
    });
  }, [page, group, activeTab]);

  function navigate(next: Page, event?: MouseEvent<HTMLAnchorElement>, libraryId?: string, state?: { tasksTab: 'history' }) {
    if (event && (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0)) return;
    event?.preventDefault();
    const changing = page !== next || metadataLibraryId !== libraryId;
    const internalSettings = section && settingsSection(next);
    if (changing && internalSettings && settingsBusyRef.current) return;
    if (changing && !internalSettings && navigationGuard.current && !navigationGuard.current()) return;
    if (changing) window.history.pushState(state ?? null, '', pageURL(next, libraryId));
    currentPage.current = next;
    currentMetadataLibrary.current = libraryId;
    setPage(next);
    setMetadataLibraryId(libraryId);
    if (changing) {
      content.current?.scrollTo({ top: 0, behavior: 'instant' });
      requestAnimationFrame(() => content.current?.focus());
    }
  }
  async function signOut() {
    if (signingOut || (navigationGuard.current && !navigationGuard.current())) return;
    setSigningOut(true);
    setError(null);
    try { await adminApi.logout(); onLogout(); }
    catch (cause) { if (!isAbortError(cause)) setError(cause); }
    finally { setSigningOut(false); setAccountAnchor(null); }
  }

  return <Box sx={{ height: '100dvh', display: 'flex', flexDirection: { xs: 'column-reverse', md: 'row' }, overflow: 'hidden', bgcolor: colors.canvas }}>
    <a className="skip-link" href="#main-content">Skip to content</a>
    <Navigation selected={group.id} lastTabs={lastTabs} navigate={navigate} />
    <Box sx={{ flex: 1, minWidth: 0, minHeight: 0, m: { xs: '8px 8px 0', md: '8px 8px 8px 0' }, bgcolor: 'background.paper', borderRadius: '24px', overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
      <Stack component="header" direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 2, p: { xs: '14px 16px', md: '14px 20px 14px 32px' }, minHeight: 68 }}>
        <Typography component="h1" sx={{ fontSize: 22, lineHeight: '28px', fontWeight: 500 }}>{group.title}</Typography>
        <ButtonBase aria-label={`Account: ${user.Name}`} aria-haspopup="menu" aria-expanded={Boolean(accountAnchor)} aria-controls={accountAnchor ? 'account-menu' : undefined} onClick={(event) => setAccountAnchor(event.currentTarget)} sx={{ gap: 1.25, pl: 1.5, pr: 0.5, py: 0.5, borderRadius: '20px', height: 40, '&:hover': { bgcolor: colors.surface }, '&.Mui-focusVisible': { outline: `3px solid ${colors.primary}` } }}>
          <Typography variant="body2" noWrap sx={{ maxWidth: { xs: 100, sm: 180 } }}>{user.Name}</Typography>
          <Avatar sx={{ width: 32, height: 32, bgcolor: colors.secondaryContainer, color: colors.deep, fontSize: 12, fontWeight: 700 }}>{user.Name.trim().slice(0, 2).toUpperCase()}</Avatar>
        </ButtonBase>
        <Menu id="account-menu" anchorEl={accountAnchor} open={Boolean(accountAnchor)} onClose={() => setAccountAnchor(null)} anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }} transformOrigin={{ vertical: 'top', horizontal: 'right' }} slotProps={{ paper: { sx: { width: 264, borderRadius: '16px', mt: 1 } } }}>
          <Stack direction="row" sx={{ gap: 1.5, px: 2, py: 1.5, alignItems: 'center' }}><Avatar sx={{ bgcolor: colors.secondaryContainer, color: colors.deep }}>{user.Name.trim().slice(0, 2).toUpperCase()}</Avatar><Box sx={{ minWidth: 0 }}><Typography noWrap sx={{ fontWeight: 600 }}>{user.Name}</Typography><Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>Administrator · {serverName}</Typography></Box></Stack>
          <Divider />
          <MenuItem onClick={() => void signOut()} disabled={signingOut} sx={{ gap: 1.5, mx: 1, mt: 1, borderRadius: 2 }}><LogoutRounded sx={{ fontSize: 20 }} />{signingOut ? 'Signing out…' : 'Sign out'}</MenuItem>
        </Menu>
      </Stack>
      {group.tabs.length > 0 && <Tabs value={activeTab} aria-label={`${group.title} pages`} variant="scrollable" scrollButtons="auto" allowScrollButtonsMobile sx={{ px: { xs: 0, md: 2 }, flexShrink: 0, borderBottom: 1, borderColor: 'divider', '& .MuiTabs-list': { gap: 0.5 } }}>
        {group.tabs.map((tab) => <Tab component="a" href={pageURL(tab.page)} key={tab.page} value={tab.page} label={tab.label} disabled={settingsBusy && tab.page !== page} onClick={(event: MouseEvent<HTMLAnchorElement>) => navigate(tab.page, event)} />)}
      </Tabs>}
      {group.tabs.length === 0 && <Divider />}
      <Box component="main" id="main-content" ref={content} className="dashboard-content" tabIndex={-1} sx={{ flex: 1, minHeight: 0, overflowY: 'auto', overflowX: 'hidden', p: { xs: '20px 16px 24px', md: '24px 32px 32px' }, outline: 'none' }}>
        {error != null && <Box sx={{ mb: 2.5 }}><ErrorNotice error={error} /></Box>}
        <Suspense fallback={<Stack role="status" aria-label="Loading page" spacing={2.5}><Skeleton height={40} width="45%" /><Skeleton variant="rounded" height={160} /><Skeleton variant="rounded" height={240} /></Stack>}>
          {page === 'overview' && <OverviewPage user={user} onUsers={() => navigate('users')} onLibraries={() => navigate('libraries')} onItems={() => navigate('libraries')} onSessions={() => navigate('sessions')} onActivity={() => navigate('observability')} />}
          {page === 'users' && <UsersPage currentUser={user} onCurrentUserUpdated={onUserUpdated} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'libraries' && <LibrariesPage onTasks={() => navigate('tasks', undefined, undefined, { tasksTab: 'history' })} onManageItems={(library) => navigate('metadata', undefined, library.Id)} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'metadata' && metadataLibraryId && <MetadataItemsPage key={metadataLibraryId} libraryId={metadataLibraryId} onLibraries={() => navigate('libraries')} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'tasks' && <TasksPage onLibraries={() => navigate('libraries')} currentUserId={user.Id} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'media-analysis' && <MediaAnalysisPage key={user.Id} currentUserId={user.Id} onTasks={() => navigate('tasks')} onLibraries={() => navigate('libraries')} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'sessions' && <SessionsPage onNavigationGuardChange={setNavigationGuard} />}
          {page === 'devices' && <DevicesPage onNavigationGuardChange={setNavigationGuard} />}
          {page === 'api-keys' && <ApiKeysPage onNavigationGuardChange={setNavigationGuard} />}
          {section && <SettingsPage section={section} currentUserId={user.Id} onProviders={() => navigate('providers')} onBusyChange={onSettingsBusyChange} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'observability' && <ObservabilityPage />}
          {page === 'backups' && <BackupsPage key={user.Id} currentUserId={user.Id} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'providers' && <ProvidersPage />}
          {page === 'collections' && <CollectionsPage onNavigationGuardChange={setNavigationGuard} />}
          {page === 'artwork' && <CatalogArtworkPage onNavigationGuardChange={setNavigationGuard} />}
          {page === 'notifications' && <NotificationsPage onNavigationGuardChange={setNavigationGuard} />}
        </Suspense>
      </Box>
    </Box>
  </Box>;
}
export function App() {
  const [state, setState] = useState<AppState>({ mode: 'loading' });
  const [revision, setRevision] = useState(0);

  useEffect(() => onSessionExpired(() => setState({ mode: 'login', notice: pageFromLocation() === 'backups'
    ? 'Your session has changed or expired. If a restore or rollback was in progress, sign in with the restored account passwords and check its job status. A disconnected session does not confirm completion.'
    : 'Your session has changed or expired. Sign in again to continue.' })), []);

  useEffect(() => {
    const controller = new AbortController();
    setState({ mode: 'loading' });
    clearSession();
    async function connect() {
      try {
        const bootstrap = await adminApi.getBootstrap({ signal: controller.signal });
        if (!bootstrap.Initialized) {
          setState({ mode: 'setup' });
          return;
        }
        const session = await adminApi.getSession({ signal: controller.signal });
        setState({ mode: 'ready', user: session.User });
      } catch (error) {
        if (isAbortError(error)) return;
        if (error instanceof ApiError && error.status === 401) setState({ mode: 'login' });
        else setState({ mode: 'error', error });
      }
    }
    void connect();
    return () => controller.abort();
  }, [revision]);

  useEffect(() => {
    if (state.mode !== 'ready') document.title = `${state.mode === 'setup' ? 'Set up your server' : 'Goby'} · Administration`;
  }, [state.mode]);

  const reload = () => setRevision((value) => value + 1);

  if (state.mode === 'loading') return <LoadingView />;
  if (state.mode === 'error') return <Stack component="main" sx={{ justifyContent: 'center', alignItems: 'center', minHeight: '100dvh', p: 3 }}><Paper variant="outlined" sx={{ p: 4, width: '100%', maxWidth: 560 }}><Brand /><Typography variant="h3" component="h1" sx={{ mt: 4, mb: 2 }}>Could not connect to your server</Typography><ErrorNotice error={state.error} /><Button variant="contained" onClick={reload} sx={{ mt: 3 }}>Try again</Button></Paper></Stack>;
  if (state.mode === 'ready') return <Dashboard user={state.user} onLogout={() => setState({ mode: 'login' })} onUserUpdated={(user) => setState((current) => current.mode === 'ready' && current.user.Id === user.Id ? { ...current, user } : current)} />;
  return <Suspense fallback={<LoadingView label="Loading sign in" />}><AuthPage key={state.mode} mode={state.mode} initialName={state.mode === 'login' ? state.name : undefined} notice={state.mode === 'login' ? state.notice : undefined} onSession={(session) => setState({ mode: 'ready', user: session.User })} onSetup={(name) => setState({ mode: 'login', name, notice: 'Your administrator account is ready. Sign in to continue.' })} onReload={reload} /></Suspense>;
}
