import { Fragment, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Chip, Collapse, IconButton, MenuItem, Paper, Skeleton, Stack, Tab, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, Tabs, TextField, Typography } from '@mui/material';
import CloseRounded from '@mui/icons-material/CloseRounded';
import DownloadRounded from '@mui/icons-material/DownloadRounded';
import ExpandLessRounded from '@mui/icons-material/ExpandLessRounded';
import ExpandMoreRounded from '@mui/icons-material/ExpandMoreRounded';
import FilterAltOffOutlined from '@mui/icons-material/FilterAltOffOutlined';
import HistoryRounded from '@mui/icons-material/HistoryRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import SearchRounded from '@mui/icons-material/SearchRounded';
import SubjectRounded from '@mui/icons-material/SubjectRounded';
import { activityActions, activitySeverities, adminApi, ApiError, isAbortError } from './api';
import type { ActivityAction, ActivityEntry, ActivityQuery, ActivityResourceKind, ActivityResponse, ActivitySeverity, ServerLogFile, ServerLogLinesResponse, ServerLogsResponse } from './api';
import { ErrorNotice, PageHeading } from './components';

const actionLabels: Record<ActivityAction, string> = {
  'user.created': 'User created', 'user.updated': 'User updated', 'user.password_reset': 'User password reset',
  'session.login': 'Signed in', 'session.revoked': 'Session revoked',
  'application_key.created': 'API key created', 'application_key.revealed': 'API key revealed', 'application_key.revoked': 'API key revoked',
  'device.updated': 'Device updated', 'device.removed': 'Device removed',
  'library.created': 'Library created', 'library.removed': 'Library removed',
  'scan.requested': 'Scan requested', 'scan.cancel_requested': 'Scan cancellation requested', 'scan.finished': 'Scan finished',
  'metadata.updated': 'Metadata updated', 'settings.updated': 'Settings updated',
  'task.admitted': 'Task admitted', 'task.cancel_requested': 'Task cancellation requested', 'task.finished': 'Task finished', 'task.schedule_updated': 'Task schedule updated',
  'backup.requested': 'Backup requested', 'backup.cancel_requested': 'Backup cancellation requested', 'backup.finished': 'Backup finished',
  'backup.imported': 'Backup imported', 'backup.delete_requested': 'Backup deletion requested', 'backup.deleted': 'Backup deleted', 'backup.downloaded': 'Backup download prepared',
  'restore.requested': 'Restore requested', 'restore.planned': 'Restore planned', 'restore.apply_requested': 'Restore application requested',
  'restore.applied': 'Restore applied', 'restore.rollback_requested': 'Restore rollback requested', 'restore.cancel_requested': 'Restore cancellation requested', 'restore.failed': 'Restore failed',
};
const resourceLabels: Record<ActivityResourceKind, string> = {
  user: 'User', session: 'Session', application_key: 'API key', device: 'Device', library: 'Library', scan: 'Scan',
  item: 'Item', settings: 'Settings', task: 'Task', task_run: 'Task run', backup: 'Backup', restore: 'Restore',
};
const sourceLabels = { native: 'Native', emby: 'Emby', system: 'System' };
const pageSizes = [25, 50, 100, 200];
const paginationSx = {
  borderTop: 1, borderColor: 'divider',
  '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', justifyContent: 'flex-end', px: { xs: 2, sm: 3 }, py: 1, gap: 0.5 },
  '& .MuiTablePagination-spacer': { display: { xs: 'none', sm: 'block' } },
  '& .MuiTablePagination-actions': { ml: { xs: 1, sm: 2 } },
};

function localTime(value: string): string {
  return new Date(value).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
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
  return <Stack role="status" aria-live="polite" aria-label={label} spacing={1} sx={{ p: 3, borderTop: 1, borderColor: 'divider' }}><Skeleton height={76} /><Skeleton height={76} /><Skeleton height={76} /></Stack>;
}

