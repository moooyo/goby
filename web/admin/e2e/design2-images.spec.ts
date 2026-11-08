import type { Locator, Page, Request, Route } from '@playwright/test';
import type { MetadataDetail, MetadataUpdateInput } from '../src/api';
import type { ArtworkCollection, ArtworkImage, ImageType } from '../src/artworkApi';
import type { ImageCandidate } from '../src/providersApi';
import type { DashboardAPI } from './dashboard-fixture';
import { csrf, expect, json, library, movieMetadata, test } from './dashboard-fixture';
import { evidence } from './dashboard-review';

// Every response is synthetic. The browser still decodes selected files and
// performs the editor's real request, guard, readback, and feedback flows.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off', preferences: { language: 'en', theme: 'light' } });

const itemPath = '/admin/v1/items/film-one';
const imagesPath = `${itemPath}/images`;
const metadataPath = `${itemPath}/metadata`;
const applyPath = `${itemPath}/providers/image`;
const searchPath = `${itemPath}/providers/images`;
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64');
const names: Partial<Record<ImageType, string>> = { Primary: 'Poster', Backdrop: 'Backdrop', Logo: 'Logo' };

interface ImageInput {
  Provider: string; Id: string; Language: string; ImageId: string;
  ImageType: ImageType; ImageIndex: number; Revision: string; ArtworkRevision: string;
}
interface ImageFixture {
  metadata: MetadataDetail;
  collection: ArtworkCollection;
  candidates: ImageCandidate[];
  apply: (route: Route, request: Request) => Promise<void>;
  acknowledge: (input: ImageInput) => void;
}

function savedImage(type: ImageType, tag: string, source = 'provider'): ArtworkImage {
  const [width, height] = type === 'Primary' ? [2000, 3000] : type === 'Backdrop' ? [3840, 2160] : [800, 310];
  return { ImageType: type, ImageIndex: 0, Tag: tag, Width: width, Height: height,
    Size: '4096', Source: source, PreviewUrl: `${imagesPath}/${type}/0/content?tag=${tag}` };
}

function candidate(type: ImageType, id: string): ImageCandidate {
  const image = savedImage(type, id);
  return { Provider: 'tmdb', Id: '12345', Type: 'Movie', Language: 'en', ImageId: id,
    ImageType: type, Width: image.Width, Height: image.Height,
    PreviewUrl: `${itemPath}/providers/image-preview?image=${id}` };
}

function imageAcknowledgement(input: ImageInput, artwork: ArtworkCollection) {
  const image = savedImage(input.ImageType, input.ImageId);
  return { Provider: input.Provider, ProviderId: input.Id, ImageId: input.ImageId,
    ImageType: input.ImageType, ImageIndex: input.ImageIndex, Width: image.Width,
    Height: image.Height, Tag: input.ImageId, Artwork: structuredClone(artwork) };
}

