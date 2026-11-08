import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { adminApi, ApiError, isAbortError } from './api';
import type { ActivityEntry, Job, LibraryStorageDirectoriesResponse, TaskDefinition, TaskRun, User } from './api';
import { errorText } from './components';
import { useTranslation } from './i18n';
import { systemStatusApi } from './systemStatusApi';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { PlaybackSessionsPanel, usePlaybackPageGuards } from './PlaybackSessionsPanel';
import { directoryByteParts, directoryCapacity, directoryReadAccess, lowestAvailableDirectory } from './directoryCapacity';
import { backupPolicyApi } from './backupPolicyApi';
import './overview.css';
import './storage-observations.css';

interface OverviewPageProps {
  user: User;
  onUsers: () => void;
  onLibraries?: () => void;
  onItems?: () => void;
  onSessions?: () => void;
  onActivity?: () => void;
  onTasks?: () => void;
  onBackupTask?: (taskId: string) => void;
  onSettings?: () => void;
  onAnalysis?: () => void;
  onNavigationGuardChange?: UserNavigationGuardChange;
}
interface Metric { label: string; icon: string; side: string; value: string; unit: string; percent: number | null; note: string; warning?: boolean }
const pollInterval = 5000;
const requestTimeout = 15000;
const byteUnits = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
const noGuard: UserNavigationGuardChange = () => undefined;
const pageVisible = () => document.visibilityState !== 'hidden';
const loadOverview = (signal: AbortSignal) => adminApi.getOverview({ signal });
const loadSystemStatus = (signal: AbortSignal) => systemStatusApi.getStatus({ signal });
const loadDirectoryCapacity = (signal: AbortSignal) => adminApi.getLibraryStorageDirectories({ signal });
const loadRecentActivity = (signal: AbortSignal) => adminApi.getActivity({ Limit: 6 }, { signal });
const loadTasks = (signal: AbortSignal) => adminApi.getTasks({ signal });
const loadBackupPolicy = (signal: AbortSignal) => backupPolicyApi.get({ signal });
const loadJobs = (signal: AbortSignal) => adminApi.getJobs({ signal });
const loadSettings = (signal: AbortSignal) => adminApi.getSettings({ signal });
const activeState = (state: string) => ['pending', 'queued', 'running', 'stopping'].includes(state.toLowerCase());

function useOverviewResource<T>(load: (signal: AbortSignal) => Promise<T>, label: string, revision: number) {
  const [state, setState] = useState<{ data?: T; error: unknown; loading: boolean }>({ error: null, loading: true });
  useEffect(() => {
    let disposed = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let pending: AbortController | undefined;
    function cancelCurrent() { clearTimeout(timer); const current = pending; pending = undefined; current?.abort(); }
    async function poll() {
      if (disposed || !pageVisible() || pending) return;
      const controller = new AbortController(); pending = controller;
      setState((current) => ({ ...current, loading: true }));
      let deadline: ReturnType<typeof setTimeout> | undefined;
      let abort: (() => void) | undefined;
      try {
        // Also settle a request whose transport does not honor cancellation.
        const cancelled = new Promise<never>((_resolve, reject) => {
          abort = () => reject(controller.signal.reason);
          controller.signal.addEventListener('abort', abort, { once: true });
        });
        deadline = setTimeout(() => controller.abort(new Error(`${label} did not respond within 15 seconds. Refresh to try again.`)), requestTimeout);
        const data = await Promise.race([load(controller.signal), cancelled]);
        if (!disposed && pending === controller) setState({ data, error: null, loading: false });
      } catch (error) {
        if (!disposed && pending === controller) setState((current) => ({ ...current, error: isAbortError(error) ? current.error : error, loading: false }));
      } finally {
        clearTimeout(deadline);
        if (abort) controller.signal.removeEventListener('abort', abort);
        const current = pending === controller;
        if (current) pending = undefined;
        if (current && !disposed && pageVisible()) timer = setTimeout(() => { void poll(); }, pollInterval);
      }
    }
    function visibilityChanged() { if (!pageVisible()) { cancelCurrent(); setState((current) => ({ ...current, loading: false })); } else void poll(); }
    document.addEventListener('visibilitychange', visibilityChanged); void poll();
    return () => { disposed = true; document.removeEventListener('visibilitychange', visibilityChanged); cancelCurrent(); };
  }, [load, label, revision]);
  return state;
}

