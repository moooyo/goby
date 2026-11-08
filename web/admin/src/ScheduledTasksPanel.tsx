import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from './i18n';
import './system-pages.css';
import { Alert, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, LinearProgress, Paper, Skeleton, Stack, Tooltip, Typography } from './ui';
import { PlayArrowRounded } from './icons';
import { RefreshRounded } from './icons';
import { InfoOutlined, ScheduleOutlined } from './icons';
import { StopRounded } from './icons';
import { SyncRounded } from './icons';
import { adminApi, isAbortError } from './api';
import type { TaskDefinition, TaskRun } from './api';
import { ErrorNotice } from './components';
import { ScheduleEditor } from './ScheduleEditor';
import { scheduleDate } from './taskSchedule';
import { taskScheduleSummary, taskTriggerLabel } from './taskTriggerPresentation';
import { isActiveTaskRun, taskBackupResultLabel, TaskRunDialog } from './TaskRunDialog';
import type { TaskPanelStatus } from './TasksPage';
import { useTaskResource } from './useTaskResource';
import { useTaskHistory } from './useTaskHistory';
import { TaskHistoryNotice } from './TaskHistoryNotice';
import { useTaskInitiators } from './useTaskInitiators';
import { TaskInitiatorNotice, TaskRunOrigin } from './TaskRunOrigin';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const alwaysPoll = () => true;
const memoryReceipts = new Map<string, string>();
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

function receiptKey(userId: string, taskId: string) { return `goby.task-start.${encodeURIComponent(userId)}.${encodeURIComponent(taskId)}`; }

function readReceipt(userId: string, taskId: string): string | undefined {
  const key = receiptKey(userId, taskId);
  const memory = memoryReceipts.get(key);
  if (memory) return memory;
  try { const value = sessionStorage.getItem(key); return value && uuidPattern.test(value) ? value : undefined; } catch { return undefined; }
}

function keepReceipt(userId: string, taskId: string, requestId: string) {
  const key = receiptKey(userId, taskId);
  memoryReceipts.set(key, requestId);
  try { sessionStorage.setItem(key, requestId); } catch { /* The in-memory receipt still survives in-app navigation. */ }
}

function forgetReceipt(userId: string, taskId: string) {
  const key = receiptKey(userId, taskId);
  memoryReceipts.delete(key);
  try { sessionStorage.removeItem(key); } catch { /* A blocked store must not hide a confirmed run. */ }
}

function requestUUID(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 15) | 64;
  bytes[8] = (bytes[8] & 63) | 128;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function CurrentProgress({ run, scan = false }: { run: TaskRun; scan?: boolean }) {
  const { t, locale, countLabel } = useTranslation();
  const progress = !scan && run.TotalChildren > 0 ? Math.min(100, run.TerminalChildren / run.TotalChildren * 100) : undefined;
  const progressText = t(countLabel(run.TotalChildren, '{done} of {total} work item finished', '{done} of {total} work items finished'), {
    done: run.TerminalChildren.toLocaleString(locale), total: run.TotalChildren.toLocaleString(locale),
  });
  return <div className="system-current-progress">
    <LinearProgress variant={progress === undefined ? 'indeterminate' : 'determinate'} value={progress} aria-label="Task work progress" aria-valuetext={scan ? t('Scanned {scanned} · added {added} · updated {updated}', { scanned: run.Scanned, added: run.Added, updated: run.Updated }) : progress === undefined ? t('Preparing work items') : progressText} />
    <dl className="system-task-counts">
      {(scan ? [['Scanned', run.Scanned], ['Added', run.Added], ['Updated', run.Updated]] : [['Processed', run.Scanned], ['Finished', run.TerminalChildren], ['Work items', run.TotalChildren]]).map(([label, value]) => <div key={label}><dt>{t(String(label))}</dt><dd>{Number(value).toLocaleString(locale)}</dd></div>)}
    </dl>
    <p className="system-task-hint">{t(scan ? 'Scans show actual counts, not an estimated percentage. Completed work is kept when stopped.' : run.State === 'stopping' ? 'Stopping remaining work' : run.TotalChildren > 0 ? 'Progress is based on completed work items.' : 'Preparing work items')}</p>
  </div>;
}

