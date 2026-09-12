import { test as base, expect, request as apiRequest } from '@playwright/test';
import type { BrowserContext, Page, Request, Response } from '@playwright/test';
import { createHash } from 'node:crypto';
import { constants, lstatSync, promises as fs, type Stats } from 'node:fs';
import path from 'node:path';
import { TextDecoder } from 'node:util';
import type { Library } from '../src/api';
import type { RegisteredRoot, RootBinding, StorageTopology } from '../src/rootBindingsApi';

const ORIGIN = 'http://127.0.0.1:18288';
const WORK = '/opt/goby-test/exec-work-m3e';
const PRODUCT_SHA = '5f88c432d96825d5c3f8c2faccc64be673dab85849670707571c20fddd71594e';
const JSON_LIMIT = 128 * 1024;
const RESULT_LIMIT = 512 * 1024;
const ID = /^[0-9a-f]{32}$/;
const SHA = /^[0-9a-f]{64}$/;
const ACTIONS = ['browser_ready', 'baseline_created', 'unbound_exposed', 'a_source2', 'controller_approved',
  'a_source3', 'a_unavailable', 'a_restored', 'final_bindings', 'browser_logged_out'] as const;
type Action = typeof ACTIONS[number];
type Slot = 'A' | 'B' | 'U';
type Slots<T> = Record<Slot, T>;
interface Backend {
  pid: number; start_ticks: string; boot_id: string; uid: number; exe: string; sha256: string; cgroup: string; namespace: string;
}
interface Fixture {
  marker: string; version: number; run_id: string; nonce: string; origin: string; output: string; runtime: string;
  paths: Slots<string>; library: { name: string; type: 'movies' };
  administrator: { name: string; password: string; setup_token: string }; backend: Backend;
  ipc: { directory: string; ack_timeout_ms: 15000 }; result_path: string; session_path: string; web_files: Record<string, string>;
}
interface IPCRequest { marker: string; run_id: string; nonce: string; sequence: number; action: Action; payload: Record<string, unknown>; }
interface IPCProof {
  backend: Backend; library_id: string | null; root_ids: Slots<string> | null; revisions: Slots<string> | null;
  binding_updates: number; scan_jobs: number; binding?: RootBinding; bindings?: Slots<RootBinding>;
}
interface IPCAck extends IPCRequest { request_sha256: string; status: 'ok'; proof: IPCProof; }
interface Exchange {
  sequence: number; phase: string; method: string; path: string; status: number | null; request_bytes: number;
  body_keys: string[]; binding_input?: BindingInput; response_bytes?: number; response_sha256?: string; complete: boolean;
}
interface BindingInput { Revision: string; ObservedFingerprint: string; AcknowledgeMissingRemoval: true; }
interface SessionDocument {
  marker: 'goby-storage-binding-live-ui-session-v1'; run_id: string; nonce: string;
  token: string; csrf_token: string; user_id: string | null; validated: boolean;
}

function requireValue(value: unknown, code: string): asserts value { if (!value) throw new Error(code); }
function record(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function exact(value: unknown, keys: string[]): value is Record<string, unknown> {
  return record(value) && JSON.stringify(Object.keys(value).sort()) === JSON.stringify([...keys].sort());
}
function ordered(value: unknown): unknown {
  return Array.isArray(value) ? value.map(ordered) : record(value)
    ? Object.fromEntries(Object.keys(value).sort().map((key) => [key, ordered(value[key])])) : value;
}
const same = (left: unknown, right: unknown): boolean => JSON.stringify(ordered(left)) === JSON.stringify(ordered(right));
const hash = (value: Buffer | string): string => createHash('sha256').update(value).digest('hex');
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T;
const text = (value: unknown, maximum = 256): value is string => typeof value === 'string' && value.length > 0 &&
  Buffer.byteLength(value) <= maximum && !/[\x00-\x1f\x7f]/.test(value);
const revision = (value: unknown): value is string => typeof value === 'string' && /^[1-9]\d{0,18}$/.test(value) && BigInt(value) <= 9223372036854775807n;
const delay = (milliseconds: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, milliseconds));
async function bounded<T>(operation: Promise<T>, milliseconds: number, code: string): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([operation, new Promise<never>((_, reject) => {
      timer = setTimeout(() => reject(new Error(code)), milliseconds);
    })]);
  } finally { if (timer) clearTimeout(timer); }
}

/** Duplicate-free bounded JSON is required for native DTOs and private IPC alike. */
function strictJSON(bytes: Buffer, maximum = JSON_LIMIT): unknown {
  requireValue(bytes.length > 0 && bytes.length <= maximum, 'json_byte_limit');
  const source = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  let index = 0, nodes = 0;
  const whitespace = () => { while (index < source.length && /[\t\r\n ]/.test(source[index])) index++; };
  const quoted = (): string => {
    const start = index++; requireValue(source[start] === '"', 'json_string_required');
    while (index < source.length) {
      const character = source[index++];
      if (character === '"') return JSON.parse(source.slice(start, index)) as string;
      if (character === '\\') index++;
    }
    throw new Error('json_string_incomplete');
  };
  const scan = (depth: number): void => {
    requireValue(depth <= 24 && ++nodes <= 16384, 'json_structure_limit'); whitespace();
    if (source[index] === '{') {
      index++; whitespace(); const keys = new Set<string>();
      if (source[index] === '}') { index++; return; }
      for (;;) {
        whitespace(); const key = quoted(); requireValue(!keys.has(key), 'json_duplicate_key'); keys.add(key);
        whitespace(); requireValue(source[index++] === ':', 'json_separator_required'); scan(depth + 1); whitespace();
        const next = source[index++]; if (next === '}') return; requireValue(next === ',', 'json_separator_required');
      }
    }
    if (source[index] === '[') {
      index++; whitespace(); if (source[index] === ']') { index++; return; }
      for (;;) { scan(depth + 1); whitespace(); const next = source[index++]; if (next === ']') return; requireValue(next === ',', 'json_separator_required'); }
    }
    if (source[index] === '"') { quoted(); return; }
    const scalar = /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(source.slice(index));
    requireValue(scalar, 'json_value_required'); index += scalar[0].length;
  };
  scan(0); whitespace(); requireValue(index === source.length, 'json_trailing_data'); return JSON.parse(source) as unknown;
}

