#!/usr/bin/env node
/** Read-only loader guards for an explicitly selected single or bounded Music upgrade chain. */

import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { TextDecoder } from 'node:util';

const ROOT = '/opt/goby-test/exec-work-m3e';
const SELF = fileURLToPath(import.meta.url);
const LOADER = fileURLToPath(new URL('./client-browser-goby-fixture.mjs', import.meta.url));
const CORE_FLAGS = ['candidate-sha256', 'music-scan-receipt-sha256', 'output'];
const SINGLE_FLAGS = ['music-upgrade-directory', 'music-upgrade-completed-sha256', 'music-upgrade-report', 'music-upgrade-report-sha256'];
const CHAIN_FLAGS = ['music-upgrade-chain', 'music-upgrade-chain-sha256'];
const FLAGS = [...CORE_FLAGS, ...SINGLE_FLAGS, ...CHAIN_FLAGS];
const CHAIN_PATH = new RegExp(`^${ROOT}/client-music-upgrade-chain-[A-Za-z0-9_-]{1,64}\\.json$`);
const digest = value => typeof value === 'string' && /^[0-9a-f]{64}$/.test(value);
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const hash = value => createHash('sha256').update(value).digest('hex');
const stable = value => Array.isArray(value) ? value.map(stable) : record(value)
  ? Object.fromEntries(Object.keys(value).sort().map(key => [key, stable(value[key])])) : value;
const same = (left, right) => JSON.stringify(stable(left)) === JSON.stringify(stable(right));
const wrongHash = value => (value[0] === '0' ? '1' : '0') + value.slice(1);

function requireThat(value) {
  if (!value) throw new Error('music_lineage_guard_failed');
}

function parseArguments(argv) {
  requireThat(argv.length > 0 && argv.length % 2 === 0);
  const parsed = {};
  for (let index = 0; index < argv.length; index += 2) {
    const name = argv[index]?.slice(2), value = argv[index + 1];
    requireThat(argv[index]?.startsWith('--') && FLAGS.includes(name) && !Object.hasOwn(parsed, name) && typeof value === 'string' && value.length > 0);
    parsed[name] = value;
  }
  selectionMode(parsed);
  return parsed;
}

function selectionMode(options) {
  requireThat(record(options) && Object.keys(options).every(name => FLAGS.includes(name)) &&
    CORE_FLAGS.every(name => Object.hasOwn(options, name) && typeof options[name] === 'string' && options[name].length > 0));
  const single = SINGLE_FLAGS.some(name => Object.hasOwn(options, name));
  const chain = CHAIN_FLAGS.some(name => Object.hasOwn(options, name));
  requireThat(single !== chain);
  requireThat((chain ? CHAIN_FLAGS : SINGLE_FLAGS).every(name => Object.hasOwn(options, name) &&
    typeof options[name] === 'string' && options[name].length > 0));
  return chain ? 'chain' : 'single';
}

function validUpgradeEntry(entry) {
  return record(entry) && same(Object.keys(entry).sort(), ['completedSHA256', 'directory', 'reportPath', 'reportSHA256']) &&
    digest(entry.completedSHA256) && digest(entry.reportSHA256) &&
    typeof entry.directory === 'string' && new RegExp(`^${ROOT}/client-upgrade-[0-9a-f]{32}$`).test(entry.directory) &&
    typeof entry.reportPath === 'string' && new RegExp(`^${ROOT}/client-fixture-report-[0-9a-f]{24}\\.json$`).test(entry.reportPath);
}

async function readAuthorizedChain(filename, expectedSHA256) {
  const limit = 2 * 1024 * 1024;
  requireThat(CHAIN_PATH.test(filename) && digest(expectedSHA256) && await fs.realpath(filename) === filename);
  const before = await fs.lstat(filename, { bigint: true });
  requireThat(before.isFile() && !before.isSymbolicLink() && before.uid === 0n && before.nlink === 1n &&
    (before.mode & 0o777n) === 0o600n && before.size > 0n && before.size <= BigInt(limit));
  const unchanged = after => ['dev', 'ino', 'uid', 'gid', 'mode', 'nlink', 'size', 'mtimeNs', 'ctimeNs'].every(key => before[key] === after[key]);
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  const bytes = Buffer.alloc(limit + 1);
  try {
    requireThat(unchanged(await handle.stat({ bigint: true })));
    let size = 0;
    while (size < bytes.length) {
      const read = await handle.read(bytes, size, bytes.length - size, size);
      if (!read.bytesRead) break;
      size += read.bytesRead;
    }
    requireThat(size === Number(before.size) && size <= limit && unchanged(await handle.stat({ bigint: true })) &&
      unchanged(await fs.lstat(filename, { bigint: true })));
    const raw = bytes.subarray(0, size);
    // Approved byte identity is checked before any JSON values are interpreted.
    requireThat(hash(raw) === expectedSHA256);
    const entries = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(raw));
    requireThat(Array.isArray(entries) && entries.length >= 1 && entries.length <= 8 && entries.every(validUpgradeEntry));
    return entries;
  } finally { bytes.fill(0); await handle.close(); }
}

