import type { Locator, Page, Request, Route } from '@playwright/test';
import type { MetadataItemSummary } from '../src/api';
import type { ArtworkCollection, ArtworkImage, ImageType } from '../src/artworkApi';
import type { MissingImageResult, MissingImageStatus } from '../src/providersApi';
import { csrf, DashboardAPI, expect, json, library, test } from './dashboard-fixture';
import { evidence } from './dashboard-review';

// Every response is synthetic. The UI still performs its real page-scoped
// download, sequential queue, request validation, and uncertain-result flow.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off', preferences: { language: 'en', theme: 'light' } });

const itemsPath = `/admin/v1/libraries/${library.Id}/items`;
const artworkRevision = `1${'0'.repeat(77)}`;
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64');
const downloadLabel = 'Download missing images on this page';
const reviewMessage = 'An image download could not be confirmed. Reload images and review the saved results before starting another download.';
const reviewPrompt = 'An image download needs review. Leave without reloading saved images?';
const imagePath = (id: string) => `/admin/v1/items/${encodeURIComponent(id)}/images`;
const missingPath = (id: string) => `/admin/v1/items/${encodeURIComponent(id)}/providers/images/missing`;

interface MissingInput { LibraryId: string; ImageType: ImageType }
interface FixtureItem { id: string; name: string; type?: string; images?: ImageType[]; status?: MissingImageStatus }
interface LibraryImageFixture {
  items: MetadataItemSummary[];
  collections: Record<string, ArtworkCollection>;
  readFailures: Set<string>;
  statuses: Record<string, MissingImageStatus>;
  missing: (route: Route, request: Request, id: string, input: MissingInput) => Promise<void>;
  result: (id: string, input: MissingInput, status?: MissingImageStatus) => MissingImageResult;
}

function item(specification: FixtureItem): MetadataItemSummary {
  return { Id: specification.id, LibraryId: library.Id, ParentId: '', ParentName: '', Name: specification.name,
    Type: specification.type ?? 'Movie', Path: `/media/movies/${specification.id}.mkv`, IsFolder: specification.type === 'Folder', ProductionYear: 2025,
    IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0 };
}

function savedImage(id: string, type: ImageType, tag: string, source = 'provider'): ArtworkImage {
  const [width, height] = type === 'Primary' ? [2000, 3000] : type === 'Backdrop' ? [3840, 2160] : [800, 310];
  return { ImageType: type, ImageIndex: 0, Tag: tag, Width: width, Height: height, Size: '4096', Source: source,
    PreviewUrl: `${imagePath(id)}/${type}/0/content?tag=${encodeURIComponent(tag)}` };
}

function installLibraryImages(api: DashboardAPI, specifications: FixtureItem[]): LibraryImageFixture {
  const fixture: LibraryImageFixture = {
    items: specifications.map(item), readFailures: new Set(), statuses: {}, collections: {},
    missing: (route, _request, id, input) => json(route, fixture.result(id, input)),
    result: (id, input, status = fixture.statuses[id] ?? 'downloaded') => {
      if (status === 'downloaded' || status === 'already_present') {
        const current = fixture.collections[id];
        fixture.collections[id] = { Revision: (BigInt(current.Revision) + 1n).toString(), Items: [
          ...current.Items.filter((image) => image.ImageType !== input.ImageType),
          savedImage(id, input.ImageType, `${id}-${input.ImageType}-${status}`),
        ] };
      }
      return { Status: status, ItemId: id, LibraryId: input.LibraryId, ImageType: input.ImageType, Artwork: fixture.collections[id] };
    },
  };
  api.handlers.set(`GET ${itemsPath}`, (route, request) => {
    const url = new URL(request.url());
    const start = Number(url.searchParams.get('StartIndex') ?? 0);
    const limit = Number(url.searchParams.get('Limit') ?? 50);
    return json(route, { Library: library, Items: fixture.items.slice(start, start + limit),
      TotalRecordCount: fixture.items.length, StartIndex: start, Limit: limit });
  });
  for (const specification of specifications) {
    const id = specification.id;
    fixture.collections[id] = { Revision: artworkRevision, Items: (specification.images ?? []).map((type) => savedImage(id, type, `${id}-${type}-existing`)) };
    fixture.statuses[id] = specification.status ?? 'downloaded';
    api.handlers.set(`GET ${imagePath(id)}`, (route) => fixture.readFailures.has(id)
      ? json(route, { Error: { Code: 'unavailable', Message: 'The current image collection could not be read.' } }, 503)
      : json(route, fixture.collections[id]));
    api.handlers.set(`POST ${missingPath(id)}`, (route, request) => {
      const input = request.postDataJSON() as MissingInput;
      expect(input.LibraryId).toBe(library.Id);
      expect(['Primary', 'Backdrop', 'Logo']).toContain(input.ImageType);
      expect(Object.keys(input).sort()).toEqual(['ImageType', 'LibraryId']);
      return fixture.missing(route, request, id, input);
    });
    for (const type of ['Primary', 'Backdrop', 'Logo'] as const) {
      api.handlers.set(`GET ${imagePath(id)}/${type}/0/content`, (route) => route.fulfill({ contentType: 'image/png', body: png }));
    }
  }
  return fixture;
}