async function privateDirectory(directory: string): Promise<void> {
  const status = await fs.lstat(directory);
  requireValue(status.isDirectory() && !status.isSymbolicLink() && status.uid === 0 && status.gid === 0 &&
    (status.mode & 0o777) === 0o700, 'private_directory_ownership');
}
function fileIdentity(status: Stats): object {
  return { dev: status.dev, ino: status.ino, size: status.size, mtime: status.mtimeMs, ctime: status.ctimeMs,
    mode: status.mode, uid: status.uid, gid: status.gid, nlink: status.nlink };
}
async function privateRead(filename: string, maximum: number): Promise<Buffer> {
  requireValue(filename === path.posix.normalize(filename) && filename.startsWith(WORK + '/storage-binding-live-ui-'), 'private_path_scope');
  let directory = path.posix.dirname(filename);
  for (;;) { await privateDirectory(directory); if (directory === WORK) break; directory = path.posix.dirname(directory); }
  const before = await fs.lstat(filename);
  requireValue(before.isFile() && !before.isSymbolicLink() && before.nlink === 1 && before.uid === 0 && before.gid === 0 &&
    (before.mode & 0o777) === 0o600 && before.size > 0 && before.size <= maximum, 'private_file_ownership');
  const held = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    requireValue(same(fileIdentity(await held.stat()), fileIdentity(before)), 'private_file_changed');
    const bytes = await held.readFile();
    requireValue(bytes.length === before.size && same(fileIdentity(await held.stat()), fileIdentity(before)) &&
      same(fileIdentity(await fs.lstat(filename)), fileIdentity(before)), 'private_file_changed');
    return bytes;
  } finally { await held.close(); }
}
async function syncDirectory(directory: string): Promise<void> {
  const held = await fs.open(directory, constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW);
  try { await held.sync(); } finally { await held.close(); }
}
async function publish(filename: string, value: unknown, maximum = JSON_LIMIT): Promise<string> {
  await privateDirectory(path.posix.dirname(filename));
  const bytes = Buffer.from(JSON.stringify(value, null, 2) + '\n');
  try {
    requireValue(bytes.length > 0 && bytes.length <= maximum, 'private_publication_limit');
    const temporary = filename + '.pending';
    const held = await fs.open(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
    let identity;
    try { await held.writeFile(bytes); await held.sync(); identity = await held.stat(); } finally { await held.close(); }
    await fs.link(temporary, filename); await syncDirectory(path.posix.dirname(filename));
    for (const name of [temporary, filename]) {
      const observed = await fs.lstat(name);
      requireValue(observed.dev === identity.dev && observed.ino === identity.ino && observed.nlink === 2 && observed.isFile() && !observed.isSymbolicLink(), 'private_publication_identity');
    }
    await fs.unlink(temporary); await syncDirectory(path.posix.dirname(filename));
    const final = await fs.lstat(filename);
    requireValue(final.ino === identity.ino && final.dev === identity.dev && final.nlink === 1, 'private_publication_identity');
    return hash(bytes);
  } finally { bytes.fill(0); }
}
async function publishFailureLocation(fixture: Fixture, error: unknown, phase: string): Promise<void> {
  const action = ACTIONS.find((value) => value === phase);
  requireValue(action, 'failure_location_phase_invalid');
  const names = new Set(['Error', 'TypeError', 'RangeError', 'ReferenceError', 'SyntaxError', 'URIError', 'EvalError',
    'AggregateError', 'AssertionError', 'TimeoutError']);
  const name = error instanceof Error ? error.name : '';
  const errorType = /^[A-Za-z][A-Za-z0-9_]{0,31}$/.test(name) && names.has(name) ? name : 'UnknownError';
  const stack = error instanceof Error ? error.stack : undefined;
  const trace = typeof stack === 'string' ? stack.slice(-16384) : '';
  const locations: { file: 'root-binding-live.spec.ts'; line: number; column: number }[] = [];
  // Retain only coordinates from this fixed spec; no message, stack text or path is serialized.
  const frames = /(?:^|\n)[ \t]*at[^\r\n]*[\\/]root-binding-live\.spec\.ts:([1-9][0-9]{0,5}):([1-9][0-9]{0,5})\)?(?=\r?\n|$)/g;
  for (const match of trace.matchAll(frames)) {
    const line = Number(match[1]), column = Number(match[2]);
    if (!locations.some((location) => location.line === line && location.column === column)) {
      locations.push({ file: 'root-binding-live.spec.ts', line, column });
    }
    if (locations.length === 3) break;
  }
  await publish(fixture.output + '/private/browser-failure-location.json', { errorType, phase: action, locations }, 1024);
}
async function optionalStat(filename: string): Promise<Stats | null> {
  try { return await fs.lstat(filename); }
  catch (error) { if ((error as NodeJS.ErrnoException).code === 'ENOENT') return null; throw error; }
}
async function readyPublication(filename: string): Promise<boolean> {
  const final = await optionalStat(filename), pending = await optionalStat(filename + '.pending');
  if (!final) {
    if (pending) requireValue(pending.isFile() && !pending.isSymbolicLink() && pending.uid === 0 && pending.gid === 0 &&
      (pending.mode & 0o777) === 0o600 && pending.nlink === 1 && pending.size <= JSON_LIMIT, 'ipc_pending_ownership');
    return false;
  }
  if (final.nlink === 2 && pending) {
    requireValue(final.isFile() && !final.isSymbolicLink() && final.uid === 0 && final.gid === 0 && (final.mode & 0o777) === 0o600 &&
      pending.isFile() && !pending.isSymbolicLink() && pending.uid === 0 && pending.gid === 0 && (pending.mode & 0o777) === 0o600 &&
      pending.nlink === 2 && pending.dev === final.dev && pending.ino === final.ino && pending.size === final.size &&
      final.size > 0 && final.size <= JSON_LIMIT, 'ipc_pending_identity');
    return false;
  }
  requireValue(final.nlink === 1 && pending === null, 'ipc_publication_links'); return true;
}
function startTicks(raw: string): string {
  requireValue(raw.length <= 8192 && raw.lastIndexOf(') ') > 0, 'process_stat_invalid');
  const values = raw.slice(raw.lastIndexOf(') ') + 2).trim().split(/\s+/);
  requireValue(values.length >= 20 && /^[1-9]\d*$/.test(values[19]), 'process_start_invalid'); return values[19];
}

async function loadFixture(): Promise<{ fixture: Fixture; fixtureHash: string; fixturePath: string }> {
  requireValue(process.platform === 'linux' && process.getuid?.() === 0 && process.getgid?.() === 0 &&
    process.env.GOBY_SMOKE_BASE_URL === ORIGIN && !process.env.DEBUG && !process.env.PWDEBUG, 'runtime_environment_invalid');
  const filename = process.env.GOBY_BINDING_LIVE_FIXTURE, expected = process.env.GOBY_BINDING_LIVE_FIXTURE_SHA256;
  requireValue(typeof filename === 'string' && typeof expected === 'string' && SHA.test(expected), 'fixture_arguments_required');
  const matched = new RegExp('^' + WORK + '/storage-binding-live-ui-([0-9]{8}_[0-9]{6}_[0-9a-f]{12})/private/browser-fixture\\.json$').exec(filename);
  requireValue(matched, 'fixture_path_invalid');
  const bytes = await privateRead(filename, JSON_LIMIT);
  let value: unknown;
  try { requireValue(hash(bytes) === expected, 'fixture_digest_changed'); value = strictJSON(bytes); } finally { bytes.fill(0); }
  requireValue(exact(value, ['marker', 'version', 'run_id', 'nonce', 'origin', 'output', 'runtime', 'paths', 'library',
    'administrator', 'backend', 'ipc', 'result_path', 'session_path', 'web_files']), 'fixture_shape_invalid');
  const fixture = value as unknown as Fixture;
  requireValue(fixture.marker === 'goby-storage-binding-live-ui-v1' && fixture.version === 1 && fixture.run_id === matched[1] &&
    SHA.test(fixture.nonce) && fixture.origin === ORIGIN && fixture.output === WORK + '/storage-binding-live-ui-' + fixture.run_id &&
    fixture.runtime === '/opt/goby-binding-ui-runtime-' + fixture.run_id &&
    fixture.result_path === fixture.output + '/private/browser-result.json' && fixture.session_path === fixture.output + '/private/browser-session.json' &&
    exact(fixture.ipc, ['directory', 'ack_timeout_ms']) && fixture.ipc.directory === fixture.output + '/private/ipc' && fixture.ipc.ack_timeout_ms === 15000,
  'fixture_scope_invalid');
  requireValue(exact(fixture.paths, ['A', 'B', 'U']) && (['A', 'B', 'U'] as Slot[]).every((slot) => fixture.paths[slot] === fixture.runtime + '/app/media/' + slot) &&
    exact(fixture.library, ['name', 'type']) && text(fixture.library.name, 128) && fixture.library.type === 'movies' &&
    exact(fixture.administrator, ['name', 'password', 'setup_token']) && text(fixture.administrator.name, 128) &&
    text(fixture.administrator.password, 256) && fixture.administrator.password.length >= 24 &&
    text(fixture.administrator.setup_token, 256) && fixture.administrator.setup_token.length >= 24, 'fixture_values_invalid');
  const backend = fixture.backend;
  requireValue(exact(backend, ['pid', 'start_ticks', 'boot_id', 'uid', 'exe', 'sha256', 'cgroup', 'namespace']) &&
    Number.isSafeInteger(backend.pid) && backend.pid > 1 && backend.uid === 995 && typeof backend.start_ticks === 'string' && /^[1-9]\d*$/.test(backend.start_ticks) &&
    /^[0-9a-f-]{36}$/.test(backend.boot_id) && backend.sha256 === PRODUCT_SHA && backend.exe.startsWith(fixture.runtime + '/') &&
    backend.exe === path.posix.normalize(backend.exe) && backend.cgroup === '/system.slice/goby-storage-binding-live-ui-worker-' + fixture.run_id.replaceAll('_', '-') + '.service' &&
    /^mnt:\[[1-9]\d*\]$/.test(backend.namespace), 'backend_fixture_invalid');
  requireValue(record(fixture.web_files) && Object.keys(fixture.web_files).length >= 2 && Object.keys(fixture.web_files).length <= 128 &&
    fixture.web_files['index.html'] === 'dd980aa1c9f4c0504aff4006b1adcd074659e2998535e071274b5471ed28a514' &&
    Object.entries(fixture.web_files).every(([name, digest]) => name === path.posix.normalize(name) && !name.startsWith('/') &&
      !name.split('/').some((part) => part === '..' || part === '.' || part === '') && !name.includes('\\') && SHA.test(digest)), 'static_asset_manifest_invalid');
  await privateDirectory(fixture.ipc.directory);
  return { fixture, fixtureHash: expected, fixturePath: filename };
}

