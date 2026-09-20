#!/usr/bin/env node
/** Native roster administration and real adapter consumption on owned media. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';

const phases = ['authentication', 'preferences', 'roster-reviewed', 'roster-saved', 'missing-queries',
  'suggestions-played', 'music-navigation', 'music-mixes', 'search-navigation', 'audio-playing',
  'audio-stopped', 'physical-arrival', 'arrival-queries', 'restart', 'persisted', 'cleanup'];
const checks = ['Authentication', 'Preferences', 'RosterReviewed', 'RosterSaved', 'MissingQueries',
  'SuggestionsPlayed', 'MusicNavigation', 'MusicMixes', 'SearchNavigation', 'AudioPlaying',
  'AudioStopped', 'PhysicalArrival', 'ArrivalQueries', 'Restart', 'Persisted', 'Cleanup'];
const result = { Marker: 'goby-selected-phase3-browser-result-v1', RunId: '', Complete: false,
  Stages: [], Checks: Object.fromEntries(checks.map(value => [value, false])), PageErrors: 0,
  PageErrorDetails: [], ForeignRequests: 0, HTTP: [], Queries: [], MusicMixes: [], Screenshots: [],
  RetiredGETReads: 0, FailurePhase: null, FailureOperation: null, ConsumerKind: 'owned adapter test consumer',
  OriginalClientUsed: false, ArbitraryThirdPartyParityClaimed: false };
const fail = code => { const error = new Error(code); error.safeCode = code; throw error; };
const requireThat = (condition, code) => { if (!condition) fail(code); };
const sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
const ids = items => items.map(item => String(item.Id)).sort();
const sameIDs = (actual, expected) => JSON.stringify([...actual].sort()) === JSON.stringify([...expected].sort());
let fixture, browser, context, page, deadline, clientContext, clientPage, diagnosticPage;
let currentPhase = 'admission', currentOperation = 'read-context';
let roster, preferencesRevision, playbackScope, arrivedItemId;

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
    const target = diagnosticPage && !diagnosticPage.isClosed() ? diagnosticPage : page;
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
    if (error.phase3Captured) throw error;
    result.FailurePhase = currentPhase; result.FailureOperation = name;
    result.ErrorKind = ['Error', 'TimeoutError', 'AggregateError', 'TypeError'].includes(error.name) ? error.name : 'OperationError';
    if (!error.safeCode) error.safeCode = `native_${name.replaceAll('-', '_')}_failed`;
    await screenshot(`failure-${currentPhase}-${name}`, true);
    error.phase3Captured = true;
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
    requireThat(acknowledgement.Marker === 'goby-selected-phase3-stage-database-v1' &&
      acknowledgement.RunId === fixture.RunId && acknowledgement.Phase === phase, 'database_acknowledgement_binding_failed');
    if (acknowledgement.Failed === true) {
      result.DatabaseFailure = { Phase: phase, Code: 'database_stage_verification_failed' };
      fail('database_stage_verification_failed');
    }
    requireThat(acknowledgement.Complete === true && acknowledgement.ExpectedFilesVerified === true, 'database_acknowledgement_failed');
    result.Stages.push({ Phase: phase, State: 'complete' });
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

async function nativeLogin() {
  await page.goto(`${fixture.BaseURL}/admin/`);
  await page.getByRole('heading', { name: 'Sign in to Goby', exact: true }).waitFor();
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill(fixture.AdminName);
  await page.locator('input#account-password[name="Password"]').fill(fixture.AdminPassword);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('navigation', { name: 'Administration', exact: true }).waitFor();
}

async function preferencesDialog() {
  diagnosticPage = page;
  await page.goto(`${fixture.BaseURL}/admin/users`);
  await page.getByRole('button', { name: `Manage ${fixture.ViewerName}`, exact: true }).click();
  await page.getByRole('dialog', { name: 'Manage user', exact: true })
    .getByRole('button', { name: 'Playback and display preferences', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Playback and display preferences', exact: true });
  await dialog.getByRole('checkbox', { name: 'Display missing episodes', exact: true }).waitFor();
  return dialog;
}

async function waitEnabled(locator, code) {
  await locator.waitFor({ state: 'attached' });
  const until = Date.now() + 20000;
  while (Date.now() < until) {
    if (await locator.isEnabled()) return;
    await sleep(25);
  }
  fail(code);
}

async function openRoster() {
  diagnosticPage = page;
  await page.goto(`${fixture.BaseURL}/admin/libraries/${fixture.TelevisionLibraryId}/items`);
  const response = await responseFor('GET', `/admin/v1/series/${fixture.SeriesId}/episode-roster`, () =>
    page.getByRole('button', { name: `Episode roster for ${fixture.SeriesName}`, exact: true }).click());
  const dialog = page.getByRole('dialog', { name: 'Episode roster', exact: true });
  await dialog.getByRole('region', { name: 'Saved episode roster', exact: true }).waitFor();
  await waitEnabled(dialog.getByLabel('Import roster JSON', { exact: true }), 'native_roster_load_not_interactive');
  return { dialog, saved: response.Body };
}

async function installConsumer() {
  clientContext = await guardedContext();
  clientPage = await clientContext.newPage(); diagnosticPage = clientPage;
  const navigation = await clientPage.goto(`${fixture.BaseURL}/__selected-phase3-consumer`);
  requireThat(navigation?.status() === 200, 'owned_consumer_document_missing');
  const identity = await clientPage.evaluate(async input => {
    const device = `phase3-adapter-${input.RunId}`;
    const authorization = `MediaBrowser Client="Goby Phase 3 Acceptance", Device="Owned Chromium", DeviceId="${device}", Version="1"`;
    let token = '', selectedAudio = '', audioContext, analyser, playScope, imageURL;
    const audio = document.querySelector('#audio');
    const http = [], audioSamples = [];
    let lastNavigation = null, lastImage = null, audioFailure = '', audioStopped = null, playing = false;
    const fetchOwned = async (target, method = 'GET', body) => {
      const url = new URL(target, input.BaseURL);
      if (url.origin !== input.BaseURL || url.username || url.password || url.hash) throw new Error('adapter_origin_mismatch');
      const headers = { 'X-Emby-Authorization': authorization };
      if (token) headers['X-Emby-Token'] = token;
      if (body !== undefined) headers['Content-Type'] = 'application/json';
      const response = await fetch(url.href, { method, headers, body: body === undefined ? undefined : JSON.stringify(body),
        cache: 'no-store', signal: AbortSignal.timeout(20000) });
      if (http.length < 320) http.push({ Method: method, Path: url.pathname, Status: response.status });
      return response;
    };
    const request = async (target, method = 'GET', body) => {
      const response = await fetchOwned(target, method, body);
      const data = response.status === 204 ? null : await response.json();
      return { Status: response.status, Body: data };
    };
    const report = async (suffix, position) => {
      if (!playScope) throw new Error('actual_audio_scope_missing');
      const response = await request(`/emby/Sessions/Playing${suffix}`, 'POST', { PlaySessionId: playScope.PlaySessionId,
        ItemId: playScope.AudioItemId, MediaSourceId: playScope.MediaSourceId, PositionTicks: position,
        IsPaused: audio.paused, IsMuted: audio.muted, VolumeLevel: Math.round(audio.volume * 100),
        CanSeek: true, PlayMethod: 'Transcode', PlaybackRate: audio.playbackRate });
      if (response.Status !== 204) throw new Error('actual_audio_report_failed');
      return response.Status;
    };
    const sampleAudio = () => {
      const values = new Float32Array(analyser?.fftSize ?? 0);
      if (analyser) analyser.getFloatTimeDomainData(values);
      let sum = 0, peak = 0;
      for (const value of values) { sum += value * value; peak = Math.max(peak, Math.abs(value)); }
      return { At: performance.now(), Seconds: audio.currentTime, ReadyState: audio.readyState,
        Paused: audio.paused, Ended: audio.ended, Seeking: audio.seeking, ErrorCode: audio.error?.code ?? 0,
        AudioContextState: audioContext?.state ?? 'absent', AudioContextTime: audioContext?.currentTime ?? 0,
        RMS: values.length ? Math.sqrt(sum / values.length) : 0, Peak: peak, SampleCount: values.length };
    };
    const stopAudio = async () => {
      if (!playScope) return { NotStarted: true };
      if (audioStopped) return audioStopped;
      const position = Math.round(audio.currentTime * 1e7);
      audio.pause();
      const status = await report('/Stopped', position);
      audio.removeAttribute('src'); audio.load();
      const retired = await request(`/emby/Videos/ActiveEncodings?DeviceId=${encodeURIComponent(device)}&PlaySessionId=${encodeURIComponent(playScope.PlaySessionId)}`, 'DELETE');
      if (audioContext && audioContext.state !== 'closed') await audioContext.close();
      playing = false;
      audioStopped = { ...playScope, PositionTicks: position, StoppedStatus: status, EncodingsStatus: retired.Status,
        Paused: audio.paused, MediaDetached: !audio.getAttribute('src'), AudioContextState: audioContext?.state ?? 'absent' };
      return audioStopped;
    };
    document.querySelector('#play-audio').addEventListener('click', () => {
      void (async () => {
        if (!selectedAudio || playScope) throw new Error('audio_selection_invalid');
        // The actual click activates the browser audio output graph before
        // asynchronous negotiation. No oscillator or synthetic signal is used.
        audioContext = new AudioContext();
        analyser = audioContext.createAnalyser(); analyser.fftSize = 2048;
        audioContext.createMediaElementSource(audio).connect(analyser); analyser.connect(audioContext.destination);
        await audioContext.resume();
        const negotiated = await request(`/emby/Items/${encodeURIComponent(selectedAudio)}/PlaybackInfo`, 'POST', {
          UserId: input.ViewerId, StartTimeTicks: 0, EnableDirectPlay: false, EnableDirectStream: false,
          EnableTranscoding: true, AllowAudioStreamCopy: false, DeviceProfile: { TranscodingProfiles: [
            { Type: 'Audio', Container: 'mp3', AudioCodec: 'mp3', Protocol: 'http', Context: 'Streaming' },
          ] },
        });
        const body = negotiated.Body, source = body?.MediaSources?.[0];
        if (negotiated.Status !== 200 || body.ErrorCode || !body.PlaySessionId || body.MediaSources?.length !== 1 ||
          !source.SupportsTranscoding || source.TranscodingSubProtocol !== 'http' || source.TranscodingContainer !== 'mp3') throw new Error('audio_negotiation_failed');
        const url = new URL(source.TranscodingUrl, input.BaseURL);
        if (url.origin !== input.BaseURL || url.searchParams.get('PlaySessionId') !== body.PlaySessionId ||
          url.searchParams.get('DeviceId') !== device || url.searchParams.get('MediaSourceId') !== source.Id) throw new Error('audio_delivery_scope_mismatch');
        playScope = { PlaySessionId: body.PlaySessionId, DeviceId: device, MediaSourceId: source.Id, AudioItemId: selectedAudio };
        audio.muted = false; audio.volume = .5; audio.src = url.href;
        await audio.play(); await report('', 0); playing = true;
        document.querySelector('#status').textContent = 'Actual audio playback started';
      })().catch(error => { audioFailure = /^[a-z_]+$/.test(error.message) ? error.message : 'actual_audio_start_failed'; });
    });
    document.querySelector('#stop-audio').addEventListener('click', () => {
      void stopAudio().then(() => { document.querySelector('#status').textContent = 'Actual audio playback stopped'; })
        .catch(() => { audioFailure = 'actual_audio_stop_failed'; });
    });
    window.gobyPhase3 = {
      request,
      http: () => http.slice(),
      render(rows) {
        const target = document.querySelector('#choices'); target.replaceChildren(); lastNavigation = null; lastImage = null;
        for (const row of rows) {
          const button = document.createElement('button'); button.textContent = row.Label;
          button.addEventListener('click', () => {
            void (async () => {
              if (row.Image) {
                const response = await fetchOwned(row.Target);
                if (response.status !== 200 || !response.headers.get('content-type')?.startsWith('image/')) throw new Error('image_http_failed');
                const bytes = await response.arrayBuffer();
                if (bytes.byteLength < 1 || bytes.byteLength > 8 * 1024 * 1024) throw new Error('image_size_invalid');
                const digest = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))).map(value => value.toString(16).padStart(2, '0')).join('');
                if (imageURL) URL.revokeObjectURL(imageURL);
                imageURL = URL.createObjectURL(new Blob([bytes], { type: response.headers.get('content-type') }));
                const image = document.querySelector('#artwork'); image.src = imageURL; image.hidden = false; await image.decode();
                lastImage = { Complete: true, Status: response.status, SHA256: digest, Bytes: bytes.byteLength,
                  Width: image.naturalWidth, Height: image.naturalHeight, Path: new URL(row.Target, input.BaseURL).pathname };
              } else {
                const response = await request(row.Target);
                lastNavigation = response;
                const body = response.Body;
                // Render only safe identity labels, never source paths or tokens.
                document.querySelector('#detail').textContent = JSON.stringify({ Status: response.Status,
                  Id: body?.Id, Name: body?.Name, Type: body?.Type, Items: body?.Items?.map(item => ({ Id: item.Id, Name: item.Name, Type: item.Type })) });
              }
            })().catch(() => { lastNavigation = { Status: 0, Failed: true }; lastImage = { Complete: false }; });
          });
          target.append(button);
        }
      },
      lastNavigation: () => lastNavigation,
      lastImage: () => lastImage,
      selectAudio(id) { if (!input.AudioIds.includes(id)) throw new Error('audio_outside_owned_mix'); selectedAudio = id; },
      state: () => ({ Playing: playing, Failure: audioFailure, Scope: playScope, Sample: sampleAudio(), Stopped: audioStopped }),
      async audioObservation() {
        const until = performance.now() + 15000;
        while (performance.now() < until) {
          if (audioFailure) throw new Error(audioFailure);
          const sample = sampleAudio();
          if (playing && !sample.Paused && !sample.Seeking && sample.ReadyState >= 2 && sample.AudioContextState === 'running' &&
            sample.RMS > .0001 && sample.Peak > .001 && sample.Seconds > 1 && sample.ErrorCode === 0) audioSamples.push(sample);
          if (audioSamples.length >= 3 && sample.Seconds >= 2 && sample.Seconds - audioSamples[0].Seconds > .25) {
            const position = Math.round(sample.Seconds * 1e7);
            const progressStatus = await report('/Progress', position);
            return { ...playScope, PositionTicks: position, Samples: audioSamples.slice(-8), ProgressStatus: progressStatus,
              OutputConnectedToDestination: true, SyntheticSignalUsed: false };
          }
          await new Promise(resolve => setTimeout(resolve, 120));
        }
        throw new Error('actual_audio_clock_or_output_timeout');
      },
      stopAudio,
      async logout() {
        if (playScope && !audioStopped) await stopAudio();
        if (imageURL) { URL.revokeObjectURL(imageURL); imageURL = ''; }
        const response = await request('/emby/Sessions/Logout', 'POST'); token = ''; return response.Status;
      },
    };
    const login = await request('/emby/Users/AuthenticateByName', 'POST', { Username: input.ViewerName, Pw: input.ViewerPassword });
    if (login.Status !== 200 || login.Body.User?.Id !== input.ViewerId || !login.Body.AccessToken) throw new Error('adapter_authentication_failed');
    token = login.Body.AccessToken;
    return { UserId: input.ViewerId, DeviceId: device, Status: login.Status };
  }, { BaseURL: fixture.BaseURL, RunId: fixture.RunId, ViewerId: fixture.ViewerId, ViewerName: fixture.ViewerName,
    ViewerPassword: fixture.ViewerPassword, AudioIds: fixture.Music.map(track => track.TrackId) });
  result.AdapterAuthentication = identity;
}

async function adapter(target, method = 'GET', body, expected = 200) {
  diagnosticPage = clientPage;
  const response = await clientPage.evaluate(input => window.gobyPhase3.request(input.Target, input.Method, input.Body), { Target: target, Method: method, Body: body });
  requireThat(response.Status === expected, `adapter_http_${response.Status}_expected_${expected}`);
  result.Queries.push({ Phase: currentPhase, Method: method, Path: new URL(target, fixture.BaseURL).pathname,
    Status: response.Status, Count: response.Body?.TotalRecordCount ?? null });
  return response.Body;
}

async function clickAdapter(label, target, image = false) {
  diagnosticPage = clientPage;
  await clientPage.evaluate(row => window.gobyPhase3.render([row]), { Label: label, Target: target, Image: image });
  await clientPage.getByRole('button', { name: label, exact: true }).click();
  await clientPage.waitForFunction(isImage => isImage ? window.gobyPhase3.lastImage() !== null : window.gobyPhase3.lastNavigation() !== null, image, { timeout: 20000 });
  const response = await clientPage.evaluate(isImage => isImage ? window.gobyPhase3.lastImage() : window.gobyPhase3.lastNavigation(), image);
  requireThat(image ? response.Complete === true && response.Width > 0 && response.Height > 0 : response.Status === 200, 'adapter_click_through_failed');
  return image ? response : response.Body;
}

const userItems = () => `/emby/Users/${encodeURIComponent(fixture.ViewerId)}/Items`;
const seriesQuery = (id, extra = '') => `${userItems()}?ParentId=${encodeURIComponent(id)}&Recursive=true&IncludeItemTypes=Episode&Limit=100&EnableImages=false${extra}`;
const expectedMissing = () => roster.Entries.filter(entry => entry.Availability === 'missing').map(entry => entry.Id);
const canonical = value => JSON.stringify(value, (_key, child) => child && !Array.isArray(child) && typeof child === 'object'
  ? Object.fromEntries(Object.entries(child).sort(([a], [b]) => a.localeCompare(b))) : child);

function checkHidden(items, kind = 'Item') {
  const hidden = kind === 'Entity' ? fixture.HiddenEntityIds : fixture.HiddenItemIds;
  requireThat(items.every(item => !hidden.includes(String(item.Id))), 'denied_identity_disclosed');
}

async function inspectMissing(afterArrival = false) {
  const missingIDs = expectedMissing();
  const all = await adapter(seriesQuery(fixture.SeriesId));
  checkHidden(all.Items);
  requireThat(sameIDs(all.Items.filter(item => item.IsMissing === true).map(item => item.Id), missingIDs), 'missing_preference_projection_mismatch');
  const only = await adapter(seriesQuery(fixture.SeriesId, '&IsMissing=true'));
  requireThat(sameIDs(ids(only.Items), missingIDs) && only.TotalRecordCount === missingIDs.length, 'explicit_missing_query_mismatch');
  const physicalIDs = [fixture.InitialEpisodeId, ...(afterArrival ? [arrivedItemId] : [])];
  for (const flag of ['IsMissing=false', 'IsPlaceHolder=false']) {
    const physical = await adapter(seriesQuery(fixture.SeriesId, `&${flag}`));
    requireThat(sameIDs(ids(physical.Items), physicalIDs), 'explicit_missing_preference_override_failed');
  }
  const future = roster.Entries.find(entry => entry.Airing === 'unaired');
  const unknown = roster.Entries.find(entry => entry.Airing === 'unknown');
  requireThat(future && unknown && unknown.PremiereDate === undefined, 'independent_expected_airing_facts_missing');
  for (const flag of ['IsVirtualUnaired=true', 'IsUnaired=true']) {
    const unaired = await adapter(seriesQuery(fixture.SeriesId, `&${flag}`));
    requireThat(sameIDs(ids(unaired.Items), [future.Id]), 'future_fact_filter_mismatch');
  }
  const nonfuture = await adapter(seriesQuery(fixture.SeriesId, '&IsVirtualUnaired=false'));
  requireThat(nonfuture.Items.some(item => item.Id === unknown.Id) && !nonfuture.Items.some(item => item.Id === future.Id), 'unknown_air_date_was_invented');
  const absent = await adapter(seriesQuery(fixture.UnknownSeriesId, '&IsMissing=true'));
  requireThat(absent.TotalRecordCount === 0 && absent.Items.length === 0, 'numbering_gap_invented_missing_facts');
  for (const id of missingIDs) {
    const item = await adapter(`/emby/Users/${encodeURIComponent(fixture.ViewerId)}/Items/${encodeURIComponent(id)}`);
    requireThat(item.Id === id && item.IsMissing === true && item.IsPlaceHolder === true && item.LocationType === 'Virtual' &&
      item.CanDownload === false && item.SupportsResume === false && !Object.hasOwn(item, 'Path') &&
      !Object.hasOwn(item, 'UserData') && (!Object.hasOwn(item, 'MediaSources') || item.MediaSources.length === 0), 'virtual_episode_exposes_playback_source');
    await adapter(`/emby/Items/${encodeURIComponent(id)}/PlaybackInfo`, 'POST', { UserId: fixture.ViewerId }, 404);
  }
  const queue = await adapter(`/emby/Shows/${encodeURIComponent(fixture.SeriesId)}/Episodes?UserId=${encodeURIComponent(fixture.ViewerId)}&IsVirtualUnaired=false&IsMissing=false`);
  requireThat(sameIDs(ids(queue.Items), physicalIDs), 'virtual_fact_entered_physical_episode_queue');
  for (const target of [`/emby/Shows/NextUp?UserId=${encodeURIComponent(fixture.ViewerId)}&Limit=100`, `${userItems()}/Resume?Limit=100`]) {
    const response = await adapter(target);
    requireThat(response.Items.every(item => item.IsMissing !== true && !String(item.Id).startsWith('missing-')), 'virtual_fact_entered_playable_discovery');
    checkHidden(response.Items);
  }
  result.Missing = { MissingIds: missingIDs, UnknownDateId: unknown.Id, FutureId: future.Id,
    PhysicalQueueIds: ids(queue.Items), NoEvidenceMissingCount: absent.TotalRecordCount, AfterArrival: afterArrival };
  return { MissingIds: missingIDs, QueryItemIds: ids(all.Items) };
}

async function inspectSuggestions() {
  const target = `/emby/Users/${encodeURIComponent(fixture.ViewerId)}/Suggestions?Ids=${[fixture.SuggestionAId, fixture.SuggestionBId, fixture.HiddenItemIds[0]].map(encodeURIComponent).join(',')}&EnableImages=false`;
  const played = `/emby/Users/${encodeURIComponent(fixture.ViewerId)}/PlayedItems/${encodeURIComponent(fixture.SuggestionAId)}`;
  const before = await adapter(target);
  requireThat(sameIDs(ids(before.Items), [fixture.SuggestionAId, fixture.SuggestionBId]), 'initial_suggestions_mismatch');
  const marked = await adapter(played, 'POST'); requireThat(marked.Played === true, 'played_mutation_not_observed');
  const hiddenPlayed = await adapter(target); requireThat(sameIDs(ids(hiddenPlayed.Items), [fixture.SuggestionBId]), 'hide_played_preference_not_applied');
  for (const explicit of ['IsPlayed=true', 'Filters=IsPlayed']) {
    const overridden = await adapter(`${target}&${explicit}`);
    requireThat(sameIDs(ids(overridden.Items), [fixture.SuggestionAId]), 'explicit_suggestions_preference_override_failed');
  }
  const count = await adapter(`${target}&Limit=0`);
  requireThat(count.TotalRecordCount === 1 && count.Items.length === 0, 'suggestions_count_only_mismatch');
  const unmarked = await adapter(played, 'DELETE'); requireThat(unmarked.Played === false, 'unplayed_mutation_not_observed');
  const restored = await adapter(target); requireThat(sameIDs(ids(restored.Items), [fixture.SuggestionAId, fixture.SuggestionBId]), 'unplayed_transition_did_not_restore_suggestion');
  requireThat((await adapter(played, 'POST')).Played === true, 'final_played_fact_missing');
  result.Suggestions = { InitialIds: ids(before.Items), HiddenPlayedIds: ids(hiddenPlayed.Items), RestoredIds: ids(restored.Items),
    FinalPlayedItemId: fixture.SuggestionAId, CountOnly: count.TotalRecordCount };
}

async function inspectMusicNavigation() {
  const selected = fixture.Music[0];
  const artists = await adapter(`/emby/Artists?UserId=${encodeURIComponent(fixture.ViewerId)}&ArtistType=Artist,AlbumArtist&Limit=100`);
  checkHidden(artists.Items, 'Entity');
  requireThat(artists.Items.some(item => String(item.Id) === selected.ArtistId) && artists.Items.some(item => String(item.Id) === selected.AlbumArtistId), 'music_artist_relationships_missing');
  const artistAlbums = await clickAdapter(`Open albums by ${selected.ArtistName}`, `/emby/Items?UserId=${encodeURIComponent(fixture.ViewerId)}&ArtistIds=${encodeURIComponent(selected.ArtistId)}&IncludeItemTypes=MusicAlbum&Recursive=true&Limit=100`);
  const albumArtistAlbums = await clickAdapter(`Open albums credited to ${selected.AlbumArtistName}`, `/emby/Items?UserId=${encodeURIComponent(fixture.ViewerId)}&AlbumArtistIds=${encodeURIComponent(selected.AlbumArtistId)}&IncludeItemTypes=MusicAlbum&Recursive=true&Limit=100`);
  const expectedAlbums = fixture.Music.filter(track => track.ArtistId === selected.ArtistId).map(track => track.AlbumId);
  const expectedAlbumArtist = fixture.Music.filter(track => track.AlbumArtistId === selected.AlbumArtistId).map(track => track.AlbumId);
  requireThat(sameIDs(ids(artistAlbums.Items), expectedAlbums) && sameIDs(ids(albumArtistAlbums.Items), expectedAlbumArtist), 'artist_album_navigation_mismatch');
  const tracks = await clickAdapter(`Open tracks on ${selected.AlbumName}`, `/emby/Items?UserId=${encodeURIComponent(fixture.ViewerId)}&ParentId=${encodeURIComponent(selected.AlbumId)}&IncludeItemTypes=Audio&Recursive=true&Limit=100`);
  requireThat(sameIDs(ids(tracks.Items), [selected.TrackId]) && tracks.Items[0].Type === 'Audio', 'album_track_navigation_mismatch');
  result.MusicNavigation = { ArtistId: selected.ArtistId, AlbumArtistId: selected.AlbumArtistId, AlbumId: selected.AlbumId,
    TrackId: selected.TrackId, ArtistAlbumIds: ids(artistAlbums.Items), AlbumArtistAlbumIds: ids(albumArtistAlbums.Items) };
  return result.MusicNavigation;
}

async function inspectMixes() {
  const selected = fixture.Music[0], expected = fixture.Music.map(track => track.TrackId);
  const routes = [
    ['item-audio', `/emby/Items/${encodeURIComponent(selected.TrackId)}/InstantMix`],
    ['item-album', `/emby/Items/${encodeURIComponent(selected.AlbumId)}/InstantMix`],
    ['song', `/emby/Songs/${encodeURIComponent(selected.TrackId)}/InstantMix`],
    ['album', `/emby/Albums/${encodeURIComponent(selected.AlbumId)}/InstantMix`],
    ['artist', `/emby/Artists/InstantMix?Id=${encodeURIComponent(selected.ArtistId)}`],
    ['playlist', `/emby/Playlists/${encodeURIComponent(fixture.PlaylistId)}/InstantMix`],
    ['genre-id', `/emby/MusicGenres/InstantMix?Id=${encodeURIComponent(selected.GenreId)}`],
    ['genre-name', `/emby/MusicGenres/${encodeURIComponent(selected.GenreName)}/InstantMix`],
  ];
  for (const [kind, route] of routes) {
    const response = await adapter(`${route}${route.includes('?') ? '&' : '?'}UserId=${encodeURIComponent(fixture.ViewerId)}&Limit=100`);
    requireThat(response.Items.every(item => item.Type === 'Audio' && item.MediaType === 'Audio' && item.RunTimeTicks > 0) && sameIDs(ids(response.Items), expected) &&
      new Set(response.Items.map(item => item.Id)).size === response.Items.length && response.TotalRecordCount === expected.length,
    'mix_is_not_complete_authorized_playable_audio');
    if (['item-audio', 'song', 'album', 'item-album'].includes(kind)) requireThat(response.Items[0].Id === selected.TrackId, 'mix_seed_priority_missing');
    result.MusicMixes.push({ Kind: kind, ItemIds: response.Items.map(item => item.Id), TotalRecordCount: response.TotalRecordCount });
  }
  const count = await adapter(`/emby/Songs/${selected.TrackId}/InstantMix?UserId=${fixture.ViewerId}&Limit=0`);
  const limited = await adapter(`/emby/Songs/${selected.TrackId}/InstantMix?UserId=${fixture.ViewerId}&Limit=1`);
  requireThat(count.TotalRecordCount === expected.length && count.Items.length === 0 && limited.TotalRecordCount === expected.length &&
    limited.Items.length === 1 && limited.Items[0].Id === selected.TrackId, 'mix_count_or_limit_mismatch');
  await adapter(`/emby/Items/${encodeURIComponent(fixture.HiddenItemIds[0])}/InstantMix?UserId=${encodeURIComponent(fixture.ViewerId)}`, 'GET', undefined, 404);
  await clientPage.evaluate(id => window.gobyPhase3.selectAudio(id), selected.TrackId);
  return { MixTrackIds: expected, AudioItemId: selected.TrackId };
}

async function inspectSearch() {
  const response = await adapter(`/emby/Search/Hints?UserId=${encodeURIComponent(fixture.ViewerId)}&SearchTerm=${encodeURIComponent(fixture.SearchTerm)}&IncludeItemTypes=Audio,MusicAlbum,MusicArtist,Genre&Limit=100`);
  requireThat(response.TotalRecordCount === response.SearchHints.length && response.SearchHints.length > 0, 'search_hint_count_mismatch');
  for (const hint of response.SearchHints) {
    requireThat(['Item', 'Entity'].includes(hint.GobyReference?.Kind) && hint.GobyReference.Id === String(hint.Id) && hint.ItemId === String(hint.Id) &&
      !String(hint.Id).startsWith('missing-') && hint.Type !== 'Tag', 'search_hint_reference_invalid');
    checkHidden([hint], hint.GobyReference.Kind);
    const target = new URL(hint.GobyNavigationUrl, fixture.BaseURL);
    requireThat(target.origin === fixture.BaseURL && target.pathname.startsWith('/emby/') && !target.searchParams.has('api_key'), 'search_hint_navigation_escaped_scope');
  }
  const selected = fixture.Music[0];
  const physical = response.SearchHints.find(hint => hint.GobyReference.Kind === 'Item' && hint.Id === selected.TrackId);
  const entity = response.SearchHints.find(hint => hint.GobyReference.Kind === 'Entity' && String(hint.Id) === selected.ArtistId);
  requireThat(physical && entity && typeof physical.PrimaryImageUrl === 'string', 'expected_search_click_targets_missing');
  const physicalDetail = await clickAdapter(`Open result ${physical.Name}`, physical.GobyNavigationUrl);
  requireThat(physicalDetail.Id === selected.TrackId && physicalDetail.Type === 'Audio', 'physical_search_click_identity_mismatch');
  const entityDetail = await clickAdapter(`Open artist result ${entity.Name}`, entity.GobyNavigationUrl);
  requireThat(String(entityDetail.Id) === selected.ArtistId && entityDetail.Type === 'MusicArtist', 'typed_search_click_identity_mismatch');
  const image = await clickAdapter(`Load image for ${physical.Name}`, physical.PrimaryImageUrl, true);
  requireThat(/^[0-9a-f]{64}$/.test(image.SHA256), 'actual_search_image_hash_missing');
  const future = fixture.Roster.Entries.find(entry => entry.PremiereDate === '2099-01-01');
  const virtualSearch = await adapter(`/emby/Search/Hints?UserId=${fixture.ViewerId}&SearchTerm=${encodeURIComponent(future.Name)}&IncludeItemTypes=Episode&Limit=100`);
  requireThat(virtualSearch.TotalRecordCount === 0 && virtualSearch.SearchHints.length === 0, 'expected_episode_fact_entered_search_hints');
  result.Search = { HintCount: response.TotalRecordCount, PhysicalDetailId: physicalDetail.Id, EntityDetailId: String(entityDetail.Id),
    ReferenceKinds: [...new Set(response.SearchHints.map(hint => hint.GobyReference.Kind))], Image: image };
  return { PhysicalDetailId: physicalDetail.Id, EntityDetailId: String(entityDetail.Id), ImageSHA256: image.SHA256 };
}

async function main() {
  requireThat(process.platform === 'linux' && process.getuid() === 0, 'owned_linux_fixture_required');
  const contextPath = process.env.GOBY_SELECTED_PHASE3_CONTEXT;
  requireThat(typeof contextPath === 'string' && path.isAbsolute(contextPath), 'private_context_required');
  const directory = path.dirname(contextPath), stat = await fs.lstat(directory);
  requireThat(stat.isDirectory() && !stat.isSymbolicLink() && stat.uid === process.getuid() &&
    (stat.mode & 0o777) === 0o700 && await fs.realpath(directory) === directory, 'private_directory_required');
  fixture = await privateJSON(contextPath);
  requireThat(fixture.Marker === 'goby-selected-phase3-browser-fixture-v1' && fixture.RunId === process.env.GOBY_SELECTED_PHASE3_RUN_ID &&
    /^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$/.test(fixture.RunId), 'fixture_identity_mismatch');
  const origin = new URL(fixture.BaseURL);
  requireThat(origin.protocol === 'http:' && origin.hostname === '127.0.0.1' && origin.origin === fixture.BaseURL &&
    Number(origin.port) > 1024 && ![5432, 8096, 8920, 18196, 18198].includes(Number(origin.port)), 'owned_origin_required');
  requireThat(fixture.ArtifactsDir === directory && fixture.ResultPath === path.join(directory, 'browser-result.json'), 'artifact_binding_mismatch');
  for (const field of ['AdminName', 'AdminPassword', 'ViewerId', 'ViewerName', 'ViewerPassword', 'TelevisionLibraryId',
    'SeriesId', 'SeriesName', 'InitialEpisodeId', 'UnknownSeriesId', 'SuggestionAId', 'SuggestionBId', 'PlaylistId', 'SearchTerm'])
    requireThat(typeof fixture[field] === 'string' && fixture[field].length > 0 && fixture[field].length <= 4096, 'context_field_missing');
  requireThat(Array.isArray(fixture.Music) && fixture.Music.length === 3 && Array.isArray(fixture.HiddenItemIds) && fixture.HiddenItemIds.length > 0 &&
    Array.isArray(fixture.HiddenEntityIds) && fixture.Roster?.Entries?.length === 3, 'bounded_fixture_roster_and_music_required');
  for (const track of fixture.Music) for (const key of ['TrackId', 'TrackName', 'AlbumId', 'AlbumName', 'ArtistId', 'ArtistName', 'AlbumArtistId', 'AlbumArtistName', 'GenreId', 'GenreName'])
    requireThat(typeof track[key] === 'string' && track[key].length > 0, 'music_fixture_identity_missing');
  result.RunId = fixture.RunId;
  const modulePath = process.env.GOBY_TEST_PLAYWRIGHT_MODULE;
  requireThat(typeof modulePath === 'string' && path.isAbsolute(modulePath), 'playwright_module_required');
  const { chromium } = createRequire(import.meta.url)(modulePath);
  browser = await chromium.launch({ headless: true });
  deadline = setTimeout(() => { result.DeadlineExceeded = true; void browser.close(); }, 420000);
  context = await guardedContext(); page = await context.newPage(); diagnosticPage = page;

  currentPhase = 'authentication';
  await operation('native-administrator-sign-in', nativeLogin);
  result.Checks.Authentication = true; await stage(currentPhase);

  currentPhase = 'preferences';
  await operation('save-missing-and-suggestions-preferences', async () => {
    const dialog = await preferencesDialog();
    await dialog.getByRole('checkbox', { name: 'Display missing episodes', exact: true }).check();
    await dialog.getByRole('checkbox', { name: 'Hide played items from Suggestions', exact: true }).check();
    const response = await responseFor('PUT', `/admin/v1/users/${fixture.ViewerId}/preferences`, () =>
      dialog.getByRole('button', { name: 'Save preferences', exact: true }).click());
    requireThat(response.Body.Configuration.DisplayMissingEpisodes === true && response.Body.Configuration.HidePlayedInSuggestions === true &&
      response.Submitted.Configuration.DisplayMissingEpisodes === true && response.Submitted.Configuration.HidePlayedInSuggestions === true,
    'native_preferences_not_persisted');
    preferencesRevision = String(response.Body.Revision);
    requireThat(/^(?:0|[1-9]\d*)$/.test(preferencesRevision), 'preferences_revision_missing');
    await dialog.getByText('Preferences saved. New client requests use these settings.', { exact: true }).waitFor();
    await waitEnabled(dialog.getByRole('button', { name: 'Reload', exact: true }), 'native_preferences_save_not_settled');
    requireThat(!await dialog.getByRole('button', { name: 'Save preferences', exact: true }).isEnabled(), 'native_preferences_still_dirty_after_save');
    await screenshot('native-phase3-preferences');
  });
  result.Checks.Preferences = true; await stage(currentPhase, { PreferencesRevision: preferencesRevision });

  currentPhase = 'roster-reviewed';
  const editor = await operation('open-expected-episode-roster', openRoster);
  requireThat(editor.saved.Revision === '0' && editor.saved.State === 'absent' && editor.saved.Entries.length === 0, 'roster_fixture_not_initially_absent');
  await operation('import-and-review-explicit-episode-facts', async () => {
    await editor.dialog.getByLabel('Import roster JSON', { exact: true }).setInputFiles({ name: 'phase3-roster.json',
      mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(fixture.Roster)) });
    await editor.dialog.getByText('Imported roster ready for review. Save roster to replace the current expected episode list.', { exact: true }).waitFor();
    const preview = editor.dialog.getByRole('table', { name: 'Episode roster preview', exact: true });
    for (const entry of fixture.Roster.Entries) await preview.getByText(entry.Name, { exact: true }).waitFor();
    requireThat(await preview.locator('tbody tr').count() === fixture.Roster.Entries.length, 'roster_preview_differs_from_authored_facts');
    await screenshot('native-episode-roster-preview');
  });
  result.Checks.RosterReviewed = true; await stage(currentPhase, { SeriesId: fixture.SeriesId, LoadedRevision: editor.saved.Revision });

  currentPhase = 'roster-saved';
  await operation('save-roster-with-current-cas-revision', async () => {
    const response = await responseFor('PUT', `/admin/v1/series/${fixture.SeriesId}/episode-roster`, () =>
      editor.dialog.getByRole('button', { name: 'Save roster', exact: true }).click());
    requireThat(response.Submitted.Revision === editor.saved.Revision && canonical(response.Submitted.Source) === canonical(fixture.Roster.Source) &&
      canonical(response.Submitted.Entries) === canonical(fixture.Roster.Entries), 'roster_save_not_exact_reviewed_cas_input');
    roster = response.Body;
    requireThat(roster.SeriesId === fixture.SeriesId && roster.State === 'active' && BigInt(roster.Revision) > 0n && roster.Entries.length === 3 &&
      roster.Entries.every(entry => /^missing-[0-9a-f]{32}$/.test(entry.Id) && entry.Availability === 'missing' && entry.AvailableItemCount === 0 && entry.AvailableItemIds.length === 0),
    'accepted_roster_facts_differ');
    await editor.dialog.getByText('Episode roster saved. Availability reflects the current library catalog.', { exact: true }).waitFor();
    await screenshot('native-episode-roster-saved');
  });
  result.Roster = { Revision: roster.Revision, SourceKey: roster.Source.Key, SourceVersion: roster.Source.Revision,
    SourceSHA256: roster.Source.SHA256, Entries: roster.Entries };
  const originalMissingIds = expectedMissing();
  result.Checks.RosterSaved = true; await stage(currentPhase, { SeriesId: fixture.SeriesId, RosterRevision: roster.Revision, MissingIds: originalMissingIds });

  currentPhase = 'missing-queries';
  await operation('authenticate-owned-adapter-consumer', installConsumer);
  const missing = await operation('query-explicit-missing-and-unknown-facts', () => inspectMissing(false));
  result.Checks.MissingQueries = true; await stage(currentPhase, missing);

  currentPhase = 'suggestions-played';
  await operation('observe-played-transitions-and-preference-override', inspectSuggestions);
  result.Checks.SuggestionsPlayed = true; await stage(currentPhase, { PlayedItemId: fixture.SuggestionAId, UnplayedItemId: fixture.SuggestionBId });

  currentPhase = 'music-navigation';
  const music = await operation('click-artist-album-and-track-navigation', inspectMusicNavigation);
  result.Checks.MusicNavigation = true; await stage(currentPhase, music);

  currentPhase = 'music-mixes';
  const mixes = await operation('consume-every-supported-music-seed-family', inspectMixes);
  result.Checks.MusicMixes = true; await stage(currentPhase, mixes);

  currentPhase = 'search-navigation';
  const search = await operation('click-search-details-and-actual-image', inspectSearch);
  await screenshot('adapter-search-actual-image');
  result.Checks.SearchNavigation = true; await stage(currentPhase, search);

  currentPhase = 'audio-playing';
  await operation('play-real-audio-selected-from-mix', async () => {
    await clientPage.getByRole('button', { name: 'Play selected mix track', exact: true }).click();
    await clientPage.waitForFunction(() => window.gobyPhase3.state().Playing || window.gobyPhase3.state().Failure, null, { timeout: 30000 });
    requireThat((await clientPage.evaluate(() => window.gobyPhase3.state().Failure)) === '', 'actual_audio_start_failed');
    const audio = await clientPage.evaluate(() => window.gobyPhase3.audioObservation());
    requireThat(audio.AudioItemId === fixture.Music[0].TrackId && audio.ProgressStatus === 204 && audio.PositionTicks >= 20000000 &&
      audio.OutputConnectedToDestination === true && audio.SyntheticSignalUsed === false && audio.Samples.length >= 3, 'actual_audio_observation_incomplete');
    for (let index = 0; index < audio.Samples.length; index++) {
      const value = audio.Samples[index], previous = audio.Samples[index - 1];
      requireThat(!value.Paused && !value.Seeking && value.ReadyState >= 2 && value.AudioContextState === 'running' && value.ErrorCode === 0 &&
        value.RMS > .0001 && value.Peak > .001 && value.SampleCount === 2048 && (!previous ||
          value.Seconds > previous.Seconds && value.AudioContextTime > previous.AudioContextTime && value.At > previous.At), 'decoded_audio_output_or_clock_did_not_advance');
    }
    result.AudioPlaying = audio;
    playbackScope = { PlaySessionId: audio.PlaySessionId, DeviceId: audio.DeviceId, MediaSourceId: audio.MediaSourceId,
      AudioItemId: audio.AudioItemId, PositionTicks: audio.PositionTicks };
    await screenshot('adapter-actual-audio-playback');
  });
  result.Checks.AudioPlaying = true; await stage(currentPhase, playbackScope);

  currentPhase = 'audio-stopped';
  await operation('stop-and-retire-actual-audio-playback', async () => {
    await clientPage.getByRole('button', { name: 'Stop playback', exact: true }).click();
    await clientPage.waitForFunction(() => window.gobyPhase3.state().Stopped || window.gobyPhase3.state().Failure, null, { timeout: 30000 });
    const state = await clientPage.evaluate(() => window.gobyPhase3.state());
    requireThat(!state.Failure && state.Stopped.StoppedStatus === 204 && state.Stopped.EncodingsStatus === 204 &&
      state.Stopped.MediaDetached && state.Stopped.Paused && state.Stopped.AudioContextState === 'closed' &&
      state.Stopped.PlaySessionId === playbackScope.PlaySessionId && state.Stopped.PositionTicks >= playbackScope.PositionTicks,
    'actual_audio_stop_not_complete');
    result.AudioStopped = state.Stopped; playbackScope.PositionTicks = state.Stopped.PositionTicks;
  });
  result.Checks.AudioStopped = true; await stage(currentPhase, playbackScope);

  currentPhase = 'physical-arrival';
  const arrival = await stage(currentPhase);
  requireThat(typeof arrival.ArrivedItemId === 'string' && arrival.ArrivedItemId.length > 0 && !arrival.ArrivedItemId.startsWith('missing-'), 'physical_arrival_identity_missing');
  arrivedItemId = arrival.ArrivedItemId; result.Checks.PhysicalArrival = true;

  currentPhase = 'arrival-queries';
  const arrivedRoster = await operation('review-actual-arrival-in-native-roster', openRoster);
  requireThat(arrivedRoster.saved.Revision === roster.Revision, 'physical_arrival_changed_roster_source_cas');
  roster = arrivedRoster.saved;
  const arrivedFact = roster.Entries.find(entry => entry.AvailableItemIds.includes(arrivedItemId));
  requireThat(arrivedFact?.Availability === 'available' && arrivedFact.AvailableItemCount === 1 && expectedMissing().length === 2, 'arrival_not_bound_to_explicit_expected_fact');
  await operation('query-physical-arrival-and-retired-virtual-id', async () => {
    await inspectMissing(true);
    const retired = originalMissingIds.find(id => !expectedMissing().includes(id));
    requireThat(retired === arrivedFact.Id, 'arrival_retired_the_wrong_virtual_identity');
    await adapter(`/emby/Users/${fixture.ViewerId}/Items/${retired}`, 'GET', undefined, 404);
    const physical = await adapter(`/emby/Users/${fixture.ViewerId}/Items/${arrivedItemId}`);
    requireThat(physical.Id === arrivedItemId && physical.Type === 'Episode' && physical.IsMissing !== true, 'arrival_detail_is_not_physical');
  });
  diagnosticPage = page; await screenshot('native-roster-physical-arrival');
  result.Checks.ArrivalQueries = true; await stage(currentPhase, { ArrivedItemId: arrivedItemId, RemainingMissingIds: expectedMissing() });

  currentPhase = 'restart';
  await page.goto(`${fixture.BaseURL}/admin/`);
  await stage(currentPhase); result.Checks.Restart = true;

  currentPhase = 'persisted';
  await operation('read-persisted-native-preferences-and-roster', async () => {
    const dialog = await preferencesDialog();
    requireThat(await dialog.getByRole('checkbox', { name: 'Display missing episodes', exact: true }).isChecked() &&
      await dialog.getByRole('checkbox', { name: 'Hide played items from Suggestions', exact: true }).isChecked(), 'restart_lost_native_preferences');
    const saved = await openRoster();
    requireThat(saved.saved.Revision === roster.Revision && canonical(saved.saved.Entries) === canonical(roster.Entries) &&
      saved.saved.Source.SHA256 === roster.Source.SHA256, 'restart_changed_roster_facts_or_arrival');
    await screenshot('native-phase3-persisted-roster');
  });
  await operation('read-persisted-adapter-projections', async () => {
    await inspectMissing(true);
    const suggestions = await adapter(`/emby/Users/${fixture.ViewerId}/Suggestions?Ids=${fixture.SuggestionAId},${fixture.SuggestionBId}&EnableImages=false`);
    requireThat(sameIDs(ids(suggestions.Items), [fixture.SuggestionBId]), 'restart_lost_played_preference_projection');
    const mix = await adapter(`/emby/Songs/${fixture.Music[0].TrackId}/InstantMix?UserId=${fixture.ViewerId}&Limit=100`);
    requireThat(sameIDs(ids(mix.Items), fixture.Music.map(track => track.TrackId)), 'restart_changed_music_seed_sources');
  });
  result.Checks.Persisted = true; await stage(currentPhase, { RosterRevision: roster.Revision, RemainingMissingIds: expectedMissing() });

  currentPhase = 'cleanup';
  await operation('adapter-and-native-sign-out', async () => {
    requireThat(await clientPage.evaluate(() => window.gobyPhase3.logout()) === 204, 'adapter_logout_failed');
    result.AdapterHTTP = await clientPage.evaluate(() => window.gobyPhase3.http());
    await clientContext.close(); clientContext = undefined; diagnosticPage = page;
    await page.goto(`${fixture.BaseURL}/admin/`);
    await page.getByRole('navigation', { name: 'Administration', exact: true }).waitFor();
    await responseFor('DELETE', '/admin/v1/session', () => page.getByRole('button', { name: 'Sign out', exact: true }).click(), 204, false);
    await page.getByRole('heading', { name: 'Sign in to Goby', exact: true }).waitFor();
    await context.close(); context = undefined;
  });
  await stage(currentPhase); result.Checks.Cleanup = true;
  requireThat(result.PageErrors === 0 && result.ForeignRequests === 0 && checks.every(key => result.Checks[key]), 'phase3_browser_evidence_incomplete');
  result.Complete = true;
}

try { await main(); }
catch (error) { result.Complete = false; result.FailurePhase ??= currentPhase; result.FailureOperation ??= currentOperation;
  result.FailureCode = error.safeCode || 'browser_operation_failed'; process.exitCode = 1; }
finally {
  clearTimeout(deadline);
  if (clientPage && !clientPage.isClosed()) {
    await clientPage.evaluate(() => window.gobyPhase3?.logout()).catch(() => { result.AdapterFallbackCleanupFailed = true; });
  }
  if (clientContext) await clientContext.close().catch(() => { result.Complete = false; process.exitCode = 1; });
  if (browser) await browser.close().catch(() => { result.Complete = false; process.exitCode = 1; });
  if (fixture?.ResultPath) await writeJSON(fixture.ResultPath, result).catch(() => { result.Complete = false; process.exitCode = 1; });
  process.stdout.write(`${JSON.stringify({ Marker: result.Marker, RunId: result.RunId, Complete: result.Complete,
    FailurePhase: result.FailurePhase, FailureOperation: result.FailureOperation, FailureCode: result.FailureCode ?? null })}\n`);
}
