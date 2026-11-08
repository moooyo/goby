import type { Dialog, Locator, Page, Request, Route } from '@playwright/test';
import type { Library, MetadataDetail, MetadataItemSummary } from '../src/api';
import type { ArtworkCollection, ArtworkImage, ImageType } from '../src/artworkApi';
import type { MissingImageResult } from '../src/providersApi';
import { administrator, csrf, DashboardAPI, expect, json, library, movieMetadata, test } from './dashboard-fixture';

// Exercise the real library page and its modal composition with synthetic API
// responses. No request in this file mutates a real library or provider.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off', preferences: { language: 'en', theme: 'light' } });

const directoryPath = '/admin/v1/storage/library-directories';
const downloadLabel = 'Download missing images on this page';
const unknownPrompt = 'An image download needs review. Leave without reloading saved images?';
const unknownNotice = 'An image download could not be confirmed. Reload images and review the saved results before starting another download.';
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64');
const imagesPath = (id: string) => `/admin/v1/items/${encodeURIComponent(id)}/images`;
const missingPath = (id: string) => `/admin/v1/items/${encodeURIComponent(id)}/providers/images/missing`;
const downloads = (api: DashboardAPI) => api.writes().filter((request) => request.path.endsWith('/providers/images/missing'));

interface MissingInput { LibraryId: string; ImageType: ImageType }
interface ItemSpecification { id: string; images?: ImageType[] }
interface IntegrationFixture {
  library: Library;
  items: MetadataItemSummary[];
  collections: Record<string, ArtworkCollection>;
  queries: URLSearchParams[];
  download: (route: Route, request: Request, id: string, input: MissingInput) => Promise<void>;
  result: (id: string, input: MissingInput) => MissingImageResult;
}

function savedImage(id: string, type: ImageType): ArtworkImage {
  const [width, height] = type === 'Primary' ? [2000, 3000] : type === 'Backdrop' ? [3840, 2160] : [800, 310];
  return { ImageType: type, ImageIndex: 0, Tag: `${id}-${type}`, Width: width, Height: height, Size: '4096', Source: 'provider',
    PreviewUrl: `${imagesPath(id)}/${type}/0/content?tag=${id}-${type}` };
}

