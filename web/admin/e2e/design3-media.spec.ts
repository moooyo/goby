import type { Job, Library, LibraryStorageDirectory, MetadataDetail, TaskDefinition, TaskRun } from '../src/api';
import type { MetadataCandidate } from '../src/providersApi';
import { analysisItem, csrf, expect, json, library, movieMetadata, seriesLibrary, stamp, test } from './dashboard-fixture';

const libraryList = '/admin/v1/libraries';
const directoriesPath = '/admin/v1/storage/library-directories';
const itemsPath = `${libraryList}/${library.Id}/items`;
const providerPath = `/admin/v1/items/${movieMetadata.Item.Id}/providers`;
const featuredItemsUrl = `/admin/media/libraries/${library.Id}/items`;
const GiB = 1024n ** 3n;

function directory(Path: string, fields: Partial<LibraryStorageDirectory> = {}): LibraryStorageDirectory {
  return { Path, Available: true, Readable: true, Writable: false, TotalBytes: String(100n * GiB), UsedBytes: String(90n * GiB), AvailableBytes: String(7n * GiB), ...fields };
}
function scan(Id: string, LibraryId: string, fields: Partial<Job> = {}): Job {
  return { Id, LibraryId, ForceProbe: false, Status: 'completed', Error: '', Scanned: 10, Added: 3, Updated: 4, CreatedAt: stamp, StartedAt: stamp, FinishedAt: stamp, ...fields };
}

const match: MetadataCandidate = { Provider: 'tmdb', Id: '12345', Type: 'Movie', Name: 'First movie', OriginalTitle: 'Original first movie', Year: 2020, Language: 'en-US', Overview: 'The first provider result.', Score: 985.3 };
const secondMatch: MetadataCandidate = { ...match, Id: '67890', Name: 'Second movie', Year: 2025, Language: 'fr-FR' };

test.use({ serviceWorkers: 'block', preferences: { language: 'en', theme: 'light' } });

test('library results use latest exact-scope jobs and distinguish unreadable, unknown, and shared paths', async ({ page, api }) => {
  const shared = '/media/movies';
  const cinematic: Library = { ...library, Paths: [shared, shared, '/media/movies/new'] };
  const tv: Library = { ...seriesLibrary, Paths: [shared, '/media/series'] };
  const unscanned: Library = { ...library, Id: 'never-scanned', Name: 'New library', Paths: ['/Media/movies'], LastScanAt: null };
  const libraries = [cinematic, tv, unscanned];
  api.handlers.set(`GET ${libraryList}`, (route) => json(route, { Items: libraries, TotalRecordCount: libraries.length }));
  api.handlers.set(`GET ${libraryList}/${unscanned.Id}/items`, (route, request) => {
    const params = new URL(request.url()).searchParams;
    return json(route, { Library: unscanned, Items: [], TotalRecordCount: 0, StartIndex: Number(params.get('StartIndex')), Limit: Number(params.get('Limit')) });
  });
  let jobs = [
    scan('old-film', cinematic.Id, { Added: 999, Updated: 999 }),
    scan('latest-film', cinematic.Id, { CreatedAt: '2026-11-06T09:00:00Z' }),
    scan('latest-tv', tv.Id, { Status: 'failed', Error: 'Media directory cannot be read.', FinishedAt: '2026-11-05T07:11:00Z' }),
    scan('unrelated-result', 'unrelated', { Added: 888, Updated: 888 }),
  ];
  api.handlers.set('GET /admin/v1/jobs', (route) => json(route, { Items: jobs, TotalRecordCount: jobs.length }));
  api.handlers.set(`GET ${directoriesPath}`, (route) => json(route, { Items: [
    directory(shared),
    directory('/media/series', { Available: false, Readable: false, Writable: null, TotalBytes: null, UsedBytes: null, AvailableBytes: null }),
    directory('/media/movies/new', { Available: false, Readable: null, Writable: null, TotalBytes: null, UsedBytes: null, AvailableBytes: null }),
  ], TotalRecordCount: 3 }));

  await page.goto('/admin/media/libraries');
  const main = page.getByRole('main');
  const table = main.getByRole('table');
  const filmRow = table.getByRole('row', { name: /Feature films/ });
  const tvRow = table.getByRole('row', { name: /TV series/ });
  const unscannedRow = table.getByRole('row', { name: /New library/ });
  await expect(filmRow).toContainText('Added 3 · updated 4');
  await expect(filmRow).not.toContainText('999');
  await expect(filmRow).toContainText('2 directories · Read access unknown');
  await expect(tvRow).toContainText('1 directory unreadable');
  await expect(tvRow).toContainText('Media directory cannot be read.');
  await expect(tvRow.locator('time')).toHaveAttribute('datetime', '2026-11-05T07:11:00Z');
  await expect(tvRow.locator('time')).toContainText('Scan failed');
  await expect(unscannedRow).toContainText('Not scanned yet');
  await expect(unscannedRow).toContainText('1 directory · Read access unknown');
  const storage = main.getByRole('region', { name: 'Storage directories', exact: true });
  const sharedDirectory = storage.getByRole('listitem', { name: shared, exact: true });
  await expect(sharedDirectory).toContainText('Feature films · TV series');
  await expect(sharedDirectory).toContainText('Read only');
  await expect(sharedDirectory).toContainText('90 / 100 GiB');
  await expect(sharedDirectory).toContainText('7 GiB available');
  await expect(sharedDirectory).toHaveAttribute('data-warning', 'true');
  await expect(storage.getByRole('progressbar')).toHaveCount(1);
  await expect(storage.getByRole('listitem')).toHaveCount(4);
  await expect(storage.getByRole('listitem', { name: '/media/movies/new', exact: true })).toContainText('Read access unknown');

  jobs = [...jobs, ...Array.from({ length: 996 }, (_, index) => scan(`bounded-${index}`, 'another-library'))];
  await main.getByRole('button', { name: 'Refresh scan results' }).click();
  await expect(unscannedRow).toContainText('No recent scan result');
  await expect(main).toContainText('Older scans may be outside the job history returned by the server.');
  expect(api.writes()).toHaveLength(0);
});