async function pinBackend(fixture: Fixture): Promise<void> {
  const backend = fixture.backend;
  requireValue((await fs.readFile('/proc/sys/kernel/random/boot_id', 'utf8')).trim() === backend.boot_id &&
    startTicks(await fs.readFile(`/proc/${backend.pid}/stat`, 'utf8')) === backend.start_ticks &&
    await fs.readlink(`/proc/${backend.pid}/ns/mnt`) === backend.namespace && await fs.readlink('/proc/self/ns/mnt') === backend.namespace &&
    (await fs.readFile(`/proc/${backend.pid}/cgroup`, 'utf8')).trim().split('\n').some((line) => line.split(':')[2] === backend.cgroup) &&
    (await fs.readFile('/proc/self/cgroup', 'utf8')).trim().split('\n').some((line) => line.split(':')[2] === backend.cgroup), 'backend_process_changed');
  const status = await fs.readFile(`/proc/${backend.pid}/status`, 'utf8');
  requireValue(status.split('\n').some((line) => /^Uid:\s+995\s+995\s+995\s+995$/.test(line)) &&
    /^Groups:\s*$/m.test(status) && /^NoNewPrivs:\s+1$/m.test(status) &&
    ['CapEff', 'CapPrm', 'CapInh', 'CapBnd'].every((name) => new RegExp('^' + name + ':\\s+0+$', 'm').test(status)), 'backend_privileges_changed');
  requireValue(await fs.readlink(`/proc/${backend.pid}/exe`) === backend.exe, 'backend_executable_changed');
  const held = await fs.open(`/proc/${backend.pid}/exe`, constants.O_RDONLY);
  try {
    const before = await held.stat();
    requireValue(before.isFile() && before.uid === 0 && (before.mode & 0o777) === 0o550 && before.size > 0 && before.size <= 64 * 1024 * 1024, 'backend_executable_ownership');
    const bytes = await held.readFile();
    try { requireValue(hash(bytes) === PRODUCT_SHA && same(fileIdentity(await held.stat()), fileIdentity(before)), 'backend_executable_changed'); }
    finally { bytes.fill(0); }
  } finally { await held.close(); }
}

class Protocol {
  sequence = 0;
  stages: { sequence: number; action: Action; request_sha256: string; ack_sha256: string }[] = [];
  constructor(readonly fixture: Fixture, readonly fixtureHash: string, readonly fixturePath: string, readonly deadline: number) {}
  async check(): Promise<void> {
    requireValue(Date.now() < this.deadline, 'workflow_deadline');
    const bytes = await privateRead(this.fixturePath, JSON_LIMIT);
    try { requireValue(hash(bytes) === this.fixtureHash, 'fixture_digest_changed'); } finally { bytes.fill(0); }
    await bounded(pinBackend(this.fixture), 5000, 'backend_pin_timeout');
  }
  async step(action: Action, payload: Record<string, unknown>): Promise<IPCProof> {
    requireValue(ACTIONS[this.sequence] === action, 'ipc_action_order'); await this.check();
    const sequence = ++this.sequence;
    const request: IPCRequest = { marker: 'goby-storage-binding-live-ui-ipc-v1', run_id: this.fixture.run_id,
      nonce: this.fixture.nonce, sequence, action, payload };
    const base = `${this.fixture.ipc.directory}/${String(sequence).padStart(2, '0')}-${action}`;
    const requestHash = await bounded(publish(base + '.request.json', request), 5000, 'ipc_publication_timeout');
    const until = Math.min(this.deadline, Date.now() + this.fixture.ipc.ack_timeout_ms);
    while (Date.now() < until) {
      if (await readyPublication(base + '.ack.json')) {
        const bytes = await privateRead(base + '.ack.json', JSON_LIMIT);
        let value: unknown, ackHash: string;
        try { value = strictJSON(bytes); ackHash = hash(bytes); } finally { bytes.fill(0); }
        requireValue(exact(value, ['marker', 'run_id', 'nonce', 'sequence', 'action', 'payload', 'request_sha256', 'status', 'proof']), 'ipc_ack_shape');
        const ack = value as unknown as IPCAck;
        requireValue(ack.marker === request.marker && ack.run_id === request.run_id && ack.nonce === request.nonce && ack.sequence === sequence &&
          ack.action === action && same(ack.payload, payload) && ack.request_sha256 === requestHash && ack.status === 'ok', 'ipc_ack_binding');
        const extra = action === 'controller_approved' ? ['binding'] : action === 'final_bindings' ? ['bindings'] : [];
        requireValue(exact(ack.proof, ['backend', 'library_id', 'root_ids', 'revisions', 'binding_updates', 'scan_jobs', ...extra]) &&
          same(ack.proof.backend, this.fixture.backend) && ack.proof.scan_jobs === 0 && Number.isSafeInteger(ack.proof.binding_updates) &&
          ack.proof.binding_updates >= 0 && ack.proof.binding_updates <= 3, 'ipc_proof_invalid');
        await this.check(); this.stages.push({ sequence, action, request_sha256: requestHash, ack_sha256: ackHash }); return ack.proof;
      }
      await delay(100);
    }
    throw new Error('ipc_ack_timeout');
  }
}

function topology(value: unknown): value is StorageTopology {
  const identity = (entry: unknown): boolean => exact(entry, ['Profile', 'FilesystemUUID', 'Digest']) &&
    entry.Profile === 'linux-fsuuid-filehandle-v1' && typeof entry.FilesystemUUID === 'string' && /^[0-9a-f]{32}$/.test(entry.FilesystemUUID) &&
    entry.FilesystemUUID !== '0'.repeat(32) && typeof entry.Digest === 'string' && SHA.test(entry.Digest);
  return exact(value, ['Anchor', 'RegisteredRoot', 'Boundaries']) && identity(value.Anchor) && identity(value.RegisteredRoot) &&
    Array.isArray(value.Boundaries) && value.Boundaries.length <= 8 && value.Boundaries.every((entry) =>
      exact(entry, ['RelativePath', 'Identity']) && text(entry.RelativePath, 4096) && entry.RelativePath !== '.' &&
      !entry.RelativePath.startsWith('/') && entry.RelativePath === path.posix.normalize(entry.RelativePath) && identity(entry.Identity)) &&
    new Set(value.Boundaries.map((entry) => entry.RelativePath)).size === value.Boundaries.length;
}
function registeredRoot(value: unknown, fixture: Fixture, libraryID: string): RegisteredRoot {
  requireValue(exact(value, ['Id', 'LibraryId', 'Path', 'AllowedPath', 'RelativePath', 'Revision']) && typeof value.Id === 'string' && ID.test(value.Id) &&
    value.LibraryId === libraryID && typeof value.Path === 'string' && Object.values(fixture.paths).includes(value.Path) &&
    value.AllowedPath === fixture.runtime + '/app/media' && typeof value.RelativePath === 'string' && ['A', 'B', 'U'].includes(value.RelativePath) &&
    value.Path === value.AllowedPath + '/' + value.RelativePath && revision(value.Revision), 'registered_root_invalid');
  return value as unknown as RegisteredRoot;
}
function bindingDTO(value: unknown, fixture: Fixture, libraryID: string, rootID: string): RootBinding {
  requireValue(record(value), 'binding_dto_invalid');
  const fields = ['Id', 'LibraryId', 'Path', 'AllowedPath', 'RelativePath', 'Revision'];
  registeredRoot(Object.fromEntries(fields.map((key) => [key, value[key]])), fixture, libraryID);
  requireValue(Object.keys(value).every((key) => [...fields, 'Status', 'ApprovedFingerprint', 'ObservedFingerprint', 'Approved', 'Observed', 'BoundAt', 'BoundBy'].includes(key)) &&
    value.Id === rootID && ['verified', 'unbound', 'mismatch', 'unavailable'].includes(value.Status as string), 'binding_dto_invalid');
  const approved = value.Approved !== undefined, observed = value.Observed !== undefined;
  requireValue(approved === (value.ApprovedFingerprint !== undefined) && approved === (value.BoundAt !== undefined) && approved === (value.BoundBy !== undefined) &&
    observed === (value.ObservedFingerprint !== undefined), 'binding_optional_fields_invalid');
  if (approved) requireValue(topology(value.Approved) && typeof value.ApprovedFingerprint === 'string' && SHA.test(value.ApprovedFingerprint) &&
    typeof value.BoundBy === 'string' && ID.test(value.BoundBy) && typeof value.BoundAt === 'string' &&
    /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+]00:00)$/.test(value.BoundAt) && Number.isFinite(Date.parse(value.BoundAt)), 'binding_approval_invalid');
  if (observed) requireValue(topology(value.Observed) && typeof value.ObservedFingerprint === 'string' && SHA.test(value.ObservedFingerprint), 'binding_observation_invalid');
  requireValue(value.Status === 'unavailable' ? !observed : observed, 'binding_observation_status');
  requireValue(value.Status !== 'unbound' || !approved, 'binding_unbound_approval');
  requireValue(!['verified', 'mismatch'].includes(value.Status as string) || approved, 'binding_approval_missing');
  requireValue(value.Status !== 'verified' || value.ApprovedFingerprint === value.ObservedFingerprint, 'binding_verified_fingerprint');
  requireValue(value.Status !== 'mismatch' || value.ApprovedFingerprint !== value.ObservedFingerprint, 'binding_mismatch_fingerprint');
  return value as unknown as RootBinding;
}

