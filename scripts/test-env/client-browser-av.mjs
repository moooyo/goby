#!/usr/bin/env node
/** Run independent original-client AV playback or read-only TV browsing. */

import fs from 'node:fs/promises';
import path from 'node:path';
import http from 'node:http';
import { createHash } from 'node:crypto';
import { runGuardedAV } from './client-browser-av-runtime.mjs';
import { runAudioUI } from './client-browser-audio-flow.mjs';
import { runSubtitleUI } from './client-browser-subtitle-flow.mjs';
import { runTVBrowseUI } from './client-browser-tv-flow.mjs';
import { loadGobyAVFixture } from './client-browser-goby-fixture.mjs';
import { runAlbumDiagnostics } from './client-browser-album-diagnostics.mjs';
import { runSubtitleContractDiagnostics } from './client-browser-subtitle-diagnostics.mjs';
import { createAudioReportDiagnostics } from './client-browser-audio-report-diagnostics.mjs';

const ROOT = '/opt/goby-test/exec-work-m3e';
const SETUP = path.join(ROOT, 'av-user-setup');
const musicUpgradeFlags = ['music-upgrade-directory', 'music-upgrade-completed-sha256', 'music-upgrade-report', 'music-upgrade-report-sha256'];
const musicChainFlags = ['music-upgrade-chain', 'music-upgrade-chain-sha256'];
const options = { check: 'mp3', backend: 'reference' };
for (let index = 2; index < process.argv.length; index += 2) {
  const name = process.argv[index], value = process.argv[index + 1];
  if (!['--check', '--output', '--backend', '--candidate-sha256', '--music-scan-receipt-sha256',
    ...musicUpgradeFlags.map(flag => '--' + flag), ...musicChainFlags.map(flag => '--' + flag)].includes(name) || !value) throw new Error('Unsupported independent AV argument.');
  options[name.slice(2)] = value;
}
const hasMusicUpgrade = musicUpgradeFlags.some(flag => Object.hasOwn(options, flag));
const hasMusicChain = musicChainFlags.some(flag => Object.hasOwn(options, flag));
if (process.platform !== 'linux' || process.getuid() !== 0 || !['mp3', 'flac', 'subtitles', 'browse-audio', 'tv', 'diagnose-album', 'diagnose-subtitles', 'diagnose-audio'].includes(options.check) ||
    !['reference', 'goby'].includes(options.backend) ||
    (options.backend === 'goby' ? !/^[a-f0-9]{64}$/.test(options['candidate-sha256'] ?? '') : Boolean(options['candidate-sha256'])) ||
    (options['music-scan-receipt-sha256'] && (options.backend !== 'goby' || !/^[a-f0-9]{64}$/.test(options['music-scan-receipt-sha256']))) ||
    (hasMusicUpgrade && (options.backend !== 'goby' || !options['music-scan-receipt-sha256'] ||
      musicUpgradeFlags.some(flag => !Object.hasOwn(options, flag)))) ||
    (hasMusicChain && (options.backend !== 'goby' || hasMusicUpgrade || !options['music-scan-receipt-sha256'] ||
      musicChainFlags.some(flag => !Object.hasOwn(options, flag)))) ||
    (options.check === 'diagnose-audio' && (options.backend !== 'goby' || !(hasMusicUpgrade || hasMusicChain))) ||
    (options.backend === 'goby' && ['mp3', 'flac', 'diagnose-audio'].includes(options.check) && !options['music-scan-receipt-sha256']) ||
    !options.output || path.dirname(path.resolve(options.output)) !== ROOT) throw new Error('An explicit private remote AV output is required.');
const goby = options.backend === 'goby' ? await loadGobyAVFixture({ expectedSHA256: options['candidate-sha256'],
  expectedMusicScanReceiptSHA256: options['music-scan-receipt-sha256'],
  musicUpgrade: hasMusicUpgrade ? { directory: options['music-upgrade-directory'],
    completedSHA256: options['music-upgrade-completed-sha256'], reportPath: options['music-upgrade-report'],
    reportSHA256: options['music-upgrade-report-sha256'] } : undefined,
  musicUpgradeChain: hasMusicChain ? { path: options['music-upgrade-chain'], sha256: options['music-upgrade-chain-sha256'] } : undefined }) : null;
