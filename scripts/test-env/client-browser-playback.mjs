#!/usr/bin/env node
/** Drive the original Emby movie UI through the observer's guarded browser. */

import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawn } from 'node:child_process';

const MOVIE = 'M3e Client Movie', ITEM_ID = /^[a-f0-9]{32}$/i;
const requireMovie = (value, code) => { if (!value) throw new Error(code); };

export function movieFailureCode(error, fallback = 'movie_operation_failed') {
  if (error?.message === 'candidate_time_budget_exhausted') return error.message;
  if (Array.isArray(error?.errors) && error.errors.some(value => movieFailureCode(value, '') === 'candidate_time_budget_exhausted'))
    return 'candidate_time_budget_exhausted';
  return /^movie_[a-z0-9_]+$/.test(error?.message ?? '') ? error.message : fallback;
}

export async function waitMovieControl(candidates, { record = () => {}, timeout = 10000 } = {}) {
  const counts = async () => Promise.all(candidates.map(async ({ key, locator }) => ({ control: key, count: await locator.count() })));
  try { await Promise.any(candidates.map(({ locator }) => locator.first().waitFor({ state: 'visible', timeout }))); }
  catch (error) {
    try { record(await counts()); } catch { /* Diagnostic counts cannot replace the original failure. */ }
    throw new Error(movieFailureCode(error, 'movie_control_not_ready'));
  }
  const observed = await counts(); record(observed);
  requireMovie(observed.every(row => row.count <= 1), 'movie_control_not_unique');
  const index = observed.findIndex(row => row.count === 1);
  requireMovie(index >= 0, 'movie_control_not_ready');
  return candidates[index];
}

export function movieItemFromLocation(location, target) {
  try {
    const url = new URL(location, target), [route, query = ''] = url.hash.slice(1).split('?');
    if (url.origin !== target.origin || route !== '!/item' || url.username || url.password) return null;
    const values = new URLSearchParams(query).getAll('id');
    return values.length === 1 && ITEM_ID.test(values[0]) ? values[0] : null;
  } catch { return null; }
}

export function moviePlaybackIdentity(request, target, event) {
  requireMovie(['started', 'stopped'].includes(event), 'movie_report_event_invalid');
  const url = new URL(request.url()), suffix = event === 'started' ? '' : '/Stopped';
  requireMovie(request.method() === 'POST' && url.origin === target.origin &&
    new RegExp('^/(?:emby/)?Sessions/Playing' + suffix + '/?$', 'i').test(url.pathname), 'movie_report_scope_invalid');
  const bytes = request.postDataBuffer();
  requireMovie(Buffer.isBuffer(bytes) && bytes.length > 0 && bytes.length <= 65536, 'movie_report_body_unavailable');
  let body;
  try { body = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)); }
  catch { throw new Error('movie_report_body_invalid'); }
  const identity = value => typeof value === 'string' && /^[A-Za-z0-9_-]{1,256}$/.test(value);
  requireMovie(body && !Array.isArray(body) && typeof body === 'object' && ITEM_ID.test(body.ItemId ?? '') &&
    identity(body.MediaSourceId) && identity(body.PlaySessionId), 'movie_report_identity_missing');
  for (const key of ['SessionId', 'UserId']) requireMovie(body[key] === undefined || identity(body[key]), 'movie_report_identity_invalid');
  requireMovie(body.PositionTicks === undefined || Number.isSafeInteger(body.PositionTicks) && body.PositionTicks >= 0, 'movie_report_position_invalid');
  return { item_id: body.ItemId, media_source_id: body.MediaSourceId, play_session_id: body.PlaySessionId,
    session_id: body.SessionId ?? null, user_id: body.UserId ?? null, position_ticks: body.PositionTicks ?? null };
}