class LiveNetwork {
  phase = 'browser_ready';
  libraryID: string | null = null;
  roots: Slots<RegisteredRoot> | null = null;
  token: string | null = null;
  csrf: string | null = null;
  loginResponse: Response | null = null;
  exchanges: Exchange[] = [];
  violations: string[] = [];
  pageErrors = 0;
  consoleErrors = 0;
  assetBytes = 0;
  assetRequests = 0;
  bootstrapPosts = 0;
  loginPosts = 0;
  libraryPosts = 0;
  storageObserved = false;
  logoutRequests = 0;
  puts = 0;
  expectedWrite: 'bootstrap' | 'login' | 'library' | 'logout' | null = null;
  expectedPut: { rootID: string; input: BindingInput } | null = null;
  readonly pending = new Set<Promise<unknown>>();
  readonly bodies = new WeakMap<Response, Promise<unknown>>();
  readonly ledger = new WeakMap<Request, Exchange>();
  constructor(readonly fixture: Fixture, readonly protocol: Protocol, readonly context: BrowserContext) {}
  bindingPath(rootID: string): string {
    requireValue(this.libraryID && ID.test(rootID), 'binding_route_identity');
    return `/admin/v1/libraries/${this.libraryID}/roots/${rootID}/binding`;
  }
  watch(operation: Promise<unknown>): void {
    this.pending.add(operation);
    void operation.catch(() => { this.violations.push('response_observation_failed'); }).finally(() => this.pending.delete(operation));
  }
  assertClean(): void { requireValue(this.violations.length === 0 && this.pageErrors === 0, 'network_or_page_failure'); }
  async settle(): Promise<void> {
    await bounded(Promise.all([...this.pending].map((operation) => operation.catch(() => undefined))), 5000, 'network_observer_timeout'); this.assertClean();
  }
  async install(page: Page): Promise<void> {
    page.on('pageerror', () => { this.pageErrors++; });
    page.on('console', (message) => { if (message.type() === 'error') this.consoleErrors++; });
    page.on('popup', () => { this.violations.push('unexpected_popup'); });
    page.on('download', () => { this.violations.push('unexpected_download'); });
    page.on('websocket', () => { this.violations.push('unexpected_websocket'); });
    await this.context.route('**/*', async (route, request) => {
      try {
        const url = new URL(request.url()), method = request.method();
        requireValue(url.origin === ORIGIN && !url.username && !url.password && !url.search && !url.hash &&
          url.href === request.url(), 'request_origin_or_query');
        if (!url.pathname.startsWith('/admin/v1/')) {
          const relative = ['/admin/', '/admin/libraries'].includes(url.pathname) ? 'index.html'
            : url.pathname.startsWith('/admin/') ? url.pathname.slice('/admin/'.length) : '';
          requireValue(method === 'GET' && relative && Object.hasOwn(this.fixture.web_files, relative) && ++this.assetRequests <= 192, 'static_route_rejected');
          await route.continue(); return;
        }
        requireValue(this.exchanges.length < 128, 'native_request_limit');
        const raw = request.postDataBuffer(), bytes = raw ? Buffer.from(raw) : null; let body: unknown = null;
        try { if (bytes?.length) body = strictJSON(bytes, 4096); }
        finally { bytes?.fill(0); }
        const entry: Exchange = { sequence: this.exchanges.length + 1, phase: this.phase, method, path: url.pathname, status: null,
          request_bytes: request.postDataBuffer()?.length ?? 0, body_keys: record(body) ? Object.keys(body).sort() : [], complete: false };
        this.exchanges.push(entry); this.ledger.set(request, entry);
        const headers = await request.allHeaders();
        if (method === 'GET') {
          if (this.libraryID && !this.roots && url.pathname.startsWith(`/admin/v1/libraries/${this.libraryID}/roots/`) && url.pathname.endsWith('/binding')) {
            const until = Date.now() + 10000;
            while (!this.roots && this.violations.length === 0 && Date.now() < until) await delay(20);
          }
          requireValue(body === null && ['/admin/v1/bootstrap', '/admin/v1/session', '/admin/v1/storage/roots', '/admin/v1/libraries',
            ...(this.libraryID ? [`/admin/v1/libraries/${this.libraryID}/roots`] : [])].includes(url.pathname) ||
            method === 'GET' && body === null && this.roots && Object.values(this.roots).some((root) => this.bindingPath(root.Id) === url.pathname), 'native_read_rejected');
        } else {
          await this.protocol.check();
          requireValue(headers.origin === ORIGIN, 'native_origin_missing');
          if (url.pathname === '/admin/v1/bootstrap' && method === 'POST') {
            requireValue(this.expectedWrite === 'bootstrap' && this.bootstrapPosts++ === 0 &&
              exact(body, ['SetupToken', 'Name', 'Password']) && body.SetupToken === this.fixture.administrator.setup_token &&
              body.Name === this.fixture.administrator.name && body.Password === this.fixture.administrator.password, 'bootstrap_request_invalid');
            this.expectedWrite = null;
          } else if (url.pathname === '/admin/v1/session' && method === 'POST') {
            requireValue(this.expectedWrite === 'login' && this.loginPosts++ === 0 && exact(body, ['Name', 'Password']) &&
              body.Name === this.fixture.administrator.name && body.Password === this.fixture.administrator.password, 'login_request_invalid');
            this.expectedWrite = null;
          } else {
            requireValue(this.token && this.csrf && headers.cookie === `goby_session=${this.token}` && headers['x-csrf-token'] === this.csrf, 'native_session_authority');
            if (url.pathname === '/admin/v1/libraries' && method === 'POST') {
              requireValue(this.expectedWrite === 'library' && this.libraryPosts++ === 0 && exact(body, ['Name', 'CollectionType', 'Paths', 'Scan']) &&
                body.Name === this.fixture.library.name && body.CollectionType === 'movies' && body.Scan === false &&
                same(body.Paths, [this.fixture.paths.A, this.fixture.paths.B, this.fixture.paths.U]), 'library_request_invalid');
              this.expectedWrite = null;
            } else if (url.pathname === '/admin/v1/session' && method === 'DELETE') {
              requireValue(this.expectedWrite === 'logout' && this.logoutRequests++ === 0 && body === null, 'logout_request_invalid'); this.expectedWrite = null;
            } else {
              requireValue(method === 'PUT' && this.expectedPut && this.roots &&
                Object.values(this.roots).some((root) => root.Id === this.expectedPut?.rootID) &&
                url.pathname === this.bindingPath(this.expectedPut.rootID) && exact(body, ['Revision', 'ObservedFingerprint', 'AcknowledgeMissingRemoval']) &&
                same(body, this.expectedPut.input) && this.puts < 3, 'binding_put_not_armed');
              entry.binding_input = clone(this.expectedPut.input); this.expectedPut = null; this.puts++;
            }
          }
        }
        await route.continue();
      } catch {
        this.violations.push('outbound_request_rejected'); await route.abort('blockedbyclient');
      }
    });
    this.context.on('response', (response) => {
      const request = response.request(), url = new URL(response.url());
      if (url.origin !== ORIGIN) { this.violations.push('response_origin_rejected'); return; }
      const operation = (async (): Promise<unknown> => {
        if (!url.pathname.startsWith('/admin/v1/')) {
          const relative = ['/admin/', '/admin/libraries'].includes(url.pathname) ? 'index.html' : url.pathname.slice('/admin/'.length);
          requireValue(Object.hasOwn(this.fixture.web_files, relative) && response.status() === 200, 'static_response_invalid');
          const bytes = await bounded(response.body(), 10000, 'static_response_timeout');
          try {
            this.assetBytes += bytes.length;
            requireValue(bytes.length <= 16 * 1024 * 1024 && this.assetBytes <= 64 * 1024 * 1024 &&
              hash(bytes) === this.fixture.web_files[relative], 'static_response_digest');
          } finally { bytes.fill(0); }
          return null;
        }
        const entry = this.ledger.get(request); requireValue(entry, 'unrecorded_native_response'); entry.status = response.status();
        if (entry.method === 'POST' && entry.path === '/admin/v1/session') this.loginResponse = response;
        const bytes = await bounded(response.body(), 10000, 'native_response_timeout');
        try {
          entry.response_bytes = bytes.length; requireValue(bytes.length <= JSON_LIMIT, 'native_response_limit');
          if (!['/admin/v1/bootstrap', '/admin/v1/session'].includes(entry.path)) entry.response_sha256 = hash(bytes);
          const value = bytes.length ? strictJSON(bytes) : null;
          if (entry.method === 'GET' && entry.path === '/admin/v1/storage/roots' && response.status() === 200) {
            requireValue(exact(value, ['Configured', 'Items']) && value.Configured === true && Array.isArray(value.Items) && value.Items.length === 1 &&
              exact(value.Items[0], ['Path', 'Available']) && value.Items[0].Path === this.fixture.runtime + '/app/media' && value.Items[0].Available === true,
            'configured_storage_scope');
            this.storageObserved = true;
          }
          if (entry.method === 'GET' && this.libraryID && entry.path === `/admin/v1/libraries/${this.libraryID}/roots` && response.status() === 200) {
            requireValue(exact(value, ['Items', 'TotalRecordCount']) && value.TotalRecordCount === 3 && Array.isArray(value.Items) && value.Items.length === 3, 'registered_roots_invalid');
            const roots = value.Items.map((root) => registeredRoot(root, this.fixture, this.libraryID!));
            requireValue(new Set(roots.map((root) => root.Id)).size === 3 && new Set(roots.map((root) => root.RelativePath)).size === 3, 'registered_roots_ambiguous');
            this.roots = Object.fromEntries(roots.map((root) => [root.RelativePath, root])) as Slots<RegisteredRoot>;
          }
          requireValue(await response.finished() === null, 'native_transfer_failed'); entry.complete = true; return value;
        } finally { bytes.fill(0); }
      })();
      this.bodies.set(response, operation); this.watch(operation);
    });
  }
  async value(response: Response): Promise<unknown> {
    const pending = this.bodies.get(response); requireValue(pending, 'response_not_observed'); return bounded(pending, 10000, 'response_decode_timeout');
  }
  response(page: Page, method: string, pathname: string): Promise<Response> {
    return page.waitForResponse((response) => response.request().method() === method && new URL(response.url()).origin === ORIGIN &&
      new URL(response.url()).pathname === pathname, { timeout: 15000 });
  }
  async observe(page: Page, method: string, pathname: string, action: () => Promise<unknown>): Promise<{ response: Response; value: unknown }> {
    const pending = this.response(page, method, pathname);
    const [response] = await Promise.all([pending, action()]); return { response, value: await this.value(response) };
  }
}

