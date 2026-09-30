import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, LinearProgress, Paper, Skeleton, Stack, Tooltip, Typography } from '@mui/material';
import PlayArrowRounded from '@mui/icons-material/PlayArrowRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import ScheduleOutlined from '@mui/icons-material/ScheduleOutlined';
import HistoryRounded from '@mui/icons-material/HistoryRounded';
import EditCalendarRounded from '@mui/icons-material/EditCalendarRounded';
import StopRounded from '@mui/icons-material/StopRounded';
import SyncRounded from '@mui/icons-material/SyncRounded';
import SubtitlesRounded from '@mui/icons-material/SubtitlesRounded';
import CleaningServicesRounded from '@mui/icons-material/CleaningServicesRounded';
import EditNoteRounded from '@mui/icons-material/EditNoteRounded';
import { adminApi, isAbortError } from './api';
import type { TaskDefinition, TaskRun } from './api';
import { ErrorNotice } from './components';
import { ScheduleEditor } from './ScheduleEditor';
import { describeTrigger, scheduleDate } from './taskSchedule';
import { isActiveTaskRun, RunStatusChip, TaskRunDialog, TaskTimestamp } from './TaskRunDialog';
import type { TaskPanelStatus } from './TasksPage';
import { useTaskResource } from './useTaskResource';
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

function TaskIcon({ task }: { task: TaskDefinition }) {
  const name = `${task.Id} ${task.Name}`.toLowerCase();
  if (name.includes('subtitle')) return <SubtitlesRounded />;
  if (name.includes('cache') || name.includes('clean')) return <CleaningServicesRounded />;
  if (name.includes('metadata')) return <EditNoteRounded />;
  return <SyncRounded />;
}

function CurrentProgress({ run }: { run: TaskRun }) {
  const progress = run.TotalChildren > 0 ? Math.min(100, run.TerminalChildren / run.TotalChildren * 100) : undefined;
  return <Box sx={{ minWidth: 0 }}>
    <Stack direction="row" sx={{ alignItems: 'center', gap: 1 }}>
      <LinearProgress variant={progress === undefined ? 'indeterminate' : 'determinate'} value={progress} aria-label="Task work progress" aria-valuetext={progress === undefined ? 'Preparing work items' : `${run.TerminalChildren} of ${run.TotalChildren} work items finished`} sx={{ flex: 1, height: 5, borderRadius: 3, bgcolor: '#DCE4F2' }} />
      <Typography variant="caption" sx={{ fontVariantNumeric: 'tabular-nums', color: 'text.primary' }}>{progress === undefined ? run.State === 'pending' ? 'Queued' : 'Running' : `${Math.round(progress)}%`}</Typography>
    </Stack>
    <Typography variant="caption" color="text.secondary" component="div" sx={{ mt: 0.75 }}>{run.State === 'stopping' ? 'Stopping remaining work' : run.TotalChildren > 0 ? `${run.TerminalChildren.toLocaleString()} / ${run.TotalChildren.toLocaleString()} work items finished` : 'Preparing work items'}{run.Scanned > 0 ? ` · ${run.Scanned.toLocaleString()} processed` : ''}</Typography>
  </Box>;
}

