import type { Locator, Page, Request } from '@playwright/test';
import { administrator, expect, json, library, seriesLibrary, test } from './dashboard-fixture';
import type { DashboardAPI } from './dashboard-fixture';
import { evidence } from './dashboard-review';
import type { MetadataItemSummary } from '../src/api';
import type { CollectionMember, CollectionPatch, ManagedCollection } from '../src/collectionsApi';

const collectionsPath = '/admin/v1/collections';
const collectionPath = `${collectionsPath}/design2-collection`;

function collection(overrides: Partial<ManagedCollection> = {}): ManagedCollection {
  return {
    Id: 'design2-collection', Name: 'Weekend cinema', Type: 'BoxSet', IsFolder: true, ParentId: '',
    OwnerId: administrator.Id, MediaType: 'Video', IsPublic: false, IsLocked: false, ChildCount: 2,
    Shares: [], ...overrides,
  };
}

function installCollections(api: DashboardAPI, overrides: Partial<ManagedCollection> = {}) {
  const saved = collection(overrides);
  const members: CollectionMember[] = [
    { Id: 'film-one', Name: 'The feature film', Type: 'Movie' },
    { Id: 'film-two', Name: 'Another feature film', Type: 'Movie' },
  ];
  const pageOf = <T>(request: Request, items: T[]) => {
    const url = new URL(request.url());
    const start = Number(url.searchParams.get('StartIndex') ?? 0);
    const limit = Number(url.searchParams.get('Limit') ?? 25);
    return { Items: items.slice(start, start + limit), TotalRecordCount: items.length, StartIndex: start, Limit: limit };
  };
  api.handlers.set(`GET ${collectionsPath}`, (route, request) => json(route, pageOf(request, [saved])));
  api.handlers.set(`GET ${collectionPath}`, (route) => json(route, saved));
  api.handlers.set(`GET ${collectionPath}/items`, (route, request) => json(route, pageOf(request, members)));
  return { saved, members };
}

function expectCollectionOnlyRequests(api: DashboardAPI) {
  expect(api.requests.filter((request) => request.path.includes('/playlists')), 'Collection management must not read or write playlists.').toEqual([]);
  expect(api.reads('/admin/v1/users'), 'Collection management must not load users for ownership or sharing controls.').toEqual([]);
}

async function expectCollectionOnlyDialog(dialog: Locator) {
  await expect(dialog.getByRole('textbox', { name: /^Collection name/ })).toBeVisible();
  await expect(dialog.getByRole('textbox', { name: /^Playlist name/ })).toHaveCount(0);
  await expect(dialog.getByRole('combobox', { name: /Owner|Share with user/ })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: /^(?:Create playlist|Add share)$/ })).toHaveCount(0);
  await expect(dialog.getByRole('region', { name: 'Sharing', exact: true })).toHaveCount(0);
}

async function openCollection(page: Page) {
  await page.getByRole('button', { name: 'Manage Weekend cinema', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Manage collection', exact: true });
  await expect(dialog.getByRole('textbox', { name: /^Collection name/ })).toHaveValue('Weekend cinema');
  return dialog;
}

async function openNewCollection(page: Page) {
  await page.getByRole('main').getByRole('button', { name: 'New collection', exact: true }).first().click();
  return page.getByRole('dialog', { name: 'Create collection', exact: true });
}

test.use({ serviceWorkers: 'block', preferences: { language: 'en', theme: 'light' } });

for (const path of ['/admin/media/collections', '/admin/collections']) {
  test(`collection-only management is available at ${path}`, async ({ page, api }) => {
    installCollections(api);
    await page.goto(path);
    const main = page.getByRole('main');
    await expect(main.getByRole('heading', { name: 'Collections', exact: true })).toBeVisible();
    await expect(main.getByRole('button', { name: 'New collection', exact: true })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Playlists', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Create playlist', exact: true })).toHaveCount(0);
    if (path === '/admin/media/collections') {
      await expect(main.getByRole('button', { name: 'Manage Weekend cinema', exact: true }).first()).toBeVisible();
      await evidence(page, test.info(), 'design2-collections-list');
    }

    await main.getByRole('button', { name: 'New collection', exact: true }).click();
    const createDialog = page.getByRole('dialog', { name: 'Create collection', exact: true });
    await expectCollectionOnlyDialog(createDialog);
    await createDialog.getByRole('button', { name: /^(?:Close|Close dialog)$/ }).last().click();
    await expect(createDialog).toHaveCount(0);

    const dialog = await openCollection(page);
    await expectCollectionOnlyDialog(dialog);
    if (path === '/admin/media/collections') {
      await expect(dialog.getByRole('list', { name: 'Collection items', exact: true }).getByRole('listitem')).toHaveCount(2);
      await expect(dialog.getByText('Adding or removing media takes effect immediately. Save collection details separately.', { exact: true })).toBeVisible();
      await expect(dialog.getByRole('checkbox', { name: /The feature film/ })).toBeVisible();
      await evidence(page, test.info(), 'design2-collections-members-immediate');
    }
    expect(api.reads(collectionsPath).length).toBeGreaterThan(0);
    expect(api.reads(collectionPath).length).toBeGreaterThan(0);
    expectCollectionOnlyRequests(api);
    expect(api.writes()).toEqual([]);
  });
}

