/** Passively compare one owned audio request with its original-client playback reports. */

const RECORD_LIMIT = 64;
const REQUEST_BODY_LIMIT = 32768;
const REQUEST_BODY_TOTAL = 1048576;
const ERROR_BODY_LIMIT = 8192;
const ERROR_BODY_TOTAL = 32768;
const BODY_FIELDS = ['PlaySessionId', 'ItemId', 'MediaSourceId', 'SessionId'];
const REFERENCE_FIELDS = ['PlaySessionId', 'MediaSourceId', 'api_key', 'DeviceId'];
const ERROR_CODES = new Set(['not_found', 'invalid_playback_request', 'invalid_audio_request', 'invalid_json']);

const typeOf = value => value === null ? 'null' : Array.isArray(value) ? 'array' : typeof value;
const missing = () => ({ state: 'missing', present: false, type: 'missing' });
const unavailable = () => ({ state: 'unavailable', present: null, type: 'unavailable' });
function textValue(value, maximum = 4096) {
  const type = typeOf(value);
  const result = { state: 'invalid', present: true, type };
  if (type === 'string' || type === 'array') result.length = value.length;
  if (type !== 'string') return result;
  if (!value.length) return { ...result, state: 'empty' };
  if (value.length > maximum || /[\x00-\x1f\x7f]/.test(value)) return { ...result, state: 'invalid' };
  return { ...result, state: 'present', value };
}
function publicValue(value) {
  return Object.fromEntries(['state', 'present', 'type', 'length', 'occurrence_count'].filter(key =>
    Object.hasOwn(value, key)).map(key => [key, value[key]]));
}
function queryValue(parameters, name, maximum = 4096) {
  const names = [...new Set(parameters.keys())].filter(key => key.toLowerCase() === name.toLowerCase());
  const values = names.flatMap(key => parameters.getAll(key));
  if (!values.length) return missing();
  if (values.length !== 1) return { state: 'duplicate', present: true, type: 'string', occurrence_count: values.length };
  return textValue(values[0], maximum);
}
function headerValue(value, maximum = 4096) {
  if (value === null || value === undefined) return missing();
  if (typeof value === 'string' && /[,\r\n]/.test(value)) return { state: 'duplicate_or_combined', present: true, type: 'string' };
  return textValue(value, maximum);
}
function contentType(value) {
  if (value === null || value === undefined) return 'missing';
  const type = typeof value === 'string' ? value.split(';', 1)[0].trim().toLowerCase() : '';
  return ['application/json', 'text/plain', 'audio/mpeg', 'audio/flac', 'audio/x-flac', 'application/octet-stream'].includes(type) ? type : 'other';
}
function compare(observed, reference) {
  if (observed.state !== 'present') return { state: observed.state, unavailable_side: 'observed' };
  if (reference.state !== 'present') return { state: `reference_${reference.state}`, unavailable_side: 'reference' };
  return { state: 'comparable', equal: observed.value === reference.value };
}
async function bounded(promise, milliseconds) {
  let timer;
  try {
    if (milliseconds <= 0) throw new Error('audio_report_diagnostic_timeout');
    return await Promise.race([promise, new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error('audio_report_diagnostic_timeout')), milliseconds);
    })]);
  } finally { clearTimeout(timer); }
}