const ORIGIN = goby?.origin ?? 'http://127.0.0.1:18197';
const MARKER = goby?.accountMarker ?? 'goby-reference-av-client-v1';
const ALBUM = goby?.album ?? 'M3e Synthetic Album';
const CREDENTIALS = goby?.credentialsPath ?? path.join(SETUP, 'browser.json');
const diagnosticOnly = ['diagnose-album', 'diagnose-subtitles', 'diagnose-audio'].includes(options.check);

const sha = value => createHash('sha256').update(value).digest('hex');
const driverSourceHashes = {};
for (const filename of ['client-browser-av.mjs', 'client-browser-av-runtime.mjs', 'client-browser-audio-flow.mjs',
  'client-browser-subtitle-flow.mjs', 'client-browser-tv-flow.mjs', 'client-browser-session-proof.mjs',
  'client-browser-goby-fixture.mjs', 'client-browser-album-diagnostics.mjs', 'client-browser-subtitle-diagnostics.mjs',
  'client-browser-audio-report-diagnostics.mjs']) {
  driverSourceHashes[filename] = sha(await fs.readFile(new URL(filename, import.meta.url)));
}
function stable(value) {
  if (Array.isArray(value)) return value.map(stable);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map(key => [key, stable(value[key])]));
  return value;
}
async function privateJSON(filename) {
  const info = await fs.lstat(filename);
  if (!info.isFile() || info.isSymbolicLink() || info.uid !== 0 || (info.mode & 0o777) !== 0o600 || info.size > 2 * 1024 * 1024) {
    throw new Error('AV fixture evidence permissions differ.');
  }
  try { return JSON.parse(await fs.readFile(filename, 'utf8')); }
  catch { throw new Error('AV fixture evidence is not valid private JSON.'); }
}
async function pinReference() {
  const process = '/proc/332054';
  const stat = await fs.readFile(path.join(process, 'stat'), 'utf8');
  if (stat.slice(stat.lastIndexOf(')') + 2).split(' ')[19] !== '357218' ||
      await fs.readlink(path.join(process, 'exe')) !== '/dev/shm/goby-emby-reference/package/opt/emby-server/system/EmbyServer' ||
      sha(await fs.readFile(path.join(process, 'exe'))) !== 'c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2') {
    throw new Error('The fresh reference process identity changed.');
  }
  const proxy = await privateJSON(path.join(ROOT, 'dual-proxy-status-02.json'));
  if (proxy.reference_pid !== 332054 || proxy.reference_start_ticks !== '357218') throw new Error('The fresh reference proxy binding changed.');
}

const setup = goby ? { account_id: goby.accountId, initial_media_state: [] } : await privateJSON(path.join(SETUP, 'completion-report.json'));
const owner = goby ? { username: goby.username, server_id: goby.serverId } : await privateJSON(path.join(SETUP, 'private-intent.json'));
if (!goby && (!setup.complete || setup.marker !== MARKER || owner.marker !== MARKER ||
    owner.username !== 'm3e-reference-av-client' || setup.initial_media_state.length !== 3)) throw new Error('The dedicated AV account setup is incomplete.');
