import { useEffect, useRef, useState } from 'react';
import type { MouseEvent, ReactNode } from 'react';
import { adminApi, ApiError, isAbortError } from './api';
import type { TaskRun } from './api';
import { AttentionChecks } from './AttentionChecks';
import { databaseHealthy, libraryScanTask, observationTime } from './dashboardAttention';
import { dashboardTargetURL } from './dashboardNavigation';
import type { DashboardTarget } from './dashboardNavigation';
import { directoryCapacity, directoryReadAccess } from './directoryCapacity';
import { errorText } from './components';
import { useTranslation } from './i18n';
import { CheckCircleRounded, FolderOutlined, MovieOutlined, MusicNoteOutlined, NavigateNextRounded, RefreshRounded, ScanOutlined, ScheduleOutlined, TvRounded, WarningAmberRounded } from './icons';
import { indexLatestLibraryScans, recentScanJobLimit, scanFailed } from './latestLibraryScans';
import { forgetTaskAdmission, keepTaskAdmission, readTaskAdmission, taskAdmissionId } from './taskAdmissionReceipt';
import type { DashboardStatus, Observation } from './useDashboardStatus';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import './overview.css';

interface OverviewProps {
  status: DashboardStatus;
  currentUserId: string;
  onNavigate: (target: DashboardTarget, event?: MouseEvent<HTMLAnchorElement>) => void;
  onNavigationGuardChange: UserNavigationGuardChange;
}
type Tone = 'ok' | 'warning' | 'error' | 'unknown' | 'accent';
const tasksTarget: DashboardTarget = { page: 'tasks' };
const libraryTarget: DashboardTarget = { page: 'libraries' };
const taskActive = (state: string) => ['pending', 'queued', 'running', 'stopping'].includes(state.toLowerCase());

export function formatUptime(seconds: number, locale: string): string {
  const minutes = Math.floor(seconds / 60);
  const days = Math.floor(minutes / 1440), hours = Math.floor(minutes / 60) % 24;
  const format = (value: number, unit: 'day' | 'hour' | 'minute') => new Intl.NumberFormat(locale, { style: 'unit', unit, unitDisplay: 'long' }).format(value);
  return days ? [format(days, 'day'), hours ? format(hours, 'hour') : ''].filter(Boolean).join(' ')
    : hours ? [format(hours, 'hour'), minutes % 60 ? format(minutes % 60, 'minute') : ''].filter(Boolean).join(' ')
      : format(minutes, 'minute');
}

function PanelHeading({ title, count, children }: { title: string; count?: string; children?: ReactNode }) {
  return <div className="ov-panel-heading"><h2>{title}</h2>{count != null && <span className="ov-panel-count">{count}</span>}{children}</div>;
}

function ResourceNote({ observations }: { observations: readonly Observation<unknown>[] }) {
  const { t } = useTranslation();
  const unavailable = observations.some((resource) => resource.error != null);
  const stale = observations.some((resource) => resource.error != null && resource.data !== undefined);
  const loading = observations.some((resource) => resource.data === undefined && resource.loading);
  if (!unavailable && !loading) return null;
  return <p className="ov-panel-note" role="status">{t(unavailable ? stale ? 'Showing the last successful check.' : 'This information could not be loaded.' : 'Loading…')}</p>;
}