function Icon({ name, size = 16 }: { name: string; size?: number }) {
  const paths: Record<string, ReactNode> = {
    cpu: <><rect x="5" y="5" width="14" height="14" rx="2" /><rect x="9" y="9" width="6" height="6" rx="1" /><path d="M9 2v3m6-3v3M9 19v3m6-3v3M2 9h3m-3 6h3m14-6h3m-3 6h3" /></>,
    memory: <><rect x="3" y="6" width="18" height="11" rx="2" /><path d="M7 17v3m5-3v3m5-3v3M7 10v3m5-3v3m5-3v3" /></>,
    storage: <><path d="m4 4-2 9v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-2-9Z" /><path d="M2 13h20M6 17h.01M10 17h.01" /></>,
    transcode: <><path d="M3.3 19a10 10 0 1 1 17.4 0M12 14l4-4" /><circle cx="12" cy="14" r="1" /></>,
    next: <path d="m9 5 7 7-7 7" />,
    scan: <path d="M3 7V5a2 2 0 0 1 2-2h2m10 0h2a2 2 0 0 1 2 2v2m0 10v2a2 2 0 0 1-2 2h-2M7 21H5a2 2 0 0 1-2-2v-2m4-5h10" />,
    refresh: <><path d="M20 7v5h-5M4 17v-5h5" /><path d="M5 8a8 8 0 0 1 13-3l2 2M4 17l2 2a8 8 0 0 0 13-3" /></>,
    alert: <><path d="m10.3 4.2-7.7 13.3a2 2 0 0 0 1.7 3h15.4a2 2 0 0 0 1.7-3L13.7 4.2a2 2 0 0 0-3.4 0Z" /><path d="M12 9.5v4M12 17h.01" /></>,
    check: <path d="m5 12 4.5 4.5L19 7" />,
    clock: <><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>,
    stop: <rect x="6" y="6" width="12" height="12" rx="2" />,
    play: <path d="m8 4 13 8-13 8Z" />,
    info: <><circle cx="12" cy="12" r="9" /><path d="M12 11v6M12 7v.1" /></>,
  };
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name] ?? paths.info}</svg>;
}

function MetricCard({ metric, loading }: { metric: Metric; loading: boolean }) {
  const { t } = useTranslation();
  const percent = metric.percent == null ? null : Math.max(0, Math.min(100, metric.percent));
  return <section className="ov-metric" aria-label={`${t(metric.label)} ${t('status')}`}>
    <div className="ov-metric-heading"><Icon name={metric.icon} /><span>{t(metric.label)}</span><small>{metric.side}</small></div>
    <div className="ov-metric-value">{loading ? <span className="ov-skeleton" /> : <><strong aria-label={metric.value === '—' ? t('Not available') : undefined}>{metric.value}</strong><span>{metric.unit}</span></>}</div>
    <div className={`ov-progress${metric.icon === 'transcode' ? ' ov-progress-accent' : ''}`} role="progressbar" aria-label={`${t(metric.label)} ${t('utilization')}`} aria-valuenow={percent ?? undefined} aria-valuemin={0} aria-valuemax={100} aria-valuetext={percent == null ? t('Not available') : `${percent.toFixed(1)}%`}><i style={{ width: `${percent ?? 0}%` }} /></div>
    <div className={`ov-metric-note${metric.warning ? ' ov-warning' : ''}`} title={metric.note}>{loading ? t('Loading metrics…') : metric.note}</div>
  </section>;
}

