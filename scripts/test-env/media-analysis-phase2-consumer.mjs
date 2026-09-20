/**
 * Explicit Goby protocol adapter around real Video.js playback. The BIF module
 * imported below is the unchanged, pinned third-party plugin. This adapter is
 * not represented as Emby Web, an Emby SDK, or an arbitrary client-parity test.
 */
import '/__media-analysis-phase2/plugin/videojs-bif.js';

const requireThat = (condition, code) => { if (!condition) throw new Error(code); };
const origin = location.origin;
const state = document.getElementById('state');
const skip = document.getElementById('skip-intro');
const chainImage = document.getElementById('thumbnail-chain');
const player = window.videojs('phase2-player', { controls: true, autoplay: false, muted: true, preload: 'metadata', inactivityTimeout: 0 });
let token = '', viewerId = '', deviceId = '', selected, playSessionId = '', intro, thumbnailSet;
const receipts = [];
const frames = [];
let disposed = false;
let pointer;
let frameSequence = 0;

async function digest(bytes) { return Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes)), (byte) => byte.toString(16).padStart(2, '0')).join(''); }
function sanitized(value) {
  if (typeof value === 'string') {
    let clean = token ? value.split(token).join('[REDACTED]').split(encodeURIComponent(token)).join('[REDACTED]') : value;
    if (clean.includes('?')) { try { const url = new URL(clean, origin); for (const key of [...url.searchParams.keys()]) if (['api_key', 'x-emby-token', 'token'].includes(key.toLowerCase())) url.searchParams.set(key, '[REDACTED]'); clean = url.origin === origin ? url.pathname + url.search : url.href; } catch { /* Non-URL text stays text after exact credential redaction. */ } }
    return clean;
  }
  if (Array.isArray(value)) return value.map(sanitized);
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).filter(([key]) => !['AccessToken', 'Path', 'RequiredHttpHeaders'].includes(key)).map(([key, entry]) => [key, sanitized(entry)]));
  return value;
}
async function request(path, method = 'GET', body) {
  const url = new URL(path, origin); requireThat(url.origin === origin, 'consumer_origin_mismatch');
  const headers = { 'X-Emby-Client': 'Goby phase 2 protocol adapter', 'X-Emby-Client-Version': '1', 'X-Emby-Device-Id': deviceId, 'X-Emby-Device-Name': 'Video.js browser fixture' };
  if (token) headers['X-Emby-Token'] = token;
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  return fetch(url, { method, headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }), credentials: 'omit', redirect: 'error', cache: 'no-store', signal: AbortSignal.timeout(30000) });
}
function authenticatedURL(path, query = {}) {
  const url = new URL(path, origin); requireThat(url.origin === origin, 'consumer_media_origin_mismatch');
  for (const [key, value] of Object.entries(query)) url.searchParams.set(key, String(value));
  url.searchParams.set('api_key', token); return url.href;
}
function markerPair(chapters) {
  requireThat(Array.isArray(chapters), 'consumer_chapters_missing');
  const starts = chapters.filter((value) => value.MarkerType === 'IntroStart'); const ends = chapters.filter((value) => value.MarkerType === 'IntroEnd');
  if (starts.length === 0 && ends.length === 0) return null;
  requireThat(starts.length === 1 && ends.length === 1 && Number.isSafeInteger(starts[0].StartPositionTicks) && Number.isSafeInteger(ends[0].StartPositionTicks)
    && starts[0].StartPositionTicks >= 0 && ends[0].StartPositionTicks > starts[0].StartPositionTicks, 'consumer_marker_pair_invalid');
  return { StartTicks: starts[0].StartPositionTicks, EndTicks: ends[0].StartPositionTicks };
}
function video() { const element = document.querySelector('#phase2-player video'); requireThat(element instanceof HTMLVideoElement, 'consumer_actual_video_missing'); return element; }
function observeFrame(_now, metadata) {
  if (disposed) return;
  frames.push({ Sequence: ++frameSequence, MediaTime: metadata.mediaTime, PresentedFrames: metadata.presentedFrames, Width: metadata.width, Height: metadata.height });
  if (frames.length > 128) frames.shift();
  video().requestVideoFrameCallback(observeFrame);
}
async function waitFor(predicate, code, milliseconds = 30000) {
  const deadline = performance.now() + milliseconds;
  while (performance.now() < deadline) { if (predicate()) return; await new Promise((resolve) => setTimeout(resolve, 40)); }
  throw new Error(code);
}
function updateSkip() { if (disposed) return; const current = player.currentTime() * 10_000_000; skip.disabled = !intro || current < intro.StartTicks || current >= intro.EndTicks; }
player.on('timeupdate', updateSkip);
// Observe actual input without wrapping, replacing or invoking plugin methods.
document.addEventListener('mousemove', (event) => {
  if (disposed) return;
  const bar = document.querySelector('.vjs-progress-holder');
  if (!(bar instanceof HTMLElement) || !bar.contains(event.target)) return;
  const rect = bar.getBoundingClientRect(); const left = Math.round(rect.left + (window.pageXOffset || document.body.scrollLeft) - (document.documentElement.clientLeft || document.body.clientLeft || 0));
  pointer = { seconds: Math.max(0, Math.min(1, (event.pageX - left) / bar.offsetWidth)) * player.duration(), pageX: event.pageX, left, width: bar.offsetWidth };
}, true);
skip.addEventListener('click', () => {
  requireThat(intro && !skip.disabled, 'consumer_skip_outside_actual_marker');
  receipts.push({ Kind: 'actual_skip_click', BeforeSeconds: player.currentTime(), RequestedSeconds: intro.EndTicks / 10_000_000, FrameSequence: frameSequence });
  player.currentTime(intro.EndTicks / 10_000_000);
  state.textContent = 'The playback element is seeking to the server-provided intro end.';
});

