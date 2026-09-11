/** Drive the original Emby audio UI using visible controls and read-only media observations. */

const ALBUMS = ['M3e Synthetic Album', 'Music'];
const TRACKS = ['M3e MP3', 'M3e FLAC'];
const EXPECTED_DURATION = 180;
const BUTTONS = 'button:visible,a:visible,[role="button"]:visible';
const AUDIO_RESPONSE_LIMIT = 64;
const AUDIO_CONTENT_TYPES = new Set(['application/json', 'audio/mpeg', 'audio/flac', 'audio/x-flac', 'audio/aac', 'audio/mp4',
  'audio/ogg', 'audio/opus', 'audio/wav', 'audio/x-wav', 'application/octet-stream',
  'application/vnd.apple.mpegurl', 'application/x-mpegurl']);
const AUDIO_ERROR_BODY_LIMIT = 8192;
const AUDIO_ERROR_READ_LIMIT = 4;
const AUDIO_ERROR_TOTAL_LIMIT = 32768;
const MEDIA_ENUMS = new Set(('mp3 mp2 mpa mp4 m4a m4b fmp4 aac adts flac alac webm webma opus vorbis ogg oga wav wave ' +
  'ts mpegts asf wma wmav1 wmav2 wmapro ac3 eac3 dts truehd pcm_s16le pcm_s24le pcm_s32le pcm_f32le pcm_f64le pcm_u8 copy').split(' '));
const MEDIA_PROTOCOLS = new Set(['http', 'hls', 'dash']);
const MEDIA_ENUM_FIELDS = ['Container', 'AudioCodec', 'TranscodingContainer', 'TranscodingProtocol'];
const MEDIA_NUMBER_FIELDS = ['MaxStreamingBitrate', 'StartTimeTicks', 'AudioBitRate', 'MaxAudioBitDepth', 'MaxAudioSampleRate', 'TranscodingMaxAudioChannels'];
const MEDIA_BOOLEAN_FIELDS = ['EnableRedirection', 'EnableRemoteMedia'];

