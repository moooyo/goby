/** Read-only source44 authority and ownership pins for one candidate-only UI scope. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { TextDecoder } from 'node:util';
import { fileURLToPath } from 'node:url';

const WORK = '/opt/goby-test/exec-work-m3e';
const ROOT = WORK + '/client-library-changed-ui-source44-v1';
const TOOL = WORK + '/client-library-changed-source44-tool-02';
const STATE = WORK + '/client-fixture.json';
const SELF = fileURLToPath(import.meta.url);
const ORIGIN = 'http://127.0.0.1:18196', DIRECT = 'http://127.0.0.1:18198';
const USER = 'ecbbe4cb82403879bc4b4f78894c5738';
const LIBRARY = 'a9993591e72f0f2e7babcbf8b9c50790', ITEM = '268051d3ca734aefcf94e245fb25ad55';
const SERVER = 'c7cfd76b1dee728b2bad523793a37ccb';
const SOURCE_CREDENTIALS_SHA = '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790';
const BOOT = '6bdfc486-7bc8-412f-82b5-70095a09dde7';
const PUBLICATION = '35ae3d000f812fa18d234921cedb3c33d88190e0';
const CONTINUATION = WORK + '/client-notifications-source44-continuation-v1';
const CONTINUATION_MARKER = 'goby-client-notifications-source44-continuation-v1';
const CONTINUATION_UNIT = 'goby-client-notifications-source44-continuation-controller-v1.service';
const CONTINUATION_INVOCATION = 'c3c9115bd8994d6584a5aac583d71884';
const CONTROLLER_UNIT = 'goby-client-library-changed-ui-source44-controller-v1.service';
const CANDIDATE = Object.freeze({
  binary_sha256: 'cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d',
  process: Object.freeze({ pid: 1264063, start_ticks: 11104222, boot_id: BOOT }),
  runtime_sha256: 'd8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df',
  state_sha256: 'd6b8872eab70848b834c50e26cc2b84e9d137dd68049a1be85d09afa811829d4',
  server_id: SERVER, base_url: ORIGIN, direct_url: DIRECT,
  source: WORK + '/source-attempt-44',
  source_manifest_sha256: 'c2c9492589360b058c6533d0719219cf85ab5bc056bd76290cee51e7dc897f8b',
});
const CANDIDATE_PROCESS = Object.freeze({ ...CANDIDATE.process, executable: '/opt/goby-client-m3e/goby',
  sha256: CANDIDATE.binary_sha256, uid: 995, cgroup: '/system.slice/goby-client-m3e.service',
  invocation: 'c0a5244ae25646c8bd92c3e3c1636575' });
const PRIMARY = Object.freeze({ pid: 762090, start_ticks: 7637121, boot_id: BOOT,
  executable: '/opt/goby-dev/goby', sha256: 'af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620',
  uid: 995, cgroup: '/system.slice/goby-foundation-test.service', invocation: 'bb94d74b475f4382a6ec6f6df181dd74' });
const PROXY_PROCESS = Object.freeze({ pid: 334022, start_ticks: 378464, boot_id: BOOT });
const PINS = Object.freeze({
  report: { path: CONTINUATION + '/report.json', sha256: '4b426981fa6cc188ca0401c8b28005aeb871246390f1594ef66cd39ab550798e' },
  completed: { path: CONTINUATION + '/completed.json', sha256: '5ade36722b8e2af81f71ae80006039c230b574ca54b8cf3c144a3ff2216fc596' },
  terminal: { path: WORK + '/client-notifications-source44-continuation-execution-01/terminal.json',
    sha256: '08d91a6611b5d2f1cac425528c60054235770fde57a4950414416dcc92a362b0' },
  current: { path: CONTINUATION + '/after-full.json', sha256: '738eb9717506bec997456830e70d56f0867f9cf8159939ea3733e039cadb9811' },
  proxy: { path: WORK + '/dual-proxy-status-02.json', sha256: '769684e6c84eb4ea83c01f86c671d94d488626581c8dde664fa6f7ab7c05310f' },
});
const FIXTURES = Object.freeze({
  profile_receipt: { path: WORK + '/client-special-features-protocol-finalization-v1/completed.json',
    sha256: 'b443e5f6d5faceb3486d68644298b0a1527f6dcc1e4ac1109ff6d0c252fdfb36' },
  profile_report: { path: WORK + '/client-special-features-protocol-finalization-v1/report.json',
    sha256: '929e6b6fc0014e6b3afc7ce023ea981b749ce884ed6eb33c34b3617ac116855d' },
  profile_inspection: { path: WORK + '/client-special-features-finalization-inspection-v1/report.json',
    sha256: 'fdc8a38f5937964d3dba8e0e47f897108475011e70f4802e30c1803068dc4b5e' },
  music_chain: { path: WORK + '/client-music-upgrade-chain-source32.json',
    sha256: 'c235d59a317fe1b95d8d78a51ab32ca556f3ca7872e96bfea51e710e26b0c67a' },
  music_scan_receipt: { path: WORK + '/client-music-scan-v1/receipt.json',
    sha256: 'e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b' },
});
const SOURCES = Object.freeze(['client-browser-library-changed.mjs', 'client-library-changed-source44-fixture.mjs',
  'client-browser-library-home.mjs', 'client-browser-cross-user.mjs', 'client-browser-session-proof.mjs',
  'client-browser-special-features-fixture.mjs', 'client-browser-goby-fixture.mjs']);
const LIBRARIES = Object.freeze([LIBRARY, '6383d20008836e137559698c29b10395', 'a34ce665fb75421ef7551570f353d705',
  '57a85c1ca5b6c7ae602c587755250b2f']);
const JSON_LIMIT = 2 * 1024 * 1024, SNAPSHOT_LIMIT = 64 * 1024 * 1024;
const SHA = /^[0-9a-f]{64}$/, ID = /^[0-9a-f]{32}$/;
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const digest = value => typeof value === 'string' && SHA.test(value);
const canonical = value => typeof value === 'bigint' ? value.toString() + 'n' : Array.isArray(value)
  ? '[' + value.map(canonical).join(',') + ']' : record(value)
    ? '{' + Object.keys(value).sort().map(key => JSON.stringify(key) + ':' + canonical(value[key])).join(',') + '}' : JSON.stringify(value);
const same = (left, right) => canonical(left) === canonical(right);
const exact = (value, keys) => record(value) && same(Object.keys(value).sort(), [...keys].sort());
const hash = value => createHash('sha256').update(value).digest('hex');
function need(value) { if (!value) throw new Error('library_changed_source44_binding_rejected'); }
const text = (value, maximum = 256) => typeof value === 'string' && value.length > 0 && value.length <= maximum && !/[\x00-\x1f\x7f]/.test(value);
const ownedPath = value => typeof value === 'string' && value.startsWith(WORK + '/') &&
  /^[\x21-\x7e]+$/.test(value) && !value.includes('\\') && path.posix.normalize(value) === value &&
  !value.split('/').some(part => part === '.' || part === '..');
const descriptor = value => exact(value, ['path', 'sha256']) && ownedPath(value.path) && digest(value.sha256);
function freeze(value) {
  if (value && typeof value === 'object') { for (const child of Object.values(value)) freeze(child); Object.freeze(value); }
  return value;
}

export function validateLibraryChangedSource44Input(input) {
  need(exact(input, ['marker', 'version', 'mode', 'root', 'output', 'actor', 'candidate', 'fixture',
    'expected_libraries', 'target', 'source_closure', 'authority', 'controller']) &&
    input.marker === 'goby-client-library-changed-input-v1' && input.version === 1 &&
    input.mode === 'b-movies-name-automatic-refresh' && input.root === ROOT && input.output === ROOT + '/browser');
  need(same(input.candidate, CANDIDATE) && same(input.fixture, FIXTURES));
  need(exact(input.actor, ['slot', 'user_id', 'credentials', 'account_key', 'source_credentials_sha256']) &&
    input.actor.slot === 'B' && input.actor.user_id === USER && input.actor.account_key === 'viewer' &&
    input.actor.source_credentials_sha256 === SOURCE_CREDENTIALS_SHA && descriptor(input.actor.credentials) &&
    input.actor.credentials.path === ROOT + '/viewer-credentials.json');
  need(exact(input.authority, ['continuation_report', 'continuation_terminal', 'current_snapshot', 'before_snapshot']) &&
    same(input.authority.continuation_report, PINS.report) && same(input.authority.continuation_terminal, PINS.terminal) &&
    same(input.authority.current_snapshot, PINS.current) && descriptor(input.authority.before_snapshot) &&
    input.authority.before_snapshot.path === ROOT + '/before-full.json');
  need(exact(input.source_closure, SOURCES.map(name => TOOL + '/' + name)) && Object.values(input.source_closure).every(digest));
  need(Array.isArray(input.expected_libraries) && input.expected_libraries.length === 4 &&
    input.expected_libraries.every(value => exact(value, ['id', 'name']) && ID.test(value.id) && text(value.name)) &&
    same(input.expected_libraries.map(value => value.id).sort(), [...LIBRARIES].sort()) &&
    input.expected_libraries.find(value => value.id === LIBRARY)?.name === 'M3e Client Movies');
  const target = input.target;
  need(exact(target, ['id', 'library_id', 'name', 'type', 'parent_id', 'root_id', 'relative_path']) &&
    target.id === ITEM && target.library_id === LIBRARY && target.type === 'Movie' && text(target.name) &&
    ID.test(target.parent_id) && ID.test(target.root_id) && text(target.relative_path, 4096) &&
    !target.relative_path.startsWith('/') && !target.relative_path.includes('\\') &&
    !target.relative_path.split('/').some(part => part === '' || part === '.' || part === '..'));
  const controller = input.controller;
  need(exact(controller, ['pid', 'start_ticks', 'boot_id', 'unit']) && Number.isSafeInteger(controller.pid) && controller.pid > 1 &&
    ![CANDIDATE.process.pid, PRIMARY.pid, PROXY_PROCESS.pid].includes(controller.pid) &&
    typeof controller.start_ticks === 'string' && /^[1-9]\d*$/.test(controller.start_ticks) &&
    Number.isSafeInteger(Number(controller.start_ticks)) && controller.boot_id === BOOT && controller.unit === CONTROLLER_UNIT);
  return true;
}

export function validateLibraryChangedSource44Viewer(value) {
  need(exact(value, ['marker', 'slot', 'user_id', 'base_url', 'direct_url', 'viewer']) &&
    value.marker === 'goby-client-library-changed-viewer-v1' && value.slot === 'B' && value.user_id === USER &&
    value.base_url === ORIGIN && value.direct_url === DIRECT && exact(value.viewer, ['username', 'password']) &&
    value.viewer.username === 'm3e-client-viewer' && typeof value.viewer.password === 'string' && /^[0-9a-f]{48}$/.test(value.viewer.password));
  return true;
}

function sourceBinding(value) {
  need(exact(value, ['marker', 'schema', 'source', 'source_manifest_sha256', 'catalog_sha256', 'migration_27_sha256']) &&
    value.marker === 'goby-client-schema27-source-m3e-v1' && value.schema === 27 && value.source === CANDIDATE.source &&
    value.source_manifest_sha256 === CANDIDATE.source_manifest_sha256 &&
    value.catalog_sha256 === '1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d' &&
    value.migration_27_sha256 === 'b62d0422dddb9e258f46589898f672b08fc6e4c12fea7456059f855a6600353c');
}

export function validateLibraryChangedSource44Documents(input, documents) {
  validateLibraryChangedSource44Input(input);
  const { state, report, completed, terminal, current, before, profileReceipt, profileReport, profileInspection,
    musicChain, musicScanReceipt, proxy } = documents;
  need(record(state) && state.marker === 'goby-m3e-client-acceptance-v1' && state.work === WORK && state.schema === 27 &&
    state.phase === 'ready' && state.stage === 'complete' && state.server_id === SERVER && state.viewer_id === USER &&
    state.binary_sha256 === CANDIDATE.binary_sha256 && state.runtime_sha256 === CANDIDATE.runtime_sha256 &&
    state.browser_sha256 === SOURCE_CREDENTIALS_SHA && same(state.process, CANDIDATE.process) && record(state.work_identity));
  sourceBinding(state.schema27_source);
  need(report?.marker === CONTINUATION_MARKER && report.status === 'passed' && report.schema === 27 &&
    report.state_sha256 === CANDIDATE.state_sha256 && report.source_manifest_sha256 === CANDIDATE.source_manifest_sha256 &&
    report.binary?.sha256 === CANDIDATE.binary_sha256 && report.publication === PUBLICATION &&
    same(report.new_process, CANDIDATE.process) && report.http_requests === 2 && same(report.service_actions, ['stop', 'start']) &&
    report.complete_rows_sequences_catalog_preserved === true && report.credentials_recovery_and_three_media_groups_preserved === true &&
    report.primary_identity_unchanged === true && report.original_failure_trees_preserved === true &&
    ['web_assets_replaced', 'schema_migration_performed', 'client_acceptance', 'automatic_retry', 'automatic_rollback'].every(key => report[key] === false) &&
    same(report.evidence?.['completed.json'], PINS.completed) && same(report.evidence?.['after-full.json'], PINS.current));
  need(completed?.marker === CONTINUATION_MARKER && completed.id === CONTINUATION_MARKER && completed.phase === 'complete' &&
    completed.from_schema === 27 && completed.to_schema === 27 && completed.to_sha256 === CANDIDATE.binary_sha256 &&
    completed.installed_binary_sha256 === CANDIDATE.binary_sha256 && same(completed.new_process, CANDIDATE.process) &&
    completed.evidence_directory === CONTINUATION && completed.publication === PUBLICATION &&
    completed.new_service?.ControlGroup === CANDIDATE_PROCESS.cgroup &&
    same(completed.continuation, report.continuation) && same(completed.old_process, report.old_process) &&
    completed.schema_artifacts?.source === CANDIDATE.source &&
    completed.schema_artifacts.source_manifest_sha256 === CANDIDATE.source_manifest_sha256 && same(state.upgrade, completed) &&
    Array.isArray(state.upgrade_history) && same(state.upgrade_history.at(-1), completed));
  sourceBinding(completed.schema_artifacts.schema27_binding);
  need(state.notifications_upgrade?.marker === CONTINUATION_MARKER && state.notifications_upgrade.phase === 'complete' &&
    state.notifications_upgrade.evidence_directory === CONTINUATION && state.notifications_upgrade.publication === PUBLICATION &&
    state.notifications_upgrade.binary_sha256 === CANDIDATE.binary_sha256 && same(state.notifications_upgrade.continuation, report.continuation));
  for (const [service, expected] of [[completed.new_service, CANDIDATE_PROCESS], [terminal?.candidate_service, CANDIDATE_PROCESS],
    [terminal?.primary_service, PRIMARY]]) need(service?.MainPID === String(expected.pid) && service.InvocationID === expected.invocation &&
      service.ActiveState === 'active' && service.SubState === 'running');
  need(terminal?.marker === 'goby-client-notifications-source44-continuation-terminal-v1' && terminal.status === 'passed' &&
    terminal.candidate_source === 44 && terminal.candidate_schema === 27 && terminal.client_acceptance === false &&
    terminal.unit === CONTINUATION_UNIT && terminal.fixture_phase === 'ready' && terminal.fixture_stage === 'complete' &&
    terminal.candidate_binary_sha256 === CANDIDATE.binary_sha256 && terminal.primary_binary_sha256 === PRIMARY.sha256 &&
    terminal.state_sha256 === CANDIDATE.state_sha256 && terminal.report_path === PINS.report.path &&
    terminal.report_sha256 === PINS.report.sha256 && terminal.completed_sha256 === PINS.completed.sha256 &&
    terminal.recursive_cgroup_empty === true && terminal.state?.Result === 'success' && terminal.state.ExecMainStatus === '0' &&
    terminal.state.MainPID === '0' && terminal.state.SubState === 'exited' && terminal.state.ControlGroup === '' &&
    terminal.state.InvocationID === CONTINUATION_INVOCATION);
  // These are historical receipt anchors, not current source32 runtime authority.
  need(profileReceipt?.marker === 'goby-client-special-features-fixture-v1' && profileReceipt.phase === 'complete' &&
    profileReceipt.schema === 27 && profileReceipt.profile_version === 3 &&
    state.special_features_profile?.receipt_path === FIXTURES.profile_receipt.path &&
    state.special_features_profile.receipt_sha256 === FIXTURES.profile_receipt.sha256 &&
    profileReport?.result === 'passed' && profileReport.phase === 'complete' &&
    profileReport.receipt_sha256 === FIXTURES.profile_receipt.sha256 && profileInspection?.result === 'passed' &&
    profileInspection.phase === 'complete' && profileInspection.schema === 27 &&
    profileInspection.receipt_sha256 === FIXTURES.profile_receipt.sha256 &&
    Array.isArray(musicChain) && musicChain.length === 5 &&
    musicChain[4].completedSHA256 === profileReceipt.upgrade?.completed_sha256 &&
    musicChain[4].reportSHA256 === profileReceipt.upgrade?.report_sha256 &&
    musicScanReceipt?.schema === 25 && musicScanReceipt.phase === 'complete' && musicScanReceipt.result === 'passed' &&
    musicScanReceipt.logout_status === 204 && musicScanReceipt.token_readback_status === 401);
  need(proxy?.pid === PROXY_PROCESS.pid && String(proxy.start_ticks) === String(PROXY_PROCESS.start_ticks) &&
    proxy.reference_only === false && Array.isArray(proxy.listen) && proxy.listen.length <= 4 &&
    proxy.listen.filter(value => value === '127.0.0.1:18196').length === 1);
  need(current?.schema === 27 && before?.schema === 27 && current.runtime_sha256 === CANDIDATE.runtime_sha256 &&
    current.browser_sha256 === SOURCE_CREDENTIALS_SHA && record(current.database) && record(before.database));
  const withoutTime = snapshot => {
    need(record(snapshot.database.metadata) && typeof snapshot.database.metadata.captured_at === 'string' &&
      Number.isFinite(Date.parse(snapshot.database.metadata.captured_at)));
    const metadata = { ...snapshot.database.metadata }; delete metadata.captured_at;
    return { ...snapshot, database: { ...snapshot.database, metadata } };
  };
  need(same(withoutTime(current), withoutTime(before)) &&
    Date.parse(before.database.metadata.captured_at) >= Date.parse(current.database.metadata.captured_at));
  const tables = before.database.tables;
  need(record(tables) && Object.keys(tables).length === 35 && Object.values(tables).every(Array.isArray) &&
    tables.sessions?.length === 73 && tables.devices?.length === 62 && tables.activity_entries?.length === 163 &&
    tables.play_sessions?.length === 26 && tables.user_item_data?.length === 7 && tables.libraries?.length === 4 && tables.items?.length === 22);
  const user = tables.users?.find(value => value.id === USER), target = tables.items.find(value => value.id === ITEM);
  need(user?.is_administrator === false && user.is_disabled === false && user.management_revision === 5 && target?.type === 'Movie' &&
    target.is_folder === false && target.library_id === LIBRARY && target.name === input.target.name &&
    target.parent_id === input.target.parent_id && target.root_id === input.target.root_id && target.relative_path === input.target.relative_path &&
    same(tables.libraries.map(value => ({ id: value.id, name: value.name })).sort((a, b) => a.id.localeCompare(b.id)),
      [...input.expected_libraries].sort((a, b) => a.id.localeCompare(b.id))));
  return true;
}

/** Reject duplicate decoded keys and retain exact large integral ledger values. */
export function parseSource44JSON(raw, allowArray = false) {
  need(typeof raw === 'string' && Buffer.byteLength(raw, 'utf8') <= SNAPSHOT_LIMIT);
  let position = 0, nodes = 0;
  const whitespace = () => { while (position < raw.length && /[ \t\r\n]/.test(raw[position])) position += 1; };
  function string() {
    const start = position++; let finished = false;
    while (position < raw.length) {
      if (raw[position] === '\\') { position += 2; continue; }
      if (raw[position++] === '"') { finished = true; break; }
    }
    need(finished);
    try { return JSON.parse(raw.slice(start, position)); } catch { need(false); }
  }
  function value(depth) {
    need(depth <= 64 && ++nodes <= 500000); whitespace();
    if (raw[position] === '"') return string();
    const opening = raw[position];
    if (opening === '{' || opening === '[') {
      position += 1; whitespace();
      const closing = opening === '{' ? '}' : ']', result = opening === '{' ? Object.create(null) : [];
      const keys = new Set();
      if (raw[position] === closing) { position += 1; return result; }
      for (;;) {
        let key;
        if (opening === '{') {
          need(raw[position] === '"'); key = string(); need(!keys.has(key)); keys.add(key);
          whitespace(); need(raw[position++] === ':');
        }
        const child = value(depth + 1);
        if (opening === '{') result[key] = child; else result.push(child);
        whitespace(); if (raw[position] === closing) { position += 1; return result; }
        need(raw[position++] === ','); whitespace();
      }
    }
    const token = /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(raw.slice(position));
    need(token); position += token[0].length;
    if (token[0] === 'true') return true;
    if (token[0] === 'false') return false;
    if (token[0] === 'null') return null;
    const number = Number(token[0]); need(Number.isFinite(number));
    return /^-?\d+$/.test(token[0]) && !Number.isSafeInteger(number) ? BigInt(token[0]) : number;
  }
  const result = value(0); whitespace();
  need(position === raw.length && (record(result) || allowArray && Array.isArray(result)));
  return result;
}