function installIntegration(api: DashboardAPI, specifications: ItemSpecification[], selectedLibrary: Library = library): IntegrationFixture {
  const fixture: IntegrationFixture = {
    library: selectedLibrary, collections: {}, queries: [],
    items: specifications.map(({ id }) => ({ Id: id, LibraryId: selectedLibrary.Id, ParentId: '', ParentName: '', Name: `Film ${id}`,
      Type: 'Movie', Path: `${selectedLibrary.Paths[0]}/${id}.mkv`, IsFolder: false, ProductionYear: 2025,
      IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0 })),
    download: (route, _request, id, input) => json(route, fixture.result(id, input)),
    result: (id, input) => {
      const current = fixture.collections[id];
      fixture.collections[id] = { Revision: (BigInt(current.Revision) + 1n).toString(),
        Items: [...current.Items.filter((image) => image.ImageType !== input.ImageType), savedImage(id, input.ImageType)] };
      return { Status: 'downloaded', ItemId: id, LibraryId: input.LibraryId, ImageType: input.ImageType, Artwork: fixture.collections[id] };
    },
  };
  api.handlers.set('GET /admin/v1/libraries', (route) => json(route, { Items: [selectedLibrary], TotalRecordCount: 1 }));
  api.handlers.set(`GET /admin/v1/libraries/${selectedLibrary.Id}`, (route) => json(route, { Library: { ...selectedLibrary,
    Revision: '17', LibraryOptions: selectedLibrary.LibraryOptions ?? library.LibraryOptions,
    RegisteredPaths: selectedLibrary.Paths.map((path, index) => ({ Id: `root-${index}`, Path: path, ItemCount: fixture.items.length })) } }));
  api.handlers.set('GET ' + directoryPath, (route) => json(route, { Items: selectedLibrary.Paths.map((path) => ({ Path: path,
    Available: true, Readable: true, Writable: false, TotalBytes: '107374182400', UsedBytes: '42949672960', AvailableBytes: '59055800320' })),
    TotalRecordCount: selectedLibrary.Paths.length }));
  api.handlers.set(`GET /admin/v1/libraries/${selectedLibrary.Id}/items`, (route, request) => {
    const query = new URL(request.url()).searchParams;
    fixture.queries.push(query);
    const start = Number(query.get('StartIndex') ?? 0);
    const limit = Number(query.get('Limit') ?? 50);
    return json(route, { Library: selectedLibrary, Items: fixture.items.slice(start, start + limit),
      TotalRecordCount: fixture.items.length, StartIndex: start, Limit: limit });
  });
  for (const [index, specification] of specifications.entries()) {
    const id = specification.id;
    const metadata: MetadataDetail = structuredClone(movieMetadata);
    metadata.Item = { ...metadata.Item, ...fixture.items[index] };
    metadata.Automatic.Name = fixture.items[index].Name; metadata.Effective.Name = fixture.items[index].Name;
    fixture.collections[id] = { Revision: '12', Items: (specification.images ?? []).map((type) => savedImage(id, type)) };
    api.handlers.set(`GET /admin/v1/items/${id}/metadata`, (route) => json(route, metadata));
    api.handlers.set(`GET ${imagesPath(id)}`, (route) => json(route, fixture.collections[id]));
    api.handlers.set(`POST /admin/v1/items/${id}/providers/images`, (route) => json(route, { Items: [] }));
    api.handlers.set(`POST ${missingPath(id)}`, (route, request) => fixture.download(route, request, id, request.postDataJSON() as MissingInput));
    for (const type of ['Primary', 'Backdrop', 'Logo'] as const) {
      api.handlers.set(`GET ${imagesPath(id)}/${type}/0/content`, (route) => route.fulfill({ contentType: 'image/png', body: png }));
    }
  }
  return fixture;
}

