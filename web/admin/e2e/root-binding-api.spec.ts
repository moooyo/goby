import { expect, test } from '@playwright/test';
import { adminApi, clearSession } from '../src/api';
import { rootBindingsApi } from '../src/rootBindingsApi';
import type { RegisteredRoot, RootBinding, StorageTopology } from '../src/rootBindingsApi';

const currentFingerprint = 'a'.repeat(64);
const previousFingerprint = 'b'.repeat(64);
const root: RegisteredRoot = { Id: 'registered-root', LibraryId: 'library-one', Path: '/media/movies', AllowedPath: '/media', RelativePath: 'movies', Revision: '1' };

function topology(): StorageTopology {
  const identity = { Profile: 'linux-fsuuid-filehandle-v1', FilesystemUUID: '0123456789abcdef'.repeat(2), Digest: 'c'.repeat(64) };
  return { Anchor: { ...identity }, RegisteredRoot: { ...identity }, Boundaries: [] };
}

function binding(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return { ...root, Status: 'unbound', ObservedFingerprint: currentFingerprint, Observed: topology(), ...overrides };
}

function approved(): Record<string, unknown> {
  return { Approved: topology(), ApprovedFingerprint: previousFingerprint, BoundAt: '2026-09-12T08:00:00Z', BoundBy: 'administrator-one' };
}

async function withResponse(result: unknown, check: () => Promise<void>): Promise<void> {
  const originalFetch = globalThis.fetch;
  clearSession();
  // These decoder cases replace fetch and never contact a running API.
  globalThis.fetch = async () => new Response(JSON.stringify(result), { status: 200, headers: { 'Content-Type': 'application/json' } });
  try { await check(); }
  finally { globalThis.fetch = originalFetch; clearSession(); }
}

for (const revision of ['1', '9007199254740993', '9223372036854775807']) {
  test(`root binding API preserves revision ${revision} in discovery and observation`, async () => {
    await withResponse({ Items: [{ ...root, Revision: revision }], TotalRecordCount: 1 }, async () => {
      expect((await rootBindingsApi.listRoots(root.LibraryId)).Items[0].Revision).toBe(revision);
    });
    await withResponse({ Binding: binding({ Revision: revision }) }, async () => {
      expect((await rootBindingsApi.getBinding(root.LibraryId, root.Id)).Revision).toBe(revision);
    });
  });
}

for (const value of [0, 1, 9007199254740992, '0', '-1', '01', '+1', ' 1', '1 ', '1.0', '1e3', '9223372036854775808']) {
  test(`root binding API rejects inexact revision ${JSON.stringify(value)}`, async () => {
    await withResponse({ Binding: binding({ Revision: value }) }, async () => {
      await expect(rootBindingsApi.getBinding(root.LibraryId, root.Id)).rejects.toMatchObject({ code: 'invalid_response' });
    });
    await withResponse({ Items: [{ ...root, Revision: value }], TotalRecordCount: 1 }, async () => {
      await expect(rootBindingsApi.listRoots(root.LibraryId)).rejects.toMatchObject({ code: 'invalid_response' });
    });
  });
}

test('root discovery uses unique root IDs while permitting registrations at the same path', async () => {
  const items = [root, { ...root, Id: 'second-registration' }];
  await withResponse({ Items: items, TotalRecordCount: 2 }, async () => {
    expect((await rootBindingsApi.listRoots(root.LibraryId)).Items).toEqual(items);
  });
  for (const result of [
    { Items: [root, root], TotalRecordCount: 2 },
    { Items: [root], TotalRecordCount: 2 },
    { Items: [root], TotalRecordCount: '1' },
    { Items: [{ ...root, LibraryId: 'another-library' }], TotalRecordCount: 1 },
    { Items: [{ ...root, StorageDocument: {} }], TotalRecordCount: 1 },
    { Items: [root], TotalRecordCount: 1, NextIndex: 1 },
  ]) {
    await withResponse(result, async () => {
      await expect(rootBindingsApi.listRoots(root.LibraryId)).rejects.toMatchObject({ code: 'invalid_response' });
    });
  }
});

