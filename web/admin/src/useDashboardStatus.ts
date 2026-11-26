import { useCallback, useEffect, useMemo, useState } from 'react';
import { adminApi } from './api';
import { backupPolicyApi } from './backupPolicyApi';
import { dashboardAttention } from './dashboardAttention';
import { useTranslation } from './i18n';
import { systemStatusApi } from './systemStatusApi';

export interface Observation<T> {
  data?: T;
  error?: unknown;
  loading: boolean;
}

const readOverview = async (signal: AbortSignal) => {
  const data = await adminApi.getOverview({ signal });
  if (!data?.Server || !data.Counts || !data.Database) throw new Error('Overview response is incomplete.');
  return data;
};
const readSystem = (signal: AbortSignal) => systemStatusApi.getStatus({ signal });
const readDirectories = (signal: AbortSignal) => adminApi.getLibraryStorageDirectories({ signal });
const readBackupPolicy = (signal: AbortSignal) => backupPolicyApi.get({ signal });
const readTasks = (signal: AbortSignal) => adminApi.getTasks({ signal });
const readJobs = async (signal: AbortSignal) => {
  const data = await adminApi.getJobs({ signal });
  if (!Array.isArray(data?.Items)) throw new Error('Scan job response is incomplete.');
  return data;
};
const readLibraries = async (signal: AbortSignal) => {
  const data = await adminApi.getLibraries({ signal });
  if (!Array.isArray(data?.Items)) throw new Error('Library response is incomplete.');
  return data;
};
const readSettings = (signal: AbortSignal) => adminApi.getSettings({ signal });

function useObservation<T>(load: (signal: AbortSignal) => Promise<T>, interval: number, revision: number, enabled = true) {
  const [snapshot, setSnapshot] = useState<Observation<T>>({ loading: true });
  useEffect(() => {
    if (!enabled) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let pending: AbortController | undefined;
    function stop() {
      clearTimeout(timer);
      const request = pending;
      pending = undefined;
      request?.abort();
    }
    async function read() {
      if (document.hidden || pending) return;
      clearTimeout(timer);
      const request = new AbortController();
      pending = request;
      setSnapshot((previous) => previous.loading ? previous : { ...previous, loading: true });
      // Settle even if a transport does not honor its abort signal.
      let abort: (() => void) | undefined;
      const cancelled = new Promise<never>((_resolve, reject) => {
        abort = () => reject(request.signal.reason);
        request.signal.addEventListener('abort', abort, { once: true });
      });
      const deadline = setTimeout(() => request.abort(new Error('Status check timed out.')), 15_000);
      try {
        const data = await Promise.race([load(request.signal), cancelled]);
        if (pending === request) setSnapshot({ data, loading: false });
      } catch (error) {
        if (pending === request) setSnapshot((previous) => ({ ...previous, error, loading: false }));
      } finally {
        clearTimeout(deadline);
        if (abort) request.signal.removeEventListener('abort', abort);
        if (pending === request) {
          pending = undefined;
          if (!document.hidden) timer = setTimeout(() => void read(), interval);
        }
      }
    }
    function visibilityChanged() {
      if (document.hidden) {
        stop();
        setSnapshot((previous) => ({ ...previous, loading: false }));
      } else void read();
    }
    document.addEventListener('visibilitychange', visibilityChanged);
    void read();
    return () => { stop(); document.removeEventListener('visibilitychange', visibilityChanged); };
  }, [load, interval, revision, enabled]);
  return enabled ? snapshot : { ...snapshot, loading: false };
}

// One observation per resource for the workspace. Both the overview and bell
// consume the same issues and the same loading, unknown and stale states.
export function useDashboardStatus(isOverview: boolean, attentionOpen: boolean, needsLibraryName: boolean) {
  const { t, locale } = useTranslation();
  const [revision, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  const liveInterval = isOverview ? 5_000 : 30_000;
  // Uptime and running work are live; avoid re-counting the entire catalog or
  // enumerating every registered directory every five seconds.
  const overview = useObservation(readOverview, 30_000, revision);
  const system = useObservation(readSystem, liveInterval, revision);
  const directories = useObservation(readDirectories, 30_000, revision);
  const backup = useObservation(readBackupPolicy, 30_000, revision);
  // Detailed task and scan reads are only needed with the overview or bell
  // open. Existing status observations still supply the global bell badge.
  const needsWork = isOverview || attentionOpen;
  const tasks = useObservation(readTasks, liveInterval, revision, needsWork);
  const jobs = useObservation(readJobs, liveInterval, revision, needsWork);
  const libraries = useObservation(readLibraries, 30_000, revision, needsWork || needsLibraryName);
  const settings = useObservation(readSettings, 30_000, revision);
  const attention = useMemo(() => dashboardAttention({ overview, system, directories, backup, tasks, jobs, libraries, settings }, t, locale),
    [overview, system, directories, backup, tasks, jobs, libraries, settings, t, locale]);
  return { overview, system, directories, backup, tasks, jobs, libraries, settings, attention, refresh };
}

export type DashboardStatus = ReturnType<typeof useDashboardStatus>;