async function pinTarget() {
  await pinReference();
  if (goby) await goby.assertPinned();
}
await pinTarget();
const expectedMedia = [...setup.initial_media_state];
if (!goby && options.check === 'tv') {
  const fixture = await privateJSON(path.join(ROOT, 'reference-report.json'));
  const expectedTV = [
    ['Series', 'M3e Client Series', 'M3e Client Series'],
    ['Season', 'Season 1', 'M3e Client Series/Season 01'],
    ['Season', 'Season 2', 'M3e Client Series/Season 02'],
    ['Episode', 'Episode 1-1', 'M3e Client Series/Season 01/M3e Client Series S01E01.mp4'],
    ['Episode', 'Episode 1-2', 'M3e Client Series/Season 01/M3e Client Series S01E02.mp4'],
    ['Episode', 'Episode 2-1', 'M3e Client Series/Season 02/M3e Client Series S02E01.mp4'],
  ];
  if (fixture.marker !== 'goby-emby-client-reference-m3e-v1' || fixture.serverId !== owner.server_id) {
    throw new Error('The owned TV fixture identity differs.');
  }
  for (const [type, name, suffix] of expectedTV) {
    const sourcePath = '/opt/goby-fixtures/client-m3e/TV/' + suffix;
    const matches = fixture.items.filter(item => item.Type === type && item.Name === name && item.Path === sourcePath);
    if (matches.length !== 1 || !/^\d+$/.test(matches[0].Id)) throw new Error('An owned TV fixture leaf differs.');
    expectedMedia.push({ id: matches[0].Id, name, path: sourcePath, type, scope: 'tv-browse-only' });
  }
}

async function apiRead(route, token, report, label, discovery = false) {
  await pinTarget();
  if (!route.startsWith('/emby/') || route.includes('?') || route.includes('#')) throw new Error('AV state observation left its fixed read scope.');
  const url = new URL(route, ORIGIN);
  if (discovery) {
    if (!goby || label !== 'owned-media-catalog' || route !== '/emby/Users/' + setup.account_id + '/Items') {
      throw new Error('AV catalog discovery left its fixed read scope.');
    }
    for (const [key, value] of Object.entries({ Recursive: 'true', IncludeItemTypes: 'Movie,Audio,Series,Season,Episode', Fields: 'Path', Limit: '64' })) {
      url.searchParams.set(key, value);
    }
  }
  const response = await new Promise((resolve, reject) => {
    const request = http.get(url, { headers: { Accept: 'application/json', 'X-Emby-Token': token } }, incoming => {
      const chunks = []; let total = 0;
      incoming.on('data', chunk => {
        total += chunk.length;
        if (total > 2 * 1024 * 1024) {
          reject(new Error('AV state observation exceeded its limit.'));
          incoming.destroy(); request.destroy(); return;
        }
        chunks.push(chunk);
      });
      incoming.on('error', () => reject(new Error('AV state response failed.')));
      incoming.on('aborted', () => reject(new Error('AV state response was aborted.')));
      incoming.on('close', () => { if (!incoming.complete) reject(new Error('AV state response closed before completion.')); });
      incoming.on('end', () => {
        try { resolve({ status: incoming.statusCode, data: JSON.parse(Buffer.concat(chunks).toString('utf8')) }); }
        catch { reject(new Error('AV state response was not bounded JSON.')); }
      });
    });
    request.setTimeout(10000, () => request.destroy(new Error('AV state observation timed out.')));
    request.on('error', () => reject(new Error('AV state observation failed.')));
  });
  report.fixture_state_reads.push({ label, method: 'GET', status: response.status, is_ui_request: false,
    query_field_names: [...url.searchParams.keys()] });
  if (response.status !== 200) throw new Error('AV state observation was not accepted.');
  await pinTarget();
  return response.data;
}

