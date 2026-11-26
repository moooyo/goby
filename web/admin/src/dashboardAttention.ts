import type { JobsResponse, LibrariesResponse, LibraryStorageDirectoriesResponse, OverviewResponse, ServerSettings, TaskDefinitionsResponse } from './api';
import type { BackupPolicy } from './backupPolicyApi';
import type { DashboardTarget } from './dashboardNavigation';
import { directoryCapacity, directoryReadAccess, formatDirectoryBytes, formatDirectoryCapacity } from './directoryCapacity';
import type { TranslationValues } from './i18n';
import { indexLatestLibraryScans, recentScanJobLimit, scanDirectoryFailure, scanFailed } from './latestLibraryScans';
import type { SystemStatus } from './systemStatusApi';
import type { Observation } from './useDashboardStatus';

type Translate = (key: string, values?: TranslationValues) => string;
type Resources = {
  overview: Observation<OverviewResponse>;
  system: Observation<SystemStatus>;
  directories: Observation<LibraryStorageDirectoriesResponse>;
  backup: Observation<BackupPolicy>;
  tasks: Observation<TaskDefinitionsResponse>;
  jobs: Observation<JobsResponse>;
  libraries: Observation<LibrariesResponse>;
  settings: Observation<ServerSettings>;
};
export interface AttentionEntry {
  id: string;
  check?: string;
  severity: 'critical' | 'warning';
  category: 'Storage' | 'Content' | 'System' | 'Task' | 'Configuration';
  time?: number;
  stale: boolean;
  title: string;
  detail?: string;
  action?: { label: string; target: DashboardTarget };
}
export interface AttentionCheck {
  id: string;
  state: 'loading' | 'unknown' | 'failed';
  title: string;
  detail?: string;
  stale?: boolean;
}

const databaseStates = new Set(['ok', 'ready', 'connected', 'healthy']);
const lastDay = 24 * 60 * 60 * 1000;
export const databaseHealthy = (status: string) => databaseStates.has(status.toLowerCase());
export const libraryScanTask = (task: { Key: string }) => task.Key === 'library.scan' || task.Key === 'scan';

export function observationTime(value: string | null | undefined, locale: string, timeZone?: string): string {
  if (!value || !Number.isFinite(Date.parse(value))) return '';
  const date = new Date(value);
  try { return date.toLocaleString(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false, timeZone }); }
  catch { return date.toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' }); }
}

