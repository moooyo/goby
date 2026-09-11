/** Observe one original-client SRT selection without asserting playback acceptance. */
import { performance } from 'node:perf_hooks';
import { TextDecoder } from 'node:util';

const MOVIE = 'M3e Client Movie';
const CUES = ['Opening subtitle', 'Backward seek subtitle', 'Forward seek subtitle', 'Midpoint subtitle'];
const MAX_RESPONSE_BYTES = 256 * 1024;
const MAX_PARSED_BYTES = 1024 * 1024;
const MAX_RESPONSES = 12;
const FORMATS = new Set(['srt', 'subrip', 'vtt', 'webvtt', 'ass', 'ssa', 'ttml', 'pgs', 'pgssub', 'dvdsub', 'dvbsub', 'eia608', 'eia708', 'eia_608', 'eia_708', 'mov_text']);
const METHODS = new Set(['encode', 'embed', 'external', 'hls', 'videosidedata']);
const PROTOCOLS = new Set(['http', 'https', 'file', 'hls', 'ftp', 'rtsp', 'rtmp', 'rtp', 'udp']);
const CONTAINERS = new Set(['mp4', 'm4v', 'mkv', 'matroska', 'webm', 'mpegts', 'ts', 'm2ts', 'mov', 'avi', 'wmv', 'asf',
  'mpeg', 'vob', 'ogg', 'ogv', '3gp', 'flv', 'mp3', 'flac', 'm4a', 'aac', 'wav', 'alac', 'aiff', 'ape', 'opus']);
const SOURCE_FIELDS = ['Id', 'Name', 'Path', 'Protocol', 'Container', 'MediaStreams', 'DefaultAudioStreamIndex',
  'DefaultSubtitleStreamIndex', 'SupportsDirectPlay', 'SupportsDirectStream', 'SupportsTranscoding', 'TranscodingUrl'];
const STREAM_FIELDS = ['Type', 'Index', 'Codec', 'DeliveryMethod', 'DeliveryUrl', 'IsExternal', 'IsTextSubtitleStream',
  'SupportsExternalStream', 'IsDefault', 'IsForced', 'IsHearingImpaired'];
