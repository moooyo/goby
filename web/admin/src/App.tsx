import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react';
import type { MouseEvent } from 'react';
import { Box, Button, Paper, Skeleton, Stack, Typography } from './ui';
import { adminApi, ApiError, clearSession, isAbortError, onSessionExpired } from './api';
import type { User } from './api';
import { Brand, ErrorNotice, LoadingView } from './components';
import { DashboardShell } from './DashboardShell';
import { useAdminPreferences, useTranslation } from './i18n';
import type { UserNavigationGuard } from './userDraftNavigation';
import { metadataLibraryFromLocation, pageFromLocation, pageURL, settingsSection } from './dashboardNavigation';
import type { Page } from './dashboardNavigation';
const AuthPage = lazy(() => import('./AuthPage').then((module) => ({ default: module.AuthPage })));
const SetupWizard = lazy(() => import('./SetupWizard').then((module) => ({ default: module.SetupWizard })));
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
  | { mode: 'setup-wizard'; user: User }
  | { mode: 'login'; name?: string; notice?: string }
  | { mode: 'ready'; user: User };
function Dashboard({ user, onLogout, onUserUpdated }: { user: User; onLogout: () => void; onUserUpdated: (user: User) => void }) {
  const [page, setPage] = useState<Page>(pageFromLocation);
  const [metadataLibraryId, setMetadataLibraryId] = useState<string | undefined>(metadataLibraryFromLocation);
  const [taskRouteRevision, setTaskRouteRevision] = useState(0);
  const currentPage = useRef(page);
  const currentMetadataLibrary = useRef(metadataLibraryId);
  const currentTaskQuery = useRef<string | undefined>(new URLSearchParams(window.location.search).get('task') || undefined);
  const navigationGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const setNavigationGuard = useCallback((guard: UserNavigationGuard | undefined) => { navigationGuard.current = guard; }, []);
  const settingsBusyRef = useRef(false);
  const [settingsBusy, setSettingsBusy] = useState(false);
  const onSettingsBusyChange = useCallback((busy: boolean) => { settingsBusyRef.current = busy; setSettingsBusy(busy); }, []);
  const [signingOut, setSigningOut] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const content = useRef<HTMLElement>(null);
  const section = settingsSection(page);


  useEffect(() => {
    const canonical = pageURL(currentPage.current, currentMetadataLibrary.current);
    if (window.location.pathname !== canonical) window.history.replaceState(window.history.state, '', canonical + window.location.search + window.location.hash);
    const changed = () => {
      const next = pageFromLocation();
      const nextLibrary = metadataLibraryFromLocation();
      const nextTaskQuery = next === 'tasks' ? new URLSearchParams(window.location.search).get('task') || undefined : undefined;
      const taskQueryChanged = next === 'tasks' && currentPage.current === 'tasks' && nextTaskQuery !== currentTaskQuery.current;
      const restoreURL = () => pageURL(currentPage.current, currentMetadataLibrary.current)
        + (currentPage.current === 'tasks' && currentTaskQuery.current ? `?task=${encodeURIComponent(currentTaskQuery.current)}` : '');
      const internalSettings = settingsSection(currentPage.current) && settingsSection(next);
      if (internalSettings && settingsBusyRef.current && next !== currentPage.current) {
        window.history.pushState(null, '', pageURL(currentPage.current, currentMetadataLibrary.current));
        return;
      }
      if ((next !== currentPage.current || nextLibrary !== currentMetadataLibrary.current || taskQueryChanged) && !internalSettings && navigationGuard.current && !navigationGuard.current()) {
        window.history.pushState(null, '', restoreURL());
        return;
      }
      if (taskQueryChanged) setTaskRouteRevision((value) => value + 1);
      currentPage.current = next;
      currentMetadataLibrary.current = nextLibrary;
      currentTaskQuery.current = nextTaskQuery;
      setPage(next);
      setMetadataLibraryId(nextLibrary);
      content.current?.scrollTo({ top: 0 });
    };
    window.addEventListener('popstate', changed);
    return () => window.removeEventListener('popstate', changed);
  }, []);


  function navigate(next: Page, event?: MouseEvent<HTMLAnchorElement>, libraryId?: string, state?: { tasksTab: 'history' | 'available' }, taskId?: string) {
    if (event && (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0)) return;
    event?.preventDefault();
    const targetURL = pageURL(next, libraryId) + (next === 'tasks' && taskId ? `?task=${encodeURIComponent(taskId)}` : '');
    const changing = page !== next || metadataLibraryId !== libraryId || next === 'tasks' && window.location.pathname + window.location.search !== targetURL;
    const internalSettings = section && settingsSection(next);
    if (changing && internalSettings && settingsBusyRef.current) return;
    if (changing && !internalSettings && navigationGuard.current && !navigationGuard.current()) return;
    if (changing) window.history.pushState(state ?? null, '', targetURL);
    if (changing && next === 'tasks') setTaskRouteRevision((value) => value + 1);
    currentPage.current = next;
    currentMetadataLibrary.current = libraryId;
    currentTaskQuery.current = next === 'tasks' ? taskId : undefined;
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
    finally { setSigningOut(false); }
  }

  return <DashboardShell user={user} page={page} libraryId={metadataLibraryId} onNavigate={navigate} onSignOut={() => void signOut()} signingOut={signingOut} navigationBusy={settingsBusy}>
    <main id="main-content" ref={content} className="dashboard-content" tabIndex={-1}>
      {error != null && <Box sx={{ mb: 2.5 }}><ErrorNotice error={error} /></Box>}
        <Suspense fallback={<Stack role="status" aria-label="Loading page" spacing={2.5}><Skeleton height={40} width="45%" /><Skeleton variant="rounded" height={160} /><Skeleton variant="rounded" height={240} /></Stack>}>
          {page === 'overview' && <OverviewPage user={user} onUsers={() => navigate('users')} onLibraries={() => navigate('libraries')} onItems={() => navigate('libraries')} onSessions={() => navigate('sessions')} onActivity={() => navigate('observability')} onTasks={() => navigate('tasks')} onBackupTask={(taskId) => navigate('tasks', undefined, undefined, undefined, taskId)} onSettings={() => navigate('settings')} onAnalysis={() => navigate('media-analysis')} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'users' && <UsersPage currentUser={user} onCurrentUserUpdated={onUserUpdated} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'libraries' && <LibrariesPage onTasks={() => navigate('tasks', undefined, undefined, { tasksTab: 'history' })} onBackgroundTasks={() => navigate('tasks', undefined, undefined, { tasksTab: 'available' })} onManageItems={(library) => navigate('metadata', undefined, library.Id)} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'metadata' && metadataLibraryId && <MetadataItemsPage key={metadataLibraryId} libraryId={metadataLibraryId} currentUserId={user.Id} onLibraries={() => navigate('libraries')} onTasks={() => navigate('tasks')} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'tasks' && <TasksPage key={taskRouteRevision} onLibraries={() => navigate('libraries')} currentUserId={user.Id} onNavigationGuardChange={setNavigationGuard} onTaskQueryCleared={() => { currentTaskQuery.current = undefined; }} />}
          {page === 'media-analysis' && <MediaAnalysisPage key={user.Id} currentUserId={user.Id} onTasks={() => navigate('tasks')} onLibraries={() => navigate('libraries')} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'sessions' && <SessionsPage onNavigationGuardChange={setNavigationGuard} />}
          {page === 'devices' && <DevicesPage onNavigationGuardChange={setNavigationGuard} />}
          {page === 'api-keys' && <ApiKeysPage onNavigationGuardChange={setNavigationGuard} />}
          {section && <SettingsPage section={section} currentUserId={user.Id} onProviders={() => navigate('providers')} onBusyChange={onSettingsBusyChange} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'observability' && <ObservabilityPage />}
          {page === 'backups' && <BackupsPage key={user.Id} currentUserId={user.Id} onNavigationGuardChange={setNavigationGuard} />}
          {page === 'providers' && <ProvidersPage onNavigationGuardChange={setNavigationGuard} />}
          {page === 'collections' && <CollectionsPage onNavigationGuardChange={setNavigationGuard} />}
          {page === 'artwork' && <CatalogArtworkPage onNavigationGuardChange={setNavigationGuard} />}
          {page === 'notifications' && <NotificationsPage onNavigationGuardChange={setNavigationGuard} />}
        </Suspense>
    </main>
  </DashboardShell>;
}