function chromiumExecutable(): string {
  const executable = process.env.GOBY_BINDING_CHROMIUM_EXECUTABLE;
  requireValue(process.platform === 'linux' && process.getuid?.() === 0 && typeof executable === 'string' &&
    executable === '/root/.cache/ms-playwright/chromium_headless_shell-1243/chrome-headless-shell-linux64/chrome-headless-shell', 'pinned_chromium_required');
  const file = lstatSync(executable);
  requireValue(file.isFile() && !file.isSymbolicLink() && file.uid === 0 && (file.mode & 0o022) === 0 && (file.mode & 0o111) !== 0, 'chromium_executable_ownership');
  let directory = path.posix.dirname(executable);
  for (;;) {
    const status = lstatSync(directory);
    requireValue(status.isDirectory() && !status.isSymbolicLink() && status.uid === 0 && (status.mode & 0o022) === 0, 'chromium_ancestor_ownership');
    if (directory === '/') break; directory = path.posix.dirname(directory);
  }
  return executable;
}

const test = base.extend({
  launchOptions: [async ({}, use) => { await use({ executablePath: chromiumExecutable() }); }, { scope: 'worker' }],
});
test.use({ baseURL: ORIGIN, viewport: { width: 1440, height: 1000 }, serviceWorkers: 'block', trace: 'off', video: 'off',
  screenshot: 'off' });

const dialog = (page: Page) => page.getByRole('dialog', { name: 'Storage bindings', exact: true });
const consent = (page: Page) => dialog(page).getByRole('checkbox', { name: /^I understand that a later complete scan/ });
const submit = (page: Page) => dialog(page).getByRole('button', { name: /^(Bind storage|Accept replacement)$/ });
const fact = (page: Page, label: string) => dialog(page).locator('dt').filter({ hasText: new RegExp('^' + label + '$') }).locator('xpath=following-sibling::dd[1]');
const rootIDs = (roots: Slots<RegisteredRoot>): Slots<string> => ({ A: roots.A.Id, B: roots.B.Id, U: roots.U.Id });

function proofState(proof: IPCProof, libraryID: string | null, roots: Slots<string> | null, revisions: Slots<string> | null, updates: number): void {
  requireValue(proof.library_id === libraryID && same(proof.root_ids, roots) && same(proof.revisions, revisions) && proof.binding_updates === updates &&
    proof.scan_jobs === 0, 'independent_state_proof_mismatch');
}
async function bindingUI(page: Page, binding: RootBinding): Promise<void> {
  const labels = { verified: 'Verified', unbound: 'Unbound', mismatch: 'Storage changed', unavailable: 'Unavailable' };
  await expect(dialog(page).getByRole('alert').getByText(labels[binding.Status], { exact: true })).toBeVisible();
  await expect(fact(page, 'Registered root path')).toHaveText(binding.Path);
  await expect(fact(page, 'Storage anchor path')).toHaveText(binding.AllowedPath);
  await expect(fact(page, 'Root ID')).toHaveText(binding.Id);
  await expect(fact(page, 'Binding revision')).toHaveText(binding.Revision);
  if (binding.BoundBy && binding.BoundAt) {
    await expect(fact(page, 'Last approved by')).toHaveText(binding.BoundBy);
    await expect(dialog(page).locator(`time[datetime="${binding.BoundAt}"]`)).toBeVisible();
  }
}
async function responseBinding(network: LiveNetwork, response: Response, rootID: string): Promise<RootBinding> {
  requireValue(response.status() === 200 && response.headers()['cache-control'] === 'no-store' && network.libraryID, 'binding_http_failed');
  const value = await network.value(response); requireValue(exact(value, ['Binding']), 'binding_envelope_invalid');
  return bindingDTO(value.Binding, network.fixture, network.libraryID, rootID);
}
async function refreshBinding(page: Page, network: LiveNetwork, rootID: string): Promise<RootBinding> {
  const { response } = await network.observe(page, 'GET', network.bindingPath(rootID),
    () => dialog(page).getByRole('button', { name: 'Refresh observation', exact: true }).click());
  const binding = await responseBinding(network, response, rootID); await bindingUI(page, binding); await network.settle(); return binding;
}
async function selectBinding(page: Page, network: LiveNetwork, rootID: string): Promise<RootBinding> {
  await dialog(page).getByRole('combobox', { name: 'Registered root', exact: true }).click();
  const options = page.getByRole('option').filter({ hasText: rootID }); await expect(options).toHaveCount(1);
  const { response } = await network.observe(page, 'GET', network.bindingPath(rootID), () => options.click());
  const binding = await responseBinding(network, response, rootID); await bindingUI(page, binding); await network.settle(); return binding;
}
async function openBindings(page: Page, network: LiveNetwork): Promise<RootBinding> {
  requireValue(network.libraryID, 'library_identity_missing');
  const roots = network.response(page, 'GET', `/admin/v1/libraries/${network.libraryID}/roots`);
  const observed = page.waitForResponse((response) => response.request().method() === 'GET' &&
    new URL(response.url()).origin === ORIGIN && new RegExp(`^/admin/v1/libraries/${network.libraryID}/roots/[0-9a-f]{32}/binding$`).test(new URL(response.url()).pathname), { timeout: 15000 });
  await page.getByRole('button', { name: `Storage bindings for ${network.fixture.library.name}`, exact: true }).click();
  const [listed, first] = await Promise.all([roots, observed]); requireValue(listed.status() === 200, 'root_list_failed'); await network.value(listed);
  requireValue(network.roots, 'root_list_not_recorded'); await expect(dialog(page)).toBeVisible();
  const rootID = new URL(first.url()).pathname.split('/').at(-2)!;
  const binding = await responseBinding(network, first, rootID); await bindingUI(page, binding); return binding;
}
async function chosen(page: Page, network: LiveNetwork, current: RootBinding, rootID: string): Promise<RootBinding> {
  return current.Id === rootID ? refreshBinding(page, network, rootID) : selectBinding(page, network, rootID);
}
function assertVerified(binding: RootBinding, expectedRevision: string, administratorID: string): void {
  requireValue(binding.Status === 'verified' && binding.Revision === expectedRevision && binding.BoundBy === administratorID && binding.BoundAt &&
    binding.ApprovedFingerprint === binding.ObservedFingerprint && same(binding.Approved, binding.Observed), 'verified_binding_invalid');
}
function assertMismatch(binding: RootBinding, previous: RootBinding): void {
  requireValue(binding.Status === 'mismatch' && binding.Revision === previous.Revision && same(binding.Approved, previous.Approved) &&
    binding.ApprovedFingerprint === previous.ApprovedFingerprint && binding.BoundAt === previous.BoundAt && binding.BoundBy === previous.BoundBy &&
    binding.Observed && binding.Approved && same(binding.Observed.Anchor, binding.Approved.Anchor) &&
    same(binding.Observed.RegisteredRoot, binding.Approved.RegisteredRoot) && binding.Observed.Boundaries.length === 1 &&
    binding.Approved.Boundaries.length === 1 && binding.Observed.Boundaries[0].RelativePath === 'archive' &&
    binding.Approved.Boundaries[0].RelativePath === 'archive' &&
    !same(binding.Observed.Boundaries[0].Identity, binding.Approved.Boundaries[0].Identity), 'mismatch_binding_invalid');
}
async function approve(page: Page, network: LiveNetwork, binding: RootBinding, expectedStatus: 200 | 409): Promise<{ response: Response; input: BindingInput; binding?: RootBinding }> {
  requireValue(binding.ObservedFingerprint && SHA.test(binding.ObservedFingerprint), 'approval_observation_missing');
  await expect(consent(page)).not.toBeChecked(); await expect(submit(page)).toBeDisabled();
  await consent(page).check(); await expect(submit(page)).toBeEnabled();
  const input: BindingInput = { Revision: binding.Revision, ObservedFingerprint: binding.ObservedFingerprint, AcknowledgeMissingRemoval: true };
  requireValue(network.expectedPut === null, 'approval_already_armed'); network.expectedPut = { rootID: binding.Id, input };
  const { response, value } = await network.observe(page, 'PUT', network.bindingPath(binding.Id), () => submit(page).click());
  requireValue(response.status() === expectedStatus && network.expectedPut === null, 'approval_response_status');
  if (expectedStatus === 409) {
    requireValue(record(value) && record(value.Error) && value.Error.Code === 'root_binding_conflict', 'real_conflict_not_observed');
    await expect(dialog(page).getByText(/The registered root or storage changed after this observation/)).toBeVisible();
    await expect(consent(page)).not.toBeChecked(); await expect(consent(page)).toBeDisabled(); await expect(submit(page)).toBeDisabled();
    await network.settle(); return { response, input };
  }
  requireValue(exact(value, ['Binding']) && network.libraryID, 'approval_envelope_invalid');
  const accepted = bindingDTO(value.Binding, network.fixture, network.libraryID, binding.Id);
  requireValue(accepted.Revision === (BigInt(binding.Revision) + 1n).toString() && accepted.ObservedFingerprint === binding.ObservedFingerprint &&
    accepted.Status === 'verified', 'approval_receipt_invalid');
  await expect(dialog(page).getByText('Storage binding saved.', { exact: true })).toBeVisible();
  await bindingUI(page, accepted); await expect(consent(page)).toHaveCount(0); await expect(submit(page)).toBeDisabled();
  await network.settle(); return { response, input, binding: accepted };
}

