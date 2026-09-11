/** Observe one owned album detail response and its original-client UI without playback. */

import { createHash } from 'node:crypto';
import { TextDecoder } from 'node:util';

const ALBUMS = new Set(['Music', 'M3e Synthetic Album']);
const MUSIC_DIRECTORY = '/opt/goby-fixtures/client-m3e/Music';
const BODY_LIMIT = 256 * 1024;
const RESPONSE_LIMIT = 8;
const ARRAY_SAMPLE_LIMIT = 6;
const OBJECT_KEY_LIMIT = 80;
const SHAPE_NODE_LIMIT = 512;
const MUSIC_QUERY_LIMIT = 128;
const OWNED_ARTIST = 'M3e Synthetic Artist';
const RESOURCE_ID = /^(?:[0-9]{1,12}|[a-f0-9]{32})$/i;
const RESOURCE_QUERY_FIELDS = ['ParentId', 'ArtistIds', 'AlbumArtistIds', 'AlbumIds', 'ExcludeItemIds', 'ListItemIds'];
const AUDIT_FIELDS = ['Name', 'Type', 'IsFolder', 'MediaType', 'Id', 'Path', 'ParentId', 'ChildCount',
  'ArtistItems', 'AlbumArtists', 'Artists', 'Composers', 'Genres', 'GenreItems', 'TagItems', 'Studios', 'People', 'BackdropImageTags'];
const DTO_FIELDS = new Set((
  'Name OriginalTitle ServerId Id ParentId Path Type MediaType IsFolder ChildCount ArtistItems AlbumArtists Artists Composers ' +
  'Genres GenreItems TagItems Studios People BackdropImageTags ImageTags ImageBlurHashes UserData MediaSources MediaStreams ' +
  'Etag DateCreated DateLastMediaAdded DateLastRefreshed SortName ForcedSortName Overview OriginalPrimaryImageAspectRatio ' +
  'PrimaryImageAspectRatio ProductionYear PremiereDate EndDate RunTimeTicks Container Size LocationType PlayAccess ' +
  'Album AlbumId AlbumPrimaryImageTag AlbumArtist ArtistCount MusicVideoCount SongCount AlbumCount SeriesName SeriesId ' +
  'SeasonName SeasonId IndexNumber ParentIndexNumber IndexNumberEnd ProviderIds PresentationUniqueKey DisplayPreferencesId ' +
  'Status CollectionType IsPlaceHolder IsHD IsShortcut IsExternal IsRemote IsFavorite Played PlayCount PlaybackPositionTicks ' +
  'LastPlayedDate LastPlayedDateSort DateLastSaved UserRating Likes UnplayedItemCount PlayedPercentage ' +
  'RecursiveItemCount CumulativeRunTimeTicks OfficialRating CustomRating CommunityRating CriticRating ChannelId ChannelName ' +
  'Tags Taglines ProductionLocations ExternalUrls HomePage DisplayOrder LockData LockedFields ThemeSongIds ThemeVideoIds ' +
  'LocalTrailerCount SpecialFeatureCount ParentLogoItemId ParentLogoImageTag ParentThumbItemId ParentThumbImageTag ' +
  'ParentPrimaryImageItemId ParentPrimaryImageTag ParentBackdropItemId ParentBackdropImageTags PrimaryImageItemId ' +
  'PrimaryImageTag LogoImageTag ThumbImageTag ImageType ImageIndex Width Height AspectRatio ImageOrientation ' +
  'Bitrate SampleRate BitDepth Channels ChannelLayout Codec CodecTag Profile Level Language DisplayTitle Title ' +
  'IsDefault IsForced IsExternalText Format SupportsDirectPlay SupportsDirectStream SupportsTranscoding ' +
  'Protocol RequiredHttpHeaders DefaultAudioStreamIndex DefaultSubtitleStreamIndex OpenToken LiveStreamId ' +
  'AccessToken UserId Password Token ApiKey Items TotalRecordCount StartIndex'
).split(' '));
const QUERY_FIELDS = new Map(('Fields ExcludeFields IncludeItemTypes ParentId ArtistIds AlbumArtistIds AlbumIds ExcludeItemIds ListItemIds Recursive SortBy SortOrder ' +
  'EnableImages ImageTypeLimit EnableUserData InheritImages ' +
  'EnableImageTypes MinDateLastSaved MaxOfficialRating UserId api_key X-Emby-Token X-MediaBrowser-Token ' +
  'X-Emby-Client X-Emby-Device-Name X-Emby-Device-Id X-Emby-Client-Version').split(' ').map(name => [name.toLowerCase(), name]));
