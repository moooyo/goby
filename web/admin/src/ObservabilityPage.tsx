import { Fragment, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Chip, Collapse, IconButton, MenuItem, Paper, Skeleton, Stack, Tab, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, Tabs, TextField, Tooltip, Typography } from './ui';
import { CloseRounded } from './icons';
import { ContentCopyRounded } from './icons';
import { DownloadRounded } from './icons';
import { ExpandLessRounded } from './icons';
import { ExpandMoreRounded } from './icons';
import { FilterAltOffOutlined } from './icons';
import { FilterListRounded } from './icons';
import { HistoryRounded } from './icons';
import { RefreshRounded } from './icons';
import { SearchRounded } from './icons';
import { SubjectRounded } from './icons';
import { activityActions, activitySeverities, adminApi, ApiError, isAbortError } from './api';
import type { ActivityAction, ActivityEntry, ActivityQuery, ActivityResourceKind, ActivityResponse, ActivitySeverity, ServerLogFile, ServerLogLinesResponse, ServerLogsResponse } from './api';
import { ErrorNotice, PageHeading } from './components';
import { colors } from './theme';
import { adminLocale, useTranslation } from './i18n';
import './system-pages.css';

const actionLabels: Record<ActivityAction, string> = {
  'user.created': 'User created', 'user.updated': 'User updated', 'user.password_reset': 'User password reset',
  'session.login': 'Signed in', 'session.revoked': 'Session revoked',
  'playback.started': 'Playback started', 'playback.stopped': 'Playback stopped',
  'application_key.created': 'API key created', 'application_key.revealed': 'API key revealed', 'application_key.revoked': 'API key revoked',
  'device.updated': 'Device updated', 'device.removed': 'Device removed',
  'library.created': 'Library created', 'library.removed': 'Library removed', 'library.root_binding.updated': 'Library root binding updated',
  'scan.requested': 'Scan requested', 'scan.cancel_requested': 'Scan cancellation requested', 'scan.finished': 'Scan finished',
  'metadata.updated': 'Metadata updated', 'settings.updated': 'Settings updated',
  'task.admitted': 'Task admitted', 'task.cancel_requested': 'Task cancellation requested', 'task.finished': 'Task finished', 'task.schedule_updated': 'Task schedule updated',
  'backup.requested': 'Backup requested', 'backup.cancel_requested': 'Backup cancellation requested', 'backup.finished': 'Backup finished',
  'backup.published': 'Backup available',
  'user.deleted': 'User deleted',
  'library.updated': 'Library updated',
  'item.deleted': 'Media item deleted',
  'subtitle.deleted': 'Subtitle deleted',
  'backup.imported': 'Backup imported', 'backup.delete_requested': 'Backup deletion requested', 'backup.deleted': 'Backup deleted', 'backup.downloaded': 'Backup download prepared',
  'restore.requested': 'Restore requested', 'restore.planned': 'Restore planned', 'restore.apply_requested': 'Restore application requested',
  'restore.applied': 'Restore applied', 'restore.rollback_requested': 'Restore rollback requested', 'restore.cancel_requested': 'Restore cancellation requested', 'restore.failed': 'Restore failed',
};
const resourceLabels: Record<ActivityResourceKind, string> = {
  user: 'User', session: 'Session', application_key: 'API key', device: 'Device', library: 'Library', library_root: 'Library root', scan: 'Scan',
  item: 'Item', settings: 'Settings', task: 'Task', task_run: 'Task run', backup: 'Backup', restore: 'Restore',
};
const sourceLabels = { native: 'Console', emby: 'Emby API', system: 'System' };
const pageSizes = [25, 50, 100, 200];
const panelSx = { overflow: 'hidden', borderRadius: '14px', bgcolor: 'var(--s2)', borderColor: 'var(--line)' };
const sectionHeaderSx = { alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1, px: 2, py: 1.25 };
const tableSx = {
  tableLayout: 'fixed',
  '& .MuiTableCell-root': { px: 2, py: 0.75, height: 52, boxSizing: 'border-box', fontSize: 13, verticalAlign: 'middle' },
  '& .MuiTableCell-head': { height: 38, py: 1, fontSize: 12 },
  '& .MuiTableRow-root:last-child .MuiTableCell-root': { borderBottom: 0 },
};
const paginationSx = {
  borderTop: 1, borderColor: 'divider',
  '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', justifyContent: 'flex-end', px: { xs: 2, sm: 3 }, py: 1, gap: 0.5 },
  '& .MuiTablePagination-spacer': { display: { xs: 'none', sm: 'block' } },
  '& .MuiTablePagination-actions': { ml: { xs: 1, sm: 2 } },
};

