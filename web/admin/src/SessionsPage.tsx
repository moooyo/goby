import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Autocomplete, Box, Button, CircularProgress, Collapse, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, InputAdornment, MenuItem, Paper, Skeleton, Snackbar, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Tooltip, Typography } from '@mui/material';
import DevicesOutlined from '@mui/icons-material/DevicesOutlined';
import FilterAltOffOutlined from '@mui/icons-material/FilterAltOffOutlined';
import LogoutRounded from '@mui/icons-material/LogoutRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import SearchRounded from '@mui/icons-material/SearchRounded';
import BlockRounded from '@mui/icons-material/BlockRounded';
import CloseRounded from '@mui/icons-material/CloseRounded';
import TuneRounded from '@mui/icons-material/TuneRounded';
import { adminApi, ApiError, isAbortError } from './api';
import type { LoginSession, LoginSessionKind, LoginSessionStatus, SessionsResponse, User } from './api';
import { ErrorNotice, PageHeading } from './components';
import { fieldError } from './formFields';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { AccessBadge, AccessDetails, AccessTableHeader, accessSearchSx, accessTableSx } from './accessVisuals';

interface SessionFilters {
  user: User | null;
  kind: LoginSessionKind | '';
  status: LoginSessionStatus | 'all';
  deviceId: string;
  searchTerm: string;
}

function initialFilters(): SessionFilters {
  return { user: null, kind: '', status: 'all', deviceId: '', searchTerm: '' };
}

const statusLabels: Record<LoginSessionStatus | 'all', string> = {
  active: 'Active',
  revoked: 'Revoked',
  disabled: 'Disabled',
  expired: 'Expired',
  all: 'All statuses',
};

function kindLabel(kind: LoginSessionKind): string {
  return kind === 'admin' ? 'Administrator dashboard' : 'Emby client';
}

function dateTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? 'Unknown' : date.toLocaleString(undefined, {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}

function StatusChip({ status }: { status: LoginSessionStatus }) {
  return <AccessBadge label={statusLabels[status]} tone={status === 'active' ? 'success' : status === 'disabled' ? 'warning' : 'neutral'} />;
}

function SessionIdentity({ session }: { session: LoginSession }) {
  return (
    <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
      <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 0.75 }}>
        <Typography variant="body2" sx={{ fontWeight: 600 }} title={session.UserIsAdministrator ? 'Administrator' : 'Member'}>{session.UserName || 'Unnamed user'}</Typography>
        <AccessBadge label={session.Kind === 'admin' ? 'Dashboard' : 'Emby'} tone={session.Kind === 'admin' ? 'info' : 'neutral'} title={kindLabel(session.Kind)} />
        {session.IsCurrent && <Typography variant="caption" color="primary.main" sx={{ fontWeight: 600, whiteSpace: 'nowrap' }}>This login</Typography>}
        {session.UserIsDisabled && <AccessBadge label="User disabled" tone="warning" />}
      </Stack>
      <Typography component="div" variant="caption" color="text.secondary" sx={{ mt: 0.35 }}>{session.Client || 'Client not reported'}{session.ApplicationVersion ? ` ${session.ApplicationVersion}` : ''}</Typography>
    </Box>
  );
}

function SessionDevice({ session }: { session: LoginSession }) {
  return (
    <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
      <Typography variant="body2">{session.DeviceName || 'Device name not reported'}</Typography>
      <Typography component="div" variant="caption" color="text.secondary" sx={{ mt: 0.35, fontSize: 11 }}>
        Device ID: <span className="mono">{session.DeviceId || 'Not reported'}</span>
      </Typography>
    </Box>
  );
}