const TYPE_VALUES = new Set(['MusicAlbum', 'MusicArtist', 'MusicGenre', 'Audio', 'Folder', 'CollectionFolder', 'AggregateFolder',
  'UserView', 'Playlist', 'BoxSet', 'Movie', 'Series', 'Season', 'Episode', 'Video', 'Photo']);
const MEDIA_VALUES = new Set(['Audio', 'Video', 'Photo', 'Book', 'Game', '']);
const MUSIC_ITEM_TYPES = new Set(['MusicAlbum', 'MusicArtist', 'MusicGenre', 'MusicVideo', 'Audio', 'Folder',
  'CollectionFolder', 'UserView', 'Playlist', 'BoxSet', 'Movie', 'Series', 'Season', 'Episode', 'Video', 'Photo']);
const MUSIC_SORT_BY = new Set(['Name', 'SortName', 'AlbumArtist', 'Artist', 'Album', 'ParentIndexNumber', 'IndexNumber',
  'DateCreated', 'PremiereDate', 'ProductionYear', 'DatePlayed', 'PlayCount', 'Runtime', 'IsFolder', 'Random',
  'CommunityRating', 'OfficialRating'].map(value => value.toLowerCase()));
const MUSIC_SORT_ORDER = new Set(['ascending', 'descending']);

function valueType(value) {
  if (value === null) return 'null';
  if (Array.isArray(value)) return 'array';
  return typeof value;
}

function safeDTO(value, album, albumId) {
  let nodes = 0;
  const keyName = key => DTO_FIELDS.has(key) ? key : '{unrecognized field}';
  function shape(input, depth = 0) {
    const type = valueType(input);
    nodes += 1;
    if (type === 'array') {
      const result = { type, length: input.length };
      if (depth >= 3) return { ...result, truncated: input.length > 0, reason: 'depth_limit' };
      result.sample = [];
      for (const item of input.slice(0, ARRAY_SAMPLE_LIMIT)) {
        if (nodes >= SHAPE_NODE_LIMIT) break;
        result.sample.push(shape(item, depth + 1));
      }
      result.sample_count = result.sample.length;
      result.omitted_element_count = Math.max(0, input.length - result.sample.length);
      return result;
    }
    if (type === 'object') {
      const keys = Object.keys(input);
      const fields = [];
      for (const key of keys.slice(0, OBJECT_KEY_LIMIT)) {
        if (nodes >= SHAPE_NODE_LIMIT) break;
        const field = { name: keyName(key), type: valueType(input[key]) };
        if (depth < 3) field.shape = shape(input[key], depth + 1);
        else nodes += 1;
        fields.push(field);
      }
      return { type, key_count: keys.length, omitted_key_count: Math.max(0, keys.length - fields.length), fields,
        depth_limited: depth >= 3 };
    }
    return { type };
  }
  const object = value !== null && typeof value === 'object' && !Array.isArray(value) ? value : null;
  const keys = object ? Object.keys(object) : [];
  const fields = Object.fromEntries(AUDIT_FIELDS.map(key => [key,
    { present: Boolean(object && Object.hasOwn(object, key)), type: object && Object.hasOwn(object, key) ? valueType(object[key]) : 'missing' }]));
  const safeValues = {};
  if (fields.Name.present) {
    safeValues.Name = { matches_requested_album: object.Name === album };
    if (typeof object.Name === 'string' && ALBUMS.has(object.Name)) safeValues.Name.value = object.Name;
    else safeValues.Name.value_omitted = true;
  }
  for (const [key, allowed] of [['Type', TYPE_VALUES], ['MediaType', MEDIA_VALUES]]) {
    if (!fields[key].present) continue;
    safeValues[key] = typeof object[key] === 'string' && allowed.has(object[key])
      ? { value: object[key] } : { value_omitted: true };
  }
  if (typeof object?.IsFolder === 'boolean') safeValues.IsFolder = object.IsFolder;
  if (Number.isSafeInteger(object?.ChildCount) && object.ChildCount >= 0) safeValues.ChildCount = object.ChildCount;
  for (const key of ['ArtistItems', 'AlbumArtists']) {
    if (!Array.isArray(object?.[key])) continue;
    safeValues[key] = { length: object[key].length, omitted_item_count: Math.max(0, object[key].length - 32),
      items: object[key].slice(0, 32).map(item => {
        const matches = item !== null && typeof item === 'object' && !Array.isArray(item) && item.Name === OWNED_ARTIST;
        const entry = { type: valueType(item), name_matches_owned_artist: matches };
        if (matches) {
          entry.Name = item.Name;
          entry.id_state = typeof item.Id === 'string' && RESOURCE_ID.test(item.Id) ? 'valid' : Object.hasOwn(item, 'Id') ? 'invalid' : 'missing';
          if (entry.id_state === 'valid') entry.Id = item.Id;
        }
        return entry;
      }) };
  }
  if (Array.isArray(object?.Artists)) {
    safeValues.Artists = { length: object.Artists.length, omitted_item_count: Math.max(0, object.Artists.length - 32),
      items: object.Artists.slice(0, 32).map(item => ({ type: valueType(item), matches_owned_artist: item === OWNED_ARTIST,
        ...(item === OWNED_ARTIST ? { value: item } : {}) })) };
  } else if (typeof object?.Artists === 'string') {
    safeValues.Artists = { matches_owned_artist: object.Artists === OWNED_ARTIST,
      ...(object.Artists === OWNED_ARTIST ? { value: object.Artists } : {}) };
  }
  return {
    top_level_type: valueType(value), top_level_key_count: keys.length,
    top_level_keys: keys.slice(0, OBJECT_KEY_LIMIT).map(key => ({ name: keyName(key), type: valueType(object[key]) })),
    omitted_top_level_key_count: Math.max(0, keys.length - OBJECT_KEY_LIMIT),
    unrecognized_top_level_key_count: keys.filter(key => !DTO_FIELDS.has(key)).length,
    fields, safe_values: safeValues,
    owned_id_matches: typeof object?.Id === 'string' && object.Id === albumId,
    owned_path_matches: typeof object?.Path === 'string' && object.Path === MUSIC_DIRECTORY,
    shape: shape(value),
  };
}

