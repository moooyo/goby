import type { LibraryUpdate, EditableLibrary } from '../src/libraryManagementApi';
import type { Library, LibraryInput } from '../src/api';
import { csrf, expect, json, library, test } from './dashboard-fixture';
import type { DashboardAPI } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

// Explicit API fixtures verify native library policy and mutation safety.
// Provider HTTP, image publication, and task scheduling have separate Go tests.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'light' } });

function installLibrary(api: DashboardAPI, collectionType = 'movies') {
  let saved: EditableLibrary = { ...library, CollectionType: collectionType, Revision: '9007199254740993',
    LibraryOptions: { EnableLocalMetadata: false, EnableLocalImages: false, EnableEmbeddedArtwork: false },
    RegisteredPaths: [{ Id: 'image-policy-root', Path: library.Paths[0], ItemCount: 2 }],
  };
  const path = `/admin/v1/libraries/${saved.Id}`;
  api.handlers.set('GET /admin/v1/libraries', (route) => json(route, { Items: [saved], TotalRecordCount: 1 }));
  api.handlers.set(`GET ${path}`, (route) => json(route, { Library: saved }));
  const update = (input: LibraryUpdate) => {
    expect(input.Revision).toBe(saved.Revision);
    saved = { ...saved, Name: input.Name, Paths: input.Paths, LibraryOptions: input.LibraryOptions,
      Revision: (BigInt(saved.Revision) + 1n).toString() };
  };
  api.handlers.set(`PATCH ${path}`, async (route, request) => {
    update(request.postDataJSON() as LibraryUpdate);
    await json(route, { Library: saved });
  });
  return { path, update, saved: () => saved };
}

test('new library creation clears unsupported image policy and submits one exact opt-in payload', async ({ page, api }, testInfo) => {
  let created: Library | undefined;
  const createdId = 'd'.repeat(32);
  api.handlers.set('GET /admin/v1/libraries', (route) => json(route, { Items: created ? [created] : [library], TotalRecordCount: 1 }));
  api.handlers.set('POST /admin/v1/libraries', async (route, request) => {
    const input = request.postDataJSON() as LibraryInput;
    created = { ...library, Id: createdId, Name: input.Name, CollectionType: input.CollectionType,
      Paths: input.Paths, LibraryOptions: input.LibraryOptions, LastScanAt: null };
    await json(route, { Library: created }, 201);
  });
  api.handlers.set(`GET /admin/v1/libraries/${createdId}/items`, (route) => json(route, {
    Library: created, Items: [], TotalRecordCount: 0, StartIndex: 0, Limit: 1,
  }));
  await page.goto('/admin/media/libraries');
  await page.getByRole('button', { name: 'Create library', exact: true }).first().click();
  const sheet = page.getByRole('dialog', { name: 'Create library', exact: true });
  const policy = sheet.getByRole('checkbox', { name: 'Automatically download missing images', exact: true });
  await expect(policy).not.toBeChecked();
  await policy.check();
  await sheet.getByRole('combobox', { name: 'Content type', exact: true }).selectOption('music');
  await expect(policy).toHaveCount(0);
  await sheet.getByRole('combobox', { name: 'Content type', exact: true }).selectOption('movies');
  await expect(policy).not.toBeChecked();
  await policy.check();
  await sheet.getByRole('textbox', { name: 'Library name', exact: true }).fill('Artwork library');
  await sheet.getByRole('textbox', { name: 'Media directories', exact: true }).fill('/media/movies');
  await sheet.getByRole('checkbox', { name: /Scan after creating/ }).uncheck();
  await policy.scrollIntoViewIfNeeded();
  await evidence(page, testInfo, 'automatic-images-create-library');
  expect(api.writes()).toHaveLength(0);
  await sheet.getByRole('button', { name: 'Create library', exact: true }).click();
  await expect(sheet).not.toBeVisible();
  expect(api.writes()).toEqual([{ method: 'POST', path: '/admin/v1/libraries', csrf,
    body: { Name: 'Artwork library', CollectionType: 'movies', Paths: ['/media/movies'], Scan: false,
      LibraryOptions: { EnableLocalMetadata: true, EnableLocalImages: true, EnableEmbeddedArtwork: true, EnableOnlineImages: true,
        EnableIntroDetection: false, EnableCreditsDetection: false, EnablePreviewGeneration: false, EnableBackgroundPreviewGeneration: false,
        EnableAudioWaveformGeneration: false, EnableSubtitleTimelineGeneration: false },
    },
  }]);
  const row = page.getByRole('row').filter({ has: page.getByRole('button', { name: 'Artwork library', exact: true }) });
  await expect(row.getByText('Automatic images', { exact: true })).toBeVisible();
  await expect(row.getByText('No analysis enabled', { exact: true })).toBeVisible();
  const notice = page.locator('.goby-snackbar');
  await expect(notice).toContainText('Automatic image downloads are enabled.');
  await expect(notice).toContainText('View image download progress in Tasks.');
  await notice.getByRole('button', { name: 'View tasks', exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/system\/tasks$/);
  await expect(page.getByRole('region', { name: 'Scheduled tasks', exact: true })).toBeVisible();
});

test('missing images require an explicit independent opt in and preserve the exact library revision', async ({ page, api }, testInfo) => {
  const fixture = installLibrary(api);
  await page.goto('/admin/media/libraries');
  await page.getByRole('button', { name: `More actions for ${library.Name}`, exact: true }).click();
  await page.getByRole('menuitem', { name: 'Edit library', exact: true }).click();
  let sheet = page.getByRole('dialog', { name: 'Edit library', exact: true });
  const policy = sheet.getByRole('checkbox', { name: 'Automatically download missing images', exact: true });
  await expect(policy).not.toBeChecked();
  await expect(policy).toBeEnabled();
  await expect(sheet.getByRole('checkbox', { name: 'Import images from media directories', exact: true })).not.toBeChecked();
  await policy.check();
  await evidence(page, testInfo, 'automatic-images-library-draft');
  expect(api.writes()).toHaveLength(0);
  await sheet.getByRole('button', { name: 'Save library', exact: true }).click();
  await expect(sheet).not.toBeVisible();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0]).toMatchObject({ method: 'PATCH', path: fixture.path, csrf,
    body: { Revision: '9007199254740993', Scan: false, LibraryOptions: { EnableOnlineImages: true, EnableLocalImages: false, EnableEmbeddedArtwork: false } },
  });
  await page.getByRole('button', { name: `More actions for ${library.Name}`, exact: true }).click();
  await page.getByRole('menuitem', { name: 'Edit library', exact: true }).click();
  sheet = page.getByRole('dialog', { name: 'Edit library', exact: true });
  await expect(sheet.getByRole('checkbox', { name: 'Automatically download missing images', exact: true })).toBeChecked();
  await sheet.getByRole('checkbox', { name: 'Automatically download missing images', exact: true }).uncheck();
  await sheet.getByRole('button', { name: 'Save library', exact: true }).click();
  await expect(sheet).not.toBeVisible();
  expect(api.writes()).toHaveLength(2);
  expect(api.writes()[1]).toMatchObject({ body: { Revision: '9007199254740994', LibraryOptions: { EnableOnlineImages: false } } });
  await noOverflow(page);
});