function localTime(value: string): string {
  return new Date(value).toLocaleString(document.documentElement.lang || undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

function byteSize(value: string): string {
  const bytes = BigInt(value);
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB'];
  let divisor = 1n;
  let unit = 0;
  while (bytes >= divisor * 1024n && unit < units.length - 1) { divisor *= 1024n; unit += 1; }
  const whole = bytes / divisor;
  const fraction = bytes % divisor * 10n / divisor;
  return `${whole}${fraction > 0n ? `.${fraction}` : ''} ${units[unit]}`;
}

function LoadingRows({ label }: { label: string }) {
  return <Stack role="status" aria-live="polite" aria-label={label} spacing={1} sx={{ px: 3, py: 1.5, borderTop: 1, borderColor: 'divider' }}><Skeleton height={48} /><Skeleton height={48} /><Skeleton height={48} /></Stack>;
}

function Severity({ severity }: { severity: ActivitySeverity }) {
  const color = severity === 'Warn' ? 'warning' : severity === 'Error' || severity === 'Fatal' ? 'error' : severity === 'Info' ? 'info' : 'default';
  return <Chip size="small" label={severity === 'Warn' ? 'Warning' : severity} color={color} sx={{ height: 22, borderRadius: '6px', fontSize: 11, '& .MuiChip-label': { px: 0.9 } }} />;
}

function actorName(actor: ActivityEntry['Actor']): string {
  return actor.Kind === 'system' ? 'System' : actor.Kind === 'application_key' ? 'API key' : actor.Name || 'User';
}

function Actor({ actor, onFilter }: { actor: ActivityEntry['Actor']; onFilter: (id: string) => void }) {
  const label = actorName(actor);
  return actor.Id ? <Tooltip title={`Filter activity by ${label}`}><Button size="small" color="inherit" onClick={() => onFilter(actor.Id!)} aria-label={`Filter by actor ${actor.Id}`} aria-description={label} sx={{ minWidth: 0, maxWidth: '100%', justifyContent: 'flex-start', px: 0.75, ml: -0.75, fontWeight: 400, color: 'text.secondary' }}><Box component="span" translate={actor.Kind === 'user' && actor.Name ? 'no' : undefined} sx={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{label}</Box></Button></Tooltip> : <Typography variant="body2" color="text.secondary" translate={actor.Kind === 'user' && actor.Name ? 'no' : undefined}>{label}</Typography>;
}

function ActivityDetails({ entry, id, expanded }: { entry: ActivityEntry; id: string; expanded: boolean }) {
  const { t } = useTranslation();
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle');
  async function copyRecord() {
    try { await navigator.clipboard.writeText(JSON.stringify(entry, null, 2)); setCopyState('copied'); }
    catch { setCopyState('failed'); }
  }
  return <Collapse in={expanded} unmountOnExit id={id}>
    <Box sx={{ p: 2.5, bgcolor: colors.surface, borderTop: 1, borderColor: 'divider' }}>
      {entry.Overview && <Typography variant="body2" sx={{ mb: 2, overflowWrap: 'anywhere' }}>{entry.Overview}</Typography>}
      <Stack direction="row" sx={{ flexWrap: 'wrap', columnGap: 4, rowGap: 1.5 }}>
        <Box sx={{ minWidth: 0 }}><Typography variant="caption" color="text.secondary">Activity</Typography><Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>{entry.Name}</Typography></Box>
        <Box><Typography variant="caption" color="text.secondary">Activity ID</Typography><Typography variant="body2" className="mono">{entry.Id}</Typography></Box>
        <Box><Typography variant="caption" color="text.secondary">Action</Typography><Typography variant="body2" className="mono">{entry.Action}</Typography></Box>
        <Box sx={{ minWidth: 0 }}><Typography variant="caption" color="text.secondary">Actor</Typography><Typography variant="body2" translate={entry.Actor.Kind === 'user' && entry.Actor.Name ? 'no' : undefined} sx={{ overflowWrap: 'anywhere' }}>{actorName(entry.Actor)}</Typography></Box>
        {entry.Actor.Id && <Box sx={{ minWidth: 0 }}><Typography variant="caption" color="text.secondary">Actor ID</Typography><Typography variant="body2" className="mono" sx={{ overflowWrap: 'anywhere' }}>{entry.Actor.Id}</Typography></Box>}
        <Box sx={{ minWidth: 0 }}><Typography variant="caption" color="text.secondary">Resource</Typography><Typography variant="body2">{resourceLabels[entry.Resource.Kind]}</Typography>{entry.Resource.Name && <Typography variant="body2" translate="no" sx={{ overflowWrap: 'anywhere' }}>{entry.Resource.Name}</Typography>}<Typography variant="body2" className="mono" sx={{ overflowWrap: 'anywhere' }}>{entry.Resource.Id}</Typography></Box>
        <Box><Typography variant="caption" color="text.secondary">Affected count</Typography><Typography variant="body2" className="mono">{entry.Count}</Typography></Box>
        {entry.PreviousRevision !== undefined && <Box><Typography variant="caption" color="text.secondary">Previous revision</Typography><Typography variant="body2" className="mono">{entry.PreviousRevision}</Typography></Box>}
        {entry.Revision !== null && <Box><Typography variant="caption" color="text.secondary">Revision</Typography><Typography variant="body2" className="mono">{entry.Revision}</Typography></Box>}
        {entry.ObservationFingerprint !== undefined && <Box sx={{ minWidth: 0, maxWidth: '100%' }}><Typography variant="caption" color="text.secondary">Observation fingerprint</Typography><Typography variant="body2" className="mono" sx={{ overflowWrap: 'anywhere' }}>{entry.ObservationFingerprint}</Typography></Box>}
        {entry.State !== null && <Box><Typography variant="caption" color="text.secondary">Outcome</Typography><Typography variant="body2" sx={{ textTransform: 'capitalize' }}>{entry.State}</Typography></Box>}
      </Stack>
      {entry.ChangedFields.length > 0 && <Box sx={{ mt: 2 }}><Typography variant="caption" color="text.secondary" component="div" sx={{ mb: 0.8 }}>Changed fields</Typography><Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.8 }}>{entry.ChangedFields.map((field) => <Chip key={field} label={field} size="small" variant="outlined" />)}</Stack></Box>}
      <pre className="system-json" aria-label={t('Activity JSON')}>{JSON.stringify(entry, null, 2)}</pre>
      <div className="system-json-action"><Button size="small" startIcon={<ContentCopyRounded />} onClick={() => void copyRecord()}>{copyState === 'copied' ? 'Copied' : 'Copy JSON'}</Button></div>
      {copyState === 'failed' && <Alert severity="error">Copy failed. Select and copy the JSON text above.</Alert>}
    </Box>
  </Collapse>;
}

function ActivityList({ items, onActor }: { items: ActivityEntry[]; onActor: (id: string) => void }) {
  const { t, locale } = useTranslation();
  const [expanded, setExpanded] = useState<string>();
  function toggle(id: string) { setExpanded((current) => current === id ? undefined : id); }
  return <div className="system-activity-scroll"><table className="system-activity-table" aria-label={t('Activity records')}>
    <thead><tr><th>{t('Time')}</th><th>{t('Severity')}</th><th>{t('Source')}</th><th>{t('Message')}</th></tr></thead>
    <tbody>{items.map((entry) => <Fragment key={entry.Id}>
      <tr data-expanded={expanded === entry.Id}>
        <td><time className="system-mono" dateTime={entry.Date} title={localTime(entry.Date)}>{new Date(entry.Date).toLocaleString(locale, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })}</time></td>
        <td><Severity severity={entry.Severity} /></td>
        <td className="system-source-cell">{t(sourceLabels[entry.Source])}</td>
        <td><button type="button" className="system-activity-open" aria-label={`${t('Details for activity')} ${entry.Id}`} aria-expanded={expanded === entry.Id} aria-controls={`activity-table-${entry.Id}`} onClick={() => toggle(entry.Id)}><span>{entry.Name}</span>{expanded === entry.Id ? <ExpandLessRounded /> : <ExpandMoreRounded />}</button><div className="system-activity-sub" style={{ flexWrap: 'wrap' }}><Actor actor={entry.Actor} onFilter={onActor} />{entry.Resource.Name && <span translate="no" style={{ minWidth: 0, overflowWrap: 'anywhere' }}>{entry.Resource.Name}</span>}<code>{entry.Action}</code></div></td>
      </tr>
      {expanded === entry.Id && <tr className="system-activity-details"><td colSpan={4}><ActivityDetails entry={entry} expanded id={`activity-table-${entry.Id}`} /></td></tr>}
    </Fragment>)}</tbody>
  </table></div>;
}

