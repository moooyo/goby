import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, LinearProgress, Paper, Skeleton, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, Typography } from '@mui/material';
import type { ChipProps } from '@mui/material';
import ArrowBackRounded from '@mui/icons-material/ArrowBackRounded';
import CancelOutlined from '@mui/icons-material/CancelOutlined';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { adminApi, isAbortError } from './api';
import type { TaskChild, TaskChildState, TaskDefinition, TaskRun, TaskRunDetail, TaskRunsResponse, TaskRunState } from './api';
import { ErrorNotice } from './components';
import { useTaskResource } from './useTaskResource';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const alwaysPoll = () => true;
const pollActiveRun = (result: TaskRunDetail) => isActiveTaskRun(result.Run);

export function isActiveTaskRun(run: TaskRun): boolean {
  return ['pending', 'running', 'stopping'].includes(run.State);
}

export function TaskTimestamp({ value, empty = 'Not recorded' }: { value: string | null; empty?: string }) {
  if (!value) return <>{empty}</>;
  return <time dateTime={value} title={value}>{new Date(value).toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' })}</time>;
}

export function RunStatusChip({ state }: { state: TaskRunState | TaskChildState }) {
  const color: ChipProps['color'] = state === 'completed' ? 'success' : state === 'failed' ? 'error'
    : ['stopping', 'interrupted', 'unavailable'].includes(state) ? 'warning' : state === 'running' ? 'primary' : 'default';
  return <Chip size="small" variant="outlined" color={color} label={state[0].toUpperCase() + state.slice(1)} />;
}

export function RunProgress({ run, compact = false }: { run: TaskRun; compact?: boolean }) {
  const outcomes = [
    ['Completed', run.CompletedChildren], ['Failed', run.FailedChildren], ['Cancelled', run.CancelledChildren],
    ['Interrupted', run.InterruptedChildren], ['Unavailable', run.UnavailableChildren],
  ] as const;
  return <Box>
    <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'baseline', gap: 1, mb: 0.8 }}>
      <Typography variant="body2" sx={{ fontWeight: 600 }}>{run.TotalChildren > 0 ? `${run.TerminalChildren.toLocaleString()} of ${run.TotalChildren.toLocaleString()} libraries finished` : run.State === 'pending' ? 'Preparing the library list' : 'No libraries to process'}</Typography>
      {!compact && <Typography variant="caption" color="text.secondary">Library progress</Typography>}
    </Stack>
    {run.TotalChildren > 0 && <LinearProgress variant="determinate" value={run.TerminalChildren / run.TotalChildren * 100} aria-label="Library progress" aria-valuetext={`${run.TerminalChildren} of ${run.TotalChildren} libraries finished`} sx={{ height: 6, borderRadius: 1 }} />}
    {!compact && <>
      <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1.5, mt: 1.5 }}>{outcomes.filter(([, count]) => count > 0).map(([label, count]) => <Typography key={label} variant="caption" color="text.secondary">{label}: {count.toLocaleString()}</Typography>)}</Stack>
      <Box component="dl" sx={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: 1, m: 0, mt: 2 }}>
        {([['Scanned files', run.Scanned], ['Added items', run.Added], ['Updated items', run.Updated]] as const).map(([label, value]) => <Box key={label} sx={{ minWidth: 0 }}><Typography variant="caption" component="dt" color="text.secondary">{label}</Typography><Typography component="dd" sx={{ m: 0, fontWeight: 650, fontSize: '1.15rem', overflowWrap: 'anywhere', fontVariantNumeric: 'tabular-nums' }}>{value.toLocaleString()}</Typography></Box>)}
      </Box>
    </>}
  </Box>;
}

function RunMessage({ run }: { run: TaskRun }) {
  return <>
    {run.State === 'stopping' && <Alert severity="info">Stopping the remaining work. The run stays in Stopping until all library work has ended.</Alert>}
    {(run.ErrorMessage || run.State === 'failed') && <Alert severity={run.State === 'failed' ? 'error' : 'warning'} sx={{ '& .MuiAlert-message': { overflowWrap: 'anywhere', whiteSpace: 'pre-wrap' } }}>{run.ErrorMessage || 'This run failed without an error message.'}{run.ErrorCode && <Typography variant="caption" component="div">Code: {run.ErrorCode}</Typography>}</Alert>}
    {run.StopReason && <Typography variant="body2" color="text.secondary">Stop reason: {run.StopReason.replaceAll('_', ' ')}</Typography>}
  </>;
}