function installImages(api: DashboardAPI): ImageFixture {
  const fixture: ImageFixture = {
    metadata: structuredClone(movieMetadata),
    collection: { Revision: '12', Items: [savedImage('Primary', 'old-poster'), savedImage('Backdrop', 'old-backdrop'), savedImage('Logo', 'old-logo')] },
    candidates: [candidate('Primary', 'poster-one'), candidate('Primary', 'poster-two'), candidate('Backdrop', 'backdrop-one'), candidate('Logo', 'logo-one')],
    apply: async (route, request) => {
      const input = request.postDataJSON() as ImageInput;
      expect(input.Revision).toBe(fixture.metadata.Revision);
      expect(input.ArtworkRevision).toBe(fixture.collection.Revision);
      expect(input.Id).toBe(fixture.metadata.Effective.ProviderIds.Tmdb);
      fixture.acknowledge(input);
      await json(route, imageAcknowledgement(input, fixture.collection));
    },
    acknowledge: (input) => {
      fixture.collection = { Revision: (BigInt(fixture.collection.Revision) + 1n).toString(),
        Items: [...fixture.collection.Items.filter((image) => image.ImageType !== input.ImageType || image.ImageIndex !== input.ImageIndex),
          { ...savedImage(input.ImageType, input.ImageId, 'managed'), ImageIndex: input.ImageIndex }] };
    },
  };
  api.handlers.set(`GET ${metadataPath}`, (route) => json(route, fixture.metadata));
  api.handlers.set(`GET ${imagesPath}`, (route) => json(route, fixture.collection));
  api.handlers.set('GET /admin/v1/providers', (route) => json(route, { Enabled: true, Items: [
    { Id: 'tmdb', Name: 'TMDB', Configured: true, Capabilities: ['metadata', 'images'], Attribution: 'Synthetic image provider', Website: 'https://www.themoviedb.org/' },
  ] }));
  api.handlers.set(`GET ${itemPath}/providers/provenance`, (route) => json(route, { Items: [] }));
  api.handlers.set(`POST ${searchPath}`, (route, request) => {
    const input = request.postDataJSON() as { Provider: string; Id: string; Language: string };
    expect(input).toEqual({ Provider: 'tmdb', Id: fixture.metadata.Effective.ProviderIds.Tmdb, Language: 'en' });
    return json(route, { Items: fixture.candidates.map((entry) => ({ ...entry, Id: input.Id })) });
  });
  api.handlers.set(`POST ${applyPath}`, (route, request) => fixture.apply(route, request));
  for (const pathname of [`${itemPath}/providers/image-preview`, ...['Primary', 'Backdrop', 'Logo'].map((type) => `${imagesPath}/${type}/0/content`)]) {
    api.handlers.set(`GET ${pathname}`, (route) => route.fulfill({ contentType: 'image/png', body: png }));
  }
  return fixture;
}

async function openEditor(page: Page): Promise<Locator> {
  await page.goto(`/admin/media/libraries/${library.Id}/items`);
  await page.getByRole('button', { name: 'Edit metadata for The feature film', exact: true }).first().click();
  const sheet = page.getByRole('dialog', { name: /^Edit metadata/ });
  await expect(sheet.getByRole('tab', { name: 'Basic information', exact: true })).toBeVisible();
  return sheet;
}

async function openImages(page: Page): Promise<Locator> {
  const sheet = await openEditor(page);
  await sheet.getByRole('tab', { name: 'Images', exact: true }).click();
  await expect(sheet.getByRole('region', { name: 'Online image candidates', exact: true })).toBeVisible();
  await expect(imageCandidate(sheet, 'poster-one')).toBeVisible();
  return sheet;
}

function imageCandidate(sheet: Locator, id: string): Locator {
  return sheet.getByRole('region', { name: 'Online image candidates', exact: true }).locator(`[data-candidate-id="${id}"] > button.artwork-candidate-choice`);
}
function currentImage(sheet: Locator): Locator {
  return sheet.getByRole('region', { name: 'Current image', exact: true }).getByRole('img').first();
}
function selectType(sheet: Locator, type: ImageType): Promise<void> {
  return sheet.getByRole('group', { name: 'Image type', exact: true }).getByRole('button', { name: names[type]!, exact: true }).click();
}
function imageWrites(api: DashboardAPI) {
  return api.writes().filter((request) => request.path === applyPath || request.path.startsWith(`${imagesPath}/`));
}
function metadataWrites(api: DashboardAPI) {
  return api.writes().filter((request) => request.path === metadataPath);
}