function queryValues(searchParams, field) {
  return [...searchParams].filter(([name]) => name.toLowerCase() === field.toLowerCase()).map(([, value]) => value);
}

function boundedQueryList(searchParams, field, allowed) {
  const occurrences = queryValues(searchParams, field);
  if (!occurrences.length) return { state: 'missing' };
  if (occurrences.length !== 1) return { state: 'invalid', reason: 'duplicate_parameter', occurrence_count: occurrences.length };
  const value = occurrences[0];
  if (value === '') return { state: 'empty' };
  if (value.length > 4096) return { state: 'invalid', reason: 'parameter_over_limit' };
  const entries = value.split(',');
  if (entries.length > 32) return { state: 'invalid', reason: 'item_count_over_limit' };
  if (entries.some(entry => !allowed(entry))) return { state: 'invalid', reason: 'unsupported_list_value' };
  return { state: 'valid', values: entries };
}

function boundedSortQueryList(searchParams, field, allowed) {
  const occurrences = queryValues(searchParams, field);
  if (!occurrences.length) return { state: 'missing' };
  if (occurrences.length !== 1) return { state: 'invalid', reason: 'duplicate_parameter', occurrence_count: occurrences.length };
  const value = occurrences[0];
  if (value === '') return { state: 'empty' };
  if (value.length > 256 || !/^[\x20-\x7e]+$/.test(value)) return { state: 'invalid', reason: 'ascii_or_length_limit' };
  const entries = value.split(',');
  if (entries.length > 16) return { state: 'invalid', reason: 'token_count_over_limit' };
  if (entries.some(entry => !allowed.has(entry.toLowerCase()))) return { state: 'invalid', reason: 'unsupported_sort_enum' };
  // Preserve the actual case and order without inferring pairs or defaults.
  return { state: 'valid', values: entries };
}

