import type { Observation } from './useDashboardStatus';

export function latestSuccessfulRead(...observations: readonly Observation<unknown>[]): number | undefined {
  let latest: number | undefined;
  for (const { readAt } of observations) if (readAt !== undefined && (latest === undefined || readAt > latest)) latest = readAt;
  return latest;
}

export function sidebarStatus(overview: Observation<unknown>, system: Observation<unknown>) {
  const complete = overview.data !== undefined && system.data !== undefined;
  const failed = [overview, system].filter((observation) => observation.error != null);
  if (failed.length) {
    const retainedTimes = failed.flatMap(({ readAt }) => readAt === undefined ? [] : [readAt]);
    // Use the oldest retained result when more than one check failed.
    return { state: 'failed' as const, complete, retainedAt: retainedTimes.length ? Math.min(...retainedTimes) : undefined };
  }
  return { state: complete ? 'ready' as const : 'checking' as const, complete, retainedAt: undefined };
}
