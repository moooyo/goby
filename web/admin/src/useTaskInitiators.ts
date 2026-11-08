import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { adminApi } from './api';
import type { ActivityEntry, TaskRun } from './api';

export const taskInitiatorPageSize = 200;
export const taskInitiatorRecordLimit = 1000;
const activityWindowMilliseconds = 24 * 60 * 60 * 1000;

export type TaskInitiator = ActivityEntry['Actor'];
export interface TaskInitiatorIssue { reason: 'request' | 'timeout' | 'limit' | 'changed' | 'mismatch' | 'conflict'; error?: unknown }
interface InitiatorResult { byRun: Map<string, TaskInitiator>; issues: TaskInitiatorIssue[] }
interface InitiatorSnapshot extends InitiatorResult { owner: string; refresh: number; checked: Set<string>; loading: boolean }

function actorIdentity(actor: TaskInitiator) { return JSON.stringify([actor.Kind, actor.Id, actor.Name]); }

async function loadInitiators(targets: Set<string>, signal: AbortSignal): Promise<InitiatorResult> {
  const byRun = new Map<string, TaskInitiator>();
  const conflicts = new Set<string>();
  const seen = new Map<string, { signature: string; runId: string }>();
  const issues: TaskInitiatorIssue[] = [];
  const minimum = Date.now() - activityWindowMilliseconds;
  const minDate = new Date(minimum).toISOString();
  let total: number | undefined;
  let previousDate = Number.POSITIVE_INFINITY;
  const mark = (reason: TaskInitiatorIssue['reason']) => { if (!issues.some((issue) => issue.reason === reason)) issues.push({ reason }); };
  try {
    for (let offset = 0; offset < taskInitiatorRecordLimit; offset += taskInitiatorPageSize) {
      if (signal.aborted) { mark('timeout'); break; }
      const page = await adminApi.getActivity({ Action: 'task.admitted', MinDate: minDate, StartIndex: offset, Limit: taskInitiatorPageSize }, { signal });
      if (total !== undefined && total !== page.TotalRecordCount) mark('changed');
      total = page.TotalRecordCount;
      for (const entry of page.Items) {
        const date = Date.parse(entry.Date);
        if (date > previousDate) mark('changed');
        previousDate = date;
        const signature = JSON.stringify([entry.Action, entry.Resource.Kind, entry.Resource.Id, actorIdentity(entry.Actor)]);
        const priorEvent = seen.get(entry.Id);
        if (priorEvent !== undefined) {
          mark('changed');
          if (priorEvent.signature === signature) continue;
          for (const id of [priorEvent.runId, entry.Resource.Id]) if (targets.has(id)) { conflicts.add(id); byRun.delete(id); mark('conflict'); }
        }
        seen.set(entry.Id, { signature, runId: entry.Resource.Id });
        if (entry.Action !== 'task.admitted' || entry.Resource.Kind !== 'task_run' || date < minimum) { mark('mismatch'); continue; }
        const actor = entry.Actor;
        const validActor = actor.Kind === 'system' ? actor.Id === null && actor.Name === null
          : Boolean(actor.Id) && (actor.Kind === 'user' || actor.Name === null);
        if (!validActor) { mark('mismatch'); continue; }
        if (!targets.has(entry.Resource.Id) || conflicts.has(entry.Resource.Id)) continue;
        const previous = byRun.get(entry.Resource.Id);
        if (previous && actorIdentity(previous) !== actorIdentity(actor)) {
          conflicts.add(entry.Resource.Id); byRun.delete(entry.Resource.Id); mark('conflict');
        } else byRun.set(entry.Resource.Id, actor);
      }
      if (offset + page.Items.length >= page.TotalRecordCount) return { byRun, issues };
      if (page.Items.length < taskInitiatorPageSize) { mark('changed'); return { byRun, issues }; }
    }
    if (!signal.aborted && total !== undefined && total > taskInitiatorRecordLimit) mark('limit');
  } catch (error) { issues.push({ reason: signal.aborted ? 'timeout' : 'request', error: signal.aborted ? undefined : error }); }
  return { byRun, issues };
}

export function useTaskInitiators(owner: string, runs: TaskRun[], ready: boolean) {
  const [refresh, setRefresh] = useState(0);
  const reload = useCallback(() => setRefresh((value) => value + 1), []);
  const fingerprint = JSON.stringify([...new Set(runs.map((run) => run.Id))].sort());
  const ids = useMemo(() => JSON.parse(fingerprint) as string[], [fingerprint]);
  const initial: InitiatorSnapshot = { owner, refresh: -1, checked: new Set(), byRun: new Map(), issues: [], loading: false };
  const cache = useRef<InitiatorSnapshot>(initial);
  const [snapshot, setSnapshot] = useState(initial);
  const round = useRef<Promise<void> | undefined>(undefined);

  useEffect(() => {
    if (!ready || ids.length === 0) return;
    if (cache.current.owner !== owner) cache.current = { ...initial, owner };
    if (cache.current.refresh === refresh && ids.every((id) => cache.current.checked.has(id))) {
      setSnapshot({ ...cache.current, loading: false });
      return;
    }
    let obsolete = false;
    const controller = new AbortController();
    const previousRound = round.current;
    setSnapshot({ owner, refresh, checked: new Set(), byRun: cache.current.refresh === refresh ? new Map(cache.current.byRun) : new Map(), issues: [], loading: true });
    const operation = (async () => {
      if (previousRound) await previousRound.catch(() => undefined);
      if (obsolete) return;
      const deadline = setTimeout(() => controller.abort(), 20_000);
      try {
        const result = await loadInitiators(new Set(ids), controller.signal);
        if (obsolete) return;
        // A failed refresh never presents an older cached name as a new confirmation.
        const next = { ...result, owner, refresh, checked: new Set(ids), loading: false };
        cache.current = next; setSnapshot(next);
      } finally { clearTimeout(deadline); }
    })();
    round.current = operation;
    const clear = () => { if (round.current === operation) round.current = undefined; };
    void operation.then(clear, clear);
    return () => { obsolete = true; controller.abort(); };
  }, [owner, fingerprint, ids, ready, refresh]);

  const current = snapshot.owner === owner && snapshot.refresh === refresh;
  return {
    byRun: current ? snapshot.byRun : new Map<string, TaskInitiator>(),
    issues: current ? snapshot.issues : [], reload,
    loading: ready && ids.length > 0 && (!current || snapshot.loading || !ids.every((id) => snapshot.checked.has(id))),
  };
}