test('online candidates replace images immediately and serialize writes across all image types', async ({ page, api }) => {
  const fixture = installImages(api);
  const ordinaryApply = fixture.apply;
  let release = () => {};
  const pending = new Promise<void>((resolve) => { release = resolve; });
  fixture.apply = async (route, request) => { await pending; await ordinaryApply(route, request); };
  const sheet = await openImages(page);
  const poster = imageCandidate(sheet, 'poster-one');
  await expect(poster).toHaveAttribute('aria-pressed', 'false');
  await expect(sheet.locator('.artwork-editor-body')).toHaveAttribute('aria-busy', 'false');
  await poster.scrollIntoViewIfNeeded();
  await evidence(page, test.info(), 'design2-images-candidates-initial');
  try {
    await poster.click();
    await expect.poll(() => imageWrites(api).length).toBe(1);
    await expect(poster).toBeDisabled();
    await expect(sheet.getByLabel('Choose image file', { exact: true })).toBeDisabled();
    await sheet.locator('.artwork-editor-footer').scrollIntoViewIfNeeded();
    await evidence(page, test.info(), 'design2-images-candidate-applying');
    await selectType(sheet, 'Backdrop');
    await expect(imageCandidate(sheet, 'backdrop-one')).toBeDisabled();
    await page.keyboard.press('Control+s');
    expect(imageWrites(api)).toHaveLength(1);
    expect(metadataWrites(api)).toEqual([]);
  } finally { release(); }
  await selectType(sheet, 'Primary');
  await expect(poster).toHaveAttribute('aria-pressed', 'true');
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=poster-one$/);
  await expect(sheet.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'success');
  expect(imageWrites(api)[0]).toMatchObject({ method: 'POST', path: applyPath, body: {
    Provider: 'tmdb', Id: '12345', Language: 'en', ImageId: 'poster-one', ImageType: 'Primary', ImageIndex: 0,
    Revision: movieMetadata.Revision, ArtworkRevision: '12',
  }, csrf });

  await selectType(sheet, 'Backdrop');
  await imageCandidate(sheet, 'backdrop-one').click();
  await expect(imageCandidate(sheet, 'backdrop-one')).toHaveAttribute('aria-pressed', 'true');
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=backdrop-one$/);
  await selectType(sheet, 'Logo');
  await imageCandidate(sheet, 'logo-one').click();
  await expect(imageCandidate(sheet, 'logo-one')).toHaveAttribute('aria-pressed', 'true');
  await selectType(sheet, 'Primary');
  await expect(poster).toHaveAttribute('aria-pressed', 'true');
  await expect(sheet.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'success');
  await selectType(sheet, 'Backdrop');
  await expect(imageCandidate(sheet, 'backdrop-one')).toHaveAttribute('aria-pressed', 'true');
  await expect(sheet.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'success');
  expect(imageWrites(api)).toHaveLength(3);
  expect(metadataWrites(api)).toEqual([]);
});

test('a provider rejection keeps the saved image and offers an explicit candidate retry', async ({ page, api }) => {
  const fixture = installImages(api);
  const ordinaryApply = fixture.apply;
  let attempts = 0;
  fixture.apply = async (route, request) => {
    attempts += 1;
    if (attempts === 1) return json(route, { Error: { Code: 'provider_quota', Message: 'The provider rejected the request before saving an image.' } }, 429);
    await ordinaryApply(route, request);
  };
  const sheet = await openImages(page);
  await imageCandidate(sheet, 'poster-one').click();
  const retry = imageCandidate(sheet, 'poster-one');
  await expect(retry).toHaveAccessibleName(/^Retry: /);
  await expect(retry).toBeEnabled();
  await expect(retry).toHaveAttribute('aria-pressed', 'false');
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=old-poster$/);
  await selectType(sheet, 'Backdrop');
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=old-backdrop$/);
  await selectType(sheet, 'Primary');
  await expect(retry).toHaveAccessibleName(/^Retry: /);
  await expect(sheet.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'failed');
  await sheet.locator('.artwork-editor-footer').scrollIntoViewIfNeeded();
  await evidence(page, test.info(), 'design2-images-retryable-failure');
  await retry.click();
  await expect(retry).toHaveAttribute('aria-pressed', 'true');
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=poster-one$/);
  expect(imageWrites(api)).toHaveLength(2);
});

for (const digits of [77, 78]) {
  test(`a ${digits}-digit artwork revision is preserved exactly in provider selection`, async ({ page, api }) => {
    const fixture = installImages(api);
    const revision = digits === 77 ? '9'.repeat(77) : '1' + '0'.repeat(76) + '7';
    fixture.collection.Revision = revision;
    const sheet = await openImages(page);
    await imageCandidate(sheet, 'poster-one').click();
    await expect(imageCandidate(sheet, 'poster-one')).toHaveAttribute('aria-pressed', 'true');
    expect(imageWrites(api)).toHaveLength(1);
    expect(imageWrites(api)[0]).toMatchObject({ body: { Revision: movieMetadata.Revision, ArtworkRevision: revision } });
    expect(fixture.collection.Revision).toBe((BigInt(revision) + 1n).toString());
  });
}

