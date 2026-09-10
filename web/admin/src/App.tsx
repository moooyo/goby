import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react';
import type { MouseEvent } from 'react';
import { Avatar, Box, Button, Chip, Divider, Drawer, IconButton, List, ListItemButton, ListItemIcon, ListItemText, Paper, Skeleton, Stack, Tooltip, Typography } from '@mui/material';
import SpaceDashboardOutlined from '@mui/icons-material/SpaceDashboardOutlined';
import PeopleOutlineRounded from '@mui/icons-material/PeopleOutlineRounded';
import VideoLibraryOutlined from '@mui/icons-material/VideoLibraryOutlined';
import SensorsRounded from '@mui/icons-material/SensorsRounded';
import DevicesOutlined from '@mui/icons-material/DevicesOutlined';
import KeyRounded from '@mui/icons-material/KeyRounded';
import PlaylistAddCheckRounded from '@mui/icons-material/PlaylistAddCheckRounded';
import SettingsOutlined from '@mui/icons-material/SettingsOutlined';
import LogoutRounded from '@mui/icons-material/LogoutRounded';
import MenuRounded from '@mui/icons-material/MenuRounded';
import ChevronRightRounded from '@mui/icons-material/ChevronRightRounded';
import { adminApi, ApiError, clearSession, isAbortError, onSessionExpired } from './api';
import type { User } from './api';
import { Brand, ErrorNotice, LoadingView } from './components';
import { colors } from './theme';
import type { UserNavigationGuard } from './userDraftNavigation';

const AuthPage = lazy(() => import('./AuthPage').then((module) => ({ default: module.AuthPage })));
const OverviewPage = lazy(() => import('./OverviewPage').then((module) => ({ default: module.OverviewPage })));
const UsersPage = lazy(() => import('./UsersPage').then((module) => ({ default: module.UsersPage })));
const LibrariesPage = lazy(() => import('./LibrariesPage').then((module) => ({ default: module.LibrariesPage })));
const TasksPage = lazy(() => import('./TasksPage').then((module) => ({ default: module.TasksPage })));
const SessionsPage = lazy(() => import('./SessionsPage').then((module) => ({ default: module.SessionsPage })));
const DevicesPage = lazy(() => import('./DevicesPage').then((module) => ({ default: module.DevicesPage })));
const ApiKeysPage = lazy(() => import('./ApiKeysPage').then((module) => ({ default: module.ApiKeysPage })));
const MetadataItemsPage = lazy(() => import('./MetadataItemsPage').then((module) => ({ default: module.MetadataItemsPage })));

type Page = 'overview' | 'users' | 'libraries' | 'tasks' | 'metadata' | 'sessions' | 'devices' | 'api-keys';
type AppState =
  | { mode: 'loading' }
  | { mode: 'error'; error: unknown }
  | { mode: 'setup' }
  | { mode: 'login'; name?: string; notice?: string }
  | { mode: 'ready'; user: User };

const sidebarWidth = 240;
const pageTitles: Record<Page, string> = { overview: 'Overview', users: 'Users', libraries: 'Libraries', tasks: 'Tasks', metadata: 'Library items', sessions: 'Sessions', devices: 'Devices', 'api-keys': 'API keys' };

function metadataLibraryFromLocation(): string | undefined {
  const match = /^\/admin\/libraries\/([^/]+)\/items\/?$/.exec(window.location.pathname);
  if (!match) return undefined;
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return undefined;
  }
}

function pageFromLocation(): Page {
  const path = window.location.pathname.replace(/\/+$/, '');
  if (metadataLibraryFromLocation() !== undefined) return 'metadata';
  if (path.endsWith('/users')) return 'users';
  if (path.endsWith('/libraries')) return 'libraries';
  if (path.endsWith('/tasks')) return 'tasks';
  if (path.endsWith('/sessions')) return 'sessions';
  if (path.endsWith('/devices')) return 'devices';
  if (path.endsWith('/api-keys')) return 'api-keys';
  return 'overview';
}