async function discoverGobyMedia(owned, report) {
  const catalog = await apiRead('/emby/Users/' + owned.userId + '/Items', owned.token, report, 'owned-media-catalog', true);
  if (!Array.isArray(catalog.Items) || catalog.Items.length > 64 || !Number.isSafeInteger(catalog.TotalRecordCount) ||
      catalog.TotalRecordCount < catalog.Items.length || catalog.TotalRecordCount > 64) {
    throw new Error('The owned synthetic catalog exceeded its fixed scope.');
  }
  const specifications = [
    ['Movie', ['M3e Client Movie'], 'Movies/M3e Client Movie.mp4', 'movie'],
    ['Audio', [goby.music?.tracks.mp3.name ?? 'M3e Client Audio'], 'Music/M3e Client Audio.mp3', 'mp3'],
    ['Audio', [goby.music?.tracks.flac.name ?? 'M3e Client Audio'], 'Music/M3e Client Audio.flac', 'flac'],
  ];
  if (options.check === 'tv') specifications.push(
    ['Series', ['M3e Client Series'], 'TV/M3e Client Series', 'tv-browse-only'],
    ['Season', ['Season 1', 'Season 01'], 'TV/M3e Client Series/Season 01', 'tv-browse-only'],
    ['Season', ['Season 2', 'Season 02'], 'TV/M3e Client Series/Season 02', 'tv-browse-only'],
    ['Episode', ['Episode 1-1'], 'TV/M3e Client Series/Season 01/M3e Client Series S01E01.mp4', 'tv-browse-only'],
    ['Episode', ['Episode 1-2'], 'TV/M3e Client Series/Season 01/M3e Client Series S01E02.mp4', 'tv-browse-only'],
    ['Episode', ['Episode 2-1'], 'TV/M3e Client Series/Season 02/M3e Client Series S02E01.mp4', 'tv-browse-only']);
  for (const [type, names, suffix, profile] of specifications) {
    const sourcePath = '/opt/goby-fixtures/client-m3e/' + suffix;
    const scanned = goby.music?.tracks[profile];
    if (scanned && (scanned.path !== sourcePath || scanned.container !== profile || names.length !== 1 || names[0] !== scanned.name)) {
      throw new Error('The scanned audio mapping differs from its fixed fixture path.');
    }
    const matches = catalog.Items.filter(item => item.Type === type && item.Path === sourcePath && names.includes(item.Name));
    if (matches.length !== 1 || typeof matches[0].Id !== 'string' || !/^[a-f0-9-]{1,64}$/i.test(matches[0].Id) ||
        (scanned && matches[0].Id !== scanned.id)) {
      throw new Error('An observed Goby item differs from the owned synthetic whitelist.');
    }
    expectedMedia.push({ id: matches[0].Id, name: matches[0].Name, path: sourcePath, type, profile,
      ...(scanned ? { parentId: scanned.parentId } : {}),
      scope: profile === 'tv-browse-only' ? profile : 'av-media' });
  }
  report.goby_catalog_observation = { is_ui_acceptance: false, catalog_total: catalog.TotalRecordCount,
    items: expectedMedia.map(item => ({ id: item.id, name: item.name, profile: item.profile, path: item.path })),
    title_policy: goby.music ? 'Exact scanned item ID, name, parent, and physical path; UI row and actual media source identity remain independent acceptance gates'
      : 'Only pre-scan fixture names are accepted without the exact completed Music scan receipt' };
}

async function principal(context) {
  const saved = await context.storageState();
  if (saved.origins.length !== 1 || saved.origins[0].origin !== ORIGIN) throw new Error('The new browser state left its fixture origin.');
  const raw = saved.origins[0].localStorage.find(entry => entry.name === 'servercredentials3');
  const servers = JSON.parse(raw?.value ?? '{}').Servers;
  if (!Array.isArray(servers) || servers.length !== 1 || servers[0].Id !== owner.server_id || servers[0].Users?.length !== 1) {
    throw new Error('The new browser server credential scope differs.');
  }
  const user = servers[0].Users[0];
  if (user.UserId !== setup.account_id || typeof user.AccessToken !== 'string' || !user.AccessToken) throw new Error('The browser did not authenticate the dedicated new account.');
  return { token: user.AccessToken, userId: user.UserId };
}

