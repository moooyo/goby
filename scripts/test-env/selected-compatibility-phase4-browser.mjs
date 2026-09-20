#!/usr/bin/env node
/** Final native configuration and explicitly owned reference-client acceptance. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';

const phases = ['account-pin', 'runtime-saved', 'client-capabilities', 'audio-playing', 'remote-paused',
  'session-reconnected', 'audio-stopped', 'notifications-configured', 'notification-retried',
  'notification-offline', 'notification-hidden', 'notification-rotated', 'notification-new-target',
  'notification-revoked', 'notifications-disabled', 'listener-restarted', 'restarted-csrf', 'cleanup'];
const checks = ['AccountPin', 'RuntimeSaved', 'ClientCapabilities', 'AudioPlaying', 'RemotePaused',
  'SessionReconnected', 'AudioStopped', 'NotificationsConfigured', 'NotificationRetried',
  'NotificationOffline', 'NotificationHidden', 'NotificationRotated', 'NotificationNewTarget',
  'NotificationRevoked', 'NotificationsDisabled', 'ListenerRestarted', 'RestartedCSRF', 'Cleanup'];
const result = { Marker: 'goby-selected-phase4-browser-result-v1', RunId: '', Complete: false,
  Stages: [], Checks: Object.fromEntries(checks.map(value => [value, false])), HTTP: [], Screenshots: [],
  PageErrors: 0, PageErrorDetails: [], ForeignRequests: 0, RetiredGETReads: 0, NotificationEvidence: {},
  FailurePhase: null, FailureOperation: null, ConsumerKind: 'owned reference client plus independent GobyWebhookV1 receiver and console consumer',
  OriginalClientUsed: false, ArbitraryThirdPartyParityClaimed: false, OSToastOrHumanReadClaimed: false };
const fail = code => { const error = new Error(code); error.safeCode = code; throw error; };
const requireThat = (condition, code) => { if (!condition) fail(code); };
const sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
let fixture, browser, context, page, targetContext, targetPage, controllerContext, controllerPage, diagnosticPage, deadline;
let currentPhase = 'admission', currentOperation = 'read-context';
let settings, registration, notificationSettings, playbackScope;
const receivedEvents = new Set();

async function privateJSON(filename, maximum = 1 << 20) {
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = await handle.stat();
    requireThat(stat.isFile() && stat.uid === process.getuid() && stat.nlink === 1 &&
      (stat.mode & 0o777) === 0o600 && stat.size > 0 && stat.size <= maximum, 'private_json_required');
    return JSON.parse(await handle.readFile('utf8'));
  } finally { await handle.close(); }
}
async function writeJSON(filename, value) {
  requireThat(path.dirname(filename) === fixture.ArtifactsDir, 'artifact_parent_mismatch');
  const temporary = `${filename}.new`;
  await fs.writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`, { flag: 'wx', mode: 0o600 });
  await fs.rename(temporary, filename);
}
async function screenshot(name, bestEffort = false) {
  try {
    const target = diagnosticPage && !diagnosticPage.isClosed() ? diagnosticPage : page;
    requireThat(target && !target.isClosed() && new URL(target.url()).origin === fixture.BaseURL, 'owned_page_required');
    const masks = [target.locator('input,textarea,[contenteditable]')];
    for (const secret of [fixture.AdminPassword, fixture.ViewerPassword, fixture.ProfilePin, fixture.ReceiverCredential, fixture.TargetTokenA, fixture.TargetTokenB])
      if (secret) masks.push(target.getByText(secret, { exact: false }));
    const bytes = await target.screenshot({ fullPage: false, animations: 'disabled', mask: masks, timeout: 2500 });
    requireThat(bytes.length <= (8 << 20), 'screenshot_size_limit');
    const filename = `${name}.png`;
    await fs.writeFile(path.join(fixture.ArtifactsDir, filename), bytes,
      { flag: 'wx', mode: 0o600, signal: AbortSignal.timeout(1000) });
    result.Screenshots.push(filename);
  } catch (error) {
    if (!bestEffort) throw error;
    result.FailureScreenshotUnavailable = true;
  }
}
async function operation(name, action) {
  currentOperation = name;
  try { return await action(); }
  catch (error) {
    if (error.phase4Captured) throw error;
    result.FailurePhase = currentPhase; result.FailureOperation = name;
    result.ErrorKind = ['Error', 'TimeoutError', 'AggregateError', 'TypeError'].includes(error.name) ? error.name : 'OperationError';
    if (!error.safeCode) error.safeCode = `native_${name.replaceAll('-', '_')}_failed`;
    await screenshot(`failure-${currentPhase}-${name}`, true);
    error.phase4Captured = true;
    throw error;
  }
}
async function stage(phase, extra = {}) {
  requireThat(phase === phases[result.Stages.length], 'stage_order_mismatch');
  currentPhase = phase;
  await writeJSON(path.join(fixture.ArtifactsDir, `stage-${phase}-request.json`), { RunId: fixture.RunId, Phase: phase, ...extra });
  const until = Date.now() + 45000;
  while (Date.now() < until) {
    let acknowledgement;
    try { acknowledgement = await privateJSON(path.join(fixture.ArtifactsDir, `stage-${phase}-database.json`)); }
    catch (error) { if (error.code !== 'ENOENT') throw error; await sleep(100); continue; }
    requireThat(acknowledgement.Marker === 'goby-selected-phase4-stage-database-v1' &&
      acknowledgement.RunId === fixture.RunId && acknowledgement.Phase === phase, 'database_acknowledgement_binding_failed');
    if (acknowledgement.Failed === true) {
      result.DatabaseFailure = { Phase: phase, Code: 'database_stage_verification_failed' };
      fail('database_stage_verification_failed');
    }
    requireThat(acknowledgement.Observed === true && acknowledgement.Complete === true && acknowledgement.ExpectedFilesVerified === true, 'database_acknowledgement_failed');
    result.Stages.push({ Phase: phase, State: 'complete' });
    await writeJSON(fixture.ResultPath, result);
    return acknowledgement;
  }
  fail('database_acknowledgement_timeout');
}
async function responseFor(method, pathname, action, expected = 200, json = true) {
  const requests = new Set(), responses = [];
  const matches = request => request.method() === method && new URL(request.url()).origin === fixture.BaseURL &&
    new URL(request.url()).pathname === pathname;
  const requested = request => { if (matches(request)) requests.add(request); };
  const received = response => {
    if (!requests.has(response.request())) return;
    // Read at the response event, before React can retire an automatic refresh.
    const body = json ? response.body().then(value => ({ value }), error => ({ error })) : Promise.resolve({ value: null });
    responses.push({ response, body });
  };
  page.on('request', requested); page.on('response', received);
  const until = Date.now() + 20000;
  async function bounded(promise) {
    let timer;
    try { return await Promise.race([promise, new Promise((_, reject) => {
      timer = setTimeout(() => { const error = new Error('native_response_timeout'); error.safeCode = 'native_response_timeout'; reject(error); }, Math.max(1, until - Date.now()));
    })]); } finally { clearTimeout(timer); }
  }
  try {
    await action();
    while (Date.now() < until) {
      const captured = responses.shift();
      if (!captured) { await sleep(25); continue; }
      const response = captured.response;
      requireThat(response.status() === expected, 'unexpected_http_status');
      const body = await bounded(captured.body);
      if (body.error) {
        await bounded(response.finished());
        if (method === 'GET' && response.request().failure()?.errorText === 'net::ERR_ABORTED') {
          result.RetiredGETReads += 1;
          continue;
        }
        throw body.error;
      }
      if (json) requireThat(/^application\/json(?:;|$)/i.test(response.headers()['content-type'] ?? ''), 'native_json_response_required');
      if (result.HTTP.length < 160) result.HTTP.push({ Phase: currentPhase, Method: method, Path: pathname, Status: response.status() });
      return { Status: response.status(), Body: json ? JSON.parse(body.value.toString('utf8')) : null,
        Submitted: method === 'GET' || method === 'DELETE' ? null : response.request().postDataJSON() };
    }
    fail('native_response_timeout');
  } finally { page.off('request', requested); page.off('response', received); }
}


async function guardedContext() {
  const created = await browser.newContext({ viewport: { width: 1440, height: 1080 }, locale: 'en-US', serviceWorkers: 'block', acceptDownloads: false });
  await created.route('**/*', async route => {
    const url = new URL(route.request().url());
    if ((!url.username && !url.password && url.origin === fixture.BaseURL) || ['data:', 'blob:'].includes(url.protocol)) await route.continue();
    else { result.ForeignRequests += 1; await route.abort('blockedbyclient'); }
  });
  await created.routeWebSocket('**/*', socket => {
    const url = new URL(socket.url()); if (url.protocol === 'ws:') url.protocol = 'http:';
    if (!url.username && !url.password && url.origin === fixture.BaseURL) socket.connectToServer();
    else { result.ForeignRequests += 1; socket.close(); }
  });
  created.on('page', opened => opened.on('pageerror', error => {
    result.PageErrors += 1;
    if (result.PageErrorDetails.length < 32) result.PageErrorDetails.push({ Phase: currentPhase, Operation: currentOperation,
      Name: ['Error', 'TypeError', 'ReferenceError', 'RangeError', 'SyntaxError'].includes(error.name) ? error.name : 'UnknownError' });
  }));
  return created;
}

