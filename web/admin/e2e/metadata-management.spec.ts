import { createHash } from 'node:crypto';
import { constants } from 'node:fs';
import { lstat, open, realpath, rename } from 'node:fs/promises';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import type { APIRequestContext, Locator, Page, Response } from '@playwright/test';
import type { Job, Library, MetadataDetail, MetadataFieldName, MetadataItemsResponse } from '../src/api';

interface FixtureManifest {
  Marker: string;
  RunId: string;
  Root: string;
  MoviesPath: string;
  TVPath: string;
  MoviePath: string;
  MovieNFOPath: string;
  EpisodePath: string;
  EpisodeNFOPath: string;
  MixedPath: string;
  MutableEpisodePath: string;
  MutableMoviePath: string;
  MutableNFOPath: string;
  MediaSHA256: Record<string, string>;
}

interface OwnedFixture {
  manifest: FixtureManifest;
  manifestPath: string;
  manifestDigest: string;
  mutableNFODigest: string;
}

const marker = 'goby-metadata-browser-fixtures-v1';
const rootPattern = /^\/dev\/shm\/goby-metadata-[A-Za-z0-9._-]+\/media$/;
const initialPremiere = '2024-01-02T16:30:45.123456789Z';
const rescannedPremiere = '2025-02-03T14:15:16.987654321Z';
const editor = (page: Page): Locator => page.getByRole('dialog', { name: /^Edit metadata/ });
const field = (page: Page, name: MetadataFieldName): Locator => editor(page).locator(`section[aria-labelledby="metadata-${name}-heading"]`);
const input = (page: Page, name: MetadataFieldName): Locator => editor(page).locator(`#metadata-${name}-input`);
const metadataPath = (id: string): string => `/admin/v1/items/${encodeURIComponent(id)}/metadata`;
const itemsPath = (id: string): string => `/admin/v1/libraries/${encodeURIComponent(id)}/items`;
const digest = (value: Buffer): string => createHash('sha256').update(value).digest('hex');

function contained(root: string, candidate: string): boolean {
  const relative = path.relative(root, candidate);
  return candidate === path.resolve(candidate) && path.isAbsolute(candidate)
    && (relative === '' || (!relative.startsWith(`..${path.sep}`) && relative !== '..' && !path.isAbsolute(relative)));
}

async function ownedPath(root: string, candidate: string, directory: boolean): Promise<void> {
  if (!rootPattern.test(root) || !contained(root, candidate)) throw new Error('The metadata fixture path is outside its owned media root.');
  let current = path.parse(candidate).root;
  const segments = candidate.slice(current.length).split(path.sep).filter(Boolean);
  for (const [index, segment] of segments.entries()) {
    current = path.join(current, segment);
    const status = await lstat(current);
    const needsDirectory = index < segments.length - 1 || directory;
    if (status.isSymbolicLink() || (needsDirectory ? !status.isDirectory() : !status.isFile())) {
      throw new Error('Metadata fixture paths must be regular files and directories without symbolic links.');
    }
  }
  if (await realpath(candidate) !== candidate) throw new Error('The metadata fixture path is not canonical.');
}

async function ownedRead(root: string, candidate: string, maximum = 2 * 1024 * 1024): Promise<Buffer> {
  await ownedPath(root, candidate, false);
  const file = await open(candidate, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const status = await file.stat();
    if (!status.isFile() || status.size > maximum || await realpath(`/proc/self/fd/${file.fd}`) !== candidate) {
      throw new Error('The opened metadata fixture is not the bounded owned regular file.');
    }
    return await file.readFile();
  } finally {
    await file.close();
  }
}

async function pathExists(candidate: string): Promise<boolean> {
  try {
    await lstat(candidate);
    return true;
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') return false;
    throw error;
  }
}

async function mutableMediaPath(fixture: OwnedFixture): Promise<string> {
  const manifest = fixture.manifest;
  if (manifest.MixedPath !== path.join(manifest.Root, 'mixed')
    || manifest.MutableEpisodePath !== path.join(manifest.MixedPath, 'Mutable.S01E01.mp4')
    || manifest.MutableMoviePath !== path.join(manifest.MixedPath, 'Reclassified.Movie.mp4')
    || path.dirname(manifest.MutableNFOPath) !== manifest.MixedPath
    || path.extname(manifest.MutableNFOPath).toLowerCase() !== '.nfo') {
    throw new Error('The mutable media paths do not match the declared same-directory rename pair.');
  }
  await ownedPath(manifest.Root, manifest.MixedPath, true);
  await ownedPath(manifest.Root, manifest.MutableNFOPath, false);
  if ((await lstat(manifest.MutableNFOPath)).nlink !== 1) throw new Error('The mutable fixture NFO must not have hard links.');
  const originalExists = await pathExists(manifest.MutableEpisodePath);
  const renamedExists = await pathExists(manifest.MutableMoviePath);
  if (originalExists === renamedExists) throw new Error('Exactly one member of the declared media rename pair must exist.');
  const existing = originalExists ? manifest.MutableEpisodePath : manifest.MutableMoviePath;
  await ownedPath(manifest.Root, existing, false);
  if ((await lstat(existing)).nlink !== 1) throw new Error('The mutable media fixture must be a single-link regular file.');
  return existing;
}

