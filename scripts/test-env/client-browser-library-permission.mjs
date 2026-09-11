#!/usr/bin/env node
/** One B UI credential across controller-owned restriction and restoration. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { TextDecoder } from 'node:util';
import { createPermissionBrowserActor, safeBrowserFailure, resanitizeBrowserDiagnostics } from './client-browser-cross-user.mjs';
import { HomeViewsObserver, observeHomeDOM, waitHomeLoginProof, validateHomeIdentityBindings, validateHomeExpectedLibraries,
  checkedHomeFile, homeProcessIdentity, homeSourceDigest, homeSessionClosed } from './client-browser-library-home.mjs';
import { loadSpecialFeaturesFixture } from './client-browser-special-features-fixture.mjs';

const WORK = '/opt/goby-test/exec-work-m3e';
const ROOT = WORK + '/client-library-permission-ui-v1';
const OUTPUT = ROOT + '/browser';
const UNIT = 'goby-client-library-permission-ui-v1.service';
const SELF = fileURLToPath(import.meta.url);
const MOVIES = 'a9993591e72f0f2e7babcbf8b9c50790';
const USER = 'ecbbe4cb82403879bc4b4f78894c5738';
const SHA = /^[0-9a-f]{64}$/;
const SOURCES = ['client-browser-library-permission.mjs', 'client-browser-library-home.mjs', 'client-browser-cross-user.mjs',
  'client-browser-special-features-fixture.mjs', 'client-browser-goby-fixture.mjs', 'client-browser-session-proof.mjs'];
const AUTHORITY = Object.freeze({
  home_report: { path: WORK + '/client-library-ui-baseline-v3/report.json', sha256: 'a8117846d80eeed8e714b8951103632acfc05105d8a14c362e99f0f9d9e62270' },
  home_browser: { path: WORK + '/client-library-ui-baseline-v3/browser/report.json', sha256: 'a5e4cd3a16c625c85b459c20c2349375a22bf63d889d7ac2e89f0e9237a91e28' },
  current_snapshot: { path: WORK + '/client-library-ui-baseline-v3/after-full.json', sha256: '8095db0dd2e96c7f3a8e194b3f66d71c7366e756b12f1d1f549a19c336acb29a' },
});
const LIMITS = Object.freeze({ work_ms: 350000, control_wait_ms: 90000, abort_wait_ms: 170000, cleanup_ms: 90000,
  passive_ms: 10000, poll_ms: 250, sample_ms: 500, passive_samples: 205, publication_wait_ms: 5000, record_bytes: 512 * 1024 });
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const clone = value => JSON.parse(JSON.stringify(value));
const ordered = value => Array.isArray(value) ? value.map(ordered) : record(value)
  ? Object.fromEntries(Object.keys(value).sort().map(key => [key, ordered(value[key])])) : value;
const same = (a, b) => JSON.stringify(ordered(a)) === JSON.stringify(ordered(b));
const exact = (value, names) => record(value) && same(Object.keys(value).sort(), [...names].sort());
const sha = value => createHash('sha256').update(value).digest('hex');
function need(value, code = 'permission_guard_rejected') { if (!value) throw new Error(code); }
const descriptor = value => exact(value, ['path', 'sha256']) && typeof value.path === 'string' && value.path.startsWith(WORK + '/') &&
  /^[\x21-\x7e]+$/.test(value.path) && !value.path.includes('\\') && path.posix.normalize(value.path) === value.path && SHA.test(value.sha256);
function time(value) {
  need(typeof value === 'string' && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/.test(value));
  const result = Date.parse(value); need(Number.isFinite(result)); return result;
}
const delay = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));

export function parsePermissionArguments(argv) {
  need(Array.isArray(argv) && argv.length === 6);
  const result = {};
  for (let i = 0; i < argv.length; i += 2) {
    const key = argv[i]?.slice(2);
    need(argv[i]?.startsWith('--') && ['input', 'input-sha256', 'output'].includes(key) && !Object.hasOwn(result, key));
    result[key] = argv[i + 1];
  }
  need(result.input === ROOT + '/input.json' && result.output === OUTPUT && SHA.test(result['input-sha256'])); return result;
}

export function validatePermissionInput(input) {
  need(exact(input, ['marker', 'version', 'mode', 'root', 'output', 'actor', 'candidate', 'fixture', 'expected_libraries', 'source_closure', 'authority', 'controller']) &&
    input.marker === 'goby-client-library-permission-input-v1' && input.version === 1 && input.mode === 'b-home-permission-reload' &&
    input.root === ROOT && input.output === OUTPUT);
  validateHomeIdentityBindings(input); validateHomeExpectedLibraries(input.expected_libraries); need(input.expected_libraries.length === 4);
  need(exact(input.authority, ['home_report', 'home_browser', 'current_snapshot', 'before_snapshot']) && Object.values(input.authority).every(descriptor));
  for (const [key, source] of Object.entries(AUTHORITY)) need(same(input.authority[key], source));
  need(input.authority.before_snapshot.path === ROOT + '/before-full.json');
  need(record(input.source_closure) && Object.keys(input.source_closure).length >= 6 && Object.keys(input.source_closure).length <= 16 &&
    Object.entries(input.source_closure).every(([filename, hash]) => descriptor({ path: filename, sha256: hash })));
  for (const name of SOURCES) need(Object.keys(input.source_closure).filter(filename => path.posix.basename(filename) === name).length === 1);
  need(exact(input.controller, ['pid', 'start_ticks', 'boot_id', 'unit']) && Number.isSafeInteger(input.controller.pid) && input.controller.pid > 1 &&
    typeof input.controller.start_ticks === 'string' && /^[1-9]\d*$/.test(input.controller.start_ticks) &&
    input.controller.boot_id === input.candidate.process.boot_id && input.controller.unit === UNIT);
  return input;
}

export function permissionLibraries(input, restricted = false) {
  const values = input.expected_libraries.filter(item => !restricted || item.id !== MOVIES).map(item => ({ ...item }));
  validateHomeExpectedLibraries(values); need(values.length === (restricted ? 3 : 4)); return values;
}

export function validatePermissionBaseline(input, home, browser, before, current) {
  need(home.marker === 'goby-client-library-home-observation-v1' && home.result === 'passed' && home.phase === 'complete' &&
    home.outcome === 'baseline_observation' && home.worker_closed === true && home.fallback_attempted === false &&
    home.client_acceptance === false && home.permission_ui_acceptance === false && same(home.candidate, input.candidate) &&
    same(home.evidence?.['browser-report.json'], input.authority.home_browser) && same(home.evidence?.['after-full.json'], input.authority.current_snapshot));
  need(browser.marker === 'goby-client-library-home-report-v1' && browser.result === 'passed' && browser.outcome === 'baseline_observation' &&
    browser.client_acceptance === false && browser.permission_ui_acceptance === false && homeSessionClosed(browser) &&
    browser.initial?.dom?.passed === true && browser.initial.views?.result === 'passed' && browser.reload?.dom?.passed === true &&
    browser.reload.views?.result === 'passed' && browser.reload.action?.kind === 'page.reload' && browser.reload.action.count === 1 &&
    browser.reload.action.completed === true && browser.login_proof?.session_id === home.proof?.session_id &&
    browser.login_proof.token_sha256 === home.proof.token_sha256);
  need(same(home.proof, { new_b_authentication: 1, new_devices: 1, new_play_userdata_references_encodings: 0, new_session_audits: 2,
    observed_capabilities_bound: true, old_rows_sequences_private_unchanged: true, users_and_policies_unchanged: true,
    session_id: '83be430b189b6ef0a4f685381dc493b8', token_sha256: '51965e61bf44dfa417dd5d2f545847aed9f0aba4636e0733fcd3e545639c7feb' }));
  const a = before?.database, b = current?.database; need(record(a) && record(b));
  const metadata = value => { const copy = clone(value); time(copy.captured_at); delete copy.captured_at; return copy; };
  need(same(a.tables, b.tables) && same(a.sequences, b.sequences) && same(a.catalog, b.catalog) && same(metadata(a.metadata), metadata(b.metadata)) &&
    time(a.metadata.captured_at) >= time(b.metadata.captured_at));
  need(Object.keys(a.tables).length === 35 && a.tables.items.length === 22 && a.tables.libraries.length === 4 &&
    a.tables.sessions.length === 71 && a.tables.devices.length === 61 && a.tables.activity_entries.length === 157 &&
    a.tables.play_sessions.length === 26 && a.tables.user_item_data.length === 7 && a.tables.client_playback_references.length === 0 && a.tables.encoding_jobs.length === 0);
  need(same(a.tables.libraries.map(row => ({ id: row.id, name: row.name })).sort((x, y) => x.id.localeCompare(y.id)),
    [...input.expected_libraries].sort((x, y) => x.id.localeCompare(y.id))));
  const bUser = a.tables.users.find(row => row.id === USER);
  need(bUser && bUser.management_revision === 3 && bUser.is_disabled === false && bUser.is_administrator === false);
  const prior = a.tables.sessions.filter(row => row.id === home.proof.session_id);
  need(prior.length === 1 && prior[0].user_id === USER && prior[0].kind === 'emby' &&
    prior[0].token_hash === '\\x' + home.proof.token_sha256 && typeof prior[0].revoked_at === 'string'); time(prior[0].revoked_at);
  return { revision: String(bUser.management_revision), devices: a.tables.devices.map(row => row.reported_device_id),
    session_ids: a.tables.sessions.map(row => row.id), token_hashes: a.tables.sessions.map(row => {
      need(typeof row.token_hash === 'string' && /^\\x[0-9a-f]{64}$/.test(row.token_hash)); return row.token_hash.slice(2);
    }) };
}

/** Bind a controller record before it can authorize any browser action. */
export function validatePermissionControl(value, binding, state, now = Date.now()) {
  need(exact(value, ['marker', 'version', 'input_sha256', 'source_closure_sha256', 'controller', 'node_process', 'name',
    'previous_stage_sha256', 'revision', 'write_completed_at', 'expected_libraries', 'restoration']) &&
    value.marker === 'goby-client-library-permission-control-v1' && value.version === 1 &&
    ['restricted', 'restored', 'close'].includes(value.name) &&
    ['input_sha256', 'source_closure_sha256', 'controller', 'node_process'].every(key => same(value[key], binding[key])));
  const normal = value.name !== 'close', previous = state.stages.at(-1);
  if (normal) {
    need(!state.aborted && value.name === (state.stages.length === 1 ? 'restricted' : state.stages.length === 2 ? 'restored' : null) &&
      value.previous_stage_sha256 === previous?.sha256 && !state.consumed.includes(value.name));
    need(value.restoration === (value.name === 'restricted' ? 'pending' : 'confirmed'));
  } else {
    need(value.restoration === 'confirmed' || value.restoration === 'not_required');
    if (!state.aborted) need(state.stages.length === 3 && value.previous_stage_sha256 === previous.sha256 && value.restoration === 'confirmed');
    else {
      const known = [...state.stages, ...(state.stage_attempts ?? [])];
      need(known.some(stage => stage.sha256 === value.previous_stage_sha256) || state.stages.length === 0 && value.previous_stage_sha256 === null);
    }
  }
  const restricted = value.name === 'restricted';
  need(same([...value.expected_libraries].sort((a, b) => a.id.localeCompare(b.id)),
    permissionLibraries(state.input, restricted).sort((a, b) => a.id.localeCompare(b.id))));
  if (value.restoration === 'not_required') need(value.name === 'close' && state.aborted && !state.consumed.includes('restricted') &&
    value.revision === null && value.write_completed_at === null);
  else {
    need(typeof value.revision === 'string' && /^[1-9]\d*$/.test(value.revision) &&
      BigInt(value.revision) === BigInt(state.revision) + (restricted ? 1n : 2n));
    if (value.name === 'close' && state.aborted && value.write_completed_at === null) {
      // Fresh revision and owned audit can prove restoration after a lost ACK;
      // this cleanup-only control must not invent a successful observation window.
      need(value.restoration === 'confirmed');
    } else {
      const acknowledged = time(value.write_completed_at);
      need(acknowledged <= now + 1000 && acknowledged >= state.started_at &&
        (!normal || acknowledged >= previous.publication_started_at - 1));
    }
  }
  return clone(value);
}

