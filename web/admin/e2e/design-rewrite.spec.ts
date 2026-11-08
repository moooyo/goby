import type { Page } from '@playwright/test';
import type { ArtworkCollection } from '../src/artworkApi';
import { administrator, analysisItem, csrf, expect, json, library, movieMetadata, stamp, test } from './dashboard-fixture';
import { destinations, evidence, navigation, noOverflow, ready } from './dashboard-review';
import { initialUserRevision, installUserDesignFixture, viewerId } from './user-design-fixture';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'zh', theme: 'dark' } });

// Every canonical route is captured in both viewport classes. English/light
// counterparts and mutation safety cases live in dashboard-v2.spec.ts.
const readiness = [
  /Goby living room/, /Feature films/, /Alex Morgan/, /Weekend favorites/,
  /Opening episode|分析缓存|Analysis cache/, /Family viewer/, /Living room TV/, /Desktop browser/,
  /Home automation/, /Scan media libraries|扫描媒体库/, /Administrator signed in/,
  /No notification targets yet|尚无通知目标/, /Restore backup|恢复备份/,
  /Server identity|服务器标识|服务器身份/, /Maximum bitrate|最大码率/,
  /Hardware acceleration|硬件加速/, /The Movie Database/, /The Movie Database/, /The feature film/,
];

for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
  test.describe(`${viewport.width}px Chinese dark design`, () => {
    test.use({ viewport });
    for (const [index, destination] of destinations.entries()) test(`design ${destination.path}`, async ({ page, api }, testInfo) => {
      await page.goto(destination.path);
      await ready(page, destination, false);
      await expect(page.getByRole('main')).toContainText(readiness[index]);
      await noOverflow(page);
      await expect(page.locator('style[data-emotion]'), 'The rewritten dashboard must not inject Emotion styles.').toHaveCount(0);
      await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
      const heading = page.getByRole('main').getByRole('heading', { level: 1 });
      await expect(heading).toHaveCSS('font-size', viewport.width < 640 ? '24px' : '28px');
      expect(await heading.evaluate((element) => getComputedStyle(element).fontFamily)).toContain('Noto Serif SC');
      const name = destination.path === '/admin/' ? 'overview' : destination.path.includes('/items') ? 'media-items' : destination.path.slice('/admin/'.length).replaceAll('/', '-');
      await evidence(page, testInfo, `design-${name}-${viewport.width}-zh-dark`);
      const main = page.getByRole('main');
      if (await main.evaluate((element) => element.scrollHeight > element.clientHeight + 80)) {
        for (const [position, ratio] of [['middle', 0.5], ['bottom', 1]] as const) {
          await main.evaluate((element, value) => { element.scrollTop = (element.scrollHeight - element.clientHeight) * value; }, ratio);
          await noOverflow(page);
          await evidence(page, testInfo, `design-${name}-${viewport.width}-zh-dark-${position}`);
        }
      }
      expect(api.writes()).toEqual([]);
    });
  });
}