async function openImages(page: Page, fixture: IntegrationFixture, secondPage = false): Promise<Locator> {
  await page.goto('/admin/media/libraries');
  await page.getByRole('button', { name: `Manage items in ${fixture.library.Name}`, exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/admin/media/libraries/${fixture.library.Id}/items$`));
  if (secondPage) {
    await page.getByRole('combobox', { name: 'Items per page:', exact: true }).selectOption('25');
    await expect.poll(() => fixture.queries.at(-1)?.get('Limit')).toBe('25');
    await page.getByRole('button', { name: 'Go to next page', exact: true }).click();
    await expect.poll(() => fixture.queries.at(-1)?.get('StartIndex')).toBe('25');
  }
  await page.getByRole('tab', { name: 'Images', exact: true }).click();
  const region = page.getByRole('region', { name: 'Library images', exact: true });
  await expect(region.locator('.library-artwork-grid')).toHaveAttribute('aria-busy', 'false');
  return region;
}

async function confirmAction(page: Page, action: () => Promise<unknown>, message: string, accept = false) {
  const confirmation = page.waitForEvent('dialog').then(async (dialog) => {
    const observed = { type: dialog.type(), message: dialog.message() };
    if (accept) await dialog.accept(); else await dialog.dismiss();
    return observed;
  });
  await action();
  expect(await confirmation).toEqual({ type: 'confirm', message });
}

async function signOut(page: Page) {
  await page.getByRole('button', { name: `Account: ${administrator.Name}`, exact: true }).click();
  await page.getByRole('menuitem', { name: 'Sign out', exact: true }).click();
}

test('the page passes the current library and image type, and grid and menu entries open the unified metadata sheet', async ({ page, api }) => {
  const selectedLibrary = { ...library, Id: 'e'.repeat(32), Name: 'International films' };
  const fixture = installIntegration(api, [
    { id: 'existing-logo', images: ['Logo'] }, { id: 'missing-logo', images: ['Primary', 'Backdrop'] },
    ...Array.from({ length: 48 }, (_, index) => ({ id: `present-${index}`, images: ['Primary', 'Backdrop', 'Logo'] as ImageType[] })),
    { id: 'off-page' },
  ], selectedLibrary);
  const region = await openImages(page, fixture);
  await region.getByRole('group', { name: 'Image type', exact: true }).getByRole('button', { name: 'Logos', exact: true }).click();
  await region.getByRole('button', { name: downloadLabel, exact: true }).click();
  await expect(region.locator('[data-item-id="missing-logo"] [data-download-status]')).toHaveAttribute('data-download-status', 'downloaded');
  await expect(region.locator('.library-artwork-grid')).toHaveAttribute('aria-busy', 'false');
  expect(downloads(api)).toEqual([{ method: 'POST', path: missingPath('missing-logo'), body: { LibraryId: selectedLibrary.Id, ImageType: 'Logo' }, csrf }]);
  expect(api.reads(imagesPath('off-page'))).toEqual([]);
  expect(fixture.collections['missing-logo'].Items.map((image) => image.ImageType).sort()).toEqual(['Backdrop', 'Logo', 'Primary']);

  await region.getByRole('button', { name: 'Manage images: Film missing-logo', exact: true }).click();
  const sheet = page.getByRole('dialog', { name: /^Edit metadata/ });
  await expect(sheet.getByRole('tab')).toHaveCount(5);
  await expect(sheet.getByRole('tab', { name: 'Images', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(sheet.getByRole('group', { name: 'Image type', exact: true }).getByRole('button', { name: 'Logo', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByRole('dialog', { name: /^Manage (images|artwork)/ })).toHaveCount(0);
  await sheet.getByRole('tab', { name: 'Basic information', exact: true }).click();
  await sheet.locator('input[name="Name"]').fill('Unsaved metadata draft');
  await confirmAction(page, () => page.goBack({ waitUntil: 'commit' }), 'Discard unsaved metadata changes and leave this page?');
  await expect(sheet.locator('input[name="Name"]')).toHaveValue('Unsaved metadata draft');
  await sheet.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('alertdialog', { name: 'Discard unsaved metadata changes?', exact: true }).getByRole('button', { name: 'Discard changes', exact: true }).click();
  await page.getByRole('tab', { name: 'Items', exact: true }).click();
  await page.getByRole('button', { name: 'Edit metadata for Film missing-logo', exact: true }).first().click();
  await expect(sheet.getByRole('tab', { name: 'Basic information', exact: true })).toHaveAttribute('aria-selected', 'true');
  await sheet.getByRole('button', { name: 'Close', exact: true }).click();
  const menu = page.locator('.media-item-menu').filter({ has: page.locator('summary[aria-label="More actions: Film missing-logo"]') });
  await menu.locator('summary').click();
  await menu.getByRole('button', { name: 'Manage images', exact: true }).click();
  await expect(sheet.getByRole('tab', { name: 'Images', exact: true })).toHaveAttribute('aria-selected', 'true');
  expect(downloads(api)).toHaveLength(1);
});

test('pending image work blocks internal scope changes, browser back, settings, scans and sign out without replaying them', async ({ page, api }) => {
  const fixture = installIntegration(api, Array.from({ length: 52 }, (_, index) => ({ id: `pending-${index}`, images: index === 25 ? [] : ['Primary'] as ImageType[] })));
  const ordinaryDownload = fixture.download;
  let release = () => {};
  const pending = new Promise<void>((resolve) => { release = resolve; });
  fixture.download = async (route, request, id, input) => { await pending; await ordinaryDownload(route, request, id, input); };
  const region = await openImages(page, fixture, true);
  const queries = fixture.queries.length;
  const prompts: string[] = [];
  const dismiss = async (dialog: Dialog) => { prompts.push(dialog.message()); await dialog.dismiss(); };
  page.on('dialog', dismiss);
  try {
    await region.getByRole('button', { name: downloadLabel, exact: true }).click();
    await expect.poll(() => downloads(api).length).toBe(1);
    await expect(region.locator('[data-item-id="pending-25"] [data-download-status]')).toHaveAttribute('data-download-status', 'downloading');
    await page.getByRole('tab', { name: 'Media directories', exact: true }).click();
    await page.getByRole('tab', { name: 'Images', exact: true }).press('ArrowRight');
    await page.getByRole('textbox', { name: 'Search title', exact: true }).fill('Blocked query');
    await page.getByRole('textbox', { name: 'Search title', exact: true }).press('Enter');
    await page.locator('.media-types-filter summary').click();
    await page.getByRole('checkbox', { name: 'Movie', exact: true }).click();
    await page.getByRole('button', { name: 'Clear filters', exact: true }).click();
    await page.getByRole('button', { name: 'Go to previous page', exact: true }).click();
    await page.getByRole('combobox', { name: 'Items per page:', exact: true }).selectOption('50');
    await page.getByRole('button', { name: 'Refresh library items', exact: true }).click();
    await page.getByRole('button', { name: 'Library settings', exact: true }).click();
    await page.getByRole('button', { name: 'Scan library', exact: true }).click();
    await page.getByRole('button', { name: 'Back to libraries', exact: true }).click();
    await page.goBack({ waitUntil: 'commit' });
    await signOut(page);
    await expect(page.getByRole('tab', { name: 'Images', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(region).toBeVisible();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(page.getByRole('combobox', { name: 'Items per page:', exact: true })).toHaveValue('25');
    expect(fixture.queries).toHaveLength(queries);
    expect(api.writes()).toHaveLength(1);
    expect(prompts).toEqual([]);
  } finally { release(); page.off('dialog', dismiss); }
  await expect(region.locator('[data-item-id="pending-25"] [data-download-status]')).toHaveAttribute('data-download-status', 'downloaded');
  await expect(region.locator('.library-artwork-grid')).toHaveAttribute('aria-busy', 'false');
  await expect(page.getByRole('tab', { name: 'Images', exact: true })).toHaveAttribute('aria-selected', 'true');
  expect(fixture.queries).toHaveLength(queries);
  expect(api.writes()).toHaveLength(1);
});

test('unknown image results survive parent refreshes and independent modal guards until an explicit scope change', async ({ page, api }) => {
  const fixture = installIntegration(api, Array.from({ length: 52 }, (_, index) => ({ id: `review-${index}`, images: index === 25 || index === 26 ? [] : ['Primary'] as ImageType[] })));
  const ordinaryDownload = fixture.download;
  fixture.download = (route, request, id, input) => {
    if (id !== 'review-26') return ordinaryDownload(route, request, id, input);
    fixture.result(id, input);
    return json(route, { Error: { Code: 'operation_unconfirmed', Message: 'Reload and review the possibly saved image.' } }, 503);
  };
  const region = await openImages(page, fixture, true);
  const queries = fixture.queries.length;
  await region.getByRole('button', { name: downloadLabel, exact: true }).click();
  await expect(region.getByText(unknownNotice, { exact: true })).toBeVisible();
  await expect(region.locator('.library-artwork-grid')).toHaveAttribute('aria-busy', 'false');
  expect(downloads(api)).toHaveLength(2);
  expect(fixture.queries).toHaveLength(queries);

  await confirmAction(page, () => page.getByRole('tab', { name: 'Media directories', exact: true }).click(), unknownPrompt);
  await confirmAction(page, () => page.getByRole('tab', { name: 'Images', exact: true }).press('ArrowRight'), unknownPrompt);
  await page.getByRole('textbox', { name: 'Search title', exact: true }).fill('Uncommitted review query');
  await confirmAction(page, () => page.getByRole('textbox', { name: 'Search title', exact: true }).press('Enter'), unknownPrompt);
  await page.locator('.media-types-filter summary').click();
  await confirmAction(page, () => page.getByRole('checkbox', { name: 'Movie', exact: true }).click(), unknownPrompt);
  await confirmAction(page, () => page.getByRole('button', { name: 'Clear filters', exact: true }).click(), unknownPrompt);
  await confirmAction(page, () => page.getByRole('button', { name: 'Go to previous page', exact: true }).click(), unknownPrompt);
  await confirmAction(page, () => page.getByRole('combobox', { name: 'Items per page:', exact: true }).selectOption('50'), unknownPrompt);
  await confirmAction(page, () => page.getByRole('button', { name: 'Scan library', exact: true }).click(), unknownPrompt);
  await confirmAction(page, () => page.goBack({ waitUntil: 'commit' }), unknownPrompt);
  await confirmAction(page, () => signOut(page), unknownPrompt);
  expect(fixture.queries).toHaveLength(queries);
  expect(api.writes()).toHaveLength(2);
  await confirmAction(page, () => page.getByRole('button', { name: 'Refresh library items', exact: true }).click(), unknownPrompt, true);
  await expect.poll(() => fixture.queries.length).toBe(queries + 1);
  await expect(region.getByText(unknownNotice, { exact: true })).toBeVisible();
  await expect(region.getByRole('button', { name: downloadLabel, exact: true })).toBeDisabled();

  const savedItems = fixture.items;
  fixture.items = [];
  await confirmAction(page, () => page.getByRole('button', { name: 'Refresh library items', exact: true }).click(), unknownPrompt, true);
  await expect(region.locator('[data-item-id]')).toHaveCount(0);
  await expect(region.getByText(unknownNotice, { exact: true })).toBeVisible();
  expect(fixture.queries.at(-1)?.get('StartIndex')).toBe('25');
  expect(fixture.queries).toHaveLength(queries + 2);
  fixture.items = savedItems;
  await confirmAction(page, () => page.getByRole('button', { name: 'Refresh library items', exact: true }).click(), unknownPrompt, true);
  await expect(region.locator('[data-item-id="review-26"]')).toBeVisible();
  await expect(region.getByText(unknownNotice, { exact: true })).toBeVisible();

  await confirmAction(page, () => page.getByRole('button', { name: 'Library settings', exact: true }).click(), unknownPrompt, true);
  const settings = page.getByRole('dialog', { name: 'Edit library', exact: true });
  await expect(settings.getByRole('textbox', { name: 'Library name', exact: true })).toBeEnabled();
  // An idle modal publishes undefined, but the image review guard must remain.
  await confirmAction(page, () => page.goBack({ waitUntil: 'commit' }), unknownPrompt);
  await expect(settings).toBeVisible();
  await settings.getByRole('textbox', { name: 'Library name', exact: true }).fill('Unsaved library draft');
  const prompts: string[] = [];
  const keepDraft = async (dialog: Dialog) => {
    prompts.push(dialog.message());
    if (dialog.message() === unknownPrompt) await dialog.accept(); else await dialog.dismiss();
  };
  page.on('dialog', keepDraft);
  try {
    await page.goBack({ waitUntil: 'commit' });
    await expect.poll(() => prompts.length).toBe(2);
  } finally { page.off('dialog', keepDraft); }
  expect(prompts).toEqual([unknownPrompt, 'Discard unsaved library changes and leave this page?']);
  await expect(settings.getByRole('textbox', { name: 'Library name', exact: true })).toHaveValue('Unsaved library draft');
  await confirmAction(page, () => settings.getByRole('button', { name: 'Cancel', exact: true }).click(), 'Discard unsaved library changes?', true);
  await expect(settings).toBeHidden();
  // Modal cleanup also publishes undefined and must not clear the image guard.
  await confirmAction(page, () => page.getByRole('button', { name: 'Back to libraries', exact: true }).click(), unknownPrompt);
  await expect(region.getByText(unknownNotice, { exact: true })).toBeVisible();
  await confirmAction(page, () => page.getByRole('button', { name: 'Go to previous page', exact: true }).click(), unknownPrompt, true);
  await expect.poll(() => fixture.queries.at(-1)?.get('StartIndex')).toBe('0');
  await expect(region.getByText(unknownNotice, { exact: true })).toHaveCount(0);
  expect(downloads(api)).toHaveLength(2);
  expect(api.writes()).toHaveLength(2);
});

for (const destination of ['back', 'sign-out'] as const) {
  test(`unknown image work permits one explicitly confirmed ${destination} action without replay`, async ({ page, api }) => {
    const fixture = installIntegration(api, [{ id: 'unknown-exit' }]);
    fixture.download = (route) => json(route, { Error: { Code: 'operation_unconfirmed', Message: 'Review the saved image before trying again.' } }, 503);
    const region = await openImages(page, fixture);
    await region.getByRole('button', { name: downloadLabel, exact: true }).click();
    await expect(region.getByText(unknownNotice, { exact: true })).toBeVisible();
    await confirmAction(page, () => destination === 'back' ? page.goBack({ waitUntil: 'commit' }) : signOut(page), unknownPrompt, true);
    if (destination === 'back') await expect(page).toHaveURL(/\/admin\/media\/libraries$/);
    else await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
    expect(downloads(api)).toHaveLength(1);
    expect(api.writes().filter((request) => request.path === '/admin/v1/session')).toHaveLength(destination === 'sign-out' ? 1 : 0);
  });
}

test('the directory tab uses actual directory observations and keeps the separate registered-root binding workflow', async ({ page, api }) => {
  const fixture = installIntegration(api, [{ id: 'directory-film', images: ['Primary'] }]);
  const rootsPath = `/admin/v1/libraries/${library.Id}/roots`;
  const root = { Id: 'registered-movies', LibraryId: library.Id, Path: '/media/movies', AllowedPath: '/media', RelativePath: 'movies', Revision: '7' };
  api.handlers.set('GET ' + rootsPath, (route) => json(route, { Items: [root], TotalRecordCount: 1 }));
  api.handlers.set(`GET ${rootsPath}/${root.Id}/binding`, (route) => json(route, { Binding: { ...root, Status: 'unavailable' } }));
  await openImages(page, fixture);
  await page.getByRole('tab', { name: 'Media directories', exact: true }).click();
  const panel = page.getByRole('region', { name: 'Media directories', exact: true });
  const directory = panel.getByRole('listitem', { name: '/media/movies', exact: true });
  await expect(directory).toContainText('40 / 100 GiB');
  await expect(directory).toContainText('Read only');
  await expect(directory.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '40');
  await expect(panel).toContainText('these values must not be added together');
  await expect(panel.getByRole('listitem', { name: '/media', exact: true })).toHaveCount(0);
  await expect(page.getByText('Storage bindings protect catalog records when media is unavailable. Review the current storage before approving a replacement.', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Storage bindings', exact: true }).click();
  const binding = page.getByRole('dialog', { name: 'Storage bindings', exact: true });
  await expect(binding.getByRole('combobox', { name: 'Registered root', exact: true })).toHaveValue(root.Id);
  const terms = binding.getByRole('term');
  const definitions = binding.getByRole('definition');
  await expect(terms).toHaveText(['Registered root path', 'Storage anchor path', 'Root ID', 'Binding revision']);
  await expect(definitions).toHaveText([root.Path, root.AllowedPath, root.Id, root.Revision]);
  for (let index = 0; index < 4; index += 1) {
    await expect(terms.nth(index)).toBeVisible();
    await expect(definitions.nth(index)).toBeVisible();
  }
  await expect(binding.getByRole('status').getByText('Unavailable', { exact: true })).toBeVisible();
  await expect(binding.getByRole('button', { name: 'Bind storage', exact: true })).toBeDisabled();
  expect(api.reads(rootsPath)).toHaveLength(1);
  expect(api.reads(`${rootsPath}/${root.Id}/binding`)).toHaveLength(1);
  expect(api.reads(directoryPath).length).toBeGreaterThan(0);
  expect(api.writes()).toEqual([]);
});
