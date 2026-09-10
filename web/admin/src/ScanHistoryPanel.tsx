import { useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Paper, Skeleton, Snackbar, Stack, Typography } from '@mui/material';
import type { ChipProps } from '@mui/material';
import CancelOutlined from '@mui/icons-material/CancelOutlined';
import CheckCircleOutlineRounded from '@mui/icons-material/CheckCircleOutlineRounded';
import ErrorOutlineRounded from '@mui/icons-material/ErrorOutlineRounded';
import HelpOutlineRounded from '@mui/icons-material/HelpOutlineRounded';
import HourglassTopRounded from '@mui/icons-material/HourglassTopRounded';
import LibraryBooksOutlined from '@mui/icons-material/LibraryBooksOutlined';
import PauseCircleOutlineRounded from '@mui/icons-material/PauseCircleOutlineRounded';
import PlayArrowRounded from '@mui/icons-material/PlayArrowRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import WorkHistoryOutlined from '@mui/icons-material/WorkHistoryOutlined';
import { adminApi, isAbortError } from './api';
import type { Job, JobsResponse, Library } from './api';
import { ErrorNotice, errorText } from './components';

function normalizedStatus(job: Job): string {
  const status = job.Status.trim().toLowerCase();
  return status === 'queued' ? 'pending' : status;
}

function isActive(job: Job): boolean {
  return ['pending', 'running'].includes(normalizedStatus(job));
}

function statusDetails(job: Job) {
  const statuses = {
    pending: { label: 'Pending', color: 'default', icon: <HourglassTopRounded /> },
    running: { label: 'Running', color: 'primary', icon: <PlayArrowRounded /> },
    completed: { label: 'Completed', color: 'success', icon: <CheckCircleOutlineRounded /> },
    failed: { label: 'Failed', color: 'error', icon: <ErrorOutlineRounded /> },
    cancelled: { label: 'Cancelled', color: 'default', icon: <CancelOutlined /> },
    interrupted: { label: 'Interrupted', color: 'warning', icon: <PauseCircleOutlineRounded /> },
  };
  const status = normalizedStatus(job);
  if (Object.hasOwn(statuses, status)) return statuses[status as keyof typeof statuses];
  return { label: job.Status.trim() ? `Unknown: ${job.Status.trim()}` : 'Unknown status', color: 'default', icon: <HelpOutlineRounded /> };
}

function StatusChip({ job }: { job: Job }) {
  const status = statusDetails(job);
  return <Chip label={status.label} color={status.color as ChipProps['color']} icon={status.icon} variant="outlined" size="small" sx={{ maxWidth: '100%', height: 'auto', minHeight: 26, py: 0.2, '& .MuiChip-label': { whiteSpace: 'normal', overflowWrap: 'anywhere' } }} />;
}

function TaskTime({ label, value, empty }: { label: string; value: string | null; empty: string }) {
  const date = value ? new Date(value) : null;
  const valid = date !== null && !Number.isNaN(date.getTime());
  const formatted = valid
    ? date.toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit', second: '2-digit' })
    : value ? 'Unknown' : empty;

  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography component="dt" variant="caption" color="text.secondary">{label}</Typography>
      <Typography component="dd" variant="body2" sx={{ m: 0, mt: 0.25, overflowWrap: 'anywhere', fontVariantNumeric: 'tabular-nums' }}>
        {valid && value ? <time dateTime={value}>{formatted}</time> : formatted}
      </Typography>
    </Box>
  );
}

