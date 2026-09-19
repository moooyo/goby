#!/usr/bin/env node
/** Real native administration and unmodified Emby Web acceptance. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import { createRequire } from 'node:module';
import { createHash } from 'node:crypto';

const PHASES = ['admin-credentials', 'admin-preferences', 'admin-intro', 'original-local-login',
  'profile-pin', 'next-enabled', 'next-disabled', 'intro-show-button', 'intro-none',
  'intro-auto-skip', 'restart', 'persisted', 'credentials-cleared', 'cleanup'];
const CHECKS = ['AdminCredentials', 'AdminPreferences', 'AdminIntro', 'OriginalLocalLogin', 'ProfilePin',
  'IntroShowButton', 'IntroNone', 'IntroAutoSkip', 'NextEnabled', 'NextDisabled',
  'RestartPersisted', 'CredentialsCleared', 'Cleanup'];
const result = { Marker: 'goby-selected-phase1-browser-result-v1', RunId: '', Complete: false,
  Stages: [], Checks: Object.fromEntries(CHECKS.map(name => [name, false])), PageErrors: 0,
  ForeignRequests: 0, BlockedEntitlementRequests: 0, BlockedStages: [], Playback: [], Authentication: [], Screenshots: [], FailurePhase: null,
  NetworkGuard: { Started: false, Closed: false, BlockedHTTP: 0, BlockedConnect: 0, BlockedUpgrade: 0, UnexpectedTargetRequests: 0 } };
const fail = code => { const error = new Error(code); error.safeCode = code; throw error; };
const check = (condition, code) => { if (!condition) fail(code); };
const sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
const hash = value => createHash('sha256').update(value).digest('hex');
let fixture, browser, admin, client, currentPhase = 'admission', privateContextPath, networkGuard, networkGuardOrigin;
const networkGuardSockets = new Set();
let deadline;

async function privateDirectory(directory) {
  const stat = await fs.lstat(directory);
  check(stat.isDirectory() && !stat.isSymbolicLink() && stat.uid === process.getuid() &&
    (stat.mode & 0o777) === 0o700 && await fs.realpath(directory) === directory, 'private_directory_required');
}
async function privateJSON(filename, limit = 1 << 20) {
  const file = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = await file.stat();
    check(stat.isFile() && stat.uid === process.getuid() && (stat.mode & 0o777) === 0o600 &&
      stat.size > 0 && stat.size <= limit, 'private_json_required');
    return JSON.parse(await file.readFile('utf8'));
  } finally { await file.close(); }
}
async function writeJSON(filename, value) {
  check(path.dirname(filename) === fixture.ArtifactsDir, 'artifact_parent_mismatch');
  const temporary = `${filename}.new`;
  const file = await fs.open(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try { await file.writeFile(`${JSON.stringify(value, null, 2)}\n`); await file.sync(); }
  finally { await file.close(); }
  await fs.rename(temporary, filename);
}
async function stage(phase, state = 'complete') {
  check(phase === PHASES[result.Stages.length], 'stage_order_mismatch');
  check(state === 'complete' || state === 'blocked' && ['intro-show-button', 'intro-auto-skip'].includes(phase), 'stage_state_invalid');
  currentPhase = phase;
  await writeJSON(path.join(fixture.ArtifactsDir, `stage-${phase}-request.json`), { RunId: fixture.RunId, Phase: phase,
    State: state, ...(state === 'blocked' ? { Reason: 'original_client_entitlement' } : {}) });
  const until = Date.now() + 45000;
  while (Date.now() < until) {
    let acknowledgement;
    try { acknowledgement = await privateJSON(path.join(fixture.ArtifactsDir, `stage-${phase}-database.json`)); }
    catch (error) { if (error.code !== 'ENOENT') throw error; await sleep(100); continue; }
    check(acknowledgement.Marker === 'goby-selected-phase1-stage-database-v1' &&
      acknowledgement.RunId === fixture.RunId && acknowledgement.Phase === phase &&
      acknowledgement.SourcesUnchanged === true && (state === 'complete' ? acknowledgement.Complete === true :
        acknowledgement.Complete === false && acknowledgement.Observed === true && acknowledgement.Blocked === true), 'database_acknowledgement_failed');
    result.Stages.push({ Phase: phase, State: state });
    await writeJSON(fixture.ResultPath, result);
    return;
  }
  fail('database_acknowledgement_timeout');
}
async function responseFor(page, method, pathname, action, expected = 200, readJSON = true) {
  const waiting = page.waitForResponse(response => response.request().method() === method &&
    new URL(response.url()).origin === fixture.BaseURL && new URL(response.url()).pathname === pathname,
  { timeout: 15000 }).then(async response => ({ status: response.status(),
    body: readJSON ? await response.json() : null }));
  void waiting.catch(() => {});
  await action();
  const response = await waiting;
  check(response.status === expected, 'unexpected_http_status');
  return response.body;
}
function isOwnedNetworkURL(value) {
  try {
    const url = new URL(value);
    if (url.protocol === 'ws:') url.protocol = 'http:';
    return !url.username && !url.password && url.origin === fixture.BaseURL;
  } catch { return false; }
}
function blockedNetwork(value, authorityOnly = false) {
  result.ForeignRequests += 1;
  try {
    const url = new URL(value);
    if (url.origin === 'https://mb3admin.com' &&
      (authorityOnly || url.pathname === '/admin/service/registration/validateDevice') &&
      ['intro-show-button', 'intro-auto-skip'].includes(currentPhase)) result.BlockedEntitlementRequests += 1;
  } catch { /* An invalid external address is still denied. */ }
}
async function startNetworkGuard() {
  // Service workers can issue traffic outside page-route interception. This
  // deny-only proxy opens no upstream connection, and the Chromium bypass list
  // permits only the exact owned HTTP/WebSocket authority.
  const reject = (request, socket, tunnel = false) => {
    const value = tunnel ? `https://${request.url}/` : request.url ?? '';
    if (isOwnedNetworkURL(value)) result.NetworkGuard.UnexpectedTargetRequests += 1;
    else blockedNetwork(value, tunnel);
    result.NetworkGuard[tunnel ? 'BlockedConnect' : 'BlockedUpgrade'] += 1;
    socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\nContent-Length: 0\r\n\r\n');
  };
  networkGuard = http.createServer({ maxHeaderSize: 16384 }, (request, response) => {
    if (isOwnedNetworkURL(request.url ?? '')) result.NetworkGuard.UnexpectedTargetRequests += 1;
    else blockedNetwork(request.url ?? '');
    result.NetworkGuard.BlockedHTTP += 1;
    response.writeHead(403, { Connection: 'close', 'Content-Length': '0' }); response.end();
  });
  networkGuard.maxConnections = 32; networkGuard.maxHeadersCount = 64;
  networkGuard.headersTimeout = 5000; networkGuard.requestTimeout = 5000;
  networkGuard.on('connect', (request, socket) => reject(request, socket, true));
  networkGuard.on('upgrade', (request, socket) => reject(request, socket));
  networkGuard.on('clientError', (_error, socket) => socket.destroy());
  networkGuard.on('connection', socket => {
    networkGuardSockets.add(socket);
    socket.on('error', () => {});
    socket.on('close', () => networkGuardSockets.delete(socket));
  });
  await new Promise((resolve, reject) => { networkGuard.once('error', reject); networkGuard.listen(0, '127.0.0.1', resolve); });
  networkGuardOrigin = `http://127.0.0.1:${networkGuard.address().port}`;
  result.NetworkGuard.Started = true;
}
async function closeNetworkGuard() {
  if (!networkGuard) return;
  const closed = new Promise((resolve, reject) => networkGuard.close(error => error ? reject(error) : resolve()));
  for (const socket of networkGuardSockets) socket.destroy();
  await closed;
  const remaining = await new Promise((resolve, reject) => networkGuard.getConnections((error, count) => error ? reject(error) : resolve(count)));
  check(remaining === 0, 'browser_network_guard_connections_remain');
  result.NetworkGuard.Closed = true;
}
async function guardedContext(originalClient = false) {
  const authority = new URL(fixture.BaseURL).host;
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US',
    serviceWorkers: originalClient ? 'allow' : 'block', acceptDownloads: false,
    proxy: { server: networkGuardOrigin, bypass: `<-loopback>,http://${authority},ws://${authority}` } });
  await context.route('**/*', async route => {
    const url = new URL(route.request().url());
    if (url.origin === fixture.BaseURL || ['data:', 'blob:'].includes(url.protocol)) await route.continue();
    else {
      blockedNetwork(url.toString());
      await route.abort('blockedbyclient');
    }
  });
  await context.routeWebSocket('**/*', socket => {
    const url = new URL(socket.url());
    if (url.protocol === 'ws:') url.protocol = 'http:';
    if (url.origin === fixture.BaseURL) socket.connectToServer();
    else { blockedNetwork(url.toString()); socket.close(); }
  });
  context.on('page', page => page.on('pageerror', () => { result.PageErrors += 1; }));
  return context;
}
async function administratorLogin() {
  await admin.goto(`${fixture.BaseURL}/admin/`);
  await admin.getByRole('heading', { name: 'Sign in to Goby', exact: true }).waitFor();
  await admin.getByRole('textbox', { name: 'Username', exact: true }).fill(fixture.AdminName);
  await admin.locator('input#account-password[name="Password"]').fill(fixture.AdminPassword);
  await admin.getByRole('button', { name: 'Sign in', exact: true }).click();
  await admin.getByRole('navigation', { name: 'Administration', exact: true }).waitFor();
}
async function userDialog(action, dialogName) {
  await admin.goto(`${fixture.BaseURL}/admin/users`);
  await admin.getByRole('button', { name: `Manage ${fixture.UserName}`, exact: true }).click();
  await admin.getByRole('dialog', { name: 'Manage user', exact: true }).getByRole('button', { name: action, exact: true }).click();
  const dialog = admin.getByRole('dialog', { name: dialogName, exact: true });
  await dialog.waitFor();
  return dialog;
}
async function credentials(clear = false) {
  const dialog = await userDialog('Manage local credentials', 'Local credentials');
  if (!clear) {
    await dialog.getByLabel('New local password', { exact: true }).waitFor();
    check(await dialog.getByLabel('New local password', { exact: true }).inputValue() === '' &&
      await dialog.getByLabel('New profile PIN', { exact: true }).inputValue() === '', 'credential_screenshot_fields_not_empty');
    await admin.screenshot({ path: path.join(fixture.ArtifactsDir, 'native-credentials-empty-desktop.png') });
    result.Screenshots.push('native-credentials-empty-desktop.png');
  }
  await dialog.getByRole('checkbox', { name: 'Enable local password', exact: true }).setChecked(!clear);
  if (clear) {
    await dialog.getByRole('button', { name: 'Clear local password', exact: true }).click();
    await dialog.getByRole('button', { name: 'Clear profile PIN', exact: true }).click();
  } else {
    for (const label of ['New local password', 'Confirm local password']) await dialog.getByLabel(label, { exact: true }).fill(fixture.LocalPassword);
    for (const label of ['New profile PIN', 'Confirm profile PIN']) await dialog.getByLabel(label, { exact: true }).fill(fixture.ProfilePin);
  }
  await dialog.getByRole('button', { name: 'Save local credentials', exact: true }).click();
  const mutation = await responseFor(admin, 'PUT', `/admin/v1/users/${fixture.UserId}/local-credentials`, () =>
    admin.getByRole('dialog', { name: 'Save local credentials?', exact: true }).getByRole('button', { name: 'Save and end sign-ins', exact: true }).click());
  const saved = mutation.Credentials;
  check(saved && mutation.CurrentSessionRevoked === false, 'credential_mutation_scope_mismatch');
  check(saved.HasLocalPassword === !clear && saved.HasProfilePin === !clear && saved.EnableLocalPassword === !clear,
    'credential_status_not_persisted');
  check(!Object.hasOwn(saved, 'LocalPassword') && !Object.hasOwn(saved, 'ProfilePin'), 'credential_response_disclosed_secret');
}
async function preferences(mode, autoplay) {
  const dialog = await userDialog('Playback and display preferences', 'Playback and display preferences');
  await dialog.getByRole('combobox', { name: 'Intro skipping', exact: true }).click();
  await admin.getByRole('option', { name: { None: 'Off', ShowButton: 'Show skip button', AutoSkip: 'Skip automatically' }[mode], exact: true }).click();
  await dialog.getByRole('checkbox', { name: 'Automatically play the next episode', exact: true }).setChecked(autoplay);
  const saved = await responseFor(admin, 'PUT', `/admin/v1/users/${fixture.UserId}/preferences`, () =>
    dialog.getByRole('button', { name: 'Save preferences', exact: true }).click());
  check(saved.Configuration.IntroSkipMode === mode && saved.Configuration.EnableNextEpisodeAutoPlay === autoplay, 'preferences_not_persisted');
}
async function introEditor() {
  await admin.goto(`${fixture.BaseURL}/admin/libraries/${fixture.MovieLibraryId}/items`);
  await admin.getByRole('button', { name: `Edit metadata for ${fixture.MovieName}`, exact: true }).click();
  await admin.getByRole('dialog', { name: /^Edit metadata/ }).getByRole('button', { name: 'Manage intro', exact: true }).click();
  const dialog = admin.getByRole('dialog', { name: 'Intro interval', exact: true });
  await dialog.getByLabel('Intro start (seconds)', { exact: true }).fill('8');
  await dialog.getByLabel('Intro end (seconds)', { exact: true }).fill('3');
  check(!await dialog.getByRole('button', { name: 'Save intro', exact: true }).isEnabled(), 'invalid_intro_interval_writable');
  await dialog.getByLabel('Import intro JSON', { exact: true }).setInputFiles({ name: 'owned-intro.json',
    mimeType: 'application/json', buffer: Buffer.from(JSON.stringify({ StartTicks: 20000000, EndTicks: 70000000 })) });
  const imported = await responseFor(admin, 'PUT', `/admin/v1/items/${fixture.MovieId}/intro`, () =>
    dialog.getByRole('button', { name: 'Save intro', exact: true }).click());
  check(imported.Effective.StartTicks === 20000000 && imported.Effective.EndTicks === 70000000 &&
    imported.OverrideSource === 'Import', 'imported_intro_not_persisted');
  await dialog.getByRole('button', { name: 'Reset to chapter markers', exact: true }).click();
  const reset = await responseFor(admin, 'DELETE', `/admin/v1/items/${fixture.MovieId}/intro`, () =>
    admin.getByRole('dialog', { name: 'Reset intro override?', exact: true }).getByRole('button', { name: 'Reset intro', exact: true }).click());
  check(reset.Effective === null && reset.Override === null, 'intro_reset_did_not_remove_override');
  await dialog.getByLabel('Intro start (seconds)', { exact: true }).fill('3');
  await dialog.getByLabel('Intro end (seconds)', { exact: true }).fill('8');
  const saved = await responseFor(admin, 'PUT', `/admin/v1/items/${fixture.MovieId}/intro`, () =>
    dialog.getByRole('button', { name: 'Save intro', exact: true }).click());
  check(saved.Effective.StartTicks === 30000000 && saved.Effective.EndTicks === 80000000 &&
    saved.OverrideSource === 'Manual' && saved.SourceRevision && saved.Revision !== '0', 'intro_not_persisted');
  result.IntroAdministration = { InvalidIntervalRejected: true, ImportSaved: true, ResetCleared: true, ManualSaved: true };
  await dialog.getByText('Intro interval saved for this media source.', { exact: true }).waitFor();
  await admin.screenshot({ path: path.join(fixture.ArtifactsDir, 'native-intro-desktop.png') });
  result.Screenshots.push('native-intro-desktop.png');
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
}

