import assert from 'node:assert/strict';
import test from 'node:test';
import { sidebarStatus } from './dashboardServerStatus.ts';

const checking = { loading: true };
const ok = { data: {}, loading: false, readAt: 123 };
const newer = { ...ok, readAt: 456 };
const problem = new Error('Offline');

test('the sidebar only indicates readiness after both initial reads', () => {
  assert.deepEqual(sidebarStatus(checking, ok), { state: 'checking', complete: false, retainedAt: undefined });
  assert.deepEqual(sidebarStatus(ok, checking), { state: 'checking', complete: false, retainedAt: undefined });
  assert.deepEqual(sidebarStatus(ok, newer), { state: 'ready', complete: true, retainedAt: undefined });
  assert.deepEqual(sidebarStatus({ ...ok, loading: true }, newer), { state: 'ready', complete: true, retainedAt: undefined });
});

test('a refresh failure dates the retained resource, not another successful read', () => {
  assert.deepEqual(sidebarStatus({ loading: false, error: problem }, newer), { state: 'failed', complete: false, retainedAt: undefined });
  assert.deepEqual(sidebarStatus({ ...ok, error: problem }, newer), { state: 'failed', complete: true, retainedAt: 123 });
  assert.deepEqual(sidebarStatus({ ...newer, error: problem }, { ...ok, error: problem }), { state: 'failed', complete: true, retainedAt: 123 });
});
