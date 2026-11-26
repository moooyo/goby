import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, LinearProgress, MenuItem, Skeleton, Stack, Tab, Tabs, TextField, Tooltip, Typography } from './ui';
import { CleaningServicesRounded, InfoOutlined, PlaylistPlayRounded, RefreshRounded, SettingsRounded, StopRounded, WarningAmberRounded } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { Library, TaskDefinition, TaskDefinitionsResponse, TaskRun, TaskRunDetail } from './api';
import { ErrorNotice } from './components';
import { AnalysisConfigurationFields } from './AnalysisConfigurationFields';
import { BackgroundPreviewSettings } from './BackgroundPreviewSettings';
import type { BackgroundPreviewSettingsState } from './BackgroundPreviewSettings';
import { analysisDraft, parseAnalysisDraft, supportsVideoAnalysis } from './mediaAnalysis';
import { formatDirectoryBytes } from './directoryCapacity';
import type { AnalysisDraft, AnalysisOverview, AnalysisPrune, AnalysisRunInput, AnalysisRunReceipt } from './mediaAnalysis';
import { mediaAnalysisApi } from './mediaAnalysisApi';
import { MediaAnalysisResults } from './MediaAnalysisResults';
import { AudioWaveformPanel } from './AudioWaveformPanel';
import type { AudioWaveformActivity } from './audioWaveform';
import { SubtitleTimelinePanel } from './SubtitleTimelinePanel';
import type { SubtitleTimelineActivity } from './subtitleTimeline';
import type { CreditsEditorActivity } from './credits';
import { isActiveTaskRun, RunProgress, RunStatusChip } from './TaskRunDialog';
import { useTaskResource } from './useTaskResource';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { useTranslation } from './i18n';
import './media-analysis-page.css';

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
    if (value && ['intro', 'previews', 'credits'].includes(value.Kind) && typeof value.RequestId === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value.RequestId)
      && typeof value.Force === 'boolean' && Array.isArray(value.LibraryIds) && value.LibraryIds.length <= 64 && value.LibraryIds.every((id) => typeof id === 'string' && id.length > 0 && id.length <= 128)
      && Array.isArray(value.ItemIds) && value.ItemIds.length <= 256 && value.ItemIds.every((id) => typeof id === 'string' && id.length > 0 && id.length <= 128)) return value;
  } catch { /* An unreadable browser receipt is not a server result. */ }
  return undefined;
}
function unknownResult(error: unknown): boolean { return !(error instanceof ApiError) || error.status === 0 || error.status === 409 || error.status >= 500 || ['network_error', 'invalid_response', 'session_changed'].includes(error.code); }

const analysisFeatures = [
  { key: 'EnablePreviewGeneration', name: 'Seek previews', description: 'Images shown while seeking', taskKey: 'media.preview_generation' },
  { key: 'EnableIntroDetection', name: 'Intro detection', description: 'Repeated opening sequences', taskKey: 'media.intro_analysis', seriesOnly: true },
  { key: 'EnableCreditsDetection', name: 'Credits detection', description: 'End credits and skip markers', taskKey: 'media.credits_analysis' },
  { key: 'EnableBackgroundPreviewGeneration', name: 'Background previews', description: 'Short clips for media details', taskKey: 'media.background_preview_generation' },
  { key: 'EnableAudioWaveformGeneration', name: 'Audio waveforms', description: 'A separate waveform for each track', taskKey: 'media.audio_waveform_generation' },
  { key: 'EnableSubtitleTimelineGeneration', name: 'Subtitle timelines', description: 'Bitmap subtitle display intervals', taskKey: 'media.subtitle_timeline_generation' },
] as const;

type AnalysisFeature = (typeof analysisFeatures)[number];
type AnalysisFeatureStatus = 'Enabled' | 'Disabled' | 'Not applicable' | 'Not reported';

function analysisFeatureStatus(library: Library, feature: AnalysisFeature): AnalysisFeatureStatus {
  const applicable = 'seriesOnly' in feature ? library.CollectionType === 'tvshows' : supportsVideoAnalysis(library.CollectionType);
  const enabled = library.LibraryOptions?.[feature.key];
  return !applicable ? 'Not applicable' : typeof enabled !== 'boolean' ? 'Not reported' : enabled ? 'Enabled' : 'Disabled';
}

function AnalysisFeatureState({ library, feature }: { library: Library; feature: AnalysisFeature }) {
  const { t } = useTranslation();
  const status = analysisFeatureStatus(library, feature);
  return <span className={`analysis-feature-state${status === 'Enabled' ? ' is-enabled' : ''}`}><i aria-hidden="true" /><span>{t(status)}</span></span>;
}