// These listeners observe platform media events and decoded frame counts. They
// never assign currentTime, playbackRate, player state, or synthetic events.
async function installMediaObserver(context) {
  await context.addInitScript(() => {
    const observations = [];
    Object.defineProperty(window, '__gobyPhase1MediaObservations', { value: observations });
    for (const event of ['playing', 'seeking', 'seeked', 'ended', 'pause', 'timeupdate']) document.addEventListener(event, target => {
      const video = target.target;
      if (!(video instanceof HTMLVideoElement) || observations.length >= 3000) return;
      observations.push({ Event: event, WallMilliseconds: performance.now(), Seconds: video.currentTime,
        Duration: Number.isFinite(video.duration) ? video.duration : null,
        DecodedFrames: video.getVideoPlaybackQuality().totalVideoFrames });
    }, true);
  });
}
function observePlayback(page) {
  const reports = [];
  page.on('response', response => {
    const request = response.request(), url = new URL(response.url());
    const match = /^(?:\/emby)?\/Sessions\/Playing(?:\/(Progress|Stopped))?\/?$/i.exec(url.pathname);
    if (url.origin !== fixture.BaseURL || request.method() !== 'POST' || !match || reports.length >= 1000) return;
    let body;
    try { body = request.postDataJSON(); } catch { return; }
    if (!body || ![fixture.MovieId, fixture.EpisodeOneId, fixture.EpisodeTwoId].includes(body.ItemId)) return;
    reports.push({ Event: { progress: 'Progress', stopped: 'Stopped' }[match[1]?.toLowerCase()] || 'Started', ItemId: body.ItemId, Status: response.status(),
      PlayIdHash: typeof body.PlaySessionId === 'string' ? hash(body.PlaySessionId) : null,
      PositionTicks: Number.isSafeInteger(body.PositionTicks) ? body.PositionTicks : null,
      PlayMethod: ['DirectPlay', 'DirectStream', 'Transcode'].includes(body.PlayMethod) ? body.PlayMethod : null });
  });
  return reports;
}
async function originalLogin(secret = fixture.LocalPassword, enableProfilePin = false) {
  const diagnostic = { Phase: currentPhase, Operation: 'create-context', Document: null,
    StartupResponses: [], AssetFailures: [], RequestFailures: [], ConsoleErrorKinds: [], ConsoleWarningKinds: [], ServiceWorkers: [] };
  (result.OriginalClientAttempts ??= []).push(diagnostic);
  let originalPage = null;
  async function failureScreenshot(name) {
    if (!originalPage || originalPage.isClosed()) { diagnostic.FailureScreenshotState = 'PageUnavailable'; return; }
    try {
      const url = new URL(originalPage.url());
      if (url.origin !== fixture.BaseURL || !url.pathname.startsWith('/web/')) {
        diagnostic.FailureScreenshotState = 'OutsideOwnedOriginalClient'; return;
      }
      const masks = [originalPage.locator('input,textarea,[contenteditable="true"]')];
      for (const value of [fixture.AdminPassword, fixture.UserPassword, fixture.LocalPassword, fixture.ProfilePin])
        if (value) masks.push(originalPage.getByText(value, { exact: false }));
      const bytes = await originalPage.screenshot({ fullPage: false, mask: masks, timeout: 2500 });
      if (bytes.length > (8 << 20)) { diagnostic.FailureScreenshotState = 'SizeLimit'; return; }
      const filename = `original-client-failure-${result.OriginalClientAttempts.length}-${name}.png`;
      await fs.writeFile(path.join(fixture.ArtifactsDir, filename), bytes,
        { mode: 0o600, flag: 'wx', signal: AbortSignal.timeout(1000) });
      diagnostic.FailureScreenshot = filename;
      diagnostic.FailureScreenshotState = 'CapturedWithSecretsMasked';
      result.Screenshots.push(filename);
    } catch { diagnostic.FailureScreenshotState = 'CaptureUnavailable'; }
  }
  const errorCause = error => {
    const message = String(error?.message ?? '');
    if (error?.safeCode) return error.safeCode;
    if (/Browser needs to be launched with the global proxy/i.test(message)) return 'ContextProxyRequiresLaunchProxy';
    if (/Target page, context or browser has been closed|Browser has been closed/i.test(message)) return 'BrowserTargetClosed';
    if (/Executable doesn.t exist/i.test(message)) return 'BrowserExecutableMissing';
    const network = /\bnet::(ERR_[A-Z_]+)\b/.exec(message);
    if (network) return network[1];
    const timeout = /Timeout (\d{1,6})ms exceeded/i.exec(message);
    if (timeout) return `TimeoutAfter${timeout[1]}Milliseconds`;
    if (error instanceof AggregateError) return 'AllRequiredAlternativesFailed';
    return 'UnclassifiedBrowserOperationError';
  };
  async function operation(name, action) {
    diagnostic.Operation = name;
    try { return await action(); }
    catch (error) {
      diagnostic.FailureOperation = name;
      diagnostic.ErrorKind = ['TimeoutError', 'Error', 'AggregateError'].includes(error.name) ? error.name : 'BrowserError';
      diagnostic.Cause = errorCause(error);
      if (error instanceof AggregateError) diagnostic.AlternativeCauses = error.errors.slice(0, 4).map(errorCause);
      if (!error.safeCode) error.safeCode = `original_client_${name.replaceAll('-', '_')}_failed`;
      await failureScreenshot(name);
      throw error;
    }
  }
  const context = await operation('create-context', () => guardedContext(true));
  context.on('serviceworker', worker => {
    const url = new URL(worker.url());
    if (diagnostic.ServiceWorkers.length < 8) diagnostic.ServiceWorkers.push({ OwnedOrigin: url.origin === fixture.BaseURL,
      Path: url.origin === fixture.BaseURL ? url.pathname : '{external}' });
  });
  await operation('install-media-observer', () => installMediaObserver(context));
  const page = await operation('create-page', () => context.newPage());
  originalPage = page;
  const reports = observePlayback(page);
  const session = { context, page, reports, AuthenticationRequests: 0 };
  const safeAssetPath = value => {
    const url = new URL(value);
    return url.origin === fixture.BaseURL && /^\/web\/[A-Za-z0-9_./-]{0,240}$/.test(url.pathname) ? url.pathname : null;
  };
  page.on('response', response => {
    const url = new URL(response.url());
    const pathname = url.pathname.replace(/^\/emby(?=\/)/i, '').toLowerCase();
    if (url.origin === fixture.BaseURL && ['/system/info/public', '/users/public', '/users/authenticatebyname'].includes(pathname) &&
      diagnostic.StartupResponses.length < 40) diagnostic.StartupResponses.push({ Path: pathname,
      Method: response.request().method(), Status: response.status() });
    const asset = safeAssetPath(response.url());
    if (asset && response.status() >= 400 && diagnostic.AssetFailures.length < 40)
      diagnostic.AssetFailures.push({ Path: asset, Status: response.status() });
  });
  page.on('requestfailed', request => {
    const asset = safeAssetPath(request.url());
    if (asset && diagnostic.RequestFailures.length < 40) diagnostic.RequestFailures.push({ Path: asset });
  });
  page.on('console', message => {
    if (message.type() === 'warning' && diagnostic.ConsoleWarningKinds.length < 40) {
      diagnostic.ConsoleWarningKinds.push(/Service Worker registration blocked by Playwright/i.test(message.text()) ? 'ServiceWorkerBlockedByPlaywright' : 'Other');
      return;
    }
    if (message.type() !== 'error' || diagnostic.ConsoleErrorKinds.length >= 40) return;
    const text = message.text();
    diagnostic.ConsoleErrorKinds.push(/Content Security Policy|content-security-policy|Refused to execute inline/i.test(text) ? 'ContentSecurityPolicy' :
      /MIME type|module script/i.test(text) ? 'ModuleMimeType' : /Failed to load resource/i.test(text) ? 'ResourceLoad' : 'Other');
  });
  page.on('request', request => {
    if (request.method() === 'POST' && /\/(?:Users\/AuthenticateByName|Users\/[^/]+\/Authenticate)\/?$/i.test(new URL(request.url()).pathname))
      session.AuthenticationRequests += 1;
  });
  const document = await operation('document-navigation', () => page.goto(`${fixture.BaseURL}/web/index.html`, { waitUntil: 'domcontentloaded' }));
  diagnostic.Document = { Status: document?.status() ?? null, Path: new URL(page.url()).pathname,
    ContentType: (document?.headers()['content-type'] ?? '').split(';', 1)[0] };
  check(document?.status() === 200 && diagnostic.Document.ContentType === 'text/html', 'original_client_document_failed');
  await operation('service-worker-ready', async () => {
    diagnostic.ServiceWorkerReady = await page.evaluate(() => {
      if (!navigator.serviceWorker) return { Ready: false, Reason: 'Unavailable' };
      return new Promise(resolve => {
        let registration, worker, complete = false;
        const states = [];
        const snapshot = reason => ({ Ready: Boolean(registration), Reason: reason,
          OwnedOrigin: registration ? new URL(registration.scope).origin === location.origin : false,
          ScopePath: registration ? new URL(registration.scope).pathname : null, ActiveState: worker?.state ?? null,
          ScriptPath: worker ? new URL(worker.scriptURL).pathname : null, ObservedStates: states.slice(),
          ControlsCurrentPage: Boolean(navigator.serviceWorker.controller) });
        const finish = reason => {
          if (complete) return;
          complete = true;
          clearTimeout(timer);
          worker?.removeEventListener('statechange', observeState);
          resolve(snapshot(reason));
        };
        const observeState = () => {
          if (worker && states[states.length - 1] !== worker.state) states.push(worker.state);
          if (worker?.state === 'activated') finish('Activated');
          else if (worker?.state === 'redundant') finish('Redundant');
        };
        const timer = setTimeout(() => finish('Timeout'), 10000);
        navigator.serviceWorker.ready.then(value => {
          if (complete) return;
          registration = value; worker = registration.active;
          if (!worker) { finish('NoActiveWorker'); return; }
          worker.addEventListener('statechange', observeState);
          observeState();
        }, () => finish('ReadyRejected'));
      });
    });
    check(diagnostic.ServiceWorkerReady.Ready === true && diagnostic.ServiceWorkerReady.OwnedOrigin === true &&
      diagnostic.ServiceWorkerReady.ScopePath === '/web/' && diagnostic.ServiceWorkerReady.ActiveState === 'activated',
    'original_client_service_worker_not_ready');
  });
  const form = page.locator('form:has(input[type="password"]:visible)');
  const manual = page.getByText('Manual Login', { exact: true });
  await operation('login-controls', () => Promise.any([form.waitFor({ state: 'visible', timeout: 15000 }), manual.waitFor({ state: 'visible', timeout: 15000 })]));
  if (!await form.isVisible()) await operation('manual-login', () => manual.locator('xpath=..').getByRole('button').click());
  await operation('username-input', () => form.locator('input[type="text"]:visible').fill(fixture.UserName));
  await operation('password-input', () => form.locator('input[type="password"]:visible').fill(secret));
  const authentication = page.waitForResponse(response => response.request().method() === 'POST' &&
    new URL(response.url()).origin === fixture.BaseURL && /\/(?:Users\/AuthenticateByName|Users\/[^/]+\/Authenticate)\/?$/i.test(new URL(response.url()).pathname));
  void authentication.catch(() => {});
  await operation('login-submit', () => form.getByRole('button', { name: 'Sign In', exact: true }).click());
  const response = await operation('authentication-response', () => authentication);
  result.Authentication.push({ CredentialKind: secret === fixture.LocalPassword ? 'LocalPassword' : 'NormalPassword', Status: response.status() });
  check(response.status() === 200, 'original_client_login_rejected');
  // The original router offers this per-device preference after authentication
  // when the server returns the current user's configured profile PIN.
  const confirmation = page.locator('.confirmDialog:visible');
  await operation('profile-pin-choice', async () => {
    await confirmation.locator('.formDialogHeaderTitle').filter({ hasText: /^Profile PIN$/ }).waitFor();
    await confirmation.locator(`.btnOption[data-id="${enableProfilePin ? 'ok' : 'cancel'}"]`).click();
  });
  await operation('authenticated-navigation', () => form.waitFor({ state: 'hidden' }));
  diagnostic.Operation = 'complete';
  return session;
}
async function originalLogout(session, reload = false) {
  if (!session) return;
  const page = session.page;
  await page.goto(`${fixture.BaseURL}/web/index.html#!/home?serverId=${encodeURIComponent(fixture.ServerId)}`);
  if (reload) await page.reload({ waitUntil: 'domcontentloaded' });
  await page.getByRole('button', { name: 'Settings', exact: true }).filter({ visible: true }).click();
  const waiting = page.waitForResponse(response => response.request().method() === 'POST' &&
    /\/(?:emby\/)?Sessions\/Logout\/?$/i.test(new URL(response.url()).pathname));
  void waiting.catch(() => {});
  await page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true }).click();
  check((await waiting).status() === 204, 'original_client_logout_rejected');
  await session.context.close();
}
async function openItem(session, id) {
  // The real detail route triggers normal item loading and queue construction.
  const url = new URL(session.page.url());
  url.hash = `!/item?id=${encodeURIComponent(id)}&serverId=${encodeURIComponent(fixture.ServerId)}`;
  await session.page.goto(url.toString());
  const play = session.page.locator('.btnPlay.btnMainPlay[data-mode="play"]:visible');
  await play.waitFor();
  await play.click();
  await session.page.waitForFunction(() => [...document.querySelectorAll('video')].some(video =>
    !video.paused && video.readyState >= 2 && video.videoWidth > 0 && video.currentTime > 0), null, { timeout: 30000 });
  await waitReports(session, reports => reports.some(report => report.Event === 'Started' && report.ItemId === id && report.Status === 204));
}
async function waitReports(session, predicate, timeout = 30000) {
  const until = Date.now() + timeout;
  while (Date.now() < until) { if (predicate(session.reports)) return; await sleep(100); }
  fail('original_playback_report_timeout');
}
async function observations(session) {
  return session.page.evaluate(() => window.__gobyPhase1MediaObservations.slice());
}
async function videoState(session) {
  return session.page.evaluate(() => {
    const videos = [...document.querySelectorAll('video')].filter(video => video.getClientRects().length);
    if (videos.length !== 1) return null;
    const video = videos[0];
    return { Seconds: video.currentTime, Duration: video.duration, Paused: video.paused, Ended: video.ended,
      DecodedFrames: video.getVideoPlaybackQuality().totalVideoFrames, Width: video.videoWidth };
  });
}
async function stopPlayback(session, itemId) {
  const video = session.page.locator('video:visible');
  if (await video.count()) {
    const box = await video.boundingBox();
    if (box) await session.page.mouse.move(box.x + box.width * .55, box.y + box.height * .65);
  }
  const stop = session.page.locator('.btnVideoOsd-stop:visible');
  if (await stop.count() === 1) await stop.click();
  else await session.page.locator('.headerBackButton:visible').click();
  await waitReports(session, reports => reports.some(report => report.Event === 'Stopped' && report.ItemId === itemId && report.Status === 204));
  await session.page.waitForFunction(() => [...document.querySelectorAll('video,audio')].every(media => media.paused), null, { timeout: 10000 });
}
async function introJourney(mode) {
  currentPhase = { ShowButton: 'intro-show-button', None: 'intro-none', AutoSkip: 'intro-auto-skip' }[mode];
  await preferences(mode, false);
  client = await originalLogin();
  await openItem(client, fixture.MovieId);
  const first = await videoState(client);
  check(first && first.Seconds < 3 && first.DecodedFrames > 0, 'intro_beginning_not_observed');
  const entitlementAttempts = result.BlockedEntitlementRequests;
  const skip = client.page.locator('.btnSkipIntro:visible');
  if (mode === 'ShowButton') {
    await skip.waitFor({ timeout: 10000 });
    const before = await videoState(client);
    check(before.Seconds >= 2.5 && before.Seconds < 8, 'skip_button_outside_intro');
    await skip.click();
  } else if (mode === 'None') {
    await client.page.waitForFunction(() => [...document.querySelectorAll('video')].some(video =>
      video.currentTime >= 4 && video.currentTime < 7), null, { timeout: 10000 });
    check(!await skip.count(), 'disabled_intro_button_inside_interval');
  }
  await client.page.waitForFunction(() => [...document.querySelectorAll('video')].some(video =>
    video.currentTime >= 8.25 && video.getVideoPlaybackQuality().totalVideoFrames > 10), null, { timeout: 15000 });
  let events = await observations(client);
  const after = await videoState(client);
  const meaningfulSeeks = events.filter(event => event.Event === 'seeking' && event.Seconds > 1);
  const seeks = events.filter(event => event.Event === 'seeking' && event.Seconds >= 7.8 && event.Seconds <= 8.5);
  const entitlementBlocked = mode !== 'None' && result.BlockedEntitlementRequests > entitlementAttempts;
  if (mode === 'None') {
    check(meaningfulSeeks.length === 0 && !await skip.count(), 'disabled_intro_seek_or_button');
    check(events.some(event => event.Event === 'timeupdate' && event.Seconds > 4 && event.Seconds < 7), 'disabled_intro_not_played');
  } else if (!entitlementBlocked) {
    check(seeks.length === 1 && meaningfulSeeks.length === 1, 'intro_seek_not_exactly_once');
    check(events.some(event => event.Event === 'seeked' && event.Seconds >= 7.8 && event.Seconds <= 8.7), 'intro_seek_not_completed');
  } else {
    check(seeks.length === 0, 'entitlement_blocked_with_unexplained_seek');
  }
  check(after.DecodedFrames > first.DecodedFrames && after.Width > 0, 'intro_decoded_frames_did_not_advance');
  if (entitlementBlocked) {
    // The original purchase dialog may cover playback controls. Let the owned
    // short media finish naturally, preserving its actual end/stop evidence.
    await waitReports(client, rows => rows.some(row => row.Event === 'Stopped' && row.ItemId === fixture.MovieId && row.Status === 204), 25000);
    events = await observations(client);
    check(events.some(event => event.Event === 'ended'), 'blocked_intro_media_did_not_end');
  } else await stopPlayback(client, fixture.MovieId);
  const rows = client.reports.slice();
  verifyLifecycle(rows, [fixture.MovieId]);
  result.Playback.push({ Scenario: currentPhase, Mode: mode, Events: events.filter(event => event.Event !== 'timeupdate'), Reports: rows,
    DecodedFramesBefore: first.DecodedFrames, DecodedFramesAfter: after.DecodedFrames, IntroSeekCount: seeks.length,
    EntitlementBlocked: entitlementBlocked });
  await originalLogout(client, entitlementBlocked); client = null;
  result.Checks[{ ShowButton: 'IntroShowButton', None: 'IntroNone', AutoSkip: 'IntroAutoSkip' }[mode]] = !entitlementBlocked;
  if (entitlementBlocked) result.BlockedStages.push({ Phase: currentPhase, Reason: 'original_client_entitlement' });
  await stage(currentPhase, entitlementBlocked ? 'blocked' : 'complete');
}
function verifyLifecycle(rows, expectedIds) {
  check(rows.length > 0 && rows.every(row => row.Status === 204 && row.PlayIdHash), 'playback_report_failed_or_unscoped');
  const starts = rows.filter(row => row.Event === 'Started');
  check(starts.length === expectedIds.length && new Set(starts.map(row => row.PlayIdHash)).size === starts.length,
    'playback_start_cardinality_or_identity_mismatch');
  for (const [index, itemId] of expectedIds.entries()) {
    check(starts[index].ItemId === itemId, 'playback_start_order_mismatch');
    const stopped = rows.filter(row => row.Event === 'Stopped' && row.ItemId === itemId && row.PlayIdHash === starts[index].PlayIdHash);
    check(stopped.length === 1, 'playback_stop_cardinality_or_identity_mismatch');
  }
  check(rows.every(row => starts.some(start => start.ItemId === row.ItemId && start.PlayIdHash === row.PlayIdHash)), 'playback_report_has_unstarted_identity');
}
async function nextJourney(enabled) {
  currentPhase = enabled ? 'next-enabled' : 'next-disabled';
  await preferences('None', enabled);
  client = await originalLogin();
  await openItem(client, fixture.EpisodeOneId);
  if (enabled) {
    await waitReports(client, rows => rows.some(row => row.Event === 'Started' && row.ItemId === fixture.EpisodeTwoId && row.Status === 204), 45000);
    await client.page.waitForFunction(() => [...document.querySelectorAll('video')].some(video =>
      !video.paused && video.currentTime > .5 && video.getVideoPlaybackQuality().totalVideoFrames > 5), null, { timeout: 15000 });
    await waitReports(client, rows => rows.some(row => row.Event === 'Stopped' && row.ItemId === fixture.EpisodeTwoId && row.Status === 204), 45000);
  } else {
    await waitReports(client, rows => rows.some(row => row.Event === 'Stopped' && row.ItemId === fixture.EpisodeOneId && row.Status === 204), 45000);
  }
  await sleep(2000);
  const starts = client.reports.filter(row => row.Event === 'Started');
  check(starts.length === (enabled ? 2 : 1), 'next_episode_start_cardinality_mismatch');
  check(starts[0].ItemId === fixture.EpisodeOneId && starts[0].PlayIdHash, 'next_episode_first_identity_mismatch');
  if (enabled) check(starts[1].ItemId === fixture.EpisodeTwoId && starts[1].PlayIdHash &&
    starts[1].PlayIdHash !== starts[0].PlayIdHash, 'next_episode_identity_or_replay_mismatch');
  verifyLifecycle(client.reports, enabled ? [fixture.EpisodeOneId, fixture.EpisodeTwoId] : [fixture.EpisodeOneId]);
  const events = await observations(client);
  check(events.filter(event => event.Event === 'ended').length >= (enabled ? 2 : 1), 'next_episode_without_real_media_end');
  result.Playback.push({ Scenario: currentPhase, Enabled: enabled, Reports: client.reports.slice(),
    Events: events.filter(event => event.Event !== 'timeupdate') });
  await originalLogout(client); client = null;
  result.Checks[enabled ? 'NextEnabled' : 'NextDisabled'] = true;
  await stage(currentPhase);
}

