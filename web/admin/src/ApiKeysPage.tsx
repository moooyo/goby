import { useEffect, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { Alert, Box, Button, CircularProgress, Collapse, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, IconButton, InputAdornment, Paper, Skeleton, Snackbar, Stack, Switch, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Tooltip, Typography } from './ui';
import { AddRounded } from './icons';
import { BlockOutlined } from './icons';
import { ContentCopyRounded } from './icons';
import { FilterAltOffOutlined } from './icons';
import { KeyRounded } from './icons';
import { RefreshRounded } from './icons';
import { SelectAllRounded } from './icons';
import { CloseRounded } from './icons';
import { SearchRounded } from './icons';
import { TuneRounded } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { ApplicationKey, ApplicationKeysResponse } from './api';
import { ErrorNotice, PageHeading } from './components';
import { fieldError } from './formFields';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { AccessBadge, AccessDetails, AccessIconTile, AccessTableHeader, accessSearchSx, accessTableSx } from './accessVisuals';
import { useTranslation } from './i18n';

function dateTime(value: string, locale: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? 'Unknown' : date.toLocaleString(locale, {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}

function RawValue({ value, fallback = 'Not recorded' }: { value?: string | null; fallback?: string }) {
  const { t } = useTranslation();
  return <span translate="no">{value || t(fallback)}</span>;
}

function uncertainMutation(error: unknown): boolean {
  return !(error instanceof ApiError)
    || ['network_error', 'invalid_response'].includes(error.code) || error.status >= 500;
}

function inputIssue(value: string): string | undefined {
  if (/[\u0000-\u001f\u007f-\u009f]/u.test(value.trim())) return 'Control characters are not allowed.';
  if (new TextEncoder().encode(value.trim()).length > 256) return 'Use at most 256 UTF-8 bytes. Some characters use more than one byte.';
  return undefined;
}

function KeyStatus({ apiKey }: { apiKey: ApplicationKey }) {
  return <AccessBadge label={apiKey.Status === 'active' ? 'Active' : 'Revoked'} tone={apiKey.Status === 'active' ? 'success' : 'neutral'} />;
}

function KeyIdentity({ apiKey }: { apiKey: ApplicationKey }) {
  return (
    <Stack direction="row" sx={{ alignItems: 'center', gap: 1.5, minWidth: 0 }}>
      <AccessIconTile><KeyRounded /></AccessIconTile>
      <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
        <Typography variant="body2" sx={{ fontWeight: 600 }}><RawValue value={apiKey.AppName} /></Typography>
        {apiKey.Status === 'revoked' && <Box sx={{ mt: 0.25 }}><KeyStatus apiKey={apiKey} /></Box>}
      </Box>
    </Stack>
  );
}

function KeyHistory({ apiKey }: { apiKey: ApplicationKey }) {
  const { locale } = useTranslation();
  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography variant="body2" sx={{ fontSize: 12.5 }}>Created <Box component="time" dateTime={apiKey.CreatedAt} title={apiKey.CreatedAt} sx={{ whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>{dateTime(apiKey.CreatedAt, locale)}</Box></Typography>
      <Typography variant="caption" component="div" color="text.secondary">Last used {apiKey.LastUsedAt ? <Box component="time" dateTime={apiKey.LastUsedAt} title={apiKey.LastUsedAt} sx={{ whiteSpace: 'nowrap' }}>{dateTime(apiKey.LastUsedAt, locale)}</Box> : 'Never'}</Typography>
      <AccessDetails label="Key details">
        <Typography variant="caption" component="div" sx={{ overflowWrap: 'anywhere' }}>Key ID: <span className="mono"><RawValue value={apiKey.Id} /></span></Typography>
        <Typography variant="caption" component="div" sx={{ overflowWrap: 'anywhere' }}>IP address: <span className="mono"><RawValue value={apiKey.IPAddress} /></span></Typography>
        <Typography variant="caption" component="div" sx={{ overflowWrap: 'anywhere' }}>Created by: <span className="mono"><RawValue value={apiKey.CreatedBy} /></span></Typography>
        {apiKey.RevokedAt && <Typography variant="caption" component="div">Revoked <Box component="time" dateTime={apiKey.RevokedAt} title={apiKey.RevokedAt} sx={{ whiteSpace: 'nowrap' }}>{dateTime(apiKey.RevokedAt, locale)}</Box></Typography>}
        <Typography variant="caption" component="div">Server-wide access</Typography>
      </AccessDetails>
    </Box>
  );
}

function KeyActions({ apiKey, onRevoke }: { apiKey: ApplicationKey; onRevoke: (apiKey: ApplicationKey) => void }) {
  const { t } = useTranslation();
  if (apiKey.Status === 'revoked') return null;
  return (
    <Stack direction="row" sx={{ justifyContent: 'flex-end', gap: 0.5 }}>
      <Button size="small" color="error" onClick={() => onRevoke(apiKey)} aria-label={`${t('Revoke key')} ${apiKey.AppName}`} sx={{ px: 1.75, minHeight: 28, height: 28 }}>Revoke</Button>
    </Stack>
  );
}

function KeysList({ items, onRevoke }: { items: ApplicationKey[]; onRevoke: (apiKey: ApplicationKey) => void }) {
  const { locale, t } = useTranslation();
  return (
    <>
      <Box component="ul" className="access-record-cards" aria-label="Application API keys" sx={{ display: { xs: 'block', lg: 'none' }, listStyle: 'none', p: 0, m: 0 }}>
        {items.map((apiKey) => (
          <Box component="li" className="access-compact-record" key={apiKey.Id} sx={{ px: 2, py: 1.5, borderTop: 1, borderColor: 'var(--line)' }}>
            <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'flex-start', gap: 1.5 }}>
              <KeyIdentity apiKey={apiKey} />
              <KeyActions apiKey={apiKey} onRevoke={onRevoke} />
            </Stack>
            <Box sx={{ mt: 1, pl: '48px' }}><KeyHistory apiKey={apiKey} /></Box>
          </Box>
        ))}
      </Box>
      <TableContainer className="access-record-table" sx={{ display: { xs: 'none', lg: 'block' } }}>
        <Table className="access-data-table" aria-label="Application API keys" sx={{ minWidth: 900, tableLayout: 'fixed', ...accessTableSx }}>
          <TableHead><TableRow><TableCell sx={{ width: '25%' }}>Name</TableCell><TableCell sx={{ width: '13%' }}>Key</TableCell><TableCell sx={{ width: '18%' }}>Created</TableCell><TableCell sx={{ width: '20%' }}>Last used</TableCell><TableCell sx={{ width: '14%' }}>Source</TableCell><TableCell align="right" sx={{ width: '10%' }}><span className="visually-hidden">Actions</span></TableCell></TableRow></TableHead>
          <TableBody>
            {items.map((apiKey) => (
              <TableRow key={apiKey.Id}>
                <TableCell component="th" scope="row"><KeyIdentity apiKey={apiKey} /></TableCell>
                <TableCell><Typography component="span" className="mono" color="text.secondary" aria-label={t('Key hidden')} sx={{ fontSize: 12 }}>••••••••</Typography></TableCell>
                <TableCell><Typography component="time" dateTime={apiKey.CreatedAt} title={apiKey.CreatedAt} sx={{ color: 'var(--ink2)', fontSize: 12.5 }}>{dateTime(apiKey.CreatedAt, locale)}</Typography></TableCell>
                <TableCell>{apiKey.LastUsedAt ? <Typography component="time" dateTime={apiKey.LastUsedAt} title={apiKey.LastUsedAt} sx={{ color: 'var(--ink2)', fontSize: 12.5 }}>{dateTime(apiKey.LastUsedAt, locale)}</Typography> : <Typography variant="body2" color="text.secondary">Never</Typography>}</TableCell>
                <TableCell><Typography className="mono" color="text.secondary" sx={{ fontSize: 11.5, overflowWrap: 'anywhere' }}><RawValue value={apiKey.IPAddress} /></Typography></TableCell>
                <TableCell align="right"><KeyActions apiKey={apiKey} onRevoke={onRevoke} /></TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </>
  );
}

function SecretValue({ accessToken }: { accessToken: string }) {
  const input = useRef<HTMLInputElement | HTMLTextAreaElement>(null);
  const mounted = useRef(true);
  const [copying, setCopying] = useState(false);
  const [copyState, setCopyState] = useState<'copied' | 'manual' | ''>('');
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  function selectKey() {
    input.current?.focus();
    input.current?.select();
  }

  async function copyKey() {
    if (copying) return;
    setCopying(true);
    setCopyState('');
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(accessToken);
      if (mounted.current) setCopyState('copied');
    } catch {
      if (mounted.current) {
        selectKey();
        setCopyState('manual');
      }
    } finally {
      if (mounted.current) setCopying(false);
    }
  }

  return (
    <Stack spacing={2}>
      <TextField
        id="api-key-access-token"
        label="Access token"
        value={accessToken}
        inputRef={input}
        fullWidth
        multiline
        minRows={2}
        maxRows={5}
        autoFocus
        onFocus={(event) => event.target.select()}
        slotProps={{ input: { readOnly: true }, htmlInput: { autoComplete: 'off', spellCheck: false, autoCapitalize: 'none' } }}
        sx={{ '& textarea': { fontFamily: 'monospace', overflowWrap: 'anywhere' } }}
      />
      <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}>
        <Button variant="outlined" startIcon={<ContentCopyRounded />} onClick={() => void copyKey()} disabled={copying}>Copy key</Button>
        <Button color="secondary" startIcon={<SelectAllRounded />} onClick={selectKey}>Select key</Button>
      </Stack>
      {copyState === 'copied' && <Alert severity="success" role="status">API key copied.</Alert>}
      {copyState === 'manual' && <Alert severity="info" role="status">Clipboard access is unavailable. The key is selected. Press Ctrl+C or use your device's copy command.</Alert>}
      <Typography variant="body2" color="text.secondary">Copy this key now. The complete key is shown only in this dialog and will be cleared when you close it.</Typography>
    </Stack>
  );
}

function CreateApiKeyDialog({ onClose, onCreated, onCheckKeys, onNavigationGuardChange }: { onClose: () => void; onCreated: (apiKey: ApplicationKey) => void; onCheckKeys: (appName: string) => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [appName, setAppName] = useState('');
  const [createdKey, setCreatedKey] = useState<ApplicationKey>();
  const [accessToken, setAccessToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const inFlight = useRef<AbortController | undefined>(undefined);
  const outcomeUnknown = error != null && uncertainMutation(error);
  const nameIssue = inputIssue(appName);
  useUserDraftNavigation(false, busy, onNavigationGuardChange);
  useEffect(() => () => inFlight.current?.abort(), []);

  function close() {
    if (inFlight.current) return;
    setAccessToken('');
    if (outcomeUnknown) onCheckKeys(appName.trim());
    else onClose();
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || outcomeUnknown || createdKey || !appName.trim() || nameIssue) return;
    const controller = new AbortController();
    inFlight.current = controller;
    setBusy(true);
    setError(null);
    try {
      const result = await adminApi.createApplicationKey({ AppName: appName.trim() }, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setCreatedKey(result.Key);
      setAccessToken(result.AccessToken);
      onCreated(result.Key);
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) setError(cause);
    } finally {
      if (inFlight.current === controller) inFlight.current = undefined;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  return (
    <Dialog open onClose={close} fullWidth maxWidth="sm" data-layout="modal" aria-labelledby="create-api-key-title" aria-describedby="create-api-key-description">
      <Box component="form" onSubmit={create} aria-busy={busy}>
        <DialogTitle id="create-api-key-title" sx={{ pt: 3 }}>{createdKey ? 'API key created' : 'Create API key'}</DialogTitle>
        <DialogContent>
          <Stack spacing={2.5} sx={{ pt: 0.5 }}>
            <Typography id="create-api-key-description" variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>{createdKey ? <>Use this key to connect <RawValue value={createdKey.AppName} /> to your server.</> : 'Give this key a name that identifies the application using it.'}</Typography>
            <Alert severity="info">API keys grant server-wide access independently of user accounts. Anyone with this key can access the server through compatible clients.</Alert>
            {createdKey ? <SecretValue accessToken={accessToken} /> : <TextField autoFocus fullWidth required id="api-key-app-name" name="AppName" label="Application name" value={appName} disabled={busy || outcomeUnknown} onChange={(event) => { setAppName(event.target.value); setError(null); }} error={Boolean(nameIssue || fieldError(error, 'AppName'))} helperText={nameIssue ?? fieldError(error, 'AppName') ?? 'For example, Living room media app.'} slotProps={{ htmlInput: { autoComplete: 'off' } }} />}
            {error != null && <ErrorNotice error={error} />}
            {outcomeUnknown && <Alert severity="warning">The response could not be confirmed. This API key may already have been created. Check the key list for this application before creating another key.</Alert>}
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 3, flexWrap: 'wrap', gap: 1 }}>
          <Button onClick={close} color="secondary" disabled={busy}>{createdKey || outcomeUnknown ? 'Close' : 'Cancel'}</Button>
          {!createdKey && (outcomeUnknown
            ? <Button variant="contained" startIcon={<RefreshRounded />} onClick={close}>Check API keys</Button>
            : <Button type="submit" variant="contained" disabled={busy || !appName.trim() || Boolean(nameIssue)} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <AddRounded />}>{busy ? 'Creating API key...' : 'Create API key'}</Button>)}
        </DialogActions>
      </Box>
    </Dialog>
  );
}

function RevokeApiKeyDialog({ apiKey, onClose, onRevoked, onRefresh, onNavigationGuardChange }: { apiKey: ApplicationKey; onClose: () => void; onRevoked: () => void; onRefresh: () => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const inFlight = useRef<AbortController | undefined>(undefined);
  const refreshRequired = error != null && (uncertainMutation(error) || (error instanceof ApiError && [404, 409].includes(error.status)));
  useUserDraftNavigation(false, busy, onNavigationGuardChange);
  useEffect(() => () => inFlight.current?.abort(), []);

  function close() {
    if (inFlight.current) return;
    if (refreshRequired) onRefresh();
    else onClose();
  }

  async function revoke() {
    if (inFlight.current || refreshRequired) return;
    const controller = new AbortController();
    inFlight.current = controller;
    setBusy(true);
    setError(null);
    try {
      await adminApi.revokeApplicationKey(apiKey.Id, { signal: controller.signal });
      if (!controller.signal.aborted) onRevoked();
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) setError(cause);
    } finally {
      if (inFlight.current === controller) inFlight.current = undefined;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  return (
    <Dialog open onClose={close} fullWidth maxWidth="sm" data-layout="modal" aria-labelledby="revoke-api-key-title" aria-describedby="revoke-api-key-description">
      <Box aria-busy={busy}>
        <DialogTitle id="revoke-api-key-title" sx={{ pt: 3 }}>Revoke API key?</DialogTitle>
        <DialogContent>
          <Stack spacing={2.5}>
            <Typography id="revoke-api-key-description" variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>Revoke the API key for <RawValue value={apiKey.AppName} />?</Typography>
            <Paper variant="outlined" sx={{ p: 2.5, bgcolor: 'background.default' }}><Stack spacing={2}><Box><KeyStatus apiKey={apiKey} /></Box><KeyIdentity apiKey={apiKey} /><KeyHistory apiKey={apiKey} /></Stack></Paper>
            <Alert severity="warning">This permanently removes access for every client using this key, including playback requests. All copies of the key stop working. Create a new key to restore access.</Alert>
            {error != null && <ErrorNotice error={error} />}
            {refreshRequired && <Alert severity="warning">The current key status could not be confirmed. It may already be revoked. Refresh the key list before trying again.</Alert>}
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 3, flexWrap: 'wrap', gap: 1 }}>
          <Button onClick={close} color="secondary" disabled={busy} autoFocus>Cancel</Button>
          {refreshRequired
            ? <Button variant="contained" startIcon={<RefreshRounded />} onClick={onRefresh}>Refresh API keys</Button>
            : <Button variant="contained" color="error" onClick={() => void revoke()} disabled={busy} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <BlockOutlined />}>{busy ? 'Revoking API key...' : 'Revoke key'}</Button>}
        </DialogActions>
      </Box>
    </Dialog>
  );
}

export function ApiKeysPage({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const [searchTerm, setSearchTerm] = useState('');
  const [query, setQuery] = useState({ searchTerm: '', includeRevoked: true, page: 0, pageSize: 50 });
  const [loaded, setLoaded] = useState<{ requestKey: string; result: ApplicationKeysResponse }>();
  const [failure, setFailure] = useState<{ requestKey: string; cause: unknown }>();
  const [revision, setRevision] = useState(0);
  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState<ApplicationKey>();
  const [notice, setNotice] = useState<ReactNode>('');
  const [filtersExpanded, setFiltersExpanded] = useState(false);
  const requestKey = JSON.stringify([query.searchTerm, query.includeRevoked, query.page, query.pageSize, revision]);
  const data = loaded?.requestKey === requestKey ? loaded.result : undefined;
  const failed = failure?.requestKey === requestKey;
  const loading = !data && !failed;
  const queryError = failed ? failure?.cause : null;
  const canClear = Boolean(searchTerm || query.searchTerm || !query.includeRevoked);
  const searchIssue = inputIssue(searchTerm);

  useEffect(() => {
    if (searchIssue) return;
    const timer = window.setTimeout(() => {
      const nextSearchTerm = searchTerm.trim();
      setQuery((current) => current.searchTerm === nextSearchTerm ? current : { ...current, searchTerm: nextSearchTerm, page: 0 });
    }, 300);
    return () => window.clearTimeout(timer);
  }, [searchTerm, searchIssue]);

  useEffect(() => {
    const controller = new AbortController();
    setLoaded(undefined);
    setFailure(undefined);
    void adminApi.getApplicationKeys({
      SearchTerm: query.searchTerm || undefined,
      IncludeRevoked: query.includeRevoked,
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
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setFailure({ requestKey, cause }); });
    return () => controller.abort();
  }, [query, requestKey]);

  function refresh() {
    setRevision((value) => value + 1);
  }

  function clearFilters() {
    setSearchTerm('');
    setQuery((current) => ({ ...current, searchTerm: '', includeRevoked: true, page: 0 }));
  }

  function applySearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (searchIssue) return;
    setQuery((current) => ({ ...current, searchTerm: searchTerm.trim(), page: 0 }));
  }

  return (
    <Box aria-busy={loading}>
      <PageHeading title="API keys" description="Create a separate key for each application. Clients send it in the X-Emby-Token request header." action={<Button variant="contained" startIcon={<AddRounded />} onClick={() => setCreating(true)}>Create API key</Button>} />
      {failed && <Box sx={{ mb: 3 }}><ErrorNotice error={queryError} retry={refresh} /></Box>}
      <Paper variant="outlined" sx={{ overflow: 'hidden', borderRadius: '14px', bgcolor: 'var(--s2)', borderColor: 'var(--line)' }}>
        <AccessTableHeader title="Application keys" count={data?.TotalRecordCount}>
          <Stack direction="row" sx={{ alignItems: 'flex-start', gap: 0.75 }}>
            <Box component="form" onSubmit={applySearch} sx={{ flex: 1, minWidth: 0 }}>
              <TextField id="api-keys-search" name="SearchTerm" placeholder="Search by application name" value={searchTerm} onChange={(event) => setSearchTerm(event.target.value)} error={Boolean(searchIssue || fieldError(queryError, 'SearchTerm'))} helperText={searchIssue ?? fieldError(queryError, 'SearchTerm')} sx={accessSearchSx} slotProps={{ htmlInput: { 'aria-label': 'Search API keys', autoComplete: 'off', spellCheck: false }, input: { startAdornment: <InputAdornment position="start"><SearchRounded sx={{ fontSize: 18 }} /></InputAdornment>, endAdornment: searchTerm ? <InputAdornment position="end"><IconButton size="small" aria-label="Clear key search" onClick={() => { setSearchTerm(''); setQuery((current) => ({ ...current, searchTerm: '', page: 0 })); }} edge="end"><CloseRounded sx={{ fontSize: 16 }} /></IconButton></InputAdornment> : undefined } }} />
            </Box>
            <Tooltip title="More filters"><IconButton aria-label="More filters" aria-expanded={filtersExpanded} aria-controls="api-key-filters" onClick={() => setFiltersExpanded((value) => !value)} color={filtersExpanded || !query.includeRevoked ? 'primary' : 'default'}><TuneRounded sx={{ fontSize: 20 }} /></IconButton></Tooltip>
            <Tooltip title="Refresh API keys"><span><IconButton onClick={refresh} disabled={loading} aria-label="Refresh API keys"><RefreshRounded sx={{ fontSize: 19 }} /></IconButton></span></Tooltip>
          </Stack>
        </AccessTableHeader>
        <Collapse in={filtersExpanded}>
          <Stack id="api-key-filters" direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1, px: { xs: 2, sm: 3 }, pb: 2 }}>
            <FormControlLabel control={<Switch checked={query.includeRevoked} onChange={(event) => setQuery((current) => ({ ...current, includeRevoked: event.target.checked, page: 0 }))} />} label="Include revoked" />
            <Button color="secondary" startIcon={<FilterAltOffOutlined />} onClick={clearFilters} disabled={!canClear}>Clear filters</Button>
          </Stack>
        </Collapse>
        {loading && <Stack spacing={1} sx={{ p: 3, borderTop: 1, borderColor: 'divider' }} role="status" aria-live="polite" aria-label="Loading API keys"><Skeleton height={52} /><Skeleton height={52} /><Skeleton height={52} /></Stack>}
        {data && data.Items.length === 0 && (
          <Stack spacing={1.5} sx={{ alignItems: 'center', p: { xs: 3, sm: 5 }, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}>
            <KeyRounded sx={{ fontSize: 40, color: 'primary.main' }} />
            <Typography variant="h3" component="h3">{query.searchTerm ? 'No matching API keys' : query.includeRevoked ? 'No API keys' : 'No active API keys'}</Typography>
            <Typography color="text.secondary" sx={{ maxWidth: 440 }}>{query.searchTerm ? 'Try another application name or clear the filters.' : 'Create an API key to connect an application to this server.'}</Typography>
            {query.searchTerm ? <Button startIcon={<FilterAltOffOutlined />} onClick={clearFilters}>Clear filters</Button> : <Button startIcon={<AddRounded />} onClick={() => setCreating(true)}>Create API key</Button>}
          </Stack>
        )}
        {data && data.Items.length > 0 && <KeysList items={data.Items} onRevoke={setRevoking} />}
        {failed && <Typography variant="body2" color="text.secondary" sx={{ px: { xs: 2.5, sm: 3 }, py: 3 }}>The API key list could not be loaded. Retry the request to see current application access.</Typography>}
        {data && <TablePagination
          component="div"
          count={data.TotalRecordCount}
          page={query.page}
          rowsPerPage={query.pageSize}
          rowsPerPageOptions={[25, 50, 100, 200]}
          labelRowsPerPage="Keys per page:"
          onPageChange={(_event, page) => setQuery((current) => ({ ...current, page }))}
          onRowsPerPageChange={(event) => {
            const pageSize = Number(event.target.value);
            if ([25, 50, 100, 200].includes(pageSize)) setQuery((current) => ({ ...current, pageSize, page: 0 }));
          }}
          sx={{ borderTop: 1, borderColor: 'divider', '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', justifyContent: 'flex-end', px: { xs: 2, sm: 3 }, py: 1, gap: 0.5 }, '& .MuiTablePagination-spacer': { display: { xs: 'none', sm: 'block' } }, '& .MuiTablePagination-actions': { ml: { xs: 1, sm: 2 } } }}
        />}
      </Paper>
      <Box sx={{ mt: 2 }}><AccessDetails label="About application access"><Typography variant="caption">API keys grant server-wide access independently of user accounts. The complete key appears only after creation. Last used records activity, not a live connection.</Typography></AccessDetails></Box>
      {creating && <CreateApiKeyDialog
        onClose={() => setCreating(false)}
        onCreated={(apiKey) => { setNotice(<>API key created for <RawValue value={apiKey.AppName} />.</>); refresh(); }}
        onCheckKeys={(appName) => { setCreating(false); setSearchTerm(appName); setQuery((current) => ({ ...current, searchTerm: appName, includeRevoked: true, page: 0 })); refresh(); }}
        onNavigationGuardChange={onNavigationGuardChange}
      />}
      {revoking && <RevokeApiKeyDialog key={revoking.Id} apiKey={revoking} onClose={() => setRevoking(undefined)} onRevoked={() => { setNotice(<>API key revoked for <RawValue value={revoking.AppName} />.</>); setRevoking(undefined); refresh(); }} onRefresh={() => { setRevoking(undefined); refresh(); }} onNavigationGuardChange={onNavigationGuardChange} />}
      <Snackbar open={Boolean(notice) && !creating && !revoking} autoHideDuration={6000} onClose={() => setNotice('')} anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}><Alert severity="success" variant="filled" onClose={() => setNotice('')} sx={{ width: '100%', overflowWrap: 'anywhere' }}>{notice}</Alert></Snackbar>
    </Box>
  );
}
