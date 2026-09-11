/** Observe the owned album's auxiliary reads through original UI navigation. */

const ID = /^[a-f0-9]{32}$/;

export function auditOwnedItemIdentity(actual, expected) {
  const shape = actual !== null && typeof actual === 'object' && !Array.isArray(actual);
  const omitted = expected.pathOmitted === true;
  if (omitted && (expected.type !== 'MusicAlbum' || expected.scope !== 'auxiliary-album-owner' ||
      Object.hasOwn(expected, 'path'))) throw new Error('invalid_owned_item_path_contract');
  return {
    shape_valid: shape,
    id_matches: shape && actual.Id === expected.id,
    name_matches: shape && actual.Name === expected.name,
    path_matches: shape && (omitted ? !Object.hasOwn(actual, 'Path') : actual.Path === expected.path),
    type_matches: shape && (!expected.type || actual.Type === expected.type),
    parent_matches: shape && (!expected.parentId || actual.ParentId === expected.parentId),
  };
}

export async function runAuxiliaryAlbumUI({ page, context, report, snapshot, setActionPhase, requestObservation,
  target, album, albumId, libraryName, tracks }) {
  if (album !== 'M3e Synthetic Album' || !ID.test(albumId) || libraryName !== 'M3e Client Music' ||
      typeof setActionPhase !== 'function' || typeof requestObservation !== 'function' || !Array.isArray(tracks) || tracks.length !== 2 ||
      tracks.some(track => !ID.test(track.id) || !['M3e MP3', 'M3e FLAC'].includes(track.name)) ||
      new Set(tracks.map(track => track.id)).size !== 2 || new Set(tracks.map(track => track.name)).size !== 2) {
    throw new Error('auxiliary_album_identity_invalid');
  }
  const result = report.auxiliary_album = { outcome: 'in_progress', phase: 'home',
    scope: 'Original UI album navigation and auxiliary HTTP responses; no playback or history reset',
    expected_album_id: albumId, steps: [] };
  const step = value => { result.phase = value; setActionPhase(value); };
  step('auxiliary_home');
  await snapshot('auxiliary-home-before');
  const heading = page.locator('a.sectionTitleTextButton').filter({ hasText: `Latest ${libraryName}` }).filter({ visible: true });
  await heading.waitFor({ state: 'visible', timeout: 20000 });
  if (await heading.count() !== 1 || (await heading.innerText()).replace(/\ue5e1/g, '').trim() !== `Latest ${libraryName}`) {
    throw new Error('auxiliary_latest_music_ambiguous');
  }
  const section = heading.locator('xpath=ancestor::*[contains(concat(" ", normalize-space(@class), " "), " verticalSection ")][1]');
  if (await section.count() !== 1) throw new Error('auxiliary_latest_music_section_missing');
  const title = section.getByText(album, { exact: true }).filter({ visible: true });
  const control = title.locator('xpath=ancestor-or-self::*[(self::button or self::a or @role="button") and @data-action="link" and ancestor::*[contains(concat(" ", normalize-space(@class), " "), " card ") or contains(concat(" ", normalize-space(@class), " "), " cardBox ")]][1]').filter({ visible: true });
  await control.waitFor({ state: 'visible', timeout: 15000 });
  if (await control.count() !== 1) throw new Error('auxiliary_album_control_ambiguous');
  const identity = await control.evaluate(element => {
    const card = element.closest('.card');
    return { card_present: Boolean(card), id: card?.getAttribute('data-id'), type: card?.getAttribute('data-type') };
  });
  result.card = { card_present: identity.card_present, id_present: identity.id !== null,
    id_matches_receipt: identity.id === null ? null : identity.id === albumId,
    type: identity.type === null ? null : ['MusicAlbum', 'Audio', 'Folder'].includes(identity.type) ? identity.type : 'other' };
  // The recorded original client omits both attributes on its album card.
  // Present attributes must agree. Missing ones remain unknown until the
  // detail URL, exact track rows and Request objects establish the owner.
  if (!identity.card_present || identity.id !== null && identity.id !== albumId ||
      identity.type !== null && identity.type !== 'MusicAlbum') throw new Error('auxiliary_album_card_identity_differs');
  result.steps.push({ action: 'click_unique_album_title_in_latest_music', requested_album_id: albumId });
  const bound = [], beforeURL = page.url();
  const observe = request => {
    try {
      const url = new URL(request.url());
      if (url.origin !== target.origin || request.method() !== 'GET' ||
          !['Similar', 'ThemeMedia'].some(route => url.pathname === `/emby/Items/${albumId}/${route}` ||
            url.pathname === `/Items/${albumId}/${route}`)) return;
      const entry = requestObservation(request);
      if (!entry || bound.length >= 16) { result.request_binding_error = true; return; }
      bound.push(entry);
    } catch { result.request_binding_error = true; }
  };
  const responses = () => bound.map(entry => ({ request_index: entry.index, route: entry.route, status: entry.status,
    seed_matches_receipted_album: true, finished_elapsed_ms: entry.finished_elapsed_ms, failure: entry.failure ?? null,
    request_elapsed_ms: entry.elapsed_ms, response_elapsed_ms: entry.response_elapsed_ms,
    request_phase: entry.phase, response_phase: entry.response_phase }));
  context.on('request', observe);
  try {
  step('auxiliary_album_click');
  await control.click({ timeout: 8000 });
  step('auxiliary_album_wait');
  for (const track of tracks) {
    const row = page.locator(`.listItem[data-id="${track.id}"],[role="row"][data-id="${track.id}"]`).filter({ visible: true });
    await row.waitFor({ state: 'visible', timeout: 15000 });
    if (await row.count() !== 1 || await row.getByText(track.name, { exact: true }).filter({ visible: true }).count() !== 1) {
      throw new Error('auxiliary_album_track_row_identity_differs');
    }
  }
  const detail = new URL(page.url()), detailQuery = new URLSearchParams(detail.hash.split('?').slice(1).join('?'));
  const detailIDs = [...detailQuery].filter(([key]) => key.toLowerCase() === 'id').map(([, value]) => value);
  if (page.url() === beforeURL || detail.origin !== target.origin || detailIDs.length !== 1 || detailIDs[0] !== albumId) {
    throw new Error('auxiliary_album_navigation_unconfirmed');
  }
  result.detail = { navigation_observed: true, album_id_matches_receipt: true, visible_receipted_track_rows: tracks.length };
  const until = Date.now() + 10000;
    while (Date.now() < until && !(bound.every(entry => entry.failure || Number.isInteger(entry.finished_elapsed_ms)) &&
      ['Similar', 'ThemeMedia'].every(route => bound.some(entry => new RegExp(`/${route}/?$`, 'i').test(entry.route))))) {
      await page.waitForTimeout(100);
    }
  result.responses = responses();
  if (result.request_binding_error || !result.responses.some(entry => /\/Similar\/?$/i.test(entry.route) && entry.status === 200) ||
      !result.responses.some(entry => /\/ThemeMedia\/?$/i.test(entry.route) && Number.isInteger(entry.status)) ||
      result.responses.some(entry => entry.failure || !Number.isInteger(entry.finished_elapsed_ms) ||
        /\/Similar\/?$/i.test(entry.route) && entry.status !== 200)) {
    throw new Error('auxiliary_expected_responses_missing');
  }
  await snapshot('auxiliary-album-visible');
  step('auxiliary_return_home');
  let home = page.getByRole('link', { name: 'Home', exact: true }).filter({ visible: true });
  if (!await home.count()) home = page.getByRole('button', { name: 'Home', exact: true }).filter({ visible: true });
  if (await home.count() !== 1) throw new Error('auxiliary_home_control_ambiguous');
  await home.click({ timeout: 8000 });
  await heading.waitFor({ state: 'visible', timeout: 15000 });
  const location = new URL(page.url());
  if (location.origin !== target.origin || !['#!/home', '#/home', '#!/home.html', '#/home.html'].includes(location.hash.split('?')[0])) {
    throw new Error('auxiliary_home_navigation_unconfirmed');
  }
  await snapshot('auxiliary-home-after');
  result.return_home = { pathname: location.pathname === '/web/index.html' ? location.pathname : '{other path}',
    hash_route: location.hash.split('?')[0] };
  step('auxiliary_complete');
  result.outcome = 'completed';
  } finally { context.off('request', observe); result.responses = responses(); }
}