function runDuration(run: TaskRun, locale: string): string {
  if (run.State === 'pending') return '—';
  if (!run.FinishedAt && isActiveTaskRun(run)) return 'In progress';
  if (!run.StartedAt || !run.FinishedAt) return '—';
  const seconds = Math.max(0, Math.floor((Date.parse(run.FinishedAt) - Date.parse(run.StartedAt)) / 1000));
  return new Intl.NumberFormat(locale, { style: 'unit', unit: seconds >= 60 ? 'minute' : 'second', unitDisplay: 'short', maximumFractionDigits: 1 }).format(seconds >= 60 ? seconds / 60 : seconds);
}

type TaskKind = 'all' | 'scan' | 'analysis' | 'backup';
const taskKindChoices: { value: TaskKind; label: string }[] = [
  { value: 'all', label: 'All' }, { value: 'scan', label: 'Scans' }, { value: 'analysis', label: 'Analysis' }, { value: 'backup', label: 'Backups' },
];

function taskKind(task: TaskDefinition): Exclude<TaskKind, 'all'> | 'other' {
  const identity = (task.Key + ' ' + task.Category).toLowerCase();
  if (identity.includes('backup')) return 'backup';
  if (identity.includes('scan') || task.Key === 'library.refresh_media') return 'scan';
  if (/analysis|analy[sz]e|preview|waveform|timeline/.test(identity)) return 'analysis';
  return 'other';
}

function TaskStateBadge({ run }: { run: TaskRun }) {
  const { t } = useTranslation();
  const partial = run.State === 'completed' && run.FailedChildren + run.UnavailableChildren + run.InterruptedChildren > 0;
  const label = partial ? 'Partial failure' : run.State === 'pending' ? 'Queued' : run.State === 'cancelled' ? 'Stopped' : run.State[0].toUpperCase() + run.State.slice(1);
  return <span className="system-run-state" data-state={partial ? 'partial' : run.State}><span aria-hidden="true" />{t(label)}</span>;
}

function TaskResult({ task, run }: { task: TaskDefinition; run: TaskRun }) {
  const { t, locale } = useTranslation();
  const count = (value: number) => value.toLocaleString(locale);
  const result = task.Key === 'backup.create' ? [taskBackupResultLabel(run.BackupResult, t, locale), run.ErrorMessage].filter(Boolean).join(' · ') : run.ErrorMessage || (taskKind(task) === 'scan'
    ? t('Scanned {scanned} · added {added} · updated {updated}', { scanned: count(run.Scanned), added: count(run.Added), updated: count(run.Updated) })
    : run.TotalChildren > 0
      ? [t('{count} finished', { count: count(run.CompletedChildren) }), ...(run.FailedChildren ? [t('{count} failed', { count: count(run.FailedChildren) })] : []), ...(run.UnavailableChildren ? [t('{count} unavailable', { count: count(run.UnavailableChildren) })] : []), ...(run.CancelledChildren ? [t('{count} stopped', { count: count(run.CancelledChildren) })] : []), ...(run.InterruptedChildren ? [t('{count} interrupted', { count: count(run.InterruptedChildren) })] : [])].join(' · ')
      : t(isActiveTaskRun(run) ? 'Preparing work items' : 'No work items recorded'));
  return <span role="cell" className="system-mono system-run-result" data-error={Boolean(run.ErrorMessage) || undefined} title={result}>{result}</span>;
}