async function openLibraryImages(page: Page, api: DashboardAPI, fixture: LibraryImageFixture): Promise<Locator> {
  await page.goto(`/admin/media/libraries/${library.Id}/items`);
  await page.getByRole('tab', { name: 'Images', exact: true }).click();
  const region = page.getByRole('region', { name: 'Library images', exact: true });
  await expect(region).toBeVisible();
  await expect.poll(() => fixture.items.slice(0, 50).every((entry) => api.reads(imagePath(entry.Id)).length > 0)).toBe(true);
  await expect(region.locator('.library-artwork-grid')).toHaveAttribute('aria-busy', 'false');
  return region;
}

const entry = (region: Locator, id: string) => region.locator(`[data-item-id="${id}"]`);
const status = (region: Locator, id: string) => entry(region, id).locator('[data-download-status]');
const showFilter = (region: Locator) => region.locator('.media-filter-label select');
const downloads = (api: DashboardAPI) => api.writes().filter((request) => request.path.endsWith('/providers/images/missing'));
const requestedIds = (api: DashboardAPI) => downloads(api).map((request) => decodeURIComponent(request.path.split('/')[4]));

async function expectCompleteResult(label: Locator, message: string): Promise<void> {
  await label.scrollIntoViewIfNeeded();
  await expect(label).toBeVisible();
  expect(await label.innerText(), 'The complete result must be visible without opening a tooltip.').toBe(message);
  const layout = await label.evaluate((element) => {
    const entry = element.closest('.library-artwork-entry');
    if (!entry) throw new Error('An image result must belong to its artwork entry.');
    const box = (rect: DOMRect) => ({ left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom });
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
    const segments: { node: Node; start: number; end: number; text: string }[] = [];
    let offset = 0;
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const text = node.textContent ?? '';
      segments.push({ node, start: offset, end: offset + text.length, text });
      offset += text.length;
    }
    const text = segments.map((segment) => segment.text).join('');
    const words = [...text.matchAll(/\S+/g)].map((match) => {
      const start = match.index; const end = start + match[0].length;
      const first = segments.find((segment) => segment.start <= start && segment.end > start);
      const last = segments.find((segment) => segment.start < end && segment.end >= end);
      if (!first || !last) throw new Error('An image result word could not be measured.');
      const range = document.createRange();
      range.setStart(first.node, start - first.start);
      range.setEnd(last.node, end - last.start);
      const rects = [...range.getClientRects()].filter((rect) => rect.width > 0 && rect.height > 0).map(box);
      return { text: match[0], rects };
    });
    return { result: box(element.getBoundingClientRect()), entry: box(entry.getBoundingClientRect()), words,
      scrollWidth: element.scrollWidth, clientWidth: element.clientWidth, scrollHeight: element.scrollHeight, clientHeight: element.clientHeight };
  });
  expect(layout.words.map((word) => word.text)).toEqual(message.split(/\s+/));
  expect(layout.scrollWidth, 'The result must not clip text horizontally.').toBeLessThanOrEqual(layout.clientWidth + 1);
  expect(layout.scrollHeight, 'The result must not clip text vertically.').toBeLessThanOrEqual(layout.clientHeight + 1);
  for (const word of layout.words) {
    expect(word.rects, `The result word "${word.text}" must wrap as a whole word.`).toHaveLength(1);
    for (const rect of word.rects) for (const bounds of [layout.result, layout.entry]) {
      expect(rect.left, `The complete word "${word.text}" must remain inside its result and entry.`).toBeGreaterThanOrEqual(bounds.left - 1);
      expect(rect.right, `The complete word "${word.text}" must remain inside its result and entry.`).toBeLessThanOrEqual(bounds.right + 1);
      expect(rect.top, `The complete word "${word.text}" must remain inside its result and entry.`).toBeGreaterThanOrEqual(bounds.top - 1);
      expect(rect.bottom, `The complete word "${word.text}" must remain inside its result and entry.`).toBeLessThanOrEqual(bounds.bottom + 1);
    }
  }
}

