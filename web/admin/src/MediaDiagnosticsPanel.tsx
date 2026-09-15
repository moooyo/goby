import { useCallback, useEffect, useRef, useState } from 'react';
import { Accordion, AccordionDetails, AccordionSummary, Alert, Box, Button, Chip, CircularProgress, FormControl, FormControlLabel, FormLabel, Paper, Radio, RadioGroup, Skeleton, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, Typography } from '@mui/material';
import CancelOutlined from '@mui/icons-material/CancelOutlined';
import ExpandMoreRounded from '@mui/icons-material/ExpandMoreRounded';
import PlayArrowRounded from '@mui/icons-material/PlayArrowRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import ScienceOutlined from '@mui/icons-material/ScienceOutlined';
import { adminApi, ApiError, isAbortError } from './api';
import type { MediaDiagnosticMode, MediaDiagnosticReport, MediaDiagnosticRunDetail, MediaDiagnosticRunResponse, MediaDiagnosticRunState, MediaDiagnosticRunSummary, MediaDiagnosticStage, MediaDiagnosticStageMode, MediaDiagnosticsResponse } from './api';
import { ErrorNotice } from './components';
import { useTaskResource } from './useTaskResource';

interface RunReference { InstanceId: string; RequestId: string; Mode: MediaDiagnosticMode }
interface LookupProblem { key: string; kind: 'missing' | 'instance_changed'; generation: number }
interface RetainedFact { summary: MediaDiagnosticRunSummary; detailRevision?: string; sequence: number }
const activeStates: MediaDiagnosticRunState[] = ['queued', 'running', 'cancelling', 'cleanup_pending'];
const stageModes: MediaDiagnosticStageMode[] = ['decode', 'encode', 'combined'];
const stageNames = { decode: 'Decode', encode: 'Encode', combined: 'Decode and encode' };
const stateNames: Record<string, string> = {
  queued: 'Queued', running: 'Running', cancelling: 'Cancelling', cleanup_pending: 'Releasing resources',
  passed: 'Passed', failed: 'Failed', cancelled: 'Cancelled', unavailable: 'Unavailable', unverified: 'Unverified', not_run: 'Not run',
};
const isActive = (run: MediaDiagnosticRunSummary) => activeStates.includes(run.State);
const pollRun = (result: MediaDiagnosticRunResponse) => isActive(result.Run);
const referenceKey = (value?: RunReference) => value ? `${value.InstanceId}/${value.RequestId}` : '';
const runReference = (run: MediaDiagnosticRunSummary): RunReference => ({ InstanceId: run.InstanceId, RequestId: run.Id, Mode: run.Mode });
const memoryPending = new Map<string, RunReference>();

function readPending(key: string): RunReference | undefined {
  const retained = memoryPending.get(key);
  if (retained) return retained;
  try {
    const raw = sessionStorage.getItem(key);
    if (!raw || raw.length > 256) return undefined;
    const value: unknown = JSON.parse(raw);
    if (typeof value !== 'object' || value === null || Array.isArray(value)) return undefined;
    const fields = value as Record<string, unknown>;
    if (Object.keys(fields).length !== 3 || typeof fields.InstanceId !== 'string' || !/^[0-9a-f]{32}$/.test(fields.InstanceId)
      || typeof fields.RequestId !== 'string' || !/^[0-9a-f]{32}$/.test(fields.RequestId)
      || !['software', 'configured'].includes(fields.Mode as string)) return undefined;
    const reference = fields as unknown as RunReference;
    memoryPending.set(key, reference);
    return reference;
  } catch { return undefined; }
}

function requestId(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (value) => value.toString(16).padStart(2, '0')).join('');
}

function uncertainStart(error: unknown): boolean {
  // A session change after POST returns does not prove that admission failed.
  return !(error instanceof ApiError) || ['network_error', 'invalid_response', 'session_changed'].includes(error.code) || error.status >= 500;
}

function displayTime(value?: string | null): string {
  if (!value || value.startsWith('0001-01-01T00:00:00')) return 'Not recorded';
  return new Date(value).toLocaleString();
}

