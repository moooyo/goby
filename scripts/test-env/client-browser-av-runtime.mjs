/** Run an original-client AV workflow inside a fresh, target-only browser. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import { createRequire } from 'node:module';

const PLAYWRIGHT = '/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright';
const LOOPBACK = new Set(['127.0.0.1', 'localhost', '[::1]']);
const SEGMENTS = new Set(('emby System Info Public Ping Users AuthenticateByName Authenticate Sessions Logout ' +
  'Capabilities Full Playing Progress Stopped Items Root Views Latest Resume PlaybackInfo Videos Audio stream universal ' +
  'original master main hls Subtitles Stream Images Primary Backdrop Logo Thumb Shows NextUp Seasons Episodes Library ' +
  'VirtualFolders Query DisplayPreferences UserSettings TypedSettings Configuration Partial Playback BitrateTest ' +
  'Devices Auth Keys Intros ThemeMedia AdditionalParts SpecialFeatures Similar ThumbnailSet Endpoint RemoteImages Artists AlbumArtists Genres Tags ' +
  'Studios Persons Search Hints UserData FavoriteItems PlayedItems HideFromResume PlayQueue Branding Localization ' +
  'Css Culture Countries Options Languages Ratings socket embywebsocket').toLowerCase().split(' '));
const fieldName = value => /^[A-Za-z][A-Za-z0-9_.-]{0,95}$/.test(value) ? value : '{redacted field name}';
const mediaType = value => /^[A-Za-z0-9!#$&^_.+-]+\/[A-Za-z0-9!#$&^_.+-]+/.exec(value ?? '')?.[0].toLowerCase() ?? null;

function networkOrigin(value) {
  const url = new URL(value);
  if (url.protocol === 'ws:') url.protocol = 'http:';
  if (url.protocol === 'wss:') url.protocol = 'https:';
  return url.origin;
}
function safeRoute(value) {
  try {
    const url = new URL(value);
    if (url.pathname.startsWith('/web/')) return '/web/{asset}';
    return url.pathname.split('/').map(segment => {
      if (!segment) return '';
      const match = /^([A-Za-z][A-Za-z0-9]*)(?:\.(?:mp4|mp3|m4a|aac|ts|m3u8|vtt|srt|jpg|png|webp))?$/i.exec(segment);
      return match && SEGMENTS.has(match[1].toLowerCase()) ? segment : '{value}';
    }).join('/');
  } catch { return '{unavailable}'; }
}
function queryFields(value) {
  try { return [...new Set([...new URL(value).searchParams.keys()].slice(0, 128).map(fieldName))].slice(0, 64); }
  catch { return []; }
}
function browseFlags(value) {
  try {
    const url = new URL(value);
    if (!/^(?:\/emby)?\/(?:Users\/[^/]+\/Items(?:\/[^/]+)?|Shows\/[^/]+\/(?:Seasons|Episodes))\/?$/i.test(url.pathname)) return null;
    const result = {};
    for (const name of ['IsSpecialSeason', 'IsSpecialEpisode', 'IsStandaloneSpecial', 'IsFolder', 'Recursive']) {
      const values = url.searchParams.getAll(name);
      if (!values.length) continue;
      result[name] = values.length === 1 && ['true', 'false'].includes(values[0]) ? values[0]
        : { rejected_nonliteral_or_duplicate: true, count: Math.min(values.length, 129) };
    }
    return Object.keys(result).length ? result : null;
  } catch { return null; }
}
async function privateDirectory(directory) {
  const info = await fs.lstat(directory);
  if (!info.isDirectory() || info.isSymbolicLink() || info.uid !== 0 || (info.mode & 0o077) ||
      await fs.realpath(directory) !== path.resolve(directory)) throw new Error('invalid_private_directory');
}

/**
 * The callback owns fixture preparation/restoration and must stop audio through
 * the original UI before returning. It receives no credentials or tokens.
 * This helper writes observation.json and returns its sanitized report. Failure
 * throws a sanitized Error with phase, report, and output properties; it never
 * prints an exception or automatically executes as a command-line program.
 * assertTarget pins ownership before network-bearing phases. finalizeReport
 * receives the settled request report after browser and proof shutdown.
 * runtimeExceptions is reserved for diagnostic workflows, never playback timing.
 */