async function profilePinJourney(session) {
  // A profile PIN protects reuse of an already authenticated device profile.
  // The real UI navigation is deliberately required; storage edits, direct
  // module calls, or synthetic PIN comparisons are not acceptance substitutes.
  const page = session.page;
  const authentications = session.AuthenticationRequests;
  await page.reload({ waitUntil: 'domcontentloaded' });
  const prompt = page.locator('.profilePinDialogContentInner:visible');
  await prompt.waitFor({ timeout: 15000 });
  const inputs = prompt.locator('.txtProfilePinInput');
  check(await inputs.count() === 4, 'profile_pin_prompt_shape_mismatch');
  async function enterPin(value) {
    for (let index = 0; index < 4; index += 1) await inputs.nth(index).fill('');
    for (let index = 0; index < 4; index += 1) {
      await inputs.nth(index).pressSequentially(value[index]);
    }
  }
  await enterPin(fixture.ProfilePin === '0000' ? '1111' : '0000');
  await prompt.locator('.invalidHeader:not(.hide)').waitFor();
  check(await prompt.isVisible(), 'wrong_profile_pin_dismissed_prompt');
  await enterPin(fixture.ProfilePin);
  await prompt.waitFor({ state: 'hidden' });
  check(session.AuthenticationRequests === authentications, 'profile_pin_replaced_server_authentication');
  await page.getByRole('button', { name: 'Settings', exact: true }).filter({ visible: true }).waitFor();
  result.ProfileLock = { EnabledByOriginalLoginPrompt: true, StoredSessionReload: true,
    WrongPinRejected: true, CorrectPinAccepted: true, NoReplacementAuthentication: true };
}

