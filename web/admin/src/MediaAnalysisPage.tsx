import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, LinearProgress, MenuItem, Skeleton, Stack, Tab, Tabs, TextField, Tooltip, Typography } from './ui';
import { PlaylistPlayRounded } from './icons';
import { RefreshRounded } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { Library, TaskDefinition, TaskDefinitionsResponse, TaskRunDetail } from './api';
import { ErrorNotice } from './components';
import { AnalysisConfigurationFields } from './AnalysisConfigurationFields';
import { BackgroundPreviewSettings } from './BackgroundPreviewSettings';
import type { BackgroundPreviewSettingsState } from './BackgroundPreviewSettings';
import { analysisBytes, analysisDraft, parseAnalysisDraft } from './mediaAnalysis';
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
  { key: 'EnablePreviewGeneration', name: 'Seek previews', description: 'Images shown while seeking', videoOnly: true },
  { key: 'EnableIntroDetection', name: 'Intro detection', description: 'Repeated opening sequences', seriesOnly: true },
  { key: 'EnableCreditsDetection', name: 'Credits detection', description: 'End credits and skip markers', videoOnly: true },
  { key: 'EnableBackgroundPreviewGeneration', name: 'Background previews', description: 'Short clips for media details', videoOnly: true },
  { key: 'EnableAudioWaveformGeneration', name: 'Audio waveforms', description: 'A separate waveform for each track', videoOnly: true },
  { key: 'EnableSubtitleTimelineGeneration', name: 'Subtitle timelines', description: 'Bitmap subtitle display intervals', videoOnly: true },
] as const;

type AnalysisFeature = (typeof analysisFeatures)[number];
type AnalysisFeatureStatus = 'Enabled' | 'Disabled' | 'Not applicable' | 'Not reported';

function analysisFeatureStatus(library: Library, feature: AnalysisFeature): AnalysisFeatureStatus {
  const applicable = 'seriesOnly' in feature ? library.CollectionType === 'tvshows' : ['movies', 'tvshows', 'mixed'].includes(library.CollectionType);
  const enabled = library.LibraryOptions?.[feature.key];
  return !applicable ? 'Not applicable' : typeof enabled !== 'boolean' ? 'Not reported' : enabled ? 'Enabled' : 'Disabled';
}

function AnalysisFeatureState({ library, feature, text }: { library: Library; feature: AnalysisFeature; text: (value: string) => string }) {
  const status = analysisFeatureStatus(library, feature);
  return <span className={`analysis-feature-state${status === 'Enabled' ? ' is-enabled' : ''}`}><i aria-hidden="true" /><span>{text(status)}</span></span>;
}