for (const language of ['en', 'zh'] as const) test.describe(`${language} library receipts`, () => {
  test.use({ preferences: { language, theme: 'light' } });

  test('localizes scan and refresh admissions without translating the library name', async ({ page, api }) => {
    const zh = language === 'zh';
    const namedLibrary = { ...library, Name: 'Movies {name} 与收藏' };
    api.handlers.set(`GET ${libraryList}`, (route) => json(route, { Items: [namedLibrary], TotalRecordCount: 1 }));
    api.handlers.set(`POST ${libraryList}/${library.Id}/scan`, (route, request) => json(route, { Job: scan('admitted-job', library.Id, { ForceProbe: request.postData() != null, Status: 'pending', FinishedAt: null }) }, 202));
    await page.goto('/admin/media/libraries');
    const row = page.getByRole('row').filter({ hasText: namedLibrary.Name });
    const receipt = page.locator('.goby-snackbar .goby-alert-message');
    await row.getByRole('button', { name: zh ? `扫描媒体库「${namedLibrary.Name}」` : `Scan library ${namedLibrary.Name}`, exact: true }).click();
    await expect(receipt).toHaveText(zh ? `已受理扫描「${namedLibrary.Name}」的请求。` : `Scan requested for ${namedLibrary.Name}.`);
    await expect(receipt.locator('[translate="no"]')).toHaveText(namedLibrary.Name);
    await row.getByRole('button', { name: zh ? `「${namedLibrary.Name}」的更多操作` : `More actions for ${namedLibrary.Name}`, exact: true }).click();
    const refresh = zh ? '刷新媒体详情' : 'Refresh media details';
    await page.getByRole('menuitem', { name: refresh, exact: true }).click();
    await page.getByRole('dialog', { name: zh ? `${refresh}？` : `${refresh}?`, exact: true }).getByRole('button', { name: refresh, exact: true }).click();
    await expect(receipt).toHaveText(zh ? `已受理刷新「${namedLibrary.Name}」媒体详情的请求。` : `Media details refresh requested for ${namedLibrary.Name}.`);
    await expect(receipt.locator('[translate="no"]')).toHaveText(namedLibrary.Name);
    expect(api.writes().map(({ body }) => body)).toEqual([null, { ForceProbe: true }]);
  });
});

