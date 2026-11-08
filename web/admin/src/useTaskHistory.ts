import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { adminApi } from './api';
import type { TaskDefinition, TaskRun } from './api';
import { isActiveTaskRun } from './TaskRunDialog';

export const taskHistoryPageSize = 50;
export const taskHistoryRunLimit = 200;
export const taskHistoryTaskLimit = 64;
const historyWindowMilliseconds = 24 * 60 * 60 * 1000;
const historyLoadMilliseconds = 20_000;

export type TaskHistoryIssueReason = 'request' | 'timeout' | 'run-limit' | 'task-limit' | 'changed' | 'conflict' | 'unconfirmed';
export interface TaskHistoryIssue { taskId?: string; reason: TaskHistoryIssueReason; error?: unknown }
interface TaskVersion { id: string; lastRun: string }
interface TaskHistoryResult { runs: TaskRun[]; issues: TaskHistoryIssue[] }
interface CachedHistory extends TaskHistoryResult { version: string; refresh: number }
interface HistoryState { owner: string; fingerprint: string; refresh: number; entries: Map<string, CachedHistory>; loading: boolean }

async function loadTaskHistory(taskId: string, windowEnd: number, signal: AbortSignal): Promise<TaskHistoryResult> {
  const runs = new Map<string, TaskRun>();
  const issues: TaskHistoryIssue[] = [];
  const seen = new Set<string>();
  const cutoff = windowEnd - historyWindowMilliseconds;
  let total: number | undefined;
  let previousTime = Number.POSITIVE_INFINITY;
  let ordered = true;
  const markChanged = () => { if (!issues.some((issue) => issue.reason === 'changed')) issues.push({ taskId, reason: 'changed' }); };
  try {
    for (let offset = 0; offset < taskHistoryRunLimit; offset += taskHistoryPageSize) {
      if (signal.aborted) return { runs: [...runs.values()], issues: [...issues, { taskId, reason: 'timeout' }] };
      const page = await adminApi.getTaskRuns(taskId, { StartIndex: offset, Limit: taskHistoryPageSize }, { signal });
      if (total !== undefined && total !== page.TotalRecordCount) markChanged();
      total = page.TotalRecordCount;
      let crossedCutoff = false;
      for (const run of page.Items) {
        const created = Date.parse(run.CreatedAt);
        if (created > previousTime) { ordered = false; markChanged(); }
        previousTime = created;
        if (seen.has(run.Id)) markChanged();
        seen.add(run.Id);
        if (created < cutoff) crossedCutoff = true;
        if (created >= cutoff && created <= windowEnd && !runs.has(run.Id)) runs.set(run.Id, run);
      }
      if (crossedCutoff && ordered) return { runs: [...runs.values()], issues };
      if (offset + page.Items.length >= page.TotalRecordCount) return { runs: [...runs.values()], issues };
      if (page.Items.length < taskHistoryPageSize) { markChanged(); return { runs: [...runs.values()], issues }; }
    }
    issues.push({ taskId, reason: 'run-limit' });
  } catch (error) {
    issues.push({ taskId, reason: signal.aborted ? 'timeout' : 'request', error: signal.aborted ? undefined : error });
  }
  return { runs: [...runs.values()], issues };
}

function preservePartialHistory(result: TaskHistoryResult, previous?: CachedHistory): TaskHistoryResult {
  if (!result.issues.length || !previous) return result;
  const runs = new Map(result.runs.map((run) => [run.Id, run]));
  for (const run of previous.runs) if (!runs.has(run.Id) && runs.size < taskHistoryRunLimit) runs.set(run.Id, run);
  return { ...result, runs: [...runs.values()] };
}

