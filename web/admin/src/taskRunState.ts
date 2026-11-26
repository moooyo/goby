import type { TaskRun } from './api';

export function isQueuedOrRunningTask(run: Pick<TaskRun, 'State'>): boolean {
  return run.State === 'pending' || run.State === 'running';
}