test('an unconfirmed policy save is read back before another mutation can be made', async ({ page, api }) => {
  const fixture = installLibrary(api);
  api.handlers.set(`PATCH ${fixture.path}`, async (route, request) => {
    fixture.update(request.postDataJSON() as LibraryUpdate);
    await route.abort('connectionreset');
  });
  await page.goto('/admin/media/libraries');
  await page.getByRole('button', { name: `More actions for ${library.Name}`, exact: true }).click();
  await page.getByRole('menuitem', { name: 'Edit library', exact: true }).click();
  const sheet = page.getByRole('dialog', { name: 'Edit library', exact: true });
  const policy = sheet.getByRole('checkbox', { name: 'Automatically download missing images', exact: true });
  await policy.check();
  await sheet.getByRole('button', { name: 'Save library', exact: true }).click();
  await expect(sheet.getByText('The library may have changed or the save response could not be confirmed. Reload and review the saved library before trying again.', { exact: true })).toBeVisible();
  await expect(policy).toBeDisabled();
  await expect(sheet.getByRole('button', { name: 'Save library', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
  page.once('dialog', (dialog) => void dialog.accept());
  await sheet.getByRole('button', { name: 'Reload', exact: true }).last().click();
  await expect(policy).toBeEnabled();
  await expect(policy).toBeChecked();
  expect(api.writes()).toHaveLength(1);
  expect(api.reads(fixture.path).length).toBeGreaterThanOrEqual(2);
});

test('music library settings do not expose an unsupported automatic image policy', async ({ page, api }) => {
  installLibrary(api, 'music');
  await page.goto('/admin/media/libraries');
  await page.getByRole('button', { name: `More actions for ${library.Name}`, exact: true }).click();
  await page.getByRole('menuitem', { name: 'Edit library', exact: true }).click();
  const sheet = page.getByRole('dialog', { name: 'Edit library', exact: true });
  await expect(sheet.getByRole('textbox', { name: 'Library name', exact: true })).toBeVisible();
  await expect(sheet.getByRole('checkbox', { name: 'Automatically download missing images', exact: true })).toHaveCount(0);
  expect(api.writes()).toHaveLength(0);
});

test.describe('automatic image policy in Chinese on a phone', () => {
  test.use({ preferences: { language: 'zh', theme: 'dark' }, viewport: { width: 390, height: 844 } });
  test('the opt-in explanation remains readable and scrollable', async ({ page, api }, testInfo) => {
    installLibrary(api);
    await page.goto('/admin/media/libraries');
    await page.getByRole('button', { name: `More actions for ${library.Name}`, exact: true }).click();
    await page.getByRole('menuitem', { name: '媒体库设置', exact: true }).click();
    const sheet = page.getByRole('dialog', { name: '媒体库设置', exact: true });
    const policy = sheet.getByRole('checkbox', { name: '自动下载缺失图片', exact: true });
    await policy.scrollIntoViewIfNeeded();
    await expect(policy).not.toBeChecked();
    await expect(sheet.getByText('根据已有 TMDB 匹配补齐缺失的海报、背景、Logo 和剧集缩略图，保留已有图片及手动删除的结果。', { exact: true })).toBeVisible();
    await noOverflow(page);
    await evidence(page, testInfo, 'automatic-images-library-phone-zh-dark');
    expect(api.writes()).toHaveLength(0);
  });
});
