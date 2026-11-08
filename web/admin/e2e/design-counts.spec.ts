import type { Request } from '@playwright/test';
import { administrator, expect, json, library, seriesLibrary, test } from './dashboard-fixture';
import type { LibraryStorageDirectory } from '../src/api';

// Read-only synthetic cardinalities exercise rendered grammar, including empty
// states. Deliberately plural-looking record names must remain original data.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'light' } });

test('English count summaries distinguish zero, one and many while record names stay unchanged', async ({ page, api }) => {
  let count = 0;
  const expected = [
    { users: '0 Users', administrators: '0 Administrators', entries: '0 entries', collections: '0 collections', items: '0 items', directories: '0 directories' },
    { users: '1 User', administrators: '1 Administrator', entries: '1 entry', collections: '1 collection', items: '1 item', directories: '1 directory' },
    { users: '2 Users', administrators: '2 Administrators', entries: '2 entries', collections: '2 collections', items: '2 items', directories: '2 directories' },
  ];
  const pageOf = (request: Request, items: unknown[]) => {
    const url = new URL(request.url());
    const start = Number(url.searchParams.get('StartIndex') ?? 0);
    const limit = Number(url.searchParams.get('Limit') ?? 25);
    return { Items: items.slice(start, start + limit), TotalRecordCount: items.length, StartIndex: start, Limit: limit };
  };
  const records = <T>(create: (index: number) => T) => Array.from({ length: count }, (_value, index) => create(index));
  api.handlers.set('GET /admin/v1/users', (route) => json(route, { Items: records((index) => ({ ...administrator, Id: index === 0 ? administrator.Id : '2'.repeat(32), Name: index === 0 ? '1 Administrators' : 'Unchanged administrator 2' })), TotalRecordCount: count }));
  api.handlers.set('GET /admin/v1/entities', (route, request) => json(route, pageOf(request, records((index) => ({ Id: `count-person-${index}`, Name: index === 0 ? '1 entries' : 'Unchanged second entry', Type: 'Person' })))));
  api.handlers.set('GET /admin/v1/collections', (route) => json(route, { Items: records((index) => ({ Id: `count-collection-${index}`, Name: index === 0 ? '1 collections' : 'Unchanged second collection', Type: 'BoxSet', IsFolder: true, ParentId: '', OwnerId: administrator.Id, MediaType: 'Video', IsPublic: false, IsLocked: false, ChildCount: 0, Shares: [] })), TotalRecordCount: count }));
  api.handlers.set(`GET /admin/v1/libraries/${library.Id}/items`, (route, request) => json(route, { Library: library, ...pageOf(request, records((index) => ({ Id: `count-film-${index}`, LibraryId: library.Id, ParentId: '', ParentName: '', Name: index === 0 ? '1 items' : 'Unchanged second title', Type: 'Movie', Path: `/media/movies/count-${index}.mkv`, IsFolder: false, ProductionYear: 2025, IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0 }))) }));

  for (const [index, words] of expected.entries()) await test.step(`Render English ${index}-record summaries`, async () => {
    count = index;
    const directories = records((recordIndex): LibraryStorageDirectory => ({
      Path: [library, seriesLibrary][recordIndex].Paths[0], Available: true, Readable: true, Writable: false,
      TotalBytes: '1099511627776', UsedBytes: '450971566080', AvailableBytes: '644245094400',
    }));
    api.handlers.set('GET /admin/v1/storage/library-directories', (route) => json(route, { Items: directories, TotalRecordCount: directories.length }));

    await page.goto('/admin/access/users');
    const main = page.getByRole('main');
    const users = main.locator('.access-page-summary');
    await expect(users.getByText(words.users, { exact: true })).toBeVisible();
    await expect(users.getByText(words.administrators, { exact: true })).toBeVisible();
    if (count) await expect(main.getByRole('button', { name: 'Manage 1 Administrators', exact: true })).toContainText('1 Administrators');

    await page.goto('/admin/media/artwork');
    await expect(main.locator('.ac-toolbar-count')).toHaveText(words.entries);
    if (count) await expect(main.getByRole('button', { name: 'Manage artwork for 1 entries', exact: true }).first()).toContainText('1 entries');

    await page.goto('/admin/media/collections');
    await expect(main.locator('.ac-page-heading p')).toHaveText(words.collections);
    await expect(main.locator('.ac-toolbar-count')).toHaveText(words.collections);
    if (count) await expect(main.getByRole('button', { name: 'Manage 1 collections', exact: true }).first()).toContainText('1 collections');

    await page.goto(`/admin/media/libraries/${library.Id}/items`);
    await expect(main.locator('.media-library-summary').getByText(words.items, { exact: true })).toBeVisible();
    await expect(main.getByRole('tabpanel', { name: 'Items', exact: true }).getByText(words.items, { exact: true })).toBeVisible();
    if (count) await expect(main.getByRole('button', { name: 'Edit metadata for 1 items', exact: true }).first()).toContainText('1 items');

    await page.goto('/admin/');
    await expect(main.getByRole('region', { name: 'Directory with least available space status', exact: true }).getByText(words.directories, { exact: true })).toBeVisible();
    expect(api.writes()).toEqual([]);
  });
});
