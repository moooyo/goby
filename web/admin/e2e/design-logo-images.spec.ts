import type { Locator, Page } from '@playwright/test';
import type { ArtworkCollection, ArtworkImage } from '../src/artworkApi';
import type { DashboardAPI } from './dashboard-fixture';
import { csrf, expect, json, library, movieMetadata, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off' });

const item = '/admin/v1/items/film-one';
const imagesPath = item + '/images';
const imagePath = item + '/providers/image';
const previewMarker = 'synthetic-logo-preview-token';
const foreignPreview = `https://preview.invalid/logo.svg?api_key=${previewMarker}`;
const logo = '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="180"><text x="320" y="130" text-anchor="middle" fill="#e3b46a" font-family="sans-serif" font-size="110">Goby</text></svg>';
const backdrop = '<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080"><rect width="1920" height="1080" fill="#302b23"/></svg>';

const copy = {
  en: { images: 'Images', type: 'Image type', backdrop: 'Backdrop', logo: 'Logo', destination: 'Upload destination', candidates: 'Online image candidates', current: 'Current image' },
  zh: { images: '图片', type: '图片类型', backdrop: '背景', logo: '标志', destination: '上传位置', candidates: '在线候选', current: '当前图片' },
};
type Labels = typeof copy.en;

function savedImage(type: 'Logo' | 'Backdrop', index: number, tag: string): ArtworkImage {
  return { ImageType: type, ImageIndex: index, Tag: tag, Width: type === 'Logo' ? 640 : 1920,
    Height: type === 'Logo' ? 180 : 1080, Size: '2048', Source: 'managed',
    PreviewUrl: `${imagesPath}/${type}/${index}/content?tag=${tag}` };
}

function installLogoFixture(api: DashboardAPI, options: { previewUrl?: string; malformedArtwork?: 'url' | 'revision' } = {}) {
  const fixture = { collection: { Revision: '12', Items: [savedImage('Logo', 0, 'old-logo'), savedImage('Backdrop', 0, 'backdrop-zero'), savedImage('Backdrop', 2, 'backdrop-two')] } as ArtworkCollection };
  api.handlers.set('GET ' + item + '/metadata', (route) => json(route, movieMetadata));
  api.handlers.set('GET ' + imagesPath, (route) => json(route, fixture.collection));
  api.handlers.set('GET ' + item + '/providers/provenance', (route) => json(route, { Items: [] }));
  api.handlers.set('GET ' + item + '/providers/image-preview', (route, request) => route.fulfill({ contentType: 'image/svg+xml',
    body: new URL(request.url()).searchParams.get('candidate') === 'backdrop' ? backdrop : logo }));
  api.handlers.set('GET ' + imagesPath + '/Logo/0/content', (route) => route.fulfill({ contentType: 'image/svg+xml', body: logo }));
  for (const index of [0, 2]) api.handlers.set(`GET ${imagesPath}/Backdrop/${index}/content`, (route) => route.fulfill({ contentType: 'image/svg+xml', body: backdrop }));
  api.handlers.set('POST ' + item + '/providers/images', (route, request) => {
    expect(request.postDataJSON()).toEqual({ Provider: 'tmdb', Id: '12345', Language: 'en' });
    return json(route, { Items: [
      { Provider: 'tmdb', Id: '12345', Type: 'Movie', Language: 'en', ImageId: '/Logo.png', ImageType: 'Logo', Width: 640, Height: 180,
        PreviewUrl: options.previewUrl ?? item + '/providers/image-preview?candidate=logo' },
      { Provider: 'tmdb', Id: '12345', Type: 'Movie', Language: 'en', ImageId: '/Backdrop.png', ImageType: 'Backdrop', Width: 1920, Height: 1080,
        PreviewUrl: item + '/providers/image-preview?candidate=backdrop' },
    ] });
  });
  api.handlers.set('POST ' + imagePath, (route, request) => {
    const input = request.postDataJSON() as { ArtworkRevision: string };
    expect(input.ArtworkRevision).toBe(fixture.collection.Revision);
    fixture.collection = { Revision: '13', Items: fixture.collection.Items.map((image) => image.ImageType === 'Logo' ? savedImage('Logo', 0, 'saved-logo') : image) };
    const artwork = structuredClone(fixture.collection);
    if (options.malformedArtwork === 'url') artwork.Items.find((image) => image.ImageType === 'Logo')!.PreviewUrl = foreignPreview;
    if (options.malformedArtwork === 'revision') artwork.Revision = '1e2';
    return json(route, { Provider: 'tmdb', ProviderId: '12345', ImageId: '/Logo.png', ImageType: 'Logo', ImageIndex: 0,
      Width: 640, Height: 180, Tag: 'saved-logo', Artwork: artwork });
  });
  return fixture;
}

async function openLogoEditor(page: Page, labels: Labels = copy.en): Promise<{ editor: Locator; candidates: Locator; choice: Locator }> {
  await page.goto('/admin/media/libraries/' + library.Id + '/items');
  await page.getByRole('button', { name: 'Edit metadata for The feature film', exact: true }).first().click();
  const editor = page.locator('dialog.metadata-editor-sheet');
  await editor.getByRole('tab', { name: labels.images, exact: true }).click();
  const types = editor.getByRole('group', { name: labels.type, exact: true });
  await types.getByRole('button', { name: labels.backdrop, exact: true }).click();
  await editor.getByRole('combobox', { name: labels.destination, exact: true }).selectOption('2');
  await types.getByRole('button', { name: labels.logo, exact: true }).click();
  const candidates = editor.getByRole('region', { name: labels.candidates, exact: true });
  await expect(candidates).toHaveAttribute('aria-busy', 'false');
  await expect(candidates.locator('.artwork-candidate')).toHaveCount(1);
  const choice = candidates.locator('[data-candidate-id="/Logo.png"] > button.artwork-candidate-choice');
  await expect(choice).toBeVisible();
  return { editor, candidates, choice };
}

for (const appearance of ['dark', 'light'] as const) {
  const language = appearance === 'dark' ? 'en' : 'zh';
  const labels = copy[language];
  test.describe('Online logo ' + appearance, () => {
    test.use({ preferences: { language, theme: appearance }, viewport: { width: appearance === 'dark' ? 1440 : 390, height: 1000 } });
    test('filters transparent logo candidates and saves the single-image slot independently of the backdrop index', async ({ page, api }, testInfo) => {
      const fixture = installLogoFixture(api);
      const { editor, candidates, choice } = await openLogoEditor(page, labels);
      await expect(page.locator('html')).toHaveAttribute('lang', language === 'zh' ? 'zh-CN' : 'en');
      await expect(candidates.locator('.artwork-candidate-grid')).toHaveAttribute('data-image-type', 'Logo');
      await expect(candidates.locator('[data-candidate-id="/Backdrop.png"]')).toHaveCount(0);
      const preview = choice.getByRole('img');
      await expect(preview).toBeVisible();
      await expect.poll(() => preview.evaluate((element) => (element as HTMLImageElement).naturalWidth)).toBe(640);
      const transparency = await preview.evaluate((element) => {
        const image = element as HTMLImageElement;
        const canvas = document.createElement('canvas'); canvas.width = image.naturalWidth; canvas.height = image.naturalHeight;
        const context = canvas.getContext('2d')!; context.drawImage(image, 0, 0);
        return { alpha: context.getImageData(0, 0, 1, 1).data[3], fit: getComputedStyle(image).objectFit,
          background: getComputedStyle(image.parentElement!).backgroundImage };
      });
      expect(transparency.alpha).toBe(0);
      expect(transparency.fit).toBe('contain');
      expect(transparency.background).toContain('repeating-conic-gradient');
      await noOverflow(page);
      expect(await editor.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
      await evidence(page, testInfo, 'online-logo-candidate-' + appearance);

      await choice.click();
      await expect(editor.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'success');
      await expect(choice).toHaveAttribute('aria-pressed', 'true');
      const current = editor.getByRole('region', { name: labels.current, exact: true }).getByRole('img');
      await expect(current).toHaveAttribute('src', /\/Logo\/0\/content\?tag=saved-logo$/);
      expect(await current.evaluate((element) => getComputedStyle(element).backgroundImage)).toContain('repeating-conic-gradient');
      const writes = api.writes().filter((request) => request.path === imagePath);
      expect(writes).toHaveLength(1);
      expect(writes[0]).toMatchObject({ csrf });
      expect(writes[0].body).toEqual({ Provider: 'tmdb', Id: '12345', Language: 'en', ImageId: '/Logo.png', ImageType: 'Logo',
        ImageIndex: 0, Revision: movieMetadata.Revision, ArtworkRevision: '12' });
      expect(fixture.collection.Items.filter((image) => image.ImageType === 'Logo').map((image) => image.ImageIndex)).toEqual([0]);
      await editor.getByRole('group', { name: labels.type, exact: true }).getByRole('button', { name: labels.backdrop, exact: true }).click();
      await expect(editor.getByRole('combobox', { name: labels.destination, exact: true })).toHaveValue('2');
      await editor.getByRole('group', { name: labels.type, exact: true }).getByRole('button', { name: labels.logo, exact: true }).click();
      await expect(choice).toHaveAttribute('aria-pressed', 'true');
      await noOverflow(page);
      expect(await editor.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
      await evidence(page, testInfo, 'online-logo-saved-' + appearance);
    });
  });
}

test('foreign logo preview URLs are neither fetched nor forwarded to the image mutation', async ({ page, api }) => {
  installLogoFixture(api, { previewUrl: foreignPreview });
  const foreignRequests: string[] = [];
  await page.route('https://preview.invalid/**', (route, request) => { foreignRequests.push(request.url()); return route.abort(); });
  const { editor, choice } = await openLogoEditor(page);
  await expect(choice.getByRole('img')).toHaveCount(0);
  await expect(choice).toContainText('Preview unavailable');
  await expect(editor.locator(`[src*="${previewMarker}"], [href*="${previewMarker}"]`)).toHaveCount(0);
  await expect(editor).not.toContainText(previewMarker);
  await choice.click();
  await expect(editor.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'success');
  const writes = api.writes().filter((request) => request.path === imagePath);
  expect(writes).toHaveLength(1);
  expect(JSON.stringify(writes[0].body)).not.toContain(previewMarker);
  expect(JSON.stringify(writes[0].body)).not.toContain('https://');
  expect(foreignRequests).toEqual([]);
});

for (const malformedArtwork of ['url', 'revision'] as const) {
  test(`a logo acknowledgement with an invalid artwork ${malformedArtwork} requires review without replay`, async ({ page, api }) => {
    installLogoFixture(api, { malformedArtwork });
    const foreignRequests: string[] = [];
    await page.route('https://preview.invalid/**', (route, request) => { foreignRequests.push(request.url()); return route.abort(); });
    const { editor, choice } = await openLogoEditor(page);
    await choice.click();
    await expect(editor.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'unknown');
    await expect(editor.locator('.artwork-editor-body')).toHaveAttribute('aria-busy', 'false');
    await expect(choice).toBeDisabled();
    await expect(choice).toHaveAttribute('aria-pressed', 'false');
    await expect(editor.getByRole('region', { name: copy.en.current, exact: true }).getByRole('img')).toHaveAttribute('src', /tag=old-logo$/);
    await expect(editor.locator(`[src*="${previewMarker}"], [href*="${previewMarker}"]`)).toHaveCount(0);
    await expect(editor).not.toContainText(previewMarker);
    await page.keyboard.press('Control+s');
    expect(api.writes().filter((request) => request.path === imagePath)).toHaveLength(1);
    expect(foreignRequests).toEqual([]);
  });
}