async function refreshItemsThroughReviewGuard(page: Page, accept: boolean): Promise<void> {
  const prompt = page.waitForEvent('dialog');
  const refresh = page.getByRole('button', { name: 'Refresh library items', exact: true }).click();
  const dialog = await prompt;
  const type = dialog.type();
  const message = dialog.message();
  if (accept) await dialog.accept();
  else await dialog.dismiss();
  await refresh;
  expect(type).toBe('confirm');
  expect(message).toBe(reviewPrompt);
}

test('missing-image downloads stay on this page and type, serialize work, and report each real result', async ({ page, api }) => {
  const specifications: FixtureItem[] = [
    { id: 'existing', name: 'Existing poster', images: ['Primary'] },
    { id: 'downloadable', name: 'Downloadable poster', images: ['Backdrop'] },
    { id: 'no-candidate', name: 'No online candidate', status: 'no_candidate' },
    { id: 'manual-mask', name: 'Manual image choice', status: 'managed' },
    { id: 'unmatched', name: 'Unmatched item', status: 'unmatched' },
    { id: 'unsupported', name: 'Unsupported item', type: 'Folder', status: 'unsupported' },
    { id: 'concurrent', name: 'Concurrent image', status: 'already_present' },
    ...Array.from({ length: 43 }, (_, index) => ({ id: `page-filler-${index}`, name: `Page filler ${index}`, images: ['Primary', 'Backdrop', 'Logo'] as ImageType[] })),
    { id: 'off-page', name: 'Missing image on another page' },
  ];
  const fixture = installLibraryImages(api, specifications);
  const ordinaryMissing = fixture.missing;
  let release = () => {};
  const pending = new Promise<void>((resolve) => { release = resolve; });
  fixture.missing = async (route, request, id, input) => {
    if (id === 'downloadable') await pending;
    await ordinaryMissing(route, request, id, input);
  };
  const region = await openLibraryImages(page, api, fixture);
  expect(api.reads(imagePath('off-page'))).toEqual([]);
  await expect(region.getByText('This page and the selected image type only. Existing images and manual choices are kept.', { exact: true })).toBeVisible();
  try {
    await region.getByRole('button', { name: downloadLabel, exact: true }).click();
    await expect.poll(() => downloads(api).length).toBe(1);
    await expect(status(region, 'downloadable')).toHaveAttribute('data-download-status', 'downloading');
    for (const button of await region.getByRole('group', { name: 'Image type', exact: true }).getByRole('button').all()) await expect(button).toBeDisabled();
    await expect(showFilter(region)).toBeDisabled();
    await expect(region.getByRole('button', { name: downloadLabel, exact: true })).toBeDisabled();
    await expect(region.getByRole('button', { name: 'Reload images', exact: true })).toBeDisabled();
    await expect(entry(region, 'existing').getByRole('button')).toBeDisabled();
    expect(downloads(api)).toHaveLength(1);
  } finally { release(); }
  await expect(status(region, 'concurrent')).toHaveAttribute('data-download-status', 'already_present');
  await expect(region.locator('.library-artwork-grid')).toHaveAttribute('aria-busy', 'false');
  expect(requestedIds(api)).toEqual(['downloadable', 'no-candidate', 'manual-mask', 'unmatched', 'unsupported', 'concurrent']);
  expect(downloads(api).every((request) => request.method === 'POST' && request.csrf === csrf && (request.body as MissingInput).ImageType === 'Primary')).toBe(true);
  expect(api.writes()).toHaveLength(6);
  for (const [id, expected, message] of [
    ['downloadable', 'downloaded', 'Image downloaded'], ['no-candidate', 'no_candidate', 'No online image found'],
    ['manual-mask', 'managed', 'Manual image choice retained'], ['unmatched', 'unmatched', 'Identify this item before downloading images'],
    ['unsupported', 'unsupported', 'Online images are unavailable for this item type'], ['concurrent', 'already_present', 'Image already present'],
  ]) {
    await expect(status(region, id)).toHaveAttribute('data-download-status', expected);
    await expect(status(region, id)).toHaveText(message);
    await expectCompleteResult(status(region, id), message);
  }
  await expect(entry(region, 'existing').getByRole('img')).toHaveAttribute('src', /tag=existing-Primary-existing$/);
  await expect(entry(region, 'downloadable').getByRole('img')).toHaveAttribute('src', /tag=downloadable-Primary-downloaded$/);
  expect(fixture.collections['downloadable'].Items.some((image) => image.ImageType === 'Backdrop' && image.Tag === 'downloadable-Backdrop-existing')).toBe(true);
  expect(fixture.collections['manual-mask'].Items).toEqual([]);
  expect(api.reads(imagePath('off-page'))).toEqual([]);
  await region.locator('.media-artwork-toolbar').scrollIntoViewIfNeeded();
  await evidence(page, test.info(), 'design2-library-images-six-results');
});