async function readState(owned, report, label) {
  const user = await apiRead('/emby/Users/' + owned.userId, owned.token, report, `${label}-user-configuration`);
  if (user.Id !== setup.account_id || user.Name !== owner.username || user.Policy?.IsAdministrator !== false) throw new Error('The observed AV account identity differs.');
  const preferences = await apiRead('/emby/UserSettings/' + owned.userId, owned.token, report, `${label}-user-settings`);
  const items = [];
  for (const expected of expectedMedia) {
    const actual = await apiRead('/emby/Users/' + owned.userId + '/Items/' + expected.id, owned.token, report, `${label}-${expected.name}`);
    if (actual.Id !== expected.id || actual.Name !== expected.name || actual.Path !== expected.path ||
        (expected.type && actual.Type !== expected.type) || (expected.parentId && actual.ParentId !== expected.parentId)) {
      throw new Error('An observed item left the owned synthetic media whitelist.');
    }
    items.push({ id: expected.id, name: expected.name, scope: expected.scope ?? 'av-media', user_data: actual.UserData });
    if (options.check === 'diagnose-album' && expected.name === 'M3e Client Movie') {
      const safeString = value => typeof value !== 'string' ? null : value.slice(0, 256)
        .replace(/(?:https?|wss?|file|blob|data):[^\s"'<>]+/gi, '[redacted URL]')
        .replace(/\b[0-9a-f]{16,}\b|\b[A-Za-z0-9_-]{40,}\b/gi, '[redacted opaque value]');
      const streams = value => !Array.isArray(value) ? { type: value === null ? 'null' : typeof value, streams: [] } : { type: 'array', array_length: value.length,
        streams: value.filter(stream => stream && typeof stream === 'object' && stream.Type === 'Subtitle').slice(0, 8).map(stream => {
          const view = { field_presence: {}, field_types: {} };
          for (const key of ['Index', 'Codec', 'Title', 'DisplayTitle', 'DisplayLanguage', 'Language', 'IsExternal', 'IsTextSubtitleStream', 'IsForced', 'IsHearingImpaired']) {
            view.field_presence[key] = Object.hasOwn(stream, key);
            view.field_types[key] = stream[key] === null ? 'null' : typeof stream[key];
            view[key] = typeof stream[key] === 'string' ? safeString(stream[key]) :
              ['number', 'boolean'].includes(typeof stream[key]) ? stream[key] : null;
          }
          view.delivery_url_present = Object.hasOwn(stream, 'DeliveryUrl');
          view.delivery_url_type = stream.DeliveryUrl === null ? 'null' : typeof stream.DeliveryUrl;
          view.delivery_url_nonempty = typeof stream.DeliveryUrl === 'string' && Boolean(stream.DeliveryUrl);
          return view;
        }) };
      (report.movie_subtitle_dto_diagnostics ??= []).push({ label, diagnostic_only: true, is_ui_request: false,
        source: 'The existing owned item-detail state read; no additional request',
        media_streams: streams(actual.MediaStreams), media_sources: Array.isArray(actual.MediaSources) ?
          actual.MediaSources.slice(0, 4).map(source => streams(source.MediaStreams)) : [] });
    }
  }
  return { user_id: setup.account_id, items,
    configuration_sha256: sha(JSON.stringify(stable(user.Configuration))),
    policy_sha256: sha(JSON.stringify(stable(user.Policy))),
    user_settings_sha256: sha(JSON.stringify(stable(preferences))) };
}

async function readBrowseMedia(page) {
  return page.locator('audio,video').evaluateAll(elements => elements.map(element => ({
    tag: element.tagName.toLowerCase(), paused: element.paused,
    current_time: Number.isFinite(element.currentTime) ? element.currentTime : null,
    ready_state: element.readyState, network_state: element.networkState,
  })));
}

function finalizeEvidence(report) {
  if (report.tv_browse_state_proof) {
    const playback = report.requests.filter(entry => entry.origin === 'target' &&
      (/\/(?:Playing|Progress|Stopped)(?:\/|$)/i.test(entry.route) ||
        /\/(?:Videos|Audio)\/[^/]+\/(?:stream|universal|original|master|hls|main)(?:[./]|$)/i.test(entry.route)));
    report.tv_browse_state_proof.playback_request_count = playback.length;
    report.tv_browse_state_proof.playback_metadata_request_count = report.requests.filter(entry =>
      entry.origin === 'target' && /\/PlaybackInfo\/?$/i.test(entry.route)).length;
    report.tv_browse_state_proof.event_scope = 'Complete observed browser request set after browser shutdown';
    report.tv_browse_state_proof.request_overflow = report.request_overflow;
    if (playback.length || report.request_overflow || !report.tv_browse_state_proof.all_observed_item_user_data_unchanged ||
        !report.tv_browse_state_proof.observed_media_elements_inactive) throw new Error('The final read-only TV boundary differs.');
  }
  if (report.av_preferences_comparison && Object.values(report.av_preferences_comparison).some(value => value !== true)) {
    throw new Error('The observed AV preferences changed.');
  }
}

try {
  const result = await runGuardedAV({ url: ORIGIN + '/web/index.html', credentialsPath: CREDENTIALS,
    assertTarget: pinTarget, finalizeReport: finalizeEvidence,
    runtimeExceptions: options.check === 'diagnose-album',
    output: options.output, workflow: async context => {
      const { page, report, snapshot } = context;
      report.av_scope = { backend: options.backend, check: options.check, account_marker: MARKER, account_id: setup.account_id,
        diagnostic_only: diagnosticOnly,
        history_policy: 'Retain legitimate history in the newly created dedicated account; existing fixture users are not changed',
        fixture_setup_is_client_acceptance: false, state_reads_are_client_acceptance: false };
      report.driver_source_sha256 = driverSourceHashes;
      if (diagnosticOnly) {
        report.diagnostic_only = true;
        report.timing_not_acceptance = true;
        report.mode = 'guarded-original-client-contract-diagnostic';
      }
      if (goby) report.goby_fixture_evidence = goby.evidence;
      report.fixture_state_reads = [];
      const owned = await principal(context.context);
      if (goby) await discoverGobyMedia(owned, report);
      report.av_state_before = await readState(owned, report, 'before');
      if (options.check === 'tv') report.tv_browse_media_before = await readBrowseMedia(page);
      let workflowError = null;
      try {
        if (options.check === 'browse-audio') {
          const album = page.locator('.card button.cardTextActionButton[data-action="link"],.cardBox button.cardTextActionButton[data-action="link"]')
            .filter({ hasText: ALBUM }).filter({ visible: true });
          await album.first().waitFor({ state: 'visible', timeout: 20000 });
          if (await album.count() !== 1 || (await album.innerText()).trim() !== ALBUM) throw new Error('The visible album card is missing or ambiguous.');
          await album.click({ timeout: 8000 });
          await page.waitForTimeout(700);
          await snapshot('audio-album-browse-only');
          report.audio_browse_controls = await page.locator('button:visible,a:visible,[role="row"]:visible').evaluateAll(elements =>
            elements.slice(0, 100).map(element => ({ tag: element.tagName.toLowerCase(), label: element.getAttribute('aria-label'),
              title: element.getAttribute('title'), class: typeof element.className === 'string' ? element.className : null,
              text: (element.innerText ?? '').trim().slice(0, 200) })));
        } else if (options.check === 'diagnose-subtitles') {
          const movie = expectedMedia.find(item => item.name === 'M3e Client Movie');
          await runSubtitleContractDiagnostics({ ...context, movieId: movie.id, userId: owned.userId });
          if (!goby) await runAlbumDiagnostics({ ...context, album: ALBUM, albumId: '13', userId: owned.userId,
            referenceMusicNavigation: true });
        } else if (options.check === 'diagnose-album') {
          await runAlbumDiagnostics({ ...context, album: ALBUM,
            albumId: goby ? '002c2ea2c77769ae9b0b84b6e788998b' : '13', userId: owned.userId });
        } else if (options.check === 'tv') {
          await runTVBrowseUI({ ...context, library: goby?.tvLibrary ?? 'M3e Reference TV' });
        } else if (options.check === 'subtitles') {
          const movie = expectedMedia.find(item => item.name === 'M3e Client Movie');
          await runSubtitleUI({ ...context, movieId: movie.id });
        } else if (options.check === 'diagnose-audio') {
          const item = expectedMedia.find(item => item.profile === 'mp3');
          const diagnostics = createAudioReportDiagnostics({ context: context.context, target: context.target, ownedItemId: item.id });
          report.audio_report_diagnostics = diagnostics.report;
          try {
            await runAudioUI({ ...context, album: ALBUM, track: 'M3e MP3',
              itemIdentity: { id: item.id, name: item.name, container: item.profile },
              homeLibrary: { id: goby.evidence.libraries.music, name: 'M3e Client Music' }, albumId: goby.music.album.id });
          } finally {
            try { await diagnostics.drain(); }
            finally { await diagnostics.dispose(); }
          }
        } else {
          const item = goby ? expectedMedia.find(item => item.profile === options.check) : null;
          await runAudioUI({ ...context, album: ALBUM, track: options.check === 'flac' ? 'M3e FLAC' : 'M3e MP3',
            itemIdentity: item ? { id: item.id, name: item.name, container: item.profile } : undefined,
            homeLibrary: goby ? { id: goby.evidence.libraries.music, name: 'M3e Client Music' } : undefined,
            albumId: goby?.music?.album.id });
        }
      } catch (error) { workflowError = error; }
      if (options.check === 'tv') report.tv_browse_media_after = await readBrowseMedia(page);
      report.av_state_after = await readState(owned, report, 'after');
      report.av_preferences_comparison = {
        configuration_unchanged: report.av_state_before.configuration_sha256 === report.av_state_after.configuration_sha256,
        policy_unchanged: report.av_state_before.policy_sha256 === report.av_state_after.policy_sha256,
        user_settings_unchanged: report.av_state_before.user_settings_sha256 === report.av_state_after.user_settings_sha256,
      };
      if (Object.values(report.av_preferences_comparison).some(value => value !== true)) {
        workflowError ??= new Error('The observed AV preferences changed.');
      }
      if (options.check === 'tv') {
        const playbackRequests = report.requests.filter(entry => entry.origin === 'target' &&
          (/\/(?:Playing|Progress|Stopped)(?:\/|$)/i.test(entry.route) ||
            /\/(?:Videos|Audio)\/[^/]+\/(?:stream|universal|original|master|hls|main)(?:[./]|$)/i.test(entry.route)));
        const metadataRequests = report.requests.filter(entry => entry.origin === 'target' && /\/PlaybackInfo\/?$/i.test(entry.route));
        const mediaInactive = [...report.tv_browse_media_before, ...report.tv_browse_media_after]
          .every(element => element.paused && element.current_time === 0);
        report.tv_browse_state_proof = {
          all_observed_item_user_data_unchanged: JSON.stringify(stable(report.av_state_before.items)) === JSON.stringify(stable(report.av_state_after.items)),
          observed_item_count: report.av_state_before.items.length,
          observed_tv_item_count: report.av_state_before.items.filter(item => item.scope === 'tv-browse-only').length,
          playback_request_count: playbackRequests.length,
          playback_metadata_request_count: metadataRequests.length,
          playback_metadata_is_start_evidence: false,
          observed_media_elements_inactive: mediaInactive,
          independent_state_reads_are_ui_acceptance: false,
        };
        if (!report.tv_browse_state_proof.all_observed_item_user_data_unchanged || playbackRequests.length || !mediaInactive) {
          workflowError ??= new Error('The read-only TV browse boundary changed.');
        }
      }
      owned.token = null;
      if (workflowError) throw workflowError;
    } });
  process.stdout.write(JSON.stringify({ result: diagnosticOnly ? 'diagnostic_completed' : 'completed', backend: options.backend, check: options.check,
    audio_outcome: result.audio_flow?.outcome, subtitle_outcome: result.subtitle_flow?.outcome, tv_outcome: result.tv_browse?.outcome,
    album_diagnostics: result.album_diagnostics?.outcome,
    subtitle_diagnostics: result.subtitle_contract_diagnostics?.outcome,
    session_proof: result.session_proof?.outcome, output: options.output }) + '\n');
} catch (error) {
  process.stdout.write(JSON.stringify({ result: 'blocked', backend: options.backend, check: options.check, phase: error.phase ?? 'av_state_guard',
    audio_phase: error.report?.audio_flow?.phase, subtitle_phase: error.report?.subtitle_flow?.phase, tv_phase: error.report?.tv_browse?.phase,
    session_proof: error.report?.session_proof?.outcome, output: options.output }) + '\n');
  process.exitCode = 1;
}