async function waitEnabled(locator, code) {
  await locator.waitFor({ state: 'attached' });
  const until = Date.now() + 20000;
  while (Date.now() < until) { if (await locator.isEnabled()) return; await sleep(25); }
  fail(code);
}
async function select(parent, label, option) {
  await parent.getByRole('combobox', { name: label, exact: true }).click();
  await page.getByRole('option', { name: option, exact: true }).click();
}
async function nativeLogin() {
  diagnosticPage = page;
  await page.goto(`${fixture.BaseURL}/admin/`);
  await page.getByRole('heading', { name: 'Sign in to Goby', exact: true }).waitFor();
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill(fixture.AdminName);
  await page.locator('input#account-password[name="Password"]').fill(fixture.AdminPassword);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('navigation', { name: 'Administration', exact: true }).waitFor();
}
async function setProfilePin() {
  await page.goto(`${fixture.BaseURL}/admin/users`);
  await page.getByRole('button', { name: `Manage ${fixture.ViewerName}`, exact: true }).click();
  await page.getByRole('dialog', { name: 'Manage user', exact: true }).getByRole('button', { name: 'Manage local credentials', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Local credentials', exact: true });
  for (const name of ['New profile PIN', 'Confirm profile PIN']) await dialog.getByLabel(name, { exact: true }).fill(fixture.ProfilePin);
  await dialog.getByRole('button', { name: 'Save local credentials', exact: true }).click();
  const saved = await responseFor('PUT', `/admin/v1/users/${fixture.ViewerId}/local-credentials`, () =>
    page.getByRole('dialog', { name: 'Save local credentials?', exact: true }).getByRole('button', { name: 'Save profile PIN', exact: true }).click());
  requireThat(saved.Body.Credentials.HasProfilePin === true && saved.Body.CurrentSessionRevoked === false &&
    !Object.hasOwn(saved.Body.Credentials, 'ProfilePin'), 'native_profile_pin_status_mismatch');
  return String(saved.Body.Credentials.Revision);
}
async function loadSettings() {
  diagnosticPage = page;
  const response = await responseFor('GET', '/admin/v1/settings', () => page.goto(`${fixture.BaseURL}/admin/settings`));
  await page.getByRole('region', { name: 'HTTP listener', exact: true }).waitFor();
  await waitEnabled(page.getByRole('button', { name: 'Reload settings', exact: true }), 'native_settings_load_not_settled');
  return response.Body;
}
async function saveRuntime(afterRestart = false) {
  const loaded = await loadSettings();
  if (afterRestart) {
    requireThat(loaded.Revision === settings.Revision && loaded.Runtime.Network.Desired.HttpPort === fixture.NextPort &&
      loaded.Runtime.Network.Active?.HttpPort === fixture.NextPort && loaded.Runtime.Network.Active.Configured.HttpPort === fixture.NextPort &&
      loaded.Runtime.Network.RestartRequired === false && loaded.Runtime.Effective.Threads === 2 &&
      loaded.Runtime.Effective.H264.Preset === 'fast' && loaded.Runtime.Effective.H264.RateControl === 'capped_crf' && loaded.Runtime.Effective.H264.CRF === 23,
    'restarted_runtime_does_not_match_saved_configuration');
    result.RestartedNetwork = loaded.Runtime.Network;
  } else {
    const listener = page.getByRole('region', { name: 'HTTP listener', exact: true });
    await listener.getByRole('switch', { name: 'Use deployment default for HTTP port', exact: true }).uncheck();
    await listener.getByRole('textbox', { name: 'HTTP port', exact: true }).fill(String(fixture.NextPort));
    const hardware = page.getByRole('region', { name: 'Hardware acceleration', exact: true });
    await hardware.getByRole('switch', { name: 'Use deployment default for hardware acceleration', exact: true }).uncheck();
    await select(hardware, 'Video decoding', 'CPU'); await select(hardware, 'Video encoding', 'CPU');
    await select(hardware, 'AMD device', 'No device selected');
    const h264 = page.getByRole('region', { name: 'H.264 CPU encoding', exact: true });
    await h264.getByRole('switch', { name: 'Use deployment default for H.264 CPU encoding', exact: true }).uncheck();
    await select(h264, 'H.264 CPU preset', 'fast'); await select(h264, 'H.264 rate control', 'Capped CRF');
    await h264.getByRole('textbox', { name: 'H.264 CRF', exact: true }).fill('23');
  }
  const threads = page.getByRole('region', { name: 'Threads per job', exact: true });
  await threads.getByRole('switch', { name: 'Use deployment default for threads per job', exact: true }).uncheck();
  await threads.getByRole('textbox', { name: 'Threads per job', exact: true }).fill(afterRestart ? '3' : '2');
  const saved = await responseFor('PUT', '/admin/v1/settings', () => page.getByRole('button', { name: 'Save settings', exact: true }).click());
  requireThat(saved.Submitted.Revision === loaded.Revision && BigInt(saved.Body.Revision) > BigInt(loaded.Revision) &&
    saved.Body.Runtime.Effective.Threads === (afterRestart ? 3 : 2), 'native_runtime_cas_or_threads_mismatch');
  await page.getByText('Settings saved.', { exact: true }).waitFor();
  await waitEnabled(page.getByRole('button', { name: 'Reload settings', exact: true }), 'native_runtime_save_not_settled');
  requireThat(!await page.getByRole('button', { name: 'Save settings', exact: true }).isEnabled(), 'native_runtime_remains_dirty');
  if (!afterRestart) requireThat(saved.Body.Runtime.Network.Desired.HttpPort === fixture.NextPort &&
    saved.Body.Runtime.Network.Active.HttpPort === Number(new URL(fixture.BaseURL).port) && saved.Body.Runtime.Network.RestartRequired === true,
  'desired_listener_was_confused_with_active_listener');
  settings = saved.Body;
  result[afterRestart ? 'RuntimeAfterFreshCSRFSave' : 'RuntimeSaved'] = { Revision: settings.Revision,
    Network: settings.Runtime.Network, Effective: settings.Runtime.Effective };
  await screenshot(afterRestart ? 'native-restarted-settings' : 'native-desired-and-active-listener');
}
async function configureNotifications(enabled, initial = false) {
  diagnosticPage = page;
  const loaded = await responseFor('GET', '/admin/v1/notifications', () => page.goto(`${fixture.BaseURL}/admin/notifications`));
  await waitEnabled(page.getByRole('button', { name: 'Reload notifications', exact: true }), 'notification_settings_load_not_settled');
  if (initial) {
    await page.getByRole('textbox', { name: 'Receiver endpoint', exact: true }).fill(fixture.NotificationEndpoint);
    await page.getByRole('textbox', { name: 'Allowed private networks', exact: true }).fill('127.0.0.1/32');
    await select(page, 'Receiver credential action', 'Replace credential');
    await page.getByLabel('New receiver credential', { exact: true }).fill(fixture.ReceiverCredential);
  }
  await page.getByRole('switch', { name: 'Enable notifications', exact: true }).setChecked(enabled);
  const saved = await responseFor('PUT', '/admin/v1/notifications', () => page.getByRole('button', { name: 'Save notifications', exact: true }).click());
  requireThat(saved.Submitted.Revision === loaded.Body.Revision && saved.Body.Enabled === enabled && saved.Body.HasReceiverCredential === true &&
    !Object.hasOwn(saved.Body, 'ReceiverCredential') && saved.Body.Endpoint === fixture.NotificationEndpoint &&
    ['CatalogInvalidated', 'UserDataInvalidated'].every(kind => saved.Body.SupportedEvents.includes(kind)), 'notification_configuration_cas_or_secrecy_failed');
  await page.getByText('Notification settings saved. Receiver credentials are never returned.', { exact: true }).waitFor();
  await waitEnabled(page.getByRole('button', { name: 'Reload notifications', exact: true }), 'native_notification_save_not_settled');
  requireThat(!await page.getByRole('button', { name: 'Save notifications', exact: true }).isEnabled(), 'native_notification_settings_remain_dirty');
  notificationSettings = saved.Body;
  await screenshot(enabled ? 'native-notification-receiver-configured' : 'native-notification-transport-disabled');
}
async function editHiddenSource() {
  diagnosticPage = page;
  await page.goto(`${fixture.BaseURL}/admin/libraries/${fixture.LibraryId}/items`);
  await page.getByRole('button', { name: `Edit metadata for ${fixture.HiddenMovieName}`, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: /^Edit metadata/ });
  await dialog.locator('#metadata-Overview-input').fill('Phase 4 hidden-only notification source change');
  const saved = await responseFor('PUT', `/admin/v1/items/${fixture.HiddenMovieId}/metadata`, () => dialog.getByRole('button', { name: 'Save changes', exact: true }).click());
  await dialog.getByText('Metadata changes saved.', { exact: true }).waitFor();
  requireThat(saved.Body.Item.Id === fixture.HiddenMovieId && saved.Body.Effective.Overview === 'Phase 4 hidden-only notification source change', 'hidden_metadata_mutation_not_observed');
  return String(saved.Body.Revision);
}

async function installReferenceClient(controller = false) {
  const created = await guardedContext(), opened = await created.newPage();
  const response = await opened.goto(`${fixture.BaseURL}/__selected-phase4-consumer`);
  requireThat(response?.status() === 200, 'owned_reference_document_missing');
  const identity = await opened.evaluate(async input => {
    const device = `phase4-${input.Controller ? 'controller' : 'target'}-${input.RunId}`;
    const metadata = { 'X-Emby-Client': 'Goby Phase 4 Reference Client', 'X-Emby-Device-Id': device,
      'X-Emby-Device-Name': 'Owned Chromium', 'X-Emby-Client-Version': '1' };
    let token = '', profilePin = '', unlocked = input.Controller, wrongPins = 0, sessionId = '';
    let socket, socketGeneration = 0, latestSession = null, audioContext, analyser, scope, stopped;
    let failure = '', playing = false;
    const http = [], socketHistory = [], commands = [], samples = [];
    const audio = document.querySelector('#audio');
    const fetchOwned = async (target, method = 'GET', body) => {
      const url = new URL(target, input.BaseURL);
      if (url.origin !== input.BaseURL || url.username || url.password || url.hash) throw new Error('reference_origin_mismatch');
      const headers = { ...metadata }; if (token) headers['X-Emby-Token'] = token;
      if (body !== undefined) headers['Content-Type'] = 'application/json';
      const response = await fetch(url.href, { method, headers, body: body === undefined ? undefined : JSON.stringify(body),
        cache: 'no-store', signal: AbortSignal.timeout(20000) });
      if (http.length < 320) http.push({ Method: method, Path: url.pathname, Status: response.status });
      return response;
    };
    const request = async (target, method = 'GET', body) => {
      const response = await fetchOwned(target, method, body);
      return { Status: response.status, Body: [202, 204].includes(response.status) ? null : await response.json() };
    };
    const requireUnlocked = () => { if (!unlocked) throw new Error('reference_profile_locked'); };
    const projectSession = session => ({ Id: session.Id, UserId: session.UserId, DeviceId: session.DeviceId,
      Client: session.Client, DeviceName: session.DeviceName, ApplicationVersion: session.ApplicationVersion, ServerId: session.ServerId,
      SupportsRemoteControl: session.SupportsRemoteControl, NowPlayingItemId: session.NowPlayingItem?.Id ?? null,
      PlayState: session.PlayState ? { IsPaused: session.PlayState.IsPaused, PositionTicks: session.PlayState.PositionTicks,
        MediaSourceId: session.PlayState.MediaSourceId, PlayMethod: session.PlayState.PlayMethod } : null });
    const sample = () => {
      const values = new Float32Array(analyser?.fftSize ?? 0); if (analyser) analyser.getFloatTimeDomainData(values);
      let sum = 0, peak = 0; for (const value of values) { sum += value * value; peak = Math.max(peak, Math.abs(value)); }
      return { At: performance.now(), Seconds: audio.currentTime, Paused: audio.paused, Ended: audio.ended, Seeking: audio.seeking,
        ReadyState: audio.readyState, ErrorCode: audio.error?.code ?? 0, AudioContextState: audioContext?.state ?? 'absent',
        AudioContextTime: audioContext?.currentTime ?? 0, RMS: values.length ? Math.sqrt(sum / values.length) : 0, Peak: peak, SampleCount: values.length };
    };
    const report = async suffix => {
      if (!scope) throw new Error('correlated_playback_scope_missing');
      const position = Math.round(audio.currentTime * 1e7);
      const response = await request(`/emby/Sessions/Playing${suffix}`, 'POST', { PlaySessionId: scope.PlaybackReference,
        SessionId: sessionId, ItemId: scope.AudioItemId, MediaSourceId: scope.MediaSourceId, PositionTicks: position,
        IsPaused: audio.paused, CanSeek: true, IsMuted: audio.muted, VolumeLevel: Math.round(audio.volume * 100), PlayMethod: 'Transcode' });
      if (response.Status !== 204) throw new Error('correlated_playback_report_failed');
      scope.PositionTicks = position; scope.IsPaused = audio.paused;
      return { PositionTicks: position, IsPaused: audio.paused, Status: response.Status };
    };
    const handleCommand = async envelope => {
      const command = envelope.Data;
      if (command?.Id !== sessionId || command.ControllingUserId !== input.ControllerUserId || !scope) throw new Error('remote_command_identity_mismatch');
      const before = sample();
      if (command.Command === 'Pause') audio.pause();
      else if (command.Command === 'Unpause') await audio.play();
      else throw new Error('unexpected_remote_command');
      const reported = await report('/Progress');
      await new Promise(resolve => setTimeout(resolve, 220));
      const after = sample();
      commands.push({ Command: command.Command, TargetSessionId: command.Id, ControllingUserId: command.ControllingUserId,
        MessageId: typeof envelope.MessageId === 'string' ? envelope.MessageId : null, Before: before, After: after, Report: reported,
        SocketGeneration: socketGeneration });
    };
    const closeSocket = async () => {
      if (!socket || socket.readyState === WebSocket.CLOSED) return;
      const closing = socket;
      await new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error('actual_socket_close_timeout')), 5000);
        closing.addEventListener('close', () => { clearTimeout(timer); resolve(); }, { once: true });
        if (closing.readyState === WebSocket.OPEN) closing.send(JSON.stringify({ MessageType: 'SessionsStop' }));
        closing.close(1000, 'Owned reference phase transition');
      });
    };
    const connectSocket = async () => {
      requireUnlocked(); await closeSocket(); latestSession = null;
      const generation = ++socketGeneration;
      const record = { Generation: generation, Opened: false, Closed: false, Snapshots: 0, CommandsBefore: commands.length };
      socketHistory.push(record);
      const url = new URL('/emby/socket', input.BaseURL); url.protocol = 'ws:'; url.searchParams.set('api_key', token);
      const opened = new WebSocket(url.href); socket = opened;
      opened.addEventListener('close', event => { record.Closed = true; record.CloseCode = event.code; });
      await new Promise((resolve, reject) => {
        let settled = false;
        const finish = error => { if (settled) return; settled = true; clearTimeout(timer); if (error) reject(error); else resolve(); };
        const timer = setTimeout(() => finish(new Error('actual_sessions_snapshot_timeout')), 20000);
        opened.addEventListener('open', () => { record.Opened = true; opened.send(JSON.stringify({ MessageType: 'SessionsStart', Data: '0,1000' })); });
        opened.addEventListener('error', () => { failure = 'actual_websocket_error'; finish(new Error(failure)); });
        opened.addEventListener('message', event => {
          try {
            const envelope = JSON.parse(event.data);
            if (envelope.MessageType === 'Sessions') {
              if (!Array.isArray(envelope.Data)) throw new Error('sessions_payload_invalid');
              const own = envelope.Data.find(value => value.Id === sessionId);
              if (own) { latestSession = { ...projectSession(own), SocketGeneration: generation, ObservedAt: performance.now() }; record.Snapshots++; finish(); }
            } else if (envelope.MessageType === 'Playstate') {
              void handleCommand(envelope).catch(error => { failure = /^[a-z_]+$/.test(error.message) ? error.message : 'remote_action_failed'; });
            }
          } catch { failure = 'websocket_payload_invalid'; finish(new Error(failure)); }
        });
      });
      return latestSession;
    };
    const stopAudio = async () => {
      if (!scope) return { NotStarted: true };
      if (stopped) return stopped;
      audio.pause(); const reported = await report('/Stopped');
      audio.removeAttribute('src'); audio.load();
      const retired = await request(`/emby/Videos/ActiveEncodings?DeviceId=${encodeURIComponent(device)}&PlaySessionId=${encodeURIComponent(scope.PlaybackReference)}`, 'DELETE');
      if (audioContext?.state !== 'closed') await audioContext?.close();
      playing = false;
      stopped = { ...scope, PositionTicks: reported.PositionTicks, IsPaused: true, StoppedStatus: reported.Status,
        EncodingsStatus: retired.Status, MediaDetached: !audio.getAttribute('src'), AudioContextState: audioContext?.state ?? 'absent' };
      return stopped;
    };
    document.querySelector('#unlock-profile').addEventListener('click', event => {
      event.preventDefault();
      const entry = document.querySelector('#profile-pin');
      if (entry.value === profilePin && /^[0-9]{4}$/.test(profilePin)) { unlocked = true; document.querySelector('#pin-status').textContent = 'Profile unlocked'; }
      else { wrongPins++; document.querySelector('#pin-status').textContent = 'Incorrect profile PIN'; }
      entry.value = '';
    });
    document.querySelector('#play-audio').addEventListener('click', () => {
      void (async () => {
        requireUnlocked(); if (scope) throw new Error('duplicate_audio_start');
        audioContext = new AudioContext(); analyser = audioContext.createAnalyser(); analyser.fftSize = 2048;
        audioContext.createMediaElementSource(audio).connect(analyser); analyser.connect(audioContext.destination); await audioContext.resume();
        const negotiated = await request(`/emby/Items/${encodeURIComponent(input.AudioItemId)}/PlaybackInfo`, 'POST', {
          UserId: input.UserId, StartTimeTicks: 0, EnableDirectPlay: false, EnableDirectStream: false, EnableTranscoding: true,
          AllowAudioStreamCopy: false, DeviceProfile: { TranscodingProfiles: [{ Type: 'Audio', Container: 'mp3', AudioCodec: 'mp3', Protocol: 'http', Context: 'Streaming' }] },
        });
        const body = negotiated.Body, source = body?.MediaSources?.[0];
        if (negotiated.Status !== 200 || body.ErrorCode || body.MediaSources?.length !== 1 || !source.SupportsTranscoding ||
          source.TranscodingSubProtocol !== 'http' || source.TranscodingContainer !== 'mp3') throw new Error('current_audio_admission_failed');
        const url = new URL(source.TranscodingUrl, input.BaseURL);
        if (url.origin !== input.BaseURL || url.searchParams.get('DeviceId') !== device || url.searchParams.get('PlaySessionId') !== body.PlaySessionId ||
          url.searchParams.get('MediaSourceId') !== source.Id || url.pathname !== `/emby/Audio/${input.AudioItemId}/stream.mp3`) throw new Error('current_audio_admission_scope_mismatch');
        // This is the documented independent client reference. Preserve the
        // negotiated source/output parameters and do not relabel the server ID.
        const reference = `phase4-client-${input.RunId}`;
        url.searchParams.set('PlaySessionId', reference);
        scope = { PlaybackReference: reference, NegotiatedPlaySessionId: body.PlaySessionId, TargetSessionId: sessionId,
          DeviceId: device, MediaSourceId: source.Id, AudioItemId: input.AudioItemId };
        audio.muted = false; audio.volume = .5; audio.src = url.href; await audio.play(); await report(''); playing = true;
        document.querySelector('#status').textContent = 'Actual correlated audio started';
      })().catch(error => { failure = /^[a-z_]+$/.test(error.message) ? error.message : 'actual_audio_start_failed'; });
    });
    document.querySelector('#resume-audio').addEventListener('click', () => {
      void audio.play().then(() => report('/Progress')).catch(() => { failure = 'actual_audio_resume_failed'; });
    });
    document.querySelector('#pause-audio').addEventListener('click', () => {
      audio.pause(); void report('/Progress').catch(() => { failure = 'actual_audio_pause_failed'; });
    });
    document.querySelector('#stop-audio').addEventListener('click', () => { void stopAudio().catch(() => { failure = 'actual_audio_stop_failed'; }); });
    window.gobyPhase4 = {
      async api(target, method = 'GET', body) { requireUnlocked(); return request(target, method, body); },
      pinState: () => ({ Unlocked: unlocked, WrongAttempts: wrongPins, AuthenticatedFirst: token !== '', ServerPinLoginAttempted: false }),
      connectSocket, closeSocket,
      state: () => ({ Scope: scope, Sample: sample(), Playing: playing, Stopped: stopped, Failure: failure, Commands: commands.slice(),
        LatestSession: latestSession, SocketGeneration: socketGeneration, SocketClosed: !socket || socket.readyState === WebSocket.CLOSED, SocketHistory: socketHistory.slice() }),
      async observeAudio() {
        const until = performance.now() + 15000;
        while (performance.now() < until) {
          if (failure) throw new Error(failure);
          const value = sample();
          if (playing && !value.Paused && !value.Seeking && !value.Ended && value.ReadyState >= 2 && value.RMS > .0001 && value.Peak > .001 && value.Seconds > 1) samples.push(value);
          if (samples.length >= 3 && value.Seconds >= 2 && value.Seconds - samples[0].Seconds > .25) {
            const reported = await report('/Progress');
            return { ...scope, ...reported, Samples: samples.slice(-8), ActualNonceNegotiation: true,
              NegotiatedServerIDRetainedSeparately: true, OutputConnectedToDestination: true, SyntheticSignalUsed: false };
          }
          await new Promise(resolve => setTimeout(resolve, 120));
        }
        throw new Error('actual_audio_output_timeout');
      },
      async reportCurrent() { return { ...scope, ...await report('/Progress') }; },
      http: () => http.slice(),
      async logout() { await closeSocket(); if (scope && !stopped) await stopAudio(); const response = await request('/emby/Sessions/Logout', 'POST'); token = ''; return response.Status; },
    };
    const login = await request('/emby/Users/AuthenticateByName', 'POST', { Username: input.Name, Pw: input.Password });
    if (login.Status !== 200 || login.Body.User?.Id !== input.UserId || !login.Body.AccessToken) throw new Error('reference_authentication_failed');
    token = login.Body.AccessToken;
    const sessions = await request(`/emby/Sessions?DeviceId=${encodeURIComponent(device)}&ActiveWithinSeconds=0`);
    const own = sessions.Body?.find(value => value.UserId === input.UserId && value.DeviceId === device);
    if (sessions.Status !== 200 || !own?.Id || own.Client !== metadata['X-Emby-Client'] || own.DeviceName !== 'Owned Chromium' || own.ApplicationVersion !== '1') throw new Error('reference_session_identity_missing');
    sessionId = own.Id;
    if (!input.Controller) {
      const user = await request('/emby/Users/Me'); profilePin = user.Body?.Configuration?.ProfilePin;
      if (user.Status !== 200 || user.Body.Id !== input.UserId || typeof profilePin !== 'string' || !/^[0-9]{4}$/.test(profilePin)) throw new Error('own_profile_pin_projection_missing');
    }
    return { ...projectSession(own), AuthenticationStatus: login.Status };
  }, { BaseURL: fixture.BaseURL, RunId: fixture.RunId, Controller: controller,
    UserId: controller ? fixture.AdminId : fixture.ViewerId, Name: controller ? fixture.AdminName : fixture.ViewerName,
    Password: controller ? fixture.AdminPassword : fixture.ViewerPassword, ControllerUserId: fixture.AdminId, AudioItemId: fixture.AudioItemId });
  if (controller) { controllerContext = created; controllerPage = opened; result.Controller = identity; }
  else { targetContext = created; targetPage = opened; result.Target = identity; }
  return identity;
}