export function useTaskHistory(owner: string, tasks: TaskDefinition[] | undefined) {
  const [refresh, setRefresh] = useState(0);
  const reload = useCallback(() => setRefresh((value) => value + 1), []);
  const fingerprint = JSON.stringify(tasks?.map((task) => ({ id: task.Id, lastRun: JSON.stringify([task.LastRun?.Id, task.LastRun?.State, task.LastRun?.FinishedAt]) })).sort((left, right) => left.id.localeCompare(right.id)) ?? null);
  const versions = useMemo(() => JSON.parse(fingerprint) as TaskVersion[] | null, [fingerprint]);
  const cache = useRef<{ owner: string; entries: Map<string, CachedHistory> }>({ owner, entries: new Map() });
  const round = useRef<Promise<void> | undefined>(undefined);
  const [state, setState] = useState<HistoryState>({ owner, fingerprint, refresh, entries: new Map(), loading: Boolean(tasks?.length) });

  useEffect(() => {
    if (!versions) return;
    if (cache.current.owner !== owner) cache.current = { owner, entries: new Map() };
    const selected = versions.slice(0, taskHistoryTaskLimit);
    const selectedIds = new Set(selected.map((task) => task.id));
    for (const id of cache.current.entries.keys()) if (!selectedIds.has(id)) cache.current.entries.delete(id);
    const jobs = selected.filter((task) => {
      const entry = cache.current.entries.get(task.id);
      return !entry || entry.version !== task.lastRun || entry.refresh !== refresh;
    });
    let obsolete = false;
    const controller = new AbortController();
    const previousRound = round.current;
    const publish = (loading: boolean) => {
      if (!obsolete) setState({ owner, fingerprint, refresh, entries: new Map(cache.current.entries), loading });
    };
    publish(jobs.length > 0);

    const operation = (async () => {
      // An aborted request must settle before the next round starts its two workers.
      if (previousRound) await previousRound.catch(() => undefined);
      if (obsolete || jobs.length === 0) return;
      const windowEnd = Date.now();
      const deadline = setTimeout(() => controller.abort(), historyLoadMilliseconds);
      let nextJob = 0;
      async function worker() {
        while (!obsolete && nextJob < jobs.length) {
          const task = jobs[nextJob++];
          const previous = cache.current.entries.get(task.id);
          const result = await loadTaskHistory(task.id, windowEnd, controller.signal);
          if (obsolete) return;
          cache.current.entries.set(task.id, { ...preservePartialHistory(result, previous), version: task.lastRun, refresh });
          publish(true);
        }
      }
      try { await Promise.all([worker(), worker()]); }
      finally { clearTimeout(deadline); publish(false); }
    })();
    round.current = operation;
    void operation.then(() => { if (round.current === operation) round.current = undefined; }, () => { if (round.current === operation) round.current = undefined; });
    return () => { obsolete = true; controller.abort(); };
  }, [owner, fingerprint, versions, refresh]);

  const now = Date.now();
  const cutoff = now - historyWindowMilliseconds;
  const availableTasks = new Map((tasks ?? []).map((task) => [task.Id, task]));
  const runs = new Map<string, TaskRun>();
  const issues: TaskHistoryIssue[] = [];
  function merge(run: TaskRun, live = false) {
    const task = availableTasks.get(run.TaskId);
    if (!task) return;
    const created = Date.parse(run.CreatedAt);
    if (!(live && isActiveTaskRun(run)) && (created < cutoff || created > now)) return;
    if (!live && isActiveTaskRun(run)) {
      // Only a current task snapshot can confirm that a cached run is still active.
      if (task.CurrentRun?.Id !== run.Id && task.LastRun?.Id !== run.Id && !issues.some((issue) => issue.taskId === run.TaskId && issue.reason === 'unconfirmed')) issues.push({ taskId: run.TaskId, reason: 'unconfirmed' });
      return;
    }
    const previous = runs.get(run.Id);
    if (previous && previous.TaskId !== run.TaskId && !issues.some((issue) => issue.reason === 'conflict')) issues.push({ reason: 'conflict' });
    if (!previous || live) runs.set(run.Id, run);
  }
  if (state.owner === owner) {
    for (const [taskId, entry] of state.entries) {
      if (!availableTasks.has(taskId)) continue;
      issues.push(...entry.issues);
      for (const run of entry.runs) merge(run);
    }
  }
  if ((tasks?.length ?? 0) > taskHistoryTaskLimit) issues.push({ reason: 'task-limit' });
  for (const task of tasks ?? []) {
    if (task.LastRun) merge(task.LastRun, true);
    if (task.CurrentRun) merge(task.CurrentRun, true);
  }
  const items = [...runs.values()].sort((left, right) => Date.parse(right.CreatedAt) - Date.parse(left.CreatedAt) || right.Id.localeCompare(left.Id)).map((run) => ({ task: availableTasks.get(run.TaskId)!, run }));
  return {
    items, issues, reload,
    hasOlderActiveRuns: items.some(({ run }) => isActiveTaskRun(run) && Date.parse(run.CreatedAt) < cutoff),
    loading: Boolean(tasks?.length) && (state.owner !== owner || state.fingerprint !== fingerprint || state.refresh !== refresh || state.loading),
  };
}