/** Classify publication without parsing bytes from a file still linked to its pending name. */
export function permissionPublication(final, pending) {
  if (!final) { need(!pending || pending.isFile && !pending.symbolic && pending.uid === 0 && pending.gid === 0 &&
    pending.mode === 0o600 && pending.nlink === 1 && pending.size <= LIMITS.record_bytes); return pending ? 'publishing' : 'missing'; }
  need(final.isFile && !final.symbolic && final.uid === 0 && final.gid === 0 && final.mode === 0o600 && final.size > 0 && final.size <= LIMITS.record_bytes);
  if (final.nlink === 2) {
    need(pending && pending.isFile && !pending.symbolic && pending.uid === 0 && pending.gid === 0 && pending.mode === 0o600 &&
      pending.nlink === 2 && pending.dev === final.dev && pending.ino === final.ino && pending.size === final.size);
    return 'publishing';
  }
  need(final.nlink === 1 && pending === null); return 'ready';
}

export function permissionPublicationSnapshot(first, pending, last) {
  if (!same(first, last)) return 'publishing';
  return permissionPublication(last, pending);
}

async function directory(filename) {
  const stat = await fs.lstat(filename);
  need(stat.isDirectory() && !stat.isSymbolicLink() && stat.uid === 0 && stat.gid === 0 && (stat.mode & 0o777) === 0o700);
  return { dev: stat.dev, ino: stat.ino };
}
const statValue = value => value && ({ isFile: value.isFile(), symbolic: value.isSymbolicLink(), uid: value.uid, gid: value.gid,
  mode: value.mode & 0o777, nlink: value.nlink, size: value.size, dev: value.dev, ino: value.ino, mtimeMs: value.mtimeMs, ctimeMs: value.ctimeMs });