async function captureCookie(context: BrowserContext, response: Response | null): Promise<{ token: string; attributesValid: boolean } | null> {
  const canonical = (value: unknown): value is string => typeof value === 'string' && /^[A-Za-z0-9_-]{43}$/.test(value) &&
    Buffer.from(value, 'base64url').length === 32 && Buffer.from(value, 'base64url').toString('base64url') === value;
  let headerToken: string | null = null, headerValid = false;
  if (response) {
    const headers = await bounded(response.headersArray(), 2000, 'login_headers_timeout');
    const values = headers.filter((header) => header.name.toLowerCase() === 'set-cookie');
    if (values.length === 1) {
      const match = /^goby_session=([A-Za-z0-9_-]{43})(?:;|$)/.exec(values[0].value);
      if (match && canonical(match[1])) {
        headerToken = match[1]; headerValid = /;\s*Path=\/admin(?:;|$)/i.test(values[0].value) && /;\s*HttpOnly(?:;|$)/i.test(values[0].value) &&
          /;\s*SameSite=Strict(?:;|$)/i.test(values[0].value) && !/;\s*Secure(?:;|$)/i.test(values[0].value);
      }
    }
  }
  const cookies = (await context.cookies(ORIGIN + '/admin/libraries')).filter((cookie) => cookie.name === 'goby_session');
  const cookie = cookies.length === 1 && canonical(cookies[0].value) ? cookies[0] : null;
  const token = headerToken ?? cookie?.value;
  if (!token) return null;
  return { token, attributesValid: headerValid && cookie?.value === token && cookie.domain === '127.0.0.1' && cookie.path === '/admin' &&
    cookie.httpOnly === true && cookie.secure === false && cookie.sameSite === 'Strict' };
}