test('item-type choices follow library content and send one exact server filter with search', async ({ page, api }) => {
  const musicLibrary: Library = { ...library, Id: 'music-library', Name: 'Music catalog', CollectionType: 'music' };
  api.handlers.set(`GET ${libraryList}`, (route) => json(route, { Items: [library, seriesLibrary, musicLibrary], TotalRecordCount: 3 }));
  api.handlers.set(`GET ${libraryList}/${musicLibrary.Id}/items`, (route, request) => {
    const params = new URL(request.url()).searchParams;
    return json(route, { Library: musicLibrary, Items: [], TotalRecordCount: 0, StartIndex: Number(params.get('StartIndex')), Limit: Number(params.get('Limit')) });
  });
  await page.goto(featuredItemsUrl);
  const type = page.getByRole('combobox', { name: 'Item type' });
  await expect(type.getByRole('option')).toHaveText(['All types', 'Movie', 'Other video', 'Folder']);
  await page.getByRole('textbox', { name: 'Search title' }).fill('rare clip');
  const filtered = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return url.pathname === itemsPath && url.searchParams.get('Types') === 'Video';
  });
  await type.selectOption('Video');
  expect(Object.fromEntries(new URL((await filtered).url()).searchParams)).toEqual({ StartIndex: '0', Limit: '50', SearchTerm: 'rare clip', Types: 'Video' });

  await page.goto(`/admin/media/libraries/${seriesLibrary.Id}/items`);
  await expect(type.getByRole('option')).toHaveText(['All types', 'Series', 'Season', 'Episode']);
  await page.goto(`/admin/media/libraries/${musicLibrary.Id}/items`);
  await expect(type.getByRole('option')).toHaveText(['All types', 'Music artist', 'Music album', 'Audio']);
  expect(api.writes()).toHaveLength(0);
});

test('the file tab shows the known library name without another lookup', async ({ page, api }) => {
  await page.goto(featuredItemsUrl);
  await page.getByRole('button', { name: `Edit metadata for ${movieMetadata.Item.Name}`, exact: true }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('tab', { name: 'File', exact: true }).click();
  const panel = dialog.getByRole('tabpanel', { name: 'File', exact: true });
  await expect(panel.getByText(library.Name, { exact: true })).toHaveAttribute('translate', 'no');
  await expect(panel.getByText(library.Id, { exact: true })).toHaveCount(0);
  expect(api.reads(`${libraryList}/${library.Id}`)).toHaveLength(0);
});

test('re-identification applies only the explicitly selected provider result with the read revision', async ({ page, api }) => {
  const original = structuredClone(movieMetadata);
  original.LockedFields = ['Name']; original.LockedValues = { Name: original.Effective.Name };
  const secondItemName = 'Unrelated movie';
  api.handlers.set(`GET ${itemsPath}`, (route, request) => {
    const params = new URL(request.url()).searchParams;
    const item = { ...original.Item, ProductionYear: original.Effective.ProductionYear, IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 1 };
    const items = [item, { ...item, Id: 'unrelated-movie', Name: secondItemName }];
    const limit = Number(params.get('Limit'));
    return json(route, { Library: library, Items: items.slice(0, limit), TotalRecordCount: items.length, StartIndex: Number(params.get('StartIndex')), Limit: limit });
  });
  api.handlers.set(`GET /admin/v1/items/${original.Item.Id}/metadata`, (route) => json(route, original));
  api.handlers.set(`POST ${providerPath}/search`, (route) => json(route, { Items: [match, secondMatch] }));
  api.handlers.set(`POST ${providerPath}/apply`, (route) => json(route, { ...original, Revision: '9007199254740994', Automatic: { ...original.Automatic, Name: secondMatch.Name } }));
  await page.goto(featuredItemsUrl);
  await page.getByRole('checkbox', { name: `Select: ${original.Item.Name}`, exact: true }).check();
  const identify = page.getByRole('button', { name: 'Re-identify', exact: true });
  await expect(identify).toBeVisible();
  await page.getByRole('checkbox', { name: `Select: ${secondItemName}`, exact: true }).check();
  await expect(identify).toHaveCount(0);
  await page.getByRole('checkbox', { name: `Select: ${secondItemName}`, exact: true }).uncheck();
  await identify.click();
  const dialog = page.getByRole('dialog', { name: 'Re-identify', exact: true });
  const apply = dialog.getByRole('button', { name: 'Apply identification' });
  await expect(apply).toBeDisabled();
  await dialog.getByRole('button', { name: 'Search candidates' }).click();
  await expect(dialog.getByRole('radio')).toHaveCount(2);
  await expect(apply).toBeDisabled();
  await dialog.getByRole('radio', { name: 'Second movie (2025)' }).check();
  await expect(dialog).toContainText('Manual overrides and locked fields stay unchanged.');
  await expect(dialog).not.toContainText('985.3%');
  await apply.click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('status').filter({ hasText: 'Identification applied: Second movie.' })).toBeVisible();
  await expect(page.getByRole('checkbox', { name: `Select: ${original.Item.Name}`, exact: true })).toBeChecked();
  expect(api.writes().filter((entry) => entry.path.startsWith(providerPath))).toEqual([
    { method: 'POST', path: `${providerPath}/search`, csrf, body: { Provider: 'tmdb', Name: original.Effective.Name, Year: 2025, Language: '' } },
    { method: 'POST', path: `${providerPath}/apply`, csrf, body: { Provider: 'tmdb', Id: secondMatch.Id, Language: secondMatch.Language, Revision: original.Revision } },
  ]);
});