async function optionalStat(filename) { try { return await fs.lstat(filename); } catch (error) { if (error.code === 'ENOENT') return null; throw error; } }
async function syncDirectory(filename, io = fs) {
  const handle = await io.open(filename, constants.O_RDONLY | constants.O_DIRECTORY);
  try { await handle.sync(); } finally { await handle.close(); }
}

export function encodePermissionRecord(value) { return Buffer.from(JSON.stringify(value, null, 2) + '\n'); }

/** Fixed private record publication. Existing final or pending names are never overwritten. */
export async function publishPermissionRecord(filename, value, io = fs) {
  need(path.posix.dirname(filename) === OUTPUT && /^(?:stage-(?:baseline|restricted|restored)|abort|report|session-private|capabilities-private)\.json$/.test(path.posix.basename(filename)));
  const bytes = encodePermissionRecord(value);
  const maximum = filename.endsWith('/report.json') ? 4 * 1024 * 1024 : filename.endsWith('/capabilities-private.json') ? 2 * 1024 * 1024 : LIMITS.record_bytes;
  need(bytes.length > 0 && bytes.length <= maximum);
  const pending = filename + '.pending';
  const handle = await io.open(pending, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  let identity;
  try { await handle.writeFile(bytes); await handle.sync(); identity = await handle.stat(); } finally { await handle.close(); }
  await io.link(pending, filename); await syncDirectory(OUTPUT, io);
  const [temporary, final] = await Promise.all([io.lstat(pending), io.lstat(filename)]);
  need([temporary, final].every(stat => stat.dev === identity.dev && stat.ino === identity.ino && stat.nlink === 2 && stat.isFile() && !stat.isSymbolicLink()));
  await io.unlink(pending); await syncDirectory(OUTPUT, io);
  const published = await io.lstat(filename);
  need(published.dev === identity.dev && published.ino === identity.ino && published.nlink === 1 && published.isFile() && !published.isSymbolicLink());
  const result = { path: filename, sha256: sha(bytes) }; bytes.fill(0); return result;
}

async function readControlRecord(name) {
  need(['restricted', 'restored', 'close'].includes(name)); await directory(ROOT);
  const filename = ROOT + '/control-' + name + '.json', pending = filename + '.pending';
  const first = await optionalStat(filename), temporary = await optionalStat(pending), last = await optionalStat(filename);
  const publication = permissionPublicationSnapshot(statValue(first), statValue(temporary), statValue(last));
  if (publication !== 'ready') return { publication };
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  let bytes;
  try {
    need(same(statValue(await handle.stat()), statValue(first))); bytes = await handle.readFile();
    need(bytes.length === first.size && same(statValue(await handle.stat()), statValue(first)) && same(statValue(await fs.lstat(filename)), statValue(first)));
    return { publication: 'ready', value: JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)), path: filename, sha256: sha(bytes) };
  } finally { bytes?.fill(0); await handle.close(); }
}