for (const failure of [
  { name: 'a revision conflict', status: 409, code: 'revision_conflict', saved: 'concurrent-poster' },
  { name: 'an unavailable response after a write', status: 503, code: 'operation_unconfirmed', saved: 'poster-one' },
  { name: 'a dropped network response', status: 0, code: 'network_error', saved: 'poster-one' },
]) {
  test(`an image write requires readback after ${failure.name} and cannot be replayed`, async ({ page, api }) => {
    const fixture = installImages(api);
    const ordinaryApply = fixture.apply;
    let attempts = 0;
    fixture.apply = async (route, request) => {
      attempts += 1;
      if (attempts > 1) return ordinaryApply(route, request);
      fixture.collection = { Revision: '13', Items: fixture.collection.Items.map((image) => image.ImageType === 'Primary' ? savedImage('Primary', failure.saved) : image) };
      fixture.metadata = { ...fixture.metadata, Revision: '9007199254740994' };
      if (failure.status === 0) return route.abort('failed');
      await json(route, { Error: { Code: failure.code, Message: 'The image operation must be reviewed.' } }, failure.status);
    };
    const sheet = await openImages(page);
    await imageCandidate(sheet, 'poster-one').click();
    await expect(sheet.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'unknown');
    await expect(imageCandidate(sheet, 'poster-one')).toBeDisabled();
    await expect(sheet.getByLabel('Choose image file', { exact: true })).toBeDisabled();
    await expect(currentImage(sheet)).toHaveAttribute('src', /tag=old-poster$/);
    if (failure.status === 503) {
      await expect(sheet.locator('.artwork-editor-body')).toHaveAttribute('aria-busy', 'false');
      await sheet.locator('.artwork-editor-footer').scrollIntoViewIfNeeded();
      await evidence(page, test.info(), 'design2-images-unknown-review');
    }
    await selectType(sheet, 'Backdrop');
    await expect(imageCandidate(sheet, 'backdrop-one')).toBeDisabled();
    await page.keyboard.press('Control+s');
    expect(imageWrites(api)).toHaveLength(1);
    expect(metadataWrites(api)).toEqual([]);

    const reads = api.reads(imagesPath).length;
    await sheet.getByRole('button', { name: 'Reload images', exact: true }).click();
    await expect.poll(() => api.reads(imagesPath).length).toBeGreaterThan(reads);
    await selectType(sheet, 'Primary');
    await expect(currentImage(sheet)).toHaveAttribute('src', new RegExp(`tag=${failure.saved}$`));
    await expect(imageCandidate(sheet, 'poster-one')).toBeEnabled();
    // Reload invalidates session-only candidate provenance, including a write
    // which might have committed before its response was lost.
    await expect(imageCandidate(sheet, 'poster-one')).toHaveAttribute('aria-pressed', 'false');
    expect(imageWrites(api)).toHaveLength(1);
    await imageCandidate(sheet, 'poster-two').click();
    await expect(imageCandidate(sheet, 'poster-two')).toHaveAttribute('aria-pressed', 'true');
    expect(imageWrites(api)).toHaveLength(2);
    expect(imageWrites(api)[1]).toMatchObject({ body: { Revision: '9007199254740994', ArtworkRevision: '13', ImageId: 'poster-two' } });
  });
}

test('an invalid atomic artwork snapshot remains unconfirmed when an explicit reload also fails', async ({ page, api }) => {
  const fixture = installImages(api);
  let failReadback = false;
  let rejectedReadbacks = 0;
  fixture.apply = async (route, request) => {
    const input = request.postDataJSON() as ImageInput;
    fixture.acknowledge(input);
    failReadback = true;
    const response = imageAcknowledgement(input, fixture.collection);
    response.Artwork.Items[0].PreviewUrl = 'https://untrusted.invalid/image.png';
    await json(route, response);
  };
  api.handlers.set(`GET ${imagesPath}`, (route) => {
    if (!failReadback) return json(route, fixture.collection);
    rejectedReadbacks += 1;
    return json(route, { Error: { Code: 'artwork_unavailable', Message: 'The current image could not be read.' } }, 503);
  });
  const sheet = await openImages(page);
  await imageCandidate(sheet, 'poster-one').click();
  await expect(sheet.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'unknown');
  await expect(imageCandidate(sheet, 'poster-one')).toBeDisabled();
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=old-poster$/);
  await expect(imageCandidate(sheet, 'poster-one')).toHaveAttribute('aria-pressed', 'false');
  expect(imageWrites(api)).toHaveLength(1);
  await sheet.getByRole('button', { name: 'Reload images', exact: true }).click();
  await expect.poll(() => rejectedReadbacks).toBeGreaterThan(0);
  await expect(sheet.locator('.artwork-editor-body')).toHaveAttribute('aria-busy', 'false');
  await expect(sheet.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'unknown');
  await expect(imageCandidate(sheet, 'poster-one')).toBeDisabled();
  await expect(sheet.getByLabel('Choose image file', { exact: true })).toBeDisabled();
  expect(imageWrites(api)).toHaveLength(1);
  failReadback = false;
  await sheet.getByRole('button', { name: 'Reload images', exact: true }).click();
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=poster-one$/);
  await expect(imageCandidate(sheet, 'poster-one')).toBeEnabled();
  await expect(imageCandidate(sheet, 'poster-one')).toHaveAttribute('aria-pressed', 'false');
  expect(imageWrites(api)).toHaveLength(1);
});

