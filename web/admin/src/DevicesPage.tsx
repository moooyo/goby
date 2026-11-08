import { useEffect, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { Alert, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, InputAdornment, Paper, Skeleton, Snackbar, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Tooltip, Typography } from './ui';
import { ContentCopyRounded } from './icons';
import { DeleteOutlineRounded } from './icons';
import { DevicesOutlined } from './icons';
import { EditOutlined } from './icons';
import { FilterAltOffOutlined } from './icons';
import { RefreshRounded } from './icons';
import { SearchRounded } from './icons';
import { CloseRounded } from './icons';
import { DesktopWindowsRounded } from './icons';
import { SmartphoneRounded } from './icons';
import { TabletMacRounded } from './icons';
import { TvRounded } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { DeleteDeviceResponse, Device, DevicesResponse } from './api';
import { ErrorNotice, PageHeading } from './components';
import { fieldError } from './formFields';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { AccessBadge, AccessDetails, AccessIconTile, AccessTableHeader, accessSearchSx, accessTableSx } from './accessVisuals';
import { adminLocale, useTranslation } from './i18n';

function dateTime(value: string, locale: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? 'Unknown' : date.toLocaleString(locale, {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}

function RawValue({ value, fallback = 'Not recorded', title }: { value?: string | null; fallback?: string; title?: string }) {
  const { t } = useTranslation();
  return <span translate="no" title={title}>{value || t(fallback)}</span>;
}

function DeviceIcon({ device }: { device: Device }) {
  const identity = `${device.ReportedName} ${device.AppName}`.toLowerCase();
  if (/ipad|tablet/.test(identity)) return <TabletMacRounded />;
  if (/iphone|phone|ios/.test(identity)) return <SmartphoneRounded />;
  if (/tv|television|roku|firestick/.test(identity)) return <TvRounded />;
  if (/desktop|windows|mac|linux|pc/.test(identity)) return <DesktopWindowsRounded />;
  return <DevicesOutlined />;
}

function inputIssue(value: string): string | undefined {
  if (/[\u0000-\u001f\u007f-\u009f]/u.test(value)) return 'Control characters are not allowed.';
  if (new TextEncoder().encode(value).length > 256) return 'Use at most 256 UTF-8 bytes. Some characters use more than one byte.';
  return undefined;
}

function requiresRefresh(error: unknown): boolean {
  return error != null && (!(error instanceof ApiError)
    || ['network_error', 'invalid_response', 'invalid_device_revision'].includes(error.code)
    || [404, 409].includes(error.status) || error.status >= 500);
}

function MutationRecovery({ error }: { error: unknown }) {
  if (!requiresRefresh(error)) return null;
  const message = error instanceof ApiError && error.status === 409
    ? 'This device changed since you opened it. Refresh devices to review its current name before making another change.'
    : error instanceof ApiError && error.status === 404
      ? 'This device is no longer available. Refresh devices to see the current records.'
      : 'The result could not be confirmed. The change may already have been applied. Refresh devices before trying again.';
  return <Alert severity="warning">{message}</Alert>;
}

function DeviceIdentity({ device }: { device: Device }) {
  return (
    <Stack direction="row" sx={{ minWidth: 0, alignItems: 'center', gap: 1.5 }}>
      <AccessIconTile><DeviceIcon device={device} /></AccessIconTile>
      <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
        <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 0.75 }}>
          <Typography variant="body2" sx={{ fontWeight: 600 }}><RawValue value={device.Name} /></Typography>
          {device.CustomName !== null && <AccessBadge label="Renamed" />}
        </Stack>
        <Typography component="div" variant="caption" color="text.secondary" sx={{ mt: 0.25, fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}><RawValue value={device.ReportedName} fallback="Client device" title={device.ReportedDeviceId} /></Typography>
      </Box>
    </Stack>
  );
}

function DeviceApplication({ device }: { device: Device }) {
  return (
    <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
      <Typography variant="body2"><RawValue value={device.AppName} fallback="Application not reported" /></Typography>
      <Typography component="div" variant="caption" color="text.secondary" className="mono" sx={{ mt: 0.25, fontSize: 11 }}><RawValue value={device.AppVersion} fallback="Version not reported" /></Typography>
    </Box>
  );
}

function DeviceHistory({ device }: { device: Device }) {
  const { locale } = useTranslation();
  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography variant="body2" sx={{ fontSize: 12.5 }}><Box component="time" dateTime={device.LastSeenAt} title={device.LastSeenAt} sx={{ whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>{dateTime(device.LastSeenAt, locale)}</Box></Typography>
      <AccessDetails label="Connection details">
        <Typography variant="caption" component="div">Registered <Box component="time" dateTime={device.CreatedAt} title={device.CreatedAt}>{dateTime(device.CreatedAt, locale)}</Box></Typography>
        <Typography variant="caption" component="div" sx={{ overflowWrap: 'anywhere' }}>Client device ID: <span className="mono"><RawValue value={device.ReportedDeviceId} /></span></Typography>
        <Typography variant="caption" component="div" sx={{ mb: 0.5 }}>IP address: <span className="mono"><RawValue value={device.IpAddress} /></span></Typography>
        <DeviceLogins device={device} />
      </AccessDetails>
    </Box>
  );
}

function DeviceLogins({ device }: { device: Device }) {
  return <AccessBadge label={`${device.ActiveLoginCount.toLocaleString(adminLocale())} policy-eligible ${device.ActiveLoginCount === 1 ? 'login' : 'logins'}`} sx={{ maxWidth: '100%', height: 'auto', '& .MuiChip-label': { whiteSpace: 'normal', py: 0.4 } }} />;
}

function DeviceActions({ device, onRename, onRemove }: { device: Device; onRename: (device: Device) => void; onRemove: (device: Device) => void }) {
  const { t } = useTranslation();
  return (
    <Stack direction="row" sx={{ justifyContent: 'flex-end', flexWrap: 'nowrap', gap: 0.5 }}>
      <Tooltip title="Rename"><IconButton size="small" onClick={() => onRename(device)} aria-label={`${t('Rename')} ${device.Name}`} sx={{ width: 32, height: 32 }}><EditOutlined sx={{ fontSize: 16 }} /></IconButton></Tooltip>
      <Tooltip title="Remove"><IconButton size="small" onClick={() => onRemove(device)} aria-label={`${t('Remove device')} ${device.Name}`} sx={{ width: 32, height: 32, color: 'var(--err)' }}><DeleteOutlineRounded sx={{ fontSize: 16 }} /></IconButton></Tooltip>
    </Stack>
  );
}

function DevicesList({ items, onRename, onRemove }: { items: Device[]; onRename: (device: Device) => void; onRemove: (device: Device) => void }) {
  return (
    <>
      <Box component="ul" className="access-record-cards" aria-label="Registered devices" sx={{ display: { xs: 'block', lg: 'none' }, listStyle: 'none', p: 0, m: 0 }}>
        {items.map((device) => (
          <Box component="li" className="access-compact-record" key={device.Id} sx={{ px: 2, py: 1.5, borderTop: 1, borderColor: 'var(--line)' }}>
            <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1 }}><DeviceIdentity device={device} /><DeviceActions device={device} onRename={onRename} onRemove={onRemove} /></Stack>
            <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'flex-start', gap: 2, mt: 1.25, pl: '48px' }}><DeviceApplication device={device} /><Typography variant="caption" color="text.secondary"><RawValue value={device.LastUserName} fallback="No user recorded" /></Typography></Stack>
            <Box sx={{ mt: 1, pl: '48px' }}><DeviceHistory device={device} /></Box>
          </Box>
        ))}
      </Box>
      <TableContainer className="access-record-table" sx={{ display: { xs: 'none', lg: 'block' } }}>
        <Table className="access-data-table" aria-label="Registered devices" sx={{ minWidth: 900, tableLayout: 'fixed', ...accessTableSx }}>
          <TableHead><TableRow><TableCell sx={{ width: '28%' }}>Device</TableCell><TableCell sx={{ width: '16%' }}>Client</TableCell><TableCell sx={{ width: '12%' }}>User</TableCell><TableCell sx={{ width: '22%' }}>Last activity</TableCell><TableCell sx={{ width: '14%' }}>Address</TableCell><TableCell align="right" sx={{ width: '8%' }}><span className="visually-hidden">Actions</span></TableCell></TableRow></TableHead>
          <TableBody>
            {items.map((device) => (
              <TableRow key={device.Id}>
                <TableCell component="th" scope="row"><DeviceIdentity device={device} /></TableCell>
                <TableCell><DeviceApplication device={device} /></TableCell>
                <TableCell><Typography variant="body2" sx={{ fontWeight: 600, overflowWrap: 'anywhere' }}><RawValue value={device.LastUserName} fallback="No user recorded" title={device.LastUserId ?? undefined} /></Typography></TableCell>
                <TableCell><DeviceHistory device={device} /></TableCell>
                <TableCell><Typography variant="caption" color="text.secondary" className="mono" sx={{ fontSize: 11.5, overflowWrap: 'anywhere' }}><RawValue value={device.IpAddress} /></Typography></TableCell>
                <TableCell align="right"><DeviceActions device={device} onRename={onRename} onRemove={onRemove} /></TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </>
  );
}

function CopyDeviceId({ device }: { device: Device }) {
  const input = useRef<HTMLInputElement | HTMLTextAreaElement>(null);
  const mounted = useRef(true);
  const [copying, setCopying] = useState(false);
  const [copyState, setCopyState] = useState<'copied' | 'manual' | ''>('');
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  async function copy() {
    if (copying) return;
    setCopying(true);
    setCopyState('');
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(device.ReportedDeviceId);
      if (mounted.current) setCopyState('copied');
    } catch {
      if (mounted.current) {
        input.current?.focus();
        input.current?.select();
        setCopyState('manual');
      }
    } finally {
      if (mounted.current) setCopying(false);
    }
  }

  return (
    <Stack spacing={1}>
      <TextField id="remove-device-client-id" label="Client device ID" value={device.ReportedDeviceId} inputRef={input} fullWidth multiline maxRows={3} slotProps={{ input: { readOnly: true }, htmlInput: { spellCheck: false } }} sx={{ '& textarea': { fontFamily: 'monospace', overflowWrap: 'anywhere' } }} />
      <Box><Button size="small" startIcon={<ContentCopyRounded />} onClick={() => void copy()} disabled={copying}>Copy device ID</Button></Box>
      {copyState === 'copied' && <Typography variant="body2" role="status">Device ID copied.</Typography>}
      {copyState === 'manual' && <Alert severity="info" role="status">Clipboard access is unavailable. The device ID is selected. Press Ctrl+C or use your device's copy command.</Alert>}
    </Stack>
  );
}

interface DeviceDialogProps {
  device: Device;
  onClose: () => void;
  onRefresh: () => void;
  onNavigationGuardChange: UserNavigationGuardChange;
}

function RenameDeviceDialog({ device, onClose, onSaved, onRefresh, onNavigationGuardChange }: DeviceDialogProps & { onSaved: (device: Device) => void }) {
  const [customName, setCustomName] = useState(device.CustomName ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const inFlight = useRef<AbortController | undefined>(undefined);
  const refreshRequired = requiresRefresh(error);
  const nameIssue = inputIssue(customName.trim());
  const dirty = customName !== (device.CustomName ?? '');
  useUserDraftNavigation(dirty && !refreshRequired, busy, onNavigationGuardChange, 'Discard unsaved device name changes and leave this page?');
  useEffect(() => () => inFlight.current?.abort(), []);

  function close() {
    if (inFlight.current) return;
    if (refreshRequired) onRefresh();
    else onClose();
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || refreshRequired || nameIssue) return;
    const controller = new AbortController();
    inFlight.current = controller;
    setBusy(true);
    setError(null);
    try {
      const result = await adminApi.updateDeviceOptions(device.Id, { Revision: device.Revision, CustomName: customName.trim() }, { signal: controller.signal });
      if (!controller.signal.aborted) onSaved(result);
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) setError(cause);
    } finally {
      if (inFlight.current === controller) inFlight.current = undefined;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  return (
    <Dialog open onClose={close} fullWidth maxWidth="sm" aria-labelledby="rename-device-title" aria-describedby="rename-device-description">
      <Box component="form" onSubmit={save} aria-busy={busy}>
        <DialogTitle id="rename-device-title" sx={{ pt: 3 }}>Rename device</DialogTitle>
        <DialogContent>
          <Stack spacing={2.5} sx={{ pt: 0.5 }}>
            <Typography id="rename-device-description" variant="body2" color="text.secondary">Choose a name to recognize this device. Clear the custom name to use the name reported by its client.</Typography>
            <Paper variant="outlined" sx={{ p: 2.5, bgcolor: 'background.default' }}><DeviceIdentity device={device} /></Paper>
            <TextField autoFocus fullWidth id="device-custom-name" name="CustomName" label="Custom name" value={customName} disabled={busy || refreshRequired} onChange={(event) => { setCustomName(event.target.value); setError(null); }} error={Boolean(nameIssue || fieldError(error, 'CustomName'))} helperText={nameIssue ?? fieldError(error, 'CustomName') ?? 'Optional. Leading and trailing spaces are removed.'} slotProps={{ htmlInput: { autoComplete: 'off' } }} />
            <Box><Button color="secondary" onClick={() => { setCustomName(''); setError(null); }} disabled={busy || refreshRequired || customName === ''}>Clear custom name</Button></Box>
            {error != null && <ErrorNotice error={error} />}
            <MutationRecovery error={error} />
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 3, flexWrap: 'wrap', gap: 1 }}>
          <Button onClick={close} color="secondary" disabled={busy}>{refreshRequired ? 'Close' : 'Cancel'}</Button>
          {refreshRequired
            ? <Button variant="contained" startIcon={<RefreshRounded />} onClick={onRefresh}>Refresh devices</Button>
            : <Button type="submit" variant="contained" disabled={busy || Boolean(nameIssue)} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <EditOutlined />}>{busy ? 'Saving name...' : 'Save name'}</Button>}
        </DialogActions>
      </Box>
    </Dialog>
  );
}