test('a conflicted identification requires a fresh read and a new explicit candidate selection', async ({ page, api }) => {
  const latest = '9007199254740997';
  let metadata: MetadataDetail = structuredClone(movieMetadata);
  api.handlers.set(`GET /admin/v1/items/${metadata.Item.Id}/metadata`, (route) => json(route, metadata));
  api.handlers.set(`POST ${providerPath}/search`, (route) => json(route, { Items: [match] }));
  api.handlers.set(`POST ${providerPath}/apply`, (route) => {
    metadata = { ...metadata, Revision: latest };
    return json(route, { Error: { Code: 'metadata_conflict', Message: 'Metadata changed.' } }, 409);
  });
  await page.goto(featuredItemsUrl);
  await page.getByRole('checkbox', { name: `Select: ${metadata.Item.Name}`, exact: true }).check();
  await page.getByRole('button', { name: 'Re-identify', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Re-identify', exact: true });
  const apply = dialog.getByRole('button', { name: 'Apply identification' });
  await dialog.getByRole('button', { name: 'Search candidates' }).click();
  await dialog.getByRole('radio').check();
  await apply.click();
  await expect(dialog.getByText('Metadata changed.', { exact: true })).toBeVisible();
  await expect(apply).toBeDisabled();
  await dialog.getByRole('button', { name: 'Reload latest item' }).click();
  await expect(dialog.getByRole('button', { name: 'Search candidates' })).toBeEnabled();
  await expect(dialog.getByRole('radio')).toHaveCount(0);
  await expect(apply).toBeDisabled();
  expect(api.writes().filter((entry) => entry.path.endsWith('/apply'))).toHaveLength(1);
});

test('analysis inventory and credits selectors offer only supported video libraries', async ({ page, api }) => {
  const music: Library = { ...library, Id: 'music-library', Name: 'Music catalog', CollectionType: 'music' };
  const homeVideos: Library = { ...library, Id: 'mixed-video-library', Name: 'Home videos', CollectionType: 'mixed' };
  const libraries = [library, seriesLibrary, music, homeVideos];
  api.handlers.set(`GET ${libraryList}`, (route) => json(route, { Items: libraries, TotalRecordCount: libraries.length }));
  await page.goto('/admin/media/analysis');
  await expect(page.getByRole('combobox', { name: 'Filter library' }).getByRole('option')).toHaveText(['All libraries', library.Name, seriesLibrary.Name, homeVideos.Name]);
  await expect(page.getByRole('combobox', { name: 'Credits analysis library' }).getByRole('option')).toHaveText(['Choose a library', library.Name, seriesLibrary.Name, homeVideos.Name]);
  await expect(page.getByRole('table', { name: 'Media analysis items' })).toBeVisible();
  const filtered = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return url.pathname === '/admin/v1/media-analysis/items' && url.searchParams.get('LibraryId') === seriesLibrary.Id;
  });
  await page.getByRole('combobox', { name: 'Filter library' }).selectOption(seriesLibrary.Id);
  await filtered;
});

test('a music-only catalog has an analysis empty state without inventory reads or blank selectors', async ({ page, api }) => {
  const music: Library = { ...library, Id: 'music-library', Name: 'Music catalog', CollectionType: 'music' };
  api.handlers.set(`GET ${libraryList}`, (route) => json(route, { Items: [music], TotalRecordCount: 1 }));
  await page.goto('/admin/media/analysis');
  await expect(page.getByText('No video libraries are available for analysis.', { exact: true })).toHaveCount(2);
  await expect(page.getByRole('combobox', { name: 'Filter library' })).toHaveCount(0);
  await expect(page.getByRole('combobox', { name: 'Credits analysis library' })).toHaveCount(0);
  expect(api.reads('/admin/v1/media-analysis/items')).toHaveLength(0);
});