export function permissionStageRecord(name, observation, binding, session, previousControl) {
  need(['baseline', 'restricted', 'restored'].includes(name) && descriptor(session) &&
    (name === 'baseline' ? previousControl === null : SHA.test(previousControl)) && SHA.test(binding.token_sha256));
  need(exact(observation, name === 'baseline' ? ['dom', 'views'] : ['spontaneous', 'reload']));
  return { marker: 'goby-client-library-permission-stage-v1', version: 1,
    input_sha256: binding.input_sha256, source_closure_sha256: binding.source_closure_sha256,
    controller: clone(binding.controller), node_process: clone(binding.node_process), name,
    token_sha256: binding.token_sha256, session_private: clone(session), previous_control_sha256: previousControl,
    observation: clone(observation) };
}

export function spontaneousPermissionEvidence(stage, control, samples, observer, actorStarted, completedAt) {
  const acknowledged = time(control.write_completed_at), from = acknowledged - actorStarted, until = from + LIMITS.passive_ms;
  need(from >= 0 && completedAt >= until && Array.isArray(samples) && samples.length <= LIMITS.passive_samples);
  const selected = samples.filter(sample => sample.started_elapsed_ms >= from && sample.elapsed_ms <= until);
  const views = observer.viewEvidence(stage, { from_ms: from, until_ms: until, completed_by_ms: until });
  const outcome = views.result === 'not_observed' ? 'not_observed_within_window'
    : views.result === 'passed' && selected.at(-1)?.observation?.passed === true ? 'observed_correct_membership'
      : 'observed_incomplete_or_stale_membership';
  return { window: { write_completed_at: control.write_completed_at, start_elapsed_ms: from, end_elapsed_ms: until,
    duration_ms: LIMITS.passive_ms, completed_elapsed_ms: completedAt, completed: true, ui_actions: 0 },
    earlier: { dom: clone(samples.filter(sample => sample.started_elapsed_ms < from)), views: observer.viewEvidence(stage, { until_ms: from }) },
    dom: clone(selected), views, outcome };
}