function safeQuery(searchParams, musicValues = false) {
  const names = [], projections = [];
  let examined = 0, omitted = 0;
  for (const [name, value] of searchParams) {
    if (++examined > 64) { omitted += 1; continue; }
    const recognized = QUERY_FIELDS.get(name.toLowerCase());
    const safeName = recognized ?? (/^[A-Za-z][A-Za-z0-9_.-]{0,95}$/.test(name) &&
      !/^[a-f0-9]{16,}$/i.test(name) && !/^[A-Za-z0-9_-]{40,}$/.test(name) ? name : '{unrecognized query field}');
    if (!names.includes(safeName)) names.push(safeName);
    if (!['fields', 'excludefields'].includes(name.toLowerCase())) continue;
    if (value.length > 4096) {
      projections.push({ field: recognized, result: 'field_list_over_limit' });
      continue;
    }
    const entries = value.split(',');
    projections.push({ field: recognized, field_names: entries.slice(0, 64).filter(entry => DTO_FIELDS.has(entry)),
      unrecognized_field_count: entries.slice(0, 64).filter(entry => !DTO_FIELDS.has(entry)).length,
      omitted_field_count: Math.max(0, entries.length - 64) });
  }
  const result = { query_field_names: names, field_projections: projections, omitted_query_field_count: omitted };
  if (musicValues) {
    result.music_query_values = Object.fromEntries(RESOURCE_QUERY_FIELDS.map(field =>
      [field, boundedQueryList(searchParams, field, value => RESOURCE_ID.test(value))]));
    result.music_query_values.IncludeItemTypes = boundedQueryList(searchParams, 'IncludeItemTypes', value => MUSIC_ITEM_TYPES.has(value));
    result.music_query_values.SortBy = boundedSortQueryList(searchParams, 'SortBy', MUSIC_SORT_BY);
    result.music_query_values.SortOrder = boundedSortQueryList(searchParams, 'SortOrder', MUSIC_SORT_ORDER);
    const recursive = queryValues(searchParams, 'Recursive');
    result.music_query_values.Recursive = !recursive.length ? { state: 'missing' }
      : recursive.length !== 1 ? { state: 'invalid', reason: 'duplicate_parameter' }
        : recursive[0] === '' ? { state: 'empty' }
          : /^(?:true|false)$/i.test(recursive[0]) ? { state: 'valid', value: recursive[0].toLowerCase() === 'true' }
            : { state: 'invalid', reason: 'unsupported_boolean' };
  }
  return result;
}