function nextRunTime(task: TaskDefinition, locale: string): string | undefined {
  if (!task.NextRunAt) return undefined;
  try { return new Intl.DateTimeFormat(locale, { timeZone: task.ScheduleTimezone, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(task.NextRunAt)); }
  catch { return scheduleDate(task.NextRunAt, task.ScheduleTimezone); }
}

export function ScheduledTasksPanel({ currentUserId, onNavigationGuardChange, onStatusChange, onTaskQueryCleared }: { currentUserId: string; onNavigationGuardChange: UserNavigationGuardChange; onStatusChange?: (status: TaskPanelStatus) => void; onTaskQueryCleared?: () => void }) {
  const { t, locale, formatCount } = useTranslation();
  const [runFilter, setRunFilter] = useState<TaskKind>('all');
  const [visibleRunLimit, setVisibleRunLimit] = useState(100);
  const [starting, setStarting] = useState<string>();
  const [stopping, setStopping] = useState<string>();
  const [stopCandidate, setStopCandidate] = useState<{ task: TaskDefinition; run: TaskRun }>();
  const [stopError, setStopError] = useState<unknown>();
  const [startErrors, setStartErrors] = useState<Record<string, unknown>>({});
  const [pending, setPending] = useState<Map<string, string>>(new Map());
  const [editing, setEditing] = useState<string | undefined>(() => new URLSearchParams(window.location.search).get('task') || undefined);
  const [viewing, setViewing] = useState<{ task: TaskDefinition; run?: TaskRun; notice?: string; confirmedRequestId?: string }>();
  const mutation = useRef<AbortController | undefined>(undefined);
  const load = useCallback((signal: AbortSignal) => adminApi.getTasks({ signal }), []);
  const busy = Boolean(starting || stopping);
  const resource = useTaskResource({ key: currentUserId, load, poll: alwaysPoll, enabled: !busy });
  const stopTask = stopCandidate ? resource.data?.Items.find((task) => task.Id === stopCandidate.task.Id) : undefined;
  const stopRun = stopCandidate ? [stopTask?.CurrentRun, stopTask?.LastRun].find((run) => run?.Id === stopCandidate.run.Id) ?? stopCandidate.run : undefined;
  const statusMessage = resource.error != null ? 'Automatic updates stopped after a request error.' : resource.paused ? 'Updates pause while this tab is hidden.' : busy ? 'Updates resume after this request.' : 'Task status updates every 5 seconds.';
  useUserDraftNavigation(false, busy, onNavigationGuardChange);
  useEffect(() => () => mutation.current?.abort(), []);
  useEffect(() => {
    if (!resource.data) return;
    const receipts = new Map<string, string>();
    for (const task of resource.data.Items) { const value = readReceipt(currentUserId, task.Id); if (value) receipts.set(task.Id, value); }
    setPending(receipts);
  }, [resource.data, currentUserId]);
  useEffect(() => {
    if (!viewing?.run || !viewing.confirmedRequestId || readReceipt(currentUserId, viewing.task.Id) !== viewing.confirmedRequestId) return;
    // Clear the idempotency receipt only after the acknowledged run is rendered.
    forgetReceipt(currentUserId, viewing.task.Id);
    setPending((current) => { const next = new Map(current); next.delete(viewing.task.Id); return next; });
  }, [viewing, currentUserId]);

  const start = useCallback(async (task: TaskDefinition) => {
    if (mutation.current) return;
    const previousRequestId = readReceipt(currentUserId, task.Id);
    const requestId = previousRequestId ?? requestUUID();
    keepReceipt(currentUserId, task.Id, requestId);
    setPending((current) => new Map(current).set(task.Id, requestId));
    setStarting(task.Id);
    setStartErrors((current) => { const next = { ...current }; delete next[task.Id]; return next; });
    const controller = new AbortController();
    mutation.current = controller;
    try {
      const result = await adminApi.startTask(task.Id, requestId, { signal: controller.signal });
      if (!controller.signal.aborted) {
        setViewing({ task, run: result.Run, confirmedRequestId: requestId, notice: previousRequestId ? 'The start request is confirmed. Showing its recorded run.' : result.Admitted ? 'Run requested.' : isActiveTaskRun(result.Run) ? 'A run is already in progress. Showing the existing run.' : 'The earlier request has already been handled. Showing its recorded run.' });
        resource.reload();
      }
    } catch (error) {
      if (!controller.signal.aborted && !isAbortError(error)) setStartErrors((current) => ({ ...current, [task.Id]: error }));
    } finally {
      if (mutation.current === controller) mutation.current = undefined;
      if (!controller.signal.aborted) setStarting(undefined);
    }
  }, [currentUserId, resource.reload]);

  async function stop() {
    if (mutation.current || !stopCandidate || !stopRun || !isActiveTaskRun(stopRun) || stopRun.State === 'stopping') return;
    const controller = new AbortController();
    mutation.current = controller;
    setStopping(stopCandidate.task.Id);
    setStopError(undefined);
    try {
      const result = await adminApi.cancelTaskRun(stopRun.Id, { signal: controller.signal });
      if (!controller.signal.aborted) {
        setViewing({ task: stopCandidate.task, run: result.Run, notice: 'The server acknowledged the stop request. Review the run to confirm its final state.' });
        setStopCandidate(undefined);
        resource.reload();
      }
    } catch (error) {
      if (!controller.signal.aborted && !isAbortError(error)) setStopError(error);
    } finally {
      if (mutation.current === controller) mutation.current = undefined;
      if (!controller.signal.aborted) setStopping(undefined);
    }
  }

  const tasks = resource.data?.Items ?? [];
  const history = useTaskHistory(currentUserId, resource.data?.Items);
  const initiators = useTaskInitiators(currentUserId, history.items.map(({ run }) => run), Boolean(resource.data) && !history.loading);
  const refreshTasks = useCallback(() => { resource.reload(); history.reload(); initiators.reload(); setVisibleRunLimit(100); }, [resource.reload, history.reload, initiators.reload]);
  const active = tasks.filter((task) => task.CurrentRun && isActiveTaskRun(task.CurrentRun));
  const executing = active.filter((task) => task.CurrentRun?.State !== 'pending');
  const queued = active.length - executing.length;
  const scanTask = tasks.find((task) => task.Key === 'library.scan' || task.Key === 'scan');
  const summary = resource.data ? t('{running} running · {queued} queued · {tasks}', { running: executing.length, queued, tasks: formatCount(tasks.length, 'task', 'tasks') }) : undefined;
  useEffect(() => {
    onStatusChange?.({
      message: statusMessage, active: !resource.paused && !busy && resource.error == null,
      refresh: refreshTasks, refreshing: resource.loading || history.loading || initiators.loading || busy, refreshLabel: 'Refresh tasks', summary,
      primaryAction: scanTask ? {
        label: 'Scan all libraries', onClick: () => { void start(scanTask); }, busy: starting === scanTask.Id,
        disabled: busy || !scanTask.Enabled || pending.has(scanTask.Id) || Boolean(scanTask.CurrentRun && isActiveTaskRun(scanTask.CurrentRun)),
      } : undefined,
    });
  }, [onStatusChange, statusMessage, resource.paused, resource.error, resource.loading, history.loading, initiators.loading, refreshTasks, busy, summary, scanTask, start, starting, pending]);
  const latest = history.items.filter(({ task }) => runFilter === 'all' || taskKind(task) === runFilter);
  const visibleRuns = latest.slice(0, visibleRunLimit);
  const zones = [...new Set(tasks.map((task) => task.ScheduleTimezone))];
  function requestStop(task: TaskDefinition, run: TaskRun) { setStopError(undefined); setStopCandidate({ task, run }); }
  function editSchedule(taskId: string | undefined) {
    setEditing(taskId);
    const url = new URL(window.location.href);
    if (taskId) url.searchParams.set('task', taskId);
    else url.searchParams.delete('task');
    window.history.replaceState(window.history.state, '', url);
    if (!taskId) onTaskQueryCleared?.();
  }

  return <Stack spacing={2.25}>
    {!onStatusChange && <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}><Typography variant="caption" color="text.secondary">{statusMessage}</Typography><Button size="small" startIcon={<RefreshRounded />} onClick={refreshTasks} disabled={resource.loading || history.loading || initiators.loading || busy}>Refresh tasks</Button></Stack>}
    {resource.error != null && <ErrorNotice error={resource.error} retry={refreshTasks} />}
    {!resource.data && resource.loading && <Paper variant="outlined" role="status" aria-label="Loading available tasks" sx={{ px: 2 }}>{[0, 1, 2, 3].map((row) => <Skeleton key={row} height={68} />)}</Paper>}
    {resource.data?.Items.length === 0 && <Paper variant="outlined" className="system-empty"><SyncRounded /><Typography component="h2">No available tasks</Typography><Typography variant="body2" color="text.secondary">No task definitions are available on this server. Scan history is available from More task views.</Typography></Paper>}
    {tasks.length > 0 && <div className="system-task-layout">
      <div className="system-task-main">
        {executing.map((task) => {
          const run = task.CurrentRun!;
          return <section className="system-panel system-active-run" key={run.Id} aria-label={t(task.Name)}>
            <div className="system-active-title"><span className="system-status-dot is-active" aria-hidden="true" /><h2 aria-label={t(task.Name)}><button type="button" className="system-active-open" aria-label={t('View current run') + ' · ' + t(task.Name)} onClick={() => setViewing({ task, run })}>{t(task.Name)}</button></h2><span className="system-active-meta"><TaskRunOrigin source={run.Source} actor={initiators.byRun.get(run.Id)} loading={initiators.loading} /><span aria-hidden="true"> · </span><time className="system-mono" dateTime={run.StartedAt ?? run.CreatedAt}>{t('Started at {time}', { time: new Date(run.StartedAt ?? run.CreatedAt).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' }) })}</time></span><Button size="small" variant="outlined" startIcon={<StopRounded />} disabled={busy || run.State === 'stopping'} onClick={() => requestStop(task, run)}>{run.State === 'stopping' ? 'Stopping run...' : 'Stop run'}</Button></div>
            <CurrentProgress run={run} scan={taskKind(task) === 'scan'} />
          </section>;
        })}
        {executing.length === 0 && <section className="system-panel system-active-run system-no-active"><span className="system-status-dot" aria-hidden="true" /><div><h2>{t('No running tasks')}</h2><p>{t(queued ? 'Queued work will appear here when it starts.' : 'Start a task from its schedule, or wait for the next scheduled run.')}</p></div></section>}
        <section className="system-panel" aria-labelledby="task-run-history-title">
          <div className="system-panel-title system-task-history-heading"><h2 id="task-run-history-title">{t('Run history')}</h2><span className="system-muted system-task-history-period" title={t('Runs created in the last 24 hours. Active runs remain visible.')} >{t('Last 24 hours')}</span><div className="system-mini-segments" role="group" aria-label={t('Task type filter')}>{taskKindChoices.map((filter) => <button key={filter.value} type="button" aria-pressed={runFilter === filter.value} onClick={() => { setRunFilter(filter.value); setVisibleRunLimit(100); }}>{t(filter.label)}</button>)}</div></div>
          <TaskHistoryNotice tasks={tasks} issues={history.issues} loading={history.loading} hasOlderActiveRuns={history.hasOlderActiveRuns} disabled={busy} onRefresh={history.reload} />
          <TaskInitiatorNotice issues={initiators.issues} loading={initiators.loading} disabled={busy} onRefresh={initiators.reload} />
          <div className="system-run-table" role="table" aria-label={t('Recent task runs')} aria-busy={history.loading} data-initiators-loading={initiators.loading}>
            <div className="system-run-row system-run-head" role="row"><span role="columnheader">{t('Task')}</span><span role="columnheader">{t('Status')}</span><span role="columnheader">{t('Started')}</span><span role="columnheader">{t('Duration')}</span><span role="columnheader">{t('Result')}</span></div>
            {visibleRuns.map(({ task, run }) => <div role="row" className="system-run-row" data-run-id={run.Id} data-active={isActiveTaskRun(run) || undefined} key={run.Id}><span role="cell" className="system-run-name"><button type="button" className="system-run-open" aria-label={t('View runs') + ' · ' + t(task.Name)} onClick={() => setViewing({ task, run })}>{t(task.Name)}</button><small>{t(task.Category)}<span aria-hidden="true"> · </span><TaskRunOrigin source={run.Source} actor={initiators.byRun.get(run.Id)} loading={initiators.loading} /></small></span><span role="cell"><TaskStateBadge run={run} /></span><span role="cell">{run.StartedAt ? <time className="system-mono" dateTime={run.StartedAt} title={new Date(run.StartedAt).toLocaleString(locale)}>{new Date(run.StartedAt).toLocaleString(locale, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })}</time> : '—'}</span><span role="cell" className="system-mono">{t(runDuration(run, locale))}</span><TaskResult task={task} run={run} /></div>)}
          </div>
          {latest.length === 0 && !history.loading && <div className="system-table-empty">{t(history.issues.length ? 'No runs could be loaded for this view.' : runFilter === 'all' ? 'No task runs in the last 24 hours.' : 'No matching task runs.')}</div>}
          {latest.length > 100 && <div className="system-task-history-more"><span>{t('Showing {count} of {total} loaded runs', { count: visibleRuns.length.toLocaleString(locale), total: latest.length.toLocaleString(locale) })}</span>{visibleRuns.length < latest.length && <Button size="small" onClick={() => setVisibleRunLimit((value) => value + 100)}>Show more runs</Button>}</div>}
        </section>
      </div>
      <section className="system-panel system-schedule-panel" aria-labelledby="scheduled-tasks-title">
        <div className="system-panel-title"><h2 id="scheduled-tasks-title">{t('Scheduled tasks')}</h2><span className="system-mono system-muted">{tasks.length.toLocaleString(locale)}</span></div>
        <ul className="system-schedules" aria-label={t('Available server tasks')}>{tasks.map((task) => {
          const current = task.CurrentRun;
          const recovering = pending.has(task.Id);
          const running = Boolean(current && isActiveTaskRun(current));
          const primaryLabel = starting === task.Id ? 'Requesting run...' : recovering ? 'Check start result' : running ? current?.State === 'stopping' ? 'Stopping run...' : 'Stop run' : 'Start task';
          const scheduleSummary = taskScheduleSummary(task, t);
          const nextRun = nextRunTime(task, locale);
          const details = [scheduleSummary, nextRun ? t('Next {time}', { time: nextRun }) : ''].filter(Boolean).join(' · ');
          return <li key={task.Id} className="system-schedule-row" data-active={running || undefined}>
            <div className="system-schedule-description"><h3 aria-label={t(task.Name)}>{running && <span className="system-status-dot is-active" aria-hidden="true" />}<button type="button" className="system-schedule-open" title={t(task.Description)} aria-label={t('View runs') + ' · ' + t(task.Name)} disabled={busy} onClick={() => setViewing({ task })}>{t(task.Name)}</button></h3><p title={details}>{running && current ? t(current.State === 'pending' ? 'Queued' : current.State === 'stopping' ? 'Stopping run...' : 'Running') : scheduleSummary}{!running && nextRun && <> · <time className="system-mono" dateTime={task.NextRunAt!} title={scheduleDate(task.NextRunAt!, task.ScheduleTimezone)}>{t('Next {time}', { time: nextRun })}</time></>}</p></div>
            <div className="system-schedule-actions"><Tooltip title={primaryLabel}><span><IconButton aria-label={primaryLabel} disabled={busy || (!recovering && (running ? current?.State === 'stopping' : !task.Enabled))} onClick={() => { if (running && current && !recovering) requestStop(task, current); else void start(task); }}>{starting === task.Id || stopping === task.Id ? <CircularProgress size={16} color="inherit" /> : recovering ? <RefreshRounded /> : running ? <StopRounded /> : <PlayArrowRounded />}</IconButton></span></Tooltip><Tooltip title="Edit schedule"><span><IconButton aria-label="Edit schedule" onClick={() => editSchedule(task.Id)} disabled={busy}><ScheduleOutlined /></IconButton></span></Tooltip></div>
            {(!task.Enabled || task.IsHidden) && <div className="system-schedule-notice">{!task.Enabled && <Chip size="small" label="Disabled" />}{task.IsHidden && <Chip size="small" label="Hidden" />}</div>}
            {(startErrors[task.Id] != null || (recovering && starting !== task.Id) || task.Triggers.some((trigger) => trigger.CalculationError)) && <Stack spacing={1} className="system-schedule-notice">
              {startErrors[task.Id] != null && <ErrorNotice error={startErrors[task.Id]} />}
              {recovering && starting !== task.Id && <Alert severity="warning">A start request has not been confirmed. Check its result before starting another run. The same request will be reused.</Alert>}
              {task.Triggers.filter((trigger) => trigger.CalculationError).map((trigger) => <Alert key={trigger.Id} severity="warning">This trigger is paused because its next occurrence could not be calculated. Edit and save the schedule to adjust it.<Typography variant="caption" component="div">{taskTriggerLabel(trigger, t)}: {trigger.CalculationError}</Typography></Alert>)}
            </Stack>}
          </li>;
        })}</ul>
        <p className="system-panel-footnote system-schedule-footnote"><InfoOutlined aria-hidden="true" /><span>{t('Each task can have multiple rules and runs when any rule matches.')} {t('Schedules use the server time zone.')} <span className="system-mono">{zones.join(' · ')}</span></span></p>
      </section>
    </div>}
    {stopCandidate && stopRun && <Dialog open onClose={busy ? undefined : () => setStopCandidate(undefined)} fullWidth maxWidth="sm" aria-labelledby="stop-task-title"><DialogTitle id="stop-task-title">Stop this run?</DialogTitle><DialogContent><Stack spacing={2}><Typography variant="body2" color="text.secondary">Stops the remaining work for {stopCandidate.task.Name}. Changes from completed work remain in the catalog.</Typography>{!isActiveTaskRun(stopRun) && <Alert severity="info">This run has already ended.</Alert>}{stopError != null && <><ErrorNotice error={stopError} /><Typography variant="body2" color="text.secondary">The stop request could not be confirmed. Retry for this same run, or close and refresh its status.</Typography></>}</Stack></DialogContent><DialogActions><Button disabled={busy} onClick={() => setStopCandidate(undefined)}>{isActiveTaskRun(stopRun) ? 'Keep run' : 'Close'}</Button><Button variant="contained" color="error" disabled={busy || !isActiveTaskRun(stopRun) || stopRun.State === 'stopping'} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <StopRounded />} onClick={() => void stop()}>{busy ? 'Requesting stop...' : 'Stop run'}</Button></DialogActions></Dialog>}
    {editing && <ScheduleEditor key={editing} taskId={editing} onClose={() => editSchedule(undefined)} onSaved={resource.reload} onNavigationGuardChange={onNavigationGuardChange} />}
    {viewing && <TaskRunDialog key={`${viewing.task.Id}:${viewing.run?.Id ?? 'history'}`} task={viewing.task} initialRun={viewing.run} notice={viewing.notice} onClose={() => { setViewing(undefined); resource.reload(); }} onChanged={resource.reload} onNavigationGuardChange={onNavigationGuardChange} />}
  </Stack>;
}