async function raster(image) {
  await image.decode();
  requireThat(image.naturalWidth > 0 && image.naturalWidth <= 4096 && image.naturalHeight > 0 && image.naturalHeight <= 4096, 'consumer_image_dimensions_invalid');
  const canvas = document.createElement('canvas'); canvas.width = image.naturalWidth; canvas.height = image.naturalHeight;
  const context = canvas.getContext('2d', { willReadFrequently: true }); requireThat(context, 'consumer_canvas_unavailable'); context.drawImage(image, 0, 0);
  return { Width: canvas.width, Height: canvas.height, Pixels: context.getImageData(0, 0, canvas.width, canvas.height).data };
}
async function displayedBIF() {
  const image = document.querySelector('.bif-image'); const holder = document.querySelector('.bif-thumbnail');
  requireThat(image instanceof HTMLImageElement && holder instanceof HTMLElement && getComputedStyle(holder).display !== 'none', 'consumer_visible_bif_image_missing');
  const bounds = image.getBoundingClientRect(); requireThat(bounds.width > 0 && bounds.height > 0 && bounds.top >= 0 && bounds.left >= 0 && bounds.bottom <= innerHeight && bounds.right <= innerWidth, 'consumer_bif_image_not_fully_visible');
  requireThat(image.src.startsWith('data:image/jpeg;base64,'), 'consumer_plugin_image_not_jpeg');
  const bytes = Uint8Array.from(atob(image.src.slice('data:image/jpeg;base64,'.length)), (char) => char.charCodeAt(0));
  requireThat(bytes.byteLength > 0 && bytes.byteLength <= (2 << 20), 'consumer_plugin_jpeg_size_limit');
  const decoded = await raster(image); const points = new Map();
  const label = document.querySelector('.bif-time')?.textContent?.trim();
  requireThat(typeof label === 'string' && /^(?:\d+:)?\d{1,2}:\d{2}$/.test(label) && pointer && Number.isFinite(pointer.seconds), 'consumer_observed_hover_time_missing');
  const tooltip = label.split(':').reduce((seconds, part) => seconds * 60 + Number(part), 0);
  requireThat(tooltip === Math.floor(pointer.seconds), 'consumer_tooltip_pointer_disagree');
  for (const [x, y] of [[0, 0], [decoded.Width - 1, 0], [0, decoded.Height - 1], [decoded.Width - 1, decoded.Height - 1], [Math.floor(decoded.Width / 2), Math.floor(decoded.Height / 2)]]) {
    const offset = (y * decoded.Width + x) * 4; points.set(`${x},${y}`, { x, y, rgba: Array.from(decoded.Pixels.slice(offset, offset + 4)) });
  }
  const jpegSHA256 = await digest(bytes);
  return { jpeg_sha256: jpegSHA256, observed_jpeg_sha256: jpegSHA256, rgba_sha256: await digest(decoded.Pixels), width: decoded.Width, height: decoded.Height, pixels: [...points.values()], visible: true, hover_seconds: pointer.seconds, tooltip_seconds: tooltip };
}