function Severity({ severity }: { severity: ActivitySeverity }) {
  const color = severity === 'Warn' ? 'warning' : severity === 'Error' || severity === 'Fatal' ? 'error' : severity === 'Info' ? 'info' : 'default';
  return <Chip size="small" variant="outlined" label={severity} color={color} />;
}

function Actor({ actor, onFilter }: { actor: ActivityEntry['Actor']; onFilter: (id: string) => void }) {
  const label = actor.Kind === 'system' ? 'System' : actor.Kind === 'application_key' ? 'API key' : actor.Name || 'User';
  return <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
    <Typography variant="body2" sx={{ fontWeight: 600 }}>{label}</Typography>
    {actor.Id && <><Typography variant="caption" color="text.secondary" component="div" className="mono">{actor.Id}</Typography><Button size="small" onClick={() => onFilter(actor.Id!)} aria-label={`Filter by actor ${actor.Id}`} sx={{ ml: -1, mt: 0.5 }}>Filter actor</Button></>}
  </Box>;
}

function ActivityDetails({ entry, id, expanded }: { entry: ActivityEntry; id: string; expanded: boolean }) {
  return <Collapse in={expanded} unmountOnExit id={id}>
    <Box sx={{ p: 2.5, bgcolor: 'background.default', borderTop: 1, borderColor: 'divider' }}>
      {entry.Overview && <Typography variant="body2" sx={{ mb: 2 }}>{entry.Overview}</Typography>}
      <Stack direction="row" sx={{ flexWrap: 'wrap', columnGap: 4, rowGap: 1.5 }}>
        <Box><Typography variant="caption" color="text.secondary">Activity ID</Typography><Typography variant="body2" className="mono">{entry.Id}</Typography></Box>
        <Box><Typography variant="caption" color="text.secondary">Affected count</Typography><Typography variant="body2" className="mono">{entry.Count}</Typography></Box>
        {entry.Revision !== null && <Box><Typography variant="caption" color="text.secondary">Revision</Typography><Typography variant="body2" className="mono">{entry.Revision}</Typography></Box>}
        {entry.State !== null && <Box><Typography variant="caption" color="text.secondary">Outcome</Typography><Typography variant="body2" sx={{ textTransform: 'capitalize' }}>{entry.State}</Typography></Box>}
      </Stack>
      {entry.ChangedFields.length > 0 && <Box sx={{ mt: 2 }}><Typography variant="caption" color="text.secondary" component="div" sx={{ mb: 0.8 }}>Changed fields</Typography><Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.8 }}>{entry.ChangedFields.map((field) => <Chip key={field} label={field} size="small" variant="outlined" />)}</Stack></Box>}
    </Box>
  </Collapse>;
}

