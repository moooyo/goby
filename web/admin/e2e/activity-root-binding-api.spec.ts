import { expect, test } from '@playwright/test';
import { adminApi, clearSession } from '../src/api';
import type { ActivityEntry } from '../src/api';

const fingerprint = '0123456789abcdef'.repeat(4);

function legacyActivity(): ActivityEntry {
  return {
    Id: '7', Date: '2026-09-12T08:00:00Z', Action: 'settings.updated', Severity: 'Info', Source: 'native',
    Actor: { Kind: 'user', Id: 'administrator', Name: 'Administrator' },
    Resource: { Kind: 'settings', Id: '1' }, Revision: '2', Count: '1', State: null,
    ChangedFields: ['ServerName'], Name: 'Server settings updated', Overview: 'Supported server settings were updated.',
  };
}

function rootBindingActivity(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    ...legacyActivity(), Action: 'library.root_binding.updated', Resource: { Kind: 'library_root', Id: 'registered-root' },
    PreviousRevision: '1', Revision: '2', ObservationFingerprint: fingerprint, Count: '0', ChangedFields: [],
    Name: 'Library root binding updated', Overview: 'The registered root binding was updated.', ...overrides,
  };
}

async function withActivityResponse(item: unknown, check: () => Promise<void>): Promise<void> {
  const originalFetch = globalThis.fetch;
  clearSession();
  // Exercise the public API decoder without sending requests to a live server.
  globalThis.fetch = async () => new Response(JSON.stringify({
    Items: [item], TotalRecordCount: 1, StartIndex: 0, Limit: 50, RetentionDays: 30,
  }), { status: 200, headers: { 'Content-Type': 'application/json' } });
  try { await check(); }
  finally {
    globalThis.fetch = originalFetch;
    clearSession();
  }
}

test('activity API accepts the unchanged legacy response shape', async () => {
  const legacy = legacyActivity();
  await withActivityResponse(legacy, async () => {
    const result = await adminApi.getActivity();
    expect(result.Items).toEqual([legacy]);
    expect(result.Items[0]).not.toHaveProperty('PreviousRevision');
    expect(result.Items[0]).not.toHaveProperty('ObservationFingerprint');
  });
});

for (const [previous, revision] of [
  ['1', '2'],
  ['9007199254740993', '9007199254740994'],
  ['9223372036854775806', '9223372036854775807'],
]) {
  test(`activity API preserves exact root revisions ${previous} to ${revision}`, async () => {
    const item = rootBindingActivity({ PreviousRevision: previous, Revision: revision });
    await withActivityResponse(item, async () => {
      const result = await adminApi.getActivity();
      expect(result.Items[0].PreviousRevision).toBe(previous);
      expect(result.Items[0].Revision).toBe(revision);
      expect(result.Items[0].ObservationFingerprint).toBe(fingerprint);
      expect(result.Items[0].Resource).toEqual({ Kind: 'library_root', Id: 'registered-root' });
    });
  });
}

const invalidRootBindings: { name: string; fields: Record<string, unknown> }[] = [
  { name: 'missing previous revision', fields: { PreviousRevision: undefined } },
  { name: 'numeric previous revision', fields: { PreviousRevision: 1 } },
  { name: 'null previous revision', fields: { PreviousRevision: null } },
  { name: 'zero previous revision', fields: { PreviousRevision: '0' } },
  { name: 'padded previous revision', fields: { PreviousRevision: '01' } },
  { name: 'negative previous revision', fields: { PreviousRevision: '-1' } },
  { name: 'fractional previous revision', fields: { PreviousRevision: '1.5' } },
  { name: 'overflowing previous revision', fields: { PreviousRevision: '9223372036854775807', Revision: '9223372036854775808' } },
  { name: 'missing current revision', fields: { Revision: null } },
  { name: 'unchanged revision', fields: { Revision: '1' } },
  { name: 'skipped revision', fields: { Revision: '3' } },
  { name: 'missing fingerprint', fields: { ObservationFingerprint: undefined } },
  { name: 'empty fingerprint', fields: { ObservationFingerprint: '' } },
  { name: 'null fingerprint', fields: { ObservationFingerprint: null } },
  { name: 'short fingerprint', fields: { ObservationFingerprint: 'a'.repeat(63) } },
  { name: 'long fingerprint', fields: { ObservationFingerprint: 'a'.repeat(65) } },
  { name: 'uppercase fingerprint', fields: { ObservationFingerprint: 'A'.repeat(64) } },
  { name: 'non-hexadecimal fingerprint', fields: { ObservationFingerprint: 'g'.repeat(64) } },
  { name: 'non-native source', fields: { Source: 'emby' } },
  { name: 'system actor', fields: { Actor: { Kind: 'system', Id: null, Name: null } } },
  { name: 'application actor', fields: { Actor: { Kind: 'application_key', Id: 'application', Name: null } } },
  { name: 'wrong resource kind', fields: { Resource: { Kind: 'library', Id: 'registered-root' } } },
  { name: 'affected resources', fields: { Count: '1' } },
  { name: 'terminal outcome', fields: { State: 'completed' } },
  { name: 'changed fields', fields: { ChangedFields: ['Name'] } },
];

for (const invalid of invalidRootBindings) {
  test(`activity API rejects root binding facts with ${invalid.name}`, async () => {
    await withActivityResponse(rootBindingActivity(invalid.fields), async () => {
      await expect(adminApi.getActivity()).rejects.toMatchObject({ code: 'invalid_response' });
    });
  });
}

for (const fields of [{ PreviousRevision: '1' }, { ObservationFingerprint: fingerprint }]) {
  test(`activity API rejects ${Object.keys(fields)[0]} on a legacy action`, async () => {
    await withActivityResponse({ ...legacyActivity(), ...fields }, async () => {
      await expect(adminApi.getActivity()).rejects.toMatchObject({ code: 'invalid_response' });
    });
  });
}