function TaskCard({ job, library, onCancel, busy }: { job: Job; library?: Library; onCancel: () => void; busy: boolean }) {
  const status = normalizedStatus(job);
  const failure = job.Error.trim() || (status === 'failed' ? `The ${job.ForceProbe ? 'media details refresh' : 'scan'} failed without an error message.` : '');

  return (
    <Box component="li" data-job-id={job.Id} sx={{ p: { xs: 2, sm: 3 }, borderTop: 1, borderColor: 'divider' }}>
      <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', alignItems: 'flex-start', gap: 2 }}>
        <Box sx={{ minWidth: 0, flex: 1 }}>
          <Typography variant="h4" component="h3" sx={{ overflowWrap: 'anywhere' }}>{library?.Name ?? `Library ${job.LibraryId}`}</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>{job.ForceProbe ? 'Media details refresh' : 'Library scan'}</Typography>
          <Typography variant="caption" color="text.secondary" component="div" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>Task <span className="mono">{job.Id}</span></Typography>
        </Box>
        <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1.2, maxWidth: '100%' }}>
          <StatusChip job={job} />
          {isActive(job) && <Button size="small" color="secondary" onClick={onCancel} disabled={busy} startIcon={<CancelOutlined />} aria-label={`Cancel ${job.ForceProbe ? 'media details refresh' : 'scan'} for ${library?.Name ?? `library ${job.LibraryId}`}`}>Cancel task</Button>}
        </Stack>
      </Stack>

      <Box component="dl" sx={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', m: 0, my: 2.5, py: 1.5, bgcolor: 'background.default', borderRadius: 2 }}>
        {([{ label: 'Scanned', value: job.Scanned }, { label: 'Added', value: job.Added }, { label: 'Updated', value: job.Updated }]).map((metric, index) => (
          <Box key={metric.label} sx={{ px: { xs: 1.5, sm: 2.5 }, minWidth: 0, borderLeft: index > 0 ? 1 : 0, borderColor: 'divider' }}>
            <Typography component="dt" variant="caption" color="text.secondary">{metric.label}</Typography>
            <Typography component="dd" sx={{ m: 0, mt: 0.4, fontSize: { xs: '1.15rem', sm: '1.4rem' }, fontWeight: 650, fontVariantNumeric: 'tabular-nums', overflowWrap: 'anywhere' }}>{metric.value.toLocaleString()}</Typography>
          </Box>
        ))}
      </Box>

      <Box component="dl" sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'repeat(3, minmax(0, 1fr))' }, gap: { xs: 1.5, md: 2 }, m: 0 }}>
        <TaskTime label="Created" value={job.CreatedAt} empty="Unknown" />
        <TaskTime label="Started" value={job.StartedAt} empty="Not started" />
        <TaskTime label="Finished" value={job.FinishedAt} empty="Not finished" />
      </Box>
      {status === 'running' && <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>{job.ForceProbe ? 'Re-reading media details and rebuilding supported playback indexes. Counts update as the refresh progresses.' : 'Scanning files. Counts update as the scan progresses.'}</Typography>}
      {status === 'pending' && <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>Waiting to start.</Typography>}
      {status === 'interrupted' && <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>{job.ForceProbe ? 'The refresh stopped before it finished. Use Refresh media details in Libraries to start again when ready.' : 'The scan stopped before it finished. Start another scan from Libraries when ready.'}</Typography>}
      {job.ForceProbe && status === 'completed' && <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>Refresh complete. Some formats do not support playback indexes.</Typography>}
      {failure && <Alert severity={status === 'failed' ? 'error' : 'warning'} role="note" sx={{ mt: 2, '& .MuiAlert-message': { minWidth: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' } }}>{failure}</Alert>}
    </Box>
  );
}

export function ScanHistoryPanel({ onLibraries }: { onLibraries: () => void }) {
  const [data, setData] = useState<JobsResponse>();
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [loading, setLoading] = useState(true);
  const [libraryLoading, setLibraryLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [libraryError, setLibraryError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const [libraryRevision, setLibraryRevision] = useState(0);
  const [paused, setPaused] = useState(document.hidden);
  const [selectedJob, setSelectedJob] = useState<Job | null>(null);
  const [cancelling, setCancelling] = useState(false);
  const [cancelError, setCancelError] = useState<unknown>(null);
  const [notice, setNotice] = useState('');
  const stopPolling = useRef<(() => void) | null>(null);
  const jobsRequest = useRef<Promise<JobsResponse> | null>(null);
  const cancelController = useRef<AbortController | null>(null);

  useEffect(() => {
    if (cancelling) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    let inFlight = false;
    let hasActiveJobs = false;
    let failed = false;

    function clearTimer() {
      if (timer !== undefined) clearTimeout(timer);
      timer = undefined;
    }

    function stop() {
      clearTimer();
      controller.abort();
    }

    async function load() {
      if (controller.signal.aborted || inFlight) return;
      clearTimer();
      inFlight = true;
      setLoading(true);
      let request: Promise<JobsResponse> | undefined;

      try {
        // A replacement request waits for the aborted request to settle.
        if (jobsRequest.current) await jobsRequest.current.catch(() => undefined);
        if (controller.signal.aborted) return;
        request = adminApi.getJobs({ signal: controller.signal });
        jobsRequest.current = request;
        const result = await request;
        if (controller.signal.aborted) return;
        setData(result);
        setError(null);
        hasActiveJobs = result.Items.some(isActive);
        failed = false;
      } catch (cause) {
        if (!controller.signal.aborted && !isAbortError(cause)) {
          failed = true;
          setError(cause);
        }
      } finally {
        if (request && jobsRequest.current === request) jobsRequest.current = null;
        inFlight = false;
        if (!controller.signal.aborted) {
          setLoading(false);
          if (hasActiveJobs && !failed && !document.hidden) {
            timer = setTimeout(() => { timer = undefined; void load(); }, 3000);
          }
        }
      }
    }

    function visibilityChanged() {
      setPaused(document.hidden);
      clearTimer();
      if (!document.hidden && hasActiveJobs && !failed && !inFlight) void load();
    }

    stopPolling.current = stop;
    setError(null);
    setPaused(document.hidden);
    document.addEventListener('visibilitychange', visibilityChanged);
    void load();
    return () => {
      stop();
      if (stopPolling.current === stop) stopPolling.current = null;
      document.removeEventListener('visibilitychange', visibilityChanged);
    };
  }, [revision, cancelling]);

  useEffect(() => {
    const controller = new AbortController();
    setLibraryLoading(true);
    setLibraryError(null);
    adminApi.getLibraries({ signal: controller.signal })
      .then((result) => { if (!controller.signal.aborted) setLibraries(result.Items); })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setLibraryError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLibraryLoading(false); });
    return () => controller.abort();
  }, [libraryRevision]);

  useEffect(() => () => cancelController.current?.abort(), []);

  function refresh() {
    stopPolling.current?.();
    setRevision((value) => value + 1);
    setLibraryRevision((value) => value + 1);
  }

  function closeDialog() {
    if (cancelling) return;
    setSelectedJob(null);
    setCancelError(null);
  }

  const currentJob = selectedJob ? data?.Items.find((job) => job.Id === selectedJob.Id) ?? selectedJob : null;

  async function cancelTask() {
    if (!currentJob || !isActive(currentJob) || cancelController.current) return;
    const controller = new AbortController();
    cancelController.current = controller;
    stopPolling.current?.();
    setLoading(false);
    setCancelling(true);
    setCancelError(null);

    try {
      const result = await adminApi.cancelJob(currentJob.Id, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setData((previous) => previous ? { ...previous, Items: previous.Items.map((job) => job.Id === result.Job.Id ? result.Job : job) } : previous);
      setSelectedJob(null);
      setNotice(normalizedStatus(result.Job) === 'cancelled' ? result.Job.ForceProbe ? 'Task cancelled. Media details already refreshed remain in the library.' : 'Task cancelled. Items already scanned remain in the library.' : isActive(result.Job) ? 'Cancellation requested. The task is stopping.' : `Task status: ${statusDetails(result.Job).label}.`);
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) setCancelError(cause);
    } finally {
      if (cancelController.current === controller) cancelController.current = null;
      if (!controller.signal.aborted) {
        setCancelling(false);
        setRevision((value) => value + 1);
      }
    }
  }

  const libraryById = new Map(libraries.map((library) => [library.Id, library]));
  const activeCount = data?.Items.filter(isActive).length ?? 0;

  return (
    <Box>
      {error != null && <Stack sx={{ mb: 3, gap: 1 }}><ErrorNotice error={error} retry={refresh} /><Typography variant="body2" color="text.secondary">Automatic updates stopped. Refresh to load the latest tasks.</Typography></Stack>}
      {libraryError != null && (
        <Alert severity="warning" sx={{ mb: 3, '& .MuiAlert-message': { minWidth: 0, overflowWrap: 'anywhere' } }} action={<Button color="inherit" size="small" disabled={libraryLoading} onClick={() => setLibraryRevision((value) => value + 1)}>Retry names</Button>}>
          Library names could not be refreshed. Missing names use library IDs.
          <Typography variant="body2" sx={{ mt: 0.5 }}>{errorText(libraryError)}</Typography>
        </Alert>
      )}
      <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
        <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', alignItems: { xs: 'flex-start', sm: 'center' }, gap: 1.5, px: { xs: 2, sm: 3 }, py: 2.2 }}>
          <Box>
            <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1.2 }}><Typography variant="h4" component="h2">Scan and refresh history</Typography>{data && <Chip label={data.TotalRecordCount.toLocaleString()} size="small" sx={{ bgcolor: 'background.default' }} />}</Stack>
            <Typography variant="caption" color="text.secondary" component="p" sx={{ m: 0, mt: 0.5 }}>
              {loading && data ? 'Refreshing tasks...' : error != null ? 'Updates paused after a request error.' : activeCount > 0 ? paused ? 'Updates paused while this tab is hidden.' : `${activeCount.toLocaleString()} active ${activeCount === 1 ? 'task' : 'tasks'} · Updates every 3 seconds` : data ? 'No active scans.' : 'Loading scan history...'}
            </Typography>
          </Box>
          <Button size="small" onClick={refresh} disabled={loading || cancelling} startIcon={<RefreshRounded />}>Refresh</Button>
        </Stack>
        {!data && loading && <Stack role="status" aria-label="Loading scan tasks" sx={{ p: { xs: 2, sm: 3 }, pt: 0, gap: 1 }}><Skeleton height={90} /><Skeleton height={90} /><Skeleton height={90} /></Stack>}
        {data && data.Items.length === 0 && (
          <Stack sx={{ alignItems: 'center', gap: 1.5, p: { xs: 3, sm: 5 }, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}>
            <WorkHistoryOutlined sx={{ fontSize: 40, color: 'primary.main' }} />
            <Typography variant="h3" component="h3">No scan tasks yet</Typography>
            <Typography color="text.secondary">Start a scan from Libraries to discover media on your server.</Typography>
            <Button onClick={onLibraries} startIcon={<LibraryBooksOutlined />}>Go to libraries</Button>
          </Stack>
        )}
        {data && data.Items.length > 0 && <Box component="ul" aria-label="Library scan tasks" sx={{ listStyle: 'none', m: 0, p: 0 }}>{data.Items.map((job) => <TaskCard key={job.Id} job={job} library={libraryById.get(job.LibraryId)} busy={cancelling} onCancel={() => { setCancelError(null); setSelectedJob(job); }} />)}</Box>}
        {!data && !loading && error != null && <Typography variant="body2" color="text.secondary" sx={{ px: { xs: 2, sm: 3 }, pb: 3 }}>Scan history could not be loaded. Retry the request to see your tasks.</Typography>}
      </Paper>
      <Typography variant="body2" color="text.secondary" sx={{ mt: 2.5, px: 0.5 }}>Counts show scanned files and the items added to or updated in your library.</Typography>

      <Dialog open={Boolean(currentJob)} onClose={cancelling ? undefined : closeDialog} fullWidth maxWidth="sm" aria-labelledby="cancel-task-title" aria-describedby="cancel-task-description">
        <DialogTitle id="cancel-task-title" sx={{ px: 3, pt: 3, pb: 1 }}><Typography component="span" variant="h3">{currentJob?.ForceProbe ? 'Cancel this media details refresh?' : 'Cancel this scan?'}</Typography></DialogTitle>
        <DialogContent aria-busy={cancelling} sx={{ px: 3 }}>
          <Stack sx={{ gap: 2 }}>
            {cancelError != null && <ErrorNotice error={cancelError} />}
            <Typography id="cancel-task-description">{currentJob?.ForceProbe ? 'Cancelling stops the remaining refresh work. Media details already refreshed remain in the library.' : 'Cancelling stops the remaining scan work. Items already scanned remain in the library.'}</Typography>
            {currentJob && <Box sx={{ p: 2, border: 1, borderColor: 'divider', borderRadius: 2, bgcolor: 'background.default' }}><Typography sx={{ fontWeight: 600, overflowWrap: 'anywhere' }}>{libraryById.get(currentJob.LibraryId)?.Name ?? `Library ${currentJob.LibraryId}`}</Typography><Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5, mb: 1, overflowWrap: 'anywhere' }}>Task <span className="mono">{currentJob.Id}</span></Typography><StatusChip job={currentJob} /></Box>}
            {currentJob && !isActive(currentJob) && <Alert severity="info">This task has already stopped and can no longer be cancelled.</Alert>}
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 3, pt: 1 }}>
          <Button color="secondary" onClick={closeDialog} disabled={cancelling}>{currentJob && isActive(currentJob) ? 'Keep task' : 'Close'}</Button>
          <Button color="error" variant="contained" onClick={() => void cancelTask()} disabled={cancelling || !currentJob || !isActive(currentJob)} startIcon={cancelling ? <CircularProgress size={16} color="inherit" /> : <CancelOutlined />}>{cancelling ? 'Cancelling...' : 'Cancel task'}</Button>
        </DialogActions>
      </Dialog>
      <Snackbar open={Boolean(notice)} autoHideDuration={6000} onClose={() => setNotice('')} anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}><Alert severity="success" variant="filled" onClose={() => setNotice('')}>{notice}</Alert></Snackbar>
    </Box>
  );
}