type ActivityPeriod = 'all' | 'day' | 'week' | 'month' | 'custom';
const emptyFilters = { severity: '' as ActivitySeverity | '', action: '' as ActivityAction | '', period: 'all' as ActivityPeriod, since: '' };

function ActivityPanel() {
  const { t, locale, countLabel } = useTranslation();
  const [showFilters, setShowFilters] = useState(false);
  const [search, setSearch] = useState('');
  const [source, setSource] = useState<ActivityEntry['Source'] | ''>('');
  const [live, setLive] = useState(false);
  const [filters, setFilters] = useState(emptyFilters);
  const [query, setQuery] = useState<{ filters: ActivityQuery; page: number; pageSize: number }>({ filters: {}, page: 0, pageSize: 50 });
  const [revision, setRevision] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; queryKey: string; result: ActivityResponse }>();
  const [failure, setFailure] = useState<{ key: string; cause: unknown }>();
  const queryKey = JSON.stringify(query);
  const requestKey = JSON.stringify([query, revision]);
  const data = loaded?.key === requestKey || (live && loaded?.queryKey === queryKey) ? loaded?.result : undefined;
  const failed = failure?.key === requestKey;
  const loading = loaded?.key !== requestKey && !failed;
  const dateIssue = filters.period === 'custom' && (!filters.since || !Number.isFinite(Date.parse(filters.since))) ? 'Choose a valid date and time.' : undefined;
  const filtered = Object.values(query.filters).some(Boolean) || Boolean(source || search.trim());

  useEffect(() => {
    const controller = new AbortController();
    setFailure(undefined);
    void adminApi.getActivity({ ...query.filters, StartIndex: query.page * query.pageSize, Limit: query.pageSize }, { signal: controller.signal })
      .then((result) => {
        if (controller.signal.aborted) return;
        if (query.page > 0 && query.page * query.pageSize >= result.TotalRecordCount) {
          setQuery((current) => ({ ...current, page: Math.max(0, Math.ceil(result.TotalRecordCount / query.pageSize) - 1) }));
          return;
        }
        setLoaded({ key: requestKey, queryKey, result });
      })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setFailure({ key: requestKey, cause }); });
    return () => controller.abort();
  }, [query, queryKey, requestKey]);

  useEffect(() => {
    if (query.page !== 0 && live) setLive(false);
  }, [query.page, live]);
  useEffect(() => {
    if (!live || loading || failed || query.page !== 0) return;
    const timer = window.setTimeout(() => { if (document.visibilityState !== 'hidden') setRevision((value) => value + 1); }, 5000);
    const resume = () => { if (document.visibilityState !== 'hidden') setRevision((value) => value + 1); };
    document.addEventListener('visibilitychange', resume);
    return () => { window.clearTimeout(timer); document.removeEventListener('visibilitychange', resume); };
  }, [live, loading, failed, query.page, revision]);

  const visibleItems = (data?.Items ?? []).filter((entry) => (!source || entry.Source === source) && (!search.trim() || `${entry.Name} ${entry.Overview} ${entry.Action} ${actorName(entry.Actor)} ${entry.Resource.Id} ${entry.Resource.Name ?? ''}`.toLocaleLowerCase(locale).includes(search.trim().toLocaleLowerCase(locale))));
  function exportPage() {
    const url = URL.createObjectURL(new Blob([visibleItems.map((entry) => JSON.stringify(entry)).join('\n')], { type: 'application/x-ndjson' }));
    const link = document.createElement('a'); link.href = url; link.download = 'goby-activity-page.jsonl'; link.click(); window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  function refresh() { setRevision((current) => current + 1); }
  function applyFilters(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (dateIssue) return;
    const days = filters.period === 'day' ? 1 : filters.period === 'week' ? 7 : 30;
    const minDate = filters.period === 'all' ? undefined : filters.period === 'custom' ? new Date(filters.since).toISOString() : new Date(Date.now() - days * 86400000).toISOString();
    setQuery((current) => ({ ...current, page: 0, filters: { Severity: filters.severity || undefined, Action: filters.action || undefined, MinDate: minDate, ActorId: current.filters.ActorId } }));
    refresh();
  }
  function clearFilters() { setFilters(emptyFilters); setSearch(''); setSource(''); setQuery((current) => ({ ...current, filters: {}, page: 0 })); refresh(); }

  return <Box aria-busy={loading}>
    {failed && <Box sx={{ mb: 3 }}><ErrorNotice error={failure?.cause} retry={refresh} /></Box>}
    <Paper variant="outlined" sx={panelSx}>
      <Stack direction="row" sx={sectionHeaderSx}><Stack direction="row" sx={{ alignItems: 'center', gap: 1.2 }}><Typography variant="h4" component="h2">Server activity</Typography>{data && <Chip size="small" label={data.TotalRecordCount.toLocaleString(adminLocale())} sx={{ bgcolor: colors.surface }} />}</Stack><Stack direction="row" sx={{ gap: 0.5 }}><Button size="small" startIcon={<DownloadRounded />} disabled={!data || visibleItems.length === 0} onClick={exportPage}>Export current page</Button><Tooltip title={showFilters ? 'Hide filters' : 'Filter activity'}><IconButton aria-label={showFilters ? 'Hide activity filters' : 'Filter activity'} aria-expanded={showFilters} aria-controls="activity-filters" onClick={() => setShowFilters((current) => !current)} sx={{ color: filtered || showFilters ? 'primary.main' : 'text.secondary', bgcolor: filtered ? 'info.light' : undefined }}><FilterListRounded fontSize="small" /></IconButton></Tooltip><Tooltip title="Refresh activity"><span><IconButton aria-label="Refresh activity" onClick={refresh} disabled={loading}><RefreshRounded fontSize="small" /></IconButton></span></Tooltip></Stack></Stack>
      <div className="system-activity-tools">
        <TextField className="system-log-search" size="small" label="Search this page" placeholder="Search messages, actors or resources" value={search} onChange={(event) => setSearch(event.target.value)} />
        <TextField className="system-log-source" select size="small" label="Source on this page" value={source} onChange={(event) => setSource(event.target.value as ActivityEntry['Source'] | '')}><MenuItem value="">All sources</MenuItem>{Object.entries(sourceLabels).map(([key, label]) => <MenuItem key={key} value={key}>{label}</MenuItem>)}</TextField>
        <Button className="system-live-button" variant="outlined" aria-pressed={live} disabled={query.page !== 0} onClick={() => setLive((value) => !value)}><span className={`system-status-dot${live && !failed ? ' is-active' : ''}`} aria-hidden="true" />{live ? failed ? 'Live updates paused' : 'Live updates on' : 'Live updates off'}</Button>
      </div>
      <Collapse in={showFilters} id="activity-filters">
      <Box component="form" onSubmit={applyFilters} sx={{ px: { xs: 2.5, sm: 3 }, pb: 2.5, '& .MuiInputBase-root': { minHeight: 40, borderRadius: '8px' } }}>
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr', xl: '1fr 1.4fr 1fr' }, gap: 2 }}>
          <TextField select size="small" id="activity-severity" label="Severity" value={filters.severity} onChange={(event) => setFilters((current) => ({ ...current, severity: event.target.value as ActivitySeverity | '' }))}><MenuItem value="">All severities</MenuItem>{activitySeverities.map((severity) => <MenuItem key={severity} value={severity}>{severity}</MenuItem>)}</TextField>
          <TextField select size="small" id="activity-action" label="Action" value={filters.action} onChange={(event) => setFilters((current) => ({ ...current, action: event.target.value as ActivityAction | '' }))}><MenuItem value="">All actions</MenuItem>{activityActions.map((action) => <MenuItem key={action} value={action}>{actionLabels[action]}</MenuItem>)}</TextField>
          <TextField select size="small" id="activity-period" label="Date range" value={filters.period} onChange={(event) => setFilters((current) => ({ ...current, period: event.target.value as ActivityPeriod }))}><MenuItem value="all">All retained activity</MenuItem><MenuItem value="day">Last 24 hours</MenuItem><MenuItem value="week">Last 7 days</MenuItem><MenuItem value="month">Last 30 days</MenuItem><MenuItem value="custom">Since a date and time</MenuItem></TextField>
          {filters.period === 'custom' && <TextField size="small" id="activity-since" label="Activity since" type="datetime-local" value={filters.since} onChange={(event) => setFilters((current) => ({ ...current, since: event.target.value }))} error={Boolean(dateIssue)} helperText={dateIssue ?? 'Uses your local time zone.'} slotProps={{ inputLabel: { shrink: true } }} />}
        </Box>
        {query.filters.ActorId && <Chip label={`Actor: ${query.filters.ActorId}`} onDelete={() => setQuery((current) => ({ ...current, page: 0, filters: { ...current.filters, ActorId: undefined } }))} sx={{ mt: 2, maxWidth: '100%', '& .MuiChip-label': { overflowWrap: 'anywhere' } }} />}
        <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1, mt: 2 }}><Button type="submit" variant="outlined" startIcon={<SearchRounded />} disabled={Boolean(dateIssue)}>Apply filters</Button><Button color="secondary" startIcon={<FilterAltOffOutlined />} onClick={clearFilters} disabled={!filtered && !filters.severity && !filters.action && filters.period === 'all'}>Clear filters</Button></Stack>
        {query.filters.MinDate && <Typography variant="caption" color="text.secondary" component="p" sx={{ mt: 1.5, mb: 0 }}>Showing activity since {localTime(query.filters.MinDate)}.</Typography>}
      </Box>
      </Collapse>
      {filtered && !showFilters && <Stack direction="row" sx={{ px: 3, pb: 1.5, alignItems: 'center', flexWrap: 'wrap', gap: 1 }}><Chip size="small" color="info" label="Filters applied" onClick={() => setShowFilters(true)} /><Button size="small" onClick={clearFilters}>Clear filters</Button></Stack>}
      {loading && !data && <LoadingRows label="Loading activity" />}
      {data && data.Items.length === 0 && <Stack spacing={1.5} sx={{ alignItems: 'center', p: { xs: 3, sm: 5 }, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}><HistoryRounded sx={{ fontSize: 40, color: 'primary.main' }} /><Typography variant="h3" component="h3">{filtered ? 'No matching activity' : 'No recorded activity'}</Typography><Typography color="text.secondary" sx={{ maxWidth: 480 }}>{filtered ? 'Try another severity, action, or date range, or clear your filters.' : 'Committed administrative changes and task outcomes will appear here.'}</Typography>{filtered && <Button startIcon={<FilterAltOffOutlined />} onClick={clearFilters}>Clear filters</Button>}</Stack>}
      {data && data.Items.length > 0 && visibleItems.length === 0 && <div className="system-table-empty">{t('No matching activity on this page.')}</div>}
      {data && visibleItems.length > 0 && <ActivityList key={queryKey} items={visibleItems} onActor={(id) => { setShowFilters(true); setQuery((current) => ({ ...current, page: 0, filters: { ...current.filters, ActorId: id } })); }} />}
      {failed && <Typography variant="body2" color="text.secondary" sx={{ px: 3, py: 3 }}>Activity could not be loaded. Refresh to read the current records.</Typography>}
      {data && <TablePagination component="div" count={data.TotalRecordCount} page={query.page} rowsPerPage={query.pageSize} rowsPerPageOptions={pageSizes} labelRowsPerPage="Activities per page:" onPageChange={(_event, page) => setQuery((current) => ({ ...current, page }))} onRowsPerPageChange={(event) => { const pageSize = Number(event.target.value); if (pageSizes.includes(pageSize)) setQuery((current) => ({ ...current, pageSize, page: 0 })); }} sx={paginationSx} />}
    </Paper>
    <Typography variant="caption" color="text.secondary" sx={{ mt: 1.5, px: 0.5, display: 'block' }}>Search, source and export apply to the current page. Live updates refresh the first page every 5 seconds and stop after an error.</Typography>
    <Typography variant="body2" color="text.secondary" sx={{ mt: 1.5, px: 0.5 }}>{data ? `${countLabel(data.RetentionDays, `Activity is retained for ${data.RetentionDays} day.`, `Activity is retained for ${data.RetentionDays} days.`)} ` : ''}Retention is managed by the server. User names show the current account name when available. Times use your local time zone.</Typography>
  </Box>;
}