async function verifyOwnedFixture(fixture: OwnedFixture, requireOriginalName = false): Promise<void> {
  const manifest = fixture.manifest;
  await ownedPath(manifest.Root, manifest.Root, true);
  if (digest(await ownedRead(manifest.Root, fixture.manifestPath)) !== fixture.manifestDigest
    || (await ownedRead(manifest.Root, path.join(manifest.Root, '.goby-metadata-fixtures'), 256)).toString('utf8').trim() !== marker) {
    throw new Error('The metadata fixture manifest or ownership marker changed.');
  }
  await ownedPath(manifest.Root, manifest.MoviesPath, true);
  await ownedPath(manifest.Root, manifest.TVPath, true);
  const mutablePath = await mutableMediaPath(fixture);
  if (requireOriginalName && mutablePath !== manifest.MutableEpisodePath) throw new Error('The mutable fixture must have its original episode pathname.');
  if (digest(await ownedRead(manifest.Root, manifest.MutableNFOPath)) !== fixture.mutableNFODigest) {
    throw new Error('The immutable NFO for the type-change fixture was modified.');
  }
  for (const [nfo, media, library] of [
    [manifest.MovieNFOPath, manifest.MoviePath, manifest.MoviesPath],
    [manifest.EpisodeNFOPath, manifest.EpisodePath, manifest.TVPath],
  ]) {
    if (!contained(library, nfo) || !contained(library, media) || path.dirname(nfo) !== path.dirname(media)
      || path.extname(nfo).toLowerCase() !== '.nfo') throw new Error('The NFO is not the declared media file companion.');
    await ownedPath(manifest.Root, nfo, false);
    if ((await lstat(nfo)).nlink !== 1) throw new Error('Mutable NFO fixtures must not have hard links.');
  }
  const entries = Object.entries(manifest.MediaSHA256);
  if (entries.length < 28 || entries.length > 200) throw new Error('The metadata journey requires its bounded pagination, episode, and type-change fixtures.');
  for (const required of [manifest.MoviePath, manifest.EpisodePath, manifest.MutableEpisodePath]) {
    if (!Object.hasOwn(manifest.MediaSHA256, path.relative(manifest.Root, required))) throw new Error('A declared media file is missing from the fixture hash manifest.');
  }
  if (Object.hasOwn(manifest.MediaSHA256, path.relative(manifest.Root, manifest.MutableMoviePath))) {
    throw new Error('The planned rename destination must not represent an additional media file.');
  }
  for (const [relative, expected] of entries) {
    if (!relative || path.isAbsolute(relative) || relative !== path.normalize(relative) || !/^[a-f0-9]{64}$/.test(expected)) {
      throw new Error('The media hash manifest contains an invalid entry.');
    }
    const declared = path.join(manifest.Root, relative);
    const target = declared === manifest.MutableEpisodePath ? mutablePath : declared;
    if (digest(await ownedRead(manifest.Root, target)) !== expected) throw new Error('Owned media bytes no longer match the fixture manifest.');
  }
}

async function loadOwnedFixture(manifestPath: string): Promise<OwnedFixture> {
  if (process.platform !== 'linux' || manifestPath !== path.resolve(manifestPath)) throw new Error('Metadata fixtures must run on Linux with a canonical absolute manifest path.');
  const root = path.dirname(manifestPath);
  if (!rootPattern.test(root)) throw new Error('The metadata manifest must live directly in its dedicated /dev/shm media root.');
  const raw = await ownedRead(root, manifestPath);
  const manifest = JSON.parse(raw.toString('utf8')) as FixtureManifest;
  if (!manifest || manifest.Marker !== marker || manifest.Root !== root || typeof manifest.RunId !== 'string'
    || !/^[A-Za-z0-9._-]{1,128}$/.test(manifest.RunId)
    || !manifest.MediaSHA256 || typeof manifest.MediaSHA256 !== 'object' || Array.isArray(manifest.MediaSHA256)
    || ['MoviesPath', 'TVPath', 'MoviePath', 'MovieNFOPath', 'EpisodePath', 'EpisodeNFOPath', 'MixedPath', 'MutableEpisodePath', 'MutableMoviePath', 'MutableNFOPath'].some((key) => typeof manifest[key as keyof FixtureManifest] !== 'string')) {
    throw new Error('The metadata fixture manifest does not match the isolated fixture contract.');
  }
  const fixture = { manifest, manifestPath, manifestDigest: digest(raw), mutableNFODigest: digest(await ownedRead(root, manifest.MutableNFOPath)) };
  await verifyOwnedFixture(fixture, true);
  return fixture;
}

async function replaceOwnedNFO(fixture: OwnedFixture, kind: 'movie' | 'episode', content: string): Promise<void> {
  await verifyOwnedFixture(fixture);
  const target = kind === 'movie' ? fixture.manifest.MovieNFOPath : fixture.manifest.EpisodeNFOPath;
  const file = await open(target, constants.O_RDWR | constants.O_NOFOLLOW);
  try {
    const status = await file.stat();
    if (!status.isFile() || status.nlink !== 1 || await realpath(`/proc/self/fd/${file.fd}`) !== target) {
      throw new Error('Refusing to write an NFO outside the verified owned fixture.');
    }
    await file.truncate(0);
    await file.writeFile(content, 'utf8');
    await file.sync();
  } finally {
    await file.close();
  }
  await verifyOwnedFixture(fixture);
}

async function renameOwnedMedia(fixture: OwnedFixture, destination: 'movie' | 'episode'): Promise<void> {
  await verifyOwnedFixture(fixture);
  const manifest = fixture.manifest;
  const source = destination === 'movie' ? manifest.MutableEpisodePath : manifest.MutableMoviePath;
  const target = destination === 'movie' ? manifest.MutableMoviePath : manifest.MutableEpisodePath;
  if (await mutableMediaPath(fixture) !== source || await pathExists(target)) {
    throw new Error('The declared rename source or absent destination changed.');
  }
  // The runner owns this directory exclusively; the server has read access.
  // Hold the opened source across rename and verify its inode, size, mtime, and
  // actual descriptor path before accepting the new name. Never replace a file.
  const file = await open(source, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const before = await file.stat({ bigint: true });
    if (!before.isFile() || before.nlink !== 1n || await realpath(`/proc/self/fd/${file.fd}`) !== source || await pathExists(target)) {
      throw new Error('The opened rename source is not the verified single-link fixture.');
    }
    await rename(source, target);
    const after = await lstat(target, { bigint: true });
    if (!after.isFile() || after.isSymbolicLink() || after.nlink !== 1n
      || after.ino !== before.ino || after.dev !== before.dev || after.size !== before.size || after.mtimeNs !== before.mtimeNs
      || await pathExists(source) || await realpath(`/proc/self/fd/${file.fd}`) !== target) {
      throw new Error('The planned media rename did not preserve the exact owned file.');
    }
  } finally {
    await file.close();
  }
  await verifyOwnedFixture(fixture, destination === 'episode');
}

function responseFor(page: Page, method: string, pathname: string): Promise<Response> {
  return page.waitForResponse((response) => response.request().method() === method && new URL(response.url()).pathname === pathname);
}

async function signIn(page: Page, name: string, password: string): Promise<void> {
  await page.goto('/admin/libraries');
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
  await page.getByLabel(/^Username/).fill(name);
  await page.getByLabel(/^Password/).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Libraries', exact: true })).toBeVisible();
}

