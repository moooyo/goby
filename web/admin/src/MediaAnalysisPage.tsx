import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, LinearProgress, Paper, Skeleton, Stack, TextField, Tooltip, Typography } from '@mui/material';
import CheckCircleRounded from '@mui/icons-material/CheckCircleRounded';
import PauseCircleOutlineRounded from '@mui/icons-material/PauseCircleOutlineRounded';
import PlaylistPlayRounded from '@mui/icons-material/PlaylistPlayRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { adminApi, ApiError, isAbortError } from './api';
import type { Library, TaskRunDetail } from './api';
import { ErrorNotice, PageHeading } from './components';
import { analysisBytes, analysisDraft, analysisNumberFields, parseAnalysisDraft } from './mediaAnalysis';
import type { AnalysisDraft, AnalysisOverview, AnalysisPrune, AnalysisRunInput, AnalysisRunReceipt } from './mediaAnalysis';
import { mediaAnalysisApi } from './mediaAnalysisApi';
import { MediaAnalysisResults } from './MediaAnalysisResults';
import { mediaPanelSx, mediaSurface } from './MediaPagePrimitives';
import { isActiveTaskRun, RunProgress, RunStatusChip } from './TaskRunDialog';
import { useTaskResource } from './useTaskResource';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const activeRun = (value: TaskRunDetail) => isActiveTaskRun(value.Run);
const receipts = new Map<string, AnalysisRunInput>();
const receiptKey = (userId: string) => `goby.media-analysis.admission.${encodeURIComponent(userId)}`;
function remember(userId: string, value?: AnalysisRunInput): void {
  const key = receiptKey(userId);
  if (value) receipts.set(key, value); else receipts.delete(key);
  try { if (value) sessionStorage.setItem(key, JSON.stringify(value)); else sessionStorage.removeItem(key); } catch { /* Retain the in-memory receipt when session storage is unavailable. */ }
}
function previousReceipt(userId: string): AnalysisRunInput | undefined {
  const key = receiptKey(userId); const memory = receipts.get(key); if (memory) return memory;
  try {
    const value = JSON.parse(sessionStorage.getItem(key) ?? 'null') as AnalysisRunInput | null;
    if (value && ['intro', 'previews'].includes(value.Kind) && typeof value.RequestId === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value.RequestId)
      && typeof value.Force === 'boolean' && Array.isArray(value.LibraryIds) && value.LibraryIds.length <= 64 && value.LibraryIds.every((id) => typeof id === 'string' && id.length > 0 && id.length <= 128)
      && Array.isArray(value.ItemIds) && value.ItemIds.length <= 256 && value.ItemIds.every((id) => typeof id === 'string' && id.length > 0 && id.length <= 128)) return value;
  } catch { /* An unreadable browser receipt is not a server result. */ }
  return undefined;
}
function unknownResult(error: unknown): boolean { return !(error instanceof ApiError) || error.status === 409 || error.status >= 500 || ['network_error', 'invalid_response', 'session_changed'].includes(error.code); }

function AvailabilityChip({ available, label }: { available: boolean; label: string }) {
  return <Chip size="small" icon={available ? <CheckCircleRounded /> : <PauseCircleOutlineRounded />} label={label} sx={{ height: 28, borderRadius: '6px', bgcolor: 'background.paper', fontSize: 12, '& .MuiChip-icon': { fontSize: 17, color: available ? 'success.main' : 'text.secondary' } }} />;
}