test('an invalid image acknowledgement remains unconfirmed until a successful reload', async ({ page, api }) => {
  const fixture = installImages(api);
  fixture.apply = (route, request) => {
    fixture.acknowledge(request.postDataJSON() as ImageInput);
    return json(route, {});
  };
  const sheet = await openImages(page);
  await imageCandidate(sheet, 'poster-one').click();
  await expect(sheet.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'unknown');
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=old-poster$/);
  await expect(imageCandidate(sheet, 'poster-one')).toBeDisabled();
  await expect(imageCandidate(sheet, 'poster-one')).toHaveAttribute('aria-pressed', 'false');
  await page.keyboard.press('Control+s');
  expect(imageWrites(api)).toHaveLength(1);
  await sheet.getByRole('button', { name: 'Reload images', exact: true }).click();
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=poster-one$/);
  await expect(imageCandidate(sheet, 'poster-one')).toBeEnabled();
  await expect(imageCandidate(sheet, 'poster-one')).toHaveAttribute('aria-pressed', 'false');
  expect(imageWrites(api)).toHaveLength(1);
});

test('selecting a readable local image uploads immediately without an upload draft', async ({ page, api }) => {
  const fixture = installImages(api);
  const uploads: { headers: Record<string, string>; body: Buffer | null }[] = [];
  await page.route(/\/admin\/v1\/items\/film-one\/images\/Primary\/0$/, async (route, request) => {
    expect(request.method()).toBe('PUT');
    uploads.push({ headers: request.headers(), body: request.postDataBuffer() });
    fixture.collection = { Revision: '13', Items: fixture.collection.Items.map((image) => image.ImageType === 'Primary' ? savedImage('Primary', 'uploaded-poster', 'managed') : image) };
    await json(route, fixture.collection);
  });
  const sheet = await openImages(page);
  const cspViolations = await page.evaluateHandle(() => {
    const violations: { directive: string; blockedURI: string }[] = [];
    document.addEventListener('securitypolicyviolation', (event) => {
      violations.push({ directive: event.effectiveDirective, blockedURI: event.blockedURI });
    });
    const policy = document.createElement('meta');
    policy.httpEquiv = 'Content-Security-Policy';
    policy.content = "img-src 'self' data:";
    document.head.append(policy);
    return violations;
  });
  await sheet.getByLabel('Choose image file', { exact: true }).setInputFiles({ name: 'synthetic-poster.png', mimeType: 'image/png', buffer: png });
  await expect.poll(async () => ({
    uploads: uploads.length,
    imageViolations: (await cspViolations.jsonValue()).filter((entry) => entry.directive.startsWith('img-src')),
  }), { message: "Local image decoding must respect img-src 'self' data: and immediately upload the selected file." }).toEqual({ uploads: 1, imageViolations: [] });
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=uploaded-poster$/);
  await expect(sheet.locator('.artwork-editor-footer')).toContainText('Image saved.');
  await expect(sheet.getByRole('button', { name: 'Upload image', exact: true })).toHaveCount(0);
  await expect(sheet.getByText('Selected image not saved', { exact: true })).toHaveCount(0);
  expect(uploads[0].headers['if-match']).toBe('"12"');
  expect(uploads[0].headers['content-type']).toBe('image/png');
  expect(uploads[0].headers['x-csrf-token']).toBe(csrf);
  expect(uploads[0].body).toEqual(png);
  await expect(sheet.getByLabel('Choose image file', { exact: true })).toHaveValue('');
  await sheet.getByRole('region', { name: 'Current image', exact: true }).scrollIntoViewIfNeeded();
  await evidence(page, test.info(), 'design2-images-immediate-upload-saved');
  await page.keyboard.press('Control+s');
  expect(uploads).toHaveLength(1);
  expect(metadataWrites(api)).toEqual([]);
  expect((await cspViolations.jsonValue()).filter((entry) => entry.directive.startsWith('img-src'))).toEqual([]);
  await cspViolations.dispose();
});

