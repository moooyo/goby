import assert from 'node:assert/strict';
import test from 'node:test';
import type { TaskRun } from './api';
import { isQueuedOrRunningTask } from './taskRunState.ts';

test('the in-progress filter includes pending and running, but not stopping or terminal runs', () => {
  const states: TaskRun['State'][] = ['pending', 'running', 'stopping', 'completed', 'failed', 'cancelled', 'interrupted'];
  assert.deepEqual(states.filter((State) => isQueuedOrRunningTask({ State })), ['pending', 'running']);
});
