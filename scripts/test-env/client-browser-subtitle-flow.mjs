/** Select both owned external subtitle tracks through the original movie UI. */

import { createHash } from 'node:crypto';

const MOVIE = 'M3e Client Movie';
const CUES = ['Opening subtitle', 'Backward seek subtitle', 'Forward seek subtitle', 'Midpoint subtitle'];

export async function runSubtitleUI({ page, context, report, snapshot, target, movieId }) {
  const result = report.subtitle_flow = { phase: 'home_movie', outcome: 'in_progress',
    movie: MOVIE, steps: [], network_subtitles: [], initial_selection: 'Off', restored_selection: false,
    scope: 'External SRT and VTT selection, cue visibility, seek alignment, and return to Off through the original UI' };
  let responses = Promise.resolve();

  function fail(reason) {
    result.failure_reason = reason;
    const error = new Error(`subtitle_ui_${result.phase}_failed`);
    error.phase = result.phase;
    throw error;
  }

  async function media(label) {
    const data = await page.evaluate(allowed => [...document.querySelectorAll('video')].map(video => {
      const quality = typeof video.getVideoPlaybackQuality === 'function' ? video.getVideoPlaybackQuality() : null;
      return { current_time: Number.isFinite(video.currentTime) ? video.currentTime : null,
        duration: Number.isFinite(video.duration) ? video.duration : null, paused: video.paused,
        ready_state: video.readyState, network_state: video.networkState,
        video_width: video.videoWidth, video_height: video.videoHeight,
        total_video_frames: quality?.totalVideoFrames ?? null, dropped_video_frames: quality?.droppedVideoFrames ?? null,
        text_tracks: [...video.textTracks].map(track => ({ kind: track.kind, label: track.label, language: track.language,
          mode: track.mode, active_cues: [...(track.activeCues ?? [])].map(cue => ({ start: cue.startTime, end: cue.endTime,
            text: allowed.includes(cue.text) ? cue.text : '{unexpected cue}' })) })) };
    }), CUES);
    const visibleCues = [];
    for (const cue of CUES) if (await page.getByText(cue, { exact: true }).filter({ visible: true }).count()) visibleCues.push(cue);
    result.steps.push({ label, at: new Date().toISOString(), media: data, visible_dom_cues: visibleCues });
    return { video: data[0] ?? null, visibleCues };
  }

  async function inspect(label) {
    await snapshot(label);
    result.steps.push({ label: `${label}-controls`, controls: await page.locator('button:visible,[role="option"]:visible,[role="menuitem"]:visible,input[type="range"]:visible')
      .evaluateAll(elements => elements.slice(0, 80).map(element => ({ tag: element.tagName.toLowerCase(),
        label: element.getAttribute('aria-label'), title: element.getAttribute('title'), role: element.getAttribute('role'),
        class: typeof element.className === 'string' ? element.className : null, text: (element.innerText ?? '').trim().slice(0, 100) }))) });
  }

  async function showControls() {
    const box = await page.locator('video:visible').first().boundingBox();
    if (!box) fail('visible_movie_surface_missing');
    await page.mouse.move(box.x + box.width * 0.45, box.y + box.height * 0.55, { steps: 4 });
    await page.mouse.move(box.x + box.width * 0.55, box.y + box.height * 0.75, { steps: 4 });
    await page.waitForTimeout(300);
  }

  async function button(name) {
    const found = page.getByRole('button', { name, exact: typeof name === 'string' }).filter({ visible: true });
    if (await found.count() !== 1) fail('visible_button_missing_or_ambiguous');
    await found.click({ timeout: 8000 });
  }

  async function selectSubtitle(name, label) {
    await showControls();
    await button('Subtitles');
    await inspect(`${label}-menu`);
    const option = page.getByText(name, { exact: true }).filter({ visible: true });
    await option.waitFor({ state: 'visible', timeout: 8000 });
    if (await option.count() !== 1) fail('subtitle_option_missing_or_ambiguous');
    const action = option.locator('xpath=ancestor-or-self::*[self::button or self::a or @role="option" or @role="menuitem" or @role="radio"][1]');
    if (await action.count() === 1) await action.click({ timeout: 8000 });
    else await option.click({ timeout: 8000 });
    await page.waitForTimeout(400);
    await media(`${label}-selected`);
  }

  async function cue(expected, label) {
    for (let attempt = 0; attempt < 30; attempt += 1) {
      const state = await media(`${label}-${attempt}`);
      const native = state.video?.text_tracks.some(track => track.mode === 'showing' && track.active_cues.some(cue => cue.text === expected));
      if (native || state.visibleCues.includes(expected)) {
        await snapshot(label);
        return;
      }
      await page.waitForTimeout(200);
    }
    fail('expected_synthetic_subtitle_cue_not_observed');
  }

  async function observeResponse(response) {
    const url = new URL(response.url());
    const route = /\/(?:Videos|Items)\/([^/]+)\/([^/]+)\/Subtitles\/(\d+)(?:\/(\d{1,19}))?\/Stream\.(\w+)/i.exec(url.pathname);
    if (url.origin !== target.origin || !route) return;
    const record = { request_method: response.request().method(), status: response.status(), content_type: response.headers()['content-type'] ?? null,
      item_matches_owned_movie: route[1] === movieId, subtitle_index: Number(route[3]),
      start_position_ticks_path: route[4] ?? null, format: route[5],
      query_field_names: [...new Set(url.searchParams.keys())].slice(0, 48), body_result: 'not_read' };
    result.network_subtitles.push(record);
    if (!record.item_matches_owned_movie) { record.body_result = 'unowned_media_rejected'; return; }
    const size = response.headers()['content-length'];
    if (!size || !/^\d+$/.test(size) || Number(size) > 65536 || response.headers()['content-encoding']) {
      record.body_result = 'body_size_not_provably_bounded'; return;
    }
    let timer;
    try {
      const bytes = await Promise.race([response.body(), new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error('subtitle_body_timeout')), 3000);
      })]);
      if (bytes.length > 65536) { record.body_result = 'body_exceeds_limit'; return; }
      const text = bytes.toString('utf8');
      record.bytes = bytes.length;
      record.sha256 = createHash('sha256').update(bytes).digest('hex');
      record.starts_with_webvtt = text.trimStart().startsWith('WEBVTT');
      record.synthetic_cues_present = CUES.filter(cue => text.includes(cue));
      record.body_result = 'owned_subtitle_hashed';
    } catch { record.body_result = 'body_unavailable_or_timed_out'; }
    finally { clearTimeout(timer); }
  }

  const listener = response => { responses = responses.then(() => observeResponse(response)); };
  context.on('response', listener);
  try {
    const movie = page.getByText(MOVIE, { exact: true }).filter({ visible: true }).first();
    await movie.waitFor({ state: 'visible', timeout: 20000 });
    await movie.locator('xpath=ancestor-or-self::*[self::button or self::a][1]').click({ timeout: 8000 });
    await page.waitForTimeout(500);
    await inspect('subtitle-movie-detail-before');
    result.phase = 'start_movie';
    const beginning = page.getByRole('button', { name: 'From Beginning', exact: true }).filter({ visible: true });
    if (await beginning.count() === 1) await beginning.click({ timeout: 8000 });
    else await button('Play');
    await page.waitForFunction(() => [...document.querySelectorAll('video')].some(video =>
      !video.paused && video.readyState >= 2 && video.videoWidth > 0 && video.currentTime > 0.5), null, { timeout: 30000 });
    await showControls();
    await button('Pause');
    const initial = await media('subtitle-initial-paused');
    if (!initial.video?.paused || initial.video.current_time >= 5) fail('opening_cue_window_was_not_preserved');
    if (initial.video.text_tracks.some(track => track.mode === 'showing')) fail('initial_subtitle_selection_was_not_off');
    result.phase = 'select_external_srt';
    await selectSubtitle('English (SRT)', 'subtitle-srt');
    await cue('Opening subtitle', 'subtitle-srt-opening-cue');
    result.phase = 'seek_with_srt';
    await showControls();
    const slider = page.locator('input.videoOsdPositionSlider:visible');
    const box = await slider.boundingBox();
    if (!box || box.width < 50) fail('observed_movie_position_slider_missing');
    await slider.click({ position: { x: box.width * 0.205, y: box.height / 2 }, timeout: 8000 });
    await page.waitForTimeout(400);
    const sought = await media('subtitle-srt-after-seek');
    if (!sought.video || sought.video.current_time < 118 || sought.video.current_time >= 128) fail('subtitle_seek_missed_synthetic_cue_window');
    await cue('Forward seek subtitle', 'subtitle-srt-forward-cue');
    result.phase = 'select_external_vtt';
    await selectSubtitle('English (VTT)', 'subtitle-vtt');
    await cue('Forward seek subtitle', 'subtitle-vtt-forward-cue');
    result.phase = 'restore_off';
    await selectSubtitle('Off', 'subtitle-off');
    const off = await media('subtitle-off-verified');
    if (off.video?.text_tracks.some(track => track.mode === 'showing') || off.visibleCues.length) fail('subtitle_off_state_not_restored');
    result.restored_selection = true;
    result.phase = 'stop_movie';
    await showControls();
    const previous = report.requests.length;
    await button('Back');
    await page.waitForTimeout(1000);
    await inspect('subtitle-movie-stopped');
    await media('subtitle-after-stop');
    await responses;
    const stops = report.requests.slice(previous).filter(entry => entry.method === 'POST' && /\/Sessions\/Playing\/Stopped\/?$/i.test(entry.route));
    if (!stops.some(entry => entry.status >= 200 && entry.status < 300)) fail('original_client_stopped_report_not_accepted');
    const accepted = result.network_subtitles.filter(entry => entry.item_matches_owned_movie && entry.status >= 200 && entry.status < 300);
    if (new Set(accepted.map(entry => entry.subtitle_index)).size < 2) fail('both_external_subtitle_delivery_requests_not_observed');
    result.phase = 'complete';
    result.outcome = 'external_srt_vtt_ui_selection_and_seek_completed';
    return result;
  } catch (error) {
    result.outcome = 'blocked_at_observed_ui_step';
    result.failure_reason ??= 'ui_control_or_subtitle_observation_failed';
    await media(`subtitle-blocked-${result.phase}`).catch(() => {});
    await inspect(`subtitle-blocked-${result.phase}`).catch(() => {});
    throw error;
  } finally {
    context.off('response', listener);
    await responses.catch(() => {});
  }
}