async function privateDirectory(filename) {
  const info = await fs.lstat(filename);
  requireThat(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0 && (info.mode & 0o777) === 0o700 &&
    await fs.realpath(filename) === filename);
}

async function reserveReport(output) {
  requireThat(typeof output === 'string' && path.isAbsolute(output) && path.resolve(output) === output &&
    path.dirname(output) === ROOT && /^[A-Za-z0-9][A-Za-z0-9_.-]{0,95}$/.test(path.basename(output)));
  await privateDirectory(ROOT);
  let filename = output;
  if (!output.endsWith('.json')) {
    await fs.mkdir(output, { mode: 0o700 });
    await privateDirectory(output);
    filename = path.join(output, 'music-lineage-guards.json');
  }
  return fs.open(filename, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
}

async function sourceHashes() {
  const result = {};
  for (const filename of [SELF, LOADER]) {
    const info = await fs.lstat(filename);
    requireThat(info.isFile() && !info.isSymbolicLink() && info.uid === 0 && info.nlink === 1 && info.size > 0 && info.size <= 2 * 1024 * 1024);
    const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const opened = await handle.stat();
      requireThat(opened.dev === info.dev && opened.ino === info.ino && opened.size === info.size);
      const bytes = await handle.readFile();
      requireThat(bytes.length === info.size);
      result[path.basename(filename)] = hash(bytes);
    } finally { await handle.close(); }
  }
  return result;
}

function validProcess(value) {
  return record(value) && Number.isSafeInteger(value.pid) && value.pid > 1 && Number.isSafeInteger(value.start_ticks) &&
    value.start_ticks > 0 && typeof value.boot_id === 'string' && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(value.boot_id);
}