test('an unreadable local image fails before any upload request is sent', async ({ page, api }) => {
  installImages(api);
  let uploads = 0;
  await page.route(/\/admin\/v1\/items\/film-one\/images\/Primary\/0$/, (route) => {
    uploads += 1;
    return json(route, { Error: { Code: 'invalid_image', Message: 'No unreadable image may be uploaded.' } }, 400);
  });
  const sheet = await openImages(page);
  await sheet.getByLabel('Choose image file', { exact: true }).setInputFiles({
    name: 'unreadable-poster.png', mimeType: 'image/png', buffer: Buffer.from('This is not a readable PNG image.'),
  });
  await expect(sheet.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'failed');
  await expect(sheet).toContainText('Unable to read this image. Choose a different file.');
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=old-poster$/);
  await expect(sheet.getByLabel('Choose image file', { exact: true })).toHaveValue('');
  await expect(imageCandidate(sheet, 'poster-one')).toBeEnabled();
  expect(uploads).toBe(0);
  expect(imageWrites(api)).toEqual([]);
});

test('keeping a metadata draft cancels image navigation without saving or searching images', async ({ page, api }) => {
  installImages(api);
  const sheet = await openEditor(page);
  const title = sheet.getByRole('textbox', { name: 'Title', exact: true });
  await title.fill('The feature film draft');
  await sheet.getByRole('tab', { name: 'Images', exact: true }).click();
  const prompt = page.getByRole('alertdialog', { name: 'Edit images with unsaved metadata?', exact: true });
  await expect(prompt).toBeVisible();
  await expect(page.locator('#metadata-tab-0')).toHaveAttribute('aria-selected', 'true');
  await evidence(page, test.info(), 'design2-images-metadata-draft-choices');
  expect(metadataWrites(api)).toEqual([]);
  expect(imageWrites(api)).toEqual([]);
  await prompt.getByRole('button', { name: 'Keep editing', exact: true }).click();
  await expect(prompt).not.toBeVisible();
  await expect(title).toHaveValue('The feature film draft');
  await expect(sheet.getByRole('region', { name: 'Online image candidates', exact: true })).toHaveCount(0);
  await expect(sheet.getByRole('button', { name: 'Save changes', exact: true })).toBeEnabled();
  await sheet.getByRole('button', { name: 'Close', exact: true }).click();
  const discard = page.getByRole('alertdialog', { name: 'Discard unsaved metadata changes?', exact: true });
  await expect(discard).toBeVisible();
  await discard.getByRole('button', { name: 'Keep editing', exact: true }).click();
  await expect(title).toHaveValue('The feature film draft');
  expect(metadataWrites(api)).toEqual([]);
  expect(api.writes().filter((request) => request.path === searchPath)).toEqual([]);
});

