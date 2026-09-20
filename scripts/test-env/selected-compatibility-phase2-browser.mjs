#!/usr/bin/env node
/** Native administrator acceptance against real media workers and owned files. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { createHash } from 'node:crypto';

const phases = ['authentication', 'removal-ready', 'removal-applied', 'ocr-ready', 'ocr-reviewed',
  'ocr-applied', 'subtitle-selected', 'subtitle-off', 'subtitle-reselected', 'subtitle-stopped',
  'cancel-ready', 'cancelled', 'artwork', 'restart', 'persisted', 'cleanup'];
const checks = ['Authentication', 'RemovalPrepared', 'RemovalApplied', 'OCRRecognized', 'OCRReviewed',
  'OCRApplied', 'SubtitleSelected', 'SubtitleOff', 'SubtitleReselected', 'SubtitleStopped',
  'CancelPrepared', 'Cancelled', 'ArtworkObserved', 'RestartPersisted', 'HistoryPersisted', 'Cleanup'];
const result = { Marker: 'goby-selected-phase2-browser-result-v1', RunId: '', Complete: false,
  Stages: [], Checks: Object.fromEntries(checks.map(value => [value, false])), PageErrors: 0,
  PageErrorDetails: [], ForeignRequests: 0, HTTP: [], Operations: {}, CueImages: [], Screenshots: [],
  RetiredGETReads: 0, FailurePhase: null, FailureOperation: null };
const fail = code => { const error = new Error(code); error.safeCode = code; throw error; };
const requireThat = (condition, code) => { if (!condition) fail(code); };
const sha256 = bytes => createHash('sha256').update(bytes).digest('hex');
const sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
let fixture, browser, context, page, deadline, mediaContext, mediaPage;
let currentPhase = 'admission', currentOperation = 'read-context';
const operationIds = {}, readyOperations = {}, finalOperations = {};
const cueImageResponses = new Map();
const operationPath = id => `/admin/v1/media-operations/${id}`;

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
    const target = currentPhase.startsWith('subtitle-') && mediaPage && !mediaPage.isClosed() ? mediaPage : page;
    requireThat(target && !target.isClosed() && new URL(target.url()).origin === fixture.BaseURL, 'owned_page_required');
    const masks = [target.locator('input,textarea,[contenteditable]')];
    for (const secret of [fixture.AdminPassword, fixture.ViewerPassword]) if (secret) masks.push(target.getByText(secret, { exact: false }));
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
    if (error.phase2Captured) throw error;
    result.FailurePhase = currentPhase; result.FailureOperation = name;
    result.ErrorKind = ['Error', 'TimeoutError', 'AggregateError', 'TypeError'].includes(error.name) ? error.name : 'OperationError';
    if (!error.safeCode) error.safeCode = `native_${name.replaceAll('-', '_')}_failed`;
    await screenshot(`failure-${currentPhase}-${name}`, true);
    error.phase2Captured = true;
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
    requireThat(acknowledgement.Marker === 'goby-selected-phase2-stage-database-v1' &&
      acknowledgement.RunId === fixture.RunId && acknowledgement.Phase === phase, 'database_acknowledgement_binding_failed');
    if (acknowledgement.Failed === true) {
      result.DatabaseFailure = { Phase: phase, Code: 'database_stage_verification_failed' };
      fail('database_stage_verification_failed');
    }
    requireThat(acknowledgement.Complete === true && acknowledgement.ExpectedFilesVerified === true, 'database_acknowledgement_failed');
    result.Stages.push({ Phase: phase, State: 'complete' });
    if (phase === 'artwork') result.ArtworkHTTP = acknowledgement.ImageObservations;
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
function safeOperation(value) {
  return { Id: value.Id, Kind: value.Kind, State: value.State, Revision: value.Revision,
    ItemId: value.ItemId, SourceRevision: value.SourceRevision, StreamIndex: value.StreamIndex,
    ResultHash: value.ResultHash, PublicationPhase: value.PublicationPhase, Applied: value.Applied,
    Progress: value.Progress, ResultSummary: value.ResultSummary, ErrorCode: value.ErrorCode };
}
async function choose(parent, label, option) {
  await parent.getByRole('combobox', { name: label, exact: true }).click();
  await page.getByRole('option', { name: option, exact: true }).click();
}
const streamLabel = stream => `Stream ${stream.Index} · ${stream.Codec}${stream.Language ? ` · ${stream.Language}` : ''}${stream.Title ? ` · ${stream.Title}` : ''}`;
async function login() {
  await page.goto(`${fixture.BaseURL}/admin/`);
  await page.getByRole('heading', { name: 'Sign in to Goby', exact: true }).waitFor();
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill(fixture.AdminName);
  await page.locator('input#account-password[name="Password"]').fill(fixture.AdminPassword);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('navigation', { name: 'Administration', exact: true }).waitFor();
}
async function prepare(itemId, itemName, kind, selectStream) {
  await page.goto(`${fixture.BaseURL}/admin/libraries/${fixture.LibraryId}/items`);
  await page.getByRole('button', { name: `Edit metadata for ${itemName}`, exact: true }).click();
  const targetResponse = await responseFor('GET', `/admin/v1/items/${itemId}/media-processing`, () =>
    page.getByRole('dialog', { name: /^Edit metadata/ }).getByRole('button', { name: 'Media processing', exact: true }).click());
  const target = targetResponse.Body;
  requireThat(target.ItemId === itemId && target.Capabilities.Enabled && target.Capabilities.Available, 'media_processing_unavailable');
  const dialog = page.getByRole('dialog', { name: 'Prepare media processing', exact: true });
  await dialog.waitFor();
  await choose(dialog, 'Processing operation', kind === 'subtitle_ocr' ? 'Recognize bitmap subtitle (OCR)' : 'Remove embedded subtitle');
  const candidates = target.Streams.filter(stream => stream.CodecType === 'subtitle' && !stream.IsExternal && selectStream(stream));
  requireThat(candidates.length === 1, 'fixture_subtitle_stream_not_unique');
  const stream = candidates[0];
  await choose(dialog, 'Embedded subtitle stream', streamLabel(stream));
  if (kind === 'subtitle_ocr') {
    requireThat(target.Capabilities.OCR.Available && fixture.ModelIds.every(id => target.Capabilities.OCR.Models.some(model => model.Id === id)), 'ocr_inventory_unavailable');
    const labels = { eng: 'English', chi_sim: 'Simplified Chinese', chi_tra: 'Traditional Chinese' };
    for (const model of target.Capabilities.OCR.Models)
      await dialog.getByRole('checkbox', { name: `${labels[model.Language]} (${model.Id})`, exact: true }).uncheck();
    for (const id of fixture.ModelIds) {
      const model = target.Capabilities.OCR.Models.find(value => value.Id === id);
      await dialog.getByRole('checkbox', { name: `${labels[model.Language]} (${model.Id})`, exact: true }).check();
    }
    await choose(dialog, 'Subtitle output format', 'SRT');
    await dialog.getByRole('textbox', { name: 'Subtitle language', exact: true }).fill('eng');
    await dialog.getByRole('textbox', { name: 'Subtitle title', exact: true }).fill('Phase 2 reviewed subtitles');
    for (const label of ['Default subtitle', 'Forced subtitle', 'Hearing impaired subtitle'])
      await dialog.getByRole('checkbox', { name: label, exact: true }).uncheck();
  } else await choose(dialog, 'Writable container profile', 'Matroska');
  const accepted = await responseFor('POST', `/admin/v1/items/${itemId}/media-operations`, () =>
    dialog.getByRole('button', { name: 'Prepare for review', exact: true }).click(), 202);
  const job = accepted.Body.Operation;
  requireThat(accepted.Body.Admitted === true && /^[0-9a-f]{32}$/.test(job.Id) && job.Kind === kind &&
    job.ItemId === itemId && job.StreamIndex === stream.Index && job.SourceRevision === target.SourceRevision &&
    job.Applied === false && job.PublicationPhase === 'none', 'preparation_admission_mismatch');
  requireThat(accepted.Submitted.Kind === kind && accepted.Submitted.StreamIndex === stream.Index &&
    accepted.Submitted.MediaSourceId === target.MediaSourceId && accepted.Submitted.SourceRevision === target.SourceRevision,
  'preparation_request_not_source_bound');
  if (kind === 'subtitle_ocr') requireThat(JSON.stringify(accepted.Submitted.Parameters.ModelIds) === JSON.stringify(fixture.ModelIds), 'ocr_models_not_explicitly_selected');
  const detail = page.getByRole('dialog', { name: 'Media operation', exact: true });
  await detail.getByText(`Operation ${job.Id}`, { exact: true }).waitFor();
  return { job, dialog: detail };
}
async function waitState(dialog, id, wanted) {
  const until = Date.now() + 120000;
  while (Date.now() < until) {
    const response = await responseFor('GET', operationPath(id), () => dialog.getByRole('button', { name: 'Refresh operation', exact: true }).click());
    const job = response.Body.Operation;
    requireThat(job.Id === id, 'operation_identity_changed');
    result.Operations[id] = safeOperation(job);
    if (job.State === wanted) {
      const label = { ready: 'Ready for review', completed: 'Published', cancelled: 'Cancelled' }[wanted];
      await dialog.getByText(label, { exact: true }).first().waitFor();
      return job;
    }
    if (['failed', 'stale', 'interrupted', 'recovery_required', 'cancelled', 'completed'].includes(job.State)) fail(`operation_unexpected_${job.State}`);
    await sleep(1000);
  }
  fail('operation_state_timeout');
}
async function apply(dialog, ready) {
  requireThat(ready.State === 'ready' && ready.CanApply && !ready.Applied && /^[0-9a-f]{64}$/.test(ready.ResultHash), 'ready_result_required');
  await dialog.getByRole('button', { name: 'Apply reviewed result', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Apply reviewed result?', exact: true });
  await confirmation.waitFor();
  const admitted = await responseFor('POST', `${operationPath(ready.Id)}/apply`, () =>
    confirmation.getByRole('button', { name: 'Confirm apply', exact: true }).click(), 202);
  requireThat(admitted.Submitted.Revision === ready.Revision && admitted.Submitted.ResultHash === ready.ResultHash &&
    admitted.Submitted.SourceRevision === ready.SourceRevision, 'apply_did_not_bind_reviewed_result');
  const published = await waitState(dialog, ready.Id, 'completed');
  requireThat(published.Applied === true && published.PublicationPhase === 'done', 'operation_not_published');
  return published;
}
async function loadReview(dialog, id) {
  const review = dialog.getByRole('region', { name: 'OCR cue review', exact: true });
  await review.waitFor();
  const response = await responseFor('GET', `${operationPath(id)}/review`, () =>
    review.getByRole('button', { name: 'Reload review', exact: true }).click());
  requireThat(response.Body.Operation.Id === id && response.Body.TotalRecordCount >= 2 &&
    response.Body.Items.length === response.Body.TotalRecordCount && response.Body.StartIndex === 0,
  'owned_ocr_cue_set_not_fully_visible');
  return { review, data: response.Body };
}
async function inspectCueImages(review, id, cues) {
  for (const cue of cues) {
    requireThat(typeof cue.Confidence === 'number' && Number.isFinite(cue.Confidence) &&
      cue.Confidence >= 0 && cue.Confidence <= 100 && /^[0-9a-f]{64}$/.test(cue.ImageSHA256), 'real_ocr_confidence_or_image_missing');
    const section = review.getByRole('region', { name: `Cue ${cue.Ordinal + 1}`, exact: true });
    const image = section.getByRole('img', { name: `Original bitmap subtitle for cue ${cue.Ordinal + 1}`, exact: true });
    await image.scrollIntoViewIfNeeded();
    await image.waitFor();
    await page.waitForFunction(ordinal => {
      const image = document.querySelector(`img[alt="Original bitmap subtitle for cue ${ordinal + 1}"]`);
      return image instanceof HTMLImageElement && image.complete && image.naturalWidth > 1 && image.naturalHeight > 1;
    }, cue.Ordinal, { timeout: 15000 });
    await section.getByText(`Confidence: ${cue.Confidence.toFixed(1)}%`, { exact: false }).waitFor();
    const observed = await image.evaluate(element => ({ Width: element.naturalWidth, Height: element.naturalHeight, Path: new URL(element.src).pathname }));
    requireThat(observed.Path === `${operationPath(id)}/cues/${cue.Ordinal}/image`, 'cue_image_not_from_real_operation');
    const capture = cueImageResponses.get(observed.Path);
    requireThat(capture, 'cue_image_response_not_observed');
    const content = await capture;
    requireThat(content.Complete && content.SHA256 === cue.ImageSHA256, 'cue_image_bytes_do_not_match_recognition');
    result.CueImages.push({ OperationId: id, Ordinal: cue.Ordinal, SHA256: content.SHA256,
      Bytes: content.Bytes, Confidence: cue.Confidence, ...observed });
  }
}
async function history(id) {
  await page.goto(`${fixture.BaseURL}/admin/tasks`);
  const tab = page.getByRole('tab', { name: 'Media processing', exact: true });
  await tab.waitFor();
  if (await tab.getAttribute('aria-selected') !== 'true') await tab.click();
  const panel = page.getByRole('tabpanel', { name: 'Media processing', exact: true });
  await panel.getByRole('button', { name: `View media operation ${id}`, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Media operation', exact: true });
  await dialog.getByText(`Operation ${id}`, { exact: true }).waitFor();
  return dialog;
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

async function installSubtitleConsumer(streamIndex) {
  requireThat(Number.isInteger(streamIndex) && streamIndex >= 0, 'published_subtitle_stream_identity_missing');
  const bundlePath = fixture.HlsBundlePath;
  requireThat(typeof bundlePath === 'string' && path.isAbsolute(bundlePath) && await fs.realpath(bundlePath) === bundlePath &&
    fixture.HlsBundleSHA256 === '04a55387b26d6becff5b87b470b9b19c3fe41d25c0cd3c1e6c3d885a56572fdb', 'pinned_hls_engine_required');
  const handle = await fs.open(bundlePath, constants.O_RDONLY | constants.O_NOFOLLOW);
  let bundle;
  try {
    const stat = await handle.stat();
    requireThat(stat.isFile() && stat.uid === 0 && stat.nlink === 1 && (stat.mode & 0o022) === 0 &&
      stat.size >= 100000 && stat.size <= (8 << 20), 'hls_engine_identity_invalid');
    bundle = await handle.readFile('utf8');
  } finally { await handle.close(); }
  requireThat(sha256(bundle) === fixture.HlsBundleSHA256, 'hls_engine_hash_mismatch');
  mediaContext = await guardedContext();
  mediaPage = await mediaContext.newPage();
  const consumerResponse = await mediaPage.goto(`${fixture.BaseURL}/__selected-phase2-media`);
  requireThat(consumerResponse?.status() === 200, 'private_consumer_page_unavailable');
  // This is an explicitly private test consumer, not a product player or an
  // emulated media pipeline. HLS.js and HTMLVideoElement consume real responses.
  await mediaPage.evaluate(() => {
    window.define = (dependencies, factory) => {
      if (dependencies.length !== 1 || dependencies[0] !== 'exports') throw new Error('unexpected_hls_dependencies');
      const exports = {}; factory(exports); window.phase2Hls = exports.default;
    };
  });
  const [deliveredEngine] = await Promise.all([
    mediaPage.waitForResponse(response => new URL(response.url()).origin === fixture.BaseURL &&
      new URL(response.url()).pathname === '/__selected-phase2-hls.js'),
    mediaPage.addScriptTag({ url: `${fixture.BaseURL}/__selected-phase2-hls.js` }),
  ]);
  requireThat(deliveredEngine.status() === 200 && sha256(await deliveredEngine.body()) === fixture.HlsBundleSHA256,
    'delivered_hls_engine_hash_mismatch');
  const identity = await mediaPage.evaluate(async input => {
    const Hls = window.phase2Hls; delete window.define;
    if (Hls.version !== '1.6.0-beta.2' || !Hls.isSupported()) throw new Error('hls_engine_unavailable');
    const video = document.querySelector('#phase2-media');
    const canvas = document.querySelector('#phase2-pixel');
    const pixelContext = canvas.getContext('2d', { willReadFrequently: true });
    const device = `phase2-subtitles-${input.RunId}`;
    const authorization = `MediaBrowser Client="Goby Phase 2 Acceptance", Device="Owned Chromium", DeviceId="${device}", Version="1"`;
    let token = '', play, hls, stopped = false, frames = 0, presented = -1, browserFrames = 0, loaded = 0, subtitleFragments = 0;
    let selected = -1, transition = null;
    const fatalErrors = [], cueChanges = [], listeners = new Map();
    const request = async (target, method = 'GET', body) => {
      const url = new URL(target, input.BaseURL);
      if (url.origin !== input.BaseURL || url.username || url.password) throw new Error('consumer_origin_mismatch');
      const headers = { 'X-Emby-Authorization': authorization };
      if (token) headers['X-Emby-Token'] = token;
      if (body !== undefined) headers['Content-Type'] = 'application/json';
      return fetch(url.href, { method, headers, body: body === undefined ? undefined : JSON.stringify(body),
        cache: 'no-store', signal: AbortSignal.timeout(15000) });
    };
    const cues = track => Array.from(track.activeCues ?? []).map(cue => ({ Text: String(cue.text ?? '').slice(0, 512), Start: cue.startTime, End: cue.endTime }));
    const bindTracks = () => {
      for (const track of Array.from(video.textTracks)) {
        if (listeners.has(track)) continue;
        const changed = () => {
          if (cueChanges.length >= 64) cueChanges.shift();
          cueChanges.push({ MediaTime: video.currentTime, PresentedTime: presented, Language: track.language,
            Mode: track.mode, Active: cues(track) });
        };
        track.addEventListener('cuechange', changed); listeners.set(track, changed);
      }
    };
    video.textTracks.addEventListener('addtrack', bindTracks);
    const frame = (_now, metadata) => {
      frames++; presented = metadata.mediaTime; browserFrames = metadata.presentedFrames;
      if (!stopped) video.requestVideoFrameCallback(frame);
    };
    video.requestVideoFrameCallback(frame);
    const snapshot = () => {
      let pixel = [];
      if (video.readyState >= 2 && video.videoWidth > 0) {
        pixelContext.drawImage(video, 0, 0, 1, 1); pixel = Array.from(pixelContext.getImageData(0, 0, 1, 1).data).slice(0, 3);
      }
      return { MediaTime: video.currentTime, PresentedTime: presented, Frames: frames, BrowserFrames: browserFrames,
        Paused: video.paused, Seeking: video.seeking, ReadyState: video.readyState, Width: video.videoWidth, Height: video.videoHeight,
        ErrorCode: video.error?.code ?? 0, SelectedTrack: hls?.subtitleTrack ?? -1,
        Tracks: (hls?.subtitleTracks ?? []).map((track, index) => ({ Index: index, Language: track.lang ?? '', Name: track.name ?? '' })),
        ActiveCues: Array.from(video.textTracks).filter(track => track.mode === 'showing').flatMap(cues),
        CueChanges: cueChanges.slice(), FatalErrors: fatalErrors.slice(), LoadedFragments: loaded, SubtitleFragments: subtitleFragments,
        Pixel: pixel, Transition: transition };
    };
    const settle = (target, wantText) => new Promise((resolve, reject) => {
      const record = { TargetSeconds: target, StartedAt: performance.now(), FramesBefore: frames, BrowserFramesBefore: browserFrames,
        Seeked: target === null, PlayResolved: false, Completed: false, Failure: '', StableFrames: [] };
      transition = record;
      let callback, finished = false;
      const seeked = () => { record.Seeked = true; };
      const finish = reason => {
        if (finished) return;
        finished = true; clearTimeout(timer);
        if (callback !== undefined) video.cancelVideoFrameCallback(callback);
        video.removeEventListener('seeked', seeked);
        record.Completed = !reason; record.Failure = reason ?? '';
        if (reason) reject(new Error(reason)); else resolve(snapshot());
      };
      const observe = (now, metadata) => {
        if (finished) return;
        const current = { Number: metadata.presentedFrames, At: now, PresentedAt: metadata.presentationTime,
          MediaTime: video.currentTime, PresentedTime: metadata.mediaTime };
        const previous = record.StableFrames.at(-1);
        const live = !video.paused && !video.seeking && video.readyState >= 2 && !video.error && !fatalErrors.length &&
          record.Seeked && record.PlayResolved && frames > record.FramesBefore && metadata.presentedFrames > record.BrowserFramesBefore &&
          metadata.presentationTime >= record.StartedAt && Math.abs(video.currentTime - metadata.mediaTime) < .3;
        const advancing = !previous || current.Number > previous.Number && current.At > previous.At &&
          current.MediaTime > previous.MediaTime && current.PresentedTime > previous.PresentedTime;
        const active = Array.from(video.textTracks).filter(track => track.mode === 'showing').flatMap(cues);
        const desired = wantText === null || (wantText === '' ? active.length === 0 :
          active.some(cue => cue.Text === wantText && metadata.mediaTime >= cue.Start && metadata.mediaTime < cue.End));
        if (!live || !advancing || !desired) record.StableFrames = [];
        if (live && desired && (record.StableFrames.length > 0 || target === null || Math.abs(metadata.mediaTime - target) < .8)) record.StableFrames.push(current);
        if (record.StableFrames.length >= 3) { finish(); return; }
        callback = video.requestVideoFrameCallback(observe);
      };
      const timer = setTimeout(() => finish('actual_media_frames_or_cues_timeout'), 15000);
      video.addEventListener('seeked', seeked);
      if (target !== null) video.currentTime = target;
      video.play().then(() => {
        if (finished) return;
        record.PlayResolved = true; callback = video.requestVideoFrameCallback(observe);
      }, () => finish('actual_media_play_rejected'));
    });
    const stop = async () => {
      if (stopped) return { AlreadyStopped: true };
      video.pause(); hls?.destroy(); hls = undefined;
      video.removeAttribute('src'); video.load();
      for (const [track, listener] of listeners) track.removeEventListener('cuechange', listener);
      listeners.clear();
      let encodings = null, logout = null;
      try {
        if (play) encodings = (await request(`/emby/Videos/ActiveEncodings?DeviceId=${encodeURIComponent(device)}&PlaySessionId=${encodeURIComponent(play.PlaySessionId)}`, 'DELETE')).status;
      } finally {
        try { if (token) logout = (await request('/emby/Sessions/Logout', 'POST')).status; }
        finally { token = ''; stopped = true; }
      }
      return { EncodingsStatus: encodings, LogoutStatus: logout, MediaDetached: !video.getAttribute('src'), Paused: video.paused };
    };
    window.gobySelectedPhase2Media = { snapshot, stop,
      async select(enabled) {
        if (!hls || selected < 0) throw new Error('published_subtitle_track_unavailable');
        hls.subtitleTrack = enabled ? selected : -1;
        return settle(1.45, enabled ? input.ReviewedText : '');
      } };
    const authenticated = await request('/emby/Users/AuthenticateByName', 'POST', { Username: input.ViewerName, Pw: input.ViewerPassword });
    if (authenticated.status !== 200) throw new Error('viewer_authentication_failed');
    const login = await authenticated.json();
    if (login.User?.Id !== input.ViewerId || typeof login.AccessToken !== 'string' || !login.AccessToken) throw new Error('viewer_authentication_identity_mismatch');
    token = login.AccessToken;
    const negotiated = await request(`/emby/Items/${encodeURIComponent(input.OCRItemId)}/PlaybackInfo`, 'POST', {
      UserId: input.ViewerId, StartTimeTicks: 0, SubtitleStreamIndex: input.StreamIndex,
      EnableDirectPlay: false, EnableDirectStream: false, EnableTranscoding: true, AllowVideoStreamCopy: false, AllowAudioStreamCopy: false,
      DeviceProfile: { TranscodingProfiles: [{ Type: 'Video', Container: 'mp4', Protocol: 'hls', VideoCodec: 'h264', AudioCodec: 'aac',
        SegmentLength: 3, MaxWidth: 320, MaxHeight: 256, ManifestSubtitles: 'vtt', MaxManifestSubtitles: 8 }],
        SubtitleProfiles: [{ Format: 'vtt', Method: 'Hls', Container: 'mp4', Protocol: 'hls' }] },
    });
    if (negotiated.status !== 200) throw new Error('actual_playback_negotiation_failed');
    play = await negotiated.json();
    const source = play.MediaSources?.[0];
    if (play.ErrorCode || !play.PlaySessionId || play.MediaSources?.length !== 1 || !source.SupportsTranscoding || source.TranscodingSubProtocol !== 'hls') throw new Error('actual_hls_source_missing');
    const url = new URL(source.TranscodingUrl, input.BaseURL);
    if (url.origin !== input.BaseURL || url.searchParams.get('PlaySessionId') !== play.PlaySessionId || !url.searchParams.get('GobyHlsId') ||
      Number(url.searchParams.get('SubtitleStreamIndex')) !== input.StreamIndex || url.searchParams.get('DeviceId') !== device) throw new Error('actual_hls_scope_mismatch');
    hls = new Hls({ enableWorker: false, debug: false, renderTextTracksNatively: true, maxBufferLength: 12, maxMaxBufferLength: 24,
      xhrSetup(xhr, raw) {
        if (new URL(raw, input.BaseURL).origin !== input.BaseURL) throw new Error('hls_request_origin_mismatch');
        xhr.setRequestHeader('X-Emby-Token', token);
      } });
    hls.on(Hls.Events.FRAG_LOADED, () => { loaded++; });
    hls.on(Hls.Events.SUBTITLE_FRAG_PROCESSED, () => { subtitleFragments++; });
    hls.on(Hls.Events.SUBTITLE_TRACKS_UPDATED, bindTracks);
    hls.on(Hls.Events.ERROR, (_event, data) => {
      if (data.fatal && fatalErrors.length < 16) {
        const category = value => typeof value === 'string' && /^[A-Za-z][A-Za-z0-9_]{0,95}$/.test(value) ? value : 'unknown';
        fatalErrors.push({ Type: category(data.type), Detail: category(data.details) });
      }
    });
    hls.attachMedia(video); hls.loadSource(url.href);
    await settle(null, null);
    const until = performance.now() + 15000;
    while (performance.now() < until && hls.subtitleTracks.length === 0) await new Promise(resolve => setTimeout(resolve, 25));
    const tracks = hls.subtitleTracks.map((track, index) => ({ track, index })).filter(({ track }) =>
      track.lang === input.Language && track.name === `${input.Title} [${input.StreamIndex}]`);
    if (tracks.length !== 1) throw new Error('published_hls_subtitle_not_unique');
    selected = tracks[0].index;
    return { PlaySessionId: play.PlaySessionId, DeviceId: device, MediaSourceId: source.Id,
      HlsId: url.searchParams.get('GobyHlsId'), StreamIndex: input.StreamIndex, EngineSHA256: input.HlsBundleSHA256,
      EngineVersion: Hls.version, AuthenticationStatus: authenticated.status, PlaybackInfoStatus: negotiated.status };
  }, { RunId: fixture.RunId, BaseURL: fixture.BaseURL, ViewerName: fixture.ViewerName, ViewerPassword: fixture.ViewerPassword,
    ViewerId: fixture.ViewerId, OCRItemId: fixture.OCRItemId, StreamIndex: streamIndex, Language: fixture.OCRLanguage, Title: fixture.OCRTitle,
    ReviewedText: 'Phase 2 reviewed subtitle', HlsBundleSHA256: fixture.HlsBundleSHA256 });
  result.SubtitlePlayback = { Identity: identity, Observations: {} };
  return identity;
}

function requireActualSubtitlePlayback(value, enabled) {
  requireThat(value.ErrorCode === 0 && value.FatalErrors.length === 0 && value.ReadyState >= 2 && value.Width > 0 && value.Height > 0 &&
    value.Frames >= 3 && !value.Paused && !value.Seeking && value.LoadedFragments > 0 &&
    value.Transition.Completed && value.Transition.Seeked && value.Transition.PlayResolved && value.Transition.StableFrames.length >= 3,
  'actual_playback_frames_missing');
  requireThat(value.MediaTime >= 1.25 && value.MediaTime < 2.4 && Math.abs(value.MediaTime - value.PresentedTime) < .3,
    'actual_playback_clock_outside_reviewed_cue');
  if (enabled) requireThat(value.SelectedTrack >= 0 && value.SubtitleFragments > 0 &&
    value.ActiveCues.some(cue => cue.Text === 'Phase 2 reviewed subtitle' && Math.abs(cue.Start - 1.25) < .05 && Math.abs(cue.End - 2.4) < .05),
  'reviewed_cue_not_actually_presented');
  else requireThat(value.SelectedTrack === -1 && value.ActiveCues.length === 0, 'subtitle_off_not_observed');
}

async function main() {
  requireThat(process.platform === 'linux' && process.getuid() === 0, 'owned_linux_fixture_required');
  const contextPath = process.env.GOBY_SELECTED_PHASE2_CONTEXT;
  requireThat(typeof contextPath === 'string' && path.isAbsolute(contextPath), 'private_context_required');
  const directory = path.dirname(contextPath), stat = await fs.lstat(directory);
  requireThat(stat.isDirectory() && !stat.isSymbolicLink() && stat.uid === process.getuid() &&
    (stat.mode & 0o777) === 0o700 && await fs.realpath(directory) === directory, 'private_directory_required');
  fixture = await privateJSON(contextPath);
  requireThat(fixture.Marker === 'goby-selected-phase2-browser-fixture-v1' && fixture.RunId === process.env.GOBY_SELECTED_PHASE2_RUN_ID &&
    /^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$/.test(fixture.RunId), 'fixture_identity_mismatch');
  const origin = new URL(fixture.BaseURL);
  requireThat(origin.protocol === 'http:' && origin.hostname === '127.0.0.1' && origin.origin === fixture.BaseURL &&
    Number(origin.port) > 1024 && ![5432, 8096, 8920, 18196, 18198].includes(Number(origin.port)), 'owned_origin_required');
  requireThat(fixture.ArtifactsDir === directory && fixture.ResultPath === path.join(directory, 'browser-result.json'), 'artifact_binding_mismatch');
  for (const field of ['AdminName', 'AdminPassword', 'ViewerId', 'ViewerName', 'ViewerPassword', 'LibraryId', 'RemoveItemId', 'RemoveItemName', 'OCRItemId', 'OCRItemName'])
    requireThat(typeof fixture[field] === 'string' && fixture[field].length > 0 && fixture[field].length <= 4096, 'context_field_missing');
  requireThat(Array.isArray(fixture.ModelIds) && JSON.stringify(fixture.ModelIds) === JSON.stringify(['eng', 'chi_sim']), 'bilingual_fixture_model_inventory_required');
  requireThat(Number.isInteger(fixture.RemoveStreamIndex) && Number.isInteger(fixture.OCRStreamIndex) && typeof fixture.KeepSubtitleTitle === 'string', 'fixture_stream_identity_missing');
  result.RunId = fixture.RunId;
  const modulePath = process.env.GOBY_TEST_PLAYWRIGHT_MODULE;
  requireThat(typeof modulePath === 'string' && path.isAbsolute(modulePath), 'playwright_module_required');
  const { chromium } = createRequire(import.meta.url)(modulePath);
  browser = await chromium.launch({ headless: true });
  deadline = setTimeout(() => { result.DeadlineExceeded = true; void browser.close(); }, 480000);
  context = await guardedContext();
  page = await context.newPage();
  page.on('response', response => {
    const url = new URL(response.url());
    if (url.origin !== fixture.BaseURL || response.request().method() !== 'GET' ||
      !/^\/admin\/v1\/media-operations\/[0-9a-f]{32}\/cues\/\d+\/image$/.test(url.pathname)) return;
    const capture = (async () => {
      if (response.status() !== 200 || !/^image\/png(?:;|$)/i.test(response.headers()['content-type'] ?? '')) return { Complete: false };
      const bytes = await response.body();
      if (bytes.length === 0 || bytes.length > (2 << 20)) return { Complete: false };
      return { Complete: true, Bytes: bytes.length, SHA256: sha256(bytes) };
    })().catch(() => ({ Complete: false }));
    cueImageResponses.set(url.pathname, capture);
  });
  currentPhase = 'authentication'; await operation('administrator-login', login); result.Checks.Authentication = true; await stage(currentPhase);

  currentPhase = 'removal-ready';
  const removal = await operation('prepare-selected-subtitle-removal', () => prepare(fixture.RemoveItemId, fixture.RemoveItemName,
    'remove_embedded_subtitle', stream => stream.Index === fixture.RemoveStreamIndex));
  operationIds.Removal = removal.job.Id;
  readyOperations.Removal = await operation('wait-removal-review', () => waitState(removal.dialog, removal.job.Id, 'ready'));
  requireThat(!readyOperations.Removal.Applied && readyOperations.Removal.PublicationPhase === 'none', 'removal_was_applied_implicitly');
  await screenshot('removal-ready');
  result.Checks.RemovalPrepared = true; await stage(currentPhase, { OperationId: removal.job.Id });
  currentPhase = 'removal-applied';
  finalOperations.Removal = await operation('apply-reviewed-removal', () => apply(removal.dialog, readyOperations.Removal));
  requireThat(finalOperations.Removal.ResultSummary.BackupRetained === true, 'original_media_backup_not_retained');
  result.Checks.RemovalApplied = true; await stage(currentPhase, { OperationId: removal.job.Id });

  currentPhase = 'ocr-ready';
  const ocr = await operation('prepare-bitmap-recognition', () => prepare(fixture.OCRItemId, fixture.OCRItemName,
    'subtitle_ocr', stream => stream.Index === fixture.OCRStreamIndex));
  operationIds.OCR = ocr.job.Id;
  readyOperations.OCR = await operation('wait-ocr-review', () => waitState(ocr.dialog, ocr.job.Id, 'ready'));
  const recognized = await operation('load-recognized-cues', () => loadReview(ocr.dialog, ocr.job.Id));
  requireThat(recognized.data.Items.some(cue => cue.OriginalText.toLowerCase().replace(/\s+/g, ' ').includes('hello world')), 'authored_english_text_not_recognized');
  await operation('view-original-bitmaps-and-confidence', () => inspectCueImages(recognized.review, ocr.job.Id, recognized.data.Items));
  await screenshot('ocr-recognition-review');
  result.Checks.OCRRecognized = true; await stage(currentPhase, { OperationId: ocr.job.Id });

  currentPhase = 'ocr-reviewed';
  const first = recognized.data.Items.find(cue => cue.Ordinal === 0), second = recognized.data.Items.find(cue => cue.Ordinal === 1);
  requireThat(first && second, 'editable_fixture_cues_missing');
  const expectedEdits = [
    { Ordinal: 0, StartTicks: '12500000', EndTicks: '24000000', Text: 'Phase 2 reviewed subtitle', Included: true },
    { Ordinal: 1, StartTicks: second.StartTicks, EndTicks: second.EndTicks, Text: second.Text, Included: false },
  ];
  await operation('edit-cue-text-time-and-inclusion', async () => {
    await recognized.review.getByRole('textbox', { name: 'Cue 1 start (seconds)', exact: true }).fill('1.25');
    await recognized.review.getByRole('textbox', { name: 'Cue 1 end (seconds)', exact: true }).fill('2.4');
    await recognized.review.getByRole('textbox', { name: 'Cue 1 text', exact: true }).fill(expectedEdits[0].Text);
    await recognized.review.getByRole('checkbox', { name: 'Include cue 1', exact: true }).check();
    await recognized.review.getByRole('checkbox', { name: 'Include cue 2', exact: true }).uncheck();
    requireThat(!await ocr.dialog.getByRole('button', { name: 'Apply reviewed result', exact: true }).isEnabled(), 'dirty_ocr_review_can_apply');
  });
  const saved = await operation('save-reviewed-cues-with-cas', () => responseFor('PUT', `${operationPath(ocr.job.Id)}/review`, () =>
    recognized.review.getByRole('button', { name: 'Save cue changes', exact: true }).click()));
  requireThat(saved.Submitted.Revision === recognized.data.Operation.Revision &&
    JSON.stringify(saved.Submitted.Edits) === JSON.stringify(expectedEdits), 'review_request_not_exact_cas_edit');
  readyOperations.OCR = saved.Body.Operation;
  requireThat(BigInt(readyOperations.OCR.Revision) > BigInt(recognized.data.Operation.Revision) &&
    readyOperations.OCR.ResultHash !== recognized.data.Operation.ResultHash, 'review_did_not_create_new_result_identity');
  await recognized.review.getByText('Cue changes saved. Review the remaining pages before applying.', { exact: true }).waitFor();
  result.CueEdits = expectedEdits;
  result.Checks.OCRReviewed = true; await stage(currentPhase, { OperationId: ocr.job.Id,
    CueEdits: expectedEdits, Revision: readyOperations.OCR.Revision, ResultHash: readyOperations.OCR.ResultHash });
  currentPhase = 'ocr-applied';
  finalOperations.OCR = await operation('apply-reviewed-subtitle', () => apply(ocr.dialog, readyOperations.OCR));
  requireThat(finalOperations.OCR.ResultSummary.AppliedFormat === 'srt' &&
    /^[0-9a-f]{64}$/.test(finalOperations.OCR.ResultSummary.AppliedContentSHA256), 'published_subtitle_identity_missing');
  result.Checks.OCRApplied = true; await stage(currentPhase, { OperationId: ocr.job.Id });

  currentPhase = 'subtitle-selected';
  const subtitleScope = await operation('negotiate-real-owned-subtitle-playback', () =>
    installSubtitleConsumer(finalOperations.OCR.ResultSummary.AppliedStreamIndex));
  for (const [phase, enabled, check] of [
    ['subtitle-selected', true, 'SubtitleSelected'], ['subtitle-off', false, 'SubtitleOff'], ['subtitle-reselected', true, 'SubtitleReselected'],
  ]) {
    currentPhase = phase;
    const observation = await operation(`observe-${phase}`, () => mediaPage.evaluate(value => window.gobySelectedPhase2Media.select(value), enabled));
    requireActualSubtitlePlayback(observation, enabled);
    result.SubtitlePlayback.Observations[phase] = observation;
    result.Checks[check] = true;
    await stage(phase, subtitleScope);
  }
  await screenshot('reviewed-subtitle-actually-presented');
  currentPhase = 'subtitle-stopped';
  result.SubtitlePlayback.Stop = await operation('stop-owned-subtitle-playback', () => mediaPage.evaluate(() => window.gobySelectedPhase2Media.stop()));
  requireThat(result.SubtitlePlayback.Stop.EncodingsStatus === 204 && result.SubtitlePlayback.Stop.LogoutStatus === 204 &&
    result.SubtitlePlayback.Stop.MediaDetached && result.SubtitlePlayback.Stop.Paused, 'actual_subtitle_playback_stop_failed');
  await mediaContext.close(); mediaContext = undefined;
  result.Checks.SubtitleStopped = true; await stage(currentPhase, subtitleScope);

  currentPhase = 'cancel-ready';
  const cancelled = await operation('prepare-cancellable-real-removal', () => prepare(fixture.RemoveItemId, fixture.RemoveItemName,
    'remove_embedded_subtitle', stream => stream.Title === fixture.KeepSubtitleTitle));
  operationIds.Cancel = cancelled.job.Id;
  readyOperations.Cancel = await operation('wait-cancellable-ready-result', () => waitState(cancelled.dialog, cancelled.job.Id, 'ready'));
  result.Checks.CancelPrepared = true; await stage(currentPhase, { OperationId: cancelled.job.Id });
  currentPhase = 'cancelled';
  await operation('cancel-ready-operation', async () => {
    await cancelled.dialog.getByRole('button', { name: 'Cancel operation', exact: true }).click();
    const response = await responseFor('POST', `${operationPath(cancelled.job.Id)}/cancel`, () =>
      page.getByRole('dialog', { name: 'Cancel media operation?', exact: true }).getByRole('button', { name: 'Confirm cancellation', exact: true }).click(), 202);
    requireThat(response.Submitted.Revision === readyOperations.Cancel.Revision, 'cancellation_did_not_bind_revision');
    finalOperations.Cancel = await waitState(cancelled.dialog, cancelled.job.Id, 'cancelled');
    requireThat(!finalOperations.Cancel.Applied && finalOperations.Cancel.PublicationPhase === 'none', 'cancelled_result_was_published');
  });
  result.Checks.Cancelled = true; await stage(currentPhase, { OperationId: cancelled.job.Id });

  currentPhase = 'artwork';
  const artwork = await stage(currentPhase);
  requireThat(Array.isArray(artwork.ImageObservations) && artwork.ImageObservations.length >= 2 &&
    artwork.ImageObservations.every(image => image.Complete === true && image.Decoded === true && /^[0-9a-f]{64}$/.test(image.SHA256)), 'actual_artwork_http_evidence_missing');
  result.Checks.ArtworkObserved = true;
  await page.goto(`${fixture.BaseURL}/admin/tasks`);
  currentPhase = 'restart'; await stage(currentPhase);
  result.Checks.RestartPersisted = true;
  currentPhase = 'persisted';
  for (const [kind, id] of Object.entries(operationIds)) {
    const dialog = await operation(`open-persisted-${kind.toLowerCase()}-history`, () => history(id));
    const persisted = await waitState(dialog, id, kind === 'Cancel' ? 'cancelled' : 'completed');
    requireThat(persisted.Revision === finalOperations[kind].Revision && persisted.ResultHash === finalOperations[kind].ResultHash &&
      persisted.SourceRevision === finalOperations[kind].SourceRevision && persisted.Applied === finalOperations[kind].Applied,
    'restart_replayed_or_changed_operation');
    await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  }
  await screenshot('persisted-media-operation-history');
  result.Checks.HistoryPersisted = true; await stage(currentPhase, { HistoryIds: Object.values(operationIds) });
  currentPhase = 'cleanup';
  await operation('native-sign-out', async () => {
    await page.goto(`${fixture.BaseURL}/admin/`);
    await page.getByRole('navigation', { name: 'Administration', exact: true }).waitFor();
    await responseFor('DELETE', '/admin/v1/session', () => page.getByRole('button', { name: 'Sign out', exact: true }).click(), 204, false);
    await page.getByRole('heading', { name: 'Sign in to Goby', exact: true }).waitFor();
    await context.close();
  });
  await stage(currentPhase); result.Checks.Cleanup = true;
  requireThat(result.PageErrors === 0 && result.ForeignRequests === 0 && checks.every(key => result.Checks[key]), 'browser_evidence_incomplete');
  result.Complete = true;
}

try { await main(); }
catch (error) { result.Complete = false; result.FailurePhase ??= currentPhase; result.FailureOperation ??= currentOperation;
  result.FailureCode = error.safeCode || 'browser_operation_failed'; process.exitCode = 1; }
finally {
  clearTimeout(deadline);
  if (mediaPage && !mediaPage.isClosed()) {
    await mediaPage.evaluate(() => window.gobySelectedPhase2Media?.stop()).catch(() => {
      result.SubtitlePlaybackFallbackStopFailed = true; result.Complete = false; process.exitCode = 1;
    });
  }
  if (mediaContext) await mediaContext.close().catch(() => { result.Complete = false; process.exitCode = 1; });
  if (browser) await browser.close().catch(() => { result.Complete = false; process.exitCode = 1; });
  if (fixture?.ResultPath) await writeJSON(fixture.ResultPath, result).catch(() => { result.Complete = false; process.exitCode = 1; });
  process.stdout.write(`${JSON.stringify({ Marker: result.Marker, RunId: result.RunId, Complete: result.Complete,
    FailurePhase: result.FailurePhase, FailureOperation: result.FailureOperation, FailureCode: result.FailureCode ?? null })}\n`);
}