/** Importing this module performs no guard run. Full explicit inputs are required. */
export async function runMusicLineageGuards(options) {
  requireThat(process.platform === 'linux' && process.getuid?.() === 0 && typeof process.env.SSH_CONNECTION === 'string' &&
    process.env.SSH_CONNECTION.length <= 512 && process.env.SSH_CONNECTION.trim().split(/\s+/).length === 4);
  const mode = selectionMode(options), chainMode = mode === 'chain';
  for (const field of ['candidate-sha256', 'music-scan-receipt-sha256', ...(chainMode
    ? ['music-upgrade-chain-sha256'] : ['music-upgrade-completed-sha256', 'music-upgrade-report-sha256'])]) {
    requireThat(digest(options[field]));
  }
  if (chainMode) requireThat(CHAIN_PATH.test(options['music-upgrade-chain']));
  else requireThat(new RegExp(`^${ROOT}/client-upgrade-[0-9a-f]{32}$`).test(options['music-upgrade-directory']) &&
    new RegExp(`^${ROOT}/client-fixture-report-[0-9a-f]{24}\\.json$`).test(options['music-upgrade-report']));
  process.umask(0o077);
  const output = await reserveReport(options.output);
  const report = { format: 1, mode, result: 'blocked', harness_guards_only: true, client_acceptance: false,
    boundary: 'Only loader file and process guards; no browser, database, HTTP, rescan, or user mutation.',
    planned_test_count: chainMode ? 6 : 7, tests: [], source_sha256: {},
    selected_sha256: { candidate: options['candidate-sha256'], music_scan_receipt: options['music-scan-receipt-sha256'],
      ...(chainMode ? { upgrade_chain: options['music-upgrade-chain-sha256'] }
        : { upgrade_completed: options['music-upgrade-completed-sha256'], upgrade_report: options['music-upgrade-report-sha256'] }) } };
  try {
    report.source_sha256 = await sourceHashes();
    const { loadGobyAVFixture } = await import('./client-browser-goby-fixture.mjs');
    const base = { expectedSHA256: options['candidate-sha256'], expectedMusicScanReceiptSHA256: options['music-scan-receipt-sha256'],
      ...(chainMode ? { musicUpgradeChain: { path: options['music-upgrade-chain'], sha256: options['music-upgrade-chain-sha256'] } }
        : { musicUpgrade: { directory: options['music-upgrade-directory'], completedSHA256: options['music-upgrade-completed-sha256'],
          reportPath: options['music-upgrade-report'], reportSHA256: options['music-upgrade-report-sha256'] } }) };
    let fixture, chainEntries;
    const positive = { name: chainMode ? 'explicit_bounded_upgrade_chain' : 'explicit_single_upgrade_lineage', pass: false, count: 0 };
    report.tests.push(positive);
    const check = value => { positive.count += 1; requireThat(value); };
    try {
      if (chainMode) {
        chainEntries = await readAuthorizedChain(base.musicUpgradeChain.path, base.musicUpgradeChain.sha256);
        check(chainEntries.length >= 1 && chainEntries.length <= 8);
      }
      fixture = await loadGobyAVFixture(base);
      check((await fixture.assertPinned()).phase === 'pinned');
      const evidence = fixture.evidence, scan = evidence?.music_scan, lineage = scan?.upgrade_lineage;
      check(record(scan) && record(lineage));
      check(evidence.binary_sha256 === base.expectedSHA256 && scan.receipt_sha256 === base.expectedMusicScanReceiptSHA256);
      if (chainMode) {
        check(lineage.method === 'Explicit bounded completed schema25 upgrade chain');
        check(lineage.chain_path === base.musicUpgradeChain.path && lineage.chain_sha256 === base.musicUpgradeChain.sha256);
        check(lineage.hop_count === chainEntries.length && Array.isArray(lineage.hops) && lineage.hops.length === chainEntries.length);
        for (let index = 0; index < chainEntries.length; index += 1) {
          const entry = chainEntries[index], hop = lineage.hops[index];
          check(record(hop) && hop.method === 'Explicit single completed schema25 binary upgrade');
          check(hop.directory === entry.directory && hop.completed_sha256 === entry.completedSHA256 &&
            hop.report_path === entry.reportPath && hop.report_sha256 === entry.reportSHA256);
          check(digest(hop.before_state_sha256) && digest(hop.from_sha256) && digest(hop.to_sha256) &&
            hop.from_sha256 !== hop.to_sha256 && hop.operator_sha256 === lineage.operator_sha256);
          check(hop.from_schema === 25 && hop.to_schema === 25 && validProcess(hop.old_process) && validProcess(hop.new_process) &&
            !same(hop.old_process, hop.new_process));
          check(hop.full_snapshot_read === false && hop.automatic_history_discovery === false && hop.scan_repeated === false);
          if (index > 0) check(lineage.hops[index - 1].to_sha256 === hop.from_sha256 &&
            same(lineage.hops[index - 1].new_process, hop.old_process));
        }
        const first = lineage.hops[0], last = lineage.hops[lineage.hops.length - 1];
        check(lineage.before_state_sha256 === first.before_state_sha256 && lineage.from_sha256 === first.from_sha256 &&
          lineage.to_sha256 === last.to_sha256 && same(lineage.old_process, first.old_process) && same(lineage.new_process, last.new_process));
      } else {
        check(lineage.method === 'Explicit single completed schema25 binary upgrade');
        check(lineage.directory === base.musicUpgrade.directory && lineage.report_path === base.musicUpgrade.reportPath);
        check(lineage.completed_sha256 === base.musicUpgrade.completedSHA256 && lineage.report_sha256 === base.musicUpgrade.reportSHA256);
      }
      check(digest(lineage.before_state_sha256) && lineage.before_state_sha256 === scan.fixture_state_sha256 && digest(lineage.operator_sha256));
      check(lineage.from_schema === 25 && lineage.to_schema === 25 && scan.schema === 25 && evidence.schema === 25);
      check(lineage.from_sha256 === scan.candidate_sha256 && lineage.to_sha256 === base.expectedSHA256 && lineage.from_sha256 !== lineage.to_sha256);
      check(validProcess(scan.process) && validProcess(evidence.process) && validProcess(lineage.old_process) && validProcess(lineage.new_process));
      check(same(scan.process, lineage.old_process) && same(evidence.process, lineage.new_process) && !same(scan.process, evidence.process));
      check(lineage.automatic_history_discovery === false && lineage.scan_repeated === false && lineage.full_snapshot_read === false);
      const album = fixture.music?.album, tracks = fixture.music?.tracks;
      check(fixture.album === 'M3e Synthetic Album' && album?.name === 'M3e Synthetic Album' && album.path === '/opt/goby-fixtures/client-m3e/Music');
      check(album.id === '002c2ea2c77769ae9b0b84b6e788998b' && same(album, scan.album));
      check(record(tracks) && same(Object.keys(tracks).sort(), ['flac', 'mp3']) && same(tracks, scan.tracks));
      for (const [container, name, expectedId] of [['mp3', 'M3e MP3', 'e2da060a0addfafde7f92c291c413f6d'],
        ['flac', 'M3e FLAC', '4d2ec259d9ae5224f46aa6f56ca2b186']]) {
        const track = tracks[container];
        check(record(track) && track.name === name && track.container === container && track.parentId === album.id &&
          track.id === expectedId &&
          track.path === `/opt/goby-fixtures/client-m3e/Music/M3e Client Audio.${container}`);
      }
      check(tracks.mp3.id !== tracks.flac.id && tracks.mp3.id !== album.id && tracks.flac.id !== album.id);
      positive.evidence_sha256 = hash(JSON.stringify(stable({ lineage, scan_process: scan.process, current_process: evidence.process, album, tracks })));
      positive.pass = true;
    } catch { /* Never retain exception text or continue into negative cases after a failed positive. */ }

    if (positive.pass) {
      const negatives = chainMode ? [
        ['wrong_chain_sha256', 'music_scan_binding', () => ({ ...base,
          musicUpgradeChain: { ...base.musicUpgradeChain, sha256: wrongHash(base.musicUpgradeChain.sha256) } })],
        ['missing_explicit_music_upgrade_chain', 'music_scan_binding', () => ({ expectedSHA256: base.expectedSHA256,
          expectedMusicScanReceiptSHA256: base.expectedMusicScanReceiptSHA256 })],
        ['outside_music_upgrade_chain_path', 'music_scan_binding', () => ({ ...base,
          musicUpgradeChain: { ...base.musicUpgradeChain, path: path.join(path.dirname(ROOT), path.basename(base.musicUpgradeChain.path)) } })],
        ['single_and_chain_selections_conflict', 'music_scan_binding', () => ({ ...base, musicUpgrade: { ...chainEntries[0] } })],
        ['wrong_candidate_sha256', 'candidate_sha256', () => ({ ...base, expectedSHA256: wrongHash(base.expectedSHA256) })],
      ] : [
        ['missing_explicit_music_upgrade', 'music_scan_binding', () => ({ expectedSHA256: base.expectedSHA256, expectedMusicScanReceiptSHA256: base.expectedMusicScanReceiptSHA256 })],
        ['wrong_completed_sha256', 'music_scan_binding', () => ({ ...base, musicUpgrade: { ...base.musicUpgrade, completedSHA256: wrongHash(base.musicUpgrade.completedSHA256) } })],
        ['wrong_report_sha256', 'music_scan_binding', () => ({ ...base, musicUpgrade: { ...base.musicUpgrade, reportSHA256: wrongHash(base.musicUpgrade.reportSHA256) } })],
        ['outside_upgrade_evidence_directory', 'music_scan_binding', () => ({ ...base, musicUpgrade: { ...base.musicUpgrade, directory: ROOT } })],
        ['partial_music_upgrade_object', 'music_scan_binding', () => ({ ...base, musicUpgrade: { directory: base.musicUpgrade.directory,
          completedSHA256: base.musicUpgrade.completedSHA256, reportPath: base.musicUpgrade.reportPath } })],
        ['wrong_candidate_sha256', 'candidate_sha256', () => ({ ...base, expectedSHA256: wrongHash(base.expectedSHA256) })],
      ];
      for (const [name, expectedPhase, selection] of negatives) {
        const entry = { name, pass: false, count: 0 };
        report.tests.push(entry);
        try {
          requireThat((await fixture.assertPinned()).phase === 'pinned'); entry.count += 1;
          let rejected = false;
          try { await loadGobyAVFixture(selection()); }
          catch (error) { rejected = error?.phase === expectedPhase; }
          entry.count += 1;
          requireThat((await fixture.assertPinned()).phase === 'pinned'); entry.count += 1;
          entry.pass = rejected;
        } catch { break; }
      }
    }
    if (chainMode && positive.pass) {
      requireThat(same(chainEntries, await readAuthorizedChain(base.musicUpgradeChain.path, base.musicUpgradeChain.sha256)));
      report.chain_file_unchanged = true;
    }
    report.source_files_unchanged = same(report.source_sha256, await sourceHashes());
    report.result = report.source_files_unchanged && (!chainMode || report.chain_file_unchanged) &&
      report.tests.length === report.planned_test_count && report.tests.every(entry => entry.pass) ? 'passed' : 'blocked';
  } catch { report.result = 'blocked'; }
  finally {
    report.counts = { executed: report.tests.length, passed: report.tests.filter(entry => entry.pass).length,
      failed: report.tests.filter(entry => !entry.pass).length };
    try { await output.writeFile(JSON.stringify(report, null, 2) + '\n'); await output.sync(); }
    finally { await output.close(); }
  }
  return report;
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  try {
    const report = await runMusicLineageGuards(parseArguments(process.argv.slice(2)));
    process.stdout.write(JSON.stringify({ result: report.result, harness_guards_only: true, counts: report.counts }) + '\n');
    if (report.result !== 'passed') process.exitCode = 1;
  } catch {
    process.stdout.write(JSON.stringify({ result: 'blocked', harness_guards_only: true }) + '\n');
    process.exitCode = 1;
  }
}