function isAnalysisTask(task: TaskDefinition): boolean {
  return analysisFeatures.some((feature) => feature.taskKey === task.Key);
}

function runFailure(run: TaskRun): boolean {
  return run.State === 'failed' || run.State === 'interrupted' || run.FailedChildren > 0 || run.UnavailableChildren > 0 || run.InterruptedChildren > 0;
}
const loadAnalysisTasks = (signal: AbortSignal) => adminApi.getTasks({ signal });
const pollAnalysisTasks = (response: TaskDefinitionsResponse) => response.Items.some((task) => isAnalysisTask(task) && task.CurrentRun && isActiveTaskRun(task.CurrentRun));

export function MediaAnalysisPage({ currentUserId, onTasks, onTaskRun, onLibraries, onNavigationGuardChange }: {
  currentUserId: string;
  onTasks: () => void;
  onTaskRun?: (runId: string) => void;
  onLibraries: () => void;
  onNavigationGuardChange: UserNavigationGuardChange;
}) {
  const { t, locale, countLabel, formatCount } = useTranslation();
  const [overview, setOverview] = useState<AnalysisOverview>(); const [draft, setDraft] = useState<AnalysisDraft>(); const [libraries, setLibraries] = useState<Library[]>([]);
  const [loading, setLoading] = useState(true); const [loadError, setLoadError] = useState<unknown>(); const [loadRevision, setLoadRevision] = useState(0); const preserveDraft = useRef(false);
  const [busy, setBusy] = useState<string>(); const mutation = useRef<AbortController | undefined>(undefined);
  const [error, setError] = useState<unknown>(); const [reloadRequired, setReloadRequired] = useState(false); const [notice, setNotice] = useState('');
  const [pending, setPending] = useState<AnalysisRunInput | undefined>(() => previousReceipt(currentUserId));
  const [receipt, setReceipt] = useState<AnalysisRunReceipt>(); const [resultsRevision, setResultsRevision] = useState(0);
  const [stopRunId, setStopRunId] = useState<string>(); const [stopError, setStopError] = useState<unknown>();
  const [pruning, setPruning] = useState(false); const [pruned, setPruned] = useState<AnalysisPrune>(); const finished = useRef('');
  const [configurationOpen, setConfigurationOpen] = useState(false);
  const [backgroundState, setBackgroundState] = useState<BackgroundPreviewSettingsState>({ dirty: false, busy: false });
  const [waveformActivity, setWaveformActivity] = useState<AudioWaveformActivity>({ pending: false, busy: false });
  const [subtitleTimelineActivity, setSubtitleTimelineActivity] = useState<SubtitleTimelineActivity>({ pending: false, busy: false });
  const [creditsActivity, setCreditsActivity] = useState<CreditsEditorActivity>({ dirty: false, pending: false, busy: false });
  const [creditsLibraryId, setCreditsLibraryId] = useState('');
  const [resultsTab, setResultsTab] = useState<'analysis' | 'waveform' | 'subtitle-timeline'>('analysis');
  const videoLibraries = libraries.filter((library) => supportsVideoAnalysis(library.CollectionType));
  const selectedCreditsLibraryId = videoLibraries.some(({ Id }) => Id === creditsLibraryId) ? creditsLibraryId : '';
  const parsed = draft ? parseAnalysisDraft(draft) : undefined;
  const dirty = Boolean(draft && overview && JSON.stringify(draft) !== JSON.stringify(analysisDraft(overview.Configuration.Profile)));
  const locked = Boolean(busy);
  const blocked = locked || loading || loadError != null || reloadRequired;
  useUserDraftNavigation(dirty || Boolean(pending) || backgroundState.dirty || waveformActivity.pending || subtitleTimelineActivity.pending || creditsActivity.dirty || creditsActivity.pending, locked || backgroundState.busy || waveformActivity.busy || subtitleTimelineActivity.busy || creditsActivity.busy, onNavigationGuardChange, t('Leave media analysis? Unsaved settings will be discarded. Unconfirmed run requests are retained for review.'));
  const tasks = useTaskResource({ key: `media-analysis-tasks-${loadRevision}`, load: loadAnalysisTasks, poll: pollAnalysisTasks });
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
  async function submitRun(input: AnalysisRunInput) {
    if (mutation.current || blocked || dirty) return;
    // Submit the supplied request unchanged. Retries retain its request ID,
    // selected media, and force policy.
    const controller = new AbortController(); mutation.current = controller; remember(currentUserId, input); setPending(input); setBusy('start'); setError(undefined); setNotice('');
    try {
      const value = await mediaAnalysisApi.start(input, { signal: controller.signal });
      if (!controller.signal.aborted) { setReceipt(value); remember(currentUserId); setPending(undefined); setNotice(input.Kind === 'credits' ? value.Admitted ? 'Credits detection queued. Manual markers remain in effect.' : 'The recorded credits detection request was found.' : value.Admitted ? 'Run admitted. Progress below comes from the task service.' : 'The server returned an existing run. Its recorded progress is shown below.'); }
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) { setError(cause); if (!unknownResult(cause)) { remember(currentUserId); setPending(undefined); } }
    } finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(undefined); }
  }
  async function checkRequest() { if (pending) await submitRun(pending); }
  async function startCredits(force: boolean) {
    if (pending || !selectedCreditsLibraryId || overview?.Runtime.CreditsAvailable === false || run.data && isActiveTaskRun(run.data.Run)) return;
    await submitRun({ Kind: 'credits', RequestId: crypto.randomUUID(), LibraryIds: [selectedCreditsLibraryId], ItemIds: [], Force: force });
  }
  function requestStop(runId: string) { setStopError(undefined); setStopRunId(runId); }

  async function stop() {
    if (!stopRun || !isActiveTaskRun(stopRun) || stopRun.State === 'stopping' || configurationBusy || mutation.current) return;
    const controller = new AbortController(); mutation.current = controller; setBusy('stop'); setStopError(undefined);
    try {
      const { Run } = await adminApi.cancelTaskRun(stopRun.Id, { signal: controller.signal });
      if (!controller.signal.aborted) {
        setStopRunId(undefined);
        setNotice(isActiveTaskRun(Run) ? 'Stop requested. The task remains active until its workers finish.' : 'The task is no longer active.');
        if (receipt?.RunId === stopRun.Id) run.reload();
        tasks.reload();
      }
    } catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) setStopError(cause); }
    finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(undefined); }
  }
  async function prune() {
    if (!overview?.Runtime.Cache || blocked || dirty || mutation.current) return;
    const controller = new AbortController(); mutation.current = controller; setBusy('prune'); setError(undefined); setPruned(undefined); setNotice('');
    try { const value = await mediaAnalysisApi.prune(overview.Configuration.Revision, { signal: controller.signal }); if (!controller.signal.aborted) { setPruned(value); setPruning(false); reload(true); } }
    catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) { setPruning(false); setError(cause); setReloadRequired(unknownResult(cause)); } }
    finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(undefined); }
  }
  const cache = overview?.Runtime.Cache;
  const cachePercent = cache && cache.MaxBytes > 0 ? Math.min(100, cache.TotalBytes / cache.MaxBytes * 100) : null;
  const bytes = (value: number) => formatDirectoryBytes(BigInt(value), locale);
  const analysisTasks = tasks.data?.Items.filter(isAnalysisTask) ?? [];
  const activeTasks = analysisTasks.flatMap((task) => task.CurrentRun && isActiveTaskRun(task.CurrentRun) ? [{ task, run: task.CurrentRun }] : []);
  const failedTasks = analysisTasks.flatMap((task) => task.LastRun && runFailure(task.LastRun) ? [{ task, run: task.LastRun }] : [])
    .sort((left, right) => Date.parse(right.run.FinishedAt ?? right.run.CreatedAt) - Date.parse(left.run.FinishedAt ?? left.run.CreatedAt));
  const configurationBusy = locked || backgroundState.busy || waveformActivity.busy || subtitleTimelineActivity.busy || creditsActivity.busy;
  const stopRun = stopRunId ? activeTasks.find((entry) => entry.run.Id === stopRunId)?.run ?? (run.data?.Run.Id === stopRunId ? run.data.Run : undefined) : undefined;
  function viewTask(runId: string) { if (onTaskRun) onTaskRun(runId); else onTasks(); }

  return <div className="analysis-page" aria-busy={loading}>
    <header className="analysis-page-heading">
      <div>
        <h1>{t('Media analysis')}</h1>
        <div className="analysis-page-summary">
          <span>{t('Six analysis types')}</span>
          {overview && <span>{formatCount(libraries.length, 'library', 'libraries')}</span>}
          {tasks.data && <span>{t(countLabel(activeTasks.length, '{count} task active', '{count} tasks active'), { count: activeTasks.length.toLocaleString(locale) })}</span>}
        </div>
      </div>
      <div className="analysis-page-actions">
        <Tooltip title={t('Refresh analysis status')}><IconButton aria-label={t('Refresh analysis status')} disabled={configurationBusy || loading} onClick={() => reload(true)}><RefreshRounded sx={{ fontSize: 17 }} /></IconButton></Tooltip>
        <Button variant="outlined" startIcon={<SettingsRounded />} onClick={() => setConfigurationOpen(true)} disabled={configurationBusy || !overview || !draft}>{t('Analysis settings')}{dirty ? ' •' : ''}</Button>
        <Button variant="outlined" startIcon={<PlaylistPlayRounded />} onClick={onTasks}>{t('Open tasks')}</Button>
      </div>
    </header>

    {loadError != null && <ErrorNotice error={loadError} retry={() => reload(Boolean(draft))} />}
    {loadError != null && overview && <Typography variant="caption" color="text.secondary">{t('Showing the last successful update.')}</Typography>}
    {error != null && !configurationOpen && <ErrorNotice error={error} />}
    {reloadRequired && !configurationOpen && <Alert severity="warning">The change could not be confirmed or the configuration changed. Your draft is preserved. Reload the latest revision, review your draft, and then save again.<Button color="inherit" disabled={loading || locked} onClick={() => reload(true)}>Reload latest and keep draft</Button></Alert>}
    {notice && !configurationOpen && <Alert severity="success">{notice}</Alert>}
    {pruned && <Alert severity="success">
      {t(countLabel(pruned.RemovedEntries, 'Removed {count} cache entry and freed {size}.', 'Removed {count} cache entries and freed {size}.'), { count: pruned.RemovedEntries.toLocaleString(locale), size: bytes(pruned.RemovedBytes) })}{' '}
      {t(countLabel(pruned.BusyEntries, '{count} busy entry was retained.', '{count} busy entries were retained.'), { count: pruned.BusyEntries.toLocaleString(locale) })}
    </Alert>}

    <section className="analysis-panel" aria-labelledby="analysis-automation-title">
      <div className="analysis-panel-header"><h2 id="analysis-automation-title">{t('Library automation')}</h2><span className="analysis-panel-caption">{t('Analyses enabled by library')}</span><Button onClick={onLibraries} disabled={configurationBusy} size="small">{t('Library settings')}</Button></div>
      {loading && !overview ? <div className="analysis-loading" role="status" aria-label={t('Loading media analysis')}><Skeleton height={46} /><Skeleton height={46} /><Skeleton height={46} /></div> : !overview && loadError != null ? <div className="analysis-empty"><strong>{t('Library settings unavailable')}</strong></div> : libraries.length > 0 ? <>
        <div className="analysis-matrix-scroll" tabIndex={0} role="group" aria-label={t('Library automation table')}>
          <table className="analysis-matrix" aria-label={t('Library automation')}>
            <thead><tr><th scope="col">{t('Analysis')}</th>{libraries.map((library) => <th key={library.Id} scope="col" translate="no">{library.Name}</th>)}</tr></thead>
            <tbody>{analysisFeatures.map((feature) => <tr key={feature.key}>
              <th scope="row"><strong>{t(feature.name)}</strong><span>{t(feature.description)}</span></th>
              {libraries.map((library) => <td key={library.Id}><AnalysisFeatureState library={library} feature={feature} /></td>)}
            </tr>)}</tbody>
          </table>
        </div>
        <div className="analysis-coverage-libraries">
          {libraries.map((library) => <article key={library.Id} className="analysis-coverage-library" aria-labelledby={`analysis-coverage-library-${encodeURIComponent(library.Id)}`} data-library-id={library.Id}>
            <h3 id={`analysis-coverage-library-${encodeURIComponent(library.Id)}`} translate="no">{library.Name}</h3>
            <dl>{analysisFeatures.map((feature) => <div key={feature.key} className="analysis-coverage-row" data-analysis-feature={feature.key}>
              <dt><strong>{t(feature.name)}</strong><span>{t(feature.description)}</span></dt>
              <dd><AnalysisFeatureState library={library} feature={feature} /></dd>
            </div>)}</dl>
          </article>)}
        </div>
      </> : <div className="analysis-empty"><strong>{t('No libraries configured')}</strong><p>{t('Add a library to enable automatic media processing.')}</p><Button onClick={onLibraries}>{t('Library settings')}</Button></div>}
      <div className="analysis-panel-note"><InfoOutlined aria-hidden="true" sx={{ fontSize: 15, mt: 0.25, flexShrink: 0 }} /><span>{t('This table shows each library’s settings. See Failures and interruptions for task outcomes. Turning off generation does not delete existing results. Turning off intro or credits detection withdraws its automatic playback markers.')}</span></div>
    </section>

    <div className="analysis-work-grid">
      <section className="analysis-panel" aria-labelledby="analysis-queue-title">
        <div className="analysis-panel-header"><h2 id="analysis-queue-title">{t('Queue')}</h2><span className="analysis-panel-caption">{tasks.data ? activeTasks.length.toLocaleString(locale) : '—'}</span><Tooltip title={t('Refresh tasks')}><IconButton size="small" aria-label={t('Refresh tasks')} disabled={tasks.loading} onClick={tasks.reload}><RefreshRounded sx={{ fontSize: 16 }} /></IconButton></Tooltip></div>
        {tasks.error != null && <div className="analysis-panel-inset"><ErrorNotice error={tasks.error} retry={tasks.reload} /></div>}
        {tasks.loading && !tasks.data && <div className="analysis-loading" role="status" aria-label={t('Loading tasks')}><Skeleton height={44} /><Skeleton height={44} /></div>}
        {tasks.data && activeTasks.length === 0 && <div className="analysis-empty analysis-empty-compact"><strong>{t('No active analysis tasks')}</strong><p>{t('Enabled libraries can queue work after a scan or according to their task schedule.')}</p></div>}
        {activeTasks.map(({ task, run }) => <div className="analysis-task-row" key={run.Id}>
          <div className="analysis-task-top"><i className="analysis-live-dot" aria-hidden="true" /><strong>{t(task.Name)}</strong><RunStatusChip state={run.State} /></div>
          <RunProgress run={run} compact />
          <div className="analysis-task-actions">
            <Button size="small" onClick={() => viewTask(run.Id)}>{t('View task')}</Button>
            <Button size="small" variant="outlined" disabled={configurationBusy || run.State === 'stopping'} startIcon={<StopRounded />} onClick={() => requestStop(run.Id)}>Request stop</Button>
          </div>
        </div>)}
      </section>
      <section className="analysis-panel" aria-labelledby="analysis-failures-title">
        <div className="analysis-panel-header"><h2 id="analysis-failures-title">{t('Failures and interruptions')}</h2><span className="analysis-panel-caption">{tasks.data ? failedTasks.length.toLocaleString(locale) : '—'}</span></div>
        {tasks.data && failedTasks.length === 0 && <div className="analysis-empty analysis-empty-compact"><strong>{t('No failed analysis runs reported')}</strong></div>}
        {tasks.loading && !tasks.data && <div className="analysis-loading"><Skeleton height={44} /><Skeleton height={44} /></div>}
        {tasks.error != null && <div className="analysis-panel-inset"><Typography color="text.secondary">Task status is unavailable. Refresh the queue to try again.</Typography></div>}
        {failedTasks.map(({ task, run }) => {
          const time = run.FinishedAt ?? run.CreatedAt;
          const partial = run.CompletedChildren > 0 && run.FailedChildren + run.UnavailableChildren > 0;
          const tone = partial || run.State === 'interrupted' ? 'warning' : 'error';
          return <div className="analysis-failure-row" key={run.Id}>
            <WarningAmberRounded className={`analysis-failure-icon is-${tone}`} aria-hidden="true" sx={{ fontSize: 17 }} />
            <div><strong>{t(task.Name)}</strong><span className="analysis-failure-description">{t(run.ErrorMessage || run.ErrorCode || 'Some work items did not complete.')}</span><time dateTime={time}>{new Date(time).toLocaleString(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}</time></div>
            {partial ? <Chip size="small" color="warning" variant="outlined" label="Partial failure" /> : <RunStatusChip state={run.State} />}
            <Button variant="outlined" size="small" onClick={() => viewTask(run.Id)}>{t('View task')}</Button>
          </div>;
        })}
        <p className="analysis-panel-note">{t('View item results below to review detection results and manually marked credits.')}</p>
      </section>
    </div>

    {(pending || receipt) && <section className="analysis-panel" aria-labelledby="analysis-run-title">
      <div className="analysis-panel-header"><h2 id="analysis-run-title">{t('Previous run request')}</h2></div>
      <div className="analysis-panel-inset">
        {pending && <Alert severity="warning">{t('The previous run request is unconfirmed. Check its recorded result before starting another run.')}<Button disabled={blocked || dirty} color="inherit" onClick={() => void checkRequest()}>{t('Check run request')}</Button></Alert>}
        {receipt && <Box role="region" aria-label={t('Analysis task progress')}><Stack spacing={1.5}>
          <Typography variant="caption" className="mono">Run {receipt.RunId}</Typography>
          {run.loading && !run.data && <Typography role="status">{t('Loading recorded task progress...')}</Typography>}
          {run.error != null && <ErrorNotice error={run.error} retry={run.reload} />}
          {run.data && <><RunStatusChip state={run.data.Run.State} /><RunProgress run={run.data.Run} />{run.data.Run.State === 'stopping' && <Alert severity="info">Stop requested. The task remains active until its workers finish.</Alert>}{run.data.Run.ErrorMessage && <Alert severity="error">{run.data.Run.ErrorMessage}</Alert>}{run.data.Children.Items.filter((child) => child.ErrorCode || child.ErrorMessage).map((child) => <Alert severity="warning" key={child.Id}>{child.LibraryName || t('Work item')}: {child.ErrorMessage || child.ErrorCode}</Alert>)}</>}
          <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}><Button disabled={configurationBusy || run.loading || !run.data || !isActiveTaskRun(run.data.Run) || run.data.Run.State === 'stopping'} onClick={() => requestStop(receipt.RunId)}>{t('Request stop')}</Button><Button disabled={locked || run.loading} onClick={run.reload}>{t('Refresh progress')}</Button><Button onClick={() => viewTask(receipt.RunId)}>{t('View task')}</Button></Stack>
        </Stack></Box>}
      </div>
    </section>}

    <section className="analysis-results" aria-label={t('Media analysis results')}>
      <Tabs value={resultsTab} onChange={(_, value: 'analysis' | 'waveform' | 'subtitle-timeline') => setResultsTab(value)} aria-label={t('Media analysis result types')} variant="scrollable" scrollButtons="auto" sx={{ mb: 2 }}>
        <Tab id="analysis-results-tab" value="analysis" label={t('Intro and seek previews')} aria-controls="analysis-results-panel" disabled={waveformActivity.busy || subtitleTimelineActivity.busy} />
        <Tab id="waveform-results-tab" value="waveform" label={t('Audio waveforms')} aria-controls="waveform-results-panel" disabled={locked || backgroundState.busy || creditsActivity.busy || subtitleTimelineActivity.busy} />
        <Tab id="subtitle-timeline-results-tab" value="subtitle-timeline" label={t('Subtitle timelines')} aria-controls="subtitle-timeline-results-panel" disabled={locked || backgroundState.busy || creditsActivity.busy || waveformActivity.busy} />
      </Tabs>
      <div id="analysis-results-panel" role="tabpanel" aria-labelledby="analysis-results-tab" hidden={resultsTab !== 'analysis'}><MediaAnalysisResults libraries={overview ? videoLibraries : undefined} disabled={blocked} refresh={resultsRevision} currentUserId={currentUserId} onTasks={onTasks} onCreditsActivityChange={setCreditsActivity} /></div>
      <div id="waveform-results-panel" role="tabpanel" aria-labelledby="waveform-results-tab" hidden={resultsTab !== 'waveform'}><AudioWaveformPanel currentUserId={currentUserId} libraries={libraries} onLibraries={onLibraries} onTasks={onTasks} onActivityChange={setWaveformActivity} visible={resultsTab === 'waveform'} /></div>
      <div id="subtitle-timeline-results-panel" role="tabpanel" aria-labelledby="subtitle-timeline-results-tab" hidden={resultsTab !== 'subtitle-timeline'}><SubtitleTimelinePanel currentUserId={currentUserId} libraries={libraries} onLibraries={onLibraries} onTasks={onTasks} onActivityChange={setSubtitleTimelineActivity} visible={resultsTab === 'subtitle-timeline'} available={overview?.Runtime.SubtitleTimelineAvailable} reasons={overview?.Runtime.SubtitleTimelineReasons} /></div>
    </section>

    <div className="analysis-status-grid">
      <section className="analysis-panel" aria-labelledby="analysis-runtime-title">
        <div className="analysis-panel-header"><h2 id="analysis-runtime-title">{t('Runtime status')}</h2></div>
        <div className="analysis-panel-inset">
          {overview && <>
            <div className="analysis-runtime-states"><span data-available={overview.Runtime.Configured}>{t(overview.Runtime.Configured ? 'Processing available' : 'Processing unavailable')}</span><span data-available={overview.Runtime.IntroAvailable}>{t(overview.Runtime.IntroAvailable ? 'Intro available' : 'Intro unavailable')}</span><span data-available={overview.Runtime.PreviewAvailable}>{t(overview.Runtime.PreviewAvailable ? 'Previews available' : 'Previews unavailable')}</span></div>
            {overview.Runtime.Reasons.length > 0 && <Typography variant="caption" color="text.secondary" sx={{ mt: 1 }}>{overview.Runtime.Reasons.map((reason) => reason.replaceAll('_', ' ')).join(' · ')}</Typography>}
          </>}
          <div className="analysis-runtime-credits">
            <h3>{t('Automatic credits detection')}</h3>
            {overview?.Runtime.CreditsReasons && overview.Runtime.CreditsReasons.length > 0 && <Alert severity="info" sx={{ mb: 1 }}>{overview.Runtime.CreditsReasons.join('; ')}</Alert>}
            {videoLibraries.length > 0 ? <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ gap: 1, alignItems: { xs: 'stretch', sm: 'flex-end' }, flexWrap: 'wrap' }}>
              <TextField select size="small" label="Credits analysis library" value={selectedCreditsLibraryId} disabled={blocked || Boolean(pending)} onChange={(event) => setCreditsLibraryId(event.target.value)} sx={{ flex: { sm: '1 1 152px' }, minWidth: 0 }}><MenuItem value="">Choose a library</MenuItem>{videoLibraries.map((library) => <MenuItem key={library.Id} value={library.Id} translate="no">{library.Name}</MenuItem>)}</TextField>
              <Button size="small" variant="outlined" disabled={blocked || dirty || Boolean(pending) || !selectedCreditsLibraryId || overview?.Runtime.CreditsAvailable === false || Boolean(run.data && isActiveTaskRun(run.data.Run))} onClick={() => void startCredits(false)}>Detect credits in this library</Button>
              <Button size="small" disabled={blocked || dirty || Boolean(pending) || !selectedCreditsLibraryId || overview?.Runtime.CreditsAvailable === false || Boolean(run.data && isActiveTaskRun(run.data.Run))} onClick={() => void startCredits(true)}>Detect credits again</Button>
            </Stack> : overview && <Typography variant="body2" color="text.secondary">No video libraries are available for analysis.</Typography>}
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1.5 }}>Manual and imported markers take priority. Regeneration changes only automatic results.</Typography>
          </div>
        </div>
      </section>
      <section className="analysis-panel" aria-labelledby="analysis-cache-title">
        <div className="analysis-panel-header"><h2 id="analysis-cache-title">{t('Analysis cache')}</h2><Button size="small" startIcon={<CleaningServicesRounded />} disabled={blocked || dirty || !cache} onClick={() => setPruning(true)}>Manage cache</Button></div>
        <div className="analysis-panel-inset">
          {cache ? <>
            <div className="analysis-cache-value"><strong>{bytes(cache.TotalBytes)}</strong><span>{t(countLabel(cache.ReadyEntries, '{count} ready cache entry', '{count} ready cache entries'), { count: cache.ReadyEntries.toLocaleString(locale) })}</span></div>
            {cachePercent != null && <LinearProgress variant="determinate" value={cachePercent} aria-label={t('Analysis cache usage')} aria-valuetext={t('{used} used of {limit}', { used: bytes(cache.TotalBytes), limit: bytes(cache.MaxBytes) })} sx={{ height: 3, borderRadius: 2, bgcolor: 'var(--line2)' }} />}
            <div className="analysis-cache-details"><span>{t('Limit {size}', { size: bytes(cache.MaxBytes) })}</span><span>{t('Building {count}', { count: cache.BuildingEntries.toLocaleString(locale) })}</span></div>
          </> : loading ? <Skeleton height={55} aria-label="Loading analysis cache" /> : <Typography variant="body2" color="text.secondary">Analysis cache accounting is unavailable.</Typography>}
        </div>
      </section>
    </div>

    <details className="analysis-settings" open={backgroundState.dirty || backgroundState.busy ? true : undefined}>
      <summary><strong>{t('Processing settings')}</strong><span>{t('Generation profiles and library controls')}</span></summary>
      <div className="analysis-settings-content">
        <BackgroundPreviewSettings onStateChange={setBackgroundState} onLibraries={onLibraries} onTasks={onTasks} />
        <div className="analysis-panel-note">{t('Turning generation off keeps existing results. Replacing an existing result requires an explicit regeneration.')}</div>
      </div>
    </details>

    {overview && draft && <Dialog open={configurationOpen} onClose={() => { if (!locked) setConfigurationOpen(false); }} fullWidth maxWidth="md" aria-labelledby="analysis-configuration-title">
      <DialogTitle id="analysis-configuration-title">Analysis configuration</DialogTitle>
      <DialogContent><Box component="form" id="analysis-configuration-form" onSubmit={(event) => { event.preventDefault(); void save(); }}><Stack spacing={2} sx={{ pt: 0.5 }}>
        <Typography variant="body2" color="text.secondary">Revision {overview.Configuration.Revision}. Saving changed settings invalidates previous analysis results and queues fresh work for enabled libraries. Configuration remains editable when processing is unavailable.</Typography>
        {error != null && <ErrorNotice error={error} />}
        {reloadRequired && <Alert severity="warning">The change could not be confirmed or the configuration changed. Your draft is preserved.<Button color="inherit" disabled={loading || locked} onClick={() => reload(true)}>Reload latest and keep draft</Button></Alert>}
        {notice && <Alert severity="success">{notice}</Alert>}
        <AnalysisConfigurationFields draft={draft} errors={parsed?.errors ?? {}} mutationError={error} disabled={blocked} onChange={(value) => { setDraft(value); if (!reloadRequired) setError(undefined); }} />
        <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}><Button disabled={blocked} onClick={() => setDraft(analysisDraft(overview.Configuration.Defaults))}>Use default profile</Button><Button disabled={locked || loading} onClick={() => { setDraft(analysisDraft(overview.Configuration.Profile)); setNotice('Draft discarded. The last confirmed profile is shown.'); }}>Discard draft</Button></Stack>
        {dirty && <Typography role="status" variant="caption" color="text.secondary">The configuration has unsaved changes. Closing this dialog keeps the draft.</Typography>}
      </Stack></Box></DialogContent>
      <DialogActions><Button disabled={locked} onClick={() => setConfigurationOpen(false)}>Close</Button><Button type="submit" form="analysis-configuration-form" variant="contained" disabled={blocked || !dirty || !parsed?.profile} startIcon={busy === 'save' ? <CircularProgress size={16} color="inherit" /> : undefined}>Save analysis configuration</Button></DialogActions>
    </Dialog>}
    <Dialog open={pruning} role="alertdialog" onClose={locked ? undefined : () => setPruning(false)} fullWidth maxWidth="xs" aria-labelledby="analysis-prune-title" aria-describedby="analysis-prune-description">
      <DialogTitle id="analysis-prune-title">Prune the analysis cache?</DialogTitle>
      <DialogContent aria-busy={busy === 'prune'}><Typography id="analysis-prune-description" color="text.secondary">Only unused cache entries that can be recomputed will be removed. Results currently applied to media and original files stay unchanged. Actual removed entries and freed space will be reported after cleanup.</Typography></DialogContent>
      <DialogActions><Button autoFocus disabled={locked} onClick={() => setPruning(false)}>Cancel</Button><Button variant="contained" disabled={blocked || dirty || !cache} startIcon={busy === 'prune' ? <CircularProgress size={16} color="inherit" /> : undefined} onClick={() => void prune()}>Prune eligible entries</Button></DialogActions>
    </Dialog>
    <Dialog open={Boolean(stopRunId)} role="alertdialog" onClose={locked ? undefined : () => setStopRunId(undefined)} fullWidth maxWidth="xs" aria-labelledby="analysis-stop-title" aria-describedby="analysis-stop-description">
      <DialogTitle id="analysis-stop-title">Stop this run?</DialogTitle>
      <DialogContent aria-busy={busy === 'stop'}><Stack spacing={2}>
        <Typography id="analysis-stop-description" color="text.secondary">Stops the remaining work items. Changes from completed work remain in the catalog.</Typography>
        {!stopRun && <Alert severity="info">This run is no longer available to stop.</Alert>}
        {stopError != null && <ErrorNotice error={stopError} />}
      </Stack></DialogContent>
      <DialogActions><Button autoFocus disabled={locked} onClick={() => setStopRunId(undefined)}>Keep run</Button><Button variant="contained" color="error" startIcon={busy === 'stop' ? <CircularProgress size={16} color="inherit" /> : <StopRounded />} disabled={configurationBusy || !stopRun || !isActiveTaskRun(stopRun) || stopRun.State === 'stopping'} onClick={() => void stop()}>Stop run</Button></DialogActions>
    </Dialog>
  </div>;
}