test('discarding the metadata draft opens images with the saved provider identity and no metadata write', async ({ page, api }) => {
  installImages(api);
  const sheet = await openEditor(page);
  await sheet.getByRole('textbox', { name: 'Title', exact: true }).fill('The feature film draft');
  await sheet.locator('details.editor-advanced-fields > summary').click();
  await sheet.getByRole('group', { name: 'Provider identifiers', exact: true }).getByRole('textbox', { name: 'Identifier', exact: true }).fill('67890');
  await sheet.getByRole('tab', { name: 'Images', exact: true }).click();
  const prompt = page.getByRole('alertdialog', { name: 'Edit images with unsaved metadata?', exact: true });
  await prompt.getByRole('button', { name: 'Discard metadata and edit images', exact: true }).click();
  await expect(imageCandidate(sheet, 'poster-one')).toBeVisible();
  await expect(sheet.getByRole('button', { name: 'Save changes', exact: true })).toHaveCount(0);
  await expect(sheet.getByRole('button', { name: 'Discard', exact: true })).toHaveCount(0);
  await page.keyboard.press('Control+s');
  expect(metadataWrites(api)).toEqual([]);
  await imageCandidate(sheet, 'poster-one').click();
  await expect(imageCandidate(sheet, 'poster-one')).toHaveAttribute('aria-pressed', 'true');
  expect(imageWrites(api)[0]).toMatchObject({ body: { Id: '12345', Revision: movieMetadata.Revision, ArtworkRevision: '12' } });
  await sheet.getByRole('tab', { name: 'Basic information', exact: true }).click();
  await expect(sheet.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(movieMetadata.Effective.Name);
  await expect(sheet.getByRole('group', { name: 'Provider identifiers', exact: true }).getByRole('textbox', { name: 'Identifier', exact: true })).toHaveValue('12345');
  await expect(sheet.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  expect(metadataWrites(api)).toEqual([]);
});

test('saving metadata before editing images waits for the explicit save and uses its saved provider identity', async ({ page, api }) => {
  const fixture = installImages(api);
  let release = () => {};
  const pending = new Promise<void>((resolve) => { release = resolve; });
  api.handlers.set(`PUT ${metadataPath}`, async (route, request) => {
    const input = request.postDataJSON() as MetadataUpdateInput;
    expect(input).toEqual({ Revision: movieMetadata.Revision,
      Overrides: { Name: 'The feature film draft', ProviderIds: { Tmdb: '67890' } }, LockedFields: ['Name', 'ProviderIds'] });
    await pending;
    const effective = { ...fixture.metadata.Automatic, ...input.Overrides };
    fixture.metadata = { ...fixture.metadata, Revision: '9007199254740994',
      Overrides: structuredClone(input.Overrides), LockedFields: [...input.LockedFields],
      LockedValues: Object.fromEntries(input.LockedFields.map((field) => [field, effective[field]])),
      Item: { ...fixture.metadata.Item, Name: effective.Name }, Effective: effective };
    await json(route, fixture.metadata);
  });
  const sheet = await openEditor(page);
  await sheet.getByRole('tab', { name: 'Images', exact: true }).click();
  await expect(imageCandidate(sheet, 'poster-one')).toBeVisible();
  await sheet.getByRole('tab', { name: 'Basic information', exact: true }).click();
  const searchesBeforeSave = api.writes().filter((request) => request.path === searchPath).length;
  await sheet.getByRole('textbox', { name: 'Title', exact: true }).fill('The feature film draft');
  await sheet.locator('details.editor-advanced-fields > summary').click();
  await sheet.getByRole('group', { name: 'Provider identifiers', exact: true }).getByRole('textbox', { name: 'Identifier', exact: true }).fill('67890');
  await sheet.getByRole('tab', { name: 'Images', exact: true }).click();
  const prompt = page.getByRole('alertdialog', { name: 'Edit images with unsaved metadata?', exact: true });
  await expect(prompt).toBeVisible();
  expect(metadataWrites(api)).toEqual([]);
  try {
    await prompt.getByRole('button', { name: 'Save metadata and edit images', exact: true }).click();
    await expect.poll(() => metadataWrites(api).length).toBe(1);
    await expect(page.locator('#metadata-tab-0')).toHaveAttribute('aria-selected', 'true');
    await expect(sheet.getByRole('region', { name: 'Online image candidates', exact: true })).toHaveCount(0);
    await page.keyboard.press('Control+s');
    expect(metadataWrites(api)).toHaveLength(1);
    expect(imageWrites(api)).toEqual([]);
    expect(api.writes().filter((request) => request.path === searchPath)).toHaveLength(searchesBeforeSave);
  } finally { release(); }
  await expect(sheet.getByRole('tab', { name: 'Images', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect.poll(() => api.writes().filter((request) => request.path === searchPath).at(-1)?.body).toMatchObject({ Id: '67890' });
  await expect(sheet.getByRole('region', { name: 'Online image candidates', exact: true })).toHaveAttribute('aria-busy', 'false');
  await expect(imageCandidate(sheet, 'poster-one')).toBeVisible();
  await imageCandidate(sheet, 'poster-one').click();
  await expect(imageCandidate(sheet, 'poster-one')).toHaveAttribute('aria-pressed', 'true');
  expect(metadataWrites(api)).toHaveLength(1);
  expect(imageWrites(api)[0]).toMatchObject({ body: { Id: '67890', Revision: '9007199254740994', ArtworkRevision: '12' } });
  expect(api.writes().findIndex((request) => request.path === metadataPath))
    .toBeLessThan(api.writes().findIndex((request) => request.path === searchPath && (request.body as { Id: string }).Id === '67890'));
});

for (const failure of [
  { status: 409, code: 'revision_conflict', message: 'This metadata changed after you opened it.' },
  { status: 503, code: 'operation_unconfirmed', message: 'Your changes may already have been saved.' },
]) {
  test(`a failed metadata save (${failure.status}) retains the draft and does not enter images`, async ({ page, api }) => {
    installImages(api);
    api.handlers.set(`PUT ${metadataPath}`, (route) => json(route, { Error: { Code: failure.code, Message: 'The metadata write needs review.' } }, failure.status));
    const sheet = await openEditor(page);
    const title = sheet.getByRole('textbox', { name: 'Title', exact: true });
    await title.fill('The feature film draft');
    await sheet.getByRole('tab', { name: 'Images', exact: true }).click();
    await page.getByRole('alertdialog', { name: 'Edit images with unsaved metadata?', exact: true })
      .getByRole('button', { name: 'Save metadata and edit images', exact: true }).click();
    await expect(sheet).toContainText(failure.message);
    await expect(sheet.getByRole('tab', { name: 'Basic information', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(title).toHaveValue('The feature film draft');
    await expect(sheet.getByRole('region', { name: 'Online image candidates', exact: true })).toHaveCount(0);
    await expect(sheet.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
    await page.keyboard.press('Control+s');
    expect(metadataWrites(api)).toHaveLength(1);
    expect(imageWrites(api)).toEqual([]);
    expect(api.writes().filter((request) => request.path === searchPath)).toEqual([]);
  });
}

test('a provider selection replaces a managed image using the atomic artwork snapshot', async ({ page, api }) => {
  const fixture = installImages(api);
  fixture.collection.Items = fixture.collection.Items.map((image) => ({ ...image, Source: 'managed' }));
  const sheet = await openImages(page);
  await expect(imageCandidate(sheet, 'poster-one')).toHaveAttribute('aria-pressed', 'false');
  const reads = api.reads(imagesPath).length;
  await imageCandidate(sheet, 'poster-one').click();
  await expect(sheet.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'success');
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=poster-one$/);
  await expect(imageCandidate(sheet, 'poster-one')).toHaveAttribute('aria-pressed', 'true');
  expect(imageWrites(api)).toHaveLength(1);
  expect(imageWrites(api)[0]).toMatchObject({ body: { ArtworkRevision: '12', Revision: movieMetadata.Revision } });
  expect(api.reads(imagesPath)).toHaveLength(reads);
  await sheet.getByRole('region', { name: 'Current image', exact: true }).scrollIntoViewIfNeeded();
  await evidence(page, test.info(), 'design2-images-managed-replaced');
});

test('a mismatch between the selected image and its atomic artwork snapshot requires review', async ({ page, api }) => {
  const fixture = installImages(api);
  fixture.apply = (route, request) => {
    const input = request.postDataJSON() as ImageInput;
    fixture.acknowledge(input);
    const response = imageAcknowledgement(input, fixture.collection);
    response.Tag = 'a-different-image-tag';
    return json(route, response);
  };
  const sheet = await openImages(page);
  await imageCandidate(sheet, 'poster-one').click();
  await expect(sheet.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'unknown');
  await expect(currentImage(sheet)).toHaveAttribute('src', /tag=old-poster$/);
  await expect(imageCandidate(sheet, 'poster-one')).toHaveAttribute('aria-pressed', 'false');
  await expect(imageCandidate(sheet, 'poster-one')).toBeDisabled();
  await page.keyboard.press('Control+s');
  expect(imageWrites(api)).toHaveLength(1);
});
