import { useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Box, Button, Chip, Dialog, DialogContent, DialogTitle, FormControlLabel, IconButton, MenuItem, Paper, Skeleton, Stack, Switch, Tab, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, Tabs, TextField, ToggleButton, ToggleButtonGroup, Tooltip, Typography } from './ui';
import { CloseRounded, ContentCopyRounded, DownloadRounded, HistoryRounded, RefreshRounded, SubjectRounded } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { ActivityAction, ActivityEntry, ActivityResourceKind, ActivityResponse, ActivitySeverity, ServerLogFile, ServerLogLinesResponse, ServerLogsResponse } from './api';
import { ErrorNotice, PageHeading } from './components';
import { pageURL } from './dashboardNavigation';
import { colors } from './theme';
import { adminLocale, useTranslation } from './i18n';
import './system-pages.css';

const actionFilters = [
  ['', 'All actions'], ['settings.updated', 'Settings updated'], ['user.updated', 'Account updated'], ['user.created', 'Account created'],
  ['scan.finished', 'Scan finished'], ['task.finished', 'Task finished'], ['session.login', 'Sign-in'], ['playback.started', 'Playback started'],
] as const satisfies readonly (readonly [ActivityAction | '', string])[];
const severityFilters = [['', 'All'], ['Error', 'Error'], ['Warn', 'Warning'], ['Info', 'Info']] as const satisfies readonly (readonly [ActivitySeverity | '', string])[];
const periodFilters = [['all', 'All retained activity'], ['today', 'Today'], ['week', 'Last 7 days']] as const;
const resourceLabels: Record<ActivityResourceKind, string> = {
  user: 'User', session: 'Session', application_key: 'API key', device: 'Device', library: 'Library', library_root: 'Library root', scan: 'Scan',
  item: 'Item', settings: 'Settings', task: 'Task', task_run: 'Task run', backup: 'Backup', restore: 'Restore',
};
const stateLabels: Record<NonNullable<ActivityEntry['State']>, string> = { completed: 'Completed', failed: 'Failed', cancelled: 'Cancelled', interrupted: 'Interrupted' };
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

const playbackSummary = {
  'playback.started': { Name: 'Playback started', Overview: 'An authorized player reported playback starting.' },
  'playback.stopped': { Name: 'Playback stopped', Overview: "An authorized player's playback stop was committed." },
};

type ActivityRecord = Omit<ActivityEntry, 'Resource'> & { Resource: { Kind: ActivityResourceKind; Id?: string } };

function activityRecord(entry: ActivityEntry): ActivityRecord {
  const { Action } = entry;
  if (Action !== 'playback.started' && Action !== 'playback.stopped') return entry;
  // Use the same projection for search, presentation, clipboard and export.
  // A media ID or playback fingerprint also makes the viewed item traceable.
  const { Id, Date, Severity, Source, Actor, Revision, Count, State, ChangedFields } = entry;
  return { Id, Date, Action, Severity, Source, Actor, Resource: { Kind: 'item' }, Revision, Count, State, ChangedFields, ...playbackSummary[Action] };
}

function activitySource(entry: ActivityRecord): string {
  if (entry.Action === 'session.login' || entry.Action === 'playback.started' || entry.Action === 'playback.stopped') return 'User';
  if (entry.Source === 'system' || entry.Action === 'scan.finished' || entry.Action === 'task.finished') return 'System';
  return 'Console';
}