test('theme and language switches persist independently across navigation and reload', async ({ page, api }, testInfo) => {
  await page.goto('/admin/');
  await ready(page, destinations[0], false);
  await expect(page.getByRole('main').getByRole('heading', { level: 1 })).toHaveText('概览');
  await page.getByRole('button', { name: /^(Toggle theme|切换深浅|切换主题|切换深浅色)$/ }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
  await evidence(page, testInfo, 'preferences-overview-zh-light');
  await page.getByRole('button', { name: new RegExp(`^(Account|账户|账号): ${administrator.Name}$`) }).click();
  await page.getByRole('menuitemradio', { name: 'English', exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.getByRole('main').getByRole('heading', { level: 1 })).toHaveText('Overview');
  await page.reload();
  await ready(page, destinations[0]);
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await page.getByRole('button', { name: 'Toggle theme', exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await evidence(page, testInfo, 'preferences-overview-en-dark');
  expect(await page.evaluate(() => ({ language: localStorage.getItem('goby.admin.language'), theme: localStorage.getItem('goby.admin.theme') }))).toEqual({ language: 'en', theme: 'dark' });
  expect(api.writes()).toEqual([]);
});

test.describe('English keyboard workflows', () => {
  test.use({ preferences: { language: 'en', theme: 'dark' } });

  test('command search opens by shortcut, restores focus on Escape and navigates by keyboard', async ({ page, api }, testInfo) => {
    await page.goto('/admin/');
    await ready(page, destinations[0]);
    const opener = page.getByRole('button', { name: 'Search or jump to', exact: true });
    await opener.focus();
    await page.keyboard.press('Control+k');
    const command = page.getByRole('dialog', { name: 'Search or jump to', exact: true });
    await expect(command).toBeVisible();
    const input = command.getByRole('combobox', { name: 'Search pages and commands', exact: true });
    await expect(input).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(command).not.toBeVisible();
    await expect(opener).toBeFocused();
    await page.keyboard.press('Control+k');
    await expect(command).toBeVisible();
    const remainedOpen = await command.evaluate(async (element) => {
      const dialog = element as HTMLDialogElement;
      const delivered = new Promise<boolean>((resolve) => {
        dialog.addEventListener('close', () => queueMicrotask(() => resolve(dialog.open)), { once: true });
      });
      // Reopening before delivery leaves an older native close event queued.
      dialog.close();
      dialog.showModal();
      return await delivered;
    });
    expect(remainedOpen, 'An older queued close must not dismiss the currently reopened palette.').toBe(true);
    await expect(command).toBeVisible();
    await input.fill('Devices');
    await evidence(page, testInfo, 'command-search-devices');
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('Enter');
    await expect(page).toHaveURL(/\/admin\/access\/devices$/);
    await ready(page, destinations[6]);
    expect(api.writes()).toEqual([]);
  });

  test('command keyboard navigation scrolls lower page options into view before activation', async ({ page, api }, testInfo) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/admin/');
    await ready(page, destinations[0]);
    await page.keyboard.press('Control+k');
    const command = page.getByRole('dialog', { name: 'Search or jump to', exact: true });
    const input = command.getByRole('combobox', { name: 'Search pages and commands', exact: true });
    const list = command.getByRole('listbox', { name: 'Pages and commands', exact: true });
    const active = list.getByRole('option', { selected: true });
    await expect(input).toHaveValue('');
    await expect(input).toBeFocused();
    for (let index = 1; index <= 14; index += 1) {
      await input.press('ArrowDown');
      await expect(active).toHaveAttribute('id', `command-${index}`);
      await expect.poll(() => active.evaluate((element) => {
        const viewport = element.closest('[role="listbox"]')!.getBoundingClientRect();
        const option = element.getBoundingClientRect();
        return option.top >= viewport.top - 1 && option.bottom <= viewport.bottom + 1;
      }), 'The active command must stay inside the result viewport.').toBe(true);
    }
    await expect(active).toContainText('Metadata & subtitles');
    expect(await list.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
    await evidence(page, testInfo, 'command-palette-scrolled-selection');
    await input.press('Enter');
    await ready(page, destinations[16]);
    await expect(page).toHaveURL(/\/admin\/settings\/metadata$/);
    const beforeComposition = page.url();
    await page.keyboard.press('Control+k');
    await input.fill('Devices');
    await expect(active).toContainText('Devices');
    const composing = await input.evaluate((element) => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', isComposing: true, bubbles: true, cancelable: true });
      element.dispatchEvent(event);
      return event.isComposing;
    });
    expect(composing).toBe(true);
    await expect(command).toBeVisible();
    await expect(page).toHaveURL(beforeComposition);
    const legacyComposition = await input.evaluate((element) => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', keyCode: 229, bubbles: true, cancelable: true });
      element.dispatchEvent(event);
      return event.keyCode;
    });
    expect(legacyComposition).toBe(229);
    await expect(command).toBeVisible();
    await expect(page).toHaveURL(beforeComposition);
    await input.press('Enter');
    await ready(page, destinations[6]);
    await expect(page).toHaveURL(/\/admin\/access\/devices$/);
    expect(api.writes()).toEqual([]);
  });

  for (const width of [1440, 390]) test(`${width}px library sheet traps focus and returns it to its trigger`, async ({ page, api }, testInfo) => {
    await page.setViewportSize({ width, height: width < 640 ? 844 : 900 });
    await page.goto('/admin/media/libraries');
    await ready(page, destinations[1]);
    const trigger = page.getByRole('button', { name: 'Create library', exact: true }).first();
    await trigger.click();
    const sheet = page.getByRole('dialog', { name: 'Create library', exact: true });
    await expect(sheet).toBeVisible();
    await expect(sheet.getByRole('textbox', { name: 'Library name', exact: true })).toBeVisible();
    await expectFocusInside(page, sheet);
    const bounds = await sheet.boundingBox();
    expect(bounds).not.toBeNull();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width + 1);
    await evidence(page, testInfo, `create-library-sheet-${width}`);
    await page.keyboard.press('Escape');
    await expect(sheet).not.toBeVisible();
    await expect(trigger).toBeFocused();
    expect(api.writes()).toEqual([]);
  });

  test('library detail tabs expose existing data and read images only when requested', async ({ page, api }, testInfo) => {
    await page.goto(`/admin/media/libraries/${library.Id}/items`);
    await ready(page, destinations[18]);
    await expect(page.locator('.header-breadcrumb')).toContainText('Feature films');
    expect(api.reads('/admin/v1/items/film-one/images')).toHaveLength(0);
    for (const name of ['Items', 'Images', 'Media directories', 'Features and analysis', 'Scan history']) await expect(page.getByRole('tab', { name, exact: true })).toBeVisible();
    const images = page.getByRole('tab', { name: 'Images', exact: true });
    await images.focus();
    await page.keyboard.press('Enter');
    await expect(images).toHaveAttribute('aria-selected', 'true');
    await expect.poll(() => api.reads('/admin/v1/items/film-one/images').length).toBeGreaterThan(0);
    const imagePanel = page.getByRole('region', { name: 'Library images', exact: true });
    await expect(imagePanel.getByText('Loading…', { exact: true })).toHaveCount(0);
    await expect(imagePanel.getByText('No image', { exact: true })).toBeVisible();
    await noOverflow(page);
    await evidence(page, testInfo, 'library-detail-images');
    await page.getByRole('tab', { name: 'Scan history', exact: true }).click();
    await expect(page.getByRole('tab', { name: 'Scan history', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect.poll(() => api.reads('/admin/v1/jobs').length).toBeGreaterThan(0);
    await page.getByRole('tab', { name: 'Items', exact: true }).click();
    await expect(page.getByRole('main')).toContainText('The feature film');
    expect(api.writes()).toEqual([]);
  });

  for (const width of [1440, 390]) test(`${width}px automatic analysis results retain recorded intervals and preview outputs`, async ({ page, api }, testInfo) => {
    await page.setViewportSize({ width, height: width < 640 ? 844 : 900 });
    await page.goto('/admin/media/analysis');
    await ready(page, destinations[4]);
    await page.getByRole('button', { name: `View analysis for ${analysisItem.Name}`, exact: true }).click();
    const result = page.getByRole('dialog', { name: analysisItem.Name, exact: true });
    await expect(result).toContainText('Intro available');
    await expect(result).toContainText('0:10.00–0:40.00');
    const outputs = result.getByRole(width < 840 ? 'list' : 'table', { name: 'Preview outputs', exact: true });
    await expect(outputs).toContainText('320 × 180');
    await expect(outputs).toContainText('12');
    await expect(outputs).toContainText('32.0 KiB');
    await expect(result.getByRole('button', { name: /Accept|Reject|Reset/ })).toHaveCount(0);
    await evidence(page, testInfo, `automatic-analysis-result-${width}`);
    await result.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(result).not.toBeVisible();
    expect(api.writes()).toEqual([]);
  });

  test('metadata editing preserves its draft across tabs and guards image navigation and discarding', async ({ page, api }, testInfo) => {
    const searchPath = '/admin/v1/items/film-one/providers/images';
    api.handlers.set(`POST ${searchPath}`, (route) => json(route, { Items: [] }));
    await page.goto(`/admin/media/libraries/${library.Id}/items`);
    await ready(page, destinations[18]);
    await page.getByRole('button', { name: 'Edit metadata for The feature film', exact: true }).first().click();
    const sheet = page.getByRole('dialog', { name: /^Edit metadata/ });
    await expect(sheet.locator('#metadata-Name-input')).toHaveValue('The feature film');
    await evidence(page, testInfo, 'metadata-sheet-basic-information');
    for (const [tab, path] of [['Images', '/admin/v1/items/film-one/images'], ['Cast & crew', ''], ['Chapters & credits', '/admin/v1/items/film-one/media-info'], ['File', '/admin/v1/items/film-one/media-info']] as const) {
      await sheet.getByRole('tab', { name: tab, exact: true }).click();
      await expect(sheet.getByRole('tab', { name: tab, exact: true })).toHaveAttribute('aria-selected', 'true');
      if (path) await expect.poll(() => api.reads(path).length).toBeGreaterThan(0);
      if (tab === 'Images') {
        await expect(sheet.getByRole('region', { name: 'Current image', exact: true }).getByText('No image saved', { exact: true }).first()).toBeVisible();
        await expect(sheet.getByRole('region', { name: 'Online image candidates', exact: true })).toHaveAttribute('aria-busy', 'false');
        await expect(sheet.getByRole('button', { name: 'Save changes', exact: true })).toHaveCount(0);
        await expect(sheet.getByRole('button', { name: 'Discard', exact: true })).toHaveCount(0);
      }
      if (tab === 'Cast & crew') await expect(sheet.getByRole('tabpanel', { name: 'Cast & crew', exact: true }).getByRole('textbox', { name: 'Name', exact: true }).first()).toHaveValue('Alex Morgan');
      if (tab === 'File') await expect(sheet).toContainText('mkv');
      await expect(sheet).not.toContainText(/response is incomplete|No dashboard fixture/);
      await evidence(page, testInfo, `metadata-sheet-${tab.toLowerCase().replaceAll(/[^a-z]+/g, '-')}`);
    }
    await sheet.getByRole('tab', { name: 'Basic information', exact: true }).click();
    await sheet.locator('#metadata-Name-input').fill('Feature film revised');
    const titleLock = sheet.getByRole('checkbox', { name: 'Lock Title', exact: true });
    await expect(titleLock).toBeChecked();
    await titleLock.uncheck();
    await sheet.getByRole('tab', { name: 'Cast & crew', exact: true }).click();
    await sheet.getByRole('tab', { name: 'Basic information', exact: true }).click();
    await expect(sheet.locator('#metadata-Name-input')).toHaveValue('Feature film revised');
    await expect(titleLock).not.toBeChecked();
    const beforeImages = api.requests.length;
    await sheet.getByRole('tab', { name: 'Images', exact: true }).click();
    const imagesPrompt = page.getByRole('alertdialog', { name: 'Edit images with unsaved metadata?', exact: true });
    await expect(imagesPrompt).toBeVisible();
    for (const name of ['Keep editing', 'Discard metadata and edit images', 'Save metadata and edit images']) {
      await expect(imagesPrompt.getByRole('button', { name, exact: true })).toBeVisible();
    }
    await imagesPrompt.getByRole('button', { name: 'Keep editing', exact: true }).click();
    await expect(imagesPrompt).not.toBeVisible();
    await expect(sheet.getByRole('tab', { name: 'Basic information', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(sheet.locator('#metadata-Name-input')).toHaveValue('Feature film revised');
    await expect(titleLock).not.toBeChecked();
    expect(api.requests.slice(beforeImages).filter(({ path }) => path === searchPath || path === '/admin/v1/items/film-one/metadata')).toEqual([]);
    await page.keyboard.press('Escape');
    const confirm = page.getByRole('alertdialog', { name: 'Discard unsaved metadata changes?', exact: true });
    await expect(confirm).toBeVisible();
    await confirm.getByRole('button', { name: 'Keep editing', exact: true }).click();
    await expect(sheet.locator('#metadata-Name-input')).toHaveValue('Feature film revised');
    await page.keyboard.press('Escape');
    await confirm.getByRole('button', { name: 'Discard changes', exact: true }).click();
    await expect(sheet).not.toBeVisible();
    expect(api.writes()).toEqual([{ method: 'POST', path: searchPath, body: { Provider: 'tmdb', Id: '12345', Language: 'en' }, csrf }]);
  });

  test('the image editor applies inline online candidates with both revisions and retains its saved context', async ({ page, api }, testInfo) => {
    const item = '/admin/v1/items/film-one';
    let artwork: ArtworkCollection = { Revision: '1', Items: [] };
    const poster = '<svg xmlns="http://www.w3.org/2000/svg" width="1000" height="1500" viewBox="0 0 1000 1500"><rect width="1000" height="1500" fill="#302b23"/><text x="500" y="750" text-anchor="middle" fill="#e3b46a" font-family="sans-serif" font-size="64">Synthetic poster</text></svg>';
    api.handlers.set(`GET ${item}/images`, (route) => json(route, artwork));
    api.handlers.set(`GET ${item}/images/Primary/0/content`, (route) => route.fulfill({ contentType: 'image/svg+xml', body: poster }));
    api.handlers.set(`GET ${item}/providers/image-preview`, (route) => route.fulfill({ contentType: 'image/svg+xml', body: poster }));
    api.handlers.set(`GET ${item}/providers/provenance`, (route) => json(route, { Items: [] }));
    api.handlers.set(`POST ${item}/providers/images`, (route) => json(route, { Items: [{ Provider: 'tmdb', Id: '12345', Type: 'Movie', Language: 'en', ImageId: 'synthetic-poster', ImageType: 'Primary', Width: 1000, Height: 1500, PreviewUrl: `${item}/providers/image-preview?candidate=synthetic-poster` }] }));
    api.handlers.set(`POST ${item}/providers/image`, (route, request) => {
      const input = request.postDataJSON() as { Provider: string; Id: string; ImageId: string; ImageType: string; ImageIndex: number; Revision: string; ArtworkRevision: string };
      expect(input.Revision).toBe(movieMetadata.Revision);
      expect(input.ArtworkRevision).toBe(artwork.Revision);
      const saved = { ImageType: 'Primary' as const, ImageIndex: 0, Tag: 'synthetic-poster', Width: 1000, Height: 1500, Size: '2048', PreviewUrl: `${item}/images/Primary/0/content?tag=synthetic-poster`, Source: 'provider' };
      artwork = { Revision: '2', Items: [saved] };
      return json(route, { Provider: input.Provider, ProviderId: input.Id, ImageId: input.ImageId, ImageType: input.ImageType, ImageIndex: input.ImageIndex, Width: saved.Width, Height: saved.Height, Tag: saved.Tag, Artwork: artwork });
    });
    await page.goto(`/admin/media/libraries/${library.Id}/items`);
    await ready(page, destinations[18]);
    await page.getByRole('button', { name: 'Edit metadata for The feature film', exact: true }).first().click();
    const editor = page.getByRole('dialog', { name: /^Edit metadata/ });
    await editor.getByRole('tab', { name: 'Images', exact: true }).click();
    await expect(editor.getByRole('region', { name: 'Current image', exact: true }).getByText('No image saved', { exact: true }).first()).toBeVisible();
    const candidates = editor.getByRole('region', { name: 'Online image candidates', exact: true });
    const candidate = candidates.locator('[data-candidate-id="synthetic-poster"] > button.artwork-candidate-choice');
    await expect(candidate).toBeVisible();
    await expect(candidate).toHaveAttribute('aria-pressed', 'false');
    await expect(page.getByRole('dialog', { name: /^Online sources/ })).toHaveCount(0);
    const beforeApply = api.reads(`${item}/images`).length;
    await candidate.click();
    await expect(editor.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'success');
    await expect(editor.locator('.artwork-editor-footer')).toContainText('Image applied.');
    await expect(candidate).toHaveAttribute('aria-pressed', 'true');
    expect(api.reads(`${item}/images`)).toHaveLength(beforeApply);
    await expect(editor.getByRole('tab', { name: 'Images', exact: true })).toHaveAttribute('aria-selected', 'true');
    const savedPoster = editor.getByRole('region', { name: 'Current image', exact: true }).getByRole('img', { name: 'Poster 1 · The feature film', exact: true });
    await expect(savedPoster).toBeVisible();
    await expect.poll(() => savedPoster.evaluate((element) => (element as HTMLImageElement).complete && (element as HTMLImageElement).naturalWidth > 0)).toBe(true);
    await expect(editor.getByText('No image saved', { exact: true })).toHaveCount(0);
    expect(api.writes().map(({ method, path }) => `${method} ${path}`)).toEqual([`POST ${item}/providers/images`, `POST ${item}/providers/image`]);
    expect(api.writes()[1].body).toEqual({ Provider: 'tmdb', Id: '12345', Language: 'en', ImageId: 'synthetic-poster', ImageType: 'Primary', ImageIndex: 0, Revision: movieMetadata.Revision, ArtworkRevision: '1' });
    await expect(editor.getByRole('button', { name: 'Save changes', exact: true })).toHaveCount(0);
    await expect(editor.getByRole('button', { name: 'Discard', exact: true })).toHaveCount(0);
    await page.keyboard.press('Control+s');
    expect(api.writes()).toHaveLength(2);
    await editor.getByRole('tab', { name: 'Basic information', exact: true }).click();
    await expect(editor.locator('#metadata-Name-input')).toHaveValue('The feature film');
    await editor.getByRole('tab', { name: 'Images', exact: true }).click();
    await expect(candidate).toHaveAttribute('aria-pressed', 'true');
    await expect(savedPoster).toBeVisible();
    await expect(editor.locator('.artwork-editor-footer')).toHaveAttribute('data-state', 'success');
    expect(api.writes()).toHaveLength(2);
    await evidence(page, testInfo, 'online-images-inline-applied-context');
  });

  test('the 540px user sheet retains a profile draft across four tabs and saves its exact revision', async ({ page, api }, testInfo) => {
    const fixture = installUserDesignFixture(api);
    await page.goto('/admin/access/users');
    await ready(page, destinations[5]);
    await page.getByRole('button', { name: 'Manage Family viewer', exact: true }).click();
    const sheet = page.getByRole('dialog', { name: /Family viewer/ });
    await expect(sheet.getByRole('region', { name: 'Library access', exact: true })).toBeVisible();
    await expect(sheet.getByRole('button', { name: /^(Close dialog|Close user editor)$/ })).toHaveCount(1);
    await expect(sheet.getByRole('button', { name: 'Close dialog', exact: true })).toBeVisible();
    await expect.poll(async () => (await sheet.boundingBox())?.width).toBe(540);
    for (const name of ['Permissions', 'Profile', 'Devices', 'Password']) await expect(sheet.getByRole('tab', { name, exact: true })).toBeVisible();
    await expect(sheet.getByRole('tab', { name: 'Playback preferences', exact: true })).toHaveCount(0);
    await evidence(page, testInfo, 'user-sheet-permissions-540');
    await sheet.getByRole('tab', { name: 'Profile', exact: true }).click();
    const username = sheet.getByRole('textbox', { name: 'Username', exact: true });
    await expect(username).toHaveValue('Family viewer');
    await username.fill('Family viewer revised');
    await sheet.getByRole('tab', { name: 'Devices', exact: true }).click();
    await expect(sheet.getByRole('list', { name: 'User devices', exact: true })).toContainText('Family tablet');
    await evidence(page, testInfo, 'user-sheet-devices-540');
    await sheet.getByRole('tab', { name: 'Password', exact: true }).click();
    await expect(sheet.getByRole('button', { name: 'Reset password', exact: true })).toBeDisabled();
    await expect(sheet.getByRole('button', { name: 'Delete user', exact: true })).toBeDisabled();
    await evidence(page, testInfo, 'user-sheet-password-540');
    await page.keyboard.press('Escape');
    const confirmation = page.getByRole('dialog', { name: 'Discard unsaved changes?', exact: true });
    await expect(confirmation).toBeVisible();
    await confirmation.getByRole('button', { name: 'Keep editing', exact: true }).click();
    await sheet.getByRole('tab', { name: 'Profile', exact: true }).click();
    await expect(username).toHaveValue('Family viewer revised');
    await evidence(page, testInfo, 'user-sheet-profile-draft-540');
    expect(api.writes()).toEqual([]);
    await sheet.getByRole('button', { name: 'Save changes', exact: true }).click();
    await expect(sheet.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
    expect(api.writes()).toHaveLength(1);
    expect(api.writes()[0]).toMatchObject({ method: 'PUT', path: `/admin/v1/users/${viewerId}`, body: {
      Revision: initialUserRevision, Name: 'Family viewer revised', IsAdministrator: false, IsDisabled: false, Policy: fixture.initialPolicy,
    } });
    expect(fixture.user.Revision).toBe('9007199254740994');
    expect(fixture.user.Name).toBe('Family viewer revised');
    expect(api.requests.filter(({ path }) => path === `/admin/v1/users/${viewerId}/preferences`)).toEqual([]);
    await sheet.getByRole('button', { name: 'Close dialog', exact: true }).click();
    await expect(sheet).not.toBeVisible();
    await expect(page.getByRole('main')).toContainText('Family viewer revised');
  });

  test('the user editor omits playback preferences and never requests their endpoint', async ({ page, api }, testInfo) => {
    installUserDesignFixture(api);
    await page.goto('/admin/access/users');
    await ready(page, destinations[5]);
    await page.getByRole('button', { name: 'Manage Family viewer', exact: true }).click();
    const sheet = page.getByRole('dialog', { name: /Family viewer/ });
    await expect(sheet.getByRole('tab')).toHaveText(['Permissions', 'Profile', 'Devices', 'Password']);
    await expect(sheet.getByRole('tab', { name: 'Playback preferences', exact: true })).toHaveCount(0);
    await expect(sheet.getByRole('tabpanel', { name: 'Playback preferences', exact: true })).toHaveCount(0);
    await expect(sheet.getByRole('textbox', { name: 'Preferred audio language', exact: true })).toHaveCount(0);
    expect(api.requests.filter(({ path }) => path === `/admin/v1/users/${viewerId}/preferences`)).toEqual([]);
    expect(api.writes()).toEqual([]);
    await evidence(page, testInfo, 'user-sheet-account-tabs-without-preferences');
    await sheet.getByRole('button', { name: 'Close dialog', exact: true }).click();
    await expect(sheet).not.toBeVisible();
  });

  test('login preserves password-manager fields and never stores credentials', async ({ page, api }, testInfo) => {
    api.handlers.set('GET /admin/v1/session', (route) => json(route, { Error: { Code: 'unauthorized', Message: 'Sign in to continue.' } }, 401));
    api.handlers.set('POST /admin/v1/session', (route) => json(route, { Error: { Code: 'invalid_credentials', Message: 'The username or password is incorrect.' } }, 401));
    await page.goto('/admin/');
    const username = page.getByRole('textbox', { name: 'Username', exact: true });
    const password = page.getByLabel('Password', { exact: true });
    await expect(username).toHaveAttribute('autocomplete', 'username');
    await expect(password).toHaveAttribute('autocomplete', 'current-password');
    await evidence(page, testInfo, 'login-en-dark');
    await username.fill('Synthetic administrator');
    await password.fill('synthetic-only-password');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('The username or password is incorrect.');
    expect(api.writes()).toHaveLength(1);
    const storage = await page.evaluate(() => JSON.stringify([Object.entries(localStorage), Object.entries(sessionStorage)]));
    expect(storage).not.toContain('synthetic-only-password');
    expect(storage).not.toContain('Synthetic administrator');
  });

  test('first-run setup completes its authenticated configuration steps and returns to a cleared login form', async ({ page, api }, testInfo) => {
    let initialized = false;
    api.handlers.set('GET /admin/v1/bootstrap', (route) => json(route, { Initialized: initialized }));
    api.handlers.set('POST /admin/v1/bootstrap', (route) => { initialized = true; return json(route, { User: administrator }, 201); });
    api.handlers.set('POST /admin/v1/session', (route) => json(route, { User: administrator, CSRFToken: csrf }));
    await page.goto('/admin/');
    await expect(page.getByRole('heading', { name: 'Set up your server', exact: true })).toBeVisible();
    await evidence(page, testInfo, 'setup-en-dark');
    await page.getByLabel('Setup token', { exact: true }).fill('synthetic-setup-token');
    await page.getByRole('textbox', { name: 'Administrator username', exact: true }).fill(administrator.Name);
    await page.getByLabel('Password', { exact: true }).fill('synthetic-only-password');
    await page.getByRole('button', { name: 'Create administrator', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Add your first library', exact: true })).toBeVisible();
    await evidence(page, testInfo, 'setup-first-library-en-dark');
    await page.getByRole('button', { name: 'Skip for now', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Remote access', exact: true })).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Default interface language', exact: true })).toBeEnabled();
    await page.getByRole('button', { name: 'Continue', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Your server is ready', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Finish setup', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Username', exact: true })).toHaveValue('');
    await expect(page.getByLabel('Password', { exact: true })).toHaveValue('');
    expect(api.writes().map(({ method, path }) => `${method} ${path}`)).toEqual(['POST /admin/v1/bootstrap', 'POST /admin/v1/session', 'DELETE /admin/v1/session']);
    const storage = await page.evaluate(() => JSON.stringify([Object.entries(localStorage), Object.entries(sessionStorage)]));
    expect(storage).not.toContain('synthetic-setup-token');
    expect(storage).not.toContain('synthetic-only-password');
    expect(storage).not.toContain(csrf);
    expect(storage).not.toContain(administrator.Name);
  });

  test('reduced motion and tablet widths retain content and keyboard navigation', async ({ page, api }, testInfo) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    for (const viewport of [{ width: 768, height: 1024 }, { width: 844, height: 390 }]) {
      await page.setViewportSize(viewport);
      await page.goto('/admin/access/users');
      await ready(page, destinations[5]);
      await noOverflow(page);
      const opener = page.getByRole('button', { name: 'Open navigation', exact: true });
      await opener.focus();
      await page.keyboard.press('Enter');
      await expect(navigation(page)).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(opener).toBeFocused();
      await evidence(page, testInfo, `users-reduced-motion-${viewport.width}x${viewport.height}`);
    }
    expect(api.writes()).toEqual([]);
  });
});

test('Chinese controls preserve raw user, device and application names that match translation keys', async ({ page, api }, testInfo) => {
  const fixture = installUserDesignFixture(api);
  fixture.user.Name = 'Settings';
  api.settings.ServerNameMode = 'custom';
  api.settings.Overrides.ServerName = 'Settings';
  api.settings.Effective.ServerName = 'Settings';
  api.settings.Defaults.ServerName = 'Default';
  api.settings.Sources.ServerName = 'database';
  api.settings.Sorting = { SortRemoveWords: ['Default'] };
  api.handlers.set('GET /admin/v1/libraries', (route) => json(route, { Items: [{ ...library, Name: 'Settings' }], TotalRecordCount: 1 }));
  api.handlers.set('GET /admin/v1/devices', (route, request) => {
    const url = new URL(request.url());
    return json(route, { Items: [{ Id: '101', Revision: '1', ReportedDeviceId: 'translation-device', Name: 'Users', ReportedName: 'Users', CustomName: 'Users', AppName: 'Synthetic client', AppVersion: '1.0', LastUserId: viewerId, LastUserName: 'Settings', CreatedAt: stamp, LastSeenAt: stamp, IpAddress: '192.0.2.4', ActiveLoginCount: 1 }], TotalRecordCount: 1, StartIndex: Number(url.searchParams.get('StartIndex') ?? 0), Limit: Number(url.searchParams.get('Limit') ?? 25) });
  });
  api.handlers.set('GET /admin/v1/api-keys', (route, request) => {
    const url = new URL(request.url());
    return json(route, { Items: [{ Id: '201', AppName: 'General', CreatedAt: stamp, LastUsedAt: null, RevokedAt: null, CreatedBy: administrator.Id, IPAddress: '192.0.2.5', Status: 'active' }], TotalRecordCount: 1, StartIndex: Number(url.searchParams.get('StartIndex') ?? 0), Limit: Number(url.searchParams.get('Limit') ?? 25) });
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/admin/access/users');
  await expect(page.getByRole('main').getByRole('heading', { level: 1 })).toHaveText('用户');
  await expect(page.getByRole('main').getByText('Settings', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '管理 Settings', exact: true }).click();
  const sheet = page.getByRole('dialog', { name: /Settings/ });
  for (const name of ['权限', '个人资料', '设备', '密码']) await expect(sheet.getByRole('tab', { name, exact: true })).toBeVisible();
  await expect(sheet.getByRole('tab', { name: '播放偏好', exact: true })).toHaveCount(0);
  await sheet.getByRole('tab', { name: '个人资料', exact: true }).click();
  await expect(sheet.getByRole('textbox', { name: '用户名', exact: true })).toHaveValue('Settings');
  await evidence(page, testInfo, 'user-sheet-390-zh-raw-name');
  await page.keyboard.press('Escape');
  await expect(sheet).not.toBeVisible();
  await page.goto('/admin/media/analysis');
  const libraryFilter = page.getByRole('combobox', { name: '筛选媒体库', exact: true });
  await expect(libraryFilter.locator(`option[value="${library.Id}"]`)).toHaveText('Settings');
  await expect(libraryFilter.locator('option[value=""]')).toHaveText('全部媒体库');
  await page.goto('/admin/access/devices');
  await expect(page.getByRole('main').getByRole('heading', { level: 1 })).toHaveText('设备');
  await expect(page.getByRole('main').getByText('Users', { exact: true }).filter({ visible: true }).first()).toBeVisible();
  await page.goto('/admin/access/api-keys');
  await expect(page.getByRole('main').getByRole('heading', { level: 1 })).toHaveText('API 密钥');
  await expect(page.getByRole('main').getByText('General', { exact: true }).filter({ visible: true }).first()).toBeVisible();
  await page.goto('/admin/settings/general');
  await expect(page.getByRole('main').getByRole('heading', { level: 1 })).toHaveText('常规');
  const identity = page.getByRole('region', { name: '服务器标识', exact: true });
  await expect(identity.locator('dd')).toHaveText(['Settings', 'Default']);
  await expect(identity.locator('dt').last()).toHaveText('部署默认值');
  const sorting = page.getByRole('region', { name: '媒体库排序', exact: true });
  await expect(sorting.getByRole('combobox', { name: '排序名称中移除的词', exact: true })).toBeVisible();
  await expect(sorting.locator('.goby-chip')).toContainText('Default');
  await expect(sorting.locator('dd')).toHaveText('Default');
  for (const [label, value] of [['绑定地址', '127.0.0.1'], ['HTTP 端口', '8096']] as const) {
    const network = page.getByRole('region', { name: label, exact: true });
    await expect(network.getByRole('textbox', { name: label, exact: true })).toHaveValue(value);
    await expect(network.getByRole('checkbox')).toBeChecked();
    await expect(network.getByText(`使用部署默认值 (${value})`, { exact: true })).toBeVisible();
  }
  await page.getByRole('main').getByRole('button', { name: '重置已保存的覆盖', exact: true }).click();
  const reset = page.getByRole('dialog', { name: '重置已保存的覆盖', exact: true });
  await expect(reset.locator('#reset-settings-description')).toHaveText('所选名称和服务器限制将恢复部署默认值。重置额外视频宽度上限会将其设为 0，服务器宽度上限仍然适用。');
  await expect(reset).toContainText('Settings');
  await expect(reset).toContainText('Default');
  await expect(reset.getByText('当前值: Settings', { exact: true })).toBeVisible();
  await expect(reset.getByText('部署默认值: Default', { exact: true })).toBeVisible();
  await expect(reset.getByText('部署默认值: 2 声道', { exact: true })).toBeVisible();
  await expect(reset).not.toContainText('2 channels');
  await evidence(page, testInfo, 'settings-reset-raw-name-390-zh');
  await reset.getByRole('button', { name: '取消', exact: true }).click();
  await expect(reset).not.toBeVisible();
  await page.goto('/admin/settings/transcode');
  const audioChannels = page.getByRole('textbox', { name: '最大音频声道数', exact: true });
  const audioRow = page.locator('.settings-setting-row').filter({ has: audioChannels });
  await expect(audioChannels).toHaveValue('2');
  await expect(audioRow.getByRole('checkbox')).toBeChecked();
  await expect(audioRow.getByText('使用部署默认值 (2 声道)', { exact: true })).toBeVisible();
  await expect(audioRow).not.toContainText('2 channels');
  await audioRow.scrollIntoViewIfNeeded();
  await evidence(page, testInfo, 'settings-transcode-default-audio-390-zh');
  expect(api.writes()).toEqual([]);
});

async function expectFocusInside(page: Page, sheet: ReturnType<Page['getByRole']>): Promise<void> {
  for (let count = 0; count < 18; count += 1) {
    await page.keyboard.press(count < 9 ? 'Tab' : 'Shift+Tab');
    expect(await sheet.evaluate((element) => element.contains(document.activeElement)), 'Focus must stay in the topmost modal sheet.').toBe(true);
  }
}