function parseLogRecord(line: string): { raw: Record<string, unknown> | null; time: string; level: string; source: string; message: string } {
  try {
    const raw: unknown = JSON.parse(line);
    if (raw && typeof raw === 'object' && !Array.isArray(raw)) {
      const record = raw as Record<string, unknown>;
      const field = (names: string[]) => String(names.map((name) => record[name]).find((value) => typeof value === 'string' || typeof value === 'number') ?? '');
      return { raw: record, time: field(['time', 'timestamp', 'ts', 'date', 'Time', 'Timestamp']), level: field(['level', 'severity', 'Level', 'Severity']), source: field(['source', 'component', 'subsystem', 'Source', 'Component']), message: field(['message', 'msg', 'Message', 'Name']) || line };
    }
  } catch { /* Plain diagnostic lines remain readable without inventing fields. */ }
  return { raw: null, time: '', level: '', source: '', message: line };
}

function LogRecord({ line, index }: { line: string; index: number }) {
  const { t, locale } = useTranslation();
  const record = parseLogRecord(line);
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const date = record.time ? new Date(record.time) : undefined;
  const timestamp = date && Number.isFinite(date.getTime()) ? date.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : record.time;
  const json = record.raw ? JSON.stringify(record.raw, null, 2) : line;
  async function copyRecord() {
    try { await navigator.clipboard.writeText(json); setCopyState('copied'); }
    catch { setCopyState('failed'); }
  }
  return <li className="system-log-line"><details><summary className="system-log-summary"><span className="system-mono" title={record.time || t('Line number')}>{timestamp || `${index + 1}`}</span><span className={`system-log-level level-${record.level.toLowerCase()}`}>{record.level || '—'}</span><span className="system-log-source-name">{record.source || '—'}</span><span className="system-log-message">{record.message}</span></summary><pre className="system-json">{json}</pre><div className="system-json-action" style={{ padding: '0 16px 12px' }}><Button size="small" startIcon={<ContentCopyRounded />} onClick={() => void copyRecord()}>{copyState === 'copied' ? 'Copied' : record.raw ? 'Copy JSON' : 'Copy line'}</Button></div>{copyState === 'failed' && <Alert severity="error">Copy failed. Select and copy the text above.</Alert>}</details></li>;
}