const unchanged = (before, after) => ['dev', 'ino', 'mode', 'uid', 'gid', 'nlink', 'size', 'mtimeNs', 'ctimeNs']
  .every(key => before[key] === after[key]);
const directoryIdentity = info => ({ device: String(info.dev), inode: String(info.ino), mode: String(info.mode), uid: String(info.uid), gid: String(info.gid) });

async function pinParents(filename, directories) {
  const names = []; let parent = path.posix.dirname(filename);
  for (;;) { names.unshift(parent); if (parent === '/') break; parent = path.posix.dirname(parent); }
  for (const name of names) {
    const info = await fs.lstat(name, { bigint: true });
    need(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0n && info.gid === 0n && (info.mode & 0o022n) === 0n);
    if ([WORK, ROOT, TOOL].includes(name)) need((info.mode & 0o7777n) === 0o700n);
    const identity = directoryIdentity(info);
    if (directories.has(name)) need(same(directories.get(name), identity)); else directories.set(name, identity);
  }
}

async function protectedFile(filename, expectedSHA, directories, json = true, maximum = JSON_LIMIT, modes = [0o600n], expectedIdentity) {
  need(ownedPath(filename) && digest(expectedSHA));
  await pinParents(filename, directories);
  need(await fs.realpath(filename) === filename);
  const before = await fs.lstat(filename, { bigint: true });
  need(before.isFile() && !before.isSymbolicLink() && before.uid === 0n && before.gid === 0n && before.nlink === 1n &&
    modes.includes(before.mode & 0o7777n) && before.size > 0n && before.size <= BigInt(maximum));
  if (expectedIdentity) need(unchanged(expectedIdentity, before));
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  const bytes = Buffer.alloc(65536), chunks = [], sha = createHash('sha256');
  try {
    need(unchanged(before, await handle.stat({ bigint: true })));
    let size = 0;
    for (;;) {
      const part = await handle.read(bytes, 0, bytes.length, size);
      if (!part.bytesRead) break;
      size += part.bytesRead; need(size <= maximum); sha.update(bytes.subarray(0, part.bytesRead));
      if (json) chunks.push(Buffer.from(bytes.subarray(0, part.bytesRead)));
    }
    need(size === Number(before.size) && sha.digest('hex') === expectedSHA &&
      unchanged(before, await handle.stat({ bigint: true })) && unchanged(before, await fs.lstat(filename, { bigint: true })) &&
      await fs.realpath(filename) === filename);
    await pinParents(filename, directories);
    let value;
    if (json) {
      const raw = Buffer.concat(chunks);
      try { value = parseSource44JSON(new TextDecoder('utf-8', { fatal: true }).decode(raw), true); } finally { raw.fill(0); }
    }
    return { path: filename, sha256: expectedSHA, value, identity: before, modes, maximum };
  } finally { bytes.fill(0); for (const chunk of chunks) chunk.fill(0); await handle.close(); }
}