function ActivityDetails({ entry, onClose }: { entry: ActivityRecord; onClose: () => void }) {
  const { t } = useTranslation();
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const json = JSON.stringify(entry, null, 2);
  async function copyRecord() {
    try { await navigator.clipboard.writeText(json); setCopyState('copied'); }
    catch { setCopyState('failed'); }
  }
  return <Dialog open maxWidth="sm" onClose={onClose} aria-labelledby="activity-detail-title" aria-describedby="activity-detail-message">
    <DialogTitle id="activity-detail-title">Activity details</DialogTitle>
    <DialogContent>
      <Typography id="activity-detail-message" variant="body1" sx={{ overflowWrap: 'anywhere' }}>{entry.Name}</Typography>
      <Stack direction="row" sx={{ mt: 1, mb: 2, alignItems: 'center', flexWrap: 'wrap', gap: 1 }}><Severity severity={entry.Severity} /><time className="system-mono" dateTime={entry.Date}>{localTime(entry.Date)}</time></Stack>
      {entry.Overview && <Typography variant="body2" color="text.secondary" sx={{ mb: 2, overflowWrap: 'anywhere' }}>{entry.Overview}</Typography>}
      <dl className="system-activity-facts">
        <div><dt>{t('Activity ID')}</dt><dd className="mono">{entry.Id}</dd></div>
        <div><dt>{t('Action')}</dt><dd className="mono">{entry.Action}</dd></div>
        <div><dt>{t('Actor')}</dt><dd translate={entry.Actor.Kind === 'user' && entry.Actor.Name ? 'no' : undefined}>{entry.Actor.Name || t(actorName(entry.Actor))}</dd></div>
        {entry.Actor.Id && <div><dt>{t('Actor ID')}</dt><dd className="mono">{entry.Actor.Id}</dd></div>}
        <div><dt>{t('Resource')}</dt><dd>{t(resourceLabels[entry.Resource.Kind])}{entry.Resource.Id && <> · <code>{entry.Resource.Id}</code></>}</dd></div>
        <div><dt>{t('Affected count')}</dt><dd className="mono">{entry.Count}</dd></div>
        {entry.Revision !== null && <div><dt>{t('Revision')}</dt><dd className="mono">{entry.PreviousRevision !== undefined && `${entry.PreviousRevision} → `}{entry.Revision}</dd></div>}
        {entry.ObservationFingerprint !== undefined && <div><dt>{t('Observation fingerprint')}</dt><dd className="mono">{entry.ObservationFingerprint}</dd></div>}
        {entry.State !== null && <div><dt>{t('Outcome')}</dt><dd>{t(stateLabels[entry.State])}</dd></div>}
      </dl>
      {entry.ChangedFields.length > 0 && <Box sx={{ mt: 2 }}><Typography variant="caption" color="text.secondary" component="div" sx={{ mb: 0.8 }}>Changed fields</Typography><Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.8 }}>{entry.ChangedFields.map((field) => <Chip key={field} translate="no" label={field} size="small" variant="outlined" />)}</Stack></Box>}
      <pre className="system-json" aria-label={t('Activity JSON')}>{json}</pre>
      <div className="system-json-action"><Button size="small" startIcon={<ContentCopyRounded />} onClick={() => void copyRecord()}>{copyState === 'copied' ? 'Copied' : 'Copy JSON'}</Button></div>
      {copyState === 'failed' && <Alert severity="error">Copy failed. Select and copy the JSON text above.</Alert>}
    </DialogContent>
  </Dialog>;
}

function ActivityList({ items }: { items: ActivityRecord[] }) {
  const { t, locale } = useTranslation();
  const [detail, setDetail] = useState<ActivityRecord>();
  return <>
    <div className="system-activity-scroll" role="region" aria-label={t('Activity records')} tabIndex={0}>
      <table className="system-activity-table" aria-label={t('Activity records')}>
        <thead><tr><th scope="col">{t('Time')}</th><th scope="col">{t('Severity')}</th><th scope="col">{t('Source')}</th><th scope="col">{t('Message')}</th><th scope="col">{t('Actor')}</th></tr></thead>
        <tbody>{items.map((entry) => {
          const actor = actorName(entry.Actor);
          const userName = entry.Actor.Kind === 'user' && entry.Actor.Name;
          const message = t(entry.Name);
          return <tr key={entry.Id} data-activity-id={entry.Id}>
            <td><time className="system-mono" dateTime={entry.Date} title={localTime(entry.Date)}>{new Date(entry.Date).toLocaleString(locale, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })}</time></td>
            <td><Severity severity={entry.Severity} /></td>
            <td>{t(activitySource(entry))}</td>
            <td><button type="button" className="system-activity-open" aria-label={t('Activity details: {message}', { message })} aria-haspopup="dialog" onClick={() => setDetail(entry)}>{message}</button></td>
            <td className="system-activity-actor" translate={userName ? 'no' : undefined}>{userName || t(actor)}</td>
          </tr>;
        })}</tbody>
      </table>
    </div>
    {detail && <ActivityDetails entry={detail} onClose={() => setDetail(undefined)} />}
  </>;
}