test('selecting logos downloads only missing logos and retains the other image types', async ({ page, api }) => {
  const fixture = installLibraryImages(api, [
    { id: 'has-logo', name: 'Existing logo', images: ['Logo'] },
    { id: 'missing-logo', name: 'Missing logo', images: ['Primary', 'Backdrop'] },
  ]);
  const ordinaryMissing = fixture.missing;
  let release = () => {};
  const pending = new Promise<void>((resolve) => { release = resolve; });
  fixture.missing = async (route, request, id, input) => {
    await pending;
    await ordinaryMissing(route, request, id, input);
  };
  const region = await openLibraryImages(page, api, fixture);
  await region.getByRole('group', { name: 'Image type', exact: true }).getByRole('button', { name: 'Logos', exact: true }).click();
  try {
    await region.getByRole('button', { name: downloadLabel, exact: true }).click();
    await expect(status(region, 'missing-logo')).toHaveAttribute('data-download-status', 'downloading');
    await evidence(page, test.info(), 'design2-library-images-logo-progress');
  } finally { release(); }
  await expect(status(region, 'missing-logo')).toHaveAttribute('data-download-status', 'downloaded');
  expect(requestedIds(api)).toEqual(['missing-logo']);
  expect(downloads(api)[0]).toMatchObject({ body: { LibraryId: library.Id, ImageType: 'Logo' }, csrf });
  expect(fixture.collections['missing-logo'].Items.map((image) => image.ImageType).sort()).toEqual(['Backdrop', 'Logo', 'Primary']);
  await expect(entry(region, 'has-logo').getByRole('img')).toHaveAttribute('src', /tag=has-logo-Logo-existing$/);
  await expect(entry(region, 'missing-logo').getByRole('img')).toHaveAttribute('src', /tag=missing-logo-Logo-downloaded$/);
});