test.describe('storage binding with the real isolated backend', () => {
  test.skip(!process.env.GOBY_BINDING_LIVE_FIXTURE, 'This separate live gate requires its new isolated operator fixture.');
  test.describe.configure({ mode: 'serial', retries: 0, timeout: 240000 });

  test('real registration, stale conflict, replacement, unavailable storage and initial binding', async ({ page, context, browser }, testInfo) => {
    process.umask(0o077); requireValue(testInfo.retry === 0, 'browser_retry_forbidden');
    const { fixture, fixtureHash, fixturePath } = await loadFixture();
    const protocol = new Protocol(fixture, fixtureHash, fixturePath, Date.now() + 210000);
    const network = new LiveNetwork(fixture, protocol, context);
    const checks: Record<string, boolean> = {};
    const browserVersion = browser.version(); requireValue(browserVersion === '153.0.8010.12', 'browser_version_mismatch'); checks.browser_version_pinned = true;
    const screenshots: { path: string; sha256: string }[] = [];
    let complete = false, failure: string | null = null, administratorID: string | null = null;
    let library: Library | null = null, roots: Slots<RegisteredRoot> | null = null, bindings: Slots<RootBinding> | null = null;
    let session: SessionDocument | null = null, sessionHash: string | null = null;
    const screenshotsDirectory = fixture.output + '/private/screenshots';
    const saveScreenshot = async (name: 'desktop' | 'narrow') => {
      const filename = screenshotsDirectory + '/' + name + '.png';
      requireValue(await optionalStat(filename) === null, 'screenshot_already_exists');
      await page.screenshot({ path: filename, fullPage: true, animations: 'disabled', timeout: 5000 });
      const status = await fs.lstat(filename);
      requireValue(status.isFile() && !status.isSymbolicLink() && status.nlink === 1 && status.uid === 0 &&
        (status.mode & 0o777) === 0o600 && status.size <= 4 * 1024 * 1024, 'screenshot_ownership');
      const bytes = await privateRead(filename, 4 * 1024 * 1024);
      try { screenshots.push({ path: filename, sha256: hash(bytes) }); } finally { bytes.fill(0); }
    };
    try {
      await fs.mkdir(screenshotsDirectory, { mode: 0o700 });
      const ready = await protocol.step('browser_ready', { fixture_sha256: fixtureHash, runner: { pid: process.pid,
        start_ticks: startTicks(await fs.readFile('/proc/self/stat', 'utf8')), namespace: await fs.readlink('/proc/self/ns/mnt') }, origin: ORIGIN });
      proofState(ready, null, null, null, 0); checks.backend_ready_before_http = true;
      await network.install(page); network.phase = 'baseline_created';
      const bootstrap = network.response(page, 'GET', '/admin/v1/bootstrap');
      await page.goto(ORIGIN + '/admin/libraries', { waitUntil: 'domcontentloaded', timeout: 15000 });
      const initial = await network.value(await bootstrap); requireValue(record(initial) && initial.Initialized === false, 'fresh_bootstrap_required');
      await page.getByLabel(/^Setup token/).fill(fixture.administrator.setup_token);
      await page.getByLabel(/^Administrator username/).fill(fixture.administrator.name);
      await page.getByLabel(/^Password/).fill(fixture.administrator.password);
      network.expectedWrite = 'bootstrap';
      const created = await network.observe(page, 'POST', '/admin/v1/bootstrap',
        () => page.getByRole('button', { name: 'Create administrator', exact: true }).click());
      requireValue(created.response.status() === 201 && exact(created.value, ['User']) && record(created.value.User) &&
        typeof created.value.User.Id === 'string' && ID.test(created.value.User.Id) && created.value.User.Name === fixture.administrator.name &&
        created.value.User.IsAdministrator === true && created.value.User.IsDisabled === false, 'administrator_bootstrap_failed');
      administratorID = created.value.User.Id;
      checks.bootstrap_created_by_ui = true;
      await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
      await page.getByLabel(/^Username/).fill(fixture.administrator.name); await page.getByLabel(/^Password/).fill(fixture.administrator.password);
      network.expectedWrite = 'login';
      const login = await network.observe(page, 'POST', '/admin/v1/session', () => page.getByRole('button', { name: 'Sign in', exact: true }).click());
      const cookie = await captureCookie(context, login.response); requireValue(cookie, 'login_cookie_missing');
      network.token = cookie.token; network.csrf = hash('goby:admin:csrf:' + cookie.token);
      requireValue(login.response.status() === 200 && cookie.attributesValid && exact(login.value, ['User', 'CSRFToken']) &&
        record(login.value.User) && login.value.User.Id === administratorID && login.value.User.IsAdministrator === true &&
        login.value.User.IsDisabled === false && login.value.CSRFToken === network.csrf, 'native_login_invalid');
      session = { marker: 'goby-storage-binding-live-ui-session-v1', run_id: fixture.run_id, nonce: fixture.nonce,
        token: network.token, csrf_token: network.csrf, user_id: administratorID, validated: true };
      sessionHash = await publish(fixture.session_path, session); checks.native_login = true;
      await expect(page.getByRole('heading', { name: 'Libraries', exact: true })).toBeVisible(); await network.settle();
      await expect.poll(() => network.storageObserved, { timeout: 5000, intervals: [50, 100, 250] }).toBe(true);
      checks.configured_storage_only = true;
      await page.getByRole('button', { name: 'Create library', exact: true }).first().click();
      const creation = page.getByRole('dialog', { name: 'Create library', exact: true });
      await creation.getByLabel(/^Library name/).fill(fixture.library.name);
      await creation.getByRole('combobox', { name: /^Content type/ }).click(); await page.getByRole('option', { name: 'Movies', exact: true }).click();
      await creation.getByLabel(/^Media directories/).fill([fixture.paths.A, fixture.paths.B, fixture.paths.U].join('\n'));
      await creation.getByRole('checkbox', { name: /^Scan after creating/ }).uncheck(); network.expectedWrite = 'library';
      const added = await network.observe(page, 'POST', '/admin/v1/libraries', () => creation.getByRole('button', { name: 'Create library', exact: true }).click());
      requireValue(added.response.status() === 201 && exact(added.value, ['Library']) && record(added.value.Library) &&
        typeof added.value.Library.Id === 'string' && ID.test(added.value.Library.Id) && added.value.Library.Name === fixture.library.name &&
        added.value.Library.CollectionType === 'movies' && same(added.value.Library.Paths, [fixture.paths.A, fixture.paths.B, fixture.paths.U]) &&
        added.value.Library.LastScanAt === null, 'unscanned_library_creation_failed');
      library = added.value.Library as unknown as Library; network.libraryID = library.Id;
      await expect(creation).not.toBeVisible();
      let current = await openBindings(page, network); requireValue(network.roots, 'registered_roots_missing'); roots = clone(network.roots);
      const baseA = await chosen(page, network, current, roots.A.Id); assertVerified(baseA, '1', administratorID);
      const baseB = await selectBinding(page, network, roots.B.Id); assertVerified(baseB, '1', administratorID);
      const baseU = await selectBinding(page, network, roots.U.Id);
      requireValue(baseA.Approved?.Boundaries.length === 1 && baseA.Approved.Boundaries[0].RelativePath === 'archive' &&
        baseB.Approved?.Boundaries.length === 0 && baseU.Revision === '1' && baseU.Approved === undefined &&
        baseU.BoundAt === undefined && baseU.BoundBy === undefined && ['unavailable', 'unbound'].includes(baseU.Status), 'baseline_storage_capability_failed');
      bindings = { A: baseA, B: baseB, U: baseU };
      const identity = rootIDs(roots);
      proofState(await protocol.step('baseline_created', { administrator_id: administratorID, library, roots, bindings,
        session: { path: fixture.session_path, sha256: sessionHash }, ui_puts: 0 }), library.Id, identity, { A: '1', B: '1', U: '1' }, 0);
      checks.automatic_registration_and_unbound = true;

      network.phase = 'unbound_exposed';
      proofState(await protocol.step('unbound_exposed', { library_id: library.Id, root_ids: identity }), library.Id, identity, { A: '1', B: '1', U: '1' }, 0);
      let unbound = await refreshBinding(page, network, roots.U.Id);
      requireValue(unbound.Status === 'unbound' && unbound.Revision === '1' && unbound.Observed && unbound.Observed.Boundaries.length === 0, 'supported_unbound_not_observed');
      await expect(consent(page)).not.toBeChecked(); await expect(submit(page)).toBeDisabled();
      await consent(page).check(); unbound = await refreshBinding(page, network, roots.U.Id);
      await expect(consent(page)).not.toBeChecked(); await expect(submit(page)).toBeDisabled();
      await consent(page).check(); const control = await selectBinding(page, network, roots.B.Id); requireValue(same(control, baseB), 'control_root_changed');
      unbound = await selectBinding(page, network, roots.U.Id); await expect(consent(page)).not.toBeChecked(); requireValue(network.puts === 0, 'unexpected_approval');
      checks.refresh_and_selection_clear_consent = true;

      network.phase = 'a_source2';
      proofState(await protocol.step('a_source2', { library_id: library.Id, root_ids: identity, unbound, control,
        consent_reset: true, ui_puts: 0 }), library.Id, identity, { A: '1', B: '1', U: '1' }, 0);
      const stale = await selectBinding(page, network, roots.A.Id); assertMismatch(stale, baseA);
      await expect(dialog(page).getByRole('button', { name: 'archive Changed', exact: true })).toBeVisible();
      const conflictWriter = await protocol.step('controller_approved', { library_id: library.Id, root_ids: identity, stale, ui_puts: 0 });
      proofState(conflictWriter, library.Id, identity, { A: '2', B: '1', U: '1' }, 1);
      const approved2 = bindingDTO(conflictWriter.binding, fixture, library.Id, roots.A.Id); assertVerified(approved2, '2', administratorID);
      requireValue(approved2.ApprovedFingerprint === stale.ObservedFingerprint, 'controller_observation_changed');
      network.phase = 'controller_approved';
      const rejected = await approve(page, network, stale, 409); requireValue(network.puts === 1, 'conflict_was_retried');
      const refreshed2 = await refreshBinding(page, network, roots.A.Id); requireValue(same(refreshed2, approved2), 'conflict_refresh_mismatch');
      await expect(consent(page)).toHaveCount(0); await expect(submit(page)).toBeDisabled(); checks.real_stale_conflict = true;

      network.phase = 'a_source3';
      proofState(await protocol.step('a_source3', { library_id: library.Id, root_ids: identity,
        conflict: { status: 409, code: 'root_binding_conflict', request: rejected.input }, refreshed: refreshed2, ui_puts: 1 }),
      library.Id, identity, { A: '2', B: '1', U: '1' }, 1);
      const replacement = await refreshBinding(page, network, roots.A.Id); assertMismatch(replacement, approved2);
      await expect(dialog(page).getByRole('button', { name: 'archive Changed', exact: true })).toBeVisible();
      await expect(dialog(page).getByText(replacement.Approved!.Boundaries[0].Identity.Digest, { exact: true })).toBeVisible();
      await expect(dialog(page).getByText(replacement.Observed!.Boundaries[0].Identity.Digest, { exact: true })).toBeVisible();
      await saveScreenshot('desktop');
      const acceptedA = (await approve(page, network, replacement, 200)).binding!; assertVerified(acceptedA, '3', administratorID);
      checks.browser_replacement_saved = true;

      network.phase = 'a_unavailable';
      proofState(await protocol.step('a_unavailable', { library_id: library.Id, root_ids: identity, accepted: acceptedA, ui_puts: 2 }),
        library.Id, identity, { A: '3', B: '1', U: '1' }, 2);
      const unavailable = await refreshBinding(page, network, roots.A.Id);
      requireValue(unavailable.Status === 'unavailable' && unavailable.Revision === '3' && unavailable.Observed === undefined &&
        unavailable.ObservedFingerprint === undefined && same(unavailable.Approved, acceptedA.Approved) && unavailable.BoundBy === acceptedA.BoundBy &&
        unavailable.BoundAt === acceptedA.BoundAt, 'real_unavailable_not_observed');
      await expect(dialog(page).getByRole('button', { name: 'archive Not observed', exact: true })).toBeVisible();
      await expect(dialog(page).getByText('Removed', { exact: true })).toHaveCount(0); await expect(consent(page)).toHaveCount(0);
      await expect(submit(page)).toBeDisabled(); requireValue(network.puts === 2, 'unavailable_was_approved'); checks.unavailable_preserves_approval = true;

      network.phase = 'a_restored';
      proofState(await protocol.step('a_restored', { library_id: library.Id, root_ids: identity, unavailable, ui_puts: 2 }),
        library.Id, identity, { A: '3', B: '1', U: '1' }, 2);
      const restoredA = await refreshBinding(page, network, roots.A.Id); requireValue(same(restoredA, acceptedA), 'restored_root_changed');
      await page.setViewportSize({ width: 390, height: 844 });
      await dialog(page).getByRole('button', { name: 'Close', exact: true }).click(); current = await openBindings(page, network);
      for (const slot of ['A', 'B', 'U'] as Slot[]) {
        current = await chosen(page, network, current, roots[slot].Id);
        const selection = dialog(page).getByRole('combobox', { name: 'Registered root', exact: true });
        await expect(selection).toBeInViewport({ ratio: 1 }); await expect(selection).toContainText(roots[slot].Path); await expect(selection).toContainText(roots[slot].Id);
        const description = await selection.getAttribute('aria-describedby'); requireValue(description, 'root_description_missing');
        await expect(selection).toHaveAccessibleDescription(new RegExp(roots[slot].Path.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s+Root ID: ' + roots[slot].Id));
        await expect(fact(page, 'Registered root path')).toHaveText(roots[slot].Path); await expect(fact(page, 'Root ID')).toHaveText(roots[slot].Id);
        await expect(dialog(page).getByRole('button', { name: 'Refresh observation', exact: true })).toBeInViewport({ ratio: 1 });
        requireValue(await dialog(page).evaluate((element) => element.scrollWidth <= element.clientWidth && element.getBoundingClientRect().width <= innerWidth) &&
          await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'narrow_horizontal_overflow');
      }
      requireValue(current.Id === roots.U.Id && current.Status === 'unbound', 'narrow_unbound_selection_failed');
      await consent(page).check(); await expect(submit(page)).toBeEnabled();
      const narrowB = await selectBinding(page, network, roots.B.Id); requireValue(same(narrowB, baseB), 'control_root_changed');
      await expect(consent(page)).toHaveCount(0); unbound = await selectBinding(page, network, roots.U.Id);
      await expect(consent(page)).not.toBeChecked(); await expect(submit(page)).toBeDisabled(); await expect(submit(page)).toBeInViewport({ ratio: 1 });
      const acceptedU = (await approve(page, network, unbound, 200)).binding!; assertVerified(acceptedU, '2', administratorID);
      await saveScreenshot('narrow'); checks.narrow_selection_and_initial_bind = true;
      const finalA = await selectBinding(page, network, roots.A.Id), finalB = await selectBinding(page, network, roots.B.Id), finalU = await selectBinding(page, network, roots.U.Id);
      requireValue(same(finalA, acceptedA) && same(finalB, baseB) && same(finalU, acceptedU) && network.puts === 3, 'final_bindings_changed');
      bindings = { A: finalA, B: finalB, U: finalU }; network.phase = 'final_bindings';
      const final = await protocol.step('final_bindings', { library_id: library.Id, root_ids: identity, bindings,
        narrow: { width: 390, height: 844, no_horizontal_overflow: true, paths_ids_visible: true, consent_reset: true }, ui_puts: 3 });
      proofState(final, library.Id, identity, { A: '3', B: '1', U: '2' }, 3);
      requireValue(same(final.bindings, bindings), 'independent_final_bindings_mismatch'); checks.independent_final_state = true;

      network.phase = 'browser_logged_out'; await dialog(page).getByRole('button', { name: 'Close', exact: true }).click();
      network.expectedWrite = 'logout';
      const logout = await network.observe(page, 'DELETE', '/admin/v1/session', () => page.getByRole('button', { name: 'Sign out', exact: true }).click());
      requireValue(logout.response.status() === 204 && logout.value === null, 'ui_logout_failed');
      await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
      const exactCookie = await apiRequest.newContext({ baseURL: ORIGIN, extraHTTPHeaders: { Cookie: `goby_session=${network.token}` }, timeout: 5000 });
      try {
        const rejectedCookie = await exactCookie.get('/admin/v1/session', { maxRedirects: 0, timeout: 5000 });
        requireValue(rejectedCookie.status() === 401, 'exact_cookie_not_revoked');
        const bytes = await rejectedCookie.body(); try { requireValue(bytes.length <= JSON_LIMIT, 'logout_proof_limit'); } finally { bytes.fill(0); }
      } finally { await exactCookie.dispose(); }
      proofState(await protocol.step('browser_logged_out', { library_id: library.Id, root_ids: identity, logout_status: 204, exact_cookie_status: 401, ui_puts: 3 }),
        library.Id, identity, { A: '3', B: '1', U: '2' }, 3);
      checks.owned_ui_logout_and_exact_rejection = true;
      await network.settle(); requireValue(network.bootstrapPosts === 1 && network.loginPosts === 1 && network.libraryPosts === 1 &&
        network.logoutRequests === 1 && network.puts === 3 && protocol.stages.length === 10 &&
        same(network.exchanges.filter((entry) => entry.method === 'PUT').map((entry) => entry.status), [409, 200, 200]), 'browser_mutation_budget');
      checks.exact_mutation_budget = true;
      complete = true;
    } catch (error) {
      try { await bounded(publishFailureLocation(fixture, error, network.phase), 5000, 'failure_location_publish_timeout'); }
      catch { /* Preserve the original failure if its optional location sidecar cannot be published. */ }
      failure = error instanceof Error && /^[a-z][a-z0-9_]{0,79}$/.test(error.message) ? error.message : 'live_ui_assertion_failed';
      throw new Error('storage_binding_live_ui_failed');
    } finally {
      try {
        if (!sessionHash && network.loginPosts > 0) {
          try {
            const cookie = await captureCookie(context, network.loginResponse);
            if (cookie) {
              session = { marker: 'goby-storage-binding-live-ui-session-v1', run_id: fixture.run_id, nonce: fixture.nonce,
                token: cookie.token, csrf_token: hash('goby:admin:csrf:' + cookie.token), user_id: null, validated: false };
              sessionHash = await publish(fixture.session_path, session);
            }
          } catch { checks.provisional_session_preserved = false; }
        }
        try { await bounded(context.close(), 10000, 'browser_context_close_timeout'); checks.browser_context_closed = true; }
        catch { complete = false; failure ??= 'browser_context_close_failed'; checks.browser_context_closed = false; }
        try { await bounded(Promise.all([...network.pending].map((operation) => operation.catch(() => undefined))), 5000, 'final_observer_timeout'); }
        catch { complete = false; failure ??= 'final_observer_timeout'; }
        if (network.violations.length || network.pageErrors) { complete = false; failure ??= 'final_network_or_page_failure'; }
        const result = { marker: 'goby-storage-binding-live-ui-browser-result-v1', version: 1, run_id: fixture.run_id, nonce: fixture.nonce,
          complete, status: complete ? 'passed' : 'failed', checks, failure, fixture_sha256: fixtureHash, browser_version: browserVersion,
          administrator_id: administratorID, library_id: library?.Id ?? null,
          root_ids: roots ? rootIDs(roots) : null, bindings, session: sessionHash ? { path: fixture.session_path, sha256: sessionHash, validated: session?.validated } : null,
          stages: protocol.stages, requests: network.exchanges, screenshots, violations: network.violations,
          page_errors: network.pageErrors, console_errors: network.consoleErrors, static_requests: network.assetRequests, static_bytes: network.assetBytes };
        const encoded = JSON.stringify(result);
        requireValue([fixture.administrator.password, fixture.administrator.setup_token, network.token, network.csrf].every((secret) => !secret || !encoded.includes(secret)), 'result_secret_guard');
        await publish(fixture.result_path, result, RESULT_LIMIT);
      } finally {
        if (session) { session.token = ''; session.csrf_token = ''; }
        network.token = network.csrf = null; fixture.administrator.password = ''; fixture.administrator.setup_token = '';
      }
    }
    requireValue(complete, 'storage_binding_live_ui_incomplete');
  });
});
