import { createHash, randomBytes } from 'node:crypto';
import { closeSync, constants, existsSync, fstatSync, fsyncSync, lstatSync, openSync, readFileSync, realpathSync, renameSync, unlinkSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import type { Frame, Locator, Page, Response } from '@playwright/test';
import type { MetadataDetail, TaskDefinitionResponse } from '../src/api';
import type { ArtworkCollection } from '../src/artworkApi';
import type { EditableLibrary } from '../src/libraryManagementApi';
import type { UserPreferences } from '../src/userPreferencesApi';

interface Fixture {
  Marker: string; RunId: string; BaseURL: string; AdminId: string; AdminName: string; AdminPassword: string;
  UserId: string; UserName: string; LibraryId: string; LibraryName: string; LibraryPath: string; WritableDirectory: string;
  AudioItemId: string; AudioItemName: string; ArtworkEntityId: string; ArtworkEntityName: string;
  CacheTaskId: string; CacheTaskName: string; ImageUploadPath: string; ImageUploadPathSecond: string;
  ArtifactsDir: string; ResultPath: string;
}
type Phase = 'authentication' | 'library-conflict' | 'library' | 'preferences' | 'artwork' | 'music' | 'schedule' | 'restart' | 'persistence' | 'permission-revoked' | 'permission-denied' | 'cleanup';
interface StageAck { Marker: string; RunId: string; Phase: Phase; Complete: boolean; SourcesUnchanged: boolean; BaseURL?: string; StartupRunId?: string }
const checks = ['LibrarySaved', 'LibraryConflict', 'PreferencesSaved', 'ArtworkSaved', 'MusicSaved', 'TriggersSaved', 'RestartPersisted', 'PermissionRevocation', 'Completed'] as const;
interface BrowserResult { Marker: string; RunId: string; Complete: boolean; StartupRunId: string; Checks: Record<typeof checks[number], boolean>; Stages: { Phase: Phase; State: 'complete' }[]; PageErrors: number; ForeignRequests: number }

function privateDirectory(directory: string): void {
  const stat = lstatSync(directory);
  if (!path.isAbsolute(directory) || directory !== path.resolve(directory) || realpathSync(directory) !== directory || !stat.isDirectory() || stat.isSymbolicLink() || stat.uid !== process.getuid?.() || (stat.mode & 0o777) !== 0o700) throw new Error('Phase 3 artifacts require a private directory owned by the isolated runner.');
}
function privateJSON<T>(filename: string, maximum = 128 * 1024): T {
  const descriptor = openSync(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = fstatSync(descriptor);
    if (!stat.isFile() || stat.uid !== process.getuid?.() || stat.nlink !== 1 || (stat.mode & 0o777) !== 0o600 || stat.size < 1 || stat.size > maximum || realpathSync(`/proc/self/fd/${descriptor}`) !== filename) throw new Error('Phase 3 input must remain a bounded private regular file.');
    try { return JSON.parse(readFileSync(descriptor, 'utf8')) as T; } catch { throw new Error('Phase 3 input is not valid JSON.'); }
  } finally { closeSync(descriptor); }
}
function loadFixture(): Fixture {
  const filename = process.env.GOBY_PHASE3_BROWSER_CONTEXT;
  if (process.platform !== 'linux' || !filename || !path.isAbsolute(filename) || filename !== path.resolve(filename)) throw new Error('Phase 3 requires the isolated Linux browser fixture.');
  privateDirectory(path.dirname(filename));
  const fixture = privateJSON<Fixture>(filename, 32 * 1024);
  let origin: URL;
  try { origin = new URL(fixture.BaseURL); } catch { throw new Error('The Phase 3 origin is invalid.'); }
  if (fixture.Marker !== 'goby-phase3-browser-fixture-v1' || !/^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$/.test(fixture.RunId) || fixture.RunId !== process.env.GOBY_PHASE3_BROWSER_RUN_ID
    || origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || !origin.port || ['5432', '15432', '18096', '18097'].includes(origin.port) || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash
    || ![fixture.AdminId, fixture.AdminName, fixture.AdminPassword, fixture.UserId, fixture.UserName, fixture.LibraryId, fixture.LibraryName, fixture.AudioItemId, fixture.AudioItemName, fixture.ArtworkEntityId, fixture.ArtworkEntityName, fixture.CacheTaskId, fixture.CacheTaskName].every((value) => typeof value === 'string' && value.length > 0)
    || ![fixture.LibraryPath, fixture.WritableDirectory, fixture.ImageUploadPath, fixture.ImageUploadPathSecond, fixture.ResultPath].every((value) => typeof value === 'string' && path.isAbsolute(value) && value === path.resolve(value))
    || path.dirname(fixture.ResultPath) !== fixture.ArtifactsDir || path.extname(fixture.ResultPath) !== '.json' || fixture.ResultPath === filename) throw new Error('Phase 3 fixture must identify a fresh disposable run.');
  privateDirectory(fixture.ArtifactsDir);
  if (existsSync(fixture.ResultPath)) throw new Error('Phase 3 browser result already exists.');
  for (const filename of [fixture.ImageUploadPath, fixture.ImageUploadPathSecond]) {
    const stat = lstatSync(filename);
    if (!stat.isFile() || stat.isSymbolicLink() || stat.uid !== process.getuid?.() || stat.size < 1 || stat.size > 20 * 1024 * 1024 || realpathSync(filename) !== filename) throw new Error('The upload fixture must be a bounded owned regular image.');
  }
  fixture.BaseURL = origin.origin;
  return fixture;
}
function writePrivateJSON(filename: string, value: unknown, secret: string): void {
  const encoded = `${JSON.stringify(value, null, 2)}\n`;
  if (secret && encoded.includes(secret)) throw new Error('Credentials cannot be written to browser artifacts.');
  if (existsSync(filename)) throw new Error('The browser artifact already exists.');
  const temporary = path.join(path.dirname(filename), `.${path.basename(filename)}.${randomBytes(8).toString('hex')}.tmp`);
  const descriptor = openSync(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try {
    try { writeFileSync(descriptor, encoded, 'utf8'); fsyncSync(descriptor); } finally { closeSync(descriptor); }
    if (existsSync(filename)) throw new Error('The browser artifact appeared before publication.');
    renameSync(temporary, filename);
  } finally { if (existsSync(temporary)) unlinkSync(temporary); }
}
async function stage(fixture: Fixture, result: BrowserResult, phase: Phase): Promise<StageAck> {
  writePrivateJSON(path.join(fixture.ArtifactsDir, `stage-${phase}-request.json`), { RunId: fixture.RunId, Phase: phase }, fixture.AdminPassword);
  const filename = path.join(fixture.ArtifactsDir, `stage-${phase}-database.json`);
  let ack: StageAck | undefined;
  await expect.poll(() => {
    if (!existsSync(filename)) return false;
    ack = privateJSON<StageAck>(filename);
    if (ack.Marker !== 'goby-phase3-stage-database-v1' || ack.RunId !== fixture.RunId || ack.Phase !== phase || ack.Complete !== true || ack.SourcesUnchanged !== true) throw new Error('The database checkpoint did not confirm this owned stage and unchanged source files.');
    return true;
  }, { timeout: 60_000, intervals: [100, 250, 500], message: `The ${phase} database checkpoint must complete.` }).toBe(true);
  result.Stages.push({ Phase: phase, State: 'complete' });
  return ack!;
}
type ResponseBody = { ok: true; body: Buffer } | { ok: false; error: unknown };
interface ResponseCapture {
  body: Promise<ResponseBody>;
  fixture: Fixture;
  sequence: number;
  diagnostic: { Method: string; Path: string; Status: number; ResourceType: string; IsNavigationRequest: boolean; ResponseContentType: string; ResponseContentLength: string; ResponseCacheControl: string; PagePathAtArm: string; PagePathAtResponse: string; PagePathAtBodyRead?: string; MainFrameNavigations: string[] };
}
const responseCaptures = new WeakMap<Response, ResponseCapture>();
let responseSequence = 0;
let uploadSequence = 0;
function responseFor(page: Page, fixture: Fixture, method: string, pathname: string): Promise<Response> {
  const navigations: string[] = [];
  const pagePath = () => new URL(page.url()).pathname;
  const armedPath = pagePath();
  const onNavigation = (frame: Frame) => { if (frame === page.mainFrame()) navigations.push(new URL(frame.url()).pathname); };
  page.on('framenavigated', onNavigation);
  const waiting = page.waitForResponse((response) => response.request().method() === method && new URL(response.url()).pathname === pathname).then((response) => {
    const headers = response.headers();
    const diagnostic: ResponseCapture['diagnostic'] = { Method: method, Path: pathname, Status: response.status(), ResourceType: response.request().resourceType(), IsNavigationRequest: response.request().isNavigationRequest(), ResponseContentType: (headers['content-type'] ?? '').slice(0, 160), ResponseContentLength: (headers['content-length'] ?? '').slice(0, 24), ResponseCacheControl: (headers['cache-control'] ?? '').slice(0, 160), PagePathAtArm: armedPath, PagePathAtResponse: pagePath(), MainFrameNavigations: navigations };
    // Start reading at the response event, while the click and UI update are
    // still in flight. Later assertions use these exact bytes, never a replay.
    const body = response.body().then<ResponseBody, ResponseBody>((value) => ({ ok: true, body: value }), (error: unknown) => ({ ok: false, error })).finally(() => { diagnostic.PagePathAtBodyRead = pagePath(); page.off('framenavigated', onNavigation); });
    responseCaptures.set(response, { body, diagnostic, fixture, sequence: ++responseSequence });
    return response;
  }, (error: unknown) => { page.off('framenavigated', onNavigation); throw error; });
  // A failed action can finish before its response waiter. Keep rejection
  // handled without changing the promise later awaited by the workflow.
  void waiting.catch(() => undefined);
  return waiting;
}
async function payload<T>(response: Promise<Response>, status = 200): Promise<T> {
  const result = await response;
  expect(result.status(), 'The real API status must match the workflow.').toBe(status);
  const captured = responseCaptures.get(result);
  if (!captured) throw new Error('The response was not registered for immediate body capture.');
  const body = await captured.body;
  if (!body.ok) {
    writePrivateJSON(path.join(captured.fixture.ArtifactsDir, `native-response-body-read-${captured.sequence}.json`), { Marker: 'goby-phase3-response-body-diagnostic-v1', RunId: captured.fixture.RunId, ...captured.diagnostic }, captured.fixture.AdminPassword);
    throw body.error;
  }
  expect(captured.diagnostic.IsNavigationRequest, 'A native mutation must remain an API request.').toBe(false);
  expect(captured.diagnostic.MainFrameNavigations, 'The page must not navigate while its mutation response is captured.').toEqual([]);
  expect(result.headers()['content-type'], 'The native response must contain JSON.').toMatch(/^application\/json(?:;|$)/i);
  return JSON.parse(body.body.toString('utf8')) as T;
}
async function select(page: Page, scope: Locator, label: string, option: string): Promise<void> { await scope.getByRole('combobox', { name: label, exact: true }).click(); await page.getByRole('option', { name: option, exact: true }).click(); }
async function login(page: Page, fixture: Fixture): Promise<void> {
  await page.goto(`${fixture.BaseURL}/admin/`);
  await expect(page.getByRole('heading', { name: 'Sign in to Goby' })).toBeVisible();
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill(fixture.AdminName);
  const password = page.locator('input#account-password[name="Password"]');
  await expect(password).toHaveAttribute('type', 'password');
  await expect(password).toHaveAccessibleName('Password');
  await password.fill(fixture.AdminPassword);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Administration' })).toBeVisible();
}
async function libraryEditor(page: Page, fixture: Fixture, name: string): Promise<Locator> {
  await page.goto(`${fixture.BaseURL}/admin/libraries`);
  await page.getByRole('button', { name: `Edit library ${name}`, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Edit library', exact: true });
  await expect(dialog.getByRole('textbox', { name: 'Library name', exact: true })).toHaveValue(name);
  return dialog;
}
async function preferencesEditor(page: Page, fixture: Fixture): Promise<Locator> {
  await page.goto(`${fixture.BaseURL}/admin/users`);
  await page.getByRole('button', { name: `Manage ${fixture.UserName}`, exact: true }).click();
  await page.getByRole('dialog', { name: 'Manage user', exact: true }).getByRole('button', { name: 'Playback and display preferences', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Playback and display preferences', exact: true });
  await expect(dialog.getByLabel('Preferred audio language', { exact: true })).toBeVisible();
  return dialog;
}
async function closePreferences(page: Page, dialog: Locator): Promise<void> { await dialog.getByRole('button', { name: 'Close', exact: true }).click(); await page.getByRole('dialog', { name: 'Manage user', exact: true }).getByRole('button', { name: 'Close', exact: true }).click(); }
async function metadataEditor(page: Page, fixture: Fixture): Promise<Locator> {
  await page.goto(`${fixture.BaseURL}/admin/libraries/${fixture.LibraryId}/items`);
  await page.getByRole('button', { name: `Edit metadata for ${fixture.AudioItemName}`, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: /^Edit metadata/ });
  await expect(dialog.getByRole('textbox', { name: 'Album tag', exact: true })).toBeVisible();
  return dialog;
}
function imageFilePayload(fixture: Fixture, filename: string) {
  if (![fixture.ImageUploadPath, fixture.ImageUploadPathSecond].includes(filename)) throw new Error('The image must be one of this run\'s declared PNG fixtures.');
  const descriptor = openSync(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const before = fstatSync(descriptor);
    if (!before.isFile() || before.uid !== process.getuid?.() || before.nlink !== 1 || (before.mode & 0o777) !== 0o600 || before.size < 8 || before.size > 20 * 1024 * 1024 || realpathSync(`/proc/self/fd/${descriptor}`) !== filename) throw new Error('The upload fixture must remain a bounded private regular file.');
    const buffer = readFileSync(descriptor);
    const after = fstatSync(descriptor);
    if (buffer.length !== before.size || before.size !== after.size || before.ino !== after.ino || before.dev !== after.dev || before.mtimeMs !== after.mtimeMs || before.ctimeMs !== after.ctimeMs || realpathSync(`/proc/self/fd/${descriptor}`) !== filename || !buffer.subarray(0, 8).equals(Buffer.from('89504e470d0a1a0a', 'hex'))) throw new Error('The PNG fixture changed while being read or has an invalid signature.');
    return { name: path.basename(filename), mimeType: 'image/png', buffer };
  } finally { closeSync(descriptor); }
}
async function upload(page: Page, fixture: Fixture, dialog: Locator, filepath: string, pathname: string): Promise<ArtworkCollection> {
  const file = imageFilePayload(fixture, filepath);
  writePrivateJSON(path.join(fixture.ArtifactsDir, `image-upload-input-${++uploadSequence}.json`), { Marker: 'goby-phase3-image-upload-input-v1', RunId: fixture.RunId, Method: 'PUT', Path: pathname, Name: file.name, MIME: file.mimeType, Bytes: file.buffer.length, SHA256: createHash('sha256').update(file.buffer).digest('hex'), InputMode: 'memory-file-payload' }, fixture.AdminPassword);
  // Chromium 153 can assign a path-backed File an unknown BlobDataHandle size;
  // DevTools then permanently evicts its resource before any response arrives.
  // Playwright's byte payload creates a memory-backed File with the same PNG
  // bytes, name and MIME type. The browser still performs the real UI upload.
  await dialog.getByLabel('Choose image file', { exact: true }).setInputFiles(file);
  await expect(dialog.getByRole('img', { name: 'Selected image preview', exact: true })).toBeVisible();
  const originalURL = page.url();
  const navigations: string[] = [];
  const onNavigation = (frame: Frame) => { if (frame === page.mainFrame()) navigations.push(new URL(frame.url()).pathname); };
  page.on('framenavigated', onNavigation);
  try {
    const response = responseFor(page, fixture, 'PUT', pathname);
    await dialog.getByRole('button', { name: 'Upload image', exact: true }).click();
    const collection = await payload<ArtworkCollection>(response);
    await expect(dialog.getByText('Image saved.', { exact: true })).toBeVisible();
    await expect.poll(() => dialog.getByRole('list', { name: 'Saved images' }).getByRole('img').evaluateAll((images) => images.every((element) => (element as HTMLImageElement).complete && (element as HTMLImageElement).naturalWidth > 0))).toBe(true);
    expect(navigations, 'Uploading artwork must not navigate the main frame.').toEqual([]);
    await expect(page).toHaveURL(originalURL);
    return collection;
  } finally { page.off('framenavigated', onNavigation); }
}

test.use({ screenshot: 'off', trace: 'off', video: 'off', serviceWorkers: 'block', actionTimeout: 15_000, viewport: { width: 1280, height: 900 } });

// The Go fixture supplies real native endpoints, a PostgreSQL database, source
// files, independent concurrent edits, a restart, and permission revocation.
test('phase 3 administration workflows persist through conflicts and restart', async ({ page, context }) => {
  test.setTimeout(420_000);
  test.skip(!process.env.GOBY_PHASE3_BROWSER_CONTEXT, 'Requires the private Phase 3 integration fixture.');
  const fixture = loadFixture();
  const result: BrowserResult = { Marker: 'goby-phase3-browser-result-v1', RunId: fixture.RunId, Complete: false, StartupRunId: '', Checks: Object.fromEntries(checks.map((key) => [key, false])) as BrowserResult['Checks'], Stages: [], PageErrors: 0, ForeignRequests: 0 };
  page.on('pageerror', () => { result.PageErrors += 1; });
  await context.route('**/*', async (route) => { const target = new URL(route.request().url()); if (target.origin === fixture.BaseURL || target.protocol === 'blob:' || target.protocol === 'data:') await route.continue(); else { result.ForeignRequests += 1; await route.abort('blockedbyclient'); } });
  try {
    await login(page, fixture);
    await stage(fixture, result, 'authentication');

    let library = await libraryEditor(page, fixture, fixture.LibraryName);
    await library.getByRole('textbox', { name: 'Library name', exact: true }).fill('Phase 3 Music Edited');
    page.once('dialog', (dialog) => void dialog.dismiss());
    await library.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(library).toBeVisible();
    await expect(library.getByRole('textbox', { name: 'Library name', exact: true })).toHaveValue('Phase 3 Music Edited');
    await stage(fixture, result, 'library-conflict');
    let response = responseFor(page, fixture, 'PATCH', `/admin/v1/libraries/${fixture.LibraryId}`);
    await library.getByRole('button', { name: 'Save library', exact: true }).click();
    await payload(response, 409);
    await expect(library.getByRole('button', { name: 'Save library', exact: true })).toBeDisabled();
    page.once('dialog', (dialog) => void dialog.accept());
    await library.getByRole('button', { name: 'Reload', exact: true }).last().click();
    await expect(library.getByRole('textbox', { name: 'Library name', exact: true })).toHaveValue('Concurrent library edit');
    result.Checks.LibraryConflict = true;
    await library.getByRole('textbox', { name: 'Library name', exact: true }).fill('Phase 3 Music Edited');
    await library.getByRole('button', { name: 'Add directory', exact: true }).click();
    await library.getByLabel('Directory 2', { exact: true }).fill('relative/path');
    await expect(library.getByRole('button', { name: 'Save library', exact: true })).toBeDisabled();
    await library.getByRole('button', { name: 'Browse directory 2', exact: true }).click();
    const directories = page.getByRole('dialog', { name: 'Choose a media directory', exact: true });
    await directories.getByLabel('Directory path', { exact: true }).fill(fixture.WritableDirectory);
    await directories.getByRole('button', { name: 'Browse', exact: true }).click();
    await expect(directories.getByText(fixture.WritableDirectory, { exact: true })).toBeVisible();
    await directories.getByRole('button', { name: 'Use directory', exact: true }).click();
    await expect(directories).not.toBeVisible();
    await expect(library.getByLabel('Directory 2', { exact: true })).toHaveValue(fixture.WritableDirectory);
    await library.getByRole('checkbox', { name: 'Import local metadata files', exact: true }).uncheck();
    await library.getByRole('checkbox', { name: 'Import images from media directories', exact: true }).uncheck();
    response = responseFor(page, fixture, 'PATCH', `/admin/v1/libraries/${fixture.LibraryId}`);
    await library.getByRole('button', { name: 'Save library', exact: true }).click();
    const savedLibrary = await payload<{ Library: EditableLibrary }>(response);
    expect(savedLibrary.Library.Paths).toEqual([fixture.LibraryPath, fixture.WritableDirectory]);
    expect(savedLibrary.Library.LibraryOptions).toEqual({ EnableLocalMetadata: false, EnableLocalImages: false });
    await expect(library).not.toBeVisible();
    result.Checks.LibrarySaved = true;
    await stage(fixture, result, 'library');

    let preferences = await preferencesEditor(page, fixture);
    await expect(preferences.getByRole('checkbox', { name: 'Hide played items from Suggestions', exact: true })).toBeVisible();
    await expect(preferences.getByRole('checkbox', { name: 'Display missing episodes', exact: true })).toBeVisible();
    await preferences.getByLabel('Preferred audio language', { exact: true }).fill('eng');
    await preferences.getByLabel('Preferred subtitle language', { exact: true }).fill('zho');
    await select(page, preferences, 'Subtitle mode', 'Always');
    await preferences.getByLabel('Rewind on resume (seconds)', { exact: true }).fill('301');
    await expect(preferences.getByRole('button', { name: 'Save preferences', exact: true })).toBeDisabled();
    await preferences.getByLabel('Rewind on resume (seconds)', { exact: true }).fill('10');
    response = responseFor(page, fixture, 'PUT', `/admin/v1/users/${fixture.UserId}/preferences`);
    await preferences.getByRole('button', { name: 'Save preferences', exact: true }).click();
    const preferenceResponse = await response;
    const submittedPreferences = preferenceResponse.request().postDataJSON() as { Configuration: Record<string, unknown> };
    expect(Object.keys(submittedPreferences.Configuration).filter((field) => ['EnableLocalPassword', 'ProfilePin'].includes(field))).toEqual([]);
    expect(submittedPreferences.Configuration).toMatchObject({ HidePlayedInSuggestions: expect.any(Boolean), DisplayMissingEpisodes: expect.any(Boolean), EnableNextEpisodeAutoPlay: expect.any(Boolean), IntroSkipMode: expect.stringMatching(/^(?:None|ShowButton|AutoSkip)$/) });
    const savedPreferences = await payload<UserPreferences>(Promise.resolve(preferenceResponse));
    expect(savedPreferences.Configuration).toMatchObject({ AudioLanguagePreference: 'eng', SubtitleLanguagePreference: 'zho', SubtitleMode: 'Always', ResumeRewindSeconds: 10 });
    await expect(preferences.getByText('Preferences saved. New client requests use these settings.', { exact: true })).toBeVisible();
    await closePreferences(page, preferences);
    result.Checks.PreferencesSaved = true;
    await stage(fixture, result, 'preferences');

    await page.goto(`${fixture.BaseURL}/admin/artwork`);
    await select(page, page.locator('main'), 'Catalog type', 'Genres');
    await page.getByRole('button', { name: `Manage artwork for ${fixture.ArtworkEntityName}`, exact: true }).click();
    let artwork = page.getByRole('dialog', { name: 'Manage artwork', exact: true });
    const entityPath = `/admin/v1/entities/${fixture.ArtworkEntityId}/images`;
    await artwork.getByLabel('Choose image file', { exact: true }).setInputFiles({ name: 'invalid.txt', mimeType: 'text/plain', buffer: Buffer.from('not an image') });
    await expect(artwork.getByRole('button', { name: 'Upload image', exact: true })).toBeDisabled();
    await expect(artwork.getByText('Choose a JPEG, PNG, or GIF image.', { exact: true })).toBeVisible();
    await upload(page, fixture, artwork, fixture.ImageUploadPath, `${entityPath}/Primary/0`);
    await artwork.getByRole('button', { name: 'Delete Primary image 1', exact: true }).click();
    response = responseFor(page, fixture, 'DELETE', `${entityPath}/Primary/0`);
    await page.getByRole('dialog', { name: 'Delete this image?', exact: true }).getByRole('button', { name: 'Delete image', exact: true }).click();
    expect((await payload<ArtworkCollection>(response)).Items).toHaveLength(0);
    await upload(page, fixture, artwork, fixture.ImageUploadPath, `${entityPath}/Primary/0`);
    await select(page, artwork, 'Image type', 'Backdrop');
    const firstBackdrop = await upload(page, fixture, artwork, fixture.ImageUploadPath, `${entityPath}/Backdrop/0`);
    await select(page, artwork, 'Upload destination', 'Add a new image');
    const twoBackdrops = await upload(page, fixture, artwork, fixture.ImageUploadPathSecond, `${entityPath}/Backdrop/1`);
    const beforeOrder = twoBackdrops.Items.filter((image) => image.ImageType === 'Backdrop').sort((a, b) => a.ImageIndex - b.ImageIndex).map((image) => image.Tag);
    expect(beforeOrder).toHaveLength(2);
    expect(beforeOrder[0]).toBe(firstBackdrop.Items.find((image) => image.ImageType === 'Backdrop')?.Tag);
    expect(beforeOrder[0]).not.toBe(beforeOrder[1]);
    response = responseFor(page, fixture, 'POST', `${entityPath}/Backdrop/reorder`);
    await artwork.getByRole('button', { name: 'Move image 2 earlier', exact: true }).click();
    expect((await payload<ArtworkCollection>(response)).Items.filter((image) => image.ImageType === 'Backdrop').sort((a, b) => a.ImageIndex - b.ImageIndex).map((image) => image.Tag)).toEqual([...beforeOrder].reverse());
    await artwork.getByRole('button', { name: 'Restore automatic backdrop images', exact: true }).click();
    response = responseFor(page, fixture, 'POST', `${entityPath}/Backdrop/reset`);
    await page.getByRole('dialog', { name: 'Restore automatic images?', exact: true }).getByRole('button', { name: 'Restore automatic', exact: true }).click();
    expect((await payload<ArtworkCollection>(response)).Items.filter((image) => image.ImageType === 'Backdrop')).toHaveLength(0);
    await artwork.getByRole('button', { name: 'Close', exact: true }).click();
    await page.goto(`${fixture.BaseURL}/admin/users`);
    await page.getByRole('button', { name: `Manage ${fixture.UserName}`, exact: true }).click();
    await page.getByRole('dialog', { name: 'Manage user', exact: true }).getByRole('button', { name: 'Manage avatar', exact: true }).click();
    const avatar = page.getByRole('dialog', { name: 'Manage avatar', exact: true });
    await upload(page, fixture, avatar, fixture.ImageUploadPathSecond, `/admin/v1/users/${fixture.UserId}/image`);
    await avatar.getByRole('button', { name: 'Close', exact: true }).click();
    await page.getByRole('dialog', { name: 'Manage user', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
    result.Checks.ArtworkSaved = true;
    await stage(fixture, result, 'artwork');

    let metadata = await metadataEditor(page, fixture);
    await metadata.getByRole('textbox', { name: 'Album tag', exact: true }).fill('Phase 3 Album');
    const artists = metadata.getByRole('group', { name: 'Artists', exact: true });
    if (await artists.getByRole('button', { name: 'Clear all artists', exact: true }).isEnabled()) await artists.getByRole('button', { name: 'Clear all artists', exact: true }).click();
    await artists.getByRole('button', { name: 'Add artist', exact: true }).click();
    await artists.getByRole('textbox', { name: 'Artist 1', exact: true }).fill('\u754c'.repeat(342));
    await metadata.getByRole('button', { name: 'Save changes', exact: true }).click();
    await expect(artists.getByText('Use at most 1,024 UTF-8 bytes.', { exact: true })).toBeVisible();
    await artists.getByRole('textbox', { name: 'Artist 1', exact: true }).fill('Phase 3 Artist');
    await artists.getByRole('button', { name: 'Add artist', exact: true }).click();
    await artists.getByRole('textbox', { name: 'Artist 2', exact: true }).fill('Guest Artist');
    const albumArtists = metadata.getByRole('group', { name: 'Album artists', exact: true });
    if (await albumArtists.getByRole('button', { name: 'Clear all album artists', exact: true }).isEnabled()) await albumArtists.getByRole('button', { name: 'Clear all album artists', exact: true }).click();
    await albumArtists.getByRole('button', { name: 'Add album artist', exact: true }).click();
    await albumArtists.getByRole('textbox', { name: 'Album artist 1', exact: true }).fill('Phase 3 Album Artist');
    await metadata.getByRole('textbox', { name: 'Track number', exact: true }).fill('7');
    await metadata.getByRole('textbox', { name: 'Disc number', exact: true }).fill('2');
    await metadata.getByRole('tab', { name: 'People & categories', exact: true }).click();
    const people = metadata.getByRole('group', { name: 'People', exact: true });
    if (await people.getByRole('button', { name: 'Clear all people', exact: true }).isEnabled()) await people.getByRole('button', { name: 'Clear all people', exact: true }).click();
    await people.getByRole('button', { name: 'Add person', exact: true }).click();
    await people.getByRole('textbox', { name: 'Name', exact: true }).fill('Phase 3 Composer');
    await select(page, people, 'Type', 'Composer');
    response = responseFor(page, fixture, 'PUT', `/admin/v1/items/${fixture.AudioItemId}/metadata`);
    await metadata.getByRole('button', { name: 'Save changes', exact: true }).click();
    expect((await payload<MetadataDetail>(response)).Effective).toMatchObject({ Album: 'Phase 3 Album', Artists: ['Phase 3 Artist', 'Guest Artist'], AlbumArtists: ['Phase 3 Album Artist'], IndexNumber: 7, ParentIndexNumber: 2, Genres: ['Phase 3 Genre'] });
    await expect(metadata.getByText('Metadata changes saved.', { exact: true })).toBeVisible();
    await metadata.getByRole('button', { name: 'Manage artwork', exact: true }).click();
    artwork = page.getByRole('dialog', { name: 'Manage artwork', exact: true });
    await upload(page, fixture, artwork, fixture.ImageUploadPath, `/admin/v1/items/${fixture.AudioItemId}/images/Primary/0`);
    await artwork.getByRole('button', { name: 'Close', exact: true }).click();
    await metadata.getByRole('button', { name: 'Close', exact: true }).filter({ hasText: /^Close$/ }).click();
    result.Checks.MusicSaved = true;
    await stage(fixture, result, 'music');

    await page.goto(`${fixture.BaseURL}/admin/tasks`);
    const taskCard = page.getByRole('list', { name: 'Available server tasks' }).getByRole('listitem').filter({ has: page.getByRole('heading', { name: fixture.CacheTaskName, exact: true }) });
    await taskCard.getByRole('button', { name: 'Edit schedule', exact: true }).click();
    const schedule = page.getByRole('dialog', { name: /^Edit schedule/ });
    await expect(schedule.getByRole('button', { name: 'Add trigger', exact: true })).toBeVisible();
    while (await schedule.getByRole('button', { name: /^Remove trigger / }).count()) await schedule.getByRole('button', { name: /^Remove trigger / }).first().click();
    await schedule.getByRole('button', { name: 'Add trigger', exact: true }).click();
    await select(page, schedule, 'When to run', 'On a system event');
    await select(page, schedule, 'System event', 'Server started');
    response = responseFor(page, fixture, 'POST', `/admin/v1/tasks/${fixture.CacheTaskId}/triggers/preview`);
    await schedule.getByRole('button', { name: 'Preview schedule', exact: true }).click();
    expect((await payload<{ Items: { Event: string; Occurrences: string[] }[] }>(response)).Items).toMatchObject([{ Event: 'ServerStarted', Occurrences: [] }]);
    response = responseFor(page, fixture, 'PUT', `/admin/v1/tasks/${fixture.CacheTaskId}/triggers`);
    await schedule.getByRole('button', { name: 'Save schedule', exact: true }).click();
    expect((await payload<TaskDefinitionResponse>(response)).Task.Triggers).toMatchObject([{ Kind: 'system_event', SystemEvent: 'ServerStarted', NextFireAt: null }]);
    await expect(schedule.getByText('Schedule saved.', { exact: true })).toBeVisible();
    await schedule.getByRole('button', { name: 'Close', exact: true }).click();
    result.Checks.TriggersSaved = true;
    await stage(fixture, result, 'schedule');
    await page.goto('about:blank');
    const restarted = await stage(fixture, result, 'restart');
    expect(restarted.BaseURL).toBe(fixture.BaseURL);
    expect(typeof restarted.StartupRunId).toBe('string');
    expect(restarted.StartupRunId?.length).toBeGreaterThan(0);
    result.StartupRunId = restarted.StartupRunId!;

    library = await libraryEditor(page, fixture, 'Phase 3 Music Edited');
    await expect(library.getByLabel('Directory 2', { exact: true })).toHaveValue(fixture.WritableDirectory);
    await expect(library.getByRole('checkbox', { name: 'Import local metadata files', exact: true })).not.toBeChecked();
    await expect(library.getByRole('checkbox', { name: 'Import images from media directories', exact: true })).not.toBeChecked();
    await library.getByRole('button', { name: 'Cancel', exact: true }).click();
    preferences = await preferencesEditor(page, fixture);
    await expect(preferences.getByLabel('Preferred audio language', { exact: true })).toHaveValue('eng');
    await expect(preferences.getByLabel('Preferred subtitle language', { exact: true })).toHaveValue('zho');
    await expect(preferences.getByRole('combobox', { name: 'Subtitle mode', exact: true })).toHaveText('Always');
    await expect(preferences.getByLabel('Rewind on resume (seconds)', { exact: true })).toHaveValue('10');
    await closePreferences(page, preferences);
    metadata = await metadataEditor(page, fixture);
    await expect(metadata.getByRole('textbox', { name: 'Album tag', exact: true })).toHaveValue('Phase 3 Album');
    await expect(metadata.getByRole('textbox', { name: 'Artist 2', exact: true })).toHaveValue('Guest Artist');
    await expect(metadata.getByRole('textbox', { name: 'Track number', exact: true })).toHaveValue('7');
    await expect(metadata.getByRole('textbox', { name: 'Disc number', exact: true })).toHaveValue('2');
    await metadata.getByRole('textbox', { name: 'Album tag', exact: true }).scrollIntoViewIfNeeded();
    await metadata.screenshot({ path: path.join(fixture.ArtifactsDir, 'music-persisted.png'), animations: 'disabled' });
    await metadata.getByRole('button', { name: 'Manage artwork', exact: true }).click();
    artwork = page.getByRole('dialog', { name: 'Manage artwork', exact: true });
    await expect(artwork.getByRole('list', { name: 'Saved images' }).getByRole('img')).toHaveCount(1);
    await expect.poll(() => artwork.getByRole('list', { name: 'Saved images' }).getByRole('img').evaluateAll((images) => images.every((element) => (element as HTMLImageElement).complete && (element as HTMLImageElement).naturalWidth > 0))).toBe(true);
    await artwork.getByRole('button', { name: 'Close', exact: true }).click();
    await metadata.getByRole('button', { name: 'Close', exact: true }).filter({ hasText: /^Close$/ }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(`${fixture.BaseURL}/admin/artwork`);
    await select(page, page.locator('main'), 'Catalog type', 'Music artists');
    await expect(page.getByRole('rowheader', { name: 'Phase 3 Artist', exact: true })).toBeVisible();
    await expect(page.getByRole('rowheader', { name: 'Guest Artist', exact: true })).toBeVisible();
    await expect(page.getByRole('listbox', { includeHidden: true })).toBeHidden();
    await page.screenshot({ path: path.join(fixture.ArtifactsDir, 'music-artists-mobile.png'), fullPage: true, animations: 'disabled' });
    await page.setViewportSize({ width: 1280, height: 900 });
    result.Checks.RestartPersisted = true;
    await stage(fixture, result, 'persistence');

    library = await libraryEditor(page, fixture, 'Phase 3 Music Edited');
    await library.getByRole('textbox', { name: 'Library name', exact: true }).fill('Unauthorized library edit');
    await stage(fixture, result, 'permission-revoked');
    response = responseFor(page, fixture, 'PATCH', `/admin/v1/libraries/${fixture.LibraryId}`);
    await library.getByRole('button', { name: 'Save library', exact: true }).click();
    expect([401, 403]).toContain((await response).status());
    await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    result.Checks.PermissionRevocation = true;
    await stage(fixture, result, 'permission-denied');
    await login(page, fixture);
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    await stage(fixture, result, 'cleanup');
    expect(result.PageErrors, 'No browser runtime errors are allowed.').toBe(0);
    expect(result.ForeignRequests, 'No external service is contacted.').toBe(0);
    result.Checks.Completed = true;
    result.Complete = true;
  } finally {
    writePrivateJSON(fixture.ResultPath, result, fixture.AdminPassword);
  }
});