for (const failure of [
  { name: 'an unavailable response after saving', status: 503, code: 'operation_unconfirmed', tag: 'possibly-saved' },
  { name: 'a concurrent image revision', status: 409, code: 'revision_conflict', tag: 'concurrent-winner' },
  { name: 'a dropped network response', status: 0, code: 'network_error', tag: 'possibly-saved' },
]) {
  test(`a batch stops and cannot replay after ${failure.name}, including ordinary refreshes`, async ({ page, api }) => {
    const fixture = installLibraryImages(api, [
      { id: 'uncertain', name: 'Uncertain image' }, { id: 'remaining-one', name: 'Remaining image one' }, { id: 'remaining-two', name: 'Remaining image two' },
    ]);
    const ordinaryMissing = fixture.missing;
    let first = true;
    fixture.missing = async (route, request, id, input) => {
      if (id !== 'uncertain' || !first) return ordinaryMissing(route, request, id, input);
      first = false;
      fixture.collections[id] = { Revision: (BigInt(artworkRevision) + 1n).toString(), Items: [savedImage(id, 'Primary', failure.tag)] };
      if (failure.status === 0) return route.abort('failed');
      return json(route, { Error: { Code: failure.code, Message: 'The saved image result must be reviewed.' } }, failure.status);
    };
    const region = await openLibraryImages(page, api, fixture);
    await region.getByRole('button', { name: downloadLabel, exact: true }).click();
    await expect(status(region, 'uncertain')).toHaveAttribute('data-download-status', 'unknown');
    await expect(status(region, 'remaining-one')).toHaveAttribute('data-download-status', 'not_attempted');
    await expect(status(region, 'remaining-two')).toHaveAttribute('data-download-status', 'not_attempted');
    await expect(region.getByText(reviewMessage, { exact: true })).toBeVisible();
    await expect(region.getByRole('button', { name: downloadLabel, exact: true })).toBeDisabled();
    await expect(region.getByRole('button', { name: /^Retry image download/ })).toHaveCount(0);
    expect(requestedIds(api)).toEqual(['uncertain']);

    // Both the automatic read after the batch and the library's ordinary
    // refresh may show the saved image, but neither resolves an unknown write.
    // Ordinary refresh keeps this query's mounted image grid and its review
    // state, and still requires the parent navigation guard's explicit choice.
    await expect.poll(() => api.reads(imagePath('uncertain')).length).toBeGreaterThan(1);
    await expect(region.locator('.library-artwork-grid')).toHaveAttribute('aria-busy', 'false');
    if (failure.status === 503) {
      await region.getByText(reviewMessage, { exact: true }).scrollIntoViewIfNeeded();
      await evidence(page, test.info(), 'design2-library-images-unknown-review');
    }
    fixture.items[0] = { ...fixture.items[0], Name: 'Uncertain image refreshed' };
    const pageReads = api.reads(itemsPath).length;
    await refreshItemsThroughReviewGuard(page, false);
    expect(api.reads(itemsPath)).toHaveLength(pageReads);
    await expect(status(region, 'uncertain')).toHaveAttribute('data-download-status', 'unknown');
    await expect(region.getByRole('button', { name: downloadLabel, exact: true })).toBeDisabled();
    expect(requestedIds(api)).toEqual(['uncertain']);

    await refreshItemsThroughReviewGuard(page, true);
    await expect.poll(() => api.reads(itemsPath).length).toBeGreaterThan(pageReads);
    await expect(entry(region, 'uncertain')).toContainText('Uncertain image refreshed');
    await expect(region.locator('.library-artwork-grid')).toHaveAttribute('aria-busy', 'false');
    await expect(status(region, 'uncertain')).toHaveAttribute('data-download-status', 'unknown');
    await expect(region.getByRole('button', { name: downloadLabel, exact: true })).toBeDisabled();
    expect(requestedIds(api)).toEqual(['uncertain']);

    fixture.readFailures.add('uncertain');
    const failedReads = api.reads(imagePath('uncertain')).length;
    await region.getByRole('button', { name: 'Reload images', exact: true }).click();
    await expect.poll(() => api.reads(imagePath('uncertain')).length).toBeGreaterThan(failedReads);
    await expect(region.locator('.library-artwork-grid')).toHaveAttribute('aria-busy', 'false');
    await expect(status(region, 'uncertain')).toHaveAttribute('data-download-status', 'unknown');
    await expect(region.getByRole('button', { name: downloadLabel, exact: true })).toBeDisabled();
    expect(requestedIds(api)).toEqual(['uncertain']);

    fixture.readFailures.delete('uncertain');
    await region.getByRole('button', { name: 'Reload images', exact: true }).click();
    await expect(status(region, 'uncertain')).toHaveAttribute('data-download-status', 'reviewed');
    await expect(region.locator('.library-artwork-grid')).toHaveAttribute('aria-busy', 'false');
    await expect(region.getByText(reviewMessage, { exact: true })).toHaveCount(0);
    await expect(region.getByRole('button', { name: downloadLabel, exact: true })).toBeEnabled();
    await expect(entry(region, 'uncertain').getByRole('img')).toHaveAttribute('src', new RegExp(`tag=${failure.tag}$`));
    await region.getByRole('button', { name: downloadLabel, exact: true }).click();
    await expect(status(region, 'remaining-two')).toHaveAttribute('data-download-status', 'downloaded');
    expect(requestedIds(api)).toEqual(['uncertain', 'remaining-one', 'remaining-two']);
    expect(downloads(api).filter((request) => request.path === missingPath('uncertain'))).toHaveLength(1);
  });
}

