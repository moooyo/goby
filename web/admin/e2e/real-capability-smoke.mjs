import { chromium, expect as baseExpect } from '@playwright/test';
import { lstat, mkdir, readFile, realpath, writeFile } from 'node:fs/promises';
import { isAbsolute, join, resolve } from 'node:path';
import { createHash, randomBytes } from 'node:crypto';
import { fileURLToPath } from 'node:url';

const expect = baseExpect.configure({ timeout: 20_000 });

// The caller provisions the disposable backend, account, library, and synthetic
// video. This runner never bootstraps accounts, scans directories, or loads
// saved credentials. It exercises only the explicitly selected playback.
const required = (name) => {
  const value = process.env[name];
  if (!value) throw new Error('Missing required environment variable: ' + name + '.');
  return value;
};
const ensure = (condition, message) => {
  if (!condition) throw new Error(message);
};
ensure(required('GOBY_CAPABILITY_SMOKE_CONFIRM') === 'disposable-task-environment',
  'Confirm the task-owned disposable environment before running this smoke.');
const target = new URL(required('GOBY_CAPABILITY_SMOKE_BASE_URL'));
ensure(['http:', 'https:'].includes(target.protocol)
  && ['localhost', '127.0.0.1', '[::1]'].includes(target.hostname)
  && Number(target.port) >= 1024 && Number(target.port) <= 65535
  && target.pathname === '/' && !target.username && !target.password && !target.search && !target.hash,
  'Use an explicit loopback origin on an unprivileged port, without a path, query, credentials, or fragment.');
const username = required('GOBY_CAPABILITY_SMOKE_USERNAME');
const password = required('GOBY_CAPABILITY_SMOKE_PASSWORD');
const sensitiveValues = new Set([password]);
let mediaId = process.env.GOBY_CAPABILITY_SMOKE_MEDIA_ID;
const freshSetupToken = process.env.GOBY_CAPABILITY_SMOKE_SETUP_TOKEN;
const freshMediaRoot = process.env.GOBY_CAPABILITY_SMOKE_MEDIA_ROOT;
const freshEnvironment = !mediaId;
if (freshEnvironment) {
  ensure(typeof freshSetupToken === 'string' && freshSetupToken.length >= 24
    && ['/home/moooyo/goby-admin-core-api-20261106/live/media', '/home/moooyo/goby-admin-design2-20261107/live/media', '/home/moooyo/goby-admin-design2-20261107/final-live/media', '/home/moooyo/goby-admin-design2-20261107/accepted-live/media'].includes(freshMediaRoot),
  'Fresh setup requires the protected token and exact task-owned core media root.');
  sensitiveValues.add(freshSetupToken);
} else ensure(/^[A-Za-z0-9_-]{1,256}$/.test(mediaId), 'Supply the opaque item ID of the task-created synthetic video.');
const outputParent = required('GOBY_CAPABILITY_SMOKE_OUTPUT');
ensure(isAbsolute(outputParent), 'The evidence parent directory must be absolute.');
const applicationLog = required('GOBY_CAPABILITY_SMOKE_APPLICATION_LOG');
const allowedLogWorkspaces = [
  '/home/moooyo/goby-admin-capabilities-20261106/live',
  '/home/moooyo/goby-admin-core-api-20261106/live',
  '/home/moooyo/goby-admin-core-api-20261106/final-live',
  '/home/moooyo/goby-admin-design2-20261107/live',
  '/home/moooyo/goby-admin-design2-20261107/final-live',
  '/home/moooyo/goby-admin-design2-20261107/accepted-live',
];
const platformPath = (path) => process.platform === 'win32'
  ? '\\\\wsl.localhost\\Debian' + path.replaceAll('/', '\\') : path;
const pathKey = (path) => process.platform === 'win32' ? resolve(path).toLowerCase() : resolve(path);
const logWorkspace = allowedLogWorkspaces.find((workspace) =>
  pathKey(platformPath(workspace + '/logs/application.log')) === pathKey(applicationLog));
ensure(isAbsolute(applicationLog) && logWorkspace,
  'Use the known task-owned application log to witness the real generation restart.');
for (const path of [platformPath(logWorkspace), platformPath(logWorkspace + '/logs'), applicationLog]) {
  ensure(!(await lstat(path)).isSymbolicLink() && pathKey(await realpath(path)) === pathKey(path),
    'The admitted task log and its workspace must not be symbolic links or redirect to another path.');
}
ensure((await lstat(applicationLog)).isFile(), 'The admitted task log must be a regular file.');
const scriptPath = fileURLToPath(import.meta.url);
const scriptSHA256 = createHash('sha256').update(await readFile(scriptPath)).digest('hex');
const runId = new Date().toISOString().replaceAll(/[:.]/g, '-') + '-' + randomBytes(3).toString('hex');
const output = join(resolve(outputParent), 'real-capability-smoke-' + runId);
await mkdir(resolve(outputParent), { recursive: true });
await mkdir(output, { recursive: false });

