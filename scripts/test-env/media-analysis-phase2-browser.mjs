#!/usr/bin/env node
/** Real native UI and named media consumers. No business responses are mocked. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';

const hash = (bytes) => createHash('sha256').update(bytes).digest('hex');
const caseFile = (id) => `case-${hash(Buffer.from(id)).slice(0, 32)}`;
const fail = (code) => { const error = new Error(code); error.safeCode = code; throw error; };
const requireThat = (condition, code) => { if (!condition) fail(code); };
const sleep = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));
const terminal = (run) => ['completed', 'failed', 'cancelled', 'interrupted'].includes(run.State);
let fixture, browser, context, page, deadline, phase = 'admission', nativeSignedIn = false;
const consumers = new Set();
const result = { Marker: 'goby-media-analysis-phase2-browser-result-v1', RunId: '', Complete: false, Cases: [], Stages: [], PageErrors: 0, ForeignRequests: 0, Screenshots: [],
  Consumer: { VideoJS: '7.6.5', BIFPluginCommit: '69989cee2f5ffed5151042cc9aef2280ea9ce5f6', BIFPluginModified: false, IntroAdapter: 'Goby acceptance protocol adapter', OriginalEmbyClient: false, EntitlementModified: false },
  Mechanics: { NativeConfiguration: false, ConfigurationCAS: false, ActualTaskCompletion: false, ActualCancellation: false, Decisions: false, CachePrune: false, Restart: false, ActualSkip: false, BIFRange: false, AnonymousPreviewDenied: false, SourceReplacement: false, AuthorityRevocation: false } };

async function privateJSON(filename, maximum = 8 << 20) {
  requireThat(path.isAbsolute(filename) && path.resolve(filename) === filename && await fs.realpath(filename) === filename, 'private_input_path_invalid');
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try { const stat = await handle.stat(); requireThat(stat.isFile() && stat.uid === process.getuid() && stat.nlink === 1 && (stat.mode & 0o777) === 0o600 && stat.size > 0 && stat.size <= maximum, 'private_input_identity_invalid'); return JSON.parse(await handle.readFile('utf8')); }
  finally { await handle.close(); }
}
async function artifact(name, bytes) {
  requireThat(/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(name), 'artifact_name_invalid'); const filename = path.join(fixture.ArtifactsDir, name);
  await fs.writeFile(filename, bytes, { flag: 'wx', mode: 0o600 }); return { path: filename, sha256: hash(bytes) };
}
async function evidence(name, value) { return artifact(name, Buffer.from(`${JSON.stringify(value, null, 2)}\n`)); }
async function checkpoint() {
  const temporary = `${fixture.ResultPath}.pending`;
  await fs.writeFile(temporary, `${JSON.stringify(result, null, 2)}\n`, { flag: 'wx', mode: 0o600 }); await fs.rename(temporary, fixture.ResultPath);
}
async function screenshot(target, name) {
  const bytes = await target.screenshot({ fullPage: false, animations: 'disabled', mask: [target.locator('input,textarea')], timeout: 5000 });
  requireThat(bytes.length <= (8 << 20), 'screenshot_limit'); await artifact(name, bytes); result.Screenshots.push(name);
}
async function native(pathname, method = 'GET', body) {
  const value = await page.evaluate(async ({ pathname, method, body }) => {
    const headers = {}; if (body !== undefined) headers['Content-Type'] = 'application/json';
    if (method !== 'GET') { const session = await fetch('/admin/v1/session', { credentials: 'same-origin', cache: 'no-store' }); if (session.status !== 200) throw new Error('native_session_unavailable'); headers['X-CSRF-Token'] = (await session.json()).CSRFToken; }
    const response = await fetch(pathname, { method, credentials: 'same-origin', headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }), cache: 'no-store', signal: AbortSignal.timeout(30000) });
    return { Status: response.status, Body: response.status === 204 ? null : await response.json() };
  }, { pathname, method, body });
  if (!(value.Status === 200 || method !== 'GET' && value.Status === 204)) {
    result.HTTPFailures ??= []; const ref = await evidence(`failure-http-${result.HTTPFailures.length}.json`, { Method: method, Path: pathname, ...value }); result.HTTPFailures.push(ref); fail('actual_native_http_failed');
  }
  return value;
}
async function responseFor(method, pathname, action, status = 200) {
  const waiting = page.waitForResponse((response) => response.request().method() === method && new URL(response.url()).pathname === pathname, { timeout: 30000 });
  void waiting.catch(() => {});
  await action(); const response = await waiting;
  const request = response.request(); requireThat(Boolean(request.headers()['x-csrf-token']), 'native_mutation_csrf_missing');
  const value = { Status: response.status(), Submitted: request.postDataJSON(), Body: response.status() === 204 ? null : await response.json() };
  if (response.status() !== status) { result.HTTPFailures ??= []; const ref = await evidence(`failure-http-${result.HTTPFailures.length}.json`, { Method: method, Path: pathname, ...value }); result.HTTPFailures.push(ref); fail('native_mutation_status_mismatch'); }
  return value;
}
async function login() {
  await page.goto(`${fixture.BaseURL}/admin/media-analysis`); await page.getByRole('heading', { name: 'Sign in to Goby', exact: true }).waitFor();
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill(fixture.AdminName); await page.locator('input#account-password[name="Password"]').fill(fixture.AdminPassword);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click(); await page.getByRole('heading', { name: 'Analysis configuration', exact: true }).waitFor();
  await page.getByRole('button', { name: 'Refresh analysis status', exact: true }).waitFor({ state: 'visible' }); nativeSignedIn = true;
}
async function logout() {
  if (!nativeSignedIn) return;
  await page.getByRole('button', { name: 'Sign out', exact: true }).click(); await page.getByRole('heading', { name: 'Sign in to Goby', exact: true }).waitFor(); nativeSignedIn = false;
}
async function stage(name) {
  await evidence(`stage-${name}-request.json`, { RunId: fixture.RunId, Phase: name });
  const limit = Math.min(deadline, Date.now() + 90000);
  while (Date.now() < limit) {
    let ack; try { ack = await privateJSON(path.join(fixture.ArtifactsDir, `stage-${name}-ack.json`)); } catch (error) { if (error.code !== 'ENOENT') throw error; await sleep(100); continue; }
    requireThat(ack.Marker === 'goby-media-analysis-phase2-stage-v1' && ack.RunId === fixture.RunId && ack.Phase === name && ack.Complete === true, 'independent_stage_failed');
    result.Stages.push(name); await checkpoint(); return ack;
  }
  fail('independent_stage_timeout');
}
async function selectLibraries() {
  for (const library of fixture.Libraries) await page.getByRole('checkbox', { name: library.Name, exact: true }).check();
}
async function waitRun(runId, taskId) {
  while (Date.now() < deadline) {
    const value = await native(`/admin/v1/task-runs/${encodeURIComponent(runId)}?StartIndex=0&Limit=200`);
    requireThat(value.Body.Run?.Id === runId && value.Body.Run.TaskId === taskId, 'actual_task_identity_mismatch');
    if (terminal(value.Body.Run)) return value;
    await sleep(1000);
  }
  fail('actual_task_deadline');
}
async function start(kind) {
  const label = kind === 'intro' ? 'Analyze library intros' : 'Build library previews';
  const response = await responseFor('POST', '/admin/v1/media-analysis/runs', () => page.getByRole('button', { name: label, exact: true }).click(), 202);
  requireThat(response.Submitted.Kind === kind && response.Submitted.Force === false && response.Submitted.ItemIds.length === 0 &&
    JSON.stringify([...response.Submitted.LibraryIds].sort()) === JSON.stringify(fixture.Libraries.map((library) => library.Id).sort()) && response.Body.RunId && response.Body.TaskId, 'native_run_scope_mismatch');
  const completed = await waitRun(response.Body.RunId, response.Body.TaskId);
  return { Admission: response, Final: completed };
}
async function configure() {
  const initial = (await native('/admin/v1/media-analysis')).Body;
  await evidence('native-initial-analysis-http.json', initial);
  requireThat(initial.Runtime.IntroAvailable && initial.Runtime.PreviewAvailable, 'real_analysis_dependencies_unavailable');
  const auto = page.getByRole('switch', { name: 'Automatically publish qualified detected intros', exact: true });
  await auto.uncheck();
  const first = await responseFor('PUT', '/admin/v1/media-analysis/configuration', () => page.getByRole('button', { name: 'Save analysis configuration', exact: true }).click());
  requireThat(first.Submitted.Revision === initial.Configuration.Revision && first.Body.Profile.AutoPublishIntros === false, 'native_profile_write_not_consumed');
  await page.getByRole('button', { name: 'Refresh analysis status', exact: true }).waitFor();
  await auto.check(); await page.getByRole('textbox', { name: 'Preview interval (seconds)', exact: true }).fill(String(fixture.PreviewIntervalSeconds));
  // A second actual native writer changes the same singleton. No response is
  // mocked; the original page must preserve its now-stale draft after 409.
  const concurrent = await native('/admin/v1/media-analysis/configuration', 'PUT', { Revision: first.Body.Revision, Profile: { ...first.Body.Profile, PreviewQuality: first.Body.Profile.PreviewQuality === 80 ? 81 : 80 } });
  const conflict = await responseFor('PUT', '/admin/v1/media-analysis/configuration', () => page.getByRole('button', { name: 'Save analysis configuration', exact: true }).click(), 409);
  requireThat(conflict.Submitted.Revision === first.Body.Revision, 'native_conflict_draft_rebased_silently');
  await page.getByRole('button', { name: 'Reload latest and keep draft', exact: true }).click();
  await page.getByRole('button', { name: 'Save analysis configuration', exact: true }).waitFor();
  const quality = page.getByRole('textbox', { name: 'Preview image quality', exact: true });
  requireThat(await quality.inputValue() === String(first.Body.Profile.PreviewQuality), 'native_conflict_lost_quality_draft');
  // Freeze the encoder quality used by the independent RGB calibration after
  // proving that reloading the concurrent revision preserved the user's draft.
  await quality.fill('81');
  const saved = await responseFor('PUT', '/admin/v1/media-analysis/configuration', () => page.getByRole('button', { name: 'Save analysis configuration', exact: true }).click());
  requireThat(saved.Submitted.Revision === concurrent.Body.Revision && saved.Body.Profile.AutoPublishIntros === true && saved.Body.Profile.PreviewIntervalSeconds === fixture.PreviewIntervalSeconds && saved.Body.Profile.PreviewQuality === 81, 'native_conflict_reload_lost_profile');
  const readback = (await native('/admin/v1/media-analysis')).Body;
  requireThat(readback.Configuration.Revision === saved.Body.Revision && readback.Configuration.Profile.PreviewQuality === 81, 'native_preview_quality_readback_mismatch');
  result.Mechanics.NativeConfiguration = true; result.Mechanics.ConfigurationCAS = true;
  await evidence('native-configuration-http.json', { Initial: initial, First: first, Concurrent: concurrent, Conflict: conflict, Saved: saved, Readback: readback }); await screenshot(page, 'native-configuration.png');
}
function parseBIF(bytes) {
  requireThat(bytes.length >= 72 && bytes.length <= (128 << 20) && bytes.subarray(0, 8).equals(Buffer.from([0x89, 0x42, 0x49, 0x46, 13, 10, 26, 10])) && bytes.readUInt32LE(8) === 0, 'actual_bif_header_invalid');
  const count = bytes.readUInt32LE(12); const multiplier = bytes.readUInt32LE(16) || 1000;
  requireThat(count > 0 && count <= 4096 && 64 + (count + 1) * 8 < bytes.length, 'actual_bif_count_invalid');
  const frames = [];
  for (let index = 0; index < count; index++) {
    const rawTimestamp = bytes.readUInt32LE(64 + index * 8); requireThat(rawTimestamp === index, 'named_consumer_requires_ordinal_bif_profile');
    const tick = rawTimestamp * multiplier * 10000; const offset = bytes.readUInt32LE(68 + index * 8); const end = bytes.readUInt32LE(76 + index * 8);
    requireThat(Number.isSafeInteger(tick) && offset >= 64 + (count + 1) * 8 && end > offset && end <= bytes.length && end - offset <= (2 << 20), 'actual_bif_entry_invalid');
    frames.push({ tick, bytes: bytes.subarray(offset, end), sha256: hash(bytes.subarray(offset, end)) });
  }
  requireThat(bytes.readUInt32LE(64 + count * 8) === 0xffffffff && bytes.readUInt32LE(68 + count * 8) === bytes.length, 'actual_bif_sentinel_invalid');
  return frames;
}
async function consume(sample, observation) {
  const consumer = await context.newPage(); consumers.add(consumer); consumer.setDefaultTimeout(30000);
  try {
    await consumer.goto(`${fixture.BaseURL}/__media-analysis-phase2`); await consumer.waitForFunction(() => window.phase2AcceptanceReady === true);
    const authenticated = await consumer.evaluate((input) => window.phase2Acceptance.authenticate(input), fixture);
    const bifResponsePromise = consumer.waitForResponse((response) => new URL(response.url()).pathname === `/emby/Videos/${encodeURIComponent(sample.ItemId)}/index.bif` && response.request().method() === 'GET');
    void bifResponsePromise.catch(() => {});
    const opened = await consumer.evaluate((input) => window.phase2Acceptance.open(input), sample);
    const bifResponse = await bifResponsePromise; requireThat(bifResponse.status() === 200, 'consumer_actual_bif_http_failed');
    const bifBytes = await bifResponse.body();
    const bifRef = await artifact(`${caseFile(sample.CaseId)}-actual.bif`, bifBytes);
    result.PartialPreviewEvidence ??= []; result.PartialPreviewEvidence.push({ CaseId: sample.CaseId, BIF: bifRef, HTTPStatus: bifResponse.status() });
    let frames; try { frames = parseBIF(bifBytes); } catch (error) { observation.preview = { status: 'failed' }; throw error; }
    requireThat(opened.ThumbnailSet.Thumbnails.length === frames.length && opened.ThumbnailSet.Thumbnails.every((thumbnail, index) => thumbnail.PositionTicks === frames[index].tick && typeof thumbnail.ImageTag === 'string'), 'thumbnail_set_bif_timeline_mismatch');
    const etag = bifResponse.headers().etag;
    requireThat(typeof etag === 'string' && etag, 'actual_bif_validator_missing');
    const httpChecks = await consumer.evaluate((etag) => window.phase2Acceptance.httpChecks(etag), etag);
    requireThat(httpChecks.RangeHeaderSHA256 === hash(bifBytes.subarray(0, 64)), 'actual_bif_range_bytes_differ');
    result.Mechanics.BIFRange = true; result.Mechanics.AnonymousPreviewDenied = true;
    if (sample.CaseId === fixture.ConsumerCaseId) {
      const expected = observation.detection;
      requireThat(expected.status === 'published' && opened.MarkerPair?.StartTicks === expected.start_ticks && opened.MarkerPair.EndTicks === expected.end_ticks, 'actual_detected_markers_not_consumable');
      const before = await consumer.evaluate(() => window.phase2Acceptance.positionForSkip());
      await consumer.getByRole('button', { name: 'Skip detected intro', exact: true }).click(); const after = await consumer.evaluate(() => window.phase2Acceptance.confirmSkip());
      requireThat(after.ErrorCode === 0 && after.ProgressStatus === 204 && after.Frames.length >= 3 && after.Frames.every((frame) => frame.MediaTime >= expected.end_ticks / 10_000_000 && frame.Width > 0 && frame.Height > 0), 'actual_skip_did_not_present_source_frames');
      result.Skip = { Before: before, After: after, CaseId: sample.CaseId }; result.Mechanics.ActualSkip = true; await screenshot(consumer, 'consumer-actual-skip.png');
    }
    await consumer.evaluate(() => window.phase2Acceptance.pause());
    const bar = consumer.locator('.vjs-progress-holder'); await bar.waitFor(); const bounds = await bar.boundingBox(); requireThat(bounds && bounds.width > 100 && bounds.height > 0, 'actual_consumer_seek_bar_missing');
    const browserFrames = [], thumbnails = [];
    for (const index of [...new Set([0, Math.floor(frames.length / 2), frames.length - 1])]) {
      const start = frames[index].tick / 10_000_000; const end = index + 1 < frames.length ? frames[index + 1].tick / 10_000_000 : opened.DurationSeconds;
      const seconds = start + (end - start) / 3; requireThat(seconds >= start && seconds < end, 'consumer_hover_slot_invalid');
      await consumer.mouse.move(bounds.x + bounds.width * seconds / opened.DurationSeconds, bounds.y + bounds.height / 2, { steps: 5 });
      let displayed;
      const until = Date.now() + 15000;
      while (Date.now() < until) { try { displayed = await consumer.evaluate(() => window.phase2Acceptance.bifFrame()); if (displayed.jpeg_sha256 === frames[index].sha256) break; } catch { /* The real plugin can still be decoding the new image. */ } await sleep(80); }
      requireThat(displayed?.visible === true && displayed.jpeg_sha256 === frames[index].sha256 && displayed.width === sample.Width, 'actual_plugin_visible_frame_mismatch');
      const equivalents = frames.flatMap((frame, number) => frame.sha256 === displayed.observed_jpeg_sha256 ? [number] : []);
      requireThat(equivalents.includes(index) && displayed.hover_seconds >= start && displayed.hover_seconds < end && displayed.tooltip_seconds >= start && displayed.tooltip_seconds < end, 'observed_hover_does_not_bind_nominal_slot');
      browserFrames.push({ index, ...displayed, observed_equivalent_indexes: equivalents, observed_index: equivalents.length === 1 ? equivalents[0] : null });
      const imageResponsePromise = consumer.waitForResponse((response) => new URL(response.url()).pathname === `/emby/Items/${encodeURIComponent(sample.ItemId)}/Images/Thumbnail`);
      void imageResponsePromise.catch(() => {});
      const thumbnail = await consumer.evaluate(({ index, limits }) => window.phase2Acceptance.thumbnail(index, limits), { index, limits: fixture });
      const imageResponse = await imageResponsePromise; const image = await imageResponse.body(); requireThat(imageResponse.status() === 200 && image.length > 0 && image.length <= (8 << 20) && thumbnail.Complete, 'actual_thumbnail_jpeg_chain_failed');
      const imageRef = await artifact(`${caseFile(sample.CaseId)}-thumbnail-${index}.jpg`, image); thumbnails.push({ ...thumbnail, HTTPStatus: imageResponse.status(), JPEG: imageRef });
      await screenshot(consumer, `${caseFile(sample.CaseId)}-hover-${index}.png`);
    }
    const target = opened.DurationSeconds * .55; const beforeDrag = await consumer.evaluate(() => window.phase2Acceptance.playbackState());
    await consumer.mouse.move(bounds.x + bounds.width * .2, bounds.y + bounds.height / 2); await consumer.mouse.down(); await consumer.mouse.move(bounds.x + bounds.width * .55, bounds.y + bounds.height / 2, { steps: 12 }); await consumer.mouse.up();
    await consumer.waitForFunction(({ target, tolerance, sequence }) => { const state = window.phase2Acceptance.playbackState(); return !state.Seeking && state.ErrorCode === 0 && Math.abs(state.CurrentSeconds - target) <= tolerance && state.Frames.some((frame) => frame.Sequence > sequence && Math.abs(frame.MediaTime - target) <= tolerance); }, { target, tolerance: opened.DurationSeconds / bounds.width * 2 + .25, sequence: beforeDrag.Frames.at(-1)?.Sequence ?? 0 });
    const afterDrag = await consumer.evaluate(() => window.phase2Acceptance.playbackState());
    const close = await consumer.evaluate(() => window.phase2Acceptance.close()); requireThat(close.PlayerDisposed && close.LogoutStatus === 204 && close.StopStatus === 204, 'actual_consumer_cleanup_failed');
    const http = await evidence(`${caseFile(sample.CaseId)}-preview-http.json`, { Authentication: authenticated, Open: opened, BIF: { Status: bifResponse.status(), Bytes: bifBytes.length, SHA256: bifRef.sha256, ETag: etag, ContentType: bifResponse.headers()['content-type'], HeaderMultiplierMillis: bifBytes.readUInt32LE(16), RawTimestampsAreOrdinals: true }, HTTP: httpChecks, Thumbnails: thumbnails, Drag: { Before: beforeDrag, After: afterDrag, TargetSeconds: target }, Close: close, Screenshots: result.Screenshots.filter((name) => name.startsWith(`${caseFile(sample.CaseId)}-`)) });
    observation.preview = { status: 'ready', bif_path: bifRef.path, bif_sha256: bifRef.sha256, width: browserFrames[0].width, height: browserFrames[0].height,
      nominal_ticks: frames.map((frame) => frame.tick), browser_frames: browserFrames, http_evidence: http };
    await checkpoint();
  } finally {
    try { if (!consumer.isClosed()) await consumer.evaluate(() => window.phase2Acceptance?.close()); } catch { result.ConsumerCleanupIncomplete = true; }
    await consumer.close(); consumers.delete(consumer);
  }
}
async function decisionJourney(sample) {
  await page.getByRole('textbox', { name: 'Search media', exact: true }).fill(sample.Name); await page.getByRole('button', { name: 'Search', exact: true }).click();
  await page.getByRole('button', { name: `Review analysis for ${sample.Name}`, exact: true }).click(); const dialog = page.getByRole('dialog', { name: sample.Name, exact: true });
  const before = (await native(`/admin/v1/media-analysis/items/${encodeURIComponent(sample.ItemId)}`)).Body;
  requireThat(before.Detection.Candidate && ['qualified', 'review'].includes(before.Detection.Status), 'native_review_candidate_missing');
  const records = [];
  for (const action of ['accept', 'reject', 'reset']) {
    const labels = { accept: ['Accept candidate', 'Accept this candidate as a manual intro?', 'Accept as manual intro'], reject: ['Reject detected intro', 'Reject this detected intro?', 'Reject detected intro'], reset: ['Reset detection decision', 'Reset this detection decision?', 'Reset detection decision'] }[action];
    await dialog.getByRole('button', { name: labels[0], exact: true }).click(); const confirmation = page.getByRole('dialog', { name: labels[1], exact: true });
    const saved = await responseFor('POST', `/admin/v1/media-analysis/items/${encodeURIComponent(sample.ItemId)}/decision`, () => confirmation.getByRole('button', { name: labels[2], exact: true }).click());
    requireThat(saved.Submitted.Action === action && typeof saved.Submitted.Revision === 'string' && typeof saved.Submitted.ManualRevision === 'string' && saved.Submitted.SourceRevision === sample.SourceRevision && saved.Body.Effective?.Provenance === 'Manual', 'native_manual_priority_not_retained');
    requireThat(saved.Body.Suppressed === (action === 'reject'), 'native_source_suppression_mismatch'); records.push(saved);
  }
  await screenshot(page, 'native-result-review.png'); await dialog.getByRole('button', { name: 'Close', exact: true }).click(); await evidence('native-decisions-http.json', { Before: before, Changes: records }); result.Mechanics.Decisions = true;
}
async function cancelJourney() {
  await page.getByRole('checkbox', { name: 'Force rebuild existing analysis and previews', exact: true }).check();
  const admitted = await responseFor('POST', '/admin/v1/media-analysis/runs', () => page.getByRole('button', { name: 'Build library previews', exact: true }).click(), 202);
  requireThat(admitted.Submitted.Force === true && admitted.Body.Admitted === true, 'native_force_run_not_admitted');
  let running;
  const until = Math.min(deadline, Date.now() + 30000);
  while (Date.now() < until) {
    running = await native(`/admin/v1/task-runs/${encodeURIComponent(admitted.Body.RunId)}?StartIndex=0&Limit=200`);
    requireThat(!terminal(running.Body.Run), 'force_run_completed_before_cancellation');
    if (running.Body.Run.State === 'running' && running.Body.Children.Items.some((child) => child.State === 'running')) break;
    await sleep(100);
  }
  requireThat(running?.Body.Run.State === 'running' && running.Body.Children.Items.some((child) => child.State === 'running'), 'actual_running_child_not_observed_for_cancel');
  const progress = page.getByRole('region', { name: 'Analysis task progress' });
  // The production run must still be active when this actual UI stop happens.
  // A run that won the race and completed is not reported as a cancellation.
  const stopped = await responseFor('POST', `/admin/v1/task-runs/${encodeURIComponent(admitted.Body.RunId)}/cancel`, () => progress.getByRole('button', { name: 'Request stop', exact: true }).click());
  const final = await waitRun(admitted.Body.RunId, admitted.Body.TaskId);
  requireThat(final.Body.Run.State === 'cancelled' && final.Body.Run.TerminalChildren === final.Body.Run.TotalChildren, 'actual_cancel_did_not_join_terminal_work');
  await evidence('native-cancellation-http.json', { Admission: admitted, Running: running, Stop: stopped, Final: final }); result.Mechanics.ActualCancellation = true;
}