async function main() {
  check(process.platform === 'linux' && process.getuid() === 0, 'linux_owned_fixture_required');
  privateContextPath = process.env.GOBY_SELECTED_PHASE1_CONTEXT;
  check(typeof privateContextPath === 'string' && path.isAbsolute(privateContextPath), 'context_required');
  await privateDirectory(path.dirname(privateContextPath));
  fixture = await privateJSON(privateContextPath);
  check(fixture.Marker === 'goby-selected-phase1-browser-fixture-v1' && fixture.RunId === process.env.GOBY_SELECTED_PHASE1_RUN_ID &&
    /^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$/.test(fixture.RunId), 'fixture_identity_mismatch');
  result.RunId = fixture.RunId;
  const origin = new URL(fixture.BaseURL);
  check(origin.protocol === 'http:' && origin.hostname === '127.0.0.1' && origin.origin === fixture.BaseURL &&
    Number(origin.port) > 1024 && ![5432, 8096, 8920, 18196, 18198].includes(Number(origin.port)), 'owned_origin_required');
  check(fixture.ArtifactsDir === path.dirname(privateContextPath) &&
    fixture.ResultPath === path.join(fixture.ArtifactsDir, 'browser-result.json'), 'artifact_binding_mismatch');
  for (const name of ['AdminName', 'AdminPassword', 'UserName', 'UserPassword', 'LocalPassword', 'ProfilePin', 'ServerId',
    'UserId', 'MovieId', 'MovieName', 'MovieLibraryId', 'EpisodeOneId', 'EpisodeTwoId'])
    check(typeof fixture[name] === 'string' && fixture[name].length > 0 && fixture[name].length <= 4096, 'fixture_field_missing');
  const modulePath = process.env.GOBY_TEST_PLAYWRIGHT_MODULE;
  check(typeof modulePath === 'string' && path.isAbsolute(modulePath), 'playwright_module_required');
  const { chromium } = createRequire(import.meta.url)(modulePath);
  await startNetworkGuard();
  browser = await chromium.launch({ headless: true, args: ['--autoplay-policy=no-user-gesture-required', '--disable-background-networking'] });
  deadline = setTimeout(() => { currentPhase = `${currentPhase}-deadline`; void browser.close(); }, 480000);
  const adminContext = await guardedContext();
  admin = await adminContext.newPage();
  await administratorLogin();
  currentPhase = 'admin-credentials'; await credentials(); result.Checks.AdminCredentials = true; await stage(currentPhase);
  currentPhase = 'admin-preferences'; await preferences('ShowButton', true); result.Checks.AdminPreferences = true; await stage(currentPhase);
  currentPhase = 'admin-intro'; await introEditor(); result.Checks.AdminIntro = true; await stage(currentPhase);
  currentPhase = 'original-local-login'; client = await originalLogin(fixture.LocalPassword, true); result.Checks.OriginalLocalLogin = true; await stage(currentPhase);
  currentPhase = 'profile-pin'; await profilePinJourney(client); result.Checks.ProfilePin = true; await stage(currentPhase);
  await originalLogout(client); client = null;
  await nextJourney(true); await nextJourney(false);
  for (const mode of ['ShowButton', 'None', 'AutoSkip']) await introJourney(mode);
  await stage('restart');
  currentPhase = 'persisted';
  await admin.goto(`${fixture.BaseURL}/admin/users`);
  const preferencesDialog = await userDialog('Playback and display preferences', 'Playback and display preferences');
  check(await preferencesDialog.getByRole('combobox', { name: 'Intro skipping', exact: true }).innerText() === 'Skip automatically' &&
    !await preferencesDialog.getByRole('checkbox', { name: 'Automatically play the next episode', exact: true }).isChecked(), 'restart_preferences_changed');
  client = await originalLogin();
  result.Checks.RestartPersisted = true; await stage(currentPhase);
  currentPhase = 'credentials-cleared'; await credentials(true);
  const revokedResponse = client.page.waitForResponse(response => response.status() === 401 && new URL(response.url()).origin === fixture.BaseURL);
  void revokedResponse.catch(() => {});
  await client.page.reload({ waitUntil: 'domcontentloaded' });
  await revokedResponse;
  await Promise.any([client.page.getByText('Manual Login', { exact: true }).waitFor({ state: 'visible', timeout: 10000 }),
    client.page.locator('form:has(input[type="password"]:visible)').waitFor({ state: 'visible', timeout: 10000 })]);
  await client.context.close(); client = null;
  result.CredentialRevocation = { PreviouslyActiveLocalSessionRejected: true, AdministratorSessionRetained: true };
  result.Checks.CredentialsCleared = true; await stage(currentPhase);
  currentPhase = 'cleanup';
  await admin.getByRole('button', { name: 'Sign out', exact: true }).click();
  await admin.getByRole('heading', { name: 'Sign in to Goby', exact: true }).waitFor();
  await adminContext.close();
  result.Checks.Cleanup = true; await stage(currentPhase);
  check(result.PageErrors === 0 && result.ForeignRequests === result.BlockedEntitlementRequests &&
    result.NetworkGuard.UnexpectedTargetRequests === 0, 'browser_or_network_errors');
  result.Complete = result.BlockedStages.length === 0 && CHECKS.every(name => result.Checks[name]);
  if (!result.Complete) { result.FailureCode = 'original_client_entitlement'; process.exitCode = 1; }
}

try { await main(); }
catch (error) {
  result.FailurePhase = currentPhase;
  result.FailureCode = error.safeCode || 'browser_operation_failed';
  result.Complete = false;
  process.exitCode = 1;
} finally {
  clearTimeout(deadline);
  if (browser) await browser.close().catch(() => { result.Complete = false; process.exitCode = 1; });
  await closeNetworkGuard().catch(() => { result.Complete = false; result.FailureCode ??= 'browser_network_guard_cleanup_failed'; process.exitCode = 1; });
  if (fixture?.ResultPath) await writeJSON(fixture.ResultPath, result).catch(() => { result.Complete = false; process.exitCode = 1; });
  // Results intentionally omit raw exceptions, request URLs, credential values,
  // authentication bodies, client source, screenshots of PIN prompts, and traces.
  process.stdout.write(`${JSON.stringify({ Marker: result.Marker, RunId: result.RunId, Complete: result.Complete,
    FailurePhase: result.FailurePhase, FailureCode: result.FailureCode ?? null })}\n`);
}