function DirectoryCapacityMetric({ resource }: { resource: { data?: LibraryStorageDirectoriesResponse; error: unknown; loading: boolean } }) {
  const { t, locale, formatCount } = useTranslation();
  const directories = resource.data?.Items ?? [];
  const selected = lowestAvailableDirectory(directories);
  const capacity = directoryCapacity(selected);
  const parts = directoryByteParts(capacity.available, locale);
  const unreadable = directories.filter((directory) => directoryReadAccess(directory) === false).length;
  const unknown = directories.filter((directory) => directoryCapacity(directory).available == null).length;
  const stale = resource.error != null && resource.data != null;
  const warning = capacity.warning || unreadable > 0 || unknown > 0 || resource.error != null || !!selected && !capacity.complete;
  const percent = capacity.percent == null ? null : new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(capacity.percent);
  const note = [
    selected?.Path ?? t(resource.data?.Items.length === 0 ? 'No registered media directories' : 'Directory capacity unavailable'),
    percent == null ? selected ? t(capacity.used == null ? 'Used space unknown' : capacity.total == null ? 'Total capacity unknown' : 'Usage percentage unavailable') : '' : t('Used {percent}%', { percent }),
    unreadable > 0 ? t('{count} unreadable', { count: unreadable }) : '',
    unknown > 0 ? t('{count} available-space observations unknown', { count: unknown }) : '',
    selected && unknown > 0 ? t('Among directories with known available space') : '',
    stale ? t('Directory observations are stale') : '',
  ].filter(Boolean).join(' · ');
  return <section className="ov-metric ov-directory-capacity" aria-label={`${t('Directory with least available space')} ${t('status')}`} aria-busy={resource.loading} data-warning={warning} data-capacity-warning={capacity.warning} data-stale={stale}>
    <div className="ov-metric-heading"><Icon name="storage" /><span title={t('Directory with least available space')}>{t('Directory with least available space')}</span><small className={stale ? 'ov-warning' : undefined}>{stale ? t('Stale') : resource.data ? formatCount(directories.length, 'directory', 'directories') : ''}</small></div>
    <div className="ov-metric-value" title={capacity.available == null ? undefined : `${t('Available space')}: ${capacity.available.toString()} B`}>
      {!resource.data && resource.loading && resource.error == null ? <span className="ov-skeleton" /> : <><strong aria-label={capacity.available == null ? t('Not available') : undefined}>{parts.value}</strong><span>{parts.unit}</span></>}
    </div>
    {capacity.percent != null ? <div className="ov-progress" role="progressbar" aria-label={t('Directory usage for {path}', { path: selected!.Path })} aria-valuenow={capacity.percent} aria-valuemin={0} aria-valuemax={100} aria-valuetext={t('Used {percent}%', { percent: percent! })}><i style={{ width: `${capacity.percent}%` }} /></div> : <div className="ov-directory-empty-meter" aria-hidden="true" />}
    <div className={`ov-metric-note ov-directory-note${warning ? ' ov-warning' : ''}`} title={note}>{!resource.data && resource.loading && resource.error == null ? t('Checking directory capacity…') : note}</div>
  </section>;
}

function Notice({ error, stale = false, onRetry }: { error: unknown; stale?: boolean; onRetry: () => void }) {
  const { t } = useTranslation();
  return <div className="ov-notice" role="alert"><Icon name="alert" /><div>{t(errorText(error))}{stale && <small>{t('Showing the last successful update.')}</small>}</div><button type="button" className="ov-text-button" onClick={onRetry}>{t('Retry')}</button></div>;
}
function PanelHeading({ title, count, action, onAction }: { title: string; count?: ReactNode; action?: string; onAction?: () => void }) {
  const { t } = useTranslation();
  return <div className="ov-panel-heading"><h2>{t(title)}</h2>{count != null && <span className="ov-panel-count">{count}</span>}{action && onAction && <button type="button" className="ov-text-button" onClick={onAction}>{t(action)}<Icon name="next" size={13} /></button>}</div>;
}
function ActivityRow({ entry }: { entry: ActivityEntry }) {
  const { t, language } = useTranslation();
  const locale = language === 'zh' ? 'zh-CN' : 'en-US';
  const date = new Date(entry.Date), today = date.toDateString() === new Date().toDateString();
  const time = new Intl.DateTimeFormat(locale, today ? { hour: '2-digit', minute: '2-digit', hour12: false } : { month: 'short', day: 'numeric' }).format(date);
  const actor = entry.Actor.Name || t(entry.Actor.Kind === 'system' ? 'System' : entry.Actor.Kind === 'application_key' ? 'API key' : 'User');
  const tone = entry.Severity === 'Warn' ? 'warning' : ['Error', 'Fatal'].includes(entry.Severity) ? 'error' : entry.Action.endsWith('finished') ? 'success' : '';
  return <li className="ov-activity-row"><time dateTime={entry.Date} title={new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(date)}>{time}</time><i className={`ov-dot ${tone}`} aria-label={t(entry.Severity)} /><span title={entry.Name}>{entry.Name}</span><small title={actor}>{actor}</small></li>;
}
function StopDialog({ label, busy, onClose, onConfirm }: { label: string; busy: boolean; onClose: () => void; onConfirm: () => void }) {
  const { t } = useTranslation();
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => { dialog.current?.showModal(); return () => { dialog.current?.close(); }; }, []);
  return <dialog ref={dialog} className="ov-stop-dialog" aria-labelledby="ov-stop-title" aria-describedby="ov-stop-description" onCancel={(event) => { event.preventDefault(); if (!busy) onClose(); }}>
    <h2 id="ov-stop-title">{t('Stop this task?')}</h2><p id="ov-stop-description">{label}</p><p>{t('Items already scanned will be kept. Stopping may take a moment.')}</p>
    <div className="ov-dialog-actions"><button type="button" className="ov-button" disabled={busy} onClick={onClose}>{t('Keep running')}</button><button type="button" className="ov-button ov-button-danger" disabled={busy} onClick={onConfirm}>{busy ? t('Stopping…') : t('Stop task')}</button></div>
  </dialog>;
}