const PROFILE_FIELDS = ['Format', 'Method', 'Container', 'Language', 'Protocol', 'AllowChunkedResponse'];
const QUERY_FIELDS = ['SubtitleStreamIndex', 'AudioStreamIndex', 'StartTimeTicks', 'IsPlayback', 'AutoOpenLiveStream'];
const object = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const type = value => value === null ? 'null' : Array.isArray(value) ? 'array' : typeof value;
const mediaType = value => /^[A-Za-z0-9!#$&^_.+-]+\/[A-Za-z0-9!#$&^_.+-]+/.exec(value ?? '')?.[0].toLowerCase() ?? null;
const fieldName = value => /^[A-Za-z][A-Za-z0-9_]{0,63}$/.test(value) && !/^[a-f0-9]{24,}$/i.test(value) ? value : '{unsupported_field_name}';
function shape(owner, name) {
  return object(owner) && Object.hasOwn(owner, name) ? { present: true, type: type(owner[name]) } : { present: false, type: 'missing' };
}
function integerField(owner, name) {
  const result = shape(owner, name);
  if (result.present && result.type !== 'null') {
    if (Number.isSafeInteger(owner[name])) result.value = owner[name];
    else result.value_result = 'unsupported_integer';
  }
  return result;
}
function booleanField(owner, name) {
  const result = shape(owner, name);
  if (result.type === 'boolean') result.value = owner[name];
  else if (result.present && result.type !== 'null') result.value_result = 'unsupported_boolean';
  return result;
}
function enumField(owner, name, allowed, lists = false) {
  const result = shape(owner, name);
  if (!result.present || result.type === 'null') return result;
  const value = owner[name];
  const values = typeof value === 'string' && value.length <= 128 ? value.split(',') : [];
  if (typeof value === 'string' && value.length <= 128 && (value === '' || values.length <= (lists ? 16 : 1) &&
      values.every(item => allowed.has(item.trim().toLowerCase())))) result.value = value;
  else result.value_result = 'unsupported_enum_or_length';
  return result;
}
function fieldShapes(value, names) {
  return Object.fromEntries(names.map(name => [name, shape(value, name)]));
}
function fieldNames(value) {
  return object(value) ? Object.keys(value).slice(0, 64).map(fieldName) : [];
}
function actualFieldTypes(value) {
  return object(value) ? Object.fromEntries(Object.keys(value).slice(0, 64).map(name => [fieldName(name), type(value[name])])) : {};
}

export async function runSubtitleContractDiagnostics({ page, context, target, report, snapshot, movieId, userId }) {
  const started = performance.now();
  const result = report.subtitle_contract_diagnostics = { diagnostic_only: true, acceptance_passed: false,
    phase: 'before_movie_click', outcome: 'in_progress', movie: MOVIE, actions: [], observations: [], requests: [],
    request_overflow: 0, observer_errors: 0, json_responses_observed: 0, response_read_slots: 0, response_read_overflow: 0,
    parsed_response_bytes: 0, parsed_response_count: 0, request_parse_bytes: 0,
    cleanup: { off: 'not_attempted', off_selection_state: 'not_asserted', back: 'not_attempted', failures: [] },
    capture_policy: { json_routes: ['GET /emby/Users/{user}/Items/{item}', 'POST /emby/Items/{item}/PlaybackInfo'],
      successful_json_only: true, per_response_parse_bytes: MAX_RESPONSE_BYTES, total_response_parse_bytes: MAX_PARSED_BYTES,
      max_response_reads: MAX_RESPONSES, max_sources: 16, max_streams_per_source: 64, max_subtitle_profiles: 16,
      parse_budget_includes_invalid_json_attempts: true,
      body_deadline_ms: 3000, subtitle_bodies_read: false, hard_allocation_cap: false,
      allocation_boundary: 'Playwright may buffer the complete decoded response before the byte check. Limits bound parsing and retained projections, not transport allocation.',
      authentication_bodies_read: false, raw_bodies_and_urls_retained: false },
    initial_selection_policy: 'Record actual native modes and visible synthetic cues; do not infer an initial Off selection.' };
  const bindings = new WeakMap(), pendingBodies = new Set(), controlledErrors = new WeakSet();
  let currentAction = null, srtAction = null, chain = Promise.resolve(), queuedReads = 0, readsStopped = false, lastCaptureActivity = 0;
  let primaryFailure = null, captureFailure = null, listenersAttached = false, playRequested = false;
  const now = () => Math.round((performance.now() - started) * 1000) / 1000;
  const moment = () => ({ monotonic_ms: now(), phase: result.phase, action_current: currentAction?.name ?? 'before_movie_click',
    action_index: currentAction?.index ?? null, action_state: currentAction?.result ?? 'not_started' });
  function fail(reason) {
    const error = Object.assign(new Error(`subtitle_contract_${result.phase}_failed`), { phase: result.phase, safe_reason: reason });
    error.stack = undefined; controlledErrors.add(error); throw error;
  }
  function failure(error, fallback) {
    return controlledErrors.has(error) ? { phase: error.phase, reason: error.safe_reason } : { phase: result.phase, reason: fallback };
  }
  async function bounded(promise, milliseconds) {
    let timer;
    try { return await Promise.race([promise, new Promise((_, reject) => {
      timer = setTimeout(() => reject(Object.assign(new Error('subtitle_diagnostic_deadline'), { code: 'subtitle_diagnostic_deadline' })), milliseconds);
    })]); } finally { clearTimeout(timer); }
  }
  async function action(name, operation) {
    result.phase = name;
    const record = currentAction = { index: result.actions.length, name, started_ms: now(), ended_ms: null, result: 'running' };
    result.actions.push(record);
    if (name === 'select_srt') srtAction = record;
    try { const value = await operation(); record.result = 'completed'; return value; }
    catch (error) { record.result = 'failed'; throw error; }
    finally { record.ended_ms = now(); }
  }
  function subtitleRoute(value) {
    try {
      const url = new URL(value, target.origin + '/');
      if (url.origin !== target.origin || !['http:', 'https:'].includes(url.protocol)) return { result: 'unsupported' };
      const match = /^(\/emby)?\/(Videos|Items)\/([^/]+)\/([^/]+)\/Subtitles\/(\d{1,10})(?:\/(\d{1,19}))?\/Stream\.(srt|vtt)$/i.exec(url.pathname);
      if (!match || decodeURIComponent(match[3]) !== movieId || !/^[A-Za-z0-9_-]{1,128}$/.test(decodeURIComponent(match[4])) ||
          Number(match[5]) > 2147483647) return { result: 'unsupported' };
      const extension = match[7].toLowerCase();
      return { result: 'supported_owned_subtitle', pathname: `${match[1] ? '/emby' : ''}/${match[2].toLowerCase() === 'videos' ? 'Videos' : 'Items'}` +
        `/{item}/{source}/Subtitles/${match[5]}${match[6] === undefined ? '' : '/' + match[6]}/Stream.${extension}`,
        subtitle_index: Number(match[5]), start_ticks_present: match[6] !== undefined,
        ...(match[6] === undefined ? {} : { start_ticks: match[6] }), extension };
    } catch { return { result: 'unsupported' }; }
  }
  function queryProjection(url) {
    return Object.fromEntries(QUERY_FIELDS.map(name => {
      const keys = [...url.searchParams.keys()].filter(key => key.toLowerCase() === name.toLowerCase());
      if (!keys.length) return [name, { present: false, type: 'missing' }];
      const values = [...new Set(keys)].flatMap(key => url.searchParams.getAll(key));
      return [name, { present: true, type: 'query_string_values', count: values.length, overflow: values.length > 8,
        values: values.slice(0, 8).map(value => {
          if (['IsPlayback', 'AutoOpenLiveStream'].includes(name)) return /^(?:true|false)$/i.test(value)
            ? { type: 'string', parsed_type: 'boolean', value: value.toLowerCase() === 'true' } : { type: 'string', value_result: 'unsupported_boolean' };
          return /^-?\d{1,19}$/.test(value) ? { type: 'string', parsed_type: 'integer',
            integer_value: Number.isSafeInteger(Number(value)) ? Number(value) : value,
            integer_encoding: Number.isSafeInteger(Number(value)) ? 'number' : 'decimal_string' }
            : { type: 'string', value_result: 'unsupported_integer' };
        }) }];
    }));
  }
  function requestProfile(request) {
    const projection = { result: 'not_read' };
    try {
      const body = request.postDataBuffer();
      if (!body) { projection.result = 'no_request_body'; return projection; }
      if (body.length > 128 * 1024 || result.request_parse_bytes + body.length > 512 * 1024) {
        projection.result = 'request_parse_budget_exceeded'; return projection;
      }
      result.request_parse_bytes += body.length;
      const data = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body));
      projection.result = 'whitelisted_projection'; projection.type = type(data);
      projection.SubtitleStreamIndex = integerField(data, 'SubtitleStreamIndex');
      projection.DeviceProfile = shape(data, 'DeviceProfile');
      projection.SubtitleProfiles = shape(data?.DeviceProfile, 'SubtitleProfiles');
      const profiles = data?.DeviceProfile?.SubtitleProfiles;
      if (Array.isArray(profiles)) {
        projection.profile_count = profiles.length; projection.profile_overflow = profiles.length > 16;
        projection.profiles = profiles.slice(0, 16).map((profile, index) => {
          const fields = fieldShapes(profile, PROFILE_FIELDS);
          fields.Format = enumField(profile, 'Format', FORMATS, true); fields.Method = enumField(profile, 'Method', METHODS);
          fields.Container = enumField(profile, 'Container', CONTAINERS, true); fields.Protocol = enumField(profile, 'Protocol', PROTOCOLS);
          if (fields.Language.present && fields.Language.type === 'string') {
            if (profile.Language.length <= 64 && (profile.Language === '' || profile.Language.split(',').length <= 16 &&
                profile.Language.split(',').every(value => /^(?:[A-Za-z]{2,3}|\*)$/.test(value.trim())))) fields.Language.value = profile.Language;
            else fields.Language.value_result = 'unsupported_language_selector';
          }
          if (fields.AllowChunkedResponse.type === 'boolean') fields.AllowChunkedResponse.value = profile.AllowChunkedResponse;
          return { index, type: type(profile), field_names: fieldNames(profile), actual_field_types: actualFieldTypes(profile), fields };
        });
      }
    } catch { projection.result = 'request_body_unavailable_or_invalid_json'; }
    return projection;
  }
  function streams(owner) {
    const collection = owner?.MediaStreams;
    if (!Array.isArray(collection)) return { field: shape(owner, 'MediaStreams') };
    const subtitles = collection.filter(stream => object(stream) && stream.Type === 'Subtitle');
    return { field: shape(owner, 'MediaStreams'), stream_count: collection.length, subtitle_count: subtitles.length,
      overflow: collection.length > 64, subtitles: collection.slice(0, 64).filter(stream => object(stream) && stream.Type === 'Subtitle').map(stream => {
        const fields = fieldShapes(stream, STREAM_FIELDS);
        fields.Index = integerField(stream, 'Index'); fields.Codec = enumField(stream, 'Codec', FORMATS);
        fields.DeliveryMethod = enumField(stream, 'DeliveryMethod', METHODS);
        for (const name of ['IsExternal', 'IsTextSubtitleStream', 'IsForced', 'IsHearingImpaired']) fields[name] = booleanField(stream, name);
        if (fields.DeliveryUrl.type === 'string') fields.DeliveryUrl.normalized = subtitleRoute(stream.DeliveryUrl);
        return { fields, field_names: fieldNames(stream), actual_field_types: actualFieldTypes(stream) };
      }) };
  }
  function sourceProjection(source) {
    const fields = fieldShapes(source, SOURCE_FIELDS);
    fields.Container = enumField(source, 'Container', CONTAINERS, true);
    fields.DefaultSubtitleStreamIndex = integerField(source, 'DefaultSubtitleStreamIndex');
    for (const name of ['SupportsDirectPlay', 'SupportsDirectStream', 'SupportsTranscoding']) fields[name] = booleanField(source, name);
    return { type: type(source), field_names: fieldNames(source), actual_field_types: actualFieldTypes(source), fields, media_streams: streams(source) };
  }
  function responseProjection(data) {
    const projected = { root: sourceProjection(data), media_sources: { field: shape(data, 'MediaSources') } };
    if (Array.isArray(data?.MediaSources)) {
      projected.media_sources.count = data.MediaSources.length;
      projected.media_sources.overflow = data.MediaSources.length > 16;
      projected.media_sources.sources = data.MediaSources.slice(0, 16).map((source, index) => ({ index, ...sourceProjection(source) }));
    }
    return projected;
  }
  function requestKind(request) {
    const raw = request.url();
    if (typeof raw !== 'string' || raw.length > 32768) return null;
    const url = new URL(raw);
    if (url.origin !== target.origin || url.username || url.password) return null;
    if (request.method() === 'GET' && url.pathname === `/emby/Users/${encodeURIComponent(userId)}/Items/${encodeURIComponent(movieId)}`) {
      return { kind: 'item_detail', route: '/emby/Users/{user}/Items/{item}', url };
    }
    if (request.method() === 'POST' && url.pathname === `/emby/Items/${encodeURIComponent(movieId)}/PlaybackInfo`) {
      return { kind: 'playback_info', route: '/emby/Items/{item}/PlaybackInfo', url };
    }
    if (request.method() === 'GET') {
      const normalized = subtitleRoute(raw);
      if (normalized.result === 'supported_owned_subtitle') return { kind: 'subtitle_delivery', route: normalized.pathname, normalized };
    }
    if (request.method() === 'POST' && /^(?:\/emby)?\/Sessions\/Playing\/Stopped\/?$/i.test(url.pathname)) {
      return { kind: 'playback_stopped', route: '/emby/Sessions/Playing/Stopped' };
    }
    return null;
  }
  const onRequest = request => {
    try {
      const selected = requestKind(request);
      if (!selected) return;
      lastCaptureActivity = now();
      if (result.requests.length >= 128) { result.request_overflow += 1; return; }
      const entry = { request_index: result.requests.length, kind: selected.kind, method: request.method(), route: selected.route,
        request_received: moment(), response_received: null, status: null, content_type: null, finished: false, failed: false, body_result: 'not_read' };
      bindings.set(request, entry); result.requests.push(entry);
      if (selected.kind === 'item_detail' || selected.kind === 'playback_info') entry.query = queryProjection(selected.url);
      if (selected.kind === 'playback_info') entry.request_body = requestProfile(request);
      if (selected.kind === 'subtitle_delivery') entry.subtitle = selected.normalized;
    } catch { result.observer_errors += 1; }
  };
  async function readResponse(response, entry) {
    if (readsStopped || result.parsed_response_bytes >= MAX_PARSED_BYTES) { entry.body_result = 'reads_stopped_or_parse_budget_exhausted'; return; }
    entry.body_read_started = moment();
    let bodyPromise;
    try {
      bodyPromise = response.body(); pendingBodies.add(bodyPromise);
      bodyPromise.then(() => pendingBodies.delete(bodyPromise), () => pendingBodies.delete(bodyPromise));
      const bytes = await bounded(bodyPromise, 3000);
      entry.decoded_bytes = bytes.length;
      if (bytes.length > MAX_RESPONSE_BYTES) { entry.body_result = 'decoded_response_exceeds_parse_limit'; readsStopped = true; return; }
      if (result.parsed_response_bytes + bytes.length > MAX_PARSED_BYTES) {
        entry.body_result = 'total_parse_budget_exceeded'; readsStopped = true; return;
      }
      result.parsed_response_bytes += bytes.length; result.parsed_response_count += 1;
      const data = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes));
      entry.projection = responseProjection(data); entry.body_result = 'whitelisted_json_projection';
    } catch (error) {
      entry.body_result = error?.code === 'subtitle_diagnostic_deadline' ? 'body_read_timed_out' : 'body_unavailable_or_invalid_json';
      readsStopped = true;
    } finally { entry.body_read_finished = moment(); }
  }
  const onResponse = response => {
    try {
      const entry = bindings.get(response.request());
      if (!entry) return;
      lastCaptureActivity = now();
      entry.response_received = moment(); entry.status = response.status(); entry.content_type = mediaType(response.headers()['content-type']);
      if (!['item_detail', 'playback_info'].includes(entry.kind)) return;
      if (entry.status < 200 || entry.status >= 300) { entry.body_result = 'http_not_successful'; return; }
      if (!/^application\/(?:[a-z0-9.+-]+\+)?json$/.test(entry.content_type ?? '')) { entry.body_result = 'non_json_content_type'; return; }
      result.json_responses_observed += 1;
      if (result.response_read_slots >= MAX_RESPONSES) { result.response_read_overflow += 1; entry.body_result = 'response_read_count_limit'; return; }
      result.response_read_slots += 1; queuedReads += 1; entry.body_result = 'queued';
      chain = chain.then(() => readResponse(response, entry)).catch(() => {
        entry.body_result = 'response_projection_failed'; readsStopped = true; result.observer_errors += 1;
      }).finally(() => { queuedReads -= 1; });
    } catch { result.observer_errors += 1; }
  };
  const onFinished = request => { const entry = bindings.get(request); if (entry) { lastCaptureActivity = now(); entry.finished = true; entry.finished_at = moment(); } };
  const onFailed = request => { const entry = bindings.get(request); if (entry) { lastCaptureActivity = now(); entry.failed = true; entry.failed_at = moment(); } };

  async function media(label) {
    const videos = await page.evaluate(allowed => [...document.querySelectorAll('video')].slice(0, 4).map(video => ({
      visible: Boolean(video.getClientRects().length), current_time: Number.isFinite(video.currentTime) ? video.currentTime : null,
      duration: Number.isFinite(video.duration) ? video.duration : null, paused: video.paused, ready_state: video.readyState,
      video_width: video.videoWidth, video_height: video.videoHeight, track_count: video.textTracks.length,
      tracks: [...video.textTracks].slice(0, 16).map(track => ({
        mode: ['disabled', 'hidden', 'showing'].includes(track.mode) ? track.mode : 'unsupported',
        active_cue_count: track.activeCues?.length ?? 0, cues: [...(track.activeCues ?? [])].slice(0, 16)
          .filter(cue => allowed.includes(cue.text)).map(cue => ({ text: cue.text, start: cue.startTime, end: cue.endTime })) })) })), CUES);
    const visibleCues = [];
    for (const cue of CUES) if (await page.getByText(cue, { exact: true }).filter({ visible: true }).count()) visibleCues.push(cue);
    const observation = { label, observed: moment(), videos, visible_dom_cues: visibleCues };
    result.observations.push(observation); return observation;
  }
  async function button(name) {
    const found = page.getByRole('button', { name, exact: true }).filter({ visible: true });
    if (await found.count() !== 1) fail('visible_button_missing_or_ambiguous');
    await found.click({ timeout: 8000 });
  }
  async function showControls() {
    const videos = page.locator('video:visible');
    if (await videos.count() !== 1) fail('visible_video_missing_or_ambiguous');
    const box = await videos.boundingBox();
    if (!box) fail('visible_video_surface_missing');
    await page.mouse.move(box.x + box.width * 0.45, box.y + box.height * 0.55, { steps: 4 });
    await page.mouse.move(box.x + box.width * 0.55, box.y + box.height * 0.75, { steps: 4 });
    await page.waitForTimeout(250);
  }
  async function selectSubtitle(name, label) {
    await action(`open_${label}_menu`, async () => {
      if (await page.getByRole('button', { name: 'Subtitles', exact: true }).filter({ visible: true }).count() !== 1) await showControls();
      await button('Subtitles');
    });
    const option = page.getByText(name, { exact: true }).filter({ visible: true });
    await option.waitFor({ state: 'visible', timeout: 8000 });
    if (await option.count() !== 1) fail('subtitle_option_missing_or_ambiguous');
    await snapshot(`subtitle-contract-${label}-menu`);
    await action(label === 'srt' ? 'select_srt' : 'select_off', async () => {
      const control = option.locator('xpath=ancestor-or-self::*[self::button or self::a or @role="option" or @role="menuitem" or @role="radio"][1]');
      const clickable = await control.count() === 1 ? control : option;
      currentAction.click_started_ms = now();
      try { await clickable.click({ timeout: 8000 }); }
      finally { currentAction.click_ended_ms = now(); }
    });
  }
  async function restoreOff() {
    await selectSubtitle('Off', 'off');
    await action('verify_off', async () => {
      const deadline = now() + 4000;
      do {
        const observed = await media('off-verification');
        if (observed.videos.some(video => video.visible) && !observed.videos.some(video => video.visible && video.tracks.some(track => track.mode === 'showing')) &&
            observed.visible_dom_cues.length === 0) { result.cleanup.off = 'off_clicked_and_no_showing_track_or_known_cue_observed'; return; }
        await page.waitForTimeout(200);
      } while (now() < deadline);
      fail('subtitle_off_state_not_observed');
    });
  }
  async function stopMovie() {
    result.phase = 'back_stop';
    if (await page.getByRole('button', { name: 'Back', exact: true }).filter({ visible: true }).count() !== 1) await showControls();
    await action('back_stop', async () => {
      const beginning = now();
      await button('Back');
      await page.locator('video:visible').waitFor({ state: 'hidden', timeout: 10000 });
      const deadline = now() + 10000;
      let stopped;
      do {
        stopped = result.requests.find(entry => entry.kind === 'playback_stopped' && entry.request_received.monotonic_ms >= beginning && entry.status !== null);
        if (stopped) break;
        await page.waitForTimeout(100);
      } while (now() < deadline);
      result.cleanup.back_status = stopped?.status ?? null;
      if (!stopped || stopped.status < 200 || stopped.status >= 300) fail('original_client_stopped_response_not_accepted');
      result.cleanup.back = 'back_ui_and_stopped_http_accepted';
      await media('after-back'); await snapshot('subtitle-contract-after-back');
    });
  }
  async function closeCapture() {
    const deadline = now() + 5000;
    try {
      while (now() < deadline && (queuedReads || pendingBodies.size || result.requests.some(entry => !entry.finished && !entry.failed) || now() - lastCaptureActivity < 500)) {
        await page.waitForTimeout(100);
      }
    } catch { result.capture_wait_result = 'page_wait_unavailable'; }
    finally {
      context.off('request', onRequest); context.off('response', onResponse);
      context.off('requestfinished', onFinished); context.off('requestfailed', onFailed);
      listenersAttached = false; readsStopped = true;
    }
    try { await bounded(chain, 4000); }
    catch { result.capture_wait_result = 'response_queue_did_not_converge'; }
    result.capture_finished = { observed: moment(), queued_reads: queuedReads, pending_body_reads: pendingBodies.size, quiet_ms: now() - lastCaptureActivity,
      unfinished_requests: result.requests.filter(entry => !entry.finished && !entry.failed).length,
      converged: queuedReads === 0 && pendingBodies.size === 0 && now() - lastCaptureActivity >= 500 && result.requests.every(entry => entry.finished || entry.failed) };
    const jsonRequests = result.requests.filter(entry => ['item_detail', 'playback_info'].includes(entry.kind));
    result.capture_finished.response_array_overflow = jsonRequests.some(entry => entry.projection?.media_sources?.overflow ||
      entry.projection?.root?.media_streams?.overflow || entry.projection?.media_sources?.sources?.some(source => source.media_streams?.overflow));
    result.capture_finished.response_projection_complete = jsonRequests.length > 0 && !result.request_overflow && !result.observer_errors &&
      !result.capture_finished.response_array_overflow && jsonRequests.every(entry => entry.body_result === 'whitelisted_json_projection');
    const playbackRequests = jsonRequests.filter(entry => entry.kind === 'playback_info');
    result.capture_finished.request_profile_projection_complete = playbackRequests.length > 0 && playbackRequests
      .every(entry => entry.request_body?.result === 'whitelisted_projection' && !entry.request_body.profile_overflow);
    const relative = time => srtAction?.click_started_ms === undefined ? 'srt_click_not_invoked' : time < srtAction.click_started_ms ? 'before_srt_click'
      : srtAction.click_ended_ms === undefined || time <= srtAction.click_ended_ms ? 'during_srt_click' : 'after_srt_click';
    result.srt_action_window = srtAction ? { action_index: srtAction.index, started_ms: srtAction.started_ms, ended_ms: srtAction.ended_ms,
      click_started_ms: srtAction.click_started_ms ?? null, click_ended_ms: srtAction.click_ended_ms ?? null,
      boundary: 'Playwright click invocation and completion; requests and responses retain their original receipt times' } : null;
    for (const entry of result.requests) {
      entry.request_relative_to_srt = relative(entry.request_received.monotonic_ms);
      entry.response_relative_to_srt = entry.response_received ? relative(entry.response_received.monotonic_ms) : 'response_not_observed';
    }
  }

  try {
    if (!target || typeof target.origin !== 'string' || !/^https?:$/.test(target.protocol) ||
        ![movieId, userId].every(value => typeof value === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(value))) fail('invalid_owned_diagnostic_scope');
    context.on('request', onRequest); context.on('response', onResponse);
    context.on('requestfinished', onFinished); context.on('requestfailed', onFailed); listenersAttached = true;
    await action('movie_card', async () => {
      const beforeURL = page.url();
      const movies = page.getByText(MOVIE, { exact: true }).filter({ visible: true });
      await movies.first().waitFor({ state: 'visible', timeout: 20000 });
      result.visible_movie_title_count = await movies.count();
      const title = movies.first(), clickable = title.locator('xpath=ancestor-or-self::*[self::button or self::a][1]');
      if (await clickable.count() === 1) await clickable.click({ timeout: 8000 });
      else {
        const card = title.locator('xpath=ancestor::*[contains(concat(" ", normalize-space(@class), " "), " cardBox ")][1]');
        const control = card.locator('button.cardContent-button');
        if (await control.count() !== 1) fail('owned_movie_card_control_missing');
        await control.click({ timeout: 8000 });
      }
      await page.waitForURL(value => value.toString() !== beforeURL, { timeout: 10000 });
      result.detail_url_changed = true;
    });
    await snapshot('subtitle-contract-movie-detail');
    await action('start_movie', async () => {
      const beginning = page.getByRole('button', { name: 'From Beginning', exact: true }).filter({ visible: true });
      const play = page.getByRole('button', { name: 'Play', exact: true }).filter({ visible: true });
      const deadline = now() + 10000;
      for (;;) {
        const beginningCount = await beginning.count(), playCount = await play.count();
        if (beginningCount > 1 || playCount > 1) fail('movie_play_control_ambiguous');
        if (beginningCount === 1 || playCount === 1) {
          result.start_control = beginningCount === 1 ? 'From Beginning' : 'Play';
          playRequested = true;
          if (beginningCount === 1) await beginning.click({ timeout: 8000 });
          else await button('Play');
          break;
        }
        if (now() >= deadline) fail('movie_play_control_not_observed');
        await page.waitForTimeout(100);
      }
      await page.waitForFunction(() => [...document.querySelectorAll('video')].some(video => video.getClientRects().length &&
        !video.paused && video.readyState >= 2 && video.videoWidth > 0 && video.currentTime > 0.5), null, { timeout: 30000 });
    });
    await action('pause_movie', async () => {
      await showControls(); await button('Pause');
      await page.waitForFunction(() => [...document.querySelectorAll('video')].some(video => video.getClientRects().length && video.paused),
        null, { timeout: 8000 });
    });
    result.initial_subtitle_observation = await media('initial-paused-before-srt');
    await snapshot('subtitle-contract-initial-paused');
    await selectSubtitle('English (SRT)', 'srt');
    await action('observe_first_cue', async () => {
      const deadline = now() + 4000;
      result.first_cue = { expected: CUES[0], observed: false, result: 'not_observed_in_short_window', samples: 0 };
      do {
        const observed = await media('srt-first-cue'); result.first_cue.samples += 1;
        const native = observed.videos.some(video => video.visible && video.tracks.some(track => track.mode === 'showing' && track.cues.some(cue => cue.text === CUES[0])));
        if (native || observed.visible_dom_cues.includes(CUES[0])) {
          result.first_cue.observed = true; result.first_cue.result = native ? 'observed_native_showing_cue' : 'observed_visible_dom_cue'; break;
        }
        await page.waitForTimeout(200);
      } while (now() < deadline);
      result.first_cue.finished_ms = now(); await snapshot('subtitle-contract-srt-observed');
    });
  } catch (error) { primaryFailure = failure(error, 'original_ui_diagnostic_step_failed'); }
  finally {
    if (listenersAttached) {
      try {
        if (playRequested || await page.locator('video:visible').count()) {
          try { await restoreOff(); }
          catch (error) { result.cleanup.off = 'failed'; result.cleanup.failures.push(failure(error, 'off_ui_cleanup_failed')); }
          try { await stopMovie(); }
          catch (error) { result.cleanup.back = 'failed'; result.cleanup.failures.push(failure(error, 'back_ui_cleanup_failed')); }
        } else { result.cleanup.off = 'no_visible_video'; result.cleanup.back = 'no_visible_video'; }
      } catch (error) { result.cleanup.failures.push(failure(error, 'cleanup_video_observation_failed')); }
      finally {
        try { await closeCapture(); }
        catch { captureFailure = { phase: 'response_capture', reason: 'subtitle_capture_cleanup_failed' }; }
      }
    }
    if (result.capture_finished && (!result.capture_finished.converged || !result.capture_finished.response_projection_complete ||
        !result.capture_finished.request_profile_projection_complete || result.response_read_overflow)) {
      captureFailure ??= { phase: 'response_capture', reason: 'subtitle_response_capture_incomplete' };
    }
    result.primary_failure = primaryFailure;
    result.capture_failure = captureFailure;
    result.phase = primaryFailure?.phase ?? result.cleanup.failures[0]?.phase ?? captureFailure?.phase ?? 'complete';
    result.outcome = primaryFailure || result.cleanup.failures.length ? 'diagnostic_ui_or_cleanup_failed'
      : captureFailure ? 'diagnostic_response_capture_incomplete' : 'diagnostic_completed';
    result.finished_monotonic_ms = now();
  }
  if (primaryFailure || result.cleanup.failures.length || captureFailure) fail(primaryFailure?.reason ?? result.cleanup.failures[0]?.reason ?? captureFailure.reason);
  return result;
}