export async function runGuardedAV({ url, credentialsPath, output, workflow, assertTarget, finalizeReport, runtimeExceptions = false }) {
  const started = Date.now();
  const report = { format: 1, mode: runtimeExceptions === true ? 'guarded-original-client-av-diagnostic' : 'guarded-original-client-av',
    diagnostic_only: runtimeExceptions === true, timing_not_acceptance: runtimeExceptions === true, started_at: new Date().toISOString(),
    requests: [], request_overflow: 0, snapshots: [], page_errors: [], page_error_count: 0,
    console_errors: [], console_error_count: 0, websocket_events: [],
    blocked_external: [], blocked_external_count: 0, network_policy_guard_errors: 0, logout_request_count: 0,
    login: { attempted: false, request_observed: false, http_status: null, result: 'not_attempted' },
    logout: { attempted: false, http_status: null, result: 'not_attempted' },
    target_assertions: { enabled: typeof assertTarget === 'function', checks: [], failed: false },
    network_policy: { target_only: true, allowed_non_network_schemes: ['data:', 'blob:'],
      websocket_mapping: 'ws to http and wss to https, preserving host and port', service_workers: 'allow',
      external_action: 'Abort or deny with no upstream connection; no replacement success responses or frames' },
    browse_query_policy: { allowed_literal_values: ['true', 'false'],
      allowed_names: ['IsSpecialSeason', 'IsSpecialEpisode', 'IsStandaloneSpecial', 'IsFolder', 'Recursive'],
      other_query_values: 'Not retained; duplicate or nonliteral boolean values are rejected' },
    evidence_boundary: 'Original UI login, workflow and logout; request field names, allowlisted browse boolean literals, and response status. No client source or authentication response bodies are read.' };
  let target, account, browser, context, page, guard, proof, privateOutput;
  let phase = 'configuration', actionPhase = null, failedPhase = null, submitted = false, targetPinFailed = false;
  let exceptionSession = null, exceptionChain = Promise.resolve(), exceptionStopping = false, exceptionPaused = false;
  let exceptionPauseHandler = null, exceptionResumeHandler = null, exceptionLimitApplied = false;
  let secrets = [];
  const sockets = new Set(), entries = new WeakMap();
  const elapsed = () => Date.now() - started;
  const observedPhase = () => phase === 'workflow' && actionPhase ? `workflow:${actionPhase}` : phase;
  function setActionPhase(value) {
    if (typeof value !== 'string' || !/^[a-z][a-z0-9_]{0,63}$/.test(value)) throw new Error('invalid_action_phase');
    actionPhase = value;
    (report.action_phases ??= []).push({ phase: observedPhase(), elapsed_ms: elapsed() });
  }
  async function assertTargetAt(label) {
    if (assertTarget === undefined) return;
    phase = `target_pin_${label}`;
    const entry = { phase, elapsed_ms: elapsed(), result: 'pending' };
    report.target_assertions.checks.push(entry);
    try { await assertTarget(); entry.result = 'passed'; }
    catch {
      targetPinFailed = report.target_assertions.failed = true;
      entry.result = 'target_ownership_unconfirmed';
      if (context && label !== 'after_shutdown') {
        try { await context.setOffline(true); report.target_assertions.network_after_failure = 'offline'; }
        catch { report.target_assertions.network_after_failure = 'offline_transition_failed'; }
      }
      throw new Error('target_ownership_unconfirmed');
    }
  }
  function redact(value, seen = new WeakSet()) {
    if (typeof value === 'string') return secrets.reduce((text, secret) => text.split(secret).join('[redacted credential]'), value);
    if (['bigint', 'function', 'symbol'].includes(typeof value)) return '[unsupported report value]';
    if (!value || typeof value !== 'object') return typeof value === 'number' && !Number.isFinite(value) ? null : value;
    if (seen.has(value)) return '[unavailable cyclic value]';
    seen.add(value);
    const result = Array.isArray(value) ? value.map(item => redact(item, seen))
      : Object.fromEntries(Object.entries(value).map(([key, item]) => [redact(key), redact(item, seen)]));
    seen.delete(value);
    return result;
  }
  function safeText(value) {
    return redact(String(value)).replace(/(?:https?|wss?|file|blob|data):[^\s<>"']+|\/\/[^\s<>"']+/gi, '[redacted URL]')
      .replace(/\/(?:[^\s<>"'?]+\/)*[^\s<>"'?]*\?[^\s<>"']*/g, '[redacted URL]')
      .replace(/\b(?:token|api_key|x-emby-token|x-mediabrowser-token|password)\s*[:=]\s*[^\s,;]+/gi, '[redacted secret field]')
      .replace(/\b[0-9a-f]{8}-[0-9a-f-]{27,}\b|\b[0-9a-f]{16,}\b|\b[A-Za-z0-9_-]{40,}\b/gi, '[redacted value]');
  }
  function exceptionControlError() {
    const diagnostic = report.runtime_exception_diagnostics;
    diagnostic.control_errors = Math.min(diagnostic.limit, diagnostic.control_errors + 1);
  }
  async function exceptionDeadline(operation) {
    let timer;
    try {
      return await Promise.race([operation, new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error('runtime_exception_control_timeout')), 5000);
      })]);
    } finally { clearTimeout(timer); }
  }
  async function drainExceptions() {
    for (;;) {
      const pending = exceptionChain;
      await pending;
      if (pending === exceptionChain) return;
    }
  }
  async function startRuntimeExceptions() {
    if (!runtimeExceptions) return;
    phase = 'runtime_exception_setup';
    const diagnostic = report.runtime_exception_diagnostics = { enabled: true, diagnostic_only: true, timing_not_acceptance: true,
      count: 0, limit: 256, limit_reached: false, errors: [], control_errors: 0, setup_complete: false, cleanup: 'not_started',
      boundary: 'Immediately resume pauses; retain only sanitized exception-description first lines. No source, stack, scope, object properties, or runtime values are inspected. Debugger timing is not playback acceptance.' };
    const session = exceptionSession = await exceptionDeadline(context.newCDPSession(page));
    exceptionResumeHandler = () => { exceptionPaused = false; };
    exceptionPauseHandler = event => {
      exceptionPaused = true;
      exceptionChain = exceptionChain.then(async () => {
        try {
          if (!exceptionStopping && diagnostic.count < diagnostic.limit) {
            diagnostic.count += 1;
            const line = typeof event.data?.description === 'string'
              ? event.data.description.slice(0, 4096).split(/[\r\n]/, 1)[0] : '';
            const error = /^((?:Type|Reference|Range|Syntax|URI|Eval)?Error): (.*)$/.exec(line);
            if (error && diagnostic.errors.length < diagnostic.limit) diagnostic.errors.push({ elapsed_ms: elapsed(),
              name: safeText(error[1]), message: safeText(error[2]).slice(0, 1200) });
          }
          if (diagnostic.count >= diagnostic.limit && !exceptionLimitApplied) {
            exceptionLimitApplied = diagnostic.limit_reached = true;
            await exceptionDeadline(session.send('Debugger.setPauseOnExceptions', { state: 'none' }));
          }
        } finally {
          await exceptionDeadline(session.send('Debugger.resume'));
        }
      }).catch(() => { exceptionStopping = true; exceptionControlError(); });
    };
    session.on('Debugger.paused', exceptionPauseHandler);
    session.on('Debugger.resumed', exceptionResumeHandler);
    await exceptionDeadline(session.send('Debugger.enable'));
    await exceptionDeadline(session.send('Debugger.setPauseOnExceptions', { state: 'all' }));
    diagnostic.setup_complete = true;
  }
  async function stopRuntimeExceptions() {
    if (!exceptionSession) return;
    const session = exceptionSession, diagnostic = report.runtime_exception_diagnostics;
    exceptionStopping = true;
    diagnostic.cleanup = 'in_progress';
    let disabled = false, detached = false;
    try { await exceptionDeadline(session.send('Debugger.setPauseOnExceptions', { state: 'none' })); }
    catch { exceptionControlError(); }
    await exceptionDeadline(drainExceptions()).catch(exceptionControlError);
    if (exceptionPaused) {
      // A racing resumed event can make this best-effort resume unnecessary.
      await exceptionDeadline(session.send('Debugger.resume')).catch(() => {});
    }
    try { await exceptionDeadline(session.send('Debugger.disable')); disabled = true; }
    catch { exceptionControlError(); }
    await exceptionDeadline(drainExceptions()).catch(exceptionControlError);
    try { await exceptionDeadline(session.detach()); detached = true; }
    catch { exceptionControlError(); }
    session.off('Debugger.paused', exceptionPauseHandler);
    session.off('Debugger.resumed', exceptionResumeHandler);
    await exceptionDeadline(drainExceptions()).catch(exceptionControlError);
    exceptionSession = null;
    diagnostic.cleanup = disabled && detached ? 'disabled_and_detached' : 'control_cleanup_failed';
  }
  function allowed(value) {
    try {
      const parsed = new URL(value);
      return ['data:', 'blob:'].includes(parsed.protocol) || (!parsed.username && !parsed.password &&
        ['http:', 'https:', 'ws:', 'wss:'].includes(parsed.protocol) && networkOrigin(value) === target.origin);
    } catch { return false; }
  }
  const isRequest = (request, route) => {
    try { return request.method() === 'POST' && new URL(request.url()).origin === target.origin && route.test(new URL(request.url()).pathname); }
    catch { return false; }
  };
  const isLogin = request => isRequest(request, /^(?:\/emby)?\/users\/authenticatebyname\/?$/i);
  const isLogout = request => isRequest(request, /^(?:\/emby)?\/sessions\/logout\/?$/i);
  function blocked(value, method, transport) {
    report.blocked_external_count += 1;
    if (report.blocked_external.length < 256) report.blocked_external.push({ elapsed_ms: elapsed(),
      route: safeRoute(value), query_field_names: queryFields(value), method: fieldName(method), transport,
      action: 'blocked_without_upstream_connection' });
  }
  async function startGuard() {
    const deny = (request, socket, tunnel = false) => {
      const value = tunnel ? `https://${request.url}/` : request.url ?? '';
      if (allowed(value)) report.network_policy_guard_errors += 1;
      else blocked(value, request.method ?? 'GET', tunnel ? 'deny-only-connect-proxy' : 'deny-only-upgrade-proxy');
      socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\nContent-Length: 0\r\n\r\n');
    };
    guard = http.createServer({ maxHeaderSize: 16384 }, (request, response) => {
      if (allowed(request.url ?? '')) report.network_policy_guard_errors += 1;
      else blocked(request.url ?? '', request.method ?? 'GET', 'deny-only-http-proxy');
      response.writeHead(403, { Connection: 'close', 'Content-Length': '0' }); response.end();
    });
    guard.maxConnections = 32; guard.maxHeadersCount = 64; guard.headersTimeout = 5000; guard.requestTimeout = 5000;
    guard.on('connect', (request, socket) => deny(request, socket, true));
    guard.on('upgrade', (request, socket) => deny(request, socket));
    guard.on('clientError', (_error, socket) => socket.destroy());
    guard.on('connection', socket => { sockets.add(socket); socket.on('error', () => {}); socket.on('close', () => sockets.delete(socket)); });
    await new Promise((resolve, reject) => { guard.once('error', reject); guard.listen(0, '127.0.0.1', resolve); });
    return `http://127.0.0.1:${guard.address().port}`;
  }
  async function snapshot(name) {
    if (typeof name !== 'string' || !/^[A-Za-z][A-Za-z0-9_-]{0,79}$/.test(name)) throw new Error('invalid_snapshot_name');
    const data = await page.evaluate(() => ({ title: document.title, text: (document.body?.innerText ?? '').slice(0, 24000),
      controls: [...document.querySelectorAll('input,button,select,a,[role="button"]')].slice(0, 180).map(element => ({
        tag: element.tagName.toLowerCase(), type: element.getAttribute('type'), role: element.getAttribute('role'),
        label: element.getAttribute('aria-label'), text: (element.innerText ?? '').trim().slice(0, 180),
        disabled: Boolean(element.disabled), visible: Boolean(element.getClientRects().length) })) }));
    const filename = `${String(report.snapshots.length).padStart(3, '0')}-${name}.png`;
    data.title = safeText(data.title); data.text = safeText(data.text);
    for (const control of data.controls) for (const key of ['label', 'text']) if (control[key] !== null) control[key] = safeText(control[key]);
    const masks = [page.locator('input,textarea'), ...secrets.map(secret => page.getByText(secret, { exact: false })),
      page.getByText(/(?:https?:\/\/|wss?:\/\/)[^\s]*\?[^\s]*|(?:api_key|x-emby-token|x-mediabrowser-token|token)\s*[:=]\s*\S+/i)];
    await page.screenshot({ path: path.join(privateOutput, filename), fullPage: true, mask: masks, timeout: 10000 });
    await fs.chmod(path.join(privateOutput, filename), 0o600);
    const entry = redact({ name, elapsed_ms: elapsed(), route: safeRoute(page.url()), ...data, screenshot: filename });
    report.snapshots.push(entry);
    return entry;
  }
  async function signOut() {
    phase = 'logout';
    const video = page.locator('video:visible');
    if (await video.count()) {
      const box = await video.first().boundingBox();
      if (!box) throw new Error('video_surface_unavailable');
      await page.mouse.move(box.x + box.width * 0.45, box.y + box.height * 0.55, { steps: 4 });
      await page.mouse.move(box.x + box.width * 0.55, box.y + box.height * 0.75, { steps: 4 });
      await page.getByRole('button', { name: 'Back', exact: true }).filter({ visible: true }).click({ timeout: 8000 });
      await video.waitFor({ state: 'hidden', timeout: 10000 });
    }
    let control = page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true });
    if (!await control.count()) {
      await page.getByRole('button', { name: 'Settings', exact: true }).filter({ visible: true }).click({ timeout: 8000 });
      control = page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true });
    }
    await control.waitFor({ state: 'visible', timeout: 10000 });
    if (await control.count() !== 1) throw new Error('sign_out_control_ambiguous');
    const observed = page.waitForResponse(response => isLogout(response.request()), { timeout: 15000 }).catch(() => null);
    report.logout.attempted = true;
    await control.click({ timeout: 8000 });
    const response = await observed;
    report.logout.http_status = response?.status() ?? null;
    if (!response || response.status() < 200 || response.status() >= 300) throw new Error('logout_not_accepted');
    await Promise.any([page.getByText('Manual Login', { exact: true }).waitFor({ state: 'visible', timeout: 10000 }),
      page.locator('form:has(input[type="password"]:visible)').waitFor({ state: 'visible', timeout: 10000 })]);
    report.logout.result = 'ui_logout_http_accepted_and_login_view_visible';
    await snapshot('after-logout');
  }
  try {
    if (process.platform !== 'linux' || process.getuid?.() !== 0 || typeof workflow !== 'function' || typeof runtimeExceptions !== 'boolean' ||
        (assertTarget !== undefined && typeof assertTarget !== 'function') ||
        (finalizeReport !== undefined && typeof finalizeReport !== 'function') ||
        !path.isAbsolute(output ?? '') || !path.isAbsolute(credentialsPath ?? '')) throw new Error('invalid_runtime_configuration');
    target = new URL(url);
    if (target.protocol !== 'http:' || !LOOPBACK.has(target.hostname) || target.username || target.password || target.search || target.hash) {
      throw new Error('invalid_loopback_target');
    }
    process.umask(0o077);
    await privateDirectory(path.dirname(path.resolve(output)));
    await fs.mkdir(output, { mode: 0o700 });
    privateOutput = await fs.realpath(output);
    phase = 'credentials';
    await privateDirectory(path.dirname(path.resolve(credentialsPath)));
    const handle = await fs.open(credentialsPath, constants.O_RDONLY | constants.O_NOFOLLOW);
    const buffer = Buffer.alloc(16385);
    try {
      const info = await handle.stat();
      if (!info.isFile() || info.uid !== 0 || (info.mode & 0o777) !== 0o600 || info.nlink !== 1 || info.size < 1 || info.size > 16384) {
        throw new Error('invalid_credentials_file');
      }
      const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0);
      if (bytesRead !== info.size) throw new Error('credentials_changed');
      const parsed = JSON.parse(buffer.subarray(0, bytesRead).toString('utf8'));
      const valid = value => value && typeof value.username === 'string' && value.username.length > 0 && value.username.length <= 256 &&
        typeof value.password === 'string' && value.password.length <= 4096;
      if (!parsed || typeof parsed.marker !== 'string' || !parsed.marker || parsed.marker.length > 256 || !valid(parsed.admin) || !valid(parsed.viewer)) {
        throw new Error('invalid_credentials_schema');
      }
      secrets = [...new Set([parsed.admin.username, parsed.admin.password, parsed.viewer.username, parsed.viewer.password].filter(Boolean))]
        .sort((left, right) => right.length - left.length);
      const bound = new URL(parsed.base_url), direct = new URL(parsed.direct_url);
      if (bound.origin !== target.origin || bound.username || bound.password || bound.search || bound.hash || direct.protocol !== 'http:' ||
          !LOOPBACK.has(direct.hostname) || direct.username || direct.password || direct.search || direct.hash) throw new Error('credentials_origin_mismatch');
      account = { username: parsed.viewer.username, password: parsed.viewer.password };
      report.credentials = { account: 'viewer', bound_origin: bound.origin, owner_marker_present: true };
    } finally { buffer.fill(0); await handle.close(); }
    await assertTargetAt('before_browser');
    phase = 'browser_start';
    const require = createRequire(import.meta.url), { chromium } = require(PLAYWRIGHT);
    report.playwright_version = require(path.join(PLAYWRIGHT, 'package.json')).version;
    const guardOrigin = await startGuard();
    browser = await chromium.launch({ headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking'] });
    report.browser_version = browser.version();
    const authority = `${target.hostname}:${target.port || '80'}`;
    context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', serviceWorkers: 'allow',
      proxy: { server: guardOrigin, bypass: `<-loopback>,http://${authority},ws://${authority}` } });
    await context.route('**/*', async route => {
      try {
        if (targetPinFailed) {
          report.target_assertions.aborted_requests = (report.target_assertions.aborted_requests ?? 0) + 1;
          await route.abort('blockedbyclient'); return;
        }
        if (allowed(route.request().url())) { await route.continue(); return; }
        blocked(route.request().url(), route.request().method(), 'request-route');
        await route.abort('blockedbyclient');
      } catch { report.network_policy_guard_errors += 1; await route.abort('blockedbyclient').catch(() => {}); }
    });
    // Attach proof only after policy installation and before any page exists.
    const { createBrowserSessionProof } = await import('./client-browser-session-proof.mjs');
    proof = await createBrowserSessionProof({ context, target });
    report.session_proof = proof.report;
    context.on('request', request => {
      try {
        if (isLogin(request)) report.login.request_observed = true;
        if (isLogout(request)) report.logout_request_count += 1;
        if (report.requests.length >= 4000) { report.request_overflow += 1; return; }
        const headers = request.headers(), requestURL = new URL(request.url());
        const sameOrigin = networkOrigin(request.url()) === target.origin;
        const entry = { index: report.requests.length, elapsed_ms: elapsed(), phase: observedPhase(), method: request.method(),
          origin: ['data:', 'blob:'].includes(requestURL.protocol) ? 'non-network' : sameOrigin ? 'target' : 'external',
          route: safeRoute(request.url()), query_field_names: queryFields(request.url()), resource_type: request.resourceType(),
          content_type: mediaType(headers['content-type']), response_content_type: null, status: null };
        if (sameOrigin && request.method() === 'GET') entry.browse_query = browseFlags(request.url());
        if (sameOrigin && request.method() === 'POST' && /^(?:application\/(?:[a-z0-9.+-]+\+)?json|text\/plain|application\/x-www-form-urlencoded)$/.test(entry.content_type ?? '')) {
          const body = request.postDataBuffer();
          if (body && body.length <= 65536) {
            if (entry.content_type === 'application/x-www-form-urlencoded') entry.form_field_names = [...new Set([...new URLSearchParams(body.toString('utf8')).keys()].map(fieldName))].slice(0, 64);
            else try {
              const decoded = JSON.parse(body.toString('utf8'));
              if (decoded && typeof decoded === 'object' && !Array.isArray(decoded)) entry.json_field_names = Object.keys(decoded).slice(0, 64).map(fieldName);
            } catch { entry.body_field_names_unavailable = 'invalid_json'; }
          } else if (body) entry.body_field_names_unavailable = 'body_exceeds_limit';
        }
        if (sameOrigin && (request.resourceType() === 'media' || /^(?:\/emby)?\/(?:videos|audio)\//i.test(requestURL.pathname)) && headers.range) {
          entry.range = headers.range.length <= 128 && /^bytes=\d*-\d*(?:,\s*\d*-\d*){0,7}$/.test(headers.range) ? headers.range : '{invalid range}';
        }
        entries.set(request, entry); report.requests.push(entry);
      } catch { report.request_observer_errors = (report.request_observer_errors ?? 0) + 1; }
    });
    context.on('response', response => {
      try {
        const entry = entries.get(response.request());
        if (entry) {
          entry.status = response.status(); entry.response_content_type = mediaType(response.headers()['content-type']);
          entry.response_elapsed_ms = elapsed(); entry.response_phase = observedPhase();
        }
      } catch { report.request_observer_errors = (report.request_observer_errors ?? 0) + 1; }
    });
    context.on('requestfailed', request => {
      try {
        const entry = entries.get(request), failure = request.failure()?.errorText ?? '';
        if (entry) {
          entry.failure = /^net::ERR_[A-Z0-9_]+$/.test(failure) ? failure : 'browser_request_failed';
          entry.failure_elapsed_ms = elapsed(); entry.failure_phase = observedPhase();
        }
      } catch { report.request_observer_errors = (report.request_observer_errors ?? 0) + 1; }
    });
    context.on('requestfinished', request => {
      try {
        const entry = entries.get(request);
        if (entry) { entry.finished_elapsed_ms = elapsed(); entry.finished_phase = observedPhase(); }
      } catch { report.request_observer_errors = (report.request_observer_errors ?? 0) + 1; }
    });
    page = await context.newPage();
    await startRuntimeExceptions();
    page.on('websocket', socket => {
      if (report.websocket_events.length >= 100) return;
      const entry = { route: safeRoute(socket.url()), query_field_names: queryFields(socket.url()),
        opened_ms: elapsed(), sent_frames: 0, received_frames: 0, closed: false, error: false };
      report.websocket_events.push(entry);
      socket.on('framesent', () => { entry.sent_frames += 1; }); socket.on('framereceived', () => { entry.received_frames += 1; });
      socket.on('close', () => { entry.closed = true; }); socket.on('socketerror', () => { entry.error = true; });
    });
    page.on('pageerror', error => {
      report.page_error_count += 1;
      if (report.page_errors.length < 64) report.page_errors.push({ elapsed_ms: elapsed(), phase: observedPhase(),
        name: /^[A-Za-z]{1,50}Error$/.test(error.name ?? '') ? error.name : 'Error',
        message: safeText(String(error.message ?? '').split(/[\r\n]/, 1)[0]).slice(0, 1200) });
    });
    page.on('console', message => {
      if (message.type() !== 'error') return;
      report.console_error_count += 1;
      if (report.console_errors.length < 64) report.console_errors.push({ elapsed_ms: elapsed(), type: 'error',
        message: safeText(String(message.text()).split(/[\r\n]/, 1)[0]).slice(0, 1200) });
    });
    phase = 'navigation';
    const navigation = await page.goto(target.toString(), { waitUntil: 'domcontentloaded', timeout: 30000 });
    report.navigation = { status: navigation?.status() ?? null, result: 'domcontentloaded' };
    phase = 'manual_login';
    const form = page.locator('form:has(input[type="password"]:visible)'), manual = page.getByText('Manual Login', { exact: true });
    await Promise.any([form.waitFor({ state: 'visible', timeout: 10000 }), manual.waitFor({ state: 'visible', timeout: 10000 })]);
    if (!await form.isVisible()) await manual.locator('xpath=..').getByRole('button').click({ timeout: 10000 });
    await form.waitFor({ state: 'visible', timeout: 10000 });
    const username = form.locator('input[type="text"]:visible'), password = form.locator('input[type="password"]:visible');
    const submit = form.getByRole('button', { name: 'Sign In', exact: true });
    if (await username.count() !== 1 || await password.count() !== 1 || await submit.count() !== 1) throw new Error('login_controls_ambiguous');
    await snapshot('before-login');
    await username.fill(account.username); await password.fill(account.password);
    await assertTargetAt('before_login_submit');
    phase = 'login_submit';
    const loginResponse = page.waitForResponse(response => isLogin(response.request()), { timeout: 15000 }).catch(() => null);
    submitted = true; report.login.attempted = true; report.login.result = 'submitted_outcome_unknown';
    await submit.click({ timeout: 10000 });
    const authentication = await loginResponse;
    report.login.http_status = authentication?.status() ?? null;
    if (!authentication || authentication.status() < 200 || authentication.status() >= 300) {
      report.login.result = 'login_http_not_accepted'; throw new Error('login_not_accepted');
    }
    report.login.result = 'http_accepted';
    await password.waitFor({ state: 'hidden', timeout: 10000 });
    report.login.result = 'http_accepted_and_login_form_closed';
    await snapshot('after-login');
    await assertTargetAt('before_workflow');
    phase = 'workflow';
    await workflow({ page, context, target, report, snapshot, setActionPhase,
      requestObservation: request => entries.get(request) });
  } catch { failedPhase = phase; if (page) await snapshot('failure').catch(() => {}); }
  finally {
    try { await stopRuntimeExceptions(); }
    catch {
      failedPhase ??= 'runtime_exception_control';
      if (report.runtime_exception_diagnostics) report.runtime_exception_diagnostics.cleanup = 'control_cleanup_failed';
    }
    if (report.runtime_exception_diagnostics?.control_errors) failedPhase ??= 'runtime_exception_control';
    if (page && submitted && !targetPinFailed) {
      try { await assertTargetAt('before_logout'); await signOut(); }
      catch { failedPhase ??= phase; report.logout.result = 'ui_logout_not_completed'; }
    }
    if (targetPinFailed) {
      report.logout.result = 'skipped_target_ownership_unconfirmed';
      report.logout.reason = 'target_pin_failed_before_cleanup';
    }
    if (proof) {
      phase = 'session_proof';
      try {
        await proof.drain();
        if (proof.report.outcome !== 'all_observed_logout_tokens_rejected' || report.logout_request_count < 1 ||
            proof.report.entries.length !== report.logout_request_count) failedPhase ??= phase;
      } catch { failedPhase ??= phase; }
    }
    // Keep a private candidate until shutdown settles all browser event counts.
    // Successful runs remove it; any failure retains it without publishing it.
    const filename = 'private-browser-storage-state.json';
    let recoverySaved = false;
    if (context) {
      try {
        await context.storageState({ path: path.join(privateOutput, filename) });
        await fs.chmod(path.join(privateOutput, filename), 0o600);
        recoverySaved = true;
      } catch { failedPhase ??= 'storage_state_save'; }
    }
    if (browser) await browser.close().catch(() => { failedPhase ??= 'browser_close'; });
    if (guard) { for (const socket of sockets) socket.destroy(); await new Promise(resolve => guard.close(resolve)); }
    if (proof) {
      phase = 'session_proof';
      try {
        await proof.drain();
        if (proof.report.outcome !== 'all_observed_logout_tokens_rejected' || report.logout_request_count < 1 ||
            proof.report.entries.length !== report.logout_request_count) failedPhase ??= phase;
      } catch { failedPhase ??= phase; }
      await proof.dispose().catch(() => { failedPhase ??= phase; });
    }
    if (browser || proof) {
      try { await assertTargetAt('after_shutdown'); }
      catch { failedPhase ??= phase; }
    }
    if (report.network_policy_guard_errors) failedPhase ??= 'network_policy';
    if (report.request_observer_errors) failedPhase ??= 'request_observer';
    if (report.request_overflow > 0) failedPhase ??= 'request_overflow';
    if (typeof finalizeReport === 'function') {
      phase = 'finalize_report';
      report.failure = failedPhase;
      const finalization = report.finalization = { attempted: true, result: 'pending' };
      try { await finalizeReport(report); finalization.result = 'completed'; }
      catch { failedPhase ??= phase; finalization.result = 'report_finalization_failed'; }
      report.finalization = finalization;
    }
    if (recoverySaved && failedPhase) report.private_session_recovery = { filename, mode: '0600', publish: false, phase: failedPhase };
    if (!recoverySaved && failedPhase && context) report.private_session_recovery = { filename,
      result: 'storage_state_save_failed_or_incomplete', publish: false, phase: failedPhase };
    report.finished_at = new Date().toISOString(); report.elapsed_ms = elapsed(); report.failure = failedPhase;
    if (privateOutput) {
      try { await fs.writeFile(path.join(privateOutput, 'observation.json'), JSON.stringify(redact(report), null, 2) + '\n', { mode: 0o600, flag: 'wx' }); }
      catch { failedPhase ??= 'report_write'; report.failure = failedPhase;
        if (recoverySaved) report.private_session_recovery = { filename, mode: '0600', publish: false, phase: failedPhase }; }
    }
    if (recoverySaved && !failedPhase) {
      try { await fs.unlink(path.join(privateOutput, filename)); }
      catch {
        failedPhase = report.failure = 'recovery_remove';
        report.private_session_recovery = { filename, mode: '0600', publish: false, phase: failedPhase };
        await fs.writeFile(path.join(privateOutput, 'observation.json'), JSON.stringify(redact(report), null, 2) + '\n', { mode: 0o600 })
          .catch(() => {});
      }
    }
  }
  const safeReport = redact(report);
  if (failedPhase) {
    const error = Object.assign(new Error(`av_runtime_${failedPhase}_failed`), { phase: failedPhase, report: safeReport, output: redact(privateOutput ?? null) });
    error.stack = undefined;
    throw error;
  }
  return safeReport;
}