function RemoveDeviceDialog({ device, onClose, onRemoved, onRefresh, onNavigationGuardChange }: DeviceDialogProps & { onRemoved: (result: DeleteDeviceResponse) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const inFlight = useRef<AbortController | undefined>(undefined);
  const refreshRequired = requiresRefresh(error);
  useUserDraftNavigation(false, busy, onNavigationGuardChange);
  useEffect(() => () => inFlight.current?.abort(), []);

  function close() {
    if (inFlight.current) return;
    if (refreshRequired) onRefresh();
    else onClose();
  }

  async function remove() {
    if (inFlight.current || refreshRequired) return;
    const controller = new AbortController();
    inFlight.current = controller;
    setBusy(true);
    setError(null);
    try {
      const result = await adminApi.deleteDevice(device.Id, { Revision: device.Revision }, { signal: controller.signal });
      if (!controller.signal.aborted) onRemoved(result);
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) setError(cause);
    } finally {
      if (inFlight.current === controller) inFlight.current = undefined;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  return (
    <Dialog open onClose={close} fullWidth maxWidth="sm" data-layout="modal" aria-labelledby="remove-device-title" aria-describedby="remove-device-description">
      <Box aria-busy={busy}>
        <DialogTitle id="remove-device-title" sx={{ pt: 3 }}>Remove device?</DialogTitle>
        <DialogContent>
          <Stack spacing={2.5}>
            <Typography id="remove-device-description" variant="body2" color="text.secondary">Remove this device and sign out its associated client logins. The device can register again when a user signs in.</Typography>
            <Paper variant="outlined" sx={{ p: 2.5, bgcolor: 'background.default' }}><Stack spacing={2}><DeviceIdentity device={device} /><DeviceApplication device={device} /><Box><DeviceLogins device={device} /></Box></Stack></Paper>
            <CopyDeviceId device={device} />
            {error != null && <ErrorNotice error={error} />}
            <MutationRecovery error={error} />
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 3, flexWrap: 'wrap', gap: 1 }}>
          <Button onClick={close} color="secondary" disabled={busy} autoFocus>{refreshRequired ? 'Close' : 'Cancel'}</Button>
          {refreshRequired
            ? <Button variant="contained" startIcon={<RefreshRounded />} onClick={onRefresh}>Refresh devices</Button>
            : <Button variant="contained" color="error" onClick={() => void remove()} disabled={busy} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <DeleteOutlineRounded />}>{busy ? 'Removing device...' : 'Remove device'}</Button>}
        </DialogActions>
      </Box>
    </Dialog>
  );
}