/** Fixed three-stage choreography. The injected transport consists only of private records. */
export class PermissionWorkflow {
  constructor({ input, binding, revision, actor, observer, report, publish = publishPermissionRecord, read = readControlRecord,
    now = () => Date.now(), wait = delay, dom = observeHomeDOM }) {
    Object.assign(this, { input, binding, actor, observer, report, publish, read, now, wait, dom });
    this.state = { input, revision, stages: [], stage_attempts: [], consumed: [], aborted: false, started_at: actor.started };
    this.started = actor.started; this.deadline = this.started + LIMITS.work_ms;
    this.stage = 'baseline'; this.controlClose = null; this.publishing = new Map(); this.samples = new Map(); this.disposed = false;
    report.stages = []; report.stage_publication_attempts = []; report.controls = []; report.abort = null; report.restoration = 'pending';
  }
  active() { need(!this.disposed && !this.state.aborted && this.now() < this.deadline, 'permission_work_deadline'); }
  async checkedControl(name) {
    const source = await this.read(name);
    if (source.publication === 'publishing') {
      if (!this.publishing.has(name)) this.publishing.set(name, this.now());
      need(this.now() - this.publishing.get(name) <= LIMITS.publication_wait_ms, 'permission_publication_timeout');
    } else this.publishing.delete(name);
    return source;
  }
  async control(name, { failing = false, sample = null } = {}) {
    const deadline = this.now() + (failing ? LIMITS.abort_wait_ms : LIMITS.control_wait_ms);
    while (!this.disposed && this.now() < deadline) {
      if (!failing) this.active();
      const close = this.controlClose ?? await this.checkedControl('close');
      if (close.publication === 'ready') {
        const state = name === 'close' ? this.state : { ...this.state, aborted: true };
        validatePermissionControl(close.value, this.binding, state, this.now());
        this.controlClose = close;
        if (name !== 'close') throw new Error('permission_controller_closed_early');
        return close;
      }
      if (name !== 'close') {
        const next = await this.checkedControl(name);
        if (next.publication === 'ready') {
          validatePermissionControl(next.value, this.binding, this.state, this.now());
          await this.actor.assertPinned(); this.state.consumed.push(name);
          this.report.controls.push({ name, path: next.path, sha256: next.sha256, value: clone(next.value) }); return next;
        }
      }
      if (sample) await sample();
      await this.wait(Math.min(LIMITS.poll_ms, Math.max(1, deadline - this.now())));
    }
    throw new Error(failing ? 'permission_restore_confirmation_timeout' : 'permission_control_timeout');
  }
  async capture(expected, excluded) {
    this.active(); await this.noticeEarlyClose(); await this.actor.settled();
    need(this.actor.report.page_error_count === 0, 'permission_page_error');
    return this.dom(this.actor.page, expected, { require_ids: true, excluded });
  }
  async noticeEarlyClose() {
    const source = this.controlClose ?? await this.checkedControl('close');
    if (source.publication !== 'ready') return;
    validatePermissionControl(source.value, this.binding, { ...this.state, aborted: true }, this.now());
    this.controlClose = source; throw new Error('permission_controller_closed_early');
  }
  async observe(stage, expected, excluded, window = {}) {
    const until = Math.min(this.deadline, this.now() + 20000);
    let dom, views;
    do {
      dom = await this.capture(expected, excluded); views = this.observer.viewEvidence(stage, window);
      if (dom.passed && views.result === 'passed') break;
      await this.wait(100);
    } while (this.now() < until);
    need(dom?.passed && views?.result === 'passed', views?.result === 'not_observed' ? 'permission_views_not_observed' : 'permission_membership_incomplete');
    await this.actor.proxyIdle(); await this.actor.settled(); await this.actor.assertPinned();
    views = this.observer.viewEvidence(stage, window); need(views.result === 'passed', 'permission_transfer_incomplete');
    return { dom, views };
  }
  arm(name, expected) { this.observer.armStage(name, expected); this.samples.set(name, []); }
  async sample(name, expected, excluded) {
    const samples = this.samples.get(name); need(samples);
    if (samples.length && this.now() - this.started - samples.at(-1).elapsed_ms < LIMITS.sample_ms) return;
    need(samples.length < LIMITS.passive_samples, 'permission_sample_limit');
    const started = this.now() - this.started, observation = await this.capture(expected, excluded);
    samples.push({ started_elapsed_ms: started, elapsed_ms: this.now() - this.started, observation });
  }
  async publishStage(name, observation, previousControl) {
    this.active(); need(this.observer.bound && this.report.session_private);
    need(name === ['baseline', 'restricted', 'restored'][this.state.stages.length]);
    const stage = permissionStageRecord(name, observation, { ...this.binding, token_sha256: this.observer.bound.token_sha256 },
      this.report.session_private, previousControl);
    // The controller may consume a durable final name before the publisher's
    // final directory sync returns. Only this conservative start is a lower bound.
    const publicationStarted = this.now();
    const encoded = encodePermissionRecord(stage), attemptSHA = sha(encoded); encoded.fill(0);
    const attempt = { name, path: OUTPUT + '/stage-' + name + '.json', sha256: attemptSHA,
      publication_started_at: publicationStarted };
    this.state.stage_attempts.push(attempt);
    const diagnostic = { name, path: attempt.path, sha256: attempt.sha256, publication_started_elapsed_ms: publicationStarted - this.started, completed: false };
    this.report.stage_publication_attempts.push(diagnostic);
    const source = await this.publish(OUTPUT + '/stage-' + name + '.json', stage);
    need(source.path === attempt.path && source.sha256 === attempt.sha256, 'permission_stage_publication_mismatch');
    diagnostic.completed = true;
    this.state.stages.push({ name, ...source, publication_started_at: publicationStarted });
    this.report.stages.push({ name, ...source }); this.report[name] = clone(observation);
  }
  async changed(name, control, expected, excluded) {
    const passiveStage = name + '_passive', reloadStage = name + '_reload';
    const end = time(control.value.write_completed_at) + LIMITS.passive_ms;
    while (this.now() < end) {
      this.active(); await this.sample(passiveStage, expected, excluded);
      await this.wait(Math.min(LIMITS.poll_ms, Math.max(1, end - this.now())));
    }
    const spontaneous = spontaneousPermissionEvidence(passiveStage, control.value, this.samples.get(passiveStage), this.observer,
      this.started, this.now() - this.started);
    await this.actor.proxyIdle(); this.active(); await this.noticeEarlyClose(); await this.actor.assertPinned();
    this.observer.armStage(reloadStage, expected);
    const action = { kind: 'page.reload', count: 1, control_sha256: control.sha256, completed: false, status: null,
      invoked_elapsed_ms: this.now() - this.started };
    const response = await this.actor.page.reload({ waitUntil: 'domcontentloaded', timeout: 30000 });
    action.completed = true; action.status = response?.status() ?? null; action.completed_elapsed_ms = this.now() - this.started;
    need(this.actor.report.proxy.login === 1 && this.actor.token && sha(this.actor.token) === this.observer.bound.token_sha256, 'permission_token_changed');
    const observed = await this.observe(reloadStage, expected, excluded, { from_ms: action.invoked_elapsed_ms });
    return { spontaneous, reload: { action, ...observed } };
  }
  async execute() {
    const full = permissionLibraries(this.input), limited = permissionLibraries(this.input, true), excluded = full.filter(item => item.id === MOVIES);
    await this.actor.open(); await waitHomeLoginProof(this.observer, { settle: () => this.actor.settled() });
    need(this.report.session_private, 'permission_login_receipt_missing');
    this.actor.phase = 'ui_home';
    const baseline = await this.observe('initial', full, []);
    this.arm('restricted_passive', limited); await this.sample('restricted_passive', limited, excluded);
    await this.publishStage('baseline', baseline, null);
    this.stage = 'restricted';
    const restrictedControl = await this.control('restricted', { sample: () => this.sample('restricted_passive', limited, excluded) });
    const restricted = await this.changed('restricted', restrictedControl, limited, excluded);
    this.arm('restored_passive', full); await this.sample('restored_passive', full, []);
    await this.publishStage('restricted', restricted, restrictedControl.sha256);
    this.stage = 'restored';
    const restoredControl = await this.control('restored', { sample: () => this.sample('restored_passive', full, []) });
    const restored = await this.changed('restored', restoredControl, full, []);
    await this.publishStage('restored', restored, restoredControl.sha256);
    this.stage = 'close'; const close = await this.control('close');
    this.report.control_close = { path: close.path, sha256: close.sha256, value: clone(close.value) };
    this.report.restoration = close.value.restoration;
  }
  async abort(failure) {
    this.state.aborted = true; this.report.failure = failure; this.report.restoration = 'unconfirmed';
    try {
      const value = { marker: 'goby-client-library-permission-abort-v1', version: 1, ...clone(this.binding), stage: this.stage, failure };
      this.report.abort = await this.publish(OUTPUT + '/abort.json', value);
    } catch { this.report.abort_journal_failed = true; }
    try {
      const close = await this.control('close', { failing: true });
      this.report.control_close = { path: close.path, sha256: close.sha256, value: clone(close.value) };
      this.report.restoration = close.value.restoration;
    } catch { this.report.restoration_wait_failed = true; }
  }
}

