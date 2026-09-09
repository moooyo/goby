import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Autocomplete, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, MenuItem, Paper, Skeleton, Snackbar, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Typography } from '@mui/material';
import DevicesOutlined from '@mui/icons-material/DevicesOutlined';
import FilterAltOffOutlined from '@mui/icons-material/FilterAltOffOutlined';
import LogoutRounded from '@mui/icons-material/LogoutRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import SearchRounded from '@mui/icons-material/SearchRounded';
import { adminApi, ApiError, isAbortError } from './api';
import type { LoginSession, LoginSessionKind, LoginSessionStatus, SessionsResponse, User } from './api';
import { ErrorNotice, PageHeading } from './components';
import { fieldError } from './formFields';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

interface SessionFilters {
  user: User | null;
  kind: LoginSessionKind | '';
  status: LoginSessionStatus | 'all';
  deviceId: string;
  searchTerm: string;
}

function initialFilters(): SessionFilters {
  return { user: null, kind: '', status: 'active', deviceId: '', searchTerm: '' };
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
  return <Chip label={statusLabels[status]} size="small" variant="outlined" color={status === 'active' ? 'success' : status === 'disabled' ? 'warning' : 'default'} />;
}

function SessionIdentity({ session }: { session: LoginSession }) {
  return (
    <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
      <Typography variant="body2" sx={{ fontWeight: 650 }}>{session.UserName || 'Unnamed user'}</Typography>
      <Typography variant="caption" color="text.secondary">{session.UserIsAdministrator ? 'Administrator' : 'Member'}{session.UserIsDisabled ? ' · User disabled' : ''}</Typography>
      <Typography variant="body2" sx={{ mt: 1 }}>{session.Client || 'Client not reported'}</Typography>
      <Typography variant="caption" color="text.secondary">{kindLabel(session.Kind)}{session.ApplicationVersion ? ` · ${session.ApplicationVersion}` : ''}</Typography>
    </Box>
  );
}

function SessionDevice({ session }: { session: LoginSession }) {
  return (
    <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
      <Typography variant="body2">{session.DeviceName || 'Device name not reported'}</Typography>
      <Typography component="div" variant="caption" color="text.secondary" sx={{ mt: 0.5 }}>
        Device ID: <span className="mono">{session.DeviceId || 'Not reported'}</span>
      </Typography>
    </Box>
  );
}

function SessionHistory({ session }: { session: LoginSession }) {
  const events = [
    ['Created', session.CreatedAt],
    ['Last activity', session.LastSeenAt],
    ['Expires', session.ExpiresAt],
    ...(session.RevokedAt ? [['Revoked', session.RevokedAt]] : []),
  ];
  return (
    <Box component="dl" sx={{ m: 0, display: 'grid', gridTemplateColumns: 'max-content minmax(0, 1fr)', columnGap: 1.5, rowGap: 0.6 }}>
      {events.map(([label, value]) => (
        <Box key={label} sx={{ display: 'contents' }}>
          <Typography component="dt" variant="caption" color="text.secondary">{label}</Typography>
          <Typography component="dd" variant="caption" sx={{ m: 0, overflowWrap: 'anywhere', fontVariantNumeric: 'tabular-nums' }}><time dateTime={value} title={value}>{dateTime(value)}</time></Typography>
        </Box>
      ))}
    </Box>
  );
}

function RevokeButton({ session, onRevoke }: { session: LoginSession; onRevoke: (session: LoginSession) => void }) {
  if (session.Status === 'revoked') return null;
  return <Button size="small" color="error" startIcon={<LogoutRounded />} onClick={() => onRevoke(session)} aria-label={`Revoke login for ${session.UserName} on ${session.DeviceName || session.Client || 'unreported device'}`} sx={{ px: 1 }}>Revoke login</Button>;
}