function TaskPagination({ total, page, limit, label, loading, onPage, onLimit }: { total: number; page: number; limit: number; label: string; loading: boolean; onPage: (page: number) => void; onLimit: (limit: number) => void }) {
  return <TablePagination component="div" count={total} page={page} rowsPerPage={limit} rowsPerPageOptions={[25, 50, 100, 200]} labelRowsPerPage={label} disabled={loading}
    onPageChange={(_event, value) => onPage(value)} onRowsPerPageChange={(event) => { const value = Number(event.target.value); if ([25, 50, 100, 200].includes(value)) onLimit(value); }}
    sx={{ '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', justifyContent: 'flex-end', gap: 0.5, px: { xs: 0, sm: 1 } }, '& .MuiTablePagination-spacer': { display: 'none' }, '& .MuiTablePagination-actions': { ml: 1 } }} />;
}

function ChildRecord({ child }: { child: TaskChild }) {
  return <Box component="li" sx={{ p: { xs: 2, sm: 2.5 }, borderTop: 1, borderColor: 'divider' }}>
    <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: 1 }}><Box sx={{ minWidth: 0 }}><Typography variant="body2" sx={{ fontWeight: 650, overflowWrap: 'anywhere' }}>{child.LibraryName || child.LibraryId || 'Unavailable library'}</Typography><Typography variant="caption" color="text.secondary">Library {child.Ordinal + 1}</Typography></Box><RunStatusChip state={child.State} /></Stack>
    <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 2, mt: 1.5 }}>{([['Scanned', child.Scanned], ['Added', child.Added], ['Updated', child.Updated]] as const).map(([label, count]) => <Typography variant="body2" key={label}>{label}: {count.toLocaleString()}</Typography>)}</Stack>
    <Box component="dl" sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(3, minmax(0, 1fr))' }, gap: 1.5, m: 0, mt: 1.5 }}>
      {([['Created', child.CreatedAt], ['Started', child.StartedAt], ['Finished', child.FinishedAt]] as const).map(([label, value]) => <Box key={label}><Typography component="dt" variant="caption" color="text.secondary">{label}</Typography><Typography component="dd" variant="caption" sx={{ m: 0 }}><TaskTimestamp value={value} /></Typography></Box>)}
    </Box>
    {child.ErrorMessage && <Alert severity={child.State === 'failed' ? 'error' : 'warning'} sx={{ mt: 1.5, '& .MuiAlert-message': { whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' } }}>{child.ErrorMessage}{child.ErrorCode && <Typography variant="caption" component="div">Code: {child.ErrorCode}</Typography>}</Alert>}
  </Box>;
}

function RunDetails({ initialRun, onChanged, onBusyChange }: { initialRun: TaskRun; onChanged: () => void; onBusyChange: (busy: boolean) => void }) {
  const [page, setPage] = useState(0);
  const [limit, setLimit] = useState(50);
  const [busy, setBusy] = useState(false);
  const [cancelError, setCancelError] = useState<unknown>();
  const [confirming, setConfirming] = useState(false);
  const [acknowledged, setAcknowledged] = useState<TaskRun>();
  const [lastObserved, setLastObserved] = useState(initialRun);
  const mutation = useRef<AbortController | undefined>(undefined);
  const load = useCallback((signal: AbortSignal) => adminApi.getTaskRun(initialRun.Id, { StartIndex: page * limit, Limit: limit }, { signal }), [initialRun.Id, page, limit]);
  const resource = useTaskResource({ key: `${initialRun.Id}:${page}:${limit}`, load, poll: pollActiveRun, enabled: !busy && cancelError == null });
  const run = acknowledged ?? resource.data?.Run ?? lastObserved;

  useEffect(() => { if (resource.data) { setLastObserved(resource.data.Run); setAcknowledged(undefined); } }, [resource.data]);
  useEffect(() => () => { mutation.current?.abort(); onBusyChange(false); }, [onBusyChange]);
  useEffect(() => {
    if (resource.data && page > 0 && page * limit >= resource.data.Children.TotalRecordCount) setPage(Math.max(0, Math.ceil(resource.data.Children.TotalRecordCount / limit) - 1));
  }, [resource.data, page, limit]);

  function refresh() { setCancelError(undefined); resource.reload(); }

  async function stopRun() {
    if (mutation.current || !isActiveTaskRun(run) || run.State === 'stopping') return;
    const controller = new AbortController();
    mutation.current = controller;
    setBusy(true);
    onBusyChange(true);
    setCancelError(undefined);
    try {
      const result = await adminApi.cancelTaskRun(run.Id, { signal: controller.signal });
      if (!controller.signal.aborted) { setAcknowledged(result.Run); setConfirming(false); onChanged(); resource.reload(); }
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) setCancelError(cause);
    } finally {
      if (mutation.current === controller) mutation.current = undefined;
      if (!controller.signal.aborted) { setBusy(false); onBusyChange(false); }
    }
  }

  return <Stack spacing={2.5}>
    <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1.5 }}><Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1 }}><RunStatusChip state={run.State} /><Typography variant="body2" color="text.secondary">{run.Source.replaceAll('_', ' ')}</Typography></Stack><Stack direction="row" sx={{ gap: 1 }}><Button size="small" startIcon={<RefreshRounded />} disabled={busy || resource.loading} onClick={refresh}>Refresh run</Button>{isActiveTaskRun(run) && <Button size="small" color="error" startIcon={<CancelOutlined />} disabled={busy || run.State === 'stopping'} onClick={() => setConfirming(true)}>{run.State === 'stopping' ? 'Stopping...' : 'Stop run'}</Button>}</Stack></Stack>
    {resource.error != null && <Stack spacing={1}><ErrorNotice error={resource.error} retry={refresh} /><Typography variant="body2" color="text.secondary">Automatic updates stopped. The summary shows the last recorded state. Refresh this run to retry.</Typography></Stack>}
    {cancelError != null && !confirming && <ErrorNotice error={cancelError} retry={refresh} />}
    <RunMessage run={run} />
    <Paper variant="outlined" sx={{ p: 2.5, bgcolor: 'background.default' }}><RunProgress run={run} /></Paper>
    <Box component="dl" sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))' }, m: 0, gap: 1.5 }}>
      {([['Created', run.CreatedAt], ['Started', run.StartedAt], ['Finished', run.FinishedAt], ['Stop requested', run.StopRequestedAt]] as const).map(([label, value]) => <Box key={label}><Typography variant="caption" component="dt" color="text.secondary">{label}</Typography><Typography variant="body2" component="dd" sx={{ m: 0 }}><TaskTimestamp value={value} /></Typography></Box>)}
    </Box>
    <Box><Typography variant="h4" component="h3">Library work</Typography><Typography variant="caption" color="text.secondary">{resource.paused ? 'Updates pause while this tab is hidden.' : isActiveTaskRun(run) && resource.error == null ? 'Updates every 5 seconds while the run is active.' : 'Counts describe recorded library work.'}</Typography></Box>
    {!resource.data && resource.loading && <Stack role="status" aria-label="Loading library work"><Skeleton height={75} /><Skeleton height={75} /></Stack>}
    {resource.data && resource.data.Children.Items.length === 0 && <Typography variant="body2" color="text.secondary">{run.State === 'pending' ? 'The library list is being prepared.' : 'No library work was recorded for this run.'}</Typography>}
    {resource.data && resource.data.Children.Items.length > 0 && <Paper variant="outlined" component="ul" aria-label="Task library work" sx={{ listStyle: 'none', p: 0, m: 0, overflow: 'hidden', '& > li:first-of-type': { borderTop: 0 } }}>{resource.data.Children.Items.map((child) => <ChildRecord key={child.Id} child={child} />)}</Paper>}
    {resource.data && <TaskPagination total={resource.data.Children.TotalRecordCount} page={page} limit={limit} label="Libraries per page:" loading={resource.loading || busy} onPage={setPage} onLimit={(value) => { setLimit(value); setPage(0); }} />}
    <Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>Run <span className="mono">{run.Id}</span> · Times use your local time zone.</Typography>
    {confirming && <Dialog open onClose={busy ? undefined : () => setConfirming(false)} fullWidth maxWidth="sm" aria-labelledby="stop-run-title"><DialogTitle id="stop-run-title">Stop this run?</DialogTitle><DialogContent><Stack spacing={2}><Typography variant="body2" color="text.secondary">Stops the remaining library work. Changes from completed work remain in the catalog.</Typography>{!isActiveTaskRun(run) && <Alert severity="info">This run has already ended.</Alert>}{cancelError != null && <><ErrorNotice error={cancelError} /><Typography variant="body2" color="text.secondary">The stop request could not be confirmed. Retry for this same run, or close and refresh its status.</Typography></>}</Stack></DialogContent><DialogActions sx={{ px: 3, pb: 3, flexWrap: 'wrap', gap: 1 }}><Button color="secondary" disabled={busy} onClick={() => setConfirming(false)}>{isActiveTaskRun(run) ? 'Keep run' : 'Close'}</Button><Button color="error" variant="contained" disabled={busy || !isActiveTaskRun(run) || run.State === 'stopping'} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <CancelOutlined />} onClick={() => void stopRun()}>{busy ? 'Requesting stop...' : 'Stop run'}</Button></DialogActions></Dialog>}
  </Stack>;
}