export function ScheduledTasksPanel({ currentUserId, onNavigationGuardChange, onStatusChange }: { currentUserId: string; onNavigationGuardChange: UserNavigationGuardChange; onStatusChange?: (status: TaskPanelStatus) => void }) {
  const [starting, setStarting] = useState<string>();
  const [stopping, setStopping] = useState<string>();
  const [stopCandidate, setStopCandidate] = useState<{ task: TaskDefinition; run: TaskRun }>();
  const [stopError, setStopError] = useState<unknown>();
  const [startErrors, setStartErrors] = useState<Record<string, unknown>>({});
  const [pending, setPending] = useState<Map<string, string>>(new Map());
  const [editing, setEditing] = useState<string>();
  const [viewing, setViewing] = useState<{ task: TaskDefinition; run?: TaskRun; notice?: string; confirmedRequestId?: string }>();
  const mutation = useRef<AbortController | undefined>(undefined);
  const load = useCallback((signal: AbortSignal) => adminApi.getTasks({ signal }), []);
  const busy = Boolean(starting || stopping);
  const resource = useTaskResource({ key: currentUserId, load, poll: alwaysPoll, enabled: !busy });
  const stopTask = stopCandidate ? resource.data?.Items.find((task) => task.Id === stopCandidate.task.Id) : undefined;
  const stopRun = stopCandidate ? [stopTask?.CurrentRun, stopTask?.LastRun].find((run) => run?.Id === stopCandidate.run.Id) ?? stopCandidate.run : undefined;
  const statusMessage = resource.error != null ? 'Automatic updates stopped after a request error.' : resource.paused ? 'Updates pause while this tab is hidden.' : busy ? 'Updates resume after this request.' : 'Task status updates every 5 seconds.';
  useEffect(() => {
    onStatusChange?.({ message: statusMessage, active: !resource.paused && !busy && resource.error == null, refresh: resource.reload, refreshing: resource.loading || busy, refreshLabel: 'Refresh tasks' });
  }, [onStatusChange, statusMessage, resource.paused, resource.error, resource.loading, resource.reload, busy]);
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

  async function start(task: TaskDefinition) {
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
  }

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

  return <Stack spacing={2.5}>
    {!onStatusChange && <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
      <Typography variant="caption" color="text.secondary">{statusMessage}</Typography>
      <Button size="small" startIcon={<RefreshRounded />} onClick={resource.reload} disabled={resource.loading || busy}>Refresh tasks</Button>
    </Stack>}
    {resource.error != null && <ErrorNotice error={resource.error} retry={resource.reload} />}
    {!resource.data && resource.loading && <Paper variant="outlined" role="status" aria-label="Loading available tasks" sx={{ borderRadius: '20px', overflow: 'hidden', px: 3 }}>{[0, 1, 2, 3].map((row) => <Skeleton key={row} height={90} />)}</Paper>}
    {resource.data?.Items.length === 0 && <Paper variant="outlined" sx={{ p: 5, borderStyle: 'dashed', borderRadius: '20px', textAlign: 'center' }}><SyncRounded sx={{ fontSize: 32, color: 'text.disabled', mb: 1.5 }} /><Typography component="h2" sx={{ fontSize: 16, fontWeight: 600 }}>No available tasks</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.75 }}>No task definitions are available on this server. Scan history remains available in its tab.</Typography></Paper>}
    {resource.data && resource.data.Items.length > 0 && <Paper component="ul" variant="outlined" aria-label="Available server tasks" sx={{ p: 0, m: 0, listStyle: 'none', borderRadius: '20px', overflow: 'hidden' }}>{resource.data.Items.map((task) => {
      const current = task.CurrentRun;
      const recovering = pending.has(task.Id);
      const running = Boolean(current && isActiveTaskRun(current));
      const primaryLabel = starting === task.Id ? 'Requesting run...' : recovering ? 'Check start result' : running ? current?.State === 'stopping' ? 'Stopping run...' : 'Stop run' : 'Start task';
      const nextRun = !task.Enabled ? 'Task is disabled' : task.NextRunAt ? scheduleDate(task.NextRunAt, task.ScheduleTimezone) : 'No timed occurrence';
      return <Box component="li" key={task.Id} sx={{ borderBottom: 1, borderColor: 'divider', '&:last-child': { borderBottom: 0 }, '&:hover': { bgcolor: '#F7F9FD' } }}>
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '48px minmax(0, 1fr)', md: '48px minmax(0, 1.6fr) minmax(0, 1fr) minmax(0, 1.2fr) 136px' }, gap: { xs: '16px', md: '20px' }, alignItems: 'center', px: { xs: 2, sm: 3 }, py: 2.25 }}>
          <Box aria-hidden="true" sx={{ width: 48, height: 48, borderRadius: '12px', display: 'grid', placeItems: 'center', bgcolor: '#F3F6FB', color: 'text.secondary', alignSelf: { xs: 'start', md: 'center' }, '& svg': { fontSize: 24 } }}><TaskIcon task={task} /></Box>
          <Box sx={{ minWidth: 0 }}>
            <Typography component="h3" sx={{ fontSize: 14, fontWeight: 600, lineHeight: '20px', overflowWrap: 'anywhere' }}>{task.Name}</Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, fontSize: 13, lineHeight: '20px', overflowWrap: 'anywhere' }}>{task.Description}</Typography>
            {(!task.Enabled || task.IsHidden) && <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.75, mt: 1 }}>{!task.Enabled && <Chip size="small" label="Disabled" />}{task.IsHidden && <Chip size="small" label="Hidden" />}</Stack>}
          </Box>
          <Box sx={{ minWidth: 0, gridColumn: { xs: '2', md: 'auto' } }}>
            {task.Triggers.length ? <Stack direction="row" sx={{ gap: 0.75, flexWrap: 'wrap' }}>{task.Triggers.map((trigger) => <Tooltip key={trigger.Id} title={`${task.ScheduleTimezone} · Next run: ${nextRun}`}><Chip variant="outlined" size="small" icon={<ScheduleOutlined />} label={describeTrigger(trigger)} color={trigger.CalculationError ? 'warning' : 'default'} sx={{ maxWidth: '100%', height: 'auto', minHeight: 24, fontSize: 11, borderRadius: '6px', bgcolor: '#FFFFFF', '& .MuiChip-icon': { fontSize: 14 }, '& .MuiChip-label': { py: 0.4, whiteSpace: 'normal', overflowWrap: 'anywhere' } }} /></Tooltip>)}</Stack> : <Typography variant="caption" color="text.secondary">Manual starts only</Typography>}
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.75, fontSize: 11, overflowWrap: 'anywhere' }}>Next run: {nextRun}</Typography>
          </Box>
          <Box sx={{ minWidth: 0, gridColumn: { xs: '1 / -1', sm: '2', md: 'auto' } }}>
            {current ? <Box><CurrentProgress run={current} /><Button size="small" sx={{ px: 0, minHeight: 24, mt: 0.25, fontSize: 11, justifyContent: 'flex-start' }} onClick={() => setViewing({ task, run: current })}>View current run</Button></Box>
              : task.LastRun ? <Stack spacing={0.75} sx={{ alignItems: 'flex-start' }}><Tooltip title={<TaskTimestamp value={task.LastRun.FinishedAt ?? task.LastRun.CreatedAt} />}><Typography variant="caption" color="text.secondary" noWrap sx={{ maxWidth: '100%' }}>Last run <TaskTimestamp value={task.LastRun.FinishedAt ?? task.LastRun.CreatedAt} /></Typography></Tooltip><Box sx={{ '& .MuiChip-root': { height: 22, fontSize: 11, borderRadius: '6px' } }}><RunStatusChip state={task.LastRun.State} /></Box></Stack>
                : <Typography variant="caption" color="text.secondary">This task has not run yet.</Typography>}
          </Box>
          <Stack direction="row" sx={{ gap: 0.5, justifyContent: { xs: 'flex-end', md: 'center' }, gridColumn: { xs: '1 / -1', md: 'auto' } }}>
            <Tooltip title={primaryLabel}><span><IconButton aria-label={primaryLabel} disabled={busy || (!recovering && (running ? current?.State === 'stopping' : !task.Enabled))} onClick={() => { if (running && current && !recovering) { setStopError(undefined); setStopCandidate({ task, run: current }); } else void start(task); }} sx={{ width: 40, height: 40, bgcolor: '#D8E4FA', color: '#0F2A57', '&:hover': { bgcolor: '#C7D8F5' }, '&.Mui-disabled': { bgcolor: '#E4E8F0' } }}>{starting === task.Id || stopping === task.Id ? <CircularProgress size={18} color="inherit" /> : recovering ? <RefreshRounded sx={{ fontSize: 20 }} /> : running ? <StopRounded sx={{ fontSize: 20 }} /> : <PlayArrowRounded sx={{ fontSize: 22 }} />}</IconButton></span></Tooltip>
            <Tooltip title="Edit schedule"><span><IconButton aria-label="Edit schedule" onClick={() => setEditing(task.Id)} disabled={busy} sx={{ width: 40, height: 40 }}><EditCalendarRounded sx={{ fontSize: 20 }} /></IconButton></span></Tooltip>
            <Tooltip title="View runs"><span><IconButton aria-label="View runs" onClick={() => setViewing({ task })} disabled={busy} sx={{ width: 40, height: 40 }}><HistoryRounded sx={{ fontSize: 20 }} /></IconButton></span></Tooltip>
          </Stack>
        </Box>
        {(startErrors[task.Id] != null || (recovering && starting !== task.Id) || task.Triggers.some((trigger) => trigger.CalculationError)) && <Stack spacing={1} sx={{ px: { xs: 2, sm: 3 }, pb: 2 }}>
          {startErrors[task.Id] != null && <ErrorNotice error={startErrors[task.Id]} />}
          {recovering && starting !== task.Id && <Alert severity="warning">A start request has not been confirmed. Check its result before starting another run. The same request will be reused.</Alert>}
          {task.Triggers.filter((trigger) => trigger.CalculationError).map((trigger) => <Alert key={trigger.Id} severity="warning" sx={{ '& .MuiAlert-message': { minWidth: 0, overflowWrap: 'anywhere' } }}>This trigger is paused because its next occurrence could not be calculated. Edit and save the schedule to adjust it.<Typography variant="caption" component="div" sx={{ mt: 0.5 }}>{describeTrigger(trigger)}: {trigger.CalculationError}</Typography></Alert>)}
        </Stack>}
      </Box>;
    })}</Paper>}
    {stopCandidate && stopRun && <Dialog open onClose={busy ? undefined : () => setStopCandidate(undefined)} fullWidth maxWidth="sm" aria-labelledby="stop-task-title"><DialogTitle id="stop-task-title">Stop this run?</DialogTitle><DialogContent><Stack spacing={2}><Typography variant="body2" color="text.secondary">Stops the remaining work for {stopCandidate.task.Name}. Changes from completed work remain in the catalog.</Typography>{!isActiveTaskRun(stopRun) && <Alert severity="info">This run has already ended.</Alert>}{stopError != null && <><ErrorNotice error={stopError} /><Typography variant="body2" color="text.secondary">The stop request could not be confirmed. Retry for this same run, or close and refresh its status.</Typography></>}</Stack></DialogContent><DialogActions><Button disabled={busy} onClick={() => setStopCandidate(undefined)}>{isActiveTaskRun(stopRun) ? 'Keep run' : 'Close'}</Button><Button variant="contained" color="error" disabled={busy || !isActiveTaskRun(stopRun) || stopRun.State === 'stopping'} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <StopRounded />} onClick={() => void stop()}>{busy ? 'Requesting stop...' : 'Stop run'}</Button></DialogActions></Dialog>}
    {editing && <ScheduleEditor key={editing} taskId={editing} onClose={() => setEditing(undefined)} onSaved={resource.reload} onNavigationGuardChange={onNavigationGuardChange} />}
    {viewing && <TaskRunDialog key={`${viewing.task.Id}:${viewing.run?.Id ?? 'history'}`} task={viewing.task} initialRun={viewing.run} notice={viewing.notice} onClose={() => { setViewing(undefined); resource.reload(); }} onChanged={resource.reload} onNavigationGuardChange={onNavigationGuardChange} />}
  </Stack>;
}