export function permissionObservationPassed(report) {
  const stages = report.stages ?? [], controls = report.controls ?? [];
  return Boolean(!report.failure && !report.abort && !report.abort_journal_failed && report.restoration === 'confirmed' &&
    stages.length === 3 && same(stages.map(stage => stage.name), ['baseline', 'restricted', 'restored']) &&
    controls.length === 2 && same(controls.map(control => control.name), ['restricted', 'restored']) &&
    report.control_close?.value?.restoration === 'confirmed' && report.control_close.value.previous_stage_sha256 === stages[2].sha256 &&
    report.baseline?.dom?.passed && report.baseline.views?.result === 'passed' &&
    ['restricted', 'restored'].every(name => report[name]?.reload?.dom?.passed && report[name].reload.views?.result === 'passed' &&
      report[name].reload.action?.kind === 'page.reload' && report[name].reload.action.count === 1 && report[name].reload.action.completed === true &&
      report[name].reload.action.control_sha256 === controls.find(control => control.name === name).sha256 &&
      report[name].reload.action.invoked_elapsed_ms >= report[name].spontaneous.window.end_elapsed_ms &&
      report[name].reload.views.stage === name + '_reload' &&
      report[name].spontaneous?.window?.completed === true && report[name].spontaneous.window.duration_ms === LIMITS.passive_ms &&
      report[name].spontaneous.window.ui_actions === 0) &&
    report.restricted.reload.dom.excluded_libraries?.length === 1 && report.restricted.reload.dom.excluded_libraries[0].id === MOVIES &&
    report.restricted.reload.dom.excluded_libraries[0].absent === true &&
    report.final_stage_checks?.every(value => value.passed === true) && report.final_stage_checks.length === 3 &&
    report.actor?.websocket_handshake_budget === 3 && report.closure?.websocket_opened <= 3 && homeSessionClosed(report));
}