export function MediaAnalysisPage({ currentUserId, onTasks, onLibraries, onNavigationGuardChange }: { currentUserId: string; onTasks: () => void; onLibraries: () => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [overview, setOverview] = useState<AnalysisOverview>(); const [draft, setDraft] = useState<AnalysisDraft>(); const [libraries, setLibraries] = useState<Library[]>([]);
  const [loading, setLoading] = useState(true); const [loadError, setLoadError] = useState<unknown>(); const [loadRevision, setLoadRevision] = useState(0); const preserveDraft = useRef(false);
  const [busy, setBusy] = useState<string>(); const mutation = useRef<AbortController | undefined>(undefined);
  const [error, setError] = useState<unknown>(); const [reloadRequired, setReloadRequired] = useState(false); const [notice, setNotice] = useState('');
  const [pending, setPending] = useState<AnalysisRunInput | undefined>(() => previousReceipt(currentUserId));
  const [receipt, setReceipt] = useState<AnalysisRunReceipt>(); const [runError, setRunError] = useState<unknown>(); const [resultsRevision, setResultsRevision] = useState(0);
  const [pruning, setPruning] = useState(false); const [pruned, setPruned] = useState<AnalysisPrune>(); const finished = useRef('');
  const [configurationOpen, setConfigurationOpen] = useState(false);
  const parsed = draft ? parseAnalysisDraft(draft) : undefined;
  const dirty = Boolean(draft && overview && JSON.stringify(draft) !== JSON.stringify(analysisDraft(overview.Configuration.Profile)));
  const locked = Boolean(busy);
  const blocked = locked || loading || loadError != null || reloadRequired;
  useUserDraftNavigation(dirty || Boolean(pending), locked, onNavigationGuardChange, 'Leave media analysis with unsaved changes or an unconfirmed run request? Your run receipt will be retained.');
  useEffect(() => () => mutation.current?.abort(), []);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setLoadError(undefined);
    void Promise.all([mediaAnalysisApi.overview({ signal: controller.signal }), adminApi.getLibraries({ signal: controller.signal })]).then(([value, list]) => {
      if (controller.signal.aborted) return;
      if (!list || !Array.isArray(list.Items) || !list.Items.every((item) => typeof item.Id === 'string' && item.Id !== '' && typeof item.Name === 'string')) throw new ApiError('The library list is incomplete. Reload to view library results.', { code: 'invalid_response' });
      setOverview(value); setLibraries(list.Items);
      if (!preserveDraft.current) setDraft(analysisDraft(value.Configuration.Profile));
      setReloadRequired(false); setError(undefined);
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setLoadError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [loadRevision]);
  function reload(keepDraft: boolean) { preserveDraft.current = keepDraft; setLoadRevision((value) => value + 1); setResultsRevision((value) => value + 1); }
  const loadRun = useCallback(async (signal: AbortSignal) => {
    const value = await adminApi.getTaskRun(receipt!.RunId, { Limit: 25 }, { signal });
    if (value.Run.TaskId !== receipt!.TaskId) throw new ApiError('The task response belongs to another analysis task. Reload its recorded status.', { code: 'invalid_response' });
    return value;
  }, [receipt]);
  const run = useTaskResource({ key: receipt?.RunId ?? 'none', load: loadRun, poll: activeRun, enabled: Boolean(receipt) && busy !== 'stop' });
  useEffect(() => {
    if (!run.data || isActiveTaskRun(run.data.Run) || finished.current === run.data.Run.Id) return;
    finished.current = run.data.Run.Id; preserveDraft.current = true; setLoadRevision((value) => value + 1); setResultsRevision((value) => value + 1);
  }, [run.data]);
  async function save() {
    if (!overview || !parsed?.profile || !dirty || blocked || mutation.current) return;
    const controller = new AbortController(); mutation.current = controller; setBusy('save'); setError(undefined); setNotice('');
    try {
      const value = await mediaAnalysisApi.configure(overview.Configuration.Revision, parsed.profile, { signal: controller.signal });
      if (!controller.signal.aborted) { setOverview({ ...overview, Configuration: value }); setDraft(analysisDraft(value.Profile)); setNotice('Analysis configuration saved. New runs use this profile.'); reload(false); }
    } catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) { setError(cause); setReloadRequired(unknownResult(cause)); } }
    finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(undefined); }
  }
  async function checkRequest() {
    if (mutation.current || blocked || dirty || !pending) return;
    // Retain the exact receipt from the previous manual workflow. This path
    // never creates a new request ID or changes the selected media or policy.
    const input = pending;
    const controller = new AbortController(); mutation.current = controller; remember(currentUserId, input); setPending(input); setBusy('start'); setError(undefined); setNotice('');
    try {
      const value = await mediaAnalysisApi.start(input, { signal: controller.signal });
      if (!controller.signal.aborted) { setReceipt(value); remember(currentUserId); setPending(undefined); setRunError(undefined); setNotice(value.Admitted ? 'Run admitted. Progress below comes from the task service.' : 'The server returned an existing run. Its recorded progress is shown below.'); }
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) { setError(cause); if (!unknownResult(cause)) { remember(currentUserId); setPending(undefined); } }
    } finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(undefined); }
  }
  async function stop() {
    if (!receipt || !run.data || !isActiveTaskRun(run.data.Run) || locked || mutation.current || run.error || run.loading) return;
    const controller = new AbortController(); mutation.current = controller; setBusy('stop'); setRunError(undefined);
    try { await adminApi.cancelTaskRun(receipt.RunId, { signal: controller.signal }); if (!controller.signal.aborted) run.reload(); }
    catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) setRunError(cause); }
    finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(undefined); }
  }
  async function prune() {
    if (!overview || blocked || dirty || mutation.current) return;
    const controller = new AbortController(); mutation.current = controller; setBusy('prune'); setError(undefined); setPruned(undefined);
    try { const value = await mediaAnalysisApi.prune(overview.Configuration.Revision, { signal: controller.signal }); if (!controller.signal.aborted) { setPruned(value); setPruning(false); reload(true); } }
    catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) { setPruning(false); setError(cause); setReloadRequired(unknownResult(cause)); } }
    finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(undefined); }
  }
  const cache = overview?.Runtime.Cache;
  const cachePercent = cache && cache.MaxBytes > 0 ? Math.min(100, cache.TotalBytes / cache.MaxBytes * 100) : 0;
  return <Box>
    <PageHeading title="Media analysis" description="View automatic intro detection, seek previews, and background task progress." action={<Button variant="outlined" startIcon={<PlaylistPlayRounded />} onClick={onTasks}>Open tasks</Button>} />
    <Stack spacing={2.5}>
      {loadError != null && <ErrorNotice error={loadError} retry={() => reload(Boolean(draft))} />}
      {error != null && <ErrorNotice error={error} />}
      {reloadRequired && <Alert severity="warning">The change could not be confirmed or the configuration changed. Your draft is preserved. Reload the latest revision, review your draft, and then save again.<Button color="inherit" disabled={loading || locked} onClick={() => reload(true)}>Reload latest and keep draft</Button></Alert>}
      {notice && <Alert severity="success">{notice}</Alert>}
      {pruned && <Alert severity="success">Removed {pruned.RemovedEntries} entries ({analysisBytes(pruned.RemovedBytes)}). {analysisBytes(pruned.RemainingBytes)} remains; {pruned.BusyEntries} busy entries were retained.</Alert>}
      {loading && !overview && <Box role="status" aria-label="Loading media analysis"><Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 2 }}><Skeleton variant="rounded" height={156} /><Skeleton variant="rounded" height={156} /></Box><Skeleton height={140} /></Box>}
      {overview && draft && <>
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 2 }}>
          <Paper component="section" aria-labelledby="analysis-runtime-title" elevation={0} sx={{ p: 2.5, borderRadius: '20px', bgcolor: mediaSurface }}>
            <Stack spacing={1.5}>
              <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1 }}>
                <Typography component="h2" variant="h3" id="analysis-runtime-title">Availability</Typography>
                <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
                  <Button size="small" onClick={() => setConfigurationOpen(true)} disabled={locked}>Configure{dirty ? ' •' : ''}</Button>
                  <Tooltip title="Refresh analysis status"><span><IconButton size="small" aria-label="Refresh analysis status" disabled={locked || loading} onClick={() => reload(true)}><RefreshRounded sx={{ fontSize: 18 }} /></IconButton></span></Tooltip>
                </Stack>
              </Stack>
              <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}>
                <AvailabilityChip available={overview.Runtime.Configured} label={overview.Runtime.Configured ? 'Processing configured' : 'Processing disabled'} />
                <AvailabilityChip available={overview.Runtime.IntroAvailable} label={overview.Runtime.IntroAvailable ? 'Intro analysis available' : 'Intro analysis unavailable'} />
                <AvailabilityChip available={overview.Runtime.PreviewAvailable} label={overview.Runtime.PreviewAvailable ? 'Previews available' : 'Previews unavailable'} />
              </Stack>
              <Typography variant="caption" color="text.secondary">Configuration revision {overview.Configuration.Revision}. Saved values apply to new work; existing jobs retain their profile.</Typography>
              {overview.Runtime.Reasons.length > 0 && <Alert severity="info"><Box component="ul" sx={{ m: 0, pl: 2 }}>{overview.Runtime.Reasons.map((reason, index) => <li key={`${reason}-${index}`}>{reason.replaceAll('_', ' ')}</li>)}</Box></Alert>}
            </Stack>
          </Paper>
          <Paper component="section" aria-labelledby="analysis-cache-title" elevation={0} sx={{ p: 2.5, borderRadius: '20px', bgcolor: mediaSurface }}>
            <Stack spacing={1}>
              <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1 }}>
                <Typography component="h2" variant="h3" id="analysis-cache-title">Analysis cache</Typography>
                <Button size="small" disabled={blocked || dirty || !cache} onClick={() => setPruning(true)}>Manage cache</Button>
              </Stack>
              {cache ? <>
                <Typography component="p" sx={{ fontSize: 24, lineHeight: 1.35, fontWeight: 600, fontVariantNumeric: 'tabular-nums' }}>{analysisBytes(cache.TotalBytes)}<Typography component="span" sx={{ ml: 1, fontSize: 13, fontWeight: 400, color: 'text.secondary' }}>/ {analysisBytes(cache.MaxBytes)}</Typography></Typography>
                <LinearProgress variant="determinate" value={cachePercent} aria-label="Analysis cache usage" aria-valuetext={`${analysisBytes(cache.TotalBytes)} used of ${analysisBytes(cache.MaxBytes)}`} color={cachePercent >= 85 ? 'error' : cachePercent >= 75 ? 'warning' : 'primary'} sx={{ height: 6, borderRadius: 3, bgcolor: '#DCE4F2' }} />
                <Typography variant="caption" color="text.secondary">Ready {cache.ReadyEntries.toLocaleString()} ({analysisBytes(cache.ReadyBytes)}) · Building {cache.BuildingEntries.toLocaleString()} · Pending {cache.PendingPublications.toLocaleString()}</Typography>
              </> : <Typography variant="body2" color="text.secondary">Cache accounting is unavailable while this runtime is not configured.</Typography>}
            </Stack>
          </Paper>
        </Box>
        <Paper component="section" aria-labelledby="analysis-automation-title" variant="outlined" sx={{ ...mediaPanelSx, p: 2.5 }}>
          <Stack direction={{ xs: 'column', md: 'row' }} sx={{ alignItems: { xs: 'stretch', md: 'center' }, gap: 2 }}>
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Typography component="h2" variant="h3" id="analysis-automation-title">Automatic media processing</Typography>
              <Box component="section" aria-labelledby="analysis-intro-title" sx={{ mt: 1.5 }}>
                <Typography component="h3" variant="body1" id="analysis-intro-title" sx={{ fontWeight: 600 }}>Automatic intro detection</Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>Enable Automatic intro detection in a TV library's settings. Episodes are analyzed in the background and reliable matches become available for playback. If no intro is found, playback stays unchanged.</Typography>
              </Box>
              <Box component="section" aria-labelledby="analysis-preview-title" sx={{ mt: 1.5 }}>
                <Typography component="h3" variant="body1" id="analysis-preview-title" sx={{ fontWeight: 600 }}>Automatic seek previews</Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>Enable Automatic seek previews in a Movies, TV shows, or Mixed media library. Preview images are generated in the background for seeking in compatible players. Turning the option off keeps existing valid previews.</Typography>
              </Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>View progress, failures, and stop running work in Tasks.</Typography>
            </Box>
            <Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap' }}><Button variant="contained" onClick={onLibraries}>Open library settings</Button><Button variant="outlined" onClick={onTasks}>View background tasks</Button></Stack>
          </Stack>
        </Paper>
        {(pending || receipt) && <Paper component="section" aria-labelledby="analysis-run-title" variant="outlined" sx={{ ...mediaPanelSx, p: 2.5 }}><Stack spacing={2}>
          <Typography component="h2" variant="h3" id="analysis-run-title">Previous run request</Typography>
          {pending && <Alert severity="warning">A previous {pending.Kind === 'intro' ? 'intro analysis' : 'preview generation'} request is unconfirmed. Check that original request to recover its recorded result.<Button disabled={blocked || dirty} color="inherit" onClick={() => void checkRequest()}>Check run request</Button></Alert>}
          {receipt && <Box role="region" aria-label="Analysis task progress" sx={{ p: 2, borderRadius: '12px', bgcolor: mediaSurface }}><Stack spacing={1.5}><Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>Run {receipt.RunId}</Typography>{run.loading && !run.data && <Typography role="status">Loading recorded task progress...</Typography>}{run.error != null && <ErrorNotice error={run.error} retry={run.reload} />}{runError != null && <ErrorNotice error={runError} retry={() => { setRunError(undefined); run.reload(); }} />}{run.data && <><RunStatusChip state={run.data.Run.State} /><RunProgress run={run.data.Run} />{run.data.Run.State === 'stopping' && <Alert severity="info">Stop requested. The task remains active until its workers finish.</Alert>}{run.data.Run.ErrorMessage && <Alert severity="error">{run.data.Run.ErrorMessage}</Alert>}{run.data.Children.Items.filter((child) => child.ErrorCode || child.ErrorMessage).map((child) => <Alert severity="warning" key={child.Id}>{child.LibraryName || 'Work item'}: {child.ErrorMessage || child.ErrorCode}</Alert>)}</>}<Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}><Button disabled={locked || run.loading || !run.data || !isActiveTaskRun(run.data.Run) || run.data.Run.State === 'stopping' || runError != null} onClick={() => void stop()}>Request stop</Button><Button disabled={locked || run.loading} onClick={() => { setRunError(undefined); run.reload(); }}>Refresh progress</Button><Button onClick={onTasks}>View tasks</Button></Stack></Stack></Box>}
        </Stack></Paper>}
        <MediaAnalysisResults libraries={libraries} disabled={blocked} refresh={resultsRevision} />
        <Dialog open={configurationOpen} onClose={() => { if (!locked) setConfigurationOpen(false); }} fullWidth maxWidth="md" aria-labelledby="analysis-configuration-title">
          <DialogTitle id="analysis-configuration-title">Analysis configuration</DialogTitle>
          <DialogContent>
            <Box component="form" id="analysis-configuration-form" onSubmit={(event) => { event.preventDefault(); void save(); }}>
              <Stack spacing={2.5} sx={{ pt: 0.5 }}>
                <Typography variant="body2" color="text.secondary">Revision {overview.Configuration.Revision}. Saved values apply to newly admitted work; existing jobs retain their profile. Configuration remains editable when processing is unavailable.</Typography>
                {error != null && <ErrorNotice error={error} />}
                {reloadRequired && <Alert severity="warning">The change could not be confirmed or the configuration changed. Your draft is preserved.<Button color="inherit" disabled={loading || locked} onClick={() => reload(true)}>Reload latest and keep draft</Button></Alert>}
                {notice && <Alert severity="success">{notice}</Alert>}
                <Typography variant="body2" color="text.secondary">Library settings control automatic intro detection and seek previews. These processing limits and preview settings apply to new work.</Typography>
                <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 2.5 }}>{analysisNumberFields.map((field) => <TextField key={field.key} label={field.label} value={draft[field.key]} disabled={blocked} onChange={(event) => setDraft({ ...draft, [field.key]: event.target.value })} error={Boolean(parsed?.errors[field.key])} helperText={parsed?.errors[field.key] ?? field.help} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 16, autoComplete: 'off' } }} />)}</Box>
                <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}><Button disabled={blocked} onClick={() => setDraft(analysisDraft(overview.Configuration.Defaults))}>Use default profile</Button><Button disabled={locked || loading} onClick={() => { setDraft(analysisDraft(overview.Configuration.Profile)); setNotice('Draft discarded. The last confirmed profile is shown.'); }}>Discard draft</Button></Stack>
                {dirty && <Typography role="status" variant="caption" color="text.secondary">The configuration has unsaved changes. Closing this dialog keeps the draft.</Typography>}
              </Stack>
            </Box>
          </DialogContent>
          <DialogActions><Button disabled={locked} onClick={() => setConfigurationOpen(false)}>Close</Button><Button type="submit" form="analysis-configuration-form" variant="contained" disabled={blocked || !dirty || !parsed?.profile} startIcon={busy === 'save' ? <CircularProgress size={16} color="inherit" /> : undefined}>Save analysis configuration</Button></DialogActions>
        </Dialog>
      </>}
    </Stack>
    <Dialog open={pruning} onClose={() => { if (!locked) setPruning(false); }} fullWidth maxWidth="sm" aria-labelledby="analysis-prune-title">
      <DialogTitle id="analysis-prune-title">Prune the analysis cache?</DialogTitle>
      <DialogContent><Stack spacing={2}>{cache && <Box sx={{ p: 2, bgcolor: 'background.paper', borderRadius: '12px' }}><Typography variant="body2">{analysisBytes(cache.TotalBytes)} used of {analysisBytes(cache.MaxBytes)}</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>Active readers: {cache.Readers} · Reserved: {analysisBytes(cache.ReservedBytes)} · Control data: {analysisBytes(cache.ControlBytes)}</Typography></Box>}<Typography>Remove eligible unused entries under the current saved configuration. Busy readers, builds, and publications may retain entries. Original media is unchanged.</Typography></Stack></DialogContent>
      <DialogActions><Button disabled={locked} onClick={() => setPruning(false)}>Cancel</Button><Button variant="contained" disabled={blocked || dirty} onClick={() => void prune()}>Prune eligible entries</Button></DialogActions>
    </Dialog>
  </Box>;
}