function SessionHistory({ session }: { session: LoginSession }) {
  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography variant="body2" sx={{ fontSize: 13 }}>Created <Box component="time" dateTime={session.CreatedAt} title={session.CreatedAt} sx={{ whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>{dateTime(session.CreatedAt)}</Box></Typography>
      <Typography variant="caption" component="div" color="text.secondary">Last activity <Box component="time" dateTime={session.LastSeenAt} title={session.LastSeenAt} sx={{ whiteSpace: 'nowrap' }}>{dateTime(session.LastSeenAt)}</Box></Typography>
      <AccessDetails label="Login details">
        <Typography variant="caption" component="div">Expires <Box component="time" dateTime={session.ExpiresAt} title={session.ExpiresAt} sx={{ whiteSpace: 'nowrap' }}>{dateTime(session.ExpiresAt)}</Box></Typography>
        {session.RevokedAt && <Typography variant="caption" component="div">Revoked <Box component="time" dateTime={session.RevokedAt} title={session.RevokedAt} sx={{ whiteSpace: 'nowrap' }}>{dateTime(session.RevokedAt)}</Box></Typography>}
        <Typography variant="caption" component="div">{session.UserIsAdministrator ? 'Administrator account' : 'Member account'}</Typography>
      </AccessDetails>
    </Box>
  );
}

function RevokeButton({ session, onRevoke }: { session: LoginSession; onRevoke: (session: LoginSession) => void }) {
  if (session.Status === 'revoked') return null;
  return <Button size="small" variant="outlined" color="error" startIcon={<BlockRounded />} onClick={() => onRevoke(session)} aria-label={`Revoke login for ${session.UserName} on ${session.DeviceName || session.Client || 'unreported device'}`} sx={{ px: 1.75, borderColor: '#DDE3EE' }}>Revoke</Button>;
}

function SessionsList({ items, onRevoke }: { items: LoginSession[]; onRevoke: (session: LoginSession) => void }) {
  return (
    <>
      <Box component="ul" aria-label="Login sessions" sx={{ display: { xs: 'block', lg: 'none' }, listStyle: 'none', p: 0, m: 0 }}>
        {items.map((session) => (
          <Box component="li" key={session.Id} sx={{ p: 2.5, borderTop: 1, borderColor: 'divider' }}>
            <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: 1.5 }}>
              <SessionIdentity session={session} />
              <StatusChip status={session.Status} />
            </Stack>
            <Box sx={{ mt: 2 }}><SessionDevice session={session} /></Box>
            <Box sx={{ mt: 2, p: 1.5, borderRadius: '12px', bgcolor: '#F3F6FB' }}><SessionHistory session={session} /></Box>
            {session.Status !== 'revoked' && <Box sx={{ mt: 1.5, ml: -1 }}><RevokeButton session={session} onRevoke={onRevoke} /></Box>}
          </Box>
        ))}
      </Box>
      <TableContainer sx={{ display: { xs: 'none', lg: 'block' } }}>
        <Table aria-label="Login sessions" sx={{ minWidth: 820, tableLayout: 'fixed', ...accessTableSx }}>
          <TableHead><TableRow><TableCell sx={{ width: '31%' }}>User and client</TableCell><TableCell sx={{ width: '27%' }}>Device</TableCell><TableCell sx={{ width: '28%' }}>Login history</TableCell><TableCell align="right" sx={{ width: '14%' }}>Access</TableCell></TableRow></TableHead>
          <TableBody>
            {items.map((session) => (
              <TableRow key={session.Id}>
                <TableCell component="th" scope="row" sx={{ pl: 3 }}><SessionIdentity session={session} /></TableCell>
                <TableCell><SessionDevice session={session} /></TableCell>
                <TableCell><SessionHistory session={session} /></TableCell>
                <TableCell align="right">
                  {session.Status !== 'active' && <Box sx={{ mb: session.Status !== 'revoked' ? 1 : 0 }}><StatusChip status={session.Status} /></Box>}
                  {session.Status !== 'revoked' && <RevokeButton session={session} onRevoke={onRevoke} />}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </>
  );
}

function RevokeSessionDialog({ session, onClose, onRevoked, onRefresh, onNavigationGuardChange }: { session: LoginSession; onClose: () => void; onRevoked: () => void; onRefresh: () => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const inFlight = useRef<AbortController | undefined>(undefined);
  const refreshRequired = error instanceof ApiError && (['network_error', 'invalid_response'].includes(error.code) || error.status === 404);
  useUserDraftNavigation(false, busy, onNavigationGuardChange);
  useEffect(() => () => inFlight.current?.abort(), []);

  function close() {
    if (!inFlight.current) onClose();
  }

  async function revoke() {
    if (inFlight.current || refreshRequired) return;
    const controller = new AbortController();
    inFlight.current = controller;
    setBusy(true);
    setError(null);
    try {
      const result = await adminApi.revokeSession(session.Id, { signal: controller.signal });
      if (!controller.signal.aborted && !result.CurrentSessionRevoked) onRevoked();
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) setError(cause);
    } finally {
      if (inFlight.current === controller) inFlight.current = undefined;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  return (
    <Dialog open onClose={close} fullWidth maxWidth="sm" aria-labelledby="revoke-session-title" aria-describedby="revoke-session-description">
      <Box aria-busy={busy}>
        <DialogTitle id="revoke-session-title" sx={{ pt: 3 }}>Revoke this login?</DialogTitle>
        <DialogContent>
          <Stack spacing={2.5}>
            <Typography id="revoke-session-description" variant="body2" color="text.secondary">This ends only the selected login. The user can sign in again on the same device.</Typography>
            <Paper variant="outlined" sx={{ p: 2.5, bgcolor: 'background.default' }}>
              <Stack spacing={2}>
                <Box><StatusChip status={session.Status} /></Box>
                <SessionIdentity session={session} />
                <SessionDevice session={session} />
                <SessionHistory session={session} />
              </Stack>
            </Paper>
            {session.IsCurrent && <Alert severity="warning">This is your current administrator login. Revoking it signs you out of this dashboard.</Alert>}
            {error != null && <ErrorNotice error={error} />}
            {refreshRequired && <Alert severity="warning">{error instanceof ApiError && error.status === 404 ? 'This login no longer exists. Refresh the sessions to see the latest records.' : 'The result could not be confirmed. This login may already be revoked. Refresh the sessions before trying again.'}</Alert>}
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 3, flexWrap: 'wrap', gap: 1 }}>
          <Button onClick={close} color="secondary" disabled={busy} autoFocus>Cancel</Button>
          {refreshRequired
            ? <Button variant="contained" startIcon={<RefreshRounded />} onClick={onRefresh}>Refresh sessions</Button>
            : <Button variant="contained" color="error" onClick={() => void revoke()} disabled={busy} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <LogoutRounded />}>{busy ? 'Revoking login...' : 'Revoke login'}</Button>}
        </DialogActions>
      </Box>
    </Dialog>
  );
}

export function SessionsPage({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const [filters, setFilters] = useState<SessionFilters>(initialFilters);
  const [query, setQuery] = useState(() => ({ filters: initialFilters(), page: 0, pageSize: 50 }));
  const [loaded, setLoaded] = useState<{ requestKey: string; result: SessionsResponse }>();
  const [failure, setFailure] = useState<{ requestKey: string; cause: unknown }>();
  const [revision, setRevision] = useState(0);
  const [users, setUsers] = useState<User[]>([]);
  const [usersLoading, setUsersLoading] = useState(true);
  const [usersError, setUsersError] = useState<unknown>(null);
  const [usersRevision, setUsersRevision] = useState(0);
  const [revoking, setRevoking] = useState<LoginSession>();
  const [notice, setNotice] = useState('');
  const [filtersExpanded, setFiltersExpanded] = useState(false);
  const requestKey = JSON.stringify([query.filters.user?.Id, query.filters.kind, query.filters.status, query.filters.deviceId, query.filters.searchTerm, query.page, query.pageSize, revision]);
  const data = loaded?.requestKey === requestKey ? loaded.result : undefined;
  const failed = failure?.requestKey === requestKey;
  const loading = !data && !failed;
  const searchTooLong = new TextEncoder().encode(filters.searchTerm).length > 256;
  const isFiltered = Boolean(query.filters.user || query.filters.kind || query.filters.deviceId || query.filters.searchTerm);
  const canClear = Boolean(filters.user || filters.kind || filters.deviceId || filters.searchTerm || filters.status !== 'all' || isFiltered || query.filters.status !== 'all');
  const queryError = failed ? failure?.cause : null;

  useEffect(() => {
    if (searchTooLong) return;
    const timer = window.setTimeout(() => {
      setQuery((current) => current.filters.searchTerm === filters.searchTerm ? current : { ...current, filters: { ...current.filters, searchTerm: filters.searchTerm }, page: 0 });
    }, 300);
    return () => window.clearTimeout(timer);
  }, [filters.searchTerm, searchTooLong]);

  useEffect(() => {
    const controller = new AbortController();
    setLoaded(undefined);
    setFailure(undefined);
    void adminApi.getSessions({
      UserId: query.filters.user?.Id,
      Kind: query.filters.kind || undefined,
      Status: query.filters.status,
      DeviceId: query.filters.deviceId || undefined,
      SearchTerm: query.filters.searchTerm || undefined,
      StartIndex: query.page * query.pageSize,
      Limit: query.pageSize,
    }, { signal: controller.signal })
      .then((result) => {
        if (controller.signal.aborted) return;
        if (query.page > 0 && query.page * query.pageSize >= result.TotalRecordCount) {
          setQuery((current) => ({ ...current, page: Math.max(0, Math.ceil(result.TotalRecordCount / query.pageSize) - 1) }));
          return;
        }
        setLoaded({ requestKey, result });
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted && !isAbortError(cause)) setFailure({ requestKey, cause });
      });
    return () => controller.abort();
  }, [query, requestKey]);

  useEffect(() => {
    const controller = new AbortController();
    setUsersLoading(true);
    setUsersError(null);
    setUsers([]);
    void adminApi.getUsers({ signal: controller.signal })
      .then((result) => {
        if (!controller.signal.aborted) setUsers([...result.Items].sort((left, right) => left.Name.localeCompare(right.Name)));
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted && !isAbortError(cause)) setUsersError(cause);
      })
      .finally(() => { if (!controller.signal.aborted) setUsersLoading(false); });
    return () => controller.abort();
  }, [usersRevision]);

  function refresh() {
    setRevision((value) => value + 1);
    setUsersRevision((value) => value + 1);
  }

  function applyFilters(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (searchTooLong) return;
    setQuery((current) => ({ ...current, filters, page: 0 }));
    setRevision((value) => value + 1);
  }

  function resetFilters(status: SessionFilters['status'] = 'all') {
    const next = { ...initialFilters(), status };
    setFilters(next);
    setQuery((current) => ({ ...current, filters: next, page: 0 }));
    setRevision((value) => value + 1);
  }

  function changeFilter<K extends keyof SessionFilters>(name: K, value: SessionFilters[K]) {
    setFilters((current) => ({ ...current, [name]: value }));
  }

  return (
    <Box aria-busy={loading}>
      <PageHeading title="Sessions" description="Review user sign-ins and revoke individual logins. Revoking your current login returns you to sign in." />
      {failed && <Box sx={{ mb: 3 }}><ErrorNotice error={queryError} retry={refresh} /></Box>}
      <Paper variant="outlined" sx={{ overflow: 'hidden', borderRadius: '20px' }}>
        <Box component="form" onSubmit={applyFilters}>
          <AccessTableHeader title="Login sessions" count={data?.TotalRecordCount}>
            <Stack direction="row" sx={{ alignItems: 'flex-start', flexWrap: 'wrap', gap: 1 }}>
              <TextField id="sessions-status" select label="Status" size="small" value={filters.status} onChange={(event) => {
                const status = event.target.value as SessionFilters['status'];
                changeFilter('status', status);
                setQuery((current) => ({ ...current, filters: { ...current.filters, status }, page: 0 }));
              }} sx={{ minWidth: 138, '& .MuiOutlinedInput-root': { height: 40, borderRadius: '8px' } }}>
                {(['all', 'active', 'revoked', 'disabled', 'expired'] as const).map((status) => <MenuItem key={status} value={status}>{statusLabels[status]}</MenuItem>)}
              </TextField>
              <TextField id="sessions-search" name="SearchTerm" placeholder="Search user, client, or device" value={filters.searchTerm} onChange={(event) => changeFilter('searchTerm', event.target.value)} error={searchTooLong || Boolean(fieldError(queryError, 'SearchTerm'))} helperText={searchTooLong ? 'Use at most 256 UTF-8 bytes.' : fieldError(queryError, 'SearchTerm')} sx={accessSearchSx} slotProps={{ htmlInput: { 'aria-label': 'Search user, client, or device', autoComplete: 'off', spellCheck: false }, input: { startAdornment: <InputAdornment position="start"><IconButton type="submit" size="small" aria-label="Search sessions" disabled={searchTooLong} edge="start"><SearchRounded sx={{ fontSize: 18 }} /></IconButton></InputAdornment>, endAdornment: filters.searchTerm ? <InputAdornment position="end"><IconButton size="small" aria-label="Clear session search" onClick={() => { changeFilter('searchTerm', ''); setQuery((current) => ({ ...current, filters: { ...current.filters, searchTerm: '' }, page: 0 })); }} edge="end"><CloseRounded sx={{ fontSize: 16 }} /></IconButton></InputAdornment> : undefined } }} />
              <Tooltip title="More filters"><IconButton aria-label="More filters" aria-expanded={filtersExpanded} aria-controls="session-advanced-filters" onClick={() => setFiltersExpanded((value) => !value)} color={filtersExpanded || Boolean(filters.user || filters.kind || filters.deviceId) ? 'primary' : 'default'}><TuneRounded sx={{ fontSize: 20 }} /></IconButton></Tooltip>
              <Tooltip title="Refresh sessions"><span><IconButton onClick={refresh} disabled={loading} aria-label="Refresh sessions"><RefreshRounded sx={{ fontSize: 19 }} /></IconButton></span></Tooltip>
            </Stack>
          </AccessTableHeader>
          <Collapse in={filtersExpanded}>
            <Box id="session-advanced-filters" sx={{ px: { xs: 2, sm: 3 }, pb: 2.5 }}>
              {usersError != null && <Box sx={{ mb: 2 }}><Typography variant="body2" sx={{ mb: 1 }}>The user filter could not be loaded. Other filters are still available.</Typography><ErrorNotice error={usersError} retry={() => setUsersRevision((value) => value + 1)} /></Box>}
              <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', md: 'repeat(3, minmax(0, 1fr))' }, gap: 2, alignItems: 'start' }}>
                <Autocomplete<User> id="sessions-user" options={users} value={filters.user} loading={usersLoading} loadingText="Loading users..." noOptionsText={usersError ? 'User list unavailable' : 'No matching users'} getOptionLabel={(user) => user.Name} isOptionEqualToValue={(option, value) => option.Id === value.Id} onChange={(_event, user) => changeFilter('user', user)} renderInput={(params) => <TextField {...params} label="User" placeholder="All users" error={Boolean(fieldError(queryError, 'UserId'))} helperText={fieldError(queryError, 'UserId')} />} />
                <TextField id="sessions-kind" select fullWidth label="Login type" value={filters.kind || 'all'} onChange={(event) => changeFilter('kind', event.target.value === 'all' ? '' : event.target.value as LoginSessionKind)}>
                  <MenuItem value="all">All login types</MenuItem><MenuItem value="admin">Administrator dashboard</MenuItem><MenuItem value="emby">Emby client</MenuItem>
                </TextField>
                <TextField id="sessions-device-id" name="DeviceId" fullWidth label="Device ID" value={filters.deviceId} onChange={(event) => changeFilter('deviceId', event.target.value)} error={Boolean(fieldError(queryError, 'DeviceId'))} helperText={fieldError(queryError, 'DeviceId') ?? 'Matches this device ID exactly.'} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} />
              </Box>
              <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1, mt: 2 }}>
                <Button type="submit" variant="outlined" startIcon={<SearchRounded />} disabled={searchTooLong}>Apply filters</Button>
                <Button color="secondary" startIcon={<FilterAltOffOutlined />} onClick={() => resetFilters()} disabled={!canClear}>Clear filters</Button>
              </Stack>
            </Box>
          </Collapse>
        </Box>
        {loading && <Stack spacing={1} sx={{ p: 3 }} role="status" aria-live="polite" aria-label="Loading sessions"><Skeleton height={70} /><Skeleton height={70} /><Skeleton height={70} /></Stack>}
        {data && data.Items.length === 0 && (
          <Stack spacing={1.5} sx={{ alignItems: 'center', p: { xs: 3, sm: 5 }, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}>
            <DevicesOutlined sx={{ fontSize: 40, color: 'primary.main' }} />
            <Typography variant="h3" component="h3">{isFiltered ? 'No matching login sessions' : query.filters.status === 'all' ? 'No login sessions' : `No ${query.filters.status} login sessions`}</Typography>
            <Typography color="text.secondary" sx={{ maxWidth: 440 }}>{isFiltered ? 'Try another user, client, or device, or clear the filters.' : query.filters.status === 'all' ? 'Logins appear here after a user signs in.' : 'Select all statuses to review other login records.'}</Typography>
            {(isFiltered || query.filters.status !== 'all') && <Button startIcon={<FilterAltOffOutlined />} onClick={() => resetFilters()}>{isFiltered ? 'Clear filters' : 'Show all statuses'}</Button>}
          </Stack>
        )}
        {data && data.Items.length > 0 && <SessionsList items={data.Items} onRevoke={setRevoking} />}
        {failed && <Typography variant="body2" color="text.secondary" sx={{ px: { xs: 2.5, sm: 3 }, py: 3 }}>The session list could not be loaded. Retry the request to see current login records.</Typography>}
        {data && <TablePagination
          component="div"
          count={data.TotalRecordCount}
          page={query.page}
          rowsPerPage={query.pageSize}
          rowsPerPageOptions={[25, 50, 100, 200]}
          labelRowsPerPage="Sessions per page:"
          onPageChange={(_event, page) => setQuery((current) => ({ ...current, page }))}
          onRowsPerPageChange={(event) => {
            const pageSize = Number(event.target.value);
            if ([25, 50, 100, 200].includes(pageSize)) setQuery((current) => ({ ...current, pageSize, page: 0 }));
          }}
          sx={{ borderTop: 1, borderColor: 'divider', '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', justifyContent: 'flex-end', px: { xs: 2, sm: 3 }, py: 1, gap: 0.5 }, '& .MuiTablePagination-spacer': { display: { xs: 'none', sm: 'block' } }, '& .MuiTablePagination-actions': { ml: { xs: 1, sm: 2 } } }}
        />}
      </Paper>
      <Box sx={{ mt: 2 }}><AccessDetails label="About session activity"><Typography variant="caption">Active means a login is authorized; it does not show whether a user is online. Last activity is the last recorded token activity and may not change with administrator requests. Times use your local time zone. A user can sign in again after a login is revoked.</Typography></AccessDetails></Box>
      {revoking && <RevokeSessionDialog
        key={revoking.Id}
        session={revoking}
        onClose={() => setRevoking(undefined)}
        onRevoked={() => { setNotice(`Login revoked for ${revoking.UserName}.`); setRevoking(undefined); refresh(); }}
        onRefresh={() => { setRevoking(undefined); refresh(); }}
        onNavigationGuardChange={onNavigationGuardChange}
      />}
      <Snackbar open={Boolean(notice)} autoHideDuration={6000} onClose={() => setNotice('')} anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}><Alert severity="success" variant="filled" onClose={() => setNotice('')} sx={{ width: '100%', overflowWrap: 'anywhere' }}>{notice}</Alert></Snackbar>
    </Box>
  );
}