function scrubAudioError(value) {
  if (typeof value !== 'string') return null;
  return value.split(/[\r\n]/, 1)[0].slice(0, 4096)
    .replace(/(?:https?|wss?|file|blob|data):[^\s<>"']+|\/\/[^\s<>"']+/gi, '[redacted URL]')
    .replace(/(?:^|\s)(?:\/[\w./?%=&+-]+|[A-Za-z]:\\[^\s]+)/g, ' [redacted path]')
    .replace(/\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b/g, '[redacted address]')
    .replace(/(["'])[^"'\r\n]*\1/g, '[redacted quoted value]')
    .replace(/\b(?:access[_-]?token|token|api[_-]?key|password|passwd|pwd|authorization|credential|secret|username|user[_-]?id|device[_-]?id|server[_-]?id)\b\s*(?:[:=]\s*|\s+)\S+/gi, '[redacted secret field]')
    .replace(/\b([A-Za-z][A-Za-z0-9_.-]{0,63})\s*=\s*[^\s,;]+/g, '$1=[redacted value]')
    .replace(/\b[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\b|\b[0-9a-f]{16,}\b/gi, '[redacted ID]')
    .replace(/\b(?=[A-Za-z0-9_-]{20,}\b)(?=[A-Za-z0-9_-]*[0-9])[A-Za-z0-9_-]+\b|\b[A-Za-z0-9_+\/-]{40,}={0,2}/g, '[redacted opaque value]')
    .replace(/\bm3e-[A-Za-z0-9_-]+\b/gi, '[redacted fixture account]')
    .trim().slice(0, 512);
}

function audioRequestQuery(searchParams, ownedId) {
  const safeKey = key => /^[A-Za-z_][A-Za-z0-9_.-]{0,95}$/.test(key) ? key : '{unsupported field name}';
  const fields = [...new Set(searchParams.keys())];
  const known = new Map([...MEDIA_ENUM_FIELDS, ...MEDIA_NUMBER_FIELDS, ...MEDIA_BOOLEAN_FIELDS, 'MediaSourceId']
    .map(field => [field.toLowerCase(), field]));
  const values = {};
  for (const [lower, field] of known) {
    const actual = fields.filter(key => key.toLowerCase() === lower).flatMap(key => searchParams.getAll(key));
    if (!actual.length) continue;
    if (field === 'MediaSourceId') { values[field] = { matches_owned_source: actual.length === 1 && actual[0] === ownedId }; continue; }
    if (actual.length !== 1) { values[field] = { state: 'invalid', reason: 'duplicate_parameter' }; continue; }
    const value = actual[0];
    if (value === '') { values[field] = { state: 'empty' }; continue; }
    if (MEDIA_ENUM_FIELDS.includes(field)) {
      const tokens = value.split(/[,|]/), allowed = field === 'TranscodingProtocol' ? MEDIA_PROTOCOLS : MEDIA_ENUMS;
      values[field] = value.length <= 1024 && tokens.length <= 32 && tokens.every(token => allowed.has(token.toLowerCase()))
        ? { state: 'valid', tokens, separators: value.match(/[,|]/g) ?? [] } : { state: 'invalid', reason: 'unsupported_media_enum_list' };
    } else if (MEDIA_NUMBER_FIELDS.includes(field)) {
      values[field] = /^[0-9]{1,16}$/.test(value) && Number.isSafeInteger(Number(value))
        ? { state: 'valid', value: Number(value) } : { state: 'invalid', reason: 'nonnegative_safe_integer_required' };
    } else {
      values[field] = /^(?:true|false)$/i.test(value) ? { state: 'valid', value: value.toLowerCase() === 'true' }
        : { state: 'invalid', reason: 'boolean_required' };
    }
  }
  return { field_names: fields.slice(0, 128).map(safeKey), omitted_field_count: Math.max(0, fields.length - 128),
    values, other_values: fields.filter(field => !known.has(field.toLowerCase())).slice(0, 128)
      .map(field => ({ field: safeKey(field), state: 'unknown' })) };
}

export async function runAudioUI({ page, context, report, snapshot, target, album = 'M3e Synthetic Album', track = 'M3e MP3', itemIdentity,
  homeLibrary, albumId }) {
  const selectedAlbum = ALBUMS.includes(album) ? album : null;
  const selectedTrack = TRACKS.includes(track) ? track : null;
  const otherTrack = TRACKS.find(title => title !== selectedTrack);
  const suppliedIdentity = itemIdentity !== undefined;
  const expectedContainer = selectedTrack === 'M3e MP3' ? 'mp3' : 'flac';
  let expectedOrigin = null;
  if (suppliedIdentity) {
    try {
      const bound = new URL(target?.origin ?? '');
      if (bound.protocol === 'http:' && ['127.0.0.1', 'localhost', '[::1]'].includes(bound.hostname) &&
          bound.origin === target.origin) expectedOrigin = bound.origin;
    } catch { /* An explicit bound target is required for source identity checks. */ }
  }
  const validIdentity = !suppliedIdentity || itemIdentity && typeof itemIdentity === 'object' && !Array.isArray(itemIdentity) &&
    expectedOrigin !== null &&
    typeof itemIdentity.id === 'string' && /^[a-f0-9]{32}$/.test(itemIdentity.id) &&
    [selectedTrack, 'M3e Client Audio'].includes(itemIdentity.name) && itemIdentity.container === expectedContainer &&
    Object.keys(itemIdentity).every(key => ['id', 'name', 'container'].includes(key));
  const selectedIdentity = suppliedIdentity && validIdentity
    ? { id: itemIdentity.id, name: itemIdentity.name, container: itemIdentity.container } : null;
  const suppliedHome = homeLibrary !== undefined || albumId !== undefined;
  const validHome = !suppliedHome || selectedIdentity && homeLibrary && typeof homeLibrary === 'object' &&
    Object.keys(homeLibrary).sort().join(',') === 'id,name' && homeLibrary.name === 'M3e Client Music' &&
    typeof homeLibrary.id === 'string' && /^[a-f0-9]{32}$/.test(homeLibrary.id) &&
    typeof albumId === 'string' && /^[a-f0-9]{32}$/.test(albumId);
  const displayTitle = selectedIdentity?.name ?? selectedTrack;
  const result = report.audio_flow = {
    album: selectedAlbum,
    track: selectedTrack,
    reference_tag_title: selectedTrack,
    actual_display_title: null,
    requested_item_identity: selectedIdentity,
    requested_home_library: suppliedHome && validHome ? { ...homeLibrary } : null,
    expected_duration_seconds: EXPECTED_DURATION,
    phase: !selectedAlbum ? 'album_selection' : !selectedTrack ? 'track_selection' : !validIdentity ? 'item_identity_selection' : 'home_album',
    steps: [],
    outcome: 'in_progress',
    coverage: selectedTrack,
    method: 'Original client UI actions and read-only HTMLMediaElement observations',
    cleanup_owner: 'caller',
  };
  let selectedAudio = null;
  const mediaEvents = context ?? page;
  const mediaResponses = new WeakSet(), pendingMediaResponses = new Set();
  let mediaListening = false, pendingErrorBodies = 0;
  const mediaNetwork = selectedIdentity ? result.media_network = {
    outcome: 'not_yet_observed', responses: [], response_limit: AUDIO_RESPONSE_LIMIT, response_overflow: 0,
    observer_errors: 0, expected_content_types: expectedContainer === 'mp3' ? ['audio/mpeg'] : ['audio/flac', 'audio/x-flac'],
    response_bodies_read: false, successful_response_bodies_read: false, browser_output_codec_verified: false, audible_output_verified: false,
    error_diagnostics: { diagnostic_only: true, read_limit: AUDIO_ERROR_READ_LIMIT, per_body_limit: AUDIO_ERROR_BODY_LIMIT,
      total_budget: AUDIO_ERROR_TOTAL_LIMIT, reads: 0, reserved_bytes: 0, parsed_bytes: 0, stopped_after_over_limit: false },
    mime_evidence: 'HTTP Content-Type declaration only; no decoded codec or audible-output claim',
    source_binding: 'The caller supplies the receipted ID/path/container mapping; this module observes UI, currentSrc, and HTTP metadata',
  } : null;

  function blocked(reason) {
    result.failure_reason = reason;
    const error = new Error(`audio_ui_${result.phase}_failed`);
    error.code = 'AUDIO_UI_FLOW_BLOCKED';
    error.stage = result.phase;
    throw error;
  }

  async function errorReadBeforeDeadline(promise, deadline) {
    let timer;
    try {
      const remaining = deadline - Date.now();
      if (remaining <= 0) throw new Error('audio_error_diagnostic_timeout');
      return await Promise.race([promise, new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error('audio_error_diagnostic_timeout')), remaining);
      })]);
    } finally { clearTimeout(timer); }
  }

  async function recordAudioError(response, entry) {
    const diagnostic = entry.error_body = { diagnostic_only: true, result: 'not_read' };
    const budget = mediaNetwork.error_diagnostics;
    const deadline = Date.now() + 2000;
    try {
      const declared = await errorReadBeforeDeadline(response.headerValue('content-length'), deadline);
      if (declared === null || declared === undefined) { diagnostic.result = 'content_length_missing_body_not_read'; return; }
      if (!/^[0-9]{1,16}$/.test(declared) || !Number.isSafeInteger(Number(declared))) {
        diagnostic.result = 'content_length_invalid_body_not_read'; return;
      }
      diagnostic.declared_bytes = Number(declared);
      if (Number(declared) > AUDIO_ERROR_BODY_LIMIT) { diagnostic.result = 'declared_body_over_limit'; return; }
      if (budget.stopped_after_over_limit || budget.reads >= AUDIO_ERROR_READ_LIMIT ||
          budget.reserved_bytes + AUDIO_ERROR_BODY_LIMIT > AUDIO_ERROR_TOTAL_LIMIT) {
        diagnostic.result = 'error_body_budget_exhausted'; return;
      }
      // Reserve the full per-body allowance before starting concurrent reads.
      budget.reads += 1;
      budget.reserved_bytes += AUDIO_ERROR_BODY_LIMIT;
      pendingErrorBodies += 1;
      const body = Promise.resolve().then(() => {
        if (Date.now() >= deadline) throw new Error('audio_error_diagnostic_timeout');
        mediaNetwork.response_bodies_read = true;
        return response.body();
      });
      body.finally(() => { pendingErrorBodies -= 1; }).catch(() => {});
      const bytes = await errorReadBeforeDeadline(body, deadline);
      diagnostic.actual_bytes = bytes.length;
      if (bytes.length > AUDIO_ERROR_BODY_LIMIT) {
        budget.stopped_after_over_limit = true;
        diagnostic.result = 'actual_body_over_limit_not_parsed'; return;
      }
      budget.parsed_bytes += bytes.length;
      let parsed;
      try { parsed = JSON.parse(bytes.toString('utf8')); }
      catch { diagnostic.result = 'invalid_json'; return; }
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) { diagnostic.result = 'non_object_json'; return; }
      const status = parsed.ResponseStatus;
      const candidates = [{ source: 'ResponseStatus', value: status }, { source: 'top_level', value: parsed }];
      const selected = candidates.find(candidate => candidate.value && typeof candidate.value === 'object' && !Array.isArray(candidate.value) &&
        ['ErrorCode', 'Message'].some(field => Object.hasOwn(candidate.value, field) && typeof candidate.value[field] === 'string'));
      if (!selected) { diagnostic.result = 'allowed_error_fields_absent'; return; }
      diagnostic.field_source = selected.source;
      if (Object.hasOwn(selected.value, 'ErrorCode')) diagnostic.error_code = scrubAudioError(selected.value.ErrorCode);
      if (Object.hasOwn(selected.value, 'Message')) diagnostic.message = scrubAudioError(selected.value.Message);
      diagnostic.result = 'sanitized_error_first_line_recorded';
    } catch { diagnostic.result = 'error_body_unavailable_or_timed_out'; }
  }

  async function recordAudioResponse(response, request, entry) {
    let timer;
    try {
      const [range, contentType] = await Promise.race([
        Promise.all([request.headerValue('range'), response.headerValue('content-type')]),
        new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('audio_response_metadata_timeout')), 1500); }),
      ]);
      const normalized = typeof contentType === 'string' ? contentType.split(';', 1)[0].trim().toLowerCase() : '';
      entry.response_content_type = AUDIO_CONTENT_TYPES.has(normalized) ? normalized : normalized ? 'other' : 'missing';
      entry.range_state = range === null || range === undefined ? 'missing' : 'outside_recording_allowlist';
      // Retain one bounded decimal byte range, including open-ended and suffix forms.
      if (typeof range === 'string' && range.length <= 64 && /^bytes=(?:[0-9]{1,20}-[0-9]{0,20}|-[0-9]{1,20})$/.test(range)) {
        entry.range = range;
        entry.range_state = 'recorded';
      }
      entry.metadata_result = 'recorded';
      clearTimeout(timer);
      if (entry.matches_owned_item && entry.http_status === 400 && normalized === 'application/json') {
        await recordAudioError(response, entry);
      }
    } catch {
      entry.metadata_result = 'unavailable_or_timed_out';
      mediaNetwork.observer_errors += 1;
    } finally { clearTimeout(timer); }
  }

  const audioResponseListener = response => {
    if (!mediaListening) return;
    try {
      const request = response.request();
      if (mediaResponses.has(request) || request.method() !== 'GET') return;
      const observed = new URL(response.url());
      if (observed.origin !== expectedOrigin || observed.username || observed.password) return;
      const requested = new URL(request.url());
      if (requested.origin !== expectedOrigin || requested.username || requested.password || requested.pathname !== observed.pathname) return;
      const match = /^\/(?:emby\/)?Audio\/([a-f0-9]{32})\/(universal(?:\.[a-z0-9_-]{1,32})?|stream(?:\.(?:mp3|flac|aac|m4a|ogg|opus|wav))?|original\.(?:mp3|flac|aac|m4a|ogg|opus|wav)|(?:master|main)\.m3u8)\/?$/i.exec(observed.pathname);
      if (!match) return;
      mediaResponses.add(request);
      if (mediaNetwork.responses.length >= AUDIO_RESPONSE_LIMIT) { mediaNetwork.response_overflow += 1; return; }
      const route = match[2].toLowerCase();
      const entry = { phase: result.phase, request_method: 'GET', http_status: response.status(),
        route_kind: route.startsWith('universal') ? 'universal' : route.startsWith('stream') ? 'stream'
          : route.startsWith('original.') ? 'original' : route === 'master.m3u8' ? 'hls_master' : 'hls_main',
        target_origin_matches: true, matches_owned_item: match[1] === selectedIdentity.id,
        metadata_result: 'pending', response_content_type: null, range_state: 'pending' };
      if (entry.matches_owned_item) entry.request_query = audioRequestQuery(requested.searchParams, selectedIdentity.id);
      mediaNetwork.responses.push(entry);
      const pending = recordAudioResponse(response, request, entry);
      pendingMediaResponses.add(pending);
      pending.finally(() => pendingMediaResponses.delete(pending)).catch(() => {});
    } catch { mediaNetwork.observer_errors += 1; }
  };

  async function finishAudioResponses() {
    if (!mediaNetwork) return;
    if (mediaListening) {
      mediaListening = false;
      mediaEvents.off('response', audioResponseListener);
    }
    await Promise.allSettled([...pendingMediaResponses]);
    mediaNetwork.listener_removed = true;
    mediaNetwork.pending_metadata_drained = pendingMediaResponses.size === 0;
    mediaNetwork.error_diagnostics.pending_body_reads_at_return = pendingErrorBodies;
    const accepted = mediaNetwork.responses.filter(entry => entry.matches_owned_item &&
      [200, 206].includes(entry.http_status) && entry.metadata_result === 'recorded' &&
      mediaNetwork.expected_content_types.includes(entry.response_content_type));
    mediaNetwork.accepted_response_count = accepted.length;
    mediaNetwork.outcome = accepted.length ? 'owned_audio_http_response_observed' : 'owned_audio_http_response_not_observed';
  }

  async function dismissObservedPlaybackError() {
    const cleanup = result.playback_error_dialog_cleanup = { attempted: false, result: 'not_observed',
      playback_outcome_unchanged: true, counts_as_playback_success: false };
    try {
      const button = page.getByRole('button', { name: 'Got It', exact: true }).filter({ visible: true });
      const count = await button.count();
      if (count !== 1) { cleanup.result = count ? 'button_ambiguous_not_clicked' : 'button_missing_not_clicked'; return; }
      const dialog = button.locator('xpath=ancestor::*[contains(concat(" ", normalize-space(@class), " "), " dialogContainer ") or contains(concat(" ", normalize-space(@class), " "), " dialog ")][1]').filter({ visible: true });
      if (await dialog.count() !== 1) { cleanup.result = 'observed_dialog_context_missing_not_clicked'; return; }
      cleanup.dialog_first_line = scrubAudioError(await dialog.innerText());
      cleanup.attempted = true;
      await button.click({ timeout: 8000 });
      await dialog.waitFor({ state: 'hidden', timeout: 3000 });
      cleanup.result = 'observed_playback_error_dialog_dismissed';
      await snapshot('audio-error-dialog-dismissed').catch(() => {});
    } catch { cleanup.result = 'dialog_dismissal_not_completed'; }
  }

  async function readMedia() {
    return page.evaluate(({ selected, expectedId, expectedOrigin }) => [...document.querySelectorAll('audio,video')].map((element, index) => {
      const rect = element.getBoundingClientRect();
      let sourceIdentity;
      if (expectedId !== null && element.tagName === 'AUDIO') {
        sourceIdentity = { recognized_route: false, route_kind: 'unavailable', target_origin_matches: false, matches_selected_item: false };
        try {
          // Parse only inside the page and return booleans plus a fixed route kind.
          const source = new URL(element.currentSrc, document.baseURI);
          const pathname = source.pathname;
          const match = /^\/(?:emby\/)?Audio\/([a-f0-9]{32})\/(universal(?:\.[a-z0-9_-]{1,32})?|stream(?:\.(?:mp3|flac|aac|m4a|ogg|opus|wav))?|original\.(?:mp3|flac|aac|m4a|ogg|opus|wav)|(?:master|main)\.m3u8)\/?$/i.exec(pathname);
          sourceIdentity.target_origin_matches = source.origin === expectedOrigin;
          if (match) {
            const route = match[2].toLowerCase();
            sourceIdentity.recognized_route = true;
            sourceIdentity.route_kind = route.startsWith('universal') ? 'universal' : route.startsWith('stream') ? 'stream'
              : route.startsWith('original.') ? 'original' : route === 'master.m3u8' ? 'hls_master' : 'hls_main';
            sourceIdentity.matches_selected_item = sourceIdentity.target_origin_matches && match[1] === expectedId;
          }
        } catch { /* Missing or unsupported media source paths cannot prove item identity. */ }
      }
      return {
        index,
        selected: selected !== null && element === selected,
        tag: element.tagName.toLowerCase(),
        current_time: Number.isFinite(element.currentTime) ? element.currentTime : null,
        duration: Number.isFinite(element.duration) ? element.duration : null,
        paused: element.paused,
        ended: element.ended,
        seeking: element.seeking,
        ready_state: element.readyState,
        network_state: element.networkState,
        visible: rect.width > 0 && rect.height > 0 && getComputedStyle(element).visibility !== 'hidden',
        video_width: element.tagName === 'VIDEO' ? element.videoWidth : null,
        video_height: element.tagName === 'VIDEO' ? element.videoHeight : null,
        source_identity: sourceIdentity,
      };
    }), { selected: selectedAudio, expectedId: selectedIdentity?.id ?? null, expectedOrigin });
  }

  async function metrics(label) {
    const media = await readMedia();
    result.steps.push({ label, observed_at: new Date().toISOString(), media });
    return media;
  }

  async function describeControls(locator) {
    return locator.evaluateAll(elements => elements.map((element, index) => {
      const trim = value => String(value ?? '').trim()
        .replace(/(?:https?|wss?|file|blob|data):[^\s"'<>]+/gi, '[redacted URL]')
        .replace(/\b[0-9a-f]{16,}\b/gi, '[redacted ID]')
        .replace(/\b[A-Za-z0-9_-]{40,}\b/g, '[redacted opaque value]')
        .slice(0, 180);
      const ancestors = [];
      let playerText = null;
      for (let parent = element.parentElement, depth = 0; parent && depth < 6; parent = parent.parentElement, depth += 1) {
        const className = typeof parent.className === 'string' ? parent.className : '';
        if (depth < 3) ancestors.push({ tag: parent.tagName.toLowerCase(), class: trim(className), role: parent.getAttribute('role') });
        if (playerText === null && /player|nowplaying|now-playing|appfooter|osd/i.test(className)) {
          playerText = trim(parent.innerText);
        }
      }
      return {
        index,
        tag: element.tagName.toLowerCase(),
        role: element.getAttribute('role'),
        type: element.getAttribute('type'),
        data_action: element.getAttribute('data-action'),
        label: trim(element.getAttribute('aria-label')),
        title: trim(element.getAttribute('title')),
        class: trim(typeof element.className === 'string' ? element.className : ''),
        text: trim(element.innerText),
        icon_text: trim([...element.querySelectorAll('[class*="icon"],[class*="Icon"]')].map(icon => icon.innerText).join(' ')),
        disabled: Boolean(element.disabled) || element.getAttribute('aria-disabled') === 'true',
        minimum: element.getAttribute('min') ?? element.getAttribute('aria-valuemin'),
        maximum: element.getAttribute('max') ?? element.getAttribute('aria-valuemax'),
        step: element.getAttribute('step'),
        player_context: playerText !== null,
        player_text: playerText,
        ancestors,
      };
    }));
  }

  async function inspect(label) {
    const controls = await describeControls(page.locator(`${BUTTONS},input:visible,select:visible,[role="slider"]:visible`));
    const entry = { label: `${label}-controls`, phase: result.phase, controls: controls.slice(0, 240),
      omitted_control_count: Math.max(0, controls.length - 240) };
    // Retain DOM evidence before the screenshot, which can fail independently.
    result.steps.push(entry);
    try {
      await snapshot(label);
      entry.screenshot = 'captured_by_caller';
    } catch {
      entry.screenshot = 'capture_failed';
      result.screenshot_failure_count = (result.screenshot_failure_count ?? 0) + 1;
      if (result.outcome === 'in_progress') blocked('snapshot_capture_failed');
    }
  }

  function matchesControl(control, kind) {
    if (control.disabled) return false;
    const labels = [control.label, control.title, control.text].filter(Boolean);
    const names = {
      play: /^play(?:\s+now|\s+track|\s+song|\s+from beginning|\s*\([^)]*\))?$/i,
      pause: /^pause(?:\s+playback|\s*\([^)]*\))?$/i,
      stop: /^stop(?:\s+playback|\s*\([^)]*\))?$/i,
      close: /^close(?:\s+player|\s+playback)?$/i,
    };
    const icons = {
      play: /^(?:play_arrow|play_circle|play_circle_filled|play_circle_outline|\ue037)$/i,
      pause: /^(?:pause|pause_circle|pause_circle_filled|pause_circle_outline|\ue034)$/i,
      stop: /^(?:stop|stop_circle|\ue047)$/i,
      close: /^(?:close|cancel)$/i,
    };
    const classes = {
      play: /(?:^|\s)(?:btnPlay|playButton)(?:\s|$)/i,
      pause: /(?:^|\s)(?:btnPause|pauseButton)(?:\s|$)/i,
      stop: /(?:^|\s)(?:btnStop|stopButton)(?:\s|$)/i,
      close: /(?:^|\s)(?:btnClose|closeButton)(?:\s|$)/i,
    };
    if (kind === 'close' && !control.player_context) return false;
    const namedKinds = Object.keys(names).filter(action => labels.some(label => names[action].test(label)));
    // The observed mini-player places a single Material icon glyph directly in the button.
    const iconSources = [control.icon_text, control.text].filter(Boolean);
    const iconKinds = Object.keys(icons).filter(action => iconSources.some(icon => icons[action].test(icon)));
    const observedKinds = [...new Set([...namedKinds, ...iconKinds])];
    if (observedKinds.length) return observedKinds.length === 1 && observedKinds[0] === kind;
    if (labels.length) return false;
    return classes[kind].test(control.class);
  }

  async function findControl(kind, { scope = page, optional = false, playerOnly = false } = {}) {
    const controls = scope.locator(BUTTONS);
    const descriptions = await describeControls(controls);
    const candidates = descriptions.filter(control => matchesControl(control, kind) && (!playerOnly || control.player_context));
    if (!candidates.length && optional) return null;
    if (candidates.length !== 1) blocked(`${kind}_control_${candidates.length ? 'ambiguous' : 'missing'}`);
    const selected = candidates[0];
    result.steps.push({ label: `${result.phase}-selected-control`, action: kind, control: selected });
    return controls.nth(selected.index);
  }

  async function clickControl(kind, options) {
    const control = await findControl(kind, options);
    if (control) await control.click({ timeout: 8000 });
    return Boolean(control);
  }

  function targetAudio(media, { playing = false } = {}) {
    const candidates = media.filter(element => element.tag === 'audio' && element.ready_state >= 2 &&
      element.duration !== null && Math.abs(element.duration - EXPECTED_DURATION) <= 3 && (!playing || !element.paused) &&
      (!selectedAudio || element.selected) && (!selectedIdentity || element.source_identity?.matches_selected_item));
    if (candidates.length > 1) blocked('target_audio_element_ambiguous');
    return candidates[0] ?? null;
  }

  async function waitForAudio(label, predicate, timeout = 15000) {
    const deadline = Date.now() + timeout;
    do {
      const media = await readMedia();
      const audio = targetAudio(media);
      if (audio && predicate(audio)) {
        result.steps.push({ label, observed_at: new Date().toISOString(), media });
        return audio;
      }
      await page.waitForTimeout(250);
    } while (Date.now() < deadline);
    await metrics(`${label}-timeout`);
    blocked('audio_state_not_observed_before_timeout');
  }

  async function seek(fraction, direction) {
    await inspect(`audio-${result.phase}-before`);
    const before = targetAudio(await metrics(`audio-${result.phase}-media-before`));
    if (!before) blocked('target_audio_element_missing');
    let ranges = page.locator('input[type="range"]:visible');
    let descriptions = await describeControls(ranges);
    const seekCandidates = controls => controls.filter(control => {
      const own = [control.label, control.title, control.class].join(' ');
      const related = `${own} ${control.ancestors.map(parent => parent.class).join(' ')}`;
      return !control.disabled && !/volume|brightness|rate/i.test(own) && /seek|progress|position|timeline|playback.?time/i.test(related);
    });
    let candidates = seekCandidates(descriptions);
    if (!candidates.length) {
      ranges = page.locator('[role="slider"]:visible');
      descriptions = await describeControls(ranges);
      candidates = seekCandidates(descriptions);
    }
    if (candidates.length !== 1) blocked(`seek_control_${candidates.length ? 'ambiguous' : 'missing'}`);
    const selected = candidates[0];
    const slider = ranges.nth(selected.index);
    const box = await slider.boundingBox();
    if (!box || box.width < 60 || box.height <= 0) blocked('seek_control_geometry_unusable');
    result.steps.push({ label: `audio-${result.phase}-selected-control`, action: 'pointer_seek', fraction, control: selected });
    // Pointer input uses the observed slider geometry; no media or input value is assigned.
    await slider.click({ position: { x: box.width * fraction, y: box.height / 2 }, timeout: 8000 });
    const after = await waitForAudio(`audio-${result.phase}-media-after`, audio => {
      const moved = direction === 'forward' ? audio.current_time > before.current_time + 10 : audio.current_time < before.current_time - 10;
      return !audio.seeking && moved && Math.abs(audio.current_time - audio.duration * fraction) <= Math.max(5, audio.duration * 0.035);
    });
    await inspect(`audio-${result.phase}-after`);
    return after;
  }

  function cardTitleControls(name, scope = page) {
    const titles = scope.getByText(name, { exact: true }).filter({ visible: true });
    return titles.locator('xpath=ancestor-or-self::*[(self::button or self::a or @role="button") and @data-action="link" and ancestor::*[contains(concat(" ", normalize-space(@class), " "), " card ") or contains(concat(" ", normalize-space(@class), " "), " cardBox ")]][1]').filter({ visible: true });
  }

  async function cardIdentity(control, expectedId) {
    const observed = await control.evaluate(element => {
      const card = element.closest('.card');
      const id = card?.getAttribute('data-id') ?? null, type = card?.getAttribute('data-type') ?? null;
      return { card_present: Boolean(card), id_present: id !== null,
        id: id !== null && /^[a-f0-9]{32}$/i.test(id) ? id : null,
        id_format_supported: id === null || /^[a-f0-9]{32}$/i.test(id),
        type: ['MusicAlbum', 'Audio', 'CollectionFolder', 'Folder'].includes(type) ? type : type === null ? null : 'other' };
    });
    observed.matches_receipted_id = observed.id === null ? null : observed.id === expectedId;
    if (!observed.card_present || observed.id_present && (!observed.id_format_supported || !observed.matches_receipted_id)) {
      blocked('observed_card_identity_differs_from_receipt');
    }
    return observed;
  }

  async function latestMusicSection(timeout) {
    const expected = `Latest ${homeLibrary.name}`;
    const headings = page.locator('a.sectionTitleTextButton').filter({ hasText: expected }).filter({ visible: true });
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) {
      const count = await headings.count();
      if (count > 1) blocked('latest_music_section_heading_ambiguous');
      if (count === 1) {
        const title = (await headings.innerText()).replace(/\ue5e1/g, '').trim();
        if (title !== expected) blocked('latest_music_section_title_differs');
        const section = headings.locator('xpath=ancestor::*[contains(concat(" ", normalize-space(@class), " "), " verticalSection ")][1]').filter({ visible: true });
        if (await section.count() !== 1) blocked('latest_music_section_context_missing');
        result.steps.push({ label: 'audio-latest-music-section', heading: title, selection: 'observed_latest_music_section' });
        return section;
      }
      await page.waitForTimeout(250);
    }
    blocked('latest_music_section_missing');
  }

  async function observeHome(label, previousURL = null, timeout = 15000) {
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) {
      const controls = cardTitleControls(homeLibrary.name);
      const count = await controls.count();
      const headingAbsent = await page.getByRole('heading', { name: selectedAlbum, exact: true }).filter({ visible: true }).count() === 0;
      const navigationObserved = previousURL === null || page.url() !== previousURL;
      if (count === 1 && headingAbsent && navigationObserved) {
        const identity = await cardIdentity(controls, homeLibrary.id);
        const current = new URL(page.url());
        if (current.origin !== expectedOrigin) blocked('home_navigation_left_target_origin');
        const hashRoute = current.hash.split('?')[0];
        result.steps.push({ label, library_name: homeLibrary.name, library_card: identity,
          library_card_count: count, album_detail_heading_absent: true, navigation_observed: navigationObserved,
          location: { pathname: current.pathname === '/web/index.html' ? current.pathname : '{unsupported path}',
            hash_route: ['', '#!/home', '#/home', '#!/home.html', '#/home.html'].includes(hashRoute) ? hashRoute : '{unsupported route}' },
          visible_album_title_link_count: await cardTitleControls(selectedAlbum).count() });
        return;
      }
      await page.waitForTimeout(250);
    }
    blocked('home_navigation_and_music_library_card_not_observed');
  }

  async function albumCardControl(label, timeout = 20000) {
    const scope = suppliedHome ? await latestMusicSection(timeout) : page;
    const titles = scope.getByText(selectedAlbum, { exact: true }).filter({ visible: true });
    const controls = titles.locator('xpath=ancestor-or-self::*[(self::button or self::a or @role="button") and @data-action="link" and ancestor::*[contains(concat(" ", normalize-space(@class), " "), " card ") or contains(concat(" ", normalize-space(@class), " "), " cardBox ")]][1]').filter({ visible: true });
    const deadline = Date.now() + timeout;
    do {
      const count = await controls.count();
      if (count > 1) blocked('album_card_title_control_ambiguous');
      if (count === 1) {
        const identity = suppliedHome ? await cardIdentity(controls, albumId) : null;
        if (identity?.type === 'Audio') blocked('audio_history_card_is_not_album_selection');
        result.steps.push({ label, selection: suppliedHome ? 'exact_album_title_in_observed_latest_music_section'
          : 'exact_album_title_link_action_in_card', card_identity: identity, controls: await describeControls(controls) });
        return controls;
      }
      await page.waitForTimeout(250);
    } while (Date.now() < deadline);
    blocked('album_card_title_control_missing');
  }

  async function identifiedTrackRow() {
    const titles = page.getByText(displayTitle, { exact: true }).filter({ visible: true });
    const deadline = Date.now() + 15000;
    while (!await titles.count() && Date.now() < deadline) await page.waitForTimeout(250);
    const titleEvidence = await titles.evaluateAll(elements => elements.map((element, index) => {
      const ancestors = [];
      for (let parent = element.parentElement, depth = 0; parent && depth < 10; parent = parent.parentElement, depth += 1) {
        if (!parent.matches('.listItem,.card,.cardBox,[role="row"],[role="listitem"]')) continue;
        const identifier = parent.getAttribute('data-id');
        ancestors.push({ tag: parent.tagName.toLowerCase(), role: parent.getAttribute('role'),
          class: typeof parent.className === 'string' ? parent.className.slice(0, 180) : '',
          data_id: identifier === null ? null : /^[a-f0-9]{32}$/i.test(identifier) ? identifier : '[unsupported data-id]' });
      }
      return { index, actual_display_title: (element.innerText ?? '').trim().slice(0, 180), row_ancestors: ancestors };
    }));
    result.steps.push({ label: 'audio-same-name-row-identities', phase: result.phase,
      expected_item_id: selectedIdentity.id, actual_display_title: displayTitle, titles: titleEvidence });
    if (!titleEvidence.length) blocked('track_title_missing');
    if (!titleEvidence.some(title => title.row_ancestors.some(row => row.data_id === selectedIdentity.id))) {
      blocked('selected_item_row_id_not_observed');
    }
    const rows = titles.locator('xpath=ancestor::*[(@role="row" or @role="listitem" or contains(concat(" ", normalize-space(@class), " "), " listItem ") or contains(concat(" ", normalize-space(@class), " "), " card ") or contains(concat(" ", normalize-space(@class), " "), " cardBox ")) and @data-id][1]').filter({ visible: true });
    const matching = rows.locator(`xpath=self::*[@data-id="${selectedIdentity.id}"]`);
    const count = await matching.count();
    if (count !== 1) blocked(`selected_item_row_${count ? 'ambiguous' : 'missing'}`);
    const title = matching.getByText(displayTitle, { exact: true }).filter({ visible: true });
    if (await title.count() !== 1) blocked('selected_item_row_title_missing_or_ambiguous');
    const observedItemId = await matching.getAttribute('data-id');
    if (observedItemId !== selectedIdentity.id) blocked('selected_item_row_identity_changed');
    result.steps.push({ label: 'audio-selected-item-row', phase: result.phase, observed_item_id: observedItemId,
      actual_display_title: displayTitle, controls: await describeControls(matching) });
    return { row: matching, title };
  }

  async function returnHome() {
    result.phase = 'return_home';
    let home = page.getByRole('link', { name: 'Home', exact: true }).filter({ visible: true });
    if (!await home.count()) home = page.getByRole('button', { name: 'Home', exact: true }).filter({ visible: true });
    if (await home.count() !== 1) blocked('home_control_missing_or_ambiguous');
    const previousURL = page.url();
    result.steps.push({ label: 'audio-return-home-action', action: 'click_exact_home_control', controls: await describeControls(home) });
    await home.click({ timeout: 8000 });
    if (suppliedHome) {
      await observeHome('audio-return-home-library-evidence', previousURL);
      result.return_home = 'home_control_clicked_navigation_observed_and_receipted_music_library_card_visible';
    } else {
      await albumCardControl('audio-return-home-album-card', 15000);
      result.return_home = 'home_control_clicked_and_album_visible';
    }
    await inspect('audio-returned-home');
  }

  try {
    if (!selectedAlbum) blocked('unsupported_album_title');
    if (!selectedTrack) blocked('unsupported_track_title');
    if (!validIdentity) blocked('unsupported_item_identity');
    if (!validHome) blocked('unsupported_home_library_identity');
    await inspect('audio-home-before');
    if (suppliedHome) await observeHome('audio-initial-home-library-evidence', null, 20000);
    const baseline = await metrics('audio-home-media-before');
    if (baseline.some(element => !element.paused && element.ready_state >= 2)) blocked('preexisting_playback_is_active');
    const albumControl = await albumCardControl('audio-album-selected-card');
    result.steps.push({ label: 'audio-album-action', action: 'click_exact_album_card_title', controls: await describeControls(albumControl) });
    await albumControl.click({ timeout: 8000 });

    result.phase = 'album_tracks';
    let trackTitle, row;
    if (selectedIdentity) {
      await page.getByText(displayTitle, { exact: true }).filter({ visible: true }).first()
        .waitFor({ state: 'visible', timeout: 15000 });
      ({ title: trackTitle, row } = await identifiedTrackRow());
      await inspect('audio-album-tracks');
    } else {
      trackTitle = page.getByText(selectedTrack, { exact: true }).filter({ visible: true });
      await trackTitle.waitFor({ state: 'visible', timeout: 15000 });
      await inspect('audio-album-tracks');
      if (await trackTitle.count() !== 1) blocked('track_title_ambiguous');
      result.available_fixture_tracks = await page.getByText(otherTrack, { exact: true }).filter({ visible: true }).count() > 0
        ? [selectedTrack, otherTrack] : [selectedTrack];
      row = trackTitle.locator('xpath=ancestor::*[@role="row" or contains(concat(" ", normalize-space(@class), " "), " listItem ")][1]');
    }
    result.actual_display_title = displayTitle;
    result.display_title_matches_reference_tag = displayTitle === selectedTrack;

    result.phase = 'track_open';
    if (mediaNetwork) {
      if (typeof mediaEvents?.on !== 'function' || typeof mediaEvents?.off !== 'function') blocked('audio_response_events_unavailable');
      mediaEvents.on('response', audioResponseListener);
      mediaListening = true;
    }
    const rowPlay = await row.count() === 1 ? await findControl('play', { scope: row, optional: true }) : null;
    if (rowPlay) await rowPlay.click({ timeout: 8000 });
    else {
      // Clicking the exact visible title also supports the client's delegated list-row action.
      result.steps.push({ label: 'audio-track-title-action', action: 'click_exact_track_title', controls: await describeControls(trackTitle) });
      await trackTitle.click({ timeout: 8000 });
    }
    result.track_selection = selectedIdentity ? 'exact_observed_item_id_row_and_its_title_or_play_control'
      : 'exact_visible_track_title_or_its_row_play_control';
    await page.waitForTimeout(1000);
    await inspect('audio-track-opened');

    result.phase = 'track_play';
    if (!targetAudio(await readMedia(), { playing: true })) {
      const trackHeading = page.getByRole('heading', { name: displayTitle, exact: true }).filter({ visible: true });
      let activeAudio = false;
      let detailVisible = false;
      const deadline = Date.now() + 15000;
      do {
        activeAudio = (await readMedia()).some(element => element.tag === 'audio' && !element.paused);
        detailVisible = await trackHeading.count() === 1;
        if (activeAudio || detailVisible) break;
        await page.waitForTimeout(250);
      } while (Date.now() < deadline);
      // A direct row action may still be buffering; an explicit track detail heading permits its Play control.
      if (!activeAudio) {
        if (selectedIdentity) blocked('identified_row_did_not_start_audio');
        if (!detailVisible) blocked('track_detail_play_context_not_observed');
        const detail = trackHeading.locator('xpath=ancestor::*[self::main or @role="main" or contains(concat(" ", normalize-space(@class), " "), " itemDetailPage ") or contains(concat(" ", normalize-space(@class), " "), " detailPageContent ") or contains(concat(" ", normalize-space(@class), " "), " page ")][1]');
        if (await detail.count() !== 1) blocked('track_detail_play_scope_not_observed');
        await clickControl('play', { scope: detail });
      }
    }
    const started = await waitForAudio('audio-playing-start', audio => !audio.paused && audio.current_time > 0, 30000);
    result.phase = 'track_identity';
    const playerTitles = [];
    for (const title of selectedIdentity ? [displayTitle] : [selectedTrack, otherTrack]) {
      const contexts = await page.getByText(title, { exact: true }).filter({ visible: true }).evaluateAll(elements => elements.map(element => {
        const classes = [];
        for (let parent = element.parentElement, depth = 0; parent && depth < 6; parent = parent.parentElement, depth += 1) {
          const name = typeof parent.className === 'string' ? parent.className : '';
          if (/nowplayingbar|nowplayingitem|nowplayingtitle|nowplayingname|player.?title|player.?name|player.?info|player.?metadata|audio.?player|mini.?player|appfooter/i.test(name)) {
            classes.push(name.slice(0, 180));
          }
        }
        return classes;
      }));
      playerTitles.push({ title, player_contexts: contexts.filter(classes => classes.length) });
    }
    result.steps.push({ label: 'audio-player-track-identity', titles: playerTitles });
    if (!playerTitles[0].player_contexts.length || playerTitles[1]?.player_contexts.length) blocked('player_track_identity_not_observed');
    selectedAudio = await page.locator('audio,video').nth(started.index).elementHandle();
    if (!selectedAudio) blocked('selected_audio_element_not_retained');
    const retainedStart = targetAudio(await metrics('audio-retained-playing-start'), { playing: true });
    if (!retainedStart) blocked('selected_audio_element_not_retained');
    result.track_identity = selectedIdentity ? 'observed_row_data_id_and_current_source_path_with_retained_audio_element'
      : 'exact_selected_track_title_in_visible_player_context_and_retained_audio_element';
    if (selectedIdentity) {
      result.source_identity_evidence = retainedStart.source_identity;
      result.source_container = selectedIdentity.container;
      result.container_identity_evidence = 'Caller-owned catalog path mapping bound to the observed row data-id and actual media source pathname';
      result.playback_output_codec_verified = false;
    }
    result.phase = 'playback_advance';
    const advanced = await waitForAudio('audio-playing-advanced', audio => !audio.paused && audio.index === retainedStart.index &&
      audio.current_time > retainedStart.current_time + 2, 10000);
    result.playback_evidence = { media_tag: 'audio', current_time_delta_seconds: advanced.current_time - retainedStart.current_time,
      ready_state: advanced.ready_state, duration_seconds: advanced.duration, audible_output_verified: false };
    await inspect('audio-playing');

    result.phase = 'pause';
    await metrics('audio-pause-before');
    await clickControl('pause');
    const paused = await waitForAudio('audio-paused', audio => audio.paused);
    await page.waitForTimeout(1000);
    const held = targetAudio(await metrics('audio-paused-held'));
    if (!held?.paused || Math.abs(held.current_time - paused.current_time) > 0.35) blocked('ui_pause_did_not_hold_position');
    await inspect('audio-paused');

    result.phase = 'seek_forward';
    await seek(0.65, 'forward');
    result.phase = 'seek_backward';
    const afterSeek = await seek(0.30, 'backward');

    result.phase = 'resume';
    await metrics('audio-resume-before');
    if (afterSeek.paused) await clickControl('play', { playerOnly: true });
    const resumed = await waitForAudio('audio-resumed', audio => !audio.paused);
    await waitForAudio('audio-resumed-advanced', audio => !audio.paused && audio.current_time > resumed.current_time + 2, 10000);
    await inspect('audio-resumed');

    result.phase = 'stop';
    await metrics('audio-stop-before');
    await inspect('audio-stop-before');
    const previousRequests = Array.isArray(report.requests) ? report.requests.length : 0;
    if (!await clickControl('stop', { optional: true })) await clickControl('close', { playerOnly: true });
    let stoppedMedia = [];
    let stopped = false;
    const deadline = Date.now() + 10000;
    do {
      stoppedMedia = await readMedia();
      stopped = stoppedMedia.every(element => element.paused);
      if (stopped) break;
      await page.waitForTimeout(250);
    } while (Date.now() < deadline);
    await page.waitForTimeout(1000);
    stoppedMedia = await metrics('audio-stop-after');
    if (!stopped || stoppedMedia.some(element => !element.paused)) blocked('ui_stop_did_not_stop_media');
    if (!Array.isArray(report.requests)) blocked('stopped_report_observation_unavailable');
    let stopReports = [];
    const reportDeadline = Date.now() + 10000;
    do {
      stopReports = report.requests.slice(previousRequests).filter(entry =>
        entry.method === 'POST' && entry.origin === 'target' && /\/Sessions\/Playing\/Stopped\/?$/i.test(entry.route));
      if (stopReports.some(entry => entry.status >= 400)) blocked('stopped_report_rejected');
      if (stopReports.some(entry => entry.status >= 200 && entry.status < 300)) break;
      await page.waitForTimeout(250);
    } while (Date.now() < reportDeadline);
    if (!stopReports.some(entry => entry.status >= 200 && entry.status < 300)) blocked('successful_stopped_report_not_observed');
    result.stop_evidence = { all_media_paused_or_removed: true, remaining_audio_elements: stoppedMedia.filter(element => element.tag === 'audio').length,
      observed_stopped_report_statuses: stopReports.map(entry => entry.status) };
    await inspect('audio-stopped');
    await returnHome();
    if (mediaNetwork) {
      result.phase = 'media_response_identity';
      await finishAudioResponses();
      if (mediaNetwork.response_overflow) blocked('audio_response_observation_limit_reached');
      if (mediaNetwork.observer_errors || !mediaNetwork.pending_metadata_drained) blocked('audio_response_metadata_incomplete');
      if (!mediaNetwork.accepted_response_count) blocked('owned_audio_http_content_type_not_observed');
    }
    result.phase = 'complete';
    result.outcome = 'audio_ui_flow_completed';
    return result;
  } catch {
    result.outcome = 'blocked_at_observed_ui_step';
    result.failure_reason ??= 'ui_action_or_observation_failed';
    await metrics(`audio-blocked-${result.phase}-media`).catch(() => {});
    await inspect(`audio-blocked-${result.phase}`).catch(() => {});
    if (selectedIdentity) await dismissObservedPlaybackError();
    const error = new Error(`audio_ui_${result.phase}_failed`);
    error.code = 'AUDIO_UI_FLOW_BLOCKED';
    error.stage = result.phase;
    throw error;
  } finally {
    await finishAudioResponses();
    if (selectedAudio) await selectedAudio.dispose().catch(() => {});
  }
}