export function OverviewPage({ user, onUsers, onLibraries, onItems, onSessions, onActivity, onTasks, onBackupTask, onSettings, onAnalysis, onNavigationGuardChange = noGuard }: OverviewPageProps) {
  const { t, language, countLabel, formatCount } = useTranslation();
  const guards = usePlaybackPageGuards(onNavigationGuardChange);
  const locale = language === 'zh' ? 'zh-CN' : 'en-US';
  const text = (english: string, chinese: string) => language === 'zh' ? chinese : english;
  const [revision, setRevision] = useState(0);
  const overview = useOverviewResource(loadOverview, 'Server overview', revision);
  const telemetry = useOverviewResource(loadSystemStatus, 'System metrics', revision);
  const directoryObservations = useOverviewResource(loadDirectoryCapacity, 'Directory capacity', revision);
  const activity = useOverviewResource(loadRecentActivity, 'Recent activity', revision);
  const tasks = useOverviewResource(loadTasks, 'Tasks', revision);
  const backupPolicy = useOverviewResource(loadBackupPolicy, 'Automatic backup rules', revision);
  const jobs = useOverviewResource(loadJobs, 'Scan jobs', revision);
  const settings = useOverviewResource(loadSettings, 'Settings', revision);
  const [mutationError, setMutationError] = useState<unknown>(null);
  const [mutationNotice, setMutationNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [stopTarget, setStopTarget] = useState<{ id: string; kind: 'task' | 'job'; label: string } | null>(null);
  const mutation = useRef<AbortController | null>(null);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; mutation.current?.abort(); }; }, []);
  useUserDraftNavigation(false, busy, guards.page);
  const refresh = () => setRevision((value) => value + 1);
  const data = overview.data, status = telemetry.data;
  const formatNumber = (value: number, precision = 1) => new Intl.NumberFormat(locale, { maximumFractionDigits: precision }).format(value);
  const formatBytes = (value: number | null | undefined) => {
    if (value == null) return t('Not available');
    const index = value > 0 ? Math.min(Math.floor(Math.log(value) / Math.log(1024)), byteUnits.length - 1) : 0;
    return `${formatNumber(value / 1024 ** index)} ${byteUnits[index]}`;
  };
  const formatCapacity = (used: number | null | undefined, total: number | null | undefined) => {
    if (used == null || total == null) return { value: '—', unit: '', percent: null };
    const index = total > 0 ? Math.min(Math.floor(Math.log(total) / Math.log(1024)), byteUnits.length - 1) : 0;
    return { value: formatNumber(used / 1024 ** index), unit: `/ ${formatNumber(total / 1024 ** index)} ${byteUnits[index]}`, percent: total > 0 ? used / total * 100 : null };
  };
  const formatTime = (value: string, timezone?: string) => {
    try { return new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false, ...(timezone ? { timeZone: timezone } : {}) }).format(new Date(value)); }
    catch { return value; }
  };
  const cpu = status?.CPU, memory = status?.Memory, transcoding = status?.Transcoding;
  const registeredDirectories = directoryObservations.data?.Items ?? [];
  const unreadable = registeredDirectories.filter((directory) => directoryReadAccess(directory) === false);
  const nearlyFull = registeredDirectories.filter((directory) => directoryCapacity(directory).warning);
  const activeTranscodes = transcoding?.Available ? transcoding.Active : null, transcodeLimit = transcoding?.Available ? transcoding.Limit : null;
  const loadNote = cpu && [cpu.Load1, cpu.Load5, cpu.Load15].every((value) => value != null)
    ? `${t('Load')} ${formatNumber(cpu.Load1!, 2)} · ${formatNumber(cpu.Load5!, 2)} · ${formatNumber(cpu.Load15!, 2)}` : t('Load averages not available');
  const metricCards: Metric[] = [
    { label: 'CPU', icon: 'cpu', side: status ? text(formatCount(status.Host.CPUCount, 'thread', 'threads'), `${formatNumber(status.Host.CPUCount, 0)} 线程`) : '',
      value: cpu?.UsagePercent == null ? '—' : formatNumber(cpu.UsagePercent, 0), unit: cpu?.UsagePercent == null ? '' : '%', percent: cpu?.UsagePercent ?? null,
      note: cpu?.UsagePercent == null ? `${t('CPU usage not available')} · ${loadNote}` : loadNote },
    { label: 'Memory', icon: 'memory', side: memory?.TotalBytes == null ? '' : formatBytes(memory.TotalBytes), ...formatCapacity(memory?.UsedBytes, memory?.TotalBytes),
      note: memory ? `${t('Cache')} ${formatBytes(memory.CachedBytes)} · ${t('Swap')} ${formatBytes(memory.SwapUsedBytes)}` : t('Not available') },
    { label: 'Transcoding', icon: 'transcode', side: '', value: activeTranscodes == null ? '—' : formatNumber(activeTranscodes, 0), unit: transcodeLimit == null ? '' : text(`/ ${formatCount(transcodeLimit, 'job', 'jobs')}`, `/ ${transcodeLimit} 路`),
      percent: activeTranscodes != null && transcodeLimit != null && transcodeLimit > 0 ? activeTranscodes / transcodeLimit * 100 : null,
      note: transcoding?.Available ? text(`${transcoding.HardwareActive ?? '—'} hardware · ${transcoding.SoftwareActive ?? '—'} software`, `硬件 ${transcoding.HardwareActive ?? '—'} · 软件 ${transcoding.SoftwareActive ?? '—'}`) : status && !transcoding?.Available && transcoding?.Limit === 0 ? t('Transcoding is disabled') : t('Transcoding metrics not available') },
  ];
  const databaseReady = !!data && ['ok', 'ready', 'connected', 'healthy'].includes(data.Database.Status.toLowerCase());
  const elapsed = status ? Math.floor(status.UptimeSeconds / 60) : 0, days = Math.floor(elapsed / 1440), hours = Math.floor(elapsed / 60) % 24;
  const uptime = days > 0 ? text(`Up ${days}d ${hours}h`, `已运行 ${days} 天 ${hours} 小时`) : hours > 0 ? text(`Up ${hours}h ${elapsed % 60}m`, `已运行 ${hours} 小时 ${elapsed % 60} 分钟`) : text(`Up ${elapsed}m`, `已运行 ${elapsed} 分钟`);
  const visibleTasks = tasks.data?.Items.filter((task) => !task.IsHidden) ?? [];
  const activeTasks = visibleTasks.filter((task) => task.CurrentRun && activeState(task.CurrentRun.State));
  const activeJobs = (jobs.data?.Items ?? []).filter((job) => activeState(job.Status));
  const separateJobs = activeTasks.some((task) => task.Key === 'library.scan') ? [] : activeJobs;
  const scheduledTasks = visibleTasks.filter((task) => task.Enabled && task.NextRunAt).sort((a, b) => Date.parse(a.NextRunAt!) - Date.parse(b.NextRunAt!)).slice(0, 3);
  const failedTasks = visibleTasks.filter((task) => task.LastRun && ['failed', 'interrupted'].includes(task.LastRun.State));
  const failedJobs = (jobs.data?.Items ?? []).filter((job) => ['failed', 'error'].includes(job.Status.toLowerCase()));
  const pendingNetwork = settings.data?.Runtime?.Network;
  const attention: { key: string; title: string; detail: string; label: string; action?: () => void; tone: string }[] = [
    ...(backupPolicy.data?.Available && !backupPolicy.data.Enabled && !backupPolicy.error ? [{ key: 'backup-rules', title: t('No automatic backup rules'), detail: t('The backup task has no active trigger rules. Backups must be started manually.'), label: t('Trigger rules'), action: onBackupTask && backupPolicy.data.TaskId ? () => onBackupTask(backupPolicy.data!.TaskId) : onTasks, tone: 'warning' }] : []),
    ...(backupPolicy.error ? [{ key: 'backup-rules-unknown', title: t('Automatic backup rules check unavailable'), detail: t(backupPolicy.data ? 'Showing the last successful backup rules check.' : 'Automatic backup trigger rules are unknown.'), label: t('Refresh'), action: refresh, tone: 'warning' }] : []),
    ...(backupPolicy.data && !backupPolicy.data.Available && !backupPolicy.error ? [{ key: 'backup-unavailable', title: t('The automatic backup service is unavailable.'), detail: t(backupPolicy.data.UnavailableReason || 'Automatic backup trigger rules are unknown.'), label: t('View'), action: onTasks, tone: 'warning' }] : []),
    ...unreadable.map((directory) => ({ key: directory.Path, title: t(directory.Available ? 'Media directory cannot be read' : 'Media directory unavailable'), detail: directory.Path, label: t('Check bindings'), action: onLibraries, tone: 'warning' })),
    ...nearlyFull.filter((directory) => !unreadable.includes(directory)).map((directory) => ({ key: `capacity:${directory.Path}`, title: t('Media directory is nearly full'), detail: `${directory.Path} · ${t('Used {percent}%', { percent: formatNumber(directoryCapacity(directory).percent!) })}`, label: t('View'), action: onLibraries, tone: 'warning' })),
    ...(pendingNetwork?.RestartRequired ? [{ key: 'network', title: t('Network changes require a restart'), detail: `${pendingNetwork.Active ? `${pendingNetwork.Active.BoundHost || '*'}:${pendingNetwork.Active.HttpPort}` : t('Not available')} → ${pendingNetwork.Desired.BindHost || '*'}:${pendingNetwork.Desired.HttpPort}`, label: t('View'), action: onSettings, tone: 'warning' }] : []),
    ...failedTasks.slice(0, 2).map((task) => ({ key: task.Id, title: task.Name, detail: task.LastRun!.ErrorMessage || t(task.LastRun!.State), label: t('Open tasks'), action: task.Key.includes('analysis') ? onAnalysis ?? onTasks : onTasks, tone: 'error' })),
    ...(!failedTasks.some((task) => task.Key === 'library.scan') ? failedJobs.slice(0, 1).map((job) => ({ key: job.Id, title: t('Library scan failed'), detail: job.Error || job.LibraryId, label: t('Open tasks'), action: onTasks, tone: 'error' })) : []),
    ...(data && !databaseReady ? [{ key: 'database', title: t('Database needs attention'), detail: data.Database.Status, label: t('View'), action: onSettings, tone: 'warning' }] : []),
  ];
  const checksIncomplete = !status || !directoryObservations.data || !tasks.data || !jobs.data || !settings.data || !pendingNetwork || !backupPolicy.data || !backupPolicy.data.Available
    || registeredDirectories.some((directory) => !directoryCapacity(directory).complete || directoryReadAccess(directory) == null || directory.Writable == null)
    || [telemetry.error, directoryObservations.error, tasks.error, jobs.error, settings.error, backupPolicy.error].some((error) => error != null);
  const runningCount = activeTasks.filter((task) => task.CurrentRun && !['pending', 'queued'].includes(task.CurrentRun.State)).length + separateJobs.filter((job) => !['pending', 'queued'].includes(job.Status)).length;
  const queuedCount = activeTasks.length + separateJobs.length - runningCount;
  const scanTask = visibleTasks.find((task) => task.Key === 'library.scan');
  const scanning = !!scanTask?.CurrentRun && activeState(scanTask.CurrentRun.State) || activeJobs.length > 0;

  async function mutate(action: (signal: AbortSignal) => Promise<unknown>, message: string) {
    if (mutation.current || uncertain) return;
    const controller = new AbortController(); mutation.current = controller;
    setBusy(true); setMutationError(null); setMutationNotice('');
    let deadline: ReturnType<typeof setTimeout> | undefined, timedOut = false;
    try {
      const timeout = new Promise<never>((_resolve, reject) => {
        deadline = setTimeout(() => { timedOut = true; controller.abort(); reject(new ApiError('The request timed out. Check task status before trying again.', { code: 'network_error' })); }, requestTimeout);
      });
      await Promise.race([action(controller.signal), timeout]);
      if (!mounted.current) return;
      setMutationNotice(message); setStopTarget(null); refresh();
    } catch (error) {
      if (!mounted.current) return;
      if (timedOut || !isAbortError(error)) {
        setMutationError(error);
        setStopTarget(null);
        setUncertain(timedOut || error instanceof ApiError && ['network_error', 'invalid_response'].includes(error.code));
      }
    } finally {
      clearTimeout(deadline);
      if (mutation.current === controller) mutation.current = null;
      if (mounted.current) setBusy(false);
    }
  }
  function startScan() {
    if (!scanTask || scanning || tasks.error != null || busy || uncertain) return;
    const requestId = globalThis.crypto.randomUUID();
    void mutate((signal) => adminApi.startTask(scanTask.Id, requestId, { signal }), t('Library scan requested. Check tasks for progress.'));
  }
  function stopTask() {
    if (!stopTarget) return;
    const target = stopTarget;
    void mutate((signal) => target.kind === 'task' ? adminApi.cancelTaskRun(target.id, { signal }) : adminApi.cancelJob(target.id, { signal }), t('Stop requested. Scanned items are kept.'));
  }
  function taskRow(task: TaskDefinition) {
    const run = task.CurrentRun!;
    return <TaskRow key={run.Id} label={task.Name} run={run} scan={task.Key === 'library.scan'} disabled={busy || uncertain} onStop={() => setStopTarget({ id: run.Id, kind: 'task', label: task.Name })} />;
  }
  function jobRow(job: Job) {
    return <TaskRow key={job.Id} label={t('Scan media libraries')} run={{ State: job.Status, Scanned: job.Scanned, Added: job.Added, Updated: job.Updated, StartedAt: job.StartedAt }} scan disabled={busy || uncertain} onStop={() => setStopTarget({ id: job.Id, kind: 'job', label: t('Scan media libraries') })} />;
  }

  return <div className="overview-page" aria-label={t('Server overview')}>
    <div className="ov-page-heading"><div><h1>{t('Overview')}</h1><div className="ov-server-line"><strong>{data?.Server.Name ?? t(overview.error ? 'Server unavailable' : 'Connecting…')}</strong>{data && <span className="ov-mono">goby {data.Server.Version}</span>}{data && <span className={overview.error || !databaseReady ? 'ov-warning' : undefined}><i className={`ov-dot${databaseReady && !overview.error ? ' success' : ' warning'}`} />{t(overview.error ? 'Database status outdated' : databaseReady ? 'Database healthy' : 'Database needs attention')}</span>}{status && <span>{uptime}{telemetry.error ? ` · ${t('Last update')}` : ''}</span>}</div></div><div className="ov-heading-actions"><button type="button" className="ov-icon-button" aria-label={t('Refresh overview')} title={t('Refresh overview')} onClick={refresh}><Icon name="refresh" size={17} /></button><button type="button" className="ov-button" onClick={startScan} disabled={busy || uncertain || scanning || !scanTask || tasks.error != null} title={!scanTask ? t('Configure a library scan task first.') : undefined}><Icon name="scan" size={15} />{busy && !stopTarget ? t('Requesting…') : t('Scan all libraries')}</button></div></div>
    {overview.error != null && <Notice error={overview.error} stale={!!data} onRetry={refresh} />}
    {mutationError != null && <Notice error={mutationError} onRetry={refresh} />}
    {uncertain && <div className="ov-notice" role="status"><Icon name="info" /><span>{t('The task outcome is unknown. Check task status before making another request.')}</span>{onTasks && <button type="button" className="ov-text-button" onClick={() => { setStopTarget(null); onTasks(); }}>{t('Open tasks')}<Icon name="next" size={13} /></button>}</div>}
    {mutationNotice && <div className="ov-success-notice" role="status"><Icon name="check" /><span>{mutationNotice}</span>{onTasks && <button type="button" className="ov-text-button" onClick={onTasks}>{t('Open tasks')}<Icon name="next" size={13} /></button>}</div>}
    {telemetry.error != null && <Notice error={telemetry.error} stale={!!status} onRetry={refresh} />}
    {directoryObservations.error != null && <Notice error={directoryObservations.error} stale={!!directoryObservations.data} onRetry={refresh} />}
    <div className="ov-metrics">{metricCards.slice(0, 2).map((metric) => <MetricCard key={metric.label} metric={metric} loading={!status && telemetry.loading && telemetry.error == null} />)}<DirectoryCapacityMetric resource={directoryObservations} />{metricCards.slice(2).map((metric) => <MetricCard key={metric.label} metric={metric} loading={!status && telemetry.loading && telemetry.error == null} />)}</div>
    <div className="ov-columns"><div className="ov-column">
      <PlaybackSessionsPanel compact onAllSessions={onSessions} onNavigationGuardChange={guards.playback} />
      <section className="ov-panel" aria-label={t('Recent activity')}><PanelHeading title="Recent activity" action="Activity & logs" onAction={onActivity} />{activity.error != null && <Notice error={activity.error} stale={!!activity.data} onRetry={refresh} />}{!activity.data && activity.loading && activity.error == null && <div className="ov-loading-rows" role="status" aria-label={t('Loading recent activity')}>{Array.from({ length: 5 }, (_, i) => <span className="ov-skeleton" key={i} />)}</div>}{activity.data?.Items.length === 0 && <div className="ov-empty-text">{t('No recorded activity')}</div>}{activity.data && activity.data.Items.length > 0 && <ul className="ov-activity" aria-label={t('Recent activity records')}>{activity.data.Items.map((entry) => <ActivityRow key={entry.Id} entry={entry} />)}</ul>}</section>
    </div><div className="ov-column">
      <section className="ov-panel" aria-label={t('Needs attention')}><PanelHeading title="Needs attention" count={attention.length > 0 ? attention.length : undefined} />{attention.map((item) => <div className={`ov-attention-row ${item.tone}`} key={item.key}><Icon name="alert" /><div><strong>{item.title}</strong><small title={item.detail}>{item.detail}</small></div>{item.action && <button type="button" className="ov-text-button" onClick={item.action}>{item.label}</button>}</div>)}{attention.length === 0 && <div className={`ov-calm${checksIncomplete ? ' ov-calm-incomplete' : ''}`}><span className="ov-calm-icon"><Icon name={checksIncomplete ? 'info' : 'check'} size={15} /></span><div><strong>{t(checksIncomplete ? 'Checks incomplete' : 'All checks passed')}</strong><p>{t(checksIncomplete ? 'Some status checks are unavailable. Refresh to try again.' : 'No failed tasks, unreadable directories, or pending network changes.')}</p></div></div>}{(tasks.error != null || jobs.error != null || settings.error != null) && <div className="ov-check-retry"><button type="button" className="ov-text-button" onClick={refresh}>{t('Retry status checks')}<Icon name="refresh" size={13} /></button></div>}</section>
      <section className="ov-panel" aria-label={t('Tasks')}><PanelHeading title="Tasks" count={tasks.data && jobs.data ? text(`${runningCount} running · ${queuedCount} queued`, `${runningCount} 运行 · ${queuedCount} 排队`) : '—'} action="Tasks and schedules" onAction={onTasks} />{tasks.error != null && <Notice error={tasks.error} stale={!!tasks.data} onRetry={refresh} />}{jobs.error != null && <Notice error={jobs.error} stale={!!jobs.data} onRetry={refresh} />}{!tasks.data && tasks.loading && <div className="ov-loading-rows" role="status" aria-label={t('Loading tasks')}><span className="ov-skeleton" /><span className="ov-skeleton" /></div>}{activeTasks.map(taskRow)}{separateJobs.map(jobRow)}{tasks.data && jobs.data && activeTasks.length + separateJobs.length === 0 && <div className="ov-empty-text ov-task-empty"><Icon name="check" />{t('No active tasks')}</div>}<div className="ov-schedule-title">{t('Scheduled')}</div>{scheduledTasks.map((task) => <button type="button" className="ov-schedule-row" key={task.Id} onClick={onTasks} disabled={!onTasks}><Icon name="clock" size={14} /><span>{task.Name}</span><time dateTime={task.NextRunAt!} title={task.ScheduleTimezone}>{formatTime(task.NextRunAt!, task.ScheduleTimezone)}</time></button>)}{tasks.data && scheduledTasks.length === 0 && <div className="ov-empty-text ov-schedule-empty">{t('No scheduled tasks')}</div>}</section>
    </div></div>
    {data && <footer className="ov-footer" aria-label={t('Server counts')}><span>{text(`Signed in as ${user.Name}`, `已登录为 ${user.Name}`)}</span><div>{[{ singular: 'user', label: 'users', aria: 'users', count: data.Counts.Users, action: onUsers }, { singular: 'library', label: 'libraries', aria: 'libraries', count: data.Counts.Libraries, action: onLibraries }, { singular: 'media item', label: 'media items', aria: 'media items', count: data.Counts.Items, action: onItems ?? onLibraries }, { singular: 'active login', label: 'active logins', aria: 'active sessions', count: data.Counts.ActiveSessions, action: onSessions }].map((item) => <button type="button" key={item.label} className="ov-text-button" onClick={item.action} disabled={!item.action} aria-label={`${t('View')} ${t(item.aria)}`}><span className="ov-mono">{formatNumber(item.count, 0)}</span>{countLabel(item.count, item.singular, item.label)}</button>)}</div></footer>}
    {stopTarget && <StopDialog label={stopTarget.label} busy={busy} onClose={() => setStopTarget(null)} onConfirm={stopTask} />}
  </div>;
}