export function dashboardAttention({ overview, system, directories, backup, tasks, jobs, libraries, settings }: Resources, t: Translate, locale: string, now = Date.now()) {
  const entries: AttentionEntry[] = [];
  const checks: AttentionCheck[] = [];
  const number = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 });
  const storage = [...new Map((directories.data?.Items ?? []).map((directory) => [directory.Path, directory])).values()];
  const libraryList = libraries.data?.Items ?? [];
  const byLibrary = new Map(libraryList.map((library) => [library.Id, library]));
  const byPath = new Map<string, string[]>();
  for (const library of libraryList) {
    for (const path of new Set(library.Paths)) {
      const references = byPath.get(path);
      if (references) references.push(library.Name);
      else byPath.set(path, [library.Name]);
    }
  }
  const unreadablePaths = new Set(storage.filter((directory) => directoryReadAccess(directory) === false).map(({ Path }) => Path));
  const knownPaths = new Set(storage.map(({ Path }) => Path));
  const unknownDirectories = [...byPath.keys()].some((path) => !knownPaths.has(path)) || storage.some((directory) => {
    const readAccess = directoryReadAccess(directory);
    const capacity = directoryCapacity(directory);
    return readAccess == null || readAccess && (!capacity.complete || capacity.percent == null);
  });

  function check(id: string, resource: Observation<unknown>, strings: { loading: string; failed: string; unknown: string }, unknown = false, details?: string) {
    if (resource.error != null) checks.push({ id, state: 'failed', title: t(strings.failed), stale: resource.data !== undefined,
      detail: t(resource.data === undefined ? strings.unknown : 'Showing the last successful check.') });
    else if (resource.data === undefined && resource.loading) checks.push({ id, state: 'loading', title: t(strings.loading) });
    else if (resource.data === undefined || unknown) checks.push({ id, state: 'unknown', title: t(strings.unknown), detail: details });
  }

  check('service-status', system, { loading: 'Checking service status…', failed: 'Could not check service status', unknown: 'Service status is unknown.' });
  check('database-status', overview, { loading: 'Checking database status…', failed: 'Could not check database status', unknown: 'Database status is unknown.' }, !overview.data?.Database.Status);
  check('directory-status', directories, { loading: 'Checking registered media directories…', failed: 'Media directory checks unavailable', unknown: 'Media directory access or capacity is unknown.' }, unknownDirectories,
    t('Some registered directories have no access or capacity observation.'));
  check('backup-status', backup, { loading: 'Checking automatic backup rules…', failed: 'Automatic backup rules check unavailable', unknown: 'Automatic backup trigger rules are unknown.' }, backup.data?.Available === false,
    t('The automatic backup service is unavailable.'));
  check('task-status', tasks, { loading: 'Checking task outcomes…', failed: 'Could not check task outcomes', unknown: 'Task outcomes are unknown.' });
  check('scan-status', jobs, { loading: 'Checking scan outcomes…', failed: 'Could not check scan outcomes', unknown: 'Scan outcomes are unknown.' });
  check('library-status', libraries, { loading: 'Checking registered libraries…', failed: 'Could not check registered libraries', unknown: 'Registered libraries are unknown.' });
  check('network-status', settings, { loading: 'Checking network settings…', failed: 'Could not check network settings', unknown: 'Network restart requirements are unknown.' }, !settings.data?.Runtime?.Network);

  for (const directory of storage) {
    const capacity = directoryCapacity(directory);
    const names = byPath.get(directory.Path)?.join(t(', '));
    const stale = directories.error != null;
    if (capacity.warning) entries.push({ id: `capacity:${directory.Path}`, check: 'capacity', severity: 'critical', category: 'Storage', stale,
      title: t('{path} is {percent}% full', { path: directory.Path, percent: number.format(capacity.percent!) }),
      detail: [formatDirectoryCapacity(capacity, locale), capacity.available != null && t('{space} available', { space: formatDirectoryBytes(capacity.available, locale) }), names].filter(Boolean).join(' · '),
      action: { target: { page: 'libraries' }, label: 'View storage' } });
    if (unreadablePaths.has(directory.Path)) entries.push({ id: `directory:${directory.Path}`, check: 'directory', severity: 'critical', category: 'Content', stale: stale || libraries.error != null,
      title: names ? t('{libraries}: media directory cannot be read', { libraries: names }) : t('Media directory cannot be read'), detail: directory.Path,
      action: { target: { page: 'libraries' }, label: 'Check bindings' } });
  }

  const dbStatus = overview.data?.Database.Status;
  if (dbStatus && !databaseHealthy(dbStatus)) entries.push({ id: 'database', check: 'database', severity: 'critical', category: 'System', stale: overview.error != null,
    title: t('Database needs attention'), detail: dbStatus });

  for (const task of tasks.data?.Items ?? []) {
    const run = task.LastRun;
    if (!run) continue;
    const time = Date.parse(run.FinishedAt ?? '');
    if (!(time >= now - lastDay && time <= now)) continue;
    const failed = run.State === 'failed' || run.State === 'interrupted';
    const partial = run.State === 'completed' && (run.FailedChildren > 0 || run.InterruptedChildren > 0 || run.UnavailableChildren > 0);
    if (!failed && !partial) continue;
    entries.push({ id: `task:${task.Id}`, check: 'task', severity: 'warning', category: 'Task', time, stale: tasks.error != null,
      title: t(run.State === 'interrupted' ? '{task}: run interrupted' : failed ? '{task}: run failed' : '{task}: some work failed', { task: t(task.Name) }),
      detail: [observationTime(run.FinishedAt, locale), run.ErrorMessage].filter(Boolean).join(' · '),
      action: { target: { page: 'tasks', runId: run.Id }, label: 'View tasks' } });
  }

  const latest = indexLatestLibraryScans(libraryList, jobs.data?.Items ?? []);
  if (jobs.error == null && (jobs.data?.Items.length ?? 0) >= recentScanJobLimit && libraryList.some(({ Id }) => !latest[Id])) {
    checks.push({ id: 'scan-coverage', state: 'unknown', title: t('Scan history is limited.'), detail: t('Some libraries have no result among the most recent 1,000 scan requests.') });
  }
  for (const job of Object.values(latest)) {
    if (!scanFailed(job)) continue;
    const library = byLibrary.get(job.LibraryId)!;
    if (scanDirectoryFailure(job.Error) && library.Paths.some((path) => unreadablePaths.has(path))) continue;
    entries.push({ id: `scan:${job.LibraryId}`, check: 'scan', severity: 'warning', category: 'Content', time: Date.parse(job.FinishedAt ?? job.CreatedAt),
      stale: jobs.error != null || libraries.error != null, title: t('{library}: scan failed', { library: library.Name }), detail: job.Error || t('No error description was recorded.'),
      action: { target: { page: 'tasks', tasksTab: 'history' }, label: 'View tasks' } });
  }

  const network = settings.data?.Runtime?.Network;
  if (network?.RestartRequired) {
    const desired = `${network.Desired.BindHost || '*'}:${network.Desired.HttpPort}`;
    const active = network.Active && `${network.Active.BoundHost || '*'}:${network.Active.HttpPort}`;
    entries.push({ id: 'network', check: 'network', severity: 'warning', category: 'Configuration', stale: settings.error != null, title: t('Network changes require a restart'),
      detail: active ? t('Saved listener {desired} is not active. Currently listening on {active}.', { desired, active }) : t('Saved network settings are not active yet.'),
      action: { target: { page: 'settings' }, label: 'Go to restart' } });
  }
  if (backup.data?.Available && !backup.data.Enabled) entries.push({ id: 'backup-rules', check: 'backup-rules', severity: 'warning', category: 'Configuration', stale: backup.error != null,
    title: t('No automatic backup rules'), detail: [backup.data.LastRunAt && t('Last automatic backup run: {time}', { time: observationTime(backup.data.LastRunAt, locale) }),
      t('The backup task has no active trigger rules. Backups must be started manually.')].filter(Boolean).join(' · '),
    action: { target: { page: 'tasks', taskId: backup.data.TaskId || undefined }, label: 'Set trigger rules' } });

  // API snapshots don't timestamp when capacity crossed a threshold, a mount
  // became unreadable, or a rule was removed. Never sort these as newly occurred.
  entries.sort((a, b) => Number(a.severity !== 'critical') - Number(b.severity !== 'critical')
    || (Number.isFinite(b.time) ? b.time! : 0) - (Number.isFinite(a.time) ? a.time! : 0) || a.id.localeCompare(b.id));
  return { entries, checks, loading: [overview, system, directories, backup, tasks, jobs, libraries, settings].some((resource) => resource.loading),
    critical: entries.filter((entry) => entry.severity === 'critical').length, unknownDirectories };
}