export function DevicesPage({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const [searchTerm, setSearchTerm] = useState('');
  const [query, setQuery] = useState({ searchTerm: '', page: 0, pageSize: 25 });
  const [loaded, setLoaded] = useState<{ requestKey: string; result: DevicesResponse }>();
  const [failure, setFailure] = useState<{ requestKey: string; cause: unknown }>();
  const [revision, setRevision] = useState(0);
  const [renaming, setRenaming] = useState<Device>();
  const [removing, setRemoving] = useState<Device>();
  const [notice, setNotice] = useState<ReactNode>('');
  const searchInput = useRef<HTMLInputElement>(null);
  const requestKey = JSON.stringify([query.searchTerm, query.page, query.pageSize, revision]);
  const data = loaded?.requestKey === requestKey ? loaded.result : undefined;
  const failed = failure?.requestKey === requestKey;
  const loading = !data && !failed;
  const queryError = failed ? failure?.cause : null;
  const searchIssue = inputIssue(searchTerm);
  const canClear = Boolean(searchTerm || query.searchTerm);

  useEffect(() => {
    if (searchIssue) return;
    const timer = window.setTimeout(() => {
      setQuery((current) => current.searchTerm === searchTerm ? current : { ...current, searchTerm, page: 0 });
    }, 300);
    return () => window.clearTimeout(timer);
  }, [searchTerm, searchIssue]);

  useEffect(() => {
    const controller = new AbortController();
    setLoaded(undefined);
    setFailure(undefined);
    void adminApi.getDevices({
      SearchTerm: query.searchTerm || undefined,
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

  function closeAndRefresh() {
    setRenaming(undefined);
    setRemoving(undefined);
    refresh();
    requestAnimationFrame(() => searchInput.current?.focus());
  }

  function applySearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (searchIssue) return;
    setQuery((current) => ({ ...current, searchTerm, page: 0 }));
    refresh();
  }

  function clearSearch() {
    setSearchTerm('');
    setQuery((current) => ({ ...current, searchTerm: '', page: 0 }));
    refresh();
  }

  return (
    <Box aria-busy={loading}>
      <PageHeading title="Devices" description="Devices registered by your clients. Rename a device or remove its access." />
      {failed && <Box sx={{ mb: 3 }}><ErrorNotice error={queryError} retry={refresh} /></Box>}
      <Paper variant="outlined" sx={{ overflow: 'hidden', borderRadius: '14px', bgcolor: 'var(--s2)', borderColor: 'var(--line)' }}>
        <AccessTableHeader title="Registered devices" count={data?.TotalRecordCount}>
          <Stack direction="row" sx={{ alignItems: 'flex-start', gap: 0.75 }}>
            <Box component="form" onSubmit={applySearch} sx={{ flex: 1, minWidth: 0 }}>
              <TextField id="devices-search" name="SearchTerm" inputRef={searchInput} placeholder="Search device, ID, app, or user" value={searchTerm} onChange={(event) => setSearchTerm(event.target.value)} error={Boolean(searchIssue || fieldError(queryError, 'SearchTerm'))} helperText={searchIssue ?? fieldError(queryError, 'SearchTerm')} sx={accessSearchSx} slotProps={{ htmlInput: { 'aria-label': 'Search devices', autoComplete: 'off', spellCheck: false }, input: { startAdornment: <InputAdornment position="start"><IconButton type="submit" size="small" aria-label="Search devices" disabled={Boolean(searchIssue)} edge="start"><SearchRounded sx={{ fontSize: 18 }} /></IconButton></InputAdornment>, endAdornment: canClear ? <InputAdornment position="end"><IconButton size="small" aria-label="Clear search" onClick={clearSearch} edge="end"><CloseRounded sx={{ fontSize: 16 }} /></IconButton></InputAdornment> : undefined } }} />
            </Box>
            <Tooltip title="Refresh devices"><span><IconButton onClick={refresh} disabled={loading} aria-label="Refresh devices"><RefreshRounded sx={{ fontSize: 19 }} /></IconButton></span></Tooltip>
          </Stack>
        </AccessTableHeader>
        {loading && <Stack spacing={1} sx={{ p: 2, borderTop: 1, borderColor: 'var(--line)' }} role="status" aria-live="polite" aria-label="Loading devices"><Skeleton height={52} /><Skeleton height={52} /><Skeleton height={52} /></Stack>}
        {data && data.Items.length === 0 && (
          <Stack spacing={1.5} sx={{ alignItems: 'center', p: { xs: 3, sm: 5 }, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}>
            <DevicesOutlined sx={{ fontSize: 40, color: 'primary.main' }} />
            <Typography variant="h3" component="h3">{query.searchTerm ? 'No matching devices' : 'No registered devices'}</Typography>
            <Typography color="text.secondary" sx={{ maxWidth: 440 }}>{query.searchTerm ? 'Try another device, application, or user name, or clear your search.' : 'Devices appear here when users sign in to a compatible client. Dashboard logins and API keys are managed on their own pages.'}</Typography>
            {query.searchTerm && <Button startIcon={<FilterAltOffOutlined />} onClick={clearSearch}>Clear search</Button>}
          </Stack>
        )}
        {data && data.Items.length > 0 && <DevicesList items={data.Items} onRename={setRenaming} onRemove={setRemoving} />}
        {failed && <Typography variant="body2" color="text.secondary" sx={{ px: { xs: 2.5, sm: 3 }, py: 3 }}>The device list could not be loaded. Refresh devices to see current records.</Typography>}
        {data && <TablePagination
          component="div"
          count={data.TotalRecordCount}
          page={query.page}
          rowsPerPage={query.pageSize}
          rowsPerPageOptions={[25, 50, 100, 200]}
          labelRowsPerPage="Devices per page:"
          onPageChange={(_event, page) => setQuery((current) => ({ ...current, page }))}
          onRowsPerPageChange={(event) => {
            const pageSize = Number(event.target.value);
            if ([25, 50, 100, 200].includes(pageSize)) setQuery((current) => ({ ...current, pageSize, page: 0 }));
          }}
          sx={{ borderTop: 1, borderColor: 'divider', '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', justifyContent: 'flex-end', px: { xs: 2, sm: 3 }, py: 1, gap: 0.5 }, '& .MuiTablePagination-spacer': { display: { xs: 'none', sm: 'block' } }, '& .MuiTablePagination-actions': { ml: { xs: 1, sm: 2 } } }}
        />}
      </Paper>
      <Box sx={{ mt: 2 }}><AccessDetails label="About device activity"><Typography variant="caption">Policy-eligible logins satisfy the stored account, credential, device, schedule, and lockout rules at the time of this view. Actual requests also depend on their connection context. Times use your local time zone.</Typography></AccessDetails></Box>
      {renaming && <RenameDeviceDialog key={renaming.Id} device={renaming} onClose={() => setRenaming(undefined)} onSaved={(device) => { setNotice(<>Device name saved as <RawValue value={device.Name} />.</>); closeAndRefresh(); }} onRefresh={closeAndRefresh} onNavigationGuardChange={onNavigationGuardChange} />}
      {removing && <RemoveDeviceDialog key={removing.Id} device={removing} onClose={() => setRemoving(undefined)} onRemoved={(result) => { setNotice(`Device removed. ${result.RevokedLoginCount.toLocaleString(adminLocale())} client ${result.RevokedLoginCount === 1 ? 'login' : 'logins'} signed out.`); closeAndRefresh(); }} onRefresh={closeAndRefresh} onNavigationGuardChange={onNavigationGuardChange} />}
      <Snackbar open={Boolean(notice) && !renaming && !removing} autoHideDuration={6000} onClose={() => setNotice('')} anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}><Alert severity="success" variant="filled" onClose={() => setNotice('')} sx={{ width: '100%', overflowWrap: 'anywhere' }}>{notice}</Alert></Snackbar>
    </Box>
  );
}