/** The only network actions are genuine B UI actions through the existing fixed-target guard. */
export async function runLibraryPermission(options) {
  process.umask(0o077);
  const parsed = parsePermissionArguments(Object.entries(options).flatMap(([key, value]) => ['--' + key, value]));
  const input = validatePermissionInput(await checkedHomeFile(parsed.input, parsed['input-sha256']));
  const rootIdentity = await directory(ROOT), node = await homeProcessIdentity(input, UNIT);
  need(Object.hasOwn(input.source_closure, SELF));
  for (const name of SOURCES) need(Object.hasOwn(input.source_closure, path.posix.join(path.posix.dirname(SELF), name)));
  for (const [filename, hash] of Object.entries(input.source_closure)) await checkedHomeFile(filename, hash, 2 * 1024 * 1024, false, false);
  const home = await checkedHomeFile(input.authority.home_report.path, input.authority.home_report.sha256);
  const browser = await checkedHomeFile(input.authority.home_browser.path, input.authority.home_browser.sha256);
  const current = await checkedHomeFile(input.authority.current_snapshot.path, input.authority.current_snapshot.sha256, 64 * 1024 * 1024);
  const before = await checkedHomeFile(input.authority.before_snapshot.path, input.authority.before_snapshot.sha256, 64 * 1024 * 1024);
  const baseline = validatePermissionBaseline(input, home, browser, before, current);
  need(Date.now() >= time(before.database.metadata.captured_at) && Date.now() - time(before.database.metadata.captured_at) <= 180000);
  const credentials = await checkedHomeFile(input.actor.credentials.path, input.actor.credentials.sha256);
  need(credentials.marker === 'goby-m3e-client-acceptance-v1' && credentials.base_url === input.candidate.base_url &&
    credentials.direct_url === input.candidate.direct_url && credentials.viewer?.username === 'm3e-client-viewer' && /^[0-9a-f]{48}$/.test(credentials.viewer.password));
  const account = { slot: 'B', id: USER, username: credentials.viewer.username, password: credentials.viewer.password,
    credentialsPath: input.actor.credentials.path, credentialsSHA: input.actor.credentials.sha256 };
  const fixture = await loadSpecialFeaturesFixture({ candidateSHA256: input.candidate.binary_sha256, source: input.candidate.source,
    sourceManifestSHA256: input.candidate.source_manifest_sha256, receiptSHA256: input.fixture.profile_receipt.sha256,
    reportSHA256: input.fixture.profile_report.sha256, inspectSHA256: input.fixture.profile_inspection.sha256,
    musicChainPath: input.fixture.music_chain.path, musicChainSHA256: input.fixture.music_chain.sha256,
    musicScanReceiptSHA256: input.fixture.music_scan_receipt.sha256 });
  need(fixture.serverId === input.candidate.server_id && same(fixture.evidence.process, input.candidate.process) &&
    fixture.evidence.fixture_state_sha256 === input.candidate.state_sha256);
  await fs.mkdir(OUTPUT, { mode: 0o700 }); const outputIdentity = await directory(OUTPUT);
  const sourceDigest = homeSourceDigest(input.source_closure);
  const binding = { input_sha256: parsed['input-sha256'], source_closure_sha256: sourceDigest, controller: clone(input.controller), node_process: node };
  const report = { marker: 'goby-client-library-permission-report-v1', version: 1, mode: input.mode, result: 'failed', outcome: 'failed',
    client_acceptance: false, permission_ui_acceptance: false, full_m3_complete: false, ...clone(binding), candidate: clone(input.candidate),
    authority: clone(input.authority), limits: { ...LIMITS, websocket_handshakes: 3, worker_ms: 660000 },
    boundary: 'One B UI login, baseline four cards, two controller-authorized changes, independent ten-second passive windows and one explicit reload after each; no automatic-update guarantee',
    scope: { administrator_credentials: false, browser_policy_writes: 0, media_playback: false, playback_info: false,
      shared_core_difference: 'Only the fixed permission factory admits three handshakes; old Home and cross-user remain at two' },
    login_proof: null, session_private: null, capabilities_private: null, actor: {}, baseline: null, restricted: null, restored: null,
    closure: null, failure: null };
  const secrets = [account.password]; let actor, flow;
  const observer = new HomeViewsObserver({ account, expected: permissionLibraries(input), existingDevices: baseline.devices,
    existingSessions: baseline.session_ids, existingTokens: baseline.token_hashes, scope: 'permission',
    serverId: fixture.serverId, started: Date.now(),
    onProven: ({ token, proof }) => {
      need(actor.token === null || actor.token === token); actor.token = token; actor.proven = true;
      actor.report.token_fingerprint = proof.token_sha256; actor.report.ordinary_authority_confirmed = true;
      report.login_proof = clone(proof); if (!secrets.includes(token)) secrets.push(token);
    },
    persistLogin: async ({ token, proof }) => {
      report.session_private = await publishPermissionRecord(OUTPUT + '/session-private.json', {
        marker: 'goby-client-library-home-session-v1', version: 1, ...clone(binding), proof, token });
      return report.session_private;
    } });
  const pin = async () => {
    need(same(await directory(ROOT), rootIdentity) && same(await directory(OUTPUT), outputIdentity));
    await checkedHomeFile(parsed.input, parsed['input-sha256'], 2 * 1024 * 1024, false);
    for (const source of [...(report.stages ?? []), ...(report.controls ?? []), report.session_private, report.control_close].filter(Boolean)) {
      await checkedHomeFile(source.path, source.sha256, 512 * 1024, false);
    }
    need(same(await homeProcessIdentity(input, UNIT), node)); await fixture.assertPinned();
  };
  actor = createPermissionBrowserActor({ account, pin, report: report.actor, observer: observer.hooks() });
  actor.diagnosticSecrets = () => secrets; actor.rememberSecret = value => { if (!secrets.includes(value)) secrets.push(value); };
  actor.authorizeSocket = async () => { await waitHomeLoginProof(observer); need(observer.bound.token_sha256 === sha(actor.token) && report.session_private); };
  report.started_at = new Date(actor.started).toISOString();
  report.elapsed_clock = 'All frame, physical transfer, DOM, control and UI action offsets use the actor start time';
  flow = new PermissionWorkflow({ input, binding, revision: baseline.revision, actor, observer, report });
  try { await flow.execute(); }
  catch (error) {
    const allowed = ['permission_controller_closed_early', 'permission_work_deadline', 'permission_control_timeout', 'permission_views_not_observed',
      'permission_membership_incomplete', 'permission_transfer_incomplete', 'permission_page_error', 'permission_token_changed',
      'permission_login_receipt_missing', 'permission_publication_timeout', 'permission_sample_limit'];
    const failure = allowed.includes(error?.message) ? error.message : 'permission_flow_failed';
    report.failure_diagnostic = safeBrowserFailure(error, actor.phase); await flow.abort(failure);
  } finally {
    observer.phase = 'cleanup'; flow.disposed = true;
    report.closed_after_restoration_confirmation = ['confirmed', 'not_required'].includes(report.restoration);
    try { await actor.close(); } catch { report.failure ??= 'permission_browser_close_failed'; }
    report.closure = actor.report.home_closure ?? null; report.observation = observer.safeEvidence();
    report.final_stage_checks = [['baseline', 'initial'], ['restricted', 'restricted_reload'], ['restored', 'restored_reload']]
      .map(([name, stage]) => ({ name, passed: report[name] !== null && observer.viewEvidence(stage,
        name === 'baseline' ? {} : { from_ms: report[name].reload.action.invoked_elapsed_ms }).result === 'passed' }));
    if (observer.bound) {
      try {
        const entries = observer.privateCapabilities();
        const source = await publishPermissionRecord(OUTPUT + '/capabilities-private.json', { marker: 'goby-client-library-home-capabilities-v1', version: 1,
          input_sha256: binding.input_sha256, source_closure_sha256: sourceDigest, node_process: node,
          session_id: observer.bound.session_id, token_sha256: observer.bound.token_sha256, entries });
        const successes = entries.filter(entry => entry.completed && entry.status === 204);
        report.capabilities_private = { ...source, request_count: entries.length, last_successful_body_sha256: successes.at(-1)?.body_sha256 ?? null };
        need(entries.length === actor.report.proxy.capabilities && entries.every(entry => entry.completed && entry.status === 204));
      } catch { report.failure ??= 'permission_capabilities_evidence_failed'; }
    }
    resanitizeBrowserDiagnostics({ accounts: [actor.report] }, secrets); report.completed_at = new Date().toISOString();
    const passed = permissionObservationPassed(report);
    report.result = passed ? 'passed' : 'failed'; report.permission_ui_acceptance = passed;
    report.outcome = passed ? 'permission_observation_after_explicit_reload' : 'failed';
    if (!passed) report.failure ??= 'permission_evidence_incomplete';
    const encoded = JSON.stringify(report); need(secrets.every(secret => !secret || !encoded.includes(secret)), 'permission_report_secret_guard');
    await publishPermissionRecord(OUTPUT + '/report.json', report);
    observer.dispose(); secrets.fill(null); credentials.viewer.password = null;
  }
  return report;
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  try {
    const report = await runLibraryPermission(parsePermissionArguments(process.argv.slice(2)));
    process.stdout.write(JSON.stringify({ marker: report.marker, result: report.result, outcome: report.outcome,
      permission_ui_acceptance: report.permission_ui_acceptance, client_acceptance: false, failure: report.failure }) + '\n');
    if (report.result !== 'passed') process.exitCode = 1;
  } catch {
    process.stdout.write(JSON.stringify({ marker: 'goby-client-library-permission-report-v1', result: 'failed',
      permission_ui_acceptance: false, client_acceptance: false, failure: 'permission_setup_or_terminal_report' }) + '\n');
    process.exitCode = 1;
  }
}