test('root binding API accepts complete boundary arrays at the supported limit', async () => {
  const observed = topology();
  observed.Boundaries = Array.from({ length: 256 }, (_, index) => ({ RelativePath: `nested/${String(index).padStart(3, '0')}`, Identity: { ...observed.RegisteredRoot } }));
  await withResponse({ Binding: binding({ Observed: observed }) }, async () => {
    const result = await rootBindingsApi.getBinding(root.LibraryId, root.Id);
    expect(result.Observed?.Boundaries).toEqual(observed.Boundaries);
    expect(result.Observed?.Boundaries.at(-1)?.RelativePath).toBe('nested/255');
  });
});

test('root binding API accepts the four supported states with complete approval facts', async () => {
  for (const value of [
    binding(),
    binding({ Status: 'mismatch', ...approved() }),
    binding({ Status: 'verified', ...approved(), ApprovedFingerprint: currentFingerprint }),
    binding({ Status: 'unavailable', Observed: undefined, ObservedFingerprint: undefined }),
    binding({ Status: 'unavailable', Observed: undefined, ObservedFingerprint: undefined, ...approved() }),
  ]) {
    await withResponse({ Binding: value }, async () => {
      expect(await rootBindingsApi.getBinding(root.LibraryId, root.Id)).toEqual(JSON.parse(JSON.stringify(value)));
    });
  }
});

const invalidBindings: { name: string; value: () => Record<string, unknown> }[] = [
  { name: 'unknown status', value: () => binding({ Status: 'expired' }) },
  { name: 'another root', value: () => binding({ Id: 'other-root' }) },
  { name: 'another library', value: () => binding({ LibraryId: 'other-library' }) },
  { name: 'outside anchor mapping', value: () => binding({ Path: '/elsewhere/movies' }) },
  { name: 'padded absolute path', value: () => binding({ Path: '/media//movies' }) },
  { name: 'unknown binding field', value: () => binding({ StorageDocument: {} }) },
  { name: 'missing observed fingerprint', value: () => binding({ ObservedFingerprint: undefined }) },
  { name: 'numeric observed fingerprint', value: () => binding({ ObservedFingerprint: 12 }) },
  { name: 'uppercase observed fingerprint', value: () => binding({ ObservedFingerprint: 'A'.repeat(64) }) },
  { name: 'missing observed topology', value: () => binding({ Observed: undefined }) },
  { name: 'null observed topology', value: () => binding({ Observed: null }) },
  { name: 'unavailable with current observation', value: () => binding({ Status: 'unavailable' }) },
  { name: 'unbound with approval', value: () => binding({ ...approved() }) },
  { name: 'verified without approval', value: () => binding({ Status: 'verified' }) },
  { name: 'verified with mismatching fingerprint', value: () => binding({ Status: 'verified', ...approved() }) },
  { name: 'mismatch with matching fingerprint', value: () => binding({ Status: 'mismatch', ...approved(), ApprovedFingerprint: currentFingerprint }) },
  { name: 'approval without actor', value: () => binding({ Status: 'mismatch', ...approved(), BoundBy: undefined }) },
  { name: 'approval with null time', value: () => binding({ Status: 'mismatch', ...approved(), BoundAt: null }) },
  { name: 'approval with invalid time', value: () => binding({ Status: 'mismatch', ...approved(), BoundAt: 'not-a-time' }) },
  { name: 'unknown topology field', value: () => binding({ Observed: { ...topology(), Truncated: true } }) },
  { name: 'missing boundary array', value: () => binding({ Observed: { ...topology(), Boundaries: undefined } }) },
  { name: 'opaque identity handle', value: () => {
    const observed = topology();
    return binding({ Observed: { ...observed, Anchor: { ...observed.Anchor, Handle: 'private-handle' } } });
  } },
  { name: 'unknown identity profile', value: () => {
    const observed = topology();
    observed.Anchor.Profile = 'unrecognized-profile';
    return binding({ Observed: observed });
  } },
  { name: 'zero filesystem ID', value: () => {
    const observed = topology();
    observed.Anchor.FilesystemUUID = '0'.repeat(32);
    return binding({ Observed: observed });
  } },
  { name: 'invalid directory digest', value: () => {
    const observed = topology();
    observed.RegisteredRoot.Digest = 'c'.repeat(63);
    return binding({ Observed: observed });
  } },
];

