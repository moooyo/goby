import type { Dialog, Locator, Page } from '@playwright/test';
import type { ManagedUser, UpdateUserInput } from '../src/api';
import type { ArtworkCollection } from '../src/artworkApi';
import { csrf, expect, json, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';
import { initialUserRevision, installUserDesignFixture, viewerId } from './user-design-fixture';

// Every response is synthetic. These workflows exercise the admin client and
// preserve account mutation safeguards without changing a real user.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off', preferences: { language: 'en', theme: 'light' } });

const userPath = `/admin/v1/users/${viewerId}`;
const preferencesPath = `${userPath}/preferences`;

async function openViewer(page: Page): Promise<Locator> {
  await page.goto('/admin/access/users');
  await page.getByRole('button', { name: 'Manage Family viewer', exact: true }).click();
  const sheet = page.getByRole('dialog').filter({ has: page.locator('#managed-user-form') });
  await expect(sheet.getByRole('region', { name: 'Library access', exact: true })).toBeVisible();
  return sheet;
}

test('the four user tabs preserve one account draft and save profile and policy together', async ({ page, api }, testInfo) => {
  const fixture = installUserDesignFixture(api);
  const originalSave = api.handlers.get(`PUT ${userPath}`)!;
  let release = () => {};
  const pending = new Promise<void>((resolve) => { release = resolve; });
  api.handlers.set(`PUT ${userPath}`, async (route, request) => { await pending; await originalSave(route, request); });
  const sheet = await openViewer(page);
  await expect(sheet.getByRole('tab')).toHaveText(['Permissions', 'Profile', 'Devices', 'Password']);
  await sheet.getByRole('switch', { name: 'Remuxing', exact: true }).uncheck();
  await evidence(page, testInfo, 'design2-user-permissions-draft');

  await sheet.getByRole('tab', { name: 'Profile', exact: true }).click();
  const username = sheet.getByRole('textbox', { name: 'Username', exact: true });
  await username.fill('  Family viewer revised  ');
  await expect(sheet.getByRole('button', { name: 'Manage avatar', exact: true })).toBeDisabled();
  await sheet.getByRole('tab', { name: 'Devices', exact: true }).click();
  await expect(sheet.getByRole('list', { name: 'User devices', exact: true })).toContainText('Family tablet');
  await sheet.getByRole('tab', { name: 'Password', exact: true }).click();
  for (const name of ['Reset password', 'Manage local credentials', 'Delete user']) {
    await expect(sheet.getByRole('button', { name, exact: true })).toBeDisabled();
  }
  await page.keyboard.press('Escape');
  const discard = page.getByRole('dialog', { name: 'Discard unsaved changes?', exact: true });
  await discard.getByRole('button', { name: 'Keep editing', exact: true }).click();
  await sheet.getByRole('tab', { name: 'Profile', exact: true }).click();
  await expect(username).toHaveValue('  Family viewer revised  ');
  expect(api.writes()).toEqual([]);

  try {
    await sheet.getByRole('button', { name: 'Save changes', exact: true }).click();
    await expect.poll(() => api.writes().length).toBe(1);
    await expect(username).toBeDisabled();
    for (const tab of await sheet.getByRole('tab').all()) await expect(tab).toBeDisabled();
    await page.keyboard.press('Control+s');
    expect(api.writes()).toEqual([{ method: 'PUT', path: userPath, body: {
      Revision: initialUserRevision, Name: 'Family viewer revised', IsAdministrator: false, IsDisabled: false,
      Policy: { ...fixture.initialPolicy, EnablePlaybackRemuxing: false },
    }, csrf }]);
  } finally { release(); }
  await expect(sheet.getByText('All changes saved', { exact: true })).toBeVisible();
  await expect(username).toHaveValue('Family viewer revised');
  expect(fixture.user.Revision).toBe('9007199254740994');
  expect(fixture.user.Policy).toEqual({ ...fixture.initialPolicy, EnablePlaybackRemuxing: false });
  expect(api.requests.filter((request) => request.path === preferencesPath)).toEqual([]);
  await noOverflow(page);
  await evidence(page, testInfo, 'design2-user-profile-saved');
  await sheet.getByRole('button', { name: 'Close dialog', exact: true }).click();
  await page.getByRole('button', { name: 'Manage Family viewer revised', exact: true }).click();
  await sheet.getByRole('tab', { name: 'Profile', exact: true }).click();
  await expect(username).toHaveValue('Family viewer revised');
  expect(api.requests.filter((request) => request.path === preferencesPath)).toEqual([]);
});

for (const failure of [
  { name: 'a revision conflict', status: 409, code: 'revision_conflict', savedName: 'Family viewer concurrent', warning: 'This user changed after you opened it. Your draft is still here.' },
  { name: 'an unconfirmed response', status: 503, code: 'operation_unconfirmed', savedName: 'Family viewer candidate', warning: 'Your changes may already have been saved.' },
]) {
  test(`account saves require an explicit reload after ${failure.name}`, async ({ page, api }) => {
    const fixture = installUserDesignFixture(api);
    let stored: ManagedUser = structuredClone(fixture.user);
    let attempts = 0;
    api.handlers.set(`GET ${userPath}`, (route) => json(route, { User: stored }));
    api.handlers.set(`PUT ${userPath}`, (route, request) => {
      const input = request.postDataJSON() as UpdateUserInput;
      attempts += 1;
      if (attempts === 1) {
        // A conflict has a concurrent winner; an unavailable response can follow
        // an applied write. Both require reading the authoritative saved user.
        stored = { ...stored, ...(failure.status === 503 ? input : {}), Name: failure.savedName, Revision: '9007199254740994' };
        return json(route, { Error: { Code: failure.code, Message: 'Review the latest saved user.' } }, failure.status);
      }
      if (input.Revision !== stored.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'The saved revision changed.' } }, 409);
      stored = { ...stored, ...input, Revision: (BigInt(stored.Revision) + 1n).toString() };
      return json(route, { User: stored, CurrentSessionRevoked: false });
    });

    const sheet = await openViewer(page);
    await sheet.getByRole('tab', { name: 'Profile', exact: true }).click();
    const username = sheet.getByRole('textbox', { name: 'Username', exact: true });
    const save = sheet.getByRole('button', { name: 'Save changes', exact: true });
    await username.fill('Family viewer candidate');
    await save.click();
    await expect(sheet.getByRole('status').filter({ hasText: failure.warning })).toBeVisible();
    await expect(username).toHaveValue('Family viewer candidate');
    await expect(save).toBeDisabled();
    await page.keyboard.press('Control+s');
    await username.press('Enter');
    expect(api.writes()).toHaveLength(1);
    expect(api.writes()[0]).toMatchObject({ method: 'PUT', path: userPath, body: { Revision: initialUserRevision } });

    const reads = api.reads(userPath).length;
    await sheet.getByRole('button', { name: 'Reload latest user', exact: true }).click();
    const discard = page.getByRole('dialog', { name: 'Discard unsaved changes?', exact: true });
    await expect(discard).toBeVisible();
    expect(api.reads(userPath)).toHaveLength(reads);
    await discard.getByRole('button', { name: 'Keep editing', exact: true }).click();
    await expect(username).toHaveValue('Family viewer candidate');
    await sheet.getByRole('button', { name: 'Discard', exact: true }).click();
    await username.fill('Family viewer retry');
    await expect(save).toBeDisabled();
    await page.keyboard.press('Control+s');
    expect(api.writes()).toHaveLength(1);

    await sheet.getByRole('button', { name: 'Reload latest user', exact: true }).click();
    await discard.getByRole('button', { name: 'Discard draft and reload', exact: true }).click();
    await expect(username).toHaveValue(failure.savedName);
    expect(api.reads(userPath)).toHaveLength(reads + 1);
    await username.fill('Family viewer reviewed');
    await expect(save).toBeEnabled();
    await save.click();
    await expect(sheet.getByText('All changes saved', { exact: true })).toBeVisible();
    expect(api.writes()).toHaveLength(2);
    expect(api.writes()[1]).toMatchObject({ method: 'PUT', path: userPath, body: { Revision: '9007199254740994', Name: 'Family viewer reviewed' }, csrf });
    expect(stored.Revision).toBe('9007199254740995');
    expect(api.requests.filter((request) => request.path === preferencesPath)).toEqual([]);
  });
}

