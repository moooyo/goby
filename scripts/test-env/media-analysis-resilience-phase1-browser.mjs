#!/usr/bin/env node
/** Actual native administration and compatibility consumers; no business mocks. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';

const phases = ['authentication', 'user-saved', 'user-query', 'specials-before', 'metadata-saved', 'specials-after',
  'sorting-saved', 'sorting-consumed', 'imports-disabled', 'imports-retained', 'imports-enabled', 'imports-consumed',
  'embedded-disabled', 'embedded-absent', 'embedded-enabled', 'embedded-present', 'fields', 'restart', 'persisted', 'cleanup'];
const result = { Marker: 'goby-media-analysis-phase1-browser-result-v1', RunId: '', Complete: false, Stages: [],
  Checks: Object.fromEntries(phases.map(phase => [phase, false])), PageErrors: 0, PageErrorDetails: [], ForeignRequests: 0,
  HTTP: [], Queries: [], Screenshots: [], RetiredGETReads: 0, FailurePhase: null, FailureOperation: null,
  ConsumerKind: 'declared account and catalog compatibility consumer', OriginalClientUsed: false,
  ArbitraryThirdPartyParityClaimed: false };
const fail = code => { const error = new Error(code); error.safeCode = code; throw error; };
const requireThat = (condition, code) => { if (!condition) fail(code); };
const sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
const sameIDs = (actual, expected) => JSON.stringify([...actual].sort()) === JSON.stringify([...expected].sort());
let fixture, browser, context, page, deadline, diagnosticPage;
let currentPhase = 'admission', currentOperation = 'read-context';
const clients = [];
let administrator, viewer, settingsRevision, seriesRevision, placementRevision, libraryRevision, musicLibraryRevision, newAudioId;
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
    if (error.phase1Captured) throw error;
    result.FailurePhase = currentPhase; result.FailureOperation = name;
    result.ErrorKind = ['Error', 'TimeoutError', 'AggregateError', 'TypeError'].includes(error.name) ? error.name : 'OperationError';
    if (!error.safeCode) error.safeCode = `native_${name.replaceAll('-', '_')}_failed`;
    await screenshot(`failure-${currentPhase}-${name}`, true);
    error.phase1Captured = true;
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
    requireThat(acknowledgement.Marker === 'goby-media-analysis-phase1-stage-database-v1' &&
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

async function consumer(name, password, id, kind) {
  const isolated = await guardedContext(), target = await isolated.newPage();
  clients.push({ context: isolated, page: target });
  await target.goto(`${fixture.BaseURL}/__media-analysis-phase1-consumer`);
  await target.evaluate(async input => {
    let token = '', last = null, imageURL = '';
    const history = [];
    const authorization = `MediaBrowser Client="Compatibility acceptance", Device="Owned browser", DeviceId="media-analysis-phase1-${input.Kind}", Version="1"`;
    async function request(pathname, method = 'GET', body, revision) {
      const endpoint = new URL(pathname, input.BaseURL);
      if (endpoint.origin !== input.BaseURL || endpoint.username || endpoint.password) throw new Error('foreign_consumer_target');
      const headers = { 'X-Emby-Authorization': authorization };
      if (token) headers['X-Emby-Token'] = token;
      if (body !== undefined) headers['Content-Type'] = 'application/json';
      if (revision !== undefined) headers['If-Match'] = `"${revision}"`;
      const response = await fetch(endpoint.href, { method, headers, credentials: 'omit', redirect: 'error',
        body: body === undefined ? undefined : JSON.stringify(body) });
      const text = await response.text();
      if (text.length > 2 * 1024 * 1024) throw new Error('consumer_response_limit');
      history.push({ Method: method, Path: endpoint.pathname, Status: response.status });
      return { Status: response.status, Body: text === '' ? null : JSON.parse(text), CacheControl: response.headers.get('cache-control'), ETag: response.headers.get('etag') };
    }
    window.gobyCompatibility = {
      request, history: () => history, last: () => last,
      async image(itemId) {
        const pathname = `/emby/Items/${encodeURIComponent(itemId)}/Images/Primary`;
        const response = await fetch(pathname, { headers: { 'X-Emby-Token': token, 'X-Emby-Authorization': authorization }, credentials: 'omit', redirect: 'error' });
        history.push({ Method: 'GET', Path: pathname, Status: response.status });
        if (response.status !== 200) { await response.arrayBuffer(); return { ItemId: itemId, Status: response.status }; }
        const bytes = await response.arrayBuffer();
        if (bytes.byteLength < 1 || bytes.byteLength > 8 * 1024 * 1024 || !response.headers.get('content-type')?.startsWith('image/')) throw new Error('artwork_response_invalid');
        const digest = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))).map(value => value.toString(16).padStart(2, '0')).join('');
        const picture = document.querySelector('#observed-artwork') ?? document.createElement('img');
        picture.id = 'observed-artwork'; picture.alt = 'Actual scanned audio artwork';
        if (!picture.parentNode) document.querySelector('main').append(picture);
        const nextURL = URL.createObjectURL(new Blob([bytes], { type: response.headers.get('content-type') }));
        picture.src = nextURL; await picture.decode();
        if (imageURL) URL.revokeObjectURL(imageURL); imageURL = nextURL;
        if (picture.naturalWidth < 1 || picture.naturalHeight < 1) throw new Error('artwork_not_decoded');
        return { ItemId: itemId, Status: response.status, SHA256: digest, Bytes: bytes.byteLength, Width: picture.naturalWidth, Height: picture.naturalHeight };
      },
      choices(rows) {
        const choices = document.querySelector('#choices'); choices.replaceChildren(); last = null;
        for (const row of rows) {
          const button = document.createElement('button'); button.textContent = row.Label;
          button.addEventListener('click', () => {
            void request(row.Target).then(response => {
              last = response;
              document.querySelector('#detail').textContent = JSON.stringify({ Status: response.Status,
                Id: response.Body?.Id, Name: response.Body?.Name, Type: response.Body?.Type,
                IsStandaloneSpecial: response.Body?.IsStandaloneSpecial, StatusValue: response.Body?.Status });
            }).catch(() => { last = { Status: 0 }; });
          });
          choices.append(button);
        }
      },
      async logout() { const response = await request('/emby/Sessions/Logout', 'POST'); token = ''; if (imageURL) { URL.revokeObjectURL(imageURL); imageURL = ''; } return response.Status; },
    };
    const login = await request('/emby/Users/AuthenticateByName', 'POST', { Username: input.Name, Pw: input.Password });
    if (login.Status !== 200 || login.Body?.User?.Id !== input.Id || !login.Body.AccessToken) throw new Error('consumer_login_failed');
    token = login.Body.AccessToken;
    document.querySelector('#status').textContent = 'Authenticated compatibility consumer';
  }, { BaseURL: fixture.BaseURL, Name: name, Password: password, Id: id, Kind: kind });
  return target;
}

async function api(target, route, method = 'GET', body, expected = 200, revision) {
  const response = await target.evaluate(args => window.gobyCompatibility.request(args.route, args.method, args.body, args.revision), { route, method, body, revision });
  requireThat(response.Status === expected, 'consumer_http_status');
  return response;
}
async function navigate(target, label, route, expectedId) {
  await target.evaluate(row => window.gobyCompatibility.choices([row]), { Label: label, Target: route });
  await target.getByRole('button', { name: label, exact: true }).click();
  await target.waitForFunction(id => window.gobyCompatibility.last()?.Body?.Id === id, expectedId, { timeout: 15000 });
  const response = await target.evaluate(() => window.gobyCompatibility.last());
  requireThat(response.Status === 200 && (await target.locator('#detail').textContent()).includes(expectedId), 'consumer_navigation_failed');
  return response.Body;
}
async function waitDisabled(button, code) {
  const until = Date.now() + 15000;
  while (Date.now() < until) { if (!await button.isEnabled()) return; await sleep(60); }
  fail(code);
}
async function saveUser() {
  await page.goto(`${fixture.BaseURL}/admin/users`);
  await page.getByRole('button', { name: `Manage ${fixture.EditedUserName}`, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Manage user', exact: true });
  const hidden = dialog.getByRole('checkbox', { name: 'Hide from sign-in screens', exact: true });
  await hidden.waitFor(); await hidden.check();
  await dialog.getByRole('checkbox', { name: 'Disable account', exact: true }).check();
  const saved = await responseFor('PUT', `/admin/v1/users/${fixture.EditedUserId}`, () => dialog.getByRole('button', { name: 'Save changes', exact: true }).click());
  requireThat(saved.Submitted.IsDisabled === true && saved.Submitted.Policy.IsHidden === true && typeof saved.Submitted.Revision === 'string', 'user_save_not_bound');
  await waitDisabled(dialog.getByRole('button', { name: 'Save changes', exact: true }), 'user_save_not_settled');
  await screenshot('native-account-query-flags');
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
}

async function inspectUsers() {
  const directory = fixture.UserDirectory.map(user => user.Id === fixture.EditedUserId ? { ...user, IsHidden: true, IsDisabled: true } : user);
  // These authored names contain only ordinary Latin letters and BMP Chinese;
  // their fixed C-collation order does not require a host locale oracle.
  const cases = [
    { Key: 'combined', Query: 'IsHidden=false&IsDisabled=false&NameStartsWithOrGreater=alpha&SortOrder=Descending&StartIndex=1&Limit=2', Hidden: false, Disabled: false, Bound: 'alpha', Desc: true, Start: 1, Limit: 2 },
    { Key: 'hidden-disabled', Query: 'IsHidden=true&IsDisabled=true', Hidden: true, Disabled: true },
    { Key: 'lower-bound', Query: 'NameStartsWithOrGreater=Bravo&SortOrder=aScEnDiNg', Bound: 'Bravo' },
    { Key: 'count-only', Query: 'Limit=0', Limit: 0 },
    { Key: 'beyond-end', Query: 'StartIndex=999', Start: 999 },
  ];
  const observations = [];
  for (const test of cases) {
    const filtered = directory.filter(user => (test.Hidden === undefined || user.IsHidden === test.Hidden) &&
      (test.Disabled === undefined || user.IsDisabled === test.Disabled) && (!test.Bound || user.Name.toLowerCase() >= test.Bound.toLowerCase()));
    filtered.sort((left, right) => {
      const a = `${left.Name.toLowerCase()}\0${left.Id}`, b = `${right.Name.toLowerCase()}\0${right.Id}`;
      return (a < b ? -1 : a > b ? 1 : 0) * (test.Desc ? -1 : 1);
    });
    const wanted = filtered.slice(test.Start ?? 0, (test.Start ?? 0) + (test.Limit ?? 100));
    const response = await api(administrator, `/emby/Users/Query?${test.Query}&x-emby-language=zh-CN`);
    requireThat(response.CacheControl === 'no-store' && response.Body.TotalRecordCount === filtered.length &&
      JSON.stringify(response.Body.Items.map(user => user.Id)) === JSON.stringify(wanted.map(user => user.Id)), 'user_filter_order_page_mismatch');
    const observed = { Key: test.Key, Ids: response.Body.Items.map(user => user.Id), TotalRecordCount: response.Body.TotalRecordCount };
    observations.push(observed); result.Queries.push(observed);
  }
  await api(viewer, '/emby/Users/Query?IsHidden=true', 'GET', undefined, 403);
  for (const query of ['IsHidden=1', 'SortOrder=sideways', 'Limit=-1', 'IsHidden=true&ishidden=false'])
    await api(administrator, `/emby/Users/Query?${query}`, 'GET', undefined, 400);
  await navigate(administrator, 'Open filtered account', `/emby/Users/${fixture.EditedUserId}`, fixture.EditedUserId);
  return observations;
}

const episodeRoute = query => `/emby/Users/${fixture.ViewerId}/Items?ParentId=${fixture.TelevisionLibraryId}&Recursive=true&IncludeItemTypes=Episode&Fields=IsStandaloneSpecial&${query}`;
async function inspectSpecials(after) {
  const yes = (await api(viewer, episodeRoute('IsStandaloneSpecial=true&SortBy=IndexNumber&Limit=20'))).Body;
  const expected = after ? [fixture.StandaloneSpecialId, fixture.PlacedSpecialId] : [fixture.StandaloneSpecialId];
  requireThat(yes.TotalRecordCount === expected.length && sameIDs(yes.Items.map(item => item.Id), expected) &&
    yes.Items.every(item => item.IsStandaloneSpecial === true && item.ParentIndexNumber === 0), 'standalone_special_projection');
  const no = (await api(viewer, episodeRoute('IsStandaloneSpecial=false&Limit=20'))).Body;
  const excluded = after ? [fixture.RegularEpisodeId] : [fixture.PlacedSpecialId, fixture.RegularEpisodeId];
  requireThat(no.TotalRecordCount === excluded.length && sameIDs(no.Items.map(item => item.Id), excluded), 'standalone_false_projection');
  const count = (await api(viewer, episodeRoute('IsStandaloneSpecial=true&Limit=0'))).Body;
  requireThat(count.TotalRecordCount === expected.length && count.Items.length === 0, 'standalone_count_before_page');
  const paged = (await api(viewer, episodeRoute('IsStandaloneSpecial=true&SortBy=IndexNumber&StartIndex=1&Limit=1'))).Body;
  requireThat(paged.TotalRecordCount === expected.length && sameIDs(paged.Items.map(item => item.Id), after ? [fixture.PlacedSpecialId] : []), 'standalone_filter_before_page');
  const detail = await navigate(viewer, 'Open selected special', `/emby/Users/${fixture.ViewerId}/Items/${fixture.PlacedSpecialId}`, fixture.PlacedSpecialId);
  requireThat(detail.IsStandaloneSpecial === after && detail.ParentIndexNumber === 0, 'placement_changed_physical_season');
  if (after) {
    const hints = (await api(viewer, `/emby/Search/Hints?UserId=${fixture.ViewerId}&SearchTerm=${encodeURIComponent(fixture.PlacedSpecialName)}&IncludeItemTypes=Episode`)).Body;
    const hint = hints.SearchHints?.find(value => value.Id === fixture.PlacedSpecialId && value.GobyReference?.Kind === 'Item');
    requireThat(hint && typeof hint.GobyNavigationUrl === 'string', 'special_search_navigation_missing');
    const searched = await navigate(viewer, 'Open searched special', hint.GobyNavigationUrl, fixture.PlacedSpecialId);
    requireThat(searched.IsStandaloneSpecial === true && searched.ParentIndexNumber === 0 && searched.AirsBeforeEpisodeNumber === 2, 'searched_special_placement_mismatch');
  }
  return expected;
}
async function metadataDialog(itemId, itemName) {
  await page.goto(`${fixture.BaseURL}/admin/libraries/${fixture.TelevisionLibraryId}/items`);
  const response = await responseFor('GET', `/admin/v1/items/${itemId}/metadata`, () => page.getByRole('button', { name: `Edit metadata for ${itemName}`, exact: true }).click());
  return { dialog: page.getByRole('dialog', { name: /^Edit metadata/ }), detail: response.Body };
}
async function saveMetadata() {
  let loaded = await metadataDialog(fixture.SeriesId, fixture.SeriesName);
  await loaded.dialog.getByRole('combobox', { name: 'Series status', exact: true }).click();
  await page.getByRole('option', { name: 'Ended', exact: true }).click();
  await loaded.dialog.locator('#metadata-EndDate-input').fill('2025-12-31');
  let saved = await responseFor('PUT', `/admin/v1/items/${fixture.SeriesId}/metadata`, () => loaded.dialog.getByRole('button', { name: 'Save changes', exact: true }).click());
  requireThat(saved.Submitted.Revision === loaded.detail.Revision && saved.Submitted.Overrides.Status === 'Ended' &&
    saved.Submitted.Overrides.EndDate === '2025-12-31T00:00:00Z', 'series_metadata_cas');
  seriesRevision = saved.Body.Revision;
  await waitDisabled(loaded.dialog.getByRole('button', { name: 'Save changes', exact: true }), 'series_save_not_settled');
  await loaded.dialog.getByRole('button', { name: 'Close', exact: true }).click();
  loaded = await metadataDialog(fixture.PlacedSpecialId, fixture.PlacedSpecialName);
  for (const [label, value] of [['Airs before season', '0'], ['Airs after season', '0'], ['Airs before episode', '2']])
    await loaded.dialog.getByRole('textbox', { name: label, exact: true }).fill(value);
  saved = await responseFor('PUT', `/admin/v1/items/${fixture.PlacedSpecialId}/metadata`, () => loaded.dialog.getByRole('button', { name: 'Save changes', exact: true }).click());
  requireThat(saved.Submitted.Revision === loaded.detail.Revision && saved.Submitted.Overrides.AirsBeforeSeasonNumber === 0 &&
    saved.Submitted.Overrides.AirsAfterSeasonNumber === 0 && saved.Body.Effective.ParentIndexNumber === 0, 'special_placement_cas');
  placementRevision = saved.Body.Revision;
  await waitDisabled(loaded.dialog.getByRole('button', { name: 'Save changes', exact: true }), 'placement_save_not_settled');
  await screenshot('native-special-placement');
  await loaded.dialog.getByRole('button', { name: 'Close', exact: true }).click();
}
async function waitScan(id) {
  requireThat(typeof id === 'string' && id.length > 0, 'scan_job_missing');
  const until = Date.now() + 90000;
  while (Date.now() < until) {
    const observed = await page.evaluate(async jobId => {
      const response = await fetch('/admin/v1/jobs', { credentials: 'same-origin', redirect: 'error' });
      if (response.status !== 200) throw new Error('native_job_observation_failed');
      const body = await response.json();
      const job = body.Items.find(value => value.Id === jobId);
      return job ? { Status: job.Status, Error: job.Error } : null;
    }, id);
    if (observed?.Status === 'Completed') { requireThat(!observed.Error, 'scan_completed_with_warning'); return id; }
    if (observed && ['Failed', 'Cancelled', 'Interrupted'].includes(observed.Status)) fail('actual_scan_failed');
    await sleep(200);
  }
  fail('actual_scan_timeout');
}
async function scanLibrary(id, name) {
  await page.goto(`${fixture.BaseURL}/admin/libraries`);
  const row = page.getByRole('listitem').filter({ has: page.getByRole('button', { name: `Edit library ${name}`, exact: true }) });
  const response = await responseFor('POST', `/admin/v1/libraries/${id}/scan`, () => row.getByRole('button', { name: 'Scan library', exact: true }).click(), 202);
  return waitScan(response.Body.Job.Id);
}

async function inspectMovieOrder(imported = false) {
  const response = (await api(viewer, `/emby/Items?UserId=${fixture.ViewerId}&ParentId=${fixture.MovieLibraryId}&Recursive=true&IncludeItemTypes=Movie&SortBy=SortName&SortOrder=Ascending&Limit=20`)).Body;
  const expected = imported ? [fixture.MovieBId, fixture.MovieAId] : [fixture.MovieAId, fixture.MovieBId];
  requireThat(response.TotalRecordCount === 2 && JSON.stringify(response.Items.map(item => item.Id)) === JSON.stringify(expected), 'saved_sorting_not_consumed');
  const item = response.Items.find(value => value.Id === fixture.MovieAId);
  requireThat(item.Name === (imported ? fixture.ImportedMovieName : fixture.MovieAName), 'local_import_setting_not_consumed');
  return expected;
}
async function saveSorting() {
  const loaded = await responseFor('GET', '/admin/v1/settings', () => page.goto(`${fixture.BaseURL}/admin/settings`));
  requireThat(Array.isArray(loaded.Body.Sorting.SortRemoveWords) && loaded.Body.Sorting.SortRemoveWords.length === 0, 'sorting_fixture_default_differs');
  const before = (await api(viewer, `/emby/Items?UserId=${fixture.ViewerId}&ParentId=${fixture.MovieLibraryId}&Recursive=true&IncludeItemTypes=Movie&SortBy=SortName`)).Body;
  requireThat(JSON.stringify(before.Items.map(item => item.Id)) === JSON.stringify([fixture.MovieBId, fixture.MovieAId]), 'sorting_baseline_order');
  await page.getByRole('textbox', { name: 'Words removed from sort names', exact: true }).fill('The');
  const saved = await responseFor('PUT', '/admin/v1/settings', () => page.getByRole('button', { name: 'Save settings', exact: true }).click());
  requireThat(saved.Submitted.Revision === loaded.Body.Revision && saved.Submitted.Sorting.SortRemoveWords.length === 1 &&
    saved.Submitted.Sorting.SortRemoveWords[0].toLowerCase() === 'the', 'sorting_save_not_current_cas');
  settingsRevision = saved.Body.Revision;
  requireThat(saved.Body.Sorting.SortRemoveWords.length === 1 && saved.Body.Sorting.SortRemoveWords[0].toLowerCase() === 'the', 'sorting_save_response');
  await waitDisabled(page.getByRole('button', { name: 'Save settings', exact: true }), 'settings_save_not_settled');
  await inspectMovieOrder(false);
  await screenshot('native-sorting-configuration');
}
async function setImports(enabled) {
  await page.goto(`${fixture.BaseURL}/admin/libraries`);
  const loaded = await responseFor('GET', `/admin/v1/libraries/${fixture.MovieLibraryId}`, () =>
    page.getByRole('button', { name: `Edit library ${fixture.MovieLibraryName}`, exact: true }).click());
  const dialog = page.getByRole('dialog', { name: 'Edit library', exact: true });
  await dialog.getByRole('checkbox', { name: 'Import local metadata files', exact: true }).setChecked(enabled);
  await dialog.getByRole('checkbox', { name: 'Scan after saving', exact: true }).check();
  const saved = await responseFor('PATCH', `/admin/v1/libraries/${fixture.MovieLibraryId}`, () => dialog.getByRole('button', { name: 'Save library', exact: true }).click());
  requireThat(saved.Submitted.Revision === loaded.Body.Library.Revision && saved.Submitted.LibraryOptions.EnableLocalMetadata === enabled &&
    saved.Submitted.Scan === true && !saved.Body.ScanError && saved.Body.Job?.Id, 'library_save_and_scan_contract');
  libraryRevision = saved.Body.Library.Revision;
  return waitScan(saved.Body.Job.Id);
}
async function musicEditor() {
  await page.goto(`${fixture.BaseURL}/admin/libraries`);
  const loaded = await responseFor('GET', `/admin/v1/libraries/${fixture.MusicLibraryId}`, () =>
    page.getByRole('button', { name: `Edit library ${fixture.MusicLibraryName}`, exact: true }).click());
  return { library: loaded.Body.Library, dialog: page.getByRole('dialog', { name: 'Edit library', exact: true }) };
}
async function compatibilityArtworkOptions(enabled) {
  const inventory = (await api(administrator, '/emby/Libraries/AvailableOptions')).Body;
  const audio = inventory.TypeOptions?.find(entry => entry.Type === 'Audio');
  requireThat(audio?.ImageFetchers?.some(fetcher => fetcher.Name === 'Goby Embedded Artwork' && fetcher.DefaultEnabled === true) &&
    audio.SupportedImageTypes?.includes('Primary'), 'embedded_fetcher_not_discovered');
  const directory = (await api(administrator, '/emby/Library/VirtualFolders/Query')).Body;
  const library = directory.Items?.find(entry => entry.ItemId === fixture.MusicLibraryId);
  const selected = library?.LibraryOptions?.TypeOptions?.find(entry => entry.Type === 'Audio');
  requireThat(library && library.Revision === musicLibraryRevision && Array.isArray(selected?.ImageFetchers) &&
    JSON.stringify(selected.ImageFetchers) === JSON.stringify(enabled ? ['Goby Embedded Artwork'] : []) &&
    !library.LibraryOptions.DisabledLocalMetadataReaders.includes('Nfo'), 'embedded_compatibility_readback');
  result.EmbeddedOptions ??= [];
  result.EmbeddedOptions.push({ Revision: library.Revision, Enabled: enabled, Fetcher: 'Goby Embedded Artwork', Discovery: true });
}
async function disableEmbeddedArtwork() {
  const loaded = await musicEditor();
  const control = loaded.dialog.getByRole('checkbox', { name: 'Extract embedded audio artwork', exact: true });
  requireThat(loaded.library.LibraryOptions.EnableEmbeddedArtwork === true && await control.isChecked() &&
    await loaded.dialog.getByRole('checkbox', { name: 'Import images from media directories', exact: true }).isChecked(), 'embedded_default_or_master');
  await control.uncheck();
  const saved = await responseFor('PATCH', `/admin/v1/libraries/${fixture.MusicLibraryId}`, () => loaded.dialog.getByRole('button', { name: 'Save library', exact: true }).click());
  requireThat(saved.Submitted.Revision === loaded.library.Revision && saved.Submitted.Scan === false &&
    saved.Submitted.LibraryOptions.EnableEmbeddedArtwork === false && saved.Body.Library.LibraryOptions.EnableLocalImages === true &&
    saved.Body.Library.LibraryOptions.EnableLocalMetadata === true, 'embedded_native_disable_changed_other_options');
  musicLibraryRevision = saved.Body.Library.Revision;
  await compatibilityArtworkOptions(false);
}
async function enableEmbeddedArtwork() {
  const response = await api(administrator, '/emby/Library/VirtualFolders/LibraryOptions', 'POST', {
    Id: fixture.MusicLibraryId, LibraryOptions: { TypeOptions: [{ Type: 'Audio', ImageFetchers: ['Goby Embedded Artwork'] }] },
  }, 204, musicLibraryRevision);
  requireThat(/^"[1-9][0-9]*"$/.test(response.ETag ?? ''), 'compatibility_library_revision_missing');
  const previous = musicLibraryRevision;
  musicLibraryRevision = response.ETag.slice(1, -1);
  requireThat(BigInt(musicLibraryRevision) > BigInt(previous), 'compatibility_library_revision_not_advanced');
  await compatibilityArtworkOptions(true);
  const loaded = await musicEditor();
  requireThat(loaded.library.Revision === musicLibraryRevision && loaded.library.LibraryOptions.EnableEmbeddedArtwork === true &&
    loaded.library.LibraryOptions.EnableLocalMetadata === true && loaded.library.LibraryOptions.EnableLocalImages === true &&
    await loaded.dialog.getByRole('checkbox', { name: 'Extract embedded audio artwork', exact: true }).isChecked() &&
    await loaded.dialog.getByRole('checkbox', { name: 'Import images from media directories', exact: true }).isChecked(), 'compatibility_save_not_visible_in_native_controls');
  await screenshot('native-compatible-embedded-artwork-option');
  await loaded.dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
}
async function inspectAudioArtwork(enabled) {
  const found = (await api(viewer, `/emby/Items?UserId=${fixture.ViewerId}&ParentId=${fixture.MusicLibraryId}&Recursive=true&IncludeItemTypes=Audio&SearchTerm=${encodeURIComponent(fixture.NewEmbeddedAudioName)}&Limit=20`)).Body;
  requireThat(found.TotalRecordCount === 1 && found.Items.length === 1 && found.Items[0].Name === fixture.NewEmbeddedAudioName &&
    typeof found.Items[0].Id === 'string' && (!newAudioId || newAudioId === found.Items[0].Id), 'new_audio_identity_changed');
  newAudioId = found.Items[0].Id;
  const Images = [];
  for (const id of [fixture.ExistingEmbeddedAudioId, fixture.SidecarAudioId, ...(enabled ? [newAudioId] : [])]) {
    const image = await viewer.evaluate(itemId => window.gobyCompatibility.image(itemId), id);
    requireThat(image.Status === 200 && /^[0-9a-f]{64}$/.test(image.SHA256) && image.Width > 0 && image.Height > 0 && image.Bytes > 0, 'actual_audio_artwork_decode_failed');
    Images.push(image);
  }
  if (!enabled) {
    const missing = await viewer.evaluate(itemId => window.gobyCompatibility.image(itemId), newAudioId);
    requireThat(missing.Status === 404, 'disabled_embedded_extraction_published_new_cover');
  }
  result.Artwork ??= [];
  result.Artwork.push({ Enabled: enabled, NewAudioId: newAudioId, Images });
  return { NewAudioId: newAudioId, Images };
}
async function inspectFields() {
  const route = `/emby/Items?UserId=${fixture.ViewerId}&Ids=${fixture.MovieAId}&fIeLdS=oVeRvIeW,mEdIaStReAmS,mEdIaSoUrCeS&x-emby-language=zh-CN`;
  const list = (await api(viewer, route)).Body;
  const item = list.Items?.[0];
  requireThat(list.TotalRecordCount === 1 && item?.Id === fixture.MovieAId && item.Overview === 'Changed local import' &&
    Array.isArray(item.MediaStreams) && item.MediaStreams.some(stream => stream.Type === 'Video') &&
    Array.isArray(item.MediaSources) && item.MediaSources.length === 1 &&
    Array.isArray(item.MediaSources[0].MediaStreams) && JSON.stringify(item.MediaSources[0].MediaStreams.map(stream => [stream.Index, stream.Type])) ===
      JSON.stringify(item.MediaStreams.map(stream => [stream.Index, stream.Type])), 'case_insensitive_list_fields');
  const detail = await navigate(viewer, 'Open projected movie', `/emby/Users/${fixture.ViewerId}/Items/${item.Id}?eXcLuDeFiElDs=mEdIaStReAmS&x-emby-language=zh-CN`, item.Id);
  requireThat(!Object.hasOwn(detail, 'MediaStreams') && Array.isArray(detail.MediaSources) && detail.MediaSources.length > 0 &&
    detail.MediaSources.every(source => !Object.hasOwn(source, 'MediaStreams')) && detail.Name === fixture.ImportedMovieName, 'detail_excluded_streams_restored');
  for (const field of ['Id', 'Name', 'Type', 'ServerId', 'IsFolder', 'MediaType']) requireThat(Object.hasOwn(detail, field), 'excluded_fields_removed_core_identity');
  const hints = (await api(viewer, `/emby/Search/Hints?UserId=${fixture.ViewerId}&SearchTerm=${encodeURIComponent(fixture.ImportedMovieName)}&IncludeItemTypes=Movie&x-emby-language=zh-CN`)).Body;
  const hint = hints.SearchHints?.find(value => value.Id === fixture.MovieAId && value.GobyReference?.Kind === 'Item');
  requireThat(hint && typeof hint.GobyNavigationUrl === 'string', 'search_current_imported_name_missing');
  const selected = await navigate(viewer, 'Open actual search result', hint.GobyNavigationUrl, fixture.MovieAId);
  requireThat(selected.Name === fixture.ImportedMovieName, 'search_navigation_source_mismatch');
  result.Fields = { Id: item.Id, ListMediaStreams: item.MediaStreams.length, ExcludedDetailMediaStreams: true,
    ExcludedNestedMediaStreams: true, SearchHintId: hint.Id, SearchDetailId: selected.Id, Language: 'zh-CN' };
  return result.Fields;
}
async function inspectPersisted() {
  const loaded = await responseFor('GET', '/admin/v1/settings', () => page.goto(`${fixture.BaseURL}/admin/settings`));
  requireThat(loaded.Body.Revision === settingsRevision && loaded.Body.Sorting.SortRemoveWords.length === 1 &&
    loaded.Body.Sorting.SortRemoveWords[0].toLowerCase() === 'the', 'restart_lost_sorting');
  requireThat((await page.getByRole('textbox', { name: 'Words removed from sort names', exact: true }).inputValue()).toLowerCase() === 'the', 'restart_native_sorting_controls');
  const detail = await metadataDialog(fixture.SeriesId, fixture.SeriesName);
  requireThat(detail.detail.Revision === seriesRevision && detail.detail.Effective.Status === 'Ended' &&
    detail.detail.Effective.EndDate.startsWith('2025-12-31'), 'restart_lost_series_metadata');
  await detail.dialog.getByRole('button', { name: 'Close', exact: true }).click();
  const placed = await metadataDialog(fixture.PlacedSpecialId, fixture.PlacedSpecialName);
  requireThat(placed.detail.Revision === placementRevision && placed.detail.Effective.ParentIndexNumber === 0 &&
    placed.detail.Effective.AirsBeforeSeasonNumber === 0 && placed.detail.Effective.AirsAfterSeasonNumber === 0 &&
    placed.detail.Effective.AirsBeforeEpisodeNumber === 2, 'restart_lost_native_placement');
  requireThat(await placed.dialog.getByRole('textbox', { name: 'Airs before season', exact: true }).inputValue() === '0' &&
    await placed.dialog.getByRole('textbox', { name: 'Airs after season', exact: true }).inputValue() === '0', 'restart_native_placement_controls');
  await placed.dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await page.goto(`${fixture.BaseURL}/admin/libraries`);
  const library = await responseFor('GET', `/admin/v1/libraries/${fixture.MovieLibraryId}`, () =>
    page.getByRole('button', { name: `Edit library ${fixture.MovieLibraryName}`, exact: true }).click());
  const editor = page.getByRole('dialog', { name: 'Edit library', exact: true });
  requireThat(library.Body.Library.Revision === libraryRevision && library.Body.Library.LibraryOptions.EnableLocalMetadata === true &&
    await editor.getByRole('checkbox', { name: 'Import local metadata files', exact: true }).isChecked(), 'restart_lost_native_import_option');
  await editor.getByRole('button', { name: 'Cancel', exact: true }).click();
  const music = await musicEditor();
  requireThat(music.library.Revision === musicLibraryRevision && music.library.LibraryOptions.EnableEmbeddedArtwork === true &&
    await music.dialog.getByRole('checkbox', { name: 'Extract embedded audio artwork', exact: true }).isChecked(), 'restart_lost_embedded_option');
  await music.dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await compatibilityArtworkOptions(true);
  const artwork = await inspectAudioArtwork(true);
  await inspectSpecials(true); const QueryIds = await inspectMovieOrder(true); await inspectUsers(); await inspectFields();
  return { SettingsRevision: loaded.Body.Revision, LibraryRevision: library.Body.Library.Revision,
    SeriesRevision: detail.detail.Revision, PlacementRevision: placed.detail.Revision, QueryIds,
    MusicLibraryRevision: music.library.Revision, ...artwork };
}
async function acknowledge(phase, extra = {}) {
  result.Checks[phase] = true;
  return stage(phase, extra);
}
async function main() {
  requireThat(process.platform === 'linux' && process.getuid() === 0, 'owned_linux_fixture_required');
  const contextPath = process.env.GOBY_MEDIA_ANALYSIS_PHASE1_CONTEXT;
  requireThat(typeof contextPath === 'string' && path.isAbsolute(contextPath), 'private_context_required');
  const directory = path.dirname(contextPath), stat = await fs.lstat(directory);
  requireThat(stat.isDirectory() && !stat.isSymbolicLink() && stat.uid === process.getuid() && (stat.mode & 0o777) === 0o700 && await fs.realpath(directory) === directory, 'private_directory_required');
  fixture = await privateJSON(contextPath);
  requireThat(fixture.Marker === 'goby-media-analysis-phase1-browser-fixture-v1' && fixture.RunId === process.env.GOBY_MEDIA_ANALYSIS_PHASE1_RUN_ID &&
    /^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$/.test(fixture.RunId), 'fixture_identity_mismatch');
  const origin = new URL(fixture.BaseURL);
  requireThat(origin.protocol === 'http:' && origin.hostname === '127.0.0.1' && origin.origin === fixture.BaseURL && Number(origin.port) > 1024 &&
    ![5432, 8096, 8920, 18196, 18198].includes(Number(origin.port)), 'owned_origin_required');
  requireThat(fixture.ArtifactsDir === directory && fixture.ResultPath === path.join(directory, 'browser-result.json') && Array.isArray(fixture.UserDirectory), 'artifact_binding_mismatch');
  const modulePath = process.env.GOBY_TEST_PLAYWRIGHT_MODULE;
  requireThat(typeof modulePath === 'string' && path.isAbsolute(modulePath), 'playwright_module_required');
  const { chromium } = createRequire(import.meta.url)(modulePath);
  browser = await chromium.launch({ headless: true });
  deadline = setTimeout(() => { result.DeadlineExceeded = true; void browser.close(); }, 540000);
  result.RunId = fixture.RunId;
  context = await guardedContext(); page = await context.newPage(); diagnosticPage = page;
  currentPhase = 'authentication';
  await operation('native-sign-in', nativeLogin);
  administrator = await operation('administrator-consumer-login', () => consumer(fixture.AdminName, fixture.AdminPassword, fixture.AdminId, 'administrator'));
  viewer = await operation('viewer-consumer-login', () => consumer(fixture.ViewerName, fixture.ViewerPassword, fixture.ViewerId, 'viewer'));
  await acknowledge(currentPhase);
  currentPhase = 'user-saved'; await operation('save-account-flags', saveUser); await acknowledge(currentPhase);
  currentPhase = 'user-query'; const UserQueries = await operation('consume-filtered-account-directory', inspectUsers); await acknowledge(currentPhase, { UserQueries });
  currentPhase = 'specials-before'; let QueryIds = await operation('consume-initial-special-placement', () => inspectSpecials(false)); await acknowledge(currentPhase, { QueryIds });
  currentPhase = 'metadata-saved'; await operation('edit-series-and-special-metadata', saveMetadata); await acknowledge(currentPhase, { SeriesRevision: seriesRevision, PlacementRevision: placementRevision });
  currentPhase = 'specials-after'; let ScanJobId = await operation('scan-tv-with-manual-placement', () => scanLibrary(fixture.TelevisionLibraryId, fixture.TelevisionLibraryName));
  QueryIds = await operation('consume-edited-special-placement', () => inspectSpecials(true)); await acknowledge(currentPhase, { ScanJobId, QueryIds });
  currentPhase = 'sorting-saved'; await operation('save-live-sorting-policy', saveSorting); await acknowledge(currentPhase, { SettingsRevision: settingsRevision });
  currentPhase = 'sorting-consumed'; ScanJobId = await operation('scan-with-saved-sorting', () => scanLibrary(fixture.MovieLibraryId, fixture.MovieLibraryName));
  QueryIds = await inspectMovieOrder(false); await acknowledge(currentPhase, { ScanJobId, QueryIds });
  currentPhase = 'imports-disabled'; ScanJobId = await operation('disable-import-and-scan', () => setImports(false)); await acknowledge(currentPhase, { ScanJobId, LibraryRevision: libraryRevision });
  currentPhase = 'imports-retained'; QueryIds = await operation('consume-retained-disabled-import', () => inspectMovieOrder(false)); await acknowledge(currentPhase, { ScanJobId, QueryIds });
  currentPhase = 'imports-enabled'; ScanJobId = await operation('enable-import-and-scan', () => setImports(true)); await acknowledge(currentPhase, { ScanJobId, LibraryRevision: libraryRevision });
  currentPhase = 'imports-consumed'; QueryIds = await operation('consume-updated-local-metadata', () => inspectMovieOrder(true)); await acknowledge(currentPhase, { ScanJobId, QueryIds });
  currentPhase = 'embedded-disabled'; await operation('disable-embedded-artwork-through-native-ui', disableEmbeddedArtwork); await acknowledge(currentPhase, { MusicLibraryRevision: musicLibraryRevision });
  currentPhase = 'embedded-absent'; ScanJobId = await operation('scan-new-audio-with-extraction-disabled', () => scanLibrary(fixture.MusicLibraryId, fixture.MusicLibraryName));
  let artwork = await operation('consume-retained-artwork-and-new-cover-absence', () => inspectAudioArtwork(false)); await acknowledge(currentPhase, { ScanJobId, ...artwork });
  currentPhase = 'embedded-enabled'; await operation('enable-discovered-fetcher-through-compatibility-api', enableEmbeddedArtwork); await acknowledge(currentPhase, { MusicLibraryRevision: musicLibraryRevision, NewAudioId: newAudioId });
  currentPhase = 'embedded-present'; ScanJobId = await operation('scan-enabled-embedded-artwork', () => scanLibrary(fixture.MusicLibraryId, fixture.MusicLibraryName));
  artwork = await operation('decode-new-and-retained-audio-covers', () => inspectAudioArtwork(true)); await acknowledge(currentPhase, { ScanJobId, ...artwork });
  currentPhase = 'fields'; const Fields = await operation('consume-projected-search-detail', inspectFields); await acknowledge(currentPhase, { Fields });
  currentPhase = 'restart'; await page.goto(`${fixture.BaseURL}/admin/`); await acknowledge(currentPhase);
  currentPhase = 'persisted'; const persisted = await operation('read-persisted-native-and-consumer-state', inspectPersisted); await screenshot('native-persisted-compatibility');
  await acknowledge(currentPhase, persisted);
  currentPhase = 'cleanup';
  await operation('normal-consumer-and-native-logout', async () => {
    for (const client of clients) {
      requireThat(await client.page.evaluate(() => window.gobyCompatibility.logout()) === 204, 'consumer_logout_failed');
      result.HTTP.push(...await client.page.evaluate(() => window.gobyCompatibility.history()));
      await client.context.close();
    }
    await page.goto(`${fixture.BaseURL}/admin/`);
    await responseFor('DELETE', '/admin/v1/session', () => page.getByRole('button', { name: 'Sign out', exact: true }).click(), 204, false);
    await page.getByRole('heading', { name: 'Sign in to Goby', exact: true }).waitFor();
    await context.close(); context = undefined;
  });
  await acknowledge(currentPhase);
  requireThat(result.PageErrors === 0 && result.ForeignRequests === 0 && phases.every(phase => result.Checks[phase]), 'phase1_browser_evidence_incomplete');
  result.Complete = true;
}

try { await main(); }
catch (error) { result.Complete = false; result.FailurePhase ??= currentPhase; result.FailureOperation ??= currentOperation;
  result.FailureCode = error.safeCode || 'browser_operation_failed'; process.exitCode = 1; }
finally {
  clearTimeout(deadline);
  for (const client of clients) {
    if (!client.page.isClosed()) await client.page.evaluate(() => window.gobyCompatibility?.logout()).catch(() => { result.ConsumerFallbackCleanupFailed = true; });
    await client.context.close().catch(() => { result.Complete = false; process.exitCode = 1; });
  }
  if (browser) await browser.close().catch(() => { result.Complete = false; process.exitCode = 1; });
  if (fixture?.ResultPath) await writeJSON(fixture.ResultPath, result).catch(() => { result.Complete = false; process.exitCode = 1; });
  process.stdout.write(`${JSON.stringify({ Marker: result.Marker, RunId: result.RunId, Complete: result.Complete,
    FailurePhase: result.FailurePhase, FailureOperation: result.FailureOperation, FailureCode: result.FailureCode ?? null })}\n`);
}
