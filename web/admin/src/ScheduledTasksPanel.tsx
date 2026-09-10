import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Paper, Skeleton, Stack, Typography } from '@mui/material';
import PlayArrowRounded from '@mui/icons-material/PlayArrowRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import ScheduleOutlined from '@mui/icons-material/ScheduleOutlined';
import WorkHistoryOutlined from '@mui/icons-material/WorkHistoryOutlined';
import { adminApi, isAbortError } from './api';
import type { TaskDefinition, TaskRun } from './api';
import { ErrorNotice } from './components';
import { ScheduleEditor } from './ScheduleEditor';
import { describeTrigger, scheduleDate } from './taskSchedule';
import { isActiveTaskRun, RunProgress, RunStatusChip, TaskRunDialog, TaskTimestamp } from './TaskRunDialog';
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

export function ScheduledTasksPanel({ currentUserId, onNavigationGuardChange }: { currentUserId: string; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [starting, setStarting] = useState<string>();
  const [startErrors, setStartErrors] = useState<Record<string, unknown>>({});
  const [pending, setPending] = useState<Map<string, string>>(new Map());
  const [editing, setEditing] = useState<string>();
  const [viewing, setViewing] = useState<{ task: TaskDefinition; run?: TaskRun; notice?: string; confirmedRequestId?: string }>();
  const mutation = useRef<AbortController | undefined>(undefined);
  const load = useCallback((signal: AbortSignal) => adminApi.getTasks({ signal }), []);
  const resource = useTaskResource({ key: currentUserId, load, poll: alwaysPoll, enabled: !starting });
  useUserDraftNavigation(false, Boolean(starting), onNavigationGuardChange);
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

  return <Stack spacing={2.5}>
    <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
      <Box><Stack direction="row" sx={{ alignItems: 'center', gap: 1 }}><Typography component="h2" variant="h4">Available tasks</Typography>{resource.data && <Chip size="small" label={resource.data.TotalRecordCount.toLocaleString()} />}</Stack><Typography variant="caption" color="text.secondary">{resource.error != null ? 'Automatic updates stopped after a request error.' : resource.paused ? 'Updates pause while this tab is hidden.' : 'Task status updates every 5 seconds.'}</Typography></Box>
      <Button size="small" startIcon={<RefreshRounded />} onClick={resource.reload} disabled={resource.loading || Boolean(starting)}>Refresh tasks</Button>
    </Stack>
    {resource.error != null && <ErrorNotice error={resource.error} retry={resource.reload} />}
    {!resource.data && resource.loading && <Stack role="status" aria-label="Loading available tasks" spacing={2}><Skeleton variant="rounded" height={220} /><Skeleton variant="rounded" height={220} /></Stack>}
    {resource.data?.Items.length === 0 && <Paper variant="outlined" sx={{ p: 4 }}><Typography variant="h3" component="h3">No available tasks</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>No task definitions are available on this server. Scan history remains available in its tab.</Typography></Paper>}
    {resource.data && <Stack component="ul" aria-label="Available server tasks" spacing={2.5} sx={{ p: 0, m: 0, listStyle: 'none' }}>{resource.data.Items.map((task) => <Paper key={task.Id} component="li" variant="outlined" sx={{ p: { xs: 2, sm: 3 } }}>
      <Stack spacing={2.5}>
        <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', alignItems: 'flex-start', gap: 1.5 }}><Box sx={{ minWidth: 0 }}>{task.Category && <Typography variant="overline" color="text.secondary">{task.Category}</Typography>}<Typography component="h3" variant="h3" sx={{ overflowWrap: 'anywhere' }}>{task.Name}</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.75, overflowWrap: 'anywhere' }}>{task.Description}</Typography></Box><Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.75 }}><Chip size="small" variant="outlined" label={task.Enabled ? 'Enabled' : 'Disabled'} color={task.Enabled ? 'success' : 'default'} />{task.IsHidden && <Chip size="small" variant="outlined" label="Hidden" />}</Stack></Stack>
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'minmax(0, 1fr) minmax(0, 1fr)' }, gap: 2.5 }}>
          <Box sx={{ minWidth: 0 }}><Typography variant="caption" color="text.secondary">Schedule · {task.ScheduleTimezone}</Typography>{task.Triggers.length > 0 ? <Box component="ul" sx={{ m: 0, mt: 0.75, pl: 2.5 }}>{task.Triggers.map((trigger) => <Box key={trigger.Id} component="li" sx={{ overflowWrap: 'anywhere', mb: 0.75 }}><Typography variant="body2">{describeTrigger(trigger)}</Typography>{trigger.CalculationError && <Alert severity="warning" sx={{ mt: 1, '& .MuiAlert-message': { minWidth: 0, overflowWrap: 'anywhere' } }}>This trigger is paused because its next occurrence could not be calculated. Edit and save the schedule to adjust it.<Typography variant="caption" component="div" sx={{ mt: 0.5 }}>{trigger.CalculationError}</Typography></Alert>}</Box>)}</Box> : <Typography variant="body2" sx={{ mt: 0.75 }}>Manual starts only</Typography>}<Typography variant="caption" color="text.secondary" component="p" sx={{ m: 0, mt: 1.5, overflowWrap: 'anywhere' }}>Next run: {!task.Enabled ? 'Task is disabled' : task.NextRunAt ? scheduleDate(task.NextRunAt, task.ScheduleTimezone) : 'No timed occurrence'}</Typography></Box>
          <Box sx={{ p: 2, bgcolor: 'background.default', borderRadius: 2 }}>
            {task.CurrentRun ? <Stack spacing={1.5}><Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography variant="body2" sx={{ fontWeight: 650 }}>Current run</Typography><RunStatusChip state={task.CurrentRun.State} /></Stack><RunProgress run={task.CurrentRun} compact /><Box><Button size="small" sx={{ ml: -1, px: 1 }} onClick={() => setViewing({ task, run: task.CurrentRun! })}>View current run</Button></Box></Stack>
              : task.LastRun ? <Stack spacing={1}><Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1 }}><Typography variant="body2" sx={{ fontWeight: 650 }}>Last run</Typography><RunStatusChip state={task.LastRun.State} /></Stack><Typography variant="caption" color="text.secondary"><TaskTimestamp value={task.LastRun.FinishedAt ?? task.LastRun.CreatedAt} /></Typography><RunProgress run={task.LastRun} compact /></Stack>
                : <Typography variant="body2" color="text.secondary">This task has not run yet.</Typography>}
          </Box>
        </Box>
        {startErrors[task.Id] != null && <ErrorNotice error={startErrors[task.Id]} />}
        {pending.has(task.Id) && starting !== task.Id && <Alert severity="warning">A start request has not been confirmed. Check its result before starting another run. The same request will be reused.</Alert>}
        <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}>
          <Button variant="contained" startIcon={starting === task.Id ? <CircularProgress size={16} color="inherit" /> : pending.has(task.Id) ? <RefreshRounded /> : <PlayArrowRounded />} disabled={Boolean(starting) || (!pending.has(task.Id) && (!task.Enabled || Boolean(task.CurrentRun)))} onClick={() => void start(task)}>{starting === task.Id ? 'Requesting run...' : pending.has(task.Id) ? 'Check start result' : 'Start task'}</Button>
          <Button variant="outlined" startIcon={<WorkHistoryOutlined />} onClick={() => setViewing({ task })} disabled={Boolean(starting)}>View runs</Button>
          <Button startIcon={<ScheduleOutlined />} onClick={() => setEditing(task.Id)} disabled={Boolean(starting)}>Edit schedule</Button>
        </Stack>
      </Stack>
    </Paper>)}</Stack>}
    {editing && <ScheduleEditor key={editing} taskId={editing} onClose={() => setEditing(undefined)} onSaved={resource.reload} onNavigationGuardChange={onNavigationGuardChange} />}
    {viewing && <TaskRunDialog key={`${viewing.task.Id}:${viewing.run?.Id ?? 'history'}`} task={viewing.task} initialRun={viewing.run} notice={viewing.notice} onClose={() => { setViewing(undefined); resource.reload(); }} onChanged={resource.reload} onNavigationGuardChange={onNavigationGuardChange} />}
  </Stack>;
}