export function observeMovieResponse(page, target, event) {
  const suffix = { started: 'Playing', stopped: 'Playing/Stopped', logout: 'Logout' }[event];
  requireMovie(suffix, 'movie_response_event_invalid');
  const observed = page.waitForResponse(response => {
    const request = response.request();
    try { return request.method() === 'POST' && request.frame() === page.mainFrame() && new URL(response.url()).origin === target.origin &&
      new RegExp('^/(?:emby/)?Sessions/' + suffix + '/?$', 'i').test(new URL(response.url()).pathname); }
    catch { return false; }
  }, { timeout: 30000 }).then(response => ({ response }), error => ({ failure: movieFailureCode(error, 'movie_response_not_observed') }));
  let completion;
  return { wait(expected = {}) {
    return completion ??= (async () => {
      const value = await observed;
      requireMovie(!value.failure, value.failure);
      const response = value.response;
      requireMovie(response.status() === 204, 'movie_' + event + '_response_not_204');
      const identity = event === 'logout' ? null : moviePlaybackIdentity(response.request(), target, event);
      if (identity && expected.itemId) requireMovie(identity.item_id === expected.itemId, 'movie_playback_item_changed');
      if (identity && expected.identity) {
        requireMovie(['item_id', 'media_source_id', 'play_session_id'].every(key => identity[key] === expected.identity[key]), 'movie_stopped_identity_mismatch');
        for (const key of ['session_id', 'user_id']) requireMovie(!identity[key] || !expected.identity[key] || identity[key] === expected.identity[key], 'movie_stopped_owner_mismatch');
      }
      let finished;
      const completionResult = Promise.resolve().then(() => response.finished()).then(value => ({ value }),
        error => ({ failure: movieFailureCode(error, 'movie_response_completion_failed') }));
      try {
        // The page wait retains the caller's remaining budget; both promises are handled immediately.
        finished = await Promise.race([completionResult, page.waitForTimeout(10000).then(() => { throw new Error('movie_response_completion_timeout'); })]);
      } catch (error) { throw new Error(movieFailureCode(error, 'movie_response_completion_failed')); }
      requireMovie(!finished.failure, finished.failure);
      requireMovie(finished.value === null && !response.request().failure(), 'movie_response_incomplete');
      return { response_status: 204, response_complete: true, identity };
    })();
  } };
}

export async function completeMovieCleanup({ stop, logout, onFailure }) {
  for (const [operation, action] of [['stop', stop], ['logout', logout]]) {
    try { await action(); }
    catch (error) { onFailure(operation, movieFailureCode(error, 'movie_cleanup_' + operation + '_failed')); }
  }
}