const analysisCopy: Record<string, string> = {
  'Media analysis': '媒体分析', 'Six analysis types': '6 种分析', 'Coverage': '覆盖率', 'Library automation': '按媒体库自动处理',
  'Configure': '配置', 'Open tasks': '打开任务', 'Refresh analysis status': '刷新分析状态', 'Library settings': '按媒体库设置',
  'Analysis': '分析', 'Seek previews': '进度预览', 'Images shown while seeking': '拖动进度时显示的预览图',
  'Intro detection': '片头检测', 'Repeated opening sequences': '查找剧集中重复的片头',
  'Credits detection': '片尾检测', 'End credits and skip markers': '识别片尾与跳过区间',
  'Background previews': '背景短片', 'Short clips for media details': '媒体详情页的预览短片',
  'Audio waveforms': '音轨波形', 'A separate waveform for each track': '每条音轨分别生成',
  'Subtitle timelines': '字幕时间轴', 'Bitmap subtitle display intervals': '位图字幕的显示区间',
  'Enabled': '已启用', 'Disabled': '未启用', 'Not applicable': '不适用', 'Not reported': '未提供',
  'Library settings are shown here. Completed and eligible item totals are not reported by this server.': '此处显示媒体库的功能开关；当前服务端未提供已完成与可处理条目总数。',
  'Turning generation off keeps existing results. Replacing an existing result requires an explicit regeneration.': '关闭生成不会删除已有结果；替换已有结果需要明确执行重新生成。',
  'No libraries configured': '尚未配置媒体库', 'Add a library to enable automatic media processing.': '添加媒体库后，可以为每个库启用自动分析。',
  'Queue': '队列', 'No active analysis tasks': '当前没有运行中的分析任务',
  'Enabled libraries queue work after a scan or according to their task schedule.': '启用的媒体库会在扫描后，或按任务计划排队处理。',
  'Failures and interruptions': '无结果与失败', 'No failed analysis runs reported': '没有已报告的失败分析任务',
  'View item results below to review detection results and manually marked credits.': '在下方查看条目结果，核对检测状态和人工片尾标记。',
  'View task': '查看任务', 'Analysis cache': '分析缓存', 'Manage cache': '管理缓存',
  'Runtime status': '运行环境', 'Processing available': '处理功能可用', 'Processing unavailable': '处理功能不可用',
  'Intro available': '片头检测可用', 'Intro unavailable': '片头检测不可用',
  'Previews available': '预览生成可用', 'Previews unavailable': '预览生成不可用',
  'Cache accounting is unavailable while this runtime is not configured.': '运行环境尚未配置，暂时无法读取缓存用量。',
  'Processing settings': '处理设置', 'Generation profiles and library controls': '生成规格与媒体库开关',
  'Automatic credits detection': '自动识别片尾', 'Credits analysis library': '片尾识别媒体库',
  'Choose a library': '请选择媒体库', 'Detect credits in this library': '识别此库片尾', 'Detect credits again': '重新识别此库片尾',
  'Manual and imported markers take priority. Regeneration changes only automatic results.': '人工和导入标记始终优先；重新识别只更新自动结果。',
  'Credits detection is disabled by default. Enable it in a video library to analyze chapters, black frames, and repeated audio near the end.': '自动识别默认关闭。请在视频媒体库中启用；任务会分析片尾的章节、黑场与重复音频。',
  'Intro and seek previews': '片头与进度预览', 'Media analysis result types': '媒体分析结果类型',
  'Previous run request': '上次任务请求', 'The previous run request is unconfirmed. Check its recorded result before starting another run.': '上次任务请求尚未确认，请核对已记录的结果后再开始新的任务。',
  'Check run request': '核对任务请求', 'Analysis task progress': '分析任务进度', 'Loading recorded task progress...': '正在读取已记录的任务进度…',
  'Request stop': '请求停止', 'Refresh progress': '刷新进度', 'View tasks': '查看任务', 'Work item': '处理条目',
  'active': '项运行中', 'failed': '项失败', 'Library settings unavailable': '无法读取媒体库设置',
  'Showing the last successful update.': '正在显示上次成功读取的数据。',
};

function isAnalysisTask(task: TaskDefinition): boolean {
  return /intro|preview|waveform|subtitle|credits|analysis|background/i.test(task.Key);
}
const loadAnalysisTasks = (signal: AbortSignal) => adminApi.getTasks({ signal });
const pollAnalysisTasks = (response: TaskDefinitionsResponse) => response.Items.some((task) => isAnalysisTask(task) && task.CurrentRun && isActiveTaskRun(task.CurrentRun));