async function procText(filename, maximum = 65536) {
  const handle = await fs.open(filename, constants.O_RDONLY), bytes = Buffer.alloc(maximum + 1);
  try {
    let size = 0;
    while (size < bytes.length) {
      const part = await handle.read(bytes, size, bytes.length - size, size);
      if (!part.bytesRead) break; size += part.bytesRead;
    }
    need(size <= maximum);
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes.subarray(0, size));
  } finally { bytes.fill(0); await handle.close(); }
}

async function checkProcess(expected) {
  const raw = await procText(`/proc/${expected.pid}/stat`), end = raw.lastIndexOf(')');
  need(raw.startsWith(`${expected.pid} (`) && end > 0);
  const fields = raw.slice(end + 1).trim().split(/\s+/);
  need(fields[19] === String(expected.start_ticks) && !['Z', 'X', 'x'].includes(fields[0]));
}

async function hashExecutable(expected, identities) {
  const filename = `/proc/${expected.pid}/exe`;
  need(await fs.readlink(filename) === expected.executable);
  const handle = await fs.open(filename, constants.O_RDONLY), bytes = Buffer.alloc(65536), sha = createHash('sha256');
  try {
    const before = await handle.stat({ bigint: true });
    need(before.isFile() && before.uid === 0n && before.gid === 0n && before.nlink === 1n &&
      (before.mode & 0o7777n) === 0o755n && before.size > 0n && before.size <= 64n * 1024n * 1024n);
    if (identities.has(filename)) need(unchanged(identities.get(filename), before)); else identities.set(filename, before);
    let size = 0;
    for (;;) {
      const part = await handle.read(bytes, 0, bytes.length, size);
      if (!part.bytesRead) break;
      size += part.bytesRead; need(size <= 64 * 1024 * 1024); sha.update(bytes.subarray(0, part.bytesRead));
    }
    need(size === Number(before.size) && sha.digest('hex') === expected.sha256 &&
      unchanged(before, await handle.stat({ bigint: true })) && await fs.readlink(filename) === expected.executable);
  } finally { bytes.fill(0); await handle.close(); }
}