async function clientAPI(target, method = 'GET', body, expected = 200, controller = false) {
  const selected = controller ? controllerPage : targetPage; diagnosticPage = selected;
  const response = await selected.evaluate(input => window.gobyPhase4.api(input.Target, input.Method, input.Body), { Target: target, Method: method, Body: body });
  requireThat(response.Status === expected, `reference_http_${response.Status}_expected_${expected}`);
  return response.Body;
}

function safeRegistration(value) {
  const keys = ['Id', 'Revision', 'Transport', 'Enabled', 'EventIds', 'HasTargetToken', 'LastOutcome'];
  requireThat(value && typeof value.Revision === 'string' && Object.keys(value).length === keys.length &&
    keys.every(key => Object.hasOwn(value, key)), 'notification_registration_dto_invalid');
  return { Id: value.Id, Revision: value.Revision, Transport: value.Transport, Enabled: value.Enabled,
    EventIds: value.EventIds, HasTargetToken: value.HasTargetToken, LastOutcome: value.LastOutcome };
}
async function putRegistration(token) {
  const before = safeRegistration(await clientAPI('/emby/Sessions/Notifications'));
  const saved = safeRegistration(await clientAPI('/emby/Sessions/Notifications', 'PUT', { Revision: before.Revision,
    Transport: 'GobyWebhookV1', EventIds: ['CatalogInvalidated', 'UserDataInvalidated'], TargetToken: token }));
  requireThat(/^[0-9a-f]{32}$/.test(saved.Id) && saved.Enabled && saved.HasTargetToken && saved.Transport === 'GobyWebhookV1' &&
    BigInt(saved.Revision) === BigInt(before.Revision) + 1n && (!before.Id || saved.Id === before.Id) && saved.LastOutcome === '',
  'notification_registration_cas_failed');
  requireThat(Array.isArray(saved.EventIds) && JSON.stringify([...saved.EventIds].sort()) ===
    JSON.stringify(['CatalogInvalidated', 'UserDataInvalidated']), 'notification_subscription_event_set_mismatch');
  registration = saved; return saved;
}
async function favorite(enabled) {
  const value = await clientAPI(`/emby/Users/${fixture.ViewerId}/FavoriteItems/${fixture.VisibleMovieId}`, enabled ? 'POST' : 'DELETE');
  requireThat(value.IsFavorite === enabled, 'actual_userdata_trigger_failed');
}
async function notificationAck(phase, extra = {}, deliveryKind = '') {
  const ack = await stage(phase, { RegistrationId: registration.Id, RegistrationRevision: registration.Revision, ...extra });
  const value = ack.NotificationEvidence;
  requireThat(value?.Complete === true, 'independent_notification_evidence_missing');
  if (deliveryKind) {
    requireThat(value.ReceiverAccepted === true && value.ClientConsumed === true && value.Kind === deliveryKind &&
      value.RegistrationId === registration.Id && value.Generation === registration.Revision && /^[0-9a-f]{32}$/.test(value.EventId) &&
      !receivedEvents.has(value.EventId) && value.Attempts >= (phase === 'notification-retried' ? 2 : 1), 'matched_notification_delivery_not_proven');
    receivedEvents.add(value.EventId);
    if (phase === 'notification-offline') requireThat(value.NoWebSocket === true, 'offline_notification_depended_on_socket');
    const current = safeRegistration(await clientAPI('/emby/Sessions/Notifications'));
    requireThat(current.Id === registration.Id && current.Revision === registration.Revision && current.LastOutcome === 'delivered', 'notification_receipt_and_sender_outcome_disagree');
    registration = current;
  }
  result.NotificationEvidence[phase] = value;
  return value;
}