function SessionsList({ items, onRevoke }: { items: LoginSession[]; onRevoke: (session: LoginSession) => void }) {
  return (
    <>
      <Box component="ul" aria-label="Login sessions" sx={{ display: { xs: 'block', lg: 'none' }, listStyle: 'none', p: 0, m: 0 }}>
        {items.map((session) => (
          <Box component="li" key={session.Id} sx={{ p: 2.5, borderTop: 1, borderColor: 'divider' }}>
            <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: 1.5 }}>
              <SessionIdentity session={session} />
              <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.7 }}><StatusChip status={session.Status} />{session.IsCurrent && <Chip label="This login" size="small" sx={{ bgcolor: 'background.default' }} />}</Stack>
            </Stack>
            <Box sx={{ mt: 2 }}><SessionDevice session={session} /></Box>
            <Box sx={{ mt: 2, p: 1.5, borderRadius: 1, bgcolor: 'background.default' }}><SessionHistory session={session} /></Box>
            {session.Status !== 'revoked' && <Box sx={{ mt: 1.5, ml: -1 }}><RevokeButton session={session} onRevoke={onRevoke} /></Box>}
          </Box>
        ))}
      </Box>
      <TableContainer sx={{ display: { xs: 'none', lg: 'block' } }}>
        <Table aria-label="Login sessions" sx={{ minWidth: 820, tableLayout: 'fixed' }}>
          <TableHead><TableRow><TableCell sx={{ pl: 3, width: '23%' }}>User and client</TableCell><TableCell sx={{ width: '23%' }}>Device</TableCell><TableCell sx={{ width: '34%' }}>Login history</TableCell><TableCell sx={{ pr: 3, width: '20%' }}>Access</TableCell></TableRow></TableHead>
          <TableBody>
            {items.map((session) => (
              <TableRow key={session.Id} sx={{ '& > .MuiTableCell-root': { verticalAlign: 'top' } }}>
                <TableCell component="th" scope="row" sx={{ pl: 3 }}><SessionIdentity session={session} /></TableCell>
                <TableCell><SessionDevice session={session} /></TableCell>
                <TableCell><SessionHistory session={session} /></TableCell>
                <TableCell sx={{ pr: 3 }}>
                  <Stack sx={{ alignItems: 'flex-start', gap: 0.8 }}><StatusChip status={session.Status} />{session.IsCurrent && <Chip label="This login" size="small" sx={{ bgcolor: 'background.default' }} />}</Stack>
                  {session.Status !== 'revoked' && <Box sx={{ mt: 1, ml: -1 }}><RevokeButton session={session} onRevoke={onRevoke} /></Box>}
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
  const requestKey = JSON.stringify([query.filters.user?.Id, query.filters.kind, query.filters.status, query.filters.deviceId, query.filters.searchTerm, query.page, query.pageSize, revision]);
  const data = loaded?.requestKey === requestKey ? loaded.result : undefined;
  const failed = failure?.requestKey === requestKey;
  const loading = !data && !failed;
  const searchTooLong = new TextEncoder().encode(filters.searchTerm).length > 256;
  const isFiltered = Boolean(query.filters.user || query.filters.kind || query.filters.deviceId || query.filters.searchTerm);
  const canClear = Boolean(filters.user || filters.kind || filters.deviceId || filters.searchTerm || filters.status !== 'active' || isFiltered || query.filters.status !== 'active');
  const queryError = failed ? failure?.cause : null;

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

  function resetFilters(status: SessionFilters['status'] = 'active') {
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
      <PageHeading title="Sessions" description="Review user logins and revoke access for an individual login." />
      {failed && <Box sx={{ mb: 3 }}><ErrorNotice error={queryError} retry={refresh} /></Box>}
      <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
        <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1, px: { xs: 2.5, sm: 3 }, py: 2.2 }}>
          <Stack direction="row" sx={{ alignItems: 'center', gap: 1.2 }}><Typography variant="h4" component="h2">Login sessions</Typography>{data && <Chip label={data.TotalRecordCount.toLocaleString()} size="small" sx={{ bgcolor: 'background.default' }} />}</Stack>
          <Button size="small" startIcon={<RefreshRounded />} onClick={refresh} disabled={loading}>Refresh</Button>
        </Stack>
        <Box component="form" onSubmit={applyFilters} sx={{ px: { xs: 2.5, sm: 3 }, pb: 2.5 }}>
          {usersError != null && <Box sx={{ mb: 2 }}><Typography variant="body2" sx={{ mb: 1 }}>The user filter could not be loaded. Other filters are still available.</Typography><ErrorNotice error={usersError} retry={() => setUsersRevision((value) => value + 1)} /></Box>}
          <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'repeat(2, minmax(0, 1fr))', xl: 'repeat(3, minmax(0, 1fr))' }, gap: 2, alignItems: 'start' }}>
            <Autocomplete<User>
              id="sessions-user"
              options={users}
              value={filters.user}
              loading={usersLoading}
              loadingText="Loading users..."
              noOptionsText={usersError ? 'User list unavailable' : 'No matching users'}
              getOptionLabel={(user) => user.Name}
              isOptionEqualToValue={(option, value) => option.Id === value.Id}
              onChange={(_event, user) => changeFilter('user', user)}
              renderInput={(params) => <TextField {...params} label="User" placeholder="All users" error={Boolean(fieldError(queryError, 'UserId'))} helperText={fieldError(queryError, 'UserId')} />}
              sx={{ gridColumn: { sm: '1 / -1', xl: 'auto' } }}
            />
            <TextField id="sessions-kind" select fullWidth label="Login type" value={filters.kind || 'all'} onChange={(event) => changeFilter('kind', event.target.value === 'all' ? '' : event.target.value as LoginSessionKind)}>
              <MenuItem value="all">All login types</MenuItem><MenuItem value="admin">Administrator dashboard</MenuItem><MenuItem value="emby">Emby client</MenuItem>
            </TextField>
            <TextField id="sessions-status" select fullWidth label="Status" value={filters.status} onChange={(event) => changeFilter('status', event.target.value as SessionFilters['status'])}>
              {(['active', 'revoked', 'disabled', 'expired', 'all'] as const).map((status) => <MenuItem key={status} value={status}>{statusLabels[status]}</MenuItem>)}
            </TextField>
            <TextField id="sessions-search" name="SearchTerm" fullWidth label="Search user, client, or device" value={filters.searchTerm} onChange={(event) => changeFilter('searchTerm', event.target.value)} error={searchTooLong || Boolean(fieldError(queryError, 'SearchTerm'))} helperText={searchTooLong ? 'Use at most 256 UTF-8 bytes. Some characters use more than one byte.' : fieldError(queryError, 'SearchTerm') ?? 'Matches literal text, including spaces. Versions are also searchable.'} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} sx={{ gridColumn: { xs: 'auto', sm: '1 / -1', xl: 'span 2' } }} />
            <TextField id="sessions-device-id" name="DeviceId" fullWidth label="Device ID" value={filters.deviceId} onChange={(event) => changeFilter('deviceId', event.target.value)} error={Boolean(fieldError(queryError, 'DeviceId'))} helperText={fieldError(queryError, 'DeviceId') ?? 'Matches this device ID exactly.'} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} sx={{ gridColumn: { sm: '1 / -1', xl: 'auto' } }} />
          </Box>
          <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1, mt: 2 }}>
            <Button type="submit" variant="outlined" startIcon={<SearchRounded />} disabled={searchTooLong}>Apply filters</Button>
            <Button color="secondary" startIcon={<FilterAltOffOutlined />} onClick={() => resetFilters()} disabled={!canClear}>Clear filters</Button>
          </Stack>
        </Box>
        <Box sx={{ px: { xs: 2.5, sm: 3 }, py: 1.5, borderTop: 1, borderColor: 'divider', bgcolor: 'background.default' }}>
          <Typography variant="body2" color="text.secondary">Active means a login is authorized; it does not show whether a user is online. Last activity is the last recorded token activity and may not change with administrator requests.</Typography>
        </Box>
        {loading && <Stack spacing={1} sx={{ p: 3 }} role="status" aria-live="polite" aria-label="Loading sessions"><Skeleton height={70} /><Skeleton height={70} /><Skeleton height={70} /></Stack>}
        {data && data.Items.length === 0 && (
          <Stack spacing={1.5} sx={{ alignItems: 'center', p: { xs: 3, sm: 5 }, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}>
            <DevicesOutlined sx={{ fontSize: 40, color: 'primary.main' }} />
            <Typography variant="h3" component="h3">{isFiltered ? 'No matching login sessions' : query.filters.status === 'all' ? 'No login sessions' : `No ${query.filters.status} login sessions`}</Typography>
            <Typography color="text.secondary" sx={{ maxWidth: 440 }}>{isFiltered ? 'Try another user, client, or device, or clear the filters.' : query.filters.status === 'all' ? 'Logins appear here after a user signs in.' : 'Select all statuses to review other login records.'}</Typography>
            {(isFiltered || query.filters.status !== 'all') && <Button startIcon={<FilterAltOffOutlined />} onClick={() => resetFilters(isFiltered ? 'active' : 'all')}>{isFiltered ? 'Clear filters' : 'Show all statuses'}</Button>}
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
      <Typography variant="body2" color="text.secondary" sx={{ mt: 2.5, px: 0.5 }}>Times use your local time zone. Revoking a login ends only that login; the user can sign in again on the same device.</Typography>
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