function ActivityList({ items, onActor }: { items: ActivityEntry[]; onActor: (id: string) => void }) {
  const [expanded, setExpanded] = useState<string>();
  function toggle(id: string) { setExpanded((current) => current === id ? undefined : id); }
  return <>
    <Box component="ul" aria-label="Activity records" sx={{ display: { xs: 'block', lg: 'none' }, listStyle: 'none', p: 0, m: 0 }}>
      {items.map((entry) => <Box component="li" key={entry.Id} sx={{ borderTop: 1, borderColor: 'divider' }}>
        <Stack spacing={2} sx={{ p: 2.5 }}>
          <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'flex-start', gap: 1 }}><Box sx={{ minWidth: 0 }}><Typography sx={{ fontWeight: 650 }}>{entry.Name}</Typography><Typography variant="caption" color="text.secondary" component="div" className="mono" sx={{ overflowWrap: 'anywhere' }}>{entry.Action}</Typography></Box><Severity severity={entry.Severity} /></Stack>
          <Typography variant="body2" color="text.secondary" component="time" dateTime={entry.Date}>{localTime(entry.Date)}</Typography>
          <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 3 }}><Box sx={{ flex: '1 1 130px', minWidth: 0 }}><Typography variant="caption" color="text.secondary">Actor</Typography><Actor actor={entry.Actor} onFilter={onActor} /></Box><Box sx={{ flex: '1 1 130px', minWidth: 0, overflowWrap: 'anywhere' }}><Typography variant="caption" color="text.secondary">Resource</Typography><Typography variant="body2">{resourceLabels[entry.Resource.Kind]}</Typography><Typography variant="caption" className="mono">{entry.Resource.Id}</Typography></Box></Stack>
          <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center', gap: 1 }}><Chip size="small" label={sourceLabels[entry.Source]} sx={{ bgcolor: 'background.default' }} /><Button size="small" endIcon={expanded === entry.Id ? <ExpandLessRounded /> : <ExpandMoreRounded />} aria-expanded={expanded === entry.Id} aria-controls={`activity-card-${entry.Id}`} onClick={() => toggle(entry.Id)}>Details</Button></Stack>
        </Stack>
        <ActivityDetails entry={entry} expanded={expanded === entry.Id} id={`activity-card-${entry.Id}`} />
      </Box>)}
    </Box>
    <TableContainer sx={{ display: { xs: 'none', lg: 'block' } }}><Table aria-label="Activity records" sx={{ tableLayout: 'fixed', minWidth: 820 }}>
      <TableHead><TableRow><TableCell sx={{ pl: 3, width: '17%' }}>When</TableCell><TableCell sx={{ width: '23%' }}>Action</TableCell><TableCell sx={{ width: '11%' }}>Severity</TableCell><TableCell sx={{ width: '20%' }}>Actor</TableCell><TableCell sx={{ width: '10%' }}>Source</TableCell><TableCell sx={{ width: '19%' }}>Resource</TableCell></TableRow></TableHead>
      <TableBody>{items.map((entry) => <Fragment key={entry.Id}>
        <TableRow sx={{ '& > .MuiTableCell-root': { verticalAlign: 'top', overflowWrap: 'anywhere' } }}>
          <TableCell sx={{ pl: 3 }}><Typography variant="body2" component="time" dateTime={entry.Date}>{localTime(entry.Date)}</Typography><Button size="small" sx={{ ml: -1, mt: 1 }} endIcon={expanded === entry.Id ? <ExpandLessRounded /> : <ExpandMoreRounded />} aria-label={`Details for activity ${entry.Id}`} aria-expanded={expanded === entry.Id} aria-controls={`activity-table-${entry.Id}`} onClick={() => toggle(entry.Id)}>Details</Button></TableCell>
          <TableCell component="th" scope="row"><Typography variant="body2" sx={{ fontWeight: 650 }}>{entry.Name}</Typography><Typography variant="caption" color="text.secondary" component="div" className="mono" sx={{ mt: 0.5 }}>{entry.Action}</Typography></TableCell>
          <TableCell><Severity severity={entry.Severity} /></TableCell><TableCell><Actor actor={entry.Actor} onFilter={onActor} /></TableCell><TableCell><Typography variant="body2">{sourceLabels[entry.Source]}</Typography></TableCell>
          <TableCell sx={{ pr: 3 }}><Typography variant="body2">{resourceLabels[entry.Resource.Kind]}</Typography><Typography variant="caption" color="text.secondary" className="mono">{entry.Resource.Id}</Typography></TableCell>
        </TableRow>
        {expanded === entry.Id && <TableRow><TableCell colSpan={6} sx={{ p: 0 }}><ActivityDetails entry={entry} expanded id={`activity-table-${entry.Id}`} /></TableCell></TableRow>}
      </Fragment>)}</TableBody>
    </Table></TableContainer>
  </>;
}

type ActivityPeriod = 'all' | 'day' | 'week' | 'month' | 'custom';
const emptyFilters = { severity: '' as ActivitySeverity | '', action: '' as ActivityAction | '', period: 'all' as ActivityPeriod, since: '' };