function LogPreview({ file, onClose, onRefreshFiles }: { file: ServerLogFile; onClose: () => void; onRefreshFiles: () => void }) {
  const { t } = useTranslation();
  const [startIndex, setStartIndex] = useState(0);
  const [pageSize, setPageSize] = useState(10);
  const [revision, setRevision] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; cursorKey: string; result: ServerLogLinesResponse }>();
  const [failure, setFailure] = useState<{ key: string; cause: unknown }>();
  const [search, setSearch] = useState('');
  const [source, setSource] = useState('');
  const [live, setLive] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  const cursorKey = JSON.stringify([file.Name, startIndex, pageSize]);
  const requestKey = JSON.stringify([file.Name, startIndex, pageSize, revision]);
  const failed = failure?.key === requestKey;
  const missing = failed && failure?.cause instanceof ApiError && failure.cause.status === 404;
  const data = !missing && (loaded?.key === requestKey || (live && loaded?.cursorKey === cursorKey)) ? loaded?.result : undefined;
  const loading = loaded?.key !== requestKey && !failed;
  useEffect(() => { heading.current?.focus(); }, []);
  useEffect(() => {
    const controller = new AbortController();
    setFailure(undefined);
    void adminApi.getServerLogLines(file.Name, { StartIndex: startIndex, Limit: pageSize }, { signal: controller.signal })
      .then((result) => { if (!controller.signal.aborted) setLoaded({ key: requestKey, cursorKey, result }); })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setFailure({ key: requestKey, cause }); });
    return () => controller.abort();
  }, [file.Name, startIndex, pageSize, requestKey, cursorKey]);
  useEffect(() => {
    if (!live || loading || failed || !data) return;
    const tailStart = Math.max(0, data.TotalRecordCount - pageSize);
    if (startIndex !== tailStart) { setStartIndex(tailStart); return; }
    const timer = window.setTimeout(() => { if (document.visibilityState !== 'hidden') setRevision((value) => value + 1); }, 5000);
    const resume = () => { if (document.visibilityState !== 'hidden') setRevision((value) => value + 1); };
    document.addEventListener('visibilitychange', resume);
    return () => { window.clearTimeout(timer); document.removeEventListener('visibilitychange', resume); };
  }, [live, loading, failed, startIndex, pageSize, revision, data]);
  const sources = [...new Set([source, ...(data?.Items ?? []).map((line) => parseLogRecord(line).source)].filter(Boolean))];
  const visibleLines = (data?.Items ?? []).map((line, index) => ({ line, index: startIndex + index })).filter(({ line }) => (!source || parseLogRecord(line).source === source) && (!search.trim() || line.toLowerCase().includes(search.trim().toLowerCase())));
  function reload() { setStartIndex(0); setRevision((current) => current + 1); }
  return <Paper component="section" aria-labelledby="log-preview-title" aria-busy={loading} variant="outlined" sx={{ mt: 3, overflow: 'hidden' }}>
    <Stack direction="row" sx={{ px: { xs: 2.5, sm: 3 }, py: 2.2, justifyContent: 'space-between', alignItems: 'flex-start', gap: 1 }}><Box sx={{ minWidth: 0 }}><Typography variant="h4" component="h2" ref={heading} tabIndex={-1} id="log-preview-title" sx={{ overflowWrap: 'anywhere', outline: 'none' }}>{file.Name}</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.8 }}>The file can grow while you read. Reload to read its latest content from the beginning.</Typography></Box><IconButton aria-label="Close log preview" onClick={onClose} size="small"><CloseRounded /></IconButton></Stack>
    <Stack direction="row" sx={{ px: { xs: 2.5, sm: 3 }, pb: 2, flexWrap: 'wrap', alignItems: 'center', gap: 1 }}><Button startIcon={<RefreshRounded />} onClick={reload} disabled={loading || Boolean(missing)}>Reload log</Button>{!missing && <Button component="a" href={adminApi.serverLogDownloadURL(file.Name)} download startIcon={<DownloadRounded />}>Download log</Button>}<TextField select id="log-page-size" label="Lines per page" size="small" value={pageSize} disabled={Boolean(missing)} onChange={(event) => { const size = Number(event.target.value); if ([10, 50, 200].includes(size)) { setPageSize(size); setStartIndex(0); } }} sx={{ minWidth: 150, ml: { sm: 'auto' } }}>{[10, 50, 200].map((size) => <MenuItem key={size} value={size}>{size}</MenuItem>)}</TextField></Stack>
    {loading && !data && <LoadingRows label="Loading log lines" />}
    {missing && <Box sx={{ px: 3, pb: 3 }}><Alert severity="warning" action={<Button color="inherit" size="small" onClick={onRefreshFiles}>Refresh files</Button>}>This log file is no longer available. It may have rotated or expired. Refresh the file list to choose an available log.</Alert></Box>}
    {failed && !missing && <Box sx={{ px: 3, pb: 3 }}><ErrorNotice error={failure?.cause} retry={() => setRevision((current) => current + 1)} /></Box>}
    {data && <>
      <Stack direction="row" sx={{ px: { xs: 2.5, sm: 3 }, py: 1.5, borderTop: 1, borderColor: 'divider', bgcolor: 'background.default', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1 }}><Typography variant="caption" color="text.secondary">{data.Items.length ? `Lines ${data.StartIndex + 1}–${data.NextIndex} of ${data.TotalRecordCount}` : 'No lines at this position'}</Typography><Typography variant="caption" color="text.secondary">Snapshot size: {byteSize(data.SnapshotSize)}</Typography></Stack>
      {data.Items.length > 0 ? <><div className="system-activity-tools" style={{ paddingTop: 14 }}><TextField className="system-log-search" size="small" label="Search these lines" value={search} onChange={(event) => setSearch(event.target.value)} /><TextField className="system-log-source" select size="small" label="Source in these lines" value={source} onChange={(event) => setSource(event.target.value)}><MenuItem value="">All sources</MenuItem>{sources.map((name) => <MenuItem key={name} value={name}>{name}</MenuItem>)}</TextField><Button variant="outlined" aria-pressed={live} onClick={() => setLive((value) => !value)}>{live ? failed ? 'Live updates paused' : 'Live updates on' : 'Live updates off'}</Button></div><div className="system-log-summary system-log-header" aria-hidden="true"><span>{t('Time')}</span><span>{t('Severity')}</span><span>{t('Source')}</span><span>{t('Message')}</span></div><ul className="system-log-lines" aria-label={t('Log text')}>{visibleLines.map(({ line, index }) => <LogRecord key={`${index}:${line}`} line={line} index={index} />)}</ul>{visibleLines.length === 0 && <div className="system-table-empty">{t('No matching log lines on this page.')}</div>}</> : <Typography variant="body2" color="text.secondary" sx={{ p: 3 }}>{startIndex === 0 ? 'This log file has no readable lines yet. Reload the log to check for new entries.' : 'The file has no lines at this position. Reload the log to read from the beginning.'}</Typography>}
      <Typography variant="caption" color="text.secondary" sx={{ p: 2, display: 'block' }}>Search and source filters apply to these loaded lines. Live updates follow the latest lines every 5 seconds and stop after an error.</Typography>
      <Stack direction="row" sx={{ p: 2, borderTop: 1, borderColor: 'divider', justifyContent: 'flex-end', flexWrap: 'wrap', gap: 1 }}><Button onClick={() => { setLive(false); setStartIndex(Math.max(0, startIndex - pageSize)); }} disabled={startIndex === 0}>Previous lines</Button><Button variant="outlined" onClick={() => { setLive(false); setStartIndex(data.NextIndex); }} disabled={data.NextIndex >= data.TotalRecordCount || data.NextIndex <= startIndex}>Read more</Button></Stack>
    </>}
  </Paper>;
}