export async function runMovieWorkflow({ page, report, snapshot, repeatLogin, target }) {
  const result = report.playback = { movie: MOVIE, phase: 'home', operation: 'home_ready', steps: [], lifecycles: [],
    outcome: 'in_progress', method: 'Original client UI actions and read-only HTMLMediaElement observations' };
  let itemId = null, playback = null, currentLogout = null, ownedSession = true, cleaning = false, signOutMenuRequested = false;
  const operation = value => { result.operation = value; };
  const counts = values => result.steps.push({ label: 'control-counts', phase: result.phase, operation: result.operation, controls: values });
  const locateButton = name => page.getByRole('button', { name, exact: typeof name === 'string' }).filter({ visible: true });
  const ready = candidates => waitMovieControl(candidates, { record: counts });

  async function metrics(label) {
    operation('metrics_' + label.replaceAll('-', '_'));
    const videos = await page.evaluate(() => [...document.querySelectorAll('video')].map(video => {
      const quality = typeof video.getVideoPlaybackQuality === 'function' ? video.getVideoPlaybackQuality() : null;
      const rect = video.getBoundingClientRect();
      return { current_time: Number.isFinite(video.currentTime) ? video.currentTime : null,
        duration: Number.isFinite(video.duration) ? video.duration : null, paused: video.paused, ended: video.ended, seeking: video.seeking,
        ready_state: video.readyState, network_state: video.networkState, video_width: video.videoWidth, video_height: video.videoHeight,
        total_video_frames: quality?.totalVideoFrames ?? null, dropped_video_frames: quality?.droppedVideoFrames ?? null,
        visible: rect.width > 0 && rect.height > 0 };
    }));
    result.steps.push({ label, videos, observed_at: new Date().toISOString() });
    return videos.find(video => video.visible) ?? videos[0] ?? null;
  }

  async function inspect(label) {
    operation('snapshot_' + label.replaceAll('-', '_'));
    await snapshot(label);
    operation('controls_' + label.replaceAll('-', '_'));
    result.steps.push({ label: `${label}-controls`, controls: await page.evaluate(() =>
      [...document.querySelectorAll('button,a,input,[role="button"],[role="slider"]')]
        .filter(element => element.getClientRects().length).slice(0, 180).map(element => ({
          tag: element.tagName.toLowerCase(), type: element.getAttribute('type'), role: element.getAttribute('role'),
          label: element.getAttribute('aria-label'), title: element.getAttribute('title'),
          class: typeof element.className === 'string' ? element.className : null, text: (element.innerText ?? '').trim().slice(0, 120) }))) });
  }

  async function diagnostic(label, action) {
    try { await action(); return null; }
    catch (error) {
      const code = movieFailureCode(error, 'movie_observation_failed');
      (result.observation_errors ??= []).push({ phase: result.phase, operation: label, code });
      return code;
    }
  }

  async function button(name, key) {
    operation(key + '_ready');
    const selected = await ready([{ key, locator: locateButton(name) }]);
    operation(key + '_click'); await selected.locator.click({ timeout: 8000 });
  }

  async function showControls() {
    operation('video_surface_ready');
    const selected = await ready([{ key: 'video_surface', locator: page.locator('video:visible') }]);
    const box = await selected.locator.boundingBox(); requireMovie(box, 'movie_video_surface_missing');
    await page.mouse.move(box.x + box.width * 0.45, box.y + box.height * 0.55, { steps: 4 });
    await page.mouse.move(box.x + box.width * 0.55, box.y + box.height * 0.75, { steps: 4 });
  }

  async function waitPlaying() {
    operation('playing_ready');
    await page.waitForFunction(() => [...document.querySelectorAll('video')].some(video =>
      video.getClientRects().length && !video.paused && video.readyState >= 2 && video.videoWidth > 0 && video.currentTime > 1), null, { timeout: 30000 });
  }

  async function advance(before, seconds = 2) {
    requireMovie(before && Number.isFinite(before.current_time) && Number.isFinite(before.total_video_frames), 'movie_advancement_baseline_missing');
    operation('frames_advance');
    await page.waitForFunction(expected => [...document.querySelectorAll('video')].some(video => video.getClientRects().length &&
      !video.paused && video.currentTime > expected.time + expected.seconds &&
      (video.getVideoPlaybackQuality?.().totalVideoFrames ?? 0) > expected.frames),
    { time: before.current_time, frames: before.total_video_frames, seconds }, { timeout: 15000 });
  }

  async function openMovieDetail(homeLabel) {
    operation('home_movie_ready');
    const titles = page.getByText(MOVIE, { exact: true }).filter({ visible: true });
    await titles.first().waitFor({ state: 'visible', timeout: 20000 });
    counts([{ control: 'movie_title', count: await titles.count() }]); await inspect(homeLabel);
    result.phase = 'movie_detail'; operation('movie_card_ready');
    const title = titles.first(), clickable = title.locator('xpath=ancestor-or-self::*[self::button or self::a][1]');
    const imageButton = title.locator('xpath=ancestor::*[contains(concat(" ", normalize-space(@class), " "), " cardBox ")][1]').locator('button.cardContent-button');
    const selected = await ready([{ key: 'movie_title_action', locator: clickable }, { key: 'movie_image_action', locator: imageButton }]);
    operation('movie_card_click'); await selected.locator.click({ timeout: 8000 });
    operation('movie_detail_location'); await page.waitForURL(location => movieItemFromLocation(location, target) !== null, { timeout: 20000 });
    const observedItem = movieItemFromLocation(page.url(), target);
    requireMovie(!itemId || observedItem === itemId, 'movie_relogin_item_changed'); itemId = observedItem; result.item_id = itemId;
    operation('movie_detail_title');
    // The item URL can change while both Home cards are still visible.
    const detailTitles = page.getByText(MOVIE, { exact: true }).filter({ visible: true });
    await detailTitles.first().waitFor({ state: 'visible', timeout: 10000 });
    counts([{ control: 'movie_detail_title', count: await detailTitles.count() }]);
  }

  async function beginPlayback(resume) {
    result.phase = resume ? 'resume' : 'play'; operation(resume ? 'resume_ready' : 'initial_play_ready');
    const selected = await ready(resume ? [{ key: 'resume', locator: locateButton(/\bResume\b/i) }] :
      [{ key: 'from_beginning', locator: locateButton('From Beginning') }, { key: 'play', locator: locateButton('Play') }]);
    await inspect(resume ? 'movie-before-resume' : 'movie-detail');
    operation('started_response_arm');
    playback = { start: observeMovieResponse(page, target, 'started'), identity: null, stop: null, back_attempted: false, stopped: false, evidence: null };
    operation(resume ? 'resume_click' : 'initial_play_click'); await selected.locator.click({ timeout: 8000 });
    operation('started_complete'); const started = await playback.start.wait({ itemId });
    requireMovie(!result.lifecycles.some(row => row.play_session_id === started.identity.play_session_id), 'movie_relogin_play_session_reused');
    playback.identity = started.identity;
    playback.evidence = { ...started.identity, started_status: 204, started_complete: true, stopped_complete: false,
      event_scope: 'Original logical page responses; physical cardinality remains in the gateway ledger.' };
    result.lifecycles.push(playback.evidence); await waitPlaying();
  }

  async function stopPlayback() {
    if (!playback || playback.stopped) return;
    if (!playback.stop) {
      const visible = await page.locator('video:visible').count();
      if (!visible && !playback.identity) return;
      await showControls(); operation('stop_back_ready');
      const selected = await ready([{ key: 'back', locator: locateButton('Back') }]);
      operation('stopped_response_arm');
      playback.stop = observeMovieResponse(page, target, 'stopped'); playback.back_attempted = true;
      operation('stop_back_click'); await selected.locator.click({ timeout: 8000 });
    }
    operation('stopped_complete'); const stopped = await playback.stop.wait({ itemId, identity: playback.identity });
    operation('media_stopped');
    await page.waitForFunction(() => [...document.querySelectorAll('video,audio')].every(media => media.paused), null, { timeout: 15000 });
    playback.stopped = true;
    if (playback.evidence) Object.assign(playback.evidence, { stopped_status: 204, stopped_complete: true, stopped_position_ticks: stopped.identity.position_ticks });
    else (result.cleanup_stopped_reports ??= []).push({ ...stopped.identity, stopped_status: 204, stopped_complete: true, started_complete: false });
  }

  async function logout(label) {
    if (!ownedSession) return;
    const observe = name => cleaning ? diagnostic(name, () => inspect(name)) : inspect(name);
    await observe(`${label}-before`);
    if (!currentLogout) {
      operation('sign_out_ready'); let signOut = locateButton(/\bSign Out\b/i);
      if (!await signOut.count() && !signOutMenuRequested) {
        operation('settings_ready'); const settings = await ready([{ key: 'settings', locator: locateButton('Settings') }]);
        operation('settings_click'); signOutMenuRequested = true; await settings.locator.click({ timeout: 8000 });
        await observe(`${label}-menu`);
        signOut = locateButton(/\bSign Out\b/i);
      }
      const selected = await ready([{ key: 'sign_out', locator: signOut }]);
      operation('logout_response_arm');
      currentLogout = { response: observeMovieResponse(page, target, 'logout'), submitted: true };
      report.logout = { attempted: true, result: 'submitted_outcome_unknown', owned_session: 'possibly_retained_owned_session_no_ui_logout' };
      operation('sign_out_click'); await selected.locator.click({ timeout: 8000 });
    }
    operation('logout_complete'); await currentLogout.response.wait();
    report.logout = { attempted: true, result: 'logout_http_accepted_ui_transition_pending', http_status: 204,
      owned_session: 'possibly_retained_owned_session_ui_logout_transition_unconfirmed' };
    operation('logout_view_ready');
    await ready([{ key: 'manual_login', locator: page.getByText('Manual Login', { exact: true }) },
      { key: 'login_form', locator: page.locator('form:has(input[type="password"]:visible)') }]);
    report.logout.result = 'ui_logout_http_accepted_and_login_view_visible'; report.logout.owned_session = 'ui_logout_observed';
    (report.logout_history ??= []).push(report.logout); ownedSession = false;
    await observe(`${label}-after`);
  }

  try {
    await openMovieDetail('movie-home'); await beginPlayback(false);
    const started = await metrics('playing-start'); await advance(started); const advanced = await metrics('playing-advanced');
    requireMovie(advanced && advanced.current_time > started.current_time && advanced.total_video_frames > started.total_video_frames, 'movie_frames_not_advancing');
    await showControls(); await inspect('movie-playing');
    result.phase = 'pause'; await button('Pause', 'pause'); operation('paused_ready');
    await page.waitForFunction(() => { const videos = [...document.querySelectorAll('video')].filter(video => video.getClientRects().length); return videos.length === 1 && videos[0].paused; }, null, { timeout: 5000 });
    const paused = await metrics('paused'); requireMovie(paused?.paused, 'movie_pause_not_observed');
    for (const [phase, fraction, label] of [['seek_forward', 0.30, 'movie-seek-forward'], ['seek_backward', 0.20, 'movie-seek-backward']]) {
      result.phase = phase; await showControls(); operation('seek_slider_ready');
      await page.waitForFunction(() => [...document.querySelectorAll('input[type="range"]')].some(element => element.getClientRects().length &&
        /seek|progress|position|timeline/i.test([element.className, element.getAttribute('aria-label'), element.getAttribute('title')].join(' '))), null, { timeout: 10000 });
      const ranges = page.locator('input[type="range"]:visible'), candidates = [];
      for (let index = 0; index < await ranges.count(); index++) { const control = ranges.nth(index);
        if (/seek|progress|position|timeline/i.test(await control.evaluate(element => [element.className, element.getAttribute('aria-label'), element.getAttribute('title')].join(' ')))) candidates.push(control); }
      counts([{ control: 'seek_slider', count: candidates.length }]); requireMovie(candidates.length === 1, 'movie_seek_slider_not_unique');
      const before = await metrics(label + '-before'), box = await candidates[0].boundingBox();
      requireMovie(before && box && box.width >= 50, 'movie_seek_geometry_unavailable');
      operation('seek_click'); await candidates[0].click({ position: { x: box.width * fraction, y: box.height / 2 }, timeout: 8000 });
      operation('seek_complete');
      await page.waitForFunction(expected => [...document.querySelectorAll('video')].some(video => video.getClientRects().length && !video.seeking &&
        (expected.forward ? video.currentTime > expected.before + 10 : video.currentTime < expected.before - 10) &&
        Number.isFinite(video.duration) && Math.abs(video.currentTime - video.duration * expected.fraction) <= Math.max(5, video.duration * 0.02)),
      { before: before.current_time, fraction, forward: phase === 'seek_forward' }, { timeout: 15000 });
      await metrics(label + '-after'); await inspect(label);
    }
    const beforeContinue = await metrics('before-continue');
    if (beforeContinue?.paused) await button('Play', 'continue_play');
    await waitPlaying(); await advance(beforeContinue, 1.5);
    result.phase = 'stop'; const stoppingAt = await metrics('before-stop'); await inspect('movie-before-stop');
    await stopPlayback(); await inspect('movie-stopped'); await metrics('after-stop'); result.stop_position_seconds = stoppingAt?.current_time ?? null;
    result.phase = 'relogin'; await inspect('movie-before-relogin'); await logout('movie-relogin-sign-out');
    operation('repeat_login'); ownedSession = true; currentLogout = null; playback = null; signOutMenuRequested = false;
    report.logout = { attempted: false, result: 'not_attempted', owned_session: 'retained_owned_session_no_ui_logout' };
    await repeatLogin();
    await openMovieDetail('movie-relogin-home'); await beginPlayback(true);
    const resumed = await metrics('resumed');
    requireMovie(resumed && stoppingAt && Math.abs(resumed.current_time - stoppingAt.current_time) <= 15, 'movie_resume_position_mismatch');
    await advance(resumed); const resumedAdvanced = await metrics('resumed-advanced');
    requireMovie(resumedAdvanced && !resumedAdvanced.paused && resumedAdvanced.current_time > resumed.current_time + 1 &&
      resumedAdvanced.total_video_frames > resumed.total_video_frames, 'movie_resumed_frames_not_advancing');
    await inspect('movie-resumed'); await stopPlayback(); result.phase = 'logout'; await logout('movie-sign-out');
    result.phase = 'complete'; result.operation = 'complete'; result.outcome = 'movie_ui_flow_completed';
  } catch (error) {
    const failure = { phase: result.phase, operation: result.operation, code: movieFailureCode(error, 'movie_' + result.operation + '_failed') };
    result.failure = failure; result.outcome = 'blocked_at_observed_ui_step'; result.cleanup_attempted = true; cleaning = true;
    await diagnostic('blocked_metrics', () => metrics(`blocked-${failure.phase}`));
    await diagnostic('blocked_controls', () => inspect(`movie-blocked-${failure.phase}`));
    await completeMovieCleanup({ stop: stopPlayback, logout: () => logout('movie-blocked-cleanup'),
      onFailure(operation, code) { (result.cleanup_errors ??= []).push({ operation, code }); } });
    if (result.cleanup_errors?.length) result.cleanup_result = ownedSession
      ? 'ui_logout_not_completed_private_state_required' : 'ui_logout_completed_stop_evidence_incomplete';
    result.phase = failure.phase; result.operation = failure.operation;
    throw new Error(failure.code);
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  if (process.platform !== 'linux') throw new Error('Run movie acceptance only on the authorized Linux test environment.');
  const observer = fileURLToPath(new URL('./client-browser-observe.mjs', import.meta.url));
  const child = spawn(process.execPath, [observer, ...process.argv.slice(2), '--workflow', 'movie'], { stdio: 'inherit' });
  child.on('error', () => { process.stderr.write('The guarded observer could not start.\n'); process.exitCode = 1; });
  child.on('exit', (code, signal) => { process.exitCode = signal ? 1 : code ?? 1; });
}