export function validateSource44ProxyArguments(argv) {
  need(Array.isArray(argv) && argv.length >= 3 && argv.length <= 32 && argv.every(value => text(value, 4096)));
  const start = argv.findIndex(value => value.startsWith('--'));
  need(start > 0 && start <= 4 && argv.slice(0, start).filter(value => value.startsWith('/opt/goby-test/') &&
    path.posix.normalize(value) === value && path.posix.basename(value) === 'client-acceptance-proxy.py').length === 1);
  const allowed = new Set(['reference-pid', 'reference-start-ticks', 'reference-sha256', 'reference-port', 'reference-listen',
    'goby-listen', 'goby-port', 'idle-seconds', 'status-file']);
  const options = new Map();
  for (let index = start; index < argv.length; index += 1) {
    const matched = /^--([a-z0-9-]+)(?:=(.*))?$/.exec(argv[index]);
    need(matched && allowed.has(matched[1]) && !options.has(matched[1]));
    const value = matched[2] ?? argv[++index]; need(text(value, 4096) && !value.startsWith('--'));
    options.set(matched[1], value);
  }
  // Reference options are opaque launch arguments. Never resolve their values.
  need(options.get('status-file') === PINS.proxy.path &&
    (!options.has('goby-listen') || options.get('goby-listen') === '18196') &&
    (!options.has('goby-port') || options.get('goby-port') === '18198'));
  if (options.has('idle-seconds')) need(/^\d+$/.test(options.get('idle-seconds')) &&
    Number(options.get('idle-seconds')) >= 30 && Number(options.get('idle-seconds')) <= 3600);
  return true;
}

