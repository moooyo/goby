import { randomBytes } from 'node:crypto';
import { closeSync, constants, existsSync, fstatSync, fsyncSync, lstatSync, openSync, readFileSync, realpathSync, renameSync, unlinkSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { expect, request, test } from '@playwright/test';
import type { APIRequestContext, APIResponse, Locator, Page, Response } from '@playwright/test';
import type { FeatureInfo, ManagedUser, ManagementSettings, MetadataItemsResponse, ServerSettings, TaskDefinitionsResponse, TaskRunDetail } from '../src/api';
import type { CollectionMember, CollectionPage, ManagedCollection } from '../src/collectionsApi';

interface Fixture {
  Marker: string;
  RunId: string;
  BaseURL: string;
  AdminName: string;
  AdminPassword: string;
  MemberId: string;
  MemberName: string;
  MemberPassword: string;
  LibraryId: string;
  MovieIds: string[];
  PlaylistSeedName?: string;
  ArtifactsDir: string;
  ResultPath: string;
}

type Phase = 'authentication' | 'playlist' | 'collection' | 'user-policy' | 'management' | 'cache' | 'cleanup';
interface Stage { Phase: Phase; State: 'complete'; Ids: string[]; Counts: Record<string, number> }
interface BrowserResult {
  Marker: 'goby-feature-wave-browser-result-v1';
  RunId: string;
  Complete: boolean;
  PlaylistId: string;
  CollectionId: string;
  CacheRunId: string;
  Checks: {
    PlaylistLifecycle: boolean;
    CollectionLifecycle: boolean;
    PolicyRestrictions: boolean;
    ManagementPersistence: boolean;
    GlobalCacheRun: boolean;
    NoPageErrors: boolean;
    NoOnlineProviderRequests: boolean;
    LogoutCompleted: boolean;
  };
  Stages: Stage[];
}

function privateDirectory(directory: string): void {
  const stat = lstatSync(directory);
  if (!path.isAbsolute(directory) || directory !== path.resolve(directory) || realpathSync(directory) !== directory
    || !stat.isDirectory() || stat.isSymbolicLink() || stat.uid !== process.getuid?.() || (stat.mode & 0o777) !== 0o700) {
    throw new Error('Feature-wave artifacts require a private directory owned by the isolated runner.');
  }
}

function privateJSON<T>(filename: string, maximumBytes = 128 * 1024): T {
  const descriptor = openSync(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = fstatSync(descriptor);
    if (!stat.isFile() || stat.uid !== process.getuid?.() || stat.nlink !== 1 || (stat.mode & 0o777) !== 0o600
      || stat.size < 1 || stat.size > maximumBytes || realpathSync(`/proc/self/fd/${descriptor}`) !== filename) {
      throw new Error('Feature-wave input must remain a bounded private regular file.');
    }
    try { return JSON.parse(readFileSync(descriptor, 'utf8')) as T; }
    catch { throw new Error('Feature-wave input is not valid JSON.'); }
  } finally { closeSync(descriptor); }
}

function loadFixture(): Fixture {
  const filename = process.env.GOBY_FEATURE_WAVE_BROWSER_CONTEXT;
  if (process.platform !== 'linux' || !filename || !path.isAbsolute(filename) || filename !== path.resolve(filename)) {
    throw new Error('The feature-wave browser journey requires its isolated Linux fixture.');
  }
  privateDirectory(path.dirname(filename));
  const fixture = privateJSON<Fixture>(filename, 32 * 1024);
  let origin: URL;
  try { origin = new URL(fixture.BaseURL); } catch { throw new Error('The feature-wave origin is invalid.'); }
  const identifier = /^[0-9a-f]{32}$/;
  if (fixture.Marker !== 'goby-feature-wave-browser-fixture-v1'
    || !/^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$/.test(fixture.RunId)
    || fixture.RunId !== process.env.GOBY_FEATURE_WAVE_RUN_ID
    || origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || !origin.port
    || ['5432', '15432', '18096', '18097'].includes(origin.port)
    || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash
    || ![fixture.AdminName, fixture.AdminPassword, fixture.MemberName, fixture.MemberPassword].every((value) => typeof value === 'string' && value.length > 0)
    || !identifier.test(fixture.MemberId) || !identifier.test(fixture.LibraryId)
    || !Array.isArray(fixture.MovieIds) || fixture.MovieIds.length !== 2 || !fixture.MovieIds.every((id) => identifier.test(id))
    || new Set(fixture.MovieIds).size !== 2
    || typeof fixture.ArtifactsDir !== 'string' || typeof fixture.ResultPath !== 'string'
    || !path.isAbsolute(fixture.ResultPath) || fixture.ResultPath !== path.resolve(fixture.ResultPath)
    || path.dirname(fixture.ResultPath) !== fixture.ArtifactsDir || path.extname(fixture.ResultPath) !== '.json'
    || fixture.ResultPath === filename
    || (fixture.PlaylistSeedName !== undefined && (typeof fixture.PlaylistSeedName !== 'string' || !fixture.PlaylistSeedName || fixture.PlaylistSeedName.length > 180))) {
    throw new Error('The feature-wave fixture does not describe a fresh disposable run.');
  }
  privateDirectory(fixture.ArtifactsDir);
  if (existsSync(fixture.ResultPath)) throw new Error('The feature-wave result already exists.');
  fixture.BaseURL = origin.origin;
  return fixture;
}

function writePrivateJSON(filename: string, value: unknown, secrets: string[]): void {
  const encoded = `${JSON.stringify(value, null, 2)}\n`;
  if (secrets.some((secret) => secret && encoded.includes(secret))) throw new Error('Sensitive values cannot be written to browser artifacts.');
  if (existsSync(filename)) throw new Error('The browser artifact already exists.');
  const temporary = path.join(path.dirname(filename), `.${path.basename(filename)}.${randomBytes(8).toString('hex')}.tmp`);
  const descriptor = openSync(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try {
    try {
      const stat = fstatSync(descriptor);
      if (!stat.isFile() || stat.uid !== process.getuid?.() || stat.nlink !== 1 || (stat.mode & 0o777) !== 0o600
        || realpathSync(`/proc/self/fd/${descriptor}`) !== temporary) throw new Error('The browser artifact must remain private.');
      writeFileSync(descriptor, encoded, 'utf8');
      fsyncSync(descriptor);
    } finally { closeSync(descriptor); }
    if (existsSync(filename)) throw new Error('The browser artifact appeared before publication.');
    renameSync(temporary, filename);
  } finally { if (existsSync(temporary)) unlinkSync(temporary); }
}

async function checkpoint(fixture: Fixture, result: BrowserResult, stage: Stage, secrets: string[]): Promise<void> {
  result.Stages.push(stage);
  writePrivateJSON(path.join(fixture.ArtifactsDir, `stage-${stage.Phase}-browser.json`), {
    Marker: 'goby-feature-wave-browser-stage-v1', RunId: fixture.RunId, ...stage,
  }, secrets);
  writePrivateJSON(path.join(fixture.ArtifactsDir, `stage-${stage.Phase}-request.json`), { RunId: fixture.RunId, Phase: stage.Phase }, secrets);
  const databasePath = path.join(fixture.ArtifactsDir, `stage-${stage.Phase}-database.json`);
  await expect.poll(() => {
    if (!existsSync(databasePath)) return false;
    const snapshot = privateJSON<{ RunId?: string; Phase?: string; Complete?: boolean; SourcesUnchanged?: boolean }>(databasePath);
    if (snapshot.RunId !== fixture.RunId || snapshot.Phase !== stage.Phase || snapshot.Complete !== true || snapshot.SourcesUnchanged !== true) {
      throw new Error('The database checkpoint must confirm this completed stage and unchanged source files.');
    }
    return true;
  }, { timeout: 60_000, intervals: [100, 250, 500], message: `The ${stage.Phase} database snapshot must finish before the next stage.` }).toBe(true);
}

async function payload<T>(response: APIResponse | Response, expectedStatus = 200): Promise<T> {
  expect(response.status(), 'The live API returned an unexpected status.').toBe(expectedStatus);
  try { return await response.json() as T; }
  catch { throw new Error('The live API returned invalid JSON.'); }
}

function responseFor(page: Page, method: string, pathname: string): Promise<Response> {
  return page.waitForResponse((response) => response.request().method() === method && new URL(response.url()).pathname === pathname);
}

function escapePattern(value: string): string { return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'); }
function groupDialog(page: Page, kind: 'playlists' | 'collections'): Locator {
  return page.getByRole('dialog', { name: kind === 'playlists' ? 'Manage playlist' : 'Manage collection', exact: true });
}

async function createGroup(page: Page, kind: 'playlists' | 'collections', name: string): Promise<string> {
  const label = kind === 'playlists' ? 'playlist' : 'collection';
  await page.getByRole('button', { name: `Create ${label}`, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: `Create ${label}`, exact: true });
  await dialog.getByRole('textbox', { name: kind === 'playlists' ? /^Playlist name/ : /^Collection name/ }).fill(name);
  if (kind === 'playlists') {
    await dialog.getByRole('combobox', { name: 'Media type', exact: true }).click();
    await page.getByRole('option', { name: 'Video', exact: true }).click();
  }
  const response = responseFor(page, 'POST', `/admin/v1/${kind}`);
  await dialog.getByRole('button', { name: `Create ${label}`, exact: true }).click();
  const created = await payload<{ Id: string; Name: string }>(await response);
  expect(created.Id).toMatch(/^[0-9a-f]{32}$/);
  expect(created.Name).toBe(name);
  await expect(groupDialog(page, kind).getByRole('heading', { name, exact: true })).toBeVisible();
  return created.Id;
}

async function saveGroup(page: Page, kind: 'playlists' | 'collections', id: string): Promise<ManagedCollection> {
  const response = responseFor(page, 'POST', `/admin/v1/${kind}/${id}`);
  await groupDialog(page, kind).getByRole('button', { name: 'Save details', exact: true }).click();
  const saved = await payload<ManagedCollection>(await response);
  await expect(groupDialog(page, kind).getByRole('button', { name: 'Save details', exact: true })).toBeDisabled();
  return saved;
}

async function shareGroup(page: Page, kind: 'playlists' | 'collections', id: string, fixture: Fixture): Promise<void> {
  const dialog = groupDialog(page, kind);
  await dialog.getByRole('combobox', { name: 'Share with user', exact: true }).click();
  await page.getByRole('option', { name: fixture.MemberName, exact: true }).click();
  await dialog.getByRole('button', { name: 'Add share', exact: true }).click();
  await dialog.getByRole('switch', { name: 'Can edit items', exact: true }).check();
  const saved = await saveGroup(page, kind, id);
  expect(saved.IsPublic).toBe(false);
  expect(saved.Shares).toEqual([{ UserId: fixture.MemberId, CanEdit: true }]);
}

async function addItems(page: Page, kind: 'playlists' | 'collections', id: string, names: string[], expectedCount: number): Promise<void> {
  const dialog = groupDialog(page, kind);
  for (const name of names) await dialog.getByRole('checkbox', { name: new RegExp(`^${escapePattern(name)}(?:\\s|$)`) }).check();
  const response = responseFor(page, 'POST', `/admin/v1/${kind}/${id}/items`);
  await dialog.getByRole('button', { name: 'Add selected media', exact: true }).click();
  await payload(await response);
  await expect(dialog.getByRole('list', { name: 'Collection items', exact: true }).getByRole('listitem')).toHaveCount(expectedCount);
  await expect(dialog.getByRole('button', { name: 'Add selected media', exact: true })).toBeDisabled();
}

async function deleteGroup(page: Page, kind: 'playlists' | 'collections', id: string): Promise<void> {
  const label = kind === 'playlists' ? 'playlist' : 'collection';
  const dialog = groupDialog(page, kind);
  await dialog.getByRole('tab', { name: 'Details and sharing', exact: true }).click();
  await dialog.getByRole('button', { name: `Delete ${label}`, exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: `Delete ${label}`, exact: true });
  const response = responseFor(page, 'DELETE', `/admin/v1/${kind}/${id}`);
  await confirmation.getByRole('button', { name: `Delete ${label}`, exact: true }).click();
  await payload(await response);
  await expect(dialog).not.toBeVisible();
}

test.use({ screenshot: 'off', trace: 'off', video: 'off', serviceWorkers: 'block' });

// This journey uses the disposable Go harness and real native/compatibility APIs.
// No API response, task result, catalog item, or authorization result is mocked.
test('live feature wave preserves media and enforces saved administrator controls', async ({ page, context }) => {
  test.setTimeout(330_000);
  const fixture = loadFixture();
  const url = (pathname: string) => `${fixture.BaseURL}${pathname}`;
  const secrets = [fixture.AdminPassword, fixture.MemberPassword];
  const result: BrowserResult = {
    Marker: 'goby-feature-wave-browser-result-v1', RunId: fixture.RunId, Complete: false,
    PlaylistId: '', CollectionId: '', CacheRunId: '',
    Checks: { PlaylistLifecycle: false, CollectionLifecycle: false, PolicyRestrictions: false, ManagementPersistence: false, GlobalCacheRun: false, NoPageErrors: false, NoOnlineProviderRequests: false, LogoutCompleted: false },
    Stages: [],
  };
  let pageErrorCount = 0;
  let providerRequestCount = 0;
  let memberToken = '';
  let csrf = '';
  let nativeLoggedOut = false;
  let memberLoggedOut = false;
  let signedIn = false;
  page.on('pageerror', () => { pageErrorCount += 1; });
  context.on('request', (entry) => {
    const target = new URL(entry.url());
    if (target.pathname === '/admin/v1/providers' || /^\/admin\/v1\/items\/[^/]+\/providers(?:\/|$)/.test(target.pathname)) providerRequestCount += 1;
  });
  const member: APIRequestContext = await request.newContext({ baseURL: fixture.BaseURL });
  const nativeGet = async <T>(pathname: string): Promise<T> => payload<T>(await context.request.get(url(pathname)));
  const memberGet = (pathname: string) => member.get(url(pathname), { headers: { 'X-Emby-Token': memberToken } });
  const readMembers = (kind: 'playlists' | 'collections', id: string) => nativeGet<CollectionPage<CollectionMember>>(`/admin/v1/${kind}/${id}/items?StartIndex=0&Limit=25`);
  const capture = async (name: string) => {
    if (!signedIn || nativeLoggedOut) throw new Error('Screenshots require an authenticated page.');
    await expect(page.locator('input[type="password"]:visible')).toHaveCount(0);
    await page.screenshot({ path: path.join(fixture.ArtifactsDir, `${name}.png`), fullPage: true, animations: 'disabled' });
  };
  try {
    await page.goto(url('/admin/collections'));
    await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    await page.getByLabel(/^Username/).fill(fixture.AdminName);
    await page.getByLabel(/^Password/).fill(fixture.AdminPassword);
    const signedInResponse = responseFor(page, 'POST', '/admin/v1/session');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    const session = await payload<{ User: { Id: string }; CSRFToken: string }>(await signedInResponse);
    if (typeof session.CSRFToken !== 'string' || !session.CSRFToken) throw new Error('The administrator session did not contain its CSRF token.');
    csrf = session.CSRFToken;
    secrets.push(csrf);
    await expect(page.getByRole('heading', { name: 'Playlists and collections', exact: true })).toBeVisible();
    signedIn = true;
    const login = await member.post(url('/emby/Users/AuthenticateByName'), {
      data: { Username: fixture.MemberName, Pw: fixture.MemberPassword },
      headers: { Authorization: `Emby Client="Feature wave browser", DeviceId="feature-wave-${fixture.RunId}", Device="Linux", Version="1.0"` },
    });
    const identity = await payload<{ User: { Id: string }; AccessToken: string }>(login);
    if (identity.User?.Id !== fixture.MemberId || typeof identity.AccessToken !== 'string' || !identity.AccessToken) throw new Error('The compatibility session does not belong to the fixture member.');
    memberToken = identity.AccessToken;
    secrets.push(memberToken);
    const downloadPath = `/emby/Items/${fixture.MovieIds[0]}/Download`;
    const initialDownload = await memberGet(downloadPath);
    expect(initialDownload.status(), 'The seeded movie must be downloadable before changing feature access.').toBe(200);
    const downloadBytes = (await initialDownload.body()).length;
    expect(downloadBytes).toBeGreaterThan(0);
    const catalog = await nativeGet<MetadataItemsResponse>(`/admin/v1/libraries/${fixture.LibraryId}/items?StartIndex=0&Limit=25`);
    const movies = fixture.MovieIds.map((id) => catalog.Items.find((item) => item.Id === id));
    if (movies.some((item) => !item) || new Set(movies.map((item) => item!.Name)).size !== 2) throw new Error('The fixture requires two uniquely named catalog movies.');
    const movieNames = movies.map((item) => item!.Name);
    await capture('authentication');
    await checkpoint(fixture, result, { Phase: 'authentication', State: 'complete', Ids: [fixture.MemberId, fixture.LibraryId], Counts: { MovieCount: movies.length, DownloadBytes: downloadBytes } }, secrets);

    const seedName = fixture.PlaylistSeedName ?? `Feature wave ${fixture.RunId}`;
    result.PlaylistId = await createGroup(page, 'playlists', `${seedName} playlist`);
    const playlistId = result.PlaylistId;
    expect((await memberGet(`/emby/Playlists/${playlistId}`)).status()).toBe(404);
    await shareGroup(page, 'playlists', playlistId, fixture);
    const sharedPlaylist = await payload<ManagedCollection>(await memberGet(`/emby/Playlists/${playlistId}`));
    expect(sharedPlaylist.Id).toBe(playlistId);
    expect(sharedPlaylist.OwnerId).toBe(session.User.Id);
    let dialog = groupDialog(page, 'playlists');
    await dialog.getByRole('tab', { name: 'Items', exact: true }).click();
    await dialog.getByRole('combobox', { name: 'Source library', exact: true }).click();
    await page.getByRole('option', { name: catalog.Library.Name, exact: true }).click();
    await addItems(page, 'playlists', playlistId, movieNames, 2);
    await addItems(page, 'playlists', playlistId, [movieNames[0]], 3);
    const original = await readMembers('playlists', playlistId);
    expect(original.Items.map((item) => item.Id)).toEqual([fixture.MovieIds[0], fixture.MovieIds[1], fixture.MovieIds[0]]);
    const entries = original.Items.map((item) => item.PlaylistItemId!);
    expect(new Set(entries).size).toBe(3);
    const rows = dialog.getByRole('list', { name: 'Collection items', exact: true }).getByRole('listitem');
    for (const target of [1, 0]) {
      const moved = responseFor(page, 'POST', `/admin/v1/playlists/${playlistId}/items/${entries[2]}/move/${target}`);
      const refreshedItems = responseFor(page, 'GET', `/admin/v1/playlists/${playlistId}/items`);
      await rows.nth(target + 1).getByRole('button', { name: `Move ${movieNames[0]} up`, exact: true }).click();
      await payload(await moved);
      await payload(await refreshedItems);
      await expect(rows).toHaveCount(3);
      await expect.poll(async () => (await readMembers('playlists', playlistId)).Items[target].PlaylistItemId).toBe(entries[2]);
    }
    const removed = responseFor(page, 'DELETE', `/admin/v1/playlists/${playlistId}/items`);
    await rows.nth(1).getByRole('button', { name: `Remove ${movieNames[0]} from playlist`, exact: true }).click();
    const removedResponse = await removed;
    expect(new URL(removedResponse.url()).searchParams.get('EntryIds')).toBe(entries[0]);
    await payload(removedResponse);
    await expect(rows).toHaveCount(2);
    expect((await readMembers('playlists', playlistId)).Items.map((item) => item.PlaylistItemId)).toEqual([entries[2], entries[1]]);
    await dialog.getByRole('tab', { name: 'Details and sharing', exact: true }).click();
    await dialog.getByRole('switch', { name: 'Lock membership and deletion', exact: true }).check();
    expect((await saveGroup(page, 'playlists', playlistId)).IsLocked).toBe(true);
    await expect(dialog.getByRole('button', { name: 'Delete playlist', exact: true })).toBeDisabled();
    await dialog.getByRole('tab', { name: 'Items', exact: true }).click();
    await expect(rows).toHaveCount(2);
    for (const name of movieNames) await expect(dialog.getByRole('button', { name: `Remove ${name} from playlist`, exact: true })).toBeDisabled();
    await expect(rows.nth(0).getByRole('button', { name: `Move ${movieNames[0]} down`, exact: true })).toBeDisabled();
    await expect(rows.nth(1).getByRole('button', { name: `Move ${movieNames[1]} up`, exact: true })).toBeDisabled();
    await expect(dialog.getByRole('checkbox', { name: new RegExp(`^${escapePattern(movieNames[0])}(?:\\s|$)`) })).toBeDisabled();
    await expect(dialog.getByRole('button', { name: 'Add selected media', exact: true })).toBeDisabled();
    const lockedWrite = await member.post(url(`/emby/Playlists/${playlistId}/Items?Ids=${fixture.MovieIds[0]}`), { headers: { 'X-Emby-Token': memberToken } });
    expect(lockedWrite.status()).toBe(403);
    expect((await readMembers('playlists', playlistId)).TotalRecordCount).toBe(2);
    await capture('playlist-locked');
    await dialog.getByRole('tab', { name: 'Details and sharing', exact: true }).click();
    await dialog.getByRole('switch', { name: 'Lock membership and deletion', exact: true }).uncheck();
    await saveGroup(page, 'playlists', playlistId);
    await deleteGroup(page, 'playlists', playlistId);
    expect((await context.request.get(url(`/admin/v1/playlists/${playlistId}`))).status()).toBe(404);
    expect((await memberGet(`/emby/Playlists/${playlistId}`)).status()).toBe(404);
    result.Checks.PlaylistLifecycle = true;
    await checkpoint(fixture, result, { Phase: 'playlist', State: 'complete', Ids: [playlistId], Counts: { AddedEntries: 3, RemainingBeforeDeletion: 2, DeletedGroups: 1 } }, secrets);

    await page.getByRole('tab', { name: 'Collections', exact: true }).click();
    result.CollectionId = await createGroup(page, 'collections', `${seedName} collection`);
    const collectionId = result.CollectionId;
    expect((await memberGet(`/emby/Collections/${collectionId}`)).status()).toBe(404);
    await shareGroup(page, 'collections', collectionId, fixture);
    expect((await payload<ManagedCollection>(await memberGet(`/emby/Collections/${collectionId}`))).Id).toBe(collectionId);
    dialog = groupDialog(page, 'collections');
    await dialog.getByRole('tab', { name: 'Items', exact: true }).click();
    await dialog.getByRole('combobox', { name: 'Source library', exact: true }).click();
    await page.getByRole('option', { name: catalog.Library.Name, exact: true }).click();
    await addItems(page, 'collections', collectionId, movieNames, 2);
    await addItems(page, 'collections', collectionId, [movieNames[0]], 2);
    const boxMembers = await readMembers('collections', collectionId);
    expect(boxMembers.Items.map((item) => item.Id).sort()).toEqual([...fixture.MovieIds].sort());
    expect(boxMembers.Items.every((item) => item.PlaylistItemId === undefined)).toBe(true);
    const sharedMembers = await payload<CollectionPage<CollectionMember>>(await memberGet(`/emby/Collections/${collectionId}/Items?EnableImages=false`));
    expect(sharedMembers.TotalRecordCount).toBe(2);
    await capture('collection-shared');
    await deleteGroup(page, 'collections', collectionId);
    expect((await context.request.get(url(`/admin/v1/collections/${collectionId}`))).status()).toBe(404);
    expect((await memberGet(`/emby/Collections/${collectionId}`)).status()).toBe(404);
    const surviving = await nativeGet<MetadataItemsResponse>(`/admin/v1/libraries/${fixture.LibraryId}/items?StartIndex=0&Limit=25`);
    expect(surviving.Items.map((item) => item.Id).sort()).toEqual([...fixture.MovieIds].sort());
    result.Checks.CollectionLifecycle = true;
    await checkpoint(fixture, result, { Phase: 'collection', State: 'complete', Ids: [collectionId], Counts: { UniqueMembers: 2, DeletedGroups: 1, SurvivingMovies: 2 } }, secrets);

    await page.goto(url('/admin/users'));
    const registry = await nativeGet<{ Items: FeatureInfo[] }>('/admin/v1/features');
    const downloadFeature = registry.Items.find((feature) => feature.Id === 'goby_downloads');
    if (!downloadFeature) throw new Error('The installed download feature is missing from the registry.');
    const beforeUser = await nativeGet<{ User: ManagedUser }>(`/admin/v1/users/${fixture.MemberId}`);
    await page.getByRole('button', { name: `Manage ${fixture.MemberName}`, exact: true }).click();
    const userDialog = page.getByRole('dialog', { name: 'Manage user', exact: true });
    await userDialog.getByRole('group', { name: 'Feature access', exact: true }).getByRole('checkbox', { name: downloadFeature.Name, exact: true }).uncheck();
    await userDialog.getByRole('switch', { name: 'Download media', exact: true }).check();
    await userDialog.getByLabel('Automatic remote quality (bits per second)', { exact: true }).fill('12000000');
    await userDialog.getByLabel('Remote bitrate limit (bits per second)', { exact: true }).fill('6000000');
    const savedUserResponse = responseFor(page, 'PUT', `/admin/v1/users/${fixture.MemberId}`);
    await userDialog.getByRole('button', { name: 'Save changes', exact: true }).click();
    const savedUser = await payload<{ User: ManagedUser; CurrentSessionRevoked: boolean }>(await savedUserResponse);
    expect(savedUser.User.Revision).toBe((BigInt(beforeUser.User.Revision) + 1n).toString());
    expect(savedUser.CurrentSessionRevoked).toBe(false);
    await expect(userDialog.getByText('All changes saved', { exact: true })).toBeVisible();
    const persistedUser = await nativeGet<{ User: ManagedUser }>(`/admin/v1/users/${fixture.MemberId}`);
    expect(persistedUser.User.Policy.RestrictedFeatures).toEqual([downloadFeature.Id]);
    expect(persistedUser.User.Policy.EnableContentDownloading).toBe(true);
    expect(persistedUser.User.Policy.AutoRemoteQuality).toBe(12_000_000);
    expect(persistedUser.User.Policy.RemoteClientBitrateLimit).toBe(6_000_000);
    expect((await memberGet(downloadPath)).status(), 'The same valid media download must now be denied by its feature restriction.').toBe(403);
    await capture('user-policy-saved');
    await userDialog.getByRole('button', { name: 'Close', exact: true }).filter({ hasText: /^Close$/ }).click();
    result.Checks.PolicyRestrictions = true;
    await checkpoint(fixture, result, { Phase: 'user-policy', State: 'complete', Ids: [fixture.MemberId], Counts: { RestrictedFeatures: 1, DownloadStatusBefore: 200, DownloadStatusAfter: 403 } }, secrets);

    await page.goto(url('/admin/settings'));
    const managementPanel = page.getByRole('region', { name: 'Metadata, subtitles, and tasks', exact: true });
    await expect(managementPanel).toBeVisible();
    const beforeSettings = await nativeGet<ServerSettings>('/admin/v1/settings');
    const expectedManagement: ManagementSettings = {
      Metadata: { EnableInternetProviders: false, PreferredMetadataLanguage: 'fr', MetadataCountryCode: 'FR' },
      Subtitles: { DownloadLanguages: [], DownloadMovieSubtitles: false, DownloadEpisodeSubtitles: false },
      Tasks: { MaxConcurrent: 3, CacheRetentionDays: 1, CacheMaxEntries: 1 },
    };
    await managementPanel.getByRole('switch', { name: 'Enable internet providers', exact: true }).uncheck();
    await managementPanel.getByLabel('Preferred metadata language', { exact: true }).fill('fr');
    await managementPanel.getByLabel('Metadata country', { exact: true }).fill('FR');
    await managementPanel.getByLabel('Subtitle download languages', { exact: true }).fill('');
    await managementPanel.getByRole('switch', { name: 'Download movie subtitles', exact: true }).uncheck();
    await managementPanel.getByRole('switch', { name: 'Download episode subtitles', exact: true }).uncheck();
    await managementPanel.getByLabel('Concurrent provider and cache workers', { exact: true }).fill('3');
    await managementPanel.getByLabel('Cache retention (days)', { exact: true }).fill('1');
    await managementPanel.getByLabel('Maximum cache entries', { exact: true }).fill('1');
    const settingsResponse = responseFor(page, 'PUT', '/admin/v1/settings');
    await page.getByRole('button', { name: 'Save settings', exact: true }).click();
    const savedSettings = await payload<ServerSettings>(await settingsResponse);
    expect(savedSettings.Revision).toBe((BigInt(beforeSettings.Revision) + 1n).toString());
    expect(savedSettings.Management).toEqual(expectedManagement);
    await expect(page.getByText('Settings saved.', { exact: true })).toBeVisible();
    await page.reload();
    await expect(managementPanel.getByLabel('Preferred metadata language', { exact: true })).toHaveValue('fr');
    await expect(managementPanel.getByLabel('Metadata country', { exact: true })).toHaveValue('FR');
    await expect(managementPanel.getByRole('switch', { name: 'Enable internet providers', exact: true })).not.toBeChecked();
    await expect(managementPanel.getByLabel('Subtitle download languages', { exact: true })).toHaveValue('');
    await expect(managementPanel.getByLabel('Concurrent provider and cache workers', { exact: true })).toHaveValue('3');
    await expect(managementPanel.getByLabel('Cache retention (days)', { exact: true })).toHaveValue('1');
    await expect(managementPanel.getByLabel('Maximum cache entries', { exact: true })).toHaveValue('1');
    expect((await nativeGet<ServerSettings>('/admin/v1/settings')).Management).toEqual(expectedManagement);
    await capture('management-saved');
    result.Checks.ManagementPersistence = true;
    await checkpoint(fixture, result, { Phase: 'management', State: 'complete', Ids: [], Counts: { ConcurrentWorkers: 3, CacheRetentionDays: 1, CacheMaxEntries: 1, SubtitleLanguages: 0 } }, secrets);

    await page.goto(url('/admin/tasks'));
    const tasks = await nativeGet<TaskDefinitionsResponse>('/admin/v1/tasks');
    const cacheTask = tasks.Items.find((entry) => entry.Key === 'cache.maintain');
    if (!cacheTask || !cacheTask.Enabled || cacheTask.CurrentRun) throw new Error('The fixture cache task is not ready for its isolated run.');
    const taskCard = page.getByRole('list', { name: 'Available server tasks', exact: true }).getByRole('listitem').filter({ has: page.getByRole('heading', { name: cacheTask.Name, exact: true }) });
    const admissionResponse = responseFor(page, 'POST', `/admin/v1/tasks/${cacheTask.Id}/runs`);
    await taskCard.getByRole('button', { name: 'Start task', exact: true }).click();
    const admission = await payload<{ Admitted: boolean; Run: TaskRunDetail['Run'] }>(await admissionResponse, 202);
    expect(admission.Admitted).toBe(true);
    result.CacheRunId = admission.Run.Id;
    const taskDialog = page.getByRole('dialog', { name: `Run details · ${cacheTask.Name}`, exact: true });
    await expect(taskDialog).toBeVisible();
    let cacheRun: TaskRunDetail | undefined;
    await expect.poll(async () => {
      cacheRun = await nativeGet<TaskRunDetail>(`/admin/v1/task-runs/${result.CacheRunId}?StartIndex=0&Limit=50`);
      return cacheRun.Run.State;
    }, { timeout: 60_000, intervals: [250, 500, 1000], message: 'The real global cache task must complete.' }).toBe('completed');
    expect(cacheRun!.Run.TaskId).toBe(cacheTask.Id);
    expect(cacheRun!.Run.TotalChildren).toBe(1);
    expect(cacheRun!.Run.CompletedChildren).toBe(1);
    expect(cacheRun!.Run.FailedChildren).toBe(0);
    expect(cacheRun!.Children.TotalRecordCount).toBe(1);
    expect(cacheRun!.Children.Items[0].State).toBe('completed');
    expect(cacheRun!.Children.Items[0].LibraryId).toBe('');
    await taskDialog.getByRole('button', { name: 'Refresh run', exact: true }).click();
    await expect(taskDialog.getByText('Completed', { exact: true }).first()).toBeVisible();
    await capture('cache-completed');
    await taskDialog.getByRole('button', { name: 'Close', exact: true }).filter({ hasText: /^Close$/ }).click();
    result.Checks.GlobalCacheRun = true;
    await checkpoint(fixture, result, { Phase: 'cache', State: 'complete', Ids: [cacheTask.Id, result.CacheRunId], Counts: { TotalChildren: 1, CompletedChildren: 1, FailedChildren: 0 } }, secrets);

    const memberLogout = await member.post(url('/emby/Sessions/Logout'), { headers: { 'X-Emby-Token': memberToken } });
    expect(memberLogout.status()).toBe(204);
    memberLoggedOut = true;
    const logoutResponse = responseFor(page, 'DELETE', '/admin/v1/session');
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    expect((await logoutResponse).status()).toBe(204);
    nativeLoggedOut = true;
    await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    expect((await context.request.get(url('/admin/v1/session'))).status()).toBe(401);
    expect((await memberGet(downloadPath)).status()).toBe(401);
    expect(pageErrorCount).toBe(0);
    expect(providerRequestCount).toBe(0);
    result.Checks.NoPageErrors = true;
    result.Checks.NoOnlineProviderRequests = true;
    result.Checks.LogoutCompleted = true;
    await checkpoint(fixture, result, { Phase: 'cleanup', State: 'complete', Ids: [], Counts: { NativeSessionsEnded: 1, MemberSessionsEnded: 1, PageErrors: pageErrorCount, OnlineProviderRequests: providerRequestCount } }, secrets);
    result.Complete = true;
    writePrivateJSON(fixture.ResultPath, result, secrets);
  } finally {
    // Revoke fixture sessions even when a prior stage failed. Never persist them.
    if (memberToken && !memberLoggedOut) await member.post(url('/emby/Sessions/Logout'), { headers: { 'X-Emby-Token': memberToken } }).catch(() => undefined);
    if (csrf && !nativeLoggedOut) await context.request.delete(url('/admin/v1/session'), { headers: { 'X-CSRF-Token': csrf, Origin: fixture.BaseURL } }).catch(() => undefined);
    await member.dispose();
  }
});