for (const relative of ['.', '..', '../outside', 'nested/../outside', '/absolute', 'nested//child', 'nested/', 'bad\0path', 'x'.repeat(4097), 'é'.repeat(2049), '\uD800']) {
  invalidBindings.push({ name: `invalid boundary path ${JSON.stringify(relative).slice(0, 40)}`, value: () => {
    const observed = topology();
    observed.Boundaries = [{ RelativePath: relative, Identity: observed.RegisteredRoot }];
    return binding({ Observed: observed });
  } });
}

for (const name of ['duplicate boundary', '257 boundaries', 'malformed last boundary']) {
  invalidBindings.push({ name, value: () => {
    const observed = topology();
    const count = name === '257 boundaries' ? 257 : 256;
    observed.Boundaries = Array.from({ length: count }, (_, index) => ({ RelativePath: `nested/${index}`, Identity: { ...observed.RegisteredRoot } }));
    if (name === 'duplicate boundary') observed.Boundaries[255].RelativePath = observed.Boundaries[0].RelativePath;
    if (name === 'malformed last boundary') observed.Boundaries[255].Identity.Digest = '';
    return binding({ Observed: observed });
  } });
}

for (const invalid of invalidBindings) {
  test(`root binding API rejects ${invalid.name}`, async () => {
    await withResponse({ Binding: invalid.value() }, async () => {
      await expect(rootBindingsApi.getBinding(root.LibraryId, root.Id)).rejects.toMatchObject({ code: 'invalid_response' });
    });
  });
}

test('root binding approval keeps its exact revision, sends only the acknowledgement contract, and never replays a conflict', async () => {
  const originalFetch = globalThis.fetch;
  const requests: { path: string; options?: RequestInit }[] = [];
  let conflict = false;
  clearSession();
  globalThis.fetch = async (input, options) => {
    const path = String(input);
    requests.push({ path, options });
    const payload = path.endsWith('/session') ? { User: { Id: 'administrator-one' }, CSRFToken: 'mock-csrf-token' }
      : conflict ? { Error: { Code: 'root_binding_conflict', Message: 'Refresh the observation.' }, RequestId: 'conflict-request' }
        : { Binding: binding({ Status: 'verified', Revision: '9007199254740994', ...approved(), ApprovedFingerprint: currentFingerprint }) };
    return new Response(JSON.stringify(payload), { status: conflict && !path.endsWith('/session') ? 409 : 200, headers: { 'Content-Type': 'application/json' } });
  };
  try {
    await adminApi.getSession();
    const input = { Revision: '9007199254740993', ObservedFingerprint: currentFingerprint, AcknowledgeMissingRemoval: true as const };
    const result: RootBinding = await rootBindingsApi.updateBinding(root.LibraryId, root.Id, input);
    expect(result.Revision).toBe('9007199254740994');
    const sent = requests.at(-1)!;
    expect(sent.path).toBe('/admin/v1/libraries/library-one/roots/registered-root/binding');
    expect(sent.options?.method).toBe('PUT');
    expect(new Headers(sent.options?.headers).get('X-CSRF-Token')).toBe('mock-csrf-token');
    expect(JSON.parse(String(sent.options?.body))).toEqual(input);
    expect(new TextEncoder().encode(String(sent.options?.body)).length).toBeLessThanOrEqual(4096);
    conflict = true;
    await expect(rootBindingsApi.updateBinding(root.LibraryId, root.Id, input)).rejects.toMatchObject({ status: 409, code: 'root_binding_conflict', requestId: 'conflict-request' });
    expect(requests.filter((request) => request.options?.method === 'PUT')).toHaveLength(2);
  } finally { globalThis.fetch = originalFetch; clearSession(); }
});