async function completedScan(page: Page, request: APIRequestContext, job: Job): Promise<void> {
  await page.bringToFront();
  await page.getByRole('button', { name: 'View tasks', exact: true }).first().click();
  await expect(page.getByRole('heading', { name: 'Tasks', exact: true })).toBeVisible();
  const card = page.locator(`[data-job-id="${job.Id}"]`);
  await expect(card).toBeVisible();
  await expect(card.getByText('Completed', { exact: true })).toBeVisible({ timeout: 90_000 });
  const response = await request.get('/admin/v1/jobs');
  expect(response.status()).toBe(200);
  const result = await response.json() as { Items: Job[] };
  const completed = result.Items.find((entry) => entry.Id === job.Id);
  expect(completed?.Status).toBe('completed');
  expect(completed?.Error).toBe('');
  expect(completed?.Scanned).toBeGreaterThan(0);
  await page.getByRole('button', { name: 'Manage libraries', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Libraries', exact: true })).toBeVisible();
}

async function createScannedLibrary(page: Page, request: APIRequestContext, name: string, mediaPath: string, type: 'Movies' | 'TV shows' | 'Mixed media'): Promise<Library> {
  await page.getByRole('button', { name: 'Create library', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Create library', exact: true });
  await dialog.getByLabel(/^Library name/).fill(name);
  await dialog.getByRole('combobox', { name: /^Content type/ }).click();
  await page.getByRole('option', { name: type, exact: true }).click();
  await dialog.getByLabel(/^Media directories/).fill(mediaPath);
  await dialog.getByRole('checkbox', { name: /^Scan after creating/ }).check();
  const [response] = await Promise.all([
    responseFor(page, 'POST', '/admin/v1/libraries'),
    dialog.getByRole('button', { name: 'Create library', exact: true }).click(),
  ]);
  expect(response.status()).toBe(201);
  const result = await response.json() as { Library: Library; Job?: Job; ScanError?: unknown };
  expect(result.Library.Name).toBe(name);
  expect(result.Library.Paths).toEqual([mediaPath]);
  expect(result.ScanError).toBeUndefined();
  expect(result.Job).toBeDefined();
  await expect(dialog).not.toBeVisible();
  await completedScan(page, request, result.Job!);
  return result.Library;
}

async function scanLibrary(page: Page, request: APIRequestContext, library: Library): Promise<void> {
  const card = page.getByRole('list', { name: 'Media libraries', exact: true }).getByRole('listitem').filter({ has: page.getByRole('heading', { name: library.Name, exact: true }) });
  const [response] = await Promise.all([
    responseFor(page, 'POST', `/admin/v1/libraries/${encodeURIComponent(library.Id)}/scan`),
    card.getByRole('button', { name: 'Scan library', exact: true }).click(),
  ]);
  expect(response.status()).toBe(202);
  const result = await response.json() as { Job: Job };
  await completedScan(page, request, result.Job);
}

async function itemsAction(page: Page, library: Library, action: () => Promise<unknown>, matches: (parameters: URLSearchParams) => boolean = () => true): Promise<MetadataItemsResponse> {
  const response = page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return candidate.request().method() === 'GET' && url.pathname === itemsPath(library.Id) && matches(url.searchParams);
  });
  const [loaded] = await Promise.all([response, action()]);
  expect(loaded.status()).toBe(200);
  const data = await loaded.json() as MetadataItemsResponse;
  expect(data.Library.Id).toBe(library.Id);
  expect(data.Items.every((item) => item.LibraryId === library.Id)).toBe(true);
  await expect(page.getByRole('button', { name: 'Search', exact: true })).toBeEnabled();
  return data;
}

async function manageLibrary(page: Page, library: Library): Promise<MetadataItemsResponse> {
  return itemsAction(page, library, () => page.getByRole('button', { name: `Manage items in ${library.Name}`, exact: true }).click());
}

async function searchTitle(page: Page, library: Library, title: string): Promise<MetadataItemsResponse> {
  await page.getByRole('textbox', { name: 'Search title', exact: true }).fill(title);
  return itemsAction(page, library, () => page.getByRole('button', { name: 'Search', exact: true }).click(), (parameters) => parameters.get('SearchTerm') === title && parameters.get('StartIndex') === '0');
}

async function selectType(page: Page, library: Library, type: string, expected: string): Promise<MetadataItemsResponse> {
  await page.getByRole('combobox', { name: 'Types', exact: true }).click();
  return itemsAction(page, library, async () => {
    await page.getByRole('option', { name: type, exact: true }).click();
    // Multiple selections keep their modal menu open until the user closes it.
    await page.keyboard.press('Escape');
  }, (parameters) => parameters.get('Types') === expected && parameters.get('StartIndex') === '0');
}

async function openItem(page: Page, library: Library, title: string, ownedMediaPath: string, expectedInactive: MetadataFieldName[] = []): Promise<MetadataDetail> {
  const results = await searchTitle(page, library, title);
  const selected = results.Items.find((item) => item.Path === ownedMediaPath);
  expect(selected, 'The editor must select the exact owned media fixture.').toBeDefined();
  const [response] = await Promise.all([
    responseFor(page, 'GET', metadataPath(selected!.Id)),
    page.getByRole('button', { name: `Edit metadata for ${selected!.Name}`, exact: true }).click(),
  ]);
  expect(response.status()).toBe(200);
  const detail = await response.json() as MetadataDetail;
  expect(detail.Item.Id).toBe(selected!.Id);
  expect(detail.Item.LibraryId).toBe(library.Id);
  expect(detail.Item.Path).toBe(ownedMediaPath);
  expect([...detail.InactiveFields].sort()).toEqual([...expectedInactive].sort());
  await expect(editor(page)).toBeVisible();
  await expect(input(page, 'Name')).toHaveValue(detail.Effective.Name);
  return detail;
}

async function readMetadata(request: APIRequestContext, expected: MetadataDetail): Promise<MetadataDetail> {
  const response = await request.get(metadataPath(expected.Item.Id));
  expect(response.status()).toBe(200);
  const detail = await response.json() as MetadataDetail;
  expect(detail.Item.Id).toBe(expected.Item.Id);
  expect(detail.Item.LibraryId).toBe(expected.Item.LibraryId);
  expect(detail.Item.Path).toBe(expected.Item.Path);
  return detail;
}

async function saveMetadata(page: Page, previous: MetadataDetail, expectedInactive: MetadataFieldName[] = []): Promise<MetadataDetail> {
  const [response] = await Promise.all([
    responseFor(page, 'PUT', metadataPath(previous.Item.Id)),
    editor(page).getByRole('button', { name: 'Save changes', exact: true }).click(),
  ]);
  expect(response.status()).toBe(200);
  const detail = await response.json() as MetadataDetail;
  expect(detail.Item.Id).toBe(previous.Item.Id);
  expect(detail.Item.Path).toBe(previous.Item.Path);
  expect(detail.Item.ParentId).toBe(previous.Item.ParentId);
  expect([...detail.InactiveFields].sort()).toEqual([...expectedInactive].sort());
  expect(detail.Revision).toMatch(/^[1-9][0-9]*$/);
  expect(BigInt(detail.Revision)).toBeGreaterThan(BigInt(previous.Revision));
  expect(detail.LastEditedBy).toBeTruthy();
  expect(detail.LastEditedAt).not.toBeNull();
  await expect(editor(page).getByText('Metadata changes saved.', { exact: true })).toBeVisible();
  await expect(editor(page).getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  return detail;
}

async function closeEditor(page: Page): Promise<void> {
  await editor(page).getByRole('button', { name: 'Close', exact: true }).filter({ hasText: /^Close$/ }).click();
  await expect(editor(page)).not.toBeVisible();
}

async function tab(page: Page, name: 'Details' | 'People & categories' | 'Identifiers'): Promise<void> {
  await editor(page).getByRole('tab', { name, exact: true }).click();
}

async function useAutomatic(page: Page, fields: Array<[MetadataFieldName, string]>): Promise<void> {
  for (const [name, label] of fields) await field(page, name).getByRole('button', { name: `Use automatic ${label}`, exact: true }).click();
}

async function visibleControl(page: Page, control: Locator): Promise<void> {
  await control.scrollIntoViewIfNeeded();
  await expect(control).toBeVisible();
  const bounds = await control.boundingBox();
  const viewport = page.viewportSize();
  expect(bounds).not.toBeNull();
  expect(viewport).not.toBeNull();
  expect(bounds!.x).toBeGreaterThanOrEqual(-1);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(viewport!.width + 1);
  expect(bounds!.y).toBeGreaterThanOrEqual(-1);
  expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(viewport!.height + 1);
}

async function selectPersonType(page: Page, person: Locator, type: 'Actor' | 'Director'): Promise<void> {
  const control = person.getByRole('combobox', { name: /^Type/ });
  await visibleControl(page, control);
  await control.click();
  await page.getByRole('option', { name: type, exact: true }).click();
  await expect(control).toHaveText(type);
}

// The orchestrator creates and later removes this entire database and fixture
// tree. This spec never deletes users/libraries, edits the dedicated admin, or
// writes shared media. Only two declared NFO files may change content; the one
// declared media rename pair is restored to its original pathname before exit.
// Automatic failure screenshots are disabled because failures can occur during
// login. Only explicitly named, non-secret metadata screenshots are produced.
test.use({ screenshot: 'off', trace: 'off', video: 'off' });

test('isolated metadata scans, overrides, locks, conflict recovery, and mobile episode editing', async ({ page, context }, testInfo) => {
  test.skip(process.env.GOBY_SMOKE_METADATA_DISPOSABLE_DATABASE !== '1'
    || process.env.GOBY_SMOKE_METADATA_DEDICATED_ADMIN !== '1',
  'Explicit disposable-database and dedicated-administrator confirmations are required.');
  test.setTimeout(300_000);
  const name = process.env.GOBY_SMOKE_NAME;
  const password = process.env.GOBY_SMOKE_PASSWORD;
  const baseURL = process.env.GOBY_SMOKE_BASE_URL;
  const manifestPath = process.env.GOBY_SMOKE_METADATA_FIXTURE_MANIFEST;
  if (!name || !password || !baseURL || !manifestPath) throw new Error('Metadata smoke credentials, base URL, and owned fixture manifest are required.');
  expect(new URL(testInfo.project.use.baseURL ?? '').origin).toBe(new URL(baseURL).origin);
  const fixture = await loadOwnedFixture(manifestPath);
  const manifest = fixture.manifest;
  const initialMovieNFO = digest(await ownedRead(manifest.Root, manifest.MovieNFOPath));
  const initialEpisodeNFO = digest(await ownedRead(manifest.Root, manifest.EpisodeNFOPath));
  let pageErrors = 0;
  const observePage = (observed: Page) => observed.on('pageerror', () => { pageErrors += 1; });
  observePage(page);
  context.on('page', observePage);
  await signIn(page, name, password);

  const sessionResponse = await context.request.get('/admin/v1/session');
  expect(sessionResponse.status()).toBe(200);
  const session = await sessionResponse.json() as { User: { Id: string; Name: string; IsAdministrator: boolean } };
  expect(session.User.Name).toBe(name);
  expect(session.User.IsAdministrator).toBe(true);
  const usersResponse = await context.request.get('/admin/v1/users');
  expect(usersResponse.status()).toBe(200);
  const users = await usersResponse.json() as { Items: Array<{ Id: string }> };
  expect(users.Items.map((user) => user.Id)).toEqual([session.User.Id]);
  const librariesResponse = await context.request.get('/admin/v1/libraries');
  expect(librariesResponse.status()).toBe(200);
  expect((await librariesResponse.json() as { Items: unknown[] }).Items).toEqual([]);

  const movies = await createScannedLibrary(page, context.request, `Metadata movies ${manifest.RunId}`, manifest.MoviesPath, 'Movies');
  const television = await createScannedLibrary(page, context.request, `Metadata television ${manifest.RunId}`, manifest.TVPath, 'TV shows');
  const mixed = await createScannedLibrary(page, context.request, `Metadata mixed ${manifest.RunId}`, manifest.MixedPath, 'Mixed media');
  await manageLibrary(page, movies);

  await test.step('browse owned movies through search, type filtering, and real pagination', async () => {
    const filtered = await selectType(page, movies, 'Movie', 'Movie');
    expect(filtered.TotalRecordCount).toBeGreaterThan(25);
    expect(filtered.Items.every((item) => item.Type === 'Movie')).toBe(true);
    await page.getByRole('combobox', { name: /Items per page/ }).click();
    const first = await itemsAction(page, movies, () => page.getByRole('option', { name: '25', exact: true }).click(), (parameters) => parameters.get('Limit') === '25' && parameters.get('StartIndex') === '0');
    expect(first.Items).toHaveLength(25);
    const second = await itemsAction(page, movies, () => page.getByRole('button', { name: 'Go to next page', exact: true }).click(), (parameters) => parameters.get('StartIndex') === '25');
    expect(second.StartIndex).toBe(25);
    expect(second.Items.length).toBeGreaterThan(0);
    expect(second.Items.every((item) => !first.Items.some((earlier) => earlier.Id === item.Id))).toBe(true);
    await itemsAction(page, movies, () => page.getByRole('button', { name: 'Go to previous page', exact: true }).click(), (parameters) => parameters.get('StartIndex') === '0');
    await page.screenshot({ path: testInfo.outputPath('metadata-items-desktop.png'), fullPage: true, animations: 'disabled' });
  });

  let current = await openItem(page, movies, 'Automatic movie', manifest.MoviePath);
  expect(current.Automatic).toMatchObject({ Name: 'Automatic movie', OriginalTitle: 'Original automatic', Overview: 'Automatic overview one', ProductionYear: 2024, CommunityRating: 7.5, PremiereDate: initialPremiere, Genres: ['Drama'], Tags: ['Initial'], Studios: ['Initial Studio'], ProviderIds: { Tmdb: '100' }, People: [{ Name: 'Initial Actor', Role: 'Lead', Type: 'Actor', SortOrder: 0 }] });
  expect(current.Overrides).toEqual({});
  expect(current.LockedFields).toEqual([]);
  const originalSort = current.Effective.SortName;

  await test.step('keep dirty drafts and save typed manual values plus prospective locks', async () => {
    await input(page, 'Name').fill('Curated movie');
    const confirmation = page.waitForEvent('dialog').then(async (dialog) => {
      const observed = { type: dialog.type(), message: dialog.message() };
      await dialog.dismiss();
      return observed;
    });
    const [, dismissed] = await Promise.all([page.goBack({ waitUntil: 'commit' }), confirmation]);
    expect(dismissed).toEqual({ type: 'confirm', message: 'Discard unsaved metadata changes and leave this page?' });
    await expect(page).toHaveURL(new RegExp(`/admin/libraries/${movies.Id}/items$`));
    await expect(input(page, 'Name')).toHaveValue('Curated movie');
    await editor(page).getByRole('button', { name: 'Close', exact: true }).click();
    const discard = page.getByRole('dialog', { name: 'Discard unsaved metadata changes?', exact: true });
    await discard.getByRole('button', { name: 'Keep editing', exact: true }).click();
    await expect(input(page, 'Name')).toHaveValue('Curated movie');
    await input(page, 'Overview').fill('Curated overview');
    await input(page, 'CommunityRating').fill('');
    for (const label of ['Title', 'Original title', 'Premiere date']) await editor(page).getByRole('checkbox', { name: `Lock ${label}`, exact: true }).check();
    await expect(field(page, 'OriginalTitle').getByText('Lock on save', { exact: true })).toBeVisible();
    await tab(page, 'People & categories');
    await editor(page).getByRole('button', { name: 'Clear all tags', exact: true }).click();
    await editor(page).getByRole('button', { name: 'Add genre', exact: true }).click();
    await editor(page).getByLabel('Genre 2', { exact: true }).fill('Drama, comedy');
    await editor(page).getByRole('button', { name: 'Add studio', exact: true }).click();
    await editor(page).getByLabel('Studio 2', { exact: true }).fill('Curated Studio');
    await editor(page).getByRole('region', { name: 'Person 1', exact: true }).getByLabel('Role', { exact: true }).fill('Narrator');
    await editor(page).getByRole('button', { name: 'Add person', exact: true }).click();
    const person = editor(page).getByRole('region', { name: 'Person 2', exact: true });
    await person.getByLabel(/^Name/).fill('Initial Actor');
    await person.getByLabel('Role', { exact: true }).fill('Guest');
    await selectPersonType(page, person, 'Actor');
    await person.getByLabel('Sort order', { exact: true }).fill('2');
    await tab(page, 'Identifiers');
    await editor(page).getByRole('region', { name: 'Provider identifier 1', exact: true }).getByLabel('Identifier', { exact: true }).fill('101');
    await editor(page).getByRole('button', { name: 'Add provider identifier', exact: true }).click();
    const provider = editor(page).getByRole('region', { name: 'Provider identifier 2', exact: true });
    await provider.getByLabel('Provider', { exact: true }).fill('Imdb');
    await provider.getByLabel('Identifier', { exact: true }).fill('tt1234567');
    current = await saveMetadata(page, current);
    expect(current.Overrides).toMatchObject({ Name: 'Curated movie', Overview: 'Curated overview', CommunityRating: null, Tags: [], Genres: ['Drama', 'Drama, comedy'], Studios: ['Initial Studio', 'Curated Studio'], ProviderIds: { Tmdb: '101', Imdb: 'tt1234567' } });
    expect(current.Overrides.People).toEqual([{ Name: 'Initial Actor', Role: 'Narrator', Type: 'Actor', SortOrder: 0 }, { Name: 'Initial Actor', Role: 'Guest', Type: 'Actor', SortOrder: 2 }]);
    expect(current.Overrides).not.toHaveProperty('SortName');
    expect(current.Effective.SortName).toBe(originalSort);
    expect(current.LockedValues).toMatchObject({ Name: 'Curated movie', OriginalTitle: 'Original automatic', PremiereDate: initialPremiere });
    expect(current.Effective.PremiereDate).toBe(initialPremiere);
    expect(digest(await ownedRead(manifest.Root, manifest.MovieNFOPath))).toBe(initialMovieNFO);
    await closeEditor(page);
  });

  await test.step('normal scans update Automatic while manual overrides and old locks remain', async () => {
    await page.getByRole('button', { name: 'Back to libraries', exact: true }).click();
    await replaceOwnedNFO(fixture, 'movie', `<?xml version="1.0" encoding="UTF-8"?>
<movie><title>Automatic movie rescanned</title><sorttitle>Source sort two</sorttitle><originaltitle>Original rescanned</originaltitle>
<plot>Automatic overview two</plot><year>2025</year><rating>8.25</rating><premiered>${rescannedPremiere}</premiered>
<genre>Adventure</genre><tag>Rescanned</tag><studio>Rescanned Studio</studio><uniqueid type="tmdb">200</uniqueid>
<actor><name>Rescanned Actor</name><role>Guest</role><order>1</order></actor></movie>\n`);
    await scanLibrary(page, context.request, movies);
    await manageLibrary(page, movies);
    current = await openItem(page, movies, 'Curated movie', manifest.MoviePath);
    expect(current.Automatic).toMatchObject({ Name: 'Automatic movie rescanned', OriginalTitle: 'Original rescanned', Overview: 'Automatic overview two', ProductionYear: 2025, CommunityRating: 8.25, PremiereDate: rescannedPremiere, Tags: ['Rescanned'], Genres: ['Adventure'], ProviderIds: { Tmdb: '200' } });
    expect(current.Effective).toMatchObject({ Name: 'Curated movie', Overview: 'Curated overview', OriginalTitle: 'Original automatic', PremiereDate: initialPremiere, CommunityRating: null, Tags: [], ProductionYear: 2025 });
    expect(current.Effective.SortName).toBe(current.Automatic.SortName);
    for (const name of ['Name', 'OriginalTitle', 'PremiereDate'] as const) await field(page, name).getByRole('button', { name: 'Compare source', exact: true }).click();
    await expect(editor(page).locator('#metadata-Name-comparison')).toContainText('Automatic movie rescanned');
    await expect(editor(page).locator('#metadata-OriginalTitle-comparison')).toContainText('Original automatic');
    await expect(editor(page).locator('#metadata-PremiereDate-comparison')).toContainText(rescannedPremiere);
    await expect(field(page, 'OriginalTitle').getByText('Locked automatic', { exact: true })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath('metadata-editor-desktop.png'), fullPage: true, animations: 'disabled' });
    await input(page, 'Name').fill('Curated movie revised');
    current = await saveMetadata(page, current);
    expect(current.Effective.Name).toBe('Curated movie revised');
    expect(current.LockedValues.Name).toBe('Curated movie');
    await editor(page).getByRole('checkbox', { name: 'Lock Title', exact: true }).uncheck();
    current = await saveMetadata(page, current);
    expect(current.Overrides.Name).toBe('Curated movie revised');
    expect(current.LockedFields).not.toContain('Name');
  });

  await test.step('Use automatic removes overrides and locks rather than storing empty values', async () => {
    await useAutomatic(page, [['Name', 'Title'], ['Overview', 'Overview'], ['OriginalTitle', 'Original title'], ['PremiereDate', 'Premiere date'], ['CommunityRating', 'Community rating']]);
    await tab(page, 'People & categories');
    await useAutomatic(page, [['People', 'People'], ['Genres', 'Genres'], ['Tags', 'Tags'], ['Studios', 'Studios']]);
    await tab(page, 'Identifiers');
    await useAutomatic(page, [['ProviderIds', 'Provider identifiers']]);
    current = await saveMetadata(page, current);
    expect(current.Overrides).toEqual({});
    expect(current.LockedFields).toEqual([]);
    expect(current.LockedValues).toEqual({});
    expect(current.Effective).toEqual(current.Automatic);
    expect(current.Effective.CommunityRating).toBe(8.25);
    expect(current.Effective.Tags).toEqual(['Rescanned']);
    expect(current.Effective.PremiereDate).toBe(rescannedPremiere);
    await tab(page, 'Details');
  });

  await test.step('two real editors produce a conflict without losing the stale draft', async () => {
    const other = await context.newPage();
    try {
      await other.goto('/admin/libraries');
      await manageLibrary(other, movies);
      const concurrent = await openItem(other, movies, current.Effective.Name, manifest.MoviePath);
      expect(concurrent.Revision).toBe(current.Revision);
      await input(page, 'Overview').fill('Unsaved conflict draft');
      await input(other, 'Name').fill('Concurrent movie winner');
      const winner = await saveMetadata(other, concurrent);
      const [response] = await Promise.all([responseFor(page, 'PUT', metadataPath(current.Item.Id)), editor(page).getByRole('button', { name: 'Save changes', exact: true }).click()]);
      expect(response.status()).toBe(409);
      expect((await response.json() as { Error: { Code: string } }).Error.Code).toBe('revision_conflict');
      await expect(editor(page).getByText(/Your draft is still here\./)).toBeVisible();
      await expect(input(page, 'Overview')).toHaveValue('Unsaved conflict draft');
      await expect(editor(page).getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
      expect((await readMetadata(context.request, current)).Effective.Name).toBe(winner.Effective.Name);
      await editor(page).getByRole('button', { name: 'Reload latest metadata', exact: true }).click();
      const discard = page.getByRole('dialog', { name: 'Discard unsaved metadata changes?', exact: true });
      await discard.getByRole('button', { name: 'Keep editing', exact: true }).click();
      await expect(input(page, 'Overview')).toHaveValue('Unsaved conflict draft');
      await editor(page).getByRole('button', { name: 'Reload latest metadata', exact: true }).click();
      const [reloaded] = await Promise.all([responseFor(page, 'GET', metadataPath(current.Item.Id)), discard.getByRole('button', { name: 'Discard draft and reload', exact: true }).click()]);
      expect(reloaded.status()).toBe(200);
      current = await reloaded.json() as MetadataDetail;
      expect(current.Revision).toBe(winner.Revision);
      await expect(input(page, 'Name')).toHaveValue('Concurrent movie winner');
      await expect(input(page, 'Overview')).toHaveValue('Automatic overview two');
      await input(page, 'Overview').fill('Reviewed after conflict');
      current = await saveMetadata(page, current);
      await closeEditor(page);
      await closeEditor(other);
    } finally {
      await other.close();
    }
  });

  await test.step('mobile controls edit a real episode while preserving its hierarchy', async () => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: 'Back to libraries', exact: true }).click();
    await manageLibrary(page, television);
    await expect(page.getByRole('list', { name: 'Library items', exact: true })).toBeVisible();
    const series = await selectType(page, television, 'Series', 'Series');
    expect(series.Items.every((item) => item.Type === 'Series')).toBe(true);
    const selectedTypes = await selectType(page, television, 'Episode', 'Series,Episode');
    expect(new Set(selectedTypes.Items.map((item) => item.Type))).toEqual(new Set(['Series', 'Episode']));
    let episode = await openItem(page, television, 'Automatic episode', manifest.EpisodePath);
    expect(episode.Automatic.IndexNumber).toBe(1);
    expect(episode.Automatic.ParentIndexNumber).toBe(1);
    expect(episode.EditableFields).toContain('IndexNumber');
    expect(episode.EditableFields).not.toContain('ParentIndexNumber');
    await visibleControl(page, input(page, 'ParentIndexNumber'));
    await expect(input(page, 'ParentIndexNumber')).toHaveValue('1');
    await expect(input(page, 'ParentIndexNumber')).toBeDisabled();
    await expect(editor(page).getByRole('checkbox', { name: 'Lock Season number', exact: true })).toBeDisabled();
    await expect(field(page, 'ParentIndexNumber').getByRole('button', { name: 'Use automatic Season number', exact: true })).toBeDisabled();
    const originalParent = episode.Item.ParentId;
    // An episode number may be zero, but clearing it cannot persist null. This
    // assertion accepts validation in the form or the server's field response.
    await input(page, 'IndexNumber').fill('');
    await editor(page).getByRole('button', { name: 'Save changes', exact: true }).click();
    await expect(input(page, 'IndexNumber')).toHaveAttribute('aria-invalid', 'true');
    const rejectedNumber = await readMetadata(context.request, episode);
    expect(rejectedNumber.Revision).toBe(episode.Revision);
    expect(rejectedNumber.Effective.IndexNumber).toBe(1);
    const changes: Array<[MetadataFieldName, string]> = [['Name', 'Curated mobile episode'], ['SortName', 'Independent mobile sort'], ['OriginalTitle', 'Mobile original'], ['Overview', 'Mobile overview'], ['ProductionYear', '2026'], ['PremiereDate', '2026-03-04'], ['OfficialRating', 'TV-PG'], ['CommunityRating', '9.1'], ['IndexNumber', '7']];
    for (const [name, value] of changes) {
      await visibleControl(page, input(page, name));
      await input(page, name).fill(value);
    }
    for (const label of ['Title', 'Episode number']) await editor(page).getByRole('checkbox', { name: `Lock ${label}`, exact: true }).check();
    await expect(editor(page).getByRole('region', { name: 'Item location', exact: true }).getByRole('textbox')).toHaveCount(0);
    await tab(page, 'People & categories');
    for (const [plural, singular, value] of [['Genres', 'Genre', 'Mobile drama'], ['Tags', 'Tag', 'Mobile tag'], ['Studios', 'Studio', 'Mobile Studio']]) {
      const collection = field(page, plural as MetadataFieldName);
      const clear = collection.getByRole('button', { name: `Clear all ${plural.toLowerCase()}`, exact: true });
      if (await clear.isEnabled()) await clear.click();
      await collection.getByRole('button', { name: `Add ${singular.toLowerCase()}`, exact: true }).click();
      const control = collection.getByLabel(`${singular} 1`, { exact: true });
      await visibleControl(page, control);
      await control.fill(value);
    }
    const clearPeople = field(page, 'People').getByRole('button', { name: 'Clear all people', exact: true });
    if (await clearPeople.isEnabled()) await clearPeople.click();
    await editor(page).getByRole('button', { name: 'Add person', exact: true }).click();
    const person = editor(page).getByRole('region', { name: 'Person 1', exact: true });
    for (const [label, value] of [[/^Name/, 'Mobile Actor'], [/^Role$/, 'Lead'], [/^Sort order$/, '0']] as const) {
      const control = person.getByLabel(label);
      await visibleControl(page, control);
      await control.fill(value);
    }
    await selectPersonType(page, person, 'Actor');
    await tab(page, 'Identifiers');
    const clearProviders = field(page, 'ProviderIds').getByRole('button', { name: 'Clear all provider identifiers', exact: true });
    if (await clearProviders.isEnabled()) await clearProviders.click();
    await editor(page).getByRole('button', { name: 'Add provider identifier', exact: true }).click();
    const provider = editor(page).getByRole('region', { name: 'Provider identifier 1', exact: true });
    await visibleControl(page, provider.getByLabel('Provider', { exact: true }));
    await provider.getByLabel('Provider', { exact: true }).fill('Tmdb');
    await provider.getByLabel('Identifier', { exact: true }).fill('300');
    await visibleControl(page, editor(page).getByRole('button', { name: 'Save changes', exact: true }));
    episode = await saveMetadata(page, episode);
    expect(episode.Effective).toMatchObject({ Name: 'Curated mobile episode', SortName: 'Independent mobile sort', OriginalTitle: 'Mobile original', Overview: 'Mobile overview', ProductionYear: 2026, PremiereDate: '2026-03-04T00:00:00Z', OfficialRating: 'TV-PG', CommunityRating: 9.1, IndexNumber: 7, ParentIndexNumber: 1, Genres: ['Mobile drama'], Tags: ['Mobile tag'], Studios: ['Mobile Studio'], People: [{ Name: 'Mobile Actor', Role: 'Lead', Type: 'Actor', SortOrder: 0 }], ProviderIds: { Tmdb: '300' } });
    expect(episode.Overrides).not.toHaveProperty('ParentIndexNumber');
    expect(episode.LockedFields).not.toContain('ParentIndexNumber');
    expect(episode.Item.ParentId).toBe(originalParent);
    expect(digest(await ownedRead(manifest.Root, manifest.EpisodeNFOPath))).toBe(initialEpisodeNFO);
    await tab(page, 'Details');
    await input(page, 'Name').scrollIntoViewIfNeeded();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    const bounds = await editor(page).boundingBox();
    expect(bounds).not.toBeNull();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(391);
    await page.screenshot({ path: testInfo.outputPath('metadata-editor-mobile.png'), fullPage: true, animations: 'disabled' });
    await closeEditor(page);
    await page.getByRole('button', { name: 'Back to libraries', exact: true }).click();
    await replaceOwnedNFO(fixture, 'episode', '<?xml version="1.0" encoding="UTF-8"?><episodedetails><title>Automatic episode rescanned</title><season>1</season><episode>3</episode><plot>Episode overview rescanned</plot></episodedetails>\n');
    await scanLibrary(page, context.request, television);
    await manageLibrary(page, television);
    episode = await openItem(page, television, 'Curated mobile episode', manifest.EpisodePath);
    expect(episode.Automatic).toMatchObject({ Name: 'Automatic episode rescanned', IndexNumber: 3, ParentIndexNumber: 1 });
    expect(episode.Effective).toMatchObject({ Name: 'Curated mobile episode', IndexNumber: 7, ParentIndexNumber: 1, SortName: 'Independent mobile sort' });
    expect(episode.Item.ParentId).toBe(originalParent);
    await expect(input(page, 'ParentIndexNumber')).toHaveValue('1');
    await expect(input(page, 'ParentIndexNumber')).toBeDisabled();
    await expect(editor(page).getByRole('checkbox', { name: 'Lock Season number', exact: true })).toBeDisabled();
    await useAutomatic(page, [['Name', 'Title'], ['IndexNumber', 'Episode number']]);
    episode = await saveMetadata(page, episode);
    expect(episode.Effective).toMatchObject({ Name: 'Automatic episode rescanned', IndexNumber: 3, ParentIndexNumber: 1, SortName: 'Independent mobile sort' });
    expect(episode.Overrides).not.toHaveProperty('Name');
    expect(episode.Overrides).not.toHaveProperty('IndexNumber');
    expect(episode.Overrides).not.toHaveProperty('ParentIndexNumber');
    expect(episode.LockedFields).toEqual([]);
    await closeEditor(page);
  });

  await test.step('type changes retain inactive saved values until explicitly removed', async () => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByRole('button', { name: 'Back to libraries', exact: true }).click();
    await manageLibrary(page, mixed);
    let mutable = await openItem(page, mixed, 'Reclassifiable episode', manifest.MutableEpisodePath);
    expect(mutable.Item.Type).toBe('Episode');
    expect(mutable.Effective.IndexNumber).toBe(1);
    const originalID = mutable.Item.Id;
    const originalParent = mutable.Item.ParentId;
    await input(page, 'IndexNumber').fill('7');
    await editor(page).getByRole('checkbox', { name: 'Lock Episode number', exact: true }).check();
    mutable = await saveMetadata(page, mutable);
    expect(mutable.Overrides.IndexNumber).toBe(7);
    expect(mutable.LockedValues.IndexNumber).toBe(7);
    await closeEditor(page);
    await page.getByRole('button', { name: 'Back to libraries', exact: true }).click();
    try {
      await renameOwnedMedia(fixture, 'movie');
      await scanLibrary(page, context.request, mixed);
      const movieItems = await manageLibrary(page, mixed);
      const movie = movieItems.Items.find((item) => item.Path === manifest.MutableMoviePath);
      expect(movie).toBeDefined();
      expect(movie!.Id).toBe(originalID);
      expect(movie!.Type).toBe('Movie');
      expect(movie!.IndexNumber).toBeNull();
      mutable = await openItem(page, mixed, movie!.Name, manifest.MutableMoviePath, ['IndexNumber']);
      expect(mutable.Item.Id).toBe(originalID);
      expect(mutable.Item.Type).toBe('Movie');
      expect(mutable.EditableFields).not.toContain('IndexNumber');
      expect(mutable.Overrides.IndexNumber).toBe(7);
      expect(mutable.LockedValues.IndexNumber).toBe(7);
      expect(mutable.LockedFields).toEqual(['IndexNumber']);
      expect(mutable.Effective.IndexNumber).toBeNull();
      const inactiveSection = editor(page).getByRole('region', { name: 'Inactive saved fields', exact: true });
      const inactiveNumber = editor(page).locator('section[aria-labelledby="metadata-inactive-IndexNumber-heading"]');
      await expect(inactiveSection).toBeVisible();
      await expect(input(page, 'IndexNumber')).toHaveCount(0);
      await expect(inactiveNumber.getByText('Not applied to this item type', { exact: true })).toBeVisible();
      await expect(inactiveNumber.getByRole('textbox')).toHaveCount(0);
      await expect(inactiveNumber.getByRole('checkbox')).toHaveCount(0);
      await expect(inactiveNumber.locator('dt').filter({ hasText: /^Saved manual override$/ }).locator('xpath=following-sibling::dd')).toHaveText('7');
      await expect(inactiveNumber.locator('dt').filter({ hasText: /^Saved locked value$/ }).locator('xpath=following-sibling::dd')).toHaveText('7');
      await input(page, 'Overview').fill('Overview retained through item type changes');
      mutable = await saveMetadata(page, mutable, ['IndexNumber']);
      expect(mutable.Overrides.IndexNumber).toBe(7);
      expect(mutable.LockedValues.IndexNumber).toBe(7);
      expect(mutable.LockedFields).toEqual(['IndexNumber']);
      expect(mutable.Effective.IndexNumber).toBeNull();
      await inactiveNumber.scrollIntoViewIfNeeded();
      await page.screenshot({ path: testInfo.outputPath('metadata-inactive-desktop.png'), fullPage: true, animations: 'disabled' });
      const restore = inactiveNumber.getByRole('button', { name: 'Use automatic Item number', exact: true });
      await restore.click();
      await expect(inactiveNumber.getByText('This saved override and lock will be removed when you save.', { exact: true })).toBeVisible();
      await expect(restore).toBeDisabled();
      mutable = await saveMetadata(page, mutable);
      expect(mutable.Overrides).not.toHaveProperty('IndexNumber');
      expect(mutable.LockedValues).not.toHaveProperty('IndexNumber');
      expect(mutable.LockedFields).toEqual([]);
      expect(mutable.Effective.IndexNumber).toBeNull();
      await expect(inactiveSection).not.toBeVisible();
      await closeEditor(page);
      await page.getByRole('button', { name: 'Back to libraries', exact: true }).click();
      await renameOwnedMedia(fixture, 'episode');
      await scanLibrary(page, context.request, mixed);
      const restoredItems = await manageLibrary(page, mixed);
      const restored = restoredItems.Items.find((item) => item.Path === manifest.MutableEpisodePath);
      expect(restored).toBeDefined();
      expect(restored!.Id).toBe(originalID);
      expect(restored!.Type).toBe('Episode');
      expect(restored!.IndexNumber).toBe(1);
      mutable = await openItem(page, mixed, restored!.Name, manifest.MutableEpisodePath);
      expect(mutable.Item.Id).toBe(originalID);
      expect(mutable.Item.Type).toBe('Episode');
      expect(mutable.Item.ParentId).toBe(originalParent);
      expect(mutable.Automatic.IndexNumber).toBe(1);
      expect(mutable.Effective.IndexNumber).toBe(1);
      expect(mutable.Effective.Overview).toBe('Overview retained through item type changes');
      expect(mutable.Overrides).not.toHaveProperty('IndexNumber');
      expect(mutable.LockedValues).not.toHaveProperty('IndexNumber');
      expect(mutable.LockedFields).toEqual([]);
      await expect(input(page, 'IndexNumber')).toHaveValue('1');
      await expect(editor(page).getByRole('checkbox', { name: 'Lock Episode number', exact: true })).not.toBeChecked();
      await closeEditor(page);
    } finally {
      // Restore only the declared pair on failure; database cleanup belongs to
      // the orchestrator. Every rollback rename repeats all ownership checks.
      if (await mutableMediaPath(fixture) === manifest.MutableMoviePath) await renameOwnedMedia(fixture, 'episode');
    }
  });

  await verifyOwnedFixture(fixture, true);
  expect(pageErrors).toBe(0);
});