async function inspectBrowseAndCapabilities() {
  const pin = targetPage.locator('#profile-pin');
  await pin.fill(fixture.ProfilePin === '0000' ? '9999' : '0000'); await targetPage.locator('#unlock-profile').click();
  await targetPage.getByText('Incorrect profile PIN', { exact: true }).waitFor();
  const rejected = await targetPage.evaluate(() => window.gobyPhase4.pinState());
  requireThat(!rejected.Unlocked && rejected.WrongAttempts === 1 && rejected.AuthenticatedFirst, 'reference_wrong_profile_pin_not_rejected');
  await pin.fill(fixture.ProfilePin); await targetPage.locator('#unlock-profile').click();
  await targetPage.getByText('Profile unlocked', { exact: true }).waitFor();
  result.ProfileLock = await targetPage.evaluate(() => window.gobyPhase4.pinState());
  requireThat(result.ProfileLock.Unlocked && result.ProfileLock.ServerPinLoginAttempted === false, 'reference_profile_lock_not_unlocked');
  const user = encodeURIComponent(fixture.ViewerId);
  const artists = await clientAPI(`/emby/Artists?UserId=${user}&ArtistType=Artist,AlbumArtist&Limit=100`);
  requireThat(artists.Items.some(item => String(item.Id) === fixture.ArtistId), 'actual_artist_browse_missing');
  const albums = await clientAPI(`/emby/Items?UserId=${user}&ArtistIds=${encodeURIComponent(fixture.ArtistId)}&IncludeItemTypes=MusicAlbum&Recursive=true`);
  requireThat(albums.Items.some(item => item.Id === fixture.AlbumId), 'actual_album_browse_missing');
  const tracks = await clientAPI(`/emby/Items?UserId=${user}&ParentId=${encodeURIComponent(fixture.AlbumId)}&IncludeItemTypes=Audio&Recursive=true`);
  requireThat(tracks.Items.some(item => item.Id === fixture.AudioItemId), 'actual_track_browse_missing');
  const mix = await clientAPI(`/emby/Songs/${fixture.AudioItemId}/InstantMix?UserId=${user}&Limit=20`);
  requireThat(mix.Items[0]?.Id === fixture.AudioItemId && mix.Items.every(item => item.Type === 'Audio' && item.RunTimeTicks > 0), 'actual_mix_playable_seed_missing');
  const hints = await clientAPI(`/emby/Search/Hints?UserId=${user}&SearchTerm=${encodeURIComponent(fixture.SearchTerm)}&IncludeItemTypes=Audio,MusicAlbum,MusicArtist&Limit=100`);
  const hint = hints.SearchHints.find(item => item.Id === fixture.AudioItemId && item.GobyReference?.Kind === 'Item');
  requireThat(hint && typeof hint.GobyNavigationUrl === 'string', 'actual_search_reference_missing');
  const detail = await clientAPI(hint.GobyNavigationUrl);
  requireThat(detail.Id === fixture.AudioItemId && detail.Type === 'Audio', 'actual_search_detail_mismatch');
  result.Browse = { ArtistId: fixture.ArtistId, AlbumId: fixture.AlbumId, AudioItemId: detail.Id,
    MixIds: mix.Items.map(item => item.Id), SearchHintCount: hints.TotalRecordCount };
  await clientAPI('/emby/Sessions/Capabilities/Full', 'POST', { PlayableMediaTypes: ['Audio'],
    SupportedCommands: ['Pause', 'Unpause', 'Stop'], SupportsMediaControl: true, SupportsSync: false }, 204);
  const snapshot = await targetPage.evaluate(() => window.gobyPhase4.connectSocket());
  requireThat(snapshot.Id === result.Target.Id && snapshot.DeviceId === result.Target.DeviceId && snapshot.UserId === fixture.ViewerId &&
    snapshot.SupportsRemoteControl === true, 'actual_capabilities_and_sessions_subscription_mismatch');
  result.ClientCapabilities = snapshot;
}
function scoped(value) {
  return { PlaybackReference: value.PlaybackReference, NegotiatedPlaySessionId: value.NegotiatedPlaySessionId,
    TargetSessionId: value.TargetSessionId, DeviceId: value.DeviceId, MediaSourceId: value.MediaSourceId,
    AudioItemId: value.AudioItemId, PositionTicks: value.PositionTicks, IsPaused: value.IsPaused };
}
async function nativeLogout() {
  diagnosticPage = page;
  await page.goto(`${fixture.BaseURL}/admin/`);
  await page.getByRole('navigation', { name: 'Administration', exact: true }).waitFor();
  await responseFor('DELETE', '/admin/v1/session', () => page.getByRole('button', { name: 'Sign out', exact: true }).click(), 204, false);
  await page.getByRole('heading', { name: 'Sign in to Goby', exact: true }).waitFor();
  await context.close(); context = undefined;
}