async function socketOwners(pid) {
  const names = await fs.readdir(`/proc/${pid}/fd`); need(names.length <= 4096);
  const result = new Set();
  for (const name of names) {
    need(/^\d+$/.test(name));
    try { const match = /^socket:\[(\d+)\]$/.exec(await fs.readlink(`/proc/${pid}/fd/${name}`)); if (match) result.add(match[1]); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
  }
  return result;
}

async function pinListeners() {
  const bindings = [[18196, PROXY_PROCESS.pid], [18198, CANDIDATE.process.pid]], selected = [];
  for (const table of ['tcp', 'tcp6']) {
    const raw = await procText(`/proc/self/net/${table}`, JSON_LIMIT);
    for (const line of raw.trim().split('\n').slice(1)) {
      const fields = line.trim().split(/\s+/);
      if (fields[3] === '0A' && bindings.some(([port]) => fields[1]?.endsWith(':' + port.toString(16).toUpperCase())))
        selected.push({ table, address: fields[1], inode: fields[9] });
    }
  }
  for (const [port, pid] of bindings) {
    const suffix = ':' + port.toString(16).toUpperCase(), matches = selected.filter(row => row.address.endsWith(suffix));
    need(matches.length === 1 && matches[0].table === 'tcp' && matches[0].address === '0100007F' + suffix &&
      /^\d+$/.test(matches[0].inode) && (await socketOwners(pid)).has(matches[0].inode));
  }
}

export async function loadLibraryChangedSource44Fixture(options = {}) {
  let phase = 'input';
  try {
    need(exact(options, ['input', 'inputSHA256']) && digest(options.inputSHA256));
    validateLibraryChangedSource44Input(options.input);
    need(process.platform === 'linux' && process.getuid?.() === 0 && SELF === TOOL + '/client-library-changed-source44-fixture.mjs');
    const directories = new Map(), pins = [], executableIdentities = new Map();
    async function read(value, json = true, maximum = JSON_LIMIT, modes = [0o600n]) {
      const file = await protectedFile(value.path, value.sha256, directories, json, maximum, modes);
      const { value: ignored, ...pin } = file; pins.push(pin); return file.value;
    }
    const input = await read({ path: ROOT + '/input.json', sha256: options.inputSHA256 });
    need(same(input, options.input)); validateLibraryChangedSource44Input(input);
    phase = 'sources';
    for (const [filename, digestValue] of Object.entries(input.source_closure))
      await read({ path: filename, sha256: digestValue }, false, JSON_LIMIT, [0o600n, 0o644n, 0o700n, 0o755n]);
    phase = 'authority';
    const documents = {
      state: await read({ path: STATE, sha256: CANDIDATE.state_sha256 }),
      report: await read(PINS.report), completed: await read(PINS.completed), terminal: await read(PINS.terminal),
      current: await read(PINS.current, true, SNAPSHOT_LIMIT), before: await read(input.authority.before_snapshot, true, SNAPSHOT_LIMIT),
      profileReceipt: await read(FIXTURES.profile_receipt), profileReport: await read(FIXTURES.profile_report),
      profileInspection: await read(FIXTURES.profile_inspection), musicChain: await read(FIXTURES.music_chain),
      musicScanReceipt: await read(FIXTURES.music_scan_receipt), proxy: await read(PINS.proxy),
    };
    validateLibraryChangedSource44Documents(input, documents);
    const work = directories.get(WORK);
    need(work && String(documents.state.work_identity.device) === work.device && String(documents.state.work_identity.inode) === work.inode);
    phase = 'private_inputs';
    validateLibraryChangedSource44Viewer(await read(input.actor.credentials, true, 16384));
    await read({ path: WORK + '/runtime.env', sha256: CANDIDATE.runtime_sha256 }, false, 65536);
    await read({ path: CANDIDATE.source + '/backup-source-inputs.json', sha256: CANDIDATE.source_manifest_sha256 }, false, JSON_LIMIT, [0o644n]);
    const evidence = freeze({ schema: 27, source: 'source-attempt-44', process: { ...CANDIDATE.process },
      binary_sha256: CANDIDATE.binary_sha256, fixture_state_sha256: CANDIDATE.state_sha256,
      source_manifest_sha256: CANDIDATE.source_manifest_sha256, input_sha256: options.inputSHA256,
      source_closure_sha256: hash(canonical(input.source_closure)), authority: { ...input.authority },
      historical_anchors: FIXTURES, account_id: USER, credentials_sha256: input.actor.credentials.sha256,
      proxy_process: { ...PROXY_PROCESS }, primary_process: { pid: PRIMARY.pid, start_ticks: PRIMARY.start_ticks, boot_id: BOOT },
      boundary: 'Read-only Goby files, source closure, process lifetimes and two listener owners; live database deltas belong to the controller ledger' });
    let checks = 0;
    async function assertPinned() {
      try {
        for (const pin of pins) await protectedFile(pin.path, pin.sha256, directories, false, pin.maximum, pin.modes, pin.identity);
        need((await procText('/proc/sys/kernel/random/boot_id')).trim() === BOOT);
        const network = await fs.readlink('/proc/self/ns/net');
        need(network === await fs.readlink('/proc/1/ns/net'));
        const processes = [CANDIDATE_PROCESS, PRIMARY, PROXY_PROCESS];
        for (const expected of processes) {
          await checkProcess(expected); need(await fs.readlink(`/proc/${expected.pid}/ns/net`) === network);
        }
        for (const expected of [CANDIDATE_PROCESS, PRIMARY]) {
          need((await fs.stat(`/proc/${expected.pid}`, { bigint: true })).uid === BigInt(expected.uid) &&
            (await procText(`/proc/${expected.pid}/cgroup`)).trim() === '0::' + expected.cgroup &&
            await procText(`/proc/${expected.pid}/cmdline`) === expected.executable + '\0');
          const environment = await procText(`/proc/${expected.pid}/environ`);
          need(environment.endsWith('\0') && environment.slice(0, -1).split('\0')
            .filter(value => value.startsWith('INVOCATION_ID=')).join('') === 'INVOCATION_ID=' + expected.invocation);
          await hashExecutable(expected, executableIdentities);
        }
        const argv = await procText(`/proc/${PROXY_PROCESS.pid}/cmdline`); need(argv.endsWith('\0'));
        validateSource44ProxyArguments(argv.slice(0, -1).split('\0'));
        await pinListeners();
        for (const expected of processes) await checkProcess(expected);
        need((await procText('/proc/sys/kernel/random/boot_id')).trim() === BOOT);
        for (const pin of pins) await pinParents(pin.path, directories);
        return { phase: 'pinned', check: ++checks, fixture_state_sha256: CANDIDATE.state_sha256 };
      } catch { throw new Error('library_changed_source44_live_pin_failed'); }
    }
    phase = 'initial_pin'; await assertPinned();
    return Object.freeze({ serverId: SERVER, evidence, assertPinned });
  } catch { throw Object.assign(new Error('library_changed_source44_' + phase + '_failed'), { phase }); }
}