function TaskRow({ label, run, scan, disabled, onStop }: { label: string; run: Pick<TaskRun, 'State' | 'Scanned' | 'Added' | 'Updated' | 'StartedAt'> | { State: string; Scanned: number; Added: number; Updated: number; StartedAt: string | null }; scan: boolean; disabled: boolean; onStop: () => void }) {
  const { t, language } = useTranslation();
  const locale = language === 'zh' ? 'zh-CN' : 'en-US';
  const number = (value: number) => new Intl.NumberFormat(locale).format(value);
  const running = run.State === 'running' || run.State === 'stopping';
  return <div className="ov-task-row"><div className="ov-task-main"><i className={`ov-dot${running ? ' accent' : ' queued'}`} /><strong>{label}</strong><span className={running ? 'ov-accent' : ''}>{t(({ pending: 'Queued', queued: 'Queued', running: 'Running', stopping: 'Stopping' } as Record<string, string>)[run.State] ?? run.State)}</span>{run.State !== 'stopping' && <button type="button" className="ov-button ov-button-small" disabled={disabled} onClick={onStop}><Icon name="stop" size={13} />{t('Stop')}</button>}</div>{running && <div className="ov-task-indeterminate" role="status" aria-label={t('Task running')}><i /></div>}{scan && <div className="ov-task-counts"><span>{t('Scanned')} {number(run.Scanned)}</span><span>{t('Added')} {number(run.Added)}</span><span>{t('Updated')} {number(run.Updated)}</span></div>}{run.StartedAt && <div className="ov-task-started"><time dateTime={run.StartedAt}>{new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(run.StartedAt))}</time> {t('started')}</div>}</div>;
}