export async function runAlbumDiagnostics({ page, context, target, report, snapshot, album, albumId, userId, referenceMusicNavigation = false }) {
  const result = report.album_diagnostics = {
    diagnostic_only: true, acceptance_passed: false, album: ALBUMS.has(album) ? album : null,
    phase: 'configuration', outcome: 'in_progress', responses: [], steps: [], response_overflow: 0,
    reference_music_navigation: referenceMusicNavigation === true, music_queries: [], music_query_overflow: 0,
    limits: { response_count: RESPONSE_LIMIT, body_read_count: 1, parse_and_retention_bytes: BODY_LIMIT,
      shape_depth: 3, shape_nodes: SHAPE_NODE_LIMIT, object_keys: OBJECT_KEY_LIMIT, array_sample: ARRAY_SAMPLE_LIMIT,
      music_query_count: MUSIC_QUERY_LIMIT, resource_ids_per_query_field: 32,
      sort_parameter_ascii_bytes: 256, sort_tokens_per_field: 16 },
    body_boundary: 'Playwright reads the browser-received entity. The size limit governs parsing and retention, not decompression allocation.',
    owned_identity_basis: 'exact observed response after unique UI album click',
    owned_identity_confirmed: false,
    cleanup_owner: 'caller',
  };
  let chain = Promise.resolve(), listening = false, queryListening = false, bodyReads = 0, pendingBodyReads = 0;
  let clickCalledMs = null, referenceNavigationStarted = false;
  const startedMs = Date.now(), elapsedMs = () => Date.now() - startedMs;
  let origin, route;
  const seen = new WeakSet();
  const seenQueries = new WeakSet();
  const albumRequests = new WeakMap();
  function blocked(reason) {
    result.failure_reason = reason;
    const error = new Error(`album_diagnostics_${result.phase}_failed`);
    error.code = 'ALBUM_DIAGNOSTICS_BLOCKED';
    error.stage = result.phase;
    throw error;
  }
  async function observeResponse(response, entry) {
    let timer;
    try {
      const headers = response.headers();
      const contentType = /^([A-Za-z0-9!#$&^_.+-]+\/[A-Za-z0-9!#$&^_.+-]+)(?:\s*;|$)/.exec(headers['content-type'] ?? '')?.[1].toLowerCase() ?? null;
      const declared = headers['content-length'];
      const declaredDecimal = typeof declared === 'string' && /^\d+$/.test(declared);
      const declaredValid = declaredDecimal && Number.isSafeInteger(Number(declared));
      entry.content_type = contentType;
      entry.declared_bytes = declaredValid ? Number(declared) : null;
      entry.declared_length_present = declared !== undefined;
      entry.encoding_type = headers['content-encoding'] === undefined ? 'unspecified'
        : /^(?:identity|gzip|br|deflate|zstd)$/i.test(headers['content-encoding']) ? headers['content-encoding'].toLowerCase() : 'unsupported';
      if (!entry.observed_after_click_attempt) { entry.body_result = 'pre_click_or_unbound_request_body_not_read'; return; }
      if (entry.http_status !== 200) { entry.body_result = 'non_200_body_not_read'; return; }
      if (contentType !== 'application/json') { entry.body_result = 'non_json_body_not_read'; return; }
      if (declaredDecimal && Number(declared) > BODY_LIMIT) { entry.body_result = 'declared_body_over_limit'; return; }
      if (bodyReads >= 1) { entry.body_result = 'single_body_read_budget_used'; return; }
      bodyReads += 1;
      pendingBodyReads += 1;
      const body = Promise.resolve().then(() => {
        entry.body_read_started_ms = elapsedMs();
        return response.body();
      });
      body.finally(() => { pendingBodyReads -= 1; }).catch(() => {});
      const bytes = await Promise.race([body, new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error('album_body_timeout')), 5000);
      })]);
      entry.body_read_finished_ms = elapsedMs();
      entry.actual_bytes = bytes.length;
      if (bytes.length > BODY_LIMIT) { entry.body_result = 'actual_body_over_limit'; return; }
      entry.source_response_sha256 = createHash('sha256').update(bytes).digest('hex');
      entry.hash_representation = 'Browser-decoded response entity bytes';
      let value;
      try { value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)); }
      catch { entry.body_result = 'invalid_json_or_utf8'; return; }
      entry.dto = safeDTO(value, album, albumId);
      entry.owned_identity_confirmed = entry.dto.owned_id_matches &&
        (!entry.dto.fields.Path.present || entry.dto.owned_path_matches);
      entry.path_identity_check = entry.dto.fields.Path.present ? 'present_path_must_match_owned_directory' : 'not_projected_by_client_request';
      entry.body_result = 'safe_dto_recorded';
    } catch { entry.body_result = 'body_unavailable_or_timed_out'; }
    finally {
      clearTimeout(timer);
      if (entry.body_read_started_ms !== undefined) entry.body_observation_finished_ms = elapsedMs();
    }
  }
  const albumRequestListener = request => {
    if (!listening || albumRequests.has(request)) return;
    try {
      if (request.method() !== 'GET') return;
      const observed = new URL(request.url());
      if (observed.origin !== origin || observed.username || observed.password || observed.pathname !== route) return;
      const start = request.timing().startTime;
      const available = typeof start === 'number' && Number.isFinite(start) && start > 0;
      albumRequests.set(request, { phase: result.phase, request_event_received_ms: elapsedMs(),
        request_started_ms: available ? start - startedMs : null,
        request_start_time_available: available, request_start_time_read_at: available ? 'request_event' : 'unavailable',
        click_call_already_recorded: clickCalledMs !== null });
    } catch { result.album_request_observer_errors = (result.album_request_observer_errors ?? 0) + 1; }
  };
  const listener = response => {
    if (!listening) return;
    try {
      const request = response.request();
      if (seen.has(request) || request.method() !== 'GET') return;
      const observed = new URL(response.url());
      if (observed.origin !== origin || observed.username || observed.password || observed.pathname !== route) return;
      seen.add(request);
      if (result.responses.length >= RESPONSE_LIMIT) { result.response_overflow += 1; return; }
      const binding = albumRequests.get(request);
      if (binding && !binding.request_start_time_available) {
        // Some browser timing fields become available with the response; the request event still owns its phase and click boundary.
        const start = request.timing().startTime;
        if (typeof start === 'number' && Number.isFinite(start) && start > 0) {
          binding.request_started_ms = start - startedMs;
          binding.request_start_time_available = true;
          binding.request_start_time_read_at = 'response_event';
        }
      }
      const afterClick = Boolean(binding?.request_start_time_available && binding.click_call_already_recorded &&
        clickCalledMs !== null && binding.request_started_ms >= clickCalledMs);
      const entry = { route_kind: 'owned_album_detail', method: 'GET', http_status: response.status(),
        phase: result.phase, request_phase: binding?.phase ?? null, response_phase: result.phase,
        request_event_received_ms: binding?.request_event_received_ms ?? null,
        request_started_ms: binding?.request_started_ms ?? null,
        request_start_time_available: binding?.request_start_time_available ?? false,
        request_start_time_read_at: binding?.request_start_time_read_at ?? 'unavailable',
        response_received_ms: elapsedMs(), click_called_ms: clickCalledMs,
        observed_after_click_attempt: afterClick, ...safeQuery(observed.searchParams, referenceMusicNavigation), body_result: 'pending' };
      result.responses.push(entry);
      chain = chain.then(() => observeResponse(response, entry)).catch(() => {
        entry.body_result = 'response_observation_failed';
      });
    } catch { result.response_observer_errors = (result.response_observer_errors ?? 0) + 1; }
  };
  const queryListener = request => {
    if (!queryListening || seenQueries.has(request)) return;
    try {
      if (request.method() !== 'GET') return;
      const observed = new URL(request.url());
      if (observed.origin !== origin || observed.username || observed.password) return;
      const queryUsers = queryValues(observed.searchParams, 'UserId');
      if (queryUsers.length && (queryUsers.length !== 1 || queryUsers[0] !== userId)) return;
      const itemPrefix = `/emby/Users/${encodeURIComponent(userId)}/Items`;
      let routeKind = null, userScope = null;
      if (observed.pathname === itemPrefix) {
        routeKind = 'current_user_items'; userScope = 'path';
      } else if (observed.pathname.startsWith(`${itemPrefix}/`)) {
        const suffix = observed.pathname.slice(itemPrefix.length + 1);
        if (RESOURCE_ID.test(suffix) || ['Latest', 'Resume', 'Root'].includes(suffix)) {
          routeKind = RESOURCE_ID.test(suffix) ? 'current_user_item_detail' : `current_user_items_${suffix.toLowerCase()}`;
          userScope = 'path';
        }
      } else if (['/emby/Items', '/emby/Artists', '/emby/Artists/AlbumArtists', '/emby/AlbumArtists'].includes(observed.pathname)) {
        if (queryUsers.length === 1) {
          routeKind = observed.pathname === '/emby/Items' ? 'current_user_items'
            : observed.pathname === '/emby/Artists' ? 'current_user_artists' : 'current_user_album_artists';
          userScope = 'query';
        }
      }
      if (!routeKind) return;
      seenQueries.add(request);
      if (result.music_queries.length >= MUSIC_QUERY_LIMIT) { result.music_query_overflow += 1; return; }
      result.music_queries.push({ phase: result.phase, method: 'GET', route_kind: routeKind,
        owned_current_user_scope: userScope, is_original_ui_request: true, ...safeQuery(observed.searchParams, true) });
    } catch { result.music_query_observer_errors = (result.music_query_observer_errors ?? 0) + 1; }
  };
  async function capture(name) {
    await snapshot(name);
    result.steps.push({ label: name, evidence: 'Caller-sanitized screenshot and visible DOM snapshot' });
  }
  function cardControls(title) {
    return page.getByText(title, { exact: true }).filter({ visible: true })
      .locator('xpath=ancestor-or-self::*[(self::button or self::a or @role="button") and @data-action="link" and ancestor::*[contains(concat(" ", normalize-space(@class), " "), " card ") or contains(concat(" ", normalize-space(@class), " "), " cardBox ")]][1]')
      .filter({ visible: true });
  }
  async function uniqueCard(title, reason) {
    const cards = cardControls(title);
    const deadline = Date.now() + 15000;
    while (!await cards.count() && Date.now() < deadline) await page.waitForTimeout(250);
    if (await cards.count() !== 1) blocked(reason);
    return cards;
  }
  async function navigationEvidence(control, label) {
    const evidence = await control.evaluate(element => {
      const allowedNames = new Set(['Home', 'M3e Reference Music', 'Albums', 'M3e Synthetic Album', 'Music']);
      const text = (element.innerText ?? '').trim(), ariaLabel = element.getAttribute('aria-label');
      return { tag: element.tagName.toLowerCase(), role: element.getAttribute('role'),
        text: allowedNames.has(text) ? text : null, label: allowedNames.has(ariaLabel) ? ariaLabel : null,
        semantic_classes: [...element.classList].filter(value => ['cardTextActionButton', 'actionlink', 'emby-button',
          'emby-tab-button', 'tabButton', 'navMenuOption'].includes(value)),
        is_link_action: element.getAttribute('data-action') === 'link',
        selected: element.getAttribute('aria-selected') === 'true', visible: element.getClientRects().length > 0 };
    });
    result.steps.push({ label, phase: result.phase, control: evidence });
  }
  async function homeThroughUI(label) {
    const home = page.getByRole('link', { name: 'Home', exact: true }).filter({ visible: true });
    if (await home.count() !== 1) blocked('unique_home_link_not_observed');
    await navigationEvidence(home, `${label}-control`);
    const changeExpected = await cardControls('M3e Reference Music').count() !== 1;
    const before = page.url();
    await home.click({ timeout: 8000 });
    await uniqueCard('M3e Reference Music', 'reference_music_home_card_missing_or_ambiguous');
    const deadline = Date.now() + 15000;
    while (changeExpected && page.url() === before && Date.now() < deadline) await page.waitForTimeout(250);
    const changed = page.url() !== before;
    result.steps.push({ label: `${label}-navigation`, phase: result.phase, url_changed: changed, url_change_expected: changeExpected });
    if (changeExpected && !changed) blocked('reference_home_navigation_not_observed');
    await capture(label);
  }
  async function openReferenceMusic() {
    result.phase = 'reference_home_before_music';
    await homeThroughUI('reference-music-home-before');
    result.phase = 'reference_music_library';
    const library = await uniqueCard('M3e Reference Music', 'reference_music_library_card_missing_or_ambiguous');
    await navigationEvidence(library, 'reference-music-library-control');
    const before = page.url();
    await library.click({ timeout: 8000 });
    const deadline = Date.now() + 15000;
    while (page.url() === before && Date.now() < deadline) await page.waitForTimeout(250);
    const changed = page.url() !== before;
    result.steps.push({ label: 'reference-music-library-navigation', phase: result.phase, url_changed: changed });
    if (!changed) blocked('reference_music_library_navigation_not_observed');
    await page.waitForTimeout(500);
    await capture('reference-music-library-observed');
    result.phase = 'reference_music_albums_tab';
    const tabs = page.getByText('Albums', { exact: true }).filter({ visible: true })
      .locator('xpath=ancestor-or-self::*[@role="tab" or (self::button and (contains(concat(" ", normalize-space(@class), " "), " emby-tab-button ") or contains(concat(" ", normalize-space(@class), " "), " tabButton ")))][1]')
      .filter({ visible: true });
    const tabDeadline = Date.now() + 15000;
    while (!await tabs.count() && !await cardControls(album).count() && Date.now() < tabDeadline) await page.waitForTimeout(250);
    const count = await tabs.count();
    if (count > 1) blocked('albums_tab_ambiguous');
    result.albums_tab_observation = { visible_count: count, clicked: count === 1 };
    if (count === 1) {
      await navigationEvidence(tabs, 'reference-music-albums-tab-control');
      const tabURL = page.url();
      await tabs.click({ timeout: 8000 });
      await uniqueCard(album, 'reference_owned_album_card_missing_or_ambiguous');
      result.steps.push({ label: 'reference-music-albums-tab-navigation', phase: result.phase, url_changed: page.url() !== tabURL });
      await capture('reference-music-albums-observed');
    }
  }
  try {
    if (!ALBUMS.has(album) || typeof albumId !== 'string' || !/^[A-Za-z0-9-]{1,64}$/.test(albumId) ||
        typeof userId !== 'string' || !/^[A-Za-z0-9-]{1,64}$/.test(userId) || typeof referenceMusicNavigation !== 'boolean') blocked('invalid_owned_album_input');
    const bound = new URL(target?.origin ?? '');
    if (bound.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(bound.hostname) || bound.origin !== target.origin) {
      blocked('invalid_target_origin');
    }
    origin = bound.origin;
    if (referenceMusicNavigation && (origin !== 'http://127.0.0.1:18197' || albumId !== '13' || album !== 'M3e Synthetic Album')) {
      blocked('reference_music_navigation_scope_mismatch');
    }
    route = `/emby/Users/${encodeURIComponent(userId)}/Items/${encodeURIComponent(albumId)}`;
    context.on('request', albumRequestListener);
    context.on('response', listener);
    listening = true;
    if (referenceMusicNavigation) {
      context.on('request', queryListener);
      queryListening = true;
      await capture('reference-music-navigation-before');
      referenceNavigationStarted = true;
      await openReferenceMusic();
    }
    result.phase = 'album_card';
    await capture('album-diagnostics-before');
    const cards = await uniqueCard(album, 'exact_album_card_missing_or_ambiguous');
    const cardIdentity = await cards.evaluate((element, expected) => {
      const card = element.closest('.card[data-id],.cardBox[data-id]');
      return { card_with_data_id_present: card !== null, owned_album_id_matches: card?.getAttribute('data-id') === expected,
        link_action_observed: element.getAttribute('data-action') === 'link' };
    }, albumId);
    result.card_identity = cardIdentity;
    if (!cardIdentity.link_action_observed || cardIdentity.card_with_data_id_present && !cardIdentity.owned_album_id_matches) {
      blocked('album_card_owned_id_mismatch');
    }
    result.phase = 'open_album';
    const albumURLBefore = page.url();
    await navigationEvidence(cards, 'album-diagnostics-selected-card');
    clickCalledMs = elapsedMs();
    result.click_called_ms = clickCalledMs;
    await cards.click({ timeout: 8000 });
    result.phase = 'observe_album';
    const observationDeadline = Date.now() + 15000;
    do {
      result.ui_observation = await page.evaluate(expected => {
        const visible = element => element.getClientRects().length > 0 && getComputedStyle(element).visibility !== 'hidden';
        const headings = [...document.querySelectorAll('h1,h2,h3,h4,h5,h6,[role="heading"]')].filter(visible);
        const tracks = [...document.querySelectorAll('.listItem,[role="row"],[role="listitem"]')].filter(visible)
          .filter(element => /(?:M3e MP3|M3e FLAC|M3e Client Audio)/.test(element.innerText ?? ''));
        return { album_heading_observed: headings.some(element => (element.innerText ?? '').trim() === expected),
          fixture_track_row_count: tracks.length, visible_heading_count: headings.length };
      }, album);
      if (referenceMusicNavigation) {
        const counts = await Promise.all(['M3e MP3', 'M3e FLAC'].map(title =>
          page.getByText(title, { exact: true }).filter({ visible: true }).evaluateAll(elements => elements.filter(element =>
            element.closest('.listItem,[role="row"],[role="listitem"]')).length)));
        result.ui_observation.reference_mp3_title_count = counts[0];
        result.ui_observation.reference_flac_title_count = counts[1];
        result.ui_observation.reference_fixture_tracks_observed = counts.every(count => count > 0);
      }
      const contentReady = referenceMusicNavigation
        ? result.ui_observation.album_heading_observed && result.ui_observation.reference_fixture_tracks_observed
        : result.ui_observation.album_heading_observed || result.ui_observation.fixture_track_row_count;
      if (result.responses.some(entry => entry.observed_after_click_attempt) && contentReady) break;
      await page.waitForTimeout(250);
    } while (Date.now() < observationDeadline);
    result.ui_observation.wait_limit_reached = Date.now() >= observationDeadline;
    result.ui_observation.album_content_observed = result.ui_observation.album_heading_observed || result.ui_observation.fixture_track_row_count > 0;
    result.steps.push({ label: 'album-diagnostics-navigation', phase: result.phase, url_changed: page.url() !== albumURLBefore });
    await capture('album-diagnostics-observed');
    if (referenceMusicNavigation) {
      if (!result.ui_observation.album_heading_observed || !result.ui_observation.reference_fixture_tracks_observed) {
        result.reference_music_ui_incomplete = true;
        blocked('reference_music_ui_incomplete');
      }
      result.phase = 'reference_home_after_music';
      await homeThroughUI('reference-music-home-after');
      result.returned_home_through_ui = true;
    }
    result.phase = 'complete';
  } catch {
    result.outcome = result.reference_music_ui_incomplete ? 'reference_music_ui_incomplete' : 'diagnostic_blocked';
    result.failure_reason ??= 'ui_or_diagnostic_observation_failed';
    await capture('album-diagnostics-blocked').catch(() => {});
    if (referenceNavigationStarted && !result.returned_home_through_ui) {
      const failurePhase = result.phase, failureReason = result.failure_reason;
      result.phase = 'reference_home_cleanup';
      result.home_cleanup_attempted = true;
      try {
        await homeThroughUI('reference-music-home-cleanup');
        result.returned_home_through_ui = true;
        result.home_cleanup_result = 'returned_home_through_ui';
      } catch { result.home_cleanup_result = 'home_navigation_not_completed'; }
      finally { result.phase = failurePhase; result.failure_reason = failureReason; }
    }
    throw Object.assign(new Error(`album_diagnostics_${result.phase}_failed`), { code: 'ALBUM_DIAGNOSTICS_BLOCKED', stage: result.phase });
  } finally {
    listening = false;
    queryListening = false;
    context.off('response', listener);
    context.off('request', albumRequestListener);
    context.off('request', queryListener);
    await chain;
    result.listener_removed = true;
    result.pending_observer_tasks_drained = true;
    result.pending_body_reads_at_return = pendingBodyReads;
    result.pending_observations_drained = pendingBodyReads === 0;
    result.body_read_count = bodyReads;
    result.music_query_capture_complete = result.music_query_overflow === 0 && !result.music_query_observer_errors;
    result.owned_identity_confirmed = result.responses.some(entry => entry.body_result === 'safe_dto_recorded' && entry.owned_identity_confirmed === true);
    if (result.outcome === 'in_progress') {
      result.outcome = pendingBodyReads ? 'diagnostic_body_read_unsettled' : result.responses.some(entry => entry.body_result === 'safe_dto_recorded')
        ? 'diagnostic_dto_observed' : result.responses.length ? 'diagnostic_response_metadata_only' : 'diagnostic_no_matching_response';
    }
  }
  if (!result.owned_identity_confirmed) {
    result.phase = 'response_identity';
    result.outcome = 'diagnostic_failed';
    blocked('owned_album_response_not_confirmed');
  }
  return result;
}