for (const code of ['artwork_limit', 'provider_quota']) {
  test(`a known ${code} rejection offers one explicit retry without advancing the remaining queue`, async ({ page, api }) => {
    const fixture = installLibraryImages(api, [{ id: 'limited', name: 'Limited image' }, { id: 'later', name: 'Later image' }]);
    const ordinaryMissing = fixture.missing;
    let rejected = false;
    fixture.missing = async (route, request, id, input) => {
      if (id === 'limited' && !rejected) {
        rejected = true;
        return json(route, { Error: { Code: code, Message: 'This image request was rejected before saving.' } }, 429);
      }
      return ordinaryMissing(route, request, id, input);
    };
    const region = await openLibraryImages(page, api, fixture);
    await region.getByRole('button', { name: downloadLabel, exact: true }).click();
    await expect(status(region, 'limited')).toHaveAttribute('data-download-status', 'failed');
    await expect(status(region, 'later')).toHaveAttribute('data-download-status', 'not_attempted');
    await expect(region.locator('.library-artwork-grid')).toHaveAttribute('aria-busy', 'false');
    await expect(region.getByText(reviewMessage, { exact: true })).toHaveCount(0);
    const retry = region.getByRole('button', { name: 'Retry image download: Limited image', exact: true });
    await expect(retry).toBeEnabled();
    expect(fixture.collections['limited'].Items).toEqual([]);
    expect(requestedIds(api)).toEqual(['limited']);
    await retry.click();
    await expect(status(region, 'limited')).toHaveAttribute('data-download-status', 'downloaded');
    await expect(status(region, 'later')).toHaveAttribute('data-download-status', 'not_attempted');
    expect(requestedIds(api)).toEqual(['limited', 'limited']);
    await expect(entry(region, 'limited').getByRole('img')).toHaveAttribute('src', /tag=limited-Primary-downloaded$/);
  });
}

test('unreadable image collections are excluded from missing downloads and filters', async ({ page, api }) => {
  const fixture = installLibraryImages(api, [
    { id: 'unreadable', name: 'Unreadable image status' }, { id: 'readable', name: 'Readable missing image' },
    { id: 'present', name: 'Present image', images: ['Primary'] },
  ]);
  fixture.readFailures.add('unreadable');
  const region = await openLibraryImages(page, api, fixture);
  await expect(entry(region, 'unreadable')).toContainText('Image status unavailable');
  await expect(region.getByText('Some image collections could not be read. They are excluded from missing-image downloads.', { exact: false })).toBeVisible();
  await showFilter(region).selectOption('missing');
  await expect(entry(region, 'readable')).toBeVisible();
  await expect(entry(region, 'unreadable')).toHaveCount(0);
  await expect(entry(region, 'present')).toHaveCount(0);
  await showFilter(region).selectOption('all');
  await region.getByRole('button', { name: downloadLabel, exact: true }).click();
  await expect(status(region, 'readable')).toHaveAttribute('data-download-status', 'downloaded');
  expect(requestedIds(api)).toEqual(['readable']);
  await expect(entry(region, 'unreadable')).toContainText('Image status unavailable');
});

test('an image receipt for another library is unknown and stops the page queue', async ({ page, api }) => {
  const fixture = installLibraryImages(api, [{ id: 'wrong-scope', name: 'Wrong receipt scope' }, { id: 'untouched', name: 'Untouched image' }]);
  fixture.missing = (route, _request, id, input) => json(route, { ...fixture.result(id, input), LibraryId: 'c'.repeat(32) });
  const region = await openLibraryImages(page, api, fixture);
  await region.getByRole('button', { name: downloadLabel, exact: true }).click();
  await expect(status(region, 'wrong-scope')).toHaveAttribute('data-download-status', 'unknown');
  await expect(status(region, 'untouched')).toHaveAttribute('data-download-status', 'not_attempted');
  await expect(region.getByRole('button', { name: downloadLabel, exact: true })).toBeDisabled();
  await expect(region.getByRole('button', { name: /^Retry image download/ })).toHaveCount(0);
  expect(requestedIds(api)).toEqual(['wrong-scope']);
});