window.phase2Acceptance = {
  async authenticate(input) {
    requireThat(window.videojs.VERSION === '7.6.5', 'consumer_videojs_version_mismatch'); deviceId = `phase2-${input.RunId.slice(0, 80)}`;
    const response = await request('/emby/Users/AuthenticateByName', 'POST', { Username: input.ViewerName, Pw: input.ViewerPassword });
    requireThat(response.status === 200, 'consumer_authentication_failed'); const value = await response.json();
    requireThat(value.User?.Id === input.ViewerId && typeof value.AccessToken === 'string' && value.AccessToken, 'consumer_authenticated_identity_mismatch');
    token = value.AccessToken; viewerId = value.User.Id;
    state.textContent = 'Authenticated with actual viewer authority.';
    return { Status: response.status, UserId: viewerId, DeviceId: deviceId, VideoJSVersion: window.videojs.VERSION };
  },
  async open(input) {
    requireThat(token && !selected, 'consumer_source_already_open'); selected = input;
    const detailResponse = await request(`/emby/Users/${encodeURIComponent(viewerId)}/Items/${encodeURIComponent(input.ItemId)}`);
    requireThat(detailResponse.status === 200, 'consumer_item_detail_failed'); const detail = await detailResponse.json();
    const response = await request(`/emby/Items/${encodeURIComponent(input.ItemId)}/PlaybackInfo`, 'POST', {
      UserId: viewerId, MediaSourceId: input.MediaSourceId, StartTimeTicks: 0, IsPlayback: true,
      EnableDirectPlay: true, EnableDirectStream: true, EnableTranscoding: false,
      DeviceProfile: { DirectPlayProfiles: [{ Type: 'Video', Container: 'mp4', VideoCodec: 'h264', AudioCodec: 'aac,mp3' }] },
    });
    requireThat(response.status === 200, 'consumer_playback_info_failed'); const info = await response.json(); const source = info.MediaSources?.[0];
    requireThat(!info.ErrorCode && info.MediaSources?.length === 1 && source.Id === input.MediaSourceId && source.SupportsDirectStream && typeof source.DirectStreamUrl === 'string' && info.PlaySessionId, 'consumer_browser_mp4_source_unavailable');
    intro = markerPair(source.Chapters); requireThat(JSON.stringify(intro) === JSON.stringify(markerPair(detail.Chapters)), 'consumer_item_playback_markers_differ');
    playSessionId = info.PlaySessionId;
    const url = new URL(source.DirectStreamUrl, origin); requireThat(url.origin === origin && url.searchParams.get('MediaSourceId') === input.MediaSourceId && url.searchParams.get('PlaySessionId') === playSessionId, 'consumer_source_scope_mismatch');
    player.src({ src: url.href, type: 'video/mp4' });
    await waitFor(() => Number.isFinite(player.duration()) && player.duration() > 0 && video().readyState >= 1, 'consumer_metadata_timeout');
    const started = await request('/emby/Sessions/Playing', 'POST', { ItemId: input.ItemId, MediaSourceId: input.MediaSourceId, PlaySessionId: playSessionId, PositionTicks: 0, PlayMethod: 'DirectStream', IsPaused: false });
    requireThat(started.status === 204, 'consumer_playback_start_report_failed');
    requireThat(typeof video().requestVideoFrameCallback === 'function', 'consumer_presented_frame_observer_unavailable'); video().requestVideoFrameCallback(observeFrame);
    await player.play(); await waitFor(() => frames.length >= 3 && video().videoWidth > 0 && !video().error, 'consumer_real_video_frames_missing');
    const bif = authenticatedURL(`/emby/Videos/${encodeURIComponent(input.ItemId)}/index.bif`, { Width: input.Width, MediaSourceId: input.MediaSourceId });
    player.bif({ src: bif });
    const thumbnails = await request(`/emby/Items/${encodeURIComponent(input.ItemId)}/ThumbnailSet?${new URLSearchParams({ Width: String(input.Width), MediaSourceId: input.MediaSourceId })}`);
    requireThat(thumbnails.status === 200, 'consumer_thumbnail_set_failed'); thumbnailSet = await thumbnails.json();
    requireThat(Number.isFinite(thumbnailSet.AspectRatio) && thumbnailSet.AspectRatio > 0 && Array.isArray(thumbnailSet.Thumbnails) && thumbnailSet.Thumbnails.length > 0, 'consumer_thumbnail_set_invalid');
    receipts.push({ Kind: 'source', Status: response.status, ItemStatus: detailResponse.status, ThumbnailSetStatus: thumbnails.status, PlaySessionId: playSessionId, MediaSourceId: source.Id, MarkerPair: intro, Source: sanitized(source), ThumbnailSet: thumbnailSet });
    state.textContent = 'Playing an actual negotiated MP4 source. The pinned BIF plugin owns seek-bar images.';
    return { DurationSeconds: player.duration(), MarkerPair: intro, ThumbnailSet: thumbnailSet, Receipt: receipts.at(-1) };
  },
  async positionForSkip() {
    requireThat(intro, 'consumer_detected_intro_missing'); const start = intro.StartTicks / 10_000_000; const end = intro.EndTicks / 10_000_000;
    const before = frameSequence; player.currentTime(start + Math.min(1, (end - start) / 4)); await player.play();
    await waitFor(() => !video().seeking && frames.filter((frame) => frame.Sequence > before && frame.MediaTime >= start && frame.MediaTime < end).length >= 3, 'consumer_intro_entry_not_presented'); updateSkip();
    requireThat(!skip.disabled, 'consumer_skip_control_not_available'); return { CurrentSeconds: player.currentTime(), Frames: frames.slice(-3), MarkerPair: intro };
  },
  async confirmSkip() {
    const clicked = receipts.filter((value) => value.Kind === 'actual_skip_click').at(-1); requireThat(clicked && intro, 'consumer_actual_skip_click_missing');
    await waitFor(() => !video().seeking && frames.filter((frame) => frame.Sequence > clicked.FrameSequence && frame.MediaTime >= intro.EndTicks / 10_000_000 && frame.MediaTime < intro.EndTicks / 10_000_000 + 3).length >= 3, 'consumer_post_skip_frames_missing');
    const position = Math.round(player.currentTime() * 10_000_000);
    const progress = await request('/emby/Sessions/Playing/Progress', 'POST', { ItemId: selected.ItemId, MediaSourceId: selected.MediaSourceId, PlaySessionId: playSessionId, PositionTicks: position, PlayMethod: 'DirectStream', IsPaused: false });
    requireThat(progress.status === 204, 'consumer_post_skip_progress_report_failed');
    return { Click: clicked, CurrentSeconds: player.currentTime(), Frames: frames.filter((frame) => frame.Sequence > clicked.FrameSequence && frame.MediaTime >= intro.EndTicks / 10_000_000).slice(-3), ErrorCode: video().error?.code ?? 0, ProgressStatus: progress.status };
  },
  bifFrame: displayedBIF,
  playbackState() { return { CurrentSeconds: player.currentTime(), DurationSeconds: player.duration(), Seeking: video().seeking, Paused: video().paused, ErrorCode: video().error?.code ?? 0, Frames: frames.slice(-5) }; },
  pause() { player.pause(); return { Paused: video().paused, CurrentSeconds: player.currentTime(), Frames: frames.slice(-3) }; },
  async httpChecks(expectedETag) {
    const url = authenticatedURL(`/emby/Videos/${encodeURIComponent(selected.ItemId)}/index.bif`, { Width: selected.Width, MediaSourceId: selected.MediaSourceId });
    const perform = (method, headers = {}) => fetch(url, { method, headers, credentials: 'omit', cache: 'no-store', signal: AbortSignal.timeout(30000) });
    const head = await perform('HEAD'); requireThat(head.status === 200 && head.headers.get('etag') === expectedETag && (await head.arrayBuffer()).byteLength === 0, 'consumer_bif_head_failed');
    const conditional = await perform('GET', { 'If-None-Match': expectedETag }); requireThat(conditional.status === 304, 'consumer_bif_conditional_failed');
    const partial = await perform('GET', { Range: 'bytes=0-63' }); const bytes = new Uint8Array(await partial.arrayBuffer()); requireThat(partial.status === 206 && bytes.length === 64, 'consumer_bif_range_failed');
    const invalid = await perform('GET', { Range: 'bytes=0-1,4-5' }); requireThat(invalid.status === 416, 'consumer_bif_multirange_not_rejected');
    const anonymous = new URL(url); anonymous.searchParams.delete('api_key'); const denied = await fetch(anonymous, { credentials: 'omit', cache: 'no-store' }); requireThat(denied.status === 401, 'consumer_anonymous_preview_not_denied');
    return { HEAD: head.status, Conditional: conditional.status, Range: partial.status, RangeHeaderSHA256: await digest(bytes), InvalidRange: invalid.status, Anonymous: denied.status };
  },
  async thumbnail(index, limits) {
    requireThat(thumbnailSet && index >= 0 && index < thumbnailSet.Thumbnails.length, 'consumer_thumbnail_index_invalid');
    const entry = thumbnailSet.Thumbnails[index]; const before = await raster(document.querySelector('.bif-image'));
    const target = authenticatedURL(`/emby/Items/${encodeURIComponent(selected.ItemId)}/Images/Thumbnail`, { PositionTicks: entry.PositionTicks, MediaSourceId: selected.MediaSourceId, tag: entry.ImageTag, maxWidth: selected.Width, quality: 90 });
    chainImage.src = target; const after = await raster(chainImage); requireThat(before.Width === after.Width && before.Height === after.Height, 'consumer_thumbnail_dimensions_differ');
    let sum = 0; const histogram = new Uint32Array(256); let channels = 0;
    for (let index = 0; index < before.Pixels.length; index++) if (index % 4 !== 3) { const error = Math.abs(before.Pixels[index] - after.Pixels[index]); histogram[error]++; sum += error; channels++; }
    let cumulative = 0, p95 = 0; for (; p95 < 256; p95++) { cumulative += histogram[p95]; if (cumulative >= Math.ceil(channels * .95)) break; }
    const mae = sum / channels; requireThat(mae <= limits.MaxRGBMAE && p95 <= limits.MaxRGBP95, 'consumer_thumbnail_pixels_do_not_match_bif');
    return { PositionTicks: entry.PositionTicks, ImageTag: entry.ImageTag, Width: after.Width, Height: after.Height, RGBMAE: mae, RGBP95: p95, RGBASHA256: await digest(after.Pixels), Complete: chainImage.complete };
  },
  async close() {
    if (disposed) return { AlreadyClosed: true };
    const position = Number.isFinite(player.currentTime()) ? Math.round(player.currentTime() * 10_000_000) : 0;
    player.pause(); disposed = true; player.dispose(); chainImage.removeAttribute('src');
    let stopped = null;
    if (playSessionId && selected) { const response = await request('/emby/Sessions/Playing/Stopped', 'POST', { ItemId: selected.ItemId, MediaSourceId: selected.MediaSourceId, PlaySessionId: playSessionId, PositionTicks: position, Failed: false }); stopped = response.status; requireThat([200, 204].includes(stopped), 'consumer_playback_stop_failed'); }
    let logout = null; if (token) { const response = await request('/emby/Sessions/Logout', 'POST', {}); logout = response.status; requireThat(logout === 204, 'consumer_logout_failed'); token = ''; }
    return { StopStatus: stopped, LogoutStatus: logout, PlayerDisposed: true };
  },
};
window.phase2AcceptanceReady = true;