test('cancelling collection deletion keeps the collection and sends no mutation', async ({ page, api }) => {
  installCollections(api);
  await page.goto('/admin/media/collections');
  const dialog = await openCollection(page);
  await dialog.getByRole('button', { name: 'Delete collection', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Delete collection', exact: true });
  await expect(confirmation).toBeVisible();
  await confirmation.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(confirmation).toHaveCount(0);
  await expect(dialog.getByRole('textbox', { name: /^Collection name/ })).toHaveValue('Weekend cinema');
  expectCollectionOnlyRequests(api);
  expect(api.writes(), 'Cancelling deletion must not send a mutation.').toEqual([]);
});

test('an unknown collection save retains the draft and reads the saved result without replaying the write', async ({ page, api }) => {
  const state = installCollections(api);
  api.handlers.set(`POST ${collectionPath}`, async (route, request) => {
    Object.assign(state.saved, request.postDataJSON() as CollectionPatch);
    // The mutation committed, but its success response could not be delivered.
    return json(route, { Error: { Code: 'response_unavailable', Message: 'The result could not be confirmed.' } }, 500);
  });
  await page.goto('/admin/media/collections');
  const dialog = await openCollection(page);
  const name = dialog.getByRole('textbox', { name: /^Collection name/ });
  await name.fill('Saved after response loss');
  await dialog.getByRole('button', { name: 'Save details', exact: true }).click();
  await expect(dialog.getByText(/The result could not be confirmed or the saved details changed/)).toBeVisible();
  await expect(name).toHaveValue('Saved after response loss');
  await expect(name).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Save details', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0]).toMatchObject({ method: 'POST', path: collectionPath, body: { Name: 'Saved after response loss' } });
  await expect(dialog.getByRole('list', { name: 'Collection items', exact: true }).getByRole('listitem')).toHaveCount(2);
  await expect(dialog.getByRole('checkbox', { name: /The feature film/ })).toBeVisible();
  await evidence(page, test.info(), 'design2-collections-unknown-save-blocked');

  const readsBeforeReload = api.reads(collectionPath).length;
  await dialog.getByRole('button', { name: 'Reload latest collection', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Discard unsaved changes?', exact: true });
  await confirmation.getByRole('button', { name: 'Keep editing', exact: true }).click();
  await expect(confirmation).toHaveCount(0);
  await expect(name).toHaveValue('Saved after response loss');
  expect(api.reads(collectionPath)).toHaveLength(readsBeforeReload);
  expect(api.writes()).toHaveLength(1);

  await dialog.getByRole('button', { name: 'Reload latest collection', exact: true }).click();
  await confirmation.getByRole('button', { name: 'Discard changes', exact: true }).click();
  await expect(name).toBeEnabled();
  await expect(name).toHaveValue('Saved after response loss');
  await expect(dialog.getByRole('button', { name: 'Save details', exact: true })).toBeDisabled();
  expect(api.reads(collectionPath).length).toBeGreaterThan(readsBeforeReload);
  expect(api.writes(), 'Reloading an unknown result must not replay the save.').toHaveLength(1);
  expectCollectionOnlyRequests(api);
});

test('a failed member readback blocks further changes until a read-only reload', async ({ page, api }) => {
  const state = installCollections(api);
  let failReadback = false;
  api.handlers.set(`GET ${collectionPath}`, (route) => {
    if (failReadback) {
      failReadback = false;
      return json(route, { Error: { Code: 'readback_unavailable', Message: 'The saved collection could not be loaded.' } }, 500);
    }
    return json(route, state.saved);
  });
  api.handlers.set(`DELETE ${collectionPath}/items`, (route, request) => {
    expect([...new URL(request.url()).searchParams.entries()]).toEqual([['Ids', 'film-two']]);
    state.members.splice(state.members.findIndex((member) => member.Id === 'film-two'), 1);
    state.saved.ChildCount = state.members.length;
    failReadback = true;
    return json(route, {});
  });
  await page.goto('/admin/media/collections');
  const dialog = await openCollection(page);
  const members = dialog.getByRole('list', { name: 'Collection items', exact: true }).getByRole('listitem');
  await expect(members).toHaveCount(2);
  await dialog.getByRole('button', { name: 'Remove Another feature film from collection', exact: true }).click();
  await expect(dialog.getByText(/The result could not be confirmed or the saved details changed/)).toBeVisible();
  const name = dialog.getByRole('textbox', { name: /^Collection name/ });
  await expect(name).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Remove The feature film from collection', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Add selected media', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0]).toMatchObject({ method: 'DELETE', path: `${collectionPath}/items` });

  const readsBeforeReload = api.reads(collectionPath).length;
  await dialog.getByRole('button', { name: 'Reload latest collection', exact: true }).click();
  await expect(name).toBeEnabled();
  await expect(members).toHaveCount(1);
  await expect(members.first()).toContainText('The feature film');
  await expect(dialog.getByRole('button', { name: 'Remove The feature film from collection', exact: true })).toBeEnabled();
  expect(api.reads(collectionPath).length).toBeGreaterThan(readsBeforeReload);
  expect(api.writes(), 'Reloading after a failed readback must not replay member removal.').toHaveLength(1);
  expectCollectionOnlyRequests(api);
});

test('creating a collection saves initial media atomically and later member additions take effect immediately', async ({ page, api }) => {
  const saved = collection({ Id: 'created-collection', Name: 'Saturday cinema', ChildCount: 1 });
  const createdPath = `${collectionsPath}/${saved.Id}`;
  const members: CollectionMember[] = [{ Id: 'film-one', Name: 'The feature film', Type: 'Movie' }];
  let created = false;
  api.handlers.set(`GET ${collectionsPath}`, (route) => json(route, { Items: created ? [saved] : [], TotalRecordCount: created ? 1 : 0 }));
  api.handlers.set(`POST ${collectionsPath}`, (route, request) => {
    expect(request.postDataJSON()).toEqual({ Name: saved.Name, IsPublic: false, IsLocked: false, Ids: ['film-one'] });
    created = true;
    return json(route, { Id: saved.Id, Name: saved.Name });
  });
  api.handlers.set(`GET ${createdPath}`, (route) => json(route, saved));
  api.handlers.set(`GET ${createdPath}/items`, (route) => json(route, { Items: members, TotalRecordCount: members.length }));
  api.handlers.set(`POST ${createdPath}/items`, (route, request) => {
    expect([...new URL(request.url()).searchParams.entries()]).toEqual([['Ids', 'series-one']]);
    members.push({ Id: 'series-one', Name: 'Opening series', Type: 'Series' });
    saved.ChildCount = members.length;
    return json(route, {});
  });

  await page.goto('/admin/media/collections');
  const createDialog = await openNewCollection(page);
  await createDialog.getByRole('textbox', { name: /^Collection name/ }).fill(`  ${saved.Name}  `);
  await createDialog.getByRole('checkbox', { name: /The feature film/ }).check();
  await expect(createDialog.getByRole('list', { name: 'Draft collection items', exact: true })).toContainText('The feature film');
  expect(api.writes(), 'Staging initial media must not send a mutation.').toEqual([]);
  await evidence(page, test.info(), 'design2-collections-create-draft');
  await createDialog.getByRole('button', { name: 'Create collection', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Manage collection', exact: true });
  await expect(dialog.getByRole('textbox', { name: /^Collection name/ })).toHaveValue(saved.Name);
  const rows = dialog.getByRole('list', { name: 'Collection items', exact: true }).getByRole('listitem');
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toContainText('The feature film');
  await expect(dialog.getByRole('checkbox', { name: /The feature film/ })).not.toBeChecked();
  await expect(dialog.getByRole('button', { name: 'Add selected media', exact: true })).toBeDisabled();
  expect(api.writes(), 'Creation must write initial membership in its single POST.').toHaveLength(1);
  await evidence(page, test.info(), 'design2-collections-create-atomic');

  await dialog.getByRole('combobox', { name: 'Source library', exact: true }).selectOption(seriesLibrary.Id);
  const candidate = dialog.getByRole('checkbox', { name: /Opening series/ });
  await candidate.check();
  expect(api.writes(), 'Selecting media must not mutate the collection.').toHaveLength(1);
  await dialog.getByRole('button', { name: 'Add selected media', exact: true }).click();

  await expect(rows).toHaveCount(2);
  await expect(rows.first()).toContainText('The feature film');
  await expect(rows.last()).toContainText('Opening series');
  await expect(candidate).not.toBeChecked();
  await expect(dialog.getByRole('button', { name: 'Add selected media', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Save details', exact: true })).toBeDisabled();
  expect(api.reads(createdPath).length).toBeGreaterThan(0);
  expect(api.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([
    { method: 'POST', path: collectionsPath, body: { Name: saved.Name, IsPublic: false, IsLocked: false, Ids: ['film-one'] } },
    { method: 'POST', path: `${createdPath}/items`, body: null },
  ]);
  expectCollectionOnlyRequests(api);
});

test('initial member selections survive library changes and the unsaved dialog guard without a mutation', async ({ page, api }) => {
  installCollections(api);
  await page.goto('/admin/media/collections');
  const dialog = await openNewCollection(page);
  const name = dialog.getByRole('textbox', { name: /^Collection name/ });
  const create = dialog.getByRole('button', { name: 'Create collection', exact: true });
  await name.fill('Collection draft');
  await expect(create).toBeDisabled();
  await expect(dialog.getByText('Select at least one media item before creating the collection.', { exact: true })).toBeVisible();
  await dialog.locator('form').evaluate((form: HTMLFormElement) => form.requestSubmit());
  expect(api.writes(), 'Submitting a named draft with no initial members must not create an empty collection.').toEqual([]);
  await name.fill('');

  await dialog.getByRole('checkbox', { name: /The feature film/ }).check();
  await dialog.getByRole('combobox', { name: 'Source library', exact: true }).selectOption(seriesLibrary.Id);
  await dialog.getByRole('checkbox', { name: /Opening series/ }).check();
  const draftRows = dialog.getByRole('list', { name: 'Draft collection items', exact: true }).getByRole('listitem');
  await expect(draftRows).toHaveCount(2);
  await expect(draftRows.first()).toContainText('The feature film');
  await expect(draftRows.last()).toContainText('Opening series');
  await expect(dialog.getByText('2 draft items', { exact: true })).toBeVisible();
  await expect(create).toBeDisabled();
  await page.keyboard.press('Escape');
  const confirmation = page.getByRole('dialog', { name: 'Discard unsaved changes?', exact: true });
  await confirmation.getByRole('button', { name: 'Keep editing', exact: true }).click();
  await expect(name).toHaveValue('');
  await expect(draftRows).toHaveCount(2);
  await expect(dialog.getByRole('checkbox', { name: /Opening series/ })).toBeChecked();
  await name.fill('Collection draft');
  await expect(create).toBeEnabled();

  await dialog.getByRole('button', { name: 'Remove The feature film from draft collection', exact: true }).click();
  await expect(draftRows).toHaveCount(1);
  await dialog.getByRole('combobox', { name: 'Source library', exact: true }).selectOption(library.Id);
  await expect(dialog.getByRole('checkbox', { name: /The feature film/ })).not.toBeChecked();
  await expect(draftRows.first()).toContainText('Opening series');
  await dialog.getByRole('button', { name: 'Clear selection', exact: true }).click();
  await expect(draftRows).toHaveCount(0);
  await expect(dialog.getByText('0 draft items', { exact: true })).toBeVisible();
  await expect(create).toBeDisabled();
  await name.press('Enter');
  expect(api.writes(), 'Editing or clearing initial members must remain entirely in the draft.').toEqual([]);
  await evidence(page, test.info(), 'design2-collections-create-member-guard');
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await confirmation.getByRole('button', { name: 'Discard changes', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expectCollectionOnlyRequests(api);
  expect(api.writes()).toEqual([]);
});

test('a locked collection creates its initial members once while its mutation is pending', async ({ page, api }) => {
  const saved = collection({ Id: 'locked-created-collection', Name: 'Locked cinema', IsLocked: true, ChildCount: 1 });
  const createdPath = `${collectionsPath}/${saved.Id}`;
  let created = false;
  let releaseCreate = () => {};
  const createReady = new Promise<void>((resolve) => { releaseCreate = resolve; });
  api.handlers.set(`GET ${collectionsPath}`, (route) => json(route, { Items: created ? [saved] : [], TotalRecordCount: created ? 1 : 0 }));
  api.handlers.set(`POST ${collectionsPath}`, async (route, request) => {
    expect(request.postDataJSON()).toEqual({ Name: saved.Name, IsPublic: false, IsLocked: true, Ids: ['film-one'] });
    await createReady;
    created = true;
    return json(route, { Id: saved.Id, Name: saved.Name });
  });
  api.handlers.set(`GET ${createdPath}`, (route) => json(route, saved));
  api.handlers.set(`GET ${createdPath}/items`, (route) => json(route, { Items: [{ Id: 'film-one', Name: 'The feature film', Type: 'Movie' }], TotalRecordCount: 1 }));
  await page.goto('/admin/media/collections');
  const draft = await openNewCollection(page);
  const name = draft.getByRole('textbox', { name: /^Collection name/ });
  await name.fill(saved.Name);
  await draft.getByRole('switch', { name: 'Lock membership and deletion', exact: true }).check();
  await draft.getByRole('checkbox', { name: /The feature film/ }).check();
  try {
    await draft.getByRole('button', { name: 'Create collection', exact: true }).click();
    await expect.poll(() => api.writes().length).toBe(1);
    await expect(name).toBeDisabled();
    await expect(draft.getByRole('switch', { name: 'Lock membership and deletion', exact: true })).toBeDisabled();
    await expect(draft.getByRole('checkbox', { name: /The feature film/ })).toBeDisabled();
    for (const button of ['Create collection', 'Close', 'Clear selection', 'Remove The feature film from draft collection']) {
      await expect(draft.getByRole('button', { name: button, exact: true })).toBeDisabled();
    }
    await page.keyboard.press('Escape');
    await expect(draft).toBeVisible();
    await expect(page.getByRole('dialog', { name: 'Discard unsaved changes?', exact: true })).toHaveCount(0);
    await draft.getByRole('button', { name: 'Close dialog', exact: true }).click();
    await expect(draft).toBeVisible();
    await draft.locator('form').evaluate((form: HTMLFormElement) => form.requestSubmit());
    expect(api.writes(), 'A pending create must not be replayed by another form submission.').toHaveLength(1);
    await evidence(page, test.info(), 'design2-collections-create-pending');
  } finally { releaseCreate(); }

  const dialog = page.getByRole('dialog', { name: 'Manage collection', exact: true });
  await expect(dialog.getByRole('textbox', { name: /^Collection name/ })).toHaveValue(saved.Name);
  await expect(dialog.getByRole('list', { name: 'Collection items', exact: true }).getByRole('listitem')).toHaveCount(1);
  await expect(dialog.getByRole('checkbox', { name: /The feature film/ })).not.toBeChecked();
  await expect(dialog.getByRole('switch', { name: 'Lock membership and deletion', exact: true })).toBeChecked();
  await expect(dialog.getByRole('button', { name: 'Remove The feature film from collection', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Add selected media', exact: true })).toBeDisabled();
  expect(api.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([
    { method: 'POST', path: collectionsPath, body: { Name: saved.Name, IsPublic: false, IsLocked: true, Ids: ['film-one'] } },
  ]);
  expect(api.reads(createdPath).length).toBeGreaterThan(0);
  expectCollectionOnlyRequests(api);
});

for (const failure of [
  { name: 'an unknown collection creation', status: 500, response: { Error: { Code: 'response_unavailable', Message: 'The creation result could not be confirmed.' } } },
  { name: 'a collection creation with an invalid ID response', status: 200, response: { Id: ' ', Name: 'Unconfirmed collection' } },
]) {
  test(`${failure.name} cannot be submitted again before the list is reviewed`, async ({ page, api }) => {
    const state = installCollections(api);
    const unconfirmed = collection({ Id: 'unconfirmed-collection', Name: 'Unconfirmed collection', IsLocked: true, ChildCount: 1 });
    let created = false;
    api.handlers.set(`GET ${collectionsPath}`, (route) => json(route, {
      Items: created ? [state.saved, unconfirmed] : [state.saved], TotalRecordCount: created ? 2 : 1,
    }));
    api.handlers.set(`POST ${collectionsPath}`, (route, request) => {
      expect(request.postDataJSON()).toEqual({ Name: unconfirmed.Name, IsPublic: false, IsLocked: true, Ids: ['film-one'] });
      created = true;
      return json(route, failure.response, failure.status);
    });
    await page.goto('/admin/media/collections');
    await page.getByRole('main').getByRole('button', { name: 'New collection', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Create collection', exact: true });
    const name = dialog.getByRole('textbox', { name: /^Collection name/ });
    await name.fill(unconfirmed.Name);
    await dialog.getByRole('switch', { name: 'Lock membership and deletion', exact: true }).check();
    await dialog.getByRole('checkbox', { name: /The feature film/ }).check();
    await dialog.getByRole('button', { name: 'Create collection', exact: true }).click();
    await expect(dialog.getByText(/Close this dialog and refresh the list before deciding whether to create another entry/)).toBeVisible();
    await expect(name).toHaveValue(unconfirmed.Name);
    await expect(name).toBeDisabled();
    await expect(dialog.getByRole('switch', { name: 'Lock membership and deletion', exact: true })).toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: /The feature film/ })).toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: /The feature film/ })).toBeDisabled();
    await expect(dialog.getByRole('list', { name: 'Draft collection items', exact: true })).toContainText('The feature film');
    await expect(dialog.getByRole('button', { name: 'Remove The feature film from draft collection', exact: true })).toBeDisabled();
    await expect(dialog.getByRole('button', { name: 'Clear selection', exact: true })).toBeDisabled();
    await expect(dialog.getByRole('button', { name: 'Create collection', exact: true })).toBeDisabled();
    await expect(dialog.getByRole('button', { name: 'Reload', exact: true })).toHaveCount(0);
    expect(api.writes()).toHaveLength(1);
    await dialog.locator('form').evaluate((form: HTMLFormElement) => form.requestSubmit());
    expect(api.writes(), 'Unknown creation must stay blocked even for a repeated form submission.').toHaveLength(1);
    await evidence(page, test.info(), `design2-collections-create-${failure.status === 500 ? 'unknown' : 'invalid-id'}-blocked`);

    await dialog.getByRole('button', { name: 'Close', exact: true }).click();
    const confirmation = page.getByRole('dialog', { name: 'Discard unsaved changes?', exact: true });
    await confirmation.getByRole('button', { name: 'Keep editing', exact: true }).click();
    await expect(name).toHaveValue(unconfirmed.Name);
    await expect(dialog.getByRole('checkbox', { name: /The feature film/ })).toBeChecked();
    await dialog.getByRole('button', { name: 'Close', exact: true }).click();
    await confirmation.getByRole('button', { name: 'Discard changes', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.getByRole('main').getByRole('button', { name: `Manage ${unconfirmed.Name}`, exact: true }).first()).toBeVisible();
    expect(api.writes(), 'Reviewing the list must not replay an unknown creation.').toHaveLength(1);
    expectCollectionOnlyRequests(api);
  });
}

test('initial member selections survive pagination and search and are sent together in one create request', async ({ page, api }) => {
  const state = installCollections(api);
  const candidates: MetadataItemSummary[] = Array.from({ length: 26 }, (_, index) => ({
    Id: index === 0 ? 'film-one' : `paged-film-${index}`, LibraryId: library.Id, ParentId: '', ParentName: '',
    Name: index === 0 ? 'The feature film' : index === 25 ? 'Last page film' : `Paged film ${index}`,
    Type: 'Movie', Path: `/media/movies/Paged-${index}.mkv`, IsFolder: false, ProductionYear: 2025,
    IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0,
  }));
  const initial = [candidates[0], candidates[25]];
  const saved = collection({ Id: 'paged-created-collection', Name: 'Paged cinema', ChildCount: initial.length });
  const createdPath = `${collectionsPath}/${saved.Id}`;
  let created = false;
  api.handlers.set(`GET /admin/v1/libraries/${library.Id}/items`, (route, request) => {
    const parameters = new URL(request.url()).searchParams;
    const start = Number(parameters.get('StartIndex') ?? 0), limit = Number(parameters.get('Limit') ?? 25);
    const term = parameters.get('SearchTerm')?.toLowerCase() ?? '';
    const matches = candidates.filter((candidate) => candidate.Name.toLowerCase().includes(term));
    return json(route, { Library: library, Items: matches.slice(start, start + limit), TotalRecordCount: matches.length, StartIndex: start, Limit: limit });
  });
  api.handlers.set(`GET ${collectionsPath}`, (route) => json(route, { Items: created ? [state.saved, saved] : [state.saved], TotalRecordCount: created ? 2 : 1 }));
  api.handlers.set(`POST ${collectionsPath}`, (route, request) => {
    expect(request.postDataJSON()).toEqual({ Name: saved.Name, IsPublic: false, IsLocked: false, Ids: initial.map((item) => item.Id) });
    created = true;
    return json(route, { Id: saved.Id, Name: saved.Name });
  });
  api.handlers.set(`GET ${createdPath}`, (route) => json(route, saved));
  api.handlers.set(`GET ${createdPath}/items`, (route) => json(route, { Items: initial, TotalRecordCount: initial.length }));
  await page.goto('/admin/media/collections');
  const draft = await openNewCollection(page);
  await draft.getByRole('textbox', { name: /^Collection name/ }).fill(saved.Name);
  await draft.getByRole('checkbox', { name: /The feature film/ }).check();
  await draft.getByRole('button', { name: 'Go to next page', exact: true }).click();
  await draft.getByRole('checkbox', { name: /Last page film/ }).check();
  const draftRows = draft.getByRole('list', { name: 'Draft collection items', exact: true }).getByRole('listitem');
  await expect(draftRows).toHaveCount(2);
  await expect(draftRows.first()).toContainText('The feature film');
  await expect(draftRows.last()).toContainText('Last page film');
  const search = draft.getByRole('textbox', { name: 'Find media', exact: true });
  await search.fill('The feature film');
  await search.press('Enter');
  await expect(draft.getByRole('checkbox', { name: /The feature film/ })).toBeChecked();
  await expect(draft.getByRole('checkbox', { name: /Last page film/ })).toHaveCount(0);
  await expect(draftRows).toHaveCount(2);
  expect(api.writes(), 'Searching and paging must not persist initial membership.').toEqual([]);
  await draft.getByRole('button', { name: 'Create collection', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Manage collection', exact: true });
  const rows = dialog.getByRole('list', { name: 'Collection items', exact: true }).getByRole('listitem');
  await expect(rows).toHaveCount(2);
  await expect(rows.first()).toContainText(initial[0].Name);
  await expect(rows.last()).toContainText(initial[1].Name);
  expect(api.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([
    { method: 'POST', path: collectionsPath, body: { Name: saved.Name, IsPublic: false, IsLocked: false, Ids: initial.map((item) => item.Id) } },
  ]);
  expectCollectionOnlyRequests(api);
});

test('an accepted creation ID is reloaded after a failed detail read without replaying initial membership', async ({ page, api }) => {
  const saved = collection({ Id: 'readback-created-collection', Name: 'Readback cinema', ChildCount: 1 });
  const createdPath = `${collectionsPath}/${saved.Id}`;
  let created = false, failReadback = true;
  api.handlers.set(`GET ${collectionsPath}`, (route) => json(route, { Items: created ? [saved] : [], TotalRecordCount: created ? 1 : 0 }));
  api.handlers.set(`POST ${collectionsPath}`, (route, request) => {
    expect(request.postDataJSON()).toEqual({ Name: saved.Name, IsPublic: false, IsLocked: false, Ids: ['film-one'] });
    created = true;
    return json(route, { Id: saved.Id, Name: saved.Name });
  });
  api.handlers.set(`GET ${createdPath}`, (route) => {
    if (failReadback) {
      failReadback = false;
      return json(route, { Error: { Code: 'readback_unavailable', Message: 'The created collection details could not be loaded.' } }, 500);
    }
    return json(route, saved);
  });
  api.handlers.set(`GET ${createdPath}/items`, (route) => json(route, { Items: [{ Id: 'film-one', Name: 'The feature film', Type: 'Movie' }], TotalRecordCount: 1 }));
  await page.goto('/admin/media/collections');
  const draft = await openNewCollection(page);
  await draft.getByRole('textbox', { name: /^Collection name/ }).fill(saved.Name);
  await draft.getByRole('checkbox', { name: /The feature film/ }).check();
  await draft.getByRole('button', { name: 'Create collection', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Manage collection', exact: true });
  await expect(dialog.getByText('The created collection details could not be loaded.', { exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Create collection', exact: true })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Save details', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
  const readsBeforeReload = api.reads(createdPath).length;
  await dialog.getByRole('button', { name: 'Retry', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Discard unsaved changes?', exact: true });
  await confirmation.getByRole('button', { name: 'Discard changes', exact: true }).click();
  await expect(dialog.getByRole('textbox', { name: /^Collection name/ })).toHaveValue(saved.Name);
  await expect(dialog.getByRole('list', { name: 'Collection items', exact: true }).getByRole('listitem')).toHaveCount(1);
  await expect(dialog.getByRole('checkbox', { name: /The feature film/ })).not.toBeChecked();
  await expect(dialog.getByRole('button', { name: 'Add selected media', exact: true })).toBeDisabled();
  expect(api.reads(createdPath).length).toBeGreaterThan(readsBeforeReload);
  expect(api.writes(), 'Reloading an accepted ID must not replay creation or add its initial members again.').toHaveLength(1);
  expectCollectionOnlyRequests(api);
});

test('confirmed collection deletion stays blocked while its single mutation is pending', async ({ page, api }) => {
  const state = installCollections(api);
  let removed = false;
  let releaseDelete = () => {};
  const deleteReady = new Promise<void>((resolve) => { releaseDelete = () => resolve(); });
  api.handlers.set(`GET ${collectionsPath}`, (route) => json(route, { Items: removed ? [] : [state.saved], TotalRecordCount: removed ? 0 : 1 }));
  api.handlers.set(`DELETE ${collectionPath}`, async (route) => {
    await deleteReady;
    removed = true;
    return json(route, {});
  });
  await page.goto('/admin/media/collections');
  const dialog = await openCollection(page);
  await dialog.getByRole('button', { name: 'Delete collection', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Delete collection', exact: true });
  expect(api.writes(), 'Opening the confirmation must not delete the collection.').toEqual([]);
  await confirmation.getByRole('button', { name: 'Delete collection', exact: true }).click();
  try {
    await expect(confirmation.getByRole('button', { name: 'Delete collection', exact: true })).toBeDisabled();
    await expect(confirmation.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled();
    expect(api.writes()).toHaveLength(1);
    expect(api.writes()[0]).toMatchObject({ method: 'DELETE', path: collectionPath, body: null });
  } finally {
    releaseDelete();
  }
  await expect(confirmation).toHaveCount(0);
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('main').getByRole('button', { name: 'Manage Weekend cinema', exact: true })).toHaveCount(0);
  expect(api.writes(), 'Confirmed deletion must be sent only once.').toHaveLength(1);
  expectCollectionOnlyRequests(api);
});

test('renaming a public shared collection preserves hidden access settings and submits only its name', async ({ page, api }) => {
  const shares = [{ UserId: 'viewer-user', CanEdit: false }];
  const state = installCollections(api, { IsPublic: true, Shares: structuredClone(shares) });
  const renamed = 'Saturday favorites';
  api.handlers.set(`POST ${collectionPath}`, (route, request) => {
    const input = request.postDataJSON() as CollectionPatch;
    expect(input).toEqual({ Name: renamed });
    Object.assign(state.saved, input);
    return json(route, state.saved);
  });
  await page.goto('/admin/media/collections');
  const dialog = await openCollection(page);
  await expectCollectionOnlyDialog(dialog);
  await dialog.getByRole('textbox', { name: /^Collection name/ }).fill(renamed);
  await dialog.getByRole('button', { name: 'Save details', exact: true }).click();
  await expect(dialog.getByText(`${renamed} saved.`, { exact: true })).toBeVisible();
  await expect(dialog.getByRole('textbox', { name: /^Collection name/ })).toBeEnabled();
  expect(api.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([
    { method: 'POST', path: collectionPath, body: { Name: renamed } },
  ]);
  expect(state.saved).toMatchObject({ Name: renamed, IsPublic: true, Shares: shares });
  expectCollectionOnlyRequests(api);
});

test('locking a public shared collection preserves hidden access settings and submits only the lock', async ({ page, api }) => {
  const shares = [{ UserId: 'viewer-user', CanEdit: true }];
  const state = installCollections(api, { IsPublic: true, Shares: structuredClone(shares) });
  api.handlers.set(`POST ${collectionPath}`, (route, request) => {
    const input = request.postDataJSON() as CollectionPatch;
    expect(input).toEqual({ IsLocked: true });
    Object.assign(state.saved, input);
    return json(route, state.saved);
  });
  await page.goto('/admin/media/collections');
  const dialog = await openCollection(page);
  await dialog.getByRole('switch', { name: 'Lock membership and deletion', exact: true }).check();
  await dialog.getByRole('button', { name: 'Save details', exact: true }).click();
  await expect(dialog.getByText('Weekend cinema saved.', { exact: true })).toBeVisible();
  await expect(dialog.getByRole('switch', { name: 'Lock membership and deletion', exact: true })).toBeChecked();
  expect(api.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([
    { method: 'POST', path: collectionPath, body: { IsLocked: true } },
  ]);
  expect(state.saved).toMatchObject({ Name: 'Weekend cinema', IsLocked: true, IsPublic: true, Shares: shares });
  expectCollectionOnlyRequests(api);
});

test.describe('raw collection media names in Chinese', () => {
  test.use({ preferences: { language: 'zh', theme: 'light' } });
  test('candidate and draft names keep their actual values when they match interface translation keys', async ({ page, api }) => {
    installCollections(api);
    const item: MetadataItemSummary = {
      Id: 'film-one', LibraryId: library.Id, ParentId: '', ParentName: 'Collections', Name: 'Settings', Type: 'Movie',
      Path: '/media/movies/Settings.mkv', IsFolder: false, ProductionYear: 2025, IndexNumber: null,
      ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0,
    };
    api.handlers.set(`GET /admin/v1/libraries/${library.Id}/items`, (route) => json(route, { Library: library, Items: [item], TotalRecordCount: 1, StartIndex: 0, Limit: 25 }));
    await page.goto('/admin/media/collections');
    await page.getByRole('main').getByRole('button', { name: '新建合集', exact: true }).first().click();
    const dialog = page.getByRole('dialog', { name: '创建合集', exact: true });
    await expect(dialog.getByText('Settings', { exact: true })).toBeVisible();
    await expect(dialog.getByText('Collections', { exact: true })).toBeVisible();
    await dialog.getByRole('checkbox', { name: /^Settings/ }).check();
    const draftItems = dialog.getByRole('list', { name: '合集草稿条目', exact: true });
    await expect(draftItems.getByRole('listitem').first()).toContainText('Settings');
    await expect(dialog.getByText('1 个草稿条目', { exact: true })).toBeVisible();
    await dialog.getByRole('button', { name: '从合集草稿中移除 Settings', exact: true }).click();
    await expect(dialog.getByRole('checkbox', { name: /^Settings/ })).not.toBeChecked();
    await expect(draftItems).toHaveCount(0);
    expect(api.writes(), 'Candidate selection and draft removal must not change the saved collection.').toEqual([]);
    expectCollectionOnlyRequests(api);
    await evidence(page, test.info(), 'design2-collections-create-raw-names-zh');
  });
});
