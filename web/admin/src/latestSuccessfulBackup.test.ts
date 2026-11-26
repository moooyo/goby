import assert from 'node:assert/strict';
import test from 'node:test';
import type { BackupView } from './backupsApi';
import { readLatestSuccessfulBackup } from './latestSuccessfulBackup.ts';

const createdAt = '2026-11-25T18:00:00Z';
const archive = (index: number, patch: Partial<BackupView> = {}): BackupView => ({
  Id: String(index), Kind: 'generated', State: 'ready', Verified: true, CreatedAt: createdAt, UpdatedAt: createdAt,
  SizeBytes: '1024', SHA256: 'a'.repeat(64), ErrorCode: '', Source: null, ...patch,
});

function pages(archives: BackupView[]) {
  const calls: number[] = [];
  return { calls, read: async (start: number, limit: number) => {
    calls.push(start);
    return { Items: archives.slice(start, start + limit), StartIndex: start, Limit: limit, TotalRecordCount: archives.length };
  } };
}

test('selects the newest verified ready generated archive by creation, not by later re-verification', async () => {
  const { read, calls } = pages([
    archive(1, { Kind: 'imported' }), archive(2, { State: 'writing' }), archive(3, { State: 'failed' }),
    archive(4, { State: 'deleting' }), archive(5, { Verified: false }), archive(6),
    archive(7, { CreatedAt: '2026-11-24T18:00:00Z', UpdatedAt: '2026-11-26T18:00:00Z' }),
  ]);
  assert.equal(await readLatestSuccessfulBackup(read), createdAt);
  assert.deepEqual(calls, [0]);
});

test('continues past pages of ineligible archives and stops at the first qualifying record', async () => {
  const { read, calls } = pages([...Array.from({ length: 100 }, (_, index) => archive(index, { Kind: 'imported' })), archive(100),
    ...Array.from({ length: 100 }, (_, index) => archive(index + 101, { State: 'failed' }))]);
  assert.equal(await readLatestSuccessfulBackup(read), createdAt);
  assert.deepEqual(calls, [0, 100]);
});

test('returns no result only after reading the complete catalog', async () => {
  assert.equal(await readLatestSuccessfulBackup(pages([]).read), null);
  const { read, calls } = pages(Array.from({ length: 101 }, (_, index) => archive(index, { Verified: false })));
  assert.equal(await readLatestSuccessfulBackup(read), null);
  assert.deepEqual(calls, [0, 100]);
});

test('does not turn a failed, truncated, shifted, or oversized catalog into an empty result', async () => {
  const { read } = pages(Array.from({ length: 101 }, (_, index) => archive(index, { State: 'failed' })));
  const offline = new Error('Offline');
  await assert.rejects(readLatestSuccessfulBackup((start, limit) => start ? Promise.reject(offline) : read(start, limit)), offline);
  for (const patch of [{ Items: [] }, { Items: [archive(0)] }, { TotalRecordCount: 100 }]) {
    await assert.rejects(readLatestSuccessfulBackup(async (start, limit) => {
      const page = await read(start, limit);
      return start ? { ...page, ...patch } : page;
    }), /changed or was incomplete/);
  }
  await assert.rejects(readLatestSuccessfulBackup(async (start, limit) => ({ ...await read(start, limit), TotalRecordCount: 4097 })), /changed or was incomplete/);
});