function StateChip({ state }: { state: string }) {
  const color = state === 'passed' ? 'success' : state === 'failed' ? 'error'
    : ['unverified', 'unavailable', 'cleanup_pending'].includes(state) ? 'warning' : 'default';
  return <Chip size="small" variant="outlined" color={color} label={stateNames[state] ?? 'Not reported'} />;
}

async function loadRun(reference: RunReference, signal: AbortSignal): Promise<MediaDiagnosticRunResponse> {
  const result = await adminApi.getMediaDiagnosticRun(reference.InstanceId, reference.RequestId, { signal });
  if (result.Run.Mode !== reference.Mode) throw new ApiError('The run details do not match the selected diagnostic.', { code: 'invalid_response' });
  return result;
}

function StageResult({ stage }: { stage: MediaDiagnosticStage }) {
  const evidence = stage.Command?.Evidence;
  const elapsed = (stage.Command?.Elapsed ?? 0) + (stage.Verification?.Elapsed ?? 0);
  const content = stage.Video ?? stage.Audio;
  const video = stage.Video?.ReferenceSHA256 && stage.Video.Frames > 0 ? stage.Video : undefined;
  const audio = stage.Audio?.ReferenceSHA256 && stage.Audio.SampleFrames > 0 ? stage.Audio : undefined;
  return <Paper variant="outlined" sx={{ p: 2, minWidth: 0 }}>
    <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
      <Typography variant="h4" component="h4">{stageNames[stage.Mode]}</Typography><StateChip state={stage.State} />
    </Stack>
    <Box component="dl" sx={{ m: 0, mt: 1.5, display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr)', gap: 1.5 }}>
      {[
        [stage.Mode === 'encode' ? 'Input codec' : 'Observed decoder', evidence?.Decoder || 'Not recorded'],
        [stage.Mode === 'decode' ? 'Output codec' : 'Observed encoder', evidence?.Encoder || 'Not recorded'],
        ['Command time', stage.Command?.Started ? `${(elapsed / 1_000_000_000).toFixed(2)} s` : 'Not recorded'],
        ['Content checks', content ? stage.State === 'passed' && (video || audio) ? 'Passed' : 'Not confirmed' : 'Not recorded'],
      ].map(([label, value]) => <Box key={label} sx={{ minWidth: 0 }}><Typography component="dt" variant="caption" color="text.secondary">{label}</Typography><Typography component="dd" variant="body2" sx={{ m: 0, overflowWrap: 'anywhere' }}>{value}</Typography></Box>)}
    </Box>
    {evidence?.HardwarePixelFormat && <Typography variant="body2" sx={{ mt: 1.5 }}>Decoder format reported by FFmpeg: {evidence.HardwarePixelFormat}</Typography>}
    {stage.Code && <Typography variant="body2" color="text.secondary" sx={{ mt: 1.5, overflowWrap: 'anywhere' }}>{stage.Code.replaceAll('_', ' ')}</Typography>}
    {(stage.Video || stage.Audio || stage.Verification?.Evidence) && <Accordion disableGutters elevation={0} sx={{ mt: 1, '&::before': { display: 'none' } }}>
      <AccordionSummary expandIcon={<ExpandMoreRounded />} sx={{ px: 0 }}><Typography variant="body2">Content and output details</Typography></AccordionSummary>
      <AccordionDetails sx={{ px: 0, pt: 0 }}><Stack spacing={1}>
        {video && <>
          <Typography variant="body2">{video.Frames} frames · {video.Width} × {video.Height} · {video.PixelFormat || 'Format not recorded'}</Typography>
          <Typography variant="body2">Highest frame-plane error: {Math.max(...video.FrameMetrics.flatMap((frame) => frame.Planes.map((plane) => plane.MeanAbsoluteError))).toFixed(2)} mean absolute; {Math.max(...video.FrameMetrics.flatMap((frame) => frame.Planes.map((plane) => plane.RootMeanSquareError))).toFixed(2)} RMS.</Typography>
        </>}
        {audio && <>
          <Typography variant="body2">{audio.SampleFrames.toLocaleString()} sample frames · {audio.SampleRate.toLocaleString()} Hz · {audio.Channels} channels</Typography>
          <Typography variant="body2">Channel normalized RMS errors: {audio.NormalizedRMSE.map((value) => value.toFixed(4)).join(' / ')}.</Typography>
        </>}
        {!video && !audio && <Typography variant="body2">Comparable content measurements were not produced.</Typography>}
        {stage.Verification?.Evidence && <Typography variant="body2">Output check decoder: {stage.Verification.Evidence.Decoder}. This software check is separate from the diagnostic's observed processing path.</Typography>}
        <Typography variant="caption" color="text.secondary">These checks cover this fixed sample only. They do not establish support for other media or hardware.</Typography>
      </Stack></AccordionDetails>
    </Accordion>}
  </Paper>;
}

function ReportResults({ report, runState }: { report: MediaDiagnosticReport; runState?: MediaDiagnosticRunState }) {
  const groups = [{ path: 'software', media: 'video', label: 'Software video baseline' }, { path: 'software', media: 'audio', label: 'Software audio baseline' },
    ...(report.Selection.IncludeConfiguredHardware ? [{ path: 'configured', media: 'video', label: 'Configured video path' }] : [])];
  return <Stack spacing={2.5}>
    {report.SessionClosureRequired && (runState === 'cleanup_pending' || ['stages_complete', 'incomplete'].includes(report.State))
      && <Alert severity="warning">Stage checks have finished, but the run has not finished releasing its resources.</Alert>}
    <Typography variant="body2">Observed FFmpeg version: <strong>{report.ToolVersion || 'Not recorded'}</strong> · Started: {displayTime(report.StartedAt)} · Finished: {displayTime(report.FinishedAt)}</Typography>
    {groups.map((group) => <Box component="section" key={`${group.path}-${group.media}`} aria-label={group.label}>
      <Typography variant="h4" component="h3" sx={{ mb: 1.5 }}>{group.label}</Typography>
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', lg: 'repeat(3, minmax(0, 1fr))' }, gap: 1.5 }}>
        {stageModes.map((mode) => report.Stages.find((stage) => stage.Path === group.path && stage.Media === group.media && stage.Mode === mode))
          .map((stage) => stage && <StageResult key={stage.ID} stage={stage} />)}
      </Box>
    </Box>)}
  </Stack>;
}

export function MediaDiagnosticsPanel({ currentUserId, startBlocked, onBusyChange }: { currentUserId: string; startBlocked: boolean; onBusyChange: (busy: boolean) => void }) {
  const storageKey = `goby.media-diagnostics.pending.${encodeURIComponent(currentUserId)}`;
  const [pending, setPending] = useState<RunReference | undefined>(() => readPending(storageKey));
  const pendingRef = useRef(pending);
  const [selection, setSelection] = useState<RunReference | undefined>(pending);
  const [awaitingLookup, setAwaitingLookup] = useState(Boolean(pending));
  const [index, setIndex] = useState<MediaDiagnosticsResponse>();
  const indexRef = useRef<MediaDiagnosticsResponse | undefined>(undefined);
  const facts = useRef(new Map<string, RetainedFact>());
  const detailSequence = useRef(0);
  const [indexLoading, setIndexLoading] = useState(true);
  const [indexError, setIndexError] = useState<unknown>();
  const [indexFresh, setIndexFresh] = useState(false);
  const [generation, setGeneration] = useState(0);
  const generationRef = useRef(0);
  const [mode, setMode] = useState<MediaDiagnosticMode>(pending?.Mode ?? 'software');
  const [mutation, setMutation] = useState<'start' | 'cancel'>();
  const mutationRef = useRef<AbortController | undefined>(undefined);
  const indexRequest = useRef<AbortController | undefined>(undefined);
  const [startError, setStartError] = useState<unknown>();
  const [cancelError, setCancelError] = useState<unknown>();
  const [lastResult, setLastResult] = useState<{ key: string; run: MediaDiagnosticRunDetail }>();
  const lastResultRef = useRef<{ key: string; run: MediaDiagnosticRunDetail } | undefined>(undefined);
  const [lastReport, setLastReport] = useState<{ key: string; report: MediaDiagnosticReport }>();
  const [problem, setProblem] = useState<LookupProblem>();
  const [pendingIssue, setPendingIssue] = useState<LookupProblem>();
  const [expired, setExpired] = useState(true);
  const alive = useRef(true);
  const selectedKey = referenceKey(selection);
  const pendingKey = referenceKey(pending);
  const instanceChanged = Boolean(selection && index && selection.InstanceId !== index.InstanceId);
  const active = index?.Items.find(isActive);
  const activeReference = active ? runReference(active) : undefined;
  const activeKey = referenceKey(activeReference);

  const keepPending = useCallback((value?: RunReference) => {
    pendingRef.current = value;
    setPending(value);
    if (value) memoryPending.set(storageKey, value);
    else { memoryPending.delete(storageKey); setPendingIssue(undefined); }
    try { if (value) sessionStorage.setItem(storageKey, JSON.stringify(value)); else sessionStorage.removeItem(storageKey); }
    catch { /* The in-memory receipt survives in-app navigation without retaining a start token. */ }
  }, [storageKey]);

  const publishIndex = useCallback((value: MediaDiagnosticsResponse) => { indexRef.current = value; setIndex(value); }, []);
  const rememberRun = useCallback((run: MediaDiagnosticRunDetail): boolean => {
    const key = referenceKey(runReference(run));
    const previous = facts.current.get(key);
    const current = indexRef.current;
    const visible = current?.InstanceId === run.InstanceId ? current.Items.find((item) => item.Id === run.Id) : undefined;
    // Server revisions order facts; wall-clock timestamps only sort the history.
    if ((previous && BigInt(previous.summary.Revision) > BigInt(run.Revision))
      || (visible && BigInt(visible.Revision) > BigInt(run.Revision))) return false;
    const summary: MediaDiagnosticRunSummary = { Id: run.Id, InstanceId: run.InstanceId, Revision: run.Revision, Mode: run.Mode, State: run.State,
      Code: run.Code, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt, FinishedAt: run.FinishedAt };
    const changed = !previous?.detailRevision || BigInt(run.Revision) > BigInt(previous.detailRevision);
    facts.current.set(key, { summary, detailRevision: run.Revision, sequence: changed ? ++detailSequence.current : previous?.sequence ?? 0 });
    if (current?.InstanceId === run.InstanceId) publishIndex({
      ...current, Items: [summary, ...current.Items.filter((item) => item.Id !== run.Id)]
        .sort((left, right) => Date.parse(right.CreatedAt) - Date.parse(left.CreatedAt)).slice(0, 32),
    });
    if (facts.current.size > 32) {
      const oldest = [...facts.current.entries()].sort((left, right) => left[1].sequence - right[1].sequence)[0];
      if (oldest) facts.current.delete(oldest[0]);
    }
    return true;
  }, [publishIndex]);

  const refreshIndex = useCallback(async () => {
    if (indexRequest.current || mutationRef.current) return;
    const controller = new AbortController();
    const startedAfter = detailSequence.current;
    indexRequest.current = controller;
    setIndexLoading(true);
    setIndexError(undefined);
    try {
      const result = await adminApi.getMediaDiagnostics({ signal: controller.signal });
      if (!alive.current || controller.signal.aborted) return;
      generationRef.current += 1;
      setGeneration(generationRef.current);
      const merged = new Map<string, MediaDiagnosticRunSummary>();
      for (const row of result.Items) {
        const key = referenceKey(runReference(row));
        const known = facts.current.get(key);
        const summary = known && BigInt(known.summary.Revision) >= BigInt(row.Revision) ? known.summary : row;
        merged.set(key, summary);
        facts.current.set(key, { summary, detailRevision: known?.detailRevision, sequence: known?.sequence ?? 0 });
      }
      for (const [key, known] of facts.current) {
        if (known.summary.InstanceId !== result.InstanceId) { facts.current.delete(key); continue; }
        if (!merged.has(key)) {
          // A concurrent detail update may outlive this index snapshot, but a
          // later index can expire the row when no newer detail arrived.
          if (known.sequence > startedAfter && known.detailRevision === known.summary.Revision) merged.set(key, known.summary);
          else facts.current.delete(key);
        }
      }
      const items = [...merged.values()].sort((left, right) => Date.parse(right.CreatedAt) - Date.parse(left.CreatedAt)).slice(0, 32);
      const retained = new Set(items.map((item) => referenceKey(runReference(item))));
      for (const key of facts.current.keys()) if (!retained.has(key)) facts.current.delete(key);
      publishIndex({ ...result, Items: items });
      setIndexFresh(true);
      setSelection((current) => current ?? pendingRef.current ?? (items[0] ? runReference(items.find(isActive) ?? items[0]) : undefined));
    } catch (cause) {
      if (alive.current && !controller.signal.aborted && !isAbortError(cause)) { setIndexError(cause); setIndexFresh(false); }
    } finally {
      if (indexRequest.current === controller) indexRequest.current = undefined;
      if (alive.current && !controller.signal.aborted) setIndexLoading(false);
    }
  }, [publishIndex]);

  useEffect(() => {
    alive.current = true;
    void refreshIndex();
    return () => { alive.current = false; indexRequest.current?.abort(); indexRequest.current = undefined; mutationRef.current?.abort(); onBusyChange(false); };
  }, [refreshIndex, onBusyChange]);
  useEffect(() => {
    const remaining = index ? Date.parse(index.StartTokenExpiresAt) - Date.now() : 0;
    setExpired(remaining <= 0 || !index?.StartToken);
    if (remaining <= 0) return;
    const timer = setTimeout(() => setExpired(true), Math.min(remaining + 1, 2_147_483_647));
    return () => clearTimeout(timer);
  }, [index?.StartToken, index?.StartTokenExpiresAt]);
  useEffect(() => {
    if (instanceChanged && selectedKey) setProblem({ key: selectedKey, kind: 'instance_changed', generation: generationRef.current - 1 });
  }, [instanceChanged, selectedKey]);
  useEffect(() => {
    if (pending && index && pending.InstanceId !== index.InstanceId) {
      setPendingIssue({ key: pendingKey, kind: 'instance_changed', generation: generationRef.current - 1 });
    }
  }, [pendingKey, pending?.InstanceId, index?.InstanceId]);

  const selectedInstance = selection?.InstanceId ?? '';
  const selectedId = selection?.RequestId ?? '';
  const selectedMode = selection?.Mode ?? 'software';
  const selectedLoad = useCallback((signal: AbortSignal) => loadRun({ InstanceId: selectedInstance, RequestId: selectedId, Mode: selectedMode }, signal), [selectedInstance, selectedId, selectedMode]);
  const selectedResource = useTaskResource({ key: selectedKey, load: selectedLoad, poll: pollRun,
    enabled: Boolean(selection) && !mutation && !instanceChanged && !(selectedKey === pendingKey && awaitingLookup && activeKey !== selectedKey) && cancelError == null });
  const activeInstance = activeReference?.InstanceId ?? '';
  const activeId = activeReference?.RequestId ?? '';
  const activeMode = activeReference?.Mode ?? 'software';
  const activeLoad = useCallback((signal: AbortSignal) => loadRun({ InstanceId: activeInstance, RequestId: activeId, Mode: activeMode }, signal), [activeInstance, activeId, activeMode]);
  const activeResource = useTaskResource({ key: activeKey, load: activeLoad, poll: pollRun,
    enabled: Boolean(activeReference) && activeKey !== selectedKey && !mutation });

  const acceptResult = useCallback((run: MediaDiagnosticRunDetail) => {
    const key = referenceKey(runReference(run));
    const previous = lastResultRef.current;
    if (previous?.key === key && BigInt(previous.run.Revision) > BigInt(run.Revision)) return;
    if (!rememberRun(run)) return;
    lastResultRef.current = { key, run };
    setLastResult({ key, run });
    if (run.Report) setLastReport({ key, report: run.Report });
    setProblem((current) => current?.key === key ? undefined : current);
    if (referenceKey(pendingRef.current) === key) { setAwaitingLookup(false); setPendingIssue(undefined); setStartError(undefined); }
  }, [rememberRun]);
  useEffect(() => { if (selectedResource.data) acceptResult(selectedResource.data.Run); }, [selectedResource.data, acceptResult]);
  useEffect(() => { if (activeResource.data) rememberRun(activeResource.data.Run); }, [activeResource.data, rememberRun]);
  useEffect(() => {
    if (pending && lastResult?.key === pendingKey && !awaitingLookup) keepPending(undefined);
  }, [pending, lastResult, pendingKey, awaitingLookup, keepPending]);
  useEffect(() => {
    const error = selectedResource.error;
    if (!error) return;
    setIndexFresh(false);
    if (error instanceof ApiError && (error.status === 404 || error.code === 'instance_changed')) {
      const issue: LookupProblem = { key: selectedKey, kind: error.code === 'instance_changed' ? 'instance_changed' : 'missing', generation: generationRef.current };
      setProblem(issue);
      if (referenceKey(pendingRef.current) === selectedKey) setPendingIssue(issue);
    }
  }, [selectedResource.error, selectedKey]);
  useEffect(() => { if (activeResource.error) setIndexFresh(false); }, [activeResource.error]);

  const run = lastResult?.key === selectedKey ? lastResult.run : undefined;
  const listedRun = index?.InstanceId === selection?.InstanceId ? index?.Items.find((item) => item.Id === selection?.RequestId) : undefined;
  const detailsOutdated = Boolean(run && listedRun && BigInt(listedRun.Revision) > BigInt(run.Revision));
  const report = run?.Report ?? (lastReport?.key === selectedKey ? lastReport.report : undefined);
  const selectedProblem = problem?.key === selectedKey ? problem : undefined;
  const pendingProblem = pendingIssue?.key === pendingKey ? pendingIssue : undefined;
  const canReplaceUnknown = Boolean(pending && pendingProblem && generation > pendingProblem.generation && indexFresh);
  const retentionFull = Boolean(index && index.Items.length >= index.MaxRetainedRuns);
  const canStart = Boolean(index?.Available && indexFresh && !indexLoading && !expired && !active && !retentionFull && !mutation && !startBlocked
    && (mode === 'software' || index.HardwareConfigured) && (!pending || canReplaceUnknown));

  function refreshRun() {
    if (mutationRef.current || instanceChanged) return;
    setCancelError(undefined);
    setAwaitingLookup(false);
    selectedResource.reload();
  }

  async function start() {
    if (!canStart || !index || mutationRef.current) return;
    if (Date.parse(index.StartTokenExpiresAt) <= Date.now()) { setExpired(true); return; }
    const reference: RunReference = { InstanceId: index.InstanceId, RequestId: requestId(), Mode: mode };
    const controller = new AbortController();
    mutationRef.current = controller;
    keepPending(reference);
    setPendingIssue(undefined);
    setMutation('start'); onBusyChange(true); setStartError(undefined); setCancelError(undefined);
    try {
      const result = await adminApi.startMediaDiagnostic({ ...reference, StartToken: index.StartToken }, { signal: controller.signal });
      if (!alive.current || controller.signal.aborted) return;
      setSelection(reference); setAwaitingLookup(false); acceptResult(result.Run);
    } catch (cause) {
      if (!alive.current || controller.signal.aborted || isAbortError(cause)) return;
      setStartError(cause);
      if (uncertainStart(cause)) { setSelection(reference); setAwaitingLookup(true); setProblem(undefined); }
      else { keepPending(undefined); setIndexFresh(false); }
    } finally {
      if (mutationRef.current === controller) mutationRef.current = undefined;
      if (alive.current && !controller.signal.aborted) { setMutation(undefined); onBusyChange(false); }
    }
  }

  async function cancel() {
    if (!run || !selection || !isActive(run) || ['cancelling', 'cleanup_pending'].includes(run.State)
      || mutationRef.current || instanceChanged || selectedProblem || selectedResource.error || detailsOutdated) return;
    const controller = new AbortController();
    mutationRef.current = controller; setMutation('cancel'); onBusyChange(true); setCancelError(undefined);
    try {
      const result = await adminApi.cancelMediaDiagnosticRun(run.InstanceId, run.Id, { signal: controller.signal });
      if (!alive.current || controller.signal.aborted) return;
      acceptResult(result.Run);
    } catch (cause) {
      if (alive.current && !controller.signal.aborted && !isAbortError(cause)) setCancelError(cause);
    } finally {
      if (mutationRef.current === controller) mutationRef.current = undefined;
      if (alive.current && !controller.signal.aborted) { setMutation(undefined); onBusyChange(false); }
    }
  }

  return <Paper component="section" aria-labelledby="media-diagnostics-heading" variant="outlined" sx={{ p: { xs: 2.5, sm: 3 } }}>
    <Stack spacing={2.5}>
      <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1.5, flexWrap: 'wrap' }}>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}><ScienceOutlined color="primary" /><Typography id="media-diagnostics-heading" variant="h3" component="h2">Media diagnostics</Typography></Stack>
        <Button startIcon={<RefreshRounded />} disabled={indexLoading || Boolean(mutation)} onClick={() => { void refreshIndex(); activeResource.reload(); }}>Refresh diagnostics</Button>
      </Stack>
      <Typography variant="body2" color="text.secondary">Run fixed video and audio samples to check actual decoding, encoding and their combined path. A configured codec or listed device is not a passed diagnostic. Runs continue on the server when you leave this page.</Typography>
      {indexLoading && !index && <Skeleton role="status" aria-label="Loading media diagnostics" variant="rounded" height={110} />}
      {indexError != null && <Box><ErrorNotice error={indexError} retry={() => void refreshIndex()} /><Typography variant="body2" color="text.secondary">Any displayed history is the last recorded result. Refresh diagnostics to confirm the current server.</Typography></Box>}
      {index && !index.Available && <Alert severity="warning">Media diagnostics are unavailable. {index.UnavailableReason.replaceAll('_', ' ')}</Alert>}
      <FormControl disabled={Boolean(mutation) || Boolean(pending && !canReplaceUnknown)}>
        <FormLabel id="media-diagnostics-mode-label">Diagnostic scope</FormLabel>
        <RadioGroup aria-labelledby="media-diagnostics-mode-label" value={mode} onChange={(_event, value) => { if (value === 'software' || value === 'configured') setMode(value); }}>
          <FormControlLabel value="software" control={<Radio />} label="Software video and audio baseline" />
          <FormControlLabel value="configured" control={<Radio />} disabled={!index?.HardwareConfigured} label="Configured hardware path, including the software baseline" />
        </RadioGroup>
      </FormControl>
      {index && !index.HardwareConfigured && <Typography variant="body2" color="text.secondary">No hardware path is configured. Software results do not establish hardware support.</Typography>}
      {startBlocked && <Typography variant="body2" color="text.secondary">Save or discard pending settings changes before starting diagnostics. The run uses the server's saved configuration.</Typography>}
      {active && <Alert severity="info">A diagnostic is already active. Another run cannot start until it has finished and released its resources.</Alert>}
      {retentionFull && <Alert severity="info">The recent-run history is full. Wait for older results to expire, then refresh diagnostics before starting another run.</Alert>}
      {index && (!indexFresh || expired) && <Typography variant="body2" color="text.secondary">Refresh diagnostics to confirm current availability before starting another run.</Typography>}
      {startError != null && <ErrorNotice error={startError} />}
      {pending && <Alert severity="warning">
        <Typography variant="body2">The start result has not been confirmed. The original request is retained; checking it does not start another run.</Typography>
        {pendingProblem && <Typography variant="body2" sx={{ mt: 1 }}>The original run cannot be found or confirmed. It may have expired or been cleared by a server restart. This does not prove that it never ran. Refresh diagnostics before making a new decision.</Typography>}
        <Button color="inherit" size="small" disabled={Boolean(mutation) || Boolean(index && pending.InstanceId !== index.InstanceId)} onClick={() => { setSelection(pending); setAwaitingLookup(false); selectedResource.reload(); }} sx={{ mt: 1 }}>Check start result</Button>
      </Alert>}
      <Box><Button variant="contained" startIcon={mutation === 'start' ? <CircularProgress size={16} color="inherit" /> : <PlayArrowRounded />} disabled={!canStart} onClick={() => void start()}>{mutation === 'start' ? 'Requesting diagnostic...' : pending ? 'Start another diagnostic' : 'Start diagnostic'}</Button></Box>
      {selection && <Box sx={{ borderTop: 1, borderColor: 'divider', pt: 2.5 }}>
        <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1.5, flexWrap: 'wrap', mb: 2 }}>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap' }}><Typography variant="h3" component="h2">Run results</Typography>{run && <StateChip state={run.State} />}</Stack>
          <Stack direction="row" spacing={1}><Button size="small" startIcon={<RefreshRounded />} disabled={Boolean(mutation) || selectedResource.loading || instanceChanged} onClick={refreshRun}>Refresh run</Button>
            {run && isActive(run) && <Button size="small" color="error" startIcon={<CancelOutlined />} disabled={Boolean(mutation) || ['cancelling', 'cleanup_pending'].includes(run.State) || instanceChanged || Boolean(selectedProblem) || selectedResource.error != null || detailsOutdated} onClick={() => void cancel()}>Cancel run</Button>}</Stack>
        </Stack>
        {(instanceChanged || selectedProblem) && <Alert severity="warning" sx={{ mb: 2 }}>This result cannot be confirmed against the current server. {instanceChanged || selectedProblem?.kind === 'instance_changed' ? 'The server instance has changed.' : 'The run is no longer available.'} Any displayed stages are historical observations.</Alert>}
        {detailsOutdated && <Alert severity="info" sx={{ mb: 2 }}>Newer run information is available. The details below are the last recorded result; refresh this run to view the latest report.</Alert>}
        {selectedResource.error != null && <Box sx={{ mb: 2 }}><ErrorNotice error={selectedResource.error} retry={instanceChanged ? undefined : refreshRun} /><Typography variant="body2" color="text.secondary">Automatic updates stopped. The last recorded result is retained; a read failure does not establish the run's outcome.</Typography></Box>}
        {cancelError != null && <Box sx={{ mb: 2 }}><ErrorNotice error={cancelError} retry={refreshRun} /><Typography variant="body2" color="text.secondary">The cancellation could not be confirmed. Refresh this run before deciding again.</Typography></Box>}
        {selectedResource.loading && !run && <Skeleton role="status" aria-label="Loading diagnostic run" variant="rounded" height={160} />}
        {run && <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>{run.Mode === 'configured' ? 'Configured path with software baseline' : 'Software baseline'} · Created: {displayTime(run.CreatedAt)} · Finished: {displayTime(run.FinishedAt)}{run.Code ? ` · ${run.Code.replaceAll('_', ' ')}` : ''}</Typography>}
        {run?.Report === null && report && <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>The latest response has no stage report. The last recorded stages for this run are retained below.</Typography>}
        {report ? <ReportResults report={report} runState={run?.State} /> : <Typography variant="body2" color="text.secondary">No stage results have been reported. No decoding or encoding result is confirmed yet.</Typography>}
        {(run && isActive(run)) && <Typography variant="caption" color="text.secondary" component="p" sx={{ mt: 2 }}>{selectedResource.paused ? 'Updates pause while this page is hidden.' : 'Updates every 5 seconds while this run is active.'}</Typography>}
      </Box>}
      {activeResource.error != null && <Box><ErrorNotice error={activeResource.error} retry={activeResource.reload} /><Typography variant="body2" color="text.secondary">The active run could not be refreshed. Its last recorded state is retained.</Typography></Box>}
      <Box sx={{ borderTop: 1, borderColor: 'divider', pt: 2.5 }}>
        <Typography variant="h4" component="h3">Recent runs</Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.75, mb: 1.5 }}>Up to 32 results are retained for 30 minutes. A server restart clears this history. Cancelled runs retain their completed stage results.</Typography>
        {index?.Items.length === 0 && <Typography variant="body2" color="text.secondary">No retained diagnostic runs.</Typography>}
        {index && index.Items.length > 0 && <TableContainer><Table size="small" aria-label="Recent media diagnostic runs"><TableHead><TableRow><TableCell>Created</TableCell><TableCell>Scope</TableCell><TableCell>State</TableCell><TableCell align="right">Results</TableCell></TableRow></TableHead>
          <TableBody>{index.Items.map((item) => <TableRow key={item.Id} selected={selectedKey === referenceKey(runReference(item))}><TableCell>{displayTime(item.CreatedAt)}</TableCell><TableCell>{item.Mode === 'configured' ? 'Configured + software' : 'Software'}</TableCell><TableCell><StateChip state={item.State} /></TableCell><TableCell align="right"><Button size="small" disabled={Boolean(mutation)} onClick={() => { setSelection(runReference(item)); setCancelError(undefined); setAwaitingLookup(false); selectedResource.reload(); }}>View run</Button></TableCell></TableRow>)}</TableBody>
        </Table></TableContainer>}
      </Box>
    </Stack>
  </Paper>;
}