type ActivityPeriod = typeof periodFilters[number][0];
type SeverityFilter = typeof severityFilters[number][0];
type ActionFilter = typeof actionFilters[number][0];
const emptyFilters: { severity: SeverityFilter; action: ActionFilter; period: ActivityPeriod; minDate?: string } = { severity: '', action: '', period: 'all', minDate: undefined };

function activityMinDate(period: ActivityPeriod): string | undefined {
  if (period === 'all') return undefined;
  const date = new Date();
  if (period === 'today') date.setHours(0, 0, 0, 0);
  else date.setTime(date.getTime() - 7 * 86400000);
  return date.toISOString();
}

type ActivityCounts = Partial<Record<SeverityFilter, number | null>>;

function useActivityCounts(action: ActionFilter, minDate: string | undefined, revision: number): ActivityCounts {
  const key = JSON.stringify([action, minDate, revision]);
  const [counts, setCounts] = useState<{ key: string; totals: ActivityCounts }>();
  useEffect(() => {
    const controller = new AbortController();
    const signal = controller.signal;
    function store(severity: SeverityFilter, total: number | null) {
      if (!signal.aborted) setCounts((current) => ({ key, totals: { ...(current?.key === key ? current.totals : {}), [severity]: total } }));
    }
    for (const [severity] of severityFilters) {
      void adminApi.getActivity({ Action: action || undefined, MinDate: minDate, Severity: severity || undefined, Limit: 1 }, { signal })
        .then((result) => store(severity, result.TotalRecordCount))
        .catch((cause: unknown) => { if (!isAbortError(cause)) store(severity, null); });
    }
    return () => controller.abort();
  }, [action, minDate, key]);
  return counts?.key === key ? counts.totals : {};
}