export function MediaAnalysisPage({ currentUserId, onTasks, onLibraries, onNavigationGuardChange }: { currentUserId: string; onTasks: () => void; onLibraries: () => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const { t, language, locale, formatCount } = useTranslation();
  const text = (value: string) => language === 'zh' ? analysisCopy[value] ?? t(value) : value;
  const [overview, setOverview] = useState<AnalysisOverview>(); const [draft, setDraft] = useState<AnalysisDraft>(); const [libraries, setLibraries] = useState<Library[]>([]);
  const [loading, setLoading] = useState(true); const [loadError, setLoadError] = useState<unknown>(); const [loadRevision, setLoadRevision] = useState(0); const preserveDraft = useRef(false);
  const [busy, setBusy] = useState<string>(); const mutation = useRef<AbortController | undefined>(undefined);
  const [error, setError] = useState<unknown>(); const [reloadRequired, setReloadRequired] = useState(false); const [notice, setNotice] = useState('');
  const [pending, setPending] = useState<AnalysisRunInput | undefined>(() => previousReceipt(currentUserId));
  const [receipt, setReceipt] = useState<AnalysisRunReceipt>(); const [runError, setRunError] = useState<unknown>(); const [resultsRevision, setResultsRevision] = useState(0);
  const [pruning, setPruning] = useState(false); const [pruned, setPruned] = useState<AnalysisPrune>(); const finished = useRef('');
  const [configurationOpen, setConfigurationOpen] = useState(false);
  const [backgroundState, setBackgroundState] = useState<BackgroundPreviewSettingsState>({ dirty: false, busy: false });
  const [waveformActivity, setWaveformActivity] = useState<AudioWaveformActivity>({ pending: false, busy: false });
  const [subtitleTimelineActivity, setSubtitleTimelineActivity] = useState<SubtitleTimelineActivity>({ pending: false, busy: false });
  const [creditsActivity, setCreditsActivity] = useState<CreditsEditorActivity>({ dirty: false, pending: false, busy: false });
  const [creditsLibraryId, setCreditsLibraryId] = useState('');
  const [resultsTab, setResultsTab] = useState<'analysis' | 'waveform' | 'subtitle-timeline'>('analysis');
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
      if (!controller.signal.aborted) { setReceipt(value); remember(currentUserId); setPending(undefined); setRunError(undefined); setNotice(input.Kind === 'credits' ? value.Admitted ? 'Credits detection queued. Manual markers remain in effect.' : 'The recorded credits detection request was found.' : value.Admitted ? 'Run admitted. Progress below comes from the task service.' : 'The server returned an existing run. Its recorded progress is shown below.'); }
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) { setError(cause); if (!unknownResult(cause)) { remember(currentUserId); setPending(undefined); } }
    } finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(undefined); }
  }
  async function checkRequest() { if (pending) await submitRun(pending); }
  async function startCredits(force: boolean) {
    if (pending || !creditsLibraryId || overview?.Runtime.CreditsAvailable === false || run.data && isActiveTaskRun(run.data.Run)) return;
    await submitRun({ Kind: 'credits', RequestId: crypto.randomUUID(), LibraryIds: [creditsLibraryId], ItemIds: [], Force: force });
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
  const analysisTasks = tasks.data?.Items.filter(isAnalysisTask) ?? [];
  const activeTasks = analysisTasks.filter((task) => task.CurrentRun && isActiveTaskRun(task.CurrentRun));
  const failedTasks = analysisTasks.filter((task) => task.LastRun && (['failed', 'interrupted'].includes(task.LastRun.State) || task.LastRun.FailedChildren > 0 || task.LastRun.UnavailableChildren > 0));
  const configurationBusy = locked || backgroundState.busy || waveformActivity.busy || subtitleTimelineActivity.busy || creditsActivity.busy;

  return <div className="analysis-page" aria-busy={loading}>
    <header className="analysis-page-heading">
      <div>
        <h1>{text('Media analysis')}</h1>
        <div className="analysis-page-summary"><span>{text('Six analysis types')}</span><span>{tasks.data ? activeTasks.length.toLocaleString(locale) : '—'} {text('active')}</span>{failedTasks.length > 0 && <span className="analysis-tone-error">{failedTasks.length.toLocaleString(locale)} {text('failed')}</span>}</div>
      </div>
      <div className="analysis-page-actions">
        <Tooltip title={text('Refresh analysis status')}><IconButton aria-label={text('Refresh analysis status')} disabled={configurationBusy || loading} onClick={() => reload(true)}><RefreshRounded sx={{ fontSize: 17 }} /></IconButton></Tooltip>
        <Button variant="outlined" onClick={() => setConfigurationOpen(true)} disabled={configurationBusy || !overview || !draft}>{text('Configure')}{dirty ? ' •' : ''}</Button>
        <Button variant="contained" startIcon={<PlaylistPlayRounded />} onClick={onTasks}>{text('Open tasks')}</Button>
      </div>
    </header>

    {loadError != null && <ErrorNotice error={loadError} retry={() => reload(Boolean(draft))} />}
    {loadError != null && overview && <Typography variant="caption" color="text.secondary">{text('Showing the last successful update.')}</Typography>}
    {error != null && !configurationOpen && <ErrorNotice error={error} />}
    {reloadRequired && !configurationOpen && <Alert severity="warning">The change could not be confirmed or the configuration changed. Your draft is preserved. Reload the latest revision, review your draft, and then save again.<Button color="inherit" disabled={loading || locked} onClick={() => reload(true)}>Reload latest and keep draft</Button></Alert>}
    {notice && !configurationOpen && <Alert severity="success">{notice}</Alert>}
    {pruned && <Alert severity="success">Removed {formatCount(pruned.RemovedEntries, 'entry', 'entries')} ({analysisBytes(pruned.RemovedBytes)}). {analysisBytes(pruned.RemainingBytes)} remains; {formatCount(pruned.BusyEntries, 'busy entry', 'busy entries')} retained.</Alert>}

    <section className="analysis-panel" aria-labelledby="analysis-coverage-title">
      <div className="analysis-panel-header"><h2 id="analysis-coverage-title">{text('Coverage')}</h2><span className="analysis-panel-caption">{text('Library automation')}</span><Button onClick={onLibraries} disabled={configurationBusy} size="small">{text('Library settings')}</Button></div>
      {loading && !overview ? <div className="analysis-loading" role="status" aria-label={t('Loading media analysis')}><Skeleton height={46} /><Skeleton height={46} /><Skeleton height={46} /></div> : !overview && loadError != null ? <div className="analysis-empty"><strong>{text('Library settings unavailable')}</strong></div> : libraries.length > 0 ? <>
        <div className="analysis-matrix-scroll">
          <table className="analysis-matrix" aria-label={text('Library automation')}>
            <thead><tr><th scope="col">{text('Analysis')}</th>{libraries.map((library) => <th key={library.Id} scope="col" translate="no">{library.Name}</th>)}</tr></thead>
            <tbody>{analysisFeatures.map((feature) => <tr key={feature.key}>
              <th scope="row"><strong>{text(feature.name)}</strong><span>{text(feature.description)}</span></th>
              {libraries.map((library) => <td key={library.Id}><AnalysisFeatureState library={library} feature={feature} text={text} /></td>)}
            </tr>)}</tbody>
          </table>
        </div>
        <div className="analysis-coverage-libraries">
          {libraries.map((library) => <article key={library.Id} className="analysis-coverage-library" aria-labelledby={`analysis-coverage-library-${encodeURIComponent(library.Id)}`} data-library-id={library.Id}>
            <h3 id={`analysis-coverage-library-${encodeURIComponent(library.Id)}`} translate="no">{library.Name}</h3>
            <dl>{analysisFeatures.map((feature) => <div key={feature.key} className="analysis-coverage-row" data-analysis-feature={feature.key}>
              <dt><strong>{text(feature.name)}</strong><span>{text(feature.description)}</span></dt>
              <dd><AnalysisFeatureState library={library} feature={feature} text={text} /></dd>
            </div>)}</dl>
          </article>)}
        </div>
      </> : <div className="analysis-empty"><strong>{text('No libraries configured')}</strong><p>{text('Add a library to enable automatic media processing.')}</p><Button onClick={onLibraries}>{text('Library settings')}</Button></div>}
      <div className="analysis-panel-note"><span aria-hidden="true">ⓘ</span><span>{text('Library settings are shown here. Completed and eligible item totals are not reported by this server.')}</span></div>
    </section>

    <div className="analysis-work-grid">
      <section className="analysis-panel" aria-labelledby="analysis-queue-title">
        <div className="analysis-panel-header"><h2 id="analysis-queue-title">{text('Queue')}</h2><span className="analysis-panel-caption">{activeTasks.length.toLocaleString(locale)}</span><Tooltip title={t('Refresh tasks')}><IconButton size="small" aria-label={t('Refresh tasks')} disabled={tasks.loading} onClick={tasks.reload}><RefreshRounded sx={{ fontSize: 16 }} /></IconButton></Tooltip></div>
        {tasks.error != null && <div className="analysis-panel-inset"><ErrorNotice error={tasks.error} retry={tasks.reload} /></div>}
        {tasks.loading && !tasks.data && <div className="analysis-loading" role="status" aria-label={t('Loading tasks')}><Skeleton height={44} /><Skeleton height={44} /></div>}
        {tasks.data && activeTasks.length === 0 && <div className="analysis-empty analysis-empty-compact"><strong>{text('No active analysis tasks')}</strong><p>{text('Enabled libraries queue work after a scan or according to their task schedule.')}</p></div>}
        {activeTasks.map((task) => <div className="analysis-task-row" key={task.Id}>
          <div className="analysis-task-top"><i className="analysis-live-dot" aria-hidden="true" /><strong translate="no">{task.Name}</strong><RunStatusChip state={task.CurrentRun!.State} /></div>
          <RunProgress run={task.CurrentRun!} />
          <Button size="small" onClick={onTasks}>{text('View task')}</Button>
        </div>)}
      </section>
      <section className="analysis-panel" aria-labelledby="analysis-failures-title">
        <div className="analysis-panel-header"><h2 id="analysis-failures-title">{text('Failures and interruptions')}</h2><span className="analysis-panel-caption">{failedTasks.length.toLocaleString(locale)}</span></div>
        {tasks.data && failedTasks.length === 0 && <div className="analysis-empty analysis-empty-compact"><strong>{text('No failed analysis runs reported')}</strong><p>{text('View item results below to review detection results and manually marked credits.')}</p></div>}
        {tasks.loading && !tasks.data && <div className="analysis-loading"><Skeleton height={44} /><Skeleton height={44} /></div>}
        {tasks.error != null && <div className="analysis-panel-inset"><Typography color="text.secondary">Task status is unavailable. Refresh the queue to try again.</Typography></div>}
        {failedTasks.map((task) => <div className="analysis-failure-row" key={task.Id}><div><strong translate="no">{task.Name}</strong><span className="analysis-failure-description">{task.LastRun!.ErrorMessage || task.LastRun!.ErrorCode || t('Some work items did not complete.')}</span><time dateTime={task.LastRun!.FinishedAt ?? task.LastRun!.CreatedAt}>{new Date(task.LastRun!.FinishedAt ?? task.LastRun!.CreatedAt).toLocaleString(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}</time></div><Button variant="outlined" size="small" onClick={onTasks}>{text('View task')}</Button></div>)}
      </section>
    </div>

    {(pending || receipt) && <section className="analysis-panel" aria-labelledby="analysis-run-title">
      <div className="analysis-panel-header"><h2 id="analysis-run-title">{text('Previous run request')}</h2></div>
      <div className="analysis-panel-inset">
        {pending && <Alert severity="warning">{text('The previous run request is unconfirmed. Check its recorded result before starting another run.')}<Button disabled={blocked || dirty} color="inherit" onClick={() => void checkRequest()}>{text('Check run request')}</Button></Alert>}
        {receipt && <Box role="region" aria-label={text('Analysis task progress')}><Stack spacing={1.5}>
          <Typography variant="caption" className="mono">Run {receipt.RunId}</Typography>
          {run.loading && !run.data && <Typography role="status">{text('Loading recorded task progress...')}</Typography>}
          {run.error != null && <ErrorNotice error={run.error} retry={run.reload} />}
          {runError != null && <ErrorNotice error={runError} retry={() => { setRunError(undefined); run.reload(); }} />}
          {run.data && <><RunStatusChip state={run.data.Run.State} /><RunProgress run={run.data.Run} />{run.data.Run.State === 'stopping' && <Alert severity="info">Stop requested. The task remains active until its workers finish.</Alert>}{run.data.Run.ErrorMessage && <Alert severity="error">{run.data.Run.ErrorMessage}</Alert>}{run.data.Children.Items.filter((child) => child.ErrorCode || child.ErrorMessage).map((child) => <Alert severity="warning" key={child.Id}>{child.LibraryName || text('Work item')}: {child.ErrorMessage || child.ErrorCode}</Alert>)}</>}
          <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}><Button disabled={locked || run.loading || !run.data || !isActiveTaskRun(run.data.Run) || run.data.Run.State === 'stopping' || runError != null} onClick={() => void stop()}>{text('Request stop')}</Button><Button disabled={locked || run.loading} onClick={() => { setRunError(undefined); run.reload(); }}>{text('Refresh progress')}</Button><Button onClick={onTasks}>{text('View tasks')}</Button></Stack>
        </Stack></Box>}
      </div>
    </section>}

    <section className="analysis-results" aria-label={t('Media analysis results')}>
      <Tabs value={resultsTab} onChange={(_, value: 'analysis' | 'waveform' | 'subtitle-timeline') => setResultsTab(value)} aria-label={text('Media analysis result types')} variant="scrollable" scrollButtons="auto" sx={{ mb: 2 }}>
        <Tab id="analysis-results-tab" value="analysis" label={text('Intro and seek previews')} aria-controls="analysis-results-panel" disabled={waveformActivity.busy || subtitleTimelineActivity.busy} />
        <Tab id="waveform-results-tab" value="waveform" label={text('Audio waveforms')} aria-controls="waveform-results-panel" disabled={locked || backgroundState.busy || creditsActivity.busy || subtitleTimelineActivity.busy} />
        <Tab id="subtitle-timeline-results-tab" value="subtitle-timeline" label={text('Subtitle timelines')} aria-controls="subtitle-timeline-results-panel" disabled={locked || backgroundState.busy || creditsActivity.busy || waveformActivity.busy} />
      </Tabs>
      <div id="analysis-results-panel" role="tabpanel" aria-labelledby="analysis-results-tab" hidden={resultsTab !== 'analysis'}><MediaAnalysisResults libraries={libraries} disabled={blocked} refresh={resultsRevision} currentUserId={currentUserId} onTasks={onTasks} onCreditsActivityChange={setCreditsActivity} /></div>
      <div id="waveform-results-panel" role="tabpanel" aria-labelledby="waveform-results-tab" hidden={resultsTab !== 'waveform'}><AudioWaveformPanel currentUserId={currentUserId} libraries={libraries} onLibraries={onLibraries} onTasks={onTasks} onActivityChange={setWaveformActivity} visible={resultsTab === 'waveform'} /></div>
      <div id="subtitle-timeline-results-panel" role="tabpanel" aria-labelledby="subtitle-timeline-results-tab" hidden={resultsTab !== 'subtitle-timeline'}><SubtitleTimelinePanel currentUserId={currentUserId} libraries={libraries} onLibraries={onLibraries} onTasks={onTasks} onActivityChange={setSubtitleTimelineActivity} visible={resultsTab === 'subtitle-timeline'} available={overview?.Runtime.SubtitleTimelineAvailable} reasons={overview?.Runtime.SubtitleTimelineReasons} /></div>
    </section>

    <div className="analysis-status-grid">
      <section className="analysis-panel" aria-labelledby="analysis-cache-title"><div className="analysis-panel-header"><h2 id="analysis-cache-title">{text('Analysis cache')}</h2><Button size="small" disabled={blocked || dirty || !cache} onClick={() => setPruning(true)}>{text('Manage cache')}</Button></div><div className="analysis-panel-inset">
        {cache ? <><div className="analysis-cache-value">{analysisBytes(cache.TotalBytes)}<span>/ {analysisBytes(cache.MaxBytes)}</span></div><LinearProgress variant="determinate" value={cachePercent} aria-label={t('Analysis cache usage')} aria-valuetext={`${analysisBytes(cache.TotalBytes)} used of ${analysisBytes(cache.MaxBytes)}`} color={cachePercent >= 85 ? 'error' : cachePercent >= 75 ? 'warning' : 'primary'} sx={{ height: 3, borderRadius: 2, bgcolor: 'var(--line2)' }} /><Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>Ready {cache.ReadyEntries.toLocaleString(locale)} ({analysisBytes(cache.ReadyBytes)}) · Building {cache.BuildingEntries.toLocaleString(locale)} · Pending {cache.PendingPublications.toLocaleString(locale)}</Typography></> : <Typography variant="body2" color="text.secondary">{text('Cache accounting is unavailable while this runtime is not configured.')}</Typography>}
      </div></section>
      <section className="analysis-panel" aria-labelledby="analysis-runtime-title"><div className="analysis-panel-header"><h2 id="analysis-runtime-title">{text('Runtime status')}</h2></div><div className="analysis-panel-inset">
        {overview && <><div className="analysis-runtime-states"><span data-available={overview.Runtime.Configured}>{text(overview.Runtime.Configured ? 'Processing available' : 'Processing unavailable')}</span><span data-available={overview.Runtime.IntroAvailable}>{text(overview.Runtime.IntroAvailable ? 'Intro available' : 'Intro unavailable')}</span><span data-available={overview.Runtime.PreviewAvailable}>{text(overview.Runtime.PreviewAvailable ? 'Previews available' : 'Previews unavailable')}</span></div>{overview.Runtime.Reasons.length > 0 && <Typography variant="caption" color="text.secondary" sx={{ mt: 1 }}>{overview.Runtime.Reasons.map((reason) => reason.replaceAll('_', ' ')).join(' · ')}</Typography>}<Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>Configuration revision {overview.Configuration.Revision}</Typography></>}
      </div></section>
    </div>

    <details className="analysis-settings" open={backgroundState.dirty || backgroundState.busy ? true : undefined}>
      <summary><strong>{text('Processing settings')}</strong><span>{text('Generation profiles and library controls')}</span></summary>
      <div className="analysis-settings-content">
        <BackgroundPreviewSettings onStateChange={setBackgroundState} onLibraries={onLibraries} onTasks={onTasks} />
        <section className="analysis-panel" aria-labelledby="credits-analysis-title"><div className="analysis-panel-header"><h2 id="credits-analysis-title">{text('Automatic credits detection')}</h2></div><div className="analysis-panel-inset">
          <Typography variant="body2" color="text.secondary">{text('Credits detection is disabled by default. Enable it in a video library to analyze chapters, black frames, and repeated audio near the end.')}</Typography>
          {overview?.Runtime.CreditsReasons && overview.Runtime.CreditsReasons.length > 0 && <Alert severity="info" sx={{ mt: 1 }}>{overview.Runtime.CreditsReasons.join('; ')}</Alert>}
          <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ mt: 1.5, gap: 1, alignItems: { xs: 'stretch', sm: 'flex-end' }, flexWrap: 'wrap' }}>
            <TextField select size="small" label={text('Credits analysis library')} value={creditsLibraryId} disabled={blocked || Boolean(pending)} onChange={(event) => setCreditsLibraryId(event.target.value)} slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true } }} sx={{ minWidth: { sm: 220 } }}><MenuItem value="">{text('Choose a library')}</MenuItem>{libraries.filter((library) => ['movies', 'tvshows', 'mixed'].includes(library.CollectionType)).map((library) => <MenuItem key={library.Id} value={library.Id} translate="no">{library.Name}</MenuItem>)}</TextField>
            <Button variant="outlined" disabled={blocked || dirty || Boolean(pending) || !creditsLibraryId || overview?.Runtime.CreditsAvailable === false || Boolean(run.data && isActiveTaskRun(run.data.Run))} onClick={() => void startCredits(false)}>{text('Detect credits in this library')}</Button>
            <Button disabled={blocked || dirty || Boolean(pending) || !creditsLibraryId || overview?.Runtime.CreditsAvailable === false || Boolean(run.data && isActiveTaskRun(run.data.Run))} onClick={() => void startCredits(true)}>{text('Detect credits again')}</Button><Button disabled={locked} onClick={onLibraries}>{text('Library settings')}</Button>
          </Stack>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1.5 }}>{text('Manual and imported markers take priority. Regeneration changes only automatic results.')}</Typography>
        </div></section>
        <div className="analysis-panel-note">{text('Turning generation off keeps existing results. Replacing an existing result requires an explicit regeneration.')}</div>
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
    <Dialog open={pruning} onClose={() => { if (!locked) setPruning(false); }} fullWidth maxWidth="sm" aria-labelledby="analysis-prune-title">
      <DialogTitle id="analysis-prune-title">Prune the analysis cache?</DialogTitle>
      <DialogContent><Stack spacing={2}>{cache && <Box sx={{ p: 2, bgcolor: 'var(--s3)', borderRadius: '10px' }}><Typography variant="body2">{analysisBytes(cache.TotalBytes)} used of {analysisBytes(cache.MaxBytes)}</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>Active readers: {cache.Readers} · Reserved: {analysisBytes(cache.ReservedBytes)} · Control data: {analysisBytes(cache.ControlBytes)}</Typography></Box>}<Typography>Remove eligible unused entries under the current saved configuration. Busy readers, builds, and publications may retain entries. Original media is unchanged.</Typography></Stack></DialogContent>
      <DialogActions><Button disabled={locked} onClick={() => setPruning(false)}>Cancel</Button><Button variant="contained" disabled={blocked || dirty} onClick={() => void prune()}>Prune eligible entries</Button></DialogActions>
    </Dialog>
  </div>;
}