function ActivityPanel() {
  const [filters, setFilters] = useState(emptyFilters);
  const [query, setQuery] = useState<{ filters: ActivityQuery; page: number; pageSize: number }>({ filters: {}, page: 0, pageSize: 50 });
  const [revision, setRevision] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; result: ActivityResponse }>();
  const [failure, setFailure] = useState<{ key: string; cause: unknown }>();
  const requestKey = JSON.stringify([query, revision]);
  const data = loaded?.key === requestKey ? loaded.result : undefined;
  const failed = failure?.key === requestKey;
  const loading = !data && !failed;
  const dateIssue = filters.period === 'custom' && (!filters.since || !Number.isFinite(Date.parse(filters.since))) ? 'Choose a valid date and time.' : undefined;
  const filtered = Object.values(query.filters).some(Boolean);

  useEffect(() => {
    const controller = new AbortController();
    setLoaded(undefined);
    setFailure(undefined);
    void adminApi.getActivity({ ...query.filters, StartIndex: query.page * query.pageSize, Limit: query.pageSize }, { signal: controller.signal })
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

  function refresh() { setRevision((current) => current + 1); }
  function applyFilters(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (dateIssue) return;
    const days = filters.period === 'day' ? 1 : filters.period === 'week' ? 7 : 30;
    const minDate = filters.period === 'all' ? undefined : filters.period === 'custom' ? new Date(filters.since).toISOString() : new Date(Date.now() - days * 86400000).toISOString();
    setQuery((current) => ({ ...current, page: 0, filters: { Severity: filters.severity || undefined, Action: filters.action || undefined, MinDate: minDate, ActorId: current.filters.ActorId } }));
    refresh();
  }
  function clearFilters() { setFilters(emptyFilters); setQuery((current) => ({ ...current, filters: {}, page: 0 })); refresh(); }

  return <Box aria-busy={loading}>
    {failed && <Box sx={{ mb: 3 }}><ErrorNotice error={failure?.cause} retry={refresh} /></Box>}
    <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
      <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1, px: { xs: 2.5, sm: 3 }, py: 2.2 }}><Stack direction="row" sx={{ alignItems: 'center', gap: 1.2 }}><Typography variant="h4" component="h2">Server activity</Typography>{data && <Chip size="small" label={data.TotalRecordCount.toLocaleString()} sx={{ bgcolor: 'background.default' }} />}</Stack><Button size="small" startIcon={<RefreshRounded />} onClick={refresh} disabled={loading}>Refresh activity</Button></Stack>
      <Box component="form" onSubmit={applyFilters} sx={{ px: { xs: 2.5, sm: 3 }, pb: 2.5 }}>
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr', xl: '1fr 1.4fr 1fr' }, gap: 2 }}>
          <TextField select id="activity-severity" label="Severity" value={filters.severity} onChange={(event) => setFilters((current) => ({ ...current, severity: event.target.value as ActivitySeverity | '' }))}><MenuItem value="">All severities</MenuItem>{activitySeverities.map((severity) => <MenuItem key={severity} value={severity}>{severity}</MenuItem>)}</TextField>
          <TextField select id="activity-action" label="Action" value={filters.action} onChange={(event) => setFilters((current) => ({ ...current, action: event.target.value as ActivityAction | '' }))}><MenuItem value="">All actions</MenuItem>{activityActions.map((action) => <MenuItem key={action} value={action}>{actionLabels[action]}</MenuItem>)}</TextField>
          <TextField select id="activity-period" label="Date range" value={filters.period} onChange={(event) => setFilters((current) => ({ ...current, period: event.target.value as ActivityPeriod }))}><MenuItem value="all">All retained activity</MenuItem><MenuItem value="day">Last 24 hours</MenuItem><MenuItem value="week">Last 7 days</MenuItem><MenuItem value="month">Last 30 days</MenuItem><MenuItem value="custom">Since a date and time</MenuItem></TextField>
          {filters.period === 'custom' && <TextField id="activity-since" label="Activity since" type="datetime-local" value={filters.since} onChange={(event) => setFilters((current) => ({ ...current, since: event.target.value }))} error={Boolean(dateIssue)} helperText={dateIssue ?? 'Uses your local time zone.'} slotProps={{ inputLabel: { shrink: true } }} />}
        </Box>
        {query.filters.ActorId && <Chip label={`Actor: ${query.filters.ActorId}`} onDelete={() => setQuery((current) => ({ ...current, page: 0, filters: { ...current.filters, ActorId: undefined } }))} sx={{ mt: 2, maxWidth: '100%', '& .MuiChip-label': { overflowWrap: 'anywhere' } }} />}
        <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1, mt: 2 }}><Button type="submit" variant="outlined" startIcon={<SearchRounded />} disabled={Boolean(dateIssue)}>Apply filters</Button><Button color="secondary" startIcon={<FilterAltOffOutlined />} onClick={clearFilters} disabled={!filtered && !filters.severity && !filters.action && filters.period === 'all'}>Clear filters</Button></Stack>
        {query.filters.MinDate && <Typography variant="caption" color="text.secondary" component="p" sx={{ mt: 1.5, mb: 0 }}>Showing activity since {localTime(query.filters.MinDate)}.</Typography>}
      </Box>
      {loading && <LoadingRows label="Loading activity" />}
      {data && data.Items.length === 0 && <Stack spacing={1.5} sx={{ alignItems: 'center', p: { xs: 3, sm: 5 }, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}><HistoryRounded sx={{ fontSize: 40, color: 'primary.main' }} /><Typography variant="h3" component="h3">{filtered ? 'No matching activity' : 'No recorded activity'}</Typography><Typography color="text.secondary" sx={{ maxWidth: 480 }}>{filtered ? 'Try another severity, action, or date range, or clear your filters.' : 'Committed administrative changes and task outcomes will appear here.'}</Typography>{filtered && <Button startIcon={<FilterAltOffOutlined />} onClick={clearFilters}>Clear filters</Button>}</Stack>}
      {data && data.Items.length > 0 && <ActivityList key={requestKey} items={data.Items} onActor={(id) => setQuery((current) => ({ ...current, page: 0, filters: { ...current.filters, ActorId: id } }))} />}
      {failed && <Typography variant="body2" color="text.secondary" sx={{ px: 3, py: 3 }}>Activity could not be loaded. Refresh to read the current records.</Typography>}
      {data && <TablePagination component="div" count={data.TotalRecordCount} page={query.page} rowsPerPage={query.pageSize} rowsPerPageOptions={pageSizes} labelRowsPerPage="Activities per page:" onPageChange={(_event, page) => setQuery((current) => ({ ...current, page }))} onRowsPerPageChange={(event) => { const pageSize = Number(event.target.value); if (pageSizes.includes(pageSize)) setQuery((current) => ({ ...current, pageSize, page: 0 })); }} sx={paginationSx} />}
    </Paper>
    <Typography variant="body2" color="text.secondary" sx={{ mt: 2.5, px: 0.5 }}>{data ? `Activity is retained for ${data.RetentionDays} days. ` : ''}Retention is managed by the server. User names show the current account name when available. Times use your local time zone.</Typography>
  </Box>;
}