const start = Date.now();
const checks = [];
const screenshots = [];
const pageErrors = [];
const commands = [];
const observations = {};
let browser;
let administrator;
let player;
let administratorContext;
let playerContext;
let authenticated = false;
let outcome;
let embyAuthorization;
const url = (path) => new URL(path, target).href;
const redact = (value) => {
  let text = String(value);
  for (const secret of sensitiveValues) if (secret) text = text.replaceAll(secret, '[redacted]');
  return text.replaceAll(/wss?:\/\/[^\s"'<>]+/g, '[redacted WebSocket URL]')
    .replaceAll(/([?&](?:api_key|access_token|token|password)=)[^&\s"'<>]+/gi, '$1[redacted]')
    .replaceAll(/("(?:AccessToken|CSRFToken|Password|Pw)"\s*:\s*")[^"]*"/gi, '$1[redacted]"')
    .slice(0, 2000);
};
const checkpoint = (name, facts = {}) => checks.push({ name, passed: true, ...facts });
async function eventually(name, inspect, acceptable, timeout = 20_000) {
  const expires = Date.now() + timeout;
  while (Date.now() < expires) {
    const value = await inspect();
    if (acceptable(value)) return value;
    await new Promise((done) => setTimeout(done, 150));
  }
  throw new Error(name + ' did not reach its expected observed state.');
}
async function nativeJSON(path) {
  const response = await administratorContext.request.get(url(path), { timeout: 15_000 });
  ensure(response.ok(), 'A native administrator read failed: ' + path + ' (' + response.status() + ').');
  return response.json();
}
async function coreRequest(path, method = 'GET', body) {
  ensure(embyAuthorization, 'A real Emby administrator login is required for the core contract.');
  return playerContext.request.fetch(url(path), {
    method, timeout: 15_000, headers: { Accept: 'application/json', Origin: target.origin, 'X-Emby-Authorization': embyAuthorization },
    ...(body === undefined ? {} : { data: body }),
  });
}
async function coreJSON(path) {
  const response = await coreRequest(path);
  ensure(response.ok(), 'A real Emby administrator read failed: ' + path + ' (' + response.status() + ').');
  return response.json();
}
async function snapshot() {
  const value = await nativeJSON('/admin/v1/playback/sessions');
  ensure(Array.isArray(value.Items) && typeof value.ObservedAt === 'string', 'The native playback snapshot was incomplete.');
  return value;
}
async function playingSession() {
  const value = await snapshot();
  const matching = value.Items.filter((entry) => entry.NowPlayingItem?.Id === mediaId);
  ensure(matching.length <= 1, 'Multiple players use this task video; the disposable environment is not isolated.');
  return matching[0];
}
async function capture(page, name) {
  const filename = name + '.png';
  await page.screenshot({ path: join(output, filename), fullPage: true, animations: 'disabled' });
  screenshots.push(filename);
}
async function applicationFacts() {
  const rows = (await readFile(applicationLog, 'utf8')).split(/\r?\n/).flatMap((line) => {
    try { return [JSON.parse(line)]; } catch { return []; }
  });
  return {
    restartCount: rows.filter((entry) => entry.msg === 'administrator restart rebuilding application generation').length,
    listeningCount: rows.filter((entry) => entry.msg === 'server listening').length,
  };
}
function ticks(value) {
  if (typeof value === 'string' && /^(?:0|[1-9]\d{0,18})$/.test(value)) return BigInt(value);
  if (typeof value === 'number' && Number.isSafeInteger(value) && value >= 0) return BigInt(value);
  return undefined;
}
function playbackFingerprint(action, playSessionId) {
  ensure(typeof playSessionId === 'string' && playSessionId.startsWith('play_'), 'The activity witness requires the canonical playback lifetime.');
  return createHash('sha256').update('goby/playback-observation/v1\0' + action + '\0' + playSessionId).digest('hex');
}
async function verifyPlaybackActivity(playSessionId, userId, itemName, notBefore, notAfter, phase) {
  const native = {};
  for (const action of ['playback.started', 'playback.stopped']) {
    const fingerprint = playbackFingerprint(action, playSessionId);
    const query = new URLSearchParams({ Action: action, Limit: '200', MinDate: notBefore });
    const page = await eventually('The committed ' + action + ' activity', () => nativeJSON('/admin/v1/activity?' + query),
      (value) => Array.isArray(value.Items) && value.Items.some((entry) => entry.ObservationFingerprint === fingerprint));
    const matching = page.Items.filter((entry) => entry.ObservationFingerprint === fingerprint);
    ensure(matching.length === 1, 'A canonical playback transition was duplicated in native activity.');
    const entry = matching[0];
    const stamp = Date.parse(entry.Date);
    ensure(entry.Action === action && entry.Source === 'emby' && entry.Severity === 'Info'
      && entry.Actor?.Kind === 'user' && entry.Actor.Id === userId && entry.Actor.Name === username
      && entry.Resource?.Kind === 'item' && entry.Resource.Id === mediaId && entry.Resource.Name === itemName
      && Number.isFinite(stamp) && stamp >= Date.parse(notBefore) - 2000 && stamp <= Date.parse(notAfter) + 2000
      && entry.Count === '0' && entry.Revision === null && entry.State === null
      && Array.isArray(entry.ChangedFields) && entry.ChangedFields.length === 0,
    'Playback activity did not preserve its observed user, media, timestamp, or fixed fact shape.');
    native[action] = entry;
  }
  ensure(Date.parse(native['playback.stopped'].Date) >= Date.parse(native['playback.started'].Date),
    'The committed playback stop predates its start.');
  checkpoint('Real native playback activity retains user, item, and observed times', {
    phase, startedActivityId: native['playback.started'].Id, stoppedActivityId: native['playback.stopped'].Id,
    startedAt: native['playback.started'].Date, stoppedAt: native['playback.stopped'].Date,
  });
  const core = await coreJSON('/emby/System/ActivityLog/Entries?' + new URLSearchParams({ Limit: '200', MinDate: notBefore }));
  ensure(Array.isArray(core.Items), 'The core activity result was incomplete.');
  for (const [action, type] of [['playback.started', 'VideoPlayback'], ['playback.stopped', 'VideoPlaybackStopped']]) {
    const matching = core.Items.filter((entry) => String(entry.Id) === native[action].Id);
    ensure(matching.length === 1 && matching[0].Type === type && matching[0].ItemId === mediaId
      && matching[0].UserId === userId && Date.parse(matching[0].Date) === Date.parse(native[action].Date)
      && matching[0].Severity === 'Info', 'The Emby activity mapping lost a real playback association or date.');
  }
  checkpoint('Core activity maps actual VideoPlayback and VideoPlaybackStopped observations', { phase, itemId: mediaId, userId });
}
async function main() {
  browser = await chromium.launch({ headless: true, channel: process.env.GOBY_BROWSER_CHANNEL || undefined });
  administratorContext = await browser.newContext({
    viewport: { width: 1440, height: 1000 }, locale: 'en-US', timezoneId: 'Asia/Shanghai', reducedMotion: 'reduce',
  });
  playerContext = await browser.newContext({
    viewport: { width: 1440, height: 900 }, locale: 'zh-CN', timezoneId: 'Asia/Shanghai', reducedMotion: 'reduce',
  });
  // Independent contexts preserve the administrator-cookie / player-token
  // boundary. No storageState, traces, video, or request dumps are saved.
  await administratorContext.addInitScript(() => {
    localStorage.setItem('goby.admin.language', 'en');
    localStorage.setItem('goby.admin.theme', 'dark');
  });
  administrator = await administratorContext.newPage();
  player = await playerContext.newPage();
  for (const [name, page] of [['administrator', administrator], ['player', player]]) {
    page.on('pageerror', (error) => pageErrors.push({ page: name, message: redact(error.message) }));
    page.setDefaultTimeout(15_000);
    page.setDefaultNavigationTimeout(20_000);
  }

  if (freshEnvironment) {
    const status = await administratorContext.request.get(url('/admin/v1/bootstrap'));
    ensure(status.status() === 200 && (await status.json()).Initialized === false,
      'The fresh core smoke requires an uninitialized disposable database; no existing account was reused.');
    const bootstrapped = await administratorContext.request.post(url('/admin/v1/bootstrap'), {
      headers: { Origin: target.origin }, data: { SetupToken: freshSetupToken, Name: username, Password: password }, timeout: 20_000,
    });
    ensure(bootstrapped.status() === 201, 'The fresh task bootstrap did not acknowledge a newly created administrator.');
    const created = await bootstrapped.json();
    ensure(created.User?.Name === username && created.User.IsAdministrator === true,
      'The fresh task bootstrap returned a different administrator.');
    const completed = await administratorContext.request.get(url('/admin/v1/bootstrap'));
    ensure((await completed.json()).Initialized === true, 'The fresh bootstrap did not persist its initialized state.');
    checkpoint('Fresh disposable bootstrap creates a task administrator before actual UI sign-in', { userId: created.User.Id });
  }
  await administrator.goto(url('/admin/access/sessions'));
  await administrator.getByLabel('Username', { exact: true }).fill(username);
  await administrator.getByLabel('Password', { exact: true }).fill(password);
  await administrator.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(administrator.getByRole('heading', { name: 'Sessions', exact: true })).toBeVisible();
  const nativeSession = await nativeJSON('/admin/v1/session');
  ensure(nativeSession.User?.IsAdministrator === true && typeof nativeSession.CSRFToken === 'string',
    'The task account did not establish a native administrator session.');
  authenticated = true;
  const csrf = nativeSession.CSRFToken;
  sensitiveValues.add(csrf);
  const overviewBefore = await nativeJSON('/admin/v1/overview');
  checkpoint('Native administrator UI login', { userId: nativeSession.User.Id, serverId: overviewBefore.Server.Id });

  if (freshEnvironment) {
    const libraries = await nativeJSON('/admin/v1/libraries');
    ensure(Array.isArray(libraries.Items) && libraries.Items.length === 0, 'The fresh core database already contains a library.');
    const created = await administratorContext.request.post(url('/admin/v1/libraries'), {
      headers: { Origin: target.origin, 'X-CSRF-Token': csrf }, timeout: 20_000,
      data: {
        Name: 'Core Capability Smoke', CollectionType: 'movies', Paths: [freshMediaRoot], Scan: true,
        LibraryOptions: {
          EnableLocalMetadata: true, EnableLocalImages: false, EnableEmbeddedArtwork: false, EnableOnlineImages: false,
          EnableIntroDetection: false, EnablePreviewGeneration: false, EnableBackgroundPreviewGeneration: false,
          EnableAudioWaveformGeneration: false, EnableCreditsDetection: false, EnableSubtitleTimelineGeneration: false,
        },
      },
    });
    ensure(created.status() === 201, 'The authenticated first-library creation did not acknowledge its task-owned directory.');
    const library = await created.json();
    ensure(library.Library?.Id && library.Job?.Id && library.Job.LibraryId === library.Library.Id,
      'The first library did not acknowledge a real scan job.');
    const job = await eventually('The first real task media scan', async () => {
      const jobs = await nativeJSON('/admin/v1/jobs');
      return jobs.Items?.find((entry) => entry.Id === library.Job.Id);
    }, (value) => ['completed', 'failed', 'cancelled', 'interrupted'].includes(value?.Status), 120_000);
    ensure(job.Status === 'completed' && job.Scanned >= 1 && job.Added >= 1, 'The first task media scan did not actually add the synthetic source.');
    const items = await nativeJSON('/admin/v1/libraries/' + encodeURIComponent(library.Library.Id) + '/items?StartIndex=0&Limit=50');
    const selected = items.Items?.filter((item) => !item.IsFolder && item.Path === freshMediaRoot + '/Core Capability Smoke/Core Capability Smoke.webm') ?? [];
    ensure(selected.length === 1 && /^[A-Za-z0-9_-]{1,256}$/.test(selected[0].Id), 'The first scan did not produce the unique task-owned synthetic video.');
    mediaId = selected[0].Id;
    checkpoint('Authenticated first-library setup scans a real task source', { libraryId: library.Library.Id, mediaId, jobId: job.Id, scanStatus: job.Status });
  }
  const media = await nativeJSON('/admin/v1/items/' + encodeURIComponent(mediaId) + '/media-info');
  ensure(media.Id === mediaId && media.Summary?.State === 'available' && ticks(media.Summary.DurationTicks) > 0n
    && ticks(media.Summary.Size) > 0n && Array.isArray(media.Streams)
    && media.Streams.some((stream) => stream.Type === 'video'), 'The synthetic video has no real scanned media facts.');
  checkpoint('Real native media information', {
    mediaId: media.Id, libraryId: media.LibraryId, streamCount: media.Streams.length, chapterCount: media.ChapterCount,
    durationTicks: String(media.Summary.DurationTicks), sourceBytes: String(media.Summary.Size),
  });
  const policy = await nativeJSON('/admin/v1/server-policy');
  ensure(typeof policy.Revision === 'string' && ['zh-CN', 'en-US'].includes(policy.UICulture) && policy.Restart?.Available === true,
    'The independent server policy is unavailable.');
  const providers = await nativeJSON('/admin/v1/providers/configuration');
  ensure(typeof providers.Revision === 'string' && Array.isArray(providers.Items)
    && providers.Items.every((provider) => Array.isArray(provider.Fields)
      && provider.Fields.every((field) => !Object.hasOwn(field, 'Value'))),
    'Provider status exposed a value or was incomplete.');
  checkpoint('Real policy and redacted provider configuration reads');

  await player.goto(url('/'));
  await player.getByLabel('服务器地址', { exact: true }).fill(target.origin);
  await player.getByLabel('用户名', { exact: true }).fill(username);
  await player.getByLabel('密码', { exact: true }).fill(password);
  await player.getByRole('button', { name: '登录', exact: true }).click();
  await expect(player.getByRole('navigation', { name: '主导航', exact: true })).toBeVisible();
  const playerAuthority = await player.evaluate(() => {
    const saved = JSON.parse(localStorage.getItem('goby.player.session.v1') || 'null');
    return {
      token: saved?.AccessToken, userId: saved?.User?.Id, administrator: saved?.User?.Policy?.IsAdministrator === true,
      sessionId: saved?.SessionInfo?.Id, deviceId: localStorage.getItem('goby.player.device.v1'),
    };
  });
  ensure(typeof playerAuthority.token === 'string' && playerAuthority.token.length > 0
    && playerAuthority.userId === nativeSession.User.Id && playerAuthority.administrator
    && typeof playerAuthority.sessionId === 'string' && typeof playerAuthority.deviceId === 'string',
  'The actual player did not establish its independent Emby administrator authority.');
  sensitiveValues.add(playerAuthority.token);
  const headerValue = (value) => String(value).replace(/["\\\r\n]/g, '');
  embyAuthorization = 'Emby Client="Goby Player", Device="Web Browser", DeviceId="' + headerValue(playerAuthority.deviceId)
    + '", Version="1.0.0", Token="' + headerValue(playerAuthority.token) + '"';
  sensitiveValues.add(embyAuthorization);
  const coreInfo = await coreJSON('/emby/System/Info');
  ensure(coreInfo.Id === overviewBefore.Server.Id && coreInfo.CanSelfRestart === true && coreInfo.CanSelfUpdate === false,
    'Authenticated core system information did not expose the real managed restart capability.');
  checkpoint('Real Emby administrator login exposes CanSelfRestart');

  const coreConfiguration = await coreJSON('/emby/System/Configuration');
  const policyBefore = await nativeJSON('/admin/v1/server-policy');
  const settingsBefore = await nativeJSON('/admin/v1/settings');
  const samePolicyValues = (value) => value.EnableRemoteAccess === policyBefore.EnableRemoteAccess
    && value.UICulture === policyBefore.UICulture
    && Array.isArray(value.LocalNetworkSubnets) && JSON.stringify(value.LocalNetworkSubnets) === JSON.stringify(policyBefore.LocalNetworkSubnets);
  ensure(samePolicyValues(coreConfiguration), 'The core configuration did not project the actual native access policy.');
  checkpoint('Core configuration projects the real remote-access, LAN, and language values');
  const noOp = await coreRequest('/emby/System/Configuration/Partial', 'POST', {
    EnableRemoteAccess: coreConfiguration.EnableRemoteAccess,
    LocalNetworkSubnets: coreConfiguration.LocalNetworkSubnets,
    UICulture: coreConfiguration.UICulture,
  });
  ensure(noOp.status() === 204 && (await noOp.body()).length === 0, 'The core configuration no-op did not acknowledge the documented empty 204 response.');
  const coreConfigurationAfter = await coreJSON('/emby/System/Configuration');
  const policyAfter = await nativeJSON('/admin/v1/server-policy');
  const settingsAfter = await nativeJSON('/admin/v1/settings');
  const unrelatedSettings = (value) => ({
    Effective: value.Effective, Overrides: value.Overrides, ServerNameMode: value.ServerNameMode,
    Management: value.Management, Sorting: value.Sorting, Encoding: value.Encoding, RuntimeOverrides: value.Runtime?.Overrides,
  });
  ensure(samePolicyValues(coreConfigurationAfter) && samePolicyValues(policyAfter)
    && policyAfter.Revision === policyBefore.Revision && settingsAfter.Revision === settingsBefore.Revision
    && JSON.stringify(unrelatedSettings(settingsAfter)) === JSON.stringify(unrelatedSettings(settingsBefore)),
  'The safe core no-op changed a native revision, policy, or unrelated setting.');
  checkpoint('Core Partial no-op preserves both native revisions and unrelated settings', {
    policyRevision: policyAfter.Revision, settingsRevision: settingsAfter.Revision,
  });

  const updates = await nativeJSON('/admin/v1/server-updates');
  const unconfigured = (value) => value.State === 'unconfigured' && value.ConfiguredSource === false
    && value.CheckedAt === null && value.Release === null && value.ErrorCode === null
    && value.CanSelfUpdate === false && value.Delivery === 'docker';
  ensure(unconfigured(updates), 'An unconfigured update source fabricated a checked or current release.');
  checkpoint('Native update status truthfully remains unconfigured');
  const packages = await coreRequest('/emby/Packages/Updates?PackageType=System');
  const packagesError = await packages.json();
  ensure(packages.status() === 503 && packagesError.ResponseStatus?.ErrorCode === 'update_source_unconfigured',
    'The core package check did not distinguish an absent source from a verified empty update list.');
  ensure(unconfigured(await nativeJSON('/admin/v1/server-updates')), 'The absent-source core check invented a native update observation.');
  checkpoint('Core package updates returns explicit 503 for the absent release source', {
    status: packages.status(), errorCode: packagesError.ResponseStatus.ErrorCode,
  });

  const nativeCookies = (await administratorContext.cookies(url('/admin/'))).filter((cookie) => cookie.httpOnly && cookie.path === '/admin');
  ensure(nativeCookies.length === 1 && nativeCookies[0].value, 'The task native cookie could not be isolated for the audience-boundary check.');
  const nativeCookie = nativeCookies[0];
  sensitiveValues.add(nativeCookie.value);
  const cookieOnly = await administratorContext.request.post(url('/emby/System/Restart'), {
    headers: { Origin: target.origin, Cookie: nativeCookie.name + '=' + nativeCookie.value, 'X-CSRF-Token': csrf }, timeout: 15_000,
  });
  ensure(cookieOnly.status() === 401, 'The core restart accepted the native administrator cookie without an Emby token.');
  const cookieAsToken = await administratorContext.request.post(url('/emby/System/Restart'), {
    headers: { Origin: target.origin, 'X-Emby-Token': nativeCookie.value }, timeout: 15_000,
  });
  ensure(cookieAsToken.status() === 401 && (await nativeJSON('/admin/v1/server-policy')).Restart.InProgress === false,
    'The native cookie was accepted as an Emby token or consumed the restart latch.');
  checkpoint('Native cookie cannot authorize or impersonate an Emby restart token');

  const playbackWindowStart = (await snapshot()).ObservedAt;
  await player.goto(url('/#/player/' + encodeURIComponent(mediaId)));
  const video = player.locator('video.player-video');
  await expect(video).toBeVisible();
  await player.waitForFunction(() => {
    const video = document.querySelector('video.player-video');
    return video && video.readyState >= 2 && Number.isFinite(video.duration) && video.duration > 10;
  }, undefined, { timeout: 20_000 });
  if (await video.evaluate((element) => element.paused)) {
    const bigPlay = player.locator('button.player-big-play');
    if (await bigPlay.isVisible()) await bigPlay.click();
    else {
      await player.mouse.move(720, 820);
      await player.getByRole('button', { name: '播放', exact: true }).last().click();
    }
  }
  await eventually('The actual synthetic video', () => video.evaluate((element) => ({ paused: element.paused, time: element.currentTime })),
    (value) => !value.paused && value.time > 0);
  let observed = await eventually('The real player report', playingSession,
    (value) => Boolean(value?.TransportConnected && value.PlaySessionId && value.Controls?.Pause && value.Controls?.Seek && value.Controls?.Message && value.Controls?.Stop));
  ensure(observed.PlayState?.IsPaused === false && observed.PlayState.CanSeek === true,
    'The real player did not report active, seekable playback.');
  ensure(observed.Id === playerAuthority.sessionId, 'The observed player is not the actual Emby login used for core authority.');
  checkpoint('Authenticated WebSocket and real playback report', { playMethod: observed.PlayState.PlayMethod });

  await administrator.bringToFront();
  await administrator.reload();
  const row = administrator.locator('article.playback-row').filter({
    has: administrator.getByText(observed.NowPlayingItem.Name, { exact: true }),
  });
  await expect(row).toHaveCount(1);
  ensure(await row.getAttribute('data-playback-session') === observed.Id,
    'The management UI displayed a different playback session.');
  await expect(row.getByRole('button', { name: 'Pause playback', exact: true })).toBeEnabled();
  await capture(administrator, '01-real-now-playing');

  const commandPath = '/admin/v1/playback/sessions/' + encodeURIComponent(observed.Id) + '/command';
  async function uiCommand(command, submit) {
    const responsePromise = administrator.waitForResponse((response) =>
      new URL(response.url()).pathname === commandPath && response.request().method() === 'POST', { timeout: 20_000 });
    responsePromise.catch(() => undefined);
    await submit();
    const response = await responsePromise;
    ensure(response.status() === 202, 'The ' + command + ' command was not accepted.');
    const body = response.request().postDataJSON();
    ensure(response.request().headers()['x-csrf-token'] === csrf && body.Command === command,
      'The ' + command + ' UI action did not preserve its native request contract.');
    if (['Pause', 'Unpause', 'Seek', 'Stop'].includes(command)) {
      ensure(body.PlaySessionId === observed.PlaySessionId, 'The UI did not bind its control to the observed playback lifetime.');
    }
    const receipt = await response.json();
    ensure(receipt.SessionId === observed.Id && receipt.Command === command && receipt.Status === 'queued'
      && receipt.ExecutionConfirmed === false && receipt.DeliveryCount >= 1,
      'The command receipt was incomplete or claimed execution without a player report.');
    commands.push({ command, queued: true, executionConfirmedByReceipt: false });
  }

  await uiCommand('Pause', () => row.getByRole('button', { name: 'Pause playback', exact: true }).click());
  await eventually('The actual paused video', () => video.evaluate((element) => element.paused), (value) => value === true);
  observed = await eventually('The authenticated pause report', playingSession, (value) => value?.PlayState?.IsPaused === true);
  await expect(row.getByRole('button', { name: 'Resume playback', exact: true })).toBeEnabled();
  checkpoint('Pause confirmed independently by media and server report');
  await capture(administrator, '02-real-paused-report');

  await row.locator('details.playback-more > summary').click();
  await row.getByRole('button', { name: 'Seek to time', exact: true }).click();
  const seek = administrator.getByRole('dialog', { name: 'Seek to time', exact: true });
  const positionBeforeSeek = await video.evaluate((element) => element.currentTime);
  const seekSeconds = Math.abs(positionBeforeSeek - 7) > 1 ? 7 : 3;
  const requestedSeekTicks = BigInt(seekSeconds) * 10_000_000n;
  await seek.getByLabel('Playback time', { exact: true }).fill('0:' + String(seekSeconds).padStart(2, '0'));
  await uiCommand('Seek', () => seek.getByRole('button', { name: 'Request seek', exact: true }).click());
  await eventually('The actual seek position', () => video.evaluate((element) => element.currentTime), (value) => Math.abs(value - seekSeconds) < 0.2);
  observed = await eventually('The authenticated seek report', playingSession,
    (value) => value?.PlayState?.IsPaused === true && ticks(value.PlayState.PositionTicks) !== undefined
      && ticks(value.PlayState.PositionTicks) >= requestedSeekTicks - 2_000_000n && ticks(value.PlayState.PositionTicks) <= requestedSeekTicks + 2_000_000n);
  checkpoint('Seek confirmed independently by media and server report', { positionBeforeSeconds: positionBeforeSeek, requestedTicks: String(requestedSeekTicks), positionTicks: String(observed.PlayState.PositionTicks) });

  await uiCommand('Unpause', () => row.getByRole('button', { name: 'Resume playback', exact: true }).click());
  await eventually('The actual resumed video', () => video.evaluate((element) => ({ paused: element.paused, time: element.currentTime })),
    (value) => value.paused === false && value.time >= seekSeconds);
  observed = await eventually('The authenticated resume report', playingSession, (value) => value?.PlayState?.IsPaused === false);
  checkpoint('Resume confirmed independently by media and server report');

  const messageText = '<b>Task-owned Goby smoke message</b> ' + runId.slice(-6);
  await row.getByRole('button', { name: 'Message', exact: true }).click();
  const messageDialog = administrator.getByRole('dialog', { name: 'Message this player', exact: true });
  await messageDialog.getByRole('textbox', { name: 'Message', exact: true }).fill(messageText);
  await uiCommand('DisplayMessage', () => messageDialog.getByRole('button', { name: 'Send message', exact: true }).click());
  const notice = player.getByRole('status').filter({ hasText: messageText });
  await expect(notice).toBeVisible();
  await expect(notice.locator('b, img, script')).toHaveCount(0);
  await capture(player, '03-real-player-message');
  checkpoint('Remote message displayed as literal text by the actual player');
  await notice.getByRole('button', { name: '关闭消息', exact: true }).click();

  await administrator.bringToFront();
  await row.getByRole('button', { name: 'Stop', exact: true }).click();
  const stop = administrator.getByRole('dialog', { name: 'Stop this playback?', exact: true });
  await uiCommand('Stop', () => stop.getByRole('button', { name: 'Request stop', exact: true }).click());
  await expect(video).toHaveCount(0);
  await expect(player).toHaveURL(new RegExp('#/detail/' + mediaId + '$'));
  const firstStoppedSnapshot = await eventually('The authenticated stop report', snapshot,
    (value) => !value.Items.some((entry) => entry.Id === observed.Id && entry.NowPlayingItem));
  await expect(row).toHaveCount(0);
  checkpoint('Stop confirmed independently by player route and server report');
  await verifyPlaybackActivity(observed.PlaySessionId, nativeSession.User.Id, observed.NowPlayingItem.Name,
    playbackWindowStart, firstStoppedSnapshot.ObservedAt, 'before_restart');

  const beforeRestart = await applicationFacts();
  ensure(beforeRestart.listeningCount >= 1, 'The task application log did not witness its initial listener.');
  await administrator.goto(url('/admin/settings/general'));
  await expect(administrator.getByRole('button', { name: 'Restart server', exact: true })).toBeEnabled();
  const restartResponse = await coreRequest('/emby/System/Restart', 'POST');
  const restartBody = await restartResponse.body();
  ensure(restartResponse.status() === 200 && restartBody.length === 0,
    'The real Emby administrator restart did not acknowledge its documented empty 200 response.');
  checkpoint('Restart request accepted without claiming completion', { audience: 'emby', status: 200, bodyBytes: 0 });
  observations.beforeRestart = beforeRestart;
  const recovered = await eventually('The newly prepared application generation', async () => {
    try {
      const currentPolicy = await nativeJSON('/admin/v1/server-policy');
      const health = await nativeJSON('/readyz');
      const logs = await applicationFacts();
      observations.latestRestart = { ready: health.Status === 'ready', available: currentPolicy.Restart.Available, inProgress: currentPolicy.Restart.InProgress, ...logs };
      return { policy: currentPolicy, health, logs };
    } catch { observations.restartTransientDisconnectObserved = true; return undefined; }
  }, (value) => value?.health?.Status === 'ready' && value.policy.Restart.Available === true
    && value.policy.Restart.InProgress === false && value.logs.restartCount === beforeRestart.restartCount + 1
    && value.logs.listeningCount === beforeRestart.listeningCount + 1, 45_000);
  const nativeSessionAfter = await nativeJSON('/admin/v1/session');
  const overviewAfter = await nativeJSON('/admin/v1/overview');
  const librariesAfter = await nativeJSON('/admin/v1/libraries');
  const mediaAfter = await nativeJSON('/admin/v1/items/' + encodeURIComponent(mediaId) + '/media-info');
  ensure(nativeSessionAfter.User.Id === nativeSession.User.Id && overviewAfter.Server.Id === overviewBefore.Server.Id
    && librariesAfter.Items.some((entry) => entry.Id === media.LibraryId) && mediaAfter.Id === mediaId
    && mediaAfter.LibraryId === media.LibraryId, 'The generation restart changed persisted server, user, library, or media identity.');
  checkpoint('Actual generation restart and retained identities', {
    acceptedOnlyByReceipt: true,
    observedRestartCount: recovered.logs.restartCount,
    observedListenerCount: recovered.logs.listeningCount,
    healthRecovered: true,
  });
  await administrator.getByRole('button', { name: 'Reload saved settings', exact: true }).click();
  await expect(administrator.getByRole('button', { name: 'Restart server', exact: true })).toBeEnabled();
  await capture(administrator, '04-real-restarted-generation');

  const clientAfterRestart = await eventually('The real player reconnecting after the restart', snapshot,
    (value) => value.Items.some((entry) => entry.Id === observed.Id && entry.TransportConnected && entry.Controls?.Play));
  ensure(clientAfterRestart.Items.some((entry) => entry.Id === observed.Id),
    'The retained player login did not reconnect to the new generation.');
  await administrator.goto(url('/admin/access/sessions'));
  const available = administrator.locator('details.playback-idle');
  await expect(available).toBeVisible();
  await available.locator('summary').click();
  const idleTarget = available.locator('li').filter({ has: administrator.locator('.playback-device-status.is-connected') });
  await expect(idleTarget).toHaveCount(1);
  await idleTarget.getByRole('button', { name: 'Play media', exact: true }).click();
  const play = administrator.getByRole('dialog', { name: 'Play media on this device', exact: true });
  await expect(play.getByRole('radio')).toHaveCount(1);
  await play.getByRole('radio').check();
  const previousLifetime = observed.PlaySessionId;
  const secondPlaybackWindowStart = (await snapshot()).ObservedAt;
  await uiCommand('PlayNow', () => play.getByRole('button', { name: 'Send to player', exact: true }).click());
  await expect(player).toHaveURL(new RegExp('#/player/' + mediaId + '$'));
  await eventually('Actual playback controlled through the new generation', () => player.locator('video.player-video').evaluate((element) =>
    ({ paused: element.paused, ready: element.readyState })), (value) => value.ready >= 2 && value.paused === false);
  observed = await eventually('The new authenticated playback lifetime', playingSession,
    (value) => Boolean(value?.TransportConnected && value.PlaySessionId && value.PlaySessionId !== previousLifetime && value.Controls?.Stop));
  await expect(row).toHaveCount(1);
  await row.getByRole('button', { name: 'Stop', exact: true }).click();
  const secondStop = administrator.getByRole('dialog', { name: 'Stop this playback?', exact: true });
  await uiCommand('Stop', () => secondStop.getByRole('button', { name: 'Request stop', exact: true }).click());
  await expect(player.locator('video.player-video')).toHaveCount(0);
  const secondStoppedSnapshot = await eventually('The stop report in the rebuilt generation', snapshot,
    (value) => !value.Items.some((entry) => entry.Id === observed.Id && entry.NowPlayingItem));
  checkpoint('Retained controller and player used again after restart', { newPlaybackLifetime: true });
  await verifyPlaybackActivity(observed.PlaySessionId, nativeSession.User.Id, observed.NowPlayingItem.Name,
    secondPlaybackWindowStart, secondStoppedSnapshot.ObservedAt, 'after_restart');
  ensure(pageErrors.length === 0, 'A browser page error occurred during the real capability workflow.');
}

try {
  await main();
  outcome = { passed: true };
} catch (error) {
  outcome = { passed: false, error: redact(error instanceof Error ? error.message : error) };
  if (authenticated) {
    for (const [name, page] of [['administrator', administrator], ['player', player]]) {
      try { await capture(page, 'failure-' + name); } catch { /* Preserve earlier evidence without masking the original failure. */ }
    }
  }
} finally {
  await playerContext?.close().catch(() => undefined);
  await administratorContext?.close().catch(() => undefined);
  await browser?.close().catch(() => undefined);
  const endingScriptSHA256 = createHash('sha256').update(await readFile(scriptPath)).digest('hex');
  if (endingScriptSHA256 !== scriptSHA256) outcome = { passed: false, error: 'The smoke runner changed during execution.' };
  const receipt = {
    ...outcome, runId, targetOrigin: target.origin, taskOwnedEnvironmentConfirmed: true, usesRealBackend: true,
    freshEnvironment, mediaId, restartAudience: 'emby',
    fixtureInterception: false, observedExecutionRequired: true, traceRecorded: false, checks, commands, observations, pageErrors, screenshots,
    scriptSHA256, endingScriptSHA256, scriptUnchangedDuringExecution: endingScriptSHA256 === scriptSHA256,
    elapsedSeconds: Math.round((Date.now() - start) / 100) / 10,
  };
  await writeFile(join(output, 'receipt.json'), JSON.stringify(receipt, null, 2) + '\n', { flag: 'wx' });
  console.log(JSON.stringify({ passed: receipt.passed, checks: checks.length, commands: commands.length, evidence: output }));
  process.exitCode = receipt.passed ? 0 : 1;
}