function pageURL(page: Page, libraryId?: string): string {
  if (page === 'metadata') return libraryId ? `/admin/libraries/${encodeURIComponent(libraryId)}/items` : '/admin/libraries';
  return page === 'overview' ? '/admin/' : `/admin/${page}`;
}

function Navigation({ page, navigate }: { page: Page; navigate: (page: Page, event?: MouseEvent<HTMLAnchorElement>) => void }) {
  const selectedPage = page === 'metadata' ? 'libraries' : page;
  return (
    <Stack component="nav" aria-label="Administration" sx={{ height: '100%', bgcolor: colors.deep, color: 'white', p: 2.5 }}>
      <Box sx={{ px: 0.75, pt: 1, pb: 5 }}><Brand light /></Box>
      <Typography variant="overline" sx={{ px: 1.5, mb: 1, color: '#92B6C1' }}>Workspace</Typography>
      <List disablePadding sx={{ '& .MuiListItemButton-root': { borderRadius: 2, minHeight: 46, px: 1.5, mb: 0.6, color: '#BCD1D8', '&.Mui-selected': { bgcolor: '#FFFFFF14', color: 'white', boxShadow: 'inset 3px 0 0 #72C5C2' }, '&.Mui-selected:hover': { bgcolor: '#FFFFFF1C' }, '&:hover': { bgcolor: '#FFFFFF0B' } }, '& .MuiListItemIcon-root': { minWidth: 34, color: 'inherit' }, '& .MuiListItemText-primary': { fontSize: 13, fontWeight: 580 } }}>
        {[
          { id: 'overview' as const, label: 'Overview', icon: SpaceDashboardOutlined },
          { id: 'users' as const, label: 'Users', icon: PeopleOutlineRounded },
          { id: 'libraries' as const, label: 'Libraries', icon: VideoLibraryOutlined },
          { id: 'tasks' as const, label: 'Tasks', icon: PlaylistAddCheckRounded },
          { id: 'sessions' as const, label: 'Sessions', icon: SensorsRounded },
          { id: 'devices' as const, label: 'Devices', icon: DevicesOutlined },
          { id: 'api-keys' as const, label: 'API keys', icon: KeyRounded },
        ].map(({ id, label, icon: Icon }) => (
          <ListItemButton key={id} component="a" href={pageURL(id)} selected={selectedPage === id} aria-current={selectedPage === id ? 'page' : undefined} onClick={(event: MouseEvent<HTMLAnchorElement>) => navigate(id, event)}>
            <ListItemIcon><Icon sx={{ fontSize: 21 }} /></ListItemIcon><ListItemText primary={label} />
          </ListItemButton>
        ))}
        {[
          { label: 'Settings', icon: SettingsOutlined },
        ].map(({ label, icon: Icon }) => (
          <Tooltip key={label} title="Available in a future release" placement="right">
            <Box>
              <ListItemButton disabled aria-label={`${label}, available in a future release`} sx={{ '&.Mui-disabled': { opacity: 0.44 } }}>
                <ListItemIcon><Icon sx={{ fontSize: 21 }} /></ListItemIcon><ListItemText primary={label} /><Typography component="span" sx={{ fontSize: 9, letterSpacing: '0.04em', border: '1px solid #B6CDD444', borderRadius: '4px', px: 0.6, py: 0.15 }}>Later</Typography>
              </ListItemButton>
            </Box>
          </Tooltip>
        ))}
      </List>
      <Box sx={{ mt: 'auto', pt: 4 }}>
        <Divider sx={{ borderColor: '#FFFFFF1A', mb: 2 }} />
        <Stack direction="row" sx={{ alignItems: 'center', gap: 0.8, px: 0.75 }}><Box sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: '#72C5C2' }} /><Typography variant="body2" sx={{ fontSize: 11, color: '#ACC6CE' }}>Goby administrator workspace</Typography></Stack>
      </Box>
    </Stack>
  );
}