function authorizationFields(raw) {
  const absent = () => ({ token: missing(), device: missing() });
  if (raw === null || raw === undefined) return absent();
  const invalid = () => ({ token: { state: 'invalid_authorization', present: true, type: 'string' },
    device: { state: 'invalid_authorization', present: true, type: 'string' } });
  if (typeof raw !== 'string' || raw.length > 8192 || /[\x00-\x1f\x7f]/.test(raw)) return invalid();
  const scheme = /^(?:Emby|MediaBrowser)\s+/i.exec(raw);
  if (!scheme) return invalid();
  let position = scheme[0].length, count = 0;
  const selected = { token: [], deviceid: [] };
  while (position < raw.length) {
    const field = /^([A-Za-z][A-Za-z0-9_-]{0,63})\s*=\s*/.exec(raw.slice(position));
    if (!field || ++count > 32) return invalid();
    const name = field[1].toLowerCase();
    position += field[0].length;
    let value = '';
    if (raw[position] === '"') {
      position += 1;
      let closed = false;
      while (position < raw.length) {
        const character = raw[position++];
        if (character === '"') { closed = true; break; }
        if (character === '\\') {
          if (position >= raw.length) return invalid();
          value += raw[position++];
        } else value += character;
      }
      if (!closed) return invalid();
    } else {
      const end = raw.indexOf(',', position);
      value = raw.slice(position, end < 0 ? raw.length : end).trim();
      position = end < 0 ? raw.length : end;
      if (!value || /["\\]/.test(value)) return invalid();
    }
    if (Object.hasOwn(selected, name)) selected[name].push(value);
    while (raw[position] === ' ') position += 1;
    if (position === raw.length) break;
    if (raw[position++] !== ',') return invalid();
    while (raw[position] === ' ') position += 1;
    if (position === raw.length) return invalid();
  }
  const one = (values, maximum) => !values.length ? missing() : values.length !== 1
    ? { state: 'duplicate', present: true, type: 'string', occurrence_count: values.length } : textValue(values[0], maximum);
  return { token: one(selected.token, 4096), device: one(selected.deviceid, 256) };
}

function topLevelOccurrences(text) {
  const counts = new Map();
  let depth = 0;
  for (let index = 0; index < text.length; index += 1) {
    const character = text[index];
    if (character === '{' || character === '[') { depth += 1; continue; }
    if (character === '}' || character === ']') { depth -= 1; continue; }
    if (character !== '"') continue;
    const start = index;
    for (index += 1; index < text.length; index += 1) {
      if (text[index] === '\\') { index += 1; continue; }
      if (text[index] === '"') break;
    }
    let next = index + 1;
    while (/\s/.test(text[next] ?? '') && next < text.length) next += 1;
    if (depth === 1 && text[next] === ':') {
      const name = JSON.parse(text.slice(start, index + 1)).toLowerCase();
      if (BODY_FIELDS.some(field => field.toLowerCase() === name)) counts.set(name, (counts.get(name) ?? 0) + 1);
    }
  }
  return counts;
}

/** No network initiation, login, browser control, or successful response body reads. */
export function createAudioReportDiagnostics({ context, target, ownedItemId }) {
  let origin;
  try {
    const parsed = new URL(target?.origin ?? target);
    if (parsed.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(parsed.hostname) ||
        parsed.username || parsed.password || parsed.search || parsed.hash || typeof ownedItemId !== 'string' ||
        !/^[a-f0-9]{32}$/.test(ownedItemId) || typeof context?.on !== 'function' || typeof context?.off !== 'function') throw new Error();
    origin = parsed.origin;
  } catch { throw Object.assign(new Error('audio_report_diagnostics_configuration_failed'), { stack: undefined }); }
  const started = Date.now(), elapsed = () => Date.now() - started;
  const report = { diagnostic_only: true, ui_acceptance: false, outcome: 'collecting', records: [], record_overflow: 0,
    observer_errors: 0, optional_metadata_errors: 0, request_body_bytes: 0, request_bodies_over_limit: 0,
    limits: { records: RECORD_LIMIT, request_body_bytes: REQUEST_BODY_LIMIT, total_request_body_bytes: REQUEST_BODY_TOTAL,
      request_header_wait_ms: 1500, error_bodies: 4, error_body_bytes: ERROR_BODY_LIMIT, total_error_body_bytes: ERROR_BODY_TOTAL, error_wait_ms: 2000 },
    error_diagnostics: { diagnostic_only: true, reads: 0, reserved_bytes: 0, parsed_bytes: 0, stopped_after_over_limit: false,
      selection: 'First HTTP 400 or 404 response for each Playing, Progress, and Stopped route' },
    successful_response_bodies_read: false,
    comparison_policy: 'Raw values exist only in memory. Missing, duplicate, invalid, or ambiguous sources never become false equality.',
  };
  let accepting = true, disposed = false, cleared = false, eventOrder = 0;
  let states = new WeakMap(), privateRows = [], privateReferences = [];
  let owned = textValue(ownedItemId), prefixedOwned = textValue(`mediasource_${ownedItemId}`);
  const pending = new Set(), failedKinds = new Set();
  let pendingErrorBodies = 0;

  function reference(field, references = privateReferences) {
    if (!references.length) return { state: 'unobserved', present: false, type: 'unavailable' };
    const values = references.map(row => row.reference[field]);
    const first = values[0];
    if (values.some(value => value.state !== first.state || value.state === 'present' && value.value !== first.value)) {
      return { state: 'ambiguous', present: true, type: 'unavailable', occurrence_count: values.length };
    }
    return first;
  }
  function refresh() {
    if (cleared) return;
    const baseline = Object.fromEntries(REFERENCE_FIELDS.map(field => [field, reference(field)]));
    report.universal_reference = { request_count: privateReferences.length,
      purpose: 'Inventory only; report comparisons use only preceding universal request events',
      http_206_observed: privateReferences.some(row => row.public.http_status === 206),
      response_statuses: privateReferences.map(row => row.public.http_status),
      ambiguous: Object.values(baseline).some(value => value.state === 'ambiguous'),
      fields: Object.fromEntries(REFERENCE_FIELDS.map(field => [field, publicValue(baseline[field])])) };
    for (const row of privateRows) {
      if (row.kind === 'universal') continue;
      const preceding = privateReferences.filter(candidate => candidate.public.request_order < row.public.request_order);
      const precedingValues = Object.fromEntries(REFERENCE_FIELDS.map(field => [field, reference(field, preceding)]));
      const ambiguous = Object.values(precedingValues).some(value => value.state === 'ambiguous') || preceding.length > 1 &&
        Object.values(precedingValues).some(value => !['present', 'missing', 'empty'].includes(value.state));
      const comparisonReference = field => ambiguous ? { state: 'ambiguous' } : precedingValues[field];
      row.public.universal_reference = { preceding_request_count: preceding.length, ambiguous,
        http_206_observed: preceding.some(candidate => candidate.public.http_status === 206),
        http_206_received_before_report_request: preceding.some(candidate => candidate.public.http_status === 206 &&
          candidate.public.response_order < row.public.request_order),
        fields: Object.fromEntries(REFERENCE_FIELDS.map(field => [field, publicValue(precedingValues[field])])),
        event_timing: preceding.map(candidate => ({ request_order: candidate.public.request_order,
          request_elapsed_ms: candidate.public.request_elapsed_ms, response_order: candidate.public.response_order ?? null,
          response_elapsed_ms: candidate.public.response_elapsed_ms ?? null, http_status: candidate.public.http_status,
          response_received_before_report_request: candidate.public.response_order === undefined ? null
            : candidate.public.response_order < row.public.request_order })) };
      row.public.body_fields = Object.fromEntries(BODY_FIELDS.map(field => [field, publicValue(row.body[field])]));
      row.public.comparisons = {
        PlaySessionId: compare(row.body.PlaySessionId, comparisonReference('PlaySessionId')),
        ItemId_owned: compare(row.body.ItemId, owned),
        MediaSourceId_owned: compare(row.body.MediaSourceId, owned),
        MediaSourceId_prefixed_owned: compare(row.body.MediaSourceId, prefixedOwned),
        MediaSourceId_universal: compare(row.body.MediaSourceId, comparisonReference('MediaSourceId')),
        SessionId: { state: 'reference_unavailable', reference_acquisition: 'No login or successful authentication body is read' },
        token_sources: Object.fromEntries(Object.entries(row.tokens).map(([name, value]) => [name,
          { presence: publicValue(value), comparison: compare(value, comparisonReference('api_key')) }])),
        device_sources: Object.fromEntries(Object.entries(row.devices).map(([name, value]) => [name,
          { presence: publicValue(value), comparison: compare(value, comparisonReference('DeviceId')) }])),
      };
    }
  }
  function track(work) {
    const task = Promise.resolve().then(work).catch(() => { report.observer_errors += 1; })
      .finally(() => { pending.delete(task); if (!cleared) refresh(); });
    pending.add(task);
  }
  function routeKind(request) {
    const url = new URL(request.url());
    if (url.origin !== origin || url.username || url.password) return null;
    const audio = /^\/(?:emby\/)?Audio\/([a-f0-9]{32})\/universal\/?$/i.exec(url.pathname);
    if (request.method() === 'GET' && audio?.[1] === owned.value) return { kind: 'universal', parameters: url.searchParams };
    const playing = /^\/emby\/Sessions\/Playing(?:\/(Progress|Stopped))?\/?$/i.exec(url.pathname);
    if (request.method() === 'POST' && playing) return { kind: playing[1]?.toLowerCase() ?? 'playing', parameters: url.searchParams };
    return null;
  }
  async function inspectRequest(request, row) {
    try {
      const [type, token, device, authorization] = await Promise.allSettled([
        'content-type', 'x-emby-token', 'x-emby-device-id', 'authorization',
      ].map(name => bounded(request.headerValue(name), 1500)));
      if (cleared) return;
      row.tokens.header_x_emby_token = token.status === 'fulfilled' ? headerValue(token.value) : unavailable();
      row.devices.header_x_emby_device_id = device.status === 'fulfilled' ? headerValue(device.value, 256) : unavailable();
      const fields = authorization.status === 'fulfilled' ? authorizationFields(authorization.value)
        : { token: unavailable(), device: unavailable() };
      row.tokens.authorization_token = fields.token;
      row.devices.authorization_device_id = fields.device;
      const optionalErrors = [token, device, authorization].filter(value => value.status === 'rejected').length;
      report.optional_metadata_errors += optionalErrors;
      row.public.optional_request_metadata_state = optionalErrors ? 'partial' : 'recorded';
      if (type.status !== 'fulfilled') {
        row.public.request_metadata_state = 'unavailable_or_timed_out'; report.observer_errors += 1; return;
      }
      row.public.request_content_type = contentType(type.value);
      row.public.request_metadata_state = 'recorded';
      if (!['application/json', 'text/plain'].includes(row.public.request_content_type)) { row.public.body_state = 'unsupported_content_type_not_read'; return; }
      if (report.request_body_bytes + REQUEST_BODY_LIMIT > REQUEST_BODY_TOTAL) { row.public.body_state = 'total_body_budget_exhausted'; return; }
      const bytes = request.postDataBuffer();
      if (bytes === null) { row.public.body_state = 'missing'; row.body = Object.fromEntries(BODY_FIELDS.map(field => [field, missing()])); return; }
      row.public.request_body_bytes = bytes.length;
      if (bytes.length > REQUEST_BODY_LIMIT) { report.request_bodies_over_limit += 1; row.public.body_state = 'body_over_limit_not_parsed'; return; }
      report.request_body_bytes += bytes.length;
      if (!bytes.length) { row.public.body_state = 'empty'; row.body = Object.fromEntries(BODY_FIELDS.map(field => [field, missing()])); return; }
      let value;
      const text = bytes.toString('utf8');
      try { value = JSON.parse(text); } catch { row.public.body_state = 'invalid_json'; return; }
      row.public.body_top_level_type = typeOf(value);
      if (!value || typeof value !== 'object' || Array.isArray(value)) { row.public.body_state = 'unsupported_top_level'; return; }
      const occurrences = topLevelOccurrences(text), keys = Object.keys(value);
      for (const field of BODY_FIELDS) {
        const count = occurrences.get(field.toLowerCase()) ?? 0;
        if (!count) row.body[field] = missing();
        else if (count !== 1) row.body[field] = { state: 'duplicate', present: true, type: 'unavailable', occurrence_count: count };
        else {
          row.body[field] = textValue(value[keys.find(key => key.toLowerCase() === field.toLowerCase())]);
          if (field === 'SessionId') delete row.body[field].value;
        }
      }
      row.public.body_state = 'necessary_fields_observed';
    } catch {
      if (!cleared) { row.public.request_metadata_state = 'unavailable_or_timed_out'; report.observer_errors += 1; }
    }
  }
  async function inspectResponse(response, row, firstFailure) {
    const deadline = Date.now() + 2000;
    try {
      const type = await bounded(response.headerValue('content-type'), Math.min(1500, deadline - Date.now()));
      if (cleared) return;
      row.public.response_content_type = contentType(type);
      row.public.response_metadata_state = 'recorded';
      if (!firstFailure) return;
      const diagnostic = row.public.error_body = { diagnostic_only: true, state: 'not_read' };
      if (!['application/json', 'text/plain'].includes(row.public.response_content_type)) { diagnostic.state = 'unsupported_response_content_type'; return; }
      const declared = await bounded(response.headerValue('content-length'), deadline - Date.now());
      if (cleared) return;
      if (declared === null || declared === undefined) { diagnostic.state = 'content_length_missing'; return; }
      if (!/^[0-9]{1,16}$/.test(declared) || !Number.isSafeInteger(Number(declared))) { diagnostic.state = 'content_length_invalid'; return; }
      diagnostic.declared_bytes = Number(declared);
      if (Number(declared) > ERROR_BODY_LIMIT) { diagnostic.state = 'declared_body_over_limit'; return; }
      const budget = report.error_diagnostics;
      if (budget.stopped_after_over_limit || budget.reads >= 4 || budget.reserved_bytes + ERROR_BODY_LIMIT > ERROR_BODY_TOTAL) {
        diagnostic.state = 'error_body_budget_exhausted'; return;
      }
      budget.reads += 1; budget.reserved_bytes += ERROR_BODY_LIMIT; pendingErrorBodies += 1;
      const body = Promise.resolve().then(() => {
        if (cleared || Date.now() >= deadline) throw new Error('error_read_deadline');
        return response.body();
      });
      body.finally(() => { pendingErrorBodies -= 1; }).catch(() => {});
      const bytes = await bounded(body, deadline - Date.now());
      if (cleared) return;
      diagnostic.actual_bytes = bytes.length;
      if (bytes.length > ERROR_BODY_LIMIT) { budget.stopped_after_over_limit = true; diagnostic.state = 'actual_body_over_limit_not_parsed'; return; }
      budget.parsed_bytes += bytes.length;
      let parsed;
      try { parsed = JSON.parse(bytes.toString('utf8')); } catch { diagnostic.state = 'invalid_json'; return; }
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) { diagnostic.state = 'unsupported_error_shape'; return; }
      const candidates = [{ source: 'ResponseStatus', value: parsed.ResponseStatus }, { source: 'top_level', value: parsed }];
      const eligible = candidate => candidate.value && typeof candidate.value === 'object' && !Array.isArray(candidate.value);
      const selected = candidates.find(candidate => eligible(candidate) && Object.hasOwn(candidate.value, 'ErrorCode'))
        ?? candidates.find(candidate => eligible(candidate) && Object.hasOwn(candidate.value, 'Message'));
      if (!selected) { diagnostic.state = 'allowed_error_fields_absent'; return; }
      diagnostic.field_source = selected.source;
      diagnostic.message_present = Object.hasOwn(selected.value, 'Message');
      if (!Object.hasOwn(selected.value, 'ErrorCode')) diagnostic.error_code = { state: 'missing', present: false, type: 'missing' };
      else {
        const code = selected.value.ErrorCode;
        diagnostic.error_code = { state: ERROR_CODES.has(code) ? 'known' : 'unknown', present: true, type: typeOf(code) };
        if (typeof code === 'string' || Array.isArray(code)) diagnostic.error_code.length = code.length;
        if (ERROR_CODES.has(code)) diagnostic.error_code.value = code;
      }
      diagnostic.state = 'error_code_observed';
    } catch {
      if (!cleared) {
        report.observer_errors += 1;
        if (firstFailure) (row.public.error_body ??= { diagnostic_only: true }).state = 'unavailable_or_timed_out';
        else row.public.response_metadata_state = 'unavailable_or_timed_out';
      }
    }
  }

  const onRequest = request => {
    if (!accepting || states.has(request)) return;
    try {
      const selected = routeKind(request);
      if (!selected) return;
      if (report.records.length >= RECORD_LIMIT) { report.record_overflow += 1; return; }
      const entry = { index: report.records.length, route_kind: selected.kind, method: request.method(),
        request_order: ++eventOrder, request_elapsed_ms: elapsed(), http_status: null, request_content_type: selected.kind === 'universal' ? 'not_inspected' : 'pending',
        response_content_type: 'pending' };
      const row = { kind: selected.kind, public: entry };
      states.set(request, row); privateRows.push(row); report.records.push(entry);
      if (selected.kind === 'universal') {
        row.reference = Object.fromEntries(REFERENCE_FIELDS.map(field => [field, queryValue(selected.parameters, field,
          field === 'api_key' ? 4096 : 256)]));
        privateReferences.push(row);
        entry.reference_fields = Object.fromEntries(REFERENCE_FIELDS.map(field => [field, publicValue(row.reference[field])]));
      } else {
        row.body = Object.fromEntries(BODY_FIELDS.map(field => [field, unavailable()]));
        row.tokens = { query_x_emby_token: queryValue(selected.parameters, 'X-Emby-Token'),
          header_x_emby_token: unavailable(), authorization_token: unavailable() };
        row.devices = { query_device_id: queryValue(selected.parameters, 'DeviceId', 256),
          query_x_emby_device_id: queryValue(selected.parameters, 'X-Emby-Device-Id', 256),
          header_x_emby_device_id: unavailable(), authorization_device_id: unavailable() };
        entry.body_state = 'pending';
        track(() => inspectRequest(request, row));
      }
      refresh();
    } catch { report.observer_errors += 1; }
  };
  const onResponse = response => {
    if (!accepting) return;
    try {
      const row = states.get(response.request());
      if (!row || row.public.response_elapsed_ms !== undefined) return;
      row.public.http_status = response.status(); row.public.response_elapsed_ms = elapsed(); row.public.response_order = ++eventOrder;
      const failure = row.kind !== 'universal' && [400, 404].includes(row.public.http_status);
      const firstFailure = failure && !failedKinds.has(row.kind);
      if (firstFailure) failedKinds.add(row.kind);
      if (failure && !firstFailure) row.public.error_body = { diagnostic_only: true, state: 'first_failure_for_route_already_selected' };
      track(() => inspectResponse(response, row, firstFailure));
      refresh();
    } catch { report.observer_errors += 1; }
  };

  async function drain() {
    if (disposed) return report;
    const deadline = Date.now() + 5000;
    try {
      while (pending.size) await bounded(Promise.allSettled([...pending]), deadline - Date.now());
      report.pending_tasks_drained = true;
    } catch { report.pending_tasks_drained = false; }
    refresh();
    report.pending_error_body_reads = pendingErrorBodies;
    const firstPlaying = report.records.find(entry => entry.route_kind === 'playing');
    report.coverage = {
      owned_universal_206_observed: report.universal_reference?.http_206_observed === true,
      first_playing_observed: Boolean(firstPlaying),
      first_playing_body_fields_observed: firstPlaying?.body_state === 'necessary_fields_observed',
      first_playing_failure_observed: [400, 404].includes(firstPlaying?.http_status),
      first_playing_error_code_observed: firstPlaying?.error_body?.state === 'error_code_observed' &&
        ['known', 'unknown'].includes(firstPlaying.error_body.error_code?.state),
      optional_progress_observed: report.records.some(entry => entry.route_kind === 'progress'),
      optional_stopped_observed: report.records.some(entry => entry.route_kind === 'stopped'),
      optional_metadata_errors: report.optional_metadata_errors,
    };
    report.primary_observation_complete = report.coverage.owned_universal_206_observed && report.coverage.first_playing_observed &&
      report.coverage.first_playing_body_fields_observed && report.coverage.first_playing_failure_observed &&
      report.coverage.first_playing_error_code_observed;
    // Missing or unequal identifiers are observations, not collection errors.
    // Optional token/device metadata and later reports are not primary gates.
    report.outcome = report.pending_tasks_drained && pendingErrorBodies === 0 && !report.record_overflow &&
      !report.observer_errors && report.primary_observation_complete
      ? 'diagnostic_observations_collected' : 'diagnostic_observations_incomplete';
    return report;
  }
  function clearPrivateValues() {
    cleared = true;
    for (const row of privateRows) {
      for (const group of [row.reference, row.body, row.tokens, row.devices]) {
        if (!group) continue;
        for (const value of Object.values(group)) if (value && Object.hasOwn(value, 'value')) value.value = null;
      }
      row.reference = null; row.body = null; row.tokens = null; row.devices = null;
    }
    owned.value = null; prefixedOwned.value = null; owned = null; prefixedOwned = null;
    privateRows = []; privateReferences = []; states = new WeakMap(); failedKinds.clear();
    report.raw_comparison_values_cleared = true;
  }
  async function dispose() {
    if (disposed) return report;
    accepting = false;
    let removed = true;
    try {
      try { context.off('request', onRequest); } catch { removed = false; report.observer_errors += 1; }
      try { context.off('response', onResponse); } catch { removed = false; report.observer_errors += 1; }
      await drain();
    } finally { clearPrivateValues(); disposed = true; report.listeners_removed = removed; }
    return report;
  }
  try {
    context.on('request', onRequest); context.on('response', onResponse);
  } catch {
    accepting = false;
    try { context.off('request', onRequest); } catch { /* Preserve the fixed attachment error. */ }
    try { context.off('response', onResponse); } catch { /* Preserve the fixed attachment error. */ }
    clearPrivateValues();
    throw Object.assign(new Error('audio_report_diagnostics_attach_failed'), { stack: undefined });
  }
  return Object.freeze({ report, drain, dispose });
}