async function main() {
  requireThat(process.platform === 'linux' && process.getuid() === 0, 'owned_linux_fixture_required');
  const contextPath = process.env.GOBY_SELECTED_PHASE4_CONTEXT;
  requireThat(typeof contextPath === 'string' && path.isAbsolute(contextPath), 'private_context_required');
  const directory = path.dirname(contextPath), stat = await fs.lstat(directory);
  requireThat(stat.isDirectory() && !stat.isSymbolicLink() && stat.uid === process.getuid() &&
    (stat.mode & 0o777) === 0o700 && await fs.realpath(directory) === directory, 'private_directory_required');
  fixture = await privateJSON(contextPath);
  requireThat(fixture.Marker === 'goby-selected-phase4-browser-fixture-v1' && fixture.RunId === process.env.GOBY_SELECTED_PHASE4_RUN_ID &&
    /^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$/.test(fixture.RunId), 'fixture_identity_mismatch');
  const initial = new URL(fixture.BaseURL), restarted = new URL(fixture.RestartBaseURL);
  for (const origin of [initial, restarted]) requireThat(origin.protocol === 'http:' && origin.hostname === '127.0.0.1' &&
    origin.href === `${origin.origin}/` && Number(origin.port) > 1024, 'owned_goby_origin_required');
  requireThat(initial.origin === fixture.BaseURL && restarted.origin === fixture.RestartBaseURL && initial.origin !== restarted.origin &&
    Number(restarted.port) === fixture.NextPort && fixture.ArtifactsDir === directory && fixture.ResultPath === path.join(directory, 'browser-result.json'), 'owned_listener_and_artifact_binding_mismatch');
  const receiver = new URL(fixture.NotificationEndpoint);
  requireThat(receiver.protocol === 'https:' && receiver.hostname === '127.0.0.1' && receiver.pathname === '/events' &&
    !receiver.search && !receiver.hash && !receiver.username && !receiver.password && Number(receiver.port) > 1024, 'owned_receiver_endpoint_required');
  for (const key of ['AdminId', 'AdminName', 'AdminPassword', 'ViewerId', 'ViewerName', 'ViewerPassword', 'LibraryId', 'VisibleMovieId',
    'HiddenMovieId', 'HiddenMovieName', 'AudioItemId', 'AudioName', 'AlbumId', 'ArtistId', 'SearchTerm', 'ReceiverCredential', 'TargetTokenA', 'TargetTokenB'])
    requireThat(typeof fixture[key] === 'string' && fixture[key].length > 0 && fixture[key].length <= 4096, 'context_field_missing');
  requireThat(/^[0-9]{4}$/.test(fixture.ProfilePin) && fixture.TargetTokenA !== fixture.TargetTokenB, 'owned_profile_and_target_secrets_required');
  result.RunId = fixture.RunId;
  const modulePath = process.env.GOBY_TEST_PLAYWRIGHT_MODULE;
  requireThat(typeof modulePath === 'string' && path.isAbsolute(modulePath), 'playwright_module_required');
  const { chromium } = createRequire(import.meta.url)(modulePath);
  browser = await chromium.launch({ headless: true });
  deadline = setTimeout(() => { result.DeadlineExceeded = true; void browser.close(); }, 600000);
  context = await guardedContext(); page = await context.newPage(); diagnosticPage = page;

  currentPhase = 'account-pin';
  await operation('native-administrator-sign-in', nativeLogin);
  const credentialRevision = await operation('save-authenticated-profile-pin', setProfilePin);
  result.Checks.AccountPin = true; await stage(currentPhase, { UserId: fixture.ViewerId, CredentialRevision: credentialRevision });

  currentPhase = 'runtime-saved';
  await operation('save-desired-listener-and-managed-execution', () => saveRuntime(false));
  const runtimeAck = await stage(currentPhase, { SettingsRevision: settings.Revision, NextPort: fixture.NextPort });
  result.ManagedVideoEvidence = runtimeAck.ManagedVideoEvidence;
  requireThat(result.ManagedVideoEvidence?.Complete === true, 'managed_settings_actual_video_evidence_missing');
  result.Checks.RuntimeSaved = true;

  currentPhase = 'client-capabilities';
  await operation('authenticate-owned-reference-client', () => installReferenceClient(false)); diagnosticPage = targetPage;
  await operation('unlock-profile-browse-and-subscribe-sessions', inspectBrowseAndCapabilities);
  result.Checks.ClientCapabilities = true; await stage(currentPhase, { TargetSessionId: result.Target.Id, DeviceId: result.Target.DeviceId, AudioItemId: fixture.AudioItemId });

  currentPhase = 'audio-playing';
  await operation('play-real-correlated-audio', async () => {
    diagnosticPage = targetPage; await targetPage.locator('#play-audio').click();
    await targetPage.waitForFunction(() => window.gobyPhase4.state().Playing || window.gobyPhase4.state().Failure, null, { timeout: 30000 });
    requireThat(!(await targetPage.evaluate(() => window.gobyPhase4.state().Failure)), 'actual_correlated_audio_start_failed');
    const observed = await targetPage.evaluate(() => window.gobyPhase4.observeAudio());
    requireThat(observed.ActualNonceNegotiation && observed.NegotiatedServerIDRetainedSeparately && observed.PositionTicks >= 20000000 &&
      observed.PlaybackReference === `phase4-client-${fixture.RunId}` && observed.TargetSessionId === result.Target.Id && observed.Samples.length >= 3,
    'actual_correlated_audio_observation_missing');
    for (let index = 0; index < observed.Samples.length; index++) {
      const value = observed.Samples[index], before = observed.Samples[index - 1];
      requireThat(!value.Paused && !value.Ended && !value.Seeking && value.ReadyState >= 2 && value.ErrorCode === 0 && value.AudioContextState === 'running' &&
        value.RMS > .0001 && value.Peak > .001 && (!before || value.Seconds > before.Seconds && value.AudioContextTime > before.AudioContextTime), 'decoded_audio_clock_or_output_invalid');
    }
    result.AudioPlaying = observed; playbackScope = scoped(observed);
  });
  result.Checks.AudioPlaying = true; await stage(currentPhase, playbackScope);

  currentPhase = 'remote-paused';
  await operation('authenticate-independent-controller', () => installReferenceClient(true));
  await operation('remote-pause-actual-client-and-report', async () => {
    await clientAPI(`/emby/Sessions/${encodeURIComponent(result.Target.Id)}/Playing/Pause`, 'POST', undefined, 204, true);
    diagnosticPage = targetPage;
    await targetPage.waitForFunction(() => { const state = window.gobyPhase4.state(); return state.Failure ||
      state.Commands.length === 1 && state.LatestSession?.PlayState?.IsPaused === true; }, null, { timeout: 20000 });
    const state = await targetPage.evaluate(() => window.gobyPhase4.state()), command = state.Commands[0];
    requireThat(!state.Failure && command?.Command === 'Pause' && command.TargetSessionId === result.Target.Id &&
      command.ControllingUserId === fixture.AdminId && !command.Before.Paused && !command.Before.Ended && command.After.Paused && !command.After.Ended &&
      Math.abs(command.After.Seconds - command.Before.Seconds) < .03 && command.Report.Status === 204 && command.Report.IsPaused === true &&
      state.LatestSession.NowPlayingItemId === fixture.AudioItemId && state.LatestSession.PlayState.MediaSourceId === playbackScope.MediaSourceId,
    'remote_pause_was_not_executed_by_actual_media_client');
    result.RemotePause = { Command: command, Session: state.LatestSession };
    playbackScope = scoped(state.Scope); playbackScope.PositionTicks = command.Report.PositionTicks; playbackScope.IsPaused = true;
    await screenshot('reference-client-remote-paused');
  });
  result.Checks.RemotePaused = true; await stage(currentPhase, playbackScope);

  currentPhase = 'session-reconnected';
  await operation('resume-offline-and-resubscribe-current-session', async () => {
    await targetPage.evaluate(() => window.gobyPhase4.closeSocket());
    const before = await targetPage.evaluate(() => window.gobyPhase4.state().Sample.Seconds);
    await targetPage.locator('#resume-audio').click();
    await targetPage.waitForFunction(seconds => { const state = window.gobyPhase4.state(); return state.Failure ||
      !state.Sample.Paused && state.Sample.Seconds > seconds + .25 && state.Scope.IsPaused === false; }, before, { timeout: 10000 });
    const reported = await targetPage.evaluate(() => window.gobyPhase4.reportCurrent());
    const snapshot = await targetPage.evaluate(() => window.gobyPhase4.connectSocket());
    const state = await targetPage.evaluate(() => window.gobyPhase4.state());
    requireThat(!state.Failure && state.SocketGeneration === 2 && state.Commands.length === 1 && snapshot.Id === result.Target.Id &&
      snapshot.SupportsRemoteControl === true && snapshot.PlayState?.IsPaused === false && snapshot.NowPlayingItemId === fixture.AudioItemId &&
      state.SocketHistory[0].Closed && state.SocketHistory[1].Snapshots > 0 && !state.Sample.Paused && !state.Sample.Ended,
    'sessions_reconnect_did_not_resynchronize_real_client_state');
    playbackScope = scoped(reported); playbackScope.IsPaused = false;
    result.SessionReconnect = { Session: snapshot, SocketHistory: state.SocketHistory, CommandCount: state.Commands.length,
      MissedCommandReplayClaimed: false, ActualMedia: state.Sample };
  });
  result.Checks.SessionReconnected = true; await stage(currentPhase, playbackScope);

  currentPhase = 'audio-stopped';
  await operation('stop-correlated-audio-and-retire-output', async () => {
    await targetPage.locator('#stop-audio').click();
    await targetPage.waitForFunction(() => window.gobyPhase4.state().Stopped || window.gobyPhase4.state().Failure, null, { timeout: 30000 });
    const state = await targetPage.evaluate(() => window.gobyPhase4.state());
    requireThat(!state.Failure && state.Stopped?.StoppedStatus === 204 && state.Stopped.EncodingsStatus === 204 && state.Stopped.MediaDetached &&
      state.Stopped.AudioContextState === 'closed' && state.Stopped.PlaybackReference === playbackScope.PlaybackReference &&
      state.Stopped.PositionTicks >= playbackScope.PositionTicks, 'correlated_audio_stop_incomplete');
    result.AudioStopped = state.Stopped; playbackScope = scoped(state.Stopped);
  });
  result.Checks.AudioStopped = true; await stage(currentPhase, playbackScope);

  currentPhase = 'notifications-configured';
  await operation('configure-real-https-notification-receiver', () => configureNotifications(true, true));
  result.Checks.NotificationsConfigured = true; await stage(currentPhase, { NotificationSettingsRevision: notificationSettings.Revision });

  currentPhase = 'notification-retried';
  await operation('register-matched-target-and-enqueue-one-test', async () => {
    await putRegistration(fixture.TargetTokenA);
    await clientAPI('/emby/Sessions/Notifications/Test', 'POST', undefined, 202);
  });
  await notificationAck(currentPhase, {}, 'Test'); result.Checks.NotificationRetried = true;

  currentPhase = 'notification-offline';
  await operation('deliver-real-userdata-without-websocket', async () => {
    await targetPage.evaluate(() => window.gobyPhase4.closeSocket());
    requireThat(await targetPage.evaluate(() => window.gobyPhase4.state().SocketClosed), 'target_websocket_still_open');
    await favorite(true);
  });
  await notificationAck(currentPhase, { VisibleItemId: fixture.VisibleMovieId }, 'UserDataInvalidated'); result.Checks.NotificationOffline = true;

  currentPhase = 'notification-hidden';
  const metadataRevision = await operation('change-actual-denied-source-metadata', editHiddenSource);
  const hidden = await notificationAck(currentPhase, { HiddenItemId: fixture.HiddenMovieId, MetadataRevision: metadataRevision });
  requireThat(hidden.HiddenEvaluated && hidden.NoNewDeliveries && hidden.NoNewReceipts && hidden.LastOutcomeUnchanged && hidden.RegistrationUpdatedAtUnchanged,
    'hidden_source_notification_side_effects_not_excluded');
  result.Checks.NotificationHidden = true;

  currentPhase = 'notification-rotated';
  await operation('rotate-personal-notification-target', () => putRegistration(fixture.TargetTokenB));
  const rotated = await notificationAck(currentPhase);
  requireThat(rotated.ReceiverTargetRotated && rotated.OldDeliveriesRetired && rotated.PrivateGenerationAdvanced, 'notification_rotation_not_fenced');
  result.Checks.NotificationRotated = true;

  currentPhase = 'notification-new-target';
  await operation('deliver-fresh-userdata-to-rotated-target', () => favorite(false));
  await notificationAck(currentPhase, { VisibleItemId: fixture.VisibleMovieId }, 'UserDataInvalidated'); result.Checks.NotificationNewTarget = true;

  currentPhase = 'notification-revoked';
  await operation('revoke-target-before-new-userdata', async () => {
    const previous = registration;
    registration = safeRegistration(await clientAPI(`/emby/Sessions/Notifications?Revision=${encodeURIComponent(previous.Revision)}`, 'DELETE'));
    requireThat(registration.Id === previous.Id && !registration.Enabled && registration.LastOutcome === 'revoked' &&
      BigInt(registration.Revision) === BigInt(previous.Revision) + 1n, 'personal_notification_revoke_not_observed');
    await favorite(true);
  });
  const revoked = await notificationAck(currentPhase, { VisibleItemId: fixture.VisibleMovieId });
  requireThat(revoked.NoNewDeliveries && revoked.NoNewReceipts, 'revoked_target_received_new_notification'); result.Checks.NotificationRevoked = true;

  currentPhase = 'notifications-disabled';
  await operation('disable-transport-with-configured-registration', async () => {
    await putRegistration(fixture.TargetTokenB); await configureNotifications(false); await favorite(false);
  });
  const disabled = await notificationAck(currentPhase, { NotificationSettingsRevision: notificationSettings.Revision, VisibleItemId: fixture.VisibleMovieId });
  requireThat(disabled.NoNewDeliveries && disabled.NoNewReceipts, 'disabled_transport_received_new_notification'); result.Checks.NotificationsDisabled = true;

  currentPhase = 'listener-restarted';
  await operation('normal-client-logouts-before-listener-restart', async () => {
    requireThat(await targetPage.evaluate(() => window.gobyPhase4.logout()) === 204, 'target_logout_failed');
    requireThat(await controllerPage.evaluate(() => window.gobyPhase4.logout()) === 204, 'controller_logout_failed');
    result.TargetHTTP = await targetPage.evaluate(() => window.gobyPhase4.http());
    result.ControllerHTTP = await controllerPage.evaluate(() => window.gobyPhase4.http());
    await targetContext.close(); targetContext = undefined; await controllerContext.close(); controllerContext = undefined;
    await nativeLogout();
  });
  const rebound = await stage(currentPhase, { SettingsRevision: settings.Revision, NextPort: fixture.NextPort });
  requireThat(rebound.BaseURL === fixture.RestartBaseURL, 'actual_restarted_listener_origin_mismatch');
  fixture.BaseURL = rebound.BaseURL; result.ReboundOrigin = fixture.BaseURL; result.Checks.ListenerRestarted = true;

  currentPhase = 'restarted-csrf';
  context = await guardedContext(); page = await context.newPage(); diagnosticPage = page;
  await operation('new-origin-native-login-and-current-settings-save', async () => {
    await nativeLogin(); await saveRuntime(true);
    const notifications = await responseFor('GET', '/admin/v1/notifications', () => page.goto(`${fixture.BaseURL}/admin/notifications`));
    requireThat(notifications.Body.Enabled === false && notifications.Body.HasReceiverCredential === true &&
      notifications.Body.Revision === notificationSettings.Revision && !Object.hasOwn(notifications.Body, 'ReceiverCredential'), 'restart_changed_notification_configuration');
  });
  result.Checks.RestartedCSRF = true; await stage(currentPhase, { SettingsRevision: settings.Revision });

  currentPhase = 'cleanup'; await operation('final-native-sign-out', nativeLogout);
  await stage(currentPhase); result.Checks.Cleanup = true;
  requireThat(result.PageErrors === 0 && result.ForeignRequests === 0 && checks.every(key => result.Checks[key]), 'phase4_browser_evidence_incomplete');
  result.Complete = true;
}

try { await main(); }
catch (error) { result.Complete = false; result.FailurePhase ??= currentPhase; result.FailureOperation ??= currentOperation;
  result.FailureCode = error.safeCode || 'browser_operation_failed'; process.exitCode = 1; }
finally {
  clearTimeout(deadline);
  for (const client of [targetPage, controllerPage]) if (client && !client.isClosed())
    await client.evaluate(() => window.gobyPhase4?.logout()).catch(() => { result.ReferenceFallbackCleanupFailed = true; });
  if (browser) await browser.close().catch(() => { result.Complete = false; process.exitCode = 1; });
  if (fixture?.ResultPath) await writeJSON(fixture.ResultPath, result).catch(() => { result.Complete = false; process.exitCode = 1; });
  process.stdout.write(`${JSON.stringify({ Marker: result.Marker, RunId: result.RunId, Complete: result.Complete,
    FailurePhase: result.FailurePhase, FailureOperation: result.FailureOperation, FailureCode: result.FailureCode ?? null })}\n`);
}