export function App() {
  const { t } = useTranslation();
  const { applyServerLanguage } = useAdminPreferences();
  const [state, setState] = useState<AppState>({ mode: 'loading' });
  const [revision, setRevision] = useState(0);

  useEffect(() => onSessionExpired(() => setState((current) => ({ mode: 'login', notice: current.mode === 'setup-wizard'
    ? 'Your setup session changed or expired. Sign in to manage your libraries and finish configuration.'
    : pageFromLocation() === 'backups'
      ? 'Your session has changed or expired. If a restore or rollback was in progress, sign in with the restored account passwords and check its job status. A disconnected session does not confirm completion.'
      : 'Your session has changed or expired. Sign in again to continue.' }))), []);

  useEffect(() => {
    const controller = new AbortController();
    setState({ mode: 'loading' });
    clearSession();
    async function connect() {
      try {
        const bootstrap = await adminApi.getBootstrap({ signal: controller.signal });
        if (controller.signal.aborted) return;
        applyServerLanguage(bootstrap.UICulture ?? 'zh-CN');
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
  }, [revision, applyServerLanguage]);

  useEffect(() => {
    if (state.mode !== 'ready') document.title = `${t(state.mode === 'setup' || state.mode === 'setup-wizard' ? 'Set up your server' : 'Goby')} · ${t('Administration')}`;
  }, [state.mode, t]);

  const reload = () => setRevision((value) => value + 1);

  if (state.mode === 'loading') return <LoadingView />;
  if (state.mode === 'error') return <Stack component="main" sx={{ justifyContent: 'center', alignItems: 'center', minHeight: '100dvh', p: 3 }}><Paper className="connection-error"><Brand /><Typography variant="h3" component="h1" sx={{ mt: 4, mb: 2 }}>Could not connect to your server</Typography><ErrorNotice error={state.error} /><Button variant="contained" onClick={reload} sx={{ mt: 3 }}>Try again</Button></Paper></Stack>;
  if (state.mode === 'ready') return <Dashboard user={state.user} onLogout={() => setState({ mode: 'login' })} onUserUpdated={(user) => setState((current) => current.mode === 'ready' && current.user.Id === user.Id ? { ...current, user } : current)} />;
  if (state.mode === 'setup-wizard') return <Suspense fallback={<LoadingView label="Loading setup" />}><SetupWizard user={state.user} onComplete={() => setState({ mode: 'login', notice: 'Your administrator account is ready. Sign in to continue.' })} onInterrupted={(notice) => { clearSession(); setState({ mode: 'login', notice }); }} /></Suspense>;
  return <Suspense fallback={<LoadingView label="Loading sign in" />}><AuthPage key={state.mode} mode={state.mode} initialName={state.mode === 'login' ? state.name : undefined} notice={state.mode === 'login' ? state.notice : undefined} onSession={(session) => setState({ mode: 'ready', user: session.User })} onSetupSession={(session) => setState({ mode: 'setup-wizard', user: session.User })} onSetupInterrupted={(notice) => { clearSession(); setState({ mode: 'login', notice }); }} onReload={reload} /></Suspense>;
}