function Dashboard({ user, onLogout, onUserUpdated }: { user: User; onLogout: () => void; onUserUpdated: (user: User) => void }) {
  const [page, setPage] = useState<Page>(pageFromLocation);
  const [metadataLibraryId, setMetadataLibraryId] = useState<string | undefined>(metadataLibraryFromLocation);
  const currentPage = useRef(page);
  const currentMetadataLibrary = useRef(metadataLibraryId);
  const navigationGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const setNavigationGuard = useCallback((guard: UserNavigationGuard | undefined) => { navigationGuard.current = guard; }, []);
  const [mobileOpen, setMobileOpen] = useState(false);
  const focusAfterDrawer = useRef(false);
  const menuButton = useRef<HTMLButtonElement>(null);
  const [signingOut, setSigningOut] = useState(false);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    const changed = () => {
      const next = pageFromLocation();
      const nextLibrary = metadataLibraryFromLocation();
      if ((next !== currentPage.current || nextLibrary !== currentMetadataLibrary.current) && navigationGuard.current && !navigationGuard.current()) {
        window.history.pushState(null, '', pageURL(currentPage.current, currentMetadataLibrary.current));
        return;
      }
      currentPage.current = next;
      currentMetadataLibrary.current = nextLibrary;
      setPage(next);
      setMetadataLibraryId(nextLibrary);
    };
    window.addEventListener('popstate', changed);
    return () => window.removeEventListener('popstate', changed);
  }, []);

  useEffect(() => {
    document.title = `${pageTitles[page]} · Goby administration`;
  }, [page]);

  function navigate(next: Page, event?: MouseEvent<HTMLAnchorElement>, libraryId?: string, state?: { tasksTab: 'history' }) {
    if (event && (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0)) return;
    event?.preventDefault();
    const changing = page !== next || metadataLibraryId !== libraryId;
    if (changing && navigationGuard.current && !navigationGuard.current()) return;
    if (changing) window.history.pushState(state ?? null, '', pageURL(next, libraryId));
    currentPage.current = next;
    currentMetadataLibrary.current = libraryId;
    setPage(next);
    setMetadataLibraryId(libraryId);
    setMobileOpen(false);
    if (changing) {
      window.scrollTo({ top: 0, behavior: 'instant' });
      if (mobileOpen) focusAfterDrawer.current = true;
      else requestAnimationFrame(() => document.getElementById('main-content')?.focus());
    }
  }

  async function signOut() {
    if (signingOut) return;
    setSigningOut(true);
    setError(null);
    try {
      await adminApi.logout();
      onLogout();
    } catch (cause) {
      if (!isAbortError(cause)) setError(cause);
    } finally {
      setSigningOut(false);
    }
  }

  return (
    <Box sx={{ minHeight: '100dvh' }}>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <Box sx={{ position: 'fixed', inset: '0 auto 0 0', width: sidebarWidth, display: { xs: 'none', md: 'block' } }}><Navigation page={page} navigate={navigate} /></Box>
      <Drawer open={mobileOpen} onClose={() => setMobileOpen(false)} slotProps={{ root: { disableRestoreFocus: true }, transition: { onExited: () => { if (focusAfterDrawer.current) { focusAfterDrawer.current = false; document.getElementById('main-content')?.focus(); } else menuButton.current?.focus(); } } }} sx={{ display: { md: 'none' }, '& .MuiDrawer-paper': { width: sidebarWidth, border: 0 } }}><Navigation page={page} navigate={navigate} /></Drawer>
      <Box sx={{ ml: { md: `${sidebarWidth}px` } }}>
        <Stack component="header" direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center', gap: 1, minHeight: 77, px: { xs: 2, sm: 3, lg: 5 }, borderBottom: 1, borderColor: 'divider', bgcolor: 'background.paper' }}>
          <Stack direction="row" sx={{ alignItems: 'center', gap: 1, minWidth: 0 }}>
            <IconButton ref={menuButton} onClick={() => { focusAfterDrawer.current = false; setMobileOpen(true); }} aria-label="Open navigation" sx={{ display: { md: 'none' }, ml: -1 }}><MenuRounded /></IconButton>
            <Typography variant="body2" color="text.secondary" sx={{ display: { xs: 'none', sm: 'block' } }}>Administration</Typography>
            <ChevronRightRounded sx={{ display: { xs: 'none', sm: 'block' }, color: '#A3B7BF', fontSize: 15 }} />
            <Typography variant="body2" sx={{ fontWeight: 600 }}>{pageTitles[page]}</Typography>
          </Stack>
          <Stack direction="row" sx={{ alignItems: 'center', gap: { xs: 0.8, sm: 1.5 }, minWidth: 0 }}>
            <Avatar sx={{ bgcolor: '#E1EEF0', color: colors.deep, width: 31, height: 31, fontSize: 11, fontWeight: 650 }}>{user.Name.trim().slice(0, 2).toUpperCase()}</Avatar>
            <Typography variant="body2" noWrap sx={{ maxWidth: 160, display: { xs: 'none', sm: 'block' } }}>{user.Name}</Typography>
            <Chip label="Admin" size="small" variant="outlined" sx={{ display: { xs: 'none', lg: 'flex' }, color: 'text.secondary' }} />
            <Tooltip title="Sign out"><IconButton aria-label="Sign out" onClick={signOut} disabled={signingOut} size="small" sx={{ ml: 0.5 }}><LogoutRounded sx={{ fontSize: 19 }} /></IconButton></Tooltip>
          </Stack>
        </Stack>
        <Box component="main" id="main-content" tabIndex={-1} sx={{ p: { xs: 2.5, sm: 3, lg: 5 }, maxWidth: 1460, mx: 'auto', outline: 'none' }}>
          {error != null && <Box sx={{ mb: 3 }}><ErrorNotice error={error} /></Box>}
          <Suspense fallback={<Stack role="status" aria-label="Loading page" spacing={3}><Skeleton height={64} width="45%" /><Skeleton variant="rounded" height={160} /><Skeleton variant="rounded" height={240} /></Stack>}>
            {page === 'overview' && <OverviewPage user={user} onUsers={() => navigate('users')} />}
            {page === 'users' && <UsersPage currentUser={user} onCurrentUserUpdated={onUserUpdated} onNavigationGuardChange={setNavigationGuard} />}
            {page === 'libraries' && <LibrariesPage onTasks={() => navigate('tasks', undefined, undefined, { tasksTab: 'history' })} onManageItems={(library) => navigate('metadata', undefined, library.Id)} />}
            {page === 'metadata' && metadataLibraryId && <MetadataItemsPage key={metadataLibraryId} libraryId={metadataLibraryId} onLibraries={() => navigate('libraries')} onNavigationGuardChange={setNavigationGuard} />}
            {page === 'tasks' && <TasksPage onLibraries={() => navigate('libraries')} currentUserId={user.Id} onNavigationGuardChange={setNavigationGuard} />}
            {page === 'sessions' && <SessionsPage onNavigationGuardChange={setNavigationGuard} />}
            {page === 'devices' && <DevicesPage onNavigationGuardChange={setNavigationGuard} />}
            {page === 'api-keys' && <ApiKeysPage onNavigationGuardChange={setNavigationGuard} />}
          </Suspense>
        </Box>
      </Box>
    </Box>
  );
}

export function App() {
  const [state, setState] = useState<AppState>({ mode: 'loading' });
  const [revision, setRevision] = useState(0);

  useEffect(() => onSessionExpired(() => setState({ mode: 'login', notice: 'Your session has changed or expired. Sign in again to continue.' })), []);

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