function LogFileName({ file, selected, onSelect }: { file: ServerLogFile; selected: boolean; onSelect: (trigger: HTMLButtonElement) => void }) {
  return <Tooltip title={selected ? `Viewing ${file.Name}` : `View ${file.Name}`}><span><Button size="small" color="inherit" startIcon={<SubjectRounded sx={{ color: 'text.secondary' }} />} onClick={(event) => onSelect(event.currentTarget)} disabled={selected} aria-label={`View ${file.Name}`} sx={{ minWidth: 0, maxWidth: '100%', px: 0, justifyContent: 'flex-start', fontWeight: 400 }}><Box component="span" className="mono" sx={{ minWidth: 0, overflow: 'hidden', whiteSpace: 'nowrap', textOverflow: 'ellipsis' }}>{file.Name}</Box></Button></span></Tooltip>;
}

function LogDownload({ file }: { file: ServerLogFile }) {
  return <Button component="a" size="small" href={adminApi.serverLogDownloadURL(file.Name)} download startIcon={<DownloadRounded />} aria-label={`Download ${file.Name}`} sx={{ px: 0.5 }}>Download</Button>;
}

function LogsPanel() {
  const { countLabel } = useTranslation();
  const [query, setQuery] = useState({ page: 0, pageSize: 50 });
  const [revision, setRevision] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; result: ServerLogsResponse }>();
  const [failure, setFailure] = useState<{ key: string; cause: unknown }>();
  const [selected, setSelected] = useState<ServerLogFile>();
  const previewTrigger = useRef<HTMLButtonElement | null>(null);
  const requestKey = JSON.stringify([query, revision]);
  const data = loaded?.key === requestKey ? loaded.result : undefined;
  const failed = failure?.key === requestKey;
  const loading = !data && !failed;
  useEffect(() => {
    const controller = new AbortController();
    setLoaded(undefined);
    setFailure(undefined);
    void adminApi.getServerLogs({ StartIndex: query.page * query.pageSize, Limit: query.pageSize }, { signal: controller.signal })
      .then((result) => {
        if (controller.signal.aborted) return;
        if (query.page > 0 && query.page * query.pageSize >= result.TotalRecordCount) {
          setQuery((current) => ({ ...current, page: Math.max(0, Math.ceil(result.TotalRecordCount / query.pageSize) - 1) }));
          return;
        }
        setLoaded({ key: requestKey, result });
      })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setFailure({ key: requestKey, cause }); });
    return () => controller.abort();
  }, [query, requestKey]);
  function refresh() { setSelected(undefined); setRevision((current) => current + 1); }
  function changePage(page: number, pageSize = query.pageSize) { setSelected(undefined); setQuery({ page, pageSize }); }
  function selectFile(file: ServerLogFile, trigger: HTMLButtonElement) { previewTrigger.current = trigger; setSelected(file); }
  function closePreview() { setSelected(undefined); requestAnimationFrame(() => previewTrigger.current?.focus()); }
  return <Box aria-busy={loading}>
    {failed && <Box sx={{ mb: 3 }}><ErrorNotice error={failure?.cause} retry={refresh} /></Box>}
    {data && (data.Status.Closed || data.Status.Degraded || !data.Status.Healthy) && <Alert severity="warning" sx={{ mb: 2.5 }}><Typography variant="body2" sx={{ fontWeight: 600 }}>{data.Status.Closed ? 'Log storage is closed' : 'Log storage is degraded'}</Typography><Typography variant="body2">{data.Status.Closed ? 'New diagnostics are not being written.' : 'Some diagnostics may be missing. Check server storage and refresh the file list.'}</Typography></Alert>}
    <Paper variant="outlined" sx={panelSx}>
      <Stack direction="row" sx={sectionHeaderSx}><Stack direction="row" sx={{ alignItems: 'center', gap: 1.2 }}><Typography variant="h4" component="h2">Log files</Typography>{data && <Chip size="small" label={data.TotalRecordCount.toLocaleString(adminLocale())} sx={{ bgcolor: colors.surface }} />}</Stack><Tooltip title="Refresh files"><span><IconButton aria-label="Refresh files" onClick={refresh} disabled={loading}><RefreshRounded fontSize="small" /></IconButton></span></Tooltip></Stack>
      {loading && <LoadingRows label="Loading log files" />}
      {data && data.Items.length === 0 && <Stack spacing={1.5} sx={{ alignItems: 'center', p: { xs: 3, sm: 5 }, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}><SubjectRounded sx={{ fontSize: 40, color: 'primary.main' }} /><Typography variant="h3" component="h3">No log files</Typography><Typography color="text.secondary" sx={{ maxWidth: 480 }}>No retained diagnostic files are available. Refresh after the server writes a log entry.</Typography></Stack>}
      {data && data.Items.length > 0 && <>
        <Box component="ul" aria-label="Server log files" sx={{ display: { xs: 'block', lg: 'none' }, listStyle: 'none', p: 0, m: 0 }}>{data.Items.map((file) => <Box component="li" key={file.Name} sx={{ p: 2.5, borderTop: 1, borderColor: 'divider' }}><LogFileName file={file} selected={selected?.Name === file.Name} onSelect={(trigger) => selectFile(file, trigger)} /><Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>Modified {localTime(file.DateModified)} · {byteSize(file.Size)}</Typography><Typography variant="caption" color="text.secondary">Created {localTime(file.DateCreated)}</Typography><Box sx={{ mt: 1 }}><LogDownload file={file} /></Box></Box>)}</Box>
        <TableContainer sx={{ display: { xs: 'none', lg: 'block' }, borderRadius: 0 }}><Table aria-label="Server log files" sx={tableSx}><TableHead><TableRow><TableCell sx={{ width: '32%', '&&': { pl: 3 } }}>File</TableCell><TableCell sx={{ width: '22%' }}>Created</TableCell><TableCell sx={{ width: '22%' }}>Modified</TableCell><TableCell sx={{ width: '10%' }}>Size</TableCell><TableCell align="right" sx={{ width: '14%', '&&': { pr: 3 } }}>Download</TableCell></TableRow></TableHead><TableBody>{data.Items.map((file) => <TableRow key={file.Name} selected={selected?.Name === file.Name}><TableCell component="th" scope="row" sx={{ '&&': { pl: 3 } }}><LogFileName file={file} selected={selected?.Name === file.Name} onSelect={(trigger) => selectFile(file, trigger)} /></TableCell><TableCell sx={{ whiteSpace: 'nowrap', color: 'text.secondary' }}><time dateTime={file.DateCreated}>{localTime(file.DateCreated)}</time></TableCell><TableCell sx={{ whiteSpace: 'nowrap', color: 'text.secondary' }}><time dateTime={file.DateModified}>{localTime(file.DateModified)}</time></TableCell><TableCell sx={{ whiteSpace: 'nowrap', color: 'text.secondary' }}>{byteSize(file.Size)}</TableCell><TableCell align="right" sx={{ '&&': { pr: 3 } }}><LogDownload file={file} /></TableCell></TableRow>)}</TableBody></Table></TableContainer>
      </>}
      {failed && <Typography variant="body2" color="text.secondary" sx={{ px: 3, py: 3 }}>Log storage could not be read. Refresh the file list to try again.</Typography>}
      {data && <TablePagination component="div" count={data.TotalRecordCount} page={query.page} rowsPerPage={query.pageSize} rowsPerPageOptions={pageSizes} labelRowsPerPage="Files per page:" onPageChange={(_event, page) => changePage(page)} onRowsPerPageChange={(event) => { const pageSize = Number(event.target.value); if (pageSizes.includes(pageSize)) changePage(0, pageSize); }} sx={paginationSx} />}
    </Paper>
    <Typography variant="body2" color="text.secondary" sx={{ mt: 2.5 }}>Downloads use a fixed snapshot. Files that are still being written include only the content captured when the download begins.</Typography>
    {data && <Typography variant="caption" color="text.secondary" component="p" sx={{ mt: 1, mb: 0 }}>{data.Status.Healthy && !data.Status.Degraded && !data.Status.Closed ? 'Log storage is healthy. ' : ''}Server policy keeps logs for up to {data.Status.RetentionDays} {countLabel(data.Status.RetentionDays, 'day, with at most', 'days, with at most')} {data.Status.MaxFiles} {countLabel(data.Status.MaxFiles, 'file of', 'files of')} {byteSize(data.Status.MaxFileBytes)} each. Files may be removed earlier to stay within these limits. Minimum free storage: {byteSize(data.Status.MinFreeBytes)}. Format: JSONL. This policy is read-only.</Typography>}
    {selected && <LogPreview key={selected.Name} file={selected} onClose={closePreview} onRefreshFiles={refresh} />}
  </Box>;
}

export function ObservabilityPage() {
  const [tab, setTab] = useState<'activity' | 'logs'>('activity');
  return <Box className="system-page system-observability">
    <PageHeading title="Activity & logs" description="Review server events and retained diagnostic records." />
    <Tabs value={tab} onChange={(_event, value: 'activity' | 'logs') => setTab(value)} aria-label="Activity and server logs" className="system-log-tabs" sx={{ mb: 2.25 }}><Tab value="activity" label="Activity" id="observability-activity-tab" aria-controls="observability-activity-panel" /><Tab value="logs" label="Server logs" id="observability-logs-tab" aria-controls="observability-logs-panel" /></Tabs>
    {tab === 'activity' ? <Box role="tabpanel" id="observability-activity-panel" aria-labelledby="observability-activity-tab"><ActivityPanel /></Box> : <Box role="tabpanel" id="observability-logs-panel" aria-labelledby="observability-logs-tab"><LogsPanel /></Box>}
  </Box>;
}