function RunHistory({ taskId, onSelect }: { taskId: string; onSelect: (run: TaskRun) => void }) {
  const [page, setPage] = useState(0);
  const [limit, setLimit] = useState(50);
  const load = useCallback((signal: AbortSignal): Promise<TaskRunsResponse> => adminApi.getTaskRuns(taskId, { StartIndex: page * limit, Limit: limit }, { signal }), [taskId, page, limit]);
  const resource = useTaskResource({ key: `${taskId}:${page}:${limit}`, load, poll: alwaysPoll });
  useEffect(() => { if (resource.data && page > 0 && page * limit >= resource.data.TotalRecordCount) setPage(Math.max(0, Math.ceil(resource.data.TotalRecordCount / limit) - 1)); }, [resource.data, page, limit]);
  return <Stack spacing={2}>
    <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center', gap: 1 }}><Typography variant="body2" color="text.secondary">{resource.paused ? 'Updates pause while this tab is hidden.' : 'Runs update every 5 seconds.'}</Typography><Button size="small" startIcon={<RefreshRounded />} onClick={resource.reload} disabled={resource.loading}>Refresh runs</Button></Stack>
    {resource.error != null && <Stack spacing={1}><ErrorNotice error={resource.error} retry={resource.reload} /><Typography variant="body2" color="text.secondary">Automatic updates stopped. Refresh runs to retry.</Typography></Stack>}
    {!resource.data && resource.loading && <Stack role="status" aria-label="Loading task runs"><Skeleton height={70} /><Skeleton height={70} /></Stack>}
    {resource.data?.Items.length === 0 && <Alert severity="info">This task has not run yet. Start it from Available tasks or add an automatic trigger.</Alert>}
    {resource.data && resource.data.Items.length > 0 && <>
      <Box component="ul" aria-label="Task runs" sx={{ display: { xs: 'block', md: 'none' }, listStyle: 'none', m: 0, p: 0 }}>{resource.data.Items.map((run) => <Paper component="li" variant="outlined" key={run.Id} sx={{ p: 2, mb: 1.5 }}><Stack spacing={1.5}><Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1 }}><RunStatusChip state={run.State} /><Typography variant="caption" color="text.secondary">{run.Source.replaceAll('_', ' ')}</Typography></Stack><Typography variant="body2"><TaskTimestamp value={run.CreatedAt} /></Typography><RunProgress run={run} compact /><Box><Button size="small" variant="outlined" onClick={() => onSelect(run)}>View run</Button></Box></Stack></Paper>)}</Box>
      <TableContainer sx={{ display: { xs: 'none', md: 'block' } }}><Table aria-label="Task runs"><TableHead><TableRow><TableCell>Created</TableCell><TableCell>State</TableCell><TableCell>Libraries finished</TableCell><TableCell align="right">Details</TableCell></TableRow></TableHead><TableBody>{resource.data.Items.map((run) => <TableRow key={run.Id}><TableCell><Typography variant="body2"><TaskTimestamp value={run.CreatedAt} /></Typography><Typography variant="caption" color="text.secondary">{run.Source.replaceAll('_', ' ')}</Typography></TableCell><TableCell><RunStatusChip state={run.State} /></TableCell><TableCell>{run.TerminalChildren.toLocaleString()} / {run.TotalChildren.toLocaleString()}</TableCell><TableCell align="right"><Button size="small" onClick={() => onSelect(run)}>View run</Button></TableCell></TableRow>)}</TableBody></Table></TableContainer>
    </>}
    {resource.data && <TaskPagination total={resource.data.TotalRecordCount} page={page} limit={limit} label="Runs per page:" loading={resource.loading} onPage={setPage} onLimit={(value) => { setLimit(value); setPage(0); }} />}
  </Stack>;
}