for (const language of ['en', 'zh'] as const) test.describe(`${language} analysis labels`, () => {
  test.use({ preferences: { language, theme: 'light' } });

  test('localizes the analysis headings and distinguishes library and item detection', async ({ page, api }) => {
    const zh = language === 'zh';
    api.handlers.set(`GET /admin/v1/items/${analysisItem.Id}/credits`, (route) => json(route, {
      ItemId: analysisItem.Id, MediaSourceId: analysisItem.MediaSourceId, SourceRevision: analysisItem.SourceRevision, Revision: '1', DurationTicks: 1_000_000_000,
      Automatic: null, Effective: null, Override: null, OverrideStale: false, LastEditedBy: '', LastEditedAt: null,
      Detected: [], DetectedStale: false, DetectedRevision: '0', DetectedStatus: 'not_analyzed', DetectedReason: '', DetectedUpdatedAt: null,
    }));
    await page.goto('/admin/media/analysis');
    const table = page.getByRole('table', { name: zh ? '媒体分析条目' : 'Media analysis items' });
    await expect(table.getByRole('columnheader')).toHaveText(zh ? ['条目', '片头分析', 'BIF 预览', '片尾标记'] : ['Item', 'Intro analysis', 'Seek previews', 'Credits markers']);
    await expect(page.getByRole('button', { name: zh ? '检测此媒体库的片尾' : 'Detect credits in this library', exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: zh ? '重新检测此媒体库的片尾' : 'Detect credits in this library again', exact: true })).toBeVisible();
    await table.getByRole('button', { name: zh ? /^查看「.+」的片尾标记$/ : /^View credits markers for / }).click();
    const dialog = page.getByRole('dialog', { name: zh ? '片尾标记' : 'Credits marker', exact: true });
    await expect(dialog.getByRole('button', { name: zh ? '识别此影片片尾' : 'Detect credits', exact: true })).toBeVisible();
  });
});

function taskRun(Id: string, TaskId: string, fields: Partial<TaskRun> = {}): TaskRun {
  return { Id, TaskId, State: 'running', Source: 'manual', RequestId: null, CreatedAt: stamp, StartedAt: stamp, FinishedAt: null, StopRequestedAt: null,
    StopReason: '', ErrorCode: '', ErrorMessage: '', TotalChildren: 3, TerminalChildren: 1, CompletedChildren: 1, FailedChildren: 0,
    CancelledChildren: 0, InterruptedChildren: 0, UnavailableChildren: 0, Scanned: 4, Added: 0, Updated: 0, ...fields };
}
function task(Id: string, Key: string, Name: string, CurrentRun: TaskRun | null, LastRun: TaskRun | null): TaskDefinition {
  return { Id, Key, Name, CurrentRun, LastRun, Description: '', Category: 'Media analysis', IsHidden: false, Enabled: true, Revision: '1', ScheduleTimezone: 'UTC', Triggers: [], NextRunAt: null };
}

test('analysis lists actual task outcomes and deep-links to the reported run', async ({ page, api }) => {
  const failed = taskRun('c'.repeat(32), 'credits', { State: 'failed', ErrorCode: 'child_failed', ErrorMessage: 'A source could not be decoded.', FinishedAt: stamp, TerminalChildren: 3, CompletedChildren: 2, FailedChildren: 1 });
  const unrelated = taskRun('d'.repeat(32), 'downloads', { State: 'failed', FinishedAt: stamp, TerminalChildren: 3, CompletedChildren: 2, FailedChildren: 1 });
  const running = taskRun('e'.repeat(32), 'previews');
  const tasks = [task('credits', 'media.credits_analysis', 'Analyze credits', null, failed), task('previews', 'media.preview_generation', 'Generate seek previews', running, null), task('downloads', 'subtitles.download', 'Download missing subtitles', null, unrelated)];
  api.handlers.set('GET /admin/v1/tasks', (route) => json(route, { Items: tasks, TotalRecordCount: tasks.length }));
  api.handlers.set(`GET /admin/v1/task-runs/${failed.Id}`, (route, request) => {
    const params = new URL(request.url()).searchParams;
    return json(route, { Run: failed, Children: { Items: [], TotalRecordCount: 0, StartIndex: Number(params.get('StartIndex')), Limit: Number(params.get('Limit')) } });
  });
  await page.goto('/admin/media/analysis');
  const matrix = page.getByRole('region', { name: 'Library automation' });
  await expect(matrix.getByRole('table')).toContainText('Enabled');
  await expect(matrix.getByRole('table')).toContainText('Not applicable');
  await expect(matrix.getByRole('progressbar')).toHaveCount(0);
  const queue = page.getByRole('region', { name: 'Queue' });
  await expect(queue).toContainText('Generate seek previews');
  const failures = page.getByRole('region', { name: 'Failures and interruptions' });
  await expect(failures).toContainText('A source could not be decoded.');
  await expect(failures).toContainText('Partial failure');
  await expect(failures).not.toContainText('Download missing subtitles');
  await failures.getByRole('button', { name: 'View task' }).click();
  await expect(page).toHaveURL(new RegExp(`/admin/system/tasks\\?run=${failed.Id}$`));
  await expect(page.getByRole('dialog', { name: 'Run details · Analyze credits' })).toContainText('A source could not be decoded.');
});