try {
  requireThat(process.platform === 'linux' && process.getuid() === 0, 'remote_linux_fixture_required');
  fixture = await privateJSON(process.env.GOBY_MEDIA_ANALYSIS_PHASE2_CONTEXT, 256 << 10);
  requireThat(fixture.Marker === 'goby-media-analysis-phase2-browser-v1' && fixture.RunId === process.env.GOBY_MEDIA_ANALYSIS_PHASE2_RUN_ID && Array.isArray(fixture.Cases) && fixture.Cases.length > 0 && fixture.Cases.length <= 32, 'browser_context_binding_invalid');
  requireThat([1, 2].includes(fixture.ManifestVersion) && new Set(fixture.Cases.map((sample) => sample.CaseId)).size === fixture.Cases.length, 'browser_manifest_identity_invalid');
  const designated = fixture.Cases.find((sample) => sample.CaseId === fixture.ConsumerCaseId);
  requireThat(designated?.PreviewRequired === true, 'designated_consumer_missing');
  if (fixture.ManifestVersion === 2) {
    requireThat(designated.EvaluationRole === 'fresh_holdout' && fixture.Cases.every((sample) => ['calibration', 'regression', 'fresh_holdout'].includes(sample.EvaluationRole)), 'browser_evaluation_role_invalid');
    result.EvaluationRoles = Object.fromEntries(fixture.Cases.map((sample) => [sample.CaseId, sample.EvaluationRole]));
  }
  result.ManifestVersion = fixture.ManifestVersion; result.Consumer.CaseId = designated.CaseId;
  requireThat(new URL(fixture.BaseURL).hostname === '127.0.0.1' && new URL(fixture.BaseURL).protocol === 'http:' && path.dirname(fixture.ResultPath) === fixture.ArtifactsDir, 'browser_fixture_authority_invalid');
  const directory = await fs.stat(fixture.ArtifactsDir); requireThat(directory.isDirectory() && directory.uid === process.getuid() && (directory.mode & 0o777) === 0o700 && await fs.realpath(fixture.ArtifactsDir) === fixture.ArtifactsDir, 'private_artifact_root_required');
  deadline = Date.now() + 75 * 60 * 1000; result.RunId = fixture.RunId; result.Cases = fixture.Cases.map((sample) => ({ case_id: sample.CaseId, task: { status: 'pending' }, detection: { status: 'pending' }, preview: { status: 'pending' } })); await checkpoint();
  const require = createRequire(import.meta.url); const { chromium } = require(process.env.GOBY_TEST_PLAYWRIGHT_MODULE);
  browser = await chromium.launch({ headless: true, args: ['--no-sandbox', '--autoplay-policy=no-user-gesture-required'] });
  context = await browser.newContext({ viewport: { width: 1600, height: 1000 }, serviceWorkers: 'block' });
  await context.route('**/*', async (route) => { const url = new URL(route.request().url()); if (url.origin === fixture.BaseURL || ['data:', 'blob:'].includes(url.protocol)) await route.continue(); else { result.ForeignRequests++; await route.abort('blockedbyclient'); } });
  context.on('page', (target) => target.on('pageerror', () => { result.PageErrors++; }));
  page = await context.newPage(); page.setDefaultTimeout(30000);
  phase = 'authentication'; await login();
  phase = 'configuration'; await configure(); await selectLibraries();
  phase = 'intro-tasks'; const introRun = await start('intro'); const taskRef = await evidence('intro-task-http.json', introRun);
  phase = 'preview-tasks'; const previewRun = await start('previews'); await evidence('preview-task-http.json', previewRun);
  const actual = [];
  const sourceToCase = new Map(fixture.Cases.map((sample) => [sample.SourceRevision, sample.CaseId]));
  phase = 'automatic-observed';
  for (const sample of fixture.Cases) {
    const response = await native(`/admin/v1/media-analysis/items/${encodeURIComponent(sample.ItemId)}`); const value = response.Body;
    requireThat(value.Id === sample.ItemId && value.MediaSourceId === sample.MediaSourceId && value.SourceRevision === sample.SourceRevision, 'automatic_result_source_identity_changed');
    actual.push({ CaseId: sample.CaseId, Detection: value.Detection });
    const raw = await evidence(`${caseFile(sample.CaseId)}-detection-http.json`, response); const published = value.Detection.Status === 'qualified' && value.Detection.Effective?.Provenance === 'Detected' && !value.Detection.Suppressed;
    const support = [...new Set((value.Detection.Candidate?.Support ?? []).map((entry) => { const id = sourceToCase.get(entry.SourceKey); requireThat(id, 'detected_support_not_in_frozen_corpus'); return id; }))];
    const observation = result.Cases.find((entry) => entry.case_id === sample.CaseId);
    observation.task = { run_id: introRun.Final.Body.Run.Id, status: introRun.Final.Body.Run.State === 'completed' ? 'succeeded' : introRun.Final.Body.Run.State === 'cancelled' ? 'cancelled' : 'failed', http_evidence: taskRef };
    observation.detection = { status: published ? 'published' : 'abstained', support_case_ids: support, reason: value.Detection.Reasons.join(',') || value.Detection.Status, evidence: raw,
      ...(published ? { start_ticks: value.Detection.Effective.StartTicks, end_ticks: value.Detection.Effective.EndTicks } : {}) };
    await checkpoint();
  }
  await evidence('automatic-http.json', { Cases: actual }); await stage('automatic-observed');
  requireThat(introRun.Final.Body.Run.State === 'completed' && previewRun.Final.Body.Run.State === 'completed', 'real_analysis_tasks_not_successful'); result.Mechanics.ActualTaskCompletion = true;
  phase = 'consumers';
  const previewCases = fixture.Cases.filter((sample) => sample.PreviewRequired);
  const orderedPreviews = fixture.ManifestVersion === 2 ? [designated, ...previewCases.filter((sample) => sample.CaseId !== designated.CaseId)] : previewCases;
  for (const sample of orderedPreviews) await consume(sample, result.Cases.find((entry) => entry.case_id === sample.CaseId));
  requireThat(result.Mechanics.ActualSkip, 'actual_automatic_intro_skip_not_exercised');
  phase = 'decisions'; await decisionJourney(fixture.Cases.find((sample) => sample.CaseId === fixture.ConsumerCaseId));
  phase = 'cancellation'; await cancelJourney();
  phase = 'cache';
  const beforePrune = (await native('/admin/v1/media-analysis')).Body;
  await page.getByRole('button', { name: 'Prune analysis cache', exact: true }).click();
  const pruned = await responseFor('POST', '/admin/v1/media-analysis/cache/prune', () => page.getByRole('dialog', { name: 'Prune the analysis cache?', exact: true }).getByRole('button', { name: 'Prune eligible entries', exact: true }).click());
  requireThat(pruned.Submitted.Revision === beforePrune.Configuration.Revision && Number.isSafeInteger(pruned.Body.BusyEntries) && pruned.Body.BusyEntries >= 0, 'native_cache_prune_receipt_invalid');
  await evidence('native-cache-prune-http.json', { Before: beforePrune.Runtime.Cache, Mutation: pruned, After: (await native('/admin/v1/media-analysis')).Body.Runtime.Cache }); result.Mechanics.CachePrune = true; await screenshot(page, 'native-cache-prune.png');
  phase = 'restart'; const persisted = (await native('/admin/v1/media-analysis')).Body.Configuration; await logout();
  const restarted = await stage('restart'); requireThat(new URL(restarted.BaseURL).hostname === '127.0.0.1' && new URL(restarted.BaseURL).protocol === 'http:', 'restarted_origin_invalid'); fixture.BaseURL = restarted.BaseURL;
  await login(); const afterRestart = (await native('/admin/v1/media-analysis')).Body.Configuration; requireThat(JSON.stringify(afterRestart) === JSON.stringify(persisted), 'restart_configuration_replayed_or_changed');
  phase = 'persisted'; await stage('persisted'); result.Mechanics.Restart = true;
  phase = 'cleanup'; await logout(); await stage('cleanup');
  requireThat(result.PageErrors === 0 && result.ForeignRequests === 0 && !result.ConsumerCleanupIncomplete, 'browser_error_or_external_request'); result.Complete = true;
} catch (error) {
  result.Complete = false; result.FailurePhase = phase; result.FailureCode = error.safeCode ?? String(error.message).match(/\bconsumer_[a-z0-9_]+\b/)?.[0] ?? 'browser_operation_failed';
  result.ErrorKind = ['Error', 'TimeoutError', 'TypeError'].includes(error.name) ? error.name : 'OperationError';
  if (String(error.message).includes('strict mode violation')) result.LocatorFailure = 'multiple_matches';
  if (fixture && page && !page.isClosed()) { try { await screenshot(page, `failure-${phase}.png`); } catch { result.FailureScreenshotUnavailable = true; } }
} finally {
  for (const consumer of consumers) { try { await consumer.evaluate(() => window.phase2Acceptance?.close()); } catch { result.ConsumerCleanupIncomplete = true; } try { await consumer.close(); } catch { result.ConsumerCleanupIncomplete = true; } }
  if (nativeSignedIn && page && !page.isClosed()) { try { await logout(); } catch { result.NativeCleanupIncomplete = true; } }
  try { await context?.close(); result.BrowserContextClosed = true; } catch { result.Complete = false; }
  try { await browser?.close(); result.BrowserClosed = true; } catch { result.Complete = false; }
  if (fixture) { try { await checkpoint(); } catch { result.Complete = false; } }
  process.exitCode = result.Complete ? 0 : 1;
  if (!result.Complete) process.stderr.write(`Phase 2 browser did not pass: ${result.FailurePhase ?? phase}/${result.FailureCode ?? 'cleanup_failed'}\n`);
}