function LogPreview({ file, onClose, onRefreshFiles }: { file: ServerLogFile; onClose: () => void; onRefreshFiles: () => void }) {
  const [startIndex, setStartIndex] = useState(0);
  const [pageSize, setPageSize] = useState(10);
  const [revision, setRevision] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; result: ServerLogLinesResponse }>();
  const [failure, setFailure] = useState<{ key: string; cause: unknown }>();
  const heading = useRef<HTMLHeadingElement>(null);
  const requestKey = JSON.stringify([file.Name, startIndex, pageSize, revision]);
  const data = loaded?.key === requestKey ? loaded.result : undefined;
  const failed = failure?.key === requestKey;
  const loading = !data && !failed;
  const missing = failed && failure?.cause instanceof ApiError && failure.cause.status === 404;
  useEffect(() => { heading.current?.focus(); }, []);
  useEffect(() => {
    const controller = new AbortController();
    setLoaded(undefined);
    setFailure(undefined);
    void adminApi.getServerLogLines(file.Name, { StartIndex: startIndex, Limit: pageSize }, { signal: controller.signal })
      .then((result) => { if (!controller.signal.aborted) setLoaded({ key: requestKey, result }); })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setFailure({ key: requestKey, cause }); });
    return () => controller.abort();
  }, [file.Name, startIndex, pageSize, requestKey]);
  function reload() { setStartIndex(0); setRevision((current) => current + 1); }
  return <Paper component="section" aria-labelledby="log-preview-title" aria-busy={loading} variant="outlined" sx={{ mt: 3, overflow: 'hidden' }}>
    <Stack direction="row" sx={{ px: { xs: 2.5, sm: 3 }, py: 2.2, justifyContent: 'space-between', alignItems: 'flex-start', gap: 1 }}><Box sx={{ minWidth: 0 }}><Typography variant="h4" component="h2" ref={heading} tabIndex={-1} id="log-preview-title" sx={{ overflowWrap: 'anywhere', outline: 'none' }}>{file.Name}</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.8 }}>The file can grow while you read. Reload to read its latest content from the beginning.</Typography></Box><IconButton aria-label="Close log preview" onClick={onClose} size="small"><CloseRounded /></IconButton></Stack>
    <Stack direction="row" sx={{ px: { xs: 2.5, sm: 3 }, pb: 2, flexWrap: 'wrap', alignItems: 'center', gap: 1 }}><Button startIcon={<RefreshRounded />} onClick={reload} disabled={loading || Boolean(missing)}>Reload log</Button>{!missing && <Button component="a" href={adminApi.serverLogDownloadURL(file.Name)} download startIcon={<DownloadRounded />}>Download log</Button>}<TextField select id="log-page-size" label="Lines per page" size="small" value={pageSize} disabled={Boolean(missing)} onChange={(event) => { const size = Number(event.target.value); if ([10, 50, 200].includes(size)) { setPageSize(size); setStartIndex(0); } }} sx={{ minWidth: 150, ml: { sm: 'auto' } }}>{[10, 50, 200].map((size) => <MenuItem key={size} value={size}>{size}</MenuItem>)}</TextField></Stack>
    {loading && <LoadingRows label="Loading log lines" />}
    {missing && <Box sx={{ px: 3, pb: 3 }}><Alert severity="warning" action={<Button color="inherit" size="small" onClick={onRefreshFiles}>Refresh files</Button>}>This log file is no longer available. It may have rotated or expired. Refresh the file list to choose an available log.</Alert></Box>}
    {failed && !missing && <Box sx={{ px: 3, pb: 3 }}><ErrorNotice error={failure?.cause} retry={() => setRevision((current) => current + 1)} /></Box>}
    {data && <>
      <Stack direction="row" sx={{ px: { xs: 2.5, sm: 3 }, py: 1.5, borderTop: 1, borderColor: 'divider', bgcolor: 'background.default', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1 }}><Typography variant="caption" color="text.secondary">{data.Items.length ? `Lines ${data.StartIndex + 1}–${data.NextIndex} of ${data.TotalRecordCount}` : 'No lines at this position'}</Typography><Typography variant="caption" color="text.secondary">Snapshot size: {byteSize(data.SnapshotSize)}</Typography></Stack>
      {data.Items.length > 0 ? <Box component="pre" aria-label="Log text" sx={{ m: 0, p: { xs: 2.5, sm: 3 }, maxHeight: '65vh', overflow: 'auto', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontFamily: 'monospace', fontSize: 12, lineHeight: 1.8, bgcolor: 'background.default' }}>{data.Items.join('\n')}</Box> : <Typography variant="body2" color="text.secondary" sx={{ p: 3 }}>{startIndex === 0 ? 'This log file has no readable lines yet. Reload the log to check for new entries.' : 'The file has no lines at this position. Reload the log to read from the beginning.'}</Typography>}
      <Stack direction="row" sx={{ p: 2, borderTop: 1, borderColor: 'divider', justifyContent: 'flex-end', flexWrap: 'wrap', gap: 1 }}><Button onClick={() => setStartIndex(Math.max(0, startIndex - pageSize))} disabled={startIndex === 0}>Previous lines</Button><Button variant="outlined" onClick={() => setStartIndex(data.NextIndex)} disabled={data.NextIndex >= data.TotalRecordCount || data.NextIndex <= startIndex}>Read more</Button></Stack>
    </>}
  </Paper>;
}