function SuccessfulBackupNote({ observation }: { observation: Observation<string | null> }) {
  const { t, locale } = useTranslation();
  const { data: createdAt, error } = observation;
  if (createdAt === undefined) return t(error != null ? 'Could not check the latest successful backup.' : 'Checking the latest successful backup…');
  const [before, after] = t('Latest successful backup: {time}').split('{time}');
  const date = createdAt && new Intl.DateTimeFormat(locale, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(new Date(createdAt));
  return <>{createdAt ? <>{before}<time dateTime={createdAt}>{date}</time>{after}</> : t('No successful backups are retained.')}
    {error != null && <> · {t('Showing the last successful check.')}</>}</>;
}

function TaskProgress({ label, run }: { label: string; run: Pick<TaskRun, 'State' | 'TotalChildren' | 'TerminalChildren' | 'Scanned'> }) {
  const { t, locale } = useTranslation();
  const count = (value: number) => value.toLocaleString(locale);
  const running = run.State !== 'pending';
  const progress = run.TotalChildren > 0 ? `${count(run.TerminalChildren)} / ${count(run.TotalChildren)}` : '';
  const state = t(run.State === 'pending' ? 'Queued' : run.State === 'stopping' ? 'Stopping' : 'Running');
  const description = progress ? t('{done} of {total} work items finished', { done: count(run.TerminalChildren), total: count(run.TotalChildren) }) : state;
  return <div className="ov-task-row"><div className="ov-task-main"><i className="ov-dot" data-tone={running ? 'accent' : 'unknown'} aria-hidden="true" />
    <strong>{label}</strong><span className="ov-task-value">{progress || (run.Scanned > 0 ? t('Scanned {count}', { count: count(run.Scanned) }) : state)}</span>
  </div>{(running || progress) && <progress className="ov-task-progress" max={progress ? run.TotalChildren : undefined} value={progress ? run.TerminalChildren : undefined} aria-label={`${label} · ${t('Work progress')}`} aria-valuetext={description} />}</div>;
}

export function OverviewPage({ status, currentUserId, onNavigate, onNavigationGuardChange }: OverviewProps) {
  const { t, locale } = useTranslation();
  const { overview, system, directories, backup, successfulBackup, tasks, jobs, libraries, settings, attention, refresh } = status;
  const [busy, setBusy] = useState(false);
  const [issue, setIssue] = useState<unknown>();
  const [uncertain, setUncertain] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const mutation = useRef<AbortController | undefined>(undefined);
  const number = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 });
  const count = (value: number) => number.format(value);
  const go = (target: DashboardTarget) => (event: MouseEvent<HTMLAnchorElement>) => onNavigate(target, event);
  const link = (target: DashboardTarget, label: string) => <a className="ov-text-button" href={dashboardTargetURL(target)} onClick={go(target)}>{t(label)}<NavigateNextRounded /></a>;
  useUserDraftNavigation(false, busy, onNavigationGuardChange);
  useEffect(() => () => mutation.current?.abort(), []);

  const allLibraries = libraries.data?.Items ?? [];
  const scanJobs = jobs.data?.Items ?? [];
  const recent = Object.values(indexLatestLibraryScans(allLibraries, scanJobs, true))
    .sort((a, b) => Date.parse(b.FinishedAt!) - Date.parse(a.FinishedAt!) || a.Id.localeCompare(b.Id)).slice(0, 6);
  const libraryIndex = new Map(allLibraries.map((library) => [library.Id, library]));
  const activeJobs = scanJobs.filter((job) => libraryIndex.has(job.LibraryId) && taskActive(job.Status));
  const allTasks = tasks.data?.Items ?? [];
  const activeTasks = allTasks.flatMap((task) => task.CurrentRun && taskActive(task.CurrentRun.State) ? [{ task, run: task.CurrentRun }] : []);
  const scanTask = allTasks.find(libraryScanTask);
  const previousAdmission = Boolean(scanTask && readTaskAdmission(currentUserId, scanTask.Id));
  const showUnconfirmed = !busy && (uncertain || previousAdmission);
  const standaloneScans = activeTasks.some(({ task }) => libraryScanTask(task)) ? [] : activeJobs;
  const scanQueued = Boolean(scanTask?.CurrentRun && taskActive(scanTask.CurrentRun.State)) || activeJobs.length > 0;
  const nextScan = allTasks.filter((task) => libraryScanTask(task) && task.Enabled && task.NextRunAt)
    .sort((a, b) => Date.parse(a.NextRunAt!) - Date.parse(b.NextRunAt!))[0];

  const uptime = system.data && t('Up for {duration}', { duration: formatUptime(system.data.UptimeSeconds, locale) });
  const db = overview.data?.Database;
  const storage = [...new Map((directories.data?.Items ?? []).map((directory) => [directory.Path, directory])).values()];
  const full = storage.filter((directory) => directoryCapacity(directory).warning).length;
  const unreadable = storage.filter((directory) => directoryReadAccess(directory) === false).length;
  const fullest = storage.map((directory) => ({ path: directory.Path, capacity: directoryCapacity(directory) }))
    .filter(({ capacity }) => capacity.percent != null)
    .sort((a, b) => {
      // Compare exact ratios even when both observations display the same tenth.
      const difference = b.capacity.used! * a.capacity.total! - a.capacity.used! * b.capacity.total!;
      return difference > 0n ? 1 : difference < 0n ? -1 : a.path.localeCompare(b.path);
    })[0];
  const transcode = system.data?.Transcoding;
  const hardware = settings.data?.Runtime?.Effective.Hardware;
  const hardwareConfigured = hardware && [hardware.Decode, hardware.Encode].some((mode) => mode === 'vaapi');
  const hardwareDevice = settings.data?.Runtime?.Hardware.Devices.find(({ DeviceId, Available }) => Available && DeviceId === hardware?.DeviceId);
  const acceleration = transcode?.Available && hardwareConfigured && settings.error == null ? t('Configured: {hardware}', { hardware: hardwareDevice?.Label || 'VA-API' }) : '';
  const cells: { label: string; value: string; note?: ReactNode; tone: Tone; target: DashboardTarget; observation: Observation<unknown> }[] = [
    { label: 'Service', value: t(system.data ? 'Running normally' : 'Unknown'), note: uptime, tone: system.data ? 'ok' : 'unknown', target: { page: 'settings' }, observation: system },
    { label: 'Database', value: t(db?.Status ? databaseHealthy(db.Status) ? 'Normal' : 'Database needs attention' : 'Unknown'),
      note: db ? [db.Engine, t(databaseHealthy(db.Status) ? 'Connected' : db.Status)].filter(Boolean).join(' · ') : undefined,
      tone: db?.Status ? databaseHealthy(db.Status) ? 'ok' : 'error' : 'unknown', target: { page: 'settings' }, observation: overview },
    { label: 'Storage', value: t(full === 1 ? '{count} directory low on space' : full > 1 ? '{count} directories low on space' : unreadable ? 'Directory access needs attention'
      : !directories.data || attention.unknownDirectories ? 'Unknown' : storage.length ? 'Normal' : 'No registered media directories', { count: count(full) }),
      note: fullest ? `${fullest.path} · ${t('Used {percent}%', { percent: count(fullest.capacity.percent!) })}` : undefined,
      tone: full || unreadable ? 'warning' : !directories.data || attention.unknownDirectories ? 'unknown' : 'ok', target: libraryTarget, observation: directories },
    { label: 'Transcoding', value: t(transcode?.Available ? 'Available' : transcode?.Limit === 0 ? 'Disabled' : 'Not available'),
      note: transcode?.Available ? [acceleration || (transcode.HardwareActive != null && t('Hardware jobs: {count}', { count: count(transcode.HardwareActive) })),
        transcode.Active != null && (transcode.Limit != null ? t('{active} / {limit} slots', { active: count(transcode.Active), limit: count(transcode.Limit) }) : t('{count} active', { count: count(transcode.Active) }))].filter(Boolean).join(' · ') : undefined,
      tone: transcode?.Available ? 'ok' : 'unknown', target: { page: 'settings-transcode' }, observation: system },
    { label: 'Backups', value: t(backup.data?.Available ? backup.data.Enabled ? 'Automatic backup scheduled' : 'No automatic backup rules' : 'Unknown'),
      note: <SuccessfulBackupNote observation={successfulBackup} />,
      tone: backup.data?.Available ? backup.data.Enabled ? 'ok' : 'warning' : 'unknown', target: { page: 'backups' }, observation: backup },
  ];

  async function startScan() {
    if (!scanTask?.Enabled || scanQueued || tasks.error != null || jobs.error != null || mutation.current || showUnconfirmed) return;
    const requestId = taskAdmissionId();
    keepTaskAdmission(currentUserId, scanTask.Id, requestId);
    const controller = new AbortController();
    const deadline = AbortSignal.timeout(15_000);
    const signal = AbortSignal.any([controller.signal, deadline]);
    mutation.current = controller;
    setBusy(true); setIssue(undefined); setSubmitted(false);
    try {
      await adminApi.startTask(scanTask.Id, requestId, { signal });
      if (!controller.signal.aborted) { forgetTaskAdmission(currentUserId, scanTask.Id); setSubmitted(true); refresh(); }
    } catch (error) {
      if (controller.signal.aborted) return;
      if (!isAbortError(error) || deadline.aborted) {
        const unknown = deadline.aborted || !(error instanceof ApiError) || error.status >= 500 || error.status === 0 || ['network_error', 'invalid_response', 'session_changed'].includes(error.code);
        if (!unknown) forgetTaskAdmission(currentUserId, scanTask.Id);
        setIssue(deadline.aborted ? new ApiError('The request timed out. Check task status before trying again.', { code: 'network_error' }) : error);
        setUncertain(unknown);
        refresh();
      }
    } finally {
      if (mutation.current === controller) mutation.current = undefined;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  return <div className="overview-page" aria-label={t('Server overview')}>
    <div className="ov-page-heading"><div><h1>{t('Overview')}</h1><div className="ov-server-line"><strong>{overview.data?.Server.Name ?? t(overview.error ? 'Server unavailable' : 'Connecting…')}</strong>
      {overview.data && <span className="mono">goby {overview.data.Server.Version}</span>}{uptime && <span>{uptime}</span>}</div></div>
      <div className="ov-heading-actions"><button type="button" className="ov-icon-button" aria-label={t('Refresh overview')} title={t('Refresh overview')} onClick={refresh}><RefreshRounded /></button>
        <button type="button" className="ov-button" onClick={() => void startScan()} disabled={busy || showUnconfirmed || scanQueued || (submitted && tasks.loading) || !scanTask?.Enabled || tasks.error != null || jobs.error != null}><ScanOutlined />{t(busy ? 'Requesting…' : 'Scan all libraries')}</button></div>
    </div>
    {(issue != null || showUnconfirmed) && <div className="ov-notice" role="alert"><WarningAmberRounded /><span>{issue != null && t(errorText(issue))}{showUnconfirmed && <small>{t('The task outcome is unknown. Check task status before making another request.')}</small>}</span>{link(tasksTarget, 'Open tasks')}</div>}
    {submitted && <div className="ov-success-notice" role="status"><CheckCircleRounded /><span>{t('Library scan requested. Check tasks for progress.')}</span>{link(tasksTarget, 'Open tasks')}</div>}
    <section className="ov-panel ov-status-strip" aria-label={t('Service status')}>{cells.map((cell) => <a key={cell.label} className="ov-service-status" href={dashboardTargetURL(cell.target)} onClick={go(cell.target)} data-stale={cell.observation.data !== undefined && cell.observation.error != null}>
      <i className="ov-dot" data-tone={cell.observation.error != null ? 'warning' : cell.tone} aria-hidden="true"/><span><span className="ov-status-label">{t(cell.label)}</span>
        <strong>{cell.observation.data === undefined && cell.observation.loading ? t('Checking…') : cell.value}</strong>{cell.note && <small>{cell.note}</small>}{cell.observation.error != null && <small>{t(cell.observation.data ? 'Stale' : 'Check unavailable')}</small>}</span>
    </a>)}</section>
    <div className="ov-columns">
      <section className="ov-panel" aria-label={t('To do')} aria-busy={attention.loading}>
        <PanelHeading title={t('To do')} count={t('{count} total, {critical} critical', { count: count(attention.entries.length), critical: count(attention.critical) })} />
        {attention.entries.map((entry) => <div className="ov-attention-row" key={entry.id} data-attention-id={entry.id} data-check={entry.check} data-severity={entry.severity} data-stale={entry.stale}>
          <WarningAmberRounded /><div><div className="ov-attention-title"><strong>{entry.title}</strong><span className="ov-chip">{t(entry.severity === 'critical' ? 'Critical' : 'Notice')} · {t(entry.category)}</span></div>
            {entry.detail && <small>{entry.detail}</small>}{entry.stale && <small>{t('Stale')}</small>}</div>
          {entry.action && <a className="ov-button ov-button-small" href={dashboardTargetURL(entry.action.target)} onClick={go(entry.action.target)}>{t(entry.action.label)}</a>}
        </div>)}
        <AttentionChecks checks={attention.checks} />
        {attention.entries.length === 0 && attention.checks.length === 0 && <div className="ov-calm"><CheckCircleRounded /><div><strong>{t('Nothing needs attention')}</strong><p>{t('Storage, scan, task and configuration checks passed.')}</p></div></div>}
      </section>
      <div className="ov-column">
        <section className="ov-panel" aria-label={t('Recent scans')}>
          <PanelHeading title={t('Recent scans')}>{link(libraryTarget, 'Libraries')}</PanelHeading>
          <ResourceNote observations={[jobs, libraries]} />
          {recent.map((job) => {
            const library = libraryIndex.get(job.LibraryId)!;
            const target: DashboardTarget = { page: 'metadata', libraryId: library.Id };
            const Icon = library.CollectionType === 'tvshows' ? TvRounded : library.CollectionType === 'music' ? MusicNoteOutlined : library.CollectionType === 'movies' ? MovieOutlined : FolderOutlined;
            const failed = scanFailed(job);
            const complete = job.Status.toLowerCase() === 'completed';
            const result = t('Added {added} · updated {updated}', { added: count(job.Added), updated: count(job.Updated) });
            return <a className="ov-scan-row" key={job.Id} href={dashboardTargetURL(target)} onClick={go(target)}><span className="ov-library-icon"><Icon /></span>
              <span><strong>{library.Name}</strong><small><time dateTime={job.FinishedAt!}>{observationTime(job.FinishedAt, locale)}</time> · {failed ? t(job.Error || 'No error description was recorded.') : result}</small></span>
              <span className="ov-scan-state" data-tone={failed ? 'error' : complete ? 'ok' : 'unknown'}><i className="ov-dot" data-tone={failed ? 'error' : complete ? 'ok' : 'unknown'} aria-hidden="true" />{t(failed ? job.Status.toLowerCase() === 'interrupted' ? 'Interrupted' : 'Failed' : complete ? 'Completed' : 'Cancelled')}</span></a>;
          })}
          {recent.length === 0 && jobs.data && libraries.data && <p className="ov-panel-note">{t('No completed scans were found in recent history.')}</p>}
          {(jobs.data?.Items.length ?? 0) >= recentScanJobLimit && <p className="ov-panel-note">{t('Based on the most recent 1,000 scan requests.')}</p>}
          <p className="ov-panel-note">{t('Added and updated counts reflect files accepted during the scan. A failed scan retains existing records; changes already accepted are kept.')}</p>
        </section>
        <section className="ov-panel" aria-label={t('In progress')}>
          <PanelHeading title={t('In progress')}>{link(tasksTarget, 'Tasks')}</PanelHeading>
          <ResourceNote observations={[tasks, jobs]} />
          {activeTasks.map(({ task, run }) => <TaskProgress key={run.Id} label={t(task.Name)} run={run} />)}
          {standaloneScans.map((job) => <TaskProgress key={job.Id} label={t('Scanning {library}', { library: libraryIndex.get(job.LibraryId)!.Name })}
            run={{ State: ['pending', 'queued'].includes(job.Status.toLowerCase()) ? 'pending' : 'running', Scanned: job.Scanned, TotalChildren: 0, TerminalChildren: 0 }} />)}
          {tasks.data && jobs.data && activeTasks.length === 0 && standaloneScans.length === 0 && <p className="ov-panel-note">{t('No active tasks')}</p>}
          <a className="ov-schedule-row" href={dashboardTargetURL(tasksTarget)} onClick={go(tasksTarget)}><ScheduleOutlined />
            <span>{t('Next scheduled library scan')}</span>{nextScan?.NextRunAt ? <time dateTime={nextScan.NextRunAt} title={nextScan.ScheduleTimezone}>{observationTime(nextScan.NextRunAt, locale, nextScan.ScheduleTimezone)}</time> : <small>{t(tasks.error || !tasks.data ? 'Unknown' : 'Not scheduled')}</small>}</a>
        </section>
      </div>
    </div>
  </div>;
}