function ActivityPanel({ onUsers }: { onUsers?: () => void }) {
  const { t, locale } = useTranslation();
  const [search, setSearch] = useState('');
  const [live, setLive] = useState(false);
  const [query, setQuery] = useState({ ...emptyFilters, page: 0, pageSize: 50 });
  const [revision, setRevision] = useState(0);
  const [countRevision, setCountRevision] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; queryKey: string; result: Omit<ActivityResponse, 'Items'> & { Items: ActivityRecord[] } }>();
  const [failure, setFailure] = useState<{ key: string; cause: unknown }>();
  const minDate = query.minDate;
  const queryKey = JSON.stringify(query);
  const requestKey = JSON.stringify([query, revision]);
  const data = loaded?.key === requestKey || (live && loaded?.queryKey === queryKey) ? loaded?.result : undefined;
  const failed = failure?.key === requestKey;
  const loading = loaded?.key !== requestKey && !failed;
  const filtered = Boolean(query.severity || query.action || minDate || search.trim());

  useEffect(() => {
    const controller = new AbortController();
    setFailure(undefined);
    void adminApi.getActivity({ Severity: query.severity || undefined, Action: query.action || undefined, MinDate: minDate, StartIndex: query.page * query.pageSize, Limit: query.pageSize }, { signal: controller.signal })
      .then((result) => {
        if (controller.signal.aborted) return;
        if (query.page > 0 && query.page * query.pageSize >= result.TotalRecordCount) {
          setQuery((current) => ({ ...current, page: Math.max(0, Math.ceil(result.TotalRecordCount / query.pageSize) - 1) }));
        } else setLoaded({ key: requestKey, queryKey, result: { ...result, Items: result.Items.map(activityRecord) } });
      })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setFailure({ key: requestKey, cause }); });
    return () => controller.abort();
  }, [query, queryKey, requestKey, minDate]);

  // Start loading the page before asking for the supplementary counts.
  const counts = useActivityCounts(query.action, minDate, countRevision);
  useEffect(() => {
    if (!live || loading || failed || query.page !== 0) return;
    const updateIfVisible = () => {
      if (document.visibilityState === 'hidden') return;
      // A new local day needs a new midnight bound. Otherwise keep the window
      // steady until a manual refresh rather than recounting every live poll.
      if (query.period === 'today' && minDate !== activityMinDate('today')) {
        setQuery((current) => ({ ...current, minDate: activityMinDate('today') }));
      }
      setRevision((value) => value + 1);
    };
    const timer = window.setTimeout(updateIfVisible, 5000);
    document.addEventListener('visibilitychange', updateIfVisible);
    return () => { window.clearTimeout(timer); document.removeEventListener('visibilitychange', updateIfVisible); };
  }, [live, loading, failed, query.page, query.period, minDate, revision]);

  const searchTerm = search.trim().toLocaleLowerCase(locale);
  const visibleItems = useMemo(() => (data?.Items ?? []).filter((entry) => !searchTerm || [
    entry.Name, t(entry.Name), entry.Overview, t(entry.Overview), entry.Action, actorName(entry.Actor), t(activitySource(entry)), entry.Resource.Id,
  ].join(' ').toLocaleLowerCase(locale).includes(searchTerm)), [data, locale, searchTerm, t]);
  function exportPage() {
    const url = URL.createObjectURL(new Blob([visibleItems.map((entry) => JSON.stringify(entry)).join('\n')], { type: 'application/x-ndjson' }));
    const link = document.createElement('a'); link.href = url; link.download = 'goby-activity-page.jsonl'; link.click(); window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  function refresh() { setRevision((current) => current + 1); setCountRevision((current) => current + 1); }
  function filter(patch: Partial<typeof emptyFilters>) {
    setQuery((current) => ({ ...current, page: 0, ...patch }));
    setRevision((current) => current + 1);
  }
  function changePage(page: number, pageSize = query.pageSize) {
    if (page !== 0) setLive(false);
    setQuery((current) => ({ ...current, page, pageSize }));
    setRevision((current) => current + 1);
  }

  const [beforeUsers, afterUsers] = t("To review an account's sign-ins, open its Login devices tab in {users}.").split('{users}');
  return <Box aria-busy={loading}>
    {failed && <Box sx={{ mb: 2 }}><ErrorNotice error={failure?.cause} retry={refresh} /></Box>}
    <Paper component="section" aria-label="Server activity" variant="outlined" sx={panelSx}>
      <div className="system-activity-toolbar">
        <ToggleButtonGroup className="system-activity-severity" exclusive value={query.severity} aria-label="Severity" onChange={(_, severity: SeverityFilter | null) => { if (severity !== null) filter({ severity }); }}>
          {severityFilters.map(([severity, label]) => {
            // Live updates already return the selected segment's exact total.
            const total = severity === query.severity && data ? data.TotalRecordCount : counts[severity];
            return <ToggleButton key={severity} value={severity} aria-label={label} aria-description={total === null ? t('Activity count unavailable') : total === undefined ? t('Loading activity count') : t('Activity count: {count}', { count: total.toLocaleString(locale) })}>
              {t(label)} <span className="system-mono" aria-hidden="true">{total?.toLocaleString(locale) ?? '—'}</span>
            </ToggleButton>;
          })}
        </ToggleButtonGroup>
        <TextField className="system-activity-select" select size="small" value={query.action} slotProps={{ htmlInput: { 'aria-label': 'Action' } }} onChange={(event) => filter({ action: event.target.value as ActionFilter })}>
          {actionFilters.map(([action, label]) => <MenuItem key={action} value={action}>{label}</MenuItem>)}
        </TextField>
        <TextField className="system-activity-select" select size="small" value={query.period} slotProps={{ htmlInput: { 'aria-label': 'Time range' } }} onChange={(event) => { const period = event.target.value as ActivityPeriod; filter({ period, minDate: activityMinDate(period) }); }}>
          {periodFilters.map(([period, label]) => <MenuItem key={period} value={period}>{label}</MenuItem>)}
        </TextField>
        <TextField className="system-activity-search" type="search" size="small" placeholder="Find on this page" slotProps={{ htmlInput: { 'aria-label': 'Find on this page' } }} value={search} onChange={(event) => setSearch(event.target.value)} />
        <div className="system-activity-actions">
          <FormControlLabel className="system-activity-live" label={live ? failed ? 'Live updates paused' : 'Live updates on' : 'Live updates off'} control={<Switch checked={live} disabled={query.page !== 0} slotProps={{ input: { 'aria-label': 'Live updates', 'aria-description': t('Live updates refresh the first page every 5 seconds and pause after an error.') } }} onChange={(_, checked) => setLive(checked)} />} />
          <Button size="small" startIcon={<DownloadRounded />} disabled={!visibleItems.length} onClick={exportPage}>Export current page</Button>
          <Tooltip title="Refresh activity"><span><IconButton aria-label="Refresh activity" onClick={refresh} disabled={loading}><RefreshRounded fontSize="small" /></IconButton></span></Tooltip>
        </div>
      </div>
      {loading && !data && <LoadingRows label="Loading activity" />}
      {data && visibleItems.length === 0 && <Stack spacing={1} sx={{ alignItems: 'center', p: { xs: 3, sm: 5 }, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}>
        <HistoryRounded sx={{ fontSize: 32, color: 'primary.main' }} />
        <Typography variant="h4" component="h3">{data.Items.length ? 'No matching activity on this page.' : filtered ? 'No matching activity' : 'No recorded activity'}</Typography>
        <Typography variant="body2" color="text.secondary">{filtered ? 'Try another severity, action, or date range, or clear your filters.' : 'Administrative changes, sign-ins, playback reports, and task outcomes appear here.'}</Typography>
        {filtered && <Button onClick={() => { filter(emptyFilters); setSearch(''); }}>Clear filters</Button>}
      </Stack>}
      {data && visibleItems.length > 0 && <ActivityList key={queryKey} items={visibleItems} />}
      <p className="system-panel-footnote">
        {data ? t(data.RetentionDays === 1 ? 'Activity is retained for {days} day and includes sign-ins and playback records.' : 'Activity is retained for {days} days and includes sign-ins and playback records.', { days: data.RetentionDays.toLocaleString(locale) }) : t('Activity includes sign-ins and playback records.')}{' '}
        {t('On servers with many users, sign-ins and playback can make up most records. Filter by severity or action to find administrative changes. Search only checks the current page.')}{' '}
        {beforeUsers}<a href={pageURL('users')} onClick={(event) => {
          if (!onUsers || event.button !== 0 || event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) return;
          event.preventDefault(); onUsers();
        }}>{t('Users')}</a>{afterUsers}
      </p>
      {data && <TablePagination component="div" count={data.TotalRecordCount} page={query.page} rowsPerPage={query.pageSize} rowsPerPageOptions={pageSizes} labelRowsPerPage="Activities per page:" disabled={loading} onPageChange={(_event, page) => changePage(page)} onRowsPerPageChange={(event) => { const size = Number(event.target.value); if (pageSizes.includes(size)) changePage(0, size); }} labelDisplayedRows={({ from, to, count }) => `${from.toLocaleString(locale)}–${to.toLocaleString(locale)} / ${count.toLocaleString(locale)}`} sx={paginationSx} />}
    </Paper>
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

export function ObservabilityPage({ onUsers }: { onUsers?: () => void } = {}) {
  const [tab, setTab] = useState<'activity' | 'logs'>('activity');
  return <Box className="system-page system-observability">
    <PageHeading title="Activity & logs" description="Review administrative changes, sign-ins, playback, scan and task results, and diagnostic logs. On servers with many users, start by filtering by severity or action." />
    <Tabs value={tab} onChange={(_event, value: 'activity' | 'logs') => setTab(value)} aria-label="Activity and server logs" className="system-log-tabs" sx={{ mb: 2.25 }}><Tab value="activity" label="Activity" id="observability-activity-tab" aria-controls="observability-activity-panel" /><Tab value="logs" label="Server logs" id="observability-logs-tab" aria-controls="observability-logs-panel" /></Tabs>
    {tab === 'activity' ? <Box role="tabpanel" id="observability-activity-panel" aria-labelledby="observability-activity-tab"><ActivityPanel onUsers={onUsers} /></Box> : <Box role="tabpanel" id="observability-logs-panel" aria-labelledby="observability-logs-tab"><LogsPanel /></Box>}
  </Box>;
}