export function TaskRunDialog({ task, initialRun, notice, onClose, onChanged, onNavigationGuardChange }: { task: TaskDefinition; initialRun?: TaskRun; notice?: string; onClose: () => void; onChanged: () => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [selected, setSelected] = useState(initialRun);
  const [busy, setBusy] = useState(false);
  useUserDraftNavigation(false, busy, onNavigationGuardChange);
  return <Dialog open onClose={busy ? undefined : onClose} fullWidth maxWidth="md" aria-labelledby="task-runs-title">
    <DialogTitle id="task-runs-title" sx={{ pt: 3, overflowWrap: 'anywhere' }}>{selected ? 'Run details' : 'Run history'} · {task.Name}</DialogTitle>
    <DialogContent>{notice && <Alert severity="info" sx={{ mb: 2 }}>{notice}</Alert>}{selected ? <><Button size="small" color="secondary" startIcon={<ArrowBackRounded />} onClick={() => setSelected(undefined)} disabled={busy} sx={{ mb: 2 }}>Back to run history</Button><RunDetails key={selected.Id} initialRun={selected} onChanged={onChanged} onBusyChange={setBusy} /></> : <RunHistory taskId={task.Id} onSelect={setSelected} />}</DialogContent>
    <DialogActions sx={{ px: 3, pb: 3 }}><Button color="secondary" onClick={onClose} disabled={busy}>Close</Button></DialogActions>
  </Dialog>;
}