test('avatar uploads immediately while credential drafts keep separate confirmations', async ({ page, api }) => {
  installUserDesignFixture(api);
  api.handlers.set(`GET ${userPath}/local-credentials`, (route) => json(route, {
    UserId: viewerId, Revision: '9007199254741002', HasLocalPassword: false, HasProfilePin: false, EnableLocalPassword: false,
  }));
  const picture = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64');
  let images: ArtworkCollection = { Revision: '1', Items: [] };
  api.handlers.set(`GET ${userPath}/image`, (route) => json(route, images));
  api.handlers.set(`GET ${userPath}/image/content`, (route) => route.fulfill({ contentType: 'image/png', body: picture }));
  const uploads: { body: Buffer | null; csrf?: string; contentType?: string; revision?: string }[] = [];
  let release = () => {};
  const pending = new Promise<void>((resolve) => { release = resolve; });
  // Capture the raw image before the dashboard fixture's JSON request recorder.
  await page.route(`**${userPath}/image`, async (route, request) => {
    if (request.method() !== 'PUT') return route.fallback();
    const headers = request.headers();
    uploads.push({ body: request.postDataBuffer(), csrf: headers['x-csrf-token'], contentType: headers['content-type'], revision: headers['if-match'] });
    await pending;
    images = { Revision: '2', Items: [{
      ImageType: 'Primary', ImageIndex: 0, Tag: 'synthetic-avatar', Width: 1, Height: 1, Size: String(picture.length),
      PreviewUrl: `${userPath}/image/content?tag=synthetic-avatar`, Source: 'managed',
    }] };
    await json(route, images);
  });
  const sheet = await openViewer(page);
  await sheet.getByRole('tab', { name: 'Profile', exact: true }).click();
  await sheet.getByRole('button', { name: 'Manage avatar', exact: true }).click();
  const avatar = page.getByRole('dialog', { name: 'Manage avatar', exact: true });
  const currentImage = avatar.getByRole('region', { name: 'Current image', exact: true });
  await expect(currentImage.locator('.artwork-current-stage').getByText('No image saved', { exact: true })).toBeVisible();
  await expect(currentImage.getByRole('img')).toHaveCount(0);
  try {
    await avatar.getByLabel('Choose image file', { exact: true }).setInputFiles({ name: 'synthetic-avatar.png', mimeType: 'image/png', buffer: picture });
    await expect.poll(() => uploads.length).toBe(1);
    await expect(avatar.getByRole('button', { name: 'Close', exact: true })).toBeDisabled();
    await page.keyboard.press('Escape');
    await expect(avatar).toBeVisible();
    expect(uploads[0]).toEqual({ body: picture, csrf, contentType: 'image/png', revision: '"1"' });
  } finally { release(); }
  await expect(avatar.getByText('Image saved.', { exact: true })).toBeVisible();
  await expect(avatar.getByLabel('Choose image file', { exact: true })).toHaveValue('');
  const confirmations: string[] = [];
  const dismissConfirmation = async (dialog: Dialog) => { confirmations.push(dialog.message()); await dialog.dismiss(); };
  page.on('dialog', dismissConfirmation);
  try {
    await avatar.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(avatar).not.toBeVisible();
  } finally { page.off('dialog', dismissConfirmation); }
  expect(confirmations).toEqual([]);

  await sheet.getByRole('tab', { name: 'Password', exact: true }).click();
  await sheet.getByRole('button', { name: 'Reset password', exact: true }).click();
  const password = page.getByRole('dialog', { name: 'Reset password', exact: true });
  const newPassword = password.locator('#reset-user-password');
  const confirmPassword = password.locator('#reset-user-confirm-password');
  await newPassword.fill('synthetic-new-password');
  await confirmPassword.fill('synthetic-new-password');
  await password.getByRole('button', { name: 'Cancel', exact: true }).click();
  const discard = page.getByRole('dialog', { name: 'Discard unsaved changes?', exact: true });
  await discard.getByRole('button', { name: 'Keep editing', exact: true }).click();
  await expect(newPassword).toHaveValue('synthetic-new-password');
  await expect(confirmPassword).toHaveValue('synthetic-new-password');
  await password.getByRole('button', { name: 'Cancel', exact: true }).click();
  await discard.getByRole('button', { name: 'Discard changes', exact: true }).click();
  await expect(password).not.toBeVisible();

  await sheet.getByRole('button', { name: 'Manage local credentials', exact: true }).click();
  const credentials = page.getByRole('dialog', { name: 'Local credentials', exact: true });
  const localPassword = credentials.getByLabel(/^New local password(?:\s*\*)?$/);
  const confirmLocalPassword = credentials.getByLabel(/^Confirm local password(?:\s*\*)?$/);
  await localPassword.fill('synthetic-local-password');
  await confirmLocalPassword.fill('synthetic-local-password');
  page.once('dialog', async (dialog) => { expect(dialog.message()).toBe('Discard unsaved local credentials?'); await dialog.dismiss(); });
  await credentials.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(localPassword).toHaveValue('synthetic-local-password');
  await expect(confirmLocalPassword).toHaveValue('synthetic-local-password');
  page.once('dialog', (dialog) => dialog.accept());
  await credentials.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(credentials).not.toBeVisible();

  await sheet.getByRole('button', { name: 'Delete user', exact: true }).click();
  const deletion = page.getByRole('dialog', { name: 'Delete user', exact: true });
  await expect(deletion).toContainText('This permanently deletes the account');
  await deletion.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(deletion).not.toBeVisible();
  expect(uploads).toHaveLength(1);
  expect(api.writes()).toEqual([]);
  expect(api.requests.filter((request) => request.path === preferencesPath)).toEqual([]);
});