function LogActions({ file, selected, onSelect }: { file: ServerLogFile; selected: boolean; onSelect: (trigger: HTMLButtonElement) => void }) {
  return <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5 }}><Button size="small" startIcon={<SubjectRounded />} onClick={(event) => onSelect(event.currentTarget)} disabled={selected} aria-label={`View ${file.Name}`}>{selected ? 'Viewing' : 'View'}</Button><Button component="a" size="small" href={adminApi.serverLogDownloadURL(file.Name)} download startIcon={<DownloadRounded />} aria-label={`Download ${file.Name}`}>Download</Button></Stack>;
}

function LogsPanel() {
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
    {data && <Alert severity={data.Status.Healthy && !data.Status.Degraded && !data.Status.Closed ? 'success' : 'warning'} sx={{ mb: 3 }}><Typography variant="body2" sx={{ fontWeight: 600 }}>{data.Status.Closed ? 'Log storage is closed' : data.Status.Degraded || !data.Status.Healthy ? 'Log storage is degraded' : 'Log storage is healthy'}</Typography><Typography variant="body2">{data.Status.Closed ? 'New diagnostics are not being written.' : data.Status.Degraded || !data.Status.Healthy ? 'Some diagnostics may be missing. Check server storage and refresh the file list.' : 'Read recent server diagnostics and download a file for investigation.'}</Typography></Alert>}
    <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
      <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1, px: { xs: 2.5, sm: 3 }, py: 2.2 }}><Stack direction="row" sx={{ alignItems: 'center', gap: 1.2 }}><Typography variant="h4" component="h2">Log files</Typography>{data && <Chip size="small" label={data.TotalRecordCount.toLocaleString()} sx={{ bgcolor: 'background.default' }} />}</Stack><Button size="small" startIcon={<RefreshRounded />} onClick={refresh} disabled={loading}>Refresh files</Button></Stack>
      {loading && <LoadingRows label="Loading log files" />}
      {data && data.Items.length === 0 && <Stack spacing={1.5} sx={{ alignItems: 'center', p: { xs: 3, sm: 5 }, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}><SubjectRounded sx={{ fontSize: 40, color: 'primary.main' }} /><Typography variant="h3" component="h3">No log files</Typography><Typography color="text.secondary" sx={{ maxWidth: 480 }}>No retained diagnostic files are available. Refresh after the server writes a log entry.</Typography></Stack>}
      {data && data.Items.length > 0 && <>
        <Box component="ul" aria-label="Server log files" sx={{ display: { xs: 'block', lg: 'none' }, listStyle: 'none', p: 0, m: 0 }}>{data.Items.map((file) => <Box component="li" key={file.Name} sx={{ p: 2.5, borderTop: 1, borderColor: 'divider' }}><Typography variant="body2" sx={{ fontWeight: 650, overflowWrap: 'anywhere' }} className="mono">{file.Name}</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>Modified {localTime(file.DateModified)} · {byteSize(file.Size)}</Typography><Typography variant="caption" color="text.secondary">Created {localTime(file.DateCreated)}</Typography><Box sx={{ ml: -1, mt: 1.5 }}><LogActions file={file} selected={selected?.Name === file.Name} onSelect={(trigger) => selectFile(file, trigger)} /></Box></Box>)}</Box>
        <TableContainer sx={{ display: { xs: 'none', lg: 'block' } }}><Table aria-label="Server log files" sx={{ tableLayout: 'fixed' }}><TableHead><TableRow><TableCell sx={{ pl: 3, width: '29%' }}>File</TableCell><TableCell sx={{ width: '19%' }}>Created</TableCell><TableCell sx={{ width: '19%' }}>Modified</TableCell><TableCell sx={{ width: '10%' }}>Size</TableCell><TableCell sx={{ width: '23%' }}>Read</TableCell></TableRow></TableHead><TableBody>{data.Items.map((file) => <TableRow key={file.Name} selected={selected?.Name === file.Name}><TableCell component="th" scope="row" sx={{ pl: 3, overflowWrap: 'anywhere' }}><Typography variant="body2" className="mono" sx={{ fontWeight: 600 }}>{file.Name}</Typography></TableCell><TableCell>{localTime(file.DateCreated)}</TableCell><TableCell>{localTime(file.DateModified)}</TableCell><TableCell>{byteSize(file.Size)}</TableCell><TableCell sx={{ pr: 3 }}><LogActions file={file} selected={selected?.Name === file.Name} onSelect={(trigger) => selectFile(file, trigger)} /></TableCell></TableRow>)}</TableBody></Table></TableContainer>
      </>}
      {failed && <Typography variant="body2" color="text.secondary" sx={{ px: 3, py: 3 }}>Log storage could not be read. Refresh the file list to try again.</Typography>}
      {data && <TablePagination component="div" count={data.TotalRecordCount} page={query.page} rowsPerPage={query.pageSize} rowsPerPageOptions={pageSizes} labelRowsPerPage="Files per page:" onPageChange={(_event, page) => changePage(page)} onRowsPerPageChange={(event) => { const pageSize = Number(event.target.value); if (pageSizes.includes(pageSize)) changePage(0, pageSize); }} sx={paginationSx} />}
    </Paper>
    {data && <Typography variant="body2" color="text.secondary" sx={{ mt: 2.5, px: 0.5 }}>Server policy keeps logs for up to {data.Status.RetentionDays} days, with at most {data.Status.MaxFiles} files of {byteSize(data.Status.MaxFileBytes)} each. Files may be removed earlier to stay within these limits. Minimum free storage: {byteSize(data.Status.MinFreeBytes)}. Format: JSONL. This policy is read-only.</Typography>}
    {selected && <LogPreview key={selected.Name} file={selected} onClose={closePreview} onRefreshFiles={refresh} />}
  </Box>;
}

export function ObservabilityPage() {
  const [tab, setTab] = useState<'activity' | 'logs'>('activity');
  return <Box>
    <PageHeading title="Activity & logs" description="Review administrative activity and recent server diagnostics." />
    <Tabs value={tab} onChange={(_event, value: 'activity' | 'logs') => setTab(value)} aria-label="Activity and server logs" sx={{ mb: 3, borderBottom: 1, borderColor: 'divider' }}><Tab value="activity" label="Activity" id="observability-activity-tab" aria-controls="observability-activity-panel" /><Tab value="logs" label="Server logs" id="observability-logs-tab" aria-controls="observability-logs-panel" /></Tabs>
    {tab === 'activity' ? <Box role="tabpanel" id="observability-activity-panel" aria-labelledby="observability-activity-tab"><ActivityPanel /></Box> : <Box role="tabpanel" id="observability-logs-panel" aria-labelledby="observability-logs-tab"><LogsPanel /></Box>}
  </Box>;
}
